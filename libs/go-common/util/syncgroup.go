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

package util

import (
	"context"
	"sync"
)

// SyncGroup is a generic memoization library that uses our implementation
// of SyncMap under the hood

type SyncGroup[K comparable, V any] struct {
	m     SyncMap[K, V]
	muMap SyncMap[K, *sync.Mutex]
}

// Expose SyncMap Range function
func (g *SyncGroup[K, V]) Range(f func(key K, value V) bool) {
	g.m.Range(f)
}

// LoadOrCompute returns the existing value for the key if present. Otherwise, it calls the
// compute function and stores the result. Subsequent callers will wait until the result is
// ready. The compute function is only called once per key, even under concurrent access.
func (g *SyncGroup[K, V]) LoadOrCompute(key K, compute func() (V, error)) (V, bool, error) {
	mu, _ := g.muMap.LoadOrStore(key, &sync.Mutex{})
	mu.Lock()
	defer mu.Unlock()

	// Fast path: check if value already exists
	if v, loaded := g.m.Load(key); loaded {
		return v, true, nil
	}

	// Create and store the value
	value, err := compute()
	if err != nil {
		return value, false, err
	}

	g.m.Store(key, value)
	return value, false, nil
}

// LoadOrComputeWithContext is a context-aware version of LoadOrComputer. If the context is
// cancelled during computation, it returns the context error.
func (g *SyncGroup[K, V]) LoadOrComputeWithContext(ctx context.Context, key K, compute func() (V, error)) (V, bool, error) {
	// Exit early if context already canceled
	if err := ctx.Err(); err != nil {
		var zero V
		return zero, false, err
	}

	type result struct {
		value  V
		loaded bool
		err    error
	}

	resultCh := make(chan result, 1)

	// Execute inner LoadOrCompute() inside goroutine
	go func() {
		v, loaded, err := g.LoadOrCompute(key, compute)
		resultCh <- result{v, loaded, err}
	}()

	select {
	case <-ctx.Done():
		var zero V
		return zero, false, ctx.Err()
	case res := <-resultCh:
		return res.value, res.loaded, res.err
	}
}
