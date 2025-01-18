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

	"github.com/kubetail-org/kubetail/modules/dashboard/internal/k8shelpers"
	"github.com/kubetail-org/kubetail/modules/shared/config"
	"k8s.io/utils/ptr"
)

// Represents HealthMonitorManager
type HealthMonitorManager interface {
	GetOrCreateMonitor(ctx context.Context, kubeContextPtr *string, namespacePtr *string, serviceNamePtr *string) (HealthMonitor, error)
	Shutdown()
}

// Create new HealthMonitorManager instance
func NewHealthMonitorManager(cfg *config.Config, cm k8shelpers.ConnectionManager) (HealthMonitorManager, error) {
	switch cfg.Dashboard.Environment {
	case config.EnvironmentDesktop:
		return NewDesktopHealthMonitorManager(cm), nil
	case config.EnvironmentCluster:
		return NewInClusterHealthMonitorManager(cm, cfg.Dashboard.ClusterAPIEndpoint)
	default:
		panic("not implemented")
	}
}

// Represents DesktopHealthMonitorManager
type DesktopHealthMonitorManager struct {
	cm           k8shelpers.ConnectionManager
	monitorCache map[string]HealthMonitor
	mu           sync.Mutex
}

// Create new DesktopHealthMonitorManager instance
func NewDesktopHealthMonitorManager(cm k8shelpers.ConnectionManager) *DesktopHealthMonitorManager {
	return &DesktopHealthMonitorManager{
		cm:           cm,
		monitorCache: make(map[string]HealthMonitor),
	}
}

// Shutdown all managed monitors
func (hmm *DesktopHealthMonitorManager) Shutdown() {
	var wg sync.WaitGroup
	for _, monitor := range hmm.monitorCache {
		wg.Add(1)
		go func() {
			defer wg.Done()
			monitor.Shutdown()
		}()
	}
	wg.Wait()
}

// GetOrCreateMonitor
func (hmm *DesktopHealthMonitorManager) GetOrCreateMonitor(ctx context.Context, kubeContextPtr *string, namespacePtr *string, serviceNamePtr *string) (HealthMonitor, error) {
	hmm.mu.Lock()
	defer hmm.mu.Unlock()

	kubeContext := hmm.cm.DerefKubeContext(kubeContextPtr)
	namespace := ptr.Deref(namespacePtr, DefaultNamespace)
	serviceName := ptr.Deref(serviceNamePtr, DefaultServiceName)

	// Constuct cache key
	k := fmt.Sprintf("%s::%s::%s", kubeContext, namespace, serviceName)

	// Check cache
	monitor, exists := hmm.monitorCache[k]
	if !exists {
		// Get clientset
		clientset, err := hmm.cm.GetOrCreateClientset(ptr.To(kubeContext))
		if err != nil {
			return nil, err
		}

		// Initialize health monitor
		monitor, err = NewEndpointSlicesHealthMonitor(clientset, namespace, serviceName)
		if err != nil {
			return nil, err
		}

		// Add to cache
		hmm.monitorCache[k] = monitor

		// Start background processes and wait for cache to sync
		if err := monitor.Start(ctx); err != nil {
			return nil, err
		}
	}

	return monitor, nil
}

// Represents InClusterHealthMonitorManager
type InClusterHealthMonitorManager struct {
	cm                 k8shelpers.ConnectionManager
	clusterAPIEndpoint string
	monitor            HealthMonitor
	mu                 sync.Mutex
}

// Create new InClusterHealthMonitorManager instance
func NewInClusterHealthMonitorManager(cm k8shelpers.ConnectionManager, clusterAPIEndpoint string) (*InClusterHealthMonitorManager, error) {
	hmm := &InClusterHealthMonitorManager{
		cm:                 cm,
		clusterAPIEndpoint: clusterAPIEndpoint,
	}

	if clusterAPIEndpoint == "" {
		// Initialize NoopHealthMonitor and return
		hmm.monitor = NewNoopHealthMonitor()
	}

	return hmm, nil
}

// Shutdown all managed monitors
func (hmm *InClusterHealthMonitorManager) Shutdown() {
	if hmm.monitor != nil {
		hmm.monitor.Shutdown()
	}
}

// GetOrCreateMonitor
func (hmm *InClusterHealthMonitorManager) GetOrCreateMonitor(ctx context.Context, kubeContextPtr *string, namespacePtr *string, serviceNamePtr *string) (HealthMonitor, error) {
	hmm.mu.Lock()
	defer hmm.mu.Unlock()

	// Check cache
	if hmm.monitor == nil {
		// Parse endpoint url
		connectArgs, err := parseConnectUrl(hmm.clusterAPIEndpoint)
		if err != nil {
			return nil, err
		}

		// Get clientset
		clientset, err := hmm.cm.GetOrCreateClientset(nil)
		if err != nil {
			return nil, err
		}

		// Initialize EndpointSlicesHealthMonitor
		monitor, err := NewEndpointSlicesHealthMonitor(clientset, connectArgs.Namespace, connectArgs.ServiceName)
		if err != nil {
			return nil, err
		}

		// Start background processes
		if err := monitor.Start(ctx); err != nil {
			return nil, err
		}

		// Cache monitor
		hmm.monitor = monitor
	}

	return hmm.monitor, nil
}
