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
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

// policyCase is one request and what the policy answers it.
type policyCase struct {
	method, path string
	header       http.Header
	want         refusal
}

func TestThePolicy(t *testing.T) {
	for name, c := range map[string]policyCase{
		"core discovery":          {method: "GET", path: "/api", want: pass},
		"groups":                  {method: "GET", path: "/apis", want: pass},
		"version":                 {method: "GET", path: "/version", want: pass},
		"openapi":                 {method: "GET", path: "/openapi/v3/apis/apps/v1", want: pass},
		"livez":                   {method: "GET", path: "/livez", want: pass},
		"readyz":                  {method: "GET", path: "/readyz", want: pass},
		"healthz":                 {method: "GET", path: "/healthz", want: pass},
		"group version discovery": {method: "GET", path: "/apis/apps/v1", want: pass},
		"group discovery":         {method: "GET", path: "/apis/apps", want: pass},
		"list":                    {method: "GET", path: "/api/v1/pods", want: pass},
		"get":                     {method: "GET", path: "/apis/apps/v1/namespaces/web/deployments/x", want: pass},
		"watch":                   {method: "GET", path: "/api/v1/namespaces/web/pods?watch=true", want: pass},
		"legacy watch":            {method: "GET", path: "/api/v1/watch/pods", want: pass},
		"follow":                  {method: "GET", path: "/api/v1/namespaces/web/pods/x/log?follow=true", want: pass},
		"a status":                {method: "GET", path: "/apis/apps/v1/namespaces/web/deployments/x/status", want: pass},
		"a configmap named proxy": {method: "GET", path: "/api/v1/namespaces/web/configmaps/proxy", want: pass},

		// A write passes the policy; the handler puts it to the user.
		"delete":        {method: "DELETE", path: "/api/v1/namespaces/web/pods/x", want: pass},
		"create":        {method: "POST", path: "/api/v1/namespaces/web/pods", want: pass},
		"patch":         {method: "PATCH", path: "/apis/apps/v1/namespaces/web/deployments/x", want: pass},
		"replace":       {method: "PUT", path: "/api/v1/namespaces/web/configmaps/x", want: pass},
		"a dry run":     {method: "POST", path: "/api/v1/namespaces/web/pods?dryRun=All", want: pass},
		"an eviction":   {method: "POST", path: "/api/v1/namespaces/web/pods/x/eviction", want: pass},
		"a status post": {method: "PUT", path: "/api/v1/namespaces/web/status", want: pass},

		"a token":                    {method: "POST", path: "/api/v1/namespaces/web/serviceaccounts/x/token", want: refusedToken},
		"a token read":               {method: "GET", path: "/api/v1/namespaces/web/serviceaccounts/x/token", want: refusedToken},
		"a token of another group's": {method: "POST", path: "/apis/example.com/v1/namespaces/web/serviceaccounts/x/token", want: pass},

		"metrics":              {method: "GET", path: "/metrics", want: refusedPath},
		"logs":                 {method: "GET", path: "/logs/kube-apiserver.log", want: refusedPath},
		"debug":                {method: "GET", path: "/debug/pprof/", want: refusedPath},
		"a post to discovery":  {method: "POST", path: "/api", want: refusedPath},
		"a delete of openapi":  {method: "DELETE", path: "/openapi/v2", want: refusedPath},
		"a post to a version":  {method: "POST", path: "/apis/apps/v1", want: refusedPath},
		"a head":               {method: "HEAD", path: "/api/v1/pods", want: refusedMethod},
		"an unknown top level": {method: "GET", path: "/foo", want: refusedPath},

		"a secret":             {method: "GET", path: "/api/v1/namespaces/web/secrets/x", want: pass},
		"secrets listed":       {method: "GET", path: "/api/v1/secrets", want: pass},
		"secrets watched":      {method: "GET", path: "/api/v1/watch/namespaces/web/secrets", want: pass},
		"a secret deleted":     {method: "DELETE", path: "/api/v1/namespaces/web/secrets/x", want: pass},
		"a secret created":     {method: "POST", path: "/api/v1/namespaces/web/secrets", want: pass},
		"a secret replaced":    {method: "PUT", path: "/api/v1/namespaces/web/secrets/x", want: pass},
		"a secret patched":     {method: "PATCH", path: "/api/v1/namespaces/web/secrets/x", want: pass},
		"another group's":      {method: "GET", path: "/apis/example.com/v1/secrets", want: pass},
		"a configmap":          {method: "GET", path: "/api/v1/namespaces/secrets/configmaps/secrets", want: pass},
		"exec":                 {method: "POST", path: "/api/v1/namespaces/web/pods/x/exec", want: refusedReach},
		"exec by get":          {method: "GET", path: "/api/v1/namespaces/web/pods/x/exec?command=sh", want: refusedReach},
		"attach":               {method: "POST", path: "/api/v1/namespaces/web/pods/x/attach", want: refusedReach},
		"portforward":          {method: "POST", path: "/api/v1/namespaces/web/pods/x/portforward", want: refusedReach},
		"a pod's proxy":        {method: "GET", path: "/api/v1/namespaces/web/pods/x/proxy/metrics", want: refusedReach},
		"a service's proxy":    {method: "GET", path: "/api/v1/namespaces/web/services/x/proxy", want: refusedReach},
		"a node's proxy":       {method: "GET", path: "/api/v1/nodes/n1/proxy/configz", want: refusedReach},
		"the legacy proxy":     {method: "GET", path: "/api/v1/proxy/namespaces/web/pods/x", want: refusedReach},
		"an upgrade":           {method: "GET", path: "/api/v1/pods", header: http.Header{"Upgrade": {"SPDY/3.1"}}, want: refusedReach},
		"an empty upgrade":     {method: "GET", path: "/api/v1/pods", header: http.Header{"Upgrade": {""}}, want: refusedReach},
		"a connection upgrade": {method: "GET", path: "/api/v1/pods", header: http.Header{"Connection": {"keep-alive, Upgrade"}}, want: refusedReach},
		"a user impersonated":  {method: "GET", path: "/api/v1/pods", header: http.Header{"Impersonate-User": {"admin"}}, want: refusedReach},
		"a group impersonated": {method: "GET", path: "/version", header: http.Header{"Impersonate-Group": {"system:masters"}}, want: refusedReach},
		"extra impersonated":   {method: "GET", path: "/api", header: http.Header{"Impersonate-Extra-Scopes": {"x"}}, want: refusedReach},

		"can-i":                     {method: "POST", path: "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", want: pass},
		"can-i --list":              {method: "POST", path: "/apis/authorization.k8s.io/v1/selfsubjectrulesreviews", want: pass},
		"whoami":                    {method: "POST", path: "/apis/authentication.k8s.io/v1/selfsubjectreviews", want: pass},
		"whoami, beta":              {method: "POST", path: "/apis/authentication.k8s.io/v1beta1/selfsubjectreviews", want: pass},
		"a review in a namespace":   {method: "POST", path: "/apis/authorization.k8s.io/v1/namespaces/web/selfsubjectaccessreviews", want: pass},
		"a review with a name":      {method: "POST", path: "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews/x", want: pass},
		"a review's subresource":    {method: "POST", path: "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews/x/status", want: pass},
		"a review in another group": {method: "POST", path: "/apis/example.com/v1/selfsubjectaccessreviews", want: pass},
		"a subject access review":   {method: "POST", path: "/apis/authorization.k8s.io/v1/subjectaccessreviews", want: pass},
		"a review put":              {method: "PUT", path: "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", want: pass},

		// The parse reads the decoded path and the proxy forwards the raw one.
		"a secret behind ..":  {method: "GET", path: "/api/v1/namespaces/web/configmaps/../secrets/x", want: refusedPath},
		"a . segment":         {method: "GET", path: "/api/v1/./pods", want: refusedPath},
		"an empty segment":    {method: "GET", path: "/apis//v1/pods", want: refusedPath},
		"a trailing slash":    {method: "GET", path: "/api/v1/pods/", want: refusedPath},
		"an escaped slash":    {method: "GET", path: "/api/v1/namespaces/web/configmaps/a%2Fb", want: refusedPath},
		"an escaped secret":   {method: "GET", path: "/api/v1/namespaces/web/%73ecrets/x", want: refusedPath},
		"a query is not path": {method: "GET", path: "/api/v1/pods?labelSelector=a%2Fb", want: pass},
	} {
		t.Run(name, func(t *testing.T) {
			_, got := decide(policyRequest(c))
			assert.Equal(t, c.want, got)
		})
	}
}

// policyRequest is c as the proxy receives it.
func policyRequest(c policyCase) *http.Request {
	r := httptest.NewRequest(c.method, "http://cluster.kubetail.invalid"+c.path, nil)
	maps.Copy(r.Header, c.header)
	return r
}

func TestThePathParse(t *testing.T) {
	for path, want := range map[string]apiPath{
		"/api/v1/pods":                             {kind: resourcePath, version: "v1", resource: "pods"},
		"/api/v1/namespaces/web/pods/x":            {kind: resourcePath, version: "v1", namespace: "web", resource: "pods", name: "x"},
		"/api/v1/namespaces/web/pods/x/log":        {kind: resourcePath, version: "v1", namespace: "web", resource: "pods", name: "x", subresource: "log"},
		"/api/v1/nodes/n1":                         {kind: resourcePath, version: "v1", resource: "nodes", name: "n1"},
		"/api/v1/namespaces":                       {kind: resourcePath, version: "v1", resource: "namespaces"},
		"/api/v1/namespaces/web":                   {kind: resourcePath, version: "v1", resource: "namespaces", name: "web"},
		"/apis/apps/v1/namespaces/web/deployments": {kind: resourcePath, group: "apps", version: "v1", namespace: "web", resource: "deployments"},
		"/apis/rbac.authorization.k8s.io/v1/clusterroles/admin": {
			kind: resourcePath, group: "rbac.authorization.k8s.io", version: "v1", resource: "clusterroles", name: "admin",
		},
		// A name is a name wherever it sits, whatever it spells.
		"/api/v1/namespaces/web/configmaps/proxy":  {kind: resourcePath, version: "v1", namespace: "web", resource: "configmaps", name: "proxy"},
		"/api/v1/namespaces/proxy/configmaps/exec": {kind: resourcePath, version: "v1", namespace: "proxy", resource: "configmaps", name: "exec"},
		// A legacy watch/ or proxy/ is stripped and kept as what it is.
		"/api/v1/watch/namespaces/web/pods":       {kind: resourcePath, watch: true, version: "v1", namespace: "web", resource: "pods"},
		"/apis/apps/v1/watch/deployments":         {kind: resourcePath, watch: true, group: "apps", version: "v1", resource: "deployments"},
		"/api/v1/proxy/namespaces/web/pods/x":     {kind: resourcePath, proxy: true, version: "v1", namespace: "web", resource: "pods", name: "x"},
		"/api/v1/namespaces/web/pods/x/proxy/a/b": {kind: resourcePath, version: "v1", namespace: "web", resource: "pods", name: "x", subresource: "proxy"},
		// A namespace's own subresources.
		"/api/v1/namespaces/web/status":   {kind: resourcePath, version: "v1", resource: "namespaces", name: "web", subresource: "status"},
		"/api/v1/namespaces/web/finalize": {kind: resourcePath, version: "v1", resource: "namespaces", name: "web", subresource: "finalize"},
		// Discovery of a group version, and of a group.
		"/api/v1":       {kind: discoveryPath, version: "v1"},
		"/apis/apps/v1": {kind: discoveryPath, group: "apps", version: "v1"},
		"/apis/apps":    {kind: discoveryPath, group: "apps"},
		// Neither prefix.
		"/api":        {kind: nonResourcePath},
		"/apis":       {kind: nonResourcePath},
		"/version":    {kind: nonResourcePath},
		"/metrics":    {kind: nonResourcePath},
		"/openapi/v3": {kind: nonResourcePath},
	} {
		assert.Equal(t, want, parsePath(path), path)
	}
}

// Each refusal of something that reaches past the API server, a credential
// included, is the reach level, which the boundary never allows; every other
// refusal has no level.
func TestEachRefusalIsReach(t *testing.T) {
	for _, why := range []refusal{refusedReach, refusedToken} {
		level, ok := classifyRefusal(why)
		assert.True(t, ok, why)
		assert.Equal(t, permissions.Reach, level, why)
		for _, principal := range permissions.Principals {
			act := permissions.Action{Principal: principal, Target: permissions.Kubernetes, Scope: "c1", AccessLevel: level}
			allow := permissions.Rule{Effect: permissions.Allow, AccessLevel: level, Principal: "*", Target: "*", Scope: "*", Service: "*", Operation: "*", Resource: "*"}
			v, _ := permissions.Policy{Mode: permissions.ModeAuto, Rules: []permissions.Rule{allow}}.Authorize(act)
			assert.Equal(t, permissions.Deny, v.Effect, "%s %s", why, principal)
		}
	}
	for _, why := range []refusal{pass, refusedMethod, refusedPath, refusedDenied, refusedQuery} {
		_, ok := classifyRefusal(why)
		assert.False(t, ok, why)
	}
}
