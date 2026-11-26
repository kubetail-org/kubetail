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

// Package workqueue queues keyed work: Add names a key, and Next hands it to exactly one worker.
//
// **It is not a bus.** A bus fans out — every receiver gets a copy of every send. The three
// differences are each load-bearing for work:
//
//   - A key reaches one worker and no other, which is what makes it safe to drain a queue with
//     several.
//   - Queued work is never lost to timing. It lives in the queue, so a key added before any
//     worker runs is still waiting when one arrives; a bus drops a send nobody is receiving.
//   - A key waits once, however many times it is added, so a burst of adds asks for one pass.
//
// Add queues a held key afresh when the worker reports Done, so work arriving mid-pass is
// never folded into a pass that could not have seen it. AddIfAbsent leaves queued and held
// keys alone; callers deriving work from state must recheck that state after Done.
package workqueue

import (
	"context"
	"sync"
)

// keyState is where a key is in its life. A key is in exactly one state at a time — everything
// the queue promises hangs on that — and an idle key is simply absent from the map.
type keyState int

const (
	waiting   keyState = iota + 1 // queued in pending
	held                          // handed to a worker, Done not yet called
	heldDirty                     // held, and added again meanwhile — queued afresh on Done
)

// Queue hands each added key to exactly one worker. Producers call Add; each worker goroutine
// loops on Next and calls Done for every key it takes.
//
// **Every key taken must be given back with Done**, or the queue goes on believing that key is
// being worked and every later Add for it waits on a pass that ended. A worker that may return
// early defers the call.
//
// Use New — the zero value has no map to key on.
type Queue[K comparable] struct {
	mu sync.Mutex
	// pending is the order keys are handed out in; state is where each key is in its life.
	// The two agree: pending holds exactly the keys whose state is waiting.
	pending []K
	state   map[K]keyState
	closed  bool
	// ready wakes workers parked in Next. Capacity 1 and sent non-blocking: it says work may
	// be there, never how much, so a woken worker re-checks and passes the signal on if it
	// left any.
	ready chan struct{}
}

// New returns an empty Queue.
func New[K comparable]() *Queue[K] {
	return &Queue[K]{
		state: map[K]keyState{},
		ready: make(chan struct{}, 1),
	}
}

// Add asks for k to be worked. It never blocks and never fails: a key already waiting is left
// where it is, and one a worker is holding is queued again when that worker finishes — the pass
// in flight is never assumed to have seen this add.
func (q *Queue[K]) Add(k K) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return
	}
	switch q.state[k] {
	case waiting, heldDirty:
		// Already owed a fresh pass.
	case held:
		q.state[k] = heldDirty
	default: // idle
		q.enqueueLocked(k)
	}
}

// AddIfAbsent asks for k only if it is neither queued nor held. Unlike Add, it does not
// request a redelivery of held work. A caller deriving work from state must check that state
// again after Done, since this call can be ignored while the previous work is finishing.
func (q *Queue[K]) AddIfAbsent(k K) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return
	}
	if _, exists := q.state[k]; !exists {
		q.enqueueLocked(k)
	}
}

// Next waits for the next key. It reports false when ctx ends, and when the queue is closed and
// drained — the two ways a worker loop is meant to finish, so a caller that separates them wants
// ctx.Err() rather than another return value.
func (q *Queue[K]) Next(ctx context.Context) (K, bool) {
	for {
		k, ok, drained := q.take()
		if ok {
			return k, true
		}
		if drained {
			var zero K
			return zero, false
		}

		select {
		case <-ctx.Done():
			var zero K
			return zero, false
		case <-q.ready:
			// Closed, or work may have landed. take says which.
		}
	}
}

// Done reports that the pass over k has finished, freeing it to be handed out again. A key
// nobody holds is not an error — a worker that cannot tell whether it took one may call this
// either way.
func (q *Queue[K]) Done(k K) {
	q.mu.Lock()
	defer q.mu.Unlock()

	switch q.state[k] {
	case held:
		delete(q.state, k)
	case heldDirty:
		if q.closed {
			delete(q.state, k)
			return
		}
		q.enqueueLocked(k)
	}
}

// Close stops the queue taking new work. Workers drain what is already queued and Next then
// reports closed, so a shutdown that wants the backlog finished waits for its workers and one
// that does not cancels their context instead.
//
// Idempotent. Adds after it are dropped rather than refused: a producer racing a shutdown has
// nothing useful to do with the news.
func (q *Queue[K]) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return
	}
	q.closed = true
	close(q.ready)
}

// take hands out the oldest waiting key, or reports that the caller must wait — or, once closed
// and drained, that there is nothing left to wait for.
func (q *Queue[K]) take() (key K, ok bool, drained bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.pending) == 0 {
		var zero K
		return zero, false, q.closed
	}

	k := q.pending[0]
	q.pending = q.pending[1:]
	q.state[k] = held

	// Pass the wake on: one signal was spent to deliver one key, and another worker is owed
	// what is left.
	if len(q.pending) > 0 {
		q.signalLocked()
	}
	return k, true, false
}

// enqueueLocked queues k, which must not already be waiting or held.
func (q *Queue[K]) enqueueLocked(k K) {
	q.state[k] = waiting
	q.pending = append(q.pending, k)
	q.signalLocked()
}

// signalLocked wakes one parked worker. Under the lock because that is what keeps the send off a
// closed channel: Close sets closed and closes ready in the same critical section.
func (q *Queue[K]) signalLocked() {
	if q.closed {
		return
	}
	select {
	case q.ready <- struct{}{}:
	default:
	}
}
