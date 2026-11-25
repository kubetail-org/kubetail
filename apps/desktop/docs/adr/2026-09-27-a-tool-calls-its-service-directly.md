---
title: A tool calls its service directly
date: 2026-09-27
scope: sidecar
status: Accepted
---

# A tool calls its service directly

## Context

Two tools act through a service: Memory through `services/memory`, KubeQuery through `services/cluster`.
Each reached its service through a per-chat binding: an interface declared in `tools`
(`tools.Memory`, `tools.ClusterCachedData`), implemented by an adapter that fixed the chat's
cluster, handed to the tool on its runtime, and chosen per chat by `services/chat` through an interface
of its own (`MemoryService`, `ClusterCachedDataService`).

The layer cost more than it gave. The memory rules and refusals lived in `tools` so the tool could
name them, which made `services/memory` import `tools` for its own rules. Each binding repeated its
service's vocabulary in the tool's, with a table translating one into the other. Each capability
has one tool and one service, so the indirection bought no second backend, and nothing in the
module made it necessary: no service imports a tool.

## Decision

**A tool that acts through a service is built with it and calls it.** `memory.New(memorySvc)` and
`kubequery.New(clusterSvc)` in `app`'s `chatTools`. Each maps its service's own errors to the
model's codes.

**The chat's scope rides the runtime.** `tools.Runtime` carries `ClusterID`, the chat's stored
cluster, and `ChatID`. `services/chat` reads the cluster once as a run starts (`chatCluster`) and sets
both for the turn and the subagents it spawns; the model names neither.

**A subagent is offered no Memory tool**, beside no `Agent` tool: a note outlives the chat, and the
chat's own turns are what write one.

## Alternatives considered

- **Keep the bindings, gathered in one package.** It moved the adapters out of the services but
  kept the second vocabulary, the translation tables and the per-chat interfaces in `services/chat`.
- **A subagent runtime without `ChatID`, the Memory tool refusing one.** The rule would live in a
  missing field; leaving the tool out of the subagent's box says it where the box is chosen.

## Consequences

- One vocabulary per domain: `services/memory`'s rules and refusals are its own, and the tools read
  them.
- A tool's tests fake its service's interface, which is larger than a one-method binding.
- That a chat reaches only its own cluster rests on `services/chat` setting `Runtime.ClusterID` from the
  stored cluster, and on the tool using it; `security-model.md` names the tests.
- A tool is built once per process, so its service is a process-wide one.
