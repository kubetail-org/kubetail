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
)

func TestSyncMapBasic(t *testing.T) {
	var m SyncMap[string, int]

	// Store and Load
	m.Store("a", 1)
	v, ok := m.Load("a")
	if !ok || v != 1 {
		t.Fatalf("expected value 1, got %v ok=%v", v, ok)
	}

	// LoadOrStore existing
	actual, loaded := m.LoadOrStore("a", 2)
	if !loaded || actual != 1 {
		t.Fatalf("LoadOrStore did not return existing value")
	}

	// Swap
	prev, loaded := m.Swap("a", 3)
	if !loaded || prev != 1 {
		t.Fatalf("Swap expected 1 got %v", prev)
	}

	// CompareAndSwap success
	if !m.CompareAndSwap("a", 3, 4) {
		t.Fatalf("CompareAndSwap should succeed")
	}
	v, _ = m.Load("a")
	if v != 4 {
		t.Fatalf("expected 4 got %v", v)
	}

	// Range and Delete
	count := 0
	m.Range(func(k string, v int) bool {
		count++
		return true
	})
	if count != 1 {
		t.Fatalf("expected range count 1 got %d", count)
	}

	m.Delete("a")
	_, ok = m.Load("a")
	if ok {
		t.Fatalf("expected key deleted")
	}
}

func TestSyncMapLoadOrCompute(t *testing.T) {
	var m SyncMap[string, int]

	// Test creating new value
	result, err := m.LoadOrCompute("key1", func() (int, error) {
		return 42, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 42 {
		t.Fatalf("expected 42, got %d", result)
	}

	// Test loading existing value (compute function should not be called)
	computeCalled := false
	result, err = m.LoadOrCompute("key1", func() (int, error) {
		computeCalled = true
		return 99, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 42 {
		t.Fatalf("expected 42, got %d", result)
	}
	if computeCalled {
		t.Fatalf("compute function should not be called for existing key")
	}

	// Test error handling
	_, err = m.LoadOrCompute("key2", func() (int, error) {
		return 0, errors.New("compute error")
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if err.Error() != "compute error" {
		t.Fatalf("expected 'compute error', got '%s'", err.Error())
	}

	// Verify error case didn't store anything
	_, ok := m.Load("key2")
	if ok {
		t.Fatalf("expected key2 to not exist after error")
	}
}

func TestSyncMapLoadOrComputeConcurrency(t *testing.T) {
	var m SyncMap[string, int]
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
			result, err := m.LoadOrCompute("shared_key", func() (int, error) {
				atomic.AddInt32(&computeCount, 1)
				return 123, nil
			})
			results[index] = result
			errors[index] = err
		}(i)
	}

	wg.Wait()

	// Verify compute function was called exactly once
	if computeCount != 1 {
		t.Fatalf("expected compute function to be called once, got %d", computeCount)
	}

	// Verify all goroutines got the same result
	for i := range numGoroutines {
		if errors[i] != nil {
			t.Fatalf("goroutine %d got error: %v", i, errors[i])
		}
		if results[i] != 123 {
			t.Fatalf("goroutine %d got result %d, expected 123", i, results[i])
		}
	}

	// Verify the value is stored correctly
	value, ok := m.Load("shared_key")
	if !ok {
		t.Fatalf("expected shared_key to exist")
	}
	if value != 123 {
		t.Fatalf("expected 123, got %d", value)
	}
}

func TestSyncMapLoadOrComputeWithContext(t *testing.T) {
	var m SyncMap[string, int]

	t.Run("successful computation", func(t *testing.T) {
		ctx := context.Background()
		result, err := m.LoadOrComputeWithContext(ctx, "key1", func() (int, error) {
			return 42, nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != 42 {
			t.Fatalf("expected 42, got %d", result)
		}
	})

	t.Run("load existing value", func(t *testing.T) {
		ctx := context.Background()
		computeCalled := false
		result, err := m.LoadOrComputeWithContext(ctx, "key1", func() (int, error) {
			computeCalled = true
			return 99, nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != 42 {
			t.Fatalf("expected 42, got %d", result)
		}
		if computeCalled {
			t.Fatalf("compute function should not be called for existing key")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// Cancel context immediately
		cancel()

		result, err := m.LoadOrComputeWithContext(ctx, "key2", func() (int, error) {
			time.Sleep(100 * time.Millisecond)
			return 99, nil
		})

		if err != context.Canceled {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if result != 0 {
			t.Fatalf("expected zero value, got %d", result)
		}
	})

	t.Run("context timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		result, err := m.LoadOrComputeWithContext(ctx, "key3", func() (int, error) {
			time.Sleep(50 * time.Millisecond)
			return 99, nil
		})

		if err != context.DeadlineExceeded {
			t.Fatalf("expected context.DeadlineExceeded, got %v", err)
		}
		if result != 0 {
			t.Fatalf("expected zero value, got %d", result)
		}
	})

	t.Run("compute error", func(t *testing.T) {
		ctx := context.Background()
		_, err := m.LoadOrComputeWithContext(ctx, "key4", func() (int, error) {
			return 0, errors.New("compute error")
		})
		if err == nil {
			t.Fatalf("expected error")
		}
		if err.Error() != "compute error" {
			t.Fatalf("expected 'compute error', got '%s'", err.Error())
		}
	})

	t.Run("concurrent access", func(t *testing.T) {
		var m2 SyncMap[string, int]
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
				result, err := m2.LoadOrComputeWithContext(ctx, "shared_key", func() (int, error) {
					atomic.AddInt32(&computeCount, 1)
					return 123, nil
				})
				results[index] = result
				errors[index] = err
			}(i)
		}

		wg.Wait()

		// Verify compute function was called exactly once
		if computeCount != 1 {
			t.Fatalf("expected compute function to be called once, got %d", computeCount)
		}

		// Verify all goroutines got the same result
		for i := range numGoroutines {
			if errors[i] != nil {
				t.Fatalf("goroutine %d got error: %v", i, errors[i])
			}
			if results[i] != 123 {
				t.Fatalf("goroutine %d got result %d, expected 123", i, results[i])
			}
		}
	})
}
