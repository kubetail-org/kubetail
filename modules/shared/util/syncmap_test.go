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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncMapBasic(t *testing.T) {
	var m SyncMap[string, int]

	// Store and Load
	m.Store("a", 1)
	v, ok := m.Load("a")
	require.True(t, ok)
	assert.Equal(t, 1, v)

	// LoadOrStore existing
	actual, loaded := m.LoadOrStore("a", 2)
	require.True(t, loaded)
	assert.Equal(t, 1, actual)

	// Swap
	prev, loaded := m.Swap("a", 3)
	require.True(t, loaded)
	assert.Equal(t, 1, prev)

	// CompareAndSwap success
	require.True(t, m.CompareAndSwap("a", 3, 4))
	v, _ = m.Load("a")
	assert.Equal(t, 4, v)

	// Range and Delete
	count := 0
	m.Range(func(k string, v int) bool {
		count++
		return true
	})
	assert.Equal(t, 1, count)

	m.Delete("a")
	_, ok = m.Load("a")
	assert.False(t, ok)
}
