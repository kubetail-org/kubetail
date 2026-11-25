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

package supervisor

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// subj is the one subject most tests track.
const subj = "ctx-1"

// quietWindow bounds a negative assertion — something that must NOT have happened — which has no
// event to wait for and so needs a window of its own rather than testutil's failsafe. Short:
// what it watches for would already have happened.
const quietWindow = 50 * time.Millisecond

// steered is a job the test steers: the result is swapped at will, runs are counted, and an
// optional hook fires inside the run — which is how a test interleaves a Remove or a Wake with
// a run in flight.
type steered struct {
	mu          sync.Mutex
	res         Result
	next        *string
	onRun       func()
	runs        int
	sawDeadline bool
}

func (p *steered) Run(ctx context.Context, pass *JobPass[string]) Result {
	p.mu.Lock()
	p.runs++
	_, p.sawDeadline = ctx.Deadline()
	res, next, hook := p.res, p.next, p.onRun
	p.mu.Unlock()

	if hook != nil {
		hook()
	}
	if next != nil {
		pass.Commit(*next)
	}
	return res
}

func (p *steered) set(res Result) { p.mu.Lock(); defer p.mu.Unlock(); p.res = res }
func (p *steered) count() int     { p.mu.Lock(); defer p.mu.Unlock(); return p.runs }

// jobFunc adapts a bare func for a test that needs a one-off body.
type jobFunc func(ctx context.Context, pass *JobPass[string]) Result

func (f jobFunc) Run(ctx context.Context, pass *JobPass[string]) Result {
	return f(ctx, pass)
}

// single is an supervisor with one steered job, closed on cleanup.
func single(t *testing.T, res Result, opts ...RegistrationOption) (*Supervisor, *steered, string) {
	t.Helper()
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	p := &steered{res: res}
	RegisterJob(e, "conn", p, opts...)
	return e, p, "conn"
}

// pair is single plus a job requiring the first.
func pair(t *testing.T, aRes, bRes Result) (e *Supervisor, a, b *steered, aName, bName string) {
	t.Helper()
	e = New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	a, b = &steered{res: aRes}, &steered{res: bRes}
	RegisterJob(e, "conn", a)
	RegisterJob(e, "uid", b, WithDependencies("conn"))
	return e, a, b, "conn", "uid"
}

// linked is a pair where the second declares both edges on the first — the shape kubeconn's
// watchers have, and the only one where a value change and a health gate are both in play.
func linked(t *testing.T, aRes, bRes Result) (e *Supervisor, a, b *steered, aName, bName string) {
	t.Helper()
	e = New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	a, b = &steered{res: aRes}, &steered{res: bRes}
	RegisterJob(e, "conn", a)
	RegisterJob(e, "uid", b, WithDependencies("conn"), WithWatches("conn"))
	return e, a, b, "conn", "uid"
}

// commits makes the body hand back v, which the supervisor reads as "the value moved".
func (p *steered) commits(v string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.next = &v
}

// commitsNothing makes the body hand back nil — the contract for an answer that did not move.
func (p *steered) commitsNothing() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.next = nil
}

// settled runs both jobs of a linked pair to a succeeded rest, so what follows is measured
// against two jobs parked on their intervals rather than against a fresh subject's first pass.
func settled(t *testing.T, e *Supervisor) {
	t.Helper()
	e.Add(subj)
	e.settle()
	runNext(t, e)
	runNext(t, e)
	noRuns(t, e, "the pair should be parked on its interval")
}

// within bounds a wait for something that must arrive, so a regression fails the test rather
// than hanging it until the suite's own deadline.
func within(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// settle runs every pass currently queued, on the test's goroutine — where passLoop would pick
// them up.
func (e *Supervisor) settle() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Next takes what is queued and reports rather than waiting for more.

	for {
		name, ok := e.passQ.Next(ctx)
		if !ok {
			return
		}
		e.pass(name)
		e.passQ.Done(name)
	}
}

// runNext takes the next due run and executes it the way a worker would, then settles the pass
// it asked for. All on the test's goroutine, so assertions read settled state.
func runNext(t *testing.T, e *Supervisor) key {
	t.Helper()
	k, ok := e.runQ.Next(within(t))
	require.True(t, ok, "the run queue closed")
	e.runOne(t.Context(), k, func() {})
	e.runQ.Done(k)
	e.settle()
	return k
}

// takeRun pops one due run without executing it, giving the key back.
func takeRun(t *testing.T, e *Supervisor) key {
	t.Helper()
	k, ok := e.runQ.Next(within(t))
	require.True(t, ok, "the run queue closed")
	e.runQ.Done(k)
	return k
}

// drainRuns empties the run queue, so a following negative assertion answers to what the test
// did rather than to what the subject already owed.
func drainRuns(t *testing.T, e *Supervisor) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Next takes what is queued and reports rather than waiting for more.
	for {
		k, ok := e.runQ.Next(ctx)
		if !ok {
			return
		}
		e.runQ.Done(k)
	}
}

// noRuns is a negative assertion, so it needs a bounded window rather than the failsafe: it
// fails the instant a run is handed out.
func noRuns(t *testing.T, e *Supervisor, msg string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if k, ok := e.runQ.Next(ctx); ok {
		e.runQ.Done(k)
		t.Fatalf("%s: %v was handed out", msg, k)
	}
}

// keyOf is the run-queue key for a registration the test names, which is what the queue is keyed by
// even though a name is what a caller addresses.
func keyOf(e *Supervisor, name string) key {
	return key{subject: subj, registration: e.byName[name]}
}

// att reads one registration's attempts through Read.
func att(t *testing.T, e *Supervisor, name string) Attempts {
	t.Helper()
	v, ok := e.Read(subj)
	require.True(t, ok, "subject not tracked")
	return v.Attempts(name)
}

func startSupervisor(t *testing.T, e *Supervisor) {
	t.Helper()
	stop := e.Start(t.Context())
	t.Cleanup(func() { assert.NoError(t, stop(context.Background())) })
}

// --- the backoff ladder ---

func TestBackoffClimbsToItsCap(t *testing.T) {
	b := Backoff{Base: time.Second, Factor: 2, Cap: time.Minute}
	assert.Equal(t, time.Second, b.Delay(1), "the first failure waits the base")
	assert.Equal(t, 2*time.Second, b.Delay(2))
	assert.Equal(t, 8*time.Second, b.Delay(4), "each rung widens by the factor")
	assert.Equal(t, time.Minute, b.Delay(7), "until the cap")
	assert.Equal(t, time.Minute, b.Delay(20), "and stays there")
}

// --- registration ---

// The supervisor's own knobs are options too, and a New that states none takes the defaults.
func TestWithStartConcurrencySetsTheSlotCount(t *testing.T) {
	assert.Equal(t, 3, New(WithStartConcurrency(3)).settings.startConcurrency)
	assert.Positive(t, New().settings.startConcurrency, "a default fleet")
}

// A supervisor with no slots admits nothing: every subject queues and no run is ever dispatched,
// with no error, no log and no verdict to read. It is a table wired wrong at boot — a settings
// struct built without the field — so it is refused the way every other one is.
func TestASupervisorWithNoStartSlotsIsRefused(t *testing.T) {
	assert.PanicsWithValue(t, "supervisor: WithStartConcurrency needs at least one slot",
		func() { New(WithStartConcurrency(0)) })
	assert.Panics(t, func() { New(WithStartConcurrency(-1)) })
}

// Register needs something to run, and something to call it.
func TestRegisterPanicsWithoutABodyOrAName(t *testing.T) {
	e := New()

	assert.Panics(t, func() { RegisterJob[string](e, "conn", nil) })
	assert.Panics(t, func() { RegisterJob(e, "", &steered{}) })
	assert.Panics(t, func() { RegisterWorker[string](e, "sync", nil) })
}

// OnPass is wiring, set before the supervisor runs — like a registration.
func TestOnPassPanicsAfterStart(t *testing.T) {
	e, _, _ := single(t, Skip())
	startSupervisor(t, e)

	assert.Panics(t, func() { e.OnPass(func(string, Snapshot) {}) })
}

// A registration's public identity is its name; the index behind it is the supervisor's own.
func TestRegisterResolvesAnEdgeToWhatItNames(t *testing.T) {
	e, _, a := single(t, Skip())
	RegisterJob(e, "uid", &steered{}, WithDependencies(a))

	assert.Equal(t, "uid", e.specs[1].name)
	assert.Equal(t, []registrationID{0}, e.specs[1].dependencies)
	assert.Equal(t, registrationID(1), e.byName["uid"])
}

func TestRegisterPanicsOnADuplicateName(t *testing.T) {
	e, p, _ := single(t, Skip())

	assert.Panics(t, func() { RegisterJob(e, "conn", p) })
}

// An edge resolves against what is registered so far, so a forward reference cannot be
// expressed and the registration order stays topological.
func TestRegisterPanicsOnADependencyNotYetRegistered(t *testing.T) {
	e := New()

	assert.Panics(t, func() { RegisterJob(e, "uid", &steered{}, WithDependencies("nope")) })
}

// A registration states only what deviates from the package defaults.
func TestRegistrationDefaultsWhatItDoesNotState(t *testing.T) {
	e, _, name := single(t, Skip(), WithInterval(10*time.Minute))

	cfg := e.specs[e.byName[name]].cfg
	assert.Equal(t, 10*time.Minute, cfg.interval)
	assert.Equal(t, defaultJobCfg.timeout, cfg.timeout)
	assert.Equal(t, defaultJobCfg.backoff, cfg.backoff)
}

func TestRegisterPanicsAfterStart(t *testing.T) {
	e, p, _ := single(t, Skip())
	startSupervisor(t, e)

	assert.Panics(t, func() { RegisterJob(e, "late", p) })
}

// A subject's bookkeeping is sized when it is added, so the registration set must be complete first.
func TestRegisterPanicsAfterAdd(t *testing.T) {
	e, p, _ := single(t, Skip())
	e.Add(subj)

	assert.Panics(t, func() { RegisterJob(e, "late", p) })
}

// --- subjects and the pass ---

// Removing what was never added, passing over it, and dispatching against it are all no-ops: a
// subject can go while work naming it is still queued.
func TestASubjectNothingTracksIsANoOpEverywhere(t *testing.T) {
	e, p, _ := single(t, Succeeded())

	e.Remove("never-added")
	e.pass("never-added")
	e.runOne(t.Context(), key{subject: "never-added"}, func() {})

	assert.Zero(t, p.count(), "a run was dispatched against a subject nothing tracks")
	noRuns(t, e, "a pass over a subject nothing tracks queued work")
}

// A fresh subject owes exactly its runnable registrations: the dependent waits on an answer, and a
// second Add of the same name asks for nothing.
func TestAddQueuesWhatAFreshSubjectOwes(t *testing.T) {
	e, _, _, aID, _ := pair(t, Skip(), Skip())

	e.Add(subj)
	e.settle()

	assert.Equal(t, keyOf(e, aID), takeRun(t, e))
	noRuns(t, e, "more than the connection job was queued")

	e.Add(subj)
	e.settle()
	noRuns(t, e, "a second Add asked for a run")
}

func TestWorkQueuedBeforeStartIsServedOnceItRuns(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	ran := testutil.NewProbe[struct{}](1)
	RegisterJob(e, "conn", jobFunc(func(context.Context, *JobPass[string]) Result {
		ran.Fire(struct{}{})
		return Suspend("Resolved", "")
	}))
	e.Add(subj)

	startSupervisor(t, e)

	ran.Await(t, "the run a pre-Start Add asked for")
}

func TestRemoveStopsTheSubjectsTimer(t *testing.T) {
	e, _, _ := single(t, Fail("Unreachable", assert.AnError))
	e.Add(subj)
	e.settle()
	runNext(t, e) // a failure earns a retry, which arms the timer
	sub := e.subjects[subj]
	require.NotNil(t, sub.timer)

	e.Remove(subj)

	assert.Nil(t, sub.timer)
	_, ok := e.Read(subj)
	assert.False(t, ok)
}

// The last holder can remove and another caller re-add the same name. The subject it gets is a
// different one, and what a run dispatched against the old one concluded is refused rather than
// filed against it.
//
// Remove joins, so nothing can reach this in a running supervisor — which is why the guard is
// driven directly here. It is the check that backs the join up.
func TestACommitAgainstAReplacedSubjectIsRefused(t *testing.T) {
	e, p, id := single(t, Fail("Unreachable", assert.AnError))
	p.commits("v1")
	e.Add(subj)
	e.settle()
	stale := e.subjects[subj]

	e.Remove(subj)
	e.Add(subj)
	k := keyOf(e, id)
	e.commit(k, stale, &runHandle{}, e.specs[k.registration], time.Now(),
		runOutcome{res: Fail("Unreachable", assert.AnError), val: "v1"}, true)

	a := att(t, e, id)
	assert.False(t, a.LastAttempt.Done(), "the stale run reached the replacement subject")
	assert.Zero(t, a.Failures)
}

// Remove waits for the run in flight, so past it nothing is still writing on the subject's
// behalf — which is what a caller about to release what that run wrote through needs.
func TestRemoveWaitsForTheRunInFlight(t *testing.T) {
	e, p, _ := single(t, Succeeded())
	inRun, letGo := testutil.NewSignal(), make(chan struct{})
	p.onRun = func() { inRun.Fire(); <-letGo }
	startSupervisor(t, e)

	e.Add(subj)
	inRun.Wait(t, "the run to be in flight")

	removed := make(chan struct{})
	go func() { defer close(removed); e.Remove(subj) }()
	testutil.NoRecv(t, removed, quietWindow, "Remove returned while its run was still out")

	close(letGo)
	testutil.Wait(t, removed, "Remove to join its run")
}

// --- wakes ---

// A name nothing was registered under is a wiring bug; a subject nothing tracks is not, since a
// claim can be released while a wake for it is in flight.
func TestWakeIgnoresAnUntrackedSubjectAndPanicsOnAnUnknownReconciler(t *testing.T) {
	e, _, id := single(t, Succeeded())
	e.Add(subj)
	e.settle()
	drainRuns(t, e)

	e.Wake("nobody-tracks-this", id)
	noRuns(t, e, "a wake reached a subject nothing tracks")

	assert.Panics(t, func() { e.Wake(subj, "nope") })
}

// Restart takes a subject nothing tracks as Wake does: a no-op, not a run.
func TestRestartIgnoresAnUntrackedSubject(t *testing.T) {
	e, _, id := single(t, Succeeded())
	e.Add(subj)
	e.settle()
	drainRuns(t, e)

	e.Restart("nobody-tracks-this", id)
	noRuns(t, e, "a restart reached a subject nothing tracks")
}

// A Wake landing mid-run is redelivered when that run commits: the run in flight had already
// passed the thing the Wake is about.
func TestAWakeMidRunEarnsAFreshRun(t *testing.T) {
	e, p, id := single(t, Suspend("ContextNotFound", ""))
	woke := false
	p.onRun = func() {
		if !woke {
			woke = true
			e.Wake(subj, id)
		}
	}
	e.Add(subj)
	e.settle()

	runNext(t, e)

	assert.Equal(t, keyOf(e, id), takeRun(t, e), "the mid-run Wake was folded into the run")
}

// A Wake overrides suspension — it is the one input the derivation cannot produce.
func TestAWakeRunsASuspendedReconciler(t *testing.T) {
	e, _, id := single(t, Suspend("ContextNotFound", ""))
	e.Add(subj)
	e.settle()
	runNext(t, e)
	noRuns(t, e, "a suspended registration was scheduled")

	e.Wake(subj, id)

	assert.Equal(t, keyOf(e, id), takeRun(t, e))
}

func TestWakeAllReachesEveryTrackedSubject(t *testing.T) {
	e, _, id := single(t, Suspend("ContextNotFound", ""))
	e.Add("ctx-1")
	e.Add("ctx-2")
	e.settle()
	seen := []key{runNext(t, e), runNext(t, e)} // the fresh subjects' own runs
	require.ElementsMatch(t, seen, []key{{"ctx-1", e.byName[id]}, {"ctx-2", e.byName[id]}})

	e.WakeAll(id)

	assert.ElementsMatch(t,
		[]key{takeRun(t, e), takeRun(t, e)},
		[]key{{"ctx-1", e.byName[id]}, {"ctx-2", e.byName[id]}})
}

// --- the schedule ---

// NextAttempt is the run in flight; writing a schedule over it would erase both the in-flight
// mark and the schedule it was dispatched on. Its commit passes again.
func TestARunInFlightIsLeftAlone(t *testing.T) {
	e, _, id := single(t, Skip())
	e.Add(subj)
	e.settle()
	require.Equal(t, keyOf(e, id), takeRun(t, e), "the fresh subject's own run")
	// The worker that took it marks the run before dropping the lock. Safe unlocked here: no
	// loops are running.
	e.subjects[subj].obs[e.byName[id]].begin(runAt)

	e.pass(subj)

	a := att(t, e, id)
	assert.Equal(t, runAt, a.NextAttempt.StartedAt, "the pass wrote over the run in flight")
	noRuns(t, e, "a second run of a registration already out")
}

// The success-path poll is the default; Suspend is the only opt-out.
func TestASucceededReconcilerIsDueAgainAfterItsInterval(t *testing.T) {
	e, _, id := single(t, Succeeded(), WithInterval(250*time.Millisecond))
	e.Add(subj)
	e.settle()

	runNext(t, e)

	a := att(t, e, id)
	require.True(t, a.OK())
	assert.Equal(t, a.LastAttempt.FinishedAt.Add(250*time.Millisecond), a.NextAttempt.ScheduledAt)
}

// A run that knows it should come back sooner says so, and the registration goes on bounding
// every run that does not.
func TestASucceededRunCanAskToComeBackSooner(t *testing.T) {
	e, _, id := single(t, Succeeded().RequeueAfter(50*time.Millisecond), WithInterval(250*time.Millisecond))
	e.Add(subj)
	e.settle()

	runNext(t, e)

	a := att(t, e, id)
	assert.Equal(t, a.LastAttempt.FinishedAt.Add(50*time.Millisecond), a.NextAttempt.ScheduledAt)
}

// The registration is the correctness bound, so a run asking for longer is ignored rather than
// obeyed: forget the ask and a subject is slower, never wrong.
func TestASucceededRunCannotAskToComeBackLater(t *testing.T) {
	e, _, id := single(t, Succeeded().RequeueAfter(time.Hour), WithInterval(250*time.Millisecond))
	e.Add(subj)
	e.settle()

	runNext(t, e)

	a := att(t, e, id)
	assert.Equal(t, a.LastAttempt.FinishedAt.Add(250*time.Millisecond), a.NextAttempt.ScheduledAt)
}

// The ladder widens per consecutive failure and holds at the cap — and it is a pure function of
// Failures, so a later pass derives the same ScheduledAt it derived before.
func TestAFailedReconcilerClimbsTheLadder(t *testing.T) {
	e, _, id := single(t, Fail("Unreachable", assert.AnError),
		WithBackoff(10*time.Millisecond, 2, 40*time.Millisecond))
	e.Add(subj)
	e.settle()

	for _, want := range []time.Duration{10, 20, 40, 40} {
		if att(t, e, id).LastAttempt.Done() {
			e.Wake(subj, id) // the scheduled retry is in the future; force it
		}
		runNext(t, e)

		a := att(t, e, id)
		assert.Equal(t, want*time.Millisecond, a.NextAttempt.ScheduledAt.Sub(a.LastAttempt.FinishedAt))

		e.passQ.Add(subj)
		e.settle()
		assert.Equal(t, a.NextAttempt.ScheduledAt, att(t, e, id).NextAttempt.ScheduledAt,
			"a second pass moved ScheduledAt over unchanged state")
	}
}

// Skip leaves no record — the previous answer stands — and nothing scheduled: only a Wake
// brings it back, so an unreadable source does not become a busy loop.
func TestASkipLeavesNoRecordAndNothingScheduled(t *testing.T) {
	e, p, id := single(t, Fail("ResolveFailed", assert.AnError))
	e.Add(subj)
	e.settle()
	runNext(t, e)
	failed := att(t, e, id).LastAttempt
	require.True(t, failed.Done())

	p.set(Skip())
	e.Wake(subj, "conn")
	runNext(t, e)

	a := att(t, e, id)
	assert.Equal(t, failed, a.LastAttempt, "a Skip must leave the last record standing")
	assert.False(t, a.Scheduled())
	assert.False(t, a.Suspended(), "a Skip is not a suspension, whatever it schedules")
	noRuns(t, e, "a skipped registration was re-dispatched")
}

// Every stamp the supervisor writes is read as a level, so two runs must never share an instant:
// a reader comparing them is asking whether something ran again, and a repeat answers no.
func TestTickNeverRepeatsAnInstant(t *testing.T) {
	// A clock that never moves, which is what a coarse one looks like within one of its ticks.
	frozen := time.Now()
	e := New(withNow(func() time.Time { return frozen }))
	t.Cleanup(func() { assert.NoError(t, e.Close()) })

	first := e.tick()
	assert.True(t, e.tick().After(first), "a repeated reading cannot say a second run happened")
}

// Now shares the clock the stamps come from, so a baseline read off it orders exactly against
// them — which is what a caller waiting for the run it asked for compares. The wall clock cannot
// answer that: tick's nudge can put a stamp ahead of a later wall reading.
func TestNowOrdersAgainstTheStampsItShares(t *testing.T) {
	e, p, id := single(t, Succeeded())
	e.Add(subj)
	e.settle()
	runNext(t, e)
	assert.True(t, e.Now().After(att(t, e, id).LastRunAt), "a run that finished before the ask is behind it")

	askedAt := e.Now()
	p.set(Succeeded())
	e.Wake(subj, "conn")
	runNext(t, e)
	assert.False(t, att(t, e, id).LastRunAt.Before(askedAt), "and the run after the ask answers it")
}

// A Skip leaves no record, so LastAttempt cannot say a run happened at all. LastRunAt is what
// does — the level a caller waiting on a run of its own reads, whatever that run concluded.
func TestASkipStampsTheRunItLeftNoRecordOf(t *testing.T) {
	e, p, id := single(t, Fail("ResolveFailed", assert.AnError))
	e.Add(subj)
	e.settle()
	runNext(t, e)
	recorded := att(t, e, id)
	require.Equal(t, recorded.LastAttempt.StartedAt, recorded.LastRunAt)

	p.set(Skip())
	e.Wake(subj, "conn")
	runNext(t, e)

	a := att(t, e, id)
	assert.Equal(t, recorded.LastAttempt, a.LastAttempt, "a Skip records nothing")
	assert.True(t, a.LastRunAt.After(recorded.LastRunAt), "and still stamps that it ran")
}

// The two ways to end up with nothing due read apart, which is what a caller waking whatever is
// parked needs: a suspension is waiting on that wake, and a Skip is waiting on the edge that
// asks for it again.
func TestASuspensionReadsApartFromASkip(t *testing.T) {
	e, p, id := single(t, Suspend("ContextNotFound", "the kubeconfig no longer names it"))
	e.Add(subj)
	e.settle()
	runNext(t, e)

	a := att(t, e, id)
	require.False(t, a.Scheduled())
	assert.True(t, a.Suspended())

	p.set(Skip())
	e.Wake(subj, "conn")
	runNext(t, e)

	assert.False(t, att(t, e, id).Suspended(),
		"a Skip over a suspension leaves the record standing but is not itself one")
}

// The timer only brings the pass back; the pass decides again per registration.
func TestTheTimerIsAWakeNotACadence(t *testing.T) {
	// Wide enough that the retry is still in the future when the pass derives the schedule:
	// a rung shorter than a loaded machine's own scheduling noise is already due by then, and
	// a due run is dispatched rather than timed, so there is no timer left to observe.
	e, _, id := single(t, Fail("Unreachable", assert.AnError),
		WithBackoff(100*time.Millisecond, 2, 100*time.Millisecond))
	e.Add(subj)
	e.settle()

	runNext(t, e) // the failure schedules a retry, which arms the timer
	require.True(t, att(t, e, id).Scheduled(), "a retry already due is dispatched, never timed")

	name, ok := e.passQ.Next(within(t))
	require.True(t, ok, "the timer never asked for a pass")
	e.passQ.Done(name)
	assert.Equal(t, subj, name)
}

// --- dependencies ---

// Nothing to say about a server nobody has tried: the dependent stays untouched rather than
// recording a dependency that has not failed.
func TestADependentIsUntouchedBeforeItsDependenciesAnswer(t *testing.T) {
	e, _, _, aID, bID := pair(t, Skip(), Succeeded())
	e.Add(subj)
	e.settle()

	require.Equal(t, keyOf(e, aID), takeRun(t, e))
	b := att(t, e, bID)
	assert.False(t, b.LastAttempt.Done())
	assert.False(t, b.Scheduled())
}

// One run records DependencyFailed — never dialed, so no StartedAt — and the rest of the outage
// costs nothing: one timeout per cycle, not one per job.
func TestADependentRecordsDependencyFailedOnceThenSuspends(t *testing.T) {
	e, _, bBody, aID, bID := pair(t, Fail("Unreachable", assert.AnError), Succeeded())
	e.Add(subj)
	e.settle()
	require.Equal(t, keyOf(e, aID), runNext(t, e), "the connection answers first")

	require.Equal(t, keyOf(e, bID), runNext(t, e), "the failure makes the dependent due once")

	b := att(t, e, bID)
	assert.Equal(t, VerdictSuspended, b.LastAttempt.Verdict)
	assert.Equal(t, ReasonDependencyFailed, b.LastAttempt.Reason)
	assert.True(t, b.LastAttempt.StartedAt.IsZero(), "recorded, never dispatched")
	assert.Zero(t, b.Failures, "the dependency carries the streak, not the dependent")
	assert.Zero(t, bBody.count(), "the body must not have run")

	e.Wake(subj, aID)
	runNext(t, e) // the outage continues
	noRuns(t, e, "the dependent was recorded again for the same outage")
}

// A suspended dependent has nothing scheduled, so the recovery re-arm is read off the state —
// nothing has to notice it and go looking for what suspended on it.
func TestARecoveredDependencyMakesItsDependentsDue(t *testing.T) {
	e, aBody, _, aID, bID := pair(t, Fail("Unreachable", assert.AnError), Succeeded())
	e.Add(subj)
	e.settle()
	runNext(t, e) // connection fails
	runNext(t, e) // dependent records DependencyFailed

	aBody.set(Succeeded())
	e.Wake(subj, aID)
	runNext(t, e) // connection recovers

	assert.Equal(t, keyOf(e, bID), takeRun(t, e))
}

// A dependency that failed between the pass and the worker picking the key up means the run is
// recorded, not dialed — a worker must not spend a timeout learning what the state already says.
func TestDependenciesAreRecheckedAtDispatch(t *testing.T) {
	e, aBody, bBody, aID, bID := pair(t, Succeeded(), Succeeded())
	e.Add(subj)
	e.settle()
	runNext(t, e) // connection succeeds; the dependent is now queued

	aBody.set(Fail("Unreachable", assert.AnError))
	e.runOne(t.Context(), keyOf(e, aID), func() {}) // the connection dies before b dispatches
	e.settle()
	require.Equal(t, keyOf(e, bID), runNext(t, e))

	assert.Zero(t, bBody.count(), "the body dialed a server the state already said was down")
	assert.Equal(t, ReasonDependencyFailed, att(t, e, bID).LastAttempt.Reason)
}

// A wake can outrun the pass and dispatch a job whose dependency has never answered. The run
// ends as a no-op — nothing to say about a server nobody has tried — and the pass owns the
// question again.
func TestARunDispatchedBeforeItsDependencyAnsweredRecordsNothing(t *testing.T) {
	e, _, b, _, bName := pair(t, Skip(), Succeeded())
	e.Add(subj)
	e.settle()
	drainRuns(t, e)

	e.Wake(subj, bName)
	runNext(t, e)

	assert.Zero(t, b.count(), "the body ran with nothing to run over")
	at := att(t, e, bName)
	assert.False(t, at.LastAttempt.Done(), "an untouched registration records nothing")
}

// --- the data edge ---

// The gap the data edge closes: a dependent parked on its interval goes on using a value that
// moved under it, because health did not change and nothing else re-arms it.
func TestAChangedValueWakesAWatcher(t *testing.T) {
	e, a, _, aID, bID := linked(t, Succeeded(), Succeeded())
	a.commits("v1")
	settled(t, e)

	a.commits("v2")
	e.Wake(subj, aID)
	runNext(t, e)

	assert.Equal(t, keyOf(e, bID), takeRun(t, e))
}

// The supervisor never compares values — the body says whether its answer moved by committing or
// not. Without this the wake fires every cycle and the interval stops meaning anything.
func TestACommitThatCarriedNoValueWakesNobody(t *testing.T) {
	e, a, _, aID, _ := linked(t, Succeeded(), Succeeded())
	a.commits("v1")
	settled(t, e)

	a.commitsNothing()
	e.Wake(subj, aID)
	runNext(t, e)

	noRuns(t, e, "an unchanged answer woke a dependent")
}

// A wake goes through the run queue, so it overrides a suspension exactly as Wake does — which
// is what re-asks a job parked "for the life of the connection" when a new one arrives.
func TestAChangedValueWakesASuspendedWatcher(t *testing.T) {
	e, a, _, aID, bID := linked(t, Succeeded(), Suspend("Unsupported", "no such endpoint"))
	a.commits("v1")
	settled(t, e)
	require.False(t, att(t, e, bID).Scheduled(), "the dependent is suspended")

	a.commits("v2")
	e.Wake(subj, aID)
	runNext(t, e)

	assert.Equal(t, keyOf(e, bID), takeRun(t, e))
}

// The data edge does not consult health: a registration declaring only a dependency must not be gated
// by health it never declared, so a failing run that learned something still wakes it.
func TestAFailedRunThatChangedItsValueStillWakes(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	a := &steered{res: Succeeded()}
	watcher := &steered{res: Succeeded()}
	RegisterJob(e, "conn", a)
	RegisterJob(e, "watcher", watcher, WithWatches("conn"))
	a.commits("v1")
	settled(t, e)

	a.set(Fail("Unreachable", assert.AnError))
	a.commits("v2")
	e.Wake(subj, "conn")
	runNext(t, e)

	assert.Equal(t, keyOf(e, "watcher"), takeRun(t, e))
}

// A wake is not a way past a dependency. The dependent here is already suspended for the
// outage, so only the data edge can reach it — and what it earns is a record, never a dial: the
// edge costs a run, never a timeout, and "one timeout per cycle" survives it.
func TestAWokenRunWithAFailingDependencyRecordsRatherThanDialing(t *testing.T) {
	e, a, b, aID, bID := linked(t, Succeeded(), Succeeded())
	a.commits("v1")
	settled(t, e)
	a.set(Fail("Unreachable", assert.AnError))
	a.commitsNothing()
	e.Wake(subj, aID)
	runNext(t, e)                                  // the connection fails
	require.Equal(t, keyOf(e, bID), runNext(t, e)) // the dependent suspends on it
	require.Equal(t, ReasonDependencyFailed, att(t, e, bID).LastAttempt.Reason)
	noRuns(t, e, "the dependent is suspended for the rest of the outage")
	before := b.count()

	a.commits("v2") // still failing, but it learned something
	e.Wake(subj, aID)
	runNext(t, e)
	require.Equal(t, keyOf(e, bID), runNext(t, e), "the data edge reached it")

	assert.Equal(t, before, b.count(), "the body dialed a server the state already said was down")
	assert.Equal(t, ReasonDependencyFailed, att(t, e, bID).LastAttempt.Reason)
}

// Watches take IDs Register already returned, like dependencies, so a cascade only ever runs
// forward and the wiring bug is caught at registration.
func TestRegisterPanicsOnAWatchNotYetRegistered(t *testing.T) {
	e := New()

	assert.Panics(t, func() { RegisterJob(e, "uid", &steered{}, WithWatches("nope")) })
}

// --- what a run records ---

// The last value a run commits is the one that lands, and it lands once: one attempt, and one
// wake for whoever watches it.
func TestARunCommitsTheLastValueItRecorded(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	RegisterJob(e, "conn", jobFunc(func(_ context.Context, pass *JobPass[string]) Result {
		pass.Commit("v1")
		pass.Commit("v2")
		return Succeeded()
	}))
	RegisterJob(e, "uid", &steered{res: Succeeded()}, WithWatches("conn"))
	e.Add(subj)
	e.settle()

	runNext(t, e)

	read, _ := e.Read(subj)
	assert.Equal(t, "v2", GetJobObservation[string](read, "conn").Value)
	assert.Equal(t, keyOf(e, "uid"), takeRun(t, e))
	noRuns(t, e, "the watcher was woken more than once")
}

// A body may record what it found before it works out how to classify it, so a run that ends in
// Skip discards the value with the record — Skip records nothing, and that has to mean nothing.
func TestARunThatSkipsCommitsNothingItRecorded(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	RegisterJob(e, "conn", jobFunc(func(_ context.Context, pass *JobPass[string]) Result {
		pass.Commit("v1")
		return Skip()
	}))
	e.Add(subj)
	e.settle()

	runNext(t, e)

	read, _ := e.Read(subj)
	o := GetJobObservation[string](read, "conn")
	assert.False(t, o.Known(), "a skipped run published a value")
	assert.Empty(t, o.Value)
}

// Containment is all or nothing: a panicking body's buffered value goes with the wreckage, or a
// half-finished run publishes an answer the record beside it calls Internal.
func TestAPanickingRunCommitsNothingItRecorded(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	RegisterJob(e, "conn", jobFunc(func(_ context.Context, pass *JobPass[string]) Result {
		pass.Commit("v1")
		panic("boom")
	}))
	e.Add(subj)
	e.settle()

	runNext(t, e)

	read, _ := e.Read(subj)
	o := GetJobObservation[string](read, "conn")
	assert.Empty(t, o.Value, "a panicking run published a value")
	assert.False(t, o.Known())
	assert.Equal(t, ReasonInternal, o.LastAttempt.Reason)
}

// --- failure containment ---

// A panicking body must still produce a record and free its key — otherwise the registration reads as
// in flight forever and the key stays held in the queue.
func TestAPanickingRunRecordsInternalAndFreesItsKey(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	RegisterJob(e, "conn", jobFunc(func(context.Context, *JobPass[string]) Result {
		panic("boom")
	}))
	e.Add(subj)
	e.settle()

	runNext(t, e)

	a := att(t, e, "conn")
	assert.Equal(t, VerdictFailed, a.LastAttempt.Verdict)
	assert.Equal(t, ReasonInternal, a.LastAttempt.Reason)
	assert.False(t, a.InFlight())

	e.Wake(subj, "conn")
	assert.Equal(t, keyOf(e, "conn"), takeRun(t, e), "the key stayed held")
}

// A body that hands back nothing is the same class of bug as one that panics, and is contained
// the same way. Panicking over it would take the supervisor's lock down with the worker.
func TestARunThatReturnsNoResultIsRecordedAsInternal(t *testing.T) {
	e, _, id := single(t, Result{})
	e.Add(subj)
	e.settle()

	runNext(t, e)

	a := att(t, e, id)
	assert.Equal(t, VerdictFailed, a.LastAttempt.Verdict)
	assert.Equal(t, ReasonInternal, a.LastAttempt.Reason)
	assert.False(t, a.InFlight())
}

// The supervisor deadlines the context it hands the run; the body classifies the expiry itself.
func TestARunIsBoundedByItsTimeout(t *testing.T) {
	e, p, _ := single(t, Succeeded(), WithTimeout(50*time.Millisecond))
	e.Add(subj)
	e.settle()

	runNext(t, e)

	p.mu.Lock()
	defer p.mu.Unlock()
	assert.True(t, p.sawDeadline)
}

// --- publishing ---

// The supervisor reports every pass; the caller decides what is news.
func TestOnPassFiresAfterEveryPassInOrderPerSubject(t *testing.T) {
	e, _, id := single(t, Succeeded())
	var calls []Snapshot
	e.OnPass(func(name string, snap Snapshot) {
		require.Equal(t, subj, name)
		calls = append(calls, snap)
	})
	e.Add(subj)
	e.settle()
	require.Len(t, calls, 1, "the fresh subject's own pass")
	assert.False(t, calls[0].Attempts(id).LastAttempt.Done())

	runNext(t, e)

	require.Len(t, calls, 2)
	assert.True(t, calls[1].Attempts(id).OK(), "the second call carries the committed run")
}

// A run's whole round-trip is publishable state, not just its two ends: beginning asks for a
// pass, so the window a probe spends dialing reaches a reader as a run in flight.
//
// The run is held open on a channel because that is what makes the in-flight pass observable —
// released at once, its begin- and commit-passes coalesce into one and there is nothing to see.
func TestOnPassFiresWhileARunIsInFlight(t *testing.T) {
	e, p, id := single(t, Succeeded())
	inRun, letGo := testutil.NewSignal(), make(chan struct{})
	release := sync.OnceFunc(func() { close(letGo) })
	p.onRun = func() { inRun.Fire(); <-letGo }

	var mu sync.Mutex
	var sawInFlight, sawCommitted bool
	e.OnPass(func(_ string, snap Snapshot) {
		mu.Lock()
		defer mu.Unlock()
		a := snap.Attempts(id)
		sawInFlight = sawInFlight || a.InFlight()
		sawCommitted = sawCommitted || a.LastAttempt.Done()
	})
	saw := func(flag *bool) func() bool {
		return func() bool { mu.Lock(); defer mu.Unlock(); return *flag }
	}
	stop := e.Start(t.Context())
	t.Cleanup(func() { assert.NoError(t, stop(context.Background())) })
	// Registered after the stop cleanup so it runs before it: a failed assertion must not leave
	// the run held while stop waits for it.
	t.Cleanup(release)

	e.Add(subj)
	inRun.Wait(t, "the run to be in flight")
	require.Eventually(t, saw(&sawInFlight), testutil.Timeout, time.Millisecond,
		"a pass taken while the run was out")

	release()
	require.Eventually(t, saw(&sawCommitted), testutil.Timeout, time.Millisecond,
		"a pass carrying the committed run")
}

// LastSeenAt dates the value, not the run: a failure that follows does not un-read what was read,
// so the value and its provenance both stand.
func TestAFailureLeavesTheValueAndItsLastSeenStanding(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	uid := "uid-1"
	p := &steered{res: Succeeded(), next: &uid}
	RegisterJob(e, "conn", p)
	e.Add(subj)
	e.settle()
	runNext(t, e)
	read, _ := e.Read(subj)
	seen := GetJobObservation[string](read, "conn")
	require.True(t, seen.Known())

	p.set(Fail("Unreachable", assert.AnError))
	p.commitsNothing()
	e.Wake(subj, "conn")
	runNext(t, e)

	read, _ = e.Read(subj)
	after := GetJobObservation[string](read, "conn")
	assert.Equal(t, "uid-1", after.Value, "the value outlives the failure")
	assert.Equal(t, seen.LastSeenAt, after.LastSeenAt, "a failure that read nothing is not a read")
	assert.False(t, after.OK())
}

// A success dates the value it confirmed, so a run that has never had a value to confirm dates
// nothing — otherwise Known() passes and hands the reader a zero it will dereference.
func TestASuccessWithNoValueEverCommittedIsNotKnown(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	RegisterJob(e, "conn", &steered{res: Succeeded()}) // succeeds, commits nothing
	e.Add(subj)
	e.settle()

	runNext(t, e)

	read, _ := e.Read(subj)
	o := GetJobObservation[string](read, "conn")
	require.True(t, o.OK(), "the run did succeed")
	assert.False(t, o.Known(), "there is no value to read")
	assert.True(t, o.LastSeenAt.IsZero())
}

// Every success dates the value, whether or not it committed a new one: a run that found the
// answer unchanged still read it.
func TestASuccessWithNoNewValueStillAdvancesLastSeen(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	uid := "uid-1"
	p := &steered{res: Succeeded(), next: &uid}
	RegisterJob(e, "conn", p)
	e.Add(subj)
	e.settle()
	runNext(t, e)
	read, _ := e.Read(subj)
	first := GetJobObservation[string](read, "conn")

	p.mu.Lock()
	p.next = nil
	p.mu.Unlock()
	e.Wake(subj, "conn")
	runNext(t, e)

	read, _ = e.Read(subj)
	after := GetJobObservation[string](read, "conn")
	assert.Equal(t, "uid-1", after.Value, "nothing new to commit, so the value stands")
	assert.Equal(t, after.LastAttempt.FinishedAt, after.LastSeenAt, "the run that confirmed it")
	assert.True(t, after.LastSeenAt.After(first.LastSeenAt))
}

func TestReadReportsAnUntrackedSubject(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	uid := "uid-1"
	RegisterJob(e, "conn", &steered{res: Succeeded(), next: &uid})

	_, ok := e.Read(subj)
	require.False(t, ok)

	e.Add(subj)
	e.settle()
	runNext(t, e)

	v, ok := e.Read(subj)
	require.True(t, ok)
	o := GetJobObservation[string](v, "conn")
	assert.Equal(t, "uid-1", o.Value, "the run's committed value")
	assert.True(t, o.OK())
	assert.True(t, o.Known(), "the supervisor stamps when the value was read")
}

// A run that hands back no value keeps the previous one: an Observation outlives the failure
// that follows it, and the failing run is still recorded beside it.
func TestANilValueKeepsThePreviousOne(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	uid := "uid-1"
	p := &steered{res: Succeeded(), next: &uid}
	RegisterJob(e, "conn", p)
	e.Add(subj)
	e.settle()
	runNext(t, e)

	p.set(Fail("Unreachable", assert.AnError))
	p.next = nil
	e.Wake(subj, "conn")
	runNext(t, e)

	v, ok := e.Read(subj)
	require.True(t, ok)
	o := GetJobObservation[string](v, "conn")
	assert.Equal(t, "uid-1", o.Value, "the failure erased the last answer")
	assert.True(t, o.Known())
	assert.False(t, o.OK())
}

func TestCloseStopsTheTimersAndTheQueues(t *testing.T) {
	e, _, _ := single(t, Fail("Unreachable", assert.AnError))
	e.Add(subj)
	e.settle()
	runNext(t, e)
	sub := e.subjects[subj]
	require.NotNil(t, sub.timer)

	require.NoError(t, e.Close())

	assert.Nil(t, sub.timer)
	assert.Empty(t, e.subjects)
	_, ok := e.runQ.Next(within(t))
	assert.False(t, ok, "the run queue outlived Close")
}

// --- values the supervisor drops ---

// discarding is a job whose committed values it records when the supervisor hands them back.
type discarding struct {
	steered
	discarded *testutil.Probe[string]
	// panicAfterCommit is a body that commits and then panics, which steered cannot express:
	// its hook runs before the commit.
	panicAfterCommit string
}

func (p *discarding) Discard(v string) { p.discarded.Fire(v) }

func (p *discarding) Run(ctx context.Context, pass *JobPass[string]) Result {
	if p.panicAfterCommit != "" {
		pass.Commit(p.panicAfterCommit)
		panic("job body bug")
	}
	return p.steered.Run(ctx, pass)
}

// A committed value can own something — a connection, a file — so a value the supervisor drops is
// handed back rather than dropped silently: nothing else can reach it to release it.
func TestARefusedCommitIsHandedBack(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	p := &discarding{discarded: testutil.NewProbe[string](4)}
	p.res = Succeeded()
	p.commits("built")
	// The subject going while the run is out is the race a Release loses: Remove drops the
	// name under the lock before it joins, so the commit arrives against a subject that has
	// gone.
	inRun, letGo := testutil.NewSignal(), make(chan struct{})
	p.onRun = func() { inRun.Fire(); <-letGo }
	RegisterJob(e, "conn", p)
	stop := e.Start(t.Context())
	t.Cleanup(func() { assert.NoError(t, stop(context.Background())) })

	e.Add("prod")
	inRun.Wait(t, "the run to be in flight")
	removed := make(chan struct{})
	go func() { defer close(removed); e.Remove("prod") }()
	require.Eventually(t, func() bool { _, ok := e.Read("prod"); return !ok },
		testutil.Timeout, time.Millisecond, "the subject to be dropped")

	close(letGo)
	testutil.Wait(t, removed, "Remove to join its run")
	assert.Equal(t, "built", p.discarded.Await(t, "the refused value"))
}

// A Skip records nothing, so a value the body committed before concluding one goes the same way.
func TestASkippedRunsValueIsHandedBack(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	p := &discarding{discarded: testutil.NewProbe[string](4)}
	p.res = Skip()
	p.commits("built")
	RegisterJob(e, "conn", p)
	stop := e.Start(t.Context())
	t.Cleanup(func() { assert.NoError(t, stop(context.Background())) })

	e.Add("prod")

	assert.Equal(t, "built", p.discarded.Await(t, "the skipped value"))
}

// A body that panics has still committed whatever it committed before it did, and the supervisor
// applies none of it — so that value is handed back too, or a panic leaks what it built.
func TestAPanickingRunsValueIsHandedBack(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	p := &discarding{discarded: testutil.NewProbe[string](4), panicAfterCommit: "built"}
	p.res = Succeeded()
	RegisterJob(e, "conn", p)
	stop := e.Start(t.Context())
	t.Cleanup(func() { assert.NoError(t, stop(context.Background())) })

	e.Add("prod")

	assert.Equal(t, "built", p.discarded.Await(t, "the panicking run's value"))
}

// The zero Result is a body bug the supervisor records as a failure, so a value committed before
// it is applied nowhere and goes back.
func TestAZeroResultsValueIsHandedBack(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	p := &discarding{discarded: testutil.NewProbe[string](4)}
	p.commits("built")
	RegisterJob(e, "conn", p)
	stop := e.Start(t.Context())
	t.Cleanup(func() { assert.NoError(t, stop(context.Background())) })

	e.Add("prod")

	assert.Equal(t, "built", p.discarded.Await(t, "the zero Result's value"))
}

// A run Restart stopped records nothing, so what it committed on the way out goes back.
func TestAStoppedRunsValueIsHandedBack(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	p := &discarding{discarded: testutil.NewProbe[string](4)}
	p.res = Succeeded()
	p.commits("built")
	inRun, letGo := testutil.NewSignal(), make(chan struct{})
	var started atomic.Bool
	p.onRun = func() {
		if started.CompareAndSwap(false, true) {
			inRun.Fire()
			<-letGo
		}
	}
	RegisterJob(e, "conn", p, WithInterval(time.Hour))
	stop := e.Start(t.Context())
	t.Cleanup(func() { assert.NoError(t, stop(context.Background())) })

	e.Add("prod")
	inRun.Wait(t, "the run to be in flight")
	e.Restart("prod", "conn")
	close(letGo)

	assert.Equal(t, "built", p.discarded.Await(t, "the stopped run's value"))
}

// The ordinary path: a value the supervisor applied is the job's own to read back, and handing it
// back as dropped would have a caller release what is live.
func TestAnAppliedValueIsNotHandedBack(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	p := &discarding{discarded: testutil.NewProbe[string](4)}
	p.res = Succeeded()
	p.commits("built")
	RegisterJob(e, "conn", p)
	stop := e.Start(t.Context())
	t.Cleanup(func() { assert.NoError(t, stop(context.Background())) })

	e.Add("prod")

	require.Eventually(t, func() bool {
		snap, ok := e.Read("prod")
		return ok && GetJobObservation[string](snap, "conn").Value == "built"
	}, testutil.Timeout, time.Millisecond, "the value to land")
	_, dropped := p.discarded.TryAwait()
	assert.False(t, dropped, "an applied value is not a dropped one")
}

// The supervisor is what tells a body a value has landed, so a job committing the zero T lands it
// once and reads as known from then on — without which such a job reports itself never
// observed for as long as its answer stays the zero value.
func TestTheZeroValueLandsAndReadsAsKnown(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	seen := testutil.NewProbe[bool](4)
	RegisterJob(e, "conn", jobFunc(func(_ context.Context, pass *JobPass[string]) Result {
		seen.Fire(pass.Known())
		if !pass.Known() {
			pass.Commit("")
		}
		return Succeeded()
	}), WithInterval(time.Millisecond))
	stop := e.Start(t.Context())
	t.Cleanup(func() { assert.NoError(t, stop(context.Background())) })

	e.Add("prod")

	assert.False(t, seen.Await(t, "the first run"), "nothing has landed yet")
	assert.True(t, seen.Await(t, "the run after it"), "the zero value it committed")
	require.Eventually(t, func() bool {
		snap, ok := e.Read("prod")
		return ok && GetJobObservation[string](snap, "conn").Known()
	}, testutil.Timeout, time.Millisecond, "the observation to date itself")
}

// A run that failed can still have read something — which components are down — so the value it
// commits is dated now. Dating it by the last success instead leaves a value nothing has ever
// confirmed, and a job that has only ever failed reads as never observed while holding the
// answer a caller came for.
func TestAFailedRunsCommittedValueIsDated(t *testing.T) {
	e, p, name := single(t, Fail("Unreachable", assert.AnError))
	p.commits("etcd")
	stop := e.Start(t.Context())
	t.Cleanup(func() { assert.NoError(t, stop(context.Background())) })

	e.Add("prod")

	require.Eventually(t, func() bool {
		snap, ok := e.Read("prod")
		return ok && GetJobObservation[string](snap, name).Known()
	}, testutil.Timeout, time.Millisecond, "the failing answer to be dated")
	snap, _ := e.Read("prod")
	assert.Equal(t, "etcd", GetJobObservation[string](snap, name).Value)
	assert.False(t, GetJobObservation[string](snap, name).OK(), "dated, and still a failure")
}

// A success that commits nothing re-confirms what stands, which is what makes "identified, as of
// 10:00" readable — but a success with nothing to date must leave seen alone, or a reader's Known
// guard passes and hands it the zero value.
func TestASuccessWithNothingToDateLeavesTheObservationUnknown(t *testing.T) {
	e, _, name := single(t, Succeeded())
	stop := e.Start(t.Context())
	t.Cleanup(func() { assert.NoError(t, stop(context.Background())) })

	e.Add("prod")

	require.Eventually(t, func() bool {
		snap, ok := e.Read("prod")
		return ok && GetJobObservation[string](snap, name).OK()
	}, testutil.Timeout, time.Millisecond, "the run to land")
	snap, _ := e.Read("prod")
	assert.False(t, GetJobObservation[string](snap, name).Known(), "nothing was ever read")
}

// The supervisor hands back every value it stops holding. A value owning a connection was fine
// to drop — the pool retires it — but one owning a goroutine leaks unless the job is told,
// so dropping the subject is a hand-back like a refused commit is.
func TestRemoveHandsBackTheStandingValue(t *testing.T) {
	e, p := standing(t, "built")

	e.Remove("prod")

	assert.Equal(t, "built", p.discarded.Await(t, "the standing value"))
}

func TestCloseHandsBackEveryStandingValue(t *testing.T) {
	e, p := standing(t, "built")

	require.NoError(t, e.Close())

	assert.Equal(t, "built", p.discarded.Await(t, "the standing value"))
}

// A subject with nothing committed has nothing to hand back, and a second Remove has nothing
// left: a handle is handed back exactly once, or a caller would join a goroutine twice.
func TestAValueIsHandedBackOnceAndOnlyIfThereIsOne(t *testing.T) {
	e, p := standing(t, "built")

	e.Remove("prod")
	assert.Equal(t, "built", p.discarded.Await(t, "the standing value"))

	e.Remove("prod")
	// "empty" may run before its Remove, so its runs must commit nothing for it to stay empty.
	p.commitsNothing()
	e.Add("empty")
	e.Remove("empty")
	require.NoError(t, e.Close())

	// Deterministic rather than a window: Remove and Close discard inline, so anything they
	// hand back has fired by the time they return.
	_, more := p.discarded.TryAwait()
	assert.False(t, more, "nothing more to hand back")
}

// standing is a supervisor holding one committed value, ready for whatever drops it.
func standing(t *testing.T, value string) (*Supervisor, *discarding) {
	t.Helper()
	e := New()
	p := &discarding{discarded: testutil.NewProbe[string](4)}
	p.res = Succeeded()
	p.commits(value)
	RegisterJob(e, "conn", p)
	stop := e.Start(t.Context())
	t.Cleanup(func() { assert.NoError(t, stop(context.Background())) })

	e.Add("prod")
	require.Eventually(t, func() bool {
		snap, ok := e.Read("prod")
		return ok && GetJobObservation[string](snap, "conn").Known()
	}, testutil.Timeout, time.Millisecond, "the value to stand")
	return e, p
}

// A commit REPLACES the standing value and does not hand it back — the one place the supervisor
// drops a value without telling the job. A commit often carries the last value's holdings
// forward: kubeconn's connInfo is a struct whose runs commit a copy with one field moved and the
// same live connection inside, and handing that back would retire a connection still in use.
// What a run stops holding is the run's to release before it commits.
func TestAReplacedValueIsNotHandedBack(t *testing.T) {
	e, p := standing(t, "first")

	p.commits("second")
	e.Wake("prod", "conn")

	require.Eventually(t, func() bool {
		snap, ok := e.Read("prod")
		return ok && GetJobObservation[string](snap, "conn").Value == "second"
	}, testutil.Timeout, time.Millisecond, "the committed value to stand")

	_, handedBack := p.discarded.TryAwait()
	assert.False(t, handedBack, "a replaced value stays the job's to reason about")
}

// --- workers ---

// steeredWorker is a Worker the test drives: every run reports the pass it was handed and then
// blocks, so a test decides when — and how — it stops. Its whole shape is what a worker is: the
// run outlives the call that started it.
type steeredWorker struct {
	started *testutil.Probe[*WorkerPass[string]]
	exits   chan Result

	mu          sync.Mutex
	runs        int
	sawDeadline bool
	// onCancel is what the run returns when its context ends rather than the test handing it a
	// result. Succeeded is the ordinary reading — the supervisor asked it to stop. readyOnCancel
	// makes it call Ready on the way out, standing in for a first frame that arrived as the
	// cancel did.
	onCancel      Result
	readyOnCancel bool
}

func newSteeredWorker() *steeredWorker {
	return &steeredWorker{
		started:  testutil.NewProbe[*WorkerPass[string]](8),
		exits:    make(chan Result),
		onCancel: Succeeded(),
	}
}

func (w *steeredWorker) Run(ctx context.Context, pass *WorkerPass[string]) Result {
	w.mu.Lock()
	w.runs++
	_, w.sawDeadline = ctx.Deadline()
	w.mu.Unlock()

	w.started.Fire(pass)
	select {
	case res := <-w.exits:
		return res
	case <-ctx.Done():
		w.mu.Lock()
		res, readyLate := w.onCancel, w.readyOnCancel
		w.mu.Unlock()
		if readyLate {
			pass.Ready()
		}
		return res
	}
}

func (w *steeredWorker) count() int { w.mu.Lock(); defer w.mu.Unlock(); return w.runs }

func (w *steeredWorker) cancelledWith(res Result) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onCancel = res
}

// readiesOnCancel makes the run call Ready as it unwinds, which is a first frame landing at the
// same moment as the cancel that ended the start.
func (w *steeredWorker) readiesOnCancel() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.readyOnCancel = true
}

// exit hands the running worker its result and waits for it to be taken, so what follows is
// measured against a run that is on its way out rather than one still parked.
func (w *steeredWorker) exit(t *testing.T, res Result) {
	t.Helper()
	select {
	case w.exits <- res:
	case <-within(t).Done():
		t.Fatal("no worker was waiting to be given a result")
	}
}

// runningWorker is a started supervisor with one steered worker and its subject tracked, settled
// on its first run being in flight — and the pass that run was handed, which is how a test says
// the worker is ready or makes it report.
func runningWorker(t *testing.T, opts ...RegistrationOption) (*Supervisor, *steeredWorker, *WorkerPass[string]) {
	t.Helper()
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	w := newSteeredWorker()
	RegisterWorker(e, "sync", w, opts...)
	startSupervisor(t, e)
	e.Add(subj)
	return e, w, w.started.Await(t, "the worker's first run")
}

// awaitAttempts settles on one registration's bookkeeping. Under a running supervisor a run is
// recorded on its own goroutine, so everything about an exit is read by waiting for it.
func awaitAttempts(t *testing.T, e *Supervisor, name, what string, match func(Attempts) bool) Attempts {
	t.Helper()
	var last Attempts
	require.Eventually(t, func() bool {
		snap, ok := e.Read(subj)
		if !ok {
			return false
		}
		last = snap.Attempts(name)
		return match(last)
	}, testutil.Timeout, time.Millisecond, what)
	return last
}

func hasRecorded(a Attempts) bool { return a.LastAttempt.Done() }

// A worker's Run blocks for its whole life, and the supervisor never runs a second one beside it:
// the key is held for the run, so the schedule cannot reach a registration that is already up.
func TestAWorkerRunsUntilItIsStopped(t *testing.T) {
	e, w, _ := runningWorker(t, WithInterval(time.Millisecond))

	noRuns(t, e, "a second run was dispatched over a worker that is already up")
	assert.Equal(t, 1, w.count())
	assert.True(t, att(t, e, "sync").InFlight())
}

// A worker reports while it runs, which is the whole point of one: a commit is applied the moment
// it is made rather than at an end the worker does not have.
func TestAWorkersCommitIsVisibleBeforeItsRunEnds(t *testing.T) {
	e, _, pass := runningWorker(t)

	pass.Commit("watching")

	awaitAttempts(t, e, "sync", "the live commit to land", func(a Attempts) bool {
		snap, _ := e.Read(subj)
		return GetWorkerObservation[string](snap, "sync").Value == "watching"
	})
	assert.True(t, att(t, e, "sync").InFlight(), "the run is still going")
}

// Ready releases the slot while the worker keeps running, which is what makes the cap a bound on
// STARTS rather than on streams. A job holds its slot to the end, having nothing to release early.
func TestReadyReleasesTheSlotAndAJobHoldsItToTheEnd(t *testing.T) {
	e, _, pass := runningWorker(t)
	require.Len(t, e.slots, 1, "the starting worker holds a slot")

	pass.Ready()

	require.Eventually(t, func() bool { return len(e.slots) == 0 }, testutil.Timeout, time.Millisecond,
		"the slot to be released at Ready")
	assert.True(t, att(t, e, "sync").Ready())
}

// A second start cannot begin while the cap is full, so a supervisor of one admits one cold list
// at a time however many subjects are due.
func TestStartsAreBoundedByTheSlotCount(t *testing.T) {
	e := New(WithStartConcurrency(1))
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	w := newSteeredWorker()
	RegisterWorker(e, "sync", w)
	startSupervisor(t, e)

	e.Add("a")
	e.Add("b")
	w.started.Await(t, "the first start")

	testutil.NoRecv(t, w.started.Chan(), quietWindow, "a second start ran while the one slot was taken")

	// Ready frees the slot, and the queued start goes at once.
	w.started.Drain()
	e.Wake("a", "sync")
	assert.Equal(t, 1, w.count(), "nothing new started")
}

// --- the exit table ---

// A clean stop after Ready is a rotation: the rows stayed current across it, so it is recorded a
// success and paced by the floor rather than the ladder.
func TestAWorkerThatStopsCleanlyAfterReadyIsASuccess(t *testing.T) {
	e, w, pass := runningWorker(t, WithInterval(time.Hour))
	pass.Ready()

	w.exit(t, Succeeded())

	a := awaitAttempts(t, e, "sync", "the exit to be recorded", hasRecorded)
	assert.Equal(t, VerdictSucceeded, a.LastAttempt.Verdict)
	assert.Zero(t, a.Failures)
	assert.False(t, a.HealthySince.IsZero(), "the stretch it opened at Ready stands")
}

// A worker that stops cleanly having never proved it was up has proved nothing, and restarting it
// at the floor would hot-loop against a server that accepts every start and drops it. So the
// clean exit is recorded a FAILURE, and the ladder paces the retry.
func TestAWorkerThatStopsBeforeReadyIsNeverReady(t *testing.T) {
	e, w, _ := runningWorker(t, WithInterval(time.Hour))

	w.exit(t, Succeeded())

	a := awaitAttempts(t, e, "sync", "the exit to be recorded", hasRecorded)
	assert.Equal(t, VerdictFailed, a.LastAttempt.Verdict)
	assert.Equal(t, ReasonNeverReady, a.LastAttempt.Reason)
	assert.Equal(t, 1, a.Failures, "the ladder, not the floor")
	assert.True(t, a.HealthySince.IsZero())
}

// A clean exit is what resets the count NeverReady built up — the worker having run and finished,
// not merely started. **Ready is deliberately not enough**: a source that accepts every start and
// drops it calls Ready every time, and a streak it cleared would hold the retry at the base delay
// forever, which is the loop NeverReady exists to pace.
func TestACleanExitResetsAStreakOfNeverReadyExitsAndReadyDoesNot(t *testing.T) {
	e, w, _ := runningWorker(t, WithBackoff(time.Millisecond, 1, time.Millisecond))
	w.exit(t, Succeeded())
	awaitAttempts(t, e, "sync", "the first exit to be recorded", func(a Attempts) bool {
		return a.Failures == 1
	})

	pass := w.started.Await(t, "the restart")
	pass.Ready()
	awaitAttempts(t, e, "sync", "the restart to be up", func(a Attempts) bool { return a.Ready() })
	assert.Equal(t, 1, att(t, e, "sync").Failures, "starting is not proof")

	w.exit(t, Succeeded())

	awaitAttempts(t, e, "sync", "the streak to clear", func(a Attempts) bool { return a.Failures == 0 })
}

// A worker's Fail is a job's: the ladder.
func TestAFailingWorkerClimbsTheLadder(t *testing.T) {
	e, w, pass := runningWorker(t, WithBackoff(time.Hour, 2, time.Hour))
	pass.Ready()

	w.exit(t, Fail("Unreachable", assert.AnError))

	// The pacing is what the wait is for: commit schedules "due now" and the pass that follows
	// is what derives the rung, so a read taken between the two is of a run mid-record.
	a := awaitAttempts(t, e, "sync", "the failure to be recorded and laddered", func(a Attempts) bool {
		return a.LastAttempt.Done() && a.NextAttempt.ScheduledAt.After(time.Now().Add(time.Minute))
	})
	assert.Equal(t, 1, a.Failures)
	assert.True(t, a.HealthySince.IsZero(), "a failure ends the stretch")
}

// A worker's Suspend parks it: nothing is scheduled, and a Wake is what starts it again. This is
// the kind sync at its identity gate.
func TestASuspendedWorkerParksUntilAWake(t *testing.T) {
	e, w, _ := runningWorker(t)
	w.exit(t, Suspend("NoConnection", "nothing has reached the server"))
	awaitAttempts(t, e, "sync", "the suspension to be recorded", func(a Attempts) bool {
		return a.LastAttempt.Verdict == VerdictSuspended && !a.Scheduled()
	})

	e.Wake(subj, "sync")

	w.started.Await(t, "the wake to start it again")
}

// A Skip is "not my failure" — a connection retired under a cold list is nobody's fault and
// nothing to report — so it records nothing at all and parks.
func TestASkippingWorkerRecordsNothingAndParks(t *testing.T) {
	e, w, _ := runningWorker(t)

	w.exit(t, Skip())

	awaitAttempts(t, e, "sync", "the run to end with nothing scheduled", func(a Attempts) bool {
		return !a.InFlight() && !a.Scheduled()
	})
	assert.False(t, att(t, e, "sync").LastAttempt.Done(), "a Skip records nothing")
}

// A stop is not an attempt: Restart, Remove and Close ask for the end, so it is not the body's
// doing and the streak stands. A poke over three hundred workers must not reset the rung a
// struggling server earned.
func TestAStoppedRunRecordsNothingAndLeavesTheStreakStanding(t *testing.T) {
	e, w, _ := runningWorker(t, WithBackoff(time.Millisecond, 1, time.Millisecond))
	w.exit(t, Fail("Unreachable", assert.AnError))
	awaitAttempts(t, e, "sync", "one rung", func(a Attempts) bool { return a.Failures == 1 })
	w.started.Await(t, "the retry")
	// Anything but a clean stop, so a recorded exit would be visible as a second rung.
	w.cancelledWith(Fail("Unreachable", assert.AnError))

	e.Restart(subj, "sync")

	w.started.Await(t, "the replacement run")
	assert.Equal(t, 1, att(t, e, "sync").Failures, "the stopped run climbed a rung")
}

// A worker stopped before Ready is not NeverReady either — for the same reason: it was not the
// worker that decided to stop.
func TestAWorkerStoppedBeforeReadyIsNotNeverReady(t *testing.T) {
	e, w, _ := runningWorker(t)

	e.Restart(subj, "sync")

	w.started.Await(t, "the replacement run")
	assert.False(t, att(t, e, "sync").LastAttempt.Done(), "the stopped run was recorded")
}

// --- the floor ---

// A worker that says nothing about its interval is paced by the backoff base, resolved after the
// options run so WithBackoff decides it whichever order the two are written in.
func TestTheWorkerFloorDefaultsToTheBackoffBase(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	RegisterWorker(e, "unset", newSteeredWorker())
	RegisterWorker(e, "laddered", newSteeredWorker(), WithBackoff(3*time.Second, 2, time.Minute))
	RegisterWorker(e, "stated", newSteeredWorker(), WithInterval(time.Hour), WithBackoff(3*time.Second, 2, time.Minute))

	assert.Equal(t, time.Second, e.specs[e.byName["unset"]].cfg.interval)
	assert.Equal(t, 3*time.Second, e.specs[e.byName["laddered"]].cfg.interval, "the floor follows the ladder")
	assert.Equal(t, time.Hour, e.specs[e.byName["stated"]].cfg.interval)
}

// A worker has no timeout unless it asks for one, and a job's 30s stands: a cold list of a large
// collection legitimately outlasts any bound a read would want.
func TestAWorkerHasNoStartTimeoutUnlessItAsksForOne(t *testing.T) {
	_, w, _ := runningWorker(t)

	w.mu.Lock()
	defer w.mu.Unlock()
	assert.False(t, w.sawDeadline, "a worker's ctx carried a deadline")
}

// The startup timeout bounds the time until Ready and nothing after it, so a worker that is up
// runs as long as it likes.
func TestTheStartupTimeoutIsStoppedByReady(t *testing.T) {
	e, w, pass := runningWorker(t, WithTimeout(20*time.Millisecond), WithInterval(time.Hour))
	pass.Ready()

	testutil.NoRecv(t, w.started.Chan(), quietWindow, "the startup timeout ended a worker that was ready")
	assert.True(t, att(t, e, "sync").Ready())
}

// A worker that hangs in startup is ended by the timeout — and that is NOT a stop: it is recorded
// NeverReady and climbs the ladder, so it is visible rather than going quiet.
//
// **Whatever the body returns.** One that reads its cancelled context and reports Skip or Suspend
// is answering our cancel rather than choosing to park, and taking it at its word would leave the
// worker waiting on a wake nobody owes it.
func TestAStartupTimeoutIsNeverReadyWhateverTheWorkerReturns(t *testing.T) {
	for _, tc := range []struct {
		name string
		res  Result
	}{
		{"a clean exit", Succeeded()},
		{"a skip", Skip()},
		{"a suspension", Suspend("NoConnection", "nothing reached the server")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := New()
			t.Cleanup(func() { assert.NoError(t, e.Close()) })
			w := newSteeredWorker()
			w.cancelledWith(tc.res)
			RegisterWorker(e, "sync", w, WithTimeout(20*time.Millisecond),
				WithBackoff(time.Hour, 2, time.Hour))
			startSupervisor(t, e)
			e.Add(subj)

			a := awaitAttempts(t, e, "sync", "the timed-out start to be recorded", hasRecorded)
			assert.Equal(t, VerdictFailed, a.LastAttempt.Verdict)
			assert.Equal(t, ReasonNeverReady, a.LastAttempt.Reason)
			assert.Equal(t, 1, a.Failures, "paced by the ladder rather than parked")
			assert.True(t, a.Scheduled(), "and due again")
		})
	}
}

// A worker that parks at a gate of its OWN is not never-ready in the sense the ladder is for: its
// Suspend is the answer, and converting it would have every kind on an unreachable cluster climb
// the ladder under a reason nothing observed. Only a start the supervisor ended, or one the worker
// says finished cleanly, is a failure it never proved itself out of.
func TestASuspensionTheWorkerChoseIsNotNeverReady(t *testing.T) {
	e, w, _ := runningWorker(t)

	w.exit(t, Suspend("NoConnection", "nothing reached the server"))

	a := awaitAttempts(t, e, "sync", "the suspension to be recorded", hasRecorded)
	assert.Equal(t, VerdictSuspended, a.LastAttempt.Verdict)
	assert.Equal(t, Reason("NoConnection"), a.LastAttempt.Reason)
	assert.Zero(t, a.Failures, "a gate is not a failure")
}

// --- Wake and Restart ---

// A Wake never tears a live worker down — it means "when you next stop, start again at once",
// which is what lets a connection bridge wake every kind on a cache per state frame.
func TestAWakeLeavesARunningWorkerRunningAndRestartsItOnItsNextExit(t *testing.T) {
	e, w, pass := runningWorker(t, WithInterval(time.Hour))
	pass.Ready()

	e.Wake(subj, "sync")

	testutil.NoRecv(t, w.started.Chan(), quietWindow, "the Wake tore the live worker down")
	assert.Equal(t, 1, w.count())

	// The exit is what the wake is redelivered behind, and it goes under the floor.
	w.exit(t, Succeeded())
	w.started.Await(t, "the woken restart, under the floor")
}

// Restart stops the run and starts exactly one more — for both kinds, since a job mid-dial
// against a machine that just woke is as stale as a worker's stream.
func TestRestartStopsTheRunAndStartsExactlyOneMore(t *testing.T) {
	e, w, pass := runningWorker(t, WithInterval(time.Hour))
	pass.Ready()

	e.Restart(subj, "sync")

	w.started.Await(t, "the replacement run")
	testutil.NoRecv(t, w.started.Chan(), quietWindow, "one Restart started more than one run")
	assert.Equal(t, 2, w.count())
}

// Restart cancels the job's run and it is due again at once, where without one it would sit out
// the hour. **What makes it due is that a stop records nothing**, so the pass reads it as never
// run — Restart's own ask only makes that prompt, and the two collapse in the queue. One run then
// records and the interval takes over, which is what stops it looping.
func TestRestartCancelsARunningJobAndItIsDueAgainAtOnce(t *testing.T) {
	e, p, id := single(t, Succeeded(), WithInterval(time.Hour))
	inRun, letGo := testutil.NewSignal(), make(chan struct{})
	p.onRun = func() { inRun.Fire(); <-letGo }
	startSupervisor(t, e)
	e.Add(subj)
	inRun.Wait(t, "the job to be in flight")

	e.Restart(subj, id)
	close(letGo)

	// Settled on the interval rather than looping is the whole claim, so it is what the wait
	// is for — a count that has stopped moving would need a window to read.
	awaitAttempts(t, e, id, "the replacement to record and be paced by the interval", func(a Attempts) bool {
		return a.LastAttempt.Done() && a.NextAttempt.ScheduledAt.After(time.Now().Add(time.Minute))
	})
	assert.GreaterOrEqual(t, p.count(), 2, "the cancelled run was not replaced")
}

// RestartAll is the resume poke: every tracked subject, and it does not wait — three hundred
// cancels are one poke, three hundred joins are not.
func TestRestartAllReachesEveryTrackedSubject(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	w := newSteeredWorker()
	RegisterWorker(e, "sync", w, WithInterval(time.Hour))
	startSupervisor(t, e)
	e.Add("a")
	e.Add("b")
	w.started.Await(t, "the first start")
	w.started.Await(t, "the second start")

	e.RestartAll("sync")

	w.started.Await(t, "one replacement")
	w.started.Await(t, "the other replacement")
}

// Remove joins a worker: past it the goroutine is gone, which is what makes forgetting one
// synchronous for a caller about to release what it wrote through.
func TestRemoveJoinsAWorker(t *testing.T) {
	e, w, pass := runningWorker(t)
	pass.Ready()

	e.Remove(subj)

	assert.Equal(t, 1, w.count())
	_, ok := e.Read(subj)
	assert.False(t, ok)
}

// --- the graph ---

// A worker is not started until its dependency has succeeded, exactly as a job would not be.
func TestAWorkerWaitsForItsDependency(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	dep := &steered{res: Fail("Unreachable", assert.AnError)}
	RegisterJob(e, "conn", dep)
	w := newSteeredWorker()
	RegisterWorker(e, "sync", w, WithDependencies("conn"), WithInterval(time.Hour))
	startSupervisor(t, e)

	e.Add(subj)
	awaitAttempts(t, e, "sync", "the worker to record DependencyFailed", func(a Attempts) bool {
		return a.LastAttempt.Reason == ReasonDependencyFailed
	})
	assert.Zero(t, w.count(), "the worker was started over a failing dependency")

	dep.set(Succeeded())
	e.Wake(subj, "conn")
	w.started.Await(t, "the worker, once its dependency came back")
}

// A dependency gates a worker's START and nothing more: one that fails under a running worker
// leaves it alone, and is checked again at its next start.
func TestADependencyThatFailsUnderARunningWorkerLeavesItAlone(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	dep := &steered{res: Succeeded()}
	RegisterJob(e, "conn", dep)
	w := newSteeredWorker()
	RegisterWorker(e, "sync", w, WithDependencies("conn"), WithInterval(time.Hour))
	startSupervisor(t, e)

	e.Add(subj)
	w.started.Await(t, "the worker's run").Ready()

	dep.set(Fail("Unreachable", assert.AnError))
	e.Wake(subj, "conn")
	awaitAttempts(t, e, "conn", "the dependency to fail", func(a Attempts) bool { return !a.OK() })

	assert.Equal(t, 1, w.count(), "the running worker was restarted by its dependency failing")
	assert.True(t, att(t, e, "sync").Ready())
}

func TestARetryDueBeforeTheQueueKeyIsReleasedIsRescheduled(t *testing.T) {
	now := time.Now()
	e := New(withNow(func() time.Time { return now }))
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	p := &steered{res: Fail("Unreachable", assert.AnError)}
	RegisterJob(e, "conn", p, WithBackoff(time.Second, 2, time.Minute), WithInterval(time.Hour))
	e.Add(subj)
	e.settle()
	k, ok := e.runQ.Next(within(t))
	require.True(t, ok)

	e.runQueued(t.Context(), k, func() {
		// A job releases its start slot after commit, while runQueued still holds its
		// key. Run the pending pass here with the retry already due.
		a := att(t, e, "conn")
		require.Equal(t, VerdictFailed, a.LastAttempt.Verdict)
		require.False(t, a.InFlight())
		now = a.LastAttempt.FinishedAt.Add(time.Minute)
		e.settle()
		require.Nil(t, e.subjects[subj].timer, "an already due retry has no timer to wake it")
	})

	// Only runQueued's post-Done pass can recover the scheduling request dropped
	// while the key was held. No external Wake or direct pass is supplied here.
	e.settle()
	p.set(Succeeded())
	runNext(t, e)
	assert.Equal(t, 2, p.count())
	assert.Equal(t, VerdictSucceeded, att(t, e, "conn").LastAttempt.Verdict)
}

func TestAPassBeforeADispatchedJobStartsDoesNotQueueAnotherRun(t *testing.T) {
	e, p, _ := single(t, Succeeded(), WithInterval(time.Hour))
	e.Add(subj)
	e.settle()
	k, ok := e.runQ.Next(within(t))
	require.True(t, ok)

	// The dispatcher holds the key, but runOne has not marked it in flight yet.
	e.pass(subj)
	e.runOne(t.Context(), k, func() {})
	e.runQ.Done(k)
	e.settle()

	assert.Equal(t, 1, p.count())
	noRuns(t, e, "a scheduling pass queued a duplicate run before the first began")
}

// A job depending on a worker reads the worker's health rather than its last exit: Ready is OK,
// and Ready asks for a pass so the dependent is scheduled the moment the worker comes up.
func TestAJobDependingOnAWorkerRunsOnceItIsReady(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	w := newSteeredWorker()
	RegisterWorker(e, "sync", w, WithInterval(time.Hour))
	dependent := &steered{res: Succeeded()}
	RegisterJob(e, "reader", dependent, WithDependencies("sync"), WithInterval(time.Hour))
	startSupervisor(t, e)

	e.Add(subj)
	pass := w.started.Await(t, "the worker's run")
	// Starting is unanswered, so nothing has been said about the dependent yet.
	assert.Zero(t, dependent.count())

	pass.Ready()

	require.Eventually(t, func() bool { return dependent.count() == 1 }, testutil.Timeout, time.Millisecond,
		"the dependent to run once its worker was up")
}

// A worker that is down is a failing dependency, not a starting one: its dependent records
// DependencyFailed rather than waiting on it.
func TestAJobDependingOnADownWorkerRecordsTheFailingDependency(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	w := newSteeredWorker()
	RegisterWorker(e, "sync", w, WithBackoff(time.Hour, 2, time.Hour))
	dependent := &steered{res: Succeeded()}
	RegisterJob(e, "reader", dependent, WithDependencies("sync"), WithInterval(time.Hour))
	startSupervisor(t, e)

	e.Add(subj)
	w.started.Await(t, "the worker's run")
	w.exit(t, Fail("Unreachable", assert.AnError))

	a := awaitAttempts(t, e, "reader", "the dependent to record its failing dependency", hasRecorded)
	assert.Equal(t, ReasonDependencyFailed, a.LastAttempt.Reason)
	assert.Zero(t, dependent.count())
}

// The watch edge is a Restart for a worker where it is a Wake for a job: a worker's input moving
// means the one it is running on is stale. **The restart is made from inside commit**, under the
// supervisor's own lock, which is why it must not be the kind of call that waits.
func TestAChangedValueRestartsAWatchingWorker(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	source := &steered{res: Succeeded()}
	RegisterJob(e, "conn", source, WithInterval(time.Hour))
	w := newSteeredWorker()
	RegisterWorker(e, "sync", w, WithWatches("conn"), WithInterval(time.Hour))
	startSupervisor(t, e)

	e.Add(subj)
	w.started.Await(t, "the worker's run").Ready()
	awaitAttempts(t, e, "conn", "the source to settle", hasRecorded)

	source.commits("moved")
	e.Wake(subj, "conn")

	w.started.Await(t, "the worker restarted onto the new value")
}

// --- reading a worker ---

// The two observation types split because a reader judges them differently, and reading one as
// the other is a wiring bug rather than a zero value quietly handed back.
func TestATypedReadRefusesTheOtherKind(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	RegisterJob(e, "conn", &steered{res: Succeeded()})
	RegisterWorker(e, "sync", newSteeredWorker())
	e.Add(subj)
	snap, _ := e.Read(subj)

	assert.PanicsWithValue(t, `supervisor: "sync" is a worker, read as a job`,
		func() { GetJobObservation[string](snap, "sync") })
	assert.PanicsWithValue(t, `supervisor: "conn" is a job, read as a worker`,
		func() { GetWorkerObservation[string](snap, "conn") })
}

// A worker's value does not outlive the worker: "watching" is false the moment it exits, where a
// job's "identified, as of 10:00" still holds. Live is what says so, and ChangedAt dates the
// value rather than the last time something confirmed it.
func TestAWorkersValueIsLiveOnlyWhileItRuns(t *testing.T) {
	e, w, pass := runningWorker(t, WithInterval(time.Hour))
	pass.Ready()
	pass.Commit("watching")
	awaitAttempts(t, e, "sync", "the commit to land", func(Attempts) bool {
		snap, _ := e.Read(subj)
		return GetWorkerObservation[string](snap, "sync").Known()
	})

	snap, _ := e.Read(subj)
	live := GetWorkerObservation[string](snap, "sync")
	assert.True(t, live.Live())
	assert.Equal(t, "watching", live.Value)
	changedAt := live.ChangedAt

	w.exit(t, Fail("Unreachable", assert.AnError))
	awaitAttempts(t, e, "sync", "the exit to be recorded", hasRecorded)

	snap, _ = e.Read(subj)
	down := GetWorkerObservation[string](snap, "sync")
	assert.False(t, down.Live(), "the value outlived the worker")
	assert.Equal(t, "watching", down.Value, "what it last said still reads")
	assert.Equal(t, changedAt, down.ChangedAt, "an exit is not a change of value")
}

// A rotation is the worker ending, not it speaking. Dating the value by a clean exit would make
// ChangedAt read as "this run has answered" to anyone comparing it against the attempt it ended.
func TestAWorkersCleanExitDoesNotDateItsValue(t *testing.T) {
	e, w, pass := runningWorker(t, WithInterval(time.Hour))
	pass.Ready()
	pass.Commit("watching")
	awaitAttempts(t, e, "sync", "the commit to land", func(Attempts) bool {
		snap, _ := e.Read(subj)
		return GetWorkerObservation[string](snap, "sync").Known()
	})

	snap, _ := e.Read(subj)
	changedAt := GetWorkerObservation[string](snap, "sync").ChangedAt

	w.exit(t, Succeeded())
	awaitAttempts(t, e, "sync", "the exit to be recorded", hasRecorded)

	snap, _ = e.Read(subj)
	assert.Equal(t, changedAt, GetWorkerObservation[string](snap, "sync").ChangedAt,
		"a clean exit dated a value it never committed")
}

// A run dispatched while its WORKER dependency is starting is the supervisor's own no-op: the
// worker has not answered yet, so there is nothing to record and nothing to dial. **It must not
// park the registration.** A Skip the body chose parks until a Wake; this one is not the body's,
// so it leaves no memory behind and the worker coming up is enough to schedule the run it
// displaced — which is the window a restarting worker leaves open on every rotation.
func TestARunDispatchedWhileItsWorkerDependencyIsStartingIsNotParked(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	w := newSteeredWorker()
	RegisterWorker(e, "sync", w, WithInterval(time.Hour))
	dialed := testutil.NewSignal()
	dependent := &steered{res: Succeeded(), onRun: func() { dialed.Fire() }}
	RegisterJob(e, "reader", dependent, WithDependencies("sync"), WithInterval(time.Hour))
	startSupervisor(t, e)

	e.Add(subj)
	pass := w.started.Await(t, "the worker's run")

	// A Wake outruns the pass, which is how a run reaches dispatch over a dependency that has
	// not answered. The window is bounded because nothing is expected to happen in it — and
	// waiting it out is also what puts the dispatch inside the window this test is about.
	e.Wake(subj, "reader")
	testutil.NoRecv(t, dialed.Chan(), quietWindow, "the dependent was dialed over a worker that had not answered")
	assert.False(t, att(t, e, "reader").LastAttempt.Done(), "the displaced run recorded something")

	// Nothing else asks for it, so the worker coming up has to be enough.
	pass.Ready()

	dialed.Wait(t, "the dependent to run once its worker was up")
	assert.Equal(t, 1, dependent.count())
}

// The startup timer and a first frame can land together: the timer takes the lock, marks the start
// timed out and cancels, and the worker — with its frame already in hand — calls Ready on the way
// out. **The timeout wins**, because the two are one decision and the timer made it first. A Ready
// admitted after it would open a healthy stretch and clear the streak over a start that failed,
// and a body then reporting its cancelled context with a Skip would park for good.
func TestReadyAfterTheStartupTimeoutIsRefused(t *testing.T) {
	e := New()
	t.Cleanup(func() { assert.NoError(t, e.Close()) })
	w := newSteeredWorker()
	w.readiesOnCancel()
	w.cancelledWith(Skip())
	RegisterWorker(e, "sync", w, WithTimeout(20*time.Millisecond), WithBackoff(time.Hour, 2, time.Hour))
	startSupervisor(t, e)

	e.Add(subj)

	a := awaitAttempts(t, e, "sync", "the timed-out start to be recorded", hasRecorded)
	assert.Equal(t, VerdictFailed, a.LastAttempt.Verdict)
	assert.Equal(t, ReasonNeverReady, a.LastAttempt.Reason)
	assert.Equal(t, 1, a.Failures, "paced by the ladder rather than parked")
	assert.True(t, a.HealthySince.IsZero(), "the late Ready opened a healthy stretch over a failed start")
	assert.True(t, a.Scheduled(), "and it is due again")
}

// A source that accepts every start and drops it calls Ready on each one, so the streak has to
// survive them to escalate. This is what the ladder is for, and what a Ready that cleared the
// count would flatten to the base delay forever.
func TestSuccessiveFailuresClimbThoughEachStartedCleanly(t *testing.T) {
	// A short ladder, since what is asserted is the COUNT climbing rather than the wait it
	// buys — a long one would just make the restarts outlast the test.
	e, w, pass := runningWorker(t, WithBackoff(time.Millisecond, 1, time.Millisecond))

	for i := range 3 {
		pass.Ready()
		w.exit(t, Fail("Unreachable", assert.AnError))
		awaitAttempts(t, e, "sync", "the failure to be recorded", func(a Attempts) bool {
			return a.Failures == i+1
		})
		if i < 2 {
			pass = w.started.Await(t, "the restart")
		}
	}
}
