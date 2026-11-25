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

// Package supervisor runs work over a set of subjects: the controller pattern without Kubernetes
// — a work queue, a level-triggered pass, and a schedule derived from recorded state. Nothing here
// knows what a subject is beyond its name.
//
// It runs two kinds of thing, and the difference is what a Run's return means.
//
//   - A JOB runs, returns, and is quiet until it is due again. Its lifetime is the call, its
//     Commit is buffered and applied with its verdict, and its whole run is bounded by a timeout.
//     A probe or a sweep.
//   - A WORKER starts, blocks until it is stopped or it dies, and reports while it runs. Its
//     lifetime is the supervisor's, its Commit is applied at once, and its Ready says the
//     starting phase is over. A watch stream.
//
// The rule of thumb: work with a natural end is a job; work that would need a goroutine outliving
// the call is a worker.
//
// A caller registers both under a name — the registration's whole public identity, which the
// edges, Wake, Restart and every read address it by. Add and Remove track subjects; Wake asks for
// another run and Restart stops the current one first; Read and OnPass hand back a Snapshot, every
// registration's observation copied under one lock, which GetJobObservation and
// GetWorkerObservation project by name.
//
// A run's own Result is its schedule — Succeeded waits out the interval, Fail climbs the backoff
// ladder, Suspend and Skip wait for a Wake — so no domain rule lives in the scheduler. What the
// supervisor does with each verdict differs by kind, which due and commit spell out.
//
// See docs/adr/2026-08-24-probe-engine.md and docs/adr/2026-08-28-jobs-and-workers.md.
package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"runtime/debug"
	"slices"
	"sync"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/drain"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/workqueue"
)

// registrationID is a registration's index — supervisor-internal, since a registration's public
// identity is the name it was registered under.
type registrationID int

// Job is one job's body: request against the subject, classify, and return the result. What the
// run found is recorded on the pass, wherever the body learns it; a body with no news just
// returns. The supervisor applies the pass under its lock when the run returns, so a run must do
// its waiting before it returns, and its whole life is bounded by the registered timeout.
//
// A job whose value owns something — a connection, a file, a goroutine it started — also
// implements `Discard(T)`, and is handed back every value the supervisor drops without a run
// having said so: a commit refused because the subject was removed mid-run, one refused because
// the run was stopped, a run that concluded Skip or returned the zero Result, one that panicked,
// and the standing value of a subject dropped by Remove or Close. Nothing else can reach such a
// value to release it.
//
// **A commit is the exception.** The value it replaces is not handed back, because a commit
// often carries the last one's holdings forward — a struct value with one field moved keeps the
// connection inside it — and a hand-back would release what the new value still holds. A run
// that means to drop what the last one held releases it itself, before it commits.
type Job[T any] interface {
	Run(ctx context.Context, pass *JobPass[T]) Result
}

// Worker is one worker's body, and **it is expected to block**: it starts whatever it is, calls
// pass.Ready once the expensive part of that is done, and runs until its ctx is cancelled or it
// dies. Returning is the worker having STOPPED, and the Result says how to start it again — a
// clean stop waits out the floor, a failure climbs the ladder, and a clean stop before Ready is
// recorded NeverReady, which is what stops a source that accepts and drops every start
// hot-looping.
//
// **Ready is "started", not "proven healthy".** A body that holds it back until its source says
// something holds a start slot for as long as that source stays quiet — which for a watch on an
// idle collection is indefinitely. Whether what it started is WORKING is the body's to report
// through its value, and to answer for in the verdict it finally returns.
//
// It reports through the pass while it runs: every Commit is applied at once and asks for a pass.
//
// **Its value must own nothing.** Commits are live, so one can be refused with nothing handed
// back — the subject was removed while the worker ran — and there is no Discard for a worker.
// What the worker itself owns it releases on the way out, which Remove and Close wait for.
type Worker[T any] interface {
	Run(ctx context.Context, pass *WorkerPass[T]) Result
}

// Backoff paces a failing run: Base widened by Factor per consecutive failure, capped at Cap.
type Backoff struct {
	Base   time.Duration
	Factor float64
	Cap    time.Duration
}

// Delay is the ladder for the given count of consecutive failures — a pure function of it, so
// the same state derives the same ScheduledAt on every pass. That is what lets the frontend
// render the countdown: no jitter, and no stateful rate limiter.
//
// Exported for a body that paces its own retries, so one ladder shape reaches the frontend
// from every seam.
func (b Backoff) Delay(failures int) time.Duration {
	d := b.Base
	for range failures - 1 {
		d = time.Duration(float64(d) * b.Factor)
		if d >= b.Cap {
			return b.Cap
		}
	}
	return min(d, b.Cap)
}

// registrationCfg is the per-registration knobs, all optional.
type registrationCfg struct {
	interval     time.Duration
	backoff      Backoff
	timeout      time.Duration
	dependencies []string
	watches      []string
}

var defaultJobCfg = registrationCfg{
	interval: time.Minute,
	backoff:  Backoff{Base: time.Second, Factor: 2, Cap: 5 * time.Minute},
	timeout:  30 * time.Second,
}

// A worker states neither of the other two. Its interval is the FLOOR between a clean exit and
// the restart, filled from the backoff base once the options have run — so WithBackoff decides it
// whichever order the two are written in — and its timeout bounds only the time until Ready,
// which most workers want unbounded, since a cold list of a large collection legitimately
// outlasts any bound a read would want.
var defaultWorkerCfg = registrationCfg{
	backoff: Backoff{Base: time.Second, Factor: 2, Cap: 5 * time.Minute},
}

// RegistrationOption tunes one registration; a registration states only what deviates from the
// defaults. The same options serve both kinds — what differs is the default each starts from.
type RegistrationOption func(*registrationCfg)

// WithDependencies declares the registrations this one can only run over the success of — the
// health edge, answering "can this run?". While a dependency is failing this one is recorded as
// DependencyFailed rather than dispatched, and it is due again when the dependency recovers. It
// takes names already registered, so a dependency exists before whatever depends on it and the
// graph is acyclic by construction.
//
// **A dependency gates a worker's START and nothing more.** One that fails while the worker runs
// leaves it alone, and is checked again at the worker's next start; a worker its dependency
// really must stop watches for that itself and exits.
//
// A worker at the far END is OK once it is Ready, unanswered while it is starting, and failing
// once it is down — so a job depending on one is scheduled when the worker comes up.
func WithDependencies(names ...string) RegistrationOption {
	return func(c *registrationCfg) { c.dependencies = append(c.dependencies, names...) }
}

// WithWatches declares the registrations whose values this one reads — the data edge, answering
// "is this answer stale?". When one of them commits a changed value, this one runs again, whatever
// its schedule said; it takes no other part in scheduling, and it never gates a run. Health is the
// other edge's job: something that also cannot run without what it reads declares WithDependencies
// too.
//
// **For a worker the edge is a Restart**, where for a job it is a Wake: a worker's input moving
// means the one it is running on is stale, so it comes down and starts again on the new one.
func WithWatches(names ...string) RegistrationOption {
	return func(c *registrationCfg) { c.watches = append(c.watches, names...) }
}

// WithInterval is how long after a succeeded run the registration is due again. For a worker that
// is the floor under a clean restart rather than a rest — the gap between the exit and the next
// start, which is what paces a server that accepts a watch and closes it after one frame.
func WithInterval(d time.Duration) RegistrationOption {
	return func(c *registrationCfg) { c.interval = d }
}

// WithBackoff paces the failures. For a worker it also fills in the restart floor, unless
// WithInterval states one.
func WithBackoff(base time.Duration, factor float64, cap time.Duration) RegistrationOption {
	return func(c *registrationCfg) { c.backoff = Backoff{Base: base, Factor: factor, Cap: cap} }
}

// WithTimeout bounds a JOB's whole run and a WORKER's start — the time until it calls Ready,
// after which it runs as long as it likes. Enforced by the supervisor on the context it hands the
// run, and it is not a stop: what the run then returns is recorded like any other, so one that
// hangs in startup climbs the ladder rather than going quiet.
func WithTimeout(d time.Duration) RegistrationOption {
	return func(c *registrationCfg) { c.timeout = d }
}

// spec is one registration, its body erased to the value the observable stores. RegisterJob and
// RegisterWorker's wrappers are the only things that box and unbox, so the types stay checked at
// the boundary.
type spec struct {
	name string
	cfg  registrationCfg
	// worker is which kind this is, and it decides everything the two do not share: when the
	// start slot is released, how the exit is classified, what a watch edge means, and how a
	// dependent reads this one's health.
	worker bool
	// discard hands one standing value back to the job that committed it, nil for a job with no
	// Discard and always nil for a worker. Type-erased because the supervisor holds every
	// registration's values in one untyped slice; the register wrapper closes over the real type.
	discard func(any)
	// dependencies and watches are cfg's names resolved once, at registration.
	dependencies []registrationID
	watches      []registrationID
	run          func(runReq) runOutcome
}

// runReq is what the supervisor hands one run: its inputs, plus the two calls back a worker
// reports through — both nil for a job, which has no way to reach them.
type runReq struct {
	ctx     context.Context
	subject string
	prev    any
	snap    Snapshot
	ready   func()
	commit  func(any)
}

// runOutcome is what one run left behind. val and discard are a job's buffered commit; a worker
// commits live, so it leaves a verdict and nothing else.
type runOutcome struct {
	res     Result
	val     any
	discard func()
}

// Option configures a Supervisor.
type Option func(*settings)

// WithStartConcurrency is how many runs may be STARTING at once, across every subject. A
// fleet-wide cap rather than a per-subject one, because what it holds back is the first pass over
// a large kubeconfig: without it every cluster's credential helper runs in the same second.
//
// **A job is starting for its whole run; a worker only until Ready.** So eight is eight cold
// lists however many streams are already up — which is what a supervisor of hundreds of workers
// needs, and what a cap on runs in flight could not give.
//
// It panics below one, as every wiring bug here does. A supervisor with no slots admits nothing —
// every subject queues and no run is ever dispatched — and it is silent about it, which is the one
// failure a caller cannot debug from what the supervisor reports.
// withNow freezes or steers the clock, for a test over tick's own guarantee.
func withNow(fn func() time.Time) Option {
	return func(s *settings) { s.now = fn }
}

func WithStartConcurrency(n int) Option {
	if n < 1 {
		panic("supervisor: WithStartConcurrency needs at least one slot")
	}
	return func(s *settings) { s.startConcurrency = n }
}

// settings is what the options write.
type settings struct {
	startConcurrency int
	// now reads the wall clock. A seam only so a test can freeze it; tick is what the
	// supervisor calls.
	now func() time.Time
}

// Supervisor runs the registered jobs and workers over the tracked subjects. Build with New,
// register with RegisterJob and RegisterWorker, then Start; the zero Supervisor has no queues to
// work.
type Supervisor struct {
	settings settings
	onPass   func(subjectName string, snap Snapshot)

	// runQ carries the runs that are due, one key per registration per subject, so one
	// registration never runs twice at once. Explicit Wake, Restart, and watcher requests
	// arriving mid-run are redelivered on Done. Scheduling passes use AddIfAbsent instead;
	// the dispatcher must request another pass after Done to re-derive any due work ignored
	// while the key was held. passQ carries the subjects whose schedule has to be re-derived;
	// everything that changes a subject ends with an add there.
	runQ  *workqueue.Queue[key]
	passQ *workqueue.Queue[string]
	// clockMu guards lastTick alone, so tick never waits on the supervisor's own lock.
	clockMu  sync.Mutex
	lastTick time.Time

	// slots is the start semaphore WithStartConcurrency sizes: the dispatcher takes one before
	// it starts a run, a job gives it back when it returns, and a worker at Ready or return,
	// whichever comes first.
	slots chan struct{}

	// mu guards specs, started, and subjects together with the entries behind them: a value
	// and its attempts are read against each other, and nothing may see one without the other.
	mu      sync.Mutex
	specs   []spec
	started bool
	// watchers is the data edge reversed: for each registration, who watches its value and so
	// runs again when it moves. byName is what a read resolves through, and workers is which
	// kind each registration is, which is what makes a typed read of the wrong kind a panic.
	// All three are built at registration, and none is written once a subject exists.
	watchers map[registrationID][]registrationID
	byName   map[string]registrationID
	workers  []bool
	subjects map[string]*subject

	wg sync.WaitGroup
}

// key is one run of one registration against one subject — the unit runQ keys on.
type key struct {
	subject      string
	registration registrationID
}

// observable is what the supervisor holds for one registration of one subject: the value its runs
// committed beside the bookkeeping for the run that read it. The untyped half of the two
// observation types, which is what the typed reads project it into — one supervisor carries
// registrations of different value types, so the slice itself cannot be typed.
type observable struct {
	// value is the last committed answer, nil until a run commits one, and seen dates it. For a
	// job that is when a run last CONFIRMED it: whenever one is committed, and on a success
	// that found the standing answer unchanged, which still read it. For a worker it is when
	// the value last CHANGED, since a worker confirms its value by still running rather than by
	// running again.
	value any
	seen  time.Time
	// run is the run in flight, nil when none. It is what makes a running job or worker
	// reachable: Restart cancels it, and Remove and Close cancel it and wait on done.
	run *runHandle

	Attempts
}

// runHandle is one run in flight — the half of it the supervisor holds while the body has the
// other. Every field is written under e.mu; done is closed by the run itself, after its commit.
type runHandle struct {
	cancel context.CancelFunc
	done   chan struct{}
	// stopped marks a run whose end was ASKED FOR, by Restart, Remove or Close. Such an end is
	// not the body's doing, so the run records nothing and the failure streak stands — which
	// matters because a poke restarts every worker on a cache at once, and one that reset the
	// streak would have the whole cache retry a struggling server at the base delay.
	//
	// Only those three set it. The startup timer cancels the same ctx and sets nothing, so a
	// worker that hangs in startup is recorded and climbs the ladder.
	stopped bool
	// readyAt is when the run called Ready, zero for a job and for a worker that never did.
	// timedOut marks a start the supervisor's own startup timer ended, which is not a stop:
	// the run is recorded, and recorded as a failure whatever it returned.
	readyAt  time.Time
	timedOut bool
}

// subject is what the supervisor holds for one tracked name. Identity matters: a subject removed
// and re-added is a new *subject, and a run dispatched against the old one commits nothing.
type subject struct {
	// obs is one observable per registration, indexed by registrationID.
	obs []observable
	// timer brings the pass back when the soonest scheduled run comes due. One per subject,
	// and it is a wake, not a cadence: the pass decides again per registration.
	timer *time.Timer
}

func (s *subject) stopTimer() {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
}

// snapshotOf copies one subject's observables for a reader. Called under e.mu, so the values and
// the schedule beside them agree. The two registration indexes are shared rather than copied —
// registration closes before the first subject, so nothing writes them from here on.
func (e *Supervisor) snapshotOf(sub *subject) Snapshot {
	return Snapshot{obs: slices.Clone(sub.obs), byName: e.byName, workers: e.workers}
}

// New returns a Supervisor with nothing registered and no subjects.
func New(opts ...Option) *Supervisor {
	e := &Supervisor{
		settings: settings{startConcurrency: 8, now: time.Now},
		runQ:     workqueue.New[key](),
		passQ:    workqueue.New[string](),
		watchers: map[registrationID][]registrationID{},
		byName:   map[string]registrationID{},
		subjects: map[string]*subject{},
	}
	for _, opt := range opts {
		opt(&e.settings)
	}
	e.slots = make(chan struct{}, e.settings.startConcurrency)
	return e
}

// tick reads the clock, never returning an instant it has already returned.
//
// Every stamp here is read as a level — LastRunAt promises that a reader who missed the frames
// still sees it move, which is how a caller asks "did it run again?". A clock coarse enough to
// hand two runs the same instant cannot answer that, and Windows' is: two runs of a job that
// does nothing land on the same tick. The nudge is a nanosecond, and the wall clock reclaims it
// on the next tick.
func (e *Supervisor) tick() time.Time {
	e.clockMu.Lock()
	defer e.clockMu.Unlock()
	now := e.settings.now()
	if !now.After(e.lastTick) {
		now = e.lastTick.Add(time.Nanosecond)
	}
	e.lastTick = now
	return now
}

// Now reads the clock the supervisor stamps its runs with — for a caller taking a baseline to
// compare against those stamps, which must not be the wall clock: tick nudges a repeated instant
// forward, so the two can disagree.
//
// Reading it advances the clock, which is what makes the comparison exact: every stamp issued
// before this reading is behind it, and every one after is ahead.
func (e *Supervisor) Now() time.Time { return e.tick() }

// OnPass sets the callback the supervisor fires after every pass, with the Snapshot that pass
// produced. Called outside the supervisor's lock but serialized per subject; it must not block.
// Wiring, not state — set it before Start, like a registration.
//
// Every pass, not every change: a snapshot carries Attempts, which every run rewrites, so the
// supervisor cannot tell a new answer from the same one re-confirmed. A caller that wakes something
// expensive projects the snapshot down to what its readers react to and compares that itself.
func (e *Supervisor) OnPass(fn func(subjectName string, snap Snapshot)) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.started {
		panic("supervisor: OnPass after Start")
	}
	e.onPass = fn
}

// RegisterJob adds one job under name, which is its whole public identity: the edges, Wake,
// Restart, and every read address it by that. A package function rather than a method so each
// registration picks its own value type; T is inferred from the instance.
//
// It panics on an edge naming something not yet registered, a duplicate name, or a call after
// Start or the first Add — a table wired wrong at boot, not a runtime error. A subject's
// bookkeeping is sized when it is added, which is why the set must be complete before anything
// is tracked.
func RegisterJob[T any](e *Supervisor, name string, j Job[T], opts ...RegistrationOption) {
	if j == nil {
		panic("supervisor: RegisterJob needs a job")
	}
	run := func(req runReq) runOutcome {
		pass := &JobPass[T]{passCore: newPassCore[T](req)}
		// A run that panics has still committed whatever it committed before it did, and the
		// supervisor applies none of it — so it is handed back here, on the way out to the
		// recover that turns the panic into a result.
		defer func() {
			if r := recover(); r != nil {
				handBack(j, pass.next)
				panic(r)
			}
		}()
		res := j.Run(req.ctx, pass)
		if pass.next == nil {
			return runOutcome{res: res}
		}
		return runOutcome{res: res, val: *pass.next, discard: func() { handBack(j, pass.next) }}
	}
	e.register(name, false, run, discarderOf(j), defaultJobCfg, opts)
}

// RegisterWorker adds one worker under name, on the same terms as RegisterJob. What differs is
// what the supervisor does with it: the run is expected to block, its commits are applied as they
// are made, and its exit is what schedules the next start.
//
// A worker declares no Discard and is handed nothing back — its value must own nothing, and what
// the worker itself holds it releases before Run returns.
func RegisterWorker[T any](e *Supervisor, name string, w Worker[T], opts ...RegistrationOption) {
	if w == nil {
		panic("supervisor: RegisterWorker needs a worker")
	}
	run := func(req runReq) runOutcome {
		pass := &WorkerPass[T]{
			passCore: newPassCore[T](req),
			commit:   req.commit,
			ready:    req.ready,
		}
		return runOutcome{res: w.Run(req.ctx, pass)}
	}
	e.register(name, true, run, nil, defaultWorkerCfg, opts)
}

// newPassCore unboxes one run's inputs into the half both passes carry. A nil prev is what
// "nothing has committed" means, since the supervisor only ever stores a value a run handed it.
func newPassCore[T any](req runReq) passCore[T] {
	var pv T
	if req.prev != nil {
		pv = req.prev.(T)
	}
	return passCore[T]{subject: req.subject, prev: pv, known: req.prev != nil, snap: req.snap}
}

// discarderOf is the type-erased hand-back for a job that implements Discard, nil for one that
// does not. The value is whatever this job's runs committed, so the assertion holds.
func discarderOf[T any](j Job[T]) func(any) {
	d, ok := j.(interface{ Discard(T) })
	if !ok {
		return nil
	}
	return func(v any) { d.Discard(v.(T)) }
}

// handBack tells a job that the supervisor dropped what its run committed, for one that asked to
// be told by implementing Discard. A committed value can own something — a connection, a file, a
// goroutine — and one the supervisor never applied is one nothing else can reach to release.
func handBack[T any](j Job[T], next *T) {
	d, ok := j.(interface{ Discard(T) })
	if !ok || next == nil {
		return
	}
	d.Discard(*next)
}

func (e *Supervisor) register(name string, worker bool, run func(runReq) runOutcome, discard func(any), cfg registrationCfg, opts []RegistrationOption) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.started {
		panic("supervisor: Register after Start")
	}
	if len(e.subjects) != 0 {
		panic("supervisor: Register after Add")
	}
	if name == "" {
		panic("supervisor: Register needs a name")
	}
	for _, s := range e.specs {
		if s.name == name {
			panic(fmt.Sprintf("supervisor: %q registered twice", name))
		}
	}

	for _, opt := range opts {
		opt(&cfg)
	}
	// The worker floor is resolved here rather than baked into the default, so an option that
	// moves the ladder moves the floor with it whichever order the two were written in.
	if worker && cfg.interval == 0 {
		cfg.interval = cfg.backoff.Base
	}
	// Resolved here, against what is registered so far: a forward reference panics, so the
	// registration order stays topological and both graphs are acyclic by construction.
	dependencies := e.resolveLocked(name, "depends on", cfg.dependencies)
	watches := e.resolveLocked(name, "watches", cfg.watches)

	e.specs = append(e.specs, spec{
		name: name, cfg: cfg, worker: worker, dependencies: dependencies, watches: watches,
		run: run, discard: discard,
	})
	id := registrationID(len(e.specs) - 1)
	e.byName[name] = id
	e.workers = append(e.workers, worker)
	// The edge is walked from whoever committed, so it is indexed that way here rather than
	// searched for at wake time. Both ends exist already, which is what makes it safe.
	for _, watched := range watches {
		e.watchers[watched] = append(e.watchers[watched], id)
	}
}

// resolveLocked turns an edge's names into indexes. A name nothing was registered under is a
// wiring bug: the edge names something that does not exist, or something that comes later.
func (e *Supervisor) resolveLocked(name, edge string, names []string) []registrationID {
	if len(names) == 0 {
		return nil
	}
	ids := make([]registrationID, 0, len(names))
	for _, want := range names {
		id, ok := e.byName[want]
		if !ok {
			panic(fmt.Sprintf("supervisor: %q %s %q, which is not registered yet", name, edge, want))
		}
		ids = append(ids, id)
	}
	return ids
}

// Add tracks subject. Everything registered derives from zero, so the pass this queues dispatches
// whatever a fresh subject owes; adding a subject already tracked changes nothing.
func (e *Supervisor) Add(subjectName string) {
	e.mu.Lock()
	if _, ok := e.subjects[subjectName]; ok {
		e.mu.Unlock()
		return
	}
	e.subjects[subjectName] = &subject{obs: make([]observable, len(e.specs))}
	e.mu.Unlock()

	e.passQ.Add(subjectName)
}

// Remove stops tracking subject and **waits for every run against it to return**: its timer
// stops, each run in flight is cancelled and joined, and what those runs commit is refused. Every
// value it was holding is handed back. Past here nothing can still be writing on the subject's
// behalf, which is what a caller about to release what a run wrote through needs. Removing a
// subject not tracked changes nothing.
//
// Because it joins, it must not be called from inside a Run.
func (e *Supervisor) Remove(subjectName string) {
	e.mu.Lock()
	sub := e.subjects[subjectName]
	if sub == nil {
		e.mu.Unlock()
		return
	}
	sub.stopTimer()
	delete(e.subjects, subjectName)
	running := e.stopAllLocked(sub)
	held := e.heldLocked(sub)
	e.mu.Unlock()

	joinAll(running)
	e.handBackAll(held)
}

// Wake says these registrations' answers are stale: run them again, suspension notwithstanding.
// It adds straight to the run queue, whose held/dirty machinery redelivers a key that was mid-run
// when its commit lands — a Wake is never lost.
//
// **A run already going is left alone**, which for a worker means "when you next stop, start
// again at once": it un-parks a Suspend the worker was about to enter and makes a rotation's
// restart immediate, and a live worker is never torn down by one. A subject not tracked is
// ignored; a name nothing was registered under is a wiring bug and panics.
func (e *Supervisor) Wake(subjectName string, names ...string) {
	ids, tracked := e.resolveWake(subjectName, names)
	if !tracked {
		return
	}
	for _, id := range ids {
		e.runQ.Add(key{subject: subjectName, registration: id})
	}
}

// resolveWake reads what a Wake needs under one lock. Deferred rather than unlocked inline,
// because namesLocked panics on a name nothing was registered under and a caller that recovers
// must not be left holding the supervisor's lock.
func (e *Supervisor) resolveWake(subjectName string, names []string) ([]registrationID, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.namesLocked("Wake", names), e.subjects[subjectName] != nil
}

// WakeAll is Wake over every tracked subject.
func (e *Supervisor) WakeAll(names ...string) {
	e.mu.Lock()
	subjects := slices.Collect(maps.Keys(e.subjects))
	e.mu.Unlock()

	for _, subjectName := range subjects {
		e.Wake(subjectName, names...)
	}
}

// Restart is Wake with the current run stopped first: its context is cancelled and its exit
// records nothing, so the replacement is immediate rather than laddered. A job mid-dial against a
// machine that just woke, and a worker streaming over a connection that just changed, are neither
// left to time out nor left to notice on their own.
//
// **It does not wait.** Three hundred cancels are one poke; three hundred joins of unwinding cold
// lists on the poke's own goroutine are not — and a call that cannot block is one that is safe to
// make from anywhere, a lock held or not.
//
// One race is accepted: a Restart landing while the run is already returning Fail sets the flag
// first, so that failure is not recorded and the restart is immediate rather than laddered. A
// caller restarting often enough could hold the streak at zero.
func (e *Supervisor) Restart(subjectName string, names ...string) {
	ids, tracked := e.stopForRestart(subjectName, names)
	if !tracked {
		return
	}
	for _, id := range ids {
		e.runQ.Add(key{subject: subjectName, registration: id})
	}
}

// stopForRestart cancels what a Restart is replacing, under the same lock that resolves its
// names — deferred for the reason resolveWake's is.
func (e *Supervisor) stopForRestart(subjectName string, names []string) ([]registrationID, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	ids := e.namesLocked("Restart", names)
	sub := e.subjects[subjectName]
	if sub == nil {
		return nil, false
	}
	for _, id := range ids {
		e.stopRunLocked(sub, id)
	}
	return ids, true
}

// RestartAll is Restart over every tracked subject.
func (e *Supervisor) RestartAll(names ...string) {
	e.mu.Lock()
	subjects := slices.Collect(maps.Keys(e.subjects))
	e.mu.Unlock()

	for _, subjectName := range subjects {
		e.Restart(subjectName, names...)
	}
}

// namesLocked resolves the names a Wake or Restart addresses. A name nothing was registered under
// is a wiring bug and panics, as it does at registration.
func (e *Supervisor) namesLocked(call string, names []string) []registrationID {
	ids := make([]registrationID, 0, len(names))
	for _, name := range names {
		id, ok := e.byName[name]
		if !ok {
			panic(fmt.Sprintf("supervisor: %s of %q, which is not registered", call, name))
		}
		ids = append(ids, id)
	}
	return ids
}

// stopRunLocked cancels the run in flight for one registration and marks its end as asked for, so
// its exit records nothing. It hands the handle back for a caller that means to wait; nil when
// nothing is out. The cancel never blocks, which is what makes this safe under e.mu — and commit,
// which holds the lock, is one of its callers.
func (e *Supervisor) stopRunLocked(sub *subject, id registrationID) *runHandle {
	h := sub.obs[id].run
	if h == nil {
		return nil
	}
	h.stopped = true
	h.cancel()
	return h
}

// stopAllLocked stops every run in flight against a subject, for Remove and Close.
func (e *Supervisor) stopAllLocked(sub *subject) []*runHandle {
	var running []*runHandle
	for id := range sub.obs {
		if h := e.stopRunLocked(sub, registrationID(id)); h != nil {
			running = append(running, h)
		}
	}
	return running
}

// joinAll waits for stopped runs to return, OUTSIDE e.mu — a run's own commit takes it. A
// cancelled job unwinds as fast as a cancelled worker, since every body already returns on its
// ctx, so the wait is short.
func joinAll(running []*runHandle) {
	for _, h := range running {
		<-h.done
	}
}

// Read is subject's observables, copied under one lock so the values and the schedule beside
// them agree. ok is false for a subject not tracked.
func (e *Supervisor) Read(subjectName string) (snap Snapshot, ok bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	sub := e.subjects[subjectName]
	if sub == nil {
		return Snapshot{}, false
	}
	return e.snapshotOf(sub), true
}

// Start runs the pass loop and the dispatcher, and hands back the stop func that cancels them and
// waits. ctx bounds startup alone, and nothing here can fail — the queues were built in New, so
// work queued before Start (an Add, a Wake) simply waits there.
func (e *Supervisor) Start(context.Context) func(context.Context) error {
	e.mu.Lock()
	e.started = true
	e.mu.Unlock()

	// Not Start's context, which bounds startup: this one bounds the loops and every run under
	// them, so it lives until the stop func cancels it — which is also what ends the workers.
	loopCtx, stopLoops := context.WithCancel(context.Background())

	e.wg.Go(func() { e.passLoop(loopCtx) })
	e.wg.Go(func() { e.dispatchLoop(loopCtx) })

	return func(ctx context.Context) error {
		stopLoops()
		return drain.WithContext(ctx, e.wg.Wait)
	}
}

// Close drops every subject, waits for the runs against them, hands back every value they held,
// and closes the queues. Past here nothing works them off.
//
// Because it joins, it must not be called from inside a Run.
func (e *Supervisor) Close() error {
	e.runQ.Close()
	e.passQ.Close()

	e.mu.Lock()
	var running []*runHandle
	var held []heldValue
	for _, sub := range e.subjects {
		sub.stopTimer()
		running = append(running, e.stopAllLocked(sub)...)
		held = append(held, e.heldLocked(sub)...)
	}
	clear(e.subjects)
	e.mu.Unlock()

	joinAll(running)
	e.handBackAll(held)
	return nil
}

// heldValue is one standing value and the registration to give it back to.
type heldValue struct {
	id  registrationID
	val any
}

// heldLocked collects what a subject is holding, for a caller about to drop it. Called under
// e.mu; handing back is not.
func (e *Supervisor) heldLocked(sub *subject) []heldValue {
	var held []heldValue
	for id := range sub.obs {
		if v := sub.obs[id].value; v != nil && e.specs[id].discard != nil {
			held = append(held, heldValue{registrationID(id), v})
		}
	}
	return held
}

// handBackAll returns values to whoever committed them, OUTSIDE e.mu: a Discard can join a
// goroutine whose exit calls Wake, which takes the lock. A value is handed back once — the
// subject it stood on is already gone by here, so nothing can reach it again.
func (e *Supervisor) handBackAll(held []heldValue) {
	for _, h := range held {
		e.specs[h.id].discard(h.val)
	}
}

// passLoop derives schedules until stopped. One worker: the pass is arithmetic under the
// supervisor's lock, so more would only contend for it — and one is what serializes OnPass
// per subject.
func (e *Supervisor) passLoop(ctx context.Context) {
	for {
		name, ok := e.passQ.Next(ctx)
		if !ok {
			return
		}
		e.pass(name)
		e.passQ.Done(name)
	}
}

// dispatchLoop starts due runs until stopped. One dispatcher and a goroutine per run, because a
// worker's Run blocks for its whole life: a pool of loops running bodies inline would have the
// first worker pin one of them forever, and hold its key with it.
//
// The slot is taken HERE rather than inside the run. The run queue is ordered, so this is the
// gate — a dispatcher handing keys out faster than runs finish would leave the cap counting
// goroutines rather than bounding starts.
func (e *Supervisor) dispatchLoop(ctx context.Context) {
	for {
		k, ok := e.runQ.Next(ctx)
		if !ok {
			return
		}
		select {
		case e.slots <- struct{}{}:
		case <-ctx.Done():
			// A dispatcher parked on a full semaphore is not parked on Next, so without this
			// arm it would not see the stop until a run happened to finish.
			// Shutdown needs no scheduling recheck: this dispatcher is stopping.
			e.runQ.Done(k)
			return
		}
		e.wg.Go(func() {
			e.runQueued(ctx, k, sync.OnceFunc(func() { <-e.slots }))
		})
	}
}

// runQueued holds the queue key for the whole run, for both kinds. A wake mid-run becomes
// a dirty mark and a redelivery rather than a second Run beside the live one.
func (e *Supervisor) runQueued(ctx context.Context, k key, release func()) {
	defer func() {
		e.runQ.Done(k)
		// A pass may have found this run due again while its key was still held.
		// Recheck after releasing it so that scheduled retries are not lost.
		e.passQ.Add(k.subject)
	}()
	e.runOne(ctx, k, release)
}

// pass re-derives one subject's schedule and publishes: due runs go on runQ, the soonest future
// time arms the subject's one timer, and OnPass fires after — outside the lock, serialized
// because passLoop is the one caller. It is the only publisher, so every state a reader sees
// carries a schedule that matches the answers beside it.
//
// A run in flight is left alone: NextAttempt is that run, and writing a schedule over it would
// erase both the in-flight mark and the schedule it was dispatched on — and for a worker it would
// schedule a second start over one that is up. The exit's record is what re-enters the schedule.
func (e *Supervisor) pass(subjectName string) {
	e.mu.Lock()
	sub := e.subjects[subjectName]
	if sub == nil {
		e.mu.Unlock()
		return
	}

	now := e.tick()
	var soonest time.Time
	for id := range e.specs {
		a := &sub.obs[id].Attempts
		if a.InFlight() {
			continue
		}
		at := e.due(sub, registrationID(id), now)
		a.schedule(at)

		switch {
		case at.IsZero():
			// Suspended: nothing is due, and the last answer stands.
		case at.After(now):
			if soonest.IsZero() || at.Before(soonest) {
				soonest = at
			}
		default:
			// A dispatcher may hold the key before runOne marks it in flight. This
			// schedule still describes that run, so it must not request a redelivery.
			e.runQ.AddIfAbsent(key{subject: subjectName, registration: registrationID(id)})
		}
	}

	sub.stopTimer()
	if !soonest.IsZero() {
		sub.timer = time.AfterFunc(soonest.Sub(now), func() { e.passQ.Add(subjectName) })
	}

	var snap Snapshot
	if e.onPass != nil {
		snap = e.snapshotOf(sub)
	}
	e.mu.Unlock()

	if e.onPass != nil {
		e.onPass(subjectName, snap)
	}
}

// dependenciesState is where a registration's dependencies collectively are; a failing one
// outranks an unanswered one, since it is a definitive reason not to run.
type dependenciesState int

const (
	dependenciesOK dependenciesState = iota
	dependenciesUnanswered
	dependenciesFailing
)

func (e *Supervisor) dependenciesOf(sub *subject, dependencies []registrationID) dependenciesState {
	state := dependenciesOK
	for _, id := range dependencies {
		dep := sub.obs[id]
		if e.specs[id].worker {
			// A worker's health is whether it is UP, not what its last exit said: its value
			// is a status that stops holding the moment it stops running. Starting is
			// unanswered, and so is one nothing has dispatched; everything else is down,
			// which is a definitive reason not to run over it.
			switch {
			case dep.Ready():
			case dep.InFlight(), !dep.LastAttempt.Done() && !dep.skipped:
				state = dependenciesUnanswered
			default:
				return dependenciesFailing
			}
			continue
		}
		switch {
		case dep.LastAttempt.Done() && !dep.OK():
			return dependenciesFailing
		case !dep.LastAttempt.Done():
			state = dependenciesUnanswered
		}
	}
	return state
}

// due is when registration id should next run, zero for nothing scheduled. The whole scheduling
// policy, in the order the cases have to be read. The caller has already set aside a run in
// flight.
//
// Both kinds read the same table, and the succeeded branch is where they differ in meaning rather
// than in code: for a job the interval is a rest, for a worker it is the floor under a clean
// restart. A worker that knows the wait is pointless shortens it with RequeueAfter the way a job
// does.
func (e *Supervisor) due(sub *subject, id registrationID, now time.Time) time.Time {
	a := sub.obs[id]
	if a.skipped {
		// The last run declined to record; only a Wake or a Restart brings it back.
		return time.Time{}
	}

	cfg := e.specs[id].cfg

	switch e.dependenciesOf(sub, e.specs[id].dependencies) {
	case dependenciesUnanswered:
		// Nothing to say about a server nobody has tried, so this stays untouched rather than
		// recording a dependency that has not failed.
		return time.Time{}
	case dependenciesFailing:
		// One run records DependencyFailed and the rest of the outage costs nothing, which
		// is what keeps a dead cluster at one timeout per cycle instead of one per probe.
		if a.LastAttempt.Reason == ReasonDependencyFailed {
			return time.Time{}
		}
		return now
	}

	switch a.LastAttempt.Verdict {
	case VerdictSucceeded:
		// The interval unless the run asked for less: RequeueAfter accelerates and never
		// lengthens, and an unset one leaves the registration standing rather than
		// scheduling at zero.
		wait := cfg.interval
		if d := a.LastAttempt.requeueAfter; d > 0 && d < wait {
			wait = d
		}
		return a.LastAttempt.FinishedAt.Add(wait)
	case VerdictFailed:
		return a.LastAttempt.FinishedAt.Add(cfg.backoff.Delay(a.Failures))
	case VerdictSuspended:
		if a.LastAttempt.Reason == ReasonDependencyFailed {
			// Its dependencies came back. This is the whole re-arm — nothing has to notice
			// the recovery and go looking for what suspended on it.
			return now
		}
		return time.Time{}
	default:
		// Never run, and runnable.
		return now
	}
}

// runOne runs one due registration and commits what it found. It blocks for a job's whole run and
// for a worker's whole life, which is why the dispatcher gives it a goroutine of its own.
//
// The subject is captured first and re-checked at the commit — what can race a run is a Remove,
// not another run of the same key.
//
// Dependencies are re-checked at dispatch: one that failed since the pass means the run is
// recorded as DependencyFailed, never dialed — nothing must spend a timeout learning what the
// state already says. One still unanswered means a Wake outran it; the run ends as a no-op and
// the pass owns the question again.
func (e *Supervisor) runOne(ctx context.Context, k key, release func()) {
	defer release()

	e.mu.Lock()
	sub := e.subjects[k.subject]
	if sub == nil {
		e.mu.Unlock()
		return
	}
	sp := e.specs[k.registration]
	a := &sub.obs[k.registration]
	req := runReq{subject: k.subject, prev: a.value, snap: e.snapshotOf(sub)}
	dependencies := e.dependenciesOf(sub, sp.dependencies)

	// Marked before the lock is dropped, so InFlight is true for as long as the run is out and
	// a pass landing meanwhile leaves it alone.
	startedAt := e.tick()
	a.begin(startedAt)
	runCtx, cancel := context.WithCancel(ctx)
	h := &runHandle{cancel: cancel, done: make(chan struct{})}
	a.run = h
	e.mu.Unlock()

	// A pass is what publishes, so without this ask the in-flight window — a probe's whole
	// round-trip — reaches no reader. An ask rather than a snapshot taken here: passLoop is
	// what serializes OnPass, and one taken on this goroutine could land after the commit's.
	e.passQ.Add(k.subject)

	// done closes after the commit, so a joiner that saw it knows what the run concluded has
	// landed. Both after cancel, which releases the context whichever way the run ended.
	defer close(h.done)
	defer cancel()

	var out runOutcome
	ran := false
	switch dependencies {
	case dependenciesOK:
		req.ctx = runCtx
		out = e.dispatch(sp, k, sub, h, req, release)
		ran = true
	case dependenciesFailing:
		out.res = Suspend(ReasonDependencyFailed, "a dependency is failing")
		startedAt = time.Time{} // recorded, never dispatched
	default:
		// dependenciesUnanswered: a Wake outran the pass. Nothing has been said about the
		// server this depends on, so there is nothing to record and nothing to dial — the run
		// ends as a no-op and the pass owns the question again. It is the supervisor's Skip
		// rather than the body's, which is what `ran` tells commit apart.
		out.res = Skip()
	}
	e.commit(k, sub, h, sp, startedAt, out, ran)
}

// dispatch runs the body, bounded by its timeout, and answers for it when it misbehaves. A body
// that panics or hands back nothing is a bug, and both must still produce a result — otherwise
// the registration reads as in flight forever and its key stays held in the queue. Only here does
// the supervisor log: the Internal record a caller sees carries no stack, and nothing else in
// the system can report a bug in a body.
//
// The two kinds part over the timeout. A job's bounds its whole run, so it rides the context. A
// worker's bounds only the start, so it is a timer that cancels the run and is stopped by Ready.
func (e *Supervisor) dispatch(sp spec, k key, sub *subject, h *runHandle, req runReq, release func()) (out runOutcome) {
	if sp.worker {
		markReady := func() { e.markReady(k, sub, h); release() }
		if sp.cfg.timeout > 0 {
			startup := time.AfterFunc(sp.cfg.timeout, func() { e.startupExpired(h) })
			defer startup.Stop()
			// Ready is stamped BEFORE the timer is stopped, and that order is what closes the
			// race: Stop does not wait for a callback already running, so the stamp under the
			// lock is what tells it there is nothing left to end.
			markReady = func() { e.markReady(k, sub, h); startup.Stop(); release() }
		}
		req.ready = markReady
		req.commit = func(v any) { e.commitLive(k, sub, h, v) }
	} else if sp.cfg.timeout > 0 {
		var cancelTimeout context.CancelFunc
		req.ctx, cancelTimeout = context.WithTimeout(req.ctx, sp.cfg.timeout)
		defer cancelTimeout()
	}

	defer func() {
		if r := recover(); r != nil {
			slog.Error("supervisor: run panicked", "registration", sp.name, "subject", req.subject,
				"panic", r, "stack", string(debug.Stack()))
			// The run handed its value back on the way out of the panic, so there is
			// nothing left here to discard.
			out = runOutcome{res: Fail(ReasonInternal, fmt.Errorf("%s panicked: %v", sp.name, r))}
		}
	}()

	out = sp.run(req)
	if out.res.kind == resultInvalid {
		slog.Error("supervisor: run returned the zero Result", "registration", sp.name, "subject", req.subject)
		if out.discard != nil {
			out.discard()
		}
		return runOutcome{res: Fail(ReasonInternal, fmt.Errorf("%s returned the zero Result", sp.name))}
	}
	return out
}

// startupExpired is the startup timer firing: the worker has not become ready in the time its
// registration allows. It ends the run — and marks WHY, since a run the supervisor cut short is
// recorded rather than parked, however the body reports the cancel.
//
// **This and markReady are one decision, and the lock is what settles it.** Whichever takes it
// first wins: a run already ready is left alone here, and a Ready arriving after this is refused
// there. Stopping the timer is the cheap path, not the guarantee — Stop does not wait for a
// callback already running.
func (e *Supervisor) startupExpired(h *runHandle) {
	e.mu.Lock()
	expired := h.readyAt.IsZero()
	h.timedOut = expired
	e.mu.Unlock()

	if expired {
		h.cancel()
	}
}

// markReady is a worker saying its starting phase is over. It stamps the attempt, ends the
// failure streak and opens a healthy stretch, then asks for a pass — so whatever depends on this
// worker is scheduled now that it is up.
//
// Idempotent, because Ready is: the stamp is written once and the rest with it.
func (e *Supervisor) markReady(k key, held *subject, h *runHandle) {
	e.mu.Lock()
	a := &held.obs[k.registration]
	// Refused once the startup timer has ended the run: a first frame can arrive as the cancel
	// does, and admitting it would open a healthy stretch and clear the streak over a start that
	// had already failed — then leave the exit outside the never-ready rule, so a body reporting
	// its cancelled context with a Skip would park for good.
	fresh := e.subjects[k.subject] == held && a.run == h && h.readyAt.IsZero() && !h.timedOut
	if fresh {
		h.readyAt = e.tick()
		a.markReady(h.readyAt)
	}
	e.mu.Unlock()

	if fresh {
		e.passQ.Add(k.subject)
	}
}

// commitLive applies a worker's commit the moment it is made: the value and its stamp under the
// lock, the watchers woken in the same critical section, and a pass asked for after — so OnPass
// fires for it from the pass loop, serialized, as every publication is.
//
// A commit from a run the subject no longer holds is refused and NOT handed back. A worker's
// value owns nothing, which is exactly what makes that free.
func (e *Supervisor) commitLive(k key, held *subject, h *runHandle, v any) {
	e.mu.Lock()
	if e.subjects[k.subject] != held || held.obs[k.registration].run != h {
		e.mu.Unlock()
		return
	}
	a := &held.obs[k.registration]
	a.value, a.seen = v, e.tick()
	e.wakeWatchersLocked(k, held)
	e.mu.Unlock()

	e.passQ.Add(k.subject)
}

// wakeWatchersLocked runs everything that reads this registration's value, because that value
// moved. Called in the same critical section as the write, so no reader sees the value without
// the runs it earned. Health is not consulted: a watch is a data edge, and something that also
// cannot run without this declares WithDependencies for that.
//
// **A watching worker is restarted rather than woken**: its input moving means the one it is
// running on is stale. The cancel never blocks, which is what makes it safe from under the lock.
func (e *Supervisor) wakeWatchersLocked(k key, held *subject) {
	for _, watcher := range e.watchers[k.registration] {
		if e.specs[watcher].worker {
			e.stopRunLocked(held, watcher)
		}
		e.runQ.Add(key{subject: k.subject, registration: watcher})
	}
}

// commit files what a run concluded and asks for a pass. Called for every run, including one that
// concluded nothing: ending the run is what lets the pass schedule this registration again.
//
// The subject is re-checked under the lock, so a Remove landing mid-run cannot let a run that
// predates a re-Add be committed against whatever holds the name now.
func (e *Supervisor) commit(k key, held *subject, h *runHandle, sp spec, startedAt time.Time, out runOutcome, ran bool) {
	e.mu.Lock()
	if e.subjects[k.subject] != held || held.obs[k.registration].run != h {
		e.mu.Unlock()
		// Nothing will ever see what this run committed, so it goes back to the job: a value
		// can own something nothing else can now reach to release.
		if out.discard != nil {
			out.discard()
		}
		return
	}

	a := &held.obs[k.registration]
	a.run = nil
	now := e.tick()

	// **A stop is not an attempt.** Restart, Remove and Close ask for the end, so it is not the
	// body's own doing: the last record and the failure streak stand, and only the schedule
	// moves — which is what makes a poke over three hundred workers cost no rung, and what
	// keeps a worker stopped before Ready from reading NeverReady.
	if h.stopped {
		a.schedule(now)
		e.mu.Unlock()
		if out.discard != nil {
			out.discard()
		}
		e.passQ.Add(k.subject)
		return
	}

	res := out.res
	if sp.worker && h.readyAt.IsZero() {
		// **A worker that never proved itself, whose exit would otherwise leave it unpaced.**
		// Two ways to get here, and each would go wrong differently:
		//
		//   - It says it finished cleanly. It has proven nothing, and restarting it at the
		//     floor would hot-loop against a server that accepts every start and drops it.
		//   - The startup timer ended it. Whatever it then returns is an answer to OUR cancel
		//     rather than a verdict it chose, so a Skip or a Suspend taken at its word would
		//     park it on a wake nobody owes it.
		//
		// Either way "never ready" is the failure, the ladder paces it, and the first Ready is
		// what resets the count. A Fail is left alone — it carries a better reason up the same
		// ladder — and so is a Suspend or a Skip the worker chose for itself, which is it
		// parking at a gate rather than failing at one.
		switch {
		case res.verdict == VerdictSucceeded:
			res = Fail(ReasonNeverReady, fmt.Errorf("%s stopped before it was ready", sp.name))
		case h.timedOut && res.verdict != VerdictFailed:
			res = Fail(ReasonNeverReady, fmt.Errorf("%s was not ready within %s", sp.name, sp.cfg.timeout))
		}
	}

	if ran {
		a.LastRunAt = startedAt
	}

	switch res.kind {
	case resultRecord:
		a.record(attemptOf(res, startedAt, h.readyAt, now))
		a.skipped = false
		switch {
		case out.val != nil:
			// seen dates the value, and a value is confirmed when it is committed —
			// whatever the verdict, since a run that failed can still have read something
			// (which components are down). Dating it by the last success instead would
			// leave this answer undated, and would date a replaced one by a read of what
			// it replaced.
			a.value, a.seen = out.val, now
			// A committed value is one that moved — the body says so by handing one back at
			// all — and whoever reads it is owed a run against the new one.
			e.wakeWatchersLocked(k, held)
		case !sp.worker && res.verdict == VerdictSucceeded && a.value != nil:
			// Nothing new, and the job's run confirmed what stands. A success with nothing
			// to date leaves seen alone: a reader's Known guard would otherwise pass and
			// hand it the zero value.
			//
			// **A worker's clean exit confirms nothing**, which is what keeps ChangedAt the
			// stamp it is named for: a rotation is the worker ending, not it speaking, and a
			// reader telling the two apart by the stamp against the attempt would read the
			// exit as this run having answered.
			a.seen = now
		}
	case resultSkip:
		// **Only a body's Skip parks.** It is a run declining to record, and nothing but a
		// Wake brings it back. The supervisor's own no-op over an unanswered dependency is not
		// that: parking there would strand the registration on the wake it never earned, when
		// what it is waiting for — the dependency answering — asks for a pass by itself.
		if ran {
			a.skipped = true
		}
		// A Skip records nothing, its value included.
		if out.discard != nil {
			defer out.discard()
		}
	}
	// Due now rather than suspended: the pass this asks for is a queue hop away, and a zero
	// here would read as suspended for as long as that takes.
	a.schedule(now)
	e.mu.Unlock()

	e.passQ.Add(k.subject)
}
