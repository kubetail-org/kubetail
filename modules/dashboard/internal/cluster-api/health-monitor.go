// Copyright 2024-2025 Andres Morey
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

package clusterapi

import (
	"context"
	"fmt"
	"sync"
	"time"

	evbus "github.com/asaskevich/EventBus"
	zlog "github.com/rs/zerolog/log"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/ptr"
)

// Represents HealthStatus enum
type HealthStatus string

const (
	HealthStatusSuccess  = "SUCCESS"
	HealthStatusFailure  = "FAILURE"
	HealthStatusNotFound = "NOTFOUND"
	HealthStatusUknown   = "UNKNOWN"
)

type HealthMonitor interface {
	Start(ctx context.Context) error
	Shutdown()
	GetHealthStatus() HealthStatus
	WatchHealthStatus(ctx context.Context) (<-chan HealthStatus, error)
	ReadyWait(ctx context.Context) error
}

// Represents EndpointSlicesHealthMonitor
type EndpointSlicesHealthMonitor struct {
	lastStatus HealthStatus
	factory    informers.SharedInformerFactory
	informer   cache.SharedIndexInformer
	eventbus   evbus.Bus
	shutdownCh chan struct{}
	mu         sync.RWMutex
}

// Create new EndpointSlicesHealthMonitor instance
func NewEndpointSlicesHealthMonitor(ctx context.Context, clientset kubernetes.Interface, namespace string, serviceName string) (*EndpointSlicesHealthMonitor, error) {
	// Init factory
	labelSelector := labels.Set{
		discoveryv1.LabelServiceName: serviceName,
	}.String()

	factory := informers.NewFilteredSharedInformerFactory(clientset, 10*time.Minute, namespace, func(options *metav1.ListOptions) {
		options.LabelSelector = labelSelector
	})

	// Init informer
	informer := factory.Discovery().V1().EndpointSlices().Informer()

	// Initialize instance
	hm := &EndpointSlicesHealthMonitor{
		lastStatus: HealthStatusUknown,
		factory:    factory,
		informer:   informer,
		eventbus:   evbus.New(),
		shutdownCh: make(chan struct{}),
	}

	// Register event handlers
	_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { hm.onInformerEvent() },
		UpdateFunc: func(oldObj, newObj interface{}) { hm.onInformerEvent() },
		DeleteFunc: func(obj interface{}) { hm.onInformerEvent() },
	})
	if err != nil {
		return nil, err
	}

	return hm, nil
}

// Start
func (hm *EndpointSlicesHealthMonitor) Start(ctx context.Context) error {
	// Start background processes
	hm.factory.Start(hm.shutdownCh)

	// Wait for cache to sync
	if !cache.WaitForCacheSync(ctx.Done(), hm.informer.HasSynced) {
		return fmt.Errorf("failed to sync")
	}

	// Exit if context canceled
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Initialize status
	hm.onInformerEvent()

	return nil
}

// Shutdown
func (hm *EndpointSlicesHealthMonitor) Shutdown() {
	close(hm.shutdownCh)
	hm.factory.Shutdown()
}

// GetHealthStatus
func (hm *EndpointSlicesHealthMonitor) GetHealthStatus() HealthStatus {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return hm.lastStatus
}

// WatchHealthStatus
func (hm *EndpointSlicesHealthMonitor) WatchHealthStatus(ctx context.Context) (<-chan HealthStatus, error) {
	outCh := make(chan HealthStatus)

	var mu sync.Mutex
	var lastStatus *HealthStatus

	sendStatus := func(newStatus HealthStatus) {
		mu.Lock()
		defer mu.Unlock()
		if ctx.Err() == nil && (lastStatus == nil || *lastStatus != newStatus) {
			lastStatus = &newStatus
			outCh <- newStatus
		}
	}

	// Subscribe to updates
	err := hm.eventbus.SubscribeAsync("UPDATE", sendStatus, true)
	if err != nil {
		return nil, err
	}

	go func() {
		// send initial state
		sendStatus(hm.GetHealthStatus())

		// Wait for client to close
		<-ctx.Done()

		// Unsubscribe and close output channel
		err := hm.eventbus.Unsubscribe("UPDATE", sendStatus)
		if err != nil {
			zlog.Error().Err(err).Caller().Send()
		}

		close(outCh)
	}()

	return outCh, nil
}

// ReadyWait
func (hm *EndpointSlicesHealthMonitor) ReadyWait(ctx context.Context) error {
	if hm.GetHealthStatus() == HealthStatusSuccess {
		return nil
	}

	// Otherwise, watch for updates until success or context canceled
	ch, err := hm.WatchHealthStatus(ctx)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case status := <-ch:
			if status == HealthStatusSuccess {
				return nil
			}
		}
	}
}

// onInformerEvent
func (hm *EndpointSlicesHealthMonitor) onInformerEvent() {
	list := hm.informer.GetStore().List()

	// Return NotFound if no endpoint slices exist
	if len(list) == 0 {
		hm.updateHealthStatus(HealthStatusNotFound)
		return
	}

	// Return Healthy if at least one EndpointSlice is in Ready state
	for _, obj := range list {
		es := obj.(*discoveryv1.EndpointSlice)
		for _, endpoint := range es.Endpoints {
			if ptr.Deref(endpoint.Conditions.Ready, false) {
				hm.updateHealthStatus(HealthStatusSuccess)
				return
			}
		}
	}

	hm.updateHealthStatus(HealthStatusFailure)
}

// updateHealthStatus
func (hm *EndpointSlicesHealthMonitor) updateHealthStatus(newStatus HealthStatus) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	if newStatus != hm.lastStatus {
		hm.lastStatus = newStatus
		hm.eventbus.Publish("UPDATE", newStatus)
	}
}
