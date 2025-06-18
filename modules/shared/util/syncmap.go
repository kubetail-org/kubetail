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

import "sync"

// SyncMap is a typed wrapper around sync.Map.
// The zero value is ready for use.
type SyncMap[K comparable, V any] struct {
	m sync.Map
}

// Load returns the value stored in the map for a key, or zero value if none.
// The ok result indicates whether value was found in the map.
func (m *SyncMap[K, V]) Load(key K) (V, bool) {
	v, ok := m.m.Load(key)
	if !ok {
		var zero V
		return zero, false
	}
	return v.(V), true
}

// Store sets the value for a key.
func (m *SyncMap[K, V]) Store(key K, value V) {
	m.m.Store(key, value)
}

// LoadOrStore returns the existing value for the key if present.
// Otherwise, it stores and returns the given value.
func (m *SyncMap[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	v, loaded := m.m.LoadOrStore(key, value)
	if loaded {
		return v.(V), true
	}
	return value, false
}

// LoadAndDelete deletes the value for a key, returning the previous value if any.
func (m *SyncMap[K, V]) LoadAndDelete(key K) (V, bool) {
	v, loaded := m.m.LoadAndDelete(key)
	if !loaded {
		var zero V
		return zero, false
	}
	return v.(V), true
}

// Delete deletes the value for a key.
func (m *SyncMap[K, V]) Delete(key K) {
	m.m.Delete(key)
}

// Swap swaps the value for a key and returns the previous value if any.
func (m *SyncMap[K, V]) Swap(key K, value V) (V, bool) {
	v, loaded := m.m.Swap(key, value)
	if !loaded {
		var zero V
		return zero, false
	}
	return v.(V), true
}

// CompareAndSwap swaps the old and new values if the value stored for the key equals old.
func (m *SyncMap[K, V]) CompareAndSwap(key K, old, new V) bool {
	return m.m.CompareAndSwap(key, old, new)
}

// Range calls f sequentially for each key and value present in the map.
// If f returns false, range stops the iteration.
func (m *SyncMap[K, V]) Range(f func(key K, value V) bool) {
	m.m.Range(func(k, v any) bool {
		return f(k.(K), v.(V))
	})
}
