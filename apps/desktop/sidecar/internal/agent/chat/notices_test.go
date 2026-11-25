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
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// A notice turn renders no card, so it carries a context block only when the
// chat's switch moved since the newest one: the model then learns where the
// turn's commands run before it runs one.
func TestANoticeTurnSaysWhereCommandsRunWhenTheSwitchMoved(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	s.sandboxState = sandbox.State{Available: true}
	first, ft := startTaskTurn(t, s, tt, nil, "1")
	sandboxed := newestContextOf(t, s, first.ChatID)

	_, err := s.SetSandboxDisabled(t.Context(), first.ChatID, true)
	require.NoError(t, err)
	ft.exit(0)
	answer := awaitNoticeTurn(t, s, first.ChatID, first.ID)

	q := questionOf(t, s, answer)
	require.Equal(t, []llm.BlockType{llm.BlockContext, llm.BlockTaskNotification}, []llm.BlockType{q[0].Type, q[1].Type})
	outside := strings.Replace(sandboxed, `{"commands":"sandboxed","network":"unavailable on this machine"}`, `{"commands":"outside"}`, 1)
	assert.Equal(t, outside, newestContextOf(t, s, first.ChatID), "the newest block with the switch swapped")
}

// So does a notice turn whose chat's network switch moved: the model learns
// what its commands reach before it runs one.
func TestANoticeTurnSaysTheNetworkWhenTheSwitchMoved(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	s.sandboxState = sandbox.State{Available: true, NetworkAvailable: true}
	first, ft := startTaskTurn(t, s, tt, nil, "1")
	off := newestContextOf(t, s, first.ChatID)

	_, err := s.SetNetworkEnabled(t.Context(), first.ChatID, true)
	require.NoError(t, err)
	ft.exit(0)
	answer := awaitNoticeTurn(t, s, first.ChatID, first.ID)

	q := questionOf(t, s, answer)
	require.Equal(t, llm.BlockContext, q[0].Type)
	on := strings.Replace(off, `"network":"off"`, `"network":"on for this chat"`, 1)
	assert.Equal(t, on, newestContextOf(t, s, first.ChatID))
}

// A notice turn whose switch is where the newest context says carries none.
func TestANoticeTurnCarriesNoContextWhenTheSwitchStayed(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	s.sandboxState = sandbox.State{Available: true}
	first, ft := startTaskTurn(t, s, tt, nil, "1")

	ft.exit(0)
	answer := awaitNoticeTurn(t, s, first.ChatID, first.ID)

	q := questionOf(t, s, answer)
	require.Len(t, q, 1)
	assert.Equal(t, llm.BlockTaskNotification, q[0].Type)
}

// A notice turn makes every check a send makes, the length check included: a
// task that exits in a chat past the ceiling starts no turn, files nothing, and
// its notice waits for a question on a model that can read the chat.
func TestAFullChatStartsNoNoticeTurn(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s, f := windowed(t, 200_000, 64_000, 0, tt)
	f.SetToolCalls(taskCall())
	f.SetUsage(reads(10), reads(150_000))
	first := converse(t, s, nil, "fake", "fake", "start it")
	ft := testutil.Recv(t, tt.ready, "the task to start")
	done := taskDoneOf(t, s, first.ChatID, ft)

	ft.exit(0)
	testutil.Wait(t, done, "the row")

	assert.Nil(t, s.turnOf(first.ChatID), "no turn started")
	msgs, err := s.transcript(t.Context(), first.ChatID)
	require.NoError(t, err)
	assert.Len(t, msgs, 2, "nothing filed")
	assert.Equal(t, 1, tableCount(t, s.db, "agent_runs"))
	assert.False(t, taskRows(t, s.db)[0].notified, "the notice waits")
}

// A notice turn whose read fails starts nothing, and its notice waits for the
// next question.
func TestANoticeTurnWhoseReadFailsStartsNothing(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, corrupt string }{
		{name: "no answer names a run", corrupt: `UPDATE messages SET run_id = NULL`},
		{name: "the newest context does not parse", corrupt: `UPDATE messages SET content = '[' WHERE role = 'user'`},
		{name: "the tool settings do not read", corrupt: `DROP TABLE settings`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tt := newTaskTool()
			s := startServiceWithTool(t, tt)
			first, ft := startTaskTurn(t, s, tt, nil, "1")
			done := taskDoneOf(t, s, first.ChatID, ft)
			_, err := s.db.Write.Exec(tc.corrupt)
			require.NoError(t, err)

			ft.exit(0)
			// The task's end kicks before done closes.
			testutil.Wait(t, done, "the row")

			assert.Nil(t, s.turnOf(first.ChatID), "no turn started")
			assert.False(t, taskRows(t, s.db)[0].notified, "the notice waits")
		})
	}
}

// A notice names its task in one short line, whatever the model wrote: the first
// line with anything on it, cut to 200 characters, the transcript's rule.
func TestANoticeHoldsOneLine(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "List files…", noticeLine("List files\nthen delete them"))
	assert.Equal(t, strings.Repeat("é", 200)+"…", noticeLine(strings.Repeat("é", 300)))
	assert.Equal(t, "List files", noticeLine("List files"))
	assert.Equal(t, "List files", noticeLine("List files  \n \n"), "no mark for whitespace alone")
	assert.Equal(t, "List files", noticeLine("\n  \nList files"))
	assert.Empty(t, noticeLine(" \n\t "))

	n := noticeOf(waitingNotice{
		id: "t1", toolUseID: "call-1", path: "/p", status: taskExited,
		description: "Serve the site\nand watch it", command: "make serve && " + strings.Repeat("x", 250),
	})
	assert.Equal(t, "Serve the site…", n.Description)
	assert.Equal(t, "make serve && "+strings.Repeat("x", 186)+"…", n.Command)
}

// An agent whose run succeeded with no report could not answer, and its notice
// says so in place of an error.
func TestAnAgentWithNothingToSayIsTold(t *testing.T) {
	t.Parallel()

	n := noticeOf(waitingNotice{id: "t1", path: "/p", status: taskFailed, agent: true, description: "look"})
	assert.Equal(t, "it had nothing to say", n.Error)
	assert.Empty(t, n.OutputFile, "a failed agent's file is empty")
}

// A stopped or lost agent's file is empty, so its notice names none.
func TestAStoppedAgentsNoticeNamesNoFile(t *testing.T) {
	t.Parallel()

	for _, status := range []string{taskStopped, taskLost} {
		n := noticeOf(waitingNotice{id: "t1", path: "/p", status: status, agent: true, description: "look"})
		assert.Empty(t, n.OutputFile, status)
		assert.Empty(t, n.Error, status)
	}
}

// A notice turn also carries the context when the chat's folders changed
// since the newest one, so the model learns what its commands now read.
func TestANoticeTurnListsTheGrantsWhenTheyChanged(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	home := grantable(t, s)
	first, ft := startTaskTurn(t, s, tt, nil, "1")
	before := newestContextOf(t, s, first.ChatID)
	code := filepath.Join(home, "code")

	require.NoError(t, s.GrantFolder(t.Context(), first.ChatID, code, false))
	ft.exit(0)
	answer := awaitNoticeTurn(t, s, first.ChatID, first.ID)

	q := questionOf(t, s, answer)
	require.Equal(t, []llm.BlockType{llm.BlockContext, llm.BlockTaskNotification}, []llm.BlockType{q[0].Type, q[1].Type})
	after := newestContextOf(t, s, first.ChatID)
	assert.Equal(t, s.withSandboxReplaced(before, sandboxState{folders: []session.Folder{{Path: code}}}), after)
	assert.Contains(t, after, `"read":[`)
}

// A grant made between a notice turn's folder read and its transaction still
// reaches the turn's context.
func TestANoticeTurnListsAGrantMadeAsItStarts(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	home := grantable(t, s)
	// The first turn is taken while the task's start holds it, and the hook set
	// once its goroutine has returned: its kick reads the hook after the slot is
	// released.
	tt.holding, tt.hold = make(chan struct{}), make(chan struct{})
	fakeOf(s).SetToolCalls(taskCall())
	first := send(t, s, nil, "1", "start it")
	testutil.Wait(t, tt.holding, "the task's start")
	turn := heldTurn(t, s, first.ChatID)
	close(tt.hold)
	ft := testutil.Recv(t, tt.ready, "the task to start")
	awaitTurnDone(t, turn)
	before := newestContextOf(t, s, first.ChatID)
	code := filepath.Join(home, "code")

	var once sync.Once
	granted := make(chan error, 1)
	s.onFoldersRead = func() {
		once.Do(func() { granted <- s.GrantFolder(t.Context(), first.ChatID, code, false) })
	}
	ft.exit(0)
	require.NoError(t, testutil.Recv(t, granted, "the grant"))
	answer := awaitNoticeTurn(t, s, first.ChatID, first.ID)

	q := questionOf(t, s, answer)
	require.Len(t, q, 2, "a context block, then the notice")
	require.Equal(t, []llm.BlockType{llm.BlockContext, llm.BlockTaskNotification}, []llm.BlockType{q[0].Type, q[1].Type})
	assert.Equal(t, s.withSandboxReplaced(before, sandboxState{folders: []session.Folder{{Path: code}}}), newestContextOf(t, s, first.ChatID))
}

// answersOf is the chat's answers so far, which a notice turn's is not.
func answersOf(t *testing.T, s *service, chatID ChatID) []MessageID {
	t.Helper()
	msgs, err := s.transcript(t.Context(), chatID)
	require.NoError(t, err)
	var ids []MessageID
	for _, m := range msgs {
		if m.Role == RoleAssistant {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

// A notice turn has no screen behind it: it re-sends the newest block without
// its View, the switch moved or not, so the model reads the view as closed.
func TestANoticeTurnDropsTheView(t *testing.T) {
	t.Parallel()

	for name, leave := range map[string]bool{"the switch stayed": false, "the switch moved": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tt := newTaskTool()
			s := startServiceWithTool(t, tt)
			s.sandboxState = sandbox.State{Available: true}
			first := send(t, s, nil, "1", "tail it")
			awaitSettled(t, s, first.ChatID, first.ID)
			action := tailWebapp()
			call := seedViewCall(t, s.db, first.ChatID, "toolu_view", &tools.Action{LogsView: &action})

			fakeOf(s).SetToolCalls(taskCall())
			msg, err := sendWithView(t, s, &first.ChatID, "2", &Snapshot{CallID: call, Lines: 1})
			require.NoError(t, err)
			ft := testutil.Recv(t, tt.ready, "the task to start")
			awaitSettled(t, s, msg.ChatID, msg.ID)
			before := newestContextOf(t, s, first.ChatID)
			require.Contains(t, before, viewHeading)

			if leave {
				_, err := s.SetSandboxDisabled(t.Context(), first.ChatID, true)
				require.NoError(t, err)
			}
			seen := answersOf(t, s, first.ChatID)
			ft.exit(0)
			answer := awaitNoticeTurn(t, s, first.ChatID, seen...)

			q := questionOf(t, s, answer)
			require.Equal(t, llm.BlockContext, q[0].Type)
			assert.Equal(t, s.withSandboxReplaced(withoutView(before), sandboxState{outside: leave}), newestContextOf(t, s, first.ChatID))
		})
	}
}

// A watch's notice carries its sample whatever its status, names no file, since
// its file is empty, and counts the lines it held; a failed watch's reason is its
// error, and a lost watch has neither.
func TestAWatchsNoticeCarriesItsSample(t *testing.T) {
	t.Parallel()

	result := &tools.WatchResult{
		Text: "Watched 2 lines.", Lines: []tools.LogsReadLine{{Message: "a"}, {Message: "b"}}, EndedBecause: "it reached its line limit",
	}
	n := noticeOf(waitingNotice{id: "t1", path: "/p", status: taskMatched, watch: true, description: "Panics", result: result})
	assert.Equal(t, llm.TaskNotice{
		Kind: llm.TaskWatch, ID: "t1", Status: llm.TaskMatched, Description: "Panics", Result: "Watched 2 lines.", Matched: 2,
	}, n)

	failed := noticeOf(waitingNotice{id: "t1", path: "/p", status: taskFailed, watch: true, description: "Panics",
		result: &tools.WatchResult{Text: "every container stopped", Lines: []tools.LogsReadLine{}, EndedBecause: "every container stopped"}})
	assert.Equal(t, "every container stopped", failed.Error)

	lost := noticeOf(waitingNotice{id: "t1", path: "/p", status: taskLost, watch: true, description: "Panics"})
	assert.Equal(t, llm.TaskNotice{Kind: llm.TaskWatch, ID: "t1", Status: llm.TaskLost, Description: "Panics"}, lost)
}

// A watch's end starts a turn only under a user's send, and only when it ended on
// its own: matched, expired or failed. A stop never does.
func TestAWatchStartsATurnOnlyUnderASend(t *testing.T) {
	t.Parallel()

	for _, status := range []string{taskMatched, taskExpired, taskFailed} {
		assert.True(t, startsTurn([]waitingNotice{{status: status, watch: true, underSend: true}}), status)
		assert.False(t, startsTurn([]waitingNotice{{status: status, watch: true}}), status)
	}
	for _, status := range []string{taskStopped, taskLost} {
		assert.False(t, startsTurn([]waitingNotice{{status: status, watch: true, underSend: true}}), status)
	}
}

// A notice turn whose folders cannot be read does not start.
func TestANoticeTurnWhoseFoldersCannotBeReadDoesNotStart(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	grantable(t, s)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	_, err := s.db.Write.Exec(`DROP TABLE folder_grants`)
	require.NoError(t, err)

	assert.ErrorContains(t, s.tryNoticeTurn(t.Context(), c.ID), "folder_grants")
}

// A notice turn whose grant's check cannot read the PATH does not start
// either: a failed read is not a refused grant.
func TestANoticeTurnWhoseGrantCheckCannotReadThePathDoesNotStart(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	svc := filepath.Join(grantable(t, s), "code", "svc")
	c := seedChat(t, s.db, aChat("1", time.Now()))
	seedFolderGrant(t, s.db, c.ID, "stored", svc, "write")
	_, err := s.db.Write.Exec(`DROP TABLE shell_path`)
	require.NoError(t, err)

	assert.ErrorContains(t, s.tryNoticeTurn(t.Context(), c.ID), "shell")
}

// A chat switched outside the sandbox between a notice turn's folder read and
// its transaction reads again, and its turn takes no folders.
func TestANoticeTurnRereadsAfterTheSwitchMoves(t *testing.T) {
	t.Parallel()

	tt := newTaskTool()
	s := startServiceWithTool(t, tt)
	home := grantable(t, s)
	tt.holding, tt.hold = make(chan struct{}), make(chan struct{})
	fakeOf(s).SetToolCalls(taskCall())
	first := send(t, s, nil, "1", "start it")
	testutil.Wait(t, tt.holding, "the task's start")
	turn := heldTurn(t, s, first.ChatID)
	close(tt.hold)
	ft := testutil.Recv(t, tt.ready, "the task to start")
	awaitTurnDone(t, turn)
	require.NoError(t, s.GrantFolder(t.Context(), first.ChatID, filepath.Join(home, "code"), false))

	var once sync.Once
	switched := make(chan error, 1)
	s.onFoldersRead = func() {
		once.Do(func() {
			_, err := s.SetSandboxDisabled(t.Context(), first.ChatID, true)
			switched <- err
		})
	}
	ft.exit(0)
	require.NoError(t, testutil.Recv(t, switched, "the switch"))
	answer := awaitNoticeTurn(t, s, first.ChatID, first.ID)

	assert.NotContains(t, newestContextOf(t, s, answer.ChatID), filepath.Join(home, "code"), "no folders outside the sandbox")
}

// A turn outside the sandbox takes no folders, so a notice turn there starts
// though they cannot be read.
func TestANoticeTurnOutsideTheSandboxReadsNoFolders(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	grantable(t, s)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	_, err := s.SetSandboxDisabled(t.Context(), c.ID, true)
	require.NoError(t, err)
	_, err = s.db.Write.Exec(`DROP TABLE folder_grants`)
	require.NoError(t, err)

	assert.ErrorIs(t, s.tryNoticeTurn(t.Context(), c.ID), errNothingToTell)
	assert.ErrorIs(t, s.tryNoticeTurn(t.Context(), ChatID(appdb.NewID())), ErrChatGone)
}
