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
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/rawjson"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/rootdir"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// fakeTask is a background process a test ends by hand. A stop ends it as a
// signal would, with the code a shell reports: 137 now, 143 with a grace.
type fakeTask struct {
	out   *os.File
	exits chan tools.Exit
	once  sync.Once

	mu    sync.Mutex
	stops []bool // each Stop's now, in order
	deaf  bool   // a stop is recorded and ends nothing: the process exits on its own
	// onStop, when set, runs as each Stop begins.
	onStop func()
}

func newFakeTask(out *os.File) *fakeTask {
	return &fakeTask{out: out, exits: make(chan tools.Exit, 1)}
}

func (f *fakeTask) Wait() tools.Exit { return <-f.exits }

func (f *fakeTask) Stop(now bool) {
	f.mu.Lock()
	f.stops = append(f.stops, now)
	deaf, onStop := f.deaf, f.onStop
	f.mu.Unlock()
	if onStop != nil {
		onStop()
	}
	if deaf {
		return
	}
	code := 143
	if now {
		code = 137
	}
	f.end(tools.Exit{Code: code, OK: true, Stopped: true})
}

// exit ends the task on its own with code, as a command exiting would.
func (f *fakeTask) exit(code int) { f.end(tools.Exit{Code: code, OK: true}) }

func (f *fakeTask) end(e tools.Exit) { f.once.Do(func() { f.exits <- e }) }

// stopsSeen is each Stop's now, in order.
func (f *fakeTask) stopsSeen() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]bool(nil), f.stops...)
}

// taskTool is a tool that starts a fakeTask as the chat's task and answers with
// what Tasks.Start said, the way bash's background call does. write, when set,
// is what the task writes to its file before it runs.
type taskTool struct {
	write string
	fail  error // what start returns in place of a task
	// closeOut closes the task's file before the process starts, so its end
	// line cannot be written.
	closeOut bool
	// around, when set, runs the start inside it, for a test that reads what the
	// start alone changed.
	around func(start func())
	// holding, when set, receives once the task's row is written, and the start
	// then waits for hold to close before the process exists.
	holding, hold chan struct{}

	mu      sync.Mutex
	started []*fakeTask
	ready   chan *fakeTask
}

func newTaskTool() *taskTool { return &taskTool{ready: make(chan *fakeTask, 32)} }

func (e *taskTool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{Name: "Task", Description: "starts a task", InputSchema: json.RawMessage(`{"type":"object"}`)}
}

func (e *taskTool) Prompt() string { return "## Task\n\nStarts a task." }

func (e *taskTool) Name() string                     { return "Task" }
func (e *taskTool) ContractName() tools.ContractName { return "task/v1" }
func (e *taskTool) ActionKind() tools.ActionKind     { return tools.ActionCommand }
func (e *taskTool) Action(json.RawMessage, string, bool) (tools.Action, error) {
	return tools.Action{Command: &tools.CommandAction{}}, nil
}

func (e *taskTool) Run(_ context.Context, rt tools.Runtime, _ json.RawMessage) (string, bool) {
	var (
		id, path string
		err      error
	)
	around := e.around
	if around == nil {
		around = func(start func()) { start() }
	}
	around(func() { id, path, err = e.startOne(rt.Tasks) })
	if err != nil {
		return "refused: " + err.Error(), true
	}
	return "started " + id + " at " + path, false
}

func (e *taskTool) startOne(tasks tools.Tasks) (string, string, error) {
	return tasks.Start(func(out *os.File) (tools.Task, error) {
		if e.holding != nil {
			e.holding <- struct{}{}
			<-e.hold
		}
		if e.fail != nil {
			return nil, e.fail
		}
		if e.write != "" {
			if _, err := out.WriteString(e.write); err != nil {
				return nil, err
			}
		}
		if e.closeOut {
			_ = out.Close()
		}
		ft := newFakeTask(out)
		e.mu.Lock()
		e.started = append(e.started, ft)
		e.mu.Unlock()
		e.ready <- ft
		return ft, nil
	})
}

// taskCall is a staged call of the task tool.
func taskCall() llm.Block { return llm.StagedCall("Task", `{}`) }

// startTaskTurn sends a question whose answer starts one task, and returns the
// answer once its turn has ended, kick included, with the task. The turn kicks
// after it settles, so a test that waited only for the settle could have a task's
// end start a notice turn from that kick, behind the task's own.
func startTaskTurn(t *testing.T, s *service, tt *taskTool, chatID *ChatID, key string) (ChatMessage, *fakeTask) {
	t.Helper()
	// The turn is taken while the task's start holds it.
	tt.holding, tt.hold = make(chan struct{}), make(chan struct{})
	fakeOf(s).SetToolCalls(taskCall())
	msg := send(t, s, chatID, key, "start it")
	testutil.Wait(t, tt.holding, "the task's start")
	turn := heldTurn(t, s, msg.ChatID)
	close(tt.hold)
	ft := testutil.Recv(t, tt.ready, "the task to start")
	tt.holding, tt.hold = nil, nil
	awaitTurnDone(t, turn)
	return awaitSettled(t, s, msg.ChatID, msg.ID), ft
}

// taskRow is what one background_tasks row stored.
type taskRow struct {
	id, chatID, toolCallID, path, status string
	stoppedBy                            sql.NullString
	exitCode                             sql.NullInt64
	finished, notified                   bool
}

func taskRows(t *testing.T, db *appdb.DB) []taskRow {
	t.Helper()
	rows, err := db.Read.Query(`SELECT id, chat_id, tool_call_id, output_path, status, stopped_by, exit_code,
		finished_at, notified_at FROM background_tasks ORDER BY started_at, id`)
	require.NoError(t, err)
	defer rows.Close()
	var out []taskRow
	for rows.Next() {
		var (
			r                  taskRow
			finished, notified sql.NullInt64
		)
		require.NoError(t, rows.Scan(&r.id, &r.chatID, &r.toolCallID, &r.path, &r.status, &r.stoppedBy, &r.exitCode, &finished, &notified))
		r.finished, r.notified = finished.Valid, notified.Valid
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

// toolCallIDOf is the id of the run's one tool call.
func toolCallIDOf(t *testing.T, db *appdb.DB, run RunID) string {
	t.Helper()
	var id string
	require.NoError(t, db.Read.QueryRow(`SELECT t.id FROM tool_calls t JOIN llm_calls c ON c.id = t.llm_call_id
		WHERE c.run_id = ?`, string(run)).Scan(&id))
	return id
}

// A started task is a running row under the call that started it, with its
// output file in the chat's directory, and the call answers while the
// process runs on.
func TestStartingATaskRecordsItAndAnswersAtOnce(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	msg, ft := startTaskTurn(t, s, tt, nil, "1")

	rows := taskRows(t, s.db)
	require.Len(t, rows, 1)
	r := rows[0]
	assert.Equal(t, "running", r.status)
	assert.Equal(t, string(msg.ChatID), r.chatID)
	assert.Equal(t, toolCallIDOf(t, s.db, msg.RunID), r.toolCallID)
	want := filepath.Join(s.chatsRoot.Name(), string(msg.ChatID), "tasks", r.id+".output")
	assert.Equal(t, want, r.path)
	assert.Equal(t, "started "+r.id+" at "+want, toolCallsOf(t, msg)[0].Output)
	assert.FileExists(t, want)
	assert.Equal(t, ft.out.Name(), want)
	select {
	case <-ft.exits:
		t.Fatal("nothing waited the task out")
	default:
	}
}

// sendTask sends a question whose answer asks for one task, and returns the
// answer once settled.
func sendTask(t *testing.T, s *service, chatID *ChatID, key string) ChatMessage {
	t.Helper()
	fakeOf(s).SetToolCalls(taskCall())
	msg := send(t, s, chatID, key, "start it")
	return awaitSettled(t, s, msg.ChatID, msg.ID)
}

// taskFiles is what the chat's tasks directory holds.
func taskFiles(t *testing.T, s *service, chatID ChatID) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(s.chatDir(chatID).Path(), taskDirName))
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// A chat runs four tasks at once: the fifth is refused with the chat's limit,
// and nothing of it is written.
func TestAFifthTaskIsRefused(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	first, _ := startTaskTurn(t, s, tt, nil, "1")
	chatID := first.ChatID
	for i := 2; i <= 4; i++ {
		startTaskTurn(t, s, tt, &chatID, string(rune('0'+i)))
	}

	msg := sendTask(t, s, &chatID, "5")
	call := toolCallsOf(t, msg)[0]
	assert.True(t, call.IsError)
	assert.Equal(t, "refused: "+tools.ErrChatTaskLimit.Error(), call.Output)
	assert.Len(t, taskRows(t, s.db), 4)
	assert.Len(t, taskFiles(t, s, chatID), 4)
}

// The app runs sixteen at once: four chats of four, then one more in a fifth
// chat is refused with the app's limit.
func TestASeventeenthTaskIsRefused(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	for c := range 4 {
		first, _ := startTaskTurn(t, s, tt, nil, "c"+string(rune('0'+c)))
		chatID := first.ChatID
		for i := 1; i < 4; i++ {
			startTaskTurn(t, s, tt, &chatID, "c"+string(rune('0'+c))+string(rune('0'+i)))
		}
	}

	msg := sendTask(t, s, nil, "fifth chat")
	call := toolCallsOf(t, msg)[0]
	assert.True(t, call.IsError)
	assert.Equal(t, "refused: "+tools.ErrTaskLimit.Error(), call.Output)
	assert.Len(t, taskRows(t, s.db), 16)
	assert.Empty(t, taskFiles(t, s, msg.ChatID))
}

// A start that fails leaves no row and no file, and its slot is free again.
func TestAFailedStartLeavesNoTask(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	tt.fail = errors.New("no shell")
	s := startServiceWithTool(t, tt)
	msg := sendTask(t, s, nil, "1")
	assert.Equal(t, "refused: no shell", toolCallsOf(t, msg)[0].Output)
	assert.Empty(t, taskRows(t, s.db))
	assert.Empty(t, taskFiles(t, s, msg.ChatID))
	s.turnsMu.Lock()
	assert.Empty(t, s.tasks)
	s.turnsMu.Unlock()
}

// taskOf is the one task chatID holds a slot for.
func taskOf(t *testing.T, s *service, chatID ChatID) *task {
	t.Helper()
	s.turnsMu.Lock()
	defer s.turnsMu.Unlock()
	require.Len(t, s.tasks[chatID], 1)
	for _, tk := range s.tasks[chatID] {
		return tk
	}
	return nil
}

// The file ends with the line that says how the task ended, on a line of its
// own, and the row says the same.
func TestTheOutputEndsWithTheExitLine(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	tt.write = "partial"
	s := startServiceWithTool(t, tt)
	msg, ft := startTaskTurn(t, s, tt, nil, "1")
	tk := taskOf(t, s, msg.ChatID)

	ft.exit(3)
	testutil.Wait(t, tk.done, "the task's row")
	assert.Equal(t, "partial\n[exited with code 3]\n", readFile(t, ft.out.Name()))
	rows := taskRows(t, s.db)
	require.Len(t, rows, 1)
	assert.Equal(t, "exited", rows[0].status)
	assert.Equal(t, sql.NullInt64{Int64: 3, Valid: true}, rows[0].exitCode)
	assert.True(t, rows[0].finished)
	assert.True(t, rows[0].notified, "the turn the exit started told it")
	assert.False(t, rows[0].stoppedBy.Valid)
}

// A stopped task's file ends [stopped], and its row names who stopped it.
func TestAStoppedTaskEndsStopped(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	msg, ft := startTaskTurn(t, s, tt, nil, "1")
	tk := taskOf(t, s, msg.ChatID)

	ok, err := s.StopBackgroundTask(t.Context(), tk.toolCallID)
	require.NoError(t, err)
	assert.True(t, ok)
	testutil.Wait(t, tk.done, "the task's row")
	assert.Equal(t, "[stopped]\n", readFile(t, ft.out.Name()))
	ok, err = s.StopBackgroundTask(t.Context(), tk.toolCallID)
	require.NoError(t, err)
	assert.False(t, ok, "nothing of that call is running now")
	assert.Equal(t, []bool{false}, ft.stopsSeen(), "the user's stop gives the grace")
	rows := taskRows(t, s.db)
	assert.Equal(t, "stopped", rows[0].status)
	assert.Equal(t, sql.NullString{String: "user", Valid: true}, rows[0].stoppedBy)
	assert.False(t, rows[0].exitCode.Valid)
	assert.False(t, rows[0].notified, "the user's stop is a waiting notice")
}

// A task holds its slot until its row is written, so a start after an exit is
// refused until then: the count always matches the rows still running.
func TestASlotFreesWhenTheRowIsWritten(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	reaped, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s.finishWrite = func(ctx context.Context, id TaskID, end taskEnd) error {
		once.Do(func() { close(reaped) })
		<-release
		return s.finishRow(ctx, id, end)
	}
	first, ft := startTaskTurn(t, s, tt, nil, "1")
	chatID := first.ChatID
	for i := 2; i <= 4; i++ {
		startTaskTurn(t, s, tt, &chatID, string(rune('0'+i)))
	}
	tk := taskOf4(t, s, chatID, ft)

	ft.exit(0)
	testutil.Wait(t, reaped, "the row write to begin")
	msg := sendTask(t, s, &chatID, "5")
	assert.Equal(t, "refused: "+tools.ErrChatTaskLimit.Error(), toolCallsOf(t, msg)[0].Output, "the slot is still held")

	close(release)
	testutil.Wait(t, tk.done, "the task's row")
	awaitTurnReleased(t, s, chatID) // the turn the exit started
	startTaskTurn(t, s, tt, &chatID, "6")
}

// taskOf4 is the task of chatID that ft is.
func taskOf4(t *testing.T, s *service, chatID ChatID, ft *fakeTask) *task {
	t.Helper()
	s.turnsMu.Lock()
	defer s.turnsMu.Unlock()
	for _, tk := range s.tasks[chatID] {
		if tk.proc == ft {
			return tk
		}
	}
	t.Fatal("no such task")
	return nil
}

// readFile is what a file holds.
func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

// Starting a task is not activity the list sorts by: the chat's updated_at is
// the turn's to move.
func TestStartingATaskDoesNotTouchTheChat(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	var before, after int64
	updatedAt := func() int64 {
		var at int64
		require.NoError(t, s.db.Read.QueryRow(`SELECT updated_at FROM chats`).Scan(&at))
		return at
	}
	tt.around = func(start func()) {
		before = updatedAt()
		start()
		after = updatedAt()
	}
	startTaskTurn(t, s, tt, nil, "1")
	assert.Equal(t, before, after)
}

// A task started in a turn still in flight reaches the watch as the live list
// has it: running, then exited, before the turn settles.
func TestTheLiveTurnShowsItsTask(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	gate := make(chan struct{})
	tt.around = func(start func()) {
		start()
		fakeOf(s).SetGate(gate) // holds the reply after the call, so the turn stays in flight
	}
	fakeOf(s).SetToolCalls(taskCall())
	msg := send(t, s, nil, "1", "start it")
	ft := testutil.Recv(t, tt.ready, "the task to start")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	st, err := s.WatchMessageCollection(ctx, msg.ChatID)
	require.NoError(t, err)
	withTask := func(status BackgroundTaskStatus) func(ChatMessageCollectionWatchFrame) bool {
		return func(f ChatMessageCollectionWatchFrame) bool {
			if f.Message == nil || f.Message.ID != msg.ID {
				return false
			}
			calls := toolCallsOf(t, *f.Message)
			return len(calls) == 1 && calls[0].Background != nil && calls[0].Background.Status == status
		}
	}
	running := awaitFrame(t, st.Frames, withTask(BackgroundTaskRunning))
	assert.True(t, running.Message.Status.inFlight())

	ft.exit(2)
	exited := awaitFrame(t, st.Frames, withTask(BackgroundTaskExited))
	assert.True(t, exited.Message.Status.inFlight(), "before the turn settles")
	assert.Equal(t, 2, *toolCallsOf(t, *exited.Message)[0].Background.ExitCode)

	close(gate)
	settled := awaitSettled(t, s, msg.ChatID, msg.ID)
	assert.Equal(t, BackgroundTaskExited, toolCallsOf(t, settled)[0].Background.Status, "the stored read")
}

// questionOf is the stored content of the question the answer msg follows.
func questionOf(t *testing.T, s *service, msg ChatMessage) []llm.Block {
	t.Helper()
	msgs, err := s.transcript(t.Context(), msg.ChatID)
	require.NoError(t, err)
	for i, m := range msgs {
		if m.ID == msg.ID {
			blocks, err := unmarshalBlocks(msgs[i-1].Content)
			require.NoError(t, err)
			return blocks
		}
	}
	t.Fatal("no such answer")
	return nil
}

// A notice that starts no turn of its own rides the chat's next question, after
// the card and ahead of the text, and is marked told in the same send.
func TestANoticeRidesTheNextSend(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	cards := &stubClusterCards{card: "the card"}
	box, lists := testBox(tt)
	s := startServiceWith(t, t.TempDir(), fakeLLM(), cards, box, lists)
	first, _ := startTaskTurn(t, s, tt, nil, "1")
	tk := taskOf(t, s, first.ChatID)
	_, err := s.StopBackgroundTask(t.Context(), tk.toolCallID)
	require.NoError(t, err)
	testutil.Wait(t, tk.done, "the task's row")
	require.False(t, taskRows(t, s.db)[0].notified, "a user's stop waits")

	cards.set("a new card")
	next := send(t, s, &first.ChatID, "2", "and now?")
	awaitSettled(t, s, next.ChatID, next.ID)

	q := questionOf(t, s, next)
	require.Len(t, q, 3)
	assert.Equal(t, llm.BlockContext, q[0].Type)
	assert.Equal(t, llm.BlockTaskNotification, q[1].Type)
	assert.Equal(t, llm.TaskNotice{
		Kind: llm.TaskCommand, ID: string(tk.id), ToolUseID: "call-1", OutputFile: taskRows(t, s.db)[0].path,
		Status: llm.TaskStopped, StoppedBy: llm.TaskStoppedByUser,
	}, *q[1].Task, "an ungated tool has no approval to name it by")
	assert.Equal(t, llm.TextBlock("and now?"), q[2])
	assert.True(t, taskRows(t, s.db)[0].notified)
	assert.Contains(t, llm.Prompt(fakeOf(s).LastRequest().Messages[2].Blocks), "was stopped by the user.",
		"the model was told")
}

// awaitNoticeTurn watches chatID until an answer other than those in seen
// settles, and returns it: the turn a notice started.
func awaitNoticeTurn(t *testing.T, s *service, chatID ChatID, seen ...MessageID) ChatMessage {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	st, err := s.WatchMessageCollection(ctx, chatID)
	require.NoError(t, err)
	f := awaitFrame(t, st.Frames, func(f ChatMessageCollectionWatchFrame) bool {
		m := f.Message
		return m != nil && m.Role == RoleAssistant && !slices.Contains(seen, m.ID) && !m.Status.inFlight()
	})
	return *f.Message
}

// runOf is what a run's row says of its trigger and what it ran on.
func runOf(t *testing.T, db *appdb.DB, id RunID) (trigger, triggerMessage, provider, model, effort string) {
	t.Helper()
	var eff sql.NullString
	require.NoError(t, db.Read.QueryRow(`SELECT trigger, trigger_message_id, provider, model, effort FROM agent_runs WHERE id = ?`,
		string(id)).Scan(&trigger, &triggerMessage, &provider, &model, &eff))
	return trigger, triggerMessage, provider, model, eff.String
}

// An exit after the turn has settled starts a turn of its own: a question that
// is the notice alone, a chat run triggered by it on the last answer's model, the
// notice told in the same transaction, and the chat moved up its list.
func TestAnExitStartsATurn(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	first, ft := startTaskTurn(t, s, tt, nil, "1")
	before, ok, err := s.Get(t.Context(), first.ChatID)
	require.NoError(t, err)
	require.True(t, ok)

	later := time.Now().Add(time.Hour)
	s.now = func() time.Time { return later } // before the exit, which every read of it follows
	ft.exit(0)
	answer := awaitNoticeTurn(t, s, first.ChatID, first.ID)
	assert.Equal(t, StatusComplete, answer.Status)

	q := questionOf(t, s, answer)
	require.Len(t, q, 1)
	assert.Equal(t, llm.BlockTaskNotification, q[0].Type)
	assert.Equal(t, llm.TaskExited, q[0].Task.Status)
	trigger, triggerMessage, provider, model, effort := runOf(t, s.db, answer.RunID)
	assert.Equal(t, "chat", trigger)
	msgs, err := s.transcript(t.Context(), first.ChatID)
	require.NoError(t, err)
	assert.Equal(t, string(msgs[len(msgs)-2].ID), triggerMessage)
	assert.Equal(t, []string{"fake", "fake", "high"}, []string{provider, model, effort}, "the last answer's")
	assert.True(t, taskRows(t, s.db)[0].notified)
	after, _, err := s.Get(t.Context(), first.ChatID)
	require.NoError(t, err)
	assert.Equal(t, normalizeTime(later), after.UpdatedAt, "moved up from %v", before.UpdatedAt)
	var requestKey sql.NullString
	require.NoError(t, s.db.Read.QueryRow(`SELECT request_key FROM messages WHERE id = ?`, triggerMessage).Scan(&requestKey))
	assert.False(t, requestKey.Valid, "a turn the sidecar starts has no request key")
}

// An exit while a turn runs waits for it: the turn's settle kicks the chat, and
// every notice that piled up meanwhile rides one message.
func TestNoticesThatPileUpRideOneMessage(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	first, ft1 := startTaskTurn(t, s, tt, nil, "1")
	chatID := first.ChatID
	second, ft2 := startTaskTurn(t, s, tt, &chatID, "2")

	gate := make(chan struct{})
	fakeOf(s).SetGate(gate)
	held := send(t, s, &chatID, "3", "hold on")
	done1, done2 := taskDoneOf(t, s, chatID, ft1), taskDoneOf(t, s, chatID, ft2)
	ft1.exit(0)
	ft2.exit(1)
	testutil.Wait(t, done1, "the first row")
	testutil.Wait(t, done2, "the second row")
	assert.Equal(t, held.ID, s.turnOf(chatID).msg.ID, "the exits started nothing while the turn runs")

	close(gate)
	answer := awaitNoticeTurn(t, s, chatID, first.ID, second.ID, held.ID)
	q := questionOf(t, s, answer)
	require.Len(t, q, 2, "both notices, one message")
	assert.Equal(t, llm.BlockTaskNotification, q[0].Type)
	assert.Equal(t, llm.BlockTaskNotification, q[1].Type)
}

// A cancelled turn kicks nothing, so a Cancel stops the chat; the task it started
// keeps running, and an exit's notice waits for the next question.
func TestACancelledTurnKicksNothing(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	first, ft := startTaskTurn(t, s, tt, nil, "1")
	chatID := first.ChatID

	// The second turn kicks after its row settles; a late kick would find the
	// exit's notice once the Cancel frees the slot, so wait out the whole turn.
	gate := make(chan struct{})
	fakeOf(s).SetToolCalls(taskCall())
	fakeOf(s).SetGate(gate)
	send(t, s, &chatID, "2", "start it")
	second := s.turnOf(chatID)
	close(gate)
	running := testutil.Recv(t, tt.ready, "the task to start")
	testutil.Wait(t, second.done, "the second turn")

	gate = make(chan struct{})
	fakeOf(s).SetGate(gate)
	held := send(t, s, &chatID, "3", "hold on")
	done := taskDoneOf(t, s, chatID, ft)
	ft.exit(0)
	testutil.Wait(t, done, "the row")
	turn := s.turnOf(chatID)
	require.NoError(t, s.Cancel(t.Context(), chatID))
	testutil.Wait(t, turn.done, "the cancelled turn")

	assert.Nil(t, s.turnOf(chatID), "nothing kicked")
	got := awaitSettled(t, s, chatID, held.ID)
	assert.Equal(t, StatusCancelled, got.Status)
	assert.Empty(t, running.stopsSeen(), "a Cancel leaves the tasks running")
	notified := 0
	for _, r := range taskRows(t, s.db) {
		if r.notified {
			notified++
		}
	}
	assert.Zero(t, notified, "the exit waits")
}

// A kick the send's checks refuse starts nothing and leaves the notices waiting:
// a cluster marked for deletion, or a model the catalog no longer holds.
func TestAKickTheSendRefusesStartsNothing(t *testing.T) {
	t.Parallel()

	for name, refuse := range map[string]func(*service, ChatMessage){
		"marked cluster": func(s *service, _ ChatMessage) { markClusterIn(s, "1") },
		"model gone": func(s *service, msg ChatMessage) {
			_, _ = s.db.Write.Exec(`UPDATE agent_runs SET model = 'gone' WHERE id = ?`, string(msg.RunID))
		},
	} {
		t.Run(name, func(t *testing.T) {
			tt := newTaskTool()
			s := startServiceWithTool(t, tt)
			first, ft := startTaskTurn(t, s, tt, nil, "1")
			refuse(s, first)

			done := taskDoneOf(t, s, first.ChatID, ft)
			ft.exit(0)
			testutil.Wait(t, done, "the row")
			assert.Nil(t, s.turnOf(first.ChatID))
			msgs, err := s.transcript(t.Context(), first.ChatID)
			require.NoError(t, err)
			assert.Len(t, msgs, 2, "no message was filed")
			assert.False(t, taskRows(t, s.db)[0].notified)
		})
	}
}

// markClusterIn marks a cluster of s's own DB for deletion.
func markClusterIn(s *service, id string) {
	_, _ = s.db.Write.Exec(`UPDATE clusters SET delete_requested_at = 1 WHERE id = ?`, id)
}

// taskDoneOf is the done of chatID's task that ft is, taken while it holds its slot.
func taskDoneOf(t *testing.T, s *service, chatID ChatID, ft *fakeTask) chan struct{} {
	t.Helper()
	return taskOf4(t, s, chatID, ft).done
}

// The model stops its own chat's tasks alone, with the grace, and its stop is
// written told, since TaskStop's result told it. Another chat's id, and a task
// that has finished, are no task of this chat's.
func TestTheModelStopsTheChatsOwnTask(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	a, ftA := startTaskTurn(t, s, tt, nil, "a")
	b, _ := startTaskTurn(t, s, tt, nil, "b")
	tkA, tkB := taskOf(t, s, a.ChatID), taskOf(t, s, b.ChatID)

	assert.False(t, s.chatTasks(a.ChatID, nil).Stop(string(tkB.id)), "another chat's")
	assert.True(t, s.chatTasks(a.ChatID, nil).Stop(string(tkA.id)))
	testutil.Wait(t, tkA.done, "the row")
	assert.Equal(t, []bool{false}, ftA.stopsSeen(), "with the grace")
	assert.False(t, s.chatTasks(a.ChatID, nil).Stop(string(tkA.id)), "a finished one")

	for _, r := range taskRows(t, s.db) {
		if r.id == string(tkA.id) {
			assert.Equal(t, "stopped", r.status)
			assert.Equal(t, sql.NullString{String: "model", Valid: true}, r.stoppedBy)
			assert.True(t, r.notified)
		}
	}
	assert.Nil(t, s.turnOf(a.ChatID), "a stop starts no turn")
}

// A stop that lands after the reap, while the task still holds its slot, answers
// that nothing is running, the model's and the user's alike: the row is the exit,
// and its notice waits, so the model hears of the exit alone.
func TestAStopAfterTheReapLeavesTheExit(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	msg, ft := startTaskTurn(t, s, tt, nil, "1")
	tk := taskOf(t, s, msg.ChatID)
	written, release := make(chan taskEnd, 1), make(chan struct{})
	s.finishWrite = func(ctx context.Context, id TaskID, end taskEnd) error {
		written <- end
		<-release
		return s.finishRow(ctx, id, end)
	}

	ft.exit(0)
	end := testutil.Recv(t, written, "the row write")
	assert.False(t, s.chatTasks(msg.ChatID, nil).Stop(string(tk.id)), "the model's")
	ok, err := s.StopBackgroundTask(t.Context(), tk.toolCallID)
	require.NoError(t, err)
	assert.False(t, ok, "the user's")
	close(release)
	testutil.Wait(t, tk.done, "the row")

	assert.Equal(t, taskExited, end.Status)
	assert.False(t, end.Notified, "the exit waits for the model")
}

// A model's stop that the process never felt — it exited on its own first — is
// the exit too, and its notice waits.
func TestAModelsStopThatMissedIsTheExit(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	msg, ft := startTaskTurn(t, s, tt, nil, "1")
	tk := taskOf(t, s, msg.ChatID)
	ft.mu.Lock()
	ft.deaf = true
	ft.mu.Unlock()
	written := make(chan taskEnd, 1)
	s.finishWrite = func(ctx context.Context, id TaskID, end taskEnd) error {
		written <- end
		return s.finishRow(ctx, id, end)
	}

	require.True(t, s.chatTasks(msg.ChatID, nil).Stop(string(tk.id)))
	ft.exit(0)
	end := testutil.Recv(t, written, "the row write")
	assert.Equal(t, taskExited, end.Status)
	assert.False(t, end.Notified)
}

// Deleting a chat joins its turn, then stops its tasks at once and joins them,
// so every row is written before the rows go and every file is closed before the
// directory does; a start during the delete is refused.
func TestDeletingAChatStopsAndJoinsItsTasks(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	first, ft := startTaskTurn(t, s, tt, nil, "1")
	chatID := first.ChatID

	gate := make(chan struct{})
	fakeOf(s).SetGate(gate)
	send(t, s, &chatID, "2", "hold on")
	turn := s.turnOf(chatID)
	require.NotNil(t, turn)

	var (
		mu    sync.Mutex
		order []string
	)
	log := func(step string) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, step)
	}
	ft.mu.Lock()
	ft.onStop = func() {
		select {
		case <-turn.done:
			log("stop after the turn")
		default:
			log("stop before the turn")
		}
	}
	ft.mu.Unlock()
	s.finishWrite = func(ctx context.Context, id TaskID, end taskEnd) error {
		log("task row")
		return s.finishRow(ctx, id, end)
	}
	s.deleteWrite = func(ctx context.Context, id ChatID) (bool, error) {
		log("delete")
		err := s.registerTask(&task{id: newTaskID(), chatID: chatID, done: make(chan struct{})})
		assert.ErrorIs(t, err, ErrChatGone, "a start during the delete")
		return s.deleteRow(ctx, id)
	}

	require.NoError(t, s.Delete(t.Context(), chatID))
	assert.Equal(t, []string{"stop after the turn", "task row", "delete"}, order)
	assert.Equal(t, []bool{true}, ft.stopsSeen(), "at once")
	assert.Empty(t, taskRows(t, s.db))
	_, err := os.Stat(s.chatDir(chatID).Path())
	assert.True(t, os.IsNotExist(err), "the chat's directory goes")
	s.turnsMu.Lock()
	assert.Empty(t, s.tasks)
	s.turnsMu.Unlock()
}

// stopNow runs the service's stop, as the app's shutdown does.
func stopNow(t *testing.T, s *service) {
	t.Helper()
	require.NoError(t, s.stop(t.Context()))
}

// Stopping the service kills every task at once and writes each row stopped by
// the app, on a context its own cancel does not end; no exit notice follows and
// no turn starts.
func TestShutdownStopsEveryTask(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	a, ftA := startTaskTurn(t, s, tt, nil, "a")
	_, ftB := startTaskTurn(t, s, tt, nil, "b")

	stopNow(t, s)
	assert.Equal(t, []bool{true}, ftA.stopsSeen())
	assert.Equal(t, []bool{true}, ftB.stopsSeen())
	rows := taskRows(t, s.db)
	require.Len(t, rows, 2)
	for _, r := range rows {
		assert.Equal(t, "stopped", r.status)
		assert.Equal(t, sql.NullString{String: "app", Valid: true}, r.stoppedBy)
		assert.False(t, r.notified, "it rides the next question")
	}
	msgs, err := s.transcript(context.Background(), a.ChatID)
	require.NoError(t, err)
	assert.Len(t, msgs, 2, "no turn")
}

// A task registered before the stop but started after its walk is killed the
// moment it has a process, and the stop waits for its row.
func TestATaskStartingDuringShutdownIsStopped(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	tt.holding, tt.hold = make(chan struct{}), make(chan struct{})
	s := startServiceWithTool(t, tt)
	fakeOf(s).SetToolCalls(taskCall())
	msg := send(t, s, nil, "1", "start it")
	testutil.Recv(t, tt.holding, "the task's row")

	stopped := make(chan error, 1)
	go func() { stopped <- s.stop(context.Background()) }()
	require.Eventually(t, func() bool {
		s.turnsMu.Lock()
		defer s.turnsMu.Unlock()
		for _, tk := range s.tasks[msg.ChatID] {
			return tk.stopPending
		}
		return false
	}, testutil.Timeout, time.Millisecond, "the stop's walk to find the task")
	close(tt.hold)

	require.NoError(t, testutil.Recv(t, stopped, "the stop"))
	ft := testutil.Recv(t, tt.ready, "the task")
	assert.Equal(t, []bool{true}, ft.stopsSeen(), "killed as it started")
	rows := taskRows(t, s.db)
	require.Len(t, rows, 1)
	assert.Equal(t, sql.NullString{String: "app", Valid: true}, rows[0].stoppedBy)
}

// A start past enter but not yet registered when stopped closes is refused at
// registration, and one after stopped closes at enter: nothing starts behind
// the stop's walk.
func TestAStartBehindTheStopIsRefused(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	require.NoError(t, s.enter())
	stopped := make(chan error, 1)
	go func() { stopped <- s.stop(context.Background()) }()
	testutil.Wait(t, s.stopped, "stopped to close")
	err := s.registerTask(&task{id: newTaskID(), chatID: "c", done: make(chan struct{})})
	assert.ErrorIs(t, err, ErrStopping)
	s.wg.Done()
	require.NoError(t, testutil.Recv(t, stopped, "the stop"))

	tc := s.chatTasks("c", &runJournal{openTool: &toolCallEntry{ID: "call"}})
	_, _, err = tc.Start(func(*os.File) (tools.Task, error) {
		t.Error("nothing starts")
		return nil, nil
	})
	assert.ErrorIs(t, err, ErrStopping)
}

// A task a crash left running is lost at the next start: its row says so and a
// watch opened before the start hears it, no turn starts, and its notice rides
// the chat's next question.
func TestTheStartSweepMarksRunningTasksLost(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.UnixMilli(1_000).UTC()
	db := openTestDB(t, dir)
	c := seedChat(t, db, aChat("1", now))
	turn := seedTurn(t, db, c.ID, now)
	settleSeededRun(t, db, turn.Run, RunSucceeded, now)
	for _, q := range []string{
		`INSERT INTO llm_calls (id, run_id, seq, provider, model, started_at, finished_at) VALUES ('l', '` + string(turn.Run) + `', 0, 'fake', 'fake', 0, 0)`,
		`INSERT INTO tool_calls (id, llm_call_id, seq, tool_name, contract_name, tool_use_id, arguments, cwd, status, created_at, started_at, finished_at)
		 VALUES ('t', 'l', 0, 'Bash', 'bash/v1', 'call-9', '{"command":"make serve","description":"Serve the site","run_in_background":true}', '/home', 'succeeded', 0, 0, 0)`,
		`INSERT INTO approvals (id, tool_call_id, status, created_at) VALUES ('a', 't', 'approved', 0)`,
		`INSERT INTO background_tasks (id, chat_id, tool_call_id, output_path, status, started_at)
		 VALUES ('task-1', '` + string(c.ID) + `', 't', '/r/task-1.output', 'running', 0)`,
	} {
		_, err := db.Write.Exec(q)
		require.NoError(t, err, q)
	}
	require.NoError(t, db.Close())

	s, err := newService(openTestDB(t, dir), chatsDirIn(dir), monitorDirIn(dir), fakeLLM(), noClusterCards, nil, testReaders, noLists, sandbox.State{}, testSettings(t), nil)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	st, err := s.WatchMessageCollection(ctx, c.ID)
	require.NoError(t, err)
	startPrepared(t, s)
	awaitFrame(t, st.Frames, func(f ChatMessageCollectionWatchFrame) bool {
		if f.Message == nil || f.Message.ID != turn.Assistant {
			return false
		}
		calls := toolCallsOf(t, *f.Message)
		return len(calls) == 1 && calls[0].Background != nil && calls[0].Background.Status == BackgroundTaskLost
	})
	assert.Nil(t, s.turnOf(c.ID), "no turn at startup")
	rows := taskRows(t, s.db)
	assert.Equal(t, "lost", rows[0].status)
	assert.True(t, rows[0].finished)
	assert.False(t, rows[0].notified)

	next := send(t, s, &c.ID, "2", "what happened?")
	q := questionOf(t, s, next)
	require.Len(t, q, 3, "the context, the notice and the text")
	assert.Equal(t, llm.TaskLost, q[1].Task.Status)
	assert.Equal(t, "make serve", q[1].Task.Command, "read off the call's arguments")
	assert.Equal(t, "Serve the site", q[1].Task.Description)
}

// A start with no call running is refused: a task's row hangs off
// its call.
func TestATaskStartsOnlyFromARunningCall(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	_, _, err := s.chatTasks("c", nil).Start(func(*os.File) (tools.Task, error) {
		t.Error("nothing starts")
		return nil, nil
	})
	assert.ErrorIs(t, err, errNoOpenCall)
}

// A start whose file or row cannot be made leaves nothing behind: no file, no
// row, and its slot free.
func TestAStartWhoseFileOrRowFailsLeavesNothing(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	tc := s.chatTasks(c.ID, &runJournal{openTool: &toolCallEntry{ID: "no-such-call"}})
	never := func(*os.File) (tools.Task, error) {
		t.Error("nothing starts")
		return nil, nil
	}

	_, _, err := tc.Start(never)
	require.Error(t, err, "the row refers to a call that is not there")
	assert.Empty(t, taskFiles(t, s, c.ID))

	tasks := filepath.Join(s.chatDir(c.ID).Path(), taskDirName)
	require.NoError(t, os.RemoveAll(tasks))
	require.NoError(t, os.WriteFile(tasks, nil, 0o600))
	_, _, err = tc.Start(never)
	require.Error(t, err, "the tasks directory is a file")

	require.NoError(t, os.RemoveAll(s.chatDir(c.ID).Path()))
	require.NoError(t, os.WriteFile(s.chatDir(c.ID).Path(), nil, 0o600))
	_, _, err = tc.Start(never)
	assert.ErrorIs(t, err, rootdir.ErrNotADirectory, "the chat's entry is a file")

	assert.Empty(t, taskRows(t, s.db))
	s.turnsMu.Lock()
	assert.Empty(t, s.tasks)
	s.turnsMu.Unlock()
}

// An exit whose code could not be read says so in its file, and its row holds
// no code.
func TestAnUnreadExitSaysSo(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	msg, ft := startTaskTurn(t, s, tt, nil, "1")
	tk := taskOf(t, s, msg.ChatID)

	ft.end(tools.Exit{})
	testutil.Wait(t, tk.done, "the row")
	assert.Equal(t, "[exited; its code could not be read]\n", readFile(t, ft.out.Name()))
	rows := taskRows(t, s.db)
	assert.Equal(t, "exited", rows[0].status)
	assert.False(t, rows[0].exitCode.Valid)
}

// A row write that fails still frees the slot: the row stays running, and the
// next start marks it lost.
func TestAFailedRowWriteStillFreesTheSlot(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	s.finishWrite = func(context.Context, TaskID, taskEnd) error { return errors.New("disk is full") }
	msg, ft := startTaskTurn(t, s, tt, nil, "1")
	tk := taskOf(t, s, msg.ChatID)

	ft.exit(0)
	testutil.Wait(t, tk.done, "the watcher")
	s.turnsMu.Lock()
	assert.Empty(t, s.tasks)
	s.turnsMu.Unlock()
	assert.Equal(t, "running", taskRows(t, s.db)[0].status)
}

// A notice turn is refused while the chat's slot is held, and files nothing:
// the turn holding it kicks when it settles.
func TestANoticeTurnWaitsForTheSlot(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	first, ft := startTaskTurn(t, s, tt, nil, "1")
	chatID := first.ChatID
	fakeOf(s).SetGate(make(chan struct{}))
	send(t, s, &chatID, "2", "hold on")
	done := taskDoneOf(t, s, chatID, ft)
	ft.exit(0)
	testutil.Wait(t, done, "the row")

	before, err := s.transcript(t.Context(), chatID)
	require.NoError(t, err)
	assert.ErrorIs(t, s.startNoticeTurn(chatID), ErrTurnInFlight)
	after, err := s.transcript(t.Context(), chatID)
	require.NoError(t, err)
	assert.Len(t, after, len(before), "nothing filed")
	assert.False(t, taskRows(t, s.db)[0].notified)
}

// The live list keeps its own when either list does not parse.
func TestWithStoredCallsKeepsAListItCannotRead(t *testing.T) {
	t.Parallel()

	assert.Equal(t, rawjson.RawJSON("nope"), withStoredCalls("nope", "[]"))
	assert.Equal(t, rawjson.RawJSON("[]"), withStoredCalls("[]", "nope"))
}

// refuse makes every write of a statement of kind on table fail, as a full disk
// or a broken file would.
func refuse(t *testing.T, s *service, kind, table string) {
	t.Helper()
	_, err := s.db.Write.Exec(`CREATE TRIGGER refuse_` + table + ` BEFORE ` + kind + ` ON ` + table +
		` BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)
}

// A notice turn whose writes fail files nothing, frees the slot it reserved,
// and leaves the notices waiting.
func TestANoticeTurnThatCannotWriteStartsNothing(t *testing.T) {
	t.Parallel()

	// Each trigger is armed before the task ends, since the first turn's own kick
	// can start the notice turn the moment the row lands, and refuses only the
	// turn's writes: the row's end leaves notified_at null and chats alone.
	for table, when := range map[string]string{
		"background_tasks": "NEW.notified_at IS NOT NULL",
		"chats":            "1",
	} {
		t.Run(table, func(t *testing.T) {
			tt := newTaskTool()
			s := startServiceWithTool(t, tt)
			first, ft := startTaskTurn(t, s, tt, nil, "1")
			done := taskDoneOf(t, s, first.ChatID, ft)
			_, err := s.db.Write.Exec(`CREATE TRIGGER refuse_` + table + ` BEFORE UPDATE ON ` + table +
				` WHEN ` + when + ` BEGIN SELECT RAISE(ABORT, 'refused'); END`)
			require.NoError(t, err)
			ft.exit(0)
			testutil.Wait(t, done, "the row")

			// The notice turn the row's kick starts runs on, reserving the
			// slot until its refused write releases it.
			require.Eventually(t, func() bool { return s.turnOf(first.ChatID) == nil }, testutil.Timeout, time.Millisecond, "the slot is free")
			msgs, err := s.transcript(t.Context(), first.ChatID)
			require.NoError(t, err)
			assert.Len(t, msgs, 2, "nothing filed")
			assert.False(t, taskRows(t, s.db)[0].notified)
		})
	}
}

// A send whose notices cannot be marked told is refused whole: the question does
// not go without them, and they keep waiting.
func TestASendThatCannotTakeItsNoticesIsRefused(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	first, _ := startTaskTurn(t, s, tt, nil, "1")
	tk := taskOf(t, s, first.ChatID)
	_, err := s.StopBackgroundTask(t.Context(), tk.toolCallID)
	require.NoError(t, err)
	testutil.Wait(t, tk.done, "the row")
	refuse(t, s, "UPDATE", "background_tasks")

	_, err = s.Send(t.Context(), &first.ChatID, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("2"), "and?", nil)
	require.ErrorContains(t, err, "mark notified")
	msgs, err := s.transcript(t.Context(), first.ChatID)
	require.NoError(t, err)
	assert.Len(t, msgs, 2)
	assert.False(t, taskRows(t, s.db)[0].notified)
}

// A file whose end line cannot be written still gets its row: the record of how
// the task ended does not wait on the file.
func TestAFileThatCannotBeEndedStillGetsItsRow(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	tt.closeOut = true
	s := startServiceWithTool(t, tt)
	msg, ft := startTaskTurn(t, s, tt, nil, "1")
	tk := taskOf(t, s, msg.ChatID)

	ft.exit(5)
	testutil.Wait(t, tk.done, "the row")
	rows := taskRows(t, s.db)
	assert.Equal(t, "exited", rows[0].status)
	assert.Equal(t, sql.NullInt64{Int64: 5, Valid: true}, rows[0].exitCode)
	assert.Empty(t, readFile(t, ft.out.Name()), "nothing was written to it")
}

// holdTasks registers a running fake task under each of the runs given, in chat
// "c", and returns them by run.
func holdTasks(s *service, runs ...RunID) map[RunID]*fakeTask {
	out := map[RunID]*fakeTask{}
	s.turnsMu.Lock()
	defer s.turnsMu.Unlock()
	s.tasks["c"] = map[TaskID]*task{}
	for _, run := range runs {
		ft := newFakeTask(nil)
		s.tasks["c"][TaskID(run)] = &task{id: TaskID(run), chatID: "c", runID: run, proc: ft, done: make(chan struct{})}
		out[run] = ft
	}
	return out
}

// A subagent's TaskStop reaches only the tasks its own run's calls started: a
// stop by the model is written notified, and only the model that called it read
// the result, so a subagent stopping a sibling or its own agent would leave the
// parent waiting for a notice that never comes.
func TestASubagentStopsOnlyWhatItStarted(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	sub := &runJournal{s: s, chatID: "c", runID: "sub", agentCallID: "agent-call"}
	held := holdTasks(s, "sub", "parent", "sibling")

	assert.False(t, s.chatTasks("c", sub).Stop("parent"), "its own agent is its parent's task")
	assert.False(t, s.chatTasks("c", sub).Stop("sibling"))
	assert.True(t, s.chatTasks("c", sub).Stop("sub"))
	assert.Empty(t, held["parent"].stopsSeen())
	assert.Empty(t, held["sibling"].stopsSeen())
	assert.Equal(t, []bool{false}, held["sub"].stopsSeen())
}

// The parent's TaskStop reaches every task of the chat, a subagent's command
// included.
func TestTheParentsTaskStopReachesASubagentsCommand(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	parent := &runJournal{s: s, chatID: "c", runID: "parent"}
	held := holdTasks(s, "sub")

	assert.True(t, s.chatTasks("c", parent).Stop("sub"))
	assert.Equal(t, []bool{false}, held["sub"].stopsSeen())
}

// taskDirOf is the entry c1 under a root of the test's own, closed at cleanup.
func taskDirOf(t *testing.T) (entryDir, string) {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	return entryDir{root: root, name: "c1"}, dir
}

// A task's file is always a new one: a second start under the same id is refused.
func TestATaskFileIsNeverOpenedTwice(t *testing.T) {
	t.Parallel()

	d, _ := taskDirOf(t)
	f, _, err := openTaskFile(d, "t1")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	_, _, err = openTaskFile(d, "t1")

	assert.ErrorIs(t, err, fs.ErrExist)
}

// Removing the file of a chat with no directory makes none.
func TestRemovingATaskFileOfNoDirectoryMakesNone(t *testing.T) {
	t.Parallel()

	d, dir := taskDirOf(t)

	removeTaskFile(d, "t1")

	_, err := os.Stat(filepath.Join(dir, "c1"))
	assert.ErrorIs(t, err, fs.ErrNotExist)
}
