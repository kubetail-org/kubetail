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
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"k8s.io/client-go/kubernetes/fake"
)

func NewTestConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		conns:     make(map[string]ClientConnInterface),
		clientset: fake.NewSimpleClientset(),
	}
}

// Mock for ClientConn
type ClientConnMock struct {
	mock.Mock
	*grpc.ClientConn
}

func (m *ClientConnMock) Close() error {
	args := m.Called()
	return args.Error(0)
}

func TestConnectionManagerGet(t *testing.T) {
	// init
	cm := NewTestConnectionManager()

	// populate
	conn1 := new(ClientConnMock)
	cm.add("node1", conn1)

	// check that get returns connection for given node
	assert.Equal(t, conn1, cm.Get("node1"))

	// check that missing node returns nil
	assert.Nil(t, cm.Get("node2"))
}

func TestConnectionManagerGetAll(t *testing.T) {
	// init and populate
	cm := NewTestConnectionManager()
	conn1 := new(ClientConnMock)
	conn2 := new(ClientConnMock)
	cm.add("node1", conn1)
	cm.add("node2", conn2)

	// check map
	m := cm.GetAll()
	require.Equal(t, 2, len(m))
	assert.Equal(t, m["node1"], conn1)
	assert.Equal(t, m["node2"], conn2)
}

func TestConnectionManagerStart(t *testing.T) {
	t.Run("sets isRunning to true", func(t *testing.T) {
		cm := NewTestConnectionManager()
		defer cm.Teardown()
		require.False(t, cm.isRunning)
		cm.Start(context.Background())
		require.True(t, cm.isRunning)
	})

	t.Run("initializes stopCh", func(t *testing.T) {
		cm := NewTestConnectionManager()
		defer cm.Teardown()
		require.Nil(t, cm.stopCh)
		cm.Start(context.Background())
		require.NotNil(t, cm.stopCh)
	})

	t.Run("doesnt run if isRunning is true", func(t *testing.T) {
		cm := NewTestConnectionManager()
		defer cm.Teardown()
		cm.Start(context.Background())
		stopCh := cm.stopCh
		cm.Start(context.Background())
		require.Equal(t, stopCh, cm.stopCh)
	})

	t.Run("calls stop on context cancel", func(t *testing.T) {
		cm := NewTestConnectionManager()
		defer cm.Teardown()

		ctx, cancel := context.WithCancel(context.Background())
		cm.Start(ctx)

		var wg sync.WaitGroup

		// check that stopCh is closed
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok := <-cm.stopCh
			require.False(t, ok)
		}()

		// before
		require.True(t, cm.isRunning)

		cancel()
		wg.Wait()

		// after
		cm.mu.Lock()
		require.False(t, cm.isRunning)
		cm.mu.Unlock()
	})
}

func TestConnectionManagerTeardown(t *testing.T) {
	t.Run("sets isRunning to false", func(t *testing.T) {
		cm := NewTestConnectionManager()
		cm.Start(context.Background())

		require.True(t, cm.isRunning)
		cm.Teardown()
		require.False(t, cm.isRunning)
	})

	t.Run("closes stopCh", func(t *testing.T) {
		cm := NewTestConnectionManager()
		cm.Start(context.Background())

		var wg sync.WaitGroup

		// check that stopCh is closed
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok := <-cm.stopCh
			require.False(t, ok)
		}()

		require.NotNil(t, cm.stopCh)
		cm.Teardown()
		wg.Wait()
	})

	t.Run("calls close on grpc conns", func(t *testing.T) {
		cm := NewTestConnectionManager()
		cm.Start(context.Background())

		// add conns
		conn1 := new(ClientConnMock)
		conn1.On("Close").Return(nil)
		cm.add("node1", conn1)

		conn2 := new(ClientConnMock)
		conn2.On("Close").Return(nil)
		cm.add("node2", conn2)

		// teardown and check function calls
		cm.Teardown()
		conn1.AssertNumberOfCalls(t, "Close", 1)
		conn2.AssertNumberOfCalls(t, "Close", 1)
	})

	t.Run("resets conns map", func(t *testing.T) {
		cm := NewTestConnectionManager()
		cm.Start(context.Background())

		// add conns
		conn1 := new(ClientConnMock)
		conn1.On("Close").Return(nil)
		cm.add("node1", conn1)

		conn2 := new(ClientConnMock)
		conn2.On("Close").Return(nil)
		cm.add("node2", conn2)

		// teardown and check conns
		require.Equal(t, 2, len(cm.conns))
		cm.Teardown()
		require.Equal(t, 0, len(cm.conns))
	})

	t.Run("doesnt run if isRunning is false", func(t *testing.T) {
		cm := NewTestConnectionManager()

		// add conns
		conn1 := new(ClientConnMock)
		conn1.On("Close").Return(nil)
		cm.add("node1", conn1)

		// teardown and check conns
		require.Equal(t, 1, len(cm.conns))
		cm.Teardown()
		require.Equal(t, 1, len(cm.conns))
	})
}

func TestConnectionManagerAdd(t *testing.T) {
	cm := NewTestConnectionManager()

	// add connection and check that get returns it
	conn1 := new(ClientConnMock)
	cm.add("node1", conn1)
	assert.Equal(t, conn1, cm.Get("node1"))

	// add new connection and check that get returns that one
	conn2 := new(ClientConnMock)
	cm.add("node1", conn2)
	assert.Equal(t, conn2, cm.Get("node1"))
}

func TestConnectionManagerRemove(t *testing.T) {
	cm := NewTestConnectionManager()

	// add connection and check that get returns it
	conn1 := new(ClientConnMock)
	conn1.On("Close").Return(nil)
	cm.add("node1", conn1)
	assert.Equal(t, conn1, cm.Get("node1"))

	// remove and check again
	cm.remove("node1")
	conn1.On("Close").Return(nil)
	assert.Nil(t, cm.Get("node1"))
}
