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

package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/providercredentials"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/grpc/authpb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/catalog"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/app/layout"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/filelock"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/lifecycle"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/anthropicwebsearch"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/bash"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/taskstop"
)

// Read, Memory, Write, Edit and WebFetch work on every machine, so a machine with no shell is
// offered them, and TaskStop, since agents and watches start tasks there too.
func TestTheFileToolsAreOfferedWithoutAShell(t *testing.T) {
	box, err := chatTools(toolDeps{fenced: []string{t.TempDir()}, umask: 0o022})
	require.NoError(t, err)

	defs, native := box.Offer()
	assert.Equal(t, []string{"Read", "Memory", "Write", "Edit", "WebFetch", "TaskStop", "Agent", "KubeQuery", "LogsView", "LogsRead", "LogsWatch"}, definitionNames(defs))
	require.Len(t, native, 1)
	assert.Equal(t, anthropicwebsearch.Name, native[0].Tool.Name())
	got, ok := box.Action(bash.Name, bash.ContractName, json.RawMessage(`{"command":"ls"}`), "/srv", false)
	assert.True(t, ok, "a stored bash call still shows")
	assert.Equal(t, "ls", got.Command.Text)
}

// The file tools fence Kubetail's directories, so a box with none to fence is refused.
func TestChatToolsRefusesAnEmptyFence(t *testing.T) {
	_, err := chatTools(toolDeps{umask: 0o022})
	require.ErrorContains(t, err, "a fence needs a directory")
}

// A machine with no shell still knows what every stored call did, a TaskStop
// included.
func TestAStoredCallsKindIsKnownWithoutAShell(t *testing.T) {
	box, err := chatTools(toolDeps{fenced: []string{t.TempDir()}, umask: 0o022})
	require.NoError(t, err)

	for name, want := range map[string]tools.ActionKind{taskstop.Name: tools.ActionStop, bash.Name: tools.ActionCommand} {
		got, ok := box.ActionKind(name)
		assert.True(t, ok, name)
		assert.Equal(t, want, got, name)
	}
}

// definitionNames is each definition's name, in order.
func definitionNames(defs []llm.ToolDefinition) []string {
	var out []string
	for _, d := range defs {
		out = append(out, d.Name)
	}
	return out
}

// Every model of every catalog is offered each contract it lists, and an offer
// its target's Stream accepts: a contract the box cannot offer, or a call name
// spelled twice, shows up here rather than on a user's turn.
func TestEveryCatalogTargetTakesItsBox(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"refused"}}`))
	}))
	t.Cleanup(srv.Close)
	box := everyTool(t)
	lists := everyCatalog(t)

	for _, p := range lists.Providers() {
		p.BaseURL = srv.URL
		for _, m := range p.Catalog {
			target := llm.Target{Provider: p, Model: m}
			turn := box.For(target, lists.ToolsFor(target))

			defs, native := turn.Offer()
			_, err := target.Stream(t.Context(), "", nil, "", defs, native, func(llm.Chunk) {})
			assert.NotErrorIs(t, err, llm.ErrToolsUnsupported, "%s/%s", p.ID, m.ID)
		}
	}
}

// everyCatalog is the catalog with every provider it can list, each keyed.
func everyCatalog(t *testing.T) catalog.Catalog {
	keys := map[string]string{}
	for id := range catalog.KeyVars() {
		keys[id] = "key-0123456789abcdef"
	}
	return catalog.New(catalog.Config{Credentials: newProviderCredentials(t, keys, nil), Fake: llm.NewFake(0)})
}

// newProviderCredentials is every provider's credentials service over an
// empty keyring, holding keys as the environment's.
func newProviderCredentials(t *testing.T, keys, baseURLs map[string]string) *providercredentials.Service {
	t.Helper()
	s, err := providercredentials.New(providercredentials.Config{Keyring: oskeyringtest.New(), Providers: catalog.Providers(baseURLs, ""), EnvKeys: keys})
	require.NoError(t, err)
	t.Cleanup(s.Close)
	return s
}

// everyToolTarget is every target of cat whose model takes tools.
func everyToolTarget(cat catalog.Catalog) []llm.Target {
	var out []llm.Target
	for _, p := range cat.Providers() {
		for _, m := range p.Catalog {
			if m.Tools {
				out = append(out, llm.Target{Provider: p, Model: m})
			}
		}
	}
	return out
}

// everyTool is the box of a machine with a shell: every tool the app builds.
func everyTool(t *testing.T) tools.Box {
	t.Helper()
	box, err := chatTools(toolDeps{shell: &bash.Tool{}, fenced: []string{t.TempDir()}, umask: 0o022})
	require.NoError(t, err)
	return box
}

// offerNames is the name of every tool box offers, in box order.
func offerNames(box tools.Box) []string {
	defs, native := box.Offer()
	names := definitionNames(defs)
	for _, o := range native {
		names = append(names, o.Tool.Name())
	}
	return names
}

// Every target is offered exactly its list: each listed name is a tool the app
// builds, and each listed vendor tool rides that target's wire.
func TestEachTargetIsOfferedItsList(t *testing.T) {
	box := everyTool(t)
	lists := everyCatalog(t)

	for _, target := range everyToolTarget(lists) {
		turn := box.For(target, lists.ToolsFor(target))
		assert.ElementsMatch(t, lists.ToolsFor(target), offerNames(turn), "%s/%s", target.Provider.ID, target.Model.ID)
	}
}

// No list names two tools of one kind, and every list covers each kind our
// tools cover.
func TestEachListIsOneToolOfEachKind(t *testing.T) {
	box := everyTool(t)
	lists := everyCatalog(t)
	defs, _ := box.Offer()
	var ours []tools.ActionKind
	for _, name := range definitionNames(defs) {
		k, ok := box.ActionKind(name)
		require.True(t, ok, name)
		ours = append(ours, k)
	}

	for _, target := range everyToolTarget(lists) {
		id := target.Provider.ID + "/" + target.Model.ID
		var kinds []tools.ActionKind
		for _, name := range lists.ToolsFor(target) {
			k, ok := box.ActionKind(name)
			require.True(t, ok, "%s lists %s", id, name)
			assert.NotContains(t, kinds, k, "%s lists two tools of kind %s", id, k)
			kinds = append(kinds, k)
		}
		assert.Subset(t, kinds, ours, id)
	}
}

// Every tool the app builds is on a list, so a new tool left off every list
// fails here rather than never being offered.
func TestEveryToolIsOnAList(t *testing.T) {
	lists := everyCatalog(t)
	listed := map[string]bool{}
	for _, target := range everyToolTarget(lists) {
		for _, name := range lists.ToolsFor(target) {
			listed[name] = true
		}
	}

	for _, name := range offerNames(everyTool(t)) {
		assert.True(t, listed[name], name)
	}
}

// Every tool of ours answers a contract of the form `<name>/v<n>`, its name
// lower-cased; NewBox holds them unique.
func TestEveryToolOfOursHasAContractOfItsForm(t *testing.T) {
	box := everyTool(t)
	defs, _ := box.Offer()

	for _, name := range definitionNames(defs) {
		c := box.ContractOf(name)
		assert.Regexp(t, `^[a-z]+/v[1-9][0-9]*$`, string(c), name)
		assert.True(t, strings.HasPrefix(string(c), strings.ToLower(name)+"/"), name)
	}
}

// newTestApp builds an App backed by a kubeconfig with two contexts, then serves
// it over httptest (HTTP/1.1 for GraphQL/SSE, prior-knowledge h2c for gRPC —
// exactly the production split). It does NOT call Start(), so the cluster-cache coordinator
// never runs; the lifecycle surface is what's under test.
func newTestApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	writeKubeconfig(t, kubeconfig, "context-A", "context-B")

	a, err := New(t.Context(), withDirs(t, Config{
		KubeconfigPath: kubeconfig,
	}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = a.Close() })

	return a, kubeconfig
}

// withDirs gives a config the folders it leaves empty, from testFolders,
// and no cloud platform unless it brings one, so no test runs the machine's
// own gcloud.
func withDirs(t *testing.T, cfg Config) Config {
	t.Helper()
	f := testFolders(t)
	for _, dir := range []struct {
		dst  *string
		from string
	}{
		{&cfg.Folders.Data, f.Data}, {&cfg.Folders.Cache, f.Cache}, {&cfg.Folders.Runtime, f.Runtime},
	} {
		if *dir.dst == "" {
			*dir.dst = dir.from
		}
	}
	if cfg.Folders.Layout == (layout.Layout{}) {
		cfg.Folders.Layout = f.Layout
	}
	if cfg.cloudPlatforms == nil {
		cfg.cloudPlatforms = []bash.CloudPlatform{}
	}
	if cfg.Keyring == nil {
		cfg.Keyring = oskeyringtest.New()
	}
	return cfg
}

// writeKubeconfig writes a kubeconfig at path naming each context over one
// cluster and user.
func writeKubeconfig(t *testing.T, path string, contexts ...string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Config\ncurrent-context: context-A\ncontexts:\n")
	for _, name := range contexts {
		fmt.Fprintf(&b, "- name: %s\n  context: {cluster: c, user: u}\n", name)
	}
	b.WriteString("clusters:\n- name: c\n  cluster: {server: https://example}\nusers:\n- name: u\n  user: {}\n")
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))
}

// graphql POSTs one query to the app and returns the raw response.
func graphql(t *testing.T, srvURL, query string) string {
	t.Helper()
	raw, err := postGraphQL(srvURL, query)
	require.NoError(t, err)
	return raw
}

// postGraphQL is graphql answering its error, for a condition that runs off
// the test's goroutine.
func postGraphQL(srvURL, query string) (string, error) {
	body, _ := json.Marshal(map[string]string{"query": query})
	req, err := http.NewRequest(http.MethodPost, srvURL+"/graphql", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	return string(raw), err
}

// firstFrame opens subscription on the app and returns the data of its first
// frame: a state watch's value as it is now.
func firstFrame(t *testing.T, srvURL, subscription string) string {
	t.Helper()
	frames, closeFrames, err := subscribe(srvURL, subscription)
	require.NoError(t, err)
	defer closeFrames()
	return testutil.Recv(t, frames, "the first frame")
}

// clusterSnapshot is the cluster collection watch's snapshot, each cluster
// selecting id and selection, answering its error for a condition that runs off
// the test's goroutine.
func clusterSnapshot(srvURL, selection string) ([]json.RawMessage, error) {
	frames, closeFrames, err := subscribe(srvURL, `subscription { clusterCollectionWatch { type cluster { id `+selection+` } } }`)
	if err != nil {
		return nil, err
	}
	defer closeFrames()
	var clusters []json.RawMessage
	for data := range frames {
		var frame struct {
			Data struct {
				Watch struct {
					Type    string          `json:"type"`
					Cluster json.RawMessage `json:"cluster"`
				} `json:"clusterCollectionWatch"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(data), &frame); err != nil {
			return nil, err
		}
		if frame.Data.Watch.Type == "Bookmark" {
			return clusters, nil
		}
		clusters = append(clusters, frame.Data.Watch.Cluster)
	}
	return nil, errors.New("the watch ended before its Bookmark")
}

// subscribe opens subscription on the app and sends the data of each frame on
// frames until stop is called.
func subscribe(srvURL, subscription string) (frames <-chan string, stop func(), err error) {
	body, _ := json.Marshal(map[string]string{"query": subscription})
	req, err := http.NewRequest(http.MethodPost, srvURL+"/graphql", bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	out := make(chan string)
	done := make(chan struct{})
	go func() {
		defer close(out)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(nil, 1<<20)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				select {
				case out <- data:
				case <-done:
					return
				}
			}
		}
	}()
	return out, func() { close(done); _ = resp.Body.Close() }, nil
}

// startApp starts a and registers its stop.
func startApp(t *testing.T, a *App) {
	t.Helper()
	stop, err := a.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testutil.Timeout)
		defer cancel()
		assert.NoError(t, stop(ctx))
	})
}

// The chat surface answers over the app's own handler: the composition root is the
// one place the resolver's chat service is set, and an unwired one panics rather
// than refusing. The id is well-formed, so the refusal is the lookup's.
func TestAppServesTheChatSurface(t *testing.T) {
	a, err := New(t.Context(), withDirs(t, Config{}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })
	srv := httptest.NewServer(a)
	t.Cleanup(srv.Close)

	raw := graphql(t, srv.URL, `mutation { chatRename(id: "`+appdb.NewID()+`", title: "t") { id } }`)

	assert.Contains(t, raw, `"code":"KUBETAIL_RECORD_NOT_FOUND"`)
}

// The whole cluster lifecycle through the app: a context is imported, toggled, chatted
// under, and deleted once it has left the file. The mark tears the runtime and the
// chats down behind it, and the row goes last.
func TestAppTearsDownADeletedCluster(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kubeconfig")
	writeKubeconfig(t, path, "context-A", "context-B")
	a, err := New(t.Context(), withDirs(t, Config{KubeconfigPath: path, Folders: Folders{Data: dir}, AddFake: true}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })
	startApp(t, a)
	srv := httptest.NewServer(a)
	t.Cleanup(srv.Close)

	// converge bounds each wait: a file watcher, beehive's queue and the mirror's
	// pass sit between a write and its effect.
	const converge = 10 * time.Second
	clusterFor := func(contextName string) (id string, present bool, ok bool) {
		clusters, err := clusterSnapshot(srv.URL, `spec { source { kubeconfig { context } } } status { source { kubeconfig { isPresent } } }`)
		if err != nil {
			return "", false, false
		}
		for _, raw := range clusters {
			var c struct {
				ID   string `json:"id"`
				Spec struct {
					Source struct {
						Kubeconfig *struct {
							Context string `json:"context"`
						} `json:"kubeconfig"`
					} `json:"source"`
				} `json:"spec"`
				Status struct {
					Source struct {
						Kubeconfig *struct {
							IsPresent bool `json:"isPresent"`
						} `json:"kubeconfig"`
					} `json:"source"`
				} `json:"status"`
			}
			if json.Unmarshal(raw, &c) != nil {
				return "", false, false
			}
			if c.Spec.Source.Kubeconfig != nil && c.Spec.Source.Kubeconfig.Context == contextName {
				observed := c.Status.Source.Kubeconfig
				return c.ID, observed != nil && observed.IsPresent, true
			}
		}
		return "", false, false
	}

	var id string
	require.Eventually(t, func() bool {
		var present, ok bool
		id, present, ok = clusterFor("context-B")
		return ok && present
	}, converge, 10*time.Millisecond, "context-B imported and observed present")

	raw := graphql(t, srv.URL, `mutation { clusterSyncEnabledSet(id: "`+id+`", syncEnabled: false) { spec { syncEnabled } } }`)
	assert.Contains(t, raw, `"syncEnabled":false`)

	raw = graphql(t, srv.URL, `mutation { chatSend(mode: Chat, clusterID: "`+id+`", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, providerID: "fake", modelID: "fake", effort: "high", requestID: "`+appdb.NewID()+`", content: "hello") { chatID } }`)
	var sent struct {
		Data struct {
			ChatSend struct {
				ChatID string `json:"chatID"`
			} `json:"chatSend"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &sent), raw)
	chatID := sent.Data.ChatSend.ChatID
	require.NotEmpty(t, chatID, raw)

	// While the file still declares the context the delete is refused.
	raw = graphql(t, srv.URL, `mutation { clusterDelete(id: "`+id+`") }`)
	assert.Contains(t, raw, "KUBETAIL_CONFLICT")

	writeKubeconfig(t, path, "context-A")
	require.Eventually(t, func() bool {
		_, present, ok := clusterFor("context-B")
		return ok && !present
	}, converge, 10*time.Millisecond, "context-B observed absent")

	raw = graphql(t, srv.URL, `mutation { clusterDelete(id: "`+id+`") }`)
	assert.Contains(t, raw, `"clusterDelete":true`)
	assert.Contains(t, graphql(t, srv.URL, `mutation { clusterDelete(id: "`+id+`") }`), `"clusterDelete":true`, "a repeat")

	require.Eventually(t, func() bool {
		_, _, ok := clusterFor("context-B")
		return !ok
	}, converge, 10*time.Millisecond, "the row removed once the runtime and the chats are gone")
	assert.Contains(t, graphql(t, srv.URL, `mutation { chatRename(id: "`+chatID+`", title: "t") { id } }`), "KUBETAIL_RECORD_NOT_FOUND")
}

// The cluster reconciles read the kubeconfig on their first pass and defer while it
// reports unread, which is a state only this ordering keeps them out of: Start reads
// synchronously, so a service started after it always observes a read. Reversed, every
// record would observe its context absent and orphan itself — a mass write, not a
// pause — and the guards in cluster are the only thing that would stand between the
// reordering and that.
func TestAppStartsKubeconfigBeforeTheClusterService(t *testing.T) {
	a, err := New(t.Context(), withDirs(t, Config{}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })

	assert.Less(t, partIndex(t, a, "kubeconfig service"), partIndex(t, a, "cluster service"))
}

// The logs service owns the log files and is built over the cluster service, so it
// closes first; the cache pass still removes a cache's file through it after that.
func TestAppAddsTheLogsServiceAfterTheClusterService(t *testing.T) {
	a, err := New(t.Context(), withDirs(t, Config{}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })

	assert.Greater(t, partIndex(t, a, "logs service"), partIndex(t, a, "cluster service"))
}

// The Kubetail Cloud connector holds app.db's statements and a revocation's goroutine,
// so it is a part that closes before app.db does.
func TestAppClosesTheKubetailCloudConnectorBeforeAppDB(t *testing.T) {
	a, err := New(t.Context(), withDirs(t, Config{}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })

	assert.Greater(t, partIndex(t, a, "kubetail cloud service"), partIndex(t, a, "app.db"))
}

// The chat service is a part like the others: built by New, started and stopped
// with the app.
func TestAppStartsAndStopsWithTheChatService(t *testing.T) {
	a, err := New(t.Context(), withDirs(t, Config{}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })

	require.GreaterOrEqual(t, partIndex(t, a, "chat service"), 0)

	stop, err := a.Start(t.Context())
	require.NoError(t, err)
	require.NoError(t, stop(t.Context()))
}

// The memory service starts before the chat service and so stops after it: a
// turn reads the index and saves through it until the chat service has stopped.
func TestTheMemoryServiceOutlivesTheChatService(t *testing.T) {
	a, err := New(t.Context(), withDirs(t, Config{}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })

	assert.Less(t, partIndex(t, a, "memory service"), partIndex(t, a, "chat service"))
}

// The UIDs memory stamps are the cluster service's last probe; a cluster it
// does not know, or has never probed, has none.
func TestServerUIDLookupReadsTheClusterService(t *testing.T) {
	uid := "uid-1"
	uids := serverUIDLookup{clusters: fakeClusters{"probed": {Status: cluster.ClusterStatus{Server: cluster.ClusterServer{UID: &uid}}}, "unprobed": {}}}

	assert.Equal(t, "uid-1", uids.ServerUID(t.Context(), "probed"))
	assert.Empty(t, uids.ServerUID(t.Context(), "unprobed"))
	assert.Empty(t, uids.ServerUID(t.Context(), "unknown"))
}

type fakeClusters map[apimeta.ClusterID]*cluster.Cluster

func (f fakeClusters) Get(_ context.Context, id apimeta.ClusterID) (*cluster.Cluster, error) {
	return f[id], nil
}

// partIndex returns where the named part sits in start order.
func partIndex(t *testing.T, a *App, name string) int {
	t.Helper()
	i := slices.IndexFunc(a.rt.parts, func(p lifecycle.Part) bool { return p.Name == name })
	require.GreaterOrEqual(t, i, 0, "no part named %q", name)
	return i
}

// With no OAuth config (the standalone/test default), the account surface is
// wired through composition but degraded: the auth state watch answers signed-out
// instead of panicking. This is also the canary that the composed App is a
// working http.Handler with the GraphQL surface wired through composition.
func TestAppAuthStateDegradesSignedOut(t *testing.T) {
	a, _ := newTestApp(t)
	ts := httptest.NewServer(a)
	defer ts.Close()

	frame := firstFrame(t, ts.URL, `subscription { authStateWatch { authenticated identity { sub } } }`)
	assert.Contains(t, frame, `"identity":null`)
	assert.Contains(t, frame, `"authenticated":false`)
}

// TestAppShutdownEndsBothTransports pins main's shutdown order: with a live SSE
// subscription AND a live gRPC AuthStateWatch stream open, http.Server.Shutdown
// fires CancelStreams, both streams end cleanly, and Shutdown returns at once.
// Shutdown waits on the gRPC stream itself, so a stream left dangling would run
// it into its deadline.
func TestAppShutdownEndsBothTransports(t *testing.T) {
	a, _ := newTestApp(t)
	ts := serveApp(t, a)
	ts.Config.RegisterOnShutdown(a.CancelStreams)

	// Live SSE subscription.
	sseReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/graphql", strings.NewReader(`{"query":"subscription { authStateWatch { authenticated } }"}`))
	sseReq.Header.Set("Content-Type", "application/json")
	sseReq.Header.Set("Accept", "text/event-stream")
	sseResp, err := http.DefaultClient.Do(sseReq)
	require.NoError(t, err)
	defer sseResp.Body.Close()
	buf := make([]byte, 1)
	_, err = sseResp.Body.Read(buf) // ensure the stream is established
	require.NoError(t, err)

	// Live gRPC AuthStateWatch stream over h2c on the same listener.
	conn, err := grpc.NewClient(strings.TrimPrefix(ts.URL, "http://"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()
	stream, err := authpb.NewAuthServiceClient(conn).AuthStateWatch(context.Background(), &authpb.AuthStateWatchRequest{})
	require.NoError(t, err)
	snap, err := stream.Recv()
	require.NoError(t, err)
	assert.False(t, snap.GetAuthenticated())

	grpcRecvErr := make(chan error, 1)
	go func() {
		_, e := stream.Recv()
		grpcRecvErr <- e
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, ts.Config.Shutdown(ctx))

	// The gRPC stream ended cleanly (OK trailers → EOF), not cut mid-flight.
	assert.ErrorIs(t, testutil.Recv(t, grpcRecvErr, "the gRPC Watch to end"), io.EOF)
}

// A data dir that is not a directory fails at the first subsystem that needs
// one: New reports it rather than returning a half-built App.
func TestAppRejectsAnUnusableDataDir(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(notADir, []byte("x"), 0o600))

	_, err := New(t.Context(), withDirs(t, Config{Folders: Folders{Data: notADir}}))
	require.Error(t, err)
}

// requireUnlocked fails the test while anything holds the data directory's lock.
func requireUnlocked(t *testing.T, dataDir string) {
	t.Helper()
	f, err := filelock.TryLock(filepath.Join(dataDir, "sidecar.lock"))
	require.NoError(t, err, "the data directory is still locked")
	require.NoError(t, f.Close())
}

// A second app on a data directory is refused, naming the directory, until the
// first closes. The lock is the first part, so it closes after app.db.
func TestASecondAppOnTheDataDirectoryIsRefused(t *testing.T) {
	cfg := withDirs(t, Config{})
	first, err := New(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	assert.Equal(t, 0, partIndex(t, first, "data directory lock"))

	second := cfg
	second.Folders.Cache, second.Folders.Runtime = t.TempDir(), t.TempDir()
	_, err = New(t.Context(), second)
	require.EqualError(t, err, "another Kubetail is using "+cfg.Folders.Data)

	require.NoError(t, first.Close())
	requireUnlocked(t, cfg.Folders.Data)
}

// A lock file that cannot be opened stops the build before app.db opens.
func TestALockFileThatCannotBeOpenedFailsNew(t *testing.T) {
	cfg := withDirs(t, Config{})
	require.NoError(t, os.Mkdir(filepath.Join(cfg.Folders.Data, "sidecar.lock"), 0o700))

	_, err := New(t.Context(), cfg)
	require.ErrorContains(t, err, "lock the data directory")
	assert.NoFileExists(t, filepath.Join(cfg.Folders.Data, "app.db"))
}

// The app opens app.db once, right after the lock in parts, so the reverse close
// order releases it after every service. SQLite deletes the -wal beside a database
// when its last connection closes, so the sibling's absence shows the file was released.
func TestAppOwnsTheDatabase(t *testing.T) {
	dir := t.TempDir()
	a, err := New(t.Context(), withDirs(t, Config{Folders: Folders{Data: dir}}))
	require.NoError(t, err)
	assert.Equal(t, 1, partIndex(t, a, "app.db"))

	wal := filepath.Join(dir, "app.db-wal")
	require.FileExists(t, wal)
	require.NoError(t, a.Close())
	assert.NoFileExists(t, wal)
}

// A subfolder that cannot be made fails the build before any service opens a
// file, and releases the lock.
func TestASubfolderThatCannotBeMadeFailsBeforeAnyService(t *testing.T) {
	cfg := withDirs(t, Config{})
	require.NoError(t, os.WriteFile(filepath.Join(cfg.Folders.Data, "chats"), nil, 0o600))

	_, err := New(t.Context(), cfg)
	require.ErrorContains(t, err, "chats is not a folder")
	assert.NoFileExists(t, filepath.Join(cfg.Folders.Data, "app.db"))
	requireUnlocked(t, cfg.Folders.Data)
}

// A constructor failing after the open closes the file. A service's statements fail
// to prepare on a database whose migration is recorded but whose tables are gone.
func TestAppClosesTheDatabaseWhenAConstructorFails(t *testing.T) {
	for table, service := range map[string]string{"clusters": "cluster", "approvals": "chat", "memories": "memory", "env_vars": "variables"} {
		t.Run(service, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "app.db")
			db, err := appdb.Open(path, 0)
			require.NoError(t, err)
			_, err = db.Write.Exec(`DROP TABLE ` + table)
			require.NoError(t, err)
			require.NoError(t, db.Close())

			_, err = New(t.Context(), withDirs(t, Config{Folders: Folders{Data: dir}}))
			require.ErrorContains(t, err, table)
			assert.NoFileExists(t, path+"-wal")
		})
	}
}

// A KUBETAIL_FAULTS spec the build cannot honour stops the start naming the variable,
// which run prints as FATAL for the start screen.
func TestAFaultSpecTheBuildCannotHonourFailsNew(t *testing.T) {
	_, err := New(t.Context(), withDirs(t, Config{Faults: "get pods=teapot"}))

	require.ErrorContains(t, err, "KUBETAIL_FAULTS: ")
}

// The catalog is the config's: each provider at the base URL a debug build
// moved it to, then the fake when asked for.
func TestTheCatalogIsTheConfigs(t *testing.T) {
	cfg := Config{
		LLMBaseURLs: map[string]string{"anthropic": "http://127.0.0.1:1"},
		AddFake:     true,
	}
	keys := newProviderCredentials(t, map[string]string{"anthropic": "sk-ant-test-0123456789abcdef"}, cfg.LLMBaseURLs)
	providers := newCatalog(cfg, keys).Providers()

	require.Len(t, providers, len(catalog.Providers(nil, ""))+1)
	assert.Equal(t, keys.Signer("anthropic"), providers[0].Signer)
	assert.Equal(t, "http://127.0.0.1:1", providers[0].BaseURL)
	assert.Equal(t, "fake", providers[len(providers)-1].ID)
}

// The fake is a debug build's alone, and last: a release-shaped config lists only
// the rows it was given.
func TestTheFakeIsListedOnlyWhenAsked(t *testing.T) {
	// The models are the catalogs flattened, so the providers are read off them in
	// order, each named once however many models it lists.
	providers := func(addFake bool) []string {
		a, err := New(t.Context(), withDirs(t, Config{LLMKeys: map[string]string{"anthropic": "sk-ant-test"}, AddFake: addFake}))
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, a.Close()) })
		ts := serveApp(t, a)
		var body struct {
			Data struct {
				Models []struct{ Provider struct{ ID string } } `json:"modelsStateWatch"`
			}
		}
		require.NoError(t, json.Unmarshal([]byte(firstFrame(t, ts.URL, `subscription { modelsStateWatch { provider { id } } }`)), &body))
		var ids []string
		for _, m := range body.Data.Models {
			if len(ids) == 0 || ids[len(ids)-1] != m.Provider.ID {
				ids = append(ids, m.Provider.ID)
			}
		}
		return ids
	}

	assert.Equal(t, []string{"anthropic"}, providers(false))
	assert.Equal(t, []string{"anthropic", "fake"}, providers(true))
}

// Sandboxed Bash is offered where a shell was found and the probe found a
// sandbox; the reason is the probe's, or that there is no shell.
func TestTheSandboxStateIsTheShellAndTheProbe(t *testing.T) {
	found := sandbox.State{Available: true, Reason: "bwrap at /usr/bin/bwrap"}
	missing := sandbox.State{Reason: "bwrap was not found"}

	assert.Equal(t, sandbox.State{Available: true, Reason: "bwrap at /usr/bin/bwrap"}, sandboxStateOf(true, found))
	assert.Equal(t, sandbox.State{Reason: "bwrap was not found"}, sandboxStateOf(true, missing))
	assert.Equal(t, sandbox.State{Reason: "no shell was found"}, sandboxStateOf(false, found))
}

// A sandbox with no shell confines no command, so it offers no network either.
func TestNoShellOffersNoNetwork(t *testing.T) {
	found := sandbox.State{Available: true, Reason: "bwrap at /usr/bin/bwrap", NetworkAvailable: true}

	assert.Equal(t, found, sandboxStateOf(true, found))
	got := sandboxStateOf(false, found)
	assert.False(t, got.NetworkAvailable)
	assert.Empty(t, got.NetworkReason)
}

// On a machine with no sandbox the service stores no shell PATH and keeps no
// fault, and a write that fails at Start is a warning, not a startup error.
func TestASecurityServiceWithNoSandboxSyncsNothing(t *testing.T) {
	store, err := settings.Open(appdbtest.Open(t))
	require.NoError(t, err)
	svc := newSettingsService(store, nil, nil, sandbox.State{}, bash.Paths{}, "timeout")
	assert.Empty(t, svc.PathFault())

	log := testutil.CaptureLogs(t)
	assert.False(t, setShellPath(t.Context(), svc, []string{t.TempDir()}), "nothing changed")
	assert.Contains(t, log.String(), "shell PATH not stored")
}

// A probe after a read of the shell probes the registered executables as
// read; a read that fails starts no probe, the curated executables' included,
// and is a warning.
func TestAProbeAfterAShellReadReadsTheRegisteredExecutables(t *testing.T) {
	k9s := []settings.Executable{{Name: "k9s", Invocation: "k9s version"}}
	var started [][]settings.Executable
	start := func(registered []settings.Executable) { started = append(started, registered) }

	probeExecutables(t.Context(), func(context.Context) ([]settings.Executable, error) { return k9s, nil }, start)
	assert.Equal(t, [][]settings.Executable{k9s}, started)

	log := testutil.CaptureLogs(t)
	probeExecutables(t.Context(), func(context.Context) ([]settings.Executable, error) { return nil, errors.New("boom") }, start)
	assert.Len(t, started, 1, "no probe started")
	assert.Contains(t, log.String(), "executables not read")
}

// A folder under internal/ is a layer: an import between two folders points
// from right to left in this order. A new package goes in the folder its
// imports allow; a cross-folder import the other way reopens the ADR rather
// than adding the edge.
// → docs/adr/2026-10-07-sidecar-folders-are-layers.md
func TestInternalFoldersAreLayers(t *testing.T) {
	const module = "github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/"
	order := []string{"lib", "appdb", "llm", "run", "services", "tools", "agent", "app"}
	rank := func(folder string) int {
		i := slices.Index(order, folder)
		require.NotEqual(t, -1, i, "folder %q is not in the layer order", folder)
		return i
	}
	folderOf := func(pkg string) string { return strings.SplitN(pkg, "/", 2)[0] }

	root := filepath.Join("..")
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, err := filepath.Rel(root, path)
		require.NoError(t, err)
		src := folderOf(filepath.ToSlash(rel))
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		require.NoError(t, err, rel)
		for _, imp := range f.Imports {
			target, ok := strings.CutPrefix(strings.Trim(imp.Path.Value, `"`), module)
			if !ok {
				continue
			}
			dst := folderOf(target)
			if dst == src {
				continue
			}
			assert.Greater(t, rank(src), rank(dst), "%s imports %s: %s may not import %s", rel, target, src, dst)
		}
		return nil
	})
	require.NoError(t, err)
}
