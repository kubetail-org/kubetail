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
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/anthropics/anthropic-sdk-go/shared"
)

// messagesIdle bounds a Messages API stream that has gone quiet. The wire's
// bound, not the turn's: the API pings every few seconds, so a minute of silence
// is a dead socket.
const messagesIdle = time.Minute

// cacheHour is how long a cached prefix lives. A chat is often picked up again
// within the hour, and a long think would spend the 5-minute default.
var cacheHour = anthropic.CacheControlEphemeralParam{TTL: anthropic.CacheControlEphemeralTTLTTL1h}

// messagesWire is one request's view of the Messages API: what every helper
// in this file reads of the request.
type messagesWire struct {
	provider Provider
	// thinks says the request asks the model to think, which decides what a
	// thinking block does on this wire.
	thinks bool
	// serverTools is the server tools the request offers. A server call is read
	// under the offered tool whose call name it carries.
	serverTools []messagesOffer
}

// messagesOffer is one server tool offered on this wire, at its cap.
type messagesOffer struct {
	tool    MessagesServerTool
	maxUses int
}

// MessagesServerTool is a server tool the Messages wire can spell. Its package
// implements it; the wire names no tool itself.
type MessagesServerTool interface {
	NativeTool
	MessagesOffer(maxUses int) anthropic.ToolUnionParam
	MessagesCallName() string   // the server_tool_use name its calls arrive under
	MessagesResultType() string // the block type its results arrive as
	MessagesUses(anthropic.ServerToolUsage) int
}

// streamMessages speaks the Anthropic Messages API. Text and tool rounds in,
// thinking, text and calls out, the prompt cached for an hour. It sends one
// request and streams the reply, ending a stream quiet for longer than idle,
// as clock measures it. The client is built from the provider alone, never the
// environment, with retries off: a retry repeats a paid call, and after an
// ambiguous failure can run a turn twice. emit is called with each chunk as it
// arrives, on the calling goroutine. An error means the response is incomplete:
// what was streamed is what there is.
func streamMessages(ctx context.Context, req Request, idle time.Duration, clock idleClock, emit func(Chunk)) (Response, error) {
	if req.Model.MaxOutputTokens <= 0 {
		return Response{}, fmt.Errorf("messages: model %q states no output cap", req.Model.ID)
	}
	opts := []option.RequestOption{option.WithoutEnvironmentDefaults(), option.WithMaxRetries(0)}
	if req.Provider.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(req.Provider.BaseURL))
	}
	opts = append(opts, option.WithHTTPClient(NoRedirectClient), option.WithMiddleware(func(r *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		return send(req.Provider, r, next)
	}))
	opts = append(opts, providerOptions(req.Provider, nil, option.WithQueryAdd, option.WithJSONSet, option.WithJSONDel)...)
	client := anthropic.NewMessageService(opts...)

	w := messagesWire{provider: req.Provider, thinks: req.Effort != ""}
	for _, o := range req.NativeTools {
		tool, ok := o.Tool.(MessagesServerTool)
		if !o.Server || !ok {
			return Response{}, ErrToolsUnsupported
		}
		w.serverTools = append(w.serverTools, messagesOffer{tool: tool, maxUses: o.MaxUses})
	}
	tools, err := w.tools(req.Tools)
	if err != nil {
		return Response{}, err
	}
	params := anthropic.MessageNewParams{
		Model:     req.Model.ID,
		MaxTokens: int64(req.Model.MaxOutputTokens),
		Messages:  w.conversation(req.Messages),
		Tools:     tools,
		// The API places this mark after the last block and moves it forward each
		// turn, so a turn reads the transcript before it and writes only what the
		// last one added.
		CacheControl: cacheHour,
	}
	if req.SystemPrompt != "" {
		// A fixed read point every chat shares.
		params.System = []anthropic.TextBlockParam{{Text: req.SystemPrompt, CacheControl: cacheHour}}
	}
	if req.Effort != "" {
		// Adaptive thinking and the effort are one feature here: a model that lists
		// no efforts refuses adaptive, and is sent neither.
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{
			Display: anthropic.ThinkingConfigAdaptiveDisplaySummarized,
		}}
		params.OutputConfig = anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffort(req.Effort)}
	}

	guard, ctx := newIdleGuard(ctx, idle, clock)
	defer guard.stop()

	stream := client.NewStreaming(ctx, params, option.WithMiddleware(func(r *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		return guard.watchBody(next(r))
	}))
	defer stream.Close()

	var acc anthropic.Message
	// Each thinking block is a section, so the second block's first delta arrives
	// led by the separator, as the record will read.
	var sections sectionJoiner[int64]
	inputs := messagesToolInputs{}
	var serverUses map[string]int
	for stream.Next() {
		// Every event, not every chunk: a model can send deltas that carry no text.
		guard.event()
		event := stream.Current()
		if err := acc.Accumulate(event); err != nil {
			return Response{}, ReadError(req.Provider.ID)
		}
		inputs.read(event)
		if uses, ok := w.serverUses(event); ok {
			serverUses = uses
		}
		if c, ok := w.delta(event, acc.Content, inputs); ok {
			if c.Kind == ChunkThinking {
				c.Text = sections.join(event.Index, c.Text)
			}
			emit(c)
		}
		// The reply is whole at its last event; a body left open past it is not
		// waited on.
		if event.Type == "message_stop" {
			break
		}
	}
	if err := stream.Err(); err != nil {
		return Response{}, w.failure(ctx, err)
	}
	if acc.StopReason == "" {
		// A connection that dropped is not an answer that finished.
		return Response{}, IncompleteError(req.Provider.ID)
	}
	blocks, err := w.answer(acc.Content, inputs, acc.StopReason == anthropic.StopReasonMaxTokens)
	if err != nil {
		return Response{}, err
	}
	return Response{
		Blocks: blocks, StopReason: string(acc.StopReason), Model: string(acc.Model), ServerUses: serverUses,
		Usage: messagesUsage(acc),
	}, nil
}

// messagesUsage is the accumulated message's usage. input_tokens is the uncached
// remainder, so the input sums it with both cache counts. The accumulator keeps
// message_start's presence flag for the object and overwrites the counts from
// message_delta.
func messagesUsage(acc anthropic.Message) Usage {
	if !acc.JSON.Usage.Valid() {
		return Usage{}
	}
	u := acc.Usage
	return Usage{
		Reported:         true,
		InputTokens:      int(u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens),
		CacheReadTokens:  int(u.CacheReadInputTokens),
		CacheWriteTokens: int(u.CacheCreationInputTokens),
		OutputTokens:     int(u.OutputTokens),
	}
}

// serverUses is the count an event's usage reports for each offered server
// tool, by its name, and false when it reports none or the request offers none. Each is
// the message's running total, so the caller keeps the last. Read off the events, never the accumulated message: the SDK copies
// server_tool_use into the message but not the flag saying it was reported, so a
// reported zero and no report would read the same.
func (w messagesWire) serverUses(event anthropic.MessageStreamEventUnion) (map[string]int, bool) {
	if len(w.serverTools) == 0 {
		return nil, false
	}
	var usage anthropic.ServerToolUsage
	switch {
	case event.Type == "message_start" && event.Message.Usage.JSON.ServerToolUse.Valid():
		usage = event.Message.Usage.ServerToolUse
	case event.Type == "message_delta" && event.Usage.JSON.ServerToolUse.Valid():
		usage = event.Usage.ServerToolUse
	default:
		return nil, false
	}
	uses := map[string]int{}
	for _, s := range w.serverTools {
		uses[s.tool.Name()] += s.tool.MessagesUses(usage)
	}
	return uses, true
}

// tools is the offer as the API takes it, each schema whole: type,
// properties and required in the param's own fields, every other top-level key
// (additionalProperties, $defs, …) through ExtraFields, so a $ref still finds
// its target. type is never an extra, since an extra overrides the field on
// marshal. A schema that is not a JSON object fails the send, naming the tool.
// Each offered server tool follows the definitions in its own binding's shape.
// Nil for none, so the body carries no tools key.
func (w messagesWire) tools(defs []ToolDefinition) ([]anthropic.ToolUnionParam, error) {
	var out []anthropic.ToolUnionParam
	for _, d := range defs {
		var schema map[string]json.RawMessage
		if err := json.Unmarshal(d.InputSchema, &schema); err != nil {
			return nil, fmt.Errorf("messages: encode %s schema: %w", d.Name, err)
		}
		input := anthropic.ToolInputSchemaParam{ExtraFields: map[string]any{}}
		for key, value := range schema {
			switch key {
			case "type":
			case "properties":
				input.Properties = value
			case "required":
				if err := json.Unmarshal(value, &input.Required); err != nil {
					return nil, fmt.Errorf("messages: encode %s schema: %w", d.Name, err)
				}
			default:
				input.ExtraFields[key] = value
			}
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &anthropic.ToolParam{
			Name:        d.Name,
			Description: anthropic.String(d.Description),
			InputSchema: input,
		}})
	}
	for _, s := range w.serverTools {
		out = append(out, s.tool.MessagesOffer(s.maxUses))
	}
	return out, nil
}

// conversation is the messages as the API takes them: a user row is one user
// message of its context and text, and an assistant row unfolds into the wire's
// alternation, every call under the id replayIDs gives it. A message left with
// no content is left out, since the API refuses an empty one.
func (w messagesWire) conversation(msgs []Message) []anthropic.MessageParam {
	// The rule below protects the lastmost round, so the row to repair is the one
	// holding it — a text-only answer after it does not shadow it.
	last := -1
	for i, m := range msgs {
		if m.Role == "assistant" && slices.ContainsFunc(m.Blocks, Block.isToolBlock) {
			last = i
		}
	}
	var out []anthropic.MessageParam
	var ids replayIDs
	for i, m := range msgs {
		blocks := stripForeign(w.provider.ID, DialectMessages, m)
		if m.Role != "assistant" {
			if content := w.content(blocks); len(content) > 0 {
				out = append(out, anthropic.NewUserMessage(content...))
			}
			continue
		}
		if i == last && w.thinks && (m.foreign(w.provider.ID, DialectMessages) || m.Effort == "") {
			// With thinking on the API refuses a final assistant message whose
			// lastmost tool_use has no thinking block ahead of it, and neither a foreign
			// row, its payloads stripped, nor one written with thinking off
			// can supply one. Its text still goes. The facts are the message's,
			// never read off its blocks: a model that chose not to think before a
			// call is not a row written with thinking off.
			blocks = WithoutRounds(blocks)
		}
		if i < len(msgs)-1 {
			// The last message can only be the turn's own rounds, and a server
			// call with no result at its end is a paused reply the provider
			// resumes from that call.
			blocks = w.answeredServerCalls(blocks)
		}
		out = append(out, w.unfold(ids.replay(blocks, m.foreign(w.provider.ID, DialectMessages)))...)
	}
	return out
}

// answeredServerCalls is blocks less every server call no native block among
// them answers. The API refuses a server call sent without its result, and a
// reply can settle Complete on one (a pause past the cap, a max_tokens cut
// mid-search); the record keeps it, and only the request leaves it out. A copy:
// the caller's slice is not written to.
func (w messagesWire) answeredServerCalls(blocks []Block) []Block {
	answered := map[string]bool{}
	for _, b := range blocks {
		if b.Type == BlockNative && b.ID != "" {
			answered[b.ID] = true
		}
	}
	return slices.DeleteFunc(slices.Clone(blocks), func(b Block) bool {
		return b.Type == BlockServerUse && !answered[b.ID]
	})
}

// unfold is one assistant row as the wire's messages: each run of
// tool_result blocks is a user message, and the blocks between two runs (text,
// thinking, the calls a run answers) an assistant message. A row with no tool
// blocks is one assistant message; an empty one is none.
func (w messagesWire) unfold(blocks []Block) []anthropic.MessageParam {
	var out []anthropic.MessageParam
	for len(blocks) > 0 {
		results := blocks[0].Type == BlockToolResult
		n := 1
		for n < len(blocks) && (blocks[n].Type == BlockToolResult) == results {
			n++
		}
		content := w.content(blocks[:n])
		blocks = blocks[n:]
		switch {
		case len(content) == 0:
		case results:
			out = append(out, anthropic.NewUserMessage(content...))
		default:
			out = append(out, anthropic.NewAssistantMessage(content...))
		}
	}
	return out
}

// content is a run of blocks as the API's content. A thinking block goes
// back only as the item this provider signed, and only when the request thinks:
// a summary alone is not a block the API takes, and a request with thinking off
// takes none. Any other block with a payload goes back as its item, ahead of the
// plain-text arm, so a citation keeps its encrypted_index; a server call or a
// result with no item, another provider's, is not a block the API takes and is
// left out.
func (w messagesWire) content(blocks []Block) []anthropic.ContentBlockParamUnion {
	var content []anthropic.ContentBlockParamUnion
	for _, b := range blocks {
		switch {
		case b.Type == BlockThinking:
			if w.thinks && b.Payload != nil {
				content = append(content, param.Override[anthropic.ContentBlockParamUnion](b.Payload))
			}
		case b.Payload != nil:
			content = append(content, param.Override[anthropic.ContentBlockParamUnion](b.Payload))
		case b.readsAsText():
			content = append(content, anthropic.NewTextBlock(b.wireText()))
		case b.Type == BlockToolUse:
			content = append(content, anthropic.NewToolUseBlock(b.ID, b.Input, b.Name))
		case b.Type == BlockToolResult:
			content = append(content, w.toolResult(b))
		}
	}
	return content
}

// toolResult is the answer to a call as the API takes it. A tool that returned
// nothing is a result with no content at all: the API refuses an empty text
// block, and there is nothing of the tool's to put in one.
func (w messagesWire) toolResult(b Block) anthropic.ContentBlockParamUnion {
	if b.Text == "" {
		return anthropic.ContentBlockParamUnion{OfToolResult: &anthropic.ToolResultBlockParam{
			ToolUseID: b.ID,
			IsError:   anthropic.Bool(b.IsError),
		}}
	}
	return anthropic.NewToolResultBlock(b.ID, b.Text, b.IsError)
}

// messagesToolInputs is each call's arguments as the deltas carried them, by
// block index. The SDK's accumulator replaces input it cannot parse with {}, and {} is
// a legitimate input a tool runs on its defaults, so the deltas are the only copy
// that tells the two apart.
type messagesToolInputs map[int64]*strings.Builder

// read keeps an event's piece of a call's arguments. An empty piece is no
// arguments: the API sends one beside a block that has none, and opening an
// accumulator for it would lose the block's own {}.
func (t messagesToolInputs) read(event anthropic.MessageStreamEventUnion) {
	e, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent)
	if !ok {
		return
	}
	d, ok := e.Delta.AsAny().(anthropic.InputJSONDelta)
	if !ok || d.PartialJSON == "" {
		return
	}
	if t[event.Index] == nil {
		t[event.Index] = &strings.Builder{}
	}
	t[event.Index].WriteString(d.PartialJSON)
}

// raw is what the model sent for the call at index, or the block's own input
// when no delta carried any: a call made with no arguments at all.
func (t messagesToolInputs) raw(index int64, block json.RawMessage) json.RawMessage {
	if sent := t[index]; sent != nil {
		return json.RawMessage(sent.String())
	}
	return block
}

// delta is the chunk an event carries, if any: a piece of text or of thinking
// from a delta, or a server call from the stop of its block, the first moment
// its input is whole. content is the reply so far, which the event has been
// folded into.
func (w messagesWire) delta(event anthropic.MessageStreamEventUnion, content []anthropic.ContentBlockUnion, inputs messagesToolInputs) (Chunk, bool) {
	switch e := event.AsAny().(type) {
	case anthropic.ContentBlockDeltaEvent:
		switch d := e.Delta.AsAny().(type) {
		case anthropic.TextDelta:
			return Chunk{Text: d.Text}, d.Text != ""
		case anthropic.ThinkingDelta:
			return Chunk{Kind: ChunkThinking, Text: d.Thinking}, d.Thinking != ""
		}
	case anthropic.ContentBlockStopEvent:
		b := content[e.Index]
		if call, ok := w.serverCall(b, inputs.raw(e.Index, b.Input)); ok {
			return Chunk{Kind: ChunkServer, Call: call}, true
		}
	}
	return Chunk{}, false
}

// serverCall is b as the record keeps a server call, with no payload, under the
// name of the offered tool whose call name it carries: its input the object the
// deltas carried, or {} for one the cap cut off, which still replays as the API
// sent it. False for a block no offered tool claims.
func (w messagesWire) serverCall(b anthropic.ContentBlockUnion, raw json.RawMessage) (Block, bool) {
	if b.Type != "server_tool_use" {
		return Block{}, false
	}
	for _, s := range w.serverTools {
		if s.tool.MessagesCallName() != b.Name {
			continue
		}
		input, ok := toolInput(raw)
		if !ok {
			input = json.RawMessage(`{}`)
		}
		return ServerUseBlock(b.ID, s.tool.Name(), input), true
	}
	return Block{}, false
}

// isServerResult reports whether a block of type t answers a call of an
// offered server tool.
func (w messagesWire) isServerResult(t string) bool {
	return slices.ContainsFunc(w.serverTools, func(s messagesOffer) bool {
		return s.tool.MessagesResultType() == t
	})
}

// messagesCitations is the citations blocks make, read from each text block's
// item: a web source's url and title, a document's title, and every kind's
// cited text.
func messagesCitations(blocks []Block) []Citation {
	var out []Citation
	for _, b := range blocks {
		if b.Type != BlockText || b.Payload == nil {
			continue
		}
		var text anthropic.TextBlock
		if json.Unmarshal(b.Payload, &text) != nil {
			continue
		}
		for _, c := range text.Citations {
			title := c.Title
			if title == "" {
				title = c.DocumentTitle
			}
			out = append(out, Citation{Type: c.Type, URL: c.URL, Title: title, CitedText: c.CitedText})
		}
	}
	return out
}

// answer is the reply as the record holds it, in the reply's order: each
// thinking block with its item as the payload, since the signature goes back on a
// round; the text, a cited one with its web sources and its item, since the API
// refuses a replay that lost a citation's encrypted_index; each call as the app's
// block; each call of an offered server tool as a server_use block and its
// result as a native one, both with their items, the result under its call's id. cutOff says the cap ended the
// reply, so a call whose input is not an object is stored as {} for the loop to
// answer not-run; under a reply that ended normally it is one this wire could
// not read, and so is an item that is not an object, which the record must
// never hold. Any other kind is kept out and counted in the log by index; its type
// is the reply's own text and stays out with the rest.
func (w messagesWire) answer(content []anthropic.ContentBlockUnion, inputs messagesToolInputs, cutOff bool) ([]Block, error) {
	var out []Block
	for i, c := range content {
		switch c.Type {
		case "thinking", "redacted_thinking":
			block := ThinkingBlock(c.Thinking)
			block.Payload = json.RawMessage(c.RawJSON())
			out = append(out, block)
		case "text":
			block := TextBlock(c.Text)
			if len(c.Citations) > 0 {
				block.Payload = json.RawMessage(c.RawJSON())
			}
			out = append(out, block)
		case "tool_use":
			input, ok := toolInput(inputs.raw(int64(i), c.Input))
			switch {
			case ok:
			case cutOff:
				input = json.RawMessage(`{}`)
			default:
				return nil, ReadError(w.provider.ID)
			}
			out = append(out, ToolUseBlock(c.ID, c.Name, input))
		default:
			if call, ok := w.serverCall(c, inputs.raw(int64(i), c.Input)); ok {
				call.Payload = json.RawMessage(c.RawJSON())
				out = append(out, call)
			} else if w.isServerResult(c.Type) {
				out = append(out, Block{Type: BlockNative, ID: c.ToolUseID, Payload: json.RawMessage(c.RawJSON())})
			} else {
				slog.Debug("messages: a block the stream does not carry", "provider", w.provider.ID, "index", i)
			}
		}
	}
	if slices.ContainsFunc(out, func(b Block) bool { return !b.replayable() }) {
		return nil, ReadError(w.provider.ID)
	}
	return out, nil
}

// failure is how a stream that did not finish is recorded. A stream that ended on
// its context reports the context's cause: the guard's trip or the caller's cancel.
// An error the API described keeps its status, its type when it is one the API
// defines, and never its message. A failure with no response at all names a host
// and no body, so it may be logged. Everything else is the reply's own bytes,
// which can echo the conversation, and is kept nowhere.
func (w messagesWire) failure(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	// The middleware's own: nothing was sent, or a redirect was refused.
	if own, ok := errors.AsType[*Error](err); ok {
		return own
	}
	if api, ok := errors.AsType[*anthropic.Error](err); ok {
		// An error event that was not the API's shape has no type: unreadable.
		if api.Type() == "" {
			return ReadError(w.provider.ID)
		}
		return ResponseError(w.provider, api.StatusCode, w.errorType(api.Type()), w.code(api.RawJSON()))
	}
	if noResponse(err) {
		return TransportError(w.provider.ID, err)
	}
	return ReadError(w.provider.ID)
}

// messagesErrorTypes is every type the API defines. Any other spelling is the
// reply's own text and is not kept.
var messagesErrorTypes = map[shared.ErrorType]bool{
	shared.ErrorTypeInvalidRequestError: true,
	shared.ErrorTypeAuthenticationError: true,
	shared.ErrorTypePermissionError:     true,
	shared.ErrorTypeNotFoundError:       true,
	shared.ErrorTypeRateLimitError:      true,
	shared.ErrorTypeTimeoutError:        true,
	shared.ErrorTypeOverloadedError:     true,
	shared.ErrorTypeAPIError:            true,
	shared.ErrorTypeBillingError:        true,
}

func (w messagesWire) errorType(t shared.ErrorType) string {
	if messagesErrorTypes[t] {
		return string(t)
	}
	return ""
}

// code gives one refusal a code: the request the model cannot read, which the API
// says two ways. The message is read for this alone and dropped; every other
// message is "".
func (w messagesWire) code(raw string) string {
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(raw), &body) != nil {
		return ""
	}
	msg := body.Error.Message
	if strings.HasPrefix(msg, "prompt is too long") || strings.HasPrefix(msg, "input length and `max_tokens` exceed context limit") {
		return CodeContextLengthExceeded
	}
	return ""
}
