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

// Behavioral tests for the GraphQL resolvers, exercised over a real gqlgen HTTP
// server. Fixtures and the fakeClusterService live in cluster_testutils_test.go.

import (
	"bytes"
	"context"
	"encoding/json"

	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amorey/gochan/watch"

	"github.com/amorey/beehive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/catalog"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/chat"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor/kubesignals"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/logging"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/clustercard"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/chatgpt"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/github"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/openrouter"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/envvars"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstore"
	logsrefusal "github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/memory"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/providercredentials"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/toolsettings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/agent"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/anthropicwebsearch"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/bash"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/edit"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/kubequery"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/logsread"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/logsview"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/logswatch"
	memorytool "github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/memory"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/read"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/taskstop"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/webfetch"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/write"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/graph"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oauth"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/rawjson"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
)

// --- Cluster ---

// The cluster collection watch maps the beehive records including the nil → null
// convention for unset/never-probed pointer fields.
func TestClusterCollectionWatchMapsTheRecords(t *testing.T) {
	srv := newTestServer(t, clusterFixtures())
	clusters := clusterSnapshot(t, srv.URL, `
		updatedAt
		spec { name syncEnabled enabled source { kubeconfig { context } } }
		status {
			source { kubeconfig { cluster { name entry { server insecureSkipTLSVerify } } user { name } isPresent isDefault } }
			server { uid version endpoint }
			principal { username groups }
		}`)
	require.Len(t, clusters, 2)

	probed := clusters["1"]
	spec := probed["spec"].(map[string]any)
	if spec["name"] != "Production" || spec["syncEnabled"] != true || spec["enabled"] != true {
		t.Errorf("probed cluster spec: %v", spec)
	}
	if kcSrc := spec["source"].(map[string]any)["kubeconfig"].(map[string]any); kcSrc["context"] != "prod" {
		t.Errorf("probed cluster source: %v", kcSrc)
	}
	status := probed["status"].(map[string]any)
	if ci := status["server"].(map[string]any); ci["uid"] != "uid-1" || ci["version"] != "v1.29.3" {
		t.Errorf("probed server: %v", ci)
	}
	if p := status["principal"].(map[string]any); p["username"] != "system:admin" {
		t.Errorf("probed principal: %v", p)
	}
	kc := status["source"].(map[string]any)["kubeconfig"].(map[string]any)
	if kc["isPresent"] != true || kc["isDefault"] != true {
		t.Errorf("probed kubeconfig: %v", kc)
	}
	if c := kc["cluster"].(map[string]any); c["name"] != "prod-cluster" {
		t.Errorf("probed kubeconfig cluster: %v", c)
	}

	unprobed := clusters["2"]
	if name := unprobed["spec"].(map[string]any)["name"]; name != nil {
		t.Errorf("unset name should be null, got: %v", name)
	}
	unprobedStatus := unprobed["status"].(map[string]any)
	if ci := unprobedStatus["server"].(map[string]any); ci["uid"] != nil || ci["version"] != nil {
		t.Errorf("never-probed server should be null, got: %v", ci)
	}
	if p := unprobedStatus["principal"].(map[string]any); p["username"] != nil {
		t.Errorf("never-probed username should be null, got: %v", p)
	}
	if ep := unprobedStatus["server"].(map[string]any)["endpoint"]; ep != nil {
		t.Errorf("never-probed endpoint should be null, got: %v", ep)
	}
}

// Cluster.events maps the service's Events onto the wire: the run id rides
// the ObjectID scalar (decimal string), the type binds to the EventType enum
// (Normal/Warning), and the value slice is adapted to gqlgen's pointer slice.
func TestClusterEventsResolver(t *testing.T) {
	fix := clusterFixtures()
	svc := newFakeClusterService(fix)
	id := fix[0].id
	now := time.Now().UTC()
	svc.events[id] = []cluster.Event{{
		ID: 1, Category: "connection", Type: beehive.EventWarning,
		Reason: "ProbeFailed", Message: "boom", Count: 3, FirstAt: now, LastAt: now,
	}}
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		ClusterSvc: svc, Auth: newFakeAuth(oauth.Identity{}),
	}))
	t.Cleanup(srv.Close)

	events := clusterSnapshot(t, srv.URL,
		`events(category: "connection") { id category type reason message count firstAt lastAt }`)[string(id)]["events"].([]any)
	require.Len(t, events, 1)
	ev := events[0].(map[string]any)
	if ev["id"] != "1" {
		t.Errorf("id: want the run's decimal-string id, got %v", ev["id"])
	}
	if ev["type"] != "Warning" {
		t.Errorf("type: want Warning enum, got %v", ev["type"])
	}
	if ev["reason"] != "ProbeFailed" || ev["category"] != "connection" {
		t.Errorf("reason/category: %v", ev)
	}
	if ev["count"] != float64(3) {
		t.Errorf("count: want 3, got %v", ev["count"])
	}
}

// clusterCollectionWatch is a collection watch: the snapshot arrives as one Added change per
// cluster (not a single list frame), then the stream holds open (no completion)
// until the subscriber goes away.
func TestClusterCollectionWatchEmitsSnapshotAndStaysOpen(t *testing.T) {
	srv := newTestServer(t, clusterFixtures())

	resp := openSSESubscription(t, srv.URL, "",
		"subscription { clusterCollectionWatch { type cluster { id spec { name } } } }")
	defer resp.Body.Close()
	events := sseEvents(t, resp)

	// Collect frames until both fixtures have arrived; each is an Added change.
	seen := map[string]bool{}
	deadline := time.After(2 * time.Second)
	for len(seen) < 2 {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("stream closed before snapshot completed")
			}
			if ev.event != "next" {
				continue
			}
			if !strings.Contains(ev.data, `"type":"Added"`) {
				t.Fatalf("snapshot change should be Added, got: %s", ev.data)
			}
			if strings.Contains(ev.data, `"id":"1"`) {
				seen["1"] = true
			}
			if strings.Contains(ev.data, `"id":"2"`) {
				seen["2"] = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for snapshot; saw %v", seen)
		}
	}

	// The stream stays open after the snapshot (no completion).
	select {
	case _, ok := <-events:
		if !ok {
			t.Fatal("stream closed; want it held open")
		}
	case <-time.After(250 * time.Millisecond):
		// stayed open ✓
	}
}

// clusterEnabledSet writes through and returns the updated record; the change
// is visible in subsequent reads.
func TestClusterEnabledSetMutation(t *testing.T) {
	srv := newTestServer(t, clusterFixtures())

	raw := string(postGQL(t, srv.URL,
		`{"query":"mutation { clusterEnabledSet(id: \"1\", enabled: false) { id spec { enabled } } }"}`))
	if !strings.Contains(raw, `"enabled":false`) || strings.Contains(raw, `"errors"`) {
		t.Fatalf("mutation result: %s", raw)
	}

	spec := clusterSnapshot(t, srv.URL, `spec { enabled }`)["1"]["spec"].(map[string]any)
	assert.Equal(t, false, spec["enabled"], "the change is visible to a new watch")
}

// clusterSyncEnabledSet writes through the beehive store and returns the
// updated record; the change is visible in subsequent reads.
func TestClusterSyncEnabledSetMutation(t *testing.T) {
	srv := newTestServer(t, clusterFixtures())

	raw := string(postGQL(t, srv.URL,
		`{"query":"mutation { clusterSyncEnabledSet(id: \"1\", syncEnabled: false) { id spec { syncEnabled } } }"}`))
	if !strings.Contains(raw, `"syncEnabled":false`) || strings.Contains(raw, `"errors"`) {
		t.Fatalf("mutation result: %s", raw)
	}

	spec := clusterSnapshot(t, srv.URL, `spec { syncEnabled }`)["1"]["spec"].(map[string]any)
	assert.Equal(t, false, spec["syncEnabled"], "the change is visible to a new watch")
}

// clusterDefaultSet makes the cluster the default and returns it as the default; the
// change is visible to a new watch.
func TestClusterDefaultSetMutation(t *testing.T) {
	srv := newTestServer(t, clusterFixtures())

	raw := string(postGQL(t, srv.URL,
		`{"query":"mutation { clusterDefaultSet(id: \"1\", isDefault: true) { id spec { isDefault } } }"}`))
	if !strings.Contains(raw, `"isDefault":true`) || strings.Contains(raw, `"errors"`) {
		t.Fatalf("mutation result: %s", raw)
	}

	spec := clusterSnapshot(t, srv.URL, `spec { isDefault }`)["1"]["spec"].(map[string]any)
	assert.Equal(t, true, spec["isDefault"], "the change is visible to a new watch")
}

// clusterDelete marks the cluster for deletion; the record leaves the cluster
// collection watch. An unknown id is a GraphQL error.
func TestClusterDeleteMutation(t *testing.T) {
	srv := newTestServer(t, clusterFixtures())

	raw := string(postGQL(t, srv.URL, `{"query":"mutation { clusterDelete(id: \"2\") }"}`))
	if !strings.Contains(raw, `"clusterDelete":true`) {
		t.Fatalf("delete result: %s", raw)
	}

	assert.NotContains(t, clusterSnapshot(t, srv.URL, ""), "2", "the deleted cluster is gone")

	raw = string(postGQL(t, srv.URL, `{"query":"mutation { clusterDelete(id: \"999\") }"}`))
	if !strings.Contains(raw, `"errors"`) {
		t.Fatalf("want error for unknown id, got: %s", raw)
	}
}

// A delete the service refuses is a GraphQL error, and the record stays.
func TestClusterDeleteReportsARecordThatWouldNotDelete(t *testing.T) {
	cs := newFakeClusterService(clusterFixtures())
	cs.deleteErr = errors.New("store unavailable")
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		ClusterSvc: cs,
		Auth:       newFakeAuth(oauth.Identity{}),
	}))
	t.Cleanup(srv.Close)

	_, errs := mutation(t, srv.URL, `mutation { clusterDelete(id: "2") }`)

	require.Len(t, errs, 1)
}

// clusterConnectionRetry answers true once the probe it asked for has run; what the
// probe found rides the cluster watch, not the reply. An unknown id is a GraphQL error.
func TestClusterConnectionRetryMutation(t *testing.T) {
	srv := newTestServer(t, clusterFixtures())

	raw := string(postGQL(t, srv.URL, `{"query":"mutation { clusterConnectionRetry(id: \"1\") }"}`))
	if !strings.Contains(raw, `"clusterConnectionRetry":true`) || strings.Contains(raw, `"errors"`) {
		t.Fatalf("retry result: %s", raw)
	}

	raw = string(postGQL(t, srv.URL, `{"query":"mutation { clusterConnectionRetry(id: \"999\") }"}`))
	if !strings.Contains(raw, `"errors"`) {
		t.Fatalf("want error for unknown id, got: %s", raw)
	}
}

// The status condition lists and the cache object resolve without panicking
// or erroring on bare fixtures: the cluster carries no conditions (empty arrays
// on the wire, never null) and the cache — streamed via clusterCacheCollectionWatch —
// has no on-disk files (exists=false, bytes=0, objectCount=0, kindCount=0).
func TestClusterEphemeralFields(t *testing.T) {
	srv := newTestServer(t, clusterFixtures())

	// The cluster's own conditions are an empty list (never null).
	conds := clusterSnapshot(t, srv.URL, `conditions { type status reason }`)["1"]["conditions"]
	assert.Equal(t, []any{}, conds, "conditions should be an empty list")

	// The cache resolves its conditions on a bare fixture. It has no status block: the
	// kind measures nothing itself.
	cache := firstCacheFrame(t, srv.URL)
	spec, _ := cache["spec"].(map[string]any)
	if spec["serverUid"] != "uid-1" || cache["clusterID"] != "1" {
		t.Errorf("cache identity: %v", cache)
	}
	if conds, ok := cache["conditions"].([]any); !ok || len(conds) != 0 {
		t.Errorf("sync conditions should be an empty list, got: %v", cache["conditions"])
	}
}

// Live conditions (cluster + cache) reach the wire with the correct GraphQL shapes —
// type/status/reason/message/liveness/timestamps. Conditions sit beside status, not
// inside it, since beehive stores them as their own object rows.
func TestConditionsAndSyncStatusOnWire(t *testing.T) {
	at := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	fixtures := clusterFixtures()
	fixtures[0].connConds = []cluster.Condition{{
		Type: string(cluster.ConditionConnected), Status: cluster.ConditionFalse,
		Reason: "ProbeFailed", Message: "connection refused",
		Liveness: true, TransitionedAt: at, UpdatedAt: at,
	}}
	fixtures[0].cacheConds = []cluster.Condition{{
		Type: string(cluster.ConditionSynced), Status: cluster.ConditionTrue,
		Reason: "Watching", Liveness: true, TransitionedAt: at, UpdatedAt: at,
	}}

	srv := newTestServer(t, fixtures)

	// The cluster's own conditions ride the cluster collection watch.
	conds := clusterSnapshot(t, srv.URL,
		`conditions { type status reason message liveness transitionedAt updatedAt }`)["1"]["conditions"].([]any)
	require.Len(t, conds, 1)
	c := conds[0].(map[string]any)
	assert.Equal(t, "Connected", c["type"])
	assert.Equal(t, "False", c["status"])
	assert.Equal(t, "ProbeFailed", c["reason"])
	assert.Equal(t, "connection refused", c["message"])
	assert.Equal(t, true, c["liveness"])
	assert.NotNil(t, c["transitionedAt"])
	assert.NotNil(t, c["updatedAt"])

	// The cache's coarse Synced condition rides clusterCacheCollectionWatch. Freshness does not:
	// the sync children report it, out of band from the object graph.
	cache := firstCacheFrame(t, srv.URL)
	syncConds, _ := cache["conditions"].([]any)
	if len(syncConds) != 1 {
		t.Fatalf("Synced condition on the wire: %+v", cache["conditions"])
	}
	c0 := syncConds[0].(map[string]any)
	if c0["type"] != "Synced" || c0["status"] != "True" || c0["reason"] != "Watching" {
		t.Errorf("Synced condition on the wire: %+v", c0)
	}
}

// --- ClusterCache ---

// ClusterCache.syncs completes the navigable path Cluster → caches → syncs.
func TestClusterCacheSyncsResolver(t *testing.T) {
	fix := clusterFixtures()
	srv := newTestServer(t, fix)
	cacheID := fixtureCacheID(fix[0].id)

	got := cacheSnapshot(t, srv.URL, `cachedKinds { id owner { id kind } spec { apiVersion resource } }`)[strconv.FormatInt(int64(cacheID), 10)]["cachedKinds"].([]any)
	require.Len(t, got, 1, "this cache's one record")
	kind := got[0].(map[string]any)
	if kind["id"] != strconv.FormatInt(int64(fixtureKindID(fix[0].id)), 10) {
		t.Errorf("id: want the sync record's own id, got %v", kind["id"])
	}
	if ownerOf(kind)["id"] != strconv.FormatInt(int64(cacheID), 10) {
		t.Errorf("owner: want the cache's id, got %v", ownerOf(kind))
	}
}

// Cluster.caches is the navigable path down the owner chain. Asserted on the wire
// because a resolver that isn't wired returns an empty list rather than failing.
func TestClusterCachesResolver(t *testing.T) {
	fix := clusterFixtures()
	srv := newTestServer(t, fix)
	id := fix[0].id

	got := clusterSnapshot(t, srv.URL, `caches { id clusterID spec { serverUid } }`)[string(id)]["caches"].([]any)
	require.Len(t, got, 1, "this cluster's one cache")
	cache := got[0].(map[string]any)
	// Its own id, not its cluster's — the two differ in the fixture on purpose.
	if cache["id"] != strconv.FormatInt(int64(fixtureCacheID(id)), 10) {
		t.Errorf("id: want the cache's own id, got %v", cache["id"])
	}
	if cache["clusterID"] != string(id) {
		t.Errorf("clusterID: want the parent's id, got %v", cache["clusterID"])
	}
}

// The two cache-side event timelines are the same generic reader hung off a different
// record: `ClusterCache.events` reads the cache's own timeline (what the cache layer
// records, e.g. SyncStopped), `ClusterCachedKind.events` one synced kind's (where
// each worker report lands). One table because the wire mapping under test —
// cluster.Event → the generic Event shape, enum included — is identical; only the record it
// hangs off differs.
func TestCacheEventTimelineResolvers(t *testing.T) {
	now := time.Now().UTC()
	const events = `events(category: "sync") { id category type reason message count firstAt lastAt }`
	tests := []struct {
		name string
		// lookupID derives the record's own id from its cluster's, since the two are
		// deliberately different in the fixture.
		lookupID func(cluster.ClusterID) cluster.ObjectID
		snapshot func(t *testing.T, srvURL string, id cluster.ClusterID) map[string]map[string]any
		seed     func(*fakeClusterService, cluster.ClusterID, cluster.Event)
		event    cluster.Event
		wantEnum string
	}{{
		name:     "cache timeline",
		lookupID: func(id cluster.ClusterID) cluster.ObjectID { return cluster.ObjectID(fixtureCacheID(id)) },
		snapshot: func(t *testing.T, srvURL string, _ cluster.ClusterID) map[string]map[string]any {
			return cacheSnapshot(t, srvURL, events)
		},
		seed: func(f *fakeClusterService, id cluster.ClusterID, ev cluster.Event) {
			f.cacheEvents = map[cluster.ClusterCacheID][]cluster.Event{fixtureCacheID(id): {ev}}
		},
		event: cluster.Event{
			Category: "sync", Type: beehive.EventWarning, Reason: "SyncFailed",
			Message: "boom", Count: 2, FirstAt: now, LastAt: now,
		},
		wantEnum: "Warning",
	}, {
		name:     "per-kind sync timeline",
		lookupID: func(id cluster.ClusterID) cluster.ObjectID { return cluster.ObjectID(fixtureKindID(id)) },
		snapshot: func(t *testing.T, srvURL string, id cluster.ClusterID) map[string]map[string]any {
			cacheID := strconv.FormatInt(int64(fixtureCacheID(id)), 10)
			return collectionSnapshot(t, srvURL,
				`subscription { clusterCachedKindCollectionWatch(cacheID: "`+cacheID+`") { type kind { id `+events+` } } }`,
				"clusterCachedKindCollectionWatch", "kind")
		},
		seed: func(f *fakeClusterService, id cluster.ClusterID, ev cluster.Event) {
			f.syncEvents = map[cluster.ClusterCachedKindID][]cluster.Event{fixtureKindID(id): {ev}}
		},
		event: cluster.Event{
			Category: "sync", Type: beehive.EventNormal, Reason: "SyncComplete",
			Message: "cached 12 events", Count: 2, FirstAt: now, LastAt: now,
		},
		wantEnum: "Normal",
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fix := clusterFixtures()
			svc := newFakeClusterService(fix)
			id := tt.lookupID(fix[0].id)
			ev := tt.event
			ev.ID = id
			tt.seed(svc, fix[0].id, ev)
			srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
				ClusterSvc: svc, Auth: newFakeAuth(oauth.Identity{}),
			}))
			t.Cleanup(srv.Close)

			got := tt.snapshot(t, srv.URL, fix[0].id)[strconv.FormatInt(int64(id), 10)]["events"].([]any)
			require.Len(t, got, 1)
			event := got[0].(map[string]any)
			if event["type"] != tt.wantEnum {
				t.Errorf("type: want %s enum, got %v", tt.wantEnum, event["type"])
			}
			if event["reason"] != tt.event.Reason || event["category"] != "sync" {
				t.Errorf("reason/category: %v", event)
			}
			if event["count"] != float64(2) {
				t.Errorf("count: want 2, got %v", event["count"])
			}
		})
	}
}

// clusterCacheClear empties one cache's file and returns the (still-tracked) record.
// The id it takes is the cache's own — a cluster id names no single cache once a UID
// migration has left two — so the fixture's cluster id must not resolve. An unknown id
// surfaces the not-found error.
func TestClusterCacheClearMutation(t *testing.T) {
	srv := newTestServer(t, clusterFixtures())

	body, _ := json.Marshal(map[string]string{"query": `mutation { clusterCacheClear(id: 101) { id } }`})
	raw := postGQL(t, srv.URL, string(body))
	if !strings.Contains(string(raw), `"id":"101"`) {
		t.Errorf("expected the cleared cache back, got %s", raw)
	}

	body, _ = json.Marshal(map[string]string{"query": `mutation { clusterCacheClear(id: 1) { id } }`})
	raw = postGQL(t, srv.URL, string(body))
	if !strings.Contains(string(raw), "errors") {
		t.Errorf("expected a GraphQL error for a cluster id, got %s", raw)
	}

	body, _ = json.Marshal(map[string]string{"query": `mutation { clusterCacheClear(id: "999") { id } }`})
	raw = postGQL(t, srv.URL, string(body))
	if !strings.Contains(string(raw), "errors") {
		t.Errorf("expected a GraphQL error for an unknown id, got %s", raw)
	}
}

// The wire keeps the positive form, matching the cluster's own two toggles, while the
// stored field is its inverse so a record written before the field decodes as syncing. One
// negation at the projection — so the round trip is what pins them together.
func TestClusterCachedKindSyncEnabledSetMutation(t *testing.T) {
	srv := newTestServer(t, clusterFixtures())
	id := strconv.FormatInt(int64(fixtureKindID("1")), 10)

	body, _ := json.Marshal(map[string]string{
		"query": `mutation { clusterCachedKindSyncEnabledSet(id: ` + id + `, syncEnabled: false) { id spec { syncEnabled } } }`,
	})
	raw := postGQL(t, srv.URL, string(body))
	if !strings.Contains(string(raw), `"syncEnabled":false`) {
		t.Errorf("expected the paused record back, got %s", raw)
	}

	body, _ = json.Marshal(map[string]string{
		"query": `mutation { clusterCachedKindSyncEnabledSet(id: "999", syncEnabled: false) { id } }`,
	})
	raw = postGQL(t, srv.URL, string(body))
	if !strings.Contains(string(raw), "errors") {
		t.Errorf("expected a GraphQL error for an unknown id, got %s", raw)
	}
}

// TestClusterCachedKindCollectionWatchIsCacheScoped pins the scoping on the wire: the stream is
// opened for one cache and must carry only that cache's kinds. The fixture gives each
// cache one record, so a leak shows up as a second frame.
func TestClusterCachedKindCollectionWatchIsCacheScoped(t *testing.T) {
	srv := newTestServer(t, clusterFixtures())

	resp := openSSESubscription(t, srv.URL, "",
		`subscription { clusterCachedKindCollectionWatch(cacheID: "`+strconv.FormatInt(int64(fixtureCacheID("1")), 10)+`") { type kind { id owner { id kind } `+
			`spec { apiVersion kind resource namespaced } conditions { type status reason } } } }`)
	defer resp.Body.Close()
	events := sseEvents(t, resp)

	// The failsafe until the bookmark; then a negative window, bounded on purpose, for a
	// record of another cache arriving after it.
	seen := 0
	deadline := time.After(testutil.Timeout)
	for {
		var frame struct {
			Data struct {
				Watch struct {
					Type string         `json:"type"`
					Kind map[string]any `json:"kind"`
				} `json:"clusterCachedKindCollectionWatch"`
			} `json:"data"`
		}
		select {
		case ev, ok := <-events:
			if !ok {
				if seen == 0 {
					t.Fatal("stream closed before a frame arrived")
				}
				return
			}
			if ev.event != "next" {
				continue
			}
			if err := json.Unmarshal([]byte(ev.data), &frame); err != nil {
				t.Fatalf("decode cached-kind frame %s: %v", ev.data, err)
			}
			// Detect the snapshot boundary by type, never by a missing entity: an
			// errored non-null field nulls its parent, so a null entity also rides
			// ordinary frames.
			if frame.Data.Watch.Type == string(cluster.CollectionWatchFrameBookmark) {
				deadline = time.After(time.Second)
				continue
			}
			seen++
			cachedKind := frame.Data.Watch.Kind
			if ownerOf(cachedKind)["id"] != strconv.FormatInt(int64(fixtureCacheID("1")), 10) {
				t.Fatalf("another cache's record leaked into the stream: %v", cachedKind)
			}
			spec, _ := cachedKind["spec"].(map[string]any)
			if spec["resource"] != "deployments" {
				t.Errorf("resource = %v, want deployments", spec["resource"])
			}
		case <-deadline:
			if seen != 1 {
				t.Fatalf("expected exactly this cache's one record, saw %d", seen)
			}
			return
		}
	}
}

// TestCollectionWatchClosesSnapshotWithBookmark pins the boundary a consumer waits on before
// rendering an empty state: exactly one Bookmark, after the Added frames, carrying no
// entity. Without it a still-listing collection is indistinguishable from an empty one.
func TestCollectionWatchClosesSnapshotWithBookmark(t *testing.T) {
	srv := newTestServer(t, clusterFixtures())

	resp := openSSESubscription(t, srv.URL, "",
		`subscription { clusterCacheCollectionWatch { type cache { id } } }`)
	defer resp.Body.Close()
	events := sseEvents(t, resp)

	// The failsafe until the bookmark; then a negative window, bounded on purpose, for a second
	// bookmark or a late Added.
	added, bookmarks := 0, 0
	deadline := time.After(testutil.Timeout)
	for {
		var frame struct {
			Data struct {
				Watch struct {
					Type  string         `json:"type"`
					Cache map[string]any `json:"cache"`
				} `json:"clusterCacheCollectionWatch"`
			} `json:"data"`
		}
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("stream closed before the bookmark arrived")
			}
			if ev.event != "next" {
				continue
			}
			if err := json.Unmarshal([]byte(ev.data), &frame); err != nil {
				t.Fatalf("decode cache frame %s: %v", ev.data, err)
			}
			switch frame.Data.Watch.Type {
			case string(cluster.CollectionWatchFrameBookmark):
				bookmarks++
				deadline = time.After(time.Second)
				if frame.Data.Watch.Cache != nil {
					t.Errorf("the bookmark carries no entity, got: %v", frame.Data.Watch.Cache)
				}
				if bookmarks == 1 && added != len(clusterFixtures()) {
					t.Errorf("bookmark closed the snapshot after %d of %d records", added, len(clusterFixtures()))
				}
			case string(cluster.CollectionWatchFrameAdded):
				if bookmarks > 0 {
					t.Error("an Added frame arrived after the snapshot closed")
				}
				added++
			}
		case <-deadline:
			if bookmarks != 1 {
				t.Fatalf("expected exactly one bookmark, saw %d", bookmarks)
			}
			return
		}
	}
}

// TestClusterCacheStatsStateWatchServesTheStats pins that the cache summary is reachable as a
// stream — the only shape that keeps reporting once the cache record itself settles.
func TestClusterCacheStatsStateWatchServesTheStats(t *testing.T) {
	srv := newTestServer(t, clusterFixtures())

	resp := openSSESubscription(t, srv.URL, "",
		`subscription { clusterCacheStatsStateWatch(id: "1", cacheID: "`+strconv.FormatInt(int64(fixtureCacheID("1")), 10)+`") { exists bytes objectCount kindCount } }`)
	defer resp.Body.Close()
	events := sseEvents(t, resp)

	deadline := time.After(2 * time.Second)
	for {
		var frame struct {
			Data struct {
				Stats map[string]any `json:"clusterCacheStatsStateWatch"`
			} `json:"data"`
		}
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("stream closed before a frame arrived")
			}
			if ev.event != "next" {
				continue
			}
			if err := json.Unmarshal([]byte(ev.data), &frame); err != nil {
				t.Fatalf("decode stats frame %s: %v", ev.data, err)
			}
			if got := frame.Data.Stats["objectCount"]; got != float64(1386) {
				t.Errorf("objectCount = %v, want 1386", got)
			}
			if got := frame.Data.Stats["kindCount"]; got != float64(62) {
				t.Errorf("kindCount = %v, want 62", got)
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for a stats frame")
		}
	}
}

// --- Cluster data ---
// The discovered kind catalog and the cached objects read out of a ClusterCache.

// ClusterCache.kinds hands its namespace to the service, which counts in it.
func TestClusterCachedDataKindsResolverPassesTheNamespace(t *testing.T) {
	fix := clusterFixtures()
	svc := newFakeClusterService(fix)
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		ClusterSvc: svc, Auth: newFakeAuth(oauth.Identity{}),
	}))
	t.Cleanup(srv.Close)

	cacheSnapshot(t, srv.URL, `kinds(namespace: "prod") { kind }`)

	svc.mu.Lock()
	defer svc.mu.Unlock()
	if svc.kindsIn != "prod" {
		t.Errorf("ListKinds counted in %q, want prod", svc.kindsIn)
	}
}

// ClusterCache.kinds maps the service's ClusterCachedDataKinds onto the wire 1:1
// (bound via gqlgen.yml), so the resolver just adapts the value slice to a pointer
// slice. Both ids it reads with come off the record, so the pair cannot disagree.
func TestClusterCachedDataKindsResolver(t *testing.T) {
	fix := clusterFixtures()
	svc := newFakeClusterService(fix)
	id := fix[0].id
	svc.kinds = map[cluster.ClusterID][]cluster.ClusterCachedDataKind{
		id: {
			{APIVersion: "apps/v1", Kind: "Deployment", Resource: "deployments", Scope: "Namespaced", IsCRD: false},
			{APIVersion: "example.com/v1", Kind: "Widget", Resource: "widgets", Scope: "Namespaced", IsCRD: true},
		},
	}
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		ClusterSvc: svc, Auth: newFakeAuth(oauth.Identity{}),
	}))
	t.Cleanup(srv.Close)

	kinds := cacheSnapshot(t, srv.URL, `kinds { apiVersion kind resource scope isCRD }`)[strconv.FormatInt(int64(fixtureCacheID(id)), 10)]["kinds"].([]any)
	require.Len(t, kinds, 2)
	k := kinds[0].(map[string]any)
	if k["apiVersion"] != "apps/v1" || k["kind"] != "Deployment" || k["resource"] != "deployments" {
		t.Errorf("first kind: %v", k)
	}
	if k["scope"] != "Namespaced" || k["isCRD"] != false {
		t.Errorf("first kind scope/isCRD: %v", k)
	}
	if kinds[1].(map[string]any)["isCRD"] != true {
		t.Errorf("second kind should be a CRD: %v", kinds[1])
	}
}

// clusterCachedDataObjectCollectionWatch wires the subscription resolver to the service: the fake with
// no seeded objects opens an empty-until-ctx stream, so the SSE dial succeeds and the
// stream stays open with no frames rather than erroring.
func TestClusterCachedDataObjectCollectionWatchOpensWithoutError(t *testing.T) {
	fix := clusterFixtures()
	svc := newFakeClusterService(fix)
	id := fix[0].id
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		ClusterSvc: svc, Auth: newFakeAuth(oauth.Identity{}),
	}))
	t.Cleanup(srv.Close)

	idStr := string(id)
	q := `subscription { clusterCachedDataObjectCollectionWatch(id: "` + idStr + `", cacheID: "` + idStr +
		`", apiVersion: "apps/v1", resource: "deployments") { type object { uid name } } }`
	resp := openSSESubscription(t, srv.URL, "", q)
	defer resp.Body.Close()
	events := sseEvents(t, resp)

	// The stub emits no frames; assert only that the dial produced no error frame within a
	// short window (a `next` carrying `errors`, or an SSE `error`/`complete` on open).
	select {
	case ev, ok := <-events:
		if ok && (ev.event == "error" || strings.Contains(ev.data, `"errors"`)) {
			t.Fatalf("objects watch should open cleanly, got %s: %s", ev.event, ev.data)
		}
	case <-time.After(200 * time.Millisecond):
		// No frame is the expected empty-until-ctx posture.
	}
}

// The resolver-gated `object` field carries the full native body as the JSON scalar,
// marshaled verbatim through gqlgen — a consumer selecting it gets the object JSON back
// as a nested value (not a string), with the identity fields alongside.
func TestClusterCachedDataObjectCollectionWatchServesNativeBody(t *testing.T) {
	fix := clusterFixtures()
	svc := newFakeClusterService(fix)
	id := fix[0].id
	svc.dataObjects = map[cluster.ClusterID][]cluster.ClusterCachedDataObject{
		id: {{
			UID: "d1", APIVersion: "apps/v1", Kind: "Deployment", Namespace: "default", Name: "web",
			RawJSON: rawjson.RawJSON(`{"kind":"Deployment","spec":{"replicas":3}}`),
		}},
	}
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		ClusterSvc: svc, Auth: newFakeAuth(oauth.Identity{}),
	}))
	t.Cleanup(srv.Close)

	idStr := string(id)
	q := `subscription { clusterCachedDataObjectCollectionWatch(id: "` + idStr + `", cacheID: "` + idStr +
		`", apiVersion: "apps/v1", resource: "deployments") { type object { uid name rawJSON } } }`
	resp := openSSESubscription(t, srv.URL, "", q)
	defer resp.Body.Close()
	events := sseEvents(t, resp)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("stream closed before the snapshot frame")
			}
			if ev.event != "next" {
				continue
			}
			var frame struct {
				Data struct {
					ClusterCachedDataObjectCollectionWatch struct {
						Type   string `json:"type"`
						Object struct {
							UID     string         `json:"uid"`
							Name    string         `json:"name"`
							RawJSON map[string]any `json:"rawJSON"`
						} `json:"object"`
					} `json:"clusterCachedDataObjectCollectionWatch"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(ev.data), &frame); err != nil {
				t.Fatalf("decode frame %s: %v", ev.data, err)
			}
			got := frame.Data.ClusterCachedDataObjectCollectionWatch
			if got.Object.UID != "d1" || got.Object.Name != "web" {
				t.Fatalf("identity fields: got %+v", got.Object)
			}
			// The body decoded as a nested JSON object, not a string.
			if got.Object.RawJSON["kind"] != "Deployment" {
				t.Fatalf("native body not served as JSON: %s", ev.data)
			}
			if spec, _ := got.Object.RawJSON["spec"].(map[string]any); spec == nil || spec["replicas"] != float64(3) {
				t.Fatalf("nested body fields missing: %s", ev.data)
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for the object snapshot frame")
		}
	}
}

// Each frame names the namespace its counts were taken in: the one subscribed with, and
// "" for the whole cache when none is.
func TestClusterCachedDataKindCollectionWatchNamesItsNamespace(t *testing.T) {
	svc := newFakeClusterService(clusterFixtures())
	svc.kinds = map[cluster.ClusterID][]cluster.ClusterCachedDataKind{
		"1": {{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: "Namespaced"}},
	}
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{ClusterSvc: svc, Auth: newFakeAuth(oauth.Identity{})}))
	t.Cleanup(srv.Close)

	for name, tc := range map[string]struct{ arg, want string }{
		"a namespace":     {`, namespace: "checkout"`, `"namespace":"checkout"`},
		"the whole cache": {``, `"namespace":""`},
	} {
		t.Run(name, func(t *testing.T) {
			resp := openSSESubscription(t, srv.URL, "",
				`subscription { clusterCachedDataKindCollectionWatch(id: "1", cacheID: "1"`+tc.arg+`) { namespace kind { kind } } }`)
			defer resp.Body.Close()

			frame := testutil.Recv(t, sseEvents(t, resp), "the first frame")
			assert.Contains(t, frame.data, tc.want)
		})
	}
}

// clusterCachedDataKindCollectionWatch streams the kind catalog as a collection watch: the resolver
// adapts the service's ClusterCachedDataKindCollectionWatchFrame stream to the wire 1:1, so the snapshot
// arrives as Added changes carrying the kind's fields (incl. the live count) and the
// stream stays open for live updates.
func TestClusterCachedDataKindCollectionWatchEmitsSnapshotAndStaysOpen(t *testing.T) {
	fix := clusterFixtures()
	svc := newFakeClusterService(fix)
	id := fix[0].id
	svc.kinds = map[cluster.ClusterID][]cluster.ClusterCachedDataKind{
		id: {
			{APIVersion: "apps/v1", Kind: "Deployment", Resource: "deployments", Scope: "Namespaced", IsCRD: false, Count: 3},
			{APIVersion: "example.com/v1", Kind: "Widget", Resource: "widgets", Scope: "Namespaced", IsCRD: true, Count: 0},
		},
	}
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		ClusterSvc: svc, Auth: newFakeAuth(oauth.Identity{}),
	}))
	t.Cleanup(srv.Close)

	q := `subscription { clusterCachedDataKindCollectionWatch(id: "` + string(id) +
		`", cacheID: "` + string(id) + `") { type kind { apiVersion kind resource count } } }`
	resp := openSSESubscription(t, srv.URL, "", q)
	defer resp.Body.Close()
	events := sseEvents(t, resp)

	seen := map[string]int{}
	deadline := time.After(2 * time.Second)
	for len(seen) < 2 {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("stream closed before snapshot completed")
			}
			if ev.event != "next" {
				continue
			}
			if !strings.Contains(ev.data, `"type":"Added"`) {
				t.Fatalf("snapshot change should be Added, got: %s", ev.data)
			}
			var frame struct {
				Data struct {
					ClusterCachedDataKindCollectionWatch struct {
						Type string `json:"type"`
						Kind struct {
							Kind  string `json:"kind"`
							Count int    `json:"count"`
						} `json:"kind"`
					} `json:"clusterCachedDataKindCollectionWatch"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(ev.data), &frame); err != nil {
				t.Fatalf("decode frame %s: %v", ev.data, err)
			}
			seen[frame.Data.ClusterCachedDataKindCollectionWatch.Kind.Kind] = frame.Data.ClusterCachedDataKindCollectionWatch.Kind.Count
		case <-deadline:
			t.Fatalf("timed out waiting for snapshot; saw %v", seen)
		}
	}
	if seen["Deployment"] != 3 {
		t.Errorf("Deployment count: want 3, got %d", seen["Deployment"])
	}
	if _, ok := seen["Widget"]; !ok {
		t.Errorf("Widget kind missing from snapshot: %v", seen)
	}

	// The stream stays open after the snapshot (no completion).
	select {
	case _, ok := <-events:
		if !ok {
			t.Fatal("stream closed; want it held open")
		}
	case <-time.After(250 * time.Millisecond):
		// stayed open ✓
	}
}

// --- Cloud account ---

// postGQL POSTs a GraphQL query/mutation body to url's /graphql endpoint and
// returns the raw response.
func postGQL(t *testing.T, url, body string) []byte {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url+"/graphql", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return raw
}

// The fakes (memCredStore/fakeOAuthFlow/fakeLoopback) live in testutils_test.go;
// the postGQL / SSE helpers are defined above + in server_test.go.

// The auth state watch starts on a signed-out auth service with no stale identity.
func TestAuthStateWatchSignedOut(t *testing.T) {
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{Auth: newFakeAuth(oauth.Identity{})}))
	defer srv.Close()

	data := firstFrame(t, srv.URL, `subscription { authStateWatch { authenticated identity { sub } } }`)
	assert.Equal(t, map[string]any{"authenticated": false, "identity": nil}, data["authStateWatch"])
}

// A connector that has not read the keyring yet is restoring, and says
// nothing of a session; a sign-in or sign-out then is a validation error.
func TestAuthStateWhileRestoring(t *testing.T) {
	svc := newFakeAuthState(connectors.State{Phase: connectors.Restoring}, oauth.Identity{})
	svc.connectErr, svc.disconnectErr = connectors.ErrRestoring, connectors.ErrRestoring
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{Auth: svc}))
	defer srv.Close()

	data := firstFrame(t, srv.URL, `subscription { authStateWatch { restoring authenticated identity { sub } } }`)
	assert.Equal(t, map[string]any{"restoring": true, "authenticated": false, "identity": nil}, data["authStateWatch"])
	for _, op := range []string{`mutation { authLoginStart }`, `mutation { authLogout }`} {
		_, errs := mutation(t, srv.URL, op)
		require.Len(t, errs, 1, op)
		assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", errs[0].Extensions["code"], op)
	}
}

// The auth state watch starts on the current signed-in identity.
func TestAuthStateWatchSignedIn(t *testing.T) {
	svc := signedInFakeAuth(oauth.Identity{UserID: "u1", Email: "a@x.com", Name: "Ada"})

	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{Auth: svc}))
	defer srv.Close()

	data := firstFrame(t, srv.URL, `subscription { authStateWatch { authenticated identity { sub email name } } }`)
	assert.Equal(t, map[string]any{
		"authenticated": true, "identity": map[string]any{"sub": "u1", "email": "a@x.com", "name": "Ada"},
	}, data["authStateWatch"])
}

// authStateWatch emits the current snapshot first, then a fresh snapshot on change.
func TestAuthStateWatchSnapshotThenDelta(t *testing.T) {
	svc := newFakeAuth(oauth.Identity{Email: "a@x.com"})
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{Auth: svc}))
	defer srv.Close()

	resp := openSSESubscription(t, srv.URL, "",
		"subscription { authStateWatch { authenticated identity { email } } }")
	defer resp.Body.Close() // ends the subscription; must run before srv.Close()
	events := sseEvents(t, resp)

	if ev := nextSSE(t, events); ev.event != "next" || !strings.Contains(ev.data, `"authenticated":false`) {
		t.Fatalf("first frame: event=%q data=%s", ev.event, ev.data)
	}

	if err := svc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if ev := nextSSE(t, events); ev.event != "next" || !strings.Contains(ev.data, `"email":"a@x.com"`) {
		t.Fatalf("delta frame: event=%q data=%s", ev.event, ev.data)
	}
}

// logout delegates to the account: returns true and flips the session signed-out.
func TestLogoutMutation(t *testing.T) {
	svc := signedInFakeAuth(oauth.Identity{})
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{Auth: svc}))
	defer srv.Close()

	raw := string(postGQL(t, srv.URL, `{"query":"mutation { authLogout }"}`))
	if !strings.Contains(raw, `"authLogout":true`) {
		t.Fatalf("want authLogout true, got: %s", raw)
	}
	if svc.State().Phase == connectors.Connected {
		t.Fatal("session still signed in after logout")
	}
}

// login is non-blocking: it returns true immediately and the resulting signed-in
// session arrives asynchronously (observed here via the auth-state watch), proving
// the mutation kicked off the flow without blocking on the browser round-trip.
func TestLoginMutationKicksOffFlow(t *testing.T) {
	svc := newFakeAuth(oauth.Identity{Email: "a@x.com"})

	// The auth-state stream is latest-value (current-on-subscribe): the first State
	// is the signed-out baseline, and the sign-in surfaces as a later Connected
	// State. The loop skips the baseline and waits for sign-in.
	states, cancel := svc.WatchState()
	defer cancel()

	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{Auth: svc}))
	defer srv.Close()

	raw := string(postGQL(t, srv.URL, `{"query":"mutation { authLoginStart }"}`))
	if !strings.Contains(raw, `"authLoginStart":true`) {
		t.Fatalf("want login true, got: %s", raw)
	}

	for {
		select {
		case st := <-states:
			if st.Phase == connectors.Connected {
				if st.Email != "a@x.com" {
					t.Fatalf("signed-in state = %+v", st)
				}
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatal("login never produced a signed-in session")
		}
	}
}

// A sign-in that could not start (no issuer, no keyring, no browser) surfaces
// as a GraphQL error rather than a silent login:true.
func TestLoginMutationSurfacesSetupError(t *testing.T) {
	svc := newFakeAuth(oauth.Identity{Email: "a@x.com"})
	svc.connectErr = errors.New("kubetail: the sign-in could not start: Refused")

	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{Auth: svc}))
	defer srv.Close()

	raw := string(postGQL(t, srv.URL, `{"query":"mutation { authLoginStart }"}`))
	if !strings.Contains(raw, `"errors"`) {
		t.Fatalf("want GraphQL error for a setup failure, got: %s", raw)
	}
	if strings.Contains(raw, `"authLoginStart":true`) {
		t.Fatalf("login must not report true when setup failed, got: %s", raw)
	}
}

// TestClusterCacheSyncStateWatchServesEveryKind pins the one field on the wire that carries
// a per-kind verdict: nothing else can say which of a cache's hundred kinds is failing.
func TestClusterCacheSyncStateWatchServesEveryKind(t *testing.T) {
	srv := newTestServer(t, clusterFixtures())

	resp := openSSESubscription(t, srv.URL, "",
		`subscription { clusterCacheSyncStateWatch(id: "1", cacheID: "`+strconv.FormatInt(int64(fixtureCacheID("1")), 10)+`") { cacheID exists discovery { reason } kinds { apiVersion resource reason restarts objectCount } } }`)
	defer resp.Body.Close()
	events := sseEvents(t, resp)

	deadline := time.After(2 * time.Second)
	for {
		var frame struct {
			Data struct {
				Status struct {
					CacheID   string           `json:"cacheID"`
					Exists    bool             `json:"exists"`
					Discovery map[string]any   `json:"discovery"`
					Kinds     []map[string]any `json:"kinds"`
				} `json:"clusterCacheSyncStateWatch"`
			} `json:"data"`
		}
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("stream closed before a frame arrived")
			}
			if ev.event != "next" {
				continue
			}
			if err := json.Unmarshal([]byte(ev.data), &frame); err != nil {
				t.Fatalf("decode sync status frame %s: %v", ev.data, err)
			}
			if !frame.Data.Status.Exists {
				t.Error("a live cache's detail says it does not exist")
			}
			if got := frame.Data.Status.Discovery["reason"]; got != "Discovered" {
				t.Errorf("discovery reason = %v, want Discovered", got)
			}
			if len(frame.Data.Status.Kinds) == 0 {
				t.Fatal("a cache with mirrored kinds served none")
			}
			for _, kind := range frame.Data.Status.Kinds {
				if kind["apiVersion"] == "" || kind["resource"] == "" {
					t.Errorf("a kind row identifies nothing: %v", kind)
				}
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for a sync status frame")
		}
	}
}

// The descriptors reach the wire off the kinds watch, typed — not as the JSON the store holds.
func TestPrinterColumnsReachTheWire(t *testing.T) {
	svc := newFakeClusterService(clusterFixtures())
	svc.kinds = map[cluster.ClusterID][]cluster.ClusterCachedDataKind{
		"1": {{
			APIVersion: "example.com/v1", Kind: "Widget", Resource: "widgets", Scope: "Namespaced", IsCRD: true,
			PrinterColumns: []cluster.PrinterColumn{
				{Name: "Replicas", Type: "integer", JSONPath: ".spec.replicas", Priority: 1},
			},
		}},
	}
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{ClusterSvc: svc, Auth: newFakeAuth(oauth.Identity{})}))
	t.Cleanup(srv.Close)

	resp := openSSESubscription(t, srv.URL, "",
		`subscription { clusterCachedDataKindCollectionWatch(id: "1", cacheID: "1") { kind { printerColumns { name type jsonPath priority } } } }`)
	defer resp.Body.Close()

	frame := testutil.Recv(t, sseEvents(t, resp), "the first frame")

	assert.Contains(t, frame.data, `"name":"Replicas"`)
	assert.Contains(t, frame.data, `"jsonPath":".spec.replicas"`)
	assert.Contains(t, frame.data, `"priority":1`)
}

// A timestamp the source object never carried serializes as null, not as 0001-01-01. The records
// keep a value time.Time — the collection watch diff compares frames with ==, so they must stay
// comparable — and this is the wire's whole answer for "absent".
func TestAnAbsentTimestampSerializesAsNull(t *testing.T) {
	seen := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	svc := newFakeClusterService(clusterFixtures())
	svc.dataEvents = map[cluster.ClusterID][]cluster.ClusterCachedDataEvent{
		"1": {{UID: "e-1", LastSeen: seen}},
	}
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{ClusterSvc: svc, Auth: newFakeAuth(oauth.Identity{})}))
	t.Cleanup(srv.Close)

	resp := openSSESubscription(t, srv.URL, "",
		`subscription { clusterCachedDataEventCollectionWatch(id: "1", cacheID: "1") { event { firstSeen lastSeen } } }`)
	defer resp.Body.Close()

	frame := testutil.Recv(t, sseEvents(t, resp), "the first frame")

	assert.Contains(t, frame.data, `"firstSeen":null`)
	assert.Contains(t, frame.data, `"lastSeen":"2026-08-30T12:00:00Z"`)
}

// --- Chat ---

// The cluster service's named errors reach the client as codes the same way: an id
// naming nothing is not found, and a record its source still declares or one that
// will not be connected is a conflict with the record's own state.
func TestClusterRefusalsCarryTheirCode(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{cluster.ErrNotFound, "KUBETAIL_RECORD_NOT_FOUND"},
		{cluster.ErrDeclaredBySource, "KUBETAIL_CONFLICT"},
		{cluster.ErrNotConnectable, "KUBETAIL_CONFLICT"},
		{cluster.ErrCannotBeDefault, "KUBETAIL_VALIDATION_ERROR"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
				ClusterSvc: errClusterService{err: tc.err}, Auth: newFakeAuth(oauth.Identity{}),
			}))
			t.Cleanup(srv.Close)

			for _, q := range []string{
				`mutation { clusterEnabledSet(id: "1", enabled: true) { id } }`,
				`mutation { clusterSyncEnabledSet(id: "1", syncEnabled: true) { id } }`,
				`mutation { clusterDefaultSet(id: "1", isDefault: true) { id } }`,
				`mutation { clusterConnectionRetry(id: "1") }`,
				`mutation { clusterDelete(id: "1") }`,
			} {
				_, errs := mutation(t, srv.URL, q)
				require.Len(t, errs, 1, q)
				assert.Equal(t, tc.code, errs[0].Extensions["code"], q)
			}
		})
	}
}

// --- Error arms ---

// Every resolver that can fail hands the service's error straight back, and none
// of them swallows one into a zero value. Driven through the resolver interfaces
// rather than the wire: the arm under test is the same one either way, and one
// table keeps a newly added resolver visibly absent.
func TestResolverErrorsReachTheCaller(t *testing.T) {
	wantErr := errors.New("service unavailable")
	r := &graph.Resolver{
		ClusterSvc: errClusterService{err: wantErr},
		Auth:       errAuth{err: wantErr},
		Monitor:    errMonitor{err: wantErr},
	}
	ctx := context.Background()
	cl := &cluster.Cluster{ID: "1"}
	cache := &cluster.ClusterCache{
		RecordMeta: cluster.RecordMeta{ID: 101},
		ClusterID:  "1",
	}
	kind := &cluster.ClusterCachedKind{RecordMeta: cluster.RecordMeta{ID: 301}}
	id := cluster.ClusterID("1")
	objID := cluster.ObjectID(1)

	// Each case calls one resolver and reports only its error; the values are
	// asserted by the tests above, on a service that works.
	calls := map[string]func() error{
		"Cluster.caches":                           func() error { _, err := r.Cluster().Caches(ctx, cl); return err },
		"Cluster.events":                           func() error { _, err := r.Cluster().Events(ctx, cl, nil, nil); return err },
		"ClusterCache.kinds":                       func() error { _, err := r.ClusterCache().Kinds(ctx, cache, ""); return err },
		"ClusterCache.cachedKinds":                 func() error { _, err := r.ClusterCache().CachedKinds(ctx, cache); return err },
		"ClusterCache.events":                      func() error { _, err := r.ClusterCache().Events(ctx, cache, nil, nil); return err },
		"ClusterCachedKind.events":                 func() error { _, err := r.ClusterCachedKind().Events(ctx, kind, nil, nil); return err },
		"Mutation.clusterEnabledSet":               func() error { _, err := r.Mutation().ClusterEnabledSet(ctx, id, true); return err },
		"Mutation.clusterSyncEnabledSet":           func() error { _, err := r.Mutation().ClusterSyncEnabledSet(ctx, id, true); return err },
		"Mutation.monitorTargetEnabledSet":         func() error { _, err := r.Mutation().MonitorTargetEnabledSet(ctx, "t1", true); return err },
		"Subscription.incidentCollectionWatch":     func() error { _, err := r.Subscription().IncidentCollectionWatch(ctx); return err },
		"Subscription.incidentOccurrenceEventFeed": func() error { _, err := r.Subscription().IncidentOccurrenceEventFeed(ctx, "i1"); return err },
		"Mutation.clusterDefaultSet":               func() error { _, err := r.Mutation().ClusterDefaultSet(ctx, id, true); return err },
		"Mutation.clusterConnectionRetry":          func() error { _, err := r.Mutation().ClusterConnectionRetry(ctx, id); return err },
		"Mutation.clusterDelete":                   func() error { _, err := r.Mutation().ClusterDelete(ctx, id); return err },
		"Mutation.clusterCacheClear":               func() error { _, err := r.Mutation().ClusterCacheClear(ctx, objID); return err },
		"Mutation.clusterCachedKindSyncSet":        func() error { _, err := r.Mutation().ClusterCachedKindSyncEnabledSet(ctx, objID, true); return err },
		"Mutation.authLogout":                      func() error { _, err := r.Mutation().AuthLogout(ctx); return err },
		"Subscription.objectEventFeed":             func() error { _, err := r.Subscription().ObjectEventFeed(ctx, objID, nil); return err },
		"Subscription.clusterCollectionWatch":      func() error { _, err := r.Subscription().ClusterCollectionWatch(ctx); return err },
		"Subscription.clusterEventFeed":            func() error { _, err := r.Subscription().ClusterEventFeed(ctx, id, nil); return err },
		"Subscription.clusterScheduleStateWatch":   func() error { _, err := r.Subscription().ClusterScheduleStateWatch(ctx, id); return err },
		"Subscription.clusterDataRateSampleFeed":   func() error { _, err := r.Subscription().ClusterDataRateSampleFeed(ctx, id); return err },
		"Subscription.clusterCacheCollectionWatch": func() error { _, err := r.Subscription().ClusterCacheCollectionWatch(ctx); return err },
		"Subscription.cacheHealthWatch":            func() error { _, err := r.Subscription().ClusterCacheHealthKeyedStateWatch(ctx); return err },
		"Subscription.cachedKindsWatch":            func() error { _, err := r.Subscription().ClusterCachedKindCollectionWatch(ctx, objID); return err },
		"Subscription.cacheStatsWatch":             func() error { _, err := r.Subscription().ClusterCacheStatsStateWatch(ctx, id, objID); return err },
		"Subscription.cacheSyncStatusWatch":        func() error { _, err := r.Subscription().ClusterCacheSyncStateWatch(ctx, id, objID); return err },
		"Subscription.cachedDataKindsWatch": func() error {
			_, err := r.Subscription().ClusterCachedDataKindCollectionWatch(ctx, id, objID, "")
			return err
		},
		"Subscription.cachedDataEventsWatch": func() error {
			_, err := r.Subscription().ClusterCachedDataEventCollectionWatch(ctx, id, objID)
			return err
		},

		"Subscription.monitorTargetCollectionWatch": func() error {
			_, err := r.Subscription().MonitorTargetCollectionWatch(ctx)
			return err
		},
		"Subscription.cachedDataObjectsWatch": func() error {
			_, err := r.Subscription().ClusterCachedDataObjectCollectionWatch(ctx, id, objID, "apps/v1", "deployments")
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, wantErr) {
				t.Errorf("err = %v, want %v", err, wantErr)
			}
		})
	}
}

// authLoginStart answers the error of a sign-in that could not start, and
// a sign-in while connected as a validation error.
func TestAuthLoginStartSurfacesTheConnectorsError(t *testing.T) {
	wantErr := errors.New("kubetail: the sign-in could not start: Unavailable")
	fake := newFakeAuth(oauth.Identity{})
	fake.connectErr = wantErr
	r := &graph.Resolver{Auth: fake}

	ok, err := r.Mutation().AuthLoginStart(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if ok {
		t.Error("authLoginStart = true on a failed start, want false")
	}

	fake.connectErr = fmt.Errorf("refused: %w", connectors.ErrConnected)
	srv := httptest.NewServer(graph.NewServer(context.Background(), r))
	defer srv.Close()
	var got struct{ Errors []gqlError }
	if err := json.Unmarshal(postGQL(t, srv.URL, `{"query":"mutation { authLoginStart }"}`), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Errors) != 1 || got.Errors[0].Extensions["code"] != "KUBETAIL_VALIDATION_ERROR" {
		t.Fatalf("errors = %+v, want one KUBETAIL_VALIDATION_ERROR", got.Errors)
	}
}

// ClusterPrincipal.permissions is the one field with no implementation behind
// it: selecting it errors rather than resolving to an empty grant, so a
// consumer can't read "no permissions" out of "not built yet".
func TestClusterPrincipalPermissionsIsNotImplemented(t *testing.T) {
	r := &graph.Resolver{}
	perms, err := r.ClusterPrincipal().Permissions(context.Background(), &cluster.ClusterPrincipal{}, "default")
	if err == nil {
		t.Fatalf("permissions resolved to %+v, want an error", perms)
	}
}

// The three watches with no wire test of their own open against a working
// service and carry their first value: objectEventFeed's snapshot boundary, the
// schedule state watch's current value, and one cache's health verdict. Each also
// exercises the stream adapter behind it — subscriptionStream for the frame types,
// ptrSubscription for the bare state watch.
func TestStateWatchesAndEventFeedsCarryTheirFirstValue(t *testing.T) {
	ctx := t.Context()
	r := &graph.Resolver{ClusterSvc: newFakeClusterService(clusterFixtures())}

	events, err := r.Subscription().ObjectEventFeed(ctx, 1, nil)
	if err != nil {
		t.Fatalf("objectEventFeed: %v", err)
	}
	// The fixture logs no events, so the Bookmark alone closes the snapshot.
	if frame := testutil.Recv(t, events, "objectEventFeed frame"); frame.Type != cluster.EventFeedFrameBookmark {
		t.Errorf("first frame = %v, want Bookmark", frame.Type)
	}
	clusterEvents, err := r.Subscription().ClusterEventFeed(ctx, "1", nil)
	if err != nil {
		t.Fatalf("clusterEventFeed: %v", err)
	}
	if frame := testutil.Recv(t, clusterEvents, "clusterEventFeed frame"); frame.Type != cluster.EventFeedFrameBookmark {
		t.Errorf("first frame = %v, want Bookmark", frame.Type)
	}

	schedule, err := r.Subscription().ClusterScheduleStateWatch(ctx, "1")
	if err != nil {
		t.Fatalf("clusterScheduleStateWatch: %v", err)
	}
	if s := testutil.Recv(t, schedule, "schedule state watch"); !s.Probing {
		t.Errorf("schedule = %+v, want the probing value the fake publishes", s)
	}

	health, err := r.Subscription().ClusterCacheHealthKeyedStateWatch(ctx)
	if err != nil {
		t.Fatalf("clusterCacheHealthKeyedStateWatch: %v", err)
	}
	if v := testutil.Recv(t, health, "cache health verdict"); v.CacheID == 0 {
		t.Errorf("verdict = %+v, want one keyed to a cache", v)
	}
}

// The frame reaches the wire as the service sent it, interval and window included.
func TestClusterDataRateSampleFeedMapsTheFrame(t *testing.T) {
	r := &graph.Resolver{ClusterSvc: newFakeClusterService(clusterFixtures())}

	frames, err := r.Subscription().ClusterDataRateSampleFeed(t.Context(), "1")
	require.NoError(t, err)

	assert.Equal(t, fakeDataRateFrame, *testutil.Recv(t, frames, "the window"))
}

// Every Dialect member is named for its API, and the Go set and the schema's are
// the same set: gqlgen refuses a member with no constant behind it at generate
// time, and this is the other direction, a constant the schema does not declare.
func TestLLMDialectBindsEveryMember(t *testing.T) {
	declared := map[string]bool{}
	for _, v := range graph.NewExecutableSchema(graph.Config{}).Schema().Types["LLMDialect"].EnumValues {
		declared[strings.ToLower(v.Name)] = true
	}
	held := map[string]bool{}
	for _, p := range llm.Dialects {
		held[string(p)] = true
	}
	assert.Equal(t, held, declared)
}

// --- chat ---

// newChatServer is a GraphQL server over a real chat service on the fake,
// with cluster "1" seeded for sends to file under.
func newChatServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv, _, _ := newChatServerOver(t)
	return srv
}

// newSandboxedChatServer is newChatServer on a machine whose sandbox is state.
func newSandboxedChatServer(t *testing.T, state sandbox.State) *httptest.Server {
	t.Helper()
	srv, _, _ := newChatServerWith(t, state)
	return srv
}

// newChatServerOver is newChatServer handing back the app.db, for a test that fails
// the store by closing it, and the fake, for one that stages a reply.
func newChatServerOver(t *testing.T) (*httptest.Server, *appdb.DB, *llm.Fake) {
	t.Helper()
	return newChatServerWith(t, sandbox.State{})
}

// newChatServerWith is newChatServerOver on a machine whose sandbox is state.
func newChatServerWith(t *testing.T, state sandbox.State) (*httptest.Server, *appdb.DB, *llm.Fake) {
	t.Helper()
	return newChatServerOn(t, state, testSettings(t))
}

// newChatServerOn is newChatServerWith over the settings store svc.
func newChatServerOn(t *testing.T, state sandbox.State, svc *settings.Service) (*httptest.Server, *appdb.DB, *llm.Fake) {
	t.Helper()
	db := appdbtest.Open(t)
	_, err := db.Write.Exec(`INSERT INTO clusters (id, source, source_key, created_at, updated_at) VALUES ('1', 'kubeconfig', 'ctx', 0, 0)`)
	require.NoError(t, err)
	fake := llm.NewFake(0)
	cat := catalog.New(catalog.Config{Fake: fake})
	llmSvc := llm.New(cat.Providers()...)
	// The search is offered, since a test stages a turn that searched, and LogsView
	// answering one fixed view; the rest is read alone, so no call runs while stored
	// calls still show.
	box := tools.NewBox([]tools.Tool{agent.New(), anthropicwebsearch.New(time.Now), openedView{logsview.New(nil)}}, bash.Reader{}, &read.Tool{}, &write.Tool{}, &edit.Tool{}, &webfetch.Tool{}, taskstop.New(), memorytool.New(nil), kubequery.New(nil), logsread.New(nil, nil), logswatch.New(nil, nil))
	chatSvc, err := chat.New(db, filepath.Join(t.TempDir(), "chats"), filepath.Join(t.TempDir(), "monitor"), llmSvc, clustercard.New(newFakeClusterService(nil)), nil, box, cat, state, svc, nil)
	require.NoError(t, err)
	stop, err := chatSvc.Start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, stop(context.Background()))
		require.NoError(t, chatSvc.Close())
	})
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		ClusterSvc: newFakeClusterService(nil), ChatSvc: chatSvc, LLMSvc: llmSvc, Auth: newFakeAuth(oauth.Identity{}),
		SandboxState: state, Settings: svc,
	}))
	t.Cleanup(srv.Close)
	return srv, db, fake
}

// openedView is LogsView opening viewOpened whatever it is asked, so a call shows
// a view with no mirror behind it.
type openedView struct{ *logsview.Tool }

var viewOpened = tools.LogsViewAction{
	LogsScopeInput: tools.LogsScopeInput{
		Sources: []tools.LogsViewSource{{Namespace: "prod", Kind: tools.LogsSourceDeployment, Name: "webapp", Containers: []string{}}},
		Filters: []tools.LogsViewFilter{},
	},
	Anchor:     tools.LogsViewAnchor{Kind: tools.LogsAnchorTail},
	PinToEnd:   true,
	ResolvedAt: time.Date(2026, 10, 8, 14, 4, 0, 0, time.UTC),
}

func (openedView) RunShown(context.Context, tools.Runtime, json.RawMessage) (string, bool, *tools.Action) {
	view := viewOpened
	return "Viewing deployments/webapp in prod at the newest line, pinned to the end.", false, &tools.Action{LogsView: &view}
}

func (o openedView) Run(ctx context.Context, rt tools.Runtime, input json.RawMessage) (string, bool) {
	text, isError, _ := o.RunShown(ctx, rt, input)
	return text, isError
}

// mutate posts one operation and returns its data, failing on any error in the response.
func mutate(t *testing.T, srv *httptest.Server, query string) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"query": query})
	raw := postGQL(t, srv.URL, string(body))
	var resp struct {
		Data   map[string]any
		Errors []struct{ Message string }
	}
	require.NoError(t, json.Unmarshal(raw, &resp), "%s", raw)
	require.Empty(t, resp.Errors)
	return resp.Data
}

// chatSend returns the answer row: streaming, on the names the send gave, with the
// provider labelled by its id and no thinking.
func TestChatSendReturnsTheAnswerRow(t *testing.T) {
	srv := newChatServer(t)

	data := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "how many pods?") {
		seq role status model effort thinking provider { id label } content
	} }`)

	assert.Equal(t, map[string]any{
		"seq": float64(1), "role": "Assistant", "status": "Streaming", "model": "fake", "effort": "high",
		"thinking": "", "provider": map[string]any{"id": "fake", "label": "Fake"}, "content": []any{},
	}, data["chatSend"])
}

// A send naming a model on another dialect than the chat's last answer's is passed
// through like any other.
func TestChatSendAcceptsAModelOnAnotherDialect(t *testing.T) {
	srv, db, _ := newChatServerOver(t)
	for _, stmt := range []string{
		`INSERT INTO chats (id, cluster_id, mode, created_at, updated_at) VALUES ('c', '1', 'chat', 0, 0)`,
		`INSERT INTO messages (id, chat_id, seq, role, content, created_at) VALUES ('u', 'c', 0, 'user', '[{"type":"text","text":"hi"}]', 0)`,
		`INSERT INTO agent_runs (id, agent_type, app_version, trigger, chat_id, trigger_message_id, provider, model, dialect, status, created_at)
		 VALUES ('r', 'chat', 'test', 'chat', 'c', 'u', 'anthropic', 'claude-haiku-4-5', 'messages', 'succeeded', 0)`,
		`INSERT INTO messages (id, chat_id, seq, role, content, run_id, created_at) VALUES ('a', 'c', 1, 'assistant', '[{"type":"text","text":"Hello."}]', 'r', 0)`,
	} {
		_, err := db.Write.Exec(stmt)
		require.NoError(t, err)
	}

	data := mutate(t, srv, `mutation { chatSend(chatID: "c", mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "and now?") { chatID provider { id } } }`)

	assert.Equal(t, map[string]any{"chatID": "c", "provider": map[string]any{"id": "fake"}}, data["chatSend"])
}

// chatSend hands the service the snapshot it was sent, field by field, and nil
// for a send with none.
func TestChatSendHandsItsSnapshotToTheService(t *testing.T) {
	rec := recordingChat{snaps: make(chan *chat.Snapshot, 1)}
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{ChatSvc: rec}))
	defer srv.Close()
	send := func(snapshot string) {
		mutate(t, srv, `mutation { chatSend(chatID: "c", mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
			requestID: "`+appdb.NewID()+`", content: "what's this?"`+snapshot+`) { id } }`)
	}

	send(`, snapshot: {callID: "call-1", from: "2026-10-17T09:14:02.118Z", to: "2026-10-17T09:14:31.902Z", lines: 38, atEnd: true, pinnedToEnd: true, paused: true, grep: "panic",
		selected: [{timestamp: "2026-10-17T09:14:20.004Z", namespace: "payments", pod: "webapp-x", container: "app", previous: true, message: "boom"}], selectedCount: 2}`)
	from := time.Date(2026, 10, 17, 9, 14, 2, 118_000_000, time.UTC)
	to := time.Date(2026, 10, 17, 9, 14, 31, 902_000_000, time.UTC)
	assert.Equal(t, &chat.Snapshot{
		CallID: "call-1", From: &from, To: &to, Grep: new("panic"), Lines: 38, AtEnd: true, PinnedToEnd: true, Paused: true,
		Selected: []chat.SelectedLine{{
			Timestamp: time.Date(2026, 10, 17, 9, 14, 20, 4_000_000, time.UTC),
			Namespace: "payments", Pod: "webapp-x", Container: "app", Previous: true, Message: "boom",
		}},
		SelectedCount: 2,
	}, testutil.Recv(t, rec.snaps, "the send"))

	send("")
	assert.Nil(t, testutil.Recv(t, rec.snaps, "the send without one"))
}

// chatFrames opens a subscription over the chat server and returns its events.
// The caller closes the response body, which is what cancels the subscription.
func chatFrames(t *testing.T, srv *httptest.Server, query string) (*http.Response, <-chan sseEvent) {
	t.Helper()
	resp := openSSESubscription(t, srv.URL, "", query)
	return resp, sseEvents(t, resp)
}

// A chat with no turn running is cancelled without error, and its rows stay: the
// mutation answers true whether or not there was anything to stop.
func TestChatCancelOfAnIdleChatAnswersTrue(t *testing.T) {
	srv := newChatServer(t)
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)
	chatID := sent["chatSend"].(map[string]any)["chatID"].(string)

	data := mutate(t, srv, `mutation { chatCancel(chatID: "`+chatID+`") }`)

	assert.Equal(t, map[string]any{"chatCancel": true}, data)
}

// Delete answers true and the chat leaves the list; deleting one already gone is
// true too, since the row the caller asked to be rid of is gone either way.
func TestChatDeleteTakesTheChatOffTheList(t *testing.T) {
	srv := newChatServer(t)
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)
	chatID := sent["chatSend"].(map[string]any)["chatID"].(string)

	assert.Equal(t, map[string]any{"chatDelete": true}, mutate(t, srv, `mutation { chatDelete(id: "`+chatID+`") }`))
	assert.Equal(t, map[string]any{"chatDelete": true}, mutate(t, srv, `mutation { chatDelete(id: "`+chatID+`") }`))

	resp, events := chatFrames(t, srv, `subscription { chatCollectionWatch { type chat { id } } }`)
	defer resp.Body.Close()
	assert.Equal(t, "Bookmark", firstChatFrameType(t, events), "the snapshot is empty")
}

// A rename answers with the chat as committed, not as asked: the service trims the
// title and moves updatedAt.
func TestChatRenameServesTheCommittedChat(t *testing.T) {
	srv := newChatServer(t)
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)
	chatID := sent["chatSend"].(map[string]any)["chatID"].(string)

	data := mutate(t, srv, `mutation { chatRename(id: "`+chatID+`", title: "  Renamed  ") { id title createdAt updatedAt } }`)

	chat := data["chatRename"].(map[string]any)
	assert.Equal(t, chatID, chat["id"])
	assert.Equal(t, "Renamed", chat["title"])
	createdAt, err := time.Parse(time.RFC3339Nano, chat["createdAt"].(string))
	require.NoError(t, err)
	updatedAt, err := time.Parse(time.RFC3339Nano, chat["updatedAt"].(string))
	require.NoError(t, err)
	assert.False(t, updatedAt.Before(createdAt), "updatedAt %s is before createdAt %s", updatedAt, createdAt)
}

// A send with no chat creates one, filed under the mode and cluster it names. The
// other sends all say Chat, so this is the one that carries Dashboard through.
func TestChatSendWithNoChatCreatesOne(t *testing.T) {
	srv := newChatServer(t)
	sent := mutate(t, srv, `mutation { chatSend(mode: Dashboard, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)
	chatID := sent["chatSend"].(map[string]any)["chatID"].(string)

	resp, events := chatFrames(t, srv, `subscription { chatCollectionWatch { type chat { id mode clusterID } } }`)
	defer resp.Body.Close()

	frame := awaitChatFrame(t, events, func(f map[string]any) bool { return f["type"] == "Added" })
	assert.Equal(t, map[string]any{"id": chatID, "mode": "Dashboard", "clusterID": "1"}, frame["chat"])
}

// The list watch opens with the chats there are, each an Added, closed by the
// Bookmark that says the snapshot is whole. The Bookmark carries no chat: the
// webview detects it by type and drops a change with no entity.
func TestChatCollectionWatchClosesItsSnapshotWithABookmark(t *testing.T) {
	srv := newChatServer(t)
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "how many pods?") { chatID } }`)
	chatID := sent["chatSend"].(map[string]any)["chatID"].(string)

	resp, events := chatFrames(t, srv, `subscription { chatCollectionWatch { type chat { id title mode clusterID } } }`)
	defer resp.Body.Close()

	frame := awaitChatFrame(t, events, func(f map[string]any) bool { return f["type"] == "Added" })
	assert.Equal(t, map[string]any{
		"id": chatID, "title": "how many pods?", "mode": "Chat", "clusterID": "1",
	}, frame["chat"])

	assert.Equal(t, map[string]any{"type": "Bookmark", "chat": nil}, nextChatFrame(t, events))
}

// The transcript watch carries the question and the answer, and finishedAt is null
// while the answer is still streaming and a time once its run has finished.
func TestChatMessageCollectionWatchCarriesTheAnswerToCompletion(t *testing.T) {
	srv := newChatServer(t)
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID finishedAt } }`)
	answer := sent["chatSend"].(map[string]any)
	assert.Nil(t, answer["finishedAt"], "a streaming answer has not finished")

	resp, events := chatFrames(t, srv,
		`subscription { chatMessageCollectionWatch(chatID: "`+answer["chatID"].(string)+`") { type message { seq status finishedAt } } }`)
	defer resp.Body.Close()

	frame := awaitChatFrame(t, events, func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["seq"] == float64(1) && msg["status"] == "Complete"
	})
	assert.NotNil(t, frame["message"].(map[string]any)["finishedAt"])
}

// A transcript watch serves its own chat's rows alone: the snapshot is the question,
// the answer as far as it has streamed, and the Bookmark; then the answer's changes.
func TestChatMessageCollectionWatchIsScopedAndStreamsChanges(t *testing.T) {
	srv, _, fake := newChatServerOver(t)
	mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "first") { chatID } }`)
	// A route, so the first chat's turn cannot take the staged reply.
	second := fake.Route("second")
	second.SetReply(llm.Chunk{Kind: llm.ChunkThinking, Text: "so far"}, llm.Chunk{Text: "done"})
	gate := make(chan struct{})
	second.SetGate(gate)
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "second") { chatID } }`)
	chatID := sent["chatSend"].(map[string]any)["chatID"].(string)
	query := `subscription { chatMessageCollectionWatch(chatID: "` + chatID + `") { type message { chatID seq role status thinking } } }`

	// Send returns before the turn streams; once the thought is in, the turn is
	// parked at the gate and the next snapshot is fixed.
	first, events := chatFrames(t, srv, query)
	defer first.Body.Close()
	awaitChatFrame(t, events, func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["thinking"] == "so far"
	})
	first.Body.Close()

	resp, events := chatFrames(t, srv, query)
	defer resp.Body.Close()
	assert.Equal(t, map[string]any{"type": "Added", "message": map[string]any{
		"chatID": chatID, "seq": float64(0), "role": "User", "status": "Complete", "thinking": "",
	}}, nextChatFrame(t, events))
	assert.Equal(t, map[string]any{"type": "Added", "message": map[string]any{
		"chatID": chatID, "seq": float64(1), "role": "Assistant", "status": "Streaming", "thinking": "so far",
	}}, nextChatFrame(t, events))
	assert.Equal(t, map[string]any{"type": "Bookmark", "message": nil}, nextChatFrame(t, events))

	close(gate)
	for {
		frame := nextChatFrame(t, events)
		require.Equal(t, chatID, frame["message"].(map[string]any)["chatID"])
		if frame["type"] == "Modified" {
			break
		}
	}
}

// A question has no run, so it names no provider and no effort: the provider is
// null rather than a row labelled by an empty id, and the effort is empty.
func TestAQuestionNamesNoProvider(t *testing.T) {
	srv := newChatServer(t)
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)

	resp, events := chatFrames(t, srv,
		`subscription { chatMessageCollectionWatch(chatID: "`+sent["chatSend"].(map[string]any)["chatID"].(string)+`") { message { role provider { id } effort } } }`)
	defer resp.Body.Close()

	frame := awaitChatFrame(t, events, func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["role"] == "User"
	})
	assert.Equal(t, map[string]any{"role": "User", "provider": nil, "effort": ""}, frame["message"])
}

// A question carries the cluster card as its first block, where the webview reads
// it. The test server's card source can read nothing, so the card is the
// unavailable one: still a card, still first.
func TestAQuestionCarriesTheClusterCardFirst(t *testing.T) {
	srv := newChatServer(t)
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)

	resp, events := chatFrames(t, srv,
		`subscription { chatMessageCollectionWatch(chatID: "`+sent["chatSend"].(map[string]any)["chatID"].(string)+`") { message { role content } } }`)
	defer resp.Body.Close()

	frame := awaitChatFrame(t, events, func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["role"] == "User"
	})
	blocks := frame["message"].(map[string]any)["content"].([]any)
	require.Len(t, blocks, 2)
	assert.Equal(t, "context", blocks[0].(map[string]any)["type"])
	assert.Contains(t, blocks[0].(map[string]any)["text"], "unavailable")
	assert.Equal(t, map[string]any{"type": "text", "text": "hi"}, blocks[1])
}

// A message whose provider the service no longer holds is labelled by the stored
// id, with no dialect: what was run on is known, what it spoke is not.
func TestAMessageOutlivesItsProvidersRow(t *testing.T) {
	srv, db, _ := newChatServerOver(t)
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)
	_, err := db.Write.Exec(`UPDATE agent_runs SET provider = 'gone'`)
	require.NoError(t, err)

	resp, events := chatFrames(t, srv,
		`subscription { chatMessageCollectionWatch(chatID: "`+sent["chatSend"].(map[string]any)["chatID"].(string)+`") { message { role status provider { id label llmDialect } } } }`)
	defer resp.Body.Close()

	// The settled row, not the live one: while the turn runs the watch serves the
	// overlay, which carries the provider the send named rather than the row's.
	frame := awaitChatFrame(t, events, func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["role"] == "Assistant" && msg["status"] == "Complete"
	})
	assert.Equal(t, map[string]any{"id": "gone", "label": "gone", "llmDialect": nil}, frame["message"].(map[string]any)["provider"])
}

// A send the service refuses carries its code too: the mutation is the one the
// composer branches on.
func TestARefusedSendCarriesItsCode(t *testing.T) {
	srv := newChatServer(t)

	raw := postGQL(t, srv.URL, `{"query":"mutation { chatSend(chatID: \"`+appdb.NewID()+`\", mode: Chat, clusterID: \"1\", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: \"fake\", modelID: \"fake\", effort: \"high\", requestID: \"`+appdb.NewID()+`\", content: \"hi\") { id } }"}`)

	assert.Contains(t, string(raw), `"code":"KUBETAIL_RECORD_NOT_FOUND"`)
}

// A send under a cluster no row holds is refused as not found.
func TestChatSendForwardsAClusterRefusal(t *testing.T) {
	srv := newChatServer(t)

	raw := postGQL(t, srv.URL, `{"query":"mutation { chatSend(mode: Chat, clusterID: \"999\", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: \"fake\", modelID: \"fake\", effort: \"high\", requestID: \"`+appdb.NewID()+`\", content: \"hi\") { id } }"}`)

	assert.Contains(t, string(raw), `"code":"KUBETAIL_RECORD_NOT_FOUND"`)
}

// A refusal the table names carries its code, which is the only thing that crosses
// GraphQL: a Go error's identity does not.
func TestAChatRefusalCarriesItsCode(t *testing.T) {
	srv := newChatServer(t)

	raw := postGQL(t, srv.URL, `{"query":"mutation { chatRename(id: \"`+appdb.NewID()+`\", title: \"t\") { id } }"}`)

	assert.Contains(t, string(raw), `"code":"KUBETAIL_RECORD_NOT_FOUND"`)
}

// Every refusal chatErr maps reaches the client as its code, one case per entry of
// chatRefusals (TestTheChatRefusalTableIsPinned holds the count).
func TestChatRefusalsCarryTheirCode(t *testing.T) {
	send := func(effort string) string {
		return `{"query":"mutation { chatSend(mode: Chat, clusterID: \"1\", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: \"fake\", modelID: \"fake\", effort: \"` + effort +
			`\", requestID: \"` + appdb.NewID() + `\", content: \"hi\") { id } }"}`
	}
	for _, tc := range []struct {
		err  error
		code string
	}{
		{chat.ErrBadRequest, "KUBETAIL_VALIDATION_ERROR"},
		{chat.ErrChatGone, "KUBETAIL_RECORD_NOT_FOUND"},
		{chat.ErrClusterGone, "KUBETAIL_RECORD_NOT_FOUND"},
		{chat.ErrTurnInFlight, "KUBETAIL_CONFLICT"},
		{chat.ErrStopping, "KUBETAIL_SERVICE_UNAVAILABLE"},
		{chat.ErrChatContextFull, "KUBETAIL_CHAT_CONTEXT_FULL"},
		{chat.ErrChatSandboxChanged, "KUBETAIL_CHAT_SANDBOX_CHANGED"},
		{chat.ErrGrantGone, "KUBETAIL_RECORD_NOT_FOUND"},
		{chat.ErrChatNetworkChanged, "KUBETAIL_CHAT_NETWORK_CHANGED"},
	} {
		t.Run(tc.err.Error(), func(t *testing.T) {
			srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{ChatSvc: refusingChat{err: tc.err}}))
			defer srv.Close()

			raw := postGQL(t, srv.URL, send("high"))

			assert.Contains(t, string(raw), `"code":"`+tc.code+`"`)
		})
	}

	// Send wraps the catalog's refusal in ErrBadRequest, which chatErr sees through.
	t.Run("an effort the model does not list", func(t *testing.T) {
		raw := postGQL(t, newChatServer(t).URL, send("max"))

		assert.Contains(t, string(raw), `"code":"KUBETAIL_VALIDATION_ERROR"`)
	})
}

// The chat's other mutations map their refusals through the same table.
func TestChatMutationRefusalsCarryTheirCode(t *testing.T) {
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{ChatSvc: refusingChat{err: chat.ErrChatGone}}))
	defer srv.Close()
	for _, mutation := range []string{
		`chatCancel(chatID: \"` + appdb.NewID() + `\")`,
		`approvalDecide(id: \"` + appdb.NewID() + `\", decision: Once)`,
		`backgroundTaskStop(id: \"` + appdb.NewID() + `\")`,
	} {
		raw := postGQL(t, srv.URL, `{"query":"mutation { `+mutation+` }"}`)

		assert.Contains(t, string(raw), `"code":"KUBETAIL_RECORD_NOT_FOUND"`, mutation)
	}
}

// A rule an answer would write that Kubetail cannot apply is a validation error
// saying why, so the request names it rather than a lost decision.
func TestApprovalDecideCarriesARuleRefusal(t *testing.T) {
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		ChatSvc: refusingChat{err: fmt.Errorf("write the rule: %w", settings.RuleRefusal("allows a destructive write, which always asks"))},
	}))
	defer srv.Close()

	message, code := refusalOf(t, srv, `mutation { approvalDecide(id: "`+appdb.NewID()+`", decision: Always) }`)

	assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", code)
	assert.Equal(t, "allows a destructive write, which always asks", message)
}

// The chat's reads map their refusals through the same table: the rules query, and
// both watches at the subscribe.
func TestChatReadRefusalsCarryTheirCode(t *testing.T) {
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{ChatSvc: refusingChat{err: chat.ErrChatGone}}))
	defer srv.Close()

	_, code := refusalOf(t, srv, `{ chatPermissionRulesGet(chatID: "`+appdb.NewID()+`") { id } }`)
	assert.Equal(t, "KUBETAIL_RECORD_NOT_FOUND", code)

	for _, query := range []string{
		`subscription { chatCollectionWatch { type } }`,
		`subscription { chatMessageCollectionWatch(chatID: "` + appdb.NewID() + `") { type } }`,
	} {
		resp, events := chatFrames(t, srv, query)
		assert.Contains(t, testutil.Recv(t, events, "the refusal").data, `"code":"KUBETAIL_RECORD_NOT_FOUND"`, query)
		resp.Body.Close()
	}
}

// Two refusals in one request are two errors: gqlgen stamps a path onto the error a
// resolver returns, so a shared value would report the first field for both.
func TestChatRefusalsDoNotShareErrorState(t *testing.T) {
	srv := newChatServer(t)

	raw := postGQL(t, srv.URL, `{"query":"mutation { alpha: chatRename(id: \"`+appdb.NewID()+`\", title: \"t\") { id } beta: chatRename(id: \"`+appdb.NewID()+`\", title: \"t\") { id } }"}`)

	var resp struct {
		Errors []struct {
			Path       []string
			Extensions map[string]any
		}
	}
	require.NoError(t, json.Unmarshal(raw, &resp), "%s", raw)
	require.Len(t, resp.Errors, 2, "%s", raw)
	assert.Equal(t, []string{"alpha"}, resp.Errors[0].Path)
	assert.Equal(t, []string{"beta"}, resp.Errors[1].Path)
	assert.Equal(t, "KUBETAIL_RECORD_NOT_FOUND", resp.Errors[1].Extensions["code"])
}

// A failure the table does not name stays opaque: a store failure is not something
// a client can act on, so it carries no code of ours.
func TestAFailedChatStoreCarriesNoCode(t *testing.T) {
	srv, db, _ := newChatServerOver(t)
	require.NoError(t, db.Close())

	raw := postGQL(t, srv.URL, `{"query":"mutation { chatDelete(id: \"`+appdb.NewID()+`\") }"}`)

	assert.Contains(t, string(raw), `"errors"`)
	assert.NotContains(t, string(raw), `"code":"KUBETAIL_`)
}

// Both chat watches go through subscriptionStream, so a read that fails ends the
// subscription with the reason under the subscriptionFailed extension rather than leaving the
// client on a stream that has stopped.
func TestAChatCollectionWatchOverAFailedStoreEndsWithItsReason(t *testing.T) {
	srv, db, _ := newChatServerOver(t)
	require.NoError(t, db.Close())

	for _, tc := range []struct{ query, read string }{
		{`subscription { chatCollectionWatch { type } }`, "list chats"},
		{`subscription { chatMessageCollectionWatch(chatID: "` + appdb.NewID() + `") { type } }`, "list messages"},
	} {
		resp, events := chatFrames(t, srv, tc.query)
		ev := testutil.Recv(t, events, "the failure frame")
		assert.Contains(t, ev.data, `"subscriptionFailed":true`)
		assert.Contains(t, ev.data, tc.read)
		resp.Body.Close()
	}
}

// The models state watch is every provider's catalog under its provider, dialect included.
func TestTheModelsStateWatchListsEveryProvidersCatalog(t *testing.T) {
	srv := newChatServer(t)

	data := firstFrame(t, srv.URL, `subscription { modelsStateWatch { provider { id label llmDialect } id label efforts defaultEffort } }`)

	got, _ := json.Marshal(data)
	assert.JSONEq(t, `{"modelsStateWatch":[{
		"provider":{"id":"fake","label":"Fake","llmDialect":"Fake"},
		"id":"fake","label":"Fake model","efforts":["low","high"],"defaultEffort":"low"},{
		"provider":{"id":"fake","label":"Fake","llmDialect":"Fake"},
		"id":"fake-no-tools","label":"Fake model (no tools)","efforts":[],"defaultEffort":""}]}`, string(got))
}

// approvalDecide reaches the service, and a decision nothing waits on answers false
// rather than an error.
func TestApprovalDecideReachesTheService(t *testing.T) {
	srv := newChatServer(t)

	for _, d := range []string{"Once", "Command", "Chat", "Always", "Deny"} {
		data := mutate(t, srv, `mutation { approvalDecide(id: "`+appdb.NewID()+`", decision: `+d+`) }`)
		assert.Equal(t, false, data["approvalDecide"], d)
	}
}

// An answer carries its turn's tool calls off their rows: a call of a tool the turn
// was not offered is listed, not run, with the refusal the model read and what it
// asked for.
func TestAMessageCarriesItsToolCalls(t *testing.T) {
	srv, _, fake := newChatServerOver(t)
	fake.SetToolCalls(llm.StagedCall("Bash", `{"command":"ls","description":"List files"}`))
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID toolCalls { id } } }`)
	answer := sent["chatSend"].(map[string]any)
	assert.Equal(t, []any{}, answer["toolCalls"], "a new answer has none")

	resp, events := chatFrames(t, srv, `subscription { chatMessageCollectionWatch(chatID: "`+answer["chatID"].(string)+`") {
		message { seq status toolCalls { toolUseID name arguments status
			action { description command { text cwd background env } } approval { id } output isError } } } }`)
	defer resp.Body.Close()

	frame := awaitChatFrame(t, events, func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["seq"] == float64(1) && msg["status"] == "Complete"
	})
	assert.Equal(t, []any{map[string]any{
		"toolUseID": "call-1", "name": "Bash", "arguments": map[string]any{"command": "ls", "description": "List files"}, "status": "NotRun",
		"action": map[string]any{
			"description": "List files",
			"command":     map[string]any{"text": "ls", "cwd": "", "background": false, "env": []any{}},
		},
		"approval": nil, "output": `{"error":"unknown-tool"}`, "isError": true,
	}}, frame["message"].(map[string]any)["toolCalls"])
}

// A call carries its sandbox actions in the order asked, a cluster's and a
// cloud platform's in one list: one that waits with its body and subresource,
// one decided or abandoned, and a pending one on a call no longer running, with
// neither body nor media type.
func TestToolCallCarriesItsSandboxActions(t *testing.T) {
	srv, db, fake := newChatServerOver(t)
	fake.SetToolCalls(llm.StagedCall("Bash", `{"command":"kubectl delete pod x"}`))
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)
	chatID := sent["chatSend"].(map[string]any)["chatID"].(string)
	query := `subscription { chatMessageCollectionWatch(chatID: "` + chatID + `") {
		message { seq status toolCalls { sandboxActions { approval { id status } action { target } request {
			... on ClusterWrite { method path subresource contentType body dryRun }
			... on CloudRequest { host method path contentType body unshown details { label value } } } } } } } }`
	settled := func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["seq"] == float64(1) && msg["status"] == "Complete"
	}
	writes := func() []any {
		resp, events := chatFrames(t, srv, query)
		defer resp.Body.Close()
		f := awaitChatFrame(t, events, settled)
		return f["message"].(map[string]any)["toolCalls"].([]any)[0].(map[string]any)["sandboxActions"].([]any)
	}
	resp, events := chatFrames(t, srv, query)
	awaitChatFrame(t, events, settled)
	resp.Body.Close()

	var callID string
	require.NoError(t, db.Read.QueryRow(`SELECT id FROM tool_calls`).Scan(&callID))
	_, err := db.Write.Exec(`UPDATE tool_calls SET status = 'running', started_at = 1, finished_at = NULL WHERE id = ?`, callID)
	require.NoError(t, err)
	ids := []string{appdb.NewID(), appdb.NewID(), appdb.NewID(), appdb.NewID()}
	for i, row := range []struct{ status, request string }{
		{"approved", `{"action":{"target":"kubernetes"},"write":{"method":"DELETE","path":"/api/v1/namespaces/web/pods/a","subresource":"","contentType":"","body":"","dryRun":false}}`},
		{"abandoned", `{"action":{"target":"kubernetes"},"write":{"method":"PATCH","path":"/apis/apps/v1/namespaces/web/deployments/b?dryRun=All","subresource":"","contentType":"application/merge-patch+json","body":"{}","dryRun":true}}`},
		{"pending", `{"action":{"target":"aws","accessLevel":"write"},"cloud":{"host":"ec2.amazonaws.com","method":"POST","path":"/","contentType":"text/plain","body":"Action=X","details":[{"label":"Region","value":"eu-west-1"}]}}`},
		{"pending", `{"action":{"target":"kubernetes"},"write":{"method":"POST","path":"/api/v1/namespaces/web/pods/c/eviction","subresource":"eviction","contentType":"application/json","body":"{\"kind\":\"Eviction\"}","dryRun":false}}`},
	} {
		_, err := db.Write.Exec(`INSERT INTO approvals (id, tool_call_id, kind, request, status, created_at) VALUES (?, ?, 'action', ?, ?, ?)`,
			ids[i], callID, row.request, row.status, 10+i)
		require.NoError(t, err)
	}

	cluster := map[string]any{"target": "Kubernetes"}
	assert.Equal(t, []any{
		map[string]any{"approval": map[string]any{"id": ids[0], "status": "Approved"}, "action": cluster, "request": map[string]any{
			"method": "DELETE", "path": "/api/v1/namespaces/web/pods/a", "subresource": "", "contentType": "", "body": "", "dryRun": false,
		}},
		map[string]any{"approval": map[string]any{"id": ids[1], "status": "Abandoned"}, "action": cluster, "request": map[string]any{
			"method": "PATCH", "path": "/apis/apps/v1/namespaces/web/deployments/b?dryRun=All", "subresource": "", "contentType": "", "body": "", "dryRun": true,
		}},
		map[string]any{"approval": map[string]any{"id": ids[2], "status": "Pending"}, "action": map[string]any{"target": "AWS"}, "request": map[string]any{
			"host": "ec2.amazonaws.com", "method": "POST", "path": "/", "contentType": "text/plain", "body": "Action=X", "unshown": "",
			"details": []any{map[string]any{"label": "Region", "value": "eu-west-1"}},
		}},
		map[string]any{"approval": map[string]any{"id": ids[3], "status": "Pending"}, "action": cluster, "request": map[string]any{
			"method": "POST", "path": "/api/v1/namespaces/web/pods/c/eviction", "subresource": "eviction", "contentType": "application/json",
			"body": `{"kind":"Eviction"}`, "dryRun": false,
		}},
	}, writes())

	_, err = db.Write.Exec(`UPDATE tool_calls SET status = 'failed', error = '{"error":"stranded"}', finished_at = 2 WHERE id = ?`, callID)
	require.NoError(t, err)
	for _, i := range []int{2, 3} {
		stranded := writes()[i].(map[string]any)["request"].(map[string]any)
		assert.Equal(t, "", stranded["body"], "a pending action on a call no longer running does not wait")
		assert.Equal(t, "", stranded["contentType"])
	}
}

// A memory call's action carries who the note reaches, which is what the approval
// request asks about.
func TestAMessageCarriesAMemoryCallsScope(t *testing.T) {
	srv, _, fake := newChatServerOver(t)
	fake.SetToolCalls(llm.StagedCall("Memory", `{"op":"save","name":"prefs","body":"b","scope":"everywhere"}`))
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)

	resp, events := chatFrames(t, srv, `subscription { chatMessageCollectionWatch(chatID: "`+sent["chatSend"].(map[string]any)["chatID"].(string)+`") {
		message { seq status toolCalls { action { memory { op name body scope } } } } } }`)
	defer resp.Body.Close()

	frame := awaitChatFrame(t, events, func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["seq"] == float64(1) && msg["status"] == "Complete"
	})
	assert.Equal(t, []any{map[string]any{
		"action": map[string]any{"memory": map[string]any{"op": "save", "name": "prefs", "body": "b", "scope": "everywhere"}},
	}}, frame["message"].(map[string]any)["toolCalls"])
}

// A search is the provider's call, drawn by its query, and the answer carries what
// its text cites.
func TestAMessageCarriesItsSearchesAndCitations(t *testing.T) {
	srv, _, fake := newChatServerOver(t)
	fake.SetReply(
		llm.Chunk{Kind: llm.ChunkServer, Call: llm.ServerUseBlock("srv_1", anthropicwebsearch.Name, json.RawMessage(`{"query":"kubernetes 1.36"}`))},
		llm.Chunk{Text: "It shipped."},
	)
	fake.SetCitations(llm.Citation{Type: "web_search_result_location", URL: "https://kubernetes.io/releases", Title: "Releases", CitedText: "1.36"})
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID citations { url } } }`)
	answer := sent["chatSend"].(map[string]any)
	assert.Equal(t, []any{}, answer["citations"], "a new answer cites nothing")

	resp, events := chatFrames(t, srv, `subscription { chatMessageCollectionWatch(chatID: "`+answer["chatID"].(string)+`") {
		message { seq status toolCalls { name contract status runsOn action { search { query } } } citations { type url title citedText } } } }`)
	defer resp.Body.Close()

	frame := awaitChatFrame(t, events, func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["seq"] == float64(1) && msg["status"] == "Complete"
	})
	msg := frame["message"].(map[string]any)
	assert.Equal(t, []any{map[string]any{
		"name": "anthropic_web_search_20260318", "contract": "web_search_20260318", "status": nil, "runsOn": "Provider",
		"action": map[string]any{"search": map[string]any{"query": "kubernetes 1.36"}},
	}}, msg["toolCalls"])
	assert.Equal(t, []any{map[string]any{
		"type": "web_search_result_location", "url": "https://kubernetes.io/releases", "title": "Releases", "citedText": "1.36",
	}}, msg["citations"])
}

// Every kind is served as its enum value: gqlgen serves a kind the binding lacks
// as "", with no error.
func TestEveryActionKindIsServed(t *testing.T) {
	srv, _, fake := newChatServerOver(t)
	fake.SetReply(llm.Chunk{Kind: llm.ChunkServer, Call: llm.ServerUseBlock("srv_1", anthropicwebsearch.Name, json.RawMessage(`{"query":"q"}`))})
	fake.SetToolCalls(
		llm.StagedCall(bash.Name, `{"command":"ls"}`),
		llm.StagedCall(read.Name, `{"file_path":"/a"}`),
		llm.StagedCall(write.Name, `{"file_path":"/a","content":""}`),
		llm.StagedCall(edit.Name, `{"file_path":"/a","old_string":"a","new_string":"b"}`),
		llm.StagedCall(webfetch.Name, `{"url":"https://a.test/"}`),
		llm.StagedCall(taskstop.Name, `{"task_id":"t"}`),
		llm.StagedCall(memorytool.Name, `{"op":"forget","name":"a"}`),
		llm.StagedCall(agent.Name, `{"description":"d","prompt":"p"}`),
		llm.StagedCall(kubequery.Name, `{"sql":"SELECT 1"}`),
		llm.StagedCall(logsview.Name, `{"sources":[{"namespace":"prod","resource":"deployments/webapp"}]}`),
		llm.StagedCall(logsread.Name, `{"sources":[{"namespace":"prod","resource":"deployments/webapp"}]}`),
		llm.StagedCall(logswatch.Name, `{"sources":[{"namespace":"prod","resource":"deployments/webapp"}]}`),
	)
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)

	resp, events := chatFrames(t, srv, `subscription { chatMessageCollectionWatch(chatID: "`+sent["chatSend"].(map[string]any)["chatID"].(string)+`") {
		message { seq status toolCalls { actionKind } } } }`)
	defer resp.Body.Close()

	frame := awaitChatFrame(t, events, func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["seq"] == float64(1) && msg["status"] == "Complete"
	})
	var served []any
	for _, c := range frame["message"].(map[string]any)["toolCalls"].([]any) {
		served = append(served, c.(map[string]any)["actionKind"])
	}
	var want []any
	for _, k := range tools.ActionKinds {
		want = append(want, enumOfKind[k])
	}
	assert.ElementsMatch(t, want, served)
}

// enumOfKind is each kind's ToolActionKind value. kube-query is KubeQuery, not
// the kind with its first letter capitalised.
var enumOfKind = map[tools.ActionKind]string{
	tools.ActionCommand: "Command", tools.ActionRead: "Read", tools.ActionWrite: "Write", tools.ActionEdit: "Edit",
	tools.ActionSearch: "Search", tools.ActionFetch: "Fetch", tools.ActionStop: "Stop", tools.ActionMemory: "Memory",
	tools.ActionDelegate: "Delegate", tools.ActionKubeQuery: "KubeQuery", tools.ActionLogsView: "LogsView",
	tools.ActionLogsRead: "LogsRead", tools.ActionLogsWatch: "LogsWatch",
}

// A subagent's call is served in its parent's answer, naming the Agent call it ran
// under; the answer's own calls name none.
func TestASubagentsCallIsServedUnderItsAgentCall(t *testing.T) {
	srv, _, fake := newChatServerOver(t)
	fake.SetToolCalls(llm.StagedCall(agent.Name, `{"description":"d","prompt":"p"}`))
	fake.Route("p").SetToolCalls(llm.StagedCall(taskstop.Name, `{"task_id":"t"}`))
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)

	resp, events := chatFrames(t, srv, `subscription { chatMessageCollectionWatch(chatID: "`+sent["chatSend"].(map[string]any)["chatID"].(string)+`") {
		message { seq status toolCalls { id name agentCallID action { delegate { prompt agentType model } } } } } }`)
	defer resp.Body.Close()

	// The subagent runs in the background, so its call can land after the answer settles.
	frame := awaitChatFrame(t, events, func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["seq"] == float64(1) && msg["status"] == "Complete" && len(msg["toolCalls"].([]any)) == 2
	})
	calls := frame["message"].(map[string]any)["toolCalls"].([]any)
	agent, sub := calls[0].(map[string]any), calls[1].(map[string]any)
	assert.Nil(t, agent["agentCallID"])
	assert.Equal(t, map[string]any{"delegate": map[string]any{"prompt": "p", "agentType": "general-purpose", "model": ""}}, agent["action"])
	assert.Equal(t, taskstop.Name, sub["name"])
	assert.Equal(t, agent["id"], sub["agentCallID"])
}

// A call that started a background task carries the task off its row: its status,
// and the exit code once it exited with one; its action says whether it asked to
// run in the background.
func TestAToolCallCarriesItsBackgroundTask(t *testing.T) {
	srv, db, fake := newChatServerOver(t)
	fake.SetToolCalls(llm.StagedCall("Bash", `{"command":"make serve","run_in_background":true}`))
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)
	chatID := sent["chatSend"].(map[string]any)["chatID"].(string)
	resp, events := chatFrames(t, srv, `subscription { chatMessageCollectionWatch(chatID: "`+chatID+`") { message { seq status } } }`)
	awaitChatFrame(t, events, func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["seq"] == float64(1) && msg["status"] == "Complete"
	})
	resp.Body.Close()
	for _, q := range []string{
		`INSERT INTO approvals (id, tool_call_id, status, created_at) SELECT 'a', id, 'approved', 0 FROM tool_calls`,
		`INSERT INTO background_tasks (id, chat_id, tool_call_id, output_path, status, exit_code, started_at, finished_at)
		 SELECT 'task-1', '` + chatID + `', id, '/r/task-1.output', 'exited', 3, 0, 0 FROM tool_calls`,
	} {
		_, err := db.Write.Exec(q)
		require.NoError(t, err, q)
	}

	resp, events = chatFrames(t, srv, `subscription { chatMessageCollectionWatch(chatID: "`+chatID+`") {
		message { seq toolCalls { action { command { background } } approval { status } background { status exitCode } } } } }`)
	defer resp.Body.Close()
	frame := awaitChatFrame(t, events, func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["seq"] == float64(1)
	})
	assert.Equal(t, []any{map[string]any{
		"action":     map[string]any{"command": map[string]any{"background": true}},
		"approval":   map[string]any{"status": "Approved"},
		"background": map[string]any{"status": "Exited", "exitCode": float64(3)},
	}}, frame["message"].(map[string]any)["toolCalls"])
}

// backgroundTaskStop reaches the service, and a call that started no running
// task answers false rather than an error.
func TestBackgroundTaskStopReachesTheService(t *testing.T) {
	srv := newChatServer(t)

	data := mutate(t, srv, `mutation { backgroundTaskStop(id: "`+appdb.NewID()+`") }`)

	assert.Equal(t, false, data["backgroundTaskStop"])
}

// awaitChatFrame reads frames until one satisfies want, and returns it.
func awaitChatFrame(t *testing.T, events <-chan sseEvent, want func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.After(testutil.Timeout)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("the stream closed before the frame arrived")
			}
			if f := chatFrameOf(t, ev); f != nil && want(f) {
				return f
			}
		case <-deadline:
			t.Fatal("timed out waiting for a frame")
		}
	}
}

// firstChatFrameType is the type of the first frame the stream carries.
func firstChatFrameType(t *testing.T, events <-chan sseEvent) string {
	t.Helper()
	return nextChatFrame(t, events)["type"].(string)
}

// nextChatFrame is the next watch frame, whatever it holds.
func nextChatFrame(t *testing.T, events <-chan sseEvent) map[string]any {
	t.Helper()
	return awaitChatFrame(t, events, func(map[string]any) bool { return true })
}

// chatFrameOf is the one watch frame an event carries, or nil for a frame that is
// not one (gqlgen's keep-alives and its terminal event).
func chatFrameOf(t *testing.T, ev sseEvent) map[string]any {
	t.Helper()
	var payload struct {
		Data   map[string]any
		Errors []struct{ Message string }
	}
	if err := json.Unmarshal([]byte(ev.data), &payload); err != nil {
		return nil
	}
	require.Empty(t, payload.Errors, "%s", ev.data)
	for _, v := range payload.Data {
		if f, ok := v.(map[string]any); ok {
			return f
		}
	}
	return nil
}

// --- memory ---

type noUIDs struct{}

func (noUIDs) ServerUID(context.Context, apimeta.ClusterID) string { return "" }

// newMemoryServer is a GraphQL server over a real memory service, with clusters
// "1" and "2" seeded.
func newMemoryServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv, _, _ := newMemoryServerOver(t)
	return srv
}

// newMemoryServerOver is newMemoryServer handing back the app.db, for a test that
// fails the store by closing it, and the service's stop.
func newMemoryServerOver(t *testing.T) (*httptest.Server, *appdb.DB, func(context.Context) error) {
	t.Helper()
	db := appdbtest.Open(t)
	for _, id := range []string{"1", "2"} {
		_, err := db.Write.Exec(`INSERT INTO clusters (id, source, source_key, created_at, updated_at) VALUES (?, 'kubeconfig', ?, 0, 0)`, id, "ctx-"+id)
		require.NoError(t, err)
	}
	memorySvc, err := memory.New(db, noUIDs{})
	require.NoError(t, err)
	stop, err := memorySvc.Start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, stop(context.Background()))
		require.NoError(t, memorySvc.Close())
	})
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{MemorySvc: memorySvc}))
	t.Cleanup(srv.Close)
	return srv, db, stop
}

// A failure the table does not name stays opaque, and a watch opened as the
// service stops is refused as unavailable.
func TestAFailedMemoryStoreCarriesNoCode(t *testing.T) {
	srv, db, stop := newMemoryServerOver(t)
	require.NoError(t, db.Close())

	body, _ := json.Marshal(map[string]string{"query": memorySave(`clusterID: "1", name: "n", body: "b"`)})
	raw := string(postGQL(t, srv.URL, string(body)))
	assert.Contains(t, raw, `"errors"`)
	assert.NotContains(t, raw, `"code":"KUBETAIL_`)

	require.NoError(t, stop(t.Context()))
	body, _ = json.Marshal(map[string]string{"query": `subscription { memoryCollectionWatch(clusterID: "1") { type } }`})
	assert.Contains(t, string(postGQL(t, srv.URL, string(body))), `"code":"KUBETAIL_SERVICE_UNAVAILABLE"`)
}

// memorySave is the mutation for one input, as GraphQL text.
func memorySave(input string) string {
	return `mutation { memorySave(input: {` + input + `}) { id clusterID name body writtenBy } }`
}

func TestMemorySaveCreatesAndRewrites(t *testing.T) {
	srv := newMemoryServer(t)

	data := mutate(t, srv, memorySave(`clusterID: "1", name: "pages", body: "b"`))
	created := data["memorySave"].(map[string]any)
	id := created["id"].(string)
	delete(created, "id")
	assert.Equal(t, map[string]any{
		"clusterID": "1", "name": "pages", "body": "b",
		"writtenBy": "User",
	}, created)

	data = mutate(t, srv, memorySave(`id: "`+id+`", clusterID: null, name: "renamed", body: "b2"`))
	assert.Equal(t, "renamed", data["memorySave"].(map[string]any)["name"])
	assert.Nil(t, data["memorySave"].(map[string]any)["clusterID"])

	assert.Equal(t, map[string]any{"memoryDelete": true}, mutate(t, srv, `mutation { memoryDelete(id: "`+id+`") }`))
}

// Each refusal carries its code; the dialog branches on the code alone.
func TestAMemoryRefusalCarriesItsCode(t *testing.T) {
	srv := newMemoryServer(t)
	mutate(t, srv, memorySave(`clusterID: "1", name: "pages", body: "b"`))
	mutate(t, srv, memorySave(`clusterID: "2", name: "theirs", body: "b"`))

	for code, op := range map[string]string{
		"KUBETAIL_MEMORY_NAME_TAKEN": memorySave(`clusterID: "2", name: "theirs", body: "b"`),
		"KUBETAIL_MEMORY_SECRET":     memorySave(`clusterID: "1", name: "s", body: "sk-ant-0123456789abcdefghij"`),
		"KUBETAIL_VALIDATION_ERROR":  memorySave(`clusterID: "1", name: "Bad Name", body: "b"`),
		"KUBETAIL_RECORD_NOT_FOUND":  `mutation { memoryDelete(id: "` + appdb.NewID() + `") }`,
	} {
		t.Run(code, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"query": op})
			raw := postGQL(t, srv.URL, string(body))
			assert.Contains(t, string(raw), `"code":"`+code+`"`)
		})
	}

	full := strings.Repeat("b", memory.BodyMax)
	for i := range 20 {
		body, _ := json.Marshal(map[string]string{"query": memorySave(`clusterID: "1", name: "n` + strconv.Itoa(i) + `", body: "` + full + `"`)})
		raw := string(postGQL(t, srv.URL, string(body)))
		if strings.Contains(raw, `"errors"`) {
			assert.Contains(t, raw, `"code":"KUBETAIL_MEMORY_FULL"`)
			return
		}
	}
	t.Fatal("the scope never filled")
}

func TestMemoryCollectionWatchOpensWithASnapshot(t *testing.T) {
	srv := newMemoryServer(t)
	mutate(t, srv, memorySave(`clusterID: "1", name: "pages", body: "b"`))
	mutate(t, srv, memorySave(`clusterID: "2", name: "theirs", body: "b"`))

	resp, events := chatFrames(t, srv, `subscription { memoryCollectionWatch(clusterID: "1") { type memory { name } } }`)
	defer resp.Body.Close()

	first := awaitChatFrame(t, events, func(map[string]any) bool { return true })
	assert.Equal(t, map[string]any{"type": "Added", "memory": map[string]any{"name": "pages"}}, first)
	assert.Equal(t, "Bookmark", firstChatFrameType(t, events))
}

// A plan's usage limit reaches the wire as its kind, which the transcript branches
// on; error is the text it draws.
func TestAUsageLimitArrivesWithItsKind(t *testing.T) {
	srv, _, fake := newChatServerOver(t)
	plan := llm.Provider{ID: "fake", Codes: map[string]llm.CodeClass{"usage_limit_reached": llm.CodeUsageLimit}}
	fake.FailAfter(0, llm.ResponseError(plan, 429, "", "usage_limit_reached"))
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)
	chatID := sent["chatSend"].(map[string]any)["chatID"].(string)

	resp, events := chatFrames(t, srv, `subscription { chatMessageCollectionWatch(chatID: "`+chatID+`") { message { seq status error failure } } }`)
	defer resp.Body.Close()
	frame := awaitChatFrame(t, events, func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["seq"] == float64(1) && msg["status"] == "Failed"
	})

	msg := frame["message"].(map[string]any)
	assert.Equal(t, "UsageLimit", msg["failure"])
	assert.NotEmpty(t, msg["error"])
}

// An agent's task serves its status and report, and an answer and its chat say
// when a run under them waits on the user.
func TestAnAgentsTaskAndWaitAreServed(t *testing.T) {
	srv, db, fake := newChatServerOver(t)
	fake.SetToolCalls(llm.StagedCall("Bash", `{"command":"make serve"}`))
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)
	chatID := sent["chatSend"].(map[string]any)["chatID"].(string)
	resp, events := chatFrames(t, srv, `subscription { chatMessageCollectionWatch(chatID: "`+chatID+`") { message { seq status } } }`)
	awaitChatFrame(t, events, func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["seq"] == float64(1) && msg["status"] == "Complete"
	})
	resp.Body.Close()
	for _, q := range []string{
		`INSERT INTO agent_runs (id, parent_run_id, agent_type, app_version, trigger, chat_id, provider, model, dialect, task, result, status, created_at)
		 SELECT 'sub', run_id, 'general-purpose', 'test', 'agent', chat_id, 'fake', 'fake', 'fake', 'p', 'Two pods.', 'waiting_approval', 0
		 FROM messages WHERE seq = 1`,
		`UPDATE tool_calls SET spawned_run_id = 'sub'`,
		`INSERT INTO background_tasks (id, chat_id, tool_call_id, output_path, status, started_at, finished_at)
		 SELECT 'task-1', '` + chatID + `', id, '/r/task-1.output', 'completed', 0, 0 FROM tool_calls`,
	} {
		_, err := db.Write.Exec(q)
		require.NoError(t, err, q)
	}

	resp, events = chatFrames(t, srv, `subscription { chatMessageCollectionWatch(chatID: "`+chatID+`") {
		message { seq awaitingApproval toolCalls { background { status report } } } } }`)
	defer resp.Body.Close()
	frame := awaitChatFrame(t, events, func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["seq"] == float64(1)
	})
	msg := frame["message"].(map[string]any)
	assert.Equal(t, true, msg["awaitingApproval"])
	assert.Equal(t, []any{map[string]any{
		"background": map[string]any{"status": "Completed", "report": "Two pods."},
	}}, msg["toolCalls"])

	list, chats := chatFrames(t, srv, `subscription { chatCollectionWatch { chat { id awaitingApproval } } }`)
	defer list.Body.Close()
	awaitChatFrame(t, chats, func(f map[string]any) bool {
		chat, ok := f["chat"].(map[string]any)
		return ok && chat["id"] == chatID && chat["awaitingApproval"] == true
	})
}

// A KubeQuery call serves the statement as it runs and the limit as read, beside the
// model's description.
func TestAKubeQueryCallServesItsQuery(t *testing.T) {
	srv, _, fake := newChatServerOver(t)
	fake.SetToolCalls(llm.StagedCall(kubequery.Name, `{"sql":"SELECT 1;","limit":5,"description":"Count the pods"}`))
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)

	resp, events := chatFrames(t, srv, `subscription { chatMessageCollectionWatch(chatID: "`+sent["chatSend"].(map[string]any)["chatID"].(string)+`") {
		message { seq status toolCalls { action { description kubeQuery { sql limit } } } } } }`)
	defer resp.Body.Close()

	frame := awaitChatFrame(t, events, func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["seq"] == float64(1) && msg["status"] == "Complete"
	})
	assert.Equal(t, []any{map[string]any{"action": map[string]any{
		"description": "Count the pods", "kubeQuery": map[string]any{"sql": "SELECT 1", "limit": float64(5)},
	}}}, frame["message"].(map[string]any)["toolCalls"])
}

// A LogsView call serves the view it opened, with the moment it was resolved.
func TestALogsViewCallServesWhenItResolved(t *testing.T) {
	srv, _, fake := newChatServerOver(t)
	fake.SetToolCalls(llm.StagedCall(logsview.Name, `{"sources":[{"namespace":"prod","resource":"deployments/webapp"}]}`))
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)

	resp, events := chatFrames(t, srv, `subscription { chatMessageCollectionWatch(chatID: "`+sent["chatSend"].(map[string]any)["chatID"].(string)+`") {
		message { seq status toolCalls { action { logsView { pinToEnd resolvedAt } } } } } }`)
	defer resp.Body.Close()

	frame := awaitChatFrame(t, events, func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["seq"] == float64(1) && msg["status"] == "Complete"
	})
	assert.Equal(t, []any{map[string]any{"action": map[string]any{
		"logsView": map[string]any{"pinToEnd": true, "resolvedAt": "2026-10-08T14:04:00Z"},
	}}}, frame["message"].(map[string]any)["toolCalls"])
}

// sandboxStateGet answers the state the app built, reason and all.
func TestSandboxStateGetAnswersTheState(t *testing.T) {
	srv := newSandboxedChatServer(t, sandbox.State{Available: true, Reason: "bwrap at /usr/bin/bwrap", NetworkReason: "pasta not found"})

	data := mutate(t, srv, `{ sandboxStateGet { available reason networkAvailable networkReason } }`)

	assert.Equal(t, map[string]any{
		"available": true, "reason": "bwrap at /usr/bin/bwrap", "networkAvailable": false, "networkReason": "pasta not found",
	}, data["sandboxStateGet"])
}

// The network switch answers the chat it committed, and the send carries the
// switch its sender saw and the turn's toggle: a stale switch is refused, and
// so is the toggle where network is unavailable.
func TestChatNetworkEnabledSetServesTheSwitchedChat(t *testing.T) {
	srv := newSandboxedChatServer(t, sandbox.State{Available: true, NetworkAvailable: true})
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: true, sandboxDisabledThisTurn: false,
		providerID: "fake", modelID: "fake", effort: "high", requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)
	chatID := sent["chatSend"].(map[string]any)["chatID"].(string)

	data := mutate(t, srv, `mutation { chatNetworkEnabledSet(id: "`+chatID+`", enabled: true) { id networkEnabled } }`)
	assert.Equal(t, map[string]any{"id": chatID, "networkEnabled": true}, data["chatNetworkEnabledSet"])
	raw := postGQL(t, srv.URL, `{"query":"mutation { chatNetworkEnabledSet(id: \"`+appdb.NewID()+`\", enabled: false) { id } }"}`)
	assert.Contains(t, string(raw), `"code":"KUBETAIL_RECORD_NOT_FOUND"`)

	raw = postGQL(t, srv.URL, `{"query":"mutation { chatSend(chatID: \"`+chatID+`\", mode: Chat, clusterID: \"1\", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: \"fake\", modelID: \"fake\", effort: \"high\", requestID: \"`+appdb.NewID()+`\", content: \"and?\") { id } }"}`)
	assert.Contains(t, string(raw), `"code":"KUBETAIL_CHAT_NETWORK_CHANGED"`)

	none := newSandboxedChatServer(t, sandbox.State{Available: true, NetworkReason: "pasta not found"})
	raw = postGQL(t, none.URL, `{"query":"mutation { chatSend(mode: Chat, clusterID: \"1\", sandboxDisabled: false, networkEnabled: false, networkThisTurn: true, sandboxDisabledThisTurn: false, providerID: \"fake\", modelID: \"fake\", effort: \"high\", requestID: \"`+appdb.NewID()+`\", content: \"hi\") { id } }"}`)
	assert.Contains(t, string(raw), `"code":"KUBETAIL_VALIDATION_ERROR"`)
}

// The switch answers the chat it committed, and is refused on a machine with no
// sandbox.
func TestChatSandboxDisabledSetServesTheSwitchedChat(t *testing.T) {
	srv := newSandboxedChatServer(t, sandbox.State{Available: true})
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)
	chatID := sent["chatSend"].(map[string]any)["chatID"].(string)

	data := mutate(t, srv, `mutation { chatSandboxDisabledSet(id: "`+chatID+`", sandboxDisabled: true) { id sandboxDisabled } }`)

	assert.Equal(t, map[string]any{"id": chatID, "sandboxDisabled": true}, data["chatSandboxDisabledSet"])

	none := newChatServer(t)
	sent = mutate(t, none, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)
	chatID = sent["chatSend"].(map[string]any)["chatID"].(string)
	raw := postGQL(t, none.URL, `{"query":"mutation { chatSandboxDisabledSet(id: \"`+chatID+`\", sandboxDisabled: true) { id } }"}`)
	assert.Contains(t, string(raw), `"code":"KUBETAIL_VALIDATION_ERROR"`)
}

// refusal is errs' one error's code and message.
func refusal(t *testing.T, errs []gqlError) (code, message string) {
	t.Helper()
	require.Len(t, errs, 1)
	code, _ = errs[0].Extensions["code"].(string)
	return code, errs[0].Message
}

// newPermissionServer is a server over the cluster fixtures and settings over
// an app.db holding their clusters, and a cloud cluster with no kube-context.
func newPermissionServer(t *testing.T) *httptest.Server {
	t.Helper()
	db := appdbtest.Open(t)
	for _, q := range []string{
		`INSERT INTO clusters (id, source, source_key, created_at, updated_at) VALUES ('1', 'kubeconfig', 'prod', 0, 0), ('2', 'kubeconfig', 'staging', 0, 0)`,
		`INSERT INTO clusters (id, source, source_key, created_at, updated_at) VALUES ('3', 'cloud', '3', 0, 0)`,
	} {
		_, err := db.Write.Exec(q)
		require.NoError(t, err)
	}
	cfg, err := settings.Open(db)
	require.NoError(t, err)
	// A cluster with no kube-context has no mode of its own, so Settings lists none for it.
	clusters := append(clusterFixtures(), clusterFixture{id: "3"})
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		ClusterSvc: newFakeClusterService(clusters), Settings: settings.NewService(cfg, nil, nil, nil, ""),
		CloudTargets: []permissions.Target{permissions.AWS, permissions.GitHub, permissions.GCP, permissions.Azure},
	}))
	t.Cleanup(srv.Close)
	return srv
}

const permissionSettingsFields = `targets { target defaultMode accounts { account name clusterID mode source } }
	rules { effect accessLevel principal target scope service operation resource line }`

// refusalOf posts query and answers its one error's message and code.
func refusalOf(t *testing.T, srv *httptest.Server, query string) (string, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"query": query})
	raw := postGQL(t, srv.URL, string(body))
	var resp struct {
		Errors []struct {
			Message    string
			Extensions struct{ Code string }
		}
	}
	require.NoError(t, json.Unmarshal(raw, &resp), "%s", raw)
	require.Len(t, resp.Errors, 1, "%s", raw)
	return resp.Errors[0].Message, resp.Errors[0].Extensions.Code
}

// targetOf is the modes of target in a permission settings answer.
func targetOf(t *testing.T, settings any, target string) map[string]any {
	t.Helper()
	for _, m := range settings.(map[string]any)["targets"].([]any) {
		if m := m.(map[string]any); m["target"] == target {
			return m
		}
	}
	t.Fatalf("no target %s", target)
	return nil
}

// permissionSettings answers each target's default mode and its accounts'
// modes, the always rules, and the two lists in words.
func TestPermissionSettingsShowsModesAndRules(t *testing.T) {
	srv := newPermissionServer(t)
	mutate(t, srv, `mutation { permissionModeSet(target: Kubernetes, account: "1", mode: ReadOnly) { rules { id } } }`)
	mutate(t, srv, `mutation { permissionModeSet(target: AWS, account: "123456789012", mode: Auto) { rules { id } } }`)
	mutate(t, srv, `mutation { permissionRuleAdd(input: {effect: Deny, accessLevel: Destructive, target: Kubernetes, scope: "1", service: "", operation: "delete", resource: "namespaces"}) { rules { id } } }`)

	data := mutate(t, srv, `{ permissionSettingsGet { `+permissionSettingsFields+` alwaysAsks accessControl } }`)

	got := data["permissionSettingsGet"].(map[string]any)
	kube := targetOf(t, got, "Kubernetes")
	assert.Equal(t, "Ask", kube["defaultMode"])
	assert.ElementsMatch(t, []any{
		map[string]any{"account": "1", "name": "prod", "clusterID": "1", "mode": "ReadOnly", "source": "Entry"},
		map[string]any{"account": "2", "name": "staging", "clusterID": "2", "mode": "Ask", "source": "Default"},
	}, kube["accounts"])
	aws := targetOf(t, got, "AWS")
	assert.Equal(t, []any{map[string]any{"account": "123456789012", "name": "123456789012", "clusterID": nil, "mode": "Auto", "source": "Entry"}}, aws["accounts"])
	assert.Equal(t, []any{map[string]any{
		"effect": "Deny", "accessLevel": "Destructive", "principal": nil, "target": "Kubernetes", "scope": "1",
		"service": "", "operation": "delete", "resource": "namespaces",
		"line": "Deny destructive delete of core namespaces in prod",
	}}, got["rules"])
	assert.NotEmpty(t, got["alwaysAsks"])
	assert.NotEmpty(t, got["accessControl"])
}

// Each mutation writes the settings and answers them as they now stand.
func TestPermissionMutationsWriteTheSettings(t *testing.T) {
	srv := newPermissionServer(t)
	modeOf := func(data map[string]any, field, target, account string) map[string]any {
		for _, c := range targetOf(t, data[field], target)["accounts"].([]any) {
			if m := c.(map[string]any); m["account"] == account {
				return m
			}
		}
		return nil
	}
	const accounts = `targets { target defaultMode accounts { account mode source } }`

	data := mutate(t, srv, `mutation { permissionModeSet(target: Kubernetes, account: "1", mode: Auto) { `+accounts+` } }`)
	assert.Equal(t, map[string]any{"account": "1", "mode": "Auto", "source": "Entry"}, modeOf(data, "permissionModeSet", "Kubernetes", "1"))

	data = mutate(t, srv, `mutation { permissionRuleAdd(input: {effect: Allow, accessLevel: Write, target: Kubernetes, scope: "2/web"}) {
		rules { id line } } }`)
	rules := data["permissionRuleAdd"].(map[string]any)["rules"].([]any)
	require.Len(t, rules, 1)
	added := rules[0].(map[string]any)
	assert.Equal(t, "Allow writes in staging / web", added["line"])
	assert.NoError(t, appdb.ValidateUUID(added["id"].(string)), "the store mints the id")

	data = mutate(t, srv, `mutation { permissionRuleUpdate(id: "`+added["id"].(string)+`", input: {effect: Ask, accessLevel: Write, target: Kubernetes, scope: "2/api"}) {
		rules { id line } } }`)
	assert.Equal(t, []any{map[string]any{"id": added["id"], "line": "Ask for writes in staging / api"}},
		data["permissionRuleUpdate"].(map[string]any)["rules"], "an edit keeps the rule's id")

	data = mutate(t, srv, `mutation { permissionRuleRemove(id: "`+added["id"].(string)+`") { rules { id } } }`)
	assert.Empty(t, data["permissionRuleRemove"].(map[string]any)["rules"])

	data = mutate(t, srv, `mutation { permissionRuleAdd(input: {effect: Deny, accessLevel: DataRead, principal: Subagent}) { rules { principal target line } } }`)
	assert.Equal(t, []any{map[string]any{"principal": "Subagent", "target": nil, "line": "Deny data reads by subagent everywhere"}},
		data["permissionRuleAdd"].(map[string]any)["rules"])

	data = mutate(t, srv, `mutation { permissionModeClear(target: Kubernetes, account: "1") { `+accounts+` } }`)
	assert.Equal(t, map[string]any{"account": "1", "mode": "Ask", "source": "Default"}, modeOf(data, "permissionModeClear", "Kubernetes", "1"))

	data = mutate(t, srv, `mutation { permissionDefaultModeSet(target: Kubernetes, mode: Auto) { `+accounts+` } }`)
	assert.Equal(t, "Auto", targetOf(t, data["permissionDefaultModeSet"], "Kubernetes")["defaultMode"])
	assert.Equal(t, map[string]any{"account": "2", "mode": "Auto", "source": "Default"}, modeOf(data, "permissionDefaultModeSet", "Kubernetes", "2"))
	assert.Equal(t, "Ask", targetOf(t, data["permissionDefaultModeSet"], "GitHub")["defaultMode"], "another target's default is its own")

	data = mutate(t, srv, `mutation { permissionModeSet(target: GitHub, account: "kubetail-org", mode: ReadOnly) { `+accounts+` } }`)
	assert.Equal(t, map[string]any{"account": "kubetail-org", "mode": "ReadOnly", "source": "Entry"}, modeOf(data, "permissionModeSet", "GitHub", "kubetail-org"))
	data = mutate(t, srv, `mutation { permissionModeClear(target: GitHub, account: "kubetail-org") { `+accounts+` } }`)
	assert.Empty(t, targetOf(t, data["permissionModeClear"], "GitHub")["accounts"], "a cloud account with no mode is not listed")
}

// A rule Kubetail cannot apply, an unknown rule, and a mode for an account
// Settings does not list are refused, each saying why.
func TestAPermissionRefusalCarriesItsCode(t *testing.T) {
	srv := newPermissionServer(t)
	for query, want := range map[string]struct{ code, message string }{
		`mutation { permissionRuleAdd(input: {effect: Allow, accessLevel: Destructive, target: Kubernetes, scope: "1"}) { rules { id } } }`:         {"KUBETAIL_VALIDATION_ERROR", "allows a destructive change"},
		`mutation { permissionRuleAdd(input: {effect: Allow, accessLevel: Read, target: Kubernetes, scope: "1"}) { rules { id } } }`:                {"KUBETAIL_VALIDATION_ERROR", "names reads or reach"},
		`mutation { permissionRuleRemove(id: "nope") { rules { id } } }`:                                                                            {"KUBETAIL_VALIDATION_ERROR", "no rule has this id"},
		`mutation { permissionRuleUpdate(id: "nope", input: {effect: Deny, accessLevel: Write}) { rules { id } } }`:                                 {"KUBETAIL_VALIDATION_ERROR", "no rule has this id"},
		`mutation { permissionRuleUpdate(id: "nope", input: {effect: Allow, accessLevel: Read, target: Kubernetes, scope: "1"}) { rules { id } } }`: {"KUBETAIL_VALIDATION_ERROR", "names reads or reach"},
		`mutation { permissionModeSet(target: Kubernetes, account: "9", mode: Auto) { rules { id } } }`:                                             {"KUBETAIL_RECORD_NOT_FOUND", ""},
		`mutation { permissionModeClear(target: Kubernetes, account: "9") { rules { id } } }`:                                                       {"KUBETAIL_RECORD_NOT_FOUND", ""},
		`mutation { permissionModeSet(target: Kubernetes, account: "3", mode: Auto) { rules { id } } }`:                                             {"KUBETAIL_VALIDATION_ERROR", "kube-context"},
		`mutation { permissionModeSet(target: AWS, account: "a/b", mode: Auto) { rules { id } } }`:                                                  {"KUBETAIL_VALIDATION_ERROR", "an account is one name"},
		`mutation { permissionModeSet(target: AWS, account: "*", mode: Auto) { rules { id } } }`:                                                    {"KUBETAIL_VALIDATION_ERROR", "an account is one name"},
		`mutation { permissionModeClear(target: AWS, account: "*") { rules { id } } }`:                                                              {"KUBETAIL_VALIDATION_ERROR", "an account is one name"},
		`mutation { permissionRuleAdd(input: {effect: Allow, accessLevel: Write}) { rules { id } } }`:                                               {"KUBETAIL_VALIDATION_ERROR", "allows on every target or account"},
		`mutation { permissionRuleAdd(input: {effect: Deny, accessLevel: Write, target: Kubernetes, scope: "9/**"}) { rules { id } } }`:             {"KUBETAIL_RECORD_NOT_FOUND", ""},
		`mutation { permissionRuleAdd(input: {effect: Deny, accessLevel: Write, target: Kubernetes, scope: "3"}) { rules { id } } }`:                {"KUBETAIL_VALIDATION_ERROR", "kube-context"},
	} {
		message, code := refusalOf(t, srv, query)
		assert.Equal(t, want.code, code, query)
		assert.Contains(t, message, want.message, query)
	}
	data := mutate(t, srv, `{ permissionSettingsGet { rules { id } } }`)
	assert.Empty(t, data["permissionSettingsGet"].(map[string]any)["rules"], "nothing was written")
}

// The settings read the clusters off the cluster service, so a list it cannot
// answer fails the query rather than drawing no clusters.
func TestPermissionSettingsFailsWithTheClusterList(t *testing.T) {
	svc := newFakeClusterService(clusterFixtures())
	svc.listErr = errors.New("store closed")
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{ClusterSvc: svc, Settings: testSettings(t)}))
	t.Cleanup(srv.Close)

	_, code := refusalOf(t, srv, `{ permissionSettingsGet { rules { id } } }`)

	assert.NotEqual(t, "KUBETAIL_VALIDATION_ERROR", code)
}

// A change that took, whose answer could not be read after, says it took:
// KUBETAIL_SAVED_UNREAD, never a refusal.
func TestAPermissionChangeThatTookSaysSoWhenTheAnswerFails(t *testing.T) {
	db := appdbtest.Open(t)
	cfg, err := settings.Open(db)
	require.NoError(t, err)
	svc := newFakeClusterService(clusterFixtures())
	svc.listErr = errors.New("store closed")
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{ClusterSvc: svc, Settings: settings.NewService(cfg, nil, nil, nil, "")}))
	t.Cleanup(srv.Close)

	message, code := refusalOf(t, srv, `mutation { permissionRuleAdd(input: {effect: Deny, accessLevel: Write}) { rules { id } } }`)

	assert.Equal(t, "KUBETAIL_SAVED_UNREAD", code)
	assert.Contains(t, message, "The change was saved")
	rules, err := cfg.Rules(t.Context())
	require.NoError(t, err)
	assert.Len(t, rules, 1, "the rule took")
}

// A read of the modes or the rules that fails fails the query: Settings never
// draws a default it did not read.
func TestPermissionSettingsFailsWithTheirRead(t *testing.T) {
	for name, c := range map[string]struct {
		table    string
		clusters []clusterFixture
	}{
		"the modes": {"permission_modes", clusterFixtures()},
		"the rules": {"permission_rules", clusterFixtures()},
	} {
		db := appdbtest.Open(t)
		cfg, err := settings.Open(db)
		require.NoError(t, err)
		_, err = db.Write.Exec(`DROP TABLE ` + c.table)
		require.NoError(t, err)
		srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{ClusterSvc: newFakeClusterService(c.clusters), Settings: settings.NewService(cfg, nil, nil, nil, "")}))
		t.Cleanup(srv.Close)

		message, code := refusalOf(t, srv, `{ permissionSettingsGet { rules { id } } }`)
		assert.NotEqual(t, "KUBETAIL_VALIDATION_ERROR", code, name)
		assert.Contains(t, message, c.table, name)
	}
}

// A cluster write carries the action the proxy classified it as: its summary
// and scope, whether a rule may allow it, and the rule each allow answer adds.
func TestAClusterWriteCarriesItsAction(t *testing.T) {
	srv, db, fake := newChatServerOver(t)
	fake.SetToolCalls(llm.StagedCall("Bash", `{"command":"kubectl delete pod x"}`))
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)
	chatID := sent["chatSend"].(map[string]any)["chatID"].(string)
	query := `subscription { chatMessageCollectionWatch(chatID: "` + chatID + `") {
		message { seq status toolCalls { sandboxActions { action { target summary accessLevel principal scope accountName service operation resource name once grantable commandRule chatRule } } } } } }`
	settled := func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["seq"] == float64(1) && msg["status"] == "Complete"
	}
	resp, events := chatFrames(t, srv, query)
	awaitChatFrame(t, events, settled)
	resp.Body.Close()

	var callID string
	require.NoError(t, db.Read.QueryRow(`SELECT id FROM tool_calls`).Scan(&callID))
	_, err := db.Write.Exec(`INSERT INTO approvals (id, tool_call_id, kind, request, status, created_at) VALUES (?, ?, 'action', ?, 'denied', 1)`,
		appdb.NewID(), callID, `{"action":{"principal":"","target":"kubernetes","accessLevel":"write","scope":"","accountName":"dev",
		"service":"","operation":"delete","resource":"pods","name":"x","summary":"Delete pods/x in web on dev","dryRun":false,
		"confined":false,"once":false},"grantable":true,
		"commandRule":"Allow delete of core pods in dev / web for this command","chatRule":"Allow writes in dev / web",
		"write":{"method":"DELETE","path":"/api/v1/namespaces/web/pods/x"}}`)
	require.NoError(t, err)

	resp, events = chatFrames(t, srv, query)
	defer resp.Body.Close()
	f := awaitChatFrame(t, events, settled)
	writes := f["message"].(map[string]any)["toolCalls"].([]any)[0].(map[string]any)["sandboxActions"].([]any)
	assert.Equal(t, []any{map[string]any{"action": map[string]any{
		"target": "Kubernetes", "summary": "Delete pods/x in web on dev", "accessLevel": "Write", "principal": nil,
		"scope": "", "accountName": "dev", "service": "", "operation": "delete", "resource": "pods", "name": "x",
		"once": false, "grantable": true,
		"commandRule": "Allow delete of core pods in dev / web for this command", "chatRule": "Allow writes in dev / web",
	}}}, writes)
}

// A rule's line names its cluster off the cluster service, so a read that
// fails fails the line, and a rule naming no cluster reads none.
func TestARulesLineFailsWithItsClustersRead(t *testing.T) {
	wantErr := errors.New("store closed")
	rules := (&graph.Resolver{ClusterSvc: errClusterService{err: wantErr}}).PermissionRule()
	rule := func(effect permissions.Effect, target, scope string) *permissions.Rule {
		return &permissions.Rule{Effect: effect, AccessLevel: permissions.Write, Principal: "*", Target: target, Scope: scope, Service: "*", Operation: "*", Resource: "*"}
	}

	_, err := rules.Line(t.Context(), rule(permissions.Allow, "kubernetes", "1/**"))
	assert.ErrorIs(t, err, wantErr)
	line, err := rules.Line(t.Context(), rule(permissions.Deny, "*", "*"))
	require.NoError(t, err)
	assert.Equal(t, "Deny writes everywhere", line)
	line, err = rules.Line(t.Context(), rule(permissions.Allow, "aws", "123456789012/eu-west-1"))
	require.NoError(t, err)
	assert.Equal(t, "Allow writes in AWS 123456789012 / eu-west-1", line, "a cloud account is named by itself")
}

// An action's principal is null for a stored action that names none.
func TestAnActionsPrincipalIsNullWhenItNamesNone(t *testing.T) {
	actions := (&graph.Resolver{}).PermissionAction()
	got, err := actions.Principal(t.Context(), &chat.PermissionAction{Action: permissions.Action{Principal: permissions.Subagent}})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, permissions.Subagent, *got)
	got, err = actions.Principal(t.Context(), &chat.PermissionAction{})
	require.NoError(t, err)
	assert.Nil(t, got)
}

// chatPermissionRules lists a chat's rules by their lines, and
// chatPermissionRuleRemove answers the list without the one removed; an id the
// chat does not hold and a chat that is gone are each not found.
func TestChatPermissionRulesListAndRemove(t *testing.T) {
	srv, db, _ := newChatServerOver(t)
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)
	chatID := sent["chatSend"].(map[string]any)["chatID"].(string)
	id := appdb.NewID()
	_, err := db.Write.Exec(`INSERT INTO permission_rules (id, chat_id, effect, access_level, principal, target, scope, service, operation, resource, created_at, updated_at)
		VALUES (?, ?, 'allow', 'write', '*', 'kubernetes', '1/web', '*', '*', '*', 1, 1)`, id, chatID)
	require.NoError(t, err)

	data := mutate(t, srv, `query { chatPermissionRulesGet(chatID: "`+chatID+`") { id scope line } }`)
	assert.Equal(t, []any{map[string]any{"id": id, "scope": "1/web", "line": "Allow writes in 1 / web"}}, data["chatPermissionRulesGet"], "a cluster the service does not list is named by its id")

	_, code := refusalOf(t, srv, `mutation { chatPermissionRuleRemove(chatID: "`+chatID+`", id: "`+appdb.NewID()+`") { id } }`)
	assert.Equal(t, "KUBETAIL_RECORD_NOT_FOUND", code)
	data = mutate(t, srv, `mutation { chatPermissionRuleRemove(chatID: "`+chatID+`", id: "`+id+`") { id } }`)
	assert.Equal(t, []any{}, data["chatPermissionRuleRemove"])

	_, code = refusalOf(t, srv, `mutation { chatPermissionRuleRemove(chatID: "`+appdb.NewID()+`", id: "`+id+`") { id } }`)
	assert.Equal(t, "KUBETAIL_RECORD_NOT_FOUND", code)
}

// A write carries how long its approval holds, and while it waits its diff;
// one that no longer waits carries none.
func TestAClusterWriteCarriesItsDurationAndDiff(t *testing.T) {
	srv, db, fake := newChatServerOver(t)
	fake.SetToolCalls(llm.StagedCall("Bash", `{"command":"kubectl delete pod x"}`))
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)
	chatID := sent["chatSend"].(map[string]any)["chatID"].(string)
	query := `subscription { chatMessageCollectionWatch(chatID: "` + chatID + `") {
		message { seq status toolCalls { sandboxActions { approval { status duration } request { ... on ClusterWrite { diff diffCut diffError } } } } } } }`
	settled := func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["seq"] == float64(1) && msg["status"] == "Complete"
	}
	resp, events := chatFrames(t, srv, query)
	awaitChatFrame(t, events, settled)
	resp.Body.Close()

	var callID string
	require.NoError(t, db.Read.QueryRow(`SELECT id FROM tool_calls`).Scan(&callID))
	_, err := db.Write.Exec(`UPDATE tool_calls SET status = 'running', started_at = 1, finished_at = NULL WHERE id = ?`, callID)
	require.NoError(t, err)
	request := `{"write":{"method":"PATCH","path":"/api/v1/namespaces/web/configmaps/c"},"diff":"-a\n+b\n","diffCut":true,"diffError":""}`
	for i, row := range []struct{ status, duration any }{{"approved", "chat"}, {"pending", nil}} {
		_, err := db.Write.Exec(`INSERT INTO approvals (id, tool_call_id, kind, request, status, duration, created_at) VALUES (?, ?, 'action', ?, ?, ?, ?)`,
			appdb.NewID(), callID, request, row.status, row.duration, 10+i)
		require.NoError(t, err)
	}

	resp, events = chatFrames(t, srv, query)
	defer resp.Body.Close()
	f := awaitChatFrame(t, events, func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["seq"] == float64(1)
	})
	assert.Equal(t, []any{
		map[string]any{"approval": map[string]any{"status": "Approved", "duration": "Chat"}, "request": map[string]any{"diff": "", "diffCut": false, "diffError": ""}},
		map[string]any{"approval": map[string]any{"status": "Pending", "duration": nil}, "request": map[string]any{"diff": "-a\n+b\n", "diffCut": true, "diffError": ""}},
	}, f["message"].(map[string]any)["toolCalls"].([]any)[0].(map[string]any)["sandboxActions"])
}

// fakeProber probes each tool as found and passing but for helm, which
// fails, and a refused one, which it leaves.
// A probe runs when the test says: StartProbe publishes it running and
// keeps what it was asked, and finish runs it and publishes its end.
type fakeProber struct {
	mu         sync.Mutex
	last       []bash.ExecutableReport
	registered []settings.Executable
	probes     int
	hub        *watch.Hub[bash.ProbeState]
}

func newFakeProber() *fakeProber {
	return &fakeProber{hub: watch.New(bash.ProbeState{})}
}

func (f *fakeProber) StartProbe(registered []settings.Executable) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registered = registered
	f.probes++
	f.hub.Sender().Send(bash.ProbeState{Probing: true, Probes: f.probes, Last: f.last}) //nolint:errcheck // never closed
}

func (f *fakeProber) WatchProbeState() *watch.Receiver[bash.ProbeState] { return f.hub.Receiver() }

// finish runs the probe StartProbe was asked for and publishes its end.
func (f *fakeProber) finish() {
	f.mu.Lock()
	registered := f.registered
	f.mu.Unlock()
	var probed []bash.ExecutableReport
	for _, report := range f.Report(registered) {
		if report.Refusal != "" { // never probed
			probed = append(probed, report)
			continue
		}
		report.Probed, report.Resolved, report.OK, report.Version, report.Error = true, "/bin/"+report.Name, report.Name != "helm", "v1", ""
		if report.Name == "helm" {
			report.Error = "Exit code 1"
		}
		probed = append(probed, report)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last = probed
	f.hub.Sender().Send(bash.ProbeState{Probes: f.probes, Last: probed}) //nolint:errcheck // never closed
}

// Report is bash.ReportOver the last probe.
func (f *fakeProber) Report(registered []settings.Executable) []bash.ExecutableReport {
	f.mu.Lock()
	defer f.mu.Unlock()
	return bash.ReportOver(f.last, registered)
}

// started is how many probes StartProbe was asked for.
func (f *fakeProber) started() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.probes
}

// sandboxExecutablesServer is a server over a fake prober and a settings store,
// on a machine with a sandbox or without.
func sandboxExecutablesServer(t *testing.T, available bool) (*httptest.Server, *fakeProber, *appdb.DB) {
	t.Helper()
	db := appdbtest.Open(t)
	store, err := settings.Open(db)
	require.NoError(t, err)
	prober := newFakeProber()
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		Settings:     settings.NewService(store, nil, nil, nil, ""),
		SandboxState: sandbox.State{Available: available},
		Executables:  prober,
	}))
	t.Cleanup(srv.Close)
	return srv, prober, db
}

// executablesReport is the first sandboxExecutablesStateWatch frame on events that probing
// and names describe: a state watch over two receivers can say one thing twice,
// so a frame is never counted.
func executablesReport(t *testing.T, events <-chan sseEvent, probing bool, names []string) map[string]any {
	t.Helper()
	for {
		ev := testutil.Recv(t, events, "a sandboxExecutablesStateWatch frame")
		var payload struct {
			Data struct {
				Report map[string]any `json:"sandboxExecutablesStateWatch"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(ev.data), &payload), ev.data)
		report := payload.Data.Report
		if report["probing"] == probing && slices.Equal(namesOf(report["executables"]), names) {
			return report
		}
	}
}

const sandboxExecutableFields = `{ name invocation registered probed resolved shim target ok version error refusal }`

// namesOf is each tool's name in tools, a list off the wire.
func namesOf(tools any) []string {
	var names []string
	for _, tl := range tools.([]any) {
		names = append(names, tl.(map[string]any)["name"].(string))
	}
	return names
}

// The watch, the probe, register and remove each answer the whole report in
// the list's order: an executable listed since the last probe unprobed. The probe
// answers the report as it stands and its end reaches the watch, which
// carries whether one runs and re-draws when the registered tools change.
// Each refusal is a KUBETAIL_VALIDATION_ERROR in the user's words.
func TestSandboxExecutableMutationsAnswerTheReport(t *testing.T) {
	srv, prober, _ := sandboxExecutablesServer(t, true)
	curated := []string{"kubectl", "helm", "kustomize", "git", "jq", "yq"}

	resp := openSSESubscription(t, srv.URL, "", `subscription { sandboxExecutablesStateWatch { probing probes executables `+sandboxExecutableFields+` } }`)
	defer resp.Body.Close()
	events := sseEvents(t, resp)
	report := executablesReport(t, events, false, curated)
	assert.Equal(t, float64(0), report["probes"])
	first := report["executables"].([]any)[0].(map[string]any)
	assert.Equal(t, false, first["probed"])
	assert.Equal(t, "not probed yet", first["error"])

	data, _ := mutation(t, srv.URL, `mutation { sandboxExecutablesProbe `+sandboxExecutableFields+` }`)
	assert.Equal(t, false, data["sandboxExecutablesProbe"].([]any)[0].(map[string]any)["probed"], "the report as it stands")
	report = executablesReport(t, events, true, curated)
	assert.Equal(t, false, report["executables"].([]any)[0].(map[string]any)["probed"])
	assert.Equal(t, float64(1), report["probes"])

	prober.finish()
	report = executablesReport(t, events, false, curated)
	assert.Equal(t, float64(1), report["probes"])
	kubectl := report["executables"].([]any)[0].(map[string]any)
	assert.Equal(t, map[string]any{
		"name": "kubectl", "invocation": "kubectl version --client", "registered": false, "probed": true,
		"resolved": "/bin/kubectl", "shim": false, "target": "", "ok": true, "version": "v1", "error": "", "refusal": nil,
	}, kubectl)

	data, _ = mutation(t, srv.URL, `mutation { sandboxExecutableRegister(name: "k9s") `+sandboxExecutableFields+` }`)
	tools := data["sandboxExecutableRegister"].([]any)
	assert.Equal(t, append(slices.Clone(curated), "k9s"), namesOf(tools))
	k9s := tools[len(tools)-1].(map[string]any)
	assert.Equal(t, "k9s --version", k9s["invocation"])
	assert.Equal(t, true, k9s["registered"])
	assert.Equal(t, false, k9s["probed"])
	assert.Equal(t, true, tools[0].(map[string]any)["probed"], "the last probe's reports stay")
	executablesReport(t, events, false, append(slices.Clone(curated), "k9s"))

	data, _ = mutation(t, srv.URL, `mutation { sandboxExecutableRemove(name: "k9s") `+sandboxExecutableFields+` }`)
	assert.Equal(t, curated, namesOf(data["sandboxExecutableRemove"]))
	executablesReport(t, events, false, curated)

	for query, message := range map[string]string{
		`mutation { sandboxExecutableRegister(name: "kubectl") { name } }`:                            "Kubetail probes kubectl already.",
		`mutation { sandboxExecutableRegister(name: "a/b") { name } }`:                                "An executable's name is 1 to 64 letters, digits, ., _, + or -, and does not start with -.",
		`mutation { sandboxExecutableRegister(name: "k9s", invocation: "other --version") { name } }`: "An invocation starts with the executable's name.",
		`mutation { sandboxExecutableRemove(name: "k9s") { name } }`:                                  "That executable is not registered.",
	} {
		_, errs := mutation(t, srv.URL, query)
		code, got := refusal(t, errs)
		assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", code, query)
		assert.Equal(t, message, got, query)
	}
	mutation(t, srv.URL, `mutation { sandboxExecutableRegister(name: "k9s") { name } }`)
	_, errs := mutation(t, srv.URL, `mutation { sandboxExecutableRegister(name: "k9s") { name } }`)
	_, got := refusal(t, errs)
	assert.Equal(t, "That executable is already listed.", got)
}

// A prober that shuts its feed down ends the watch, which has nothing left to report.
func TestSandboxExecutablesStateWatchEndsWithTheProbersFeed(t *testing.T) {
	srv, prober, _ := sandboxExecutablesServer(t, true)
	resp := openSSESubscription(t, srv.URL, "", `subscription { sandboxExecutablesStateWatch { probing executables { name } } }`)
	defer resp.Body.Close()
	events := sseEvents(t, resp)
	executablesReport(t, events, false, []string{"kubectl", "helm", "kustomize", "git", "jq", "yq"})

	prober.hub.Close()

	testutil.WaitClosed(t, events, "the watch to end with the feed")
}

// A machine with no sandbox watches no tools, and refuses the probe and a register.
func TestSandboxExecutablesAreEmptyWithNoSandbox(t *testing.T) {
	srv, _, _ := sandboxExecutablesServer(t, false)

	resp := openSSESubscription(t, srv.URL, "", `subscription { sandboxExecutablesStateWatch { probing executables { name } } }`)
	defer resp.Body.Close()
	executablesReport(t, sseEvents(t, resp), false, nil)
	for _, query := range []string{
		`mutation { sandboxExecutablesProbe { name } }`,
		`mutation { sandboxExecutableRegister(name: "k9s") { name } }`,
	} {
		_, errs := mutation(t, srv.URL, query)
		code, _ := refusal(t, errs)
		assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", code, query)
	}
}

// With no sandbox, an executable registered on a machine that had one can still be
// removed, and the answer is the empty report.
func TestSandboxExecutableRemoveWorksWithNoSandbox(t *testing.T) {
	store, err := settings.Open(appdbtest.Open(t))
	require.NoError(t, err)
	require.NoError(t, store.RegisterExecutable(t.Context(), "k9s", ""))
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		Settings:    settings.NewService(store, nil, nil, nil, ""),
		Executables: newFakeProber(),
	}))
	t.Cleanup(srv.Close)

	data, _ := mutation(t, srv.URL, `mutation { sandboxExecutableRemove(name: "k9s") { name } }`)
	assert.Equal(t, []any{}, data["sandboxExecutableRemove"])
	registered, err := store.Executables(t.Context())
	require.NoError(t, err)
	assert.Empty(t, registered)
}

// A stored row the shape check refuses is listed where it sits with its
// refusal, unprobed and with no error; its name is already listed, and
// Remove takes it while the curated executable of that name stays.
func TestARefusedExecutableIsListedWithItsRefusal(t *testing.T) {
	srv, prober, db := sandboxExecutablesServer(t, true)
	_, err := db.Write.Exec(`INSERT INTO executables (name, invocation, created_at) VALUES ('kubectl', 'kubectl version', 1)`)
	require.NoError(t, err)

	resp := openSSESubscription(t, srv.URL, "", `subscription { sandboxExecutablesStateWatch { probing executables `+sandboxExecutableFields+` } }`)
	defer resp.Body.Close()
	events := sseEvents(t, resp)
	names := []string{"kubectl", "helm", "kustomize", "git", "jq", "yq", "kubectl"}
	executablesReport(t, events, false, names)
	mutation(t, srv.URL, `mutation { sandboxExecutablesProbe { name } }`)
	executablesReport(t, events, true, names)
	prober.finish()
	tools := executablesReport(t, events, false, names)["executables"].([]any)
	assert.Equal(t, map[string]any{
		"name": "kubectl", "invocation": "kubectl version", "registered": true, "probed": false,
		"resolved": "", "shim": false, "target": "", "ok": false, "version": "", "error": "",
		"refusal": "Kubetail probes kubectl already.",
	}, tools[len(tools)-1])
	assert.Equal(t, true, tools[0].(map[string]any)["probed"], "the curated kubectl")

	_, errs := mutation(t, srv.URL, `mutation { sandboxExecutableRegister(name: "kubectl") { name } }`)
	_, got := refusal(t, errs)
	assert.Equal(t, "That executable is already listed.", got)

	data, _ := mutation(t, srv.URL, `mutation { sandboxExecutableRemove(name: "kubectl") { name registered } }`)
	assert.Equal(t, []string{"kubectl", "helm", "kustomize", "git", "jq", "yq"}, namesOf(data["sandboxExecutableRemove"]))
}

// A read of the executables that fails is an error on the probe, which
// starts no probe; a register or a remove whose write took
// answers KUBETAIL_SAVED_UNREAD; and a watch's read on the register's ping ends
// it with the reason.
func TestAnExecutablesReadThatFailsIsAnError(t *testing.T) {
	srv, prober, db := sandboxExecutablesServer(t, true)
	resp := openSSESubscription(t, srv.URL, "", `subscription { sandboxExecutablesStateWatch { probing executables { name } } }`)
	defer resp.Body.Close()
	events := sseEvents(t, resp)
	executablesReport(t, events, false, []string{"kubectl", "helm", "kustomize", "git", "jq", "yq"})

	// A NULL invocation fails every read; a write still takes.
	for _, q := range []string{
		`DROP TABLE executables`,
		`CREATE TABLE executables (name, invocation, created_at)`,
		`INSERT INTO executables VALUES ('stern', NULL, 1)`,
	} {
		_, err := db.Write.Exec(q)
		require.NoError(t, err, q)
	}

	_, errs := mutation(t, srv.URL, `mutation { sandboxExecutablesProbe { name } }`)
	assert.NotEmpty(t, errs, "the probe")
	assert.Zero(t, prober.started(), "no probe started")

	for _, query := range []string{
		`mutation { sandboxExecutableRegister(name: "k9s") { name } }`,
		`mutation { sandboxExecutableRemove(name: "k9s") { name } }`,
	} {
		_, errs = mutation(t, srv.URL, query)
		code, _ := refusal(t, errs)
		assert.Equal(t, "KUBETAIL_SAVED_UNREAD", code, query)
	}

	ev := testutil.Recv(t, events, "the failure frame")
	assert.Contains(t, ev.data, `"subscriptionFailed":true`)
	assert.Contains(t, ev.data, "read the executables")
	testutil.WaitClosed(t, events, "the watch to end")

	resp = openSSESubscription(t, srv.URL, "", `subscription { sandboxExecutablesStateWatch { probing executables { name } } }`)
	defer resp.Body.Close()
	events = sseEvents(t, resp)
	assert.Contains(t, testutil.Recv(t, events, "the failure frame").data, `"subscriptionFailed":true`, "a watch whose first read fails")
	testutil.WaitClosed(t, events, "the second watch to end")
}

// --- Logs ---

// fakeLogs is the log service answering a page or a stream, and keeping what it
// was asked.
type fakeLogs struct {
	page    logs.FetchResponse
	refused error
	stream  *cluster.Stream[logs.TailFrame]
	fetched logs.FetchRequest
	follow  logs.TailRequest
}

func (f *fakeLogs) Fetch(_ context.Context, _ cluster.ClusterID, q logs.FetchRequest) (logs.FetchResponse, error) {
	f.fetched = q
	return f.page, f.refused
}

func (f *fakeLogs) TailFromCursor(_ context.Context, _ cluster.ClusterID, q logs.TailRequest) (logs.TailResponse, error) {
	f.follow = q
	if f.refused != nil {
		return logs.TailResponse{}, f.refused
	}
	return logs.TailResponse{Request: q, Frames: f.stream}, nil
}

// logsScope is a scope's input as the client sends it.
const logsScope = `{sources: [{namespace: "prod", kind: Deployment, name: "web", containers: ["app"], allContainers: false, previous: false}],
filters: [{field: Zone, values: ["eu-1a"]}], grep: "error"}`

// logsLine is a line of prod/web-1/app saying hello.
func logsLine(stamp time.Time) *logline.Record {
	return &logline.Record{Timestamp: stamp, Message: "hello", Stream: logline.StreamKey{Namespace: "prod", Pod: "web-1", Container: "app"}, TruncatedBytes: 3}
}

// A first page with context reaches the service as asked, and its answer comes
// back whole: the request as it ran, its place, its lines and its notices.
func TestLogsFetchAnswersAFirstPage(t *testing.T) {
	stamp := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	next := "next-cursor"
	scope := logs.Scope{
		Sources: []logs.ScopeSource{{Namespace: "prod", Kind: logs.SourceDeployment, Name: "web", Containers: []string{"app"}}},
		Filters: []logs.ScopeFilter{{Field: logs.FilterZone, Values: []string{"eu-1a"}}},
		Grep:    "error",
	}
	fake := &fakeLogs{page: logs.FetchResponse{
		Request: logs.FetchRequest{
			Scope: scope, Window: logs.Window{Since: &stamp, Before: logs.Padding{Lines: 10}, After: logs.Padding{Span: 5 * time.Minute}},
			PageFrom: logs.PageFromFirst, PageSize: 100, MaxMessageBytes: 16387, MaxPageBytes: 1 << 20,
		},
		ResolvedAt: stamp.Add(time.Hour),
		Page:       logs.Page{Next: &next},
		Records:    []logline.Record{*logsLine(stamp)},
		Notices:    []logs.Notice{{Code: logsrefusal.CodePaddingShort, Message: "fewer lines before"}},
	}}
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{Logs: fake}))
	defer srv.Close()

	data, errs := mutation(t, srv.URL, `{ logsFetch(id: "7", request: {scope: `+logsScope+`, since: "2026-10-08T10:00:00Z",
		before: {lines: 10}, after: {span: "PT300S"}, pageFrom: First}) {
		request { sources { name } grep since until before { lines span } after { lines span } pageFrom pageSize maxMessageBytes maxPageBytes }
		resolvedAt page { prev next follow } records { message truncatedBytes cursor } notices { code message } } }`)

	require.Empty(t, errs)
	assert.Equal(t, logs.FetchRequest{
		Scope: scope, Window: logs.Window{Since: &stamp, Before: logs.Padding{Lines: 10}, After: logs.Padding{Span: 5 * time.Minute}},
		PageFrom: logs.PageFromFirst,
	}, fake.fetched)
	page := data["logsFetch"].(map[string]any)
	assert.Equal(t, map[string]any{
		"sources": []any{map[string]any{"name": "web"}}, "grep": "error", "since": "2026-10-08T10:00:00Z", "until": nil,
		"before": map[string]any{"lines": float64(10), "span": ""}, "after": map[string]any{"lines": float64(0), "span": "PT5M"},
		"pageFrom": "First", "pageSize": float64(100), "maxMessageBytes": float64(16387), "maxPageBytes": float64(1 << 20),
	}, page["request"])
	assert.Equal(t, "2026-10-08T11:00:00Z", page["resolvedAt"])
	assert.Equal(t, map[string]any{"prev": nil, "next": "next-cursor", "follow": nil}, page["page"])
	assert.Equal(t, []any{map[string]any{"message": "hello", "truncatedBytes": float64(3), "cursor": logsLine(stamp).Cursor()}}, page["records"])
	assert.Equal(t, []any{map[string]any{"code": "padding-short", "message": "fewer lines before"}}, page["notices"])
}

// A continuation's cursor reaches the service as it came, with seen beside it,
// and its echoed request comes back with no pageFrom.
func TestLogsFetchPassesAContinuationThrough(t *testing.T) {
	fake := &fakeLogs{page: logs.FetchResponse{Request: logs.FetchRequest{PageSize: 100}, Notices: []logs.Notice{}}}
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{Logs: fake}))
	defer srv.Close()

	data, errs := mutation(t, srv.URL, `{ logsFetch(id: "7", request: {cursor: "opaque", seen: ["line-a"]}) { request { pageFrom } records { message } notices { code } } }`)

	require.Empty(t, errs)
	cursor := "opaque"
	assert.Equal(t, logs.FetchRequest{Cursor: &cursor, Seen: []string{"line-a"}}, fake.fetched)
	page := data["logsFetch"].(map[string]any)
	assert.Equal(t, map[string]any{"pageFrom": nil}, page["request"])
	assert.Equal(t, []any{}, page["records"])
	assert.Equal(t, []any{}, page["notices"])
}

// A refusal arrives as a validation error whose rule is its code, and a span the
// wire cannot read is refused before the service is asked.
func TestLogsFetchRefusesWithTheCode(t *testing.T) {
	fake := &fakeLogs{refused: logsrefusal.New(logsrefusal.CodeCursorInvalid, "read again without the cursor")}
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{Logs: fake}))
	defer srv.Close()

	_, errs := mutation(t, srv.URL, `{ logsFetch(id: "7", request: {cursor: "stale"}) { resolvedAt } }`)
	require.Len(t, errs, 1)
	assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", errs[0].Extensions["code"])
	assert.Equal(t, "cursor-invalid", errs[0].Extensions["rule"])
	assert.Equal(t, "read again without the cursor", errs[0].Message)

	fake.fetched = logs.FetchRequest{PageSize: -1}
	_, errs = mutation(t, srv.URL, `{ logsFetch(id: "7", request: {scope: `+logsScope+`, pageFrom: Last, before: {span: "P1M"}}) { resolvedAt } }`)
	require.Len(t, errs, 1)
	assert.Equal(t, "bad-padding", errs[0].Extensions["rule"])
	assert.Equal(t, -1, fake.fetched.PageSize, "the service was not asked")
}

// Null and zero are one thing on the wire, as they are to the service: an empty
// scope beside a cursor is a continuation, which the service reads as far as the
// cursor; one naming a grep is refused beside it.
func TestLogsFetchReadsAnEmptyScopeAsNone(t *testing.T) {
	svc := logs.New(nil, t.TempDir(), logstore.Retention{})
	t.Cleanup(func() { _ = svc.Close() })
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{Logs: svc}))
	defer srv.Close()

	for grep, rule := range map[string]string{"": "cursor-invalid", "error": "conflicting-parameters"} {
		_, errs := mutation(t, srv.URL, `{ logsFetch(id: "7", request: {scope: {sources: [], filters: [], grep: "`+grep+`"}, cursor: "opaque"}) { resolvedAt } }`)
		require.Len(t, errs, 1, grep)
		assert.Equal(t, rule, errs[0].Extensions["rule"], grep)
	}
}

// logsTail passes its request through, then streams a line, a notice and the
// frame that ends it with its code, each bound by its type.
func TestLogsTailStreamsUntilItEnds(t *testing.T) {
	stamp := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	release := make(chan struct{})
	fake := &fakeLogs{stream: cluster.NewStream(t.Context(), func(ctx context.Context, out chan<- logs.TailFrame) error {
		out <- logs.TailFrame{Record: logsLine(stamp)}
		out <- logs.TailFrame{Notice: &logs.Notice{Code: logsrefusal.CodeStreamSkipped, Message: "prod/web-2/app: forbidden"}}
		<-release
		ended := logsrefusal.New(logsrefusal.CodeFinished, "every container of this view has stopped")
		out <- logs.TailFrame{Ended: &ended}
		return nil
	})}
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{Logs: fake}))
	defer srv.Close()

	resp, events := chatFrames(t, srv, `subscription { logsTail(id: "7", request: {cursor: "c", seen: ["line-a"], maxMessageBytes: 100}) {
		type record { message truncatedBytes stream { pod } } notice { code message } ended { code message } } }`)
	defer resp.Body.Close()

	line := testutil.Recv(t, events, "the line").data
	assert.Contains(t, line, `"type":"Line","record":{"message":"hello","truncatedBytes":3,"stream":{"pod":"web-1"}},"notice":null,"ended":null`)
	cursor := "c"
	assert.Equal(t, logs.TailRequest{Cursor: &cursor, Seen: []string{"line-a"}, MaxMessageBytes: 100}, fake.follow)
	assert.Contains(t, testutil.Recv(t, events, "the notice").data, `"type":"Notice","record":null,"notice":{"code":"stream-skipped","message":"prod/web-2/app: forbidden"},"ended":null`)
	close(release)
	ev := testutil.Recv(t, events, "the end")
	assert.Contains(t, ev.data, `"type":"Ended","record":null,"notice":null,"ended":{"code":"finished","message":"every container of this view has stopped"}`)
	assert.NotContains(t, ev.data, `"subscriptionFailed"`)
}

// A tail whose Err is set ends with the reason under the subscriptionFailed extension.
func TestLogsTailReportsItsFailure(t *testing.T) {
	fake := &fakeLogs{stream: cluster.NewStream(t.Context(), func(context.Context, chan<- logs.TailFrame) error {
		return errors.New("db closed")
	})}
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{Logs: fake}))
	defer srv.Close()

	resp, events := chatFrames(t, srv, `subscription { logsTail(id: "7", request: {scope: `+logsScope+`}) { type } }`)
	defer resp.Body.Close()

	assert.Contains(t, testutil.Recv(t, events, "the failure frame").data, `"subscriptionFailed":true`)
}

// A tail the service refuses at the subscribe carries the refusal's rule, as a
// fetch's does.
func TestLogsTailCarriesARefusal(t *testing.T) {
	fake := &fakeLogs{refused: logsrefusal.New(logsrefusal.CodeCursorInvalid, "read again without the cursor")}
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{Logs: fake}))
	defer srv.Close()

	resp, events := chatFrames(t, srv, `subscription { logsTail(id: "7", request: {scope: `+logsScope+`}) { type } }`)
	defer resp.Body.Close()

	ev := testutil.Recv(t, events, "the refusal").data
	assert.Contains(t, ev, `"rule":"cursor-invalid"`)
	assert.NotContains(t, ev, `"subscriptionFailed"`)
}

// newCloudServer is a resolver over a fresh app.db whose
// build has the cloud platforms targets.
func newCloudServer(t *testing.T, targets ...permissions.Target) (*httptest.Server, *settings.Store, *appdb.DB) {
	t.Helper()
	db := appdbtest.Open(t)
	cfg, err := settings.Open(db)
	require.NoError(t, err)
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		ClusterSvc: newFakeClusterService(clusterFixtures()), Settings: settings.NewService(cfg, nil, nil, nil, ""), CloudTargets: targets,
	}))
	t.Cleanup(srv.Close)
	return srv, cfg, db
}

// switches is cfg's cloud platform switches.
func switches(t *testing.T, cfg *settings.Store) settings.CloudPlatformSwitches {
	t.Helper()
	sw, err := cfg.CloudPlatformSwitches(t.Context())
	require.NoError(t, err)
	return sw
}

// newSandboxDefaultServer is a server over a fresh app.db holding the cluster
// dev, on a machine with a sandbox unless none.
func newSandboxDefaultServer(t *testing.T, none bool) (*httptest.Server, *settings.Store) {
	t.Helper()
	db := appdbtest.Open(t)
	_, err := db.Write.Exec(`INSERT INTO clusters (id, source, source_key, created_at, updated_at) VALUES ('dev', 'kubeconfig', 'dev', 0, 0)`)
	require.NoError(t, err)
	cfg, err := settings.Open(db)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cfg.Close()) })
	zones := func() settings.Zones { return settings.Zones{} }
	if none {
		zones = nil
	}
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		ClusterSvc: newFakeClusterService(clusterFixtures()), Settings: settings.NewService(cfg, zones, nil, nil, ""),
	}))
	t.Cleanup(srv.Close)
	return srv, cfg
}

// sandboxDefaultSet writes a scope's default and answers it; a cluster's own
// value overrides every cluster's, and a reset takes it back.
func TestSandboxDefaultSetAnswersTheScopesDefault(t *testing.T) {
	srv, cfg := newSandboxDefaultServer(t, false)

	data := mutate(t, srv, `mutation { sandboxDefaultSet(sandboxDisabled: true) { sandboxDisabled clusterOverride } }`)
	assert.Equal(t, map[string]any{"sandboxDisabled": true, "clusterOverride": false}, data["sandboxDefaultSet"])
	data = mutate(t, srv, `mutation { sandboxDefaultSet(clusterID: "dev", sandboxDisabled: false) { sandboxDisabled clusterOverride } }`)
	assert.Equal(t, map[string]any{"sandboxDisabled": false, "clusterOverride": true}, data["sandboxDefaultSet"])
	data = mutate(t, srv, `mutation { sandboxDefaultReset(clusterID: "dev", sandboxDisabled: true) { sandboxDisabled clusterOverride } }`)
	assert.Equal(t, map[string]any{"sandboxDisabled": true, "clusterOverride": false}, data["sandboxDefaultReset"])

	d, err := cfg.GetSandboxDefault(t.Context(), "dev")
	require.NoError(t, err)
	assert.Equal(t, settings.SandboxDefault{Disabled: true}, d)
}

// A gone cluster is not found, and a machine with no sandbox has no default
// to write.
func TestSandboxDefaultSetRefusals(t *testing.T) {
	srv, _ := newSandboxDefaultServer(t, false)
	_, code := refusalOf(t, srv, `mutation { sandboxDefaultSet(clusterID: "gone", sandboxDisabled: true) { sandboxDisabled } }`)
	assert.Equal(t, "KUBETAIL_RECORD_NOT_FOUND", code)
	_, code = refusalOf(t, srv, `mutation { sandboxDefaultReset(clusterID: "gone", sandboxDisabled: false) { sandboxDisabled } }`)
	assert.Equal(t, "KUBETAIL_RECORD_NOT_FOUND", code)

	// An empty id would name every cluster's scope, which only a null one does.
	_, code = refusalOf(t, srv, `mutation { sandboxDefaultSet(clusterID: "", sandboxDisabled: true) { sandboxDisabled } }`)
	assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", code)
	_, code = refusalOf(t, srv, `mutation { sandboxDefaultReset(clusterID: "", sandboxDisabled: false) { sandboxDisabled } }`)
	assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", code)

	// A reset that would land elsewhere than the caller saw writes nothing.
	mutate(t, srv, `mutation { sandboxDefaultSet(clusterID: "dev", sandboxDisabled: false) { sandboxDisabled } }`)
	mutate(t, srv, `mutation { sandboxDefaultSet(sandboxDisabled: true) { sandboxDisabled } }`)
	_, code = refusalOf(t, srv, `mutation { sandboxDefaultReset(clusterID: "dev", sandboxDisabled: false) { sandboxDisabled } }`)
	assert.Equal(t, "KUBETAIL_CONFLICT", code)

	none, _ := newSandboxDefaultServer(t, true)
	message, code := refusalOf(t, none, `mutation { sandboxDefaultSet(sandboxDisabled: true) { sandboxDisabled } }`)
	assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", code)
	assert.Equal(t, settings.ErrNoSandbox.Error(), message)
}

// The watch sends the scope's default on subscribe, then again after a write
// in any scope that changes it.
func TestSandboxDefaultStateWatchFollowsEveryScope(t *testing.T) {
	srv, _ := newSandboxDefaultServer(t, false)
	resp := openSSESubscription(t, srv.URL, "", `subscription { sandboxDefaultStateWatch(clusterID: "dev") { sandboxDisabled clusterOverride } }`)
	defer resp.Body.Close()
	events := sseEvents(t, resp)
	assert.Contains(t, nextSSE(t, events).data, `{"sandboxDisabled":false,"clusterOverride":false}`)

	mutate(t, srv, `mutation { sandboxDefaultSet(sandboxDisabled: true) { sandboxDisabled } }`)
	assert.Contains(t, nextSSE(t, events).data, `{"sandboxDisabled":true,"clusterOverride":false}`)
	// A write that leaves the scope's value as it was sends nothing: the next
	// frame is the change after it.
	mutate(t, srv, `mutation { sandboxDefaultSet(sandboxDisabled: true) { sandboxDisabled } }`)
	mutate(t, srv, `mutation { sandboxDefaultSet(clusterID: "dev", sandboxDisabled: false) { sandboxDisabled } }`)
	assert.Contains(t, nextSSE(t, events).data, `{"sandboxDisabled":false,"clusterOverride":true}`)
}

// A store that cannot be read fails the watch with its reason and the
// mutation with an error, never a default.
func TestSandboxDefaultReportsAStoreItCannotRead(t *testing.T) {
	srv, cfg := newSandboxDefaultServer(t, false)
	require.NoError(t, cfg.Close())

	resp := openSSESubscription(t, srv.URL, "", `subscription { sandboxDefaultStateWatch { sandboxDisabled } }`)
	defer resp.Body.Close()
	assert.Contains(t, nextSSE(t, sseEvents(t, resp)).data, `"subscriptionFailed":true`)
	message, _ := refusalOf(t, srv, `mutation { sandboxDefaultSet(sandboxDisabled: true) { sandboxDisabled } }`)
	assert.NotEmpty(t, message)
}

// chatSend hands each switch to the chat service under its own name.
func TestChatSendPassesEachSwitch(t *testing.T) {
	switches := make(chan chat.SendSwitches, 4)
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		ClusterSvc: newFakeClusterService(clusterFixtures()), ChatSvc: switchesChat{switches: switches},
	}))
	t.Cleanup(srv.Close)

	for _, want := range []chat.SendSwitches{
		{SandboxDisabled: true},
		{NetworkEnabled: true},
		{NetworkThisTurn: true},
		{SandboxDisabledThisTurn: true},
	} {
		mutate(t, srv, fmt.Sprintf(`mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: %t, networkEnabled: %t, networkThisTurn: %t, sandboxDisabledThisTurn: %t, providerID: "fake", modelID: "fake", effort: "high",
			requestID: %q, content: "hi") { id } }`, want.SandboxDisabled, want.NetworkEnabled, want.NetworkThisTurn, want.SandboxDisabledThisTurn, appdb.NewID()))
		assert.Equal(t, want, testutil.Recv(t, switches, "the send"))
	}
}

// sandboxCloudAccess lists only the cloud platforms the build has, each on
// until turned off; sandboxCloudAccessSet writes its cloud platform's field and
// answers the list.
func TestSandboxCloudAccessListsTheBuildsCloudPlatforms(t *testing.T) {
	srv, cfg, _ := newCloudServer(t, permissions.AWS, permissions.GCP)
	data := mutate(t, srv, `{ sandboxCloudAccessGet { target on } }`)
	assert.Equal(t, []any{
		map[string]any{"target": "AWS", "on": true},
		map[string]any{"target": "GCP", "on": true},
	}, data["sandboxCloudAccessGet"])

	data = mutate(t, srv, `mutation { sandboxCloudAccessSet(target: GCP, on: false) { target on } }`)
	assert.Equal(t, []any{
		map[string]any{"target": "AWS", "on": true},
		map[string]any{"target": "GCP", "on": false},
	}, data["sandboxCloudAccessSet"])
	assert.False(t, switches(t, cfg).GCP)
	assert.True(t, switches(t, cfg).AWS)

	empty, _, _ := newCloudServer(t)
	assert.Equal(t, []any{}, mutate(t, empty, `{ sandboxCloudAccessGet { target on } }`)["sandboxCloudAccessGet"])
}

// The cluster has no switch, and a cloud platform the build lacks has none
// either.
func TestSandboxCloudAccessSetRefusesWhatHasNoSwitch(t *testing.T) {
	srv, cfg, _ := newCloudServer(t, permissions.AWS)
	for _, target := range []string{"Kubernetes", "GitHub"} {
		message, code := refusalOf(t, srv, `mutation { sandboxCloudAccessSet(target: `+target+`, on: false) { target } }`)
		assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", code, target)
		assert.Equal(t, "This build has no switch for that target.", message, target)
	}
	assert.True(t, switches(t, cfg).GitHub, "nothing was written")
}

// A read of the switches that fails is the query's error and the mutation's,
// never a list of switches that are off.
func TestSandboxCloudAccessAnswersAFailedRead(t *testing.T) {
	srv, _, db := newCloudServer(t, permissions.AWS)
	_, err := db.Write.Exec(`DROP TABLE settings`)
	require.NoError(t, err)
	for _, q := range []string{`{ sandboxCloudAccessGet { target on } }`, `mutation { sandboxCloudAccessSet(target: AWS, on: false) { target on } }`} {
		body, _ := json.Marshal(map[string]string{"query": q})
		var resp struct {
			Data   map[string]any
			Errors []struct{ Message string }
		}
		raw := postGQL(t, srv.URL, string(body))
		require.NoError(t, json.Unmarshal(raw, &resp), "%s", raw)
		assert.Len(t, resp.Errors, 1, q)
		assert.Nil(t, resp.Data, q)
	}
}

// newToolServer is a server listing three tools, one always on, over a fresh
// app.db.
func newToolServer(t *testing.T) (*httptest.Server, *toolsettings.Service) {
	t.Helper()
	settings, err := toolsettings.New(appdbtest.Open(t), []toolsettings.Listing{
		{Name: "Bash", Label: "Bash", Group: toolsettings.GroupCommands, Description: "Run shell commands."},
		{Name: "TaskStop", Label: "TaskStop", Group: toolsettings.GroupCommands, Description: "Stop a task.", AlwaysOn: true},
		{Name: "web_search", Label: "WebSearch", Group: toolsettings.GroupWeb, Description: "Search the web."},
	}, time.Now)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settings.Close()) })
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		ClusterSvc: newFakeClusterService(clusterFixtures()), ToolSettings: settings,
	}))
	t.Cleanup(srv.Close)
	return srv, settings
}

// disabledTools is the names of the tools settings has off.
func disabledTools(t *testing.T, settings *toolsettings.Service) []string {
	t.Helper()
	list, err := settings.List(t.Context())
	require.NoError(t, err)
	var off []string
	for _, s := range list {
		if !s.Enabled {
			off = append(off, s.Name)
		}
	}
	return off
}

// toolSettingsStateWatch lists every tool with its setting, on until turned off;
// toolSettingSet writes the setting and answers the list.
func TestToolSettingsListsEveryToolWithItsSetting(t *testing.T) {
	srv, settings := newToolServer(t)
	listed := firstFrame(t, srv.URL, `subscription { toolSettingsStateWatch { name label group description enabled alwaysOn } }`)["toolSettingsStateWatch"]
	assert.Equal(t, []any{
		map[string]any{"name": "Bash", "label": "Bash", "group": "Commands", "description": "Run shell commands.", "enabled": true, "alwaysOn": false},
		map[string]any{"name": "TaskStop", "label": "TaskStop", "group": "Commands", "description": "Stop a task.", "enabled": true, "alwaysOn": true},
		map[string]any{"name": "web_search", "label": "WebSearch", "group": "Web", "description": "Search the web.", "enabled": true, "alwaysOn": false},
	}, listed)

	data := mutate(t, srv, `mutation { toolSettingSet(name: "web_search", enabled: false) { name enabled } }`)
	assert.Equal(t, []any{
		map[string]any{"name": "Bash", "enabled": true},
		map[string]any{"name": "TaskStop", "enabled": true},
		map[string]any{"name": "web_search", "enabled": false},
	}, data["toolSettingSet"])
	assert.Equal(t, []string{"web_search"}, disabledTools(t, settings))
}

// toolSettingsStateWatch sends the current list on subscribe, then the list after
// each change.
func TestToolSettingsStateWatchSendsTheWholeValue(t *testing.T) {
	srv, _ := newToolServer(t)
	resp := openSSESubscription(t, srv.URL, "", `subscription { toolSettingsStateWatch { name enabled } }`)
	defer resp.Body.Close()
	events := sseEvents(t, resp)
	assert.Contains(t, nextSSE(t, events).data, `[{"name":"Bash","enabled":true},{"name":"TaskStop","enabled":true},{"name":"web_search","enabled":true}]`)

	mutate(t, srv, `mutation { toolSettingSet(name: "Bash", enabled: false) { name } }`)
	assert.Contains(t, nextSSE(t, events).data, `[{"name":"Bash","enabled":false},{"name":"TaskStop","enabled":true},{"name":"web_search","enabled":true}]`)
}

// A name no listed tool has has no setting, so nothing is written.
func TestToolSettingSetRefusesANameNotListed(t *testing.T) {
	srv, settings := newToolServer(t)
	message, code := refusalOf(t, srv, `mutation { toolSettingSet(name: "Nope", enabled: false) { name } }`)
	assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", code)
	assert.Equal(t, "No listed tool has that name.", message)
	assert.Empty(t, disabledTools(t, settings))
}

// An always-on tool cannot be turned off, so nothing is written.
func TestToolSettingSetRefusesToTurnOffAnAlwaysOnTool(t *testing.T) {
	srv, settings := newToolServer(t)
	message, code := refusalOf(t, srv, `mutation { toolSettingSet(name: "TaskStop", enabled: false) { name } }`)
	assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", code)
	assert.Equal(t, "That tool is always on.", message)
	assert.Empty(t, disabledTools(t, settings))
}

// A setting that cannot be written reaches the caller as an error.
func TestToolSettingsReportAStoreItCannotReach(t *testing.T) {
	srv, settings := newToolServer(t)
	require.NoError(t, settings.Close())
	message, _ := refusalOf(t, srv, `mutation { toolSettingSet(name: "Bash", enabled: false) { name } }`)
	assert.NotEmpty(t, message)
}

// fakeChatGPT is the ChatGPT connector as the wire sees it: a state the test
// sets, and a record of what was asked of it.
type fakeChatGPT struct {
	mu            sync.Mutex
	state         connectors.State
	connectErr    error
	disconnectErr error
	connects      []bool
	cancels       int
	disconnect    int
	hub           *watch.Hub[connectors.State]
	tx            *watch.Sender[connectors.State]
}

func newFakeChatGPT(st connectors.State) *fakeChatGPT {
	hub := watch.New(st)
	return &fakeChatGPT{state: st, hub: hub, tx: hub.Sender()}
}

func (f *fakeChatGPT) State() connectors.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *fakeChatGPT) WatchState() (<-chan connectors.State, func()) {
	rx := f.hub.Receiver()
	return rx.Chan(), rx.Close
}

func (f *fakeChatGPT) Connect(_ context.Context, fresh bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connects = append(f.connects, fresh)
	return f.connectErr
}

func (f *fakeChatGPT) Cancel() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancels++
}

func (f *fakeChatGPT) Disconnect(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disconnect++
	return f.disconnectErr
}

func (f *fakeChatGPT) publish(st connectors.State) {
	f.mu.Lock()
	f.state = st
	f.mu.Unlock()
	_ = f.tx.Send(st)
}

func chatGPTServer(t *testing.T, f *fakeChatGPT, opened *[]string) *httptest.Server {
	t.Helper()
	return chatGPTServerOpening(t, f, func(u string) error { *opened = append(*opened, u); return nil })
}

func chatGPTServerOpening(t *testing.T, f *fakeChatGPT, open func(string) error) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		ChatGPT: f,
		OpenURL: open,
		LLMSvc:  llm.New(),
	}))
	t.Cleanup(srv.Close)
	return srv
}

const chatGPTFields = `phase email failure registered revocationUnconfirmed`

// The connection projects the state: the email only while connected and known, the
// failure only while failed, and nothing a token could ride.
func TestChatGPTConnectionProjectsTheState(t *testing.T) {
	f := newFakeChatGPT(connectors.State{Phase: connectors.Connected, Email: "ada@example.com", Failure: connectors.Refused, Registered: true})
	var opened []string
	srv := chatGPTServer(t, f, &opened)

	data := firstFrame(t, srv.URL, `subscription { chatGPTConnectionStateWatch { `+chatGPTFields+` } }`)
	assert.Equal(t, map[string]any{
		"phase": "Connected", "email": "ada@example.com", "failure": nil, "registered": true, "revocationUnconfirmed": false,
	}, data["chatGPTConnectionStateWatch"])

	f.publish(connectors.State{Phase: connectors.Connected})
	data = firstFrame(t, srv.URL, `subscription { chatGPTConnectionStateWatch { email } }`)
	assert.Equal(t, map[string]any{"email": nil}, data["chatGPTConnectionStateWatch"], "no email is null, never empty")

	f.publish(connectors.State{Phase: connectors.Failed, Email: "ada@example.com", Failure: connectors.SessionEnded, RevocationUnconfirmed: true})
	data = firstFrame(t, srv.URL, `subscription { chatGPTConnectionStateWatch { `+chatGPTFields+` } }`)
	assert.Equal(t, map[string]any{
		"phase": "Failed", "email": nil, "failure": "SessionEnded", "registered": false, "revocationUnconfirmed": true,
	}, data["chatGPTConnectionStateWatch"])
}

// A state watch: the current state, then each change.
func TestChatGPTConnectionStateWatchSendsTheWholeValue(t *testing.T) {
	f := newFakeChatGPT(connectors.State{Phase: connectors.SignedOut})
	var opened []string
	srv := chatGPTServer(t, f, &opened)

	resp := openSSESubscription(t, srv.URL, "", `subscription { chatGPTConnectionStateWatch { phase } }`)
	defer resp.Body.Close()
	events := sseEvents(t, resp)
	assert.Contains(t, nextSSE(t, events).data, `"phase":"SignedOut"`)

	f.publish(connectors.State{Phase: connectors.SigningIn})

	assert.Contains(t, nextSSE(t, events).data, `"phase":"SigningIn"`)
}

// The mutations drive the connector.
func TestChatGPTMutations(t *testing.T) {
	f := newFakeChatGPT(connectors.State{Phase: connectors.SignedOut})
	var opened []string
	srv := chatGPTServer(t, f, &opened)

	data, errs := mutation(t, srv.URL, `mutation { chatGPTConnect(fresh: true) }`)
	require.Empty(t, errs)
	assert.Equal(t, true, data["chatGPTConnect"])
	assert.Equal(t, []bool{true}, f.connects)

	f.connectErr = errors.New("boom")
	_, errs = mutation(t, srv.URL, `mutation { chatGPTConnect(fresh: false) }`)
	require.Len(t, errs, 1)
	assert.Nil(t, errs[0].Extensions["code"])

	data, errs = mutation(t, srv.URL, `mutation { chatGPTConnectCancel chatGPTDisconnect chatGPTUsageOpen }`)
	require.Empty(t, errs)
	assert.Equal(t, map[string]any{"chatGPTConnectCancel": true, "chatGPTDisconnect": true, "chatGPTUsageOpen": true}, data)
	assert.Equal(t, 1, f.cancels)
	assert.Equal(t, 1, f.disconnect)
	assert.Equal(t, []string{chatgpt.UsageURL}, opened)
}

// fakeGoogle is the Google connector as the wire sees it, which drives it as it
// drives ChatGPT's, and the project its models run in.
type fakeGoogle struct {
	*fakeChatGPT
	project string
}

func (f fakeGoogle) Project() string { return f.project }

func googleServer(t *testing.T, f fakeGoogle) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		Google: f,
		LLMSvc: llm.New(),
	}))
	t.Cleanup(srv.Close)
	return srv
}

const googleFields = `phase email project failure revocationUnconfirmed`

// The connection projects the state: the email only while connected and known,
// the project whenever one was found, the failure only while failed, and
// nothing a token could ride.
func TestGoogleConnectionProjectsTheState(t *testing.T) {
	f := fakeGoogle{newFakeChatGPT(connectors.State{Phase: connectors.Connected, Email: "ada@example.com", Failure: connectors.Refused}), "my-project"}
	srv := googleServer(t, f)

	for _, c := range []struct {
		st   connectors.State
		want map[string]any
	}{
		{f.State(), map[string]any{"phase": "Connected", "email": "ada@example.com", "project": "my-project", "failure": nil, "revocationUnconfirmed": false}},
		{connectors.State{Phase: connectors.Connected}, map[string]any{"phase": "Connected", "email": nil, "project": "my-project", "failure": nil, "revocationUnconfirmed": false}},
		{
			connectors.State{Phase: connectors.Failed, Email: "ada@example.com", Failure: connectors.ScopeNotGranted, RevocationUnconfirmed: true},
			map[string]any{"phase": "Failed", "email": nil, "project": "my-project", "failure": "ScopeNotGranted", "revocationUnconfirmed": true},
		},
	} {
		f.publish(c.st)
		data := firstFrame(t, srv.URL, `subscription { googleConnectionStateWatch { `+googleFields+` } }`)
		assert.Equal(t, c.want, data["googleConnectionStateWatch"], c.st)
	}

	none := googleServer(t, fakeGoogle{newFakeChatGPT(connectors.State{Phase: connectors.SignedOut}), ""})
	data := firstFrame(t, none.URL, `subscription { googleConnectionStateWatch { project } }`)
	assert.Equal(t, map[string]any{"project": nil}, data["googleConnectionStateWatch"], "no project is null, never empty")
}

// A state watch: the current state, then each change.
func TestGoogleConnectionStateWatchSendsTheWholeValue(t *testing.T) {
	f := fakeGoogle{newFakeChatGPT(connectors.State{Phase: connectors.SignedOut}), "my-project"}
	srv := googleServer(t, f)

	resp := openSSESubscription(t, srv.URL, "", `subscription { googleConnectionStateWatch { phase project } }`)
	defer resp.Body.Close()
	events := sseEvents(t, resp)
	assert.Contains(t, nextSSE(t, events).data, `"phase":"SignedOut"`)

	f.publish(connectors.State{Phase: connectors.SigningIn})

	assert.Contains(t, nextSSE(t, events).data, `"phase":"SigningIn"`)
}

// The mutations drive the connector; a refusal, a build with no client
// included, is a validation error, and any other failure an error with no code.
func TestGoogleMutations(t *testing.T) {
	f := fakeGoogle{newFakeChatGPT(connectors.State{Phase: connectors.SignedOut}), ""}
	srv := googleServer(t, f)

	data, errs := mutation(t, srv.URL, `mutation { googleConnect(fresh: true) googleConnectCancel googleDisconnect }`)
	require.Empty(t, errs)
	assert.Equal(t, map[string]any{"googleConnect": true, "googleConnectCancel": true, "googleDisconnect": true}, data)
	assert.Equal(t, []bool{true}, f.connects)
	assert.Equal(t, 1, f.cancels)
	assert.Equal(t, 1, f.disconnect)

	f.connectErr, f.disconnectErr = errors.New("boom"), errors.New("boom")
	for _, op := range []string{`mutation { googleConnect(fresh: false) }`, `mutation { googleDisconnect }`} {
		_, errs = mutation(t, srv.URL, op)
		require.Len(t, errs, 1, op)
		assert.Nil(t, errs[0].Extensions["code"], op)
	}

	f.connectErr = connectors.ErrNoClient
	_, errs = mutation(t, srv.URL, `mutation { googleConnect(fresh: false) }`)
	require.Len(t, errs, 1)
	assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", errs[0].Extensions["code"])
}

// modelsStateWatch is a state watch of the catalog: the models now, then the list after a
// provider's catalog changes.
func TestModelsStateWatchFollowsTheCatalog(t *testing.T) {
	svc := llm.New(llm.Provider{ID: "chatgpt", Label: "ChatGPT plan", Dialect: llm.DialectResponses})
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{LLMSvc: svc}))
	defer srv.Close()

	resp := openSSESubscription(t, srv.URL, "", `subscription { modelsStateWatch { id provider { id label } } }`)
	defer resp.Body.Close()
	events := sseEvents(t, resp)
	assert.Contains(t, nextSSE(t, events).data, `"modelsStateWatch":[]`)

	svc.SetCatalog("chatgpt", []llm.Model{{ID: "gpt-6.1-sol"}})

	ev := nextSSE(t, events)
	assert.Contains(t, ev.data, `"id":"gpt-6.1-sol"`)
	assert.Contains(t, ev.data, `"label":"ChatGPT plan"`)
}

// A sign-out that could not be recorded, and a browser that cannot open, are
// errors on the wire.
func TestChatGPTMutationsFail(t *testing.T) {
	f := newFakeChatGPT(connectors.State{Phase: connectors.Connected})
	f.disconnectErr = errors.New("the keyring refused")
	srv := chatGPTServerOpening(t, f, func(string) error { return errors.New("no browser") })

	for _, op := range []string{`mutation { chatGPTDisconnect }`, `mutation { chatGPTUsageOpen }`} {
		_, errs := mutation(t, srv.URL, op)
		assert.Len(t, errs, 1, op)
	}
}

// fakeOpenRouter is the OpenRouter connector as the wire sees it: a state the
// test sets, and a record of what was asked of it.
type fakeOpenRouter struct {
	mu            sync.Mutex
	state         connectors.State
	connectErr    error
	disconnectErr error
	connects      int
	cancels       int
	disconnects   int
	hub           *watch.Hub[connectors.State]
	tx            *watch.Sender[connectors.State]
}

func newFakeOpenRouter(st connectors.State) *fakeOpenRouter {
	hub := watch.New(st)
	return &fakeOpenRouter{state: st, hub: hub, tx: hub.Sender()}
}

func (f *fakeOpenRouter) State() connectors.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *fakeOpenRouter) WatchState() (<-chan connectors.State, func()) {
	rx := f.hub.Receiver()
	return rx.Chan(), rx.Close
}

func (f *fakeOpenRouter) Connect(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connects++
	return f.connectErr
}

func (f *fakeOpenRouter) Cancel() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancels++
}

func (f *fakeOpenRouter) Disconnect() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disconnects++
	return f.disconnectErr
}

func (f *fakeOpenRouter) publish(st connectors.State) {
	f.mu.Lock()
	f.state = st
	f.mu.Unlock()
	_ = f.tx.Send(st)
}

func openRouterServer(t *testing.T, f *fakeOpenRouter, open func(string) error) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		OpenRouter: f,
		OpenURL:    open,
		LLMSvc:     llm.New(),
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The connection projects the state: the failure only while failed, and
// nothing a key could ride.
func TestOpenRouterConnectionProjectsTheState(t *testing.T) {
	f := newFakeOpenRouter(connectors.State{Phase: connectors.Connected, Failure: connectors.Refused, Session: 3})
	srv := openRouterServer(t, f, func(string) error { return nil })

	data := firstFrame(t, srv.URL, `subscription { openRouterConnectionStateWatch { phase failure } }`)
	assert.Equal(t, map[string]any{"phase": "Connected", "failure": nil}, data["openRouterConnectionStateWatch"])

	for _, st := range []connectors.State{
		{Phase: connectors.Failed, Failure: connectors.NotDeleted},
		{Phase: connectors.Failed, Failure: connectors.KeyRejected},
	} {
		f.publish(st)
		data = firstFrame(t, srv.URL, `subscription { openRouterConnectionStateWatch { phase failure } }`)
		var failure any
		if st.Failure != "" {
			failure = string(st.Failure)
		}
		assert.Equal(t, map[string]any{"phase": string(st.Phase), "failure": failure}, data["openRouterConnectionStateWatch"])
	}
}

// A state watch: the current state, then each change.
func TestOpenRouterConnectionStateWatchSendsTheWholeValue(t *testing.T) {
	f := newFakeOpenRouter(connectors.State{Phase: connectors.SignedOut})
	srv := openRouterServer(t, f, func(string) error { return nil })

	resp := openSSESubscription(t, srv.URL, "", `subscription { openRouterConnectionStateWatch { phase } }`)
	defer resp.Body.Close()
	events := sseEvents(t, resp)
	assert.Contains(t, nextSSE(t, events).data, `"phase":"SignedOut"`)

	f.publish(connectors.State{Phase: connectors.SigningIn})

	assert.Contains(t, nextSSE(t, events).data, `"phase":"SigningIn"`)
}

// The mutations drive the connector, and the key settings page is the sidecar's
// constant.
func TestOpenRouterMutations(t *testing.T) {
	f := newFakeOpenRouter(connectors.State{Phase: connectors.SignedOut})
	var opened []string
	srv := openRouterServer(t, f, func(u string) error { opened = append(opened, u); return nil })

	data, errs := mutation(t, srv.URL, `mutation { openRouterConnect }`)
	require.Empty(t, errs)
	assert.Equal(t, true, data["openRouterConnect"])
	assert.Equal(t, 1, f.connects)

	data, errs = mutation(t, srv.URL, `mutation { openRouterConnectCancel openRouterDisconnect openRouterKeysOpen }`)
	require.Empty(t, errs)
	assert.Equal(t, map[string]any{"openRouterConnectCancel": true, "openRouterDisconnect": true, "openRouterKeysOpen": true}, data)
	assert.Equal(t, 1, f.cancels)
	assert.Equal(t, 1, f.disconnects)
	assert.Equal(t, []string{openrouter.KeysURL}, opened)
}

// A delete the keyring refused, a browser that cannot open, and a Connect that
// fails for another reason are errors on the wire, with no validation code.
func TestOpenRouterMutationsFail(t *testing.T) {
	f := newFakeOpenRouter(connectors.State{Phase: connectors.Connected})
	f.disconnectErr = errors.New("the keyring refused")
	f.connectErr = errors.New("boom")
	srv := openRouterServer(t, f, func(string) error { return errors.New("no browser") })

	for _, op := range []string{`mutation { openRouterDisconnect }`, `mutation { openRouterKeysOpen }`, `mutation { openRouterConnect }`} {
		_, errs := mutation(t, srv.URL, op)
		require.Len(t, errs, 1, op)
		assert.Nil(t, errs[0].Extensions["code"], op)
	}
}

// The AWS connector's wire is OpenRouter's but for its ARN, so one fake drives both.
func awsServer(t *testing.T, f *fakeOpenRouter) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		AWS:     f,
		OpenURL: func(string) error { return nil },
		LLMSvc:  llm.New(),
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The connection projects the state: the ARN only while connected, the failure
// only while failed, and no credential.
func TestAWSConnectionProjectsTheState(t *testing.T) {
	const arn = "arn:aws:iam::123456789012:user/alice"
	f := newFakeOpenRouter(connectors.State{Phase: connectors.Connected, Subject: arn, Failure: connectors.Refused})
	srv := awsServer(t, f)

	data := firstFrame(t, srv.URL, `subscription { awsConnectionStateWatch { phase arn failure } }`)
	assert.Equal(t, map[string]any{"phase": "Connected", "arn": arn, "failure": nil}, data["awsConnectionStateWatch"])

	f.publish(connectors.State{Phase: connectors.Failed, Failure: connectors.SessionEnded, Subject: arn})
	data = firstFrame(t, srv.URL, `subscription { awsConnectionStateWatch { phase arn failure } }`)
	assert.Equal(t, map[string]any{"phase": "Failed", "arn": nil, "failure": "SessionEnded"}, data["awsConnectionStateWatch"])
}

// A state watch: the current state, then each change.
func TestAWSConnectionStateWatchSendsTheWholeValue(t *testing.T) {
	f := newFakeOpenRouter(connectors.State{Phase: connectors.SignedOut})
	srv := awsServer(t, f)

	resp := openSSESubscription(t, srv.URL, "", `subscription { awsConnectionStateWatch { phase } }`)
	defer resp.Body.Close()
	events := sseEvents(t, resp)
	assert.Contains(t, nextSSE(t, events).data, `"phase":"SignedOut"`)

	f.publish(connectors.State{Phase: connectors.SigningIn})

	assert.Contains(t, nextSSE(t, events).data, `"phase":"SigningIn"`)
}

// The mutations drive the connector; a refusal is a validation error, and any
// other failure an error with no code.
func TestAWSMutations(t *testing.T) {
	f := newFakeOpenRouter(connectors.State{Phase: connectors.SignedOut})
	srv := awsServer(t, f)

	data, errs := mutation(t, srv.URL, `mutation { awsConnect awsConnectCancel awsDisconnect }`)
	require.Empty(t, errs)
	assert.Equal(t, map[string]any{"awsConnect": true, "awsConnectCancel": true, "awsDisconnect": true}, data)
	assert.Equal(t, 1, f.connects)
	assert.Equal(t, 1, f.cancels)
	assert.Equal(t, 1, f.disconnects)

	f.connectErr, f.disconnectErr = connectors.ErrNotDeleted, connectors.ErrNotDeleted
	for _, op := range []string{`mutation { awsConnect }`, `mutation { awsDisconnect }`} {
		_, errs := mutation(t, srv.URL, op)
		require.Len(t, errs, 1, op)
		assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", errs[0].Extensions["code"], op)
	}

	f.connectErr, f.disconnectErr = errors.New("boom"), errors.New("the keyring refused")
	for _, op := range []string{`mutation { awsConnect }`, `mutation { awsDisconnect }`} {
		_, errs := mutation(t, srv.URL, op)
		require.Len(t, errs, 1, op)
		assert.Nil(t, errs[0].Extensions["code"], op)
	}
}

// fakeGitHub is the GitHub connector as the wire sees it, which drives it as it
// drives OpenRouter's.
type fakeGitHub = fakeOpenRouter

func gitHubServer(t *testing.T, f *fakeGitHub, open func(string) error) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		GitHub:  f,
		OpenURL: open,
		LLMSvc:  llm.New(),
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The connection projects the state: the login only while connected, the
// code only while signing in, the failure only while failed, and nothing a
// token could ride.
func TestGitHubConnectionProjectsTheState(t *testing.T) {
	f := newFakeOpenRouter(connectors.State{Phase: connectors.Connected, Name: "octocat", UserCode: "WDJB-MJHT", Failure: connectors.Refused})
	srv := gitHubServer(t, f, func(string) error { return nil })
	const query = `subscription { gitHubConnectionStateWatch { phase login userCode failure } }`

	for _, c := range []struct {
		st   connectors.State
		want map[string]any
	}{
		{f.State(), map[string]any{"phase": "Connected", "login": "octocat", "userCode": nil, "failure": nil}},
		{
			connectors.State{Phase: connectors.SigningIn, Name: "octocat", UserCode: "WDJB-MJHT"},
			map[string]any{"phase": "SigningIn", "login": nil, "userCode": "WDJB-MJHT", "failure": nil},
		},
		{connectors.State{Phase: connectors.SigningIn}, map[string]any{"phase": "SigningIn", "login": nil, "userCode": nil, "failure": nil}},
		{
			connectors.State{Phase: connectors.Failed, Failure: connectors.KeyRejected, Name: "octocat"},
			map[string]any{"phase": "Failed", "login": nil, "userCode": nil, "failure": "KeyRejected"},
		},
	} {
		f.publish(c.st)
		data := firstFrame(t, srv.URL, query)
		assert.Equal(t, c.want, data["gitHubConnectionStateWatch"], c.st)
	}
}

// A state watch: the current state, then each change.
func TestGitHubConnectionStateWatchSendsTheWholeValue(t *testing.T) {
	f := newFakeOpenRouter(connectors.State{Phase: connectors.SignedOut})
	srv := gitHubServer(t, f, func(string) error { return nil })

	resp := openSSESubscription(t, srv.URL, "", `subscription { gitHubConnectionStateWatch { phase userCode } }`)
	defer resp.Body.Close()
	events := sseEvents(t, resp)
	assert.Contains(t, nextSSE(t, events).data, `"phase":"SignedOut"`)

	f.publish(connectors.State{Phase: connectors.SigningIn, UserCode: "WDJB-MJHT"})

	assert.Contains(t, nextSSE(t, events).data, `"userCode":"WDJB-MJHT"`)
}

// The mutations drive the connector, and GitHub's settings page is the
// sidecar's constant.
func TestGitHubMutations(t *testing.T) {
	f := newFakeOpenRouter(connectors.State{Phase: connectors.SignedOut})
	var opened []string
	srv := gitHubServer(t, f, func(u string) error { opened = append(opened, u); return nil })

	data, errs := mutation(t, srv.URL, `mutation { gitHubConnect gitHubConnectCancel gitHubDisconnect gitHubSettingsOpen }`)
	require.Empty(t, errs)
	assert.Equal(t, map[string]any{"gitHubConnect": true, "gitHubConnectCancel": true, "gitHubDisconnect": true, "gitHubSettingsOpen": true}, data)
	assert.Equal(t, 1, f.connects)
	assert.Equal(t, 1, f.cancels)
	assert.Equal(t, 1, f.disconnects)
	assert.Equal(t, []string{github.SettingsURL}, opened)
}

// A delete the keyring refused, a browser that cannot open, and a Connect that
// fails for another reason are errors on the wire, with no validation code;
// a refusal, a build with no OAuth App included, is a validation error.
func TestGitHubMutationsFail(t *testing.T) {
	f := newFakeOpenRouter(connectors.State{Phase: connectors.Connected})
	f.disconnectErr = errors.New("the keyring refused")
	f.connectErr = errors.New("boom")
	srv := gitHubServer(t, f, func(string) error { return errors.New("no browser") })

	for _, op := range []string{`mutation { gitHubDisconnect }`, `mutation { gitHubSettingsOpen }`, `mutation { gitHubConnect }`} {
		_, errs := mutation(t, srv.URL, op)
		require.Len(t, errs, 1, op)
		assert.Nil(t, errs[0].Extensions["code"], op)
	}

	for _, refusal := range []error{connectors.ErrNoClient, connectors.ErrSigningIn, connectors.ErrConnected, connectors.ErrNotDeleted} {
		f.connectErr, f.disconnectErr = refusal, refusal
		for _, op := range []string{`mutation { gitHubConnect }`, `mutation { gitHubDisconnect }`} {
			_, errs := mutation(t, srv.URL, op)
			require.Len(t, errs, 1, op)
			assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", errs[0].Extensions["code"], "%s: %v", op, refusal)
		}
	}
}

// Every refusal of either connector's Connect or Disconnect reaches the wire as
// a validation error.
func TestAConnectorRefusalIsAValidationError(t *testing.T) {
	chatGPT := newFakeChatGPT(connectors.State{Phase: connectors.SignedOut})
	chatGPTSrv := chatGPTServer(t, chatGPT, new([]string))
	openRouter := newFakeOpenRouter(connectors.State{Phase: connectors.SignedOut})
	openRouterSrv := openRouterServer(t, openRouter, func(string) error { return nil })

	for _, refusal := range []error{connectors.ErrRestoring, connectors.ErrSigningIn, connectors.ErrConnected, connectors.ErrNotDeleted} {
		chatGPT.connectErr, chatGPT.disconnectErr = refusal, refusal
		openRouter.connectErr, openRouter.disconnectErr = refusal, refusal
		for _, c := range []struct{ url, op string }{
			{chatGPTSrv.URL, `mutation { chatGPTConnect(fresh: false) }`},
			{chatGPTSrv.URL, `mutation { chatGPTDisconnect }`},
			{openRouterSrv.URL, `mutation { openRouterConnect }`},
			{openRouterSrv.URL, `mutation { openRouterDisconnect }`},
		} {
			_, errs := mutation(t, c.url, c.op)
			require.Len(t, errs, 1, c.op)
			assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", errs[0].Extensions["code"], "%s: %v", c.op, refusal)
		}
	}
}

// Every Phase and Failure connectors.go declares reaches the wire as the value
// of its name. The constants are read from the source, so one added without a
// gqlgen.yml value fails here.
func TestEveryConnectorValueIsOnTheWire(t *testing.T) {
	phases, failures := connectorConstants(t)
	require.NotEmpty(t, phases)
	require.NotEmpty(t, failures)
	f := newFakeOpenRouter(connectors.State{})
	srv := openRouterServer(t, f, func(string) error { return nil })

	for _, p := range phases {
		f.publish(connectors.State{Phase: connectors.Phase(p)})
		data := firstFrame(t, srv.URL, `subscription { openRouterConnectionStateWatch { phase } }`)
		assert.Equal(t, map[string]any{"phase": p}, data["openRouterConnectionStateWatch"], p)
	}
	for _, fl := range failures {
		f.publish(connectors.State{Phase: connectors.Failed, Failure: connectors.Failure(fl)})
		data := firstFrame(t, srv.URL, `subscription { openRouterConnectionStateWatch { failure } }`)
		assert.Equal(t, map[string]any{"failure": fl}, data["openRouterConnectionStateWatch"], fl)
	}
}

// connectorConstants is the value of every constant connectors.go declares of
// type Phase and of type Failure.
func connectorConstants(t *testing.T) (phases, failures []string) {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "internal", "services", "connectors", "connectors.go"), nil, 0)
	require.NoError(t, err)
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			typ, ok := vs.Type.(*ast.Ident)
			require.True(t, ok, "%s has no named type", vs.Names[0])
			for _, v := range vs.Values {
				value, err := strconv.Unquote(v.(*ast.BasicLit).Value)
				require.NoError(t, err)
				switch typ.Name {
				case "Phase":
					phases = append(phases, value)
				case "Failure":
					failures = append(failures, value)
				}
			}
		}
	}
	return phases, failures
}

// providerCredentialsServer serves a provider credentials service over k, with one keyed
// provider holding an environment key.
func providerCredentialsServer(t *testing.T, k *oskeyringtest.Keyring) (*httptest.Server, *providercredentials.Service) {
	t.Helper()
	srv, keys := startProviderCredentialsServer(t, k)
	testutil.Wait(t, keys.Restored(), "the restore")
	return srv, keys
}

// startProviderCredentialsServer is providerCredentialsServer without the wait for the
// restore.
func startProviderCredentialsServer(t *testing.T, k *oskeyringtest.Keyring) (*httptest.Server, *providercredentials.Service) {
	t.Helper()
	keys, err := providercredentials.New(providercredentials.Config{
		Keyring:   k,
		Providers: catalog.Providers(nil, ""),
		EnvKeys:   map[string]string{"anthropic": "sk-ant-env-0123456789abcdef"},
	})
	require.NoError(t, err)
	t.Cleanup(keys.Close)
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{ProviderCredentials: keys, LLMSvc: llm.New()}))
	t.Cleanup(srv.Close)
	return srv, keys
}

// The state watch lists every provider with where its credential comes from, and no
// key. One that takes no key has no key variable.
func TestProviderCredentialsProjectTheState(t *testing.T) {
	srv, _ := providerCredentialsServer(t, oskeyringtest.New())

	data := firstFrame(t, srv.URL, `subscription { providerCredentialsStateWatch { id label keyVariable source hasEnvironmentKey hasSavedKey failure } }`)
	rows := data["providerCredentialsStateWatch"].([]any)
	require.Len(t, rows, len(catalog.Providers(nil, "")))
	assert.Equal(t, map[string]any{
		"id": "anthropic", "label": "Anthropic", "keyVariable": "ANTHROPIC_API_KEY",
		"source": "EnvironmentKey", "hasEnvironmentKey": true, "hasSavedKey": false, "failure": nil,
	}, rows[0])
	assert.Equal(t, map[string]any{
		"id": "chatgpt", "label": "ChatGPT plan", "keyVariable": nil,
		"source": "None", "hasEnvironmentKey": false, "hasSavedKey": false, "failure": nil,
	}, rows[len(rows)-1])
}

// A save and a delete drive the service; the state watch sends
// each change.
func TestProviderKeyMutationsAndWatch(t *testing.T) {
	const key = "gsk-wire-0123456789abcdef"
	k := oskeyringtest.New()
	srv, keys := providerCredentialsServer(t, k)
	resp := openSSESubscription(t, srv.URL, "", `subscription { providerCredentialsStateWatch { id source hasSavedKey failure } }`)
	defer resp.Body.Close()
	events := sseEvents(t, resp)
	assert.Contains(t, nextSSE(t, events).data, `{"id":"groq","source":"None","hasSavedKey":false,"failure":null}`)

	data, errs := mutation(t, srv.URL, `mutation { providerKeySave(providerID: "groq", key: "`+key+`") }`)
	require.Empty(t, errs)
	assert.Equal(t, true, data["providerKeySave"])
	assert.Contains(t, nextSSE(t, events).data, `{"id":"groq","source":"SavedKey","hasSavedKey":true,"failure":null}`)

	k.FailDelete = errors.New("locked")
	_, errs = mutation(t, srv.URL, `mutation { providerKeyDelete(providerID: "groq") }`)
	require.Len(t, errs, 1)
	assert.Equal(t, "KUBETAIL_PROVIDER_KEY_NOT_DELETED", errs[0].Extensions["code"])
	// The delete publishes the cleared provider, then the failure; the state watch may send either first.
	event := nextSSE(t, events)
	for !strings.Contains(event.data, `"NotDeleted"`) {
		event = nextSSE(t, events)
	}
	assert.Contains(t, event.data, `{"id":"groq","source":"None","hasSavedKey":false,"failure":"NotDeleted"}`)

	k.FailDelete = nil
	data, errs = mutation(t, srv.URL, `mutation { providerKeyDelete(providerID: "groq") }`)
	require.Empty(t, errs)
	assert.Equal(t, true, data["providerKeyDelete"])
	st, _ := keys.State("groq")
	assert.Empty(t, st.Failure)
}

// Each refusal reaches the wire as its code.
func TestProviderKeyRefusals(t *testing.T) {
	k := oskeyringtest.New()
	srv, _ := providerCredentialsServer(t, k)
	k.FailSet = errors.New("locked")
	for op, code := range map[string]string{
		`mutation { providerKeySave(providerID: "nope", key: "key-0123456789abcdef") }`: "KUBETAIL_VALIDATION_ERROR",
		`mutation { providerKeySave(providerID: "groq", key: "short") }`:                "KUBETAIL_VALIDATION_ERROR",
		`mutation { providerKeySave(providerID: "groq", key: "key-0123456789abcdef") }`: "KUBETAIL_PROVIDER_KEY_NOT_SAVED",
		`mutation { providerKeyDelete(providerID: "nope") }`:                            "KUBETAIL_VALIDATION_ERROR",
		// A provider that takes no key.
		`mutation { providerKeySave(providerID: "chatgpt", key: "key-0123456789abcdef") }`: "KUBETAIL_VALIDATION_ERROR",
		`mutation { providerKeyDelete(providerID: "chatgpt") }`:                            "KUBETAIL_VALIDATION_ERROR",
	} {
		_, errs := mutation(t, srv.URL, op)
		require.Len(t, errs, 1, op)
		assert.Equal(t, code, errs[0].Extensions["code"], op)
	}
}

// While the keyring is unread, every provider is Restoring, one with an
// environment key says so, and a Save or Delete is a validation error.
func TestProviderKeysWhileRestoring(t *testing.T) {
	k := oskeyringtest.New()
	t.Cleanup(k.Hold(oskeyringtest.Get))
	srv, _ := startProviderCredentialsServer(t, k)

	data := firstFrame(t, srv.URL, `subscription { providerCredentialsStateWatch { id source hasEnvironmentKey hasSavedKey } }`)
	assert.Contains(t, data["providerCredentialsStateWatch"], map[string]any{"id": "anthropic", "source": "Restoring", "hasEnvironmentKey": true, "hasSavedKey": false})
	for _, op := range []string{
		`mutation { providerKeySave(providerID: "groq", key: "key-0123456789abcdef") }`,
		`mutation { providerKeyDelete(providerID: "groq") }`,
	} {
		_, errs := mutation(t, srv.URL, op)
		require.Len(t, errs, 1, op)
		assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", errs[0].Extensions["code"], op)
	}
}

// No saved key comes back in a response or reaches the log, on a save that
// succeeds or fails. Serial: it swaps the default logger.
func TestProviderKeySaveReturnsAndLogsNoKey(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var logs bytes.Buffer
	slog.SetDefault(logging.Init(&logs, slog.LevelDebug))

	const saved, refused = "sk-wire-saved-0123456789abcdef", "sk-wire-refused-0123456789abcdef"
	k := oskeyringtest.New()
	srv, _ := providerCredentialsServer(t, k)
	var bodies []string
	for _, key := range []string{saved, refused} {
		bodies = append(bodies, string(postGQL(t, srv.URL, `{"query":"mutation { providerKeySave(providerID: \"groq\", key: \"`+key+`\") }"}`)))
		k.FailSet = errors.New("locked")
	}
	data := firstFrame(t, srv.URL, `subscription { providerCredentialsStateWatch { id source hasSavedKey } }`)
	watched, _ := json.Marshal(data)
	bodies = append(bodies, string(watched))
	for _, key := range []string{saved, refused} {
		for _, body := range bodies {
			assert.NotContains(t, body, key)
		}
		assert.NotContains(t, logs.String(), key)
	}
	assert.Contains(t, bodies[1], "KUBETAIL_PROVIDER_KEY_NOT_SAVED")
}

// envVarServer is a server whose resolver holds the variables service over an
// app.db of the test's own, holding the cluster prod.
func envVarServer(t *testing.T) (*httptest.Server, *appdb.DB) {
	t.Helper()
	db := appdbtest.Open(t)
	_, err := db.Write.Exec(`INSERT INTO clusters (id, source, source_key, created_at, updated_at) VALUES ('prod', 'kubeconfig', 'prod', 0, 0)`)
	require.NoError(t, err)
	svc, err := envvars.New(db, time.Now)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, svc.Close()) })
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{EnvVars: svc}))
	t.Cleanup(srv.Close)
	return srv, db
}

// The user's variables are listed, set and removed by scope, each mutation
// answering its scope's list in order; each refusal names its reason in its
// code, and writes nothing.
func TestEnvVarMutationsAnswerTheirScopesList(t *testing.T) {
	srv, _ := envVarServer(t)

	data, _ := mutation(t, srv.URL, `{ envVarsGet { name } }`)
	assert.Equal(t, []any{}, data["envVarsGet"])

	mutation(t, srv.URL, `mutation { envVarSet(name: "HELM_NAMESPACE", value: "apps", target: Both) { name } }`)
	data, _ = mutation(t, srv.URL, `mutation { envVarSet(name: "AWS_PROFILE", value: "dev", target: Outside) { clusterID name value target refusal } }`)
	assert.Equal(t, []any{
		map[string]any{"clusterID": nil, "name": "HELM_NAMESPACE", "value": "apps", "target": "Both", "refusal": nil},
		map[string]any{"clusterID": nil, "name": "AWS_PROFILE", "value": "dev", "target": "Outside", "refusal": nil},
	}, data["envVarSet"])

	data, _ = mutation(t, srv.URL, `mutation { envVarSet(clusterID: "prod", name: "HELM_NAMESPACE", value: "prod-apps", target: Sandbox) { clusterID name value target } }`)
	prod := []any{map[string]any{"clusterID": "prod", "name": "HELM_NAMESPACE", "value": "prod-apps", "target": "Sandbox"}}
	assert.Equal(t, prod, data["envVarSet"])
	data, _ = mutation(t, srv.URL, `{ envVarsGet(clusterID: "prod") { clusterID name value target } }`)
	assert.Equal(t, prod, data["envVarsGet"])

	data, _ = mutation(t, srv.URL, `mutation { envVarRemove(name: "HELM_NAMESPACE") { name } }`)
	assert.Equal(t, []any{map[string]any{"name": "AWS_PROFILE"}}, data["envVarRemove"])
	data, _ = mutation(t, srv.URL, `mutation { envVarRemove(clusterID: "prod", name: "HELM_NAMESPACE") { name } }`)
	assert.Equal(t, []any{}, data["envVarRemove"])

	for query, want := range map[string][2]string{
		`mutation { envVarSet(name: "AWS_PROFILE", value: "x", target: Both) { name } }`: {"KUBETAIL_VALIDATION_ERROR",
			"AWS_PROFILE is never passed to a sandboxed command. It can reach commands outside the sandbox alone."},
		`mutation { envVarSet(name: "LD_PRELOAD", value: "/x", target: Outside) { name } }`:       {"KUBETAIL_VALIDATION_ERROR", "LD_PRELOAD changes how a program starts."},
		`mutation { envVarSet(name: "X", value: "a\u0000b", target: Both) { name } }`:             {"KUBETAIL_VALIDATION_ERROR", "A value holds no NUL character."},
		`mutation { envVarRemove(name: "PAGER") { name } }`:                                       {"KUBETAIL_VALIDATION_ERROR", "That variable is not set."},
		`mutation { envVarSet(clusterID: "gone", name: "X", value: "x", target: Both) { name } }`: {"KUBETAIL_RECORD_NOT_FOUND", "Record not found"},
	} {
		_, errs := mutation(t, srv.URL, query)
		code, got := refusal(t, errs)
		assert.Equal(t, want[0], code, query)
		assert.Equal(t, want[1], got, query)
	}
	data, _ = mutation(t, srv.URL, `{ envVarsGet { name target } }`)
	assert.Equal(t, []any{map[string]any{"name": "AWS_PROFILE", "target": "Outside"}}, data["envVarsGet"], "a refusal writes nothing")
}

// A stored row the check refuses is listed with its reason, and removed like
// any other.
func TestARefusedVariableIsListedWithItsReason(t *testing.T) {
	srv, db := envVarServer(t)
	_, err := db.Write.Exec(`INSERT INTO env_vars (id, name, value, target, created_at, updated_at) VALUES (?, 'PATH', '/evil', 'both', 0, 0)`, appdb.NewID())
	require.NoError(t, err)

	data, _ := mutation(t, srv.URL, `{ envVarsGet { name value refusal } }`)
	assert.Equal(t, []any{map[string]any{"name": "PATH", "value": "/evil", "refusal": "Kubetail sets PATH itself."}}, data["envVarsGet"])

	data, errs := mutation(t, srv.URL, `mutation { envVarRemove(name: "PATH") { name } }`)
	assert.Empty(t, errs)
	assert.Equal(t, []any{}, data["envVarRemove"])
}

// A list the service cannot read is an error, never an empty list.
func TestEnvVarsOverAFailedStoreIsAnError(t *testing.T) {
	srv, db := envVarServer(t)
	require.NoError(t, db.Close())

	data, errs := mutation(t, srv.URL, `{ envVarsGet { name } }`)
	assert.NotEmpty(t, errs)
	assert.Nil(t, data)
}

// fakeConsole records each command it is asked to start and answers err;
// its hub is the console's state watch.
type fakeConsole struct {
	err     error
	started []bash.ConsoleRequest
	stopped []int
	hub     *watch.Hub[*bash.ConsoleRun]
}

func newFakeConsole() *fakeConsole {
	return &fakeConsole{hub: watch.New[*bash.ConsoleRun](nil)}
}

// StartConsoleCommand numbers each command it starts from 1.
func (f *fakeConsole) StartConsoleCommand(req bash.ConsoleRequest) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.started = append(f.started, req)
	return len(f.started), nil
}

func (f *fakeConsole) StopConsoleCommand(number int) { f.stopped = append(f.stopped, number) }

func (f *fakeConsole) WatchConsoleState() *watch.Receiver[*bash.ConsoleRun] { return f.hub.Receiver() }

// sandboxConsoleServer is a server over console, nil for a machine with no
// shell.
func sandboxConsoleServer(t *testing.T, console graph.SandboxConsole) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{Console: console}))
	t.Cleanup(srv.Close)
	return srv
}

const sandboxConsoleFields = `{ run { number command clusterID network running output discarded exitCode notice durationMs } }`

// consoleFrame is the next sandboxConsoleStateWatch frame on events.
func consoleFrame(t *testing.T, events <-chan sseEvent) map[string]any {
	t.Helper()
	ev := testutil.Recv(t, events, "a sandboxConsoleStateWatch frame")
	var payload struct {
		Data struct {
			Console map[string]any `json:"sandboxConsoleStateWatch"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(ev.data), &payload), ev.data)
	return payload.Data.Console
}

// A run starts the command as typed, with its cluster and network, and
// answers its number; a stop stops one command, or whichever runs.
func TestSandboxConsoleRunStartsTheCommand(t *testing.T) {
	console := newFakeConsole()
	srv := sandboxConsoleServer(t, console)

	data, errs := mutation(t, srv.URL, `mutation { sandboxConsoleRun(command: "kubectl get ns", clusterID: "7", network: true) }`)
	require.Empty(t, errs)
	assert.Equal(t, float64(1), data["sandboxConsoleRun"])
	data, errs = mutation(t, srv.URL, `mutation { sandboxConsoleRun(command: "ls ~", network: false) }`)
	require.Empty(t, errs)
	assert.Equal(t, float64(2), data["sandboxConsoleRun"])
	data, errs = mutation(t, srv.URL, `mutation { sandboxConsoleStop(number: 2) }`)
	require.Empty(t, errs)
	assert.Equal(t, true, data["sandboxConsoleStop"])
	_, errs = mutation(t, srv.URL, `mutation { sandboxConsoleStop }`)
	require.Empty(t, errs)

	assert.Equal(t, []bash.ConsoleRequest{
		{Command: "kubectl get ns", ClusterID: "7", Network: true},
		{Command: "ls ~"},
	}, console.started)
	assert.Equal(t, []int{2, 0}, console.stopped)
}

// A refused run names why: a command running is a conflict, and a refusal
// in the user's words, or a machine whose sandbox confines nothing, is a
// validation error carrying them. Anything else stays opaque.
func TestSandboxConsoleRunRefusals(t *testing.T) {
	for err, want := range map[error][2]string{
		bash.ErrConsoleBusy:                       {"KUBETAIL_CONFLICT", bash.ErrConsoleBusy.Error()},
		bash.ErrNoConsole:                         {"KUBETAIL_VALIDATION_ERROR", bash.ErrNoConsole.Error()},
		bash.ErrConsoleClosed:                     {"KUBETAIL_VALIDATION_ERROR", bash.ErrConsoleClosed.Error()},
		&bash.ConsoleRefusal{Reason: "Type one."}: {"KUBETAIL_VALIDATION_ERROR", "Type one."},
	} {
		console := newFakeConsole()
		console.err = err
		message, code := refusalOf(t, sandboxConsoleServer(t, console), `mutation { sandboxConsoleRun(command: "x", network: false) }`)
		assert.Equal(t, want[0], code, err)
		assert.Equal(t, want[1], message, err)
	}

	console := newFakeConsole()
	console.err = errors.New("disk on fire")
	_, code := refusalOf(t, sandboxConsoleServer(t, console), `mutation { sandboxConsoleRun(command: "x", network: false) }`)
	assert.NotEqual(t, "KUBETAIL_VALIDATION_ERROR", code)
}

// A machine with no shell refuses a run, stops nothing, and its watch sends
// one empty frame.
func TestSandboxConsoleWithNoShell(t *testing.T) {
	srv := sandboxConsoleServer(t, nil)

	_, code := refusalOf(t, srv, `mutation { sandboxConsoleRun(command: "x", network: false) }`)
	assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", code)
	data, errs := mutation(t, srv.URL, `mutation { sandboxConsoleStop }`)
	require.Empty(t, errs)
	assert.Equal(t, true, data["sandboxConsoleStop"])

	resp := openSSESubscription(t, srv.URL, "", `subscription { sandboxConsoleStateWatch `+sandboxConsoleFields+` }`)
	defer resp.Body.Close()
	assert.Equal(t, map[string]any{"run": nil}, consoleFrame(t, sseEvents(t, resp)))
}

// The watch sends the console's last command as it stands, first null, and
// ends with the console's feed.
func TestSandboxConsoleStateWatchSendsTheLastCommand(t *testing.T) {
	console := newFakeConsole()
	srv := sandboxConsoleServer(t, console)
	resp := openSSESubscription(t, srv.URL, "", `subscription { sandboxConsoleStateWatch `+sandboxConsoleFields+` }`)
	defer resp.Body.Close()
	events := sseEvents(t, resp)
	assert.Equal(t, map[string]any{"run": nil}, consoleFrame(t, events))

	tx := console.hub.Sender()
	tx.Send(&bash.ConsoleRun{ConsoleRequest: bash.ConsoleRequest{Command: "ls ~"}, Number: 1, Running: true, Output: "a"}) //nolint:errcheck // Send never blocks
	assert.Equal(t, map[string]any{"run": map[string]any{
		"number": float64(1), "command": "ls ~", "clusterID": nil, "network": false, "running": true,
		"output": "a", "discarded": float64(0), "exitCode": nil, "notice": nil, "durationMs": float64(0),
	}}, consoleFrame(t, events))

	code := 130
	tx.Send(&bash.ConsoleRun{ //nolint:errcheck // Send never blocks
		ConsoleRequest: bash.ConsoleRequest{Command: "kubectl get ns", ClusterID: "7", Network: true},
		Number:         2, Output: "b", Discarded: 9, ExitCode: &code, Notice: "Command cancelled (exit code 130)",
		Duration: 1500 * time.Millisecond,
	})
	assert.Equal(t, map[string]any{"run": map[string]any{
		"number": float64(2), "command": "kubectl get ns", "clusterID": "7", "network": true, "running": false,
		"output": "b", "discarded": float64(9), "exitCode": float64(130), "notice": "Command cancelled (exit code 130)",
		"durationMs": float64(1500),
	}}, consoleFrame(t, events))

	console.hub.Close()
	testutil.WaitClosed(t, events, "the watch to end with the feed")
}

// --- monitoring ---

// otherIntegration is an integration with one signal, which lists nothing: its
// targets are rows a test writes.
type otherIntegration struct{}

func (otherIntegration) ListTargets(context.Context) ([]monitor.Target, error)  { return nil, nil }
func (otherIntegration) ListenForTargetChanges(context.Context) <-chan struct{} { return nil }
func (otherIntegration) Read(context.Context, monitor.Target) (monitor.Reading, error) {
	return monitor.Reading{}, errors.New("unread")
}

// otherRegistration registers otherIntegration, which has no MonitorIntegration
// value: a query of its targets or signals leaves out their integration.
var otherRegistration = monitor.Registration{Name: "other", Cadence: time.Minute, Integration: otherIntegration{},
	Signals: []monitor.Signal{{Name: "other.down", Label: "Down", Severity: monitor.SeverityCritical}}}

// newMonitorServer is a GraphQL server over a real monitor, never started, with
// the Kubernetes registration and regs. Its rows are a Kubernetes target for
// cluster 1 holding one open incident of two occurrences, and a target of the
// other integration, served only when regs register it.
func newMonitorServer(t *testing.T, regs ...monitor.Registration) (*httptest.Server, *appdb.DB, *monitor.Monitor) {
	t.Helper()
	db := appdbtest.Open(t)
	mon, err := monitor.New(db, append([]monitor.Registration{kubesignals.Register(db, newFakeClusterService(nil))}, regs...)...)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mon.Close()) })
	for _, q := range []string{
		`INSERT INTO monitor_targets (id, integration, key, label, current_uid, unknown_signals, last_read_at, created_at, updated_at)
		VALUES ('t1', 'kubernetes', '1', 'prod', 'srv-1', '{"kubernetes.node_not_ready":"permission_denied","kubernetes.gone":"invalid"}', 1, 0, 0)`,
		`INSERT INTO monitor_targets (id, integration, key, label, created_at, updated_at) VALUES ('t2', 'other', 'acct', 'Account', 0, 0)`,
		`INSERT INTO incidents (id, target_id, target_uid, subject_key, subject_label, subject_attributes, signal, signal_label, severity,
		  first_seen_at, last_seen_at, occurrence_count)
		VALUES ('i1', 't1', 'srv-1', 'apps/Deployment/default/web', 'Deployment default/web',
		  '{"name":"web","namespace":"default","kind":"Deployment","apiGroup":"apps"}', 'kubernetes.crash_loop', 'Crash loop', 'warning',
		  1000, 5000, 2)`,
		`INSERT INTO incident_occurrences (id, incident_id, number, started_at, ended_at, end_reason, detail)
		VALUES ('o1', 'i1', 1, 1000, 2000, 'resolved', '{"pod":"web-0","container":"app"}')`,
		`INSERT INTO incident_occurrences (id, incident_id, number, started_at, detail) VALUES ('o2', 'i1', 2, 3000, '{}')`,
	} {
		_, err := db.Write.Exec(q)
		require.NoError(t, err, q)
	}
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{Monitor: mon}))
	t.Cleanup(srv.Close)
	return srv, db, mon
}

// framesUntilBookmark is a watch's snapshot frames, the Bookmark left out.
func framesUntilBookmark(t *testing.T, events <-chan sseEvent) []map[string]any {
	t.Helper()
	var got []map[string]any
	for {
		f := nextChatFrame(t, events)
		if f["type"] == "Bookmark" {
			return got
		}
		got = append(got, f)
	}
}

// targetsByID is a target watch's snapshot, each target by its id.
func targetsByID(t *testing.T, events <-chan sseEvent) map[string]map[string]any {
	t.Helper()
	got := map[string]map[string]any{}
	for _, f := range framesUntilBookmark(t, events) {
		target := f["target"].(map[string]any)
		got[target["id"].(string)] = target
	}
	return got
}

// monitorSignalList answers Kubernetes's six signals, each with its label and
// severity, then each other registration's.
func TestMonitorSignalListAnswersEveryRegisteredSignal(t *testing.T) {
	srv, _, _ := newMonitorServer(t)

	data := mutate(t, srv, `query { monitorSignalList { name integration label severity } }`)

	list := data["monitorSignalList"].([]any)
	require.Len(t, list, 6)
	assert.Equal(t, map[string]any{"name": "kubernetes.crash_loop", "integration": "Kubernetes", "label": "Crash loop", "severity": "Warning"}, list[0])
	assert.Equal(t, map[string]any{"name": "kubernetes.node_not_ready", "integration": "Kubernetes", "label": "Node not ready", "severity": "Critical"}, list[3])

	srv, _, _ = newMonitorServer(t, otherRegistration)
	data = mutate(t, srv, `query { monitorSignalList { name label } }`)
	list = data["monitorSignalList"].([]any)
	require.Len(t, list, 7)
	assert.Equal(t, map[string]any{"name": "other.down", "label": "Down"}, list[6])
}

// A target is served with its cluster's id, resolved from its key, and its read
// state; a signal this build does not register is left out of its unknown
// signals. Another integration's target has a null clusterID.
func TestMonitorTargetCollectionWatchServesEveryTarget(t *testing.T) {
	srv, _, _ := newMonitorServer(t)

	resp, events := chatFrames(t, srv, `subscription { monitorTargetCollectionWatch { type target {
		id integration label clusterID currentUID enabled readState unknownSignals { name reason } } } }`)
	defer resp.Body.Close()
	got := targetsByID(t, events)

	assert.Equal(t, map[string]any{
		"id": "t1", "integration": "Kubernetes", "label": "prod", "clusterID": "1", "currentUID": "srv-1",
		"enabled": true, "readState": "Partial",
		"unknownSignals": []any{map[string]any{"name": "kubernetes.node_not_ready", "reason": "PermissionDenied"}},
	}, got["t1"])

	assert.NotContains(t, got, "t2", "a target of an integration the build lacks")

	srv, _, _ = newMonitorServer(t, otherRegistration)
	resp2, events2 := chatFrames(t, srv, `subscription { monitorTargetCollectionWatch { type target { id label clusterID currentUID readState } } }`)
	defer resp2.Body.Close()
	got = targetsByID(t, events2)
	assert.Equal(t, map[string]any{"id": "t2", "label": "Account", "clusterID": nil, "currentUID": nil, "readState": "NeverRead"}, got["t2"])
}

// monitorTargetEnabledSet off returns the target with enabled false and ends its
// open occurrences; an unknown id is refused as not found.
func TestMonitorTargetEnabledSetReturnsTheTarget(t *testing.T) {
	srv, db, _ := newMonitorServer(t)

	data := mutate(t, srv, `mutation { monitorTargetEnabledSet(id: "t1", enabled: false) { id enabled readState } }`)

	assert.Equal(t, map[string]any{"id": "t1", "enabled": false, "readState": "Partial"}, data["monitorTargetEnabledSet"])
	var reason string
	require.NoError(t, db.Read.QueryRow(`SELECT end_reason FROM incident_occurrences WHERE id = 'o2'`).Scan(&reason))
	assert.Equal(t, "stopped", reason)
	raw := string(postGQL(t, srv.URL, `{"query":"mutation { monitorTargetEnabledSet(id: \"nope\", enabled: true) { id } }"}`))
	assert.Contains(t, raw, "KUBETAIL_RECORD_NOT_FOUND")
}

// An incident is served with its subject's label and sorted attributes, its
// stored signal label and severity, its server's UID, and a null last sighting
// and end while open.
func TestIncidentCollectionWatchServesEveryIncident(t *testing.T) {
	srv, _, _ := newMonitorServer(t)

	resp, events := chatFrames(t, srv, `subscription { incidentCollectionWatch { type incident {
		id targetID targetUID subjectLabel subjectAttributes { label value } signal signalLabel severity
		firstSeenAt open occurrenceCount lastStartedAt lastSeenAt lastEndReason } } }`)
	defer resp.Body.Close()
	frames := framesUntilBookmark(t, events)

	require.Len(t, frames, 1)
	assert.Equal(t, map[string]any{
		"id": "i1", "targetID": "t1", "targetUID": "srv-1", "subjectLabel": "Deployment default/web",
		"subjectAttributes": []any{
			map[string]any{"label": "apiGroup", "value": "apps"}, map[string]any{"label": "kind", "value": "Deployment"},
			map[string]any{"label": "name", "value": "web"}, map[string]any{"label": "namespace", "value": "default"},
		},
		"signal": "kubernetes.crash_loop", "signalLabel": "Crash loop", "severity": "Warning",
		"firstSeenAt": "1970-01-01T00:00:01Z", "open": true, "occurrenceCount": float64(2),
		"lastStartedAt": "1970-01-01T00:00:03Z", "lastSeenAt": nil, "lastEndReason": nil,
	}, frames[0]["incident"])
}

// An incident's feed sends its occurrences, highest number first, each frame
// carrying the incident's id, then one Bookmark.
func TestIncidentOccurrenceEventFeedServesTheOccurrences(t *testing.T) {
	srv, _, _ := newMonitorServer(t)

	resp, events := chatFrames(t, srv, `subscription { incidentOccurrenceEventFeed(incidentID: "i1") { type incidentID occurrence {
		id incidentID number startedAt endedAt endReason detail { label value } } } }`)
	defer resp.Body.Close()

	assert.Equal(t, map[string]any{"type": "Occurrence", "incidentID": "i1", "occurrence": map[string]any{
		"id": "o2", "incidentID": "i1", "number": float64(2), "startedAt": "1970-01-01T00:00:03Z",
		"endedAt": nil, "endReason": nil, "detail": []any{},
	}}, nextChatFrame(t, events))
	assert.Equal(t, map[string]any{"type": "Occurrence", "incidentID": "i1", "occurrence": map[string]any{
		"id": "o1", "incidentID": "i1", "number": float64(1), "startedAt": "1970-01-01T00:00:01Z",
		"endedAt": "1970-01-01T00:00:02Z", "endReason": "Resolved",
		"detail": []any{map[string]any{"label": "container", "value": "app"}, map[string]any{"label": "pod", "value": "web-0"}},
	}}, nextChatFrame(t, events))
	assert.Equal(t, map[string]any{"type": "Bookmark", "incidentID": "i1", "occurrence": nil}, nextChatFrame(t, events))
}

// A read that fails ends each monitoring subscription with subscriptionFailed,
// and one opened once the monitor stops is refused as unavailable.
func TestAFailedMonitoringReadEndsTheSubscription(t *testing.T) {
	srv, db, mon := newMonitorServer(t)
	_, err := db.Write.Exec(`ALTER TABLE monitor_targets RENAME TO gone`)
	require.NoError(t, err)

	for _, query := range []string{
		`subscription { monitorTargetCollectionWatch { type } }`,
		`subscription { incidentCollectionWatch { type } }`,
		`subscription { incidentOccurrenceEventFeed(incidentID: "i1") { type } }`,
	} {
		resp := openSSESubscription(t, srv.URL, "", query)
		events := sseEvents(t, resp)
		assert.Contains(t, testutil.Recv(t, events, "the failure frame").data, `"subscriptionFailed":true`, query)
		resp.Body.Close()
	}

	stop, err := mon.Start(t.Context())
	require.NoError(t, err)
	require.NoError(t, stop(context.Background()))
	raw := string(postGQL(t, srv.URL, `{"query":"subscription { incidentCollectionWatch { type } }"}`))
	assert.Contains(t, raw, `"code":"KUBETAIL_SERVICE_UNAVAILABLE"`)
}
