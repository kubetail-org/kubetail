// Copyright 2024 Andres Morey
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package grpchelpers

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	eventbus "github.com/asaskevich/EventBus"
	zlog "github.com/rs/zerolog/log"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/resolver"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

var agentLabelSet = labels.Set{
	"app.kubernetes.io/name":      "kubetail",
	"app.kubernetes.io/component": "agent",
}

var agentLabelSelectorString = labels.SelectorFromSet(agentLabelSet).String()

// Agent Resolver
type agentResolver struct {
	cc       resolver.ClientConn
	nodeName string
	addr     resolver.Address
	eventBus eventbus.Bus
	mu       sync.Mutex
}

// no-op
func (r *agentResolver) ResolveNow(o resolver.ResolveNowOptions) {}

// Stop background processes
func (r *agentResolver) Close() {
	zlog.Debug().Msg("closing agent resolver")
	r.eventBus.Unsubscribe("addr:added", r.handleAddrAdd)
}

// Start background processes
func (r *agentResolver) start() {
	r.eventBus.SubscribeAsync("addr:added", r.handleAddrAdd, false)
	r.updateClientConnState()
}

// Callback method for addr:added event
func (r *agentResolver) handleAddrAdd(nodeName string, addr resolver.Address) {
	if nodeName != r.nodeName {
		return
	}
	r.addAddr(addr)
	r.updateClientConnState()
}

// Thread-safe function to add address
func (r *agentResolver) addAddr(addr resolver.Address) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.addr = addr
}

// Thread-safe function to update clientconn state
func (r *agentResolver) updateClientConnState() {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := resolver.State{Addresses: []resolver.Address{r.addr}}
	if err := r.cc.UpdateState(state); err != nil && err != balancer.ErrBadResolverState {
		zlog.Error().Err(err).Msg("resolver encountered error while updating clientconn state")
	}
}

// Create new agent resolver instance
func NewAgentResolver(target resolver.Target, cc resolver.ClientConn, addr resolver.Address, eventBus eventbus.Bus) *agentResolver {
	r := &agentResolver{cc: cc, nodeName: target.Endpoint(), addr: addr, eventBus: eventBus}
	r.start()
	return r
}

// Agent Resolver Builder
type agentResolverBuilder struct {
	mu        sync.Mutex
	addrMap   map[string]resolver.Address
	clientset kubernetes.Interface
	namespace string
	isRunning bool
	stopCh    chan struct{}
	eventBus  eventbus.Bus
}

func (b *agentResolverBuilder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	addr, exists := b.addrMap[target.Endpoint()]
	if !exists {
		return nil, fmt.Errorf("address not found for node: %s", target.Endpoint())
	}

	// init resolver with subset from addresses
	return NewAgentResolver(target, cc, addr, b.eventBus), nil
}

func (*agentResolverBuilder) Scheme() string {
	return "kubetail-agent"
}

// Start background process to update pod addresses
func (b *agentResolverBuilder) Start(ctx context.Context) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// check if running
	if b.isRunning {
		return
	}

	// init listwatch
	watchlist := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = agentLabelSelectorString
			return b.clientset.CoreV1().Pods(b.namespace).List(context.Background(), options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = agentLabelSelectorString
			return b.clientset.CoreV1().Pods(b.namespace).Watch(context.Background(), options)
		},
	}

	// init informer with 5 minute resync period
	informer := cache.NewSharedInformer(
		watchlist,
		&corev1.Pod{},
		5*time.Minute,
	)

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			defer testEventBus.Publish("informer:added")
			b.handlePodAddOrUpdate(obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			defer testEventBus.Publish("informer:updated")
			b.handlePodAddOrUpdate(newObj)
		},
	},
	)

	b.stopCh = make(chan struct{})

	// run watcher in a go routine
	go informer.Run(b.stopCh)

	// set flag
	b.isRunning = true

	// handle context stopping in a goroutine
	go func() {
		<-ctx.Done()

		b.mu.Lock()
		defer b.mu.Unlock()

		close(b.stopCh)
		b.isRunning = false
	}()
}

// Teardown
func (b *agentResolverBuilder) Teardown() {
	b.mu.Lock()
	defer b.mu.Unlock()

	// check flag
	if !b.isRunning {
		return
	}

	// stop informer
	close(b.stopCh)

	// set flag
	b.isRunning = false
}

// Pod add-or-update handler
func (b *agentResolverBuilder) handlePodAddOrUpdate(obj interface{}) {
	b.mu.Lock()
	defer b.mu.Unlock()

	pod := obj.(*corev1.Pod)

	addr, exists := b.addrMap[pod.Spec.NodeName]
	if exists && addr.Attributes.Value("podName").(string) != pod.Name {
		exists = false
	}

	if !exists && isPodRunning(pod) {
		attrs := attributes.
			New("nodeName", pod.Spec.NodeName).
			WithValue("podName", pod.Name)

		// init address
		addr := resolver.Address{
			Addr:       fmt.Sprintf("%s:50051", pod.Status.PodIP),
			Attributes: attrs,
		}

		// add to map
		b.addrMap[pod.Spec.NodeName] = addr

		// publish event
		b.eventBus.Publish("addr:added", pod.Spec.NodeName, addr)
		b.eventBus.WaitAsync()

		zlog.Debug().Caller().Msgf("addr added: %s (%s)", pod.Name, pod.Status.PodIP)
	}
}

// Create new AgentResolverBuilder instance
func NewAgentResolverBuilder() (*agentResolverBuilder, error) {
	// config k8s
	// TODO: should connection manager support out-of-cluster config?
	k8scfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(k8scfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	// get current namespace from file system
	nsPathname := "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	nsBytes, err := os.ReadFile(nsPathname)
	if err != nil {
		return nil, fmt.Errorf("unable to read current namespace from %s: %v", nsPathname, err)
	}

	return &agentResolverBuilder{
		addrMap:   make(map[string]resolver.Address),
		clientset: clientset,
		namespace: string(nsBytes),
		eventBus:  eventbus.New(),
	}, nil
}

// Check if pod is running
func isPodRunning(pod *corev1.Pod) bool {
	if pod.ObjectMeta.DeletionTimestamp != nil {
		// terminating
		return false
	}
	return pod.Status.Phase == corev1.PodRunning
}
