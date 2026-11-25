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

// Package kubesync fills the caches: it discovers what a cluster serves, mirrors every
// served kind into that cluster's kubestore file, and stands behind an answer about each
// one.
//
// It knows nothing about records. It speaks cache ids, kube-contexts, server UIDs and
// GVRs, and cluster translates — the layering rule every leaf under it follows. A
// record type reaching this package is an import cycle, which is the enforcement.
//
// Two levels of arming, and they AND rather than nest. TrackDiscovery/ForgetDiscovery say
// whether a cache syncs at all — they also supply the cache, since the connection claim
// and the store claim are taken there — and TrackKind/ForgetKind say which kinds. A
// registration outlives its cache being forgotten, which is what makes a pause one call
// and a resume one call, with no record written and none requeued.
//
// This file is the seam: arming, the gate between its two levels, the reads, the news
// feeds, and the lifecycle.
package kubesync

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/amorey/gobus/conflate"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/drain"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/supervisor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubeconn"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
)

// Params is what one cache syncs: over which context, and as which server. A context is
// not an identity — it can be re-pointed at another cluster — so ServerUID is what makes
// a discovered kind and a mirrored object belong to this cache.
type Params struct {
	ContextName string
	ServerUID   string
}

// connService and storeManager are the two leaves this one stands on, named for the
// types they stand for and narrow enough that a test hands over a fake cluster.
type (
	connService interface {
		Acquire(contextName string) kubeconn.Lease
	}
	storeManager interface {
		OpenOrCreate(cacheID int64) (*kubestore.Store, error)
	}
)

// KindKey names one kind in one cache — the whole key of a kind's news, and everything a
// caller needs to address the record standing for it. It embeds the same value the
// methods take, so a caller composes one from what it already holds (note key.Kind.Kind
// is the singular; key.Kind is the embedded value).
//
// Carrying the singular is safe HERE, where a lookup would not tolerate it: a rename
// splits one key into two, and both translate to the same record name, so the cost is a
// duplicate wake and never a missed one.
type KindKey struct {
	CacheID int64
	kubestore.Kind
}

// DiscoveryChangeNotifications is keyed by cache id, the whole address of the record it wakes.
type DiscoveryChangeNotifications = *conflate.Receiver[int64, struct{}]

// KindChangeNotifications is keyed by KindKey, which is the whole address of one kind's record.
type KindChangeNotifications = *conflate.Receiver[KindKey, struct{}]

// kindID is what a kind is looked up by: the pair the server guarantees unique per
// group-version. The singular is data, not identity — a Kind renamed under an unchanged
// plural is the same collection, and keying on it too would read one kind as two.
type kindID struct{ apiVersion, resource string }

func idOf(k kubestore.Kind) kindID { return kindID{k.APIVersion, k.Resource} }

// option is the test seam: the exported constructor takes production knobs only, and the
// kind worker is substituted from white-box tests. The sweep has none — both run on a
// supervisor over whatever the connection reaches, so a test hands it an api server.
type option func(*Service)

// withKindSync substitutes the worker every kind subject runs, for a test about arming rather
// than about what a kind sync reads. It is handed the Service because a substitute takes the
// same admission the real one does.
func withKindSync(build func(*Service) supervisor.Worker[Reason]) option {
	return func(s *Service) { s.buildKindSync = build }
}

// withPacing shrinks what a kind sync runs on, so no test outwaits a production number.
func withPacing(p pacing) option { return func(s *Service) { s.pacing = p } }

// Service is the seam. One session per tracked cache holds that cache's claims and the runs
// and streams under it; everything a caller reads is answered out of what those committed.
type Service struct {
	connSvc  connService
	storeMgr storeManager

	// Two supervisors, because their subjects are different things — and two kinds of thing
	// run on them. discoverySupervisor runs the three JOBS of discovery.go over one subject
	// per armed cache; kindSupervisor runs one WORKER per kind, whose run is that kind's
	// stream from the cold list to the last delta.
	discoverySupervisor *supervisor.Supervisor
	kindSupervisor      *supervisor.Supervisor
	buildKindSync       func(*Service) supervisor.Worker[Reason]
	pacing              pacing

	// One news feed per level, because their consumers are two beehive triggers and a
	// trigger wakes a record for every value its feed carries — one feed carrying both
	// would wake a cache for each of its hundreds of kinds. Each coalesces per key, so a
	// fleet syncing at once neither loses a cache behind a busier one nor overflows a
	// buffer. News is not a status: the key is the whole message, and the reader answers
	// it by re-reading.
	discoveryHub *conflate.Hub[int64, struct{}]
	kindHub      *conflate.Hub[KindKey, struct{}]

	// ctx bounds every run and stream in the process; the stop func cancels it. Built here
	// rather than in Start so a pass that arms a cache before the lifecycle has run is armed
	// rather than dropped.
	ctx    context.Context
	cancel context.CancelFunc

	// Two locks, one rule each. armMu serializes arming and forgetting, which wait for runs to
	// stop. mu guards everything a run and a reader share — the maps below plus each session's
	// runs and stamps — and, because a run writes through it, is NEVER held while waiting for
	// one.
	armMu sync.Mutex

	mu sync.Mutex
	// sessions holds the armed caches; tracked holds the registered kinds, which outlive
	// their cache being forgotten.
	sessions map[int64]*session
	tracked  map[int64]map[kindID]kubestore.Kind
	// storeFailures holds exactly the caches whose most recent arm could not open a store,
	// by the driver's message. It lives outside the session because a failed arm leaves
	// none, and it is mu's rather than armMu's: the reader below takes mu already, and so
	// does every writer.
	storeFailures map[int64]string
	started       bool
	stopped       bool
}

// New returns a Service over the connection pool and the cache directory. Nothing is
// armed until a caller tracks it.
func New(connSvc connService, storeMgr storeManager, opts ...option) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{
		connSvc:             connSvc,
		storeMgr:            storeMgr,
		discoverySupervisor: supervisor.New(),
		discoveryHub:        conflate.New[int64, struct{}](),
		kindHub:             conflate.New[KindKey, struct{}](),
		ctx:                 ctx,
		cancel:              cancel,
		sessions:            map[int64]*session{},
		tracked:             map[int64]map[kindID]kubestore.Kind{},
		storeFailures:       map[int64]string{},
	}
	s.pacing = defaultPacing()
	for _, opt := range opts {
		opt(s)
	}
	s.kindSupervisor = supervisor.New(supervisor.WithStartConcurrency(s.pacing.kindStartConcurrency))
	if s.buildKindSync == nil {
		s.buildKindSync = func(s *Service) supervisor.Worker[Reason] {
			return kindSync{s: s, pacing: s.pacing}
		}
	}
	registerProbes(s.discoverySupervisor, s)
	s.discoverySupervisor.OnPass(s.publishDiscovery)
	registerKindSync(s.kindSupervisor, s)
	s.kindSupervisor.OnPass(s.publishKind)
	return s
}

// registerKindSync wires the kind worker. It states only the ladder: a worker's start timeout
// defaults to none, which a cold list of a large kind needs, and its restart floor comes from the
// ladder's base — the exit is the re-entry, so there is no cadence to state.
func registerKindSync(e *supervisor.Supervisor, s *Service) {
	supervisor.RegisterWorker(e, nameKindSync, s.buildKindSync(s),
		supervisor.WithBackoff(s.pacing.backoff.Base, s.pacing.backoff.Factor, s.pacing.backoff.Cap))
}

// publishDiscovery projects what a pass left and records it, which is what wakes the cache
// when its verdict moved. Every pass, because the supervisor cannot tell a new answer from the
// same one re-confirmed — commitDiscovery is where that is decided.
func (s *Service) publishDiscovery(subject string, snap supervisor.Snapshot) {
	cacheID, ok := parseDiscoverySubject(subject)
	if !ok {
		return
	}
	sess := s.sessionOf(cacheID)
	if sess == nil {
		return
	}
	partial, message := sess.lastSweep()
	s.commitDiscovery(sess, discoveryStateOf(snap, partial, message))
}

// TrackDiscovery arms cacheID's sweep, or updates it in place when the params move.
//
// **It is also what supplies the cache.** Params is what every run dials over and the lease
// and store claim are taken here, so a kind tracked against a cache with no discovery has
// nothing to run on: this pair says whether a cache syncs at all, and TrackKind says which
// kinds.
//
// Arming is the caller's policy, never interest: nothing a reader does re-arms a cache
// the user paused.
func (s *Service) TrackDiscovery(cacheID int64, p Params) {
	s.armMu.Lock()
	defer s.armMu.Unlock()

	if sess := s.sessionOf(cacheID); sess != nil {
		if sess.params == p {
			return
		}
		// The identity or the credentials moved, so nothing the standing session holds is
		// the right claim any more.
		s.tearDown(cacheID)
	}
	s.arm(cacheID, p)
}

// ForgetDiscovery stops everything under cacheID and returns only once nothing can still
// write through that cache's store, which is what a teardown needs. The kinds stay registered:
// re-arming starts every one of them again, with no record written and none requeued.
func (s *Service) ForgetDiscovery(cacheID int64) {
	s.armMu.Lock()
	defer s.armMu.Unlock()
	s.tearDown(cacheID)
}

// ForgetCache is ForgetDiscovery plus the registrations, for a cache that is gone rather than
// paused: nothing under it is held for a resume that can never come.
//
// The teardown comes first and the drop second, because what it stops is read off the same map:
// emptied ahead of the join, this would take the sweep down and leave every kind's worker running
// against a store whose claims were just given back.
//
// A kind pass already in flight can call TrackKind after this returns and register that kind
// again. What removes it is the record's own teardown, which withdraws the kind whether or not
// the cache record is still there to read.
func (s *Service) ForgetCache(cacheID int64) {
	s.armMu.Lock()
	defer s.armMu.Unlock()

	s.tearDown(cacheID)
	s.mu.Lock()
	delete(s.tracked, cacheID)
	s.mu.Unlock()
}

// TrackKind registers that one kind should be synced into cacheID, or updates its shape in
// place. A kind registered against a cache that is not armed is held rather than
// refused, so the record's pass and the cache's may land in either order.
func (s *Service) TrackKind(cacheID int64, k kubestore.Kind) {
	s.armMu.Lock()
	defer s.armMu.Unlock()

	id := idOf(k)
	s.mu.Lock()
	kinds, ok := s.tracked[cacheID]
	if !ok {
		kinds = map[kindID]kubestore.Kind{}
		s.tracked[cacheID] = kinds
	}
	prev, held := kinds[id]
	kinds[id] = k
	sess := s.sessions[cacheID]
	s.mu.Unlock()

	if sess == nil || (held && prev == k) {
		return
	}
	// A rename under an unchanged plural is the same collection, but the stream and the run
	// in flight were both started with the whole value — the rows are keyed by the singular —
	// so this kind comes down and goes up again. Stopped before it is armed, so the generation
	// that keys rows by the old singular is gone before the one that keys them by the new.
	s.stopKind(sess, id)
	s.kindSupervisor.Add(kindSubject(cacheID, k))
}

// ForgetKind withdraws a kind's registration and returns only once nothing can still write
// through it: the stream is joined and the run in flight is cancelled and joined.
func (s *Service) ForgetKind(cacheID int64, k kubestore.Kind) {
	// Held across the join, like every other arming path. Releasing it first would let a
	// TrackKind for this kind land while the withdrawn run was still unwinding: the kind would
	// read as tracked again, so commitKind would stop refusing that run's late writes, and its
	// replacement would be dispatched alongside it.
	s.armMu.Lock()
	defer s.armMu.Unlock()

	id := idOf(k)
	s.mu.Lock()
	if kinds, ok := s.tracked[cacheID]; ok {
		delete(kinds, id)
		if len(kinds) == 0 {
			delete(s.tracked, cacheID)
		}
	}
	sess := s.sessions[cacheID]
	s.mu.Unlock()

	s.stopKind(sess, id)
}

// stopKind takes one kind down and waits for it: Remove cancels the worker and joins it, and what
// the session held for the kind is dropped after. Past here nothing can write through this kind,
// which is what makes forgetting synchronous and a rename safe to re-arm behind.
//
// The cancel is what bounds the join: what remains of a run is a page request unwinding, not
// the cold list it was in the middle of.
func (s *Service) stopKind(sess *session, id kindID) {
	if sess == nil {
		return
	}
	s.kindSupervisor.Remove(kindSubject(sess.cacheID, kubestore.Kind{
		APIVersion: id.apiVersion, Resource: id.resource,
	}))
	// **After the join, and that is what makes it final.** A rename leaves the kind tracked
	// under its new singular, so nothing refuses what the old generation stamps on its way
	// down — dropped any earlier, those stamps would come back and be served for the
	// generation that replaced it, before it had listed a row.
	sess.dropKind(id)
}

// publishKind wakes one kind's record when the answer a reader would get has MOVED. OnPass fires
// on every pass, schedule-only ones included, so the baseline is the session's: without it every
// pass would wake every record.
func (s *Service) publishKind(subject string, snap supervisor.Snapshot) {
	cacheID, id, ok := parseKindSubject(subject)
	if !ok { // coverage:ignore every subject on the kind supervisor is made by kindSubject
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if key, moved := s.recordKindReason(cacheID, id, snap); moved {
		_ = s.kindHub.Sender().Send(key, struct{}{})
	}
}

// recordKindReason files what this pass would tell a reader and reports whether it is news. A
// pass for a kind nobody tracks, or one under a cache nobody has armed, is neither. The caller
// holds s.mu and sends the wake before releasing it, so a move anyone can read has always been
// sent: a wake sent after the lock could reach a subscriber that read the move first. Sending
// under the lock is safe because conflate's Send never blocks and runs no caller code.
func (s *Service) recordKindReason(cacheID int64, id kindID, snap supervisor.Snapshot) (KindKey, bool) {
	sess, armed := s.sessions[cacheID]
	k, tracked := s.tracked[cacheID][id]
	if !armed || !tracked {
		return KindKey{}, false
	}
	state, ok := kindStateOf(snap, sess.kindStamps[id])
	if !ok {
		// A kind armed but not yet answered stands behind no answer, and the seam promises
		// the getter says so — so there is nothing to wake a record for.
		return KindKey{}, false
	}
	if prev, had := sess.published[id]; had && prev == Reason(state.Reason) {
		return KindKey{}, false
	}
	sess.published[id] = Reason(state.Reason)
	return KindKey{CacheID: cacheID, Kind: k}, true
}

// RunWithCacheSyncStopped calls fn once, on the caller's goroutine, after the sweep and every
// tracked kind under cacheID have stopped AND EXITED — a signalled stop is not enough, since the
// point is that nothing is mid-write — and arms them again on the way out. A cache nobody has
// armed runs fn directly: there is nothing to stop, and its file is still there to clear, which
// is why the store work stays with the caller.
//
// This is what a clear needs and cannot arrange from outside: Manager.Clear swaps the file under
// whoever holds it open, and a relist page or a SyncKinds landing across that swap writes into a
// file nothing holds. The session keeps its claims throughout, which is what the manager reopens
// a fresh file for.
//
// **fn runs under armMu, so it must not call back into this Service.**
func (s *Service) RunWithCacheSyncStopped(cacheID int64, fn func() error) error {
	s.armMu.Lock()
	defer s.armMu.Unlock()

	sess := s.sessionOf(cacheID)
	if sess == nil {
		return fn()
	}

	// The sweep too: kind_catalog is written through the same file.
	s.discoverySupervisor.Remove(sess.discoverySubject())
	kinds := sess.trackedKinds()
	for _, k := range kinds {
		s.stopKind(sess, idOf(k))
	}
	// Deferred, so a clear that failed leaves a cache that still syncs.
	defer func() {
		for _, k := range kinds {
			s.kindSupervisor.Add(kindSubject(cacheID, k))
		}
		// The catalog went with the file, so the sweep writes it again now rather than at its
		// next cadence tick. Nothing is skipped by the fingerprint: the table has none.
		s.discoverySupervisor.Add(sess.discoverySubject())
	}()
	return fn()
}

// RunWithKindSyncStopped is RunWithCacheSyncStopped narrowed to one kind — same join before fn, for
// the clear that drops one kind's rows and its cookie. The kind comes back only if it is still
// tracked: fn cannot call in to withdraw it, but a ForgetKind may have landed before this did.
func (s *Service) RunWithKindSyncStopped(cacheID int64, k kubestore.Kind, fn func() error) error {
	s.armMu.Lock()
	defer s.armMu.Unlock()

	sess := s.sessionOf(cacheID)
	if sess == nil {
		return fn()
	}

	s.stopKind(sess, idOf(k))
	defer func() {
		if s.isKindTracked(cacheID, idOf(k)) {
			s.kindSupervisor.Add(kindSubject(cacheID, k))
		}
	}()
	return fn()
}

func (s *Service) isKindTracked(cacheID int64, id kindID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, tracked := s.tracked[cacheID][id]
	return tracked
}

// RestartAll restarts every armed kind in place, off its cookie — what a resume poke needs,
// since a watch that died under a sleeping machine reports nothing.
func (s *Service) RestartAll() {
	for _, sess := range s.sessionsSnapshot() {
		sess.restart()
	}
}

// GetDiscoveryState is cacheID's standing sweep answer. It reports false for what has not
// answered yet — a cache nobody has armed, or one whose sweep has committed nothing — and
// a caller that folds that into "serves no kinds" deletes a record set that was only
// waiting.
func (s *Service) GetDiscoveryState(cacheID int64) (DiscoveryState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[cacheID]
	if !ok {
		// The one verdict that lives outside a session, because a store that would not open
		// is why there is none. Belt-and-braces against the invariant above it: a session
		// exists only where the last arm succeeded, so a stale entry cannot be read here.
		if msg, failed := s.storeFailures[cacheID]; failed {
			return DiscoveryState{Reason: ReasonStoreFailed, Message: msg}, true
		}
		return DiscoveryState{}, false
	}
	if !sess.hasDiscoveryState {
		return DiscoveryState{}, false
	}
	return sess.discoveryState, true
}

// GetKindState is one kind's standing answer, false until something has answered. **Assembled at
// read**, from the three that own its parts: the verdict the worker committed, the pacing the
// supervisor knows, and the stamps the session keeps. Nothing stores it, because a stored copy
// would be a stale duplicate of all three.
func (s *Service) GetKindState(cacheID int64, k kubestore.Kind) (KindState, bool) {
	snap, ok := s.kindSupervisor.Read(kindSubject(cacheID, k))
	if !ok {
		return KindState{}, false
	}

	s.mu.Lock()
	sess, armed := s.sessions[cacheID]
	var stamps kindStamps
	if armed {
		stamps = sess.kindStamps[idOf(k)]
	}
	s.mu.Unlock()

	if !armed {
		return KindState{}, false
	}
	return kindStateOf(snap, stamps)
}

// kindStateOf is that assembly. The verdict is the worker's except where an attempt outranks it:
// a suspension is why nothing is syncing, and a failure is what went wrong. A clean exit outranks
// nothing — the rows stay current across a rotation, and saying otherwise would flicker every
// kind through SyncFailed every few minutes.
func kindStateOf(snap supervisor.Snapshot, stamps kindStamps) (KindState, bool) {
	o := supervisor.GetWorkerObservation[Reason](snap, nameKindSync)
	// **A run in flight speaks for itself**, two ways: it is up — which is what Live means,
	// and what a re-established stream has to say for itself, since re-reporting the Watching
	// it already stood behind commits nothing — or it has committed something since the last
	// attempt ended, which is a relist announcing Resyncing before it has a frame.
	//
	// Without this a kind cold-listing after a 410 would read SyncFailed while it relisted, and
	// one whose connection came back would read IdentityMismatch for as long as it streamed. A
	// run that has said nothing yet and is not up is still described by that exit, which is
	// what keeps a suspension standing across the gate check that re-enters it.
	spoken := o.Live() || (o.InFlight() && o.ChangedAt.After(o.LastAttempt.FinishedAt))
	outranks := !spoken && (o.LastAttempt.Verdict == supervisor.VerdictSuspended ||
		o.LastAttempt.Verdict == supervisor.VerdictFailed)
	if !o.Known() && !outranks {
		return KindState{}, false
	}

	state := KindState{
		Reason: string(o.Value),
		Live:   o.Live(),
		// The worker commits exactly when its reason moves, so when the supervisor last saw
		// its value change IS when the reason last moved.
		SinceAt:      o.ChangedAt,
		LastUpdateAt: stamps.LastUpdateAt,
		LastLiveAt:   stamps.LastLiveAt,
		Restarts:     o.Restarts,
	}
	if outranks && o.LastAttempt.Verdict == supervisor.VerdictFailed {
		// Gated on the same rule, because the seam promises this is zero while a stream is up:
		// only a run that is DOWN is retrying, and a run that has spoken is not down. Left
		// ungated it would serve the countdown of the failure the live stream recovered from.
		state.NextRetryAt = o.NextAttempt.ScheduledAt
	}
	if outranks {
		reason := o.LastAttempt.Reason
		state.SinceAt = o.LastAttempt.FinishedAt
		if o.LastAttempt.Verdict == supervisor.VerdictFailed {
			// Every way a stream fails reads the same to a consumer — NeverReady and the
			// rest are the supervisor's vocabulary, and the message is what says which.
			reason = ReasonSyncFailed
			state.Failure = kubeconn.ClassifyRequest(o.LastAttempt.Err)
			// The streak's start rather than the last retry in it: "failing since 10:02" is
			// what a reader wants, where "failing since a second ago" is not.
			state.SinceAt = o.FailingSince
		}
		state.Reason, state.Message = string(reason), o.LastAttempt.Message
	}
	return state, true
}

// ListenForDiscoveryChanges carries the caches whose sweep has something new to say. Close it
// when done.
func (s *Service) ListenForDiscoveryChanges() DiscoveryChangeNotifications {
	return s.discoveryHub.Receiver()
}

// ListenForKindChanges carries the kinds with something new to say. Close it when done.
func (s *Service) ListenForKindChanges() KindChangeNotifications { return s.kindHub.Receiver() }

// Start runs the two supervisors, and returns the func that cancels every run and drains
// everything. No cache is armed here: one is armed when a pass tracks it, which may be before
// or after this.
func (s *Service) Start(ctx context.Context) (func(context.Context) error, error) {
	// Refused rather than tolerated: a second Start puts a second pair of supervisor loops on
	// the same wait group, so the first stop would drain loops that only the second can end.
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil, errors.New("kubesync: already started")
	}
	s.started = true
	s.mu.Unlock()

	stopDiscovery := s.discoverySupervisor.Start(ctx)
	stopKinds := s.kindSupervisor.Start(ctx)

	return func(ctx context.Context) error {
		// armMu for the whole shutdown, like every other path that waits on runs: it is what
		// makes closing the door and draining one step. An arm in flight has published its
		// session but not yet launched its goroutines, so a drain that overlapped it would
		// join a session that does not hold them yet — and release claims it has not taken.
		s.armMu.Lock()
		defer s.armMu.Unlock()

		s.mu.Lock()
		s.stopped = true
		s.mu.Unlock()

		s.cancel()
		// The supervisors first, and not through s.cancel: a run in flight holds the store claim
		// Close is about to give back, and their loops are bounded by their own contexts.
		if err := errors.Join(stopKinds(ctx), stopDiscovery(ctx)); err != nil {
			return err
		}
		return drain.WithContext(ctx, s.wait)
	}, nil
}

// wait joins every run and stream still up. Each session's own wait is what makes it safe to
// release the claims below.
func (s *Service) wait() {
	for _, sess := range s.sessionsSnapshot() {
		sess.wait()
	}
}

// sessionsSnapshot copies the armed sessions out from under mu, for a caller about to act on
// them outside it.
func (s *Service) sessionsSnapshot() []*session {
	s.mu.Lock()
	defer s.mu.Unlock()
	sessions := make([]*session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	return sessions
}

// Close releases the claims and the news hubs. Call after the stop func returns: a claim
// given back while a run could still write through it is a write into a file nothing holds
// open.
func (s *Service) Close() error {
	s.armMu.Lock()
	defer s.armMu.Unlock()

	s.mu.Lock()
	sessions := s.sessions
	s.sessions = map[int64]*session{}
	s.stopped = true
	s.mu.Unlock()

	s.cancel()
	for _, sess := range sessions {
		sess.close()
	}
	s.discoveryHub.Close()
	s.kindHub.Close()
	if err := s.kindSupervisor.Close(); err != nil { // coverage:ignore Supervisor.Close never fails
		return err
	}
	return s.discoverySupervisor.Close()
}

// arm builds the session for one cache and starts its sweep plus a subject per registered
// kind. Called under armMu.
//
// A session that will not start arms nothing and is retried by the next pass, which calls
// this again: no session means no answer, and "no answer" is what a caller must not fold
// into "serves no kinds" anyway. It is logged because nothing else would report it.
func (s *Service) arm(cacheID int64, p Params) {
	sess := newSession(s, cacheID, p)

	// The kinds and the session under one hold, so the replay below covers exactly the
	// registrations this session did not see arrive.
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	kinds := make([]kubestore.Kind, 0, len(s.tracked[cacheID]))
	for _, k := range s.tracked[cacheID] {
		kinds = append(kinds, k)
	}
	// Cleared on the way in, so the map holds exactly the caches whose most recent arm
	// failed: a retry reaches here without passing through tearDown, since a failed arm
	// leaves no session for the next TrackDiscovery to tear down.
	delete(s.storeFailures, cacheID)
	s.sessions[cacheID] = sess
	s.mu.Unlock()

	// Published before it holds anything, so a start that fails takes the entry back out —
	// a session in the map is what a run the supervisor schedules during start resolves through.
	// The context is the one thing a failed start leaves behind: nothing else was claimed,
	// but the child stays on s.ctx until it is cancelled, and this cache is armed again on
	// every pass.
	if err := sess.start(); err != nil {
		slog.Error("kubesync: arm cache", "cacheID", cacheID, "err", err)
		sess.cancel()
		s.mu.Lock()
		delete(s.sessions, cacheID)
		s.storeFailures[cacheID] = capStoreFailure(safe.Safe(err))
		s.mu.Unlock()
		return
	}

	for _, k := range kinds {
		s.kindSupervisor.Add(kindSubject(cacheID, k))
	}
}

// maxStoreFailureMessage bounds a driver error, which is the one discovery message this
// package does not write itself. The value is cluster's MaxMessageLen written again:
// that package imports this one, so the constant cannot be shared without a leaf to hold it.
const maxStoreFailureMessage = 200

// capStoreFailure bounds a message at birth, as safe.Safe renders it there. Both paths out
// of here — the state watch and the event run cluster writes from it — carry the message
// unchanged, so doing either at a boundary would fix one and leave the other.
func capStoreFailure(msg string) string {
	if len(msg) <= maxStoreFailureMessage {
		return msg
	}
	return msg[:maxStoreFailureMessage] + "…"
}

// tearDown stops cacheID's session and gives its claims back. Called under armMu, and
// never under mu: stopping waits for bodies that commit through it.
func (s *Service) tearDown(cacheID int64) {
	s.mu.Lock()
	sess, ok := s.sessions[cacheID]
	delete(s.sessions, cacheID)
	// Above the guard below: a cache whose arm failed has no session, which is precisely
	// the only state that ever has an entry here.
	delete(s.storeFailures, cacheID)
	s.mu.Unlock()
	if !ok {
		return
	}
	sess.close()
}

// sessionOf is the armed session for a cache, or nil.
func (s *Service) sessionOf(cacheID int64) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[cacheID]
}

// commitDiscovery records a sweep's answer and wakes the cache when its reason moved. A
// run that outlived its session commits nothing: the claims it wrote through are gone.
func (s *Service) commitDiscovery(sess *session, state DiscoveryState) {
	s.mu.Lock()
	if s.sessions[sess.cacheID] != sess {
		s.mu.Unlock()
		return
	}
	moved := !sess.hasDiscoveryState || sess.discoveryState.Reason != state.Reason
	sess.discoveryState, sess.hasDiscoveryState = state, true
	s.mu.Unlock()

	if moved {
		_ = s.discoveryHub.Sender().Send(sess.cacheID, struct{}{})
	}
}
