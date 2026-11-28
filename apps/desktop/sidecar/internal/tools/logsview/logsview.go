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

// Package logsview is LogsView: a live view of logs the user looks at, opened by
// the model's call. The tool checks the call's sources against the mirror,
// resolves its anchor on the sidecar's clock, hands what it showed back as the
// call's action and answers a one-line receipt. It reads no log line and asks
// no one.
package logsview

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/logsinput"
)

//go:embed prompts/logsview.md
var prompt string

// description is the tool description the model reads with the schema.
//
//go:embed prompts/description.md
var description string

//go:embed prompts/schema.json
var inputSchema []byte

// Name is the name LogsView is offered under.
const Name = "LogsView"

// ContractName is the shape of the tool's stored calls, recorded on each row.
const ContractName tools.ContractName = "logsview/v1"

// errNotShown is a call read off its arguments: the view is the run's.
var errNotShown = errors.New("logsview: the view is the run's")

var _ tools.Shown = (*Tool)(nil)

// Tool is LogsView, over the cluster service whose mirror it checks against,
// and the clock a duration is measured back from: time.Now, or a test's.
type Tool struct {
	svc cluster.Service
	now func() time.Time
}

// New is the tool over svc.
func New(svc cluster.Service) *Tool { return &Tool{svc: svc, now: time.Now} }

func (t *Tool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{Name: Name, Description: description, InputSchema: json.RawMessage(inputSchema)}
}

func (t *Tool) Prompt() string { return prompt }

func (t *Tool) Name() string { return Name }

func (t *Tool) ContractName() tools.ContractName { return ContractName }

func (t *Tool) ActionKind() tools.ActionKind { return tools.ActionLogsView }

// Action is none: the view is what the run showed, which RunShown hands back and
// the row keeps, so a call that did not open one shows its kind alone.
func (t *Tool) Action(json.RawMessage, string, bool) (tools.Action, error) {
	return tools.Action{}, errNotShown
}

// RunShown checks the call's sources, resolves its anchor, and answers the
// receipt with the view as the action. A refusal names what to change and
// never the model's text.
func (t *Tool) RunShown(ctx context.Context, rt tools.Runtime, raw json.RawMessage) (string, bool, *tools.Action) {
	var anchorText string
	var pinToEnd bool
	in, err := logsinput.Parse(raw, logsinput.Fields{"anchor": &anchorText, "pin_to_end": &pinToEnd})
	if err != nil {
		return logsinput.Refusal(err), true, nil
	}
	if len(in.Sources) == 0 {
		return logsinput.Refusal(logsinput.BadInput("sources", "")), true, nil
	}
	now := t.now()
	anchor, err := anchorOf(anchorText, now)
	if err != nil {
		return logsinput.Refusal(err), true, nil
	}
	view, notes, err := logsinput.Resolve(ctx, t.svc, now, rt.ClusterID, rt.ServerUID, in)
	if err != nil {
		return logsinput.RefusalIn(rt, err), true, nil
	}
	view.Anchor, view.PinToEnd = anchor, pinToEnd
	return receipt(in.Sources, view, notes), false, &tools.Action{Description: in.Description, LogsView: &view}
}

// Run is RunShown with the action dropped.
func (t *Tool) Run(ctx context.Context, rt tools.Runtime, raw json.RawMessage) (string, bool) {
	text, isError, _ := t.RunShown(ctx, rt, raw)
	return text, isError
}

// receipt is the one line the model reads of a view that opened: the first
// source and how many more, where it opened and whether it is pinned, then what
// each source shows beyond its name, each note under its source when there are
// several, then the grep. Every name in it is cluster data, redacted as a
// command's output is.
func receipt(typed []logsinput.Source, view tools.LogsViewAction, notes []logsinput.Note) string {
	var b strings.Builder
	b.WriteString("Viewing " + logsinput.SourcesText(typed))
	b.WriteString(" " + anchorText(view.Anchor))
	if view.PinToEnd {
		b.WriteString(", pinned to the end")
	}
	b.WriteString(".")
	for _, text := range logsinput.NotesText(typed, view.Sources, notes) {
		b.WriteString(text)
	}
	if view.Grep != "" {
		b.WriteString(" Matching /" + view.Grep + "/.")
	}
	return safe.Redact(b.String())
}

// anchorText is where the view opened.
func anchorText(a tools.LogsViewAnchor) string {
	switch a.Kind {
	case tools.LogsAnchorHead:
		return "from the start"
	case tools.LogsAnchorAt:
		return "from " + a.At.Format(time.RFC3339)
	}
	return "at the newest line"
}

// anchorOf is the anchor the model typed on the clock: tail, head, or a moment.
func anchorOf(s string, now time.Time) (tools.LogsViewAnchor, error) {
	switch s {
	case "", "tail":
		return tools.LogsViewAnchor{Kind: tools.LogsAnchorTail}, nil
	case "head":
		return tools.LogsViewAnchor{Kind: tools.LogsAnchorHead}, nil
	}
	at, err := logsinput.MomentOf(s, now, logsinput.Since, logsinput.Behind)
	if err != nil {
		return tools.LogsViewAnchor{}, logsinput.BadInput("anchor", logsinput.MomentMessage(err, "tail, head, an RFC 3339 time, or an ISO 8601 duration back from now such as PT15M", now))
	}
	return tools.LogsViewAnchor{Kind: tools.LogsAnchorAt, At: &at}, nil
}
