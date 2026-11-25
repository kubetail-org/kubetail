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

// The read side: what a caller — and every Run — sees of a subject's observables.
package supervisor

import (
	"fmt"
	"time"
)

// Snapshot is one subject's observables, copied under the supervisor's lock so the values and the
// schedule beside them agree. It is frozen at the moment it was taken — a run holding one can
// have it go stale under it, which is what the data edge exists to answer.
// GetJobObservation and GetWorkerObservation are the typed reads, by registration name; Attempts
// is the untyped one, for a reader walking every registration.
type Snapshot struct {
	obs []observable
	// byName is the supervisor's index of which registration a name addresses, and workers is
	// which kind each one is. Shared rather than copied: registration is closed before any
	// subject exists, so nothing writes either once a Snapshot can be taken.
	byName  map[string]registrationID
	workers []bool
}

// Attempts is one registration's bookkeeping, untyped — for a reader walking every registration,
// which walks the names it registered. Panics on a name nothing was registered under, as the
// typed reads do.
func (snap Snapshot) Attempts(name string) Attempts {
	return snap.obs[snap.idOf(name)].Attempts
}

// idOf resolves a name, panicking on one nothing was registered under: that is a wiring bug, and
// a zero value handed back as an answer is worse than a stopped process — a caller's "nothing
// known yet" branch would swallow it and park the reader for good.
func (snap Snapshot) idOf(name string) registrationID {
	id, ok := snap.byName[name]
	if !ok {
		panic("supervisor: nothing registered as " + name)
	}
	return id
}

// GetJobObservation is one job's observation, by the name it was registered under: the value its
// runs committed — the zero T until one has, which Known reports — beside the attempts that
// account for it. This is how a Run reads a sibling, and it needs nothing wired to do it.
//
// It panics on a name nothing was registered under, on a name registered as a WORKER, and on a T
// that is not what that job commits. All three are wiring bugs. A type is only checkable against
// a value that exists, so a mistyped read of a job that has committed nothing reads as not Known,
// and panics as soon as one lands — the kind is checkable either way, so it is checked first.
func GetJobObservation[T any](snap Snapshot, name string) JobObservation[T] {
	src := snap.read(name, false)
	o := JobObservation[T]{LastSeenAt: src.seen, Attempts: src.Attempts}
	o.Value = valueOf[T](name, src)
	return o
}

// GetWorkerObservation is one worker's observation: the last status it committed, when that
// status changed, and the attempts. It panics on the same three wiring bugs, a name registered as
// a job included.
func GetWorkerObservation[T any](snap Snapshot, name string) WorkerObservation[T] {
	src := snap.read(name, true)
	o := WorkerObservation[T]{ChangedAt: src.seen, Attempts: src.Attempts}
	o.Value = valueOf[T](name, src)
	return o
}

// read resolves a name and checks it was registered as the kind the caller is reading it as.
func (snap Snapshot) read(name string, worker bool) observable {
	id := snap.idOf(name)
	if snap.workers[id] != worker {
		kinds := [2]string{"a job", "a worker"}
		panic(fmt.Sprintf("supervisor: %q is %s, read as %s", name, kinds[b2i(snap.workers[id])], kinds[b2i(worker)]))
	}
	return snap.obs[id]
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// valueOf unboxes a committed value, the zero T while nothing has committed one.
func valueOf[T any](name string, src observable) T {
	var zero T
	if src.value == nil {
		return zero
	}
	val, ok := src.value.(T)
	if !ok {
		panic(fmt.Sprintf("supervisor: %q observes %T, read as %T", name, src.value, zero))
	}
	return val
}

// JobObservation is one job's last answer and the provenance to judge it: the committed value
// beside the supervisor's bookkeeping for the run that read it.
//
// Value outlives the failure that follows it — a read that stops being permitted does not mean
// the fact changed — and LastSeenAt is what makes the survivor readable: "identified, as of
// 10:00" is usable where "ready, as of 10:00" is not. A job that has never run is the zero value,
// which needs no sentinel: a zero LastAttempt is not Done, so every accessor already answers
// for it.
type JobObservation[T any] struct {
	// Value is the last committed answer. Meaningless until Known.
	Value T
	// LastSeenAt is when a run last confirmed Value: every success that has a value to date
	// advances it, whether or not that run committed a new one. Zero while there is no value,
	// which is what Known reads.
	LastSeenAt time.Time

	Attempts
}

// Known reports whether a run has committed a value, which is what makes Value readable. A
// succeeded run is not enough on its own — one can conclude nothing to write.
func (o JobObservation[T]) Known() bool { return !o.LastSeenAt.IsZero() }

// WorkerObservation is one worker's last status and the provenance to judge it. It splits from a
// job's over the two things a reader judges differently:
//
//   - **ChangedAt, not LastSeenAt.** A job confirms its value by running again; a worker confirms
//     it by still running. A live worker that has not committed for an hour has a current value
//     and an hour-old stamp, so the stamp is named for what it is.
//   - **Live, because the value does not outlive the worker.** A job's "identified, as of 10:00"
//     still holds after the read that follows it fails; a worker's "watching" is false the moment
//     the worker exits. After an exit, Value is the last status before it and Attempts says how
//     it ended.
type WorkerObservation[T any] struct {
	// Value is the last committed status. Meaningless until Known.
	Value T
	// ChangedAt is when the worker last committed, zero while it has committed nothing —
	// which is what Known reads.
	ChangedAt time.Time

	Attempts
}

// Known reports whether the worker has committed a status, which is what makes Value readable.
func (o WorkerObservation[T]) Known() bool { return !o.ChangedAt.IsZero() }

// Live reports that Value is current: the worker is running and has called Ready.
func (o WorkerObservation[T]) Live() bool { return o.Ready() }

// NewSnapshot builds a snapshot carrying the named JOB values, for a body's own tests, standing
// in for the supervisor. Values only: the bookkeeping beside each one is the zero Attempts, which
// reads as something that has never run.
func NewSnapshot(values map[string]any) Snapshot {
	snap := Snapshot{byName: make(map[string]registrationID, len(values))}
	for name, v := range values {
		snap.byName[name] = registrationID(len(snap.obs))
		snap.obs = append(snap.obs, observable{value: v})
		snap.workers = append(snap.workers, false)
	}
	return snap
}

// NewWorkerSnapshot is NewSnapshot for worker values, so a body reading a worker sibling can
// build the snapshot its own tests hand it.
func NewWorkerSnapshot(values map[string]any) Snapshot {
	snap := NewSnapshot(values)
	for i := range snap.workers {
		snap.workers[i] = true
	}
	return snap
}
