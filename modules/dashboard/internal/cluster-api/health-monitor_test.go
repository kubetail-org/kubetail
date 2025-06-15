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
	"sync/atomic"
	"testing"
	"time"

	"github.com/kubetail-org/kubetail/modules/shared/config"
	k8shelpersmock "github.com/kubetail-org/kubetail/modules/shared/k8shelpers/mock"
	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

// MockHealthMonitorWorker is a mock implementation of the HealthMonitorWorker interface
type MockHealthMonitorWorker struct {
	shutdownTime time.Duration
	shutdown     atomic.Bool
	healthStatus HealthStatus

	statusMu       sync.RWMutex
	statusWatchers map[string]chan HealthStatus
	watcherId      atomic.Uint64
	done           chan struct{}
	statusTrigger  chan HealthStatus
}

func newMockHealthMonitorWorker(showdownTime time.Duration) *MockHealthMonitorWorker {
	return &MockHealthMonitorWorker{
		shutdown:       atomic.Bool{},
		shutdownTime:   showdownTime,
		healthStatus:   HealthStatusSuccess,
		statusWatchers: make(map[string]chan HealthStatus),
		done:           make(chan struct{}),
		statusTrigger:  make(chan HealthStatus, 10),
	}
}

func (m *MockHealthMonitorWorker) Start(ctx context.Context) error {
	return nil
}

func (m *MockHealthMonitorWorker) Shutdown() {
	m.shutdown.Store(true)
	close(m.done)
}

func (m *MockHealthMonitorWorker) GetHealthStatus() HealthStatus {
	return m.healthStatus
}

func (m *MockHealthMonitorWorker) WatchHealthStatus(ctx context.Context) (<-chan HealthStatus, error) {
	watcherId := fmt.Sprintf("watcher-%d", m.watcherId.Add(1))
	outCh := make(chan HealthStatus, 1)

	// Register watcher
	m.statusMu.Lock()
	m.statusWatchers[watcherId] = outCh
	currentStatus := m.healthStatus
	m.statusMu.Unlock()

	// Send status to the channel immediately
	select {
	case outCh <- currentStatus:
	default:
	}

	go m.handleWatcher(ctx, watcherId, outCh)

	return outCh, nil
}

func (m *MockHealthMonitorWorker) handleWatcher(ctx context.Context, watcherId string, outCh chan HealthStatus) {
	// Cleanup
	defer func() {
		m.statusMu.Lock()
		delete(m.statusWatchers, watcherId)
		m.statusMu.Unlock()
		close(outCh)
	}()

	//
	for {
		select {
		case <-ctx.Done():
			return

		case <-m.done:
			return

		case newStatus := <-m.statusTrigger:
			// Get the status from status trigger and Broadcast to the registered statusWatcher
			m.statusMu.RLock()
			if _, exist := m.statusWatchers[watcherId]; exist {
				select {
				case outCh <- newStatus:
				// if ctx is done
				case <-ctx.Done():
					m.statusMu.RUnlock()
					return
				}
			}
			m.statusMu.RUnlock()
		}
	}
}

func (m *MockHealthMonitorWorker) TriggerHealthStatus(newStatus HealthStatus) {
	m.statusMu.Lock()
	m.healthStatus = newStatus
	m.statusMu.Unlock()

	// Non-blocking send to trigger channel
	select {
	case m.statusTrigger <- newStatus:
	default:
	}
}

func (m *MockHealthMonitorWorker) ReadyWait(ctx context.Context) error {
	if m.GetHealthStatus() == HealthStatusSuccess {
		return nil
	}

	// Otherwise, watch for updates until success or context canceled
	ch, err := m.WatchHealthStatus(ctx)
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

func TestNewHealthMonitor(t *testing.T) {
	tests := []struct {
		name        string
		environment config.Environment
		endpoint    string
		want        interface{}
	}{
		{
			name:        "Desktop environment",
			environment: config.EnvironmentDesktop,
			endpoint:    "",
			want:        &DesktopHealthMonitor{},
		},
		{
			name:        "Cluster environment",
			environment: config.EnvironmentCluster,
			endpoint:    "",
			want:        &InClusterHealthMonitor{},
		},
		{
			name:        "Cluster environment with endpoint",
			environment: config.EnvironmentCluster,
			endpoint:    "kubetail-cluster-api.kubetail-system.svc.cluster.local:50051",
			want:        &InClusterHealthMonitor{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// create connection manager
			cm := &k8shelpersmock.MockConnectionManager{}

			// create config
			cfg := config.DefaultConfig()
			cfg.Dashboard.Environment = tt.environment
			if tt.endpoint != "" {
				cfg.Dashboard.ClusterAPIEndpoint = tt.endpoint
			}

			// create health monitor
			hm := NewHealthMonitor(cfg, cm)

			// assert health monitor is not nil
			assert.NotNil(t, hm)

			// assert health monitor is of the expected type
			assert.IsType(t, tt.want, hm)

		})
	}

}

func TestNewHealthMonitor_InvalidEnvironment(t *testing.T) {
	// create connection manager
	cm := &k8shelpersmock.MockConnectionManager{}

	// create config
	cfg := config.DefaultConfig()
	cfg.Dashboard.Environment = "invalid"

	// Assert panic
	assert.Panics(t, func() {
		NewHealthMonitor(cfg, cm)
	}, "NewHealthMonitor should panic if environment is invalid")
}

// Shutdown
func TestDesktopHealthMonitor_Shutdown(t *testing.T) {

	tests := []struct {
		name          string
		numberWorkers int
		showdownTime  time.Duration
	}{
		{
			name:          "Shutdown with 0 workers",
			numberWorkers: 0,
			showdownTime:  1 * time.Second,
		},
		{
			name:          "Shutdown with 1 worker",
			numberWorkers: 2,
			showdownTime:  1 * time.Second,
		},
		{
			name:          "Shutdown with multiple workers",
			numberWorkers: 2,
			showdownTime:  1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// create connection manager
			cm := &k8shelpersmock.MockConnectionManager{}

			// create health monitor
			hm := NewDesktopHealthMonitor(cm)

			var workers []*MockHealthMonitorWorker

			// create mock health monitor worker
			for i := 0; i < tt.numberWorkers; i++ {
				worker := newMockHealthMonitorWorker(tt.showdownTime)
				workers = append(workers, worker)
				hm.workerCache.Store(fmt.Sprintf("worker-%d", i), worker)
			}

			// shutdown health monitor
			hm.Shutdown()

			// assert worker is shutdown
			for _, worker := range workers {
				assert.True(t, worker.shutdown.Load())
			}
		})
	}
}

func TestInClusterHealthMonitor_Shutdown(t *testing.T) {

	tests := []struct {
		name         string
		endpoint     string
		hasWorker    bool
		showdownTime time.Duration
		want         interface{}
	}{
		{
			name:         "Shutdown with endpoint and 0 workers",
			endpoint:     "kubetail-cluster-api.kubetail-system.svc.cluster.local:50051",
			hasWorker:    false,
			showdownTime: 1 * time.Second,
			want:         &InClusterHealthMonitor{},
		},
		{
			name:         "Shutdown with endpoint and 1 worker",
			endpoint:     "kubetail-cluster-api.kubetail-system.svc.cluster.local:50051",
			hasWorker:    true,
			showdownTime: 1 * time.Second,
			want:         &InClusterHealthMonitor{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// create connection manager
			cm := &k8shelpersmock.MockConnectionManager{}

			// create health monitor
			hm := NewInClusterHealthMonitor(cm, tt.endpoint)
			var worker *MockHealthMonitorWorker

			// create mock health monitor worker
			if tt.hasWorker {
				worker = newMockHealthMonitorWorker(tt.showdownTime)
				hm.worker = worker
			}

			// assert health monitor is not nil
			assert.NotNil(t, hm)

			// assert health monitor is of the expected type
			assert.IsType(t, tt.want, hm)

			// shutdown health monitor
			hm.Shutdown()

			// assert worker is shutdown
			if tt.hasWorker {
				assert.True(t, worker.shutdown.Load())
			}

		})
	}
}
func TestDesktopHealthMonitor_GetHealthStatus(t *testing.T) {
	tests := []struct {
		name           string
		kubeContext    string
		namespace      *string
		serviceName    *string
		mockStatus     HealthStatus
		setupMockError bool
		expectedStatus HealthStatus
		expectError    bool
	}{
		{
			name:           "Successful status retrieval - SUCCESS",
			kubeContext:    "test-context",
			namespace:      ptr.To("test-namespace"),
			serviceName:    ptr.To("test-service"),
			mockStatus:     HealthStatusSuccess,
			setupMockError: false,
			expectedStatus: HealthStatusSuccess,
			expectError:    false,
		},
		{
			name:           "Successful status retrieval - FAILURE",
			kubeContext:    "test-context",
			namespace:      ptr.To("test-namespace"),
			serviceName:    ptr.To("test-service"),
			mockStatus:     HealthStatusFailure,
			setupMockError: false,
			expectedStatus: HealthStatusFailure,
			expectError:    false,
		},
		{
			name:           "Successful status retrieval - PENDING",
			kubeContext:    "test-context",
			namespace:      ptr.To("test-namespace"),
			serviceName:    ptr.To("test-service"),
			mockStatus:     HealthStatusPending,
			setupMockError: false,
			expectedStatus: HealthStatusPending,
			expectError:    false,
		},
		{
			name:           "Successful status retrieval - NOTFOUND",
			kubeContext:    "test-context",
			namespace:      ptr.To("test-namespace"),
			serviceName:    ptr.To("test-service"),
			mockStatus:     HealthStatusNotFound,
			setupMockError: false,
			expectedStatus: HealthStatusNotFound,
			expectError:    false,
		},
		{
			name:           "Worker creation error",
			kubeContext:    "invalid-context",
			namespace:      ptr.To("test-namespace"),
			serviceName:    ptr.To("test-service"),
			mockStatus:     HealthStatusUknown,
			setupMockError: true,
			expectedStatus: HealthStatusUknown,
			expectError:    true,
		},
		{
			name:           "Default namespace and service name",
			kubeContext:    "test-context",
			namespace:      nil,
			serviceName:    nil,
			mockStatus:     HealthStatusSuccess,
			setupMockError: false,
			expectedStatus: HealthStatusSuccess,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock connection manager
			cm := &k8shelpersmock.MockConnectionManager{}

			if tt.setupMockError {
				cm.On("GetOrCreateClientset", tt.kubeContext).Return(nil, fmt.Errorf("connection error"))
			} else {
				mockClientset := fake.NewClientset()
				cm.On("GetOrCreateClientset", tt.kubeContext).Return(mockClientset, nil)
			}

			hm := NewDesktopHealthMonitor(cm)

			if !tt.setupMockError {
				namespace := ptr.Deref(tt.namespace, DefaultNamespace)
				serviceName := ptr.Deref(tt.serviceName, DefaultServiceName)
				cacheKey := fmt.Sprintf("%s::%s::%s", tt.kubeContext, namespace, serviceName)

				mockWorker := newMockHealthMonitorWorker(1 * time.Second)
				mockWorker.healthStatus = tt.mockStatus
				hm.workerCache.Store(cacheKey, mockWorker)
			}

			// Call GetHealthStatus
			ctx := context.Background()
			status, err := hm.GetHealthStatus(ctx, tt.kubeContext, tt.namespace, tt.serviceName)

			// Assert results
			if tt.expectError {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedStatus, status)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedStatus, status)
			}
		})
	}
}

func TestInClusterHealthMonitor_GetHealthStatus(t *testing.T) {
	tests := []struct {
		name           string
		endpoint       string
		mockStatus     HealthStatus
		setupMockError bool
		expectedStatus HealthStatus
		expectError    bool
	}{
		{
			name:           "Successful status retrieval - SUCCESS",
			endpoint:       "kubetail-cluster-api.kubetail-system.svc.cluster.local:50051",
			mockStatus:     HealthStatusSuccess,
			setupMockError: false,
			expectedStatus: HealthStatusSuccess,
			expectError:    false,
		},
		{
			name:           "No endpoint - should use noop worker",
			endpoint:       "",
			mockStatus:     HealthStatusUknown,
			setupMockError: false,
			expectedStatus: HealthStatusUknown,
			expectError:    false,
		},
		{
			name:           "Worker creation error",
			endpoint:       "kubetail-cluster-api.kubetail-system.svc.cluster.local:50051",
			mockStatus:     HealthStatusUknown,
			setupMockError: true,
			expectedStatus: HealthStatusUknown,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock connection manager
			cm := &k8shelpersmock.MockConnectionManager{}

			if tt.setupMockError {
				cm.On("GetOrCreateClientset", "").Return(nil, fmt.Errorf("connection error"))
			} else {
				mockClientset := fake.NewClientset()
				cm.On("GetOrCreateClientset", "").Return(mockClientset, nil)
			}

			hm := NewInClusterHealthMonitor(cm, tt.endpoint)

			if !tt.setupMockError {
				mockWorker := newMockHealthMonitorWorker(1 * time.Second)
				mockWorker.healthStatus = tt.mockStatus
				hm.worker = mockWorker
			}

			// Call GetHealthStatus
			ctx := context.Background()
			status, err := hm.GetHealthStatus(ctx, "", nil, nil)

			// Assert results
			if tt.expectError {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedStatus, status)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedStatus, status)
			}
		})
	}
}

// WatchHealthStatus

// DesktopHealthMonitor
func TestDesktopHealthMonitor_WatchHealthStatus(t *testing.T) {
	tests := []struct {
		name           string
		kubeContext    string
		namespace      *string
		serviceName    *string
		mockStatus     HealthStatus
		setupMockError bool
		expectedStatus HealthStatus
		expectError    bool
	}{
		{
			name:           "Successful status retrieval - SUCCESS",
			kubeContext:    "test-context",
			namespace:      ptr.To("test-namespace"),
			serviceName:    ptr.To("test-service"),
			mockStatus:     HealthStatusSuccess,
			setupMockError: false,
			expectedStatus: HealthStatusSuccess,
			expectError:    false,
		},
		{
			name:           "Successful status retrieval - FAILURE",
			kubeContext:    "test-context",
			namespace:      ptr.To("test-namespace"),
			serviceName:    ptr.To("test-service"),
			mockStatus:     HealthStatusFailure,
			setupMockError: false,
			expectedStatus: HealthStatusFailure,
			expectError:    false,
		},
		{
			name:           "Successful status retrieval - PENDING",
			kubeContext:    "test-context",
			namespace:      ptr.To("test-namespace"),
			serviceName:    ptr.To("test-service"),
			mockStatus:     HealthStatusPending,
			setupMockError: false,
			expectedStatus: HealthStatusPending,
			expectError:    false,
		},
		{
			name:           "Successful status retrieval - NOTFOUND",
			kubeContext:    "test-context",
			namespace:      ptr.To("test-namespace"),
			serviceName:    ptr.To("test-service"),
			mockStatus:     HealthStatusNotFound,
			setupMockError: false,
			expectedStatus: HealthStatusNotFound,
			expectError:    false,
		},
		{
			name:           "Worker creation error",
			kubeContext:    "invalid-context",
			namespace:      ptr.To("test-namespace"),
			serviceName:    ptr.To("test-service"),
			mockStatus:     HealthStatusUknown,
			setupMockError: true,
			expectedStatus: HealthStatusUknown,
			expectError:    true,
		},
		{
			name:           "Default namespace and service name",
			kubeContext:    "test-context",
			namespace:      nil,
			serviceName:    nil,
			mockStatus:     HealthStatusSuccess,
			setupMockError: false,
			expectedStatus: HealthStatusSuccess,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			cm := &k8shelpersmock.MockConnectionManager{}

			if tt.setupMockError {
				cm.On("GetOrCreateClientset", tt.kubeContext).Return(nil, fmt.Errorf("connection error"))
			} else {
				mockClientset := fake.NewClientset()
				cm.On("GetOrCreateClientset", tt.kubeContext).Return(mockClientset, nil)
			}

			hm := NewDesktopHealthMonitor(cm)

			var mockWorker *MockHealthMonitorWorker
			namespace := ptr.Deref(tt.namespace, DefaultNamespace)
			serviceName := ptr.Deref(tt.serviceName, DefaultServiceName)
			cacheKey := fmt.Sprintf("%s::%s::%s", tt.kubeContext, namespace, serviceName)

			mockWorker = newMockHealthMonitorWorker(1 * time.Second)
			mockWorker.healthStatus = tt.mockStatus
			hm.workerCache.Store(cacheKey, mockWorker)

			// Action
			ctx := context.Background()
			mockWorker.TriggerHealthStatus(tt.mockStatus)
			status, err := hm.WatchHealthStatus(ctx, tt.kubeContext, tt.namespace, tt.serviceName)

			// Assert
			assert.NoError(t, err)
			workerStatus := <-status
			assert.Equal(t, tt.expectedStatus, workerStatus)
		})
	}
}

func TestDesktopHealthMonitor_WatchHealthStatus_ContextCancellation(t *testing.T) {
	// Setup
	cm := &k8shelpersmock.MockConnectionManager{}
	kubeContext := "test-context"
	mockClientset := fake.NewClientset()
	cm.On("GetOrCreateClientset", kubeContext).Return(mockClientset, nil)

	hm := NewDesktopHealthMonitor(cm)
	mockWorker := newMockHealthMonitorWorker(1 * time.Second)
	cacheKey := fmt.Sprintf("%s::%s::%s", kubeContext, DefaultNamespace, DefaultServiceName)
	hm.workerCache.Store(cacheKey, mockWorker)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Action
	// Start watching
	mockWorker.TriggerHealthStatus(HealthStatusSuccess)
	statusCh, err := hm.WatchHealthStatus(ctx, kubeContext, nil, nil)
	assert.NoError(t, err)

	// Receive initial status
	workerStatus := <-statusCh
	assert.Equal(t, (HealthStatus)(HealthStatusSuccess), workerStatus)

	// Cancel context
	cancel()

	// Wait for the channel to be closed
	timeout := time.After(1 * time.Second)

	// Verify channel is closed
	for {
		select {
		case _, ok := <-statusCh:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("Channel should be closed after context cancellation")
		}
	}
}

// InClusterHealthMonitor
func TestInClusterHealthMonitor_WatchHealthStatus(t *testing.T) {
	tests := []struct {
		name           string
		endpoint       string
		mockStatus     HealthStatus
		setupMockError bool
		expectedStatus HealthStatus
		expectError    bool
	}{
		{
			name:           "Successful status retrieval - SUCCESS",
			endpoint:       "kubetail-cluster-api.kubetail-system.svc.cluster.local:50051",
			mockStatus:     HealthStatusSuccess,
			expectedStatus: HealthStatusSuccess,
			expectError:    false,
		},
		{
			name:           "Successful status retrieval - Ungrouped",
			endpoint:       "",
			mockStatus:     HealthStatusUknown,
			expectedStatus: HealthStatusUknown,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock connection manager
			cm := &k8shelpersmock.MockConnectionManager{}

			if tt.setupMockError {
				cm.On("GetOrCreateClientset", "").Return(nil, fmt.Errorf("connection error"))
			} else {
				mockClientset := fake.NewClientset()
				cm.On("GetOrCreateClientset", "").Return(mockClientset, nil)
			}

			hm := NewInClusterHealthMonitor(cm, tt.endpoint)
			mockWorker := newMockHealthMonitorWorker(1 * time.Second)
			mockWorker.healthStatus = tt.mockStatus
			hm.worker = mockWorker

			// Action
			ctx := context.Background()
			mockWorker.TriggerHealthStatus(tt.mockStatus)
			status, err := hm.WatchHealthStatus(ctx, "", nil, nil)

			// Assert
			assert.NoError(t, err)
			workerStatus := <-status
			assert.Equal(t, tt.expectedStatus, workerStatus)
		})
	}
}

func TestInClusterHealthMonitor_WatchHealthStatus_ContextCancellation(t *testing.T) {
	// Setup
	cm := &k8shelpersmock.MockConnectionManager{}
	kubeContext := "test-context"
	mockClientset := fake.NewClientset()
	cm.On("GetOrCreateClientset", kubeContext).Return(mockClientset, nil)

	hm := NewInClusterHealthMonitor(cm, "")
	mockWorker := newMockHealthMonitorWorker(1 * time.Second)
	mockWorker.healthStatus = HealthStatusSuccess
	hm.worker = mockWorker

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Action
	// Start watching
	mockWorker.TriggerHealthStatus(HealthStatusSuccess)
	statusCh, err := hm.WatchHealthStatus(ctx, "", nil, nil)
	assert.NoError(t, err)

	// Receive initial status
	workerStatus := <-statusCh
	assert.Equal(t, (HealthStatus)(HealthStatusSuccess), workerStatus)

	// Cancel context
	cancel()

	// Wait for the channel to be closed
	timeout := time.After(1 * time.Second)

	// Verify channel is closed
	for {
		select {
		case _, ok := <-statusCh:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("Channel should be closed after context cancellation")
		}
	}
}

// ReadyWait
func TestDesktopHealthMonitor_ReadyWait_Immediate_Success(t *testing.T) {
	// Setup
	cm := &k8shelpersmock.MockConnectionManager{}
	kubeContext := "test-context"
	mockClientset := fake.NewClientset()
	cm.On("GetOrCreateClientset", kubeContext).Return(mockClientset, nil)

	hm := NewDesktopHealthMonitor(cm)
	mockWorker := newMockHealthMonitorWorker(1 * time.Second)
	cacheKey := fmt.Sprintf("%s::%s::%s", kubeContext, DefaultNamespace, DefaultServiceName)
	hm.workerCache.Store(cacheKey, mockWorker)

	// Action
	ctx := context.Background()
	mockWorker.healthStatus = HealthStatusSuccess
	err := hm.ReadyWait(ctx, kubeContext, nil, nil)

	// Assert
	assert.NoError(t, err)
}
func TestDesktopHealthMonitor_ReadyWait_WaitSuccess(t *testing.T) {
	// Setup
	cm := &k8shelpersmock.MockConnectionManager{}
	kubeContext := "test-context"
	mockClientset := fake.NewClientset()
	cm.On("GetOrCreateClientset", kubeContext).Return(mockClientset, nil)

	hm := NewDesktopHealthMonitor(cm)
	mockWorker := newMockHealthMonitorWorker(1 * time.Second)
	cacheKey := fmt.Sprintf("%s::%s::%s", kubeContext, DefaultNamespace, DefaultServiceName)
	hm.workerCache.Store(cacheKey, mockWorker)

	// Action
	ctx := context.Background()

	go func() {
		time.Sleep(100 * time.Millisecond)
		mockWorker.TriggerHealthStatus(HealthStatusSuccess)
	}()

	// Call the health monitor's ReadyWait
	err := hm.ReadyWait(ctx, kubeContext, nil, nil)

	// Assert
	assert.NoError(t, err)
}

func TestDesktopHealthMonitor_ReadyWait_WaitFailure(t *testing.T) {
	// Setup
	cm := &k8shelpersmock.MockConnectionManager{}
	kubeContext := "test-context"
	mockClientset := fake.NewClientset()
	cm.On("GetOrCreateClientset", kubeContext).Return(mockClientset, nil)

	hm := NewDesktopHealthMonitor(cm)
	mockWorker := newMockHealthMonitorWorker(1 * time.Second)
	mockWorker.healthStatus = HealthStatusPending
	cacheKey := fmt.Sprintf("%s::%s::%s", kubeContext, DefaultNamespace, DefaultServiceName)
	hm.workerCache.Store(cacheKey, mockWorker)

	// Action
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	// Call the health monitor's ReadyWait
	err := hm.ReadyWait(ctx, kubeContext, nil, nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

func TestInClusterHealthMonitor_ReadyWait_ImmediateSuccess(t *testing.T) {
	// Setup
	cm := &k8shelpersmock.MockConnectionManager{}
	kubeContext := "test-context"
	mockClientset := fake.NewClientset()
	cm.On("GetOrCreateClientset", kubeContext).Return(mockClientset, nil)
	hm := NewInClusterHealthMonitor(cm, "")
	mockWorker := newMockHealthMonitorWorker(1 * time.Second)
	hm.worker = mockWorker

	// Action
	ctx := context.Background()
	mockWorker.healthStatus = HealthStatusSuccess
	err := hm.ReadyWait(ctx, kubeContext, nil, nil)

	// Assert
	assert.NoError(t, err)
}
func TestInClusterHealthMonitor_ReadyWait_WaitSuccess(t *testing.T) {
	// Setup
	cm := &k8shelpersmock.MockConnectionManager{}
	kubeContext := "test-context"
	mockClientset := fake.NewClientset()
	cm.On("GetOrCreateClientset", kubeContext).Return(mockClientset, nil)
	hm := NewInClusterHealthMonitor(cm, "")
	mockWorker := newMockHealthMonitorWorker(1 * time.Second)
	hm.worker = mockWorker

	// Action
	ctx := context.Background()
	go func() {
		time.Sleep(100 * time.Millisecond)
		mockWorker.TriggerHealthStatus(HealthStatusSuccess)
	}()
	err := hm.ReadyWait(ctx, kubeContext, nil, nil)

	// Assert
	assert.NoError(t, err)
}

func TestInClusterHealthMonitor_ReadyWait_WaitFailure(t *testing.T) {
	// Setup
	cm := &k8shelpersmock.MockConnectionManager{}
	kubeContext := "test-context"
	mockClientset := fake.NewClientset()
	cm.On("GetOrCreateClientset", kubeContext).Return(mockClientset, nil)
	hm := NewInClusterHealthMonitor(cm, "")
	mockWorker := newMockHealthMonitorWorker(1 * time.Second)
	mockWorker.healthStatus = HealthStatusPending
	hm.worker = mockWorker

	// Action
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	// Call the health monitor's ReadyWait
	err := hm.ReadyWait(ctx, kubeContext, nil, nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}
