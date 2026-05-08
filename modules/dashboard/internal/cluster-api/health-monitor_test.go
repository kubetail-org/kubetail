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
	"testing"
	"time"

	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/config"
	sharedcfg "github.com/kubetail-org/kubetail/modules/shared/config"
	k8shelpersmock "github.com/kubetail-org/kubetail/modules/shared/k8shelpers/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockHealthMonitorWorker is a testify-based mock implementation of the healthMonitorWorker interface
type MockHealthMonitorWorker struct {
	mock.Mock
}

func (m *MockHealthMonitorWorker) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockHealthMonitorWorker) Shutdown() {
	m.Called()
}

func (m *MockHealthMonitorWorker) GetHealthStatus() HealthStatus {
	args := m.Called()
	return args.Get(0).(HealthStatus)
}

func (m *MockHealthMonitorWorker) WatchHealthStatus(ctx context.Context) (<-chan HealthStatus, error) {
	args := m.Called(ctx)
	return args.Get(0).(<-chan HealthStatus), args.Error(1)
}

func (m *MockHealthMonitorWorker) ReadyWait(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// Helper function to create a channel with initial status
func createMockStatusChannel(initialStatus HealthStatus) <-chan HealthStatus {
	ch := make(chan HealthStatus, 1)
	ch <- initialStatus
	return ch
}

// Helper function to create a channel that closes when context is done
func createMockStatusChannelWithContext(ctx context.Context, initialStatus HealthStatus) <-chan HealthStatus {
	ch := make(chan HealthStatus, 1)
	ch <- initialStatus

	go func() {
		<-ctx.Done()
		close(ch)
	}()

	return ch
}

func TestNewHealthMonitor(t *testing.T) {
	tests := []struct {
		name        string
		environment sharedcfg.Environment
		enabled     bool
		want        any
	}{
		{
			name:        "Desktop environment",
			environment: sharedcfg.EnvironmentDesktop,
			want:        &DesktopHealthMonitor{},
		},
		{
			name:        "Cluster environment, cluster-api disabled",
			environment: sharedcfg.EnvironmentCluster,
			enabled:     false,
			want:        &InClusterHealthMonitor{},
		},
		{
			name:        "Cluster environment, cluster-api enabled",
			environment: sharedcfg.EnvironmentCluster,
			enabled:     true,
			want:        &InClusterHealthMonitor{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := &k8shelpersmock.MockConnectionManager{}

			cfg := config.DefaultConfig()
			cfg.Environment = tt.environment
			cfg.ClusterAPIEnabled = tt.enabled

			hm := NewHealthMonitor(cfg, cm)

			assert.NotNil(t, hm)
			assert.IsType(t, tt.want, hm)
		})
	}
}

func TestNewHealthMonitor_InvalidEnvironment(t *testing.T) {
	// create connection manager
	cm := &k8shelpersmock.MockConnectionManager{}

	// create config
	cfg := config.DefaultConfig()
	cfg.Environment = "invalid"

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
	}{
		{
			name:          "Shutdown with 0 workers",
			numberWorkers: 0,
		},
		{
			name:          "Shutdown with 1 worker",
			numberWorkers: 1,
		},
		{
			name:          "Shutdown with multiple workers",
			numberWorkers: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// create connection manager
			cm := &k8shelpersmock.MockConnectionManager{}

			// create health monitor
			hm := NewDesktopHealthMonitor(cm)

			var workers []*MockHealthMonitorWorker

			// create mock health monitor workers
			for i := 0; i < tt.numberWorkers; i++ {
				worker := new(MockHealthMonitorWorker)
				worker.On("Shutdown").Return()
				workers = append(workers, worker)
				hm.workerCache.Store(fmt.Sprintf("worker-%d", i), worker)
			}

			// shutdown health monitor
			hm.Shutdown()

			// assert all workers were shutdown
			for _, worker := range workers {
				worker.AssertExpectations(t)
			}
		})
	}
}

func TestInClusterHealthMonitor_Shutdown(t *testing.T) {

	tests := []struct {
		name      string
		hasWorker bool
		want      interface{}
	}{
		{
			name:      "Shutdown with endpoint and 0 workers",
			hasWorker: false,
			want:      &InClusterHealthMonitor{},
		},
		{
			name:      "Shutdown with endpoint and 1 worker",
			hasWorker: true,
			want:      &InClusterHealthMonitor{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// create connection manager
			cm := &k8shelpersmock.MockConnectionManager{}

			// create health monitor
			hm := NewInClusterHealthMonitor(cm, true)
			var worker *MockHealthMonitorWorker

			// create mock health monitor worker
			if tt.hasWorker {
				worker = new(MockHealthMonitorWorker)
				worker.On("Shutdown").Return()
				hm.worker = worker
			}

			// assert health monitor is not nil
			assert.NotNil(t, hm)

			// assert health monitor is of the expected type
			assert.IsType(t, tt.want, hm)

			// shutdown health monitor
			hm.Shutdown()

			// assert worker shutdown was called
			if tt.hasWorker {
				worker.AssertExpectations(t)
			}

		})
	}
}

func TestDesktopHealthMonitor_GetHealthStatus(t *testing.T) {
	tests := []struct {
		name           string
		kubeContext    string
		mockStatus     HealthStatus
		setupMockError bool
		expectedStatus HealthStatus
		expectError    bool
	}{
		{
			name:           "Successful status retrieval - SUCCESS",
			kubeContext:    "test-context",
			mockStatus:     HealthStatusSuccess,
			setupMockError: false,
			expectedStatus: HealthStatusSuccess,
			expectError:    false,
		},
		{
			name:           "Successful status retrieval - FAILURE",
			kubeContext:    "test-context",
			mockStatus:     HealthStatusFailure,
			setupMockError: false,
			expectedStatus: HealthStatusFailure,
			expectError:    false,
		},
		{
			name:           "Successful status retrieval - PENDING",
			kubeContext:    "test-context",
			mockStatus:     HealthStatusPending,
			setupMockError: false,
			expectedStatus: HealthStatusPending,
			expectError:    false,
		},
		{
			name:           "Successful status retrieval - NOTFOUND",
			kubeContext:    "test-context",
			mockStatus:     HealthStatusNotFound,
			setupMockError: false,
			expectedStatus: HealthStatusNotFound,
			expectError:    false,
		},
		{
			name:           "Worker creation error",
			kubeContext:    "invalid-context",
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
				cm.On("GetOrCreateRestConfig", tt.kubeContext).Return(nil, fmt.Errorf("connection error"))
			}

			hm := NewDesktopHealthMonitor(cm)

			if !tt.setupMockError {
				mockWorker := new(MockHealthMonitorWorker)
				mockWorker.On("GetHealthStatus").Return(tt.mockStatus)
				hm.workerCache.Store(tt.kubeContext, mockWorker)
			}

			// Call GetHealthStatus
			ctx := context.Background()
			status, err := hm.GetHealthStatus(ctx, tt.kubeContext)

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
		enabled        bool
		mockStatus     HealthStatus
		setupMockError bool
		expectedStatus HealthStatus
		expectError    bool
	}{
		{
			name:           "Successful status retrieval - SUCCESS",
			enabled:        true,
			mockStatus:     HealthStatusSuccess,
			setupMockError: false,
			expectedStatus: HealthStatusSuccess,
			expectError:    false,
		},
		{
			name:           "Disabled - should use noop worker",
			enabled:        false,
			mockStatus:     HealthStatusUknown,
			setupMockError: false,
			expectedStatus: HealthStatusUknown,
			expectError:    false,
		},
		{
			name:           "Worker creation error",
			enabled:        true,
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
				cm.On("GetOrCreateRestConfig", "").Return(nil, fmt.Errorf("connection error"))
			}

			hm := NewInClusterHealthMonitor(cm, tt.enabled)

			if !tt.setupMockError {
				mockWorker := new(MockHealthMonitorWorker)
				mockWorker.On("GetHealthStatus").Return(tt.mockStatus)
				hm.worker = mockWorker
			}

			// Call GetHealthStatus
			ctx := context.Background()
			status, err := hm.GetHealthStatus(ctx, "")

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
		mockStatus     HealthStatus
		setupMockError bool
		expectedStatus HealthStatus
		expectError    bool
	}{
		{
			name:           "Successful status retrieval - SUCCESS",
			kubeContext:    "test-context",
			mockStatus:     HealthStatusSuccess,
			setupMockError: false,
			expectedStatus: HealthStatusSuccess,
			expectError:    false,
		},
		{
			name:           "Successful status retrieval - FAILURE",
			kubeContext:    "test-context",
			mockStatus:     HealthStatusFailure,
			setupMockError: false,
			expectedStatus: HealthStatusFailure,
			expectError:    false,
		},
		{
			name:           "Successful status retrieval - PENDING",
			kubeContext:    "test-context",
			mockStatus:     HealthStatusPending,
			setupMockError: false,
			expectedStatus: HealthStatusPending,
			expectError:    false,
		},
		{
			name:           "Successful status retrieval - NOTFOUND",
			kubeContext:    "test-context",
			mockStatus:     HealthStatusNotFound,
			setupMockError: false,
			expectedStatus: HealthStatusNotFound,
			expectError:    false,
		},
		{
			name:           "Worker creation error",
			kubeContext:    "invalid-context",
			mockStatus:     HealthStatusUknown,
			setupMockError: true,
			expectedStatus: HealthStatusUknown,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			cm := &k8shelpersmock.MockConnectionManager{}

			if tt.setupMockError {
				cm.On("GetOrCreateRestConfig", tt.kubeContext).Return(nil, fmt.Errorf("connection error"))
			}

			hm := NewDesktopHealthMonitor(cm)

			if !tt.setupMockError {
				mockWorker := new(MockHealthMonitorWorker)
				ctx := context.Background()
				statusCh := createMockStatusChannel(tt.mockStatus)
				mockWorker.On("WatchHealthStatus", ctx).Return(statusCh, nil)
				hm.workerCache.Store(tt.kubeContext, mockWorker)

				// Action
				status, err := hm.WatchHealthStatus(ctx, tt.kubeContext)

				// Assert
				assert.NoError(t, err)
				workerStatus := <-status
				assert.Equal(t, tt.expectedStatus, workerStatus)
				mockWorker.AssertExpectations(t)
			} else {
				// Action for error case
				ctx := context.Background()
				_, err := hm.WatchHealthStatus(ctx, tt.kubeContext)

				// Assert
				assert.Error(t, err)
			}
		})
	}
}

func TestDesktopHealthMonitor_WatchHealthStatus_ContextCancellation(t *testing.T) {
	// Setup
	cm := &k8shelpersmock.MockConnectionManager{}
	kubeContext := "test-context"

	hm := NewDesktopHealthMonitor(cm)
	mockWorker := new(MockHealthMonitorWorker)
	hm.workerCache.Store(kubeContext, mockWorker)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Action
	statusCh := createMockStatusChannelWithContext(ctx, HealthStatusSuccess)
	mockWorker.On("WatchHealthStatus", mock.Anything).Return(statusCh, nil)

	watchCh, err := hm.WatchHealthStatus(ctx, kubeContext)
	assert.NoError(t, err)

	// Receive initial status
	workerStatus := <-watchCh
	assert.Equal(t, HealthStatus("SUCCESS"), workerStatus)

	// Cancel context
	cancel()

	// Wait for the channel to be closed
	timeout := time.After(1 * time.Second)

	// Verify channel is closed
	for {
		select {
		case _, ok := <-watchCh:
			if !ok {
				mockWorker.AssertExpectations(t)
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
		enabled        bool
		mockStatus     HealthStatus
		setupMockError bool
		expectedStatus HealthStatus
		expectError    bool
	}{
		{
			name:           "Successful status retrieval - SUCCESS",
			enabled:        true,
			mockStatus:     HealthStatusSuccess,
			expectedStatus: HealthStatusSuccess,
			expectError:    false,
		},
		{
			name:           "Disabled - should use noop worker",
			enabled:        false,
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
				cm.On("GetOrCreateRestConfig", "").Return(nil, fmt.Errorf("connection error"))
			}

			hm := NewInClusterHealthMonitor(cm, tt.enabled)
			mockWorker := new(MockHealthMonitorWorker)
			hm.worker = mockWorker

			// Action
			ctx := context.Background()
			statusCh := createMockStatusChannel(tt.mockStatus)
			mockWorker.On("WatchHealthStatus", ctx).Return(statusCh, nil)

			status, err := hm.WatchHealthStatus(ctx, "")

			// Assert
			assert.NoError(t, err)
			workerStatus := <-status
			assert.Equal(t, tt.expectedStatus, workerStatus)
			mockWorker.AssertExpectations(t)
		})
	}
}

func TestInClusterHealthMonitor_WatchHealthStatus_ContextCancellation(t *testing.T) {
	// Setup
	cm := &k8shelpersmock.MockConnectionManager{}

	hm := NewInClusterHealthMonitor(cm, false)
	mockWorker := new(MockHealthMonitorWorker)
	hm.worker = mockWorker

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Action
	statusCh := createMockStatusChannelWithContext(ctx, HealthStatusSuccess)
	mockWorker.On("WatchHealthStatus", mock.Anything).Return(statusCh, nil)

	watchCh, err := hm.WatchHealthStatus(ctx, "")
	assert.NoError(t, err)

	// Receive initial status
	workerStatus := <-watchCh
	assert.Equal(t, HealthStatus("SUCCESS"), workerStatus)

	// Cancel context
	cancel()

	// Wait for the channel to be closed
	timeout := time.After(1 * time.Second)

	// Verify channel is closed
	for {
		select {
		case _, ok := <-watchCh:
			if !ok {
				mockWorker.AssertExpectations(t)
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

	hm := NewDesktopHealthMonitor(cm)
	mockWorker := new(MockHealthMonitorWorker)
	hm.workerCache.Store(kubeContext, mockWorker)

	ctx := context.Background()
	mockWorker.On("ReadyWait", ctx).Return(nil)

	// Action
	err := hm.ReadyWait(ctx, kubeContext)

	// Assert
	assert.NoError(t, err)
	mockWorker.AssertExpectations(t)
}

func TestDesktopHealthMonitor_ReadyWait_WaitSuccess(t *testing.T) {
	// Setup
	cm := &k8shelpersmock.MockConnectionManager{}
	kubeContext := "test-context"

	hm := NewDesktopHealthMonitor(cm)
	mockWorker := new(MockHealthMonitorWorker)
	hm.workerCache.Store(kubeContext, mockWorker)

	ctx := context.Background()
	mockWorker.On("ReadyWait", ctx).Return(nil)

	// Call the health monitor's ReadyWait
	err := hm.ReadyWait(ctx, kubeContext)

	// Assert
	assert.NoError(t, err)
	mockWorker.AssertExpectations(t)
}

func TestDesktopHealthMonitor_ReadyWait_WaitFailure(t *testing.T) {
	// Setup
	cm := &k8shelpersmock.MockConnectionManager{}
	kubeContext := "test-context"

	hm := NewDesktopHealthMonitor(cm)
	mockWorker := new(MockHealthMonitorWorker)
	hm.workerCache.Store(kubeContext, mockWorker)

	// Action
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	expectedErr := fmt.Errorf("context deadline exceeded")
	mockWorker.On("ReadyWait", ctx).Return(expectedErr)

	// Call the health monitor's ReadyWait
	err := hm.ReadyWait(ctx, kubeContext)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
	mockWorker.AssertExpectations(t)
}

func TestInClusterHealthMonitor_ReadyWait_ImmediateSuccess(t *testing.T) {
	// Setup
	cm := &k8shelpersmock.MockConnectionManager{}
	kubeContext := "test-context"
	hm := NewInClusterHealthMonitor(cm, false)
	mockWorker := new(MockHealthMonitorWorker)
	hm.worker = mockWorker

	ctx := context.Background()
	mockWorker.On("ReadyWait", ctx).Return(nil)

	// Action
	err := hm.ReadyWait(ctx, kubeContext)

	// Assert
	assert.NoError(t, err)
	mockWorker.AssertExpectations(t)
}

func TestInClusterHealthMonitor_ReadyWait_WaitSuccess(t *testing.T) {
	// Setup
	cm := &k8shelpersmock.MockConnectionManager{}
	kubeContext := "test-context"
	hm := NewInClusterHealthMonitor(cm, false)
	mockWorker := new(MockHealthMonitorWorker)
	hm.worker = mockWorker

	ctx := context.Background()
	mockWorker.On("ReadyWait", ctx).Return(nil)

	// Action
	err := hm.ReadyWait(ctx, kubeContext)

	// Assert
	assert.NoError(t, err)
	mockWorker.AssertExpectations(t)
}

func TestInClusterHealthMonitor_ReadyWait_WaitFailure(t *testing.T) {
	// Setup
	cm := &k8shelpersmock.MockConnectionManager{}
	kubeContext := "test-context"
	hm := NewInClusterHealthMonitor(cm, false)
	mockWorker := new(MockHealthMonitorWorker)
	hm.worker = mockWorker

	// Action
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	expectedErr := fmt.Errorf("context deadline exceeded")
	mockWorker.On("ReadyWait", ctx).Return(expectedErr)

	// Call the health monitor's ReadyWait
	err := hm.ReadyWait(ctx, kubeContext)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
	mockWorker.AssertExpectations(t)
}
