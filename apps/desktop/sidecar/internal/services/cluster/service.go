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

// Package cluster is the sidecar's Kubernetes boundary: Service and its four
// record families (Clusters, Caches, CachedKinds, CachedData).
//
// This file specifies the whole API — Service and the four family interfaces — and
// bootstraps beehive. One file per family implements it and holds everything else
// about that kind: its beehive shapes, the record served to GraphQL, its collection watch
// frame, and the controller that writes it. shared.go holds the vocabulary every
// family reuses. Every beehive detail lives behind the Service interface.
//
// Layering: a controller holds policy only, and the mechanisms it drives are leaves
// under internal/ — private to this package by compiler rule. The leaves speak native
// vocabulary (GVRs, a rest.Config, cache rows) and never the records above; the
// controllers translate. A leaf that reaches for one of these types gets an import
// cycle, which is the enforcement. The connection surface is the exception: it reads no
// beehive object, so its types alias straight through and the leaf's exported shape is
// the boundary's. Mechanism growing in a controller instead is the signal to extract
// another leaf: this package's tests stay fast only while the controllers do no I/O of
// their own.
//
// The clusters table in app.db owns a cluster's identity and the user's choices; the
// beehive kinds hold the runtime and their ownership chain:
//
//	Cluster                 (name: the clusters row id; the mirror keeps one per row)
//	    ↓ owns
//	ClusterCache            (name: "{ClusterID}/{serverUID}")
//	    ↓ owns
//	ClusterCachedKind       (name: "cachedkind/{CacheID}/{apiVersion}/{resource}")
//
// A cluster's runtime object is looked up by name, which is its ClusterID; every
// other name is a per-kind reconcile key rather than an identity. The on-disk cache
// is keyed by ObjectID, so a name's arbitrary text never reaches the filesystem.
//
// A kind's GroupKind.Kind string is its Go type name, and both the Kind and the name
// prefixes above are persisted. Renaming one is a store migration the moment anything
// writes.
//
// Cluster carries connection status (Connected, Identified conditions + server/principal
// facts); its ClusterCache child carries sync status, folded per kind from the
// ClusterCachedKind records below it.
package cluster

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/amorey/beehive"
	beehivesqlite "github.com/amorey/beehive/sqlite"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/drain"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/lifecycle"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubeconn"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubesync"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/poke"
)

// Service is the frontend-facing boundary: every beehive detail (names, owner chain,
// spec/status split, collection watch mapping) lives behind it. Each record family hangs
// off its own sub-interface; only the connection surface, which reads no beehive
// object, sits at the top level.
//
// Every method that keeps delivering is named for its contract, as
// docs/read-contracts/go.md states: Watch…Collection, Watch…State, Watch…KeyedState,
// Subscribe…EventFeed, Subscribe…SampleFeed. Each ends when ctx ends.
//
// WatchCollectionByID runs the same protocol as WatchCollection, over one id: snapshot,
// one Bookmark, then changes. Absence is never an error — an id holding nothing gets the
// Bookmark alone, which is what lets a view open a subscription on an id it expects to
// be filled. A removal is Deleted; it does not end the stream, but it is the last frame
// that id will carry, because an id is never reused.
type Service interface {
	// Start launches the background work and returns the func that drains it. ctx
	// bounds startup; the stop func takes a drain deadline. Call stop before Close.
	Start(ctx context.Context) (func(context.Context) error, error)
	// Close releases the boundary's resources. Call after the stop func returns.
	Close() error
	// SetCacheFiles hands the passes the log files a cache's clear and removal reach.
	// The app calls it once the logs service exists and before Start, so no pass meets
	// it unset; one that did would fail rather than leave a file behind.
	SetCacheFiles(CacheFileOwner)

	Clusters() Clusters
	Caches() Caches
	CachedKinds() CachedKinds
	CachedData() CachedData

	// AcquireConnection claims id's connection and arms its probe cadence. It does not
	// dial — Lease.Conn waits — so a caller may hold a claim across a cluster being
	// down without a retry loop of its own. Release the claim.
	//
	// Where the record gates a dial: an id naming nothing, a disabled cluster, and one
	// awaiting deletion are refused here rather than handed a connection.
	AcquireConnection(ctx context.Context, id ClusterID) (Lease, error)
	// RetryConnection re-probes now and returns when that probe has finished, so it
	// blocks for the probe's round trip. The outcome lands on the record's conditions
	// and reaches watchers through Clusters().WatchCollectionByID, not here.
	RetryConnection(ctx context.Context, id ClusterID) error

	// The event timeline of any record that has one — ClusterCache and
	// ClusterCachedKind here, by object id; a Cluster's is on Clusters(), since a
	// cluster is addressed by ClusterID. Top-level, not per family: an event carries no
	// kind of its own, so one reader serves every timeline and the id is the whole key.
	// Both are off the collection watches, so event chatter never re-emits a record.
	//
	// ListEvents returns newest run first, optionally filtered by category and
	// bounded by limit. SubscribeObjectEventFeed streams the same log — snapshot, one
	// Bookmark, then growth. Distinct from CachedData().WatchEventCollection, which streams the *cluster's*
	// cached Kubernetes Events rather than kubetail's own log.
	ListEvents(ctx context.Context, id ObjectID, category *string, limit *int) ([]Event, error)
	SubscribeObjectEventFeed(ctx context.Context, id ObjectID, category *string) (*Stream[EventFeedFrame], error)
}

// The connection vocabulary is the leaf's, aliased rather than copied: a connection is native
// vocabulary all the way down, so there is nothing here to translate. Aliases and not plain
// return types because an internal package's type cannot be named by the packages implementing
// this interface — the resolver tests' fake among them. The Conn prefix belongs to this level,
// where a bare State or Identity would read as a record's.
type (
	Lease          = kubeconn.Lease
	Connection     = kubeconn.Connection
	ConnIdentity   = kubeconn.Identity
	ConnState      = kubeconn.State
	ConnStateWatch = kubeconn.StateWatch
)

// ErrIdentityMismatch is what Lease.ConnFor refuses a connection to another
// cluster with, matched with errors.Is.
var ErrIdentityMismatch = kubeconn.ErrIdentityMismatch

// The four families are specified together, apart from the kinds implementing them,
// because their rules are rules ACROSS the set: VerbNoun with the noun elided when it
// equals the family's subject, one method per scope rather than a scope argument, and
// By* naming the scope the caller passes. A violation is visible when the four read
// side by side and invisible when they don't.
//
// **A read reports the store as it is, and never filters.** A record awaiting deletion
// is served like any other, carrying the tombstone the consumer decides what to do
// with — a UI that renders it "Deleting…" is as valid as one that hides it, and only
// the consumer knows which. Deleted therefore means what beehive means by it: the row
// is gone. Filtering here instead cost an invariant every read, every watch, and every
// mutation had to maintain in agreement, and each place that forgot was a bug the
// types could not catch.

// Clusters is the Cluster record surface: the tracked clusters, their spec
// toggles, and the per-cluster streams that deliberately do not ride the record
// watch.
type Clusters interface {
	// Get returns one cluster by id, or (nil, nil) when the id names nothing.
	Get(ctx context.Context, id ClusterID) (*Cluster, error)
	// List returns every tracked cluster. Cache sync status is the caller's join from
	// Caches().WatchCollection.
	List(ctx context.Context) ([]*Cluster, error)
	// ReadActive hands read the record and the cache that mirrors its identity, then
	// reads the identity again, and runs the whole reading once more when it moved:
	// everything read reads one identity. read may run twice, so it sets what it
	// returns and never appends to it. A record that is gone is ErrNotFound; an
	// identity that moves under both attempts is ErrIdentityMoved; read's own error
	// is returned as it is.
	ReadActive(ctx context.Context, id ClusterID, read func(context.Context, ActiveCluster) error) error

	// WatchCollectionByID streams one cluster as a collection watch, scoped to the record's
	// id. Bookmark-only while the id names no tracked cluster.
	//
	// Deleted is terminal for the id, since an id is never reused: a context still in
	// the kubeconfig is re-imported under a fresh one, which a caller has to watch
	// itself. A context merely dropped from the kubeconfig is not a deletion at all —
	// the record is orphaned in place (IsPresent=false), which arrives as Modified.
	WatchCollectionByID(ctx context.Context, id ClusterID) (*Stream[ClusterCollectionWatchFrame], error)
	// WatchCollection streams every cluster as a collection watch. The mark that starts a
	// record's deletion is an ordinary Modified — the row is still there, wearing a tombstone —
	// and Deleted follows when it is collected.
	WatchCollection(ctx context.Context) (*Stream[ClusterCollectionWatchFrame], error)

	// WatchScheduleState streams a cluster's next probe as a state watch. A stream of its
	// own because a scheduling change fires no object WatchList, so it cannot ride the
	// collection watch.
	// Refuses the records AcquireConnection refuses: an unprobed cluster has no
	// cadence.
	WatchScheduleState(ctx context.Context, id ClusterID) (<-chan Schedule, error)
	// SubscribeDataRateSampleFeed streams the rate of a cluster's connection: the pool's
	// last samples, then each new one. Refuses what WatchScheduleState refuses.
	SubscribeDataRateSampleFeed(ctx context.Context, id ClusterID) (<-chan ClusterDataRateSampleFeedFrame, error)
	// SetEnabled enables or disables a cluster and returns the updated record.
	SetEnabled(ctx context.Context, id ClusterID, enabled bool) (*Cluster, error)
	// SetSyncEnabled toggles a cluster's sync and returns the updated record.
	SetSyncEnabled(ctx context.Context, id ClusterID, enabled bool) (*Cluster, error)
	// SetMonitoringEnabled starts or stops the monitor watching the cluster for
	// failures and returns the updated record. It gates detection, which calls no model.
	SetMonitoringEnabled(ctx context.Context, id ClusterID, enabled bool) (*Cluster, error)
	// SetDefault makes a cluster the user's default, clearing the one before, or stops
	// it being the default, and returns the updated record. A call that changes nothing
	// writes nothing. Making a cluster the default is refused (ErrCannotBeDefault) unless
	// it is enabled and its kubeconfig declares it.
	SetDefault(ctx context.Context, id ClusterID, isDefault bool) (*Cluster, error)

	// ListEvents and SubscribeEventFeed are the cluster's own timeline, the top-level pair
	// resolved by ClusterID. Absence is empty, or the Bookmark alone.
	ListEvents(ctx context.Context, id ClusterID, category *string, limit *int) ([]Event, error)
	SubscribeEventFeed(ctx context.Context, id ClusterID, category *string) (*Stream[EventFeedFrame], error)

	// Delete marks the row for deletion and returns; the mirror and the chat service
	// tear the cluster down behind it, and the row goes last.
	Delete(ctx context.Context, id ClusterID) error
}

// Caches is the ClusterCache record surface. A cluster owns zero or one cache at
// steady state, but a UID migration leaves the old one behind — so every
// per-cache read names the exact cache it means, never "the" cache of a cluster.
type Caches interface {
	// Get returns one cache by id, or (nil, nil) when the id names nothing.
	Get(ctx context.Context, id ClusterCacheID) (*ClusterCache, error)

	// WatchCollectionByID streams one cache as a collection watch. Bookmark-only until the
	// cluster has been probed and its cache created, so a caller may open this on a cache a
	// migration has not produced yet.
	WatchCollectionByID(ctx context.Context, id ClusterCacheID) (*Stream[ClusterCacheCollectionWatchFrame], error)
	// WatchCollection streams every cache as a collection watch parallel to
	// Clusters().WatchCollection; the caller joins caches onto clusters by ClusterID.
	WatchCollection(ctx context.Context) (*Stream[ClusterCacheCollectionWatchFrame], error)

	// ListByCluster returns one cluster's caches in creation order. Which one is
	// active is the caller's live join (CacheIsActive), not a property here.
	ListByCluster(ctx context.Context, clusterID ClusterID) ([]*ClusterCache, error)
	// WatchCollectionByCluster streams one cluster's caches as a collection watch — what a
	// view scoped to a single cluster opens instead of filtering WatchCollection.
	WatchCollectionByCluster(ctx context.Context, clusterID ClusterID) (*Stream[ClusterCacheCollectionWatchFrame], error)

	// WatchStatsState streams one cache's contents as a state watch. A stream, not a
	// ClusterCache field: a settled cache's object never changes, so a field would
	// freeze at subscribe time.
	WatchStatsState(ctx context.Context, clusterID ClusterID, cacheID ClusterCacheID) (*Stream[ClusterCacheStats], error)
	// WatchHealthKeyedState streams every cache's sync verdict, folded from its per-kind
	// records. Unscoped where the rest of this family is per-cache: one fold serves
	// the fleet. A keyed state watch, and a failable one — the fold reads watches of its own.
	WatchHealthKeyedState(ctx context.Context) (*Stream[ClusterCacheHealth], error)
	// WatchSyncState streams one cache's sync detail — the discovery verdict and a row
	// per mirrored kind. What WatchHealthKeyedState folds for a fleet, expanded for one cache.
	WatchSyncState(ctx context.Context, clusterID ClusterID, cacheID ClusterCacheID) (*Stream[ClusterCacheSyncState], error)
	// Health and SyncState are one reading each of what WatchHealthKeyedState and
	// WatchSyncState stream, for a caller that reads once rather than watching. ok is false for a pair that names
	// no cache of that cluster.
	Health(ctx context.Context, clusterID ClusterID, cacheID ClusterCacheID) (ClusterCacheHealth, bool, error)
	SyncState(ctx context.Context, clusterID ClusterID, cacheID ClusterCacheID) (ClusterCacheSyncState, bool, error)

	// Clear deletes one cache's on-disk file and restarts its syncs; the record stays.
	Clear(ctx context.Context, id ClusterCacheID) (*ClusterCache, error)
}

// CachedKinds is the ClusterCachedKind surface — one record per kind a
// cache mirrors. Distinct from CachedData, which serves the mirrored content itself;
// these are the control-plane records describing what is mirrored.
//
// This family is the fleet's largest by an order of magnitude: a record per served
// kind per cache, so hundreds per cluster. Scope every read that can be scoped.
type CachedKinds interface {
	// WatchCollectionByID streams one per-kind record as a collection watch. Bookmark-only
	// until the kind is discovered; a kind the cluster stops serving is Deleted.
	WatchCollectionByID(ctx context.Context, id ClusterCachedKindID) (*Stream[ClusterCachedKindCollectionWatchFrame], error)
	// WatchCollection streams every per-kind record across every cache — the fleet's
	// widest stream, and one a view scoped to a cache wants WatchCollectionByCache for
	// instead. For a reader that genuinely spans caches: the sync-health rollup.
	WatchCollection(ctx context.Context) (*Stream[ClusterCachedKindCollectionWatchFrame], error)

	// ListByCache returns one cache's per-kind records — the owner edge, so a cache
	// nothing has discovered kinds for yet reads empty rather than as an error.
	ListByCache(ctx context.Context, cacheID ClusterCacheID) ([]*ClusterCachedKind, error)
	// WatchCollectionByCache streams one cache's per-kind records as a collection watch — what
	// a view scoped to a single cache opens instead of filtering WatchCollection.
	WatchCollectionByCache(ctx context.Context, cacheID ClusterCacheID) (*Stream[ClusterCachedKindCollectionWatchFrame], error)

	// Clear drops one kind's cached objects and restarts its sync from an empty
	// mirror; the record stays and resyncs. Caches().Clear is the whole-cache form.
	Clear(ctx context.Context, id ClusterCachedKindID) (*ClusterCachedKind, error)

	// SetSyncEnabled stops or resumes one kind's sync and returns the updated record.
	// Pausing KEEPS the cached rows — readable throughout, and reconciled into rather
	// than rebuilt on the resume. Clear is the form that throws them away.
	SetSyncEnabled(ctx context.Context, id ClusterCachedKindID, syncEnabled bool) (*ClusterCachedKind, error)
}

// CachedData is the cached Kubernetes content in one cache's db — the only family whose
// reads leave beehive entirely.
//
// **A read binds to what is open and never creates a file**, so a cache with nothing
// synced yet answers the Bookmark alone and goes live when a writer opens one. A cache
// that goes away under a live watch ends it CLEANLY — Err is nil, and the client
// reconnects into the fresh snapshot — because a clear is a user pressing a button, not a
// watch breaking.
type CachedData interface {
	// ListKinds returns one cache's discovered kind catalog, counted as WatchKindCollection counts it.
	ListKinds(ctx context.Context, clusterID ClusterID, cacheID ClusterCacheID, namespace string) ([]ClusterCachedDataKind, error)
	// WatchKindCollection streams one cache's kind catalog as a collection watch (per-kind
	// counts update live). A namespace counts each namespaced kind in it alone; "" counts the
	// whole cache.
	WatchKindCollection(ctx context.Context, clusterID ClusterID, cacheID ClusterCacheID, namespace string) (*Stream[ClusterCachedDataKindCollectionWatchFrame], error)
	// ListObjects returns one kind's cached objects as WatchObjectCollection would snapshot
	// them, with no body loaded: identity alone, for a reader that wants names.
	// ok is false for a cache the pair does not resolve to, or one with no file —
	// never synced, or gone under the read — so a count is never read off nothing.
	ListObjects(ctx context.Context, clusterID ClusterID, cacheID ClusterCacheID, apiVersion, resource string) ([]ClusterCachedDataObject, bool, error)
	// WatchObjectCollection streams one kind's cached objects as a collection watch keyed by
	// UID. No frames while that kind hasn't synced.
	WatchObjectCollection(ctx context.Context, clusterID ClusterID, cacheID ClusterCacheID, apiVersion, resource string) (*Stream[ClusterCachedDataObjectCollectionWatchFrame], error)
	// WatchObjectCollectionByNamespaces is WatchObjectCollection narrowed to the objects in
	// namespaces, each frame carrying no body: a watcher that wants to know what moved, not what it
	// holds, pays for neither the rest of the cluster nor the bodies.
	WatchObjectCollectionByNamespaces(ctx context.Context, clusterID ClusterID, cacheID ClusterCacheID, apiVersion, resource string, namespaces []string) (*Stream[ClusterCachedDataObjectCollectionWatchFrame], error)
	// WatchEventCollection streams one cache's cached Kubernetes Events, newest first, as a
	// collection watch keyed by event UID. Woken separately from WatchKindCollection, so an event
	// burst never drives the kind-catalog re-read.
	WatchEventCollection(ctx context.Context, clusterID ClusterID, cacheID ClusterCacheID) (*Stream[ClusterCachedDataEventCollectionWatchFrame], error)
	// Query runs one statement over the cache's views on a read-only connection of its
	// own, with args bound to its placeholders, and answers at most maxRows rows and
	// maxBytes of cells. ok is false for a
	// cache the pair does not resolve to, or one with no file. A statement SQLite
	// refuses is a *QueryError.
	Query(ctx context.Context, clusterID ClusterID, cacheID ClusterCacheID, sql string, maxRows, maxBytes int, args ...any) (ClusterCachedDataQueryResult, bool, error)
	// ListedKinds is the kinds whose list has committed, in this session or an earlier
	// one: a kind's cookie is on disk from its list's commit until a relist's first
	// page clears it. ok is false for a cache the pair does not resolve to, or one
	// with no file.
	ListedKinds(ctx context.Context, clusterID ClusterID, cacheID ClusterCacheID) (map[KindRef]bool, bool, error)
	// ReadOwn runs read over one snapshot of the cache, each statement it runs one the
	// sidecar wrote, on a connection kept between such readings: a reader joining
	// what several statements answer never joins two states of the cache. ok is false
	// for a cache the pair does not resolve to, or one with no file; a statement in a
	// cache gone under the reading answers ErrNoCache. A statement a model wrote goes
	// through Query.
	ReadOwn(ctx context.Context, clusterID ClusterID, cacheID ClusterCacheID, read func(OwnQuery) error) (bool, error)
	// ReadActive runs read over one reading of cluster id's active cache, whose rows
	// must be trusted, its statements bounded by maxRows and maxBytes: ErrNoCache for
	// a cluster with no cache, ErrSyncing for one whose rows are not. Every statement
	// read runs reads that one cache. read may run twice, as Clusters().ReadActive's
	// does, so it sets what it returns and never appends to it.
	ReadActive(ctx context.Context, id ClusterID, maxRows, maxBytes int, read func(CacheReading, ActiveCluster) error) error
}

// The family accessors are stateless views onto the one *service: the split is
// about the shape of the API, not a split of the control plane behind it.
type (
	clustersAPI    struct{ s *service }
	cachesAPI      struct{ s *service }
	cachedKindsAPI struct{ s *service }
	cachedDataAPI  struct{ s *service }
)

func (s *service) Clusters() Clusters { return clustersAPI{s} }

func (s *service) Caches() Caches { return cachesAPI{s} }

func (s *service) CachedKinds() CachedKinds { return cachedKindsAPI{s} }

func (s *service) CachedData() CachedData { return cachedDataAPI{s} }

// Each family is asserted separately: satisfying Service only proves the accessors
// exist, not that any family is fully implemented.
var (
	_ Service     = (*service)(nil)
	_ Clusters    = clustersAPI{}
	_ Caches      = cachesAPI{}
	_ CachedKinds = cachedKindsAPI{}
	_ CachedData  = cachedDataAPI{}
)

// deps is what everything in this package draws from: one beehive client per kind, and
// the process-wide services. Built once in New and embedded by each owner, so a family
// reads through its own kind's client and a controller writing a kind it owns takes
// that kind's from here. A new kind or a new shared service is a field here, never
// another constructor parameter.
type deps struct {
	// The app's database and this package's statements over it: the clusters table.
	db    *appdb.DB
	store *sqlstmt.Set[stmtID]

	clusterClient beehive.Client[ClusterRuntimeSpec, ClusterStatus]
	cacheClient   beehive.Client[ClusterCacheSpec, ClusterCacheStatus]
	kindClient    beehive.Client[ClusterCachedKindSpec, ClusterCachedKindStatus]
	sourceClient  beehive.Client[ClusterSourceSpec, ClusterSourceStatus]

	kubeconfigSvc kubeconfigService
	kubeconnSvc   kubeconnService
	kubestoreMgr  kubestoreManager
	kubesyncSvc   kubesyncService
	pokeSvc       *poke.Service
	// cacheFiles is the log store as the passes reach it, set by SetCacheFiles.
	cacheFiles *cacheFiles

	// kindSpecMu serializes the read-modify-write on a ClusterCachedKind spec, which two
	// writers share: the sync-enabled setter, and the sweep converging a catalog change
	// onto a record whose pause it has to carry forward. beehive's Update takes the whole
	// spec and offers no compare-and-swap, so without this the later write restores what
	// the earlier one changed. A pointer because deps is copied by value.
	kindSpecMu *sync.Mutex
}

func newDeps(bh *beehive.Beehive, db *appdb.DB, store *sqlstmt.Set[stmtID], kubeconfigSvc kubeconfigService, kubeconnSvc kubeconnService, kubestoreMgr kubestoreManager, kubesyncSvc kubesyncService, pokeSvc *poke.Service) deps {
	return deps{
		db:            db,
		store:         store,
		clusterClient: beehive.NewClient[ClusterRuntimeSpec, ClusterStatus](bh, ClusterGroupKind),
		cacheClient:   beehive.NewClient[ClusterCacheSpec, ClusterCacheStatus](bh, ClusterCacheGroupKind),
		kindClient:    beehive.NewClient[ClusterCachedKindSpec, ClusterCachedKindStatus](bh, ClusterCachedKindGroupKind),
		sourceClient:  beehive.NewClient[ClusterSourceSpec, ClusterSourceStatus](bh, ClusterSourceGroupKind),
		kubeconfigSvc: kubeconfigSvc,
		kubeconnSvc:   kubeconnSvc,
		kubestoreMgr:  kubestoreMgr,
		kubesyncSvc:   kubesyncSvc,
		pokeSvc:       pokeSvc,
		cacheFiles:    &cacheFiles{},
		kindSpecMu:    &sync.Mutex{},
	}
}

// service is the concrete Service: the shared deps, plus what the boundary owns on
// top of them.
type service struct {
	deps
	// now stamps the rows this package writes; a test pins it.
	now func() time.Time

	// The server cache a reconcile reads, beehive, the anchors it must be up to create,
	// the controllers in registration order, then the triggers. Order is the whole
	// contract: beehive starts before anything that reaches the store and, being last
	// to stop and close among them, outlives every reconcile and every poke that could
	// still touch it.
	parts []lifecycle.Part
	// stateWatchCadence re-measures the state watches. They carry numbers that move while
	// their record settles — a file's size, a freshness stamp — so a state watch that only
	// woke on a change notification would go quiet exactly when it is healthy. A parameter so a test
	// picks its own timescale.
	stateWatchCadence time.Duration
	// health is the one read loop every WatchHealthKeyedState subscriber shares.
	health *sharedStateWatch[[]ClusterCacheHealth]
}

// beehiveRuntime gives beehive the lifecycle.StartCloser shape. Start already matches; the
// only thing missing is Close, because what closes is the store rather than the
// runtime on top of it.
type beehiveRuntime struct {
	bh    *beehive.Beehive
	store beehive.Store
}

func (r beehiveRuntime) Start(ctx context.Context) (func(context.Context) error, error) {
	return r.bh.Start(ctx)
}

func (r beehiveRuntime) Close() error { return r.store.Close() }

// maxEventRuns bounds retention per (object, category) timeline — counted in
// aggregated RUNS (state transitions), not occurrences. Global (set at beehive.New).
const maxEventRuns = 20

// Paths is where the service keeps its files. BeehiveDBFile is the runtime store,
// kept with what a user would lose: it names each mirror file by cache id, so
// it outlives a cleared cache and the files come back under the same names.
// KubestoreDir holds the mirror, one file per cache, which Kubetail rebuilds.
type Paths struct {
	BeehiveDBFile, KubestoreDir string
}

func (s *service) SetCacheFiles(owner CacheFileOwner) { s.cacheFiles.set(owner) }

// Option tunes New.
type Option func(*options)

type options struct {
	faults string
}

// WithFaults is KUBETAIL_FAULTS, the request failures a debug sidecar injects into every
// connection (kubeconn.ParseFaults). New refuses a spec it cannot read.
func WithFaults(spec string) Option {
	return func(o *options) { o.faults = spec }
}

// New builds the cluster boundary over the app's database and its files at
// paths: it prepares its statements, opens the runtime store, registers the
// controllers, and leaves everything stopped until Start. The log files a cache's clear
// and removal reach arrive by SetCacheFiles, once the logs service exists.
func New(db *appdb.DB, paths Paths, kubeconfigSvc kubeconfigService, pokeSvc *poke.Service, opts ...Option) (Service, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	faults, err := kubeconn.ParseFaults(o.faults)
	if err != nil {
		return nil, fmt.Errorf("KUBETAIL_FAULTS: %w", err)
	}
	store, err := sqlstmt.Prepare[stmtID](context.Background(), db.Write.DB, db.Read.DB, statements)
	if err != nil {
		return nil, fmt.Errorf("prepare cluster statements: %w", err)
	}
	bhStore, err := beehivesqlite.Open(paths.BeehiveDBFile)
	if err != nil {
		store.Close()
		// This text reaches the user: a newer file names itself and both versions.
		if newer, ok := errors.AsType[*beehivesqlite.NewerSchemaError](err); ok {
			return nil, fmt.Errorf("beehive.db was written by a newer Kubetail (schema %d; this build reads up to %d)", newer.Recorded, newer.Supported)
		}
		return nil, fmt.Errorf("open beehive store: %w", err)
	}
	// Everything from here on has both to release on failure.
	fail := func(err error) (Service, error) { // coverage:ignore only the two failures below call it, and neither happens
		bhStore.Close()
		store.Close()
		return nil, err
	}
	// WithEventRetention bounds each (object, category) timeline to maxEventRuns runs.
	// The pass cadences are declared per kind at registration; see startupPass.
	bh, err := beehive.New(bhStore, beehive.WithEventRetention(maxEventRuns, 0))
	if err != nil { // coverage:ignore beehive.New fails only on an option's error, and this option returns none
		return fail(fmt.Errorf("init beehive: %w", err))
	}

	// The one that names credentials: everything above it asks about a kube-context or a
	// cluster, and this is what turns one into the credentials a probe dials.
	kubeconnSvc := kubeconn.New(kubeconfigSvc, kubeconn.WithFaults(faults))
	// One store per cache under the registry, which the boundary reads, clears, and
	// removes.
	kubestoreMgr := kubestore.NewManager(paths.KubestoreDir, kubestore.DefaultRetention)
	// What fills them: it discovers what each cluster serves and mirrors every served kind
	// into that cluster's file. Armed by the cache and kind passes, never by a reader.
	kubesyncSvc := kubesync.New(kubeconnSvc, syncStores(kubestoreMgr, faults))
	d := newDeps(bh, db, store, kubeconfigSvc, kubeconnSvc, kubestoreMgr, kubesyncSvc, pokeSvc)

	controllers, err := registerControllers(bh, d)
	if err != nil { // coverage:ignore a fresh beehive takes each kind's first registration
		return fail(fmt.Errorf("register cluster controllers: %w", err))
	}

	parts := []lifecycle.Part{
		// First, so the reverse close order releases the statements after every part
		// that issues one.
		{Name: "cluster statements", StartCloser: lifecycle.CloseFunc(store.Close)},
		// Ahead of beehive: closing drops sockets and open files, and both have to
		// outlive every pass that could still be reaching for one. Stop and close
		// reverse the slice.
		{Name: "kubeconn", StartCloser: kubeconnSvc},
		{Name: "kubestore", StartCloser: kubestoreMgr},
		// Between the store and beehive: no pass can arm a cache that is stopping, and no
		// worker outlives the file it writes into.
		{Name: "kubesync", StartCloser: kubesyncSvc},
		{Name: "beehive", StartCloser: beehiveRuntime{bh: bh, store: bhStore}},
		clusterSourceBootstrap(d),
	}
	parts = append(parts, controllers...)
	// After the controllers: the objects it creates carry finalizers, which beehive
	// accepts only for a kind with a controller registered.
	parts = append(parts, lifecycle.Part{Name: "cluster mirror", StartCloser: newClusterMirror(d, mirrorResyncInterval, mirrorRewatchDelay, mirrorPassRetry)})
	parts = append(parts, restartSyncsOnResume(kubesyncSvc, pokeSvc))

	s := &service{deps: d, now: time.Now, parts: parts, stateWatchCadence: defaultStateWatchCadence}
	s.health = newSharedStateWatch(cachesAPI{s}.readAllCacheHealth)
	return s, nil
}

// clusterSourceBootstrap creates the discovery anchors once beehive is up. A Part
// rather than a step inside Start so a failure unwinds through StartAll and reports
// under a name, like every other participant; it has no background work to stop.
func clusterSourceBootstrap(d deps) lifecycle.Part {
	return lifecycle.Part{
		Name: "cluster source records",
		StartCloser: lifecycle.StartFunc(func(ctx context.Context) (func(context.Context) error, error) {
			return func(context.Context) error { return nil }, ensureClusterSources(ctx, d.sourceClient)
		}),
	}
}

// restartSyncsOnResume restarts every sync on a resume. A watch that died under a sleeping
// machine reports nothing, so nothing else would notice; a nil poke service — which a test
// builds against — subscribes to nothing and has nothing to stop.
func restartSyncsOnResume(kubesyncSvc kubesyncService, pokeSvc *poke.Service) lifecycle.Part {
	return lifecycle.Part{
		Name: "kubesync resume poke",
		StartCloser: lifecycle.StartFunc(func(context.Context) (func(context.Context) error, error) {
			if pokeSvc == nil {
				return func(context.Context) error { return nil }, nil
			}
			signals, unsubscribe := pokeSvc.ListenForNotifications()
			var wg sync.WaitGroup
			wg.Go(func() {
				for range signals {
					kubesyncSvc.RestartAll()
				}
			})
			return func(ctx context.Context) error {
				// Unsubscribing closes the channel, which is what ends the loop above.
				unsubscribe()
				return drain.WithContext(ctx, wg.Wait)
			}, nil
		}),
	}
}

// startupPass reconciles every object of a kind once per process. Each owns state a
// restart invalidates and the store cannot report as owed: a live connection, an
// in-memory schedule. The store reads settled, because the generation was observed by a
// process that is gone.
var startupPass = beehive.WithStartupFullPass(true)

// sourceResync re-runs the discovery pass, each anchor timed from the end of its own
// last pass. Only ClusterSource takes one: it is the kind whose correctness rests on a
// poll, since what it reads is a file the store cannot see and a lost trigger poke is
// a change nothing else would report. Every other kind is woken by a spec write or a
// dependency edge, which is what a pass here would be re-deriving.
var sourceResync = beehive.WithIndividualPassInterval(clusterSourceResyncInterval)

// clusterResync re-probes each cluster, timed from the end of its own last pass. The
// second kind whose correctness rests on a poll: what it reports is a remote server's,
// so nothing in the store moves when the answer does. Per object rather than a sweep of
// the kind, which is what keeps a fleet from dialing in one burst.
var clusterResync = beehive.WithIndividualPassInterval(clusterProbeInterval)

// registerControllers builds and registers each kind's controller, which lives in that
// kind's file, and returns them in registration order. Together here rather than four
// calls spread across those files: the options are the whole subsystem's concurrency
// and retry budget, and it only reads as a budget in one place.
func registerControllers(bh *beehive.Beehive, d deps) ([]lifecycle.Part, error) {
	// Built here because a trigger is a registration option like any other, and its feed
	// is what makes the option mean anything. Each returns below as a Part after the
	// controllers, so nothing pokes a kind before there is something to poke.
	kubeconfigTrigger := newKubeconfigTrigger(d.kubeconfigSvc)
	cluster := &clusterController{deps: d}
	// A probe names its context, and the record it wakes is whichever holds a claim
	// on it: the controller's own index, since a claim is what arms the probe.
	kubeconnTrigger := newKubeconnTrigger(d.kubeconnSvc, cluster.leases.clusterFor)
	// One per registration, because a trigger wakes a record for every value its feed carries:
	// one feed carrying both would wake a cache for each of its hundreds of kinds.
	syncCacheTrigger := newKubesyncDiscoveryTrigger(d.kubesyncSvc)
	syncKindTrigger := newKubesyncKindTrigger(d.kubesyncSvc)
	// A second feed onto the cache kind: the store speaks of files and the sync of
	// discovery, and one channel carrying both would be a translation neither owns.
	sizeLimitTrigger := newKubestoreSizeLimitTrigger(d.kubestoreMgr)

	source := &clusterSourceController{deps: d}
	cache := &clusterCacheController{deps: d}
	resource := &clusterCachedKindController{deps: d}

	errSource := beehive.Register(bh, ClusterSourceGroupKind, source, startupPass, sourceResync, beehive.WithTriggerByName(kubeconfigTrigger.Wakes()))
	errCluster := beehive.Register(bh, ClusterGroupKind, cluster, startupPass, clusterResync, beehive.WithTriggerByName(kubeconnTrigger.Wakes()))
	errCache := beehive.Register(bh, ClusterCacheGroupKind, cache, startupPass,
		beehive.WithTriggerByID(syncCacheTrigger.Wakes()), beehive.WithTriggerByID(sizeLimitTrigger.Wakes()))
	errResource := beehive.Register(bh, ClusterCachedKindGroupKind, resource, startupPass, beehive.WithTriggerByName(syncKindTrigger.Wakes()))
	if err := errors.Join(errSource, errCluster, errCache, errResource); err != nil {
		return nil, err
	}
	return []lifecycle.Part{
		{Name: "cluster source controller", StartCloser: source},
		{Name: "cluster controller", StartCloser: cluster},
		{Name: "cache controller", StartCloser: cache},
		{Name: "cached-resource controller", StartCloser: resource},
		{Name: "kubestore size-limit trigger", StartCloser: lifecycle.StartFunc(sizeLimitTrigger.Start)},
		{Name: "kubeconfig trigger", StartCloser: lifecycle.StartFunc(kubeconfigTrigger.Start)},
		{Name: "kubeconn trigger", StartCloser: lifecycle.StartFunc(kubeconnTrigger.Start)},
		{Name: "kubesync cache trigger", StartCloser: lifecycle.StartFunc(syncCacheTrigger.Start)},
		{Name: "kubesync kind trigger", StartCloser: lifecycle.StartFunc(syncKindTrigger.Start)},
	}, nil
}

// Start launches beehive (the controller harness + store subscription loop), then
// each controller's own background work. ctx bounds startup only; the returned stop
// func takes a drain deadline and blocks until the background work finishes. Call
// stop before Close.
func (s *service) Start(ctx context.Context) (func(context.Context) error, error) {
	return lifecycle.StartAll(ctx, s.parts)
}

// Close releases the controllers' resources and the beehive store. Call after the
// stop func returns.
func (s *service) Close() error {
	return lifecycle.CloseAll(s.parts)
}

// AcquireConnection claims the pool's connection for id's context. The claim is the
// caller's own, refcounted alongside the one clusterController holds, so releasing it
// never stops the cluster being probed.
func (s *service) AcquireConnection(ctx context.Context, id ClusterID) (Lease, error) {
	contextName, err := s.connectableContext(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.kubeconnSvc.Acquire(contextName), nil
}

// RetryConnection re-probes id's context and returns when that probe has finished, so a
// caller showing the retry as busy shows it for exactly as long as the probe took. What
// the probe found lands on the record's conditions, which is where a caller reads it.
//
// The pool claims the context for the wait, so a cluster nothing else holds is probed too.
// A probe already mid-run does not answer for this one: the wait is for the run the ask
// bought, which is the next one to begin.
func (s *service) RetryConnection(ctx context.Context, id ClusterID) error {
	contextName, err := s.connectableContext(ctx, id)
	if err != nil {
		return err
	}
	return s.kubeconnSvc.RetryAndWait(ctx, contextName)
}

// connectableContext reads id's row and the kube-context behind it, or why the record
// will not be connected. Shared by both methods above so no caller finds one of them
// willing to act on a record the other refuses. The row, not the runtime object: the
// mark lands on the row first, and the object follows on the mirror's next pass.
//
// The three refusals are the record's own state, never the cluster's: whether the
// server answers is the probe's to find out and report, so an unreachable cluster is
// claimed and retried like any other.
func (s *service) connectableContext(ctx context.Context, id ClusterID) (string, error) {
	row, ok, err := getCluster(ctx, s.store.Stmts(), id)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("get cluster %s: %w", id, ErrNotFound)
	}
	switch {
	case row.DeleteRequestedAt != nil:
		return "", fmt.Errorf("cluster %s is being deleted: %w", id, ErrNotConnectable)
	case !row.Enabled:
		return "", fmt.Errorf("cluster %s is disabled: %w", id, ErrNotConnectable)
	case row.Source != SourceKubeconfig || row.SourceKey == nil:
		// Another source's credentials are not this package's to resolve, the same rule
		// the cluster pass follows when it declines to claim one.
		return "", fmt.Errorf("cluster %s has no kubeconfig credentials: %w", id, ErrNotConnectable)
	}
	return *row.SourceKey, nil
}

// The one resolution more than one family needs. A helper only one family uses is that
// family's own, on its *API type in its kind's file; this is here because a caller in
// another file would otherwise reach across kinds for it.

// cacheBelongsTo reports whether cacheID names a live cache owned by clusterID — the
// gate every per-cache read shares. An absent or mismatched pair is not an error: a
// caller holds ids from watch frames, so a record collected in between is a race.
func (s *service) cacheBelongsTo(ctx context.Context, clusterID ClusterID, cacheID ClusterCacheID) (bool, error) {
	obj, err := s.cacheClient.Get(ctx, beehive.ObjectID(cacheID))
	if errors.Is(err, beehive.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get cluster cache %d: %w", cacheID, err)
	}
	return obj.Spec.ClusterID == clusterID, nil
}
