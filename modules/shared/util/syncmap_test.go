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

import "testing"

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
