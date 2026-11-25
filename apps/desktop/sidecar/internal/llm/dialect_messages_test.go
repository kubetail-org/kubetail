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
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
)

const messageStart = `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5-20251001","content":[],"stop_reason":null,"usage":{"input_tokens":2,"output_tokens":1}}}`

// textStream is a recorded reply: one text block in two pieces, then its end.
var textStream = []event{
	{"message_start", messageStart},
	{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"A pod "}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"is a group."}}`},
	{"content_block_stop", `{"type":"content_block_stop","index":0}`},
	{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":9}}`},
	{"message_stop", `{"type":"message_stop"}`},
}

// messagesReported is textStream with every count the wire reads: the input is
// the uncached remainder, the two cache counts beside it.
var messagesReported = []event{
	{"message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5-20251001","content":[],"stop_reason":null,` +
		`"usage":{"input_tokens":10,"cache_creation_input_tokens":5,"cache_read_input_tokens":20,"output_tokens":1}}}`},
	textStream[1], textStream[2], textStream[3], textStream[4],
	{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`},
	textStream[6],
}

// messagesUnreported is textStream with no usage object anywhere.
var messagesUnreported = []event{
	{"message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5-20251001","content":[],"stop_reason":null}}`},
	textStream[1], textStream[2], textStream[3], textStream[4],
	{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`},
	textStream[6],
}

// thinkingStream is a recorded reply that thinks in two blocks before its text.
var thinkingStream = []event{
	{"message_start", messageStart},
	{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"I count "}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"the pods."}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"c2ln"}}`},
	{"content_block_stop", `{"type":"content_block_stop","index":0}`},
	{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"thinking","thinking":"","signature":""}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"thinking_delta","thinking":"They are twelve."}}`},
	{"content_block_stop", `{"type":"content_block_stop","index":1}`},
	{"content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"A pod "}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"is a group."}}`},
	{"content_block_stop", `{"type":"content_block_stop","index":2}`},
	textStream[5], textStream[6],
}

// thinksAgainStream is a recorded reply that thinks again after its first text:
// two thinking blocks and two text blocks, in the order the stream served them.
var thinksAgainStream = []event{
	{"message_start", messageStart},
	{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"I count "}}`},
	{"content_block_stop", `{"type":"content_block_stop","index":0}`},
	{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"A pod "}}`},
	{"content_block_stop", `{"type":"content_block_stop","index":1}`},
	{"content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"thinking","thinking":"","signature":""}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"thinking_delta","thinking":"and a node."}}`},
	{"content_block_stop", `{"type":"content_block_stop","index":2}`},
	{"content_block_start", `{"type":"content_block_start","index":3,"content_block":{"type":"text","text":""}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":3,"delta":{"type":"text_delta","text":"is a group."}}`},
	{"content_block_stop", `{"type":"content_block_stop","index":3}`},
	textStream[5], textStream[6],
}

// ended is textStream ending with the given stop reason.
func ended(reason string) []event {
	out := append([]event{}, textStream[:len(textStream)-2]...)
	return append(out,
		event{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"` + reason + `"},"usage":{"output_tokens":9}}`},
		event{"message_stop", `{"type":"message_stop"}`},
	)
}

// anthropicAt is the Anthropic provider pointed at baseURL.
func anthropicAt(baseURL string) Provider {
	return Provider{ID: "anthropic", Label: "Anthropic", Dialect: DialectMessages, BaseURL: baseURL, Key: "sk-ant-test"}
}

// request is what every test sends, before its messages.
func request(p Provider, msgs ...Message) Request {
	if msgs == nil {
		msgs = []Message{{Role: "user", Blocks: []Block{TextBlock("what is a pod?")}}}
	}
	return Request{
		Provider: p, Model: Model{ID: "claude-haiku-4-5-20251001", MaxOutputTokens: 64_000},
		SystemPrompt: "be terse", Messages: msgs,
	}
}

// serve is a server running handler, as the Anthropic provider.
func serve(t *testing.T, handler http.HandlerFunc) Provider {
	t.Helper()
	return anthropicAt(newServer(t, handler))
}

// replay serves one recorded stream and records the request it was asked with.
func replay(t *testing.T, events []event) (Provider, *recorded) {
	t.Helper()
	rec := &recorded{}
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		rec.take(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			sse(w, e)
		}
	}), rec
}

// refuse serves one refusal: status, and the API's error body.
func refuse(t *testing.T, status int, body string) Provider {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	})
}

// ask streams req and returns the chunks' text joined, the response
// and the error.
func ask(t *testing.T, req Request) (string, Response, error) {
	t.Helper()
	var text strings.Builder
	resp, err := streamMessages(t.Context(), req, testutil.Timeout, wallClock, func(c Chunk) { text.WriteString(c.Text) })
	return text.String(), resp, err
}

// The text arrives as it streams, the response holds it whole with the stop reason,
// and the request carried the model, the cap, the prompt and the messages.
func TestMessagesStreamsAReplyAndRecordsIt(t *testing.T) {
	p, rec := replay(t, textStream)

	text, resp, err := ask(t, request(p))

	require.NoError(t, err)
	assert.Equal(t, "A pod is a group.", text)
	assert.Equal(t, Response{Blocks: []Block{TextBlock("A pod is a group.")}, StopReason: "end_turn", Model: "claude-haiku-4-5-20251001",
		Usage: Usage{Reported: true, InputTokens: 2, OutputTokens: 9}}, resp)
	assert.Equal(t, "/v1/messages", rec.path)
	assert.Equal(t, "sk-ant-test", rec.header.Get("X-Api-Key"))
	assert.Regexp(t, "^"+regexp.QuoteMeta(version.UserAgent())+" Anthropic/Go [0-9.]+$", rec.header.Get("User-Agent"))
	assert.Equal(t, "claude-haiku-4-5-20251001", rec.body["model"])
	assert.Equal(t, float64(64_000), rec.body["max_tokens"])
	assert.Equal(t, []any{map[string]any{
		"type": "text", "text": "be terse", "cache_control": map[string]any{"type": "ephemeral", "ttl": "1h"},
	}}, rec.body["system"])
	assert.Equal(t, []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "what is a pod?"}}}}, rec.body["messages"])
	assert.Nil(t, rec.body["thinking"], "no thinking is asked for")
	assert.Nil(t, rec.body["tools"])
}

// A model that lists efforts asks for adaptive thinking with a summary, at the
// effort the send named: the two are one feature on this wire.
func TestMessagesSendsThinkingWithTheEffort(t *testing.T) {
	p, rec := replay(t, textStream)
	req := request(p)
	req.Effort = "xhigh"

	_, _, err := ask(t, req)

	require.NoError(t, err)
	assert.Equal(t, map[string]any{"type": "adaptive", "display": "summarized"}, rec.body["thinking"])
	assert.Equal(t, map[string]any{"effort": "xhigh"}, rec.body["output_config"])
}

// A model that lists none is sent neither field: it refuses adaptive thinking.
func TestMessagesSendsNoThinkingForAModelWithNoEfforts(t *testing.T) {
	p, rec := replay(t, textStream)

	_, _, err := ask(t, request(p))

	require.NoError(t, err)
	assert.NotContains(t, rec.body, "thinking")
	assert.NotContains(t, rec.body, "output_config")
}

// A model with no output cap is refused before anything is sent: max_tokens is
// required, and a provider that states none has nothing to put there.
func TestMessagesRefusesAModelWithNoOutputCap(t *testing.T) {
	p, rec := replay(t, textStream)
	req := request(p)
	req.Model.MaxOutputTokens = 0

	_, _, err := ask(t, req)

	assert.EqualError(t, err, `messages: model "claude-haiku-4-5-20251001" states no output cap`)
	assert.Zero(t, rec.calls.Load(), "nothing was sent")
}

// Every way the stream can end.
func TestMessagesTerminalCases(t *testing.T) {
	// The cap reached and a refusal are both complete turns, in the provider's word.
	for _, reason := range []string{"max_tokens", "refusal"} {
		t.Run(reason, func(t *testing.T) {
			p, _ := replay(t, ended(reason))
			_, resp, err := ask(t, request(p))

			require.NoError(t, err)
			assert.Equal(t, reason, resp.StopReason)
			assert.NotEmpty(t, resp.Blocks, "what was written is kept")
		})
	}

	t.Run("an error event", func(t *testing.T) {
		p, _ := replay(t, []event{
			{"message_start", messageStart},
			{"error", `{"type":"error","error":{"type":"overloaded_error","message":"the request carried sk-ant-SEKRIT"}}`},
		})
		_, _, err := ask(t, request(p))

		var e *Error
		require.ErrorAs(t, err, &e)
		assert.Equal(t, "overloaded_error", e.Type)
		assert.NotContains(t, err.Error(), "SEKRIT")
	})

	t.Run("no terminal event", func(t *testing.T) {
		p, _ := replay(t, textStream[:3])
		text, _, err := ask(t, request(p))

		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
		assert.Equal(t, "A pod ", text, "what was streamed was emitted")
	})

	// A block opened out of order is a stream the accumulator cannot follow.
	t.Run("a block out of order", func(t *testing.T) {
		p, _ := replay(t, []event{
			{"message_start", messageStart},
			{"content_block_start", `{"type":"content_block_start","index":3,"content_block":{"type":"text","text":""}}`},
		})
		_, _, err := ask(t, request(p))

		assert.Equal(t, ReadError("anthropic"), err)
	})
}

// A reply the stream cannot read leaves its bytes nowhere: not in the error's text
// and not in the log, since a body can echo the conversation.
func TestMessagesKeepsAMalformedReplyOutOfTheRecordAndTheLog(t *testing.T) {
	const marker = "MARKER-the-user-said-this"

	t.Run("an error event that is not the API's shape", func(t *testing.T) {
		buf := captureLog(t)
		p, _ := replay(t, []event{{"message_start", messageStart}, {"error", marker}})

		_, _, err := ask(t, request(p))

		assert.Equal(t, ReadError("anthropic"), err)
		assert.NotContains(t, buf.String(), marker)
	})

	// The decoder skips a line with no field, so a body that is not SSE at all can
	// end clean with no stop reason; either way the bytes go nowhere.
	t.Run("a body that is not SSE", func(t *testing.T) {
		buf := captureLog(t)
		p := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, marker)
		})

		_, _, err := ask(t, request(p))

		require.Error(t, err)
		assert.NotContains(t, err.Error(), marker)
		assert.NotContains(t, buf.String(), marker)
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			assert.Equal(t, ReadError("anthropic"), err)
		}
	})

	t.Run("an error event that is not the API's shape", func(t *testing.T) {
		buf := captureLog(t)
		p, _ := replay(t, []event{{"message_start", messageStart}, {"error", marker}})

		_, _, err := ask(t, request(p))

		assert.Equal(t, ReadError("anthropic"), err)
		assert.NotContains(t, buf.String(), marker)
	})

	// The decoder skips a line with no field, so a body that is not SSE at all can
	// end clean with no stop reason; either way the bytes go nowhere.
	t.Run("a body that is not SSE", func(t *testing.T) {
		buf := captureLog(t)
		p := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, marker)
		})

		_, _, err := ask(t, request(p))

		require.Error(t, err)
		assert.NotContains(t, err.Error(), marker)
		assert.NotContains(t, buf.String(), marker)
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			assert.Equal(t, ReadError("anthropic"), err)
		}
	})

	// The client wraps a status line it cannot parse the way it wraps a refused
	// connection, with the line's bytes in the text.
	t.Run("a status line that is not HTTP", func(t *testing.T) {
		buf := captureLog(t)

		_, _, err := ask(t, request(anthropicAt(newRawServer(t, marker))))

		assert.Equal(t, ReadError("anthropic"), err)
		assert.NotContains(t, buf.String(), marker)
	})

	// The type is the API's own vocabulary only when it is a word the API defines.
	t.Run("an error type the API does not define", func(t *testing.T) {
		p := refuse(t, 503, `{"type":"error","error":{"type":"`+marker+`","message":"x"}}`)

		_, _, err := ask(t, request(p))

		assert.EqualError(t, err, "anthropic: 503")
	})
}

// The reply is whole at message_stop: a body held open past it is not waited on,
// so it cannot run the answer into the idle bound.
func TestMessagesEndsAtMessageStop(t *testing.T) {
	p := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range textStream {
			sse(w, e)
		}
		<-r.Context().Done()
	})

	_, resp, err := ask(t, request(p))

	require.NoError(t, err)
	assert.Equal(t, "end_turn", resp.StopReason)
}

// A stream that only beats past the bound is alive; one that goes silent is ended
// by the bound and says so, not as a cancel.
func TestMessagesIdleBound(t *testing.T) {
	const bound = time.Minute

	t.Run("a stream that only beats", func(t *testing.T) {
		beats := make(chan struct{})
		p := serve(t, serveBeats(beats,
			func(w http.ResponseWriter) { sse(w, textStream[0]) },
			func(w http.ResponseWriter) { sse(w, event{"ping", `{"type":"ping"}`}) },
			func(w http.ResponseWriter) {
				sse(w, textStream[5])
				sse(w, textStream[6])
			}))
		timer := newFakeTimer()
		var text strings.Builder

		done := goStream(func() (Response, error) {
			return streamMessages(t.Context(), request(p), bound, timer.clock, func(c Chunk) { text.WriteString(c.Text) })
		})
		beat(t, timer, beats)
		got := testutil.Recv(t, done, "the stream's end")

		require.NoError(t, got.err)
		assert.Empty(t, text.String())
		assert.Equal(t, "end_turn", got.resp.StopReason)
	})

	t.Run("a stream that goes silent", func(t *testing.T) {
		p := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			sse(w, textStream[0])
			<-r.Context().Done()
		})
		timer := newFakeTimer()

		done := goStream(func() (Response, error) {
			return streamMessages(t.Context(), request(p), bound, timer.clock, func(Chunk) {})
		})
		timer.awaitReset(t)
		timer.trip()
		got := testutil.Recv(t, done, "the stream's end")

		assert.ErrorIs(t, got.err, ErrStreamIdle)
		assert.NotErrorIs(t, got.err, context.Canceled)
	})
}

// A cancelled context ends the stream as a cancel, with what was streamed already
// emitted.
func TestMessagesEndsOnCancel(t *testing.T) {
	p := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range textStream[:3] {
			sse(w, e)
		}
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(t.Context())
	var text strings.Builder

	_, err := streamMessages(ctx, request(p), testutil.Timeout, wallClock, func(c Chunk) {
		text.WriteString(c.Text)
		cancel()
	})

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, "A pod ", text.String())
}

// A question is its context block, in its tags, then its text, each a text block;
// a block with no text is left out, and a message the drop empties with it.
func TestMessagesEncodesTheConversation(t *testing.T) {
	p, rec := replay(t, textStream)

	_, _, err := ask(t, request(p,
		Message{Role: "user", Blocks: []Block{ContextBlock("card"), TextBlock("hi")}},
		Message{Role: "assistant", Blocks: []Block{TextBlock("")}},
		Message{Role: "assistant", Blocks: []Block{TextBlock("hello")}},
		Message{Role: "user", Blocks: []Block{TextBlock(""), TextBlock("and?")}},
	))

	require.NoError(t, err)
	assert.Equal(t, []any{
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "<context>\ncard\n</context>"},
			map[string]any{"type": "text", "text": "hi"},
		}},
		map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "hello"}}},
		map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "and?"}}},
	}, rec.body["messages"])
}

// The prompt is cached for an hour at two marks: the system block, and the
// request's own, which the API places after the last block. A request with no
// system prompt carries the request's mark alone.
func TestMessagesCachesThePromptForAnHour(t *testing.T) {
	hour := map[string]any{"type": "ephemeral", "ttl": "1h"}
	p, rec := replay(t, textStream)

	_, _, err := ask(t, request(p))

	require.NoError(t, err)
	assert.Equal(t, []any{map[string]any{"type": "text", "text": "be terse", "cache_control": hour}}, rec.body["system"])
	assert.Equal(t, hour, rec.body["cache_control"])

	bare, bareRec := replay(t, textStream)
	req := request(bare)
	req.SystemPrompt = ""
	_, _, err = ask(t, req)

	require.NoError(t, err)
	assert.NotContains(t, bareRec.body, "system")
	assert.Equal(t, hour, bareRec.body["cache_control"])
}

// A notice rides as a text block of its own, ahead of the question it came with.
func TestMessagesSendsANoticeAsText(t *testing.T) {
	p, rec := replay(t, textStream)

	_, _, err := ask(t, request(p, Message{Role: "user", Blocks: []Block{exitedNotice(0), TextBlock("and?")}}))

	require.NoError(t, err)
	assert.Equal(t, []any{map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "text", "text": Prompt([]Block{exitedNotice(0)})},
		map[string]any{"type": "text", "text": "and?"},
	}}}, rec.body["messages"])
}

// A redacted thinking block has nothing to show and still has to go back, so the
// record keeps it for its payload alone; a block of a kind this wire does not carry
// is kept out and counted in the log, a server call other than the search among
// them.
func TestMessagesKeepsOutABlockItDoesNotRead(t *testing.T) {
	buf := captureLog(t)
	p, _ := replay(t, []event{
		{"message_start", messageStart},
		{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"redacted_thinking","data":"c2Vrcml0"}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":0}`},
		{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"server_tool_use","id":"srv_1","name":"code_execution","input":{}}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":1}`},
		{"content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"A pod."}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":2}`},
		{"content_block_start", `{"type":"content_block_start","index":3,"content_block":{"type":"container_upload","file_id":"file_1"}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":3}`},
		textStream[5], textStream[6],
	})

	text, resp, err := ask(t, request(p))

	require.NoError(t, err)
	assert.Equal(t, "A pod.", text)
	require.Len(t, resp.Blocks, 2)
	assert.Equal(t, BlockThinking, resp.Blocks[0].Type)
	assert.Empty(t, resp.Blocks[0].Text)
	assert.JSONEq(t, `{"type":"redacted_thinking","data":"c2Vrcml0"}`, string(resp.Blocks[0].Payload))
	assert.Equal(t, TextBlock("A pod."), resp.Blocks[1])
	assert.Equal(t, 2, strings.Count(buf.String(), "a block the stream does not carry"), "the other server call and the other kind")
	assert.NotContains(t, buf.String(), "redacted", "the block's type is the reply's text")
	assert.NotContains(t, buf.String(), "c2Vrcml0")
}

// Each thinking block is a section: its deltas stream as thinking chunks, the
// second block's first delta led by the separator, and the record holds one block
// per provider item, each with its own signature in its payload, in the reply's
// order. A reader joins them and sees what the stream said.
func TestMessagesKeepsThinkingBlocksApart(t *testing.T) {
	p, _ := replay(t, thinkingStream)
	var chunks []Chunk

	resp, err := streamMessages(t.Context(), request(p), testutil.Timeout, wallClock, func(c Chunk) { chunks = append(chunks, c) })

	require.NoError(t, err)
	assert.Equal(t, []Chunk{
		{Kind: ChunkThinking, Text: "I count "},
		{Kind: ChunkThinking, Text: "the pods."},
		{Kind: ChunkThinking, Text: "\n\nThey are twelve."},
		{Text: "A pod "},
		{Text: "is a group."},
	}, chunks)
	require.Len(t, resp.Blocks, 3)
	assert.Equal(t, BlockThinking, resp.Blocks[0].Type)
	assert.JSONEq(t, `{"type":"thinking","thinking":"I count the pods.","signature":"c2ln"}`, string(resp.Blocks[0].Payload))
	assert.Equal(t, "They are twelve.", resp.Blocks[1].Text)
	assert.Equal(t, TextBlock("A pod is a group."), resp.Blocks[2])
	assert.Equal(t, "I count the pods.\n\nThey are twelve.", Thinking(resp.Blocks))
}

// A call the cap cut off inside its input is stored with an empty object: the
// wire refuses a non-object back, and the loop answers such a call not-run. It
// keeps no payload, since the payload would resend the arguments the record repaired.
func TestMessagesStoresACutOffCallWithAnEmptyInput(t *testing.T) {
	cutOff := append([]event{}, toolStream[:6]...)
	cutOff = append(cutOff,
		event{"content_block_stop", `{"type":"content_block_stop","index":1}`},
		event{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":9}}`},
		event{"message_stop", `{"type":"message_stop"}`})
	p, _ := replay(t, cutOff)

	_, resp, err := ask(t, request(p))

	require.NoError(t, err)
	assert.Equal(t, "max_tokens", resp.StopReason)
	call := resp.Blocks[len(resp.Blocks)-1]
	assert.Equal(t, `{}`, string(call.Input))
	assert.Nil(t, call.Payload)
}

// messagesEnv is every variable the pinned SDK's DefaultClientOptions reads. A bump
// re-reads it for one it did not read before, and this list moves with it.
var messagesEnv = []string{
	"ANTHROPIC_BASE_URL",
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_AUTH_TOKEN",
	"ANTHROPIC_PROFILE",
	"ANTHROPIC_CONFIG_DIR",
	"ANTHROPIC_CUSTOM_HEADERS",
	"ANTHROPIC_WEBHOOK_SIGNING_KEY",
	"ANTHROPIC_ORGANIZATION_ID",
	"ANTHROPIC_WORKSPACE_ID",
	"ANTHROPIC_FEDERATION_RULE_ID",
	"ANTHROPIC_SERVICE_ACCOUNT_ID",
	"ANTHROPIC_IDENTITY_TOKEN",
	"ANTHROPIC_IDENTITY_TOKEN_FILE",
}

// A client takes nothing from the environment: every variable the SDK reads is set
// hostile, and HOME holds a profile its autoloader would otherwise fall through to.
func TestMessagesClientTakesNothingFromTheEnvironment(t *testing.T) {
	forbidden := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the client reached the base URL from the environment")
	}))
	t.Cleanup(forbidden.Close)

	home := t.TempDir()
	profiles := filepath.Join(home, ".config", "anthropic")
	require.NoError(t, os.MkdirAll(filepath.Join(profiles, "configs"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(profiles, "credentials"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(profiles, "configs", "default.json"),
		[]byte(`{"version":"1.0","authentication":{"type":"user_oauth","client_id":"cid"},"organization_id":"org-from-disk","workspace_id":"ws-from-disk"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(profiles, "credentials", "default.json"),
		[]byte(`{"version":"1.0","type":"user_oauth","access_token":"PROFILE-TOKEN"}`), 0o600))

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	for _, name := range messagesEnv {
		t.Setenv(name, "ENV-"+name)
	}
	// The two that must name something usable to be dangerous at all.
	t.Setenv("ANTHROPIC_BASE_URL", forbidden.URL)
	t.Setenv("ANTHROPIC_CUSTOM_HEADERS", "x-smuggled: ENV-HEADER")

	p, rec := replay(t, textStream)
	_, _, err := ask(t, request(p))

	require.NoError(t, err)
	assert.Equal(t, int64(1), rec.calls.Load(), "the request went where the client was built to go")
	assert.Equal(t, "sk-ant-test", rec.header.Get("X-Api-Key"))
	for _, name := range []string{"Authorization", "X-Smuggled", "Anthropic-Workspace-Id", "Anthropic-Organization-Id"} {
		assert.Empty(t, rec.header.Get(name), name)
	}
	for _, values := range rec.header {
		for _, v := range values {
			assert.NotContains(t, v, "ENV-")
			assert.NotContains(t, v, "PROFILE-TOKEN")
		}
	}
}

// The request the model cannot read whole is one refusal the record names, said two
// ways by the API, each spelled here as the API spells it.
func TestMessagesNamesTheRequestItCannotRead(t *testing.T) {
	for _, msg := range []string{
		"prompt is too long: 213456 tokens > 200000 maximum",
		"input length and `max_tokens` exceed context limit: 190000 + 64000 > 200000",
	} {
		p := refuse(t, 400, `{"type":"error","error":{"type":"invalid_request_error","message":"`+msg+`"}}`)

		_, _, err := ask(t, request(p))

		var e *Error
		require.ErrorAs(t, err, &e, msg)
		assert.True(t, e.ContextFull(), msg)
		assert.EqualError(t, err, "anthropic: 400 invalid_request_error context_length_exceeded")
	}
}

// Every other refusal keeps the API's status and type, no code, and never the
// message.
func TestMessagesGivesOtherRefusalsNoCode(t *testing.T) {
	p := refuse(t, 429, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down, sk-ant-SEKRIT"}}`)

	_, _, err := ask(t, request(p))

	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Empty(t, e.Code)
	assert.EqualError(t, err, "anthropic: 429 rate_limit_error")
}

func TestMessagesCodeReadsNothingFromABodyItCannotParse(t *testing.T) {
	assert.Empty(t, messagesWire{}.code("not json"))
	assert.Empty(t, messagesWire{}.code(`{"error":{"message":"something else"}}`))
	assert.Equal(t, CodeContextLengthExceeded, messagesWire{}.code(`{"error":{"message":"prompt is too long"}}`))
}

// A connection the transport could not make is a transport failure: its kind in the
// text, its cause in the log, which names a host and never a body.
func TestMessagesRendersAFailureWithNoResponse(t *testing.T) {
	buf := captureLog(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	p := anthropicAt(srv.URL)

	_, _, err := ask(t, request(p))

	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, "connection failed", e.Kind)
	assert.Contains(t, buf.String(), "provider transport failure")
}

// toolStream is a recorded reply that says a word, then asks for a call whose
// input arrives in two pieces.
var toolStream = []event{
	{"message_start", messageStart},
	{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Let me check."}}`},
	{"content_block_stop", `{"type":"content_block_stop","index":0}`},
	{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_1","name":"list_objects","input":{}}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"resource\":"}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"pods\"}"}}`},
	{"content_block_stop", `{"type":"content_block_stop","index":1}`},
	{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`},
	{"message_stop", `{"type":"message_stop"}`},
}

// messagesMalformedCall is toolStream with arguments that are not an object,
// under the same normal end.
var messagesMalformedCall = append(append([]event{}, toolStream[:5]...),
	event{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"[1,\"MARKER\"]"}}`},
	toolStream[7], toolStream[8], toolStream[9])

// listObjects is the one definition the tool tests offer.
var listObjects = ToolDefinition{
	Name:        "list_objects",
	Description: "List one kind.",
	InputSchema: json.RawMessage(`{"type":"object","properties":{"resource":{"type":"string"}},"required":["resource"]}`),
}

// The definitions go on the request as the API takes them, and a streamed call is
// stored as the app's own block with the input as it accumulated — no chunk for
// the pieces, which are not for display.
func TestMessagesOffersToolsAndStoresACall(t *testing.T) {
	p, rec := replay(t, toolStream)
	req := request(p)
	req.Tools = []ToolDefinition{listObjects}

	text, resp, err := ask(t, req)

	require.NoError(t, err)
	assert.Equal(t, []any{map[string]any{
		"name": "list_objects", "description": "List one kind.",
		"input_schema": map[string]any{
			"type":       "object",
			"properties": map[string]any{"resource": map[string]any{"type": "string"}},
			"required":   []any{"resource"},
		},
	}}, rec.body["tools"])
	assert.Equal(t, "Let me check.", text, "the input's pieces are not chunks")
	assert.Equal(t, []Block{
		TextBlock("Let me check."),
		ToolUseBlock("call_1", "list_objects", json.RawMessage(`{"resource":"pods"}`)),
	}, resp.Blocks)
	assert.Equal(t, StopToolUse, resp.StopReason)
}

// A server tool this wire does not spell is refused with nothing sent, the
// backstop behind Stream's check.
func TestMessagesRefusesAServerToolItDoesNotSpell(t *testing.T) {
	p, rec := replay(t, textStream)
	req := request(p)
	req.NativeTools = []NativeOffer{{Tool: nativeTool{name: "fetch"}, Server: true, MaxUses: 2}}

	_, _, err := ask(t, req)

	assert.ErrorIs(t, err, ErrToolsUnsupported)
	assert.Zero(t, rec.calls.Load())
}

// searchStream is a recorded reply that searched: text, the call with its query
// in two pieces, the result, a text citing it and a location that is not a web
// source, then a usage reporting the one search.
var searchStream = []event{
	{"message_start", messageStart},
	{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Let me search."}}`},
	{"content_block_stop", `{"type":"content_block_stop","index":0}`},
	{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"server_tool_use","id":"srv_1","name":"web_search","input":{}}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"kubernetes"}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":" 1.36\"}"}}`},
	{"content_block_stop", `{"type":"content_block_stop","index":1}`},
	{"content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"web_search_tool_result","tool_use_id":"srv_1","content":[{"type":"web_search_result","url":"https://kubernetes.io/releases","title":"Releases","encrypted_content":"ZW5j"}]}}`},
	{"content_block_stop", `{"type":"content_block_stop","index":2}`},
	{"content_block_start", `{"type":"content_block_start","index":3,"content_block":{"type":"text","text":"","citations":[]}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":3,"delta":{"type":"citations_delta","citation":{"type":"web_search_result_location","url":"https://kubernetes.io/releases","title":"Releases","cited_text":"1.36","encrypted_index":"aWR4"}}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":3,"delta":{"type":"citations_delta","citation":{"type":"char_location","cited_text":"1.36","document_index":0,"document_title":"notes","start_char_index":0,"end_char_index":4}}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":3,"delta":{"type":"text_delta","text":"It shipped."}}`},
	{"content_block_stop", `{"type":"content_block_stop","index":3}`},
	{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":9,"server_tool_use":{"web_search_requests":1}}}`},
	{"message_stop", `{"type":"message_stop"}`},
}

// searching is request with the search offered, which is what reads a search.
func searching(p Provider) Request {
	req := request(p)
	req.NativeTools = []NativeOffer{search}
	return req
}

// A search is read into the record: the call under its id with its query and its
// item, the result as a native block under the call's id, and the citing text
// with its web sources alone and its item, since the replay needs the
// encrypted_index. The query streams once the call's input is whole.
func TestMessagesReadsASearchIntoTheRecord(t *testing.T) {
	p, _ := replay(t, searchStream)
	var chunks []Chunk

	resp, err := streamMessages(t.Context(), searching(p), testutil.Timeout, wallClock, func(c Chunk) { chunks = append(chunks, c) })

	require.NoError(t, err)
	assert.Equal(t, []Chunk{
		{Text: "Let me search."},
		{Kind: ChunkServer, Call: withoutPayload(searchUse("srv_1", "kubernetes 1.36"))},
		{Text: "It shipped."},
	}, chunks)
	require.Len(t, resp.Blocks, 4)
	assert.Equal(t, TextBlock("Let me search."), resp.Blocks[0])

	use := resp.Blocks[1]
	assert.Equal(t, withoutPayload(searchUse("srv_1", "kubernetes 1.36")), withoutPayload(use))
	assert.JSONEq(t, `{"type":"server_tool_use","id":"srv_1","name":"web_search","input":{"query":"kubernetes 1.36"}}`, string(use.Payload))

	result := resp.Blocks[2]
	assert.Equal(t, Block{Type: BlockNative, ID: "srv_1"}, withoutPayload(result))
	assert.Contains(t, string(result.Payload), `"encrypted_content":"ZW5j"`)

	cited := resp.Blocks[3]
	assert.Equal(t, TextBlock("It shipped."), withoutPayload(cited))
	assert.Contains(t, string(cited.Payload), `"encrypted_index":"aWR4"`)
	assert.Equal(t, []Citation{
		releases,
		{Type: "char_location", Title: "notes", CitedText: "1.36"},
	}, Citations(DialectMessages, resp.Blocks), "every citation, read back from the payload")

	assert.Equal(t, map[string]int{searchName: 1}, resp.ServerUses)
}

// A text block's citations are read from its payload whatever their kind: a web
// source's url and title, a document's title, and every kind's cited text. A
// block with no payload cites nothing, and a payload that does not decode gives
// nothing rather than failing the message.
func TestMessagesReadsEveryCitation(t *testing.T) {
	document := withPayload(TextBlock("Per the notes."),
		`{"type":"text","text":"Per the notes.","citations":[{"type":"page_location","cited_text":"p. 4","document_index":0,"document_title":"Runbook","start_page_number":4,"end_page_number":5}]}`)
	broken := withPayload(TextBlock("Odd."), `{"type":"text","citations":`)

	got := Citations(DialectMessages, []Block{TextBlock("Plain."), citedText("It shipped."), document, broken, thought("hmm")})

	assert.Equal(t, []Citation{releases, {Type: "page_location", Title: "Runbook", CitedText: "p. 4"}}, got)
}

// askSearchRow sends row as an answer the given provider wrote, with a
// follow-up after it unless last, and returns the wire's shape and the
// answer's content as sent.
func askSearchRow(t *testing.T, providerID string, row []Block, last bool) ([]string, []any) {
	t.Helper()
	p, rec := replay(t, textStream)
	msgs := []Message{
		{Role: "user", Blocks: []Block{TextBlock("what shipped?")}},
		{Role: "assistant", Blocks: row, ProviderID: providerID, Dialect: DialectMessages},
	}
	if !last {
		msgs = append(msgs, Message{Role: "user", Blocks: []Block{TextBlock("and before?")}})
	}
	req := request(p, msgs...)
	req.NativeTools = []NativeOffer{search}

	_, _, err := ask(t, req)

	require.NoError(t, err)
	content := rec.body["messages"].([]any)[1].(map[string]any)["content"].([]any)
	return wireShape(t, rec.body), content
}

// A search goes back as the items the API wrote, the citing text with its
// citations, so the encrypted_index the API checks is there.
func TestMessagesReplaysASearchVerbatim(t *testing.T) {
	shape, content := askSearchRow(t, "anthropic", searchRow(), false)

	assert.Equal(t, []string{
		"user: text",
		"assistant: text server_tool_use(srv_1) web_search_tool_result(srv_1) text",
		"user: text",
	}, shape)
	require.Len(t, content, 4)
	for i, b := range searchRow()[1:] {
		raw, err := json.Marshal(content[i+1])
		require.NoError(t, err)
		assert.JSONEq(t, string(b.Payload), string(raw))
	}
}

// Another provider's search is its query alone, which is not a block the API
// takes: the call and its result stay out, and the cited text goes as text.
func TestAForeignSearchIsDropped(t *testing.T) {
	shape, content := askSearchRow(t, "vertex-claude", searchRow(), false)

	assert.Equal(t, []string{"user: text", "assistant: text text", "user: text"}, shape)
	assert.Equal(t, map[string]any{"type": "text", "text": "It shipped."}, content[1])
}

// unansweredRow is searchRow ending on a second search the reply never got a
// result for: a pause past the cap, or a max_tokens cut mid-search.
func unansweredRow() []Block {
	return append(searchRow(), searchUse("srv_2", "kubernetes 1.35"))
}

// A search with no result in its row is refused by the API, and that row is
// replayed on every later turn, so it stays out of the request; the rest of the
// row goes.
func TestAnUnansweredSearchIsDropped(t *testing.T) {
	shape, _ := askSearchRow(t, "anthropic", unansweredRow(), false)

	assert.Equal(t, []string{
		"user: text",
		"assistant: text server_tool_use(srv_1) web_search_tool_result(srv_1) text",
		"user: text",
	}, shape)
}

// The request's last message can only be the turn's own rounds, and a search
// with no result at its end is a paused reply being resumed, which the provider
// continues from that call: it goes whole.
func TestAPausedSearchIsResumedWhole(t *testing.T) {
	shape, _ := askSearchRow(t, "anthropic", unansweredRow(), true)

	assert.Equal(t, []string{
		"user: text",
		"assistant: text server_tool_use(srv_1) web_search_tool_result(srv_1) text server_tool_use(srv_2)",
	}, shape)
}

// Every search the reply holds is a chunk, one with nothing to draw included:
// what to draw is the reader's to decide. A call with no input, and one the cap
// cut off inside it, are recorded with {} and still replay as the provider's
// items.
func TestMessagesSendsAChunkForEveryCall(t *testing.T) {
	start := event{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"srv_1","name":"web_search","input":{}}}`}
	stop := event{"content_block_stop", `{"type":"content_block_stop","index":0}`}
	cases := map[string][]event{
		"no input": {{"message_start", messageStart}, start, stop, textStream[5], textStream[6]},
		"a query the cap cut off": {
			{"message_start", messageStart}, start,
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"kube"}}`},
			stop,
			{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":9}}`},
			{"message_stop", `{"type":"message_stop"}`},
		},
	}
	for name, events := range cases {
		t.Run(name, func(t *testing.T) {
			p, _ := replay(t, events)
			var chunks []Chunk

			resp, err := streamMessages(t.Context(), searching(p), testutil.Timeout, wallClock, func(c Chunk) { chunks = append(chunks, c) })

			require.NoError(t, err)
			call := ServerUseBlock("srv_1", searchName, json.RawMessage(`{}`))
			assert.Equal(t, []Chunk{{Kind: ChunkServer, Call: call}}, chunks)
			require.Len(t, resp.Blocks, 1)
			assert.Equal(t, call, withoutPayload(resp.Blocks[0]))
			assert.NotNil(t, resp.Blocks[0].Payload)
		})
	}
}

// The count is read off the last event whose usage reports it, a zero included;
// a reply whose usage never reports one has none.
func TestMessagesTellsAZeroSearchCountFromNone(t *testing.T) {
	withUsage := func(usage string) []event {
		return []event{
			{"message_start", messageStart},
			textStream[1], textStream[2], textStream[3], textStream[4],
			{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":` + usage + `}`},
			{"message_stop", `{"type":"message_stop"}`},
		}
	}
	cases := map[string]struct {
		events []event
		want   map[string]int
	}{
		"a reported zero": {withUsage(`{"output_tokens":9,"server_tool_use":{"web_search_requests":0}}`), map[string]int{searchName: 0}},
		"no report":       {textStream, nil},
		"a report at the start alone": {append([]event{
			{"message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"usage":{"input_tokens":2,"output_tokens":1,"server_tool_use":{"web_search_requests":2}}}}`},
		}, textStream[1:]...), map[string]int{searchName: 2}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			p, _ := replay(t, c.events)

			_, resp, err := ask(t, searching(p))

			require.NoError(t, err)
			assert.Equal(t, c.want, resp.ServerUses)
		})
	}
}

// A server call is read under the tool the request offered, so one the
// request did not offer is kept out like any block the stream does not carry,
// its result and its count with it.
func TestMessagesReadsOnlyTheServerToolsItOffers(t *testing.T) {
	p, _ := replay(t, searchStream)
	var chunks []Chunk

	resp, err := streamMessages(t.Context(), request(p), testutil.Timeout, wallClock, func(c Chunk) { chunks = append(chunks, c) })

	require.NoError(t, err)
	assert.Equal(t, []Chunk{{Text: "Let me search."}, {Text: "It shipped."}}, chunks)
	require.Len(t, resp.Blocks, 2)
	assert.Equal(t, TextBlock("Let me search."), resp.Blocks[0])
	assert.Equal(t, TextBlock("It shipped."), withoutPayload(resp.Blocks[1]))
	assert.Nil(t, resp.ServerUses)
}

// fetchStream is a reply that fetched a page: the call, its result, and a usage
// reporting the one fetch.
var fetchStream = []event{
	{"message_start", messageStart},
	{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"srv_1","name":"web_fetch","input":{}}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"url\":\"https://kubernetes.io\"}"}}`},
	{"content_block_stop", `{"type":"content_block_stop","index":0}`},
	{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"web_fetch_tool_result","tool_use_id":"srv_1","content":{"type":"web_fetch_tool_result_error","error_code":"url_not_accessible"}}}`},
	{"content_block_stop", `{"type":"content_block_stop","index":1}`},
	{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":9,"server_tool_use":{"web_search_requests":0,"web_fetch_requests":1}}}`},
	{"message_stop", `{"type":"message_stop"}`},
}

// A server tool reaches the wire through its own binding: offered in its shape,
// its call and result read under its name beside another tool's, and each count
// kept under its own name.
func TestMessagesSpeaksAServerToolThroughItsBinding(t *testing.T) {
	p, rec := replay(t, fetchStream)
	req := request(p)
	req.NativeTools = []NativeOffer{search, {Tool: messagesFetch, Server: true, MaxUses: 2}}
	var chunks []Chunk

	resp, err := streamMessages(t.Context(), req, testutil.Timeout, wallClock, func(c Chunk) { chunks = append(chunks, c) })

	require.NoError(t, err)
	tools := rec.body["tools"].([]any)
	require.Len(t, tools, 2)
	assert.Equal(t, "web_fetch_20260318", tools[1].(map[string]any)["type"])
	assert.Equal(t, float64(2), tools[1].(map[string]any)["max_uses"])

	call := ServerUseBlock("srv_1", messagesFetch.name, json.RawMessage(`{"url":"https://kubernetes.io"}`))
	assert.Equal(t, []Chunk{{Kind: ChunkServer, Call: call}}, chunks)
	require.Len(t, resp.Blocks, 2)
	assert.Equal(t, call, withoutPayload(resp.Blocks[0]))
	assert.Equal(t, Block{Type: BlockNative, ID: "srv_1"}, withoutPayload(resp.Blocks[1]))
	assert.Equal(t, map[string]int{searchName: 0, messagesFetch.name: 1}, resp.ServerUses)
}

// A request handed no definitions sends none.
func TestMessagesSendsNoToolsWhenHandedNone(t *testing.T) {
	p, rec := replay(t, textStream)

	_, _, err := ask(t, request(p))

	require.NoError(t, err)
	assert.NotContains(t, rec.body, "tools")
}

// The schema goes whole, every top-level key as written, so a $ref finds its
// target.
func TestMessagesSendsTheWholeSchema(t *testing.T) {
	p, rec := replay(t, toolStream)
	schema := `{"type":"object","additionalProperties":false,` +
		`"properties":{"kind":{"$ref":"#/$defs/kind"}},"required":["kind"],` +
		`"$defs":{"kind":{"type":"string"}}}`
	req := request(p)
	req.Tools = []ToolDefinition{{Name: "list_objects", InputSchema: json.RawMessage(schema)}}

	_, _, err := ask(t, req)

	require.NoError(t, err)
	sent, err := json.Marshal(rec.body["tools"].([]any)[0].(map[string]any)["input_schema"])
	require.NoError(t, err)
	assert.JSONEq(t, schema, string(sent))
}

// A schema that is not a JSON object fails the send, naming the tool; nothing
// reaches the server.
func TestMessagesRefusesAToolItCannotEncode(t *testing.T) {
	p, rec := replay(t, toolStream)
	req := request(p)
	req.Tools = []ToolDefinition{{Name: "broken", InputSchema: json.RawMessage(`[1]`)}}

	_, _, err := ask(t, req)

	assert.ErrorContains(t, err, "broken")
	assert.Zero(t, rec.calls.Load())
}

// wireShape is what a rendering looks like on the wire: each message's role and
// the type and, for a tool block, the id of every block in it.
func wireShape(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, err := json.Marshal(body["messages"])
	require.NoError(t, err)
	var decoded []struct {
		Role    string `json:"role"`
		Content []struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			ToolUseID string `json:"tool_use_id"`
			IsError   bool   `json:"is_error"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(raw, &decoded))
	var out []string
	for _, m := range decoded {
		line := m.Role + ":"
		for _, b := range m.Content {
			line += " " + b.Type
			if id := b.ID + b.ToolUseID; id != "" {
				line += "(" + id + ")"
			}
			if b.IsError {
				line += "!"
			}
		}
		out = append(out, line)
	}
	return out
}

// round is an assistant row's call and its answer.
func round(id, result string) []Block {
	return []Block{
		ToolUseBlock(id, "list_objects", json.RawMessage(`{"resource":"pods"}`)),
		ToolResultBlock(id, result, false),
	}
}

// thought is a thinking block as this provider wrote it, payload and all.
func thought(text string) Block {
	b := ThinkingBlock(text)
	b.Payload = json.RawMessage(`{"type":"thinking","thinking":"` + text + `","signature":"c2ln"}`)
	return b
}

// An assistant row holding rounds unfolds into the wire's alternation: each run
// of results is a user message, and the blocks between two runs are an assistant
// message.
func TestMessagesUnfoldsToolRoundsIntoTheWiresAlternation(t *testing.T) {
	p, rec := replay(t, textStream)
	row := []Block{TextBlock("Let me check.")}
	row = append(row, round("toolu_1", "2")...)
	row = append(row, round("toolu_2", "3")...)
	row = append(row, TextBlock("Five."))
	req := request(p,
		Message{Role: "user", Blocks: []Block{TextBlock("how many?")}},
		Message{Role: "assistant", Blocks: row, ProviderID: "anthropic", Dialect: DialectMessages},
	)
	req.Tools = []ToolDefinition{listObjects}

	_, _, err := ask(t, req)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"user: text",
		"assistant: text tool_use(toolu_1)",
		"user: tool_result(toolu_1)",
		"assistant: tool_use(toolu_2)",
		"user: tool_result(toolu_2)",
		"assistant: text",
	}, wireShape(t, rec.body))
}

// A run of calls the model asked in one breath stays one message, and so do the
// results that answer them.
func TestMessagesKeepsARunOfCallsInOneMessage(t *testing.T) {
	p, rec := replay(t, textStream)
	row := []Block{
		ToolUseBlock("toolu_1", "list_objects", json.RawMessage(`{"resource":"pods"}`)),
		ToolUseBlock("toolu_2", "list_objects", json.RawMessage(`{"resource":"nodes"}`)),
		ToolResultBlock("toolu_1", "2", false),
		ToolResultBlock("toolu_2", "3", false),
	}
	req := request(p, Message{Role: "assistant", Blocks: row, ProviderID: "anthropic", Dialect: DialectMessages})
	req.Tools = []ToolDefinition{listObjects}

	_, _, err := ask(t, req)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"assistant: tool_use(toolu_1) tool_use(toolu_2)",
		"user: tool_result(toolu_1) tool_result(toolu_2)",
	}, wireShape(t, rec.body))
}

// A thinking block goes back as the item the API signed, and only to the provider
// that signed it: another provider is sent the text alone, which is no thinking
// block at all.
func TestMessagesReplaysItsOwnThinkingVerbatim(t *testing.T) {
	row := []Block{thought("I count them."), TextBlock("Twelve.")}

	t.Run("its writer", func(t *testing.T) {
		p, rec := replay(t, textStream)
		req := request(p, Message{Role: "assistant", Blocks: row, ProviderID: "anthropic", Dialect: DialectMessages, Effort: "high"})
		req.Effort = "high"

		_, _, err := ask(t, req)

		require.NoError(t, err)
		assert.Equal(t, []string{"assistant: thinking text"}, wireShape(t, rec.body))
		content := rec.body["messages"].([]any)[0].(map[string]any)["content"].([]any)
		assert.Equal(t, map[string]any{"type": "thinking", "thinking": "I count them.", "signature": "c2ln"}, content[0])
	})

	t.Run("another provider", func(t *testing.T) {
		p, rec := replay(t, textStream)
		req := request(p, Message{Role: "assistant", Blocks: row, ProviderID: "vertex-claude", Dialect: DialectMessages, Effort: "high"})
		req.Effort = "high"

		_, _, err := ask(t, req)

		require.NoError(t, err)
		assert.Equal(t, []string{"assistant: text"}, wireShape(t, rec.body))
	})
	t.Run("its writer on another dialect", func(t *testing.T) {
		p, rec := replay(t, textStream)
		req := request(p, Message{Role: "assistant", Blocks: row, ProviderID: "anthropic", Dialect: DialectChatCompletions, Effort: "high"})
		req.Effort = "high"

		_, _, err := ask(t, req)

		require.NoError(t, err)
		assert.Equal(t, []string{"assistant: text"}, wireShape(t, rec.body))
	})
}

// roundRow is an answer row of this provider's shape: a thought, a word, a round
// and the answer that followed it.
func roundRow() []Block {
	row := []Block{thought("hmm"), TextBlock("Let me check.")}
	row = append(row, round("toolu_1", "2")...)
	return append(row, TextBlock("There are 2."))
}

// askRow sends question, one assistant row and a follow-up, at the effort given.
func askRow(t *testing.T, effort string, row Message) []string {
	t.Helper()
	p, rec := replay(t, textStream)
	req := request(p,
		Message{Role: "user", Blocks: []Block{TextBlock("how many pods?")}},
		row,
		Message{Role: "user", Blocks: []Block{TextBlock("and nodes?")}},
	)
	req.Effort = effort
	req.Tools = []ToolDefinition{listObjects}

	_, _, err := ask(t, req)

	require.NoError(t, err)
	return wireShape(t, rec.body)
}

// The row holding the lastmost round loses its tool blocks when it is another
// provider's: its thinking was stripped with its payloads, and the API refuses a
// final assistant message whose lastmost tool_use has no thinking ahead of it.
// Its text stays.
func TestMessagesDropsTheLastForeignRowsToolBlocks(t *testing.T) {
	assert.Equal(t, []string{"user: text", "assistant: text text", "user: text"},
		askRow(t, "high", Message{Role: "assistant", Blocks: roundRow(), ProviderID: "vertex-claude", Dialect: DialectMessages, Effort: "high"}))

	assert.Equal(t, []string{
		"user: text", "assistant: thinking text tool_use(toolu_1)",
		"user: tool_result(toolu_1)", "assistant: text", "user: text",
	}, askRow(t, "high", Message{Role: "assistant", Blocks: roundRow(), ProviderID: "anthropic", Dialect: DialectMessages, Effort: "high"}))

	p, rec := replay(t, textStream)
	req := request(p,
		Message{Role: "assistant", Blocks: roundRow(), ProviderID: "vertex-claude", Dialect: DialectMessages, Effort: "high"},
		Message{Role: "user", Blocks: []Block{TextBlock("and nodes?")}},
		Message{Role: "assistant", Blocks: []Block{TextBlock("Three.")}, ProviderID: "anthropic", Dialect: DialectMessages, Effort: "high"},
	)
	req.Effort = "high"
	req.Tools = []ToolDefinition{listObjects}
	_, _, err := ask(t, req)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"assistant: text text", "user: text", "assistant: text",
	}, wireShape(t, rec.body), "a text-only answer after the round does not shadow the row that holds it")
}

// The switch is the message's, not a block's: a foreign row with no payloaded block
// at all — a row of another wire — still loses its tool blocks.
func TestMessagesDropsTheLastForeignRoundWithoutAPayload(t *testing.T) {
	row := append([]Block{TextBlock("Let me check.")}, round("call_1", "2")...)

	assert.Equal(t, []string{"user: text", "assistant: text", "user: text"},
		askRow(t, "high", Message{Role: "assistant", Blocks: row, ProviderID: "groq", Dialect: DialectMessages, Effort: "high"}))
}

// A row this provider wrote with thinking off never had a thinking block either,
// so the last one loses its tool blocks under a request that thinks — and keeps
// them under one that does not.
func TestMessagesDropsTheLastRowWrittenWithoutThinking(t *testing.T) {
	row := append([]Block{TextBlock("Let me check.")}, round("toolu_1", "2")...)
	written := Message{Role: "assistant", Blocks: row, ProviderID: "anthropic", Dialect: DialectMessages}

	assert.Equal(t, []string{"user: text", "assistant: text", "user: text"},
		askRow(t, "high", written))

	assert.Equal(t, []string{
		"user: text", "assistant: text tool_use(toolu_1)",
		"user: tool_result(toolu_1)", "user: text",
	}, askRow(t, "", written), "a request that does not think refuses no round")

	thinking := written
	thinking.Effort = "high"
	assert.Equal(t, []string{
		"user: text", "assistant: text tool_use(toolu_1)",
		"user: tool_result(toolu_1)", "user: text",
	}, askRow(t, "high", thinking), "a model that chose not to think before a call is not a row written without it")
}

// A request that does not think sends no thinking block, with a payload or without: the API
// takes them only with thinking on.
func TestMessagesSendsNoThinkingWhenTheRequestDoesNotThink(t *testing.T) {
	row := Message{Role: "assistant", Blocks: []Block{thought("I count them."), TextBlock("Twelve.")}, ProviderID: "anthropic", Dialect: DialectMessages, Effort: "high"}

	assert.Equal(t, []string{"user: text", "assistant: text", "user: text"}, askRow(t, "", row))
}

// A schema whose required is not a list of names is a tool this wire cannot
// encode, named like any other.
func TestMessagesRefusesASchemaWithABadRequired(t *testing.T) {
	p, rec := replay(t, toolStream)
	req := request(p)
	req.Tools = []ToolDefinition{{Name: "broken", InputSchema: json.RawMessage(`{"type":"object","required":"resource"}`)}}

	_, _, err := ask(t, req)

	assert.ErrorContains(t, err, "broken")
	assert.Zero(t, rec.calls.Load())
}

// A cut-off call whose arguments read as something other than an object is
// stored as {} like any other: the wire refuses them back.
func TestMessagesStoresACutOffCallThatReadsAsAList(t *testing.T) {
	cutOff := append([]event{}, toolStream[:5]...)
	cutOff = append(cutOff,
		event{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"[1]"}}`},
		event{"content_block_stop", `{"type":"content_block_stop","index":1}`},
		event{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":9}}`},
		event{"message_stop", `{"type":"message_stop"}`})
	p, _ := replay(t, cutOff)

	_, resp, err := ask(t, request(p))

	require.NoError(t, err)
	assert.Equal(t, `{}`, string(resp.Blocks[len(resp.Blocks)-1].Input))
}

// The SDK's accumulator replaces input it cannot parse with an empty object, so
// the deltas are the only copy of what the model sent: a reply that ended asking
// with arguments that never parsed is one this wire could not read, not a call
// to run on its defaults.
func TestMessagesFailsACallWhoseDeltasNeverParsed(t *testing.T) {
	broken := append([]event{}, toolStream[:6]...)
	broken = append(broken, toolStream[7], toolStream[8], toolStream[9])
	p, _ := replay(t, broken)

	_, resp, err := ask(t, request(p))

	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, "unreadable reply", e.Kind)
	assert.Empty(t, resp.Blocks)
}

// A call the model made with no arguments carries no arguments of its own, and
// {} is what it asked with: the tool runs on its defaults. An empty delta is no
// arguments either — the API sends one beside a block that has none.
func TestMessagesKeepsACallSentWithNoArguments(t *testing.T) {
	p, _ := replay(t, []event{
		toolStream[0],
		{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"list_objects","input":{}}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":""}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":0}`},
		toolStream[8], toolStream[9],
	})

	_, resp, err := ask(t, request(p))

	require.NoError(t, err)
	require.Len(t, resp.Blocks, 1)
	assert.Equal(t, `{}`, string(resp.Blocks[0].Input))
	assert.Equal(t, StopToolUse, resp.StopReason)
}

// The rule protects the lastmost round, which is not always the last assistant
// row's: a text-only answer after it must not shadow the row that holds it.
func TestMessagesRepairsTheRowHoldingTheLastRound(t *testing.T) {
	p, rec := replay(t, textStream)
	round := append([]Block{TextBlock("Let me check.")}, round("toolu_1", "2")...)
	req := request(p,
		Message{Role: "user", Blocks: []Block{TextBlock("how many pods?")}},
		Message{Role: "assistant", Blocks: round, ProviderID: "vertex-claude", Dialect: DialectMessages, Effort: "high"},
		Message{Role: "user", Blocks: []Block{TextBlock("and nodes?")}},
		Message{Role: "assistant", Blocks: []Block{TextBlock("Three.")}, ProviderID: "vertex-claude", Dialect: DialectMessages, Effort: "high"},
	)
	req.Effort = "high"
	req.Tools = []ToolDefinition{listObjects}

	_, _, err := ask(t, req)

	require.NoError(t, err)
	assert.Equal(t, []string{"user: text", "assistant: text", "user: text", "assistant: text"},
		wireShape(t, rec.body), "the round goes with the row that holds it, whichever row that is")
}

// A tool that returned nothing is a result with no content: the API refuses an
// empty text block, and there is nothing of the tool's to put in one.
func TestMessagesSendsAnEmptyResultWithNoContent(t *testing.T) {
	p, rec := replay(t, textStream)
	row := []Block{
		ToolUseBlock("toolu_1", "list_objects", json.RawMessage(`{"resource":"pods"}`)),
		ToolResultBlock("toolu_1", "", false),
	}
	req := request(p, Message{Role: "assistant", Blocks: row, ProviderID: "anthropic", Dialect: DialectMessages})
	req.Tools = []ToolDefinition{listObjects}

	_, _, err := ask(t, req)

	require.NoError(t, err)
	result := rec.body["messages"].([]any)[1].(map[string]any)["content"].([]any)[0].(map[string]any)
	assert.Equal(t, "tool_result", result["type"])
	assert.NotContains(t, result, "content", "no content at all, never an empty text block")
}

// messagesTool is a server tool spelled on the Messages wire by the test's own
// codec, since llm's tests cannot import a tool package.
type messagesTool struct {
	name       string
	callName   string
	resultType string
	offer      func(maxUses int) anthropic.ToolUnionParam
	uses       func(anthropic.ServerToolUsage) int64
}

func (m messagesTool) Name() string                                       { return m.name }
func (m messagesTool) MessagesCallName() string                           { return m.callName }
func (m messagesTool) MessagesResultType() string                         { return m.resultType }
func (m messagesTool) MessagesOffer(maxUses int) anthropic.ToolUnionParam { return m.offer(maxUses) }
func (m messagesTool) MessagesUses(u anthropic.ServerToolUsage) int       { return int(m.uses(u)) }

// messagesSearch spells the search as the real tool does.
var messagesSearch = &messagesTool{
	name:     searchName,
	callName: "web_search", resultType: "web_search_tool_result",
	offer: func(maxUses int) anthropic.ToolUnionParam {
		return anthropic.ToolUnionParam{OfWebSearchTool20260318: &anthropic.WebSearchTool20260318Param{
			MaxUses: anthropic.Int(int64(maxUses)), AllowedCallers: []string{"direct"},
		}}
	},
	uses: func(u anthropic.ServerToolUsage) int64 { return u.WebSearchRequests },
}

// messagesFetch is a server tool this wire's code names nowhere.
var messagesFetch = &messagesTool{
	name:     "anthropic_web_fetch_20260318",
	callName: "web_fetch", resultType: "web_fetch_tool_result",
	offer: func(maxUses int) anthropic.ToolUnionParam {
		return anthropic.ToolUnionParam{OfWebFetchTool20260318: &anthropic.WebFetchTool20260318Param{MaxUses: anthropic.Int(int64(maxUses))}}
	},
	uses: func(u anthropic.ServerToolUsage) int64 { return u.WebFetchRequests },
}

// A server tool goes after the functions, in its own binding's shape, which the
// tool's package pins.
func TestMessagesOffersTheSearch(t *testing.T) {
	p, rec := replay(t, textStream)
	req := request(p)
	req.Tools = []ToolDefinition{listObjects}
	req.NativeTools = []NativeOffer{search}

	_, _, err := ask(t, req)

	require.NoError(t, err)
	tools := rec.body["tools"].([]any)
	require.Len(t, tools, 2)
	assert.Equal(t, "list_objects", tools[0].(map[string]any)["name"])
	assert.Equal(t, "web_search_20260318", tools[1].(map[string]any)["type"])
}

// messagesContent is message i's content blocks in a recorded body.
func messagesContent(body map[string]any, i int) []any {
	return body["messages"].([]any)[i].(map[string]any)["content"].([]any)
}

// A foreign call's id can hold what the API refuses in a tool_use id (OpenRouter
// routes to vendors spelling one functions.echo:0): it goes under a minted id,
// and its result answers the same one.
func TestMessagesMintsAForeignCallsID(t *testing.T) {
	p, rec := replay(t, textStream)
	row := Message{Role: "assistant", ProviderID: "openrouter", Dialect: DialectMessages, Blocks: []Block{
		ToolUseBlock("functions.echo:0", "echo", json.RawMessage(`{"text":"hi"}`)),
		ToolResultBlock("functions.echo:0", "hi", false),
		TextBlock("It said hi."),
	}}

	_, _, err := ask(t, request(p, Message{Role: "user", Blocks: []Block{TextBlock("echo hi")}}, row,
		Message{Role: "user", Blocks: []Block{TextBlock("again?")}}))

	require.NoError(t, err)
	assert.Equal(t, "call00000", messagesContent(rec.body, 1)[0].(map[string]any)["id"])
	assert.Equal(t, "call00000", messagesContent(rec.body, 2)[0].(map[string]any)["tool_use_id"])
}

// Two foreign rows can hold one id twice (the Chat Completions reader numbers
// call-<n> per reply): each goes under an id of its own, and each result answers
// its own call.
func TestMessagesMintsARepeatedForeignID(t *testing.T) {
	p, rec := replay(t, textStream)
	row := Message{Role: "assistant", ProviderID: "groq", Dialect: DialectMessages, Blocks: []Block{
		ToolUseBlock("call-1", "echo", json.RawMessage(`{"text":"hi"}`)),
		ToolResultBlock("call-1", "hi", false),
	}}
	question := Message{Role: "user", Blocks: []Block{TextBlock("echo hi")}}

	_, _, err := ask(t, request(p, question, row, question, row, question))

	require.NoError(t, err)
	assert.Equal(t, "call00000", messagesContent(rec.body, 1)[0].(map[string]any)["id"])
	assert.Equal(t, "call00000", messagesContent(rec.body, 2)[0].(map[string]any)["tool_use_id"])
	assert.Equal(t, "call00001", messagesContent(rec.body, 4)[0].(map[string]any)["id"])
	assert.Equal(t, "call00001", messagesContent(rec.body, 5)[0].(map[string]any)["tool_use_id"])
}

// A reply block whose item is not a JSON object is unreadable: the record never
// holds an item the API would refuse on replay.
func TestMessagesRefusesABlockWhoseItemIsNotAnObject(t *testing.T) {
	blocks, err := messagesWire{provider: Provider{ID: "anthropic"}}.answer([]anthropic.ContentBlockUnion{{Type: "thinking"}}, messagesToolInputs{}, false)

	assert.Nil(t, blocks)
	assert.Equal(t, ReadError("anthropic"), err)
}
