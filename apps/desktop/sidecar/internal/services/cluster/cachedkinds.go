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

// The ClusterCachedKind kind: one record per kind a cache mirrors. Its beehive
// shapes, the record served to resolvers, its collection watch frame, the CachedKinds
// implementation, and its controller. Mirrors the ClusterCachedKind section of
// graph/schema.graphqls.
package cluster

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/amorey/beehive"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/lifecycle"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubesync"
)

// ClusterCachedKindGroupKind identifies the per-GVR sync kind: one object per
// served GVR, owned by its ClusterCache.
var ClusterCachedKindGroupKind = beehive.GroupKind{Kind: "ClusterCachedKind"}

// ClusterCachedKindName returns "cachedkind/{cacheObjID}/{apiVersion}/{resource}" —
// deterministic, so a discovery pass is a set reconcile with no per-child
// bookkeeping, and derivable from the cache id alone, so anything holding a cache and
// a kind can name the record. (apiVersion, resource) rather than Kind: the
// plural is what the worker's REST path needs and what the server guarantees unique
// per group-version.
func ClusterCachedKindName(cacheID beehive.ObjectID, apiVersion, resource string) string {
	return cachedKindNamePrefix + strconv.FormatInt(int64(cacheID), 10) + "/" + apiVersion + "/" + resource
}

const cachedKindNamePrefix = "cachedkind/"

// cacheIDInKindName reads the cache back out of a record's name, for the one pass that has no
// owner edge left to read it from.
func cacheIDInKindName(name string) (int64, bool) {
	rest, ok := strings.CutPrefix(name, cachedKindNamePrefix)
	if !ok {
		return 0, false
	}
	digits, _, ok := strings.Cut(rest, "/")
	if !ok {
		return 0, false
	}
	cacheID, err := strconv.ParseInt(digits, 10, 64)
	return cacheID, err == nil
}

// EventsKind / EventsAPIVersion / EventsResource identify the Event collection — an
// ordinary synced kind, written to its own table. The server serves the same events
// under two spellings backed by one store, so exactly one may be synced, and this is
// the canonical spelling: whatever discovers kinds must drop the events.k8s.io one.
const (
	EventsKind       = "Event"
	EventsAPIVersion = "v1"
	EventsResource   = "events"
)

// ClusterCachedKindSpec is the desired sync for one GVR: the identity the discovery
// sweep owns, plus the one switch the user owns. The catalog fields refresh each sweep,
// so a kind that changes shape converges without recreation; nothing that writes on a
// schedule may touch Paused.
type ClusterCachedKindSpec struct {
	// APIVersion is the group/version this kind is served at, e.g. "apps/v1" — or a bare
	// version ("v1") for the core group, matching the wire form Kubernetes uses.
	APIVersion string `json:"apiVersion"`
	// Kind is the singular Kind name, e.g. "Deployment".
	Kind string `json:"kind"`
	// Resource is the lowercase plural URL segment, e.g. "deployments".
	Resource string `json:"resource"`
	// Namespaced is true when objects of this kind live in a namespace.
	Namespaced bool `json:"namespaced"`
	// Paused is the user's switch for this one kind. Inverted so the zero value is the
	// default: beehive decodes a spec with json.Unmarshal, and every stored record
	// predates this field — a positive Enabled would decode false for the whole fleet on
	// the upgrade that shipped it. The wire keeps the positive form (`syncEnabled`), so
	// the projection negates.
	Paused bool `json:"paused"`
}

// ClusterCachedKindStatus is the observed sync state for one GVR. Empty placeholder.
type ClusterCachedKindStatus struct{}

// ClusterCachedKind is the view of one ClusterCachedKind beehive object: one
// Kubernetes kind being mirrored into a cache. Shaped like the records above it —
// {ID, Owner, Spec, Conditions} — but streamed **cache-scoped**, because there is one per
// served kind rather than one per cache and an unscoped stream of a hundred-plus records
// would be a firehose.
type ClusterCachedKind struct {
	// ID is the ClusterCachedKindID; see RecordMeta for the rest. Conditions there are
	// empty in practice: a kind's verdict is a read-side state watch rather than a stored
	// condition, so nothing writes one. → docs/adr/2026-08-28-records-as-timeline-anchors.md.
	RecordMeta
	// Owner is the ClusterCache this kind is mirrored into — the join key a client
	// already holds from the cache stream.
	Owner ObjectRef
	Spec  ClusterCachedKindSpec
}

// ClusterCachedKindCollectionWatchFrame is one frame on the cache-scoped per-kind sync watch.
// Consumers key on Kind.ID.
type ClusterCachedKindCollectionWatchFrame struct {
	Type CollectionWatchFrameType
	Kind *ClusterCachedKind
}

// KindRef identifies one kind exactly, whatever its sync state. The plural alone is
// what a UI renders, but it does not IDENTIFY a kind — a CRD may reuse a built-in's
// plural under another api group — so anything that keys on a kind needs the pair.
type KindRef struct {
	APIVersion string
	Resource   string
}

// UnhealthyKindRef is one kind that is not Watching, with its own reason, so a consumer
// can tell the kinds behind a cache's verdict from those merely catching up.
type UnhealthyKindRef struct {
	KindRef
	Reason string
}

// toClusterCachedKind builds the served record from the stored object.
func toClusterCachedKind(obj *beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus]) (*ClusterCachedKind, error) {
	owner, err := toOwnerRef(obj)
	if err != nil {
		return nil, err
	}
	return &ClusterCachedKind{
		RecordMeta: toRecordMeta(obj),
		Owner:      owner,
		Spec:       obj.Spec,
	}, nil
}

// toClusterCachedKinds projects a whole read. beehive lists by id, which is creation
// order, and that is the order this family promises — so nothing here sorts.
func toClusterCachedKinds(objs []*beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus]) ([]*ClusterCachedKind, error) {
	kinds := make([]*ClusterCachedKind, 0, len(objs))
	for _, obj := range objs {
		kind, err := toClusterCachedKind(obj)
		if err != nil {
			return nil, err
		}
		kinds = append(kinds, kind)
	}
	return kinds, nil
}

func (a cachedKindsAPI) WatchCollectionByID(ctx context.Context, id ClusterCachedKindID) (*Stream[ClusterCachedKindCollectionWatchFrame], error) {
	src, err := a.s.kindClient.Watch(ctx, beehive.ObjectID(id), loadKindOwner)
	if err != nil {
		return nil, fmt.Errorf("watch cached kind %d: %w", id, err)
	}

	return kindWatch.streamOne(ctx, src), nil
}

// WatchCollection is the fleet's largest stream by an order of magnitude — a record per served
// kind per cache. Served because the boundary fills its matrix, but a view scoped to one
// cache opens WatchCollectionByCache instead; the schema exposes only the scoped one.
func (a cachedKindsAPI) WatchCollection(ctx context.Context) (*Stream[ClusterCachedKindCollectionWatchFrame], error) {
	src, err := a.s.kindClient.WatchList(ctx, loadKindOwner)
	if err != nil {
		return nil, fmt.Errorf("watch cached kinds: %w", err)
	}

	return kindWatch.streamList(ctx, src), nil
}

func (a cachedKindsAPI) ListByCache(ctx context.Context, cacheID ClusterCacheID) ([]*ClusterCachedKind, error) {
	objs, err := a.s.kindClient.ListOwnedObjects(ctx, beehive.ObjectID(cacheID), beehive.LoadOwner())
	if err != nil {
		return nil, fmt.Errorf("list cache %d cached kinds: %w", cacheID, err)
	}
	return toClusterCachedKinds(objs)
}

func (a cachedKindsAPI) WatchCollectionByCache(ctx context.Context, cacheID ClusterCacheID) (*Stream[ClusterCachedKindCollectionWatchFrame], error) {
	src, err := a.s.kindClient.WatchOwnedObjects(ctx, beehive.ObjectID(cacheID), loadKindOwner)
	if err != nil {
		return nil, fmt.Errorf("watch cache %d cached kinds: %w", cacheID, err)
	}
	return kindWatch.streamList(ctx, src), nil
}

// Clear drops one kind's rows from the cache holding them — the cache-wide clear,
// scoped to a kind.
func (a cachedKindsAPI) Clear(ctx context.Context, id ClusterCachedKindID) (*ClusterCachedKind, error) {
	obj, err := a.s.kindClient.Get(ctx, beehive.ObjectID(id), beehive.LoadOwner())
	if errors.Is(err, beehive.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get cached kind %d: %w", id, err)
	}

	// The cache is gone when this reports none, so its file went with it and there are
	// no rows left to clear.
	cacheID, ok, err := a.cacheIDForKind(obj)
	if err != nil || !ok {
		if err != nil { // coverage:ignore the Get above loaded the owner, the one thing Owner can fail on
			return nil, err
		}
		return toClusterCachedKind(obj)
	}

	// Inside the sync's hold, scoped to this kind: a worker resuming from its cookie after
	// the rows went would apply deltas with no cold list behind them, leaving the cache
	// permanently short of what it held.
	if err := a.s.kubesyncSvc.RunWithKindSyncStopped(int64(cacheID), toKubestoreKind(obj.Spec), func() error {
		return clearKindRows(ctx, a.s.kubestoreMgr, int64(cacheID), obj.Spec)
	}); err != nil {
		return nil, fmt.Errorf("clear cached kind %d rows: %w", id, err)
	}
	return toClusterCachedKind(obj)
}

// SetSyncEnabled is the user's switch for one kind. The wire's positive form; the stored
// field is its inverse, so the zero value can mean syncing.
func (a cachedKindsAPI) SetSyncEnabled(ctx context.Context, id ClusterCachedKindID, syncEnabled bool) (*ClusterCachedKind, error) {
	// Read-modify-write under the lock the discovery sweep also takes: beehive's Update
	// wants the whole spec and offers no compare-and-swap, so a sweep converging a catalog
	// change alongside this would write its own read back over the switch.
	a.s.kindSpecMu.Lock()
	defer a.s.kindSpecMu.Unlock()

	obj, err := a.s.kindClient.Get(ctx, beehive.ObjectID(id))
	if err != nil {
		return nil, wrapKindErr("get", id, err)
	}
	spec := obj.Spec
	spec.Paused = !syncEnabled
	// The record can be collected between the read and the write, so this reports a
	// missing record too.
	if _, err := a.s.kindClient.Update(ctx, beehive.ObjectID(id), spec); err != nil {
		return nil, wrapKindErr("update", id, err)
	}
	// Read back rather than project what Update returned: the served record carries the
	// owner edge a client joins on, and a write loads no edges.
	updated, err := a.s.kindClient.Get(ctx, beehive.ObjectID(id), beehive.LoadOwner())
	if err != nil {
		return nil, wrapKindErr("get", id, err)
	}
	return toClusterCachedKind(updated)
}

// wrapKindErr annotates a store error, mapping beehive's missing-record sentinel onto the
// boundary's own.
func wrapKindErr(verb string, id ClusterCachedKindID, err error) error {
	if errors.Is(err, beehive.ErrNotFound) {
		return fmt.Errorf("%s cached kind %d: %w", verb, id, ErrNotFound)
	}
	return fmt.Errorf("%s cached kind %d: %w", verb, id, err)
}

// clearKindRows drops one kind's rows from the cache holding them. A cache with no file
// has nothing to clear — and opening one would create the very file the clear is
// removing, which is why this goes through OpenExisting rather than claiming a store
// outright.
func clearKindRows(ctx context.Context, mgr kubestoreManager, cacheID int64, spec ClusterCachedKindSpec) error {
	store, ok, err := mgr.OpenExisting(cacheID)
	if err != nil || !ok {
		return err
	}
	defer store.Release()

	// The record's own Kind, so a clear never has to ask the catalog table which rows are
	// this kind's — a kind that table has not registered would keep every row.
	return store.ClearKind(ctx, kubestore.Kind{
		APIVersion: spec.APIVersion,
		Kind:       spec.Kind,
		Resource:   spec.Resource,
	})
}

// cacheIDForKind is the cache holding a kind's rows, read off the record's owner
// edge. A cache that is already gone is not an error: the file went with it, so there
// is nothing left to clear per kind.
func (a cachedKindsAPI) cacheIDForKind(
	obj *beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus],
) (ClusterCacheID, bool, error) {
	cacheRef, ok, err := obj.Owner()
	if err != nil {
		return 0, false, fmt.Errorf("read cached kind %d owner: %w", obj.ID, err)
	}
	if !ok {
		return 0, false, nil
	}
	return ClusterCacheID(cacheRef.ID), true, nil
}

// loadKindOwner eager-loads the owner edge every frame carries as its join key; beehive
// batches the lookup per change batch, so a watch does not become an N+1.
var loadKindOwner = beehive.WithLoads(beehive.LoadOwner())

// kindWatch projects this kind into delta frames. The departure carries the spec but no
// owner: the row is gone, so beehive loads no edge for it and reading one would fail the whole
// stream — and a consumer keys the record it is dropping by id anyway.
var kindWatch = collectionWatch[ClusterCachedKindSpec, ClusterCachedKindStatus, ClusterCachedKindCollectionWatchFrame]{
	frame: func(t CollectionWatchFrameType, obj *beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus]) (ClusterCachedKindCollectionWatchFrame, error) {
		kind, err := toClusterCachedKind(obj)
		if err != nil {
			return ClusterCachedKindCollectionWatchFrame{}, err
		}
		return ClusterCachedKindCollectionWatchFrame{Type: t, Kind: kind}, nil
	},
	departed: func(change beehive.ObjectChange[ClusterCachedKindSpec, ClusterCachedKindStatus]) ClusterCachedKindCollectionWatchFrame {
		kind := &ClusterCachedKind{RecordMeta: RecordMeta{ID: ObjectID(change.ID)}}
		if obj := change.Object; obj != nil {
			kind.RecordMeta = toRecordMeta(obj)
			kind.Spec = obj.Spec
		}
		return ClusterCachedKindCollectionWatchFrame{Type: CollectionWatchFrameDeleted, Kind: kind}
	},
	bookmark: ClusterCachedKindCollectionWatchFrame{Type: CollectionWatchFrameBookmark},
}

// clusterCachedKindController arms one kind's sync and logs its transitions. The record is
// what turns a kind the cluster serves into one that is actually mirrored: kubesync decides
// what exists, and this decides what is synced.
type clusterCachedKindController struct {
	lifecycle.None
	// Every kind's client, not just this one's: a cached kind reads the cache and cluster
	// it hangs off, and reaches the store through the shared services.
	deps
}

func (c *clusterCachedKindController) Reconcile(
	ctx context.Context,
	client beehive.ControllerClient[ClusterCachedKindStatus],
	obj *beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus],
) beehive.Result {
	cacheID, hasCache, err := c.ownerCacheID(ctx, obj)
	if err != nil {
		return beehive.Fail(err)
	}
	// A kind whose cache is gone is on its way out with it: the cache's own pass stopped the
	// sync and removed the file, and nothing registers against a cache id that names no record.
	//
	// The registration is withdrawn again anyway, because ForgetCache is not the last word on
	// it: a kind pass already in flight when the cache went can re-register this kind behind
	// that call, and no cache pass will come round to drop it. The id comes off the record's
	// name, which is all that still carries it here.
	if !hasCache {
		if obj.DeletionRequestedAt != nil {
			if id, ok := cacheIDInKindName(obj.Name); ok {
				c.kubesyncSvc.ForgetKind(id, toKubestoreKind(obj.Spec))
			}
		}
		return beehive.Settled()
	}

	if obj.DeletionRequestedAt != nil {
		if err := c.stopSyncAndClearRows(ctx, cacheID, obj.Spec); err != nil {
			return beehive.Fail(err)
		}
		return beehive.Settled()
	}

	// Paused stops the sync and KEEPS THE ROWS — no clearKindRows, which is what makes a
	// deletion a deletion. Level-triggered, so this fires on every pass while the kind is
	// paused; the whole chain is idempotent, and one armMu acquisition per paused kind is
	// cheaper than tracking the transition.
	if obj.Spec.Paused {
		c.kubesyncSvc.ForgetKind(int64(cacheID), toKubestoreKind(obj.Spec))
		// Ahead of the return, or the pause never reaches the timeline: the logging below
		// this branch is not reached.
		if err := c.logSyncVerdict(ctx, client, cacheID, obj.Spec); err != nil {
			return beehive.Fail(err)
		}
		return beehive.Settled()
	}

	// Registering outlives the cache being paused, which is what makes a resume one call:
	// kubesync holds this and runs nothing until the cache above is armed.
	c.kubesyncSvc.TrackKind(int64(cacheID), toKubestoreKind(obj.Spec))

	if err := c.logSyncVerdict(ctx, client, cacheID, obj.Spec); err != nil {
		return beehive.Fail(err)
	}
	// No condition: the verdict is the state watch's, and a stored one would serve a dead
	// process's answer until the passes caught up.
	return beehive.Settled()
}

// stopSyncAndClearRows takes one kind down, in this order and only this one: ForgetKind
// returns once the worker is joined, and the rows go after. Clearing first would race a relist
// page landing behind it, leaving rows for a kind nothing syncs any more.
func (c *clusterCachedKindController) stopSyncAndClearRows(ctx context.Context, cacheID ClusterCacheID, spec ClusterCachedKindSpec) error {
	c.kubesyncSvc.ForgetKind(int64(cacheID), toKubestoreKind(spec))
	if err := clearKindRows(ctx, c.kubestoreMgr, int64(cacheID), spec); err != nil {
		return fmt.Errorf("clear cached kind %s rows: %w", spec.Resource, err)
	}
	return nil
}

// ownerCacheID is the cache a kind is mirrored into. Reporting false is the cache being gone,
// which is not an error: beehive's GC cascades, so a kind outliving its cache is a race this
// pass has to answer rather than fail on.
func (c *clusterCachedKindController) ownerCacheID(
	ctx context.Context,
	obj *beehive.Object[ClusterCachedKindSpec, ClusterCachedKindStatus],
) (ClusterCacheID, bool, error) {
	owner, ok, err := c.kindClient.GetOwner(ctx, obj.ID)
	if err != nil {
		return 0, false, fmt.Errorf("read cached kind %d owner: %w", obj.ID, err)
	}
	return ClusterCacheID(owner.ID), ok, nil
}

// logSyncVerdict records this kind's verdict on its own timeline. Every pass, because
// repeating a run's (Category, Type, Reason) extends that run rather than appending — so a
// flapping kind costs one row per transition.
func (c *clusterCachedKindController) logSyncVerdict(
	ctx context.Context,
	client beehive.ControllerClient[ClusterCachedKindStatus],
	cacheID ClusterCacheID,
	spec ClusterCachedKindSpec,
) error {
	reason, message, ok := kindVerdict(c.kubesyncSvc, cacheID, spec)
	if !ok {
		return nil
	}
	if err := client.AddEvent(ctx, beehive.EventSpec{
		Category: categorySync,
		Type:     syncEventType(reason),
		Reason:   reason,
		Message:  message,
	}); err != nil {
		return fmt.Errorf("log cached kind %s sync: %w", spec.Resource, err)
	}
	return nil
}

// kindVerdict is one kind's reason and message, and whether there is one at all.
//
// **Paused is decided ahead of the state read, never derived from its absence.** A paused
// kind is forgotten, so kubesync has nothing to say about it — and it deliberately does not
// know why it was not asked to sync something. The record does.
func kindVerdict(svc kubesyncService, cacheID ClusterCacheID, spec ClusterCachedKindSpec) (reason, message string, ok bool) {
	if spec.Paused {
		return ReasonPaused, "", true
	}
	state, ok := svc.GetKindState(int64(cacheID), toKubestoreKind(spec))
	if !ok {
		return "", "", false
	}
	return state.Reason, state.Message, true
}

// syncEventType grades a kind's verdict. Stale is a warning and not a failure: the rows are
// still served, they have simply stopped being known current.
func syncEventType(reason string) beehive.EventType {
	switch reason {
	case kubesync.ReasonSyncFailed, kubesync.ReasonStale,
		kubesync.ReasonNoConnection, kubesync.ReasonIdentityMismatch:
		return beehive.EventWarning
	default:
		return beehive.EventNormal
	}
}

// toKubestoreKind is the record's identity as the store and the sync seam both name it. The
// three fields and nothing else: whether the kind syncs is its cache's, never relayed here.
func toKubestoreKind(spec ClusterCachedKindSpec) kubestore.Kind {
	return kubestore.Kind{
		APIVersion: spec.APIVersion,
		Kind:       spec.Kind,
		Resource:   spec.Resource,
	}
}
