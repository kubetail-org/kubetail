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
	"encoding/json"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/amorey/beehive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubesync"
)

// Deterministic per (anchor, apiVersion, resource), which is what lets a discovery
// pass reconcile its children as a set with no per-child bookkeeping. Keyed on the
// plural, not the Kind: a CRD may reuse a built-in's plural under another group, so
// the group-version has to be in the name too.
func TestClusterCachedKindName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "cachedkind/3/apps/v1/deployments", ClusterCachedKindName(3, "apps/v1", "deployments"))
	assert.NotEqual(t,
		ClusterCachedKindName(3, "apps/v1", "deployments"),
		ClusterCachedKindName(3, "example.com/v1", "deployments"),
		"the same plural under another group is a different kind")
	assert.NotEqual(t,
		ClusterCachedKindName(3, "apps/v1", "deployments"),
		ClusterCachedKindName(4, "apps/v1", "deployments"),
		"the same kind under another cache")
}

func createKind(t *testing.T, d deps, cacheID beehive.ObjectID, spec ClusterCachedKindSpec) *beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus] {
	t.Helper()
	ctx := context.Background()
	name := ClusterCachedKindName(cacheID, spec.APIVersion, spec.Resource)
	obj, _, err := d.kindClient.CreateOrUpdate(ctx, name, spec, beehive.WithOwner(cacheID))
	require.NoError(t, err)
	return obj
}

// deploymentsSpec and podsSpec are two served kinds, enough to prove ordering and scoping.
var (
	deploymentsSpec = ClusterCachedKindSpec{APIVersion: "apps/v1", Kind: "Deployment", Resource: "deployments", Namespaced: true}
	podsSpec        = ClusterCachedKindSpec{APIVersion: "v1", Kind: "Pod", Resource: "pods", Namespaced: true}
)

// Every stored record predates the pause field, and beehive decodes a spec with
// json.Unmarshal — so a missing key has to mean SYNCING. That is why the field is Paused
// rather than Enabled: an Enabled bool would decode false for the whole fleet on the
// upgrade that shipped it, and every kind would stop syncing at once.
func TestACachedKindStoredBeforeThePauseFieldDecodesAsSyncing(t *testing.T) {
	t.Parallel()

	var spec ClusterCachedKindSpec

	require.NoError(t, json.Unmarshal(
		[]byte(`{"apiVersion":"apps/v1","kind":"Deployment","resource":"deployments","namespaced":true}`), &spec))

	assert.False(t, spec.Paused, "a record with no pause key syncs")
}

// twoCachesTwoResources stores a kind under each of two caches, and returns the first
// cache's id — enough for one fixture to prove both a read's contents and its scoping.
func twoCachesTwoResources(t *testing.T) (deps, ClusterCacheID) {
	t.Helper()
	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	one := createCache(t, d.cacheClient, cluster, "uid-1")
	two := createCache(t, d.cacheClient, cluster, "uid-2")
	createKind(t, d, one.ID, deploymentsSpec)
	createKind(t, d, two.ID, podsSpec)
	return d, ClusterCacheID(one.ID)
}

// watchKinds opens the unscoped stream for the test's life.
func watchKinds(t *testing.T, d deps) *Stream[ClusterCachedKindCollectionWatchFrame] {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream, err := serviceOver(t, d).CachedKinds().WatchCollection(ctx)
	require.NoError(t, err)
	return stream
}

func TestCachedKindsWatchCollectionEmitsTheSnapshotThenABookmark(t *testing.T) {
	t.Parallel()

	d := newRunningDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	createKind(t, d, cache.ID, deploymentsSpec)

	stream := watchKinds(t, d)

	f := testutil.Recv(t, stream.Frames, "the snapshot frame")
	require.Equal(t, CollectionWatchFrameAdded, f.Type)
	require.NotNil(t, f.Kind)
	assert.Equal(t, "deployments", f.Kind.Spec.Resource)
	assert.Equal(t, ObjectRef{ID: ObjectID(cache.ID), Kind: "ClusterCache"}, f.Kind.Owner)
	assert.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)
}

// A fleet with nothing discovered yet bookmarks an empty collection rather than holding
// the snapshot back: the wait is the consumer's to render.
func TestCachedKindsWatchCollectionBookmarksAnEmptyCollection(t *testing.T) {
	t.Parallel()

	stream := watchKinds(t, newRunningDeps(t))

	assert.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)
}

// A departure is a collected row, not the soft delete that precedes it — a prune marks the
// record and that arrives as an ordinary Modified. The frame carries no owner, because
// beehive loads no edges for a collected row and a frame that failed over that would kill
// the stream and strand the record in the client's map.
func TestCachedKindsWatchCollectionReportsADeparture(t *testing.T) {
	t.Parallel()

	frames := pumpFrames(t, kindWatch, nil,
		beehive.ObjectChange[ClusterCachedKindSpec, ClusterCachedKindStatus]{Type: beehive.Deleted, ID: 7, Object: nil},
	)

	require.Len(t, frames, 2)
	assert.Equal(t, CollectionWatchFrameDeleted, frames[1].Type)
	require.NotNil(t, frames[1].Kind)
	assert.Equal(t, ClusterCachedKindID(7), frames[1].Kind.ID)
}

// A prune is beehive's soft delete, so the row lingers holding its name and the mark arrives as
// an ordinary Modified — which is what makes the missing tombstone field on this record matter:
// a consumer cannot tell the mark from any other spec write.
//
// The mark is not promised on its own. beehive folds a run of writes to one object into its
// last op, so a prune collected before the tailer reads the mark arrives as the departure
// alone. What every shape guarantees is the departure, which is what clears the id.
func TestCachedKindsWatchCollectionReportsAPruneAsAModifyUntilItIsCollected(t *testing.T) {
	t.Parallel()

	// Sweeping far faster than production, because the departure is what ends the loop below
	// and the default cadence outlasts the test's own failsafe.
	d := newRunningDeps(t, beehive.WithGCInterval(10*time.Millisecond))
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	obj := createKind(t, d, cache.ID, deploymentsSpec)

	stream := watchKinds(t, d)
	require.Equal(t, CollectionWatchFrameAdded, testutil.Recv(t, stream.Frames, "the snapshot frame").Type)
	require.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)

	require.NoError(t, d.kindClient.Delete(context.Background(), obj.ID))

	for {
		f := testutil.Recv(t, stream.Frames, "the prune")
		require.NotNil(t, f.Kind, "every frame of a prune names the id it is about")
		assert.Equal(t, ClusterCachedKindID(obj.ID), f.Kind.ID)
		if f.Type == CollectionWatchFrameDeleted {
			return
		}
		assert.Equal(t, CollectionWatchFrameModified, f.Type)
		assert.NotNil(t, f.Kind.DeletionRequestedAt, "the mark is all that tells a prune from a spec write")
	}
}

// One record's own stream, for a consumer holding an id from a frame.
func TestCachedKindsWatchCollectionByIDStreamsOneRecord(t *testing.T) {
	t.Parallel()

	d := newRunningDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	obj := createKind(t, d, cache.ID, deploymentsSpec)
	createKind(t, d, cache.ID, podsSpec)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stream, err := serviceOver(t, d).CachedKinds().WatchCollectionByID(ctx, ClusterCachedKindID(obj.ID))
	require.NoError(t, err)

	f := testutil.Recv(t, stream.Frames, "the snapshot frame")
	require.NotNil(t, f.Kind)
	assert.Equal(t, "deployments", f.Kind.Spec.Resource, "the other kind is not this record")
	assert.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)
}

// Scoped to one cache: another cache's kinds must not reach this read.
func TestCachedKindsListByCache(t *testing.T) {
	t.Parallel()

	d, one := twoCachesTwoResources(t)

	got, err := serviceOver(t, d).CachedKinds().ListByCache(context.Background(), one)

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "deployments", got[0].Spec.Resource, "the other cache's kinds are not this cache's")
	assert.Equal(t, ObjectRef{ID: ObjectID(one), Kind: "ClusterCache"}, got[0].Owner,
		"the owner is the cache — the join key a client already holds from the cache stream")
}

// A cache nothing has discovered kinds for owns none, which reads empty rather than
// failing — the same wait an unsynced cache shows everywhere else.
func TestCachedKindsListByCacheWithNoKindsIsEmpty(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")

	got, err := serviceOver(t, d).CachedKinds().ListByCache(context.Background(), ClusterCacheID(cache.ID))

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestCachedKindsWatchCollectionByCache(t *testing.T) {
	t.Parallel()

	d := newRunningDeps(t)
	cluster := createCluster(t, d, "prod")
	one := createCache(t, d.cacheClient, cluster, "uid-1")
	two := createCache(t, d.cacheClient, cluster, "uid-2")
	createKind(t, d, one.ID, deploymentsSpec)
	createKind(t, d, two.ID, podsSpec)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stream, err := serviceOver(t, d).CachedKinds().WatchCollectionByCache(ctx, ClusterCacheID(one.ID))
	require.NoError(t, err)

	f := testutil.Recv(t, stream.Frames, "the snapshot frame")
	require.NotNil(t, f.Kind)
	assert.Equal(t, "deployments", f.Kind.Spec.Resource, "the other cache's kinds are not on this stream")
	assert.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)
}

// A cache with no kinds yet bookmarks an empty collection: an unsynced cache is
// definitively empty, not pending, and holding the bookmark back would render a populated
// table as loading for as long as discovery takes.
func TestCachedKindsWatchCollectionByCacheWithNoKindsBookmarksEmpty(t *testing.T) {
	t.Parallel()

	d := newRunningDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stream, err := serviceOver(t, d).CachedKinds().WatchCollectionByCache(ctx, ClusterCacheID(cache.ID))
	require.NoError(t, err)

	assert.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)
}

// clearKindRows is what both teardown paths go through, and it must reach exactly the
// kind it was handed: the cookie is the observable half — a kind whose position is gone
// cold-syncs, and one whose survived does not.
func TestClearKindRowsClearsOnlyThatKind(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stores := newFakeKubestore(t)
	store, _, err := stores.OpenExisting(1)
	require.NoError(t, err)
	require.NoError(t, store.SetCookie(ctx, "apps/v1", "deployments", "10"))
	require.NoError(t, store.SetCookie(ctx, "v1", "pods", "20"))
	store.Release()

	require.NoError(t, clearKindRows(ctx, stores, 1, deploymentsSpec))

	store, _, err = stores.OpenExisting(1)
	require.NoError(t, err)
	defer store.Release()
	_, ok, err := store.Cookie(ctx, "apps/v1", "deployments")
	require.NoError(t, err)
	assert.False(t, ok, "the kind's own position survived its clear")
	_, ok, err = store.Cookie(ctx, "v1", "pods")
	require.NoError(t, err)
	assert.True(t, ok, "another kind's position went with it")
}

// A cache with no file has nothing to clear, and finding that out must not create one.
func TestClearKindRowsSkipsACacheWithNoFile(t *testing.T) {
	t.Parallel()

	stores := newFakeKubestore(t)
	stores.noFile = true

	require.NoError(t, clearKindRows(context.Background(), stores, 1, deploymentsSpec))
}

// The per-kind clear owes the same re-arm as the cache-wide one: the worker is stopped
// before the rows are touched, so a clear that fails has to ask the record to reconcile
// anyway — its pass is what arms the worker again, and the rows it failed to remove are
// still there being served.

// A client subscribes on the frame announcing the cache, which is before any kind has been
// discovered for it. That is a wait, not an empty collection: the bookmark closes a
// snapshot of none, and the kinds are reported as they land.
func TestCachedKindsWatchCollectionByCacheReportsKindsThatArriveLater(t *testing.T) {
	t.Parallel()

	d := newRunningDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stream, err := serviceOver(t, d).CachedKinds().WatchCollectionByCache(ctx, ClusterCacheID(cache.ID))
	require.NoError(t, err)
	require.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)

	// Discovery, arriving after the subscription.
	obj := createKind(t, d, cache.ID, deploymentsSpec)

	got := testutil.Recv(t, stream.Frames, "the kind that landed after the snapshot")
	assert.Equal(t, CollectionWatchFrameAdded, got.Type)
	require.NotNil(t, got.Kind)
	assert.Equal(t, ClusterCachedKindID(obj.ID), got.Kind.ID)
}

// kindControllerClient stands in for the client beehive binds to the object being reconciled.
// The embedded interface is nil: a method the pass grows shows up as a panic, not as silence.
type kindControllerClient struct {
	beehive.ControllerClient[ClusterCachedKindStatus]
	events []beehive.EventSpec
	// eventErr fails the one write a pass makes through this client, which is the only
	// way to reach what a pass does when its verdict will not land.
	eventErr error
}

func (c *kindControllerClient) AddEvent(_ context.Context, event beehive.EventSpec) error {
	c.events = append(c.events, event)
	return c.eventErr
}

// reconcileKind runs one kind pass the way beehive would.
func reconcileKind(t *testing.T, d deps, obj *beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus]) *kindControllerClient {
	t.Helper()
	client := &kindControllerClient{}
	res := (&clusterCachedKindController{deps: d}).Reconcile(context.Background(), client, obj)
	require.Equal(t, beehive.Settled(), res)
	return client
}

// oneCachedKind stores a cache with one kind under it and hands back both.
func oneCachedKind(t *testing.T) (deps, ClusterCacheID, *beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus]) {
	t.Helper()
	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	return d, ClusterCacheID(cache.ID), createKind(t, d, cache.ID, deploymentsSpec)
}

// The record is what arms its own kind: kubesync decides what EXISTS, and a record decides
// what is MIRRORED.
func TestKindPassArmsItsOwnSync(t *testing.T) {
	t.Parallel()

	d, cacheID, kind := oneCachedKind(t)

	reconcileKind(t, d, kind)

	assert.Equal(t, []kubesync.KindKey{{
		CacheID: int64(cacheID),
		Kind: kubestore.Kind{
			APIVersion: "apps/v1", Kind: "Deployment", Resource: "deployments",
		},
	}}, syncFake(d).armedKinds(int64(cacheID)))
}

// **Pause is not deletion, and the distinction is the whole feature.** The worker is
// joined, and the rows it already wrote stay readable.
func TestKindPassStopsAPausedKindWithoutClearingItsRows(t *testing.T) {
	t.Parallel()

	d, cacheID, kind := oneCachedKind(t)
	reconcileKind(t, d, kind)

	ctx := context.Background()
	store, _, err := d.kubestoreMgr.OpenExisting(int64(cacheID))
	require.NoError(t, err)
	require.NoError(t, store.SetCookie(ctx, "apps/v1", "deployments", "10"))
	store.Release()

	reconcileKind(t, d, pausedKind(t, d, kind))

	assert.Empty(t, syncFake(d).armedKinds(int64(cacheID)), "the worker is joined")
	store, _, err = d.kubestoreMgr.OpenExisting(int64(cacheID))
	require.NoError(t, err)
	defer store.Release()
	_, ok, err := store.Cookie(ctx, "apps/v1", "deployments")
	require.NoError(t, err)
	assert.True(t, ok, "what the kind already cached survives the pause")
}

// pausedKind flips one record's switch and reads it back, standing in for the setter.
func pausedKind(t *testing.T, d deps, obj *beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus]) *beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus] {
	t.Helper()
	spec := obj.Spec
	spec.Paused = true
	updated, err := d.kindClient.Update(context.Background(), obj.ID, spec)
	require.NoError(t, err)
	return updated
}

// **Forget first, then clear.** Clearing ahead of the join would race a relist page landing
// behind it, leaving rows for a kind nothing syncs any more.
func TestKindPassForgetsTheSyncBeforeItClearsTheRows(t *testing.T) {
	t.Parallel()

	d, cacheID, kind := oneCachedKind(t)
	reconcileKind(t, d, kind)

	ctx := context.Background()
	store, _, err := d.kubestoreMgr.OpenExisting(int64(cacheID))
	require.NoError(t, err)
	require.NoError(t, store.SetCookie(ctx, "apps/v1", "deployments", "10"))
	store.Release()

	require.NoError(t, d.kindClient.Delete(ctx, kind.ID))
	obj, err := d.kindClient.Get(ctx, kind.ID)
	require.NoError(t, err)
	reconcileKind(t, d, obj)

	assert.Empty(t, syncFake(d).armedKinds(int64(cacheID)), "nothing syncs the kind that is going")
	store, _, err = d.kubestoreMgr.OpenExisting(int64(cacheID))
	require.NoError(t, err)
	defer store.Release()
	_, ok, err := store.Cookie(ctx, "apps/v1", "deployments")
	require.NoError(t, err)
	assert.False(t, ok, "the rows and the position went with the record")
}

// A record whose cache is already gone skips both: the file went with the cache, and there
// is nothing left to stop or to clear.
func TestKindPassSkipsTheClearWhenTheCacheIsGone(t *testing.T) {
	t.Parallel()

	d, _, kind := oneCachedKind(t)
	ctx := context.Background()
	require.NoError(t, d.kindClient.Delete(ctx, kind.ID))
	obj, err := d.kindClient.Get(ctx, kind.ID)
	require.NoError(t, err)
	kubestoreFake(d).noFile = true

	reconcileKind(t, d, obj)
}

// A record whose cache record is gone still withdraws its registration. Nothing else is left
// to: the cache's pass has been and gone, and the id survives only in the record's own name.
func TestKindPassForgetsTheSyncWhenItsCacheRecordIsGone(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	cacheID := ClusterCacheID(7)
	syncFake(d).TrackKind(int64(cacheID), toKubestoreKind(deploymentsSpec))
	now := time.Now()

	reconcileKind(t, d, &beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus]{
		ID: 999,
		Name: ClusterCachedKindName(
			beehive.ObjectID(cacheID), deploymentsSpec.APIVersion, deploymentsSpec.Resource),
		Spec:                deploymentsSpec,
		DeletionRequestedAt: &now,
	})

	assert.Empty(t, syncFake(d).armedKinds(int64(cacheID)))
}

// One kind's transitions land on its own timeline, which is what makes a cache's hundred
// kinds legible: the parent's carries only what the cache itself records.
func TestKindPassLogsItsOwnVerdict(t *testing.T) {
	t.Parallel()

	d, cacheID, kind := oneCachedKind(t)
	syncFake(d).setKindState(int64(cacheID), kubestore.Kind{
		APIVersion: "apps/v1", Kind: "Deployment", Resource: "deployments",
	}, kubesync.KindState{Reason: kubesync.ReasonSyncFailed, Message: "deployments is forbidden"})

	client := reconcileKind(t, d, kind)

	require.Len(t, client.events, 1)
	assert.Equal(t, categorySync, client.events[0].Category)
	assert.Equal(t, kubesync.ReasonSyncFailed, client.events[0].Reason)
	assert.Equal(t, beehive.EventWarning, client.events[0].Type)
}

// **The reason comes from the spec, never from kubesync.** A forgotten kind has no state,
// so the verdict has to be decided ahead of the read that would come back empty — and the
// timeline is where a user looks to see the pause took.
func TestKindPassLogsAPausedVerdictWithNoWorkerToAsk(t *testing.T) {
	t.Parallel()

	d, _, kind := oneCachedKind(t)

	client := reconcileKind(t, d, pausedKind(t, d, kind))

	require.Len(t, client.events, 1)
	assert.Equal(t, categorySync, client.events[0].Category)
	assert.Equal(t, ReasonPaused, client.events[0].Reason)
	assert.Equal(t, beehive.EventNormal, client.events[0].Type)
}

// A kind whose worker has answered nothing has no verdict to log — a cache that is paused,
// or one whose sweep has not reached this kind yet.
func TestKindPassLogsNothingBeforeItsWorkerAnswers(t *testing.T) {
	t.Parallel()

	d, _, kind := oneCachedKind(t)

	client := reconcileKind(t, d, kind)

	assert.Empty(t, client.events)
}

// The pass writes no condition: the verdict is the state watch's, and a stored one would serve a
// dead process's answer until the passes caught up.
func TestKindPassWritesNoCondition(t *testing.T) {
	t.Parallel()

	d, cacheID, kind := oneCachedKind(t)
	syncFake(d).setKindState(int64(cacheID), kubestore.Kind{
		APIVersion: "apps/v1", Kind: "Deployment", Resource: "deployments",
	}, kubesync.KindState{Reason: kubesync.ReasonWatching})

	reconcileKind(t, d, kind)

	obj, err := d.kindClient.Get(context.Background(), kind.ID)
	require.NoError(t, err)
	assert.Empty(t, obj.Conditions)
}

// The wire keeps the positive form, matching the cluster's own two toggles; the stored
// field is its opposite so the zero value can be the default. One negation, at the setter
// and at the projection.
func TestCachedKindsSetSyncEnabledFlipsThePauseBothWays(t *testing.T) {
	t.Parallel()

	d, _, kind := oneCachedKind(t)
	ctx := context.Background()
	api := serviceOver(t, d).CachedKinds()

	got, err := api.SetSyncEnabled(ctx, ClusterCachedKindID(kind.ID), false)

	require.NoError(t, err)
	assert.True(t, got.Spec.Paused, "syncEnabled false is a pause")

	got, err = api.SetSyncEnabled(ctx, ClusterCachedKindID(kind.ID), true)

	require.NoError(t, err)
	assert.False(t, got.Spec.Paused)
	assert.Equal(t, "deployments", got.Spec.Resource, "and the catalog fields ride through it")
}

// The setter takes the whole spec, so an id naming nothing has to report it rather than
// create a record with no catalog fields.
func TestCachedKindsSetSyncEnabledReportsAnUnknownID(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)

	_, err := serviceOver(t, d).CachedKinds().SetSyncEnabled(context.Background(), 404, false)

	assert.ErrorIs(t, err, ErrNotFound)
}

// One kind's clear needs the same hold as the cache-wide one, scoped to the kind: a worker
// that resumed from its cookie afterwards would apply deltas to rows nothing cold-listed.
func TestCachedKindsClearRunsInsideTheSyncsHold(t *testing.T) {
	t.Parallel()

	d, cacheID, kind := oneCachedKind(t)

	var heldAtClear bool
	kubestoreFake(d).onOpen = func(int64) {
		heldAtClear = slices.ContainsFunc(syncFake(d).stoppedKinds(), func(key kubesync.KindKey) bool {
			return key.CacheID == int64(cacheID) && key.Resource == "deployments"
		})
	}
	_, err := serviceOver(t, d).CachedKinds().Clear(context.Background(), ClusterCachedKindID(kind.ID))

	require.NoError(t, err)
	assert.True(t, heldAtClear, "the kind's worker is stopped before its rows go")
}

// The record's name is the only place the cache id survives a teardown, so reading it back
// has to refuse anything that is not one — a name the parser guessed at would send a clear
// at whatever cache that number named.
func TestCacheIDInKindNameRefusesANameThatIsNotOne(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"cluster/1/v1/pods", // another kind's name entirely
		"cachedkind/1",      // the prefix, but nothing after the id
		"cachedkind/x/v1/pods",
	} {
		_, ok := cacheIDInKindName(name)

		assert.False(t, ok, name)
	}
}

// The name a record is created under is what the parser reads back.
func TestCacheIDInKindNameReadsWhatTheNameCarries(t *testing.T) {
	t.Parallel()

	id, ok := cacheIDInKindName(ClusterCachedKindName(7, "apps/v1", "deployments"))

	require.True(t, ok)
	assert.Equal(t, int64(7), id)
}

// A record collected between a client reading its id off a watch frame and acting on it
// is an ordinary race, not a bad request — the clear has nothing to do.
func TestCachedKindsClearOfAnUnknownIdIsNotAnError(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)

	got, err := serviceOver(t, d).CachedKinds().Clear(context.Background(), 404)

	require.NoError(t, err)
	assert.Nil(t, got)
}

// A kind whose cache has gone has no rows left to clear: the file went with the cache, and
// opening one would create the very file the clear is trying to be rid of.
func TestCachedKindsClearOfAKindWhoseCacheIsGoneJustReturnsIt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	obj := createKind(t, d, cache.ID, deploymentsSpec)
	require.NoError(t, d.cacheClient.Delete(ctx, cache.ID))

	got, err := serviceOver(t, d).CachedKinds().Clear(ctx, ClusterCachedKindID(obj.ID))

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, ClusterCachedKindID(obj.ID), got.ID)
}

// A clear that cannot reach the cache's file is reported: answering as though the rows
// went would leave the user looking at a kind they just cleared, still full.
func TestCachedKindsClearReportsAStoreItCannotReach(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	obj := createKind(t, d, cache.ID, deploymentsSpec)
	d.kubestoreMgr.(*fakeKubestore).err = assert.AnError

	_, err := serviceOver(t, d).CachedKinds().Clear(context.Background(), ClusterCachedKindID(obj.ID))

	assert.ErrorContains(t, err, "clear cached kind")
}

// The switch is per record, so a record that is gone is the boundary's own not-found
// rather than beehive's — the resolver above maps one and not the other.
func TestCachedKindsSetSyncEnabledOnAnUnknownIdIsNotFound(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)

	_, err := serviceOver(t, d).CachedKinds().SetSyncEnabled(context.Background(), 404, false)

	assert.ErrorIs(t, err, ErrNotFound)
}

// A pass that cannot write its verdict fails rather than settling: a settled generation
// beehive does not come back to would leave the kind's timeline permanently short of the
// transition, with nothing to say why.
func TestKindPassFailsWhenItsVerdictWillNotLand(t *testing.T) {
	t.Parallel()

	for _, paused := range []bool{false, true} {
		t.Run("paused="+strconv.FormatBool(paused), func(t *testing.T) {
			d, cacheID, kind := oneCachedKind(t)
			d.kubesyncSvc.(*fakeKubesync).setKindState(int64(cacheID), toKubestoreKind(kind.Spec),
				kubesync.KindState{Reason: kubesync.ReasonWatching})
			kind.Spec.Paused = paused

			res := (&clusterCachedKindController{deps: d}).Reconcile(
				context.Background(), &kindControllerClient{eventErr: assert.AnError}, kind)

			assert.NotEqual(t, beehive.Settled(), res)
		})
	}
}

// A dying kind's rows go with it, and a store that will not give them up fails the pass —
// settling would leave rows for a kind no record names, which nothing would ever collect.
func TestKindTeardownFailsWhenTheRowsWillNotClear(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d, _, kind := oneCachedKind(t)
	require.NoError(t, d.kindClient.Delete(ctx, kind.ID))
	dying, err := d.kindClient.Get(ctx, kind.ID, beehive.LoadOwner())
	require.NoError(t, err)
	d.kubestoreMgr.(*fakeKubestore).err = assert.AnError

	res := (&clusterCachedKindController{deps: d}).Reconcile(ctx, &kindControllerClient{}, dying)

	assert.NotEqual(t, beehive.Settled(), res)
}

// An owner edge that was never loaded is a programming error, not an unowned record:
// projecting one as owner-less would put a record on the wire with no join key, and the
// client would render it under no cache at all.
func TestProjectingAKindWhoseOwnerWasNotLoadedIsReported(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	obj := createKind(t, d, cache.ID, deploymentsSpec)
	unloaded, err := d.kindClient.Get(ctx, obj.ID)
	require.NoError(t, err)

	_, oneErr := toClusterCachedKind(unloaded)
	_, manyErr := toClusterCachedKinds([]*beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus]{unloaded})
	_, frameErr := kindWatch.frame(CollectionWatchFrameAdded, unloaded)
	_, _, refErr := cachedKindsAPI{serviceOver(t, d)}.cacheIDForKind(unloaded)

	assert.Error(t, oneErr)
	assert.Error(t, manyErr)
	assert.Error(t, frameErr)
	assert.ErrorContains(t, refErr, "owner")
}

// A departure carries what the row said before it went, so a client can render the row it
// is dropping — and a change that carries no object still yields a frame with the id,
// which is what the drop is keyed by.
func TestADepartedKindFrameCarriesTheSpecWhenTheChangeHasOne(t *testing.T) {
	t.Parallel()

	withObject := kindWatch.departed(beehive.ObjectChange[ClusterCachedKindSpec, ClusterCachedKindStatus]{
		ID:     7,
		Object: &beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus]{Spec: deploymentsSpec},
	})
	withNone := kindWatch.departed(beehive.ObjectChange[ClusterCachedKindSpec, ClusterCachedKindStatus]{ID: 7})

	assert.Equal(t, deploymentsSpec, withObject.Kind.Spec)
	assert.Equal(t, ClusterCachedKindID(7), withNone.Kind.ID)
	assert.Zero(t, withNone.Kind.Spec)
}

// A kind with no cache above it has no rows anywhere: the record is served back
// unchanged rather than the clear reaching for a file that was never named.
func TestCachedKindsClearOfAKindWithNoOwnerJustReturnsIt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d := newTestDeps(t)
	obj, _, err := d.kindClient.CreateOrUpdate(ctx, "orphan-kind", deploymentsSpec)
	require.NoError(t, err)

	got, err := serviceOver(t, d).CachedKinds().Clear(ctx, ClusterCachedKindID(obj.ID))

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, ClusterCachedKindID(obj.ID), got.ID)
}

// The record can be collected between the read and the write, so a write that lands on
// one already going is reported rather than silently doing nothing.
func TestCachedKindsSetSyncEnabledReportsARecordAlreadyGoing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	obj := createKind(t, d, cache.ID, deploymentsSpec)
	require.NoError(t, d.kindClient.Delete(ctx, obj.ID))

	_, err := serviceOver(t, d).CachedKinds().SetSyncEnabled(ctx, ClusterCachedKindID(obj.ID), false)

	assert.ErrorContains(t, err, "update cached kind")
}

// The record can also go between the write and the read back, which reports it missing
// rather than answering with the record as it was before the switch.
func TestCachedKindsSetSyncEnabledReportsARecordGoneBeforeTheReadBack(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	obj := createKind(t, d, cache.ID, deploymentsSpec)
	d.kindClient = &collectedAfterUpdate{Client: d.kindClient}

	_, err := serviceOver(t, d).CachedKinds().SetSyncEnabled(ctx, ClusterCachedKindID(obj.ID), false)

	assert.ErrorIs(t, err, ErrNotFound)
	assert.ErrorContains(t, err, "get cached kind")
}

// collectedAfterUpdate is a kind client whose records read as collected once one has been
// updated, standing in for a cache teardown landing between the write and the read back.
type collectedAfterUpdate struct {
	beehive.Client[ClusterCachedKindSpec, ClusterCachedKindStatus]
	updated bool
}

func (c *collectedAfterUpdate) Update(ctx context.Context, id beehive.ObjectID, spec ClusterCachedKindSpec) (*beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus], error) {
	c.updated = true
	return c.Client.Update(ctx, id, spec)
}

func (c *collectedAfterUpdate) Get(ctx context.Context, id beehive.ObjectID, loads ...beehive.LoadOption) (*beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus], error) {
	if c.updated {
		return nil, beehive.ErrNotFound
	}
	return c.Client.Get(ctx, id, loads...)
}

// A pass that cannot read which cache it belongs to fails rather than settling: settling
// would leave the kind unarmed, and beehive would not come back to it.
func TestKindPassFailsWhenItsCacheCannotBeRead(t *testing.T) {
	t.Parallel()

	d, closeStore := newTestDepsWithABreakableStore(t)
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	kind := createKind(t, d, cache.ID, deploymentsSpec)
	closeStore()

	res := (&clusterCachedKindController{deps: d}).Reconcile(
		context.Background(), &kindControllerClient{}, kind)

	assert.NotEqual(t, beehive.Settled(), res)
}
