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

package kubeproxy

import (
	"cmp"
	"encoding/json"
	"mime"
	"net/http"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

// AlwaysAsks is the destructive list in words, as Settings shows it under
// Always asks: one line per item destructive decides on.
var AlwaysAsks = []string{
	"Deleting a namespace, node, persistent volume, persistent volume claim or custom resource definition, or finalizing a namespace",
	"Deleting a whole collection in one request (kubectl deletes one object at a time, so its --all and -l are decided object by object)",
	"Adding an ephemeral container to a pod",
	"Scaling to zero, or any change to a scale or to a deployment's, stateful set's, replica set's or replication controller's replicas that is not a plain positive count",
	"Any change to a Namespace object, a create included",
	"Any change Kubetail does not know, such as a subresource of a built-in kind it does not list",
}

// AccessControl is the access control list in words, as Settings shows it: each
// asks unless a rule inside a namespace allows it, and always asks across a
// cluster.
var AccessControl = []string{
	"Any change to roles, cluster roles or their bindings",
	"Any change to admission webhooks or admission policies",
	"Approving a certificate signing request",
}

// verbs is the Kubernetes verb of each method on a named resource.
var verbs = map[string]string{
	http.MethodGet: "get", http.MethodPost: "create", http.MethodPut: "update",
	http.MethodPatch: "patch", http.MethodDelete: "delete",
}

// classify is what a request the policy passed does, for a decision: its
// level, scope and target, read off the path, and for a replica write or a
// Namespace's create off the body, which checkBody has passed. cluster and
// context are the grant's. The scope is the cluster, then the namespace; a
// Namespace object sits at cluster scope.
func classify(r *http.Request, p apiPath, body []byte, cluster apimeta.ClusterID, context string) permissions.Action {
	act := permissions.Action{
		Target: permissions.Kubernetes, AccountName: context,
		Service: p.group, Operation: verbs[r.Method], Resource: p.resource, Name: p.name,
		DryRun: isDryRun(r),
	}
	if cluster != "" {
		act.Scope = string(cluster)
		if p.namespace != "" {
			act.Scope += "/" + p.namespace
		}
	}
	// A request on Secrets in no namespace reaches every namespace's.
	act.Spans = p.namespace == "" && p.onSecrets()
	if p.subresource != "" {
		act.Resource += "/" + p.subresource
	}
	if isNamespace(p) && act.Name == "" && r.Method == http.MethodPost {
		act.Name = createdName(r.Header.Get("Content-Type"), body)
	}
	if r.Method == http.MethodDelete && p.name == "" {
		act.Operation = "deletecollection"
	}
	// A read's verb is the API server's, which a data read's rule can name.
	if r.Method == http.MethodGet && p.kind == resourcePath {
		switch {
		case isWatch(p, r.URL.Query()):
			act.Operation = "watch"
		case p.name == "":
			act.Operation = "list"
		}
	}
	group := cmp.Or(p.group, "core")
	write := isWrite(r, p)
	switch {
	case write && act.DryRun && honorsDryRun[group+"/"+p.version]:
		act.AccessLevel = permissions.Read
	case !write && r.Method == http.MethodGet && p.onSecrets() && !metadataOnly(r):
		act.AccessLevel = permissions.DataRead
	case !write:
		act.AccessLevel = permissions.Read
	case destructive(r, p, act, body):
		act.AccessLevel = permissions.Destructive
	case writtenHard[group][p.resource],
		group == "certificates.k8s.io" && p.resource == "certificatesigningrequests" && p.subresource == "approval":
		act.AccessLevel = permissions.AccessControl
		act.Confined = p.namespace != ""
	default:
		act.AccessLevel = permissions.Write
	}
	// A write no table names asks each time, and so does any change to a
	// Namespace object: no rule names one object, and a rule over every
	// Namespace would reach every namespace's.
	if act.AccessLevel != permissions.Read && act.AccessLevel != permissions.DataRead {
		act.Once = !named(p) || isNamespace(p)
	}
	act.Summary = summary(p, act)
	return act
}

// isNamespace is whether p is a core Namespace object or its subresource.
func isNamespace(p apiPath) bool {
	return p.kind == resourcePath && p.group == "" && p.resource == "namespaces"
}

// named is whether a write's resource is one a table names: in a group
// version the API server serves itself, a row of builtin; in any other
// group, a resource, its status or its scale. A built-in group's other
// version may be an aggregated API, so it is named only by its own rows.
func named(p apiPath) bool {
	group := cmp.Or(p.group, "core")
	resource := p.resource
	if p.subresource != "" {
		resource += "/" + p.subresource
	}
	if builtinGroups[group] {
		return builtin[group+"/"+p.version][resource]
	}
	return p.subresource == "" || p.subresource == "status" || p.subresource == "scale"
}

// honorsDryRun are the group versions the API server serves itself, which
// honor dryRun. The aggregator routes by group and version, so any other —
// even a new version of a built-in group — may be an aggregated API, whose
// server can ignore dryRun and apply the write. Only stable versions are
// listed: a missing one asks, which is safe.
var honorsDryRun = map[string]bool{
	"core/v1": true, "apps/v1": true, "batch/v1": true, "autoscaling/v1": true, "autoscaling/v2": true,
	"policy/v1": true, "networking.k8s.io/v1": true, "storage.k8s.io/v1": true,
	"rbac.authorization.k8s.io/v1": true, "apiextensions.k8s.io/v1": true, "apiregistration.k8s.io/v1": true,
	"certificates.k8s.io/v1": true, "admissionregistration.k8s.io/v1": true, "coordination.k8s.io/v1": true,
	"discovery.k8s.io/v1": true, "scheduling.k8s.io/v1": true, "node.k8s.io/v1": true,
	"events.k8s.io/v1": true, "flowcontrol.apiserver.k8s.io/v1": true, "resource.k8s.io/v1": true,
	"authentication.k8s.io/v1": true, "authorization.k8s.io/v1": true,
}

// builtin are the resources and subresources the API server serves itself,
// by group version, for each group version in honorsDryRun. A write to a
// built-in group that these rows lack is named by no table, so it asks each
// time. Adding a row is a security change.
var builtin = map[string]map[string]bool{
	"core/v1": set(
		"bindings", "componentstatuses", "configmaps", "endpoints", "events", "limitranges",
		"namespaces", "namespaces/status", "namespaces/finalize",
		"nodes", "nodes/status", "persistentvolumes", "persistentvolumes/status",
		"persistentvolumeclaims", "persistentvolumeclaims/status",
		"pods", "pods/status", "pods/binding", "pods/eviction", "pods/ephemeralcontainers", "pods/resize",
		"podtemplates", "replicationcontrollers", "replicationcontrollers/status", "replicationcontrollers/scale",
		"resourcequotas", "resourcequotas/status", "secrets", "serviceaccounts", "services", "services/status",
	),
	"apps/v1": set(
		"controllerrevisions", "daemonsets", "daemonsets/status",
		"deployments", "deployments/status", "deployments/scale",
		"replicasets", "replicasets/status", "replicasets/scale",
		"statefulsets", "statefulsets/status", "statefulsets/scale",
	),
	"batch/v1":                        set("cronjobs", "cronjobs/status", "jobs", "jobs/status"),
	"autoscaling/v1":                  set("horizontalpodautoscalers", "horizontalpodautoscalers/status"),
	"autoscaling/v2":                  set("horizontalpodautoscalers", "horizontalpodautoscalers/status"),
	"policy/v1":                       set("poddisruptionbudgets", "poddisruptionbudgets/status"),
	"networking.k8s.io/v1":            set("ingressclasses", "ingresses", "ingresses/status", "ipaddresses", "networkpolicies", "servicecidrs", "servicecidrs/status"),
	"storage.k8s.io/v1":               set("csidrivers", "csinodes", "csistoragecapacities", "storageclasses", "volumeattachments", "volumeattachments/status", "volumeattributesclasses"),
	"rbac.authorization.k8s.io/v1":    set("clusterroles", "clusterrolebindings", "roles", "rolebindings"),
	"apiextensions.k8s.io/v1":         set("customresourcedefinitions", "customresourcedefinitions/status"),
	"apiregistration.k8s.io/v1":       set("apiservices", "apiservices/status"),
	"certificates.k8s.io/v1":          set("certificatesigningrequests", "certificatesigningrequests/status", "certificatesigningrequests/approval", "clustertrustbundles", "podcertificaterequests", "podcertificaterequests/status"),
	"admissionregistration.k8s.io/v1": set("mutatingadmissionpolicies", "mutatingadmissionpolicybindings", "mutatingwebhookconfigurations", "validatingadmissionpolicies", "validatingadmissionpolicies/status", "validatingadmissionpolicybindings", "validatingwebhookconfigurations"),
	"coordination.k8s.io/v1":          set("leases"),
	"discovery.k8s.io/v1":             set("endpointslices"),
	"scheduling.k8s.io/v1":            set("priorityclasses"),
	"node.k8s.io/v1":                  set("runtimeclasses"),
	"events.k8s.io/v1":                set("events"),
	"flowcontrol.apiserver.k8s.io/v1": set("flowschemas", "flowschemas/status", "prioritylevelconfigurations", "prioritylevelconfigurations/status"),
	"resource.k8s.io/v1":              set("deviceclasses", "devicetaintrules", "devicetaintrules/status", "resourceclaims", "resourceclaims/status", "resourceclaimtemplates", "resourceslices"),
	"authentication.k8s.io/v1":        set("selfsubjectreviews", "tokenreviews"),
	"authorization.k8s.io/v1":         set("localsubjectaccessreviews", "selfsubjectaccessreviews", "selfsubjectrulesreviews", "subjectaccessreviews"),
}

// builtinGroups are the groups of honorsDryRun's group versions.
var builtinGroups = func() map[string]bool {
	groups := map[string]bool{}
	for gv := range honorsDryRun {
		group, _, _ := strings.Cut(gv, "/")
		groups[group] = true
	}
	return groups
}()

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, item := range items {
		m[item] = true
	}
	return m
}

// deletedHard are the resources whose delete is destructive, by group then
// resource.
var deletedHard = map[string]map[string]bool{
	"core":                 {"namespaces": true, "nodes": true, "persistentvolumes": true, "persistentvolumeclaims": true},
	"apiextensions.k8s.io": {"customresourcedefinitions": true},
}

// writtenHard are the resources any write of which is an access control change,
// by group then resource: who may do what, and what sees or changes every
// write.
var writtenHard = map[string]map[string]bool{
	"rbac.authorization.k8s.io": {"clusterroles": true, "clusterrolebindings": true, "roles": true, "rolebindings": true},
	"admissionregistration.k8s.io": {
		"validatingwebhookconfigurations": true, "mutatingwebhookconfigurations": true,
		"validatingadmissionpolicies": true, "validatingadmissionpolicybindings": true,
		"mutatingadmissionpolicies": true, "mutatingadmissionpolicybindings": true,
	},
}

// scaled are the resources whose spec.replicas a PUT or PATCH can set to 0, by
// group then resource.
var scaled = map[string]map[string]bool{
	"apps": {"deployments": true, "statefulsets": true, "replicasets": true},
	"core": {"replicationcontrollers": true},
}

// destructive is whether a write that is not a dry run is on the destructive
// list.
func destructive(r *http.Request, p apiPath, act permissions.Action, body []byte) bool {
	group := cmp.Or(p.group, "core")
	switch {
	case act.Operation == "deletecollection":
		return true
	case act.Operation == "delete" && p.subresource == "" && deletedHard[group][p.resource]:
		return true
	// Finalize clears what holds a terminating namespace, so it completes the
	// delete above.
	case isNamespace(p) && p.subresource == "finalize":
		return true
	case p.subresource == "ephemeralcontainers":
		return true
	}
	if r.Method != http.MethodPut && r.Method != http.MethodPatch {
		return false
	}
	if p.subresource == "scale" || p.subresource == "" && scaled[group][p.resource] {
		return !keepsReplicas(r.Header.Get("Content-Type"), body, p.subresource == "scale")
	}
	return false
}

// createdName is the metadata.name a create's body gives the object, "" for a
// body that gives none.
func createdName(contentType string, body []byte) string {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	value, err := decodeBody(mediaType, body)
	if err != nil {
		return ""
	}
	root, _ := value.(map[string]any)
	metadata, _ := root["metadata"].(map[string]any)
	name, _ := metadata["name"].(string)
	return name
}

// keepsReplicas is whether a write that can set replicas is in a form the
// proxy reads whole and that leaves at least one. Anything else is destructive, so
// a form the proxy does not know asks rather than passes. On a Scale that is a
// literal positive spec.replicas in a JSON, merge or strategic body, or a JSON
// Patch that only adds or replaces /spec/replicas with one. On a workload it is
// also a body that leaves replicas alone, which the API server then keeps or
// defaults to one.
func keepsReplicas(contentType string, body []byte, onScale bool) bool {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	switch mediaType {
	case "application/json-patch+json":
		return patchKeepsReplicas(body, onScale)
	case "application/apply-patch+yaml":
		if onScale {
			return false
		}
	}
	value, err := decodeBody(mediaType, body)
	if err != nil {
		return false
	}
	return objectKeepsReplicas(value, onScale)
}

// objectKeepsReplicas reads a body that is the object: no $ directive on it or
// its spec, which can replace or drop what it leaves out, and spec.replicas a
// positive count, or on a workload absent.
func objectKeepsReplicas(value any, onScale bool) bool {
	root, ok := value.(map[string]any)
	if !ok || hasDirective(root) {
		return false
	}
	rawSpec, hasSpec := root["spec"]
	if !hasSpec {
		return !onScale
	}
	spec, ok := rawSpec.(map[string]any)
	if !ok || hasDirective(spec) {
		return false
	}
	replicas, hasReplicas := spec["replicas"]
	if !hasReplicas {
		return !onScale
	}
	return positiveCount(replicas)
}

// patchKeepsReplicas reads a JSON Patch, its operations' keys exactly, as the
// API server reads them: an add or replace of /spec/replicas with a positive
// count, and on a workload an add, replace, remove or test under /metadata/ or
// under /spec/ beside replicas. A move or copy is never plain: its value is
// not in the body.
func patchKeepsReplicas(body []byte, onScale bool) bool {
	var ops []map[string]json.RawMessage
	if json.Unmarshal(body, &ops) != nil {
		return false
	}
	for _, op := range ops {
		kind, path := patchString(op["op"]), patchString(op["path"])
		switch {
		case path == "/spec/replicas":
			value, err := decodeBody("application/json", op["value"])
			if (kind != "add" && kind != "replace") || err != nil || !positiveCount(value) {
				return false
			}
		case onScale || !besideReplicas(path):
			return false
		case kind != "add" && kind != "replace" && kind != "remove" && kind != "test":
			return false
		}
	}
	return true
}

// besideReplicas is whether a JSON Pointer names something under metadata, or
// under spec other than replicas.
func besideReplicas(path string) bool {
	if strings.HasPrefix(path, "/metadata/") {
		return true
	}
	field, _, _ := strings.Cut(strings.TrimPrefix(path, "/spec/"), "/")
	return strings.HasPrefix(path, "/spec/") && field != "" && field != "replicas"
}

func hasDirective(obj map[string]any) bool {
	for key := range obj {
		if strings.HasPrefix(key, "$") {
			return true
		}
	}
	return false
}

// positiveCount is whether value is a whole number above zero.
func positiveCount(value any) bool {
	n, ok := value.(json.Number)
	if !ok {
		return false
	}
	count, err := n.Int64()
	return err == nil && count > 0
}

// patchString is a JSON Patch member read as a string, "" for one that is
// missing or of another type.
func patchString(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

// verbWords spell each verb at the head of a summary.
var verbWords = map[string]string{
	"get": "Read", "create": "Create", "update": "Replace", "patch": "Patch",
	"delete": "Delete", "deletecollection": "Delete all",
}

// summary is act in one line, naming the target as kubectl does: "Delete
// pods/api-7f9c in team-a on dev-eks". A Secret read names what it shows:
// "Show Secret db-creds in team-a on dev-eks", "Watch Secret data on dev-eks".
func summary(p apiPath, act permissions.Action) string {
	var line string
	switch {
	case act.AccessLevel == permissions.DataRead && act.Operation == "watch":
		line = "Watch Secret " + cmp.Or(act.Name, "data")
	case act.AccessLevel == permissions.DataRead:
		line = "Show Secret " + cmp.Or(act.Name, "data")
	default:
		line = verbWords[act.Operation] + " " + p.resource
		if act.Name != "" {
			line += "/" + act.Name
		}
		if p.subresource != "" {
			line += "/" + p.subresource
		}
	}
	if p.namespace != "" {
		line += " in " + p.namespace
	}
	if act.AccountName != "" {
		line += " on " + act.AccountName
	}
	return line
}
