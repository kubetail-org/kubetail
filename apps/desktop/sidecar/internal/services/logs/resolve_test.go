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
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/kubelet"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
)

// resolveOf is what view resolves to over f.
func resolveOf(t *testing.T, f *fakeService, view Scope) (target, error) {
	t.Helper()
	return resolve(t.Context(), f, "7", view, false)
}

// A deployment's pods come off the mirror through its replica set, one stream per
// container named, with where each runs.
func TestADeploymentsPodsComeOffTheMirror(t *testing.T) {
	t.Parallel()
	got, err := resolveOf(t, webMirror(t), webScope)

	require.NoError(t, err)
	assert.Equal(t, "uid-1", got.serverUID)
	assert.Equal(t, cluster.ClusterCacheID(11), got.cacheID)
	assert.Equal(t, []logline.StreamKey{
		{Namespace: "prod", Pod: "web-1", Container: "app"},
		{Namespace: "prod", Pod: "web-2", Container: "app"},
	}, keysOf(got))
	assert.Equal(t, []logline.Place{
		{Node: "node-a", Region: "eu", Zone: "eu-1a", OS: "linux", Arch: "amd64"},
		{Node: "node-b", Region: "eu", Zone: "eu-1b", OS: "linux", Arch: "arm64"},
	}, placesOf(got))
}

// A workload is walked down through controller references alone: a pod that only
// names it as an owner, without controlling, is not one of its pods.
func TestAWorkloadIsWalkedDownItsControllers(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	notController := webReplicaSet()
	notController["controller"] = false
	f.store.Apply(t, pod("adopted", "uid-adopted", "node-a", notController, container{name: "app"}))

	got, err := resolveOf(t, f, webScope)

	require.NoError(t, err)
	assert.Len(t, keysOf(got), 2)
}

// A stream carries the controllers above its pod, nearest first, under a Pod source
// as under its workload, so the file can find the pod by any of them once it is gone.
// A pod the mirror shows before its ReplicaSet carries the ReplicaSet alone, and the
// Deployment too once the ReplicaSet arrives.
func TestAStreamCarriesItsPodsControllerChain(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	web1 := Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: "web-1", Containers: []string{"app"}}}}
	for _, scope := range []Scope{webScope, web1} {
		got, err := resolveOf(t, f, scope)
		require.NoError(t, err)
		assert.Equal(t, []string{"rs-web", "dep-web"}, got.streams[0].owners)
	}

	f.store.Apply(t, pod("api-1", "uid-api-1", "node-a", controller("apps/v1", "ReplicaSet", "api-5c", "rs-api"), container{name: "app"}))
	api1 := Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: "api-1"}}}
	got, err := resolveOf(t, f, api1)
	require.NoError(t, err)
	assert.Equal(t, []string{"rs-api"}, got.streams[0].owners)

	f.store.Apply(t, workload("apps/v1", "ReplicaSet", "api-5c", "rs-api", controller("apps/v1", "Deployment", "api", "dep-api")))
	got, err = resolveOf(t, f, api1)
	require.NoError(t, err)
	assert.Equal(t, []string{"rs-api", "dep-api"}, got.streams[0].owners)
}

// A target carries each source's UID, which the file's chains name: a source with
// pods carries its own as one with none does, and a source the mirror lacks, none.
func TestATargetCarriesEachSourcesUID(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	f.store.Apply(t, workload("apps/v1", "Deployment", "idle", "dep-idle", nil))
	scope := Scope{Sources: []ScopeSource{
		webScope.Sources[0],
		{Namespace: "prod", Kind: SourceDeployment, Name: "idle"},
		{Namespace: "prod", Kind: SourceDeployment, Name: "missing"},
		{Namespace: "prod", Kind: SourcePod, Name: "web-1"},
	}}

	got, err := resolveOf(t, f, scope)

	require.NoError(t, err)
	assert.Equal(t, []string{"dep-web", "dep-idle", "", "uid-web-1"}, got.sourceUIDs)
	assert.Equal(t, []SourceState{Reading, Idle, Missing, Reading}, got.states)
}

// AllContainers reads every container, and previous goes through.
func TestAllContainersReadsEveryContainer(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	f.store.Apply(t, pod("web-1", "uid-web-1", "node-a", nil, container{name: "app", restarted: true}, container{name: "istio-proxy", restarted: true}))

	got, err := resolveOf(t, f, Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: "web-1", AllContainers: true, Previous: true}}})

	require.NoError(t, err)
	assert.Equal(t, []logline.StreamKey{
		{Namespace: "prod", Pod: "web-1", Container: "app", Previous: true},
		{Namespace: "prod", Pod: "web-1", Container: "istio-proxy", Previous: true},
	}, keysOf(got))
}

// With no container named, each pod reads its own default, as kubectl logs picks
// it: the one its annotation names when that is one of its containers, else its
// first container, never an init container.
func TestEachPodReadsItsOwnDefault(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	annotated := pod("web-2", "uid-web-2", "node-b", webReplicaSet(), container{name: "app"}, container{name: "istio-proxy"})
	annotated["metadata"].(map[string]any)["annotations"] = map[string]any{DefaultContainerAnnotation: "istio-proxy"}
	f.store.Apply(t, annotated)
	stale := pod("web-3", "uid-web-3", "node-b", webReplicaSet(), container{name: "app"})
	stale["metadata"].(map[string]any)["annotations"] = map[string]any{DefaultContainerAnnotation: "gone"}
	f.store.Apply(t, withInit(stale, "migrate"))

	got, err := resolveOf(t, f, Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourceDeployment, Name: "web"}}})

	require.NoError(t, err)
	assert.Equal(t, []logline.StreamKey{
		{Namespace: "prod", Pod: "web-1", Container: "app"},
		{Namespace: "prod", Pod: "web-2", Container: "istio-proxy"},
		{Namespace: "prod", Pod: "web-3", Container: "app"},
	}, keysOf(got))
}

// Init containers come before the others, as they run and as kubectl lists them.
func TestInitContainersComeFirst(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	f.store.Apply(t, withInit(pod("web-1", "uid-web-1", "node-a", nil, container{name: "app"}), "migrate"))

	got, err := resolveOf(t, f, Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: "web-1", AllContainers: true}}})

	require.NoError(t, err)
	assert.Equal(t, []logline.StreamKey{
		{Namespace: "prod", Pod: "web-1", Container: "migrate"},
		{Namespace: "prod", Pod: "web-1", Container: "app"},
	}, keysOf(got))
}

// CountPods counts the pods the view reads: those a workload controls, and among
// them those with a container named that restarted.
func TestCountPodsCountsTheViewsPods(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	f.store.Apply(t, pod("web-2", "uid-web-2", "node-b", webReplicaSet(), container{name: "app", restarted: true}, container{name: "istio-proxy", restarted: true}))
	notController := webReplicaSet()
	notController["controller"] = false
	f.store.Apply(t, pod("adopted", "uid-adopted", "node-a", notController, container{name: "app", restarted: true}))

	count := func(src ScopeSource) PodCount {
		t.Helper()
		var c PodCount
		require.NoError(t, f.CachedData().ReadActive(t.Context(), "7", maxRows, maxBytes, func(q cluster.CacheReading, _ cluster.ActiveCluster) error {
			var err error
			c, err = CountPods(q, src)
			return err
		}))
		return c
	}

	assert.Equal(t, PodCount{Pods: 2, Previous: 1}, count(webScope.Sources[0]))
	assert.Equal(t, PodCount{Pods: 2, Previous: 1}, count(ScopeSource{Namespace: "prod", Kind: SourceDeployment, Name: "web"}))
	assert.Equal(t, PodCount{Pods: 2}, count(ScopeSource{Namespace: "prod", Kind: SourceDeployment, Name: "web", Containers: []string{"db"}}))
}

// A filter on a node's label leaves a pod out before any stream opens.
func TestALabelFilterLeavesAPodOut(t *testing.T) {
	t.Parallel()
	for _, f := range []ScopeFilter{
		{Field: FilterZone, Values: []string{"eu-1b"}},
		{Field: FilterArch, Values: []string{"arm64"}},
		{Field: FilterRegion, Values: []string{"eu"}},
		{Field: FilterOS, Values: []string{"linux"}},
	} {
		got, err := resolveOf(t, webMirror(t), Scope{Sources: webScope.Sources, Filters: []ScopeFilter{f}})

		require.NoError(t, err, f.Field)
		switch f.Field {
		case FilterZone, FilterArch:
			require.Len(t, keysOf(got), 1, f.Field)
			assert.Equal(t, "web-2", keysOf(got)[0].Pod)
		default:
			assert.Len(t, keysOf(got), 2, f.Field)
		}
	}
}

// A Node filter reads the pod's node and needs no Node object.
func TestANodeFilterNeedsNoNodeObject(t *testing.T) {
	t.Parallel()
	got, err := resolveOf(t, newMirror(t, webObjects()...), Scope{
		Sources: webScope.Sources,
		Filters: []ScopeFilter{{Field: FilterNode, Values: []string{"node-a"}}},
	})

	require.NoError(t, err)
	require.Len(t, keysOf(got), 1)
	assert.Equal(t, "web-1", keysOf(got)[0].Pod)
}

// A label filter on a mirror with no nodes is refused, naming every label, by a
// page or a tail once the file holds nothing the filter keeps.
func TestALabelFilterWithNoNodesIsRefused(t *testing.T) {
	t.Parallel()
	f := newMirror(t, webObjects()...)
	view := Scope{
		Sources: webScope.Sources,
		Filters: []ScopeFilter{{Field: FilterZone, Values: []string{"eu-1a"}}, {Field: FilterArch, Values: []string{"arm64"}}},
	}
	const refused = "Kubetail cannot read this cluster's nodes, so it cannot filter by zone or arch"

	for _, follow := range []bool{false, true} {
		got, err := resolve(t.Context(), f, "7", view, follow)
		require.NoError(t, err, "a page and a tail look in the file before they refuse")
		assert.EqualError(t, got.noNodes, refused)
		assert.Equal(t, []string{"dep-web"}, got.sourceUIDs, "the file is asked for the workload's gone pods")
	}
}

// A pod whose node the mirror does not hold, while it holds others, is left out
// by a label filter, and the view reads no node.
func TestAPodOnAnUnknownNodeIsLeftOut(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	got, err := resolveOf(t, f, Scope{
		Sources: []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: "job-1"}},
		Filters: []ScopeFilter{{Field: FilterOS, Values: []string{"linux"}}},
	})

	require.NoError(t, err)
	assert.Empty(t, keysOf(got))
	assert.Equal(t, []SourceState{Idle}, got.states)
	for _, s := range f.statements {
		assert.NotContains(t, s, "n.name IN")
	}
}

// A previous instance is read only of a container that restarted.
func TestAPreviousInstanceIsReadOnlyWhereItRestarted(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	f.store.Apply(t, pod("web-2", "uid-web-2", "node-b", webReplicaSet(), container{name: "app", restarted: true}, container{name: "istio-proxy"}))

	got, err := resolveOf(t, f, Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourceDeployment, Name: "web", Containers: []string{"app"}, Previous: true}}})

	require.NoError(t, err)
	assert.Equal(t, []logline.StreamKey{{Namespace: "prod", Pod: "web-2", Container: "app", Previous: true}}, keysOf(got))
}

// Each stream carries the runtime id of the instance it reads, and a container
// whose id a cursor could not carry is not read: the cluster wrote the id, and a
// page must never hand out a cursor it would refusal.New.
func TestAStreamCarriesItsInstancesID(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	body := pod("odd", "uid-odd", "node-a", nil, container{name: "app"}, container{name: "tabbed"}, container{name: "long"})
	statuses := body["status"].(map[string]any)["containerStatuses"].([]any)
	statuses[1].(map[string]any)["containerID"] = "containerd://a\tb"
	statuses[2].(map[string]any)["containerID"] = "containerd://" + strings.Repeat("a", 244)
	f.store.Apply(t, body)

	got, err := resolveOf(t, f, Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: "odd", AllContainers: true}}})

	require.NoError(t, err)
	assert.Equal(t, []logline.StreamKey{{Namespace: "prod", Pod: "odd", Container: "app"}}, keysOf(got))
	assert.Equal(t, containerID("uid-odd", "app", 0), got.streams[0].gen.instance)
}

// Each stream carries the snapshot its file row keeps once the pod is gone: the
// pod's UID and labels, whether the container is the pod's default, when the
// instance read started and finished, and the pod's current instance; a fetch
// learns each Pod source's pod and its current instances too.
func TestResolveCarriesEachStreamsSnapshot(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	body := pod("api", "uid-api", "node-a", nil, container{name: "app", restarted: true}, container{name: "sidecar"})
	body["metadata"].(map[string]any)["labels"] = map[string]any{"app": "api"}
	statuses := body["status"].(map[string]any)["containerStatuses"].([]any)
	app := statuses[0].(map[string]any)
	app["state"] = map[string]any{"running": map[string]any{"startedAt": "2026-10-08T10:05:00Z"}}
	app["lastState"].(map[string]any)["terminated"].(map[string]any)["startedAt"] = "2026-10-08T10:00:00Z"
	app["lastState"].(map[string]any)["terminated"].(map[string]any)["finishedAt"] = "2026-10-08T10:04:59Z"
	f.store.Apply(t, body)
	source := ScopeSource{Namespace: "prod", Kind: SourcePod, Name: "api", AllContainers: true}
	previous := source
	previous.Previous = true

	got, err := resolveOf(t, f, Scope{Sources: []ScopeSource{source, previous}})

	require.NoError(t, err)
	require.Len(t, got.streams, 3)
	current, sidecar, last := got.streams[0], got.streams[1], got.streams[2]
	assert.Equal(t, "uid-api", current.gen.uid)
	assert.JSONEq(t, `{"app":"api"}`, current.labels)
	assert.True(t, current.isDefault)
	assert.False(t, sidecar.isDefault)
	assert.Equal(t, at(5*time.Minute).UnixNano(), current.startedAt)
	assert.Zero(t, current.finishedAt)
	assert.Equal(t, containerID("uid-api", "app", 1), current.gen.instance)
	assert.Equal(t, containerID("uid-api", "app", 0), last.gen.instance)
	assert.Equal(t, at(0).UnixNano(), last.startedAt)
	assert.Equal(t, at(5*time.Minute-time.Second).UnixNano(), last.finishedAt)
	assert.Equal(t, map[string]string{"prod/api": "uid-api"}, got.pods)
	assert.Equal(t, map[string]string{"prod/api": "app"}, got.defaults)
	assert.Equal(t, map[string]string{
		"prod/api/app":     containerID("uid-api", "app", 1),
		"prod/api/sidecar": containerID("uid-api", "sidecar", 0),
	}, got.current)
}

// A container waiting to start has no log, so its source is idle until it does;
// one restarting after a crash still has its last instance's, which kubectl logs
// serves meanwhile.
func TestAContainerWaitingToStartIsNotRead(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	f.store.Apply(t, waiting(pod("creating", "uid-creating", "node-a", nil, container{name: "app"})))
	f.store.Apply(t, waiting(pod("crashing", "uid-crashing", "node-a", nil, container{name: "app", restarted: true})))

	got, err := resolveOf(t, f, Scope{Sources: []ScopeSource{
		{Namespace: "prod", Kind: SourcePod, Name: "creating"},
		{Namespace: "prod", Kind: SourcePod, Name: "crashing"},
	}})

	require.NoError(t, err)
	assert.Equal(t, []SourceState{Idle, Reading}, got.states)
	assert.Equal(t, []logline.StreamKey{{Namespace: "prod", Pod: "crashing", Container: "app"}}, keysOf(got))
}

// A container named twice across sources is read once.
func TestAContainerIsReadOnce(t *testing.T) {
	t.Parallel()
	got, err := resolveOf(t, webMirror(t), Scope{Sources: []ScopeSource{
		webScope.Sources[0],
		{Namespace: "prod", Kind: SourcePod, Name: "web-1", Containers: []string{"app"}},
	}})

	require.NoError(t, err)
	assert.Len(t, keysOf(got), 2)
}

// A view follows each source as its object and pods come and go, so none is
// refused for reading nothing now: each says whether its object is missing or
// it has nothing to read yet.
func TestEachSourceSaysWhatItHoldsNow(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	f.store.Mirror(t, "apps/v1", "StatefulSet")
	f.store.Apply(t, workload("apps/v1", "Deployment", "idle", "dep-idle", nil))
	f.store.Apply(t, pod("bare", "uid-bare", "", nil))
	sources := []struct {
		src  ScopeSource
		want SourceState
	}{
		{ScopeSource{Namespace: "prod", Kind: SourcePod, Name: "web-1"}, Reading},
		{ScopeSource{Namespace: "prod", Kind: SourcePod, Name: "nope"}, Missing},
		{ScopeSource{Namespace: "prod", Kind: SourceStatefulSet, Name: "web"}, Missing},
		{ScopeSource{Namespace: "prod", Kind: SourceDeployment, Name: "idle"}, Idle},
		{ScopeSource{Namespace: "prod", Kind: SourceDeployment, Name: "web", Containers: []string{"api"}}, Idle},
		{ScopeSource{Namespace: "prod", Kind: SourceDeployment, Name: "web", Previous: true}, Idle},
		{ScopeSource{Namespace: "prod", Kind: SourcePod, Name: "bare"}, Idle},
		{ScopeSource{Namespace: "prod", Kind: SourcePod, Name: "web-1", Previous: true}, Idle},
	}
	var view Scope
	var want []SourceState
	for _, s := range sources {
		view.Sources = append(view.Sources, s.src)
		want = append(want, s.want)
	}

	got, err := resolveOf(t, f, view)

	require.NoError(t, err)
	assert.Equal(t, want, got.states)
	assert.Equal(t, []logline.StreamKey{{Namespace: "prod", Pod: "web-1", Container: "app"}}, keysOf(got))
}

// A container whose runtime id a line cursor could not carry is left out, so a
// pod with no other reads as idle.
func TestAContainerWithAnUnreadableIDIsLeftOut(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	odd := pod("odd", "uid-odd", "node-a", nil, container{name: "app"})
	odd["status"].(map[string]any)["containerStatuses"].([]any)[0].(map[string]any)["containerID"] = "containerd://odd\x01"
	f.store.Apply(t, odd)

	got, err := resolveOf(t, f, Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: "odd"}}})

	require.NoError(t, err)
	assert.Empty(t, keysOf(got))
	assert.Equal(t, []SourceState{Idle}, got.states)
}

// A kind the cache does not mirror is refused, since no object of it could ever
// appear to follow, and so is a cache that mirrors no pods.
func TestAnUnmirroredKindIsRefused(t *testing.T) {
	t.Parallel()
	_, err := resolveOf(t, webMirror(t), Scope{Sources: []ScopeSource{webScope.Sources[0], {Namespace: "prod", Kind: SourceJob, Name: "nightly"}}})
	assert.EqualError(t, err, "Kubetail's cache of this cluster holds no jobs, so it cannot follow jobs/nightly in prod")
	var unmirrored refusal.Unmirrored
	require.ErrorAs(t, err, &unmirrored)
	assert.Equal(t, 1, unmirrored.Source)

	_, err = resolveOf(t, newMirror(t, workload("apps/v1", "Deployment", "web", "dep-web", nil)), webScope)
	require.ErrorAs(t, err, &unmirrored)
	assert.Equal(t, -1, unmirrored.Source)
}

// A kind Kubetail does not know is refused, never read as matching nothing.
func TestAnUnknownKindIsRefused(t *testing.T) {
	t.Parallel()
	_, err := resolveOf(t, webMirror(t), Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: "cronjobs", Name: "web"}}})
	assert.EqualError(t, err, `Kubetail cannot read the logs of a "cronjobs"`)
}

// A filter field Kubetail does not know is refused, never read as matching nothing.
func TestAnUnknownFilterFieldIsRefused(t *testing.T) {
	t.Parallel()
	_, err := resolveOf(t, webMirror(t), Scope{Sources: webScope.Sources, Filters: []ScopeFilter{{Field: "rack", Values: []string{"a"}}}})
	assert.EqualError(t, err, `Kubetail cannot filter by "rack"`)
}

// A cluster with no cache, or a cache still syncing, is refused as the tool
// refuses it, and so is a cluster gone or moved under the read, or a cache gone
// under one of its statements.
func TestACacheNotReadyIsRefused(t *testing.T) {
	t.Parallel()
	for name, set := range map[string]func(f *fakeService){
		"no cache":       func(f *fakeService) { f.active.Cache = nil },
		"no health":      func(f *fakeService) { f.noHealth = true },
		"record gone":    func(f *fakeService) { f.readActiveErr = cluster.ErrNotFound },
		"identity moved": func(f *fakeService) { f.readActiveErr = cluster.ErrIdentityMoved },
		"no cache file":  func(f *fakeService) { f.noQuery = true },
	} {
		t.Run(name, func(t *testing.T) {
			f := webMirror(t)
			set(f)
			_, err := resolveOf(t, f, webScope)
			assert.ErrorIs(t, err, ErrNoCache)
		})
	}

	f := webMirror(t)
	f.health = cluster.ClusterCacheHealth{Reason: "Connecting"}
	_, err := resolveOf(t, f, Scope{})
	assert.ErrorIs(t, err, ErrSyncing)
}

// A read of the cluster that fails is the failure, whichever statement it fails on.
func TestAFailedReadIsTheFailure(t *testing.T) {
	t.Parallel()
	// web1Elsewhere reads a container web-1 lacks, so only the pod's own reading
	// asks where it runs.
	web1Elsewhere := podSource("web-1", false, "db")
	// starting opens with a previous instance, which never starts, so the check
	// asks of the source after it.
	starting := Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: "web-2", Previous: true}, webScope.Sources[0]}}
	nowhere := webScope
	nowhere.Filters = []ScopeFilter{{Field: FilterZone, Values: []string{"nowhere"}}}
	for name, c := range map[string]struct {
		set    func(f *fakeService)
		view   Scope
		follow bool
	}{
		"reading":         {set: func(f *fakeService) { f.readActiveErr = errors.New("db closed") }, view: webScope},
		"health":          {set: func(f *fakeService) { f.healthErr = errors.New("db closed") }, view: webScope},
		"every query":     {set: func(f *fakeService) { f.queryErr = errors.New("db closed") }, view: webScope},
		"the node query":  {set: func(f *fakeService) { f.queryErr, f.queryErrOn = errors.New("db closed"), "kind = 'Node'" }, view: webScope},
		"the kinds query": {set: func(f *fakeService) { f.queryErr, f.queryErrOn = errors.New("db closed"), "FROM kinds" }, view: webScope},
		"the source's kind": {set: func(f *fakeService) {
			f.queryErr, f.queryErrOn = errors.New("db closed"), "FROM kinds WHERE api_version = 'apps/v1'"
		}, view: webScope},
		"a pod source's query": {set: func(f *fakeService) {
			f.queryErr, f.queryErrOn = errors.New("db closed"), "json_group_object"
		}, view: web1Scope},
		"a pod source's node": {set: func(f *fakeService) { f.queryErr, f.queryErrOn = errors.New("db closed"), "kind = 'Node'" }, view: web1Elsewhere},
		"the nodes listed": {set: func(f *fakeService) {
			f.queryErr, f.queryErrOn = errors.New("db closed"), "n.kind = 'Node' LIMIT 1"
		}, view: nowhere},
		"the starting query": {set: func(f *fakeService) {
			f.queryErr, f.queryErrOn = errors.New("db closed"), "coalesce(c.last_container_id, '') = '' LIMIT 1"
		}, view: starting, follow: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := webMirror(t)
			c.set(f)
			_, err := resolve(t.Context(), f, "7", c.view, c.follow)
			assert.EqualError(t, err, "db closed")
		})
	}
}

// CountPods refuses a kind it cannot read, and fails with the mirror.
func TestCountPodsFailsWithItsRead(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	count := func(src ScopeSource) error {
		return f.CachedData().ReadActive(t.Context(), "7", maxRows, maxBytes, func(q cluster.CacheReading, _ cluster.ActiveCluster) error {
			_, err := CountPods(q, src)
			return err
		})
	}

	assert.EqualError(t, count(ScopeSource{Namespace: "prod", Kind: "Gadget", Name: "web"}), `Kubetail cannot read the logs of a "Gadget"`)
	f.queryErr = errors.New("db closed")
	assert.EqualError(t, count(webScope.Sources[0]), "db closed")
}

// An answer the mirror cut short is refused rather than read as every row: the
// nodes, or a source's pods.
func TestACutAnswerIsRefused(t *testing.T) {
	t.Parallel()
	for name, on := range map[string]string{"nodes": "kind = 'Node'", "pods": "kind = 'Pod'"} {
		t.Run(name, func(t *testing.T) {
			f := webMirror(t)
			f.moreOn = on
			_, err := resolveOf(t, f, webScope)
			assert.EqualError(t, err, "the cluster's cache answered more than Kubetail reads at once")
		})
	}
}

// A view's containers and filters narrow the pods statement itself, so the row cap
// counts only what the view reads, and the nodes statement reads only the nodes
// its streams run on.
func TestTheFiltersNarrowTheStatement(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	got, err := resolveOf(t, f, Scope{
		Sources: webScope.Sources,
		Filters: []ScopeFilter{
			{Field: FilterNode, Values: []string{"node-a"}},
			{Field: FilterZone, Values: []string{"eu-1a", "eu-1b"}},
		},
	})
	require.NoError(t, err)
	require.Len(t, keysOf(got), 1)

	n := len(f.statements)
	pods := f.statements[n-2]
	assert.Contains(t, pods, "c.name IN ('app')")
	assert.Contains(t, pods, "p.node IN ('node-a')")
	assert.Contains(t, pods, "l.key = 'topology.kubernetes.io/zone' AND l.value IN ('eu-1a', 'eu-1b')")
	assert.Contains(t, f.statements[n-1], "n.name IN ('node-a')")
}

// A view of more containers than Kubetail reads at once is refused, reading no more
// than one past the limit and no node; one at the limit is not.
func TestAViewOfTooManyContainersIsRefused(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	addPods(t, f, MaxStreams-1)
	_, err := resolveOf(t, f, webScope)
	require.EqualError(t, err, "this view reads more than 100 containers, the most Kubetail reads at once; filter it by node, or narrow it to fewer sources, containers or pods")
	pods := f.statements[len(f.statements)-1]
	assert.True(t, strings.HasSuffix(pods, "LIMIT 101"), pods)

	f = webMirror(t)
	addPods(t, f, MaxStreams-2)
	got, err := resolveOf(t, f, webScope)
	require.NoError(t, err)
	assert.Len(t, got.streams, MaxStreams)
}

// Too many containers across sources are refused too.
func TestTooManyAcrossSourcesIsRefused(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	addPods(t, f, MaxStreams-2)
	_, err := resolveOf(t, f, Scope{Sources: []ScopeSource{webScope.Sources[0], {Namespace: "prod", Kind: SourcePod, Name: "job-1"}}})
	assert.ErrorIs(t, err, ErrTooMany)
}

// A resolve decompresses a pod's body once a statement, however many containers
// the pod has: a tail's resolve of a Pod source reads the pod in three statements.
func TestAResolveReadsEachPodsBodyOnceAStatement(t *testing.T) {
	t.Parallel()
	containers := make([]container, MaxStreams)
	for i := range containers {
		containers[i] = container{name: "c" + strconv.Itoa(i)}
	}
	f := newMirror(t, append([]map[string]any{pod("big", "uid-big", "node-a", nil, containers...)}, webNodes()...)...)
	all := Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: "big", AllContainers: true}}}

	for _, view := range []Scope{podSource("big", false), all} {
		before := f.store.BodyCalls()
		got, err := resolve(t.Context(), f, "7", view, true)
		require.NoError(t, err)
		require.NotEmpty(t, got.streams)
		assert.LessOrEqual(t, f.store.BodyCalls()-before, int64(3))
	}
}

// A container status the mirror serves that is not one Kubetail can read dates the
// instance at nothing, so a stream of it has no start and no end.
func TestAnUnreadableStatusDatesNothing(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"", "not json", `{"state":{"running":{"startedAt":7}}}`} {
		started, finished := instanceTimes(status, false)
		assert.Zero(t, started, status)
		assert.Zero(t, finished, status)
	}
}

// A resolve fails with whichever of its reads of the mirror fails, rather than
// reading a refused answer as a kind not mirrored, an object missing or a pod gone.
func TestAResolveFailsWithEachOfItsReads(t *testing.T) {
	t.Parallel()
	for name, on := range map[string]string{
		"kinds":        "FROM kinds",
		"object":       "SELECT uid FROM objects WHERE api_version",
		"pod's ids":    "json_group_object",
		"first starts": "JOIN containers c ON c.pod_uid = p.uid",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := webMirror(t)
			f.queryErr, f.queryErrOn = errors.New("db closed"), on
			view := Scope{Sources: []ScopeSource{webScope.Sources[0], {Namespace: "prod", Kind: SourcePod, Name: "nope"}}}
			_, err := resolve(t.Context(), f, "7", view, true)
			assert.EqualError(t, err, "db closed")
		})
	}
}

// refusal.New builds a refusal of its code and words.
func TestRefuseBuildsACodedRefusal(t *testing.T) {
	t.Parallel()
	r := refusal.New(refusal.CodeBadPage, "a page holds at most 500 lines")

	assert.Equal(t, refusal.CodeBadPage, r.Code)
	assert.EqualError(t, r, "a page holds at most 500 lines")
}

// Every refusal the package makes names its code.
func TestEveryRefusalCarriesItsCode(t *testing.T) {
	t.Parallel()
	_, badGrep := logline.CompileGrep("(")
	_, longGrep := logline.CompileGrep(string(make([]byte, logline.MaxGrep+1)))
	_, badKind := kindOf(ScopeSource{Kind: "secret"})
	unauthorized := kubelet.FatalError(&kubelet.StatusError{Code: http.StatusUnauthorized})
	for code, refusals := range map[refusal.Code][]error{
		refusal.CodeNoCache:      {ErrNoCache},
		refusal.CodeSyncing:      {ErrSyncing},
		refusal.CodeTooMany:      {ErrTooMany, errCut},
		refusal.CodeRevoked:      {kubelet.ErrRevoked},
		refusal.CodeReplaced:     {errReplaced},
		refusal.CodeFinished:     {errStopped},
		refusal.CodeBadGrep:      {badGrep, longGrep},
		refusal.CodeBadScope:     {checkFilters([]ScopeFilter{{Field: "color"}}), badKind},
		refusal.CodeReadFailed:   {everyStreamFailed([]string{"web-1/app: refused"})},
		refusal.CodeUnauthorized: {unauthorized},
		refusal.CodeTooLoud:      {kubelet.LoudAt(time.Time{}).Refusal},
	} {
		for _, err := range refusals {
			r, ok := refusal.As(err)
			if assert.True(t, ok, "%s: %v", code, err) {
				assert.Equal(t, code, r.Code, err.Error())
			}
		}
	}
}

// A resolve of a deployment's view over a real store, as every page and tail
// reconcile runs one.
func BenchmarkResolve(b *testing.B) {
	f := webMirror(b)
	for b.Loop() {
		if _, err := resolve(b.Context(), f, "7", webScope, false); err != nil {
			b.Fatal(err)
		}
	}
}
