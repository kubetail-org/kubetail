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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// The fake's first two replies, as a two-round turn takes them, and the first
// chunk of all: the first thought's first word.
const (
	firstWord      = "Let "
	thought        = "Let me count the pods in that namespace."
	twelvePods     = "Twelve pods are running in the default namespace."
	secondThought  = "I should check every replica's readiness."
	secondSentence = "The deployment rolled out cleanly; every replica is ready."
)

// answered is what the fake's first reply settles as.
var answered = llm.Response{Blocks: llm.AnswerBlocks(thought, twelvePods), StopReason: "end_turn"}

// fakeTurn is the one turn every test takes: a target resolved off a service
// listing the fake alone, and that fake, for the test to steer.
func fakeTurn() (*llm.Fake, Turn) {
	f := llm.NewFake(0)
	target, err := llm.New(llm.FakeProvider(f)).Resolve("fake", "fake", "low")
	if err != nil {
		panic(err)
	}
	return f, Turn{
		Target:       target,
		SystemPrompt: "You count pods.",
		Messages:     []llm.Message{{Role: "user", Blocks: []llm.Block{llm.TextBlock("How many?")}}},
		MaxToolCalls: 8,
	}
}

func TestRunStreamsAnAnswer(t *testing.T) {
	f, turn := fakeTurn()

	res, err := Run(t.Context(), turn, &recording{}, &recording{})

	require.NoError(t, err)
	assert.Equal(t, Result{Blocks: answered.Blocks, StopReason: "end_turn"}, res)
	require.Equal(t, 1, f.Asked())
	assert.Equal(t, llm.Request{Provider: turn.Target.Provider, Model: turn.Target.Model, Effort: turn.Target.Effort,
		SystemPrompt: SystemPrompt(turn), Messages: turn.Messages}, f.LastRequest(),
		"the provider, model and effort reach the request whole, under the assembled prompt")
}

func TestRunRecordsTheCallBeforeTheModelIsAsked(t *testing.T) {
	rec := &recording{}

	_, turn := fakeTurn()

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, "started", rec.log[0])
	assert.Equal(t, []string{"finished", "settled"}, rec.log[len(rec.log)-2:])
	assert.Equal(t, "first-chunk", rec.log[1])
	assert.Equal(t, len(strings.Fields(thought))+len(strings.Fields(twelvePods)), len(rec.log)-4, "one progress per chunk between")
	assert.Equal(t, Result{Blocks: answered.Blocks, StopReason: "end_turn"}, rec.result)
	assert.NoError(t, rec.streamErr)
}

func TestARefusedStartAsksNoModel(t *testing.T) {
	f, turn := fakeTurn()
	rec := &recording{startErr: assert.AnError}

	res, err := Run(t.Context(), turn, rec, rec)

	assert.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, Result{}, res)
	assert.Zero(t, f.Asked(), "no external operation follows a failed write")
	assert.Equal(t, []string{"started", "settled"}, rec.log, "the settle is told on every path out")
}

// Each progress snapshot is the answer so far, its own copy: the thought's
// prefixes alone while the model is silent, then the thought whole beside the
// sentence's prefixes.
func TestRunStreamsAThinkingAnswer(t *testing.T) {
	f, turn := fakeTurn()
	rec := &recording{}

	res, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, snapshots(thought, twelvePods), rec.progress)
	assert.Equal(t, llm.AnswerBlocks(thought, twelvePods), res.Blocks)

	t.Run("a run whose chunks are text alone has no thinking block", func(t *testing.T) {
		f.SetReply(llm.Chunk{Text: "Twelve "}, llm.Chunk{Text: "pods."})
		rec := &recording{}

		res, err := Run(t.Context(), turn, rec, rec)

		require.NoError(t, err)
		assert.Equal(t, [][]llm.Block{
			{llm.TextBlock("Twelve ")},
			{llm.TextBlock("Twelve pods.")},
		}, rec.progress)
		assert.Equal(t, []llm.Block{llm.TextBlock("Twelve pods.")}, res.Blocks)
	})
}

// A reply that thinks again after its first text grows the thinking block while
// the text block stands: the record reads thinking then text whatever the stream
// did.
func TestRunKeepsLateThinkingAheadOfTheText(t *testing.T) {
	f, turn := fakeTurn()
	f.SetReply(
		llm.Chunk{Kind: llm.ChunkThinking, Text: "counting"},
		llm.Chunk{Text: "twelve"},
		llm.Chunk{Kind: llm.ChunkThinking, Text: " again"},
		llm.Chunk{Text: " pods"},
	)
	rec := &recording{}

	res, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, [][]llm.Block{
		llm.AnswerBlocks("counting", ""),
		llm.AnswerBlocks("counting", "twelve"),
		llm.AnswerBlocks("counting again", "twelve"),
		llm.AnswerBlocks("counting again", "twelve pods"),
	}, rec.progress)
	assert.Equal(t, rec.progress[len(rec.progress)-1], res.Blocks)
}

// snapshots is the progress a reply of the thought then the sentence carries: the
// thought's prefixes alone, then the thought whole beside the sentence's.
func snapshots(thought, sentence string) [][]llm.Block {
	var want [][]llm.Block
	sofar := ""
	for _, word := range strings.SplitAfter(thought, " ") {
		sofar += word
		want = append(want, llm.AnswerBlocks(sofar, ""))
	}
	sofar = ""
	for _, word := range strings.SplitAfter(sentence, " ") {
		sofar += word
		want = append(want, llm.AnswerBlocks(thought, sofar))
	}
	return want
}

func TestCancelKeepsThePartialAnswer(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	f, turn := fakeTurn()
	f.SetGate(make(chan struct{}))
	rec := &recording{progressed: make(chan struct{})}
	type ret struct {
		res Result
		err error
	}
	done := make(chan ret, 1)
	go func() {
		res, err := Run(ctx, turn, rec, rec)
		done <- ret{res, err}
	}()

	// The first chunk's progress is the fake parked on its gate, inside its thought.
	testutil.Wait(t, rec.progressed, "the first chunk's progress")
	cancel()
	got := testutil.Recv(t, done, "the run to return")

	assert.ErrorIs(t, got.err, context.Canceled)
	assert.Equal(t, Result{Blocks: []llm.Block{llm.ThinkingBlock(firstWord)}}, got.res)
	assert.Equal(t, got.res, rec.result)
	assert.ErrorIs(t, rec.streamErr, context.Canceled)
}

func TestAStreamFailureKeepsThePartialAnswer(t *testing.T) {
	f, turn := fakeTurn()
	f.FailAfter(1, assert.AnError)
	rec := &recording{}

	res, err := Run(t.Context(), turn, rec, rec)

	assert.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, Result{Blocks: []llm.Block{llm.ThinkingBlock(firstWord)}}, res)
	assert.Equal(t, res, rec.result)
	assert.ErrorIs(t, rec.streamErr, assert.AnError)
}

// A query seen on the stream stays in the answer between the text around it,
// on the live row and on the one a broken stream leaves; the recorder is told a
// zero response, since there is none.
func TestABrokenReplyKeepsItsServerCalls(t *testing.T) {
	f, turn := fakeTurn()
	f.SetReply(
		llm.Chunk{Text: "Let me search."},
		llm.Chunk{Kind: llm.ChunkServer, Call: searchCall("srv_1", "kubernetes 1.36")},
		llm.Chunk{Text: "It "},
		llm.Chunk{Text: "shipped."},
	)
	f.FailAfter(3, assert.AnError)
	rec := &recording{}

	res, err := Run(t.Context(), turn, rec, rec)

	assert.ErrorIs(t, err, assert.AnError)
	want := []llm.Block{
		llm.TextBlock("Let me search."),
		searchCall("srv_1", "kubernetes 1.36"),
		llm.TextBlock("It "),
	}
	assert.Equal(t, want, res.Blocks)
	assert.Equal(t, want, rec.progress[len(rec.progress)-1])
	assert.Equal(t, []llm.Response{{}}, rec.finishedWith)
}

// search is the server tool a turn offers in these tests.
var search = llm.NativeOffer{Tool: searchTool{}, Server: true, MaxUses: 5}

// A reply the provider paused is asked again with its blocks as the last
// message, offered the same server tools, and the turn goes on to the answer. A
// pause runs no call and spends none of the budget.
func TestAPausedReplyIsResumed(t *testing.T) {
	f, turn := echoTurn(searchTool{})
	f.SetStop(llm.StopPauseTurn, llm.StopPauseTurn)
	rec := &recording{}

	res, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, 3, f.Asked())
	assert.Equal(t, "end_turn", res.StopReason)
	assert.Empty(t, rec.answers)
	req := f.LastRequest()
	assert.Equal(t, []llm.NativeOffer{search}, req.NativeTools)
	paused := slices.Concat(llm.AnswerBlocks(thought, twelvePods), llm.AnswerBlocks(secondThought, secondSentence))
	assert.Equal(t, paused, req.Messages[len(req.Messages)-1].Blocks, "both paused replies, whole")
}

// The cap is the turn's, not the request's: each round is offered what the
// earlier rounds left of it.
func TestEachRoundIsOfferedWhatIsLeftOfTheSearchBudget(t *testing.T) {
	f, turn := echoTurn(searchTool{})
	f.SetStop(llm.StopPauseTurn)
	f.SetServerUses(map[string]int{searchTool{}.Name(): 3})
	rec := &recording{}

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, 2, f.Asked())
	assert.Equal(t, []llm.NativeOffer{{Tool: searchTool{}, Server: true, MaxUses: 2}}, f.LastRequest().NativeTools)
}

// A spent budget is not offered at all, since no wire takes a cap of zero.
func TestASpentSearchBudgetIsNotOffered(t *testing.T) {
	f, turn := echoTurn(echoTool{name: "echo"}, searchTool{})
	f.SetToolCalls(llm.StagedCall("echo", `{}`))
	f.SetServerUses(map[string]int{searchTool{}.Name(): 5})
	rec := &recording{}

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, 2, f.Asked())
	assert.Empty(t, f.LastRequest().NativeTools)
}

// A pause is not resumed once a server tool the turn offered is spent: the
// request that resumed it would not offer that tool, and would go without the
// paused reply's calls of it. The turn settles on the pause.
func TestAPauseIsNotResumedOnceAServerToolIsSpent(t *testing.T) {
	f, turn := echoTurn(searchTool{})
	f.SetStop(llm.StopPauseTurn, llm.StopPauseTurn)
	f.SetServerUses(map[string]int{searchTool{}.Name(): 5})
	rec := &recording{}

	res, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, 1, f.Asked())
	assert.Equal(t, llm.StopPauseTurn, res.StopReason)
}

// A reply whose usage reports no count spends a search per call it holds.
func TestUnreportedSearchesAreCountedByTheirCalls(t *testing.T) {
	f, turn := echoTurn(searchTool{})
	f.SetReply(
		llm.Chunk{Kind: llm.ChunkServer, Call: searchCall("srv_1", "kubernetes 1.36")},
		llm.Chunk{Kind: llm.ChunkServer, Call: searchCall("srv_2", "kubernetes 1.35")},
		llm.Chunk{Text: "Still looking."},
	)
	f.SetStop(llm.StopPauseTurn)
	rec := &recording{}

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, []llm.NativeOffer{{Tool: searchTool{}, Server: true, MaxUses: 3}}, f.LastRequest().NativeTools)
}

// A turn resumes at most maxPauses times and then settles on the pause.
func TestPausesAreCapped(t *testing.T) {
	f, turn := fakeTurn()
	f.SetStop(llm.StopPauseTurn, llm.StopPauseTurn, llm.StopPauseTurn, llm.StopPauseTurn, llm.StopPauseTurn)
	rec := &recording{}

	res, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, 1+maxPauses, f.Asked())
	assert.Equal(t, llm.StopPauseTurn, res.StopReason)
	assert.Empty(t, rec.answers)
}

// A pause that also asked for a call is not the provider's own loop: the call is
// answered not-run and the turn ends there, as under any other stop.
func TestAPauseWithACallIsNotResumed(t *testing.T) {
	f, turn := echoTurn(echoTool{name: "echo"})
	f.SetToolCalls(llm.StagedCall("echo", `{}`))
	f.SetStop(llm.StopPauseTurn)
	rec := &recording{}

	res, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, 1, f.Asked())
	assert.Equal(t, llm.StopPauseTurn, res.StopReason)
	assert.Equal(t, []Code{CodeNotRun}, rec.codes())
}

// The recorder is told of each server call once, as the stream shows it and
// before the progress that carries it, so a query is on record however the
// reply ends.
func TestAServerCallIsToldAsItIsSeen(t *testing.T) {
	f, turn := echoTurn(searchTool{})
	f.SetReply(
		llm.Chunk{Text: "Let me search."},
		llm.Chunk{Kind: llm.ChunkServer, Call: searchCall("srv_1", "kubernetes 1.36")},
		llm.Chunk{Text: "It shipped."},
	)
	rec := &recording{}

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, []llm.Block{searchCall("srv_1", "kubernetes 1.36")}, rec.serverCalls)
	assert.Equal(t, []llm.NativeTool{searchTool{}}, rec.serverTools)
	assert.Equal(t, []string{"started", "first-chunk", "progress", "server-call", "progress", "progress", "finished", "settled"}, rec.log)
	assert.Contains(t, rec.progress[1], searchCall("srv_1", "kubernetes 1.36"))
}

// The recorder is told the reply whole, so it can keep what the provider
// counted beside the stop reason.
func TestTheRecorderIsToldTheWholeResponse(t *testing.T) {
	f, turn := fakeTurn()
	f.SetServerUses(map[string]int{searchTool{}.Name(): 2})
	rec := &recording{}

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	require.Len(t, rec.finishedWith, 1)
	assert.Equal(t, "end_turn", rec.finishedWith[0].StopReason)
	assert.Equal(t, map[string]int{searchTool{}.Name(): 2}, rec.finishedWith[0].ServerUses)
}

// A finish write that failed under a reply that asked for nothing is returned for
// the caller to log, and the turn settles as it would have: the settle heals the
// row, and a clean answer is not a failed one.
func TestARefusedFinishStillReturnsTheAnswer(t *testing.T) {
	finishErr := errors.New("row gone")
	rec := &recording{finishErr: finishErr}

	_, turn := fakeTurn()

	res, err := Run(t.Context(), turn, rec, rec)

	assert.ErrorIs(t, err, finishErr)
	assert.Equal(t, Result{Blocks: answered.Blocks, StopReason: "end_turn"}, res)
	assert.NoError(t, rec.settledErr, "the turn's outcome is the settle's")
}

// A turn with no prompt of its own still tells the model what it can do and that
// data is not instructions: those sections are the agent's, not chat's.
func TestARunWithNoSystemSendsTheAgentsSectionsAlone(t *testing.T) {
	f, turn := fakeTurn()
	turn.SystemPrompt = ""

	_, err := Run(t.Context(), turn, &recording{}, &recording{})

	require.NoError(t, err)
	require.Equal(t, 1, f.Asked())
	assert.Equal(t, SystemPrompt(turn), f.LastRequest().SystemPrompt)
	assert.True(t, strings.HasPrefix(f.LastRequest().SystemPrompt, "# What you can do"))
}

// echoTurn is fakeTurn with the test tool in its box.
func echoTurn(offered ...tools.Tool) (*llm.Fake, Turn) {
	f, turn := fakeTurn()
	turn.Tools = tools.NewBox(offered)
	return f, turn
}

// A staged call is run and answered inside the same turn: the second model call
// carries the round as the last assistant message, and the turn ends on the reply
// that followed.
func TestAToolCallIsRunAndAnsweredOnTheSameTurn(t *testing.T) {
	f, turn := echoTurn(echoTool{name: "echo"})
	f.SetToolCalls(llm.StagedCall("echo", `{"say":"hi"}`))
	rec := &recording{}

	res, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, 2, f.Asked())
	use := llm.ToolUseBlock("call-1", "echo", []byte(`{"say":"hi"}`))
	round := append(llm.AnswerBlocks(thought, twelvePods), use, llm.ToolResultBlock("call-1", `{"say":"hi"}`, false))
	assert.Equal(t, Result{
		Blocks:     append(slices.Clone(round), llm.AnswerBlocks(secondThought, secondSentence)...),
		StopReason: "end_turn",
	}, res)
	assert.Equal(t, []llm.Message{
		{Role: "user", Blocks: []llm.Block{llm.TextBlock("How many?")}},
		{Role: "assistant", Blocks: round, ProviderID: "fake", Dialect: llm.DialectFake, Effort: "low"},
	}, f.LastRequest().Messages, "the rounds so far are the last assistant message")
	assert.Equal(t, []llm.Block{use}, rec.toolStarts)
	assert.Equal(t, llm.ToolDefinition{Name: "echo", Description: "says it back", InputSchema: []byte(`{"type":"object"}`)},
		f.LastRequest().Tools[0])
}

// The key names the conversation, so every round of a turn carries it.
func TestEveryRequestCarriesTheTurnsAffinityKey(t *testing.T) {
	f, turn := echoTurn(echoTool{name: "echo"})
	turn.AffinityKey = "chat-1"
	f.SetToolCalls(llm.StagedCall("echo", `{"say":"hi"}`))

	_, err := Run(t.Context(), turn, &recording{}, &recording{})

	require.NoError(t, err)
	require.Len(t, f.Requests(), 2)
	for _, req := range f.Requests() {
		assert.Equal(t, "chat-1", req.AffinityKey)
	}
}

// Every call of one reply runs and is answered before the next stream, in the
// order the model asked them, and each result is published as it lands, so a turn
// that ends inside a later call keeps the ones before it.
func TestARunOfCallsIsAnsweredInOneRound(t *testing.T) {
	f, turn := echoTurn(echoTool{name: "echo"})
	f.SetToolCalls(
		llm.StagedCall("echo", `{"n":1}`),
		llm.StagedCall("echo", `{"n":2}`),
		llm.StagedCall("echo", `{"n":3}`))
	rec := &recording{}

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, []string{`{"n":1}`, `{"n":2}`, `{"n":3}`}, answeredTexts(rec))
	assert.Equal(t, []string{"call-1", "call-2", "call-3"}, startedIDs(rec))
	assert.Equal(t, 2, f.Asked(), "one stream per round, the calls between")
	firstAnswered := slices.Concat(llm.AnswerBlocks(thought, twelvePods), toolUses(f.LastRequest().Messages[1].Blocks),
		[]llm.Block{llm.ToolResultBlock("call-1", `{"n":1}`, false)})
	assert.Contains(t, rec.progress, firstAnswered, "the first result is published before the second call runs")
}

// A name the box lacks is answered where it was asked, and nothing is started for
// it: there was nothing to run.
func TestAnUnknownToolBesideARunningOneKeepsItsPlace(t *testing.T) {
	f, turn := echoTurn(echoTool{name: "echo"})
	f.SetToolCalls(llm.StagedCall("nosuch", `{}`), llm.StagedCall("echo", `{"n":2}`))
	rec := &recording{}

	res, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, []Code{CodeUnknownTool, ""}, rec.codes())
	assert.Equal(t, []string{"call-2"}, startedIDs(rec))
	assertEveryToolUseHasItsResult(t, res)
}

// The budget counts calls, not rounds: a reply asking for more than remain is
// refused whole and gets one more reply, and a second such reply ends the turn.
func TestTheBudgetCountsCalls(t *testing.T) {
	f, turn := echoTurn(echoTool{name: "echo"})
	turn.MaxToolCalls = 3
	f.RepeatToolCalls(llm.StagedCall("echo", `{"n":1}`), llm.StagedCall("echo", `{"n":2}`))
	rec := &recording{}

	res, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, 3, f.Asked())
	assert.Equal(t, []Code{"", "", CodeBudget, CodeBudget, CodeBudget, CodeBudget}, rec.codes())
	assert.Equal(t, llm.StopToolUse, res.StopReason)
	assertEveryToolUseHasItsResult(t, res)
}

// Every round that continues on a call spends at least one, so a turn streams at
// most MaxToolCalls + 2 times: the budget's rounds, the refusal, and the
// synthesis. Pauses add at most maxPauses more (TestPausesAreCapped).
func TestTheStreamCountIsBounded(t *testing.T) {
	f, turn := echoTurn(echoTool{name: "echo"})
	turn.MaxToolCalls = 3
	f.RepeatToolCalls(llm.StagedCall("echo", `{}`))
	rec := &recording{}

	res, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, turn.MaxToolCalls+2, f.Asked(), "the bound, met exactly")
	assert.Equal(t, []Code{"", "", "", CodeBudget, CodeBudget}, rec.codes())
	assert.Equal(t, llm.StopToolUse, res.StopReason)
}

// A reply that asked for a call and then stopped on something else is a call the
// cap cut off: it is answered not-run, and the turn ends on that reason.
func TestACallInASettledReplyIsAnsweredNotRun(t *testing.T) {
	f, turn := echoTurn(echoTool{name: "echo"})
	f.SetToolCalls(llm.StagedCall("echo", `{}`))
	f.SetStop("max_tokens")
	rec := &recording{}

	res, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, 1, f.Asked())
	assert.Equal(t, []Code{CodeNotRun}, rec.codes())
	assert.Empty(t, rec.toolStarts)
	assert.Equal(t, "max_tokens", res.StopReason)
	assertEveryToolUseHasItsResult(t, res)
}

// A reply that stopped on tool_use with nothing to run is malformed: the turn ends
// there rather than asking again forever.
func TestAMalformedToolUseReplyEndsTheTurn(t *testing.T) {
	f, turn := echoTurn(echoTool{name: "echo"})
	f.SetStop(llm.StopToolUse)
	rec := &recording{}

	res, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, 1, f.Asked())
	assert.Empty(t, rec.answers)
	assert.Equal(t, llm.StopToolUse, res.StopReason)
}

// A tool that answers its own deadline is answered timeout, and the turn goes on:
// the bound is the call's, not the turn's.
func TestATimeoutIsNotACancel(t *testing.T) {
	ctx := t.Context()
	f, turn := echoTurn(echoTool{name: "echo", run: func(ctx context.Context, _ json.RawMessage) (string, bool) {
		<-ctx.Done()
		return "the tool gave up", true
	}})
	turn.DefaultToolTimeout = time.Millisecond
	f.SetToolCalls(llm.StagedCall("echo", `{}`))
	rec := &recording{}

	res, err := Run(ctx, turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, []Code{CodeTimeout}, rec.codes())
	assert.Equal(t, 2, f.Asked(), "the turn went on")
	assert.NoError(t, ctx.Err())
	assertEveryToolUseHasItsResult(t, res)
}

// The turn's own cancel mid-call ends the turn: the call that felt it is answered
// cancelled, and so is every call of the reply that never ran.
func TestACancelMidCallAnswersWhatDidNotRun(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	running := make(chan struct{})
	f, turn := echoTurn(echoTool{name: "echo", run: func(ctx context.Context, _ json.RawMessage) (string, bool) {
		close(running)
		<-ctx.Done()
		return "the tool gave up", true
	}})
	f.SetToolCalls(llm.StagedCall("echo", `{}`), llm.StagedCall("echo", `{}`))
	rec := &recording{}
	type ret struct {
		res Result
		err error
	}
	done := make(chan ret, 1)
	go func() {
		res, err := Run(ctx, turn, rec, rec)
		done <- ret{res, err}
	}()

	testutil.Wait(t, running, "the tool to start")
	cancel()
	got := testutil.Recv(t, done, "the run to return")

	assert.ErrorIs(t, got.err, context.Canceled)
	assert.Equal(t, []Code{CodeCancelled, CodeCancelled}, rec.codes())
	assertEveryToolUseHasItsResult(t, got.res)
}

// A turn cancelled between two calls starts the second: the first's result is
// kept, and the rest of the reply is answered cancelled with nothing started.
func TestACancelBetweenCallsStartsNoMore(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	f, turn := echoTurn(echoTool{name: "echo", run: func(context.Context, json.RawMessage) (string, bool) {
		cancel()
		return "twelve", false
	}})
	f.SetToolCalls(llm.StagedCall("echo", `{}`), llm.StagedCall("echo", `{}`))
	rec := &recording{}

	res, err := Run(ctx, turn, rec, rec)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, []string{"twelve", `{"error":"cancelled"}`}, answeredTexts(rec))
	assert.Equal(t, []string{"call-1"}, startedIDs(rec))
	assertEveryToolUseHasItsResult(t, res)
}

// A result the tool produced is kept whatever the clock says: the read happened,
// and only a tool answering its deadline with an error is a timeout.
func TestAResultThatArrivedIsKeptPastTheClock(t *testing.T) {
	f, turn := echoTurn(echoTool{name: "echo", run: func(ctx context.Context, _ json.RawMessage) (string, bool) {
		<-ctx.Done()
		return "twelve", false
	}})
	turn.DefaultToolTimeout = time.Millisecond
	f.SetToolCalls(llm.StagedCall("echo", `{}`))
	rec := &recording{}

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, []string{"twelve"}, answeredTexts(rec))
	assert.Equal(t, []Code{""}, rec.codes())
}

// A start that did not land runs nothing: that call and every later one of the
// reply are answered not-run, and the turn fails with the write's error.
func TestAFailedToolStartRunsNothingOfTheReply(t *testing.T) {
	ran := 0
	f, turn := echoTurn(echoTool{name: "echo", run: func(context.Context, json.RawMessage) (string, bool) {
		ran++
		return "ok", false
	}})
	f.SetToolCalls(llm.StagedCall("echo", `{}`), llm.StagedCall("echo", `{}`), llm.StagedCall("echo", `{}`))
	rec := &recording{toolStartErr: assert.AnError, toolStartFailAt: 1}

	res, err := Run(t.Context(), turn, rec, rec)

	assert.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, 1, ran)
	assert.Equal(t, []Code{"", CodeNotRun, CodeNotRun}, rec.codes())
	assert.Equal(t, 1, f.Asked(), "no round follows a write that did not land")
	assertEveryToolUseHasItsResult(t, res)
}

// A finish that did not land keeps the result it was told: the row is healed at
// settlement, and every later call of the reply is answered not-run.
func TestAFailedToolFinishKeepsTheResultAndStopsTheReply(t *testing.T) {
	f, turn := echoTurn(echoTool{name: "echo"})
	f.SetToolCalls(llm.StagedCall("echo", `{"n":1}`), llm.StagedCall("echo", `{"n":2}`))
	rec := &recording{toolFinishErr: assert.AnError}

	res, err := Run(t.Context(), turn, rec, rec)

	assert.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, []Code{"", CodeNotRun}, rec.codes())
	assert.Equal(t, []string{`{"n":1}`, `{"error":"not-run"}`}, answeredTexts(rec))
	assert.Equal(t, []string{"call-1"}, startedIDs(rec), "nothing started for the call behind it")
	assertEveryToolUseHasItsResult(t, res)
}

// A finish write that failed stops the turn only where something external would
// have followed it.
func TestAFailedCallFinishStopsOnlyAReplyThatAsksForTools(t *testing.T) {
	t.Run("the second round's", func(t *testing.T) {
		f, turn := echoTurn(echoTool{name: "echo"})
		f.SetToolCalls(llm.StagedCall("echo", `{}`))
		rec := &recording{finishErr: assert.AnError, finishFailAt: 1}

		res, err := Run(t.Context(), turn, rec, rec)

		assert.ErrorIs(t, err, assert.AnError)
		assert.NoError(t, rec.settledErr, "the second reply asked for nothing")
		assert.Equal(t, 2, f.Asked())
		assert.Equal(t, "end_turn", res.StopReason)
	})

	t.Run("a reply that asked", func(t *testing.T) {
		f, turn := echoTurn(echoTool{name: "echo"})
		f.SetToolCalls(llm.StagedCall("echo", `{}`))
		rec := &recording{finishErr: assert.AnError}

		res, err := Run(t.Context(), turn, rec, rec)

		assert.ErrorIs(t, err, assert.AnError)
		assert.Equal(t, []Code{CodeNotRun}, rec.codes())
		assert.Empty(t, rec.toolStarts)
		assert.Equal(t, 1, f.Asked())
		assert.Equal(t, llm.StopToolUse, res.StopReason, "the reply is in the record")
	})

	t.Run("a reply that did not", func(t *testing.T) {
		f, turn := echoTurn(echoTool{name: "echo"})
		rec := &recording{finishErr: assert.AnError}

		res, err := Run(t.Context(), turn, rec, rec)

		assert.ErrorIs(t, err, assert.AnError)
		assert.Equal(t, Result{Blocks: answered.Blocks, StopReason: "end_turn"}, res)
		assert.Equal(t, 1, f.Asked())
	})
}

// Mid-turn, progress is the rounds so far then the reply in flight: the reply's
// calls once it lands, since no chunk carried them, then the round whole once its
// results are in.
func TestProgressCarriesTheRoundsThenTheReplyInFlight(t *testing.T) {
	f, turn := echoTurn(echoTool{name: "echo"})
	f.SetToolCalls(llm.StagedCall("echo", `{"say":"hi"}`))
	rec := &recording{}

	res, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	use := llm.ToolUseBlock("call-1", "echo", []byte(`{"say":"hi"}`))
	round := append(llm.AnswerBlocks(thought, twelvePods), use, llm.ToolResultBlock("call-1", `{"say":"hi"}`, false))
	streamed := len(strings.Fields(thought)) + len(strings.Fields(twelvePods))
	assert.Equal(t, append(llm.AnswerBlocks(thought, twelvePods), use), rec.progress[streamed], "the reply with its call, before it runs")
	assert.Equal(t, round, rec.progress[streamed+1], "the round whole, once its result is in")
	assert.Equal(t, res.Blocks, rec.progress[len(rec.progress)-1], "the last snapshot is the answer")
}

// A tools.Shown tool's action is told with its result, and none with a refusal:
// a view shows what the run resolved, never what the arguments say.
func TestAShownToolsActionIsToldWithItsResult(t *testing.T) {
	f, turn := echoTurn(shownTool{echoTool{name: "view", run: func(_ context.Context, input json.RawMessage) (string, bool) {
		return "opened", !bytes.Contains(input, []byte("ok"))
	}}})
	f.SetToolCalls(llm.StagedCall("view", `{"ok":true}`), llm.StagedCall("view", `{}`))
	rec := &recording{}

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	require.Len(t, rec.shown, 2)
	assert.Equal(t, &tools.Action{Description: "opened", Command: &tools.CommandAction{Text: `{"ok":true}`}}, rec.shown[0])
	assert.Nil(t, rec.shown[1])
}

// The order of a two-round turn, whole.
func TestTheRecorderIsToldInOrder(t *testing.T) {
	f, turn := echoTurn(echoTool{name: "echo"})
	f.SetReply(llm.Chunk{Text: "looking"})
	f.SetToolCalls(llm.StagedCall("echo", `{}`))
	rec := &recording{}

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"started", "first-chunk", "progress", "finished", "progress", "tool-started", "tool-finished", "progress",
		"started", "first-chunk", "progress", "finished", "settled",
	}, compress(rec.log))
	assert.Equal(t, 1, rec.settled)
}

// The recorder is told of a call's first text or thinking chunk once, before
// the Progress that carries it. A server call is neither, so a reply that
// streams nothing else is never told.
func TestTheRecorderIsToldTheFirstChunkOnce(t *testing.T) {
	f, turn := echoTurn(searchTool{})
	f.SetReply(llm.Chunk{Kind: llm.ChunkServer, Call: searchCall("srv_1", "kubernetes 1.36")})
	f.SetStop(llm.StopPauseTurn)
	rec := &recording{}

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"started", "server-call", "progress", "finished",
		"started", "first-chunk", "progress", "finished", "settled",
	}, compress(rec.log))
}

// compress collapses each run of progress a word-per-chunk reply makes, so the
// assertion is the log's shape rather than its length.
func compress(log []string) []string {
	out := []string{log[0]}
	for i := 1; i < len(log); i++ {
		if log[i] == "progress" && log[i-1] == "progress" {
			continue
		}
		out = append(out, log[i])
	}
	return out
}

// answeredTexts is the text of each result, in the order the recorder was told.
func answeredTexts(rec *recording) []string {
	out := make([]string, 0, len(rec.answers))
	for _, a := range rec.answers {
		out = append(out, a.result.Text)
	}
	return out
}

// startedIDs is the id of each call that reached a tool.
func startedIDs(rec *recording) []string {
	out := make([]string, 0, len(rec.toolStarts))
	for _, c := range rec.toolStarts {
		out = append(out, c.ID)
	}
	return out
}

// assertEveryToolUseHasItsResult pins the record's one invariant: a completed row
// pairs every call with a result, in the reply's order.
func assertEveryToolUseHasItsResult(t *testing.T, res Result) {
	t.Helper()
	var want []string
	var got []string
	for _, b := range res.Blocks {
		switch b.Type {
		case llm.BlockToolUse:
			want = append(want, b.ID)
		case llm.BlockToolResult:
			got = append(got, b.ID)
		}
	}
	assert.Equal(t, want, got)
}

// A tool's own error result is kept as it is: only a tool answering the turn's
// cancel or its own deadline is rewritten as a refusal.
func TestAToolsOwnErrorIsKept(t *testing.T) {
	f, turn := echoTurn(echoTool{name: "echo", run: func(context.Context, json.RawMessage) (string, bool) {
		return "no such namespace", true
	}})
	f.SetToolCalls(llm.StagedCall("echo", `{}`))
	rec := &recording{}

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, []string{"no such namespace"}, answeredTexts(rec))
	assert.True(t, rec.answers[0].result.IsError)
}

// A finish that did not land on an unknown tool stops the reply the same way one
// on a call that ran does: nothing ran for either.
func TestAFailedFinishOnAnUnknownToolStopsTheReply(t *testing.T) {
	f, turn := echoTurn(echoTool{name: "echo"})
	f.SetToolCalls(llm.StagedCall("nosuch", `{}`), llm.StagedCall("echo", `{}`))
	rec := &recording{toolFinishErr: assert.AnError}

	_, err := Run(t.Context(), turn, rec, rec)

	assert.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, []Code{CodeUnknownTool, CodeNotRun}, rec.codes())
	assert.Empty(t, rec.toolStarts)
}

// A refusal is told once per call whatever the writes do, and a refusal whose
// write failed ends the turn once the reply is answered: no synthesis round
// follows a write that did not land.
func TestARefusedBudgetWhoseWriteFailedEndsTheTurn(t *testing.T) {
	f, turn := echoTurn(echoTool{name: "echo"})
	turn.MaxToolCalls = 1
	f.RepeatToolCalls(llm.StagedCall("echo", `{}`), llm.StagedCall("echo", `{}`))
	rec := &recording{toolFinishErr: assert.AnError}

	res, err := Run(t.Context(), turn, rec, rec)

	assert.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, []Code{CodeBudget, CodeBudget}, rec.codes())
	assert.Equal(t, 1, f.Asked())
	assertEveryToolUseHasItsResult(t, res)
}

// The rounds message says what wrote it and on which wire, so a wire replaying
// its blocks knows whose payloads they are and whether the row thought.
func TestTheRoundsMessageNamesItsProviderDialectAndEffort(t *testing.T) {
	f, turn := echoTurn(echoTool{name: "echo"})
	f.SetToolCalls(llm.StagedCall("echo", `{"say":"hi"}`))

	_, err := Run(t.Context(), turn, &recording{}, &recording{})

	require.NoError(t, err)
	last := f.LastRequest().Messages[1]
	assert.Equal(t, "fake", last.ProviderID)
	assert.Equal(t, llm.DialectFake, last.Dialect)
	assert.Equal(t, "low", last.Effort)
}

// gatedTurn is a turn offering one gated tool named bash.
func gatedTurn(tool gatedTool) (*llm.Fake, Turn) {
	tool.name = "bash"
	return echoTurn(tool)
}

// Approval and Run are both handed the turn's runtime, the same value for every
// call of the turn.
func TestEveryCallIsHandedTheTurnsRuntime(t *testing.T) {
	tool := &runtimeTool{echoTool: echoTool{name: "bash"}}
	f, turn := echoTurn(tool)
	turn.Runtime = tools.Runtime{Dir: chatDir("/results/c1")}
	f.SetToolCalls(llm.StagedCall("bash", `{}`), llm.StagedCall("bash", `{}`))

	_, err := Run(t.Context(), turn, &recording{}, &recording{})

	require.NoError(t, err)
	want := []tools.Runtime{turn.Runtime, turn.Runtime}
	assert.Equal(t, want, tool.approvals)
	assert.Equal(t, want, tool.runs)
}

// A gated call is put to the approver with the tool's approval whole before
// anything runs, and runs once it answers yes.
func TestAGatedCallWaitsOnTheApprover(t *testing.T) {
	f, turn := gatedTurn(gatedTool{approval: func(in json.RawMessage) (tools.Approval, error) {
		return tools.Approval{Cwd: "/srv/app"}, nil
	}})
	f.SetToolCalls(llm.StagedCall("bash", `{"command":"ls"}`))
	rec := &recording{}

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, []tools.Approval{{Cwd: "/srv/app"}}, rec.approvals)
	assert.Less(t, slices.Index(rec.log, "approve"), slices.Index(rec.log, "tool-started"))
	assert.Equal(t, []string{`{"command":"ls"}`}, answeredTexts(rec))
}

// An approval that skips runs the call with nothing put to the approver.
func TestASkippedApprovalRunsUnasked(t *testing.T) {
	f, turn := gatedTurn(gatedTool{approval: func(json.RawMessage) (tools.Approval, error) {
		return tools.Approval{Skip: true}, nil
	}})
	f.SetToolCalls(llm.StagedCall("bash", `{"command":"ls"}`))
	rec := &recording{}

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Empty(t, rec.approvals)
	assert.NotContains(t, rec.log, "approve")
	assert.Equal(t, []string{"call-1"}, startedIDs(rec))
	assert.Equal(t, []string{`{"command":"ls"}`}, answeredTexts(rec))
}

// The recorder is told each call's approval as it starts, whether or not it
// asked, so a skipped call's row still says where and how it ran; an ungated
// call has the zero value.
func TestASkippedCallReportsItsApproval(t *testing.T) {
	for _, approval := range []tools.Approval{
		{Cwd: "/work", Sandboxed: true, Skip: true},
		{Cwd: "/srv", Sandboxed: true},
	} {
		f, turn := gatedTurn(gatedTool{approval: func(json.RawMessage) (tools.Approval, error) {
			return approval, nil
		}})
		f.SetToolCalls(llm.StagedCall("bash", `{"command":"ls"}`))
		rec := &recording{}

		_, err := Run(t.Context(), turn, rec, rec)

		require.NoError(t, err)
		assert.Equal(t, []tools.Approval{approval}, rec.startApprovals)
	}

	f, turn := echoTurn(echoTool{name: "echo"})
	f.SetToolCalls(llm.StagedCall("echo", `{}`))
	rec := &recording{}

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, []tools.Approval{{}}, rec.startApprovals)
}

// A tool that runs by its approval is handed the one its gate decided, asked
// or skipped, so what runs is what was decided whatever its session says by
// then: the network, or the folder a skip rested on. A tool that does not is
// run as ever.
func TestTheRunTakesTheGatesApproval(t *testing.T) {
	for _, approval := range []tools.Approval{
		{Cwd: "/work", Sandboxed: true, Skip: true, Network: session.NetworkTurn},
		{Cwd: "/work", Sandboxed: true, Network: session.NetworkApproved},
		{Skip: true, Folder: &session.Folder{Path: "/home/me/code", Write: true}},
	} {
		tool := &approvedTool{gatedTool: gatedTool{approval: func(json.RawMessage) (tools.Approval, error) { return approval, nil }}}
		tool.name = "bash"
		f, turn := echoTurn(tool)
		f.SetToolCalls(llm.StagedCall("bash", `{"command":"curl"}`))
		rec := &recording{}

		_, err := Run(t.Context(), turn, rec, rec)

		require.NoError(t, err)
		assert.Equal(t, []tools.Approval{approval}, tool.ran)
		assert.Equal(t, []string{`{"command":"curl"}`}, answeredTexts(rec))
	}

	f, turn := gatedTurn(gatedTool{approval: func(json.RawMessage) (tools.Approval, error) {
		return tools.Approval{Skip: true}, nil
	}})
	f.SetToolCalls(llm.StagedCall("bash", `{"command":"ls"}`))
	rec := &recording{}
	_, err := Run(t.Context(), turn, rec, rec)
	require.NoError(t, err)
	assert.Equal(t, []string{`{"command":"ls"}`}, answeredTexts(rec), "Run, for a tool with no RunApproved")
}

// The zero Approval asks, so a gated tool that forgets to decide is asked.
func TestAGatedToolAsksByDefault(t *testing.T) {
	f, turn := gatedTurn(gatedTool{approval: func(json.RawMessage) (tools.Approval, error) {
		return tools.Approval{}, nil
	}})
	f.SetToolCalls(llm.StagedCall("bash", `{"command":"ls"}`))
	rec := &recording{}

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, []tools.Approval{{}}, rec.approvals)
}

// An input the tool cannot read is refused with nothing shown and nothing started, and
// the next call goes on.
func TestABadInputIsRefusedWithoutARequest(t *testing.T) {
	f, turn := gatedTurn(gatedTool{approval: func(in json.RawMessage) (tools.Approval, error) {
		if string(in) == `{}` {
			return tools.Approval{}, errors.New("bad")
		}
		return tools.Approval{Cwd: string(in)}, nil
	}})
	f.SetToolCalls(llm.StagedCall("bash", `{}`), llm.StagedCall("bash", `{"command":"ls"}`))
	rec := &recording{}

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, []Code{CodeBadInput, ""}, rec.codes())
	assert.Equal(t, []tools.Approval{{Cwd: `{"command":"ls"}`}}, rec.approvals)
	assert.Equal(t, []string{"call-2"}, startedIDs(rec))
}

// A tool's own refusal is answered in its words, with nothing shown and nothing
// started.
func TestAToolsRefusalIsAnsweredInItsWords(t *testing.T) {
	f, turn := gatedTurn(gatedTool{approval: func(json.RawMessage) (tools.Approval, error) {
		return tools.Approval{}, &tools.Refusal{Result: `{"error":"full"}`}
	}})
	f.SetToolCalls(llm.StagedCall("bash", `{}`))
	rec := &recording{}

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Empty(t, rec.approvals)
	assert.Empty(t, startedIDs(rec))
	assert.Equal(t, []string{`{"error":"full"}`}, answeredTexts(rec))
}

// A no is answered denied, nothing started, and the next call is asked.
func TestADeniedCallIsAnsweredDenied(t *testing.T) {
	f, turn := gatedTurn(gatedTool{})
	f.SetToolCalls(llm.StagedCall("bash", `{"command":"rm"}`), llm.StagedCall("bash", `{"command":"ls"}`))
	rec := &recording{approve: func(_ context.Context, call llm.Block, _ tools.Approval) (bool, error) {
		return call.ID == "call-2", nil
	}}

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, []Code{CodeDenied, ""}, rec.codes())
	assert.Equal(t, []string{"call-2"}, startedIDs(rec))
}

// Two gated calls in one reply are asked one at a time in reply order: the second
// is not put to the user until the first is answered, and a denied first does not
// hold it.
func TestGatedCallsAreAskedOneAtATimeInReplyOrder(t *testing.T) {
	f, turn := gatedTurn(gatedTool{})
	f.SetToolCalls(llm.StagedCall("bash", `{"command":"one"}`), llm.StagedCall("bash", `{"command":"two"}`))
	var answeredWhenAsked []int
	rec := &recording{}
	rec.approve = func(_ context.Context, call llm.Block, _ tools.Approval) (bool, error) {
		answeredWhenAsked = append(answeredWhenAsked, len(rec.answers))
		return call.ID == "call-2", nil
	}

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	assert.Equal(t, []tools.Approval{{Cwd: `{"command":"one"}`}, {Cwd: `{"command":"two"}`}}, rec.approvals)
	assert.Equal(t, []int{0, 1}, answeredWhenAsked)
	assert.Equal(t, []Code{CodeDenied, ""}, rec.codes())
}

// A decision stands once it was made, whatever the clock says. A no under a cancel
// is still denied and the rest of the reply is cancelled; a yes under a cancel
// never starts.
func TestADenialUnderACancelIsStillDenied(t *testing.T) {
	for name, approve := range map[string]bool{"a denial": false, "an approval": true} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			f, turn := gatedTurn(gatedTool{})
			f.SetToolCalls(llm.StagedCall("bash", `{"command":"one"}`), llm.StagedCall("bash", `{"command":"two"}`))
			rec := &recording{approve: func(context.Context, llm.Block, tools.Approval) (bool, error) {
				cancel()
				return approve, nil
			}}

			res, err := Run(ctx, turn, rec, rec)

			assert.ErrorIs(t, err, context.Canceled)
			first := CodeDenied
			if approve {
				first = CodeCancelled
			}
			assert.Equal(t, []Code{first, CodeCancelled}, rec.codes())
			assert.Empty(t, rec.toolStarts)
			assert.Len(t, rec.approvals, 1, "nothing more is asked")
			assertEveryToolUseHasItsResult(t, res)
		})
	}
}

// A cancel that ends the wait answers the call and the rest cancelled, nothing
// started.
func TestACancelDuringTheWaitAnswersCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	f, turn := gatedTurn(gatedTool{})
	f.SetToolCalls(llm.StagedCall("bash", `{"command":"one"}`), llm.StagedCall("bash", `{"command":"two"}`))
	rec := &recording{approve: func(ctx context.Context, _ llm.Block, _ tools.Approval) (bool, error) {
		cancel()
		<-ctx.Done()
		return false, ctx.Err()
	}}

	res, err := Run(ctx, turn, rec, rec)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, []Code{CodeCancelled, CodeCancelled}, rec.codes())
	assert.Empty(t, rec.toolStarts)
	assertEveryToolUseHasItsResult(t, res)
}

// An approval write that did not land runs nothing more: the call and the rest are
// refused not-run and the turn ends there.
func TestAFailedApprovalWriteRefusesTheRest(t *testing.T) {
	f, turn := gatedTurn(gatedTool{})
	f.SetToolCalls(llm.StagedCall("bash", `{"command":"one"}`), llm.StagedCall("bash", `{"command":"two"}`))
	disk := errors.New("disk")
	rec := &recording{approve: func(context.Context, llm.Block, tools.Approval) (bool, error) { return false, disk }}

	res, err := Run(t.Context(), turn, rec, rec)

	assert.ErrorIs(t, err, disk)
	assert.Equal(t, []Code{CodeNotRun, CodeNotRun}, rec.codes())
	assert.Empty(t, rec.toolStarts)
	assert.Equal(t, 1, f.Asked(), "the turn ended on the failed write")
	assertEveryToolUseHasItsResult(t, res)
}

// A refusal's text reads back as its code, so a caller keys on the constant rather
// than on a spelling of its own; a text that is not one of the loop's refusals is
// not read as one, a tool's own error object included.
func TestARefusalReadsBackAsItsCode(t *testing.T) {
	for _, c := range []Code{CodeBudget, CodeNotRun, CodeUnknownTool, CodeTimeout, CodeCancelled, CodeBadInput, CodeDenied} {
		assert.JSONEq(t, `{"error":"`+string(c)+`"}`, c.Text())
		got, ok := RefusalOf(c.Text())
		assert.True(t, ok, c)
		assert.Equal(t, c, got)
	}
	for _, text := range []string{
		`{"error":"no such namespace"}`,
		`{"error":"denied","detail":"x"}`,
		`{"error":""}`,
		`no such namespace`,
		`[]`,
		``,
	} {
		_, ok := RefusalOf(text)
		assert.False(t, ok, text)
	}
}

// boundedTool asks for a bound of its own, read off each call's input.
type boundedTool struct {
	echoTool
	bound func(input json.RawMessage) time.Duration
}

func (b boundedTool) CallTimeout(input json.RawMessage) time.Duration { return b.bound(input) }

// A tool with a bound of its own runs each call under the bound it names for that
// call's input, rather than the turn's.
func TestABoundedToolRunsUnderItsOwnDeadline(t *testing.T) {
	deadlines := map[string]time.Time{}
	f, turn := echoTurn(boundedTool{
		bound: func(input json.RawMessage) time.Duration {
			if string(input) == `{"long":true}` {
				return time.Hour
			}
			return 2 * time.Hour
		},
		echoTool: echoTool{name: "slow", run: func(ctx context.Context, input json.RawMessage) (string, bool) {
			deadlines[string(input)], _ = ctx.Deadline()
			return "ok", false
		}},
	})
	turn.DefaultToolTimeout = time.Millisecond
	f.SetToolCalls(llm.StagedCall("slow", `{"long":true}`), llm.StagedCall("slow", `{"long":false}`))
	rec := &recording{}

	_, err := Run(t.Context(), turn, rec, rec)

	require.NoError(t, err)
	one, two := time.Until(deadlines[`{"long":true}`]), time.Until(deadlines[`{"long":false}`])
	assert.Greater(t, one, 30*time.Minute, "the turn's millisecond did not apply")
	assert.LessOrEqual(t, one, time.Hour)
	assert.Greater(t, two, 90*time.Minute, "each call has its own input's bound")
}
