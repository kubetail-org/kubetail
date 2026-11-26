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

package app

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/catalog"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/lifecycle"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/azureproxy"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/githubproxy"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/loginshell"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/gcpauth"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/bash"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/webfetch"
)

// TestMain answers sandbox-init and sandbox-shell: New's probe starts
// this test binary as both, through the machine's sandbox.
func TestMain(m *testing.M) {
	if code, ok := sandbox.Main(os.Args); ok {
		testutil.ExitHelper(code)
	}
	os.Exit(m.Run())
}

// Where the probe passes, Bash is offered through the sandbox, and its schema
// has no way out of it. Without one, testutil.RequireSandbox decides.
func TestBashIsOfferedWithTheSandbox(t *testing.T) {
	sb, v, err := sandbox.Probe(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sb.Close() })
	if !v.Available {
		testutil.RequireSandbox(t, "no sandbox: "+v.Reason)
	}
	t.Setenv("SHELL", "")
	withBash := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(withBash, "bash"), []byte("#!/bin/sh\n"), 0o755))
	t.Setenv("PATH", withBash)
	shell, found := bash.New(bash.Paths{ShellDir: t.TempDir()}, 0, sb, nil, nil, nil, bash.Cloud{})
	require.True(t, found)

	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(shell.Definition().InputSchema, &schema))
	assert.Len(t, schema.Properties, 6)
	assert.NotContains(t, schema.Properties, "dangerouslyDisableSandbox")
}

// Off Windows, bash.New finds the shell on PATH once $SHELL names none, so the
// test clears it and points PATH at a directory it controls. Read, Memory and Write come
// after bash, then TaskStop, which only a bash has tasks for.
func TestBashIsOfferedWhenItIsFound(t *testing.T) {
	t.Setenv("SHELL", "")
	withBash := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(withBash, "bash"), []byte("#!/bin/sh\n"), 0o755))
	t.Setenv("PATH", withBash)
	shell, found := bash.New(bash.Paths{ShellDir: t.TempDir()}, 0, nil, nil, nil, nil, bash.Cloud{})
	require.True(t, found)
	box, err := chatTools(toolDeps{shell: shell, fenced: []string{t.TempDir()}, umask: 0o022})
	require.NoError(t, err)
	defs, _ := box.Offer()
	assert.Equal(t, []string{"Bash", "Read", "Memory", "Write", "Edit", "WebFetch", "TaskStop", "Agent", "KubeQuery", "LogsView", "LogsRead", "LogsWatch"}, definitionNames(defs))
	runner, ok := box.Runner(bash.Name)
	require.True(t, ok)
	assert.Same(t, shell, runner)
}

// The approval request draws what a gated call does, so every gated tool's calls
// are of a kind it can draw.
func TestEveryGatedToolCanBeShown(t *testing.T) {
	t.Setenv("SHELL", "")
	withBash := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(withBash, "bash"), []byte("#!/bin/sh\n"), 0o755))
	t.Setenv("PATH", withBash)
	shell, found := bash.New(bash.Paths{ShellDir: t.TempDir()}, 0, nil, nil, nil, nil, bash.Cloud{})
	require.True(t, found)

	box, err := chatTools(toolDeps{shell: shell, fenced: []string{t.TempDir()}, umask: 0o022})
	require.NoError(t, err)
	defs, _ := box.Offer()
	gated := 0
	var names []string
	for _, d := range defs {
		names = append(names, d.Name)
		runner, ok := box.Runner(d.Name)
		require.True(t, ok, d.Name)
		if _, ok := runner.(tools.Gated); ok {
			gated++
			assert.Contains(t, []tools.ActionKind{tools.ActionCommand, tools.ActionRead, tools.ActionWrite, tools.ActionEdit, tools.ActionFetch, tools.ActionMemory}, runner.ActionKind(), d.Name)
		}
	}
	assert.Positive(t, gated)
	assert.Contains(t, names, webfetch.Name)
}

// The snapshot is a part only when the config asks for one, which main alone
// does: a test's app never spawns the developer's login shell. It comes last, so
// its stop runs first.
func TestAppTakesTheSnapshotOnlyWhenAsked(t *testing.T) {
	withBash := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(withBash, "bash"), []byte("#!/bin/sh\n"), 0o755))
	t.Setenv("SHELL", "")
	t.Setenv("PATH", withBash)

	a, err := New(t.Context(), withDirs(t, Config{}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })
	assert.False(t, slices.ContainsFunc(a.rt.parts, func(p lifecycle.Part) bool { return p.Name == "shell snapshot" }))

	stubLaunch(t, loginshell.Result{}, &loginshell.Fault{Reason: "no shell", ExitCode: -1})
	a, err = New(t.Context(), withDirs(t, Config{RunLoginShell: true}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })
	assert.Equal(t, len(a.rt.parts)-1, partIndex(t, a, "shell snapshot"))
}

// A sandboxed read of the chat's cluster runs as the model asks for it: no one
// is asked, and the row says the sandbox confined it.
func TestASandboxedReadRunsUnasked(t *testing.T) {
	e := startE2E(t)

	e.ask(t, "how many pods", llm.StagedCall("Bash", bashInput("kubectl get pods -o json | jq '.items | length'")))

	row := e.bashCall(t, "succeeded", "failed", "awaiting_approval")
	assert.Equal(t, "succeeded", row.status, row.result)
	assert.Equal(t, "2", firstLine(row.result))
	assert.True(t, row.sandboxed)
	assert.Empty(t, row.approvalID)
	assert.Contains(t, e.cluster.requests(), "GET /api/v1/namespaces/default/pods", "kubectl reached the cluster")
}

// A variable the user set in Settings reaches a sandboxed command.
func TestAStoredVariableReachesASandboxedCommand(t *testing.T) {
	e := startE2E(t)
	raw := graphql(t, e.url, `mutation { envVarSet(name: "HELM_NAMESPACE", value: "apps", target: Sandbox) { name } }`)
	require.Contains(t, raw, `"name":"HELM_NAMESPACE"`, raw)

	e.ask(t, "which namespace", llm.StagedCall("Bash", bashInput(`echo "ns=$HELM_NAMESPACE"`)))

	row := e.bashCall(t, "succeeded", "failed", "awaiting_approval")
	assert.Equal(t, "succeeded", row.status, row.result)
	assert.Equal(t, "ns=apps", firstLine(row.result))
	assert.True(t, row.sandboxed)
}

// A delete from the sandbox waits for the user on one request showing the
// DELETE, reaches the cluster once approved, and the command reads its answer.
func TestASandboxedDeleteAsksAndRuns(t *testing.T) {
	e := startE2E(t)

	e.ask(t, "delete pod x", llm.StagedCall("Bash", bashInput("kubectl delete pod x --wait=false")))

	w := e.clusterWrite(t, "pending")
	assert.Equal(t, "DELETE", w.request.Write.Method)
	assert.Equal(t, "/api/v1/namespaces/default/pods/x", w.request.Write.Path)
	assert.Equal(t, "waiting_approval", e.runStatus(t))
	assert.NotContains(t, e.cluster.requests(), "DELETE /api/v1/namespaces/default/pods/x", "nothing reaches the cluster before the user decides")
	raw := graphql(t, e.url, `mutation { approvalDecide(id: "`+w.id+`", decision: Once) }`)
	require.Contains(t, raw, `"approvalDecide":true`, raw)

	row := e.bashCall(t, "succeeded", "failed")
	assert.Equal(t, "succeeded", row.status, row.result)
	// kubectl names the namespace after it in later versions.
	assert.True(t, strings.HasPrefix(firstLine(row.result), `pod "x" deleted`), row.result)
	assert.True(t, row.sandboxed)
	assert.Empty(t, row.approvalID, "the call itself ran unasked")
	assert.Contains(t, e.cluster.requests(), "DELETE /api/v1/namespaces/default/pods/x")
	assert.Equal(t, "approved", e.clusterWrite(t, "approved").status)
}

// The Sandbox console runs what the user typed in the real sandbox, with the
// folders granted always: a read of the cluster runs, and a delete is refused
// and never reaches it, since nobody is there to ask.
func TestTheSandboxConsoleReadsTheClusterAndChangesNothing(t *testing.T) {
	e := startE2E(t)
	granted := testutil.GrantableDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(granted, "note"), []byte("granted\n"), 0o600))
	raw := graphql(t, e.url, `mutation { folderGrant(path: "`+granted+`", write: false, duration: Always) { always { path } } }`)
	require.NotContains(t, raw, `"errors"`, raw)

	req, err := http.NewRequest(http.MethodPost, e.url+"/graphql", strings.NewReader(`{"query":"subscription { sandboxConsoleStateWatch { run { running output } } }"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	frames := make(chan string)
	go func() {
		defer close(frames)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(nil, 1<<20)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				frames <- data
			}
		}
	}()

	command := `cat ` + filepath.Join(granted, "note") + `; kubectl get pods -o json | jq '.items | length'; kubectl delete pod x --wait=false`
	body, err := json.Marshal(command)
	require.NoError(t, err)
	raw = graphql(t, e.url, `mutation { sandboxConsoleRun(command: `+string(body)+`, clusterID: "`+e.clusterID+`", network: false) }`)
	require.Contains(t, raw, `"sandboxConsoleRun":1`, raw)

	var output string
	for {
		var frame struct {
			Data struct {
				Watch struct {
					Run *struct {
						Running bool   `json:"running"`
						Output  string `json:"output"`
					} `json:"run"`
				} `json:"sandboxConsoleStateWatch"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(testutil.Recv(t, frames, "a console frame")), &frame))
		if run := frame.Data.Watch.Run; run != nil && !run.Running {
			output = run.Output
			break
		}
	}
	assert.True(t, strings.HasPrefix(output, "granted\n2\n"), output)
	assert.Contains(t, output, "kubetail: this sandbox reads the cluster and changes nothing.")
	assert.Contains(t, e.cluster.requests(), "GET /api/v1/namespaces/default/pods")
	assert.NotContains(t, e.cluster.requests(), "DELETE /api/v1/namespaces/default/pods/x")
}

// A denied delete comes back Forbidden and never reaches the cluster.
func TestASandboxedDeleteDeniedIsForbidden(t *testing.T) {
	e := startE2E(t)

	e.ask(t, "delete pod x", llm.StagedCall("Bash", bashInput("kubectl delete pod x --wait=false")))
	w := e.clusterWrite(t, "pending")
	raw := graphql(t, e.url, `mutation { approvalDecide(id: "`+w.id+`", decision: Deny) }`)
	require.Contains(t, raw, `"approvalDecide":true`, raw)

	row := e.bashCall(t, "succeeded", "failed")
	assert.Equal(t, "failed", row.status, row.result)
	assert.Contains(t, row.result, "Forbidden")
	assert.Contains(t, row.result, "kubetail: the user did not approve this change.")
	for _, r := range e.cluster.requests() {
		assert.NotContains(t, r, "DELETE", "the cluster saw a change")
	}
}

// A call whose time runs out while its write waits ends: the write is
// abandoned, the run is not left waiting, and the cluster sees nothing. The
// test fires the deadline once the write waits, so how long kubectl takes to
// start never decides it.
func TestACallTimingOutWhileAWriteWaits(t *testing.T) {
	deadline := make(chan time.Time)
	e := startE2E(t, func(cfg *Config) {
		cfg.callDeadline = func(time.Duration) <-chan time.Time { return deadline }
	})

	e.ask(t, "delete pod x", llm.StagedCall("Bash", bashInput("kubectl delete pod x --wait=false")))
	e.clusterWrite(t, "pending")
	close(deadline)

	row := e.bashCall(t, "succeeded", "failed")
	assert.Equal(t, "failed", row.status, row.result)
	assert.True(t, strings.HasPrefix(row.result, "Command timed out"), row.result)
	assert.Equal(t, "abandoned", e.clusterWrite(t, "abandoned").status)
	assert.NotEqual(t, "waiting_approval", e.runStatus(t))
	for _, r := range e.cluster.requests() {
		assert.NotContains(t, r, "DELETE", "the cluster saw a change")
	}
}

// The same change in a chat the user switched outside the sandbox waits for
// the user, and a denial runs nothing.
func TestASwitchedChatAsks(t *testing.T) {
	e := startE2E(t)
	chatID := e.ask(t, "switch this chat")
	raw := graphql(t, e.url, `mutation { chatSandboxDisabledSet(id: "`+chatID+`", sandboxDisabled: true) { sandboxDisabled } }`)
	require.Contains(t, raw, `"sandboxDisabled":true`, raw)

	e.askAgain(t, chatID, true, "switch this chat", llm.StagedCall("Bash", bashInput("kubectl delete pod x")))

	row := e.bashCall(t, "awaiting_approval")
	assert.False(t, row.sandboxed)
	assert.Equal(t, "pending", row.approvalStatus)
	raw = graphql(t, e.url, `mutation { approvalDecide(id: "`+row.approvalID+`", decision: Deny) }`)
	require.Contains(t, raw, `"approvalDecide":true`, raw)

	row = e.bashCall(t, "denied")
	assert.Equal(t, "denied", row.approvalStatus)
	for _, r := range e.cluster.requests() {
		assert.NotContains(t, r, "DELETE", "the cluster saw a change")
	}
}

// A subagent's sandboxed call runs unasked, as its parent's does.
func TestASubagentsSandboxedCallRunsUnasked(t *testing.T) {
	e := startE2E(t)
	e.fake.Route("count the pods").SetToolCalls(
		llm.StagedCall("Bash", bashInput("kubectl get pods -o json | jq '.items | length'")))

	e.ask(t, "have an agent count pods",
		llm.StagedCall("Agent", `{"description":"Count pods","prompt":"count the pods"}`))

	row := e.bashCall(t, "succeeded", "failed", "awaiting_approval")
	assert.Equal(t, "succeeded", row.status, row.result)
	assert.Equal(t, "2", firstLine(row.result))
	assert.True(t, row.sandboxed)
	assert.Empty(t, row.approvalID)
}

// The PATH the launch read is stored at Start, before the snapshot, on a
// machine with a sandbox, from the launch's one run of the shell; with none
// read there is nothing to store.
func TestTheLaunchRunsTheShellOnce(t *testing.T) {
	requireSandbox(t)
	t.Setenv("SHELL", "")
	withBash := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(withBash, "bash"), []byte("#!/bin/sh\n"), 0o755))
	t.Setenv("PATH", withBash)

	a, err := New(t.Context(), withDirs(t, Config{}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })
	assert.False(t, slices.ContainsFunc(a.rt.parts, func(p lifecycle.Part) bool { return p.Name == "shell PATH" }))

	data := t.TempDir()
	runs := stubLaunch(t, loginshell.Result{Path: []string{"/usr/bin", "bin"}}, nil)
	a, err = New(t.Context(), withDirs(t, Config{Folders: Folders{Data: data}, RunLoginShell: true}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })
	assert.Equal(t, partIndex(t, a, "shell snapshot")-1, partIndex(t, a, "shell PATH"))
	startApp(t, a)
	assert.Equal(t, 1, *runs, "the shell runs once")

	list, err := a.rt.Settings.RunPath(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []settings.PathFolder{
		{Path: "/usr/bin", Origin: settings.OriginShell}, {Path: "bin", Origin: settings.OriginShell},
	}, list.Search, "stored as the shell answered")
}

// A launch whose shell answers no usable PATH, as either reader answers an
// empty one, keeps the PATH an earlier launch stored.
func TestAnEmptyShellPathKeepsTheLastRow(t *testing.T) {
	requireSandbox(t)
	t.Setenv("SHELL", "")
	data := t.TempDir()
	db, err := appdb.Open(filepath.Join(data, "app.db"), 0)
	require.NoError(t, err)
	_, err = db.Write.Exec(`INSERT INTO shell_path (id, path) VALUES (1, '/usr/bin')`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	stubLaunch(t, loginshell.Result{}, &loginshell.Fault{Reason: "bad output"})
	a, err := New(t.Context(), withDirs(t, Config{Folders: Folders{Data: data}, RunLoginShell: true}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })
	startApp(t, a)
	assert.Equal(t, "bad output", a.rt.Settings.PathFault())
	list, err := a.rt.Settings.RunPath(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []settings.PathFolder{{Path: "/usr/bin", Origin: settings.OriginShell}}, list.Search)
}

// countProbes replaces probeSandbox with one that counts its calls and keeps
// the sandbox the machine's probe answered.
func countProbes(t *testing.T) (count *atomic.Int32, probed **sandbox.Sandbox) {
	t.Helper()
	count, probed = new(atomic.Int32), new(*sandbox.Sandbox)
	probe := probeSandbox
	t.Cleanup(func() { probeSandbox = probe })
	probeSandbox = func(ctx context.Context) (*sandbox.Sandbox, sandbox.State, error) {
		count.Add(1)
		sb, status, err := probe(ctx)
		*probed = sb
		return sb, status, err
	}
	return count, probed
}

// New probes the sandbox once, for the login shell and the bash tool alike,
// and runs the login shell once wherever the platform reads its answer.
func TestTheSandboxIsProbedOnce(t *testing.T) {
	probes, probed := countProbes(t)
	launches := stubLaunch(t, loginshell.Result{Path: []string{"/usr/bin"}}, nil)

	a, err := New(t.Context(), withDirs(t, Config{RunLoginShell: true}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })

	assert.Equal(t, int32(1), probes.Load())
	var sb sandboxer
	if *probed != nil {
		sb = *probed
	}
	want := 1
	if skipResolution(sb) {
		want = 0
	}
	assert.Equal(t, want, *launches)
}

// refusingSandbox records each Run it is handed and refuses it; its Never
// list is never, and it reads nothing.
type refusingSandbox struct {
	never []string
	runs  []sandbox.Run
}

func (s *refusingSandbox) Command(_ context.Context, r sandbox.Run) (*exec.Cmd, error) {
	s.runs = append(s.runs, r)
	return nil, errors.New("refused")
}

func (s *refusingSandbox) Never(string) []string { return s.never }

func (s *refusingSandbox) System(string, string) sandbox.System { return sandbox.System{} }

// Refresh PATH runs the login shell in the sandbox, under the launch's
// policy: the denied-always list, as it stands at each refresh, and Kubetail's
// directories shut.
func TestTheRefreshRunsInTheSandbox(t *testing.T) {
	store, err := settings.Open(appdbtest.Open(t))
	require.NoError(t, err)
	sb := &refusingSandbox{never: []string{"/never"}}
	kubetailDirs := []string{t.TempDir(), t.TempDir(), t.TempDir()}
	svc := newSettingsService(store, sb, nil, sandbox.State{Available: true}, bash.Paths{DeniedDirs: kubetailDirs, TmpDir: filepath.Join(kubetailDirs[1], "tmp")}, "")

	err = svc.RefreshPath(t.Context())
	require.ErrorContains(t, err, "sandbox refused")
	sb.never = []string{"/never", "/home/new"}
	err = svc.RefreshPath(t.Context())
	require.ErrorContains(t, err, "sandbox refused")

	require.Len(t, sb.runs, 2)
	assert.Equal(t, []string{"/never"}, sb.runs[0].Policy.Always.Deny)
	assert.Equal(t, kubetailDirs, sb.runs[0].Policy.Always.Kubetail)
	assert.Equal(t, []string{"/never", "/home/new"}, sb.runs[1].Policy.Always.Deny)
}

// A context that ends during startup stops New before the login shell runs,
// so a probe a quit cut short never stands for no sandbox.
func TestNewStopsWhenItsContextEnds(t *testing.T) {
	launches := stubLaunch(t, loginshell.Result{Path: []string{"/usr/bin"}}, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	a, err := New(ctx, withDirs(t, Config{RunLoginShell: true}))

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, a)
	assert.Zero(t, *launches)
}

// New asks the login shell for every provider key and lists the providers it
// sets; a key the launch environment set wins.
func TestNewTakesTheProviderKeysTheShellSets(t *testing.T) {
	requireSandbox(t)
	t.Cleanup(safe.ResetSecrets)
	var asked []string
	resolve := resolveShell
	t.Cleanup(func() { resolveShell = resolve })
	resolveShell = func(_ context.Context, _ loginshell.Start, keyVars []string) (loginshell.Result, *loginshell.Fault) {
		asked = keyVars
		return loginshell.Result{Keys: map[string]string{
			"ANTHROPIC_API_KEY": "sk-ant-shell-0123456789",
			"OPENAI_API_KEY":    "sk-openai-shell-0123456789",
		}}, nil
	}

	a, err := New(t.Context(), withDirs(t, Config{
		RunLoginShell: true,
		LLMKeys:       map[string]string{"openai": "sk-openai-launch-0123456789"},
	}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })

	assert.Equal(t, slices.Sorted(maps.Values(catalog.KeyVars())), asked)
	keys := map[string]string{}
	for _, p := range a.rt.Catalog.Providers() {
		if st, _ := a.rt.ProviderCredentials.State(p.ID); st.HasEnvironmentKey {
			r, err := http.NewRequest(http.MethodPost, p.BaseURL+"/x", nil)
			require.NoError(t, err)
			require.NoError(t, p.Signer.Sign(r))
			keys[p.ID] = r.Header.Get("X-Api-Key") + strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
	}
	assert.Equal(t, map[string]string{"anthropic": "sk-ant-shell-0123456789", "openai": "sk-openai-launch-0123456789"}, keys)
}

// A quit during the sandbox probe fails the build and releases the lock.
func TestAQuitDuringTheProbeReleasesTheLock(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cfg := withDirs(t, Config{})

	_, err := build(ctx, cfg)
	require.ErrorIs(t, err, context.Canceled)
	requireUnlocked(t, cfg.Folders.Data)
}

// fakeCloud is a cloud platform whose upstream is an httptest TLS server: a GET
// is a read, forwarded unasked, and anything else a change put to the user. Its
// client finds the route and its token in FAKE_CLOUD_URL and FAKE_CLOUD_TOKEN,
// and carries the token in X-Fake-Token.
type fakeCloud struct {
	upstream *httptest.Server
	seen     chan string // each request the upstream received, "<method> <path>"
}

func newFakeCloud(t *testing.T) *fakeCloud {
	f := &fakeCloud{seen: make(chan string, 8)}
	f.upstream = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.seen <- r.Method + " " + r.URL.Path
		_, _ = io.WriteString(w, "upstream "+r.Method)
	}))
	t.Cleanup(f.upstream.Close)
	return f
}

func (f *fakeCloud) Target() permissions.Target { return permissions.AWS }

func (f *fakeCloud) Mount(_ context.Context, run bash.CloudRun) (bash.CloudMount, error) {
	g := runproxy.NewGrant(permissions.AWS, run.On, run.Session, run.Asker, run.Refusal, runproxy.Limits{QPS: 100, Burst: 100, Open: 8, Bodies: 8})
	client := f.upstream.Client()
	host := "127.0.0.1:" + strconv.Itoa(run.Port)
	forward := func(w http.ResponseWriter, r *http.Request) error {
		out, err := http.NewRequestWithContext(r.Context(), r.Method, f.upstream.URL+r.URL.Path, r.Body)
		if err != nil {
			return err
		}
		out.Header = runproxy.Outbound(r.Header, []string{"Content-Type"})
		resp, err := client.Do(out)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		_, err = io.Copy(w, resp.Body)
		return err
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r, done, ok := g.Admit(w, r, r.Header.Get("X-Fake-Token"))
		if !ok {
			return
		}
		defer done()
		if r.Method == http.MethodGet {
			release, status := g.Slot(r.Context())
			if status != 0 {
				http.Error(w, "kubetail: no slot", status)
				return
			}
			defer release()
			_ = forward(w, r)
			return
		}
		body, release, status := g.Body(w, r)
		defer release()
		if status != 0 {
			http.Error(w, "kubetail: unreadable body", status)
			return
		}
		out := g.Decide(r.Context(), runproxy.Request{
			Action: permissions.Action{AccessLevel: permissions.Write, Once: true, Summary: "AWS: fake change"},
			Sent: runproxy.Sent{
				Host: f.upstream.Listener.Addr().String(), Method: r.Method, Path: r.URL.RequestURI(),
				ContentType: r.Header.Get("Content-Type"), Body: body,
			},
		}, func() error { return forward(w, r) })
		if !out.Forwarded {
			http.Error(w, out.Message, out.Status)
		}
	})
	return bash.CloudMount{
		Routes: []bash.Route{{Host: host, Handler: handler}},
		Env:    []string{"FAKE_CLOUD_URL=http://" + host, "FAKE_CLOUD_TOKEN=" + g.Token()},
		Grant:  g,
	}, nil
}

// With its switch turned on through the wire, a sandboxed curl reads the
// cloud platform unasked, and its change waits on one request and reaches the
// upstream once approved; with the switch off the route is the door's 401.
func TestASandboxedCommandReachesACloudPlatformThroughTheDoor(t *testing.T) {
	cloud := newFakeCloud(t)
	e := startE2E(t, func(c *Config) { c.cloudPlatforms = []bash.CloudPlatform{cloud} })
	raw := graphql(t, e.url, `mutation { sandboxCloudAccessSet(target: AWS, on: true) { target on } }`)
	require.Contains(t, raw, `{"target":"AWS","on":true}`, raw)

	curl := `curl -sS -H "X-Fake-Token: $FAKE_CLOUD_TOKEN" `
	chatID := e.ask(t, "reach the cloud platform", llm.StagedCall("Bash", bashInput(
		curl+`"$FAKE_CLOUD_URL/read"; echo; `+curl+`-X POST -H 'Content-Type: text/plain' -d 'change=1' "$FAKE_CLOUD_URL/change"`)))

	assert.Equal(t, "GET /read", testutil.Recv(t, cloud.seen, "the read"))
	w := e.clusterWrite(t, "pending")
	require.NotNil(t, w.request.Cloud)
	assert.Equal(t, permissions.AWS, w.request.Action.Target)
	assert.Equal(t, "POST", w.request.Cloud.Method)
	assert.Equal(t, "change=1", w.request.Cloud.Body)
	assert.Empty(t, cloud.seen, "nothing reaches the cloud platform before the user decides")
	raw = graphql(t, e.url, `mutation { approvalDecide(id: "`+w.id+`", decision: Once) }`)
	require.Contains(t, raw, `"approvalDecide":true`, raw)
	assert.Equal(t, "POST /change", testutil.Recv(t, cloud.seen, "the change"))
	row := e.bashCall(t, "succeeded", "failed")
	assert.Contains(t, row.result, "upstream GET")
	assert.Contains(t, row.result, "upstream POST")

	raw = graphql(t, e.url, `mutation { sandboxCloudAccessSet(target: AWS, on: false) { target on } }`)
	require.Contains(t, raw, `{"target":"AWS","on":false}`, raw)
	// A run started while it is off mounts nothing, so the route is the door's.
	// The door's port is the run's own, read from its kubeconfig.
	e.askAgain(t, chatID, false, "reach the cloud platform", llm.StagedCall("Bash", bashInput(
		`door=$(grep -o '127\.0\.0\.1:[0-9]*' "$KUBECONFIG" | head -n 1); `+
			`curl -s -o /dev/null -w '%{http_code}' -H "X-Fake-Token: none" "http://$door/read"; echo " $FAKE_CLOUD_URL"`)))
	result := e.lastBashResult(t, "401")
	assert.NotContains(t, result, "http://", "a run with the switch off holds no route")
	assert.Empty(t, cloud.seen)
}

const (
	azSub    = "00000000-0000-0000-0000-000000000001"
	azTenant = "0000000a-0000-0000-0000-000000000000"
	// azConverge bounds a command running az, whose first run in a fresh
	// configuration directory takes seconds, more on a loaded runner.
	azConverge = 2 * time.Minute
)

// fakeAzLogin is the user's az as Azure's mount reads it: signed into one
// subscription, its token a known string.
type fakeAzLogin struct{}

func (fakeAzLogin) Account(context.Context) (azureproxy.Account, bool) {
	return azureproxy.Account{
		Cloud: azureproxy.Cloud{Name: "AzureCloud", Endpoints: map[string]string{
			"resourceManager":           "https://management.azure.com/",
			"activeDirectory":           "https://login.microsoftonline.com",
			"activeDirectoryResourceId": "https://management.core.windows.net/",
		}},
		Subscriptions: []azureproxy.Subscription{{ID: azSub, Name: "Pay-As-You-Go", TenantID: azTenant, IsDefault: true}},
	}, true
}

func (fakeAzLogin) Token(context.Context, string) (string, error) { return "users-own-arm-token", nil }
func (fakeAzLogin) Drop(string, string)                           {}

// fakeARM is Resource Manager over TLS: two pages of AKS clusters, one VM,
// and a resource group delete Azure finishes after one poll. Its polls say
// Retry-After 1: az reads 0 as unset and waits its default 30 s. seen is
// each request's method and path.
type fakeARM struct {
	srv  *httptest.Server
	mu   sync.Mutex
	seen []string
}

func newFakeARM(t *testing.T) *fakeARM {
	f := &fakeARM{}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seen = append(f.seen, r.Method+" "+r.URL.Path)
		polls := 0
		for _, s := range f.seen {
			if strings.Contains(s, "/operationresults/") {
				polls++
			}
		}
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer users-own-arm-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		p := strings.ToLower(r.URL.Path)
		switch {
		case strings.HasSuffix(p, "/managedclusters") && r.URL.Query().Get("page") == "":
			_, _ = io.WriteString(w, `{"value": [{"name": "aks-1", "location": "westus"}], "nextLink": "https://management.azure.com`+r.URL.Path+`?api-version=2024-05-01&page=2"}`)
		case strings.HasSuffix(p, "/managedclusters"):
			_, _ = io.WriteString(w, `{"value": [{"name": "aks-2", "location": "westus"}]}`)
		case strings.HasSuffix(p, "/virtualmachines"):
			_, _ = io.WriteString(w, `{"value": [{"name": "vm-1", "location": "westus"}]}`)
		case r.Method == http.MethodDelete:
			w.Header().Set("Location", "https://management.azure.com/subscriptions/"+azSub+"/operationresults/tok?api-version=2024-11-01")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusAccepted)
		case strings.Contains(p, "/operationresults/") && polls < 2:
			w.Header().Set("Location", "https://management.azure.com"+r.URL.RequestURI())
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusAccepted)
		case strings.Contains(p, "/operationresults/"):
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error": {"code": "NotFound", "message": "none"}}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeARM) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.seen)
}

// startAzureE2E is startE2E with Azure's mount over the fake login and
// arm, its switch turned on, and the az KUBETAIL_TEST_AZ names on the run's
// PATH. It skips without one: a runner image's own az is any version, and
// may be a launcher that finds its Python by a path relative to itself.
func startAzureE2E(t *testing.T, arm *fakeARM) *e2e {
	t.Helper()
	az := os.Getenv("KUBETAIL_TEST_AZ")
	if az == "" {
		t.Skip("KUBETAIL_TEST_AZ names no az")
	}
	// Not under /tmp, which a typed folder's check refuses on Linux.
	bin := testutil.GrantableDir(t)
	require.NoError(t, os.Symlink(az, filepath.Join(bin, "az")))
	e := startE2E(t, func(c *Config) {
		c.cloudPlatforms = []bash.CloudPlatform{bash.NewAzure(fakeAzLogin{}, testutil.DialAnywhere(arm.srv))}
	})
	raw := graphql(t, e.url, `mutation { sandboxPathSet(value: "`+bin+`:$PATH") { value } }`)
	require.Contains(t, raw, `"value":"`+bin+`:$PATH"`, raw)
	raw = graphql(t, e.url, `mutation { sandboxCloudAccessSet(target: Azure, on: true) { target on } }`)
	require.Contains(t, raw, `{"target":"Azure","on":true}`, raw)
	return e
}

// az reads Resource Manager in the sandbox across a paged answer, unasked.
func TestAzListsInTheSandbox(t *testing.T) {
	arm := newFakeARM(t)
	e := startAzureE2E(t, arm)
	e.ask(t, "list azure", llm.StagedCall("Bash", bashInput(`az aks list --query '[].name' -o tsv && az vm list --query '[].name' -o tsv`)))

	row := e.bashCallWithin(t, azConverge, "succeeded", "failed")
	assert.Equal(t, "succeeded", row.status, row.result)
	assert.Contains(t, row.result, "aks-1\naks-2\nvm-1")
	assert.NotContains(t, row.result, "users-own-arm-token")
}

// A resource group delete waits on one request; denied, az fails and Azure
// sees no DELETE.
func TestAnAzChangeAsks(t *testing.T) {
	arm := newFakeARM(t)
	e := startAzureE2E(t, arm)
	e.ask(t, "delete azure group", llm.StagedCall("Bash", bashInput(`az group delete -n rg --yes`)))

	w := e.clusterWriteWithin(t, azConverge, "pending")
	require.NotNil(t, w.request.Cloud)
	assert.Equal(t, permissions.Azure, w.request.Action.Target)
	assert.Equal(t, permissions.Destructive, w.request.Action.AccessLevel)
	assert.Equal(t, "Azure: delete resource group rg in Pay-As-You-Go", w.request.Action.Summary)
	raw := graphql(t, e.url, `mutation { approvalDecide(id: "`+w.id+`", decision: Deny) }`)
	require.Contains(t, raw, `"approvalDecide":true`, raw)

	row := e.bashCallWithin(t, azConverge, "succeeded", "failed")
	assert.Contains(t, row.result, "kubetail: the user did not approve this request.")
	for _, r := range arm.requests() {
		assert.False(t, strings.HasPrefix(r, http.MethodDelete), r)
	}
}

// Approved, the delete reaches Azure once and az polls its result through
// the door to the end.
func TestAnApprovedAzChangePollsToItsEnd(t *testing.T) {
	arm := newFakeARM(t)
	e := startAzureE2E(t, arm)
	e.ask(t, "delete azure group for real", llm.StagedCall("Bash", bashInput(`az group delete -n rg --yes && echo deleted`)))

	w := e.clusterWriteWithin(t, azConverge, "pending")
	raw := graphql(t, e.url, `mutation { approvalDecide(id: "`+w.id+`", decision: Once) }`)
	require.Contains(t, raw, `"approvalDecide":true`, raw)

	row := e.bashCallWithin(t, azConverge, "succeeded", "failed")
	assert.Contains(t, row.result, "deleted", row.result)
	assert.Equal(t, []string{
		"DELETE /subscriptions/" + azSub + "/resourcegroups/rg",
		"GET /subscriptions/" + azSub + "/operationresults/tok",
		"GET /subscriptions/" + azSub + "/operationresults/tok",
	}, arm.requests())
}

// One run holds the cluster and Azure: kubectl and az each read, and no
// variable is set two ways.
func TestAzureBesideTheCluster(t *testing.T) {
	arm := newFakeARM(t)
	e := startAzureE2E(t, arm)
	e.ask(t, "kubectl and az", llm.StagedCall("Bash", bashInput(
		`kubectl get pods -o name && az vm list --query '[].name' -o tsv && env | cut -d= -f1 | sort | uniq -d | sed 's/^/twice: /'`)))

	row := e.bashCallWithin(t, azConverge, "succeeded", "failed")
	assert.Equal(t, "succeeded", row.status, row.result)
	assert.Contains(t, row.result, "pod/")
	assert.Contains(t, row.result, "vm-1")
	assert.NotContains(t, row.result, "twice:")
}

// awsLogin is the user's AWS login for an end-to-end run: these credentials,
// in eu-west-1.
type awsLogin aws.Credentials

func (l awsLogin) Credentials(context.Context) (aws.Credentials, error) {
	return aws.Credentials(l), nil
}
func (awsLogin) Drop()          {}
func (awsLogin) Region() string { return "eu-west-1" }

// With AWS turned on through the wire, a sandboxed curl signing with the run's
// variables reads unasked, and a change waits on one request and reaches AWS
// once approved, signed with the user's credentials; kubectl still reads the
// cluster. With the switch off the run holds no AWS variable.
func TestASandboxedCommandReachesAWS(t *testing.T) {
	// Each request AWS got: its host, its body and who signed it.
	seen := make(chan string, 8)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		signer, _, _ := strings.Cut(r.Header.Get("Authorization"), "/")
		seen <- r.Host + " " + string(body) + " " + signer
		_, _ = io.WriteString(w, "<DescribeInstancesResponse/>")
	}))
	t.Cleanup(upstream.Close)
	client := &http.Client{Transport: &http.Transport{
		// Every host of the table dials the test's upstream.
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
		},
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // the test's own upstream
	}}
	login := awsLogin{AccessKeyID: "AKIAIOSFODNN7EXAMPLE", SecretAccessKey: "wJalrXUtnFEMIK7MDENGbPxRfiCYzzKEY"}
	// No kube-context names a profile, so each run reaches AWS as login.
	platform := bash.NewAWS(func(string) string { return "" }, func(string) bash.AWSLogin { return login }, client)
	e := startE2E(t, func(c *Config) { c.cloudPlatforms = []bash.CloudPlatform{platform} })
	raw := graphql(t, e.url, `mutation { sandboxCloudAccessSet(target: AWS, on: true) { target on } }`)
	require.Contains(t, raw, `{"target":"AWS","on":true}`, raw)

	curl := `curl -sS --aws-sigv4 aws:amz:eu-west-1:ec2 --user "$AWS_ACCESS_KEY_ID:$AWS_SECRET_ACCESS_KEY" `
	chatID := e.ask(t, "reach AWS", llm.StagedCall("Bash", bashInput(
		curl+`-d Action=DescribeInstances "$AWS_ENDPOINT_URL/"; echo; `+curl+`-d Action=StopInstances "$AWS_ENDPOINT_URL/"; echo; kubectl get pods -o name`)))

	signed := "AWS4-HMAC-SHA256 Credential=" + login.AccessKeyID
	assert.Equal(t, "ec2.eu-west-1.amazonaws.com Action=DescribeInstances "+signed, testutil.Recv(t, seen, "the read"))
	w := e.clusterWrite(t, "pending")
	require.NotNil(t, w.request.Cloud)
	assert.Equal(t, permissions.AWS, w.request.Action.Target)
	assert.Equal(t, "AWS: call ec2:StopInstances in eu-west-1", w.request.Action.Summary)
	assert.Empty(t, seen, "nothing reaches AWS before the user decides")
	raw = graphql(t, e.url, `mutation { approvalDecide(id: "`+w.id+`", decision: Once) }`)
	require.Contains(t, raw, `"approvalDecide":true`, raw)
	assert.Equal(t, "ec2.eu-west-1.amazonaws.com Action=StopInstances "+signed, testutil.Recv(t, seen, "the change"))
	row := e.bashCall(t, "succeeded", "failed")
	assert.Contains(t, row.result, "<DescribeInstancesResponse/>")
	assert.Contains(t, row.result, "pod/")

	raw = graphql(t, e.url, `mutation { sandboxCloudAccessSet(target: AWS, on: false) { target on } }`)
	require.Contains(t, raw, `{"target":"AWS","on":false}`, raw)
	e.askAgain(t, chatID, false, "reach AWS", llm.StagedCall("Bash", bashInput(`echo "aws names: $(env | grep -c '^AWS_')"`)))
	e.lastBashResult(t, "aws names: 0")
	assert.Empty(t, seen)
}

// With GitHub turned on through the wire, and the fake cloud platform beside it, a
// sandboxed git reads github.com unasked while kubectl still reads the
// cluster, and a push waits on one request and lands once approved. The run
// starting at all says no two cloud platforms set one variable two ways.
func TestASandboxedGitReachesGitHubThroughTheDoor(t *testing.T) {
	origin, repo := testutil.GitOrigin(t)
	cloud := newFakeCloud(t)
	github := bash.NewGitHub(knownTokens("gho_theUsersOwnGitHubToken0001"), githubproxy.Upstreams{
		Git: origin.URL, API: origin.URL, Client: origin.Client(),
	})
	e := startE2E(t, func(c *Config) { c.cloudPlatforms = []bash.CloudPlatform{github, cloud} })
	for _, target := range []string{"GitHub", "AWS"} {
		raw := graphql(t, e.url, `mutation { sandboxCloudAccessSet(target: `+target+`, on: true) { target on } }`)
		require.Contains(t, raw, `{"target":"`+target+`","on":true}`, raw)
	}

	e.ask(t, "push to github", llm.StagedCall("Bash", bashInput(
		`git ls-remote https://github.com/o/r.git main | grep -c .; `+
			`kubectl get pods -o json | jq '.items | length'; `+
			`git clone -q https://github.com/o/r.git r && cd r && `+
			`git -c user.name=k -c user.email=k@example.com -c maintenance.auto=false commit -q --allow-empty -m pushed && `+
			`git push -q origin HEAD:main; echo "push $?"`)))

	w := e.clusterWrite(t, "pending")
	require.NotNil(t, w.request.Cloud)
	assert.Equal(t, permissions.GitHub, w.request.Action.Target)
	assert.Equal(t, "GitHub: push to o/r", w.request.Action.Summary)
	assert.Equal(t, "seeded\n", testutil.Git(t, "", "-C", repo, "log", "--format=%s", "main"), "nothing lands before the user decides")
	raw := graphql(t, e.url, `mutation { approvalDecide(id: "`+w.id+`", decision: Once) }`)
	require.Contains(t, raw, `"approvalDecide":true`, raw)

	row := e.bashCall(t, "succeeded", "failed")
	assert.True(t, strings.HasPrefix(row.result, "1\n2\npush 0\n"), row.result)
	assert.Contains(t, e.cluster.requests(), "GET /api/v1/namespaces/default/pods", "kubectl reached the cluster")
	assert.Equal(t, "pushed\nseeded\n", testutil.Git(t, "", "-C", repo, "log", "--format=%s", "main"))
}

// knownTokens answers one known token.
type knownTokens string

func (k knownTokens) Token(context.Context) (string, error) { return string(k), nil }
func (knownTokens) Refused(string)                          {}

// fakeGoogle is Google's helper and upstream for Google's real cloud platform: the
// helper answers a token and counts its runs, and the upstream is a TLS
// server reached whatever the host, which keeps "<method> <path>" and the
// bearer of each request.
type fakeGoogle struct {
	upstream *httptest.Server
	seen     chan string
	runs     atomic.Int32
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	g := &fakeGoogle{seen: make(chan string, 8)}
	g.upstream = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.seen <- r.Method + " " + r.URL.Path + " " + r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"clusters":[{"name":"from-google"}]}`)
	}))
	t.Cleanup(g.upstream.Close)
	return g
}

func (g *fakeGoogle) RoundTrip(r *http.Request) (*http.Response, error) {
	out := r.Clone(r.Context())
	out.URL.Host = g.upstream.Listener.Addr().String()
	return g.upstream.Client().Transport.RoundTrip(out)
}

// cloudPlatform is Google's real cloud platform over g.
func (g *fakeGoogle) cloudPlatform() bash.CloudPlatform {
	src := gcpauth.New(func(context.Context, bool) ([]byte, error) {
		g.runs.Add(1)
		return fmt.Appendf(nil, `{"configuration":{"properties":{"core":{"account":"dev@example.com","project":"proj-1"}}},`+
			`"credential":{"access_token":"users-google-token","token_expiry":%q}}`, time.Now().Add(time.Hour).Format(time.RFC3339)), nil
	})
	return bash.NewGCP(src, g)
}

// gcloudCurl is a curl sending method to path on service as gcloud does:
// through the run's door as its proxy, with the run's token.
func gcloudCurl(method, service, path string) string {
	return `curl -sS -X ` + method + ` -x "http://127.0.0.1:$CLOUDSDK_PROXY_PORT" -H "Authorization: Bearer $CLOUDSDK_AUTH_ACCESS_TOKEN" ` +
		`"http://` + service + `.gcp.kubetail.invalid` + path + `"`
}

// googleOn starts the app with Google's cloud platform over g and its switch on.
func googleOn(t *testing.T, g *fakeGoogle) *e2e {
	e := startE2E(t, func(c *Config) { c.cloudPlatforms = []bash.CloudPlatform{g.cloudPlatform()} })
	raw := graphql(t, e.url, `mutation { sandboxCloudAccessSet(target: GCP, on: true) { target on } }`)
	require.Contains(t, raw, `{"target":"GCP","on":true}`, raw)
	return e
}

func TestASandboxedGCPReadRunsUnasked(t *testing.T) {
	g := newFakeGoogle(t)
	e := googleOn(t, g)

	e.ask(t, "list the clusters", llm.StagedCall("Bash", bashInput(gcloudCurl("GET", "container", "/v1/projects/p/locations/-/clusters"))))

	assert.Equal(t, "GET /v1/projects/p/locations/-/clusters Bearer users-google-token", testutil.Recv(t, g.seen, "the read"))
	row := e.bashCall(t, "succeeded")
	assert.Contains(t, row.result, "from-google")
	var asked int
	require.NoError(t, e.db.QueryRow(`SELECT count(*) FROM approvals WHERE kind = 'action'`).Scan(&asked))
	assert.Zero(t, asked, "a read asks nobody")
}

func TestASandboxedGCPChangeAsks(t *testing.T) {
	g := newFakeGoogle(t)
	e := googleOn(t, g)

	e.ask(t, "delete the cluster", llm.StagedCall("Bash", bashInput(gcloudCurl("DELETE", "container", "/v1/projects/p/locations/l/clusters/dev"))))

	w := e.clusterWrite(t, "pending")
	require.NotNil(t, w.request.Cloud)
	assert.Equal(t, permissions.GCP, w.request.Action.Target)
	assert.Equal(t, permissions.Destructive, w.request.Action.AccessLevel)
	assert.Equal(t, "DELETE", w.request.Cloud.Method)
	assert.Contains(t, w.request.Cloud.Details, tools.Detail{Label: "Account", Value: "dev@example.com"})
	assert.Empty(t, g.seen, "nothing reaches Google before the user decides")
	raw := graphql(t, e.url, `mutation { approvalDecide(id: "`+w.id+`", decision: Once) }`)
	require.Contains(t, raw, `"approvalDecide":true`, raw)
	assert.Equal(t, "DELETE /v1/projects/p/locations/l/clusters/dev Bearer users-google-token", testutil.Recv(t, g.seen, "the change"))
	e.bashCall(t, "succeeded")
}

// With the switch turned off, a run mounts nothing: the route is the door's
// 401, and gcloud's helper never runs.
func TestGCPOffServesNothing(t *testing.T) {
	g := newFakeGoogle(t)
	e := startE2E(t, func(c *Config) { c.cloudPlatforms = []bash.CloudPlatform{g.cloudPlatform()} })
	raw := graphql(t, e.url, `mutation { sandboxCloudAccessSet(target: GCP, on: false) { target on } }`)
	require.Contains(t, raw, `{"target":"GCP","on":false}`, raw)

	// The door's port is the run's own, read from its kubeconfig.
	e.ask(t, "list the clusters", llm.StagedCall("Bash", bashInput(
		`door=$(grep -o '127\.0\.0\.1:[0-9]*' "$KUBECONFIG" | head -n 1); `+
			`curl -s -o /dev/null -w '%{http_code}' -x "http://$door" "http://container.gcp.kubetail.invalid/v1/projects/p/locations/-/clusters"; `+
			`echo " ${CLOUDSDK_AUTH_ACCESS_TOKEN:-unset}"`)))

	row := e.bashCall(t, "succeeded", "failed")
	assert.Equal(t, "401 unset", firstLine(row.result))
	assert.Empty(t, g.seen)
	assert.Zero(t, g.runs.Load(), "the helper never runs")
}
