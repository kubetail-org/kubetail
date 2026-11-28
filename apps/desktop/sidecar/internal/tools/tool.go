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

// Package tools is what the sidecar can do for a model: the offer, the prompt
// section that explains it, and the code that answers one call. A binding is a
// Tool over a capability — the capability is the one piece of code that runs
// something and knows no contract, and the binding parses its contract's input
// into the capability's request and renders the outcome in the contract's shape.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/iso8601"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
)

const (
	// InlineLimit is the most a result carries, header included.
	InlineLimit = 30_000
	// FileLimit is the most output kept, and the largest file Read opens.
	FileLimit = 8 << 20
)

// Reader reads what a stored call of one tool did. Every Tool is one.
type Reader interface {
	// Name is the tool's identity, unique in the box, and what every stored call
	// of it names: a custom tool's definition name, or a native tool's own name,
	// qualified by its vendor.
	Name() string
	// ContractName is the shape of what a call of the tool stores, unique in the
	// box and recorded on each of its rows: `<name>/v<n>` for a tool of ours, the
	// vendor's own identifier for a vendor's.
	ContractName() ContractName
	// ActionKind is what the tool's calls do.
	ActionKind() ActionKind
	// Action is what a call does, read from its arguments and what its row
	// keeps: the cwd, and whether a sandbox confined the call. An error is
	// arguments the tool refuses, or a tool with nothing to show.
	Action(input json.RawMessage, cwd string, sandboxed bool) (Action, error)
}

// Tool is one thing the model can call. What else it is follows from the
// interfaces it implements: a Custom tool the app defines, or else a vendor's,
// which the sidecar runs when it is a Runner and the provider runs when it is
// Budgeted. A vendor's tool has three names: Name, our identity for it,
// vendor-qualified; ContractName, the vendor's own identifier for its shape;
// and each wire's call name (MessagesCallName), which its calls arrive under
// there.
type Tool interface {
	Reader
	// Prompt is the tool's section of "What you can do": how to use it, what its
	// results mean. Markdown opening with its own ## heading.
	Prompt() string
}

// Runner is a Tool the sidecar runs.
type Runner interface {
	Tool
	// Run answers one call in the chat rt stands for. text is what the model
	// reads; isError marks a refusal. Neither may carry a Go error's text.
	Run(ctx context.Context, rt Runtime, input json.RawMessage) (text string, isError bool)
}

// Custom is a tool the app defines, offered by its definition.
type Custom interface {
	Runner
	// Definition is the offer the model sees. Its Name is the tool's.
	Definition() llm.ToolDefinition
}

// PerTarget is a Custom tool whose offer depends on the turn's target.
type PerTarget interface {
	Custom
	// For is the tool as a turn on t is offered it.
	For(t llm.Target) Custom
}

// ContractName names the shape of a tool's stored calls: its arguments, its
// shown action and its task's result. A tool of ours names it `bash/v1`; a
// vendor's tool by the vendor's own identifier, the Messages API's
// web_search_20260318, say.
type ContractName string

// Budgeted is a vendor's tool the provider runs, capped per turn.
type Budgeted interface {
	Tool
	// MaxUses is the calls one turn may make, spread over its requests.
	MaxUses() int
	// Allowance is the tokens the provider adds to one turn for this tool at its
	// cap: what a length check keeps free for it.
	Allowance() int
}

// Approval is what the gate records on a gated call's row once its tool has
// read the input: Cwd, where it will start, exactly as it starts there, empty
// for a tool that runs nowhere.
type Approval struct {
	Cwd string
	// Sandboxed is whether a sandbox confines the call, recorded whether or not
	// it asks.
	Sandboxed bool
	// Skip runs the call without asking. The zero value asks.
	Skip bool
	// Network is the network a sandboxed call runs with. NetworkApproved holds
	// only once the user approves the call.
	Network session.Network
	// Folder is the granted folder a skip rests on, nil for any other skip and
	// for a call that asks. A tool reaches the call's path through it alone.
	Folder *session.Folder
	// Env is the user's variables a command outside the sandbox is shown with
	// and runs with, NAME=value. A sandboxed command reads its own at its start.
	Env []string
}

// Gated is a Runner whose call runs only once the user has approved it, unless
// its Approval skips. The loop finds it by type assertion at the call; the box
// need not know. A call that asks has an action the approval request can draw.
type Gated interface {
	Runner
	// Approval is what the gate records. An error is an input the tool cannot
	// read or refuses: the call is refused and nothing is shown, answered bad-input
	// unless the error is a *Refusal. It reads rt and never starts or stops a task,
	// or writes a stamp.
	Approval(ctx context.Context, rt Runtime, input json.RawMessage) (Approval, error)
}

// ApprovedRunner is a Gated tool whose run takes the Approval its gate
// decided, so what runs is what was decided or approved, whatever the
// session says by the time it starts. The loop calls RunApproved in place of
// Run.
type ApprovedRunner interface {
	Gated
	RunApproved(ctx context.Context, rt Runtime, input json.RawMessage, a Approval) (text string, isError bool)
}

// Shown is a Runner whose run resolves what the call showed, which the row keeps
// as the call's action: read off the arguments alone it would say what the model
// typed, not what the mirror and the clock made of it. shown is nil on a refusal,
// and Run on such a tool is RunShown with the action dropped.
type Shown interface {
	Runner
	RunShown(ctx context.Context, rt Runtime, input json.RawMessage) (text string, isError bool, shown *Action)
}

// Refusal is an Approval error carrying the result the model reads, so a call
// refused before the user is asked says why in the tool's own words.
type Refusal struct {
	Result string
}

func (e *Refusal) Error() string { return "refused: " + e.Result }

// FoldersUnread is a file tool's refusal when the session's folder grants
// could not be read: the call fails rather than read as no grant. The reason
// is logged, never shown.
func FoldersUnread(tool string, err error) *Refusal {
	slog.Warn("folder grants not read", "tool", tool, "err", err)
	return &Refusal{Result: "Kubetail could not read the folders granted. Try again."}
}

// PathUnread is a sandboxed run's refusal when the PATH setting could not be
// read: the run does not start rather than search a default. The reason is
// logged, never shown.
func PathUnread(tool string, err error) *Refusal {
	slog.Warn("PATH setting not read", "tool", tool, "err", err)
	return &Refusal{Result: "Kubetail could not read your PATH setting. Try again."}
}

// ActionKind is what a call of a tool does, from a closed set. A turn is offered
// at most one tool of each kind, and a tool's Action is always of its kind.
type ActionKind string

const (
	ActionCommand   ActionKind = "command"
	ActionRead      ActionKind = "read"
	ActionWrite     ActionKind = "write"
	ActionEdit      ActionKind = "edit"
	ActionSearch    ActionKind = "search"
	ActionFetch     ActionKind = "fetch"
	ActionStop      ActionKind = "stop" // stops a background task; its calls show no action
	ActionMemory    ActionKind = "memory"
	ActionDelegate  ActionKind = "delegate"   // hands a task to a subagent
	ActionKubeQuery ActionKind = "kube-query" // SQL over the cluster's cache
	ActionLogsView  ActionKind = "logs-view"  // a live view of logs the user looks at
	ActionLogsRead  ActionKind = "logs-read"  // log lines the model read
	ActionLogsWatch ActionKind = "logs-watch" // a follow of logs that wakes the model when it ends
)

// ActionKinds is every kind, in declaration order.
var ActionKinds = []ActionKind{ActionCommand, ActionRead, ActionWrite, ActionEdit, ActionSearch, ActionFetch, ActionStop, ActionMemory, ActionDelegate, ActionKubeQuery, ActionLogsView, ActionLogsRead, ActionLogsWatch}

// valid reports whether k is one of ActionKinds.
func (k ActionKind) valid() bool {
	return slices.Contains(ActionKinds, k)
}

// Action is what a call does, read from its arguments by its own tool:
// Description, the model's own account of it, empty when it gave none, and
// exactly one kind.
type Action struct {
	Description string           `json:"description"`
	Command     *CommandAction   `json:"command"`
	Read        *ReadAction      `json:"read"`
	Write       *WriteAction     `json:"write"`
	Edit        *EditAction      `json:"edit"`
	Search      *SearchAction    `json:"search"`
	Fetch       *FetchAction     `json:"fetch"`
	Memory      *MemoryAction    `json:"memory"`
	Delegate    *DelegateAction  `json:"delegate"`
	KubeQuery   *KubeQueryAction `json:"kubeQuery"`
	LogsView    *LogsViewAction  `json:"logsView"`
	LogsRead    *LogsReadAction  `json:"logsRead"`
	LogsWatch   *LogsWatchAction `json:"logsWatch"`
}

// Kind is the kind whose field is set, or "" for none.
func (a Action) Kind() ActionKind {
	switch {
	case a.Command != nil:
		return ActionCommand
	case a.Read != nil:
		return ActionRead
	case a.Write != nil:
		return ActionWrite
	case a.Edit != nil:
		return ActionEdit
	case a.Search != nil:
		return ActionSearch
	case a.Fetch != nil:
		return ActionFetch
	case a.Memory != nil:
		return ActionMemory
	case a.Delegate != nil:
		return ActionDelegate
	case a.KubeQuery != nil:
		return ActionKubeQuery
	case a.LogsView != nil:
		return ActionLogsView
	case a.LogsRead != nil:
		return ActionLogsRead
	case a.LogsWatch != nil:
		return ActionLogsWatch
	}
	return ""
}

// CommandAction is a command: Text is what the shell receives, byte for byte;
// Cwd is where it started, empty when the call never reached the gate;
// Background is a command that keeps running after its call answers; Network
// is a call that asked for the internet, which changes nothing outside the
// sandbox.
type CommandAction struct {
	Text       string `json:"text"`
	Cwd        string `json:"cwd"`
	Background bool   `json:"background"`
	Network    bool   `json:"network"`
	// Sandboxed is whether a sandbox confined the command, off its row.
	Sandboxed bool `json:"sandboxed"`
	// Env is the user's variables a command outside the sandbox was shown
	// with, NAME=value, off its row.
	Env []string `json:"env,omitempty"`
}

// ReadAction is a read of the file the call named, as it named it.
type ReadAction struct {
	Path string `json:"path"`
}

// WriteAction is a file written whole: Path as the call named it, and Content,
// the file's new bytes.
type WriteAction struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// EditAction is a replacement in a file: Path as the call named it, OldString
// the text that goes and NewString the text that replaces it, byte for byte,
// and ReplaceAll every occurrence rather than one.
type EditAction struct {
	Path       string `json:"path"`
	OldString  string `json:"oldString"`
	NewString  string `json:"newString"`
	ReplaceAll bool   `json:"replaceAll"`
}

// SearchAction is a web search: Query is what left the machine, empty for a call
// that sent none.
type SearchAction struct {
	Query string `json:"query"`
}

// FetchAction is a page fetched: URL as the sidecar fetches it, https with its
// host in ASCII, and Host, the host it dials, with the port when it is not 443.
type FetchAction struct {
	URL  string `json:"url"`
	Host string `json:"host"`
}

// MemoryAction is a call of the Memory tool as its arguments name it: Op, Name,
// on a save the note's Body, and Scope, who the note reaches: cluster, or
// everywhere for a call that asks the user.
type MemoryAction struct {
	Op    string `json:"op"`
	Name  string `json:"name"`
	Body  string `json:"body"`
	Scope string `json:"scope"`
}

// KubeQueryAction is a statement over the chat's cluster's cache: SQL as it runs,
// without a trailing semicolon, and Limit, the most rows the call asked for.
type KubeQueryAction struct {
	SQL   string `json:"sql"`
	Limit int    `json:"limit"`
}

// LogsViewAction is a live view of logs the user looks at: the LogsScopeInput the
// log backend reads, then the Anchor the view opens at and PinToEnd, whether the
// viewer keeps the end in view as lines arrive. Those two are the viewer's, so the
// backend never sees them. ResolvedAt is when the tool resolved it, on its clock:
// the moment a view that no longer runs shows its lines up to.
type LogsViewAction struct {
	LogsScopeInput
	Anchor     LogsViewAnchor `json:"anchor"`
	PinToEnd   bool           `json:"pinToEnd"`
	ResolvedAt time.Time      `json:"resolvedAt"`
}

// LogsViewAnchor is where a view opens: an edge of the stream, or the moment At,
// which is set for LogsAnchorAt alone.
type LogsViewAnchor struct {
	Kind LogsAnchorKind `json:"kind"`
	At   *time.Time     `json:"at"`
}

// LogsAnchorKind is the oldest line the backend keeps, the newest, or a moment
// between. The GraphQL LogsViewAnchorKind enum binds member by member onto it.
type LogsAnchorKind string

const (
	LogsAnchorHead LogsAnchorKind = "head"
	LogsAnchorTail LogsAnchorKind = "tail"
	LogsAnchorAt   LogsAnchorKind = "at"
)

// LogsReadAction is log lines the model read: the scope, the window as the service
// ran it (Since and Until the range's bounds, nil for open; Before and After its
// padding; PageFrom which end the first page came from, nil on a continuation),
// ResolvedAt the first page's clock, Lines what the model read, oldest first,
// stripped, redacted and cut, OlderMore and NewerMore whether the page had a
// prev or a next, Notices what the page said, and ViewAround the view View around
// this opens, nil with no line. Every string but the scope's is cluster text.
type LogsReadAction struct {
	LogsScopeInput
	Since      *time.Time      `json:"since"`
	Until      *time.Time      `json:"until"`
	Before     LogsPadding     `json:"before"`
	After      LogsPadding     `json:"after"`
	PageFrom   *logs.PageFrom  `json:"pageFrom"`
	ResolvedAt time.Time       `json:"resolvedAt"`
	Lines      []LogsReadLine  `json:"lines"`
	OlderMore  bool            `json:"olderMore"`
	NewerMore  bool            `json:"newerMore"`
	Notices    []LogsNotice    `json:"notices"`
	ViewAround *LogsViewAction `json:"viewAround"`
}

// LogsPadding is one side's padding as the service ran it: a number of lines, or
// a span as an ISO 8601 duration, normalised. At most one is set.
type LogsPadding struct {
	Lines int    `json:"lines"`
	Span  string `json:"span"`
}

// LogsPaddingOf is p as an action keeps it, a span as ISO 8601.
func LogsPaddingOf(p logs.Padding) LogsPadding {
	return LogsPadding{Lines: p.Lines, Span: iso8601.Format(p.Span)}
}

// LogsNotice is something a page or a follow said, by code.
type LogsNotice struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// LogsWatchAction is what a watch follows: the scope it watches, and its bounds,
// resolved on the tool's clock. Since is where the window starts, the call's own
// or ResolvedAt, nil when the watch went on from a read's token, which names no
// moment; Limit and MaxBytes are how many matching lines, and how many bytes of
// them, end it; MaxMessageBytes cuts each line; Until is when it ends on time
// alone.
type LogsWatchAction struct {
	LogsScopeInput
	Since           *time.Time `json:"since"`
	Limit           int        `json:"limit"`
	MaxBytes        int        `json:"maxBytes"`
	MaxMessageBytes int        `json:"maxMessageBytes"`
	ResolvedAt      time.Time  `json:"resolvedAt"`
	Until           time.Time  `json:"until"`
}

// LogsReadLine is one line of a sample, as the model read it, and the hole before
// it, nil with none.
type LogsReadLine struct {
	Timestamp time.Time `json:"timestamp"`
	Namespace string    `json:"namespace"`
	Pod       string    `json:"pod"`
	Container string    `json:"container"`
	Previous  bool      `json:"previous"`
	Message   string    `json:"message"`
	Gap       *LogsGap  `json:"gap"`
	// TruncatedBytes is the bytes the request's message cap cut off Message.
	TruncatedBytes int `json:"truncatedBytes"`
}

// The view's backend half is the log service's vocabulary, named here as the tool
// spells it.
type (
	LogsScopeInput  = logs.Scope
	LogsViewSource  = logs.ScopeSource
	LogsSourceKind  = logs.SourceKind
	LogsViewFilter  = logs.ScopeFilter
	LogsFilterField = logs.FilterField
	LogsGap         = logline.Gap
)

const (
	LogsSourcePod         = logs.SourcePod
	LogsSourceDeployment  = logs.SourceDeployment
	LogsSourceStatefulSet = logs.SourceStatefulSet
	LogsSourceDaemonSet   = logs.SourceDaemonSet
	LogsSourceJob         = logs.SourceJob
	LogsSourceCronJob     = logs.SourceCronJob
	LogsSourceReplicaSet  = logs.SourceReplicaSet

	LogsFilterNode   = logs.FilterNode
	LogsFilterRegion = logs.FilterRegion
	LogsFilterZone   = logs.FilterZone
	LogsFilterOS     = logs.FilterOS
	LogsFilterArch   = logs.FilterArch
)

// DelegateAction is a task handed to a subagent: Prompt, the brief the parent
// wrote; AgentType, the kind of agent; Model, the model it runs on, empty for
// the parent's.
type DelegateAction struct {
	Prompt    string `json:"prompt"`
	AgentType string `json:"agentType"`
	Model     string `json:"model"`
}

// Bounded is a Runner whose calls run under a bound of their own rather than the
// turn's DefaultToolTimeout.
type Bounded interface {
	Runner
	// CallTimeout is the loop's bound on this call. input is the call's own; an
	// input the tool cannot read gets any bound, since it never runs.
	CallTimeout(input json.RawMessage) time.Duration
}

// ChatDir is a chat's own directory, deleted with the chat: its saved results,
// its background tasks' output and its workspace.
type ChatDir interface {
	// Path is the directory as a result names it to the model. It may not exist.
	Path() string
	// Root opens the directory as a root, so no name under it resolves outside
	// it. create makes it, 0700, when it is missing; without create a missing
	// directory is fs.ErrNotExist. The caller closes the root.
	Root(create bool) (*os.Root, error)
}

// Tasks is a chat's background tasks.
type Tasks interface {
	// Start records a task for the call now running and starts it. start gets
	// the task's output file, open for writing, and returns the running process.
	// It answers the task's id and the file's path as a result names it, or
	// ErrChatTaskLimit, ErrTaskLimit, or why the task could not start.
	Start(start func(out *os.File) (Task, error)) (id, path string, err error)
	// Stop stops a running task of this chat on the model's word. false when
	// the chat has no running task by that id.
	Stop(id string) bool
}

// The limits a task start can meet: the chat's own, or the app's.
var (
	ErrChatTaskLimit = errors.New("tools: the chat runs as many background tasks as it may")
	ErrTaskLimit     = errors.New("tools: the app runs as many background tasks as it may")
)

// StartRefusal is what the model reads when a task could not start: a limit in
// words it can act on, naming commands, agents and watches since they share the
// slots, or the start's own error.
func StartRefusal(err error) string {
	switch {
	case errors.Is(err, ErrChatTaskLimit):
		return "At most 4 background commands, agents and watches run at once in a chat. Wait for a notification, or stop one with TaskStop."
	case errors.Is(err, ErrTaskLimit):
		return "At most 16 background commands, agents and watches run at once in Kubetail. Wait for one to finish."
	}
	return "could not start: " + safe.String(err.Error())
}

// TaskOutputLimit is the most of a task's output its file keeps: FileLimit less
// room for the line that ends it, so Read still opens a full file.
const TaskOutputLimit = FileLimit - 64

// Task is one background process a tool started.
type Task interface {
	// Wait blocks until the process is reaped and its output copied, and
	// answers how it ended. It releases everything the process held.
	Wait() Exit
	// Stop ends the process's group, or its job on Windows. now kills at once;
	// otherwise the tool's own grace applies where the platform has one. Safe to
	// call more than once, and after the reap, when it does nothing.
	Stop(now bool)
}

// Exit is how a task ended: its code as the foreground reads it, OK false when
// the code could not be read, and Stopped when a stop reached it before the reap.
type Exit struct {
	Code    int
	OK      bool
	Stopped bool
}

// Watches is a chat's log watches.
type Watches interface {
	// Start holds w as a background task of the call now running, and answers the
	// task's id, or ErrChatTaskLimit, ErrTaskLimit, or why it could not start. w is
	// the chat's from here: the chat calls its Wait once, and Stop on any stop.
	Start(w Watch) (id string, err error)
}

// Watch is a follow of logs a tool opened, which the chat holds as a task until
// it ends.
type Watch interface {
	// Wait reads the follow until the watch ends: full, past its deadline, ended
	// by the follow, or stopped. It returns once the follow is let go.
	Wait()
	// Stop ends the follow. Safe to call more than once, and after Wait.
	Stop()
	// End is how the watch ended, once Wait has returned; stoppedBy is who
	// stopped it, the chat's word, "" for none.
	End(stoppedBy string) WatchEnd
}

// WatchEnd is how a watch ended, and what it saw.
type WatchEnd struct {
	Status WatchStatus
	Result WatchResult
}

// WatchStatus is how a watch ended: holding lines, at its time with none, on an
// error or an end of the follow's with none, or stopped.
type WatchStatus string

const (
	WatchMatched WatchStatus = "matched"
	WatchExpired WatchStatus = "expired"
	WatchFailed  WatchStatus = "failed"
	WatchStopped WatchStatus = "stopped"
)

// WatchResult is a finished watch as its task's row keeps it: Text, what the
// model reads, a sample whose header names the window, why it ended and the
// cursors to read around; Lines, the lines it held as the model read them;
// EndedBecause, why it ended, in the header's words; ViewAround, the view View
// around this opens, nil with no line.
type WatchResult struct {
	Text         string          `json:"text"`
	Lines        []LogsReadLine  `json:"lines"`
	EndedBecause string          `json:"endedBecause"`
	ViewAround   *LogsViewAction `json:"viewAround"`
}

// Stamp is what a turn last saw of a file: the SHA-256 of its bytes, and
// whether the model was shown every byte as it is.
type Stamp struct {
	Sum   [32]byte
	Whole bool
}

// FileStamps is what a chat's turns have seen of the files they read or
// wrote, by the path as fileguard.Abs spells it.
type FileStamps interface {
	Stamp(path string) (Stamp, bool)
	SetStamp(path string, s Stamp)
}

// Delegation is one Agent call's request: the type, the brief, and the model
// override, empty for the parent's.
type Delegation struct {
	Type, Prompt, Model string
}

// ErrUnknownModel is a model the parent's provider does not offer with tools.
var ErrUnknownModel = errors.New("tools: no such model takes tools")

// Spawner starts a subagent for the call now running.
type Spawner interface {
	// Start writes the subagent's run and task and starts it. It answers the
	// task's id, or ErrUnknownModel, ErrChatTaskLimit, ErrTaskLimit, or the
	// error that stopped it.
	Start(ctx context.Context, d Delegation) (id string, err error)
}

// ClusterWrite is what a sandboxed command sent to change the cluster, as the
// user is shown it.
type ClusterWrite struct {
	Method string `json:"method"`
	// Path is the path and raw query, as forwarded.
	Path string `json:"path"`
	// Subresource is the path's subresource as the proxy parsed it; "" for none.
	Subresource string `json:"subresource"`
	ContentType string `json:"contentType"`
	// Body is valid UTF-8, or empty.
	Body   string `json:"body"`
	DryRun bool   `json:"dryRun"`
}

// CloudRequest is what a sandboxed command sent to a cloud platform, as the
// user is shown it. The cloud platform is the action's Target.
type CloudRequest struct {
	Host        string `json:"host"` // the upstream host dialled
	Method      string `json:"method"`
	Path        string `json:"path"` // path and raw query, as forwarded
	ContentType string `json:"contentType"`
	Body        string `json:"body"` // as sent; empty with Unshown
	// Unshown is the cloud platform's sentence for a body the request does not
	// show.
	Unshown string   `json:"unshown,omitempty"`
	Details []Detail `json:"details,omitempty"`
}

// Detail is one line the cloud platform's classifier read from the request: a
// label of the cloud platform's, and the command's text.
type Detail struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ActionRequest is one classified action held for the user, or decided with
// nobody asked. It is what an action approval's request column holds. At most
// one of Write and Cloud is set.
type ActionRequest struct {
	Action permissions.Action `json:"action"`
	// Grantable is whether an answer may write a rule that allows it.
	Grantable bool `json:"grantable"`
	// CommandRule and ChatRule are the rules each allow answer adds, in the
	// words Settings uses: Rule.Line of the rule a command answer adds, and of
	// the one a chat or always answer writes. Empty when not grantable.
	CommandRule string `json:"commandRule"`
	ChatRule    string `json:"chatRule"`
	// Write is the request as sent; nil for an action with none.
	Write *ClusterWrite `json:"write,omitempty"`
	// Cloud is a request to a cloud platform as sent; nil for a cluster
	// request.
	Cloud *CloudRequest `json:"cloud,omitempty"`
	// Diff is the change as a unified diff of YAML; "" for none.
	Diff string `json:"diff"`
	// DiffCut is a diff that stops short of the whole change.
	DiffCut bool `json:"diffCut"`
	// DiffError is why there is no diff, when one was looked for.
	DiffError string `json:"diffError"`
}

// Answer is the user's decision on an action.
type Answer = permissions.Answer

// ActionAsker puts an action to the user, as a request of the call that is
// running, and records one the proxy decided with nobody asked against that
// call, with the reason in the user's words.
type ActionAsker interface {
	Ask(ctx context.Context, r ActionRequest) (Answer, error)
	Record(ctx context.Context, r ActionRequest, d permissions.Decision, reason string) error
}

// Runtime is what a tool gets of the chat its call runs in. chat sets every
// field it has; a test sets the ones its tool reads. ClusterID is the chat's stored
// cluster, the one a tool acting on a cluster reaches, and ChatID the chat; a model
// names neither. Session is the run's policy, the chat's switch as its turn read
// it included. Agent and Watches are nil in a subagent's and a monitor's runtime,
// so neither spawns an agent or starts a watch. ActionAsker is nil where nobody can
// be asked. ServerUID is the server a monitor run reads, "" for a chat's, which
// reads whichever server its cluster reaches: a tool reaching the cluster refuses
// a cache or a connection of another and calls ServerMoved, which ends the run.
type Runtime struct {
	ClusterID   apimeta.ClusterID
	ChatID      apimeta.ChatID
	Session     session.Session
	Dir         ChatDir
	Tasks       Tasks
	Files       FileStamps
	Agent       Spawner
	Watches     Watches
	ActionAsker ActionAsker
	ServerUID   string
	// OnServerMoved ends the run; nil where ServerUID is "".
	OnServerMoved func()
}

// ServerMoved tells the run that the cluster reaches another server than
// ServerUID. It ends a monitor run and does nothing in a chat's.
func (rt Runtime) ServerMoved() {
	if rt.OnServerMoved != nil {
		rt.OnServerMoved()
	}
}
