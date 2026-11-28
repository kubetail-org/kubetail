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
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/graph"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/catalog"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/chat"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor/kubesignals"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor/kubetriage"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/filelock"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/lifecycle"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oauth/browser"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlitemigrate"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/clustercard"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/aws"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/chatgpt"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/github"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/google"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/kubetailcloud"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/openrouter"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/envvars"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/kubeconfig"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstore"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/memory"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/poke"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/providercredentials"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/toolsettings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/bash"
)

// Runtime is every service the sidecar builds, in build order: a constructor
// takes only fields above its own. Services never see the Runtime; each takes
// what it needs as arguments.
type Runtime struct {
	DB         *appdb.DB
	Poke       *poke.Service
	Kubeconfig *kubeconfig.Service
	Cluster    cluster.Service
	// Logs reads views' lines through the cluster service and owns each cluster
	// cache's log file, which the cluster service clears and removes with the cache.
	Logs *logs.Service
	// KubetailCloud is the Kubetail Cloud account's sign-in, which signs nothing.
	KubetailCloud *kubetailcloud.Service
	// ChatGPT is Sign in with ChatGPT, which signs the chatgpt provider.
	ChatGPT *chatgpt.Service
	// OpenRouter is the OpenRouter connector, which signs the openrouter row
	// over its keys while it is connected.
	OpenRouter *openrouter.Service
	// AWS is the AWS connector, whose credentials nothing uses yet.
	AWS *aws.Service
	// GitHub is the GitHub connector, whose token the sandbox's GitHub proxy
	// sends while it is connected.
	GitHub *github.Service
	// Google is the Google connector, which signs the google row.
	Google *google.Service
	// ProviderCredentials holds each provider's keys and signs every provider.
	ProviderCredentials *providercredentials.Service
	Catalog             catalog.Catalog
	LLM                 *llm.Service
	// EnvVars is the user's variables, which every command reads.
	EnvVars *envvars.Service
	// ToolSettings is the user's settings of the model's tools.
	ToolSettings *toolsettings.Service
	// Shell is nil on a machine with no shell.
	Shell        *bash.Tool
	SandboxState sandbox.State
	// CloudTargets are the cloud platforms this build has, in Settings' order.
	CloudTargets []permissions.Target
	Settings     *settings.Service
	Memory       memory.Service
	Tools        tools.Box
	Chat         chat.Service
	// Monitor reads each integration's targets and records their incidents.
	Monitor *monitor.Monitor

	// parts is added to as each service is built, so start order is build order;
	// stop and close run in reverse, which is what keeps poke's hub open until its
	// subscribers have drained, and app.db open until every service over it has
	// closed.
	parts []lifecycle.Part
}

// build makes every service, in order. A constructor failing closes every part
// built before it, newest first.
func build(ctx context.Context, cfg Config) (*Runtime, error) {
	p := pathsOf(cfg)

	// One sidecar per data directory: the lock is taken before anything opens
	// a file there. This text reaches the user.
	lock, err := filelock.TryLock(p.LockFile)
	if errors.Is(err, filelock.ErrLocked) {
		return nil, fmt.Errorf("another Kubetail is using %s", cfg.Folders.Data)
	}
	if err != nil {
		return nil, fmt.Errorf("lock the data directory: %w", err)
	}
	rt := &Runtime{}
	// The first part, so it closes after every file it guards. Once: a runtime
	// may be closed twice, and a file cannot.
	rt.add("data directory lock", lifecycle.CloseFunc(sync.OnceValue(lock.Close)))
	fail := func(err error) (*Runtime, error) {
		rt.Close()
		return nil, err
	}
	// Before any service: bash.New empties the shell folder, so a link there
	// is refused, not followed.
	if err := makeSubfolders(p); err != nil {
		return fail(err)
	}

	sb, probed, err := probeSandbox(ctx)
	if err != nil {
		return fail(err)
	}
	slog.Info("sandbox probed", "available", probed.Available, "reason", probed.Reason)
	// A nil pointer in an interface is not a nil interface.
	var boxer sandboxer
	if sb != nil {
		boxer = sb
	}
	// Before anything that reads the environment: on macOS the login shell's
	// sets it process-wide, credential plugins resolve against it, and net/http
	// and WebFetch read the proxy variables from it. The keys it answers reach
	// the catalog alone.
	login := launch{path: cfg.launchPath}
	if cfg.RunLoginShell {
		login = launchShell(ctx, boxer, p.Bash.DeniedDirs, p.Bash.TmpDir)
		cfg.LLMKeys = withShellKeys(cfg.LLMKeys, login.keys)
	}
	cfg.googleProject, cfg.vertexBaseURL = findGoogleProject(cfg.Getenv)

	// The app owns app.db and hands it to every service that writes or watches it.
	// It closes after every service over it.
	db, err := appdb.Open(p.AppDBFile, appdb.DefaultSweepInterval)
	if err != nil {
		_ = sb.Close()
		// This text reaches the user: a newer file names itself and both versions.
		if newer, ok := errors.AsType[*sqlitemigrate.NewerSchemaError](err); ok {
			return fail(fmt.Errorf("app.db was written by a newer Kubetail (schema %d; this build reads up to %d)", newer.Recorded, newer.Supported))
		}
		return fail(fmt.Errorf("open app database: %w", err))
	}
	rt.DB = db
	rt.add("app.db", lifecycle.CloseFunc(db.Close))
	// Its close ends every sandboxed run still going, after every service
	// over the sandbox has stopped its own.
	rt.add("sandbox", lifecycle.CloseFunc(sb.Close))

	// The settings are rows in app.db, so they open after it and
	// close before it.
	settingsStore, err := settings.Open(db)
	if err != nil {
		return fail(err)
	}
	rt.add("settings", lifecycle.CloseFunc(settingsStore.Close))

	// Shared cross-subsystem poke bus (wall-clock gap detector + host pokes via
	// gRPC PokeService); see docs/adr/2026-08-09-poke-resync-fanout.md.
	rt.Poke = poke.New()
	rt.add("poke service", lifecycle.StartFunc(rt.Poke.Start))

	// One reader of the user's kubeconfig, shared by everything that resolves a
	// context. Closing it ends every subscription, so it is the app's alone.
	rt.Kubeconfig = kubeconfig.New(cfg.KubeconfigPath, rt.Poke)
	rt.add("kubeconfig service", rt.Kubeconfig)

	rt.Cluster, err = cluster.New(db, p.Cluster, rt.Kubeconfig, rt.Poke, cluster.WithFaults(cfg.Faults))
	if err != nil {
		return fail(err)
	}
	rt.add("cluster service", rt.Cluster)

	// The edge back: the cluster service's cache pass clears and removes a cache's log
	// file through the logs service, which closes first and still deletes after that.
	rt.Logs = logs.New(rt.Cluster, p.LogsDir, logstore.DefaultRetention)
	rt.add("logs service", lifecycle.CloseFunc(rt.Logs.Close))
	rt.Cluster.SetCacheFiles(rt.Logs)

	// One keyring for every service, so they share its gate: a locked keyring
	// asks the user once for their reads.
	keyring := cmp.Or(cfg.Keyring, oskeyring.New(cmp.Or(cfg.KeychainService, defaultKeychainService)))
	rt.KubetailCloud, err = kubetailcloud.New(kubetailcloud.Config{
		DB:       db,
		Keyring:  keyring,
		Issuer:   cfg.OAuthIssuerURL,
		ClientID: cfg.OAuthClientID,
	})
	if err != nil { // coverage:ignore the statements prepare on the schema app.db was just migrated to
		return fail(err)
	}
	rt.add("kubetail cloud service", lifecycle.CloseFunc(func() error { rt.KubetailCloud.Close(); return nil }))

	rt.ChatGPT, err = chatgpt.New(chatgpt.Config{
		DB:         db,
		Keyring:    keyring,
		Issuer:     cmp.Or(cfg.ConnectorAuthURLs[chatgpt.ID], chatgpt.Issuer),
		APIBaseURL: cmp.Or(cfg.LLMBaseURLs[chatgpt.ID], chatgpt.APIBaseURL),
	})
	if err != nil { // coverage:ignore the statements prepare on the schema app.db was just migrated to
		return fail(err)
	}
	rt.add("chatgpt service", lifecycle.CloseFunc(func() error { rt.ChatGPT.Close(); return nil }))

	rt.OpenRouter, err = openrouter.New(openrouter.Config{
		Keyring:    keyring,
		AuthURL:    cmp.Or(cfg.ConnectorAuthURLs[openrouter.ID], openrouter.AuthURL),
		APIBaseURL: cmp.Or(cfg.LLMBaseURLs[openrouter.ID], openrouter.APIBaseURL),
	})
	if err != nil {
		return fail(err)
	}
	rt.add("openrouter service", lifecycle.CloseFunc(func() error { rt.OpenRouter.Close(); return nil }))

	rt.AWS, err = aws.New(aws.Config{
		Keyring:   keyring,
		SigninURL: cmp.Or(cfg.ConnectorAuthURLs[aws.ID], aws.SigninURL),
	})
	if err != nil {
		return fail(err)
	}
	rt.add("aws service", lifecycle.CloseFunc(func() error { rt.AWS.Close(); return nil }))

	rt.GitHub, err = github.New(github.Config{
		Keyring:  keyring,
		ClientID: cfg.GitHubClientID,
		AuthURL:  cmp.Or(cfg.ConnectorAuthURLs[github.ID], github.AuthURL),
		APIURL:   github.APIURL,
	})
	if err != nil {
		return fail(err)
	}
	rt.add("github service", lifecycle.CloseFunc(func() error { rt.GitHub.Close(); return nil }))

	rt.Google, err = google.New(google.Config{
		DB:           db,
		Keyring:      keyring,
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		APIBaseURL:   cmp.Or(cfg.LLMBaseURLs[google.ID], cfg.vertexBaseURL),
		Project:      cfg.googleProject,
	})
	if err != nil { // coverage:ignore the statements prepare on the schema app.db was just migrated to
		return fail(err)
	}
	rt.add("google service", lifecycle.CloseFunc(func() error { rt.Google.Close(); return nil }))

	rt.ProviderCredentials, err = providercredentials.New(providercredentials.Config{
		Keyring:    keyring,
		Providers:  catalog.Providers(cfg.LLMBaseURLs, cfg.vertexBaseURL),
		EnvKeys:    cfg.LLMKeys,
		Connectors: map[string]providercredentials.Connector{openrouter.ID: rt.OpenRouter, chatgpt.ID: rt.ChatGPT, google.ID: rt.Google},
	})
	if err != nil {
		return fail(err)
	}
	rt.add("provider credentials service", lifecycle.CloseFunc(func() error { rt.ProviderCredentials.Close(); return nil }))

	rt.Catalog = newCatalog(cfg, rt.ProviderCredentials)
	rt.LLM = llm.New(rt.Catalog.Providers()...)
	rt.add("provider catalogs", lifecycle.StartFunc(func(context.Context) (func(context.Context) error, error) {
		stop := rt.ProviderCredentials.SyncCatalogs(rt.Poke, rt.LLM, catalog.Models(cfg.LLMBaseURLs, cfg.vertexBaseURL))
		return func(context.Context) error { stop(); return nil }, nil
	}))

	rt.EnvVars, err = envvars.New(db, time.Now)
	if err != nil {
		return fail(err)
	}
	rt.add("user's variables", lifecycle.CloseFunc(rt.EnvVars.Close))

	// rt.Settings is set below, before any run reads it.
	pathList := func(ctx context.Context) (settings.RunPath, error) { return rt.Settings.RunPath(ctx) }
	platforms := cloudPlatformsFor(probed, cfg.cloudPlatforms, rt.Kubeconfig, rt.GitHub)
	shell, found := bash.New(p.Bash, cfg.HostPID, sb, rt.Cluster, pathList, rt.EnvVars.For, bash.Cloud{Platforms: platforms, Switches: settingsStore.CloudPlatformSwitches})
	if found && cfg.callDeadline != nil {
		shell.SetCallDeadline(cfg.callDeadline)
	}
	rt.Shell = shell
	rt.SandboxState = sandboxStateOf(found, probed)
	rt.CloudTargets = cloudTargets(platforms)
	rt.Settings = newSettingsService(settingsStore, boxer, shell, rt.SandboxState, p.Bash, login.fault)

	rt.Memory, err = memory.New(db, serverUIDLookup{clusters: rt.Cluster.Clusters()})
	if err != nil {
		return fail(err)
	}
	rt.add("memory service", rt.Memory)

	// The file tools are fenced out of every one of Kubetail's directories.
	rt.Tools, err = chatTools(toolDeps{
		shell: shell, fenced: p.Bash.DeniedDirs, hidden: rt.Settings.Hidden, umask: cfg.UserUmask,
		memory: rt.Memory, clusters: rt.Cluster, logs: rt.Logs,
	})
	if err != nil { // coverage:ignore pathsOf always names Kubetail's folders
		return fail(fmt.Errorf("fence Kubetail's directories: %w", err))
	}

	// Settings lists only the tools this machine's box holds.
	listed := slices.DeleteFunc(slices.Clone(catalog.ToolListings()), func(l toolsettings.Listing) bool { return !rt.Tools.Has(l.Name) })
	rt.ToolSettings, err = toolsettings.New(db, listed, time.Now)
	if err != nil { // coverage:ignore the statements prepare on the schema app.db was just migrated to
		return fail(err)
	}
	rt.add("tool settings", lifecycle.CloseFunc(rt.ToolSettings.Close))

	rt.Chat, err = chat.New(db, p.ChatsDir, p.MonitorDir, rt.LLM, clustercard.New(rt.Cluster), rt.Memory, rt.Tools, rt.Catalog, rt.SandboxState, rt.Settings, rt.CloudTargets)
	if err != nil {
		return fail(err)
	}
	rt.add("chat service", rt.Chat)

	// Kubernetes is the one integration: a target per cluster, read from its cache,
	// and triaged by a monitor run of the chat service.
	kube := kubesignals.Register(db, rt.Cluster)
	kube.Triager = kubetriage.New(rt.Chat, rt.LLM)
	rt.Monitor, err = monitor.New(db, kube)
	if err != nil { // coverage:ignore the statements prepare on the schema app.db was just migrated to, and the registration is fixed
		return fail(err)
	}
	rt.add("monitor", rt.Monitor)

	// Edges back to a service built later, set once both exist. The part goes
	// with the edge, so it closes before the service it reads.
	if shell != nil {
		// A probe or a console command has no chat, so its run reads the folders granted always alone.
		shell.SetAlwaysFolders(func(ctx context.Context) ([]session.Folder, error) { return rt.Chat.FoldersFor(ctx, "") })
		// Ends a running probe at Close.
		rt.add("executable probe", lifecycle.CloseFunc(shell.Close))
	}

	// Before the snapshot, so the first sandboxed run reads the shell's PATH
	// this launch read. A launch whose read failed keeps the last one stored.
	if login.path != nil && rt.SandboxState.Available {
		rt.add("shell PATH", lifecycle.StartFunc(func(ctx context.Context) (func(context.Context) error, error) {
			// Only when it changed, so nothing runs unasked on a machine
			// whose tools did not; the executable probe part's Close ends it.
			if setShellPath(ctx, rt.Settings, login.path) {
				probeExecutables(ctx, rt.Settings.Executables, shell.StartProbe)
			}
			return func(context.Context) error { return nil }, nil
		}))
	}
	// Started after READY and before Serve, so no command can run ahead of it.
	if cfg.RunLoginShell && shell != nil {
		rt.add("shell snapshot", lifecycle.StartFunc(shell.StartSnapshot))
	}
	return rt, nil
}

// add appends a part in build order.
func (rt *Runtime) add(name string, sc lifecycle.StartCloser) {
	rt.parts = append(rt.parts, lifecycle.Part{Name: name, StartCloser: sc})
}

// Close closes every part, newest first. Call it after the stop func Start
// returned, or on a runtime that never started.
func (rt *Runtime) Close() error {
	return lifecycle.CloseAll(rt.parts)
}

// resolver is the GraphQL resolver over the runtime's services.
func (rt *Runtime) resolver() *graph.Resolver {
	// A nil pointer in an interface is not a nil interface.
	var prober graph.ExecutableProber
	var console graph.SandboxConsole
	if rt.Shell != nil {
		prober, console = rt.Shell, rt.Shell
	}
	return &graph.Resolver{
		ClusterSvc:          rt.Cluster,
		ChatSvc:             rt.Chat,
		MemorySvc:           rt.Memory,
		LLMSvc:              rt.LLM,
		SandboxState:        rt.SandboxState,
		Auth:                rt.KubetailCloud,
		ChatGPT:             rt.ChatGPT,
		OpenRouter:          rt.OpenRouter,
		AWS:                 rt.AWS,
		GitHub:              rt.GitHub,
		Google:              rt.Google,
		ProviderCredentials: rt.ProviderCredentials,
		OpenURL:             browser.Open,
		Settings:            rt.Settings,
		EnvVars:             rt.EnvVars,
		ToolSettings:        rt.ToolSettings,
		Executables:         prober,
		Console:             console,
		Logs:                rt.Logs,
		CloudTargets:        rt.CloudTargets,
		Monitor:             rt.Monitor,
	}
}

// cloudPlatformsFor is the cloud platforms a sandboxed run may reach: the
// build's, over kc and the GitHub connector gh, or a test's in their place. A
// machine with no sandbox has none, so it offers no switch.
func cloudPlatformsFor(state sandbox.State, configured []bash.CloudPlatform, kc *kubeconfig.Service, gh *github.Service) []bash.CloudPlatform {
	switch {
	case !state.Available:
		return nil
	case configured != nil:
		return configured
	}
	return builtCloudPlatforms(kc, gh)
}

// cloudTargets is each cloud platform's target, in the build's order.
func cloudTargets(platforms []bash.CloudPlatform) []permissions.Target {
	targets := make([]permissions.Target, 0, len(platforms))
	for _, p := range platforms {
		targets = append(targets, p.Target())
	}
	return targets
}

// findGoogleProject is the Google Cloud project getenv names and its Vertex AI
// endpoint. Both are empty when getenv is nil or names none, and when the
// project builds no endpoint, so the row never shows a project it cannot sign.
func findGoogleProject(getenv func(string) string) (project, vertexBaseURL string) {
	if getenv == nil {
		return "", ""
	}
	project, location := google.FindProject(getenv)
	if base := google.BaseURL(project, location); base != "" {
		return project, base
	}
	return "", ""
}
