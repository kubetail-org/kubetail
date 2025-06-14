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

// Helper functions and structs for testing

// TestCase represents a common test case structure for health monitor tests
type TestCase struct {
	Name           string
	KubeContext    string
	Namespace      *string
	ServiceName    *string
	Endpoint       string
	MockStatus     HealthStatus
	SetupMockError bool
	ExpectedStatus HealthStatus
	ExpectError    bool
	ShutdownTime   time.Duration
	NumberWorkers  int
	HasWorker      bool
}

// setupMockConnectionManager creates and configures a mock connection manager
func setupMockConnectionManager(tc TestCase) *k8shelpersmock.MockConnectionManager {
	cm := &k8shelpersmock.MockConnectionManager{}

	if tc.SetupMockError {
		cm.On("GetOrCreateClientset", tc.KubeContext).Return(nil, fmt.Errorf("connection error"))
	} else {
		mockClientset := fake.NewClientset()
		cm.On("GetOrCreateClientset", tc.KubeContext).Return(mockClientset, nil)
	}

	return cm
}

// setupDesktopHealthMonitor creates and configures a DesktopHealthMonitor with mock workers
func setupDesktopHealthMonitor(tc TestCase) (*DesktopHealthMonitor, *MockHealthMonitorWorker) {
	cm := setupMockConnectionManager(tc)
	hm := NewDesktopHealthMonitor(cm)

	var mockWorker *MockHealthMonitorWorker

	// Only create and store a worker if we're not testing an error case
	if !tc.SetupMockError {
		namespace := ptr.Deref(tc.Namespace, DefaultNamespace)
		serviceName := ptr.Deref(tc.ServiceName, DefaultServiceName)
		cacheKey := fmt.Sprintf("%s::%s::%s", tc.KubeContext, namespace, serviceName)

		mockWorker = newMockHealthMonitorWorker(tc.ShutdownTime)
		mockWorker.healthStatus = tc.MockStatus
		hm.workerCache.Store(cacheKey, mockWorker)
	}

	return hm, mockWorker
}

// setupInClusterHealthMonitor creates and configures an InClusterHealthMonitor with mock worker
func setupInClusterHealthMonitor(tc TestCase) (*InClusterHealthMonitor, *MockHealthMonitorWorker) {
	cm := setupMockConnectionManager(tc)
	hm := NewInClusterHealthMonitor(cm, tc.Endpoint)

	var mockWorker *MockHealthMonitorWorker
	if tc.HasWorker {
		mockWorker = newMockHealthMonitorWorker(tc.ShutdownTime)
		mockWorker.healthStatus = tc.MockStatus
		hm.worker = mockWorker
	}

	return hm, mockWorker
}

// assertHealthStatus asserts the expected health status and error
func assertHealthStatus(t *testing.T, status HealthStatus, err error, tc TestCase) {
	if tc.ExpectError {
		assert.Error(t, err)
	} else {
		assert.NoError(t, err)
	}
	assert.Equal(t, tc.ExpectedStatus, status)
}

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
	tests := []TestCase{
		{
			Name:          "Shutdown with 0 workers",
			NumberWorkers: 0,
			ShutdownTime:  1 * time.Second,
		},
		{
			Name:          "Shutdown with 1 worker",
			NumberWorkers: 1,
			ShutdownTime:  1 * time.Second,
		},
		{
			Name:          "Shutdown with multiple workers",
			NumberWorkers: 2,
			ShutdownTime:  1 * time.Second,
		},
	}

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			// Create connection manager
			cm := setupMockConnectionManager(tc)

			// Create health monitor
			hm := NewDesktopHealthMonitor(cm)

			var workers []*MockHealthMonitorWorker

			// Create mock health monitor workers
			for i := 0; i < tc.NumberWorkers; i++ {
				worker := newMockHealthMonitorWorker(tc.ShutdownTime)
				workers = append(workers, worker)
				hm.workerCache.Store(fmt.Sprintf("worker-%d", i), worker)
			}

			// Shutdown health monitor
			hm.Shutdown()

			// Assert workers are shutdown
			for _, worker := range workers {
				assert.True(t, worker.shutdown.Load())
			}
		})
	}
}

func TestInClusterHealthMonitor_Shutdown(t *testing.T) {
	tests := []TestCase{
		{
			Name:         "Shutdown with endpoint and 0 workers",
			Endpoint:     "kubetail-cluster-api.kubetail-system.svc.cluster.local:50051",
			HasWorker:    false,
			ShutdownTime: 1 * time.Second,
		},
		{
			Name:         "Shutdown with endpoint and 1 worker",
			Endpoint:     "kubetail-cluster-api.kubetail-system.svc.cluster.local:50051",
			HasWorker:    true,
			ShutdownTime: 1 * time.Second,
		},
	}

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			// Setup test environment
			hm, worker := setupInClusterHealthMonitor(tc)

			// Assert health monitor is not nil
			assert.NotNil(t, hm)

			// Assert health monitor is of the expected type
			assert.IsType(t, &InClusterHealthMonitor{}, hm)

			// Shutdown health monitor
			hm.Shutdown()

			// Assert worker is shutdown
			if tc.HasWorker {
				assert.True(t, worker.shutdown.Load())
			}
		})
	}
}
func TestDesktopHealthMonitor_GetHealthStatus(t *testing.T) {
	tests := []TestCase{
		{
			Name:           "Successful status retrieval - SUCCESS",
			KubeContext:    "test-context",
			Namespace:      ptr.To("test-namespace"),
			ServiceName:    ptr.To("test-service"),
			MockStatus:     HealthStatusSuccess,
			SetupMockError: false,
			ExpectedStatus: HealthStatusSuccess,
			ExpectError:    false,
			ShutdownTime:   1 * time.Second,
		},
		{
			Name:           "Successful status retrieval - FAILURE",
			KubeContext:    "test-context",
			Namespace:      ptr.To("test-namespace"),
			ServiceName:    ptr.To("test-service"),
			MockStatus:     HealthStatusFailure,
			SetupMockError: false,
			ExpectedStatus: HealthStatusFailure,
			ExpectError:    false,
			ShutdownTime:   1 * time.Second,
		},
		{
			Name:           "Successful status retrieval - PENDING",
			KubeContext:    "test-context",
			Namespace:      ptr.To("test-namespace"),
			ServiceName:    ptr.To("test-service"),
			MockStatus:     HealthStatusPending,
			SetupMockError: false,
			ExpectedStatus: HealthStatusPending,
			ExpectError:    false,
			ShutdownTime:   1 * time.Second,
		},
		{
			Name:           "Successful status retrieval - NOTFOUND",
			KubeContext:    "test-context",
			Namespace:      ptr.To("test-namespace"),
			ServiceName:    ptr.To("test-service"),
			MockStatus:     HealthStatusNotFound,
			SetupMockError: false,
			ExpectedStatus: HealthStatusNotFound,
			ExpectError:    false,
			ShutdownTime:   1 * time.Second,
		},
		{
			Name:           "Worker creation error",
			KubeContext:    "invalid-context",
			Namespace:      ptr.To("test-namespace"),
			ServiceName:    ptr.To("test-service"),
			MockStatus:     HealthStatusUknown,
			SetupMockError: true,
			ExpectedStatus: HealthStatusUknown,
			ExpectError:    true,
			ShutdownTime:   1 * time.Second,
		},
		{
			Name:           "Default namespace and service name",
			KubeContext:    "test-context",
			Namespace:      nil,
			ServiceName:    nil,
			MockStatus:     HealthStatusSuccess,
			SetupMockError: false,
			ExpectedStatus: HealthStatusSuccess,
			ExpectError:    false,
			ShutdownTime:   1 * time.Second,
		},
	}

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			// Setup test environment
			hm, _ := setupDesktopHealthMonitor(tc)

			// Call GetHealthStatus
			ctx := context.Background()
			status, err := hm.GetHealthStatus(ctx, tc.KubeContext, tc.Namespace, tc.ServiceName)

			// Assert results
			assertHealthStatus(t, status, err, tc)
		})
	}
}

func TestInClusterHealthMonitor_GetHealthStatus(t *testing.T) {
	tests := []TestCase{
		{
			Name:           "Successful status retrieval - SUCCESS",
			KubeContext:    "",
			Endpoint:       "kubetail-cluster-api.kubetail-system.svc.cluster.local:50051",
			MockStatus:     HealthStatusSuccess,
			SetupMockError: false,
			ExpectedStatus: HealthStatusSuccess,
			ExpectError:    false,
			ShutdownTime:   1 * time.Second,
			HasWorker:      true,
		},
		{
			Name:           "No endpoint - should use noop worker",
			KubeContext:    "",
			Endpoint:       "",
			MockStatus:     HealthStatusUknown,
			SetupMockError: false,
			ExpectedStatus: HealthStatusUknown,
			ExpectError:    false,
			ShutdownTime:   1 * time.Second,
			HasWorker:      true,
		},
		{
			Name:           "Worker creation error",
			KubeContext:    "",
			Endpoint:       "kubetail-cluster-api.kubetail-system.svc.cluster.local:50051",
			MockStatus:     HealthStatusUknown,
			SetupMockError: true,
			ExpectedStatus: HealthStatusUknown,
			ExpectError:    true,
			ShutdownTime:   1 * time.Second,
			HasWorker:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			// Setup test environment
			hm, _ := setupInClusterHealthMonitor(tc)

			// Call GetHealthStatus
			ctx := context.Background()
			status, err := hm.GetHealthStatus(ctx, tc.KubeContext, tc.Namespace, tc.ServiceName)

			// Assert results
			assertHealthStatus(t, status, err, tc)
		})
	}
}

// WatchHealthStatus

// DesktopHealthMonitor
func TestDesktopHealthMonitor_WatchHealthStatus(t *testing.T) {
	tests := []TestCase{
		{
			Name:           "Successful status retrieval - SUCCESS",
			KubeContext:    "test-context",
			Namespace:      ptr.To("test-namespace"),
			ServiceName:    ptr.To("test-service"),
			MockStatus:     HealthStatusSuccess,
			SetupMockError: false,
			ExpectedStatus: HealthStatusSuccess,
			ExpectError:    false,
			ShutdownTime:   1 * time.Second,
		},
		{
			Name:           "Successful status retrieval - FAILURE",
			KubeContext:    "test-context",
			Namespace:      ptr.To("test-namespace"),
			ServiceName:    ptr.To("test-service"),
			MockStatus:     HealthStatusFailure,
			SetupMockError: false,
			ExpectedStatus: HealthStatusFailure,
			ExpectError:    false,
			ShutdownTime:   1 * time.Second,
		},
		{
			Name:           "Successful status retrieval - PENDING",
			KubeContext:    "test-context",
			Namespace:      ptr.To("test-namespace"),
			ServiceName:    ptr.To("test-service"),
			MockStatus:     HealthStatusPending,
			SetupMockError: false,
			ExpectedStatus: HealthStatusPending,
			ExpectError:    false,
			ShutdownTime:   1 * time.Second,
		},
		{
			Name:           "Successful status retrieval - NOTFOUND",
			KubeContext:    "test-context",
			Namespace:      ptr.To("test-namespace"),
			ServiceName:    ptr.To("test-service"),
			MockStatus:     HealthStatusNotFound,
			SetupMockError: false,
			ExpectedStatus: HealthStatusNotFound,
			ExpectError:    false,
			ShutdownTime:   1 * time.Second,
		},
		{
			Name:           "Worker creation error",
			KubeContext:    "invalid-context",
			Namespace:      ptr.To("test-namespace"),
			ServiceName:    ptr.To("test-service"),
			MockStatus:     HealthStatusUknown,
			SetupMockError: true,
			ExpectedStatus: HealthStatusUknown,
			ExpectError:    true,
			ShutdownTime:   1 * time.Second,
		},
		{
			Name:           "Default namespace and service name",
			KubeContext:    "test-context",
			Namespace:      nil,
			ServiceName:    nil,
			MockStatus:     HealthStatusSuccess,
			SetupMockError: false,
			ExpectedStatus: HealthStatusSuccess,
			ExpectError:    false,
			ShutdownTime:   1 * time.Second,
		},
	}

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			// Setup test environment
			hm, mockWorker := setupDesktopHealthMonitor(tc)

			// Action
			ctx := context.Background()

			// Only trigger health status if we have a mock worker (not an error case)
			if mockWorker != nil {
				mockWorker.TriggerHealthStatus(tc.MockStatus)
			}

			statusCh, err := hm.WatchHealthStatus(ctx, tc.KubeContext, tc.Namespace, tc.ServiceName)

			// Assert
			if tc.ExpectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				workerStatus := <-statusCh
				assert.Equal(t, tc.ExpectedStatus, workerStatus)
			}
		})
	}
}

func TestDesktopHealthMonitor_WatchHealthStatus_ContextCancellation(t *testing.T) {
	// Setup test case
	tc := TestCase{
		Name:           "Context Cancellation",
		KubeContext:    "test-context",
		MockStatus:     HealthStatusSuccess,
		SetupMockError: false,
		ExpectedStatus: HealthStatusSuccess,
		ExpectError:    false,
		ShutdownTime:   1 * time.Second,
	}

	// Setup test environment
	hm, mockWorker := setupDesktopHealthMonitor(tc)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Action
	// Start watching
	mockWorker.TriggerHealthStatus(tc.MockStatus)
	statusCh, err := hm.WatchHealthStatus(ctx, tc.KubeContext, tc.Namespace, tc.ServiceName)
	assert.NoError(t, err)

	// Receive initial status
	workerStatus := <-statusCh
	assert.Equal(t, tc.MockStatus, workerStatus)

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
	tests := []TestCase{
		{
			Name:           "Successful status retrieval - SUCCESS",
			KubeContext:    "",
			Endpoint:       "kubetail-cluster-api.kubetail-system.svc.cluster.local:50051",
			MockStatus:     HealthStatusSuccess,
			SetupMockError: false,
			ExpectedStatus: HealthStatusSuccess,
			ExpectError:    false,
			ShutdownTime:   1 * time.Second,
			HasWorker:      true,
		},
		{
			Name:           "Successful status retrieval - Ungrouped",
			KubeContext:    "",
			Endpoint:       "",
			MockStatus:     HealthStatusUknown,
			SetupMockError: false,
			ExpectedStatus: HealthStatusUknown,
			ExpectError:    false,
			ShutdownTime:   1 * time.Second,
			HasWorker:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			// Setup test environment
			hm, mockWorker := setupInClusterHealthMonitor(tc)

			// Action
			ctx := context.Background()
			mockWorker.TriggerHealthStatus(tc.MockStatus)
			statusCh, err := hm.WatchHealthStatus(ctx, tc.KubeContext, tc.Namespace, tc.ServiceName)

			// Assert
			assert.NoError(t, err)
			workerStatus := <-statusCh
			assert.Equal(t, tc.ExpectedStatus, workerStatus)
		})
	}
}

func TestInClusterHealthMonitor_WatchHealthStatus_ContextCancellation(t *testing.T) {
	// Setup test case
	tc := TestCase{
		Name:           "Context Cancellation",
		KubeContext:    "",
		Endpoint:       "",
		MockStatus:     HealthStatusSuccess,
		SetupMockError: false,
		ExpectedStatus: HealthStatusSuccess,
		ExpectError:    false,
		ShutdownTime:   1 * time.Second,
		HasWorker:      true,
	}

	// Setup test environment
	hm, mockWorker := setupInClusterHealthMonitor(tc)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Action
	// Start watching
	mockWorker.TriggerHealthStatus(tc.MockStatus)
	statusCh, err := hm.WatchHealthStatus(ctx, tc.KubeContext, tc.Namespace, tc.ServiceName)
	assert.NoError(t, err)

	// Receive initial status
	workerStatus := <-statusCh
	assert.Equal(t, tc.MockStatus, workerStatus)

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
	// Setup test case
	tc := TestCase{
		Name:           "Immediate Success",
		KubeContext:    "test-context",
		MockStatus:     HealthStatusSuccess,
		SetupMockError: false,
		ExpectedStatus: HealthStatusSuccess,
		ExpectError:    false,
		ShutdownTime:   1 * time.Second,
	}

	// Setup test environment
	hm, _ := setupDesktopHealthMonitor(tc)

	// Action
	ctx := context.Background()
	err := hm.ReadyWait(ctx, tc.KubeContext, tc.Namespace, tc.ServiceName)

	// Assert
	assert.NoError(t, err)
}
func TestDesktopHealthMonitor_ReadyWait_WaitSuccess(t *testing.T) {
	// Setup test case
	tc := TestCase{
		Name:           "Wait Success",
		KubeContext:    "test-context",
		MockStatus:     HealthStatusPending, // Start with pending status
		SetupMockError: false,
		ExpectedStatus: HealthStatusSuccess,
		ExpectError:    false,
		ShutdownTime:   1 * time.Second,
	}

	// Setup test environment
	hm, mockWorker := setupDesktopHealthMonitor(tc)

	// Action
	ctx := context.Background()

	go func() {
		time.Sleep(100 * time.Millisecond)
		mockWorker.TriggerHealthStatus(HealthStatusSuccess)
	}()

	// Call the health monitor's ReadyWait
	err := hm.ReadyWait(ctx, tc.KubeContext, tc.Namespace, tc.ServiceName)

	// Assert
	assert.NoError(t, err)
}

func TestDesktopHealthMonitor_ReadyWait_WaitFailure(t *testing.T) {
	// Setup test case
	tc := TestCase{
		Name:           "Wait Failure",
		KubeContext:    "test-context",
		MockStatus:     HealthStatusPending, // Start with pending status and never change
		SetupMockError: false,
		ExpectedStatus: HealthStatusPending,
		ExpectError:    true,
		ShutdownTime:   1 * time.Second,
	}

	// Setup test environment
	hm, _ := setupDesktopHealthMonitor(tc)

	// Action
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Call the health monitor's ReadyWait
	err := hm.ReadyWait(ctx, tc.KubeContext, tc.Namespace, tc.ServiceName)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

func TestInClusterHealthMonitor_ReadyWait_ImmediateSuccess(t *testing.T) {
	// Setup test case
	tc := TestCase{
		Name:           "Immediate Success",
		KubeContext:    "test-context",
		Endpoint:       "",
		MockStatus:     HealthStatusSuccess,
		SetupMockError: false,
		ExpectedStatus: HealthStatusSuccess,
		ExpectError:    false,
		ShutdownTime:   1 * time.Second,
		HasWorker:      true,
	}

	// Setup test environment
	hm, _ := setupInClusterHealthMonitor(tc)

	// Action
	ctx := context.Background()
	err := hm.ReadyWait(ctx, tc.KubeContext, tc.Namespace, tc.ServiceName)

	// Assert
	assert.NoError(t, err)
}
func TestInClusterHealthMonitor_ReadyWait_WaitSuccess(t *testing.T) {
	// Setup test case
	tc := TestCase{
		Name:           "Wait Success",
		KubeContext:    "test-context",
		Endpoint:       "",
		MockStatus:     HealthStatusPending, // Start with pending status
		SetupMockError: false,
		ExpectedStatus: HealthStatusSuccess,
		ExpectError:    false,
		ShutdownTime:   1 * time.Second,
		HasWorker:      true,
	}

	// Setup test environment
	hm, mockWorker := setupInClusterHealthMonitor(tc)

	// Action
	ctx := context.Background()
	go func() {
		time.Sleep(100 * time.Millisecond)
		mockWorker.TriggerHealthStatus(HealthStatusSuccess)
	}()
	err := hm.ReadyWait(ctx, tc.KubeContext, tc.Namespace, tc.ServiceName)

	// Assert
	assert.NoError(t, err)
}

func TestInClusterHealthMonitor_ReadyWait_WaitFailure(t *testing.T) {
	// Setup test case
	tc := TestCase{
		Name:           "Wait Failure",
		KubeContext:    "test-context",
		Endpoint:       "",
		MockStatus:     HealthStatusPending, // Start with pending status and never change
		SetupMockError: false,
		ExpectedStatus: HealthStatusPending,
		ExpectError:    true,
		ShutdownTime:   1 * time.Second,
		HasWorker:      true,
	}

	// Setup test environment
	hm, _ := setupInClusterHealthMonitor(tc)

	// Action
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Call the health monitor's ReadyWait
	err := hm.ReadyWait(ctx, tc.KubeContext, tc.Namespace, tc.ServiceName)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}
