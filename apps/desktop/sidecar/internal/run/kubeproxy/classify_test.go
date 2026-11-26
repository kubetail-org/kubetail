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
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

func classifyOf(method, target, contentType, body string) permissions.Action {
	r := httptest.NewRequest(method, "http://"+Host+target, strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	return classify(r, parsePath(r.URL.Path), []byte(body), "c1", "dev-eks")
}

const (
	jsonType     = "application/json"
	mergeType    = "application/merge-patch+json"
	strategic    = "application/strategic-merge-patch+json"
	jsonPatch    = "application/json-patch+json"
	applyPatch   = "application/apply-patch+yaml"
	deployPath   = "/apis/apps/v1/namespaces/team-a/deployments/api"
	scalePath    = "/apis/apps/v1/namespaces/team-a/deployments/api/scale"
	rcPath       = "/api/v1/namespaces/team-a/replicationcontrollers/rc"
	replicasZero = `{"spec":{"replicas":0}}`
)

func TestEveryRequestIsClassified(t *testing.T) {
	const (
		read, data, write, perms, destr = permissions.Read, permissions.DataRead, permissions.Write, permissions.AccessControl, permissions.Destructive
	)
	for _, c := range []struct {
		name                     string
		method, target, ct, body string
		level                    permissions.AccessLevel
		scope, service, op, res  string
		once                     bool
	}{
		{"a read", "GET", "/api/v1/namespaces/team-a/pods/api", "", "", read, "c1/team-a", "", "get", "pods", false},
		{"discovery", "GET", "/apis/apps/v1", "", "", read, "c1", "apps", "get", "", false},
		{"a log", "GET", "/api/v1/namespaces/team-a/pods/api/log", "", "", read, "c1/team-a", "", "get", "pods/log", false},
		{"a secret read", "GET", "/api/v1/namespaces/team-a/secrets/x", "", "", data, "c1/team-a", "", "get", "secrets", false},
		{"a secret list", "GET", "/api/v1/secrets", "", "", data, "c1", "", "list", "secrets", false},
		{"a self review", "POST", "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", jsonType, `{}`, read, "c1", "authorization.k8s.io", "create", "selfsubjectaccessreviews", false},
		{"a dry run on a deployment", "PATCH", deployPath + "?dryRun=All", mergeType, replicasZero, read, "c1/team-a", "apps", "patch", "deployments", false},
		{"a dry run on a secret", "POST", "/api/v1/namespaces/team-a/secrets?dryRun=All", jsonType, `{}`, read, "c1/team-a", "", "create", "secrets", false},
		{"a dry run on a role", "POST", "/apis/rbac.authorization.k8s.io/v1/namespaces/team-a/roles?dryRun=All", jsonType, `{}`, read, "c1/team-a", "rbac.authorization.k8s.io", "create", "roles", false},
		{"a dry run on a custom group", "POST", "/apis/example.com/v1/namespaces/team-a/widgets?dryRun=All", jsonType, `{}`, write, "c1/team-a", "example.com", "create", "widgets", false},
		{"a dry run on an aggregated group", "POST", "/apis/metrics.k8s.io/v1beta1/namespaces/team-a/pods?dryRun=All", jsonType, `{}`, write, "c1/team-a", "metrics.k8s.io", "create", "pods", false},
		{"a dry run on an unknown version of a built-in group", "PATCH", "/apis/apps/v9/namespaces/team-a/deployments/api?dryRun=All", mergeType, `{}`, write, "c1/team-a", "apps", "patch", "deployments", true},
		{"a create", "POST", "/api/v1/namespaces/team-a/pods", jsonType, `{}`, write, "c1/team-a", "", "create", "pods", false},
		{"an update", "PUT", "/api/v1/namespaces/team-a/configmaps/x", jsonType, `{}`, write, "c1/team-a", "", "update", "configmaps", false},
		{"a delete", "DELETE", "/api/v1/namespaces/team-a/pods/api", "", "", write, "c1/team-a", "", "delete", "pods", false},
		{"a dry-run delete", "DELETE", "/api/v1/namespaces/team-a/pods/api?dryRun=All", "", "", write, "c1/team-a", "", "delete", "pods", false},
		{"a delete with no name", "DELETE", "/api/v1/namespaces/team-a/pods", "", "", destr, "c1/team-a", "", "deletecollection", "pods", false},
		{"a namespace delete", "DELETE", "/api/v1/namespaces/team-a", "", "", destr, "c1", "", "delete", "namespaces", true},
		{"a node delete", "DELETE", "/api/v1/nodes/n1", "", "", destr, "c1", "", "delete", "nodes", false},
		{"a node patch", "PATCH", "/api/v1/nodes/n1", mergeType, `{}`, write, "c1", "", "patch", "nodes", false},
		{"a pv delete", "DELETE", "/api/v1/persistentvolumes/pv", "", "", destr, "c1", "", "delete", "persistentvolumes", false},
		{"a pvc delete", "DELETE", "/api/v1/namespaces/team-a/persistentvolumeclaims/c", "", "", destr, "c1/team-a", "", "delete", "persistentvolumeclaims", false},
		{"a crd delete", "DELETE", "/apis/apiextensions.k8s.io/v1/customresourcedefinitions/w.example.com", "", "", destr, "c1", "apiextensions.k8s.io", "delete", "customresourcedefinitions", false},
		{"a namespace finalize", "PUT", "/api/v1/namespaces/team-a/finalize", jsonType, `{}`, destr, "c1", "", "update", "namespaces/finalize", true},
		{"a namespace status", "PUT", "/api/v1/namespaces/team-a/status", jsonType, `{}`, write, "c1", "", "update", "namespaces/status", true},
		{"a namespace create", "POST", "/api/v1/namespaces", jsonType, `{"metadata":{"name":"payments"}}`, write, "c1", "", "create", "namespaces", true},
		{"a custom namespaces delete", "DELETE", "/apis/example.com/v1/namespaces/team-a/namespaces/x", "", "", write, "c1/team-a", "example.com", "delete", "namespaces", false},
		{"an rbac write", "POST", "/apis/rbac.authorization.k8s.io/v1/namespaces/team-a/rolebindings", jsonType, `{}`, perms, "c1/team-a", "rbac.authorization.k8s.io", "create", "rolebindings", false},
		{"a cluster role patch", "PATCH", "/apis/rbac.authorization.k8s.io/v1/clusterroles/x", mergeType, `{}`, perms, "c1", "rbac.authorization.k8s.io", "patch", "clusterroles", false},
		{"a webhook write", "POST", "/apis/admissionregistration.k8s.io/v1/mutatingwebhookconfigurations", jsonType, `{}`, perms, "c1", "admissionregistration.k8s.io", "create", "mutatingwebhookconfigurations", false},
		{"a mutating policy write", "POST", "/apis/admissionregistration.k8s.io/v1beta1/mutatingadmissionpolicies", jsonType, `{}`, perms, "c1", "admissionregistration.k8s.io", "create", "mutatingadmissionpolicies", true},
		{"a mutating policy binding write", "DELETE", "/apis/admissionregistration.k8s.io/v1beta1/mutatingadmissionpolicybindings/x", "", "", perms, "c1", "admissionregistration.k8s.io", "delete", "mutatingadmissionpolicybindings", true},
		{"a policy binding write", "DELETE", "/apis/admissionregistration.k8s.io/v1/validatingadmissionpolicybindings/x", "", "", perms, "c1", "admissionregistration.k8s.io", "delete", "validatingadmissionpolicybindings", false},
		{"a CSR approval", "PUT", "/apis/certificates.k8s.io/v1/certificatesigningrequests/x/approval", jsonType, `{}`, perms, "c1", "certificates.k8s.io", "update", "certificatesigningrequests/approval", false},
		{"a CSR create", "POST", "/apis/certificates.k8s.io/v1/certificatesigningrequests", jsonType, `{}`, write, "c1", "certificates.k8s.io", "create", "certificatesigningrequests", false},
		{"an ephemeral container", "PATCH", "/api/v1/namespaces/team-a/pods/api/ephemeralcontainers", strategic, `{}`, destr, "c1/team-a", "", "patch", "pods/ephemeralcontainers", false},
		{"an eviction", "POST", "/api/v1/namespaces/team-a/pods/api/eviction", jsonType, `{}`, write, "c1/team-a", "", "create", "pods/eviction", false},
		{"a built-in group's unknown subresource", "POST", "/api/v1/namespaces/team-a/pods/api/frobnicate", jsonType, `{}`, write, "c1/team-a", "", "create", "pods/frobnicate", true},
		{"a built-in group's unknown resource", "POST", "/apis/apps/v1/namespaces/team-a/frobs", jsonType, `{}`, write, "c1/team-a", "apps", "create", "frobs", true},
		{"a custom resource", "PATCH", "/apis/example.com/v1/namespaces/team-a/widgets/w", mergeType, `{}`, write, "c1/team-a", "example.com", "patch", "widgets", false},
		{"a custom resource's status", "PUT", "/apis/example.com/v1/namespaces/team-a/widgets/w/status", jsonType, `{}`, write, "c1/team-a", "example.com", "update", "widgets/status", false},
		{"a custom resource's scale", "PATCH", "/apis/example.com/v1/namespaces/team-a/widgets/w/scale", mergeType, `{"spec":{"replicas":2}}`, write, "c1/team-a", "example.com", "patch", "widgets/scale", false},
		{"a custom resource's odd subresource", "POST", "/apis/example.com/v1/namespaces/team-a/widgets/w/restart", jsonType, `{}`, write, "c1/team-a", "example.com", "create", "widgets/restart", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			act := classifyOf(c.method, c.target, c.ct, c.body)
			assert.Equal(t, c.level, act.AccessLevel)
			assert.Equal(t, c.scope, act.Scope)
			assert.Equal(t, c.service, act.Service)
			assert.Equal(t, c.op, act.Operation)
			assert.Equal(t, c.res, act.Resource)
			assert.Equal(t, c.once, act.Once)
			assert.Equal(t, permissions.Kubernetes, act.Target)
			assert.Equal(t, "dev-eks", act.AccountName)
		})
	}
	act := classifyOf("DELETE", "/api/v1/namespaces/team-a/pods/api", "", "")
	assert.Equal(t, "api", act.Name)
	meta := httptest.NewRequest("GET", "http://"+Host+"/api/v1/namespaces/team-a/secrets", nil)
	meta.Header.Set("Accept", "application/json;as=Table;v=v1;g=meta.k8s.io")
	assert.Equal(t, permissions.Read, classify(meta, parsePath(meta.URL.Path), nil, "c1", "dev-eks").AccessLevel, "a Secret's metadata is a read")

	act = classifyOf("POST", "/api/v1/namespaces", jsonType, `{"metadata":{"name":"payments"}}`)
	assert.Equal(t, "payments", act.Name, "a create names its Namespace in the body")
	assert.Equal(t, "Create namespaces/payments on dev-eks", act.Summary)
	act = classifyOf("POST", "/api/v1/namespaces", jsonType, "")
	assert.Empty(t, act.Name, "a create with no body names none")
	assert.Equal(t, "", classify(httptest.NewRequest("DELETE", "http://"+Host+"/api/v1/namespaces/team-a/pods/api", nil),
		parsePath("/api/v1/namespaces/team-a/pods/api"), nil, "", "dev").Scope, "a grant naming no cluster has no account")

	// Every subresource builtin names is named: a write to it is no Once.
	for gv, resources := range builtin {
		for resource := range resources {
			if !strings.Contains(resource, "/") {
				continue
			}
			res, sub, _ := strings.Cut(resource, "/")
			prefix := "/apis/" + gv
			if after, ok := strings.CutPrefix(gv, "core/"); ok {
				prefix = "/api/" + after
			}
			act := classifyOf("PUT", prefix+"/namespaces/team-a/"+res+"/x/"+sub, jsonType, `{"spec":{"replicas":1}}`)
			if res == "namespaces" {
				continue
			}
			assert.False(t, act.Once, "%s %s", gv, resource)
		}
	}
}

func TestAWriteToANamespaceObjectAsksOnce(t *testing.T) {
	for _, c := range []struct{ method, target, body string }{
		{"POST", "/api/v1/namespaces", `{"metadata":{"name":"team-a"}}`},
		{"PATCH", "/api/v1/namespaces/team-a", `{}`},
		{"PUT", "/api/v1/namespaces/team-a/status", `{}`},
	} {
		act := classifyOf(c.method, c.target, mergeType, c.body)
		assert.Equal(t, "c1", act.Scope, "a Namespace object sits at cluster scope")
		assert.True(t, act.Once, c.target)
		v, why := permissions.Policy{Mode: permissions.ModeAuto, Rules: []permissions.Rule{permissions.GrantRule(act)}}.Authorize(chat(act))
		assert.Equal(t, permissions.Verdict{Effect: permissions.Ask}, v, c.target)
		assert.Equal(t, "it always asks", why)
	}
}

func TestRBACAndAdmissionAreAccessControl(t *testing.T) {
	role := classifyOf("POST", "/apis/rbac.authorization.k8s.io/v1/namespaces/team-a/roles", jsonType, `{}`)
	assert.Equal(t, permissions.AccessControl, role.AccessLevel)
	assert.True(t, role.Confined, "a Role in a namespace is confined")
	v, _ := permissions.Policy{Mode: permissions.ModeAsk}.Authorize(chat(role))
	assert.Equal(t, permissions.Verdict{Effect: permissions.Ask, Grantable: true}, v)

	for _, target := range []string{
		"/apis/rbac.authorization.k8s.io/v1/clusterroles",
		"/apis/rbac.authorization.k8s.io/v1/clusterrolebindings",
		"/apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations",
	} {
		act := classifyOf("POST", target, jsonType, `{}`)
		assert.Equal(t, permissions.AccessControl, act.AccessLevel, target)
		assert.False(t, act.Confined, target)
		v, _ := permissions.Policy{Mode: permissions.ModeAuto, Rules: []permissions.Rule{permissions.GrantRule(act)}}.Authorize(chat(act))
		assert.Equal(t, permissions.Verdict{Effect: permissions.Ask}, v, target)
	}
	csr := classifyOf("PUT", "/apis/certificates.k8s.io/v1/certificatesigningrequests/x/approval", jsonType, `{}`)
	assert.Equal(t, permissions.AccessControl, csr.AccessLevel)
	assert.False(t, csr.Confined)
}

// chat is act as the chat sent it.
func chat(act permissions.Action) permissions.Action {
	act.Principal = permissions.Chat
	return act
}

func TestAScaleIsWriteOnlyInAPlainForm(t *testing.T) {
	for _, c := range []struct {
		name, method, target, ct, body string
		level                          permissions.AccessLevel
	}{
		// What passes on a Scale.
		{"merge to 3", "PATCH", scalePath, mergeType, `{"spec":{"replicas":3}}`, permissions.Write},
		{"strategic to 3", "PATCH", scalePath, strategic, `{"spec":{"replicas":3}}`, permissions.Write},
		{"put to 3", "PUT", scalePath, jsonType, `{"metadata":{"name":"api"},"spec":{"replicas":3}}`, permissions.Write},
		{"json patch to 3", "PATCH", scalePath, jsonPatch, `[{"op":"replace","path":"/spec/replicas","value":3}]`, permissions.Write},
		{"json patch add 1", "PATCH", scalePath, jsonPatch, `[{"op":"add","path":"/spec/replicas","value":1}]`, permissions.Write},

		// What asks on a Scale.
		{"merge to 0", "PATCH", scalePath, mergeType, replicasZero, permissions.Destructive},
		{"put to 0", "PUT", scalePath, jsonType, replicasZero, permissions.Destructive},
		{"merge without replicas", "PATCH", scalePath, mergeType, `{"metadata":{"labels":{"a":"b"}}}`, permissions.Destructive},
		{"merge spec null", "PATCH", scalePath, mergeType, `{"spec":null}`, permissions.Destructive},
		{"merge replicas null", "PATCH", scalePath, mergeType, `{"spec":{"replicas":null}}`, permissions.Destructive},
		{"replicas a string", "PATCH", scalePath, mergeType, `{"spec":{"replicas":"3"}}`, permissions.Destructive},
		{"replicas a fraction", "PATCH", scalePath, mergeType, `{"spec":{"replicas":2.5}}`, permissions.Destructive},
		{"put an empty spec", "PUT", scalePath, jsonType, `{"spec":{}}`, permissions.Destructive},
		{"apply to 3", "PATCH", scalePath, applyPatch, "spec:\n  replicas: 3\n", permissions.Destructive},
		{"strategic replace keeping 3", "PATCH", scalePath, strategic, `{"spec":{"$patch":"replace","replicas":3}}`, permissions.Destructive},
		{"strategic delete beside 3", "PATCH", scalePath, strategic, `{"spec":{"$patch":"delete","replicas":3}}`, permissions.Destructive},
		{"strategic retain keys", "PATCH", scalePath, strategic, `{"spec":{"$retainKeys":["replicas"],"replicas":3}}`, permissions.Destructive},
		{"directive on the object", "PATCH", scalePath, strategic, `{"$patch":"replace","spec":{"replicas":3}}`, permissions.Destructive},
		{"does not decode", "PATCH", scalePath, mergeType, `{"spec":`, permissions.Destructive},
		{"json patch to 0", "PATCH", scalePath, jsonPatch, `[{"op":"replace","path":"/spec/replicas","value":0}]`, permissions.Destructive},
		{"json patch remove", "PATCH", scalePath, jsonPatch, `[{"op":"remove","path":"/spec/replicas"}]`, permissions.Destructive},
		{"json patch replace spec", "PATCH", scalePath, jsonPatch, `[{"op":"replace","path":"/spec","value":{"replicas":3}}]`, permissions.Destructive},
		{"json patch move away", "PATCH", scalePath, jsonPatch, `[{"op":"move","from":"/spec/replicas","path":"/status/replicas"}]`, permissions.Destructive},
		{"json patch copy away", "PATCH", scalePath, jsonPatch, `[{"op":"copy","from":"/spec/replicas","path":"/status/replicas"}]`, permissions.Destructive},
		{"json patch elsewhere", "PATCH", scalePath, jsonPatch, `[{"op":"replace","path":"/metadata/labels/a","value":"b"}]`, permissions.Destructive},
		{"json patch key in another case", "PATCH", scalePath, jsonPatch, `[{"op":"replace","path":"/spec/replicas","Path":"/metadata/labels","value":0}]`, permissions.Destructive},
		{"json patch op in another case", "PATCH", scalePath, jsonPatch, `[{"op":"replace","Op":"test","path":"/spec/replicas","value":0}]`, permissions.Destructive},
		{"json patch does not decode", "PATCH", scalePath, jsonPatch, `[{"op":`, permissions.Destructive},

		// What passes on a workload, which leaves replicas alone when it omits them.
		{"workload merge to 3", "PATCH", deployPath, mergeType, `{"spec":{"replicas":3}}`, permissions.Write},
		{"workload merge of labels", "PATCH", deployPath, mergeType, `{"metadata":{"labels":{"a":"b"}}}`, permissions.Write},
		{"workload set image", "PATCH", deployPath, strategic,
			`{"spec":{"template":{"spec":{"$setElementOrder/containers":[{"name":"api"}],"containers":[{"name":"api","image":"api:2"}]}}}}`, permissions.Write},
		{"workload put without replicas", "PUT", deployPath, jsonType, `{"spec":{}}`, permissions.Write},
		{"workload apply without replicas", "PATCH", deployPath, applyPatch, "spec:\n  template: {}\n", permissions.Write},
		{"workload json patch of the image", "PATCH", deployPath, jsonPatch,
			`[{"op":"replace","path":"/spec/template/spec/containers/0/image","value":"api:2"}]`, permissions.Write},
		{"workload json patch of a label", "PATCH", deployPath, jsonPatch, `[{"op":"add","path":"/metadata/labels/a","value":"b"}]`, permissions.Write},

		// What asks on a workload.
		{"workload merge to 0", "PATCH", deployPath, mergeType, replicasZero, permissions.Destructive},
		{"workload strategic to 0", "PATCH", deployPath, strategic, replicasZero, permissions.Destructive},
		{"workload apply to 0", "PATCH", deployPath, applyPatch, "spec:\n  replicas: 0\n", permissions.Destructive},
		{"workload put to 0", "PUT", "/apis/apps/v1/namespaces/team-a/statefulsets/db", jsonType, replicasZero, permissions.Destructive},
		{"workload spec null", "PATCH", deployPath, mergeType, `{"spec":null}`, permissions.Destructive},
		{"workload directive on spec", "PATCH", deployPath, strategic, `{"spec":{"$patch":"delete"}}`, permissions.Destructive},
		{"workload json patch remove", "PATCH", deployPath, jsonPatch, `[{"op":"remove","path":"/spec/replicas"}]`, permissions.Destructive},
		{"workload json patch replace spec", "PATCH", deployPath, jsonPatch, `[{"op":"replace","path":"/spec","value":{}}]`, permissions.Destructive},
		{"workload json patch move from replicas", "PATCH", deployPath, jsonPatch,
			`[{"op":"move","from":"/spec/replicas","path":"/metadata/annotations/r"}]`, permissions.Destructive},
		{"workload json patch move onto replicas", "PATCH", deployPath, jsonPatch,
			`[{"op":"move","from":"/metadata/annotations/r","path":"/spec/replicas"}]`, permissions.Destructive},
		{"workload json patch move beside replicas", "PATCH", deployPath, jsonPatch,
			`[{"op":"move","from":"/metadata/annotations/r","path":"/metadata/labels/r"}]`, permissions.Destructive},
		{"workload json patch elsewhere at the top", "PATCH", deployPath, jsonPatch, `[{"op":"add","path":"/status","value":{}}]`, permissions.Destructive},
		{"workload json patch unknown op", "PATCH", deployPath, jsonPatch, `[{"op":"frobnicate","path":"/metadata/labels/a"}]`, permissions.Destructive},
		{"replication controller merge to 3", "PATCH", rcPath, mergeType, `{"spec":{"replicas":3}}`, permissions.Write},
		{"replication controller merge to 0", "PATCH", rcPath, mergeType, replicasZero, permissions.Destructive},
		{"replication controller put to 0", "PUT", rcPath, jsonType, replicasZero, permissions.Destructive},
		{"replication controller json patch remove", "PATCH", rcPath, jsonPatch, `[{"op":"remove","path":"/spec/replicas"}]`, permissions.Destructive},
		{"custom deployments to 0", "PATCH", "/apis/example.com/v1/namespaces/team-a/deployments/x", mergeType, replicasZero, permissions.Write},
	} {
		act := classifyOf(c.method, c.target, c.ct, c.body)
		assert.Equal(t, c.level, act.AccessLevel, c.name)
	}
}

func TestASummaryNamesTheTargetAsKubectlDoes(t *testing.T) {
	for want, act := range map[string]permissions.Action{
		"Delete pods/api-7f9c in team-a on dev-eks":        classifyOf("DELETE", "/api/v1/namespaces/team-a/pods/api-7f9c", "", ""),
		"Delete nodes/n1 on dev-eks":                       classifyOf("DELETE", "/api/v1/nodes/n1", "", ""),
		"Create pods in team-a on dev-eks":                 classifyOf("POST", "/api/v1/namespaces/team-a/pods", jsonType, `{}`),
		"Delete all pods in team-a on dev-eks":             classifyOf("DELETE", "/api/v1/namespaces/team-a/pods", "", ""),
		"Patch deployments/api/scale in team-a on dev-eks": classifyOf("PATCH", scalePath, mergeType, `{}`),
		"Replace widgets/w1 in team-a on dev-eks":          classifyOf("PUT", "/apis/example.com/v1/namespaces/team-a/widgets/w1", jsonType, `{}`),
		"Delete namespaces/team-a on dev-eks":              classifyOf("DELETE", "/api/v1/namespaces/team-a", "", ""),
	} {
		assert.Equal(t, want, act.Summary)
	}
	r := httptest.NewRequest("DELETE", "http://"+Host+"/api/v1/nodes/n1", nil)
	assert.Equal(t, "Delete nodes/n1", classify(r, parsePath(r.URL.Path), nil, "c1", "").Summary, "a record naming no context")
}

func TestTheAlwaysAsksListsAreInWords(t *testing.T) {
	for _, list := range [][]string{AlwaysAsks, AccessControl} {
		assert.NotEmpty(t, list)
		for _, line := range list {
			assert.NotEmpty(t, line)
		}
	}
}

func TestClassifyMarksADryRun(t *testing.T) {
	act := classifyOf("PATCH", "/apis/example.com/v1/namespaces/team-a/widgets/w?dryRun=All", mergeType, `{}`)
	assert.True(t, act.DryRun)
	assert.Equal(t, permissions.Write, act.AccessLevel, "a dry run an API may ignore stays a write")
	v, _ := permissions.Policy{Mode: permissions.ModeAsk}.Authorize(chat(act))
	assert.Equal(t, permissions.Verdict{Effect: permissions.Ask}, v, "a dry run is not grantable")

	act = classifyOf("PATCH", "/apis/example.com/v1/namespaces/team-a/widgets/w", mergeType, `{}`)
	assert.False(t, act.DryRun)
	act = classifyOf("DELETE", "/api/v1/namespaces/team-a/pods/api?dryRun=All", "", "")
	assert.False(t, act.DryRun, "a DELETE's dry run is in a body the proxy does not read")
}

func TestADataReadVerbIsTheServers(t *testing.T) {
	for _, c := range []struct {
		target, verb, summary string
	}{
		{"/api/v1/namespaces/team-a/secrets/db-creds", "get", "Show Secret db-creds in team-a on dev-eks"},
		{"/api/v1/namespaces/team-a/secrets", "list", "Show Secret data in team-a on dev-eks"},
		{"/api/v1/namespaces/team-a/secrets?watch=1", "watch", "Watch Secret data in team-a on dev-eks"},
		{"/api/v1/watch/namespaces/team-a/secrets", "watch", "Watch Secret data in team-a on dev-eks"},
		{"/api/v1/watch/namespaces/team-a/secrets/db-creds", "watch", "Watch Secret db-creds in team-a on dev-eks"},
		{"/api/v1/secrets", "list", "Show Secret data on dev-eks"},
	} {
		act := classifyOf("GET", c.target, "", "")
		assert.Equal(t, permissions.DataRead, act.AccessLevel, c.target)
		assert.Equal(t, c.verb, act.Operation, c.target)
		assert.Equal(t, c.summary, act.Summary, c.target)
	}
	assert.Equal(t, "update", classifyOf("PUT", "/api/v1/namespaces/team-a/secrets/x", jsonType, `{}`).Operation, "a write's verb is unchanged")
}

// excluded are the kinds the scheme serves in a built-in group version that
// are no resource a write names: lists, options, events on a watch, and
// objects only a subresource takes.
var excluded = map[string]bool{
	"Status": true, "WatchEvent": true, "APIGroup": true, "APIGroupList": true, "APIResourceList": true, "APIVersions": true,
	"CreateOptions": true, "DeleteOptions": true, "GetOptions": true, "ListOptions": true, "PatchOptions": true, "UpdateOptions": true,
	"ExportOptions": true, "TableOptions": true, "Table": true, "PartialObjectMetadata": true, "PartialObjectMetadataList": true,
	"PodLogOptions": true, "PodExecOptions": true, "PodAttachOptions": true, "PodPortForwardOptions": true, "PodProxyOptions": true,
	"NodeProxyOptions": true, "ServiceProxyOptions": true, "SerializedReference": true, "RangeAllocation": true,
	"Scale": true, "Eviction": true, "TokenRequest": true,
}

// TestBuiltinNamesEveryServedKind walks client-go's scheme for every group
// version the API server serves itself, and fails on a kind builtin lacks
// and excluded does not hold.
func TestBuiltinNamesEveryServedKind(t *testing.T) {
	for gvk := range scheme.Scheme.AllKnownTypes() {
		group := cmp.Or(gvk.Group, "core")
		gv := group + "/" + gvk.Version
		if !honorsDryRun[gv] || excluded[gvk.Kind] || strings.HasSuffix(gvk.Kind, "List") {
			continue
		}
		plural, _ := meta.UnsafeGuessKindToResource(gvk)
		assert.True(t, builtin[gv][plural.Resource], "%s %s (%s) is served but not in builtin", gv, gvk.Kind, plural.Resource)
	}
	for gv := range builtin {
		assert.True(t, honorsDryRun[gv], "builtin names %s, which honorsDryRun does not", gv)
	}
}

// A write no table names asks each time under every mode and rule, and no
// answer to it writes a rule; a deny and read-only still stop it.
func TestAnUnnamedOperationAsksOnceAndIsNotGranted(t *testing.T) {
	for _, target := range []string{
		"/api/v1/namespaces/team-a/pods/api/frobnicate",
		"/apis/apps/v1/namespaces/team-a/frobs",
		"/apis/example.com/v1/namespaces/team-a/widgets/w/restart",
	} {
		act := chat(classifyOf("POST", target, jsonType, `{}`))
		assert.True(t, act.Once, target)
		for _, mode := range []permissions.Mode{permissions.ModeAsk, permissions.ModeAuto} {
			for _, rules := range [][]permissions.Rule{nil, {permissions.GrantRule(act)}, {permissions.CommandRule(act)}} {
				v, _ := permissions.Policy{Mode: mode, Rules: rules}.Authorize(act)
				assert.Equal(t, permissions.Verdict{Effect: permissions.Ask}, v, "%s %s", target, mode)
			}
		}
		v, _ := permissions.Policy{Mode: permissions.ModeReadOnly}.Authorize(act)
		assert.Equal(t, permissions.Deny, v.Effect, target)
	}
}
