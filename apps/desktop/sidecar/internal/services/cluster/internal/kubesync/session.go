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

// One armed cache: its connection claim, its store claim, the identity gate, and the set
// of runs under it.
package kubesync

import (
	"context"
	"sync"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubeconn"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
)

// session is one tracked cache. Its two claims are the session's rather than each run's: a
// tracked cache has a file the moment it is armed, which is what Manager.ListenForCacheOpenChanges readers
// are waiting on, and one lease is what everything under it dials over. It takes them in start
// and gives them back in close — both halves here, so neither can outlive the other.
type session struct {
	s       *Service
	cacheID int64
	params  Params

	// Held from start until close.
	lease kubeconn.Lease
	store *kubestore.Store

	// ctx bounds every run and every stream under this session; cancel ends them all.
	ctx    context.Context
	cancel context.CancelFunc

	// wakeLoops holds the session's two wake loops, which carry wakes rather than running a
	// body. runs holds everything that can still write through the claims — the sweep's
	// passes and each kind worker's whole life.
	wakeLoops sync.WaitGroup
	runs      sync.WaitGroup

	// Everything below is guarded by Service.mu, like the session map itself — one lock
	// covers a session, what is running under it, and their answers, so no read finds one
	// without the others.
	//
	// stopping closes runs to new registrations, so nothing joins the group after the wait
	// on it has begun.
	stopping          bool
	discoveryState    DiscoveryState
	hasDiscoveryState bool
	// kindStamps is the half of a kind's answer the supervisor does not hold: the two liveness
	// stamps, which move on every frame. The verdict is the worker's committed value, and
	// GetKindState joins the two.
	kindStamps map[kindID]kindStamps
	// published is the last reason each kind's record was told about. OnPass fires on every
	// pass, schedule-only ones included, so without a baseline here every pass would wake
	// every record.
	published map[kindID]Reason
	// relistNeeded holds the kinds whose cookie names a position the server has dropped. It
	// outlives the run that learned it: the 410 arrives on a run that establishes nothing, and
	// an intent that did not survive a later failure would send the next start back to the
	// same dead cookie. Cleared only when a cold list has landed.
	relistNeeded map[kindID]bool
	// waiting holds every run under this cache that is at the connection gate or parked behind
	// it, against the gate reason it recorded — empty until it has answered. Set before a run
	// asks the lease, cleared only when the lease says yes. It is the session's fact rather
	// than the supervisor's: the snapshot files a suspension only after the run returns, and a
	// connection frame read in that gap would see nothing parked and let the wake go missing.
	//
	// The reason is each run's own. Two runs at one gate can be parked under different ones —
	// a kind that has not been asked since the pool moved still names the older cause — so the
	// bridge judges each against what the pool now says rather than taking the sweep's verdict
	// as evidence about registrations that were never asked.
	waiting map[gateKey]Reason
	// sweepPartial and sweepMessage are the one thing a probe Result cannot carry: a run
	// that succeeded without answering for every group-version. Succeeded takes no reason,
	// and both neighbours are wrong — Fail would climb the backoff ladder over an aggregated
	// API that is down, and a plain success would read as Discovered.
	sweepPartial bool
	sweepMessage string
}

func newSession(s *Service, cacheID int64, p Params) *session {
	ctx, cancel := context.WithCancel(s.ctx)
	return &session{
		s:            s,
		cacheID:      cacheID,
		params:       p,
		ctx:          ctx,
		cancel:       cancel,
		kindStamps:   map[kindID]kindStamps{},
		published:    map[kindID]Reason{},
		relistNeeded: map[kindID]bool{},
		waiting:      map[gateKey]Reason{},
	}
}

// gateKey names one run at the connection gate. The registration is part of it because the three
// discovery probes run concurrently under one subject: a key they shared would let the one that
// passed the gate clear the mark of one that was refused.
type gateKey struct {
	kind kindID // zero for a discovery probe
	name string
}

func kindGateKey(id kindID) gateKey    { return gateKey{kind: id, name: nameKindSync} }
func probeGateKey(name string) gateKey { return gateKey{name: name} }

// setWaiting marks a run as at the gate with nothing recorded yet, waitingOn records what the
// gate refused it with, and clearWaiting takes the mark back.
func (sess *session) setWaiting(key gateKey) {
	sess.s.mu.Lock()
	defer sess.s.mu.Unlock()
	sess.waiting[key] = ""
}

func (sess *session) waitingOn(key gateKey, reason Reason) {
	sess.s.mu.Lock()
	defer sess.s.mu.Unlock()
	sess.waiting[key] = reason
}

func (sess *session) clearWaiting(key gateKey) {
	sess.s.mu.Lock()
	defer sess.s.mu.Unlock()
	delete(sess.waiting, key)
}

// anyWaiting reports whether this cache has anything a connection would bring back.
func (sess *session) anyWaiting() bool {
	sess.s.mu.Lock()
	defer sess.s.mu.Unlock()
	return len(sess.waiting) > 0
}

// waitingOtherThan reports a run at the gate whose recorded reason is not this one — a run that
// has not answered yet included, since what it is about to record cannot be known.
func (sess *session) waitingOtherThan(reason Reason) bool {
	sess.s.mu.Lock()
	defer sess.s.mu.Unlock()
	for _, recorded := range sess.waiting {
		if recorded != reason {
			return true
		}
	}
	return false
}

// connFor is the connection gate every run passes: the lease's answer for this cache's server
// UID, with the run marked as waiting until that answer is yes. Marking before the question is
// what leaves no window — a refused run is already marked by the time it decides to park — and
// failed is the verdict this caller parks under, so the mark names the same cause the run does.
//
// A caller that turns a refused gate into something other than a park clears the mark itself.
func (sess *session) connFor(ctx context.Context, key gateKey, failed Reason) (*kubeconn.Connection, error) {
	sess.setWaiting(key)
	conn, err := sess.lease.ConnFor(ctx, sess.params.ServerUID)
	if err != nil {
		sess.waitingOn(key, connectionReason(err, failed))
		return nil, err
	}
	sess.clearWaiting(key)
	return conn, nil
}

// discoverySubject is this cache's name on the discovery supervisor.
func (sess *session) discoverySubject() string { return discoverySubject(sess.cacheID) }

// start takes the two claims, then arms the sweep and the two things that wake it. The
// supervisor owns the sweep's cadence and its backoff ladder, so there is no loop here — only
// the wakes it cannot derive.
//
// A store that will not open arms nothing, and the caller retries on its next pass: there
// is nothing to unwind, because the lease is taken only once the file is open.
//
// Both subscriptions are taken before the goroutines that read them, never inside: a value
// published in the gap reaches nobody, and the sweep suspends rather than polling.
func (sess *session) start() error {
	store, err := sess.s.storeMgr.OpenOrCreate(sess.cacheID)
	if err != nil {
		return err
	}
	sess.store = store
	sess.lease = sess.s.connSvc.Acquire(sess.params.ContextName)

	// Every move in this lease's connection: one appearing or going away, and the identity
	// it answers as.
	connStateSub := sess.lease.WatchState()
	sess.wakeLoops.Go(func() { sess.wakeOnConnectionChange(sess.ctx, connStateSub) })

	// Writes to this cache's own CustomResourceDefinition and APIService rows — the synced
	// objects, not the kind_catalog the sweep itself writes, so a sweep cannot wake itself.
	//
	// Promptness alone, so a store that will not subscribe costs a wake rather than the
	// sweep: a cache is armed below whether or not this attaches.
	catalogChangeSub, err := sess.store.ListenForChanges(
		kubestore.ObjectsKey(crdGVR.GroupVersion().String(), crdGVR.Resource),
		kubestore.ObjectsKey(apiServiceAPIVersion, apiServiceResource))
	if err == nil {
		sess.wakeLoops.Go(func() { sess.wakeDiscoverySweepOnCatalogChange(sess.ctx, catalogChangeSub) })
	}

	sess.s.discoverySupervisor.Add(sess.discoverySubject())
	return nil
}

// wakeOnConnectionChange is the session's one connection bridge, and it carries both directions
// neither the sweep nor a kind run can see for itself. A connection that arrived brings back
// whatever suspended for the want of one, which cannot wait for it because a run in flight holds
// a supervisor worker. A connection that stopped dialing makes a settled verdict wrong, and a
// settled sweep is scheduled rather than parked — so without this it would read Discovered until
// its interval came round.
//
// The pool's answer is ONE fact for every kind under the cache, so the guard below is applied
// once per session rather than per kind: a suspended cache of three hundred kinds is not polled
// per frame.
//
// Level-triggered against the facts rather than edge-triggered on the connection: WatchState
// keeps only the latest value per key, so a reader that falls behind skips the frame where
// the connection went away, and the edge that matters with it.
func (sess *session) wakeOnConnectionChange(ctx context.Context, connStateSub *kubeconn.StateWatch) {
	defer connStateSub.Close()
	for {
		if _, err := connStateSub.RecvContext(ctx); err != nil {
			return
		}
		switch _, err := sess.lease.ConnFor(ctx, sess.params.ServerUID); {
		case err != nil:
			// Only what the answers do not already say. This feed publishes every pass, not
			// only the ones that changed something, so waking on each frame would make a
			// suspended cache poll — which is the one thing suspending is for.
			//
			// Two questions, because a settled sweep and a parked run are wrong in different
			// ways: the sweep's verdict outlives the connection it was drawn over, and a run
			// at the gate names a cause the pool may have replaced since it was asked.
			if sess.discoveryReason() != connectionReason(err, ReasonDiscoveryFailed) ||
				sess.waitingOtherThan(connectionReason(err, ReasonSyncFailed)) {
				sess.wakeAll()
			}
		case sess.anyWaiting():
			sess.wakeAll()
		}
	}
}

// wakeAll brings back everything under this cache: the sweep, and every kind, each of which
// suspends at the same gate for the same reason. A Wake never tears a live worker down, which is
// what makes calling this on every state frame cheap: parked kinds start, streaming ones are
// left alone.
func (sess *session) wakeAll() {
	sess.s.discoverySupervisor.Wake(sess.discoverySubject(), discoveryProbes...)
	for _, k := range sess.trackedKinds() {
		sess.s.kindSupervisor.Wake(kindSubject(sess.cacheID, k), nameKindSync)
	}
}

// discoveryReason is the verdict this cache stands behind, empty until a run commits one.
func (sess *session) discoveryReason() Reason {
	sess.s.mu.Lock()
	defer sess.s.mu.Unlock()
	if !sess.hasDiscoveryState {
		return ""
	}
	return Reason(sess.discoveryState.Reason)
}

// wakeDiscoverySweepOnCatalogChange rides the cache's own CustomResourceDefinition and
// APIService rows. Those two kinds are what change a catalog and the cache already syncs
// both, so a private watch here would be a second watch on the same collections over the
// same connection.
//
// The whole sweep, not the fan-out alone: a CRD for a group the cluster did not serve adds
// that group to /apis, so the list the fan-out reads has moved too — waking only the fan-out
// leaves the new kind unseen until the group list's own interval comes round.
//
// The loop it forms — discovery arms the kinds whose writes wake discovery — bottoms out
// on the interval, which is also the cold start. It can only ever be a wake: an api server
// upgrade changes the built-in kinds with no CRD or APIService write at all.
func (sess *session) wakeDiscoverySweepOnCatalogChange(ctx context.Context, catalogChangeSub kubestore.ChangeNotifications) {
	defer catalogChangeSub.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-catalogChangeSub.Chan():
			if !ok {
				return
			}
			sess.s.discoverySupervisor.Wake(sess.discoverySubject(), discoveryProbes...)
		}
	}
}

// announce wakes the cache with its verdict unmoved — the one publication a reason cannot
// carry. Two sweeps can both settle on Discovered with a CRD appearing between them, so a
// reason-only feed would leave the new kind unsynced until something unrelated moved.
func (sess *session) announce() {
	_ = sess.s.discoveryHub.Sender().Send(sess.cacheID, struct{}{})
}

// recordSweep and lastSweep carry what a Result cannot: whether the last fan-out answered
// for every group-version.
func (sess *session) recordSweep(partial bool, message string) {
	sess.s.mu.Lock()
	defer sess.s.mu.Unlock()
	sess.sweepPartial, sess.sweepMessage = partial, message
}

func (sess *session) lastSweep() (bool, string) {
	sess.s.mu.Lock()
	defer sess.s.mu.Unlock()
	return sess.sweepPartial, sess.sweepMessage
}

// trackedKinds is what this session should be syncing: the kinds registered against its
// cache, snapshotted so the caller acts outside s.mu. The supervisor keeps no subject listing
// and needs none — this map is already exactly the set that was Added.
func (sess *session) trackedKinds() []kubestore.Kind {
	sess.s.mu.Lock()
	defer sess.s.mu.Unlock()
	kinds := make([]kubestore.Kind, 0, len(sess.s.tracked[sess.cacheID]))
	for _, k := range sess.s.tracked[sess.cacheID] {
		kinds = append(kinds, k)
	}
	return kinds
}

// kindStamps is what moves on every frame, and so what stays out of the supervisor: a worker
// commits its verdict, which is what a reader reacts to, and stamps these beside it.
type kindStamps struct {
	// LastUpdateAt is when data last arrived; LastLiveAt the last proof the stream is live,
	// which is the later of the two and the only one that distinguishes idle from wedged.
	LastUpdateAt time.Time
	LastLiveAt   time.Time
}

// stampUpdate records data arriving. **It moves both stamps**, because a delta is proof the
// stream is current as well as data — so the delta path, which is every object a busy cluster
// serves, takes this lock once rather than twice. The lock is the Service's, shared with every
// other cache and every reader.
func (sess *session) stampUpdate(id kindID) {
	now := time.Now()

	sess.s.mu.Lock()
	defer sess.s.mu.Unlock()
	sess.kindStamps[id] = kindStamps{LastUpdateAt: now, LastLiveAt: now}
}

// stampLive records proof the stream was current at at, with no data behind it — a bookmark, or
// a quiet watch that stood a whole window.
func (sess *session) stampLive(id kindID, at time.Time) {
	sess.s.mu.Lock()
	defer sess.s.mu.Unlock()
	st := sess.kindStamps[id]
	st.LastLiveAt = at
	sess.kindStamps[id] = st
}

// dropKind forgets everything the session holds for a kind. **Called after the worker has been
// removed, which joins it** — not behind the tracked guard, which a rename does not provide: the
// kind stays tracked under its new singular, so nothing refuses what the old generation stamps
// on its way down.
func (sess *session) dropKind(id kindID) {
	sess.s.mu.Lock()
	defer sess.s.mu.Unlock()
	delete(sess.kindStamps, id)
	delete(sess.published, id)
	delete(sess.relistNeeded, id)
	delete(sess.waiting, kindGateKey(id))
}

// markRelist records that this kind's cookie names a position the server has dropped, and
// needsRelist reads it back. clearRelist is called only once a cold list has committed — the
// intent has to outlast every failure between learning it and acting on it, or a relist refused
// once would resume from the cookie it was replacing.
func (sess *session) markRelist(id kindID) {
	sess.s.mu.Lock()
	defer sess.s.mu.Unlock()
	sess.relistNeeded[id] = true
}

func (sess *session) needsRelist(id kindID) bool {
	sess.s.mu.Lock()
	defer sess.s.mu.Unlock()
	return sess.relistNeeded[id]
}

func (sess *session) clearRelist(id kindID) {
	sess.s.mu.Lock()
	defer sess.s.mu.Unlock()
	delete(sess.relistNeeded, id)
}

// restart re-enters every kind off its cookie and re-runs the sweep. The sweep is a Wake — a run
// in flight has nothing stale in it, and a wake is redelivered to one that is out. A kind is a
// Restart: whatever it is streaming, it is streaming over a connection a sleeping machine may
// already have killed, and a stop records nothing, so the reopen costs no rung and holds its
// reason.
//
// Neither waits. Three hundred cancels are one poke; three hundred joins of unwinding cold lists
// on the poke consumer's own goroutine are not.
func (sess *session) restart() {
	sess.s.discoverySupervisor.Wake(sess.discoverySubject(), discoveryProbes...)
	for _, k := range sess.trackedKinds() {
		sess.s.kindSupervisor.Restart(kindSubject(sess.cacheID, k), nameKindSync)
	}
}

// close ends every kind and the sweep, waits for them, and gives the two claims back — one
// call, because a claim released while a body could still write through it is a write into a
// file nothing holds open, and the join is the only thing standing between them.
//
// Dropping the subject stops the sweep being SCHEDULED, and a run in flight against it then
// commits nothing — but it neither cancels that run nor joins it, so the cancel and the wait
// below are what the release rests on.
func (sess *session) close() {
	sess.s.discoverySupervisor.Remove(sess.discoverySubject())
	// Each kind's Remove cancels its worker and waits for it, so every stream is down before
	// the cancel below reaches the sweep.
	for _, k := range sess.trackedKinds() {
		sess.s.kindSupervisor.Remove(kindSubject(sess.cacheID, k))
	}
	sess.cancel()
	sess.wait()

	sess.store.Release()
	sess.lease.Release()
}

// wait joins the wake loops and every run in flight. It closes the session to new runs first, so
// nothing registers after the join has begun.
func (sess *session) wait() {
	sess.s.mu.Lock()
	sess.stopping = true
	sess.s.mu.Unlock()

	sess.wakeLoops.Wait()
	sess.runs.Wait()
}

// enterRun registers a run against its cache, so a teardown waits for it, and hands back the
// session it runs against. False for a cache nobody has armed and for one already stopping —
// both runs that must not write.
func (s *Service) enterRun(cacheID int64) (*session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enterRunLocked(cacheID)
}

func (s *Service) enterRunLocked(cacheID int64) (*session, bool) {
	sess, ok := s.sessions[cacheID]
	if !ok || sess.stopping {
		return nil, false
	}
	sess.runs.Add(1)
	return sess, true
}

func (sess *session) leaveRun() { sess.runs.Done() }

// enterKindRun admits one kind's worker: it checks the cache and reads the kind under one lock,
// and counts the run against the session so a teardown waits for it.
//
// The whole kubestore.Kind comes back, singular included: the subject names a kind by the pair
// the server guarantees unique, and the rows are keyed by a name no body can learn from a
// collection that lists empty.
func (s *Service) enterKindRun(cacheID int64, id kindID) (*session, kubestore.Kind, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	k, ok := s.tracked[cacheID][id]
	if !ok {
		return nil, kubestore.Kind{}, false
	}
	sess, ok := s.enterRunLocked(cacheID)
	if !ok {
		return nil, kubestore.Kind{}, false
	}
	return sess, k, true
}
