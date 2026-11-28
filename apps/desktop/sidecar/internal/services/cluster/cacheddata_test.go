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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
)

// dataFixture is a cluster with a cache, and that cache's real store to seed.
type dataFixture struct {
	t         *testing.T
	d         deps
	clusterID ClusterID
	cacheID   ClusterCacheID
	store     *kubestore.Store
}

// newDataFixture builds the record chain the gate walks, and opens the cache's file the
// way a worker would.
func newDataFixture(t *testing.T) *dataFixture {
	t.Helper()
	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")

	store, err := kubestoreFake(d).mgr.OpenOrCreate(int64(cache.ID))
	require.NoError(t, err)
	t.Cleanup(func() {
		if store != nil {
			store.Release()
		}
	})

	return &dataFixture{
		t:         t,
		d:         d,
		clusterID: clusterIDOf(cluster),
		cacheID:   ClusterCacheID(cache.ID),
		store:     store,
	}
}

// data is the family under test.
func (f *dataFixture) data() CachedData { return serviceOver(f.t, f.d).CachedData() }

// stopWorkers releases the cache's only claim, which is what a pause does: the workers are
// forgotten, their claims go, and the last release closes the file. The rows stay on disk.
func (f *dataFixture) stopWorkers() {
	f.t.Helper()
	require.NotNil(f.t, f.store)
	f.store.Release()
	f.store = nil
	require.False(f.t, cacheIsOpen(kubestoreFake(f.d).mgr, int64(f.cacheID)), "the file stayed open")
}

// A paused cache still holds its rows, and reading them is not what pausing stops. Nothing
// keeps its file open once the workers are forgotten, so a read that bound only to an
// already-open file would show an empty nav over a full cache — on a pause, and on every
// restart until the first worker arms.
func TestCachedDataReadsACacheWhoseWorkersStopped(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataFixture(t)
	require.NoError(t, f.store.SyncKinds(ctx, []kubestore.KindRow{
		{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: kubestore.ScopeNamespaced},
	}, true, 7))
	require.NoError(t, f.store.ApplyChange(ctx, kubestore.Kind{
		APIVersion: "v1", Kind: "Pod", Resource: "pods",
	}, watch.Added, dataPod("uid-1", "api-0", "42")))

	f.stopWorkers()

	kinds, err := f.data().ListKinds(ctx, f.clusterID, f.cacheID, "")
	require.NoError(t, err)
	require.Len(t, kinds, 1, "ListKinds lost a paused cache's catalog")
	assert.Equal(t, 1, kinds[0].Count)

	stream, err := f.data().WatchObjectCollection(ctx, f.clusterID, f.cacheID, "v1", "pods")
	require.NoError(t, err)
	first := testutil.Recv(t, stream.Frames, "the object")
	require.NotNil(t, first.Object, "WatchObjects lost a paused cache's rows")
	assert.Equal(t, "uid-1", first.Object.UID)
}

// pod is a Pod body the store will accept.
func dataPod(uid, name, rv string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{
			"uid": uid, "name": name, "namespace": "prod", "resourceVersion": rv,
		},
	}}
}

// dataEvent is an Event body the store will accept, at a given resourceVersion and count.
func dataEvent(uid, rv string, count int64) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Event",
		"metadata":       map[string]any{"uid": uid, "name": uid, "resourceVersion": rv},
		"involvedObject": map[string]any{"uid": "pod-1", "kind": "Pod", "name": "api-0"},
		"reason":         "BackOff", "message": "restarting", "type": "Warning", "count": count,
	}}
}

// A pair that does not resolve is definitively empty rather than an error or a wait: the
// cache is gone, or was never this cluster's, and no row will ever arrive for it.
func TestCachedDataGatesAnUnknownPair(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataFixture(t)

	kinds, err := f.data().ListKinds(ctx, f.clusterID, 9999, "")
	require.NoError(t, err)
	assert.Empty(t, kinds)

	stream, err := f.data().WatchKindCollection(ctx, f.clusterID, 9999, "")
	require.NoError(t, err)
	assert.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)
	_, open := <-stream.Frames
	assert.False(t, open, "a scope that can never be filled stayed open")
}

// A cache owned by another cluster is the same answer: the gate is the pair, not the id.
func TestCachedDataGatesACacheOfAnotherCluster(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	f := newDataFixture(t)

	kinds, err := f.data().ListKinds(ctx, ClusterID("9999"), f.cacheID, "")
	require.NoError(t, err)
	assert.Empty(t, kinds)
}

// A cache whose file exists but will not open — corrupt, unreadable, out of descriptors —
// is a storage failure, not an empty cache. Answering "no kinds" would report the cluster
// as serving nothing and give a caller nothing to act on.
func TestListKindsReportsAStoreThatWillNotOpen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	f := newDataFixture(t)
	boom := errors.New("permission denied")
	kubestoreFake(f.d).err = boom

	_, err := f.data().ListKinds(ctx, f.clusterID, f.cacheID, "")

	assert.ErrorIs(t, err, boom)
}

// ListKinds is one read: what ClusterCache.kinds resolves.
func TestListKindsReadsTheCatalog(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	f := newDataFixture(t)
	require.NoError(t, f.store.SyncKinds(ctx, []kubestore.KindRow{
		{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: kubestore.ScopeNamespaced},
	}, true, 7))

	got, err := f.data().ListKinds(ctx, f.clusterID, f.cacheID, "")

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, ClusterCachedDataKind{
		APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: "Namespaced",
	}, got[0])
}

// An advertised kind shows before its worker has synced anything — Count 0, not absent,
// which is what the nav promises.
func TestWatchKindCollectionStreamsTheCatalogWithCounts(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataFixture(t)
	require.NoError(t, f.store.SyncKinds(ctx, []kubestore.KindRow{
		{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: kubestore.ScopeNamespaced},
	}, true, 7))

	stream, err := f.data().WatchKindCollection(ctx, f.clusterID, f.cacheID, "")
	require.NoError(t, err)

	first := testutil.Recv(t, stream.Frames, "the kind")
	require.NotNil(t, first.Kind)
	assert.Equal(t, 0, first.Kind.Count)
	assert.Equal(t, f.cacheID, first.CacheID, "the frame carries no provenance")
	assert.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)

	require.NoError(t, f.store.ApplyChange(ctx, kubestore.Kind{
		APIVersion: "v1", Kind: "Pod", Resource: "pods",
	}, watch.Added, dataPod("uid-1", "api-0", "42")))

	moved := testutil.Recv(t, stream.Frames, "the count moving")
	assert.Equal(t, CollectionWatchFrameModified, moved.Type)
	assert.Equal(t, 1, moved.Kind.Count)
}

// Under a namespace a namespaced kind counts its objects there alone, and every frame
// names the namespace, so a client can drop one from a watch of another.
func TestWatchKindCollectionCountsInANamespace(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataFixture(t)
	require.NoError(t, f.store.SyncKinds(ctx, []kubestore.KindRow{
		{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: kubestore.ScopeNamespaced},
	}, true, 7))
	podKind := kubestore.Kind{APIVersion: "v1", Kind: "Pod", Resource: "pods"}
	require.NoError(t, f.store.ApplyChange(ctx, podKind, watch.Added, dataPod("uid-1", "api-0", "42")))

	stream, err := f.data().WatchKindCollection(ctx, f.clusterID, f.cacheID, "staging")
	require.NoError(t, err)

	first := testutil.Recv(t, stream.Frames, "the kind")
	require.NotNil(t, first.Kind)
	assert.Equal(t, 0, first.Kind.Count, "the pod is in prod")
	assert.Equal(t, "staging", first.Namespace)
	bookmark := testutil.Recv(t, stream.Frames, "the bookmark")
	assert.Equal(t, CollectionWatchFrameBookmark, bookmark.Type)
	assert.Equal(t, "staging", bookmark.Namespace)

	staging := dataPod("uid-2", "api-1", "43")
	staging.SetNamespace("staging")
	require.NoError(t, f.store.ApplyChange(ctx, podKind, watch.Added, staging))

	moved := testutil.Recv(t, stream.Frames, "the count moving")
	assert.Equal(t, CollectionWatchFrameModified, moved.Type)
	assert.Equal(t, 1, moved.Kind.Count)
	assert.Equal(t, "staging", moved.Namespace)
}

// ListKinds counts in a namespace as the watch does.
func TestListKindsCountsInANamespace(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataFixture(t)
	require.NoError(t, f.store.SyncKinds(ctx, []kubestore.KindRow{
		{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: kubestore.ScopeNamespaced},
	}, true, 7))
	require.NoError(t, f.store.ApplyChange(ctx, kubestore.Kind{
		APIVersion: "v1", Kind: "Pod", Resource: "pods",
	}, watch.Added, dataPod("uid-1", "api-0", "42")))

	prod, err := f.data().ListKinds(ctx, f.clusterID, f.cacheID, "prod")
	require.NoError(t, err)
	staging, err := f.data().ListKinds(ctx, f.clusterID, f.cacheID, "staging")
	require.NoError(t, err)

	require.Len(t, prod, 1)
	assert.Equal(t, 1, prod[0].Count)
	require.Len(t, staging, 1)
	assert.Equal(t, 0, staging[0].Count, "the pod is in prod")
}

// An unknown pair's empty answer names the namespace too.
func TestWatchKindCollectionNamesTheNamespaceOnAnEmptyAnswer(t *testing.T) {
	t.Parallel()

	f := newDataFixture(t)

	stream, err := f.data().WatchKindCollection(t.Context(), f.clusterID, 9999, "staging")
	require.NoError(t, err)

	assert.Equal(t, "staging", testutil.Recv(t, stream.Frames, "the bookmark").Namespace)
}

// The store carries a CRD's printer columns as JSON, and this projection is where they become
// the typed value the wire serves.
func TestWatchKindCollectionDecodesPrinterColumns(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataFixture(t)
	require.NoError(t, f.store.SyncKinds(ctx, []kubestore.KindRow{{
		APIVersion: "example.com/v1", Kind: "Widget", Resource: "widgets",
		Scope: kubestore.ScopeNamespaced, IsCRD: true,
		PrinterColumns: `[{"name":"Replicas","type":"integer","jsonPath":".spec.replicas","priority":1}]`,
	}}, true, 7))

	stream, err := f.data().WatchKindCollection(ctx, f.clusterID, f.cacheID, "")
	require.NoError(t, err)

	first := testutil.Recv(t, stream.Frames, "the kind")
	require.NotNil(t, first.Kind)
	assert.Equal(t, []PrinterColumn{
		{Name: "Replicas", Type: "integer", JSONPath: ".spec.replicas", Priority: 1},
	}, first.Kind.PrinterColumns)
}

// A projection has no error path, so a blob that will not parse yields no columns rather than
// dropping the kind out of the nav. The sidecar is the only writer, so it should never happen —
// the branch is there so "never happens" has an answer.
func TestAMalformedPrinterColumnBlobYieldsNoColumns(t *testing.T) {
	t.Parallel()

	got := toCachedDataKind(kubestore.KindRow{
		APIVersion: "example.com/v1", Kind: "Widget", Resource: "widgets", PrinterColumns: "{not json",
	})

	assert.Equal(t, "Widget", got.Kind)
	assert.Empty(t, got.PrinterColumns)
}

// The kinds watch diffs whole rows, which is what makes an edited CRD reach a client already
// sitting on the dashboard. Decoding anywhere before the diff would leave the columns out of the
// compared value, and the table would only pick them up on the next reconnect.
func TestWatchKindCollectionReportsAColumnsOnlyChange(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataFixture(t)
	row := kubestore.KindRow{
		APIVersion: "example.com/v1", Kind: "Widget", Resource: "widgets",
		Scope: kubestore.ScopeNamespaced, IsCRD: true,
	}
	require.NoError(t, f.store.SyncKinds(ctx, []kubestore.KindRow{row}, true, 7))

	stream, err := f.data().WatchKindCollection(ctx, f.clusterID, f.cacheID, "")
	require.NoError(t, err)
	testutil.Recv(t, stream.Frames, "the kind")
	require.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)

	edited := row
	edited.PrinterColumns = `[{"name":"Phase","type":"string","jsonPath":".status.phase","priority":0}]`
	require.NoError(t, f.store.SyncKinds(ctx, []kubestore.KindRow{edited}, true, 8))

	moved := testutil.Recv(t, stream.Frames, "the edited CRD")
	assert.Equal(t, CollectionWatchFrameModified, moved.Type)
	assert.Equal(t, "Phase", moved.Kind.PrinterColumns[0].Name)
}

// The objects watch carries the kind as provenance beside the cache, since one client
// switching resources within a cache has to reject the previous subscription's stragglers.
func TestWatchObjectCollectionCarriesItsKindAsProvenance(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataFixture(t)
	require.NoError(t, f.store.SyncKinds(ctx, []kubestore.KindRow{
		{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: kubestore.ScopeNamespaced},
	}, true, 7))
	require.NoError(t, f.store.ApplyChange(ctx, kubestore.Kind{
		APIVersion: "v1", Kind: "Pod", Resource: "pods",
	}, watch.Added, dataPod("uid-1", "api-0", "42")))

	stream, err := f.data().WatchObjectCollection(ctx, f.clusterID, f.cacheID, "v1", "pods")
	require.NoError(t, err)

	first := testutil.Recv(t, stream.Frames, "the object")
	require.NotNil(t, first.Object)
	assert.Equal(t, "uid-1", first.Object.UID)
	assert.Equal(t, f.cacheID, first.CacheID)
	assert.Equal(t, "v1", first.APIVersion)
	assert.Equal(t, "pods", first.Resource)
	assert.Contains(t, string(first.Object.RawJSON), `"uid":"uid-1"`, "an Added frame carries the body")
}

// A Deleted frame's object is the key the client removes by, so it stays non-null — but its
// body is dead weight: the client deletes by uid and never looks at the item. Nothing
// else would catch a hydrate that fired on every frame type.
func TestWatchObjectCollectionSendsADeletedFrameWithoutABody(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataFixture(t)
	podKind := kubestore.Kind{APIVersion: "v1", Kind: "Pod", Resource: "pods"}
	require.NoError(t, f.store.SyncKinds(ctx, []kubestore.KindRow{
		{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: kubestore.ScopeNamespaced},
	}, true, 7))
	require.NoError(t, f.store.ApplyChange(ctx, podKind, watch.Added, dataPod("uid-1", "api-0", "42")))

	stream, err := f.data().WatchObjectCollection(ctx, f.clusterID, f.cacheID, "v1", "pods")
	require.NoError(t, err)
	testutil.Recv(t, stream.Frames, "the object")
	require.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)

	require.NoError(t, f.store.ApplyChange(ctx, podKind, watch.Deleted, dataPod("uid-1", "api-0", "42")))

	gone := testutil.Recv(t, stream.Frames, "the removal")
	assert.Equal(t, CollectionWatchFrameDeleted, gone.Type)
	require.NotNil(t, gone.Object, "the client keys the removal off the object")
	assert.Equal(t, "uid-1", gone.Object.UID)
	assert.Empty(t, gone.Object.RawJSON, "a removal carries no body")
}

// A watch narrowed to namespaces sends only their objects, with no body, and a
// change elsewhere sends nothing.
func TestWatchObjectCollectionByNamespacesSendsOnlyItsNamespaces(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataFixture(t)
	podKind := kubestore.Kind{APIVersion: "v1", Kind: "Pod", Resource: "pods"}
	require.NoError(t, f.store.SyncKinds(ctx, []kubestore.KindRow{
		{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: kubestore.ScopeNamespaced},
	}, true, 7))
	elsewhere := func(uid, name, rv string) *unstructured.Unstructured {
		pod := dataPod(uid, name, rv)
		pod.SetNamespace("staging")
		return pod
	}
	require.NoError(t, f.store.ApplyChange(ctx, podKind, watch.Added, dataPod("uid-1", "api-0", "42")))
	require.NoError(t, f.store.ApplyChange(ctx, podKind, watch.Added, elsewhere("uid-2", "api-0", "43")))

	stream, err := f.data().WatchObjectCollectionByNamespaces(ctx, f.clusterID, f.cacheID, "v1", "pods", []string{"prod"})
	require.NoError(t, err)

	first := testutil.Recv(t, stream.Frames, "the object")
	require.NotNil(t, first.Object)
	assert.Equal(t, "uid-1", first.Object.UID)
	assert.Empty(t, first.Object.RawJSON, "a narrowed watch carries no body")
	require.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)

	require.NoError(t, f.store.ApplyChange(ctx, podKind, watch.Added, elsewhere("uid-3", "api-1", "44")))
	require.NoError(t, f.store.ApplyChange(ctx, podKind, watch.Deleted, elsewhere("uid-2", "api-0", "43")))
	require.NoError(t, f.store.ApplyChange(ctx, podKind, watch.Added, dataPod("uid-4", "api-1", "45")))

	next := testutil.Recv(t, stream.Frames, "the next object in prod")
	assert.Equal(t, CollectionWatchFrameAdded, next.Type)
	assert.Equal(t, "uid-4", next.Object.UID)
}

// A guard on the split's cost: the diff read and the body fetch are two queries against a
// moving table, so a row can go between them. The frame serves a null body and the watch
// carries on — the next resync's Deleted frame is the real answer, and failing the stream
// would be reporting a race as a breakage.
func TestHydrateObjectServesNoBodyForARowThatIsGone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	f := newDataFixture(t)

	obj := hydrateObject(ctx, f.store, CollectionWatchFrameAdded, kubestore.ObjectRow{
		UID: "uid-gone", APIVersion: "v1", Kind: "Pod", Namespace: "prod", Name: "api-0",
	})

	assert.Equal(t, "uid-gone", obj.UID)
	assert.Empty(t, obj.RawJSON)
}

// The events watch is its own bus, so an event storm does not drive the object watches.
func TestWatchEventCollectionStreamsTheNewestWindow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataFixture(t)
	require.NoError(t, f.store.ApplyChange(ctx, kubestore.Kind{
		APIVersion: "v1", Kind: "Event", Resource: "events",
	}, watch.Added, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Event",
		"metadata": map[string]any{"uid": "uid-ev", "name": "ev", "namespace": "prod"},
		"reason":   "Pulled", "message": "ok", "type": "Normal",
	}}))

	stream, err := f.data().WatchEventCollection(ctx, f.clusterID, f.cacheID)
	require.NoError(t, err)

	first := testutil.Recv(t, stream.Frames, "the event")
	require.NotNil(t, first.Event)
	assert.Equal(t, "uid-ev", first.Event.UID)
	assert.Equal(t, "Pulled", first.Event.Reason)
	assert.Equal(t, f.cacheID, first.CacheID)
}

// A stored stamp of zero is absence, not the epoch: the field resolvers map a zero time to
// null, and 1970 would sort to the bottom of every window instead.
func TestMillisToTimeKeepsZeroAsAbsence(t *testing.T) {
	t.Parallel()

	assert.True(t, millisToTime(0).IsZero())
	assert.Equal(t, int64(1500), millisToTime(1500).UnixMilli())
}

// The two watches gate on the same pair the reads do, and a pair that can never resolve
// gets a bookmark over an empty collection rather than a stream that waits forever.
func TestTheObjectAndEventWatchesGateOnThePairToo(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataFixture(t)

	objects, err := f.data().WatchObjectCollection(ctx, ClusterID("9999"), f.cacheID, "v1", "pods")
	require.NoError(t, err)
	assert.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, objects.Frames, "the bookmark").Type)

	events, err := f.data().WatchEventCollection(ctx, ClusterID("9999"), f.cacheID)
	require.NoError(t, err)
	assert.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, events.Frames, "the bookmark").Type)

	narrowed, err := f.data().WatchObjectCollectionByNamespaces(ctx, ClusterID("9999"), f.cacheID, "v1", "pods", []string{"prod"})
	require.NoError(t, err)
	assert.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, narrowed.Frames, "the bookmark").Type)
	testutil.WaitClosed(t, narrowed.Frames, "a scope that can never be filled to end")
}

// A cache with no file has never synced anything, which is empty rather than a fault: the
// read never creates one, so there is nothing to report.
func TestListKindsAnswersACacheWithNoFileAsEmpty(t *testing.T) {
	t.Parallel()

	f := newDataFixture(t)
	kubestoreFake(f.d).noFile = true

	kinds, err := f.data().ListKinds(context.Background(), f.clusterID, f.cacheID, "")

	require.NoError(t, err)
	assert.Empty(t, kinds)
}

// A cache whose file goes between the claim and the read is a fault to report — the rows
// were there a moment ago, and answering empty would blank a populated table.
func TestListKindsReportsACatalogItCannotRead(t *testing.T) {
	t.Parallel()

	f := newDataFixture(t)
	store := kubestoreFake(f.d)
	store.afterOpen = func(cacheID int64) { require.NoError(t, store.mgr.Remove(cacheID)) }

	_, err := f.data().ListKinds(context.Background(), f.clusterID, f.cacheID, "")

	assert.ErrorContains(t, err, "kinds")
}

// A cleared kind that relists the same uids is the case deletes-before-writes exists for:
// ClearKind logs a delete per row and the sync writes the same objects back above it, so a
// watch reading both in one burst must end holding the rows. Final state, not frame count —
// the two land in one burst here and in two on a kind large enough to outrun the debounce.
func TestWatchObjectCollectionKeepsRowsThatWereClearedAndRelisted(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataFixture(t)
	podKind := kubestore.Kind{APIVersion: "v1", Kind: "Pod", Resource: "pods"}
	require.NoError(t, f.store.SyncKinds(ctx, []kubestore.KindRow{
		{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: kubestore.ScopeNamespaced},
	}, true, 7))
	require.NoError(t, f.store.ApplyChange(ctx, podKind, watch.Added, dataPod("uid-1", "api-0", "42")))

	stream, err := f.data().WatchObjectCollection(ctx, f.clusterID, f.cacheID, "v1", "pods")
	require.NoError(t, err)
	testutil.Recv(t, stream.Frames, "the object")
	require.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)

	require.NoError(t, f.store.ClearKind(ctx, podKind))
	require.NoError(t, f.store.ApplyChange(ctx, podKind, watch.Added, dataPod("uid-1", "api-0", "43")))

	held := map[string]bool{}
	for range 2 {
		fr := testutil.Recv(t, stream.Frames, "a frame")
		require.NotNil(t, fr.Object)
		if fr.Type == CollectionWatchFrameDeleted {
			delete(held, fr.Object.UID)
			continue
		}
		held[fr.Object.UID] = len(fr.Object.RawJSON) > 0
	}
	assert.Equal(t, map[string]bool{"uid-1": true}, held, "the relisted row was dropped")
}

// A CRD whose Kind is renamed keeps its plural, and the catalog remaps to the new Kind
// (stmtResolveKindRename). The rows and the deletes the old worker logged are keyed by the
// OLD Kind, so a cursor read under the new one sees neither — the watch would hold the old
// rows for as long as it stayed connected.
func TestWatchObjectCollectionDropsRowsWhenTheKindBehindThePluralIsRenamed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataFixture(t)
	was := kubestore.Kind{APIVersion: "v1", Kind: "Widget", Resource: "widgets"}
	require.NoError(t, f.store.SyncKinds(ctx, []kubestore.KindRow{
		{APIVersion: "v1", Kind: "Widget", Resource: "widgets", Scope: kubestore.ScopeNamespaced},
	}, true, 7))
	require.NoError(t, f.store.ApplyChange(ctx, was, watch.Added, dataPod("uid-1", "api-0", "42")))

	stream, err := f.data().WatchObjectCollection(ctx, f.clusterID, f.cacheID, "v1", "widgets")
	require.NoError(t, err)
	testutil.Recv(t, stream.Frames, "the object")
	require.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)

	// The sweep remaps the plural onto the new Kind, and the old kind's worker clears its
	// rows on the way out.
	require.NoError(t, f.store.SyncKinds(ctx, []kubestore.KindRow{
		{APIVersion: "v1", Kind: "Gadget", Resource: "widgets", Scope: kubestore.ScopeNamespaced},
	}, true, 8))
	require.NoError(t, f.store.ClearKind(ctx, was))

	gone := testutil.Recv(t, stream.Frames, "the removal")
	assert.Equal(t, CollectionWatchFrameDeleted, gone.Type)
	assert.Equal(t, "uid-1", gone.Object.UID)
}

// The events watch reads what moved past its cursor like the objects watch does, so a
// re-fired event reaches the client as a Modified rather than waiting for a reconnect.
func TestWatchEventCollectionSendsWhatMovedAfterTheSnapshot(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataFixture(t)
	eventsKind := kubestore.Kind{APIVersion: "v1", Kind: "Event", Resource: "events"}
	require.NoError(t, f.store.ApplyChange(ctx, eventsKind, watch.Added, dataEvent("ev-1", "10", 1)))

	stream, err := f.data().WatchEventCollection(ctx, f.clusterID, f.cacheID)
	require.NoError(t, err)
	testutil.Recv(t, stream.Frames, "the event")
	require.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)

	require.NoError(t, f.store.ApplyChange(ctx, eventsKind, watch.Modified, dataEvent("ev-1", "11", 2)))

	fr := testutil.Recv(t, stream.Frames, "the re-fired event")
	assert.Equal(t, CollectionWatchFrameModified, fr.Type)
	require.NotNil(t, fr.Event)
	assert.Equal(t, 2, fr.Event.Count)
}

// ListObjects is the snapshot WatchObjectCollection would open with, bodies never loaded: a
// reader that wants names alone does not pay for a body per row.
func TestListObjectsReadsTheIdentityRowsWithoutBodies(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	f := newDataFixture(t)
	require.NoError(t, f.store.SyncKinds(ctx, []kubestore.KindRow{
		{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: kubestore.ScopeNamespaced},
	}, true, 7))
	require.NoError(t, f.store.ApplyChange(ctx, kubestore.Kind{APIVersion: "v1", Kind: "Pod", Resource: "pods"},
		watch.Added, dataPod("uid-1", "api-0", "42")))
	require.NoError(t, f.store.ApplyChange(ctx, kubestore.Kind{APIVersion: "v1", Kind: "Pod", Resource: "pods"},
		watch.Added, dataPod("uid-2", "api-1", "43")))

	got, ok, err := f.data().ListObjects(ctx, f.clusterID, f.cacheID, "v1", "pods")

	require.NoError(t, err)
	assert.True(t, ok)
	require.Len(t, got, 2)
	names := []string{got[0].Name, got[1].Name}
	assert.ElementsMatch(t, []string{"api-0", "api-1"}, names)
	assert.Equal(t, "prod", got[0].Namespace)
	assert.Empty(t, got[0].RawJSON, "no body is loaded")
}

// A cache the pair does not resolve to, or one with no file, is reported rather
// than answered empty: a reader that wants a count must not read zero off a cache
// that is not there.
func TestListObjectsAnswersNothingForACacheOfAnotherCluster(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	f := newDataFixture(t)
	other := createCluster(t, f.d, "other")

	got, ok, err := f.data().ListObjects(ctx, clusterIDOf(other), f.cacheID, "v1", "pods")

	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, got)
}

func TestListObjectsAnswersAnEmptyCacheAsEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d, status := newClusterStatusDeps(t)
	cluster := storedCluster(t, d, status, true, "uid-1")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")

	got, ok, err := serviceOver(t, d).CachedData().ListObjects(ctx, clusterIDOf(cluster), ClusterCacheID(cache.ID), "v1", "pods")

	require.NoError(t, err)
	assert.True(t, ok, "the file is there, with nothing in it")
	assert.Empty(t, got)
}

func TestListObjectsReportsAStoreThatWillNotOpen(t *testing.T) {
	t.Parallel()

	f := newDataFixture(t)
	boom := errors.New("permission denied")
	kubestoreFake(f.d).err = boom

	_, _, err := f.data().ListObjects(context.Background(), f.clusterID, f.cacheID, "v1", "pods")

	assert.ErrorIs(t, err, boom)
}

func TestListObjectsReportsRowsItCannotRead(t *testing.T) {
	t.Parallel()

	f := newDataFixture(t)
	store := kubestoreFake(f.d)
	store.afterOpen = func(cacheID int64) { require.NoError(t, store.mgr.Remove(cacheID)) }

	_, _, err := f.data().ListObjects(context.Background(), f.clusterID, f.cacheID, "v1", "pods")

	assert.ErrorContains(t, err, "objects")
}

func TestListObjectsAnswersACacheWhoseFileIsGoneAsMissing(t *testing.T) {
	t.Parallel()

	f := newDataFixture(t)
	kubestoreFake(f.d).noFile = true

	got, ok, err := f.data().ListObjects(context.Background(), f.clusterID, f.cacheID, "v1", "pods")

	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, got)
}

// Query runs a statement over the cache's views, a paused cache's included.
func TestQueryReadsTheCache(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	f := newDataFixture(t)
	require.NoError(t, f.store.ApplyChange(ctx, kubestore.Kind{APIVersion: "v1", Kind: "Pod", Resource: "pods"},
		watch.Added, dataPod("uid-1", "api-0", "42")))
	f.stopWorkers()

	got, ok, err := f.data().Query(ctx, f.clusterID, f.cacheID, `SELECT name FROM objects`, 10, 1<<20)

	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, ClusterCachedDataQueryResult{Columns: []string{"name"}, Rows: [][]any{{"api-0"}}}, got)
}

// A reading's statements read the cache's views as Query does, their values bound.
func TestReadOwnReadsTheCache(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	f := newDataFixture(t)
	require.NoError(t, f.store.ApplyChange(ctx, kubestore.Kind{APIVersion: "v1", Kind: "Pod", Resource: "pods"},
		watch.Added, dataPod("uid-1", "api-0", "42")))
	f.stopWorkers()

	var got ClusterCachedDataQueryResult
	ok, err := f.data().ReadOwn(ctx, f.clusterID, f.cacheID, func(query OwnQuery) error {
		var err error
		got, err = query(`SELECT name FROM objects WHERE uid = ?`, 10, 1<<20, "uid-1")
		return err
	})

	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, ClusterCachedDataQueryResult{Columns: []string{"name"}, Rows: [][]any{{"api-0"}}}, got)
}

// A reading's statement SQLite refuses is a *QueryError, and a reading of a cache of
// another cluster is not ok.
func TestReadOwnRefusesWhatQueryRefuses(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	f := newDataFixture(t)
	f.stopWorkers()

	ok, err := f.data().ReadOwn(ctx, f.clusterID, f.cacheID, func(query OwnQuery) error {
		_, err := query(`SELECT nope FROM objects`, 10, 1<<20)
		return err
	})
	var qe *QueryError
	require.ErrorAs(t, err, &qe)
	assert.True(t, ok)

	other := createCluster(t, f.d, "other")
	ok, err = f.data().ReadOwn(ctx, clusterIDOf(other), f.cacheID, func(OwnQuery) error { return nil })
	require.NoError(t, err)
	assert.False(t, ok)
}

// ListedKinds is the store's listed kinds keyed as the health names them; a cache of
// another cluster is not ok.
func TestListedKindsReadsTheKindsWithACookie(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	f := newDataFixture(t)
	require.NoError(t, f.store.SyncKinds(ctx, []kubestore.KindRow{
		{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: kubestore.ScopeNamespaced},
		{APIVersion: "apps/v1", Kind: "Deployment", Resource: "deployments", Scope: kubestore.ScopeNamespaced},
	}, true, 7))
	require.NoError(t, f.store.SetCookie(ctx, "v1", "pods", "20"))
	f.stopWorkers()

	listed, ok, err := f.data().ListedKinds(ctx, f.clusterID, f.cacheID)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, map[KindRef]bool{{APIVersion: "v1", Resource: "pods"}: true}, listed)

	other := createCluster(t, f.d, "other")
	_, ok, err = f.data().ListedKinds(ctx, clusterIDOf(other), f.cacheID)
	require.NoError(t, err)
	assert.False(t, ok)
}

// A clear that ends the listed-kinds read is a cache gone, not a fault; any other
// failure of it is reported.
func TestListedKindsOfACacheAClearEndsIsNotOK(t *testing.T) {
	t.Parallel()

	f := newDataFixture(t)
	store := kubestoreFake(f.d)
	store.afterOpen = func(cacheID int64) { require.NoError(t, store.mgr.Remove(cacheID)) }

	_, ok, err := f.data().ListedKinds(context.Background(), f.clusterID, f.cacheID)
	require.NoError(t, err)
	assert.False(t, ok)

	f = newDataFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	kubestoreFake(f.d).afterOpen = func(int64) { cancel() }
	_, ok, err = f.data().ListedKinds(ctx, f.clusterID, f.cacheID)
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, ok)
}

// A clear that ends a reading before it starts is a cache gone, not a fault.
func TestAReadingAClearEndsIsNotOK(t *testing.T) {
	t.Parallel()

	f := newDataFixture(t)
	store := kubestoreFake(f.d)
	store.afterOpen = func(cacheID int64) { require.NoError(t, store.mgr.Remove(cacheID)) }

	ok, err := f.data().ReadOwn(context.Background(), f.clusterID, f.cacheID, func(OwnQuery) error { return nil })

	require.NoError(t, err)
	assert.False(t, ok)
}

func TestQueryOfACacheOfAnotherClusterIsNotOK(t *testing.T) {
	t.Parallel()

	f := newDataFixture(t)
	other := createCluster(t, f.d, "other")

	_, ok, err := f.data().Query(context.Background(), clusterIDOf(other), f.cacheID, `SELECT 1`, 10, 1<<20)

	require.NoError(t, err)
	assert.False(t, ok)
}

func TestQueryOfACacheWithNoFileIsNotOK(t *testing.T) {
	t.Parallel()

	f := newDataFixture(t)
	kubestoreFake(f.d).noFile = true

	_, ok, err := f.data().Query(context.Background(), f.clusterID, f.cacheID, `SELECT 1`, 10, 1<<20)

	require.NoError(t, err)
	assert.False(t, ok)
}

// A clear that ends the query is a cache gone, not a fault.
func TestAQueryAClearEndsIsNotOK(t *testing.T) {
	t.Parallel()

	f := newDataFixture(t)
	store := kubestoreFake(f.d)
	store.afterOpen = func(cacheID int64) { require.NoError(t, store.mgr.Remove(cacheID)) }

	_, ok, err := f.data().Query(context.Background(), f.clusterID, f.cacheID, `SELECT 1`, 10, 1<<20)

	require.NoError(t, err)
	assert.False(t, ok)
}

func TestAStatementErrorIsAClusterQueryError(t *testing.T) {
	t.Parallel()

	f := newDataFixture(t)

	_, _, err := f.data().Query(context.Background(), f.clusterID, f.cacheID, `SELECT nope FROM objects`, 10, 1<<20)

	var qe *QueryError
	require.ErrorAs(t, err, &qe)
	assert.Contains(t, qe.Message, "no such column: nope")
	assert.Equal(t, qe.Message, err.Error())
}

// Any other failure of the query is reported, never read as a cache gone.
func TestAQueryThatFailsIsReported(t *testing.T) {
	t.Parallel()

	f := newDataFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	kubestoreFake(f.d).afterOpen = func(int64) { cancel() }

	_, ok, err := f.data().Query(ctx, f.clusterID, f.cacheID, `SELECT 1`, 10, 1<<20)

	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, ok)
}

func TestQueryReportsAStoreThatWillNotOpen(t *testing.T) {
	t.Parallel()

	f := newDataFixture(t)
	boom := errors.New("permission denied")
	kubestoreFake(f.d).err = boom

	_, _, err := f.data().Query(context.Background(), f.clusterID, f.cacheID, `SELECT 1`, 10, 1<<20)

	assert.ErrorIs(t, err, boom)
}

// ReadActive reads the service's own records: a cache that has synced nothing is
// not trusted, and a cluster that is gone has no cache.
func TestReadActiveReadsTheServicesCluster(t *testing.T) {
	t.Parallel()

	f := newDataFixture(t)
	read := func(CacheReading, ActiveCluster) error { return nil }

	assert.ErrorIs(t, f.data().ReadActive(context.Background(), f.clusterID, "", 10, 1<<20, read), ErrSyncing)
	assert.ErrorIs(t, f.data().ReadActive(context.Background(), "404", "", 10, 1<<20, read), ErrNoCache)
}
