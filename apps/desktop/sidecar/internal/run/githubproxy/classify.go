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
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

// verdict is what the classifier makes of a request: refused with refusal,
// or its action, whose level is Read for a read. A credential's refusal
// carries the reach level; any other refusal none.
type verdict struct {
	refusal string
	act     permissions.Action
}

// A path pattern matches segment by segment: * is any one segment, and a
// trailing /... is the path itself and anything below it.

// credentialPaths are the requests whose answer is a credential.
var credentialPaths = []string{
	"/app/installations/*/access_tokens",
	"/app-manifests/*/conversions",
	"/applications/*/token/...",
	"/copilot_internal/...",
	"/repos/*/*/actions/runners/registration-token",
	"/repos/*/*/actions/runners/remove-token",
	"/repos/*/*/actions/runners/generate-jitconfig",
	"/orgs/*/actions/runners/registration-token",
	"/orgs/*/actions/runners/remove-token",
	"/orgs/*/actions/runners/generate-jitconfig",
	"/enterprises/*/actions/runners/registration-token",
	"/enterprises/*/actions/runners/remove-token",
	"/enterprises/*/actions/runners/generate-jitconfig",
}

// dataPaths are the reads that ask: those whose answer is a signed link to
// stored data, and the metadata GitHub documents as able to hold a secret.
var dataPaths = []string{
	"/repos/*/*/actions/runs/*/logs",
	"/repos/*/*/actions/runs/*/attempts/*/logs",
	"/repos/*/*/actions/jobs/*/logs",
	"/repos/*/*/actions/artifacts/*/*",
	"/repos/*/*/tarball/...",
	"/repos/*/*/zipball/...",
	"/orgs/*/migrations/*/archive",
	"/user/migrations/*/archive",
	"/repos/*/*/secret-scanning/...",
	"/orgs/*/secret-scanning/...",
	"/enterprises/*/secret-scanning/...",
	"/repos/*/*/hooks/...",
	"/orgs/*/hooks/...",
	"/app/hook/config/...",
	"/app/hook/deliveries/...",
	"/repos/*/*/actions/variables/...",
	"/repos/*/*/actions/organization-variables/...",
	"/orgs/*/actions/variables/...",
	"/repos/*/*/environments/*/variables/...",
}

// releaseAsset is a release asset, a data read when Accept asks for its bytes.
const releaseAsset = "/repos/*/*/releases/assets/*"

// destructivePaths are the deletes that are destructive.
var destructivePaths = []string{"/repos/*/*", "/repos/*/*/git/refs/..."}

// permissionsWords are what a change under a repository or an organization
// changes who may do what through, or what sees every request.
var permissionsWords = []string{
	"collaborators", "outside_collaborators", "teams", "members", "memberships", "invitations", "keys", "hooks",
	"rulesets", "environments", "organization-roles", "security-managers",
}

// spanningPaths are an owner's data reads that gather its repositories':
// their alerts and their archives.
var spanningPaths = []string{"/orgs/*/secret-scanning/...", "/orgs/*/migrations/*/archive"}

// accessControlPaths are the changes that are an access control change: under
// permissionsWords, a branch's protection, the Actions permissions, a
// repository's transfer to another owner, and the user's own keys.
var accessControlPaths = func() []string {
	paths := []string{
		"/repos/*/*/actions/permissions/...", "/orgs/*/actions/permissions/...", "/repos/*/*/transfer",
		"/user/keys/...", "/user/gpg_keys/...", "/user/ssh_signing_keys/...",
	}
	for _, w := range permissionsWords {
		paths = append(paths, "/repos/*/*/"+w+"/...", "/orgs/*/"+w+"/...")
	}
	return paths
}()

// ordinaryRow is one change a mode or a rule may allow: a method and a path
// pattern on the API host, matched segment by segment and no deeper.
type ordinaryRow struct{ method, path string }

// ordinary are the changes a mode or a rule may allow. Any other change asks
// each time. Adding a row is a security change.
var ordinary = []ordinaryRow{
	{http.MethodPost, "/repos/*/*/issues"},
	{http.MethodPost, "/repos/*/*/issues/*/comments"},
	{http.MethodPost, "/repos/*/*/issues/*/labels"},
	{http.MethodPost, "/repos/*/*/pulls"},
	{http.MethodPost, "/repos/*/*/pulls/*/comments"},
	{http.MethodPatch, "/repos/*/*/issues/*"},
	{http.MethodPatch, "/repos/*/*/pulls/*"},
}

// accessFields are the fields of a change's body that say who may do what,
// by the path the change is to: a repository's visibility, and whether a pull
// request's base maintainers may push to its branch.
var accessFields = []struct {
	path   string
	fields []string
}{
	{"/repos/*/*", []string{"private", "visibility"}},
	{"/repos/*/*/pulls/*", []string{"maintainer_can_modify"}},
}

// withBody is v once the change's body is read: an access control change when
// the body sets one of accessFields for its path. A body that is not a JSON
// object sets none; GitHub refuses it.
func (v verdict) withBody(path string, body []byte) verdict {
	if v.act.AccessLevel != permissions.Write {
		return v
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return v
	}
	segs := segmentsOf(path)
	for _, a := range accessFields {
		if matches(a.path, segs) && slices.ContainsFunc(a.fields, func(f string) bool { _, ok := fields[f]; return ok }) {
			v.act.AccessLevel, v.act.Once = permissions.AccessControl, false
			return v
		}
	}
	return v
}

// classifyAPI is how the API host takes a request to path other than
// GraphQL's, by its method and, for a release asset, its Accept. The levels
// go reach, destructive, permissions, data read, read, write; a write
// ordinary does not name asks each time.
func classifyAPI(method, path, accept string) verdict {
	segs := segmentsOf(path)
	raw := strings.Split(strings.ToLower(strings.TrimPrefix(path, "/")), "/")
	act := apiAction(method, raw, segs)
	switch {
	case matchesAny(credentialPaths, segs):
		act.AccessLevel = permissions.Reach
		return verdict{refusal: refusedCredential, act: act}
	case method == http.MethodGet || method == http.MethodHead:
		act.AccessLevel = permissions.Read
		if matchesAny(dataPaths, segs) || matches(releaseAsset, segs) && strings.Contains(strings.ToLower(accept), "application/octet-stream") {
			act.AccessLevel = permissions.DataRead
			act.Spans = matchesAny(spanningPaths, segs)
		}
	case method == http.MethodDelete && matchesAny(destructivePaths, segs):
		act.AccessLevel = permissions.Destructive
	case method != http.MethodPost && method != http.MethodPut && method != http.MethodPatch && method != http.MethodDelete:
		return verdict{refusal: refusedMethod}
	case matchesAny(accessControlPaths, segs) || protectsABranch(segs):
		act.AccessLevel = permissions.AccessControl
	default:
		act.AccessLevel = permissions.Write
		act.Once = !slices.ContainsFunc(ordinary, func(o ordinaryRow) bool { return o.method == method && matches(o.path, raw) })
	}
	return verdict{act: act}
}

// protectsABranch is whether segs is under a branch's protection.
func protectsABranch(segs []string) bool {
	return len(segs) > 5 && matches("/repos/*/*/branches/...", segs) && slices.Contains(segs[5:], "protection")
}

// apiAction is the action an API request names, from its path's raw segments
// and segs as the lists read them: its owner and repository as written by
// name, in lower case, the method, and the resource, the first segment after
// the repository, else the first. A route by id and one naming no owner have
// no scope: an id is an alias Kubetail cannot read as an account.
func apiAction(method string, raw, segs []string) permissions.Action {
	act := permissions.Action{Service: "api", Operation: strings.ToLower(method), Resource: segs[0]}
	switch {
	case len(segs) >= 4 && segs[0] == "repos":
		act.Resource = segs[3]
	case len(segs) >= 3 && (segs[0] == "orgs" || segs[0] == "users"):
		// An owner's resource is named as a repository's is, so one rule names
		// both: secret-scanning, members.
		act.Resource = segs[2]
	}
	switch {
	case len(raw) >= 3 && raw[0] == "repos":
		act.Scope, act.AccountName = raw[1]+"/"+raw[2], raw[1]
	case len(raw) >= 2 && (raw[0] == "orgs" || raw[0] == "users"):
		act.Scope, act.AccountName = raw[1], raw[1]
	}
	return act
}

// segmentsOf is path's segments as the lists read them: in lower case, so no
// case variant of a route escapes them. GitHub serves every
// /repos/{owner}/{repo} route as /repositories/{id} too, and every
// /orgs/{org} route as /organizations/{id}, so those read as their form by
// name.
func segmentsOf(path string) []string {
	segs := strings.Split(strings.ToLower(strings.TrimPrefix(path, "/")), "/")
	if len(segs) >= 2 {
		switch segs[0] {
		case "repositories":
			return slices.Concat([]string{"repos", segs[1], segs[1]}, segs[2:])
		case "organizations":
			return slices.Concat([]string{"orgs", segs[1]}, segs[2:])
		}
	}
	return segs
}

func matchesAny(patterns []string, segs []string) bool {
	return slices.ContainsFunc(patterns, func(p string) bool { return matches(p, segs) })
}

// matches is whether segs is pattern's path.
func matches(pattern string, segs []string) bool {
	want := strings.Split(strings.TrimPrefix(pattern, "/"), "/")
	below := want[len(want)-1] == "..."
	if below {
		want = want[:len(want)-1]
		if len(segs) < len(want) {
			return false
		}
		segs = segs[:len(want)]
	}
	if len(segs) != len(want) {
		return false
	}
	for i, w := range want {
		if w != "*" && w != segs[i] {
			return false
		}
	}
	return true
}

// gitService is one of Git's smart HTTP requests.
type gitService int

const (
	uploadPackRefs  gitService = iota // GET info/refs?service=git-upload-pack
	uploadPack                        // POST git-upload-pack
	receivePackRefs                   // GET info/refs?service=git-receive-pack
	receivePack                       // POST git-receive-pack
)

// gitPath is a smart HTTP request's path: the owner, the repository with
// any .git, and the request.
var gitPath = regexp.MustCompile(`^/([A-Za-z0-9-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?/(info/refs|git-upload-pack|git-receive-pack)$`)

// gitAction is a push's action: its repository, in lower case, and a
// destructive change when it deletes a ref. No push is on ordinary, so each
// asks each time.
func gitAction(repo string, p push) permissions.Action {
	repo = strings.ToLower(repo)
	owner, _, _ := strings.Cut(repo, "/")
	act := permissions.Action{
		Scope: repo, AccountName: owner, Service: "git", Operation: "push", Resource: "git-receive-pack",
		AccessLevel: permissions.Write, Once: true,
	}
	if p.deletes() {
		act.AccessLevel, act.Once = permissions.Destructive, false
	}
	return act
}

// classifyGit is the repository, as owner/repo, and the service of a request
// to the Git host, if it is one of Git's smart HTTP requests.
func classifyGit(method, path, query string) (repo string, svc gitService, ok bool) {
	m := gitPath.FindStringSubmatch(path)
	if m == nil {
		return "", 0, false
	}
	repo = m[1] + "/" + m[2]
	switch {
	case m[3] == "info/refs" && method == http.MethodGet && query == "service=git-upload-pack":
		return repo, uploadPackRefs, true
	case m[3] == "info/refs" && method == http.MethodGet && query == "service=git-receive-pack":
		return repo, receivePackRefs, true
	case m[3] == "git-upload-pack" && method == http.MethodPost && query == "":
		return repo, uploadPack, true
	case m[3] == "git-receive-pack" && method == http.MethodPost && query == "":
		return repo, receivePack, true
	}
	return "", 0, false
}
