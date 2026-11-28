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

package appdb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amorey/gobus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlitemigrate"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlitemigrate/sqlitemigratetest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// Open creates a missing parent dir, runs the embedded migrations (recording
// the sequence in schema_migrations), and the file survives a close/reopen
// cycle.
func TestOpenCreatesMigratesAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "app.db")

	db, err := Open(path, 0)
	require.NoError(t, err)
	require.FileExists(t, path)

	// Migrations ran: the schema_migrations sequence is at version 1.
	var v int
	require.NoError(t, db.Write.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&v))
	require.Equal(t, 1, v)
	requireTables(t, db.Write.DB)
	require.NoError(t, db.Close())

	// Reopening the same file is idempotent — migrations are not re-applied.
	db2, err := Open(path, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db2.Close() })
	require.NoError(t, db2.Write.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&v))
	require.Equal(t, 1, v)
	requireTables(t, db2.Write.DB)
}

// The reader is opened through OpenReader, not a second OpenWriter: query_only is in
// its DSN, so a write there is refused — and a read there sees what the writer
// committed, which is the two pools sharing one file.
func TestOpenReturnsAWriterAndAReader(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "app.db"), 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Write.Exec(`INSERT INTO clusters (id, source, created_at, updated_at) VALUES ('c', 'cloud', 0, 0)`)
	require.NoError(t, err)
	const insert = `INSERT INTO chats (id, cluster_id, mode, created_at, updated_at) VALUES (?, 'c', 'chat', 0, 0)`
	_, err = db.Read.Exec(insert, "r")
	require.ErrorContains(t, err, "attempt to write a readonly database")
	_, err = db.Write.Exec(insert, "w")
	require.NoError(t, err)

	var id string
	require.NoError(t, db.Read.QueryRow(`SELECT id FROM chats`).Scan(&id))
	require.Equal(t, "w", id)
}

// A chat's messages and runs reference each other both ways, and neither
// cascades off the other: the run keeps its message, the answer keeps its run. One
// DELETE of the chat takes all of them, because SQLite checks a foreign key
// at the end of the statement — after the cascade off chats has removed
// both sides.
func TestDeletingAChatTakesItsMessagesAndRuns(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "app.db"), 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	for _, q := range []string{
		`INSERT INTO clusters (id, source, created_at, updated_at) VALUES ('k', 'cloud', 0, 0)`,
		`INSERT INTO chats (id, cluster_id, mode, created_at, updated_at) VALUES ('c', 'k', 'chat', 0, 0)`,
		`INSERT INTO messages (id, chat_id, seq, role, content, request_key, created_at) VALUES ('u', 'c', 0, 'user', '[]', 'key', 0)`,
		`INSERT INTO agent_runs (id, agent_type, app_version, trigger, chat_id, trigger_message_id, provider, model, dialect, created_at)
		 VALUES ('r', 'chat', 'dev', 'chat', 'c', 'u', 'p', 'm', 'fake', 0)`,
		`INSERT INTO messages (id, chat_id, seq, role, content, run_id, created_at) VALUES ('a', 'c', 1, 'assistant', '[]', 'r', 0)`,
	} {
		_, err := db.Write.Exec(q)
		require.NoError(t, err, q)
	}

	_, err = db.Write.Exec(`DELETE FROM chats WHERE id = 'c'`)
	require.NoError(t, err)
	for _, table := range []string{"messages", "agent_runs"} {
		var n int
		require.NoError(t, db.Read.QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&n))
		require.Zero(t, n, table)
	}
}

// The two keys the send transaction relies on: a seq handed out twice in one
// chat is refused, and a message starts at most one run.
func TestMessagesAndRunsAreKeyedForOneTurn(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "app.db"), 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	for _, q := range []string{
		`INSERT INTO clusters (id, source, created_at, updated_at) VALUES ('k', 'cloud', 0, 0)`,
		`INSERT INTO chats (id, cluster_id, mode, created_at, updated_at) VALUES ('c', 'k', 'chat', 0, 0)`,
		`INSERT INTO messages (id, chat_id, seq, role, content, created_at) VALUES ('u', 'c', 0, 'user', '[]', 0)`,
		`INSERT INTO agent_runs (id, agent_type, app_version, trigger, chat_id, trigger_message_id, provider, model, dialect, created_at)
		 VALUES ('r', 'chat', 'dev', 'chat', 'c', 'u', 'p', 'm', 'fake', 0)`,
	} {
		_, err := db.Write.Exec(q)
		require.NoError(t, err, q)
	}

	_, err = db.Write.Exec(`INSERT INTO messages (id, chat_id, seq, role, content, created_at) VALUES ('u2', 'c', 0, 'user', '[]', 0)`)
	require.ErrorContains(t, err, "UNIQUE")
	_, err = db.Write.Exec(`INSERT INTO agent_runs (id, agent_type, app_version, trigger, chat_id, trigger_message_id, provider, model, dialect, created_at)
		 VALUES ('r2', 'chat', 'dev', 'chat', 'c', 'u', 'p', 'm', 'fake', 0)`)
	require.ErrorContains(t, err, "UNIQUE")
}

// requireTables asserts the schema the migrations create: exactly the
// application tables (schema_migrations is the migrator's), each STRICT,
// the small-row ones WITHOUT ROWID, and the named indexes as declared — partial
// WHEREs and column order included.
func requireTables(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`SELECT name, wr, strict FROM pragma_table_list
		WHERE schema = 'main' AND type = 'table' AND name NOT LIKE 'sqlite_%' AND name <> 'schema_migrations' ORDER BY name`)
	require.NoError(t, err)
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		var withoutRowid, strict int
		require.NoError(t, rows.Scan(&name, &withoutRowid, &strict))
		tables = append(tables, fmt.Sprintf("%s wr=%d strict=%d", name, withoutRowid, strict))
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{
		"agent_runs wr=0 strict=1",
		"approvals wr=1 strict=1",
		"background_tasks wr=1 strict=1",
		"chats wr=1 strict=1",
		"clusters wr=1 strict=1",
		"env_vars wr=0 strict=1",
		"executables wr=0 strict=1",
		"folder_grants wr=0 strict=1",
		"incident_occurrences wr=1 strict=1",
		"incidents wr=0 strict=1",
		"llm_calls wr=0 strict=1",
		"memories wr=0 strict=1",
		"messages wr=0 strict=1",
		"monitor_alert_seq wr=0 strict=1",
		"monitor_targets wr=0 strict=1",
		"monitor_triage_starts wr=0 strict=1",
		"oauth_connections wr=1 strict=1",
		"permission_modes wr=0 strict=1",
		"permission_rules wr=0 strict=1",
		"settings wr=0 strict=1",
		"shell_path wr=0 strict=1",
		"tool_calls wr=0 strict=1",
	}, tables)

	idx, err := db.Query(`SELECT sql FROM sqlite_master WHERE type = 'index' AND sql IS NOT NULL ORDER BY name`)
	require.NoError(t, err)
	defer idx.Close()
	var indexes []string
	for idx.Next() {
		var ddl string
		require.NoError(t, idx.Scan(&ddl))
		indexes = append(indexes, strings.Join(strings.Fields(ddl), " "))
	}
	require.NoError(t, idx.Err())
	require.Equal(t, []string{
		"CREATE INDEX agent_runs_chat_idx ON agent_runs (chat_id)",
		"CREATE INDEX agent_runs_cluster_idx ON agent_runs (cluster_id)",
		"CREATE INDEX agent_runs_parent_idx ON agent_runs (parent_run_id)",
		"CREATE INDEX agent_runs_queued_idx ON agent_runs (id) WHERE status = 'queued'",
		"CREATE INDEX agent_runs_waiting_idx ON agent_runs (chat_id) WHERE status = 'waiting_approval'",
		"CREATE INDEX approvals_actions_idx ON approvals (tool_call_id) WHERE kind = 'action'",
		"CREATE UNIQUE INDEX approvals_call_idx ON approvals (tool_call_id) WHERE kind = 'call'",
		"CREATE INDEX approvals_pending_idx ON approvals (id) WHERE status = 'pending'",
		"CREATE INDEX background_tasks_chat_idx ON background_tasks (chat_id)",
		"CREATE UNIQUE INDEX clusters_one_default ON clusters (is_default) WHERE is_default = 1",
		"CREATE UNIQUE INDEX env_vars_name ON env_vars (ifnull(cluster_id, ''), name)",
		"CREATE INDEX folder_grants_chat_idx ON folder_grants (chat_id, created_at, id)",
		"CREATE UNIQUE INDEX folder_grants_folder ON folder_grants (ifnull(chat_id, ''), folder)",
		"CREATE UNIQUE INDEX incident_occurrences_one_open ON incident_occurrences (incident_id) WHERE ended_at IS NULL",
		"CREATE UNIQUE INDEX incidents_alerted ON incidents (alert_seq) WHERE alert_seq IS NOT NULL",
		"CREATE INDEX incidents_last_seen ON incidents (last_seen_at)",
		"CREATE INDEX incidents_subject ON incidents (target_id, target_uid, signal, subject_key)",
		"CREATE INDEX incidents_triage_run ON incidents (triage_run_id) WHERE triage_run_id IS NOT NULL",
		"CREATE UNIQUE INDEX llm_calls_run_idx ON llm_calls (run_id, seq)",
		"CREATE UNIQUE INDEX memories_name ON memories (ifnull(cluster_id, ''), name)",
		"CREATE INDEX messages_run_idx ON messages (run_id)",
		"CREATE INDEX monitor_triage_starts_target ON monitor_triage_starts (target_id, started_at)",
		"CREATE INDEX permission_rules_chat_idx ON permission_rules (chat_id, created_at, id)",
		"CREATE UNIQUE INDEX settings_scope_key ON settings (ifnull(cluster_id, ''), key)",
		"CREATE UNIQUE INDEX tool_calls_llm_call_idx ON tool_calls (llm_call_id, seq)",
		"CREATE INDEX tool_calls_spawned_idx ON tool_calls (spawned_run_id)",
	}, indexes)
}

// foreignKeys is a table's references as (column → table.column, on delete).
func foreignKeys(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT "from", "table", "to", on_delete FROM pragma_foreign_key_list(?) ORDER BY id`, table)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var from, to, col, onDelete string
		require.NoError(t, rows.Scan(&from, &to, &col, &onDelete))
		out = append(out, fmt.Sprintf("%s -> %s.%s %s", from, to, col, onDelete))
	}
	require.NoError(t, rows.Err())
	return out
}

// The call tables hang off the runs: a model call cascades off its run, a tool
// call off its model call, an approval off its tool call, and a spawned run's
// deletion nulls the tool call that spawned it. One DELETE of the chat
// takes every row of the turn, and the foreign keys check clean before and after.
func TestTheCallTablesCascadeOffTheRun(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "app.db"), 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.Equal(t, []string{"run_id -> agent_runs.id CASCADE"}, foreignKeys(t, db.Read.DB, "llm_calls"))
	require.Equal(t, []string{"spawned_run_id -> agent_runs.id SET NULL", "llm_call_id -> llm_calls.id CASCADE"}, foreignKeys(t, db.Read.DB, "tool_calls"))
	require.Equal(t, []string{"tool_call_id -> tool_calls.id CASCADE"}, foreignKeys(t, db.Read.DB, "approvals"))
	require.Equal(t, []string{"tool_call_id -> tool_calls.id CASCADE", "chat_id -> chats.id CASCADE"},
		foreignKeys(t, db.Read.DB, "background_tasks"))

	for _, q := range []string{
		`INSERT INTO clusters (id, source, created_at, updated_at) VALUES ('k', 'cloud', 0, 0)`,
		`INSERT INTO chats (id, cluster_id, mode, created_at, updated_at) VALUES ('c', 'k', 'chat', 0, 0)`,
		`INSERT INTO messages (id, chat_id, seq, role, content, request_key, created_at) VALUES ('u', 'c', 0, 'user', '[]', 'key', 0)`,
		`INSERT INTO agent_runs (id, agent_type, app_version, trigger, chat_id, trigger_message_id, provider, model, dialect, created_at)
		 VALUES ('r', 'chat', 'dev', 'chat', 'c', 'u', 'p', 'm', 'fake', 0)`,
		`INSERT INTO agent_runs (id, parent_run_id, agent_type, app_version, trigger, chat_id, provider, model, dialect, created_at)
		 VALUES ('child', 'r', 'chat', 'dev', 'agent', 'c', 'p', 'm', 'fake', 0)`,
		`INSERT INTO llm_calls (id, run_id, seq, provider, model, started_at) VALUES ('l', 'r', 0, 'p', 'm', 0)`,
		`INSERT INTO tool_calls (id, llm_call_id, seq, tool_name, status, spawned_run_id, created_at) VALUES ('t', 'l', 0, 'spawn', 'running', 'child', 0)`,
		`INSERT INTO approvals (id, tool_call_id, created_at) VALUES ('a', 't', 0)`,
		`INSERT INTO background_tasks (id, chat_id, tool_call_id, output_path, status, started_at) VALUES ('b', 'c', 't', '/o', 'running', 0)`,
	} {
		_, err := db.Write.Exec(q)
		require.NoError(t, err, q)
	}
	requireForeignKeysClean(t, db.Read.DB)

	_, err = db.Write.Exec(`DELETE FROM agent_runs WHERE id = 'child'`)
	require.NoError(t, err)
	var spawned *string
	require.NoError(t, db.Read.QueryRow(`SELECT spawned_run_id FROM tool_calls WHERE id = 't'`).Scan(&spawned))
	require.Nil(t, spawned, "the spawned run's deletion nulls the pointer")

	_, err = db.Write.Exec(`DELETE FROM chats WHERE id = 'c'`)
	require.NoError(t, err)
	for _, table := range []string{"messages", "agent_runs", "llm_calls", "tool_calls", "approvals", "background_tasks"} {
		var n int
		require.NoError(t, db.Read.QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&n))
		require.Zero(t, n, table)
	}
	requireForeignKeysClean(t, db.Read.DB)
}

// A cluster's memories go with its row; a chat's delete leaves the memories it
// wrote and clears the pointer. A name is unique within a scope, the global scope
// included, though NULL never equals NULL.
func TestMemoriesHangOffTheirClusterAndOutliveTheirChat(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "app.db"), 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.Equal(t, []string{"chat_id -> chats.id SET NULL", "cluster_id -> clusters.id CASCADE"},
		foreignKeys(t, db.Read.DB, "memories"))

	for _, q := range []string{
		`INSERT INTO clusters (id, source, created_at, updated_at) VALUES ('k', 'cloud', 0, 0)`,
		`INSERT INTO chats (id, cluster_id, mode, created_at, updated_at) VALUES ('c', 'k', 'chat', 0, 0)`,
		`INSERT INTO memories (id, cluster_id, name, body, written_by, chat_id, created_at, updated_at)
		 VALUES ('m1', 'k', 'n', 'b', 'model', 'c', 0, 0)`,
		`INSERT INTO memories (id, cluster_id, name, body, written_by, created_at, updated_at)
		 VALUES ('m2', NULL, 'n', 'b', 'user', 0, 0)`,
		`INSERT INTO memories (id, cluster_id, name, body, written_by, created_at, updated_at)
		 VALUES ('m4', NULL, 'o', 'b', 'model', 0, 0)`,
	} {
		_, err := db.Write.Exec(q)
		require.NoError(t, err, q)
	}
	_, err = db.Write.Exec(`INSERT INTO memories (id, cluster_id, name, body, written_by, created_at, updated_at)
		VALUES ('m3', NULL, 'n', 'b', 'user', 0, 0)`)
	require.Error(t, err, "a second global memory under one name")
	_, err = db.Write.Exec(`INSERT INTO memories (id, cluster_id, server_uid, name, body, written_by, created_at, updated_at)
		VALUES ('m5', NULL, 'uid', 'h', 'b', 'user', 0, 0)`)
	require.Error(t, err, "a global memory with a server UID")

	_, err = db.Write.Exec(`DELETE FROM chats WHERE id = 'c'`)
	require.NoError(t, err)
	var chatID *string
	require.NoError(t, db.Read.QueryRow(`SELECT chat_id FROM memories WHERE id = 'm1'`).Scan(&chatID))
	require.Nil(t, chatID)

	_, err = db.Write.Exec(`DELETE FROM clusters WHERE id = 'k'`)
	require.NoError(t, err)
	var ids []string
	rows, err := db.Read.Query(`SELECT id FROM memories`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"m2", "m4"}, ids)
	requireForeignKeysClean(t, db.Read.DB)
}

func requireForeignKeysClean(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	require.NoError(t, err)
	defer rows.Close()
	require.False(t, rows.Next(), "foreign_key_check reported a violation")
}

// The CHECK constraints refuse what the app never writes: a status, trigger, mode
// or role outside its list, and a flag that is not 0 or 1.
func TestTheChecksRefuseValuesOutsideTheirLists(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "app.db"), 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{
		`INSERT INTO clusters (id, source, created_at, updated_at) VALUES ('k', 'cloud', 0, 0)`,
		`INSERT INTO chats (id, cluster_id, mode, created_at, updated_at) VALUES ('c', 'k', 'chat', 0, 0)`,
		`INSERT INTO agent_runs (id, agent_type, app_version, trigger, chat_id, provider, model, dialect, created_at) VALUES ('r', 'chat', 'dev', 'chat', 'c', 'p', 'm', 'fake', 0)`,
		`INSERT INTO llm_calls (id, run_id, seq, provider, model, started_at) VALUES ('l', 'r', 0, 'p', 'm', 0)`,
		`INSERT INTO tool_calls (id, llm_call_id, seq, tool_name, created_at) VALUES ('t', 'l', 0, 'list', 0)`,
	} {
		_, err := db.Write.Exec(q)
		require.NoError(t, err, q)
	}
	for _, q := range []string{
		`INSERT INTO clusters (id, source, created_at, updated_at) VALUES ('k2', 'file', 0, 0)`,
		`INSERT INTO clusters (id, source, enabled, created_at, updated_at) VALUES ('k2', 'cloud', 2, 0, 0)`,
		`INSERT INTO chats (id, cluster_id, mode, created_at, updated_at) VALUES ('c2', 'k', 'monitor', 0, 0)`,
		`INSERT INTO messages (id, chat_id, seq, role, content, created_at) VALUES ('m', 'c', 0, 'system', '[]', 0)`,
		`INSERT INTO agent_runs (id, agent_type, app_version, trigger, chat_id, provider, model, dialect, created_at) VALUES ('r2', 'chat', 'dev', 'cron', 'c', 'p', 'm', 'fake', 0)`,
		`UPDATE agent_runs SET status = 'done' WHERE id = 'r'`,
		`UPDATE tool_calls SET status = 'ok' WHERE id = 't'`,
		`UPDATE tool_calls SET is_mutating = 2 WHERE id = 't'`,
		`INSERT INTO approvals (id, tool_call_id, status, created_at) VALUES ('a', 't', 'maybe', 0)`,
	} {
		_, err := db.Write.Exec(q)
		require.ErrorContains(t, err, "CHECK", q)
	}
	var status string
	require.NoError(t, db.Read.QueryRow(`SELECT status FROM tool_calls WHERE id = 't'`).Scan(&status))
	require.Equal(t, "pending", status, "the column's default")
}

// The clusters table's defaults: a row inserted with nothing but its identity and
// stamps is enabled and syncing; the source claim is unique.
func TestClustersTableDefaultsAndClaim(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "app.db"), 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	const insert = `INSERT INTO clusters (id, source, source_key, created_at, updated_at) VALUES (?, 'kubeconfig', 'dev', 0, 0)`
	_, err = db.Write.Exec(insert, NewID())
	require.NoError(t, err)
	_, err = db.Write.Exec(insert, NewID())
	require.ErrorContains(t, err, "UNIQUE")

	var enabled, syncEnabled int
	var deleteRequestedAt *int64
	require.NoError(t, db.Read.QueryRow(`SELECT enabled, sync_enabled, delete_requested_at FROM clusters`).
		Scan(&enabled, &syncEnabled, &deleteRequestedAt))
	require.Equal(t, []int{1, 1}, []int{enabled, syncEnabled})
	require.Nil(t, deleteRequestedAt)
}

// The monitor's tables: a target is enabled by default with an empty read state,
// held as an object, one row per integration's key; an incident and its occurrences go with their
// target; an incident holds one open occurrence at most, and an occurrence's end
// and its reason come together, never before its start.
func TestTheMonitorTablesHoldWhatTheirChecksAllow(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "app.db"), 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	exec := func(q string, args ...any) error { _, err := db.Write.Exec(q, args...); return err }

	const target = `INSERT INTO monitor_targets (id, integration, key, label, created_at, updated_at) VALUES (?, 'kubernetes', ?, 'dev', 0, 0)`
	require.NoError(t, exec(target, "t1", "c1"))
	require.ErrorContains(t, exec(target, "t2", "c1"), "UNIQUE")
	var enabled, generation int
	var unknown, baselines string
	var uid, readAt *string
	require.NoError(t, db.Read.QueryRow(`SELECT enabled, generation, unknown_signals, baselines, current_uid, last_read_at FROM monitor_targets`).
		Scan(&enabled, &generation, &unknown, &baselines, &uid, &readAt))
	assert.Equal(t, []any{1, 0, "{}", "{}"}, []any{enabled, generation, unknown, baselines})
	assert.Nil(t, uid)
	assert.Nil(t, readAt)
	assert.ErrorContains(t, exec(`UPDATE monitor_targets SET unknown_signals = 'nope'`), "CHECK")
	assert.ErrorContains(t, exec(`UPDATE monitor_targets SET unknown_signals = '[]'`), "CHECK")
	assert.ErrorContains(t, exec(`UPDATE monitor_targets SET baselines = 'nope'`), "CHECK")

	const incident = `INSERT INTO incidents (id, target_id, target_uid, subject_key, subject_label, signal, signal_label, severity, first_seen_at, last_seen_at, occurrence_count)
		VALUES (?, 't1', 'uid', 'apps/Deployment/default/web', 'Deployment default/web', 'kubernetes.crash_loop', 'Crash loop', ?, 0, 0, 1)`
	require.NoError(t, exec(incident, "i1", "warning"))
	assert.ErrorContains(t, exec(incident, "i2", "loud"), "CHECK")
	var incidentBaseline int
	var alertedAt *int64
	require.NoError(t, db.Read.QueryRow(`SELECT baseline, alerted_at FROM incidents`).Scan(&incidentBaseline, &alertedAt))
	assert.Zero(t, incidentBaseline)
	assert.Nil(t, alertedAt)
	assert.ErrorContains(t, exec(`UPDATE incidents SET baseline = 2`), "CHECK")
	var triageState string
	require.NoError(t, db.Read.QueryRow(`SELECT triage_state FROM incidents`).Scan(&triageState))
	assert.Equal(t, "none", triageState)
	assert.ErrorContains(t, exec(`UPDATE incidents SET triage_state = 'maybe'`), "CHECK")
	assert.ErrorContains(t, exec(`UPDATE incidents SET triage_state = 'queued'`), "CHECK", "a queued triage has its request")
	assert.ErrorContains(t, exec(`UPDATE incidents SET triage_request_id = 'r1'`), "CHECK", "and only a queued one")
	assert.ErrorContains(t, exec(`UPDATE incidents SET triage_state = 'done'`), "CHECK", "a done triage has its verdict")
	assert.ErrorContains(t, exec(`UPDATE incidents SET triage_state = 'done', triage_verdict = 'shrug'`), "CHECK")
	assert.ErrorContains(t, exec(`UPDATE incidents SET triage_state = 'failed'`), "CHECK", "a failed triage has its error")
	assert.ErrorContains(t, exec(`UPDATE incidents SET triage_run_id = 'nope'`), "FOREIGN KEY")
	assert.ErrorContains(t, exec(`UPDATE incidents SET triage_trigger = 'manual'`), "CHECK", "a triage never asked for has no trigger")
	assert.ErrorContains(t, exec(`UPDATE incidents SET triage_state = 'failed', triage_error = 'x'`), "CHECK", "and one asked for has one")
	assert.ErrorContains(t, exec(`UPDATE incidents SET triage_state = 'failed', triage_error = 'x', triage_trigger = 'whim'`), "CHECK")
	require.NoError(t, exec(`INSERT INTO clusters (id, source, created_at, updated_at) VALUES ('c1', 'kubeconfig', 0, 0)`))
	require.NoError(t, exec(`INSERT INTO agent_runs (id, agent_type, app_version, trigger, cluster_id, provider, model, dialect, created_at)
		VALUES ('r1', 'monitor', 'test', 'monitor', 'c1', 'fake', 'fake', 'fake', 0)`))
	require.NoError(t, exec(`UPDATE incidents SET triage_state = 'done', triage_verdict = 'dismiss', triage_run_id = 'r1', triage_trigger = 'automatic'`))
	require.NoError(t, exec(`DELETE FROM agent_runs WHERE id = 'r1'`))
	var runID *string
	require.NoError(t, db.Read.QueryRow(`SELECT triage_run_id FROM incidents`).Scan(&runID))
	assert.Nil(t, runID, "a run's delete leaves its triage")

	const occurrence = `INSERT INTO incident_occurrences (id, incident_id, number, started_at, ended_at, end_reason) VALUES (?, 'i1', ?, 10, ?, ?)`
	require.NoError(t, exec(occurrence, "o1", 1, 20, "resolved"))
	require.NoError(t, exec(occurrence, "o2", 2, nil, nil))
	assert.ErrorContains(t, exec(occurrence, "o3", 3, nil, nil), "UNIQUE", "one open occurrence")
	assert.ErrorContains(t, exec(occurrence, "o4", 2, 30, "stale"), "UNIQUE", "a number is never reused")
	assert.ErrorContains(t, exec(occurrence, "o5", 5, 30, nil), "CHECK", "an end has its reason")
	assert.ErrorContains(t, exec(occurrence, "o6", 6, 5, "stopped"), "CHECK", "no end before the start")
	assert.ErrorContains(t, exec(occurrence, "o7", 7, 30, "gone"), "CHECK")
	assert.ErrorContains(t, exec(`UPDATE incident_occurrences SET baseline = 2`), "CHECK")

	const start = `INSERT INTO monitor_triage_starts (target_id, started_at) VALUES (?, 0)`
	require.NoError(t, exec(start, "t1"))
	assert.ErrorContains(t, exec(start, "nope"), "FOREIGN KEY")

	require.NoError(t, exec(`DELETE FROM monitor_targets WHERE id = 't1'`))
	var left int
	require.NoError(t, db.Read.QueryRow(`SELECT (SELECT count(*) FROM incidents) + (SELECT count(*) FROM incident_occurrences)
		+ (SELECT count(*) FROM monitor_triage_starts)`).Scan(&left))
	assert.Zero(t, left, "a target's delete takes its incidents, their occurrences and its triage starts")
}

// A data dir that cannot hold app.db fails the open — both when the parent path
// cannot be created and when the file itself cannot be an SQLite database.
func TestOpenFailsOnAnUnusablePath(t *testing.T) {
	dir := t.TempDir()

	notADir := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(notADir, []byte("x"), 0o600))
	_, err := Open(filepath.Join(notADir, "app.db"), 0)
	require.ErrorContains(t, err, "mkdir")

	// A directory where the database file belongs: the dir already exists, so the
	// failure comes from the pool's first connection.
	_, err = Open(dir, 0)
	require.Error(t, err)
}

// A file a newer build migrated is refused with the runner's typed error, so a caller
// can name it to the user.
func TestOpenRefusesANewerFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	db, err := Open(path, 0)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	sqlitemigratetest.StampNewer(t, path)

	_, err = Open(path, 0)

	require.ErrorAs(t, err, new(*sqlitemigrate.NewerSchemaError))
}

// shippedMigrations is every file in migrations/ by SHA-256. A new migration adds its
// line in the PR that writes it. Once a desktop release carries a migration, every
// installed app.db has run it, so its line never changes again.
var shippedMigrations = map[string]string{
	"0001_init.sql": "a8088d63acaf0e947af95c501b2b7edf528931ced283a07498763b7407f70ff4",
}

func TestAShippedMigrationIsNeverEdited(t *testing.T) {
	assert.Equal(t, shippedMigrations, sqlitemigratetest.Checksums(t, migrationsFS, "migrations"))
}

// oauth_connections holds one row per provider, and a row's name is NULL until a
// sign-in writes it.
func TestTheOAuthConnectionsHoldOneRowPerProvider(t *testing.T) {
	ctx := context.Background()
	h, _ := openTestDB(t)
	db := h.Write

	_, err := db.ExecContext(ctx, `INSERT INTO oauth_connections (provider, created_at, updated_at) VALUES ('chatgpt', 0, 0)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO oauth_connections (provider, created_at, updated_at) VALUES ('chatgpt', 0, 0)`)
	assert.Error(t, err, "one row per provider")

	var name *string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT name FROM oauth_connections`).Scan(&name))
	assert.Nil(t, name)
	_, err = db.ExecContext(ctx, `UPDATE oauth_connections SET name = 'Ada' WHERE provider = 'chatgpt'`)
	require.NoError(t, err)
}

// The user's variables hold one name per scope, a cluster's going with it, and a
// tool call is shown none unless it says so.
func TestTheUsersVariablesHoldOneNamePerScope(t *testing.T) {
	ctx := context.Background()
	h, _ := openTestDB(t)
	db := h.Write
	for _, seed := range []string{
		`INSERT INTO clusters (id, source, source_key, created_at, updated_at) VALUES ('c1', 'kubeconfig', 'ctx', 0, 0)`,
		`INSERT INTO chats (id, cluster_id, mode, created_at, updated_at) VALUES ('h1', 'c1', 'chat', 0, 0)`,
		`INSERT INTO messages (id, chat_id, seq, role, content, request_key, created_at) VALUES ('m1', 'h1', 0, 'user', '[]', 'key', 0)`,
		`INSERT INTO agent_runs (id, agent_type, app_version, trigger, chat_id, trigger_message_id, provider, model, dialect, created_at)
		 VALUES ('r1', 'chat', 'dev', 'chat', 'h1', 'm1', 'p', 'm', 'fake', 0)`,
		`INSERT INTO llm_calls (id, run_id, seq, provider, model, started_at) VALUES ('l1', 'r1', 0, 'p', 'm', 0)`,
		`INSERT INTO tool_calls (id, llm_call_id, seq, tool_name, status, cwd, created_at) VALUES ('t1', 'l1', 0, 'Bash', 'running', '/w', 0)`,
	} {
		_, err := db.ExecContext(ctx, seed)
		require.NoError(t, err, seed)
	}

	var cwd, env string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT cwd, env FROM tool_calls WHERE id = 't1'`).Scan(&cwd, &env))
	assert.Equal(t, []string{"/w", ""}, []string{cwd, env}, "a call written without env was shown no variables")

	insert := func(id string, cluster any, target string) error {
		_, err := db.ExecContext(ctx, `INSERT INTO env_vars (id, cluster_id, name, value, target, created_at, updated_at)
			VALUES (?, ?, 'HELM_NAMESPACE', 'apps', ?, 0, 0)`, id, cluster, target)
		return err
	}
	require.NoError(t, insert("every", nil, "both"))
	assert.Error(t, insert("every-again", nil, "sandbox"), "a second row of one name for every cluster")
	require.NoError(t, insert("cluster", "c1", "outside"), "the name again, in a cluster")
	assert.Error(t, insert("nowhere", "c1", "chat"), "a target outside the list")

	_, err := db.ExecContext(ctx, `DELETE FROM clusters WHERE id = 'c1'`)
	require.NoError(t, err)
	var ids string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT group_concat(id) FROM env_vars`).Scan(&ids))
	assert.Equal(t, "every", ids, "the cluster's row goes with it")
}

// A subscriber hears the keys it asked for and nothing else, and a burst on one key
// arrives as one ping.
func TestNotifyReachesASubscriberAndConflates(t *testing.T) {
	h, _ := openTestDB(t)
	rx := h.ListenForChanges("a", "b")
	defer rx.Close()

	for range 3 {
		h.Notify("a")
	}
	h.Notify("other")
	h.Notify("b")

	ev, err := rx.RecvContext(t.Context())
	require.NoError(t, err)
	require.Equal(t, "a", ev.Key)
	ev, err = rx.RecvContext(t.Context())
	require.NoError(t, err)
	require.Equal(t, "b", ev.Key)

	// Nothing else is pending: "other" was not subscribed to and the burst on "a"
	// collapsed.
	_, err = rx.TryRecv()
	require.Error(t, err)
}

// Closing the DB ends every subscription, so a receiver parked on RecvContext
// returns rather than outliving the pools it would read.
func TestSubscriptionsEndOnClose(t *testing.T) {
	h, err := Open(filepath.Join(t.TempDir(), "app.db"), 0)
	require.NoError(t, err)
	rx := h.ListenForChanges("a")

	done := make(chan error, 1)
	go func() {
		_, err := rx.RecvContext(context.Background())
		done <- err
	}()
	// Whether the receive parks before Close or finds the hub already closed, it
	// returns with the bus's error.
	require.NoError(t, h.Close())
	require.ErrorIs(t, testutil.Recv(t, done, "receiver woken by close"), gobus.ErrClosed)
	rx.Close()
}

// A subscription to no keys would wait forever: a caller error.
func TestListenForChangesWithNoKeysPanics(t *testing.T) {
	h, _ := openTestDB(t)
	require.Panics(t, func() { h.ListenForChanges() })
}

// The settings tables hold what their keys, checks and triggers allow, and a
// chat's or a cluster's rows go with it.
func TestTheSettingsTablesHoldWhatTheirChecksAllow(t *testing.T) {
	ctx := context.Background()
	h, _ := openTestDB(t)
	db := h.Write
	for _, seed := range []string{
		`INSERT INTO clusters (id, source, source_key, created_at, updated_at) VALUES ('c1', 'kubeconfig', 'ctx', 1, 2)`,
		`INSERT INTO chats (id, cluster_id, mode, created_at, updated_at) VALUES ('h1', 'c1', 'chat', 3, 4)`,
	} {
		_, err := db.ExecContext(ctx, seed)
		require.NoError(t, err, seed)
	}

	// count is how many rows of from there are.
	count := func(from string) int {
		t.Helper()
		var n int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM `+from).Scan(&n))
		return n
	}
	exec := func(q string, args ...any) error {
		_, err := db.ExecContext(ctx, q, args...)
		return err
	}

	setting := func(key string, cluster, value any) error {
		return exec(`INSERT INTO settings (key, cluster_id, value, created_at, updated_at) VALUES (?, ?, ?, 0, 0)`, key, cluster, value)
	}
	require.NoError(t, setting("tools.Bash.enabled", nil, 0), "a setting for every cluster")
	assert.Error(t, setting("tools.Bash.enabled", nil, 1), "a second row of one key for every cluster")
	require.NoError(t, setting("tools.Bash.enabled", "c1", 1), "the key again, in a cluster")
	assert.Error(t, setting("tools.Bash.enabled", "c1", 0), "a second row of one key in a cluster")
	assert.Error(t, setting("x.y", "c2", 1), "a cluster that does not exist")
	assert.Error(t, setting("", nil, 1), "an empty key")
	assert.Error(t, setting("x.real", nil, 1.5), "a real")
	assert.Error(t, setting("x.blob", nil, []byte{1}), "a blob")
	require.NoError(t, setting("x.null", nil, nil), "a NULL is the default, chosen")
	require.NoError(t, setting("x.int", nil, int64(7)))
	var kind string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT typeof(value) FROM settings WHERE key = 'x.int'`).Scan(&kind))
	assert.Equal(t, "integer", kind, "an integer stays an integer")

	rule := func(id string, chat any, target, scope string) error {
		return exec(`INSERT INTO permission_rules (id, chat_id, effect, access_level, principal, target, scope, service, operation, resource, created_at, updated_at)
			VALUES (?, ?, 'allow', 'write', '*', ?, ?, '*', '*', '*', 0, 0)`, id, chat, target, scope)
	}
	require.NoError(t, rule("r1", nil, "kubernetes", "c1"), "a cluster's cluster-scoped rule")
	require.NoError(t, rule("r2", nil, "kubernetes", "c1/web"), "a cluster's namespace rule")
	require.NoError(t, rule("r3", "h1", "kubernetes", "c1/**"), "a chat's rule")
	require.NoError(t, rule("r4", nil, "kubernetes", "c10/web"), "another cluster whose id starts the same")
	require.NoError(t, rule("r5", nil, "aws", "c1"), "another target's account of the same name")
	require.NoError(t, rule("r6", nil, "kubernetes", "*/web"), "a rule for every cluster")
	require.NoError(t, rule("r7", nil, "kubernetes", "c_/web"), "a scope LIKE would take for the cluster's")
	for name, err := range map[string]error{
		"the empty id":        rule("", nil, "kubernetes", "c1"),
		"a second id":         rule("r1", nil, "kubernetes", "c1"),
		"a chat that is gone": rule("x1", "h2", "kubernetes", "c1"),
		"a NULL scope": exec(`INSERT INTO permission_rules (id, effect, access_level, principal, target, scope, service, operation, resource, created_at, updated_at)
			VALUES ('x2', 'allow', 'write', '*', 'kubernetes', NULL, '*', '*', '*', 0, 0)`),
	} {
		assert.Error(t, err, name)
	}

	mode := func(target, account, mode string) error {
		return exec(`INSERT INTO permission_modes (target, account, mode, created_at, updated_at) VALUES (?, ?, ?, 0, 0)`, target, account, mode)
	}
	require.NoError(t, mode("kubernetes", "*", "ask"), "the target's default")
	require.NoError(t, mode("kubernetes", "c1", "auto"), "a cluster's")
	require.NoError(t, mode("kubernetes", "c10", "auto"), "another cluster's")
	require.NoError(t, mode("aws", "c1", "read-only"), "another target's account of the same name")
	assert.Error(t, mode("kubernetes", "c1", "ask"), "a second row of one account")
	assert.Zero(t, count(`sqlite_schema WHERE name = 'kube_rules'`), "kube_rules is not created")

	grant := func(id string, chat any, folder, access string) error {
		return exec(`INSERT INTO folder_grants (id, chat_id, folder, access, created_at) VALUES (?, ?, ?, ?, 0)`, id, chat, folder, access)
	}
	require.NoError(t, grant("f1", nil, "/srv/code", "read"), "an always grant")
	require.NoError(t, grant("f2", "h1", "/srv/code", "write"), "the folder again, in a chat")
	assert.Error(t, grant("f3", nil, "/srv/code", "write"), "a second always grant of one folder")
	assert.Error(t, grant("f4", "h1", "/srv/code", "read"), "a second grant of one folder in a chat")
	assert.Error(t, grant("f5", nil, "", "read"), "no folder")
	assert.Error(t, grant("f6", nil, "/srv/other", "execute"), "an access outside the two")
	assert.Error(t, grant("f7", "h2", "/srv/other", "read"), "a chat that is gone")
	require.NoError(t, exec(`INSERT INTO folder_grants (id, chat_id, folder, access, created_at) VALUES ('f8', NULL, '/srv/code', 'write', 1)
		ON CONFLICT (ifnull(chat_id, ''), folder) DO UPDATE SET access = excluded.access`))
	var id, access string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT id, access FROM folder_grants WHERE chat_id IS NULL`).Scan(&id, &access))
	assert.Equal(t, []string{"f1", "write"}, []string{id, access}, "a grant again keeps its row")

	require.NoError(t, exec(`INSERT INTO executables (name, invocation, created_at) VALUES ('k9s', 'k9s version', 22)`))
	assert.Error(t, exec(`INSERT INTO executables (name, invocation, created_at) VALUES ('k9s', 'k9s info', 23)`), "a second row of one name")
	assert.Error(t, exec(`INSERT INTO executables (name, invocation, created_at) VALUES ('stern', NULL, 24)`), "an invocation is NOT NULL")
	assert.Error(t, exec(`INSERT INTO executables (name, invocation, created_at) VALUES ('stern', 'stern', 'now')`), "created_at is an INTEGER")

	require.NoError(t, exec(`INSERT INTO shell_path (id, path) VALUES (1, '/usr/bin:/bin')`))
	assert.Error(t, exec(`INSERT INTO shell_path (id, path) VALUES (2, '/bin')`), "one row at most")
	assert.Error(t, exec(`INSERT OR REPLACE INTO shell_path (id, path) VALUES (1, NULL)`), "a path is NOT NULL")

	require.NoError(t, exec(`DELETE FROM chats WHERE id = 'h1'`))
	assert.Zero(t, count(`permission_rules WHERE chat_id IS NOT NULL`), "a chat's rules go with it")
	assert.Zero(t, count(`folder_grants WHERE chat_id IS NOT NULL`), "a chat's folder grants go with it")
	require.NoError(t, exec(`DELETE FROM clusters WHERE id = 'c1'`))
	assert.Zero(t, count(`settings WHERE cluster_id IS NOT NULL`), "a cluster's settings go with it")
	// ids is the ids of from's rows, in order.
	ids := func(q string) string {
		t.Helper()
		var s string
		require.NoError(t, db.QueryRowContext(ctx, q).Scan(&s))
		return s
	}
	assert.Equal(t, "r4,r5,r6,r7", ids(`SELECT group_concat(id) FROM (SELECT id FROM permission_rules ORDER BY id)`), "a cluster's rules go with it, and no other")
	assert.Equal(t, "aws:c1,kubernetes:*,kubernetes:c10", ids(`SELECT group_concat(target || ':' || account) FROM (SELECT * FROM permission_modes ORDER BY 1, 2)`), "a cluster's mode goes with it, and no other")
}

// A cluster is not the default until marked, at most one row is marked, and the
// mark is 0 or 1.
func TestAtMostOneClusterIsTheDefault(t *testing.T) {
	ctx := context.Background()
	h, _ := openTestDB(t)
	db := h.Write
	for _, seed := range []string{
		`INSERT INTO clusters (id, source, source_key, created_at, updated_at) VALUES ('c1', 'kubeconfig', 'a', 1, 2)`,
		`INSERT INTO clusters (id, source, source_key, created_at, updated_at) VALUES ('c2', 'kubeconfig', 'b', 3, 4)`,
	} {
		_, err := db.ExecContext(ctx, seed)
		require.NoError(t, err, seed)
	}

	var marked int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM clusters WHERE is_default <> 0`).Scan(&marked))
	assert.Zero(t, marked, "a new row is not the default")

	mark := func(id string, value int) error {
		_, err := db.ExecContext(ctx, `UPDATE clusters SET is_default = ? WHERE id = ?`, value, id)
		return err
	}
	require.NoError(t, mark("c1", 1))
	assert.Error(t, mark("c2", 1), "a second default")
	assert.Error(t, mark("c2", 2), "is_default is 0 or 1")
	require.NoError(t, mark("c1", 0))
	require.NoError(t, mark("c2", 1), "the default moves once the first is cleared")
}
