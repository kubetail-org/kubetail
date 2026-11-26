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
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
)

// responsesStream is a recorded reply: one text block in two pieces, then its end.
var responsesStream = []event{
	{"response.created", `{"type":"response.created","sequence_number":0,"response":{"id":"resp_1","status":"in_progress","output":[]}}`},
	{"response.output_text.delta", `{"type":"response.output_text.delta","sequence_number":1,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"A pod "}`},
	{"response.output_text.delta", `{"type":"response.output_text.delta","sequence_number":2,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"is a group."}`},
	{"response.completed", responsesDone(`"completed"`, `{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"A pod is a group.","annotations":[]}]}`)},
}

// responsesReported is responsesStream ending on a response that reports its
// usage: the input includes the cached part.
var responsesReported = []event{
	responsesStream[0], responsesStream[1], responsesStream[2],
	{"response.completed", `{"type":"response.completed","sequence_number":9,"response":{"id":"resp_1","status":"completed","output":[` +
		`{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"A pod is a group.","annotations":[]}]}],` +
		`"usage":{"input_tokens":35,"input_tokens_details":{"cached_tokens":20},"output_tokens":7,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":42}}}`},
}

// responsesThinkingStream is a recorded reply that summarizes its reasoning: two
// parts on the first item, one on the second, then the text.
var responsesThinkingStream = []event{
	responsesStream[0],
	summaryDelta("rs_1", 0, "I count "),
	summaryDelta("rs_1", 0, "the pods."),
	summaryDelta("rs_1", 1, "Then the nodes."),
	summaryDelta("rs_2", 0, "They are twelve."),
	{"response.output_text.delta", `{"type":"response.output_text.delta","sequence_number":5,"item_id":"msg_1","output_index":2,"content_index":0,"delta":"A pod "}`},
	{"response.output_text.delta", `{"type":"response.output_text.delta","sequence_number":6,"item_id":"msg_1","output_index":2,"content_index":0,"delta":"is a group."}`},
	{"response.completed", responsesDone(`"completed"`,
		reasoningItem("rs_1", "I count the pods.", "Then the nodes."),
		reasoningItem("rs_2", "They are twelve."),
		`{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"A pod is a group.","annotations":[]}]}`)},
}

// responsesThinksAgainStream is a recorded reply that summarizes again after its
// first text: two reasoning items and two message items, in the order served.
var responsesThinksAgainStream = []event{
	responsesStream[0],
	summaryDelta("rs_1", 0, "I count "),
	{"response.output_text.delta", `{"type":"response.output_text.delta","sequence_number":2,"item_id":"msg_1","output_index":1,"content_index":0,"delta":"A pod "}`},
	summaryDelta("rs_2", 0, "and a node."),
	{"response.output_text.delta", `{"type":"response.output_text.delta","sequence_number":4,"item_id":"msg_2","output_index":3,"content_index":0,"delta":"is a group."}`},
	{"response.completed", responsesDone(`"completed"`,
		reasoningItem("rs_1", "I count "),
		`{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"A pod ","annotations":[]}]}`,
		reasoningItem("rs_2", "and a node."),
		`{"id":"msg_2","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"is a group.","annotations":[]}]}`)},
}

// summaryDelta is one piece of a reasoning item's summary part.
func summaryDelta(itemID string, index int, delta string) event {
	return event{"response.reasoning_summary_text.delta", fmt.Sprintf(
		`{"type":"response.reasoning_summary_text.delta","sequence_number":1,"item_id":%q,"output_index":0,"summary_index":%d,"delta":%q}`,
		itemID, index, delta)}
}

// reasoningItem is a finished reasoning item carrying the given summary parts.
func reasoningItem(id string, parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, fmt.Sprintf(`{"type":"summary_text","text":%q}`, p))
	}
	return fmt.Sprintf(`{"id":%q,"type":"reasoning","summary":[%s]}`, id, strings.Join(out, ","))
}

// responsesDone is a terminal event's data: a response of the given status
// carrying the given output items.
func responsesDone(status string, items ...string) string {
	return `{"type":"response.completed","sequence_number":9,"response":{"id":"resp_1","model":"gpt-5-mini-2025-08-07","status":` + status +
		`,"output":[` + strings.Join(items, ",") + `]}}`
}

// openAIAt is the OpenAI provider pointed at baseURL.
func openAIAt(baseURL string) Provider {
	return Provider{ID: "openai", Label: "OpenAI", Dialect: DialectResponses, BaseURL: baseURL, Key: "sk-test"}
}

// responsesRequest is what every test on this wire sends, before its messages.
func responsesRequest(p Provider, msgs ...Message) Request {
	if msgs == nil {
		msgs = []Message{{Role: "user", Blocks: []Block{TextBlock("what is a pod?")}}}
	}
	return Request{
		Provider: p, Model: Model{ID: "gpt-5-mini", MaxOutputTokens: 64_000},
		SystemPrompt: "be terse", Messages: msgs,
	}
}

// responsesReplay serves one recorded stream and records the request it was asked with.
func responsesReplay(t *testing.T, events []event) (Provider, *recorded) {
	t.Helper()
	rec := &recorded{}
	return openAIAt(newServer(t, func(w http.ResponseWriter, r *http.Request) {
		rec.take(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			sse(w, e)
		}
	})), rec
}

// askResponses streams req and returns the chunks' text joined, the response and
// the error.
func askResponses(t *testing.T, req Request) (string, Response, error) {
	t.Helper()
	var text strings.Builder
	resp, err := streamResponses(t.Context(), req, testutil.Timeout, wallClock, func(c Chunk) { text.WriteString(c.Text) })
	return text.String(), resp, err
}

// The text arrives as it streams, the response holds it whole with the stop reason,
// and the request carried the model, the cap, the instructions, store: false and
// the input items.
func TestResponsesStreamsAReplyAndRecordsIt(t *testing.T) {
	p, rec := responsesReplay(t, responsesStream)

	text, resp, err := askResponses(t, responsesRequest(p))

	require.NoError(t, err)
	assert.Equal(t, "A pod is a group.", text)
	assert.Equal(t, []Block{TextBlock("A pod is a group.")}, resp.Blocks)
	assert.Equal(t, "completed", resp.StopReason)
	assert.Equal(t, "gpt-5-mini", rec.body["model"])
	assert.InDelta(t, 64_000, rec.body["max_output_tokens"], 0)
	assert.Equal(t, "be terse", rec.body["instructions"])
	assert.Equal(t, false, rec.body["store"])
	assert.Equal(t, "/responses", rec.path)
	assert.Equal(t, "Bearer sk-test", rec.header.Get("Authorization"))
	assert.Regexp(t, "^"+regexp.QuoteMeta(version.UserAgent())+" OpenAI/Go [0-9.]+$", rec.header.Get("User-Agent"))
}

// A model that lists efforts asks to reason at that level and for a summary of it.
func TestResponsesSendsReasoningWithTheEffort(t *testing.T) {
	p, rec := responsesReplay(t, responsesStream)
	req := responsesRequest(p)
	req.Effort = "xhigh"

	_, _, err := askResponses(t, req)

	require.NoError(t, err)
	assert.Equal(t, map[string]any{"effort": "xhigh", "summary": "auto"}, rec.body["reasoning"])

	plain, plainRec := responsesReplay(t, responsesStream)
	_, _, err = askResponses(t, responsesRequest(plain))
	require.NoError(t, err)
	assert.NotContains(t, plainRec.body, "reasoning")
}

// Each summary part is a section, and the index restarts per reasoning item, so
// the section is the pair: the parts stream as thinking chunks with one separator
// between each, and the record holds them as one thinking block ahead of the text.
func TestResponsesKeepsSummarySectionsApart(t *testing.T) {
	p, _ := responsesReplay(t, responsesThinkingStream)
	var chunks []Chunk

	resp, err := streamResponses(t.Context(), responsesRequest(p), testutil.Timeout, wallClock, func(c Chunk) { chunks = append(chunks, c) })

	require.NoError(t, err)
	assert.Equal(t, []Chunk{
		{Kind: ChunkThinking, Text: "I count "},
		{Kind: ChunkThinking, Text: "the pods."},
		{Kind: ChunkThinking, Text: "\n\nThen the nodes."},
		{Kind: ChunkThinking, Text: "\n\nThey are twelve."},
		{Text: "A pod "},
		{Text: "is a group."},
	}, chunks)
	require.Len(t, resp.Blocks, 3, "one thinking block per reasoning item, then the text")
	assert.Equal(t, "I count the pods.\n\nThen the nodes.", resp.Blocks[0].Text)
	assert.JSONEq(t, reasoningItem("rs_1", "I count the pods.", "Then the nodes."), string(resp.Blocks[0].Payload))
	assert.Equal(t, "They are twelve.", resp.Blocks[1].Text)
	assert.Equal(t, TextBlock("A pod is a group."), resp.Blocks[2])
	assert.Equal(t, "I count the pods.\n\nThen the nodes.\n\nThey are twelve.", Thinking(resp.Blocks))

	t.Run("an empty part is left out, as the joiner opened no section for it", func(t *testing.T) {
		p, _ := responsesReplay(t, []event{responsesStream[0],
			{"response.completed", responsesDone(`"completed"`, reasoningItem("rs_1", "", "I counted.", ""))}})

		_, resp, err := askResponses(t, responsesRequest(p))

		require.NoError(t, err)
		require.Len(t, resp.Blocks, 1)
		assert.Equal(t, "I counted.", resp.Blocks[0].Text)
	})
}

// The wire requires a cap, so a model that states none is refused before the call.
func TestResponsesRefusesAModelWithNoOutputCap(t *testing.T) {
	p, rec := responsesReplay(t, responsesStream)
	req := responsesRequest(p)
	req.Model.MaxOutputTokens = 0

	_, _, err := askResponses(t, req)

	require.Error(t, err)
	assert.Zero(t, rec.calls.Load())
}

// Each way a stream can end, and what the record keeps of it.
func TestResponsesTerminalCases(t *testing.T) {
	t.Run("incomplete is a complete response under its own reason", func(t *testing.T) {
		p, _ := responsesReplay(t, []event{{"response.incomplete", `{"type":"response.incomplete","sequence_number":9,"response":{"id":"resp_1","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"A pod","annotations":[]}]}]}}`}})

		_, resp, err := askResponses(t, responsesRequest(p))

		require.NoError(t, err)
		assert.Equal(t, "max_output_tokens", resp.StopReason)
		assert.Equal(t, []Block{TextBlock("A pod")}, resp.Blocks)
	})

	t.Run("incomplete with no reason keeps its status", func(t *testing.T) {
		p, _ := responsesReplay(t, []event{{"response.incomplete", `{"type":"response.incomplete","sequence_number":9,"response":{"id":"resp_1","status":"incomplete","output":[]}}`}})

		_, resp, err := askResponses(t, responsesRequest(p))

		require.NoError(t, err)
		assert.Equal(t, "incomplete", resp.StopReason)
	})

	t.Run("a refusal is the answer's text", func(t *testing.T) {
		p, _ := responsesReplay(t, []event{
			{"response.refusal.delta", `{"type":"response.refusal.delta","sequence_number":1,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"I cannot help "}`},
			{"response.refusal.delta", `{"type":"response.refusal.delta","sequence_number":2,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"with that."}`},
			{"response.completed", responsesDone(`"completed"`, `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"refusal","refusal":"I cannot help with that."}]}`)},
		})

		text, resp, err := askResponses(t, responsesRequest(p))

		require.NoError(t, err)
		assert.Equal(t, "I cannot help with that.", text)
		assert.Equal(t, []Block{TextBlock("I cannot help with that.")}, resp.Blocks)
		assert.Equal(t, "refusal", resp.StopReason)
	})

	t.Run("a failed response keeps the status word and no message", func(t *testing.T) {
		p, _ := responsesReplay(t, []event{{"response.failed", `{"type":"response.failed","sequence_number":9,"response":{"id":"resp_1","status":"failed","error":{"code":"server_error","message":"MARKER"},"output":[]}}`}})

		_, _, err := askResponses(t, responsesRequest(p))

		assert.EqualError(t, err, "openai: failed")
	})

	t.Run("an error event is its own type, its code classified", func(t *testing.T) {
		p, _ := responsesReplay(t, []event{{"error", `{"type":"error","sequence_number":1,"code":"server_error","message":"MARKER","param":null}`}})
		_, _, err := askResponses(t, responsesRequest(p))
		assert.EqualError(t, err, "openai: error")

		p, _ = responsesReplay(t, []event{{"error", `{"type":"error","sequence_number":1,"code":"context_length_exceeded","message":"MARKER","param":null}`}})
		_, _, err = askResponses(t, responsesRequest(p))
		assert.EqualError(t, err, "openai: error context_length_exceeded")
	})

	t.Run("no terminal event is an incomplete reply", func(t *testing.T) {
		p, _ := responsesReplay(t, responsesStream[:3])

		_, _, err := askResponses(t, responsesRequest(p))

		assert.Equal(t, IncompleteError("openai"), err)
		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})
}

// A reply that cannot be read leaks none of its bytes, into the error or the log.
func TestResponsesKeepsAMalformedReplyOutOfTheRecordAndTheLog(t *testing.T) {
	const marker = "MARKER-the-user-said-this"

	t.Run("an error type the API does not define", func(t *testing.T) {
		p := openAIAt(newServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(503)
			fmt.Fprint(w, `{"error":{"type":"`+marker+`","message":"`+marker+`","code":"`+marker+`"}}`)
		}))

		_, _, err := askResponses(t, responsesRequest(p))

		assert.EqualError(t, err, "openai: 503")
	})

	t.Run("a body that is not SSE", func(t *testing.T) {
		buf := captureLog(t)
		p := openAIAt(newServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, marker)
		}))

		_, _, err := askResponses(t, responsesRequest(p))

		require.Error(t, err)
		assert.NotContains(t, err.Error(), marker)
		assert.NotContains(t, buf.String(), marker)
	})

	t.Run("a status line that is not HTTP", func(t *testing.T) {
		buf := captureLog(t)

		_, _, err := askResponses(t, responsesRequest(openAIAt(newRawServer(t, marker))))

		assert.Equal(t, ReadError("openai"), err)
		assert.NotContains(t, buf.String(), marker)
	})
}

// The bound measures the wire: a stream that reasons in silence trips it, one
// that only sends bytes does not.
func TestResponsesIdleBound(t *testing.T) {
	const bound = time.Minute

	t.Run("a stream that only sends bytes", func(t *testing.T) {
		beats := make(chan struct{})
		p := openAIAt(newServer(t, serveBeats(beats,
			func(w http.ResponseWriter) { sse(w, responsesStream[0]) },
			func(w http.ResponseWriter) {
				fmt.Fprint(w, ": keep reading\n\n")
				w.(http.Flusher).Flush()
			},
			func(w http.ResponseWriter) { sse(w, responsesStream[3]) },
		)))
		timer := newFakeTimer()

		done := goStream(func() (Response, error) {
			return streamResponses(t.Context(), responsesRequest(p), bound, timer.clock, func(Chunk) {})
		})
		beat(t, timer, beats)
		got := testutil.Recv(t, done, "the stream's end")

		require.NoError(t, got.err)
	})

	t.Run("a stream that goes silent", func(t *testing.T) {
		p := openAIAt(newServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			sse(w, responsesStream[0])
			<-r.Context().Done()
		}))
		timer := newFakeTimer()

		done := goStream(func() (Response, error) {
			return streamResponses(t.Context(), responsesRequest(p), bound, timer.clock, func(Chunk) {})
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
func TestResponsesEndsOnCancel(t *testing.T) {
	p := openAIAt(newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range responsesStream[:2] {
			sse(w, e)
		}
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithCancel(t.Context())
	var text strings.Builder

	_, err := streamResponses(ctx, responsesRequest(p), testutil.Timeout, wallClock, func(c Chunk) {
		text.WriteString(c.Text)
		cancel()
	})

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, "A pod ", text.String())
}

// A message is one item whose content is its blocks as one string; a message the
// drop empties is left out, and an empty system prompt sends no instructions.
func TestResponsesEncodesTheConversation(t *testing.T) {
	p, rec := responsesReplay(t, responsesStream)
	req := responsesRequest(p,
		Message{Role: "user", Blocks: []Block{ContextBlock("the card"), TextBlock("what is a pod?")}},
		Message{Role: "assistant", Blocks: []Block{TextBlock("a group.")}},
		Message{Role: "user", Blocks: []Block{TextBlock("")}},
	)
	req.SystemPrompt = ""

	_, _, err := askResponses(t, req)

	require.NoError(t, err)
	assert.Equal(t, []any{
		map[string]any{"role": "user", "content": "<context>\nthe card\n</context>\n\nwhat is a pod?"},
		map[string]any{"role": "assistant", "content": "a group."},
	}, rec.body["input"])
	assert.NotContains(t, rec.body, "instructions")
}

// A notice rides as text ahead of the question it came with.
func TestResponsesSendsANoticeAsText(t *testing.T) {
	p, rec := responsesReplay(t, responsesStream)
	req := responsesRequest(p, Message{Role: "user", Blocks: []Block{exitedNotice(0), TextBlock("and?")}})
	req.SystemPrompt = ""

	_, _, err := askResponses(t, req)

	require.NoError(t, err)
	assert.Equal(t, []any{
		map[string]any{"role": "user", "content": Prompt([]Block{exitedNotice(0)}) + "\n\nand?"},
	}, rec.body["input"])
}

// The answer is the reasoning items' summary and the message items' text and
// refusal parts; every other item and part is kept out, and the log names where it
// was and nothing of what it said.
func TestResponsesKeepsOnlyTextAndRefusalParts(t *testing.T) {
	const marker = "MARKER-the-model-reasoned-this"
	buf := captureLog(t)
	p, _ := responsesReplay(t, []event{{"response.completed", responsesDone(`"completed"`,
		reasoningItem("rs_1", "I counted."),
		`{"id":"ws_1","type":"web_search_call","status":"completed","action":{"type":"search","query":"`+marker+`"}}`,
		`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"reasoning_text","text":"`+marker+`"},{"type":"output_text","text":"A pod.","annotations":[]}]}`,
	)}})

	_, resp, err := askResponses(t, responsesRequest(p))

	require.NoError(t, err)
	require.Len(t, resp.Blocks, 2)
	assert.Equal(t, "I counted.", resp.Blocks[0].Text)
	assert.Equal(t, TextBlock("A pod."), resp.Blocks[1])
	assert.NotContains(t, buf.String(), marker)
	assert.Contains(t, buf.String(), "index=1", "the item of neither kind, by position")
	assert.Contains(t, buf.String(), "index=2")
	assert.Contains(t, buf.String(), "part=0")
}

// The client is built from the provider alone, so a hostile environment reaches
// no request.
func TestResponsesClientTakesNothingFromTheEnvironment(t *testing.T) {
	forbidden := newServer(t, func(http.ResponseWriter, *http.Request) {
		t.Error("the client reached the base URL from the environment")
	})
	for _, name := range openAIEnv {
		t.Setenv(name, "ENV-"+name)
	}
	// The two that must name something usable to be dangerous at all.
	t.Setenv("OPENAI_BASE_URL", forbidden)
	t.Setenv("OPENAI_CUSTOM_HEADERS", "x-smuggled: ENV-HEADER")

	p, rec := responsesReplay(t, responsesStream)
	_, _, err := askResponses(t, responsesRequest(p))

	require.NoError(t, err)
	assert.Equal(t, int64(1), rec.calls.Load(), "the request went where the client was built to go")
	assert.Equal(t, "Bearer sk-test", rec.header.Get("Authorization"))
	assert.Empty(t, rec.header.Get("X-Smuggled"))
	for _, values := range rec.header {
		for _, v := range values {
			assert.NotContains(t, v, "ENV-")
		}
	}
}

// The one refusal the client classifies, and every other left uncoded.
func TestResponsesNamesTheRequestItCannotRead(t *testing.T) {
	p := openAIAt(newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"type":"invalid_request_error","code":"context_length_exceeded","message":"too long"}}`)
	}))

	_, _, err := askResponses(t, responsesRequest(p))

	var llmErr *Error
	require.ErrorAs(t, err, &llmErr)
	assert.True(t, llmErr.ContextFull())
	assert.EqualError(t, err, "openai: 400 invalid_request_error context_length_exceeded")
}

// A vendor's own request parameter is a line of data on its entry, at its JSON path
// in the body, and an entry without one sends no such field.
func TestResponsesSendsAnEntrysExtrasOnTheBody(t *testing.T) {
	p, rec := responsesReplay(t, responsesStream)
	p.Extra = map[string]any{"service_tier": "flex"}

	_, _, err := askResponses(t, responsesRequest(p))

	require.NoError(t, err)
	assert.Equal(t, "flex", rec.body["service_tier"])

	plain, plainRec := responsesReplay(t, responsesStream)
	_, _, err = askResponses(t, responsesRequest(plain))
	require.NoError(t, err)
	assert.NotContains(t, plainRec.body, "service_tier")
}

// responsesToolStream is a recorded reply that reasons, asks for a call, then
// says a word: the order the API writes them and pairs them by.
var responsesToolStream = []event{
	responsesStream[0],
	summaryDelta("rs_1", 0, "I should list them."),
	{"response.output_text.delta", `{"type":"response.output_text.delta","sequence_number":3,"item_id":"msg_1","output_index":1,"content_index":0,"delta":"Listing."}`},
	{"response.completed", responsesDone(`"completed"`,
		reasoningItem("rs_1", "I should list them."),
		`{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Listing.","annotations":[]}]}`,
		functionCallItem("completed", `{\"resource\":\"pods\"}`))},
}

// functionCallItem is one call item of the given status, with arguments as the
// JSON string the API sends.
func functionCallItem(status, arguments string) string {
	return `{"id":"fc_1","type":"function_call","call_id":"call_1","name":"list_objects","arguments":"` + arguments + `","status":"` + status + `"}`
}

// The definitions go on the request as function tools, the schema as written and
// strict false, since strict wants every property required.
func TestResponsesOffersTheToolsItIsHanded(t *testing.T) {
	p, rec := responsesReplay(t, responsesToolStream)
	req := responsesRequest(p)
	req.Tools = []ToolDefinition{listObjects}

	_, _, err := askResponses(t, req)

	require.NoError(t, err)
	assert.Equal(t, []any{map[string]any{
		"type": "function", "name": "list_objects", "description": "List one kind.",
		"parameters": map[string]any{
			"type":       "object",
			"properties": map[string]any{"resource": map[string]any{"type": "string"}},
			"required":   []any{"resource"},
		},
		"strict": false,
	}}, rec.body["tools"])

	none, noneRec := responsesReplay(t, responsesStream)
	_, _, err = askResponses(t, responsesRequest(none))
	require.NoError(t, err)
	assert.NotContains(t, noneRec.body, "tools")
}

// A schema that is not JSON fails the send, naming the tool.
func TestResponsesRefusesAToolItCannotEncode(t *testing.T) {
	p, rec := responsesReplay(t, responsesStream)
	req := responsesRequest(p)
	req.Tools = []ToolDefinition{{Name: "broken", InputSchema: json.RawMessage(`{`)}}

	_, _, err := askResponses(t, req)

	assert.ErrorContains(t, err, "broken")
	assert.Zero(t, rec.calls.Load())
}

// store is false, so a reasoning item is the only copy there is: a request that
// reasons asks for it back, and one that does not asks for nothing.
func TestResponsesAsksForTheReasoningBack(t *testing.T) {
	p, rec := responsesReplay(t, responsesStream)
	req := responsesRequest(p)
	req.Effort = "xhigh"

	_, _, err := askResponses(t, req)

	require.NoError(t, err)
	assert.Equal(t, []any{"reasoning.encrypted_content"}, rec.body["include"])

	plain, plainRec := responsesReplay(t, responsesStream)
	_, _, err = askResponses(t, responsesRequest(plain))
	require.NoError(t, err)
	assert.NotContains(t, plainRec.body, "include")
}

// The record reads the output in the order the API wrote it: a thinking block
// carrying its reasoning item, the call carrying its own item, then the text. A
// reply holding a call stops on tool_use, the one word the loop keys on.
func TestResponsesReadsACallIntoTheRecord(t *testing.T) {
	p, _ := responsesReplay(t, responsesToolStream)
	req := responsesRequest(p)
	req.Effort = "high"
	req.Tools = []ToolDefinition{listObjects}

	var chunks []Chunk
	resp, err := streamResponses(t.Context(), req, testutil.Timeout, wallClock, func(c Chunk) { chunks = append(chunks, c) })

	require.NoError(t, err)
	assert.Equal(t, []Chunk{
		{Kind: ChunkThinking, Text: "I should list them."},
		{Text: "Listing."},
	}, chunks, "no chunk carries the call")
	require.Len(t, resp.Blocks, 3)
	assert.Equal(t, BlockThinking, resp.Blocks[0].Type)
	assert.Equal(t, "I should list them.", resp.Blocks[0].Text)
	assert.JSONEq(t, reasoningItem("rs_1", "I should list them."), string(resp.Blocks[0].Payload))
	assert.Equal(t, TextBlock("Listing."), resp.Blocks[1])
	call := resp.Blocks[2]
	assert.Equal(t, ToolUseBlock("call_1", "list_objects", json.RawMessage(`{"resource":"pods"}`)), WithoutPayloads([]Block{call})[0])
	assert.JSONEq(t, functionCallItem("completed", `{\"resource\":\"pods\"}`), string(call.Payload))
	assert.Equal(t, StopToolUse, resp.StopReason)
}

// A call the cap cut off inside its arguments is stored as {} and keeps no payload,
// since the wire refuses those arguments back; the reply's own reason stands.
func TestResponsesStoresACutOffCallWithoutItsPayload(t *testing.T) {
	cutOff := []event{
		responsesStream[0],
		{"response.incomplete", `{"type":"response.incomplete","sequence_number":9,"response":{"id":"resp_1","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[` +
			functionCallItem("incomplete", `{\"resource\":`) + `]}}`},
	}
	p, _ := responsesReplay(t, cutOff)

	_, resp, err := askResponses(t, responsesRequest(p))

	require.NoError(t, err)
	assert.Equal(t, "max_output_tokens", resp.StopReason)
	require.Len(t, resp.Blocks, 1)
	assert.Equal(t, `{}`, string(resp.Blocks[0].Input))
	assert.Nil(t, resp.Blocks[0].Payload)
}

// A reply that ended well with no call keeps the API's own word for how it ended.
func TestResponsesFinishReasonStaysTheAPIsWithoutACall(t *testing.T) {
	p, _ := responsesReplay(t, responsesStream)

	_, resp, err := askResponses(t, responsesRequest(p))

	require.NoError(t, err)
	assert.Equal(t, "completed", resp.StopReason)
}

// responsesMalformedCall is a completed call whose arguments are not an object.
var responsesMalformedCall = []event{
	responsesStream[0],
	{"response.completed", responsesDone(`"completed"`, functionCallItem("completed", `[1,\"MARKER\"]`))},
}

// inputShape is what a rendering looks like on the wire: each item's type and,
// for a call or its answer, the call id it pairs on.
func inputShape(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, err := json.Marshal(body["input"])
	require.NoError(t, err)
	var decoded []struct {
		Type   string `json:"type"`
		Role   string `json:"role"`
		CallID string `json:"call_id"`
	}
	require.NoError(t, json.Unmarshal(raw, &decoded))
	var out []string
	for _, item := range decoded {
		line := item.Type
		if line == "" {
			line = "message:" + item.Role
		}
		if item.CallID != "" {
			line += "(" + item.CallID + ")"
		}
		out = append(out, line)
	}
	return out
}

// reasoned is a thinking block as this provider wrote it, its item as the payload.
func reasoned(id, text string) Block {
	b := ThinkingBlock(text)
	b.Payload = json.RawMessage(reasoningItem(id, text))
	return b
}

// calledWithPayload is a call block carrying its own item, as the wire read it.
func calledWithPayload() Block {
	b := ToolUseBlock("call_1", "list_objects", json.RawMessage(`{"resource":"pods"}`))
	b.Payload = json.RawMessage(functionCallItem("completed", `{\"resource\":\"pods\"}`))
	return b
}

// A row whose Extra asks for the reasoning gets it with no effort named: both
// requests of a tool round ask for the encrypted item and a summary, and the
// second replays the first's reasoning item whole.
func TestResponsesRunsAToolRoundOnAModelWithNoEfforts(t *testing.T) {
	asksForTheReasoning := func(rec *recorded) {
		t.Helper()
		assert.Equal(t, []any{"reasoning.encrypted_content"}, rec.body["include"])
		assert.Equal(t, map[string]any{"summary": "auto"}, rec.body["reasoning"])
		assert.NotContains(t, rec.body, "max_output_tokens")
	}
	planRow := func(p Provider) Provider {
		p.Extra = map[string]any{"include": []string{"reasoning.encrypted_content"}, "reasoning.summary": "auto"}
		p.Omit = []string{"max_output_tokens"}
		return p
	}
	p, rec := responsesReplay(t, responsesToolStream)
	ask := Message{Role: "user", Blocks: []Block{TextBlock("how many?")}}
	req := responsesRequest(planRow(p), ask)
	req.Tools = []ToolDefinition{listObjects}

	_, first, err := askResponses(t, req)

	require.NoError(t, err)
	asksForTheReasoning(rec)

	p, rec = responsesReplay(t, responsesStream)
	round := append(first.Blocks, ToolResultBlock("call_1", "2 pods", false))
	req = responsesRequest(planRow(p), ask, Message{Role: "assistant", Blocks: round, ProviderID: "openai", Dialect: DialectResponses})
	req.Tools = []ToolDefinition{listObjects}

	_, _, err = askResponses(t, req)

	require.NoError(t, err)
	asksForTheReasoning(rec)
	item, err := json.Marshal(rec.body["input"].([]any)[1])
	require.NoError(t, err)
	assert.JSONEq(t, reasoningItem("rs_1", "I should list them."), string(item))
}

// A row with rounds replays as the items the API wrote, each verbatim, and the
// answer to a call as a function_call_output that pairs on its id.
func TestResponsesReplaysAToolRoundAsItems(t *testing.T) {
	p, rec := responsesReplay(t, responsesStream)
	row := []Block{
		reasoned("rs_1", "I should list them."),
		calledWithPayload(),
		ToolResultBlock("call_1", "2 pods", false),
		reasoned("rs_2", "That is all."),
		TextBlock("Two."),
	}
	req := responsesRequest(p,
		Message{Role: "user", Blocks: []Block{TextBlock("how many?")}},
		Message{Role: "assistant", Blocks: row, ProviderID: "openai", Dialect: DialectResponses, Effort: "high"},
	)

	_, _, err := askResponses(t, req)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"message:user", "reasoning", "function_call(call_1)", "function_call_output(call_1)",
		"reasoning", "message:assistant",
	}, inputShape(t, rec.body))
	items := rec.body["input"].([]any)
	assert.Equal(t, "rs_1", items[1].(map[string]any)["id"], "the item goes back verbatim")
	assert.Equal(t, "fc_1", items[2].(map[string]any)["id"])
	assert.Equal(t, "2 pods", items[3].(map[string]any)["output"])
}

// A call with no payload — the fake's, another writer's, or one the cap cut off —
// is rebuilt from the app's fields.
func TestResponsesReplaysACallWithNoPayloadFromItsFields(t *testing.T) {
	p, rec := responsesReplay(t, responsesStream)
	row := []Block{
		ToolUseBlock("call_9", "list_objects", json.RawMessage(`{}`)),
		ToolResultBlock("call_9", "none", false),
	}

	_, _, err := askResponses(t, responsesRequest(p, Message{Role: "assistant", Blocks: row, ProviderID: "openai", Dialect: DialectResponses}))

	require.NoError(t, err)
	assert.Equal(t, []string{"function_call(call_9)", "function_call_output(call_9)"}, inputShape(t, rec.body))
	call := rec.body["input"].([]any)[0].(map[string]any)
	assert.Equal(t, "list_objects", call["name"])
	assert.Equal(t, "{}", call["arguments"])
}

// The payloads are the provider's: another provider on this dialect sends no
// reasoning item at all, and the call it kept goes as a plain function_call,
// under a minted id like any foreign call.
func TestResponsesPayloadsAreKeyedOnTheProvider(t *testing.T) {
	p, rec := responsesReplay(t, responsesStream)
	row := []Block{reasoned("rs_1", "I should list them."), calledWithPayload(), ToolResultBlock("call_1", "2 pods", false)}
	req := responsesRequest(p, Message{Role: "assistant", Blocks: row, ProviderID: "azure-openai", Dialect: DialectResponses, Effort: "high"})
	req.Effort = "high"

	_, _, err := askResponses(t, req)

	require.NoError(t, err)
	assert.Equal(t, []string{"function_call(call00000)", "function_call_output(call00000)"}, inputShape(t, rec.body))
	assert.NotContains(t, rec.body["input"].([]any)[0].(map[string]any), "id")
}

// The payloads are the dialect's too: what this provider wrote on another API
// goes back as Kubetail's fields alone.
func TestResponsesPayloadsAreKeyedOnTheDialect(t *testing.T) {
	p, rec := responsesReplay(t, responsesStream)
	row := []Block{reasoned("rs_1", "I should list them."), calledWithPayload(), ToolResultBlock("call_1", "2 pods", false)}
	req := responsesRequest(p, Message{Role: "assistant", Blocks: row, ProviderID: "openai", Dialect: DialectChatCompletions, Effort: "high"})
	req.Effort = "high"

	_, _, err := askResponses(t, req)

	require.NoError(t, err)
	assert.Equal(t, []string{"function_call(call00000)", "function_call_output(call00000)"}, inputShape(t, rec.body))
	assert.NotContains(t, rec.body["input"].([]any)[0].(map[string]any), "id")
}

// The loop's second request carries the round it has run so far: the items this
// provider wrote go back as they came, the answer after them.
func TestResponsesContinuesItsOwnRound(t *testing.T) {
	p, rec := responsesReplay(t, responsesStream)
	rounds := []Block{reasoned("rs_1", "I should list them."), calledWithPayload(), ToolResultBlock("call_1", "2 pods", false)}
	req := responsesRequest(p,
		Message{Role: "user", Blocks: []Block{TextBlock("how many?")}},
		Message{Role: "assistant", Blocks: rounds, ProviderID: "openai", Dialect: DialectResponses, Effort: "high"},
	)
	req.Effort = "high"
	req.Tools = []ToolDefinition{listObjects}

	_, _, err := askResponses(t, req)

	require.NoError(t, err)
	assert.Equal(t, []string{"message:user", "reasoning", "function_call(call_1)", "function_call_output(call_1)"},
		inputShape(t, rec.body))
	items := rec.body["input"].([]any)
	var reasoning map[string]any
	require.NoError(t, json.Unmarshal([]byte(reasoningItem("rs_1", "I should list them.")), &reasoning))
	assert.Equal(t, reasoning, items[1])
}

// An incomplete reply is cut off whether or not it says why, so a call in one is
// not a reply that finished asking: the record keeps the API's own word and the
// loop answers the call not-run.
func TestResponsesKeepsAnIncompleteStatusOverACall(t *testing.T) {
	p, _ := responsesReplay(t, []event{
		responsesStream[0],
		{"response.incomplete", `{"type":"response.incomplete","sequence_number":9,"response":{"id":"resp_1","status":"incomplete","output":[` +
			functionCallItem("incomplete", `{\"resource\":\"pods\"}`) + `]}}`},
	})

	_, resp, err := askResponses(t, responsesRequest(p))

	require.NoError(t, err)
	assert.Equal(t, "incomplete", resp.StopReason)
}

// Whether a call's arguments may be repaired is the reply's word, not the item's:
// a reply that ended carrying arguments that are not an object is one this wire
// could not read, whatever the item says about itself.
func TestResponsesFailsAMalformedCallOnAnItemWithNoStatus(t *testing.T) {
	p, _ := responsesReplay(t, []event{
		responsesStream[0],
		{"response.completed", responsesDone(`"completed"`,
			`{"id":"fc_1","type":"function_call","call_id":"call_1","name":"list_objects","arguments":"[1]"}`)},
	})

	_, resp, err := askResponses(t, responsesRequest(p))

	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, "unreadable reply", e.Kind)
	assert.Empty(t, resp.Blocks)
}

// responsesInput is item i of a recorded body's input.
func responsesInput(body map[string]any, i int) map[string]any {
	return body["input"].([]any)[i].(map[string]any)
}

// A foreign call goes under a minted call_id, and its output answers the same
// one: the API pairs the two by it, so an id another vendor's chat repeats would
// be ambiguous.
func TestResponsesMintsAForeignCallsID(t *testing.T) {
	p, rec := responsesReplay(t, responsesStream)
	row := Message{Role: "assistant", ProviderID: "anthropic", Dialect: DialectResponses, Blocks: []Block{
		ToolUseBlock("toolu_01ABCDEFG", "list_objects", json.RawMessage(`{"resource":"pods"}`)),
		ToolResultBlock("toolu_01ABCDEFG", "2 pods", false),
		TextBlock("Two."),
	}}

	_, _, err := askResponses(t, responsesRequest(p, Message{Role: "user", Blocks: []Block{TextBlock("how many pods?")}}, row))

	require.NoError(t, err)
	assert.Equal(t, "function_call", responsesInput(rec.body, 1)["type"])
	assert.Equal(t, "call00000", responsesInput(rec.body, 1)["call_id"])
	assert.Equal(t, "call00000", responsesInput(rec.body, 2)["call_id"])
	assert.Equal(t, "toolu_01ABCDEFG", row.Blocks[0].ID, "the record keeps the writer's own")
}

// An own function_call whose call_id an earlier call already went under is
// minted, and the item it keeps goes with the minted call_id, so the item and
// its output pair up.
func TestResponsesMintsASpentIDInsideItsPayload(t *testing.T) {
	p, rec := responsesReplay(t, responsesStream)
	row := Message{Role: "assistant", ProviderID: "openai", Dialect: DialectResponses, Blocks: []Block{
		calledWithPayload(), ToolResultBlock("call_1", "2 pods", false),
	}}
	question := Message{Role: "user", Blocks: []Block{TextBlock("how many pods?")}}

	_, _, err := askResponses(t, responsesRequest(p, question, row, question, row))

	require.NoError(t, err)
	assert.Equal(t, []string{
		"message:user", "function_call(call_1)", "function_call_output(call_1)",
		"message:user", "function_call(call00001)", "function_call_output(call00001)",
	}, inputShape(t, rec.body))
	assert.Equal(t, "fc_1", responsesInput(rec.body, 4)["id"], "the rest of the item goes as it came")
}

// An output item that is not a JSON object is unreadable: the record never
// holds an item the API would refuse on replay.
func TestResponsesRefusesAnItemThatIsNotAnObject(t *testing.T) {
	blocks, refused, err := responsesWire{providerID: "openai"}.blocks([]responses.ResponseOutputItemUnion{{Type: "reasoning"}}, false)

	assert.Nil(t, blocks)
	assert.False(t, refused)
	assert.Equal(t, ReadError("openai"), err)
}

// limitCodes is a row's own codes: one usage limit, one other.
var limitCodes = map[string]CodeClass{
	"subscription_sharing_usage_limit_exceeded": CodeUsageLimit,
	"chatpass_v2_scope_not_authorized":          CodeOther,
}

// A code the row defines is kept with its class, on an HTTP error and on a
// failed response alike; a code no one defines is still dropped.
func TestResponsesKeepsTheCodesItsRowDefines(t *testing.T) {
	refuse := func(code string) Provider {
		p := openAIAt(newServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprintf(w, `{"error":{"type":"rate_limit_error","code":%q,"message":"MARKER"}}`, code)
		}))
		p.Codes = limitCodes
		return p
	}

	_, _, err := askResponses(t, responsesRequest(refuse("subscription_sharing_usage_limit_exceeded")))
	var llmErr *Error
	require.ErrorAs(t, err, &llmErr)
	assert.True(t, llmErr.UsageLimit())
	assert.EqualError(t, err, "openai: 429 rate_limit_error subscription_sharing_usage_limit_exceeded")

	_, _, err = askResponses(t, responsesRequest(refuse("chatpass_v2_scope_not_authorized")))
	require.ErrorAs(t, err, &llmErr)
	assert.False(t, llmErr.UsageLimit())
	assert.Equal(t, "chatpass_v2_scope_not_authorized", llmErr.Code)

	_, _, err = askResponses(t, responsesRequest(refuse("something_else")))
	require.ErrorAs(t, err, &llmErr)
	assert.Empty(t, llmErr.Code)

	p, _ := responsesReplay(t, []event{{"response.failed", `{"type":"response.failed","sequence_number":9,"response":{"id":"resp_1","status":"failed","error":{"code":"subscription_sharing_usage_limit_exceeded","message":"MARKER"},"output":[]}}`}})
	p.Codes = limitCodes
	_, _, err = askResponses(t, responsesRequest(p))
	require.ErrorAs(t, err, &llmErr)
	assert.True(t, llmErr.UsageLimit())

	p, _ = responsesReplay(t, []event{{"error", `{"type":"error","sequence_number":1,"code":"subscription_sharing_usage_limit_exceeded","message":"MARKER","param":null}`}})
	p.Codes = limitCodes
	_, _, err = askResponses(t, responsesRequest(p))
	require.ErrorAs(t, err, &llmErr)
	assert.True(t, llmErr.UsageLimit())
}
