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
	"sync"
	"time"

	eventbus "github.com/asaskevich/EventBus"
	zlog "github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

var testEventBus = eventbus.New()

type ConnectionManager struct {
	mu        sync.Mutex
	clientset kubernetes.Interface
	resolver  *agentResolverBuilder
	conns     map[string]ClientConnInterface
	stopCh    chan struct{}
	isRunning bool
}

// Get gRPC connection for a specific node
func (cm *ConnectionManager) Get(nodeName string) ClientConnInterface {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	return cm.conns[nodeName]
}

// Get all gRPC connections (one per node)
func (cm *ConnectionManager) GetAll() map[string]ClientConnInterface {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	return cm.conns
}

// Start background process to monitor kubetail-agent pods and
// initialize grpc connections to them
func (cm *ConnectionManager) Start(ctx context.Context) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// check if running
	if cm.isRunning {
		return
	}

	// start resolver
	cm.resolver.Start(ctx)

	// init listwatch
	watchlist := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return cm.clientset.CoreV1().Nodes().List(context.Background(), options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return cm.clientset.CoreV1().Nodes().Watch(context.Background(), options)
		},
	}

	// init informer with 10 minute resync period
	informer := cache.NewSharedInformer(
		watchlist,
		&corev1.Node{},
		10*time.Minute,
	)

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			zlog.Debug().Msgf("[grpc-connection-manager] node added: %v", obj)
			defer testEventBus.Publish("informer:added")
			cm.handleNodeAdd(obj)
		},
		DeleteFunc: func(obj interface{}) {
			zlog.Debug().Msgf("[grpc-connection-manager] node deleted: %v", obj)
			defer testEventBus.Publish("informer:deleted")
			cm.handleNodeDelete(obj)
		},
	},
	)

	cm.stopCh = make(chan struct{})

	// run watcher in a go routine
	go informer.Run(cm.stopCh)

	// ensure the informer has synced
	if !cache.WaitForCacheSync(cm.stopCh, informer.HasSynced) {
		panic("informer cache sync timeout")
	}

	// set flag
	cm.isRunning = true

	// handle context stopping in a goroutine
	go func() {
		<-ctx.Done()

		cm.mu.Lock()
		defer cm.mu.Unlock()

		close(cm.stopCh)
		cm.isRunning = false
	}()
}

// Teardown
func (cm *ConnectionManager) Teardown() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// check flag
	if !cm.isRunning {
		return
	}

	// stop resolver
	cm.resolver.Teardown()

	// stop informer
	close(cm.stopCh)

	// close grpc connections
	for _, conn := range cm.conns {
		if err := conn.Close(); err != nil {
			zlog.Error().Err(err).Msg("grpc clientconn close error")
		}
	}

	// reset map
	cm.conns = make(map[string]ClientConnInterface)

	// set flag
	cm.isRunning = false
}

// Handle a node add event
func (cm *ConnectionManager) handleNodeAdd(obj interface{}) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	node := obj.(*corev1.Node)

	// initialize connection
	conn, err := cm.newConn(node.Name)
	if err != nil {
		zlog.Error().Err(err).Send()
	}

	// add to map
	cm.conns[node.Name] = conn
}

// Handle a node delete event
func (cm *ConnectionManager) handleNodeDelete(obj interface{}) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	node := obj.(*corev1.Node)

	// close old connection if exists
	if oldConn, exists := cm.conns[node.Name]; exists {
		oldConn.Close()
	}

	// remove from map
	delete(cm.conns, node.Name)
}

// Initialize new gRPC connection
func (cm *ConnectionManager) newConn(nodeName string) (*grpc.ClientConn, error) {
	zlog.Debug().Msgf("initializing grpc clientconn for node %s", nodeName)
	return grpc.NewClient(
		fmt.Sprintf("kubetail-agent:///%s", nodeName),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(cm.resolver),
	)
}

// Create new ConnectionManager instance
func NewConnectionManager() (*ConnectionManager, error) {
	// config k8s
	// TODO: should connection manager support out-of-cluster config?
	k8scfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(k8scfg)
	if err != nil {
		return nil, err
	}

	// initialize agent resolver
	resolver, err := NewAgentResolverBuilder()
	if err != nil {
		return nil, err
	}

	// Convert the byte slice to a string and return
	return &ConnectionManager{
		conns:     make(map[string]ClientConnInterface),
		clientset: clientset,
		resolver:  resolver,
	}, nil
}
