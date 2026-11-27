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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/rawjson"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/anthropicwebsearch"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/bash"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/logsview"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/logswatch"
)

// Every row helper names the statement that failed. A closed pool is the one
// failure every helper shares.
func TestEveryRowHelperNamesItsFailure(t *testing.T) {
	t.Parallel()

	set := newTestSet(t)
	require.NoError(t, set.Close())
	st := set.Stmts()

	ctx := t.Context()
	now := time.UnixMilli(1_000).UTC()
	id := ChatID(appdb.NewID())

	calls := map[string]func() error{
		"rename chat":               func() error { _, _, err := renameChat(ctx, st, id, "t", now); return err },
		"get chat":                  func() error { _, _, err := getChat(ctx, st, id); return err },
		"list chats":                func() error { _, err := listChats(ctx, st); return err },
		"chat ids by cluster":       func() error { _, err := chatIDsByCluster(ctx, st, "1"); return err },
		"delete chat":               func() error { _, err := deleteChat(ctx, st, id); return err },
		"fail stranded runs":        func() error { _, err := failStrandedRuns(ctx, st, "reason", now); return err },
		"list messages":             func() error { _, err := listMessages(ctx, st, id, testReaders); return err },
		"marked clusters":           func() error { _, err := markedClusterIDs(ctx, st); return err },
		"insert chat":               func() error { return insertChat(ctx, st, aChat("1", now)) },
		"touch chat":                func() error { return touchChat(ctx, st, id, now) },
		"next seq":                  func() error { _, err := nextSeq(ctx, st, id); return err },
		"insert message":            func() error { return insertMessage(ctx, st, ChatMessage{}, "") },
		"insert run":                func() error { return insertRun(ctx, st, agentRun{}) },
		"insert monitor run":        func() error { return insertMonitorRun(ctx, st, agentRun{}) },
		"cluster monitoring":        func() error { _, _, err := clusterMonitoring(ctx, st, "1"); return err },
		"live clusters":             func() error { _, err := liveClusterIDs(ctx, st); return err },
		"claim run":                 func() error { return claimRun(ctx, st, "r", now) },
		"write content":             func() error { return writeContent(ctx, st, "m", emptyContent) },
		"settle run":                func() error { return settleRun(ctx, st, "r", RunFailed, "", "", now) },
		"answer by request key":     func() error { _, _, err := answerByRequestKey(ctx, st, "k", testReaders); return err },
		"insert llm call":           func() error { return insertLLMCall(ctx, st, llmCallEntry{}) },
		"close llm call":            func() error { return closeLLMCall(ctx, st, llmCallEntry{ID: "c", FinishedAt: now}) },
		"close stranded llm calls":  func() error { _, err := closeStrandedLLMCalls(ctx, st, now); return err },
		"upsert tool call":          func() error { return upsertToolCall(ctx, st, toolCallEntry{}) },
		"close stranded tool calls": func() error { return closeStrandedToolCalls(ctx, st, now) },
		"upsert approval":           func() error { return upsertApproval(ctx, st, approval{}) },
		"flip run":                  func() error { return flipRun(ctx, st, "r", RunRunning) },
		"cluster accepts":           func() error { _, err := clusterAccepts(ctx, st, "1"); return err },
		"insert task":               func() error { return insertTask(ctx, st, "t", id, "c", "/p", now) },
		"delete task":               func() error { return deleteTask(ctx, st, "t") },
		"finish task":               func() error { return finishTask(ctx, st, "t", taskEnd{Status: taskExited, At: now}) },
		"mark lost tasks":           func() error { _, err := markLostTasks(ctx, st, now); return err },
		"waiting notices":           func() error { _, err := waitingNotices(ctx, st, id, testReaders); return err },
		"mark notified":             func() error { return markNotified(ctx, st, id, now) },
		"last answer run":           func() error { _, _, _, err := lastAnswerRun(ctx, st, id); return err },
		"newest run":                func() error { _, _, err := newestRun(ctx, st, id); return err },
		"last context use":          func() error { _, err := lastContextUse(ctx, st, id, "fake"); return err },
	}
	for want, call := range calls {
		assert.ErrorContains(t, call(), want)
	}
}

// Rows of the wrong shape are reported, never taken as an empty collection. The
// table is swapped under the helpers: each multi-row read is answered with two
// columns, which no scanner here takes — the one-column reads included.
func TestAReadWithTheWrongShapeIsReported(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	swaps := map[stmtID]string{}
	for _, id := range multiRowReads {
		swaps[id] = `SELECT 1, 2`
	}
	st := prepareTable(t, openTestDB(t, t.TempDir()), swapped(swaps)).Stmts()

	_, err := listChats(ctx, st)
	assert.ErrorContains(t, err, "list chats")
	_, err = listMessages(ctx, st, ChatID(appdb.NewID()), testReaders)
	assert.ErrorContains(t, err, "list messages")
	_, err = chatIDsByCluster(ctx, st, "1")
	assert.ErrorContains(t, err, "chat ids by cluster")
	_, err = failStrandedRuns(ctx, st, "reason", time.UnixMilli(1_000).UTC())
	assert.ErrorContains(t, err, "fail stranded runs")
	assertCallReadsFail(t, st, ChatID(appdb.NewID()))
}

// assertCallReadsFail checks that each read of a chat's calls and notices reports
// the failure its swapped statement provokes. The calls are read beside the run's
// own writes, which no test swaps, so the failure is the calls' own.
func assertCallReadsFail(t *testing.T, st stmts, id ChatID) {
	t.Helper()
	ctx := t.Context()
	_, err := toolCallsByRun(ctx, st, callReads{calls: stmtSelectToolCalls, actions: stmtSelectRunSandboxActions}, string(id))
	assert.ErrorContains(t, err, "tool calls")
	_, err = sandboxActionsByCall(ctx, st, stmtSelectSandboxActions, string(id))
	assert.ErrorContains(t, err, "sandbox actions")
	_, err = waitingNotices(ctx, st, id, testReaders)
	assert.ErrorContains(t, err, "waiting notices")
}

// multiRowReads is every helper that scans a loop of rows: the reads whose failures
// the two tests around this swap the statement to provoke.
var multiRowReads = []stmtID{
	stmtSelectChats, stmtSelectMessages, stmtSelectChatIDsByCluster, stmtFailStrandedRuns,
	stmtSelectToolCalls, stmtSelectSandboxActions, stmtSelectWaitingNotices,
}

// A read that fails partway through its rows is reported, never taken as the rows
// that did arrive — a truncated list reads as chats the user deleted. Each statement
// is wrapped so that stepping past its own rows raises a SQLite error; the CTE is what
// keeps the wrapper indifferent to the statement's columns and parameters, and
// stmtFailStrandedRuns is written out because SQLite takes no UPDATE in one.
func TestAReadThatFailsPartwayIsReported(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	const overflow = `abs(-9223372036854775808) IS NOT NULL`
	swaps := map[stmtID]string{}
	for _, id := range multiRowReads {
		swaps[id] = `WITH q AS (` + statements[id].Text + `) SELECT * FROM q UNION ALL SELECT * FROM q WHERE ` + overflow
	}
	swaps[stmtFailStrandedRuns] = `SELECT chat_id FROM agent_runs UNION ALL SELECT '' WHERE ` + overflow
	db := openTestDB(t, t.TempDir())
	st := prepareTable(t, db, swapped(swaps)).Stmts()
	c := seedChat(t, db, aChat("1", time.UnixMilli(1_000).UTC()))
	turn := seedTurn(t, db, c.ID, time.UnixMilli(1_000).UTC())

	_, err := listChats(ctx, st)
	assert.ErrorContains(t, err, "list chats")
	_, err = listMessages(ctx, st, c.ID, testReaders)
	assert.ErrorContains(t, err, "list messages")
	_, err = chatIDsByCluster(ctx, st, "1")
	assert.ErrorContains(t, err, "chat ids by cluster")
	_, err = failStrandedRuns(ctx, st, "reason", time.UnixMilli(1_000).UTC())
	assert.ErrorContains(t, err, "fail stranded runs")
	seedCallRows(t, db, turn.Run, c.ID)
	assertCallReadsFail(t, st, c.ID)
}

// seedCallRows files under run one call with a cluster write, whose task has
// ended with its notice waiting: a row for each read of a chat's calls.
func seedCallRows(t *testing.T, db *appdb.DB, run RunID, chatID ChatID) {
	t.Helper()
	call, toolCall := appdb.NewID(), appdb.NewID()
	for _, q := range []struct {
		text string
		args []any
	}{
		{`INSERT INTO llm_calls (id, run_id, seq, provider, model, started_at) VALUES (?, ?, 0, 'fake', 'fake', 0)`, []any{call, string(run)}},
		{`INSERT INTO tool_calls (id, llm_call_id, seq, tool_name, contract_name, status, created_at) VALUES (?, ?, 0, 'echo', 'echo/v1', 'succeeded', 0)`, []any{toolCall, call}},
		{`INSERT INTO approvals (id, tool_call_id, kind, request, status, reason, created_at, decided_at) VALUES (?, ?, 'action', '{}', 'allowed', 'auto', 0, 0)`, []any{appdb.NewID(), toolCall}},
		{`INSERT INTO background_tasks (id, chat_id, tool_call_id, output_path, status, started_at, finished_at) VALUES (?, ?, ?, '/r/t.output', 'exited', 0, 0)`, []any{appdb.NewID(), string(chatID), toolCall}},
	} {
		_, err := db.Write.Exec(q.text, q.args...)
		require.NoError(t, err, q.text)
	}
}

// A replayed send whose calls cannot be read is the read's error, never an answer
// that made no calls.
func TestAReplayWhoseCallsCannotBeReadIsReported(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareTable(t, db, swapped(map[stmtID]string{stmtSelectRunToolCalls: `SELECT 1, 2`})).Stmts()
	c := seedChat(t, db, aChat("1", time.UnixMilli(1_000).UTC()))
	turn := seedTurn(t, db, c.ID, time.UnixMilli(1_000).UTC())
	var key string
	require.NoError(t, db.Read.QueryRow(`SELECT request_key FROM messages WHERE id = ?`, string(turn.User)).Scan(&key))

	_, _, err := answerByRequestKey(t.Context(), st, key, testReaders)

	assert.ErrorContains(t, err, "answer by request key")
}

// The watches re-read on every ping, and a write transaction is what they would
// otherwise queue behind. The reader pool is what lets a read return while the writer
// holds a transaction open, and nothing else in the suite would notice its loss: the
// read would park in database/sql's wait for the writer's one connection, which no
// busy_timeout bounds. So the read carries a deadline — the bounded window a "must not
// block" assertion is allowed.
func TestAReadDoesNotWaitForAHeldWriteTransaction(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	s := prepareOn(t, db)
	c := seedChat(t, db, aChat("1", time.UnixMilli(1_000).UTC()))

	errRollBack := errors.New("roll back")
	err := s.InTx(t.Context(), func(st stmts) error {
		_, ok, err := renameChat(t.Context(), st, c.ID, "renamed", time.UnixMilli(2_000).UTC())
		require.NoError(t, err)
		require.True(t, ok)

		ctx, cancel := context.WithTimeout(t.Context(), testutil.Timeout)
		defer cancel()
		chats, err := listChats(ctx, s.Stmts())
		require.NoError(t, err, "the read waited for the writer")
		require.Len(t, chats, 1)
		assert.Equal(t, "t", chats[0].Title, "the read saw an uncommitted write")
		return errRollBack
	})
	require.ErrorIs(t, err, errRollBack)
}

// --- chats ---

// The row carries its mode, its cluster and its title, and both reads see them: the
// list is what the client filters by cluster and mode.
func TestAChatRoundTrips(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	c := aChat("7", time.UnixMilli(1_000).UTC())
	c.Title, c.Mode = "pods?", ModeDashboard
	seedChat(t, db, c)

	got, ok, err := getChat(t.Context(), st, c.ID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, c, got)

	chats, err := listChats(t.Context(), st)
	require.NoError(t, err)
	assert.Equal(t, []Chat{c}, chats)
}

// title is nullable in the table; the service reads a NULL as "".
func TestAChatWithNoTitleReadsAsEmpty(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	_, err := db.Write.Exec(`INSERT INTO chats (id, cluster_id, mode, created_at, updated_at) VALUES ('c', '1', 'chat', 0, 0)`)
	require.NoError(t, err)

	got, ok, err := getChat(t.Context(), st, "c")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "", got.Title)
}

func TestChatModeIsCheckedByTheColumn(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	_, err := db.Write.Exec(`INSERT INTO chats (id, cluster_id, mode, created_at, updated_at) VALUES ('c', '1', 'sideways', 0, 0)`)
	assert.ErrorContains(t, err, "CHECK")
}

// A run's dialect is checked non-empty alone: the set is llm.Dialects' to list.
func TestARunDialectIsCheckedByTheColumn(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	_, err := db.Write.Exec(`INSERT INTO agent_runs (id, agent_type, app_version, trigger, cluster_id, provider, model, dialect, created_at)
		VALUES ('r', 'monitor', 'test', 'monitor', '1', 'fake', 'fake', '', 0)`)
	assert.ErrorContains(t, err, "CHECK")
}

// A monitor's run is filed under its cluster and no chat, queued, its task the
// brief; the CHECKs keep a cluster on a monitor's run alone.
func TestAMonitorRunIsFiledUnderItsClusterWithNoChat(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	run := agentRun{
		ID: newRunID(), ClusterID: "7", ProviderID: "fake", ModelID: "fake", Effort: "low",
		Dialect: "fake", Task: "look", AppVersion: "test", CreatedAt: time.UnixMilli(1_000).UTC(),
	}

	require.NoError(t, insertMonitorRun(t.Context(), st, run))

	var (
		agentType, trigger, task, status string
		chatID, clusterID                sql.NullString
	)
	require.NoError(t, db.Read.QueryRow(`SELECT agent_type, trigger, chat_id, cluster_id, task, status FROM agent_runs WHERE id = ?`,
		string(run.ID)).Scan(&agentType, &trigger, &chatID, &clusterID, &task, &status))
	assert.Equal(t, "monitor", agentType)
	assert.Equal(t, "monitor", trigger)
	assert.False(t, chatID.Valid)
	assert.Equal(t, "7", clusterID.String)
	assert.Equal(t, "look", task)
	assert.Equal(t, string(RunQueued), status)

	c := seedChat(t, db, aChat("7", time.UnixMilli(1_000).UTC()))
	for _, row := range []struct {
		name, trigger string
		chat, cluster any
	}{
		{"a monitor's run with a chat", "monitor", string(c.ID), "7"},
		{"a monitor's run with no cluster", "monitor", nil, nil},
		{"a chat's run with a cluster", "chat", string(c.ID), "7"},
	} {
		_, err := db.Write.Exec(`INSERT INTO agent_runs (id, agent_type, app_version, trigger, chat_id, cluster_id, provider, model, dialect, created_at)
			VALUES (?, 'x', 'test', ?, ?, ?, 'fake', 'fake', 'fake', 0)`, appdb.NewID(), row.trigger, row.chat, row.cluster)
		assert.ErrorContains(t, err, "CHECK", row.name)
	}
}

// A cluster's delete takes its monitor's runs and their rows.
func TestAClustersDeleteTakesItsMonitorRuns(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	run := agentRun{ID: newRunID(), ClusterID: "7", ProviderID: "fake", ModelID: "fake", Dialect: "fake", Task: "look", AppVersion: "test"}
	require.NoError(t, insertMonitorRun(t.Context(), st, run))
	_, err := db.Write.Exec(`INSERT INTO llm_calls (id, run_id, seq, provider, model, started_at) VALUES ('l', ?, 0, 'fake', 'fake', 0)`, string(run.ID))
	require.NoError(t, err)

	_, err = db.Write.Exec(`DELETE FROM clusters WHERE id = '7'`)
	require.NoError(t, err)

	assert.Zero(t, tableCount(t, db, "agent_runs"))
	assert.Zero(t, tableCount(t, db, "llm_calls"))
}

// Whether a monitor may run on a cluster: a row that is there and unmarked,
// and whose switch is on.
func TestClusterMonitoring(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	setMonitoring(t, db, "1", false)
	markCluster(t, db, "8")

	for id, want := range map[apimeta.ClusterID][2]bool{
		"7": {true, true}, "1": {true, false}, "8": {false, false}, "nope": {false, false},
	} {
		found, enabled, err := clusterMonitoring(t.Context(), st, id)
		require.NoError(t, err)
		assert.Equal(t, want, [2]bool{found, enabled}, id)
	}
}

// The clusters a monitor may be kept under are the unmarked rows.
func TestLiveClusterIDsAreTheUnmarkedRows(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	markCluster(t, db, "8")

	ids, err := liveClusterIDs(t.Context(), st)
	require.NoError(t, err)
	assert.ElementsMatch(t, []apimeta.ClusterID{"1", "2", "7", "9", "42"}, ids)
}

// The list sorts by updated_at: a chat moves to the top when its row is touched.
func TestListChatsIsNewestActivityFirst(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	older := seedChat(t, db, aChat("1", time.UnixMilli(1_000).UTC()))
	newer := seedChat(t, db, aChat("1", time.UnixMilli(2_000).UTC()))

	chats, err := listChats(t.Context(), st)
	require.NoError(t, err)
	require.Len(t, chats, 2)
	assert.Equal(t, newer.ID, chats[0].ID)

	_, err = db.Write.Exec(`UPDATE chats SET updated_at = 3000 WHERE id = ?`, string(older.ID))
	require.NoError(t, err)
	chats, err = listChats(t.Context(), st)
	require.NoError(t, err)
	assert.Equal(t, older.ID, chats[0].ID)
}

func TestRenameChatReturnsTheRowItWroteOrNothing(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	c := seedChat(t, db, aChat("1", time.UnixMilli(1_000).UTC()))

	got, ok, err := renameChat(t.Context(), st, c.ID, "renamed", time.UnixMilli(2_000).UTC())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "renamed", got.Title)
	assert.Equal(t, time.UnixMilli(2_000).UTC(), got.UpdatedAt)

	_, ok, err = renameChat(t.Context(), st, "nope", "renamed", time.UnixMilli(2_000).UTC())
	require.NoError(t, err)
	assert.False(t, ok)
}

// What the cluster sweep reads: the ids of one cluster's chats and nobody else's.
func TestChatIDsByClusterNamesOnlyThatClusters(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	now := time.UnixMilli(1_000).UTC()
	mine := seedChat(t, db, aChat("7", now))
	seedChat(t, db, aChat("8", now))

	ids, err := chatIDsByCluster(t.Context(), st, "7")
	require.NoError(t, err)
	assert.Equal(t, []ChatID{mine.ID}, ids)

	deleted, err := deleteChat(t.Context(), st, mine.ID)
	require.NoError(t, err)
	assert.True(t, deleted)
	ids, err = chatIDsByCluster(t.Context(), st, "7")
	require.NoError(t, err)
	assert.Empty(t, ids)
}

// The messages, the runs and their call rows go with the chat: ON DELETE
// CASCADE, with foreign keys on in the writer's DSN.
func TestDeletingAChatRemovesItsCalls(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	s := prepareOn(t, db)
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, db, aChat("1", now))
	turn := seedTurn(t, db, c.ID, now)
	_, err := db.Write.Exec(`INSERT INTO llm_calls (id, run_id, seq, provider, model, started_at) VALUES ('l', ?, 0, 'fake', 'fake', 0)`, string(turn.Run))
	require.NoError(t, err)
	_, err = db.Write.Exec(`INSERT INTO tool_calls (id, llm_call_id, seq, tool_name, created_at) VALUES ('t', 'l', 0, 'list_objects', 0)`)
	require.NoError(t, err)

	require.NoError(t, s.InTx(t.Context(), func(st stmts) error { _, err := deleteChat(t.Context(), st, c.ID); return err }))

	for _, table := range []string{"messages", "agent_runs", "llm_calls", "tool_calls"} {
		assert.Zero(t, tableCount(t, db, table), table)
	}
}

// --- messages and agent_runs ---

// A send's three rows read back as two messages in seq order: the question
// Complete with nothing of a run on it, the answer Streaming with what its run was
// asked to run.
func TestATurnReadsBackAsItsTwoMessages(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, db, aChat("1", now))
	turn := seedTurn(t, db, c.ID, now)

	msgs, err := listMessages(t.Context(), st, c.ID, testReaders)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	user := msgs[0]
	assert.Equal(t, turn.User, user.ID)
	assert.Equal(t, RoleUser, user.Role)
	assert.Equal(t, int64(0), user.Seq)
	assert.Equal(t, StatusComplete, user.Status)
	assert.Equal(t, RunID(""), user.RunID)
	assert.Equal(t, []string{"", "", "", "", ""}, []string{user.Error, user.ProviderID, user.ModelID, user.Effort, user.FinishReason})
	assert.Equal(t, sql.NullTime{}, user.FinishedAt)
	assert.Equal(t, now, user.CreatedAt)

	got := msgs[1]
	assert.Equal(t, turn.Assistant, got.ID)
	assert.Equal(t, int64(1), got.Seq)
	assert.Equal(t, StatusStreaming, got.Status)
	assert.Equal(t, turn.Run, got.RunID)
	assert.Equal(t, []string{"fake", "fake", "high"}, []string{got.ProviderID, got.ModelID, got.Effort})
	assert.Equal(t, sql.NullTime{}, got.FinishedAt)
	assert.Equal(t, rawjson.RawJSON("[]"), got.Content)
}

// A model with no effort knob stores NULL, and the read gives it back as "".
func TestARunWithNoEffortRoundTripsAsEmpty(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, db, aChat("1", now))
	turn := seedTurn(t, db, c.ID, now)
	_, err := db.Write.Exec(`UPDATE agent_runs SET effort = NULL WHERE id = ?`, string(turn.Run))
	require.NoError(t, err)

	msgs, err := listMessages(t.Context(), st, c.ID, testReaders)
	require.NoError(t, err)
	assert.Equal(t, "", msgs[1].Effort)
}

// Each run status reads as the public status the transcript draws, and the finish
// time reads as FinishedAt once the run has one.
func TestAMessagesStatusIsItsRuns(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, db, aChat("1", now))
	turns := map[RunStatus]seededTurn{}
	for _, status := range []RunStatus{RunQueued, RunRunning, RunWaitingApproval, RunSucceeded, RunFailed, RunCancelled} {
		turn := seedTurn(t, db, c.ID, now)
		setRunStatus(t, db, turn.Run, status)
		turns[status] = turn
	}
	settleSeededRun(t, db, turns[RunSucceeded].Run, RunSucceeded, time.UnixMilli(5_000).UTC())

	msgs, err := listMessages(t.Context(), st, c.ID, testReaders)
	require.NoError(t, err)
	byID := map[MessageID]ChatMessage{}
	for _, m := range msgs {
		byID[m.ID] = m
	}
	want := map[RunStatus]MessageStatus{
		RunQueued: StatusStreaming, RunRunning: StatusStreaming, RunWaitingApproval: StatusWaitingApproval,
		RunSucceeded: StatusComplete, RunFailed: StatusFailed, RunCancelled: StatusCancelled,
	}
	for status, turn := range turns {
		assert.Equal(t, want[status], byID[turn.Assistant].Status, string(status))
	}
	assert.Equal(t, sql.NullTime{Time: time.UnixMilli(5_000).UTC(), Valid: true}, byID[turns[RunSucceeded].Assistant].FinishedAt)
	assert.Equal(t, sql.NullTime{}, byID[turns[RunFailed].Assistant].FinishedAt)
}

func TestFailStrandedRunsFailsEveryUnfinishedRun(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, db, aChat("1", now))
	queued := seedTurn(t, db, c.ID, now)
	running := seedTurn(t, db, c.ID, now)
	setRunStatus(t, db, running.Run, RunRunning)
	waiting := seedTurn(t, db, c.ID, now)
	setRunStatus(t, db, waiting.Run, RunWaitingApproval)
	done := seedTurn(t, db, c.ID, now)
	settleSeededRun(t, db, done.Run, RunSucceeded, now)

	stranded, err := failStrandedRuns(t.Context(), st, "stranded", time.UnixMilli(9_000).UTC())
	require.NoError(t, err)
	assert.Equal(t, []ChatID{c.ID, c.ID, c.ID}, stranded, "one entry per run, so a watcher of each chat can be told")

	msgs, err := listMessages(t.Context(), st, c.ID, testReaders)
	require.NoError(t, err)
	byID := map[MessageID]ChatMessage{}
	for _, m := range msgs {
		byID[m.ID] = m
	}
	for _, id := range []MessageID{queued.Assistant, running.Assistant, waiting.Assistant} {
		assert.Equal(t, StatusFailed, byID[id].Status)
		assert.Equal(t, "stranded", byID[id].Error)
		assert.Equal(t, time.UnixMilli(9_000).UTC(), byID[id].FinishedAt.Time)
	}
	assert.Equal(t, StatusComplete, byID[done.Assistant].Status)
	assert.Equal(t, "", byID[done.Assistant].Error)
}

// FinishReason is the last model call's by seq, whatever the run's status; a
// user message, with no calls, has none. The ids are minted against the seqs, so
// a read that ordered by id would answer the other call.
func TestFinishReasonIsTheLastModelCalls(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, db, aChat("1", now))
	turn := seedTurn(t, db, c.ID, now)
	for seq, reason := range []string{"end_turn", "tool_use"} {
		_, err := db.Write.Exec(`INSERT INTO llm_calls (id, run_id, seq, provider, model, stop_reason, started_at) VALUES (?, ?, ?, 'fake', 'fake', ?, 0)`,
			appdb.NewID(), string(turn.Run), 1-seq, reason)
		require.NoError(t, err)
	}

	msgs, err := listMessages(t.Context(), st, c.ID, testReaders)
	require.NoError(t, err)
	assert.Equal(t, "end_turn", msgs[1].FinishReason)
	assert.Equal(t, "", msgs[0].FinishReason)
}

// A report is stored with its input uncached, and read back inclusive. An absent
// report, or one whose cache counts exceed its input, stores nothing; a reported
// zero is a zero.
func TestUsageIsStoredUncachedAndReadInclusive(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		u    llm.Usage
		want [4]sql.NullInt64
		in   sql.NullInt64
	}{
		"read and write": {llm.Usage{Reported: true, InputTokens: 12, CacheReadTokens: 6, CacheWriteTokens: 4, OutputTokens: 3}, [4]sql.NullInt64{count(2), count(6), count(4), count(3)}, count(12)},
		"read alone":     {llm.Usage{Reported: true, InputTokens: 12, CacheReadTokens: 6, OutputTokens: 3}, [4]sql.NullInt64{count(6), count(6), count(0), count(3)}, count(12)},
		"all zero":       {llm.Usage{Reported: true}, [4]sql.NullInt64{count(0), count(0), count(0), count(0)}, count(0)},
		"absent":         {llm.Usage{}, [4]sql.NullInt64{}, sql.NullInt64{}},
		"inconsistent":   {llm.Usage{Reported: true, InputTokens: 12, CacheReadTokens: 15, OutputTokens: 3}, [4]sql.NullInt64{}, sql.NullInt64{}},
	} {
		t.Run(name, func(t *testing.T) {
			var c llmCallEntry
			c.setUsage(tc.u)

			assert.Equal(t, tc.want, [4]sql.NullInt64{c.UncachedTokens, c.CacheReadTokens, c.CacheWriteTokens, c.OutputTokens})
			assert.Equal(t, tc.in, c.inputTokens())
		})
	}
}

// --- the send's rows ---

func TestNextSeqCountsFromZeroPerChat(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	now := time.UnixMilli(1_000).UTC()
	a := seedChat(t, db, aChat("1", now))
	b := seedChat(t, db, aChat("1", now))
	seedTurn(t, db, a.ID, now)

	seq, err := nextSeq(t.Context(), st, a.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), seq)
	seq, err = nextSeq(t.Context(), st, b.ID)
	require.NoError(t, err)
	assert.Zero(t, seq)
}

func TestClaimRunTakesOnlyAQueuedRun(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, db, aChat("1", now))
	turn := seedTurn(t, db, c.ID, now)

	require.NoError(t, claimRun(t.Context(), st, turn.Run, now))
	assert.Equal(t, RunRunning, runStatusOf(t, db, turn.Run))
	assert.ErrorIs(t, claimRun(t.Context(), st, turn.Run, now), errRunNotQueued, "claimed twice")
	assert.ErrorIs(t, claimRun(t.Context(), st, RunID(appdb.NewID()), now), errRunNotQueued, "no such run")
}

func TestWriteContentLeavesTheRunAlone(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, db, aChat("1", now))
	turn := seedTurn(t, db, c.ID, now)

	require.NoError(t, writeContent(t.Context(), st, turn.Assistant, marshalBlocks([]llm.Block{llm.TextBlock("so far")})))

	msgs, err := listMessages(t.Context(), st, c.ID, testReaders)
	require.NoError(t, err)
	assert.Equal(t, marshalBlocks([]llm.Block{llm.TextBlock("so far")}), msgs[1].Content)
	assert.Equal(t, StatusStreaming, msgs[1].Status)
}

// The key alone finds the answer: neither the chat nor the content is part of it.
func TestAnswerByRequestKeyIsFoundByTheKeyAlone(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, db, aChat("1", now))
	turn := seedTurn(t, db, c.ID, now)
	var key string
	require.NoError(t, db.Read.QueryRow(`SELECT request_key FROM messages WHERE id = ?`, string(turn.User)).Scan(&key))

	m, ok, err := answerByRequestKey(t.Context(), st, key, testReaders)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, turn.Assistant, m.ID)
	assert.Equal(t, StatusStreaming, m.Status)

	_, ok, err = answerByRequestKey(t.Context(), st, appdb.NewID(), testReaders)
	require.NoError(t, err)
	assert.False(t, ok)
}

// What a settle writes is what the read serves: the content, the status, the
// finish reason off the call row, the completion time.
func TestASettledTurnEqualsItsOwnRoundTrip(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, db, aChat("1", now))
	turn := seedTurn(t, db, c.ID, now)
	call := llmCallEntry{ID: newLLMCallID(), RunID: turn.Run, ProviderID: "fake", ModelID: "fake", StartedAt: now}
	require.NoError(t, claimRun(t.Context(), st, turn.Run, now))
	require.NoError(t, insertLLMCall(t.Context(), st, call))

	done := time.UnixMilli(3_000).UTC()
	answer := marshalBlocks([]llm.Block{llm.TextBlock("Twelve pods.")})
	require.NoError(t, writeContent(t.Context(), st, turn.Assistant, answer))
	require.NoError(t, settleRun(t.Context(), st, turn.Run, RunSucceeded, "", "", done))
	call.StopReason, call.FinishedAt = "end_turn", done
	require.NoError(t, closeLLMCall(t.Context(), st, call))

	msgs, err := listMessages(t.Context(), st, c.ID, testReaders)
	require.NoError(t, err)
	got := msgs[1]
	assert.Equal(t, answer, got.Content)
	assert.Equal(t, StatusComplete, got.Status)
	assert.Equal(t, "end_turn", got.FinishReason)
	assert.Equal(t, sql.NullTime{Time: done, Valid: true}, got.FinishedAt)
	assert.Empty(t, got.Error)
}

// The live list and the stored read are one function over the rows, so they agree
// byte for byte on every status — with the characters Go's encoder and SQLite's
// json_object spell differently in the arguments and the output — and a message
// with no rows reads [].
func TestToolCallsMatchTheStoredRead(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	set := prepareOn(t, db)
	ctx := t.Context()
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, db, aChat("1", now))
	turn := seedTurn(t, db, c.ID, now)
	call := llmCallEntry{ID: newLLMCallID(), RunID: turn.Run, ProviderID: "fake", ModelID: "fake", StartedAt: now}
	started := nullMillis(now)
	finished := nullMillis(now.Add(time.Second))
	odd := "ls > a && cat a <b>   é"
	oddDescription := "List \"a\" <b>\u202e é\nand more"
	args := `{"command":"` + odd + `"}`
	describedArgs, _ := json.Marshal(map[string]string{"command": odd, "description": oddDescription})
	row := func(seq int, status, result, errText string, start sql.NullInt64) toolCallEntry {
		return toolCallEntry{
			ID: newToolCallID(), LLMCallID: call.ID, Seq: seq, Name: bash.Name, Contract: string(bash.ContractName), ToolUseID: "toolu_" + string(rune('a'+seq)),
			Arguments: args, Result: result, Error: errText, Status: status, CreatedAt: now,
			StartedAt: start, FinishedAt: finished,
		}
	}
	decided := func(tc toolCallEntry, status ApprovalStatus) toolCallEntry {
		tc.IsMutating, tc.Cwd = true, "/srv/app"
		tc.Approval = &approval{ID: newApprovalID(), ToolCallID: tc.ID, Status: status, CreatedAt: now}
		return tc
	}
	described := func(tc toolCallEntry) toolCallEntry {
		tc.Arguments = string(describedArgs)
		return tc
	}
	rows := []toolCallEntry{
		row(0, toolSucceeded, odd+"\n", "", started),
		row(1, toolFailed, "boom\nexit status 1", toolErrorOwn, started),
		row(2, toolFailed, `{"error":"not-run"}`, `{"error":"not-run"}`, sql.NullInt64{}),
		row(3, toolFailed, `{"error":"cancelled"}`, `{"error":"cancelled"}`, started),
		row(4, toolFailed, `{"error":"timeout"}`, `{"error":"timeout"}`, started),
		decided(row(5, toolDenied, `{"error":"denied"}`, `{"error":"denied"}`, sql.NullInt64{}), ApprovalDenied),
		described(decided(row(6, toolAwaitingApproval, "", "", sql.NullInt64{}), ApprovalPending)),
		decided(row(7, toolRunning, "", "", started), ApprovalApproved),
	}
	rows[6].FinishedAt, rows[7].FinishedAt = sql.NullInt64{}, sql.NullInt64{}
	require.NoError(t, set.InTx(ctx, func(st stmts) error {
		require.NoError(t, insertLLMCall(ctx, st, call))
		for _, r := range rows {
			require.NoError(t, upsertToolCall(ctx, st, r))
			if r.Approval != nil {
				require.NoError(t, upsertApproval(ctx, st, *r.Approval))
			}
		}
		return nil
	}))

	var msgs []ChatMessage
	require.NoError(t, set.InReadTx(ctx, func(st stmts) (err error) {
		msgs, err = listMessages(ctx, st, c.ID, testReaders)
		return err
	}))
	require.Len(t, msgs, 2)
	assert.Equal(t, emptyToolCalls, msgs[0].ToolCalls, "a question has no calls")
	live := marshalToolCalls(rows, testReaders)
	assert.Equal(t, live, msgs[1].ToolCalls)

	var key string
	require.NoError(t, db.Read.QueryRow(`SELECT request_key FROM messages WHERE id = ?`, string(turn.User)).Scan(&key))
	answer, ok, err := answerByRequestKey(ctx, set.Stmts(), key, testReaders)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, live, answer.ToolCalls, "the repeated send's read is the same")

	var got []ToolCall
	require.NoError(t, json.Unmarshal([]byte(live), &got))
	var statuses []ToolCallStatus
	for _, tc := range got {
		statuses = append(statuses, tc.Status)
	}
	assert.Equal(t, []ToolCallStatus{
		ToolCallSucceeded, ToolCallFailed, ToolCallNotRun, ToolCallInterrupted, ToolCallInterrupted,
		ToolCallDenied, ToolCallAwaitingApproval, ToolCallRunning,
	}, statuses)
	assert.Equal(t, odd+"\n", got[0].Output)
	assert.False(t, got[0].IsError)
	assert.Equal(t, "boom\nexit status 1", got[1].Output, "a tool's own error reads as the model read it")
	assert.True(t, got[1].IsError)
	assert.JSONEq(t, args, string(got[0].Arguments))
	assert.Nil(t, got[0].Approval, "no one was asked about an ungated call")
	for i, tc := range got {
		require.NotNil(t, tc.Action, "call %d", i)
		assert.Equal(t, odd, tc.Action.Command.Text, "call %d", i)
		require.NotNil(t, tc.ActionKind, "call %d", i)
		assert.Equal(t, tools.ActionCommand, *tc.ActionKind, "call %d", i)
	}
	assert.Empty(t, got[0].Action.Command.Cwd, "a call that never reached the gate has no cwd")
	require.NotNil(t, got[6].Approval)
	assert.Equal(t, ToolCallApproval{ID: rows[6].Approval.ID, Status: ApprovalPending}, *got[6].Approval)
	assert.Equal(t, tools.Action{
		Description: oddDescription,
		Command:     &tools.CommandAction{Text: odd, Cwd: "/srv/app"},
	}, *got[6].Action)
	assert.Empty(t, got[5].Action.Description, "a gated call without a description reads empty")
	assert.Equal(t, ApprovalDenied, got[5].Approval.Status)
	assert.Equal(t, ApprovalApproved, got[7].Approval.Status, "a decided approval stays on the call")
}

// seedLLMCall is a model call under a seeded turn, for a test of its tool rows.
func seedLLMCall(t *testing.T, db *appdb.DB, st stmts) LLMCallID {
	t.Helper()
	now := time.UnixMilli(1_000).UTC()
	turn := seedTurn(t, db, seedChat(t, db, aChat("1", now)).ID, now)
	call := llmCallEntry{ID: newLLMCallID(), RunID: turn.Run, ProviderID: "fake", ModelID: "fake", StartedAt: now}
	require.NoError(t, insertLLMCall(t.Context(), st, call))
	return call.ID
}

// providerSearchRow is a search's row as ServerCallSeen writes it, under callID.
func providerSearchRow(callID LLMCallID) toolCallEntry {
	return toolCallEntry{
		ID: newToolCallID(), LLMCallID: callID, ByProvider: true, Name: anthropicwebsearch.Name,
		Contract: string(anthropicwebsearch.ContractName), Arguments: `{}`, CreatedAt: time.UnixMilli(2_000).UTC(),
	}
}

// A provider row carries none of the sidecar's lifecycle and a sidecar row always
// has a status: the schema refuses either shape broken.
func TestTheToolCallRowShapeIsChecked(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	callID := seedLLMCall(t, db, st)
	require.NoError(t, upsertToolCall(t.Context(), st, providerSearchRow(callID)), "a provider row with no lifecycle")

	now := nullMillis(time.UnixMilli(2_000).UTC())
	broken := map[string]func(*toolCallEntry){
		"a provider row with a status": func(r *toolCallEntry) { r.Status = toolSucceeded },
		"a provider row with a start":  func(r *toolCallEntry) { r.StartedAt = now },
		"a provider row with a finish": func(r *toolCallEntry) { r.FinishedAt = now },
		"a sidecar row with no status": func(r *toolCallEntry) { r.ByProvider, r.Contract = false, "" },
	}
	for name, breakRow := range broken {
		row := providerSearchRow(callID)
		row.Seq = 1
		breakRow(&row)

		assert.Error(t, upsertToolCall(t.Context(), st, row), name)
	}
}

// A provider row has no finish to be stranded without: the sweep that closes
// what a previous process left open passes it by.
func TestTheStrandedSweepLeavesProviderRows(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	callID := seedLLMCall(t, db, st)
	running := toolCallEntry{
		ID: newToolCallID(), LLMCallID: callID, Seq: 1, Name: "echo", Status: toolRunning,
		StartedAt: nullMillis(time.UnixMilli(2_000).UTC()), CreatedAt: time.UnixMilli(2_000).UTC(),
	}
	require.NoError(t, upsertToolCall(t.Context(), st, providerSearchRow(callID)))
	require.NoError(t, upsertToolCall(t.Context(), st, running))

	require.NoError(t, closeStrandedToolCalls(t.Context(), st, time.UnixMilli(3_000).UTC()))

	rows, err := db.Read.Query(`SELECT status FROM tool_calls ORDER BY seq`)
	require.NoError(t, err)
	defer rows.Close()
	var statuses []sql.NullString
	for rows.Next() {
		var s sql.NullString
		require.NoError(t, rows.Scan(&s))
		statuses = append(statuses, s)
	}
	assert.Equal(t, []sql.NullString{{}, {String: toolFailed, Valid: true}}, statuses)
}

// A task's row takes an agent's ends as well as a command's, and a stop for a
// request that went unanswered.
func TestATaskRowTakesAnAgentsEnds(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, db, aChat("1", now))
	turn := seedTurn(t, db, c.ID, now)
	for _, q := range []string{
		`INSERT INTO llm_calls (id, run_id, seq, provider, model, started_at) VALUES ('l', '` + string(turn.Run) + `', 0, 'fake', 'fake', 0)`,
		`INSERT INTO tool_calls (id, llm_call_id, seq, tool_name, contract_name, tool_use_id, arguments, status, created_at)
		 VALUES ('t', 'l', 0, 'Agent', 'agent/v1', 'call-1', '{}', 'running', 0)`,
	} {
		_, err := db.Write.Exec(q)
		require.NoError(t, err, q)
	}
	ends := []taskEnd{
		{Status: taskCompleted, At: now},
		{Status: taskFailed, At: now},
		{Status: taskStopped, StoppedBy: stoppedByUnanswered, At: now},
	}
	for _, end := range ends {
		require.NoError(t, insertTask(t.Context(), st, "task", c.ID, "t", "/r/task.output", now))
		assert.NoError(t, finishTask(t.Context(), st, "task", end), end.Status)
		require.NoError(t, deleteTask(t.Context(), st, "task"))
	}
}

// A task's row takes a watch's end with its result, which a command's and an
// agent's leave NULL.
func TestATaskRowTakesAWatchsEndWithItsResult(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, db, aChat("1", now))
	turn := seedTurn(t, db, c.ID, now)
	for _, q := range []string{
		`INSERT INTO llm_calls (id, run_id, seq, provider, model, started_at) VALUES ('l', '` + string(turn.Run) + `', 0, 'fake', 'fake', 0)`,
		`INSERT INTO tool_calls (id, llm_call_id, seq, tool_name, contract_name, tool_use_id, arguments, status, created_at)
		 VALUES ('t', 'l', 0, 'LogsWatch', 'logswatch/v1', 'call-1', '{}', 'running', 0)`,
	} {
		_, err := db.Write.Exec(q)
		require.NoError(t, err, q)
	}
	result := sql.NullString{String: `{"text":"Watched 1 line."}`, Valid: true}
	require.NoError(t, insertTask(t.Context(), st, "task", c.ID, "t", "/r/task.output", now))
	require.NoError(t, finishTask(t.Context(), st, "task", taskEnd{Status: taskMatched, At: now, Result: result}))
	var stored sql.NullString
	require.NoError(t, db.Write.QueryRow(`SELECT result FROM background_tasks WHERE id = 'task'`).Scan(&stored))
	assert.Equal(t, result, stored)

	require.NoError(t, deleteTask(t.Context(), st, "task"))
	require.NoError(t, insertTask(t.Context(), st, "task", c.ID, "t", "/r/task.output", now))
	require.NoError(t, finishTask(t.Context(), st, "task", taskEnd{Status: taskExited, At: now}))
	require.NoError(t, db.Write.QueryRow(`SELECT result FROM background_tasks WHERE id = 'task'`).Scan(&stored))
	assert.False(t, stored.Valid, "a command has none")
}

// A tool row references its model call: one naming a call that is not there is
// refused, which is what makes the audit trail's parent real.
func TestAToolRowNeedsItsModelCall(t *testing.T) {
	t.Parallel()

	s := newTestSet(t)

	err := s.InTx(t.Context(), func(st stmts) error {
		return upsertToolCall(t.Context(), st, toolCallEntry{
			ID: newToolCallID(), LLMCallID: newLLMCallID(), Name: "echo", ToolUseID: "call-1", Arguments: `{}`,
			Status: toolRunning, CreatedAt: time.UnixMilli(1_000).UTC(),
		})
	})

	assert.ErrorContains(t, err, "FOREIGN KEY")
}

// flagReader reads bash's rows and keeps the sandboxed flag each read was handed.
type flagReader struct {
	bash.Reader
	seen *[]bool
}

func (r flagReader) Action(raw json.RawMessage, cwd string, sandboxed bool) (tools.Action, error) {
	*r.seen = append(*r.seen, sandboxed)
	return r.Reader.Action(raw, cwd, sandboxed)
}

// A stored call's action is read with its row's sandboxed flag, on the
// transcript's read and on the task-notice read alike.
func TestTheActionCarriesTheRowsFlag(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	now := time.UnixMilli(1_000).UTC()
	db := openTestDB(t, t.TempDir())
	set := prepareOn(t, db)
	c := seedChat(t, db, aChat("1", now))
	turn := seedTurn(t, db, c.ID, now)
	settleSeededRun(t, db, turn.Run, RunSucceeded, now)
	for _, q := range []string{
		`INSERT INTO llm_calls (id, run_id, seq, provider, model, started_at, finished_at) VALUES ('l', '` + string(turn.Run) + `', 0, 'fake', 'fake', 0, 0)`,
		`INSERT INTO tool_calls (id, llm_call_id, seq, tool_name, contract_name, tool_use_id, arguments, cwd, sandboxed, status, created_at, started_at, finished_at)
		 VALUES ('t', 'l', 0, 'Bash', 'bash/v1', 'call-1', '{"command":"make serve","run_in_background":true}', '/work', 1, 'succeeded', 0, 0, 0)`,
		`INSERT INTO background_tasks (id, chat_id, tool_call_id, output_path, status, exit_code, started_at, finished_at)
		 VALUES ('task-1', '` + string(c.ID) + `', 't', '/r/task-1.output', 'exited', 0, 0, 1)`,
	} {
		_, err := db.Write.Exec(q)
		require.NoError(t, err, q)
	}

	var seen []bool
	box := tools.NewBox(nil, flagReader{seen: &seen})
	msgs, err := listMessages(ctx, set.Stmts(), c.ID, box)
	require.NoError(t, err)
	var calls []ToolCall
	require.NoError(t, json.Unmarshal([]byte(msgs[1].ToolCalls), &calls))
	require.Len(t, calls, 1)
	assert.True(t, calls[0].Action.Command.Sandboxed, "the transcript's read")

	seen = nil
	notices, err := waitingNotices(ctx, set.Stmts(), c.ID, box)
	require.NoError(t, err)
	require.Len(t, notices, 1)
	assert.Equal(t, []bool{true}, seen, "the task-notice read")
	assert.Equal(t, "make serve", notices[0].command, "read under the row's contract")
}

// sidecarRow is a sidecar call's running row under callID.
func sidecarRow(callID LLMCallID) toolCallEntry {
	return toolCallEntry{
		ID: newToolCallID(), LLMCallID: callID, Name: bash.Name, Contract: string(bash.ContractName), Arguments: `{"command":"kubectl delete pod x"}`,
		Status: toolRunning, CreatedAt: time.UnixMilli(2_000).UTC(),
	}
}

// aWrite is a cluster write's approval on call, pending, created at millis.
func aWrite(call ToolCallID, millis int64) approval {
	return approval{
		ID: newApprovalID(), ToolCallID: call, Status: ApprovalPending, CreatedAt: time.UnixMilli(millis).UTC(),
		Request: &tools.ActionRequest{Write: &tools.ClusterWrite{Method: "DELETE", Path: "/api/v1/namespaces/web/pods/x"}},
	}
}

// A call has at most one approval of its own, and any number of writes.
func TestACallKeepsOneOwnApproval(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	row := sidecarRow(seedLLMCall(t, db, st))
	require.NoError(t, upsertToolCall(t.Context(), st, row))
	own := approval{ID: newApprovalID(), ToolCallID: row.ID, Status: ApprovalPending, CreatedAt: row.CreatedAt}

	require.NoError(t, upsertApproval(t.Context(), st, own))
	own.ID = newApprovalID()
	assert.Error(t, upsertApproval(t.Context(), st, own), "a second approval of the call's own")
	for i := range 3 {
		require.NoError(t, upsertApproval(t.Context(), st, aWrite(row.ID, int64(3_000+i))))
	}
	assert.Equal(t, 4, tableCount(t, db, "approvals"))
}

// Only a write is abandoned: a call's own approval is decided or left pending.
func TestAbandonedIsOnlyAWrites(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	row := sidecarRow(seedLLMCall(t, db, st))
	require.NoError(t, upsertToolCall(t.Context(), st, row))

	w := aWrite(row.ID, 3_000)
	w.Status = ApprovalAbandoned
	require.NoError(t, upsertApproval(t.Context(), st, w))
	own := approval{ID: newApprovalID(), ToolCallID: row.ID, Status: ApprovalAbandoned, CreatedAt: row.CreatedAt}
	assert.Error(t, upsertApproval(t.Context(), st, own))
}

// A call with writes reads as one call carrying them in the order asked, its own
// approval beside them, the stored read spelling them as the live list does. A
// write that waits carries its body; one that no longer waits does not.
func TestACallWithWritesReadsOnce(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	set := prepareOn(t, db)
	st := set.Stmts()
	ctx := t.Context()
	callID := seedLLMCall(t, db, st)
	row := sidecarRow(callID)
	row.IsMutating = true
	row.Approval = &approval{ID: newApprovalID(), ToolCallID: row.ID, Status: ApprovalApproved, CreatedAt: row.CreatedAt}
	for i, status := range []ApprovalStatus{ApprovalDenied, ApprovalAbandoned, ApprovalPending} {
		w := aWrite(row.ID, int64(3_000+i))
		w.Status = status
		w.Request.Write.ContentType, w.Request.Write.Body = "application/json", `{"n":`+fmt.Sprint(i)+`}`
		row.SandboxActions = append(row.SandboxActions, &w)
	}
	require.NoError(t, set.InTx(ctx, func(st stmts) error {
		require.NoError(t, upsertToolCall(ctx, st, row))
		require.NoError(t, upsertApproval(ctx, st, *row.Approval))
		for _, w := range row.SandboxActions {
			require.NoError(t, upsertApproval(ctx, st, *w))
		}
		return nil
	}))

	var runID RunID
	require.NoError(t, db.Read.QueryRow(`SELECT run_id FROM llm_calls WHERE id = ?`, string(callID)).Scan(&runID))
	byRun, err := toolCallsByRun(ctx, st, runCallReads, string(runID))
	require.NoError(t, err)

	require.Len(t, byRun[runID], 1, "the call reads once")
	stored := marshalToolCalls(byRun[runID], testReaders)
	assert.Equal(t, marshalToolCalls([]toolCallEntry{row}, testReaders), stored)
	var got []ToolCall
	require.NoError(t, json.Unmarshal([]byte(stored), &got))
	require.NotNil(t, got[0].Approval)
	assert.Equal(t, ApprovalApproved, got[0].Approval.Status)
	require.Len(t, got[0].SandboxActions, 3)
	var statuses []ApprovalStatus
	for _, w := range got[0].SandboxActions {
		statuses = append(statuses, w.Approval.Status)
	}
	assert.Equal(t, []ApprovalStatus{ApprovalDenied, ApprovalAbandoned, ApprovalPending}, statuses)
	assert.Empty(t, got[0].SandboxActions[0].Write.Body, "a decided write carries no body")
	assert.Empty(t, got[0].SandboxActions[1].Write.ContentType, "an abandoned one carries no media type")
	assert.Equal(t, `{"n":2}`, got[0].SandboxActions[2].Write.Body, "a write that waits carries its body")
}

// The approvals table holds its own shape: a call's row holds no request and
// an action's one, only an action is abandoned, allowed or refused, and a
// duration is the user's choice on an approval alone.
func TestTheApprovalChecksHold(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	row := sidecarRow(seedLLMCall(t, db, st))
	require.NoError(t, upsertToolCall(t.Context(), st, row))
	insert := func(kind string, request any, status string, duration any) error {
		_, err := db.Write.Exec(`INSERT INTO approvals (id, tool_call_id, kind, request, status, duration, created_at) VALUES (?, ?, ?, ?, ?, ?, 1)`,
			string(newApprovalID()), string(row.ID), kind, request, status, duration)
		return err
	}
	for name, err := range map[string]error{
		"a call with a request":       insert("call", "{}", "pending", nil),
		"an action without one":       insert("action", nil, "pending", nil),
		"an allowed call":             insert("call", nil, "allowed", nil),
		"a duration on a denial":      insert("action", "{}", "denied", "once"),
		"a kind outside its list":     insert("cluster", "{}", "pending", nil),
		"a duration outside its list": insert("action", "{}", "approved", "forever"),
	} {
		assert.Error(t, err, name)
	}
	assert.NoError(t, insert("action", "{}", "approved", "chat"))
	assert.NoError(t, insert("call", nil, "approved", "once"))
}

// viewBox reads LogsView's calls, as the service's box does.
var viewBox = tools.NewBox(nil, &logsview.Tool{})

// seedShownView is a settled LogsView call in a new chat whose shown_action is
// stored, returning the chat and the call.
func seedShownView(t *testing.T, db *appdb.DB, stored string) (ChatID, ToolCallID) {
	t.Helper()
	c := seedChat(t, db, aChat("1", time.Now()))
	id := seedViewCall(t, db, c.ID, "call-1", nil)
	_, err := db.Write.Exec(`UPDATE tool_calls SET shown_action = ? WHERE id = ?`, stored, string(id))
	require.NoError(t, err)
	return c.ID, id
}

// A shown action that does not decode costs its call the action, not the chat
// its read: the call shows its kind, and its view does not open.
func TestAShownActionThatDoesNotDecodeCostsItsCallAlone(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	set := prepareOn(t, db)
	chatID, id := seedShownView(t, db, `{"logsView":7}`)

	msgs, err := listMessages(t.Context(), set.Stmts(), chatID, viewBox)
	require.NoError(t, err)
	calls := toolCallsOf(t, msgs[1])
	require.Len(t, calls, 1)
	assert.Nil(t, calls[0].Action)
	assert.Equal(t, new(tools.ActionLogsView), calls[0].ActionKind)
	_, _, ok, err := viewCall(t.Context(), set.Stmts(), viewBox, chatID, id)
	require.NoError(t, err)
	assert.False(t, ok)
}

// A refused shown action logs its sentinel and none of the stored text.
func TestARefusedShownActionLogsNoneOfTheRow(t *testing.T) {
	// CaptureLogs swaps the process's default logger, so this test stays serial.

	db := openTestDB(t, t.TempDir())
	set := prepareOn(t, db)
	chatID, _ := seedShownView(t, db, `{"logsView":{"resolvedAt":"MARKER-5f3a"}}`)
	logs := testutil.CaptureLogs(t)

	_, err := listMessages(t.Context(), set.Stmts(), chatID, viewBox)
	require.NoError(t, err)

	assert.Contains(t, logs.String(), tools.ErrDecode.Error())
	assert.Contains(t, logs.String(), "shown_action")
	assert.NotContains(t, logs.String(), "MARKER")
}

// seedWatch is a matched LogsWatch call in a new chat whose task's result is
// stored, under contract, returning the chat and the call.
func seedWatch(t *testing.T, db *appdb.DB, contract, stored string) (ChatID, ToolCallID) {
	t.Helper()
	c := seedChat(t, db, aChat("1", time.Now()))
	around := readWebapp().ViewAround
	id := seedViewCall(t, db, c.ID, "call-1", &tools.Action{LogsWatch: &tools.LogsWatchAction{LogsScopeInput: around.LogsScopeInput, Limit: 1}})
	seedWatchTask(t, db, c.ID, id, taskMatched, nil)
	_, err := db.Write.Exec(`UPDATE tool_calls SET contract_name = ? WHERE id = ?`, contract, string(id))
	require.NoError(t, err)
	_, err = db.Write.Exec(`UPDATE background_tasks SET result = ? WHERE tool_call_id = ?`, stored, string(id))
	require.NoError(t, err)
	return c.ID, id
}

// watchReads is what the transcript, the notices and the view read of a watch:
// its matched lines, its notice's text and count, and whether its view opens.
func watchReads(t *testing.T, set *sqlstmt.Set[stmtID], chatID ChatID, id ToolCallID) (matched int, notice llm.TaskNotice, viewed bool) {
	t.Helper()
	st := set.Stmts()
	msgs, err := listMessages(t.Context(), st, chatID, testReaders)
	require.NoError(t, err)
	calls := toolCallsOf(t, msgs[1])
	require.Len(t, calls, 1)
	require.NotNil(t, calls[0].Background)
	waiting, err := waitingNotices(t.Context(), st, chatID, testReaders)
	require.NoError(t, err)
	require.Len(t, waiting, 1)
	_, _, viewed, err = viewCall(t.Context(), st, testReaders, chatID, id)
	require.NoError(t, err)
	return len(calls[0].Background.Matched), noticeOf(waiting[0]), viewed
}

// A watch's result is read under its call's contract alone: one that does not
// decode, or one under a contract this build does not know, is no lines, no
// notice text and no view, and the chat still loads.
func TestAWatchsResultIsReadUnderItsCallsContract(t *testing.T) {
	t.Parallel()

	result, err := json.Marshal(tools.WatchResult{Text: "1 line", Lines: []tools.LogsReadLine{{Message: "boom"}}, ViewAround: readWebapp().ViewAround})
	require.NoError(t, err)

	db := openTestDB(t, t.TempDir())
	set := prepareOn(t, db)
	chatID, id := seedWatch(t, db, string(logswatch.ContractName), string(result))
	matched, notice, viewed := watchReads(t, set, chatID, id)
	assert.Equal(t, 1, matched)
	assert.Equal(t, "1 line", notice.Result)
	assert.Equal(t, 1, notice.Matched)
	assert.True(t, viewed)

	for name, c := range map[string]struct{ contract, stored string }{
		"a result that does not decode": {string(logswatch.ContractName), `{"text":1}`},
		"a contract this build lacks":   {"logswatch/v999", string(result)},
	} {
		db := openTestDB(t, t.TempDir())
		set := prepareOn(t, db)
		chatID, id := seedWatch(t, db, c.contract, c.stored)
		matched, notice, viewed := watchReads(t, set, chatID, id)
		assert.Zero(t, matched, name)
		assert.Empty(t, notice.Result, name)
		assert.Zero(t, notice.Matched, name)
		assert.False(t, viewed, name)
	}
}

// A refused watch result logs its sentinel and none of the stored text.
func TestARefusedWatchResultLogsNoneOfTheRow(t *testing.T) {
	// CaptureLogs swaps the process's default logger, so this test stays serial.

	db := openTestDB(t, t.TempDir())
	set := prepareOn(t, db)
	chatID, _ := seedWatch(t, db, string(logswatch.ContractName), `{"text":"t","viewAround":{"resolvedAt":"MARKER-5f3a"}}`)
	logs := testutil.CaptureLogs(t)

	_, err := listMessages(t.Context(), set.Stmts(), chatID, testReaders)
	require.NoError(t, err)

	assert.Contains(t, logs.String(), tools.ErrDecode.Error())
	assert.Contains(t, logs.String(), `"result"`)
	assert.NotContains(t, logs.String(), "MARKER")
}

// readStoredWrite is a running Bash call with one sandbox action of status, its
// request stored as given, and answers the action as the run's read serves it.
func readStoredWrite(t *testing.T, status ApprovalStatus, stored string) SandboxAction {
	t.Helper()
	db := openTestDB(t, t.TempDir())
	st := prepareOn(t, db).Stmts()
	callID := seedLLMCall(t, db, st)
	row := sidecarRow(callID)
	require.NoError(t, upsertToolCall(t.Context(), st, row))
	var decided any
	if status != ApprovalPending {
		decided = int64(4_000)
	}
	_, err := db.Write.Exec(`INSERT INTO approvals (id, tool_call_id, kind, request, status, created_at, decided_at)
		VALUES (?, ?, 'action', ?, ?, 3000, ?)`, string(newApprovalID()), string(row.ID), stored, string(status), decided)
	require.NoError(t, err)

	var runID RunID
	require.NoError(t, db.Read.QueryRow(`SELECT run_id FROM llm_calls WHERE id = ?`, string(callID)).Scan(&runID))
	byRun, err := toolCallsByRun(t.Context(), st, runCallReads, string(runID))
	require.NoError(t, err)
	calls := toolCallsOf(t, ChatMessage{ToolCalls: marshalToolCalls(byRun[runID], testReaders)})
	require.Len(t, calls, 1)
	require.Len(t, calls[0].SandboxActions, 1)
	return calls[0].SandboxActions[0]
}

// Stored cluster writes read the same forever: never edit this table. Its shape is the gate's, not a tool's.
func TestClusterWritesReadOldRowsTheSame(t *testing.T) {
	t.Parallel()

	for stored, want := range map[string]SandboxAction{
		`{"action":{"principal":"","target":"kubernetes","accessLevel":"destructive","scope":"","accountName":"prod","service":"","operation":"delete","resource":"pods","name":"x","summary":"Delete pod web/x","dryRun":false,"confined":false,"once":false},"grantable":false,"commandRule":"","chatRule":"","write":{"method":"DELETE","path":"/api/v1/namespaces/web/pods/x","subresource":"","contentType":"","body":"","dryRun":false},"diff":"","diffCut":false,"diffError":""}`: {
			Write: &ClusterWrite{ClusterWrite: tools.ClusterWrite{Method: "DELETE", Path: "/api/v1/namespaces/web/pods/x"}},
			Action: &PermissionAction{Action: permissions.Action{
				Target: permissions.Kubernetes, AccessLevel: permissions.Destructive, AccountName: "prod", Operation: "delete", Resource: "pods", Name: "x", Summary: "Delete pod web/x",
			}},
		},
		`{"action":{"principal":"","target":"kubernetes","accessLevel":"write","scope":"","accountName":"prod","service":"apps","operation":"patch","resource":"deployments/scale","name":"api","summary":"Scale deployment web/api","dryRun":false,"confined":false,"once":false},"grantable":true,"commandRule":"patch deployments/scale in web","chatRule":"patch deployments/scale in web","write":{"method":"PATCH","path":"/apis/apps/v1/namespaces/web/deployments/api/scale","subresource":"scale","contentType":"application/merge-patch+json","body":"{\"spec\":{\"replicas\":3}}","dryRun":false},"diff":"-replicas: 1\n+replicas: 3\n","diffCut":false,"diffError":""}`: {
			Write: &ClusterWrite{
				ClusterWrite: tools.ClusterWrite{
					Method: "PATCH", Path: "/apis/apps/v1/namespaces/web/deployments/api/scale", Subresource: "scale",
					ContentType: "application/merge-patch+json", Body: `{"spec":{"replicas":3}}`,
				},
				Diff: "-replicas: 1\n+replicas: 3\n",
			},
			Action: &PermissionAction{
				Action: permissions.Action{
					Target: permissions.Kubernetes, AccessLevel: permissions.Write, AccountName: "prod", Service: "apps", Operation: "patch",
					Resource: "deployments/scale", Name: "api", Summary: "Scale deployment web/api",
				},
				Grantable: true, CommandRule: "patch deployments/scale in web", ChatRule: "patch deployments/scale in web",
			},
		},
	} {
		got := readStoredWrite(t, ApprovalPending, stored)
		want.Approval = got.Approval
		assert.Equal(t, want, got, stored)
	}
}

// A cluster write whose request does not decode keeps its row: its approval is
// served whole, with no action and no request, and the warning quotes none of
// it.
func TestAnUnreadableClusterWriteKeepsItsApproval(t *testing.T) {
	// CaptureLogs swaps the process's default logger, so this test stays serial.

	logs := testutil.CaptureLogs(t)
	got := readStoredWrite(t, ApprovalDenied, `{"action":{"summary":"MARKER-5f3a"`)

	assert.Nil(t, got.Action)
	assert.Nil(t, got.Request())
	assert.Equal(t, ApprovalDenied, got.Approval.Status)
	assert.Contains(t, logs.String(), tools.ErrDecode.Error())
	assert.Contains(t, logs.String(), `"request"`)
	assert.NotContains(t, logs.String(), "MARKER")
}

// aCloudRequest is a pending cloud platform request under call.
func aCloudRequest(call ToolCallID, millis int64) approval {
	return approval{
		ID: newApprovalID(), ToolCallID: call, Status: ApprovalPending, CreatedAt: time.UnixMilli(millis).UTC(),
		Request: &tools.ActionRequest{
			Action: permissions.Action{Target: permissions.AWS, AccessLevel: permissions.Write, Once: true, Summary: "AWS: call ec2:TerminateInstances"},
			Cloud: &tools.CloudRequest{
				Host: "ec2.amazonaws.com", Method: "POST", Path: "/", ContentType: "application/x-www-form-urlencoded",
				Body: "Action=TerminateInstances", Details: []tools.Detail{{Label: "Region", Value: "eu-west-1"}},
			},
		},
	}
}

// A cluster write, a cloud request and a cluster write under one call are
// served in that order, each with its own request.
func TestACallsActionsKeepTheirOrder(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	set := prepareOn(t, db)
	st := set.Stmts()
	ctx := t.Context()
	callID := seedLLMCall(t, db, st)
	row := sidecarRow(callID)
	first, cloud, last := aWrite(row.ID, 3_000), aCloudRequest(row.ID, 3_001), aWrite(row.ID, 3_002)
	row.SandboxActions = []*approval{&first, &cloud, &last}
	require.NoError(t, set.InTx(ctx, func(st stmts) error {
		require.NoError(t, upsertToolCall(ctx, st, row))
		for _, a := range row.SandboxActions {
			require.NoError(t, upsertApproval(ctx, st, *a))
		}
		return nil
	}))

	var runID RunID
	require.NoError(t, db.Read.QueryRow(`SELECT run_id FROM llm_calls WHERE id = ?`, string(callID)).Scan(&runID))
	byRun, err := toolCallsByRun(ctx, st, runCallReads, string(runID))
	require.NoError(t, err)
	calls := toolCallsOf(t, ChatMessage{ToolCalls: marshalToolCalls(byRun[runID], testReaders)})
	require.Len(t, calls, 1)
	got := calls[0].SandboxActions
	require.Len(t, got, 3)
	assert.Equal(t, first.ID, got[0].Approval.ID)
	assert.NotNil(t, got[0].Write)
	assert.Equal(t, cloud.ID, got[1].Approval.ID)
	assert.Nil(t, got[1].Write)
	require.NotNil(t, got[1].Cloud)
	assert.Equal(t, *cloud.Request.Cloud, got[1].Cloud.CloudRequest, "a waiting cloud request carries its body")
	assert.Equal(t, permissions.AWS, got[1].Action.Target)
	assert.Equal(t, got[1].Cloud, got[1].Request())
	assert.Equal(t, last.ID, got[2].Approval.ID)
}

// A decided cloud request is served with no body or media type; the record
// keeps them.
func TestADecidedCloudRequestCarriesNoBody(t *testing.T) {
	t.Parallel()

	stored := `{"action":{"target":"aws","accessLevel":"destructive","summary":"AWS: call ec2:TerminateInstances"},"cloud":{"host":"ec2.amazonaws.com","method":"POST","path":"/","contentType":"text/plain","body":"Action=TerminateInstances","details":[{"label":"Region","value":"eu-west-1"}]}}`
	got := readStoredWrite(t, ApprovalDenied, stored)
	require.NotNil(t, got.Cloud)
	assert.Equal(t, tools.CloudRequest{Host: "ec2.amazonaws.com", Method: "POST", Path: "/", Details: []tools.Detail{{Label: "Region", Value: "eu-west-1"}}}, got.Cloud.CloudRequest)
	assert.Equal(t, permissions.AWS, got.Action.Target)
}

// A cloud request this build cannot read keeps its approval, with no action
// and no request, so it offers Deny alone.
func TestAnUnreadableCloudRequestKeepsItsApproval(t *testing.T) {
	t.Parallel()

	got := readStoredWrite(t, ApprovalPending, `{"action":{"target":"aws"},"cloud":{"host":`)
	assert.Nil(t, got.Action)
	assert.Nil(t, got.Request())
	assert.Equal(t, ApprovalPending, got.Approval.Status)
}
