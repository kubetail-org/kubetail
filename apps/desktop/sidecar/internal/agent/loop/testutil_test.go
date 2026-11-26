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

package loop

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/anthropicwebsearch"
)

// recording logs every recorder call in order and can refuse any of the writes:
// each *Err answers the call of its method that the matching *FailAt names,
// counting from 0, so the first by default. progressed, when set, is closed on the
// first progress, for a test that acts once the stream is under way.
type recording struct {
	startErr, finishErr, toolStartErr, toolFinishErr             error
	startFailAt, finishFailAt, toolStartFailAt, toolFinishFailAt int
	progressed                                                   chan struct{}
	// approve answers Approve in its place; nil approves.
	approve func(ctx context.Context, call llm.Block, approval tools.Approval) (bool, error)

	log         []string // "started", "first-chunk", "progress", "server-call", "finished", "tool-started", "tool-finished", "settled"
	progress    [][]llm.Block
	serverCalls []llm.Block      // each call ServerCallSeen was told of, in order
	serverTools []llm.NativeTool // the tool each was told with
	starts      int
	finishes    int
	toolStarts  []llm.Block
	// startApprovals is the approval ToolCallStarted was told with each start.
	startApprovals []tools.Approval
	answers        []pair          // each call with the result ToolCallFinished was told for it, in order
	shown          []*tools.Action // the action ToolCallFinished was told beside each answer, in order
	result         Result
	streamErr      error
	finishedWith   []llm.Response // each response LLMCallFinished was told, in order
	settled        int
	settledErr     error            // the outcome Settled was told
	approvals      []tools.Approval // each approval Approve was asked about, in order
}

// pair is one call and its result.
type pair struct{ call, result llm.Block }

func (r *recording) LLMCallStarted(context.Context, string) error {
	r.log = append(r.log, "started")
	r.starts++
	return failAt(r.startErr, r.startFailAt, r.starts)
}

func (r *recording) LLMCallFirstChunk() {
	r.log = append(r.log, "first-chunk")
}

func (r *recording) Progress(blocks []llm.Block) {
	r.log = append(r.log, "progress")
	r.progress = append(r.progress, blocks)
	if r.progressed != nil && len(r.progress) == 1 {
		close(r.progressed)
	}
}

func (r *recording) ServerCallSeen(call llm.Block, tool tools.Tool) {
	r.log = append(r.log, "server-call")
	r.serverCalls = append(r.serverCalls, call)
	r.serverTools = append(r.serverTools, tool)
}

func (r *recording) LLMCallFinished(_ context.Context, resp llm.Response, streamErr error) error {
	r.log = append(r.log, "finished")
	r.finishedWith = append(r.finishedWith, resp)
	r.streamErr = streamErr
	r.finishes++
	return failAt(r.finishErr, r.finishFailAt, r.finishes)
}

func (r *recording) ToolCallStarted(_ context.Context, call llm.Block, approval tools.Approval) error {
	r.log = append(r.log, "tool-started")
	r.toolStarts = append(r.toolStarts, call)
	r.startApprovals = append(r.startApprovals, approval)
	return failAt(r.toolStartErr, r.toolStartFailAt, len(r.toolStarts))
}

func (r *recording) ToolCallFinished(_ context.Context, call, result llm.Block, shown *tools.Action) error {
	r.log = append(r.log, "tool-finished")
	r.answers = append(r.answers, pair{call, result})
	r.shown = append(r.shown, shown)
	return failAt(r.toolFinishErr, r.toolFinishFailAt, len(r.answers))
}

// Approve logs the text and answers by approve, yes by default: a recording is
// its own approver, so one log orders the decision against the writes.
func (r *recording) Approve(ctx context.Context, call llm.Block, approval tools.Approval) (bool, error) {
	r.log = append(r.log, "approve")
	r.approvals = append(r.approvals, approval)
	if r.approve != nil {
		return r.approve(ctx, call, approval)
	}
	return true, nil
}

// failAt is err on the call at (counting from 0), of which this is the nth
// (counting from 1), and nil on every other.
func failAt(err error, at, n int) error {
	if n-1 == at {
		return err
	}
	return nil
}

func (r *recording) Settled(_ context.Context, result Result, err error) error {
	r.log = append(r.log, "settled")
	r.result, r.settledErr, r.settled = result, err, r.settled+1
	return nil
}

// codes is the code each answer carries, in order: a refusal's code, or "" for a
// result a tool produced.
func (r *recording) codes() []Code {
	out := make([]Code, 0, len(r.answers))
	for _, a := range r.answers {
		var code Code
		if a.result.IsError {
			code, _ = RefusalOf(a.result.Text)
		}
		out = append(out, code)
	}
	return out
}

// searchCall is a search as the stream shows it: its call, with no payload.
func searchCall(id, query string) llm.Block {
	return llm.ServerUseBlock(id, searchTool{}.Name(), json.RawMessage(`{"query":"`+query+`"}`))
}

// searchTool is the provider's search as these tests offer it, capped at five a
// turn.
type searchTool struct{}

func (searchTool) Name() string                     { return anthropicwebsearch.Name }
func (searchTool) ContractName() tools.ContractName { return anthropicwebsearch.ContractName }
func (searchTool) ActionKind() tools.ActionKind     { return tools.ActionSearch }
func (searchTool) MaxUses() int                     { return 5 }
func (searchTool) Allowance() int                   { return 10_000 }
func (searchTool) Prompt() string                   { return "## Searching the web\n\nThe provider searches for you." }
func (searchTool) Action(json.RawMessage, string, bool) (tools.Action, error) {
	return tools.Action{Search: &tools.SearchAction{}}, nil
}

// echoTool answers a call with what it was asked, so a test can see the input
// reach it. run, when set, answers in its place.
type echoTool struct {
	name string
	run  func(ctx context.Context, input json.RawMessage) (string, bool)
}

func (e echoTool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{Name: e.name, Description: "says it back", InputSchema: json.RawMessage(`{"type":"object"}`)}
}

func (e echoTool) Prompt() string { return "## " + e.name + "\n\nSays back what it is given." }

func (e echoTool) Name() string { return e.name }
func (e echoTool) ContractName() tools.ContractName {
	return tools.ContractName(strings.ToLower(e.name) + "/v1")
}
func (e echoTool) ActionKind() tools.ActionKind { return tools.ActionCommand }
func (e echoTool) Action(input json.RawMessage, _ string, _ bool) (tools.Action, error) {
	return tools.Action{Command: &tools.CommandAction{Text: string(input)}}, nil
}

func (e echoTool) Run(ctx context.Context, _ tools.Runtime, input json.RawMessage) (string, bool) {
	if e.run != nil {
		return e.run(ctx, input)
	}
	return string(input), false
}

// shownTool is an echoTool whose run resolves its action: the input as a
// command's text, under what it answered, and none on a refusal.
type shownTool struct{ echoTool }

func (s shownTool) RunShown(ctx context.Context, rt tools.Runtime, input json.RawMessage) (string, bool, *tools.Action) {
	text, isError := s.echoTool.Run(ctx, rt, input)
	if isError {
		return text, true, nil
	}
	return text, false, &tools.Action{Description: text, Command: &tools.CommandAction{Text: string(input)}}
}

// gatedTool is an echoTool that asks for a decision: its approval's cwd is the
// input, unless approval answers in its place.
type gatedTool struct {
	echoTool
	approval func(input json.RawMessage) (tools.Approval, error)
}

func (g gatedTool) Approval(_ context.Context, _ tools.Runtime, input json.RawMessage) (tools.Approval, error) {
	if g.approval != nil {
		return g.approval(input)
	}
	return tools.Approval{Cwd: string(input)}, nil
}

// approvedTool is a gated tool that runs by the approval its gate decided,
// recording each, and answers from its plain Run never.
type approvedTool struct {
	gatedTool
	ran []tools.Approval
}

func (a *approvedTool) Run(context.Context, tools.Runtime, json.RawMessage) (string, bool) {
	return "ran without its approval", true
}

func (a *approvedTool) RunApproved(_ context.Context, _ tools.Runtime, input json.RawMessage, approval tools.Approval) (string, bool) {
	a.ran = append(a.ran, approval)
	return string(input), false
}

// runtimeTool is a gated tool that records the runtime each Approval and Run is
// handed, and skips so no approver is needed.
type runtimeTool struct {
	echoTool
	approvals, runs []tools.Runtime
}

func (r *runtimeTool) Approval(_ context.Context, rt tools.Runtime, _ json.RawMessage) (tools.Approval, error) {
	r.approvals = append(r.approvals, rt)
	return tools.Approval{Skip: true}, nil
}

func (r *runtimeTool) Run(_ context.Context, rt tools.Runtime, _ json.RawMessage) (string, bool) {
	r.runs = append(r.runs, rt)
	return "ran", false
}

// chatDir is a ChatDir that names its directory and opens nothing.
type chatDir string

func (d chatDir) Path() string                { return string(d) }
func (d chatDir) Root(bool) (*os.Root, error) { return nil, fs.ErrNotExist }
