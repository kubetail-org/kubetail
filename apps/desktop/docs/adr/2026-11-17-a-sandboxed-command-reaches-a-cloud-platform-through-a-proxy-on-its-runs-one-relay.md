---
title: A sandboxed command reaches a cloud platform through a proxy on its run's one relay
date: 2026-11-17
scope: sidecar
status: Accepted
amended_by: [Every cloud platform is on until the user turns it off](2026-11-18-every-cloud-platform-is-on-until-the-user-turns-it-off.md)
---

# A sandboxed command reaches a cloud platform through a proxy on its run's one relay

## Context

A sandboxed run reaches one thing outside itself: its chat's cluster, through one relay from a
loopback port to a Unix socket, where a grant holds the run's token. An agent also needs the user's
cloud accounts and code host (AWS, GitHub, Google Cloud, Azure), which the sandbox denies: the
logins are on disk, the sandbox reads none, and no credential variable passes. Issue #935 listed
what the four cloud platforms' proxies share and where they disagreed.

## Decision

- **One relay; the door routes by `Host`.** The run's socket serves one mux (`tools/bash/proxy.go`,
  `door`), keyed by `Host`, in front of the cluster grant and each cloud platform's. Anything else
  is the cluster grant's `401`. The door authenticates nothing: each grant checks its own token.
- **Plain HTTP.** The hop is the run's own loopback to a socket in its directory. A cloud platform
  whose client refuses plain HTTP adds a TLS route in its own step.
- **One token per grant.** `runproxy.Grant` mints 256 bits per cloud platform per run, so a token
  read out of one cloud platform's output opens that cloud platform alone.
- **One action family.** `permissions.Action` gains `Target`; `tools.ActionRequest` gains
  `Cloud`; the call serves one ordered list, `ToolCall.sandboxActions`, whose request is a
  `ClusterWrite` or a `CloudRequest`.
- **One gate in `permissions`.** A cloud platform's action is `Forbid` (it always asks),
  `Rule.Matches` is false for it, and no mode reads it. The session's `NoPrompts` and `NoSecretData`
  still apply.
- **Consent is a switch.** `sandbox.<platform>` in `settings.json`, off by default, read at a run's
  start and on every request.

## Alternatives considered

- **A relay per cloud platform.** It changes `run/sandbox`, and on macOS opens one loopback port per
  cloud platform per run.
- **TLS for every cloud platform.** `gh` verifies through the system's verifier and reads no
  certificate file, so a per-run certificate serves no one but `az`.
- **A second list on the call.** One command can change the cluster and a cloud platform in turn,
  and two lists cannot say which came first.
- **A type per cloud platform.** The four requests are one thing to the user, an HTTP request to a
  named host as their identity; what differs is lines of text (`Details`).
- **A token per run.** Clients print, log and embed their credential slot, so one cloud platform's
  output would open the others.
- **Consent read from a login.** On EKS, GKE and AKS the cluster credential and the cloud login are
  one identity, so a login on the machine says nothing of what the user wants an agent to read.
- **A gate per proxy.** Four hand-ordered copies of one rule would drift.
- **A kube-context's mode deciding a cloud platform's change.** A mode is the user's word on one
  cluster, and no login is tied to it; `Auto` rests on the cluster classifier's curated class 5
  list, which no cloud platform has.

## Consequences

- A routing mistake could hand a request to the wrong handler; each grant authenticating for
  itself bounds it.
- The relay leads to a mux, so on macOS a run with a cloud platform and no cluster holds a loopback
  port.
- No rule, mode or duration decides a cloud platform's action yet: each change asks, Once or Deny.
- The cluster grant keeps its own token, life, limits and queue; building it on `runproxy.Grant`
  is left for later, so there are two copies until then.

## Revisit when

A cloud platform's users approve the same change often enough to want a rule, or a cloud platform
needs a route the loopback authority cannot key.
