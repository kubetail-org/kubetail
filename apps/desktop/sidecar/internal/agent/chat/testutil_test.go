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
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/rawjson"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/toolsettings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/anthropicwebsearch"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/bash"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/logsread"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/logsview"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/logswatch"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/read"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/taskstop"
)

// update rewrites the golden files in testdata; `make generate-desktop-prompts` passes it.
var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// openTestDB is an app.db of this test's own, with no janitor, holding the cluster
// rows the tests file chats under. dir is the data directory, so a test that reopens
// the file can name it — and finds the rows it seeded.
func openTestDB(t *testing.T, dir string) *appdb.DB {
	t.Helper()
	db := appdbtest.OpenIn(t, dir)
	seedClusters(t, db, testClusterIDs...)
	return db
}

// testClusterIDs are the clusters the tests file chats under: chat.cluster_id
// references clusters, so each needs a row. Raw SQL, since this package owns no
// cluster statement.
var testClusterIDs = []apimeta.ClusterID{"1", "2", "7", "8", "9", "42"}

func seedClusters(t *testing.T, db *appdb.DB, ids ...apimeta.ClusterID) {
	t.Helper()
	for _, id := range ids {
		_, err := db.Write.Exec(`INSERT INTO clusters (id, source, source_key, created_at, updated_at)
		VALUES (?, 'kubeconfig', ?, 0, 0) ON CONFLICT DO NOTHING`, string(id), "ctx-"+string(id))
		require.NoError(t, err)
	}
}

// markCluster marks a seeded cluster for deletion, the way the cluster service does.
func markCluster(t *testing.T, db *appdb.DB, id apimeta.ClusterID) {
	t.Helper()
	_, err := db.Write.Exec(`UPDATE clusters SET delete_requested_at = 1 WHERE id = ?`, string(id))
	require.NoError(t, err)
}

// admitted admits every monitor run; refused admits none.
func admitted(context.Context, *sql.Tx, RunID) (bool, error) { return true, nil }

func refused(context.Context, *sql.Tx, RunID) (bool, error) { return false, nil }

// newTestSet is the statement set prepared on an app.db of this test's own.
func newTestSet(t *testing.T) *sqlstmt.Set[stmtID] {
	t.Helper()
	return prepareOn(t, openTestDB(t, t.TempDir()))
}

// prepareOn is the statement set over db, for a test that also reads db directly.
func prepareOn(t *testing.T, db *appdb.DB) *sqlstmt.Set[stmtID] {
	t.Helper()
	return prepareTable(t, db, statements)
}

// prepareTable is prepareOn over a table of the test's own, from swapped.
func prepareTable(t *testing.T, db *appdb.DB, table []sqlstmt.Statement) *sqlstmt.Set[stmtID] {
	t.Helper()
	s, err := sqlstmt.Prepare[stmtID](t.Context(), db.Write.DB, db.Read.DB, table)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// swapped is a copy of statements with each id in swaps given that text, on the
// pool the original names. A test that swaps a statement prepares its own copy:
// the package's table is read by every test running beside it.
func swapped(swaps map[stmtID]string) []sqlstmt.Statement {
	table := slices.Clone(statements)
	for id, text := range swaps {
		table[id].Text = text
	}
	return table
}

// The rows a send writes, seeded by hand, for the tests of what reads and removes
// them.

// aChat is a chat row to seed, filed under clusterID at now.
func aChat(clusterID apimeta.ClusterID, at time.Time) Chat {
	return Chat{ID: ChatID(appdb.NewID()), Title: "t", Mode: ModeChat, ClusterID: clusterID, CreatedAt: at, UpdatedAt: at}
}

// seedChat writes c's row.
func seedChat(t *testing.T, db *appdb.DB, c Chat) Chat {
	t.Helper()
	_, err := db.Write.Exec(`INSERT INTO chats (id, title, mode, cluster_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		string(c.ID), c.Title, string(c.Mode), string(c.ClusterID), millis(c.CreatedAt), millis(c.UpdatedAt))
	require.NoError(t, err)
	return c
}

// seededTurn is what seedTurn wrote: the question, its queued run and the empty answer.
type seededTurn struct {
	User, Assistant MessageID
	Run             RunID
}

// seedTurn files one turn under chatID the way a send does — the question, its
// queued run and the empty answer — at the next two seqs.
func seedTurn(t *testing.T, db *appdb.DB, chatID ChatID, at time.Time) seededTurn {
	t.Helper()
	var seq int64
	require.NoError(t, db.Read.QueryRow(`SELECT COALESCE(MAX(seq), -1) + 1 FROM messages WHERE chat_id = ?`, string(chatID)).Scan(&seq))
	turn := seededTurn{User: MessageID(appdb.NewID()), Assistant: MessageID(appdb.NewID()), Run: RunID(appdb.NewID())}
	_, err := db.Write.Exec(`INSERT INTO messages (id, chat_id, seq, role, content, request_key, created_at) VALUES (?, ?, ?, 'user', ?, ?, ?)`,
		string(turn.User), string(chatID), seq, `[{"type":"text","text":"hi"}]`, appdb.NewID(), millis(at))
	require.NoError(t, err)
	_, err = db.Write.Exec(`INSERT INTO agent_runs (id, agent_type, app_version, trigger, chat_id, trigger_message_id, provider, model, dialect, effort, created_at)
		VALUES (?, 'chat', 'test', 'chat', ?, ?, 'fake', 'fake', 'fake', 'high', ?)`,
		string(turn.Run), string(chatID), string(turn.User), millis(at))
	require.NoError(t, err)
	_, err = db.Write.Exec(`INSERT INTO messages (id, chat_id, seq, role, content, run_id, created_at) VALUES (?, ?, ?, 'assistant', '[]', ?, ?)`,
		string(turn.Assistant), string(chatID), seq+1, string(turn.Run), millis(at))
	require.NoError(t, err)
	return turn
}

// setRunStatus moves a seeded run, the way a turn would.
func setRunStatus(t *testing.T, db *appdb.DB, id RunID, status RunStatus) {
	t.Helper()
	_, err := db.Write.Exec(`UPDATE agent_runs SET status = ? WHERE id = ?`, string(status), string(id))
	require.NoError(t, err)
}

// settleSeededRun settles a seeded run at finishedAt.
func settleSeededRun(t *testing.T, db *appdb.DB, id RunID, status RunStatus, finishedAt time.Time) {
	t.Helper()
	_, err := db.Write.Exec(`UPDATE agent_runs SET status = ?, finished_at = ? WHERE id = ?`, string(status), millis(finishedAt), string(id))
	require.NoError(t, err)
}

// runStatusOf reads a run's stored status.
func runStatusOf(t *testing.T, db *appdb.DB, id RunID) RunStatus {
	t.Helper()
	var status RunStatus
	require.NoError(t, db.Read.QueryRow(`SELECT status FROM agent_runs WHERE id = ?`, string(id)).Scan(&status))
	return status
}

// tableCount is how many rows a table holds.
func tableCount(t *testing.T, db *appdb.DB, table string) int {
	t.Helper()
	var n int
	require.NoError(t, db.Read.QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&n))
	return n
}

// chatsDirIn is the chats' directory under dir.
func chatsDirIn(dir string) string { return filepath.Join(dir, "chats") }

// monitorDirIn is the monitor's directory under dir.
func monitorDirIn(dir string) string { return filepath.Join(dir, "monitor") }

// startService opens a service over dir and starts it, on the fake and no card:
// the stub's empty card is what a fresh chat holds, so no question carries a
// context block.
func startService(t *testing.T, dir string) *service {
	t.Helper()
	return startServiceWith(t, dir, fakeLLM(), &stubClusterCards{}, testReaders, noLists)
}

// startServiceWith is startService over the given llm service, cluster cards,
// tool box and lists.
func startServiceWith(t *testing.T, dir string, llmSvc *llm.Service, clusterCards ClusterCards, box tools.Box, lists ToolLists) *service {
	t.Helper()
	s, err := newService(openTestDB(t, dir), chatsDirIn(dir), monitorDirIn(dir), llmSvc, clusterCards, nil, box, lists, sandbox.State{}, testSettings(t), nil)
	require.NoError(t, err)
	startPrepared(t, s)
	return s
}

// startServiceOn is a started service over a fresh data dir whose statements are
// prepared from table.
func startServiceOn(t *testing.T, table []sqlstmt.Statement) *service {
	t.Helper()
	dir := t.TempDir()
	db := openTestDB(t, dir)
	s, err := newService(db, chatsDirIn(dir), monitorDirIn(dir), fakeLLM(), &stubClusterCards{}, nil, testReaders, noLists, sandbox.State{}, testSettings(t), nil)
	require.NoError(t, err)
	require.NoError(t, s.store.Close())
	s.store = prepareTable(t, db, table)
	startPrepared(t, s)
	return s
}

// testSearch is the search the chat tests offer, its prompt naming March 2026.
var testSearch = anthropicwebsearch.New(func() time.Time { return time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC) })

// ourReaders reads the calls of bash, Read and the log tools, as production does.
var ourReaders = []tools.Reader{bash.Reader{}, &read.Tool{}, &logsview.Tool{}, &logsread.Tool{}, &logswatch.Tool{}}

// testReaders offers nothing and reads calls the way production does: ours,
// and the search's.
var testReaders = tools.NewBox(nil, append(slices.Clone(ourReaders), testSearch)...)

// testBox offers the tools given, then the search, and reads ourReaders' calls
// where it does not offer them. The lists name every tool it offers.
func testBox(offered ...tools.Tool) (tools.Box, stubLists) {
	all := append(slices.Clone(offered), tools.Tool(testSearch))
	var readers []tools.Reader
	var names stubLists
	for _, t := range all {
		names = append(names, t.Name())
	}
	for _, r := range ourReaders {
		if !slices.Contains(names, r.Name()) {
			readers = append(readers, r)
		}
	}
	return tools.NewBox(all, readers...), names
}

// stubLists lists its names for every target.
type stubLists []string

func (l stubLists) ToolsFor(llm.Target) []string { return l }

// AlwaysOn holds TaskStop on, as the catalog does.
func (stubLists) AlwaysOn(name string) bool { return name == taskstop.Name }

// noLists offers no turn a tool.
var noLists = stubLists(nil)

// noClusterCards is the card source of a test that never sends: no card at all.
var noClusterCards = &stubClusterCards{}

// stubClusterCards answers a fixed cluster card and remembers which cluster it was
// asked for.
type stubClusterCards struct {
	mu    sync.Mutex
	card  string
	asked []apimeta.ClusterID
}

func (c *stubClusterCards) ClusterCard(_ context.Context, clusterID apimeta.ClusterID) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked = append(c.asked, clusterID)
	return c.card
}

func (c *stubClusterCards) set(card string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.card = card
}

func (c *stubClusterCards) askedFor() []apimeta.ClusterID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.asked)
}

// startPrepared starts a service a test built, waits out its startup sweep, and
// stops it at cleanup. The sweep runs on the sweeper's goroutine, so waiting
// keeps it from racing an entry the test makes under the chats' directory.
func startPrepared(t *testing.T, s *service) {
	t.Helper()
	s.sweepRetry = time.Millisecond
	s.writeBackoff = time.Millisecond
	s.onSwept = make(chan struct{}, 64)

	stop, err := s.Start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		// t.Context() is already cancelled by now, and a drain on a dead context
		// reports drained while the work is still running.
		ctx, cancel := context.WithTimeout(context.Background(), testutil.Timeout)
		defer cancel()
		require.NoError(t, stop(ctx))
		require.NoError(t, s.Close())
	})
	testutil.Wait(t, s.onSwept, "the startup sweep")
}

// newTestService is a started service over a fresh data dir.
func newTestService(t *testing.T) *service {
	t.Helper()
	return startService(t, t.TempDir())
}

// awaitFrame reads frames until one satisfies want.
func awaitFrame[T any](t *testing.T, frames <-chan T, want func(T) bool) T {
	t.Helper()
	for {
		f := testutil.Recv(t, frames, "a frame")
		if want(f) {
			return f
		}
	}
}

// collectSnapshot reads a messages watch up to its Bookmark.
func collectSnapshot(t *testing.T, frames <-chan ChatMessageCollectionWatchFrame) []ChatMessage {
	t.Helper()
	var out []ChatMessage
	for {
		f := testutil.Recv(t, frames, "a snapshot frame")
		if f.Type == CollectionWatchFrameBookmark {
			return out
		}
		require.Equal(t, CollectionWatchFrameAdded, f.Type)
		out = append(out, *f.Message)
	}
}

// collectChatSnapshot reads a list watch up to its Bookmark.
func collectChatSnapshot(t *testing.T, frames <-chan ChatCollectionWatchFrame) []Chat {
	t.Helper()
	var out []Chat
	for {
		f := testutil.Recv(t, frames, "a snapshot frame")
		if f.Type == CollectionWatchFrameBookmark {
			return out
		}
		require.Equal(t, CollectionWatchFrameAdded, f.Type)
		out = append(out, *f.Chat)
	}
}

// fakeLLM is an llm service holding the fake alone, as provider "fake", unpaced.
func fakeLLM() *llm.Service {
	return llm.New(llm.FakeProvider(llm.NewFake(0)))
}

// fakeOf is the fake a test service answers on.
func fakeOf(s *service) *llm.Fake {
	p, _ := s.llmSvc.Provider("fake")
	return p.Fake()
}

// fakeTarget is what a turn on the fake is aimed at.
func fakeTarget(s *service) llm.Target {
	target, err := s.llmSvc.Resolve("fake", "fake", "low")
	if err != nil {
		panic(err)
	}
	return target
}

// reqID is a request key derived from a name, so a test can spell the same key twice.
func reqID(name string) string {
	sum := sha256.Sum256([]byte(name))
	return uuid.Must(uuid.NewRandomFromReader(bytes.NewReader(sum[:]))).String()
}

// send is a create-shaped send under cluster "1" on the fake's names.
func send(t *testing.T, s *service, chatID *ChatID, key, text string) ChatMessage {
	t.Helper()
	msg, err := s.Send(t.Context(), chatID, ModeChat, "1", false, false, false, "fake", "fake", "high", reqID(key), text, nil)
	require.NoError(t, err)
	return msg
}

// awaitSettled watches chatID until message id is no longer in flight, then waits
// for the turn answering it to end, and returns it as the watch delivered it. The
// settled frame follows the release, but the turn goes on to kick the chat: a kick
// that lands after the test files a notice starts a notice turn, which holds the
// slot against the test's next send.
func awaitSettled(t *testing.T, s *service, chatID ChatID, id MessageID) ChatMessage {
	t.Helper()
	turn := answeringTurn(s, chatID, id)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	st, err := s.WatchMessageCollection(ctx, chatID)
	require.NoError(t, err)
	f := awaitFrame(t, st.Frames, func(f ChatMessageCollectionWatchFrame) bool {
		return f.Message != nil && f.Message.ID == id && !f.Message.Status.inFlight()
	})
	if turn != nil {
		awaitTurnDone(t, turn)
	}
	return *f.Message
}

// answeringTurn is the turn writing message id in chatID, or nil once it has
// released the slot.
func answeringTurn(s *service, chatID ChatID, id MessageID) *turn {
	s.turnsMu.Lock()
	defer s.turnsMu.Unlock()
	if turn := s.turns[chatID]; turn != nil && turn.msg.ID == id {
		return turn
	}
	return nil
}

// The fake's first reply: the thought it streams first, the sentence it ends on,
// and the first chunk of either, which is the thought's first word.
const (
	fakeThought   = "Let me count the pods in that namespace."
	fakeSentence  = "Twelve pods are running in the default namespace."
	fakeFirstWord = "Let "
)

// awaitLiveContent watches chatID until its last message carries content of any
// kind: what its callers wait for is the overlay's first write, which the fake
// makes inside its thought.
func awaitLiveContent(t *testing.T, s *service, chatID ChatID) ChatMessage {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	st, err := s.WatchMessageCollection(ctx, chatID)
	require.NoError(t, err)
	f := awaitFrame(t, st.Frames, func(f ChatMessageCollectionWatchFrame) bool {
		return f.Message != nil && f.Message.Role == RoleAssistant && f.Message.Content != emptyContent
	})
	return *f.Message
}

// awaitTurnReleased waits for the chat's slot to be free: the turn's rows are
// written and its overlay gone. Its goroutine may still be kicking the chat, so
// a test that changes the service afterwards waits on awaitTurnDone instead.
func awaitTurnReleased(t *testing.T, s *service, chatID ChatID) {
	t.Helper()
	if turn := s.turnOf(chatID); turn != nil {
		testutil.Wait(t, turn.done, "the turn to end")
	}
}

// heldTurn is chatID's turn, taken at a moment the caller knows it holds its slot.
func heldTurn(t *testing.T, s *service, chatID ChatID) *turn {
	t.Helper()
	turn := s.turnOf(chatID)
	require.NotNil(t, turn, "the turn holds its slot")
	return turn
}

// awaitTurnDone waits for the turn's goroutine to return, its kick included.
func awaitTurnDone(t *testing.T, turn *turn) {
	t.Helper()
	testutil.Wait(t, turn.done, "the turn to end")
}

// searchCall is a search as the stream shows it: its call, with no payload.
func searchCall(id, query string) llm.Block {
	return llm.ServerUseBlock(id, anthropicwebsearch.Name, json.RawMessage(`{"query":"`+query+`"}`))
}

// blocksOf is a served message's content as blocks.
func blocksOf(t *testing.T, msg ChatMessage) []llm.Block {
	t.Helper()
	blocks, err := unmarshalBlocks(msg.Content)
	require.NoError(t, err)
	return blocks
}

// llmCallRow is what a run's call row stored. serverUses is the column's JSON,
// "" for NULL.
type llmCallRow struct {
	stopReason, err, serverUses string
	finished                    bool
}

// llmCallRowColumns is what scanLLMCallRow reads, in order.
const llmCallRowColumns = `stop_reason, error, server_uses, finished_at`

func scanLLMCallRow(t *testing.T, s scanner) llmCallRow {
	t.Helper()
	var (
		reason, errText, uses sql.NullString
		finished              sql.NullInt64
	)
	require.NoError(t, s.Scan(&reason, &errText, &uses, &finished))
	return llmCallRow{stopReason: reason.String, err: errText.String, serverUses: uses.String, finished: finished.Valid}
}

func llmCallOf(t *testing.T, db *appdb.DB, run RunID) llmCallRow {
	t.Helper()
	return scanLLMCallRow(t, db.Read.QueryRow(`SELECT `+llmCallRowColumns+` FROM llm_calls WHERE run_id = ?`, string(run)))
}

// awaitContent watches the chat until the message's content is want, and is that
// message as the watch served it.
func awaitContent(t *testing.T, s *service, chatID ChatID, id MessageID, want rawjson.RawJSON) ChatMessage {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	st, err := s.WatchMessageCollection(ctx, chatID)
	require.NoError(t, err)
	f := awaitFrame(t, st.Frames, func(f ChatMessageCollectionWatchFrame) bool {
		return f.Message != nil && f.Message.ID == id && f.Message.Content == want
	})
	return *f.Message
}

// The fake's second reply, which a two-round turn takes: its thought, the sentence
// it ends on, and the thought's first word, where a gate parks it.
const (
	fakeSecondThought   = "I should check every replica's readiness."
	fakeSecondSentence  = "The deployment rolled out cleanly; every replica is ready."
	fakeSecondFirstWord = "I "
)

// testTool is the tool the chat tests offer: it answers with what it was asked.
// run, when set, answers in its place, and is the one point between the turn's two
// rounds a test controls — where a case arms the gate for the second reply, fires
// a cancel, or reads its own row.
type testTool struct {
	name string
	run  func(ctx context.Context, input json.RawMessage) (string, bool)
}

func (e testTool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{Name: e.name, Description: "says it back", InputSchema: json.RawMessage(`{"type":"object"}`)}
}

func (e testTool) Prompt() string { return "## " + e.name + "\n\nSays back what it is given." }

func (e testTool) Name() string { return e.name }
func (e testTool) ContractName() tools.ContractName {
	return tools.ContractName(strings.ToLower(e.name) + "/v1")
}
func (e testTool) ActionKind() tools.ActionKind { return tools.ActionCommand }
func (e testTool) Action(input json.RawMessage, _ string, _ bool) (tools.Action, error) {
	return tools.Action{Command: &tools.CommandAction{Text: string(input)}}, nil
}

func (e testTool) Run(ctx context.Context, _ tools.Runtime, input json.RawMessage) (string, bool) {
	if e.run != nil {
		return e.run(ctx, input)
	}
	return string(input), false
}

// shownTool is a testTool whose run resolves its action: the input as a command's
// text, under what it answered, and none on a refusal.
type shownTool struct{ testTool }

func (s shownTool) RunShown(ctx context.Context, rt tools.Runtime, input json.RawMessage) (string, bool, *tools.Action) {
	text, isError := s.testTool.Run(ctx, rt, input)
	if isError {
		return text, true, nil
	}
	return text, false, &tools.Action{Description: text, Command: &tools.CommandAction{Text: string(input)}}
}

// startServiceWithTool is a started service offering the tools given.
func startServiceWithTool(t *testing.T, offered ...tools.Tool) *service {
	t.Helper()
	box, lists := testBox(offered...)
	return startServiceWith(t, t.TempDir(), fakeLLM(), &stubClusterCards{}, box, lists)
}

// storedToolCall is what one tool_calls row stored.
type storedToolCall struct {
	seq                     int
	name, useID, args       string
	result, errText, status string
	hasStarted, hasFinished bool
	llmCallSeq              int
	// provider is a row the provider ran, and contract its contract_name.
	provider bool
	contract string
}

// toolCallRows is every tool call of a run, in the order the model asked them.
func toolCallRows(t *testing.T, db *appdb.DB, run RunID) []storedToolCall {
	t.Helper()
	rows, err := db.Read.Query(`SELECT t.seq, t.tool_name, t.tool_use_id, t.arguments, t.result, t.error, t.status,
		t.started_at, t.finished_at, c.seq, t.runs_on, t.contract_name
		FROM tool_calls t JOIN llm_calls c ON c.id = t.llm_call_id
		WHERE c.run_id = ? ORDER BY c.seq, t.seq`, string(run))
	require.NoError(t, err)
	defer rows.Close()

	var out []storedToolCall
	for rows.Next() {
		var (
			r                            storedToolCall
			useID, args, result, errText sql.NullString
			status                       sql.NullString
			started, finished            sql.NullInt64
			runsOn                       string
			contract                     sql.NullString
		)
		require.NoError(t, rows.Scan(&r.seq, &r.name, &useID, &args, &result, &errText, &status, &started, &finished, &r.llmCallSeq, &runsOn, &contract))
		r.contract = contract.String
		r.useID, r.args, r.result, r.errText, r.status = useID.String, args.String, result.String, errText.String, status.String
		r.hasStarted, r.hasFinished, r.provider = started.Valid, finished.Valid, runsOn == runsOnProvider
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

// callSpan is what a model call's row names and when it ran, in Unix millis;
// NULL reads 0.
type callSpan struct {
	provider, model, effort       string
	started, firstChunk, finished int64
}

// callSpansOf is every model call of a run, in seq order.
func callSpansOf(t *testing.T, db *appdb.DB, run RunID) []callSpan {
	t.Helper()
	rows, err := db.Read.Query(`SELECT provider, model, COALESCE(effort, ''), started_at,
		COALESCE(first_chunk_at, 0), COALESCE(finished_at, 0) FROM llm_calls WHERE run_id = ? ORDER BY seq`, string(run))
	require.NoError(t, err)
	defer rows.Close()

	var out []callSpan
	for rows.Next() {
		var c callSpan
		require.NoError(t, rows.Scan(&c.provider, &c.model, &c.effort, &c.started, &c.firstChunk, &c.finished))
		out = append(out, c)
	}
	require.NoError(t, rows.Err())
	return out
}

// stepClock makes every read of the service's clock a millisecond after the
// last, so two moments a test compares are never equal by accident.
func stepClock(s *service) {
	var mu sync.Mutex
	at := time.UnixMilli(1_000_000).UTC()
	s.now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		at = at.Add(time.Millisecond)
		return at
	}
}

// settleSeam puts each settle attempt in the test's hands: an attempt waits for
// the test's answer, fails with a non-nil one and writes with nil. A send on the
// channel returns once an attempt has taken it. The service's stop ends a wait.
func settleSeam(s *service) chan<- error {
	answers := make(chan error)
	write := s.settleWrite
	s.writeBackoff = time.Millisecond
	s.settleWrite = func(ctx context.Context, t *turn) error {
		select {
		case err := <-answers:
			if err != nil {
				return err
			}
			return write(ctx, t)
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
	}
	return answers
}

// errRefused is a settle attempt the test fails.
var errRefused = errors.New("refused")

// llmCallRows is every model call of a run, in seq order.
func llmCallRows(t *testing.T, db *appdb.DB, run RunID) []llmCallRow {
	t.Helper()
	rows, err := db.Read.Query(`SELECT `+llmCallRowColumns+` FROM llm_calls WHERE run_id = ? ORDER BY seq`, string(run))
	require.NoError(t, err)
	defer rows.Close()

	var out []llmCallRow
	for rows.Next() {
		out = append(out, scanLLMCallRow(t, rows))
	}
	require.NoError(t, rows.Err())
	return out
}

// fakeBash is the real bash offer and approval over a Run of the test's own, so
// the chat tests read the input exactly as production does. run, when set, answers a
// command in the fake's place.
type fakeBash struct {
	bash.Tool
	mu  sync.Mutex
	ran []string // the commands Run was given, in order
	run func(ctx context.Context, command string) (string, bool)
}

func (b *fakeBash) Run(ctx context.Context, _ tools.Runtime, input json.RawMessage) (string, bool) {
	action, err := bash.ActionOf(input, "", false)
	if err != nil {
		return `{"error":"bad-input"}`, true
	}
	command := action.Command.Text
	b.mu.Lock()
	b.ran = append(b.ran, command)
	run := b.run
	b.mu.Unlock()
	if run != nil {
		return run(ctx, command)
	}
	return "ran: " + command, false
}

// RunApproved is Run, since the loop runs a call by its approval: the
// embedded tool's would run the real shell.
func (b *fakeBash) RunApproved(ctx context.Context, rt tools.Runtime, input json.RawMessage, _ tools.Approval) (string, bool) {
	return b.Run(ctx, rt, input)
}

// commands is what Run was given, in order.
func (b *fakeBash) commands() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.ran)
}

// bashCall is a staged call of the bash tool running command.
func bashCall(command string) llm.Block {
	b, _ := json.Marshal(map[string]string{"command": command})
	return llm.StagedCall("Bash", string(b))
}

// describedBashCall is bashCall with the model's description of the command.
func describedBashCall(command, description string) llm.Block {
	b, _ := json.Marshal(map[string]string{"command": command, "description": description})
	return llm.StagedCall("Bash", string(b))
}

// offered is the names a request offered, in order.
func offered(req llm.Request) []string {
	var out []string
	for _, d := range req.Tools {
		out = append(out, d.Name)
	}
	return out
}

// grantable makes s a machine with a sandbox whose home is a fresh folder
// holding a never-readable .ssh and a code/svc, its settings over s's app.db,
// and answers the home.
func grantable(t *testing.T, s *service) string {
	t.Helper()
	home := testutil.GrantableDir(t)
	for _, d := range []string{".ssh", "code/svc"} {
		require.NoError(t, os.MkdirAll(filepath.Join(home, d), 0o755))
	}
	s.settings = settings.NewService(settingsStore(t, s.db), func() settings.Zones {
		return settings.Zones{Never: []string{filepath.Join(home, ".ssh")}, Home: home, NoWrite: sandbox.NoWrite(home)}
	}, nil, nil, "")
	s.sandboxState = sandbox.State{Available: true}
	return home
}

// testSettings is a settings service over a file not yet written and an
// app.db of its own, holding the test clusters: every default.
func testSettings(t *testing.T) *settings.Service {
	t.Helper()
	db := appdbtest.Open(t)
	seedClusters(t, db, testClusterIDs...)
	return settings.NewService(settingsStore(t, db), nil, nil, nil, "")
}

// settingsStore is a settings store over a file not yet written and db.
func settingsStore(t *testing.T, db *appdb.DB) *settings.Store {
	t.Helper()
	store, err := settings.Open(db)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store
}

// setToolEnabled turns the tool named name on or off, as Settings does.
func setToolEnabled(t *testing.T, s *service, name string, enabled bool) {
	t.Helper()
	settings, err := toolsettings.New(s.db, []toolsettings.Listing{{Name: name}}, time.Now)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settings.Close()) })
	require.NoError(t, settings.Set(t.Context(), name, enabled))
}
