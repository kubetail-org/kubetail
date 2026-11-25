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

package githubproxy

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

// byID is path with its /repos/o/r or /orgs/o written by id, as GitHub
// serves it too; "" for a path with neither.
func byID(path string) string {
	switch {
	case strings.HasPrefix(path, "/repos/o/r"):
		return "/repositories/42" + strings.TrimPrefix(path, "/repos/o/r")
	case strings.HasPrefix(path, "/orgs/o"):
		return "/organizations/7" + strings.TrimPrefix(path, "/orgs/o")
	}
	return ""
}

// classified is what classifyAPI makes of method and path, with accept, in
// one word: refused, or the access level.
func classified(method, path, accept string) string {
	v := classifyAPI(method, path, accept)
	if v.refusal != "" {
		return "refused"
	}
	return string(v.act.AccessLevel)
}

func TestEveryRequestIsClassified(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ method, path, accept, want string }{
		{http.MethodGet, "/user", "", "read"},
		{http.MethodHead, "/repos/o/r", "", "read"},
		{http.MethodGet, "/repos/o/r/contents/README.md", "", "read"},
		{http.MethodGet, "/repos/o/r/actions/secrets", "", "read"},
		{http.MethodGet, "/repos/o/r/releases/assets/9", "application/json", "read"},
		{http.MethodGet, "/repos/o/r/releases/assets/9", "application/octet-stream", "data-read"},
		{http.MethodGet, "/repos/o/r/actions/runs/1/logs", "", "data-read"},
		{http.MethodPost, "/repos/o/r/issues", "", "write"},
		{http.MethodPut, "/repos/o/r/contents/a", "", "write"},
		{http.MethodPatch, "/repos/o/r", "", "write"},
		{http.MethodDelete, "/repos/o/r/issues/comments/1", "", "write"},
		{http.MethodDelete, "/repos/o/r", "", "destructive"},
		{http.MethodDelete, "/repos/o/r/git/refs/heads/main", "", "destructive"},
		{http.MethodOptions, "/user", "", "refused"},
		{"PROPFIND", "/user", "", "refused"},
		{http.MethodPost, "/app/installations/1/access_tokens", "", "refused"},
	} {
		assert.Equal(t, c.want, classified(c.method, c.path, c.accept), "%s %s", c.method, c.path)
	}
}

// A request names a repository or an org by id as GitHub serves it, and
// lands in the row of its form by name.
func TestAPathByIDIsClassifiedAsItsRoute(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/repos/o/r/hooks"},
		{http.MethodGet, "/repos/o/r/tarball/main"},
		{http.MethodPost, "/repos/o/r/actions/runners/registration-token"},
		{http.MethodPost, "/orgs/o/actions/runners/remove-token"},
		{http.MethodGet, "/orgs/o/actions/variables"},
		{http.MethodDelete, "/repos/o/r"},
		{http.MethodDelete, "/repos/o/r/git/refs/tags/v1"},
		{http.MethodGet, "/repos/o/r/pulls"},
	} {
		assert.Equal(t, classified(c.method, c.path, ""), classified(c.method, byID(c.path), ""), "%s %s", c.method, byID(c.path))
	}
	assert.Equal(t, "read", classified(http.MethodGet, "/repositories/42", ""))
	assert.Equal(t, "refused", classified(http.MethodPost, "/Repositories/42/actions/runners/registration-token", ""), "the alias in any case")
	assert.Equal(t, "read", classified(http.MethodGet, "/repositories", ""), "the list of every repository names none")
}

func TestACredentialPathIsRefused(t *testing.T) {
	t.Parallel()
	paths := []string{
		"/app/installations/1/access_tokens",
		"/app-manifests/abc/conversions",
		"/applications/Iv1/token",
		"/applications/Iv1/token/scoped",
		"/copilot_internal",
		"/copilot_internal/v2/token",
	}
	for _, scope := range []string{"/repos/o/r", "/orgs/o", "/enterprises/e"} {
		for _, name := range []string{"registration-token", "remove-token", "generate-jitconfig"} {
			paths = append(paths, scope+"/actions/runners/"+name)
		}
	}
	for _, path := range paths {
		for _, p := range []string{path, byID(path)} {
			if p == "" {
				continue
			}
			for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
				v := classifyAPI(method, p, "")
				assert.Equal(t, refusedCredential, v.refusal, "%s %s", method, p)
			}
		}
	}
	assert.Equal(t, "read", classified(http.MethodGet, "/applications/Iv1/grants", ""))
	assert.Equal(t, "read", classified(http.MethodGet, "/repos/o/r/actions/runners", ""))
}

// The lists match a path in any case, so a case variant of a route, should
// GitHub serve one, is never read unasked.
func TestAPathIsMatchedInAnyCase(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "refused", classified(http.MethodPost, "/repos/o/r/actions/runners/Registration-Token", ""))
	assert.Equal(t, "refused", classified(http.MethodGet, "/COPILOT_INTERNAL/v2/token", ""))
	assert.Equal(t, "data-read", classified(http.MethodGet, "/repos/o/r/Hooks/1/config", ""))
	assert.Equal(t, "data-read", classified(http.MethodGet, "/REPOS/o/r/actions/Variables", ""))
	assert.Equal(t, "destructive", classified(http.MethodDelete, "/Repos/o/r", ""))
	assert.Equal(t, "data-read", classified(http.MethodGet, "/repos/o/r/releases/assets/9", "Application/Octet-Stream"), "the media type in any case")
}

func TestADataPathAsks(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/repos/o/r/actions/runs/1/logs",
		"/repos/o/r/actions/runs/1/attempts/2/logs",
		"/repos/o/r/actions/jobs/3/logs",
		"/repos/o/r/actions/artifacts/4/zip",
		"/repos/o/r/tarball",
		"/repos/o/r/tarball/main",
		"/repos/o/r/zipball/v1",
		"/orgs/o/migrations/5/archive",
		"/user/migrations/6/archive",
		"/repos/o/r/secret-scanning/alerts",
		"/orgs/o/secret-scanning/alerts",
		"/enterprises/e/secret-scanning/alerts",
		"/repos/o/r/hooks",
		"/repos/o/r/hooks/1/deliveries/2",
		"/orgs/o/hooks/1/config",
		"/app/hook/config",
		"/app/hook/deliveries/3",
		"/repos/o/r/actions/variables",
		"/orgs/o/actions/variables/NAME",
		"/repos/o/r/actions/organization-variables",
		"/repos/o/r/actions/organization-variables/NAME",
		"/repos/o/r/environments/prod/variables",
	} {
		for _, p := range []string{path, byID(path)} {
			if p == "" {
				continue
			}
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				assert.Equal(t, "data-read", classified(method, p, ""), "%s %s", method, p)
			}
		}
	}
	assert.Equal(t, "read", classified(http.MethodGet, "/repos/o/r/releases/assets/9", ""), "an asset's metadata")
	assert.Equal(t, "data-read", classified(http.MethodGet, "/repos/o/r/releases/assets/9", "application/octet-stream"))
	assert.Equal(t, "data-read", classified(http.MethodGet, byID("/repos/o/r/releases/assets/9"), "application/octet-stream"))
	assert.Equal(t, "read", classified(http.MethodGet, "/repos/o/r/actions/runs/1", ""), "a run's metadata")
	assert.Equal(t, "access-control", classified(http.MethodPost, "/repos/o/r/hooks", ""), "a change to a hook is who sees every request")
}

func TestOnlyGitsSmartProtocolIsAGitRoute(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		method, path, query string
		repo                string
		svc                 gitService
	}{
		{http.MethodGet, "/o/r/info/refs", "service=git-upload-pack", "o/r", uploadPackRefs},
		{http.MethodGet, "/o/r.git/info/refs", "service=git-upload-pack", "o/r", uploadPackRefs},
		{http.MethodPost, "/o/r/git-upload-pack", "", "o/r", uploadPack},
		{http.MethodGet, "/o/r.wiki.git/info/refs", "service=git-receive-pack", "o/r.wiki", receivePackRefs},
		{http.MethodPost, "/my-org/my_repo.js/git-receive-pack", "", "my-org/my_repo.js", receivePack},
	} {
		repo, svc, ok := classifyGit(c.method, c.path, c.query)
		assert.True(t, ok, "%s %s?%s", c.method, c.path, c.query)
		assert.Equal(t, c.repo, repo)
		assert.Equal(t, c.svc, svc)
	}
	for _, c := range []struct{ method, path, query string }{
		{http.MethodGet, "/o/r/info/refs", ""},
		{http.MethodGet, "/o/r/info/refs", "service=git-upload-archive"},
		{http.MethodGet, "/o/r/info/refs", "service=git-upload-pack&x=1"},
		{http.MethodPost, "/o/r/info/refs", "service=git-upload-pack"},
		{http.MethodGet, "/o/r/git-upload-pack", ""},
		{http.MethodPost, "/o/r/git-upload-pack", "x=1"},
		{http.MethodGet, "/o/r/HEAD", ""},
		{http.MethodGet, "/o/r/objects/info/packs", ""},
		{http.MethodPost, "/o/r.git/info/lfs/objects/batch", ""},
		{http.MethodGet, "/o/r", ""},
		{http.MethodGet, "/o/r/pulls", ""},
		{http.MethodPost, "/o/git-receive-pack", ""},
		{http.MethodPost, "/o/r/x/git-receive-pack", ""},
	} {
		_, _, ok := classifyGit(c.method, c.path, c.query)
		assert.False(t, ok, "%s %s?%s", c.method, c.path, c.query)
	}
}

func TestEveryFieldIsFilled(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		method, path                          string
		level                                 permissions.AccessLevel
		scope, account, service, op, resource string
		once                                  bool
	}{
		{http.MethodPost, "/repos/o/r/issues", permissions.Write, "o/r", "o", "api", "post", "issues", false},
		{http.MethodPatch, "/repos/o/r/pulls/3", permissions.Write, "o/r", "o", "api", "patch", "pulls", false},
		{http.MethodPut, "/repos/o/r/contents/a", permissions.Write, "o/r", "o", "api", "put", "contents", true},
		{http.MethodPatch, "/repos/o/r", permissions.Write, "o/r", "o", "api", "patch", "repos", true},
		{http.MethodDelete, "/repos/o/r", permissions.Destructive, "o/r", "o", "api", "delete", "repos", false},
		{http.MethodDelete, "/repos/o/r/git/refs/heads/main", permissions.Destructive, "o/r", "o", "api", "delete", "git", false},
		{http.MethodGet, "/repos/o/r/actions/runs/1/logs", permissions.DataRead, "o/r", "o", "api", "get", "actions", false},
		{http.MethodPut, "/orgs/o/memberships/u", permissions.AccessControl, "o", "o", "api", "put", "memberships", false},
		{http.MethodPost, "/user/keys", permissions.AccessControl, "", "", "api", "post", "user", false},
		{http.MethodPost, "/gists", permissions.Write, "", "", "api", "post", "gists", true},
		{http.MethodPut, "/user/starred/o/r", permissions.Write, "", "", "api", "put", "user", true},
	} {
		v := classifyAPI(c.method, c.path, "")
		want := permissions.Action{
			Scope: c.scope, AccountName: c.account, Service: c.service, Operation: c.op, Resource: c.resource,
			AccessLevel: c.level, Once: c.once,
		}
		assert.Empty(t, v.refusal, "%s %s", c.method, c.path)
		assert.Equal(t, want, v.act, "%s %s", c.method, c.path)
	}
	act := gitAction("O/R", push{})
	assert.Equal(t, permissions.Action{
		Scope: "o/r", AccountName: "o", Service: "git", Operation: "push", Resource: "git-receive-pack",
		AccessLevel: permissions.Write, Once: true,
	}, act)
	g, ok := readGraphQL([]byte(`{"query":"mutation { addStar(input: {}) { clientMutationId } }"}`))
	require.True(t, ok)
	assert.Equal(t, permissions.Action{
		Service: "graphql", Operation: "mutation", Resource: "graphql", AccessLevel: permissions.Write, Once: true,
	}, g.action())
}

// A change whose body sets who may do what is an access control change,
// though its route is ordinary: a pull request's maintainer edits and a
// repository's visibility, by name or by id.
func TestABodyThatChangesAccessIsAccessControl(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPatch, "/repos/o/r/pulls/1", `{"maintainer_can_modify":true}`},
		{http.MethodPatch, "/repos/o/r", `{"private":false}`},
		{http.MethodPatch, "/repos/o/r", `{"visibility":"public"}`},
		{http.MethodPatch, "/repositories/42", `{"visibility":"public"}`},
		{http.MethodPost, "/repos/o/r/transfer", `{"new_owner":"them"}`},
	} {
		v := classifyAPI(c.method, c.path, "").withBody(c.path, []byte(c.body))
		assert.Equal(t, permissions.AccessControl, v.act.AccessLevel, "%+v", c)
	}
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPatch, "/repos/o/r/pulls/1", `{"title":"t"}`},
		{http.MethodPatch, "/repos/o/r", `{"description":"d"}`},
		{http.MethodPatch, "/repos/o/r/issues/1", `{"private":true}`},
		{http.MethodPatch, "/repos/o/r/pulls/1", `not json`},
	} {
		v := classifyAPI(c.method, c.path, "").withBody(c.path, []byte(c.body))
		assert.Equal(t, permissions.Write, v.act.AccessLevel, "%+v", c)
	}
}

// An owner's alerts and archives gather its repositories', so a rule for one
// repository decides them; the owner's and a repository's alerts are one
// resource.
func TestAnOwnersAggregateDataReadSpansItsRepositories(t *testing.T) {
	t.Parallel()
	org := classifyAPI(http.MethodGet, "/orgs/o/secret-scanning/alerts", "").act
	repo := classifyAPI(http.MethodGet, "/repos/o/r/secret-scanning/alerts", "").act
	assert.True(t, org.Spans)
	assert.Equal(t, repo.Resource, org.Resource)
	assert.True(t, classifyAPI(http.MethodGet, "/orgs/o/migrations/1/archive", "").act.Spans)
	assert.False(t, classifyAPI(http.MethodGet, "/orgs/o/hooks/1", "").act.Spans, "the owner's own webhooks")
	assert.False(t, repo.Spans)

	deny := permissions.Rule{ID: "d", Effect: permissions.Deny, AccessLevel: permissions.DataRead, Principal: "*", Target: "github",
		Scope: "o/r", Service: "*", Operation: "*", Resource: "secret-scanning"}
	org.Principal, org.Target = permissions.Chat, permissions.GitHub
	v, _ := permissions.Policy{Mode: permissions.ModeAuto, Rules: []permissions.Rule{deny}}.Authorize(org)
	assert.Equal(t, permissions.Denied, v.Outcome())
}

func TestAScopeIsLowerCase(t *testing.T) {
	t.Parallel()
	v := classifyAPI(http.MethodPost, "/repos/Kubetail-Org/Kubetail/issues", "")
	assert.Equal(t, "kubetail-org/kubetail", v.act.Scope)
	assert.Equal(t, "kubetail-org", v.act.AccountName)
	v = classifyAPI(http.MethodPut, "/orgs/Kubetail-Org/members/u", "")
	assert.Equal(t, "kubetail-org", v.act.Scope)
	assert.Equal(t, "kubetail-org/kubetail", gitAction("Kubetail-Org/Kubetail", push{}).Scope)
}

func TestAnAliasedOrForeignScopeIsEmpty(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/repositories/42/issues",
		"/organizations/7/members/u",
		"/user/repos",
		"/gists",
		"/enterprises/e/actions/permissions",
		"/repos/o",
	} {
		v := classifyAPI(http.MethodPost, path, "")
		assert.Empty(t, v.act.Scope, path)
		assert.Empty(t, v.act.AccountName, path)
	}
	g, ok := readGraphQL([]byte(`{"query":"mutation { addStar(input: {starrableId: \"x\"}) { clientMutationId } }"}`))
	require.True(t, ok)
	assert.Empty(t, g.action().Scope, "GraphQL names its objects in its body")
}

func TestAccessControlChanges(t *testing.T) {
	t.Parallel()
	var paths []string
	for _, under := range []string{
		"collaborators/u", "outside_collaborators/u", "teams/t", "members/u", "memberships/u", "invitations/1",
		"keys", "hooks/1", "rulesets/2", "environments/prod", "organization-roles/3", "security-managers/teams/t",
		"actions/permissions", "actions/permissions/workflow",
	} {
		paths = append(paths, "/repos/o/r/"+under, "/orgs/o/"+under)
	}
	paths = append(paths, "/repos/o/r/branches/main/protection", "/repos/o/r/branches/main/protection/required_status_checks",
		"/user/keys", "/user/keys/1", "/user/gpg_keys", "/user/ssh_signing_keys/2")
	for _, path := range paths {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			assert.Equal(t, "access-control", classified(method, path, ""), "%s %s", method, path)
		}
		if p := byID(path); p != "" {
			assert.Equal(t, "access-control", classified(http.MethodPut, p, ""), p)
		}
	}
	assert.Equal(t, "read", classified(http.MethodGet, "/orgs/o/members", ""), "a read is a read")
	assert.Equal(t, "data-read", classified(http.MethodGet, "/repos/o/r/hooks/1/config", ""))
	v := classifyAPI(http.MethodPut, "/repos/o/r/collaborators/u", "")
	assert.False(t, v.act.Confined, "no GitHub access control change is confined")
}

// ordinaryCases are every change the ordinary table names, one row each.
var ordinaryCases = []struct{ method, path string }{
	{http.MethodPost, "/repos/o/r/issues"},
	{http.MethodPost, "/repos/o/r/issues/1/comments"},
	{http.MethodPost, "/repos/o/r/issues/1/labels"},
	{http.MethodPost, "/repos/o/r/pulls"},
	{http.MethodPost, "/repos/o/r/pulls/2/comments"},
	{http.MethodPatch, "/repos/o/r/issues/1"},
	{http.MethodPatch, "/repos/o/r/pulls/2"},
}

func TestOrdinaryIsExactlyTheTable(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []ordinaryRow{
		{http.MethodPost, "/repos/*/*/issues"},
		{http.MethodPost, "/repos/*/*/issues/*/comments"},
		{http.MethodPost, "/repos/*/*/issues/*/labels"},
		{http.MethodPost, "/repos/*/*/pulls"},
		{http.MethodPost, "/repos/*/*/pulls/*/comments"},
		{http.MethodPatch, "/repos/*/*/issues/*"},
		{http.MethodPatch, "/repos/*/*/pulls/*"},
	}, ordinary)
	for _, c := range ordinaryCases {
		v := classifyAPI(c.method, c.path, "")
		assert.Equal(t, permissions.Write, v.act.AccessLevel, c.path)
		assert.False(t, v.act.Once, "%s %s", c.method, c.path)
	}
}

func TestAChangeOffTheOrdinaryTableAsksOnce(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/repos/o/r/pulls/2/reviews"},
		{http.MethodPut, "/repos/o/r/pulls/2/merge"},
		{http.MethodPost, "/repos/o/r/issues/1/comments/2/reactions"},
		{http.MethodPut, "/repos/o/r/issues/1"},
		{http.MethodPatch, "/repos/o/r/issues"},
		{http.MethodPost, "/repos/o/r/dispatches"},
		{http.MethodPost, "/repositories/42/issues"},
		{http.MethodPost, "/repos/o/r/actions/workflows/1/dispatches"},
	} {
		v := classifyAPI(c.method, c.path, "")
		assert.True(t, v.act.Once, "%s %s", c.method, c.path)
	}
	assert.True(t, gitAction("o/r", push{}).Once, "a push")
	g, ok := readGraphQL([]byte(`{"query":"mutation { addComment(input: {}) { clientMutationId } }"}`))
	require.True(t, ok)
	assert.True(t, g.action().Once, "a GraphQL mutation")
}

func TestEachCredentialRefusalIsReach(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/app/installations/1/access_tokens", "/repos/o/r/actions/runners/registration-token", "/copilot_internal/v2/token"} {
		v := classifyAPI(http.MethodPost, path, "")
		assert.Equal(t, refusedCredential, v.refusal, path)
		assert.Equal(t, permissions.Reach, v.act.AccessLevel, path)
		act := v.act
		act.Principal, act.Target = permissions.Chat, permissions.GitHub
		allow := permissions.Rule{Effect: permissions.Allow, AccessLevel: permissions.Reach, Principal: "*", Target: "*", Scope: "*", Service: "*", Operation: "*", Resource: "*"}
		got, _ := permissions.Policy{Mode: permissions.ModeAuto, Rules: []permissions.Rule{allow}}.Authorize(act)
		assert.Equal(t, permissions.Deny, got.Effect, path)
	}
	v := classifyAPI(http.MethodOptions, "/user", "")
	assert.Equal(t, refusedMethod, v.refusal)
	assert.Empty(t, v.act.AccessLevel, "a refusal for any other reason has no level")
}

func TestAReadIsClassifiedAsToday(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/user", "/repos/o/r/contents/README.md", "/repositories/42", "/orgs/o/members", "/repos/o/r/actions/runs/1"} {
		assert.Equal(t, "read", classified(http.MethodGet, path, ""), path)
	}
	g, ok := readGraphQL([]byte(`{"query":"query { viewer { login } }"}`))
	require.True(t, ok)
	assert.Equal(t, permissions.Read, g.action().AccessLevel)
}

func TestAnUnnamedOperationAsksOnceAndIsNotGranted(t *testing.T) {
	t.Parallel()
	v := classifyAPI(http.MethodPost, "/repos/o/r/transfer", "")
	act := v.act
	act.Principal, act.Target = permissions.Chat, permissions.GitHub
	for _, mode := range []permissions.Mode{permissions.ModeAsk, permissions.ModeAuto} {
		for _, rules := range [][]permissions.Rule{nil, {permissions.GrantRule(act)}, {permissions.CommandRule(act)}} {
			got, _ := permissions.Policy{Mode: mode, Rules: rules}.Authorize(act)
			assert.Equal(t, permissions.Verdict{Effect: permissions.Ask}, got, "%s", mode)
		}
	}
	got, _ := permissions.Policy{Mode: permissions.ModeReadOnly}.Authorize(act)
	assert.Equal(t, permissions.Deny, got.Effect)
}
