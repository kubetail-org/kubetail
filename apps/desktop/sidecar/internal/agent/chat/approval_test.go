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
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// toolCallsOf is a message's list as the wire serves it.
func toolCallsOf(t *testing.T, m ChatMessage) []ToolCall {
	t.Helper()
	var out []ToolCall
	require.NoError(t, json.Unmarshal([]byte(m.ToolCalls), &out))
	return out
}

// awaitToolCall watches chatID until message id's first call has status, and
// returns the message as the watch served it.
func awaitToolCall(t *testing.T, s *service, chatID ChatID, id MessageID, status ToolCallStatus) ChatMessage {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	st, err := s.WatchMessageCollection(ctx, chatID)
	require.NoError(t, err)
	f := awaitFrame(t, st.Frames, func(f ChatMessageCollectionWatchFrame) bool {
		if f.Message == nil || f.Message.ID != id {
			return false
		}
		var calls []ToolCall
		return json.Unmarshal([]byte(f.Message.ToolCalls), &calls) == nil && len(calls) > 0 && calls[0].Status == status
	})
	return *f.Message
}

// awaitRequest waits for the message's first call to be put to the user, and
// returns the message and the approval to decide.
func awaitRequest(t *testing.T, s *service, msg ChatMessage) (ChatMessage, ApprovalID) {
	t.Helper()
	got := awaitToolCall(t, s, msg.ChatID, msg.ID, ToolCallAwaitingApproval)
	calls := toolCallsOf(t, got)
	require.NotNil(t, calls[0].Approval)
	return got, calls[0].Approval.ID
}

// approve decides id and requires that a turn was waiting on it.
func approve(t *testing.T, s *service, id ApprovalID, yes bool) {
	t.Helper()
	d := DecisionDeny
	if yes {
		d = DecisionOnce
	}
	ok, err := s.Approve(t.Context(), id, d)
	require.NoError(t, err)
	require.True(t, ok, "a turn was waiting on the approval")
}

// approvalRow is what one approvals row stored.
type approvalRow struct {
	status  ApprovalStatus
	decided bool
}

func approvalRows(t *testing.T, db *appdb.DB) []approvalRow {
	t.Helper()
	rows, err := db.Read.Query(`SELECT status, decided_at FROM approvals ORDER BY created_at, id`)
	require.NoError(t, err)
	defer rows.Close()
	var out []approvalRow
	for rows.Next() {
		var (
			r       approvalRow
			decided sql.NullInt64
		)
		require.NoError(t, rows.Scan(&r.status, &decided))
		r.decided = decided.Valid
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

// isMutating is a tool call row's is_mutating, by its tool_use_id.
func isMutating(t *testing.T, db *appdb.DB, useID string) bool {
	t.Helper()
	var v bool
	require.NoError(t, db.Read.QueryRow(`SELECT is_mutating FROM tool_calls WHERE tool_use_id = ?`, useID).Scan(&v))
	return v
}

// skipTool is a gated tool whose every approval skips.
type skipTool struct{ testTool }

func (skipTool) Approval(context.Context, tools.Runtime, json.RawMessage) (tools.Approval, error) {
	return tools.Approval{Skip: true}, nil
}

// A gated call whose approval skips runs with nothing asked, and its row is an
// ungated call's: no approval, is_mutating 0, running then succeeded.
func TestASkippedApprovalRunsUnasked(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, skipTool{testTool{name: "Read"}})
	fakeOf(s).SetToolCalls(llm.StagedCall("Read", `{"file_path":"/x"}`))

	msg := send(t, s, nil, "1", "hi")
	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, StatusComplete, got.Status)
	calls := toolCallsOf(t, got)
	require.Len(t, calls, 1)
	assert.Nil(t, calls[0].Approval)
	assert.Equal(t, ToolCallSucceeded, calls[0].Status)
	assert.Empty(t, approvalRows(t, s.db))
	assert.False(t, isMutating(t, s.db, "call-1"))
	rows := toolCallRows(t, s.db, msg.RunID)
	require.Len(t, rows, 1)
	assert.True(t, rows[0].hasStarted)
}

// A command is a committed row and an approval before anything runs: the row
// awaiting_approval with no started_at, the approval pending, the run and the
// message waiting, and the list carrying the approval id beside the exact
// command and the description shown with it.
func TestACommandWaitsOnTheUser(t *testing.T) {
	t.Parallel()

	sh := &fakeBash{}
	s := startServiceWithTool(t, sh)
	command := `kubectl get pods > out && cat out | grep "é"`
	fakeOf(s).SetToolCalls(describedBashCall(command, "List pods\nand grep them"))

	msg := send(t, s, nil, "1", "hi")
	got, _ := awaitRequest(t, s, msg)

	assert.Equal(t, StatusWaitingApproval, got.Status)
	calls := toolCallsOf(t, got)
	require.NotNil(t, calls[0].Action)
	assert.Equal(t, command, calls[0].Action.Command.Text, "the request is the command byte for byte")
	assert.Equal(t, "List pods\nand grep them", calls[0].Action.Description)
	assert.Equal(t, ApprovalPending, calls[0].Approval.Status)
	assert.Equal(t, "Bash", calls[0].Name)
	ran := sh.commands()
	assert.Empty(t, ran, "nothing ran")
	rows := toolCallRows(t, s.db, msg.RunID)
	require.Len(t, rows, 1)
	assert.Equal(t, toolAwaitingApproval, rows[0].status)
	assert.False(t, rows[0].hasStarted)
	assert.True(t, isMutating(t, s.db, "call-1"))
	assert.Equal(t, []approvalRow{{status: ApprovalPending}}, approvalRows(t, s.db))
	assert.Equal(t, RunWaitingApproval, runStatusOf(t, s.db, msg.RunID))
}

// A background command's request says so, on the live list and the stored read
// alike.
func TestABackgroundApprovalSaysSo(t *testing.T) {
	t.Parallel()

	sh := &fakeBash{}
	s := startServiceWithTool(t, sh)
	fakeOf(s).SetToolCalls(llm.StagedCall("Bash", `{"command":"make serve","run_in_background":true}`))

	msg := send(t, s, nil, "1", "hi")
	got, _ := awaitRequest(t, s, msg)

	assert.True(t, toolCallsOf(t, got)[0].Action.Command.Background, "the live list")
	stored, err := s.transcript(t.Context(), msg.ChatID)
	require.NoError(t, err)
	assert.True(t, toolCallsOf(t, stored[1])[0].Action.Command.Background, "the stored read")
}

// The call's first write is awaiting_approval, with its approval; nothing writes
// a pending row first.
func TestACommandRowStartsAwaiting(t *testing.T) {
	t.Parallel()

	sh := &fakeBash{}
	s := startServiceWithTool(t, sh)
	_, err := s.db.Write.Exec(`CREATE TABLE first_status (status TEXT);
		CREATE TRIGGER log_insert AFTER INSERT ON tool_calls BEGIN INSERT INTO first_status VALUES (NEW.status); END`)
	require.NoError(t, err)
	fakeOf(s).SetToolCalls(bashCall("ls"))

	msg := send(t, s, nil, "1", "hi")
	_, id := awaitRequest(t, s, msg)
	approve(t, s, id, true)
	awaitSettled(t, s, msg.ChatID, msg.ID)

	var first []string
	rows, err := s.db.Read.Query(`SELECT status FROM first_status`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var st string
		require.NoError(t, rows.Scan(&st))
		first = append(first, st)
	}
	assert.Equal(t, []string{toolAwaitingApproval}, first)
}

// A decision that arrives before the turn reaches its wait is kept for it, and a
// second decision finds no one waiting.
func TestAFastDecisionFindsItsWaiter(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	id := newApprovalID()
	w := s.await(id, "", nil)

	ok, err := s.Approve(t.Context(), id, DecisionOnce)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, decision{status: ApprovalApproved, duration: permissions.DurationOnce}, <-w.decided)

	ok, err = s.Approve(t.Context(), id, DecisionDeny)
	require.NoError(t, err)
	assert.False(t, ok, "a second decision finds no waiter")
	ok, err = s.Approve(t.Context(), newApprovalID(), DecisionOnce)
	require.NoError(t, err)
	assert.False(t, ok, "nor does an id nobody minted")
}

// Approved, the command is shown running, then done, with what the model read.
func TestAnApprovedCommandIsShownRunningThenDone(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	sh := &fakeBash{run: func(context.Context, string) (string, bool) {
		<-release
		return "three pods\n", false
	}}
	s := startServiceWithTool(t, sh)
	fakeOf(s).SetToolCalls(bashCall("ls"))

	msg := send(t, s, nil, "1", "hi")
	_, id := awaitRequest(t, s, msg)
	approve(t, s, id, true)
	running := awaitToolCall(t, s, msg.ChatID, msg.ID, ToolCallRunning)
	assert.Equal(t, ApprovalApproved, toolCallsOf(t, running)[0].Approval.Status)
	close(release)
	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	calls := toolCallsOf(t, got)
	assert.Equal(t, ToolCallSucceeded, calls[0].Status)
	assert.Equal(t, "three pods\n", calls[0].Output)
	assert.Equal(t, "ls", calls[0].Action.Command.Text)
	rows := toolCallRows(t, s.db, msg.RunID)
	assert.Equal(t, toolSucceeded, rows[0].status)
	assert.True(t, rows[0].hasStarted)
	assert.Equal(t, []approvalRow{{status: ApprovalApproved, decided: true}}, approvalRows(t, s.db))
}

// Denied, nothing runs: the model is answered denied, the row is denied with no
// started_at, and the approval says what the user decided.
func TestADeniedCommandDoesNotRun(t *testing.T) {
	t.Parallel()

	sh := &fakeBash{}
	s := startServiceWithTool(t, sh)
	fakeOf(s).SetToolCalls(bashCall("rm -rf ~"))

	msg := send(t, s, nil, "1", "hi")
	_, id := awaitRequest(t, s, msg)
	approve(t, s, id, false)
	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, StatusComplete, got.Status)
	ran := sh.commands()
	assert.Empty(t, ran)
	calls := toolCallsOf(t, got)
	assert.Equal(t, ToolCallDenied, calls[0].Status)
	assert.Equal(t, `{"error":"denied"}`, calls[0].Output)
	rows := toolCallRows(t, s.db, msg.RunID)
	assert.Equal(t, toolDenied, rows[0].status)
	assert.False(t, rows[0].hasStarted)
	assert.Equal(t, []approvalRow{{status: ApprovalDenied, decided: true}}, approvalRows(t, s.db))
}

// The decision flips the run and the message back from waiting while the command
// runs.
func TestTheRunFlipsBackOnTheDecision(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	sh := &fakeBash{run: func(context.Context, string) (string, bool) {
		<-release
		return "ok", false
	}}
	s := startServiceWithTool(t, sh)
	fakeOf(s).SetToolCalls(bashCall("ls"))

	msg := send(t, s, nil, "1", "hi")
	_, id := awaitRequest(t, s, msg)
	approve(t, s, id, true)
	running := awaitToolCall(t, s, msg.ChatID, msg.ID, ToolCallRunning)

	assert.Equal(t, StatusStreaming, running.Status)
	assert.Equal(t, RunRunning, runStatusOf(t, s.db, msg.RunID))
	close(release)
	awaitSettled(t, s, msg.ChatID, msg.ID)
}

// A bash call the budget refused asks no one and is still listed, not run.
func TestARefusedBashCallIsStillListed(t *testing.T) {
	t.Parallel()

	sh := &fakeBash{}
	s := startServiceWithTool(t, sh)
	calls := make([]llm.Block, MaxToolCalls+1)
	for i := range calls {
		calls[i] = bashCall("ls")
	}
	fakeOf(s).SetToolCalls(calls...)

	msg := send(t, s, nil, "1", "hi")
	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	listed := toolCallsOf(t, got)
	require.Len(t, listed, MaxToolCalls+1)
	for _, c := range listed {
		assert.Equal(t, ToolCallNotRun, c.Status)
		assert.Equal(t, `{"error":"budget"}`, c.Output)
	}
	assert.Empty(t, approvalRows(t, s.db))
}

// A cancel while the request is up runs nothing: the call is answered cancelled
// with no started_at, and a decision that arrives after finds no one waiting.
func TestACancelledWaitRunsNothing(t *testing.T) {
	t.Parallel()

	sh := &fakeBash{}
	s := startServiceWithTool(t, sh)
	fakeOf(s).SetToolCalls(bashCall("ls"))

	msg := send(t, s, nil, "1", "hi")
	_, id := awaitRequest(t, s, msg)
	require.NoError(t, s.Cancel(t.Context(), msg.ChatID))
	got := awaitSettled(t, s, msg.ChatID, msg.ID)
	awaitTurnReleased(t, s, msg.ChatID)

	assert.Equal(t, StatusCancelled, got.Status)
	ran := sh.commands()
	assert.Empty(t, ran)
	assert.Equal(t, ToolCallNotRun, toolCallsOf(t, got)[0].Status)
	ok, err := s.Approve(t.Context(), id, DecisionOnce)
	require.NoError(t, err)
	assert.False(t, ok, "a late decision changes nothing")
	assert.Equal(t, []approvalRow{{status: ApprovalPending}}, approvalRows(t, s.db))
}

// A cancel while an approved command runs kills it: the row keeps started_at and
// reads as interrupted, since it may have run.
func TestACancelledRunIsInterrupted(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	sh := &fakeBash{run: func(ctx context.Context, _ string) (string, bool) {
		close(started)
		<-ctx.Done()
		return "killed", true
	}}
	s := startServiceWithTool(t, sh)
	fakeOf(s).SetToolCalls(bashCall("sleep 60"))

	msg := send(t, s, nil, "1", "hi")
	_, id := awaitRequest(t, s, msg)
	approve(t, s, id, true)
	<-started
	require.NoError(t, s.Cancel(t.Context(), msg.ChatID))
	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, ToolCallInterrupted, toolCallsOf(t, got)[0].Status)
	rows := toolCallRows(t, s.db, msg.RunID)
	assert.True(t, rows[0].hasStarted)
	assert.JSONEq(t, `{"error":"cancelled"}`, rows[0].errText)
}

// A cancel observed before the decision leaves the record of an unanswered
// question: the approval pending and undecided, the call not run, the run cancelled.
func TestACancelBeforeTheDecisionCommitsLeavesItPending(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, &fakeBash{})
	fakeOf(s).SetToolCalls(bashCall("ls"))

	msg := send(t, s, nil, "1", "hi")
	awaitRequest(t, s, msg)
	require.NoError(t, s.Cancel(t.Context(), msg.ChatID))
	awaitSettled(t, s, msg.ChatID, msg.ID)
	awaitTurnReleased(t, s, msg.ChatID)

	assert.Equal(t, []approvalRow{{status: ApprovalPending}}, approvalRows(t, s.db))
	rows := toolCallRows(t, s.db, msg.RunID)
	assert.Equal(t, toolFailed, rows[0].status)
	assert.False(t, rows[0].hasStarted)
	assert.Equal(t, RunCancelled, runStatusOf(t, s.db, msg.RunID))
	s.turnsMu.Lock()
	assert.Empty(t, s.pending, "the waiter was taken back")
	s.turnsMu.Unlock()
}

// Once the decision is taken it commits and stands, and a cancel landing as it is
// written stops the command before it starts: the user approved, and nothing ran.
func TestACancelAfterTheDecisionCommitsKeepsItAndRunsNothing(t *testing.T) {
	t.Parallel()

	sh := &fakeBash{}
	s := startServiceWithTool(t, sh)
	fakeOf(s).SetToolCalls(bashCall("ls"))
	var (
		mu    sync.Mutex
		armed func()
	)
	// The decision's time is read after the wait took it and before its write, so a
	// cancel fired there lands while the write commits.
	s.now = func() time.Time {
		mu.Lock()
		fire := armed
		armed = nil
		mu.Unlock()
		if fire != nil {
			fire()
		}
		return time.Now()
	}

	msg := send(t, s, nil, "1", "hi")
	_, id := awaitRequest(t, s, msg)
	mu.Lock()
	armed = func() { _ = s.Cancel(context.Background(), msg.ChatID) }
	mu.Unlock()
	approve(t, s, id, true)
	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	ran := sh.commands()
	assert.Empty(t, ran)
	assert.Equal(t, []approvalRow{{status: ApprovalApproved, decided: true}}, approvalRows(t, s.db))
	calls := toolCallsOf(t, got)
	assert.Equal(t, ToolCallNotRun, calls[0].Status, "the command did not run")
	assert.Equal(t, ApprovalApproved, calls[0].Approval.Status, "and the user approved it")
	rows := toolCallRows(t, s.db, msg.RunID)
	assert.Equal(t, toolFailed, rows[0].status)
	assert.False(t, rows[0].hasStarted)
	assert.JSONEq(t, `{"error":"cancelled"}`, rows[0].errText)
}

// A request that cannot be written is never shown and never runs: the call is
// refused not-run and the turn fails.
func TestAFailedApprovalWriteRefusesTheCommand(t *testing.T) {
	t.Parallel()

	sh := &fakeBash{}
	s := startServiceWithTool(t, sh)
	_, err := s.db.Write.Exec(`CREATE TRIGGER refuse BEFORE INSERT ON approvals BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)
	fakeOf(s).SetToolCalls(bashCall("ls"))

	msg := send(t, s, nil, "1", "hi")
	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, StatusFailed, got.Status)
	ran := sh.commands()
	assert.Empty(t, ran)
	assert.Empty(t, approvalRows(t, s.db))
	rows := toolCallRows(t, s.db, msg.RunID)
	require.Len(t, rows, 1)
	assert.JSONEq(t, `{"error":"not-run"}`, rows[0].errText)
	assert.False(t, rows[0].hasStarted)
	s.turnsMu.Lock()
	assert.Empty(t, s.pending, "the waiter was taken back")
	s.turnsMu.Unlock()
}

// A decision the store refuses ends the turn with the store's error; the approval
// stays pending, the question recorded and its answer not, and nothing runs.
func TestTheCommandWritesReportWhatTheStoreRefuses(t *testing.T) {
	t.Parallel()

	sh := &fakeBash{}
	s := startServiceWithTool(t, sh)
	_, err := s.db.Write.Exec(`CREATE TRIGGER refuse BEFORE UPDATE ON approvals WHEN NEW.status != 'pending'
		BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)
	fakeOf(s).SetToolCalls(bashCall("ls"))

	msg := send(t, s, nil, "1", "hi")
	_, id := awaitRequest(t, s, msg)
	approve(t, s, id, true)
	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, StatusFailed, got.Status)
	assert.Contains(t, got.Error, "refused")
	ran := sh.commands()
	assert.Empty(t, ran)
	assert.Equal(t, []approvalRow{{status: ApprovalPending}}, approvalRows(t, s.db))
	assert.Equal(t, ToolCallNotRun, toolCallsOf(t, got)[0].Status)
}

// An approved command whose running write fails never starts: its row is closed
// not-run with no started_at, and the shell is never called.
func TestAFailedStartWriteLeavesTheCommandNotRun(t *testing.T) {
	t.Parallel()

	sh := &fakeBash{}
	s := startServiceWithTool(t, sh)
	_, err := s.db.Write.Exec(`CREATE TRIGGER refuse BEFORE UPDATE ON tool_calls WHEN NEW.status = 'running'
		BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)
	fakeOf(s).SetToolCalls(bashCall("ls"))

	msg := send(t, s, nil, "1", "hi")
	_, id := awaitRequest(t, s, msg)
	approve(t, s, id, true)
	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	ran := sh.commands()
	assert.Empty(t, ran)
	assert.Equal(t, ToolCallNotRun, toolCallsOf(t, got)[0].Status)
	rows := toolCallRows(t, s.db, msg.RunID)
	require.Len(t, rows, 1)
	assert.False(t, rows[0].hasStarted)
	assert.JSONEq(t, `{"error":"not-run"}`, rows[0].errText)
}

// Whichever way a gated call ends, it is one row under its seq with its approval
// still joined.
func TestEveryEndOfAGatedCallClosesItsOneRow(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		trigger string
		end     func(t *testing.T, s *service, msg ChatMessage, id ApprovalID)
	}{
		"denied": {end: func(t *testing.T, s *service, _ ChatMessage, id ApprovalID) { approve(t, s, id, false) }},
		"cancelled while waiting": {end: func(t *testing.T, s *service, msg ChatMessage, _ ApprovalID) {
			require.NoError(t, s.Cancel(t.Context(), msg.ChatID))
		}},
		"a failed decision write": {
			trigger: `CREATE TRIGGER refuse BEFORE UPDATE ON approvals WHEN NEW.status != 'pending' BEGIN SELECT RAISE(ABORT, 'refused'); END`,
			end:     func(t *testing.T, s *service, _ ChatMessage, id ApprovalID) { approve(t, s, id, true) },
		},
		"a failed start write": {
			trigger: `CREATE TRIGGER refuse BEFORE UPDATE ON tool_calls WHEN NEW.status = 'running' BEGIN SELECT RAISE(ABORT, 'refused'); END`,
			end:     func(t *testing.T, s *service, _ ChatMessage, id ApprovalID) { approve(t, s, id, true) },
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := startServiceWithTool(t, &fakeBash{})
			if tc.trigger != "" {
				_, err := s.db.Write.Exec(tc.trigger)
				require.NoError(t, err)
			}
			fakeOf(s).SetToolCalls(bashCall("ls"))
			msg := send(t, s, nil, "1", "hi")
			_, id := awaitRequest(t, s, msg)
			tc.end(t, s, msg, id)
			awaitSettled(t, s, msg.ChatID, msg.ID)
			awaitTurnReleased(t, s, msg.ChatID)

			rows := toolCallRows(t, s.db, msg.RunID)
			require.Len(t, rows, 1)
			assert.Equal(t, 0, rows[0].seq)
			assert.True(t, rows[0].hasFinished)
			var joined int
			require.NoError(t, s.db.Read.QueryRow(`SELECT COUNT(*) FROM approvals a JOIN tool_calls t ON t.id = a.tool_call_id`).Scan(&joined))
			assert.Equal(t, 1, joined)
		})
	}
}

// A call queued behind a command whose wait was cancelled is answered cancelled
// and never asked about, let alone run.
func TestACallQueuedBehindACancelledWaitRunsNothing(t *testing.T) {
	t.Parallel()

	sh := &fakeBash{}
	s := startServiceWithTool(t, sh)
	fakeOf(s).SetToolCalls(bashCall("ls"), bashCall("rm -rf /tmp/x"))

	msg := send(t, s, nil, "1", "hi")
	awaitRequest(t, s, msg)
	require.NoError(t, s.Cancel(t.Context(), msg.ChatID))
	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Empty(t, sh.commands())
	calls := toolCallsOf(t, got)
	require.Len(t, calls, 2)
	assert.Equal(t, ToolCallNotRun, calls[1].Status)
	assert.Equal(t, `{"error":"cancelled"}`, calls[1].Output)
	assert.Nil(t, calls[1].Approval, "the second command was never put to the user")
	assert.Len(t, approvalRows(t, s.db), 1)
}

// seedWait files a turn stopped on a command, as a process that died with the
// request up leaves it: the run waiting, the call's row awaiting_approval and
// its approval pending. content is the answer's stored content.
func seedWait(t *testing.T, db *appdb.DB, chatID ChatID, command, content string) (seededTurn, ApprovalID) {
	t.Helper()
	now := time.UnixMilli(1_000).UTC()
	turn := seedTurn(t, db, chatID, now)
	setRunStatus(t, db, turn.Run, RunWaitingApproval)
	_, err := db.Write.Exec(`UPDATE messages SET content = ? WHERE id = ?`, content, string(turn.Assistant))
	require.NoError(t, err)
	call, row, id := appdb.NewID(), appdb.NewID(), newApprovalID()
	args, _ := json.Marshal(map[string]string{"command": command})
	_, err = db.Write.Exec(`INSERT INTO llm_calls (id, run_id, seq, provider, model, started_at) VALUES (?, ?, 0, 'fake', 'fake', 0)`, call, string(turn.Run))
	require.NoError(t, err)
	_, err = db.Write.Exec(`INSERT INTO tool_calls (id, llm_call_id, seq, tool_name, contract_name, tool_use_id, arguments, is_mutating, status, created_at)
		VALUES (?, ?, 0, 'Bash', 'bash/v1', 'toolu_1', ?, 1, 'awaiting_approval', 0)`, row, call, string(args))
	require.NoError(t, err)
	_, err = db.Write.Exec(`INSERT INTO approvals (id, tool_call_id, status, created_at) VALUES (?, ?, 'pending', 0)`, string(id), row)
	require.NoError(t, err)
	return turn, id
}

// The directory a command runs in is on its request: in the tool_calls row, on
// the live list, and on the stored read once the turn has settled.
func TestTheToolCallRowHoldsTheCwd(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, &fakeBash{})
	// Absolute on every OS; the request reads it without touching the disk.
	workdir := filepath.Join(t.TempDir(), "app")
	input, _ := json.Marshal(map[string]string{"command": "ls", "workdir": workdir})
	fakeOf(s).SetToolCalls(llm.StagedCall("Bash", string(input)))

	msg := send(t, s, nil, "1", "hi")
	got, id := awaitRequest(t, s, msg)
	assert.Equal(t, workdir, toolCallsOf(t, got)[0].Action.Command.Cwd)
	var cwd string
	require.NoError(t, s.db.Read.QueryRow(`SELECT cwd FROM tool_calls`).Scan(&cwd))
	assert.Equal(t, workdir, cwd)

	approve(t, s, id, true)
	awaitSettled(t, s, msg.ChatID, msg.ID)
	awaitTurnReleased(t, s, msg.ChatID)
	msgs, err := s.readMessages(t.Context(), msg.ChatID)
	require.NoError(t, err)
	assert.Equal(t, workdir, toolCallsOf(t, msgs[1])[0].Action.Command.Cwd)
	require.NoError(t, s.db.Read.QueryRow(`SELECT cwd FROM tool_calls`).Scan(&cwd))
	assert.Equal(t, workdir, cwd, "the settle's rewrite keeps it")
}

// bashShownEnv is fakeBash whose approval carries the user's variables, keeping
// the ones its run was handed.
type bashShownEnv struct {
	*fakeBash
	env    []string
	ranEnv chan []string
}

func (b bashShownEnv) Approval(ctx context.Context, rt tools.Runtime, input json.RawMessage) (tools.Approval, error) {
	a, err := b.fakeBash.Approval(ctx, rt, input)
	a.Env = b.env
	return a, err
}

func (b bashShownEnv) RunApproved(ctx context.Context, rt tools.Runtime, input json.RawMessage, a tools.Approval) (string, bool) {
	b.ranEnv <- a.Env
	return b.Run(ctx, rt, input)
}

// The variables a command outside the sandbox gets are on its request: in the
// tool_calls row, on the live list, handed to the run, and on the stored read
// once the turn has settled. A call shown none reads none.
func TestTheToolCallRowHoldsTheVariablesItWasShownWith(t *testing.T) {
	t.Parallel()

	env := []string{"HELM_NAMESPACE=apps", "NOTE=two\nlines"}
	tool := bashShownEnv{fakeBash: &fakeBash{}, env: env, ranEnv: make(chan []string, 1)}
	s := startServiceWithTool(t, tool)
	fakeOf(s).SetToolCalls(bashCall("helm list"))

	msg := send(t, s, nil, "1", "hi")
	got, id := awaitRequest(t, s, msg)
	assert.Equal(t, env, toolCallsOf(t, got)[0].Action.Command.Env)

	approve(t, s, id, true)
	assert.Equal(t, env, testutil.Recv(t, tool.ranEnv, "the run"))
	awaitSettled(t, s, msg.ChatID, msg.ID)
	awaitTurnReleased(t, s, msg.ChatID)
	msgs, err := s.readMessages(t.Context(), msg.ChatID)
	require.NoError(t, err)
	assert.Equal(t, env, toolCallsOf(t, msgs[1])[0].Action.Command.Env, "the settle's rewrite keeps it")

	row := toolCallEntry{Name: "Bash", Contract: string(s.tools.ContractOf("Bash")), Arguments: `{"command":"ls"}`}
	assert.Empty(t, actionOf(s.tools, row).Command.Env)
	row.Env = "{"
	assert.Empty(t, actionOf(s.tools, row).Command.Env, "a row whose variables do not read shows none")
}

// approvingTool is a testTool gated by a fixed approval.
type approvingTool struct {
	testTool
	approval tools.Approval
}

func (a approvingTool) Approval(context.Context, tools.Runtime, json.RawMessage) (tools.Approval, error) {
	return a.approval, nil
}

// cwdAndSandboxed is the one tool_calls row's cwd and sandboxed.
func cwdAndSandboxed(t *testing.T, s *service) (string, bool) {
	t.Helper()
	var cwd string
	var sandboxed bool
	require.NoError(t, s.db.Read.QueryRow(`SELECT cwd, sandboxed FROM tool_calls`).Scan(&cwd, &sandboxed))
	return cwd, sandboxed
}

// Whether a sandbox confined a call is on its row beside the cwd, from the first
// write through the settle, whether the call asked or skipped the question.
func TestASandboxedCallIsRecordedAsSandboxed(t *testing.T) {
	t.Parallel()

	for _, skip := range []bool{false, true} {
		var s *service
		var whileRunning struct {
			cwd       string
			sandboxed bool
		}
		tool := approvingTool{
			testTool: testTool{name: "sbx", run: func(context.Context, json.RawMessage) (string, bool) {
				whileRunning.cwd, whileRunning.sandboxed = cwdAndSandboxed(t, s)
				return "ran", false
			}},
			approval: tools.Approval{Cwd: "/work", Sandboxed: true, Skip: skip},
		}
		s = startServiceWithTool(t, tool)
		fakeOf(s).SetToolCalls(llm.StagedCall("sbx", `{}`))

		msg := send(t, s, nil, "1", "hi")
		if !skip {
			_, id := awaitRequest(t, s, msg)
			cwd, sandboxed := cwdAndSandboxed(t, s)
			assert.Equal(t, "/work", cwd)
			assert.True(t, sandboxed, "the request's row")
			approve(t, s, id, true)
		}
		awaitSettled(t, s, msg.ChatID, msg.ID)
		awaitTurnReleased(t, s, msg.ChatID)

		assert.Equal(t, "/work", whileRunning.cwd, "skip=%v", skip)
		assert.True(t, whileRunning.sandboxed, "the running row, skip=%v", skip)
		cwd, sandboxed := cwdAndSandboxed(t, s)
		assert.Equal(t, "/work", cwd, "skip=%v", skip)
		assert.True(t, sandboxed, "the settle's rewrite keeps it, skip=%v", skip)
	}
}

// A command that never ran reads back off its row once the turn has settled and
// the overlay is gone: its request, and that it did not run.
func TestAStoredReadKeepsAnUnrunCommand(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, &fakeBash{})
	fakeOf(s).SetToolCalls(bashCall("kubectl delete pod web-0"))

	msg := send(t, s, nil, "1", "hi")
	awaitRequest(t, s, msg)
	require.NoError(t, s.Cancel(t.Context(), msg.ChatID))
	awaitSettled(t, s, msg.ChatID, msg.ID)
	awaitTurnReleased(t, s, msg.ChatID)

	msgs, err := s.readMessages(t.Context(), msg.ChatID)
	require.NoError(t, err)
	calls := toolCallsOf(t, msgs[1])
	require.Len(t, calls, 1)
	assert.Equal(t, "kubectl delete pod web-0", calls[0].Action.Command.Text)
	assert.Equal(t, ToolCallNotRun, calls[0].Status)
	assert.Equal(t, ApprovalPending, calls[0].Approval.Status, "a question nobody answered")
}

// A wait the process died in is failed at the next start: the row closed stranded
// with no started_at, the approval left pending, and a decision on it finds no one.
func TestStartupFailsAStrandedWait(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	db := openTestDB(t, dir)
	c := seedChat(t, db, aChat("1", time.UnixMilli(1_000).UTC()))
	turn, id := seedWait(t, db, c.ID, "ls", `[]`)
	require.NoError(t, db.Close())

	s := startService(t, dir)

	assert.Equal(t, RunFailed, runStatusOf(t, s.db, turn.Run))
	rows := toolCallRows(t, s.db, turn.Run)
	require.Len(t, rows, 1)
	assert.Equal(t, toolCallStranded, rows[0].errText)
	assert.False(t, rows[0].hasStarted)
	assert.Equal(t, []approvalRow{{status: ApprovalPending}}, approvalRows(t, s.db))
	ok, err := s.Approve(t.Context(), id, DecisionOnce)
	require.NoError(t, err)
	assert.False(t, ok)
	msgs, err := s.readMessages(t.Context(), c.ID)
	require.NoError(t, err)
	assert.Equal(t, ToolCallNotRun, toolCallsOf(t, msgs[1])[0].Status)
}

// The transcript reads a command off its row, never off the content, so a command
// whose tool_use never reached the stored content is still shown.
func TestACommandSurvivesACrashBeforeItsCheckpoint(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	db := openTestDB(t, dir)
	c := seedChat(t, db, aChat("1", time.UnixMilli(1_000).UTC()))
	seedWait(t, db, c.ID, "rm -rf /tmp/x", `[]`)
	require.NoError(t, db.Close())

	s := startService(t, dir)

	msgs, err := s.readMessages(t.Context(), c.ID)
	require.NoError(t, err)
	assert.Equal(t, emptyContent, msgs[1].Content)
	calls := toolCallsOf(t, msgs[1])
	require.Len(t, calls, 1)
	assert.Equal(t, "rm -rf /tmp/x", calls[0].Action.Command.Text)
	assert.JSONEq(t, `{"command":"rm -rf /tmp/x"}`, string(calls[0].Arguments))
}

// A command whose own output spells a refusal ran and succeeded: the row is
// judged by whether the result is an error, never by what its text happens to say.
func TestACommandPrintingARefusalStillSucceeded(t *testing.T) {
	t.Parallel()

	sh := &fakeBash{run: func(context.Context, string) (string, bool) { return "{\"error\":\"denied\"}\n", false }}
	s := startServiceWithTool(t, sh)
	fakeOf(s).SetToolCalls(bashCall(`echo '{"error":"denied"}'`))

	msg := send(t, s, nil, "1", "hi")
	_, id := awaitRequest(t, s, msg)
	approve(t, s, id, true)
	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	calls := toolCallsOf(t, got)
	assert.Equal(t, ToolCallSucceeded, calls[0].Status)
	assert.False(t, calls[0].IsError)
	rows := toolCallRows(t, s.db, msg.RunID)
	assert.Equal(t, toolSucceeded, rows[0].status)
	assert.True(t, rows[0].hasStarted)
}

// A Bash call that never reached the gate still shows what it asked for, with
// no directory: one cancelled behind another's wait, and one on a box with no
// shell, which the loop refuses unknown-tool.
func TestAnUngatedBashCallShowsItsCommand(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, &fakeBash{})
	fakeOf(s).SetToolCalls(bashCall("ls"), describedBashCall("rm -rf /tmp/x", "Clear the scratch"))
	msg := send(t, s, nil, "1", "hi")
	awaitRequest(t, s, msg)
	require.NoError(t, s.Cancel(t.Context(), msg.ChatID))
	awaitSettled(t, s, msg.ChatID, msg.ID)
	awaitTurnReleased(t, s, msg.ChatID)
	msgs, err := s.readMessages(t.Context(), msg.ChatID)
	require.NoError(t, err)
	queued := toolCallsOf(t, msgs[1])[1]
	assert.Nil(t, queued.Approval)
	assert.Equal(t, &tools.Action{
		Description: "Clear the scratch",
		Command:     &tools.CommandAction{Text: "rm -rf /tmp/x"},
	}, queued.Action)

	s = startServiceWithTool(t, testTool{name: "echo"})
	fakeOf(s).SetToolCalls(describedBashCall("kubectl get pods", "List pods"))
	msg = send(t, s, nil, "1", "hi")
	got := awaitSettled(t, s, msg.ChatID, msg.ID)
	unknown := toolCallsOf(t, got)[0]
	assert.Equal(t, `{"error":"unknown-tool"}`, unknown.Output)
	assert.Equal(t, &tools.Action{
		Description: "List pods",
		Command:     &tools.CommandAction{Text: "kubectl get pods"},
	}, unknown.Action)
}

// A message awaits approval while its own run waits on the user, on the live
// answer, and not once the decision is in.
func TestAWaitingRequestMarksItsMessage(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, &fakeBash{})
	fakeOf(s).SetToolCalls(bashCall("kubectl get pods"))

	msg := send(t, s, nil, "1", "hi")
	waiting, id := awaitRequest(t, s, msg)
	assert.True(t, waiting.AwaitingApproval)

	approve(t, s, id, true)
	settled := awaitSettled(t, s, msg.ChatID, msg.ID)
	assert.False(t, settled.AwaitingApproval)
}

// A settled answer awaits approval while a run under it waits on the user: the
// stored read looks at the subagents' runs too.
func TestASubagentWaitingMarksItsSettledAnswer(t *testing.T) {
	t.Parallel()

	now := time.UnixMilli(1_000).UTC()
	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", now))
	turn := seedTurn(t, s.db, c.ID, now)
	settleSeededRun(t, s.db, turn.Run, RunSucceeded, now)
	sub := appdb.NewID()
	_, err := s.db.Write.Exec(`INSERT INTO agent_runs (id, parent_run_id, agent_type, app_version, trigger, chat_id, provider, model, dialect, task, status, created_at)
		VALUES (?, ?, 'general-purpose', 'test', 'agent', ?, 'fake', 'fake', 'fake', 'p', 'waiting_approval', 0)`, sub, string(turn.Run), string(c.ID))
	require.NoError(t, err)

	msgs, err := s.readMessages(t.Context(), c.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.False(t, msgs[0].AwaitingApproval, "a question has no run")
	assert.True(t, msgs[1].AwaitingApproval)
	assert.Equal(t, StatusComplete, msgs[1].Status, "the answer's status stays its own run's")

	setRunStatus(t, s.db, RunID(sub), RunRunning)
	msgs, err = s.readMessages(t.Context(), c.ID)
	require.NoError(t, err)
	assert.False(t, msgs[1].AwaitingApproval)
}

// An agent's request left unanswered past the bound stops the agent,
// stopped_by unanswered: its slot is freed, the request can no longer be
// decided, and its notice rides the next question.
func TestAnUnansweredRequestStopsItsAgent(t *testing.T) {
	t.Parallel()

	sh := &fakeBash{}
	s := startServiceWithAgent(t, sh)
	s.unansweredLimit = time.Millisecond
	fakeOf(s).SetToolCalls(agentCall("List the pods."))
	subagentFake(s, "List the pods.").SetToolCalls(bashCall("kubectl get pods"))

	msg := send(t, s, nil, "k", "which pods?")
	awaitSettled(t, s, msg.ChatID, msg.ID)
	awaitAgentOf(t, s, msg.ChatID, msg.RunID)

	row := taskRows(t, s.db)[0]
	assert.Equal(t, taskStopped, row.status)
	assert.Equal(t, sql.NullString{String: stoppedByUnanswered, Valid: true}, row.stoppedBy)
	assert.False(t, row.notified)
	assert.Empty(t, sh.commands())
	assert.Equal(t, []approvalRow{{status: ApprovalPending}}, approvalRows(t, s.db), "the record of a question nobody answered")
	var id ApprovalID
	require.NoError(t, s.db.Read.QueryRow(`SELECT id FROM approvals`).Scan(&id))
	ok, err := s.Approve(t.Context(), id, DecisionOnce)
	require.NoError(t, err)
	assert.False(t, ok, "nothing waits on it")
}

// The parent's own request is not bounded: it holds the turn, which the user
// sees and can cancel.
func TestTheParentsOwnRequestWaits(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, &fakeBash{})
	s.unansweredLimit = time.Millisecond
	fakeOf(s).SetToolCalls(bashCall("kubectl get pods"))

	msg := send(t, s, nil, "k", "which pods?")
	awaitRequest(t, s, msg)
	turn := s.turnOf(msg.ChatID)
	require.NotNil(t, turn)

	// A negative assertion has no event to wait on: fifty times the bound, which a
	// bounded wait would have long passed.
	select {
	case <-turn.done:
		t.Fatal("the parent's request was given up")
	case <-time.After(50 * time.Millisecond):
	}
}

// A chat with an agent waiting on the user is marked on the list, which a watch
// hears, and the mark clears when the agent is stopped.
func TestAWaitingAgentMarksItsChat(t *testing.T) {
	t.Parallel()

	s := startServiceWithAgent(t, &fakeBash{})
	fakeOf(s).SetToolCalls(agentCall("List the pods."))
	subagentFake(s, "List the pods.").SetToolCalls(bashCall("kubectl get pods"))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	list, err := s.WatchCollection(ctx)
	require.NoError(t, err)

	msg := send(t, s, nil, "k", "which pods?")
	awaitFrame(t, list.Frames, func(f ChatCollectionWatchFrame) bool {
		return f.Chat != nil && f.Chat.ID == msg.ChatID && f.Chat.AwaitingApproval
	})
	awaitSettled(t, s, msg.ChatID, msg.ID)
	awaitCall(t, s, msg.ChatID, msg.ID, "Bash", ToolCallAwaitingApproval)

	_, err = s.StopBackgroundTask(t.Context(), ToolCallID(taskRows(t, s.db)[0].toolCallID))
	require.NoError(t, err)
	awaitFrame(t, list.Frames, func(f ChatCollectionWatchFrame) bool {
		return f.Chat != nil && f.Chat.ID == msg.ChatID && !f.Chat.AwaitingApproval
	})
}

// A chat a restart left waiting is not marked: the start fails the run, and a
// watch opened before it hears the mark clear.
func TestAStrandedWaitClearsTheChatsMark(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	db := openTestDB(t, dir)
	c := seedChat(t, db, aChat("1", time.UnixMilli(1_000).UTC()))
	turn := seedTurn(t, db, c.ID, time.UnixMilli(1_000).UTC())
	setRunStatus(t, db, turn.Run, RunWaitingApproval)
	require.NoError(t, db.Close())

	s, err := newService(openTestDB(t, dir), chatsDirIn(dir), monitorDirIn(dir), fakeLLM(), noClusterCards, nil, testReaders, noLists, sandbox.State{}, testSettings(t), nil)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	list, err := s.WatchCollection(ctx)
	require.NoError(t, err)
	awaitFrame(t, list.Frames, func(f ChatCollectionWatchFrame) bool { return f.Chat != nil && f.Chat.AwaitingApproval })
	startPrepared(t, s)

	awaitFrame(t, list.Frames, func(f ChatCollectionWatchFrame) bool {
		return f.Chat != nil && f.Chat.ID == c.ID && !f.Chat.AwaitingApproval
	})
}

// deleteX is the action the chat tests' writer asks.
var deleteX = tools.ActionRequest{
	Action: permissions.Action{
		Principal: permissions.Chat, Target: permissions.Kubernetes, AccessLevel: permissions.Write, Scope: "1/web", AccountName: "dev",
		Operation: "delete", Resource: "pods", Name: "x", Summary: "Delete pods/x in web on dev",
	},
	Grantable:   true,
	CommandRule: "Allow delete of core pods in dev / web for this command",
	ChatRule:    "Allow writes in dev / web",
	Write: &tools.ClusterWrite{
		Method: "DELETE", Path: "/api/v1/namespaces/web/pods/x?dryRun=All", ContentType: "application/json",
		Body: `{"propagationPolicy":"Background"}`,
	},
}

// writerTool is a tool whose run puts one cluster write to the user through its
// runtime, as Bash's grant does, and answers with the decision. hold, when set,
// keeps it running after the answer until the test closes it; ctx, when set,
// is the write's context in place of the call's; request, when set, is asked
// in place of deleteX; after, when set, runs once the answer has arrived.
type writerTool struct {
	testTool
	hold    chan struct{}
	ctx     context.Context
	request *tools.ActionRequest
	after   func()
}

func (w writerTool) Run(ctx context.Context, rt tools.Runtime, _ json.RawMessage) (string, bool) {
	if rt.ActionAsker == nil {
		return "nobody to ask", true
	}
	if w.ctx != nil {
		ctx = w.ctx
	}
	request := deleteX
	if w.request != nil {
		request = *w.request
	}
	answer, err := rt.ActionAsker.Ask(ctx, request)
	if w.after != nil {
		w.after()
	}
	if w.hold != nil {
		<-w.hold
	}
	if err != nil {
		return "unanswered", true
	}
	return fmt.Sprintf("%v %s", answer.Approved, answer.Duration), false
}

// startWriter is a started service offering w as Writer, and the model calling
// it once.
func startWriter(t *testing.T, w writerTool) *service {
	t.Helper()
	w.name = "Writer"
	s := startServiceWithTool(t, w)
	fakeOf(s).SetToolCalls(llm.StagedCall("Writer", `{}`))
	return s
}

// awaitWrite watches the message until its first call's last write has status,
// and returns the message as the watch served it.
func awaitWrite(t *testing.T, s *service, msg ChatMessage, status ApprovalStatus) ChatMessage {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	st, err := s.WatchMessageCollection(ctx, msg.ChatID)
	require.NoError(t, err)
	f := awaitFrame(t, st.Frames, func(f ChatMessageCollectionWatchFrame) bool {
		if f.Message == nil || f.Message.ID != msg.ID {
			return false
		}
		var calls []ToolCall
		if json.Unmarshal([]byte(f.Message.ToolCalls), &calls) != nil || len(calls) == 0 {
			return false
		}
		writes := calls[0].SandboxActions
		return len(writes) > 0 && writes[len(writes)-1].Approval.Status == status
	})
	return *f.Message
}

// A write is asked under the call that is running: a cluster approval on
// it, the run waiting and then running again once decided, and the call running
// throughout. While it waits the list carries the write whole; once decided,
// its method and path alone.
func TestAWriteIsAskedUnderTheRunningCall(t *testing.T) {
	t.Parallel()

	hold := make(chan struct{})
	s := startWriter(t, writerTool{hold: hold})

	msg := send(t, s, nil, "1", "hi")
	got := awaitWrite(t, s, msg, ApprovalPending)

	assert.Equal(t, StatusWaitingApproval, got.Status)
	calls := toolCallsOf(t, got)
	require.Len(t, calls, 1)
	assert.Equal(t, ToolCallRunning, calls[0].Status)
	assert.Nil(t, calls[0].Approval, "the call itself was not asked about")
	require.Len(t, calls[0].SandboxActions, 1)
	w := calls[0].SandboxActions[0]
	assert.Equal(t, sandboxActionOf(ToolCallApproval{ID: w.Approval.ID, Status: ApprovalPending}, &deleteX), w)
	assert.Equal(t, RunWaitingApproval, runStatusOf(t, s.db, msg.RunID))
	assert.Equal(t, []approvalRow{{status: ApprovalPending}}, approvalRows(t, s.db))

	approve(t, s, w.Approval.ID, true)
	got = awaitWrite(t, s, msg, ApprovalApproved)
	assert.Equal(t, RunRunning, runStatusOf(t, s.db, msg.RunID))
	assert.Equal(t, StatusStreaming, got.Status)
	w = toolCallsOf(t, got)[0].SandboxActions[0]
	once := permissions.DurationOnce
	decided := sandboxActionOf(ToolCallApproval{ID: w.Approval.ID, Status: ApprovalApproved, Duration: &once}, &deleteX)
	decided.Write.ContentType, decided.Write.Body = "", ""
	assert.Equal(t, decided, w)
	assert.Equal(t, []approvalRow{{status: ApprovalApproved, decided: true}}, approvalRows(t, s.db))

	close(hold)
	settled := awaitSettled(t, s, msg.ChatID, msg.ID)
	calls = toolCallsOf(t, settled)
	assert.Equal(t, "true once", calls[0].Output)
	assert.Equal(t, ToolCallSucceeded, calls[0].Status)
	assert.Equal(t, ApprovalApproved, calls[0].SandboxActions[0].Approval.Status)
}

// A denied write answers false to the command, which runs on.
func TestADeniedWriteAnswersFalse(t *testing.T) {
	t.Parallel()

	s := startWriter(t, writerTool{})

	msg := send(t, s, nil, "1", "hi")
	got := awaitWrite(t, s, msg, ApprovalPending)
	approve(t, s, toolCallsOf(t, got)[0].SandboxActions[0].Approval.ID, false)
	settled := awaitSettled(t, s, msg.ChatID, msg.ID)

	calls := toolCallsOf(t, settled)
	assert.Equal(t, "false ", calls[0].Output)
	assert.Equal(t, ApprovalDenied, calls[0].SandboxActions[0].Approval.Status)
}

// A write's wait that ends with its request, while the run goes on, writes it
// abandoned, flips the run back to running and publishes; a later decision on
// it reaches no one.
func TestAWriteWaitEndsWithTheRequest(t *testing.T) {
	t.Parallel()

	ctx, drop := context.WithCancel(t.Context())
	hold := make(chan struct{})
	s := startWriter(t, writerTool{hold: hold, ctx: ctx})

	msg := send(t, s, nil, "1", "hi")
	id := toolCallsOf(t, awaitWrite(t, s, msg, ApprovalPending))[0].SandboxActions[0].Approval.ID
	drop()
	got := awaitWrite(t, s, msg, ApprovalAbandoned)

	assert.Equal(t, StatusStreaming, got.Status)
	assert.Equal(t, RunRunning, runStatusOf(t, s.db, msg.RunID))
	assert.Equal(t, []approvalRow{{status: ApprovalAbandoned, decided: true}}, approvalRows(t, s.db))
	ok, err := s.Approve(t.Context(), id, DecisionOnce)
	require.NoError(t, err)
	assert.False(t, ok, "a decision after the wait ended reaches no one")
	close(hold)
	settled := awaitSettled(t, s, msg.ChatID, msg.ID)
	assert.Equal(t, "unanswered", toolCallsOf(t, settled)[0].Output)
}

// Cancelling the run ends a write's wait: the write is abandoned, and the run
// settles cancelled.
func TestAWriteWaitEndsWithTheRun(t *testing.T) {
	t.Parallel()

	s := startWriter(t, writerTool{})

	msg := send(t, s, nil, "1", "hi")
	awaitWrite(t, s, msg, ApprovalPending)
	require.NoError(t, s.Cancel(t.Context(), msg.ChatID))
	settled := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, StatusCancelled, settled.Status)
	assert.Equal(t, ApprovalAbandoned, toolCallsOf(t, settled)[0].SandboxActions[0].Approval.Status)
	assert.Equal(t, []approvalRow{{status: ApprovalAbandoned, decided: true}}, approvalRows(t, s.db))
}

// The settle writes each write's approval again from what the turn holds, so a
// decision whose row was lost is healed.
func TestTheSettleRewritesTheWrites(t *testing.T) {
	t.Parallel()

	hold := make(chan struct{})
	s := startWriter(t, writerTool{hold: hold})

	msg := send(t, s, nil, "1", "hi")
	id := toolCallsOf(t, awaitWrite(t, s, msg, ApprovalPending))[0].SandboxActions[0].Approval.ID
	approve(t, s, id, false)
	awaitWrite(t, s, msg, ApprovalDenied)
	_, err := s.db.Write.Exec(`UPDATE approvals SET status = 'pending', decided_at = NULL WHERE id = ?`, string(id))
	require.NoError(t, err)
	close(hold)
	awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, []approvalRow{{status: ApprovalDenied, decided: true}}, approvalRows(t, s.db))
}

// A subagent asks its writes as its own, under its own run: past the bound its
// write is abandoned and the agent stopped, stopped_by unanswered.
func TestASubagentsWriteIsBoundedByItsLimit(t *testing.T) {
	t.Parallel()

	s := startServiceWithAgent(t, writerTool{testTool: testTool{name: "Writer"}})
	s.unansweredLimit = time.Millisecond
	fakeOf(s).SetToolCalls(agentCall("Delete the pod."))
	subagentFake(s, "Delete the pod.").SetToolCalls(llm.StagedCall("Writer", `{}`))

	msg := send(t, s, nil, "k", "delete pod x")
	awaitSettled(t, s, msg.ChatID, msg.ID)
	awaitAgentOf(t, s, msg.ChatID, msg.RunID)

	row := taskRows(t, s.db)[0]
	assert.Equal(t, taskStopped, row.status)
	assert.Equal(t, sql.NullString{String: stoppedByUnanswered, Valid: true}, row.stoppedBy)
	assert.Equal(t, []approvalRow{{status: ApprovalAbandoned, decided: true}}, approvalRows(t, s.db))
}

// A write whose request cannot be written is never shown: the command reads
// the wait ended, and nothing waits on its id.
func TestAWriteThatCannotBeRecordedIsNotAsked(t *testing.T) {
	t.Parallel()

	s := startWriter(t, writerTool{})
	_, err := s.db.Write.Exec(`CREATE TRIGGER refuse BEFORE INSERT ON approvals BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)

	msg := send(t, s, nil, "1", "hi")
	settled := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, "unanswered", toolCallsOf(t, settled)[0].Output)
	assert.Empty(t, approvalRows(t, s.db))
	s.turnsMu.Lock()
	defer s.turnsMu.Unlock()
	assert.Empty(t, s.pending, "no waiter is left behind")
}

// A decision whose write fails answers the command with an error, so no change
// goes on a decision the record does not hold. The request still comes down,
// since the command runs on, and the settle writes the decision.
func TestADecisionThatCannotBeRecordedForwardsNothing(t *testing.T) {
	t.Parallel()

	hold := make(chan struct{})
	s := startWriter(t, writerTool{hold: hold})
	_, err := s.db.Write.Exec(`CREATE TRIGGER refuse BEFORE UPDATE ON approvals WHEN NEW.status != 'pending'
		BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)

	msg := send(t, s, nil, "1", "hi")
	id := toolCallsOf(t, awaitWrite(t, s, msg, ApprovalPending))[0].SandboxActions[0].Approval.ID
	approve(t, s, id, true)
	got := awaitWrite(t, s, msg, ApprovalApproved)
	assert.Equal(t, StatusStreaming, got.Status)
	assert.Equal(t, []approvalRow{{status: ApprovalPending}}, approvalRows(t, s.db))

	_, err = s.db.Write.Exec(`DROP TRIGGER refuse`)
	require.NoError(t, err)
	close(hold)
	settled := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, "unanswered", toolCallsOf(t, settled)[0].Output)
	assert.Equal(t, []approvalRow{{status: ApprovalApproved, decided: true}}, approvalRows(t, s.db))
	assert.Equal(t, RunSucceeded, runStatusOf(t, s.db, msg.RunID))
}

// A write asked while no call of the run is running is refused.
func TestAWriteWithNoRunningCallIsRefused(t *testing.T) {
	t.Parallel()

	j := &runJournal{s: newTestService(t)}

	answer, err := j.askAction(t.Context(), deleteX)

	assert.False(t, answer.Approved)
	assert.ErrorIs(t, err, errNoRunningCall)
}

// recorderTool is a tool whose run records two cluster writes decided with
// nobody asked, as Bash's grant does under a mode or a rule, then holds until
// the test closes hold.
type recorderTool struct {
	testTool
	hold chan struct{}
}

// allowedPatch is the action a recorderTool records allowed.
var allowedPatch = tools.ActionRequest{
	Action: permissions.Action{
		Principal: permissions.Chat, Target: permissions.Kubernetes, AccessLevel: permissions.Write, Scope: "1/web", AccountName: "dev",
		Operation: "patch", Resource: "configmaps", Name: "c", Summary: "Patch configmaps/c in web on dev",
	},
	Write: &tools.ClusterWrite{
		Method: "PATCH", Path: "/api/v1/namespaces/web/configmaps/c", ContentType: "application/merge-patch+json", Body: `{}`,
	},
}

func (r recorderTool) Run(ctx context.Context, rt tools.Runtime, _ json.RawMessage) (string, bool) {
	err := rt.ActionAsker.Record(ctx, allowedPatch, permissions.Allowed, "auto mode")
	if err == nil {
		err = rt.ActionAsker.Record(ctx, deleteX, permissions.Denied, "this cluster is read-only")
	}
	<-r.hold
	if err != nil {
		return err.Error(), true
	}
	return "recorded", false
}

// A write decided with nobody asked is recorded against the open call with its
// reason, with no wait: the run stays streaming.
func TestARecordedWriteNeedsNoWait(t *testing.T) {
	t.Parallel()

	hold := make(chan struct{})
	s := startServiceWithTool(t, recorderTool{testTool: testTool{name: "Recorder"}, hold: hold})
	fakeOf(s).SetToolCalls(llm.StagedCall("Recorder", `{}`))

	msg := send(t, s, nil, "1", "hi")
	got := awaitWrite(t, s, msg, ApprovalRefused)

	assert.Equal(t, StatusStreaming, got.Status)
	writes := toolCallsOf(t, got)[0].SandboxActions
	require.Len(t, writes, 2)
	assert.Equal(t, ApprovalAllowed, writes[0].Approval.Status)
	assert.Equal(t, "auto mode", *writes[0].Reason)
	assert.Empty(t, writes[0].Write.Body, "a recorded write waits on nobody, so its body is not served")
	assert.Equal(t, ApprovalRefused, writes[1].Approval.Status)
	assert.Equal(t, "this cluster is read-only", *writes[1].Reason)
	assert.Equal(t, []approvalRow{{status: ApprovalAllowed, decided: true}, {status: ApprovalRefused, decided: true}}, approvalRows(t, s.db))
	assert.Equal(t, RunRunning, runStatusOf(t, s.db, msg.RunID))

	close(hold)
	settled := awaitSettled(t, s, msg.ChatID, msg.ID)
	assert.Equal(t, "recorded", toolCallsOf(t, settled)[0].Output)
	assert.Equal(t, "auto mode", *toolCallsOf(t, settled)[0].SandboxActions[0].Reason, "the stored read serves the reason")
}

// A write recorded while no call of the run is running is refused.
func TestARecordWithNoRunningCallIsRefused(t *testing.T) {
	t.Parallel()

	j := &runJournal{s: newTestService(t)}

	err := j.recordAction(t.Context(), deleteX, permissions.Allowed, "auto mode")

	assert.ErrorIs(t, err, errNoRunningCall)
}

// Only an allowed or denied write is recorded, so a prompt passed by mistake
// is refused rather than recorded as allowed.
func TestARecordOfAPromptIsRefused(t *testing.T) {
	t.Parallel()

	j := &runJournal{s: newTestService(t), openTool: &toolCallEntry{}}

	err := j.recordAction(t.Context(), deleteX, permissions.Prompted, "ask mode")

	assert.ErrorIs(t, err, errNotRecorded)
	assert.Empty(t, approvalRows(t, j.s.db))
}

// A record the store refuses is the store's error, and the open call carries no
// write the record does not hold.
func TestARecordTheStoreRefusesIsNotCarried(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	refuse(t, s, "INSERT", "approvals")
	call := &toolCallEntry{ID: ToolCallID(appdb.NewID())}
	j := &runJournal{s: s, openTool: call}

	err := j.recordAction(t.Context(), deleteX, permissions.Allowed, "auto mode")

	assert.ErrorContains(t, err, "refused")
	assert.Empty(t, call.SandboxActions)
}

// A decision the request does not name is refused, whatever the request.
func TestAnUnknownDecisionIsRefused(t *testing.T) {
	t.Parallel()

	for _, request := range []*tools.ActionRequest{nil, {Grantable: true}} {
		_, err := (&waiter{request: request}).decide("sometimes")
		assert.ErrorIs(t, err, ErrBadRequest)
	}
}

// A call whose own first row cannot be written is never asked: nothing runs,
// the call is closed not-run, and nothing waits on a request nobody can see.
func TestACallWhoseRowCannotBeWrittenIsNotAsked(t *testing.T) {
	t.Parallel()

	sh := &fakeBash{}
	s := startServiceWithTool(t, sh)
	_, err := s.db.Write.Exec(`CREATE TRIGGER refuse BEFORE INSERT ON tool_calls WHEN NEW.status = 'awaiting_approval'
		BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)
	fakeOf(s).SetToolCalls(bashCall("ls"))

	msg := send(t, s, nil, "1", "hi")
	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, StatusFailed, got.Status)
	assert.Empty(t, sh.commands())
	assert.Empty(t, approvalRows(t, s.db))
	rows := toolCallRows(t, s.db, msg.RunID)
	require.Len(t, rows, 1)
	assert.JSONEq(t, `{"error":"not-run"}`, rows[0].errText)
	s.turnsMu.Lock()
	assert.Empty(t, s.pending)
	s.turnsMu.Unlock()
}

// actionRequests are the kind and the stored request of every approval, in
// the order written.
func actionRequests(t *testing.T, db *appdb.DB) (kinds []string, requests []tools.ActionRequest) {
	t.Helper()
	rows, err := db.Read.Query(`SELECT kind, request FROM approvals ORDER BY created_at, id`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var (
			kind    string
			request sql.NullString
			r       tools.ActionRequest
		)
		require.NoError(t, rows.Scan(&kind, &request))
		require.NoError(t, json.Unmarshal([]byte(request.String), &r))
		kinds, requests = append(kinds, kind), append(requests, r)
	}
	require.NoError(t, rows.Err())
	return kinds, requests
}

// An asked action's row holds its request, the action included, and so do an
// allowed and a refused one's.
func TestAnActionIsRecordedWithItsRequest(t *testing.T) {
	t.Parallel()

	s := startWriter(t, writerTool{})
	msg := send(t, s, nil, "1", "hi")
	approve(t, s, toolCallsOf(t, awaitWrite(t, s, msg, ApprovalPending))[0].SandboxActions[0].Approval.ID, false)
	awaitSettled(t, s, msg.ChatID, msg.ID)
	kinds, requests := actionRequests(t, s.db)
	assert.Equal(t, []string{"action"}, kinds)
	assert.Equal(t, []tools.ActionRequest{deleteX}, requests)

	hold := make(chan struct{})
	s = startServiceWithTool(t, recorderTool{testTool: testTool{name: "Recorder"}, hold: hold})
	fakeOf(s).SetToolCalls(llm.StagedCall("Recorder", `{}`))
	msg = send(t, s, nil, "1", "hi")
	awaitWrite(t, s, msg, ApprovalRefused)
	close(hold)
	awaitSettled(t, s, msg.ChatID, msg.ID)
	kinds, requests = actionRequests(t, s.db)
	assert.Equal(t, []string{"action", "action"}, kinds)
	assert.Equal(t, []tools.ActionRequest{allowedPatch, deleteX}, requests)
}

// twoRequestsTool is a tool whose run asks deleteX on one goroutine and, once
// the test closes second, sends allowedPatch, or then when set, on another:
// asked, or recorded allowed when record is set. Each result goes to done.
type twoRequestsTool struct {
	testTool
	second chan struct{}
	record bool
	then   *tools.ActionRequest
	done   chan error
}

func (w twoRequestsTool) Run(ctx context.Context, rt tools.Runtime, _ json.RawMessage) (string, bool) {
	go func() {
		_, err := rt.ActionAsker.Ask(ctx, deleteX)
		w.done <- err
	}()
	<-w.second
	next := allowedPatch
	if w.then != nil {
		next = *w.then
	}
	go func() {
		if w.record {
			w.done <- rt.ActionAsker.Record(ctx, next, permissions.Allowed, "auto mode")
			return
		}
		_, err := rt.ActionAsker.Ask(ctx, next)
		w.done <- err
	}()
	<-w.done
	<-w.done
	return "done", false
}

// startTwoRequests is a started service whose model calls a twoRequestsTool
// once, and a channel of the frames of the answer whose call holds n actions.
func startTwoRequests(t *testing.T, w twoRequestsTool) (*service, ChatMessage, func(n int) <-chan ChatMessage) {
	t.Helper()
	w.name = "Twice"
	s := startServiceWithTool(t, w)
	fakeOf(s).SetToolCalls(llm.StagedCall("Twice", `{}`))
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	msg := send(t, s, nil, "1", "hi")
	st, err := s.WatchMessageCollection(ctx, msg.ChatID)
	require.NoError(t, err)
	withN := func(n int) <-chan ChatMessage {
		out := make(chan ChatMessage, 1)
		go func() {
			for f := range st.Frames {
				if f.Message == nil || f.Message.ID != msg.ID {
					continue
				}
				var calls []ToolCall
				if json.Unmarshal([]byte(f.Message.ToolCalls), &calls) == nil && len(calls) > 0 && len(calls[0].SandboxActions) == n {
					out <- *f.Message
					return
				}
			}
		}()
		return out
	}
	return s, msg, withN
}

// Two asks of one run reach the journal one at a time: the second's row is
// written only once the first is answered.
func TestAsksTakeTheJournalOneAtATime(t *testing.T) {
	t.Parallel()

	second := make(chan struct{})
	s, msg, withN := startTwoRequests(t, twoRequestsTool{second: second, done: make(chan error, 2)})

	first := testutil.Recv(t, withN(1), "the first request")
	two := withN(2)
	close(second)
	// A negative assertion: the second ask must not write while the first waits.
	testutil.NoRecv(t, two, 200*time.Millisecond, "a second request while the first waits")

	approve(t, s, toolCallsOf(t, first)[0].SandboxActions[0].Approval.ID, false)
	both := testutil.Recv(t, two, "the second request once the first is answered")
	approve(t, s, toolCallsOf(t, both)[0].SandboxActions[1].Approval.ID, false)
	awaitSettled(t, s, msg.ChatID, msg.ID)
}

// A cluster request and a cloud platform's asked together are opened one after
// the other, the second once the first is answered, in one list.
func TestTwoProxiesAskOneAtATime(t *testing.T) {
	t.Parallel()

	cloud := tools.ActionRequest{
		Action: permissions.Action{Target: permissions.AWS, AccessLevel: permissions.Write, Once: true, Summary: "AWS: call ec2:X"},
		Cloud:  &tools.CloudRequest{Host: "ec2.amazonaws.com", Method: "POST", Path: "/"},
	}
	second := make(chan struct{})
	s, msg, withN := startTwoRequests(t, twoRequestsTool{second: second, then: &cloud, done: make(chan error, 2)})

	first := testutil.Recv(t, withN(1), "the cluster request")
	two := withN(2)
	close(second)
	// A negative assertion: the cloud platform's ask must not open while the
	// cluster's waits.
	testutil.NoRecv(t, two, 200*time.Millisecond, "the cloud platform's request while the cluster's waits")

	approve(t, s, toolCallsOf(t, first)[0].SandboxActions[0].Approval.ID, false)
	both := testutil.Recv(t, two, "the cloud platform's request once the first is answered")
	actions := toolCallsOf(t, both)[0].SandboxActions
	assert.NotNil(t, actions[0].Write)
	require.NotNil(t, actions[1].Cloud)
	assert.Equal(t, "ec2.amazonaws.com", actions[1].Cloud.Host)
	approve(t, s, actions[1].Approval.ID, false)
	awaitSettled(t, s, msg.ChatID, msg.ID)
}

// A record lands while an ask waits on the user, and the answer still says it
// waits.
func TestARecordNeverWaitsBehindAnAsk(t *testing.T) {
	t.Parallel()

	second := make(chan struct{})
	done := make(chan error, 2)
	s, msg, withN := startTwoRequests(t, twoRequestsTool{second: second, record: true, done: done})

	first := testutil.Recv(t, withN(1), "the first request")
	two := withN(2)
	close(second)
	got := testutil.Recv(t, two, "the record while the ask waits")
	assert.Equal(t, StatusWaitingApproval, got.Status)
	assert.Equal(t, ApprovalAllowed, toolCallsOf(t, got)[0].SandboxActions[1].Approval.Status)

	approve(t, s, toolCallsOf(t, first)[0].SandboxActions[0].Approval.ID, false)
	awaitSettled(t, s, msg.ChatID, msg.ID)
}

// durations are every approval's status and duration, in the order written.
func durations(t *testing.T, db *appdb.DB) map[ApprovalStatus][]string {
	t.Helper()
	rows, err := db.Read.Query(`SELECT status, COALESCE(duration, '') FROM approvals ORDER BY created_at, id`)
	require.NoError(t, err)
	defer rows.Close()
	out := map[ApprovalStatus][]string{}
	for rows.Next() {
		var (
			status   ApprovalStatus
			duration string
		)
		require.NoError(t, rows.Scan(&status, &duration))
		out[status] = append(out[status], duration)
	}
	require.NoError(t, rows.Err())
	return out
}

// askOnce starts a writer asking deleteX, decides it d, and waits for the
// answer to settle.
func askOnce(t *testing.T, d ApprovalDecision) (*service, ChatMessage) {
	t.Helper()
	s := startWriter(t, writerTool{})
	msg := send(t, s, nil, "1", "hi")
	id := toolCallsOf(t, awaitWrite(t, s, msg, ApprovalPending))[0].SandboxActions[0].Approval.ID
	ok, err := s.Approve(t.Context(), id, d)
	require.NoError(t, err)
	require.True(t, ok)
	return s, awaitSettled(t, s, msg.ChatID, msg.ID)
}

// Once approves the request alone: approved with once, and no rule anywhere.
func TestOnceWritesTheDurationAndNoRule(t *testing.T) {
	t.Parallel()

	s, msg := askOnce(t, DecisionOnce)

	assert.Equal(t, "true once", toolCallsOf(t, msg)[0].Output)
	assert.Equal(t, map[ApprovalStatus][]string{ApprovalApproved: {"once"}}, durations(t, s.db))
	assert.Empty(t, s.chatRules(t.Context(), msg.ChatID))
	assert.Empty(t, storedAlwaysRules(t, s))
}

// A denial is nobody's choice of how long: it records no duration.
func TestADenialRecordsNoDuration(t *testing.T) {
	t.Parallel()

	s, msg := askOnce(t, DecisionDeny)

	assert.Equal(t, "false ", toolCallsOf(t, msg)[0].Output)
	assert.Equal(t, map[ApprovalStatus][]string{ApprovalDenied: {""}}, durations(t, s.db))
}

// A call's own request, a raw command's, takes Once or Deny alone: an allow
// answer is refused and the waiter stays, so Once then lands.
func TestACallsOwnApprovalTakesOnceOrDenyAlone(t *testing.T) {
	t.Parallel()

	sh := &fakeBash{}
	s := startServiceWithTool(t, sh)
	fakeOf(s).SetToolCalls(bashCall("ls"))
	msg := send(t, s, nil, "1", "hi")
	_, id := awaitRequest(t, s, msg)

	for _, d := range []ApprovalDecision{DecisionCommand, DecisionChat, DecisionAlways} {
		ok, err := s.Approve(t.Context(), id, d)
		assert.ErrorIs(t, err, ErrBadRequest, d)
		assert.False(t, ok)
	}
	approve(t, s, id, true)
	awaitSettled(t, s, msg.ChatID, msg.ID)
	assert.Equal(t, []string{"ls"}, sh.commands())
}

// An action no rule may allow takes Once or Deny alone, as a raw command does.
func TestAnUngrantableActionTakesOnceOrDenyAlone(t *testing.T) {
	t.Parallel()

	destructive := deleteX
	destructive.Action.AccessLevel, destructive.Grantable = permissions.Destructive, false
	destructive.CommandRule, destructive.ChatRule = "", ""
	s := startWriter(t, writerTool{request: &destructive})
	msg := send(t, s, nil, "1", "hi")
	id := toolCallsOf(t, awaitWrite(t, s, msg, ApprovalPending))[0].SandboxActions[0].Approval.ID

	for _, d := range []ApprovalDecision{DecisionCommand, DecisionChat, DecisionAlways} {
		ok, err := s.Approve(t.Context(), id, d)
		assert.ErrorIs(t, err, ErrBadRequest, d)
		assert.False(t, ok)
	}
	approve(t, s, id, true)
	settled := awaitSettled(t, s, msg.ChatID, msg.ID)
	assert.Equal(t, "true once", toolCallsOf(t, settled)[0].Output)
}

// awsStart is a grantable AWS change, as the proxy asks it.
var awsStart = tools.ActionRequest{
	Action: permissions.Action{
		Principal: permissions.Chat, Target: permissions.AWS, AccessLevel: permissions.Write, Scope: "123456789012/eu-west-1",
		AccountName: "123456789012", Service: "ec2", Operation: "StartInstances", Summary: "AWS: call ec2:StartInstances",
	},
	Grantable:   true,
	CommandRule: `Allow StartInstances of ec2 "" in AWS 123456789012 / eu-west-1 for this command`,
	ChatRule:    "Allow writes in AWS 123456789012 / eu-west-1",
	Cloud:       &tools.CloudRequest{Host: "ec2.eu-west-1.amazonaws.com", Method: "POST", Path: "/"},
}

// A grantable cloud platform's action takes every duration, as a cluster's
// does: Chat writes its rule as one of the chat's, Always into the settings.
func TestAGrantableCloudActionTakesEveryDuration(t *testing.T) {
	t.Parallel()

	want := permissions.GrantRule(awsStart.Action)

	s := startWriter(t, writerTool{request: &awsStart})
	s, msg := askWith(t, s, DecisionChat)
	got := s.chatRules(t.Context(), msg.ChatID)
	require.Len(t, got, 1)
	want.ID = got[0].ID
	assert.Equal(t, want, got[0])

	s = startWriter(t, writerTool{request: &awsStart})
	_, _ = askWith(t, s, DecisionAlways)
	rules := storedAlwaysRules(t, s)
	require.Len(t, rules, 1)
	want.ID = rules[0].ID
	assert.Equal(t, want, rules[0])

	s = startWriter(t, writerTool{request: &awsStart})
	s, msg = askWith(t, s, DecisionCommand)
	assert.Equal(t, "true command", toolCallsOf(t, msg)[0].Output)
	assert.Empty(t, s.chatRules(t.Context(), msg.ChatID))
}

// Command approves with command and writes no rule: the proxy keeps it for the
// rest of the command.
func TestCommandWritesNoRule(t *testing.T) {
	t.Parallel()

	s, msg := askOnce(t, DecisionCommand)

	assert.Equal(t, "true command", toolCallsOf(t, msg)[0].Output)
	assert.Equal(t, map[ApprovalStatus][]string{ApprovalApproved: {"command"}}, durations(t, s.db))
	assert.Empty(t, s.chatRules(t.Context(), msg.ChatID))
	assert.Empty(t, storedAlwaysRules(t, s))
}

// storedAlwaysRules is the always rules as stored.
func storedAlwaysRules(t *testing.T, s *service) []permissions.Rule {
	t.Helper()
	rules, err := s.settings.Rules(t.Context())
	require.NoError(t, err)
	return rules
}

// chatRuleRows is how many permission_rules rows are a chat's.
func chatRuleRows(t *testing.T, db *appdb.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.Read.QueryRow(`SELECT COUNT(*) FROM permission_rules WHERE chat_id IS NOT NULL`).Scan(&n))
	return n
}

// Chat writes the action's grant rule as one of the chat's, and the turn hears
// the approval only once the row is there.
func TestChatWritesAGrantBeforeTheDecisionLands(t *testing.T) {
	t.Parallel()

	var s *service
	seen := make(chan int, 1)
	s = startWriter(t, writerTool{after: func() { seen <- chatRuleRows(t, s.db) }})
	s, msg := askWith(t, s, DecisionChat)

	assert.Equal(t, 1, testutil.Recv(t, seen, "the rows when the answer arrived"))
	assert.Equal(t, "true chat", toolCallsOf(t, msg)[0].Output)
	want := permissions.GrantRule(deleteX.Action)
	got := s.chatRules(t.Context(), msg.ChatID)
	require.Len(t, got, 1)
	want.ID = got[0].ID
	assert.Equal(t, []permissions.Rule{want}, got)
	assert.Equal(t, map[ApprovalStatus][]string{ApprovalApproved: {"chat"}}, durations(t, s.db))
}

// Always writes the action's grant rule into the settings, as Settings' own
// add does, under a fresh id.
func TestAlwaysWritesTheRuleIntoTheSettings(t *testing.T) {
	t.Parallel()

	s, msg := askOnce(t, DecisionAlways)

	assert.Equal(t, "true always", toolCallsOf(t, msg)[0].Output)
	rules := storedAlwaysRules(t, s)
	require.Len(t, rules, 1)
	assert.NotEmpty(t, rules[0].ID)
	want := permissions.GrantRule(deleteX.Action)
	want.ID = rules[0].ID
	assert.Equal(t, want, rules[0])
	assert.Zero(t, chatRuleRows(t, s.db))
	assert.Equal(t, map[ApprovalStatus][]string{ApprovalApproved: {"always"}}, durations(t, s.db))
}

// The same answer twice on the same action leaves one rule, and both reach
// their turn approved.
func TestARuleAlreadyHeldIsWrittenOnce(t *testing.T) {
	t.Parallel()

	for _, d := range []ApprovalDecision{DecisionChat, DecisionAlways} {
		s := startWriter(t, writerTool{})
		first := send(t, s, nil, "1", "hi")
		approveWrite(t, s, first, d)
		awaitSettled(t, s, first.ChatID, first.ID)
		fakeOf(s).SetToolCalls(llm.StagedCall("Writer", `{}`))
		second := send(t, s, &first.ChatID, "2", "again")
		approveWrite(t, s, second, d)
		settled := awaitSettled(t, s, second.ChatID, second.ID)

		assert.Equal(t, "true "+string(d), toolCallsOf(t, settled)[0].Output)
		assert.Len(t, s.chatRules(t.Context(), first.ChatID), map[ApprovalDecision]int{DecisionChat: 1, DecisionAlways: 0}[d], d)
		assert.Len(t, storedAlwaysRules(t, s), map[ApprovalDecision]int{DecisionChat: 0, DecisionAlways: 1}[d], d)
	}
}

// approveWrite answers msg's first pending write d.
func approveWrite(t *testing.T, s *service, msg ChatMessage, d ApprovalDecision) {
	t.Helper()
	id := toolCallsOf(t, awaitWrite(t, s, msg, ApprovalPending))[0].SandboxActions[0].Approval.ID
	ok, err := s.Approve(t.Context(), id, d)
	require.NoError(t, err)
	require.True(t, ok)
}

// askWith sends to s, whose model calls its writer, answers the write d, and
// waits for the answer to settle.
func askWith(t *testing.T, s *service, d ApprovalDecision) (*service, ChatMessage) {
	t.Helper()
	msg := send(t, s, nil, "1", "hi")
	approveWrite(t, s, msg, d)
	return s, awaitSettled(t, s, msg.ChatID, msg.ID)
}

// An Always answer whose rule cannot be written is refused, and the request
// still waits for Once.
func TestAlwaysWaitsWhenItsRuleCannotBeWritten(t *testing.T) {
	t.Parallel()

	s := startWriter(t, writerTool{})
	s.settings = settings.NewService(settingsStore(t, s.db), nil, nil, nil, "")
	exec(t, s.db, `CREATE TRIGGER refuse_insert BEFORE INSERT ON permission_rules BEGIN SELECT RAISE(ABORT, 'refused'); END`)

	msg := send(t, s, nil, "1", "hi")
	id := toolCallsOf(t, awaitWrite(t, s, msg, ApprovalPending))[0].SandboxActions[0].Approval.ID
	ok, err := s.Approve(t.Context(), id, DecisionAlways)
	assert.ErrorContains(t, err, "refused")
	assert.False(t, ok)
	assert.Empty(t, storedAlwaysRules(t, s), "nothing is written")

	approve(t, s, id, true)
	settled := awaitSettled(t, s, msg.ChatID, msg.ID)
	assert.Equal(t, "true once", toolCallsOf(t, settled)[0].Output)
}

// blockedRule makes s's rule writes wait for release and answer its error,
// and tells entered each time one starts.
func blockedRule(s *service) (entered chan struct{}, release chan error) {
	entered, release = make(chan struct{}, 4), make(chan error, 4)
	s.ruleWrite = func(context.Context, ChatID, permissions.Rule, ApprovalDecision) error {
		entered <- struct{}{}
		return <-release
	}
	return entered, release
}

// Two answers to one request race on the claim: one writes, the other finds no
// waiter. A rule's write runs outside the lock, so another request is decided
// while it is held.
func TestApproveClaimsTheWaiterBeforeWriting(t *testing.T) {
	t.Parallel()

	second := make(chan struct{})
	s, msg, withN := startTwoRequests(t, twoRequestsTool{second: second, record: true, done: make(chan error, 2)})
	entered, release := blockedRule(s)
	id := toolCallsOf(t, testutil.Recv(t, withN(1), "the request"))[0].SandboxActions[0].Approval.ID

	answers := make(chan bool, 2)
	for _, d := range []ApprovalDecision{DecisionChat, DecisionAlways} {
		go func() {
			ok, _ := s.Approve(t.Context(), id, d)
			answers <- ok
		}()
	}
	testutil.Recv(t, entered, "one rule write")
	assert.False(t, testutil.Recv(t, answers, "the answer that found no waiter"))
	ok, err := s.Approve(t.Context(), newApprovalID(), DecisionOnce)
	require.NoError(t, err)
	assert.False(t, ok, "another Approve is not held behind the write")

	release <- nil
	assert.True(t, testutil.Recv(t, answers, "the answer that wrote"))
	close(second)
	awaitSettled(t, s, msg.ChatID, msg.ID)
}

// A rule write that fails answers its error, and the request still waits.
func TestAFailedRuleWriteLeavesTheRequestWaiting(t *testing.T) {
	t.Parallel()

	s := startWriter(t, writerTool{})
	_, release := blockedRule(s)
	release <- errors.New("store refused")

	msg := send(t, s, nil, "1", "hi")
	id := toolCallsOf(t, awaitWrite(t, s, msg, ApprovalPending))[0].SandboxActions[0].Approval.ID
	ok, err := s.Approve(t.Context(), id, DecisionChat)
	assert.EqualError(t, err, "store refused")
	assert.False(t, ok)

	approve(t, s, id, true)
	settled := awaitSettled(t, s, msg.ChatID, msg.ID)
	assert.Equal(t, "true once", toolCallsOf(t, settled)[0].Output)
}

// A rule write that fails after the turn stopped waiting puts no waiter back.
func TestAFailedRuleWriteAfterTheTurnStoppedLeavesNoWaiter(t *testing.T) {
	t.Parallel()

	s := startWriter(t, writerTool{})
	entered, release := blockedRule(s)
	msg := send(t, s, nil, "1", "hi")
	id := toolCallsOf(t, awaitWrite(t, s, msg, ApprovalPending))[0].SandboxActions[0].Approval.ID

	answered := make(chan error, 1)
	go func() {
		_, err := s.Approve(t.Context(), id, DecisionChat)
		answered <- err
	}()
	testutil.Recv(t, entered, "the rule write")
	require.NoError(t, s.Cancel(t.Context(), msg.ChatID))
	awaitSettled(t, s, msg.ChatID, msg.ID)
	release <- errors.New("store refused")
	assert.Error(t, testutil.Recv(t, answered, "the answer"))

	s.turnsMu.Lock()
	_, waiting := s.pending[id]
	s.turnsMu.Unlock()
	assert.False(t, waiting)
	ok, err := s.Approve(t.Context(), id, DecisionOnce)
	require.NoError(t, err)
	assert.False(t, ok)
}

// networkOf is the one tool_calls row's network column.
func networkOf(t *testing.T, s *service) sql.NullString {
	t.Helper()
	var n sql.NullString
	require.NoError(t, s.db.Read.QueryRow(`SELECT network FROM tool_calls`).Scan(&n))
	return n
}

// A call's network is written on the row that marks it running, so a call
// waiting on the user, or one the user denied, never says it had any; the
// settle's rewrite keeps it, and the wire serves it.
func TestTheRecordSaysTheNetworkOnceTheCallRuns(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		approval tools.Approval
		stored   string
		wire     ToolCallNetwork
	}{
		{tools.Approval{Cwd: "/work", Sandboxed: true, Network: session.NetworkApproved}, "approved", ToolCallNetworkApproved},
		{tools.Approval{Cwd: "/work", Sandboxed: true, Skip: true, Network: session.NetworkTurn}, "turn", ToolCallNetworkTurn},
		{tools.Approval{Cwd: "/work", Sandboxed: true, Skip: true, Network: session.NetworkChat}, "chat", ToolCallNetworkChat},
	} {
		var s *service
		var whileRunning sql.NullString
		tool := approvingTool{
			testTool: testTool{name: "sbx", run: func(context.Context, json.RawMessage) (string, bool) {
				whileRunning = networkOf(t, s)
				return "ran", false
			}},
			approval: c.approval,
		}
		s = startServiceWithTool(t, tool)
		fakeOf(s).SetToolCalls(llm.StagedCall("sbx", `{}`))

		msg := send(t, s, nil, "1", "hi")
		if !c.approval.Skip {
			_, id := awaitRequest(t, s, msg)
			assert.False(t, networkOf(t, s).Valid, "a call waiting on the user has none yet")
			approve(t, s, id, true)
		}
		got := awaitSettled(t, s, msg.ChatID, msg.ID)
		awaitTurnReleased(t, s, msg.ChatID)

		assert.Equal(t, c.stored, whileRunning.String, "the running row")
		assert.Equal(t, c.stored, networkOf(t, s).String, "the settle's rewrite keeps it")
		calls := toolCallsOf(t, got)
		require.NotNil(t, calls[0].Network)
		assert.Equal(t, c.wire, *calls[0].Network)
	}

	s := startServiceWithTool(t, approvingTool{
		testTool: testTool{name: "sbx"},
		approval: tools.Approval{Cwd: "/work", Sandboxed: true, Network: session.NetworkApproved},
	})
	fakeOf(s).SetToolCalls(llm.StagedCall("sbx", `{}`))
	msg := send(t, s, nil, "1", "hi")
	_, id := awaitRequest(t, s, msg)
	approve(t, s, id, false)
	got := awaitSettled(t, s, msg.ChatID, msg.ID)
	awaitTurnReleased(t, s, msg.ChatID)
	assert.False(t, networkOf(t, s).Valid, "a denied call never had any")
	assert.Nil(t, toolCallsOf(t, got)[0].Network)
}

// readDB is a Secret read's request, as the proxy asks it.
var readDB = tools.ActionRequest{
	Action: permissions.Action{
		Principal: permissions.Chat, Target: permissions.Kubernetes, AccessLevel: permissions.DataRead, Scope: "1/web", AccountName: "dev",
		Operation: "get", Resource: "secrets", Name: "db", Summary: "Show Secret db in web on dev",
	},
	Grantable:   true,
	CommandRule: "Allow get of core secrets in dev / web for this command",
	ChatRule:    "Allow data reads in dev / web",
	Write:       &tools.ClusterWrite{Method: "GET", Path: "/api/v1/namespaces/web/secrets/db"},
}

// A Secret read's request is answered as any other: Chat writes the data
// read grant for its cluster and namespace as one of the chat's, and Always
// writes it into the settings.
func TestAChatAnswerWritesADataReadGrant(t *testing.T) {
	t.Parallel()

	want := permissions.Rule{Effect: permissions.Allow, AccessLevel: permissions.DataRead, Principal: "*", Target: "kubernetes",
		Scope: "1/web", Service: "*", Operation: "*", Resource: "*"}

	s := startWriter(t, writerTool{request: &readDB})
	s, msg := askWith(t, s, DecisionChat)
	got := s.chatRules(t.Context(), msg.ChatID)
	require.Len(t, got, 1)
	want.ID = got[0].ID
	assert.Equal(t, want, got[0])

	s = startWriter(t, writerTool{request: &readDB})
	_, _ = askWith(t, s, DecisionAlways)
	rules := storedAlwaysRules(t, s)
	require.Len(t, rules, 1)
	want.ID = rules[0].ID
	assert.Equal(t, want, rules[0])
}
