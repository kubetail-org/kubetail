---
title: Keep the Dashboard's session keys in a Secret it creates
date: 2026-11-10
scope: dashboard
status: Accepted
---

# Keep the Dashboard's session keys in a Secret it creates

## Context

The Dashboard signs and encrypts its session cookie with the key pairs in `session.key-pairs`. In `desktop`, when none are configured, it writes a random pair to a file under `local-storage-dir` and reads it back on the next start. In `cluster` it had nowhere to write, so each process made a random pair at startup. A pod restart ended every session, and with more than one replica a cookie set by one pod failed on the next. The only fix was to hand-set `session.key-pairs`, which few installs did. The Helm chart lives outside this repo, so whatever we chose had to be something the chart can carry.

## Decision

In `cluster`, when `session.key-pairs` is empty, the Dashboard keeps its key pairs in the Secret `kubetail-dashboard-session-keys` in its own namespace, under the data key `key-pairs.json`, in the desktop file's JSON format. At startup it gets the Secret with its ServiceAccount; if the Secret is missing it creates one with a random pair, and if that create loses a race with another replica it reads the winner's. A Secret it cannot parse stops startup: replacing it would race the other replicas. `loadOrCreateSecretKeyPairs` in `modules/dashboard/pkg/app/helpers.go` does this.

The Dashboard's Role in its namespace grants `get` on that one Secret by name and `create` on Secrets, which Kubernetes cannot limit by name. Explicit `session.key-pairs` still win and need neither.

## Alternatives considered

- **The chart generates the Secret and mounts it.** Helm has no stable random value: `randAlphaNum` changes on every upgrade, ending every session, unless the chart reads the old value back with `lookup`, which returns nothing under `helm template`, Argo CD and Flux. Installs without Helm would need a step of their own.
- **Require `session.key-pairs` in `cluster`.** Every existing install without it would fail to start on upgrade, and each one would generate and store keys by hand.
- **A shared store such as Redis for server-side sessions.** A new component to run for a value that fits in a Secret.

## Consequences

- Sessions survive restarts and work across replicas with no setting. Rotating the keys is deleting the Secret and restarting every replica, which ends every session.
- The Dashboard's ServiceAccount can create any Secret in its namespace. Every deployment manifest, the chart's included, grants the two rules, and a rename of the Secret changes them.
- Two Dashboards that share a namespace and leave `session.key-pairs` empty share keys, and so accept each other's cookies.

## Revisit when

Kubernetes can limit `create` to a resource name, or the Dashboard gains a store of its own that could hold the keys.
