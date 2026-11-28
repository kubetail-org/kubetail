-- Copyright 2024 The Kubetail Authors
--
-- Licensed under the Apache License, Version 2.0 (the "License");
-- you may not use this file except in compliance with the License.
-- You may obtain a copy of the License at
--
--     http://www.apache.org/licenses/LICENSE-2.0
--
-- Unless required by applicable law or agreed to in writing, software
-- distributed under the License is distributed on an "AS IS" BASIS,
-- WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
-- See the License for the specific language governing permissions and
-- limitations under the License.

-- app.db is the durable, app-level SQLite database (one schema_migrations
-- sequence, owned by internal/appdb). Timestamps are unix millis.
--
-- Shipped in desktop/v0.0.1 and never edited: a form change is the next
-- numbered file here
-- (docs/adr/2026-11-08-a-shipped-schema-changes-only-by-a-new-migration.md).

-- clusters: every cluster the app knows. This table owns a cluster's identity and
-- the user's choices; the beehive store holds one runtime object per row (probe
-- status, caches, kinds), named by the row's id and kept in step by the mirror.
--
-- id is a UUIDv7 minted by appdb.NewID (a cloud row copies the cloud's id).
-- source says how the row was created and how its credentials resolve:
-- 'kubeconfig' is a context in this device's kubeconfig and source_key its name;
-- 'cloud' is a cluster in the user's cloud account, with a NULL source_key.
-- UNIQUE (source, source_key) claims a context once; NULLs are distinct in a
-- unique index, so cloud rows are not constrained by it.
--
-- name is the display name the user set; NULL shows the context name instead.
-- enabled and sync_enabled default on, so a found context appears in the picker
-- without being switched on first. Whether the monitor reads the cluster is its
-- target's switch, monitor_targets.enabled.
-- updated_at moves on every user edit and never on a reconcile.
--
-- delete_requested_at is set when the user deletes the cluster. The row stays,
-- marked, until the runtime object and its caches are gone and no chat references
-- it; meanwhile it holds its (source, source_key) claim, so the importer cannot
-- re-create the context mid-teardown. The mirror deletes the row last.
--
-- is_default marks the user's default cluster: a new window with no cluster of its
-- own opens on it. The partial unique index allows at most one marked row. A row
-- marked for deletion or disabled is never the default: the write that marks or
-- disables it clears is_default too.
--
-- Credentials are NOT stored here: a kubeconfig row resolves them through its
-- context.
CREATE TABLE clusters (
  id                  TEXT    PRIMARY KEY,
  source              TEXT    NOT NULL CHECK (source IN ('kubeconfig', 'cloud')),
  source_key          TEXT,
  name                TEXT,
  enabled             INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  sync_enabled        INTEGER NOT NULL DEFAULT 1 CHECK (sync_enabled IN (0, 1)),
  created_at          INTEGER NOT NULL,
  updated_at          INTEGER NOT NULL,
  delete_requested_at INTEGER,
  is_default          INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),
  UNIQUE (source, source_key)
) STRICT, WITHOUT ROWID;

CREATE UNIQUE INDEX clusters_one_default ON clusters (is_default) WHERE is_default = 1;

-- chats: a thread of messages and the runs they triggered. mode is which of
-- the app's two modes lists it, fixed at creation and checked by the column; cluster_id
-- is the clusters row it was started under, fixed at creation too. The cascade is the
-- backstop: the sweeper empties a marked cluster before its row goes. A chat has no
-- dialect of its own: each run records the one it ran on. title is NULL until
-- something sets one; the service reads it as ''. sandbox_disabled is the user's switch:
-- 1 runs the chat's commands outside the sandbox, each asking first. network_enabled is
-- the other: 1 gives the chat's sandboxed commands the internet. updated_at moves when a
-- message is posted or a run settles, never on a reconcile or a switch.
CREATE TABLE chats (
  id               TEXT    PRIMARY KEY,
  cluster_id       TEXT    NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
  mode             TEXT    NOT NULL CHECK (mode IN ('chat', 'dashboard')),
  title            TEXT,
  sandbox_disabled INTEGER NOT NULL DEFAULT 0 CHECK (sandbox_disabled IN (0, 1)),
  network_enabled  INTEGER NOT NULL DEFAULT 0 CHECK (network_enabled IN (0, 1)),
  created_at       INTEGER NOT NULL,
  updated_at       INTEGER NOT NULL
) STRICT, WITHOUT ROWID;

-- agent_runs: one execution of an agent. A chat run answers the user message named
-- by trigger_message_id — UNIQUE, so a message starts at most one run however many
-- times its request is replayed; the request's own idempotency is messages.request_key.
-- Plain REFERENCES both ways between messages and runs: the pair goes with its
-- chat and neither is deleted alone.
--
-- provider / model / effort are what was requested; effort is NULL where the model
-- has no such knob. dialect is the llm.Dialect of the provider the run was sent to,
-- which reads the run's stored content (its citations and payloads) after that
-- provider has left the catalog. Its CHECK refuses the empty string alone: the set
-- of dialects is llm's to list. app_version is the build that ran, and agent_type names the
-- agent definition (prompt, tools) that ran, which lives in code; together they say
-- what produced the run.
--
-- Lifecycle: queued -> running -> succeeded | failed | cancelled, and running <->
-- waiting_approval once a mutating tool call can wait on the user. The chat turn's
-- goroutine claims its own queued run (UPDATE ... WHERE status = 'queued'); there is
-- no worker queue yet. A cancel that lands before the claim settles the run cancelled
-- from queued. On startup every run still queued, running or waiting_approval is
-- failed: the process that owned it is gone and nothing resumes a queued run.
-- A child agent's run (trigger 'agent') is inserted running under its parent_run_id
-- and the parent's chat, with the task it was handed; result is its final
-- text. A monitor's run (trigger 'monitor') is inserted queued under its
-- cluster, with no chat and no message; task is its brief and result its
-- report. cluster_id is set on a monitor's run alone: a chat's and a subagent's
-- cluster is their chat's.
CREATE TABLE agent_runs (
  id              TEXT    PRIMARY KEY,
  parent_run_id   TEXT    REFERENCES agent_runs(id) ON DELETE CASCADE,
  agent_type      TEXT    NOT NULL,
  app_version     TEXT    NOT NULL,
  trigger         TEXT    NOT NULL CHECK (trigger IN ('chat', 'monitor', 'agent')),
  chat_id         TEXT    REFERENCES chats(id) ON DELETE CASCADE,
  cluster_id      TEXT    REFERENCES clusters(id) ON DELETE CASCADE,
  trigger_message_id
                  TEXT    UNIQUE REFERENCES messages(id),

  provider        TEXT    NOT NULL,
  model           TEXT    NOT NULL,
  effort          TEXT,
  dialect         TEXT    NOT NULL CHECK (dialect <> ''),

  task            TEXT,
  result          TEXT,
  error           TEXT,
  status          TEXT    NOT NULL DEFAULT 'queued'
                          CHECK (status IN ('queued', 'running', 'waiting_approval',
                                            'succeeded', 'failed', 'cancelled')),

  created_at      INTEGER NOT NULL,
  started_at      INTEGER,
  finished_at     INTEGER,

  CHECK ((trigger = 'monitor') = (chat_id IS NULL)),
  CHECK ((trigger = 'monitor') = (cluster_id IS NOT NULL))
) STRICT;

CREATE INDEX agent_runs_parent_idx ON agent_runs (parent_run_id);
CREATE INDEX agent_runs_cluster_idx ON agent_runs (cluster_id);
CREATE INDEX agent_runs_chat_idx   ON agent_runs (chat_id);
CREATE INDEX agent_runs_queued_idx ON agent_runs (id) WHERE status = 'queued';
-- The chat list marks a chat with a run waiting on the user, on every read.
CREATE INDEX agent_runs_waiting_idx ON agent_runs (chat_id) WHERE status = 'waiting_approval';

-- messages: the transcript. A message is what a client posts; a run is what the
-- server does about it, so a message's streaming/failed state is its run's status
-- and there is no status column here.
--
-- content is the content blocks exactly as they went over the wire, as JSON.
-- seq is the transcript order: a per-chat counter the posting transaction
-- assigns from MAX(seq). The single writer serializes it, so it needs no clock; ids
-- are identity alone. request_key is the client's idempotency key on every message a
-- client posts — a UUID the client minted, so a retry finds its first attempt — and
-- NULL on messages the server writes. run_id is the run that produced an assistant
-- message; NULL on a user message, whose run is found through
-- agent_runs.trigger_message_id.
--
-- A post inserts the user message, its queued run and the assistant row with content
-- '[]' in one transaction, so a replayed key never finds half of them.
CREATE TABLE messages (
  id              TEXT    PRIMARY KEY,
  chat_id TEXT    NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
  seq             INTEGER NOT NULL,
  role            TEXT    NOT NULL CHECK (role IN ('user', 'assistant')),
  content         TEXT    NOT NULL,
  request_key     TEXT    UNIQUE,
  run_id          TEXT    REFERENCES agent_runs(id),
  created_at      INTEGER NOT NULL,
  UNIQUE (chat_id, seq)
) STRICT;
CREATE INDEX messages_run_idx ON messages (run_id);

-- llm_calls: one row per request to a model provider, under the run that made it.
-- The source of truth for token usage. The row is inserted before the provider is
-- contacted and written whole again when the call ends, so a call in flight has a
-- NULL finished_at; the next start closes one the process died under with error
-- 'stranded'. seq is the call's position in its run, assigned by the writer: a
-- run's call order is (run_id, seq), never its ids, which are minted wherever the
-- run happens to be running.
--
-- provider / effort are what was sent; model is what the provider said it served,
-- else what was asked for. stop_reason is the provider's, set only when a reply
-- arrived. error is the client side's, as safe plain text: a provider refusal, a
-- dropped stream, 'cancelled', 'stranded'. request, response and cost_usd_micros
-- are reserved and NULL.
--
-- input_tokens counts UNCACHED input only; the total the model read is the three
-- input columns summed. The four token columns are NULL together when the provider
-- reported no usage, when the reply broke off, or when the report was inconsistent
-- (cache counts past the input); a reported zero is 0. server_uses is how many
-- times the provider ran each server tool for the call, a JSON object from the
-- tool's name to its count, holding the tools its usage reports; NULL where it
-- reports none.
--
-- first_chunk_at is the first text or thinking chunk, NULL on a call that showed
-- nothing before it ended.
CREATE TABLE llm_calls (
  id                 TEXT    PRIMARY KEY,
  run_id             TEXT    NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
  seq                INTEGER NOT NULL,

  provider           TEXT    NOT NULL,
  model              TEXT    NOT NULL,
  effort             TEXT,
  request            TEXT,
  response           TEXT,
  stop_reason        TEXT,
  error              TEXT,

  input_tokens       INTEGER,
  cache_read_tokens  INTEGER,
  cache_write_tokens INTEGER,
  output_tokens      INTEGER,
  server_uses        TEXT,
  cost_usd_micros    INTEGER,

  started_at         INTEGER NOT NULL,
  first_chunk_at     INTEGER,
  finished_at        INTEGER
) STRICT;

CREATE UNIQUE INDEX llm_calls_run_idx ON llm_calls (run_id, seq);

-- tool_calls: every tool the model invoked — the audit trail of what touched the
-- cluster. The owning run is reached through llm_call_id, the model call that
-- asked. A call that runs is inserted 'running' immediately before it executes,
-- and nothing runs when that write fails; it is written whole again with its
-- result or error. A call the loop refused without running it (budget, not-run,
-- unknown-tool) is 'failed' with no started_at, written at settlement. arguments
-- is the call's input as JSON; result is what the model read, plain text, on every
-- closed row, a refusal included; error is a JSON object, the loop's refusal
-- ({"error":"timeout"}, {"error":"cancelled"}, {"error":"stranded"}) or
-- {"error":"tool"} for a tool's own error. seq is the call's position among
-- the tool_use blocks of the reply that asked, so (llm_call_id, seq) is the order
-- the model called them in, refused calls included.
--
-- is_mutating is what the app treated the call as when it ran, stored per call
-- because definitions change across app versions: 1 on a gated call the user was
-- asked about, and 0 on every other, a gated call whose tool skipped the question
-- included. A gated call's first write is 'awaiting_approval' with no started_at,
-- in one transaction with its approvals row; it is written 'running' with
-- started_at the moment before the tool runs — so a NULL started_at says the call
-- never ran; 'denied' is the user's no. tool_use_id is the provider's id of the tool_use block the call
-- answers, what joins a row to its block. spawned_run_id is the child run an
-- Agent call created, written with the run's insert in one transaction, and
-- NULL on every other call.
--
-- cwd is the absolute directory a gated call starts in, as its tool resolved it
-- at the gate, written with the call's first row and never changed; '' on a call
-- that did not reach the gate and on a tool that runs nowhere. It is stored
-- because the home it was resolved against can change between runs. sandboxed
-- is whether a sandbox confined the call, written and kept the same way: whether
-- the machine had one is not in the arguments. network is the network a sandboxed
-- call ran with — 'chat' for the chat's switch, 'turn' for the turn's toggle,
-- 'approved' for its own request — NULL for none and NULL until it runs: it is written
-- on the row that marks the call running, so a call that asked and was denied keeps
-- NULL. shown_action is what the run showed, for a tool whose action cannot be
-- read off the arguments alone (a view of logs: the mirror named the default
-- container and the clock made a duration a moment); written with the result,
-- NULL on every other call and on a refusal. Everything else a call does is read
-- again from arguments by its own tool.
--
-- tool_name is the tool's name in the box: a tool of ours by the name it is
-- offered under, a vendor's by its own name, never a provider's wire word.
-- contract_name is the shape the row's arguments, shown_action and task result
-- are in, set at the row's first write and never after: 'bash/v1' for a tool of
-- ours, the vendor's own identifier ('web_search_20260318') for a vendor's, NULL
-- for a call of a tool the app does not know. A row is read only under its
-- tool's current contract.
--
-- runs_on is who ran the call: 'sidecar' for every call above, 'provider' for a
-- call the model's provider ran on its own servers (a web search). A provider
-- row is written the moment the stream shows the call: tool_use_id is the
-- provider's id, arguments what it sent, and no result, since the provider's
-- results are the model's alone. status, started_at and finished_at are the
-- sidecar's lifecycle, so a provider row has none of them: the app ran nothing,
-- and the CHECKs below hold both shapes. It is never gated. seq counts it among
-- the reply's calls: a reply's server calls all come before the tool_use blocks
-- that end it.
--
-- env is the user's variables a gated command outside the sandbox was shown with
-- and runs with, a JSON array of NAME=value written with cwd; '' on every other
-- call.
CREATE TABLE tool_calls (
  id             TEXT    PRIMARY KEY,
  llm_call_id    TEXT    NOT NULL REFERENCES llm_calls(id) ON DELETE CASCADE,
  seq            INTEGER NOT NULL,
  runs_on        TEXT    NOT NULL DEFAULT 'sidecar' CHECK (runs_on IN ('sidecar', 'provider')),

  tool_name      TEXT    NOT NULL,
  contract_name  TEXT,
  tool_use_id    TEXT,
  arguments      TEXT,
  cwd            TEXT    NOT NULL DEFAULT '',
  sandboxed      INTEGER NOT NULL DEFAULT 0 CHECK (sandboxed IN (0, 1)),
  network        TEXT    CHECK (network IN ('chat', 'turn', 'approved')),
  result         TEXT,
  error          TEXT,
  shown_action   TEXT,

  is_mutating    INTEGER NOT NULL DEFAULT 0 CHECK (is_mutating IN (0, 1)),
  spawned_run_id TEXT    REFERENCES agent_runs(id) ON DELETE SET NULL,
  status         TEXT    DEFAULT 'pending'
                         CHECK (status IN ('pending', 'awaiting_approval', 'running',
                                           'succeeded', 'failed', 'denied')),

  created_at     INTEGER NOT NULL,
  started_at     INTEGER,
  finished_at    INTEGER,
  env            TEXT    NOT NULL DEFAULT '',

  CHECK ((runs_on = 'provider') = (status IS NULL)),
  CHECK (runs_on = 'sidecar' OR (started_at IS NULL AND finished_at IS NULL))
) STRICT;

CREATE UNIQUE INDEX tool_calls_llm_call_idx ON tool_calls (llm_call_id, seq);
CREATE INDEX tool_calls_spawned_idx  ON tool_calls (spawned_run_id);

-- approvals: the user's decisions on a tool call. A 'call' approval is the
-- decision on a mutating call, at most one per call, written with the call's
-- first row, before the approval request is shown; what the call does is its
-- tool_calls row's. The turn that waits on it writes the decision; a wait that
-- was cancelled or stranded leaves the row 'pending', the record of a question
-- nobody answered. An 'action' approval is a classified action a sandboxed
-- command's request asked for while its call ran — a change to the cluster —
-- held until the user decided, or 'abandoned' when its wait ended first;
-- request is the tools.ActionRequest as JSON, the action and the write's body
-- as sent. An action left 'pending' is one a crash stranded. An action the
-- permissions engine decided with nobody asked is 'allowed' or 'refused', and
-- reason names the mode or the rule that decided it. duration is how long an
-- approval holds, what the user chose: set on 'approved' alone, and 'once' on
-- a call's own.
CREATE TABLE approvals (
  id           TEXT    PRIMARY KEY,
  tool_call_id TEXT    NOT NULL REFERENCES tool_calls(id) ON DELETE CASCADE,
  kind         TEXT    NOT NULL DEFAULT 'call' CHECK (kind IN ('call', 'action')),
  request      TEXT,
  status       TEXT    NOT NULL DEFAULT 'pending'
                       CHECK (status IN ('pending', 'approved', 'denied', 'abandoned', 'allowed', 'refused')),
  duration     TEXT    CHECK (duration IN ('once', 'command', 'chat', 'always')),
  reason       TEXT,
  created_at   INTEGER NOT NULL,
  decided_at   INTEGER,

  CHECK ((kind = 'call') = (request IS NULL)),
  CHECK (status NOT IN ('abandoned', 'allowed', 'refused') OR kind = 'action'),
  CHECK (duration IS NULL OR status = 'approved')
) STRICT, WITHOUT ROWID;

CREATE UNIQUE INDEX approvals_call_idx ON approvals (tool_call_id) WHERE kind = 'call';
CREATE INDEX approvals_actions_idx ON approvals (tool_call_id) WHERE kind = 'action';
CREATE INDEX approvals_pending_idx ON approvals (id) WHERE status = 'pending';

-- memories: notes kept for later chats. cluster_id NULL is a note for every cluster.
-- server_uid is the cluster's kube-system UID at the last write, NULL for a note for
-- every cluster or a cluster never identified.
CREATE TABLE memories (
  id          TEXT    PRIMARY KEY,
  cluster_id  TEXT    REFERENCES clusters(id) ON DELETE CASCADE,
  server_uid  TEXT,
  name        TEXT    NOT NULL,
  body        TEXT    NOT NULL,
  written_by  TEXT    NOT NULL CHECK (written_by IN ('model', 'user')),
  chat_id     TEXT    REFERENCES chats(id) ON DELETE SET NULL,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL,
  CHECK (cluster_id IS NOT NULL OR server_uid IS NULL)
) STRICT;

-- One name per scope.
CREATE UNIQUE INDEX memories_name ON memories (ifnull(cluster_id, ''), name);

-- background_tasks: one row per command started with run_in_background, per
-- agent an Agent call started and per watch a LogsWatch call started, written
-- running before any starts. output_path is the file the call's result named: a
-- command's output, an agent's report, a watch's, which stays empty. status ends
-- exited (a command; exit_code when it could be read), completed (an agent that
-- answered), failed (an agent that ended on its own error, or a watch on an error
-- or the follow's end with no line), matched or expired (a watch that ended holding
-- lines, or at its time with none), stopped (stopped_by: the model's TaskStop, the
-- user's Stop or a chat's delete, the app's own stop, or an agent's approval request
-- that went unanswered), or lost, which the next start writes on a row a crash left
-- running. result is a finished watch's one JSON object — its sample, its lines,
-- why it ended and the view around its first line — and NULL on every other row. A
-- finished row with a NULL notified_at is a waiting notice: it rides the chat's next
-- question, and an exit, or an agent's or a watch's end under a user's send, also
-- starts a turn of its own. A stop the model made is written notified, since its
-- TaskStop result told it, except a watch's, whose lines the notice carries.
CREATE TABLE background_tasks (
  id              TEXT    PRIMARY KEY,
  chat_id TEXT    NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
  tool_call_id    TEXT    NOT NULL UNIQUE REFERENCES tool_calls(id) ON DELETE CASCADE,
  output_path     TEXT    NOT NULL,
  status          TEXT    NOT NULL CHECK (status IN ('running', 'exited', 'completed', 'failed', 'matched', 'expired', 'stopped', 'lost')),
  stopped_by      TEXT    CHECK (stopped_by IN ('model', 'user', 'app', 'unanswered')),
  exit_code       INTEGER,
  started_at      INTEGER NOT NULL,
  finished_at     INTEGER,
  notified_at     INTEGER,
  result          TEXT
) STRICT, WITHOUT ROWID;

CREATE INDEX background_tasks_chat_idx ON background_tasks (chat_id);

-- oauth_connections: one row per model provider the user signs in to with OAuth,
-- keyed by the catalog's provider id ('chatgpt'). It holds no token: the refresh
-- token is in the OS keyring, in an entry that names its sign-in.
--
-- client_id is the OAuth client the provider issued this installation, NULL until
-- one is registered and for a provider whose client is fixed in code. subject and
-- email are the account that last signed in, email for display alone; both stay
-- across a sign-out, so the next sign-in can be held to the same account.
--
-- session_id is a random value minted at each sign-in and written to the keyring
-- entry beside the refresh token; NULL is signed out. A session exists only when
-- the two match, so an entry and a row that a crash, a lost commit or a restored
-- backup left apart never make one.
--
-- name is the account's display name from its verified ID token, kept beside
-- subject and email and dropped with them, for display alone. NULL when the
-- token named none.
--
-- provider_data is what one provider alone keeps, a JSON object in its service's
-- own shape (ChatGPT: {"hostID": ...}); NULL when it keeps nothing.
CREATE TABLE oauth_connections (
  provider      TEXT    PRIMARY KEY CHECK (provider <> ''),
  client_id     TEXT,
  subject       TEXT,
  email         TEXT,
  session_id    TEXT,
  provider_data TEXT,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL,
  name          TEXT
) STRICT, WITHOUT ROWID;

-- env_vars: the variables the user gives their commands, NAME=value. cluster_id
-- NULL is a variable for every cluster; set, it is that cluster's commands' alone
-- and goes with the cluster. target is where it reaches: commands in the sandbox,
-- commands outside it, or both. A value is not a secret: any command can print it.
CREATE TABLE env_vars (
  id         TEXT    PRIMARY KEY,
  cluster_id TEXT    REFERENCES clusters(id) ON DELETE CASCADE,
  name       TEXT    NOT NULL,
  value      TEXT    NOT NULL,
  target     TEXT    NOT NULL CHECK (target IN ('sandbox', 'outside', 'both')),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
) STRICT;

-- One name per scope.
CREATE UNIQUE INDEX env_vars_name ON env_vars (ifnull(cluster_id, ''), name);

-- settings: the user's single-value settings, a row per setting and scope the
-- user has set. A setting with no row, or with a NULL value, has its default.
-- cluster_id NULL is the setting for every cluster; set, it is that cluster's
-- alone and goes with the cluster.
CREATE TABLE settings (
  key        TEXT    NOT NULL CHECK (key <> ''),
  cluster_id TEXT    REFERENCES clusters(id) ON DELETE CASCADE,
  value      ANY     CHECK (typeof(value) IN ('integer', 'text', 'null')),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
) STRICT;

CREATE UNIQUE INDEX settings_scope_key ON settings (ifnull(cluster_id, ''), key);

-- permission_rules: the user's rules over the actions sandboxed commands
-- send, each a permissions.Rule. chat_id NULL is an always rule; set, the
-- rule lasts for that chat and goes with it. No effect, level, principal or
-- target is enumerated here: settings.CheckRule refuses on write what Go
-- cannot apply, and a read turns a row it cannot read into a refusal.
CREATE TABLE permission_rules (
  id           TEXT    PRIMARY KEY CHECK (id <> ''),
  chat_id      TEXT    REFERENCES chats(id) ON DELETE CASCADE,
  effect       TEXT    NOT NULL,
  access_level TEXT    NOT NULL,
  principal    TEXT    NOT NULL,
  target       TEXT    NOT NULL,
  scope        TEXT    NOT NULL,
  service      TEXT    NOT NULL,
  operation    TEXT    NOT NULL,
  resource     TEXT    NOT NULL,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
) STRICT;

-- A chat's rules, or the always ones, in the order they are read.
CREATE INDEX permission_rules_chat_idx ON permission_rules (chat_id, created_at, id);

-- permission_modes: an account's mode, by target. account is the account a
-- scope's first segment names, or '*' for the target's default. A cloud
-- account has no record, so its row stays until the user removes it.
CREATE TABLE permission_modes (
  target     TEXT    NOT NULL,
  account    TEXT    NOT NULL,
  mode       TEXT    NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (target, account)
) STRICT;

-- A cluster's mode and rules go with it: the rules whose scope is the
-- cluster's id or starts with it and a '/'. substr, never LIKE, which reads
-- '_' and '%' in an id as wildcards.
CREATE TRIGGER clusters_permissions_delete AFTER DELETE ON clusters
BEGIN
  DELETE FROM permission_modes WHERE target = 'kubernetes' AND account = OLD.id;
  DELETE FROM permission_rules WHERE target = 'kubernetes'
    AND (scope = OLD.id OR substr(scope, 1, length(OLD.id) + 1) = OLD.id || '/');
END;

-- folder_grants: a folder the sandbox opens to a run. chat_id NULL is an always
-- grant; set, the grant lasts for that chat and goes with it.
CREATE TABLE folder_grants (
  id         TEXT    PRIMARY KEY,
  chat_id    TEXT    REFERENCES chats(id) ON DELETE CASCADE,
  folder     TEXT    NOT NULL CHECK (folder <> ''),
  access     TEXT    NOT NULL CHECK (access IN ('read', 'write')),
  created_at INTEGER NOT NULL
) STRICT;

-- One grant per folder, always or in a chat.
CREATE UNIQUE INDEX folder_grants_folder ON folder_grants (ifnull(chat_id, ''), folder);

-- A chat's grants, or the always ones, in the order they are read.
CREATE INDEX folder_grants_chat_idx ON folder_grants (chat_id, created_at, id);

-- executables: the programs the user asked the probe to check, in the order
-- added. A row is read ORDER BY created_at, rowid, since two can be added in
-- the same millisecond.
CREATE TABLE executables (
  name       TEXT    PRIMARY KEY,
  invocation TEXT    NOT NULL,
  created_at INTEGER NOT NULL
) STRICT;

-- shell_path: the login shell's PATH as the last read that answered gave it,
-- joined by ':', for a launch whose own read fails. Written by Kubetail alone.
-- No row before a first read answers.
CREATE TABLE shell_path (
  id   INTEGER PRIMARY KEY CHECK (id = 1),
  path TEXT    NOT NULL
) STRICT;

-- monitor_targets: each thing the monitor reads. Its integration lists it, and
-- the monitor's reconcile makes and deletes its row. integration is checked in
-- Go against the integrations the build has. key is the integration's own: for
-- kubernetes, the cluster's id. current_uid is what the target reached at its last
-- reading: for kubernetes, the server's UID.
--
-- enabled defaults on and gates detection alone: reading a target finds failures
-- and spends no model call. generation counts the switch's flips, the fold's guard
-- against a reading from before one. unknown_signals holds each signal the last
-- reading could not judge, with its code; last_read_at is NULL until the first
-- reading. label is the integration's, rewritten when it changes. baselines holds,
-- by signal name, the time in ms its baseline window opened: its first judged
-- reading since the target was switched on or reached a new UID.
CREATE TABLE monitor_targets (
  id              TEXT    PRIMARY KEY,
  integration     TEXT    NOT NULL,
  key             TEXT    NOT NULL,
  label           TEXT    NOT NULL,
  enabled         INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  generation      INTEGER NOT NULL DEFAULT 0,
  current_uid     TEXT,
  unknown_signals TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(unknown_signals) AND json_type(unknown_signals) = 'object'),
  baselines       TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(baselines)),
  last_read_at    INTEGER,
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL,
  UNIQUE (integration, key)
) STRICT;

-- incidents: a target under one UID, a subject and a signal, and the occurrences
-- it has held. subject_key is the integration's, compared to match an incident
-- and never parsed; subject_attributes is a JSON object of strings only the
-- integration reads. signal_label and severity are copied when the incident
-- opens. last_seen_at is the last time a reading saw its signal; occurrence_count
-- counts every occurrence the incident has had, trimmed ones included. baseline
-- is 1 until the incident's first occurrence that is not baseline, and never
-- set again; alerted_at is when that occurrence raised its alert, by the wall
-- clock, and alert_seq its place in the order the alerts committed, which the
-- alert feed reads.
CREATE TABLE incidents (
  id                 TEXT    PRIMARY KEY,
  target_id          TEXT    NOT NULL REFERENCES monitor_targets(id) ON DELETE CASCADE,
  target_uid         TEXT    NOT NULL,
  subject_key        TEXT    NOT NULL,
  subject_label      TEXT    NOT NULL,
  subject_attributes TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(subject_attributes)),
  signal             TEXT    NOT NULL,
  signal_label       TEXT    NOT NULL,
  severity           TEXT    NOT NULL CHECK (severity IN ('critical', 'warning', 'info')),
  first_seen_at      INTEGER NOT NULL,
  last_seen_at       INTEGER NOT NULL,
  occurrence_count   INTEGER NOT NULL CHECK (occurrence_count >= 1),
  baseline           INTEGER NOT NULL DEFAULT 0 CHECK (baseline IN (0, 1)),
  alerted_at         INTEGER,
  alert_seq          INTEGER,
  CHECK ((alerted_at IS NULL) = (alert_seq IS NULL))
) STRICT;

CREATE INDEX incidents_subject ON incidents (target_id, target_uid, signal, subject_key);
CREATE INDEX incidents_last_seen ON incidents (last_seen_at);
CREATE UNIQUE INDEX incidents_alerted ON incidents (alert_seq) WHERE alert_seq IS NOT NULL;

-- monitor_alert_seq: the last alert_seq given, so an alerted incident's delete
-- never frees its number. No row before the first alert.
CREATE TABLE monitor_alert_seq (
  id   INTEGER PRIMARY KEY CHECK (id = 1),
  last INTEGER NOT NULL
) STRICT;

-- incident_occurrences: one stretch an incident's signal held. number is its
-- order, never reused; started_at is the wall clock's and orders nothing. An
-- occurrence is open while ended_at is NULL; detail is what the reading that
-- started it saw, a JSON object of strings. baseline marks one whose pair was
-- first seen in its signal's baseline window: it raises no alert.
CREATE TABLE incident_occurrences (
  id          TEXT    PRIMARY KEY,
  incident_id TEXT    NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  number      INTEGER NOT NULL CHECK (number >= 1),
  started_at  INTEGER NOT NULL,
  ended_at    INTEGER CHECK (ended_at >= started_at),
  end_reason  TEXT    CHECK (end_reason IN ('resolved', 'stopped', 'stale')),
  detail      TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(detail)),
  baseline    INTEGER NOT NULL DEFAULT 0 CHECK (baseline IN (0, 1)),
  UNIQUE (incident_id, number),
  CHECK ((ended_at IS NULL) = (end_reason IS NULL))
) STRICT, WITHOUT ROWID;

CREATE UNIQUE INDEX incident_occurrences_one_open ON incident_occurrences (incident_id) WHERE ended_at IS NULL;
