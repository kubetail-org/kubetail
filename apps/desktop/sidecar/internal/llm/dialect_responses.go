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

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

// responsesIdle bounds a Responses API stream that has gone quiet. The stream
// sends no keep-alive, and a model that reasons is silent from
// response.in_progress until its first output, which can be minutes.
const responsesIdle = 10 * time.Minute

// responsesWire is one request's view of the Responses API: what every helper
// in this file reads of the request.
type responsesWire struct {
	providerID string
}

// streamResponses speaks the OpenAI Responses API. Text and tool rounds in, a
// summary of the reasoning, text and calls out; OpenAI caches the prefix on its
// own. It sends one
// request and streams the reply, ending a stream quiet for longer than idle,
// as clock measures it.
// emit is called with each chunk as it arrives, on the calling goroutine. An
// error means the response is incomplete: what was streamed is what there is.
func streamResponses(ctx context.Context, req Request, idle time.Duration, clock idleClock, emit func(Chunk)) (Response, error) {
	if req.Model.MaxOutputTokens <= 0 {
		return Response{}, fmt.Errorf("responses: model %q states no output cap", req.Model.ID)
	}
	client := responses.NewResponseService(openAIOptions(req.Provider)...)

	w := responsesWire{providerID: req.Provider.ID}
	tools, err := w.tools(req.Tools)
	if err != nil {
		return Response{}, err
	}
	params := responses.ResponseNewParams{
		Model:           req.Model.ID,
		MaxOutputTokens: openai.Int(int64(req.Model.MaxOutputTokens)),
		Input:           responses.ResponseNewParamsInputUnion{OfInputItemList: w.input(req.Messages)},
		Tools:           tools,
		// No response object is kept for the call, so nothing of the transcript
		// stays retrievable at OpenAI.
		Store: openai.Bool(false),
	}
	if req.SystemPrompt != "" {
		params.Instructions = openai.String(req.SystemPrompt)
	}
	if req.Effort != "" {
		params.Reasoning = shared.ReasoningParam{
			Effort:  shared.ReasoningEffort(req.Effort),
			Summary: shared.ReasoningSummaryAuto,
		}
		// store is false, so the reasoning item the API wrote is the only copy,
		// and a tool round needs it back.
		params.Include = []responses.ResponseIncludable{responses.ResponseIncludableReasoningEncryptedContent}
	}

	guard, ctx := newIdleGuard(ctx, idle, clock)
	defer guard.stop()

	stream := client.NewStreaming(ctx, params, option.WithMiddleware(func(r *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		return guard.watchBody(next(r))
	}))
	defer stream.Close()

	// The summary index restarts per reasoning item, so a section is the pair.
	var sections sectionJoiner[responsesSection]
	// The reply is whole at its terminal event; a body left open past it is not
	// waited on.
	var final *responses.Response
	for final == nil && stream.Next() {
		guard.event()
		event := stream.Current()
		switch event.Type {
		case "response.output_text.delta", "response.refusal.delta":
			// A refusal is the answer's text: it streams like one and a cancel
			// keeps what arrived.
			emit(Chunk{Text: event.Delta})
		case "response.reasoning_summary_text.delta":
			section := responsesSection{item: event.ItemID, index: event.SummaryIndex}
			emit(Chunk{Kind: ChunkThinking, Text: sections.join(section, event.Delta)})
		case "response.completed", "response.incomplete":
			final = &event.Response
		case "response.failed":
			return Response{}, ResponseError(req.Provider, 0, "failed", openAICode(req.Provider, string(event.Response.Error.Code)))
		case "error":
			// The event's type is its discriminator, so it is the error's type
			// too; the code is classified like the API's own.
			return Response{}, ResponseError(req.Provider, 0, "error", openAICode(req.Provider, event.Code))
		}
	}
	if err := stream.Err(); err != nil {
		return Response{}, openAIFailure(ctx, req.Provider, err)
	}
	if final == nil {
		// A connection that dropped is not an answer that finished.
		return Response{}, IncompleteError(req.Provider.ID)
	}
	blocks, refused, err := w.blocks(final.Output, final.Status == "incomplete")
	if err != nil {
		return Response{}, err
	}
	return Response{
		Blocks: blocks, StopReason: w.stopReason(final, blocks, refused), Model: string(final.Model),
		Usage: responsesUsage(final),
	}, nil
}

// responsesUsage is the final response's usage. Its input already includes the
// cached part, and the API has no cache write charge.
func responsesUsage(final *responses.Response) Usage {
	if !final.JSON.Usage.Valid() {
		return Usage{}
	}
	u := final.Usage
	return Usage{
		Reported:        true,
		InputTokens:     int(u.InputTokens),
		CacheReadTokens: int(u.InputTokensDetails.CachedTokens),
		OutputTokens:    int(u.OutputTokens),
	}
}

// tools is the offer as the API takes it: one function tool per
// definition, the schema as written, strict false. Explicitly false: left to
// the API the schema is normalised toward strict, and strict wants every
// property required, which a tool with optional filters is not. A schema that
// is not JSON fails the send, naming the tool. Nil for none.
func (w responsesWire) tools(defs []ToolDefinition) ([]responses.ToolUnionParam, error) {
	var out []responses.ToolUnionParam
	for _, d := range defs {
		var schema map[string]any
		if err := json.Unmarshal(d.InputSchema, &schema); err != nil {
			return nil, fmt.Errorf("responses: encode %s schema: %w", d.Name, err)
		}
		out = append(out, responses.ToolUnionParam{OfFunction: &responses.FunctionToolParam{
			Name:        d.Name,
			Description: openai.String(d.Description),
			Parameters:  schema,
			Strict:      openai.Bool(false),
		}})
	}
	return out, nil
}

// input is the conversation as the API takes it, a walk over each
// message's blocks with another writer's payloads stripped first: a block with a
// payload is its item verbatim, which is what the API pairs a round by; a call
// without one is rebuilt from the app's fields; a result is a
// function_call_output; each run of context and text is one message of the
// row's role. A message left with no text is left out. Every call goes under
// the id replayIDs gives it, a kept item with that call_id set.
func (w responsesWire) input(msgs []Message) responses.ResponseInputParam {
	var out responses.ResponseInputParam
	var ids replayIDs
	for _, m := range msgs {
		foreign := m.foreign(w.providerID, DialectResponses)
		var text []Block
		flush := func() {
			if prompt := Prompt(text); prompt != "" {
				out = append(out, responses.ResponseInputItemParamOfMessage(prompt, responses.EasyInputMessageRole(m.Role)))
			}
			text = nil
		}
		for _, b := range stripForeign(w.providerID, DialectResponses, m) {
			switch {
			case b.Payload != nil:
				flush()
				payload := b.Payload
				if b.Type == BlockToolUse {
					if id := ids.mint(b.ID, foreign); id != b.ID {
						payload = withReplayID(payload, "call_id", id)
					}
				}
				out = append(out, param.Override[responses.ResponseInputItemUnionParam](payload))
			case b.Type == BlockThinking:
				// A summary alone is no reasoning item: the API takes the
				// ciphertext or nothing.
			case b.Type == BlockToolUse:
				flush()
				out = append(out, responses.ResponseInputItemParamOfFunctionCall(string(b.Input), ids.mint(b.ID, foreign), b.Name))
			case b.Type == BlockToolResult:
				// The item has no error flag: the loop's refusals are {"error":…}
				// text the model reads either way.
				flush()
				result := responses.ResponseInputItemParamOfFunctionCallOutput(b.Text)
				result.OfFunctionCallOutput.CallID = openai.String(ids.of(b.ID))
				out = append(out, result)
			case b.Type == BlockText, b.Type == BlockContext, b.Type == BlockTaskNotification:
				text = append(text, b)
			}
		}
		flush()
	}
	return out
}

// responsesSection is one section of a summary on this wire: the reasoning item
// and the part within it, since the part index restarts per item.
type responsesSection struct {
	item  string
	index int64
}

// blocks is the reply as the record holds it, in the order the API
// wrote it, and whether the model refused: a thinking block per reasoning item
// with the item as its payload, since the encrypted content goes back on a round;
// a tool_use per function_call, a completed one carrying its item too, since
// the item holds more than the app's fields; a text block per part, a refusal
// among them since the reader needs to see why. cutOff says the cap ended the
// reply, so a call whose arguments are not an object is stored as {} with no
// payload for the loop to answer not-run; under a reply that completed it is one
// this wire could not read, and so is an item that is not an object, which the
// record must never hold. cutOff is the response's word, never the item's,
// which a compatible endpoint can leave out. Any other item or part is kept out
// and counted in the log by position, never by content.
func (w responsesWire) blocks(output []responses.ResponseOutputItemUnion, cutOff bool) (blocks []Block, refused bool, err error) {
	for i, item := range output {
		switch item.Type {
		case "reasoning":
			var parts []string
			for _, part := range item.Summary {
				if part.Text != "" {
					parts = append(parts, part.Text)
				}
			}
			block := ThinkingBlock(strings.Join(parts, ThinkingSeparator))
			block.Payload = json.RawMessage(item.RawJSON())
			blocks = append(blocks, block)
		case "function_call":
			call := item.AsFunctionCall()
			input, ok := toolInput(json.RawMessage(call.Arguments))
			switch {
			case ok:
			case cutOff:
				input = json.RawMessage(`{}`)
			default:
				return nil, false, ReadError(w.providerID)
			}
			block := ToolUseBlock(call.CallID, call.Name, input)
			if ok && call.Status == responses.ResponseFunctionToolCallStatusCompleted {
				block.Payload = json.RawMessage(item.RawJSON())
			}
			blocks = append(blocks, block)
		case "message":
			for j, part := range item.Content {
				switch part.Type {
				case "output_text":
					blocks = append(blocks, TextBlock(part.Text))
				case "refusal":
					blocks = append(blocks, TextBlock(part.Refusal))
					refused = true
				default:
					slog.Debug("responses: a part the stream does not carry", "provider", w.providerID, "index", i, "part", j)
				}
			}
		default:
			slog.Debug("responses: an item the stream does not carry", "provider", w.providerID, "index", i)
		}
	}
	if slices.ContainsFunc(blocks, func(b Block) bool { return !b.replayable() }) {
		return nil, false, ReadError(w.providerID)
	}
	return blocks, refused, nil
}

// stopReason is the reply's own word for why it ended, with two
// translations: a completed reply holding a call is tool_use, the one word the
// loop keys on, since completed says nothing about it; and a refusal is named
// as one, since a refused answer is complete and the transcript draws it like
// any other. An incomplete reply keeps its reason, or the bare status: it was
// cut off, so a call in it is not a model that finished asking, and the loop
// answers it not-run.
func (w responsesWire) stopReason(final *responses.Response, blocks []Block, refused bool) string {
	if final.Status == "incomplete" {
		if final.IncompleteDetails.Reason != "" {
			return final.IncompleteDetails.Reason
		}
		return string(final.Status)
	}
	if slices.ContainsFunc(blocks, func(b Block) bool { return b.Type == BlockToolUse }) {
		return StopToolUse
	}
	if refused {
		return "refusal"
	}
	return string(final.Status)
}
