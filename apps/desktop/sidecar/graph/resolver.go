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

package graph

//go:generate go tool gqlgen generate

// Hand-written; gqlgen never regenerates this file. Add resolver dependencies here.

import (
	"context"

	"github.com/amorey/gochan/watch"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/chat"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/envvars"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/memory"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/providercredentials"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/toolsettings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/bash"
)

// Resolver carries every operation's dependencies. Each field MUST be non-nil — the
// composition root always wires them and the resolvers call them without guards;
// degraded behavior lives inside the services.
type Resolver struct {
	// ClusterSvc is the boundary to the cluster backend, hiding beehive behind the
	// cluster types. (Named ClusterSvc to avoid shadowing the generated
	// queryResolver.Clusters method.)
	ClusterSvc cluster.Service
	// ChatSvc backs the chat mutations and watches. (Named ChatSvc, like ClusterSvc,
	// to stay clear of the generated resolver methods named after the schema's types.)
	ChatSvc chat.Service
	// MemorySvc backs memory's watch and writes.
	MemorySvc memory.Service
	// LLMSvc answers the models state watch, every provider's catalog in picker order, and
	// labels a message's provider.
	LLMSvc *llm.Service
	// SandboxState answers sandboxStateGet: whether this machine offers
	// sandboxed Bash.
	SandboxState sandbox.State
	// Auth is the Kubetail Cloud connector, which backs the auth state watch and
	// the login and logout mutations: *kubetailcloud.Service, or a test's fake.
	Auth AuthConnector
	// Settings is the user's settings, the approval modes and always grants
	// among them, and the frozen PATH.
	Settings *settings.Service
	// EnvVars is the user's variables for their commands.
	EnvVars *envvars.Service
	// ToolSettings is the user's settings of the model's tools.
	ToolSettings *toolsettings.Service
	// Executables probes the executables sandboxed commands run: the Bash tool, nil where
	// there is no shell.
	Executables ExecutableProber
	// Console runs the Sandbox console's commands: the Bash tool, nil where
	// there is no shell.
	Console SandboxConsole
	// Logs pages and tails a scope's lines.
	Logs LogsService
	// CloudTargets are the cloud platforms this build has, in the order
	// Settings lists them: the only ones whose switch the wire reads or writes.
	CloudTargets []permissions.Target
	// ChatGPT is the ChatGPT connector: *chatgpt.Service, or a test's fake.
	ChatGPT ChatGPTConnector
	// OpenRouter is the OpenRouter connector: *openrouter.Service, or a
	// test's fake.
	OpenRouter OpenRouterConnector
	// AWS is the AWS connector: *aws.Service, or a test's fake.
	AWS AWSConnector
	// GitHub is the GitHub connector: *github.Service, or a test's fake.
	GitHub GitHubConnector
	// Google is the Google connector: *google.Service, or a test's fake.
	Google GoogleConnector
	// ProviderCredentials holds every provider's keys: *providercredentials.Service,
	// or a test's fake.
	ProviderCredentials ProviderCredentialStore
	// OpenURL opens one of the sidecar's own addresses in the browser:
	// browser.Open, or a test's fake. No caller hands it an address.
	OpenURL func(url string) error
	// Monitor is the monitor's reads and switch: *monitor.Monitor, or a test's fake.
	Monitor MonitorService
}

// MonitorService is the monitor as the monitoring fields reach it.
type MonitorService interface {
	ListSignals() []monitor.MonitorSignal
	SetEnabled(ctx context.Context, id monitor.MonitorTargetID, enabled bool) (monitor.MonitorTarget, error)
	Triage(ctx context.Context, id monitor.IncidentID, model monitor.TriageModel, always bool) (monitor.Incident, error)
	SetTriageModel(ctx context.Context, model *monitor.TriageModel) error
	WatchTriageModelState(ctx context.Context) (*monitor.Stream[*monitor.TriageModel], error)
	WatchTargetCollection(ctx context.Context) (*monitor.Stream[monitor.MonitorTargetCollectionWatchFrame], error)
	WatchIncidentCollection(ctx context.Context) (*monitor.Stream[monitor.IncidentCollectionWatchFrame], error)
	SubscribeIncidentOccurrenceEventFeed(ctx context.Context, id monitor.IncidentID) (*monitor.Stream[monitor.IncidentOccurrenceEventFeedFrame], error)
}

// ProviderCredentialStore is every provider's keys as the wire drives them. No
// method returns a key.
type ProviderCredentialStore interface {
	WatchCredentialsState() (<-chan []providercredentials.State, func())
	Save(id, key string) error
	Delete(id string) error
}

// AuthConnector is the Kubetail Cloud connector as the wire drives it.
type AuthConnector interface {
	WatchState() (<-chan connectors.State, func())
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
}

// ChatGPTConnector is the ChatGPT connector as the wire drives it.
type ChatGPTConnector interface {
	WatchState() (<-chan connectors.State, func())
	Connect(ctx context.Context, fresh bool) error
	Cancel()
	Disconnect(ctx context.Context) error
}

// OpenRouterConnector is the OpenRouter connector as the wire drives it.
type OpenRouterConnector interface {
	WatchState() (<-chan connectors.State, func())
	Connect(ctx context.Context) error
	Cancel()
	Disconnect() error
}

// AWSConnector is the AWS connector as the wire drives it.
type AWSConnector interface {
	WatchState() (<-chan connectors.State, func())
	Connect(ctx context.Context) error
	Cancel()
	Disconnect() error
}

// GitHubConnector is the GitHub connector as the wire drives it.
type GitHubConnector interface {
	WatchState() (<-chan connectors.State, func())
	Connect(ctx context.Context) error
	Cancel()
	Disconnect() error
}

// GoogleConnector is the Google connector as the wire drives it.
type GoogleConnector interface {
	WatchState() (<-chan connectors.State, func())
	Connect(ctx context.Context, fresh bool) error
	Cancel()
	Disconnect(ctx context.Context) error
	// Project is the Google Cloud project the models run in, empty for none.
	Project() string
}

// LogsService pages and tails a scope's lines: *logs.Service, or a test's fake.
type LogsService interface {
	Fetch(ctx context.Context, id cluster.ClusterID, q logs.FetchRequest) (logs.FetchResponse, error)
	TailFromCursor(ctx context.Context, id cluster.ClusterID, q logs.TailRequest) (logs.TailResponse, error)
}

// ExecutableProber is the executable probe: *bash.Tool, or a test's fake. StartProbe
// and Report take the user's registered executables and list the curated ones
// first; a registered one the shape check refuses is listed and never probed.
// WatchProbeState says when a probe starts and ends.
type ExecutableProber interface {
	StartProbe(registered []settings.Executable)
	Report(registered []settings.Executable) []bash.ExecutableReport
	WatchProbeState() *watch.Receiver[bash.ProbeState]
}

// SandboxConsole is the Sandbox console: *bash.Tool, or a test's fake.
// StartConsoleCommand refuses a machine whose sandbox does not confine a command.
type SandboxConsole interface {
	StartConsoleCommand(req bash.ConsoleRequest) (int, error)
	StopConsoleCommand(number int)
	WatchConsoleState() *watch.Receiver[*bash.ConsoleRun]
}

// probes reports whether this machine probes executables: a sandbox and a shell.
func (r *Resolver) probes() bool {
	return r.SandboxState.Available && r.Executables != nil
}

// scopeOf is a variable's scope off the wire: the cluster, or "" for every
// cluster.
func scopeOf(cluster *apimeta.ClusterID) apimeta.ClusterID {
	if cluster == nil {
		return ""
	}
	return *cluster
}

// envVars is one scope's variables on the wire, in order; empty, never nil,
// for none.
func (r *Resolver) envVars(ctx context.Context, cluster apimeta.ClusterID) ([]*envvars.Var, error) {
	vars, err := r.EnvVars.List(ctx, cluster)
	if err != nil {
		return nil, err
	}
	out := make([]*envvars.Var, len(vars))
	for i := range vars {
		out[i] = &vars[i]
	}
	return out, nil
}
