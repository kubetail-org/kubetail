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

package gcpproxy

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

// classifyURL classifies method on a service's path and query, with no body.
func classifyURL(t *testing.T, service, method, target string) verdict {
	t.Helper()
	return classifyBody(t, service, method, target, "")
}

// classifyBody classifies method on a service's path and query, carrying
// body.
func classifyBody(t *testing.T, service, method, target, body string) verdict {
	t.Helper()
	path, query, _ := strings.Cut(target, "?")
	segs, why := checkPath(serviceNamed(service), path)
	require.Empty(t, why, target)
	q, err := url.ParseQuery(query)
	require.NoError(t, err)
	return classify(service, method, segs, q, http.Header{}, []byte(body))
}

// level is the verdict of a request that asks at l.
func level(l permissions.AccessLevel) verdict { return verdict{kind: asks, level: l} }

// TestAReadIsClassifiedAsToday pins every kind and level the classifier
// answers, reads and refusals as before.
func TestAReadIsClassifiedAsToday(t *testing.T) {
	isRefused := verdict{kind: refused}
	isData := level(permissions.DataRead)
	isRead := verdict{kind: read, level: permissions.Read}
	isChange := level(permissions.Write)
	isDestructive := level(permissions.Destructive)
	isPermissions := level(permissions.AccessControl)
	cases := []struct {
		name, service, method, target string
		want                          verdict
	}{
		{"a key create", "iam", http.MethodPost, "/v1/projects/p/serviceAccounts/sa@p.iam.gserviceaccount.com/keys", isRefused},
		{"a signBlob", "iam", http.MethodPost, "/v1/projects/-/serviceAccounts/sa:signBlob", isRefused},
		{"a signJwt", "iam", http.MethodPost, "/v1/projects/-/serviceAccounts/sa:signJwt", isRefused},
		{"an access token", "iam", http.MethodPost, "/v1/projects/-/serviceAccounts/sa:generateAccessToken", isRefused},
		{"an ID token", "iam", http.MethodPost, "/v1/projects/-/serviceAccounts/sa:generateIdToken", isRefused},
		{"an HMAC key create", "storage", http.MethodPost, "/storage/v1/projects/p/hmacKeys?serviceAccountEmail=sa", isRefused},
		{"an OAuth client credential's create", "iam", http.MethodPost, "/v1/projects/p/locations/global/oauthClients/c/credentials", isRefused},
		{"an OAuth client credential's get", "iam", http.MethodGet, "/v1/projects/p/locations/global/oauthClients/c/credentials/k", isRefused},
		{"an OAuth client credentials' list", "iam", http.MethodGet, "/v1/projects/p/locations/global/oauthClients/c/credentials", isRefused},
		{"a SCIM token's create", "iam", http.MethodPost, "/v1/locations/global/workforcePools/w/providers/p/scimTenants/s/tokens", isRefused},
		{"a SCIM token's get", "iam", http.MethodGet, "/v1/locations/global/workforcePools/w/providers/p/scimTenants/s/tokens/t", isRefused},
		{"a resumable upload", "storage", http.MethodPost, "/upload/storage/v1/b/bkt/o?uploadType=resumable&name=x", isRefused},
		{"alt=media", "storage", http.MethodGet, "/storage/v1/b/bkt/o/x?alt=media", isData},
		{"$alt=media", "storage", http.MethodGet, "/storage/v1/b/bkt/o/x?$alt=media", isData},
		{"an alt sent twice, the second media", "storage", http.MethodGet, "/storage/v1/b/bkt/o/x?alt=json&alt=media", isData},
		{"ALT=media", "storage", http.MethodGet, "/storage/v1/b/bkt/o/x?ALT=media", isData},
		{"$Alt=MEDIA", "storage", http.MethodGet, "/storage/v1/b/bkt/o/x?$Alt=MEDIA", isData},
		{"$uploadType=resumable", "storage", http.MethodPost, "/upload/storage/v1/b/bkt/o?$uploadType=resumable", isRefused},
		{"uploadType=Resumable", "storage", http.MethodPost, "/upload/storage/v1/b/bkt/o?uploadType=Resumable", isRefused},
		{"upload_protocol=resumable", "storage", http.MethodPost, "/upload/storage/v1/b/bkt/o?upload_protocol=resumable", isRefused},
		{"an HMAC key create under /download/", "storage", http.MethodPost, "/download/storage/v1/projects/p/hmacKeys", isRefused},
		{"an HMAC key create under /upload/", "storage", http.MethodPost, "/upload/storage/v1/projects/p/hmacKeys", isRefused},
		{"a download path", "storage", http.MethodGet, "/download/storage/v1/b/bkt/o/x", isData},
		{"a serial port", "compute", http.MethodGet, "/compute/v1/projects/p/zones/z/instances/i/serialPort?port=1", isData},
		{"guest attributes", "compute", http.MethodGet, "/compute/v1/projects/p/zones/z/instances/i/getGuestAttributes", isData},
		{"a screenshot", "compute", http.MethodGet, "/compute/beta/projects/p/zones/z/instances/i/screenshot", isData},
		{"an instance list", "compute", http.MethodGet, "/compute/v1/projects/p/zones/z/instances", isRead},
		{"an object's metadata", "storage", http.MethodGet, "/storage/v1/b/bkt/o/x?alt=json", isRead},
		{"a HEAD", "storage", http.MethodHead, "/storage/v1/b/bkt", isRead},
		{"a key list", "iam", http.MethodGet, "/v1/projects/p/serviceAccounts/sa/keys", isRead},
		{"compute's getIamPolicy", "compute", http.MethodGet, "/compute/v1/projects/p/zones/z/instances/i/getIamPolicy", isRead},
		{"compute's testIamPermissions", "compute", http.MethodPost, "/compute/v1/projects/p/zones/z/instances/i/testIamPermissions", isRead},
		{"Resource Manager's getIamPolicy", "cloudresourcemanager", http.MethodPost, "/v1/projects/p:getIamPolicy", isRead},
		{"Resource Manager's testIamPermissions", "cloudresourcemanager", http.MethodPost, "/v3/projects/p:testIamPermissions", isRead},
		{"entries:list", "logging", http.MethodPost, "/v2/entries:list", isRead},
		{"a project's delete", "cloudresourcemanager", http.MethodDelete, "/v3/projects/p", isDestructive},
		{"a cluster's delete", "container", http.MethodDelete, "/v1/projects/p/locations/us-central1/clusters/dev", isDestructive},
		{"a zonal cluster's delete", "container", http.MethodDelete, "/v1/projects/p/zones/us-central1-a/clusters/dev", isDestructive},
		{"a bucket's delete", "storage", http.MethodDelete, "/storage/v1/b/bkt", isDestructive},
		{"a service account's delete", "iam", http.MethodDelete, "/v1/projects/p/serviceAccounts/sa@p.iam.gserviceaccount.com", isPermissions},
		{"a setIamPolicy", "compute", http.MethodPost, "/compute/v1/projects/p/zones/z/instances/i/setIamPolicy", isPermissions},
		{"a :setIamPolicy", "cloudresourcemanager", http.MethodPost, "/v1/projects/p:setIamPolicy", isPermissions},
		{"a bucket's IAM PUT", "storage", http.MethodPut, "/storage/v1/b/bkt/iam", isPermissions},
		{"a managed folder's IAM PUT", "storage", http.MethodPut, "/storage/v1/b/bkt/managedFolders/f/iam", isPermissions},
		{"a service account's create", "iam", http.MethodPost, "/v1/projects/p/serviceAccounts", isPermissions},
		{"a role's patch", "iam", http.MethodPatch, "/v1/projects/p/roles/r", isPermissions},
		{"a bucket's delete under /upload/", "storage", http.MethodDelete, "/upload/storage/v1/b/bkt", isDestructive},
		{"an object named iam's update", "storage", http.MethodPut, "/storage/v1/b/bkt/o/iam", isChange},
		{"an instance's delete", "compute", http.MethodDelete, "/compute/v1/projects/p/zones/z/instances/i", isChange},
		{"an object's delete", "storage", http.MethodDelete, "/storage/v1/b/bkt/o/x", isChange},
		{"a node pool's delete", "container", http.MethodDelete, "/v1/projects/p/locations/l/clusters/c/nodePools/np", isChange},
		{"a log write", "logging", http.MethodPost, "/v2/entries:write", isChange},
		{"a media upload", "storage", http.MethodPost, "/upload/storage/v1/b/bkt/o?uploadType=media&name=x", isChange},
		{"a POST with alt=media", "storage", http.MethodPost, "/storage/v1/b/bkt/o/x/compose?alt=media", isChange},
	}
	for _, tc := range cases {
		got := classifyURL(t, tc.service, tc.method, tc.target)
		assert.Equal(t, tc.want.kind, got.kind, tc.name)
		if tc.want.kind != refused {
			assert.Equal(t, tc.want.level, got.level, tc.name)
		}
		if tc.want.kind == refused {
			assert.True(t, strings.HasPrefix(got.why, "kubetail: "), tc.name)
		}
	}
}

const project = "/compute/v1/projects/my-project"

// TestEveryFieldIsFilled reads each field of a change's action off its path.
func TestEveryFieldIsFilled(t *testing.T) {
	for _, c := range []struct {
		name, service, method, target         string
		scope, account, operation, res, named string
	}{
		{"an instance start", "compute", http.MethodPost, project + "/zones/us-central1-a/instances/vm-1/start",
			"my-project/us-central1-a", "my-project", "start", "instances", "vm-1"},
		{"an instance create", "compute", http.MethodPost, project + "/zones/us-central1-a/instances",
			"my-project/us-central1-a", "my-project", "post", "instances", ""},
		{"a global network's delete", "compute", http.MethodDelete, project + "/global/networks/n",
			"my-project", "my-project", "delete", "networks", "n"},
		{"a regional address", "compute", http.MethodPost, project + "/regions/europe-west1/addresses",
			"my-project/europe-west1", "my-project", "post", "addresses", ""},
		{"a node pool's setSize", "container", http.MethodPost, "/v1/projects/my-project/locations/us-central1/clusters/dev/nodePools/np:setSize",
			"my-project/us-central1", "my-project", "setsize", "clusters/nodepools", "np"},
		{"a zonal cluster's delete", "container", http.MethodDelete, "/v1/projects/my-project/zones/us-central1-a/clusters/dev",
			"my-project/us-central1-a", "my-project", "delete", "clusters", "dev"},
		{"a project's setIamPolicy", "cloudresourcemanager", http.MethodPost, "/v1/projects/my-project:setIamPolicy",
			"my-project", "my-project", "setiampolicy", "projects", "my-project"},
		{"a service account's patch", "iam", http.MethodPatch, "/v1/projects/my-project/serviceAccounts/sa@x",
			"my-project", "my-project", "patch", "serviceaccounts", "sa@x"},
		{"a log sink's create", "logging", http.MethodPost, "/v2/projects/my-project/sinks",
			"my-project", "my-project", "post", "sinks", ""},
		{"an object's delete", "storage", http.MethodDelete, "/storage/v1/b/bkt/o/x",
			"", "", "delete", "b/o", "x"},
	} {
		v := classifyURL(t, c.service, c.method, c.target)
		require.Equal(t, asks, v.kind, c.name)
		act := v.act
		assert.Equal(t, c.scope, act.Scope, c.name)
		assert.Equal(t, c.account, act.AccountName, c.name)
		assert.Equal(t, c.service, act.Service, c.name)
		assert.Equal(t, c.operation, act.Operation, c.name)
		assert.Equal(t, c.res, act.Resource, c.name)
		assert.Equal(t, c.named, act.Name, c.name)
		assert.Equal(t, v.level, act.AccessLevel, c.name)
		assert.NotEmpty(t, act.Summary, c.name)
	}
}

func TestAScopeIsLowerCase(t *testing.T) {
	v := classifyURL(t, "compute", http.MethodPost, "/compute/v1/projects/My-Project/zones/US-Central1-A/instances/vm/start")
	assert.Equal(t, "my-project/us-central1-a", v.act.Scope)
	assert.Equal(t, "my-project", v.act.AccountName)
}

// A project Kubetail cannot read as an id gives no scope. A request that also
// reaches past its project keeps the project's scope, so its mode and rules
// still decide it, and asks each time, though its operation is ordinary.
func TestAForeignRequestKeepsItsScopeAndAsksOnce(t *testing.T) {
	for _, c := range []struct{ name, service, method, target, body string }{
		{"a project number", "compute", http.MethodPost, "/compute/v1/projects/123456789012/zones/z/instances/vm/start", ""},
		{"projects/-", "container", http.MethodPost, "/v1/projects/-/locations/l/clusters", "{}"},
		{"a project too short", "compute", http.MethodPost, "/compute/v1/projects/p/zones/z/instances/vm/start", ""},
		{"a storage object", "storage", http.MethodPost, "/upload/storage/v1/b/bkt/o?uploadType=media&name=x", "hello"},
		{"a path naming no project", "logging", http.MethodPost, "/v2/entries:write", `{"logName":"projects/my-project/logs/x"}`},
	} {
		v := classifyBody(t, c.service, c.method, c.target, c.body)
		require.Equal(t, asks, v.kind, c.name)
		assert.Empty(t, v.act.Scope, c.name)
	}
	start := project + "/zones/z/instances/vm/start"
	for _, c := range []struct{ name, body string }{
		{"another project in a body", `{"networkInterfaces":[{"network":"projects/other-project/global/networks/default"}]}`},
		{"another project in a key", `{"projects/other-project":1}`},
		{"a project number in a body", `{"network":"projects/123456789/global/networks/n"}`},
		{"a body that is not JSON", `name=vm`},
		{"a body cut short", `{"name":`},
		// Ⱥ lowers from two bytes to three, so an offset in the lowered string
		// runs past the end of this one.
		{"a reference after text that lowers longer", `{"n":"ȺȺȺȺȺȺȺȺȺȺprojects/"}`},
	} {
		v := classifyBody(t, "compute", http.MethodPost, start, c.body)
		require.Equal(t, asks, v.kind, c.name)
		assert.Equal(t, "my-project/z", v.act.Scope, c.name)
		assert.True(t, v.act.Once, c.name)
	}
	// A request billed to another project charges it, so it asks too.
	for _, c := range []struct{ quota, query string }{
		{"other-project", ""}, {"", "userProject=other-project"}, {"", "$USERPROJECT=other-project"},
	} {
		segs, why := checkPath(serviceNamed("compute"), start)
		require.Empty(t, why)
		q, err := url.ParseQuery(c.query)
		require.NoError(t, err)
		v := classify("compute", http.MethodPost, segs, q, http.Header{"X-Goog-User-Project": {c.quota}}, nil)
		assert.Equal(t, "my-project/z", v.act.Scope, "%+v", c)
		assert.True(t, v.act.Once, "%+v", c)
	}
	for _, quota := range []string{"", "my-project", "My-Project"} {
		segs, _ := checkPath(serviceNamed("compute"), start)
		v := classify("compute", http.MethodPost, segs, url.Values{}, http.Header{"X-Goog-User-Project": {quota}}, nil)
		assert.False(t, v.act.Once, "an ordinary start billed to its own project, quota %q", quota)
	}
	for _, body := range []string{
		"", `{"name":"vm","network":"projects/my-project/global/networks/default"}`, `{"labels":{"a":"b"}}`,
		`{"network":"ȺȺȺȺ projects/my-project/global/networks/default"}`,
	} {
		v := classifyBody(t, "compute", http.MethodPost, start, body)
		assert.Equal(t, "my-project/z", v.act.Scope, body)
		assert.False(t, v.act.Once, body)
	}
}

// A storage change to who may reach a bucket or an object is an access
// control change: by its path, a predefined ACL or its metadata's decoded
// keys, a multipart upload's included.
func TestAStorageACLChangeIsAccessControl(t *testing.T) {
	for _, c := range []struct{ name, method, target, body string }{
		{"a bucket's ACL entry", http.MethodPut, "/storage/v1/b/bkt/acl/allUsers", `{"role":"READER"}`},
		{"a bucket's ACL insert", http.MethodPost, "/storage/v1/b/bkt/acl", `{"entity":"allUsers","role":"READER"}`},
		{"a default object ACL", http.MethodDelete, "/storage/v1/b/bkt/defaultObjectAcl/allUsers", ""},
		{"an object's ACL", http.MethodPatch, "/storage/v1/b/bkt/o/obj/acl/allUsers", `{"role":"READER"}`},
		{"a predefined ACL", http.MethodPatch, "/storage/v1/b/bkt?predefinedAcl=publicRead", `{}`},
		{"a predefined default object ACL", http.MethodPatch, "/storage/v1/b/bkt?predefinedDefaultObjectAcl=publicRead", `{}`},
		{"a bucket's metadata", http.MethodPatch, "/storage/v1/b/bkt", `{"iamConfiguration":{"uniformBucketLevelAccess":{"enabled":false}}}`},
		{"an object's metadata", http.MethodPatch, "/storage/v1/b/bkt/o/obj", `{"acl":[{"entity":"allUsers","role":"READER"}]}`},
		{"an escaped key", http.MethodPatch, "/storage/v1/b/bkt/o/obj", `{"\u0061cl":[]}`},
		{"metadata that does not decode", http.MethodPatch, "/storage/v1/b/bkt/o/obj", `{"acl":`},
	} {
		v := classifyBody(t, "storage", c.method, c.target, c.body)
		assert.Equal(t, permissions.AccessControl, v.act.AccessLevel, c.name)
	}
	for _, c := range []struct {
		name, method, target, body string
		want                       permissions.AccessLevel
	}{
		{"other metadata", http.MethodPatch, "/storage/v1/b/bkt/o/obj", `{"metadata":{"a":"b"}}`, permissions.Write},
		{"an object holding the word", http.MethodPost, "/upload/storage/v1/b/bkt/o?uploadType=media&name=x", `{"acl":[]}`, permissions.Write},
		{"a bucket named acl", http.MethodDelete, "/storage/v1/b/acl", "", permissions.Destructive},
		{"an object named acl", http.MethodPatch, "/storage/v1/b/bkt/o/acl", `{"metadata":{}}`, permissions.Write},
	} {
		v := classifyBody(t, "storage", c.method, c.target, c.body)
		assert.Equal(t, c.want, v.act.AccessLevel, c.name)
	}
	assert.Equal(t, read, classifyBody(t, "storage", http.MethodGet, "/storage/v1/b/bkt/acl", "").kind, "a read of an ACL")

	multipart := func(metadata string) verdict {
		segs, why := checkPath(serviceNamed("storage"), "/upload/storage/v1/b/bkt/o")
		require.Empty(t, why)
		body := "--b\r\nContent-Type: application/json\r\n\r\n" + metadata + "\r\n--b\r\nContent-Type: text/plain\r\n\r\nacl\r\n--b--\r\n"
		return classify("storage", http.MethodPost, segs, url.Values{"uploadType": {"multipart"}},
			http.Header{"Content-Type": {`multipart/related; boundary=b`}}, []byte(body))
	}
	assert.Equal(t, permissions.AccessControl, multipart(`{"name":"x","acl":[]}`).act.AccessLevel, "an upload's metadata")
	assert.Equal(t, permissions.Write, multipart(`{"name":"x"}`).act.AccessLevel, "an upload whose data holds the word")
	// An upload whose metadata cannot be read is taken to set access.
	segs, why := checkPath(serviceNamed("storage"), "/upload/storage/v1/b/bkt/o")
	require.Empty(t, why)
	for name, body := range map[string]string{
		"no part":          "no boundary here",
		"a part cut short": "--b\r\nContent-Type: application/json\r\n\r\n{\"name\":\"x\"}",
	} {
		v := classify("storage", http.MethodPost, segs, url.Values{"uploadType": {"multipart"}},
			http.Header{"Content-Type": {`multipart/related; boundary=b`}}, []byte(body))
		assert.Equal(t, permissions.AccessControl, v.act.AccessLevel, name)
	}
}

func TestAccessControlChanges(t *testing.T) {
	for _, c := range []struct{ name, service, method, target string }{
		{"a project's setIamPolicy", "cloudresourcemanager", http.MethodPost, "/v3/projects/my-project:setIamPolicy"},
		{"an instance's setIamPolicy", "compute", http.MethodPost, project + "/zones/z/instances/i/setIamPolicy"},
		{"a bucket's IAM PUT", "storage", http.MethodPut, "/storage/v1/b/bkt/iam"},
		{"a service account's create", "iam", http.MethodPost, "/v1/projects/my-project/serviceAccounts"},
		{"a service account's delete", "iam", http.MethodDelete, "/v1/projects/my-project/serviceAccounts/sa"},
		{"a role's undelete", "iam", http.MethodPost, "/v1/projects/my-project/roles/r:undelete"},
		{"a policy's change", "iam", http.MethodPut, "/v2/policies/x/y/denypolicies/p"},
	} {
		v := classifyURL(t, c.service, c.method, c.target)
		assert.Equal(t, permissions.AccessControl, v.act.AccessLevel, c.name)
		assert.False(t, v.act.Confined, c.name)
	}
}

// setIamPolicy asks each time under every mode and every rule, and no
// answer to it writes a rule.
func TestOnlyAConfinedAccessControlActionIsGrantable(t *testing.T) {
	act := classifyURL(t, "cloudresourcemanager", http.MethodPost, "/v3/projects/my-project:setIamPolicy").act
	act.Principal, act.Target = permissions.Chat, permissions.GCP
	for _, mode := range []permissions.Mode{permissions.ModeAsk, permissions.ModeAuto} {
		for _, rules := range [][]permissions.Rule{nil, {permissions.GrantRule(act)}, {permissions.CommandRule(act)}} {
			v, _ := permissions.Policy{Mode: mode, Rules: rules}.Authorize(act)
			assert.Equal(t, permissions.Verdict{Effect: permissions.Ask}, v, mode)
		}
	}
}

// TestOrdinaryIsExactlyTheTable pins the changes a mode or a rule may allow.
func TestOrdinaryIsExactlyTheTable(t *testing.T) {
	assert.Equal(t, map[string]map[string][]string{
		"compute": {"instances": {"start", "reset", "setlabels"}},
	}, ordinary)
	for _, op := range []string{"start", "reset", "setLabels"} {
		v := classifyURL(t, "compute", http.MethodPost, project+"/zones/z/instances/vm/"+op)
		assert.Equal(t, permissions.Write, v.act.AccessLevel, op)
		assert.False(t, v.act.Once, op)
	}
}

func TestAChangeOffTheOrdinaryTableAsksOnce(t *testing.T) {
	for _, c := range []struct{ name, service, method, target string }{
		{"an instance's stop", "compute", http.MethodPost, project + "/zones/z/instances/vm/stop"},
		{"an instance's suspend", "compute", http.MethodPost, project + "/zones/z/instances/vm/suspend"},
		{"an instance group's resize", "compute", http.MethodPost, project + "/zones/z/instanceGroupManagers/g/resize"},
		{"a node pool's setSize", "container", http.MethodPost, "/v1/projects/my-project/locations/l/clusters/c/nodePools/np:setSize"},
		{"an instance's create", "compute", http.MethodPost, project + "/zones/z/instances"},
		{"start on another collection", "compute", http.MethodPost, project + "/zones/z/disks/d/start"},
		{"a log write", "logging", http.MethodPost, "/v2/projects/my-project/logs/l:write"},
	} {
		v := classifyURL(t, c.service, c.method, c.target)
		assert.Equal(t, permissions.Write, v.act.AccessLevel, c.name)
		assert.True(t, v.act.Once, c.name)
	}
}

// A change no table names asks each time under every mode and rule; read-only
// and a deny still stop it.
func TestAnUnnamedOperationAsksOnceAndIsNotGranted(t *testing.T) {
	act := classifyURL(t, "compute", http.MethodPost, project+"/zones/z/instances/vm/stop").act
	act.Principal, act.Target = permissions.Chat, permissions.GCP
	for _, mode := range []permissions.Mode{permissions.ModeAsk, permissions.ModeAuto} {
		for _, rules := range [][]permissions.Rule{nil, {permissions.GrantRule(act)}, {permissions.CommandRule(act)}} {
			v, _ := permissions.Policy{Mode: mode, Rules: rules}.Authorize(act)
			assert.Equal(t, permissions.Verdict{Effect: permissions.Ask}, v, mode)
		}
	}
	v, _ := permissions.Policy{Mode: permissions.ModeReadOnly}.Authorize(act)
	assert.Equal(t, permissions.Deny, v.Effect)
}

// Each refusal of a request whose answer is a credential is the reach level;
// a refusal for anything else has none. The texts are as before.
func TestEachCredentialRefusalIsReach(t *testing.T) {
	for _, target := range []string{
		"/v1/projects/p/serviceAccounts/sa@p.iam.gserviceaccount.com/keys",
		"/v1/projects/-/serviceAccounts/sa:signBlob",
		"/v1/projects/-/serviceAccounts/sa:generateAccessToken",
		"/v1/projects/p/locations/global/oauthClients/c/credentials",
	} {
		v := classifyURL(t, "iam", http.MethodPost, target)
		assert.Equal(t, refused, v.kind, target)
		assert.Equal(t, permissions.Reach, v.level, target)
		assert.Equal(t, refusedCredential, v.why, target)
	}
	v := classifyURL(t, "storage", http.MethodPost, "/upload/storage/v1/b/bkt/o?uploadType=resumable")
	assert.Equal(t, refused, v.kind)
	assert.Empty(t, v.level)
	assert.Equal(t, refusedResumable, v.why)
}
