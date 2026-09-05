// Copyright 2024 The Kubetail Authors
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

	aggregatorclientset "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset"

	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/config"
	sharedcfg "github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
	"github.com/kubetail-org/kubetail/modules/shared/util"
)

// Represents HealthStatus enum
type HealthStatus string

const (
	HealthStatusSuccess  HealthStatus = "SUCCESS"
	HealthStatusFailure  HealthStatus = "FAILURE"
	HealthStatusPending  HealthStatus = "PENDING"
	HealthStatusNotFound HealthStatus = "NOTFOUND"
	HealthStatusUknown   HealthStatus = "UNKNOWN"
)

// Represents HealthMonitor
type HealthMonitor interface {
	Shutdown()
	GetHealthStatus(ctx context.Context, kubeContext string) (HealthStatus, error)
	WatchHealthStatus(ctx context.Context, kubeContext string) (<-chan HealthStatus, error)
	ReadyWait(ctx context.Context, kubeContext string) error
}

// Create new HealthMonitor instance
func NewHealthMonitor(cfg *config.Config, cm k8shelpers.ConnectionManager) HealthMonitor {
	switch cfg.Environment {
	case sharedcfg.EnvironmentDesktop:
		return NewDesktopHealthMonitor(cm)
	case sharedcfg.EnvironmentCluster:
		return NewInClusterHealthMonitor(cm, cfg.ClusterAPIEnabled)
	default:
		panic("not implemented")
	}
}

// Represents DesktopHealthMonitor
type DesktopHealthMonitor struct {
	cm          k8shelpers.ConnectionManager
	workerCache util.SyncMap[string, healthMonitorWorker]
	contextMu   util.SyncMap[string, *sync.Mutex]
}

// Create new DesktopHealthMonitor instance
func NewDesktopHealthMonitor(cm k8shelpers.ConnectionManager) *DesktopHealthMonitor {
	return &DesktopHealthMonitor{
		cm:          cm,
		workerCache: util.SyncMap[string, healthMonitorWorker]{},
		contextMu:   util.SyncMap[string, *sync.Mutex]{},
	}
}

// Shutdown all managed monitors
func (hm *DesktopHealthMonitor) Shutdown() {
	var wg sync.WaitGroup
	hm.workerCache.Range(func(_ string, worker healthMonitorWorker) bool {
		wg.Go(func() {
			worker.Shutdown()
		})
		return true
	})
	wg.Wait()
}

// GetHealthStatus
func (hm *DesktopHealthMonitor) GetHealthStatus(ctx context.Context, kubeContext string) (HealthStatus, error) {
	worker, err := hm.getOrCreateWorker(ctx, kubeContext)
	if err != nil {
		return HealthStatusUknown, err
	}
	return worker.GetHealthStatus(), nil
}

// WatchHealthStatus
func (hm *DesktopHealthMonitor) WatchHealthStatus(ctx context.Context, kubeContext string) (<-chan HealthStatus, error) {
	worker, err := hm.getOrCreateWorker(ctx, kubeContext)
	if err != nil {
		return nil, err
	}
	return worker.WatchHealthStatus(ctx)
}

// ReadyWait
func (hm *DesktopHealthMonitor) ReadyWait(ctx context.Context, kubeContext string) error {
	worker, err := hm.getOrCreateWorker(ctx, kubeContext)
	if err != nil {
		return err
	}
	return worker.ReadyWait(ctx)
}

// getOrCreateWorker
func (hm *DesktopHealthMonitor) getOrCreateWorker(ctx context.Context, kubeContext string) (healthMonitorWorker, error) {
	// Get or create mutex for this kubeContext
	contextMutex, _ := hm.contextMu.LoadOrStore(kubeContext, &sync.Mutex{})

	// Lock the context-specific mutex for worker creation
	contextMutex.Lock()
	defer contextMutex.Unlock()

	// Check cache
	worker, ok := hm.workerCache.Load(kubeContext)
	if !ok {
		restConfig, err := hm.cm.GetOrCreateRestConfig(kubeContext)
		if err != nil {
			return nil, err
		}

		client, err := aggregatorclientset.NewForConfig(restConfig)
		if err != nil {
			return nil, err
		}

		worker, err = newAPIServiceHealthMonitorWorker(client)
		if err != nil {
			return nil, err
		}

		if err := worker.Start(ctx); err != nil {
			return nil, err
		}

		hm.workerCache.Store(kubeContext, worker)
	}

	return worker, nil
}

// Respresents InClusterHealthMonitor
type InClusterHealthMonitor struct {
	cm     k8shelpers.ConnectionManager
	worker healthMonitorWorker
	mu     sync.Mutex
}

// NewInClusterHealthMonitor returns an in-cluster health monitor. Pass
// enabled=false (e.g. when the cluster-api isn't deployed) to install a
// noop worker that always reports an Unknown status.
func NewInClusterHealthMonitor(cm k8shelpers.ConnectionManager, enabled bool) *InClusterHealthMonitor {
	hm := &InClusterHealthMonitor{cm: cm}
	if !enabled {
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
func (hm *InClusterHealthMonitor) GetHealthStatus(ctx context.Context, _ string) (HealthStatus, error) {
	worker, err := hm.getOrCreateWorker(ctx)
	if err != nil {
		return HealthStatusUknown, err
	}
	return worker.GetHealthStatus(), nil
}

// WatchHealthStatus
func (hm *InClusterHealthMonitor) WatchHealthStatus(ctx context.Context, _ string) (<-chan HealthStatus, error) {
	worker, err := hm.getOrCreateWorker(ctx)
	if err != nil {
		return nil, err
	}
	return worker.WatchHealthStatus(ctx)
}

// ReadyWait
func (hm *InClusterHealthMonitor) ReadyWait(ctx context.Context, _ string) error {
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

	if hm.worker == nil {
		restConfig, err := hm.cm.GetOrCreateRestConfig("")
		if err != nil {
			return nil, err
		}

		client, err := aggregatorclientset.NewForConfig(restConfig)
		if err != nil {
			return nil, err
		}

		worker, err := newAPIServiceHealthMonitorWorker(client)
		if err != nil {
			return nil, err
		}

		if err := worker.Start(ctx); err != nil {
			return nil, err
		}

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
