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

package chat

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/loop"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/rawjson"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/anthropicwebsearch"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/bash"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/taskstop"
)

// A turn reserved just as the service stops never runs: its rows are written, and
// the next start fails the run as stranded. It still releases the chat's slot, so
// nothing is left holding a chat that has no goroutine behind it.
func TestATurnReservedAsTheServiceStopsIsAbandoned(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, s.db, aChat("1", now))
	turn := seedTurn(t, s.db, c.ID, now)
	tr, err := s.reserveTurn(c.ID, turn.Run, fakeTarget(s))
	require.NoError(t, err)
	msgs, err := listMessages(t.Context(), s.store.Stmts(), c.ID, testReaders)
	require.NoError(t, err)
	require.NoError(t, s.stop(t.Context()))

	s.startTurn(tr, msgs[1])

	testutil.Wait(t, tr.done, "the abandoned turn")
	assert.Nil(t, s.turnOf(c.ID))
	assert.Zero(t, fakeOf(s).Asked())
	assert.Equal(t, RunQueued, runStatusOf(t, s.db, turn.Run))
}

// A message with no blocks is left out of the history: no wire accepts one, and an
// answer cancelled before its first chunk is exactly that row.
func TestTheHistoryLeavesOutAMessageWithNoBlocks(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, s.db, aChat("1", now))
	seeded := seedTurn(t, s.db, c.ID, now) // its answer is still emptyContent
	settleSeededRun(t, s.db, seeded.Run, RunCancelled, now)

	msg := send(t, s, &c.ID, "1", "and?")
	awaitSettled(t, s, c.ID, msg.ID)

	assert.Equal(t, []llm.Message{
		{Role: "user", Blocks: []llm.Block{llm.TextBlock("hi")}},
		{Role: "user", Blocks: []llm.Block{llm.ContextBlock(s.withWorkspace("", c.ID)), llm.TextBlock("and?")}},
	}, fakeOf(s).LastRequest().Messages)
}

// A content column that will not read back fails the turn before the model is
// asked: the model is never shown a transcript with a message missing from it.
// JSON that is not a list of blocks is as unreadable as text that is not JSON.
func TestATurnWhoseHistoryWillNotReadAsksNoModel(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{
		"not json":   `not json`,
		"not a list": `{"type":"text"}`,
	} {
		t.Run(name, func(t *testing.T) {
			s := newTestService(t)
			now := time.UnixMilli(1_000).UTC()
			c := seedChat(t, s.db, aChat("1", now))
			seeded := seedTurn(t, s.db, c.ID, now)
			settleSeededRun(t, s.db, seeded.Run, RunSucceeded, now)
			_, err := s.db.Write.Exec(`UPDATE messages SET content = ? WHERE id = ?`, content, string(seeded.Assistant))
			require.NoError(t, err)

			msg := send(t, s, &c.ID, "1", "and?")
			got := awaitSettled(t, s, c.ID, msg.ID)

			assert.Equal(t, StatusFailed, got.Status)
			assert.Zero(t, fakeOf(s).Asked())
			assert.Zero(t, tableCount(t, s.db, "llm_calls"), "no call was opened")
		})
	}
}

// LLMCallStarted claims nothing under a context that is already cancelled, so the run
// settles cancelled from queued rather than opening a call nobody will close.
func TestLLMCallStartedClaimsNothingOnACancelledContext(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, s.db, aChat("1", now))
	turn := seedTurn(t, s.db, c.ID, now)
	tr, err := s.reserveTurn(c.ID, turn.Run, fakeTarget(s))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	assert.ErrorIs(t, tr.LLMCallStarted(ctx, "fake"), context.Canceled)

	assert.Empty(t, tr.llmCalls)
	assert.Equal(t, RunQueued, runStatusOf(t, s.db, turn.Run))
	assert.Zero(t, tableCount(t, s.db, "llm_calls"))
}

// A cancel that reaches the history read as the store's own error settles
// cancelled, not failed with the driver's text.
func TestACancelThatInterruptsAReadSettlesCancelled(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, s.db, aChat("1", now))
	seeded := seedTurn(t, s.db, c.ID, now)
	tr, err := s.reserveTurn(c.ID, seeded.Run, fakeTarget(s))
	require.NoError(t, err)
	tr.cancel()

	tr.settle(loop.Result{}, errors.New("list messages: begin: interrupted (9)"))

	assert.Equal(t, RunCancelled, tr.status)
	assert.Empty(t, tr.errText)
	assert.Equal(t, RunCancelled, runStatusOf(t, s.db, seeded.Run))
}

// A settle that cannot land leaves the run unfinished, whichever of its writes the
// store refuses: the next start is what fails it, as stranded. Which unfinished
// status it is left at is the refused write's own — a refused claim never left
// queued.
func TestASettleThatCannotLandLeavesTheRunUnfinished(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, trigger string
		reply         []llm.Chunk
	}{
		{name: "the content", trigger: `CREATE TRIGGER refuse BEFORE UPDATE OF content ON messages BEGIN SELECT RAISE(ABORT, 'refused'); END`},
		{name: "the run's status", trigger: `CREATE TRIGGER refuse BEFORE UPDATE OF status ON agent_runs BEGIN SELECT RAISE(ABORT, 'refused'); END`},
		{name: "the call's close", trigger: `CREATE TRIGGER refuse BEFORE UPDATE ON llm_calls BEGIN SELECT RAISE(ABORT, 'refused'); END`},
		{
			name:    "the provider's call",
			trigger: `CREATE TRIGGER refuse BEFORE INSERT ON tool_calls WHEN NEW.runs_on = 'provider' BEGIN SELECT RAISE(ABORT, 'refused'); END`,
			reply:   searchReply,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The search is offered beside a tool, as a turn offers it.
			s := startServiceWithTool(t, testTool{name: "echo"})
			if tc.reply != nil {
				fakeOf(s).SetReply(tc.reply...)
			}
			c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))
			_, err := s.db.Write.Exec(tc.trigger)
			require.NoError(t, err)

			msg := send(t, s, &c.ID, "1", "hi")
			testutil.Wait(t, s.turnOf(c.ID).retrying, "the settle to retry")
			require.NoError(t, s.stop(t.Context()))

			assert.True(t, messageStatusOf(runStatusOf(t, s.db, RunID(msg.RunID))).inFlight(),
				"the run is left for the next start to fail as stranded")
		})
	}
}

// A settle writes each call's approvals again, the call's own and its cluster
// writes; one it cannot write leaves the run unfinished as any refused write does.
// Each case arms the trigger while its call is held, after the approval's own
// writes and before the settle's.
func TestASettleThatCannotRewriteAnApprovalLeavesTheRunUnfinished(t *testing.T) {
	t.Parallel()

	const refuseApprovals = `CREATE TRIGGER refuse BEFORE UPDATE ON approvals BEGIN SELECT RAISE(ABORT, 'refused'); END`
	for name, start := range map[string]func(t *testing.T, hold chan struct{}) (*service, ChatMessage){
		"the call's own": func(t *testing.T, hold chan struct{}) (*service, ChatMessage) {
			ran := make(chan struct{}, 1)
			s := startServiceWithTool(t, &fakeBash{run: func(context.Context, string) (string, bool) {
				ran <- struct{}{}
				<-hold
				return "ran", false
			}})
			fakeOf(s).SetToolCalls(bashCall("ls"))
			msg := send(t, s, nil, "1", "hi")
			_, id := awaitRequest(t, s, msg)
			approve(t, s, id, true)
			testutil.Wait(t, ran, "the command to run")
			return s, msg
		},
		"a cluster write": func(t *testing.T, hold chan struct{}) (*service, ChatMessage) {
			s := startServiceWithTool(t, recorderTool{testTool: testTool{name: "Recorder"}, hold: hold})
			fakeOf(s).SetToolCalls(llm.StagedCall("Recorder", `{}`))
			msg := send(t, s, nil, "1", "hi")
			awaitWrite(t, s, msg, ApprovalRefused)
			return s, msg
		},
	} {
		t.Run(name, func(t *testing.T) {
			hold := make(chan struct{})
			s, msg := start(t, hold)
			_, err := s.db.Write.Exec(refuseApprovals)
			require.NoError(t, err)
			close(hold)

			testutil.Wait(t, s.turnOf(msg.ChatID).retrying, "the settle to retry")
			require.NoError(t, s.stop(t.Context()))

			assert.True(t, messageStatusOf(runStatusOf(t, s.db, msg.RunID)).inFlight(),
				"the run is left for the next start to fail as stranded")
		})
	}
}

// A turn that cannot read its chat, or finds none, fails before its model is
// asked: the chat's cluster is what its tools reach. A send that creates its chat
// reads no chat row, so the swap reaches the turn's read alone.
func TestATurnThatCannotReadItsChatFails(t *testing.T) {
	t.Parallel()

	for name, swap := range map[string]string{
		"a read that fails": `SELECT 1, 2`,
		"no row":            `SELECT 1 WHERE 0`,
	} {
		t.Run(name, func(t *testing.T) {
			s := startServiceOn(t, swapped(map[stmtID]string{stmtSelectChat: swap}))

			msg := send(t, s, nil, "1", "hi")

			assert.Equal(t, StatusFailed, awaitSettled(t, s, msg.ChatID, msg.ID).Status)
			assert.Zero(t, fakeOf(s).Asked())
		})
	}
}

// A provider that refused to read the chat settles the answer and its run in the
// one spelling the transcript draws, never the provider's own words. The call's
// row keeps the provider's error, code included.
func TestTheProvidersOverflowSettlesWithOneSentence(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	fakeOf(s).FailAfter(0, llm.ResponseError(llm.Provider{ID: "fake"}, 400, "invalid_request_error", llm.CodeContextLengthExceeded))
	msg := send(t, s, nil, "1", "hi")

	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, StatusFailed, got.Status)
	assert.Equal(t, contextFullText, got.Error)
	assert.Equal(t, new(FailureContextFull), got.Failure())
	var runErr string
	require.NoError(t, s.db.Read.QueryRow(`SELECT error FROM agent_runs WHERE id = ?`, string(msg.RunID)).Scan(&runErr))
	assert.Equal(t, contextFullText, runErr)
	assert.Equal(t, "fake: 400 invalid_request_error context_length_exceeded", llmCallOf(t, s.db, msg.RunID).err)
}

// A plan's usage limit settles the answer in the transcript's own words and with
// its kind, the call's row keeping the provider's code. Nothing retries.
func TestAUsageLimitSettlesWithOneSentence(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	plan := llm.Provider{ID: "fake", Codes: map[string]llm.CodeClass{"subscription_sharing_usage_limit_exceeded": llm.CodeUsageLimit}}
	fakeOf(s).FailAfter(0, llm.ResponseError(plan, 429, "", "subscription_sharing_usage_limit_exceeded"))
	msg := send(t, s, nil, "1", "hi")

	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, StatusFailed, got.Status)
	assert.Equal(t, usageLimitText, got.Error)
	assert.Equal(t, new(FailureUsageLimit), got.Failure())
	assert.Equal(t, "fake: 429 subscription_sharing_usage_limit_exceeded", llmCallOf(t, s.db, msg.RunID).err)
	assert.Equal(t, 1, fakeOf(s).Asked())
}

// A row that does not parse is left as it stands: a reader is owed its content,
// not a second way for a stored row to fail.
func TestContentForReaderLeavesARowItCannotParse(t *testing.T) {
	t.Parallel()

	broken := rawjson.RawJSON(`[{"type":"text","payload":]`)

	content, citations := contentForReader(broken, llm.DialectFake)

	assert.Equal(t, broken, content)
	assert.Equal(t, emptyCitations, citations)
}

// A tool call's status change is a Modified frame even with the content unchanged,
// and a frame already emitted still reads as it did: every frame carries the
// string it was built with.
func TestAToolCallChangeIsAModifiedFrame(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	s := startServiceWithTool(t, testTool{name: "echo", run: func(context.Context, json.RawMessage) (string, bool) {
		<-release
		return "ok", false
	}})
	fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{}`))
	msg := send(t, s, nil, "1", "hi")
	st, err := s.WatchMessageCollection(t.Context(), msg.ChatID)
	require.NoError(t, err)

	running := awaitFrame(t, st.Frames, func(f ChatMessageCollectionWatchFrame) bool {
		return f.Message != nil && f.Message.ID == msg.ID && strings.Contains(string(f.Message.ToolCalls), `"status":"Running"`)
	})
	seen := running.Message.ToolCalls
	close(release)
	done := awaitFrame(t, st.Frames, func(f ChatMessageCollectionWatchFrame) bool {
		return f.Message != nil && f.Message.ID == msg.ID && strings.Contains(string(f.Message.ToolCalls), `"status":"Succeeded"`)
	})

	assert.Equal(t, CollectionWatchFrameModified, done.Type)
	assert.Equal(t, seen, running.Message.ToolCalls, "an emitted frame is never mutated")
	assert.Contains(t, string(running.Message.ToolCalls), `"output":""`)
}

// The answer a send returns, and the live message after it, carry an empty list
// rather than the empty string the resolver cannot parse.
func TestEveryBuiltMessageStartsWithAnEmptyList(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	msg := send(t, s, nil, "1", "hi")
	assert.Equal(t, emptyToolCalls, msg.ToolCalls)
	got := awaitSettled(t, s, msg.ChatID, msg.ID)
	assert.Equal(t, emptyToolCalls, got.ToolCalls)
}

// searchReply is a reply that searched between two texts, as the fake streams it.
var searchReply = []llm.Chunk{
	{Text: "Let me search."},
	{Kind: llm.ChunkServer, Call: searchCall("srv_1", "kubernetes 1.36")},
	{Text: "It "},
	{Text: "shipped."},
}

// searched is the server_use block searchReply's query lands as.
var searched = searchCall("srv_1", "kubernetes 1.36")

// source is the citation a search's answer makes in these tests.
var source = llm.Citation{Type: "web_search_result_location", URL: "https://kubernetes.io/releases", Title: "Releases", CitedText: "1.36"}

// citationsOf is a served message's citations.
func citationsOf(t *testing.T, msg ChatMessage) []llm.Citation {
	t.Helper()
	list, err := msg.CitationList()
	require.NoError(t, err)
	citations := make([]llm.Citation, 0, len(list))
	for _, c := range list {
		citations = append(citations, *c)
	}
	return citations
}

// A tools.Shown tool's call is served with the action its run resolved, on the
// live message and the stored read alike, in place of what its arguments say;
// a call of it that was refused shows what the arguments say.
func TestAShownCallIsServedWithWhatItsRunResolved(t *testing.T) {
	s := startServiceWithTool(t, shownTool{testTool{name: "view", run: func(_ context.Context, input json.RawMessage) (string, bool) {
		return "opened", !bytes.Contains(input, []byte("ok"))
	}}})
	fakeOf(s).SetToolCalls(llm.StagedCall("view", `{"ok":true}`), llm.StagedCall("view", `{}`))
	msg := send(t, s, nil, "1", "show me")

	got := awaitSettled(t, s, msg.ChatID, msg.ID)
	stored, err := s.transcript(t.Context(), msg.ChatID)
	require.NoError(t, err)

	want := &tools.Action{Description: "opened", Command: &tools.CommandAction{Text: `{"ok":true}`}}
	for _, m := range []ChatMessage{got, stored[1]} {
		calls, err := m.ToolCallList()
		require.NoError(t, err)
		require.Len(t, calls, 2)
		assert.Equal(t, want, calls[0].Action)
		assert.Equal(t, &tools.Action{Command: &tools.CommandAction{Text: `{}`}}, calls[1].Action, "the arguments' reading")
	}
}

// A gated call's row carries its tool's contract from its first write, so the
// request drawn from it shows the command.
func TestACallRowCarriesItsContractFromItsFirstWrite(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, &fakeBash{})
	fakeOf(s).SetToolCalls(llm.StagedCall(bash.Name, `{"command":"ls"}`))
	msg := send(t, s, nil, "1", "hi")

	got, id := awaitRequest(t, s, msg)
	var contract sql.NullString
	require.NoError(t, s.db.Read.QueryRow(`SELECT contract_name FROM tool_calls`).Scan(&contract))
	assert.Equal(t, string(bash.ContractName), contract.String)
	require.NotNil(t, toolCallsOf(t, got)[0].Action)
	assert.Equal(t, "ls", toolCallsOf(t, got)[0].Action.Command.Text)
	approve(t, s, id, true)
	awaitSettled(t, s, msg.ChatID, msg.ID)
}

// A call of a tool the turn was not offered is refused, and still records its
// tool's contract and shows its action; a call of a tool the app does not know
// records none.
func TestARefusedCallRecordsTheContractOfItsTool(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t)
	fakeOf(s).SetToolCalls(llm.StagedCall(bash.Name, `{"command":"ls"}`), llm.StagedCall("Nope", `{}`))
	msg := send(t, s, nil, "1", "hi")

	got := awaitSettled(t, s, msg.ChatID, msg.ID)
	rows := toolCallRows(t, s.db, msg.RunID)
	require.Len(t, rows, 2)
	assert.Equal(t, string(bash.ContractName), rows[0].contract)
	assert.Empty(t, rows[1].contract)
	calls := toolCallsOf(t, got)
	require.NotNil(t, calls[0].Action, "the refused Bash call")
	assert.Equal(t, "ls", calls[0].Action.Command.Text)
	assert.Nil(t, calls[1].Action)
}

// A search is served the way the transcript reads it: the query as the
// provider's call, and the source as the message's citation; the content keeps
// the call for the replay, without the payload a reader is never shown.
func TestASearchIsDrawnAsItsQueryAndItsSources(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: "echo"})
	fakeOf(s).SetReply(searchReply...)
	fakeOf(s).SetCitations(source)
	msg := send(t, s, nil, "1", "what shipped in 1.36?")

	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, StatusComplete, got.Status)
	assert.Equal(t, []llm.Block{llm.TextBlock("Let me search."), searched, llm.TextBlock("It shipped.")}, blocksOf(t, got))
	calls, err := got.ToolCallList()
	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Equal(t, ToolCallRunsOnProvider, calls[0].RunsOn)
	assert.Equal(t, &tools.SearchAction{Query: "kubernetes 1.36"}, calls[0].Action.Search)
	assert.Equal(t, []llm.Citation{source}, citationsOf(t, got))
}

// The citations are read from the payloads by the run's dialect, on the live
// message and the stored one alike, before the payloads are dropped; a message
// that cites nothing carries none.
func TestCitationsAreReadByTheRunsDialect(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	s := startServiceWithTool(t, testTool{name: "echo", run: func(context.Context, json.RawMessage) (string, bool) {
		<-release
		return "ok", false
	}})
	fakeOf(s).SetReply(llm.Chunk{Text: "It shipped."})
	fakeOf(s).SetCitations(source)
	fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{}`))
	msg := send(t, s, nil, "1", "what shipped in 1.36?")
	st, err := s.WatchMessageCollection(t.Context(), msg.ChatID)
	require.NoError(t, err)

	live := awaitFrame(t, st.Frames, func(f ChatMessageCollectionWatchFrame) bool {
		return f.Message != nil && f.Message.ID == msg.ID && strings.Contains(string(f.Message.Citations), "kubernetes.io")
	})
	close(release)
	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, []llm.Citation{source}, citationsOf(t, *live.Message))
	assert.Equal(t, []llm.Citation{source}, citationsOf(t, got))
	assert.NotContains(t, string(got.Content), "payload")
	assert.Equal(t, emptyCitations, msg.Citations, "the answer a send returns cites nothing yet")
	msgs, err := s.readMessages(t.Context(), msg.ChatID)
	require.NoError(t, err)
	assert.Equal(t, emptyCitations, msgs[0].Citations, "the question cites nothing")
}

// The model call's row keeps what the provider counted, a reported zero apart
// from no report, and the settle's rewrite of the row keeps it too.
func TestTheModelCallRowKeepsTheSearchCount(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		uses map[string]int
		want string
	}{
		{"one search", map[string]int{anthropicwebsearch.Name: 1}, `{"anthropic_web_search_20260318":1}`},
		{"a reported zero", map[string]int{anthropicwebsearch.Name: 0}, `{"anthropic_web_search_20260318":0}`},
		{"no report", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := startServiceWithTool(t, testTool{name: "echo"})
			fakeOf(s).SetServerUses(tc.uses)
			msg := send(t, s, nil, "1", "what shipped in 1.36?")
			awaitSettled(t, s, msg.ChatID, msg.ID)

			assert.Equal(t, tc.want, llmCallOf(t, s.db, msg.RunID).serverUses)
		})
	}
}

// A reply that broke off after its query keeps it on the Failed row, between the
// text around it: what left the machine is recorded however the reply ended.
// A search is not a first chunk, and a broken reply reports no count.
func TestAnInterruptedReplyKeepsItsQueries(t *testing.T) {
	t.Parallel()

	t.Run("failed", func(t *testing.T) {
		s := startServiceWithTool(t, testTool{name: "echo"})
		fakeOf(s).SetReply(searchReply...)
		fakeOf(s).FailAfter(3, errors.New("provider went away"))
		msg := send(t, s, nil, "1", "what shipped in 1.36?")

		got := awaitSettled(t, s, msg.ChatID, msg.ID)

		assert.Equal(t, StatusFailed, got.Status)
		assert.Equal(t, []llm.Block{llm.TextBlock("Let me search."), searched, llm.TextBlock("It ")}, blocksOf(t, got))
		assert.Equal(t, []storedToolCall{searchedRow(0)}, toolCallRows(t, s.db, msg.RunID), "the row lands with the failed call")
	})

	t.Run("cancelled", func(t *testing.T) {
		s := startServiceWithTool(t, testTool{name: "echo"})
		fakeOf(s).SetReply(llm.Chunk{Kind: llm.ChunkServer, Call: searched}, llm.Chunk{Text: "It shipped."})
		fakeOf(s).SetServerUses(map[string]int{anthropicwebsearch.Name: 1})
		fakeOf(s).SetGate(make(chan struct{}))
		msg := send(t, s, nil, "1", "what shipped in 1.36?")
		st, err := s.WatchMessageCollection(t.Context(), msg.ChatID)
		require.NoError(t, err)
		awaitFrame(t, st.Frames, func(f ChatMessageCollectionWatchFrame) bool {
			return f.Message != nil && f.Message.ID == msg.ID && strings.Contains(string(f.Message.ToolCalls), `"runsOn":"Provider"`)
		})

		require.NoError(t, s.Cancel(t.Context(), msg.ChatID))
		got := awaitSettled(t, s, msg.ChatID, msg.ID)

		assert.Equal(t, StatusCancelled, got.Status)
		assert.Equal(t, []llm.Block{searched}, blocksOf(t, got))
		assert.Equal(t, []storedToolCall{searchedRow(0)}, toolCallRows(t, s.db, msg.RunID))
		assert.Equal(t, llmCallRow{err: callCancelled, finished: true}, llmCallOf(t, s.db, msg.RunID), "no count")
	})

	t.Run("a search alone", func(t *testing.T) {
		s := startServiceWithTool(t, testTool{name: "echo"})
		fakeOf(s).SetReply(llm.Chunk{Kind: llm.ChunkServer, Call: searched})
		fakeOf(s).FailAfter(1, errors.New("provider went away"))
		msg := send(t, s, nil, "1", "what shipped in 1.36?")

		awaitSettled(t, s, msg.ChatID, msg.ID)

		assert.Zero(t, callSpansOf(t, s.db, msg.RunID)[0].firstChunk, "a search is no first chunk")
	})
}

// searchedRow is the row searched is stored as, at seq in its reply.
func searchedRow(seq int) storedToolCall {
	return storedToolCall{
		seq: seq, name: anthropicwebsearch.Name, useID: "srv_1", args: `{"query":"kubernetes 1.36"}`,
		provider: true, contract: "web_search_20260318",
	}
}

// A search is a row the moment the stream shows it: the provider's, run and
// finished, with no result of its own, and in the live list before the reply
// ends.
func TestASearchIsAProviderRow(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: "echo"})
	gate := make(chan struct{})
	fakeOf(s).SetReply(llm.Chunk{Kind: llm.ChunkServer, Call: searched}, llm.Chunk{Text: "It "}, llm.Chunk{Text: "shipped."})
	fakeOf(s).SetGate(gate)
	msg := send(t, s, nil, "1", "what shipped in 1.36?")
	st, err := s.WatchMessageCollection(t.Context(), msg.ChatID)
	require.NoError(t, err)

	live := awaitFrame(t, st.Frames, func(f ChatMessageCollectionWatchFrame) bool {
		return f.Message != nil && f.Message.ID == msg.ID && strings.Contains(string(f.Message.ToolCalls), `"runsOn":"Provider"`)
	})
	close(gate)

	calls, err := live.Message.ToolCallList()
	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Equal(t, "srv_1", calls[0].ToolUseID)
	assert.Empty(t, calls[0].Status, "the app ran nothing, so it has no status to report")
	assert.Equal(t, &tools.Action{Search: &tools.SearchAction{Query: "kubernetes 1.36"}}, calls[0].Action)
	assert.Nil(t, calls[0].Approval)
	assert.Equal(t, StatusStreaming, live.Message.Status, "shown before the reply ends")
	awaitSettled(t, s, msg.ChatID, msg.ID)
	assert.Equal(t, []storedToolCall{searchedRow(0)}, toolCallRows(t, s.db, msg.RunID))
}

// A reply's searches are seen before the tool_use blocks that end it, so a
// search that precedes a call keeps its place ahead of it.
func TestAProviderRowKeepsItsPlaceBeforeTheReplysCalls(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: "echo"})
	fakeOf(s).SetReply(llm.Chunk{Kind: llm.ChunkServer, Call: searched}, llm.Chunk{Text: "Let me check."})
	fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{}`))
	msg := send(t, s, nil, "1", "what shipped in 1.36?")

	awaitSettled(t, s, msg.ChatID, msg.ID)

	rows := toolCallRows(t, s.db, msg.RunID)
	require.Len(t, rows, 2)
	assert.Equal(t, searchedRow(0), rows[0])
	assert.Equal(t, 1, rows[1].seq)
	assert.Equal(t, "echo", rows[1].name)
	assert.False(t, rows[1].provider)
	assert.Equal(t, "echo/v1", rows[1].contract, "a tool of ours names its own")
}

// A turn offered the search is told how to use it under "What you can do",
// after the budget line and with the month its clock reads; a turn offered no
// tool is offered no search and told nothing of it.
func TestATurnAsksForTheSearchItsBoxHolds(t *testing.T) {
	t.Parallel()

	t.Run("a box", func(t *testing.T) {
		s := startServiceWithTool(t, testTool{name: "echo"})
		msg := send(t, s, nil, "1", "what shipped in 1.36?")
		awaitSettled(t, s, msg.ChatID, msg.ID)

		req := fakeOf(s).LastRequest()
		assert.Equal(t, []llm.NativeOffer{{Tool: testSearch, Server: true, MaxUses: 5}}, req.NativeTools)
		assert.Contains(t, req.SystemPrompt, "\n## Searching the web\n\n")
		assert.NotContains(t, req.SystemPrompt, "\n# Searching the web\n")
		assert.Contains(t, req.SystemPrompt, "The current month is March 2026;")
		assert.Less(t, strings.Index(req.SystemPrompt, "# What you can do"), strings.Index(req.SystemPrompt, "## Searching the web"),
			"under the agent's section, not after system.md")
	})

	t.Run("no tools", func(t *testing.T) {
		s := newTestService(t)
		msg := send(t, s, nil, "1", "what shipped in 1.36?")
		awaitSettled(t, s, msg.ChatID, msg.ID)

		req := fakeOf(s).LastRequest()
		assert.Empty(t, req.NativeTools)
		assert.NotContains(t, req.SystemPrompt, "Searching the web")
	})
}

// A search turn names everything it writes by the tool: the provider's row, the
// server_use block and the count.
func TestAWebSearchTurnWritesItsRows(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: "echo"})
	fakeOf(s).SetReply(llm.Chunk{Kind: llm.ChunkServer, Call: searched}, llm.Chunk{Text: "It shipped."})
	fakeOf(s).SetServerUses(map[string]int{testSearch.Name(): 1})
	msg := send(t, s, nil, "1", "what shipped in 1.36?")
	awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, []storedToolCall{searchedRow(0)}, toolCallRows(t, s.db, msg.RunID))
	assert.Equal(t, `{"anthropic_web_search_20260318":1}`, llmCallOf(t, s.db, msg.RunID).serverUses)
	var content string
	require.NoError(t, s.db.Read.QueryRow(`SELECT content FROM messages WHERE id = ?`, string(msg.ID)).Scan(&content))
	var blocks []llm.Block
	require.NoError(t, json.Unmarshal([]byte(content), &blocks))
	require.NotEmpty(t, blocks)
	assert.Equal(t, llm.BlockServerUse, blocks[0].Type)
	assert.Equal(t, "anthropic_web_search_20260318", blocks[0].Name)
}

// usageOf is the four token columns of the run's one call row, in order.
func usageOf(t *testing.T, s *service, run RunID) [4]sql.NullInt64 {
	t.Helper()
	var u [4]sql.NullInt64
	require.NoError(t, s.db.Read.QueryRow(`SELECT input_tokens, cache_read_tokens, cache_write_tokens, output_tokens FROM llm_calls WHERE run_id = ?`,
		string(run)).Scan(&u[0], &u[1], &u[2], &u[3]))
	return u
}

// A call's row keeps what its provider reported, the input uncached.
func TestACallRowStoresItsUsage(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	fakeOf(s).SetUsage(llm.Usage{Reported: true, InputTokens: 40, CacheReadTokens: 10, CacheWriteTokens: 5, OutputTokens: 7})

	msg := send(t, s, nil, "a", "how many pods?")
	awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, [4]sql.NullInt64{count(25), count(10), count(5), count(7)}, usageOf(t, s, msg.RunID))
}

// A call that reported nothing stores four NULLs, never four zeros: unknown is
// not zero, and a reported zero is.
func TestUnknownUsageDiffersFromZero(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	fakeOf(s).SetUsage(llm.Usage{}, llm.Usage{Reported: true})

	unknown := send(t, s, nil, "a", "how many pods?")
	awaitSettled(t, s, unknown.ChatID, unknown.ID)
	zero := send(t, s, nil, "b", "how many nodes?")
	awaitSettled(t, s, zero.ChatID, zero.ID)

	assert.Equal(t, [4]sql.NullInt64{}, usageOf(t, s, unknown.RunID))
	assert.Equal(t, [4]sql.NullInt64{count(0), count(0), count(0), count(0)}, usageOf(t, s, zero.RunID))
}

// Each model call's row names its run's provider, the model and effort, and when
// it started, first showed something, and finished; a tool call's row sits under
// the call whose reply asked for it, between the two in time.
func TestATurnsModelCallsLandWithItsRow(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: "echo"})
	stepClock(s)
	fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{}`))

	msg := send(t, s, nil, "1", "hi")
	awaitSettled(t, s, msg.ChatID, msg.ID)

	calls := callSpansOf(t, s.db, msg.RunID)
	require.Len(t, calls, 2)
	for _, c := range calls {
		assert.Equal(t, "fake", c.provider)
		assert.Equal(t, "fake", c.model)
		assert.Equal(t, "high", c.effort)
		assert.Less(t, c.started, c.firstChunk)
		assert.Less(t, c.firstChunk, c.finished)
	}
	var toolStarted int64
	require.NoError(t, s.db.Read.QueryRow(`SELECT t.started_at FROM tool_calls t JOIN llm_calls c ON c.id = t.llm_call_id
		WHERE c.run_id = ? AND c.seq = 0`, string(msg.RunID)).Scan(&toolStarted))
	assert.Less(t, calls[0].finished, toolStarted)
	assert.Less(t, toolStarted, calls[1].started)
}

// The row is on disk before the model answers: open, with no stop reason, while
// the reply streams, and closed with both once it settles.
func TestAModelCallRowExistsBeforeTheProviderIsInvoked(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	gate := make(chan struct{})
	fakeOf(s).SetGate(gate)

	msg := send(t, s, nil, "1", "hi")
	awaitLiveContent(t, s, msg.ChatID)

	assert.Equal(t, llmCallRow{}, llmCallOf(t, s.db, msg.RunID))
	close(gate)
	awaitSettled(t, s, msg.ChatID, msg.ID)
	assert.Equal(t, llmCallRow{stopReason: "end_turn", finished: true}, llmCallOf(t, s.db, msg.RunID))
}

// The first chunk is the first the reader could be shown: a thought counts, and
// a reply that streamed nothing before asking for a call has none.
func TestFirstChunkIsTheFirstDisplayableChunk(t *testing.T) {
	t.Parallel()

	t.Run("a thought", func(t *testing.T) {
		s := newTestService(t)
		fakeOf(s).SetReply(llm.Chunk{Kind: llm.ChunkThinking, Text: "Hmm."})

		msg := send(t, s, nil, "1", "hi")
		awaitSettled(t, s, msg.ChatID, msg.ID)

		assert.NotZero(t, callSpansOf(t, s.db, msg.RunID)[0].firstChunk)
	})

	t.Run("nothing before a call", func(t *testing.T) {
		s := startServiceWithTool(t, testTool{name: "echo"})
		fakeOf(s).SetReply()
		fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{}`))

		msg := send(t, s, nil, "1", "hi")
		awaitSettled(t, s, msg.ChatID, msg.ID)

		calls := callSpansOf(t, s.db, msg.RunID)
		require.Len(t, calls, 2)
		assert.Zero(t, calls[0].firstChunk, "NULL")
		assert.NotZero(t, calls[1].firstChunk)
	})
}

// The call's row names the model the provider said it served; the run keeps the
// catalog's id the send named. A call that broke off before answering keeps the
// model it asked for.
func TestTheModelCallRowNamesTheModelServed(t *testing.T) {
	t.Parallel()

	t.Run("served", func(t *testing.T) {
		s := newTestService(t)
		fakeOf(s).SetServedModel("fake-2026-09-01")

		msg := send(t, s, nil, "1", "hi")
		got := awaitSettled(t, s, msg.ChatID, msg.ID)

		assert.Equal(t, "fake-2026-09-01", callSpansOf(t, s.db, msg.RunID)[0].model)
		assert.Equal(t, "fake", got.ModelID)
	})

	t.Run("refused", func(t *testing.T) {
		s := newTestService(t)
		fakeOf(s).SetServedModel("fake-2026-09-01")
		fakeOf(s).FailAfter(0, errors.New("provider went away"))

		msg := send(t, s, nil, "1", "hi")
		awaitSettled(t, s, msg.ChatID, msg.ID)

		assert.Equal(t, "fake", callSpansOf(t, s.db, msg.RunID)[0].model)
	})
}

// A call that broke off is closed with its own error and no stop reason, and a
// cancelled one says cancelled; either keeps when it first showed something.
func TestAnInterruptedCallIsClosedWithItsError(t *testing.T) {
	t.Parallel()

	t.Run("failed", func(t *testing.T) {
		var s *service
		s = startServiceWithTool(t, testTool{name: "echo", run: func(context.Context, json.RawMessage) (string, bool) {
			fakeOf(s).FailAfter(0, errors.New("provider went away"))
			return "ok", false
		}})
		fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{}`))

		msg := send(t, s, nil, "1", "hi")
		got := awaitSettled(t, s, msg.ChatID, msg.ID)

		assert.Equal(t, "provider went away", got.Error)
		assert.Equal(t, []llmCallRow{
			{stopReason: llm.StopToolUse, finished: true},
			{err: "provider went away", finished: true},
		}, llmCallRows(t, s.db, msg.RunID))
	})

	t.Run("cancelled", func(t *testing.T) {
		s := newTestService(t)
		fakeOf(s).SetGate(make(chan struct{}))
		msg := send(t, s, nil, "1", "hi")
		awaitLiveContent(t, s, msg.ChatID)

		require.NoError(t, s.Cancel(t.Context(), msg.ChatID))
		got := awaitSettled(t, s, msg.ChatID, msg.ID)

		assert.Empty(t, got.Error, "the run's own error stays empty")
		assert.Equal(t, llmCallRow{err: callCancelled, finished: true}, llmCallOf(t, s.db, msg.RunID))
		assert.NotZero(t, callSpansOf(t, s.db, msg.RunID)[0].firstChunk)
	})
}

// refuseLiveCallCloses refuses every write to a model call's row while its run
// is live, so a round's own close fails and the settle's, after the run's
// status, lands.
const refuseLiveCallCloses = `CREATE TRIGGER refuse BEFORE UPDATE ON llm_calls
	WHEN (SELECT status FROM agent_runs WHERE id = NEW.run_id) IN ('running', 'waiting_approval')
	BEGIN SELECT RAISE(ABORT, 'refused'); END`

// A round whose close did not land stops only a reply that asked for tools:
// nothing external follows a write that did not land, so every call is answered
// not-run, the name the box lacks too. A reply that ended the turn settles as
// it would have. The settle heals the call's row either way.
func TestAFailedFinishWriteStopsOnlyAReplyThatAsksForTools(t *testing.T) {
	t.Parallel()

	t.Run("a reply asking for tools", func(t *testing.T) {
		ran := make(chan struct{}, 3)
		s := startServiceWithTool(t, testTool{name: "echo", run: func(context.Context, json.RawMessage) (string, bool) {
			ran <- struct{}{}
			return "ok", false
		}})
		_, err := s.db.Write.Exec(refuseLiveCallCloses)
		require.NoError(t, err)
		fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{"n":1}`), llm.StagedCall("echo", `{"n":2}`), llm.StagedCall("nope", `{"n":3}`))

		msg := send(t, s, nil, "1", "hi")
		got := awaitSettled(t, s, msg.ChatID, msg.ID)

		assert.Equal(t, StatusFailed, got.Status)
		assert.NotEmpty(t, got.Error)
		assert.Empty(t, ran, "nothing ran on a failed write")
		assert.Equal(t, []llmCallRow{{stopReason: llm.StopToolUse, finished: true}}, llmCallRows(t, s.db, msg.RunID), "healed by the settle")
		rows := toolCallRows(t, s.db, msg.RunID)
		require.Len(t, rows, 3)
		for i, r := range rows {
			assert.Equal(t, toolFailed, r.status)
			assert.Equal(t, loop.CodeNotRun.Text(), r.errText)
			assert.Equal(t, fmt.Sprintf(`{"n":%d}`, i+1), r.args)
			assert.False(t, r.hasStarted)
		}
	})

	t.Run("a reply that ended the turn", func(t *testing.T) {
		s := newTestService(t)
		_, err := s.db.Write.Exec(refuseLiveCallCloses)
		require.NoError(t, err)

		msg := send(t, s, nil, "1", "hi")
		got := awaitSettled(t, s, msg.ChatID, msg.ID)

		assert.Equal(t, StatusComplete, got.Status)
		assert.Equal(t, llmCallRow{stopReason: "end_turn", finished: true}, llmCallOf(t, s.db, msg.RunID))
	})
}

// The live answer carries its own run's last stop reason, as its stored row
// will; a subagent's replies, on a run of their own, leave it alone.
func TestTheTurnsOwnRunSetsTheLiveFinishReason(t *testing.T) {
	t.Parallel()

	entered, release := make(chan struct{}), make(chan struct{})
	s := startServiceWithAgent(t, testTool{name: "echo", run: func(context.Context, json.RawMessage) (string, bool) {
		close(entered)
		<-release
		return "ok", false
	}})
	subagentFake(s, "Count the pods.")
	fakeOf(s).SetToolCalls(agentCall("Count the pods."), llm.StagedCall("echo", `{}`))

	msg := send(t, s, nil, "k", "how many pods?")
	testutil.Wait(t, entered, "the echo call to run")
	awaitTasks(t, s, msg.ChatID)

	msgs, err := s.readMessages(t.Context(), msg.ChatID)
	require.NoError(t, err)
	assert.Equal(t, llm.StopToolUse, msgs[1].FinishReason, "the subagent's end_turn is its own run's")
	close(release)
	awaitSettled(t, s, msg.ChatID, msg.ID)
}

// A settle that fails is retried until the service stops, the answer shown the
// whole while and the run still running. Nothing is written, so the next start
// fails the run as stranded; the call's row, closed by its own write before the
// settle, is left as it is.
func TestASettleThatFailsUntilStopLeavesTheRunForTheNextStart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s, err := newService(openTestDB(t, dir), chatsDirIn(dir), monitorDirIn(dir), fakeLLM(), noClusterCards, nil, testReaders, noLists, sandbox.State{}, testSettings(t), nil)
	require.NoError(t, err)
	stop, err := s.Start(t.Context())
	require.NoError(t, err)
	answers := settleSeam(s)
	fakeOf(s).SetGate(make(chan struct{}))
	msg := send(t, s, nil, "1", "hi")
	awaitLiveContent(t, s, msg.ChatID)

	require.NoError(t, s.Cancel(t.Context(), msg.ChatID))
	answers <- errRefused
	answers <- errRefused

	msgs, err := s.readMessages(t.Context(), msg.ChatID)
	require.NoError(t, err)
	assert.Equal(t, StatusStreaming, msgs[1].Status)
	assert.Equal(t, marshalBlocks([]llm.Block{llm.ThinkingBlock(fakeFirstWord)}), msgs[1].Content)
	assert.Equal(t, RunRunning, runStatusOf(t, s.db, msg.RunID))
	require.NoError(t, stop(t.Context()))
	require.NoError(t, s.Close())
	assert.Equal(t, RunRunning, runStatusOf(t, s.db, msg.RunID), "nothing written")

	next := startService(t, dir)
	assert.Equal(t, RunFailed, runStatusOf(t, next.db, msg.RunID))
	assert.Equal(t, llmCallRow{err: callCancelled, finished: true}, llmCallOf(t, next.db, msg.RunID))
}

// A call's row lands as the call ends, ahead of the settle: while the settle
// fails the run is running and the call already closed, and the settle writes
// the same row again.
func TestACallRowLandsAheadOfTheSettledWrite(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	answers := settleSeam(s)
	msg := send(t, s, nil, "1", "hi")

	answers <- errRefused
	answers <- errRefused
	assert.Equal(t, RunRunning, runStatusOf(t, s.db, msg.RunID))
	before := llmCallRows(t, s.db, msg.RunID)
	assert.Equal(t, []llmCallRow{{stopReason: "end_turn", finished: true}}, before)
	answers <- nil
	awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, RunSucceeded, runStatusOf(t, s.db, msg.RunID))
	assert.Equal(t, before, llmCallRows(t, s.db, msg.RunID))
}

// A cancelled turn's live answer serves the stop reason its stored row will,
// through the retry and after it.
func TestACancelledTurnsOverlayCarriesItsFinishReason(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	s := startServiceWithTool(t, testTool{name: "echo", run: func(ctx context.Context, _ json.RawMessage) (string, bool) {
		close(entered)
		<-ctx.Done()
		return "cancelled", true
	}})
	answers := settleSeam(s)
	fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{}`))
	msg := send(t, s, nil, "1", "hi")
	testutil.Wait(t, entered, "the call to run")

	require.NoError(t, s.Cancel(t.Context(), msg.ChatID))
	answers <- errRefused
	msgs, err := s.readMessages(t.Context(), msg.ChatID)
	require.NoError(t, err)
	assert.Equal(t, StatusStreaming, msgs[1].Status)
	assert.Equal(t, llm.StopToolUse, msgs[1].FinishReason)
	answers <- nil

	got := awaitSettled(t, s, msg.ChatID, msg.ID)
	assert.Equal(t, StatusCancelled, got.Status)
	assert.Equal(t, llm.StopToolUse, got.FinishReason)
}

// A settle that lands on a retry moves the chat up the list like any other.
func TestASettleThatLandsOnRetryReachesTheListWatch(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	stepClock(s)
	answers := settleSeam(s)
	msg := send(t, s, nil, "1", "hi")
	st, err := s.WatchCollection(t.Context())
	require.NoError(t, err)
	sent := collectChatSnapshot(t, st.Frames)
	require.Len(t, sent, 1)

	answers <- errRefused
	answers <- nil

	awaitFrame(t, st.Frames, func(f ChatCollectionWatchFrame) bool {
		return f.Type == CollectionWatchFrameModified && f.Chat.ID == msg.ChatID && f.Chat.UpdatedAt.After(sent[0].UpdatedAt)
	})
}

// A read while the settle retries is served the answer as it will be stored,
// still streaming: the stored run is.
func TestAReadDuringASettleRetrySeesTheSettledContent(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	answers := settleSeam(s)
	msg := send(t, s, nil, "1", "hi")

	answers <- errRefused
	msgs, err := s.readMessages(t.Context(), msg.ChatID)
	require.NoError(t, err)
	assert.Equal(t, StatusStreaming, msgs[1].Status)
	assert.Equal(t, marshalBlocks(llm.AnswerBlocks(fakeThought, fakeSentence)), msgs[1].Content)
	answers <- nil

	got := awaitSettled(t, s, msg.ChatID, msg.ID)
	assert.Equal(t, msgs[1].Content, got.Content)
}

// Every attempt writes the outcome the first fixed, its time included, so the
// run's end and the chat's recency are the first attempt's.
func TestASettleRetryWritesTheFirstAttemptsTime(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	stepClock(s)
	write, first, attempts := s.settleWrite, make(chan time.Time, 1), 0
	s.writeBackoff = time.Millisecond
	s.settleWrite = func(ctx context.Context, tr *turn) error {
		if attempts++; attempts == 1 {
			first <- tr.at
		}
		if attempts <= 2 {
			return errRefused
		}
		return write(ctx, tr)
	}

	msg := send(t, s, nil, "1", "hi")
	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	at := testutil.Recv(t, first, "the first attempt")
	assert.Equal(t, at, got.FinishedAt.Time)
	chat, ok, err := s.Get(t.Context(), msg.ChatID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, at, chat.UpdatedAt)
}

// A stop during the retry ends it: the slot is released and the row is left
// streaming for the next start.
func TestASettleRetryReleasesTheSlotOnStop(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	answers := settleSeam(s)
	msg := send(t, s, nil, "1", "hi")
	answers <- errRefused

	require.NoError(t, s.stop(t.Context()))

	assert.Nil(t, s.turnOf(msg.ChatID))
	msgs, err := listMessages(t.Context(), s.store.Stmts(), msg.ChatID, testReaders)
	require.NoError(t, err)
	assert.Equal(t, StatusStreaming, msgs[1].Status)
}

// A retry logs its two ends alone, however long it takes: the first failure,
// and the landing with the attempts it took. The turn itself did not fail.
func TestASettleRetryLogsItsEnds(t *testing.T) {
	// CaptureLogs swaps the process's default logger, so this test stays serial.

	logs := testutil.CaptureLogs(t)
	s := newTestService(t)
	write, attempts := s.settleWrite, 0
	s.settleWrite = func(ctx context.Context, tr *turn) error {
		if attempts++; attempts <= 5 {
			return errRefused
		}
		return write(ctx, tr)
	}

	msg := send(t, s, nil, "1", "hi")
	awaitSettled(t, s, msg.ChatID, msg.ID)

	out := logs.String()
	assert.Equal(t, 1, strings.Count(out, "did not settle"), out)
	assert.Equal(t, 1, strings.Count(out, "settled after retrying"), out)
	assert.Contains(t, out, `"attempts":6`)
	assert.NotContains(t, out, "chat turn failed")
}

// deleteIn deletes the chat on a goroutine of its own, so a test can bound how
// long the delete takes.
func deleteIn(s *service, chatID ChatID) <-chan error {
	done := make(chan error, 1)
	go func() { done <- s.Delete(context.Background(), chatID) }()
	return done
}

// A delete does not wait for a settle that keeps failing: with the next attempt
// a failsafe away, it returns.
func TestDeleteDoesNotWaitForAFailingFinalWrite(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	s.settleWrite = func(context.Context, *turn) error { return errRefused }
	s.writeBackoff = testutil.Timeout
	msg := send(t, s, nil, "1", "hi")
	testutil.Wait(t, s.turnOf(msg.ChatID).retrying, "the settle to retry")

	require.NoError(t, testutil.Recv(t, deleteIn(s, msg.ChatID), "the delete"))
	assert.Zero(t, tableCount(t, s.db, "messages"))
}

// A delete whose own write fails returns at once and leaves the turn to finish
// its own settle: the answer lands as it would have, and the slot goes with it.
func TestAFailedDeleteLeavesTheTurnToSettle(t *testing.T) {
	t.Parallel()

	failDelete := func(s *service) {
		s.deleteWrite = func(context.Context, ChatID) (bool, error) { return false, errRefused }
	}

	t.Run("a clean reply", func(t *testing.T) {
		s := newTestService(t)
		failDelete(s)
		answers := settleSeam(s)
		msg := send(t, s, nil, "1", "hi")
		answers <- errRefused
		tr := s.turnOf(msg.ChatID)

		require.ErrorIs(t, testutil.Recv(t, deleteIn(s, msg.ChatID), "the delete"), errRefused)
		answers <- nil
		testutil.Wait(t, tr.done, "the turn to end")

		got := awaitSettled(t, s, msg.ChatID, msg.ID)
		assert.Equal(t, StatusComplete, got.Status)
		assert.Equal(t, marshalBlocks(llm.AnswerBlocks(fakeThought, fakeSentence)), got.Content)
		assert.Nil(t, s.turnOf(msg.ChatID))
	})

	t.Run("a reply cancelled mid-stream", func(t *testing.T) {
		s := newTestService(t)
		failDelete(s)
		answers := settleSeam(s)
		fakeOf(s).SetGate(make(chan struct{}))
		msg := send(t, s, nil, "1", "hi")
		awaitLiveContent(t, s, msg.ChatID)
		tr := s.turnOf(msg.ChatID)

		deleted := deleteIn(s, msg.ChatID)
		answers <- errRefused
		require.ErrorIs(t, testutil.Recv(t, deleted, "the delete"), errRefused)
		answers <- nil
		testutil.Wait(t, tr.done, "the turn to end")

		got := awaitSettled(t, s, msg.ChatID, msg.ID)
		assert.Equal(t, StatusCancelled, got.Status)
		assert.Equal(t, llmCallRow{err: callCancelled, finished: true}, llmCallOf(t, s.db, msg.RunID))
		assert.Nil(t, s.turnOf(msg.ChatID))
	})
}

// A failed delete returns while the settle is still trying.
func TestAFailedDeleteDoesNotWaitOnTheSettle(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	s.deleteWrite = func(context.Context, ChatID) (bool, error) { return false, errRefused }
	answers := settleSeam(s)
	msg := send(t, s, nil, "1", "hi")
	answers <- errRefused

	require.ErrorIs(t, testutil.Recv(t, deleteIn(s, msg.ChatID), "the delete"), errRefused)
	answers <- errRefused
	answers <- nil
	awaitSettled(t, s, msg.ChatID, msg.ID)
}

// A delete that lands during the retry ends it: the retry's attempts would find
// nothing to write, the slot is released, and no row comes back.
func TestADeleteDuringASettleRetryEndsIt(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	s.settleWrite = func(context.Context, *turn) error { return errRefused }
	msg := send(t, s, nil, "1", "hi")
	tr := s.turnOf(msg.ChatID)
	testutil.Wait(t, tr.retrying, "the settle to retry")

	require.NoError(t, testutil.Recv(t, deleteIn(s, msg.ChatID), "the delete"))
	testutil.Wait(t, tr.done, "the retry to end")

	assert.Nil(t, s.turnOf(msg.ChatID))
	assert.Zero(t, tableCount(t, s.db, "chats"))
	assert.Zero(t, tableCount(t, s.db, "messages"))
}

// Stop joins a settle attempt in flight rather than pulling the store out from
// under it.
func TestStopWaitsForASettleWriteInFlight(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	entered, release := make(chan struct{}), make(chan struct{})
	attempts := 0
	s.settleWrite = func(context.Context, *turn) error {
		if attempts++; attempts == 2 {
			close(entered)
			<-release
		}
		return errRefused
	}
	send(t, s, nil, "1", "hi")
	testutil.Wait(t, entered, "the retry's attempt")

	stopped := make(chan error, 1)
	go func() { stopped <- s.stop(context.Background()) }()
	// A negative assertion: nothing marks a stop that must not return, so the
	// window is a bound, several backoffs long.
	select {
	case <-stopped:
		t.Fatal("stop returned with a settle attempt in flight")
	case <-time.After(20 * s.writeBackoff):
	}
	close(release)
	require.NoError(t, testutil.Recv(t, stopped, "the stop"))
}

// storedContent is the message's content column as it stands.
func storedContent(t *testing.T, s *service, id MessageID) rawjson.RawJSON {
	t.Helper()
	var content string
	require.NoError(t, s.db.Read.QueryRow(`SELECT content FROM messages WHERE id = ?`, string(id)).Scan(&content))
	return rawjson.RawJSON(content)
}

// A read mid-answer is served the live text from memory; the row is written only
// at a checkpoint, and none is due.
func TestAReadMidAnswerSeesTheLiveText(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	s.checkpointEvery = time.Hour
	fakeOf(s).SetGate(make(chan struct{}))
	msg := send(t, s, nil, "1", "hi")

	live := awaitLiveContent(t, s, msg.ChatID)

	assert.Equal(t, marshalBlocks([]llm.Block{llm.ThinkingBlock(fakeFirstWord)}), live.Content)
	assert.Equal(t, emptyContent, storedContent(t, s, msg.ID))
}

// The first checkpoint is counted from the answer's creation: a chunk that
// arrives checkpointEvery after it writes the row, and the next, at once after,
// does not.
func TestTheFirstCheckpointCountsFromTheAnswer(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	var clock atomic.Int64
	clock.Store(1_000_000)
	s.now = func() time.Time { return time.UnixMilli(clock.Load()).UTC() }
	s.checkpointEvery = time.Hour
	answers := settleSeam(s)
	gate := make(chan struct{})
	fakeOf(s).SetGate(gate)
	msg := send(t, s, nil, "1", "hi")
	awaitLiveContent(t, s, msg.ChatID)
	assert.Equal(t, emptyContent, storedContent(t, s, msg.ID), "an hour has not passed")

	clock.Add(time.Hour.Milliseconds())
	close(gate)
	answers <- errRefused

	assert.Equal(t, marshalBlocks([]llm.Block{llm.ThinkingBlock("Let me ")}), storedContent(t, s, msg.ID))
}

// A service that stops mid-answer with its settle failing leaves the row its
// last checkpoint wrote, so the stranded answer keeps the text the reader saw.
func TestAStrandedAnswerKeepsItsLastCheckpoint(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s, err := newService(openTestDB(t, dir), chatsDirIn(dir), monitorDirIn(dir), fakeLLM(), noClusterCards, nil, testReaders, noLists, sandbox.State{}, testSettings(t), nil)
	require.NoError(t, err)
	s.checkpointEvery = 0
	stop, err := s.Start(t.Context())
	require.NoError(t, err)
	settleSeam(s)
	fakeOf(s).SetGate(make(chan struct{}))
	msg := send(t, s, nil, "1", "hi")
	awaitLiveContent(t, s, msg.ChatID)

	require.NoError(t, stop(t.Context()))
	require.NoError(t, s.Close())
	next := startService(t, dir)

	msgs, err := next.readMessages(t.Context(), msg.ChatID)
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, msgs[1].Status)
	assert.Equal(t, marshalBlocks([]llm.Block{llm.ThinkingBlock(fakeFirstWord)}), msgs[1].Content)
}

// A checkpoint the store refuses is logged and skipped: the live message is the
// truth until the settle, and the turn goes on.
func TestACheckpointThatFailsIsSkipped(t *testing.T) {
	// CaptureLogs swaps the process's default logger, so this test stays serial.

	logs := testutil.CaptureLogs(t)
	s := newTestService(t)
	s.checkpointEvery = 0
	c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))
	seeded := seedTurn(t, s.db, c.ID, time.UnixMilli(1_000).UTC())
	msgs, err := listMessages(t.Context(), s.store.Stmts(), c.ID, testReaders)
	require.NoError(t, err)
	tr, err := s.reserveTurn(c.ID, seeded.Run, fakeTarget(s))
	require.NoError(t, err)
	defer s.abandonTurn(tr)
	tr.msg = msgs[1]
	_, err = s.db.Write.Exec(`CREATE TRIGGER refuse BEFORE UPDATE OF content ON messages BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)

	tr.Progress([]llm.Block{llm.TextBlock("so far")})

	assert.Contains(t, logs.String(), "could not checkpoint")
	assert.Equal(t, marshalBlocks([]llm.Block{llm.TextBlock("so far")}), tr.msg.Content)
	assert.Equal(t, emptyContent, storedContent(t, s, seeded.Assistant))
}

// The turn goes into the slot before its goroutine starts, so a Cancel can reach
// one that has not run; abandoning it frees the slot.
func TestAReservedTurnIsSafeToCancelBeforeItRuns(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	id := ChatID(appdb.NewID())
	tr, err := s.reserveTurn(id, newRunID(), fakeTarget(s))
	require.NoError(t, err)

	require.NoError(t, s.Cancel(t.Context(), id))

	assert.ErrorIs(t, tr.ctx.Err(), context.Canceled)
	s.abandonTurn(tr)
	assert.Nil(t, s.turnOf(id))
}

// A turn nobody has written rows for is not an overlay: a read is the stored rows.
func TestAReservedTurnIsNotAnOverlay(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	msg := send(t, s, nil, "1", "what is a pod?")
	settled := awaitSettled(t, s, msg.ChatID, msg.ID)
	tr, err := s.reserveTurn(msg.ChatID, newRunID(), fakeTarget(s))
	require.NoError(t, err)
	t.Cleanup(func() { s.abandonTurn(tr) })

	msgs, err := s.readMessages(t.Context(), msg.ChatID)

	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, settled.Status, msgs[1].Status)
	assert.Equal(t, settled.Content, msgs[1].Content)
}

// A send that fails after reserving still ends the turn a delete found in the
// slot, so the delete joining on it goes on.
func TestASendThatFailsAfterReservingUnblocksADelete(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	id := ChatID(appdb.NewID())
	tr, err := s.reserveTurn(id, newRunID(), fakeTarget(s))
	require.NoError(t, err)

	found := s.guardDelete(id)
	defer s.releaseDelete(id)
	require.Same(t, tr, found)

	s.abandonTurn(tr)
	testutil.Wait(t, tr.done, "the abandoned turn's join")
}

// A turn's context is a child of the service's and is cancelled when the turn
// ends: a sidecar up for weeks would otherwise hold every turn it ever ran.
func TestATurnEndsItsOwnContext(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	gate := make(chan struct{})
	fakeOf(s).SetGate(gate)
	msg := send(t, s, nil, "1", "what is a pod?")
	tr := s.turnOf(msg.ChatID)
	require.NotNil(t, tr)

	close(gate)
	testutil.Wait(t, tr.done, "the turn's join")

	assert.ErrorIs(t, tr.ctx.Err(), context.Canceled)
}

// The run is the turn's record: running with its start once the turn claims it,
// and succeeded with the answer's own end once it settles.
func TestATurnClaimsItsRunAndSettlesIt(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	gate := make(chan struct{})
	fakeOf(s).SetGate(gate)
	msg := send(t, s, nil, "1", "what is a pod?")
	awaitLiveContent(t, s, msg.ChatID)

	var started, finished sql.NullInt64
	assert.Equal(t, RunRunning, runStatusOf(t, s.db, msg.RunID))
	require.NoError(t, s.db.Read.QueryRow(`SELECT started_at FROM agent_runs WHERE id = ?`, string(msg.RunID)).Scan(&started))
	assert.True(t, started.Valid)
	close(gate)
	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, StatusComplete, got.Status)
	assert.Equal(t, RunSucceeded, runStatusOf(t, s.db, msg.RunID))
	require.NoError(t, s.db.Read.QueryRow(`SELECT finished_at FROM agent_runs WHERE id = ?`, string(msg.RunID)).Scan(&finished))
	assert.Equal(t, got.FinishedAt.Time.UnixMilli(), finished.Int64)
}

// Each answer's citations are read by its own run's dialect: a chat that crossed
// dialects holds rows in more than one provider's shapes, and a read by any one
// dialect would lose the other's cited text.
func TestAMessageReadsCitationsByItsRunsDialect(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, s.db, aChat("1", now))
	onMessages, onFake := seedTurn(t, s.db, c.ID, now), seedTurn(t, s.db, c.ID, now)
	answer := func(turn seededTurn, provider string, d llm.Dialect, payload string) {
		t.Helper()
		_, err := s.db.Write.Exec(`UPDATE agent_runs SET provider = ?, dialect = ? WHERE id = ?`, provider, string(d), string(turn.Run))
		require.NoError(t, err)
		text := llm.TextBlock("It shipped.")
		text.Payload = json.RawMessage(payload)
		_, err = s.db.Write.Exec(`UPDATE messages SET content = ? WHERE id = ?`, string(marshalBlocks([]llm.Block{text})), string(turn.Assistant))
		require.NoError(t, err)
		settleSeededRun(t, s.db, turn.Run, RunSucceeded, now)
	}
	answer(onMessages, "messages", llm.DialectMessages,
		`{"type":"text","text":"It shipped.","citations":[{"type":"web_search_result_location","url":"https://kubernetes.io/releases","title":"Releases","cited_text":"1.36","encrypted_index":"aQ=="}]}`)
	answer(onFake, "fake", llm.DialectFake,
		`{"citations":[{"type":"web_search_result_location","url":"https://kubernetes.io/blog","title":"Blog","citedText":"1.37"}]}`)

	msgs, err := s.readMessages(t.Context(), c.ID)

	require.NoError(t, err)
	require.Len(t, msgs, 4)
	assert.Equal(t, []llm.Citation{{Type: "web_search_result_location", URL: "https://kubernetes.io/releases", Title: "Releases", CitedText: "1.36"}},
		citationsOf(t, msgs[1]))
	assert.Equal(t, []llm.Citation{{Type: "web_search_result_location", URL: "https://kubernetes.io/blog", Title: "Blog", CitedText: "1.37"}},
		citationsOf(t, msgs[3]))
}

// runtimeTool answers with its session's kind, its runtime's cluster and chat,
// and the session's switch, after running before when it is set.
type runtimeTool struct {
	testTool
	before func(ctx context.Context, rt tools.Runtime)
}

func (r runtimeTool) Run(ctx context.Context, rt tools.Runtime, _ json.RawMessage) (string, bool) {
	if r.before != nil {
		r.before(ctx, rt)
	}
	return fmt.Sprintf("%s %s/%s/%t", rt.Session.Kind, rt.ClusterID, rt.ChatID, rt.Session.Outside), false
}

// A turn's tools reach its chat's stored cluster, never the send's, and its chat.
func TestATurnsRuntimeIsItsChatsCluster(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, runtimeTool{testTool: testTool{name: "where"}})
	fakeOf(s).SetToolCalls(llm.StagedCall("where", `{}`))

	msg := sendAndSettle(t, s, nil, "2", "first", "hi")
	fakeOf(s).SetToolCalls(llm.StagedCall("where", `{}`))
	again := sendAndSettle(t, s, &msg.ChatID, "7", "again", "hi")

	for _, run := range []RunID{msg.RunID, again.RunID} {
		rows := toolCallRows(t, s.db, run)
		require.Len(t, rows, 1)
		assert.Equal(t, "chat 2/"+string(msg.ChatID)+"/false", rows[0].result)
	}
}

// A turn reads the chat's switch once, as the send reserves it: a switch flipped
// while it runs changes the next turn, never this one.
func TestATurnsRuntimeCarriesItsChatsSwitch(t *testing.T) {
	t.Parallel()

	var s *service
	flip := func(ctx context.Context, rt tools.Runtime) {
		_, err := s.SetSandboxDisabled(ctx, rt.ChatID, false)
		assert.NoError(t, err)
	}
	s = startServiceWithTool(t, runtimeTool{testTool: testTool{name: "where"}, before: flip})
	s.sandboxState = sandbox.State{Available: true}
	msg := sendAndSettle(t, s, nil, "1", "first", "hi")
	_, err := s.SetSandboxDisabled(t.Context(), msg.ChatID, true)
	require.NoError(t, err)

	fakeOf(s).SetToolCalls(llm.StagedCall("where", `{}`))
	switched := sendAndSettle(t, s, &msg.ChatID, "1", "switched", "hi")
	fakeOf(s).SetToolCalls(llm.StagedCall("where", `{}`))
	after := sendAndSettle(t, s, &msg.ChatID, "1", "after", "hi")

	for run, want := range map[RunID]string{switched.RunID: "true", after.RunID: "false"} {
		rows := toolCallRows(t, s.db, run)
		require.Len(t, rows, 1)
		assert.Equal(t, "chat 1/"+string(msg.ChatID)+"/"+want, rows[0].result)
	}
}

// A tool the user turned off leaves the next turn's offer and its section of
// the system prompt; turned on again, it is back on the turn after.
func TestATurnIsNotOfferedAToolTheUserTurnedOff(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: "echo"}, testTool{name: "other"})
	setToolEnabled(t, s, "echo", false)

	msg := send(t, s, nil, "1", "hi")
	awaitSettled(t, s, msg.ChatID, msg.ID)
	req := fakeOf(s).LastRequest()
	assert.Equal(t, []string{"other"}, offered(req))
	assert.NotContains(t, req.SystemPrompt, "## echo")
	assert.Contains(t, req.SystemPrompt, "## other")

	setToolEnabled(t, s, "echo", true)
	next := send(t, s, &msg.ChatID, "2", "again")
	awaitSettled(t, s, msg.ChatID, next.ID)
	assert.Equal(t, []string{"echo", "other"}, offered(fakeOf(s).LastRequest()))
}

// A tool whose stored setting Kubetail cannot use leaves the offer.
func TestATurnIsNotOfferedAToolWhoseSettingCannotBeUsed(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: "echo"}, testTool{name: "other"})
	_, err := s.db.Write.Exec(`INSERT INTO settings (key, value, created_at, updated_at) VALUES ('tools.echo.enabled', 'yes', 0, 0)`)
	require.NoError(t, err)

	msg := send(t, s, nil, "1", "hi")
	awaitSettled(t, s, msg.ChatID, msg.ID)
	assert.Equal(t, []string{"other"}, offered(fakeOf(s).LastRequest()))
}

// An always-on tool stays in the offer whatever its stored setting says.
func TestATurnIsAlwaysOfferedTaskStop(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: taskstop.Name}, testTool{name: "echo"})
	setToolEnabled(t, s, taskstop.Name, false)
	setToolEnabled(t, s, "echo", false)

	msg := send(t, s, nil, "1", "hi")
	awaitSettled(t, s, msg.ChatID, msg.ID)
	assert.Equal(t, []string{taskstop.Name}, offered(fakeOf(s).LastRequest()))
}

// The tool settings are read once per turn: a tool turned off while a turn runs
// stays in that turn's offer.
func TestARunningTurnKeepsItsTools(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: "echo"})
	gate := make(chan struct{})
	fakeOf(s).SetGate(gate)
	fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{}`))

	msg := send(t, s, nil, "1", "hi")
	awaitLiveContent(t, s, msg.ChatID)
	setToolEnabled(t, s, "echo", false)
	close(gate)
	awaitSettled(t, s, msg.ChatID, msg.ID)

	reqs := fakeOf(s).Requests()
	require.Len(t, reqs, 2)
	for _, req := range reqs {
		assert.Equal(t, []string{"echo"}, offered(req))
	}
	rows := toolCallRows(t, s.db, msg.RunID)
	require.Len(t, rows, 1)
	assert.Equal(t, toolSucceeded, rows[0].status)
}

// Turning a tool off takes it from the offer, never from the box: a call of it
// is refused as a name the turn lacks, and its row still records the tool's
// contract and draws its action.
func TestACallOfAToolTurnedOffIsRefusedAndStillDraws(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: "echo"})
	setToolEnabled(t, s, "echo", false)
	fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{"x":1}`))

	msg := send(t, s, nil, "1", "hi")
	settled := awaitSettled(t, s, msg.ChatID, msg.ID)

	rows := toolCallRows(t, s.db, msg.RunID)
	require.Len(t, rows, 1)
	assert.Equal(t, `{"error":"unknown-tool"}`, rows[0].result)
	assert.Equal(t, "echo/v1", rows[0].contract)
	calls, err := settled.ToolCallList()
	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Equal(t, &tools.Action{Command: &tools.CommandAction{Text: `{"x":1}`}}, calls[0].Action)
}

// A send whose read of the tool settings fails writes nothing.
func TestASendThatCannotReadTheToolSettingsIsRefused(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: "echo"})
	_, err := s.db.Write.Exec(`DROP TABLE settings`)
	require.NoError(t, err)

	_, err = s.Send(t.Context(), nil, ModeChat, "1", false, false, false, "fake", "fake", "high", reqID("1"), "hi", nil)
	assert.ErrorContains(t, err, "disabled tools")
	assert.Empty(t, fakeOf(s).Requests(), "no model asked")
}
