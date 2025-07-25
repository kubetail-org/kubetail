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

package util

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncGroupLoadOrCompute(t *testing.T) {
	var g SyncGroup[string, int]

	// Test creating new value
	result, loaded, err := g.LoadOrCompute("key1", func() (int, error) {
		return 42, nil
	})
	require.NoError(t, err)
	assert.False(t, loaded)
	assert.Equal(t, 42, result)

	// Test loading existing value (compute function should not be called)
	computeCalled := false
	result, loaded, err = g.LoadOrCompute("key1", func() (int, error) {
		computeCalled = true
		return 99, nil
	})
	require.NoError(t, err)
	assert.True(t, loaded)
	assert.Equal(t, 42, result)
	assert.False(t, computeCalled)

	// Test error handling
	computeErr := errors.New("compute error")
	_, _, err = g.LoadOrCompute("key2", func() (int, error) {
		return 0, computeErr
	})
	require.ErrorIs(t, err, computeErr)

	// Verify error case didn't store anything
	_, ok := g.m.Load("key2")
	assert.False(t, ok)
}

func TestSyncGroupLoadOrComputeConcurrency(t *testing.T) {
	var g SyncGroup[string, int]
	var computeCount int32

	// Run multiple goroutines trying to create the same key
	const numGoroutines = 100
	var wg sync.WaitGroup
	results := make([]int, numGoroutines)
	errors := make([]error, numGoroutines)

	for i := range numGoroutines {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			result, _, err := g.LoadOrCompute("shared_key", func() (int, error) {
				atomic.AddInt32(&computeCount, 1)
				return 123, nil
			})
			results[index] = result
			errors[index] = err
		}(i)
	}

	wg.Wait()

	// Verify compute function was called exactly once
	require.Equal(t, int32(1), computeCount)

	// Verify all goroutines got the same result
	for i := range numGoroutines {
		require.NoError(t, errors[i], "%d", i)
		require.Equal(t, 123, results[i], "%d", i)
	}

	// Verify the value is stored correctly
	value, ok := g.m.Load("shared_key")
	assert.True(t, ok)
	assert.Equal(t, 123, value)
}

func TestSyncGroupLoadOrComputeWithContext(t *testing.T) {
	var g SyncGroup[string, int]

	t.Run("successful computation", func(t *testing.T) {
		ctx := context.Background()
		result, loaded, err := g.LoadOrComputeWithContext(ctx, "key1", func() (int, error) {
			return 42, nil
		})
		require.NoError(t, err)
		assert.False(t, loaded)
		assert.Equal(t, 42, result)
	})

	t.Run("load existing value", func(t *testing.T) {
		ctx := context.Background()
		computeCalled := false
		result, loaded, err := g.LoadOrComputeWithContext(ctx, "key1", func() (int, error) {
			computeCalled = true
			return 99, nil
		})
		require.NoError(t, err)
		assert.True(t, loaded)
		assert.Equal(t, 42, result)
		assert.False(t, computeCalled)
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// Cancel context immediately
		cancel()

		result, loaded, err := g.LoadOrComputeWithContext(ctx, "key2", func() (int, error) {
			time.Sleep(100 * time.Millisecond)
			return 99, nil
		})

		require.ErrorIs(t, err, context.Canceled)
		assert.False(t, loaded)
		assert.Equal(t, 0, result)
	})

	t.Run("context timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		result, loaded, err := g.LoadOrComputeWithContext(ctx, "key3", func() (int, error) {
			time.Sleep(50 * time.Millisecond)
			return 99, nil
		})

		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.False(t, loaded)
		assert.Equal(t, 0, result)
	})

	t.Run("compute error", func(t *testing.T) {
		ctx := context.Background()
		_, loaded, err := g.LoadOrComputeWithContext(ctx, "key4", func() (int, error) {
			return 0, errors.New("compute error")
		})
		require.Error(t, err)
		assert.False(t, loaded)
		assert.Equal(t, "compute error", err.Error())
	})

	t.Run("concurrent access", func(t *testing.T) {
		var m2 SyncGroup[string, int]
		var computeCount int32
		const numGoroutines = 50

		ctx := context.Background()
		var wg sync.WaitGroup
		results := make([]int, numGoroutines)
		errors := make([]error, numGoroutines)

		for i := range numGoroutines {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				result, _, err := m2.LoadOrComputeWithContext(ctx, "shared_key", func() (int, error) {
					atomic.AddInt32(&computeCount, 1)
					return 123, nil
				})
				results[index] = result
				errors[index] = err
			}(i)
		}

		wg.Wait()

		// Verify compute function was called exactly once
		require.Equal(t, int32(1), computeCount)

		// Verify all goroutines got the same result
		for i := range numGoroutines {
			require.NoError(t, errors[i], "goroutine %d", i)
			require.Equal(t, 123, results[i], "goroutine %d", i)
		}
	})
}
