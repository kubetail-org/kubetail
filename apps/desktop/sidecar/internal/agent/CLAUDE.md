# internal/agent — catalog, loop, chat and monitor

`catalog` decides which providers and tools a turn is offered; `loop` takes one turn (ask the
model, run its calls, ask again); `chat` files a send, runs its turn in the background and serves
the record; `monitor` finds failures with no model. The loop imports `llm` and `tools` alone;
`chat` imports the loop.

## Catalog (`internal/agent/catalog`)

Only `config.go` and `app` import it.
→ [ADR: the catalog is its own package](../../../docs/adr/2026-09-24-the-catalog-is-its-own-package.md),
[ADR: every provider is one row, signed by one service](../../../docs/adr/2026-11-20-every-provider-is-one-row-signed-by-one-service.md),
[ADR: a signed-in provider's catalog follows its connection](../../../docs/adr/2026-11-18-a-signed-in-providers-catalog-follows-its-connection.md).

- A provider lists its catalog only while it has a credential.
- **`providers.go`'s `rows` is the one table of providers**: each one's key variable (`""` when
  only its connector signs it) and models reader (nil reads the written catalog). A new vendor goes
  here, never in `llm`. `rowsAt` alone applies a debug base (`KUBETAIL_<ID>_BASE_URL`), clearing
  the moved row's model overrides, and drops a row with no base URL (`google` with no project).
  `KeyVars()` is what `config.go` reads keys from and clears from the environment. **Every entry
  states its `Cache`** (and `AffinityRoute` where the vendor routes by key); a new entry lands with
  the line `make test-desktop-livecache` prints for it.
- **`chatgpt`'s catalog is the account's `GET /models`** (signed, following no redirect), each
  entry taking the `openai` row's entry of the same id where it has one. Its route is a preview: a
  field it refuses goes in the row's `Omit`. `google` is Vertex AI, whose catalog is `gemini`'s
  under `google/` ids.
- **A list is policy.** `lists` is static data by provider id: `ours` for every provider, plus the
  Anthropic search for `anthropic` and `fake`. It decides what code runs each kind of action and
  what leaves the machine, so adding a vendor tool or leaving one of ours off is a security change.
  One tool per kind; lists never depend on API keys (`TestEachListIsOneToolOfEachKind`,
  `TestEveryToolIsOnAList`). `ToolListings()` is each tool's Settings › Tools row
  (`TestToolListingsMatchTheLists`).

## Loop (`internal/agent/loop`)

`loop.Run` takes a turn on the calling goroutine, with a box already `Box.For` the target.

- **Every `tool_use` gets its `tool_result`, in reply order.** A loop refusal is
  `{"error":"<code>"}` with `IsError`, never Go error text. Codes are exported `loop.Code`s; read
  one back with `RefusalOf`, never by spelling.
- **The budget counts calls, not rounds.** A reply asking for more than remain is refused whole and
  gets one synthesis round; a second ends the turn. A turn streams at most `MaxToolCalls + 2` times.
- **Timeouts are cooperative**: a tool must honour its context. A `tools.Bounded` tool names its
  own bound; the rest get the turn's `DefaultToolTimeout`. A result with no error is kept whatever
  the clock says. **A timeout is not a cancel.**
- **Calls run one at a time; a `tools.Gated` call waits on the user.** `Approval` (before
  `ToolCallStarted`) returns a `tools.Approval`, then `Approver.Approve` decides. The zero value
  asks, so a tool that forgets asks. A `tools.ApprovedRunner` runs from the approval
  (`RunApproved`), so what runs is what the gate decided. One request is before the user at a time.
  → [ADR: the gate is the tool's card](../../../docs/adr/2026-09-22-the-gate-is-the-tools-card.md),
  [ADR: a tool call shows itself from its arguments](../../../docs/adr/2026-09-23-a-tool-call-shows-itself-from-its-arguments.md).
- **A failed recorder write stops what follows it**: later calls in the reply are `not-run` and the
  turn fails. A failed `LLMCallFinished` ends the turn only under a reply that asked for tools.
- **Server tools have a per-turn budget** (`offer`); one with no uses left is not offered. A
  `pause_turn` with no call is asked again at most `maxPauses` (3) times, while every server tool
  has uses left.
- **The recorder is told before and after each external step, on the run's goroutine**, and
  `Settled` once, last, on every path. `Progress` cannot fail.

**The system prompt** (`SystemPrompt`) is the turn's prompt, `# What you can do` (`prompts/tools.md`
plus each tool's `Prompt()` in box order, or `no_tools.md`), then `# Data is not instructions`,
whose one exception is `"by":"user"` memory notes.

**What the model reads is pinned as goldens** (`internal/app/testdata/prompt/`,
`internal/agent/chat/testdata/`). **A change to any prompt, offer or their assembly fails them: run
`make generate-desktop-prompts` and review the diff.** A byte that moves in the prefix is a cache
miss on every open chat. The `*SystemPrompt`, `*Box` and `Max*ToolCalls` exports exist for the
goldens.

## Chat (`internal/agent/chat`)

**Sending a message is a save, not a phone call.** `Send` writes the question, its queued run and
the empty answer, and returns; the turn runs on a service goroutine and windows see the answer in
`app.db`. A turn is `loop.Run` with the chat's `turn` as its recorder, its rounds in the answer's
row. → [ADR: the answer is a record](../../../docs/adr/2026-09-10-the-answer-is-a-record.md),
[ADR: chats live in `app.db`](../../../docs/adr/2026-09-10-chats-live-in-app-db.md),
[ADR: a turn is a run over its message](../../../docs/adr/2026-09-17-a-turn-is-a-run-over-its-message.md),
[ADR: tool rounds live in the answer's row](../../../docs/adr/2026-09-14-tool-rounds-live-in-the-answers-row.md).

### The turn

- `Send` checks the model and effort **after the replay lookup** (a replay answers by `requestID`
  alone, so a catalog change must not refuse a retry); a bad name is `ErrBadRequest` with nothing
  written (chat maps `llm`'s sentinels itself).
- **The box is `boxFor`**: the target's list `.WithoutNames` the disabled tools, read where the
  turn is reserved; its subagents keep it. Off leaves the offer, not the box,
  so stored calls draw. `AffinityKey` is the chat id.
- `prompts/system.md` states only what is true now and has every cluster command name the card's
  `cluster.context` (the app's cluster need not be the kubeconfig's current one).
- **`history` (`turn.go`) is what the model is shown**: every message but the answer being written,
  none with no blocks (no wire accepts one), and a row's tool blocks only when it settled
  `Complete`, so an answer that failed empty is left out.
- **Never retry a failed turn.** A context-full refusal settles `failed` with `contextFullText`, a
  plan's usage limit (`llm.Error.UsageLimit`) with `usageLimitText`; `ChatMessage.Failure` reads
  each back as the wire's `failure`, which the webview branches on, never `error`.
- A subagent's model resolves afresh under its parent (`ResolveUnder`), never onto the parent's
  provider, so it signs under the parent's session.
- `clusterID`, the sandbox switch and the network toggle are read once per turn, so a mid-turn flip
  changes the next turn; subagents and tasks inherit them.
- **`roomFor` (`context.go`) refuses a send the chat cannot fit** (`ErrChatContextFull`). →
  [ADR](../../../docs/adr/2026-10-17-a-send-the-chat-cannot-fit-is-refused-from-the-providers-count.md).
- **A chat has no dialect or model; each run records its own** provider, model, effort and dialect
  when the send is accepted. → [ADR: a chat can switch to any model](../../../docs/adr/2026-09-25-a-chat-can-switch-to-any-model.md).

### Files and tables

**A chat's files are `<data>/chats/<chatID>`** (`chatdir.go`), each opened through `rootdir` so a
swapped-in link reaches nothing else. File stamps are memory only, which fails closed on restart.
**`Delete` refuses a non-UUID id** (it names the directory removed). The start sweep lists the
directory **before** reading the chats, since a send can commit before `Start`.

The migrations are the only schema authority.

- `mode` and `cluster_id` are fixed at creation; each mode and cluster lists only its own chats.
- `sandbox_disabled` and `network_enabled` are the user's switches; leaving the sandbox clears
  network in the same statement, and network cannot go on outside it. Neither moves `updated_at`.
  → [ADR](../../../docs/adr/2026-09-30-leaving-the-sandbox-is-the-users-switch-for-a-chat.md),
  [ADR](../../../docs/adr/2026-10-04-network-is-the-users-switch.md).
- **Ids are UUIDv7 and identity alone**: transcript order is `messages.seq`, list order
  `chats.updated_at`.
- `content` is the provider's blocks **verbatim**; draw from the blocks, never the role.
- `updated_at` moves on create and settle only — not on a checkpoint or the startup reconcile.
- **`messages` has no status or model columns**: an answer's are its run's (`messageStatusOf`).
  `FinishReason` is the run's latest `llm_calls.stop_reason`. `dialect` is the run's, and reads its
  citations after the provider leaves the catalog.
- Runs and answers reference each other and cascade off the chat, so a chat goes with **one
  `DELETE`** (`stmtDeleteChat`).
- **Every statement is named in `statements.go`**, declaring `OnWriter`, `OnReader` or `OnBoth`. →
  [ADR: app.db SQL discipline](../../../docs/adr/2026-09-14-app-db-sql-discipline.md).
- **Usage is inclusive in public and uncached on disk** (`llmCallEntry.setUsage`): `input_tokens`
  excludes cache reads and writes, and **unknown differs from zero** (four NULLs).

### Writes

**A send is one transaction** (`writeTurnRows`): `checkChat` refuses a switch that differs from the
row (`ErrChatSandboxChanged`, `ErrChatNetworkChanged`), then the turn is reserved and the question,
run and answer inserted, in that order. **A send is idempotent on its `requestID`** (`seenBefore`,
one joined statement, run first and again in the transaction). On a hit the key is the whole
identity; a retry after the chat is deleted is a fresh send.

**The turn is the recorder** (`turn.go`). `LLMCallStarted` claims the run and opens the call row in
one transaction, so a cancel that landed first asks no model. `run` recovers a panic in the stream
into a failed settle. Every `checkpointEvery` (3s) the live content is written, notifying nothing.
**Settlement is one transaction** (`settleRow`) that rewrites every call row (`writeCalls`, which
heals refused writes and closes a panicked tool `{"error":"interrupted"}`). **A failed settle is
retried** (`settleUntilLanded`) while the slot is held; a service that stops first leaves it for
`Start` to fail as stranded.

**A call is a row before it happens.** →
[ADR](../../../docs/adr/2026-09-17-a-tool-call-is-a-committed-row-before-it-runs.md). A tool row is
**committed `running` before the tool runs**, then rewritten with its outcome; a refused call gets
one `failed` row with no `started_at`. `seq` is the call's place among the reply's `tool_use`
blocks; ids order nothing. Every write is a full-row upsert by id. `result` is what the model read;
`error` is always a JSON object. A server call is a row with `runs_on = 'provider'` and no status,
span or result (CHECKed), which the settle and stranded sweep leave alone. **No external operation
follows a failed write**: running rows land before any call runs, and a failed write under one call
cancels the reply's context (`spawn`).

**A call row records its tool's contract at its first write** (`newToolCall`, from the service's
box, so a call the turn was not offered records one too), and no later write changes it. A shown
action and a watch's result are decoded through the box, under the row's contract; one the box
refuses costs the call its action, lines or view, never the read, and logs one warning carrying the
sentinel alone (`warnRefused`). A sandbox action whose stored request does not decode is served with
its approval and a null action and request, and offers Deny alone. →
[ADR](../../../docs/adr/2026-10-25-a-tool-call-records-its-tools-contract.md).

**The live message lists every call, off its rows.** `ChatMessage.ToolCalls` is one JSON string, so
the record stays comparable. **`marshalToolCalls` is the one spelling**, on the live and stored
paths alike, since SQLite's `json_object` and Go escape differently. The tool-call statements are
`OnBoth`, since `seenBefore` runs inside `Send`'s write transaction.

### Concurrency and lifecycle

- **`Chat` and `ChatMessage` are comparable by value**: `sql.Null*`, never pointers. The watch diff
  is `==`; pointers would re-send every message on every ping.
- **One turn per chat**, a mutex-guarded reservation in `Send`, never in a composer.
- **The turns mutex is never held across a database call** (one writer connection: deadlock).
- **A turn ends in one order** (`runTurn`): settle, drop the overlay and reservation, notify.
  Reversed, a watcher copies a stale `streaming` overlay over the settled row.
- **`Delete` cancels and joins the chat's turn** before its rows go, marking the chat `deleting` so
  `reserveTurn` refuses a send meanwhile. Its write runs on the service's context, not the caller's.
- **Work that outlives its caller joins the service's `WaitGroup` through `enter()`**, never a bare
  `wg.Add`, which races stop's `Wait`. Once stopping, `Send` and `Delete` answer `ErrStopping`. A
  failed `Start` joins the watches that arrived before it.
- **On startup every unfinished run becomes `failed` and every open model call `stranded`**
  (`failStrandedRuns`, `closeStrandedLLMCalls`). Nothing resumes.
- **The chat sweeper** (`sweep.go`) deletes marked clusters' chats, and pings `KeyClusters` only
  when a delete removed a row — a no-op ping would loop with the mirror.
- **Two watches** (`chats`, `messages/<chatID>`): subscribe, read, `Added` frames, one `Bookmark`.
  Writers notify **after** commit. A turn's `Progress` pings the messages key, so a chunk is a
  re-read and a diff. `stream/<chatID>` is not wired: a stream ping that finds no live message
  sends nothing.

### Subagents, monitors and tasks

**A turn can hand a task to a subagent, which runs in the background** (`subagent.go`). `Agent` is
not `Gated` (each subagent call is) and answers at once. Its run and the `Agent` row's
`spawned_run_id` land in the task row's transaction. `agentTask` runs under a context only a stop
cancels, so a parent's Cancel leaves it running. `SubagentBox` drops `Agent`, `Memory`, `LogsView`
and `LogsWatch` (depth is one; a view is the user's window; a watch's point is the turn it starts)
and keeps `LogsRead`. **Each run owns its own calls** (`runJournal`). **`watchTask` is the one
writer of the end.** → [ADR](../../../docs/adr/2026-09-25-an-agent-is-a-tool-in-the-box.md),
[ADR](../../../docs/adr/2026-09-25-an-agent-runs-in-the-background.md),
[the Agent tool](../../../docs/security/2026-09-25-agent-tool.md),
[background agents](../../../docs/security/2026-09-25-background-agents.md).

**A monitor run is a run of the chat service** (`RunMonitor`): one per cluster
(`ErrMonitorInFlight`), refused without a sandbox or for a gone cluster, and `ErrMonitoringOff`
when the caller's `Admit`, called inside the run's insert, refuses. It runs in
`monitorSession()`, told the card alone (never memory), offered `MonitorBox`, which keeps `LogsRead`
and drops `LogsView` and `LogsWatch`; nothing it asks is approved. →
[ADR](../../../docs/adr/2026-10-05-a-monitor-run-is-a-run-of-the-chat-service.md),
[the monitoring session](../../../docs/security/2026-10-05-the-monitoring-session.md).

**`agent/chat` holds the background tasks** (`tasks.go`): commands, agents and watches share the
slots (`maxTasksPerChat`, `maxTasks`). **The watcher writes the end row before releasing the
slot**, so the count matches the rows still running. Five stops (the model's `TaskStop`, the user's
`StopBackgroundTask`, `unansweredLimit`, `Delete`, `stop`); the first names `stopped_by`. `Start`
marks a row still `running` as `lost`.

**A watch is the third kind** (`watches.go`). `Runtime.Watches` is `chatWatches`, whose `Start`
holds a `tools.Watch` through `startTask` with the watch's end, as an agent's is: its file is
closed, empty, before it runs; `watchTask.Wait` runs the watch's `Wait`, and any stop is its `Stop`.
The end is `matched`, `expired`, `failed` or `stopped`; `background_tasks.result`, the watch's
`tools.WatchResult` as JSON, is set on every finished watch and NULL on every other row. **A watch
is never written notified**, a model's stop included: `TaskStop` answers before the lines are
written, so the notice carries them.

**Notices** (`notices.go`) are `llm.BlockTaskNotification` blocks, sent as an escaped
`<task-notification>`. They reach the model riding a send, or in a turn of their own:
`startNoticeTurn`, kicked when the slot is free and `startsTurn` passes: an exit, or an agent's or a
watch's end (not a stop) under a user's send, so no chain runs past the first turn the user did not
send. A watch's notice is its sample, read off `result`; its kind is its call's, through the box. It
makes every check a send makes. Nothing kicks at startup. → [background
commands](../../../docs/security/2026-09-23-background-commands.md), [the model watches
logs](../../../docs/security/2026-10-18-the-model-watches-logs.md).

### Approvals and grants

**A command is asked before it runs** (`approval.go`). `Approve` registers its waiter in
`service.pending` **before anything is written**, so no id is decidable ahead of its request, then
writes the call `awaiting_approval` and flips **this run** to `waiting_approval`. The waiter's
channel is buffered. **The commit is the line a cancel stops at**: a cancel that wins leaves the
approval `pending`; a decision that wins is written without the cancel, and a decision write that
fails refuses the call `not-run`. **One row per call** (`t.openTool`); `ToolCallStarted` sets
`started_at` only once its write lands. A subagent's wait ends at `unansweredLimit`.

**A sandboxed command's cluster or cloud platform action** is asked the same way under the call
that sent it, in one list, `SandboxActions` (`Runtime.ActionAsker`, `askAction` on a proxy
goroutine): `journalMu` serializes journal writes and is never held across a wait; `askMu` keeps one
ask at a time. A policy decision is recorded with no wait (`recordAction`). A refused journal write
sends the command a 403 and the change is not sent. → [the bash
tool](../../../docs/security/2026-09-18-bash-tool.md), [cluster writes
ask](../../../docs/security/2026-09-29-cluster-writes-ask.md).

**A decision claims its waiter first** (`service.Approve`, `waiter.decide`): a call's own approval,
and an action whose `Grantable` is false, take `Once` and `Deny` alone; a grantable one, any
duration. A `Chat` or `Always` rule is written **before the decision is delivered**, outside
`turnsMu`; a failed write puts the waiter back. An `Always` rule goes to `settings.AddRule`.

- A chat's rules and folder grants are read on every decision (`settings.ScanRules`); `agent/chat`
  writes no row whose `chat_id` is NULL. Each write or revoke notifies `grants/<chatID>` after its
  commit (`ListenForGrantChanges`); `sandboxFoldersStateWatch` re-reads on it.
- **A session's policy is read live** (`sessionFor`): the account's mode (a failed read is
  read-only), then the chat's and the always rules (a failed read is `permissions.Unread`). The
  boundary limits the monitor.
- **`foldersFor` is the one builder of a session's folders**, the chat's then the always ones,
  re-checking each; a failed read is an error, which fails the run, the file tool or the send.
  `FoldersFor(ctx, "")` is the always folders alone, for a run with no chat.

→ [ADR: a prompt names the action and offers a duration](../../../docs/adr/2026-10-04-a-prompt-names-the-action-and-offers-a-duration.md),
[ADR: a folder grant is its own row](../../../docs/adr/2026-11-24-a-folder-grant-is-its-own-row-and-a-mode-and-a-rule-belong-to-a-cluster.md),
[path grants](../../../docs/security/2026-10-04-path-grants.md),
[the settings move into app.db](../../../docs/security/2026-11-25-settings-in-app-db.md).

### The context block

**A question carries a context block when it has changed** (`questionBlocks` against
`newestContext`): the cluster card, `## Memory`, `## Workspace`, `## View` when the send carries a
snapshot, and `## Sandbox`, always last, whose `cloud` key names the build's cloud platforms whose
switch is on. **A context block is always its message's first block**, where the query reads it.

- The card and notes render before the transaction (`contextText`) for the chat's stored cluster,
  never the send's argument; the workspace, view and sandbox sections are appended inside it. An
  unrenderable card is `clustercard.Unavailable`; the send is never refused over it.
- **The card (`clustercard.Render`) is pure, under `Budget`, every value an escaped JSON string, and
  carries no counts.**
- **The View** (`view.go`) restates the named call's stored view (`viewCall`: a `LogsView` call's
  action, a `LogsRead` call's `viewAround`, a subagent's included, or a finished `LogsWatch` call's,
  joined from its task's `result`), never the client's, with what was on screen and the selected
  lines through `logs.ForModel`, under `viewBudget`. A snapshot naming no such call of this chat is
  `ErrBadRequest`. A notice turn and a subagent's brief drop it (`withoutView`).

→ [ADR: the cluster card rides the user message](../../../docs/adr/2026-09-13-the-cluster-card-rides-the-user-message.md),
[ADR: the cluster card carries no counts](../../../docs/adr/2026-09-14-the-cluster-card-carries-no-counts.md),
[ADR: the cluster service resolves a cluster](../../../docs/adr/2026-09-27-the-cluster-service-resolves-a-cluster.md).

### The wire

Both chat subscriptions go through **`subscriptionStream`**, never `ptrSubscription`. **The GraphQL
`Provider` declares `id`, `label` and `llmDialect` alone, in `gqlgen.yml`**, so a regen never
reintroduces a key or endpoint field as a stub. The enums bind member by member (`enum_values`); the
reverse direction is not caught, so `TestLLMDialectBindsEveryMember` walks `llm.Dialects`.

**A refused mutation names its reason in `extensions.code`** (`chatErr`, `graph/util.go`).
**Return `errors.Clone(…)`, never the `graph/errors` value itself**: gqlgen stamps `Path` onto it.

## Monitor (`internal/agent/monitor`)

A `Registration` per integration; `monitor/kubesignals` is the one, a target per cluster, and the
one package that reads a key as a cluster's id (`ClusterID`).

- **The core knows no integration**: it never parses a target's key, nor reads a subject's
  attributes or a `detail`, and is tested over a fake integration. A signal carries its
  integration's prefix (`New` refuses one without) and both its windows.
- **An integration lists durable targets.** The reconcile listens before it lists, and a failed
  listing deletes none; a target whose integration the build lacks is left alone.
- **`check` bounds every reading before the fold**: over a bound or a code the core lacks is
  `invalid`, an unregistered name is dropped. A signal is judged only while every read it depends
  on answered in full.
- **`fold` is pure and measures wall time**: only an interval between two judged folds, at most
  two cadences apart, earns time. A new UID, then stale, then the reading; an occurrence ends
  `resolved`, `stopped` or `stale`. Memory changes only after the commit.
- **The switch stops a target in its own transaction**, moving `generation`; a fold commits only
  under the generation its reading began under, and the loop drops memory on a new one.
- **The notice follows the commit** and names each key whose served rows moved; a fold that only
  advances `last_seen_at` sends none. A `KeyMonitorTargets` notice never starts a reading.
- **The reads serve the registered integrations' targets alone**, and what is under them. Each
  record is comparable (a zero value for null, JSON as stored, which the resolver decodes), so a
  watch diffs with `==`. A subscription joins `wg` through `enter`; `Close` ends it too.
