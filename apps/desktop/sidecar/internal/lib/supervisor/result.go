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

// What a run concludes and what the supervisor records of it. Pure data — nothing here reaches the
// queues or the subjects.
package supervisor

import (
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
)

// Reason classifies a run in the caller's vocabulary, opaque to the supervisor but for the two
// constants below. A caller aliases this type and defines its own set.
type Reason string

const (
	// ReasonSucceeded is stamped by Succeeded; a caller never passes it.
	ReasonSucceeded Reason = "Succeeded"
	// ReasonDependencyFailed is recorded by the supervisor when a dependency is failing,
	// in place of a run. See the dependency lifecycle on Supervisor.due.
	ReasonDependencyFailed Reason = "DependencyFailed"
	// ReasonInternal is a bug in a body, recorded by the supervisor when a run panics.
	ReasonInternal Reason = "Internal"
	// ReasonNeverReady is a worker that exited before it called Ready. Recorded as a failure
	// whatever the worker returned: it never proved it was up, so the ladder paces the restart
	// rather than a clean exit's floor.
	ReasonNeverReady Reason = "NeverReady"
)

// Verdict is how a finished run was classified, recorded on the Attempt because the schedule is
// derived from it: the supervisor never interprets a caller's Reason.
type Verdict int

const (
	// VerdictNone is the zero value: no run has finished.
	VerdictNone Verdict = iota
	VerdictSucceeded
	VerdictFailed
	VerdictSuspended
)

// Result is what a run concluded: the record it leaves and the schedule it earns. Built by
// Succeeded, Fail, Suspend, or Skip — the zero Result is invalid, and the supervisor records one as
// an Internal failure, the same as a body that panicked.
type Result struct {
	kind         resultKind
	verdict      Verdict
	reason       Reason
	message      string
	err          error
	requeueAfter time.Duration
}

type resultKind int

const (
	resultInvalid resultKind = iota
	resultRecord             // record an attempt carrying verdict/reason
	resultSkip               // record nothing
)

// Succeeded records success. A job is due again after its interval; a worker has STOPPED
// cleanly, and is started again after the floor.
func Succeeded() Result {
	return Result{kind: resultRecord, verdict: VerdictSucceeded, reason: ReasonSucceeded}
}

// RequeueAfter asks for the next run sooner than the interval, for a wait this run knows the
// length of — beehive's spelling one layer up, for the same kind of ask.
//
// **Unlike beehive's, it can only bring a run forward**: the supervisor takes it when it is positive
// and shorter than the registered interval, and ignores it otherwise. A registration is a
// registration's cadence in a way a beehive resync is not — it is what bounds requests against someone
// else's cluster — so no return path may push a subject past it. A zero is no ask rather than
// "immediately", which would be a hot loop.
//
// Read on a succeeded result and nowhere else. Fail owns the backoff ladder and Suspend schedules
// nothing, so calling this on either is inert.
func (r Result) RequeueAfter(d time.Duration) Result {
	r.requeueAfter = d
	return r
}

// Fail records a failure; the next run is up the backoff ladder. Message defaults to the
// error's text, rendered: a caller folds it onto a record, which is persisted and served to
// the UI, and a client-go error quotes the request URL it failed on. Err keeps the original,
// which is what a caller matching on it needs.
func Fail(reason Reason, err error) Result {
	r := Result{kind: resultRecord, verdict: VerdictFailed, reason: reason, err: err}
	if err != nil {
		r.message = safe.Safe(err)
	}
	return r
}

// Suspend records why and schedules nothing; a Wake is what brings it back. It ends a
// failure streak — a suspension parks a question rather than failing at one.
func Suspend(reason Reason, message string) Result {
	return Result{kind: resultRecord, verdict: VerdictSuspended, reason: reason, message: message}
}

// Skip records nothing and schedules nothing; a Wake is what brings it back. For a run
// that learned nothing usable — an unreadable source, a shutdown cancellation.
func Skip() Result {
	return Result{kind: resultSkip}
}

// The read side, for a body's own tests: what the body returned, without giving a body a
// way to build a Result the constructors cannot.
func (r Result) Verdict() Verdict { return r.verdict }
func (r Result) Reason() Reason   { return r.reason }
func (r Result) Message() string  { return r.message }
func (r Result) Err() error       { return r.err }
func (r Result) IsSkip() bool     { return r.kind == resultSkip }

// Attempt is one run of one registration, from scheduled through finished. Its fields fill in that
// order, so the same value describes a run at every stage of its life.
type Attempt struct {
	// ScheduledAt is when this run should start — the backoff ladder made visible, since
	// successive values show the interval widening. StartedAt is when it did start, FinishedAt
	// when it ended; both zero until they happen, and separate from ScheduledAt because a
	// saturated supervisor lets a scheduled time slip into the past, which would otherwise read
	// as running.
	ScheduledAt time.Time
	StartedAt   time.Time
	// ReadyAt is when a worker called Ready, and zero for a job — which is "ready" when it
	// returns, so it has no earlier moment to stamp.
	ReadyAt    time.Time
	FinishedAt time.Time
	// Verdict, Reason, and Message classify the outcome; Err is the raw error, nil on success.
	// All unset until the run finishes.
	Verdict Verdict
	Reason  Reason
	Message string
	Err     error

	// requeueAfter is what the run asked for, zero for none. Unexported because it is the
	// scheduler's own bookkeeping, and every exported field here is copied into the Snapshots
	// callers read.
	requeueAfter time.Duration
}

// attemptOf is the record one finished run leaves. The only place a Result is turned into an
// Attempt, so the bookkeeping the two carry cannot drift apart.
func attemptOf(res Result, startedAt, readyAt, finishedAt time.Time) Attempt {
	return Attempt{
		StartedAt:  startedAt,
		ReadyAt:    readyAt,
		FinishedAt: finishedAt,
		Verdict:    res.verdict,
		Reason:     res.reason,
		Message:    res.message,
		Err:        res.err,

		requeueAfter: res.requeueAfter,
	}
}

// Running reports whether this run has started and not finished.
func (a Attempt) Running() bool { return !a.StartedAt.IsZero() && a.FinishedAt.IsZero() }

// Done reports whether this run has finished, which is what makes Verdict readable.
func (a Attempt) Done() bool { return !a.FinishedAt.IsZero() }

// Latency is how long the run took: zero until it finishes, and zero for one recorded without
// being dispatched — a DependencyFailed record has no duration to report.
func (a Attempt) Latency() time.Duration {
	if !a.Done() || a.StartedAt.IsZero() {
		return 0
	}
	return a.FinishedAt.Sub(a.StartedAt)
}

// Attempts is one registration's bookkeeping for one subject. The supervisor owns every write; a caller
// reads it out of a Snapshot, embedded in the Observation beside the value it accounts for.
type Attempts struct {
	// LastAttempt is the most recent run that finished; NextAttempt is the one that has not,
	// scheduled or already running. A run moves from one to the other as it completes.
	LastAttempt Attempt
	NextAttempt Attempt

	// Failures counts consecutive failed runs and FailingSince is when the streak began, which
	// the count cannot give: the ladder widens, so failures do not map to elapsed time. For a
	// worker this is its retry streak while it is DOWN — the rung the ladder climbs — and says
	// nothing about one that flaps while healthy, which is what Restarts is for.
	Failures     int
	FailingSince time.Time

	// HealthySince is when the current healthy stretch began, zero while there is none, and
	// Restarts counts the runs started within it. FailingSince's mirror, and it answers what a
	// single attempt's stamps cannot: a rotation is a clean exit and a fresh run, so "this
	// stream has been up for a minute" is readable off the attempt and "this kind has been
	// healthy for an hour, over 120 rotations" is readable only here.
	HealthySince time.Time
	Restarts     int

	// LastRunAt is when the most recently FINISHED run started, whatever it concluded — the
	// one thing LastAttempt cannot say, since a Skip records no attempt at all. Written at the
	// end and stamped with the beginning, so one comparison answers both halves of the
	// question a caller waiting on a run it asked for has: a run that began at or after the
	// ask has finished. A level, so a reader that misses frames still sees it move.
	LastRunAt time.Time

	// skipped marks a last run that returned Skip — the one memory a Skip leaves. It records
	// nothing, so without this the pass would read the registration as never-run and
	// re-dispatch it at once. Cleared when a run records; a Wake goes through runQ, so it needs
	// no clearing to force a run.
	skipped bool
}

// OK reports whether the last FINISHED run succeeded. False while nothing has finished — and
// false for a worker that is up right now, whose health is Ready rather than a finished verdict.
func (a Attempts) OK() bool { return a.LastAttempt.Verdict == VerdictSucceeded }

// Ready reports a worker that is running and has called Ready — the only reading of a worker's
// health, since its value is a status that stops holding the moment it exits. Always false for a
// job, which stamps no ReadyAt.
func (a Attempts) Ready() bool { return a.InFlight() && !a.NextAttempt.ReadyAt.IsZero() }

// InFlight reports whether a run is under way.
func (a Attempts) InFlight() bool { return a.NextAttempt.Running() }

// Scheduled reports whether another run is due. False for a suspended one.
func (a Attempts) Scheduled() bool { return !a.NextAttempt.ScheduledAt.IsZero() }

// Suspended reports a registration parked by a run that suspended: nothing due, nothing running,
// and a suspension is the last thing that happened.
//
// **Narrower than an unscheduled next attempt**, which a Skip also leaves. A Skip is a run
// declining to record, waiting on the edge that will ask for it again; a caller waking that on a
// hunch drags whatever shares the wake off its backoff ladder. A Skip over a suspension is that
// same wait, which is why it is read here and not just the last record.
func (a Attempts) Suspended() bool {
	return !a.skipped && !a.Scheduled() && !a.InFlight() &&
		a.LastAttempt.Verdict == VerdictSuspended
}

// begin marks a run dispatched. InFlight reads true from here until the commit, which is what
// stops a pass scheduling over a run already out.
//
// Every start inside a healthy stretch is a restart — the thing went down and came back, whoever
// asked — so a clean rotation and a Restart count alike. The stretch's own first start is not
// one, which is what reading HealthySince gives that counting dispatches would not.
func (a *Attempts) begin(at time.Time) {
	a.NextAttempt.StartedAt = at
	if !a.HealthySince.IsZero() {
		a.Restarts++
	}
}

// markReady stamps the run in flight as ready and opens a healthy stretch if none is open. Both
// halves are the worker's Ready; a job reaches neither.
//
// **It leaves the failure streak alone**, which a run completing cleanly is what clears. A worker
// proves itself by finishing, not by starting: a source that accepts every start and drops it
// would otherwise reset the ladder on each one and retry at the base delay forever. Until that
// clean exit the two pairs of fields describe different phases — up now, and a rough time getting
// here — and only the down phase reads the streak.
func (a *Attempts) markReady(at time.Time) {
	a.NextAttempt.ReadyAt = at
	if a.HealthySince.IsZero() {
		a.HealthySince, a.Restarts = at, 0
	}
}

// schedule sets when the next run is due, zero for one with nothing scheduled.
func (a *Attempts) schedule(at time.Time) { a.NextAttempt = Attempt{ScheduledAt: at} }

// record files a finished run. A suspension ends a failure streak the same way a success does,
// since it parks the question rather than failing at it — but it is not health, because nothing
// is running.
//
// It writes nothing about the next run — that is derived from this, not decided here.
//
// The run moves out of NextAttempt rather than replacing it, so the schedule it was dispatched
// on survives into the record: StartedAt against ScheduledAt is how long it waited for a slot,
// and a caller cannot tell a slow body from a saturated supervisor without it.
func (a *Attempts) record(att Attempt) {
	att.ScheduledAt = a.NextAttempt.ScheduledAt
	a.LastAttempt = att

	switch att.Verdict {
	case VerdictFailed:
		a.Failures++
		if a.FailingSince.IsZero() {
			a.FailingSince = att.FinishedAt
		}
		a.HealthySince, a.Restarts = time.Time{}, 0
		return
	case VerdictSucceeded:
		// A job is healthy from the run that succeeded. A worker was already healthy at
		// Ready, and this exit is a rotation inside that stretch rather than the start of
		// one — which is what makes HealthySince stand across it.
		if a.HealthySince.IsZero() {
			a.HealthySince, a.Restarts = att.FinishedAt, 0
		}
	case VerdictSuspended:
		a.HealthySince, a.Restarts = time.Time{}, 0
	}
	a.Failures, a.FailingSince = 0, time.Time{}
}
