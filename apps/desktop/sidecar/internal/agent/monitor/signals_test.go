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
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/clustertest"
)

func unhealthy(refs ...cluster.KindRef) cluster.ClusterCacheHealth {
	h := cluster.ClusterCacheHealth{Reason: "Watching"}
	for _, ref := range refs {
		h.UnhealthyKindRefs = append(h.UnhealthyKindRefs, cluster.UnhealthyKindRef{KindRef: ref, Reason: "Stale"})
	}
	return h
}

func setOf[T comparable](xs ...T) map[T]bool {
	set := map[T]bool{}
	for _, x := range xs {
		set[x] = true
	}
	return set
}

func TestASignalIsJudgedOnlyWhileEachKindItReadsIsWatching(t *testing.T) {
	all := setOf(signals...)
	for name, tc := range map[string]struct {
		health cluster.ClusterCacheHealth
		want   map[Signal]bool
	}{
		"every kind watching": {unhealthy(), all},
		"an unrelated kind":   {unhealthy(cluster.KindRef{APIVersion: "example.com/v1", Resource: "widgets"}), all},
		"pods stale":          {unhealthy(pods), setOf(NodeNotReady)},
		// Cluster sync off freezes the cache, which reads Paused with no kind listed.
		"sync off":               {cluster.ClusterCacheHealth{Reason: "Paused"}, setOf[Signal]()},
		"pods paused":            {cluster.ClusterCacheHealth{Reason: "Watching", PausedKindRefs: []cluster.KindRef{pods}}, setOf(NodeNotReady)},
		"nodes failing":          {unhealthy(nodes), setOf(CrashLoopBackOff, ImagePullBackOff, PodPending, ReplicasUnavailable)},
		"replica sets resyncing": {unhealthy(replicaSets), setOf(CrashLoopBackOff, ImagePullBackOff, PodPending, NodeNotReady)},
		// An owner kind only moves a pod signal's subject, as a custom controller does.
		"cron jobs forbidden": {unhealthy(cluster.KindRef{APIVersion: "batch/v1", Resource: "cronjobs"}), setOf(signals...)},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, judgedBy(tc.health))
		})
	}
}

func TestACrashLoopingPodNamesItsTopController(t *testing.T) {
	f := watching(t)
	deploy := object("apps/v1", "Deployment", "prod", "web")
	rs := ownedBy(object("apps/v1", "ReplicaSet", "prod", "web-1"), deploy)
	f.apply(t, deploy, rs,
		ownedBy(pod("prod", "web-1-a", "Running", "CrashLoopBackOff"), rs),
		ownedBy(pod("prod", "web-1-b", "Running", "running", "CrashLoopBackOff"), rs),
		ownedBy(pod("prod", "web-1-c", "Running", "running"), rs),
		pod("prod", "bare", "Running", "CrashLoopBackOff"),
	)
	initLoop := pod("prod", "init", "Pending")
	initLoop["spec"].(map[string]any)["initContainers"] = []any{map[string]any{"name": "setup", "image": "img"}}
	initLoop["status"].(map[string]any)["initContainerStatuses"] = []any{map[string]any{"name": "setup", "state": map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}}}}
	f.apply(t, initLoop)

	assert.Equal(t, map[Pair]bool{
		pairOf("apps", "Deployment", "prod", "web", CrashLoopBackOff): true,
		pairOf("", "Pod", "prod", "bare", CrashLoopBackOff):           true,
		pairOf("", "Pod", "prod", "init", CrashLoopBackOff):           true,
	}, readNow(t, f).pairs)
}

func TestAnImagePullFailureIsOneSignal(t *testing.T) {
	f := watching(t)
	f.apply(t, pod("prod", "pull", "Pending", "ErrImagePull"), pod("prod", "backoff", "Pending", "ImagePullBackOff"), pod("prod", "both", "Pending", "ErrImagePull", "ImagePullBackOff"))

	assert.Equal(t, map[Pair]bool{
		pairOf("", "Pod", "prod", "pull", ImagePullBackOff):    true,
		pairOf("", "Pod", "prod", "backoff", ImagePullBackOff): true,
		pairOf("", "Pod", "prod", "both", ImagePullBackOff):    true,
	}, readNow(t, f).pairs)
}

// A Pending Pod of any age is read: the fold's window runs from when its subject was
// first seen Pending, so a workload whose Pending Pods are replaced keeps its window.
func TestAPendingPodIsDetected(t *testing.T) {
	f := watching(t)
	deploy := object("apps/v1", "Deployment", "prod", "web")
	young := youngPod("prod", "young", "Pending")
	f.apply(t, deploy,
		ownedBy(pod("prod", "creating", "Pending", "ContainerCreating"), deploy),
		pod("prod", "unscheduled", "Pending"),
		young,
		pod("prod", "running", "Running", "running"),
		pod("prod", "looping", "Pending", "CrashLoopBackOff"),
		withStatus(pod("prod", "done", "Succeeded"), map[string]any{"phase": "Succeeded"}),
	)

	assert.Equal(t, map[Pair]bool{
		pairOf("apps", "Deployment", "prod", "web", PodPending): true,
		pairOf("", "Pod", "prod", "unscheduled", PodPending):    true,
		pairOf("", "Pod", "prod", "young", PodPending):          true,
		pairOf("", "Pod", "prod", "looping", CrashLoopBackOff):  true,
	}, readNow(t, f).pairs)
}

// body() refuses to inflate past 16 MiB, so a Running Pod whose body is larger fails
// any statement that opens it.
func TestThePendingStatementOpensNoBodyOfARunningPod(t *testing.T) {
	f := watching(t)
	big := pod("prod", "big", "Running", "running")
	big["metadata"].(map[string]any)["annotations"] = map[string]any{"pad": strings.Repeat("x", 17<<20)}
	f.apply(t, big)

	assert.Empty(t, readNow(t, f).pairs)
}

func TestANotReadyNodeIsDetected(t *testing.T) {
	f := watching(t)
	f.apply(t, node("a", "True"), node("b", "False"), node("c", "Unknown"))

	assert.Equal(t, map[Pair]bool{
		pairOf("", "Node", "", "b", NodeNotReady): true,
		pairOf("", "Node", "", "c", NodeNotReady): true,
	}, readNow(t, f).pairs)
}

func TestReplicasUnavailable(t *testing.T) {
	statefulSet := func(current, update string) map[string]any {
		s := object("apps/v1", "StatefulSet", "prod", "db")
		s["spec"] = map[string]any{"replicas": int64(3)}
		return withStatus(s, map[string]any{"readyReplicas": int64(1), "currentRevision": current, "updateRevision": update})
	}
	onDelete := func() map[string]any {
		s := statefulSet("db-1", "db-2")
		s["spec"].(map[string]any)["updateStrategy"] = map[string]any{"type": "OnDelete"}
		return s
	}
	daemonSet := func(updated int) map[string]any {
		return withStatus(object("apps/v1", "DaemonSet", "prod", "agent"), map[string]any{
			"desiredNumberScheduled": int64(3), "numberReady": int64(1), "updatedNumberScheduled": int64(updated),
		})
	}
	onDeleteDaemonSet := func() map[string]any {
		d := daemonSet(2)
		d["spec"] = map[string]any{"updateStrategy": map[string]any{"type": "OnDelete"}}
		return d
	}
	// A partitioned StatefulSet updates only the pods at or above its partition.
	partitioned := func(updated int) map[string]any {
		s := statefulSet("db-1", "db-2")
		s["spec"].(map[string]any)["updateStrategy"] = map[string]any{"type": "RollingUpdate", "rollingUpdate": map[string]any{"partition": int64(2)}}
		s["status"].(map[string]any)["updatedReplicas"] = int64(updated)
		return s
	}
	// A StatefulSet or DaemonSet mid-rollout is reported, to wait a longer window.
	rolling := map[string]bool{"a StatefulSet mid-revision": true, "a partitioned StatefulSet mid-update": true, "a DaemonSet mid-update": true}
	dbShort := pairOf("apps", "StatefulSet", "prod", "db", ReplicasUnavailable)
	agentShort := pairOf("apps", "DaemonSet", "prod", "agent", ReplicasUnavailable)
	short := deployment("web", 2, "True", "NewReplicaSetAvailable")
	rs := ownedBy(object("apps/v1", "ReplicaSet", "prod", "web-1"), short)

	for name, tc := range map[string]struct {
		objects []map[string]any
		want    map[Pair]bool
	}{
		"a short Deployment":       {[]map[string]any{short}, map[Pair]bool{pairOf("apps", "Deployment", "prod", "web", ReplicasUnavailable): true}},
		"a full Deployment":        {[]map[string]any{deployment("web", 3, "True", "NewReplicaSetAvailable")}, map[Pair]bool{}},
		"a Deployment mid-rollout": {[]map[string]any{deployment("web", 2, "True", "ReplicaSetUpdated")}, map[Pair]bool{}},
		"past its deadline": {
			[]map[string]any{deployment("web", 2, "False", "ProgressDeadlineExceeded")},
			map[Pair]bool{pairOf("apps", "Deployment", "prod", "web", ReplicasUnavailable): true},
		},
		"its pod crash-loops": {
			[]map[string]any{short, rs, ownedBy(pod("prod", "web-1-a", "Running", "CrashLoopBackOff"), rs)},
			map[Pair]bool{pairOf("apps", "Deployment", "prod", "web", CrashLoopBackOff): true},
		},
		"a StatefulSet mid-revision": {[]map[string]any{statefulSet("db-1", "db-2")}, map[Pair]bool{dbShort: true}},
		// An OnDelete StatefulSet's revisions differ until each pod is deleted by hand.
		"a short OnDelete DaemonSet": {
			[]map[string]any{onDeleteDaemonSet()},
			map[Pair]bool{pairOf("apps", "DaemonSet", "prod", "agent", ReplicasUnavailable): true},
		},
		"a partitioned StatefulSet mid-update": {[]map[string]any{partitioned(0)}, map[Pair]bool{dbShort: true}},
		"a short partitioned StatefulSet, updated": {
			[]map[string]any{partitioned(1)},
			map[Pair]bool{pairOf("apps", "StatefulSet", "prod", "db", ReplicasUnavailable): true},
		},
		"a short OnDelete StatefulSet": {
			[]map[string]any{onDelete()},
			map[Pair]bool{pairOf("apps", "StatefulSet", "prod", "db", ReplicasUnavailable): true},
		},
		"its young pod is pending": {
			[]map[string]any{short, rs, ownedBy(youngPod("prod", "web-1-new", "Pending"), rs)},
			map[Pair]bool{pairOf("apps", "Deployment", "prod", "web", PodPending): true},
		},
		"a short StatefulSet": {
			[]map[string]any{statefulSet("db-2", "db-2")},
			map[Pair]bool{pairOf("apps", "StatefulSet", "prod", "db", ReplicasUnavailable): true},
		},
		"a DaemonSet mid-update": {[]map[string]any{daemonSet(2)}, map[Pair]bool{agentShort: true}},
		"a short DaemonSet": {
			[]map[string]any{daemonSet(3)},
			map[Pair]bool{pairOf("apps", "DaemonSet", "prod", "agent", ReplicasUnavailable): true},
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := watching(t)
			f.apply(t, tc.objects...)
			r := readNow(t, f)
			assert.Equal(t, tc.want, r.pairs)
			wantRolling := map[Pair]bool{}
			if rolling[name] {
				wantRolling = tc.want
			}
			assert.Equal(t, wantRolling, r.rolling)
		})
	}
}

// A cut answer would read as signals gone, so its signals are unjudged.
func TestACutAnswerLeavesItsSignalsUnjudged(t *testing.T) {
	f := watching(t)
	f.apply(t, pod("prod", "a", "Running", "CrashLoopBackOff"), pod("prod", "b", "Running", "CrashLoopBackOff"), node("n", "False"))

	r := readWith(t, f, 1)
	assert.Equal(t, setOf(PodPending, NodeNotReady), r.judged)
	assert.Equal(t, map[Pair]bool{pairOf("", "Node", "", "n", NodeNotReady): true}, r.pairs)
}

func TestACutPendingOrNodeAnswerLeavesItsSignalsUnjudged(t *testing.T) {
	f := watching(t)
	f.apply(t, pod("prod", "a", "Pending"), pod("prod", "b", "Pending"), node("m", "False"), node("n", "False"))

	r := readWith(t, f, 1)
	assert.Equal(t, setOf(CrashLoopBackOff, ImagePullBackOff), r.judged)
	assert.Empty(t, r.pairs)
}

func TestAStaleKindsSignalsAreUnjudged(t *testing.T) {
	f := watching(t)
	f.health = unhealthy(pods)
	f.apply(t, pod("prod", "a", "Running", "CrashLoopBackOff"), node("n", "False"))

	r := readNow(t, f)
	assert.Equal(t, setOf(NodeNotReady), r.judged)
	assert.Equal(t, map[Pair]bool{pairOf("", "Node", "", "n", NodeNotReady): true}, r.pairs)
}

// A cache whose rows are not trusted, or no cache at all, is no reading; any other
// failure is the reading's.
func TestASyncingCacheIsNoReading(t *testing.T) {
	syncing := watching(t)
	syncing.health = cluster.ClusterCacheHealth{Reason: "Connecting"}
	gone := watching(t)
	gone.readErr = cluster.ErrNotFound
	broken := watching(t)
	broken.readErr = errors.New("boom")

	for name, tc := range map[string]struct {
		f       *fakeService
		wantErr bool
	}{"syncing": {syncing, false}, "no cache": {gone, false}, "failing": {broken, true}} {
		t.Run(name, func(t *testing.T) {
			_, ok, err := newReader(tc.f.CachedData(), 10, 1<<20).read(t.Context(), "1")
			assert.False(t, ok)
			assert.Equal(t, tc.wantErr, err != nil, err)
		})
	}
}

func TestAFailingStatementFailsTheReading(t *testing.T) {
	for name, failIn := range map[string]string{
		"waiting":   "FROM main.containers\n  WHERE state",
		"pending":   "INDEXED BY objects_kind_ns_name",
		"nodes":     "kind = 'Node'",
		"workloads": "kind IN ('Deployment'",
	} {
		t.Run(name, func(t *testing.T) {
			f := watching(t)
			f.failIn = failIn
			_, ok, err := newReader(f.CachedData(), 10, 1<<20).read(t.Context(), "1")
			assert.False(t, ok)
			assert.Error(t, err)
		})
	}
}

// A status that will not parse, or a kind with no rollout rule, reads as no rollout:
// a short workload is reported rather than hidden.
func TestAnUnreadableStatusIsNoRollout(t *testing.T) {
	assert.False(t, rollingOut("Deployment", "not json", "{}"))
	assert.False(t, rollingOut("ReplicaSet", `{"conditions":[]}`, "{}"))
}

// A StatefulSet with no replicas set has one, which an update reaches until it is done.
func TestAStatefulSetsReplicasDefaultToOne(t *testing.T) {
	assert.True(t, rollingOut("StatefulSet", `{"currentRevision":"a","updateRevision":"b"}`, "{}"))
	assert.False(t, rollingOut("StatefulSet", `{"currentRevision":"a","updateRevision":"b","updatedReplicas":1}`, "{}"))
}

// A reading of 5,000 Pods under 500 Deployments, a few of them failing. Over 20 ms, the
// loop should move to a push from the cache.
func BenchmarkAReadingOf5000Pods(b *testing.B) {
	store := clustertest.NewStore(b)
	for d := range 500 {
		deploy := deployment(fmt.Sprintf("web-%d", d), 10, "True", "NewReplicaSetAvailable")
		rs := ownedBy(object("apps/v1", "ReplicaSet", "prod", fmt.Sprintf("web-%d-1", d)), deploy)
		store.Apply(b, deploy)
		store.Apply(b, rs)
		for p := range 10 {
			state := "running"
			if d%100 == 0 && p == 0 {
				state = "CrashLoopBackOff"
			}
			store.Apply(b, ownedBy(pod("prod", fmt.Sprintf("web-%d-1-%d", d, p), "Running", state), rs))
		}
	}
	f := &fakeService{store: store, health: cluster.ClusterCacheHealth{Reason: "Watching"}}
	r := newReader(f.CachedData(), 50_000, 16<<20)

	for b.Loop() {
		got, ok, err := r.read(b.Context(), "1")
		if err != nil || !ok || len(got.pairs) != 5 {
			b.Fatalf("read: %d pairs, ok %v, err %v", len(got.pairs), ok, err)
		}
	}
}
