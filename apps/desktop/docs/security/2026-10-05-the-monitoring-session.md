# Security record — The monitoring session, 5 October 2026

**Subject:** a monitor run of the chat service — one run of the chat's loop under a cluster and no
chat, in a session that reads the cluster and changes nothing — its record, its folder and its
teardown. This is step 6B of the agent-security sequence. The living model is
[security-model.md](../security-model.md); the decision is
[a monitor run is a run of the chat service](../adr/2026-10-05-a-monitor-run-is-a-run-of-the-chat-service.md).

## What widens

A run that nobody watches. `chat.Service.RunMonitor` takes one run of the model over a brief,
on a cluster whose `monitoring_enabled` is on, reading injected cluster content with no user to
see a request. Nothing calls it yet but its tests: the monitoring agent, its schedule and its
Settings switch are a later step, and until then no cluster is watched.

## What holds it

- **The session holds less than any chat's.** `monitorSession()` is `Kind: Monitor`, `NoPrompts`
  and `NoSecretData`, and nothing else: no `Policy`, so the proxy reads `ReadOnly` with no rules;
  no `Network`; no `Folders`. Every field it leaves zero is read by something, and the monitor is
  refused by those readers, never by its kind (`TestTheMonitorSessionIsReadOnlyAndAsksNobody`).
- **Every change is refused at the proxy and recorded.** A create, replace, patch, delete,
  delete-collection, scale, status, ephemeral-container or binding write answers 403 *this context
  is read-only*, reaches nothing upstream, and is an `approvals` row `refused` with that reason;
  exec, attach, port-forward and proxy are refused before classification
  (`TestAMonitorWriteIsRefusedAndRecorded`, through the real sandbox and proxy).
- **Secret data never reaches it.** A get and a list of Secrets pass `[redacted]`, each an
  `approvals` row `refused`, *this session never reads Secret data*
  (`TestAMonitorReadsASecretRedactedAndRecorded`).
- **It asks no one.** Its asker records what the proxy decided and answers an ask with an error,
  so a write that reached it would be refused unsent (`TestTheMonitorsAskerRecordsAndNeverAsks`);
  a file call that would ask is denied with no waiter and no approval row
  (`TestAMonitorCallThatAsksIsDenied`).
- **It has no network.** A command asking for it is refused before it starts, and one that tries
  reaches no listener outside the run (`TestAMonitorHasNoNetwork`).
- **No granted folder reaches it.** A folder granted always or to a chat is read by that chat's
  command and not by the monitor's (`TestAChatsFoldersNeverReachTheMonitor`).
- **Its folder is its own.** `<data>/monitor/<cluster id>/` is 0700 (`TestANewMonitorDirIsOwnerOnly`);
  a monitor reads no chat's workspace and no other cluster's monitor folder, and a chat reads no
  monitor's (`TestTheMonitorCannotReadAChatsFolder`, `TestAChatCannotReadTheMonitorsFolder`).
- **It runs no background command, spawns no agent, keeps no memory, fetches no page and searches
  nothing.** A start is refused (`TestAMonitorBackgroundCommandIsRefused`), and the box leaves the
  other four out (`TestRunMonitorRecordsARun`).
- **It never runs unconfined.** A machine with no sandbox answers `ErrNoSandbox` before anything is
  written (`TestRunMonitorRefuses`).
- **It leaves a trail.** The run is an `agent_runs` row with `trigger = 'monitor'`, its cluster and
  no chat; its model calls, tool calls and recorded refusals hang under it, written by the chat's
  own journal (`TestRunMonitorRecordsARun`), and a crash leaves it failed at the next start
  (`TestAStrandedMonitorRunIsFailed`).
- **It ends with its cluster and the service.** The sweeper cancels a run, then removes its folder,
  once its cluster is not a live row, before the cluster's chats go
  (`TestAMonitorRunEndsWithItsCluster`, `TestAMonitorEndsWhenItsRowIsGone`,
  `TestTheMonitorsFolderIsSwept`); `stop` cancels and joins it (`TestAMonitorRunEndsWithTheService`).

## What narrows

Nothing for a chat: `serveWrite` and `serveSecretRead` are unchanged, and a runtime with no asker is
refused as before.

## Residuals

- A cluster read leaves the machine on the monitor's word, as on a chat's, and unattended.
- A monitor's report is cluster data at one remove: the agent's step must draw it as text.
- The prompt asks the model not to try a change; the refusals above are what hold it.
