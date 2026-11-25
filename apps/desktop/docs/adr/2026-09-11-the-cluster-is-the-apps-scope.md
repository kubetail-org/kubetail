---
title: The cluster is the app's scope
date: 2026-09-11
scope: frontend, sidecar
status: Accepted
amended_by:
  - [A cluster is a row in app.db, mirrored into a runtime object it is deleted through](2026-09-16-clusters-are-rows-mirrored-into-beehive.md)
  - [The dashboard's namespace scope is a search param](2026-11-12-the-namespace-scope-is-a-search-param.md)
---

# The cluster is the app's scope

## Context

A window carried two high-level scopes in its URL: `kubeContext` and `namespace`. The omnibox read
`cluster / namespace`, and the namespace half was a complete control — a picker over the cluster's
own `v1/namespaces`, a param that yielded when the cluster had no such namespace, a hook and a
suite of its own. Nothing read it. No table filtered by it, no chat knew about it, and the plan for
each of those was to reach for it later.

Chats had the opposite problem. A chat records the mode it was started in, so each mode lists only
its own, but nothing tied a chat to the cluster it was about. One machine's recent list mixed every
cluster's conversations together, and a question whose answer was about `prod` read the same as one
about a laptop's kind cluster.

## Decision

**The cluster is the window's one high-level scope. Everything else follows from it.**

The namespace scope is removed rather than built out — the segment, the `namespace` search param,
and `useActiveNamespace`. A namespace filter can come back when a view actually reads one, and it
will be cheaper to add against real requirements than to keep alive against none.

**A chat is filed under a cluster**, `Chat.clusterID`, fixed at creation like `Chat.mode`. The
recent list shows the active cluster's chats in both modes; a new chat is filed under the active
cluster; with no active cluster the composer cannot start one and says so.

Three choices inside that are load-bearing:

**The identity is the cluster record's `ObjectID`, not the server UID.** The record id is known
before the cluster has ever been reached, which the server UID is not — a chat started against a
cluster that is not currently connectable still has somewhere to go. And the record outlives the
kube-context that produced it: the discovery pass is creation-only, a departed context leaves the
record behind, and a returning one reuses it. So a chat's cluster id stays valid across a context
that comes and goes, and only an explicit `clusterDelete` ends it.

`ObjectID` moved to `internal/lib/apimeta` for this — gqlgen binds the scalar to exactly one Go type,
and `services/chat` must name a cluster without importing `services/cluster`. That is what `apimeta` is for;
`DeltaFrameType` was already there for the same reason.

**The list is filtered client-side, like `mode`.** `chatsWatch` stays unscoped: one watch per
window, and an open chat has to be told apart from a deleted one *and* from one belonging to
another cluster, which needs the unfiltered set. A server-scoped `chatsWatch(clusterID)` is worth
revisiting when a machine holds enough chats for the cold list to cost anything.

**A cluster delete sweeps its chats, from the resolver.** `clusterDelete` is explicit, confirmed,
and only offered for a record no source declares. Its chats are about that cluster and nothing
would list, rename or delete them once the record is gone. The sweep runs in the resolver rather
than in either service: `services/chat` does not know clusters and `services/cluster` does not know chats, and
the resolver is the one place holding both. The same split checks that a chat's cluster exists
before a send creates it.

## Consequences

- A chat belonging to another cluster renders a notice with a `Switch to it` button, not the
  transcript. A draft typed into it waits in its outbox entry until the window is back on its
  cluster — there is nowhere else to put it, since unlike a deleted chat the chat is still there.
- Switching cluster from the omnibox clears the `chat` search param, so the dashboard panel returns
  to the new cluster's list. `setContext` itself leaves `chat` alone: it is what the `Switch to it`
  button calls, and clearing `chat` there would close the chat it is switching to.
- A held send carries the cluster it left with. A retry is the same send.
- A cluster record that disappears by any route other than `clusterDelete` would strand its chats
  in `app.db`, unlisted. No such route exists.
