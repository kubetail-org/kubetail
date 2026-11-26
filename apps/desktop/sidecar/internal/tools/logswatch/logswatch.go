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

// Package logswatch is LogsWatch: a tail of a scope that runs as a background
// task of the chat and wakes the model once, when it ends. The tool checks the
// call's scope against the mirror as LogsView does, or takes a read's token, which
// holds one, opens the log service's tail on a context of the watch's own, and
// hands it to the chat. The call answers at once with a receipt; the watch ends
// holding its limit of matching lines, at its time, or when the tail ends, and
// its end is a notice. It asks no one.
package logswatch

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/logsinput"
)

//go:embed prompts/logswatch.md
var prompt string

// description is the tool description the model reads with the schema.
//
//go:embed prompts/description.md
var description string

//go:embed prompts/schema.json
var inputSchema []byte

// Name is the name LogsWatch is offered under.
const Name = "LogsWatch"

// ContractName is the shape of the tool's stored calls, recorded on each row.
const ContractName tools.ContractName = "logswatch/v1"

// callTimeout bounds a call: a resolve and a tail's start, not the watch.
const callTimeout = 30 * time.Second

// errNotShown is a call read off its arguments: the watch is the run's.
var errNotShown = errors.New("logswatch: the watch is the run's")

// LogTailer opens a tail of a scope: the log service.
type LogTailer interface {
	TailFromCursor(ctx context.Context, id cluster.ClusterID, q logs.TailRequest) (logs.TailResponse, error)
}

var (
	_ tools.Shown   = (*Tool)(nil)
	_ tools.Bounded = (*Tool)(nil)
)

// Tool is LogsWatch, over the cluster service whose mirror it checks against and
// the log service it follows through. now is the clock its window is read on:
// time.Now, or a test's.
type Tool struct {
	svc  cluster.Service
	logs LogTailer
	now  func() time.Time
}

// New is the tool over svc and logs.
func New(svc cluster.Service, logs LogTailer) *Tool {
	return &Tool{svc: svc, logs: logs, now: time.Now}
}

func (t *Tool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{Name: Name, Description: description, InputSchema: json.RawMessage(inputSchema)}
}

func (t *Tool) Prompt() string { return prompt }

func (t *Tool) Name() string { return Name }

func (t *Tool) ContractName() tools.ContractName { return ContractName }

func (t *Tool) ActionKind() tools.ActionKind { return tools.ActionLogsWatch }

// CallTimeout is the loop's bound on a call.
func (t *Tool) CallTimeout(json.RawMessage) time.Duration { return callTimeout }

// Action is none: the watch is what the run resolved, which RunShown hands back and
// the row keeps.
func (t *Tool) Action(json.RawMessage, string, bool) (tools.Action, error) {
	return tools.Action{}, errNotShown
}

// RunShown checks the call's scope, or takes its token, opens its tail and
// starts the watch, and answers the receipt with what it watches as the action. A
// refusal names what to change and never the model's text.
func (t *Tool) RunShown(ctx context.Context, rt tools.Runtime, raw json.RawMessage) (string, bool, *tools.Action) {
	now := t.now()
	in, err := parse(raw, now)
	if err != nil {
		return logsinput.Refusal(err), true, nil
	}
	if rt.Watches == nil {
		return `{"error":"unavailable","message":"a watch runs only in a chat"}`, true, nil
	}
	q := logs.TailRequest{Cursor: in.cursor, MaxMessageBytes: in.maxMessageBytes, ForModel: true}
	if in.cursor == nil {
		view, _, err := logsinput.Resolve(ctx, t.svc, now, rt.ClusterID, in.Input)
		if err != nil {
			return logsinput.Refusal(err), true, nil
		}
		q.Scope, q.Since = view.LogsScopeInput, &in.since
	}
	// A tail lives as long as its context, and the call's ends with the call: the
	// watch's is free of it, ended by its deadline or the task's stop. Until the
	// tail has opened it is still the call's, so a stalled open ends with the call.
	tailCtx, cancel := context.WithDeadline(context.WithoutCancel(ctx), in.until)
	callEnds := context.AfterFunc(ctx, cancel)
	resp, err := t.logs.TailFromCursor(tailCtx, rt.ClusterID, q)
	if !callEnds() {
		return logsinput.Refusal(ctx.Err()), true, nil
	}
	if err != nil {
		cancel()
		return logsinput.Refusal(err), true, nil
	}
	// The scope is the tail's: on a token start the call named none.
	w := &watch{ctx: tailCtx, cancel: cancel, stream: resp.Frames, in: in, scope: resp.Request.Scope, since: q.Since, now: t.now}
	id, err := rt.Watches.Start(w)
	if err != nil {
		cancel()
		return tools.StartRefusal(err), true, nil
	}
	action := tools.LogsWatchAction{
		LogsScopeInput: w.scope, Since: w.since, Limit: in.limit, MaxBytes: in.maxBytes, MaxMessageBytes: in.maxMessageBytes,
		ResolvedAt: resp.ResolvedAt, Until: in.until,
	}
	return w.receipt(now, id), false, &tools.Action{Description: in.Description, LogsWatch: &action}
}

// Run is RunShown with the action dropped.
func (t *Tool) Run(ctx context.Context, rt tools.Runtime, raw json.RawMessage) (string, bool) {
	text, isError, _ := t.RunShown(ctx, rt, raw)
	return text, isError
}

// receipt is the one line the model reads of a watch that started on the clock
// now: what it watches, for what, from when, its limits and its end, cluster text
// redacted as LogsView's receipt is; then the task's id, which TaskStop takes.
func (w *watch) receipt(now time.Time, id string) string {
	pattern := "any line"
	if w.scope.Grep != "" {
		pattern = "/" + w.scope.Grep + "/"
	}
	from := "a read's token"
	switch {
	case w.since == nil:
	case w.since.Equal(now):
		from = "now"
	default:
		from = logsinput.Stamp(*w.since)
	}
	lines := "1 line"
	if w.in.limit > 1 {
		lines = fmt.Sprintf("%d lines", w.in.limit)
	}
	text := fmt.Sprintf("Watching %s for %s from %s, up to %s or %d bytes, until %s",
		logsinput.SourcesText(logsinput.SourcesOf(w.scope.Sources)), pattern, from, lines, w.in.maxBytes, logsinput.Stamp(w.in.until))
	if w.in.capped {
		text += ", the longest a watch runs"
	}
	return safe.Redact(text) + " (task " + id + "). You will be told when it ends."
}
