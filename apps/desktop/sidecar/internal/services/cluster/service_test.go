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

package cluster

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	beehivesqlite "github.com/amorey/beehive/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/lifecycle"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlitemigrate/sqlitemigratetest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubeconn"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/poke"
)

// newTestService builds a service over a temp dir, closed on cleanup.
func newTestService(t *testing.T) Service {
	t.Helper()
	svc, err := New(openTestDB(t), testPaths(t), newTestKubeconfig(t), nil)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, svc.Close()) })
	return svc
}

// A beehive.db a newer build migrated fails the service with the text the start
// screen shows, naming the file and both versions. The supported version is
// beehive's, so it is not pinned.
func TestANewerBeehiveDBFailsTheServiceSayingSo(t *testing.T) {
	t.Parallel()

	paths := testPaths(t)
	store, err := beehivesqlite.Open(paths.BeehiveDBFile)
	require.NoError(t, err)
	require.NoError(t, store.Close())
	sqlitemigratetest.StampNewer(t, paths.BeehiveDBFile)

	_, err = New(openTestDB(t), paths, newTestKubeconfig(t), nil)

	require.Error(t, err)
	assert.Regexp(t, `^beehive\.db was written by a newer Kubetail \(schema 999; this build reads up to \d+\)$`, err.Error())
}

// The runtime store and the mirror are where the service is handed them: the
// mirror apart, since it is what Kubetail can lose.
func TestTheMirrorIsInTheCacheDirectory(t *testing.T) {
	t.Parallel()

	data, cache := t.TempDir(), t.TempDir()
	paths := Paths{BeehiveDBFile: filepath.Join(data, "beehive.db"), KubestoreDir: filepath.Join(cache, "kube")}
	svc, err := New(openTestDB(t), paths, newTestKubeconfig(t), nil)
	require.NoError(t, err)
	defer func() { assert.NoError(t, svc.Close()) }()

	store, err := svc.(*service).kubestoreMgr.(*kubestore.Manager).OpenOrCreate(1)
	require.NoError(t, err)
	store.Release()

	assert.FileExists(t, paths.BeehiveDBFile)
	assert.FileExists(t, filepath.Join(cache, "kube", "1.db"))
}

// The accessors are stateless views onto the one service, so each returns something
// usable and none of them is the nil interface.
func TestFamilyAccessors(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)

	assert.NotNil(t, svc.Clusters())
	assert.NotNil(t, svc.Caches())
	assert.NotNil(t, svc.CachedKinds())
	assert.NotNil(t, svc.CachedData())
}

// The full lifecycle the composition root drives: Start, the stop func it returns,
// then Close. Anything left running here would outlive the process's drain.
func TestStartStopClose(t *testing.T) {
	t.Parallel()

	svc, err := New(openTestDB(t), testPaths(t), newTestKubeconfig(t), nil)
	require.NoError(t, err)

	stop, err := svc.Start(context.Background())
	require.NoError(t, err)

	testutil.WaitReturn(t, func() { assert.NoError(t, stop(context.Background())) }, "stop to return")

	assert.NoError(t, svc.Close())
}

// The log files arrive by SetCacheFiles, once the logs service exists; a pass that
// reaches them before that fails rather than leaving a file behind.
func TestSetCacheFilesHandsThePassesTheOwner(t *testing.T) {
	t.Parallel()

	svc, err := New(openTestDB(t), testPaths(t), newTestKubeconfig(t), nil)
	require.NoError(t, err)
	defer func() { assert.NoError(t, svc.Close()) }()
	files := svc.(*service).cacheFiles

	assert.ErrorContains(t, files.Clear(1), "cache file owner")
	assert.ErrorContains(t, files.Remove(1), "cache file owner")

	owner := &fakeCacheFiles{}
	svc.SetCacheFiles(owner)
	require.NoError(t, files.Clear(1))
	require.NoError(t, files.Remove(2))
	assert.Equal(t, []int64{1}, owner.cleared)
	assert.Equal(t, []int64{2}, owner.removed)
}

// A path that cannot hold the store is a startup failure, not a
// service running against a database it never opened.
func TestNewRejectsAnUnopenableStore(t *testing.T) {
	t.Parallel()

	paths := testPaths(t)
	// A directory where the database file goes: sqlite cannot open it.
	require.NoError(t, os.MkdirAll(paths.BeehiveDBFile, 0o700))

	_, err := New(openTestDB(t), paths, newTestKubeconfig(t), nil)
	assert.ErrorContains(t, err, "open beehive store")
}

// A spec the build cannot honour stops the start naming the variable, so the start screen
// says which setting to fix.
func TestNewRefusesAFaultSpecItCannotRead(t *testing.T) {
	t.Parallel()

	_, err := New(openTestDB(t), testPaths(t), newTestKubeconfig(t), nil, WithFaults("get pods=teapot"))

	assert.ErrorContains(t, err, "KUBETAIL_FAULTS: ")
}

// A database the statements cannot be prepared on is a startup failure.
func TestNewRejectsADatabaseThatWillNotPrepare(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	require.NoError(t, db.Close())

	_, err := New(db, testPaths(t), newTestKubeconfig(t), nil)
	assert.ErrorContains(t, err, "prepare cluster statements")
}

// beehive rejects a second controller for a kind, and that error has to reach New's
// caller rather than leaving a kind silently unreconciled.
func TestRegisterControllersRejectsADuplicateKind(t *testing.T) {
	t.Parallel()

	bh := newTestBeehive(t)

	_, err := registerControllers(bh, deps{})
	require.NoError(t, err)

	_, err = registerControllers(bh, deps{})
	assert.Error(t, err)
}

// Start is not re-entrant: beehive refuses a second start, and the failure must
// surface rather than half-starting the controllers behind it.
func TestStartRejectsASecondStart(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	stop, err := svc.Start(context.Background())
	require.NoError(t, err)
	defer func() { assert.NoError(t, stop(context.Background())) }()

	// kubeconn is the first part in the order that refuses one, so nothing with background
	// work has started for the unwind to drain.
	_, err = svc.Start(context.Background())
	assert.ErrorContains(t, err, "start kubeconn")
}

// partNamed returns the service's part with that name.
func partNamed(t *testing.T, svc *service, name string) lifecycle.Part {
	t.Helper()
	i := slices.IndexFunc(svc.parts, func(p lifecycle.Part) bool { return p.Name == name })
	require.GreaterOrEqual(t, i, 0, "no part named %q", name)
	return svc.parts[i]
}

// failingPart refuses to start, so a test can drive the unwind path.
type failingPart struct{ err error }

func (f failingPart) Start(context.Context) (func(context.Context) error, error) {
	return nil, f.err
}

func (failingPart) Close() error { return nil }

// beehive starts before the controllers, so a controller that fails to start leaves
// it running with no stop func to reach it. Start has to drain it on the way out.
func TestStartDrainsBeehiveWhenAControllerFails(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	svc := newTestService(t).(*service)
	// Beehive first, then a part that fails: by name, since a position moves whenever
	// something is added ahead of it.
	svc.parts = []lifecycle.Part{partNamed(t, svc, "beehive"), {Name: "a", StartCloser: failingPart{err: boom}}}

	_, err := svc.Start(context.Background())
	require.ErrorIs(t, err, boom)

	// beehive refuses to restart once stopped, which is how a drained instance is
	// told apart from one still running.
	_, err = svc.Start(context.Background())
	assert.ErrorContains(t, err, "already stopped")
}

// The bootstrap is a Part, so a store that cannot hold the anchors fails the start
// rather than leaving a service whose discovery pass has nothing to run against.
func TestStartFailsWhenTheAnchorsCannotBeCreated(t *testing.T) {
	t.Parallel()

	part := clusterSourceBootstrap(deps{sourceClient: &stubSourceClient{createErr: errors.New("boom")}})

	_, err := lifecycle.StartAll(context.Background(), []lifecycle.Part{part})
	assert.ErrorContains(t, err, "cluster source records")
}

// --- the connection surface ---

// A claim names the record's context, which is what arms the probe behind it.
func TestAcquireConnectionClaimsTheRecordsContext(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	pool := d.kubeconnSvc.(*fakeKubeconn)

	lease, err := serviceOver(t, d).AcquireConnection(context.Background(), clusterIDOf(obj))

	require.NoError(t, err)
	require.NotNil(t, lease)
	assert.Equal(t, []string{"prod"}, pool.asked)
}

// The claim is the caller's own: releasing it is the caller's business, and says nothing
// about the one clusterController holds to keep this cluster probed.
func TestAcquireConnectionHandsBackAReleasableClaim(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	pool := d.kubeconnSvc.(*fakeKubeconn)

	lease, err := serviceOver(t, d).AcquireConnection(context.Background(), clusterIDOf(obj))
	require.NoError(t, err)
	lease.Release()

	assert.Equal(t, []string{"prod"}, pool.released)
}

func TestRetryConnectionReprobesTheRecordsContext(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	pool := d.kubeconnSvc.(*fakeKubeconn)

	ctx := context.Background()
	require.NoError(t, serviceOver(t, d).RetryConnection(ctx, clusterIDOf(obj)))

	assert.Equal(t, []string{"prod"}, pool.retried)
	assert.Empty(t, pool.asked, "a retry reaches the probe, not the claim")
	assert.Equal(t, ctx, pool.retryCtx, "the caller's deadline bounds the wait")
}

// The call is the probe's whole round trip now, so a wait that ends without one — its ceiling, or
// a caller that went away — is the caller's answer rather than something to swallow.
func TestRetryConnectionReportsAProbeThatNeverAnswered(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	pool := d.kubeconnSvc.(*fakeKubeconn)
	pool.retryErr = errors.New("nothing committed")

	err := serviceOver(t, d).RetryConnection(context.Background(), clusterIDOf(obj))

	assert.ErrorIs(t, err, pool.retryErr)
}

// The gate is one rule, asserted over every method that goes through it: a caller must
// not find one of them willing to act on a record the others refuse.
func TestTheConnectionSurfaceRefusesTheSameRecords(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		record func(t *testing.T, d deps) ClusterID
		want   error
	}{
		"an id naming nothing": {
			record: func(*testing.T, deps) ClusterID { return ClusterID("404") },
			want:   ErrNotFound,
		},
		"a disabled cluster": {
			record: func(t *testing.T, d deps) ClusterID {
				obj := createCluster(t, d, "prod")
				_, err := setClusterToggle(context.Background(), d.store.Stmts(), stmtSetClusterEnabled, clusterIDOf(obj), false, t0)
				require.NoError(t, err)
				return clusterIDOf(obj)
			},
			want: ErrNotConnectable,
		},
		// The mark is read off the row: the runtime object is marked only on the
		// mirror's next pass, and a claim must not be handed out in between.
		"a cluster awaiting deletion": {
			record: func(t *testing.T, d deps) ClusterID {
				obj := createCluster(t, d, "prod")
				_, err := markCluster(context.Background(), d.store.Stmts(), clusterIDOf(obj), t0)
				require.NoError(t, err)
				return clusterIDOf(obj)
			},
			want: ErrNotConnectable,
		},
		"a cluster from a source with no credentials": {
			record: func(t *testing.T, d deps) ClusterID {
				_, err := d.db.Write.Exec(`INSERT INTO clusters (id, source, created_at, updated_at) VALUES ('adopted', 'cloud', 0, 0)`)
				require.NoError(t, err)
				return ClusterID("adopted")
			},
			want: ErrNotConnectable,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			d := newTestDeps(t)
			id := tt.record(t, d)
			svc := serviceOver(t, d)
			pool := d.kubeconnSvc.(*fakeKubeconn)

			_, acquireErr := svc.AcquireConnection(context.Background(), id)
			retryErr := svc.RetryConnection(context.Background(), id)
			_, scheduleErr := svc.Clusters().WatchScheduleState(context.Background(), id)

			assert.ErrorIs(t, acquireErr, tt.want)
			assert.ErrorIs(t, retryErr, tt.want)
			assert.ErrorIs(t, scheduleErr, tt.want)
			assert.Empty(t, pool.asked)
			assert.Empty(t, pool.retried)
		})
	}
}

// A cluster nothing can reach is still claimed and still retried: whether the server
// answers is the probe's to report, not the record's to refuse.
func TestTheConnectionSurfaceServesAnUnreachableCluster(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	d.kubeconnSvc = knowing(kubeconn.State{Connection: failed(errors.New("no route to host"))})
	pool := d.kubeconnSvc.(*fakeKubeconn)
	svc := serviceOver(t, d)

	lease, err := svc.AcquireConnection(context.Background(), clusterIDOf(obj))
	require.NoError(t, err)
	require.NoError(t, svc.RetryConnection(context.Background(), clusterIDOf(obj)))

	assert.False(t, lease.State().Connection.OK())
	assert.Equal(t, []string{"prod"}, pool.retried)
}

// The sync seam is a lifecycle participant like every other, and its place in the order is the
// contract: no pass can arm a cache that is stopping, and no worker outlives the store it writes.
func TestKubesyncStopsBetweenBeehiveAndTheStore(t *testing.T) {
	t.Parallel()

	svc, err := New(openTestDB(t), testPaths(t), newTestKubeconfig(t), poke.New())
	require.NoError(t, err)
	// New opens the store even unstarted, and Windows will not unlink a file something
	// still holds open.
	t.Cleanup(func() { assert.NoError(t, svc.Close()) })

	var names []string
	for _, part := range svc.(*service).parts {
		names = append(names, part.Name)
	}
	require.Subset(t, names, []string{"kubestore", "kubesync", "beehive"})
	assert.Less(t, slices.Index(names, "kubestore"), slices.Index(names, "kubesync"),
		"kubesync starts after the store it writes through, so it stops before it")
	assert.Less(t, slices.Index(names, "kubesync"), slices.Index(names, "beehive"),
		"beehive starts last and stops first, so no pass arms a cache that is stopping")
}

// A watch that died under a sleeping machine reports nothing, so a resume is the only thing that
// brings it back.
func TestAResumePokeRestartsEverySync(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pokeSvc := poke.New()
	sync := newFakeKubesync()

	stop, err := restartSyncsOnResume(sync, pokeSvc).StartCloser.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, stop(ctx)) })

	pokeSvc.Poke(poke.SourceHost)
	testutil.Wait(t, sync.restarted.Chan(), "every sync to be restarted")
}

// Every read and every write reports a storage fault rather than answering as though the
// records were not there. The distinction is the whole of it above: an empty answer is a
// fleet with no clusters, and a view that took one for an unreadable app.db would show the
// user an empty app instead of an error.
func TestEveryEntryPointReportsAStorageFault(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d := newTestDepsOverAClosedStore(t)
	// A row for the id the cluster reads name, so they reach the runtime store
	// rather than answering off the row's absence.
	_, err := d.db.Write.Exec(`INSERT INTO clusters (id, source, source_key, created_at, updated_at) VALUES ('1', 'kubeconfig', 'prod', 0, 0)`)
	require.NoError(t, err)
	s := serviceOver(t, d)
	ops := map[string]func() error{
		"Clusters.Get":                    func() error { _, err := s.Clusters().Get(ctx, ClusterID("1")); return err },
		"Clusters.List":                   func() error { _, err := s.Clusters().List(ctx); return err },
		"Clusters.WatchCollectionByID":    func() error { _, err := s.Clusters().WatchCollectionByID(ctx, ClusterID("1")); return err },
		"Clusters.WatchCollection":        func() error { _, err := s.Clusters().WatchCollection(ctx); return err },
		"Clusters.Delete":                 func() error { return s.Clusters().Delete(ctx, ClusterID("1")) },
		"Clusters.ListEvents":             func() error { _, err := s.Clusters().ListEvents(ctx, ClusterID("1"), nil, nil); return err },
		"Clusters.SubscribeEventFeed":     func() error { _, err := s.Clusters().SubscribeEventFeed(ctx, ClusterID("1"), nil); return err },
		"Caches.Get":                      func() error { _, err := s.Caches().Get(ctx, 1); return err },
		"Caches.WatchCollectionByID":      func() error { _, err := s.Caches().WatchCollectionByID(ctx, 1); return err },
		"Caches.WatchCollection":          func() error { _, err := s.Caches().WatchCollection(ctx); return err },
		"Caches.ListByCluster":            func() error { _, err := s.Caches().ListByCluster(ctx, ClusterID("1")); return err },
		"Caches.WatchCollectionByCluster": func() error { _, err := s.Caches().WatchCollectionByCluster(ctx, ClusterID("1")); return err },
		"Caches.WatchStatsState":          func() error { _, err := s.Caches().WatchStatsState(ctx, ClusterID("1"), 2); return err },
		"Caches.WatchSyncState":           func() error { _, err := s.Caches().WatchSyncState(ctx, ClusterID("1"), 2); return err },
		"Caches.Clear":                    func() error { _, err := s.Caches().Clear(ctx, 1); return err },
		"Caches.Health":                   func() error { _, _, err := s.Caches().Health(ctx, ClusterID("1"), 2); return err },
		"Caches.SyncState":                func() error { _, _, err := s.Caches().SyncState(ctx, ClusterID("1"), 2); return err },
		"CachedKinds.WatchCollectionByID": func() error { _, err := s.CachedKinds().WatchCollectionByID(ctx, 1); return err },
		"CachedKinds.WatchCollection":     func() error { _, err := s.CachedKinds().WatchCollection(ctx); return err },
		"CachedKinds.ListByCache":         func() error { _, err := s.CachedKinds().ListByCache(ctx, 1); return err },
		"CachedKinds.WatchCollectionByCache": func() error {
			_, err := s.CachedKinds().WatchCollectionByCache(ctx, 1)
			return err
		},
		"CachedKinds.Clear":              func() error { _, err := s.CachedKinds().Clear(ctx, 1); return err },
		"CachedKinds.SetSyncEnabled":     func() error { _, err := s.CachedKinds().SetSyncEnabled(ctx, 1, true); return err },
		"CachedData.ListKinds":           func() error { _, err := s.CachedData().ListKinds(ctx, ClusterID("1"), 2, ""); return err },
		"CachedData.WatchKindCollection": func() error { _, err := s.CachedData().WatchKindCollection(ctx, ClusterID("1"), 2, ""); return err },
		"CachedData.WatchObjectCollection": func() error {
			_, err := s.CachedData().WatchObjectCollection(ctx, ClusterID("1"), 2, "v1", "pods")
			return err
		},
		"CachedData.WatchObjectCollectionByNamespaces": func() error {
			_, err := s.CachedData().WatchObjectCollectionByNamespaces(ctx, ClusterID("1"), 2, "v1", "pods", []string{"prod"})
			return err
		},
		"CachedData.WatchEventCollection": func() error { _, err := s.CachedData().WatchEventCollection(ctx, ClusterID("1"), 2); return err },
		"ListEvents":                      func() error { _, err := s.ListEvents(ctx, 1, nil, nil); return err },
		"SubscribeObjectEventFeed":        func() error { _, err := s.SubscribeObjectEventFeed(ctx, 1, nil); return err },
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) { assert.Error(t, op()) })
	}
}

// WatchHealthKeyedState hands back a stream and folds afterwards, so its fault lands
// on the stream rather than at the call. Reported and not swallowed — a fleet whose health
// cannot be read is not a healthy one.
func TestWatchHealthKeyedStateReportsAStorageFaultOnItsStream(t *testing.T) {
	t.Parallel()

	s := serviceOver(t, newTestDepsOverAClosedStore(t))

	stream, err := s.Caches().WatchHealthKeyedState(context.Background())

	require.NoError(t, err)
	testutil.WaitClosed(t, stream.Frames, "the fold to give up on an unreadable store")
	assert.Error(t, stream.Err())
}

// A pair that names no cache is not an error: a caller holds ids from watch frames, so a
// record collected in between is an ordinary race, and the per-cache reads answer it as
// definitively empty rather than as a fault.
func TestCacheBelongsToAnswersAnUnknownCacheAsNoMatch(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)

	ok, err := serviceOver(t, d).cacheBelongsTo(context.Background(), ClusterID("1"), 404)

	require.NoError(t, err)
	assert.False(t, ok)
}

// The connection gate reads the row, so a database that will not answer is a fault,
// never a record that is not there.
func TestTheConnectionSurfaceReportsAStorageFault(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	require.NoError(t, d.db.Close())
	svc := serviceOver(t, d)

	_, err := svc.AcquireConnection(context.Background(), clusterIDOf(obj))
	assert.Error(t, err)
	_, err = svc.Clusters().Get(context.Background(), clusterIDOf(obj))
	assert.Error(t, err)
	_, err = svc.Clusters().List(context.Background())
	assert.Error(t, err)
	_, err = svc.Clusters().SetEnabled(context.Background(), clusterIDOf(obj), false)
	assert.Error(t, err)
	assert.Error(t, svc.Clusters().Delete(context.Background(), clusterIDOf(obj)))
}
