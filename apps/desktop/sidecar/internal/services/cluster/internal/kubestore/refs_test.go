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
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
)

// podSpec is a Pod spec making one reference of every kind a Pod spec can make.
func podSpec() map[string]any {
	return map[string]any{
		"volumes": []any{
			map[string]any{"name": "a", "configMap": map[string]any{"name": "cm-vol"}},
			map[string]any{"name": "b", "secret": map[string]any{"secretName": "sec-vol"}},
			map[string]any{"name": "c", "persistentVolumeClaim": map[string]any{"claimName": "data"}},
			map[string]any{"name": "d", "projected": map[string]any{"sources": []any{
				map[string]any{"configMap": map[string]any{"name": "cm-proj"}},
				map[string]any{"secret": map[string]any{"name": "sec-proj"}},
				map[string]any{"serviceAccountToken": map[string]any{"path": "token"}},
			}}},
			map[string]any{"name": "e", "emptyDir": map[string]any{}},
		},
		"imagePullSecrets":   []any{map[string]any{"name": "regcred"}},
		"serviceAccountName": "api",
		"priorityClassName":  "high",
		"initContainers": []any{map[string]any{"name": "init",
			"envFrom": []any{map[string]any{"secretRef": map[string]any{"name": "init-env"}}}}},
		"containers": []any{map[string]any{"name": "api",
			"env": []any{
				map[string]any{"name": "A", "value": "x"},
				map[string]any{"name": "B", "valueFrom": map[string]any{"configMapKeyRef": map[string]any{"name": "cm-env", "key": "level"}}},
				map[string]any{"name": "C", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "sec-env", "key": "token"}}},
				map[string]any{"name": "D", "valueFrom": map[string]any{"fieldRef": map[string]any{"fieldPath": "metadata.name"}}},
			},
			"envFrom": []any{map[string]any{"configMapRef": map[string]any{"name": "cm-from"}}},
		}},
	}
}

// podSpecRows are the rows podSpec makes, found under at, in a Pod spec's namespace prod.
func podSpecRows(at string) []refRow {
	return []refRow{
		{Path: at + ".volumes[0].configMap.name", ToKind: "ConfigMap", ToNamespace: "prod", ToName: "cm-vol"},
		{Path: at + ".volumes[3].projected.sources[0].configMap.name", ToKind: "ConfigMap", ToNamespace: "prod", ToName: "cm-proj"},
		{Path: at + ".volumes[1].secret.secretName", ToKind: "Secret", ToNamespace: "prod", ToName: "sec-vol"},
		{Path: at + ".volumes[3].projected.sources[1].secret.name", ToKind: "Secret", ToNamespace: "prod", ToName: "sec-proj"},
		{Path: at + ".imagePullSecrets[0].name", ToKind: "Secret", ToNamespace: "prod", ToName: "regcred"},
		{Path: at + ".volumes[2].persistentVolumeClaim.claimName", ToKind: "PersistentVolumeClaim", ToNamespace: "prod", ToName: "data"},
		{Path: at + ".containers[0].env[1].valueFrom.configMapKeyRef", ToKind: "ConfigMap", ToNamespace: "prod", ToName: "cm-env", Key: new("level")},
		{Path: at + ".containers[0].envFrom[0].configMapRef.name", ToKind: "ConfigMap", ToNamespace: "prod", ToName: "cm-from"},
		{Path: at + ".containers[0].env[2].valueFrom.secretKeyRef", ToKind: "Secret", ToNamespace: "prod", ToName: "sec-env", Key: new("token")},
		{Path: at + ".initContainers[0].envFrom[0].secretRef.name", ToKind: "Secret", ToNamespace: "prod", ToName: "init-env"},
		{Path: at + ".serviceAccountName", ToKind: "ServiceAccount", ToNamespace: "prod", ToName: "api"},
		{Path: at + ".priorityClassName", ToGroup: "scheduling.k8s.io", ToKind: "PriorityClass", ToName: "high"},
	}
}

// bodyOf is a body of the given apiVersion and kind, named x in namespace prod.
func bodyOf(apiVersion, kind string, fields map[string]any) *unstructured.Unstructured {
	m := map[string]any{
		"apiVersion": apiVersion, "kind": kind,
		"metadata": map[string]any{"uid": "uid-1", "name": "x", "namespace": "prod", "resourceVersion": "1"},
	}
	maps.Copy(m, fields)
	return obj(m)
}

func TestRefsOfReadsEveryRow(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		u    *unstructured.Unstructured
		want []refRow
	}{
		{"Pod", bodyOf("v1", "Pod", map[string]any{"spec": podSpec()}), podSpecRows("$.spec")},
		{"StatefulSet", bodyOf("apps/v1", "StatefulSet", map[string]any{"spec": map[string]any{"serviceName": "db"}}),
			[]refRow{{Path: "$.spec.serviceName", ToKind: "Service", ToNamespace: "prod", ToName: "db"}}},
		{"ServiceAccount", bodyOf("v1", "ServiceAccount", map[string]any{
			"secrets":          []any{map[string]any{"name": "token"}},
			"imagePullSecrets": []any{map[string]any{"name": "regcred"}},
		}), []refRow{
			{Path: "$.secrets[0].name", ToKind: "Secret", ToNamespace: "prod", ToName: "token"},
			{Path: "$.imagePullSecrets[0].name", ToKind: "Secret", ToNamespace: "prod", ToName: "regcred"},
		}},
		{"Ingress", bodyOf("networking.k8s.io/v1", "Ingress", map[string]any{"spec": map[string]any{
			"ingressClassName": "nginx",
			"defaultBackend":   map[string]any{"service": map[string]any{"name": "fallback"}},
			"rules": []any{map[string]any{"http": map[string]any{"paths": []any{
				map[string]any{"backend": map[string]any{"service": map[string]any{"name": "web"}}},
				map[string]any{"backend": map[string]any{"resource": map[string]any{"kind": "Bucket", "name": "static"}}},
			}}}},
			"tls": []any{map[string]any{"secretName": "web-tls"}},
		}}), []refRow{
			{Path: "$.spec.defaultBackend.service.name", ToKind: "Service", ToNamespace: "prod", ToName: "fallback"},
			{Path: "$.spec.rules[0].http.paths[0].backend.service.name", ToKind: "Service", ToNamespace: "prod", ToName: "web"},
			{Path: "$.spec.tls[0].secretName", ToKind: "Secret", ToNamespace: "prod", ToName: "web-tls"},
			{Path: "$.spec.ingressClassName", ToGroup: "networking.k8s.io", ToKind: "IngressClass", ToName: "nginx"},
		}},
		{"PersistentVolumeClaim", bodyOf("v1", "PersistentVolumeClaim", map[string]any{"spec": map[string]any{
			"volumeName": "pv-1", "storageClassName": "fast",
		}}), []refRow{
			{Path: "$.spec.volumeName", ToKind: "PersistentVolume", ToName: "pv-1"},
			{Path: "$.spec.storageClassName", ToGroup: "storage.k8s.io", ToKind: "StorageClass", ToName: "fast"},
		}},
		{"PersistentVolume", obj(map[string]any{
			"apiVersion": "v1", "kind": "PersistentVolume",
			"metadata": map[string]any{"uid": "pv", "name": "pv-1"},
			"spec": map[string]any{
				"claimRef":         map[string]any{"kind": "PersistentVolumeClaim", "namespace": "prod", "name": "data"},
				"storageClassName": "fast",
			},
		}), []refRow{
			{Path: "$.spec.claimRef", ToKind: "PersistentVolumeClaim", ToNamespace: "prod", ToName: "data"},
			{Path: "$.spec.storageClassName", ToGroup: "storage.k8s.io", ToKind: "StorageClass", ToName: "fast"},
		}},
		{"RoleBinding to a Role", bodyOf(rbacGroup+"/v1", "RoleBinding", map[string]any{
			"roleRef": map[string]any{"apiGroup": rbacGroup, "kind": "Role", "name": "reader"},
			"subjects": []any{
				map[string]any{"kind": "ServiceAccount", "name": "api", "namespace": "other"},
				map[string]any{"kind": "ServiceAccount", "name": "worker"},
				map[string]any{"kind": "User", "apiGroup": rbacGroup, "name": "alice"},
				map[string]any{"kind": "Group", "apiGroup": rbacGroup, "name": "devs"},
			},
		}), []refRow{
			{Path: "$.roleRef", ToGroup: rbacGroup, ToKind: "Role", ToNamespace: "prod", ToName: "reader"},
			{Path: "$.subjects[0]", ToKind: "ServiceAccount", ToNamespace: "other", ToName: "api"},
			{Path: "$.subjects[1]", ToKind: "ServiceAccount", ToNamespace: "prod", ToName: "worker"},
		}},
		{"RoleBinding to a ClusterRole", bodyOf(rbacGroup+"/v1", "RoleBinding", map[string]any{
			"roleRef": map[string]any{"apiGroup": rbacGroup, "kind": "ClusterRole", "name": "view"},
		}), []refRow{{Path: "$.roleRef", ToGroup: rbacGroup, ToKind: "ClusterRole", ToName: "view"}}},
		{"RoleBinding to another kind", bodyOf(rbacGroup+"/v1", "RoleBinding", map[string]any{
			"roleRef": map[string]any{"apiGroup": rbacGroup, "kind": "Policy", "name": "p"},
		}), nil},
		{"ClusterRoleBinding", obj(map[string]any{
			"apiVersion": rbacGroup + "/v1", "kind": "ClusterRoleBinding",
			"metadata": map[string]any{"uid": "crb", "name": "admins"},
			"roleRef":  map[string]any{"apiGroup": rbacGroup, "kind": "ClusterRole", "name": "admin"},
			"subjects": []any{
				map[string]any{"kind": "ServiceAccount", "name": "ops", "namespace": "tools"},
				map[string]any{"kind": "User", "apiGroup": rbacGroup, "name": "bob"},
			},
		}), []refRow{
			{Path: "$.roleRef", ToGroup: rbacGroup, ToKind: "ClusterRole", ToName: "admin"},
			{Path: "$.subjects[0]", ToKind: "ServiceAccount", ToNamespace: "tools", ToName: "ops"},
		}},
		{"ClusterRoleBinding to a Role", obj(map[string]any{
			"apiVersion": rbacGroup + "/v1", "kind": "ClusterRoleBinding",
			"metadata": map[string]any{"uid": "crb", "name": "odd"},
			"roleRef":  map[string]any{"apiGroup": rbacGroup, "kind": "Role", "name": "reader"},
		}), nil},
		{"HorizontalPodAutoscaler", bodyOf("autoscaling/v2", "HorizontalPodAutoscaler", map[string]any{"spec": map[string]any{
			"scaleTargetRef": map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "name": "api"},
		}}), []refRow{{Path: "$.spec.scaleTargetRef", ToGroup: "apps", ToKind: "Deployment", ToNamespace: "prod", ToName: "api"}}},
		{"HorizontalPodAutoscaler of a core kind", bodyOf("autoscaling/v2", "HorizontalPodAutoscaler", map[string]any{"spec": map[string]any{
			"scaleTargetRef": map[string]any{"apiVersion": "v1", "kind": "ReplicationController", "name": "rc"},
		}}), []refRow{{Path: "$.spec.scaleTargetRef", ToKind: "ReplicationController", ToNamespace: "prod", ToName: "rc"}}},
		{"a kind of another group spelled the same", bodyOf("example.com/v1", "Ingress", map[string]any{"spec": map[string]any{
			"tls": []any{map[string]any{"secretName": "web-tls"}},
		}}), nil},
	} {
		assert.ElementsMatch(t, tc.want, refsOf(tc.u), tc.name)
	}
}

// Every kind that carries a Pod template refers from it as a Pod does from its spec.
func TestAPodTemplateIsAPodSpec(t *testing.T) {
	t.Parallel()

	template := map[string]any{"template": map[string]any{"spec": podSpec()}}
	for _, kind := range [][2]string{
		{"apps/v1", "Deployment"}, {"apps/v1", "ReplicaSet"}, {"apps/v1", "StatefulSet"},
		{"apps/v1", "DaemonSet"}, {"batch/v1", "Job"},
	} {
		u := bodyOf(kind[0], kind[1], map[string]any{"spec": template})
		assert.ElementsMatch(t, podSpecRows("$.spec.template.spec"), refsOf(u), kind[1])
	}

	cronJob := bodyOf("batch/v1", "CronJob", map[string]any{"spec": map[string]any{"jobTemplate": map[string]any{"spec": template}}})
	assert.ElementsMatch(t, podSpecRows("$.spec.jobTemplate.spec.template.spec"), refsOf(cronJob))
}

// A name that is empty, or not a string, names nothing.
func TestAnEmptyNameIsNoReference(t *testing.T) {
	t.Parallel()

	for _, name := range []any{"", nil, int64(7)} {
		u := bodyOf("v1", "Pod", map[string]any{"spec": map[string]any{
			"serviceAccountName": name,
			"volumes":            []any{map[string]any{"name": "a", "configMap": map[string]any{"name": name}}},
			"containers": []any{map[string]any{"name": "api", "env": []any{
				map[string]any{"name": "A", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": name, "key": "k"}}},
			}}},
		}})
		assert.Empty(t, refsOf(u), "%v", name)
	}

	binding := bodyOf(rbacGroup+"/v1", "RoleBinding", map[string]any{
		"roleRef":  map[string]any{"apiGroup": rbacGroup, "kind": "Role", "name": ""},
		"subjects": []any{map[string]any{"kind": "ServiceAccount", "name": ""}},
	})
	assert.Empty(t, refsOf(binding))
}

// A step of the path that is not an object names nothing below it, and the walk goes on to
// the next item.
func TestAStepThatIsNotAnObjectIsNoReference(t *testing.T) {
	t.Parallel()

	u := bodyOf("v1", "Pod", map[string]any{"spec": map[string]any{
		"volumes": []any{"cm", map[string]any{"name": "b", "configMap": "cm"},
			map[string]any{"name": "c", "configMap": map[string]any{"name": "cm"}}},
	}})

	rows := refsOf(u)
	require.Len(t, rows, 1)
	assert.Equal(t, "$.spec.volumes[2].configMap.name", rows[0].Path)
}

// optional is read where a reference can say it, and nowhere else.
func TestRefsReadOptional(t *testing.T) {
	t.Parallel()

	optional := func(ref map[string]any, set bool) map[string]any {
		out := map[string]any{"optional": set}
		maps.Copy(out, ref)
		return out
	}
	spec := func(set bool) map[string]any {
		return map[string]any{
			"volumes": []any{
				map[string]any{"name": "a", "configMap": optional(map[string]any{"name": "cm"}, set)},
				map[string]any{"name": "b", "secret": optional(map[string]any{"secretName": "sec"}, set)},
				map[string]any{"name": "c", "projected": map[string]any{"sources": []any{
					map[string]any{"configMap": optional(map[string]any{"name": "cm-proj"}, set)},
					map[string]any{"secret": optional(map[string]any{"name": "sec-proj"}, set)},
				}}},
			},
			"containers": []any{map[string]any{"name": "api",
				"env": []any{
					map[string]any{"name": "A", "valueFrom": map[string]any{"configMapKeyRef": optional(map[string]any{"name": "cm-env", "key": "k"}, set)}},
					map[string]any{"name": "B", "valueFrom": map[string]any{"secretKeyRef": optional(map[string]any{"name": "sec-env", "key": "k"}, set)}},
				},
				"envFrom": []any{
					map[string]any{"configMapRef": optional(map[string]any{"name": "cm-from"}, set)},
					map[string]any{"secretRef": optional(map[string]any{"name": "sec-from"}, set)},
				},
			}},
		}
	}
	for _, set := range []bool{true, false} {
		rows := refsOf(bodyOf("v1", "Pod", map[string]any{"spec": spec(set)}))
		require.Len(t, rows, 8)
		for _, r := range rows {
			assert.Equal(t, set, r.Optional == 1, r.Path)
		}
	}

	// A PVC claim cannot say it, whatever its object holds.
	pvc := bodyOf("v1", "Pod", map[string]any{"spec": map[string]any{"volumes": []any{
		map[string]any{"name": "d", "persistentVolumeClaim": map[string]any{"claimName": "data", "optional": true}},
	}}})
	assert.Equal(t, []refRow{{Path: "$.spec.volumes[0].persistentVolumeClaim.claimName",
		ToKind: "PersistentVolumeClaim", ToNamespace: "prod", ToName: "data"}}, refsOf(pvc))
}

// refNames are an object's rows of the refs table as path=name, in path order.
func refNames(t *testing.T, s *Store, uid string) []string {
	t.Helper()
	rows, err := db(t, s).QueryContext(context.Background(),
		`SELECT path || '=' || to_name FROM refs WHERE uid = ? ORDER BY path`, uid)
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

// podMounting is a Pod of the given uid and resourceVersion mounting the named ConfigMaps.
func podMounting(uid, rv string, configMaps ...string) *unstructured.Unstructured {
	volumes := []any{}
	for _, cm := range configMaps {
		volumes = append(volumes, map[string]any{"name": cm, "configMap": map[string]any{"name": cm, "optional": true}})
	}
	return obj(map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"uid": uid, "name": uid, "namespace": "prod", "resourceVersion": rv},
		"spec":     map[string]any{"volumes": volumes},
	})
}

func TestRefsFollowTheirObject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, podMounting("pod", "1", "a", "b")))
	assert.Equal(t, []string{"$.spec.volumes[0].configMap.name=a", "$.spec.volumes[1].configMap.name=b"}, refNames(t, s, "pod"))
	assert.Equal(t, 1, countRows(t, s, `SELECT COUNT(*) FROM refs WHERE uid = 'pod' AND to_name = 'a'
		AND to_group = '' AND to_kind = 'ConfigMap' AND to_namespace = 'prod' AND key IS NULL AND optional = 1`))

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Modified, podMounting("pod", "2", "b")))
	assert.Equal(t, []string{"$.spec.volumes[0].configMap.name=b"}, refNames(t, s, "pod"))

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Deleted, podMounting("pod", "3", "b")))
	assert.Empty(t, refNames(t, s, "pod"))

	for _, uid := range []string{"gone", "kept"} {
		require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, podMounting(uid, "1", "a")))
	}
	session := beginReplace(t, s, podKind)
	require.NoError(t, session.WritePage(ctx, []*unstructured.Unstructured{podMounting("kept", "1", "a")}))
	_, err := session.Commit(ctx, "100")
	require.NoError(t, err)
	assert.Empty(t, refNames(t, s, "gone"))
	assert.NotEmpty(t, refNames(t, s, "kept"))

	require.NoError(t, s.ClearKind(ctx, podKind))
	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM refs`))
}
