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

// One run's inputs and what it records — a pass per kind of thing that runs. Pure data but for
// the two hooks a worker's pass carries; nothing here reaches the queues or the subjects.
package supervisor

import "sync"

// passCore is what both passes are: what the run is against, and what it knew going in. Embedded
// unexported, so the four reads below are spelled the same on either.
type passCore[T any] struct {
	subject string
	prev    T
	known   bool
	snap    Snapshot
}

// Subject is the subject this run is against.
func (p *passCore[T]) Subject() string { return p.subject }

// Prev is this registration's own last committed value, the zero T until one lands.
func (p *passCore[T]) Prev() T { return p.prev }

// Known reports whether a value has ever been committed. What a body whose zero T is a
// legitimate answer needs: Prev cannot tell "nothing has landed" from "the last answer was the
// zero value", so without this such a body never commits its first answer and reads as never
// observed for as long as it stays healthy.
func (p *passCore[T]) Known() bool { return p.known }

// Snapshot is every registration's observable as of dispatch, for sibling reads through
// GetJobObservation and GetWorkerObservation.
func (p *passCore[T]) Snapshot() Snapshot { return p.snap }

// JobPass is one run of one job: what it is against, what it knew going in, and where it records
// what it found. The supervisor builds one per run and reads it back when the run returns.
type JobPass[T any] struct {
	passCore[T]
	next *T
}

// NewJobPass builds a pass for a job body's own tests, standing in for the supervisor. prev is
// what the job last committed, or nil for one that has committed nothing yet.
func NewJobPass[T any](subject string, prev *T, snap Snapshot) *JobPass[T] {
	p := &JobPass[T]{passCore: passCore[T]{subject: subject, snap: snap}}
	if prev != nil {
		p.prev, p.known = *prev, true
	}
	return p
}

// Commit records v as what this run found, callable wherever the body learns it. The supervisor
// buffers it and applies it when the run returns, in the same critical section as the attempt —
// so nothing is published mid-run, and a run that then panics or concludes Skip commits nothing.
// A worker, which has no end of the run to wait for, commits through WorkerPass instead.
//
// **It asserts the value moved**, and the supervisor takes that at its word: committing a value
// equal to the last one re-runs everything watching this one against news that is not news.
// Equality is the body's to judge, since the supervisor holds values as any and a value may be
// uncomparable or carry funcs that no generic compare can read.
//
// **The value it replaces is not handed back.** A commit frequently carries the last value's
// holdings forward, so a body that owns something — a connection, a goroutine — releases what it
// is really dropping itself, before committing what replaces it.
//
// Calling it twice commits the second value: one run, one attempt, one wake.
func (p *JobPass[T]) Commit(v T) { p.next = &v }

// Updated is what the body recorded and whether it did — the read side, for a job body's own
// tests, as Result's accessors are.
func (p *JobPass[T]) Updated() (T, bool) {
	if p.next == nil {
		var zero T
		return zero, false
	}
	return *p.next, true
}

// WorkerPass is one run of one worker — one whole life of it, since a worker's Run blocks until
// it is stopped or it dies. It carries the same four reads a job's does and two calls back into
// the supervisor, which is what makes a running worker readable.
//
// Both are safe from any goroutine the body has started: a worker commits from wherever its
// frames arrive.
type WorkerPass[T any] struct {
	passCore[T]

	// commit and ready reach the supervisor, set at dispatch. Both nil on a pass NewWorkerPass
	// built, where the two record themselves for the body's own tests to read back.
	commit func(any)
	ready  func()

	mu      sync.Mutex
	next    *T
	isReady bool
}

// NewWorkerPass builds a pass for a worker body's own tests, standing in for the supervisor.
// prev is what the worker last committed, or nil for one that has committed nothing yet. Nothing
// is wired to a supervisor, so Commit buffers and Ready records itself; Updated and IsReady read
// them back.
func NewWorkerPass[T any](subject string, prev *T, snap Snapshot) *WorkerPass[T] {
	p := &WorkerPass[T]{passCore: passCore[T]{subject: subject, snap: snap}}
	if prev != nil {
		p.prev, p.known = *prev, true
	}
	return p
}

// Commit records v as this worker's answer now. **It is applied at once**, not at the end of the
// run: a worker has no end to wait for, and reporting while it runs is what it is for. It walks
// the same watchers a job's commit does and asks for a pass, so OnPass fires for it from the
// pass loop like every other publication.
//
// Every commit takes the supervisor's lock and fires a pass, so a worker's T is what a reader
// reacts to rather than what arrives: one that committed per frame would publish per frame.
//
// A commit against a subject that is gone is refused and NOT handed back — a worker's value must
// own nothing, which is what makes the refusal free.
func (p *WorkerPass[T]) Commit(v T) {
	p.mu.Lock()
	p.next = &v
	commit := p.commit
	p.mu.Unlock()

	if commit != nil {
		commit(v)
	}
}

// Ready says the STARTING phase is over — the expensive part is done and the worker is now doing
// the thing. For a stream that is the watch being open, not its first frame: what a body must not
// do is hold its start slot waiting on a source that may legitimately be silent. The supervisor
// releases the slot, stops the startup timer, stamps the attempt and opens a healthy stretch, so
// dependents are scheduled.
//
// **It does not clear the failure streak**, which a run finishing cleanly is what does: starting
// is not proof, and a source that accepts every start and drops it would otherwise hold the ladder
// at its base delay forever.
//
// Calling it twice is harmless. Not calling it at all is what makes a clean exit NeverReady.
func (p *WorkerPass[T]) Ready() {
	p.mu.Lock()
	p.isReady = true
	ready := p.ready
	p.mu.Unlock()

	if ready != nil {
		ready()
	}
}

// Updated is the last value the body committed and whether it committed one — the read side, for
// a worker body's own tests.
func (p *WorkerPass[T]) Updated() (T, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.next == nil {
		var zero T
		return zero, false
	}
	return *p.next, true
}

// IsReady reports whether the body called Ready — the read side, for a worker body's own tests.
func (p *WorkerPass[T]) IsReady() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.isReady
}
