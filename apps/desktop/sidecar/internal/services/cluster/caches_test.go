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
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amorey/beehive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubeconn"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubesync"
)

// One cache per identity per cluster: the name is the creation/dedup key beehive's
// name-uniqueness enforces, so a UID migration must yield a second, distinct name
// rather than colliding with the cache it supersedes.
func TestClusterCacheName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "7/uid-1", ClusterCacheName("7", "uid-1"))
	assert.NotEqual(t, ClusterCacheName("7", "uid-1"), ClusterCacheName("7", "uid-2"))
	assert.NotEqual(t, ClusterCacheName("7", "uid-1"), ClusterCacheName("8", "uid-1"))
}

// Sync runs only where all three hold: the cluster is tracked, syncing is on, and this
// cache mirrors the identity the cluster is currently probed at. An inactive cache is
// the trap — a UID migration leaves one behind, and it must not keep syncing.
func TestCacheSyncEnabled(t *testing.T) {
	t.Parallel()

	cluster := func(enabled, sync bool, uid string) *beehive.Object[ClusterRuntimeSpec, ClusterStatus] {
		return &beehive.Object[ClusterRuntimeSpec, ClusterStatus]{
			Spec:   ClusterRuntimeSpec{Enabled: enabled, SyncEnabled: sync},
			Status: &ClusterStatus{Server: ClusterServer{UID: &uid}},
		}
	}

	assert.True(t, cacheSyncEnabled(cluster(true, true, "uid-1"), "uid-1"))
	assert.False(t, cacheSyncEnabled(cluster(false, true, "uid-1"), "uid-1"), "the cluster is not tracked")
	assert.False(t, cacheSyncEnabled(cluster(true, false, "uid-1"), "uid-1"), "syncing is paused")
	assert.False(t, cacheSyncEnabled(cluster(true, true, "uid-2"), "uid-1"), "the identity moved on")
	assert.False(t, cacheSyncEnabled(&beehive.Object[ClusterRuntimeSpec, ClusterStatus]{Spec: ClusterRuntimeSpec{Enabled: true, SyncEnabled: true}}, "uid-1"), "never probed")
}

// --- Caches() reads ---

// createCache stores a cache for clusterID under uid, through the same write the
// cluster controller makes — a fixture that hand-rolled the name, spec and owner edge
// could drift from what production actually stores.
func createCache(t *testing.T, caches beehive.Client[ClusterCacheSpec, ClusterCacheStatus], cluster *beehive.Object[ClusterRuntimeSpec, ClusterStatus], uid string) *beehive.Object[ClusterCacheSpec, ClusterCacheStatus] {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, ensureClusterCache(ctx, caches, cluster, uid))

	obj, err := caches.GetByName(ctx, ClusterCacheName(clusterIDOf(cluster), uid))
	require.NoError(t, err)
	return obj
}

// uidsOf reads the identities out of a read, which is what the list tests assert on.
func uidsOf(caches []*ClusterCache) []string {
	uids := make([]string, 0, len(caches))
	for _, cache := range caches {
		uids = append(uids, cache.Spec.ServerUID)
	}
	return uids
}

// The rows are trusted once listed, under no cache-wide failure — stale, paused
// or partly behind included.
func TestClusterCacheHealthTrusted(t *testing.T) {
	t.Parallel()

	for reason, trusted := range map[string]bool{
		"":                              false,
		ReasonConnecting:                false,
		ReasonSizeLimit:                 false,
		kubesync.ReasonStoreFailed:      false,
		kubesync.ReasonIdentityMismatch: false,
		ReasonPaused:                    true,
		kubesync.ReasonWatching:         true,
	} {
		assert.Equal(t, trusted, ClusterCacheHealth{Reason: reason}.Trusted(), reason)
	}
	lastKnown := ClusterCacheHealth{Reason: "Disconnected", TotalKinds: 1, UnhealthyKinds: 1,
		UnhealthyKindRefs: []UnhealthyKindRef{{KindRef: KindRef{APIVersion: "v1", Resource: "pods"}}}}
	assert.True(t, lastKnown.Trusted())

	for reason, refused := range map[string]bool{
		kubesync.ReasonStoreFailed:      true,
		kubesync.ReasonIdentityMismatch: true,
		ReasonConnecting:                false,
		kubesync.ReasonSyncFailed:       false,
	} {
		assert.Equal(t, refused, RefusedCacheWide(reason), reason)
	}
}

func TestCachesGet(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	obj := createCache(t, d.cacheClient, cluster, "uid-1")

	got, err := serviceOver(t, d).Caches().Get(context.Background(), ClusterCacheID(obj.ID))

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, ClusterCacheID(obj.ID), got.ID)
	assert.Equal(t, "uid-1", got.Spec.ServerUID)
	assert.Equal(t, clusterIDOf(cluster), got.ClusterID, "the join key is the cluster the spec names")
}

// An unknown id is not an error: a caller holds ids from watch frames, and a cache
// collected in between is an ordinary race rather than a bad request.
func TestCachesGetUnknownIsNotAnError(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)

	got, err := serviceOver(t, d).Caches().Get(context.Background(), 404)

	require.NoError(t, err)
	assert.Nil(t, got)
}

// twoClustersThreeCaches stores two clusters and three caches, the first cluster owning
// two of them — enough for one fixture to prove both a list's order and its scoping. It
// returns that cluster's id.
func twoClustersThreeCaches(t *testing.T) (deps, ClusterID) {
	t.Helper()
	d := newTestDeps(t)
	one := createCluster(t, d, "prod")
	two := createCluster(t, d, "staging")
	createCache(t, d.cacheClient, one, "uid-1")
	createCache(t, d.cacheClient, two, "uid-2")
	createCache(t, d.cacheClient, one, "uid-3")
	return d, clusterIDOf(one)
}

// One cluster's caches, in the same order — what Cluster.caches serves, and what a
// migration turns into a two-element list.
func TestCachesListByCluster(t *testing.T) {
	t.Parallel()

	d, one := twoClustersThreeCaches(t)

	got, err := serviceOver(t, d).Caches().ListByCluster(context.Background(), one)

	require.NoError(t, err)
	assert.Equal(t, []string{"uid-1", "uid-3"}, uidsOf(got))
}

// A cluster that has never been probed owns none, which is empty rather than an error.
func TestCachesListByClusterWithNone(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")

	got, err := serviceOver(t, d).Caches().ListByCluster(context.Background(), clusterIDOf(cluster))

	require.NoError(t, err)
	assert.Empty(t, got)
}

// An id naming no cluster owns nothing, and answers empty rather than as an error: a
// caller holds ids from watch frames, so a record removed in between is a race.
func TestCachesListByClusterOfAnUnknownClusterIsEmpty(t *testing.T) {
	t.Parallel()

	got, err := serviceOver(t, newTestDeps(t)).Caches().ListByCluster(context.Background(), ClusterID("404"))

	require.NoError(t, err)
	assert.Empty(t, got)
}

// --- Caches() watches ---

// watchCaches opens a cache list watch bounded by the test.
func watchCaches(t *testing.T, d deps) *Stream[ClusterCacheCollectionWatchFrame] {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream, err := serviceOver(t, d).Caches().WatchCollection(ctx)
	require.NoError(t, err)
	return stream
}

// awaitCacheBookmark drains the snapshot up to and including the bookmark.
func awaitCacheBookmark(t *testing.T, stream *Stream[ClusterCacheCollectionWatchFrame]) {
	t.Helper()
	for {
		if testutil.Recv(t, stream.Frames, "the bookmark").Type == CollectionWatchFrameBookmark {
			return
		}
	}
}

// The snapshot arrives as Added frames closed by exactly one Bookmark — the frame a
// consumer renders its empty state on, and never before. Each frame carries the owner
// edge the client joins onto its cluster.
func TestCachesWatchCollectionEmitsTheSnapshotThenABookmark(t *testing.T) {
	t.Parallel()

	d := newRunningDeps(t)
	cluster := createCluster(t, d, "prod")
	createCache(t, d.cacheClient, cluster, "uid-1")
	createCache(t, d.cacheClient, cluster, "uid-2")

	stream := watchCaches(t, d)

	// The snapshot's order is the store's, which no consumer relies on.
	var uids []string
	for range 2 {
		f := testutil.Recv(t, stream.Frames, "a snapshot frame")
		require.Equal(t, CollectionWatchFrameAdded, f.Type)
		require.NotNil(t, f.Cache)
		assert.Equal(t, clusterIDOf(cluster), f.Cache.ClusterID)
		uids = append(uids, f.Cache.Spec.ServerUID)
	}
	assert.ElementsMatch(t, []string{"uid-1", "uid-2"}, uids)

	bookmark := testutil.Recv(t, stream.Frames, "the bookmark closing the snapshot")
	assert.Equal(t, CollectionWatchFrameBookmark, bookmark.Type)
	assert.Nil(t, bookmark.Cache, "the bookmark carries no entity")
}

// An empty collection is definitively empty rather than pending, so the bookmark
// still lands: without it a populated table and an empty one look alike.
func TestCachesWatchCollectionBookmarksAnEmptyCollection(t *testing.T) {
	t.Parallel()

	stream := watchCaches(t, newRunningDeps(t))

	assert.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)
}

// A cache created after the snapshot — how a client learns of the identity a probe
// just recorded.
func TestCachesWatchCollectionReportsACreate(t *testing.T) {
	t.Parallel()

	d := newRunningDeps(t)
	cluster := createCluster(t, d, "prod")
	stream := watchCaches(t, d)
	awaitCacheBookmark(t, stream)

	createCache(t, d.cacheClient, cluster, "uid-1")

	f := testutil.Recv(t, stream.Frames, "the create")
	assert.Equal(t, CollectionWatchFrameAdded, f.Type)
	require.NotNil(t, f.Cache)
	assert.Equal(t, "uid-1", f.Cache.Spec.ServerUID)
	assert.Equal(t, clusterIDOf(cluster), f.Cache.ClusterID)
}

// A removal whose final row beehive could not decode carries no object, and nothing
// later in the log mentions the id. The frame still has to name it: a consumer drops a
// change with no entity, so the record would sit in its map until the subscription ends.
func TestCachesWatchCollectionReportsAnUndecodableDeparture(t *testing.T) {
	t.Parallel()

	frames := pumpFrames(t, cacheWatch, nil,
		beehive.ObjectChange[ClusterCacheSpec, ClusterCacheStatus]{Type: beehive.Deleted, ID: 7, Object: nil},
	)

	require.Len(t, frames, 2)
	assert.Equal(t, CollectionWatchFrameDeleted, frames[1].Type)
	require.NotNil(t, frames[1].Cache, "the bookmark is the only frame that carries no entity")
	assert.Equal(t, ClusterCacheID(7), frames[1].Cache.ID)
}

// A removal reaches the subscriber carrying the row's final state and no owner: beehive
// loads no edges for a collected row, and a frame that failed over that would kill the
// stream and strand the record in the client's map. Driven through the store because
// that load is beehive's to do — a hand-built object proves nothing about it.
//
// The GC interval is shrunk rather than waited out: a cache is client-only here (no
// controller is registered), so its removal costs one sweep.
func TestCachesWatchCollectionReportsADeparture(t *testing.T) {
	t.Parallel()

	d := newRunningDeps(t, beehive.WithGCInterval(time.Millisecond))
	cluster := createCluster(t, d, "prod")
	obj := createCache(t, d.cacheClient, cluster, "uid-1")
	stream := watchCaches(t, d)
	awaitCacheBookmark(t, stream)

	require.NoError(t, d.cacheClient.Delete(context.Background(), obj.ID))

	// The mark and the removal are separate frames unless beehive folds them into one
	// Deleted, which it does when both land in a single tail page.
	for {
		f := testutil.Recv(t, stream.Frames, "the departure")
		require.NotNil(t, f.Cache)
		assert.Equal(t, ClusterCacheID(obj.ID), f.Cache.ID)
		if f.Type == CollectionWatchFrameDeleted {
			assert.Equal(t, "uid-1", f.Cache.Spec.ServerUID, "the removal carries the row's final state")
			assert.Equal(t, clusterIDOf(cluster), f.Cache.ClusterID, "the removal carries its join key")
			return
		}
	}
}

// watchCache opens a collection watch of one cache, bounded by the test.
func watchCache(t *testing.T, d deps, id ClusterCacheID) *Stream[ClusterCacheCollectionWatchFrame] {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream, err := serviceOver(t, d).Caches().WatchCollectionByID(ctx, id)
	require.NoError(t, err)
	return stream
}

func TestCachesWatchCollectionByIDEmitsTheRecordThenABookmark(t *testing.T) {
	t.Parallel()

	d := newRunningDeps(t)
	cluster := createCluster(t, d, "prod")
	obj := createCache(t, d.cacheClient, cluster, "uid-1")

	stream := watchCache(t, d, ClusterCacheID(obj.ID))

	first := testutil.Recv(t, stream.Frames, "the record")
	assert.Equal(t, CollectionWatchFrameAdded, first.Type)
	require.NotNil(t, first.Cache)
	assert.Equal(t, ClusterCacheID(obj.ID), first.Cache.ID)
	assert.Equal(t, clusterIDOf(cluster), first.Cache.ClusterID)

	assert.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)
}

// An id naming nothing is bookmark-only rather than an error: the record may not
// exist yet, and the same subscription reports it arriving.
func TestCachesWatchCollectionByIDBookmarksAnUnknownID(t *testing.T) {
	t.Parallel()

	stream := watchCache(t, newRunningDeps(t), 404)

	assert.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)
}

func TestCachesWatchCollectionByIDReportsChangesToItsRecord(t *testing.T) {
	t.Parallel()

	d := newRunningDeps(t)
	cluster := createCluster(t, d, "prod")
	obj := createCache(t, d.cacheClient, cluster, "uid-1")
	stream := watchCache(t, d, ClusterCacheID(obj.ID))
	awaitCacheBookmark(t, stream)

	require.NoError(t, d.cacheClient.Delete(context.Background(), obj.ID))

	// The type is unpinned: GC can collect the row before this reads, and beehive folds
	// the mark and the removal into one Deleted when both land in a single tail page.
	f := testutil.Recv(t, stream.Frames, "the deletion")
	require.NotNil(t, f.Cache)
	assert.Equal(t, ClusterCacheID(obj.ID), f.Cache.ID)
}

// Cancellation is an ordinary teardown, so Frames closes with nothing to report: a
// consumer reads Err on close, and a reason there is rendered as a dead watch.
func TestCachesWatchCollectionCancellationIsQuiet(t *testing.T) {
	t.Parallel()

	d := newRunningDeps(t)
	ctx, cancel := context.WithCancel(context.Background())

	stream, err := serviceOver(t, d).Caches().WatchCollection(ctx)
	require.NoError(t, err)
	awaitCacheBookmark(t, stream)

	cancel()

	testutil.WaitClosed(t, stream.Frames, "the frames to close on cancellation")
	assert.NoError(t, stream.Err())
}

// --- clusterCacheController ---

// storedCluster writes a tracked cluster with syncing set as asked, probed at uid.
// Both halves are stored, because the reconcile under test re-reads them.
func storedCluster(t *testing.T, d deps, status *beehive.AdminClient[ClusterStatus], syncEnabled bool, uid string) *beehive.Object[ClusterRuntimeSpec, ClusterStatus] {
	t.Helper()
	ctx := context.Background()
	obj := createCluster(t, d, "prod")

	obj, err := d.clusterClient.Update(ctx, obj.ID, ClusterRuntimeSpec{Enabled: true, SyncEnabled: syncEnabled, Source: obj.Spec.Source, SourceKey: obj.Spec.SourceKey})
	require.NoError(t, err)
	require.NoError(t, status.UpdateStatus(ctx, obj.ID, ClusterStatus{Server: ClusterServer{UID: &uid}}))
	return obj
}

// cacheControllerClient stands in for the client beehive binds to the object being
// reconciled. The embedded interface is nil: nothing the pass calls goes unimplemented here,
// so a method it grows shows up as a panic rather than as silence.
type cacheControllerClient struct {
	beehive.ControllerClient[ClusterCacheStatus]
	events       []beehive.EventSpec
	dependencies []beehive.ObjectID
	conditions   []Condition
	deleted      []string
	// finalizersCleared is what the deleting arm cleared, in order; finalizerErr fails
	// the clear.
	finalizersCleared []string
	finalizerErr      error
	// dependErr and eventErr fail the two writes a pass makes through this client, which
	// is the only way to reach what a pass does when one of them will not land.
	dependErr error
	eventErr  error
	// conditionErr fails the size condition's set and delete.
	conditionErr error
}

func (c *cacheControllerClient) AddDependency(_ context.Context, toID beehive.ObjectID) error {
	c.dependencies = append(c.dependencies, toID)
	return c.dependErr
}

func (c *cacheControllerClient) SetCondition(_ context.Context, cond Condition) error {
	c.conditions = append(c.conditions, cond)
	return c.conditionErr
}

func (c *cacheControllerClient) DeleteCondition(_ context.Context, conditionType string) error {
	c.deleted = append(c.deleted, conditionType)
	return c.conditionErr
}

func (c *cacheControllerClient) AddEvent(_ context.Context, event beehive.EventSpec) error {
	c.events = append(c.events, event)
	return c.eventErr
}

func (c *cacheControllerClient) DeleteFinalizer(_ context.Context, finalizer string) error {
	c.finalizersCleared = append(c.finalizersCleared, finalizer)
	return c.finalizerErr
}

// reconcileCache runs one cache pass the way beehive would.
func reconcileCache(t *testing.T, d deps, obj *beehive.Object[ClusterCacheSpec, ClusterCacheStatus]) *cacheControllerClient {
	t.Helper()
	client := &cacheControllerClient{}
	res := (&clusterCacheController{deps: d}).Reconcile(context.Background(), client, obj)
	require.Equal(t, beehive.Settled(), res)
	return client
}

// The pass writes no status, so the result it returns is the only thing that reports
// it converged. Unsettled, the store keeps every cache owed and beehive's pass re-runs
// this reconcile, store reads and all, every interval forever.
func TestCacheReconcileSettlesTheGeneration(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")

	res := (&clusterCacheController{deps: d}).Reconcile(context.Background(), &cacheControllerClient{}, cache)

	assert.Equal(t, beehive.Settled(), res)
}

// The cache's file is named for an id that dies with the record, so nothing could
// find it afterwards: the teardown pass is where it is deleted.
func TestCacheReconcileDeletesTheStoreForADyingCache(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	require.NoError(t, d.cacheClient.Delete(context.Background(), cache.ID))

	obj, err := d.cacheClient.Get(context.Background(), cache.ID)
	require.NoError(t, err)
	reconcileCache(t, d, obj)

	assert.Equal(t, []int64{int64(cache.ID)}, kubestoreFake(d).removed)
}

// The log file goes with the cache, in the same pass as the mirror's and after it.
func TestCacheReconcileRemovesTheLogFileAfterTheMirrors(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	var order []string
	kubestoreFake(d).onRemove = func(int64) { order = append(order, "mirror") }
	files := &fakeCacheFiles{onRemove: func(int64) { order = append(order, "logs") }}
	d.cacheFiles.set(files)
	require.NoError(t, d.cacheClient.Delete(context.Background(), cache.ID))

	obj, err := d.cacheClient.Get(context.Background(), cache.ID)
	require.NoError(t, err)
	reconcileCache(t, d, obj)

	assert.Equal(t, []string{"mirror", "logs"}, order)
	assert.Equal(t, []int64{int64(cache.ID)}, files.removed)
}

// A log file that will not go fails the pass as the mirror's does, with the finalizer
// intact, so the pass runs again rather than leaving a file nothing names any more.
func TestCacheReconcileKeepsTheFinalizerWhenTheLogFileWillNotGo(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	d.cacheFiles.set(&fakeCacheFiles{err: assert.AnError})
	require.NoError(t, d.cacheClient.Delete(context.Background(), cache.ID))
	obj, err := d.cacheClient.Get(context.Background(), cache.ID)
	require.NoError(t, err)
	client := &cacheControllerClient{}

	res := (&clusterCacheController{deps: d}).Reconcile(context.Background(), client, obj)

	assert.NotEqual(t, beehive.Settled(), res)
	assert.Empty(t, client.finalizersCleared, "the finalizer holds the row for the retry")
}

func TestCacheReconcileLeavesTheStoreAloneForALiveCache(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")

	reconcileCache(t, d, cache)

	assert.Empty(t, kubestoreFake(d).removed)
}

// A store that will not delete fails the pass: the file is the pass's whole job here,
// and settling would leave it on disk with nothing left to name it.
func TestCacheReconcileFailsWhenTheStoreWillNotDelete(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	require.NoError(t, d.cacheClient.Delete(context.Background(), cache.ID))
	kubestoreFake(d).err = errors.New("boom")

	obj, err := d.cacheClient.Get(context.Background(), cache.ID)
	require.NoError(t, err)
	res := (&clusterCacheController{deps: d}).Reconcile(context.Background(), &cacheControllerClient{}, obj)

	assert.NotEqual(t, beehive.Settled(), res)
}

// Scoped to one cluster: another cluster's cache must not reach this stream, or a
// per-cluster view would fold a record it never asked for.
func TestCachesWatchCollectionByClusterIsScopedToItsCluster(t *testing.T) {
	t.Parallel()

	d := newRunningDeps(t)
	mine := createCluster(t, d, "prod")
	theirs := createCluster(t, d, "staging")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream, err := serviceOver(t, d).Caches().WatchCollectionByCluster(ctx, clusterIDOf(mine))
	require.NoError(t, err)
	awaitCacheBookmark(t, stream)

	createCache(t, d.cacheClient, theirs, "uid-2")
	createCache(t, d.cacheClient, mine, "uid-1")

	f := testutil.Recv(t, stream.Frames, "this cluster's cache")
	require.NotNil(t, f.Cache)
	assert.Equal(t, "uid-1", f.Cache.Spec.ServerUID)
	assert.Equal(t, clusterIDOf(mine), f.Cache.ClusterID)
}

// A UID migration leaves the superseded cache in place, so the scoped watch carries
// both — which is what makes "the set, never the cache" true on this stream too.
func TestCachesWatchCollectionByClusterCarriesEveryIdentity(t *testing.T) {
	t.Parallel()

	d := newRunningDeps(t)
	cluster := createCluster(t, d, "prod")
	createCache(t, d.cacheClient, cluster, "uid-1")
	createCache(t, d.cacheClient, cluster, "uid-2")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream, err := serviceOver(t, d).Caches().WatchCollectionByCluster(ctx, clusterIDOf(cluster))
	require.NoError(t, err)

	var uids []string
	for range 2 {
		f := testutil.Recv(t, stream.Frames, "a snapshot frame")
		require.Equal(t, CollectionWatchFrameAdded, f.Type)
		require.NotNil(t, f.Cache)
		uids = append(uids, f.Cache.Spec.ServerUID)
	}
	assert.ElementsMatch(t, []string{"uid-1", "uid-2"}, uids)
	assert.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)
}

// A cluster owning none bookmarks an empty collection, and so does an id naming no
// cluster: either may still gain a cache this stream reports.
func TestCachesWatchCollectionByClusterBookmarksAClusterWithNone(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream, err := serviceOver(t, newRunningDeps(t)).Caches().WatchCollectionByCluster(ctx, ClusterID("404"))
	require.NoError(t, err)

	assert.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)
}

// A watch opened on a cluster that does not exist yet reports the caches that arrive
// under it once it does — and none of another cluster's.
func TestCachesWatchCollectionByClusterFollowsAClusterThatArrivesLater(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d := newRunningDeps(t)
	other := createCluster(t, d, "staging")
	row := importCluster(t, d, "prod")

	stream, err := serviceOver(t, d).Caches().WatchCollectionByCluster(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)

	createCache(t, d.cacheClient, other, "uid-2")
	obj, _, err := d.clusterClient.CreateOrUpdate(ctx, string(row.ID), runtimeSpecOf(row))
	require.NoError(t, err)
	cache := createCache(t, d.cacheClient, obj, "uid-1")

	f := testutil.Recv(t, stream.Frames, "the cache that arrived")
	assert.Equal(t, CollectionWatchFrameAdded, f.Type)
	require.NotNil(t, f.Cache)
	assert.Equal(t, ClusterCacheID(cache.ID), f.Cache.ID)
}

// --- the state watches ---

// A state watch is current-on-subscribe and emits nothing before its first measurement, so
// the first frame is what the cache holds now.
func TestCachesWatchStatsStateEmitsTheCurrentMeasurement(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	store := kubestoreFake(d)
	store.stats = kubestore.Stats{
		Exists: true, DBBytes: 4096, WALBytes: 512, SHMBytes: 32,
		Counts: kubestore.Counts{ObjectCount: 12, KindCount: 3},
	}

	stream, err := serviceOver(t, d).Caches().WatchStatsState(context.Background(), clusterIDOf(cluster), ClusterCacheID(cache.ID))

	require.NoError(t, err)
	got := testutil.Recv(t, stream.Frames, "the first measurement")
	// The parts ride along with the total: the headline is what the cache costs, and the split
	// is what says whether the WAL is being checkpointed.
	assert.Equal(t, ClusterCacheStats{
		Exists: true, Bytes: 4640, DBBytes: 4096, WALBytes: 512, SHMBytes: 32,
		ObjectCount: 12, KindCount: 3,
	}, got)
}

// The state watch borrows its change notifications and never claims the file. One runs per cache row, so
// a list of twenty caches would otherwise pin twenty idle files open — and the measurement
// needs no open file anyway, since Manager.Stats reads a closed one directly.
func TestCachesWatchStatsStateDoesNotHoldAnIdleCacheOpen(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	mgr := kubestoreFake(d).mgr

	// A cache that was synced and is now idle: the file exists, nothing holds it.
	writer, err := mgr.OpenOrCreate(int64(cache.ID))
	require.NoError(t, err)
	writer.Release()

	stream, err := serviceOver(t, d).Caches().WatchStatsState(ctx, clusterIDOf(cluster), ClusterCacheID(cache.ID))
	require.NoError(t, err)
	testutil.Recv(t, stream.Frames, "the first measurement")

	assert.False(t, cacheIsOpen(mgr, int64(cache.ID)), "the state watch opened an idle cache's file")
}

// A pair naming no live cache still gets its first value at once: a reading that says there is
// no cache, and nothing after it. A caller holding such a pair got it from a frame that has
// since moved on, and drops the subscription itself.
func TestCachesWatchStatsStateSendsOneAbsentReadingForAMismatchedPair(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	other := createCluster(t, d, "staging")
	cache := createCache(t, d.cacheClient, other, "uid-2")
	kubestoreFake(d).stats = kubestore.Stats{Exists: true, DBBytes: 4096}

	stream, err := serviceOver(t, d).Caches().WatchStatsState(context.Background(), clusterIDOf(cluster), ClusterCacheID(cache.ID))

	require.NoError(t, err)
	got := testutil.Recv(t, stream.Frames, "the absent reading")
	assert.Equal(t, ClusterCacheStats{}, got, "a reading of another cluster's cache")
	testutil.NoRecv(t, stream.Frames, quietWindow, "a frame after the absent reading")
}

// The state watch re-emits only when the measurement moved: it is re-read on a cadence, and
// a cache at rest must not push a frame per tick to every watcher.
func TestCachesWatchStatsStateEmitsOnlyOnAChange(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	store := kubestoreFake(d)
	store.stats = kubestore.Stats{Exists: true, DBBytes: 4096}

	svc := serviceOver(t, d)
	svc.stateWatchCadence = time.Millisecond
	stream, err := svc.Caches().WatchStatsState(context.Background(), clusterIDOf(cluster), ClusterCacheID(cache.ID))
	require.NoError(t, err)
	testutil.Recv(t, stream.Frames, "the first measurement")

	testutil.NoRecv(t, stream.Frames, quietWindow, "a frame for an unchanged cache")

	store.setStats(kubestore.Stats{Exists: true, DBBytes: 8192})

	got := testutil.Recv(t, stream.Frames, "the changed measurement")
	assert.Equal(t, int64(8192), got.Bytes)
}

// --- the clears ---

func TestCachesClearOfAnUnknownCacheIsNotAnError(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)

	got, err := serviceOver(t, d).Caches().Clear(context.Background(), 999)

	require.NoError(t, err)
	assert.Nil(t, got)
}

// A subscriber that arrives after a cache was paused has to hear about it: a verdict
// this subscription never witnessed the transition into is still the verdict.
func TestCachesWatchHealthKeyedStateReportsACacheThatWasAlreadyPaused(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")

	stream, err := serviceOver(t, d).Caches().WatchHealthKeyedState(context.Background())
	require.NoError(t, err)

	got := testutil.Recv(t, stream.Frames, "the paused cache's verdict")
	assert.Equal(t, ClusterCacheID(cache.ID), got.CacheID)
	assert.Equal(t, ConditionFalse, got.Status)
	assert.Equal(t, ReasonPaused, got.Reason)
	assert.Zero(t, got.TotalKinds)
}

// A cache being collected is not a cache that stopped syncing: its record says it is
// going, and a verdict for it would be noise a consumer has to unpick.
func TestCachesWatchHealthKeyedStateSkipsACacheOnItsWayOut(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	require.NoError(t, d.cacheClient.Delete(context.Background(), cache.ID))

	svc := serviceOver(t, d)
	svc.stateWatchCadence = time.Millisecond
	stream, err := svc.Caches().WatchHealthKeyedState(context.Background())
	require.NoError(t, err)

	// A negative assertion needs a bounded window, sized against the shrunk cadence.
	testutil.NoRecv(t, stream.Frames, 50*time.Millisecond, "a verdict for a cache being collected")
}

// A cache with no workers is not necessarily a paused one: an enabled cluster's cache
// has none until its discovery pass has run, and one that serves no kinds never will.
// Calling that Paused would show a user their own enabled cluster as switched off.
func TestCachesWatchHealthKeyedStateTellsAnInitializingCacheFromAPausedOne(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		syncEnabled bool
		want        string
	}{
		"still initializing": {syncEnabled: true, want: ReasonConnecting},
		"switched off":       {syncEnabled: false, want: ReasonPaused},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			d, status := newClusterStatusDeps(t)
			cluster := storedCluster(t, d, status, tt.syncEnabled, "uid-1")
			createCache(t, d.cacheClient, cluster, "uid-1")

			stream, err := serviceOver(t, d).Caches().WatchHealthKeyedState(context.Background())
			require.NoError(t, err)

			got := testutil.Recv(t, stream.Frames, "the workerless cache's verdict")
			assert.Equal(t, ConditionFalse, got.Status)
			assert.Equal(t, tt.want, got.Reason)
		})
	}
}

// --- arming the sync ---

// syncFake is the sync seam a fixture wired, for a test that drives or reads it.
func syncFake(d deps) *fakeKubesync { return d.kubesyncSvc.(*fakeKubesync) }

// writeCatalog is a sweep having landed: the kinds a cluster serves, on disk in the cache's
// own file, which is where the mirror pass reads its desired set.
func writeCatalog(t *testing.T, d deps, cacheID ClusterCacheID, rows ...kubestore.KindRow) {
	t.Helper()
	store, ok, err := d.kubestoreMgr.OpenExisting(int64(cacheID))
	require.NoError(t, err)
	require.True(t, ok)
	defer store.Release()
	require.NoError(t, store.SyncKinds(context.Background(), rows, true, uint64(len(rows)+1)))
}

// kindRow is one served kind as the sweep writes it.
func kindRow(apiVersion, kind, resource string) kubestore.KindRow {
	return kubestore.KindRow{APIVersion: apiVersion, Kind: kind, Resource: resource, Scope: "Namespaced"}
}

// recordedKinds is the set of kind records under a cache, by name.
func recordedKinds(t *testing.T, d deps, cacheID ClusterCacheID) []string {
	t.Helper()
	objs, err := d.kindClient.ListOwnedObjects(context.Background(), beehive.ObjectID(cacheID))
	require.NoError(t, err)
	names := make([]string, 0, len(objs))
	for _, obj := range objs {
		if obj.DeletionRequestedAt == nil {
			names = append(names, obj.Name)
		}
	}
	slices.Sort(names)
	return names
}

// Arming is the cache pass's, off the switch it already computes. Nothing a reader does starts
// a sync, and the params are what every worker under the cache dials over.
func TestCachePassArmsTheSyncWhileTheSwitchHolds(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")

	reconcileCache(t, d, cache)

	params, armed := syncFake(d).armedDiscovery(int64(cache.ID))
	require.True(t, armed, "a cache whose switch holds is armed")
	assert.Equal(t, kubesync.Params{ContextName: "prod", ServerUID: "uid-1"}, params)
}

// Pausing is one call and no record written: the kinds stay registered under kubesync, so a
// resume starts every one of them again with nothing requeued.
func TestCachePassDisarmsTheSyncWhenTheSwitchIsOff(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	reconcileCache(t, d, cache)

	_, err := d.clusterClient.Update(context.Background(), cluster.ID,
		ClusterRuntimeSpec{Enabled: true, SyncEnabled: false, Source: cluster.Spec.Source, SourceKey: cluster.Spec.SourceKey})
	require.NoError(t, err)
	reconcileCache(t, d, cache)

	_, armed := syncFake(d).armedDiscovery(int64(cache.ID))
	assert.False(t, armed, "a paused cache syncs nothing")
}

// A cache whose identity the cluster has moved off is not the one being probed, so nothing
// under it may write.
func TestCachePassDisarmsACacheTheClusterNoLongerAnswersAs(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	reconcileCache(t, d, cache)

	other := "uid-2"
	require.NoError(t, status.UpdateStatus(context.Background(), cluster.ID, ClusterStatus{Server: ClusterServer{UID: &other}}))
	reconcileCache(t, d, cache)

	_, armed := syncFake(d).armedDiscovery(int64(cache.ID))
	assert.False(t, armed, "a cache the cluster has migrated off syncs nothing")
}

// Forgetting returns only once nothing can still write through the cache's store, which is
// exactly what the file being deleted needs.
func TestCachePassForgetsTheSyncBeforeItRemovesTheStore(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	reconcileCache(t, d, cache)

	var armedAtRemoval bool
	kubestoreFake(d).onRemove = func(int64) {
		_, armedAtRemoval = syncFake(d).armedDiscovery(int64(cache.ID))
	}
	require.NoError(t, d.cacheClient.Delete(context.Background(), cache.ID))
	obj, err := d.cacheClient.Get(context.Background(), cache.ID)
	require.NoError(t, err)
	reconcileCache(t, d, obj)

	assert.False(t, armedAtRemoval, "nothing is still syncing when the file goes")
}

// A cache carries its finalizer from creation: without it beehive collects the row
// after a failed reconcile as readily as after a successful one, so a failed file
// removal would lose its retry and the cluster could be collected over an orphaned
// file. The deleting arm clears it only once the file is gone, and a removal that
// fails keeps it — and fails the pass, so beehive retries under backoff.
func TestCacheIsCreatedWithItsFinalizer(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")

	cache := createCache(t, d.cacheClient, cluster, "uid-1")

	assert.Equal(t, []string{cacheFinalizer}, cache.Finalizers)
}

func TestCachePassClearsTheFinalizerOnceTheFileIsGone(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	require.NoError(t, d.cacheClient.Delete(context.Background(), cache.ID))
	obj, err := d.cacheClient.Get(context.Background(), cache.ID)
	require.NoError(t, err)
	client := &cacheControllerClient{}

	res := (&clusterCacheController{deps: d}).Reconcile(context.Background(), client, obj)

	assert.Equal(t, beehive.Settled(), res)
	assert.Equal(t, []int64{int64(cache.ID)}, kubestoreFake(d).removed)
	assert.Equal(t, []string{cacheFinalizer}, client.finalizersCleared)
}

// The clear is a store write of its own; one that fails fails the pass, and the next
// pass finds the file already gone and clears it then.
func TestCachePassReportsAFinalizerThatWillNotClear(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	require.NoError(t, d.cacheClient.Delete(context.Background(), cache.ID))
	obj, err := d.cacheClient.Get(context.Background(), cache.ID)
	require.NoError(t, err)
	client := &cacheControllerClient{finalizerErr: assert.AnError}

	res := (&clusterCacheController{deps: d}).Reconcile(context.Background(), client, obj)

	assert.NotEqual(t, beehive.Settled(), res)
}

func TestCachePassKeepsTheFinalizerWhenTheRemovalFails(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	require.NoError(t, d.cacheClient.Delete(context.Background(), cache.ID))
	obj, err := d.cacheClient.Get(context.Background(), cache.ID)
	require.NoError(t, err)
	kubestoreFake(d).err = assert.AnError
	client := &cacheControllerClient{}

	res := (&clusterCacheController{deps: d}).Reconcile(context.Background(), client, obj)

	assert.NotEqual(t, beehive.Settled(), res)
	assert.Empty(t, client.finalizersCleared, "the finalizer holds the row for the retry")
}

// A cache on its way out is deleted, not paused. Pausing keeps every kind registered so a
// resume is one call; a teardown that kept them would leak a cache's worth on every delete.
func TestCachePassForgetsTheKindsWithTheCache(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	reconcileCache(t, d, cache)
	syncFake(d).TrackKind(int64(cache.ID), kubestore.Kind{APIVersion: "v1", Kind: "Pod", Resource: "pods"})

	require.NoError(t, d.cacheClient.Delete(context.Background(), cache.ID))
	obj, err := d.cacheClient.Get(context.Background(), cache.ID)
	require.NoError(t, err)
	reconcileCache(t, d, obj)

	assert.Empty(t, syncFake(d).armedKinds(int64(cache.ID)),
		"nothing under a deleted cache stays registered")
}

// --- mirroring the catalog ---

// The desired set is what the cluster serves, read back off disk; a record is what turns one
// of those into a kind that is actually mirrored.
func TestCachePassMirrorsTheCatalogIntoRecords(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	reconcileCache(t, d, cache)
	writeCatalog(t, d, ClusterCacheID(cache.ID),
		kindRow("v1", "Pod", "pods"), kindRow("apps/v1", "Deployment", "deployments"))

	reconcileCache(t, d, cache)

	assert.Equal(t, []string{
		ClusterCachedKindName(cache.ID, "apps/v1", "deployments"),
		ClusterCachedKindName(cache.ID, "v1", "pods"),
	}, recordedKinds(t, d, ClusterCacheID(cache.ID)))
}

// A kind's spec carries data outside its name — the singular, and whether it is namespaced —
// so a renamed or re-scoped kind converges in place rather than being recreated under a name
// it already holds.
func TestCachePassConvergesARenamedKindInPlace(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	reconcileCache(t, d, cache)
	writeCatalog(t, d, ClusterCacheID(cache.ID), kindRow("v1", "Pod", "pods"))
	reconcileCache(t, d, cache)

	writeCatalog(t, d, ClusterCacheID(cache.ID), kindRow("v1", "PodThing", "pods"))
	reconcileCache(t, d, cache)

	obj, err := d.kindClient.GetByName(context.Background(), ClusterCachedKindName(cache.ID, "v1", "pods"))
	require.NoError(t, err)
	assert.Equal(t, "PodThing", obj.Spec.Kind, "the singular converges under the name it already holds")
}

// **The catalog owns four fields; the user owns one.** The sweep writes on a schedule, so
// a pass that wrote the whole desired spec would un-pause a kind within one discovery
// interval — and silently, since nothing else moves.
func TestCachePassLeavesAPausedKindPaused(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	reconcileCache(t, d, cache)
	writeCatalog(t, d, ClusterCacheID(cache.ID), kindRow("v1", "Pod", "pods"))
	reconcileCache(t, d, cache)
	pauseKind(t, d, cache.ID, "v1", "pods")

	reconcileCache(t, d, cache)

	assert.True(t, storedKind(t, d, cache.ID, "v1", "pods").Spec.Paused,
		"the sweep writes nothing when no catalog field moved")
}

// The other half of the ownership split: a catalog change is the sweep's to converge, and
// it converges the four fields it owns WITHOUT taking the fifth back.
func TestCachePassCarriesAPauseThroughACatalogChange(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	reconcileCache(t, d, cache)
	writeCatalog(t, d, ClusterCacheID(cache.ID), kindRow("v1", "Pod", "pods"))
	reconcileCache(t, d, cache)
	pauseKind(t, d, cache.ID, "v1", "pods")

	writeCatalog(t, d, ClusterCacheID(cache.ID), kindRow("v1", "PodThing", "pods"))
	reconcileCache(t, d, cache)

	obj := storedKind(t, d, cache.ID, "v1", "pods")
	assert.Equal(t, "PodThing", obj.Spec.Kind, "the singular converges")
	assert.True(t, obj.Spec.Paused, "and the user's switch rides through it")
}

// The pass lists the records BEFORE it takes the lock, so a pause landing in that window is
// already stored and missing from the snapshot. The carry-forward has to read the record
// rather than the snapshot, or a catalog change arriving alongside writes the user's
// mutation away — with both writers ostensibly sharing the mutex.
//
// Driven through upsertKinds directly, with the snapshot taken before the pause: that is
// the race's outcome, without a race to reproduce.
func TestCachePassCarriesAPauseItsSnapshotMissed(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	reconcileCache(t, d, cache)
	writeCatalog(t, d, ClusterCacheID(cache.ID), kindRow("v1", "Pod", "pods"))
	reconcileCache(t, d, cache)

	ctx := context.Background()
	c := &clusterCacheController{deps: d}
	stale, err := c.storedKinds(ctx, cache.ID)
	require.NoError(t, err)
	pauseKind(t, d, cache.ID, "v1", "pods")

	name := ClusterCachedKindName(cache.ID, "v1", "pods")
	desired := map[string]ClusterCachedKindSpec{
		name: {APIVersion: "v1", Kind: "PodThing", Resource: "pods", Namespaced: true},
	}
	require.NoError(t, c.upsertKinds(ctx, cache.ID, desired, stale))

	obj := storedKind(t, d, cache.ID, "v1", "pods")
	assert.Equal(t, "PodThing", obj.Spec.Kind, "the singular converges")
	assert.True(t, obj.Spec.Paused, "and the pause the snapshot missed rides through it")
}

// pauseKind sets one record's switch the way the mutation will, so the sweep tests can
// stand a pause up before any setter exists.
func pauseKind(t *testing.T, d deps, cacheID beehive.ObjectID, apiVersion, resource string) {
	t.Helper()
	obj := storedKind(t, d, cacheID, apiVersion, resource)
	spec := obj.Spec
	spec.Paused = true
	_, err := d.kindClient.Update(context.Background(), obj.ID, spec)
	require.NoError(t, err)
}

// storedKind reads one kind record back by the name the sweep gives it.
func storedKind(t *testing.T, d deps, cacheID beehive.ObjectID, apiVersion, resource string) *beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus] {
	t.Helper()
	obj, err := d.kindClient.GetByName(context.Background(), ClusterCachedKindName(cacheID, apiVersion, resource))
	require.NoError(t, err)
	return obj
}

// A record with no row is a kind the cluster has stopped serving. Marked, not collected: the
// record's own pass is what clears the rows behind it.
func TestCachePassMarksARecordWithNoRow(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	reconcileCache(t, d, cache)
	writeCatalog(t, d, ClusterCacheID(cache.ID), kindRow("v1", "Pod", "pods"), kindRow("v1", "Node", "nodes"))
	reconcileCache(t, d, cache)

	writeCatalog(t, d, ClusterCacheID(cache.ID), kindRow("v1", "Pod", "pods"))
	reconcileCache(t, d, cache)

	assert.Equal(t, []string{ClusterCachedKindName(cache.ID, "v1", "pods")},
		recordedKinds(t, d, ClusterCacheID(cache.ID)))
}

// **"Never swept" is not "serves nothing."** A table with no fingerprint has never had an
// answer written to it, and only the first of those may delete records — otherwise a cluster
// that is merely unreachable loses its whole record set for the length of the outage.
func TestCachePassPrunesNothingBeforeAnySweep(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	reconcileCache(t, d, cache)
	writeCatalog(t, d, ClusterCacheID(cache.ID), kindRow("v1", "Pod", "pods"))
	reconcileCache(t, d, cache)

	// The file goes and comes back empty, which is what a clear leaves behind.
	require.NoError(t, d.kubestoreMgr.Clear(int64(cache.ID)))
	reconcileCache(t, d, cache)

	assert.Equal(t, []string{ClusterCachedKindName(cache.ID, "v1", "pods")},
		recordedKinds(t, d, ClusterCacheID(cache.ID)), "an unswept table deletes nothing")
}

// A pass that runs before any sweep finds no file rather than a fresh empty one, so it reads
// no rows and prunes nothing — and creates nothing the cache's teardown would have to undo.
func TestCachePassReadsNoCatalogWhenThereIsNoFile(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	kubestoreFake(d).noFile = true

	reconcileCache(t, d, cache)

	assert.Empty(t, recordedKinds(t, d, ClusterCacheID(cache.ID)))
}

// --- the discovery timeline ---

// A verdict that moved is an event on the cache's own timeline. Repeating a run's
// (Category, Type, Reason) extends that run rather than appending, so a flapping sweep costs
// one row per transition.
func TestCachePassLogsTheDiscoveryVerdict(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	syncFake(d).setDiscoveryState(int64(cache.ID), kubesync.DiscoveryState{
		Reason: kubesync.ReasonDiscoveryFailed, Message: "the /apis document would not load",
	})

	client := reconcileCache(t, d, cache)

	require.Len(t, client.events, 1)
	assert.Equal(t, categoryDiscovery, client.events[0].Category)
	assert.Equal(t, kubesync.ReasonDiscoveryFailed, client.events[0].Reason)
	assert.Equal(t, "the /apis document would not load", client.events[0].Message)
}

// A store failure is THIS cache's, not the cluster's, so it passes the guard that holds back
// NoConnection and IdentityMismatch and lands on the timeline. A guard rather than a red test:
// logDiscoveryVerdict needs no change for it, and this is what keeps it that way.
func TestCachePassLogsAStoreFailure(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	syncFake(d).setDiscoveryState(int64(cache.ID), kubesync.DiscoveryState{
		Reason: kubesync.ReasonStoreFailed, Message: "disk full",
	})

	client := reconcileCache(t, d, cache)

	require.Len(t, client.events, 1)
	assert.Equal(t, kubesync.ReasonStoreFailed, client.events[0].Reason)
	assert.Equal(t, "disk full", client.events[0].Message)
}

// A suspended session is the CLUSTER's fact, already on its own timeline; logging it per cache
// is the same news twice.
func TestCachePassLogsNoDiscoveryEventForALostConnection(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	syncFake(d).setDiscoveryState(int64(cache.ID), kubesync.DiscoveryState{Reason: kubesync.ReasonNoConnection})

	client := reconcileCache(t, d, cache)

	assert.Empty(t, client.events)
}

// Nothing has answered for a cache whose sweep has not run, and that is not a verdict to log.
func TestCachePassLogsNothingBeforeASweepAnswers(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")

	client := reconcileCache(t, d, cache)

	assert.Empty(t, client.events)
}

// A clear swaps the file under whoever holds it open, so the workers writing through it must
// be down for the whole swap. Only kubesync can stop them, so the clear runs inside its hold.
func TestCachesClearRunsInsideTheSyncsHold(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")

	var heldAtClear bool
	kubestoreFake(d).onClear = func(int64) {
		heldAtClear = slices.Contains(syncFake(d).stoppedCaches(), int64(cache.ID))
	}
	_, err := serviceOver(t, d).Caches().Clear(context.Background(), ClusterCacheID(cache.ID))

	require.NoError(t, err)
	assert.True(t, heldAtClear, "the cache's workers are stopped across the swap")
}

// The user's clear throws the cache's lines away too, since logs hold secrets more often
// than object bodies do. The log file needs no sync stopped, so its clear runs before the
// hold the mirror's runs in.
func TestCachesClearClearsTheLogFileFirstOutsideTheSyncsHold(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	var order []string
	kubestoreFake(d).onClear = func(int64) { order = append(order, "mirror") }
	heldAtClear := true
	files := &fakeCacheFiles{onClear: func(int64) {
		order = append(order, "logs")
		heldAtClear = syncFake(d).inHold()
	}}
	d.cacheFiles.set(files)

	_, err := serviceOver(t, d).Caches().Clear(context.Background(), ClusterCacheID(cache.ID))

	require.NoError(t, err)
	assert.Equal(t, []string{"logs", "mirror"}, order)
	assert.Equal(t, []int64{int64(cache.ID)}, files.cleared)
	assert.False(t, heldAtClear, "the log file clears outside the sync's hold")
}

// A log file that will not clear fails the mutation before the mirror is touched, so the
// error is the whole truth and the user runs it again.
func TestCachesClearReportsALogFileThatWillNotClear(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	d.cacheFiles.set(&fakeCacheFiles{err: assert.AnError})

	_, err := serviceOver(t, d).Caches().Clear(context.Background(), ClusterCacheID(cache.ID))

	assert.ErrorIs(t, err, assert.AnError)
	assert.Empty(t, kubestoreFake(d).clearedCaches, "the mirror is left as it was")
}

// --- the health fold ---

// syncingCacheWithKinds arms a cache with two kinds and hands back the ids, so a health test
// says only what it is about.
func syncingCacheWithKinds(t *testing.T) (deps, ClusterCacheID) {
	t.Helper()
	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	createKind(t, d, cache.ID, podsSpec)
	createKind(t, d, cache.ID, deploymentsSpec)
	return d, ClusterCacheID(cache.ID)
}

// watchSyncState opens the sync-detail state watch and ends it before the test's temp dir goes:
// the pump reopens the cache's file every tick, and Windows removes no open file.
func watchSyncState(t *testing.T, svc *service, clusterID ClusterID, cacheID ClusterCacheID) *Stream[ClusterCacheSyncState] {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := svc.Caches().WatchSyncState(ctx, clusterID, cacheID)
	require.NoError(t, err)
	t.Cleanup(func() {
		cancel()
		testutil.WaitClosed(t, stream.Frames, "the state watch to stop")
	})
	return stream
}

// setKindReason is one kind's worker having settled somewhere.
func setKindReason(d deps, cacheID ClusterCacheID, spec ClusterCachedKindSpec, state kubesync.KindState) {
	syncFake(d).setKindState(int64(cacheID), toKubestoreKind(spec), state)
}

// The verdict a cache stands behind is every kind under it folded together: ninety-nine
// healthy kinds and one forbidden CRD is not healthy, which is why no single child can serve
// this and why the fold happens here.
func TestCachesWatchHealthKeyedStateFoldsEveryKindsVerdict(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	now := time.Now()
	setKindReason(d, cacheID, podsSpec, kubesync.KindState{
		Reason: kubesync.ReasonWatching, Live: true, LastUpdateAt: now, LastLiveAt: now,
	})
	setKindReason(d, cacheID, deploymentsSpec, kubesync.KindState{
		Reason: kubesync.ReasonSyncFailed, Failure: kubeconn.ReasonForbidden, LastLiveAt: now.Add(-time.Hour),
	})

	stream, err := serviceOver(t, d).Caches().WatchHealthKeyedState(context.Background())
	require.NoError(t, err)

	got := testutil.Recv(t, stream.Frames, "the cache's folded verdict")
	assert.Equal(t, ConditionFalse, got.Status)
	assert.Equal(t, kubesync.ReasonSyncFailed, got.Reason)
	assert.Equal(t, "Forbidden", got.Failure, "the first offender's class of failure")
	assert.Equal(t, 2, got.TotalKinds)
	assert.Equal(t, 1, got.UnhealthyKinds)
	assert.Equal(t, []UnhealthyKindRef{
		{KindRef: KindRef{APIVersion: "apps/v1", Resource: "deployments"}, Reason: kubesync.ReasonSyncFailed, Failure: "Forbidden"},
	}, got.UnhealthyKindRefs, "each offender carries its own class of failure")
	assert.Equal(t, []KindRef{kindRefOf(podsSpec)}, got.WatchingKindRefs)
	assert.Empty(t, got.PausedKindRefs)
}

// Each offender carries its own reason, so a consumer can name only the kinds behind the
// cache's verdict: a kind still on its cold list is not stale, and not failing either.
func TestCachesWatchHealthKeyedStateGivesEachOffenderItsOwnReason(t *testing.T) {
	t.Parallel()

	for _, verdict := range []string{kubesync.ReasonStale, kubesync.ReasonSyncFailed} {
		t.Run(verdict, func(t *testing.T) {
			d, cacheID := syncingCacheWithKinds(t)
			setKindReason(d, cacheID, deploymentsSpec, kubesync.KindState{Reason: verdict})
			setKindReason(d, cacheID, podsSpec, kubesync.KindState{Reason: kubesync.ReasonSyncing})

			stream, err := serviceOver(t, d).Caches().WatchHealthKeyedState(t.Context())
			require.NoError(t, err)

			got := testutil.Recv(t, stream.Frames, "the cache's folded verdict")
			assert.Equal(t, verdict, got.Reason)
			assert.Equal(t, []UnhealthyKindRef{
				{KindRef: KindRef{APIVersion: "apps/v1", Resource: "deployments"}, Reason: verdict},
				{KindRef: KindRef{APIVersion: "v1", Resource: "pods"}, Reason: kubesync.ReasonSyncing},
			}, got.UnhealthyKindRefs)
		})
	}
}

// The class is part of the verdict: a failure changing class is news on its own.
func TestCachesWatchHealthKeyedStateSendsAFailureThatChangesClass(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	setKindReason(d, cacheID, podsSpec, kubesync.KindState{Reason: kubesync.ReasonWatching, LastLiveAt: probedAt})
	setKindReason(d, cacheID, deploymentsSpec, kubesync.KindState{Reason: kubesync.ReasonSyncFailed, Failure: kubeconn.ReasonTimeout})
	svc := serviceOver(t, d)
	svc.stateWatchCadence = time.Millisecond

	stream, err := svc.Caches().WatchHealthKeyedState(t.Context())
	require.NoError(t, err)
	require.Equal(t, "Timeout", testutil.Recv(t, stream.Frames, "the verdict").Failure)

	setKindReason(d, cacheID, deploymentsSpec, kubesync.KindState{Reason: kubesync.ReasonSyncFailed, Failure: kubeconn.ReasonForbidden})
	assert.Equal(t, "Forbidden", testutil.Recv(t, stream.Frames, "the verdict with its new class").Failure)
}

// Every kind watching is the one verdict a cache can report as healthy.
func TestCachesWatchHealthKeyedStateReportsAFullySyncedCache(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	now := time.Now()
	for _, spec := range []ClusterCachedKindSpec{podsSpec, deploymentsSpec} {
		setKindReason(d, cacheID, spec, kubesync.KindState{
			Reason: kubesync.ReasonWatching, Live: true, LastUpdateAt: now, LastLiveAt: now,
		})
	}

	stream, err := serviceOver(t, d).Caches().WatchHealthKeyedState(context.Background())
	require.NoError(t, err)

	got := testutil.Recv(t, stream.Frames, "the healthy cache's verdict")
	assert.Equal(t, ConditionTrue, got.Status)
	assert.Equal(t, kubesync.ReasonWatching, got.Reason)
	assert.Empty(t, got.UnhealthyKindRefs)
	require.NotNil(t, got.LastUpdateAt)
	require.NotNil(t, got.LastLiveAt)
}

// LastUpdateAt is the most recent write anywhere in the cache; LastLiveAt the OLDEST proof
// across every kind, because a cache is only as verified as its least proven watch.
func TestCachesWatchHealthKeyedStateTakesTheNewestUpdateAndTheOldestProof(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	now := time.Now().Truncate(time.Second)
	older, newer := now.Add(-time.Hour), now
	setKindReason(d, cacheID, podsSpec, kubesync.KindState{
		Reason: kubesync.ReasonWatching, Live: true, LastUpdateAt: older, LastLiveAt: older,
	})
	setKindReason(d, cacheID, deploymentsSpec, kubesync.KindState{
		Reason: kubesync.ReasonWatching, Live: true, LastUpdateAt: newer, LastLiveAt: newer,
	})

	stream, err := serviceOver(t, d).Caches().WatchHealthKeyedState(context.Background())
	require.NoError(t, err)

	got := testutil.Recv(t, stream.Frames, "the cache's stamps")
	require.NotNil(t, got.LastUpdateAt)
	require.NotNil(t, got.LastLiveAt)
	assert.True(t, got.LastUpdateAt.Equal(newer), "the newest write anywhere in the cache")
	assert.True(t, got.LastLiveAt.Equal(older), "the least proven watch")
}

// A kind nothing has proven live is weaker than any stamp its neighbours carry, so the cache
// reports no proof at all rather than one kind's.
func TestCachesWatchHealthKeyedStateReportsNoProofWhileAKindHasNone(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	now := time.Now()
	setKindReason(d, cacheID, podsSpec, kubesync.KindState{
		Reason: kubesync.ReasonWatching, Live: true, LastUpdateAt: now, LastLiveAt: now,
	})
	setKindReason(d, cacheID, deploymentsSpec, kubesync.KindState{Reason: kubesync.ReasonSyncing})

	stream, err := serviceOver(t, d).Caches().WatchHealthKeyedState(context.Background())
	require.NoError(t, err)

	got := testutil.Recv(t, stream.Frames, "the cache's stamps")
	assert.Nil(t, got.LastLiveAt)
}

// The same, whichever kind the list reaches first: a missing proof is a fact about the cache,
// not a value the next kind's stamp can outrank.
func TestCachesWatchHealthKeyedStateReportsNoProofWhenTheUnprovenKindIsFirst(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	now := time.Now()
	setKindReason(d, cacheID, podsSpec, kubesync.KindState{Reason: kubesync.ReasonSyncing})
	setKindReason(d, cacheID, deploymentsSpec, kubesync.KindState{
		Reason: kubesync.ReasonWatching, Live: true, LastUpdateAt: now, LastLiveAt: now,
	})

	stream, err := serviceOver(t, d).Caches().WatchHealthKeyedState(context.Background())
	require.NoError(t, err)

	got := testutil.Recv(t, stream.Frames, "the cache's stamps")
	assert.Nil(t, got.LastLiveAt)
}

// A kind that has answered nothing has proved nothing either, so it withholds the cache's
// proof exactly as an answered-but-unproven one does.
func TestCachesWatchHealthKeyedStateReportsNoProofWhileAKindHasNotAnswered(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	now := time.Now()
	setKindReason(d, cacheID, podsSpec, kubesync.KindState{
		Reason: kubesync.ReasonWatching, Live: true, LastUpdateAt: now, LastLiveAt: now,
	})

	stream, err := serviceOver(t, d).Caches().WatchHealthKeyedState(context.Background())
	require.NoError(t, err)

	got := testutil.Recv(t, stream.Frames, "the cache's stamps")
	assert.Nil(t, got.LastLiveAt)
}

// **No answer is not an empty answer.** A kind whose worker has committed nothing — a clear
// in progress, a cache still starting — is not a kind that stopped syncing, so the cache
// reads as still connecting rather than as unhealthy.
func TestCachesWatchHealthKeyedStateReadsAKindWithNoAnswerAsConnecting(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	setKindReason(d, cacheID, podsSpec, kubesync.KindState{Reason: kubesync.ReasonWatching, Live: true})

	stream, err := serviceOver(t, d).Caches().WatchHealthKeyedState(context.Background())
	require.NoError(t, err)

	got := testutil.Recv(t, stream.Frames, "the cache's verdict")
	assert.Equal(t, ReasonConnecting, got.Reason)
	assert.Empty(t, got.UnhealthyKindRefs, "a kind that has not answered is not an offender")
	assert.Equal(t, []KindRef{kindRefOf(deploymentsSpec)}, got.UnansweredKindRefs)
}

// Every kind that has not answered is named, sorted, and a kind that answers leaves
// the list. A tick may land between the two answers and send the list half-emptied,
// so the test reads until it is empty: the verdict moves once both have answered.
func TestCachesWatchHealthKeyedStateNamesEveryKindThatHasNotAnswered(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	svc := serviceOver(t, d)
	svc.stateWatchCadence = time.Millisecond

	stream, err := svc.Caches().WatchHealthKeyedState(t.Context())
	require.NoError(t, err)

	got := testutil.Recv(t, stream.Frames, "the cache's verdict")
	assert.Equal(t, []KindRef{kindRefOf(deploymentsSpec), kindRefOf(podsSpec)}, got.UnansweredKindRefs)

	setKindReason(d, cacheID, podsSpec, kubesync.KindState{Reason: kubesync.ReasonSyncing})
	setKindReason(d, cacheID, deploymentsSpec, kubesync.KindState{Reason: kubesync.ReasonSyncing})
	got = testutil.Recv(t, stream.Frames, "the verdict once a kind answered")
	if len(got.UnansweredKindRefs) > 0 {
		assert.Equal(t, []KindRef{kindRefOf(deploymentsSpec)}, got.UnansweredKindRefs)
		got = testutil.Recv(t, stream.Frames, "the verdict once both answered")
	}
	assert.Equal(t, kubesync.ReasonSyncing, got.Reason)
	assert.Empty(t, got.UnansweredKindRefs)
}

// **A paused kind is skipped, not folded.** It never reaches GetKindState, so it can never
// be read as unanswered — and an unanswered kind pins the whole cache at Connecting, which
// is what would leave a user's own deliberate pause looking like a cache that never started.
//
// It still counts in totalKinds: that field has a documented meaning and three consumers, so
// the paused ones are tallied separately rather than subtracted out of the census.
func TestCachesWatchHealthKeyedStateSkipsAPausedKindWithoutLosingItFromTheCensus(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	setKindReason(d, cacheID, podsSpec, kubesync.KindState{
		Reason: kubesync.ReasonWatching, LastLiveAt: probedAt,
	})
	pauseKind(t, d, beehive.ObjectID(cacheID), deploymentsSpec.APIVersion, deploymentsSpec.Resource)

	stream, err := serviceOver(t, d).Caches().WatchHealthKeyedState(context.Background())
	require.NoError(t, err)

	got := testutil.Recv(t, stream.Frames, "the cache's verdict")
	assert.Equal(t, kubesync.ReasonWatching, got.Reason, "the paused kind withholds nothing")
	assert.Equal(t, 2, got.TotalKinds, "the census keeps it")
	assert.Equal(t, 1, got.PausedKinds)
	assert.Equal(t, 0, got.UnhealthyKinds, "a paused kind is not unhealthy")
	assert.Empty(t, got.UnansweredKindRefs, "a paused kind is not unanswered")
	assert.Equal(t, []KindRef{kindRefOf(deploymentsSpec)}, got.PausedKindRefs, "a paused kind is named as paused alone")
	assert.Equal(t, []KindRef{kindRefOf(podsSpec)}, got.WatchingKindRefs)
}

// The state watch dedupes per cache against a hand-written comparison, so a field nothing
// compares is invisible by default — and pausing one kind on an otherwise-idle healthy
// cache moves NOTHING ELSE: the census holds, the paused kind is skipped so there are no
// offenders, and status and reason sit still. The frame would be suppressed and the count
// would never reach a client.
func TestCachesWatchHealthKeyedStatePublishesWhenAKindIsPaused(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	setKindReason(d, cacheID, podsSpec, kubesync.KindState{Reason: kubesync.ReasonWatching, LastLiveAt: probedAt})
	setKindReason(d, cacheID, deploymentsSpec, kubesync.KindState{Reason: kubesync.ReasonWatching, LastLiveAt: probedAt})

	svc := serviceOver(t, d)
	svc.stateWatchCadence = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := svc.Caches().WatchHealthKeyedState(ctx)
	require.NoError(t, err)
	defer func() {
		cancel()
		testutil.WaitClosed(t, stream.Frames, "the state watch to stop")
	}()
	require.Equal(t, kubesync.ReasonWatching, testutil.Recv(t, stream.Frames, "the healthy verdict").Reason)

	pauseKind(t, d, beehive.ObjectID(cacheID), deploymentsSpec.APIVersion, deploymentsSpec.Resource)

	assert.Equal(t, 1, testutil.Recv(t, stream.Frames, "the verdict after the pause").PausedKinds)
}

// The arm ordering, which is the easy thing to get wrong. Paused kinds are skipped, so a
// fully paused cache has no offenders and nothing unanswered — every later arm would call
// it healthy, and the badge would read Watching over a cache syncing nothing.
func TestCachesWatchHealthKeyedStateReadsAFullyPausedCacheAsPaused(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	pauseKind(t, d, beehive.ObjectID(cacheID), podsSpec.APIVersion, podsSpec.Resource)
	pauseKind(t, d, beehive.ObjectID(cacheID), deploymentsSpec.APIVersion, deploymentsSpec.Resource)

	stream, err := serviceOver(t, d).Caches().WatchHealthKeyedState(context.Background())
	require.NoError(t, err)

	got := testutil.Recv(t, stream.Frames, "the cache's verdict")
	assert.Equal(t, ConditionFalse, got.Status)
	assert.Equal(t, ReasonPaused, got.Reason)
	assert.Equal(t, 2, got.PausedKinds)
}

// A cache whose file will not open has no session behind it, so every kind reads as
// unanswered and the per-kind fold sits at Connecting for good — an amber badge with no
// reason anywhere. The store's verdict is the cache's own, and it decides above the fold.
func TestCachesWatchHealthKeyedStateReportsACacheWhoseStoreWillNotOpen(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	syncFake(d).setDiscoveryState(int64(cacheID), kubesync.DiscoveryState{
		Reason: kubesync.ReasonStoreFailed, Message: "disk full",
	})

	stream, err := serviceOver(t, d).Caches().WatchHealthKeyedState(context.Background())
	require.NoError(t, err)

	got := testutil.Recv(t, stream.Frames, "the cache's verdict")
	assert.Equal(t, ConditionFalse, got.Status)
	assert.Equal(t, kubesync.ReasonStoreFailed, got.Reason)
}

// A context that reaches another server is the cache's verdict before any kind's: the
// kinds have not answered yet, and Connecting would answer from the other cluster's rows.
func TestCachesWatchHealthKeyedStateReportsAnIdentityMismatchBeforeAnyKindAnswers(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	syncFake(d).setDiscoveryState(int64(cacheID), kubesync.DiscoveryState{Reason: kubesync.ReasonIdentityMismatch})

	stream, err := serviceOver(t, d).Caches().WatchHealthKeyedState(context.Background())
	require.NoError(t, err)

	got := testutil.Recv(t, stream.Frames, "the cache's verdict")
	assert.Equal(t, ConditionFalse, got.Status)
	assert.Equal(t, kubesync.ReasonIdentityMismatch, got.Reason)
	assert.False(t, got.Trusted())
	assert.Equal(t, []KindRef{kindRefOf(deploymentsSpec), kindRefOf(podsSpec)}, got.UnansweredKindRefs)
}

// --- the per-cache sync detail ---

// The detail state watch is the only thing on the wire that can carry a per-kind verdict, so it
// serves the discovery reason and a row per mirrored kind together.
func TestCachesWatchSyncStateServesTheDiscoveryReasonAndEveryKind(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	syncFake(d).setDiscoveryState(int64(cacheID), kubesync.DiscoveryState{
		Reason: kubesync.ReasonPartial, Message: "metrics.k8s.io/v1beta1 would not load",
	})
	setKindReason(d, cacheID, podsSpec, kubesync.KindState{Reason: kubesync.ReasonWatching, Live: true})
	setKindReason(d, cacheID, deploymentsSpec, kubesync.KindState{
		Reason: kubesync.ReasonSyncFailed, Message: "forbidden", Restarts: 3,
	})

	stream := watchSyncState(t, serviceOver(t, d), ownerClusterOf(t, d, cacheID), cacheID)

	got := testutil.Recv(t, stream.Frames, "the cache's sync detail")
	assert.Equal(t, kubesync.ReasonPartial, got.Discovery.Reason)
	assert.Equal(t, "metrics.k8s.io/v1beta1 would not load", got.Discovery.Message)
	require.Len(t, got.Kinds, 2)
	assert.Equal(t, "apps/v1", got.Kinds[0].APIVersion)
	assert.Equal(t, kubesync.ReasonSyncFailed, got.Kinds[0].Reason)
	assert.Equal(t, 3, got.Kinds[0].Restarts)
	assert.Equal(t, "v1", got.Kinds[1].APIVersion)
	assert.Equal(t, kubesync.ReasonWatching, got.Kinds[1].Reason)
}

// The count comes off the store, never off the seam: kubesync knows only the caches it has
// armed, where kubestore answers for a paused one too.
// The panel's own report of the failure. A guard, like the timeline one: the projection is
// reason-agnostic, and this is what stops a future narrowing from dropping the one verdict
// that has no kind to speak through.
func TestCachesWatchSyncStateCarriesAStoreFailure(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	syncFake(d).setDiscoveryState(int64(cacheID), kubesync.DiscoveryState{
		Reason: kubesync.ReasonStoreFailed, Message: "disk full",
	})

	stream := watchSyncState(t, serviceOver(t, d), ownerClusterOf(t, d, cacheID), cacheID)

	got := testutil.Recv(t, stream.Frames, "the cache's sync detail")
	assert.Equal(t, kubesync.ReasonStoreFailed, got.Discovery.Reason)
	assert.Equal(t, "disk full", got.Discovery.Message)
}

// The panel's list is where a user looks for a per-kind reason, and a paused kind has no
// worker to ask. **ObjectCount still answers**, since it comes off the store's counts and
// not the sync seam — which is the point of keeping the rows.
func TestCachesWatchSyncStateReadsAPausedKindOffItsRecord(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	writeCatalog(t, d, cacheID, kindRow("v1", "Pod", "pods"))
	pauseKind(t, d, beehive.ObjectID(cacheID), "v1", "pods")

	stream := watchSyncState(t, serviceOver(t, d), ownerClusterOf(t, d, cacheID), cacheID)

	got := testutil.Recv(t, stream.Frames, "the cache's sync detail")
	assert.True(t, got.Exists, "a live cache's detail")
	require.Len(t, got.Kinds, 2)
	assert.Equal(t, ReasonPaused, got.Kinds[1].Reason, "the reason comes off the record, not the seam")
	assert.Equal(t, 0, got.Kinds[1].ObjectCount, "and the count still answers")
}

// The state watch re-emits only when the detail moved: it is re-read on a cadence, and an idle
// cache pushing a frame per tick would re-render the panel over an answer nothing changed.
func TestCachesWatchSyncStateEmitsOnlyOnAChange(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	setKindReason(d, cacheID, podsSpec, kubesync.KindState{Reason: kubesync.ReasonWatching, Live: true})

	svc := serviceOver(t, d)
	svc.stateWatchCadence = time.Millisecond
	stream := watchSyncState(t, svc, ownerClusterOf(t, d, cacheID), cacheID)
	testutil.Recv(t, stream.Frames, "the first detail")

	// A negative assertion has no event to wait for, so it takes a bounded window — many
	// cadences wide, and failing the moment a second identical frame arrives.
	testutil.NoRecv(t, stream.Frames, quietWindow, "a frame for an unchanged cache")

	setKindReason(d, cacheID, deploymentsSpec, kubesync.KindState{
		Reason: kubesync.ReasonSyncFailed, Message: "forbidden",
	})

	got := testutil.Recv(t, stream.Frames, "the changed detail")
	assert.Equal(t, kubesync.ReasonSyncFailed, got.Kinds[0].Reason)
}

func TestCachesWatchSyncStateCountsRowsFromTheStore(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	writeCatalog(t, d, cacheID, kindRow("v1", "Pod", "pods"))

	stream := watchSyncState(t, serviceOver(t, d), ownerClusterOf(t, d, cacheID), cacheID)

	got := testutil.Recv(t, stream.Frames, "the cache's sync detail")
	assert.True(t, got.Exists, "a live cache's detail")
	require.Len(t, got.Kinds, 2)
	assert.Equal(t, 0, got.Kinds[1].ObjectCount, "a kind the catalog knows and nothing has cached")
}

// A pair naming no live cache gets one reading that says so, the way the stats state watch
// does, and nothing after it.
func TestCachesWatchSyncStateSendsOneAbsentReadingForAMismatchedPair(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)

	stream := watchSyncState(t, serviceOver(t, d), ClusterID("999"), cacheID)

	got := testutil.Recv(t, stream.Frames, "the absent reading")
	assert.False(t, got.Exists, "a cache that is not this cluster's")
	assert.Empty(t, got.Kinds)
	testutil.NoRecv(t, stream.Frames, 50*time.Millisecond, "a frame after the absent reading")
}

// **A relayed value needs a depends_on edge; the owner edge is not one.** The switch this pass
// reads lives on the cluster, and owning a child wakes nothing — so without this a paused
// cluster's cache keeps syncing until something unrelated wakes it.
func TestCachePassDependsOnTheClusterWhoseSwitchItReads(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")

	client := reconcileCache(t, d, cache)

	assert.Equal(t, []beehive.ObjectID{cluster.ID}, client.dependencies)
}

// Two stamps are the same when both are absent or both name the same instant — a pointer
// comparison would call two equal times different and re-send the state watch every tick.
func TestSameTimeComparesTheInstantNotThePointer(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC)
	same := at

	assert.True(t, sameTime(&at, &same))
	assert.True(t, sameTime(nil, nil))
	assert.False(t, sameTime(&at, nil))
}

// Kinds are ordered by apiVersion and then resource, so a rendered list holds still
// between ticks rather than reshuffling on whatever order the store returned.
func TestKindRefsSortByApiVersionThenResource(t *testing.T) {
	t.Parallel()

	refs := []KindRef{
		{APIVersion: "v1", Resource: "pods"},
		{APIVersion: "apps/v1", Resource: "statefulsets"},
		{APIVersion: "apps/v1", Resource: "deployments"},
	}

	slices.SortFunc(refs, compareKindRefs)

	assert.Equal(t, []KindRef{
		{APIVersion: "apps/v1", Resource: "deployments"},
		{APIVersion: "apps/v1", Resource: "statefulsets"},
		{APIVersion: "v1", Resource: "pods"},
	}, refs)
}

// The three steps a live cache's pass makes, each failed in turn. A pass that swallowed
// one would settle the generation over work that did not happen, and beehive would not
// come back to it — the cache would sit unarmed until something else moved.
func TestCacheReconcileFailsOnAnyStepThatDoesNot(t *testing.T) {
	t.Parallel()

	steps := map[string]func(d *deps, client *cacheControllerClient, cacheID int64){
		"arming the sync": func(_ *deps, client *cacheControllerClient, _ int64) {
			client.dependErr = assert.AnError
		},
		"mirroring the kinds": func(d *deps, _ *cacheControllerClient, _ int64) {
			d.kubestoreMgr.(*fakeKubestore).err = assert.AnError
		},
		"logging the verdict": func(d *deps, client *cacheControllerClient, cacheID int64) {
			// A sweep that has answered, so there is a verdict to write at all.
			d.kubesyncSvc.(*fakeKubesync).setDiscoveryState(cacheID, kubesync.DiscoveryState{
				Reason: kubesync.ReasonDiscovered,
			})
			client.eventErr = assert.AnError
		},
	}

	for name, breakStep := range steps {
		t.Run(name, func(t *testing.T) {
			d, status := newClusterStatusDeps(t)
			cluster := storedCluster(t, d, status, true, "uid-1")
			cache := createCache(t, d.cacheClient, cluster, "uid-1")
			client := &cacheControllerClient{}
			breakStep(&d, client, int64(cache.ID))

			res := (&clusterCacheController{deps: d}).Reconcile(context.Background(), client, cache)

			assert.NotEqual(t, beehive.Settled(), res)
		})
	}
}

// A cache whose owner edge was never loaded is a programming error, not an unowned
// record: silently reading it as "no cluster" would drop the cache out of the fold, and
// the state watch would report a fleet short of one with nothing to say why.
func TestReadingACacheOwnerThatWasNotLoadedIsReported(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	unloaded, err := d.cacheClient.Get(ctx, cache.ID)
	require.NoError(t, err)
	a := cachesAPI{serviceOver(t, d)}

	_, clusterErr := a.clusterFor(ctx, unloaded, map[beehive.ObjectID]*beehive.Object[ClusterRuntimeSpec, ClusterStatus]{})

	assert.ErrorContains(t, clusterErr, "owner")
}

// A cache with no cluster is one beehive's GC is about to take: it drops out of the fold
// rather than being reported, since its going is not a fault.
func TestTheHealthFoldSkipsACacheWithNoCluster(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d := newTestDeps(t)
	_, _, err := d.cacheClient.CreateOrUpdate(ctx, "orphan-cache", ClusterCacheSpec{ServerUID: "uid-1"})
	require.NoError(t, err)

	healths, err := cachesAPI{serviceOver(t, d)}.readAllCacheHealth(ctx)

	require.NoError(t, err)
	assert.Empty(t, healths)
}

// The cluster lookup is memoised across the fold — caches of one cluster share it, and
// this runs on the state watch's cadence. A cluster already found is not read again, and one
// already found to be gone is not looked for again either.
func TestTheClusterLookupIsMemoisedAcrossTheFold(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	first := createCache(t, d.cacheClient, cluster, "uid-1")
	second := createCache(t, d.cacheClient, cluster, "uid-2")
	a := cachesAPI{serviceOver(t, d)}
	seen := map[beehive.ObjectID]*beehive.Object[ClusterRuntimeSpec, ClusterStatus]{}

	firstObj, err := d.cacheClient.Get(ctx, first.ID, beehive.LoadOwner())
	require.NoError(t, err)
	one, err := a.clusterFor(ctx, firstObj, seen)
	require.NoError(t, err)
	secondObj, err := d.cacheClient.Get(ctx, second.ID, beehive.LoadOwner())
	require.NoError(t, err)
	two, err := a.clusterFor(ctx, secondObj, seen)

	require.NoError(t, err)
	assert.Same(t, one, two)
}

// A cache whose cluster has been collected reads as no cluster rather than as a failure:
// the fold read the cache a moment before its cluster went, and the record is on its way
// out with it.
func TestTheClusterLookupAnswersACollectedClusterAsNone(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	d, _ := newRunningRegisteredDeps(t, beehive.WithGCInterval(time.Millisecond))
	// The cluster is collected only after its cache, and the cache's deletion pass waits
	// behind its live one. With no file, that pass builds no store.
	kubestoreFake(d).noFile = true
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	cacheObj, err := d.cacheClient.Get(ctx, cache.ID, beehive.LoadOwner())
	require.NoError(t, err)
	a := cachesAPI{serviceOver(t, d)}
	watch, err := d.clusterClient.Watch(ctx, cluster.ID)
	require.NoError(t, err)

	require.NoError(t, d.clusterClient.Delete(ctx, cluster.ID))
	for {
		if change := testutil.Recv(t, watch.Changes, "the cluster to be collected"); change.Type == beehive.Deleted {
			break
		}
	}

	got, err := a.clusterFor(ctx, cacheObj, map[beehive.ObjectID]*beehive.Object[ClusterRuntimeSpec, ClusterStatus]{})
	require.NoError(t, err)
	assert.Nil(t, got)
}

// A cache with no cluster cannot be armed: every switch the pass reads lives on the
// cluster, and inventing one would arm a sync against defaults the user never set.
func TestArmingACacheWithNoClusterIsReported(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d := newTestDeps(t)
	orphan, _, err := d.cacheClient.CreateOrUpdate(ctx, "orphan-cache", ClusterCacheSpec{})
	require.NoError(t, err)

	_, err = (&clusterCacheController{deps: d}).loadCluster(ctx, orphan)

	assert.ErrorContains(t, err, "has no cluster")
}

// The catalog read is what the mirror is built from, so a cache whose file goes between
// the claim and the read fails the pass — carrying on would mirror an empty answer and
// prune every kind the cache holds.
func TestMirroringFailsWhenTheCatalogWillNotRead(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	store := d.kubestoreMgr.(*fakeKubestore)
	// Retired with the claim already handed out: the pass holds a store whose file is gone.
	store.afterOpen = func(cacheID int64) { require.NoError(t, store.mgr.Remove(cacheID)) }

	err := (&clusterCacheController{deps: d}).mirrorKinds(context.Background(), cache)

	assert.ErrorContains(t, err, "kind catalog")
}

// The records under a cache are read once and answer both the write and the prune, so a
// read that fails takes the pass with it rather than pruning against an empty set.
func TestMirroringFailsWhenTheStoredRecordsWillNotList(t *testing.T) {
	t.Parallel()

	d, cache := cacheOverABrokenStore(t)

	err := (&clusterCacheController{deps: d}).mirrorKinds(context.Background(), cache)

	assert.ErrorContains(t, err, "cached kinds")
}

// A record that will not write fails the pass: settling over it would leave the cache
// mirroring a kind the cluster serves and nothing has a record for.
func TestUpsertingKindsFailsWhenARecordWillNotWrite(t *testing.T) {
	t.Parallel()

	d, cache := cacheOverABrokenStore(t)
	desired := map[string]ClusterCachedKindSpec{
		ClusterCachedKindName(cache.ID, "apps/v1", "deployments"): deploymentsSpec,
	}

	err := (&clusterCacheController{deps: d}).upsertKinds(context.Background(), cache.ID, desired, nil)

	assert.ErrorContains(t, err, "mirror cached kind")
}

// The user's switch is re-read under the lock before a catalog change is written over it,
// and a read that fails must stop the write — carrying on would resume a paused kind
// because its singular was renamed.
func TestUpsertingKindsFailsWhenThePauseWillNotRead(t *testing.T) {
	t.Parallel()

	d, cache := cacheOverABrokenStore(t)
	name := ClusterCachedKindName(cache.ID, "apps/v1", "deployments")
	renamed := deploymentsSpec
	renamed.Kind = "Deploy"
	stored := map[string]*beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus]{
		name: {ID: 404, Name: name, Spec: deploymentsSpec},
	}

	err := (&clusterCacheController{deps: d}).upsertKinds(context.Background(), cache.ID,
		map[string]ClusterCachedKindSpec{name: renamed}, stored)

	assert.ErrorContains(t, err, "read cached kind")
}

// A record collected since the pass listed it has no switch to carry, and the write that
// follows creates it afresh — a race, not a failure.
func TestUpsertingKindsTreatsACollectedRecordAsUnpaused(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	name := ClusterCachedKindName(cache.ID, "apps/v1", "deployments")
	renamed := deploymentsSpec
	renamed.Kind = "Deploy"
	// An id nothing names, standing in for a record collected between the list and here.
	stored := map[string]*beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus]{
		name: {ID: 404, Name: name, Spec: deploymentsSpec},
	}

	err := (&clusterCacheController{deps: d}).upsertKinds(ctx, cache.ID,
		map[string]ClusterCachedKindSpec{name: renamed}, stored)

	require.NoError(t, err)
	got, err := d.kindClient.GetByName(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, "Deploy", got.Spec.Kind)
}

// A record the catalog no longer names is marked, and a mark that will not land fails the
// pass — the kind would otherwise keep syncing with nothing left to say it should.
func TestPruningKindsFailsWhenTheMarkWillNotLand(t *testing.T) {
	t.Parallel()

	d, cache := cacheOverABrokenStore(t)
	stored := map[string]*beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus]{
		"gone": {ID: beehive.ObjectID(cache.ID) + 1, Name: "gone", Spec: deploymentsSpec},
	}

	err := (&clusterCacheController{deps: d}).pruneKinds(context.Background(), nil, stored)

	assert.ErrorContains(t, err, "drop cached kind")
}

// cacheOverABrokenStore stores a cluster and a cache, then closes the store under them —
// so the records exist as Go values while every read and write through beehive fails.
func cacheOverABrokenStore(t *testing.T) (deps, *beehive.Object[ClusterCacheSpec, ClusterCacheStatus]) {
	t.Helper()
	d, closeStore := newTestDepsWithABreakableStore(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	closeStore()
	return d, cache
}

// The state watch binds to the store's change notifications when one exists, so a write is a re-measure
// rather than a wait for the next tick. It binds late, because the file opens when a
// worker arms — after the state watch is already running.
func TestCachesWatchStatsStateRemeasuresOnAWrite(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	store := kubestoreFake(d)
	writer, err := store.mgr.OpenOrCreate(int64(cache.ID))
	require.NoError(t, err)
	t.Cleanup(writer.Release)
	store.setStats(kubestore.Stats{Exists: true, DBBytes: 4096})

	stream, err := serviceOver(t, d).Caches().WatchStatsState(ctx, clusterIDOf(cluster), ClusterCacheID(cache.ID))
	require.NoError(t, err)
	testutil.Recv(t, stream.Frames, "the first measurement")

	store.setStats(kubestore.Stats{Exists: true, DBBytes: 8192})
	require.NoError(t, writer.SyncKinds(ctx, nil, true, 7))

	assert.Equal(t, int64(8192), testutil.Recv(t, stream.Frames, "the re-measurement").Bytes)
}

// A measurement that fails ends the state watch with its reason: a cache whose file cannot be
// measured is not one of zero bytes, and rendering it as such would tell the user their
// cache is empty.
func TestCachesWatchStatsStateEndsOnAMeasurementItCannotTake(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	kubestoreFake(d).err = assert.AnError

	stream, err := serviceOver(t, d).Caches().WatchStatsState(context.Background(),
		clusterIDOf(cluster), ClusterCacheID(cache.ID))

	require.NoError(t, err)
	testutil.WaitClosed(t, stream.Frames, "the state watch to end")
	assert.ErrorContains(t, stream.Err(), "measure cache")
}

// The store closing under the state watch — a clear, a shutdown — re-binds rather than ending:
// the cache is still the caller's, and the fresh file's pings come through a subscription
// the old one never carried.
func TestCachesWatchStatsStateRebindsWhenTheStoreClosesUnderIt(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	store := kubestoreFake(d)
	writer, err := store.mgr.OpenOrCreate(int64(cache.ID))
	require.NoError(t, err)
	store.setStats(kubestore.Stats{Exists: true, DBBytes: 4096})

	stream, err := serviceOver(t, d).Caches().WatchStatsState(ctx, clusterIDOf(cluster), ClusterCacheID(cache.ID))
	require.NoError(t, err)
	testutil.Recv(t, stream.Frames, "the first measurement")

	// The clear's close, which ends every subscription on the old file.
	store.setStats(kubestore.Stats{Exists: true, DBBytes: 8192})
	require.NoError(t, store.mgr.Clear(int64(cache.ID)))
	t.Cleanup(writer.Release)

	assert.Equal(t, int64(8192), testutil.Recv(t, stream.Frames, "the re-measurement after the rebind").Bytes)
}

// A record already marked for deletion is not one of the cache's kinds any more: counting
// it would hold the cache at Connecting over a kind that is on its way out, and put a row
// in the detail for something the user has stopped syncing.
func TestTheSyncReadsSkipARecordAlreadyGoing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	kind := createKind(t, d, cache.ID, deploymentsSpec)
	require.NoError(t, d.kindClient.Delete(ctx, kind.ID))
	a := cachesAPI{serviceOver(t, d)}

	health, err := a.readCacheHealth(ctx, cache)
	require.NoError(t, err)
	status, err := a.readSyncState(ctx, ClusterCacheID(cache.ID))
	require.NoError(t, err)

	assert.Zero(t, health.TotalKinds)
	assert.Empty(t, status.Kinds)
}

// Neither fold invents a verdict over records it could not read: a cache whose kinds will
// not list is unknown, not healthy, and the state watch above says so instead of rendering green.
func TestTheSyncReadsReportRecordsTheyCannotList(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d, cache := cacheOverABrokenStore(t)
	a := cachesAPI{serviceOver(t, d)}

	_, healthErr := a.readCacheHealth(ctx, cache)
	_, statusErr := a.readSyncState(ctx, ClusterCacheID(cache.ID))

	assert.ErrorContains(t, healthErr, "cached kinds")
	assert.ErrorContains(t, statusErr, "cached kinds")
}

// The per-kind counts come off the cache's file: a cache with none has no counts, which is
// a paused cache or one nothing has swept — while a file that will not read is a fault.
func TestTheKindCountsAnswerAMissingFileAndReportABrokenOne(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	a := cachesAPI{serviceOver(t, d)}
	store := kubestoreFake(d)

	store.noFile = true
	counts, err := a.readObjectCountsByKind(ctx, ClusterCacheID(cache.ID))
	require.NoError(t, err)
	assert.Empty(t, counts)

	store.noFile = false
	store.afterOpen = func(cacheID int64) { require.NoError(t, store.mgr.Remove(cacheID)) }
	_, err = a.readObjectCountsByKind(ctx, ClusterCacheID(cache.ID))
	assert.ErrorContains(t, err, "kinds")
}

// A clear that fails is reported: the file is still there, and answering with the record
// would tell the user the cache had been emptied when it had not.
func TestCacheClearReportsAStoreThatWillNotClear(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	kubestoreFake(d).err = assert.AnError

	_, err := serviceOver(t, d).Caches().Clear(context.Background(), ClusterCacheID(cache.ID))

	assert.ErrorContains(t, err, "clear cluster cache")
}

// The state watch sends only what moved: it re-reads on a cadence, and a cache whose verdict
// sits still would otherwise put a frame on the wire every tick, for every cache, forever.
func TestCachesWatchHealthKeyedStateSaysNothingWhileTheVerdictHoldsStill(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	setKindReason(d, cacheID, podsSpec, kubesync.KindState{Reason: kubesync.ReasonWatching, LastLiveAt: probedAt})
	setKindReason(d, cacheID, deploymentsSpec, kubesync.KindState{Reason: kubesync.ReasonWatching, LastLiveAt: probedAt})
	svc := serviceOver(t, d)
	svc.stateWatchCadence = time.Millisecond
	ctx := t.Context()

	stream, err := svc.Caches().WatchHealthKeyedState(ctx)
	require.NoError(t, err)
	require.Equal(t, kubesync.ReasonWatching, testutil.Recv(t, stream.Frames, "the verdict").Reason)

	testutil.NoRecv(t, stream.Frames, quietWindow, "a repeat of a verdict that has not moved")
}

// The three state watches stop where they stand when the consumer goes: every send is checked,
// so a dropped subscription ends the loop rather than parking it on a channel forever.
// Each is driven to a send that parks — Frames buffers one, and nothing reads it — so the
// cancellation is the only thing that can free it.
func TestTheCacheStateWatchesStopForAConsumerThatIsGone(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d, cacheID := syncingCacheWithKinds(t)
	cluster := ownerClusterObjectOf(t, d, cacheID)
	clusterID := clusterIDOf(cluster)
	// A second cache, so the health fold has more than one verdict to send.
	createCache(t, d.cacheClient, cluster, "uid-2")
	setKindReason(d, cacheID, podsSpec, kubesync.KindState{Reason: kubesync.ReasonWatching, LastLiveAt: probedAt})
	store := kubestoreFake(d)
	sync := syncFake(d)
	svc := serviceOver(t, d)
	svc.stateWatchCadence = time.Millisecond

	// The two per-cache state watches re-send only what moved, so their readings move on every
	// read. A state watch reads a second time only once its first frame fills the buffer, and
	// that second reading differs, so its next send parks.
	var statsReads, statusReads atomic.Int64
	statsParks, statusParks := testutil.NewSignal(), testutil.NewSignal()
	store.onStats = func(id int64) {
		if id != int64(cacheID) {
			return
		}
		n := statsReads.Add(1)
		store.setStats(kubestore.Stats{Exists: true, DBBytes: n})
		if n == 2 {
			statsParks.Fire()
		}
	}
	sync.onDiscoveryRead = func(id int64) {
		if id != int64(cacheID) {
			return
		}
		n := statusReads.Add(1)
		sync.setDiscoveryState(id, kubesync.DiscoveryState{Reason: kubesync.ReasonDiscovered, Message: strconv.FormatInt(n, 10)})
		if n == 2 {
			statusParks.Fire()
		}
	}

	health, err := svc.Caches().WatchHealthKeyedState(ctx)
	require.NoError(t, err)
	status, err := svc.Caches().WatchSyncState(ctx, clusterID, cacheID)
	require.NoError(t, err)
	stats, err := svc.Caches().WatchStatsState(ctx, clusterID, cacheID)
	require.NoError(t, err)

	statsParks.Wait(t, "the stats state watch to read again past its first frame")
	statusParks.Wait(t, "the sync-status state watch to read again past its first frame")
	// Two caches, so the health state watch's first pass sends twice: the second parks.
	require.Eventually(t, func() bool { return len(health.Frames) == 1 }, testutil.Timeout, time.Millisecond, "the health state watch to send its first frame")

	cancel()

	// Nothing reads Frames, so only the cancellation can free a parked send.
	testutil.Wait(t, health.done, "the health state watch to stop")
	testutil.Wait(t, status.done, "the sync-status state watch to stop")
	testutil.Wait(t, stats.done, "the stats state watch to stop")
}

// ownerClusterOf is the cluster a cache hangs off, which the per-cache state watches are scoped by.
func ownerClusterOf(t *testing.T, d deps, cacheID ClusterCacheID) ClusterID {
	t.Helper()
	return clusterIDOf(ownerClusterObjectOf(t, d, cacheID))
}

// ownerClusterObjectOf reads the cluster a cache hangs off, through the owner edge.
func ownerClusterObjectOf(t *testing.T, d deps, cacheID ClusterCacheID) *beehive.Object[ClusterRuntimeSpec, ClusterStatus] {
	t.Helper()
	owner, ok, err := d.cacheClient.GetOwner(context.Background(), beehive.ObjectID(cacheID))
	require.NoError(t, err)
	require.True(t, ok)
	obj, err := d.clusterClient.Get(context.Background(), owner.ID)
	require.NoError(t, err)
	return obj
}

// Neither the fold nor the detail read invents an answer over a store it cannot reach: a
// cache whose records or file will not read is unknown, not healthy and not empty.
func TestTheCacheFoldsReportWhatTheyCannotRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("the fold's cluster lookup", func(t *testing.T) {
		d, closeStore := newTestDepsWithABreakableStore(t)
		cluster := createCluster(t, d, "prod")
		cache := createCache(t, d.cacheClient, cluster, "uid-1")
		// Owner-loaded, so the lookup gets past the edge and fails on the cluster itself.
		loaded, err := d.cacheClient.Get(ctx, cache.ID, beehive.LoadOwner())
		require.NoError(t, err)
		closeStore()

		_, err = cachesAPI{serviceOver(t, d)}.clusterFor(ctx, loaded,
			map[beehive.ObjectID]*beehive.Object[ClusterRuntimeSpec, ClusterStatus]{})

		assert.ErrorContains(t, err, "read cluster 1")
	})

	t.Run("the fold's cache list", func(t *testing.T) {
		d, _ := cacheOverABrokenStore(t)
		_, err := cachesAPI{serviceOver(t, d)}.readAllCacheHealth(ctx)
		assert.Error(t, err)
	})

	t.Run("the detail's kind counts", func(t *testing.T) {
		d := newTestDeps(t)
		cluster := createCluster(t, d, "prod")
		cache := createCache(t, d.cacheClient, cluster, "uid-1")
		kubestoreFake(d).err = assert.AnError
		_, err := cachesAPI{serviceOver(t, d)}.readSyncState(ctx, ClusterCacheID(cache.ID))
		assert.ErrorContains(t, err, "open cluster cache")
	})
}

// The fold orders by cache id, so a fleet's verdicts hold still between ticks rather than
// reshuffling on whatever order the store returned.
func TestTheHealthFoldOrdersByCacheID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	first := createCache(t, d.cacheClient, cluster, "uid-1")
	second := createCache(t, d.cacheClient, cluster, "uid-2")

	healths, err := cachesAPI{serviceOver(t, d)}.readAllCacheHealth(ctx)

	require.NoError(t, err)
	require.Len(t, healths, 2)
	assert.Equal(t, ClusterCacheID(first.ID), healths[0].CacheID)
	assert.Equal(t, ClusterCacheID(second.ID), healths[1].CacheID)
}

// A cache whose cluster cannot be read cannot be armed: every switch the pass reads lives
// there, and arming on a guess would sync against settings the user never chose.
func TestArmingReportsAClusterItCannotRead(t *testing.T) {
	t.Parallel()

	d, cache := cacheOverABrokenStore(t)

	c := &clusterCacheController{deps: d}
	_, loadErr := c.loadCluster(context.Background(), cache)
	armErr := c.armSync(context.Background(), &cacheControllerClient{}, cache, false)

	assert.ErrorContains(t, loadErr, "owner")
	assert.ErrorContains(t, armErr, "owner")
}

// A user navigating away ends the state watches cleanly. Their reads all take ctx, so each one
// ends with ctx's own error — reported, that reaches the webview as a watch failure and
// puts an error in front of someone who has already left the view.
func TestTheCacheStateWatchesEndCleanlyWhenTheConsumerLeaves(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	d, cacheID := syncingCacheWithKinds(t)
	clusterID := ownerClusterOf(t, d, cacheID)
	svc := serviceOver(t, d)
	svc.stateWatchCadence = time.Millisecond

	status, err := svc.Caches().WatchSyncState(ctx, clusterID, cacheID)
	require.NoError(t, err)
	health, err := svc.Caches().WatchHealthKeyedState(ctx)
	require.NoError(t, err)
	stats, err := svc.Caches().WatchStatsState(ctx, clusterID, cacheID)
	require.NoError(t, err)
	testutil.Recv(t, status.Frames, "the first sync-status reading")
	testutil.Recv(t, health.Frames, "the first verdict")
	testutil.Recv(t, stats.Frames, "the first measurement")

	cancel()

	testutil.WaitClosed(t, status.Frames, "the sync-status state watch to stop")
	testutil.WaitClosed(t, health.Frames, "the health state watch to stop")
	testutil.WaitClosed(t, stats.Frames, "the stats state watch to stop")
	assert.NoError(t, status.Err())
	assert.NoError(t, health.Err())
	assert.NoError(t, stats.Err())
}

// The ceiling reaches the UI on the state watch the size is already on, so a client renders how
// close a cache is without a second subscription — and without hardcoding our default.
func TestMeasureCacheCarriesTheSizeCeiling(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	deps.kubestoreMgr = &fakeKubestore{stats: kubestore.Stats{
		Exists: true, DBBytes: 100, OverSizeLimit: true, SizeLimitBytes: 512,
	}}
	svc := &service{deps: deps}

	got, err := cachesAPI{s: svc}.measureCache(context.Background(), 1)

	require.NoError(t, err)
	assert.True(t, got.OverSizeLimit)
	assert.Equal(t, int64(512), got.SizeLimitBytes)
}

// --- the size ceiling ---

// overLimit is a cache whose file has passed its ceiling, as the janitor reports it.
func overLimit() kubestore.Stats {
	return kubestore.Stats{Exists: true, DBBytes: 3 * gib, SizeLimitBytes: 2 * gib, OverSizeLimit: true}
}

// gib is the unit the ceiling is set in, so a fixture reads as a size.
const gib = 1 << 30

// A cache at its ceiling stops syncing, and the record is told why — the pause closes the
// file, so the condition is the only thing that will still remember.
func TestCachePassStopsACacheOverItsSizeLimit(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	kubestoreFake(d).setStats(overLimit())

	client := reconcileCache(t, d, cache)

	_, armed := syncFake(d).armedDiscovery(int64(cache.ID))
	assert.False(t, armed, "a cache at its ceiling syncs nothing")
	require.Len(t, client.conditions, 1)
	assert.Equal(t, string(ConditionSynced), client.conditions[0].Type)
	assert.Equal(t, ConditionFalse, client.conditions[0].Status)
	assert.Equal(t, ReasonSizeLimit, client.conditions[0].Reason)
	assert.Contains(t, client.conditions[0].Message, "3221225472")
	assert.Contains(t, client.conditions[0].Message, "2147483648")
}

// sizeStopped is the condition the pass writes when a cache hits its ceiling, as a record
// that has already been stopped carries it into the next pass.
func sizeStopped() []Condition {
	return []Condition{LiveCondition(ConditionSynced, ConditionFalse, ReasonSizeLimit, "over")}
}

// The pause closes the file, and a closed file reports no verdict at all — so a pass that
// read the verdict alone would restart the cache it had just stopped, refill it, and stop
// it again. The record is what remembers.
func TestCachePassKeepsACacheStoppedOnceItsFileIsClosed(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	cache.Conditions = sizeStopped()
	closed := overLimit()
	closed.OverSizeLimit = false
	kubestoreFake(d).setStats(closed)

	client := reconcileCache(t, d, cache)

	_, armed := syncFake(d).armedDiscovery(int64(cache.ID))
	assert.False(t, armed, "a stopped cache stays stopped while its bytes are over")
	assert.Empty(t, client.deleted, "nothing releases a cache still over its limit")
}

// The verdict is the only way in. Bytes over the limit with no verdict behind them are a
// file nobody has open, which cannot be growing — stopping it would stop caches nothing is
// filling.
func TestCachePassDoesNotStopACacheOnBytesAlone(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	closed := overLimit()
	closed.OverSizeLimit = false
	kubestoreFake(d).setStats(closed)

	client := reconcileCache(t, d, cache)

	_, armed := syncFake(d).armedDiscovery(int64(cache.ID))
	assert.True(t, armed, "a cache nobody has open is not one that is filling")
	assert.Empty(t, client.conditions)
}

// The release: a cleared or unbounded cache starts again, and the condition goes with it.
func TestCachePassRestartsACacheBackUnderItsLimit(t *testing.T) {
	t.Parallel()

	tests := map[string]kubestore.Stats{
		"cleared":   {},
		"under":     {Exists: true, DBBytes: gib, SizeLimitBytes: 2 * gib},
		"unbounded": {Exists: true, DBBytes: 9 * gib},
	}

	for name, stats := range tests {
		t.Run(name, func(t *testing.T) {
			d, status := newClusterStatusDeps(t)
			cluster := storedCluster(t, d, status, true, "uid-1")
			cache := createCache(t, d.cacheClient, cluster, "uid-1")
			cache.Conditions = sizeStopped()
			kubestoreFake(d).setStats(stats)

			client := reconcileCache(t, d, cache)

			_, armed := syncFake(d).armedDiscovery(int64(cache.ID))
			assert.True(t, armed, "a cache back under its ceiling syncs again")
			assert.Equal(t, []string{string(ConditionSynced)}, client.deleted)
		})
	}
}

// Every cache that was never over its limit is this case, on every pass: a condition write
// per pass per cache would be a transaction for a verdict nothing formed.
func TestCachePassWritesNoConditionForAnOrdinaryCache(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	kubestoreFake(d).setStats(kubestore.Stats{Exists: true, DBBytes: gib, SizeLimitBytes: 2 * gib})

	client := reconcileCache(t, d, cache)

	assert.Empty(t, client.conditions)
	assert.Empty(t, client.deleted)
}

// A measurement that failed decides nothing: answering under would restart a cache nobody
// measured, and answering over would stop one over an error.
func TestTheSizeCheckLeavesAFailedMeasurementAlone(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		conditions []Condition
		want       bool
	}{
		"a stopped cache stays stopped": {conditions: sizeStopped(), want: true},
		"a running cache keeps running": {want: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			d, status := newClusterStatusDeps(t)
			cluster := storedCluster(t, d, status, true, "uid-1")
			cache := createCache(t, d.cacheClient, cluster, "uid-1")
			cache.Conditions = tt.conditions
			kubestoreFake(d).err = assert.AnError

			limited, measured := (&clusterCacheController{deps: d}).checkSizeLimit(context.Background(), cache)

			assert.Equal(t, tt.want, limited)
			assert.Nil(t, measured, "an unmeasured pass has no size to write")
		})
	}
}

// sizeStoppedCache is a synced cluster's cache whose record carries the ceiling's
// condition, as the pass that stopped it left the record.
func sizeStoppedCache(t *testing.T) (deps, ClusterCacheID) {
	t.Helper()
	d, bh := newTestDepsAndBeehive(t)
	cluster := storedCluster(t, d, beehive.NewAdminClient[ClusterStatus](bh, ClusterGroupKind), true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	admin := beehive.NewAdminClient[ClusterCacheStatus](bh, ClusterCacheGroupKind)
	require.NoError(t, admin.SetCondition(context.Background(), cache.ID, sizeStopped()[0]))
	return d, ClusterCacheID(cache.ID)
}

// The condition is where the ceiling's verdict lives, and the state watch is the only place the
// UI looks — so the fold reads it back. Nothing else stops a cache without arming a kind
// that could say so.
func TestCachesWatchHealthKeyedStateReportsACacheStoppedByItsCeiling(t *testing.T) {
	t.Parallel()

	d, _ := sizeStoppedCache(t)

	healths, err := cachesAPI{serviceOver(t, d)}.readAllCacheHealth(context.Background())

	require.NoError(t, err)
	require.Len(t, healths, 1)
	assert.Equal(t, ConditionFalse, healths[0].Status)
	assert.Equal(t, ReasonSizeLimit, healths[0].Reason)
}

// The arm ordering, the other way round: a cache that is both at its ceiling and paused
// kind by kind is stopped by the ceiling, and reporting it as merely paused is the
// confusion the two reasons exist to keep apart.
func TestCachesWatchHealthKeyedStateReadsTheCeilingAboveAFullyPausedCache(t *testing.T) {
	t.Parallel()

	d, cacheID := sizeStoppedCache(t)
	createKind(t, d, beehive.ObjectID(cacheID), podsSpec)
	pauseKind(t, d, beehive.ObjectID(cacheID), podsSpec.APIVersion, podsSpec.Resource)

	healths, err := cachesAPI{serviceOver(t, d)}.readAllCacheHealth(context.Background())

	require.NoError(t, err)
	require.Len(t, healths, 1)
	assert.Equal(t, ReasonSizeLimit, healths[0].Reason)
}

// The user's remedy has to reach the pass. A stopped cache holds no claim, so the cleared file
// reopens nothing and wakes nothing: the clear requeues the record itself, after the file is
// empty, so the pass it buys measures the cleared file and releases the stop
// (TestCachePassRestartsACacheBackUnderItsLimit).
func TestClearingACacheRequeuesItsRecord(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	var steps []string
	var requeued beehive.ObjectID
	kubestoreFake(d).onClear = func(int64) { steps = append(steps, "clear") }
	d.cacheClient = requeueRecorder{Client: d.cacheClient, requeued: func(id beehive.ObjectID) {
		steps = append(steps, "requeue")
		requeued = id
	}}

	_, err := serviceOver(t, d).Caches().Clear(context.Background(), ClusterCacheID(cache.ID))

	require.NoError(t, err)
	assert.Equal(t, []string{"clear", "requeue"}, steps)
	assert.Equal(t, cache.ID, requeued)
}

// requeueRecorder is a cache client whose requeues are recorded rather than run.
type requeueRecorder struct {
	beehive.Client[ClusterCacheSpec, ClusterCacheStatus]
	requeued func(beehive.ObjectID)
	// err is what every requeue answers.
	err error
}

func (r requeueRecorder) Requeue(_ context.Context, id beehive.ObjectID, _ ...beehive.RequeueOption) error {
	r.requeued(id)
	return r.err
}

// The sentinel for this subsystem: a client-go error carries the request URL it failed on,
// and a URL can carry a token. What reaches the record must carry no part of it — the host
// forwards every sidecar line into its own log sink, so the rendering has to happen here.
func TestCachePassLogsAFailureWithoutTheCredential(t *testing.T) {
	// CaptureLogs swaps the process's default logger, so this test stays serial.
	logs := testutil.CaptureLogs(t)
	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	kubestoreFake(d).err = errors.New(
		`Get "https://api.example.com/readyz?token=SEKRIT": context deadline exceeded`)

	(&clusterCacheController{deps: d}).checkSizeLimit(context.Background(), cache)

	assert.NotContains(t, logs.String(), "SEKRIT")
	assert.Contains(t, logs.String(), "context deadline exceeded")
}

// Health and SyncState are one reading each of what the state watches stream: the card a chat
// carries reads them once, at the send, and opens no stream.
func TestCachesHealthIsOneReadingOfTheStateWatch(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	setKindReason(d, cacheID, podsSpec, kubesync.KindState{Reason: kubesync.ReasonWatching, Live: true})
	setKindReason(d, cacheID, deploymentsSpec, kubesync.KindState{Reason: kubesync.ReasonSyncFailed})

	svc := serviceOver(t, d)
	got, ok, err := svc.Caches().Health(context.Background(), ownerClusterOf(t, d, cacheID), cacheID)
	require.NoError(t, err)
	require.True(t, ok)
	stream, err := svc.Caches().WatchHealthKeyedState(context.Background())
	require.NoError(t, err)
	want := testutil.Recv(t, stream.Frames, "the state watch's first frame")

	assert.Equal(t, want, got)
	assert.Equal(t, kubesync.ReasonSyncFailed, got.Reason)
}

func TestCachesHealthAnswersNothingForACacheOfAnotherCluster(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	other := createCluster(t, d, "other")

	_, ok, err := serviceOver(t, d).Caches().Health(context.Background(), clusterIDOf(other), cacheID)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestCachesSyncStatusIsOneReadingOfTheStateWatch(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)
	syncFake(d).setDiscoveryState(int64(cacheID), kubesync.DiscoveryState{Reason: kubesync.ReasonPartial})
	setKindReason(d, cacheID, podsSpec, kubesync.KindState{Reason: kubesync.ReasonWatching, Live: true})

	svc := serviceOver(t, d)
	got, ok, err := svc.Caches().SyncState(context.Background(), ownerClusterOf(t, d, cacheID), cacheID)
	require.NoError(t, err)
	require.True(t, ok)
	stream := watchSyncState(t, svc, ownerClusterOf(t, d, cacheID), cacheID)
	want := testutil.Recv(t, stream.Frames, "the state watch's first frame")

	assert.Equal(t, want, got)
	assert.Equal(t, kubesync.ReasonPartial, got.Discovery.Reason)

	other := createCluster(t, d, "other")
	_, ok, err = svc.Caches().SyncState(context.Background(), clusterIDOf(other), cacheID)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestCachesHealthAnswersNothingForACacheThatDoesNotExist(t *testing.T) {
	t.Parallel()

	d, cacheID := syncingCacheWithKinds(t)

	_, ok, err := serviceOver(t, d).Caches().Health(context.Background(), ownerClusterOf(t, d, cacheID), cacheID+1000)
	require.NoError(t, err)
	assert.False(t, ok)
}

// A state watch whose consumer has left ends at the send it was parked on, never reporting
// the cancellation. The first reading fills the stream's buffer unread, and the second
// read moves the verdict and ends the context, so the state watch's next send has nowhere to go.
func TestWatchSyncStateStopsAtASendTheConsumerLeft(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d, cacheID := syncingCacheWithKinds(t)
	clusterID := ownerClusterOf(t, d, cacheID)
	svc := serviceOver(t, d)
	svc.stateWatchCadence = time.Millisecond
	store := kubestoreFake(d)
	store.noFile = true
	reads := 0
	store.onOpen = func(int64) {
		reads++
		if reads == 2 {
			setKindReason(d, cacheID, podsSpec, kubesync.KindState{Reason: kubesync.ReasonWatching, Live: true})
			cancel()
		}
	}

	stream, err := svc.Caches().WatchSyncState(ctx, clusterID, cacheID)
	require.NoError(t, err)

	testutil.Wait(t, stream.done, "the state watch to stop")
	assert.Equal(t, 2, reads, "the state watch stopped at the second reading's send")
	assert.NoError(t, stream.Err())
}

// failingClusterGets is the cluster client with Get answering err; everything else is
// the real client's.
type failingClusterGets struct {
	beehive.Client[ClusterRuntimeSpec, ClusterStatus]
	err error
}

func (c failingClusterGets) Get(context.Context, beehive.ObjectID, ...beehive.LoadOption) (*beehive.Object[ClusterRuntimeSpec, ClusterStatus], error) {
	return nil, c.err
}

// failingKindLists is the kind client with ListOwnedObjects answering err; everything
// else is the real client's.
type failingKindLists struct {
	beehive.Client[ClusterCachedKindSpec, ClusterCachedKindStatus]
	err error
}

func (c failingKindLists) ListOwnedObjects(context.Context, beehive.ObjectID, ...beehive.LoadOption) ([]*beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus], error) {
	return nil, c.err
}

// One reading of a cache's verdict or sync detail reports a record it cannot read, as
// the state watches do, rather than answering a verdict nothing measured.
func TestTheOneReadingsReportWhatTheyCannotRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	boom := errors.New("store down")

	t.Run("the verdict's cluster", func(t *testing.T) {
		d, cacheID := syncingCacheWithKinds(t)
		clusterID := ownerClusterOf(t, d, cacheID)
		d.clusterClient = failingClusterGets{Client: d.clusterClient, err: boom}

		_, ok, err := serviceOver(t, d).Caches().Health(ctx, clusterID, cacheID)

		require.ErrorIs(t, err, boom)
		assert.False(t, ok)
	})
	t.Run("the verdict's kinds", func(t *testing.T) {
		d, cacheID := syncingCacheWithKinds(t)
		clusterID := ownerClusterOf(t, d, cacheID)
		d.kindClient = failingKindLists{Client: d.kindClient, err: boom}

		_, ok, err := serviceOver(t, d).Caches().Health(ctx, clusterID, cacheID)

		require.ErrorIs(t, err, boom)
		assert.False(t, ok)
	})
	t.Run("the sync detail", func(t *testing.T) {
		d, cacheID := syncingCacheWithKinds(t)
		clusterID := ownerClusterOf(t, d, cacheID)
		kubestoreFake(d).err = boom

		_, ok, err := serviceOver(t, d).Caches().SyncState(ctx, clusterID, cacheID)

		require.ErrorIs(t, err, boom)
		assert.False(t, ok)
	})
}

// The fold over every cache fails on any one it cannot read, rather than leaving that
// cache out of a fleet's verdicts.
func TestTheHealthFoldReportsACacheItCannotRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	boom := errors.New("store down")

	t.Run("its cluster", func(t *testing.T) {
		d, _ := syncingCacheWithKinds(t)
		d.clusterClient = failingClusterGets{Client: d.clusterClient, err: boom}

		_, err := cachesAPI{serviceOver(t, d)}.readAllCacheHealth(ctx)

		assert.ErrorIs(t, err, boom)
	})
	t.Run("its kinds", func(t *testing.T) {
		d, _ := syncingCacheWithKinds(t)
		d.kindClient = failingKindLists{Client: d.kindClient, err: boom}

		_, err := cachesAPI{serviceOver(t, d)}.readAllCacheHealth(ctx)

		assert.ErrorIs(t, err, boom)
	})
}

// A requeue after a clear that will not land is logged, not returned: the clear itself
// went through, and the cache's own resync covers the wake.
func TestCachesClearLogsARequeueThatFailed(t *testing.T) {
	// CaptureLogs swaps the process's default logger, so this test stays serial.
	logs := testutil.CaptureLogs(t)
	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	d.cacheClient = requeueRecorder{Client: d.cacheClient, requeued: func(beehive.ObjectID) {}, err: errors.New("record gone")}

	got, err := serviceOver(t, d).Caches().Clear(context.Background(), ClusterCacheID(cache.ID))

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Contains(t, logs.String(), "requeue after clear failed")
	assert.Contains(t, logs.String(), "record gone")
}

// The size condition is the only thing remembering why a cache stopped, so a pass that
// cannot write it, or cannot clear it on the way out, fails rather than settling.
func TestCachePassFailsWhenTheSizeConditionWillNotWrite(t *testing.T) {
	t.Parallel()

	boom := errors.New("store down")

	t.Run("set", func(t *testing.T) {
		d, status := newClusterStatusDeps(t)
		cluster := storedCluster(t, d, status, true, "uid-1")
		cache := createCache(t, d.cacheClient, cluster, "uid-1")
		kubestoreFake(d).setStats(overLimit())

		res := (&clusterCacheController{deps: d}).Reconcile(context.Background(), &cacheControllerClient{conditionErr: boom}, cache)

		assert.ErrorIs(t, res.Err(), boom)
		assert.ErrorContains(t, res.Err(), "set cluster cache")
	})
	t.Run("clear", func(t *testing.T) {
		d, status := newClusterStatusDeps(t)
		cluster := storedCluster(t, d, status, true, "uid-1")
		cache := createCache(t, d.cacheClient, cluster, "uid-1")
		cache.Conditions = sizeStopped()
		kubestoreFake(d).setStats(kubestore.Stats{})

		res := (&clusterCacheController{deps: d}).Reconcile(context.Background(), &cacheControllerClient{conditionErr: boom}, cache)

		assert.ErrorIs(t, res.Err(), boom)
		assert.ErrorContains(t, res.Err(), "clear cluster cache")
	})
}

// A cache whose cluster record will not read cannot be armed, past its owner edge too.
func TestArmingReportsAClusterRecordItCannotRead(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	boom := errors.New("store down")
	d.clusterClient = failingClusterGets{Client: d.clusterClient, err: boom}

	_, err := (&clusterCacheController{deps: d}).loadCluster(context.Background(), cache)

	assert.ErrorIs(t, err, boom)
}

// failingKindWrites is the kind client with CreateOrUpdate answering err; everything
// else is the real client's.
type failingKindWrites struct {
	beehive.Client[ClusterCachedKindSpec, ClusterCachedKindStatus]
	err error
}

func (c failingKindWrites) CreateOrUpdate(context.Context, string, ClusterCachedKindSpec, ...beehive.Option) (*beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus], bool, error) {
	return nil, false, c.err
}

// A kind record that will not write fails the mirroring before the prune, which would
// otherwise answer off a set the write never reached.
func TestMirroringFailsWhenAKindWillNotWrite(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	writeCatalog(t, d, ClusterCacheID(cache.ID), kindRow("v1", "Pod", "pods"))
	boom := errors.New("store down")
	d.kindClient = failingKindWrites{Client: d.kindClient, err: boom}

	err := (&clusterCacheController{deps: d}).mirrorKinds(context.Background(), cache)

	assert.ErrorIs(t, err, boom)
}

// Every window shares one health loop, so a window opened later joins a loop that is
// already running. It still starts from every cache's verdict, read fresh: a cache made
// since the loop last read is in its first frames. That read is everyone's: the early
// window hears of the new cache from it, long before its own cadence would have.
func TestCachesWatchHealthKeyedStateGivesALateSubscriberEveryVerdict(t *testing.T) {
	t.Parallel()

	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	first := createCache(t, d.cacheClient, cluster, "uid-1")
	svc := serviceOver(t, d)
	svc.stateWatchCadence = time.Hour

	early, err := svc.Caches().WatchHealthKeyedState(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ClusterCacheID(first.ID), testutil.Recv(t, early.Frames, "the first verdict").CacheID)

	second := createCache(t, d.cacheClient, cluster, "uid-2")
	late, err := svc.Caches().WatchHealthKeyedState(t.Context())
	require.NoError(t, err)
	got := []ClusterCacheID{
		testutil.Recv(t, late.Frames, "the first verdict, late").CacheID,
		testutil.Recv(t, late.Frames, "the second verdict, late").CacheID,
	}
	assert.ElementsMatch(t, []ClusterCacheID{ClusterCacheID(first.ID), ClusterCacheID(second.ID)}, got)
	assert.Equal(t, ClusterCacheID(second.ID), testutil.Recv(t, early.Frames, "the join's read, early").CacheID)
}
