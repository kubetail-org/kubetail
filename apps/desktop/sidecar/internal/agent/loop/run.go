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

// Package loop is the loop that takes a turn: the model is asked, the calls it
// asks for are run and answered, and it is asked again, every step told to a
// recorder as it happens.
package loop

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// Code is a refusal the loop writes in place of a result: {"error":"<code>"} with
// IsError, never a Go error's text. Exported so a recorder keys on the constant
// rather than on its own spelling of it.
type Code string

const (
	CodeBudget      Code = "budget"       // a reply asking for more calls than the turn has left
	CodeNotRun      Code = "not-run"      // a call nothing ran: a settled reply's, or one behind a failed write
	CodeUnknownTool Code = "unknown-tool" // a name the box lacks
	CodeTimeout     Code = "timeout"      // a call that ended on its own deadline
	CodeCancelled   Code = "cancelled"    // a call the turn's cancel ended, and every call left behind it
	CodeBadInput    Code = "bad-input"    // a gated call whose input its tool cannot read
	CodeDenied      Code = "denied"       // a gated call the user said no to
)

// maxPauses is how many times one turn asks again after the provider paused a
// reply in its own server tool loop. With the budget's rounds, the refusal and
// the synthesis, a turn streams at most MaxToolCalls + 2 + maxPauses times.
const maxPauses = 3

// codes is every Code, so RefusalOf reads the loop's refusals alone.
var codes = map[Code]bool{
	CodeBudget: true, CodeNotRun: true, CodeUnknownTool: true, CodeTimeout: true,
	CodeCancelled: true, CodeBadInput: true, CodeDenied: true,
}

// Text is the result the model reads for the refusal.
func (c Code) Text() string { return `{"error":"` + string(c) + `"}` }

// RefusalOf is the code a result's text carries when the text is one of the loop's
// refusals. A tool's own error that happens to spell one exactly reads as it too.
func RefusalOf(text string) (Code, bool) {
	var obj map[string]string
	if json.Unmarshal([]byte(text), &obj) != nil || len(obj) != 1 {
		return "", false
	}
	c := Code(obj["error"])
	return c, codes[c]
}

// Approver gets the user's decision on a gated call. Chat implements it over its
// rows and live overlay.
type Approver interface {
	// Approve blocks until the user decides or ctx ends. false is a denial. An
	// error is a write that did not land, or the wait answering ctx's end.
	Approve(ctx context.Context, call llm.Block, approval tools.Approval) (bool, error)
}

// Turn is one turn to take: what runs it, what the model is told, what it is
// asked, and what it may ask for.
type Turn struct {
	Target       llm.Target
	SystemPrompt string // the caller's own; the agent's sections follow it
	Messages     []llm.Message
	// AffinityKey names the conversation every request of the turn belongs to
	// (llm.Request.AffinityKey). Empty sends none.
	AffinityKey string
	// Tools is the turn's box, already chosen for Target: the calls it runs, and
	// the provider's tools it offers, each at the turn's budget. Every round is
	// offered what the earlier rounds left of a budget, and a spent one is not
	// offered. A server call is the provider's: no tool_use, and no place in
	// MaxToolCalls.
	Tools tools.Box
	// Runtime is the chat the turn runs in, handed to every call's Approval and
	// Run.
	Runtime tools.Runtime
	// MaxToolCalls is the calls one turn may make, counted per tool_use block.
	MaxToolCalls int
	// DefaultToolTimeout is the deadline on the context a call runs under when its
	// tool is not tools.Bounded; a bounded tool's own replaces it. The bound is
	// cooperative: the loop waits for Run to return, and a tool must honour its
	// context. A call that ends on it is answered timeout and the turn goes on.
	DefaultToolTimeout time.Duration
}

// Result is what a run produced, valid whether or not Run returned an error: a
// cancelled or failed run's rounds so far are still the answer's.
type Result struct {
	// Blocks is the rounds: every reply's blocks and every result, in order.
	Blocks []llm.Block
	// StopReason is the last reply's.
	StopReason string
}

// Run takes one turn on the calling goroutine, streaming until the model stops
// asking for tools or the budget is spent. A recorder write that did not land
// stops what would have followed it. Settled is told once, last, on every path
// out, with the turn's outcome; its error and any write error the turn survived
// join the one returned. Cancellation is the stream's and the tools' to honour. A
// gated tool's call is put to approver before it runs.
func Run(ctx context.Context, turn Turn, rec Recorder, approver Approver) (Result, error) {
	r := &runner{
		turn: turn, rec: rec, approver: approver, systemPrompt: SystemPrompt(turn),
		remaining: turn.MaxToolCalls, budgeted: turn.Tools.Budgeted(), serverUsed: map[string]int{},
	}
	res, err := r.loop(ctx)
	settleErr := rec.Settled(ctx, res, err)
	return res, errors.Join(err, r.reported, settleErr)
}

// runner takes one turn: the loop, and what it carries from one round to the next.
type runner struct {
	turn         Turn
	rec          Recorder
	approver     Approver
	systemPrompt string           // assembled once; the same on every round
	rounds       []llm.Block      // every reply's blocks and every result so far
	remaining    int              // the budget left
	pauses       int              // the paused replies asked again so far
	budgeted     []tools.Budgeted // the provider's tools the turn offers
	serverUsed   map[string]int   // each server tool's calls so far, by name
	synthesis    bool             // whether the one round granted after a refused budget is spent
	reported     error            // a finish write that failed under a reply that asked for nothing
}

func (r *runner) loop(ctx context.Context) (Result, error) {
	for {
		if err := r.rec.LLMCallStarted(ctx, r.turn.Target.Model.ID); err != nil {
			return r.result(""), err
		}

		var partial llm.Partial
		shown := false
		defs, native := r.offer()
		resp, streamErr := r.turn.Target.Stream(ctx, r.systemPrompt, r.messages(), r.turn.AffinityKey, defs, native, func(c llm.Chunk) {
			switch {
			case c.Kind == llm.ChunkServer:
				// The wire claims a server call only for a tool the request offered.
				if tool, ok := r.serverTool(c.Call.Name); ok {
					r.rec.ServerCallSeen(c.Call, tool)
				}
			case !shown:
				shown = true
				r.rec.LLMCallFirstChunk()
			}
			partial.Add(c)
			r.rec.Progress(slices.Concat(r.rounds, partial.Blocks()))
		})
		if streamErr != nil {
			// A stream that broke off has no response: what was streamed is the reply.
			finishErr := r.rec.LLMCallFinished(ctx, llm.Response{}, streamErr)
			r.rounds = append(r.rounds, partial.Blocks()...)
			return r.result(""), errors.Join(streamErr, finishErr)
		}
		finishErr := r.rec.LLMCallFinished(ctx, resp, nil)
		r.countServerUses(resp)
		r.rounds = append(r.rounds, resp.Blocks...)
		calls := toolUses(resp.Blocks)
		if len(calls) > 0 {
			// The calls arrive whole at the end, so no chunk carried them: publish
			// them before anything runs, and a turn that ends inside a tool keeps them.
			r.rec.Progress(slices.Clone(r.rounds))
		}

		switch {
		case finishErr != nil && len(calls) > 0:
			// Nothing external follows a write that did not land.
			err := r.refuse(ctx, calls, CodeNotRun)
			return r.result(resp.StopReason), errors.Join(finishErr, err)
		case finishErr != nil:
			// Nothing would have followed: the turn settles as it would have, and
			// the settle heals the row.
			r.reported = finishErr
			return r.result(resp.StopReason), nil
		case resp.StopReason == llm.StopPauseTurn && len(calls) == 0 && r.pauses < maxPauses && r.serverToolsLeft():
			// The provider paused its own server tool loop; its blocks are already
			// the last message, so asking again resumes it.
			r.pauses++
		case resp.StopReason != llm.StopToolUse:
			err := r.refuse(ctx, calls, CodeNotRun)
			return r.result(resp.StopReason), err
		case len(calls) == 0:
			// A malformed reply: tool_use, and nothing to run.
			return r.result(resp.StopReason), nil
		case len(calls) > r.remaining || r.synthesis:
			// None runs rather than a prefix, so the model is never answered for
			// half of what it asked in one breath.
			if err := r.refuse(ctx, calls, CodeBudget); err != nil || r.synthesis {
				return r.result(resp.StopReason), err
			}
			r.synthesis = true
		default:
			r.remaining -= len(calls)
			if err := r.runCalls(ctx, calls); err != nil {
				return r.result(resp.StopReason), err
			}
		}
	}
}

// messages is what the next call is asked: the chat so far, then this turn's
// rounds as the last assistant message.
func (r *runner) messages() []llm.Message {
	if len(r.rounds) == 0 {
		return r.turn.Messages
	}
	return append(slices.Clone(r.turn.Messages), llm.Message{
		Role: "assistant", Blocks: slices.Clone(r.rounds),
		ProviderID: r.turn.Target.Provider.ID, Dialect: r.turn.Target.Provider.Dialect, Effort: r.turn.Target.Effort,
	})
}

// offer is the next round's: the box's, each of the provider's tools at what is
// left of its budget, and none that is spent, since no wire takes a cap of zero.
func (r *runner) offer() ([]llm.ToolDefinition, []llm.NativeOffer) {
	defs, native := r.turn.Tools.Offer()
	var out []llm.NativeOffer
	for _, o := range native {
		if o.Server {
			if o.MaxUses -= r.serverUsed[o.Tool.Name()]; o.MaxUses < 1 {
				continue
			}
		}
		out = append(out, o)
	}
	return defs, out
}

// serverTool is the provider's tool named name.
func (r *runner) serverTool(name string) (tools.Budgeted, bool) {
	for _, b := range r.budgeted {
		if b.Name() == name {
			return b, true
		}
	}
	return nil, false
}

// serverToolsLeft reports whether every server tool the turn offers has uses
// left. A paused reply is resumed only then: a request that drops a tool drops
// the calls of it from the history too, the paused reply's included.
func (r *runner) serverToolsLeft() bool {
	for _, b := range r.budgeted {
		if r.serverUsed[b.Name()] >= b.MaxUses() {
			return false
		}
	}
	return true
}

// countServerUses adds a reply's server calls to the turn's: the provider's
// count where it reported one, else the calls the reply holds.
func (r *runner) countServerUses(resp llm.Response) {
	for _, b := range r.budgeted {
		name := b.Name()
		n, reported := resp.ServerUses[name]
		if !reported {
			for _, block := range resp.Blocks {
				if block.Type == llm.BlockServerUse && block.Name == name {
					n++
				}
			}
		}
		r.serverUsed[name] += n
	}
}

func (r *runner) result(stopReason string) Result {
	return Result{Blocks: r.rounds, StopReason: stopReason}
}

// runCalls runs the reply's calls one at a time, in order, so one approval
// request at a time is before the user. A write that fails stops what would
// have followed it: that call and every later one are answered not-run, and
// the error ends the turn. A turn cancelled between calls starts none of the
// rest.
func (r *runner) runCalls(ctx context.Context, calls []llm.Block) error {
	for i, call := range calls {
		if err := ctx.Err(); err != nil {
			return errors.Join(err, r.refuse(ctx, calls[i:], CodeCancelled))
		}
		tool, ok := r.turn.Tools.Runner(call.Name)
		g := gateResult{code: CodeUnknownTool, refused: refusal(call, CodeUnknownTool)}
		if ok {
			var err error
			if g, err = r.gate(ctx, tool, call); err != nil {
				return errors.Join(err, r.refuse(ctx, calls[i:], g.code))
			}
		}
		if g.code != "" {
			// Nothing ran, so there is no start to tell.
			if err := r.answer(ctx, call, g.refused, nil); err != nil {
				return errors.Join(err, r.refuse(ctx, calls[i+1:], CodeNotRun))
			}
			continue
		}
		if err := r.rec.ToolCallStarted(ctx, call, g.approval); err != nil {
			return errors.Join(err, r.refuse(ctx, calls[i:], CodeNotRun))
		}
		result, shown := r.runOne(ctx, tool, call, g.approval)
		if err := r.answer(ctx, call, result, shown); err != nil {
			return errors.Join(err, r.refuse(ctx, calls[i+1:], CodeNotRun))
		}
	}
	return ctx.Err()
}

// gateResult is what gate found for a call: the approval its tool read, when it
// may run, or the code it is refused with and the result the model reads for it.
type gateResult struct {
	approval tools.Approval // the zero value for a tool that is not Gated
	code     Code           // "" when the call may run
	refused  llm.Block      // the code's own text, or a tool's *tools.Refusal
}

// gate is whether a call may run. A tool that is not Gated runs, and so does a
// call whose approval skips, with nothing put to the approver. An error ends the
// turn, and the result's code is what this call and every later one are
// answered: cancelled when the wait answered the turn's cancel, not-run when a
// write did not land. A decision stands whatever the clock says, since it was
// committed; only an approval checks the cancel once more, so a command whose
// turn was cancelled in that moment never starts.
func (r *runner) gate(ctx context.Context, tool tools.Runner, call llm.Block) (gateResult, error) {
	g, ok := tool.(tools.Gated)
	if !ok {
		return gateResult{}, nil
	}
	approval, err := g.Approval(ctx, r.turn.Runtime, call.Input)
	var own *tools.Refusal
	switch {
	case errors.As(err, &own):
		return gateResult{code: CodeBadInput, refused: llm.ToolResultBlock(call.ID, own.Result, true)}, nil
	case err != nil:
		return gateResult{code: CodeBadInput, refused: refusal(call, CodeBadInput)}, nil
	case approval.Skip:
		return gateResult{approval: approval}, nil
	}
	code, err := r.decide(ctx, call, approval)
	if code == "" {
		return gateResult{approval: approval}, err
	}
	return gateResult{code: code, refused: refusal(call, code)}, err
}

// decide puts a call to the approver: "" when it was approved, else the code it
// is refused with.
func (r *runner) decide(ctx context.Context, call llm.Block, approval tools.Approval) (Code, error) {
	approved, err := r.approver.Approve(ctx, call, approval)
	switch {
	case err != nil && ctx.Err() != nil:
		return CodeCancelled, err
	case err != nil:
		return CodeNotRun, err
	case !approved:
		return CodeDenied, nil
	case ctx.Err() != nil:
		return CodeCancelled, ctx.Err()
	}
	return "", nil
}

// runOne runs one call under its tool's bound, else the turn's, handing a
// tools.ApprovedRunner the approval its gate decided and keeping the action a
// tools.Shown tool resolved, and reads what it returned against the context it
// ran under. A result with no error is kept whatever the clock says, since the
// read happened. An error after the turn's cancel is the tool answering the
// cancel, and one after the deadline alone is it answering the deadline; the
// cancel is checked first, since a deadline under a cancelled turn says nothing
// about the tool.
func (r *runner) runOne(ctx context.Context, tool tools.Runner, call llm.Block, approval tools.Approval) (llm.Block, *tools.Action) {
	timeout := r.turn.DefaultToolTimeout
	if b, ok := tool.(tools.Bounded); ok {
		timeout = b.CallTimeout(call.Input)
	}
	callCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var text string
	var isError bool
	var shown *tools.Action
	switch tool := tool.(type) {
	case tools.ApprovedRunner:
		text, isError = tool.RunApproved(callCtx, r.turn.Runtime, call.Input, approval)
	case tools.Shown:
		text, isError, shown = tool.RunShown(callCtx, r.turn.Runtime, call.Input)
	default:
		text, isError = tool.Run(callCtx, r.turn.Runtime, call.Input)
	}
	switch {
	case !isError:
		return llm.ToolResultBlock(call.ID, text, false), shown
	case ctx.Err() != nil:
		return refusal(call, CodeCancelled), nil
	case callCtx.Err() != nil:
		return refusal(call, CodeTimeout), nil
	}
	return llm.ToolResultBlock(call.ID, text, true), nil
}

// answer puts one call's result in the rounds, tells the recorder, with the
// action the tool resolved, and publishes the rounds so far, so a reader sees each
// result as it lands and a turn that ends inside a later call keeps this one.
func (r *runner) answer(ctx context.Context, call, result llm.Block, shown *tools.Action) error {
	r.rounds = append(r.rounds, result)
	err := r.rec.ToolCallFinished(ctx, call, result, shown)
	r.rec.Progress(slices.Clone(r.rounds))
	return err
}

// refuse answers every call with code whatever the writes do, so the record pairs
// every tool_use with a result, and returns the write errors joined.
func (r *runner) refuse(ctx context.Context, calls []llm.Block, code Code) error {
	var errs []error
	for _, call := range calls {
		if err := r.answer(ctx, call, refusal(call, code), nil); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// refusal is the result the loop writes in place of one a tool produced.
func refusal(call llm.Block, code Code) llm.Block {
	return llm.ToolResultBlock(call.ID, code.Text(), true)
}

// toolUses is the reply's calls, in the order the model asked them.
func toolUses(blocks []llm.Block) []llm.Block {
	var out []llm.Block
	for _, b := range blocks {
		if b.Type == llm.BlockToolUse {
			out = append(out, b)
		}
	}
	return out
}
