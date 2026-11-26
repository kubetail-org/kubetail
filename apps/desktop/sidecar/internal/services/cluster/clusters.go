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

// The Cluster kind: a tracked kube-context. The record served to resolvers — its
// clusters row joined with the runtime object beehive holds for it — its collection watch
// frame, the Clusters implementation, and the controller over the runtime object.
// Mirrors the Cluster section of graph/schema.graphqls. The importer that inserts the
// rows is in clustersources.go; the mirror that keeps an object per row is mirror.go.
package cluster

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/amorey/beehive"
	"k8s.io/client-go/tools/clientcmd/api"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/deltafold"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/lifecycle"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubeconn"
)

// ClusterGroupKind identifies the Cluster beehive resource kind. An object of it is
// named by its row's id, so a ClusterID resolves to it with GetByName.
var ClusterGroupKind = beehive.GroupKind{Kind: "Cluster"}

// ClusterStatusSourceKubeconfig is the kubeconfig-sourced record's last-known
// kubeconfig observation: the cluster/user entry names and presence. Cached from the
// last time the context was present, so it survives orphaning.
type ClusterStatusSourceKubeconfig struct {
	Cluster   ClusterStatusSourceKubeconfigCluster `json:"cluster"`
	User      ClusterStatusSourceKubeconfigUser    `json:"user"`
	IsPresent bool                                 `json:"isPresent"`
	IsDefault bool                                 `json:"isDefault"`
}

// ClusterStatusSourceKubeconfigCluster is the cluster half of a context: the entry name
// it references, and the entry that name resolves to. The name is stated by the context
// itself and the entry by the file, so the two go missing independently.
type ClusterStatusSourceKubeconfigCluster struct {
	Name string `json:"name"`
	// nil when the kubeconfig defines no entry by that name. Read from the file rather
	// than off the resolved rest.Config, so it is known before a first probe.
	Entry *ClusterStatusSourceKubeconfigClusterEntry `json:"entry,omitempty"`
}

// ClusterStatusSourceKubeconfigClusterEntry mirrors the kubeconfig cluster entry.
// Fields are added as a consumer needs them, so this is a subset of what the file
// holds — but every field present is what the file says, and no verdict is drawn over
// them here. A cluster entry carries no credential; the user half does, which is why
// only this one mirrors.
type ClusterStatusSourceKubeconfigClusterEntry struct {
	Server                string `json:"server"`
	InsecureSkipTLSVerify bool   `json:"insecureSkipTLSVerify"`
	// HasCertificateAuthority is whether the entry names a CA, by path or inline —
	// presence alone, since with a schemeless server it is what makes client-go
	// dial https.
	HasCertificateAuthority bool `json:"hasCertificateAuthority"`
}

// ClusterStatusSourceKubeconfigUser is the user half of a context. An authInfo
// entry holds credentials (token, client key, exec env), so whatever is served from
// it is a chosen projection, never a mirror: the entry name, and whether a client
// certificate is named — presence alone, for the same reason the CA's is.
type ClusterStatusSourceKubeconfigUser struct {
	Name                 string `json:"name"`
	HasClientCertificate bool   `json:"hasClientCertificate"`
}

// ClusterSpecSource is the discriminated union naming where a cluster record
// comes from and how its credentials resolve.
type ClusterSpecSource struct {
	Kubeconfig *ClusterSpecSourceKubeconfig `json:"kubeconfig,omitempty"`
}

// ClusterSpecSourceKubeconfig is the kubeconfig-sourced variant of ClusterSpecSource.
type ClusterSpecSourceKubeconfig struct {
	Context string `json:"context"`
}

// ClusterStatusSource is the status-side counterpart of ClusterSpecSource.
type ClusterStatusSource struct {
	Kubeconfig *ClusterStatusSourceKubeconfig `json:"kubeconfig,omitempty"`
}

// ClusterServer holds last-known facts about the remote cluster, discovered by
// connecting. Nil fields mean never probed.
type ClusterServer struct {
	UID     *string `json:"uid,omitempty"`
	Version *string `json:"version,omitempty"`
	// Endpoint is the API server URL that answered, which two contexts naming one
	// server share — the fact that explains why they resolve to one connection.
	Endpoint *string `json:"endpoint,omitempty"`
}

// ClusterPrincipal holds last-known facts about the connecting client's
// identity on the cluster. Nil fields mean never probed.
type ClusterPrincipal struct {
	Username *string `json:"username,omitempty"`
	// Groups is what the username's access actually comes from, since RBAC binds to
	// groups far more often than to users. Sorted, so a re-ordered read is not a change.
	Groups []string `json:"groups,omitempty"`
}

// ClusterSpec is a cluster record's desired state as served: the user's choices,
// projected off the SQL row. Nothing here is observed, and nothing here is stored in
// beehive — the runtime spec below carries only what the passes act on.
type ClusterSpec struct {
	Name              *string
	Enabled           bool
	SyncEnabled       bool
	MonitoringEnabled bool
	// IsDefault marks the user's default cluster, which a new window opens on. At most
	// one record is the default.
	IsDefault bool
	// Source says where this record comes from and how credentials resolve; the
	// matching observation lives on ClusterStatus.Source, rewritten each reconcile.
	Source ClusterSpecSource
}

// ClusterRuntimeSpec is the runtime object's spec: the four row fields the cluster
// and cache passes act on, and nothing else. Only the mirror writes it. A display
// name or a monitoring toggle here would be a second copy of a user's choice.
type ClusterRuntimeSpec struct {
	Source      string  `json:"source"`
	SourceKey   *string `json:"sourceKey,omitempty"`
	Enabled     bool    `json:"enabled"`
	SyncEnabled bool    `json:"syncEnabled"`
}

// runtimeSpecOf is the one converter from a row to the spec its runtime object holds.
func runtimeSpecOf(row ClusterRow) ClusterRuntimeSpec {
	return ClusterRuntimeSpec{
		Source:      row.Source,
		SourceKey:   row.SourceKey,
		Enabled:     row.Enabled,
		SyncEnabled: row.SyncEnabled,
	}
}

// kubeconfigContextOf is the kube-context a runtime spec resolves credentials
// through, or "" for a spec of another source.
func kubeconfigContextOf(spec ClusterRuntimeSpec) string {
	if spec.Source != SourceKubeconfig || spec.SourceKey == nil {
		return ""
	}
	return *spec.SourceKey
}

// specOf projects the row's user-owned fields into the served spec.
func specOf(row ClusterRow) ClusterSpec {
	spec := ClusterSpec{
		Name:              row.Name,
		Enabled:           row.Enabled,
		SyncEnabled:       row.SyncEnabled,
		MonitoringEnabled: row.MonitoringEnabled,
		IsDefault:         row.IsDefault,
	}
	if row.Source == SourceKubeconfig && row.SourceKey != nil {
		spec.Source.Kubeconfig = &ClusterSpecSourceKubeconfig{Context: *row.SourceKey}
	}
	return spec
}

// ClusterStatus is both the stored status and the one served to GraphQL:
// connection/health observations. Sync status lives on the ClusterCache child, so
// there is no merge type.
//
// **Only facts that change when the cluster does.** Every differing byte re-emits the
// record to every watcher, so per-probe telemetry — reasons, latency, failure counts,
// the next attempt — stays on the lease, where a reader that wants it takes one.
type ClusterStatus struct {
	Source    ClusterStatusSource `json:"source"`
	Server    ClusterServer       `json:"server"`
	Principal ClusterPrincipal    `json:"principal"`
}

// clusterStatus returns the stored status, or the zero value: beehive leaves Status
// nil until a controller first writes one, and a row whose runtime object the mirror
// has not created yet has none at all.
func clusterStatus(obj *beehive.Object[ClusterRuntimeSpec, ClusterStatus]) ClusterStatus {
	if obj == nil || obj.Status == nil {
		return ClusterStatus{}
	}
	return *obj.Status
}

// ClusterActiveUID returns the last-probed kube-system UID, or "" if never probed. It
// selects which owned ClusterCache is active.
func ClusterActiveUID(obj *beehive.Object[ClusterRuntimeSpec, ClusterStatus]) string {
	if uid := clusterStatus(obj).Server.UID; uid != nil {
		return *uid
	}
	return ""
}

// CacheIsActive reports whether a cache mirrors its parent's currently-active
// identity; one for an unknown identity never is. The single definition of "active
// cache" — sync gating and the read-side join must not disagree.
func CacheIsActive(clusterObj *beehive.Object[ClusterRuntimeSpec, ClusterStatus], cacheUID string) bool {
	active := ClusterActiveUID(clusterObj)
	return active != "" && cacheUID == active
}

// Cluster is the record for one tracked cluster connection (one kube-context): its
// SQL row — identity, stamps, the user's choices — joined with the status and
// conditions of the runtime object beehive holds for it, which the mirror creates
// after the row. A row without an object yet has zero status and no conditions.
// Owned ClusterCache records are not joined in here — they stream standalone via
// Caches().WatchCollection, so cache churn never re-emits a cluster. Nor is the
// next-reconcile time: a scheduling change fires no watch, so it is a state watch on
// Clusters().WatchScheduleState instead.
type Cluster struct {
	ID                  ClusterID
	CreatedAt           time.Time
	UpdatedAt           time.Time
	DeletionRequestedAt *time.Time
	Conditions          []Condition

	Spec   ClusterSpec
	Status ClusterStatus
}

// KubeContext is the kube-context the record comes from; empty for a record of
// another source.
func (c *Cluster) KubeContext() string {
	if src := c.Spec.Source.Kubeconfig; src != nil {
		return src.Context
	}
	return ""
}

// clusterIDOf is a runtime object's record id: its name.
func clusterIDOf(obj *beehive.Object[ClusterRuntimeSpec, ClusterStatus]) ClusterID {
	return ClusterID(obj.Name)
}

// ClusterCollectionWatchFrame is one frame on the cluster list watch: what happened (Type) to
// which cluster (Cluster), or the Bookmark closing the snapshot, which carries no
// cluster. On a Deleted change Cluster holds the last-known state; consumers key on
// Cluster.ID. Binds 1:1 to the GraphQL ClusterCollectionWatchFrame.
type ClusterCollectionWatchFrame struct {
	Type    CollectionWatchFrameType
	Cluster *Cluster
}

// toCluster builds the served record: the row, and the runtime object's status and
// conditions when there is one. Conditions come off the object rows rather than the
// status blob, which is where beehive keeps them. The mark is the row's — the
// object's own is set later, by the mirror, and never served.
func toCluster(row ClusterRow, obj *beehive.Object[ClusterRuntimeSpec, ClusterStatus]) *Cluster {
	c := &Cluster{
		ID:                  row.ID,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		DeletionRequestedAt: row.DeleteRequestedAt,
		Spec:                specOf(row),
		Status:              clusterStatus(obj),
	}
	if obj != nil {
		c.Conditions = obj.Conditions
	}
	return c
}

// runtimeObject reads the row's runtime object, nil when the mirror has not created
// it yet or has already removed it.
func (s *service) runtimeObject(ctx context.Context, id ClusterID) (*beehive.Object[ClusterRuntimeSpec, ClusterStatus], error) {
	obj, err := s.clusterClient.GetByName(ctx, string(id))
	if errors.Is(err, beehive.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get cluster %s runtime object: %w", id, err)
	}
	return obj, nil
}

// ActiveCluster is a cluster at one identity: its record, and the cache that mirrors
// that identity, nil while it has none.
type ActiveCluster struct {
	Cluster *Cluster
	Cache   *ClusterCache
}

func (a clustersAPI) ReadActive(ctx context.Context, id ClusterID, read func(context.Context, ActiveCluster) error) error {
	err := a.readActiveOnce(ctx, id, read)
	if errors.Is(err, ErrIdentityMoved) {
		err = a.readActiveOnce(ctx, id, read)
	}
	return err
}

// readActiveOnce is one attempt: the record and its active cache, read, then the
// record's identity again. A cache read names its cache, never the identity, so a
// UID that moved in between means read may have mixed two identities.
func (a clustersAPI) readActiveOnce(ctx context.Context, id ClusterID, read func(context.Context, ActiveCluster) error) error {
	active, uid, err := a.activeCluster(ctx, id)
	if err != nil {
		return err
	}
	if err := read(ctx, active); err != nil {
		return err
	}
	_, obj, err := a.record(ctx, id)
	if err != nil {
		return err
	}
	if ClusterActiveUID(obj) != uid {
		return ErrIdentityMoved
	}
	return nil
}

// activeCluster is the record and the cache CacheIsActive picks among the runtime
// objects, with the identity it read: none for a cluster never identified.
func (a clustersAPI) activeCluster(ctx context.Context, id ClusterID) (ActiveCluster, string, error) {
	row, obj, err := a.record(ctx, id)
	if err != nil {
		return ActiveCluster{}, "", err
	}
	active := ActiveCluster{Cluster: toCluster(row, obj)}
	uid := ClusterActiveUID(obj)
	if uid == "" {
		return active, "", nil
	}
	caches, err := a.s.cacheClient.ListOwnedObjects(ctx, obj.ID)
	if err != nil {
		return ActiveCluster{}, "", fmt.Errorf("list cluster %s caches: %w", id, err)
	}
	for _, cache := range caches {
		if CacheIsActive(obj, cache.Spec.ServerUID) {
			active.Cache = toClusterCache(cache)
			break
		}
	}
	return active, uid, nil
}

// record is the row and its runtime object, nil when the mirror has not made it
// yet; ErrNotFound for a row that is gone.
func (a clustersAPI) record(ctx context.Context, id ClusterID) (ClusterRow, *beehive.Object[ClusterRuntimeSpec, ClusterStatus], error) {
	row, ok, err := getCluster(ctx, a.s.store.Stmts(), id)
	if err != nil {
		return ClusterRow{}, nil, err
	}
	if !ok {
		return ClusterRow{}, nil, fmt.Errorf("read cluster %s: %w", id, ErrNotFound)
	}
	obj, err := a.s.runtimeObject(ctx, id)
	if err != nil {
		return ClusterRow{}, nil, err
	}
	return row, obj, nil
}

func (a clustersAPI) Get(ctx context.Context, id ClusterID) (*Cluster, error) {
	row, ok, err := getCluster(ctx, a.s.store.Stmts(), id)
	if err != nil {
		return nil, err
	}
	if !ok {
		// A caller holds ids from watch frames, so a row removed in between is an
		// ordinary race rather than a bad request.
		return nil, nil
	}
	obj, err := a.s.runtimeObject(ctx, id)
	if err != nil {
		return nil, err
	}
	return toCluster(row, obj), nil
}

// clusterSortKey is the label a list is ordered by: the display name the user set, or
// the kube-context a view renders in its place. The id is the last resort, for a row
// whose source has no natural name.
func clusterSortKey(row ClusterRow) string {
	if row.Name != nil && *row.Name != "" {
		return *row.Name
	}
	if row.SourceKey != nil {
		return *row.SourceKey
	}
	return string(row.ID)
}

// readClusters joins every row with its runtime object, in the order the list
// promises. The rows are read first: an object created after the read has a row this
// read did not see, and answers on the next; an object of a row removed since is
// just not joined.
func (s *service) readClusters(ctx context.Context) ([]*Cluster, error) {
	rows, err := listClusters(ctx, s.store.Stmts())
	if err != nil {
		return nil, err
	}
	objs, err := s.clusterClient.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list cluster runtime objects: %w", err)
	}
	byID := make(map[ClusterID]*beehive.Object[ClusterRuntimeSpec, ClusterStatus], len(objs))
	for _, obj := range objs {
		byID[clusterIDOf(obj)] = obj
	}

	slices.SortFunc(rows, func(a, b ClusterRow) int {
		return cmp.Or(
			cmp.Compare(clusterSortKey(a), clusterSortKey(b)),
			// Display names are not unique, so a total order needs one that is.
			cmp.Compare(a.ID, b.ID),
		)
	})
	clusters := make([]*Cluster, 0, len(rows))
	for _, row := range rows {
		clusters = append(clusters, toCluster(row, byID[row.ID]))
	}
	return clusters, nil
}

func (a clustersAPI) List(ctx context.Context) ([]*Cluster, error) {
	return a.s.readClusters(ctx)
}

// WatchCollectionByID is the list watch filtered to one id: a record that does not exist yet has
// nothing to watch on its own, and the same stream reports it arriving.
func (a clustersAPI) WatchCollectionByID(ctx context.Context, id ClusterID) (*Stream[ClusterCollectionWatchFrame], error) {
	list, err := a.WatchCollection(ctx)
	if err != nil {
		return nil, err
	}
	return NewStream(ctx, func(ctx context.Context, out chan<- ClusterCollectionWatchFrame) error {
		for frame := range list.Frames {
			if frame.Cluster != nil && frame.Cluster.ID != id {
				continue
			}
			if !sendFrame(ctx, out, frame) { // coverage:ignore only the consumer leaving while a frame is in flight between the two pumps
				return nil
			}
		}
		return list.Err()
	}), nil
}

// WatchCollection rereads and diffs on two signals, because the record has two sources: a
// rename moves a row and no object, a probe moves an object and no row, and a watcher
// needs both. Both subscriptions are taken before the first read, so a change landing
// between the two is reported rather than dropped. The reread is tens of rows per
// signal and each hub coalesces a burst, so there is no incremental path. Records are
// compared as JSON bytes — they hold slices, so == will not do.
//
// Only the Deleted is promised for a record the watcher has seen: a mark and a removal
// landing before one reread surface as the Deleted alone, and a row created and
// removed between two reads produces no frame.
func (a clustersAPI) WatchCollection(ctx context.Context) (*Stream[ClusterCollectionWatchFrame], error) {
	rows := a.s.db.ListenForChanges(appdb.KeyClusters)
	objs, err := a.s.clusterClient.WatchList(ctx)
	if err != nil {
		rows.Close()
		return nil, fmt.Errorf("watch cluster runtime objects: %w", err)
	}

	return NewStream(ctx, func(ctx context.Context, out chan<- ClusterCollectionWatchFrame) error {
		defer rows.Close()
		fold := deltafold.New(
			func(c *Cluster) ClusterID { return c.ID },
			clustersEqual,
			func(t CollectionWatchFrameType, c *Cluster) ClusterCollectionWatchFrame {
				return ClusterCollectionWatchFrame{Type: t, Cluster: c}
			},
		)
		clusters, err := a.s.readClusters(ctx)
		if err != nil {
			return err
		}
		if !fold.Snapshot(ctx, out, clusters) {
			return nil
		}
		for {
			select {
			case <-ctx.Done():
				return nil
			case _, ok := <-rows.Chan():
				if !ok {
					return nil
				}
			case _, ok := <-objs.Changes:
				if !ok {
					return objs.Err()
				}
			}
			clusters, err := a.s.readClusters(ctx)
			if err != nil {
				return err
			}
			if !fold.Diff(ctx, out, clusters) {
				return nil
			}
		}
	}), nil
}

// clustersEqual compares two served records by their JSON, the one comparison that
// reaches into the slices they hold. A failed marshal reads as a change, which costs
// a frame and never hides one.
func clustersEqual(a, b *Cluster) bool {
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ab, bb)
}

// WatchScheduleState streams when the cluster's context is next dialed: the current value,
// then a new one on every pass.
//
// The cadence is the pool's, never beehive's. A cluster reconcile is not requeued to
// retry a connection — the probes carry their own backoff and a pass only folds what they
// found — so the record's beehive schedule is empty and a countdown read off it would
// never move.
//
// The claim is this stream's own, released when ctx ends. It is refcounted alongside the
// one clusterController holds, so watching costs no extra dial; a record the connection
// surface refuses has no cadence to report and errors here.
func (a clustersAPI) WatchScheduleState(ctx context.Context, id ClusterID) (<-chan Schedule, error) {
	lease, err := a.s.AcquireConnection(ctx, id)
	if err != nil {
		return nil, err
	}
	// The watch's first value is the claim's state now; each pass after it is another.
	states := lease.WatchState()

	out := make(chan Schedule)
	go func() {
		defer close(out)
		defer states.Close()
		defer lease.Release()

		sent := false
		for {
			st, err := states.RecvContext(ctx)
			if err != nil {
				return
			}
			sched := clusterSchedule(st)
			// A state watch says nothing before its first measurement: the zero schedule of a
			// claim whose first pass has not landed is not "nothing is scheduled".
			if sent || sched.NextRequeueAt != nil || sched.Probing {
				if !sendFrame(ctx, out, sched) {
					return
				}
				sent = true
			}
		}
	}()
	return out, nil
}

// clusterSchedule projects the connection probe's cadence into the state watch.
//
// The connection alone, of the five: this is what "when do we next try to reach this
// cluster" means, and it is the only one a caller can act on — a retry re-dials. The
// other four run on their own clocks (readiness every 30s, the rest every 5-10m), so
// folding them in would count down to whichever happened to be due next.
//
// A zero ScheduledAt is a suspended probe: nothing is due and the last answer stands. A run in
// flight keeps the time it was dispatched for, which is the current reconcile rather than the
// next one — so probing carries no countdown.
func clusterSchedule(st kubeconn.State) Schedule {
	sched := Schedule{Probing: st.Connection.InFlight(), Failures: st.Connection.Failures}
	if at := st.Connection.NextAttempt.ScheduledAt; !sched.Probing && !at.IsZero() {
		sched.NextRequeueAt = &at
	}
	return sched
}

func (a clustersAPI) SetEnabled(ctx context.Context, id ClusterID, enabled bool) (*Cluster, error) {
	return a.setToggle(ctx, stmtSetClusterEnabled, id, enabled)
}

func (a clustersAPI) SetSyncEnabled(ctx context.Context, id ClusterID, enabled bool) (*Cluster, error) {
	return a.setToggle(ctx, stmtSetClusterSyncEnabled, id, enabled)
}

func (a clustersAPI) SetMonitoringEnabled(ctx context.Context, id ClusterID, enabled bool) (*Cluster, error) {
	return a.setToggle(ctx, stmtSetClusterMonitoringEnabled, id, enabled)
}

func (a clustersAPI) SetDefault(ctx context.Context, id ClusterID, isDefault bool) (*Cluster, error) {
	obj, err := a.s.runtimeObject(ctx, id)
	if err != nil {
		return nil, err
	}
	// A window resolves only the kubeconfig's enabled contexts, so a default it cannot
	// find would be a choice that does nothing.
	canBeDefault := func(row ClusterRow) bool {
		return row.Enabled && sourceDeclares(a.s.kubeconfigSvc, row, obj)
	}
	var (
		row   ClusterRow
		wrote bool
	)
	err = a.s.store.InTx(ctx, func(st stmts) error {
		var err error
		row, wrote, err = setClusterDefault(ctx, st, id, isDefault, normalizeTime(a.s.now()), canBeDefault)
		return err
	})
	if err != nil {
		return nil, err
	}
	if wrote {
		a.s.db.Notify(appdb.KeyClusters)
	}
	return toCluster(row, obj), nil
}

// setToggle writes one toggle to the row, wakes the watchers and the mirror, and
// answers with the record as written. The runtime object follows on the mirror's
// pass, so the record served here carries the status the object held before it.
func (a clustersAPI) setToggle(ctx context.Context, stmt stmtID, id ClusterID, on bool) (*Cluster, error) {
	row, err := setClusterToggle(ctx, a.s.store.Stmts(), stmt, id, on, normalizeTime(a.s.now()))
	if err != nil {
		return nil, err
	}
	a.s.db.Notify(appdb.KeyClusters)
	obj, err := a.s.runtimeObject(ctx, id)
	if err != nil {
		return nil, err
	}
	return toCluster(row, obj), nil
}

// Delete marks the row and returns: the mirror tears the runtime object and its
// caches down, the chat service sweeps the chats, and the mirror removes the row once
// both are done. A row already gone or already marked is the outcome the caller asked
// for, so neither is an error — and both are checked ahead of the source refusal, so
// a repeat of a delete that went through is not refused by a context that returned.
//
// A row its source still declares is refused with ErrDeclaredBySource rather than
// marked: the importer would re-create it under a fresh id once the row went, so the
// delete would read as succeeding and then undo itself.
func (a clustersAPI) Delete(ctx context.Context, id ClusterID) error {
	row, ok, err := getCluster(ctx, a.s.store.Stmts(), id)
	if err != nil {
		return err
	}
	if !ok || row.DeleteRequestedAt != nil {
		return nil
	}
	obj, err := a.s.runtimeObject(ctx, id)
	if err != nil {
		return err
	}
	if sourceDeclares(a.s.kubeconfigSvc, row, obj) {
		return fmt.Errorf("delete cluster %s: %w", id, ErrDeclaredBySource)
	}
	if _, err := markCluster(ctx, a.s.store.Stmts(), id, normalizeTime(a.s.now())); err != nil {
		return err
	}
	a.s.db.Notify(appdb.KeyClusters)
	return nil
}

// sourceDeclares reports whether the row's source still lists it. A row from no
// source is nobody's to declare.
//
// The kubeconfig itself is the answer whenever it has been read. While the file is
// unread the fallback is the runtime object's own observation, a cached view of the
// file — and a row with neither is refused: refusing is recoverable, since the caller
// retries a moment later, where allowing is not — the importer re-creates the context
// under a fresh id and the user's toggles are gone with the old one.
func sourceDeclares(cfgSvc kubeconfigService, row ClusterRow, obj *beehive.Object[ClusterRuntimeSpec, ClusterStatus]) bool {
	if row.Source != SourceKubeconfig || row.SourceKey == nil {
		return false
	}
	if cfg, loaded := cfgSvc.Get(); loaded {
		_, ok := cfg.Contexts[*row.SourceKey]
		return ok
	}
	if observed := clusterStatus(obj).Source.Kubeconfig; observed != nil {
		return observed.IsPresent
	}
	return true
}

// startupRequeue paces a reconcile that arrived inside a startup ordering window:
// before the discovery anchors exist, since the bootstrap that creates them follows
// beehive in service.parts and its startup pass dispatches asynchronously, so a record
// stored by a previous process can reach a reconcile first. It paces the unread-config
// guards below too. Bounded by the app's own startup, never by anything a user does.
const startupRequeue = time.Second

// clusterProbeInterval paces the re-probe, registered as the kind's individual pass so
// each record is timed from the end of its own last one and a fleet spreads itself out
// rather than dialing in one burst.
const clusterProbeInterval = 5 * time.Minute

// clusterController reconciles a tracked cluster: it observes what the kubeconfig says
// about the record's context, reports what connecting with that context's credentials
// revealed, and creates the ClusterCache for the identity it found.
//
// The dial is not in the pass. A claim reports what its last probe found, so a record
// whose probe is still owed reports Connecting and comes back when the probe publishes
// rather than blocking here.
//
// The kubeconfig service it reads to observe a context's presence is the app's,
// shared with every other reader, so it is a dependency rather than machinery.
type clusterController struct {
	lifecycle.None

	// Every kind's client, not just this one's: a cluster creates the ClusterCache
	// children it owns.
	deps

	// leases is this controller's own: a claim is held across passes, so it cannot live
	// in deps beside the services every kind shares.
	leases clusterLeases
}

// clusterLeases is the claims this controller holds, one per cluster it keeps connected.
// A claim is what arms the probe behind a cluster, so what this holds is which clusters
// are probed at all — and it outlives the pass that took it, which is why the controller
// owns it and a pass only ensures it.
//
// Keyed by ClusterID, since the lifetime being modelled is the record's: enabled,
// disabled, deleted. Storage only, with a usable zero value; the methods are the
// controller's, so the pool is reached through deps like every other service and there is
// one place it can be wired wrong.
//
// A claim names a context, and a row's context never changes, so a held claim stays
// the right one for as long as the record exists. Credentials moving under that
// context is the pool's to notice, not this map's.
//
// These are the controller's own claims, not every claim on a cluster. A caller reaching
// the boundary takes one of its own and releases it when done — the pool refcounts, so a
// log tail ending must not drop the claim keeping this cluster probed. The claims are
// process state a restart invalidates and the store cannot report as owed, which is what
// the Cluster kind's startup pass is for.
type clusterLeases struct {
	// Passes run per object and concurrently, and Close races whatever is mid-pass.
	mu   sync.Mutex
	held map[ClusterID]heldLease
	// byContext is the reverse index the kubeconn trigger reads: a probe names its
	// context, and the record to wake is the one holding a claim on it. A context is
	// claimed by one row at a time, since the importer claims each once.
	byContext map[string]ClusterID
}

// heldLease is a claim and the context it names, so dropping it can drop the index
// entry too.
type heldLease struct {
	kubeconn.Lease
	contextName string
}

// clusterFor is the record holding a claim on contextName, for the trigger. False
// for a context no record is probed under, which nothing needs waking for.
func (l *clusterLeases) clusterFor(contextName string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	id, ok := l.byContext[contextName]
	return string(id), ok
}

// ensureLease returns the cluster's claim, taking one if it has none.
func (c *clusterController) ensureLease(id ClusterID, contextName string) kubeconn.Lease {
	c.leases.mu.Lock()
	defer c.leases.mu.Unlock()

	if held, ok := c.leases.held[id]; ok {
		return held
	}

	lease := c.kubeconnSvc.Acquire(contextName)
	if c.leases.held == nil {
		c.leases.held = map[ClusterID]heldLease{}
		c.leases.byContext = map[string]ClusterID{}
	}
	c.leases.held[id] = heldLease{Lease: lease, contextName: contextName}
	c.leases.byContext[contextName] = id
	return lease
}

// dropLease releases the cluster's claim, if it holds one. Idempotent: a pass that finds a
// cluster disabled calls it without knowing whether one was ever taken.
func (c *clusterController) dropLease(id ClusterID) {
	c.leases.mu.Lock()
	defer c.leases.mu.Unlock()

	held, ok := c.leases.held[id]
	if !ok {
		return
	}
	delete(c.leases.held, id)
	delete(c.leases.byContext, held.contextName)
	held.Release()
}

func (c *clusterController) Reconcile(
	ctx context.Context,
	client beehive.ControllerClient[ClusterStatus],
	obj *beehive.Object[ClusterRuntimeSpec, ClusterStatus],
) beehive.Result {
	// Nothing to observe for a record on its way out, and no finalizer to clear:
	// beehive collects it either way. The claim is dropped rather than left for the
	// collection, so a cluster stops being probed the moment its deletion is asked for.
	if obj.DeletionRequestedAt != nil {
		c.dropLease(clusterIDOf(obj))
		return beehive.Settled()
	}

	// Unreachable through the composition root, which starts the kubeconfig service
	// ahead of this one and reads synchronously. It stays because the pre-read config
	// is empty: observing it would report every present context absent and wake the
	// kind's watches for a flap, so the guard is what keeps the pass correct if that
	// ordering ever stops holding.
	cfg, loaded := c.kubeconfigSvc.Get()
	if !loaded {
		return beehive.Unsettled().RequeueAfter(startupRequeue)
	}

	// The observation below reads the kubeconfig service rather than this object, so
	// beehive cannot know when it goes stale. The edge onto the source anchor is what
	// turns one status write there into a pass here; without it a departed context
	// stays marked present until something unrelated wakes the record. Beehive records
	// nothing when the edge is already there, so every later pass is free.
	contextName := kubeconfigContextOf(obj.Spec)
	if contextName != "" {
		src, err := c.sourceClient.GetByName(ctx, ClusterSourceNameKubeconfig)
		if errors.Is(err, beehive.ErrNotFound) {
			// Not a failure: the bootstrap creates the anchors after beehive starts, and
			// this record predates the process. Failing here would drop every stored
			// record into backoff on every boot, and skip the observation with it.
			return beehive.Unsettled().RequeueAfter(startupRequeue)
		}
		if err != nil {
			return beehive.Fail(fmt.Errorf("read kubeconfig cluster source: %w", err))
		}
		if err := client.AddDependency(ctx, src.ID); err != nil {
			return beehive.Fail(fmt.Errorf("depend cluster %d on its source: %w", obj.ID, err))
		}
	}

	stored := clusterStatus(obj)
	status := stored
	status.Source.Kubeconfig = observeKubeconfig(cfg, contextName, stored.Source.Kubeconfig)

	// A record from a source with no credentials to resolve gets no conditions at all,
	// rather than verdicts no probe produced.
	var conds []Condition
	finding := c.reconcileConnection(obj)
	if finding != nil {
		conds = []Condition{observeConnected(finding), observeIdentified(finding)}
		status = foldState(status, finding.observed)
	}

	// Grouped so a watcher never sees the status without the conditions that explain it.
	// Every write is unconditional: beehive compares each against what is stored and
	// reaches it only when it differs, so a pass that observed nothing new costs a
	// marshal rather than a transaction.
	if err := client.Within(ctx, func(ctx context.Context) error {
		if err := client.UpdateStatus(ctx, status); err != nil {
			return fmt.Errorf("update status: %w", err)
		}
		for _, cond := range conds {
			if err := client.SetCondition(ctx, cond); err != nil {
				return fmt.Errorf("set condition %s: %w", cond.Type, err)
			}
		}
		return nil
	}); err != nil {
		return beehive.Fail(fmt.Errorf("report cluster %d: %w", obj.ID, err))
	}

	if err := logConnectionVerdict(ctx, client, conds); err != nil {
		return beehive.Fail(err)
	}

	// After the write and outside the skip above, both deliberately: this pass may have
	// just learned the identity the cache is named for, and a record that observes
	// nothing new can still be missing the cache it already calls for.
	if err := ensureCache(ctx, c.cacheClient, obj, status); err != nil {
		return beehive.Fail(err)
	}
	// Settling is this pass's to report: an object left unsettled is re-dispatched by
	// beehive's owed pass forever.
	return beehive.Settled()
}

// connectionFinding is what one pass found about a cluster's connection. Separated from the
// verdicts so the claim's lifetime happens once while each condition reads the same finding.
//
// observed is what the claim read, nil when there is no claim to read it off — the three
// findings this package makes before the pool is involved: the record is switched off, its
// context left the file, or its credentials will not resolve. The server still exists in all
// three; what is missing is our observation of it. inactive marks the first two and takes
// precedence, since the pool cannot see a choice the user made.
type connectionFinding struct {
	inactive bool
	// reason and message are Connected's. Identified derives its own, since a server
	// nothing reached is not unidentifiable, only unassessed.
	reason   string
	message  string
	observed *kubeconn.State
}

// reconcileConnection brings this cluster's claim in line with what its record asks for —
// taken while it should be connected, dropped otherwise — and reports what it found. The
// one place the pass touches the pool, so the verdicts below stay pure.
//
// Only a kubeconfig-sourced record has credentials to resolve. Another source's are not
// this pass's to guess at, so it reports nothing at all rather than a finding no probe
// produced — and holds no claim on its behalf either.
func (c *clusterController) reconcileConnection(obj *beehive.Object[ClusterRuntimeSpec, ClusterStatus]) *connectionFinding {
	id := clusterIDOf(obj)

	contextName := kubeconfigContextOf(obj.Spec)
	if contextName == "" {
		c.dropLease(id)
		return nil
	}

	if !obj.Spec.Enabled {
		// Releasing is what stops the probe: a disabled cluster costs nothing to keep as a
		// record, and should cost no dial.
		c.dropLease(id)
		return &connectionFinding{inactive: true, reason: ReasonInactive, message: "cluster is disabled"}
	}

	// The claim is what arms the probe behind this context. Held across passes, so ensuring
	// it is all a pass does; what the probe found is read off it below. Claiming cannot fail:
	// whether the context resolves, and to what, is the pool's to find out and report through
	// State rather than a reason to refuse.
	observed := c.ensureLease(id, contextName).State()
	finding := &connectionFinding{observed: &observed}
	switch observed.Phase() {
	case kubeconn.PhasePending:
		// Claimed and not yet answered. The signal a probe publishes is what brings this
		// record back, with the kind's cadence behind it covering a signal that went missing.
		finding.reason, finding.message = ReasonConnecting, "probe pending"
	case kubeconn.PhaseUnreached:
		// The server would not answer. Left to the probe's own cadence rather than failing
		// the pass, which has nothing to retry. The reason is the probe's own class of
		// failure (Unreachable, TLSInvalid, Unauthorized, …), since each has its own remedy.
		attempt := observed.Connection.LastAttempt
		finding.reason = cmp.Or(string(attempt.Reason), ReasonProbeFailed)
		finding.message = attempt.Message
	default:
		finding.reason = ReasonConnected
	}
	return finding
}

// observeConnected reports whether the last probe reached the API server. It carries the
// reason the pass got only this far, which is why it reads its finding rather than
// deriving one.
func observeConnected(finding *connectionFinding) Condition {
	status := ConditionFalse
	switch {
	case finding.inactive, finding.observed == nil:
		// False, with the reason the finding carries.
	case finding.observed.Phase() == kubeconn.PhasePending:
		status = ConditionUnknown
	case finding.observed.Phase() == kubeconn.PhaseProbed:
		status = ConditionTrue
	}
	return LiveCondition(ConditionConnected, status, finding.reason, finding.message)
}

// observeIdentified reports whether the probe could tell which cluster answered.
//
// Reaching a server needs no authorization and naming it does, so this fails on its own —
// and when it does, no cache is ever created, since a cache is named for the identity it
// mirrors.
func observeIdentified(finding *connectionFinding) Condition {
	switch {
	case finding.inactive:
		return LiveCondition(ConditionIdentified, ConditionFalse, ReasonInactive, finding.message)
	case finding.observed == nil, finding.observed.Phase() == kubeconn.PhaseUnreached:
		return LiveCondition(ConditionIdentified, ConditionFalse, ReasonNoConnection, "")
	case finding.observed.Phase() == kubeconn.PhasePending:
		return LiveCondition(ConditionIdentified, ConditionUnknown, ReasonConnecting, "probe pending")
	case !finding.observed.ServerUID.OK():
		return LiveCondition(ConditionIdentified, ConditionFalse, ReasonUIDUnreadable,
			finding.observed.ServerUID.LastAttempt.Message)
	default:
		return LiveCondition(ConditionIdentified, ConditionTrue, ReasonIdentified, "")
	}
}

// logConnectionVerdict records this pass's verdict on the cluster's own timeline, which is
// what the UI's attempt list reads. Every pass, because repeating a run's
// (Category, Type, Reason) extends that run rather than appending — so a settled cluster
// costs nothing and a flapping one costs a row per transition.
//
// A pass that produced no conditions produced no verdict either, and writes nothing.
func logConnectionVerdict(
	ctx context.Context,
	client beehive.ControllerClient[ClusterStatus],
	conds []Condition,
) error {
	if len(conds) == 0 {
		return nil
	}
	cond := blockingCondition(conds)
	if err := client.AddEvent(ctx, beehive.EventSpec{
		Category: ConnectionEventCategory,
		Type:     connectionEventType(cond),
		Reason:   cond.Reason,
		Message:  cond.Message,
	}); err != nil {
		return fmt.Errorf("log cluster connection: %w", err)
	}
	return nil
}

// blockingCondition picks the axis worth reporting: the first that is not True, since
// that is what is stopping the cluster. Reaching a server and naming it fail separately,
// and a cluster that connects but cannot be identified never gets a cache — reporting the
// connection's success there would say it is fine while nothing syncs.
func blockingCondition(conds []Condition) Condition {
	for _, cond := range conds {
		if cond.Status != ConditionTrue {
			return cond
		}
	}
	return conds[0]
}

// connectionEventType grades a verdict. Being switched off is a choice rather than a
// fault, and an unanswered probe has found nothing to warn about yet.
func connectionEventType(cond Condition) beehive.EventType {
	if cond.Status == ConditionFalse && cond.Reason != ReasonInactive {
		return beehive.EventWarning
	}
	return beehive.EventNormal
}

// foldState folds what the pool knows into the status that serves it.
//
// The pool already retains a probe's last answer through a failure, so this copies rather
// than deciding what to keep — and a probe that has never answered leaves its field alone,
// which is what stops a first pass from clearing a UID a live cache is named for. The
// record's copy is the durable one: a restart empties the pool's.
//
// Only the probes' values land here, never their timing: a status that moved every pass
// would re-emit the record to every watcher on every cycle.
func foldState(status ClusterStatus, known *kubeconn.State) ClusterStatus {
	if known == nil {
		return status
	}
	if o := known.Connection; o.Known() {
		status.Server.Endpoint = &o.Value
	}
	if o := known.ServerUID; o.Known() {
		status.Server.UID = &o.Value
	}
	if o := known.ServerVersion; o.Known() {
		v := o.Value.GitVersion
		status.Server.Version = &v
	}
	if o := known.Principal; o.Known() {
		u := o.Value.Username
		status.Principal.Username = &u
		status.Principal.Groups = slices.Sorted(slices.Values(o.Value.Groups))
	}
	return status
}

// ensureCache gives the cluster a mirror slot for the identity this pass observed. A
// cluster that has never connected has none to mirror, and a cache named for the empty
// UID is one CacheIsActive matches against nothing.
func ensureCache(ctx context.Context, caches beehive.Client[ClusterCacheSpec, ClusterCacheStatus], cluster *beehive.Object[ClusterRuntimeSpec, ClusterStatus], status ClusterStatus) error {
	if status.Server.UID == nil || *status.Server.UID == "" {
		return nil
	}
	return ensureClusterCache(ctx, caches, cluster, *status.Server.UID)
}

// observeKubeconfig returns what cfg says about the record's context, folded over
// the previous observation. An empty context is any other source, whose observation
// is this one's to return unchanged.
//
// A departed context keeps its last-known cluster and user names with
// IsPresent=false, which is what keeps an orphaned record identifiable rather than
// blank.
func observeKubeconfig(cfg *api.Config, contextName string, prev *ClusterStatusSourceKubeconfig) *ClusterStatusSourceKubeconfig {
	if contextName == "" {
		return prev
	}

	if kctx := cfg.Contexts[contextName]; kctx != nil {
		observed := &ClusterStatusSourceKubeconfig{
			Cluster:   ClusterStatusSourceKubeconfigCluster{Name: kctx.Cluster},
			User:      ClusterStatusSourceKubeconfigUser{Name: kctx.AuthInfo},
			IsPresent: true,
			IsDefault: contextName == cfg.CurrentContext,
		}
		if c := cfg.Clusters[kctx.Cluster]; c != nil {
			observed.Cluster.Entry = &ClusterStatusSourceKubeconfigClusterEntry{
				Server:                  c.Server,
				InsecureSkipTLSVerify:   c.InsecureSkipTLSVerify,
				HasCertificateAuthority: c.CertificateAuthority != "" || len(c.CertificateAuthorityData) > 0,
			}
		}
		if u := cfg.AuthInfos[kctx.AuthInfo]; u != nil {
			observed.User.HasClientCertificate = u.ClientCertificate != "" || len(u.ClientCertificateData) > 0
		}
		return observed
	}

	observed := ClusterStatusSourceKubeconfig{}
	if prev != nil {
		observed = *prev
	}
	observed.IsPresent = false
	observed.IsDefault = false
	return &observed
}
