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
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// refRow is one row of the refs table: one by-name reference an object makes.
// The JSON names are the columns, since the rows bind as one JSON argument.
type refRow struct {
	// Path is where the reference was found, indices included.
	Path        string `json:"path"`
	ToGroup     string `json:"to_group"`
	ToKind      string `json:"to_kind"`
	ToNamespace string `json:"to_namespace"`
	ToName      string `json:"to_name"`
	// Key is a key reference's key.
	Key *string `json:"key"`
	// Optional is 1 for a reference that sets optional: true, which a Pod starts without.
	Optional int `json:"optional"`
}

// refPath is one reference a kind can make.
type refPath struct {
	// at walks to the object holding the reference: fields joined by dots, where a field
	// ending in [] walks each element of its array.
	at string
	// name is the field of that object holding the target's name.
	name string
	// whole says the object is the reference, so a row's path is at, not at.name.
	whole bool
	// key says the object names a key of the target too.
	key bool
	// optional says the object can mark the reference optional.
	optional bool
	// namespace is the field of that object naming the target's namespace, where it can
	// name one; the referring object's namespace is the target's otherwise.
	namespace   string
	group, kind string
	// target reads the target's group and kind off the object, for a reference that names
	// them itself; false is no reference.
	target func(m map[string]any) (group, kind string, ok bool)
}

// containerRefs are the references a container makes, under at.
func containerRefs(at string) []refPath {
	return []refPath{
		{at: at + ".env[].valueFrom.configMapKeyRef", name: "name", whole: true, key: true, optional: true, kind: "ConfigMap"},
		{at: at + ".envFrom[].configMapRef", name: "name", optional: true, kind: "ConfigMap"},
		{at: at + ".env[].valueFrom.secretKeyRef", name: "name", whole: true, key: true, optional: true, kind: "Secret"},
		{at: at + ".envFrom[].secretRef", name: "name", optional: true, kind: "Secret"},
	}
}

// podSpecRefs are the references a Pod spec makes, relative to the spec.
var podSpecRefs = slices.Concat([]refPath{
	{at: "volumes[].configMap", name: "name", optional: true, kind: "ConfigMap"},
	{at: "volumes[].projected.sources[].configMap", name: "name", optional: true, kind: "ConfigMap"},
	{at: "volumes[].secret", name: "secretName", optional: true, kind: "Secret"},
	{at: "volumes[].projected.sources[].secret", name: "name", optional: true, kind: "Secret"},
	{at: "imagePullSecrets[]", name: "name", kind: "Secret"},
	{at: "volumes[].persistentVolumeClaim", name: "claimName", kind: "PersistentVolumeClaim"},
}, containerRefs("containers[]"), containerRefs("initContainers[]"), []refPath{
	{name: "serviceAccountName", kind: "ServiceAccount"},
	{name: "priorityClassName", group: "scheduling.k8s.io", kind: "PriorityClass"},
})

// podSpecAt is where each kind that carries a Pod spec keeps it, keyed by api group and kind.
var podSpecAt = map[[2]string]string{
	{"", "Pod"}:             "spec",
	{"apps", "Deployment"}:  "spec.template.spec",
	{"apps", "ReplicaSet"}:  "spec.template.spec",
	{"apps", "StatefulSet"}: "spec.template.spec",
	{"apps", "DaemonSet"}:   "spec.template.spec",
	{"batch", "Job"}:        "spec.template.spec",
	{"batch", "CronJob"}:    "spec.jobTemplate.spec.template.spec",
}

const rbacGroup = "rbac.authorization.k8s.io"

// refPaths are the references each kind makes outside a Pod spec, keyed by api group and kind.
var refPaths = map[[2]string][]refPath{
	{"apps", "StatefulSet"}: {{at: "spec", name: "serviceName", kind: "Service"}},
	{"", "ServiceAccount"}: {
		{at: "secrets[]", name: "name", kind: "Secret"},
		{at: "imagePullSecrets[]", name: "name", kind: "Secret"},
	},
	{"networking.k8s.io", "Ingress"}: {
		{at: "spec.defaultBackend.service", name: "name", kind: "Service"},
		{at: "spec.rules[].http.paths[].backend.service", name: "name", kind: "Service"},
		{at: "spec.tls[]", name: "secretName", kind: "Secret"},
		{at: "spec", name: "ingressClassName", group: "networking.k8s.io", kind: "IngressClass"},
	},
	{"", "PersistentVolumeClaim"}: {
		{at: "spec", name: "volumeName", kind: "PersistentVolume"},
		{at: "spec", name: "storageClassName", group: "storage.k8s.io", kind: "StorageClass"},
	},
	{"", "PersistentVolume"}: {
		{at: "spec.claimRef", name: "name", whole: true, namespace: "namespace", kind: "PersistentVolumeClaim"},
		{at: "spec", name: "storageClassName", group: "storage.k8s.io", kind: "StorageClass"},
	},
	{rbacGroup, "RoleBinding"}: {
		{at: "roleRef", name: "name", whole: true, target: roleRef("Role", "ClusterRole")},
		{at: "subjects[]", name: "name", whole: true, namespace: "namespace", target: serviceAccountSubject},
	},
	{rbacGroup, "ClusterRoleBinding"}: {
		{at: "roleRef", name: "name", whole: true, target: roleRef("ClusterRole")},
		{at: "subjects[]", name: "name", whole: true, namespace: "namespace", target: serviceAccountSubject},
	},
	{"autoscaling", "HorizontalPodAutoscaler"}: {
		{at: "spec.scaleTargetRef", name: "name", whole: true, target: scaleTarget},
	},
}

// clusterScoped are the targets with no namespace, keyed by api group and kind.
var clusterScoped = map[[2]string]bool{
	{"scheduling.k8s.io", "PriorityClass"}: true,
	{"", "PersistentVolume"}:               true,
	{"storage.k8s.io", "StorageClass"}:     true,
	{"networking.k8s.io", "IngressClass"}:  true,
	{rbacGroup, "ClusterRole"}:             true,
}

// roleRef reads a binding's roleRef, a reference only when its kind is one of kinds.
func roleRef(kinds ...string) func(map[string]any) (string, string, bool) {
	return func(m map[string]any) (string, string, bool) {
		kind, _ := m["kind"].(string)
		return rbacGroup, kind, slices.Contains(kinds, kind)
	}
}

// serviceAccountSubject reads a binding's subject, a reference only when it names a
// ServiceAccount: users and groups are not objects.
func serviceAccountSubject(m map[string]any) (string, string, bool) {
	kind, _ := m["kind"].(string)
	return "", kind, kind == "ServiceAccount"
}

// scaleTarget reads an HPA's scaleTargetRef, which names its target's apiVersion and kind.
func scaleTarget(m map[string]any) (string, string, bool) {
	apiVersion, _ := m["apiVersion"].(string)
	kind, _ := m["kind"].(string)
	group, _, found := strings.Cut(apiVersion, "/")
	if !found {
		group = ""
	}
	return group, kind, kind != ""
}

// refersByKind says u's kind, by its body's own group and kind, can make references.
func refersByKind(u *unstructured.Unstructured) bool {
	gk := groupKindOf(u)
	_, spec := podSpecAt[gk]
	_, other := refPaths[gk]
	return spec || other
}

// refsOf reads the references one object makes, by its body's own group and kind.
func refsOf(u *unstructured.Unstructured) []refRow {
	gk := groupKindOf(u)
	var rows []refRow
	if at, ok := podSpecAt[gk]; ok {
		for _, p := range podSpecRefs {
			p.at = joinPath(at, p.at)
			rows = append(rows, p.rows(u)...)
		}
	}
	for _, p := range refPaths[gk] {
		rows = append(rows, p.rows(u)...)
	}
	return rows
}

// writeRefs rewrites the object's rows of the refs table.
func writeRefs(ctx context.Context, st stmts, row objectRow) error {
	if _, err := st.Exec(ctx, stmtDeleteRefsOfObject, row.UID); err != nil {
		return err
	}
	// The guard the labels' is: no references marshals to `null`.
	if len(row.Refs) == 0 {
		return nil
	}
	refsJSON, err := json.Marshal(row.Refs)
	if err != nil { // coverage:ignore plain values always marshal
		return err
	}
	_, err = st.Exec(ctx, stmtInsertRefs, row.UID, string(refsJSON))
	return err
}

// rows reads p's references out of u.
func (p refPath) rows(u *unstructured.Unstructured) []refRow {
	var rows []refRow
	walk(u.Object, "$", strings.Split(p.at, "."), func(path string, m map[string]any) {
		name, _ := m[p.name].(string)
		if name == "" {
			return
		}
		row := refRow{Path: path + "." + p.name, ToGroup: p.group, ToKind: p.kind, ToName: name}
		if p.target != nil {
			var ok bool
			if row.ToGroup, row.ToKind, ok = p.target(m); !ok {
				return
			}
		}
		if p.whole {
			row.Path = path
		}
		if p.key {
			if key, ok := m["key"].(string); ok {
				row.Key = &key
			}
		}
		if set, _ := m["optional"].(bool); p.optional && set {
			row.Optional = 1
		}
		if !clusterScoped[[2]string{row.ToGroup, row.ToKind}] {
			row.ToNamespace = u.GetNamespace()
			if ns, _ := m[p.namespace].(string); p.namespace != "" && ns != "" {
				row.ToNamespace = ns
			}
		}
		rows = append(rows, row)
	})
	return rows
}

// walk calls visit with each object fields reaches from v, and the path it was reached by.
func walk(v any, path string, fields []string, visit func(string, map[string]any)) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	if len(fields) == 0 {
		visit(path, m)
		return
	}
	field, each := strings.CutSuffix(fields[0], "[]")
	child, ok := m[field]
	if !ok {
		return
	}
	path += "." + field
	if !each {
		walk(child, path, fields[1:], visit)
		return
	}
	list, _ := child.([]any)
	for i, item := range list {
		walk(item, fmt.Sprintf("%s[%d]", path, i), fields[1:], visit)
	}
}

// joinPath joins two walk paths, either of which may be empty.
func joinPath(a, b string) string {
	if a == "" || b == "" {
		return a + b
	}
	return a + "." + b
}

// groupKindOf is the body's own api group, empty for the core group, and kind.
func groupKindOf(u *unstructured.Unstructured) [2]string {
	group, _, found := strings.Cut(u.GetAPIVersion(), "/")
	if !found {
		group = ""
	}
	return [2]string{group, u.GetKind()}
}
