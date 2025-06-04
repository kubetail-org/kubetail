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

package syncmap

import "sync"

// Map is a typed wrapper around sync.Map using generics.
type Map[K comparable, V any] struct {
	m sync.Map
}

// Load returns the value stored for key.
func (m *Map[K, V]) Load(key K) (V, bool) {
	if v, ok := m.m.Load(key); ok {
		return v.(V), true
	}
	var zero V
	return zero, false
}

// Store sets the value for key.
func (m *Map[K, V]) Store(key K, value V) {
	m.m.Store(key, value)
}

// Delete removes the value for key.
func (m *Map[K, V]) Delete(key K) {
	m.m.Delete(key)
}

// Range calls f sequentially for each key and value present in the map.
func (m *Map[K, V]) Range(f func(key K, value V) bool) {
	m.m.Range(func(k, v any) bool {
		return f(k.(K), v.(V))
	})
}
