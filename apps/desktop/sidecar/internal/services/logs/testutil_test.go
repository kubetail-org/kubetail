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

package logs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/rawjson"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/clustertest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/kubelet"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstore"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
)

// fakeService is the cluster service as the logs service reads it: one active
// cache, its health, its statements run over a real store, and the lease
// AcquireConnection hands out. Every method it
// does not set panics on the nil embedded service. Each call answers its error
// when one is set, and Health and Query answer no cache when told so. A test that
// changes a field while a tail runs does so through set.
type fakeService struct {
	cluster.Service
	mu     sync.Mutex
	active cluster.ActiveCluster
	health cluster.ClusterCacheHealth
	store  *clustertest.Store
	lease  *fakeLease
	// frames is what the record's watch sends, the watch failing once it is closed;
	// nil sends nothing.
	frames chan cluster.ClusterCollectionWatchFrame
	// pods is what the pods watch sends; closing it ends the watch with podsErr.
	pods    chan cluster.ClusterCachedDataObjectCollectionWatchFrame
	podsErr error
	// podNamespaces is what the pods watch was narrowed to.
	podNamespaces []string
	// podsDone closes once the pods watch's context has ended.
	podsDone chan struct{}
	// nodes is what the nodes watch sends; closing it ends the watch with nodesErr.
	nodes    chan cluster.ClusterCachedDataObjectCollectionWatchFrame
	nodesErr error
	// workloads is what each other kind's watch sends, by its resource, made on
	// first use; closing one ends that watch with workloadsErr.
	workloads    map[string]chan cluster.ClusterCachedDataObjectCollectionWatchFrame
	workloadsErr error

	acquireErr, watchErr, watchObjectsErr, readActiveErr, healthErr, queryErr error
	// onAcquire runs as a claim is acquired, before the lease is handed out.
	onAcquire func()
	// queryErrOn is the statement text queryErr applies to, "" for every one.
	queryErrOn        string
	noHealth, noQuery bool
	// healthRead, when set, has each health the fake hands out.
	healthRead *testutil.Probe[cluster.ClusterCacheHealth]
	// mirrorRead, when set, fires as each reading of the mirror returns.
	mirrorRead *testutil.Probe[struct{}]
	// moreOn is the statement text whose answer says rows were left out.
	moreOn string
	// statements is every statement the mirror was asked, its args inlined, in
	// order.
	statements []string
	// beforeStatement, when set, runs before each statement a reading runs, handed
	// its text with its args inlined.
	beforeStatement func(text string)
	// answers, when not nil, keeps each statement's answer by its text and bounds,
	// for a test whose mirror does not change while it reads it.
	answers map[string]cluster.ClusterCachedDataQueryResult
	// readings counts the readings of the mirror ReadActive has started, and reading
	// the ones running.
	readings, reading atomic.Int32
}

// workload is the channel the watch of resource sends from.
func (f *fakeService) workload(resource string) chan cluster.ClusterCachedDataObjectCollectionWatchFrame {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.workloadLocked(resource)
}

func (f *fakeService) workloadLocked(resource string) chan cluster.ClusterCachedDataObjectCollectionWatchFrame {
	if f.workloads == nil {
		f.workloads = map[string]chan cluster.ClusterCachedDataObjectCollectionWatchFrame{}
	}
	if f.workloads[resource] == nil {
		f.workloads[resource] = make(chan cluster.ClusterCachedDataObjectCollectionWatchFrame)
	}
	return f.workloads[resource]
}

// set runs change under the fake's lock.
func (f *fakeService) set(change func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change()
}

func (f *fakeService) Caches() cluster.Caches         { return fakeCaches{f: f} }
func (f *fakeService) CachedData() cluster.CachedData { return fakeCachedData{f: f} }
func (f *fakeService) Clusters() cluster.Clusters     { return fakeClusters{f: f} }

func (f *fakeService) AcquireConnection(context.Context, cluster.ClusterID) (cluster.Lease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.onAcquire != nil {
		f.onAcquire()
	}
	if f.acquireErr != nil {
		return nil, f.acquireErr
	}
	return f.lease, nil
}

type fakeCaches struct {
	cluster.Caches
	f *fakeService
}

// goneCache is a cache id the fake's Caches().Get no longer knows, and
// unreadableCache one it fails to read.
const (
	goneCache       = 404
	unreadableCache = 500
)

// Get is any cache but goneCache and unreadableCache, filed under the cluster "7".
func (fc fakeCaches) Get(_ context.Context, id cluster.ClusterCacheID) (*cluster.ClusterCache, error) {
	switch id {
	case goneCache:
		return nil, cluster.ErrNotFound
	case unreadableCache:
		return nil, errors.New("db closed")
	}
	return &cluster.ClusterCache{RecordMeta: cluster.RecordMeta{ID: id}, Spec: cluster.ClusterCacheSpec{ClusterID: "7"}}, nil
}

func (fc fakeCaches) Health(context.Context, cluster.ClusterID, cluster.ClusterCacheID) (cluster.ClusterCacheHealth, bool, error) {
	fc.f.mu.Lock()
	defer fc.f.mu.Unlock()
	if fc.f.healthRead != nil {
		fc.f.healthRead.Fire(fc.f.health)
	}
	return fc.f.health, !fc.f.noHealth, fc.f.healthErr
}

type fakeCachedData struct {
	cluster.CachedData
	f *fakeService
}

func (fc fakeCachedData) ReadOwn(ctx context.Context, _ cluster.ClusterID, _ cluster.ClusterCacheID, read func(cluster.OwnQuery) error) (bool, error) {
	fc.f.mu.Lock()
	noQuery := fc.f.noQuery
	fc.f.mu.Unlock()
	if noQuery {
		return false, nil
	}
	return true, fc.f.store.Read(ctx, func(query cluster.OwnQuery) error {
		return read(func(sql string, maxRows, maxBytes int, args ...any) (cluster.ClusterCachedDataQueryResult, error) {
			text := inlined(sql, args)
			key := fmt.Sprint(maxRows, " ", maxBytes, " ", text)
			fc.f.mu.Lock()
			fc.f.statements = append(fc.f.statements, text)
			queryErr, more := fc.f.queryErr, fc.f.moreOn != "" && strings.Contains(text, fc.f.moreOn)
			if !strings.Contains(text, fc.f.queryErrOn) {
				queryErr = nil
			}
			before := fc.f.beforeStatement
			res, kept := fc.f.answers[key]
			fc.f.mu.Unlock()
			if before != nil {
				before(text)
			}
			if queryErr != nil {
				return cluster.ClusterCachedDataQueryResult{}, queryErr
			}
			if !kept {
				var err error
				if res, err = query(sql, maxRows, maxBytes, args...); err != nil {
					return cluster.ClusterCachedDataQueryResult{}, err
				}
				fc.f.mu.Lock()
				if fc.f.answers != nil {
					fc.f.answers[key] = res
				}
				fc.f.mu.Unlock()
			}
			res.More = res.More || more
			return res, nil
		})
	})
}

func (fc fakeCachedData) ReadActive(ctx context.Context, id cluster.ClusterID, serverUID string, maxRows, maxBytes int, read func(cluster.CacheReading, cluster.ActiveCluster) error) error {
	fc.f.readings.Add(1)
	fc.f.reading.Add(1)
	defer fc.f.reading.Add(-1)
	defer func() {
		fc.f.mu.Lock()
		defer fc.f.mu.Unlock()
		if fc.f.mirrorRead != nil {
			fc.f.mirrorRead.Fire(struct{}{})
		}
	}()
	return cluster.ReadActiveCache(ctx, fc.f, id, serverUID, maxRows, maxBytes, read)
}

func (fc fakeCachedData) WatchObjectCollectionByNamespaces(ctx context.Context, id cluster.ClusterID, cacheID cluster.ClusterCacheID, apiVersion, resource string, namespaces []string) (*cluster.Stream[cluster.ClusterCachedDataObjectCollectionWatchFrame], error) {
	if resource == "pods" {
		fc.f.mu.Lock()
		fc.f.podNamespaces = namespaces
		fc.f.mu.Unlock()
	}
	return fc.WatchObjectCollection(ctx, id, cacheID, apiVersion, resource)
}

func (fc fakeCachedData) WatchObjectCollection(ctx context.Context, _ cluster.ClusterID, _ cluster.ClusterCacheID, _, resource string) (*cluster.Stream[cluster.ClusterCachedDataObjectCollectionWatchFrame], error) {
	fc.f.mu.Lock()
	watchErr, frames, framesErr, podsDone := fc.f.watchObjectsErr, fc.f.pods, fc.f.podsErr, fc.f.podsDone
	switch resource {
	case "pods":
	case "nodes":
		frames, framesErr, podsDone = fc.f.nodes, fc.f.nodesErr, nil
	default:
		frames, framesErr, podsDone = fc.f.workloadLocked(resource), fc.f.workloadsErr, nil
	}
	fc.f.mu.Unlock()
	if watchErr != nil {
		return nil, watchErr
	}
	if podsDone != nil {
		context.AfterFunc(ctx, func() { close(podsDone) })
	}
	return cluster.NewStream(ctx, func(ctx context.Context, out chan<- cluster.ClusterCachedDataObjectCollectionWatchFrame) error {
		for {
			select {
			case frame, ok := <-frames:
				if !ok {
					return framesErr
				}
				out <- frame
			case <-ctx.Done():
				return nil
			}
		}
	}), nil
}

type fakeClusters struct {
	cluster.Clusters
	f *fakeService
}

func (fc fakeClusters) ReadActive(ctx context.Context, _ cluster.ClusterID, read func(context.Context, cluster.ActiveCluster) error) error {
	fc.f.mu.Lock()
	active, err := fc.f.active, fc.f.readActiveErr
	fc.f.mu.Unlock()
	if err != nil {
		return err
	}
	return read(ctx, active)
}

func (fc fakeClusters) WatchCollectionByID(ctx context.Context, _ cluster.ClusterID) (*cluster.Stream[cluster.ClusterCollectionWatchFrame], error) {
	fc.f.mu.Lock()
	frames, err := fc.f.frames, fc.f.watchErr
	fc.f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return cluster.NewStream(ctx, func(ctx context.Context, out chan<- cluster.ClusterCollectionWatchFrame) error {
		if frames == nil {
			<-ctx.Done()
			return nil
		}
		for {
			select {
			case f, ok := <-frames:
				if !ok {
					return fmt.Errorf("watch failed")
				}
				out <- f
			case <-ctx.Done():
				return nil
			}
		}
	}), nil
}

// fakeLease is a claim on a cluster whose connection is conn, reached by serverUID
// alone; any other UID answers a mismatch.
type fakeLease struct {
	cluster.Lease
	serverUID string
	conn      *cluster.Connection
	released  atomic.Int32
}

func (l *fakeLease) ConnFor(_ context.Context, serverUID string) (*cluster.Connection, error) {
	if serverUID != l.serverUID {
		return nil, fmt.Errorf("%w: another cluster", cluster.ErrIdentityMismatch)
	}
	return l.conn, nil
}

func (l *fakeLease) Release() { l.released.Add(1) }

// serving is a service whose cluster uid-1 is reached at h.
func serving(t testing.TB, h http.Handler) *fakeService {
	t.Helper()
	// HTTP/2, as an API server speaks it: a read that closes its body early drops
	// one stream rather than its connection, so a test paging many logs dials a
	// handful of connections, not one per GET, and leaves the machine's ports alone.
	srv := httptest.NewUnstartedServer(h)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeService{lease: &fakeLease{serverUID: "uid-1", conn: &cluster.Connection{BaseURL: base, HTTPClient: srv.Client()}}}
}

// recordAt is a cluster record whose last probe reached serverUID's server.
func recordAt(serverUID string) *cluster.Cluster {
	c := &cluster.Cluster{}
	c.Status.Server.UID = &serverUID
	return c
}

// newMirror is a cluster whose cache is watching, its server uid-1, holding objects,
// with nothing yet on its pods and nodes watches.
func newMirror(t testing.TB, objects ...map[string]any) *fakeService {
	t.Helper()
	f := &fakeService{
		active: cluster.ActiveCluster{Cluster: recordAt("uid-1"), Cache: &cluster.ClusterCache{RecordMeta: cluster.RecordMeta{ID: 11}, Spec: cluster.ClusterCacheSpec{ServerUID: "uid-1"}}},
		health: cluster.ClusterCacheHealth{Reason: "Watching"},
		store:  clustertest.NewStore(t),
		pods:   make(chan cluster.ClusterCachedDataObjectCollectionWatchFrame),
		nodes:  make(chan cluster.ClusterCachedDataObjectCollectionWatchFrame),
	}
	for _, o := range objects {
		f.store.Apply(t, o)
	}
	return f
}

// webObjects is a deployment web in prod whose replica set runs two pods, each
// with app and istio-proxy, on node-a and node-b, and a lone pod job-1 on no node
// yet.
func webObjects() []map[string]any {
	return []map[string]any{
		workload("apps/v1", "Deployment", "web", "dep-web", nil),
		workload("apps/v1", "ReplicaSet", "web-7d9f", "rs-web", controller("apps/v1", "Deployment", "web", "dep-web")),
		pod("web-1", "uid-web-1", "node-a", webReplicaSet(), container{name: "app"}, container{name: "istio-proxy"}),
		pod("web-2", "uid-web-2", "node-b", webReplicaSet(), container{name: "app"}, container{name: "istio-proxy"}),
		pod("job-1", "uid-job-1", "", nil, container{name: "main"}),
	}
}

// webReplicaSet is the controller reference onto web's replica set.
func webReplicaSet() map[string]any {
	return controller("apps/v1", "ReplicaSet", "web-7d9f", "rs-web")
}

// webNodes are node-a and node-b, in two zones of one region.
func webNodes() []map[string]any {
	return []map[string]any{
		node("node-a", "eu", "eu-1a", "linux", "amd64"),
		node("node-b", "eu", "eu-1b", "linux", "arm64"),
	}
}

// webMirror is webObjects and webNodes.
func webMirror(t testing.TB) *fakeService {
	t.Helper()
	return newMirror(t, append(webObjects(), webNodes()...)...)
}

// chunk makes the cluster's responses hand out at most n bytes per Read, so a
// fetch's budget can run out mid-line.
func (f *fakeService) chunk(n int) {
	c := f.lease.conn.HTTPClient
	c.Transport = chunked{c.Transport, n}
}

type chunked struct {
	next http.RoundTripper
	n    int
}

func (c chunked) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := c.next.RoundTrip(req)
	if err == nil {
		resp.Body = chunkedBody{resp.Body, c.n}
	}
	return resp, err
}

type chunkedBody struct {
	io.ReadCloser
	n int
}

func (b chunkedBody) Read(p []byte) (int, error) {
	return b.ReadCloser.Read(p[:min(len(p), b.n)])
}

// enabledCluster is a record Kubetail connects to.
func enabledCluster() *cluster.Cluster {
	return &cluster.Cluster{Spec: cluster.ClusterSpec{
		Enabled: true,
		Source:  cluster.ClusterSpecSource{Kubeconfig: &cluster.ClusterSpecSourceKubeconfig{Context: "dev"}},
	}}
}

// at is a moment on 2026-10-08 at 10:00, offset by d.
func at(d time.Duration) time.Time {
	return time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC).Add(d)
}

// messages is each record's message.
func messages(recs []logline.Record) []string {
	out := []string{}
	for _, r := range recs {
		out = append(out, r.Message)
	}
	return out
}

// marks is each record's message, with the time past 10:00 of each edge of the hole
// it carries.
func marks(recs []logline.Record) []string {
	out := []string{}
	for _, r := range recs {
		m := r.Message
		if r.Gap != nil {
			m += fmt.Sprintf(" (%v-%v)", r.Gap.After.Sub(at(0)), r.Gap.Before.Sub(at(0)))
		}
		out = append(out, m)
	}
	return out
}

// kubeletOnly is the backends of a cluster read through its API server.
var kubeletOnly = []string{"kubelet"}

// sec is the position of a line s seconds past 10:00.
func sec(s int) logstore.Pos { return logstore.Pos{TS: at(time.Duration(s) * time.Second).UnixNano()} }

// said is a line s seconds past 10:00 saying msg.
func said(s int, msg string) logstore.Line { return logstore.Line{Pos: sec(s), Message: msg} }

// x0 y1 | y5 y6 | y9 x10, each bar a lost gap.
var twoHoles = []logstore.Fill{
	{Lines: []logstore.Line{said(0, "x0"), said(1, "y1")}},
	{Lines: []logstore.Line{said(5, "y5"), said(6, "y6")}, AtBeginning: true},
	{Lines: []logstore.Line{said(9, "y9"), said(10, "x10")}, AtBeginning: true},
}

// webScope reads the app container of deployment web in prod.
var webScope = Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourceDeployment, Name: "web", Containers: []string{"app"}}}}

// addPods adds n pods under web's replica set, each with one app container.
func addPods(t *testing.T, f *fakeService, n int) {
	t.Helper()
	for i := range n {
		name := "many-" + strconv.Itoa(i)
		f.store.Apply(t, pod(name, "uid-"+name, "node-a", webReplicaSet(), container{name: "app"}))
	}
}

// container is one running container of a fixture pod, with a previous instance
// when restarted is set.
type container struct {
	name      string
	restarted bool
}

// controller is an owner reference that controls its object.
func controller(apiVersion, kind, name, uid string) map[string]any {
	return map[string]any{"apiVersion": apiVersion, "kind": kind, "name": name, "uid": uid, "controller": true}
}

// workload is a body of kind in prod, owned by owner when it is not nil.
func workload(apiVersion, kind, name, uid string, owner map[string]any) map[string]any {
	meta := map[string]any{"uid": uid, "name": name, "namespace": "prod"}
	if owner != nil {
		meta["ownerReferences"] = []any{owner}
	}
	return map[string]any{"apiVersion": apiVersion, "kind": kind, "metadata": meta}
}

// pod is a Pod in prod on node ("" for none yet), owned by owner, running
// containers.
func pod(name, uid, node string, owner map[string]any, containers ...container) map[string]any {
	body := workload("v1", "Pod", name, uid, owner)
	var specs, statuses []any
	for _, c := range containers {
		specs = append(specs, map[string]any{"name": c.name, "image": c.name + ":1"})
		statuses = append(statuses, containerStatus(uid, c))
	}
	spec := map[string]any{"containers": specs}
	if node != "" {
		spec["nodeName"] = node
	}
	body["spec"] = spec
	body["status"] = map[string]any{"phase": "Running", "containerStatuses": statuses}
	return body
}

// containerStatus is c's status in the pod uid, with a runtime id per instance,
// as a runtime gives them: the last one's beside the running one's when it
// restarted.
func containerStatus(uid string, c container) map[string]any {
	id := func(instance int) string { return containerID(uid, c.name, instance) }
	status := map[string]any{"name": c.name, "restartCount": int64(0), "containerID": id(0), "state": map[string]any{"running": map[string]any{}}}
	if c.restarted {
		status["restartCount"] = int64(1)
		status["containerID"] = id(1)
		status["lastState"] = map[string]any{"terminated": map[string]any{"containerID": id(0)}}
	}
	return status
}

// containerID is the runtime id of the instance-th instance of the container
// name in the pod uid, as containerStatus writes it.
func containerID(uid, name string, instance int) string {
	return fmt.Sprintf("containerd://%s-%s-%d", uid, name, instance)
}

// withInit is body, a pod, with a terminated init container named name.
func withInit(body map[string]any, name string) map[string]any {
	body["spec"].(map[string]any)["initContainers"] = []any{map[string]any{"name": name, "image": name + ":1"}}
	body["status"].(map[string]any)["initContainerStatuses"] = []any{map[string]any{"name": name, "restartCount": int64(0), "state": map[string]any{"terminated": map[string]any{"containerID": "containerd://" + name}}}}
	return body
}

// finished is body, a pod, with every container terminated: its last instance's
// log is whole.
func finished(body map[string]any) map[string]any {
	for _, s := range body["status"].(map[string]any)["containerStatuses"].([]any) {
		status := s.(map[string]any)
		status["state"] = map[string]any{"terminated": map[string]any{"containerID": status["containerID"]}}
	}
	return body
}

// waiting is body, a pod, with every container waiting to start: no instance
// yet, so no log.
func waiting(body map[string]any) map[string]any {
	for _, s := range body["status"].(map[string]any)["containerStatuses"].([]any) {
		status := s.(map[string]any)
		delete(status, "containerID")
		status["state"] = map[string]any{"waiting": map[string]any{"reason": "ContainerCreating"}}
	}
	return body
}

// podFrame is a pods watch frame of the pod name in prod, and uidFrame one of
// the pod of that UID.
func podFrame(typ cluster.CollectionWatchFrameType, name string) cluster.ClusterCachedDataObjectCollectionWatchFrame {
	return uidFrame(typ, name, "")
}

func uidFrame(typ cluster.CollectionWatchFrameType, name, uid string) cluster.ClusterCachedDataObjectCollectionWatchFrame {
	return cluster.ClusterCachedDataObjectCollectionWatchFrame{Type: typ, Object: &cluster.ClusterCachedDataObject{UID: uid, APIVersion: "v1", Kind: "Pod", Namespace: "prod", Name: name}}
}

// nodeFrame is a nodes watch frame carrying body, a node.
func nodeFrame(typ cluster.CollectionWatchFrameType, body map[string]any) cluster.ClusterCachedDataObjectCollectionWatchFrame {
	raw, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	name := body["metadata"].(map[string]any)["name"].(string)
	return cluster.ClusterCachedDataObjectCollectionWatchFrame{Type: typ, Object: &cluster.ClusterCachedDataObject{
		APIVersion: "v1", Kind: "Node", Name: name, RawJSON: rawjson.RawJSON(raw),
	}}
}

// node is a Node with the well-known topology, OS and architecture labels.
func node(name, region, zone, os, arch string) map[string]any {
	return map[string]any{
		"apiVersion": "v1", "kind": "Node",
		"metadata": map[string]any{"uid": "uid-" + name, "name": name, "labels": map[string]any{
			labelRegion: region, labelZone: zone, labelOS: os, labelArch: arch,
		}},
	}
}

// inlined is sql with each placeholder replaced by its arg as a SQL literal, so a
// test reads a statement whole.
func inlined(sql string, args []any) string {
	if n := strings.Count(sql, "?"); n != len(args) {
		panic(fmt.Sprintf("%d placeholders and %d args in %s", n, len(args), sql))
	}
	for _, a := range args {
		sql = strings.Replace(sql, "?", "'"+strings.ReplaceAll(fmt.Sprint(a), "'", "''")+"'", 1)
	}
	return sql
}

// moment is a pointer to the moment d past 10:00.
func moment(d time.Duration) *time.Time {
	t := at(d)
	return &t
}

// codeOf is err's refusal code, empty for none.
func codeOf(err error) refusal.Code {
	r, _ := refusal.As(err)
	return r.Code
}

// from and to are bounds at the moment d past 10:00: since it and until it.
func from(d time.Duration) logline.Bound {
	return logline.Bound{At: logline.MomentAt(at(d)), Inclusive: true}
}
func to(d time.Duration) logline.Bound {
	return logline.Bound{At: logline.MomentAt(at(d)), Inclusive: true}
}

// lineAt is the line cursor of key's line at d past 10:00, from the first instance
// of a fixture pod, the ordinal-th of its moment.
func lineAt(key logline.StreamKey, d time.Duration, ordinal int) logline.Cursor {
	return logline.Cursor{ContainerName: logline.ContainerName{Namespace: key.Namespace, Pod: key.Pod, Container: key.Container}, RunPlace: logline.RunPlace{ContainerID: containerID("uid-"+key.Pod, key.Container, 0), Timestamp: at(d), Ordinal: ordinal}}
}

// line cursor is the cursor of pod/container's line at d past 10:00, in prod, the
// ordinal-th of its moment, from the first instance of a fixture pod.
func lineCursorFor(pod, container string, d time.Duration, ordinal int) *string {
	return cursorOf(lineAt(logline.StreamKey{Namespace: "prod", Pod: pod, Container: container}, d, ordinal))
}

// cursorOf is c encoded, as a page's edge carries it.
func cursorOf(c logline.Cursor) *string {
	s := logline.Encode(c)
	return &s
}

// keysOf is each of t's streams' key, in order.
func keysOf(t target) []logline.StreamKey {
	keys := make([]logline.StreamKey, len(t.streams))
	for i, s := range t.streams {
		keys[i] = s.key
	}
	return keys
}

// placesOf is each of t's streams' place, in order.
func placesOf(t target) []logline.Place {
	places := make([]logline.Place, len(t.streams))
	for i, s := range t.streams {
		places[i] = s.place
	}
	return places
}

// line is a record of pod/container in prod at d past 10:00, from the first
// instance of a fixture pod, the first line of its moment, and nth the
// ordinal-th.
func line(pod, container string, d time.Duration, msg string) logline.Record {
	return nth(pod, container, d, 0, msg)
}

func nth(pod, container string, d time.Duration, ordinal int, msg string) logline.Record {
	return logline.Record{Timestamp: at(d), Message: msg, Stream: logline.StreamKey{Namespace: "prod", Pod: pod, Container: container}, ContainerID: containerID("uid-"+pod, container, 0), Ordinal: ordinal}
}

// testRetention is the production setting with no janitor, which a test about
// anything else opens its log files with: no grace, so a tailer ends as its last
// tail lets go, and a writer that writes within milliseconds.
var testRetention = func() logstore.Retention {
	r := logstore.DefaultRetention
	r.Interval, r.TailGrace, r.WriteTick, r.WriteWait = 0, 0, 5*time.Millisecond, 5*time.Millisecond
	return r
}()

// serviceOver is the service over f's mirror and the API server h, on a clock
// reading 11:00, its tails resolving at once and retrying within milliseconds
// however often a stream drops.
func serviceOver(t testing.TB, f *fakeService, h http.Handler) (*Service, *fakeService) {
	t.Helper()
	return serviceIn(t, f, h, t.TempDir(), testRetention)
}

// serviceIn is serviceOver with its log files in dir, kept to ret.
func serviceIn(t testing.TB, f *fakeService, h http.Handler, dir string, ret logstore.Retention) (*Service, *fakeService) {
	t.Helper()
	f.lease = serving(t, h).lease
	svc := New(f, dir, ret)
	t.Cleanup(func() { assert.NoError(t, svc.Close()) })
	svc.now, svc.settle = func() time.Time { return at(time.Hour) }, 0
	svc.retry, svc.retryCap = time.Millisecond, 8*time.Millisecond
	return svc, f
}

// fakeBackend is a Backend that reads each stream as reads says, by
// "<pod>/<container>", and fires opened with that key on every read.
type fakeBackend struct {
	mu     sync.Mutex
	reads  map[string]fakeRead
	opened *testutil.Probe[string]
}

// fakeRead is one read of a fake backend: lines, each stamped with its stream,
// then, once wait closes, an end with err. A nil wait ends at once, and never
// ends only once the read is cancelled.
type fakeRead struct {
	lines []logline.Record
	err   error
	wait  <-chan struct{}
}

// never is a wait that never closes.
var never = make(chan struct{})

func newFakeBackend() *fakeBackend {
	return &fakeBackend{reads: map[string]fakeRead{}, opened: testutil.NewProbe[string](64)}
}

// set makes every later read of key read as r.
func (s *fakeBackend) set(key string, r fakeRead) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads[key] = r
}

func (s *fakeBackend) read(ctx context.Context, st kubelet.Log) *kubelet.Read {
	key := st.Key.Pod + "/" + st.Key.Container
	s.mu.Lock()
	r := s.reads[key]
	s.mu.Unlock()
	recs := make([]logline.Record, len(r.lines))
	for i, rec := range r.lines {
		rec.Stream, rec.Place, rec.ContainerID = st.Key, st.Place, st.Instance
		recs[i] = rec
	}
	s.opened.Fire(key)
	return cannedUntil(ctx, st.Key, recs, r.wait, r.err, logline.Cursor{})
}

func (s *fakeBackend) Forward(ctx context.Context, st kubelet.Log, _ kubelet.Options) *kubelet.Read {
	return s.read(ctx, st)
}

func (s *fakeBackend) Backward(ctx context.Context, st kubelet.Log, _ kubelet.Options) *kubelet.Read {
	return s.read(ctx, st)
}

// retiredBackend is a Backend whose every read the connection's retirement cuts
// before its first line, firing reads at each.
type retiredBackend struct {
	reads *testutil.Probe[struct{}]
}

func newRetiredBackend() retiredBackend {
	return retiredBackend{reads: testutil.NewProbe[struct{}](8)}
}

func (b retiredBackend) read(st kubelet.Log) *kubelet.Read {
	b.reads.Fire(struct{}{})
	return endedShort(st.Key, kubelet.ErrRetired)
}

func (b retiredBackend) Forward(_ context.Context, st kubelet.Log, _ kubelet.Options) *kubelet.Read {
	return b.read(st)
}

func (b retiredBackend) Backward(_ context.Context, st kubelet.Log, _ kubelet.Options) *kubelet.Read {
	return b.read(st)
}

// serviceOn is the service over the web mirror, reading through backend.
func serviceOn(t *testing.T, backend kubelet.Backend) (*Service, *fakeService) {
	t.Helper()
	svc, f := serviceOver(t, webMirror(t), newLogServer())
	svc.backends = []namedBackend{{"kubelet", func(kubelet.Getter) kubelet.Backend { return backend }}}
	return svc, f
}

// serviceBatched is serviceOn writing n lines a batch.
func serviceBatched(t *testing.T, backend kubelet.Backend, n int) (*Service, *fakeService) {
	t.Helper()
	ret := testRetention
	ret.WriteBatch = n
	svc, f := serviceIn(t, webMirror(t), newLogServer(), t.TempDir(), ret)
	svc.backends = []namedBackend{{"kubelet", func(kubelet.Getter) kubelet.Backend { return backend }}}
	return svc, f
}

// pages reads scope from the edge from names to the other, PageFromFirst on through
// each Next and PageFromLast back through each Prev, size lines a page, failing past
// 100 pages, and is every page.
func pages(t testing.TB, svc *Service, scope Scope, from PageFrom, size int) []FetchResponse {
	t.Helper()
	page := fetch(t, svc, FetchRequest{Scope: scope, PageFrom: from, PageSize: size})
	return pagesOn(t, svc, page, size)
}

// pagesOn is page and every page after it the way it read: on through each Next
// for a newer page, back through each Prev for an older one.
func pagesOn(t testing.TB, svc *Service, page FetchResponse, size int) []FetchResponse {
	t.Helper()
	newer := page.Request.PageFrom == PageFromFirst || page.Request.Cursor != nil && cursorIn(t, page.Request.Cursor).newer
	out := []FetchResponse{page}
	for range 100 {
		c := page.Page.Prev
		if newer {
			c = page.Page.Next
		}
		if c == nil {
			return out
		}
		page = fetch(t, svc, FetchRequest{Cursor: c, PageSize: size})
		out = append(out, page)
	}
	t.Fatal("the pages never end")
	return nil
}

// recordsOf is every page's records, oldest first: pages read the way from names.
func recordsOf(pages []FetchResponse, from PageFrom) []string {
	out := []string{}
	for _, page := range pages {
		if from == PageFromFirst {
			out = append(out, messages(page.Records)...)
		} else {
			out = append(messages(page.Records), out...)
		}
	}
	return out
}

// skippedOf is each stream-skipped notice page carries, its message.
func skippedOf(page FetchResponse) []string {
	out := []string{}
	for _, n := range page.Notices {
		if n.Code == refusal.CodeStreamSkipped {
			out = append(out, n.Message)
		}
	}
	return out
}

// until is a window up to just before d past 10:00: what a read back from a line
// or a moment at d, exclusive, reads.
func until(d time.Duration) Window { return Window{Until: moment(d - time.Nanosecond)} }

// prevFrom is the Prev a page over scope whose oldest line was pod/container's
// first at d past 10:00 hands out, in an open window: a read back from that line,
// exclusive, size lines a page.
func prevFrom(scope Scope, pod, container string, d time.Duration, size int) FetchRequest {
	c := pageCursor{Scope: scope, resolvedWindow: resolvedWindow{ResolvedAt: at(time.Hour)}, at: lineAt(logline.StreamKey{Namespace: "prod", Pod: pod, Container: container}, d, 0)}
	return FetchRequest{Cursor: encoded(c), PageSize: size}
}

// seenAt is Seen naming pod/container's first line at d past 10:00: what a read
// on past that line reads.
func seenAt(pod, container string, d time.Duration) []string {
	return []string{line(pod, container, d, "").Cursor()}
}

// aroundAt is the first page of scope's window from d, read with n lines of
// context before it: a read on from d and a read back from it, in one page.
func aroundAt(scope Scope, d time.Duration, n int) FetchRequest {
	return FetchRequest{Scope: scope, Window: Window{Since: moment(d), Before: Padding{Lines: n}}, PageFrom: PageFromFirst, PageSize: 2 * n}
}

// linesOf is lines as the records a page answers, their messages alone.
func linesOf(lines []string) []logline.Record {
	out := make([]logline.Record, len(lines))
	for i, l := range lines {
		out[i] = logline.Record{Message: strings.SplitN(l, " ", 2)[1]}
	}
	return out
}

// rowOf is the file row of pod/container's first instance, of a fixture pod on
// node-a, and its default container when the container is app.
func rowOf(pod, container string) logstore.Stream {
	return logstore.Stream{
		Namespace: "prod", Pod: pod, Container: container, ContainerID: containerID("uid-"+pod, container, 0),
		Node: "node-a", Region: "eu", Zone: "eu-1a", OS: "linux", Arch: "amd64", Labels: "{}", PodUID: "uid-" + pod,
		Default: container == "app",
	}
}

// seed files row in the file behind svc and writes fills to it, as earlier pages
// would have, and answers its id.
func seed(t *testing.T, svc *Service, f *fakeService, row logstore.Stream, fills ...logstore.Fill) int64 {
	t.Helper()
	return seedStamped(t, svc, f, row, true, fills...)
}

// seedUnread is seed with its chunks left unstamped, as a tail's fill leaves them.
func seedUnread(t *testing.T, svc *Service, f *fakeService, row logstore.Stream, fills ...logstore.Fill) int64 {
	t.Helper()
	return seedStamped(t, svc, f, row, false, fills...)
}

func seedStamped(t *testing.T, svc *Service, f *fakeService, row logstore.Stream, stamp bool, fills ...logstore.Fill) int64 {
	t.Helper()
	file := fileOf(t, svc, f)
	ids, err := file.PutStreams(t.Context(), []logstore.Stream{row})
	require.NoError(t, err)
	// Callers pass shared fixtures (twoHoles), so stamp a copy: parallel tests would race.
	fills = slices.Clone(fills)
	for i := range fills {
		fills[i].Stream, fills[i].Backend = ids[0], "kubelet"
	}
	_, err = file.Write(t.Context(), fills, stamp)
	require.NoError(t, err)
	return ids[0]
}

// fileLines is a line at each moment d past 10:00 saying its whole seconds.
func fileLines(ds ...time.Duration) []logstore.Line {
	out := make([]logstore.Line, len(ds))
	for i, d := range ds {
		out[i] = logstore.Line{Pos: logstore.Pos{TS: at(d).UnixNano()}, Message: strconv.Itoa(int(d / time.Second))}
	}
	return out
}

// restartedWeb1 is web-1 after its app container restarted, the instance before
// finished at 10:00:05.
func restartedWeb1() map[string]any {
	body := webPod("web-1", "node-a", container{name: "app", restarted: true}, container{name: "istio-proxy"})
	app := body["status"].(map[string]any)["containerStatuses"].([]any)[0].(map[string]any)
	app["lastState"].(map[string]any)["terminated"].(map[string]any)["finishedAt"] = "2026-10-08T10:00:05Z"
	return body
}

// restartWeb1 restarts web-1's app container in f's mirror and in s: the instance
// that ran becomes the previous one, and a new one runs, writing lines.
func restartWeb1(t *testing.T, f *fakeService, s *logstest.Server, lines ...string) {
	f.store.Apply(t, webPod("web-1", "node-a", container{name: "app", restarted: true}, container{name: "istio-proxy"}))
	s.Mu.Lock()
	s.Logs["prod/web-1/app/previous"] = s.Logs["prod/web-1/app"]
	s.Logs["prod/web-1/app"] = lines
	s.Mu.Unlock()
}

// atOf is where the page cursor c goes on from, encoded as a line cursor: its at.
func atOf(t *testing.T, c *string) string {
	t.Helper()
	return logline.Encode(cursorIn(t, c).at)
}

// The limits scaled down so a log of a few hundred lines reaches them: smallCaps
// 1,024 times, and midCaps 64 times, under which a window's GET reads little past
// its end next to what the window holds.
var (
	smallCaps = kubelet.Quota{Second: 1 << 10, Stream: 16 << 10, Gets: 16, Query: 64 << 10, Window: 4 << 10, MinWindow: 64, Part: 16}
	midCaps   = kubelet.Quota{Second: 16 << 10, Stream: 256 << 10, Gets: 16, Query: 1 << 20, Window: 64 << 10, MinWindow: 1 << 10, Part: 256}
)

// capsOf is the limits of a query that downloads n bytes at most, one stream or
// many, with no second too loud to read.
func capsOf(n int64) kubelet.Quota {
	return kubelet.Quota{Second: 1 << 20, Stream: n, Gets: 16, Query: n, Window: max(n/4, 1), MinWindow: 16, Part: 16}
}

// randomCaps is limits a seeded walk's logs of a few hundred bytes a stream reach,
// with a second of a few KiB, which a log of long lines passes.
func randomCaps(rng *rand.Rand) kubelet.Quota {
	stream := int64(2000 + rng.Intn(8000))
	return kubelet.Quota{Second: int64(1000 + rng.Intn(3000)), Stream: stream, Gets: 4 + rng.Intn(13), Query: stream * int64(1+rng.Intn(6)), Window: stream / 4, MinWindow: 16, Part: 16}
}

// newLogServer is a fake log server whose refusals are the status errors a claim
// answers.
func newLogServer() *logstest.Server {
	s := logstest.New()
	s.Refused = func(code int, message string) error { return &kubelet.StatusError{Code: code, Message: message} }
	return s
}

// startAt is the start cursor of a tail of scope from the moment d past 10:00.
func startAt(t *testing.T, scope Scope, d time.Duration) pageCursor {
	t.Helper()
	c, err := tailStartCursor(TailRequest{Scope: scope, Since: moment(d)}, at(time.Hour))
	require.NoError(t, err)
	return c
}

// breakFile runs ddl on the log file of f's active cache in dir, over a connection of
// its own: a trigger that raises fails the writes it names, and a dropped table every
// statement that reads it. It may run on the service's goroutine, so it never stops
// the test.
func breakFile(t *testing.T, dir string, f *fakeService, ddl string) {
	t.Helper()
	path := filepath.Join(dir, strconv.FormatInt(int64(f.active.Cache.ID), 10)+".db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout%3d5000")
	if !assert.NoError(t, err) {
		return
	}
	defer db.Close()
	_, err = db.ExecContext(t.Context(), ddl)
	assert.NoError(t, err)
}
