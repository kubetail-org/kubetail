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

// Package app is the sidecar's composition root and lifecycle owner. It builds
// every service into one Runtime, wires the GraphQL and gRPC servers over it,
// and multiplexes them onto one handler served as HTTP/1.1 and h2c.
// main() stays thin: it binds the listener and drives the shutdown surface this
// package exposes — CancelStreams, CloseRequests, WaitRequests, Close.
package app

import (
	"context"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"time"

	"golang.org/x/net/http/httpproxy"
	"google.golang.org/grpc"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/graph"
	grpcserver "github.com/kubetail-org/kubetail/apps/desktop/sidecar/grpc"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/catalog"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/lifecycle"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/memory"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/providercredentials"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/agent"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/anthropicwebsearch"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/bash"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/edit"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/kubequery"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/logsread"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/logsview"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/logswatch"
	memorytool "github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/memory"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/read"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/taskstop"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/webfetch"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/write"
)

// defaultKeychainService is the OS-keychain service name used when
// Config.KeychainService is empty; deliberately frontend-neutral.
const defaultKeychainService = "Kubetail"

// Config is the subset of process configuration the composition root needs.
// main() resolves these from flags/env and hands them over.
type Config struct {
	// KubeconfigPath is an explicit kubeconfig path; empty uses clientcmd's
	// default-loading rules.
	KubeconfigPath string
	// Folders is where Kubetail keeps its files, from ResolveFolders: the data
	// holds what a user would lose, the cache what Kubetail rebuilds, and the
	// runtime folder what lives for a session. Its layout is every folder of
	// Kubetail's on this platform, the host's included. Each is fenced, so a
	// grant of the home opens none of them.
	Folders Folders
	// OAuthIssuerURL is Kubetail's OAuth issuer, whose discovery document names
	// the endpoints.
	OAuthIssuerURL string
	// OAuthClientID is the app's public (PKCE/loopback) client there.
	OAuthClientID string
	// GitHubClientID is Kubetail's GitHub OAuth App, a public client the device
	// flow signs in under. Empty refuses the GitHub connector's Connect.
	GitHubClientID string
	// GoogleClientID and GoogleClientSecret are Kubetail's Google client for
	// installed apps, whose secret Google requires and no installed app can keep
	// confidential. An empty id refuses the Google connector's Connect.
	GoogleClientID     string
	GoogleClientSecret string
	// Getenv reads the process environment once New has imported the login
	// shell's, for the Google Cloud project the Google connector's models run
	// in. main alone sets it (os.Getenv); nil reads nothing, so a test's app
	// sees none of the developer's project.
	Getenv func(string) string
	// googleProject is the project Getenv names and vertexBaseURL its Vertex AI
	// endpoint; build sets both, empty when there is none.
	googleProject string
	vertexBaseURL string
	// KeychainService is the OS-keychain service name for the connectors' tokens. Empty
	// uses defaultKeychainService; dev runs set a distinct name so dev and
	// release don't share a keychain entry.
	KeychainService string
	// LLMKeys is the model-provider key per provider id, for the keys the config
	// found set; with RunLoginShell, New adds each key the login shell sets that
	// is missing here. A saved key or a connector signs over it.
	LLMKeys map[string]string
	// LLMBaseURLs moves a listed provider's base URL, by provider id. Only a
	// debug build sets it.
	LLMBaseURLs map[string]string
	// ConnectorAuthURLs moves where a connector signs in, by its provider id:
	// an OAuth issuer, OpenRouter's or GitHub's auth origin, or AWS's sign-in.
	// Only a debug build sets it.
	ConnectorAuthURLs map[string]string
	// AddFake adds the fake to the catalog, last, so a dev run answers with no
	// key. Debug builds only.
	AddFake bool
	// Faults is KUBETAIL_FAULTS, the request failures every cluster connection
	// injects (docs/development-switches.md). Only a debug build sets it.
	Faults string
	// fake is a test's own fake, added as AddFake adds one, so the test can
	// stage its replies.
	fake *llm.Fake
	// cloudPlatforms, when a test sets them, stand in place of the build's.
	cloudPlatforms []bash.CloudPlatform
	// Keyring stands in for the OS keyring in every service; nil is the OS
	// keyring under KeychainService. A test sets it, so no test asks the real one.
	Keyring oskeyring.Keyring
	// HostPID is the host's process, which a command's kill is refused against;
	// 0 when the sidecar was not told it.
	HostPID int
	// RunLoginShell runs the user's login shell: once in New, for its PATH, the
	// provider keys LLMKeys lacks and on macOS its environment, and for the
	// snapshot after Start. main alone sets
	// it, so no test spawns the developer's login shell.
	RunLoginShell bool
	// launchPath is a test's stand-in for the PATH the login shell answers,
	// used while RunLoginShell is false.
	launchPath []string
	// callDeadline is a test's stand-in for what starts a Bash call's
	// deadline, so the test fires the timeout; nil for time.After.
	callDeadline func(d time.Duration) <-chan time.Time
	// UserUmask is the umask the process started with, before main made it
	// owner-only: a file Write makes for the user takes it. Zero on Windows.
	UserUmask fs.FileMode
}

// App owns the composed sidecar: one handler fronting the GraphQL and gRPC
// servers, over the services in its runtime. It is an http.Handler; main() mounts it on
// the listener with an http.Server speaking Protocols.
type App struct {
	rt            *Runtime
	handler       http.Handler
	grpcServer    *grpc.Server
	cancelStreams context.CancelFunc
	requests      *requests
}

// New builds the runtime and the GraphQL and gRPC servers over it, which share
// one h2c socket. ctx is startup's: once it has ended, New stops and answers its
// error.
func New(ctx context.Context, cfg Config) (*App, error) {
	rt, err := build(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// Outlives every request: it ends the long-lived streams, at shutdown.
	streamsCtx, cancelStreams := context.WithCancel(context.Background())
	graphqlServer := graph.NewServer(streamsCtx, rt.resolver())
	grpcServer := grpcserver.NewServer(streamsCtx, rt.KubetailCloud, rt.Poke, rt.Monitor)

	mux := http.NewServeMux()
	mux.Handle("/graphql", graphqlServer)

	// gRPC shares the socket with GraphQL: HTTP/2 application/grpc goes to the
	// gRPC server, everything else to the mux. See
	// docs/adr/2026-10-26-single-socket-h2c-from-the-standard-library.md.
	dispatch := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if grpcserver.IsGRPCRequest(r) {
			grpcServer.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})

	reqs := newRequests()
	return &App{
		rt:            rt,
		handler:       reqs.wrap(dispatch),
		grpcServer:    grpcServer,
		cancelStreams: cancelStreams,
		requests:      reqs,
	}, nil
}

// Protocols is what the http.Server mounting an App speaks: HTTP/1.1 for
// GraphQL, and unencrypted HTTP/2 with prior knowledge for gRPC.
func Protocols() *http.Protocols {
	var p http.Protocols
	p.SetHTTP1(true)
	p.SetUnencryptedHTTP2(true)
	return &p
}

// ServeHTTP implements http.Handler, dispatching between GraphQL and gRPC.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.handler.ServeHTTP(w, r)
}

// Start launches the services' background loops. ctx bounds startup only; the
// returned stop func accepts a drain-deadline context, blocks until all
// background work finishes, and must be called before Close.
func (a *App) Start(ctx context.Context) (func(context.Context) error, error) {
	return lifecycle.StartAll(ctx, a.rt.parts)
}

// CancelStreams ends both transports' long-lived streams cleanly — gRPC streaming
// handlers return (OK trailers flush) and SSE subscriptions flush their terminal
// frame. Register it with http.Server.RegisterOnShutdown, which runs it once the
// listeners are closed, so a client cannot reopen a stream in between.
func (a *App) CancelStreams() {
	a.cancelStreams()
}

// CloseRequests refuses every later request with a 503 and cancels every request
// still running.
func (a *App) CloseRequests() {
	a.requests.close()
}

// WaitRequests returns once every request's handler has, then stops the gRPC
// server, which waits for its method handlers. Call it after CloseRequests, and
// before the stop func, which stops the services under them.
func (a *App) WaitRequests() {
	a.requests.wait()
	a.grpcServer.Stop()
}

// Close releases OS resources after the stop func returns. The gRPC server goes
// first, outside the composed parts.
func (a *App) Close() error {
	a.grpcServer.Stop()
	return a.rt.Close()
}

// serverUIDLookup reads a cluster's last-probed kube-system UID off the cluster
// service, which memory stamps on a cluster memory: "" for a cluster the service
// does not know or has never probed.
type serverUIDLookup struct {
	clusters interface {
		Get(ctx context.Context, id apimeta.ClusterID) (*cluster.Cluster, error)
	}
}

func (u serverUIDLookup) ServerUID(ctx context.Context, id apimeta.ClusterID) string {
	c, err := u.clusters.Get(ctx, id)
	if err != nil || c == nil || c.Status.Server.UID == nil {
		return ""
	}
	return *c.Status.Server.UID
}

// newCatalog is the providers cfg lists, each at the base URL a debug build
// moved it to and signed by creds, then the fake when asked for.
func newCatalog(cfg Config, creds *providercredentials.Service) catalog.Catalog {
	fake := cfg.fake
	if fake == nil && cfg.AddFake {
		fake = llm.NewFake(llm.FakeChunkDelay)
	}
	return catalog.New(catalog.Config{
		Credentials: creds, BaseURLs: cfg.LLMBaseURLs,
		VertexBaseURL: cfg.vertexBaseURL, Fake: fake,
	})
}

// probeSandbox is sandbox.Probe, which a test replaces to count its calls.
var probeSandbox = sandbox.Probe

// sandboxer is what the login shell and the settings need of the
// sandbox: *sandbox.Sandbox, or a test's fake.
type sandboxer interface {
	Command(ctx context.Context, r sandbox.Run) (*exec.Cmd, error)
	System(home, shell string) sandbox.System
	Never(home string) []string
}

// newSettingsService is the user's settings and the login shell's PATH kept
// with them. A PATH folder and a folder grant are judged by what a sandboxed run
// of the bash tool's shell reads, by the paths no rule opens, Kubetail's
// directories (kubetail.DeniedDirs) among them, and by the links Kubetail
// followed to them. Refresh PATH runs the login shell in sb as the launch
// does, its TMPDIR under kubetail.TmpDir. Each reads the denied-always list
// afresh, as a run does: it lists the other users' homes, which can appear
// while the sidecar runs. On a machine with no sandbox it stores no PATH, keeps
// no fault and refuses every folder.
func newSettingsService(store *settings.Store, sb sandboxer, shell *bash.Tool, state sandbox.State, kubetail bash.Paths, fault string) *settings.Service {
	if !state.Available {
		return settings.NewService(store, nil, nil, nil, "")
	}
	home, _ := os.UserHomeDir()
	zones := func() settings.Zones {
		return settings.Zones{
			Never: slices.Concat(sb.Never(home), kubetail.DeniedDirs), Open: sb.System(home, shell.Shell()).Files, Home: home, NoWrite: sandbox.NoWrite(home),
			Links: kubetail.Links,
		}
	}
	return settings.NewService(store, zones, shellPathResolver(sb, home, kubetail.DeniedDirs, kubetail.TmpDir), filepath.SplitList(bash.DefaultPath), fault)
}

// setShellPath stores the PATH the launch read from the login shell, and
// answers whether it changed the stored one. A write that fails changes
// nothing and is not a startup error.
func setShellPath(ctx context.Context, svc *settings.Service, path []string) bool {
	changed, err := svc.SetShellPath(ctx, path)
	if err != nil {
		slog.Warn("shell PATH not stored", "err", err)
	}
	return changed
}

// probeExecutables reads the registered executables and starts a probe of
// them. A read that fails starts none, the curated executables' included, and
// is a warning, not a startup error.
func probeExecutables(ctx context.Context, read func(context.Context) ([]settings.Executable, error), start func([]settings.Executable)) {
	registered, err := read(ctx)
	if err != nil {
		slog.Warn("executables not read; no probe started", "err", err)
		return
	}
	start(registered)
}

// sandboxStateOf is whether sandboxed Bash is offered: a sandbox with no shell
// has no command to confine.
func sandboxStateOf(shellFound bool, probed sandbox.State) sandbox.State {
	if !shellFound {
		return sandbox.State{Reason: "no shell was found"}
	}
	return probed
}

// toolDeps is what the tool box is built over.
type toolDeps struct {
	// shell is nil on a machine with no shell.
	shell *bash.Tool
	// fenced is Kubetail's directories, which the file tools stay out of.
	fenced []string
	// hidden answers what the sandbox keeps shut under a grant.
	hidden func() (never, shut []string)
	// umask is what a file Write makes for the user takes.
	umask    fs.FileMode
	memory   memory.Service
	clusters cluster.Service
	// logs is what LogsRead pages and LogsWatch follows.
	logs *logs.Service
	// now is the clock the search's prompt names the month by; nil is time.Now.
	now func() time.Time
}

// chatTools is the one box: bash where New found a shell, Read, Memory, Write, Edit
// and WebFetch on every machine, TaskStop for the tasks bash, Agent and LogsWatch
// start, then the provider's web search, then KubeQuery, LogsView, LogsRead and
// LogsWatch. WebFetch dials no local or private address
// but the proxy the environment names. The order is the preference within a kind:
// the first tool of a kind that a turn's target takes is the one it gets. A machine
// with no shell still reads the stored calls of bash, and still offers TaskStop,
// since its agents and watches are tasks.
func chatTools(d toolDeps) (tools.Box, error) {
	reader, err := read.New(d.hidden, d.fenced...)
	if err != nil {
		return tools.Box{}, err
	}
	writer, err := write.New(d.umask, d.hidden, d.fenced...)
	if err != nil { // coverage:ignore read.New accepted the same fence
		return tools.Box{}, err
	}
	editor, err := edit.New(d.hidden, d.fenced...)
	if err != nil { // coverage:ignore read.New accepted the same fence
		return tools.Box{}, err
	}
	fetcher := webfetch.New(webfetch.NewTransport(webfetch.Dialing{
		Resolver: net.DefaultResolver,
		Public:   webfetch.Public,
		Proxy:    httpproxy.FromEnvironment(),
	}), webfetch.FetchTimeout)
	now := d.now
	if now == nil {
		now = time.Now
	}
	search := anthropicwebsearch.New(now)
	notes := memorytool.New(d.memory)
	query := kubequery.New(d.clusters)
	views := logsview.New(d.clusters)
	reads := logsread.New(d.clusters, d.logs)
	watches := logswatch.New(d.clusters, d.logs)
	if d.shell == nil {
		return tools.NewBox([]tools.Tool{reader, notes, writer, editor, fetcher, taskstop.New(), agent.New(), search, query, views, reads, watches}, bash.Reader{}), nil
	}
	return tools.NewBox([]tools.Tool{d.shell, reader, notes, writer, editor, fetcher, taskstop.New(), agent.New(), search, query, views, reads, watches}), nil
}
