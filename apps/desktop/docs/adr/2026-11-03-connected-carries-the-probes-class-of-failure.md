---
title: Connected carries the probe's class of failure as its reason
date: 2026-11-03
scope: cross-cutting
status: Accepted
---

# Connected carries the probe's class of failure as its reason

## Context

[The Cluster pass folds its claim into two conditions](2026-09-02-cluster-conditions-two-subjects.md)
gave a failed `Connected` the one reason `ProbeFailed`, with the attempt's error text as its
message. The probe already classifies every failure (`kubeconn.Reason`: `Unreachable`,
`TLSInvalid`, `Unauthorized`, `Forbidden`, `Timeout`, …), each with its own remedy. The class
stopped at the pool: the cluster status popup could show only the raw error, and a user had to
read a Go error string to tell a dead network from expired credentials.

## Decision

On a failed probe, `reconcileConnection` (`clusters.go`) sets `Connected`'s reason to the
attempt's `kubeconn.Reason` and keeps the attempt's message. `ProbeFailed` remains for a failure
the probe left unclassified. This replaces that ADR's sentence "`Connected` carries `Inactive`,
`Connecting` until a probe lands, or `ProbeFailed` with that attempt's message"; the rest stands.

The webview turns the reason into a few words (`connectionFailureSummary`,
`src/lib/cluster-health.ts`) and draws them as the title of the popup's connection alert, over the
message. An unknown reason reads as *Connection failed*.

## Alternatives considered

- **Classify in the webview from the message.** Re-derives in a second language what the probe
  already knows from typed errors, and breaks on any change to an error's wording.
- **A separate field for the class.** A second field beside a reason that would say less than it.
  The reason is the condition's machine-readable explanation, which is what the class is.

## Consequences

The silence rule holds: the reason moves only when the class of failure does, and the message
already moved with the error text. The connection timeline keys a run on its reason, so a change of
class (refused, then rejected credentials) starts a new run instead of extending the last.

`Connected`'s reasons are now open-ended by the probe's vocabulary. A new `kubeconn.Reason` reaches
the wire without a schema change and reads as *Connection failed* until the webview names it.
