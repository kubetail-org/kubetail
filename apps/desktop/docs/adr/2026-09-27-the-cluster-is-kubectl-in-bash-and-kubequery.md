---
title: The cluster is kubectl in Bash, and KubeQuery over the mirror
date: 2026-09-27
scope: sidecar
status: Accepted
---

# The cluster is kubectl in Bash, and KubeQuery over the mirror

## Context

The agent needs to read and change the chat's cluster. A set of structured Kube tools, one per
kubectl verb, was offered as stubs in a dev build and measured before anything was built behind
them. On the Messages API they cost 10.6k tokens a fresh prefix, against an estimate of 6–7k, and
most of what they did, `kubectl` in Bash already does.

## Decision

**The agent reaches the cluster through `kubectl` in Bash.** A model already knows kubectl, its
flags and its output. Bash's approval request shows the exact command, which is the gate every other
command already passes.

**KubeQuery is the one cluster tool**: read-only SQL over the mirror. It does what kubectl cannot:
joins across kinds, aggregates, status history, and events past the cluster's one-hour TTL.

## Alternatives considered

- **Structured Kube tools, one per verb.** Thousands of tokens on every round, a vocabulary no model
  has seen, and a tool per verb to build and keep in step with the API, mostly for what Bash already
  does.
- **One `Kubectl` tool taking a command line, parsed by the sidecar.** Cheaper, but the sidecar's
  parse becomes the security boundary, and its grammar grows toward a shell. Bash already runs the
  real binary behind an approval request.

## Consequences

- A cluster read through kubectl asks the user, as every command does. Only KubeQuery reads without
  asking, and it reads the mirror alone.
- KubeQuery sends object bodies to the model provider on the model's request. It needs the
  body-read security record, rows in `security-model.md`, and the consent decision before it
  reaches a release.

## Revisit when

- Models misuse kubectl in Bash often enough, or its approval requests tire users enough, that a
  structured read would pay for its prompt.
