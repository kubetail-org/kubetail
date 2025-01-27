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

	"github.com/kubetail-org/kubetail/modules/shared/config"

	"github.com/kubetail-org/kubetail/modules/dashboard/internal/k8shelpers"
)

// Represents HealthStatus enum
type HealthStatus string

const (
	HealthStatusSuccess  = "SUCCESS"
	HealthStatusFailure  = "FAILURE"
	HealthStatusNotFound = "NOTFOUND"
	HealthStatusUknown   = "UNKNOWN"
)

// Represents HealthMonitor
type HealthMonitor interface {
	Shutdown()
	GetHealthStatus(ctx context.Context, kubeContextPtr *string, namespacePtr *string, serviceNamePtr *string) (HealthStatus, error)
	WatchHealthStatus(ctx context.Context, kubeContextPtr *string, namespacePtr *string, serviceNamePtr *string) (<-chan HealthStatus, error)
	ReadyWait(ctx context.Context, kubeContextPtr *string, namespacePtr *string, serviceNamePtr *string) error
}

// Create new HealthMonitor instance
func NewHealthMonitor(cfg *config.Config, cm k8shelpers.ConnectionManager) HealthMonitor {
	switch cfg.Dashboard.Environment {
	case config.EnvironmentDesktop:
		return NewDesktopHealthMonitor(cm)
	case config.EnvironmentCluster:
		return NewInClusterHealthMonitor(cm, cfg.Dashboard.ClusterAPIEndpoint)
	default:
		panic("not implemented")
	}
}

// Represents DesktopHealthMonitor
type DesktopHealthMonitor struct {
	cm          k8shelpers.ConnectionManager
	workerCache map[string]*endpointSlicesHealthMonitorWorker
	mu          sync.Mutex
}

// Create new DesktopHealthMonitor instance
func NewDesktopHealthMonitor(cm k8shelpers.ConnectionManager) *DesktopHealthMonitor {
	return &DesktopHealthMonitor{
		cm:          cm,
		workerCache: make(map[string]*endpointSlicesHealthMonitorWorker),
	}
}

// Shutdown all managed monitors
func (hm *DesktopHealthMonitor) Shutdown() {
	var wg sync.WaitGroup
	for _, worker := range hm.workerCache {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker.Shutdown()
		}()
	}
	wg.Wait()
}

// GetHealthStatus
func (hm *DesktopHealthMonitor) GetHealthStatus(ctx context.Context, kubeContextPtr *string, namespacePtr *string, serviceNamePtr *string) (HealthStatus, error) {
	worker, err := hm.getOrCreateWorker(ctx, kubeContextPtr, namespacePtr, serviceNamePtr)
	if err != nil {
		return HealthStatusUknown, err
	}
	return worker.GetHealthStatus(), nil
}

// WatchHealthStatus
func (hm *DesktopHealthMonitor) WatchHealthStatus(ctx context.Context, kubeContextPtr *string, namespacePtr *string, serviceNamePtr *string) (<-chan HealthStatus, error) {
	worker, err := hm.getOrCreateWorker(ctx, kubeContextPtr, namespacePtr, serviceNamePtr)
	if err != nil {
		return nil, err
	}
	return worker.WatchHealthStatus(ctx)
}

// ReadyWait
func (hm *DesktopHealthMonitor) ReadyWait(ctx context.Context, kubeContextPtr *string, namespacePtr *string, serviceNamePtr *string) error {
	worker, err := hm.getOrCreateWorker(ctx, kubeContextPtr, namespacePtr, serviceNamePtr)
	if err != nil {
		return err
	}
	return worker.ReadyWait(ctx)
}

// getOrCreateWorker
func (hm *DesktopHealthMonitor) getOrCreateWorker(ctx context.Context, kubeContextPtr *string, namespacePtr *string, serviceNamePtr *string) (*endpointSlicesHealthMonitorWorker, error) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	kubeContext := hm.cm.DerefKubeContext(kubeContextPtr)
	namespace := ptr.Deref(namespacePtr, DefaultNamespace)
	serviceName := ptr.Deref(serviceNamePtr, DefaultServiceName)

	// Constuct cache key
	k := fmt.Sprintf("%s::%s::%s", kubeContext, namespace, serviceName)

	// Check cache
	worker, exists := hm.workerCache[k]
	if !exists {
		// Get clientset
		clientset, err := hm.cm.GetOrCreateClientset(ptr.To(kubeContext))
		if err != nil {
			return nil, err
		}

		// Initialize worker
		worker, err = newEndpointSlicesHealthMonitorWorker(clientset, namespace, serviceName)
		if err != nil {
			return nil, err
		}

		// Add to cache
		hm.workerCache[k] = worker

		// Start background processes and wait for cache to sync
		if err := worker.Start(ctx); err != nil {
			return nil, err
		}
	}

	return worker, nil
}

// Respresents InClusterHealthMonitor
type InClusterHealthMonitor struct {
	cm                 k8shelpers.ConnectionManager
	clusterAPIEndpoint string
	worker             healthMonitorWorker
	mu                 sync.Mutex
}

// Create new InClusterHealthMonitor instance
func NewInClusterHealthMonitor(cm k8shelpers.ConnectionManager, clusterAPIEndpoint string) *InClusterHealthMonitor {
	hm := &InClusterHealthMonitor{
		cm:                 cm,
		clusterAPIEndpoint: clusterAPIEndpoint,
	}

	if clusterAPIEndpoint == "" {
		// Initialize NoopHealthMonitor and return
		hm.worker = newNoopHealthMonitorWorker()
	}

	return hm
}

// Shutdown all managed monitors
func (hm *InClusterHealthMonitor) Shutdown() {
	if hm.worker != nil {
		hm.worker.Shutdown()
	}
}

// GetHealthStatus
func (hm *InClusterHealthMonitor) GetHealthStatus(ctx context.Context, kubeContextPtr *string, namespacePtr *string, serviceNamePtr *string) (HealthStatus, error) {
	worker, err := hm.getOrCreateWorker(ctx)
	if err != nil {
		return HealthStatusUknown, err
	}
	return worker.GetHealthStatus(), nil
}

// WatchHealthStatus
func (hm *InClusterHealthMonitor) WatchHealthStatus(ctx context.Context, kubeContextPtr *string, namespacePtr *string, serviceNamePtr *string) (<-chan HealthStatus, error) {
	worker, err := hm.getOrCreateWorker(ctx)
	if err != nil {
		return nil, err
	}
	return worker.WatchHealthStatus(ctx)
}

// ReadyWait
func (hm *InClusterHealthMonitor) ReadyWait(ctx context.Context, kubeContextPtr *string, namespacePtr *string, serviceNamePtr *string) error {
	worker, err := hm.getOrCreateWorker(ctx)
	if err != nil {
		return err
	}
	return worker.ReadyWait(ctx)
}

// getOrCreateWorker
func (hm *InClusterHealthMonitor) getOrCreateWorker(ctx context.Context) (healthMonitorWorker, error) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	// Check cache
	if hm.worker == nil {
		// Parse endpoint url
		connectArgs, err := parseConnectUrl(hm.clusterAPIEndpoint)
		if err != nil {
			return nil, err
		}

		// Get clientset
		clientset, err := hm.cm.GetOrCreateClientset(nil)
		if err != nil {
			return nil, err
		}

		// Initialize EndpointSlicesHealthMonitor
		worker, err := newEndpointSlicesHealthMonitorWorker(clientset, connectArgs.Namespace, connectArgs.ServiceName)
		if err != nil {
			return nil, err
		}

		// Start background processes
		if err := worker.Start(ctx); err != nil {
			return nil, err
		}

		// Cache worker
		hm.worker = worker
	}

	return hm.worker, nil
}

// Represents healthMonitorWorker
type healthMonitorWorker interface {
	Start(ctx context.Context) error
	Shutdown()
	GetHealthStatus() HealthStatus
	WatchHealthStatus(ctx context.Context) (<-chan HealthStatus, error)
	ReadyWait(ctx context.Context) error
}

// Represents endpointSlicesHealthMonitorWorker
type endpointSlicesHealthMonitorWorker struct {
	lastStatus HealthStatus
	factory    informers.SharedInformerFactory
	informer   cache.SharedIndexInformer
	eventbus   evbus.Bus
	shutdownCh chan struct{}
	mu         sync.RWMutex
}

// Create new endpointSlicesHealthMonitorWorker instance
func newEndpointSlicesHealthMonitorWorker(clientset kubernetes.Interface, namespace string, serviceName string) (*endpointSlicesHealthMonitorWorker, error) {
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
	w := &endpointSlicesHealthMonitorWorker{
		lastStatus: HealthStatusUknown,
		factory:    factory,
		informer:   informer,
		eventbus:   evbus.New(),
		shutdownCh: make(chan struct{}),
	}

	// Register event handlers
	_, err := informer.AddEventHandlerWithResyncPeriod(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { w.onInformerEvent() },
		UpdateFunc: func(oldObj, newObj interface{}) { w.onInformerEvent() },
		DeleteFunc: func(obj interface{}) { w.onInformerEvent() },
	}, 10*time.Minute)
	if err != nil {
		return nil, err
	}

	return w, nil
}

// Start
func (w *endpointSlicesHealthMonitorWorker) Start(ctx context.Context) error {
	// Start background processes
	w.factory.Start(w.shutdownCh)

	// Wait for cache to sync
	if !cache.WaitForCacheSync(ctx.Done(), w.informer.HasSynced) {
		return fmt.Errorf("failed to sync")
	}

	// Exit if context canceled
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Initialize status
	w.onInformerEvent()

	return nil
}

// Shutdown
func (w *endpointSlicesHealthMonitorWorker) Shutdown() {
	close(w.shutdownCh)
	w.factory.Shutdown()
}

// GetHealthStatus
func (w *endpointSlicesHealthMonitorWorker) GetHealthStatus() HealthStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.lastStatus
}

// WatchHealthStatus
func (w *endpointSlicesHealthMonitorWorker) WatchHealthStatus(ctx context.Context) (<-chan HealthStatus, error) {
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
	err := w.eventbus.SubscribeAsync("UPDATE", sendStatus, true)
	if err != nil {
		return nil, err
	}

	go func() {
		// send initial state
		sendStatus(w.GetHealthStatus())

		// Wait for client to close
		<-ctx.Done()

		// Unsubscribe and close output channel
		err := w.eventbus.Unsubscribe("UPDATE", sendStatus)
		if err != nil {
			zlog.Error().Err(err).Caller().Send()
		}

		close(outCh)
	}()

	return outCh, nil
}

// ReadyWait
func (w *endpointSlicesHealthMonitorWorker) ReadyWait(ctx context.Context) error {
	if w.GetHealthStatus() == HealthStatusSuccess {
		return nil
	}

	// Otherwise, watch for updates until success or context canceled
	ch, err := w.WatchHealthStatus(ctx)
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
func (w *endpointSlicesHealthMonitorWorker) onInformerEvent() {
	list := w.informer.GetStore().List()

	// Return NotFound if no endpoint slices exist
	if len(list) == 0 {
		w.updateHealthStatus(HealthStatusNotFound)
		return
	}

	// Return Healthy if at least one EndpointSlice is in Ready state
	for _, obj := range list {
		es := obj.(*discoveryv1.EndpointSlice)
		for _, endpoint := range es.Endpoints {
			if ptr.Deref(endpoint.Conditions.Ready, false) {
				w.updateHealthStatus(HealthStatusSuccess)
				return
			}
		}
	}

	w.updateHealthStatus(HealthStatusFailure)
}

// updateHealthStatus
func (w *endpointSlicesHealthMonitorWorker) updateHealthStatus(newStatus HealthStatus) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if newStatus != w.lastStatus {
		w.lastStatus = newStatus
		w.eventbus.Publish("UPDATE", newStatus)
	}
}

// Represents noopHealthMonitorWorker
type noopHealthMonitorWorker struct{}

// Create new noopHealthMonitorWorker instance
func newNoopHealthMonitorWorker() *noopHealthMonitorWorker {
	return &noopHealthMonitorWorker{}
}

// Start
func (*noopHealthMonitorWorker) Start(ctx context.Context) error {
	return nil
}

// Shutdown
func (*noopHealthMonitorWorker) Shutdown() {
	// Do nothing
}

// GetHealthStatus
func (*noopHealthMonitorWorker) GetHealthStatus() HealthStatus {
	return HealthStatusUknown
}

// WatchHealthStatus
func (*noopHealthMonitorWorker) WatchHealthStatus(ctx context.Context) (<-chan HealthStatus, error) {
	return nil, fmt.Errorf("not configured")
}

// ReadyWait
func (*noopHealthMonitorWorker) ReadyWait(ctx context.Context) error {
	return fmt.Errorf("not configured")
}
