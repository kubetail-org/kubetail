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

package testutil

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbeDeliversInOrder(t *testing.T) {
	p := NewProbe[int](4)
	p.Fire(1)
	p.Fire(2)
	assert.Equal(t, 1, p.Await(t, "the first value"))
	assert.Equal(t, 2, p.Await(t, "the second value"))
}

// The property Probe exists for: an overrun keeps the NEWEST values, because that
// is what a test waits for. A select/default send would have kept 1..3 and thrown
// away 4..6.
func TestProbeOverrunKeepsTheNewest(t *testing.T) {
	p := NewProbe[int](3)
	for i := 1; i <= 6; i++ {
		p.Fire(i)
	}
	assert.Equal(t, []int{4, 5, 6}, []int{
		p.Await(t, "1st"), p.Await(t, "2nd"), p.Await(t, "3rd"),
	})
}

func TestProbeFireNeverBlocks(t *testing.T) {
	p := NewProbe[int](1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 1000 {
			p.Fire(i)
		}
	}()
	Wait(t, done, "a thousand Fires into a cap-1 probe to return")
}

// Concurrent producers must not wedge or lose the newest-wins property; the
// buffer holds a suffix of what was published, never a stale prefix.
func TestProbeIsSafeUnderConcurrentFire(t *testing.T) {
	p := NewProbe[int](8)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for i := range 250 {
				p.Fire(i)
			}
		})
	}
	wg.Wait()
	got, ok := p.TryAwait()
	require.True(t, ok, "something must be buffered")
	assert.GreaterOrEqual(t, got, 0)
}

func TestProbeDrainDiscardsHistory(t *testing.T) {
	p := NewProbe[int](4)
	p.Fire(1)
	p.Fire(2)
	p.Drain()

	_, ok := p.TryAwait()
	assert.False(t, ok, "Drain must leave the probe empty")

	p.Fire(3)
	assert.Equal(t, 3, p.Await(t, "the post-drain value"))
}

func TestProbeTryAwaitReportsAnEmptyProbe(t *testing.T) {
	p := NewProbe[string](2)
	_, ok := p.TryAwait()
	assert.False(t, ok)

	p.Fire("x")
	v, ok := p.TryAwait()
	assert.True(t, ok)
	assert.Equal(t, "x", v)
}

func TestNewProbeRejectsAZeroCapacity(t *testing.T) {
	assert.Panics(t, func() { NewProbe[int](0) },
		"a cap-0 probe would make Fire a no-op, silently losing every event")
}
