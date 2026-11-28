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
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor"
)

const webKey = "apps/Deployment/default/web"

// A Deployment with three crash-looping pods holds one crash loop on the
// Deployment, its detail naming the first pod by name, its container, node,
// image and restarts, and a pods of 3; its attributes name the Deployment.
func TestACrashLoopIsTheTopControllers(t *testing.T) {
	objects := webDeployment(3, 0)
	for _, name := range []string{"web-c", "web-a", "web-b"} {
		objects = append(objects, pod("uid-"+name, name, underWeb(), "Running", running("sidecar"), crashing("app")))
	}

	got := observed(readingOf(t, newFake(t, objects...)), SignalCrashLoop)

	require.Len(t, got, 1)
	o := got[webKey]
	assert.Equal(t, "Deployment default/web", o.Subject.Label)
	assert.Equal(t, map[string]string{"apiGroup": "apps", "kind": "Deployment", "namespace": "default", "name": "web"}, o.Subject.Attributes)
	assert.Equal(t, map[string]string{
		"pod": "web-a", "container": "app", "node": "node-1", "image": "app:1",
		"restarts": "7", "lastReason": "Error", "lastExitCode": "1", "pods": "3",
	}, o.Detail)
}

// An image pull failing names its reason; a pod with no controller is its own
// subject.
func TestAnImagePullNamesItsReason(t *testing.T) {
	f := newFake(t, pod("uid-1", "solo", nil, "Pending", ctr{name: "app", image: "app:9", waiting: "ErrImagePull"}))

	got := observed(readingOf(t, f), SignalImagePull)

	assert.Equal(t, map[string]string{"pod": "solo", "container": "app", "node": "node-1", "image": "app:9", "reason": "ErrImagePull", "pods": "1"},
		got["/Pod/default/solo"].Detail)
	assert.Empty(t, observed(readingOf(t, f), SignalPodPending), "a pod held by another pod signal is not pending")
}

// The container signals read no body for their detail.
func TestTheContainerStatementsDecompressNoBody(t *testing.T) {
	assert.NotContains(t, stmtCrashLoop, "body(")
	assert.NotContains(t, stmtImagePull, "body(")
}

// A Pending Pod whose summary reads ContainerCreating is detected, and so is an
// unscheduled Pod with no container statuses, which carries its PodScheduled
// condition's reason and message and no node.
func TestAPendingPodIsDetected(t *testing.T) {
	creating := pod("uid-1", "creating", nil, "Pending", ctr{name: "app", image: "app:1", waiting: "ContainerCreating"})
	unscheduled := map[string]any{
		"apiVersion": "v1", "kind": "Pod", "metadata": meta("uid-2", "unscheduled", nil),
		"spec": map[string]any{"containers": []any{map[string]any{"name": "app", "image": "app:1"}}},
		"status": map[string]any{"phase": "Pending", "conditions": []any{map[string]any{
			"type": "PodScheduled", "status": "False", "reason": "Unschedulable", "message": "0/3 nodes are available",
		}}},
	}

	got := observed(readingOf(t, newFake(t, creating, unscheduled)), SignalPodPending)

	assert.Equal(t, map[string]string{"pod": "creating", "node": "node-1", "reason": "ContainerCreating", "pods": "1"}, got["/Pod/default/creating"].Detail)
	assert.Equal(t, map[string]string{"pod": "unscheduled", "reason": "Unschedulable", "message": "0/3 nodes are available", "pods": "1"},
		got["/Pod/default/unscheduled"].Detail)
}

// The pending statement decompresses no body of a Running Pod or of one another
// pod signal holds: each body below says Pending, and neither is read.
func TestThePendingStatementSkipsTheBodiesItNeedsNot(t *testing.T) {
	f := newFake(t,
		pod("uid-1", "runs", nil, "Running", running("app")),
		pod("uid-2", "crashes", nil, "Pending", crashing("app")),
	)
	pendingBody := map[string]any{"status": map[string]any{"phase": "Pending"}}
	f.store.SetBody(t, "uid-1", pendingBody)
	f.store.SetBody(t, "uid-2", pendingBody)

	assert.Empty(t, observed(readingOf(t, f), SignalPodPending))
}

// A Node whose Ready condition is not True holds node_not_ready with its
// condition's status, reason and message; a value the cluster does not give is
// left out.
func TestANodeNotReadyCarriesItsReadyCondition(t *testing.T) {
	f := newFake(t,
		node("worker-1", "False", "KubeletNotReady", "PLEG is not healthy"),
		node("worker-2", "Unknown", "", ""),
		node("worker-3", "True", "", ""),
	)

	got := observed(readingOf(t, f), SignalNodeNotReady)

	require.Len(t, got, 2)
	assert.Equal(t, "Node worker-1", got["/Node//worker-1"].Subject.Label)
	assert.Equal(t, map[string]string{"status": "False", "reason": "KubeletNotReady", "message": "PLEG is not healthy"}, got["/Node//worker-1"].Detail)
	assert.Equal(t, map[string]string{"status": "Unknown"}, got["/Node//worker-2"].Detail)
}

// workload is a short workload's one observation: its signal and detail.
func workload(t *testing.T, objects ...map[string]any) (string, map[string]string) {
	t.Helper()
	r := readingOf(t, newFake(t, objects...))
	var held []monitor.Observation
	for _, o := range r.Observations {
		if o.Signal == SignalReplicasUnavailable || o.Signal == SignalRolloutStalled {
			held = append(held, o)
		}
	}
	if len(held) == 0 {
		return "", nil
	}
	require.Len(t, held, 1)
	return held[0].Signal, held[0].Detail
}

// A Deployment that wants three replicas and holds two, all of its current
// revision, is short and not rolling.
func TestAShortWorkloadOfItsCurrentRevisionIsUnavailable(t *testing.T) {
	signal, detail := workload(t, webDeployment(3, 2)...)

	assert.Equal(t, SignalReplicasUnavailable, signal)
	assert.Equal(t, map[string]string{"ready": "2", "desired": "3"}, detail)
}

// The rolling predicates: pods of an earlier revision remain, or the counts
// describe an earlier spec.
func TestAWorkloadIsRollingWhilePodsOfAnEarlierRevisionRemain(t *testing.T) {
	statefulSet := func(spec, status map[string]any) map[string]any {
		return map[string]any{"apiVersion": "apps/v1", "kind": "StatefulSet", "metadata": meta("sts-db", "db", nil), "spec": spec, "status": status}
	}
	daemonSet := func(spec, status map[string]any) map[string]any {
		return map[string]any{"apiVersion": "apps/v1", "kind": "DaemonSet", "metadata": meta("ds-agent", "agent", nil), "spec": spec, "status": status}
	}
	deployment := webDeployment(3, 1)[0]
	rollingDeployment := maps.Clone(deployment)
	rollingDeployment["status"] = map[string]any{"observedGeneration": int64(1), "replicas": int64(4), "updatedReplicas": int64(2), "readyReplicas": int64(1)}
	unobserved := maps.Clone(deployment)
	unobserved["status"] = map[string]any{"observedGeneration": int64(0), "replicas": int64(3), "updatedReplicas": int64(3), "readyReplicas": int64(1)}

	for name, c := range map[string]struct {
		object map[string]any
		want   string
	}{
		"a Deployment of one revision":          {deployment, SignalReplicasUnavailable},
		"a Deployment with an earlier revision": {rollingDeployment, SignalRolloutStalled},
		"a workload whose generation is unseen": {unobserved, SignalRolloutStalled},
		"a StatefulSet updating": {statefulSet(map[string]any{"replicas": int64(3)},
			map[string]any{"observedGeneration": int64(1), "readyReplicas": int64(2), "updatedReplicas": int64(1), "currentRevision": "a", "updateRevision": "b"}), SignalRolloutStalled},
		"a StatefulSet all updated and not ready": {statefulSet(map[string]any{"replicas": int64(3)},
			map[string]any{"observedGeneration": int64(1), "readyReplicas": int64(0), "updatedReplicas": int64(3), "currentRevision": "a", "updateRevision": "b"}), SignalReplicasUnavailable},
		"a StatefulSet updated as far as its partition": {statefulSet(
			map[string]any{"replicas": int64(3), "updateStrategy": map[string]any{"type": "RollingUpdate", "rollingUpdate": map[string]any{"partition": int64(2)}}},
			map[string]any{"observedGeneration": int64(1), "readyReplicas": int64(2), "updatedReplicas": int64(1), "currentRevision": "a", "updateRevision": "b"}), SignalReplicasUnavailable},
		"an OnDelete StatefulSet": {statefulSet(map[string]any{"replicas": int64(3), "updateStrategy": map[string]any{"type": "OnDelete"}},
			map[string]any{"observedGeneration": int64(1), "readyReplicas": int64(2), "updatedReplicas": int64(1), "currentRevision": "a", "updateRevision": "b"}), SignalReplicasUnavailable},
		"a DaemonSet updating": {daemonSet(map[string]any{},
			map[string]any{"observedGeneration": int64(1), "desiredNumberScheduled": int64(3), "numberReady": int64(2), "currentNumberScheduled": int64(3), "updatedNumberScheduled": int64(1)}), SignalRolloutStalled},
		"a DaemonSet short of scheduled pods": {daemonSet(map[string]any{},
			map[string]any{"observedGeneration": int64(1), "desiredNumberScheduled": int64(3), "numberReady": int64(2), "currentNumberScheduled": int64(2), "updatedNumberScheduled": int64(2)}), SignalReplicasUnavailable},
		"an OnDelete DaemonSet": {daemonSet(map[string]any{"updateStrategy": map[string]any{"type": "OnDelete"}},
			map[string]any{"observedGeneration": int64(1), "desiredNumberScheduled": int64(3), "numberReady": int64(2), "currentNumberScheduled": int64(3), "updatedNumberScheduled": int64(1)}), SignalReplicasUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			signal, _ := workload(t, c.object)
			assert.Equal(t, c.want, signal)
		})
	}
}

// A rolling workload's detail counts its updated pods.
func TestARolloutStalledCountsItsUpdatedPods(t *testing.T) {
	objects := webDeployment(3, 1)
	objects[0]["status"] = map[string]any{"observedGeneration": int64(1), "replicas": int64(4), "updatedReplicas": int64(2), "readyReplicas": int64(1)}

	_, detail := workload(t, objects...)

	assert.Equal(t, map[string]string{"ready": "1", "desired": "3", "updated": "2"}, detail)
}

// replicas_unavailable stays quiet for a Deployment whose pod crash-loops or is
// Pending, which holds its pod signal instead.
func TestAWorkloadWhosePodHoldsAPodSignalIsQuiet(t *testing.T) {
	crashLoop := append(webDeployment(3, 2), pod("uid-1", "web-a", underWeb(), "Running", crashing("app")))
	r := readingOf(t, newFake(t, crashLoop...))
	assert.Empty(t, observed(r, SignalReplicasUnavailable))
	assert.Contains(t, observed(r, SignalCrashLoop), webKey)

	pending := append(webDeployment(3, 2), pod("uid-2", "web-b", underWeb(), "Pending"))
	r = readingOf(t, newFake(t, pending...))
	assert.Empty(t, observed(r, SignalReplicasUnavailable))
	assert.Contains(t, observed(r, SignalPodPending), webKey)
}

// The walk climbs to the last owner the cache holds: a Job whose CronJob is not
// in the cache, as when CronJobs are forbidden, is its pods' subject.
func TestTheSubjectIsTheLastOwnerTheCacheHolds(t *testing.T) {
	job := map[string]any{"apiVersion": "batch/v1", "kind": "Job", "metadata": meta("job-1", "nightly-1", controller("batch/v1", "CronJob", "nightly", "cron-1"))}
	f := newFake(t, job, pod("uid-1", "nightly-1-x", controller("batch/v1", "Job", "nightly-1", "job-1"), "Running", crashing("task")))

	got := observed(readingOf(t, f), SignalCrashLoop)

	assert.Equal(t, []string{"batch/Job/default/nightly-1"}, slices.Collect(maps.Keys(got)))
}

// A Deployment and a StatefulSet of one name in one namespace are two subjects.
func TestTwoKindsOfOneNameAreTwoSubjects(t *testing.T) {
	sts := map[string]any{"apiVersion": "apps/v1", "kind": "StatefulSet", "metadata": meta("sts-web", "web", nil)}
	f := newFake(t, append(webDeployment(1, 0),
		sts,
		pod("uid-1", "web-a", underWeb(), "Running", crashing("app")),
		pod("uid-2", "web-0", controller("apps/v1", "StatefulSet", "web", "sts-web"), "Running", crashing("app")),
	)...)

	got := observed(readingOf(t, f), SignalCrashLoop)

	assert.ElementsMatch(t, []string{webKey, "apps/StatefulSet/default/web"}, slices.Collect(maps.Keys(got)))
}

// A subject name of 10,000 bytes, or one of control characters, is cut to fit
// the monitor's bounds, so its observation passes the check.
func TestALongOrControlNameIsCutToFit(t *testing.T) {
	for name, n := range map[string]string{"long": strings.Repeat("a", 10_000), "control": strings.Repeat("\x01", 10_000)} {
		t.Run(name, func(t *testing.T) {
			f := newFake(t, pod("uid-1", n, nil, "Running", crashing("app")))

			r := readingOf(t, f)

			require.Len(t, r.Observations, 1)
			o := r.Observations[0]
			assert.LessOrEqual(t, len(o.Subject.Label), maxLabelBytes)
			assert.LessOrEqual(t, jsonLen(o.Subject.Attributes["name"]), maxAttributeJSON)
			assert.LessOrEqual(t, jsonLen(o.Detail["pod"]), maxDetailJSON)
		})
	}
}

// Each read of objects by kind searches the index it names by its kind: SQLite
// takes a full scan of a named index too, so the plan is what pins it. The plan
// is of the text without body(), which only the store's own connections hold and
// which plans as the column it reads.
func TestEachKindReadSearchesItsIndex(t *testing.T) {
	f := newFake(t)
	for _, st := range statements {
		plan := strings.Join(f.store.Plan(t, strings.ReplaceAll(st.sql, "body(", "(")), "\n")
		for _, index := range indexesNamed(st.sql) {
			assert.Regexp(t, `SEARCH \S+ USING (COVERING )?INDEX `+index+` \(api_version=\? AND kind=\?`, plan, st.signals[0])
		}
	}
}

// indexesNamed is every index sql names with INDEXED BY.
func indexesNamed(sql string) []string {
	var out []string
	for _, part := range strings.Split(sql, "INDEXED BY ")[1:] {
		out = append(out, strings.Fields(part)[0])
	}
	return out
}
