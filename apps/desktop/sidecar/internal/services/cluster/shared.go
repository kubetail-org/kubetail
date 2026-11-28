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

// Vocabulary every family reuses: the app's services as this package sees them,
// identity, conditions, the kind-agnostic Event and Schedule projections, and the
// collection watch frame type. Nothing here belongs to one kind — a type only one family
// uses lives in that family's file, however generic its name reads. The projections
// mirror the shared-vocabulary section of graph/schema.graphqls, which they bind to.
package cluster

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/amorey/beehive"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd/api"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubeconn"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubesync"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/kubeconfig"
)

// ErrNotFound is the boundary's sentinel for an id that names no tracked record,
// matched with errors.Is. graph maps it to KUBETAIL_RECORD_NOT_FOUND.
var ErrNotFound = errors.New("cluster: cluster not found")

// ErrIdentityMoved is a cluster whose identity moved under both attempts of a
// ReadActive, matched with errors.Is.
var ErrIdentityMoved = errors.New("cluster: the cluster's identity moved under the read")

// ErrDeclaredBySource is the sentinel for deleting a record its source still declares,
// matched with errors.Is. The source decides which records exist, so deleting one it
// still lists would only re-import it under a fresh id — losing the user's toggles to
// the new record's defaults, and every id a client was holding. Stop tracking such a
// cluster at the source, or disable it. graph maps it to KUBETAIL_CONFLICT.
var ErrDeclaredBySource = errors.New("cluster: cluster is still declared by its source")

// ErrCannotBeDefault is the sentinel for making a cluster the default when it cannot
// be one: disabled, from a source other than the kubeconfig, or no longer declared by
// it. graph maps it to KUBETAIL_VALIDATION_ERROR.
var ErrCannotBeDefault = errors.New("cluster: cluster cannot be the default")

// ErrNotConnectable is the sentinel for a record that exists and will not be connected:
// disabled, awaiting deletion, or from a source that carries no credentials. Told apart
// from ErrNotFound because the remedy is the record's own state rather than a bad id — a
// caller offers to enable the cluster instead of reporting it gone. graph maps it to
// KUBETAIL_CONFLICT.
var ErrNotConnectable = errors.New("cluster: cluster cannot be connected")

// --- Process-wide services ---
//
// The app's own services as this package uses them: narrow, so a test hands a
// controller a static config or stands in for a live cluster without the network.
// Together here rather than beside the kind that reads one, because they travel
// together in deps and reach every kind through it.

// kubeconfigService is the one reader of the user's kubeconfig. RESTConfig is the
// connection probe's seam; the reconciles read Get and the trigger subscribes.
type kubeconfigService interface {
	Get() (*api.Config, bool)
	RESTConfig(contextName string) (*rest.Config, string, error)
	WatchConfigState() kubeconfig.ConfigStateWatch
}

// kubeconnService is the pool a cluster is talked to over. Acquire names a context and
// claims its connection, which is also what arms the probe behind it; a claim outlives
// the pass that took it, so the caller holds and releases rather than asking per pass.
// RetryAndWait re-runs a context's probes now and returns when the one it asked for has
// finished, so a caller can report the probe's own duration.
type kubeconnService interface {
	Acquire(contextName string) kubeconn.Lease
	RetryAndWait(ctx context.Context, contextName string) error
	ListenForConnectionChanges() kubeconn.ConnectionChangeNotifications
}

// kubestoreManager is the cache directory as this package reaches it: the teardowns and
// clears the controllers drive, plus what the stats state watch measures. Named for the leaf
// type it stands for, the way every narrow interface here is.
//
// **OpenExisting is the door to a cache's contents** — reading it, or clearing a kind in
// it — and claims the file for as long as the caller holds it. It never creates one: a
// reader reconnecting in the window between a cache being marked for deletion and its
// teardown pass would otherwise resurrect it as an orphan.
//
// ListenForChanges borrows the change notifications of a cache someone already holds open,
// taking no claim, for the state watch — which measures through Stats and needs no file of its
// own. ListenForCacheOpenChanges is the read side's other half: a watch that found no file
// waits on it rather than polling, since nothing else would tell it the cache came up.
type kubestoreManager interface {
	OpenExisting(cacheID int64) (*kubestore.Store, bool, error)
	ListenForChanges(cacheID int64, keys ...string) (kubestore.ChangeNotifications, bool)
	ListenForCacheOpenChanges(cacheID int64) kubestore.CacheOpenChangeNotifications
	Clear(cacheID int64) error
	Remove(cacheID int64) error
	Stats(ctx context.Context, cacheID int64) (kubestore.Stats, error)
	// ListenForSizeLimitChanges carries the caches whose size verdict has changed. Edge-triggered
	// and payload-free, like every feed a trigger rides: the pass reads Stats for the
	// verdict itself.
	ListenForSizeLimitChanges() kubestore.SizeLimitChangeNotifications
}

// CacheFileOwner keeps a file per cluster cache beside the mirror's, named by the cache's
// id: the log store. The cache pass removes the file with the cache, and the user's clear
// of the cache clears it too, so a clear means what it says. Both calls are idempotent,
// and a cache that never had a file is not an error.
type CacheFileOwner interface {
	Clear(cacheID int64) error
	Remove(cacheID int64) error
}

// cacheFiles is the CacheFileOwner as the passes reach it: unset until SetCacheFiles,
// and a call before that fails, since a file left behind would be nobody's to remove.
type cacheFiles struct {
	mu    sync.Mutex
	owner CacheFileOwner
}

var errNoCacheFiles = errors.New("cluster service: no cache file owner")

func (c *cacheFiles) set(owner CacheFileOwner) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.owner = owner
}

func (c *cacheFiles) get() (CacheFileOwner, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.owner == nil {
		return nil, errNoCacheFiles
	}
	return c.owner, nil
}

func (c *cacheFiles) Clear(cacheID int64) error {
	owner, err := c.get()
	if err != nil {
		return err
	}
	return owner.Clear(cacheID)
}

func (c *cacheFiles) Remove(cacheID int64) error {
	owner, err := c.get()
	if err != nil {
		return err
	}
	return owner.Remove(cacheID)
}

// kubesyncService is the seam that fills a cache, as this package reaches it. It speaks
// cache ids, kube-contexts, server UIDs and GVRs; turning one of those into a record is
// this package's business and never reaches back down.
//
// The two arming pairs AND rather than nest: the cache's says whether it syncs at all,
// each kind's says which kinds, and a kind registered against an unarmed cache is held
// rather than refused — so a cache's pass and a kind's may land in either order.
//
// ForgetDiscovery is a pause and ForgetCache is a teardown, and the difference is what
// becomes of the kinds: a pause keeps them registered so a resume is one call, where a
// deleted cache must leave nothing behind to hold them.
//
// **RunWithCacheSyncStopped/RunWithKindSyncStopped are what a clear runs inside.** A clear swaps the
// file under whoever holds it open, and only kubesync can stop the workers writing
// through it; the store work stays here, because a paused cache has no session and its
// file is still there to clear.
type kubesyncService interface {
	TrackDiscovery(cacheID int64, p kubesync.Params)
	ForgetDiscovery(cacheID int64)
	ForgetCache(cacheID int64)
	TrackKind(cacheID int64, k kubestore.Kind)
	ForgetKind(cacheID int64, k kubestore.Kind)
	GetDiscoveryState(cacheID int64) (kubesync.DiscoveryState, bool)
	GetKindState(cacheID int64, k kubestore.Kind) (kubesync.KindState, bool)
	ListenForDiscoveryChanges() kubesync.DiscoveryChangeNotifications
	ListenForKindChanges() kubesync.KindChangeNotifications
	RunWithCacheSyncStopped(cacheID int64, fn func() error) error
	RunWithKindSyncStopped(cacheID int64, k kubestore.Kind, fn func() error) error
	RestartAll()
}

// --- Identity ---

// ObjectID is apimeta's, so other services can name a record here without
// importing this package. The per-kind aliases below name the same type purely
// to document which kind an id refers to.
type ObjectID = apimeta.ObjectID

// ClusterID identifies a cluster record: apimeta's, so chat can file a chat under
// one. It is the name of the record's beehive object, so every cluster lookup here is
// GetByName(string(id)); the object's own ObjectID stays internal, used for the owner
// edges its caches hang off.
type ClusterID = apimeta.ClusterID

// ClusterCacheID identifies one ClusterCache record. A cluster can own several,
// so the cache id is not derivable from the cluster id.
type ClusterCacheID = ObjectID

// ClusterCachedKindID identifies one synced kind's ClusterCachedKind object.
type ClusterCachedKindID = ObjectID

// RecordMeta is the per-record metadata every served kind carries, embedded so a new
// beehive metadata field is one edit rather than one per kind. Conditions sit here
// too: beehive keeps them as object rows, not in the status blob, so every kind reads
// them off the object the same way.
//
// The id is ObjectID rather than a per-kind alias — the aliases name the same type, so
// one field serves them all and each kind's doc comment still says what it points at.
//
// Exported only so other packages can build a record literal; nothing outside this one
// fills it from an object (toRecordMeta does that).
type RecordMeta struct {
	ID                  ObjectID
	Generation          int64
	CreatedAt           time.Time
	DeletionRequestedAt *time.Time // beehive's soft-delete tombstone, surfaced as-is
	Conditions          []Condition
}

// toRecordMeta projects the metadata half of any stored object.
func toRecordMeta[Spec, Status any](obj *beehive.Object[Spec, Status]) RecordMeta {
	return RecordMeta{
		ID:                  ObjectID(obj.ID),
		Generation:          obj.Generation,
		CreatedAt:           obj.CreatedAt,
		DeletionRequestedAt: obj.DeletionRequestedAt,
		Conditions:          obj.Conditions,
	}
}

// ObjectRef points at another record — today always the beehive owner every child
// kind hangs off, which is the join key a client folds on. Kind-agnostic, so one type
// serves the whole ownership chain rather than a differently-named id per kind.
//
// A defined type rather than beehive.ObjectRef, which aliases into an internal package
// gqlgen cannot import. Group is left off: every kind here is in the empty group, so
// the field would always be "".
type ObjectRef struct {
	ID   ObjectID
	Kind string
}

// toOwnerRef reads the owner edge a child record's join key comes from. The read must
// have loaded it (beehive.LoadOwner) — the beehive name embeds the same id, but it is
// a reconcile key rather than a field, and parsing it back would let the two disagree
// the moment either moves. A read that forgot the load is a caller bug, so it errors.
//
// **No edge is the zero ref, not an error.** A live record with no owner is corruption,
// and the client's join drops it — the same outcome as any record whose owner it cannot
// find, and better than failing a whole read over one row. A collected record never
// reaches here at all: its edges went with it (ON DELETE CASCADE) and beehive loads none
// for the removal, so a watch builds that frame without an owner (see cacheDeparture).
func toOwnerRef[Spec, Status any](obj *beehive.Object[Spec, Status]) (ObjectRef, error) {
	owner, ok, err := obj.Owner()
	if err != nil {
		return ObjectRef{}, fmt.Errorf("read %s %d owner: %w", obj.Kind, obj.ID, err)
	}
	if !ok {
		return ObjectRef{}, nil
	}
	return ObjectRef{ID: ObjectID(owner.ID), Kind: owner.Kind}, nil
}

// --- Conditions ---

// ConditionStatus is a condition's three-valued verdict, Kubernetes-style.
type ConditionStatus = beehive.ConditionStatus

const (
	ConditionTrue    = beehive.ConditionTrue
	ConditionFalse   = beehive.ConditionFalse
	ConditionUnknown = beehive.ConditionUnknown
)

// ConditionType names one independently-tracked aspect of a record's observed
// state. Each type has exactly one writer.
type ConditionType string

const (
	// ConditionConnected reports whether the last connection probe
	// reached the cluster's API server and resolved its identity facts.
	ConditionConnected ConditionType = "Connected"
	// ConditionIdentified reports whether the probe could tell which cluster
	// answered. Separate from Connected, whose subject is whether we reached the
	// server at all: this one is about what these credentials are allowed to read,
	// and it is what gates the cache, since a cache is named for the identity it
	// mirrors.
	ConditionIdentified ConditionType = "Identified"
	// ConditionSynced reports the state of a sync. A cache's pass is its only writer, and
	// writes it for one reason: the size ceiling. Its presence is what holds that pause —
	// the file closes with the sync, so the verdict the pause was made on is gone.
	ConditionSynced ConditionType = "Synced"
)

// Condition reason constants — CamelCase machine-readable explanations for a
// condition's status, Kubernetes-style. Human detail goes in Message.
const (
	// ReasonInactive: no connection is maintained — the record is orphaned,
	// archived, or deactivated.
	ReasonInactive = "Inactive"
	// ReasonConnecting: a probe is owed but none has succeeded or failed yet
	// (a freshly-minted record awaiting its first pass).
	ReasonConnecting = "Connecting"
	// ReasonConnected: the last connection probe succeeded.
	ReasonConnected = "Connected"
	// ReasonProbeFailed: the connection probe failed and gave no class of failure.
	// A classified failure carries the probe's own reason instead (kubeconn.Reason).
	ReasonProbeFailed = "ProbeFailed"
	// ReasonUnreachable: the health probe's transport failed outright.
	ReasonUnreachable = "Unreachable"
	// ReasonNoConnection: health cannot be assessed without a live
	// connection this pass.
	ReasonNoConnection = "NoConnection"
	// ReasonIdentified: the probe read the cluster's identity.
	ReasonIdentified = "Identified"
	// ReasonUIDUnreadable: the probe reached the server and was refused the read
	// that names it, so the cluster is usable but cannot be mirrored.
	ReasonUIDUnreadable = "UIDUnreadable"
	// ReasonPaused: nothing is syncing — the record is sync-disabled, deactivated,
	// orphaned, or archived.
	ReasonPaused = "Paused"
	// ReasonSizeLimit: nothing is syncing because the cache's file is at its size
	// ceiling. Apart from ReasonPaused so that "the user turned sync off" and "this
	// cache filled up" stay distinguishable to whatever keys on the reason.
	ReasonSizeLimit = "SizeLimit"
)

// A kind's sync reasons and classes of failure, as ClusterCacheHealth carries
// them, for a reader outside the package.
const (
	KindReasonSyncing    = kubesync.ReasonSyncing
	KindReasonResuming   = kubesync.ReasonResuming
	KindReasonResyncing  = kubesync.ReasonResyncing
	KindReasonSyncFailed = kubesync.ReasonSyncFailed

	FailureForbidden    = string(kubeconn.ReasonForbidden)
	FailureUnauthorized = string(kubeconn.ReasonUnauthorized)
)

// Condition aliases beehive's status condition. Conditions are beehive object
// rows (not inside our status blob); the store owns TransitionedAt stamping,
// no-op suppression, and the liveness downgrade.
type Condition = beehive.Condition

// LiveCondition is the sole condition constructor. Every condition here describes
// process-scoped state, so Liveness makes beehive downgrade a previous process's
// write to Unknown until re-confirmed (docs/adr/2026-08-09-liveness-conditions.md).
// Capping the message here — the one place every condition is built — is what keeps
// an unbounded source out of a frame re-serialized to every watcher.
func LiveCondition(t ConditionType, status ConditionStatus, reason, message string) Condition {
	return Condition{
		Type: string(t), Status: status, Reason: reason, Message: TruncateMessage(message), Liveness: true,
	}
}

// FindCondition returns a pointer to the condition of the given type, or nil.
func FindCondition(conds []Condition, t ConditionType) *Condition {
	for i := range conds {
		if conds[i].Type == string(t) {
			return &conds[i]
		}
	}
	return nil
}

// MaxMessageLen caps a persisted message (on a condition or an event run). Both are read
// back on every frame of a whole-fleet watch, and their sources are unbounded: a raw
// client-go error, or a /readyz?verbose=true body, which routinely runs to kilobytes.
const MaxMessageLen = 200

// TruncateMessage caps s at MaxMessageLen bytes, appending an ellipsis when it overflows.
// (Byte-bounded; error strings are effectively ASCII.)
func TruncateMessage(s string) string {
	if len(s) <= MaxMessageLen {
		return s
	}
	return s[:MaxMessageLen] + "…"
}

// Probe outcomes are not stored on ClusterStatus — they ride beehive's event log,
// keeping per-probe chatter off the status watch (a repeated failure bumps a run's
// Count, no status rewrite).
const (
	// ConnectionEventCategory is the connection controller's probe-outcome category.
	ConnectionEventCategory = "connection"
)

// Event is one coalesced run from a beehive object's event timeline, served under
// every kind's events surface. ID is the client's upsert key (a reason can repeat;
// a run id cannot); a repeated same-outcome occurrence bumps Count, and
// [FirstAt, LastAt] is the run's window.
type Event struct {
	ID       ObjectID          `json:"id"`
	Category string            `json:"category"`
	Type     beehive.EventType `json:"type"`
	Reason   string            `json:"reason"`
	Message  string            `json:"message"`
	Count    int               `json:"count"`
	FirstAt  time.Time         `json:"firstAt"`
	LastAt   time.Time         `json:"lastAt"`
}

// Schedule is the kind-agnostic projection of when an object is next due to be
// looked at, served as a state watch of its own — a scheduling change fires no object
// WatchList. What does the looking is the kind's own: a cluster's
// is the pool's probe cadence.
type Schedule struct {
	// NextRequeueAt is the next attempt's time, or nil while a probe is in flight or
	// nothing is scheduled.
	NextRequeueAt *time.Time `json:"nextRequeueAt"`
	// Probing reports whether a network probe is in flight, asserted rather than
	// inferred from the countdown so the webview can show a definite "checking
	// now". Merged into this state watch because a probe start/end fires no WatchList.
	Probing bool `json:"probing"`
	// Failures counts the connection probe's consecutive failed attempts, zero once one
	// succeeds.
	Failures int `json:"failures"`
}

// The collection watch frame type is shared vocabulary (internal/lib/apimeta), aliased here so
// this package's watches keep speaking their own name.
type CollectionWatchFrameType = apimeta.CollectionWatchFrameType

const (
	CollectionWatchFrameAdded    = apimeta.CollectionWatchFrameAdded
	CollectionWatchFrameModified = apimeta.CollectionWatchFrameModified
	CollectionWatchFrameDeleted  = apimeta.CollectionWatchFrameDeleted
	CollectionWatchFrameBookmark = apimeta.CollectionWatchFrameBookmark
)

// deltaFrameType classifies a change that is not a removal, for any kind's watch pump.
// The soft-delete mark is one of them: the row is still there, wearing a tombstone the
// record carries through, so it is a Modified like any other field moving.
func deltaFrameType[Spec, Status any](change beehive.ObjectChange[Spec, Status]) CollectionWatchFrameType {
	if change.Type == beehive.Added {
		return CollectionWatchFrameAdded
	}
	return CollectionWatchFrameModified
}

// EventFeedFrameType classifies one frame on an event feed. Two values where
// CollectionWatchFrameType has four, because an event log is a positioned log rather than a mirrored
// set: beehive delivers the snapshot plus what grows above it and never reports a
// prune, so a run is only ever upserted and there is no Deleted to forward.
type EventFeedFrameType string

const (
	EventFeedFrameRun EventFeedFrameType = "Run"
	// EventFeedFrameBookmark closes the on-subscribe snapshot, exactly once per stream, so
	// a consumer can tell an empty timeline from one still arriving.
	EventFeedFrameBookmark EventFeedFrameType = "Bookmark"
)

// EventFeedFrame is one frame on an event feed: a run to upsert by
// Event.ID, or the bookmark closing the snapshot, which carries no run.
type EventFeedFrame struct {
	Type  EventFeedFrameType
	Event *Event
}

// TimePtrEqual compares two optional timestamps: both nil is equal, and a present
// pair compares by instant, so two readings of the same stamp never look changed.
func TimePtrEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}
