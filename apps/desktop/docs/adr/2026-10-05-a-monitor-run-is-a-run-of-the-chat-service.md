---
title: A monitor run is a run of the chat service
date: 2026-10-05
scope: sidecar
status: Accepted
---

# A monitor run is a run of the chat service

## Context

The agent-security note has two agents: the chat agent the user talks to, and a monitoring agent
that reads a cluster in the background and changes nothing. `session.Kind` named `Monitor`,
`agent_runs.trigger` named `monitor` and `clusters.monitoring_enabled` was served, with nothing
behind any of them. The audit trail already keys on a run, not a chat: `llm_calls`, `tool_calls`
and `approvals` hang off `agent_runs`, whose `chat_id` was nullable. The run's journal, the cluster
proxy's asker and the subagent's run-with-no-message are all `services/chat`'s and unexported.

## Decision

**A monitor run is one `agent.Run` of the chat service** (`services/chat/monitor.go`,
`Service.RunMonitor`), built as a subagent's run is, with no chat and no task: the chat's loop, box,
Bash tool, sandbox and cluster proxy, under `monitorSession()`. Its record is an `agent_runs` row
with `trigger = 'monitor'`, `cluster_id` set and `chat_id` NULL, two `CHECK`s holding the pair; the
chat's own journal writes its rows.

**The monitor is refused by its session, never by its kind.** `monitorSession()` sets `NoPrompts` and
`NoSecretData` and leaves `Policy`, `Network` and `Folders` nil, and the proxy, bash and the file
tools refuse it through the code that reads each field.

**Its asker records and never asks.** With an asker the proxy reaches `Authorize`, whose refusal is
recorded on the run; the nil-asker short-circuit stays for a background command alone.

**Nothing schedules a run.** The boundary is pinned by tests that call `RunMonitor` with the
production wiring — the real loop, the real Bash tool, the real sandbox and the real proxy over a
fake upstream. The folder and its teardown are wired into the app, so the lifecycle runs with zero
runs.

## Alternatives considered

- **A hidden chat**, a `chats` row in a mode no list shows: it needs a synthetic question, a
  title, a mode the `CHECK` and the webview's filters learn, and every list consumer to exclude it.
- **A table of the monitor's own**: it would duplicate `llm_calls`, `tool_calls`, `approvals` and
  the journal that writes them, and record nothing a chat's record does not.
- **A `monitor` package**: the journal, gate and teardown are `services/chat`'s and unexported, so the
  run would need them exported or rebuilt.
- **An asker of nil**: the proxy refuses such a write before `Authorize` and records nothing, so a
  hijacked monitor would leave no trail.

## Consequences

`services/chat` owns a run with no chat, so a reader of `agent_runs` cannot assume a chat. A monitor's
`agent_type` is `monitor`, and the agent's step can read its last `result` and `task`. The
package's name is stale; renaming it is a chore of its own.

## Revisit when

The monitoring agent needs a run another process or service owns, or a record the chat's does not
hold.
