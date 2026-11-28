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

package kubesignals

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/clustertest"
)

// fakeService is the cluster service as the integration reads it: its clusters,
// one active cache with the health a test sets, its statements run over a real
// store, and each statement whose text holds cut answered as cut at its bound.
type fakeService struct {
	cluster.Service
	clusters []*cluster.Cluster
	getErr   error
	readErr  error
	health   cluster.ClusterCacheHealth
	store    *clustertest.Store
	cut      string
	queryErr error // every statement's answer, when set
}

type (
	fakeClusters struct {
		cluster.Clusters
		*fakeService
	}
	fakeCaches struct {
		cluster.Caches
		*fakeService
	}
	fakeData struct {
		cluster.CachedData
		*fakeService
	}
)

func (f *fakeService) Clusters() cluster.Clusters     { return fakeClusters{fakeService: f} }
func (f *fakeService) Caches() cluster.Caches         { return fakeCaches{fakeService: f} }
func (f *fakeService) CachedData() cluster.CachedData { return fakeData{fakeService: f} }

func (f fakeClusters) List(context.Context) ([]*cluster.Cluster, error) { return f.clusters, f.getErr }

func (f fakeClusters) Get(_ context.Context, id cluster.ClusterID) (*cluster.Cluster, error) {
	for _, c := range f.clusters {
		if c.ID == id {
			return c, f.getErr
		}
	}
	return nil, f.getErr
}

func (f fakeClusters) ReadActive(ctx context.Context, id cluster.ClusterID, read func(context.Context, cluster.ActiveCluster) error) error {
	if f.readErr != nil {
		return f.readErr
	}
	c, _ := f.Get(ctx, id)
	return read(ctx, cluster.ActiveCluster{Cluster: c, Cache: &cluster.ClusterCache{
		RecordMeta: cluster.RecordMeta{ID: 11}, Spec: cluster.ClusterCacheSpec{ServerUID: "server-1", ClusterID: id},
	}})
}

func (f fakeCaches) Health(context.Context, cluster.ClusterID, cluster.ClusterCacheID) (cluster.ClusterCacheHealth, bool, error) {
	return f.health, true, nil
}

func (f fakeData) ReadActive(ctx context.Context, id cluster.ClusterID, maxRows, maxBytes int, read func(cluster.CacheReading, cluster.ActiveCluster) error) error {
	return cluster.ReadActiveCache(ctx, f.fakeService, id, maxRows, maxBytes, read)
}

func (f fakeData) ReadOwn(ctx context.Context, _ cluster.ClusterID, _ cluster.ClusterCacheID, read func(cluster.OwnQuery) error) (bool, error) {
	return true, f.store.Read(ctx, func(query cluster.OwnQuery) error {
		return read(func(sql string, maxRows, maxBytes int, args ...any) (cluster.ClusterCachedDataQueryResult, error) {
			if f.queryErr != nil {
				return cluster.ClusterCachedDataQueryResult{}, f.queryErr
			}
			res, err := query(sql, maxRows, maxBytes, args...)
			if f.cut != "" && strings.Contains(sql, f.cut) {
				res.More = true
			}
			return res, err
		})
	})
}

// watching is the health of a cache watching every kind the signals require.
func watching() cluster.ClusterCacheHealth {
	return cluster.ClusterCacheHealth{
		Status: cluster.ConditionTrue, Reason: "Watching",
		WatchingKindRefs: []cluster.KindRef{kindDaemonSet, kindDeployment, kindReplicaSet, kindStatefulSet, kindNode, kindPod},
	}
}

// enabledCluster is cluster "c1", enabled and syncing.
func enabledCluster() *cluster.Cluster {
	return &cluster.Cluster{ID: "c1", Spec: cluster.ClusterSpec{Name: new("prod"), Enabled: true, SyncEnabled: true}}
}

// newFake is a watching cluster c1 over a store holding objects.
func newFake(t testing.TB, objects ...map[string]any) *fakeService {
	t.Helper()
	store := clustertest.NewStore(t)
	for _, o := range objects {
		store.Apply(t, o)
	}
	return &fakeService{clusters: []*cluster.Cluster{enabledCluster()}, health: watching(), store: store}
}

// readingOf is one reading of c1 through the integration.
func readingOf(t *testing.T, f *fakeService) monitor.Reading {
	t.Helper()
	r, err := (&integration{db: appdbtest.Open(t), svc: f}).Read(t.Context(), monitor.Target{Key: "c1", Label: "prod"})
	require.NoError(t, err)
	return r
}

// observed is a reading's observations of signal, by subject key.
func observed(r monitor.Reading, signal string) map[string]monitor.Observation {
	out := map[string]monitor.Observation{}
	for _, o := range r.Observations {
		if o.Signal == signal {
			out[o.Subject.Key] = o
		}
	}
	return out
}

// meta is an object's metadata in default, owned by its controller when owner is set.
func meta(uid, name string, owner map[string]any) map[string]any {
	m := map[string]any{"uid": uid, "name": name, "namespace": "default", "generation": int64(1)}
	if owner != nil {
		m["ownerReferences"] = []any{owner}
	}
	return m
}

// controller is a controlling owner reference.
func controller(apiVersion, kind, name, uid string) map[string]any {
	return map[string]any{"apiVersion": apiVersion, "kind": kind, "name": name, "uid": uid, "controller": true}
}

// ctr is one container of a fixture Pod.
type ctr struct {
	name, image  string
	waiting      string // the waiting reason; "" runs
	restarts     int64
	lastReason   string
	lastExitCode int64
}

// pod is a Pod in phase on node-1 with containers.
func pod(uid, name string, owner map[string]any, phase string, containers ...ctr) map[string]any {
	var spec, statuses []any
	for _, c := range containers {
		spec = append(spec, map[string]any{"name": c.name, "image": c.image})
		st := map[string]any{"name": c.name, "image": c.image, "restartCount": c.restarts, "ready": c.waiting == ""}
		if c.waiting != "" {
			st["state"] = map[string]any{"waiting": map[string]any{"reason": c.waiting}}
		} else {
			st["state"] = map[string]any{"running": map[string]any{}}
		}
		if c.lastReason != "" {
			st["lastState"] = map[string]any{"terminated": map[string]any{"reason": c.lastReason, "exitCode": c.lastExitCode}}
		}
		statuses = append(statuses, st)
	}
	return map[string]any{
		"apiVersion": "v1", "kind": "Pod", "metadata": meta(uid, name, owner),
		"spec":   map[string]any{"nodeName": "node-1", "containers": spec},
		"status": map[string]any{"phase": phase, "containerStatuses": statuses},
	}
}

// crashing is a container in CrashLoopBackOff.
func crashing(name string) ctr {
	return ctr{name: name, image: name + ":1", waiting: "CrashLoopBackOff", restarts: 7, lastReason: "Error", lastExitCode: 1}
}

// running is a container that runs.
func running(name string) ctr { return ctr{name: name, image: name + ":1"} }

// webDeployment is Deployment web in default and its ReplicaSet, wanting replicas
// and holding ready of them, every one of its current revision.
func webDeployment(replicas, ready int64) []map[string]any {
	return []map[string]any{
		{
			"apiVersion": "apps/v1", "kind": "Deployment", "metadata": meta("dep-web", "web", nil),
			"spec":   map[string]any{"replicas": replicas},
			"status": map[string]any{"observedGeneration": int64(1), "replicas": replicas, "updatedReplicas": replicas, "readyReplicas": ready},
		},
		{
			"apiVersion": "apps/v1", "kind": "ReplicaSet",
			"metadata": meta("rs-web", "web-7d9f", controller("apps/v1", "Deployment", "web", "dep-web")),
			"spec":     map[string]any{"replicas": replicas}, "status": map[string]any{"readyReplicas": ready},
		},
	}
}

// underWeb is the owner reference onto web's ReplicaSet.
func underWeb() map[string]any { return controller("apps/v1", "ReplicaSet", "web-7d9f", "rs-web") }

// node is a Node whose Ready condition has status, reason and message.
func node(name, status, reason, message string) map[string]any {
	cond := map[string]any{"type": "Ready", "status": status}
	if reason != "" {
		cond["reason"] = reason
	}
	if message != "" {
		cond["message"] = message
	}
	return map[string]any{
		"apiVersion": "v1", "kind": "Node", "metadata": map[string]any{"uid": "node-" + name, "name": name},
		"status": map[string]any{"conditions": []any{cond}},
	}
}
