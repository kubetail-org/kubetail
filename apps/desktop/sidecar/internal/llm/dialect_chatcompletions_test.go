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
	"net"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
)

// chunk is one chat.completion.chunk carrying the given delta and finish reason.
func chunk(delta, finish string) event {
	return event{"", `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":` +
		delta + `,"finish_reason":` + finish + `}]}`}
}

// chatStream is a recorded reply: one text in two pieces, its finish, its [DONE].
var chatStream = []event{
	chunk(`{"role":"assistant","content":"A pod "}`, "null"),
	chunk(`{"content":"is a group."}`, "null"),
	chunk(`{}`, `"stop"`),
	{"", "[DONE]"},
}

// chatReported is chatStream with the usage-only chunk include_usage asks for:
// the prompt includes the cached part.
var chatReported = []event{
	chatStream[0], chatStream[1], chatStream[2],
	{"", `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[],` +
		`"usage":{"prompt_tokens":35,"prompt_tokens_details":{"cached_tokens":20},"completion_tokens":7,"total_tokens":42}}`},
	{"", "[DONE]"},
}

// chatThinkingStream is a recorded reply that shows its thinking before its text,
// under the spelling the vendors that summarize use.
var chatThinkingStream = []event{
	chunk(`{"role":"assistant","reasoning_content":"I count "}`, "null"),
	chunk(`{"reasoning_content":"the pods."}`, "null"),
	chunk(`{"content":"A pod "}`, "null"),
	chunk(`{"content":"is a group."}`, "null"),
	chunk(`{}`, `"stop"`),
	{"", "[DONE]"},
}

// chatThinksAgainStream is a recorded reply that thinks again after its first text.
var chatThinksAgainStream = []event{
	chunk(`{"role":"assistant","reasoning_content":"I count "}`, "null"),
	chunk(`{"content":"A pod "}`, "null"),
	chunk(`{"reasoning_content":"and a node."}`, "null"),
	chunk(`{"content":"is a group."}`, "null"),
	chunk(`{}`, `"stop"`),
	{"", "[DONE]"},
}

// groqAt is a Chat Completions provider pointed at baseURL.
func groqAt(baseURL string) Provider {
	return Provider{ID: "groq", Label: "Groq", Dialect: DialectChatCompletions, BaseURL: baseURL, Key: "gsk-test"}
}

// chatRequest is what every test on this wire sends, before its messages.
func chatRequest(p Provider, msgs ...Message) Request {
	if msgs == nil {
		msgs = []Message{{Role: "user", Blocks: []Block{TextBlock("what is a pod?")}}}
	}
	return Request{
		Provider: p, Model: Model{ID: "openai/gpt-oss-120b", MaxOutputTokens: 16_384},
		SystemPrompt: "be terse", Messages: msgs,
	}
}

// chatReplay serves one recorded stream and records the request it was asked with.
func chatReplay(t *testing.T, events []event) (Provider, *recorded) {
	t.Helper()
	rec := &recorded{}
	return groqAt(newServer(t, func(w http.ResponseWriter, r *http.Request) {
		rec.take(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			sse(w, e)
		}
	})), rec
}

// askChat streams req and returns the chunks' text joined, the response and the error.
func askChat(t *testing.T, req Request) (string, Response, error) {
	t.Helper()
	var text strings.Builder
	resp, err := streamChatCompletions(t.Context(), req, testutil.Timeout, wallClock, func(c Chunk) { text.WriteString(c.Text) })
	return text.String(), resp, err
}

// The text arrives as it streams, the response holds it whole with the finish
// reason, and the request carried the model, the system message first, the
// conversation and the cap.
func TestChatCompletionsStreamsAReplyAndRecordsIt(t *testing.T) {
	p, rec := chatReplay(t, chatStream)

	text, resp, err := askChat(t, chatRequest(p))

	require.NoError(t, err)
	assert.Equal(t, "A pod is a group.", text)
	assert.Equal(t, []Block{TextBlock("A pod is a group.")}, resp.Blocks)
	assert.Equal(t, "stop", resp.StopReason)
	assert.Equal(t, "openai/gpt-oss-120b", rec.body["model"])
	assert.InDelta(t, 16_384, rec.body["max_tokens"], 0)
	assert.Equal(t, []any{
		map[string]any{"role": "system", "content": "be terse"},
		map[string]any{"role": "user", "content": "what is a pod?"},
	}, rec.body["messages"])
	assert.Equal(t, "/chat/completions", rec.path)
	assert.Equal(t, "Bearer gsk-test", rec.header.Get("Authorization"))
	assert.Regexp(t, "^"+regexp.QuoteMeta(version.UserAgent())+" OpenAI/Go [0-9.]+$", rec.header.Get("User-Agent"))
}

// The wire streams no usage unless asked for it, so every request asks.
func TestChatCompletionsAsksForUsage(t *testing.T) {
	p, rec := chatReplay(t, chatStream)

	_, _, err := askChat(t, chatRequest(p))

	require.NoError(t, err)
	assert.Equal(t, map[string]any{"include_usage": true}, rec.body["stream_options"])
}

// Some vendors send usage on a chunk with choices too, the finish's among them.
// Whichever chunk carries it, the last report stands.
func TestChatCompletionsKeepsTheLastUsage(t *testing.T) {
	withUsage := func(delta, finish, usage string) event {
		e := chunk(delta, finish)
		e.data = strings.TrimSuffix(e.data, "}") + `,"usage":` + usage + `}`
		return e
	}
	p, _ := chatReplay(t, []event{
		chunk(`{"role":"assistant","content":"A pod "}`, "null"),
		withUsage(`{"content":"is a group."}`, `"stop"`, `{"prompt_tokens":30,"completion_tokens":3,"total_tokens":33}`),
		{"", `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[],"usage":{"prompt_tokens":30,"completion_tokens":7,"total_tokens":37}}`},
		{"", "[DONE]"},
	})

	_, resp, err := askChat(t, chatRequest(p))

	require.NoError(t, err)
	assert.Equal(t, Usage{Reported: true, InputTokens: 30, OutputTokens: 7}, resp.Usage)

	p, _ = chatReplay(t, []event{
		chunk(`{"role":"assistant","content":"A pod is a group."}`, "null"),
		withUsage(`{}`, `"stop"`, `{"prompt_tokens":30,"completion_tokens":5,"total_tokens":35}`),
		{"", "[DONE]"},
	})

	_, resp, err = askChat(t, chatRequest(p))

	require.NoError(t, err)
	assert.Equal(t, Usage{Reported: true, InputTokens: 30, OutputTokens: 5}, resp.Usage, "on the finish's chunk alone")
}

// Each vendor's spelling of a cache hit reads as the one count, and a report that
// names none is a known zero.
func TestChatCompletionsReadsEverySpellingOfACacheHit(t *testing.T) {
	cases := []struct {
		name, usage string
		want        Usage
	}{
		{"cached_tokens", `{"prompt_tokens":30,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":20}}`,
			Usage{Reported: true, InputTokens: 30, CacheReadTokens: 20, OutputTokens: 5}},
		{"DeepSeek's hit alone", `{"prompt_tokens":30,"completion_tokens":5,"prompt_cache_hit_tokens":18,"prompt_cache_miss_tokens":12}`,
			Usage{Reported: true, InputTokens: 30, CacheReadTokens: 18, OutputTokens: 5}},
		{"cached_tokens over DeepSeek's", `{"prompt_tokens":30,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":20},"prompt_cache_hit_tokens":18}`,
			Usage{Reported: true, InputTokens: 30, CacheReadTokens: 20, OutputTokens: 5}},
		{"OpenRouter's write", `{"prompt_tokens":30,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":25}}`,
			Usage{Reported: true, InputTokens: 30, CacheWriteTokens: 25, OutputTokens: 5}},
		{"none", `{"prompt_tokens":30,"completion_tokens":5}`,
			Usage{Reported: true, InputTokens: 30, OutputTokens: 5}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, _ := chatReplay(t, []event{
				chunk(`{"role":"assistant","content":"A pod is a group."}`, `"stop"`),
				{"", `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[],"usage":` + c.usage + `}`},
				{"", "[DONE]"},
			})

			_, resp, err := askChat(t, chatRequest(p))

			require.NoError(t, err)
			assert.Equal(t, c.want, resp.Usage)
		})
	}
}

// This wire requires no cap, so a model that states none lets the vendor's own
// default apply.
func TestChatCompletionsSendsNoCapForAModelThatStatesNone(t *testing.T) {
	p, rec := chatReplay(t, chatStream)
	req := chatRequest(p)
	req.Model.MaxOutputTokens = 0

	_, _, err := askChat(t, req)

	require.NoError(t, err)
	assert.NotContains(t, rec.body, "max_tokens")
	assert.NotContains(t, rec.body, "max_completion_tokens")
}

// [DONE] and a body that ends alike end the stream, so only a finish reason says
// the answer was whole. A reset is the transport's failure, not an ending.
func TestChatCompletionsStreamEnds(t *testing.T) {
	// "The body ending" is the handler returning: the decoder reads that as a
	// clean EOF and reports no error, so the finish reason alone decides.
	t.Run("a finish reason then [DONE]", func(t *testing.T) {
		p, _ := chatReplay(t, chatStream)
		_, resp, err := askChat(t, chatRequest(p))
		require.NoError(t, err)
		assert.Equal(t, "stop", resp.StopReason)
	})

	t.Run("a finish reason then the body ending", func(t *testing.T) {
		p, _ := chatReplay(t, chatStream[:3])
		_, resp, err := askChat(t, chatRequest(p))
		require.NoError(t, err)
		assert.Equal(t, "stop", resp.StopReason)
	})

	t.Run("no finish reason then [DONE]", func(t *testing.T) {
		p, _ := chatReplay(t, append(chatStream[:2:2], event{"", "[DONE]"}))
		_, _, err := askChat(t, chatRequest(p))
		assert.Equal(t, IncompleteError("groq"), err)
		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})

	t.Run("no finish reason then the body ending", func(t *testing.T) {
		p, _ := chatReplay(t, chatStream[:2])
		_, _, err := askChat(t, chatRequest(p))
		assert.Equal(t, IncompleteError("groq"), err)
	})

	// A reset surfaces from the body read as a *net.OpError, which noResponse
	// accepts, so it is the transport's failure and its logged cause names a host
	// and no bytes. A plain close sends a FIN and is the body ending above.
	t.Run("a connection reset mid-stream", func(t *testing.T) {
		p := groqAt(newServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			sse(w, chatStream[0])
			c, _, err := w.(http.Hijacker).Hijack()
			require.NoError(t, err)
			defer c.Close()
			require.NoError(t, c.(*net.TCPConn).SetLinger(0))
		}))

		_, _, err := askChat(t, chatRequest(p))

		var llmErr *Error
		require.ErrorAs(t, err, &llmErr)
		assert.Equal(t, "connection failed", llmErr.Kind)
	})
}

// A vendor that summarizes its reasoning sends it in a field of its own, which
// the thinking step reads and this one leaves alone.
// A model that lists efforts sends the level it named; one that lists none sends
// no field at all. The API has no field to ask for a summary with, so nothing else
// about thinking goes on the body.
func TestChatCompletionsSendsTheEffortOfAModelThatHasOne(t *testing.T) {
	p, rec := chatReplay(t, chatStream)
	req := chatRequest(p)
	req.Effort = "high"

	_, _, err := askChat(t, req)

	require.NoError(t, err)
	assert.Equal(t, "high", rec.body["reasoning_effort"])

	plain, plainRec := chatReplay(t, chatStream)
	_, _, err = askChat(t, chatRequest(plain))
	require.NoError(t, err)
	assert.NotContains(t, plainRec.body, "reasoning_effort")
}

// What a vendor shows of its thinking arrives under a spelling of its own, and
// both the vendors use are read; a delta spelling it both ways yields the text
// once. A null field is nothing, which is what DeepSeek sends beside every text
// delta, and a vendor that sends neither shows nothing.
func TestChatCompletionsReadsEitherSpellingOfTheThinking(t *testing.T) {
	p, _ := chatReplay(t, []event{
		chunk(`{"role":"assistant","reasoning_content":"I count "}`, "null"),
		chunk(`{"reasoning":"the pods."}`, "null"),
		chunk(`{"reasoning_content":" Twelve.","reasoning":" Twelve."}`, "null"),
		chunk(`{"content":"A pod.","reasoning_content":null}`, "null"),
		chunk(`{}`, `"stop"`),
		{"", "[DONE]"},
	})
	var chunks []Chunk

	resp, err := streamChatCompletions(t.Context(), chatRequest(p), testutil.Timeout, wallClock, func(c Chunk) { chunks = append(chunks, c) })

	require.NoError(t, err)
	assert.Equal(t, []Chunk{
		{Kind: ChunkThinking, Text: "I count "},
		{Kind: ChunkThinking, Text: "the pods."},
		{Kind: ChunkThinking, Text: " Twelve."},
		{Text: "A pod."},
	}, chunks)
	assert.Equal(t, []Block{ThinkingBlock("I count the pods. Twelve."), TextBlock("A pod.")}, resp.Blocks)

	t.Run("a vendor that sends neither shows nothing", func(t *testing.T) {
		plain, _ := chatReplay(t, chatStream)

		_, resp, err := askChat(t, chatRequest(plain))

		require.NoError(t, err)
		assert.Equal(t, []Block{TextBlock("A pod is a group.")}, resp.Blocks)
	})
}

// A refusal is the answer's text here as on the Responses wire, so a
// refusal-only stream is an answer and not an empty one.
func TestChatCompletionsReadsARefusalAsTheAnswer(t *testing.T) {
	refusal := []event{
		chunk(`{"role":"assistant","refusal":"I cannot help "}`, "null"),
		chunk(`{"refusal":"with that."}`, "null"),
	}

	t.Run("refusal deltas then a finish reason", func(t *testing.T) {
		p, _ := chatReplay(t, append(refusal[:2:2], chunk(`{}`, `"stop"`), event{"", "[DONE]"}))

		text, resp, err := askChat(t, chatRequest(p))

		require.NoError(t, err)
		assert.Equal(t, "I cannot help with that.", text)
		assert.Equal(t, []Block{TextBlock("I cannot help with that.")}, resp.Blocks)
		assert.Equal(t, "refusal", resp.StopReason)
	})

	t.Run("a cancel after the first refusal delta", func(t *testing.T) {
		p := groqAt(newServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			sse(w, refusal[0])
			<-r.Context().Done()
		}))
		ctx, cancel := context.WithCancel(t.Context())
		var text strings.Builder

		_, err := streamChatCompletions(ctx, chatRequest(p), testutil.Timeout, wallClock, func(c Chunk) {
			text.WriteString(c.Text)
			cancel()
		})

		assert.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, "I cannot help ", text.String())
	})

	t.Run("refusal deltas cut off by the cap keep the cap's reason", func(t *testing.T) {
		p, _ := chatReplay(t, append(refusal[:2:2], chunk(`{}`, `"length"`), event{"", "[DONE]"}))

		_, resp, err := askChat(t, chatRequest(p))

		require.NoError(t, err)
		assert.Equal(t, "length", resp.StopReason)
	})

	t.Run("refusal deltas then the body ending", func(t *testing.T) {
		p, _ := chatReplay(t, refusal)
		_, _, err := askChat(t, chatRequest(p))
		assert.Equal(t, IncompleteError("groq"), err)
	})
}

// A reply that cannot be read leaks none of its bytes, into the error or the log.
func TestChatCompletionsKeepsAMalformedReplyOutOfTheRecordAndTheLog(t *testing.T) {
	const marker = "MARKER-the-user-said-this"

	t.Run("an error type the API does not define", func(t *testing.T) {
		p := groqAt(newServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(503)
			fmt.Fprint(w, `{"error":{"type":"`+marker+`","message":"`+marker+`","code":"`+marker+`"}}`)
		}))

		_, _, err := askChat(t, chatRequest(p))

		assert.EqualError(t, err, "groq: 503")
	})

	t.Run("an error inside the stream", func(t *testing.T) {
		buf := captureLog(t)
		p, _ := chatReplay(t, []event{chatStream[0], {"", `{"error":{"message":"` + marker + `"}}`}})

		_, _, err := askChat(t, chatRequest(p))

		assert.Equal(t, ReadError("groq"), err)
		assert.NotContains(t, buf.String(), marker)
	})

	t.Run("a body that is not SSE", func(t *testing.T) {
		buf := captureLog(t)
		p := groqAt(newServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, marker)
		}))

		_, _, err := askChat(t, chatRequest(p))

		require.Error(t, err)
		assert.NotContains(t, err.Error(), marker)
		assert.NotContains(t, buf.String(), marker)
	})

	t.Run("a status line that is not HTTP", func(t *testing.T) {
		buf := captureLog(t)

		_, _, err := askChat(t, chatRequest(groqAt(newRawServer(t, marker))))

		assert.Equal(t, ReadError("groq"), err)
		assert.NotContains(t, buf.String(), marker)
	})
}

// The bound measures the wire: a stream that thinks in silence trips it, one that
// only sends bytes does not.
func TestChatCompletionsIdleBound(t *testing.T) {
	const bound = time.Minute

	t.Run("a stream that only sends bytes", func(t *testing.T) {
		beats := make(chan struct{})
		p := groqAt(newServer(t, serveBeats(beats,
			func(w http.ResponseWriter) { sse(w, chatStream[0]) },
			func(w http.ResponseWriter) {
				fmt.Fprint(w, ": keep reading\n\n")
				w.(http.Flusher).Flush()
			},
			func(w http.ResponseWriter) {
				for _, e := range chatStream[2:] {
					sse(w, e)
				}
			},
		)))
		timer := newFakeTimer()

		done := goStream(func() (Response, error) {
			return streamChatCompletions(t.Context(), chatRequest(p), bound, timer.clock, func(Chunk) {})
		})
		beat(t, timer, beats)
		got := testutil.Recv(t, done, "the stream's end")

		require.NoError(t, got.err)
	})

	t.Run("a stream that goes silent", func(t *testing.T) {
		p := groqAt(newServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			sse(w, chatStream[0])
			<-r.Context().Done()
		}))
		timer := newFakeTimer()

		done := goStream(func() (Response, error) {
			return streamChatCompletions(t.Context(), chatRequest(p), bound, timer.clock, func(Chunk) {})
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
func TestChatCompletionsEndsOnCancel(t *testing.T) {
	p := groqAt(newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w, chatStream[0])
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithCancel(t.Context())
	var text strings.Builder

	_, err := streamChatCompletions(ctx, chatRequest(p), testutil.Timeout, wallClock, func(c Chunk) {
		text.WriteString(c.Text)
		cancel()
	})

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, "A pod ", text.String())
}

// This API has no field of its own for the system prompt, so it is the first
// message; a message the drop empties is left out, and an empty prompt sends no
// system message.
func TestChatCompletionsEncodesTheConversation(t *testing.T) {
	p, rec := chatReplay(t, chatStream)
	req := chatRequest(p,
		Message{Role: "user", Blocks: []Block{ContextBlock("the card"), TextBlock("what is a pod?")}},
		Message{Role: "assistant", Blocks: []Block{TextBlock("a group.")}},
		Message{Role: "user", Blocks: []Block{TextBlock("")}},
	)
	req.SystemPrompt = ""

	_, _, err := askChat(t, req)

	require.NoError(t, err)
	assert.Equal(t, []any{
		map[string]any{"role": "user", "content": "<context>\nthe card\n</context>\n\nwhat is a pod?"},
		map[string]any{"role": "assistant", "content": "a group."},
	}, rec.body["messages"])
}

// A notice rides as text ahead of the question it came with.
func TestChatCompletionsSendsANoticeAsText(t *testing.T) {
	p, rec := chatReplay(t, chatStream)
	req := chatRequest(p, Message{Role: "user", Blocks: []Block{exitedNotice(0), TextBlock("and?")}})
	req.SystemPrompt = ""

	_, _, err := askChat(t, req)

	require.NoError(t, err)
	assert.Equal(t, []any{
		map[string]any{"role": "user", "content": Prompt([]Block{exitedNotice(0)}) + "\n\nand?"},
	}, rec.body["messages"])
}

// The client is built from the provider alone, and with no key it sends no
// Authorization header at all, which is what a local endpoint needs.
func TestChatCompletionsClientTakesNothingFromTheEnvironment(t *testing.T) {
	forbidden := newServer(t, func(http.ResponseWriter, *http.Request) {
		t.Error("the client reached the base URL from the environment")
	})
	for _, name := range openAIEnv {
		t.Setenv(name, "ENV-"+name)
	}
	t.Setenv("OPENAI_BASE_URL", forbidden)
	t.Setenv("OPENAI_CUSTOM_HEADERS", "x-smuggled: ENV-HEADER")

	for _, key := range []string{"gsk-test", ""} {
		t.Run("key "+key, func(t *testing.T) {
			p, rec := chatReplay(t, chatStream)
			p.Key = key

			_, _, err := askChat(t, chatRequest(p))

			require.NoError(t, err)
			assert.Equal(t, int64(1), rec.calls.Load(), "the request went where the client was built to go")
			if key == "" {
				assert.NotContains(t, rec.header, "Authorization")
			} else {
				assert.Equal(t, "Bearer "+key, rec.header.Get("Authorization"))
			}
			assert.Empty(t, rec.header.Get("X-Smuggled"))
			for _, values := range rec.header {
				for _, v := range values {
					assert.NotContains(t, v, "ENV-")
				}
			}
		})
	}
}

// The one refusal the client classifies, and every other left uncoded.
func TestChatCompletionsNamesTheRequestItCannotRead(t *testing.T) {
	p := groqAt(newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"type":"invalid_request_error","code":"context_length_exceeded","message":"too long"}}`)
	}))

	_, _, err := askChat(t, chatRequest(p))

	var llmErr *Error
	require.ErrorAs(t, err, &llmErr)
	assert.True(t, llmErr.ContextFull())
	assert.EqualError(t, err, "groq: 400 invalid_request_error context_length_exceeded")
}

// A vendor's own request parameter is a line of data on its entry, at its JSON path
// in the body, and an entry without one sends no such field.
func TestChatCompletionsSendsAnEntrysExtrasOnTheBody(t *testing.T) {
	p, rec := chatReplay(t, chatStream)
	p.Extra = map[string]any{"provider.data_collection": "deny"}

	_, _, err := askChat(t, chatRequest(p))

	require.NoError(t, err)
	assert.Equal(t, map[string]any{"data_collection": "deny"}, rec.body["provider"])

	plain, plainRec := chatReplay(t, chatStream)
	_, _, err = askChat(t, chatRequest(plain))
	require.NoError(t, err)
	assert.NotContains(t, plainRec.body, "provider")
}

// The affinity key goes where the row routes it: in the header it names, or at
// the body path it names. A row with no route, or a request with no key, sends
// neither.
func TestChatCompletionsSendsTheAffinityKeyWhereTheRowSaysIt(t *testing.T) {
	ask := func(route AffinityRoute, key string) *recorded {
		t.Helper()
		p, rec := chatReplay(t, chatStream)
		p.AffinityRoute = route
		req := chatRequest(p)
		req.AffinityKey = key
		_, _, err := askChat(t, req)
		require.NoError(t, err)
		return rec
	}

	rec := ask(AffinityRoute{Header: "x-grok-conv-id"}, "chat-1")
	assert.Equal(t, "chat-1", rec.header.Get("x-grok-conv-id"))
	assert.NotContains(t, rec.body, "session_id")

	rec = ask(AffinityRoute{Field: "session_id"}, "chat-1")
	assert.Equal(t, "chat-1", rec.body["session_id"])
	assert.Empty(t, rec.header.Get("x-grok-conv-id"))

	rec = ask(AffinityRoute{}, "chat-1")
	assert.NotContains(t, rec.body, "session_id")
	assert.NotContains(t, rec.body, "prompt_cache_key")

	rec = ask(AffinityRoute{Header: "x-grok-conv-id"}, "")
	assert.NotContains(t, rec.header, "X-Grok-Conv-Id")
	rec = ask(AffinityRoute{Field: "session_id"}, "")
	assert.NotContains(t, rec.body, "session_id")
}

// A model that caches only on marks is sent the request-level mark, for an hour;
// any other is sent none, since an endpoint that does not know the field may
// refuse it.
func TestChatCompletionsMarksAModelThatCachesOnMarks(t *testing.T) {
	ask := func(kind CacheKind) *recorded {
		t.Helper()
		p, rec := chatReplay(t, chatStream)
		req := chatRequest(p)
		req.Model.Cache = kind
		_, _, err := askChat(t, req)
		require.NoError(t, err)
		return rec
	}

	assert.Equal(t, map[string]any{"type": "ephemeral", "ttl": "1h"}, ask(CacheMarks).body["cache_control"])
	assert.NotContains(t, ask(CacheAuto).body, "cache_control")
	assert.NotContains(t, ask(CacheNone).body, "cache_control")
}

// A row's omits still delete what the cache options set.
func TestChatCompletionsOmitsWhatTheCacheOptionsSet(t *testing.T) {
	p, rec := chatReplay(t, chatStream)
	p.AffinityRoute = AffinityRoute{Field: "session_id"}
	p.Omit = []string{"cache_control", "session_id"}
	req := chatRequest(p)
	req.Model.Cache = CacheMarks
	req.AffinityKey = "chat-1"

	_, _, err := askChat(t, req)

	require.NoError(t, err)
	assert.NotContains(t, rec.body, "cache_control")
	assert.NotContains(t, rec.body, "session_id")
}

// The usage-only chunk a vendor sends after the finish carries no choices; it is
// skipped, and the finish reason before it still stands.
func TestChatCompletionsSkipsAChunkWithNoChoices(t *testing.T) {
	p, _ := chatReplay(t, append(chatStream[:3:3],
		event{"", `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[],"usage":{"total_tokens":9}}`},
		event{"", "[DONE]"}))

	text, resp, err := askChat(t, chatRequest(p))

	require.NoError(t, err)
	assert.Equal(t, "A pod is a group.", text)
	assert.Equal(t, "stop", resp.StopReason)
}

// callFragment is one tool_calls fragment of a delta, at the index given.
func callFragment(index int, body string) string {
	return `{"tool_calls":[{"index":` + strconv.Itoa(index) + `,` + body + `}]}`
}

// chatToolStream is a recorded reply that says a word, then asks for a call whose
// arguments arrive across two more fragments.
var chatToolStream = []event{
	chunk(`{"role":"assistant","content":"Let me check."}`, "null"),
	chunk(callFragment(0, `"id":"call_1","type":"function","function":{"name":"list_objects","arguments":""}`), "null"),
	chunk(callFragment(0, `"function":{"arguments":"{\"resource\":"}`), "null"),
	chunk(callFragment(0, `"function":{"arguments":"\"pods\"}"}`), "null"),
	chunk(`{}`, `"tool_calls"`),
	{"", "[DONE]"},
}

// chatMalformedCall is a call that ended on tool_calls with arguments that are
// not an object.
var chatMalformedCall = []event{
	chatToolStream[1],
	chunk(callFragment(0, `"function":{"arguments":"[1,\"MARKER\"]"}`), "null"),
	chunk(`{}`, `"tool_calls"`),
	{"", "[DONE]"},
}

// The definitions go on the request as function tools, the schema as written and
// no strict: unset is false where the field is known and absent where a vendor
// does not know it.
func TestChatCompletionsOffersTheToolsItIsHanded(t *testing.T) {
	p, rec := chatReplay(t, chatToolStream)
	req := chatRequest(p)
	req.Tools = []ToolDefinition{listObjects}

	_, _, err := askChat(t, req)

	require.NoError(t, err)
	assert.Equal(t, []any{map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": "list_objects", "description": "List one kind.",
			"parameters": map[string]any{
				"type":       "object",
				"properties": map[string]any{"resource": map[string]any{"type": "string"}},
				"required":   []any{"resource"},
			},
		},
	}}, rec.body["tools"])

	none, noneRec := chatReplay(t, chatStream)
	_, _, err = askChat(t, chatRequest(none))
	require.NoError(t, err)
	assert.NotContains(t, noneRec.body, "tools")
}

// A schema that is not JSON fails the send, naming the tool.
func TestChatCompletionsRefusesAToolItCannotEncode(t *testing.T) {
	p, rec := chatReplay(t, chatStream)
	req := chatRequest(p)
	req.Tools = []ToolDefinition{{Name: "broken", InputSchema: json.RawMessage(`{`)}}

	_, _, err := askChat(t, req)

	assert.ErrorContains(t, err, "broken")
	assert.Zero(t, rec.calls.Load())
}

// A call arrives as fragments at one index: the first opens it, the rest append
// their arguments. No chunk carries any of it, and a plain call keeps no payload.
func TestChatCompletionsFoldsACallFromItsFragments(t *testing.T) {
	p, _ := chatReplay(t, chatToolStream)

	text, resp, err := askChat(t, chatRequest(p))

	require.NoError(t, err)
	assert.Equal(t, "Let me check.", text)
	assert.Equal(t, []Block{
		TextBlock("Let me check."),
		ToolUseBlock("call_1", "list_objects", json.RawMessage(`{"resource":"pods"}`)),
	}, resp.Blocks)
	assert.Equal(t, StopToolUse, resp.StopReason)
}

// A vendor that sends a call whole in one fragment, and says stop with it, is a
// model that finished asking.
func TestChatCompletionsReadsACallSentWhole(t *testing.T) {
	p, _ := chatReplay(t, []event{
		chunk(callFragment(0, `"id":"call_1","type":"function","function":{"name":"list_objects","arguments":"{\"resource\":\"pods\"}"}`), "null"),
		chunk(`{}`, `"stop"`),
		{"", "[DONE]"},
	})

	_, resp, err := askChat(t, chatRequest(p))

	require.NoError(t, err)
	assert.Equal(t, []Block{ToolUseBlock("call_1", "list_objects", json.RawMessage(`{"resource":"pods"}`))}, resp.Blocks)
	assert.Equal(t, StopToolUse, resp.StopReason)
}

// The record needs an id to pair a result, and a server that sent none takes any
// back.
func TestChatCompletionsMintsAnIDForACallSentWithoutOne(t *testing.T) {
	p, _ := chatReplay(t, []event{
		chunk(`{"tool_calls":[{"function":{"name":"list_objects","arguments":"{}"}}]}`, "null"),
		chunk(`{}`, `"tool_calls"`),
		{"", "[DONE]"},
	})

	_, resp, err := askChat(t, chatRequest(p))

	require.NoError(t, err)
	assert.Equal(t, "call-1", resp.Blocks[0].ID)
}

// A call the cap cut off inside its arguments keeps the vendor's own finish, and
// the loop answers it not-run.
func TestChatCompletionsKeepsLengthOnACutOffCall(t *testing.T) {
	p, _ := chatReplay(t, []event{
		chatToolStream[1], chatToolStream[2],
		chunk(`{}`, `"length"`),
		{"", "[DONE]"},
	})

	_, resp, err := askChat(t, chatRequest(p))

	require.NoError(t, err)
	assert.Equal(t, "length", resp.StopReason)
	assert.Equal(t, `{}`, string(resp.Blocks[0].Input))
}

// A filtered reply keeps the vendor's word for why it ended, call and all: the
// loop answers a call under it not-run, so a call in a filtered reply never runs.
func TestChatCompletionsKeepsAFilteredFinish(t *testing.T) {
	p, _ := chatReplay(t, []event{
		chatToolStream[1], chatToolStream[2], chatToolStream[3],
		chunk(`{}`, `"content_filter"`),
		{"", "[DONE]"},
	})

	_, resp, err := askChat(t, chatRequest(p))

	require.NoError(t, err)
	assert.Equal(t, "content_filter", resp.StopReason)
	assert.Equal(t, ToolUseBlock("call_1", "list_objects", json.RawMessage(`{"resource":"pods"}`)), resp.Blocks[0])
}

// signedFragment is Gemini's opening fragment: the call, and a thought signature
// of the vendor's own beside it.
const signedFragment = `"id":"call_1","type":"function","function":{"name":"list_objects","arguments":""},` +
	`"extra_content":{"google":{"thought_signature":"c2ln"}}`

// A vendor's own data on a call goes back with it: the call's payload is the folded
// entry — the arguments as they accumulated, not the opening fragment's empty
// ones — with no index, which addresses the stream and not the call.
func TestChatCompletionsKeepsASignedCall(t *testing.T) {
	p, _ := chatReplay(t, []event{
		chunk(callFragment(0, signedFragment), "null"),
		chatToolStream[2], chatToolStream[3],
		chunk(`{}`, `"tool_calls"`),
		{"", "[DONE]"},
	})

	_, resp, err := askChat(t, chatRequest(p))

	require.NoError(t, err)
	require.Len(t, resp.Blocks, 1)
	assert.JSONEq(t, `{"id":"call_1","type":"function","function":{"name":"list_objects","arguments":"{\"resource\":\"pods\"}"},`+
		`"extra_content":{"google":{"thought_signature":"c2ln"}}}`, string(resp.Blocks[0].Payload))
	assert.NotContains(t, string(resp.Blocks[0].Payload), "index")

	row := append(slices.Clone(resp.Blocks), ToolResultBlock("call_1", "2", false))
	next, nextRec := chatReplay(t, chatStream)
	_, _, err = askChat(t, chatRequest(next, Message{Role: "assistant", Blocks: row, ProviderID: "groq", Dialect: DialectChatCompletions}))
	require.NoError(t, err)
	sent := nextRec.body["messages"].([]any)[1].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	assert.Equal(t, `{"resource":"pods"}`, sent["function"].(map[string]any)["arguments"])
	assert.Contains(t, sent, "extra_content")
}

// A call the cap cut off keeps no payload, signed or not: the payload would resend the
// arguments the record repaired.
func TestChatCompletionsACutOffSignedCallKeepsNoPayload(t *testing.T) {
	p, _ := chatReplay(t, []event{
		chunk(callFragment(0, signedFragment), "null"),
		chatToolStream[2],
		chunk(`{}`, `"length"`),
		{"", "[DONE]"},
	})

	_, resp, err := askChat(t, chatRequest(p))

	require.NoError(t, err)
	assert.Equal(t, `{}`, string(resp.Blocks[0].Input))
	assert.Nil(t, resp.Blocks[0].Payload)
}

// {} is a legitimate input a tool runs on its defaults, so a call the model made
// with one keeps its payload like any other.
func TestChatCompletionsASignedCallWithEmptyInputKeepsItsPayload(t *testing.T) {
	p, _ := chatReplay(t, []event{
		chunk(callFragment(0, signedFragment), "null"),
		chunk(callFragment(0, `"function":{"arguments":"{}"}`), "null"),
		chunk(`{}`, `"tool_calls"`),
		{"", "[DONE]"},
	})

	_, resp, err := askChat(t, chatRequest(p))

	require.NoError(t, err)
	assert.Equal(t, `{}`, string(resp.Blocks[0].Input))
	assert.Contains(t, string(resp.Blocks[0].Payload), "thought_signature")
}

// chatShape is what a rendering looks like on the wire: each message's role, the
// ids of the calls it carries, and the call a tool message answers.
func chatShape(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, err := json.Marshal(body["messages"])
	require.NoError(t, err)
	var decoded []struct {
		Role      string `json:"role"`
		Content   any    `json:"content"`
		ToolCalls []struct {
			ID string `json:"id"`
		} `json:"tool_calls"`
		ToolCallID string `json:"tool_call_id"`
	}
	require.NoError(t, json.Unmarshal(raw, &decoded))
	var out []string
	for _, m := range decoded {
		line := m.Role
		if m.Content != nil {
			line += ":content"
		}
		for _, c := range m.ToolCalls {
			line += " tool_call(" + c.ID + ")"
		}
		if m.ToolCallID != "" {
			line += "(" + m.ToolCallID + ")"
		}
		out = append(out, line)
	}
	return out
}

// An assistant row holding a round renders as the wire's own shape: the run of
// text and the calls in one assistant message, each result its own tool message,
// and what is left after them an assistant message of its own. Nothing of the
// thinking is sent, and it does not split a run.
func TestChatCompletionsReplaysARoundAsToolCallsAndToolMessages(t *testing.T) {
	p, rec := chatReplay(t, chatStream)
	row := []Block{
		ThinkingBlock("I should list them."),
		TextBlock("Let me check."),
		ToolUseBlock("call_1", "list_objects", json.RawMessage(`{"resource":"pods"}`)),
		ToolResultBlock("call_1", "2 pods", false),
		TextBlock("Two."),
	}

	_, _, err := askChat(t, chatRequest(p,
		Message{Role: "user", Blocks: []Block{TextBlock("how many?")}},
		Message{Role: "assistant", Blocks: row, ProviderID: "groq", Dialect: DialectChatCompletions},
	))

	require.NoError(t, err)
	assert.Equal(t, []string{
		"system:content", "user:content",
		"assistant:content tool_call(call_1)", "tool:content(call_1)", "assistant:content",
	}, chatShape(t, rec.body))
	msgs := rec.body["messages"].([]any)
	assert.Equal(t, "Let me check.", msgs[2].(map[string]any)["content"])
	call := msgs[2].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	assert.Equal(t, map[string]any{
		"id": "call_1", "type": "function",
		"function": map[string]any{"name": "list_objects", "arguments": `{"resource":"pods"}`},
	}, call)
	assert.Equal(t, "2 pods", msgs[3].(map[string]any)["content"])
}

// Two calls the model asked in one breath stay one assistant message, and their
// results are two tool messages after it, in the order they were opened.
func TestChatCompletionsFoldsTwoCallsInterleaved(t *testing.T) {
	p, _ := chatReplay(t, []event{
		chunk(callFragment(0, `"id":"call_1","type":"function","function":{"name":"list_objects","arguments":""}`), "null"),
		chunk(callFragment(1, `"id":"call_2","type":"function","function":{"name":"list_objects","arguments":""}`), "null"),
		chunk(callFragment(0, `"function":{"arguments":"{\"resource\":\"pods\"}"}`), "null"),
		chunk(callFragment(1, `"function":{"arguments":"{\"resource\":\"nodes\"}"}`), "null"),
		chunk(`{}`, `"tool_calls"`),
		{"", "[DONE]"},
	})

	_, resp, err := askChat(t, chatRequest(p))

	require.NoError(t, err)
	assert.Equal(t, []Block{
		ToolUseBlock("call_1", "list_objects", json.RawMessage(`{"resource":"pods"}`)),
		ToolUseBlock("call_2", "list_objects", json.RawMessage(`{"resource":"nodes"}`)),
	}, resp.Blocks)

	row := append(slices.Clone(resp.Blocks), ToolResultBlock("call_1", "2", false), ToolResultBlock("call_2", "3", false))
	next, nextRec := chatReplay(t, chatStream)
	_, _, err = askChat(t, chatRequest(next, Message{Role: "assistant", Blocks: row, ProviderID: "groq", Dialect: DialectChatCompletions}))

	require.NoError(t, err)
	assert.Equal(t, []string{
		"system:content", "assistant tool_call(call_1) tool_call(call_2)",
		"tool:content(call_1)", "tool:content(call_2)",
	}, chatShape(t, nextRec.body))
}

// A call with a payload goes back as the entry the vendor sent, signature and all, and
// another provider's is rebuilt from the fields alone.
func TestChatCompletionsReplaysASignedCallVerbatim(t *testing.T) {
	call := ToolUseBlock("call_1", "list_objects", json.RawMessage(`{"resource":"pods"}`))
	call.Payload = json.RawMessage(`{"id":"call_1","type":"function","function":{"name":"list_objects","arguments":"{\"resource\":\"pods\"}"},` +
		`"extra_content":{"google":{"thought_signature":"c2ln"}}}`)
	row := []Block{call, ToolResultBlock("call_1", "2", false)}

	p, rec := chatReplay(t, chatStream)
	_, _, err := askChat(t, chatRequest(p, Message{Role: "assistant", Blocks: row, ProviderID: "groq", Dialect: DialectChatCompletions}))
	require.NoError(t, err)
	sent := rec.body["messages"].([]any)[1].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	assert.Equal(t, map[string]any{"google": map[string]any{"thought_signature": "c2ln"}}, sent["extra_content"])

	other, otherRec := chatReplay(t, chatStream)
	_, _, err = askChat(t, chatRequest(other, Message{Role: "assistant", Blocks: row, ProviderID: "gemini", Dialect: DialectChatCompletions}))
	require.NoError(t, err)
	rebuilt := otherRec.body["messages"].([]any)[1].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	assert.NotContains(t, rebuilt, "extra_content")
	assert.Regexp(t, `^[A-Za-z0-9]{9}$`, rebuilt["id"], "a foreign call goes under this wire's own id")
}

// A signed call this provider wrote on another dialect is rebuilt from the
// fields alone: the payload is keyed on the dialect too.
func TestChatCompletionsKeysASignedCallOnTheDialect(t *testing.T) {
	call := ToolUseBlock("call_1", "list_objects", json.RawMessage(`{"resource":"pods"}`))
	call.Payload = json.RawMessage(`{"id":"call_1","type":"function","function":{"name":"list_objects","arguments":"{\"resource\":\"pods\"}"},` +
		`"extra_content":{"google":{"thought_signature":"c2ln"}}}`)
	row := []Block{call, ToolResultBlock("call_1", "2", false)}

	p, rec := chatReplay(t, chatStream)
	_, _, err := askChat(t, chatRequest(p, Message{Role: "assistant", Blocks: row, ProviderID: "groq", Dialect: DialectResponses}))
	require.NoError(t, err)
	rebuilt := rec.body["messages"].([]any)[1].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	assert.NotContains(t, rebuilt, "extra_content")
}

// A vendor may stream a call's name in pieces and send its id after the fragment
// that opened it: the record needs the whole name to look the tool up, and the
// id the server gave over one this wire minted.
func TestChatCompletionsFoldsANameAndALateID(t *testing.T) {
	p, _ := chatReplay(t, []event{
		chunk(callFragment(0, `"type":"function","function":{"name":"list_","arguments":""}`), "null"),
		chunk(callFragment(0, `"id":"call_9","function":{"name":"objects","arguments":"{}"}`), "null"),
		chunk(`{}`, `"tool_calls"`),
		{"", "[DONE]"},
	})

	_, resp, err := askChat(t, chatRequest(p))

	require.NoError(t, err)
	require.Len(t, resp.Blocks, 1)
	assert.Equal(t, "list_objects", resp.Blocks[0].Name)
	assert.Equal(t, "call_9", resp.Blocks[0].ID)
}

// A vendor may hang its own data on any fragment of a call, not just the one
// that opened it: a signature that arrives after the arguments rides back with
// the call like any other.
func TestChatCompletionsKeepsASignatureThatArrivesLate(t *testing.T) {
	p, _ := chatReplay(t, []event{
		chatToolStream[1], chatToolStream[2], chatToolStream[3],
		chunk(callFragment(0, `"extra_content":{"google":{"thought_signature":"c2ln"}}`), "null"),
		chunk(`{}`, `"tool_calls"`),
		{"", "[DONE]"},
	})

	_, resp, err := askChat(t, chatRequest(p))

	require.NoError(t, err)
	require.Len(t, resp.Blocks, 1)
	assert.Contains(t, string(resp.Blocks[0].Payload), "thought_signature")
	assert.JSONEq(t, `{"resource":"pods"}`, string(resp.Blocks[0].Input))
}

// A round another provider wrote carries that vendor's ids, and Mistral takes
// only nine alphanumerics: the walk replays every foreign call under an id of
// this wire's own, the same on the call and on the result that answers it. A row
// this provider wrote keeps its ids, and the record keeps them either way.
func TestChatCompletionsReplaysAForeignRoundUnderItsOwnIDs(t *testing.T) {
	row := []Block{
		ToolUseBlock("toolu_01ABCDEFG", "list_objects", json.RawMessage(`{"resource":"pods"}`)),
		ToolResultBlock("toolu_01ABCDEFG", "2 pods", false),
		TextBlock("Two."),
	}

	p, rec := chatReplay(t, chatStream)
	_, _, err := askChat(t, chatRequest(p, Message{Role: "assistant", Blocks: row, ProviderID: "anthropic", Dialect: DialectChatCompletions}))
	require.NoError(t, err)
	msgs := rec.body["messages"].([]any)
	minted := msgs[1].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["id"].(string)
	assert.Regexp(t, `^[A-Za-z0-9]{9}$`, minted, "nine alphanumerics, whatever the writer spelled")
	assert.Equal(t, minted, msgs[2].(map[string]any)["tool_call_id"], "the result answers the id the call went under")
	assert.Equal(t, "toolu_01ABCDEFG", row[0].ID, "the record keeps the writer's own")

	own, ownRec := chatReplay(t, chatStream)
	_, _, err = askChat(t, chatRequest(own, Message{Role: "assistant", Blocks: row, ProviderID: "groq", Dialect: DialectChatCompletions}))
	require.NoError(t, err)
	ownMsgs := ownRec.body["messages"].([]any)
	assert.Equal(t, "toolu_01ABCDEFG", ownMsgs[1].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["id"])
	assert.Equal(t, "toolu_01ABCDEFG", ownMsgs[2].(map[string]any)["tool_call_id"])
}

// The minted ids are the call's place in the conversation, so a turn's rendering
// is still the leading slice of the next's.
func TestChatCompletionsMintedIDsHoldStillAsTheChatGrows(t *testing.T) {
	round := func(id string) []Block {
		return []Block{
			ToolUseBlock(id, "list_objects", json.RawMessage(`{"resource":"pods"}`)),
			ToolResultBlock(id, "2 pods", false),
		}
	}
	first := Message{Role: "assistant", Blocks: round("toolu_1"), ProviderID: "anthropic", Dialect: DialectChatCompletions}
	second := Message{Role: "assistant", Blocks: round("toolu_2"), ProviderID: "anthropic", Dialect: DialectChatCompletions}

	p, rec := chatReplay(t, chatStream)
	_, _, err := askChat(t, chatRequest(p, first))
	require.NoError(t, err)

	next, nextRec := chatReplay(t, chatStream)
	_, _, err = askChat(t, chatRequest(next, first, second))
	require.NoError(t, err)

	before, after := rec.body["messages"].([]any), nextRec.body["messages"].([]any)
	require.Less(t, len(before), len(after))
	for i, msg := range before {
		assert.Equal(t, msg, after[i], "message %d moved", i)
	}
}

// A result whose call is not in the conversation answers under its own id: there
// is nothing to pair it with, and the record's id is all there is.
func TestChatCompletionsAnswersAnOrphanResultUnderItsOwnID(t *testing.T) {
	p, rec := chatReplay(t, chatStream)
	row := []Block{ToolResultBlock("toolu_01ABCDEFG", "2 pods", false)}

	_, _, err := askChat(t, chatRequest(p, Message{Role: "assistant", Blocks: row, ProviderID: "anthropic", Dialect: DialectChatCompletions}))

	require.NoError(t, err)
	assert.Equal(t, "toolu_01ABCDEFG", rec.body["messages"].([]any)[1].(map[string]any)["tool_call_id"])
}

// Two calls of one message are two ids on the wire, and each result answers the
// one it belongs to.
func TestChatCompletionsMintsOneIDPerForeignCall(t *testing.T) {
	p, rec := chatReplay(t, chatStream)
	row := []Block{
		ToolUseBlock("toolu_1", "list_objects", json.RawMessage(`{"resource":"pods"}`)),
		ToolUseBlock("toolu_2", "list_objects", json.RawMessage(`{"resource":"nodes"}`)),
		ToolResultBlock("toolu_1", "2 pods", false),
		ToolResultBlock("toolu_2", "3 nodes", false),
	}

	_, _, err := askChat(t, chatRequest(p, Message{Role: "assistant", Blocks: row, ProviderID: "anthropic", Dialect: DialectChatCompletions}))

	require.NoError(t, err)
	msgs := rec.body["messages"].([]any)
	calls := msgs[1].(map[string]any)["tool_calls"].([]any)
	require.Len(t, calls, 2)
	first, second := calls[0].(map[string]any)["id"], calls[1].(map[string]any)["id"]
	assert.NotEqual(t, first, second, "one id per call")
	assert.Equal(t, first, msgs[2].(map[string]any)["tool_call_id"])
	assert.Equal(t, second, msgs[3].(map[string]any)["tool_call_id"])
}

// This wire's reader mints call-<n> per reply, so a row of two rounds can hold
// two different calls under one record id. Each call is its own on the wire —
// including the one that follows a repeat — and each result answers the call it
// followed.
func TestChatCompletionsMintsAnIDPerCallAcrossRounds(t *testing.T) {
	p, rec := chatReplay(t, chatStream)
	row := []Block{
		ToolUseBlock("call-1", "list_objects", json.RawMessage(`{"resource":"pods"}`)),
		ToolResultBlock("call-1", "2 pods", false),
		ToolUseBlock("call-1", "list_objects", json.RawMessage(`{"resource":"nodes"}`)),
		ToolUseBlock("call-2", "list_objects", json.RawMessage(`{"resource":"jobs"}`)),
		ToolResultBlock("call-1", "3 nodes", false),
		ToolResultBlock("call-2", "1 job", false),
	}

	_, _, err := askChat(t, chatRequest(p, Message{Role: "assistant", Blocks: row, ProviderID: "anthropic", Dialect: DialectChatCompletions}))

	require.NoError(t, err)
	msgs := rec.body["messages"].([]any)
	first := msgs[1].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["id"]
	round := msgs[3].(map[string]any)["tool_calls"].([]any)
	require.Len(t, round, 2)
	second, third := round[0].(map[string]any)["id"], round[1].(map[string]any)["id"]
	assert.NotEqual(t, first, second, "a call after a repeat is not the repeat")
	assert.NotEqual(t, second, third, "one id per call")
	assert.Equal(t, first, msgs[2].(map[string]any)["tool_call_id"])
	assert.Equal(t, second, msgs[4].(map[string]any)["tool_call_id"])
	assert.Equal(t, third, msgs[5].(map[string]any)["tool_call_id"])
}

// The collision this wire mints against is its own reader's: call-<n> restarts
// per reply, so a row this provider wrote can hold two calls under one id. The
// second replays under an id of the wire's own, whoever wrote the row.
func TestChatCompletionsMintsOverItsOwnRepeatedIDs(t *testing.T) {
	p, rec := chatReplay(t, chatStream)
	row := []Block{
		ToolUseBlock("call-1", "list_objects", json.RawMessage(`{"resource":"pods"}`)),
		ToolResultBlock("call-1", "2 pods", false),
		ToolUseBlock("call-1", "list_objects", json.RawMessage(`{"resource":"nodes"}`)),
		ToolResultBlock("call-1", "3 nodes", false),
	}

	_, _, err := askChat(t, chatRequest(p, Message{Role: "assistant", Blocks: row, ProviderID: "groq", Dialect: DialectChatCompletions}))

	require.NoError(t, err)
	msgs := rec.body["messages"].([]any)
	first := msgs[1].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["id"]
	second := msgs[3].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["id"]
	assert.Equal(t, "call-1", first, "the first keeps the record's id")
	assert.NotEqual(t, first, second, "the repeat replays under an id of this wire's own")
	assert.Equal(t, first, msgs[2].(map[string]any)["tool_call_id"])
	assert.Equal(t, second, msgs[4].(map[string]any)["tool_call_id"])
}

// A vendor may send parallel calls whole, each with its own id and no index at
// all, which reads as 0 for every one of them: an id the current call does not
// carry opens the next call, so two arrive as two.
func TestChatCompletionsReadsParallelCallsSentWithoutAnIndex(t *testing.T) {
	p, _ := chatReplay(t, []event{
		{"", `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[` +
			`{"id":"call_1","type":"function","function":{"name":"list_objects","arguments":"{\"resource\":\"pods\"}"}},` +
			`{"id":"call_2","type":"function","function":{"name":"list_objects","arguments":"{\"resource\":\"nodes\"}"}}]},"finish_reason":null}]}`},
		chunk(`{}`, `"tool_calls"`),
		{"", "[DONE]"},
	})

	_, resp, err := askChat(t, chatRequest(p))

	require.NoError(t, err)
	assert.Equal(t, []Block{
		ToolUseBlock("call_1", "list_objects", json.RawMessage(`{"resource":"pods"}`)),
		ToolUseBlock("call_2", "list_objects", json.RawMessage(`{"resource":"nodes"}`)),
	}, resp.Blocks)
}

// A vendor that sends no ids but keys of its own stores call-1 in each reply, and
// the entry it keeps names that id. The second call goes under a minted id, and
// so does its entry, the vendor's key kept, so the entry and its result pair up.
func TestChatCompletionsMintsASpentIDInsideItsPayload(t *testing.T) {
	row := func() Message {
		return Message{Role: "assistant", ProviderID: "groq", Dialect: DialectChatCompletions, Blocks: []Block{
			withPayload(ToolUseBlock("call-1", "list_objects", json.RawMessage(`{"resource":"pods"}`)),
				`{"id":"call-1","type":"function","function":{"name":"list_objects","arguments":"{\"resource\":\"pods\"}"},"extra_content":{"google":{"thought_signature":"c2ln"}}}`),
			ToolResultBlock("call-1", "2 pods", false),
		}}
	}
	question := Message{Role: "user", Blocks: []Block{TextBlock("and now?")}}

	p, rec := chatReplay(t, chatStream)
	_, _, err := askChat(t, chatRequest(p, question, row(), question, row()))
	require.NoError(t, err)

	msgs := rec.body["messages"].([]any)
	first := msgs[2].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	second := msgs[5].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	assert.Equal(t, "call-1", first["id"])
	assert.Equal(t, "call-1", msgs[3].(map[string]any)["tool_call_id"])
	assert.Equal(t, "call00001", second["id"])
	assert.Contains(t, second, "extra_content", "the vendor's key goes with the entry")
	assert.Equal(t, "call00001", msgs[6].(map[string]any)["tool_call_id"])
}

// A code the row defines is kept on this wire too.
func TestChatCompletionsKeepsTheCodesItsRowDefines(t *testing.T) {
	p := groqAt(newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"type":"rate_limit_error","code":"subscription_sharing_usage_limit_exceeded","message":"MARKER"}}`)
	}))
	p.Codes = map[string]CodeClass{"subscription_sharing_usage_limit_exceeded": CodeUsageLimit}

	_, err := streamChatCompletions(t.Context(), chatRequest(p), testutil.Timeout, wallClock, func(Chunk) {})

	var llmErr *Error
	require.ErrorAs(t, err, &llmErr)
	assert.True(t, llmErr.UsageLimit())
}
