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

// Every statement chat issues, named once and prepared once on the app's DB
// (sqlstmt.Set). The text lives here and never at a call site.
package chat

import (
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
)

// stmtID indexes statements.
type stmtID int

const (
	stmtInsertChat stmtID = iota
	stmtTouchChat
	stmtRenameChat
	stmtSetSandboxDisabled
	stmtSetNetworkEnabled
	stmtDeleteChat
	stmtSelectChat
	stmtSelectChats
	stmtSelectChatIDsByCluster

	stmtNextSeq
	stmtInsertMessage
	stmtInsertRun
	stmtInsertSubagentRun
	stmtInsertMonitorRun
	stmtDeleteRun
	stmtClaimRun
	stmtWriteContent
	stmtFlipRun
	stmtSettleRun
	stmtFailStrandedRuns
	stmtSelectMessages
	stmtSelectAnswerByRequestKey
	stmtSelectNewestContext

	stmtInsertLLMCall
	stmtCloseLLMCall
	stmtCloseStrandedLLMCalls

	stmtUpsertToolCall
	stmtCloseStrandedToolCalls
	stmtUpsertApproval
	stmtSelectToolCalls
	stmtSelectRunToolCalls
	stmtSelectViewCall
	stmtSelectSandboxActions
	stmtSelectRunSandboxActions

	stmtInsertTask
	stmtDeleteTask
	stmtFinishTask
	stmtMarkLostTasks
	stmtSelectWaitingNotices
	stmtMarkNotified
	stmtSelectLastAnswerRun

	stmtSelectNewestRun
	stmtSelectLastContextUse

	stmtSelectClusterAccepts
	stmtSelectMarkedClusterIDs
	stmtSelectClusterMonitoring
	stmtSelectLiveClusterIDs

	stmtSelectToolSettings

	stmtSelectChatRules
	stmtInsertChatRule
	stmtDeleteChatRule
	stmtSelectChatFolderGrants
	stmtUpsertChatFolderGrant
	stmtDeleteChatFolderGrant
	numStmts int = iota
)

// chatColumns is the projection every chat read scans, in the
// order scanChat scans it. title is nullable in the table and a string in Go.
// The last is whether any run of the chat waits on the user.
const chatColumns = `id, COALESCE(title, ''), mode, cluster_id, sandbox_disabled, network_enabled, created_at, updated_at,
	EXISTS (SELECT 1 FROM agent_runs w WHERE w.chat_id = chats.id AND w.status = 'waiting_approval')`

// The insert projections, in the order the helpers bind them.
const (
	messageColumns = `id, chat_id, seq, role, content, request_key, run_id, created_at`
	runColumns     = `id, agent_type, app_version, trigger, chat_id, trigger_message_id,
	provider, model, effort, dialect, status, created_at`
	llmCallColumns  = `id, run_id, seq, provider, model, effort, started_at`
	toolCallColumns = `id, llm_call_id, seq, runs_on, tool_name, contract_name, tool_use_id, arguments, cwd, sandboxed, env, network, result, error,
	is_mutating, spawned_run_id, status, created_at, started_at, finished_at, shown_action`
	approvalColumns = `id, tool_call_id, kind, request, status, duration, reason, created_at, decided_at`
)

// toolCallReadColumns is what a read of the calls scans, in toolCallsByRun's order:
// the run, the row with the subagent run it spawned, its approval's id, status and duration,
// NULL on an ungated call, then the task it started, NULL on every other, with a
// completed agent's report off the run sr it started, then what the call's run
// showed and a finished watch's result. The reads alias tool_calls
// t, llm_calls c, a call's own approval a and background_tasks b; order is the model's,
// (c.seq, t.seq).
const toolCallReadColumns = `c.run_id, t.id, t.runs_on, t.tool_use_id, t.tool_name, t.contract_name, t.arguments, t.cwd, t.sandboxed, t.env, t.network, t.result, t.error,
	t.status, t.started_at, t.spawned_run_id, a.id, a.status, COALESCE(a.duration, ''), b.status, b.exit_code,
	CASE WHEN b.status = 'completed' THEN sr.result END, t.shown_action, b.result`

const toolCallReadFrom = ` FROM tool_calls t JOIN llm_calls c ON c.id = t.llm_call_id
	LEFT JOIN approvals a ON a.tool_call_id = t.id AND a.kind = 'call'
	LEFT JOIN background_tasks b ON b.tool_call_id = t.id
	LEFT JOIN agent_runs sr ON sr.id = t.spawned_run_id`

// sandboxActionReadColumns is what a read of the actions scans, in
// sandboxActionsByCall's order, over approvals a joined up to their run r. Only a
// pending action's body and diff are read: no other is served. A request that is
// not JSON is read as it is, for the read to refuse alone.
const sandboxActionReadColumns = `a.tool_call_id, a.id, a.status, COALESCE(a.duration, ''),
	CASE WHEN a.status = 'pending' OR NOT json_valid(a.request) THEN a.request
	ELSE json_remove(a.request, '$.write.body', '$.write.contentType', '$.diff', '$.cloud.body', '$.cloud.contentType') END,
	COALESCE(a.reason, ''), a.created_at, a.decided_at`

const sandboxActionReadFrom = ` FROM approvals a JOIN tool_calls t ON t.id = a.tool_call_id
	JOIN llm_calls c ON c.id = t.llm_call_id JOIN agent_runs r ON r.id = c.run_id`

// messageReadColumns is the public projection of a message: its row, then what its
// run says — status, error, what it was asked to run, when it finished — NULL on a
// user message, which has no run; then the run's latest stop reason, a correlated
// subselect over its llm_calls; then the run's dialect, which reads the content's
// citations; then whether its run or a subagent's under it waits on the user. The
// reads alias messages as m and agent_runs as r.
const messageReadColumns = `m.id, m.chat_id, m.seq, m.role, m.content, m.run_id,
	r.status, r.error, r.provider, r.model, r.effort, r.finished_at, m.created_at,
	(SELECT c.stop_reason FROM llm_calls c WHERE c.run_id = m.run_id AND c.stop_reason IS NOT NULL ORDER BY c.seq DESC LIMIT 1),
	r.dialect,
	EXISTS (SELECT 1 FROM agent_runs w WHERE (w.id = m.run_id OR w.parent_run_id = m.run_id) AND w.status = 'waiting_approval')`

const messageReadFrom = ` FROM messages m LEFT JOIN agent_runs r ON r.id = m.run_id`

// statements is the table: each id's text and the pool it is prepared on. A read
// that runs inside a write transaction is OnBoth — on the reader alone it would
// miss the transaction's own writes. statements_test.go refuses a write filed as a
// read.
var statements = []sqlstmt.Statement{
	stmtInsertChat: sqlstmt.OnWriter(`INSERT INTO chats (id, title, mode, cluster_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`),
	stmtTouchChat:  sqlstmt.OnWriter(`UPDATE chats SET updated_at = ? WHERE id = ?`),
	// RETURNING, so the renamed row comes back from the write itself: a read beside it
	// is a second statement a concurrent delete can land between.
	stmtRenameChat: sqlstmt.OnWriter(`UPDATE chats SET title = ?, updated_at = ? WHERE id = ? RETURNING ` + chatColumns),
	// Leaving the sandbox turns the network switch off: it is on only in the sandbox.
	stmtSetSandboxDisabled: sqlstmt.OnWriter(`UPDATE chats SET sandbox_disabled = ?1, network_enabled = network_enabled AND NOT ?1 WHERE id = ?2 RETURNING ` + chatColumns),
	stmtSetNetworkEnabled:  sqlstmt.OnWriter(`UPDATE chats SET network_enabled = ? WHERE id = ? RETURNING ` + chatColumns),
	// The messages and runs go with the chat: ON DELETE CASCADE, and
	// foreign_keys(on) is in the writer's DSN. One statement, so the two tables'
	// references to each other are checked once both are gone.
	stmtDeleteChat:             sqlstmt.OnWriter(`DELETE FROM chats WHERE id = ?`),
	stmtSelectChat:             sqlstmt.OnBoth(`SELECT ` + chatColumns + ` FROM chats WHERE id = ?`),
	stmtSelectChats:            sqlstmt.OnReader(`SELECT ` + chatColumns + ` FROM chats ORDER BY updated_at DESC, id DESC`),
	stmtSelectChatIDsByCluster: sqlstmt.OnReader(`SELECT id FROM chats WHERE cluster_id = ?`),

	// Inside the send's transaction, so two sends cannot take one seq.
	stmtNextSeq:       sqlstmt.OnBoth(`SELECT COALESCE(MAX(seq), -1) + 1 FROM messages WHERE chat_id = ?`),
	stmtInsertMessage: sqlstmt.OnWriter(`INSERT INTO messages (` + messageColumns + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`),
	stmtInsertRun: sqlstmt.OnWriter(`INSERT INTO agent_runs (` + runColumns + `)
	VALUES (?, 'chat', ?, 'chat', ?, ?, ?, ?, ?, ?, 'queued', ?)`),
	// A subagent's run goes in running: the goroutine that runs it is the one
	// inserting it, so there is nothing to claim.
	stmtInsertSubagentRun: sqlstmt.OnWriter(`INSERT INTO agent_runs (id, parent_run_id, agent_type, app_version, trigger,
	chat_id, provider, model, effort, dialect, task, status, created_at, started_at)
	VALUES (?, ?, ?, ?, 'agent', ?, ?, ?, ?, ?, ?, 'running', ?, ?)`),
	// A monitor's run goes in queued under its cluster, with no chat, and its
	// first round claims it as a turn's does.
	stmtInsertMonitorRun: sqlstmt.OnWriter(`INSERT INTO agent_runs (id, agent_type, app_version, trigger,
	cluster_id, provider, model, effort, dialect, task, status, created_at)
	VALUES (?, 'monitor', ?, 'monitor', ?, ?, ?, ?, ?, ?, 'queued', ?)`),
	stmtDeleteRun: sqlstmt.OnWriter(`DELETE FROM agent_runs WHERE id = ?`),
	// Guarded on queued, so a run a cancel settled first is not restarted.
	stmtClaimRun:     sqlstmt.OnWriter(`UPDATE agent_runs SET status = 'running', started_at = ? WHERE id = ? AND status = 'queued'`),
	stmtWriteContent: sqlstmt.OnWriter(`UPDATE messages SET content = ? WHERE id = ?`),
	// Between running and waiting_approval, while a command waits on the user.
	stmtFlipRun: sqlstmt.OnWriter(`UPDATE agent_runs SET status = ? WHERE id = ?`),
	// result is a subagent's final text, whole, before the call cuts it to fit; NULL
	// on a chat run.
	stmtSettleRun: sqlstmt.OnWriter(`UPDATE agent_runs SET status = ?, result = ?, error = ?, finished_at = ? WHERE id = ?`),
	// RETURNING the chat, so the watchers of each stranded chat can be told.
	stmtFailStrandedRuns: sqlstmt.OnWriter(`UPDATE agent_runs SET status = 'failed', error = ?, finished_at = ?
	WHERE status IN ('queued', 'running', 'waiting_approval') RETURNING chat_id`),
	stmtSelectMessages: sqlstmt.OnReader(`SELECT ` + messageReadColumns + messageReadFrom + ` WHERE m.chat_id = ? ORDER BY m.seq`),
	// The message carrying the key, the run it triggered, and that run's answer — one
	// statement, so a delete committing between them cannot show a message with no
	// answer. The answer is aliased m so the projection is the transcript read's.
	stmtSelectAnswerByRequestKey: sqlstmt.OnBoth(`SELECT ` + messageReadColumns + `
	FROM messages u
	JOIN agent_runs r ON r.trigger_message_id = u.id
	JOIN messages m ON m.run_id = r.id
	WHERE u.request_key = ?`),
	// The context block is always its question's first: that is where it is read.
	stmtSelectNewestContext: sqlstmt.OnBoth(`SELECT json_extract(content, '$[0].text') FROM messages
	WHERE chat_id = ? AND role = 'user' AND json_extract(content, '$[0].type') = 'context'
	ORDER BY seq DESC LIMIT 1`),

	stmtInsertLLMCall: sqlstmt.OnWriter(`INSERT INTO llm_calls (` + llmCallColumns + `) VALUES (?, ?, ?, ?, ?, ?, ?)`),
	stmtCloseLLMCall: sqlstmt.OnWriter(`UPDATE llm_calls SET model = ?, stop_reason = ?, error = ?, server_uses = ?,
	input_tokens = ?, cache_read_tokens = ?, cache_write_tokens = ?, output_tokens = ?, first_chunk_at = ?, finished_at = ?
	WHERE id = ?`),
	// A call still open at startup belongs to a process that is gone.
	stmtCloseStrandedLLMCalls: sqlstmt.OnWriter(`UPDATE llm_calls SET error = ?, finished_at = ? WHERE finished_at IS NULL`),

	// A tool call's row is written whole every time: the running row before the tool
	// runs, the outcome after, and again at settlement, which heals a write that was
	// lost. A call the loop refused without running has no earlier row, and the same
	// statement writes its first. cwd, sandboxed and env are written with the first row
	// and never after; network with the row that marks the call running, which
	// can be the second.
	// spawned_run_id is the entry's, which carries the link from the subagent's
	// insert on, so every later write keeps it.
	stmtUpsertToolCall: sqlstmt.OnWriter(`INSERT INTO tool_calls (` + toolCallColumns + `)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
	tool_name = excluded.tool_name, tool_use_id = excluded.tool_use_id, arguments = excluded.arguments, network = excluded.network,
	result = excluded.result, error = excluded.error, is_mutating = excluded.is_mutating,
	spawned_run_id = excluded.spawned_run_id, status = excluded.status,
	started_at = excluded.started_at, finished_at = excluded.finished_at, shown_action = excluded.shown_action`),
	stmtCloseStrandedToolCalls: sqlstmt.OnWriter(`UPDATE tool_calls SET status = 'failed', error = ?, finished_at = ?
	WHERE finished_at IS NULL AND runs_on = 'sidecar'`),
	// An approval is written whole like its call: pending with the request, then the
	// decision, then again at the settle.
	stmtUpsertApproval: sqlstmt.OnWriter(`INSERT INTO approvals (` + approvalColumns + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET status = excluded.status, duration = excluded.duration, reason = excluded.reason,
	decided_at = excluded.decided_at`),
	// A chat's calls, beside its messages. OnBoth, since the repeated send's read
	// runs inside the send's transaction.
	stmtSelectToolCalls: sqlstmt.OnBoth(`SELECT ` + toolCallReadColumns + toolCallReadFrom + `
	JOIN agent_runs r ON r.id = c.run_id WHERE r.chat_id = ? ORDER BY c.run_id, c.seq, t.seq`),
	// A run's calls and its subagents', which its answer lists too.
	stmtSelectRunToolCalls: sqlstmt.OnBoth(`SELECT ` + toolCallReadColumns + toolCallReadFrom + `
	JOIN agent_runs r ON r.id = c.run_id WHERE r.id = ?1 OR r.parent_run_id = ?1 ORDER BY c.run_id, c.seq, t.seq`),
	// A call of the chat by id, with what its run showed and, for a watch, its
	// task's result. OnBoth: Send reads it inside its write transaction.
	stmtSelectViewCall: sqlstmt.OnBoth(`SELECT COALESCE(t.tool_use_id, ''), t.tool_name, t.contract_name, t.shown_action, b.result FROM tool_calls t
	JOIN llm_calls l ON l.id = t.llm_call_id JOIN agent_runs r ON r.id = l.run_id
	LEFT JOIN background_tasks b ON b.tool_call_id = t.id
	WHERE t.id = ? AND r.chat_id = ?`),
	// The sandbox actions of the same calls, in the order asked.
	stmtSelectSandboxActions: sqlstmt.OnBoth(`SELECT ` + sandboxActionReadColumns + sandboxActionReadFrom + `
	WHERE a.kind = 'action' AND r.chat_id = ? ORDER BY a.created_at, a.id`),
	stmtSelectRunSandboxActions: sqlstmt.OnBoth(`SELECT ` + sandboxActionReadColumns + sandboxActionReadFrom + `
	WHERE a.kind = 'action' AND (r.id = ?1 OR r.parent_run_id = ?1) ORDER BY a.created_at, a.id`),

	// A task's row goes in running before its process starts, and is deleted when
	// the start fails, so no row stands for a process that never ran.
	stmtInsertTask: sqlstmt.OnWriter(`INSERT INTO background_tasks (id, chat_id, tool_call_id, output_path, status, started_at)
	VALUES (?, ?, ?, ?, 'running', ?)`),
	stmtDeleteTask: sqlstmt.OnWriter(`DELETE FROM background_tasks WHERE id = ?`),
	stmtFinishTask: sqlstmt.OnWriter(`UPDATE background_tasks SET status = ?, stopped_by = ?, exit_code = ?, finished_at = ?, notified_at = ?, result = ?
	WHERE id = ?`),
	// A task still running at startup belongs to a process that is gone. RETURNING the
	// chat, so the watchers of each chat can be told.
	stmtMarkLostTasks: sqlstmt.OnWriter(`UPDATE background_tasks SET status = 'lost', finished_at = ?
	WHERE status = 'running' RETURNING chat_id`),
	// A chat's finished tasks whose notices have not reached the model, with the
	// call that started each, for what it ran, the Agent call a that ran it when
	// a subagent's call did, the run r an Agent call started, which marks the task
	// an agent's, for its report and error, and whether the call's own run answered a question a user sent (its
	// trigger carries a request key), then what the call's run showed and a watch's
	// result; in the order they ended. The tool_use_id is
	// a's then, the one call of the two the model reading the notice saw. OnBoth,
	// since a send reads them inside its transaction.
	stmtSelectWaitingNotices: sqlstmt.OnBoth(`SELECT b.id, COALESCE(a.tool_use_id, t.tool_use_id), b.output_path, b.status,
	b.stopped_by, b.exit_code, t.id, t.tool_name, t.contract_name, t.arguments, t.cwd, t.sandboxed, a.tool_name, a.contract_name, a.arguments,
	r.id IS NOT NULL, r.result, r.error,
	EXISTS (SELECT 1 FROM agent_runs p JOIN messages q ON q.id = p.trigger_message_id
		WHERE p.id = c.run_id AND q.request_key IS NOT NULL), t.shown_action, b.result
	FROM background_tasks b JOIN tool_calls t ON t.id = b.tool_call_id
	JOIN llm_calls c ON c.id = t.llm_call_id
	LEFT JOIN tool_calls a ON a.spawned_run_id = c.run_id
	LEFT JOIN agent_runs r ON r.id = t.spawned_run_id
	WHERE b.chat_id = ? AND b.status <> 'running' AND b.notified_at IS NULL
	ORDER BY b.finished_at, b.id`),
	stmtMarkNotified: sqlstmt.OnWriter(`UPDATE background_tasks SET notified_at = ?
	WHERE chat_id = ? AND status <> 'running' AND notified_at IS NULL`),
	// What the chat's last answer ran on, which a turn the sidecar starts runs on too.
	stmtSelectLastAnswerRun: sqlstmt.OnBoth(`SELECT r.provider, r.model, r.effort
	FROM messages m JOIN agent_runs r ON r.id = m.run_id
	WHERE m.chat_id = ? ORDER BY m.seq DESC LIMIT 1`),

	// The chat's newest turn, and whether its first model call ended on an error. The join
	// through messages reaches the chat's own runs alone.
	stmtSelectNewestRun: sqlstmt.OnBoth(`SELECT r.status, COALESCE(r.error, ''), r.provider, r.model,
	EXISTS (SELECT 1 FROM llm_calls c WHERE c.run_id = r.id AND c.seq = 0 AND c.error IS NOT NULL)
	FROM agent_runs r JOIN messages m ON m.run_id = r.id
	WHERE m.chat_id = ? ORDER BY m.seq DESC LIMIT 1`),
	// The newest reported call among the chat's succeeded turns on a provider that
	// ran no server tool: inside one request the provider samples again after each
	// server call, and whether its usage counts the chat once or once per sampling
	// is unverified. The join through messages reaches the chat's own runs alone: a
	// subagent's has no message.
	stmtSelectLastContextUse: sqlstmt.OnBoth(`SELECT c.input_tokens, c.cache_read_tokens, c.cache_write_tokens, c.output_tokens
	FROM llm_calls c JOIN agent_runs r ON r.id = c.run_id JOIN messages m ON m.run_id = r.id
	WHERE m.chat_id = ? AND r.provider = ? AND r.status = 'succeeded' AND c.input_tokens IS NOT NULL
	AND NOT EXISTS (SELECT 1 FROM json_each(c.server_uses) WHERE value > 0)
	ORDER BY m.seq DESC, c.seq DESC LIMIT 1`),

	// A send lands under a cluster that exists and is not marked; inside the send's
	// transaction, so a mark cannot commit between the check and the rows.
	stmtSelectClusterAccepts: sqlstmt.OnBoth(`SELECT 1 FROM clusters WHERE id = ? AND delete_requested_at IS NULL`),

	// The clusters whose chats the sweeper deletes.
	stmtSelectMarkedClusterIDs: sqlstmt.OnReader(`SELECT id FROM clusters WHERE delete_requested_at IS NOT NULL ORDER BY id`),
	// A monitor's run checks its cluster inside the transaction that inserts it,
	// as a send does.
	stmtSelectClusterMonitoring: sqlstmt.OnBoth(`SELECT monitoring_enabled FROM clusters WHERE id = ? AND delete_requested_at IS NULL`),
	// The clusters a monitor may be kept under: the sweeper ends every other.
	stmtSelectLiveClusterIDs: sqlstmt.OnReader(`SELECT id FROM clusters WHERE delete_requested_at IS NULL`),

	// Every tool's setting for every cluster (services/toolsettings writes
	// them). OnBoth, since a send reads them in the transaction that reserves
	// its turn.
	stmtSelectToolSettings: sqlstmt.OnBoth(settings.SelectToolSettings),

	// OnBoth, since a rule's write reads the chat's rules in its transaction.
	stmtSelectChatRules: sqlstmt.OnBoth(`SELECT ` + settings.RuleColumns + ` FROM permission_rules WHERE chat_id = ? ORDER BY created_at, id`),
	stmtInsertChatRule: sqlstmt.OnWriter(`INSERT INTO permission_rules (` + settings.RuleColumns + `, chat_id, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
	stmtDeleteChatRule: sqlstmt.OnWriter(`DELETE FROM permission_rules WHERE chat_id = ? AND id = ?`),
	// OnBoth, since a grant's write reads the chat in its transaction.
	stmtSelectChatFolderGrants: sqlstmt.OnBoth(`SELECT ` + settings.FolderGrantColumns + ` FROM folder_grants WHERE chat_id = ? ORDER BY created_at, id`),
	// A folder granted again keeps its row's id and created_at, and changes its access.
	stmtUpsertChatFolderGrant: sqlstmt.OnWriter(`INSERT INTO folder_grants (id, chat_id, folder, access, created_at) VALUES (?, ?, ?, ?, ?)
	ON CONFLICT (ifnull(chat_id, ''), folder) DO UPDATE SET access = excluded.access`),
	stmtDeleteChatFolderGrant: sqlstmt.OnWriter(`DELETE FROM folder_grants WHERE chat_id = ? AND id = ?`),
}

// stmts issues the set's statements, on the pools or inside a transaction.
type stmts = sqlstmt.Stmts[stmtID]
