---
title: Keep each integration behind one interface, under a core that knows none
date: 2026-11-28
scope: sidecar
status: Accepted
---

# Keep each integration behind one interface, under a core that knows none

## Context

Kubernetes is the one system the monitor reads. The spec for monitoring (#1606) named others a
user would want watched next: an AWS account, a GCP project, an Azure subscription, a Cloudflare
account, a host. Each has its own credentials, its own reads and its own failures, and none of them
has a Pod or a cluster.

## Decision

**`internal/agent/monitor` is a core that imports no integration.** `monitor.New` takes a
`Registration` per integration: its name, its cadence, its signals and an `Integration`, which
lists its targets, says when the list may change, and takes one reading of a target. A reading is
plain data: the signals true now, each with its subject, and the signals it could not judge, each
with a code from the core's fixed list (`ReadErrorCode`).

The core holds everything else: the targets' rows, the windows, the incidents, the baseline, the
alerts and the triage queue. It compares a target's key and a subject's key and never parses
either, and stores a subject's attributes and a reading's detail without reading them.
`monitor/kubesignals` is the one package that knows a Pod or a cluster. A triage goes through a
`Triager`, which `monitor/kubetriage` implements.

## Alternatives considered

- **A Kubernetes monitor, generalized later.** Every table, GraphQL type and screen would name a
  cluster, and the second integration would migrate them all.
- **An integration that folds its own readings into incidents.** Each would carry its own copy of
  the windows, the reopen rule and the baseline, and they would drift.
- **One interface per kind of system** (a cloud account, a host). It fixes a taxonomy before a
  second integration exists to test it.

## Consequences

The store, the fold and the loop are tested over a fake integration, and Go refuses a cycle from
the core to an integration. A new integration is a package, its registration in `runtime.go`, a
value of `MonitorIntegration` and its signals, and nothing in the core moves. The cost is
indirection for the one integration there is: the Kubernetes reading speaks the core's vocabulary,
and a cluster is reached from an incident through its target's key.

## Revisit when

A second integration needs something a reading cannot say: a vendor's own start and end times, a
severity that differs per alert, or a target of several parts.
