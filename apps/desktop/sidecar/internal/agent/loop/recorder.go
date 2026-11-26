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

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// Recorder is told what the run is about to do and what it did, in order, on the
// run's goroutine, at most once per call. Chat implements it over its tables and
// live overlay. An error is a write that did not land, never a judgement on the
// call.
type Recorder interface {
	// LLMCallStarted is told before each model call. An error stops the turn:
	// nothing external follows a write that did not land.
	LLMCallStarted(ctx context.Context, model string) error
	// LLMCallFirstChunk is told once per model call, at its first text or
	// thinking chunk and before the Progress that carries it. A server call is
	// neither, so a reply that streams nothing else is never told.
	LLMCallFirstChunk()
	// Progress carries the answer so far, a snapshot the recorder may keep: per
	// chunk, once a reply that asked for calls lands, and after each call is answered.
	Progress(blocks []llm.Block)
	// ServerCallSeen is told of each call the provider ran, once, as the stream
	// shows it and before the Progress that carries it, with the offered tool
	// the call's block names. It cannot fail: the provider has run the call
	// already, and the finish write lands its record.
	ServerCallSeen(call llm.Block, tool tools.Tool)
	// LLMCallFinished is told after each stream returned, with the response whole,
	// or zero on a stream error. An error stops the turn when the reply asked for
	// tools; otherwise it is returned from Run beside the turn's own outcome, which
	// it does not change.
	LLMCallFinished(ctx context.Context, resp llm.Response, streamErr error) error
	// ToolCallStarted is told before a tool runs, with the approval its gate
	// read, asked or skipped; an ungated call's is the zero value. An error means
	// it does not run.
	ToolCallStarted(ctx context.Context, call llm.Block, approval tools.Approval) error
	// ToolCallFinished is told exactly once per call of the reply, in the reply's
	// order, run or refused, with its result, and with the action a tools.Shown
	// tool resolved at its run, nil on every other call and on a refusal.
	ToolCallFinished(ctx context.Context, call, result llm.Block, shown *tools.Action) error
	// Settled is told once, last, with the turn's outcome. Its error is returned
	// from Run; the result stands.
	Settled(ctx context.Context, result Result, err error) error
}
