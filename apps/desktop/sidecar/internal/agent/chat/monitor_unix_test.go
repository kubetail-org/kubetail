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

package chat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/bash"
)

// TestMain answers sandbox-init and sandbox-shell: a sandboxed run starts this
// test binary as both.
func TestMain(m *testing.M) {
	if code, ok := sandbox.Main(os.Args); ok {
		testutil.ExitHelper(code)
	}
	os.Exit(m.Run())
}

// clusterAPI is an API server that answers a pod list, a Secret and a Secret
// list, and keeps every request it was sent.
type clusterAPI struct {
	*httptest.Server
	mu   sync.Mutex
	seen []string
}

func newClusterAPI(t *testing.T) *clusterAPI {
	t.Helper()
	api := &clusterAPI{}
	api.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		api.seen = append(api.seen, r.Method+" "+r.URL.Path)
		api.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/secrets/db"):
			_, _ = io.WriteString(w, `{"kind":"Secret","apiVersion":"v1","metadata":{"name":"db","namespace":"web"},"data":{"note":"aHVudGVyMg=="}}`)
		case strings.HasSuffix(r.URL.Path, "/secrets"):
			_, _ = io.WriteString(w, `{"kind":"SecretList","apiVersion":"v1","items":[{"metadata":{"name":"db","namespace":"web"},"data":{"note":"aHVudGVyMg=="}}]}`)
		default:
			_, _ = io.WriteString(w, `{"kind":"PodList","apiVersion":"v1","items":[{"metadata":{"name":"web-1"}}]}`)
		}
	}))
	t.Cleanup(api.Close)
	return api
}

// requests is every request the server was sent, as method and path.
func (api *clusterAPI) requests() []string {
	api.mu.Lock()
	defer api.mu.Unlock()
	return append([]string(nil), api.seen...)
}

// clusterService is the cluster service bash reads a run's cluster through:
// every id is one record on the kube-context prod, leased over api.
type clusterService struct {
	cluster.Service
	api *clusterAPI
}

func (c clusterService) AcquireConnection(context.Context, apimeta.ClusterID) (cluster.Lease, error) {
	base, _ := url.Parse(c.api.URL)
	return clusterLease{conn: &cluster.Connection{BaseURL: base, HTTPClient: c.api.Client()}}, nil
}

func (c clusterService) Clusters() cluster.Clusters { return clusterRecords{} }

type clusterRecords struct{ cluster.Clusters }

func (clusterRecords) Get(_ context.Context, id apimeta.ClusterID) (*cluster.Cluster, error) {
	uid := "uid-1"
	c := &cluster.Cluster{ID: id, Spec: cluster.ClusterSpec{
		Enabled: true,
		Source:  cluster.ClusterSpecSource{Kubeconfig: &cluster.ClusterSpecSourceKubeconfig{Context: "prod"}},
	}}
	c.Status.Server.UID = &uid
	return c, nil
}

// WatchCollectionByID sends nothing until the run ends, so no claim is revoked.
func (clusterRecords) WatchCollectionByID(ctx context.Context, _ apimeta.ClusterID) (*cluster.Stream[cluster.ClusterCollectionWatchFrame], error) {
	return cluster.NewStream(ctx, func(ctx context.Context, _ chan<- cluster.ClusterCollectionWatchFrame) error {
		<-ctx.Done()
		return nil
	}), nil
}

func (clusterRecords) List(context.Context) ([]*cluster.Cluster, error) { return nil, nil }

type clusterLease struct {
	cluster.Lease
	conn *cluster.Connection
}

func (l clusterLease) ConnFor(context.Context, string) (*cluster.Connection, error) {
	return l.conn, nil
}

func (clusterLease) Release() {}

// sandboxRig is a started service whose box is the real Bash tool on the
// machine's sandbox, reaching api through the real proxy, with Kubetail's three
// directories laid out as the app lays them out.
type sandboxRig struct {
	s    *service
	sb   *sandbox.Sandbox
	api  *clusterAPI
	home string // what grantable made the home
}

func startSandboxRig(t *testing.T) sandboxRig {
	t.Helper()
	sb, v, err := sandbox.Probe(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sb.Close() })
	if sb == nil || !sb.Confines() {
		testutil.RequireSandbox(t, "no confining sandbox: "+v.Reason)
	}
	// The bash tool reads $SHELL from the process, so a test using this rig stays serial.
	t.Setenv("SHELL", "")
	data, cache := t.TempDir(), t.TempDir()
	// Short, since a run's socket path must fit.
	runtime, err := os.MkdirTemp("/tmp", "k")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	api := newClusterAPI(t)
	tool, ok := bash.New(bash.Paths{
		ShellDir: filepath.Join(runtime, "shell"), RunsDir: filepath.Join(runtime, "runs"),
		TmpDir: filepath.Join(cache, "tmp"), KubectlDir: filepath.Join(cache, "kubectl"),
		DeniedDirs: []string{data, cache, runtime},
	}, 0, sb, clusterService{api: api}, defaultPath, nil, bash.Cloud{})
	if !ok {
		t.Skip("no bash found on this machine")
	}
	box, lists := testBox(tool)
	s, err := newService(openTestDB(t, data), chatsDirIn(data), monitorDirIn(data), fakeLLM(), &stubClusterCards{}, nil, box, lists, sandbox.State{}, testSettings(t), nil)
	require.NoError(t, err)
	startPrepared(t, s)
	home := grantable(t, s)
	return sandboxRig{s: s, sb: sb, api: api, home: home}
}

// defaultPath is the platform's default PATH, as the shell's: what a run
// searches before the login shell was ever read.
func defaultPath(context.Context) (settings.RunPath, error) {
	var list settings.RunPath
	for _, dir := range filepath.SplitList(bash.DefaultPath) {
		list.Search = append(list.Search, settings.PathFolder{Path: dir, Origin: settings.OriginShell})
	}
	return list, nil
}

// monitorRuns runs one monitor reply on clusterID asking for calls, and answers
// its call rows.
func (r sandboxRig) monitorRuns(t *testing.T, clusterID apimeta.ClusterID, calls ...llm.Block) []storedToolCall {
	t.Helper()
	fakeOf(r.s).SetToolCalls(calls...)
	res, err := r.s.RunMonitor(t.Context(), clusterID, fakeTarget(r.s), "look", admitted)
	require.NoError(t, err)
	rows := toolCallRows(t, r.s.db, res.RunID)
	require.Len(t, rows, len(calls))
	return rows
}

// chatRuns runs one chat turn on cluster 1 asking for command, and answers its
// call row.
func (r sandboxRig) chatRuns(t *testing.T, command string) storedToolCall {
	t.Helper()
	fakeOf(r.s).SetToolCalls(bashCall(command))
	msg := send(t, r.s, nil, "k-"+command, "look")
	awaitSettled(t, r.s, msg.ChatID, msg.ID)
	rows := toolCallRows(t, r.s.db, msg.RunID)
	require.Len(t, rows, 1)
	return rows[0]
}

// curlLine sends one request to the run's cluster through the proxy its
// kubeconfig names, as kubectl does, and prints the answer.
func curlLine(method, path, contentType, body string) string {
	line := `P=$(sed -n 's/^ *proxy-url: //p' "$KUBECONFIG"); curl -sS -x "$P" -H 'Accept: application/json' -X ` + method
	if contentType != "" {
		line += ` -H 'Content-Type: ` + contentType + `' -d '` + body + `'`
	}
	return line + " http://cluster.kubetail.invalid" + path
}

// refusedActions is every action approval recorded, by status and reason.
func refusedActions(t *testing.T, r sandboxRig) [][2]string {
	t.Helper()
	rows, err := r.s.db.Read.Query(`SELECT status, COALESCE(reason, '') FROM approvals WHERE kind = 'action' ORDER BY created_at, id`)
	require.NoError(t, err)
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var a [2]string
		require.NoError(t, rows.Scan(&a[0], &a[1]))
		out = append(out, a)
	}
	require.NoError(t, rows.Err())
	return out
}

// Every change a monitor's command sends is refused at the proxy by the
// read-only mode and recorded, and nothing reaches the cluster; exec, attach,
// port-forward and proxy are refused before any classification, with no record.
func TestAMonitorWriteIsRefusedAndRecorded(t *testing.T) {
	r := startSandboxRig(t)
	writes := []string{
		curlLine("POST", "/api/v1/namespaces/web/pods", "application/json", `{"metadata":{"name":"x"}}`),
		curlLine("PUT", "/api/v1/namespaces/web/pods/x", "application/json", `{"metadata":{"name":"x"}}`),
		curlLine("PATCH", "/api/v1/namespaces/web/pods/x", "application/merge-patch+json", `{"metadata":{"labels":{"a":"b"}}}`),
		curlLine("DELETE", "/api/v1/namespaces/web/pods/x", "", ""),
		curlLine("DELETE", "/api/v1/namespaces/web/pods", "", ""),
		curlLine("PATCH", "/apis/apps/v1/namespaces/web/deployments/api/scale", "application/merge-patch+json", `{"spec":{"replicas":0}}`),
		curlLine("PUT", "/api/v1/namespaces/web/pods/x/status", "application/json", `{"metadata":{"name":"x"}}`),
		curlLine("PATCH", "/api/v1/namespaces/web/pods/x/ephemeralcontainers", "application/strategic-merge-patch+json", `{"spec":{}}`),
		curlLine("POST", "/api/v1/namespaces/web/pods/x/binding", "application/json", `{"target":{"name":"n"}}`),
	}
	reach := []string{
		curlLine("POST", "/api/v1/namespaces/web/pods/x/exec", "", ""),
		curlLine("POST", "/api/v1/namespaces/web/pods/x/attach", "", ""),
		curlLine("POST", "/api/v1/namespaces/web/pods/x/portforward", "", ""),
		curlLine("GET", "/api/v1/namespaces/web/pods/x/proxy/", "", ""),
	}
	var calls []llm.Block
	for _, line := range append(writes, reach...) {
		calls = append(calls, bashCall(line))
	}

	rows := r.monitorRuns(t, "7", calls...)

	for i, row := range rows {
		assert.Contains(t, row.result, "Forbidden", "call %d", i)
	}
	for i := range writes {
		assert.Contains(t, rows[i].result, "it is never allowed", "write %d", i)
	}
	assert.Empty(t, r.api.requests(), "nothing reached the cluster")
	want := make([][2]string, len(writes))
	for i := range want {
		want[i] = [2]string{string(ApprovalRefused), "it is never allowed"}
	}
	assert.Equal(t, want, refusedActions(t, r), "one refused row per write, none for the reads refused before classification")
}

// A monitor's read of Secret data is refused ahead of every rule and recorded,
// and the answer reaches it redacted.
func TestAMonitorReadsASecretRedactedAndRecorded(t *testing.T) {
	r := startSandboxRig(t)

	rows := r.monitorRuns(t, "7",
		bashCall(curlLine("GET", "/api/v1/namespaces/web/secrets/db", "", "")),
		bashCall(curlLine("GET", "/api/v1/namespaces/web/secrets", "", "")))

	for i, row := range rows {
		assert.Contains(t, row.result, "W3JlZGFjdGVkXQ==", "read %d is redacted", i)
		assert.NotContains(t, row.result, "aHVudGVyMg==", "read %d", i)
	}
	refused := [2]string{string(ApprovalRefused), "it is never allowed"}
	assert.Equal(t, [][2]string{refused, refused}, refusedActions(t, r))
}

// A monitor's command never reaches the network: one asking for it is refused
// before it starts, and one that tries anyway reaches nothing outside the run.
func TestAMonitorHasNoNetwork(t *testing.T) {
	r := startSandboxRig(t)
	var hits sync.Mutex
	reached := 0
	outside := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Lock()
		reached++
		hits.Unlock()
	}))
	t.Cleanup(outside.Close)
	asks, err := json.Marshal(map[string]any{"command": "echo hi", "network": true})
	require.NoError(t, err)

	rows := r.monitorRuns(t, "7",
		llm.StagedCall("Bash", string(asks)),
		bashCall("curl -sS --max-time 5 "+outside.URL+" && echo reached"))

	assert.Equal(t, "failed", rows[0].status)
	assert.False(t, rows[0].hasStarted)
	if available, _ := r.sb.NetworkStatus(); available {
		assert.Equal(t, "This session never has network.", rows[0].result)
	} else {
		assert.Contains(t, rows[0].result, "This machine cannot give a sandboxed command the internet")
	}
	assert.NotContains(t, rows[1].result, "reached")
	hits.Lock()
	defer hits.Unlock()
	assert.Zero(t, reached, "a listener outside the run gets no connection")
	assert.Empty(t, r.api.requests())
}

// A folder granted always and one granted to a chat are read by that chat's
// commands and never by the monitor's.
func TestAChatsFoldersNeverReachTheMonitor(t *testing.T) {
	r := startSandboxRig(t)
	always, mine := filepath.Join(r.home, "code", "svc"), filepath.Join(r.home, "mine")
	require.NoError(t, os.MkdirAll(mine, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(always, "a.txt"), []byte("always-granted"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(mine, "b.txt"), []byte("chat-granted"), 0o600))
	require.NoError(t, r.s.GrantFolder(t.Context(), "", always, false))
	read := "cat " + filepath.Join(always, "a.txt") + "; cat " + filepath.Join(mine, "b.txt")

	// The chat's grant is written once the chat exists: its first turn makes it.
	r.chatRuns(t, "true")
	chats, err := r.s.List(t.Context())
	require.NoError(t, err)
	require.Len(t, chats, 1)
	require.NoError(t, r.s.GrantFolder(t.Context(), chats[0].ID, mine, false))
	fakeOf(r.s).SetToolCalls(bashCall(read))
	msg := send(t, r.s, &chats[0].ID, "k-read", "read them")
	awaitSettled(t, r.s, msg.ChatID, msg.ID)
	chat := toolCallRows(t, r.s.db, msg.RunID)
	require.Len(t, chat, 1)
	assert.Contains(t, chat[0].result, "always-granted")
	assert.Contains(t, chat[0].result, "chat-granted")

	monitor := r.monitorRuns(t, "7", bashCall(read))[0]

	assert.Empty(t, grantedFolders(t, monitorSession()))
	assert.NotContains(t, monitor.result, "always-granted")
	assert.NotContains(t, monitor.result, "chat-granted")
}

// A monitor's command cannot read a chat's workspace, and no cluster's monitor
// can read another's folder.
func TestTheMonitorCannotReadAChatsFolder(t *testing.T) {
	r := startSandboxRig(t)
	c := seedChat(t, r.s.db, aChat("7", time.Now()))
	chatFile := writeWorkspaceFile(t, r.s.chatDir(c.ID), "chat-secret")
	otherFile := writeWorkspaceFile(t, r.s.monitorDir("8"), "other-monitor")

	rows := r.monitorRuns(t, "7", bashCall("cat "+chatFile), bashCall("cat "+otherFile))

	assert.Equal(t, "failed", rows[0].status)
	assert.NotContains(t, rows[0].result, "chat-secret")
	assert.Equal(t, "failed", rows[1].status)
	assert.NotContains(t, rows[1].result, "other-monitor")
}

// A chat's command cannot read a monitor's folder.
func TestAChatCannotReadTheMonitorsFolder(t *testing.T) {
	r := startSandboxRig(t)
	monitorFile := writeWorkspaceFile(t, r.s.monitorDir("1"), "monitor-notes")

	row := r.chatRuns(t, "cat "+monitorFile)

	assert.Equal(t, "failed", row.status)
	assert.NotContains(t, row.result, "monitor-notes")
}

// writeWorkspaceFile writes text to notes.txt in dir's workspace and answers
// its path.
func writeWorkspaceFile(t *testing.T, dir tools.ChatDir, text string) string {
	t.Helper()
	root, err := dir.Root(true)
	require.NoError(t, err)
	require.NoError(t, root.MkdirAll("workspace", 0o700))
	require.NoError(t, root.WriteFile("workspace/notes.txt", []byte(text), 0o600))
	require.NoError(t, root.Close())
	return filepath.Join(dir.Path(), "workspace", "notes.txt")
}
