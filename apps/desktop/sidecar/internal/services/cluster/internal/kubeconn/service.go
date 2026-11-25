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

// Package kubeconn hands out leases on kube-contexts and reports what probing the server behind
// one found.
//
// The probes live in probe.go and the scheduling around them is the supervisor's
// (internal/lib/supervisor). This file is the rest: leases, publishing what the supervisor observes, and
// retiring the connections its runs build.
//
// Three rules shape it:
//
//   - One context, one entry. Contexts resolving to the same credentials are not merged, so
//     what is learned about one belongs to exactly one context.
//   - A claim outlives what it is a claim on. The kubeconfig can stop naming a context while a
//     holder still holds it, and the entry stays: the file may name it again, and the claim is
//     how the holder hears about that. Only releasing drops an entry.
//   - It never learns what a cluster is. Callers name kube-contexts; whether the server behind
//     one is the cluster the caller meant is the caller's to decide.
package kubeconn

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amorey/gobus/conflate"
	"github.com/amorey/gobus/watch"
	"k8s.io/client-go/rest"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/drain"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/supervisor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/kubeconfig"
)

// kubeconfigService is the reader this package asks whether a context still resolves, and which
// tells it when the file changed. The fingerprint RESTConfig returns beside the config describes
// the credentials, and is what tells a rotation from a write that changed nothing.
type kubeconfigService interface {
	RESTConfig(contextName string) (*rest.Config, string, error)
	WatchConfigState() kubeconfig.ConfigStateWatch
}

// ConnectionChangeNotifications reports the contexts whose connection changed, one notice per
// context, for a reader whose reaction to any of them is the same: re-read that claim. A keyed,
// coalescing bus, so a fleet changing at once neither loses a context behind a busier one nor
// overflows a buffer. The notice carries nothing but its key.
type ConnectionChangeNotifications = *conflate.Receiver[string, struct{}]

// DataRateChangeNotifications reports a new data-rate sample for one claim's context. Each
// notice carries nothing but its key; the reader re-reads the rate.
type DataRateChangeNotifications = *conflate.Receiver[string, struct{}]

// Lease is a claim on one kube-context: what keeps the pool tracking it, and how a holder hears
// about it. An interface because it crosses out of this package, where a caller's test has to
// stand in for a live cluster.
type Lease interface {
	// Conn is the connection to the context's server, or ErrNoConnection while the context
	// resolves to nothing. It never dials: what it hands back is what the connection probe
	// built, failing last probe included.
	Conn(ctx context.Context) (*Connection, error)
	// State is what the probes last found, probe by probe. It never dials.
	State() State
	// ConnFor is Conn, refused with ErrIdentityMismatch unless the server behind it is
	// serverUID — for a caller whose work belongs to one cluster rather than to a context.
	//
	// It reads the identity off the connection itself, never off State: State.ServerUID is
	// its own probe's observable, queued by a committed connection rather than applied by
	// it, so a fresh connection sits beside the previous one's UID until that probe re-runs.
	// A connection nobody has identified yet is refused for the same reason.
	ConnFor(ctx context.Context, serverUID string) (*Connection, error)
	// WatchState is a state watch of this claim: its State now, then every State published
	// for it.
	//
	// **The caller closes it**, as with ListenForConnectionChanges. Release does not: the
	// watch is keyed by context, so one that outlives its claim keeps a hub slot and goes on
	// reporting whatever claims that name next.
	WatchState() *StateWatch
	// DataRates is the context's last samples, oldest first: up to DataRateWindow of them at
	// DataRateInterval. Empty until the second tick after the context was first claimed.
	DataRates() []DataRateSample
	// ListenForDataRateChanges fires after each tick adds a sample. It carries no value: the reader
	// calls DataRates for the samples, so one that falls behind loses nothing. The caller
	// closes it, as with WatchState.
	ListenForDataRateChanges() DataRateChangeNotifications
	// Departed reports that the kubeconfig no longer names this claim's context.
	//
	// Not an error and not final: the file may name it again, and this claim is how the
	// holder hears about that. ListenForConnectionChanges and WatchState both fire when this flips.
	Departed() bool
	// Release drops the claim, and the entry with it once nothing else holds the context.
	// Idempotent, so it is safe to defer.
	Release()
}

// Service is the pool the cluster service leases contexts from. The supervisor tracks one
// subject per claimed context; this type owns who holds the claims and what the fleet is told.
type Service struct {
	kubecfgSvc kubeconfigService
	// supervisor runs the five probes of probe.go over the claimed contexts; probes is what
	// registering them returned.
	supervisor *supervisor.Supervisor
	// connectionChangeHub names the contexts whose news changed; stateHub carries what the probes read.
	// Both keyed by context, both fed by publish.
	connectionChangeHub *conflate.Hub[string, struct{}]
	stateHub            *watch.Hub[string, State]
	// dataRateChangeHub names the contexts the sampler added a sample for. Apart from connectionChangeHub, so a
	// tick never wakes a reconcile.
	dataRateChangeHub *conflate.Hub[string, struct{}]

	// mu guards claimed and published together: what publish announces is measured against
	// who still holds the claim, and nothing may see one without the other.
	mu sync.Mutex
	// claimed holds one entry per claimed context — a key is here exactly while someone holds
	// that context, mirrored by a subject in the supervisor — and is also the key both hubs
	// publish under.
	claimed map[string]*entry
	// published is the news the fleet was last told per context, compared against so a pass
	// that moved only timing wakes nobody.
	published map[string]news

	// dataRateTicker paces the sampler and now stamps its readings; a test drives both by hand.
	dataRateTicker func() (<-chan time.Time, func())
	now            func() time.Time

	started atomic.Bool
	wg      sync.WaitGroup
}

// entry is a claimed context's holder count and the connection its probe built. A pointer per
// context on purpose: a claim carries the entry it was given, which is what stops a release that
// outlived Close from touching whatever claims the name next.
//
// The connection is here as well as in the supervisor's observable because the two answer different
// questions: the supervisor's copy is what a run reads and what Conn hands out, and this one is who
// to retire when the entry goes — a supervisor a Remove has already emptied can name nobody.
type entry struct {
	holders int
	conn    *Connection
	// rates is the sampler's ring for this context, guarded by the pool's mu.
	rates dataRateRing
}

// New returns a Service over the one reader of the user's kubeconfig. opts tune every
// connection its probe builds.
func New(kubecfgSvc kubeconfigService, opts ...Option) *Service {
	// Here rather than in the composition root because the vars are read when a transport is
	// built, and this package builds them — a call the root has to remember is one that goes
	// missing.
	configureHTTP2Keepalive()

	s := &Service{
		kubecfgSvc:          kubecfgSvc,
		supervisor:          supervisor.New(),
		connectionChangeHub: conflate.New[string, struct{}](),
		stateHub:            watch.New[string, State](),
		dataRateChangeHub:   conflate.New[string, struct{}](),
		claimed:             map[string]*entry{},
		published:           map[string]news{},
		now:                 time.Now,
		dataRateTicker: func() (<-chan time.Time, func()) {
			return newTicker(DataRateInterval)
		},
	}
	registerProbes(s.supervisor, kubecfgSvc, opts)
	s.supervisor.OnPass(s.publish)
	return s
}

// Acquire claims contextName and hands back the lease on it. It never fails and never waits: a
// context the kubeconfig does not name yet is claimable, because the file may name it later and
// the claim is how the holder finds out.
//
// The first holder adds the context to the supervisor, whose first pass dispatches the connection
// probe — not work to do on the caller's thread. A later holder joins what the probes found.
func (s *Service) Acquire(contextName string) Lease {
	s.mu.Lock()
	e, held := s.claimed[contextName]
	if !held {
		e = &entry{}
		s.claimed[contextName] = e
	}
	e.holders++
	s.mu.Unlock()

	if !held {
		s.supervisor.Add(contextName)
	}
	return &claim{svc: s, contextName: contextName, entry: e}
}

// retryCeiling bounds RetryAndWait once the run it asked for has BEGUN — a run still owed a
// worker slot is not covered, and must not be. Sized off the probe's own timeout plus slack: a
// run that outlives its own deadline is not going to end.
const retryCeiling = connectionTimeout + 5*time.Second

// RetryAndWait runs every probe on contextName now — whatever it was next due at and whatever
// suspended it — and returns once the connection probe it asked for has finished. For a user who
// fixed something no probe can observe (a VPN dialed, a credential rotated) and should not have to
// wait out the cadence, and whose button stays busy for exactly as long as their probe does.
//
// **ctx is what bounds the queue wait**, so a caller that will not wait forever must say so.
//
// All five rather than the connection alone: a connection that is already up commits nothing, so
// waking it would leave a probe that failed on its own — a forbidden kube-system read — suspended
// on the answer the user just fixed. The connection alone decides when this returns, because that
// is what a retry asks: can we reach it.
//
// What the probe found is not returned. It reaches watchers the way every other pass does.
func (s *Service) RetryAndWait(ctx context.Context, contextName string) error {
	return s.retryAndWait(ctx, contextName, retryCeiling)
}

// retryAndWait takes the ceiling as a parameter so a test never outwaits the production one.
//
// **The ask is satisfied by the first finished run that began at or after it.** A run already in
// flight began before, so it correctly does not answer — the supervisor holds the key and
// redelivers the wake, so the run the caller asked for is the next one to begin. `LastRunAt` is
// what makes that readable: the state feed keeps only the latest value per context, so edges are
// coalesced away, but a monotonic level compared against a baseline survives it.
//
// **The ceiling starts when that run does, never at the ask.** Dispatch waits for one of the
// supervisor's fleet-wide start slots, so a fleet with enough queued probes to fill them delays
// the run by however long those take — bounded, since every job carries a timeout, but by nothing
// this call can name. A ceiling run from the ask would report a failure for a probe nobody had
// tried yet.
func (s *Service) retryAndWait(ctx context.Context, contextName string, ceiling time.Duration) error {
	// The supervisor's clock, because LastRunAt is stamped from it and it is not the wall
	// clock: a baseline read off the wall could sit behind a stamp already issued, and a run
	// that finished before the ask would answer it. Read before the claim, so a run that begins
	// once the claim is visible began after the ask.
	askedAt := s.supervisor.Now()

	// A Wake on a subject nothing tracks is a no-op, so without a claim this would wait for a
	// run that is never dispatched. Refcounted alongside whatever else holds the context.
	lease := s.Acquire(contextName)
	defer lease.Release()

	// Before the wake, or a pass landing between the two is missed.
	sub := lease.WatchState()
	defer sub.Close()

	s.supervisor.Wake(contextName, probeNames[:]...)

	// The caller's context is the only bound until the run begins.
	for {
		st, err := sub.RecvContext(ctx)
		if err != nil {
			return fmt.Errorf("retry %q: %w", contextName, err)
		}
		conn := st.Connection
		if answers(conn, askedAt) {
			return nil
		}
		// The in-flight frame can be coalesced away by the commit that follows it, which
		// answers above — so missing it never strands the wait, it only leaves the caller's
		// context as the whole bound.
		if conn.InFlight() && !conn.NextAttempt.StartedAt.Before(askedAt) {
			return awaitRun(ctx, sub, contextName, askedAt, ceiling)
		}
	}
}

// awaitRun waits out the run that has begun, under the ceiling. A run that outlives the probe's
// own timeout is not going to end on its own.
func awaitRun(ctx context.Context, sub *StateWatch, contextName string, askedAt time.Time, ceiling time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, ceiling)
	defer cancel()

	for {
		st, err := sub.RecvContext(ctx)
		if err != nil {
			// Nothing cancels the run: the deadline is this caller's, the run is the
			// supervisor's, and a Wake is not a Restart.
			return fmt.Errorf("retry %q: %w", contextName, err)
		}
		if answers(st.Connection, askedAt) {
			return nil
		}
	}
}

// answers reports whether a run the ask bought has finished. LastRunAt rather than LastAttempt,
// because a Skip finishes a run and records no attempt: the connection probe skips on an unread
// kubeconfig, and a retry that waited for a record there would fail a probe that had already run.
func answers(conn Observation[string], askedAt time.Time) bool {
	return !conn.LastRunAt.Before(askedAt)
}

// ListenForConnectionChanges reports every context whose connection changed, for a reader whose
// reaction to any of them is the same. A holder that cares about one claim watches that claim instead.
func (s *Service) ListenForConnectionChanges() ConnectionChangeNotifications {
	return s.connectionChangeHub.Receiver()
}

// Start runs the supervisor, the kubeconfig watch and the data-rate sampler. The kubeconfig
// subscription is taken before Start returns, so nothing it says in between is dropped; the
// supervisor's queues need no such care, since they hold what a claim taken before Start asked
// for until its workers run.
func (s *Service) Start(ctx context.Context) (func(context.Context) error, error) {
	// Refused rather than tolerated: a second Start puts a second watch and a second pair of
	// supervisor loops on the same wait groups, so its stop would wait on the first's.
	if !s.started.CompareAndSwap(false, true) {
		return nil, errors.New("kubeconn: already started")
	}

	stopEngine := s.supervisor.Start(ctx)

	// Not Start's context, which bounds startup: this one bounds the watch, so it lives until
	// the stop func cancels it.
	loopCtx, stopLoop := context.WithCancel(context.Background())

	cfgs := s.kubecfgSvc.WatchConfigState()
	s.wg.Go(func() {
		defer cfgs.Close()
		s.watchKubeconfig(loopCtx, cfgs)
	})

	ticks, stopTicks := s.dataRateTicker()
	s.wg.Go(func() {
		defer stopTicks()
		s.sampleEvery(loopCtx, ticks)
	})

	return func(ctx context.Context) error {
		stopLoop()
		return errors.Join(drain.WithContext(ctx, s.wg.Wait), stopEngine(ctx))
	}, nil
}

// Close drops what the pool holds, the supervisor's subjects included. Claims are not released for
// their holders: a claim outliving the pool is the holder's bug, and reading as nothing known is
// what a dropped entry already does.
func (s *Service) Close() error {
	s.mu.Lock()
	dropped := make([]*Connection, 0, len(s.claimed))
	for contextName, e := range s.claimed {
		if e.conn != nil {
			dropped = append(dropped, e.conn)
		}
		// The supervisor's copy too, and before it is closed: a pass whose commit landed while
		// the pass worker was already stopped left the connection it built there and nowhere
		// else. Retiring is idempotent, so the usual case retires the same one twice.
		if v, tracked := s.supervisor.Read(contextName); tracked {
			if conn := keyConnection.From(v).Value.conn; conn != nil {
				dropped = append(dropped, conn)
			}
		}
	}
	clear(s.claimed)
	clear(s.published)
	s.mu.Unlock()

	err := s.supervisor.Close()
	for _, conn := range dropped {
		conn.Retire()
	}
	return err
}

// watchKubeconfig wakes every claimed context's connection probe on every change, until stopped.
// Every context rather than the ones that moved, because the feed carries a whole config and
// working out which contexts changed is what the probe does anyway. Only the connection probe:
// resolving owns the context's lifecycle, and the four behind it hang off its answer.
//
// Cancellation ends the wait: the service behind the feed is the app's and is not required to
// close its channel when released.
func (s *Service) watchKubeconfig(ctx context.Context, cfgs kubeconfig.ConfigStateWatch) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-cfgs.Chan():
			if !ok {
				return
			}
			s.supervisor.WakeAll(nameConnection)
		}
	}
}

// publish is the supervisor's OnPass: project the pass into State, tell the claim watchers, and
// signal the fleet when the news moved. The supervisor serializes it per context, and the order
// holds — a reader the signal wakes finds the state already published.
func (s *Service) publish(contextName string, v supervisor.Snapshot) {
	st := s.stateOf(v)
	n := s.newsOf(v, st)

	conn := keyConnection.From(v).Value.conn
	stale, held, changed := s.record(contextName, conn, n)
	if stale != nil {
		// Outside the lock: retiring closes sockets, and nothing about it needs the pool.
		stale.Retire()
	}
	if !held {
		// The pass raced the last release; watchers on this name hear about whatever claims
		// it next.
		return
	}

	s.stateHub.Sender().Send(contextName, st)
	if changed {
		s.connectionChangeHub.Sender().Send(contextName, struct{}{})

		// A connection whose server was replaced is rebuilt by the connection probe, which
		// would otherwise wait out its interval — nothing about the file moved, so no watch
		// reports this. The probe cannot declare a data edge on the identity probes (the graph
		// is kept acyclic by resolving only already-registered names), and a Wake is not an
		// edge.
		//
		// **Inside `changed` on purpose.** A Wake is a queue add, not a schedule, so nothing
		// paces one re-sent every pass — and the conflict outlives the run meant to clear it
		// whenever that run returns before the rebuild arm, which a kubeconfig that stops
		// resolving does. Recording the conflict empties `news.vouchedFor`, so the edge lands
		// on exactly the pass that records it, and the probe's interval is the backstop.
		if conn != nil && conn.conflicted() {
			s.supervisor.Wake(contextName, nameConnection)
		}
	}
}

// record files what a pass concluded against the entry that must still be there to receive it,
// and hands back the connection nothing holds any more for the caller to retire.
//
// One critical section, because the entry check is what makes the rest of it true: a release
// landing between the two would leave this writing published for a context nobody holds — which
// both announces a claim that is gone and leaves a stale baseline, so the first pass of whatever
// claims the name next compares equal and tells the fleet nothing.
//
// held is not the same question as stale: what a release retired is the connection the *entry*
// held, so a pass landing after one carries a connection nothing else can reach — that one is
// the stale one.
func (s *Service) record(contextName string, conn *Connection, n news) (stale *Connection, held, changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e := s.claimed[contextName]
	if e == nil {
		return conn, false, false
	}
	if e.conn != conn {
		stale, e.conn = e.conn, conn
	}
	changed = s.published[contextName] != n
	s.published[contextName] = n
	return stale, true, changed
}

// news is the part of a pass a fleet reader reacts to: what the probes concluded, never when.
// The timing moves every run, so signalling on it would wake every cluster's reconcile on every
// cadence to find nothing changed.
type news struct {
	departed bool
	phase    Phase
	identity Identity
	// conn identifies the connection itself, so a REPLACEMENT is news whatever it goes on
	// to vouch for. A rotation for an unchanged cluster moves nothing else: same endpoint,
	// same phase, same uid.
	//
	// **It cannot be derived from vouchedFor.** The stamp is a mutable word on the
	// connection, read at publish time rather than committed by the pass, so a re-stamp
	// landing before this pass publishes leaves vouchedFor exactly as it was — and the
	// rotation goes unannounced, stranding whoever was refused inside the window
	// (a kubesync session suspends there, and only this signal re-runs it).
	conn uint64
	// vouchedFor is the cluster the CURRENT connection vouches for, empty while it vouches
	// for none. Distinct from identity, which is what the probes last read over whatever
	// connection was up at the time. It is what carries a conflict: the connection is the
	// same one, and what it will vouch for has changed.
	vouchedFor string
	ok         [len(probeNames)]bool
}

func (s *Service) newsOf(v supervisor.Snapshot, st State) news {
	ci := keyConnection.From(v).Value
	n := news{
		departed: ci.departed,
		phase:    st.Phase(),
		identity: st.Identity(),
	}
	if ci.conn != nil {
		n.conn = ci.conn.Seq()
		n.vouchedFor, _ = ci.conn.ServerUID()
	}
	for i, name := range probeNames {
		n.ok[i] = v.Attempts(name).OK()
	}
	return n
}

// stateOf projects the supervisor's observables into State. The connection's observable bundles the
// context's standing with the endpoint; only the endpoint is the answer State carries.
func (s *Service) stateOf(v supervisor.Snapshot) State {
	ci := keyConnection.From(v)
	return State{
		Connection:    Observation[string]{Value: ci.Value.endpoint, LastSeenAt: ci.LastSeenAt, Attempts: ci.Attempts},
		Readiness:     supervisor.GetJobObservation[ComponentStatus](v, nameReadiness),
		ServerUID:     supervisor.GetJobObservation[string](v, nameServerUID),
		ServerVersion: supervisor.GetJobObservation[VersionInfo](v, nameServerVersion),
		Principal:     supervisor.GetJobObservation[Principal](v, namePrincipal),
	}
}

// claim is one holder's claim on the context it named, and on the entry it was given. It carries
// both because a context outlives its entries: what a claim may read and release is the one it
// was made for, never whatever has the name now.
type claim struct {
	svc         *Service
	contextName string
	entry       *entry

	released atomic.Bool
}

// Conn never dials: the connection is whatever the last run of the connection probe built.
// A holder that wants to wait for one reads WatchState, as it does for everything else — a
// connection whose last probe failed is still handed out, since only the holder can
// tell a revoked credential from a control plane mid-restart.
func (c *claim) Conn(context.Context) (*Connection, error) {
	v, ok := c.svc.read(c.contextName, c.entry)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNoConnection, c.contextName)
	}
	conn := keyConnection.From(v).Value.conn
	if conn == nil {
		return nil, fmt.Errorf("%w: %q", ErrNoConnection, c.contextName)
	}
	return conn, nil
}

func (c *claim) State() State {
	v, ok := c.svc.read(c.contextName, c.entry)
	if !ok {
		return State{}
	}
	return c.svc.stateOf(v)
}

// ConnFor asks the connection who it reached, so there is nothing to correlate: the probe
// that made the request is the one that stamped it. The connection is resolved first, so a
// cluster nothing has reached reports the outage rather than an identity it could not have
// read either way.
func (c *claim) ConnFor(ctx context.Context, serverUID string) (*Connection, error) {
	conn, err := c.Conn(ctx)
	if err != nil {
		return nil, err
	}

	if err := conn.IdentityFor(serverUID); err != nil {
		return nil, fmt.Errorf("context %q: %w", c.contextName, err)
	}
	return conn, nil
}

func (c *claim) Departed() bool {
	v, ok := c.svc.read(c.contextName, c.entry)
	if !ok {
		return true
	}
	return keyConnection.From(v).Value.departed
}

// read answers a claim from the supervisor, for the entry the claim was made for. Once the pool no
// longer holds that entry — released, or dropped by Close — nothing is known, which a claim
// reads as departed with no connection.
//
// The identity check comes after the supervisor read: the last holder can release and another
// caller re-claim the name mid-read, and the state that came back is then the new claim's — the
// check catches it, where one taken before the read would not.
func (s *Service) read(contextName string, held *entry) (supervisor.Snapshot, bool) {
	v, tracked := s.supervisor.Read(contextName)

	s.mu.Lock()
	valid := s.claimed[contextName] == held
	s.mu.Unlock()

	return v, valid && tracked
}

// WatchState registers before it reads, so the first State and every later pass both reach
// the watch.
//
// Not tracked for the caller. The watch is the caller's to close, and closing it here on
// Release would take one out from under a caller that is still reading it.
func (c *claim) WatchState() *StateWatch {
	rx := c.svc.stateHub.Watch(c.contextName)
	return NewStateWatch(c.State(), rx)
}

// Release gives the claim back, dropping the entry — and the supervisor's subject with it — once
// the last holder goes: an entry nobody holds is one nothing probes. The CAS makes it
// idempotent while other holders remain, when the entry check below cannot tell a second
// release from a first.
func (c *claim) Release() {
	if !c.released.CompareAndSwap(false, true) {
		return
	}

	s := c.svc
	s.mu.Lock()
	// Only the entry the claim was made for: Close drops entries out from under the leases
	// still holding them, so a stale release must not decrement — or delete — whatever has
	// claimed the name since.
	if s.claimed[c.contextName] != c.entry {
		s.mu.Unlock()
		return
	}
	var dropped *Connection
	c.entry.holders--
	if c.entry.holders == 0 {
		dropped = c.entry.conn
		delete(s.claimed, c.contextName)
		delete(s.published, c.contextName)
		// Under the lock, so a release racing a fresh Acquire of the same name cannot remove
		// the subject the new claim just added.
		s.supervisor.Remove(c.contextName)
	}
	s.mu.Unlock()

	// An entry that goes takes its connection with it: nothing probes the context now, so
	// nothing else would ever close these sockets.
	if dropped != nil {
		dropped.Retire()
	}
}
