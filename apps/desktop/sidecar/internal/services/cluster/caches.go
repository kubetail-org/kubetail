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

// The ClusterCache kind: one mirror of one cluster identity. Its beehive shapes, the
// record served to resolvers, its collection watch frame, the whole-cache state watches, the
// Caches implementation, and its controller. Mirrors the ClusterCache section of
// graph/schema.graphqls.
package cluster

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/amorey/beehive"
	"github.com/amorey/gobus"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/lifecycle"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubesync"
)

// ClusterCacheGroupKind identifies the ClusterCache beehive resource kind.
var ClusterCacheGroupKind = beehive.GroupKind{Kind: "ClusterCache"}

// ClusterCacheName returns a ClusterCache's name, "{ClusterID}/{serverUID}":
// one cache per identity per cluster, so a UID migration yields a second
// coexisting cache. A creation/dedup key only — caches are enumerated through the
// owner edge.
func ClusterCacheName(clusterID ClusterID, serverUID string) string {
	return string(clusterID) + "/" + serverUID
}

// ClusterCacheSpec: ServerUID is the kube-system UID this cache mirrors — the
// identity a migration turns over. Active-ness is decided by comparing it against
// the parent's last-probed Server.UID; read spec.ServerUID for that, never the name.
// ClusterID is the cluster the cache belongs to, the join key a client folds on; the
// owner edge onto the same cluster is what beehive's GC cascades along.
type ClusterCacheSpec struct {
	ServerUID string    `json:"serverUid"`
	ClusterID ClusterID `json:"clusterId"`
}

// ClusterCacheStatus is empty — this kind reports through its conditions alone;
// the rollup a UI wants is served read-side by Caches().WatchHealthKeyedState.
type ClusterCacheStatus struct{}

// ClusterCache is the view of one ClusterCache beehive object, streamed standalone
// via Caches().Watch and joined to its parent client-side. ID and ClusterID together
// locate the on-disk cache and carry that join. Active-ness is deliberately not a
// field: it is spec.ServerUID matching the parent's last-probed Server.UID, which
// changes with no cache event, so only the client's live join is correct.
//
// The stored spec is served as-is, no projection.
type ClusterCache struct {
	// ID is the ClusterCacheID; see RecordMeta for the rest.
	RecordMeta
	// ClusterID is the Cluster this cache belongs to.
	ClusterID ClusterID
	Spec      ClusterCacheSpec
}

// ClusterCacheCollectionWatchFrame is the ClusterCache-kind counterpart of ClusterCollectionWatchFrame, carried
// on the standalone cache watch. Binds 1:1 to the GraphQL ClusterCacheCollectionWatchFrame.
type ClusterCacheCollectionWatchFrame struct {
	Type  CollectionWatchFrameType
	Cache *ClusterCache
}

// ClusterCacheStats reports a cluster's live on-disk cache statistics.
type ClusterCacheStats struct {
	Exists bool
	// Bytes is what the cache costs on disk, and the three parts it is made of. The headline
	// is the total because that is the question a size answers; the split is what tells a
	// full cache from one whose WAL is not being checkpointed.
	Bytes    int64
	DBBytes  int64
	WALBytes int64
	SHMBytes int64
	// ObjectCount/KindCount are the whole-cache rollup read from the kind
	// catalog's trigger-maintained per-kind counts: the total cached objects and
	// the number of kinds with at least one cached object. Both exclude events
	// (events are not a catalog kind).
	ObjectCount int
	KindCount   int
	// OverSizeLimit is the janitor's verdict on the footprint, and SizeLimitBytes the
	// ceiling it judged against (0 when unbounded). The verdict is soft by one sweep
	// interval, and reads false for a cache nobody has open, which cannot grow.
	OverSizeLimit  bool
	SizeLimitBytes int64
}

// ClusterCacheHealth is one cache's sync verdict over every kind it syncs.
//
// A READ-SIDE projection, not a stored condition: status is for state a dependent
// reacts to, and nothing in the object graph reacts to this — only a UI does. Storing
// it would wake the cache and every watcher each time any of its hundred-plus children
// changed verdict, to publish a number no controller reads.
//
// The per-kind records can't serve that consumer themselves: there is one per synced
// kind, and no single child's verdict is the cache's — ninety-nine healthy kinds and
// one forbidden CRD is not healthy, and reading any one child would call it either way
// at random.
type ClusterCacheHealth struct {
	CacheID ClusterCacheID
	// Status/Reason mirror a Condition's shape (and its reason vocabulary), so a consumer
	// renders this exactly as it renders a per-kind verdict.
	Status ConditionStatus
	Reason string
	// UnhealthyKindRefs names every kind that is not Watching, sorted, each with its own
	// reason — the identity a consumer can act on (look the kind's record up, key a
	// subscription on it), not a rendered phrase. Empty when healthy. Only the entries whose
	// reason is the cache's are behind the verdict; the rest are catching up. It is a
	// list rather than a message because truncation ("+2 more") is a layout decision, and the
	// consumer that needs the first offender must not have to parse it back out of prose.
	UnhealthyKindRefs []UnhealthyKindRef
	// UnansweredKindRefs names every kind that has reported no state this session, sorted:
	// the kinds behind a Connecting verdict. Not on the wire, so no frame is sent for a
	// change to it alone; KubeQuery reads it.
	UnansweredKindRefs []KindRef
	// Failure is the first offender's class of failure (a kubeconn.Reason such as Forbidden);
	// empty when unclassified or not failing.
	Failure string
	// TotalKinds is how many kinds this cache mirrors; UnhealthyKinds how many are not
	// currently Watching (len(UnhealthyKindRefs), which is capped by nothing).
	TotalKinds     int
	UnhealthyKinds int
	// PausedKinds is how many of TotalKinds the user has switched off. Counted separately
	// rather than subtracted from the census: a paused kind is not unhealthy and not
	// missing, and without its own tally it is invisible in every reading — a summary that
	// spends UnhealthyKinds alone would report five kinds syncing when two are paused.
	PausedKinds int
	// PausedKindRefs names every paused kind, sorted. A paused kind keeps its rows, so a
	// reader judging them needs to know which; no other field names one. Not on the
	// wire, like UnansweredKindRefs.
	PausedKindRefs []KindRef
	// LastUpdateAt is the most recent write across every kind — when data last arrived
	// anywhere in this cache; nil until something has landed. LastLiveAt is the OLDEST
	// proof across every kind, and **nil while any of them has none**: a cache is only as
	// verified as its least proven watch, and a kind nothing has proven live is weaker
	// than any stamp its neighbours carry.
	LastUpdateAt *time.Time
	LastLiveAt   *time.Time
}

// Trusted reports whether the cache's rows are this cluster's to answer from:
// listed at least once, under no cache-wide failure. Stale rows are trusted; a
// reader says how stale. Under Connecting, KubeQuery trusts a kind by its cookie
// instead (ListedKinds).
func (h ClusterCacheHealth) Trusted() bool {
	switch h.Reason {
	case "", ReasonConnecting, ReasonSizeLimit:
		return false
	}
	return !RefusedCacheWide(h.Reason)
}

// RefusedCacheWide reports whether a reason, a cache's or a kind's, withholds every
// row of the cache: the connection answers as another cluster, or the file failed.
// Trusted and the per-kind reading under Connecting both refuse through it, so the
// two never disagree.
func RefusedCacheWide(reason string) bool {
	return reason == kubesync.ReasonStoreFailed || reason == kubesync.ReasonIdentityMismatch
}

// ClusterCacheSyncState is one cache's sync detail: how discovery is doing, and a row per
// kind actually being mirrored. A state watch, like the health rollup and for the same reason —
// nothing in the object graph reacts to it, and its counts and freshness stamps move while
// every record under it sits still.
//
// Where ClusterCacheHealth folds a cache into one verdict for a fleet view, this expands one
// cache for the panel a user opened.
type ClusterCacheSyncState struct {
	CacheID ClusterCacheID
	// Exists is false for a pair naming no live cache, whose detail is otherwise empty.
	Exists    bool
	Discovery ClusterCacheDiscoveryStatus
	// Kinds is every mirrored kind, sorted by (APIVersion, Resource) so a rendered list is
	// stable across ticks.
	Kinds []ClusterCacheKindSyncStatus
}

// ClusterCacheDiscoveryStatus is the sweep's own verdict, which is not any kind's: a cluster
// whose /apis document will not load has no kinds to report a failure through.
type ClusterCacheDiscoveryStatus struct {
	Reason  string
	Message string
}

// ClusterCacheKindSyncStatus is one mirrored kind's row. Identity, verdict, freshness, and
// the count — assembled from the two places that own them, and stored in neither.
type ClusterCacheKindSyncStatus struct {
	APIVersion string
	Kind       string
	Resource   string

	// Reason and Message are nil-free: a kind whose worker has committed nothing reads as
	// empty, which is what "no answer yet" looks like everywhere in this family.
	Reason  string
	Message string
	// SinceAt is when Reason last moved — "watching since 10:02". Nil until it has.
	SinceAt *time.Time
	// LastUpdateAt is when data last arrived; LastLiveAt the last proof the stream is live.
	LastUpdateAt *time.Time
	LastLiveAt   *time.Time
	// Restarts counts comebacks inside the current healthy stretch — the flapping question a
	// retry streak cannot answer. NextRetryAt is set only while a run is down.
	Restarts    int
	NextRetryAt *time.Time
	// ObjectCount comes off the store's trigger-maintained per-kind counts, never off the
	// sync seam: kubesync knows only the caches it has armed, where kubestore answers for a
	// paused one too.
	ObjectCount int
}

// cacheSyncEnabled is the pause switch one cache relays into the subtree it owns: the
// cluster's own toggles, and whether this cache still mirrors the identity the cluster
// is probed at. Evaluated once here rather than re-derived per child, so a paused
// subtree cannot disagree with itself.
func cacheSyncEnabled(clusterObj *beehive.Object[ClusterRuntimeSpec, ClusterStatus], cacheUID string) bool {
	return clusterObj.Spec.Enabled && clusterObj.Spec.SyncEnabled && CacheIsActive(clusterObj, cacheUID)
}

// cacheFinalizer holds a cache's row until its store file is gone. Beehive collects
// a finalizer-free object after a failed reconcile as readily as after a successful
// one, so without it a failed file removal loses its retry and the cluster can be
// collected over an orphaned file. With it, the cache holds its owner's collection —
// a cluster object absent means every cache file is gone.
const cacheFinalizer = "kubetail.io/cache-store"

// ensureClusterCache creates the mirror slot for one probed identity, owned by the
// cluster so beehive's GC cascades to it, carrying the finalizer from birth. Idempotent:
// the name is the dedup key, and a later pass has nothing to update — ServerUID *is*
// the identity.
//
// Called by the parent's reconcile, since a controller only ever reconciles an object
// that already exists and the identity is the parent's to discover. The writes live
// here so the kind's vocabulary stays in the kind's file. The finalizer needs the cache
// controller registered, which New does before any pass runs.
//
// GetOrCreate rather than CreateOrUpdate: a found row is returned as-is, which is the
// whole contract here — ServerUID *is* the identity, so a later pass has nothing to
// write, and a row awaiting collection holds its name until GC releases it.
func ensureClusterCache(ctx context.Context, client beehive.Client[ClusterCacheSpec, ClusterCacheStatus], cluster *beehive.Object[ClusterRuntimeSpec, ClusterStatus], serverUID string) error {
	clusterID := clusterIDOf(cluster)
	name := ClusterCacheName(clusterID, serverUID)
	spec := ClusterCacheSpec{ServerUID: serverUID, ClusterID: clusterID}
	if _, _, err := client.GetOrCreate(ctx, name, spec, beehive.WithOwner(cluster.ID), beehive.WithFinalizers(cacheFinalizer)); err != nil {
		return fmt.Errorf("create cluster cache %s: %w", name, err)
	}
	return nil
}

// toClusterCache builds the served record from the stored object.
func toClusterCache(obj *beehive.Object[ClusterCacheSpec, ClusterCacheStatus]) *ClusterCache {
	return &ClusterCache{
		RecordMeta: toRecordMeta(obj),
		ClusterID:  obj.Spec.ClusterID,
		Spec:       obj.Spec,
	}
}

func (a cachesAPI) Get(ctx context.Context, id ClusterCacheID) (*ClusterCache, error) {
	obj, err := a.s.cacheClient.Get(ctx, beehive.ObjectID(id))
	if err != nil {
		// A caller holds ids from watch frames, so a record collected in between is an
		// ordinary race rather than a bad request.
		if errors.Is(err, beehive.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get cluster cache %d: %w", id, err)
	}
	return toClusterCache(obj), nil
}

func (a cachesAPI) WatchCollectionByID(ctx context.Context, id ClusterCacheID) (*Stream[ClusterCacheCollectionWatchFrame], error) {
	src, err := a.s.cacheClient.Watch(ctx, beehive.ObjectID(id))
	if err != nil {
		return nil, fmt.Errorf("watch cluster cache %d: %w", id, err)
	}

	return cacheWatch.streamOne(ctx, src), nil
}

func (a cachesAPI) WatchCollection(ctx context.Context) (*Stream[ClusterCacheCollectionWatchFrame], error) {
	src, err := a.s.cacheClient.WatchList(ctx)
	if err != nil {
		return nil, fmt.Errorf("watch cluster caches: %w", err)
	}

	return cacheWatch.streamList(ctx, src), nil
}

// cacheWatch projects this kind into delta frames. The departure is built by hand:
// beehive reports a removal whose final row it could not decode with no object at
// all, and the frame must still carry the id.
var cacheWatch = collectionWatch[ClusterCacheSpec, ClusterCacheStatus, ClusterCacheCollectionWatchFrame]{
	frame: func(t CollectionWatchFrameType, obj *beehive.Object[ClusterCacheSpec, ClusterCacheStatus]) (ClusterCacheCollectionWatchFrame, error) {
		return ClusterCacheCollectionWatchFrame{Type: t, Cache: toClusterCache(obj)}, nil
	},
	departed: func(change beehive.ObjectChange[ClusterCacheSpec, ClusterCacheStatus]) ClusterCacheCollectionWatchFrame {
		cache := &ClusterCache{RecordMeta: RecordMeta{ID: ObjectID(change.ID)}}
		if obj := change.Object; obj != nil {
			cache = toClusterCache(obj)
		}
		return ClusterCacheCollectionWatchFrame{Type: CollectionWatchFrameDeleted, Cache: cache}
	},
	bookmark: ClusterCacheCollectionWatchFrame{Type: CollectionWatchFrameBookmark},
}

// The per-cluster reads filter the whole kind by the spec's ClusterID, the same join
// the client makes. A cluster owning none reads empty, and one that is not there yet
// reads the same way: a caller may open the watch on a cluster it expects to arrive.
func (a cachesAPI) ListByCluster(ctx context.Context, clusterID ClusterID) ([]*ClusterCache, error) {
	objs, err := a.s.cacheClient.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list cluster %s caches: %w", clusterID, err)
	}
	var caches []*ClusterCache
	for _, obj := range objs {
		if obj.Spec.ClusterID == clusterID {
			caches = append(caches, toClusterCache(obj))
		}
	}
	return caches, nil
}

func (a cachesAPI) WatchCollectionByCluster(ctx context.Context, clusterID ClusterID) (*Stream[ClusterCacheCollectionWatchFrame], error) {
	src, err := a.s.cacheClient.WatchList(ctx)
	if err != nil {
		return nil, fmt.Errorf("watch cluster %s caches: %w", clusterID, err)
	}
	return cacheWatch.streamListWhere(ctx, src, func(f ClusterCacheCollectionWatchFrame) bool {
		return f.Cache == nil || f.Cache.ClusterID == clusterID
	}), nil
}

// defaultStateWatchCadence is how often a state watch re-measures when nothing has pinged it.
// Human-paced: what it covers is a number drifting under a settled record, not an
// event, and every watcher pays for each tick.
const defaultStateWatchCadence = 5 * time.Second

// WatchStatsState measures one cache's file and contents. A pair naming no live cache gets one
// reading that says there is none (Exists false), and nothing after it: a caller holding such a
// pair got it from a frame that has since moved on, and drops the subscription itself.
//
// It re-measures on the store's own change pings and on a cadence, because the two
// cover different halves: pings carry the writes, and the file's size moves with
// checkpoints that ping nothing.
func (a cachesAPI) WatchStatsState(ctx context.Context, clusterID ClusterID, cacheID ClusterCacheID) (*Stream[ClusterCacheStats], error) {
	live, err := a.s.cacheBelongsTo(ctx, clusterID, cacheID)
	if err != nil {
		return nil, err
	}

	return NewStream(ctx, func(ctx context.Context, out chan<- ClusterCacheStats) error {
		if !live {
			sendAbsent(ctx, out, ClusterCacheStats{})
			return nil
		}
		ticker := time.NewTicker(a.s.stateWatchCadence)
		defer ticker.Stop()

		var (
			sub   kubestore.ChangeNotifications
			pings <-chan gobus.Event[string, struct{}]
			last  ClusterCacheStats
			sent  bool
		)
		defer func() {
			if sub != nil {
				sub.Close()
			}
		}()

		for {
			// Bind late and re-bind freely: the store opens when a worker arms, and a
			// clear swaps it for a fresh one whose pings the old subscription never
			// carries.
			if sub == nil {
				if changes, ok := a.s.kubestoreMgr.ListenForChanges(int64(cacheID)); ok {
					sub = changes
					pings = sub.Chan()
				}
			}

			stats, err := a.measureCache(ctx, cacheID)
			if err != nil {
				return err
			}
			if !sent || stats != last {
				if !sendFrame(ctx, out, stats) {
					return nil
				}
				last, sent = stats, true
			}

			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			case _, ok := <-pings:
				if !ok {
					// The store closed under us — a clear, or a shutdown. Re-measure and
					// re-bind rather than ending: the cache is still the caller's.
					sub.Close()
					sub, pings = nil, nil
				}
			}
		}
	}), nil
}

// measureCache is one reading of a cache: its file, and what is in it.
func (a cachesAPI) measureCache(ctx context.Context, cacheID ClusterCacheID) (ClusterCacheStats, error) {
	stats, err := a.s.kubestoreMgr.Stats(ctx, int64(cacheID))
	if err != nil {
		return ClusterCacheStats{}, fmt.Errorf("measure cache %d: %w", cacheID, err)
	}
	return ClusterCacheStats{
		Exists:         stats.Exists,
		Bytes:          stats.Bytes(),
		DBBytes:        stats.DBBytes,
		WALBytes:       stats.WALBytes,
		SHMBytes:       stats.SHMBytes,
		ObjectCount:    stats.ObjectCount,
		KindCount:      stats.KindCount,
		OverSizeLimit:  stats.OverSizeLimit,
		SizeLimitBytes: stats.SizeLimitBytes,
	}, nil
}

// WatchHealthKeyedState reports each live cache's verdict. A read-side projection of the per-kind
// states, which are the workers' own and change constantly: storing those would wake the
// cache and every watcher each time one moved.
//
// The one condition it reads back is the size ceiling's, because that verdict is a decision
// the cache's pass made and wrote down — and the pause it records outlives the file it was
// made on, which no live read could answer for.
//
// It re-reads on a cadence, which is what carries the stamps a filled cache reports: those
// move in healthy steady state precisely when a change signal would be silent. The read is
// the same for every subscriber, so one loop serves them all, reading afresh for a join;
// each subscriber diffs against its own last frames, so a late one still starts from every
// cache's verdict.
func (a cachesAPI) WatchHealthKeyedState(ctx context.Context) (*Stream[ClusterCacheHealth], error) {
	return NewStream(ctx, func(ctx context.Context, out chan<- ClusterCacheHealth) error {
		readings := a.s.health.subscribe(a.s.stateWatchCadence)
		defer readings.Close()

		// sent is the last frame per cache the latest reading reports: a removed cache
		// leaves it with the reading, so it never outgrows the live caches.
		sent := map[ClusterCacheID]ClusterCacheHealth{}
		for {
			healths, err := readings.Next(ctx)
			if err != nil {
				return err
			}
			next := make(map[ClusterCacheID]ClusterCacheHealth, len(healths))
			for _, health := range healths {
				if prev, ok := sent[health.CacheID]; ok && sameHealth(prev, health) {
					next[health.CacheID] = prev
					continue
				}
				if !sendFrame(ctx, out, health) {
					return nil
				}
				next[health.CacheID] = health
			}
			sent = next
		}
	}), nil
}

// readCacheHealth folds one cache's verdict over every kind it mirrors — the records, which
// is what is actually being synced, rather than everything the cluster serves.
//
// **No answer is not an empty answer.** A kind whose worker has committed nothing is not one
// that stopped syncing — a cache still starting, a clear in progress — so it is neither an
// offender nor proof of health: the cache reads as connecting until every kind has spoken.
func (a cachesAPI) readCacheHealth(
	ctx context.Context,
	cacheObj *beehive.Object[ClusterCacheSpec, ClusterCacheStatus],
) (ClusterCacheHealth, error) {
	cacheID := ClusterCacheID(cacheObj.ID)
	kindObjs, err := a.s.kindClient.ListOwnedObjects(ctx, cacheObj.ID)
	if err != nil {
		return ClusterCacheHealth{}, fmt.Errorf("list cluster cache %d cached kinds: %w", cacheID, err)
	}

	health := ClusterCacheHealth{CacheID: cacheID, Status: ConditionFalse, Reason: ReasonConnecting}
	var (
		anyKindUnanswered bool
		anyKindUnproven   bool
		newestUpdateAt    time.Time
		oldestLiveAt      time.Time
		stateByRef        = map[KindRef]kubesync.KindState{}
	)
	for _, kindObj := range kindObjs {
		if kindObj.DeletionRequestedAt != nil {
			continue
		}
		health.TotalKinds++
		// Skipped ahead of the state read, not filtered out of the tallies after it: a
		// paused kind is forgotten, so reading its state would report it as unanswered —
		// and one unanswered kind pins the whole cache at Connecting forever.
		if kindObj.Spec.Paused {
			health.PausedKinds++
			health.PausedKindRefs = append(health.PausedKindRefs, kindRefOf(kindObj.Spec))
			continue
		}

		state, ok := a.s.kubesyncSvc.GetKindState(int64(cacheID), toKubestoreKind(kindObj.Spec))
		if !ok {
			// Unproven as well as unanswered: a kind that has said nothing has proved nothing,
			// so it withholds the cache's proof like any other kind carrying none.
			anyKindUnanswered, anyKindUnproven = true, true
			health.UnansweredKindRefs = append(health.UnansweredKindRefs, kindRefOf(kindObj.Spec))
			continue
		}
		if state.Reason != kubesync.ReasonWatching {
			ref := kindRefOf(kindObj.Spec)
			health.UnhealthyKindRefs = append(health.UnhealthyKindRefs, UnhealthyKindRef{KindRef: ref, Reason: state.Reason})
			stateByRef[ref] = state
		}
		if state.LastUpdateAt.After(newestUpdateAt) {
			newestUpdateAt = state.LastUpdateAt
		}
		// The OLDEST proof across every kind: a cache is only as verified as its least proven
		// watch. A kind carrying no proof is tracked apart from the minimum rather than folded
		// into it, since a zero time is this loop's "nothing yet" as well as a value — folded
		// in, the next kind's stamp would overwrite it and the answer would turn on the order
		// the list came back in.
		switch {
		case state.LastLiveAt.IsZero():
			anyKindUnproven = true
		case oldestLiveAt.IsZero() || state.LastLiveAt.Before(oldestLiveAt):
			oldestLiveAt = state.LastLiveAt
		}
	}

	health.UnhealthyKinds = len(health.UnhealthyKindRefs)
	slices.SortFunc(health.UnhealthyKindRefs, func(a, b UnhealthyKindRef) int {
		return compareKindRefs(a.KindRef, b.KindRef)
	})
	slices.SortFunc(health.UnansweredKindRefs, compareKindRefs)
	slices.SortFunc(health.PausedKindRefs, compareKindRefs)
	health.LastUpdateAt = optionalTime(newestUpdateAt)
	if !anyKindUnproven {
		health.LastLiveAt = optionalTime(oldestLiveAt)
	}

	discovery, _ := a.s.kubesyncSvc.GetDiscoveryState(int64(cacheID))
	switch {
	case FindCondition(cacheObj.Conditions, ConditionSynced) != nil:
		// The cache's own decision, above every arm below: a cache stopped at its size
		// ceiling has armed nothing, and a cache that is also paused kind by kind would
		// otherwise read as merely paused — which is the one thing the two reasons are
		// kept apart to prevent.
		health.Reason = ReasonSizeLimit
	case health.PausedKinds == health.TotalKinds && health.TotalKinds > 0:
		// Ahead of every arm below: paused kinds are skipped, so a fully paused cache has
		// no offenders and nothing unanswered — and the Watching arm would call it healthy.
		health.Reason = ReasonPaused
	case discovery.Reason == kubesync.ReasonStoreFailed, discovery.Reason == kubesync.ReasonIdentityMismatch:
		// The cache's own verdicts, above the per-kind fold. A file that will not open arms
		// nothing, so every kind reads as unanswered and the default below would report a
		// permanently broken cache as still connecting. A context reaching another server
		// is a cache-wide refusal, and a kind reports it only once its own run has hit the
		// gate: until every kind has, the cache would read Connecting, which answers from
		// the other cluster's rows.
		health.Reason = discovery.Reason
	case health.TotalKinds == 0 || anyKindUnanswered:
		// Still connecting, which is not Paused: an enabled cluster's cache has no kinds
		// until its sweep has run, and calling that paused would show a user their own
		// enabled cluster as switched off.
	case health.UnhealthyKinds == 0:
		health.Status, health.Reason = ConditionTrue, kubesync.ReasonWatching
	default:
		// The first offender's own reason and failure, so a consumer reading the verdict
		// beside the first ref sees one story.
		first := stateByRef[health.UnhealthyKindRefs[0].KindRef]
		health.Reason, health.Failure = first.Reason, string(first.Failure)
	}
	return health, nil
}

// kindRefOf is the (APIVersion, Resource) pair that identifies a kind on the wire.
func kindRefOf(spec ClusterCachedKindSpec) KindRef {
	return KindRef{APIVersion: spec.APIVersion, Resource: spec.Resource}
}

// compareKindRefs orders kinds by apiVersion, then resource, so rendered lists are
// stable across ticks.
// sendAbsent is a state watch's one reading for a pair naming no live cache: its value now,
// which nothing will change, held until the subscriber leaves.
func sendAbsent[T any](ctx context.Context, out chan<- T, absent T) {
	if sendFrame(ctx, out, absent) {
		<-ctx.Done()
	}
}

func compareKindRefs(a, b KindRef) int {
	return cmp.Or(cmp.Compare(a.APIVersion, b.APIVersion), cmp.Compare(a.Resource, b.Resource))
}

// WatchSyncState streams one cache's sync detail as a state watch: the discovery verdict and a
// row per mirrored kind. The only thing on the wire that carries a per-kind verdict. A pair
// naming no live cache gets one reading with Exists false, as WatchStatsState does.
//
// It re-reads on the cadence and never on a signal, because its counts and freshness stamps
// move with no reason change — the only thing the sync seam signals on — so a state watch waiting
// for one would go quiet exactly while the cache is healthy.
func (a cachesAPI) WatchSyncState(ctx context.Context, clusterID ClusterID, cacheID ClusterCacheID) (*Stream[ClusterCacheSyncState], error) {
	live, err := a.s.cacheBelongsTo(ctx, clusterID, cacheID)
	if err != nil {
		return nil, err
	}

	return NewStream(ctx, func(ctx context.Context, out chan<- ClusterCacheSyncState) error {
		if !live {
			sendAbsent(ctx, out, ClusterCacheSyncState{CacheID: cacheID})
			return nil
		}
		ticker := time.NewTicker(a.s.stateWatchCadence)
		defer ticker.Stop()

		var (
			sent    ClusterCacheSyncState
			hasSent bool
		)
		for {
			status, err := a.readSyncState(ctx, cacheID)
			if err != nil {
				return err
			}
			// Only when it moved: a state watch is current-on-subscribe, so the first read always
			// goes out, and an idle cache then says nothing rather than a frame per tick.
			if !hasSent || !sameSyncState(sent, status) {
				if !sendFrame(ctx, out, status) {
					return nil
				}
				sent, hasSent = status, true
			}

			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
	}), nil
}

// Health is one reading of the cache's verdict, folded the way WatchHealthKeyedState folds it.
func (a cachesAPI) Health(ctx context.Context, clusterID ClusterID, cacheID ClusterCacheID) (ClusterCacheHealth, bool, error) {
	cacheObj, err := a.s.cacheClient.Get(ctx, beehive.ObjectID(cacheID), beehive.LoadOwner())
	if errors.Is(err, beehive.ErrNotFound) {
		return ClusterCacheHealth{}, false, nil
	}
	if err != nil {
		return ClusterCacheHealth{}, false, fmt.Errorf("get cluster cache %d: %w", cacheID, err)
	}
	cluster, err := a.clusterFor(ctx, cacheObj, map[beehive.ObjectID]*beehive.Object[ClusterRuntimeSpec, ClusterStatus]{})
	if err != nil {
		return ClusterCacheHealth{}, false, err
	}
	if cluster == nil || clusterIDOf(cluster) != clusterID {
		return ClusterCacheHealth{}, false, nil
	}
	health, err := a.readCacheHealthOf(ctx, cluster, cacheObj)
	if err != nil {
		return ClusterCacheHealth{}, false, err
	}
	return health, true, nil
}

// SyncState is one reading of the cache's sync detail, as WatchSyncState reads it.
func (a cachesAPI) SyncState(ctx context.Context, clusterID ClusterID, cacheID ClusterCacheID) (ClusterCacheSyncState, bool, error) {
	live, err := a.s.cacheBelongsTo(ctx, clusterID, cacheID)
	if err != nil || !live {
		return ClusterCacheSyncState{}, false, err
	}
	status, err := a.readSyncState(ctx, cacheID)
	if err != nil {
		return ClusterCacheSyncState{}, false, err
	}
	return status, true, nil
}

// readSyncState is one reading of a cache's sync detail.
func (a cachesAPI) readSyncState(ctx context.Context, cacheID ClusterCacheID) (ClusterCacheSyncState, error) {
	kindObjs, err := a.s.kindClient.ListOwnedObjects(ctx, beehive.ObjectID(cacheID))
	if err != nil {
		return ClusterCacheSyncState{}, fmt.Errorf("list cluster cache %d cached kinds: %w", cacheID, err)
	}
	objectCounts, err := a.readObjectCountsByKind(ctx, cacheID)
	if err != nil {
		return ClusterCacheSyncState{}, err
	}

	status := ClusterCacheSyncState{CacheID: cacheID, Exists: true}
	if discovery, ok := a.s.kubesyncSvc.GetDiscoveryState(int64(cacheID)); ok {
		status.Discovery = ClusterCacheDiscoveryStatus{Reason: discovery.Reason, Message: discovery.Message}
	}
	for _, kindObj := range kindObjs {
		if kindObj.DeletionRequestedAt != nil {
			continue
		}
		row := ClusterCacheKindSyncStatus{
			APIVersion:  kindObj.Spec.APIVersion,
			Kind:        kindObj.Spec.Kind,
			Resource:    kindObj.Spec.Resource,
			ObjectCount: objectCounts[kindRefOf(kindObj.Spec)],
		}
		// The record's own verdict first: a paused kind is forgotten, so the read below
		// would come back empty and the row would carry no reason at all.
		if kindObj.Spec.Paused {
			row.Reason = ReasonPaused
		} else if state, ok := a.s.kubesyncSvc.GetKindState(int64(cacheID), toKubestoreKind(kindObj.Spec)); ok {
			row.Reason = state.Reason
			row.Message = state.Message
			row.Restarts = state.Restarts
			row.SinceAt = optionalTime(state.SinceAt)
			row.LastUpdateAt = optionalTime(state.LastUpdateAt)
			row.LastLiveAt = optionalTime(state.LastLiveAt)
			row.NextRetryAt = optionalTime(state.NextRetryAt)
		}
		status.Kinds = append(status.Kinds, row)
	}
	slices.SortFunc(status.Kinds, func(a, b ClusterCacheKindSyncStatus) int {
		return cmp.Or(cmp.Compare(a.APIVersion, b.APIVersion), cmp.Compare(a.Resource, b.Resource))
	})
	return status, nil
}

// readObjectCountsByKind is every mirrored kind's row count in one read. A cache with no file
// has none, which is a paused cache or one nothing has swept — not an error.
func (a cachesAPI) readObjectCountsByKind(ctx context.Context, cacheID ClusterCacheID) (map[KindRef]int, error) {
	store, ok, err := a.s.kubestoreMgr.OpenExisting(int64(cacheID))
	if err != nil {
		return nil, fmt.Errorf("open cluster cache %d: %w", cacheID, err)
	}
	if !ok {
		return nil, nil
	}
	defer store.Release()

	rows, err := store.Kinds(ctx)
	if err != nil {
		return nil, fmt.Errorf("read cluster cache %d kinds: %w", cacheID, err)
	}
	counts := make(map[KindRef]int, len(rows))
	for _, row := range rows {
		counts[KindRef{APIVersion: row.APIVersion, Resource: row.Resource}] = row.Count
	}
	return counts, nil
}

// optionalTime is a stamp as the wire carries it: absent rather than the zero instant, which
// a consumer would otherwise render as 1 January year 1.
func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// Clear empties a cache's log file, then its store, swapping the file under whoever holds
// it open. The workers writing through the store are stopped for the whole swap and
// started again after — a clear that failed leaves a cache that still syncs.
func (a cachesAPI) Clear(ctx context.Context, id ClusterCacheID) (*ClusterCache, error) {
	obj, err := a.s.cacheClient.Get(ctx, beehive.ObjectID(id), beehive.LoadOwner())
	if errors.Is(err, beehive.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get cluster cache %d: %w", id, err)
	}

	// The log file first, outside the hold since no sync worker writes it: a failure here
	// leaves the cache as it was, and the error has the user clear again.
	if err := a.s.cacheFiles.Clear(int64(id)); err != nil {
		return nil, fmt.Errorf("clear cluster cache %d log file: %w", id, err)
	}

	// Inside the sync's hold: the manager swaps the file under whoever holds it open, and a
	// relist page or a catalog write landing across that swap goes into a file nothing holds.
	// The claims stay taken throughout, which is what the manager reopens a fresh file for.
	if err := a.s.kubesyncSvc.RunWithCacheSyncStopped(int64(id), func() error {
		return a.s.kubestoreMgr.Clear(int64(id))
	}); err != nil {
		return nil, fmt.Errorf("clear cluster cache %d: %w", id, err)
	}

	// The pass has to be told: an empty file wakes nothing on its own, and a cache stopped
	// at its ceiling holds no claim — so nothing reopens it, no janitor sweeps it, and no
	// verdict follows. The backoff goes with the requeue, since a clear is precisely what
	// proves an earlier pass's verdict stale. Logged rather than returned: the clear landed,
	// and a failure here is a record already gone or a process already stopping.
	if err := a.s.cacheClient.Requeue(ctx, beehive.ObjectID(id), beehive.WithResetBackoff()); err != nil {
		slog.Warn("cluster cache: requeue after clear failed", "cacheID", id, "err", err)
	}

	return toClusterCache(obj), nil
}

// readAllCacheHealth is every live cache's verdict, read off the records.
//
// It reads them rather than diffing against what a stream has sent, because a subscriber
// that arrives after a cache went quiet never witnessed the transition and would otherwise
// hear nothing about that cache at all. A keyed state watch has no removal frame,
// so silence reads as the last verdict — or as no verdict ever.
//
// A cache whose pause switch (`cacheSyncEnabled`) is off reads Paused outright; one that is
// on folds its kinds. A cache being collected is skipped: its own record says it is going.
func (a cachesAPI) readAllCacheHealth(ctx context.Context) ([]ClusterCacheHealth, error) {
	cacheObjs, err := a.s.cacheClient.List(ctx, beehive.LoadOwner())
	if err != nil {
		return nil, fmt.Errorf("list cluster caches: %w", err)
	}

	var healths []ClusterCacheHealth
	clusters := map[beehive.ObjectID]*beehive.Object[ClusterRuntimeSpec, ClusterStatus]{}
	for _, cacheObj := range cacheObjs {
		if cacheObj.DeletionRequestedAt != nil {
			continue
		}
		cluster, err := a.clusterFor(ctx, cacheObj, clusters)
		if err != nil {
			return nil, err
		}
		if cluster == nil {
			// The cluster is gone, so this cache is going with it.
			continue
		}
		health, err := a.readCacheHealthOf(ctx, cluster, cacheObj)
		if err != nil {
			return nil, err
		}
		healths = append(healths, health)
	}
	slices.SortFunc(healths, func(a, b ClusterCacheHealth) int { return cmp.Compare(a.CacheID, b.CacheID) })
	return healths, nil
}

// readCacheHealthOf is one cache's verdict under its cluster: Paused outright when its
// pause switch is off, else the fold of its kinds.
func (a cachesAPI) readCacheHealthOf(
	ctx context.Context,
	cluster *beehive.Object[ClusterRuntimeSpec, ClusterStatus],
	cacheObj *beehive.Object[ClusterCacheSpec, ClusterCacheStatus],
) (ClusterCacheHealth, error) {
	if !cacheSyncEnabled(cluster, cacheObj.Spec.ServerUID) {
		return ClusterCacheHealth{CacheID: ClusterCacheID(cacheObj.ID), Status: ConditionFalse, Reason: ReasonPaused}, nil
	}
	return a.readCacheHealth(ctx, cacheObj)
}

// clusterFor resolves a cache's cluster, memoised for the fold: caches of one cluster
// share it, and this runs on the state watch's cadence.
func (a cachesAPI) clusterFor(
	ctx context.Context,
	cache *beehive.Object[ClusterCacheSpec, ClusterCacheStatus],
	seen map[beehive.ObjectID]*beehive.Object[ClusterRuntimeSpec, ClusterStatus],
) (*beehive.Object[ClusterRuntimeSpec, ClusterStatus], error) {
	owner, ok, err := cache.Owner()
	if err != nil {
		return nil, fmt.Errorf("read cluster cache %d owner: %w", cache.ID, err)
	}
	if !ok {
		return nil, nil
	}
	if cluster, held := seen[owner.ID]; held {
		return cluster, nil
	}

	cluster, err := a.s.clusterClient.Get(ctx, owner.ID)
	if errors.Is(err, beehive.ErrNotFound) {
		seen[owner.ID] = nil
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read cluster %d: %w", owner.ID, err)
	}
	seen[owner.ID] = cluster
	return cluster, nil
}

// sameHealth reports whether a cache's verdict moved. The refs are a slice, so the
// struct is not comparable — and re-sending an unchanged verdict is exactly what the
// cadence must not do. UnansweredKindRefs is not on the wire, so it is not compared.
func sameHealth(a, b ClusterCacheHealth) bool {
	return a.Status == b.Status && a.Reason == b.Reason && a.Failure == b.Failure &&
		a.TotalKinds == b.TotalKinds && a.UnhealthyKinds == b.UnhealthyKinds &&
		a.PausedKinds == b.PausedKinds &&
		slices.Equal(a.UnhealthyKindRefs, b.UnhealthyKindRefs) &&
		sameTime(a.LastUpdateAt, b.LastUpdateAt) && sameTime(a.LastLiveAt, b.LastLiveAt)
}

// sameSyncState is equality for the state watch's dedupe. Hand-written because the row carries
// optional times as pointers, which == would compare by address.
func sameSyncState(a, b ClusterCacheSyncState) bool {
	return a.CacheID == b.CacheID && a.Exists == b.Exists && a.Discovery == b.Discovery &&
		slices.EqualFunc(a.Kinds, b.Kinds, sameKindSyncStatus)
}

func sameKindSyncStatus(a, b ClusterCacheKindSyncStatus) bool {
	return a.APIVersion == b.APIVersion && a.Kind == b.Kind && a.Resource == b.Resource &&
		a.Reason == b.Reason && a.Message == b.Message &&
		a.Restarts == b.Restarts && a.ObjectCount == b.ObjectCount &&
		sameTime(a.SinceAt, b.SinceAt) && sameTime(a.LastUpdateAt, b.LastUpdateAt) &&
		sameTime(a.LastLiveAt, b.LastLiveAt) && sameTime(a.NextRetryAt, b.NextRetryAt)
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// categoryDiscovery and categorySync name the two timelines this subsystem writes: the
// cache's own sweep verdicts, and one kind's worker transitions. The axis beehive bounds
// retention on, which is why each is a category rather than a reason prefix.
const (
	categoryDiscovery = "discovery"
	categorySync      = "sync"
)

// clusterCacheController arms one cache's sync, mirrors what its cluster serves into the
// per-kind records it owns, and logs the sweep's verdict to its own timeline.
type clusterCacheController struct {
	lifecycle.None
	// Every kind's client, not just this one's: the per-kind records a cache owns are
	// written from here.
	deps
}

func (c *clusterCacheController) Reconcile(
	ctx context.Context,
	client beehive.ControllerClient[ClusterCacheStatus],
	obj *beehive.Object[ClusterCacheSpec, ClusterCacheStatus],
) beehive.Result {
	cacheID := int64(obj.ID)

	// A cache on its way out is collected with the subtree it owns once its finalizer
	// clears, and the finalizer clears only once its file is gone: a failed removal
	// fails the pass with the finalizer intact, so beehive retries under backoff and
	// the cluster's collection waits behind it. Remove is idempotent, so a retry after
	// a crash between the removal and the clear succeeds on the missing file and clears
	// it then. Remove tombstones the id, so nothing below can claim the file back.
	if obj.DeletionRequestedAt != nil {
		// First, and that is the whole ordering: it returns only once nothing can still
		// write through the file the next line deletes. ForgetCache rather than
		// ForgetDiscovery, because the kinds go with the cache — a pause is what keeps them.
		c.kubesyncSvc.ForgetCache(cacheID)
		if err := c.kubestoreMgr.Remove(cacheID); err != nil {
			return beehive.Fail(fmt.Errorf("remove cluster cache %d store: %w", obj.ID, err))
		}
		if err := c.cacheFiles.Remove(cacheID); err != nil {
			return beehive.Fail(fmt.Errorf("remove cluster cache %d log file: %w", obj.ID, err))
		}
		if err := client.DeleteFinalizer(ctx, cacheFinalizer); err != nil {
			return beehive.Fail(fmt.Errorf("clear cluster cache %d finalizer: %w", obj.ID, err))
		}
		return beehive.Settled()
	}

	limited, measured := c.checkSizeLimit(ctx, obj)
	if err := c.reportSizeLimit(ctx, client, obj, limited, measured); err != nil {
		return beehive.Fail(err)
	}
	if err := c.armSync(ctx, client, obj, limited); err != nil {
		return beehive.Fail(err)
	}
	if err := c.mirrorKinds(ctx, obj); err != nil {
		return beehive.Fail(err)
	}
	if err := c.logDiscoveryVerdict(ctx, client, cacheID); err != nil {
		return beehive.Fail(err)
	}
	// Settling is this pass's to report: an object left unsettled is re-dispatched by
	// beehive's owed pass forever.
	return beehive.Settled()
}

// checkSizeLimit answers whether this cache's sync stays off because its file is at its
// ceiling, and hands back the measurement it decided on — nil when there was none.
//
// The janitor's verdict is the only way in, and the record's condition is what keeps the
// cache there: pausing releases the store claim, which closes the file — and a closed file
// reports OverSizeLimit false, so a pass reading the verdict alone would restart the cache
// it had just stopped, refill it, and stop it again.
//
// Which is why the way out is the bytes rather than the verdict. Nothing writes a stopped
// cache, so there is no WAL mid-checkpoint for a measurement and the janitor to disagree
// over. A limit of zero is an unbounded manager or a file that is not there; both release.
//
// A measurement that failed decides nothing: answering under would restart a cache nobody
// measured, and answering over would stop one over an error.
func (c *clusterCacheController) checkSizeLimit(
	ctx context.Context,
	obj *beehive.Object[ClusterCacheSpec, ClusterCacheStatus],
) (bool, *kubestore.Stats) {
	// Presence, not status: a liveness condition an earlier process wrote reads as Unknown
	// until this pass re-confirms it, and reading that as "not stopped" would release every
	// stopped cache on restart.
	stopped := FindCondition(obj.Conditions, ConditionSynced) != nil

	stats, err := c.kubestoreMgr.Stats(ctx, int64(obj.ID))
	if err != nil {
		slog.Warn("cluster cache: size check failed", "cacheID", obj.ID, "err", err)
		return stopped, nil
	}
	if stopped {
		return stats.SizeLimitBytes > 0 && stats.Bytes() > stats.SizeLimitBytes, &stats
	}
	return stats.OverSizeLimit, &stats
}

// reportSizeLimit keeps the record's Synced condition in step with the verdict above. It is
// written while the cache is stopped — a restart needs the re-confirmation, since the
// condition it left behind reads as Unknown — and deleted on the way out. A cache that was
// never over its limit is written nothing, which is every cache on every pass.
//
// An unmeasured pass writes nothing either: it has no size to name, and what already stands
// is the last measurement anyone took.
func (c *clusterCacheController) reportSizeLimit(
	ctx context.Context,
	client beehive.ControllerClient[ClusterCacheStatus],
	obj *beehive.Object[ClusterCacheSpec, ClusterCacheStatus],
	limited bool,
	measured *kubestore.Stats,
) error {
	switch {
	case limited && measured != nil:
		cond := LiveCondition(ConditionSynced, ConditionFalse, ReasonSizeLimit,
			fmt.Sprintf("cache is %d bytes, over its %d-byte limit", measured.Bytes(), measured.SizeLimitBytes))
		if err := client.SetCondition(ctx, cond); err != nil {
			return fmt.Errorf("set cluster cache %d size condition: %w", obj.ID, err)
		}
	case !limited && FindCondition(obj.Conditions, ConditionSynced) != nil:
		if err := client.DeleteCondition(ctx, string(ConditionSynced)); err != nil {
			return fmt.Errorf("clear cluster cache %d size condition: %w", obj.ID, err)
		}
	}
	return nil
}

// armSync relays the pause switch onto the whole subtree, in one call each way. Arming is
// policy and never interest: nothing a reader does starts a sync, and pausing writes no
// record and requeues none — the kinds stay registered under kubesync, so a resume starts
// every one of them again.
//
// The switch is NOT relayed onto the per-kind records: the two levels AND rather than nest, so
// pausing is one call here rather than a write onto hundreds of children.
func (c *clusterCacheController) armSync(
	ctx context.Context,
	client beehive.ControllerClient[ClusterCacheStatus],
	obj *beehive.Object[ClusterCacheSpec, ClusterCacheStatus],
	sizeLimited bool,
) error {
	cacheID := int64(obj.ID)

	clusterObj, err := c.loadCluster(ctx, obj)
	if err != nil {
		return err
	}
	// **A relayed value needs a depends_on edge; the owner edge is not one.** The switch below
	// lives on the cluster and is never written to ClusterCacheSpec, which is identity-only —
	// so without this a paused cluster's cache keeps syncing until something unrelated wakes
	// it. Beehive records nothing when the edge is already there, so every later pass is free.
	if err := client.AddDependency(ctx, clusterObj.ID); err != nil {
		return fmt.Errorf("depend cluster cache %d on its cluster: %w", obj.ID, err)
	}

	// Only a kubeconfig-sourced record has credentials to dial over. Another source's are
	// not this pass's to guess at, so nothing is armed rather than a session with no context.
	contextName := kubeconfigContextOf(clusterObj.Spec)
	if contextName == "" || sizeLimited || !cacheSyncEnabled(clusterObj, obj.Spec.ServerUID) {
		c.kubesyncSvc.ForgetDiscovery(cacheID)
		return nil
	}
	c.kubesyncSvc.TrackDiscovery(cacheID, kubesync.Params{
		ContextName: contextName,
		ServerUID:   obj.Spec.ServerUID,
	})
	return nil
}

// loadCluster loads the cluster a cache hangs off. A cache with no owner is unreachable
// through the object graph — beehive's GC takes it — so this reports the error rather than
// inventing a cluster whose switches would all read off.
func (c *clusterCacheController) loadCluster(
	ctx context.Context,
	obj *beehive.Object[ClusterCacheSpec, ClusterCacheStatus],
) (*beehive.Object[ClusterRuntimeSpec, ClusterStatus], error) {
	owner, ok, err := c.cacheClient.GetOwner(ctx, obj.ID)
	if err != nil {
		return nil, fmt.Errorf("read cluster cache %d owner: %w", obj.ID, err)
	}
	if !ok {
		return nil, fmt.Errorf("cluster cache %d has no cluster", obj.ID)
	}
	clusterObj, err := c.clusterClient.Get(ctx, owner.ID)
	if err != nil {
		return nil, fmt.Errorf("read cluster cache %d cluster: %w", obj.ID, err)
	}
	return clusterObj, nil
}

// mirrorKinds converges the per-kind records onto what the cluster serves: a record per
// catalog row, and no record for a row that is gone. A set reconcile, since
// ClusterCachedKindName is the dedup key and needs no per-child bookkeeping.
//
// **The desired set comes off disk, never off the seam.** kind_catalog is the one place the
// served set lives, so a partial sweep is a table that kept its rows and a restart has an
// answer before any sweep runs.
func (c *clusterCacheController) mirrorKinds(ctx context.Context, obj *beehive.Object[ClusterCacheSpec, ClusterCacheStatus]) error {
	catalogRows, everSwept, err := c.readKindCatalog(ctx, int64(obj.ID))
	if err != nil {
		return err
	}

	desired := make(map[string]ClusterCachedKindSpec, len(catalogRows))
	for _, row := range catalogRows {
		desired[ClusterCachedKindName(obj.ID, row.APIVersion, row.Resource)] = ClusterCachedKindSpec{
			APIVersion: row.APIVersion,
			Kind:       row.Kind,
			Resource:   row.Resource,
			Namespaced: row.Scope == kubestore.ScopeNamespaced,
		}
	}

	// One read for both halves: the sweep runs on a cadence over a cache with hundreds of
	// kinds, and both the write below and the prune answer off the same set of records.
	stored, err := c.storedKinds(ctx, obj.ID)
	if err != nil {
		return err
	}
	if err := c.upsertKinds(ctx, obj.ID, desired, stored); err != nil {
		return err
	}
	// **An unswept table deletes nothing.** No fingerprint means no answer has ever been
	// written there, which is not the same as a cluster that serves nothing — and only the
	// second of those may prune.
	if !everSwept {
		return nil
	}
	return c.pruneKinds(ctx, desired, stored)
}

// storedKinds is every record under a cache, by name — deletion-pending ones included,
// which is what lets the prune below tell a record already going from one to mark.
func (c *clusterCacheController) storedKinds(
	ctx context.Context,
	cacheID beehive.ObjectID,
) (map[string]*beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus], error) {
	kindObjs, err := c.kindClient.ListOwnedObjects(ctx, cacheID)
	if err != nil {
		return nil, fmt.Errorf("list cluster cache %d cached kinds: %w", cacheID, err)
	}
	stored := make(map[string]*beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus], len(kindObjs))
	for _, kindObj := range kindObjs {
		stored[kindObj.Name] = kindObj
	}
	return stored, nil
}

// upsertKinds writes one record per desired kind whose catalog fields moved, and nothing
// at all for the rest.
//
// **Writing every pass would un-pause every kind within one discovery interval.** The
// desired spec is built from the catalog row alone, so it carries the zero value of the
// one field the catalog does not own — and this pass runs on a cadence, against a cache
// with hundreds of kinds.
// The carry-forward below reads the record again under the lock rather than trusting the
// snapshot: the pass lists before it locks, so a pause landing in that window is already
// stored and missing from what it holds. Rereading rather than widening the lock over the
// list — that list is hundreds of kinds on a cadence, and holding the setter's lock across
// it would park every user mutation behind a sweep, where the reread costs one read on the
// rare pass where a catalog field actually moved.
func (c *clusterCacheController) upsertKinds(
	ctx context.Context,
	cacheID beehive.ObjectID,
	desired map[string]ClusterCachedKindSpec,
	stored map[string]*beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus],
) error {
	c.kindSpecMu.Lock()
	defer c.kindSpecMu.Unlock()

	for name, spec := range desired {
		if obj, held := stored[name]; held {
			if sameCatalogFields(obj.Spec, spec) {
				continue
			}
			// The user's switch rides through a catalog change: the desired spec is built
			// from the row alone, so it carries Paused's zero value and writing it whole
			// would resume a kind because its singular was renamed.
			paused, err := c.storedPause(ctx, obj.ID)
			if err != nil {
				return err
			}
			spec.Paused = paused
		}
		// CreateOrUpdate rather than the GetOrCreate that creates a cache, whose name IS its
		// whole spec: a kind's carries data outside its name — the singular, and the scope —
		// so a renamed or re-scoped kind converges in place.
		_, _, err := c.kindClient.CreateOrUpdate(ctx, name, spec, beehive.WithOwner(cacheID))
		// A record already marked for deletion holds its name until GC releases it; the kind
		// is created again by a later pass, off the same catalog row.
		if err != nil && !errors.Is(err, beehive.ErrDeletionPending) {
			return fmt.Errorf("mirror cached kind %s: %w", name, err)
		}
	}
	return nil
}

// storedPause is one record's switch as it stands now. A record collected since the pass
// listed it has no pause to carry, and the write that follows creates it afresh.
func (c *clusterCacheController) storedPause(ctx context.Context, id beehive.ObjectID) (bool, error) {
	obj, err := c.kindClient.Get(ctx, id)
	if errors.Is(err, beehive.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read cached kind %d: %w", id, err)
	}
	return obj.Spec.Paused, nil
}

// sameCatalogFields reports whether the four fields the discovery sweep owns match. Paused
// is deliberately absent: it is the user's, and a difference there is not the sweep's to
// converge.
func sameCatalogFields(a, b ClusterCachedKindSpec) bool {
	return a.APIVersion == b.APIVersion && a.Kind == b.Kind &&
		a.Resource == b.Resource && a.Namespaced == b.Namespaced
}

// pruneKinds marks every record the desired set no longer names. Marked, not collected: the
// record's own pass clears the rows behind it first.
func (c *clusterCacheController) pruneKinds(
	ctx context.Context,
	desired map[string]ClusterCachedKindSpec,
	stored map[string]*beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus],
) error {
	for name, kindObj := range stored {
		if _, wanted := desired[name]; wanted || kindObj.DeletionRequestedAt != nil {
			continue
		}
		if err := c.kindClient.Delete(ctx, kindObj.ID); err != nil && !errors.Is(err, beehive.ErrNotFound) {
			return fmt.Errorf("drop cached kind %s: %w", name, err)
		}
	}
	return nil
}

// readKindCatalog is the served set as the sweep last wrote it, plus whether one ever has.
//
// Three properties this rests on: OpenExisting never CREATES a file, so a pass before any
// sweep prunes nothing; the fingerprint's absence is the "never swept" bit; and rows and
// fingerprint come out of one read transaction, so a stale fingerprint can never pass its
// check beside a clear's empty table.
func (c *clusterCacheController) readKindCatalog(ctx context.Context, cacheID int64) (rows []kubestore.KindRow, everSwept bool, err error) {
	store, ok, err := c.kubestoreMgr.OpenExisting(cacheID)
	if err != nil {
		return nil, false, fmt.Errorf("open cluster cache %d: %w", cacheID, err)
	}
	if !ok {
		return nil, false, nil
	}
	defer store.Release()

	rows, _, everSwept, err = store.KindsWithFingerprint(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("read cluster cache %d kind catalog: %w", cacheID, err)
	}
	return rows, everSwept, nil
}

// logDiscoveryVerdict records the sweep's verdict on the cache's own timeline. Every pass,
// because repeating a run's (Category, Type, Reason) extends that run rather than appending —
// so a flapping sweep costs one row per transition and a settled one costs nothing.
//
// A session suspended for want of a connection writes nothing: that fact is the CLUSTER's,
// already on its own timeline, and logging it per cache is the same news twice.
func (c *clusterCacheController) logDiscoveryVerdict(
	ctx context.Context,
	client beehive.ControllerClient[ClusterCacheStatus],
	cacheID int64,
) error {
	state, ok := c.kubesyncSvc.GetDiscoveryState(cacheID)
	if !ok || state.Reason == kubesync.ReasonNoConnection || state.Reason == kubesync.ReasonIdentityMismatch {
		return nil
	}
	if err := client.AddEvent(ctx, beehive.EventSpec{
		Category: categoryDiscovery,
		Type:     discoveryEventType(state.Reason),
		Reason:   state.Reason,
		Message:  state.Message,
	}); err != nil {
		return fmt.Errorf("log cluster cache %d discovery: %w", cacheID, err)
	}
	return nil
}

// discoveryEventType grades a sweep verdict. Partial is a warning rather than a failure: the
// catalog refreshed, and one group-version did not answer.
func discoveryEventType(reason string) beehive.EventType {
	switch reason {
	case kubesync.ReasonDiscoveryFailed, kubesync.ReasonPartial:
		return beehive.EventWarning
	default:
		return beehive.EventNormal
	}
}
