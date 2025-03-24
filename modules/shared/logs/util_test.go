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

package logs

import (
	"container/heap"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMapSet_NewMapSet(t *testing.T) {
	ms := NewMapSet[string, int]()
	assert.NotNil(t, ms)
}

func TestMapSet_Add(t *testing.T) {
	// Initialize a new MapSet
	ms := NewMapSet[string, int]()

	// Test adding a value to a non-existing key
	ms.Add("key1", 1)
	assert.True(t, ms.ContainsOne("key1", 1))

	// Test adding another value to the same key
	ms.Add("key1", 2)
	assert.True(t, ms.ContainsOne("key1", 1))
	assert.True(t, ms.ContainsOne("key1", 2))

	// Test adding a duplicate value (should be a no-op for sets)
	ms.Add("key1", 1)
	assert.True(t, ms.ContainsOne("key1", 1))

	// Test adding a value to a different key
	ms.Add("key2", 3)
	assert.True(t, ms.ContainsOne("key2", 3))
	assert.False(t, ms.ContainsOne("key2", 1))
}

func TestMapSet_Remove(t *testing.T) {
	// Initialize a new MapSet
	ms := NewMapSet[string, int]()

	// Test removing a value from a non-existing key
	ms.Remove("key1", 1)

	// Test removing a value from an existing key
	ms.Add("key1", 1)
	ms.Remove("key1", 1)
	assert.False(t, ms.ContainsOne("key1", 1))

	// Test removing one value of several
	ms.Add("key1", 1)
	ms.Add("key1", 2)
	ms.Remove("key1", 1)
	assert.False(t, ms.ContainsOne("key1", 1))
	assert.True(t, ms.ContainsOne("key1", 2))
}

func TestMapSet_Append(t *testing.T) {
	// Initialize a new MapSet
	ms := NewMapSet[string, int]()

	// Test appending multiple values to a non-existing key
	ms.Append("key1", 1, 2, 3)
	assert.True(t, ms.ContainsOne("key1", 1))
	assert.True(t, ms.ContainsOne("key1", 2))
	assert.True(t, ms.ContainsOne("key1", 3))

	// Test appending more values to an existing key
	ms.Append("key1", 4, 5)
	assert.True(t, ms.ContainsOne("key1", 1))
	assert.True(t, ms.ContainsOne("key1", 2))
	assert.True(t, ms.ContainsOne("key1", 3))
	assert.True(t, ms.ContainsOne("key1", 4))
	assert.True(t, ms.ContainsOne("key1", 5))

	// Test appending duplicate values
	ms.Append("key1", 3, 4, 6)
	assert.True(t, ms.ContainsOne("key1", 1))
	assert.True(t, ms.ContainsOne("key1", 2))
	assert.True(t, ms.ContainsOne("key1", 3))
	assert.True(t, ms.ContainsOne("key1", 4))
	assert.True(t, ms.ContainsOne("key1", 5))
	assert.True(t, ms.ContainsOne("key1", 6))

	// Test appending no values (should be a no-op)
	ms.Append("key1")
	assert.True(t, ms.ContainsOne("key1", 1))
}

func TestMapSet_Get(t *testing.T) {
	// Initialize a new MapSet
	ms := NewMapSet[string, int]()

	// Test getting a non-existing key
	_, exists := ms.Get("nonexistent")
	assert.False(t, exists)

	// Add some values and test getting them
	ms.Add("key1", 1)
	ms.Add("key1", 2)
	ms.Add("key2", 3)

	// Test getting an existing key
	set1, exists := ms.Get("key1")
	assert.True(t, exists)

	// Verify the set contains the expected values
	values := set1.ToSlice()
	sort.Ints(values)
	assert.Equal(t, []int{1, 2}, values)

	// Test getting another existing key
	set2, exists := ms.Get("key2")
	assert.True(t, exists)

	values = set2.ToSlice()
	sort.Ints(values)
	assert.Equal(t, []int{3}, values)
}

func TestMapSet_ContainsOne(t *testing.T) {
	// Initialize a new MapSet
	ms := NewMapSet[string, int]()

	// Test containment on a non-existing key
	assert.False(t, ms.ContainsOne("nonexistent", 1))

	// Add some values
	ms.Add("key1", 1)
	ms.Add("key1", 2)
	ms.Add("key2", 3)

	// Test containment on existing keys and values
	assert.True(t, ms.ContainsOne("key1", 1))
	assert.True(t, ms.ContainsOne("key1", 2))
	assert.False(t, ms.ContainsOne("key1", 3))

	assert.True(t, ms.ContainsOne("key2", 3))
	assert.False(t, ms.ContainsOne("key2", 1))
}

func TestMapSet_Each(t *testing.T) {
	// Initialize a new MapSet
	ms := NewMapSet[string, int]()

	// Test Each on a non-existing key
	count := 0
	ms.Each("nonexistent", func(val int) bool {
		count++
		return false // continue iteration
	})
	assert.Equal(t, 0, count) // Should not iterate over anything

	// Add some values
	ms.Append("key1", 1, 2, 3)

	// Test Each with a function that always continues
	values := make(map[int]bool)
	ms.Each("key1", func(val int) bool {
		values[val] = true
		return false // Continue iteration
	})
	assert.Equal(t, 3, len(values))
	assert.True(t, values[1])
	assert.True(t, values[2])
	assert.True(t, values[3])

	// Test Each with early termination
	count = 0
	foundValues := make([]int, 0, 1)
	ms.Each("key1", func(val int) bool {
		count++
		foundValues = append(foundValues, val)
		return true // Stop iteration after first element
	})
	assert.Equal(t, 1, count) // Should only process one element
	assert.Equal(t, 1, len(foundValues))
	assert.Contains(t, []int{1, 2, 3}, foundValues[0]) // The value should be one of the values in the set
}

func TestPriorityQueue_Len(t *testing.T) {
	// Initialize an empty queue
	pq := priorityQueue{}
	assert.Equal(t, 0, pq.Len())

	// Add an item and check length
	pq = append(pq, recordWithSource{
		record: LogRecord{
			Timestamp: time.Now(),
		},
	})
	assert.Equal(t, 1, pq.Len())

	// Add another item and check length
	pq = append(pq, recordWithSource{
		record: LogRecord{
			Timestamp: time.Now(),
		},
	})
	assert.Equal(t, 2, pq.Len())
}

func TestPriorityQueue_Less(t *testing.T) {
	// Create timestamps with known ordering
	now := time.Now()
	earlier := now.Add(-1 * time.Hour)
	later := now.Add(1 * time.Hour)

	// Create a queue with records having different timestamps
	pq := priorityQueue{
		{record: LogRecord{Timestamp: now}},
		{record: LogRecord{Timestamp: earlier}},
		{record: LogRecord{Timestamp: later}},
	}

	// Test Less function - earlier should be less than now
	assert.True(t, pq.Less(1, 0), "Earlier timestamp should be less than now")

	// Test Less function - now should be less than later
	assert.True(t, pq.Less(0, 2), "Now should be less than later timestamp")

	// Test Less function - earlier should be less than later
	assert.True(t, pq.Less(1, 2), "Earlier timestamp should be less than later")

	// Test Less function - later should not be less than now
	assert.False(t, pq.Less(2, 0), "Later timestamp should not be less than now")
}

func TestPriorityQueue_Swap(t *testing.T) {
	// Create records with distinct timestamps
	now := time.Now()
	earlier := now.Add(-1 * time.Hour)

	// Create a queue with two records
	pq := priorityQueue{
		{record: LogRecord{Timestamp: now, Message: "now"}},
		{record: LogRecord{Timestamp: earlier, Message: "earlier"}},
	}

	// Verify initial state
	assert.Equal(t, "now", pq[0].record.Message)
	assert.Equal(t, "earlier", pq[1].record.Message)

	// Swap elements
	pq.Swap(0, 1)

	// Verify elements were swapped
	assert.Equal(t, "earlier", pq[0].record.Message)
	assert.Equal(t, "now", pq[1].record.Message)
}

func TestPriorityQueue_Push(t *testing.T) {
	// Initialize an empty queue
	pq := priorityQueue{}

	// Create a record to push
	record := recordWithSource{
		record: LogRecord{
			Timestamp: time.Now(),
			Message:   "test message",
		},
	}

	// Push the record onto the queue
	pq.Push(record)

	// Verify the record was added
	assert.Equal(t, 1, pq.Len())
	assert.Equal(t, "test message", pq[0].record.Message)
}

func TestPriorityQueue_Pop(t *testing.T) {
	// Initialize a queue with two records
	now := time.Now()
	earlier := now.Add(-1 * time.Hour)

	pq := priorityQueue{
		{record: LogRecord{Timestamp: now, Message: "now"}},
		{record: LogRecord{Timestamp: earlier, Message: "earlier"}},
	}

	// Pop an item from the queue
	item := pq.Pop().(recordWithSource)

	// Verify the item was removed and returned
	assert.Equal(t, 1, pq.Len())
	assert.Equal(t, "earlier", item.record.Message)
}

func TestPriorityQueue_HeapOperations(t *testing.T) {
	// Create timestamps with known ordering
	now := time.Now()
	t1 := now.Add(-3 * time.Hour) // earliest
	t2 := now.Add(-2 * time.Hour)
	t3 := now.Add(-1 * time.Hour)
	t4 := now // latest

	// Create a priority queue and initialize it as a heap
	pq := &priorityQueue{}
	heap.Init(pq)

	// Push items in non-chronological order
	heap.Push(pq, recordWithSource{record: LogRecord{Timestamp: t3, Message: "t3"}})
	heap.Push(pq, recordWithSource{record: LogRecord{Timestamp: t1, Message: "t1"}})
	heap.Push(pq, recordWithSource{record: LogRecord{Timestamp: t4, Message: "t4"}})
	heap.Push(pq, recordWithSource{record: LogRecord{Timestamp: t2, Message: "t2"}})

	// Pop items and verify they come out in chronological order (earliest first)
	item1 := heap.Pop(pq).(recordWithSource)
	item2 := heap.Pop(pq).(recordWithSource)
	item3 := heap.Pop(pq).(recordWithSource)
	item4 := heap.Pop(pq).(recordWithSource)

	assert.Equal(t, "t1", item1.record.Message, "First item should be t1 (earliest)")
	assert.Equal(t, "t2", item2.record.Message, "Second item should be t2")
	assert.Equal(t, "t3", item3.record.Message, "Third item should be t3")
	assert.Equal(t, "t4", item4.record.Message, "Fourth item should be t4 (latest)")

	// Verify the queue is now empty
	assert.Equal(t, 0, pq.Len())
}

func TestReversePriorityQueue_HeapOperations(t *testing.T) {
	// Create timestamps with known ordering
	now := time.Now()
	t1 := now.Add(-3 * time.Hour) // earliest
	t2 := now.Add(-2 * time.Hour)
	t3 := now.Add(-1 * time.Hour)
	t4 := now // latest

	// Create a reverse priority queue and initialize it as a heap
	pq := &reversePriorityQueue{}
	heap.Init(pq)

	// Push items in non-chronological order
	heap.Push(pq, recordWithSource{record: LogRecord{Timestamp: t3, Message: "t3"}})
	heap.Push(pq, recordWithSource{record: LogRecord{Timestamp: t1, Message: "t1"}})
	heap.Push(pq, recordWithSource{record: LogRecord{Timestamp: t4, Message: "t4"}})
	heap.Push(pq, recordWithSource{record: LogRecord{Timestamp: t2, Message: "t2"}})

	// Pop items and verify they come out in reverse chronological order (latest first)
	item1 := heap.Pop(pq).(recordWithSource)
	item2 := heap.Pop(pq).(recordWithSource)
	item3 := heap.Pop(pq).(recordWithSource)
	item4 := heap.Pop(pq).(recordWithSource)

	assert.Equal(t, "t4", item1.record.Message, "First item should be t4 (latest)")
	assert.Equal(t, "t3", item2.record.Message, "Second item should be t3")
	assert.Equal(t, "t2", item3.record.Message, "Third item should be t2")
	assert.Equal(t, "t1", item4.record.Message, "Fourth item should be t1 (earliest)")

	// Verify the queue is now empty
	assert.Equal(t, 0, pq.Len())
}

func TestThreadSafeSlice_ToSlice(t *testing.T) {
	// Initialize a ThreadSafeSlice
	ts := ThreadSafeSlice[int]{}

	// Test ToSlice on an empty slice
	result := ts.ToSlice()
	assert.Equal(t, 0, len(result), "Expected empty slice")

	// Add some elements
	ts.Add(1)
	ts.Add(2)
	ts.Add(3)

	// Test ToSlice after adding elements
	result = ts.ToSlice()
	assert.Equal(t, 3, len(result), "Expected slice length to be 3")
	assert.ElementsMatch(t, []int{1, 2, 3}, result, "Expected slice to contain [1, 2, 3]")

	// Ensure ToSlice returns a copy by modifying the original slice
	ts.Add(4)
	result = ts.ToSlice()
	assert.Equal(t, 4, len(result), "Expected slice length to be 4 after adding an element")
	assert.ElementsMatch(t, []int{1, 2, 3, 4}, result, "Expected slice to contain [1, 2, 3, 4]")
}
