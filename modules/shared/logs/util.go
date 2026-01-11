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

package logs

import (
	"sync"

	set "github.com/deckarep/golang-set/v2"
)

// MapSet is a generic structure that maps keys to sets of values
type MapSet[K comparable, T comparable] struct {
	data map[K]set.Set[T]
}

// NewMapSet initializes a new MapSet
func NewMapSet[K comparable, T comparable]() MapSet[K, T] {
	return MapSet[K, T]{data: make(map[K]set.Set[T])}
}

// Add inserts a value into the set associated with the key
func (ms *MapSet[K, T]) Add(key K, value T) {
	if _, exists := ms.data[key]; !exists {
		ms.data[key] = set.NewSet[T]()
	}
	ms.data[key].Add(value)
}

// Remove removes a value from the set associated with the key
func (ms *MapSet[K, T]) Remove(key K, value T) {
	if _, exists := ms.data[key]; !exists {
		return
	}
	ms.data[key].Remove(value)
}

// Append inserts variadic values into the set associated with the key
func (ms *MapSet[K, T]) Append(key K, values ...T) {
	if _, exists := ms.data[key]; !exists {
		ms.data[key] = set.NewSet[T]()
	}
	ms.data[key].Append(values...)
}

// Get retrieves the set of values associated with a key
func (ms *MapSet[K, T]) Get(key K) (set.Set[T], bool) {
	val, exists := ms.data[key]
	return val, exists
}

// Contains returns boolean if val exists at key
func (ms *MapSet[K, T]) ContainsOne(key K, val T) bool {
	s, exists := ms.data[key]
	if !exists {
		return false
	}
	return s.ContainsOne(val)
}

// Iterate over each value in set at key
func (ms *MapSet[K, T]) Each(key K, fn func(T) bool) {
	s, exists := ms.data[key]
	if !exists {
		return
	}
	s.Each(fn)
}

// ThreadSafeSlice is a thread-safe wrapper around a slice.
type ThreadSafeSlice[T any] struct {
	mu    sync.RWMutex
	slice []T
}

// Add appends an element to the slice.
func (ts *ThreadSafeSlice[T]) Add(item T) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.slice = append(ts.slice, item)
}

// ToSlice returns a copy of the data as a slice.
func (ts *ThreadSafeSlice[T]) ToSlice() []T {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	// Return a copy to avoid external modification.
	copied := make([]T, len(ts.slice))
	copy(copied, ts.slice)
	return copied
}

// Range over slice elements. Return false to stop iteration.
func (ts *ThreadSafeSlice[T]) Range(fn func(int, T) bool) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	for i, v := range ts.slice {
		if !fn(i, v) {
			break
		}
	}
}

// Len returns the length of the slice.
func (ts *ThreadSafeSlice[T]) Len() int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return len(ts.slice)
}

// recordWithSource wraps a LogRecord and also keeps track of which stream
// channel it came from, so when we pop from the heap we know where
// to pull the next entry.
type recordWithSource struct {
	record LogRecord
	srcCh  <-chan LogRecord
}

// priorityQueue implements heap.Interface. The "less" comparison
// is based on the LogEntry's timestamp.
type priorityQueue []recordWithSource

// Len() implements sort.Interface (embedded in heap.Interface)
func (pq priorityQueue) Len() int { return len(pq) }

// Less() implements sort.Interface (embedded in heap.Interface)
func (pq priorityQueue) Less(i, j int) bool {
	return pq[i].record.Timestamp.Before(pq[j].record.Timestamp)
}

// Swap() implements sort.Interface (embedded in heap.Interface)
func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

// Push() implements heap.Interface
func (pq *priorityQueue) Push(x any) {
	*pq = append(*pq, x.(recordWithSource))
}

// Pop() implements heap.Interface
func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}

// reversePriorityQueue wraps priorityQueue but reverses the comparison order
type reversePriorityQueue []recordWithSource

// Len() implements sort.Interface (embedded in heap.Interface)
func (pq reversePriorityQueue) Len() int { return len(pq) }

// Less() implements sort.Interface (embedded in heap.Interface)
func (pq reversePriorityQueue) Less(i, j int) bool {
	// Reverse the comparison
	return pq[j].record.Timestamp.Before(pq[i].record.Timestamp)
}

// Swap() implements sort.Interface (embedded in heap.Interface)
func (pq reversePriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

// Push() implements heap.Interface
func (pq *reversePriorityQueue) Push(x any) {
	*pq = append(*pq, x.(recordWithSource))
}

// Pop() implements heap.Interface
func (pq *reversePriorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}

type priorityQueueInterface interface {
	Len() int
	Less(i, j int) bool
	Swap(i, j int)
	Push(x any)
	Pop() any
}

func newPriorityQueue(reverse bool) priorityQueueInterface {
	if reverse {
		pq := make(reversePriorityQueue, 0)
		return &pq
	} else {
		pq := make(priorityQueue, 0)
		return &pq
	}
}
