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

package kubestore

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func obj(m map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: m}
}

// A crashing container's reason beats the phase: "Running" is what the phase says
// while the only container restarts forever.
func TestStatusPodReportsTheContainerReasonOverThePhase(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"spec": map[string]any{"nodeName": "node-a"},
		"status": map[string]any{
			"phase": "Running",
			"containerStatuses": []any{
				map[string]any{
					"ready": false, "restartCount": int64(7),
					"state": map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}},
				},
			},
		},
	})

	got := extractStatus(u)

	assert.Equal(t, "CrashLoopBackOff", got.Summary)
	assert.Equal(t, int64(0), got.Ready.Int64)
	assert.Equal(t, int64(1), got.Total.Int64)
	assert.Equal(t, int64(7), got.Restart.Int64)
	assert.Equal(t, "node-a", got.Host)
}

// spec.replicas, not status.replicas: the observed count lags a scale-up, and a
// desired-3 workload must not read as scaled to zero.
func TestStatusDeploymentCountsAgainstTheDesiredReplicas(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"spec":   map[string]any{"replicas": int64(3)},
		"status": map[string]any{"readyReplicas": int64(1), "replicas": int64(1)},
	})

	got := extractStatus(u)

	assert.Equal(t, "Progressing 1/3", got.Summary)
}

func TestStatusNodeNamesThePressureBehindNotReady(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "v1", "kind": "Node",
		"status": map[string]any{"conditions": []any{
			map[string]any{"type": "Ready", "status": "False"},
			map[string]any{"type": "DiskPressure", "status": "True"},
		}},
	})

	got := extractStatus(u)

	assert.Equal(t, "NotReady (DiskPressure)", got.Summary)
	assert.Equal(t, int64(0), got.Ready.Int64)
}

// A kind with no reader falls back to the conditions convention, which is what most
// CRDs follow.
func TestStatusFallsBackToTheConditionsConvention(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "Application",
		"status": map[string]any{"conditions": []any{
			map[string]any{"type": "Synced", "status": "False", "reason": "OutOfSync"},
		}},
	})

	assert.Equal(t, "Synced (OutOfSync)", extractStatus(u).Summary)
}

// Readers key on api GROUP and Kind, never Kind alone: a CRD named Pod in its own
// group is that user's kind and must reach the fallback rather than a built-in
// reader hunting fields it lacks.
func TestStatusReadersKeyOnTheGroupToo(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "example.com/v1", "kind": "Pod",
		"status": map[string]any{"phase": "Running"},
	})

	got := extractStatus(u)

	assert.Empty(t, got.Summary)
	assert.False(t, got.Total.Valid)
}

// A pod stuck in init reports its real reason there while containerStatuses still looks
// ordinary, so the init reason wins — prefixed, as kubectl prefixes it.
func TestStatusPodPrefixesAnInitContainersReason(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"status": map[string]any{
			"phase": "Pending",
			"containerStatuses": []any{
				map[string]any{"ready": true, "restartCount": int64(1)},
			},
			"initContainerStatuses": []any{
				map[string]any{
					"ready": false, "restartCount": int64(2),
					"state": map[string]any{"waiting": map[string]any{"reason": "ImagePullBackOff"}},
				},
			},
		},
	})

	got := extractStatus(u)

	assert.Equal(t, "Init:ImagePullBackOff", got.Summary)
	assert.Equal(t, int64(1), got.Ready.Int64)
	// Restarts are the sum across both passes: an init container that keeps dying is the
	// churn the number exists to show.
	assert.Equal(t, int64(3), got.Restart.Int64)
}

// A terminated container names its reason, unless it exited normally — "Completed" is a
// finished job, not a fault.
func TestStatusPodTakesATerminatedReasonButNotACleanExit(t *testing.T) {
	t.Parallel()

	terminated := func(reason string) *unstructured.Unstructured {
		return obj(map[string]any{
			"apiVersion": "v1", "kind": "Pod",
			"status": map[string]any{
				"phase": "Running",
				"containerStatuses": []any{
					map[string]any{
						"ready": true,
						"state": map[string]any{"terminated": map[string]any{"reason": reason}},
					},
				},
			},
		})
	}

	assert.Equal(t, "Error", extractStatus(terminated("Error")).Summary)
	assert.Equal(t, "Running", extractStatus(terminated("Completed")).Summary)
}

// The FIRST reason wins, so a pod whose containers fail in sequence keeps naming the one
// that started it rather than flipping to whichever the list happens to end on.
func TestStatusPodKeepsTheFirstContainerReason(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"status": map[string]any{
			"phase": "Running",
			"containerStatuses": []any{
				map[string]any{"state": map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}}},
				map[string]any{"state": map[string]any{"waiting": map[string]any{"reason": "ErrImagePull"}}},
			},
		},
	})

	assert.Equal(t, "CrashLoopBackOff", extractStatus(u).Summary)
}

// A server that sends something other than an object in the list must not take the
// reading down with it — the rest of the containers still count.
func TestStatusPodSkipsAContainerEntryThatIsNotAnObject(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"status": map[string]any{
			"phase":             "Running",
			"containerStatuses": []any{"not-an-object", map[string]any{"ready": true}},
		},
	})

	got := extractStatus(u)

	assert.Equal(t, "Running", got.Summary)
	assert.Equal(t, int64(1), got.Ready.Int64)
	// The bad entry is still one of the containers the server reported.
	assert.Equal(t, int64(2), got.Total.Int64)
}

// The three verdicts a replica controller reads as, including the one that keeps a
// deliberately-zeroed workload from looking broken.
func TestStatusReplicaControllerNamesItsVerdict(t *testing.T) {
	t.Parallel()

	deployment := func(spec map[string]any, ready int64) *unstructured.Unstructured {
		return obj(map[string]any{
			"apiVersion": "apps/v1", "kind": "Deployment",
			"spec":   spec,
			"status": map[string]any{"readyReplicas": ready},
		})
	}

	assert.Equal(t, "ScaledToZero 0/0", extractStatus(deployment(map[string]any{"replicas": int64(0)}, 0)).Summary)
	assert.Equal(t, "Available 2/2", extractStatus(deployment(map[string]any{"replicas": int64(2)}, 2)).Summary)
	// An omitted spec.replicas is 1, the API's own default — not 0, which would read as
	// scaled to zero for every workload that never set the field.
	assert.Equal(t, "Available 1/1", extractStatus(deployment(map[string]any{}, 1)).Summary)
}

// A DaemonSet counts against what the scheduler wants, not a replica count it has none of.
// desired 0 stays Progressing: a DaemonSet matching no node has nothing to be available.
func TestStatusDaemonSetCountsAgainstTheDesiredNodes(t *testing.T) {
	t.Parallel()

	daemonSet := func(desired, ready int64) *unstructured.Unstructured {
		return obj(map[string]any{
			"apiVersion": "apps/v1", "kind": "DaemonSet",
			"status": map[string]any{"desiredNumberScheduled": desired, "numberReady": ready},
		})
	}

	assert.Equal(t, "Available 3/3", extractStatus(daemonSet(3, 3)).Summary)
	assert.Equal(t, "Progressing 1/3", extractStatus(daemonSet(3, 1)).Summary)
	assert.Equal(t, "Progressing 0/0", extractStatus(daemonSet(0, 0)).Summary)
}

// A healthy node reads Ready and counts 1 of 1, so one "not ready" query spans nodes and
// the workload kinds alike.
func TestStatusNodeCountsAReadyNode(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "v1", "kind": "Node",
		"status": map[string]any{"conditions": []any{
			"not-an-object",
			map[string]any{"type": "Ready", "status": "True"},
			map[string]any{"type": "DiskPressure", "status": "False"},
		}},
	})

	got := extractStatus(u)

	assert.Equal(t, "Ready", got.Summary)
	assert.Equal(t, int64(1), got.Ready.Int64)
	assert.Equal(t, int64(1), got.Total.Int64)
}

// A Service's type is what distinguishes it, and an omitted one is ClusterIP — the API's
// default, so the summary matches what kubectl shows.
func TestStatusServiceNamesItsTypeAndAddress(t *testing.T) {
	t.Parallel()

	service := func(spec map[string]any) *unstructured.Unstructured {
		return obj(map[string]any{"apiVersion": "v1", "kind": "Service", "spec": spec})
	}

	assert.Equal(t, "LoadBalancer 10.0.0.1",
		extractStatus(service(map[string]any{"type": "LoadBalancer", "clusterIP": "10.0.0.1"})).Summary)
	assert.Equal(t, "ClusterIP 10.0.0.2",
		extractStatus(service(map[string]any{"clusterIP": "10.0.0.2"})).Summary)
}

// A Namespace's phase is the whole of its status — no count means the columns stay NULL
// rather than storing a zero that reads as "none ready".
func TestStatusNamespaceIsItsPhaseAlone(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "v1", "kind": "Namespace",
		"status": map[string]any{"phase": "Terminating"},
	})

	got := extractStatus(u)

	assert.Equal(t, "Terminating", got.Summary)
	assert.False(t, got.Ready.Valid)
	assert.False(t, got.Total.Valid)
}

// A PVC is ready exactly when it is Bound; anything else is a claim nothing is serving.
func TestStatusPVCIsReadyOnlyWhenBound(t *testing.T) {
	t.Parallel()

	claim := func(phase string) *unstructured.Unstructured {
		return obj(map[string]any{
			"apiVersion": "v1", "kind": "PersistentVolumeClaim",
			"status": map[string]any{"phase": phase},
		})
	}

	bound := extractStatus(claim("Bound"))
	assert.Equal(t, "Bound", bound.Summary)
	assert.Equal(t, int64(1), bound.Ready.Int64)

	pending := extractStatus(claim("Pending"))
	assert.Equal(t, "Pending", pending.Summary)
	assert.Equal(t, int64(0), pending.Ready.Int64)
}

// A Job's verdict is its first True condition; with none it is still running, and the
// succeeded count is the progress worth showing.
func TestStatusJobReadsItsTrueCondition(t *testing.T) {
	t.Parallel()

	job := func(status map[string]any) *unstructured.Unstructured {
		return obj(map[string]any{"apiVersion": "batch/v1", "kind": "Job", "status": status})
	}

	complete := extractStatus(job(map[string]any{
		"succeeded":  int64(1),
		"conditions": []any{map[string]any{"type": "Complete", "status": "True"}},
	}))
	assert.Equal(t, "Complete", complete.Summary)
	assert.Equal(t, int64(1), complete.Ready.Int64)

	failed := extractStatus(job(map[string]any{
		"failed":     int64(3),
		"conditions": []any{map[string]any{"type": "Failed", "status": "True"}},
	}))
	assert.Equal(t, "Failed (3)", failed.Summary)
	assert.Equal(t, int64(0), failed.Ready.Int64)

	// A False condition is not a verdict — it is the API saying the job has not got there.
	running := extractStatus(job(map[string]any{
		"succeeded": int64(2),
		"conditions": []any{
			"not-an-object",
			map[string]any{"type": "Complete", "status": "False"},
			map[string]any{"type": "Suspended", "status": "True"},
		},
	}))
	assert.Equal(t, "Running (2 succeeded)", running.Summary)
}

// A CronJob's reading is what it is doing now plus when it last fired; a job that has
// never run has no last time to name.
func TestStatusCronJobCountsActiveJobsAndNamesTheLastRun(t *testing.T) {
	t.Parallel()

	cronJob := func(status map[string]any) *unstructured.Unstructured {
		return obj(map[string]any{"apiVersion": "batch/v1", "kind": "CronJob", "status": status})
	}

	assert.Equal(t, "2 active, last 2026-08-29T00:00:00Z", extractStatus(cronJob(map[string]any{
		"active":           []any{map[string]any{}, map[string]any{}},
		"lastScheduleTime": "2026-08-29T00:00:00Z",
	})).Summary)
	assert.Equal(t, "0 active", extractStatus(cronJob(map[string]any{})).Summary)
}

// The most-recently-True condition is the CRD's current state, so a resource that became
// Healthy after it became Synced reads Healthy.
func TestStatusConditionsFallbackTakesTheLatestTrueCondition(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "example.com/v1", "kind": "Widget",
		"status": map[string]any{"conditions": []any{
			"not-an-object",
			map[string]any{"type": "Synced", "status": "True", "lastTransitionTime": "2026-08-01T00:00:00Z"},
			map[string]any{"type": "Healthy", "status": "True", "lastTransitionTime": "2026-08-29T00:00:00Z"},
		}},
	})

	assert.Equal(t, "Healthy", extractStatus(u).Summary)
}

// A True condition carrying no lastTransitionTime must still beat the empty string it
// would otherwise be compared against, or it is dropped for a False summary.
func TestStatusConditionsFallbackTakesATrueConditionWithNoTimestamp(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "example.com/v1", "kind": "Widget",
		"status": map[string]any{"conditions": []any{
			map[string]any{"type": "Degraded", "status": "False", "reason": "OutOfSync"},
			map[string]any{"type": "Ready", "status": "True"},
		}},
	})

	assert.Equal(t, "Ready", extractStatus(u).Summary)
}
