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

// Package logsinputtest is a cluster service for the log tools' tests: one cache
// whose statements run over a real store, and the objects a view's sources name.
package logsinputtest

import (
	"context"
	"maps"
	"strings"
	"sync"
	"testing"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/clustertest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
)

// Service is the cluster service as a log tool reads it: one reading handed to
// read, the active cache's health, and each statement's answer by what it asks.
type Service struct {
	cluster.Service
	Active      cluster.ActiveCluster
	ReadErr     error
	CacheHealth cluster.ClusterCacheHealth
	// Store answers each reading's statements; nil answers no cache.
	Store *clustertest.Store
	// QueryErr fails every statement, or with FailIn set, each whose SQL holds it.
	QueryErr error
	FailIn   string
	// Queried is every statement run, under mu: a follow reads the mirror on a
	// goroutine of its own.
	mu      sync.Mutex
	Queried []Queried
	// Conn is the connection AcquireConnection hands out, for a log service's reads.
	Conn *cluster.Connection
}

// Queried is one statement the fake ran, and the args bound to it.
type Queried struct {
	SQL  string
	Args []any
}

type (
	fakeClusters struct {
		cluster.Clusters
		*Service
	}
	fakeCaches struct {
		cluster.Caches
		*Service
	}
	fakeCachedData struct {
		cluster.CachedData
		*Service
	}
)

func (f *Service) Clusters() cluster.Clusters     { return fakeClusters{Service: f} }
func (f *Service) Caches() cluster.Caches         { return fakeCaches{Service: f} }
func (f *Service) CachedData() cluster.CachedData { return fakeCachedData{Service: f} }

func (f fakeClusters) ReadActive(ctx context.Context, _ cluster.ClusterID, read func(context.Context, cluster.ActiveCluster) error) error {
	if f.ReadErr != nil {
		return f.ReadErr
	}
	return read(ctx, f.Active)
}

func (f fakeCachedData) ReadActive(ctx context.Context, id cluster.ClusterID, serverUID string, maxRows, maxBytes int, read func(cluster.CacheReading, cluster.ActiveCluster) error) error {
	return cluster.ReadActiveCache(ctx, f.Service, id, serverUID, maxRows, maxBytes, read)
}

func (f fakeCaches) Health(context.Context, cluster.ClusterID, cluster.ClusterCacheID) (cluster.ClusterCacheHealth, bool, error) {
	return f.CacheHealth, true, nil
}

func (f fakeCachedData) ReadOwn(ctx context.Context, _ cluster.ClusterID, _ cluster.ClusterCacheID, read func(cluster.OwnQuery) error) (bool, error) {
	if f.Store == nil && f.QueryErr == nil {
		return false, nil
	}
	query := func(store cluster.OwnQuery) cluster.OwnQuery {
		return func(sql string, maxRows, maxBytes int, args ...any) (cluster.ClusterCachedDataQueryResult, error) {
			f.mu.Lock()
			f.Queried = append(f.Queried, Queried{sql, args})
			f.mu.Unlock()
			if f.QueryErr != nil && strings.Contains(sql, f.FailIn) {
				return cluster.ClusterCachedDataQueryResult{}, f.QueryErr
			}
			return store(sql, maxRows, maxBytes, args...)
		}
	}
	if f.Store == nil {
		return true, read(query(nil))
	}
	return true, f.Store.Read(ctx, func(store cluster.OwnQuery) error { return read(query(store)) })
}

// MirrorOf is a watching cluster whose statements run over a real store holding
// objects and mirroring pods, so a statement the store refuses fails the test.
func MirrorOf(t *testing.T, objects ...map[string]any) *Service {
	t.Helper()
	store := clustertest.NewStore(t)
	store.Mirror(t, "v1", "Pod")
	for _, o := range objects {
		store.Apply(t, o)
	}
	return Over(store)
}

// Over is a watching cluster whose statements run over store.
func Over(store *clustertest.Store) *Service {
	return &Service{
		Active:      cluster.ActiveCluster{Cluster: &cluster.Cluster{}, Cache: &cluster.ClusterCache{RecordMeta: cluster.RecordMeta{ID: 11}}},
		CacheHealth: cluster.ClusterCacheHealth{Reason: "Watching"},
		Store:       store,
	}
}

// Watching is a cluster whose cache is watching, holding WebappObjects.
func Watching(t *testing.T) *Service {
	t.Helper()
	return MirrorOf(t, WebappObjects()...)
}

// WebappObjects is a deployment webapp in prod whose template runs app and
// istio-proxy after init container setup, its replica set, and three pods under
// it running the same: webapp-7f9c and webapp-a with app restarted, and webapp-b.
func WebappObjects() []map[string]any {
	webappPod := func(name string, restarted bool) map[string]any {
		return PodOf(name, "", WebappReplicaSet(), Container{Name: "app", Restarted: restarted}, Container{Name: "istio-proxy"}, Container{Name: "setup", Init: true})
	}
	replicaSet := Meta("rs-webapp", "webapp-7d9f", "")
	replicaSet["ownerReferences"] = []any{controller("apps/v1", "Deployment", "webapp", "dep-webapp")}
	return []map[string]any{
		{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": Meta("dep-webapp", "webapp", ""), "spec": map[string]any{"template": Template("")}},
		{"apiVersion": "apps/v1", "kind": "ReplicaSet", "metadata": replicaSet},
		webappPod("webapp-7f9c", true),
		webappPod("webapp-a", true),
		webappPod("webapp-b", false),
	}
}

// WebappReplicaSet is the controller reference onto webapp's replica set.
func WebappReplicaSet() map[string]any {
	return controller("apps/v1", "ReplicaSet", "webapp-7d9f", "rs-webapp")
}

// controller is an owner reference that controls its object.
func controller(apiVersion, kind, name, uid string) map[string]any {
	return map[string]any{"apiVersion": apiVersion, "kind": kind, "name": name, "uid": uid, "controller": true}
}

// Meta is an object's metadata in prod, annotated with its default container
// when defaulted is not "".
func Meta(uid, name, defaulted string) map[string]any {
	m := map[string]any{"uid": uid, "name": name, "namespace": "prod"}
	maps.Copy(m, annotated(defaulted))
	return m
}

// annotated is a metadata carrying kubectl's default-container annotation,
// empty when defaulted is "".
func annotated(defaulted string) map[string]any {
	if defaulted == "" {
		return map[string]any{}
	}
	return map[string]any{"annotations": map[string]any{logs.DefaultContainerAnnotation: defaulted}}
}

// Template is a pod template with containers app and istio-proxy and init
// container setup, annotated with its default container when defaulted is not "".
func Template(defaulted string) map[string]any {
	return map[string]any{
		"metadata": annotated(defaulted),
		"spec": map[string]any{
			"initContainers": []any{map[string]any{"name": "setup", "image": "setup:1"}},
			"containers":     []any{map[string]any{"name": "app", "image": "app:1"}, map[string]any{"name": "istio-proxy", "image": "envoy:1"}},
		},
	}
}

// Container is one running container of a fixture pod: an init container when
// Init is set, with a previous instance when Restarted is.
type Container struct {
	Name      string
	Init      bool
	Restarted bool
}

// PodOf is a Pod named name in prod running containers, owned by owner when it
// is not nil, carrying the default-container annotation when defaulted is not "".
func PodOf(name, defaulted string, owner map[string]any, containers ...Container) map[string]any {
	m := Meta("uid-"+name, name, defaulted)
	if owner != nil {
		m["ownerReferences"] = []any{owner}
	}
	var specs, initSpecs, statuses, initStatuses []any
	for _, c := range containers {
		spec := map[string]any{"name": c.Name, "image": c.Name + ":1"}
		status := containerStatus(name, c)
		if c.Init {
			initSpecs, initStatuses = append(initSpecs, spec), append(initStatuses, status)
		} else {
			specs, statuses = append(specs, spec), append(statuses, status)
		}
	}
	spec := map[string]any{"containers": specs}
	status := map[string]any{"containerStatuses": statuses}
	if len(initSpecs) > 0 {
		spec["initContainers"], status["initContainerStatuses"] = initSpecs, initStatuses
	}
	return map[string]any{"apiVersion": "v1", "kind": "Pod", "metadata": m, "spec": spec, "status": status}
}

// containerStatus is c's status in the pod named pod: running, and with the id
// of the instance before when it restarted.
func containerStatus(pod string, c Container) map[string]any {
	id := "c://" + pod + "-" + c.Name
	status := map[string]any{"name": c.Name, "restartCount": int64(0), "containerID": id, "state": map[string]any{"running": map[string]any{}}}
	if c.Restarted {
		status["restartCount"] = int64(1)
		status["lastState"] = map[string]any{"terminated": map[string]any{"containerID": id + "-old"}}
	}
	return status
}
