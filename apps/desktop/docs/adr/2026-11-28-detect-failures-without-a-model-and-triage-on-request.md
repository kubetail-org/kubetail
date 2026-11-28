---
title: Detect failures without a model, and triage on request
date: 2026-11-28
scope: cross-cutting
status: Accepted
---

# Detect failures without a model, and triage on request

## Context

Kubetail Desktop could take a monitor run (`chat.RunMonitor`): one read-only, unattended run of the
agent on a cluster, returning a report. Nothing started one. Issue #1606 asked what should: a fixed
interval, a button or both, with what brief, and at what cost to the user. A run spends model calls,
and the user may have many clusters and the app open all day.

The local cache already holds every Pod, Node and workload of each cluster, current to the last
watch event, and reading it costs nothing.

## Decision

**Detection is the sidecar's own code, with no model.** `internal/agent/monitor` reads each target
every 30 seconds through its integration (`monitor/kubesignals` for a cluster), finds a fixed list of
signals that are true now, and folds them into incidents. It is on by default.

**Triage is a model run, and runs only when asked.** A press of *Triage with AI* on an incident
queues one monitor run, briefed with that incident alone (`monitor/kubetriage`). Automatic triage,
off by default, is the user's choice of a model in Settings, and then triages each new incident
that is not baseline, capped at 6 runs per target per hour. The report opens with a verdict,
`ATTENTION` or `DISMISS`.

This replaces *Nothing schedules a run* in [ADR: a monitor run is a run of the chat
service](2026-10-05-a-monitor-run-is-a-run-of-the-chat-service.md); the rest of it holds.

## Alternatives considered

- **A model run on an interval, briefed with a health check.** Each run costs model calls whether
  or not anything is wrong, and a run reads the cluster again where the cache already has it. Its
  findings would be prose, with no state to say that a failure is the same one as last time, has
  ended or has come back.
- **A run on a button alone.** The user would have to suspect a failure before Kubetail found one.
- **Detection by a model over a cheaper summary.** It keeps the cost and loses the determinism: a
  signal's windows and its alert could not be tested.

## Consequences

Kubetail finds only what the signals name; anything else waits for the user or a new signal. Each
signal is pure code, tested against a fake cache, with its own windows. Model spending is the
user's to start, so no cost appears without a press or a setting. The monitor run built in
October gained its first callers, both through `kubetriage`.

## Revisit when

A failure worth finding cannot be stated as a state of the cache, or users ask Kubetail to look
for what they cannot name.
