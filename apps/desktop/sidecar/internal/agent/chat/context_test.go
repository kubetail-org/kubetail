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
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/catalog"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/clustercard"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/memory"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/providercredentials"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/agent"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/anthropicwebsearch"
)

// The boxes the ceiling is computed over: none, a tool the sidecar runs, the
// provider's search alone, and both.
var (
	emptyBox      = tools.Box{}
	runnerBox     = tools.NewBox([]tools.Tool{testTool{name: "echo"}})
	searchBox     = tools.NewBox([]tools.Tool{testSearch})
	runnerAndBoth = tools.NewBox([]tools.Tool{testTool{name: "echo"}, testSearch})
)

// The ceiling is the window less the cap every request asks for, less what the
// turn adds, each term named so a change to a budget moves it.
func TestContextCeilingLeavesRoomForTheAnswerAndTheTurn(t *testing.T) {
	t.Parallel()

	assert.Equal(t, (clustercard.Budget+2*memory.ScopeBudget+workspaceShare+viewBudget)/bytesPerToken, contextTokens)
	base := contextTokens + questionTokens
	results := MaxToolCalls * typicalResultBytes / bytesPerToken

	assert.Equal(t, base, turnInputAllowance(emptyBox, MaxToolCalls))
	assert.Equal(t, base+results, turnInputAllowance(runnerBox, MaxToolCalls), "a tool the sidecar runs adds the call budget's results")
	assert.Equal(t, 30_880, turnInputAllowance(runnerBox, MaxToolCalls))
	assert.Equal(t, 40_880, turnInputAllowance(runnerAndBoth, MaxToolCalls))

	haiku := llm.Model{ID: "haiku", MaxOutputTokens: 64_000, ContextWindow: 200_000}
	assert.Equal(t, int64(95_120), contextCeiling(haiku, turnInputAllowance(runnerAndBoth, MaxToolCalls)))
	opus := llm.Model{ID: "opus", MaxOutputTokens: 64_000, ContextWindow: 1_000_000}
	assert.Equal(t, int64(895_120), contextCeiling(opus, turnInputAllowance(runnerAndBoth, MaxToolCalls)))

	assert.Zero(t, contextCeiling(llm.Model{ID: "unstated", MaxOutputTokens: 64_000}, base), "a window of 0 has no ceiling")
	uncapped := llm.Model{ID: "grok", ContextWindow: 500_000}
	assert.Equal(t, int64(500_000-base), contextCeiling(uncapped, base), "no cap is sent, so none is reserved")
}

// Each provider tool the box holds adds its own allowance; a box without one adds
// none for it.
func TestTheCeilingFollowsTheOffers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, turnInputAllowance(emptyBox, MaxToolCalls)+testSearch.Allowance(), turnInputAllowance(searchBox, MaxToolCalls))
	assert.Equal(t, turnInputAllowance(runnerBox, MaxToolCalls)+testSearch.Allowance(), turnInputAllowance(runnerAndBoth, MaxToolCalls))
}

// Every stated window leaves room at the largest allowance a turn can take, so no
// window in the catalog goes unchecked.
func TestEveryStatedWindowLeavesACeiling(t *testing.T) {
	t.Parallel()

	keys := map[string]string{}
	for id := range catalog.KeyVars() {
		keys[id] = "key-0123456789abcdef"
	}
	credentials, err := providercredentials.New(providercredentials.Config{Keyring: oskeyringtest.New(), Providers: catalog.Providers(nil, ""), EnvKeys: keys})
	require.NoError(t, err)
	t.Cleanup(credentials.Close)
	largest := turnInputAllowance(runnerAndBoth, MaxToolCalls)
	for _, p := range catalog.New(catalog.Config{Credentials: credentials, Fake: llm.NewFake(0)}).Providers() {
		for _, m := range p.Catalog {
			if m.ContextWindow > 0 {
				assert.Positive(t, contextCeiling(m, largest), "%s/%s", p.ID, m.ID)
			}
		}
	}
}

// windowed starts a service over one fake behind three providers: "fake", whose
// model "fake" has the window and cap a test names and whose "fake-wide" has
// wide and no cap; "other", the same catalog on the same dialect, whose count is
// its own; and "messages", on another dialect, served by a local server that
// refuses every call, so a switch to it leaves nothing on the network. It offers
// the test box: the tools given, then the search.
func windowed(t *testing.T, window, maxOutput, wide int, offered ...tools.Tool) (*service, *llm.Fake) {
	t.Helper()
	f := llm.NewFake(0)
	fake := llm.FakeProvider(f)
	fake.Catalog = []llm.Model{
		{ID: "fake", Label: "Fake", ContextWindow: window, MaxOutputTokens: maxOutput, Tools: true},
		{ID: "fake-wide", Label: "Fake wide", ContextWindow: wide, Tools: true},
	}
	other := fake
	other.ID, other.Label = "other", "Other"
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(refusing.Close)
	messages := llm.Provider{ID: "messages", Label: "Messages", Dialect: llm.DialectMessages, BaseURL: refusing.URL,
		Catalog: []llm.Model{{ID: "m", Label: "M", MaxOutputTokens: 1_000, Tools: true}}}
	box, lists := testBox(offered...)
	return startServiceWith(t, t.TempDir(), llm.New(fake, other, messages), &stubClusterCards{}, box, lists), f
}

// ask sends question into chatID (nil starts a chat) on providerID's model.
func ask(t *testing.T, s *service, chatID *ChatID, providerID, modelID, question string) (ChatMessage, error) {
	t.Helper()
	return s.Send(t.Context(), chatID, ModeChat, "1", SendSwitches{}, providerID, modelID, "", uuid.NewString(), question, nil)
}

// converse asks and waits for the answer to settle.
func converse(t *testing.T, s *service, chatID *ChatID, providerID, modelID, question string) ChatMessage {
	t.Helper()
	sent, err := ask(t, s, chatID, providerID, modelID, question)
	require.NoError(t, err)
	return awaitSettled(t, s, sent.ChatID, sent.ID)
}

// reads is a report of a call that read n tokens.
func reads(n int) llm.Usage { return llm.Usage{Reported: true, InputTokens: n} }

// A chat whose last turn read more than the model's ceiling refuses the next send
// before any row is written.
func TestASendIntoAFullChatIsRefusedAndWritesNothing(t *testing.T) {
	t.Parallel()

	s, f := windowed(t, 200_000, 64_000, 0)
	f.SetUsage(reads(150_000))

	first := converse(t, s, nil, "fake", "fake", "what is a pod?")
	require.Equal(t, StatusComplete, first.Status)

	_, err := ask(t, s, &first.ChatID, "fake", "fake", "and a node?")
	assert.ErrorIs(t, err, ErrChatContextFull)
	assert.Equal(t, 2, tableCount(t, s.db, "messages"))
	assert.Equal(t, 1, tableCount(t, s.db, "agent_runs"))
}

// The ceiling is the named model's: a chat full on one model is open on a model
// with a larger window.
func TestAChatFullOnOneModelIsOpenOnAnother(t *testing.T) {
	t.Parallel()

	s, f := windowed(t, 200_000, 64_000, 1_000_000)
	f.SetUsage(reads(150_000))

	first := converse(t, s, nil, "fake", "fake", "what is a pod?")
	_, err := ask(t, s, &first.ChatID, "fake", "fake", "and a node?")
	require.ErrorIs(t, err, ErrChatContextFull)

	second := converse(t, s, &first.ChatID, "fake", "fake-wide", "and a node?")
	assert.Equal(t, StatusComplete, second.Status)
}

// What another provider counted does not carry: each replays the transcript in
// its own shape, so a chat full on one provider is open on another, whose own
// refusal is the judge.
func TestAnotherProvidersCountDoesNotCarry(t *testing.T) {
	t.Parallel()

	s, f := windowed(t, 200_000, 64_000, 0)
	f.SetUsage(reads(150_000), reads(150_000))

	first := converse(t, s, nil, "fake", "fake", "what is a pod?")
	_, err := ask(t, s, &first.ChatID, "fake", "fake", "and a node?")
	require.ErrorIs(t, err, ErrChatContextFull)

	second := converse(t, s, &first.ChatID, "other", "fake", "and a node?")
	require.Equal(t, StatusComplete, second.Status)

	// Back on the first provider its own count still stands: the older turn's
	// request is a prefix of this one's.
	_, err = ask(t, s, &first.ChatID, "fake", "fake", "and a service?")
	assert.ErrorIs(t, err, ErrChatContextFull)
}

// A model that states no window is never refused for length; its provider is
// the judge.
func TestAModelWithNoWindowIsNeverRefusedForLength(t *testing.T) {
	t.Parallel()

	s, f := windowed(t, 0, 64_000, 0)
	f.SetUsage(reads(5_000_000))

	first := converse(t, s, nil, "fake", "fake", "what is a pod?")
	second := converse(t, s, &first.ChatID, "fake", "fake", "and a node?")
	assert.Equal(t, StatusComplete, second.Status)
}

// The count is the newest succeeded turn's: a failed turn's rounds are not
// replayed, so what its calls read does not count.
func TestAFailedTurnsCallsDoNotCount(t *testing.T) {
	t.Parallel()

	t.Run("a failed turn is skipped", func(t *testing.T) {
		var f *llm.Fake
		// Its first round reads past the ceiling, and its second fails.
		failNext := testTool{name: "echo", run: func(context.Context, json.RawMessage) (string, bool) {
			f.FailAfter(0, llm.TransportError("fake", errors.New("gone")))
			return "ok", false
		}}
		var s *service
		s, f = windowed(t, 200_000, 64_000, 0, failNext)
		f.SetUsage(reads(10), reads(150_000))

		first := converse(t, s, nil, "fake", "fake", "one")
		f.SetToolCalls(llm.StagedCall("echo", `{}`))
		second := converse(t, s, &first.ChatID, "fake", "fake", "two")
		require.Equal(t, StatusFailed, second.Status)
		third := converse(t, s, &first.ChatID, "fake", "fake", "three")
		assert.Equal(t, StatusComplete, third.Status)
	})
	t.Run("a succeeded turn counts", func(t *testing.T) {
		s, f := windowed(t, 200_000, 64_000, 0)
		f.SetUsage(reads(10), reads(150_000))

		first := converse(t, s, nil, "fake", "fake", "one")
		converse(t, s, &first.ChatID, "fake", "fake", "two")
		_, err := ask(t, s, &first.ChatID, "fake", "fake", "three")
		assert.ErrorIs(t, err, ErrChatContextFull)
	})
}

// overflow is the provider refusing to read the request.
func overflow() error {
	return llm.ResponseError(llm.Provider{ID: "fake"}, 400, "invalid_request_error", llm.CodeContextLengthExceeded)
}

// After the provider refused to read the chat on a turn's first request, a send
// naming the same model is refused ahead, since the count alone would admit it,
// and another model is not. Once a turn on another model has succeeded, the
// first is judged by its count again.
func TestARepeatOnTheModelThatOverflowedIsRefused(t *testing.T) {
	t.Parallel()

	s, f := windowed(t, 1_000_000, 64_000, 1_000_000)
	f.FailAfter(0, overflow())

	msg := converse(t, s, nil, "fake", "fake", "what is a pod?")
	require.Equal(t, StatusFailed, msg.Status)

	_, err := ask(t, s, &msg.ChatID, "fake", "fake", "what is a pod?")
	assert.ErrorIs(t, err, ErrChatContextFull)

	second := converse(t, s, &msg.ChatID, "fake", "fake-wide", "what is a pod?")
	require.Equal(t, StatusComplete, second.Status)
	third := converse(t, s, &msg.ChatID, "fake", "fake", "and a node?")
	assert.Equal(t, StatusComplete, third.Status)
}

// A turn pushed over by its own tool results overflowed on a later request. The
// next request drops those rounds, so the chat still fits and the next send on
// that model is admitted.
func TestAnOverflowAfterAToolRoundLeavesTheModelOpen(t *testing.T) {
	t.Parallel()

	var f *llm.Fake
	overflowNext := testTool{name: "echo", run: func(context.Context, json.RawMessage) (string, bool) {
		f.FailAfter(0, overflow())
		return "a very long result", false
	}}
	var s *service
	s, f = windowed(t, 1_000_000, 64_000, 0, overflowNext)
	f.SetToolCalls(llm.StagedCall("echo", `{}`))

	msg := converse(t, s, nil, "fake", "fake", "dump every pod")
	require.Equal(t, StatusFailed, msg.Status)
	require.Equal(t, contextFullText, msg.Error)

	next := converse(t, s, &msg.ChatID, "fake", "fake", "just the names")
	assert.Equal(t, StatusComplete, next.Status)
}

// A call that ran a server tool is not counted: inside one request the provider
// samples again after each server call, and its usage may count the chat once per
// sampling. An older call answers instead. A report of no searches still counts.
func TestACallThatRanServerToolsDoesNotCount(t *testing.T) {
	t.Parallel()

	t.Run("a call that searched is skipped", func(t *testing.T) {
		s, f := windowed(t, 200_000, 64_000, 0)
		f.SetUsage(reads(10), reads(150_000))
		f.SetServerUses(nil, map[string]int{anthropicwebsearch.Name: 2})

		first := converse(t, s, nil, "fake", "fake", "one")
		converse(t, s, &first.ChatID, "fake", "fake", "two")
		third := converse(t, s, &first.ChatID, "fake", "fake", "three")
		assert.Equal(t, StatusComplete, third.Status)
	})
	t.Run("a call that searched none counts", func(t *testing.T) {
		s, f := windowed(t, 200_000, 64_000, 0)
		f.SetUsage(reads(150_000))
		f.SetServerUses(map[string]int{anthropicwebsearch.Name: 0})

		first := converse(t, s, nil, "fake", "fake", "one")
		_, err := ask(t, s, &first.ChatID, "fake", "fake", "two")
		assert.ErrorIs(t, err, ErrChatContextFull)
	})
}

// A chat with no turn yet has no newest run and no use to count, and is admitted.
func TestAChatWithNoTurnHasNothingToCount(t *testing.T) {
	t.Parallel()

	s, _ := windowed(t, 200_000, 64_000, 0)
	c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))
	st := s.store.Stmts()
	target, err := s.llmSvc.Resolve("fake", "fake", "")
	require.NoError(t, err)

	_, ok, err := newestRun(t.Context(), st, c.ID)
	require.NoError(t, err)
	assert.False(t, ok)
	used, err := lastContextUse(t.Context(), st, c.ID, "fake")
	require.NoError(t, err)
	assert.Zero(t, used)
	assert.NoError(t, roomFor(t.Context(), st, c.ID, target, s.boxFor(target, nil)))
}

// A store that cannot answer either read fails the send rather than admitting it,
// naming the read.
func TestRoomForReportsAStoreItCannotRead(t *testing.T) {
	t.Parallel()

	s, _ := windowed(t, 200_000, 64_000, 0)
	target, err := s.llmSvc.Resolve("fake", "fake", "")
	require.NoError(t, err)
	closed := newTestSet(t)
	require.NoError(t, closed.Close())

	assert.ErrorContains(t, roomFor(t.Context(), closed.Stmts(), "c", target, s.boxFor(target, nil)), "newest run")

	// The first read answers, the second cannot: it is swapped for a read of the
	// wrong shape, so only the count fails.
	table := swapped(map[stmtID]string{stmtSelectLastContextUse: `SELECT 'text', 0, 0, 0`})
	open := prepareTable(t, openTestDB(t, t.TempDir()), table).Stmts()

	assert.ErrorContains(t, roomFor(t.Context(), open, "c", target, s.boxFor(target, nil)), "last context use")
}

// A count is its provider's: a switch to a provider of another dialect with no
// count on the chat is not checked ahead, whatever the fake counted.
func TestASwitchIsCheckedAgainstTheTargetsOwnCount(t *testing.T) {
	t.Parallel()

	s, f := windowed(t, 200_000, 64_000, 0)
	f.SetUsage(reads(150_000))

	first := converse(t, s, nil, "fake", "fake", "what is a pod?")
	sent, err := ask(t, s, &first.ChatID, "messages", "m", "and a node?")
	require.NoError(t, err)
	assert.Equal(t, "messages", awaitSettled(t, s, sent.ChatID, sent.ID).ProviderID)
}

// A subagent's run has no message, so what it read does not count against its
// parent's chat.
func TestASubagentsCallsDoNotCount(t *testing.T) {
	t.Parallel()

	s, f := windowed(t, 200_000, 64_000, 0, agent.New())
	f.Route("Look.").SetUsage(reads(150_000))
	// The parent's calls, and the notice turn's after the agent ends, report
	// nothing: the subagent's is the one report there is to count.
	f.SetUsage(llm.Usage{}, llm.Usage{}, llm.Usage{})
	f.SetToolCalls(agentCall("Look."))

	first := converse(t, s, nil, "fake", "fake", "what is a pod?")
	awaitTasks(t, s, first.ChatID)
	runs := subagentRuns(t, s.db, first.RunID)
	require.Len(t, runs, 1)
	require.Equal(t, [4]sql.NullInt64{count(150_000), count(0), count(0), count(0)}, usageOf(t, s, runs[0].id))

	target, err := s.llmSvc.Resolve("fake", "fake", "")
	require.NoError(t, err)
	assert.NoError(t, roomFor(t.Context(), s.store.Stmts(), first.ChatID, target, s.boxFor(target, nil)))
}
