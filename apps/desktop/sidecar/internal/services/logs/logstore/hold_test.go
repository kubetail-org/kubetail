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

package logstore

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A hold is counted: the row stays held until every holder lets go, and a release
// called twice lets go once.
func TestAHoldIsCounted(t *testing.T) {
	t.Parallel()
	c := newClock(100_000)
	s := newClockedStore(t, sweepRetention(), c)
	s.m.SetPodsGone(goneAre("uid-gone"))
	gone := podStream(t, s, "gone", "uid-gone")
	first, second := s.Hold(gone), s.Hold(gone)

	first()
	first()
	sweep(t, s)
	assert.Equal(t, []int64{gone}, idsIn(t, s, "streams"))

	second()
	sweep(t, s)
	assert.Empty(t, idsIn(t, s, "streams"))
}

// A hold of rows or chunks on a closed claim holds nothing, and its release does
// nothing.
func TestAHoldOnAClosedClaimHoldsNothing(t *testing.T) {
	t.Parallel()
	m, _ := newTestManager(t)
	s := open(t, m, 1)
	require.NoError(t, s.Close())

	s.Hold(1)()
	s.HoldChunks(1)()
}

// An eviction runs under the holds' lock, so a hold taken meanwhile waits for it and
// the chunk goes before its hold or not at all; a held chunk is left alone.
func TestAnEvictionHoldsOffAHold(t *testing.T) {
	t.Parallel()
	h := holds{n: map[int64]int{}}
	require.NoError(t, h.unlessHeld(1, func() error {
		assert.False(t, h.mu.TryLock(), "a hold waits for the eviction")
		return nil
	}))

	h.hold([]int64{1})
	ran := false
	require.NoError(t, h.unlessHeld(1, func() error {
		ran = true
		return nil
	}))
	assert.False(t, ran, "a held chunk is left alone")
}
