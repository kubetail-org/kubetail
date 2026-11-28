---
title: Give a target one UID at a time, and an incident the UID it opened under
date: 2026-11-28
scope: sidecar
status: Accepted
---

# Give a target one UID at a time, and an incident the UID it opened under

## Context

A cluster's target is keyed by the cluster's id, which follows a kube-context. The context can come
to reach another server: a kind cluster deleted and created again under its name, a kubeconfig
edited to point elsewhere. The new server can hold a workload of the same name with the same
failure. A triage reads the cluster, and must read the server the incident happened on.

## Decision

**A target has one UID at a time**: what its last reading reached, for a cluster the server UID of
its active cache. The monitor writes it to `monitor_targets.current_uid`. A reading under a new UID
ends the target's open occurrences as `stopped`, drops the target's windows and its baseline, and
writes the new UID, in one transaction.

**An incident belongs to the UID it opened under** (`incidents.target_uid`), which is part of its
match. A failure on the new server opens a new incident, though its subject has the same name, and
alerts. A target that returns to an earlier UID takes up that UID's incidents. A triage runs for its
incident's UID, and a run that finds another server ends failed with `server_changed`.

## Alternatives considered

- **Incidents keyed by the target alone.** A failure on the new server would continue the old
  server's incident, raise no alert, and be triaged against a server it never happened on.
- **A new target per server.** The user's switch and the target's place in the screens would reset
  whenever the context moved.
- **The kube-context's name as the identity.** It is exactly what changes servers without changing.

## Consequences

An integration whose targets have no identity apart from their key returns the key as the UID. A
reading that names no UID judges nothing. The old server's incidents stay listed under the target
and gain no occurrence while it reaches another. Every read a triage makes checks the server it
reaches, which is the cost: the card, `KubeQuery`, `LogsRead`, the log service and a command's
claim each refuse another server's cache or connection.

## Revisit when

An integration's target can reach several identities at once, such as an account's regions.
