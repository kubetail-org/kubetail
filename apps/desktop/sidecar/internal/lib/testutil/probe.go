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

import "testing"

// Probe is a repeating notification from a test fake to the test: a callback that
// runs many times calls Fire, and the test takes them one at a time with Await.
// [Signal] is the single-shot counterpart.
//
// Fire never blocks, because a fake that blocks stalls the code under test. That
// makes the buffer lossy, and which value it loses matters: a test almost always
// waits for the NEWEST event ("did it probe again after the poke?"), so Probe
// discards the oldest and keeps the newest. A plain buffered channel with a
// select/default send does the opposite — it drops the value being published,
// which is the one the test is waiting for, and the test times out on an event
// that really happened.
type Probe[T any] struct{ ch chan T }

// NewProbe returns a Probe buffering up to capacity values, which must be > 0.
// Capacity is a comfort margin, not a correctness knob — Fire never blocks and
// Await tolerates history, so size it to the burst a test should be able to
// inspect, not to the total a fake might publish.
func NewProbe[T any](capacity int) *Probe[T] {
	if capacity < 1 {
		panic("testutil: Probe capacity must be > 0")
	}
	return &Probe[T]{ch: make(chan T, capacity)}
}

// Fire publishes v, discarding the oldest buffered value if the buffer is full.
// Safe to call from any goroutine and from several at once.
func (p *Probe[T]) Fire(v T) {
	select {
	case p.ch <- v:
		return
	default:
	}
	select {
	case <-p.ch: // make room by dropping the oldest
	default: // coverage:ignore only a receive between the first select and this one empties the full buffer
	}
	select {
	case p.ch <- v:
	default: // coverage:ignore only a concurrent Fire landing between the two selects reaches it
		// A concurrent Fire claimed the slot we just freed. Its value is at least
		// as new as ours, so the newest-wins property still holds and dropping
		// here is correct — retrying instead could spin against a busy producer.
	}
}

// Await returns the next value, failing the test after the failsafe timeout.
func (p *Probe[T]) Await(t testing.TB, what string) T {
	t.Helper()
	return Recv(t, p.ch, what)
}

// Drain discards everything buffered, so a following Await answers to an event
// the test caused rather than to one still queued from earlier.
func (p *Probe[T]) Drain() {
	for {
		select {
		case <-p.ch:
		default:
			return
		}
	}
}

// TryAwait returns the next buffered value without waiting, reporting false if
// none is queued — for a test asserting an event has ALREADY happened rather
// than that it eventually will.
func (p *Probe[T]) TryAwait() (T, bool) {
	select {
	case v := <-p.ch:
		return v, true
	default:
		var zero T
		return zero, false
	}
}

// Chan exposes the underlying channel for a select — chiefly a negative
// assertion, which needs its own short window rather than the failsafe timeout.
func (p *Probe[T]) Chan() <-chan T { return p.ch }
