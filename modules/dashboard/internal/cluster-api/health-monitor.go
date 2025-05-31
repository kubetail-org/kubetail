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
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
)

// Represents HealthStatus enum
type HealthStatus string

const (
	HealthStatusSuccess  = "SUCCESS"
	HealthStatusFailure  = "FAILURE"
	HealthStatusPending  = "PENDING"
	HealthStatusNotFound = "NOTFOUND"
	HealthStatusUknown   = "UNKNOWN"
)

// Represents HealthMonitor
type HealthMonitor interface {
	Shutdown()
	GetHealthStatus(ctx context.Context, kubeContext string, namespacePtr *string, serviceNamePtr *string) (HealthStatus, error)
	WatchHealthStatus(ctx context.Context, kubeContext string, namespacePtr *string, serviceNamePtr *string) (<-chan HealthStatus, error)
	ReadyWait(ctx context.Context, kubeContext string, namespacePtr *string, serviceNamePtr *string) error
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
	workerCache sync.Map
	contextMu   map[string]*sync.Mutex
	mu          sync.Mutex
}

// Create new DesktopHealthMonitor instance
func NewDesktopHealthMonitor(cm k8shelpers.ConnectionManager) *DesktopHealthMonitor {
	return &DesktopHealthMonitor{
		cm:          cm,
		workerCache: sync.Map{},
		contextMu:   make(map[string]*sync.Mutex),
	}
}

// Shutdown all managed monitors
func (hm *DesktopHealthMonitor) Shutdown() {
	var wg sync.WaitGroup
	hm.workerCache.Range(func(key, value interface{}) bool {
		wg.Add(1)
		go func(worker healthMonitorWorker) {
			defer wg.Done()
			worker.Shutdown()
		}(value.(healthMonitorWorker))
		return true
	})
	wg.Wait()
}

// GetHealthStatus
func (hm *DesktopHealthMonitor) GetHealthStatus(ctx context.Context, kubeContext string, namespacePtr *string, serviceNamePtr *string) (HealthStatus, error) {
	worker, err := hm.getOrCreateWorker(ctx, kubeContext, namespacePtr, serviceNamePtr)
	if err != nil {
		return HealthStatusUknown, err
	}
	return worker.GetHealthStatus(), nil
}

// WatchHealthStatus
func (hm *DesktopHealthMonitor) WatchHealthStatus(ctx context.Context, kubeContext string, namespacePtr *string, serviceNamePtr *string) (<-chan HealthStatus, error) {
	worker, err := hm.getOrCreateWorker(ctx, kubeContext, namespacePtr, serviceNamePtr)
	if err != nil {
		return nil, err
	}
	return worker.WatchHealthStatus(ctx)
}

// ReadyWait
func (hm *DesktopHealthMonitor) ReadyWait(ctx context.Context, kubeContext string, namespacePtr *string, serviceNamePtr *string) error {
	worker, err := hm.getOrCreateWorker(ctx, kubeContext, namespacePtr, serviceNamePtr)
	if err != nil {
		return err
	}
	return worker.ReadyWait(ctx)
}

// getOrCreateWorker
func (hm *DesktopHealthMonitor) getOrCreateWorker(ctx context.Context, kubeContext string, namespacePtr *string, serviceNamePtr *string) (healthMonitorWorker, error) {
	// Get or create mutex for this kubeContext
	hm.mu.Lock()
	contextMutex, exists := hm.contextMu[kubeContext]
	if !exists {
		contextMutex = &sync.Mutex{}
		hm.contextMu[kubeContext] = contextMutex
	}
	hm.mu.Unlock()

	// Lock the context-specific mutex for worker creation
	contextMutex.Lock()
	defer contextMutex.Unlock()

	namespace := ptr.Deref(namespacePtr, DefaultNamespace)
	serviceName := ptr.Deref(serviceNamePtr, DefaultServiceName)

	// Constuct cache key
	k := fmt.Sprintf("%s::%s::%s", kubeContext, namespace, serviceName)

	// Check cache
	var worker healthMonitorWorker
	value, ok := hm.workerCache.Load(k)
	if ok {
		worker = value.(healthMonitorWorker)
	} else {
		// Get clientset
		clientset, err := hm.cm.GetOrCreateClientset(kubeContext)
		if err != nil {
			return nil, err
		}

		// Check if the Kubernetes API supports EndpointSlices
		resources, err := clientset.Discovery().ServerResourcesForGroupVersion("discovery.k8s.io/v1")
		if err != nil || resources == nil {
			// EndpointSlices not supported, initialize NoopHealthMonitor
			worker = newNoopHealthMonitorWorker()
		} else {
			// EndpointSlices supported, initialize EndpointSlicesHealthMonitor
			worker, err = newEndpointSlicesHealthMonitorWorker(clientset, namespace, serviceName)
			if err != nil {
				return nil, err
			}
		}

		// Start background processes and wait for cache to sync
		if err := worker.Start(ctx); err != nil {
			return nil, err
		}

		// Add to cache
		hm.workerCache.Store(k, worker)
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
func (hm *InClusterHealthMonitor) GetHealthStatus(ctx context.Context, _kubeContext string, namespacePtr *string, serviceNamePtr *string) (HealthStatus, error) {
	worker, err := hm.getOrCreateWorker(ctx)
	if err != nil {
		return HealthStatusUknown, err
	}
	return worker.GetHealthStatus(), nil
}

// WatchHealthStatus
func (hm *InClusterHealthMonitor) WatchHealthStatus(ctx context.Context, _kubeContext string, namespacePtr *string, serviceNamePtr *string) (<-chan HealthStatus, error) {
	worker, err := hm.getOrCreateWorker(ctx)
	if err != nil {
		return nil, err
	}
	return worker.WatchHealthStatus(ctx)
}

// ReadyWait
func (hm *InClusterHealthMonitor) ReadyWait(ctx context.Context, _kubeContext string, namespacePtr *string, serviceNamePtr *string) error {
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
		clientset, err := hm.cm.GetOrCreateClientset("")
		if err != nil {
			return nil, err
		}

		var worker healthMonitorWorker

		// Check if the Kubernetes API supports EndpointSlices
		resources, err := clientset.Discovery().ServerResourcesForGroupVersion("discovery.k8s.io/v1")
		if err != nil || resources == nil {
			// EndpointSlices not supported, initialize NoopHealthMonitor
			worker = newNoopHealthMonitorWorker()
		} else {
			// EndpointSlices supported, initialize EndpointSlicesHealthMonitor
			worker, err = newEndpointSlicesHealthMonitorWorker(clientset, connectArgs.Namespace, connectArgs.ServiceName)
			if err != nil {
				return nil, err
			}

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
	esCache    map[string]*discoveryv1.EndpointSlice
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
		esCache:    make(map[string]*discoveryv1.EndpointSlice),
		eventbus:   evbus.New(),
		shutdownCh: make(chan struct{}),
	}

	// Register event handlers
	_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    w.onInformerAdd,
		UpdateFunc: w.onInformerUpdate,
		DeleteFunc: w.onInformerDelete,
	})
	if err != nil {
		return nil, err
	}

	return w, nil
}

// Start
func (w *endpointSlicesHealthMonitorWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

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

	// Init cache
	for _, obj := range w.informer.GetStore().List() {
		es := obj.(*discoveryv1.EndpointSlice)
		w.esCache[string(es.UID)] = es
	}

	// Init status
	w.updateHealthStatus_UNSAFE()

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

// onInformerAdd
func (w *endpointSlicesHealthMonitorWorker) onInformerAdd(obj interface{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	es := obj.(*discoveryv1.EndpointSlice)
	w.esCache[string(es.UID)] = es
	w.updateHealthStatus_UNSAFE()
}

// onInformerUpdate
func (w *endpointSlicesHealthMonitorWorker) onInformerUpdate(oldObj, newObj interface{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	es := newObj.(*discoveryv1.EndpointSlice)
	w.esCache[string(es.UID)] = es
	w.updateHealthStatus_UNSAFE()
}

// onInformerDelete
func (w *endpointSlicesHealthMonitorWorker) onInformerDelete(obj interface{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	es := obj.(*discoveryv1.EndpointSlice)
	delete(w.esCache, string(es.UID))
	w.updateHealthStatus_UNSAFE()
}

func (w *endpointSlicesHealthMonitorWorker) getHealthStatus_UNSAFE() HealthStatus {
	if len(w.esCache) == 0 {
		return HealthStatusNotFound
	}

	for _, es := range w.esCache {
		for _, endpoint := range es.Endpoints {
			if ptr.Deref(endpoint.Conditions.Ready, false) {
				return HealthStatusSuccess
			}
		}
	}

	return HealthStatusFailure
}

func (w *endpointSlicesHealthMonitorWorker) updateHealthStatus_UNSAFE() {
	newStatus := w.getHealthStatus_UNSAFE()

	// Handle "pending"
	if newStatus == HealthStatusFailure && (w.lastStatus == HealthStatusNotFound || w.lastStatus == HealthStatusPending) {
		newStatus = HealthStatusPending
	}

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
	return nil, fmt.Errorf("not available")
}

// ReadyWait
func (*noopHealthMonitorWorker) ReadyWait(ctx context.Context) error {
	return fmt.Errorf("not available")
}
