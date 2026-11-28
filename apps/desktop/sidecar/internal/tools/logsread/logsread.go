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

// Package logsread is LogsRead: the model's read of a window of logs. The tool
// checks the call's scope against the mirror as LogsView does, reads one page of
// its window through the log service, or the page a printed token names, and
// answers the page as a sample whose header prints the tokens to read on with. The
// call's action keeps what the model read. It asks no one.
package logsread

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/logsinput"
)

//go:embed prompts/logsread.md
var prompt string

// description is the tool description the model reads with the schema.
//
//go:embed prompts/description.md
var description string

//go:embed prompts/schema.json
var inputSchema []byte

// Name is the name LogsRead is offered under.
const Name = "LogsRead"

// ContractName is the shape of the tool's stored calls, recorded on each row.
const ContractName tools.ContractName = "logsread/v1"

// A read's bounds: the page's own deadline, inside the loop's on the call, so a
// slow read answers timed-out itself rather than being cut off.
const (
	readTimeout = 25 * time.Second
	callTimeout = 30 * time.Second
)

// errNotShown is a call read off its arguments: the sample is the run's.
var errNotShown = errors.New("logsread: the sample is the run's")

// Fetcher reads one page of a window: the log service.
type Fetcher interface {
	Fetch(ctx context.Context, id cluster.ClusterID, q logs.FetchRequest) (logs.FetchResponse, error)
}

var (
	_ tools.Shown   = (*Tool)(nil)
	_ tools.Bounded = (*Tool)(nil)
)

// Tool is LogsRead, over the cluster service whose mirror it checks against and
// the log service it reads through. now is the clock a duration is measured back
// from and readTimeout the page's deadline: time.Now and readTimeout, or a test's.
type Tool struct {
	svc         cluster.Service
	logs        Fetcher
	now         func() time.Time
	readTimeout time.Duration
}

// New is the tool over svc and logs.
func New(svc cluster.Service, logs Fetcher) *Tool {
	return &Tool{svc: svc, logs: logs, now: time.Now, readTimeout: readTimeout}
}

func (t *Tool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{Name: Name, Description: description, InputSchema: json.RawMessage(inputSchema)}
}

func (t *Tool) Prompt() string { return prompt }

func (t *Tool) Name() string { return Name }

func (t *Tool) ContractName() tools.ContractName { return ContractName }

func (t *Tool) ActionKind() tools.ActionKind { return tools.ActionLogsRead }

// CallTimeout is the loop's bound on a call.
func (t *Tool) CallTimeout(json.RawMessage) time.Duration { return callTimeout }

// Action is none: the sample is what the run read, which RunShown hands back and
// the row keeps.
func (t *Tool) Action(json.RawMessage, string, bool) (tools.Action, error) {
	return tools.Action{}, errNotShown
}

// RunShown reads one page, the first of the call's window or the one its cursor
// names, and answers the sample with what the model read as the action. A first
// page resolves its scope against the mirror; a cursor's holds the scope as the
// read that printed it resolved it, which resolving again could change. A refusal
// names what to change and never the model's text.
func (t *Tool) RunShown(ctx context.Context, rt tools.Runtime, raw json.RawMessage) (string, bool, *tools.Action) {
	now := t.now()
	in, err := parse(raw, now)
	if err != nil {
		return logsinput.Refusal(err), true, nil
	}
	var view tools.LogsViewAction
	var notes []logsinput.Note
	if in.cursor == nil {
		if view, notes, err = logsinput.Resolve(ctx, t.svc, now, rt.ClusterID, rt.ServerUID, in.Input); err != nil {
			return logsinput.RefusalIn(rt, err), true, nil
		}
	}
	readCtx, cancel := context.WithTimeout(ctx, t.readTimeout)
	defer cancel()
	q := in.request(view.LogsScopeInput)
	q.ServerUID = rt.ServerUID
	resp, err := t.logs.Fetch(readCtx, rt.ClusterID, q)
	switch {
	// Before the deadline: a server move ends the run however the read ended.
	case errors.Is(err, cluster.ErrIdentityMismatch):
		return logsinput.RefusalIn(rt, err), true, nil
	case err != nil && errors.Is(readCtx.Err(), context.DeadlineExceeded):
		return `{"error":"timed-out"}`, true, nil
	case err != nil:
		return logsinput.RefusalIn(rt, err), true, nil
	}
	s := sampleOf(resp)
	read := s.action()
	return s.output(in, notes), false, &tools.Action{Description: in.Description, LogsRead: &read}
}

// Run is RunShown with the action dropped.
func (t *Tool) Run(ctx context.Context, rt tools.Runtime, raw json.RawMessage) (string, bool) {
	text, isError, _ := t.RunShown(ctx, rt, raw)
	return text, isError
}
