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
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
)

// resources is a container's resources block.
func resources(cpuReq, memReq, cpuLim, memLim string) map[string]any {
	return map[string]any{
		"requests": map[string]any{"cpu": cpuReq, "memory": memReq},
		"limits":   map[string]any{"cpu": cpuLim, "memory": memLim},
	}
}

// podOf builds a v1 Pod with the given spec and status.
func podOf(spec, status map[string]any) *unstructured.Unstructured {
	return obj(map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"uid": "uid-1", "name": "api-0", "namespace": "prod", "resourceVersion": "1"},
		"spec":     spec, "status": status,
	})
}

func TestContainersReadSpecAndStatus(t *testing.T) {
	t.Parallel()

	u := podOf(map[string]any{
		"initContainers": []any{
			map[string]any{"name": "migrate", "image": "api:1", "resources": resources("1", "1Gi", "2", "2Gi")},
			map[string]any{"name": "proxy", "image": "envoy:1", "restartPolicy": "Always"},
		},
		"containers": []any{
			map[string]any{"name": "api", "image": "api:1", "resources": resources("250m", "512Mi", "junk", "1e-999999999")},
			map[string]any{"name": "sidecar", "image": "log:1"},
		},
		"ephemeralContainers": []any{map[string]any{"name": "debug", "image": "busybox"}},
	}, map[string]any{
		// Out of spec order, and nothing yet for the second container.
		"initContainerStatuses": []any{
			map[string]any{"name": "proxy", "ready": true, "restartCount": int64(0), "imageID": "envoy@sha256:b",
				"containerID": "containerd://p1", "state": map[string]any{"running": map[string]any{}}},
			map[string]any{"name": "migrate", "ready": false, "restartCount": int64(0), "imageID": "api@sha256:a",
				"containerID": "containerd://m1",
				"state": map[string]any{"terminated": map[string]any{
					"reason": "Completed", "exitCode": int64(0), "containerID": "containerd://m1",
				}}},
		},
		"containerStatuses": []any{
			// A waiting state names no instance, whatever the status's own id says.
			map[string]any{"name": "api", "ready": false, "restartCount": int64(4), "imageID": "api@sha256:a",
				"containerID": "containerd://a4",
				"state":       map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}},
				"lastState":   map[string]any{"terminated": map[string]any{"reason": "OOMKilled", "exitCode": int64(137), "containerID": "containerd://a4"}}},
		},
		"ephemeralContainerStatuses": []any{map[string]any{"name": "debug", "ready": false}},
	})

	assert.Equal(t, []containerRow{
		{Init: 1, Position: 0, Name: "migrate", Image: new("api:1"), ImageID: new("api@sha256:a"), ContainerID: new("containerd://m1"),
			Ready: new(0), Restarts: new(int64(0)), State: new("terminated"), Reason: new("Completed"), ExitCode: new(int64(0)),
			CPURequest: new(1.0), CPULimit: new(2.0), MemoryRequest: new(float64(1 << 30)), MemoryLimit: new(float64(2 << 30))},
		{Init: 1, Position: 1, Name: "proxy", Sidecar: 1, Image: new("envoy:1"), ImageID: new("envoy@sha256:b"), ContainerID: new("containerd://p1"),
			Ready: new(1), Restarts: new(int64(0)), State: new("running")},
		{Init: 0, Position: 0, Name: "api", Image: new("api:1"), ImageID: new("api@sha256:a"),
			Ready: new(0), Restarts: new(int64(4)), State: new("waiting"), Reason: new("CrashLoopBackOff"),
			LastReason: new("OOMKilled"), LastExitCode: new(int64(137)), LastContainerID: new("containerd://a4"),
			CPURequest: new(0.25), MemoryRequest: new(float64(512 << 20))},
		{Init: 0, Position: 1, Name: "sidecar", Image: new("log:1")},
	}, containersOf(u))
}

// A terminated state with no id of its own names none, whatever the status's own
// id says: the kubelet then serves the last instance's log.
func TestATerminatedStateNamesItsOwnInstance(t *testing.T) {
	u := podOf(map[string]any{
		"containers": []any{map[string]any{"name": "api", "image": "api:1"}},
	}, map[string]any{
		"containerStatuses": []any{map[string]any{"name": "api", "restartCount": int64(1), "containerID": "containerd://a2",
			"state":     map[string]any{"terminated": map[string]any{"exitCode": int64(137)}},
			"lastState": map[string]any{"terminated": map[string]any{"exitCode": int64(1), "containerID": "containerd://a1"}}}},
	})

	rows := containersOf(u)
	require.Len(t, rows, 1)
	assert.Nil(t, rows[0].ContainerID)
	assert.Equal(t, new("containerd://a1"), rows[0].LastContainerID)
}

// A status naming no state names no instance, whatever the status's own id says.
func TestAStatusWithNoStateNamesNoInstance(t *testing.T) {
	t.Parallel()

	u := podOf(map[string]any{
		"containers": []any{map[string]any{"name": "api", "image": "api:1"}},
	}, map[string]any{
		"containerStatuses": []any{map[string]any{"name": "api", "containerID": "containerd://a1"}},
	})

	rows := containersOf(u)
	require.Len(t, rows, 1)
	assert.Nil(t, rows[0].State)
	assert.Nil(t, rows[0].ContainerID)
}

// An entry that is not an object says nothing, in either list: the container is skipped,
// and a container whose status is not one reads as having none.
func TestContainersSkipWhatIsNotAnObject(t *testing.T) {
	t.Parallel()

	u := podOf(map[string]any{
		"containers": []any{"api", map[string]any{"name": "log", "image": "log:1"}},
	}, map[string]any{
		"containerStatuses": []any{"log", map[string]any{"ready": true}},
	})

	assert.Equal(t, []containerRow{{Init: 0, Position: 1, Name: "log", Image: new("log:1")}}, containersOf(u))
}

// Only a core Pod has containers: another kind, or a Pod of another group, has none.
func TestContainersOfANonPodIsNone(t *testing.T) {
	t.Parallel()

	spec := map[string]any{"containers": []any{map[string]any{"name": "api", "image": "api:1"}}}
	for _, u := range []*unstructured.Unstructured{
		obj(map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "spec": spec}),
		obj(map[string]any{"apiVersion": "example.com/v1", "kind": "Pod", "spec": spec}),
	} {
		assert.Empty(t, containersOf(u), u.GetAPIVersion())
	}
}

// containerNames is a Pod's container rows as init/position:name, in key order.
func containerNames(t *testing.T, s *Store, uid string) []string {
	t.Helper()
	rows, err := db(t, s).QueryContext(context.Background(),
		`SELECT init || '/' || position || ':' || name FROM containers WHERE pod_uid = ? ORDER BY init DESC, position`, uid)
	require.NoError(t, err)
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var n string
		require.NoError(t, rows.Scan(&n))
		names = append(names, n)
	}
	require.NoError(t, rows.Err())
	return names
}

// podWithContainers is a Pod of the given uid and resourceVersion running the named
// containers, each with a request.
func podWithContainers(uid, rv string, names ...string) *unstructured.Unstructured {
	containers := []any{}
	for _, n := range names {
		containers = append(containers, map[string]any{"name": n, "image": n + ":1", "resources": resources("100m", "64Mi", "1", "1Gi")})
	}
	return obj(map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"uid": uid, "name": uid, "namespace": "prod", "resourceVersion": rv},
		"spec":     map[string]any{"initContainers": []any{map[string]any{"name": "init", "image": "init:1"}}, "containers": containers},
	})
}

func TestContainersFollowTheirPod(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, podWithContainers("pod", "1", "api", "log")))
	assert.Equal(t, []string{"1/0:init", "0/0:api", "0/1:log"}, containerNames(t, s, "pod"))
	assert.Equal(t, 1, countRows(t, s, `SELECT COUNT(*) FROM containers WHERE pod_uid = 'pod' AND name = 'api'
		AND image = 'api:1' AND cpu_request = 0.1 AND memory_limit = 1073741824 AND sidecar = 0`))

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Modified, podWithContainers("pod", "2", "api")))
	assert.Equal(t, []string{"1/0:init", "0/0:api"}, containerNames(t, s, "pod"))

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Deleted, podWithContainers("pod", "3", "api")))
	assert.Empty(t, containerNames(t, s, "pod"))
}

// Every way a Pod leaves the store takes its containers: a relist that no longer lists it,
// and its kind's clear.
func TestContainersLeaveWithTheirPod(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	for _, uid := range []string{"gone", "kept"} {
		require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, podWithContainers(uid, "1", "api")))
	}

	session := beginReplace(t, s, podKind)
	require.NoError(t, session.WritePage(ctx, []*unstructured.Unstructured{podWithContainers("kept", "1", "api")}))
	_, err := session.Commit(ctx, "100")
	require.NoError(t, err)
	assert.Empty(t, containerNames(t, s, "gone"))
	assert.NotEmpty(t, containerNames(t, s, "kept"))

	require.NoError(t, s.ClearKind(ctx, podKind))
	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM containers`))
}

// Only a core Pod writes rows, whatever the worker that wrote it calls its kind.
func TestOnlyAPodWritesContainers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	spec := map[string]any{"containers": []any{map[string]any{"name": "api", "image": "api:1"}}}
	other := Kind{APIVersion: "example.com/v1", Kind: "Pod", Resource: "pods"}
	require.NoError(t, s.ApplyChange(ctx, other, watch.Added, obj(map[string]any{
		"apiVersion": "example.com/v1", "kind": "Pod", "spec": spec,
		"metadata": map[string]any{"uid": "crd", "name": "x", "resourceVersion": "1"},
	})))
	require.NoError(t, s.ApplyChange(ctx, deploymentKind, watch.Added, obj(map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment", "spec": map[string]any{"template": map[string]any{"spec": spec}},
		"metadata": map[string]any{"uid": "dep", "name": "x", "resourceVersion": "1"},
	})))

	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM containers`))
}

// templatePod is a Pod as a Deployment's is once managedFields are dropped, about 6 KB:
// three containers and an init container, each with an image, env, mounts, probes and
// resources, and a status for all four.
func templatePod(apiVersion, uid string) *unstructured.Unstructured {
	container := func(name string) map[string]any {
		env := []any{}
		for _, k := range []string{"LOG_LEVEL", "PORT", "REGION", "FEATURE_FLAGS", "OTEL_ENDPOINT", "DB_HOST"} {
			env = append(env, map[string]any{"name": k, "value": "value-of-" + strings.ToLower(k)})
		}
		mounts := []any{}
		for _, m := range []string{"config", "certs", "cache", "kube-api-access-x7k2p"} {
			mounts = append(mounts, map[string]any{"name": m, "mountPath": "/var/run/" + m, "readOnly": true})
		}
		probe := map[string]any{"httpGet": map[string]any{"path": "/healthz", "port": int64(8080), "scheme": "HTTP"},
			"initialDelaySeconds": int64(5), "periodSeconds": int64(10), "timeoutSeconds": int64(1),
			"successThreshold": int64(1), "failureThreshold": int64(3)}
		return map[string]any{
			"name": name, "image": "registry.example.com/team/" + name + ":v1.42.7", "imagePullPolicy": "IfNotPresent",
			"env": env, "volumeMounts": mounts, "livenessProbe": probe, "readinessProbe": probe,
			"ports":                    []any{map[string]any{"containerPort": int64(8080), "name": "http", "protocol": "TCP"}},
			"resources":                resources("250m", "256Mi", "1", "512Mi"),
			"terminationMessagePath":   "/dev/termination-log",
			"terminationMessagePolicy": "File",
		}
	}
	status := func(name string) map[string]any {
		return map[string]any{
			"name": name, "ready": true, "restartCount": int64(0), "started": true,
			"image":       "registry.example.com/team/" + name + ":v1.42.7",
			"imageID":     "registry.example.com/team/" + name + "@sha256:9f2c1e4b7a6d5c3b2a1f0e9d8c7b6a5f4e3d2c1b0a9f8e7d6c5b4a3f2e1d0c9b",
			"containerID": "containerd://4b7a6d5c3b2a1f0e9d8c7b6a5f4e3d2c1b0a9f8e7d6c5b4a3f2e1d0c9b9f2c1e",
			"state":       map[string]any{"running": map[string]any{"startedAt": "2026-09-27T10:00:00Z"}},
		}
	}
	return obj(map[string]any{
		"apiVersion": apiVersion, "kind": "Pod",
		"metadata": map[string]any{
			"uid": uid, "name": "api-7d9f8c6b5-" + uid, "namespace": "prod", "resourceVersion": "1",
			"creationTimestamp": "2026-09-27T10:00:00Z", "generateName": "api-7d9f8c6b5-",
			"labels": map[string]any{"app": "api", "pod-template-hash": "7d9f8c6b5", "app.kubernetes.io/name": "api"},
			"ownerReferences": []any{map[string]any{"apiVersion": "apps/v1", "kind": "ReplicaSet",
				"name": "api-7d9f8c6b5", "uid": "rs-" + uid, "controller": true, "blockOwnerDeletion": true}},
		},
		"spec": map[string]any{
			"nodeName": "node-1", "serviceAccountName": "api", "restartPolicy": "Always",
			"initContainers": []any{container("migrate")},
			"containers":     []any{container("api"), container("worker"), container("proxy")},
			"volumes": []any{
				map[string]any{"name": "config", "configMap": map[string]any{"name": "api-config"}},
				map[string]any{"name": "certs", "secret": map[string]any{"secretName": "api-certs"}},
				map[string]any{"name": "cache", "emptyDir": map[string]any{}},
			},
		},
		"status": map[string]any{
			"phase": "Running", "podIP": "10.0.3.17", "hostIP": "10.0.0.4", "qosClass": "Burstable",
			"startTime":             "2026-09-27T10:00:00Z",
			"initContainerStatuses": []any{status("migrate")},
			"containerStatuses":     []any{status("api"), status("worker"), status("proxy")},
		},
	})
}

// The containers write lands on every Pod write, relists included. Without the table, the
// same page goes out as a Pod of another group, which takes the same write less the rows.
func BenchmarkWriteAPodPage(b *testing.B) {
	for _, bc := range []struct{ name, apiVersion string }{
		{"with containers", "v1"},
		{"without", "example.com/v1"},
	} {
		b.Run(bc.name, func(b *testing.B) {
			m := NewManager(b.TempDir(), Retention{})
			defer m.Close()
			s, err := m.OpenOrCreate(1)
			require.NoError(b, err)
			defer s.Release()
			k := Kind{APIVersion: bc.apiVersion, Kind: "Pod", Resource: "pods"}
			page := make([]*unstructured.Unstructured, 500)
			for i := range page {
				page[i] = templatePod(bc.apiVersion, fmt.Sprintf("pod-%03d", i))
			}
			ctx := context.Background()
			b.ResetTimer()
			for b.Loop() {
				session, err := s.BeginReplace(k)
				require.NoError(b, err)
				require.NoError(b, session.WritePage(ctx, page))
				_, err = session.Commit(ctx, "1")
				require.NoError(b, err)
			}
		})
	}
}
