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
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// sdkRoots is each model SDK's root package, by the name a file imports it
// under when it gives none.
var sdkRoots = map[string]string{
	anthropicSDK: "anthropic",
	openaiSDK:    "openai",
}

// The model SDKs the module depends on, by root package; each is also the
// prefix of every package it holds.
const (
	anthropicSDK = "github.com/anthropics/anthropic-sdk-go"
	openaiSDK    = "github.com/openai/openai-go/v3"
)

// sdkFile reports a file of llm's allowed to import a model SDK: a dialect, or
// the helpers the dialects share.
func sdkFile(name string) bool {
	return strings.HasPrefix(name, "dialect_") || strings.HasPrefix(name, "helpers")
}

// wireFiles is the file a tool binds each dialect's wire in, and its test.
var wireFiles = map[string]bool{
	"messages.go": true, "messages_test.go": true,
	"responses.go": true, "responses_test.go": true,
	"chatcompletions.go": true, "chatcompletions_test.go": true,
}

// wireFile reports whether file, a path under sidecar/, is a tool's wire file
// or its test.
func wireFile(file string) bool {
	dir, name := path.Split(file)
	return path.Dir(path.Clean(dir)) == "internal/tools" && wireFiles[name]
}

// isSDK reports whether imp is a model SDK's package.
func isSDK(imp string) bool {
	return sdkOf(imp) != ""
}

// sdkOf is the root of the model SDK holding imp, or "" for none.
func sdkOf(imp string) string {
	for sdk := range sdkRoots {
		if imp == sdk || strings.HasPrefix(imp, sdk+"/") {
			return sdk
		}
	}
	return ""
}

// sdkImportAllowed reports whether file, a path under sidecar/, may import imp,
// a model SDK's package: any of it from llm's dialect files and helpers, and
// its types alone, the root package and shared/…, from a tool's wire file and
// its test. The option package is the client's, so no tool imports it.
func sdkImportAllowed(file, imp string) bool {
	if dir, name := path.Split(file); dir == "internal/llm/" {
		return sdkFile(name)
	}
	if !wireFile(file) {
		return false
	}
	rest := strings.TrimPrefix(imp, sdkOf(imp))
	return rest == "" || rest == "/shared" || strings.HasPrefix(rest, "/shared/")
}

// eachGoFile calls fn with every Go file of the module, parsed, and its path
// under sidecar/.
func eachGoFile(t *testing.T, mode parser.Mode, fn func(file string, f *ast.File)) {
	t.Helper()
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, mode)
		if err != nil {
			return err
		}
		fn(filepath.ToSlash(rel), f)
		return nil
	})
	require.NoError(t, err)
}

// Inside llm a dialect file or the helpers may import a model SDK whole; a
// tool's wire file and its test may import its types alone; nothing else may
// import one.
func TestAWireFileMayImportAModelSDKsTypes(t *testing.T) {
	allowed := [][2]string{
		{"internal/llm/dialect_messages.go", anthropicSDK + "/option"},
		{"internal/llm/helpers_test.go", openaiSDK},
		{"internal/tools/anthropicwebsearch/messages.go", anthropicSDK},
		{"internal/tools/anthropicwebsearch/messages_test.go", anthropicSDK + "/shared/constant"},
		{"internal/tools/openaiwebsearch/responses.go", openaiSDK + "/shared"},
	}
	for _, c := range allowed {
		assert.True(t, sdkImportAllowed(c[0], c[1]), c)
	}
	refused := [][2]string{
		{"internal/llm/request.go", anthropicSDK},
		{"internal/llm/model.go", openaiSDK},
		{"internal/tools/anthropicwebsearch/anthropicwebsearch.go", anthropicSDK},
		{"internal/tools/anthropicwebsearch/messages.go", anthropicSDK + "/option"},
		{"internal/tools/anthropicwebsearch/messages_test.go", anthropicSDK + "/packages/ssestream"},
		{"internal/agent/chat/messages.go", anthropicSDK},
		{"internal/tools/messages.go", anthropicSDK},
	}
	for _, c := range refused {
		assert.False(t, sdkImportAllowed(c[0], c[1]), c)
	}
}

// A model SDK is imported where sdkImportAllowed says and nowhere else in the
// module: llm builds every client, and a tool's wire file spells its binding in
// the SDK's types, so the file's name says where a vendor's wire is spoken.
func TestOnlyAWireFileImportsAModelSDK(t *testing.T) {
	eachGoFile(t, parser.ImportsOnly, func(file string, f *ast.File) {
		for _, imp := range f.Imports {
			pkg, err := strconv.Unquote(imp.Path.Value)
			require.NoError(t, err, file)
			if isSDK(pkg) && !sdkImportAllowed(file, pkg) {
				t.Errorf("%s imports %s: a model SDK is for llm's dialect files, or a tool's wire file for its types", file, pkg)
			}
		}
	})
}

// wire is one real dialect as the conformance cases reach it: what it is called,
// the function that speaks it, a server replaying a canned reply and the three
// replies it has, a way to serve a handler of the case's own, the request the
// case sends, and readers of the conversation and the effort off a recorded body.
// The fake is not one of them: it has no transport, no body and no idle guard,
// and what it shares with the wires its own tests pin.
type wire struct {
	name   string
	stream func(context.Context, Request, time.Duration, idleClock, func(Chunk)) (Response, error)
	replay func(*testing.T, []event) (Provider, *recorded)
	serve  func(*testing.T, http.HandlerFunc) Provider
	at     func(baseURL string) Provider
	req    func(p Provider, msgs ...Message) Request
	// The canned replies: text alone, a thought then text, one that thinks again
	// after its first text, and one that says a word and then asks for a call.
	// head is the opening of text, up to and including its first text chunk: what
	// a case that must cancel mid-answer serves before it parks.
	text, thinks, thinksAgain, head, call []event
	// malformed is call with the call's arguments replaced by an array, under the
	// same normal end.
	malformed []event
	// tools is the offer the body carries, and toolName the name one entry of it
	// holds.
	tools    func(body map[string]any) []any
	toolName func(entry any) string
	// convo is the conversation the body carries, one element per message, each
	// as serialized; conversationKey is where it sits in the body.
	convo           func(body map[string]any) []any
	conversationKey string
	// pairs is every call id and every result id the conversation carries, each in
	// the order sent.
	pairs func(body map[string]any) (calls, results []string)
	// effort is the level the body asked to think at, empty where it asked for none.
	effort func(body map[string]any) string
	// ownKey is every value of the header the dialect sends its key in.
	ownKey func(http.Header) []string
	// sent is a body path the wire sends on every request.
	sent string
	// reported is a text reply whose usage counts every field the wire reads,
	// usage what it reads back as, and unreported a text reply with no usage.
	reported, unreported []event
	usage                Usage
	// served is the model text says the provider served.
	served string
}

func wires() []wire {
	return []wire{
		{
			name:            "messages",
			stream:          streamMessages,
			replay:          replay,
			serve:           serve,
			at:              anthropicAt,
			req:             request,
			text:            textStream,
			thinks:          thinkingStream,
			thinksAgain:     thinksAgainStream,
			head:            textStream[:3],
			call:            toolStream,
			malformed:       messagesMalformedCall,
			tools:           func(body map[string]any) []any { return body["tools"].([]any) },
			toolName:        func(entry any) string { return entry.(map[string]any)["name"].(string) },
			convo:           func(body map[string]any) []any { return body["messages"].([]any) },
			conversationKey: "messages",
			pairs:           messagesPairs,
			effort:          func(body map[string]any) string { return nested(body, "output_config", "effort") },
			ownKey:          func(h http.Header) []string { return h.Values("X-Api-Key") },
			sent:            "max_tokens",
			reported:        messagesReported,
			unreported:      messagesUnreported,
			usage:           Usage{Reported: true, InputTokens: 35, CacheReadTokens: 20, CacheWriteTokens: 5, OutputTokens: 7},
			served:          "claude-haiku-4-5-20251001",
		},
		{
			name:            "responses",
			stream:          streamResponses,
			replay:          responsesReplay,
			serve:           func(t *testing.T, h http.HandlerFunc) Provider { return openAIAt(newServer(t, h)) },
			at:              openAIAt,
			req:             responsesRequest,
			text:            responsesStream,
			thinks:          responsesThinkingStream,
			thinksAgain:     responsesThinksAgainStream,
			head:            responsesStream[:2],
			call:            responsesToolStream,
			malformed:       responsesMalformedCall,
			tools:           func(body map[string]any) []any { return body["tools"].([]any) },
			toolName:        func(entry any) string { return entry.(map[string]any)["name"].(string) },
			convo:           func(body map[string]any) []any { return body["input"].([]any) },
			conversationKey: "input",
			pairs:           responsesPairs,
			effort:          func(body map[string]any) string { return nested(body, "reasoning", "effort") },
			ownKey:          func(h http.Header) []string { return h.Values("Authorization") },
			sent:            "store",
			reported:        responsesReported,
			unreported:      responsesStream,
			usage:           Usage{Reported: true, InputTokens: 35, CacheReadTokens: 20, OutputTokens: 7},
			served:          "gpt-5-mini-2025-08-07",
		},
		{
			name:        "chatcompletions",
			stream:      streamChatCompletions,
			replay:      chatReplay,
			serve:       func(t *testing.T, h http.HandlerFunc) Provider { return groqAt(newServer(t, h)) },
			at:          groqAt,
			req:         chatRequest,
			text:        chatStream,
			thinks:      chatThinkingStream,
			thinksAgain: chatThinksAgainStream,
			head:        chatStream[:1],
			call:        chatToolStream,
			malformed:   chatMalformedCall,
			tools:       func(body map[string]any) []any { return body["tools"].([]any) },
			toolName: func(entry any) string {
				return entry.(map[string]any)["function"].(map[string]any)["name"].(string)
			},
			// The system prompt is the conversation's first message on this wire,
			// so it grows with the rest and is compared with it.
			convo:           func(body map[string]any) []any { return body["messages"].([]any) },
			conversationKey: "messages",
			pairs:           chatPairs,
			effort:          func(body map[string]any) string { return nested(body, "reasoning_effort") },
			ownKey:          func(h http.Header) []string { return h.Values("Authorization") },
			// The fixture's model states a cap, so the field is always there.
			sent:       "max_tokens",
			reported:   chatReported,
			unreported: chatStream,
			usage:      Usage{Reported: true, InputTokens: 35, CacheReadTokens: 20, OutputTokens: 7},
			served:     "m",
		},
	}
}

// eachWire runs fn once per real dialect.
func eachWire(t *testing.T, fn func(*testing.T, wire)) {
	t.Helper()
	for _, w := range wires() {
		t.Run(w.name, func(t *testing.T) { fn(t, w) })
	}
}

// What is streamed is what is answered: the chunks joined are the response's text.
func TestEveryDialectStreamsAReply(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		p, _ := w.replay(t, w.text)
		var text strings.Builder

		resp, err := w.stream(t.Context(), w.req(p), testutil.Timeout, wallClock, func(c Chunk) { text.WriteString(c.Text) })

		require.NoError(t, err)
		require.Len(t, resp.Blocks, 1)
		assert.Equal(t, resp.Blocks[0].Text, text.String())
		assert.NotEmpty(t, resp.StopReason)
	})
}

// The effort a send named reaches the model wherever its wire puts it, and a
// model that lists none asks to think at no level at all.
func TestEveryDialectSendsTheEffortOfAModelThatListsOne(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		p, rec := w.replay(t, w.text)
		req := w.req(p)
		req.Effort = "high"

		_, err := w.stream(t.Context(), req, testutil.Timeout, wallClock, func(Chunk) {})

		require.NoError(t, err)
		assert.Equal(t, "high", w.effort(rec.body))

		plain, plainRec := w.replay(t, w.text)
		_, err = w.stream(t.Context(), w.req(plain), testutil.Timeout, wallClock, func(Chunk) {})
		require.NoError(t, err)
		assert.Empty(t, w.effort(plainRec.body))
	})
}

// What the provider showed of its thinking arrives as its own kind of chunk, and
// the record reads back as what the stream said. It says nothing about the order
// the chunks arrived in, which is the fixture's and not the wire's.
func TestEveryDialectKeepsTheThinkingApartFromTheText(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		p, _ := w.replay(t, w.thinks)
		var chunks []Chunk

		resp, err := w.stream(t.Context(), w.req(p), testutil.Timeout, wallClock, func(c Chunk) { chunks = append(chunks, c) })

		require.NoError(t, err)
		assertRecordReadsTheStream(t, chunks, resp.Blocks)
	})
}

// A reply that thinks again after its first text streams in the order served, and
// the record holds each piece where the reply put it.
func TestEveryDialectReadsTheThinkingInTheRepliesOrder(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		p, _ := w.replay(t, w.thinksAgain)
		var chunks []Chunk

		resp, err := w.stream(t.Context(), w.req(p), testutil.Timeout, wallClock, func(c Chunk) { chunks = append(chunks, c) })

		require.NoError(t, err)
		kinds := make([]ChunkKind, 0, len(chunks))
		for _, c := range chunks {
			kinds = append(kinds, c.Kind)
		}
		assert.Equal(t, []ChunkKind{ChunkThinking, ChunkText, ChunkThinking, ChunkText}, kinds)
		assertRecordReadsTheStream(t, chunks, resp.Blocks)
	})
}

// assertRecordReadsTheStream checks that a reader of the record sees what the
// stream said: the thinking blocks joined are every thinking chunk, and the text
// blocks joined are every text chunk. Where each sits is the reply's, not this
// assertion's.
func assertRecordReadsTheStream(t *testing.T, chunks []Chunk, blocks []Block) {
	t.Helper()
	require.NotEmpty(t, blocks)
	assert.Equal(t, joinKind(chunks, ChunkThinking), Thinking(blocks))
	assert.Equal(t, joinKind(chunks, ChunkText), joinText(blocks))
}

// joinKind is the chunks of one kind, in order, joined.
func joinKind(chunks []Chunk, kind ChunkKind) string {
	var out strings.Builder
	for _, c := range chunks {
		if c.Kind == kind {
			out.WriteString(c.Text)
		}
	}
	return out.String()
}

// joinText is a response's text blocks, in order, joined.
func joinText(blocks []Block) string {
	var out strings.Builder
	for _, b := range blocks {
		if b.Type == BlockText {
			out.WriteString(b.Text)
		}
	}
	return out.String()
}

// The bound is on the wire, so a stream parked after its headers and one that
// stalls before them both end as an idle stream and not as a cancel.
func TestEveryDialectBoundsAnIdleStream(t *testing.T) {
	const bound = time.Minute
	eachWire(t, func(t *testing.T, w wire) {
		t.Run("parked after its headers", func(t *testing.T) {
			parked := testutil.NewSignal()
			p := w.serve(t, func(rw http.ResponseWriter, r *http.Request) {
				rw.Header().Set("Content-Type", "text/event-stream")
				rw.WriteHeader(http.StatusOK)
				rw.(http.Flusher).Flush()
				parked.Fire()
				<-r.Context().Done()
			})
			timer := newFakeTimer()

			done := goStream(func() (Response, error) {
				return w.stream(t.Context(), w.req(p), bound, timer.clock, func(Chunk) {})
			})
			parked.Wait(t, "the headers")
			timer.trip()
			got := testutil.Recv(t, done, "the stream's end")

			assert.ErrorIs(t, got.err, ErrStreamIdle)
			assert.NotErrorIs(t, got.err, context.Canceled)
		})

		t.Run("stalled before its headers", func(t *testing.T) {
			stalled := testutil.NewSignal()
			p := w.serve(t, func(rw http.ResponseWriter, r *http.Request) {
				// The request is read before the stall: a server with a body still
				// unread never notices the client leave, and the handler outlives
				// the test.
				io.Copy(io.Discard, r.Body)
				stalled.Fire()
				<-r.Context().Done()
			})
			timer := newFakeTimer()

			done := goStream(func() (Response, error) {
				return w.stream(t.Context(), w.req(p), bound, timer.clock, func(Chunk) {})
			})
			stalled.Wait(t, "the request")
			timer.trip()
			got := testutil.Recv(t, done, "the stream's end")

			assert.ErrorIs(t, got.err, ErrStreamIdle)
			assert.NotErrorIs(t, got.err, context.Canceled)
		})
	})
}

// The idle clock stops while a request is signed, so a Sign that waits for the
// keyring past the bound does not end the turn; released, the turn streams its
// answer with the clock running again from the full bound. A cancel still ends
// the wait.
func TestTheIdleClockStopsWhileARequestIsSigned(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		t.Run("released", func(t *testing.T) {
			p, _ := w.replay(t, w.text)
			signer := newHeldSigner()
			p.Key, p.Signer = "", signer
			timer := newFakeTimer()

			done := goStream(func() (Response, error) {
				return w.stream(t.Context(), w.req(p), time.Minute, timer.clock, func(Chunk) {})
			})
			signer.waiting.Wait(t, "the signature")
			timer.trip()
			timer.resets.Drain()
			close(signer.release)
			timer.awaitReset(t)
			got := testutil.Recv(t, done, "the stream's end")

			require.NoError(t, got.err)
		})
		t.Run("cancelled", func(t *testing.T) {
			p, _ := w.replay(t, w.text)
			signer := newHeldSigner()
			p.Key, p.Signer = "", signer
			ctx, cancel := context.WithCancel(t.Context())

			done := goStream(func() (Response, error) {
				return w.stream(ctx, w.req(p), time.Minute, newFakeTimer().clock, func(Chunk) {})
			})
			signer.waiting.Wait(t, "the signature")
			cancel()
			got := testutil.Recv(t, done, "the stream's end")

			assert.ErrorIs(t, got.err, context.Canceled)
			assert.NotErrorIs(t, got.err, ErrStreamIdle)
		})
	})
}

// A cancel ends the stream as a cancel, never as an idle stream, with what was
// streamed already emitted.
func TestEveryDialectEndsOnCancel(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		p := w.serve(t, func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Content-Type", "text/event-stream")
			for _, e := range w.head {
				sse(rw, e)
			}
			<-r.Context().Done()
		})
		ctx, cancel := context.WithCancel(t.Context())
		var text strings.Builder

		_, err := w.stream(ctx, w.req(p), testutil.Timeout, wallClock, func(c Chunk) {
			text.WriteString(c.Text)
			cancel()
		})

		assert.ErrorIs(t, err, context.Canceled)
		assert.NotErrorIs(t, err, ErrStreamIdle)
		assert.NotEmpty(t, text.String(), "the text so far is emitted")
	})
}

// A refusal's body can echo the request, so none of it reaches the error.
func TestEveryDialectRendersAFailureWithoutTheBody(t *testing.T) {
	const marker = "MARKER-the-user-said-this"
	eachWire(t, func(t *testing.T, w wire) {
		p := w.serve(t, func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprintf(rw, `{"type":"error","error":{"type":"rate_limit_error","message":%q}}`, marker+" sk-ant-test")
		})

		_, err := w.stream(t.Context(), w.req(p), testutil.Timeout, wallClock, func(Chunk) {})

		require.Error(t, err)
		assert.NotContains(t, err.Error(), marker)
		assert.NotContains(t, err.Error(), "sk-ant-test")
	})
}

// A port nothing listens on got no response at all.
func TestEveryDialectRendersAFailureWithNoResponse(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		addr := ln.Addr().String()
		require.NoError(t, ln.Close())

		_, err = w.stream(t.Context(), w.req(w.at("http://"+addr)), testutil.Timeout, wallClock, func(Chunk) {})

		var llmErr *Error
		require.ErrorAs(t, err, &llmErr)
		assert.Equal(t, "connection failed", llmErr.Kind)
		assert.Zero(t, llmErr.Status)
	})
}

// The cluster card goes ahead of the question's own text, whatever the wire makes
// of a message.
func TestEveryDialectSendsAContextBlockAheadOfTheText(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		p, rec := w.replay(t, w.text)
		req := w.req(p, Message{Role: "user", Blocks: []Block{ContextBlock("the card"), TextBlock("what is a pod?")}})

		_, err := w.stream(t.Context(), req, testutil.Timeout, wallClock, func(Chunk) {})

		require.NoError(t, err)
		sent := fmt.Sprint(w.convo(rec.body))
		assert.Less(t, strings.Index(sent, "the card"), strings.Index(sent, "what is a pod?"))
	})
}

// A provider's own lines reach the request the same way on every wire: the key
// in the header it names and nowhere else, its query on the URL, and each path
// it omits gone from the body, one no body holds included. With the header
// named and no key, no key goes at all, and the body keeps the path the first
// provider omitted, so the omit is of a field the wire really sends.
func TestEveryDialectSendsAProvidersKeyHeaderQueryAndOmits(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		named, rec := w.replay(t, w.text)
		named.KeyHeader = "api-key"
		named.Query = map[string]string{"api-version": "2026-01-01"}
		named.Omit = []string{w.sent, "never.sent"}
		keyless, keylessRec := w.replay(t, w.text)
		keyless.Key, keyless.KeyHeader = "", "api-key"

		for _, p := range []Provider{named, keyless} {
			_, err := w.stream(t.Context(), w.req(p), testutil.Timeout, wallClock, func(Chunk) {})
			require.NoError(t, err)
		}

		assert.Equal(t, []string{named.Key}, rec.header.Values("Api-Key"))
		assert.Empty(t, w.ownKey(rec.header), "the key went in the provider's header alone")
		assert.Equal(t, url.Values{"api-version": {"2026-01-01"}}, rec.query)
		assert.Equal(t, keylessRec.path, rec.path, "the query leaves the path alone")
		assert.NotContains(t, rec.body, w.sent)
		assert.Equal(t, w.convo(keylessRec.body), w.convo(rec.body), "the conversation still goes")

		assert.Empty(t, keylessRec.header.Values("Api-Key"))
		assert.Empty(t, w.ownKey(keylessRec.header))
		assert.Contains(t, keylessRec.body, w.sent)
	})
}

// A turn's rendering is the leading slice of the next's, and nothing outside the
// conversation moves: the conversation is what grows, and a prompt cache keys on
// the rest holding still.
func TestEveryDialectRendersATurnAsAPrefixOfTheNext(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		first := []Message{{Role: "user", Blocks: []Block{ContextBlock("the card"), TextBlock("what is a pod?")}}}
		second := append(append([]Message{}, first...),
			Message{Role: "assistant", Blocks: []Block{TextBlock("a group.")}},
			Message{Role: "user", Blocks: []Block{TextBlock("and a node?")}})

		bodies := make([]map[string]any, 2)
		for i, msgs := range [][]Message{first, second} {
			p, rec := w.replay(t, w.text)
			_, err := w.stream(t.Context(), w.req(p, msgs...), testutil.Timeout, wallClock, func(Chunk) {})
			require.NoError(t, err)
			bodies[i] = rec.body
		}

		before, after := w.convo(bodies[0]), w.convo(bodies[1])
		require.Less(t, len(before), len(after))
		for i, elem := range before {
			assert.JSONEq(t, marshal(t, elem), marshal(t, after[i]), "element %d moved", i)
		}
		for key, value := range bodies[0] {
			if key == w.conversationKey {
				continue
			}
			assert.Equal(t, value, bodies[1][key], key)
		}
		assert.Len(t, bodies[1], len(bodies[0]), "the second turn carries no new field")
	})
}

// nested is the string at path in a decoded body, empty where any step is absent.
func nested(body map[string]any, path ...string) string {
	var cur any = body
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[key]
	}
	s, _ := cur.(string)
	return s
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// The SDK's default client reads the vendor's variables, so no dialect builds
// one: DefaultClientOptions is the SDK's one reader of them and NewClient its one
// caller, and there is no public switch that turns them off. takeProviderKeys
// clears OPENAI_API_KEY alone, not OPENAI_BASE_URL or OPENAI_CUSTOM_HEADERS, so
// the environment tests prove the clients this package builds and this walk keeps
// a later one from building the other kind. A tool's wire file builds no client
// or service of either SDK: it spells types, and llm sends them.
func TestNoDialectReadsTheSDKsEnvironmentDefaults(t *testing.T) {
	inDialect := map[string]bool{"NewClient": true, "DefaultClientOptions": true}
	// A tool's wire file builds nothing at all: a client is llm's alone.
	inWire := regexp.MustCompile(`^(New\w*(Client|Service)|DefaultClientOptions)$`)

	eachGoFile(t, 0, func(file string, f *ast.File) {
		dir, name := path.Split(file)
		dialect := dir == "internal/llm/" && sdkFile(name)
		if !dialect && !wireFile(file) {
			return
		}
		// Each SDK by the import's own name, never its literal one: an alias
		// would otherwise walk straight past.
		locals := map[string]string{}
		for _, imp := range f.Imports {
			pkg, err := strconv.Unquote(imp.Path.Value)
			require.NoError(t, err, file)
			local, ok := sdkRoots[pkg]
			if !ok {
				continue
			}
			if imp.Name != nil {
				local = imp.Name.Name
			}
			locals[local] = pkg
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			sdk, ok := locals[pkg.Name]
			if !ok {
				return true
			}
			banned := inWire.MatchString(sel.Sel.Name)
			if dialect {
				banned = sdk == openaiSDK && inDialect[sel.Sel.Name]
			}
			if banned {
				t.Errorf("%s calls %s.%s: it builds a client that reads the vendor's variables", file, pkg.Name, sel.Sel.Name)
			}
			return true
		})
	})
}

// A definition the request was handed is offered as a function under its name,
// and a request handed none carries no tools field at all.
func TestEveryDialectOffersToolsAsFunctions(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		p, rec := w.replay(t, w.call)
		req := w.req(p)
		req.Tools = []ToolDefinition{listObjects}

		_, err := w.stream(t.Context(), req, testutil.Timeout, wallClock, func(Chunk) {})

		require.NoError(t, err)
		offered := w.tools(rec.body)
		require.Len(t, offered, 1)
		assert.Equal(t, "list_objects", w.toolName(offered[0]))

		none, noneRec := w.replay(t, w.text)
		_, err = w.stream(t.Context(), w.req(none), testutil.Timeout, wallClock, func(Chunk) {})
		require.NoError(t, err)
		assert.NotContains(t, noneRec.body, "tools")
	})
}

// A call arrives whole at the end of the reply, never as a chunk, and the record
// holds it as the app's own block: the provider's id for it, the offer's name and
// the arguments as an object. A reply that asked for one stops on tool_use.
func TestEveryDialectReadsACallIntoTheRecord(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		p, _ := w.replay(t, w.call)
		req := w.req(p)
		req.Tools = []ToolDefinition{listObjects}
		var chunks []Chunk

		resp, err := w.stream(t.Context(), req, testutil.Timeout, wallClock, func(c Chunk) { chunks = append(chunks, c) })

		require.NoError(t, err)
		require.NotEmpty(t, resp.Blocks)
		call := resp.Blocks[len(resp.Blocks)-1]
		assert.Equal(t, BlockToolUse, call.Type)
		assert.Equal(t, "call_1", call.ID)
		assert.Equal(t, "list_objects", call.Name)
		assert.JSONEq(t, `{"resource":"pods"}`, string(call.Input))
		assert.Equal(t, StopToolUse, resp.StopReason)
		for _, c := range chunks {
			assert.NotContains(t, c.Text, "list_objects", "no chunk carries the call")
			assert.NotContains(t, c.Text, "pods")
		}
	})
}

// A call that ended normally with arguments that are not an object is a reply the
// wire could not read: nothing is kept, and the log says where it was, never what
// it said. The truncation arm is each wire's own, since each API says "cut off"
// in its own words.
func TestEveryDialectFailsAMalformedCallThatEndedNormally(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		buf := captureLog(t)
		p, _ := w.replay(t, w.malformed)
		req := w.req(p)
		req.Tools = []ToolDefinition{listObjects}

		resp, err := w.stream(t.Context(), req, testutil.Timeout, wallClock, func(Chunk) {})

		var e *Error
		require.ErrorAs(t, err, &e)
		assert.Equal(t, "unreadable reply", e.Kind)
		assert.Empty(t, resp.Blocks)
		assert.NotContains(t, buf.String(), "MARKER")
	})
}

// A round's rendering is the leading slice of the next turn's, like any other
// turn: what a wire unfolds a row into holds still as the conversation grows.
func TestEveryDialectRendersARoundAsAPrefixOfTheNext(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		row := []Block{
			TextBlock("Let me check."),
			ToolUseBlock("call_1", "list_objects", json.RawMessage(`{"resource":"pods"}`)),
			ToolResultBlock("call_1", "2 pods", false),
			TextBlock("Two."),
		}
		bodies := make([]map[string]any, 2)
		for i := range bodies {
			p, rec := w.replay(t, w.text)
			msgs := []Message{
				{Role: "user", Blocks: []Block{TextBlock("how many pods?")}},
				{Role: "assistant", Blocks: row, ProviderID: p.ID, Dialect: p.Dialect},
			}
			if i == 1 {
				msgs = append(msgs, Message{Role: "user", Blocks: []Block{TextBlock("and nodes?")}})
			}
			req := w.req(p, msgs...)
			req.Tools = []ToolDefinition{listObjects}
			_, err := w.stream(t.Context(), req, testutil.Timeout, wallClock, func(Chunk) {})
			require.NoError(t, err)
			bodies[i] = rec.body
		}

		before, after := w.convo(bodies[0]), w.convo(bodies[1])
		require.Less(t, len(before), len(after))
		for i, elem := range before {
			assert.JSONEq(t, marshal(t, elem), marshal(t, after[i]), "element %d moved", i)
		}
		for key, value := range bodies[0] {
			if key == w.conversationKey {
				continue
			}
			assert.Equal(t, value, bodies[1][key], key)
		}
	})
}

// A row another provider wrote can be all payload: a reasoning item with no
// summary. The strip leaves nothing of it, and the wire sends nothing for it,
// since an API refuses an empty assistant message and the row would be replayed
// on every later turn.
func TestEveryDialectSendsNothingForARowTheStripEmpties(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		first := Message{Role: "user", Blocks: []Block{TextBlock("how many pods?")}}
		foreign := Message{Role: "assistant", ProviderID: "another", Blocks: []Block{
			withPayload(ThinkingBlock(""), `{"type":"reasoning","encrypted_content":"c2Vr"}`),
		}}
		second := Message{Role: "user", Blocks: []Block{TextBlock("and nodes?")}}
		bodies := make([]map[string]any, 2)
		for i, msgs := range [][]Message{{first, foreign, second}, {first, second}} {
			p, rec := w.replay(t, w.text)
			_, err := w.stream(t.Context(), w.req(p, msgs...), testutil.Timeout, wallClock, func(Chunk) {})
			require.NoError(t, err)
			bodies[i] = rec.body
		}

		assert.JSONEq(t, marshal(t, w.convo(bodies[1])), marshal(t, w.convo(bodies[0])))
	})
}

// A switch replays another dialect's rows on this one: every pair of wires, the
// source's thinking, calls and text replies recorded under its provider, sent as
// the target's history with thinking off and on. Nothing of the source's payloads
// goes, every call goes with exactly one result under an id each API takes and no
// other call uses, every text goes, and no thinking does.
func TestEveryDialectReplaysEveryOtherDialectsTurn(t *testing.T) {
	validID := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	for _, source := range wires() {
		for _, target := range wires() {
			if source.name == target.name {
				continue
			}
			for _, effort := range []string{"", "high"} {
				t.Run(fmt.Sprintf("%s to %s effort %q", source.name, target.name, effort), func(t *testing.T) {
					rows := switchedRows(t, source)
					history := interleave(rows)
					stripped := make([]Message, len(rows))
					for i, m := range rows {
						m.Blocks = WithoutPayloads(m.Blocks)
						stripped[i] = m
					}

					body := func(msgs []Message) map[string]any {
						p, rec := target.replay(t, target.text)
						req := target.req(p, msgs...)
						req.Tools, req.Effort = []ToolDefinition{listObjects}, effort
						_, err := target.stream(t.Context(), req, testutil.Timeout, wallClock, func(Chunk) {})
						require.NoError(t, err)
						return rec.body
					}
					sent := body(history)

					assert.JSONEq(t, marshal(t, target.convo(body(interleave(stripped)))), marshal(t, target.convo(sent)),
						"the source's payloads go nowhere")
					calls, results := target.pairs(sent)
					if effort == "" || target.name != "messages" {
						assert.NotEmpty(t, calls, "the round goes")
					}
					assert.ElementsMatch(t, calls, results, "each call has exactly one result")
					for i, id := range calls {
						assert.Regexp(t, validID, id)
						assert.NotContains(t, calls[:i], id, "no id goes twice")
					}
					convo := marshal(t, target.convo(sent))
					for _, m := range rows {
						for _, b := range m.Blocks {
							switch b.Type {
							case BlockText:
								assert.Contains(t, convo, b.Text)
							case BlockThinking:
								if b.Text != "" {
									assert.NotContains(t, convo, b.Text)
								}
							}
						}
					}
				})
			}
		}
	}
}

// A chat that crossed every dialect still renders as the leading slice of its next
// turn on each wire: the minted ids are the calls' places, so they hold still as
// the chat grows, and the cache prefix holds within one provider.
func TestEveryDialectRendersASwitchedChatAsAPrefixOfTheNext(t *testing.T) {
	var rows []Message
	for _, source := range wires() {
		rows = append(rows, switchedRows(t, source)...)
	}
	history := interleave(rows)
	before := history[:len(history)-1]

	eachWire(t, func(t *testing.T, w wire) {
		bodies := make([]map[string]any, 2)
		for i, msgs := range [][]Message{before, history} {
			p, rec := w.replay(t, w.text)
			req := w.req(p, msgs...)
			req.Tools = []ToolDefinition{listObjects}
			_, err := w.stream(t.Context(), req, testutil.Timeout, wallClock, func(Chunk) {})
			require.NoError(t, err)
			bodies[i] = rec.body
		}

		first, next := w.convo(bodies[0]), w.convo(bodies[1])
		require.Less(t, len(first), len(next))
		for i, elem := range first {
			assert.JSONEq(t, marshal(t, elem), marshal(t, next[i]), "element %d moved", i)
		}
	})
}

// switchedRows is w's canned thinking, call and text replies as the record keeps
// them under w's provider, the call twice so its id repeats as a chat's can: each
// call's row carries the result the loop wrote and closes on text.
func switchedRows(t *testing.T, w wire) []Message {
	t.Helper()
	var rows []Message
	for _, events := range [][]event{w.thinks, w.call, w.call, w.text} {
		p, _ := w.replay(t, events)
		resp, err := w.stream(t.Context(), w.req(p), testutil.Timeout, wallClock, func(Chunk) {})
		require.NoError(t, err)
		blocks := resp.Blocks
		for _, b := range resp.Blocks {
			if b.Type == BlockToolUse {
				blocks = append(blocks, ToolResultBlock(b.ID, "2 pods", false), TextBlock("Two pods."))
			}
		}
		rows = append(rows, Message{Role: "assistant", ProviderID: p.ID, Dialect: p.Dialect, Effort: "high", Blocks: blocks})
	}
	return rows
}

// interleave is rows as a chat's history, a question ahead of each answer and one
// after the last.
func interleave(rows []Message) []Message {
	var out []Message
	for i, m := range rows {
		out = append(out, Message{Role: "user", Blocks: []Block{TextBlock(fmt.Sprintf("question %d", i))}}, m)
	}
	return append(out, Message{Role: "user", Blocks: []Block{TextBlock("and now?")}})
}

// messagesPairs is the calls and results a Messages body carries.
func messagesPairs(body map[string]any) (calls, results []string) {
	for _, m := range body["messages"].([]any) {
		content, _ := m.(map[string]any)["content"].([]any)
		for _, c := range content {
			block := c.(map[string]any)
			switch block["type"] {
			case "tool_use":
				calls = append(calls, block["id"].(string))
			case "tool_result":
				results = append(results, block["tool_use_id"].(string))
			}
		}
	}
	return calls, results
}

// responsesPairs is the calls and results a Responses body carries.
func responsesPairs(body map[string]any) (calls, results []string) {
	for _, item := range body["input"].([]any) {
		item := item.(map[string]any)
		switch item["type"] {
		case "function_call":
			calls = append(calls, item["call_id"].(string))
		case "function_call_output":
			results = append(results, item["call_id"].(string))
		}
	}
	return calls, results
}

// chatPairs is the calls and results a Chat Completions body carries.
func chatPairs(body map[string]any) (calls, results []string) {
	for _, m := range body["messages"].([]any) {
		m := m.(map[string]any)
		toolCalls, _ := m["tool_calls"].([]any)
		for _, c := range toolCalls {
			calls = append(calls, c.(map[string]any)["id"].(string))
		}
		if m["role"] == "tool" {
			results = append(results, m["tool_call_id"].(string))
		}
	}
	return calls, results
}

// Each wire reads its API's usage into one shape: the input everything the model
// read, the cache counts part of it. A reply with no usage object is unreported,
// never a reported zero.
func TestEveryDialectReportsItsUsage(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		p, _ := w.replay(t, w.reported)
		resp, err := w.stream(t.Context(), w.req(p), testutil.Timeout, wallClock, func(Chunk) {})
		require.NoError(t, err)
		assert.Equal(t, w.usage, resp.Usage)

		p, _ = w.replay(t, w.unreported)
		resp, err = w.stream(t.Context(), w.req(p), testutil.Timeout, wallClock, func(Chunk) {})
		require.NoError(t, err)
		assert.Equal(t, Usage{}, resp.Usage)
	})
}

// Each wire reads the model its provider said it served, which the call's row
// keeps in place of the one asked for.
func TestEveryDialectReportsTheModelServed(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		p, _ := w.replay(t, w.text)
		resp, err := w.stream(t.Context(), w.req(p), testutil.Timeout, wallClock, func(Chunk) {})
		require.NoError(t, err)
		assert.Equal(t, w.served, resp.Model)
	})
}

// A provider whose credential is a signer has each request signed, and no key
// goes beside it.
func TestEveryDialectSignsARequestUnderASigner(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		p, rec := w.replay(t, w.text)

		_, err := w.stream(t.Context(), w.req(signedBy(p, &fakeSigner{token: "signed"})), testutil.Timeout, wallClock, func(Chunk) {})

		require.NoError(t, err)
		assert.Equal(t, []string{"Bearer signed"}, rec.header.Values("Authorization"))
		assert.Empty(t, rec.header.Values("X-Api-Key"))
	})
}

// A 401 tells the signer which request was refused, and fails the call: a turn
// is never retried.
func TestEveryDialectTellsTheSignerOfARefusal(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		var calls atomic.Int64
		p := w.serve(t, func(rw http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(rw, `{"type":"error","error":{"type":"authentication_error","message":"expired"}}`)
		})
		s := &fakeSigner{token: "stale"}

		_, err := w.stream(t.Context(), w.req(signedBy(p, s)), testutil.Timeout, wallClock, func(Chunk) {})

		var llmErr *Error
		require.ErrorAs(t, err, &llmErr)
		assert.Equal(t, http.StatusUnauthorized, llmErr.Status)
		assert.Equal(t, []string{"Bearer stale"}, s.refusals())
		assert.Equal(t, int64(1), calls.Load())
	})
}

// A keyed provider's 401 fails the call as any other status does.
func TestEveryDialectRendersAKeyedRefusal(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		p := w.serve(t, func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(rw, `{"type":"error","error":{"type":"authentication_error","message":"revoked"}}`)
		})

		_, err := w.stream(t.Context(), w.req(p), testutil.Timeout, wallClock, func(Chunk) {})

		var llmErr *Error
		require.ErrorAs(t, err, &llmErr)
		assert.Equal(t, http.StatusUnauthorized, llmErr.Status)
	})
}

// A signer that cannot sign sends nothing, and the failure says which kind it
// was without its cause's text.
func TestEveryDialectSendsNothingASignerCouldNotSign(t *testing.T) {
	for _, c := range []struct {
		err  error
		kind string
	}{
		{ErrSignedOut, "signed out"},
		{ErrCredentialChanged, "credential changed"},
		{context.Canceled, "cancelled"},
		{context.DeadlineExceeded, "timed out"},
		{errors.New("the keyring is locked"), "credential unavailable"},
	} {
		t.Run(c.kind, func(t *testing.T) {
			eachWire(t, func(t *testing.T, w wire) {
				p, rec := w.replay(t, w.text)

				_, err := w.stream(t.Context(), w.req(signedBy(p, &fakeSigner{err: c.err})), testutil.Timeout, wallClock, func(Chunk) {})

				var llmErr *Error
				require.ErrorAs(t, err, &llmErr)
				assert.Equal(t, c.kind, llmErr.Kind)
				assert.Zero(t, llmErr.Status)
				assert.ErrorIs(t, err, c.err)
				assert.NotContains(t, err.Error(), "keyring")
				assert.Zero(t, rec.calls.Load())
			})
		})
	}
}

// No request follows a redirect, to another host or the same one, whatever its
// credential: Go would resend an Authorization to a target on the same domain,
// and a key in any other header to any target. The redirect fails the call as
// any other status does.
func TestEveryDialectFollowsNoRedirect(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		var elsewhere atomic.Int64
		target := newServer(t, func(http.ResponseWriter, *http.Request) { elsewhere.Add(1) })
		for _, code := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
			keyed := w.serve(t, func(rw http.ResponseWriter, r *http.Request) {
				http.Redirect(rw, r, target+r.URL.Path, code)
			})
			named := keyed
			named.KeyHeader = "api-key"

			for _, p := range []Provider{keyed, named, signedBy(keyed, &fakeSigner{token: "signed"})} {
				_, err := w.stream(t.Context(), w.req(p), testutil.Timeout, wallClock, func(Chunk) {})

				var llmErr *Error
				require.ErrorAs(t, err, &llmErr)
				assert.Equal(t, code, llmErr.Status)
			}
		}
		assert.Zero(t, elsewhere.Load())
	})
}

// A signed request that reaches nothing is a transport failure, as an unsigned
// one is.
func TestEveryDialectRendersASignedFailureWithNoResponse(t *testing.T) {
	eachWire(t, func(t *testing.T, w wire) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		addr := ln.Addr().String()
		require.NoError(t, ln.Close())

		_, err = w.stream(t.Context(), w.req(signedBy(w.at("http://"+addr), &fakeSigner{token: "t"})), testutil.Timeout, wallClock, func(Chunk) {})

		var llmErr *Error
		require.ErrorAs(t, err, &llmErr)
		assert.Equal(t, "connection failed", llmErr.Kind)
	})
}
