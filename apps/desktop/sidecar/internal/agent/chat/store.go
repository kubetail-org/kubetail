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

// The row helpers: one per statement, each naming the statement in the error it
// wraps, and the scanners that turn a row into a record.
package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/rawjson"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// errRunNotQueued is a claim of a run that is not queued: a cancel settled it first,
// or it was never this process's.
var errRunNotQueued = errors.New("chat: run is not queued")

// llmCallStranded is the error written on a model call a previous process left
// open, and toolCallStranded the tool call's, whose error column is a JSON object.
// callCancelled is a model call the run's cancel ended. toolCallInterrupted is a
// tool call the turn ended inside of, by a panic.
const (
	llmCallStranded     = "stranded"
	callCancelled       = "cancelled"
	toolCallStranded    = `{"error":"stranded"}`
	toolCallInterrupted = `{"error":"interrupted"}`
	// toolErrorOwn marks a call its tool answered with an error, whose text is in
	// result.
	toolErrorOwn = `{"error":"tool"}`
)

// The tool call statuses the loop writes, five of the table's six: a gated call is
// inserted awaiting_approval, a call that runs is written running and settles
// succeeded or failed, a call the user said no to is denied, and a call the loop
// refused without running is failed with no started_at.
const (
	toolAwaitingApproval = "awaiting_approval"
	toolRunning          = "running"
	toolSucceeded        = "succeeded"
	toolFailed           = "failed"
	toolDenied           = "denied"
)

// What an approval is of, as approvals.kind stores it: a call's own question, or
// an action its sandboxed command's request asked for.
const (
	approvalCall   = "call"
	approvalAction = "action"
)

// Who ran a tool call, as tool_calls.runs_on stores it.
const (
	runsOnSidecar  = "sidecar"
	runsOnProvider = "provider"
)

// RunStatus is where an agent run is in its lifecycle: queued, then running, then
// one of the three terminal states. waiting_approval is running with a command
// waiting on the user.
type RunStatus string

const (
	RunQueued          RunStatus = "queued"
	RunRunning         RunStatus = "running"
	RunWaitingApproval RunStatus = "waiting_approval"
	RunSucceeded       RunStatus = "succeeded"
	RunFailed          RunStatus = "failed"
	RunCancelled       RunStatus = "cancelled"
)

// messageStatusOf is the public status of an assistant message, off its run's.
func messageStatusOf(s RunStatus) MessageStatus {
	switch s {
	case RunSucceeded:
		return StatusComplete
	case RunFailed:
		return StatusFailed
	case RunCancelled:
		return StatusCancelled
	case RunWaitingApproval:
		return StatusWaitingApproval
	default:
		return StatusStreaming
	}
}

// Timestamps are stored as unix millis. Every time.Time here is read back with
// fromMillis, and a minted one goes through normalizeTime before it is stored or
// compared, so a value never differs from its own round trip by a monotonic reading
// or a location.
func millis(t time.Time) int64            { return t.UnixMilli() }
func fromMillis(ms int64) time.Time       { return time.UnixMilli(ms).UTC() }
func normalizeTime(t time.Time) time.Time { return fromMillis(millis(t)) }

// scanner is what *sql.Row and *sql.Rows share.
type scanner interface{ Scan(dest ...any) error }

// nullString stores "" as NULL, for the nullable text columns.
func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

// nullMillis is a timestamp for a nullable column; the zero time is NULL.
func nullMillis(t time.Time) sql.NullInt64 {
	return sql.NullInt64{Int64: millis(t), Valid: !t.IsZero()}
}

// count is a known token count.
func count(n int) sql.NullInt64 { return sql.NullInt64{Int64: int64(n), Valid: true} }

// --- chats ---

func insertChat(ctx context.Context, st stmts, c Chat) error {
	_, err := st.Exec(ctx, stmtInsertChat,
		string(c.ID), nullString(c.Title), string(c.Mode), string(c.ClusterID), millis(c.CreatedAt), millis(c.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert chat: %w", err)
	}
	return nil
}

// touchChat moves the chat to the top of the list.
func touchChat(ctx context.Context, st stmts, id ChatID, at time.Time) error {
	if _, err := st.Exec(ctx, stmtTouchChat, millis(at), string(id)); err != nil {
		return fmt.Errorf("touch chat: %w", err)
	}
	return nil
}

// renameChat retitles a chat and returns the row it wrote. No row
// means no chat.
func renameChat(ctx context.Context, st stmts, id ChatID, title string, at time.Time) (Chat, bool, error) {
	c, err := scanChat(st.QueryRow(ctx, stmtRenameChat, title, millis(at), string(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return Chat{}, false, nil
	}
	if err != nil {
		return Chat{}, false, fmt.Errorf("rename chat: %w", err)
	}
	return c, true, nil
}

// setSandboxDisabled writes a chat's switch and returns the row it wrote.
// No row means no chat.
func setSandboxDisabled(ctx context.Context, st stmts, id ChatID, disabled bool) (Chat, bool, error) {
	c, err := scanChat(st.QueryRow(ctx, stmtSetSandboxDisabled, disabled, string(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return Chat{}, false, nil
	}
	if err != nil {
		return Chat{}, false, fmt.Errorf("set sandbox disabled: %w", err)
	}
	return c, true, nil
}

// setNetworkEnabled writes a chat's network switch and returns the row it
// wrote. No row means no chat.
func setNetworkEnabled(ctx context.Context, st stmts, id ChatID, enabled bool) (Chat, bool, error) {
	c, err := scanChat(st.QueryRow(ctx, stmtSetNetworkEnabled, enabled, string(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return Chat{}, false, nil
	}
	if err != nil {
		return Chat{}, false, fmt.Errorf("set network enabled: %w", err)
	}
	return c, true, nil
}

func getChat(ctx context.Context, st stmts, id ChatID) (Chat, bool, error) {
	c, err := scanChat(st.QueryRow(ctx, stmtSelectChat, string(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return Chat{}, false, nil
	}
	if err != nil {
		return Chat{}, false, fmt.Errorf("get chat: %w", err)
	}
	return c, true, nil
}

func listChats(ctx context.Context, st stmts) ([]Chat, error) {
	rows, err := st.Query(ctx, stmtSelectChats)
	if err != nil {
		return nil, fmt.Errorf("list chats: %w", err)
	}
	defer rows.Close()

	var out []Chat
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			return nil, fmt.Errorf("list chats: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list chats: %w", err)
	}
	return out, nil
}

// chatIDsByCluster lists the chats filed under one cluster: what
// its sweep deletes.
func chatIDsByCluster(ctx context.Context, st stmts, clusterID apimeta.ClusterID) ([]ChatID, error) {
	return collectIDs[ChatID](ctx, st, stmtSelectChatIDsByCluster, "chat ids by cluster", string(clusterID))
}

// deleteChat removes the chat's row, and reports whether there was
// one to remove.
func deleteChat(ctx context.Context, st stmts, id ChatID) (bool, error) {
	res, err := st.Exec(ctx, stmtDeleteChat, string(id))
	if err != nil {
		return false, fmt.Errorf("delete chat: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil { // coverage:ignore modernc.org/sqlite's RowsAffected never fails
		return false, fmt.Errorf("delete chat: %w", err)
	}
	return n > 0, nil
}

func scanChat(s scanner) (Chat, error) {
	var (
		c                    Chat
		createdAt, updatedAt int64
	)
	if err := s.Scan(&c.ID, &c.Title, &c.Mode, &c.ClusterID, &c.SandboxDisabled, &c.NetworkEnabled, &createdAt, &updatedAt, &c.AwaitingApproval); err != nil {
		return Chat{}, err
	}
	c.CreatedAt, c.UpdatedAt = fromMillis(createdAt), fromMillis(updatedAt)
	return c, nil
}

// --- messages, agent_runs and llm_calls ---

// agentRun is a run's row as it is inserted. A chat run is queued, answering
// the question TriggerMessageID, with the three names the send asked for. A
// subagent's run is running, under ParentID, with the AgentType that ran and the
// Task it was handed. A monitor's run is queued under ClusterID, with no chat,
// its Task the brief.
type agentRun struct {
	ID               RunID
	ParentID         RunID
	AgentType        string
	ChatID           ChatID
	ClusterID        apimeta.ClusterID
	TriggerMessageID MessageID
	ProviderID       string
	ModelID          string
	Effort           string // "" where the model has no such knob, stored NULL
	Dialect          llm.Dialect
	Task             string
	AppVersion       string
	CreatedAt        time.Time
}

// llmCallEntry is an llm_calls row as the turn holds it: what was sent and when,
// then how it closed. FinishedAt is zero while the call is open.
type llmCallEntry struct {
	ID         LLMCallID
	RunID      RunID
	Seq        int
	ProviderID string
	ModelID    string
	Effort     string
	StartedAt  time.Time
	// FirstChunkAt is the round's first text or thinking chunk; the zero time,
	// NULL, on a call that showed nothing before it ended.
	FirstChunkAt time.Time
	StopReason   string
	Error        string
	// ServerUses is the provider's count of each server tool it ran, by tool name;
	// nil where it reported none.
	ServerUses map[string]int
	// The token counts, NULL together when the call's usage is unknown.
	// UncachedTokens is the input less both cache counts, as input_tokens stores it.
	UncachedTokens   sql.NullInt64
	CacheReadTokens  sql.NullInt64
	CacheWriteTokens sql.NullInt64
	OutputTokens     sql.NullInt64
	FinishedAt       time.Time
}

// setUsage keeps a report's counts, the input stored uncached. An absent report
// keeps none, and so does one whose cache counts exceed its input, which is
// logged rather than clamped.
func (c *llmCallEntry) setUsage(u llm.Usage) {
	if !u.Reported {
		return
	}
	uncached := u.InputTokens - u.CacheReadTokens - u.CacheWriteTokens
	if uncached < 0 {
		slog.Warn("the provider's usage report is inconsistent; storing no counts for the call",
			"provider", c.ProviderID, "input", u.InputTokens, "cacheRead", u.CacheReadTokens, "cacheWrite", u.CacheWriteTokens)
		return
	}
	c.UncachedTokens = count(uncached)
	c.CacheReadTokens = count(u.CacheReadTokens)
	c.CacheWriteTokens = count(u.CacheWriteTokens)
	c.OutputTokens = count(u.OutputTokens)
}

// inputTokens is everything the call read, the cached part included: the three
// input counts summed back. NULL when the call's usage is unknown.
func (c llmCallEntry) inputTokens() sql.NullInt64 {
	if !c.UncachedTokens.Valid {
		return sql.NullInt64{}
	}
	return count(int(c.UncachedTokens.Int64 + c.CacheReadTokens.Int64 + c.CacheWriteTokens.Int64))
}

// toolCallEntry is a tool_calls row as the turn holds it: whole, so every write of
// it is the row entire. Seq is the call's place among the tool_use blocks of the
// reply that asked, so (LLMCallID, Seq) is the order the model called them in.
type toolCallEntry struct {
	ID        ToolCallID
	LLMCallID LLMCallID
	Seq       int
	// ByProvider is a call the model's provider ran, stored runs_on 'provider';
	// every other call is the sidecar's.
	ByProvider bool
	// Name is the tool's name in the box. Contract is its tool's contract when
	// the row was first written, empty for a tool the box did not hold.
	Name      string
	Contract  string
	ToolUseID string
	Arguments string
	// Cwd is where a gated call starts, set at the gate; '' on every other call.
	Cwd string
	// Sandboxed is whether a sandbox confined the call, set at the gate.
	Sandboxed bool
	// Env is the user's variables a command outside the sandbox was shown with,
	// a JSON array of NAME=value, set at the gate.
	Env string
	// Network is the network the call ran with, set when it starts running.
	Network session.Network
	// Shown is what the run showed, as stored, for a tools.Shown tool; "" where
	// the action is read off Arguments. It is decoded through the box (actionOf).
	Shown      string
	Result     string
	Error      string
	Status     string
	CreatedAt  time.Time
	StartedAt  sql.NullInt64
	FinishedAt sql.NullInt64
	// IsMutating is 1 on a call put to the user: what the app treated it as when
	// it ran. A gated call whose approval skipped is 0.
	IsMutating bool
	// SpawnedRunID is the subagent run an Agent call started, "" on every other call.
	SpawnedRunID RunID
	// AgentCallID is the Agent call a subagent's call ran under, "" on a parent's
	// own. Not a column: newToolCall and the reads set it.
	AgentCallID ToolCallID
	// Approval is the call's own approvals row, nil on an ungated call.
	Approval *approval
	// SandboxActions are the call's action approvals, a cluster's and a
	// cloud platform's in one list, in the order asked.
	SandboxActions []*approval
	// Task is the background task the call started, off its row; nil on every
	// other call. Only the reads set it: the turn's own entries carry none.
	Task *taskState
}

// taskState is what a background_tasks row says of a task: its status, the exit
// code once it exited with one that could be read, and a completed agent's
// report, whole, off its run.
type taskState struct {
	Status   string
	ExitCode sql.NullInt64
	Report   string
	// Result is a finished watch's tools.WatchResult as JSON, "" otherwise.
	Result string
}

// approval is an approvals row as the turn holds it. It is set to what landed,
// never ahead of it, since the settle writes it again.
type approval struct {
	ID         ApprovalID
	ToolCallID ToolCallID
	Status     ApprovalStatus
	CreatedAt  time.Time
	DecidedAt  sql.NullInt64
	// Request is the action an action approval holds; nil on a call's own, and
	// on an action whose stored request does not decode.
	Request *tools.ActionRequest
	// Duration is how long an approval holds, what the user chose; "" unless
	// approved.
	Duration permissions.Duration
	// Reason is the mode or rule that decided a write nobody was asked about;
	// "" for one the user answered.
	Reason string
}

// nextSeq is the seq the chat's next message takes.
func nextSeq(ctx context.Context, st stmts, id ChatID) (int64, error) {
	var seq int64
	if err := st.QueryRow(ctx, stmtNextSeq, string(id)).Scan(&seq); err != nil {
		return 0, fmt.Errorf("next seq: %w", err)
	}
	return seq, nil
}

// insertMessage writes a message's own row: the request key on a question, the run
// on an answer.
func insertMessage(ctx context.Context, st stmts, m ChatMessage, requestKey string) error {
	_, err := st.Exec(ctx, stmtInsertMessage,
		string(m.ID), string(m.ChatID), m.Seq, string(m.Role), string(m.Content), nullString(requestKey), nullString(string(m.RunID)), millis(m.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

func insertRun(ctx context.Context, st stmts, r agentRun) error {
	_, err := st.Exec(ctx, stmtInsertRun,
		string(r.ID), r.AppVersion, string(r.ChatID), string(r.TriggerMessageID),
		r.ProviderID, r.ModelID, nullString(r.Effort), string(r.Dialect), millis(r.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert run: %w", err)
	}
	return nil
}

// insertSubagentRun inserts a subagent's run, running from the insert.
func insertSubagentRun(ctx context.Context, st stmts, r agentRun) error {
	_, err := st.Exec(ctx, stmtInsertSubagentRun,
		string(r.ID), string(r.ParentID), r.AgentType, r.AppVersion, string(r.ChatID),
		r.ProviderID, r.ModelID, nullString(r.Effort), string(r.Dialect), r.Task, millis(r.CreatedAt), millis(r.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert subagent run: %w", err)
	}
	return nil
}

// insertMonitorRun inserts a monitor's run, queued under its cluster.
func insertMonitorRun(ctx context.Context, st stmts, r agentRun) error {
	_, err := st.Exec(ctx, stmtInsertMonitorRun,
		string(r.ID), r.AppVersion, string(r.ClusterID),
		r.ProviderID, r.ModelID, nullString(r.Effort), string(r.Dialect), r.Task, millis(r.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert monitor run: %w", err)
	}
	return nil
}

// deleteRun takes back a subagent's run whose start failed.
func deleteRun(ctx context.Context, st stmts, id RunID) error {
	if _, err := st.Exec(ctx, stmtDeleteRun, string(id)); err != nil {
		return fmt.Errorf("delete run: %w", err)
	}
	return nil
}

// claimRun moves a queued run to running. errRunNotQueued when it was not queued.
func claimRun(ctx context.Context, st stmts, id RunID, at time.Time) error {
	res, err := st.Exec(ctx, stmtClaimRun, millis(at), string(id))
	if err != nil {
		return fmt.Errorf("claim run: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil { // coverage:ignore modernc.org/sqlite's RowsAffected never fails
		return fmt.Errorf("claim run: %w", err)
	}
	if n == 0 {
		return errRunNotQueued
	}
	return nil
}

func writeContent(ctx context.Context, st stmts, id MessageID, c rawjson.RawJSON) error {
	if _, err := st.Exec(ctx, stmtWriteContent, string(c), string(id)); err != nil {
		return fmt.Errorf("write content: %w", err)
	}
	return nil
}

// flipRun moves a live run between running and waiting_approval.
func flipRun(ctx context.Context, st stmts, id RunID, status RunStatus) error {
	if _, err := st.Exec(ctx, stmtFlipRun, string(status), string(id)); err != nil {
		return fmt.Errorf("flip run: %w", err)
	}
	return nil
}

// settleRun ends a run: a terminal status, a subagent's final text, its error (""
// is NULL for both), when.
func settleRun(ctx context.Context, st stmts, id RunID, status RunStatus, result, errText string, at time.Time) error {
	_, err := st.Exec(ctx, stmtSettleRun, string(status), nullString(result), nullString(errText), millis(at), string(id))
	if err != nil {
		return fmt.Errorf("settle run: %w", err)
	}
	return nil
}

// answerByRequestKey answers a send whose key a question carries: the answer that send
// returned, as the transcript read serves it.
func answerByRequestKey(ctx context.Context, st stmts, requestKey string, box tools.Box) (ChatMessage, bool, error) {
	m, err := scanMessage(st.QueryRow(ctx, stmtSelectAnswerByRequestKey, requestKey))
	if errors.Is(err, sql.ErrNoRows) {
		return ChatMessage{}, false, nil
	}
	if err != nil {
		return ChatMessage{}, false, fmt.Errorf("answer by request key: %w", err)
	}
	byRun, err := toolCallsByRun(ctx, st, runCallReads, string(m.RunID))
	if err != nil {
		return ChatMessage{}, false, fmt.Errorf("answer by request key: %w", err)
	}
	msgs := []ChatMessage{m}
	withToolCalls(msgs, byRun, box)
	return msgs[0], true, nil
}

// viewCall is the call id of chatID whose view the user has open: the id its
// tool_use block carries and the view, a LogsView call's own, or the View around
// this of a LogsRead call or a finished LogsWatch call's task. ok is false for a
// call of another chat, of another tool, or one that showed no view. A subagent's
// call is the chat's: its run carries the chat's id. A call whose action box
// refuses shows no view either.
func viewCall(ctx context.Context, st stmts, box tools.Box, chatID ChatID, id ToolCallID) (toolUseID string, view tools.LogsViewAction, ok bool, err error) {
	var (
		name                    string
		contract, shown, result sql.NullString
	)
	err = st.QueryRow(ctx, stmtSelectViewCall, string(id), string(chatID)).Scan(&toolUseID, &name, &contract, &shown, &result)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !shown.Valid {
		return "", view, false, nil
	}
	if err != nil {
		return "", view, false, fmt.Errorf("view call: %w", err)
	}
	call := toolCallEntry{ID: id, Name: name, Contract: contract.String, Shown: shown.String}
	action := actionOf(box, call)
	switch {
	case action == nil:
		return "", view, false, nil
	case action.LogsView != nil:
		return toolUseID, *action.LogsView, true, nil
	case action.LogsRead != nil && action.LogsRead.ViewAround != nil:
		return toolUseID, *action.LogsRead.ViewAround, true, nil
	case action.LogsWatch != nil && result.Valid:
		r, ok := watchResultOf(box, call, result.String)
		if ok && r.ViewAround != nil {
			return toolUseID, *r.ViewAround, true, nil
		}
	}
	return "", view, false, nil
}

// newestContext is the context the model currently holds for the chat: the text
// of the newest context block among its questions, or "" when it holds none.
func newestContext(ctx context.Context, st stmts, id ChatID) (string, error) {
	var text string
	err := st.QueryRow(ctx, stmtSelectNewestContext, string(id)).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("newest context: %w", err)
	}
	return text, nil
}

func insertLLMCall(ctx context.Context, st stmts, c llmCallEntry) error {
	_, err := st.Exec(ctx, stmtInsertLLMCall,
		string(c.ID), string(c.RunID), c.Seq, c.ProviderID, c.ModelID, nullString(c.Effort), millis(c.StartedAt))
	if err != nil {
		return fmt.Errorf("insert llm call: %w", err)
	}
	return nil
}

// closeLLMCall ends a call with the row's outcome: the provider's stop reason,
// server tool counts, usage and served model when a reply arrived, the error
// otherwise, and when it first showed something and finished.
func closeLLMCall(ctx context.Context, st stmts, c llmCallEntry) error {
	var uses sql.NullString
	if c.ServerUses != nil {
		b, _ := json.Marshal(c.ServerUses) // a map of ints always marshals
		uses = sql.NullString{String: string(b), Valid: true}
	}
	if _, err := st.Exec(ctx, stmtCloseLLMCall, c.ModelID, nullString(c.StopReason), nullString(c.Error), uses,
		c.UncachedTokens, c.CacheReadTokens, c.CacheWriteTokens, c.OutputTokens,
		nullMillis(c.FirstChunkAt), millis(c.FinishedAt), string(c.ID)); err != nil {
		return fmt.Errorf("close llm call: %w", err)
	}
	return nil
}

// upsertToolCall writes a tool call's row whole, inserting it or replacing what is
// there: the running row before the tool runs, the outcome after, and again at the
// settle, which heals a write that was lost.
func upsertToolCall(ctx context.Context, st stmts, c toolCallEntry) error {
	runsOn := runsOnSidecar
	if c.ByProvider {
		runsOn = runsOnProvider
	}
	_, err := st.Exec(ctx, stmtUpsertToolCall,
		string(c.ID), string(c.LLMCallID), c.Seq, runsOn, c.Name, nullString(c.Contract), nullString(c.ToolUseID), nullString(c.Arguments), c.Cwd, c.Sandboxed, c.Env,
		nullString(string(c.Network)), nullString(c.Result), nullString(c.Error), c.IsMutating, nullString(string(c.SpawnedRunID)), nullString(c.Status),
		millis(c.CreatedAt), c.StartedAt, c.FinishedAt, nullString(c.Shown))
	if err != nil {
		return fmt.Errorf("upsert tool call: %w", err)
	}
	return nil
}

// upsertApproval writes an approvals row whole: pending when the request is shown,
// the decision once it is made, and again at the settle.
func upsertApproval(ctx context.Context, st stmts, a approval) error {
	kind, request := approvalCall, sql.NullString{}
	if a.Request != nil {
		b, err := json.Marshal(a.Request)
		if err != nil { // coverage:ignore an ActionRequest is strings and bools, which always marshal
			return fmt.Errorf("upsert approval: %w", err)
		}
		kind, request = approvalAction, sql.NullString{String: string(b), Valid: true}
	}
	_, err := st.Exec(ctx, stmtUpsertApproval,
		string(a.ID), string(a.ToolCallID), kind, request, a.Status, nullString(string(a.Duration)), nullString(a.Reason),
		millis(a.CreatedAt), a.DecidedAt)
	if err != nil {
		return fmt.Errorf("upsert approval: %w", err)
	}
	return nil
}

// callReads is a read of tool calls and the read of their sandbox actions over
// the same scope.
type callReads struct{ calls, actions stmtID }

var (
	chatCallReads = callReads{calls: stmtSelectToolCalls, actions: stmtSelectSandboxActions}
	runCallReads  = callReads{calls: stmtSelectRunToolCalls, actions: stmtSelectRunSandboxActions}
)

// toolCallsByRun reads the tool calls reads selects, joined to their own
// approvals and carrying their sandbox actions, grouped by run in the order the
// model asked them.
func toolCallsByRun(ctx context.Context, st stmts, reads callReads, arg string) (map[RunID][]toolCallEntry, error) {
	actions, err := sandboxActionsByCall(ctx, st, reads.actions, arg)
	if err != nil {
		return nil, err
	}
	rows, err := st.Query(ctx, reads.calls, arg)
	if err != nil {
		return nil, fmt.Errorf("tool calls: %w", err)
	}
	defer rows.Close()

	out := map[RunID][]toolCallEntry{}
	for rows.Next() {
		var (
			run                          RunID
			c                            toolCallEntry
			runsOn                       string
			useID, args, result, errText sql.NullString
			contract, status             sql.NullString
			spawned, network             sql.NullString
			approvalID, approvalStatus   sql.NullString
			approvalDuration             string
			taskStatus, report           sql.NullString
			exitCode                     sql.NullInt64
			shown, watched               sql.NullString
		)
		err := rows.Scan(&run, &c.ID, &runsOn, &useID, &c.Name, &contract, &args, &c.Cwd, &c.Sandboxed, &c.Env, &network, &result, &errText, &status, &c.StartedAt,
			&spawned, &approvalID, &approvalStatus, &approvalDuration, &taskStatus, &exitCode, &report, &shown, &watched)
		if err != nil {
			return nil, fmt.Errorf("tool calls: %w", err)
		}
		c.Shown = shown.String
		c.ByProvider, c.Contract, c.Status = runsOn == runsOnProvider, contract.String, status.String
		c.ToolUseID, c.Arguments, c.Result, c.Error = useID.String, args.String, result.String, errText.String
		c.SpawnedRunID, c.Network = RunID(spawned.String), session.Network(network.String)
		if approvalID.Valid {
			c.Approval = &approval{
				ID: ApprovalID(approvalID.String), Status: ApprovalStatus(approvalStatus.String),
				Duration: permissions.Duration(approvalDuration),
			}
		}
		if taskStatus.Valid {
			c.Task = &taskState{Status: taskStatus.String, ExitCode: exitCode, Report: report.String, Result: watched.String}
		}
		c.SandboxActions = actions[c.ID]
		out[run] = append(out[run], c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tool calls: %w", err)
	}
	return out, nil
}

// sandboxActionsByCall reads the sandbox actions stmt selects, by call, in the
// order asked.
func sandboxActionsByCall(ctx context.Context, st stmts, stmt stmtID, arg string) (map[ToolCallID][]*approval, error) {
	rows, err := st.Query(ctx, stmt, arg)
	if err != nil {
		return nil, fmt.Errorf("sandbox actions: %w", err)
	}
	defer rows.Close()

	out := map[ToolCallID][]*approval{}
	for rows.Next() {
		var (
			a         approval
			request   string
			createdAt int64
		)
		var duration string
		if err := rows.Scan(&a.ToolCallID, &a.ID, &a.Status, &duration, &request, &a.Reason, &createdAt, &a.DecidedAt); err != nil {
			return nil, fmt.Errorf("sandbox actions: %w", err)
		}
		a.CreatedAt, a.Duration = time.UnixMilli(createdAt).UTC(), permissions.Duration(duration)
		a.Request = &tools.ActionRequest{}
		if json.Unmarshal([]byte(request), a.Request) != nil {
			warnRefused(a.ToolCallID, "request", tools.ErrDecode)
			a.Request = nil
		}
		out[a.ToolCallID] = append(out[a.ToolCallID], &a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sandbox actions: %w", err)
	}
	return out, nil
}

// withToolCalls fills each message's ToolCalls from its run's rows, then each
// subagent's its Agent calls ran, tagged with that call, all read through box. A
// message with none reads emptyToolCalls.
func withToolCalls(msgs []ChatMessage, byRun map[RunID][]toolCallEntry, box tools.Box) {
	for i := range msgs {
		msgs[i].ToolCalls = emptyToolCalls
		if msgs[i].RunID == "" {
			continue
		}
		calls := slices.Clone(byRun[msgs[i].RunID])
		for _, c := range byRun[msgs[i].RunID] {
			if c.SpawnedRunID == "" {
				continue
			}
			for _, call := range byRun[c.SpawnedRunID] {
				call.AgentCallID = c.ID
				calls = append(calls, call)
			}
		}
		if len(calls) > 0 {
			msgs[i].ToolCalls = marshalToolCalls(calls, box)
			msgs[i].hasStored = slices.ContainsFunc(calls, func(c toolCallEntry) bool { return c.Task != nil || c.AgentCallID != "" })
		}
	}
}

// --- background_tasks ---

// The statuses a task's row takes.
const (
	taskRunning = "running"
	taskExited  = "exited"
	taskStopped = "stopped"
	taskLost    = "lost"
	// An agent's: it answered, or ended on its own error. A watch fails too, on an
	// error or the follow's end with no line.
	taskCompleted = "completed"
	taskFailed    = "failed"
	// A watch's alone: it ended holding lines, or at its time with none.
	taskMatched = "matched"
	taskExpired = "expired"
)

// Who stopped a task: the model's TaskStop, the user (the Stop button, or the
// chat's delete), the app's own stop, or an agent's approval request that went
// unanswered. All but the first are the notice's words, since a stop the model
// made is never a notice.
const (
	stoppedByModel      = "model"
	stoppedByUser       = llm.TaskStoppedByUser
	stoppedByApp        = llm.TaskStoppedByApp
	stoppedByUnanswered = llm.TaskStoppedByUnanswered
)

// taskEnd is how a task's row ends.
type taskEnd struct {
	Status    string
	StoppedBy string // on a stopped row alone
	ExitCode  sql.NullInt64
	At        time.Time
	Notified  bool
	// Result is a watch's tools.WatchResult as JSON; NULL for every other task.
	Result sql.NullString
	// Run ends an agent's run beside the task's row, and Rows writes the run's
	// rows whole again; both nil for a command's.
	Run  func(context.Context, stmts) error
	Rows func(context.Context, stmts) error
}

func insertTask(ctx context.Context, st stmts, id TaskID, chatID ChatID, toolCallID ToolCallID, path string, at time.Time) error {
	if _, err := st.Exec(ctx, stmtInsertTask, string(id), string(chatID), string(toolCallID), path, millis(at)); err != nil {
		return fmt.Errorf("insert task: %w", err)
	}
	return nil
}

func deleteTask(ctx context.Context, st stmts, id TaskID) error {
	if _, err := st.Exec(ctx, stmtDeleteTask, string(id)); err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	return nil
}

func finishTask(ctx context.Context, st stmts, id TaskID, end taskEnd) error {
	var notified sql.NullInt64
	if end.Notified {
		notified = nullMillis(end.At)
	}
	_, err := st.Exec(ctx, stmtFinishTask, end.Status, nullString(end.StoppedBy), end.ExitCode, millis(end.At), notified, end.Result, string(id))
	if err != nil {
		return fmt.Errorf("finish task: %w", err)
	}
	return nil
}

// waitingNotices is the chat's finished tasks whose notices have not reached the
// model, in the order they ended, each call's action read through box, or the one
// its run showed: its command, or, for an Agent call, the report or error of the
// run it started, and for a LogsWatch call, the watch's result.
func waitingNotices(ctx context.Context, st stmts, chatID ChatID, box tools.Box) ([]waitingNotice, error) {
	rows, err := st.Query(ctx, stmtSelectWaitingNotices, string(chatID))
	if err != nil {
		return nil, fmt.Errorf("waiting notices: %w", err)
	}
	defer rows.Close()
	var out []waitingNotice
	for rows.Next() {
		var (
			w                                   waitingNotice
			callID                              ToolCallID
			name, cwd                           string
			useID, stoppedBy, contract, args    sql.NullString
			agentName, agentContract, agentArgs sql.NullString
			report, runError, shown, result     sql.NullString
			exitCode                            sql.NullInt64
			sandboxed, underSend                bool
		)
		err := rows.Scan(&w.id, &useID, &w.path, &w.status, &stoppedBy, &exitCode, &callID, &name, &contract, &args, &cwd, &sandboxed,
			&agentName, &agentContract, &agentArgs, &w.agent, &report, &runError, &underSend, &shown, &result)
		if err != nil {
			return nil, fmt.Errorf("waiting notices: %w", err)
		}
		w.toolUseID, w.stoppedBy = useID.String, stoppedBy.String
		w.report, w.runError, w.underSend = report.String, runError.String, underSend
		kind, _ := box.ActionKind(name)
		w.watch = kind == tools.ActionLogsWatch
		call := toolCallEntry{ID: callID, Name: name, Contract: contract.String, Arguments: args.String, Cwd: cwd, Sandboxed: sandboxed}
		// A watch's description is its run's; every other call reads its own arguments.
		if w.watch {
			call.Shown = shown.String
			if result.Valid {
				if r, ok := watchResultOf(box, call, result.String); ok {
					w.result = &r
				}
			}
		}
		if a := actionOf(box, call); a != nil {
			w.description = a.Description
			if a.Command != nil {
				w.command = a.Command.Text
			}
		}
		if agentName.Valid {
			agent := toolCallEntry{Name: agentName.String, Contract: agentContract.String, Arguments: agentArgs.String}
			if a := actionOf(box, agent); a != nil {
				w.agentDescription = a.Description
			}
		}
		if exitCode.Valid {
			code := int(exitCode.Int64)
			w.exitCode = &code
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("waiting notices: %w", err)
	}
	return out, nil
}

// markNotified marks every waiting notice of the chat as told.
func markNotified(ctx context.Context, st stmts, chatID ChatID, at time.Time) error {
	if _, err := st.Exec(ctx, stmtMarkNotified, millis(at), string(chatID)); err != nil {
		return fmt.Errorf("mark notified: %w", err)
	}
	return nil
}

// lastAnswerRun is what the chat's last answer ran on. A chat holding a task
// holds the answer that started it.
func lastAnswerRun(ctx context.Context, st stmts, chatID ChatID) (providerID, modelID, effort string, err error) {
	var eff sql.NullString
	if err := st.QueryRow(ctx, stmtSelectLastAnswerRun, string(chatID)).Scan(&providerID, &modelID, &eff); err != nil {
		return "", "", "", fmt.Errorf("last answer run: %w", err)
	}
	return providerID, modelID, eff.String, nil
}

// runSummary is what the length check reads of a chat's newest turn.
type runSummary struct {
	status              RunStatus
	errText             string
	providerID, modelID string
	// firstCallErred says the turn's first model call ended on an error, a
	// cancel included: a failed turn that overflowed there did so on the chat
	// alone, before any tool round.
	firstCallErred bool
}

// newestRun is the chat's newest turn by message seq, and false when it has none.
func newestRun(ctx context.Context, st stmts, chatID ChatID) (runSummary, bool, error) {
	var r runSummary
	err := st.QueryRow(ctx, stmtSelectNewestRun, string(chatID)).
		Scan(&r.status, &r.errText, &r.providerID, &r.modelID, &r.firstCallErred)
	if errors.Is(err, sql.ErrNoRows) {
		return runSummary{}, false, nil
	}
	if err != nil {
		return runSummary{}, false, fmt.Errorf("newest run: %w", err)
	}
	return r, true, nil
}

// lastContextUse is what a model of providerID last read and wrote on the chat:
// the newest reported call of the chat's succeeded turns on that provider that
// ran no server tool. Zero when there is none.
func lastContextUse(ctx context.Context, st stmts, chatID ChatID, providerID string) (int64, error) {
	var c llmCallEntry
	err := st.QueryRow(ctx, stmtSelectLastContextUse, string(chatID), providerID).
		Scan(&c.UncachedTokens, &c.CacheReadTokens, &c.CacheWriteTokens, &c.OutputTokens)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("last context use: %w", err)
	}
	return c.inputTokens().Int64 + c.OutputTokens.Int64, nil
}

// markLostTasks ends every task a previous process left running, and returns
// their chats, one entry per task.
func markLostTasks(ctx context.Context, st stmts, at time.Time) ([]ChatID, error) {
	return collectIDs[ChatID](ctx, st, stmtMarkLostTasks, "mark lost tasks", millis(at))
}

// closeStrandedToolCalls ends every tool call a previous process left open.
func closeStrandedToolCalls(ctx context.Context, st stmts, at time.Time) error {
	if _, err := st.Exec(ctx, stmtCloseStrandedToolCalls, toolCallStranded, millis(at)); err != nil {
		return fmt.Errorf("close stranded tool calls: %w", err)
	}
	return nil
}

// closeStrandedLLMCalls ends every call a previous process left open, and says how many.
func closeStrandedLLMCalls(ctx context.Context, st stmts, at time.Time) (int64, error) {
	res, err := st.Exec(ctx, stmtCloseStrandedLLMCalls, llmCallStranded, millis(at))
	if err != nil {
		return 0, fmt.Errorf("close stranded llm calls: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil { // coverage:ignore modernc.org/sqlite's RowsAffected never fails
		return 0, fmt.Errorf("close stranded llm calls: %w", err)
	}
	return n, nil
}

// failStrandedRuns fails every run a previous process left unfinished and returns
// the chats they belong to, one entry per run under a chat. Their chats are
// not moved: the sends that stranded them did. chat_id is nullable, since a
// monitor's run has no chat.
func failStrandedRuns(ctx context.Context, st stmts, reason string, at time.Time) ([]ChatID, error) {
	rows, err := st.Query(ctx, stmtFailStrandedRuns, reason, millis(at))
	if err != nil {
		return nil, fmt.Errorf("fail stranded runs: %w", err)
	}
	defer rows.Close()

	var out []ChatID
	for rows.Next() {
		var id sql.NullString
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("fail stranded runs: %w", err)
		}
		if id.Valid {
			out = append(out, ChatID(id.String))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fail stranded runs: %w", err)
	}
	return out, nil
}

// listMessages is a chat's transcript in seq order, each answer with its tool
// calls. Two statements: a caller on the pools runs it in one read transaction so
// the two cannot tear.
func listMessages(ctx context.Context, st stmts, id ChatID, box tools.Box) ([]ChatMessage, error) {
	rows, err := st.Query(ctx, stmtSelectMessages, string(id))
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var out []ChatMessage
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("list messages: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	byRun, err := toolCallsByRun(ctx, st, chatCallReads, string(id))
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	withToolCalls(out, byRun, box)
	return out, nil
}

// scanMessage scans messageReadColumns. A user message has no run, so every run
// column is NULL and its status is Complete.
func scanMessage(s scanner) (ChatMessage, error) {
	var (
		m                                      ChatMessage
		runID, status, runErr, provider, model sql.NullString
		effort, finishReason, dialect          sql.NullString
		createdAt                              int64
		finishedAt                             sql.NullInt64
	)
	err := s.Scan(&m.ID, &m.ChatID, &m.Seq, &m.Role, &m.Content, &runID,
		&status, &runErr, &provider, &model, &effort, &finishedAt, &createdAt, &finishReason, &dialect, &m.AwaitingApproval)
	if err != nil {
		return ChatMessage{}, err
	}
	m.dialect, m.Citations = llm.Dialect(dialect.String), emptyCitations
	m.RunID = RunID(runID.String)
	m.Error, m.ProviderID, m.ModelID, m.Effort = runErr.String, provider.String, model.String, effort.String
	m.FinishReason = finishReason.String
	m.Status = StatusComplete
	if status.Valid {
		m.Status = messageStatusOf(RunStatus(status.String))
	}
	m.CreatedAt = fromMillis(createdAt)
	if finishedAt.Valid {
		m.FinishedAt = sql.NullTime{Time: fromMillis(finishedAt.Int64), Valid: true}
	}
	return m, nil
}

// --- clusters ---

// clusterAccepts is whether a send can be filed under the cluster: it exists and
// is not marked for deletion.
func clusterAccepts(ctx context.Context, st stmts, clusterID apimeta.ClusterID) (bool, error) {
	var one int
	err := st.QueryRow(ctx, stmtSelectClusterAccepts, string(clusterID)).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cluster accepts: %w", err)
	}
	return true, nil
}

// markedClusterIDs lists the clusters marked for deletion: whose chats the
// sweeper deletes.
func markedClusterIDs(ctx context.Context, st stmts) ([]apimeta.ClusterID, error) {
	return collectIDs[apimeta.ClusterID](ctx, st, stmtSelectMarkedClusterIDs, "marked clusters")
}

// clusterMonitoring is whether the cluster is there and unmarked, and whether
// its monitoring switch is on.
func clusterMonitoring(ctx context.Context, st stmts, clusterID apimeta.ClusterID) (found, enabled bool, err error) {
	err = st.QueryRow(ctx, stmtSelectClusterMonitoring, string(clusterID)).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("cluster monitoring: %w", err)
	}
	return true, enabled, nil
}

// liveClusterIDs lists the clusters not marked for deletion.
func liveClusterIDs(ctx context.Context, st stmts) ([]apimeta.ClusterID, error) {
	return collectIDs[apimeta.ClusterID](ctx, st, stmtSelectLiveClusterIDs, "live clusters")
}

// disabledTools is the names of the tools the user turned off, and of those
// whose stored setting Kubetail cannot use.
func disabledTools(ctx context.Context, st stmts) ([]string, error) {
	rows, err := st.Query(ctx, stmtSelectToolSettings)
	if err != nil {
		return nil, fmt.Errorf("disabled tools: %w", err)
	}
	return settings.OffTools(rows)
}

// collectIDs runs a statement whose rows are one id each.
func collectIDs[ID ~string](ctx context.Context, st stmts, stmt stmtID, what string, args ...any) ([]ID, error) {
	rows, err := st.Query(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	defer rows.Close()

	var out []ID
	for rows.Next() {
		var id ID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("%s: %w", what, err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return out, nil
}
