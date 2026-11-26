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

//go:build !windows

package bash

import (
	"encoding/base64"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http/httpproxy"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/githubproxy"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/gcpauth"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// upstreamsOf reaches srv as both of GitHub's origins.
func upstreamsOf(srv *httptest.Server) githubproxy.Upstreams {
	return githubproxy.Upstreams{Git: srv.URL, API: srv.URL, Client: srv.Client()}
}

// githubTool is a tool on this machine's sandbox with GitHub's cloud platform over
// srv switched on, and name on its PATH.
func githubTool(t *testing.T, srv *httptest.Server, name string) *Tool {
	t.Helper()
	tl := confiningTool(t)
	withTool(t, tl, name)
	tl.cloud = Cloud{
		Platforms: []CloudPlatform{newTestGitHub(upstreamsOf(srv))},
		Switches:  switchesOn(permissions.GitHub).read,
	}
	return tl
}

// A sandboxed run with network off clones a github.com repository through
// the proxy, unasked, by its HTTPS and its SSH name.
func TestASandboxedCloneReachesGitHub(t *testing.T) {
	origin, _ := testutil.GitOrigin(t)
	a := newAnsweringAsker()
	text, isError := githubTool(t, origin, "git").Run(t.Context(), chatRuntime(t, session.Chat, a),
		command("git clone -q https://github.com/o/r.git && git -C r log --format=%s && "+
			"git clone -q git@github.com:o/r.git ssh && echo ssh too && git clone -q http://github.com/o/r.git plain && echo http too"))
	require.False(t, isError, text)
	assert.Equal(t, "seeded\nssh too\nhttp too\n", text)
	assert.Empty(t, a.asked)
}

// pushLine clones o/r, commits "pushed" and pushes it, printing git's exit
// code. The commit runs no auto maintenance, whose detach fails in macOS's
// sandbox.
const pushLine = "git clone -q https://github.com/o/r.git && cd r && " +
	"git -c user.name=k -c user.email=k@example.com -c maintenance.auto=false commit -q --allow-empty -m pushed && " +
	"git push -q origin HEAD:main; echo exit $?"

// runAsked runs line in a chat whose asker answers its one request with
// approved, and answers the request and the command's output.
func runAsked(t *testing.T, tl *Tool, line string, approved bool) (summary, text string) {
	t.Helper()
	a := newAnsweringAsker()
	type result struct {
		text    string
		isError bool
	}
	done := make(chan result, 1)
	go func() {
		text, isError := tl.Run(t.Context(), chatRuntime(t, session.Chat, a), command(line))
		done <- result{text, isError}
	}()
	asked := testutil.Recv(t, a.asked, "the push")
	a.answers <- approved
	got := testutil.Recv(t, done, "the command")
	require.False(t, got.isError, got.text)
	require.NotNil(t, asked.Cloud)
	assert.Equal(t, permissions.GitHub, asked.Action.Target)
	return asked.Action.Summary, got.text
}

func TestASandboxedPushAsksAndRuns(t *testing.T) {
	origin, repo := testutil.GitOrigin(t)
	summary, text := runAsked(t, githubTool(t, origin, "git"), pushLine, true)
	assert.Equal(t, "GitHub: push to o/r", summary)
	assert.Equal(t, "exit 0\n", text)
	assert.Equal(t, "pushed\nseeded\n", testutil.Git(t, "", "-C", repo, "log", "--format=%s", "main"))
}

func TestASandboxedPushDeniedIsForbidden(t *testing.T) {
	origin, repo := testutil.GitOrigin(t)
	_, text := runAsked(t, githubTool(t, origin, "git"), pushLine, false)
	assert.NotEqual(t, "exit 0\n", text)
	assert.Equal(t, "seeded\n", testutil.Git(t, "", "-C", repo, "log", "--format=%s", "main"))
}

// gh reads the API as the user, and its own login is the placeholder.
func TestASandboxedGhReadsTheAPI(t *testing.T) {
	seen := make(chan string, 16)
	api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Method + " " + r.URL.Path + " " + r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"login":"octo"}`)
	}))
	t.Cleanup(api.Close)
	text, isError := githubTool(t, api, "gh").Run(t.Context(), chatRuntime(t, session.Chat, nil), command("gh api user; echo; gh auth token"))
	require.False(t, isError, text)
	assert.Equal(t, "{\"login\":\"octo\"}\nkubetail-placeholder\n", text)
	assert.Equal(t, "GET /user Bearer "+usersToken, testutil.Recv(t, seen, "gh's read"))
}

// gh reads the files GitHub's mount writes as they are: a gh that migrated
// them would write in the run's directory, which macOS's sandbox refuses.
func TestGhRunsOnGitHubsFilesUnchanged(t *testing.T) {
	t.Parallel()
	bin, err := exec.LookPath("gh")
	if err != nil {
		t.Skip("no gh on PATH")
	}
	dir := t.TempDir()
	m, err := newTestGitHub(githubproxy.GitHub()).Mount(t.Context(), CloudRun{On: switchedOn, Port: 6443, Dir: dir, Session: session.Session{Kind: session.Chat}})
	require.NoError(t, err)
	t.Cleanup(m.Grant.End)
	files := []string{filepath.Join(dir, "gh", "hosts.yml"), filepath.Join(dir, "gh", "config.yml")}
	before := map[string]string{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		require.NoError(t, err)
		before[f] = string(b)
	}

	cmd := exec.Command(bin, "auth", "token")
	cmd.Env = append([]string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "GH_NO_UPDATE_NOTIFIER=1"}, m.Env...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	assert.Equal(t, "kubetail-placeholder\n", string(out))
	for _, f := range files {
		assertFile(t, f, before[f])
	}
}

// githubRun is a chat's run with GitHub's cloud platform over usersToken switched
// on, beside others, and its runtime.
func githubRun(t *testing.T, others ...CloudPlatform) (*sandboxedRun, tools.Runtime) {
	t.Helper()
	platforms := append([]CloudPlatform{newTestGitHub(githubproxy.GitHub())}, others...)
	targets := []permissions.Target{permissions.GitHub}
	for _, p := range others {
		targets = append(targets, p.Target())
	}
	rt := chatRuntime(t, session.Chat, nil)
	return startRun(t, cloudTool(t, switchesOn(targets...), platforms...), rt, false), rt
}

// No GitHub token is in the run's environment, and none in a file under the
// run's directory or its tool home: the run holds the grant's token and a
// placeholder alone.
func TestNoGitHubTokenIsInTheRun(t *testing.T) {
	r, rt := githubRun(t)
	assert.Contains(t, r.run.Env, "GH_HOST=github.localhost")
	for _, kv := range r.run.Env {
		assert.NotContains(t, kv, usersToken)
	}
	for _, root := range []string{r.dir.path, tools.ToolHomePath(rt.Dir)} {
		require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			require.NoError(t, err)
			if d.Type().IsRegular() {
				b, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.NotContains(t, string(b), usersToken, path)
			}
			return nil
		}))
	}
}

func TestARunWithoutGitHubHasNoneOfIt(t *testing.T) {
	r := startRun(t, cloudTool(t, switchesOn(), newTestGitHub(githubproxy.GitHub())), chatRuntime(t, session.Chat, nil), false)
	for _, kv := range r.run.Env {
		name, _, _ := strings.Cut(kv, "=")
		assert.False(t, strings.HasPrefix(name, "GH_") || strings.HasPrefix(name, "GIT_") || strings.Contains(strings.ToUpper(name), "PROXY"), kv)
	}
	assert.NoFileExists(t, filepath.Join(r.dir.path, "gitconfig"))
	assert.NoDirExists(t, filepath.Join(r.dir.path, "gh"))
}

// GitHub's HTTP_PROXY brings NO_PROXY for the loopback, so in a run that
// mounts AWS, Google Cloud and Azure too, a client that reads both reaches
// AWS's and Azure's loopback endpoints directly and GitHub's hosts through the
// door; no variable is set two ways. Google's hosts are not the loopback: its
// own proxy setting and HTTP_PROXY name the same door, which routes by host,
// so a request carrying GitHub's proxy credentials still reaches Google's
// route.
func TestGitHubsProxyVariableLeavesTheLoopbackDirect(t *testing.T) {
	google := newFakeGoogle(t)
	r, _ := githubRun(t,
		awsAs(fakeAWSLogin{"eu-west-1"}, nil),
		NewGCP(gcpauth.New(helperAnswering("users-google-token", "proj-1")), google),
		NewAzure(fakeLogin{account: azureAccount("Pay-As-You-Go"), ok: true}, nil))
	env := map[string]string{}
	for _, kv := range r.run.Env {
		name, value, _ := strings.Cut(kv, "=")
		_, twice := env[name]
		assert.False(t, twice, name)
		env[name] = value
	}
	assert.Equal(t, "127.0.0.1", env["NO_PROXY"])
	assert.Equal(t, "127.0.0.1", env["no_proxy"])
	proxyFor := (&httpproxy.Config{HTTPProxy: env["HTTP_PROXY"], NoProxy: env["NO_PROXY"]}).ProxyFunc()
	for _, name := range []string{"AWS_ENDPOINT_URL", "IDENTITY_ENDPOINT"} {
		endpoint, err := url.Parse(env[name])
		require.NoError(t, err)
		require.Equal(t, "127.0.0.1", endpoint.Hostname(), name)
		direct, err := proxyFor(endpoint)
		require.NoError(t, err)
		assert.Nil(t, direct, name)
	}
	via, err := proxyFor(&url.URL{Scheme: "http", Host: "api.github.localhost"})
	require.NoError(t, err)
	require.NotNil(t, via)
	assert.Equal(t, "127.0.0.1", via.Hostname())

	logging, err := url.Parse(env["CLOUDSDK_API_ENDPOINT_OVERRIDES_LOGGING"])
	require.NoError(t, err)
	via, err = proxyFor(logging)
	require.NoError(t, err)
	require.NotNil(t, via, "Google's hosts are not the loopback")
	assert.Equal(t, env["CLOUDSDK_PROXY_ADDRESS"]+":"+env["CLOUDSDK_PROXY_PORT"], via.Host, "both proxies are the door")
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, logging.JoinPath("v2/projects/proj-1/logs").String(), nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+env["CLOUDSDK_AUTH_ACCESS_TOKEN"])
	password, _ := via.User.Password()
	req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(via.User.Username()+":"+password)))
	resp, err := overSocket(r).Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "Bearer users-google-token", testutil.Recv(t, google.auths, "Google's request"))
}

// GitHub's files are owner-only from birth.
func TestGitHubsFilesAreOwnerOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m, err := newTestGitHub(githubproxy.GitHub()).Mount(t.Context(), CloudRun{
		On:   switchedOn,
		Port: 6443, Dir: dir, Session: session.Session{Kind: session.Chat}, Refusal: "kubetail: no",
	})
	require.NoError(t, err)
	t.Cleanup(m.Grant.End)
	for path, mode := range map[string]os.FileMode{
		filepath.Join(dir, "gitconfig"):        0o600,
		filepath.Join(dir, "gh"):               0o700,
		filepath.Join(dir, "gh", "hosts.yml"):  0o600,
		filepath.Join(dir, "gh", "config.yml"): 0o600,
	} {
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, mode, info.Mode().Perm(), path)
	}
}

// No credential helper the user's config names is handed the door's or the
// route's credentials: git stores the proxy's after each request, and a
// keychain the sandbox denies fails the command's output with it.
func TestGitHubsGitconfigClearsCredentialHelpers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m, err := newTestGitHub(githubproxy.GitHub()).Mount(t.Context(), CloudRun{On: switchedOn, Port: 6443, Dir: dir, Session: session.Session{Kind: session.Chat}})
	require.NoError(t, err)
	t.Cleanup(m.Grant.End)
	called, global := filepath.Join(dir, "called"), filepath.Join(dir, "global")
	require.NoError(t, os.WriteFile(global, []byte("[credential]\n\thelper = \"!f() { echo $1 >>"+called+"; }; f\"\n"), 0o600))

	approve := func(host string) {
		cmd := exec.Command("git", "credential", "approve")
		cmd.Env = append(append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+global), m.Env...)
		cmd.Stdin = strings.NewReader("protocol=http\nhost=" + host + "\nusername=kubetail\npassword=secret\n\n")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	approve("127.0.0.1:6443")
	approve("github.localhost")
	assert.NoFileExists(t, called)
	approve("example.com")
	assertFile(t, called, "store\n")
}
