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

package graph_test

// Shared fixtures for the cluster/cache/data resolver tests. The resolvers
// delegate to a cluster.ClusterService, so the tests wire a fakeClusterService
// built from fixtures — that keeps the focus on the GraphQL wire mapping
// (nil→null, conditions/cache shapes) and off whatever backs the service.

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/graph"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oauth"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
)

// ownerOf reads a record's owner ref out of a decoded JSON response.
func ownerOf(record map[string]any) map[string]any {
	owner, _ := record["owner"].(map[string]any)
	return owner
}

// clusterFixture bundles all data for one test cluster record. id is the beehive
// ObjectID; on the wire it is its decimal string ("1", "2", …).
type clusterFixture struct {
	id         cluster.ClusterID
	spec       cluster.ClusterSpec
	connStatus cluster.ClusterStatus
	// Conditions are beehive object rows, not part of either status block. syncConds
	// belong to the fixture's per-kind sync child.
	connConds  []cluster.Condition
	cacheConds []cluster.Condition
	syncConds  []cluster.Condition
}

// fakeClusterService implements cluster.ClusterService over an in-memory map
// built from fixtures: it joins each fixture's connection + cache status into a
// cluster.Cluster (exactly as the real service's buildCluster does), so the
// resolver/wire assertions see the same shapes.
type fakeClusterService struct {
	mu          sync.Mutex
	order       []cluster.ClusterID
	clusters    map[cluster.ClusterID]*cluster.Cluster
	caches      []cluster.ClusterCache      // one active cache per fixture, streamed via Caches().Watch
	cachedKinds []cluster.ClusterCachedKind // per-kind sync records, streamed cache-scoped via CachedKinds().Watch
	cacheStats  map[cluster.ClusterCacheID]cluster.ClusterCacheStats
	// When set, Delete fails with it.
	deleteErr error
	// When set, List fails with it.
	listErr     error
	syncEvents  map[cluster.ClusterCachedKindID][]cluster.Event
	events      map[cluster.ClusterID][]cluster.Event                   // connection-event history, keyed by ClusterID
	cacheEvents map[cluster.ClusterCacheID][]cluster.Event              // sync-event history, keyed by ClusterCacheID
	kinds       map[cluster.ClusterID][]cluster.ClusterCachedDataKind   // discovered kind catalog, keyed by ClusterID
	kindsIn     string                                                  // the namespace the last ListKinds counted in
	dataEvents  map[cluster.ClusterID][]cluster.ClusterCachedDataEvent  // cached Kubernetes Events, keyed by ClusterID
	dataObjects map[cluster.ClusterID][]cluster.ClusterCachedDataObject // cached objects for one kind, keyed by ClusterID
	watchFail   error                                                   // when set, every watch ends with it after its snapshot
}

// The fake mirrors production's shape: one shared state struct, four accessor
// views that carry the family method sets. Each family is asserted separately —
// satisfying cluster.Service only proves the accessors exist.
type (
	fakeClusters    struct{ s *fakeClusterService }
	fakeCaches      struct{ s *fakeClusterService }
	fakeCachedKinds struct{ s *fakeClusterService }
	fakeCachedData  struct{ s *fakeClusterService }
)

func (f *fakeClusterService) Clusters() cluster.Clusters { return fakeClusters{f} }
func (f *fakeClusterService) Caches() cluster.Caches     { return fakeCaches{f} }
func (f *fakeClusterService) CachedKinds() cluster.CachedKinds {
	return fakeCachedKinds{f}
}
func (f *fakeClusterService) CachedData() cluster.CachedData { return fakeCachedData{f} }

// The resolvers never drive the lifecycle — the composition root does — so the fake
// satisfies it and nothing more.
func (f *fakeClusterService) Start(context.Context) (func(context.Context) error, error) {
	return func(context.Context) error { return nil }, nil
}

func (f *fakeClusterService) Close() error { return nil }

// SetCacheFiles is the composition root's wiring; no resolver calls it.
func (f *fakeClusterService) SetCacheFiles(cluster.CacheFileOwner) {}

var (
	_ cluster.Service     = (*fakeClusterService)(nil)
	_ cluster.Clusters    = fakeClusters{}
	_ cluster.Caches      = fakeCaches{}
	_ cluster.CachedKinds = fakeCachedKinds{}
	_ cluster.CachedData  = fakeCachedData{}
)

// Fixture ids are distinct per kind. A cluster's id is its number spelled as text;
// beehive draws every other kind from one AUTOINCREMENT sequence, so a cache never
// shares a number with its cluster — and a fixture that reused one would let a
// resolver read the wrong id and still pass.
func fixtureNum(id cluster.ClusterID) int64 {
	n, err := strconv.ParseInt(string(id), 10, 64)
	if err != nil {
		panic("fixture cluster id is not a number: " + string(id))
	}
	return n
}

func fixtureCacheID(id cluster.ClusterID) cluster.ClusterCacheID {
	return cluster.ClusterCacheID(fixtureNum(id) + 100)
}

func fixtureKindID(id cluster.ClusterID) cluster.ClusterCachedKindID {
	return cluster.ClusterCachedKindID(fixtureNum(id) + 300)
}

// fixtureStamp is every fixture's created and updated time: the wire serializes a
// zero time as null, and both fields are non-null.
var fixtureStamp = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func newFakeClusterService(fixtures []clusterFixture) *fakeClusterService {
	f := &fakeClusterService{
		clusters:   map[cluster.ClusterID]*cluster.Cluster{},
		events:     map[cluster.ClusterID][]cluster.Event{},
		cacheStats: map[cluster.ClusterCacheID]cluster.ClusterCacheStats{},
	}
	for _, fx := range fixtures {
		id := fx.id
		f.order = append(f.order, id)
		f.clusters[id] = &cluster.Cluster{
			ID:         id,
			CreatedAt:  fixtureStamp,
			UpdatedAt:  fixtureStamp,
			Conditions: fx.connConds,
			Spec:       fx.spec,
			Status:     fx.connStatus,
		}
		// Caches stream standalone via WatchCaches and are joined client-side.
		// Give each fixture one cache whose ServerUID matches the cluster's
		// identity (the client's active-cache rule).
		f.caches = append(f.caches, cluster.ClusterCache{
			RecordMeta: cluster.RecordMeta{ID: fixtureCacheID(id), Conditions: fx.cacheConds},
			ClusterID:  id,
			Spec:       cluster.ClusterCacheSpec{ServerUID: "uid-" + string(id), ClusterID: id},
		})
		// Each cache gets one per-kind sync record, so the cache-scoped watch has
		// something to scope. Deliberately one per cache so a leak across caches is
		// visible as an extra frame.
		f.cachedKinds = append(f.cachedKinds, cluster.ClusterCachedKind{
			RecordMeta: cluster.RecordMeta{ID: fixtureKindID(id), Conditions: fx.syncConds},
			Owner:      cluster.ObjectRef{ID: fixtureCacheID(id), Kind: "ClusterCache"},
			Spec: cluster.ClusterCachedKindSpec{
				APIVersion: "apps/v1", Kind: "Deployment",
				Resource: "deployments", Namespaced: true,
			},
		})
		f.cacheStats[fixtureCacheID(id)] = cluster.ClusterCacheStats{
			Exists: true, Bytes: 4096, ObjectCount: 1386, KindCount: 62,
		}
	}
	return f
}

func (f *fakeClusterService) snapshot() []*cluster.Cluster {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*cluster.Cluster, 0, len(f.order))
	for _, id := range f.order {
		if c, ok := f.clusters[id]; ok {
			cp := *c
			out = append(out, &cp)
		}
	}
	return out
}

func (f *fakeClusterService) cacheSnapshot() []cluster.ClusterCache {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]cluster.ClusterCache(nil), f.caches...)
}

func (f fakeClusters) List(context.Context) ([]*cluster.Cluster, error) {
	if f.s.listErr != nil {
		return nil, f.s.listErr
	}
	return f.s.snapshot(), nil
}

func (f fakeClusters) Get(_ context.Context, id cluster.ClusterID) (*cluster.Cluster, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	c, ok := f.s.clusters[id]
	if !ok {
		return nil, nil
	}
	cp := *c
	return &cp, nil
}

// ReadActive hands read the record alone: no resolver reads a cache through it.
func (f fakeClusters) ReadActive(ctx context.Context, id cluster.ClusterID, read func(context.Context, cluster.ActiveCluster) error) error {
	c, err := f.Get(ctx, id)
	if err != nil {
		return err
	}
	if c == nil {
		return cluster.ErrNotFound
	}
	return read(ctx, cluster.ActiveCluster{Cluster: c})
}

// collectionWatchStream models every collection watch the service exposes: replay the current set as
// Added frames, close the snapshot with bookmark, then hold the stream open until ctx
// ends (a real watch never completes on its own, and several tests assert exactly
// that). Each watch differs only in how it wraps an item into its frame.
//
// The bookmark rides the stream even when the snapshot is empty — that is the whole
// point of it, and how a consumer tells "nothing here" from "still listing".
//
// A fixture with watchFail set ends every watch with it instead, standing in for a
// source that died mid-stream.
func collectionWatchStream[T, C any](ctx context.Context, f *fakeClusterService, items []T, wrap func(*T) C, bookmark C) *cluster.Stream[C] {
	snap := make([]C, 0, len(items)+1)
	for i := range items {
		item := items[i]
		snap = append(snap, wrap(&item))
	}
	snap = append(snap, bookmark)
	return streamOf(ctx, f, snap)
}

// stateWatchStream is collectionWatchStream's counterpart for a state watch: current on
// subscribe, so no bookmark closes anything. WatchHealthKeyedState is the only one.
func stateWatchStream[T, C any](ctx context.Context, f *fakeClusterService, items []T, wrap func(*T) C) *cluster.Stream[C] {
	snap := make([]C, 0, len(items))
	for i := range items {
		item := items[i]
		snap = append(snap, wrap(&item))
	}
	return streamOf(ctx, f, snap)
}

func streamOf[C any](ctx context.Context, f *fakeClusterService, frames []C) *cluster.Stream[C] {
	return cluster.NewStream(ctx, func(ctx context.Context, out chan<- C) error {
		for _, c := range frames {
			select {
			case out <- c:
			case <-ctx.Done():
				return nil
			}
		}
		if err := f.failure(); err != nil {
			return err
		}
		<-ctx.Done()
		return nil
	})
}

// failure is the injected terminal reason, nil unless a test set one.
func (f *fakeClusterService) failure() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.watchFail
}

// copySlice returns a copy of src taken under the fake's lock, so a watch's replay can't
// race a concurrent mutation of the fixture.
func copySlice[T any](f *fakeClusterService, src *[]T) []T {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]T(nil), *src...)
}

func (f fakeClusters) WatchCollection(ctx context.Context) (*cluster.Stream[cluster.ClusterCollectionWatchFrame], error) {
	return collectionWatchStream(ctx, f.s, f.s.snapshot(), func(c **cluster.Cluster) cluster.ClusterCollectionWatchFrame {
		return cluster.ClusterCollectionWatchFrame{Type: cluster.CollectionWatchFrameAdded, Cluster: *c}
	}, cluster.ClusterCollectionWatchFrame{Type: cluster.CollectionWatchFrameBookmark}), nil
}

func (f fakeClusters) WatchCollectionByID(ctx context.Context, id cluster.ClusterID) (*cluster.Stream[cluster.ClusterCollectionWatchFrame], error) {
	var rows []*cluster.Cluster
	for _, c := range f.s.snapshot() {
		if c.ID == id {
			rows = append(rows, c)
		}
	}
	return collectionWatchStream(ctx, f.s, rows, func(c **cluster.Cluster) cluster.ClusterCollectionWatchFrame {
		return cluster.ClusterCollectionWatchFrame{Type: cluster.CollectionWatchFrameAdded, Cluster: *c}
	}, cluster.ClusterCollectionWatchFrame{Type: cluster.CollectionWatchFrameBookmark}), nil
}

func (f fakeCaches) WatchCollection(ctx context.Context) (*cluster.Stream[cluster.ClusterCacheCollectionWatchFrame], error) {
	return collectionWatchStream(ctx, f.s, f.s.cacheSnapshot(), func(c *cluster.ClusterCache) cluster.ClusterCacheCollectionWatchFrame {
		return cluster.ClusterCacheCollectionWatchFrame{Type: cluster.CollectionWatchFrameAdded, Cache: c}
	}, cluster.ClusterCacheCollectionWatchFrame{Type: cluster.CollectionWatchFrameBookmark}), nil
}

func (f fakeCaches) WatchCollectionByCluster(ctx context.Context, clusterID cluster.ClusterID) (*cluster.Stream[cluster.ClusterCacheCollectionWatchFrame], error) {
	var rows []cluster.ClusterCache
	for _, c := range f.s.cacheSnapshot() {
		if c.ClusterID == clusterID {
			rows = append(rows, c)
		}
	}
	return collectionWatchStream(ctx, f.s, rows, func(c *cluster.ClusterCache) cluster.ClusterCacheCollectionWatchFrame {
		return cluster.ClusterCacheCollectionWatchFrame{Type: cluster.CollectionWatchFrameAdded, Cache: c}
	}, cluster.ClusterCacheCollectionWatchFrame{Type: cluster.CollectionWatchFrameBookmark}), nil
}

func (f fakeCaches) WatchCollectionByID(ctx context.Context, id cluster.ClusterCacheID) (*cluster.Stream[cluster.ClusterCacheCollectionWatchFrame], error) {
	var rows []cluster.ClusterCache
	for _, c := range f.s.cacheSnapshot() {
		if c.ID == id {
			rows = append(rows, c)
		}
	}
	return collectionWatchStream(ctx, f.s, rows, func(c *cluster.ClusterCache) cluster.ClusterCacheCollectionWatchFrame {
		return cluster.ClusterCacheCollectionWatchFrame{Type: cluster.CollectionWatchFrameAdded, Cache: c}
	}, cluster.ClusterCacheCollectionWatchFrame{Type: cluster.CollectionWatchFrameBookmark}), nil
}

// WatchHealthKeyedState folds the fixture's per-kind records per cache, the same way the
// real service does — enough to prove the wire shape and the join key.
func (f fakeCaches) WatchHealthKeyedState(ctx context.Context) (*cluster.Stream[cluster.ClusterCacheHealth], error) {
	f.s.mu.Lock()
	byCache := map[cluster.ClusterCacheID]*cluster.ClusterCacheHealth{}
	for i := range f.s.cachedKinds {
		cacheID := f.s.cachedKinds[i].Owner.ID
		h := byCache[cacheID]
		if h == nil {
			h = &cluster.ClusterCacheHealth{CacheID: cacheID, Status: cluster.ConditionTrue, Reason: "Watching"}
			byCache[cacheID] = h
		}
		h.TotalKinds++
		for _, c := range f.s.cachedKinds[i].Conditions {
			if c.Type == string(cluster.ConditionSynced) && c.Reason != "Watching" {
				h.Status, h.Reason = c.Status, c.Reason
				h.UnhealthyKindRefs = append(h.UnhealthyKindRefs, cluster.UnhealthyKindRef{
					KindRef: cluster.KindRef{
						APIVersion: f.s.cachedKinds[i].Spec.APIVersion,
						Resource:   f.s.cachedKinds[i].Spec.Resource,
					},
					Reason: c.Reason,
				})
				h.UnhealthyKinds++
			}
		}
	}
	f.s.mu.Unlock()
	verdicts := make([]cluster.ClusterCacheHealth, 0, len(byCache))
	for _, h := range byCache {
		verdicts = append(verdicts, *h)
	}
	return stateWatchStream(ctx, f.s, verdicts, func(h *cluster.ClusterCacheHealth) cluster.ClusterCacheHealth {
		return *h
	}), nil
}

// WatchCollectionByCache serves only the records the requested cache owns, standing in for the
// real service's owner-edge filter.
func (f fakeCachedKinds) WatchCollectionByCache(ctx context.Context, cacheID cluster.ClusterCacheID) (*cluster.Stream[cluster.ClusterCachedKindCollectionWatchFrame], error) {
	f.s.mu.Lock()
	var scoped []cluster.ClusterCachedKind
	for i := range f.s.cachedKinds {
		if f.s.cachedKinds[i].Owner.ID == cacheID {
			scoped = append(scoped, f.s.cachedKinds[i])
		}
	}
	f.s.mu.Unlock()
	return collectionWatchStream(ctx, f.s, scoped, func(gs *cluster.ClusterCachedKind) cluster.ClusterCachedKindCollectionWatchFrame {
		return cluster.ClusterCachedKindCollectionWatchFrame{Type: cluster.CollectionWatchFrameAdded, Kind: gs}
	}, cluster.ClusterCachedKindCollectionWatchFrame{Type: cluster.CollectionWatchFrameBookmark}), nil
}

func (f fakeCachedKinds) WatchCollection(ctx context.Context) (*cluster.Stream[cluster.ClusterCachedKindCollectionWatchFrame], error) {
	return collectionWatchStream(ctx, f.s, copySlice(f.s, &f.s.cachedKinds), func(gs *cluster.ClusterCachedKind) cluster.ClusterCachedKindCollectionWatchFrame {
		return cluster.ClusterCachedKindCollectionWatchFrame{Type: cluster.CollectionWatchFrameAdded, Kind: gs}
	}, cluster.ClusterCachedKindCollectionWatchFrame{Type: cluster.CollectionWatchFrameBookmark}), nil
}

func (f fakeCachedKinds) WatchCollectionByID(ctx context.Context, id cluster.ClusterCachedKindID) (*cluster.Stream[cluster.ClusterCachedKindCollectionWatchFrame], error) {
	f.s.mu.Lock()
	var rows []cluster.ClusterCachedKind
	for i := range f.s.cachedKinds {
		if f.s.cachedKinds[i].ID == id {
			rows = append(rows, f.s.cachedKinds[i])
		}
	}
	f.s.mu.Unlock()
	return collectionWatchStream(ctx, f.s, rows, func(gs *cluster.ClusterCachedKind) cluster.ClusterCachedKindCollectionWatchFrame {
		return cluster.ClusterCachedKindCollectionWatchFrame{Type: cluster.CollectionWatchFrameAdded, Kind: gs}
	}, cluster.ClusterCachedKindCollectionWatchFrame{Type: cluster.CollectionWatchFrameBookmark}), nil
}

// WatchSyncState expands the fixture's per-kind records for one cache, the counterpart of
// the fold WatchHealthKeyedState does — enough to prove the wire shape.
func (f fakeCaches) WatchSyncState(ctx context.Context, _ cluster.ClusterID, cacheID cluster.ClusterCacheID) (*cluster.Stream[cluster.ClusterCacheSyncState], error) {
	f.s.mu.Lock()
	status := cluster.ClusterCacheSyncState{
		CacheID:   cacheID,
		Exists:    true,
		Discovery: cluster.ClusterCacheDiscoveryStatus{Reason: "Discovered"},
	}
	for i := range f.s.cachedKinds {
		if f.s.cachedKinds[i].Owner.ID != cacheID {
			continue
		}
		spec := f.s.cachedKinds[i].Spec
		row := cluster.ClusterCacheKindSyncStatus{
			APIVersion: spec.APIVersion, Kind: spec.Kind, Resource: spec.Resource, Reason: "Watching",
		}
		for _, c := range f.s.cachedKinds[i].Conditions {
			if c.Type == string(cluster.ConditionSynced) {
				row.Reason, row.Message = c.Reason, c.Message
			}
		}
		status.Kinds = append(status.Kinds, row)
	}
	f.s.mu.Unlock()

	return cluster.NewStream(ctx, func(ctx context.Context, out chan<- cluster.ClusterCacheSyncState) error {
		select {
		case out <- status:
		case <-ctx.Done():
			return nil
		}
		<-ctx.Done()
		return nil
	}), nil
}

// WatchStatsState emits the fixture's single measurement and then holds the
// stream open, as a state watch with nothing new to report does.
func (f fakeCaches) WatchStatsState(ctx context.Context, _ cluster.ClusterID, cacheID cluster.ClusterCacheID) (*cluster.Stream[cluster.ClusterCacheStats], error) {
	f.s.mu.Lock()
	st := f.s.cacheStats[cacheID]
	f.s.mu.Unlock()
	return cluster.NewStream(ctx, func(ctx context.Context, out chan<- cluster.ClusterCacheStats) error {
		select {
		case out <- st:
		case <-ctx.Done():
			return nil
		}
		<-ctx.Done()
		return nil
	}), nil
}

// The one-shot reads serve no resolver; the card reads them off the real service.
func (f fakeCaches) Health(context.Context, cluster.ClusterID, cluster.ClusterCacheID) (cluster.ClusterCacheHealth, bool, error) {
	return cluster.ClusterCacheHealth{}, false, nil
}

func (f fakeCaches) SyncState(context.Context, cluster.ClusterID, cluster.ClusterCacheID) (cluster.ClusterCacheSyncState, bool, error) {
	return cluster.ClusterCacheSyncState{}, false, nil
}

func (f fakeCachedData) ListObjects(context.Context, cluster.ClusterID, cluster.ClusterCacheID, string, string) ([]cluster.ClusterCachedDataObject, bool, error) {
	return nil, false, nil
}

func (f fakeCachedData) Query(context.Context, cluster.ClusterID, cluster.ClusterCacheID, string, int, int, ...any) (cluster.ClusterCachedDataQueryResult, bool, error) {
	return cluster.ClusterCachedDataQueryResult{}, false, nil
}

func (f fakeCachedData) ListedKinds(context.Context, cluster.ClusterID, cluster.ClusterCacheID) (map[cluster.KindRef]bool, bool, error) {
	return nil, false, nil
}

func (f fakeCachedData) ReadOwn(context.Context, cluster.ClusterID, cluster.ClusterCacheID, func(cluster.OwnQuery) error) (bool, error) {
	return false, nil
}

func (f fakeCachedData) ReadActive(ctx context.Context, id cluster.ClusterID, serverUID string, maxRows, maxBytes int, read func(cluster.CacheReading, cluster.ActiveCluster) error) error {
	return cluster.ReadActiveCache(ctx, f.s, id, serverUID, maxRows, maxBytes, read)
}

func (f fakeCachedData) ListKinds(_ context.Context, clusterID cluster.ClusterID, _ cluster.ClusterCacheID, namespace string) ([]cluster.ClusterCachedDataKind, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	f.s.kindsIn = namespace
	return f.s.kinds[clusterID], nil
}

// WatchKindCollection echoes the namespace on each frame, as the service does.
func (f fakeCachedData) WatchKindCollection(ctx context.Context, clusterID cluster.ClusterID, _ cluster.ClusterCacheID, namespace string) (*cluster.Stream[cluster.ClusterCachedDataKindCollectionWatchFrame], error) {
	f.s.mu.Lock()
	snap := append([]cluster.ClusterCachedDataKind(nil), f.s.kinds[clusterID]...)
	f.s.mu.Unlock()
	return cluster.NewStream(ctx, func(ctx context.Context, out chan<- cluster.ClusterCachedDataKindCollectionWatchFrame) error {
		for _, k := range snap {
			select {
			case out <- cluster.ClusterCachedDataKindCollectionWatchFrame{Type: cluster.CollectionWatchFrameAdded, Kind: &k, Namespace: namespace}:
			case <-ctx.Done():
				return nil
			}
		}
		<-ctx.Done()
		return nil
	}), nil
}

func (f fakeCachedData) WatchEventCollection(ctx context.Context, clusterID cluster.ClusterID, _ cluster.ClusterCacheID) (*cluster.Stream[cluster.ClusterCachedDataEventCollectionWatchFrame], error) {
	f.s.mu.Lock()
	snap := append([]cluster.ClusterCachedDataEvent(nil), f.s.dataEvents[clusterID]...)
	f.s.mu.Unlock()
	return cluster.NewStream(ctx, func(ctx context.Context, out chan<- cluster.ClusterCachedDataEventCollectionWatchFrame) error {
		for _, e := range snap {
			select {
			case out <- cluster.ClusterCachedDataEventCollectionWatchFrame{Type: cluster.CollectionWatchFrameAdded, Event: &e}:
			case <-ctx.Done():
				return nil
			}
		}
		<-ctx.Done()
		return nil
	}), nil
}

func (f fakeCachedData) WatchObjectCollectionByNamespaces(ctx context.Context, clusterID cluster.ClusterID, cacheID cluster.ClusterCacheID, apiVersion, resource string, _ []string) (*cluster.Stream[cluster.ClusterCachedDataObjectCollectionWatchFrame], error) {
	return f.WatchObjectCollection(ctx, clusterID, cacheID, apiVersion, resource)
}

func (f fakeCachedData) WatchObjectCollection(ctx context.Context, clusterID cluster.ClusterID, _ cluster.ClusterCacheID, _, _ string) (*cluster.Stream[cluster.ClusterCachedDataObjectCollectionWatchFrame], error) {
	f.s.mu.Lock()
	snap := append([]cluster.ClusterCachedDataObject(nil), f.s.dataObjects[clusterID]...)
	f.s.mu.Unlock()
	return cluster.NewStream(ctx, func(ctx context.Context, out chan<- cluster.ClusterCachedDataObjectCollectionWatchFrame) error {
		for _, o := range snap {
			select {
			case out <- cluster.ClusterCachedDataObjectCollectionWatchFrame{Type: cluster.CollectionWatchFrameAdded, Object: &o}:
			case <-ctx.Done():
				return nil
			}
		}
		<-ctx.Done()
		return nil
	}), nil
}

// ListEvents and SubscribeObjectEventFeed serve the cache and kind timelines from one reader, as
// the real service does; a cluster's is the Clusters() pair. The fixtures' ids are
// disjoint across kinds (see fixtureCacheID and friends), so the id alone picks the
// log out.
func (f *fakeClusterService) ListEvents(_ context.Context, id cluster.ObjectID, _ *string, _ *int) ([]cluster.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if evs, ok := f.cacheEvents[id]; ok {
		return evs, nil
	}
	return f.syncEvents[id], nil
}

func (f *fakeClusterService) SubscribeObjectEventFeed(ctx context.Context, _ cluster.ObjectID, _ *string) (*cluster.Stream[cluster.EventFeedFrame], error) {
	return f.eventStream(ctx)
}

func (f *fakeClusterService) eventStream(ctx context.Context) (*cluster.Stream[cluster.EventFeedFrame], error) {
	return collectionWatchStream(ctx, f, nil, func(e *cluster.Event) cluster.EventFeedFrame {
		return cluster.EventFeedFrame{Type: cluster.EventFeedFrameRun, Event: e}
	}, cluster.EventFeedFrame{Type: cluster.EventFeedFrameBookmark}), nil
}

func (f fakeClusters) ListEvents(_ context.Context, id cluster.ClusterID, _ *string, _ *int) ([]cluster.Event, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	return f.s.events[id], nil
}

func (f fakeClusters) SubscribeEventFeed(ctx context.Context, _ cluster.ClusterID, _ *string) (*cluster.Stream[cluster.EventFeedFrame], error) {
	return f.s.eventStream(ctx)
}

func (f fakeCaches) Get(_ context.Context, id cluster.ClusterCacheID) (*cluster.ClusterCache, error) {
	for _, c := range f.s.cacheSnapshot() {
		if c.ID == id {
			return &c, nil
		}
	}
	return nil, nil
}

func (f fakeCaches) ListByCluster(_ context.Context, clusterID cluster.ClusterID) ([]*cluster.ClusterCache, error) {
	var out []*cluster.ClusterCache
	for _, c := range f.s.cacheSnapshot() {
		if c.ClusterID == clusterID {
			out = append(out, &c)
		}
	}
	return out, nil
}

func (f fakeCachedKinds) ListByCache(_ context.Context, cacheID cluster.ClusterCacheID) ([]*cluster.ClusterCachedKind, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	var out []*cluster.ClusterCachedKind
	for i := range f.s.cachedKinds {
		if f.s.cachedKinds[i].Owner.ID == cacheID {
			gs := f.s.cachedKinds[i]
			out = append(out, &gs)
		}
	}
	return out, nil
}

// WatchScheduleState is a state watch: one current value on subscribe, then open until ctx
// ends, as a cluster with nothing newly scheduled behaves.
func (f fakeClusters) WatchScheduleState(ctx context.Context, _ cluster.ClusterID) (<-chan cluster.Schedule, error) {
	ch := make(chan cluster.Schedule)
	go func() {
		defer close(ch)
		select {
		case ch <- cluster.Schedule{Probing: true}:
		case <-ctx.Done():
			return
		}
		<-ctx.Done()
	}()
	return ch, nil
}

// fakeDataRateFrame is the window every fake sample feed opens with.
var fakeDataRateFrame = cluster.ClusterDataRateSampleFeedFrame{
	IntervalSeconds: 2,
	WindowSeconds:   120,
	Samples: []cluster.ClusterDataRateSample{
		{At: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC), ReceivedBytesPerSecond: 2048, SentBytesPerSecond: 512},
	},
}

// SubscribeDataRateSampleFeed sends the window on subscribe, then open until ctx ends.
func (f fakeClusters) SubscribeDataRateSampleFeed(ctx context.Context, _ cluster.ClusterID) (<-chan cluster.ClusterDataRateSampleFeedFrame, error) {
	ch := make(chan cluster.ClusterDataRateSampleFeedFrame)
	go func() {
		defer close(ch)
		select {
		case ch <- fakeDataRateFrame:
		case <-ctx.Done():
			return
		}
		<-ctx.Done()
	}()
	return ch, nil
}

func (f fakeClusters) SetEnabled(_ context.Context, id cluster.ClusterID, enabled bool) (*cluster.Cluster, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	c, ok := f.s.clusters[id]
	if !ok {
		return nil, cluster.ErrNotFound
	}
	c.Spec.Enabled = enabled
	cp := *c
	return &cp, nil
}

func (f fakeClusters) SetSyncEnabled(_ context.Context, id cluster.ClusterID, enabled bool) (*cluster.Cluster, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	c, ok := f.s.clusters[id]
	if !ok {
		return nil, cluster.ErrNotFound
	}
	c.Spec.SyncEnabled = enabled
	cp := *c
	return &cp, nil
}

func (f fakeClusters) SetDefault(_ context.Context, id cluster.ClusterID, isDefault bool) (*cluster.Cluster, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	c, ok := f.s.clusters[id]
	if !ok {
		return nil, cluster.ErrNotFound
	}
	if isDefault {
		for _, other := range f.s.clusters {
			other.Spec.IsDefault = false
		}
	}
	c.Spec.IsDefault = isDefault
	cp := *c
	return &cp, nil
}

func (f *fakeClusterService) RetryConnection(_ context.Context, id cluster.ClusterID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.clusters[id]; !ok {
		return cluster.ErrNotFound
	}
	return nil
}

func (f fakeCachedKinds) SetSyncEnabled(_ context.Context, id cluster.ClusterCachedKindID, syncEnabled bool) (*cluster.ClusterCachedKind, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	for i := range f.s.cachedKinds {
		if f.s.cachedKinds[i].ID == id {
			f.s.cachedKinds[i].Spec.Paused = !syncEnabled
			cr := f.s.cachedKinds[i]
			return &cr, nil
		}
	}
	return nil, cluster.ErrNotFound
}

func (f fakeCachedKinds) Clear(_ context.Context, id cluster.ClusterCachedKindID) (*cluster.ClusterCachedKind, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	for i := range f.s.cachedKinds {
		if f.s.cachedKinds[i].ID == id {
			cr := f.s.cachedKinds[i]
			return &cr, nil
		}
	}
	return nil, cluster.ErrNotFound
}

func (f fakeCaches) Clear(_ context.Context, id cluster.ClusterCacheID) (*cluster.ClusterCache, error) {
	for _, c := range f.s.cacheSnapshot() {
		if c.ID == id {
			return &c, nil
		}
	}
	return nil, cluster.ErrNotFound
}

func (f fakeClusters) Delete(_ context.Context, id cluster.ClusterID) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	if f.s.deleteErr != nil {
		return f.s.deleteErr
	}
	if _, ok := f.s.clusters[id]; !ok {
		return cluster.ErrNotFound
	}
	delete(f.s.clusters, id)
	return nil
}

func (f *fakeClusterService) AcquireConnection(context.Context, cluster.ClusterID) (cluster.Lease, error) {
	return nil, nil
}

// clusterFixtures returns two records: one fully-probed/present (1) and
// one never-probed/orphaned (2), so nullable fields exercise both arms.
func clusterFixtures() []clusterFixture {
	prodName := "Production"
	uid1 := "uid-1"
	ver := "v1.29.3"
	admin := "system:admin"
	return []clusterFixture{
		{
			id: "1",
			spec: cluster.ClusterSpec{
				Name:        &prodName,
				SyncEnabled: true,
				Enabled:     true,
				Source:      cluster.ClusterSpecSource{Kubeconfig: &cluster.ClusterSpecSourceKubeconfig{Context: "prod"}},
			},
			connStatus: cluster.ClusterStatus{
				Source: cluster.ClusterStatusSource{Kubeconfig: &cluster.ClusterStatusSourceKubeconfig{
					Cluster:   cluster.ClusterStatusSourceKubeconfigCluster{Name: "prod-cluster"},
					User:      cluster.ClusterStatusSourceKubeconfigUser{Name: "prod-user"},
					IsPresent: true, IsDefault: true,
				}},
				Server:    cluster.ClusterServer{UID: &uid1, Version: &ver},
				Principal: cluster.ClusterPrincipal{Username: &admin},
			},
		},
		{
			id: "2",
			spec: cluster.ClusterSpec{
				Source: cluster.ClusterSpecSource{Kubeconfig: &cluster.ClusterSpecSourceKubeconfig{Context: "staging"}},
			},
			connStatus: cluster.ClusterStatus{
				Source: cluster.ClusterStatusSource{Kubeconfig: &cluster.ClusterStatusSourceKubeconfig{
					Cluster: cluster.ClusterStatusSourceKubeconfigCluster{Name: "staging-cluster"},
					User:    cluster.ClusterStatusSourceKubeconfigUser{Name: "staging-user"},
				}},
			},
		},
	}
}

// newTestServer returns an httptest.Server backed by a real resolver wired to a
// fakeClusterService built from the fixtures. Cleanup is registered via
// t.Cleanup so callers need not defer Close.
func newTestServer(t *testing.T, fixtures []clusterFixture) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		ClusterSvc: newFakeClusterService(fixtures),
		Auth:       newFakeAuth(oauth.Identity{}),
	}))
	t.Cleanup(srv.Close)
	return srv
}

// collectionSnapshot opens a collection watch and returns its snapshot: the item of
// each frame before the Bookmark, keyed by its id. field is the subscription's field and
// item the frame's item field.
func collectionSnapshot(t *testing.T, srvURL, subscription, field, item string) map[string]map[string]any {
	t.Helper()
	resp := openSSESubscription(t, srvURL, "", subscription)
	defer resp.Body.Close()
	events := sseEvents(t, resp)
	got := map[string]map[string]any{}
	for {
		ev := nextSSE(t, events)
		if ev.event != "next" {
			continue
		}
		var frame struct {
			Data   map[string]map[string]any  `json:"data"`
			Errors []struct{ Message string } `json:"errors"`
		}
		require.NoError(t, json.Unmarshal([]byte(ev.data), &frame), ev.data)
		require.Empty(t, frame.Errors, ev.data)
		if frame.Data[field]["type"] == "Bookmark" {
			return got
		}
		record := frame.Data[field][item].(map[string]any)
		got[record["id"].(string)] = record
	}
}

// clusterSnapshot is clusterCollectionWatch's snapshot, each cluster selecting selection.
func clusterSnapshot(t *testing.T, srvURL, selection string) map[string]map[string]any {
	t.Helper()
	return collectionSnapshot(t, srvURL,
		`subscription { clusterCollectionWatch { type cluster { id `+selection+` } } }`,
		"clusterCollectionWatch", "cluster")
}

// cacheSnapshot is clusterCacheCollectionWatch's snapshot, each cache selecting selection.
func cacheSnapshot(t *testing.T, srvURL, selection string) map[string]map[string]any {
	t.Helper()
	return collectionSnapshot(t, srvURL,
		`subscription { clusterCacheCollectionWatch { type cache { id `+selection+` } } }`,
		"clusterCacheCollectionWatch", "cache")
}

// firstCacheFrame opens clusterCacheCollectionWatch and returns the `cache` payload of the
// first Added frame for cluster "1", decoded to a generic map, for the wire-shape tests.
func firstCacheFrame(t *testing.T, srvURL string) map[string]any {
	t.Helper()
	resp := openSSESubscription(t, srvURL, "",
		`subscription { clusterCacheCollectionWatch { type cache { id clusterID spec { serverUid } `+
			`conditions { type status reason } } } }`)
	t.Cleanup(func() { resp.Body.Close() })
	events := sseEvents(t, resp)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("cache stream closed before a frame arrived")
			}
			if ev.event != "next" {
				continue
			}
			var frame struct {
				Data struct {
					ClusterCacheCollectionWatch struct {
						Type  string         `json:"type"`
						Cache map[string]any `json:"cache"`
					} `json:"clusterCacheCollectionWatch"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(ev.data), &frame); err != nil {
				t.Fatalf("decode cache frame %s: %v", ev.data, err)
			}
			if frame.Data.ClusterCacheCollectionWatch.Cache["clusterID"] == "1" {
				return frame.Data.ClusterCacheCollectionWatch.Cache
			}
		case <-deadline:
			t.Fatal("timed out waiting for cluster 1 cache frame")
		}
	}
}

// --- Failing doubles ---
//
// Every resolver that returns (value, error) has an arm that only a failing
// service reaches. These doubles embed the interfaces rather than implementing
// them: the resolvers call a handful of methods, and anything else is a nil-call
// panic naming exactly what a test reached without meaning to.

// errClusterService fails every call the resolvers make with err.
type errClusterService struct {
	cluster.Service
	err error
}

type (
	errClusters struct {
		cluster.Clusters
		err error
	}
	errCaches struct {
		cluster.Caches
		err error
	}
	errCachedKinds struct {
		cluster.CachedKinds
		err error
	}
	errCachedData struct {
		cluster.CachedData
		err error
	}
)

func (e errClusterService) Clusters() cluster.Clusters       { return errClusters{err: e.err} }
func (e errClusterService) Caches() cluster.Caches           { return errCaches{err: e.err} }
func (e errClusterService) CachedKinds() cluster.CachedKinds { return errCachedKinds{err: e.err} }
func (e errClusterService) CachedData() cluster.CachedData   { return errCachedData{err: e.err} }

func (e errClusterService) ListEvents(context.Context, cluster.ObjectID, *string, *int) ([]cluster.Event, error) {
	return nil, e.err
}

func (e errClusterService) SubscribeObjectEventFeed(context.Context, cluster.ObjectID, *string) (*cluster.Stream[cluster.EventFeedFrame], error) {
	return nil, e.err
}

func (e errClusterService) RetryConnection(context.Context, cluster.ClusterID) error { return e.err }

func (e errClusters) List(context.Context) ([]*cluster.Cluster, error) { return nil, e.err }

func (e errClusters) Get(context.Context, cluster.ClusterID) (*cluster.Cluster, error) {
	return nil, e.err
}

func (e errClusters) ReadActive(context.Context, cluster.ClusterID, func(context.Context, cluster.ActiveCluster) error) error {
	return e.err
}

func (e errClusters) WatchCollection(context.Context) (*cluster.Stream[cluster.ClusterCollectionWatchFrame], error) {
	return nil, e.err
}

func (e errClusters) WatchScheduleState(context.Context, cluster.ClusterID) (<-chan cluster.Schedule, error) {
	return nil, e.err
}

func (e errClusters) SubscribeDataRateSampleFeed(context.Context, cluster.ClusterID) (<-chan cluster.ClusterDataRateSampleFeedFrame, error) {
	return nil, e.err
}

func (e errClusters) SetEnabled(context.Context, cluster.ClusterID, bool) (*cluster.Cluster, error) {
	return nil, e.err
}

func (e errClusters) SetSyncEnabled(context.Context, cluster.ClusterID, bool) (*cluster.Cluster, error) {
	return nil, e.err
}

func (e errClusters) SetDefault(context.Context, cluster.ClusterID, bool) (*cluster.Cluster, error) {
	return nil, e.err
}

func (e errClusters) Delete(context.Context, cluster.ClusterID) error { return e.err }

func (e errClusters) ListEvents(context.Context, cluster.ClusterID, *string, *int) ([]cluster.Event, error) {
	return nil, e.err
}
func (e errClusters) SubscribeEventFeed(context.Context, cluster.ClusterID, *string) (*cluster.Stream[cluster.EventFeedFrame], error) {
	return nil, e.err
}

func (e errCaches) Get(context.Context, cluster.ClusterCacheID) (*cluster.ClusterCache, error) {
	return nil, e.err
}

func (e errCaches) ListByCluster(context.Context, cluster.ClusterID) ([]*cluster.ClusterCache, error) {
	return nil, e.err
}

func (e errCaches) Clear(context.Context, cluster.ClusterCacheID) (*cluster.ClusterCache, error) {
	return nil, e.err
}

func (e errCaches) WatchCollection(context.Context) (*cluster.Stream[cluster.ClusterCacheCollectionWatchFrame], error) {
	return nil, e.err
}

func (e errCaches) WatchHealthKeyedState(context.Context) (*cluster.Stream[cluster.ClusterCacheHealth], error) {
	return nil, e.err
}

func (e errCaches) WatchStatsState(context.Context, cluster.ClusterID, cluster.ClusterCacheID) (*cluster.Stream[cluster.ClusterCacheStats], error) {
	return nil, e.err
}

func (e errCaches) WatchSyncState(context.Context, cluster.ClusterID, cluster.ClusterCacheID) (*cluster.Stream[cluster.ClusterCacheSyncState], error) {
	return nil, e.err
}

func (e errCaches) Health(context.Context, cluster.ClusterID, cluster.ClusterCacheID) (cluster.ClusterCacheHealth, bool, error) {
	return cluster.ClusterCacheHealth{}, false, e.err
}

func (e errCaches) SyncState(context.Context, cluster.ClusterID, cluster.ClusterCacheID) (cluster.ClusterCacheSyncState, bool, error) {
	return cluster.ClusterCacheSyncState{}, false, e.err
}

func (e errCachedKinds) ListByCache(context.Context, cluster.ClusterCacheID) ([]*cluster.ClusterCachedKind, error) {
	return nil, e.err
}

func (e errCachedKinds) SetSyncEnabled(context.Context, cluster.ClusterCachedKindID, bool) (*cluster.ClusterCachedKind, error) {
	return nil, e.err
}

func (e errCachedKinds) WatchCollectionByCache(context.Context, cluster.ClusterCacheID) (*cluster.Stream[cluster.ClusterCachedKindCollectionWatchFrame], error) {
	return nil, e.err
}

func (e errCachedData) ListKinds(context.Context, cluster.ClusterID, cluster.ClusterCacheID, string) ([]cluster.ClusterCachedDataKind, error) {
	return nil, e.err
}

func (e errCachedData) WatchKindCollection(context.Context, cluster.ClusterID, cluster.ClusterCacheID, string) (*cluster.Stream[cluster.ClusterCachedDataKindCollectionWatchFrame], error) {
	return nil, e.err
}

func (e errCachedData) WatchEventCollection(context.Context, cluster.ClusterID, cluster.ClusterCacheID) (*cluster.Stream[cluster.ClusterCachedDataEventCollectionWatchFrame], error) {
	return nil, e.err
}

func (e errCachedData) WatchObjectCollection(context.Context, cluster.ClusterID, cluster.ClusterCacheID, string, string) (*cluster.Stream[cluster.ClusterCachedDataObjectCollectionWatchFrame], error) {
	return nil, e.err
}

func (e errCachedData) WatchObjectCollectionByNamespaces(context.Context, cluster.ClusterID, cluster.ClusterCacheID, string, string, []string) (*cluster.Stream[cluster.ClusterCachedDataObjectCollectionWatchFrame], error) {
	return nil, e.err
}

func (e errCachedData) ListObjects(context.Context, cluster.ClusterID, cluster.ClusterCacheID, string, string) ([]cluster.ClusterCachedDataObject, bool, error) {
	return nil, false, e.err
}

func (e errCachedData) Query(context.Context, cluster.ClusterID, cluster.ClusterCacheID, string, int, int, ...any) (cluster.ClusterCachedDataQueryResult, bool, error) {
	return cluster.ClusterCachedDataQueryResult{}, false, e.err
}

func (e errCachedData) ReadOwn(context.Context, cluster.ClusterID, cluster.ClusterCacheID, func(cluster.OwnQuery) error) (bool, error) {
	return false, e.err
}

// errAuth fails Disconnect; Connect's failure arm is covered by
// fakeAuth.connectErr.
type errAuth struct {
	graph.AuthConnector
	err error
}

func (e errAuth) Disconnect(context.Context) error { return e.err }

// errMonitor fails every monitoring call with err.
type errMonitor struct{ err error }

func (errMonitor) ListSignals() []monitor.MonitorSignal { return nil }

func (e errMonitor) SetEnabled(context.Context, monitor.MonitorTargetID, bool) (monitor.MonitorTarget, error) {
	return monitor.MonitorTarget{}, e.err
}

func (e errMonitor) Triage(context.Context, monitor.IncidentID, monitor.TriageModel) (monitor.Incident, error) {
	return monitor.Incident{}, e.err
}

func (e errMonitor) WatchTargetCollection(context.Context) (*monitor.Stream[monitor.MonitorTargetCollectionWatchFrame], error) {
	return nil, e.err
}

func (e errMonitor) WatchIncidentCollection(context.Context) (*monitor.Stream[monitor.IncidentCollectionWatchFrame], error) {
	return nil, e.err
}

func (e errMonitor) SubscribeIncidentOccurrenceEventFeed(context.Context, monitor.IncidentID) (*monitor.Stream[monitor.IncidentOccurrenceEventFeedFrame], error) {
	return nil, e.err
}
