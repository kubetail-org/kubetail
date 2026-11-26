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
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/logswatch"
)

// fakeWatch is a watch a test ends by hand: finish ends it as a follow would, and a
// stop ends it stopped, holding what it held.
type fakeWatch struct {
	ended chan struct{}
	once  sync.Once
	// held is the end it answers when nobody stopped it.
	held tools.WatchEnd

	mu sync.Mutex
	by []string // each End's stoppedBy
}

func newFakeWatch() *fakeWatch { return &fakeWatch{ended: make(chan struct{})} }

func (w *fakeWatch) Wait() { <-w.ended }

func (w *fakeWatch) Stop() { w.once.Do(func() { close(w.ended) }) }

func (w *fakeWatch) End(by string) tools.WatchEnd {
	w.mu.Lock()
	w.by = append(w.by, by)
	w.mu.Unlock()
	if by != "" {
		return tools.WatchEnd{Status: tools.WatchStopped, Result: w.held.Result}
	}
	return w.held
}

// finish ends the watch on its own with end.
func (w *fakeWatch) finish(end tools.WatchEnd) {
	w.held = end
	w.once.Do(func() { close(w.ended) })
}

// stoppedBy is each End's stoppedBy, in order.
func (w *fakeWatch) stoppedBy() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.by...)
}

// watchTool is a LogsWatch that starts a fakeWatch through the runtime's Watches
// and answers with what Start said.
type watchTool struct {
	ready chan *fakeWatch
}

func newWatchTool() *watchTool { return &watchTool{ready: make(chan *fakeWatch, 8)} }

func (e *watchTool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{Name: "LogsWatch", Description: "watches logs", InputSchema: json.RawMessage(`{"type":"object"}`)}
}

func (e *watchTool) Prompt() string                   { return "## LogsWatch\n\nWatches logs." }
func (e *watchTool) Name() string                     { return "LogsWatch" }
func (e *watchTool) ContractName() tools.ContractName { return logswatch.ContractName }
func (e *watchTool) ActionKind() tools.ActionKind     { return tools.ActionLogsWatch }
func (e *watchTool) Action(json.RawMessage, string, bool) (tools.Action, error) {
	return tools.Action{LogsWatch: &tools.LogsWatchAction{}}, nil
}

func (e *watchTool) Run(_ context.Context, rt tools.Runtime, _ json.RawMessage) (string, bool) {
	if rt.Watches == nil {
		return "no watches here", true
	}
	w := newFakeWatch()
	id, err := rt.Watches.Start(w)
	if err != nil {
		return tools.StartRefusal(err), true
	}
	e.ready <- w
	return "watching " + id, false
}

// startWatchTurn sends a question whose answer starts one watch, and returns the
// answer once settled with the watch.
func startWatchTurn(t *testing.T, s *service, wt *watchTool, chatID *ChatID, key string) (ChatMessage, *fakeWatch) {
	t.Helper()
	fakeOf(s).SetToolCalls(llm.StagedCall("LogsWatch", `{}`))
	msg := send(t, s, chatID, key, "watch it")
	w := testutil.Recv(t, wt.ready, "the watch to start")
	return awaitSettled(t, s, msg.ChatID, msg.ID), w
}

// matchedEnd is a watch that held one line.
var matchedEnd = tools.WatchEnd{Status: tools.WatchMatched, Result: tools.WatchResult{
	Text: "Watched 1 line of pods/a in prod.\n2026-10-18T09:00:00Z prod/a/app panic", EndedBecause: "it reached its line limit",
	Lines: []tools.LogsReadLine{{Timestamp: time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC), Namespace: "prod", Pod: "a", Container: "app", Message: "panic"}},
}}

// resultOf is the task row's result, read back as the watch wrote it.
func resultOf(t *testing.T, s *service, id TaskID) tools.WatchResult {
	t.Helper()
	var raw string
	require.NoError(t, s.db.Read.QueryRow(`SELECT result FROM background_tasks WHERE id = ?`, string(id)).Scan(&raw))
	var r tools.WatchResult
	require.NoError(t, json.Unmarshal([]byte(raw), &r))
	return r
}

// A watch is a task: it takes a slot under the call that started it, with a file
// in the chat's directory, and holds it until its end is written, with its result.
// Its file stays empty.
func TestAWatchIsATaskThatEndsWithItsResult(t *testing.T) {
	t.Parallel()

	wt := newWatchTool()
	s := startServiceWithTool(t, wt)
	msg, w := startWatchTurn(t, s, wt, nil, "1")
	tk := taskOf(t, s, msg.ChatID)

	rows := taskRows(t, s.db)
	require.Len(t, rows, 1)
	assert.Equal(t, "running", rows[0].status)
	assert.Equal(t, toolCallIDOf(t, s.db, msg.RunID), rows[0].toolCallID)
	assert.Equal(t, "watching "+rows[0].id, toolCallsOf(t, msg)[0].Output)

	w.finish(matchedEnd)
	testutil.Wait(t, tk.done, "the watch's row")

	rows = taskRows(t, s.db)
	assert.Equal(t, "matched", rows[0].status)
	assert.False(t, rows[0].stoppedBy.Valid)
	assert.Equal(t, matchedEnd.Result, resultOf(t, s, tk.id))
	assert.Equal(t, []string{""}, w.stoppedBy())
	assert.Empty(t, readFile(t, rows[0].path))
	s.turnsMu.Lock()
	defer s.turnsMu.Unlock()
	assert.Empty(t, s.tasks, "the slot is free")
}

// A watch the model stops is written stopped and not notified: TaskStop answers
// before the lines are written, so the notice carries them. One the user stops is
// stopped by the user. Either keeps what it held.
func TestAStoppedWatchKeepsItsLines(t *testing.T) {
	t.Parallel()

	for _, by := range []string{stoppedByModel, stoppedByUser} {
		wt := newWatchTool()
		s := startServiceWithTool(t, wt)
		msg, w := startWatchTurn(t, s, wt, nil, "1")
		w.held = matchedEnd
		tk := taskOf(t, s, msg.ChatID)

		if by == stoppedByModel {
			require.True(t, s.chatTasks(msg.ChatID, nil).Stop(string(tk.id)))
		} else {
			_, err := s.StopBackgroundTask(t.Context(), tk.toolCallID)
			require.NoError(t, err)
		}
		testutil.Wait(t, tk.done, "the watch's row")

		r := taskRows(t, s.db)[0]
		assert.Equal(t, "stopped", r.status, by)
		assert.Equal(t, by, r.stoppedBy.String)
		assert.False(t, r.notified, "%s: the notice carries the lines", by)
		assert.Equal(t, matchedEnd.Result, resultOf(t, s, tk.id), by)
		assert.Equal(t, []string{by}, w.stoppedBy(), by)
	}
}

// A watch that cannot take a slot is refused in StartRefusal's words.
func TestAFifthWatchIsRefused(t *testing.T) {
	t.Parallel()

	wt := newWatchTool()
	s := startServiceWithTool(t, wt)
	first, _ := startWatchTurn(t, s, wt, nil, "1")
	for i := range 3 {
		startWatchTurn(t, s, wt, &first.ChatID, string(rune('2'+i)))
	}

	fakeOf(s).SetToolCalls(llm.StagedCall("LogsWatch", `{}`))
	msg := send(t, s, &first.ChatID, "5", "one more")
	msg = awaitSettled(t, s, msg.ChatID, msg.ID)
	assert.Equal(t, tools.StartRefusal(tools.ErrChatTaskLimit), toolCallsOf(t, msg)[0].Output)
}

// noticeOfWatch is the one watch notice the question before answer carries.
func noticeOfWatch(t *testing.T, s *service, answer ChatMessage) llm.TaskNotice {
	t.Helper()
	for _, b := range questionOf(t, s, answer) {
		if b.Type == llm.BlockTaskNotification && b.Task.Kind == llm.TaskWatch {
			return *b.Task
		}
	}
	t.Fatal("no watch's notice")
	return llm.TaskNotice{}
}

// A watch a user's send started starts a turn when it matches, and its notice
// carries the sample.
func TestAMatchedWatchStartsATurn(t *testing.T) {
	t.Parallel()

	wt := newWatchTool()
	s := startServiceWithTool(t, wt)
	first, w := startWatchTurn(t, s, wt, nil, "1")
	tk := taskOf(t, s, first.ChatID)

	w.finish(matchedEnd)
	answer := awaitNoticeTurn(t, s, first.ChatID, first.ID)

	assert.Equal(t, llm.TaskNotice{
		Kind: llm.TaskWatch, ID: string(tk.id), ToolUseID: "call-1", Status: llm.TaskMatched,
		Result: matchedEnd.Result.Text, Matched: 1,
	}, noticeOfWatch(t, s, answer))
	assert.True(t, taskRows(t, s.db)[0].notified)
}

// A watch the model stops starts no turn; its notice, with the lines it held,
// rides the next send.
func TestAWatchTheModelStopsRidesTheNextSend(t *testing.T) {
	t.Parallel()

	wt := newWatchTool()
	s := startServiceWithTool(t, wt)
	first, w := startWatchTurn(t, s, wt, nil, "1")
	w.held = matchedEnd
	tk := taskOf(t, s, first.ChatID)
	require.True(t, s.chatTasks(first.ChatID, nil).Stop(string(tk.id)))
	testutil.Wait(t, tk.done, "the watch's row")
	assert.Nil(t, s.turnOf(first.ChatID), "a stop starts no turn")

	next := send(t, s, &first.ChatID, "2", "and now?")
	next = awaitSettled(t, s, next.ChatID, next.ID)

	n := noticeOfWatch(t, s, next)
	assert.Equal(t, llm.TaskStopped, n.Status)
	assert.Equal(t, matchedEnd.Result.Text, n.Result)
	assert.True(t, taskRows(t, s.db)[0].notified)
}

// A watch a notice turn started starts no turn when it matches: its notice waits
// for the next question, so no chain of turns runs on past the user's send.
func TestAWatchFromANoticeTurnStartsNoTurn(t *testing.T) {
	t.Parallel()

	tt, wt := newTaskTool(), newWatchTool()
	s := startServiceWithTool(t, tt, wt)
	fakeOf(s).QueueToolCalls(nil, []llm.Block{llm.StagedCall("LogsWatch", `{}`)})
	first, ft := startTaskTurn(t, s, tt, nil, "1")
	ft.exit(0)
	w := testutil.Recv(t, wt.ready, "the notice turn's watch")
	noticeTurn := awaitNoticeTurn(t, s, first.ChatID, first.ID)
	awaitTurnReleased(t, s, noticeTurn.ChatID)
	tk := taskOf(t, s, first.ChatID)

	w.finish(matchedEnd)
	testutil.Wait(t, tk.done, "the watch's row")

	assert.Nil(t, s.turnOf(first.ChatID))
	rows := taskRows(t, s.db)
	require.Len(t, rows, 2)
	assert.Equal(t, taskMatched, rows[1].status)
	assert.False(t, rows[1].notified, "its notice waits for the next question")
}

// A watch a crash left running is lost, and its notice says so in a watch's words.
func TestALostWatchIsTold(t *testing.T) {
	t.Parallel()

	wt := newWatchTool()
	dir := t.TempDir()
	box, lists := testBox(wt)
	s := startServiceWith(t, dir, fakeLLM(), &stubClusterCards{}, box, lists)
	first, _ := startWatchTurn(t, s, wt, nil, "1")
	_, err := s.db.Write.Exec(`UPDATE background_tasks SET status = 'lost', finished_at = 1`)
	require.NoError(t, err)

	next := send(t, s, &first.ChatID, "2", "and now?")
	next = awaitSettled(t, s, next.ChatID, next.ID)

	n := noticeOfWatch(t, s, next)
	assert.Equal(t, llm.TaskLost, n.Status)
	assert.Contains(t, llm.Prompt([]llm.Block{llm.TaskNotificationBlock(n)}), `Log watch "" was lost when Kubetail stopped.`)
}

// The transcript serves a finished watch's task with its lines, why it ended and
// the view around its first line, off the row's result; a running one has none.
func TestAWatchsTaskCarriesItsLines(t *testing.T) {
	t.Parallel()

	wt := newWatchTool()
	s := startServiceWithTool(t, wt)
	first, w := startWatchTurn(t, s, wt, nil, "1")
	running := toolCallsOf(t, first)[0].Background
	require.NotNil(t, running)
	assert.Equal(t, BackgroundTaskRunning, running.Status)
	assert.Equal(t, []tools.LogsReadLine{}, running.Matched)
	tk := taskOf(t, s, first.ChatID)

	end := matchedEnd
	end.Result.ViewAround = &tools.LogsViewAction{Anchor: tools.LogsViewAnchor{Kind: tools.LogsAnchorAt, At: &end.Result.Lines[0].Timestamp}}
	w.finish(end)
	testutil.Wait(t, tk.done, "the watch's row")

	msgs, err := s.transcript(t.Context(), first.ChatID)
	require.NoError(t, err)
	task := toolCallsOf(t, msgs[1])[0].Background
	require.NotNil(t, task)
	assert.Equal(t, BackgroundTaskMatched, task.Status)
	assert.Equal(t, end.Result.Lines, task.Matched)
	assert.Equal(t, "it reached its line limit", task.EndedBecause)
	assert.Equal(t, end.Result.ViewAround, task.ViewAround)
}

// A watch starts only from a running call: its task's row hangs off the call.
func TestAWatchStartsOnlyFromARunningCall(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	for _, run := range []*runJournal{nil, {s: s, chatID: "c"}} {
		_, err := s.chatWatches("c", run).Start(newFakeWatch())
		assert.ErrorIs(t, err, errNoOpenCall)
	}
}
