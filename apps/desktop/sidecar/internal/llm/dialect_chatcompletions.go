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
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
)

// chatCompletionsIdle bounds a Chat Completions stream that has gone quiet. This
// is every local runtime's wire as well as these vendors', so the bound covers a
// cold model that loads its weights and evaluates the whole prompt before it
// sends anything, response headers included.
const chatCompletionsIdle = 5 * time.Minute

// chatWire is one request's view of the Chat Completions API: what every helper
// in this file reads of the request.
type chatWire struct {
	providerID string
}

// streamChatCompletions speaks the OpenAI Chat Completions API. Text and tool
// rounds in, text, calls and whatever the vendor shows of its thinking out, with
// the cache mark and affinity key the model and row call for. It sends one
// request and streams the reply, ending a stream quiet for longer than idle,
// as clock measures it. emit is called with each chunk as it arrives, on the
// calling goroutine. An error means the response is incomplete: what was
// streamed is what there is.
func streamChatCompletions(ctx context.Context, req Request, idle time.Duration, clock idleClock, emit func(Chunk)) (Response, error) {
	cache := cacheOptions(req.Provider, req.Model, req.AffinityKey, option.WithHeader, option.WithJSONSet)
	client := openai.NewChatCompletionService(openAIOptions(req.Provider, cache...)...)

	w := chatWire{providerID: req.Provider.ID}
	tools, err := w.tools(req.Tools)
	if err != nil {
		return Response{}, err
	}
	params := openai.ChatCompletionNewParams{
		Model:    req.Model.ID,
		Messages: w.conversation(req.SystemPrompt, req.Messages),
		Tools:    tools,
		// A stream carries no usage unless asked.
		StreamOptions: openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)},
	}
	if req.Effort != "" {
		// The API has no field to ask for a summary with: what a vendor shows of its
		// thinking is its own choice.
		params.ReasoningEffort = shared.ReasoningEffort(req.Effort)
	}
	if req.Model.MaxOutputTokens > 0 {
		// max_tokens, not max_completion_tokens: every vendor here accepts the
		// older name, and the local runtimes to come read only it.
		params.MaxTokens = openai.Int(int64(req.Model.MaxOutputTokens))
	}

	guard, ctx := newIdleGuard(ctx, idle, clock)
	defer guard.stop()

	stream := client.NewStreaming(ctx, params, option.WithMiddleware(func(r *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		return guard.watchBody(next(r))
	}))
	defer stream.Close()

	var thinking, text strings.Builder
	var finish, model string
	var refused bool
	var calls chatCalls
	var usage Usage
	for stream.Next() {
		guard.event()
		// Usage rides a last chunk with no choices, and on some vendors other
		// chunks too; the last report stands.
		if u := chatUsage(stream.Current()); u.Reported {
			usage = u
		}
		// Every chunk names the model; the first that does stands.
		if model == "" {
			model = stream.Current().Model
		}
		choices := stream.Current().Choices
		if len(choices) == 0 {
			continue
		}
		choice := choices[0]
		// A refusal is the answer's text, as on the Responses wire, so a
		// refusal-only stream is an answer and not an empty one.
		delta := choice.Delta.Content + choice.Delta.Refusal
		if delta != "" {
			text.WriteString(delta)
			emit(Chunk{Text: delta})
		}
		if think := w.thinking(choice.Delta); think != "" {
			thinking.WriteString(think)
			emit(Chunk{Kind: ChunkThinking, Text: think})
		}
		calls.read(choice.Delta.ToolCalls)
		refused = refused || choice.Delta.Refusal != ""
		if choice.FinishReason != "" {
			finish = choice.FinishReason
		}
	}
	if err := stream.Err(); err != nil {
		return Response{}, openAIFailure(ctx, req.Provider, err)
	}
	if finish == "" {
		// [DONE] and a body that ends alike end the stream clean, so only a finish
		// reason says the answer was whole.
		return Response{}, IncompleteError(req.Provider.ID)
	}
	// tool_calls is the wire's own word for a reply that finished asking, and a
	// few vendors say stop with calls present. Every other finish (length,
	// content_filter, a word of the vendor's own) stands, and the loop answers a
	// call under it not-run.
	asked := finish == "tool_calls" || finish == "stop"
	blocks, err := calls.blocks(w.providerID, !asked)
	if err != nil {
		return Response{}, err
	}
	switch {
	case len(blocks) > 0 && asked:
		finish = StopToolUse
	case refused && finish == "stop":
		// A refusal the cap cut off is still cut off: only a whole reply is renamed.
		finish = "refusal"
	}
	return Response{Blocks: append(AnswerBlocks(thinking.String(), text.String()), blocks...), StopReason: finish, Model: model, Usage: usage}, nil
}

// chatUsage is a chunk's usage: the prompt already includes the cached part. A
// hit is cached_tokens, or DeepSeek's prompt_cache_hit_tokens where that is
// absent; a write is OpenRouter's cache_write_tokens, and zero where a vendor
// names none.
func chatUsage(c openai.ChatCompletionChunk) Usage {
	if !c.JSON.Usage.Valid() {
		return Usage{}
	}
	u := c.Usage
	read := int(u.PromptTokensDetails.CachedTokens)
	if !u.PromptTokensDetails.JSON.CachedTokens.Valid() {
		read = chatCount(u.JSON.ExtraFields["prompt_cache_hit_tokens"].Raw())
	}
	return Usage{
		Reported:         true,
		InputTokens:      int(u.PromptTokens),
		CacheReadTokens:  read,
		CacheWriteTokens: int(u.PromptTokensDetails.CacheWriteTokens),
		OutputTokens:     int(u.CompletionTokens),
	}
}

// chatCount is a count a vendor sent in a field the SDK does not declare, read
// off its raw JSON: 0 for a field that is absent or not a number.
func chatCount(raw string) int {
	var n int
	if json.Unmarshal([]byte(raw), &n) != nil {
		return 0
	}
	return n
}

// tools is the offer as the API takes it: one function tool per
// definition, the schema as written, strict unset, which is false where a
// vendor knows the field and absent where it does not. A schema that is not
// JSON fails the send, naming the tool. Nil for none.
func (w chatWire) tools(defs []ToolDefinition) ([]openai.ChatCompletionToolUnionParam, error) {
	var out []openai.ChatCompletionToolUnionParam
	for _, d := range defs {
		var schema map[string]any
		if err := json.Unmarshal(d.InputSchema, &schema); err != nil {
			return nil, fmt.Errorf("chatcompletions: encode %s schema: %w", d.Name, err)
		}
		out = append(out, openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        d.Name,
			Description: openai.String(d.Description),
			Parameters:  schema,
		}))
	}
	return out, nil
}

// chatThinkingSpellings is what the vendors call the field they show their thinking
// in: reasoning_content on DeepSeek and xAI, reasoning on OpenRouter, Ollama and
// Groq. The SDK's delta declares neither, so they are read off its extra fields.
var chatThinkingSpellings = []string{"reasoning_content", "reasoning"}

// thinking is the piece of thinking a delta carries, under the first
// spelling that holds one, so a gateway spelling it both ways sends it once. An
// extra field is raw JSON, so it is decoded: null, which DeepSeek sends beside
// every text delta, reads as nothing, and so does any shape but a string.
func (w chatWire) thinking(delta openai.ChatCompletionChunkChoiceDelta) string {
	for _, name := range chatThinkingSpellings {
		field, ok := delta.JSON.ExtraFields[name]
		if !ok {
			continue
		}
		var s string
		if json.Unmarshal([]byte(field.Raw()), &s) == nil && s != "" {
			return s
		}
	}
	return ""
}

// conversation is the messages as the API takes them: the system
// prompt first, since this wire has no field of its own for it; a user row as
// one message; an assistant row in the wire's alternation, each run of text and
// the calls after it one assistant message and each result a tool message. A
// thinking block is not sent and does not split a run. Every call goes under the
// id replayIDs gives it. A message with nothing to say is left out.
func (w chatWire) conversation(systemPrompt string, msgs []Message) []openai.ChatCompletionMessageParamUnion {
	var out []openai.ChatCompletionMessageParamUnion
	if systemPrompt != "" {
		out = append(out, openai.SystemMessage(systemPrompt))
	}
	var ids replayIDs
	for _, m := range msgs {
		var text []Block
		var calls []openai.ChatCompletionMessageToolCallUnionParam
		flush := func() {
			prompt := Prompt(text)
			text = nil
			switch {
			case prompt == "" && len(calls) == 0:
			case m.Role != "assistant":
				out = append(out, openai.UserMessage(prompt))
			default:
				assistant := openai.ChatCompletionAssistantMessageParam{ToolCalls: calls}
				if prompt != "" {
					// The API takes calls with no content beside them.
					assistant.Content.OfString = openai.String(prompt)
				}
				calls = nil
				out = append(out, openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant})
			}
		}
		foreign := m.foreign(w.providerID, DialectChatCompletions)
		for _, b := range stripForeign(w.providerID, DialectChatCompletions, m) {
			switch b.Type {
			case BlockToolUse:
				calls = append(calls, w.toolCall(b, ids.mint(b.ID, foreign)))
			case BlockToolResult:
				// IsError has no field here: the loop's refusals are {"error":…}
				// text the model reads either way.
				flush()
				out = append(out, openai.ToolMessage(b.Text, ids.of(b.ID)))
			case BlockText, BlockContext, BlockTaskNotification:
				text = append(text, b)
			}
		}
		flush()
	}
	return out
}

// toolCall is one call as the wire's entry: the payload the vendor sent, when
// the call kept one, under id, and the app's fields under id otherwise.
func (w chatWire) toolCall(b Block, id string) openai.ChatCompletionMessageToolCallUnionParam {
	if b.Payload != nil {
		payload := b.Payload
		if id != b.ID {
			payload = withReplayID(payload, "id", id)
		}
		return param.Override[openai.ChatCompletionMessageToolCallUnionParam](payload)
	}
	return openai.ChatCompletionMessageToolCallUnionParam{
		OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
			ID:       id,
			Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{Name: b.Name, Arguments: string(b.Input)},
		},
	}
}

// chatCalls folds tool_calls fragments into calls, keyed by index, in the
// order they were opened. Every fragment of a call counts: each appends its
// name and its arguments, and an id or a key of the vendor's own on any of them
// is kept, since a vendor may stream the name in pieces and send the id, or a
// signature, after the fragment that opened the call.
type chatCalls struct {
	calls []*chatCall
}

// chatCall is one call as it folds: the app's fields, and the keys its
// fragments carried beyond id, type and function, where a vendor hangs its own
// data. index is the fragments' key, not the call's, and is not among them; a
// lone call may come with none, which reads as 0.
type chatCall struct {
	index     int64
	id        string
	name      strings.Builder
	arguments strings.Builder
	extras    map[string]json.RawMessage
}

// at is the call a fragment belongs to: the one open at its index, or a new one
// when that index has none — or when the fragment names an id the open call does
// not carry, since a vendor that sends parallel calls whole may leave index off
// them all, which reads as 0 for every one.
func (r *chatCalls) at(f openai.ChatCompletionChunkChoiceDeltaToolCall) *chatCall {
	// Newest first: a fragment carrying arguments alone belongs to the call last
	// opened at its index.
	for _, c := range slices.Backward(r.calls) {
		if c.index != f.Index {
			continue
		}
		if f.ID == "" || c.id == "" || c.id == f.ID {
			return c
		}
		break
	}
	c := &chatCall{index: f.Index, extras: map[string]json.RawMessage{}}
	r.calls = append(r.calls, c)
	return c
}

// read folds one delta's fragments in.
func (r *chatCalls) read(fragments []openai.ChatCompletionChunkChoiceDeltaToolCall) {
	for _, f := range fragments {
		call := r.at(f)
		if f.ID != "" {
			call.id = f.ID
		}
		call.name.WriteString(f.Function.Name)
		call.arguments.WriteString(f.Function.Arguments)
		for key, field := range f.JSON.ExtraFields {
			call.extras[key] = json.RawMessage(field.Raw())
		}
	}
}

// payload is the call as the wire's own entry, the app's fields with the vendor's
// keys beside them and the arguments as they folded, for a call the vendor put
// a key of its own on; nil for a plain call.
func (c *chatCall) payload(id string) json.RawMessage {
	if len(c.extras) == 0 {
		return nil
	}
	entry := map[string]any{
		"id":       id,
		"type":     "function",
		"function": map[string]string{"name": c.name.String(), "arguments": c.arguments.String()},
	}
	for key, value := range c.extras {
		entry[key] = value
	}
	// Strings and JSON the SDK parsed: the marshal cannot fail.
	raw, _ := json.Marshal(entry)
	return raw
}

// blocks is one tool_use per call, in opening order, with the folded entry as
// its payload when the vendor put anything of its own on it. A call sent with no
// id gets call-<n>: the record needs one to pair the result, and a server that
// sent none takes any back. cutOff says the reply did not finish asking, so a
// call whose arguments are not an object is stored as {} with no payload, since
// the payload would resend what the record repaired; under a reply that finished
// it is one this wire could not read.
func (r *chatCalls) blocks(providerID string, cutOff bool) ([]Block, error) {
	var out []Block
	for n, call := range r.calls {
		id := call.id
		if id == "" {
			id = fmt.Sprintf("call-%d", n+1)
		}
		input, ok := toolInput(json.RawMessage(call.arguments.String()))
		switch {
		case ok:
		case cutOff:
			input = json.RawMessage(`{}`)
		default:
			return nil, ReadError(providerID)
		}
		block := ToolUseBlock(id, call.name.String(), input)
		if ok {
			block.Payload = call.payload(id)
		}
		out = append(out, block)
	}
	return out, nil
}
