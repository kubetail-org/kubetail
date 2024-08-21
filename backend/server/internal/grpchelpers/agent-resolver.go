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
	"fmt"
	"sync"

	eventbus "github.com/asaskevich/EventBus"
	zlog "github.com/rs/zerolog/log"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/resolver"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

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
func newAgentResolver(target resolver.Target, cc resolver.ClientConn, addr resolver.Address, eventBus eventbus.Bus) *agentResolver {
	r := &agentResolver{cc: cc, nodeName: target.Endpoint(), addr: addr, eventBus: eventBus}
	r.start()
	return r
}

// Agent Resolver Builder
type agentResolverBuilder struct {
	mu       sync.Mutex
	addrMap  map[string]resolver.Address
	eventBus eventbus.Bus
}

func (b *agentResolverBuilder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	addr, exists := b.addrMap[target.Endpoint()]
	if !exists {
		return nil, fmt.Errorf("address not found for node: %s", target.Endpoint())
	}

	// init resolver with subset from addresses
	return newAgentResolver(target, cc, addr, b.eventBus), nil
}

func (*agentResolverBuilder) Scheme() string {
	return "kubetail-agent"
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
func newAgentResolverBuilder(podInformer cache.SharedInformer) (*agentResolverBuilder, error) {
	rb := &agentResolverBuilder{
		addrMap:  make(map[string]resolver.Address),
		eventBus: eventbus.New(),
	}

	// add event handlers
	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			defer testEventBus.Publish("informer:added")
			rb.handlePodAddOrUpdate(obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			defer testEventBus.Publish("informer:updated")
			rb.handlePodAddOrUpdate(newObj)
		},
	},
	)

	return rb, nil
}
