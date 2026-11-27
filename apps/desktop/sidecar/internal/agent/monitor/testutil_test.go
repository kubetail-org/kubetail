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

package monitor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/clustertest"
)

// fakeService is the cluster service as the monitor reads it: the clusters it lists,
// and one cache whose statements run over a real store, judged by health.
type fakeService struct {
	cluster.Service
	store    *clustertest.Store
	health   cluster.ClusterCacheHealth
	readErr  error
	clusters []*cluster.Cluster
	// failIn fails each statement whose text holds it.
	failIn string
	// serverUID is the server the active cache mirrors.
	serverUID string
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
	fakeCachedData struct {
		cluster.CachedData
		*fakeService
	}
)

func (f *fakeService) Clusters() cluster.Clusters     { return fakeClusters{fakeService: f} }
func (f *fakeService) Caches() cluster.Caches         { return fakeCaches{fakeService: f} }
func (f *fakeService) CachedData() cluster.CachedData { return fakeCachedData{fakeService: f} }

func (f fakeClusters) List(context.Context) ([]*cluster.Cluster, error) { return f.clusters, nil }

func (f fakeClusters) ReadActive(ctx context.Context, _ cluster.ClusterID, read func(context.Context, cluster.ActiveCluster) error) error {
	if f.readErr != nil {
		return f.readErr
	}
	cache := &cluster.ClusterCache{RecordMeta: cluster.RecordMeta{ID: 1}, Spec: cluster.ClusterCacheSpec{ServerUID: f.serverUID}}
	return read(ctx, cluster.ActiveCluster{Cluster: &cluster.Cluster{}, Cache: cache})
}

func (f fakeCaches) Health(context.Context, cluster.ClusterID, cluster.ClusterCacheID) (cluster.ClusterCacheHealth, bool, error) {
	return f.health, true, nil
}

func (f fakeCachedData) ReadActive(ctx context.Context, id cluster.ClusterID, maxRows, maxBytes int, read func(cluster.CacheReading, cluster.ActiveCluster) error) error {
	return cluster.ReadActiveCache(ctx, f.fakeService, id, maxRows, maxBytes, read)
}

func (f fakeCachedData) ReadOwn(ctx context.Context, _ cluster.ClusterID, _ cluster.ClusterCacheID, read func(cluster.OwnQuery) error) (bool, error) {
	return true, f.store.Read(ctx, func(query cluster.OwnQuery) error {
		return read(func(sql string, maxRows, maxBytes int, args ...any) (cluster.ClusterCachedDataQueryResult, error) {
			if f.failIn != "" && strings.Contains(sql, f.failIn) {
				return cluster.ClusterCachedDataQueryResult{}, errors.New("statement failed")
			}
			return query(sql, maxRows, maxBytes, args...)
		})
	})
}

// watching is a service over an empty store, of server "a", whose every kind is
// Watching.
func watching(t *testing.T) *fakeService {
	t.Helper()
	return &fakeService{store: clustertest.NewStore(t), health: cluster.ClusterCacheHealth{Reason: "Watching"}, serverUID: "a"}
}

func (f *fakeService) apply(t *testing.T, objects ...map[string]any) {
	t.Helper()
	for _, o := range objects {
		f.store.Apply(t, o)
	}
}

// created is when every object a builder makes was created.
var created = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// object is a body of kind named name in namespace ns, its uid derived from both.
func object(apiVersion, kind, ns, name string) map[string]any {
	meta := map[string]any{"name": name, "uid": kind + "/" + ns + "/" + name, "creationTimestamp": created.Format(time.RFC3339)}
	if ns != "" {
		meta["namespace"] = ns
	}
	return map[string]any{"apiVersion": apiVersion, "kind": kind, "metadata": meta}
}

// ownedBy makes owner o's controller.
func ownedBy(o, owner map[string]any) map[string]any {
	ownerMeta := owner["metadata"].(map[string]any)
	o["metadata"].(map[string]any)["ownerReferences"] = []any{map[string]any{
		"apiVersion": owner["apiVersion"], "kind": owner["kind"], "name": ownerMeta["name"], "uid": ownerMeta["uid"], "controller": true,
	}}
	return o
}

func withStatus(o map[string]any, status map[string]any) map[string]any {
	o["status"] = status
	return o
}

// pod is a Pod in phase whose containers are named for states, one each:
// "running", or the reason a container waits for.
func pod(ns, name, phase string, states ...string) map[string]any {
	var specs, statuses []any
	for i, state := range states {
		cname := "c" + string(rune('0'+i))
		specs = append(specs, map[string]any{"name": cname, "image": "img"})
		st := map[string]any{"running": map[string]any{}}
		if state != "running" {
			st = map[string]any{"waiting": map[string]any{"reason": state}}
		}
		statuses = append(statuses, map[string]any{"name": cname, "ready": state == "running", "state": st})
	}
	p := object("v1", "Pod", ns, name)
	p["spec"] = map[string]any{"containers": specs}
	status := map[string]any{"phase": phase}
	if len(statuses) > 0 {
		status["containerStatuses"] = statuses
	}
	return withStatus(p, status)
}

// node is a Node whose Ready condition has status ready.
func node(name, ready string) map[string]any {
	return withStatus(object("v1", "Node", "", name), map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": ready}}})
}

// deployment is a Deployment in prod wanting 3 replicas, ready of them ready, with
// its Progressing condition.
func deployment(name string, ready int, progressing, reason string) map[string]any {
	d := object("apps/v1", "Deployment", "prod", name)
	d["spec"] = map[string]any{"replicas": int64(3)}
	return withStatus(d, map[string]any{"readyReplicas": int64(ready), "conditions": []any{
		map[string]any{"type": "Progressing", "status": progressing, "reason": reason},
	}})
}

// youngPod is pod created a minute before readNow reads.
func youngPod(ns, name, phase string, states ...string) map[string]any {
	p := pod(ns, name, phase, states...)
	p["metadata"].(map[string]any)["creationTimestamp"] = created.Add(59 * time.Minute).Format(time.RFC3339)
	return p
}

// readNow is a reading with production bounds, at a time well past every window.
func readNow(t *testing.T, f *fakeService) reading { return readWith(t, f, 50_000) }

// readWith is readNow with each answer cut at maxRows.
func readWith(t *testing.T, f *fakeService, maxRows int) reading {
	t.Helper()
	r, ok, err := newReader(f.CachedData(), maxRows, 16<<20).read(t.Context(), "1")
	if err != nil || !ok {
		t.Fatalf("read: ok %v, err %v", ok, err)
	}
	return r
}

func pairOf(group, kind, ns, name string, signal Signal) Pair {
	return Pair{Subject: Subject{Group: group, Kind: kind, Namespace: ns, Name: name}, Signal: signal}
}
