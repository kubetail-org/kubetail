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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// A reply is a thought a word at a time, then the sentence a word at a time, and
// the response is the pair as blocks.
func TestFakeThinksBeforeItAnswers(t *testing.T) {
	f := NewFake(0)
	var chunks []Chunk

	resp, err := f.Stream(t.Context(), Request{Model: Model{ID: "m"}}, func(c Chunk) { chunks = append(chunks, c) })

	require.NoError(t, err)
	thinking, text := joinKind(chunks, ChunkThinking), joinKind(chunks, ChunkText)
	assert.Equal(t, thoughts[0], thinking)
	assert.Equal(t, bank[0], text)
	assert.Greater(t, len(chunks), len(strings.Fields(text)), "a word per chunk, the thought's included")
	assert.Equal(t, ChunkThinking, chunks[0].Kind, "the thought comes first")
	assert.Equal(t, AnswerBlocks(thinking, text), resp.Blocks)
	assert.Equal(t, "end_turn", resp.StopReason)
	assert.Equal(t, "m", f.LastRequest().Model.ID)
	assert.Equal(t, 1, f.Asked())
}

// A set reply is streamed exactly as given, once, and its response is its chunks
// of each kind concatenated: how a consumer's test gets a reply that thinks again
// after its first text, which the bank never does.
func TestFakeStreamsTheReplyItIsSet(t *testing.T) {
	f := NewFake(0)
	set := []Chunk{
		{Kind: ChunkThinking, Text: "counting"},
		{Text: "twelve"},
		{Kind: ChunkThinking, Text: " again"},
		{Text: " pods"},
	}
	f.SetReply(set...)
	var chunks []Chunk

	resp, err := f.Stream(t.Context(), Request{}, func(c Chunk) { chunks = append(chunks, c) })

	require.NoError(t, err)
	assert.Equal(t, set, chunks)
	assert.Equal(t, AnswerBlocks("counting again", "twelve pods"), resp.Blocks)

	chunks = nil
	_, err = f.Stream(t.Context(), Request{}, func(c Chunk) { chunks = append(chunks, c) })
	require.NoError(t, err)
	assert.Equal(t, bank[0], joinKind(chunks, ChunkText), "a set reply rides one stream")
}

// A reply set to nothing streams no chunk and holds no block, the reply with no
// displayable chunk a consumer's test needs; the bank answers the one after.
func TestFakeStreamsAnEmptyReplyItIsSet(t *testing.T) {
	f := NewFake(0)
	f.SetReply()

	resp, err := f.Stream(t.Context(), Request{}, func(c Chunk) { t.Errorf("an empty reply emitted %v", c) })

	require.NoError(t, err)
	assert.Empty(t, resp.Blocks)
	assert.Equal(t, "end_turn", resp.StopReason)

	var chunks []Chunk
	_, err = f.Stream(t.Context(), Request{}, func(c Chunk) { chunks = append(chunks, c) })
	require.NoError(t, err)
	assert.Equal(t, bank[0], joinKind(chunks, ChunkText), "an empty reply rides one stream")
}

// The model and the effort steer nothing; the request shows what was handed over.
func TestFakeIgnoresModelAndEffort(t *testing.T) {
	f := NewFake(0)
	var first, second []Chunk

	_, err := f.Stream(t.Context(), Request{Model: Model{ID: "m"}, Effort: "low"}, func(c Chunk) { first = append(first, c) })
	require.NoError(t, err)
	assert.Equal(t, "low", f.LastRequest().Effort)

	f.next = 0
	_, err = f.Stream(t.Context(), Request{Model: Model{ID: "other"}, Effort: "high"}, func(c Chunk) { second = append(second, c) })
	require.NoError(t, err)
	assert.Equal(t, "high", f.LastRequest().Effort)
	assert.Equal(t, first, second)
}

// The gate holds the reply after its first chunk, which is the thought's first
// word.
func TestFakeStopsOnACancelledContext(t *testing.T) {
	f := NewFake(0)
	f.SetGate(make(chan struct{}))
	ctx, cancel := context.WithCancel(t.Context())
	var chunks []string

	// The gate holds the reply, so the cancel is what ends it.
	_, err := f.Stream(ctx, Request{}, func(c Chunk) {
		chunks = append(chunks, c.Text)
		cancel()
	})

	assert.ErrorIs(t, err, context.Canceled)
	assert.Len(t, chunks, 1)
}

// streamed is what a Stream run in the background returned.
type streamed struct {
	resp Response
	err  error
}

// The gate holds the reply after its first chunk, and its close lets the whole
// rest through.
func TestFakeGateHoldsAfterItsFirstChunkUntilItCloses(t *testing.T) {
	const delay = time.Millisecond
	f := NewFake(delay)
	gate := make(chan struct{})
	f.SetGate(gate)
	want := wordChunks(thoughts[0], bank[0])
	chunks, done := make(chan Chunk, len(want)), make(chan streamed, 1)
	go func() {
		resp, err := f.Stream(t.Context(), Request{}, func(c Chunk) { chunks <- c })
		done <- streamed{resp, err}
	}()

	first := testutil.Recv(t, chunks, "the first chunk")
	// A negative assertion needs a window: an open gate would let the second chunk
	// through one chunk delay after the first, so fifty delays is well past it.
	testutil.NoRecv(t, chunks, 50*delay, "a second chunk before the gate closes")

	close(gate)
	got := testutil.Recv(t, done, "the reply after the gate closes")
	require.NoError(t, got.err)
	assert.Equal(t, AnswerBlocks(thoughts[0], bank[0]), got.resp.Blocks)
	close(chunks)
	all := []Chunk{first}
	for c := range chunks {
		all = append(all, c)
	}
	assert.Equal(t, want, all)
}

// A reply too short to be held after its first chunk is held at its end, so the
// gate holds every reply it is set for. A cancel is what ends each: an open gate
// would have let it return.
func TestFakeGateHoldsAShortReplyAtItsEnd(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		f := NewFake(0)
		f.SetReply()
		f.SetGate(make(chan struct{}))
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := f.Stream(ctx, Request{}, func(c Chunk) { t.Errorf("an empty reply emitted %v", c) })

		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("one chunk", func(t *testing.T) {
		f := NewFake(0)
		f.SetReply(Chunk{Text: "twelve"})
		f.SetGate(make(chan struct{}))
		ctx, cancel := context.WithCancel(t.Context())
		var chunks []string

		_, err := f.Stream(ctx, Request{}, func(c Chunk) {
			chunks = append(chunks, c.Text)
			cancel()
		})

		assert.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, []string{"twelve"}, chunks)
	})

	t.Run("released on close", func(t *testing.T) {
		f := NewFake(0)
		f.SetReply()
		gate := make(chan struct{})
		close(gate)
		f.SetGate(gate)

		resp, err := f.Stream(t.Context(), Request{}, func(Chunk) {})

		require.NoError(t, err)
		assert.Equal(t, "end_turn", resp.StopReason)
	})
}

// The gate releases on close alone, and a value sent on it panics the reply.
func TestFakeGateRefusesASend(t *testing.T) {
	f := NewFake(0)
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	f.SetGate(gate)

	assert.PanicsWithValue(t, "llm: a value was sent on the fake's gate; close it to release the reply", func() {
		_, _ = f.Stream(t.Context(), Request{}, func(Chunk) {})
	})
}

// The count is of chunks of either kind, so two is still inside the thought. A
// failed reply returns nothing: what it streamed is the consumer's to keep.
func TestFakeFailsAfterTheChunkItWasTold(t *testing.T) {
	f := NewFake(0)
	f.FailAfter(2, assert.AnError)
	var chunks []string

	resp, err := f.Stream(t.Context(), Request{}, func(c Chunk) { chunks = append(chunks, c.Text) })

	assert.ErrorIs(t, err, assert.AnError)
	assert.Zero(t, resp)
	assert.Len(t, chunks, 2)

	_, err = f.Stream(t.Context(), Request{}, func(Chunk) {})
	assert.NoError(t, err, "a failure rides one reply")
}

// A failure told to come after more chunks than the reply has comes once they
// have all streamed, so an empty reply can be refused before any chunk.
func TestFakeFailsAShortReplyAtItsEnd(t *testing.T) {
	f := NewFake(0)
	f.SetReply(Chunk{Text: "twelve"})
	f.FailAfter(3, assert.AnError)
	var chunks []string

	resp, err := f.Stream(t.Context(), Request{}, func(c Chunk) { chunks = append(chunks, c.Text) })

	assert.ErrorIs(t, err, assert.AnError)
	assert.Zero(t, resp)
	assert.Equal(t, []string{"twelve"}, chunks)

	f.SetReply()
	f.FailAfter(0, assert.AnError)
	_, err = f.Stream(t.Context(), Request{}, func(c Chunk) { t.Errorf("an empty reply emitted %v", c) })
	assert.ErrorIs(t, err, assert.AnError)
}

// A chunk delay paces the reply and nothing more: every word still arrives. The
// delay is the smallest one there is, so the test waits on the fake's own cadence
// rather than on a duration it picked; SetChunkDelay is how it gets there.
func TestFakePacesItsChunks(t *testing.T) {
	f := NewFake(time.Hour)
	f.SetChunkDelay(time.Nanosecond)
	var chunks []string

	resp, err := f.Stream(t.Context(), Request{}, func(c Chunk) { chunks = append(chunks, c.Text) })

	require.NoError(t, err)
	assert.Equal(t, AnswerBlocks(thoughts[0], bank[0]), resp.Blocks)
	assert.Equal(t, thoughts[0]+bank[0], strings.Join(chunks, ""), "every word still arrives")
}

// A paced reply honours a cancel while it is waiting out a chunk, so a turn the
// user stopped does not sit until the next word is due.
func TestAPacedFakeStopsOnACancelledContext(t *testing.T) {
	f := NewFake(time.Hour)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := f.Stream(ctx, Request{}, func(Chunk) { t.Error("a cancelled reply emitted a chunk") })

	assert.ErrorIs(t, err, context.Canceled)
}

// A panic set for a reply fires before its first chunk and rides that reply alone.
func TestFakePanicsWhenTold(t *testing.T) {
	f := NewFake(0)
	f.PanicNext("the sdk blew up")

	assert.PanicsWithValue(t, "the sdk blew up", func() {
		_, _ = f.Stream(t.Context(), Request{}, func(Chunk) { t.Error("a panicking reply emitted a chunk") })
	})
	_, err := f.Stream(t.Context(), Request{}, func(Chunk) {})
	assert.NoError(t, err)
}

// The fake is a provider like any other: the row fake, on its dialect and over its
// fake, with no endpoint. A dev run's picker has two models to show, one of them
// a model that takes no tools, the other arm a consumer tests.
func TestFakeProviderIsInTheCatalog(t *testing.T) {
	f := NewFake(0)
	p := FakeProvider(f)

	assert.Equal(t, "fake", p.ID)
	assert.Empty(t, p.BaseURL)
	assert.Equal(t, DialectFake, p.Dialect)
	assert.Same(t, f, p.Fake())
	require.Len(t, p.Catalog, 2)
	assert.Equal(t, "fake", p.Catalog[0].ID)
	assert.True(t, p.Catalog[0].Tools)
	assert.Equal(t, "fake-no-tools", p.Catalog[1].ID)
	assert.False(t, p.Catalog[1].Tools)
	assert.Contains(t, p.Catalog[0].Efforts, p.Catalog[0].DefaultEffort)
	assert.Empty(t, p.Catalog[1].Efforts)
	assert.Empty(t, p.Catalog[1].DefaultEffort)
}

// Both of the fake's models read a million tokens and state no cap, so a dev
// run's chat is never refused for its length.
func TestTheFakeStatesAContextWindow(t *testing.T) {
	for _, m := range FakeProvider(NewFake(0)).Catalog {
		assert.Equal(t, 1_000_000, m.ContextWindow, m.ID)
		assert.Zero(t, m.MaxOutputTokens, m.ID)
	}
}

// Staged calls ride after the reply's own blocks, with ids minted in order, and
// stop the reply on tool_use. The staging is the next reply's alone: the one after
// answers from the bank.
func TestFakeAnswersStagedToolCallsOnce(t *testing.T) {
	f := NewFake(0)
	f.SetToolCalls(StagedCall("echo", `{"say":"hi"}`), StagedCall("echo", `{"say":"again"}`))

	resp, err := f.Stream(t.Context(), Request{}, func(Chunk) {})

	require.NoError(t, err)
	assert.Equal(t, append(AnswerBlocks(thoughts[0], bank[0]),
		ToolUseBlock("call-1", "echo", []byte(`{"say":"hi"}`)),
		ToolUseBlock("call-2", "echo", []byte(`{"say":"again"}`))), resp.Blocks)
	assert.Equal(t, StopToolUse, resp.StopReason)

	next, err := f.Stream(t.Context(), Request{}, func(Chunk) {})

	require.NoError(t, err)
	assert.Equal(t, AnswerBlocks(thoughts[1], bank[1]), next.Blocks)
	assert.Equal(t, "end_turn", next.StopReason)
}

// A staged id rides out as the test wrote it, and the fake counts only the ids it
// mints, so the next one is still call-1.
func TestFakeKeepsAStagedCallsID(t *testing.T) {
	f := NewFake(0)
	f.SetToolCalls(StagedCallWithID("dup", "echo", `{}`), StagedCallWithID("dup", "echo", `{}`), StagedCall("echo", `{}`))

	resp, err := f.Stream(t.Context(), Request{}, func(Chunk) {})

	require.NoError(t, err)
	assert.Equal(t, append(AnswerBlocks(thoughts[0], bank[0]),
		ToolUseBlock("dup", "echo", []byte(`{}`)),
		ToolUseBlock("dup", "echo", []byte(`{}`)),
		ToolUseBlock("call-1", "echo", []byte(`{}`))), resp.Blocks)
}

// Queued rounds are asked one reply each, in order, after the next reply's
// staged calls; a nil round asks for nothing. Every request is kept, in order.
func TestFakeAsksQueuedRoundsOneReplyEach(t *testing.T) {
	f := NewFake(0)
	f.SetToolCalls(StagedCall("first", `{}`))
	f.QueueToolCalls([]Block{StagedCall("second", `{}`)}, nil, []Block{StagedCall("third", `{}`)})

	var names []string
	for i := range 5 {
		resp, err := f.Stream(t.Context(), Request{Effort: string(rune('a' + i))}, func(Chunk) {})
		require.NoError(t, err)
		name := ""
		if last := resp.Blocks[len(resp.Blocks)-1]; last.Type == BlockToolUse {
			name = last.Name
		}
		names = append(names, name)
	}

	assert.Equal(t, []string{"first", "second", "", "third", ""}, names)
	var efforts []string
	for _, r := range f.Requests() {
		efforts = append(efforts, r.Effort)
	}
	assert.Equal(t, []string{"a", "b", "c", "d", "e"}, efforts)
}

// A repeat asks on every reply, so a caller's bound on rounds can be counted, and
// the ids go on counting across the fake's life. Nil stops it.
func TestFakeRepeatsToolCallsOnEveryReply(t *testing.T) {
	f := NewFake(0)
	f.RepeatToolCalls(StagedCall("echo", `{}`))

	for i, want := range []string{"call-1", "call-2"} {
		resp, err := f.Stream(t.Context(), Request{}, func(Chunk) {})
		require.NoError(t, err, i)
		assert.Equal(t, ToolUseBlock(want, "echo", []byte(`{}`)), resp.Blocks[len(resp.Blocks)-1])
		assert.Equal(t, StopToolUse, resp.StopReason)
	}

	f.RepeatToolCalls()
	resp, err := f.Stream(t.Context(), Request{}, func(Chunk) {})

	require.NoError(t, err)
	assert.Equal(t, "end_turn", resp.StopReason)
}

// A reply that failed asked for nothing: its staged calls mint no ids, so the ids
// of the replies after it are what a test counting from the start expects.
func TestFakeMintsNoCallForAReplyThatFailed(t *testing.T) {
	f := NewFake(0)
	f.SetToolCalls(StagedCall("echo", `{}`))
	f.FailAfter(0, assert.AnError)

	_, err := f.Stream(t.Context(), Request{}, func(Chunk) {})
	require.ErrorIs(t, err, assert.AnError)

	f.SetToolCalls(StagedCall("echo", `{}`))
	resp, err := f.Stream(t.Context(), Request{}, func(Chunk) {})

	require.NoError(t, err)
	assert.Equal(t, ToolUseBlock("call-1", "echo", []byte(`{}`)), resp.Blocks[len(resp.Blocks)-1])
}

// A staged stop is the reply's whatever it holds: a call the cap cut off, and a
// tool_use with nothing to run.
func TestFakeStopIsStageable(t *testing.T) {
	f := NewFake(0)
	f.SetToolCalls(StagedCall("echo", `{}`))
	f.SetStop("max_tokens")

	cut, err := f.Stream(t.Context(), Request{}, func(Chunk) {})

	require.NoError(t, err)
	assert.Equal(t, "max_tokens", cut.StopReason)
	assert.Equal(t, BlockToolUse, cut.Blocks[len(cut.Blocks)-1].Type)

	f.SetStop(StopToolUse)
	malformed, err := f.Stream(t.Context(), Request{}, func(Chunk) {})

	require.NoError(t, err)
	assert.Equal(t, StopToolUse, malformed.StopReason)
	assert.Equal(t, AnswerBlocks(thoughts[1], bank[1]), malformed.Blocks)
}

// Stops staged together end the replies after, one each, so a test can pause a
// turn more than once; the reply past them ends as it would have.
func TestFakeStopsEachReplyOnTheNextStagedReason(t *testing.T) {
	f := NewFake(0)
	f.SetStop(StopPauseTurn, StopPauseTurn)

	var reasons []string
	for range 3 {
		resp, err := f.Stream(t.Context(), Request{}, func(Chunk) {})
		require.NoError(t, err)
		reasons = append(reasons, resp.StopReason)
	}

	assert.Equal(t, []string{StopPauseTurn, StopPauseTurn, "end_turn"}, reasons)
}

// A staged server chunk streams where it was put and lands as a server_use block
// in place, splitting the text around it; it carries no payload, as the fake
// writes none.
func TestFakeFoldsAServerChunkIntoItsReply(t *testing.T) {
	f := NewFake(0)
	reply := []Chunk{
		{Text: "Let me search."},
		{Kind: ChunkServer, Call: withoutPayload(searchUse("srv_1", "kubernetes 1.36"))},
		{Text: "It shipped."},
	}
	f.SetReply(reply...)
	var chunks []Chunk

	resp, err := f.Stream(t.Context(), Request{}, func(c Chunk) { chunks = append(chunks, c) })

	require.NoError(t, err)
	assert.Equal(t, reply, chunks)
	assert.Equal(t, []Block{
		TextBlock("Let me search."),
		withoutPayload(searchUse("srv_1", "kubernetes 1.36")),
		TextBlock("It shipped."),
	}, resp.Blocks)
}

// A staged count and staged citations ride the next reply alone: the count on
// the response, the citations as its last text block's payload, which the
// fake's reader reads back.
func TestFakeStagesACountAndCitationsForTheNextReply(t *testing.T) {
	f := NewFake(0)
	f.SetReply(Chunk{Text: "Let me search."}, Chunk{Kind: ChunkServer, Call: withoutPayload(searchUse("srv_1", "q"))}, Chunk{Text: "It shipped."})
	f.SetServerUses(map[string]int{searchName: 1})
	f.SetCitations(releases)

	first, err := f.Stream(t.Context(), Request{}, func(Chunk) {})

	require.NoError(t, err)
	assert.Equal(t, map[string]int{searchName: 1}, first.ServerUses)
	assert.Nil(t, first.Blocks[0].Payload)
	assert.NotNil(t, first.Blocks[2].Payload)
	assert.Equal(t, []Citation{releases}, Citations(DialectFake, first.Blocks))

	second, err := f.Stream(t.Context(), Request{}, func(Chunk) {})

	require.NoError(t, err)
	assert.Nil(t, second.ServerUses)
	assert.Empty(t, Citations(DialectFake, second.Blocks))
}

// A reply reports what the tag's fake did: the request's words plus the two
// cache constants as the input, which the constants are part of, and the reply's
// words, its thought's included, as the output. The system prompt is not counted.
func TestFakeReportsItsWordsAndTheCacheConstants(t *testing.T) {
	f := NewFake(0)
	f.SetReply(Chunk{Kind: ChunkThinking, Text: "a thought "}, Chunk{Text: "three word reply"})
	req := Request{SystemPrompt: "not counted", Messages: []Message{
		{Role: "user", Blocks: []Block{TextBlock("how many pods")}},
		{Role: "assistant", Blocks: []Block{TextBlock("twelve")}},
		{Role: "user", Blocks: []Block{TextBlock("and nodes")}},
	}}

	resp, err := f.Stream(t.Context(), req, func(Chunk) {})

	require.NoError(t, err)
	assert.Equal(t, Usage{Reported: true, InputTokens: 6 + 3, CacheReadTokens: 2, CacheWriteTokens: 1, OutputTokens: 5}, resp.Usage)
}

// Staged reports ride the next replies, one each and in order, whatever they
// say; the reply after them reports its words again.
func TestFakeReportsAStagedUsage(t *testing.T) {
	f := NewFake(0)
	big := Usage{Reported: true, InputTokens: 900_000, OutputTokens: 10}
	f.SetUsage(big, Usage{})

	first, err := f.Stream(t.Context(), Request{}, func(Chunk) {})
	require.NoError(t, err)
	second, err := f.Stream(t.Context(), Request{}, func(Chunk) {})
	require.NoError(t, err)
	third, err := f.Stream(t.Context(), Request{}, func(Chunk) {})
	require.NoError(t, err)

	assert.Equal(t, big, first.Usage)
	assert.Equal(t, Usage{}, second.Usage, "a staged zero is an unreported call")
	assert.True(t, third.Usage.Reported)
	assert.Equal(t, 2, third.Usage.CacheReadTokens)
}

// A staged served model rides the reply it was staged for, and a reply with
// none staged names no model, as a provider that says nothing does.
func TestFakeReportsAServedModel(t *testing.T) {
	f := NewFake(0)
	f.SetServedModel("fake-2026-09-01", "")

	first, err := f.Stream(t.Context(), Request{}, func(Chunk) {})
	require.NoError(t, err)
	second, err := f.Stream(t.Context(), Request{}, func(Chunk) {})
	require.NoError(t, err)
	third, err := f.Stream(t.Context(), Request{}, func(Chunk) {})
	require.NoError(t, err)

	assert.Equal(t, "fake-2026-09-01", first.Model)
	assert.Empty(t, second.Model)
	assert.Empty(t, third.Model)
}

// promptRequest is a request whose one message holds prompt, as a subagent's does.
func promptRequest(prompt string) Request {
	return Request{Messages: []Message{{Role: "user", Blocks: []Block{TextBlock(prompt)}}}}
}

// A route answers the requests whose first message ends in its prompt from its
// own staging, so a subagent's replies are staged apart from its parent's. The
// root still logs every request, and the ids run on across both.
func TestARouteAnswersItsOwnRequests(t *testing.T) {
	f := NewFake(0)
	sub := f.Route("Count the pods.")
	f.SetToolCalls(StagedCall("echo", `{}`))
	sub.SetToolCalls(StagedCall("look", `{}`))
	sub.SetReply(Chunk{Text: "twelve"})

	subResp, err := f.Stream(t.Context(), promptRequest("Count the pods."), func(Chunk) {})
	require.NoError(t, err)
	resp, err := f.Stream(t.Context(), promptRequest("how many pods?"), func(Chunk) {})
	require.NoError(t, err)

	assert.Equal(t, []Block{TextBlock("twelve"), ToolUseBlock("call-1", "look", []byte(`{}`))}, subResp.Blocks)
	assert.Equal(t, ToolUseBlock("call-2", "echo", []byte(`{}`)), resp.Blocks[len(resp.Blocks)-1])
	assert.Equal(t, bank[0], lastTextOf(resp.Blocks), "the root's bank is its own")
	assert.Equal(t, 2, f.Asked())
	assert.Len(t, f.Requests(), 2)
	assert.Equal(t, 1, sub.Asked())
}

// Before its first stream the fake was handed no request, and blocks with no
// text have none to answer by.
func TestFakeBeforeItsFirstStream(t *testing.T) {
	assert.Equal(t, Request{}, NewFake(0).LastRequest())
	assert.Empty(t, lastTextOf([]Block{ToolUseBlock("call-1", "echo", []byte(`{}`))}))
}
