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

// Fixtures shared across this package's test files. Anything used by one file lives
// beside it instead.
package cluster

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amorey/beehive"
	beehivesqlite "github.com/amorey/beehive/sqlite"
	"github.com/amorey/gobus/conflate"
	gobuswatch "github.com/amorey/gobus/watch"
	"github.com/amorey/gochan/watch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd/api"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/lifecycle"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubeconn"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubesync"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/kubeconfig"
)

// quietWindow bounds a negative assertion — "no frame arrived" — which has no event to
// wait for and so needs a window of its own rather than the failsafe every positive wait
// uses.
const quietWindow = 200 * time.Millisecond

// newTestBeehive returns a beehive over an in-memory store, closed on cleanup. The
// bootstrap New does, minus the disk. opts let a test shrink a cadence it would
// otherwise have to outwait.
func newTestBeehive(t *testing.T, opts ...beehive.Option) *beehive.Beehive {
	t.Helper()
	store, err := beehivesqlite.OpenMemory()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, store.Close()) })

	bh, err := beehive.New(store, opts...)
	require.NoError(t, err)
	return bh
}

// newTestDeps returns the shared set over a test beehive — the same newDeps the
// composition root calls, so a test never assembles its own. The controllers are
// registered so a requeue reaches a real reconciler, but beehive is never run, so
// nothing reconciles or collects behind these tests.
//
// One store for every kind, which the owner edges need: beehive refuses an owner in
// another store.
func newTestDeps(t *testing.T) deps {
	t.Helper()
	d, _ := newTestDepsAndBeehive(t)
	return d
}

// fakeKubeconn stands in for the pool: it answers per context from a map and records who
// was asked for. The zero value refuses nothing and knows nothing, which is a fleet whose
// first probe is still owed — what a cluster pass finds unless a test says otherwise.
type fakeKubeconn struct {
	states map[string]kubeconn.State
	// hub is the fleet feed, keyed by context the way the pool keys it. Built on demand so
	// the zero value stays usable.
	once  sync.Once
	hub   *conflate.Hub[string, struct{}]
	asked []string

	// stateOnce/stateHub are the per-claim feed, built on demand for the same reason.
	stateOnce sync.Once
	stateHub  *gobuswatch.Hub[string, kubeconn.State]

	released []string
	retried  []string
	// retryCtx is the context the last retry was given, and retryErr what it answers with:
	// the wait is the probe's round trip now, so both cross this seam.
	retryCtx context.Context
	retryErr error
	// connErr is what every claim's Conn answers, for a fleet nothing reached.
	connErr error
	// conn, when set, is what every claim's Conn hands back. Atomic, since a test replaces it
	// while a state watch reads it.
	conn atomic.Pointer[kubeconn.Connection]
	// releases, when set, fires each release's context, so a test can wait for one.
	releases *testutil.Probe[string]

	// rateReads, when set, fires each read of a claim's ring, so a test can tell where a
	// data-rate stream has got to.
	rateReads *testutil.Probe[string]
	// rates is every claim's data-rate ring, keyed by context and guarded by ratesMu, and
	// dataRateChangeHub its nudges, built on demand.
	ratesMu           sync.Mutex
	rates             map[string][]kubeconn.DataRateSample
	rateOnce          sync.Once
	dataRateChangeHub *conflate.Hub[string, struct{}]
}

// addRate is a tick landing on contextName's ring: the sample, then the nudge.
func (f *fakeKubeconn) addRate(contextName string, s kubeconn.DataRateSample) {
	f.ratesMu.Lock()
	if f.rates == nil {
		f.rates = map[string][]kubeconn.DataRateSample{}
	}
	f.rates[contextName] = append(f.rates[contextName], s)
	f.ratesMu.Unlock()
	f.nudge(contextName)
}

// nudge tells contextName's readers to look, with or without a new sample.
func (f *fakeKubeconn) nudge(contextName string) {
	f.rateFeed().Sender().Send(contextName, struct{}{})
}

func (f *fakeKubeconn) rateFeed() *conflate.Hub[string, struct{}] {
	f.rateOnce.Do(func() { f.dataRateChangeHub = conflate.New[string, struct{}]() })
	return f.dataRateChangeHub
}

func (f *fakeKubeconn) Acquire(contextName string) kubeconn.Lease {
	f.asked = append(f.asked, contextName)
	return &fakeLease{svc: f, contextName: contextName, state: f.states[contextName], connErr: f.connErr}
}

func (f *fakeKubeconn) RetryAndWait(ctx context.Context, contextName string) error {
	f.retried = append(f.retried, contextName)
	f.retryCtx = ctx
	return f.retryErr
}

// ListenForConnectionChanges is the fleet's change notifications the trigger reads. publish is the probe landing on it.
func (f *fakeKubeconn) ListenForConnectionChanges() kubeconn.ConnectionChangeNotifications {
	return f.moved().Receiver()
}

func (f *fakeKubeconn) publish(contextName string) {
	f.moved().Sender().Send(contextName, struct{}{})
}

func (f *fakeKubeconn) moved() *conflate.Hub[string, struct{}] {
	f.once.Do(func() { f.hub = conflate.New[string, struct{}]() })
	return f.hub
}

func (f *fakeKubeconn) stateFeed() *gobuswatch.Hub[string, kubeconn.State] {
	f.stateOnce.Do(func() { f.stateHub = gobuswatch.New[string, kubeconn.State]() })
	return f.stateHub
}

// publishState is a pass landing on one claim, the way the pool's OnPass would.
func (f *fakeKubeconn) publishState(contextName string, st kubeconn.State) {
	f.stateFeed().Sender().Send(contextName, st)
}

// fakeLease is one claim, holding what a probe would have found. It records its release so
// a test can pin the lifetime a pass is meant to keep.
type fakeLease struct {
	svc         *fakeKubeconn
	contextName string
	state       kubeconn.State
	connErr     error
	// departed is what the pool would report for a context the kubeconfig stopped naming.
	departed bool
}

// Conn hands back the pool's conn, or an empty connection, since the only pass that takes
// one reaches the server through a seam of its own. connErr is what a claim on a cluster
// nothing reached would answer.
func (l *fakeLease) Conn(context.Context) (*kubeconn.Connection, error) {
	if l.connErr != nil {
		return nil, l.connErr
	}
	if conn := l.svc.conn.Load(); conn != nil {
		return conn, nil
	}
	return &kubeconn.Connection{}, nil
}

// ConnFor refuses everything. Nothing in this package dials, and only kubeconn can stamp
// a connection's identity, so there is no honest way to answer here. Deciding from l.state would be the connection/State correlation the
// design forbids, passing a test against semantics the pool refuses.
func (l *fakeLease) ConnFor(context.Context, string) (*kubeconn.Connection, error) {
	return nil, kubeconn.ErrIdentityMismatch
}

func (l *fakeLease) State() kubeconn.State { return l.state }

func (l *fakeLease) Departed() bool { return l.departed }

func (l *fakeLease) DataRates() []kubeconn.DataRateSample {
	l.svc.ratesMu.Lock()
	rates := slices.Clone(l.svc.rates[l.contextName])
	l.svc.ratesMu.Unlock()
	if l.svc.rateReads != nil {
		l.svc.rateReads.Fire(l.contextName)
	}
	return rates
}

func (l *fakeLease) ListenForDataRateChanges() kubeconn.ConnectionChangeNotifications {
	feed := l.svc.rateFeed()
	return feed.Receiver(feed.WithKey(l.contextName))
}

// WatchState is the schedule watch's feed; a cluster pass reads State instead.
func (l *fakeLease) WatchState() *kubeconn.StateWatch {
	return kubeconn.NewStateWatch(l.State(), l.svc.stateFeed().Watch(l.contextName))
}

func (l *fakeLease) Release() {
	l.svc.released = append(l.svc.released, l.contextName)
	if l.svc.releases != nil {
		l.svc.releases.Fire(l.contextName)
	}
}

// fakeKubestore stands in for the store registry: it records what was cleared and
// which caches were deleted, and answers with err. The zero value clears everything
// without complaint.
type fakeKubestore struct {
	// mgr is a real manager over a temp dir, since OpenExisting hands back a concrete
	// store nothing can stand in for.
	mgr *kubestore.Manager

	opened []int64
	// noFile models a cache whose file is not there — a teardown that already ran.
	noFile        bool
	clearedCaches []int64
	removed       []int64
	err           error
	// onClear and onOpen run inside the clears, so a test can assert what had already
	// happened by then — or move the world under one. afterOpen runs with the claim
	// already handed out, which is where a test retires the cache under a live one.
	onClear   func(cacheID int64)
	onOpen    func(cacheID int64)
	afterOpen func(cacheID int64)

	// mu guards the measurement, which a test moves while the state watch reads it.
	mu    sync.Mutex
	stats kubestore.Stats
	// onStats runs inside Stats, so a test can tell when a pass has measured — which is
	// what says the pass ran at all, since a pass that changes nothing writes nothing.
	onStats func(cacheID int64)
	// onRemove runs inside Remove, so a test can assert what had already happened by
	// the time the file went.
	onRemove func(cacheID int64)
	// sizeLimitOnce/sizeLimitHub are the size-limit feed, built on demand so a fake declared
	// as a zero-value literal still hands out a receiver a publish reaches.
	sizeLimitOnce sync.Once
	sizeLimitHub  *conflate.Hub[int64, struct{}]
}

// OpenExisting records the claim and hands back a real store, standing in for a cache
// whose file exists. What the caller then does to a kind is kubestore's own business,
// and its own tests'.
func (f *fakeKubestore) OpenExisting(cacheID int64) (*kubestore.Store, bool, error) {
	if f.onOpen != nil {
		f.onOpen(cacheID)
	}
	// Under the lock: two reconciles can claim a cache at once — a cache-wide read while
	// a kind's own pass clears its rows.
	f.mu.Lock()
	f.opened = append(f.opened, cacheID)
	f.mu.Unlock()
	if f.err != nil {
		return nil, false, f.err
	}
	if f.noFile {
		return nil, false, nil
	}
	store, err := f.mgr.OpenOrCreate(cacheID)
	if err != nil {
		return nil, false, err
	}
	if f.afterOpen != nil {
		f.afterOpen(cacheID)
	}
	return store, true, nil
}

// newFakeKubestore builds the fake over a real manager, closed with the test.
func newFakeKubestore(t *testing.T) *fakeKubestore {
	t.Helper()
	mgr := kubestore.NewManager(t.TempDir(), kubestore.Retention{})
	t.Cleanup(func() { assert.NoError(t, mgr.Close()) })
	return &fakeKubestore{mgr: mgr}
}

// kubestoreFake is the store registry a fixture wired, for a test that drives it.
func kubestoreFake(d deps) *fakeKubestore { return d.kubestoreMgr.(*fakeKubestore) }

func (f *fakeKubestore) Clear(cacheID int64) error {
	if f.onClear != nil {
		f.onClear(cacheID)
	}
	f.clearedCaches = append(f.clearedCaches, cacheID)
	if f.err != nil {
		return f.err
	}
	// Through the real manager, so a clear a test drives empties the rows a later read
	// goes looking for — which is the whole of what a re-read reacts to.
	return f.mgr.Clear(cacheID)
}

// Stats is what the state watch measures; setStats moves it under a live subscription, which
// is what the state watch re-emits on.
func (f *fakeKubestore) Stats(_ context.Context, cacheID int64) (kubestore.Stats, error) {
	if f.onStats != nil {
		f.onStats(cacheID)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stats, f.err
}

// ListenForSizeLimitChanges is the size-limit feed the trigger reads; publishSizeLimitChange is one
// cache's verdict changing, as the janitor would report it.
func (f *fakeKubestore) ListenForSizeLimitChanges() kubestore.SizeLimitChangeNotifications {
	return f.sizeLimits().Receiver()
}

func (f *fakeKubestore) publishSizeLimitChange(cacheID int64) {
	_ = f.sizeLimits().Sender().Send(cacheID, struct{}{})
}

func (f *fakeKubestore) sizeLimits() *conflate.Hub[int64, struct{}] {
	f.sizeLimitOnce.Do(func() { f.sizeLimitHub = conflate.New[int64, struct{}]() })
	return f.sizeLimitHub
}

func (f *fakeKubestore) setStats(s kubestore.Stats) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stats = s
}

// cacheIsOpen asks whether anything holds cacheID's file open — what
// Manager.ListenForChanges' ok reports. The receiver is closed straight away: this is a question, not a watch.
func cacheIsOpen(m *kubestore.Manager, cacheID int64) bool {
	sub, ok := m.ListenForChanges(cacheID)
	if ok {
		sub.Close()
	}
	return ok
}

// ListenForChanges and ListenForCacheOpenChanges go to the real manager, so a reader sees
// whatever a test opened through it — and nothing when a test opened nothing, which is the
// state watch's idle-cache case.
func (f *fakeKubestore) ListenForChanges(cacheID int64, keys ...string) (kubestore.ChangeNotifications, bool) {
	return f.mgr.ListenForChanges(cacheID, keys...)
}

func (f *fakeKubestore) ListenForCacheOpenChanges(cacheID int64) kubestore.CacheOpenChangeNotifications {
	return f.mgr.ListenForCacheOpenChanges(cacheID)
}

func (f *fakeKubestore) Remove(cacheID int64) error {
	if f.onRemove != nil {
		f.onRemove(cacheID)
	}
	f.removed = append(f.removed, cacheID)
	return f.err
}

// noCacheFiles owns nothing, for a test whose passes run with no log store.
type noCacheFiles struct{}

func (noCacheFiles) Clear(int64) error  { return nil }
func (noCacheFiles) Remove(int64) error { return nil }

// fakeCacheFiles stands in for the log store as the cache's other file owner: it records
// which caches were cleared and removed, and answers err to both.
type fakeCacheFiles struct {
	cleared, removed []int64
	err              error
	// onClear and onRemove run inside the calls, so a test can see what had already
	// happened by then.
	onClear, onRemove func(cacheID int64)
}

func (f *fakeCacheFiles) Clear(cacheID int64) error {
	if f.onClear != nil {
		f.onClear(cacheID)
	}
	f.cleared = append(f.cleared, cacheID)
	return f.err
}

func (f *fakeCacheFiles) Remove(cacheID int64) error {
	if f.onRemove != nil {
		f.onRemove(cacheID)
	}
	f.removed = append(f.removed, cacheID)
	return f.err
}

// probedAt is when every fake probe landed. Fixed, so a test asserting the stamp names a
// value rather than reading the clock the code under test would.
var probedAt = time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)

// answering is a pool whose "prod" claim reached the server and read id, or failed with
// err. The shape most tests want; knowing builds anything else.
func answering(id kubeconn.Identity, err error) *fakeKubeconn {
	if err != nil {
		return knowing(kubeconn.State{Connection: failed(err)})
	}
	// A part id leaves empty is left unanswered, which is what a probe that could not read
	// it reports.
	st := kubeconn.State{
		Connection: answeredWith("https://prod.example:6443"),
		Readiness:  answeredWith(kubeconn.ComponentStatus{}),
	}
	if id.ServerUID != "" {
		st.ServerUID = answeredWith(id.ServerUID)
	}
	if id.ServerVersion != "" {
		st.ServerVersion = answeredWith(kubeconn.VersionInfo{GitVersion: id.ServerVersion})
	}
	if id.Username != "" {
		st.Principal = answeredWith(kubeconn.Principal{Username: id.Username})
	}
	return knowing(st)
}

// answeredWith is a check that answered v at probedAt.
func answeredWith[T any](v T) kubeconn.Observation[T] {
	return kubeconn.Observation[T]{
		Value:      v,
		LastSeenAt: probedAt,
		Attempts: kubeconn.Attempts{
			LastAttempt: finished(kubeconn.ReasonSucceeded, ""),
		},
	}
}

// failed is a check whose last attempt did not answer.
func failed(err error) kubeconn.Observation[string] {
	return kubeconn.Observation[string]{
		Attempts: kubeconn.Attempts{
			LastAttempt: finished(kubeconn.ReasonUnreachable, err.Error()),
			Failures:    1, FailingSince: probedAt,
		},
	}
}

// finished is an attempt that ran and ended at probedAt, which is what makes its reason
// readable. The verdict follows the reason, the way a real probe's result would set it.
func finished(reason kubeconn.Reason, msg string) kubeconn.Attempt {
	verdict := kubeconn.VerdictFailed
	if reason == kubeconn.ReasonSucceeded {
		verdict = kubeconn.VerdictSucceeded
	}
	a := kubeconn.Attempt{
		ScheduledAt: probedAt, StartedAt: probedAt, FinishedAt: probedAt,
		Verdict: verdict, Reason: reason, Message: msg,
	}
	if msg != "" {
		a.Err = errors.New(msg)
	}
	return a
}

// knowing is a pool whose "prod" claim holds exactly this state.
func knowing(state kubeconn.State) *fakeKubeconn {
	return &fakeKubeconn{states: map[string]kubeconn.State{"prod": state}}
}

// newTestDepsAndBeehive is newTestDeps plus the beehive behind it, for a test that has
// to write what only a pass can — see newClusterStatusDeps.
func newTestDepsAndBeehive(t *testing.T) (deps, *beehive.Beehive) {
	t.Helper()
	bh := newTestBeehive(t)
	d := newTestDepsOver(t, bh)

	_, err := registerControllers(bh, d)
	require.NoError(t, err)

	// The anchors the real service creates at startup: clusterController.Reconcile
	// reads one to declare its dependency edge.
	require.NoError(t, ensureClusterSources(context.Background(), d.sourceClient))
	return d, bh
}

// testPaths is the service's files in a temp directory.
func testPaths(t *testing.T) Paths {
	t.Helper()
	dir := t.TempDir()
	return Paths{BeehiveDBFile: filepath.Join(dir, "beehive.db"), KubestoreDir: filepath.Join(dir, "kube")}
}

// newTestDepsOver is newDeps over bh, a fresh app.db and the test fakes.
func newTestDepsOver(t *testing.T, bh *beehive.Beehive) deps {
	t.Helper()
	db, store := openTestStore(t)
	d := newDeps(bh, db, store, newTestKubeconfig(t), &fakeKubeconn{}, newFakeKubestore(t), newFakeKubesync(), nil)
	d.cacheFiles.set(noCacheFiles{})
	return d
}

// finalizerClearingCacheController stands in for the cache controller where a cache is
// created but no pass is under test: beehive accepts a finalizer only for a kind with
// a controller registered in-process, and over a running beehive this one clears it at
// once, so a deleted cache is collected without the file removal the real one does.
type finalizerClearingCacheController struct{ lifecycle.None }

func (finalizerClearingCacheController) Reconcile(ctx context.Context, client beehive.ControllerClient[ClusterCacheStatus], obj *beehive.Object[ClusterCacheSpec, ClusterCacheStatus]) beehive.Result {
	if obj.DeletionRequestedAt != nil {
		if err := client.DeleteFinalizer(ctx, cacheFinalizer); err != nil {
			return beehive.Fail(err)
		}
	}
	return beehive.Settled()
}

// settlingClusterController stands in for the cluster controller where a runtime
// object has to be collected and no pass is under test: it settles every object, so
// beehive collects a deleting one at once.
type settlingClusterController struct{ lifecycle.None }

func (settlingClusterController) Reconcile(context.Context, beehive.ControllerClient[ClusterStatus], *beehive.Object[ClusterRuntimeSpec, ClusterStatus]) beehive.Result {
	return beehive.Settled()
}

// newRunningStandInDeps is the shared set over a running beehive with both stand-ins
// registered, for the mirror tests: a runtime object and its caches are collected
// the moment they are asked to go.
func newRunningStandInDeps(t *testing.T) deps {
	t.Helper()
	bh := newTestBeehive(t)
	registerCacheStandIn(t, bh)
	require.NoError(t, beehive.Register(bh, ClusterGroupKind, settlingClusterController{}))
	stop, err := bh.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, stop(context.Background())) })
	return newTestDepsOver(t, bh)
}

// registerCacheStandIn registers the stand-in above for the cache kind.
func registerCacheStandIn(t *testing.T, bh *beehive.Beehive) {
	t.Helper()
	require.NoError(t, beehive.Register(bh, ClusterCacheGroupKind, finalizerClearingCacheController{}))
}

// newClusterStatusDeps returns the shared set plus the client that stores a Cluster
// status from outside a pass, standing in for the connection probe: a fixture needs a
// stored status because the reconciles under test re-read it. Beehive is never run, so
// nothing reconciles behind these tests — which is also the only state an admin write
// is safe in.
func newClusterStatusDeps(t *testing.T) (deps, *beehive.AdminClient[ClusterStatus]) {
	t.Helper()
	bh := newTestBeehive(t)
	registerCacheStandIn(t, bh)
	return newTestDepsOver(t, bh), beehive.NewAdminClient[ClusterStatus](bh, ClusterGroupKind)
}

// newTestKubeconfig returns a started kubeconfig service over an empty temp dir, so
// it has read and every context is absent. Started because a reconcile defers until
// the first read, which Start does synchronously.
func newTestKubeconfig(t *testing.T) *kubeconfig.Service {
	t.Helper()
	svc := kubeconfig.New(filepath.Join(t.TempDir(), "config"), nil)

	stop, err := svc.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, stop(context.Background()))
		assert.NoError(t, svc.Close())
	})
	return svc
}

// newRunningDeps is the shared set over a running beehive, for the watch tests:
// without one the tail is dead and no change is ever reported. Nothing reconciles, so
// every frame is the test's own doing.
func newRunningDeps(t *testing.T, opts ...beehive.Option) deps {
	t.Helper()
	d, _ := newRunningDepsAndBeehive(t, opts...)
	return d
}

// newRunningDepsAndBeehive is newRunningDeps beside the beehive, for a test that
// writes a status from outside a pass.
func newRunningDepsAndBeehive(t *testing.T, opts ...beehive.Option) (deps, *beehive.Beehive) {
	t.Helper()
	bh := newTestBeehive(t, opts...)
	registerCacheStandIn(t, bh)
	stop, err := bh.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, stop(context.Background())) })
	return newTestDepsOver(t, bh), bh
}

// newRunningRegisteredDeps is the shared set over a beehive that is both registered
// and started, which is the only state an event watch works in: beehive refuses
// WatchEvents for a kind with no controller, and a stopped beehive collects nothing,
// so a watch never learns its record went. The two halves above supply one each.
//
// The cost is what newRunningDeps avoids on purpose: the reconcilers are live and
// write their own runs into the timelines under test. Scope an assertion to a category
// no controller writes.
func newRunningRegisteredDeps(t *testing.T, opts ...beehive.Option) (deps, *beehive.Beehive) {
	t.Helper()
	bh := newTestBeehive(t, opts...)
	d := newTestDepsOver(t, bh)

	_, err := registerControllers(bh, d)
	require.NoError(t, err)
	require.NoError(t, ensureClusterSources(context.Background(), d.sourceClient))

	stop, err := bh.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, stop(context.Background())) })
	return d, bh
}

// fakeKubeconfigSource is a hub the test publishes into, standing in for the
// watcher: same current-on-subscribe contract, driven by hand.
type fakeKubeconfigSource struct{ hub *watch.Hub[*api.Config] }

func newFakeKubeconfigSource(initial *api.Config) *fakeKubeconfigSource {
	return &fakeKubeconfigSource{hub: watch.New(initial)}
}

func (f *fakeKubeconfigSource) WatchConfigState() kubeconfig.ConfigStateWatch {
	return f.hub.Receiver()
}

// publish pushes a new snapshot to every subscriber.
func (f *fakeKubeconfigSource) publish(cfg *api.Config) { f.hub.Sender().Send(cfg) }

// close ends every subscription, standing in for the watcher shutting down.
func (f *fakeKubeconfigSource) close() { f.hub.Close() }

// cfgWith builds a snapshot holding one context per name, each naming its own cluster
// and user entry.
func cfgWith(names ...string) *api.Config {
	cfg := &api.Config{Contexts: map[string]*api.Context{}, Clusters: map[string]*api.Cluster{}}
	for _, n := range names {
		cfg.Contexts[n] = &api.Context{Cluster: n + "-cluster", AuthInfo: n + "-user"}
		cfg.Clusters[n+"-cluster"] = &api.Cluster{Server: "https://" + n + ".example:6443"}
	}
	return cfg
}

// cfgCurrent is cfgWith with one of the contexts marked current.
func cfgCurrent(current string, names ...string) *api.Config {
	cfg := cfgWith(names...)
	cfg.CurrentContext = current
	return cfg
}

// forbidden is a check the server answered and refused, which is a grant to fix rather
// than an outage to wait out.
func forbidden(msg string) kubeconn.Observation[string] {
	return kubeconn.Observation[string]{
		Attempts: kubeconn.Attempts{
			LastAttempt: finished(kubeconn.ReasonForbidden, msg),
			Failures:    1, FailingSince: probedAt,
		},
	}
}

// fakeKubesync stands in for the sync seam: it records what a pass armed and answers the two
// getters from maps a test writes. The zero value has armed nothing and knows nothing, which is
// a process whose first pass is still owed.
type fakeKubesync struct {
	mu sync.Mutex
	// discovery is the params each armed cache syncs under; kinds what each has registered.
	discovery map[int64]kubesync.Params
	kinds     map[int64]map[kubesync.KindKey]bool
	// discoveryStates/kindStates are what the getters answer. Absent reads false, which is
	// what a cache nothing has answered for reports.
	discoveryStates map[int64]kubesync.DiscoveryState
	kindStates      map[kubesync.KindKey]kubesync.KindState

	// stoppedCache/stoppedKind record what a clear ran inside.
	stoppedCache []int64
	stoppedKind  []kubesync.KindKey
	// holding is set while RunWithCacheSyncStopped runs its fn.
	holding bool

	discoveryHub *conflate.Hub[int64, struct{}]
	kindHub      *conflate.Hub[kubesync.KindKey, struct{}]
	restarted    *testutil.Signal

	// onDiscoveryRead runs inside GetDiscoveryState before it reads, so a test can tell
	// when a reader asked, and move the state it is about to read.
	onDiscoveryRead func(cacheID int64)
}

func newFakeKubesync() *fakeKubesync {
	return &fakeKubesync{
		discovery:       map[int64]kubesync.Params{},
		kinds:           map[int64]map[kubesync.KindKey]bool{},
		discoveryStates: map[int64]kubesync.DiscoveryState{},
		kindStates:      map[kubesync.KindKey]kubesync.KindState{},
		discoveryHub:    conflate.New[int64, struct{}](),
		kindHub:         conflate.New[kubesync.KindKey, struct{}](),
		restarted:       testutil.NewSignal(),
	}
}

func (f *fakeKubesync) TrackDiscovery(cacheID int64, p kubesync.Params) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.discovery[cacheID] = p
}

func (f *fakeKubesync) ForgetDiscovery(cacheID int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.discovery, cacheID)
}

func (f *fakeKubesync) ForgetCache(cacheID int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.discovery, cacheID)
	delete(f.kinds, cacheID)
}

func (f *fakeKubesync) TrackKind(cacheID int64, k kubestore.Kind) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.kinds[cacheID] == nil {
		f.kinds[cacheID] = map[kubesync.KindKey]bool{}
	}
	f.kinds[cacheID][kubesync.KindKey{CacheID: cacheID, Kind: k}] = true
}

func (f *fakeKubesync) ForgetKind(cacheID int64, k kubestore.Kind) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.kinds[cacheID], kubesync.KindKey{CacheID: cacheID, Kind: k})
}

func (f *fakeKubesync) GetDiscoveryState(cacheID int64) (kubesync.DiscoveryState, bool) {
	if f.onDiscoveryRead != nil {
		f.onDiscoveryRead(cacheID)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	state, ok := f.discoveryStates[cacheID]
	return state, ok
}

func (f *fakeKubesync) GetKindState(cacheID int64, k kubestore.Kind) (kubesync.KindState, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	state, ok := f.kindStates[kubesync.KindKey{CacheID: cacheID, Kind: k}]
	return state, ok
}

func (f *fakeKubesync) ListenForDiscoveryChanges() kubesync.DiscoveryChangeNotifications {
	return f.discoveryHub.Receiver()
}
func (f *fakeKubesync) ListenForKindChanges() kubesync.KindChangeNotifications {
	return f.kindHub.Receiver()
}

func (f *fakeKubesync) RunWithCacheSyncStopped(cacheID int64, fn func() error) error {
	f.mu.Lock()
	f.stoppedCache = append(f.stoppedCache, cacheID)
	f.holding = true
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.holding = false
		f.mu.Unlock()
	}()
	return fn()
}

func (f *fakeKubesync) RunWithKindSyncStopped(cacheID int64, k kubestore.Kind, fn func() error) error {
	f.mu.Lock()
	f.stoppedKind = append(f.stoppedKind, kubesync.KindKey{CacheID: cacheID, Kind: k})
	f.mu.Unlock()
	return fn()
}

func (f *fakeKubesync) RestartAll() { f.restarted.Fire() }

// armedKinds is what a cache has registered, as the keys a test compares against.
func (f *fakeKubesync) armedKinds(cacheID int64) []kubesync.KindKey {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := slices.Collect(maps.Keys(f.kinds[cacheID]))
	slices.SortFunc(keys, func(a, b kubesync.KindKey) int {
		return cmp.Or(cmp.Compare(a.APIVersion, b.APIVersion), cmp.Compare(a.Resource, b.Resource))
	})
	return keys
}

// armedDiscovery is the params a cache is armed under, false when nothing armed it.
func (f *fakeKubesync) armedDiscovery(cacheID int64) (kubesync.Params, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.discovery[cacheID]
	return p, ok
}

// setKindState is a worker having answered for one kind.
func (f *fakeKubesync) setKindState(cacheID int64, k kubestore.Kind, state kubesync.KindState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kindStates[kubesync.KindKey{CacheID: cacheID, Kind: k}] = state
}

// setDiscoveryState is a sweep having answered for one cache.
func (f *fakeKubesync) setDiscoveryState(cacheID int64, state kubesync.DiscoveryState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.discoveryStates[cacheID] = state
}

// publishDiscoveryChange is a sweep having something new to say about one cache.
func (f *fakeKubesync) publishDiscoveryChange(cacheID int64) {
	_ = f.discoveryHub.Sender().Send(cacheID, struct{}{})
}

// publishKindChange is one kind's worker having something new to say.
func (f *fakeKubesync) publishKindChange(key kubesync.KindKey) {
	_ = f.kindHub.Sender().Send(key, struct{}{})
}

// inHold reports whether a RunWithCacheSyncStopped is running its fn now, for a test
// pinning what runs inside the hold and what runs after it.
func (f *fakeKubesync) inHold() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.holding
}

// stoppedCaches and stoppedKinds are what a clear ran inside, for a test pinning that the
// workers were down before the file moved under them.
func (f *fakeKubesync) stoppedCaches() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.stoppedCache)
}

func (f *fakeKubesync) stoppedKinds() []kubesync.KindKey {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.stoppedKind)
}

// newTestDepsOverAClosedStore returns the shared set over a beehive whose store has been
// closed, so every read and write through it fails. It stands in for the storage faults —
// an app.db that goes unreadable under a running service — that no test can otherwise
// produce, and what it proves is that a fault is reported rather than answered as absence.
func newTestDepsOverAClosedStore(t *testing.T) deps {
	t.Helper()
	d, closeStore := newTestDepsWithABreakableStore(t)
	closeStore()
	return d
}

// newTestDepsWithABreakableStore is the same set beside the close, for a test that needs
// records to exist before the store under them goes.
func newTestDepsWithABreakableStore(t *testing.T) (deps, func()) {
	t.Helper()
	store, err := beehivesqlite.OpenMemory()
	require.NoError(t, err)
	bh, err := beehive.New(store)
	require.NoError(t, err)
	registerCacheStandIn(t, bh)
	d := newTestDepsOver(t, bh)
	return d, func() { require.NoError(t, store.Close()) }
}

// importCluster inserts a kubeconfig row for contextName, as the importer does, and
// returns it.
func importCluster(t *testing.T, d deps, contextName string) ClusterRow {
	t.Helper()
	id := appdb.NewID()
	require.NoError(t, d.store.InTx(context.Background(), func(st stmts) error {
		_, err := insertClusterIfAbsent(context.Background(), st, id, contextName, t0)
		return err
	}))
	row, ok, err := getCluster(context.Background(), d.store.Stmts(), ClusterID(id))
	require.NoError(t, err)
	require.True(t, ok)
	return row
}

// createCluster gives contextName a row and the runtime object the mirror would create
// for it, and returns the object.
func createCluster(t *testing.T, d deps, contextName string) *beehive.Object[ClusterRuntimeSpec, ClusterStatus] {
	t.Helper()
	row := importCluster(t, d, contextName)
	obj, _, err := d.clusterClient.CreateOrUpdate(context.Background(), string(row.ID), runtimeSpecOf(row))
	require.NoError(t, err)
	return obj
}

// kubeconfigRuntimeSpec is the runtime spec of an enabled, syncing kubeconfig row.
func kubeconfigRuntimeSpec(contextName string) ClusterRuntimeSpec {
	return ClusterRuntimeSpec{Source: SourceKubeconfig, SourceKey: &contextName, Enabled: true, SyncEnabled: true}
}
