---
title: The dashboard's namespace scope is a search param
date: 2026-11-12
scope: frontend
status: Accepted
---

# The dashboard's namespace scope is a search param

## Context

[The cluster is the app's scope](2026-09-11-the-cluster-is-the-apps-scope.md) removed a namespace
scope that nothing read, and said one could come back when a view did. The new dashboard's mockups
put a namespace switcher at the head of every page, and its kind pages, Events and Overview all
read it. The sidecar serves no namespaced watch: every kind is watched cluster-wide.

## Decision

**The namespace scope is the `namespace` search param on `_app`**, beside `kubeContext`,
`resource` and `chat`. `retainSearchParams` keeps it across the switch to chat and back. Absent is
every namespace; it holds one namespace or none. It is validated leniently, as `resource` is: any
non-empty string. `useNamespaceScope` (`src/lib/namespace-scope.ts`) reads and writes it.

**A cluster switch from the omnibox clears it**, as it clears `chat`: a namespace belongs to a
cluster. `setContext` leaves it alone.

**Only the dashboard reads it.** A page filters its cluster-wide watch by the scope. The kinds
catalog is subscribed with it, and counts each namespaced kind in the namespace
(`clusterCachedDataKindsWatch(namespace:)`, `kubestore.KindsIn`); each frame echoes the namespace
as provenance. Chat mode ignores it, and no chat send carries it.

## Alternatives considered

- **Chrome state in a provider, like the right sidebar.** The scope decides what a page shows, so
  a deep link and Back must carry it, which is what [URL params as window
  state](2026-08-09-url-params-as-window-state.md) is for.
- **Several namespaces in one scope.** The mockups draw one. A list can come later
  (`docs/TODO.md`).
- **A trigger-maintained count per kind and namespace**, beside `kind_counts`. It reads in
  `O(kinds)`, but it is a migration of a shipped schema and a trigger on every write, for a read
  that `objects_ns_kind` already answers by index.
- **Keep the namespace across a cluster switch.** Clusters often share namespace names, but a
  namespace the new cluster lacks would leave every page empty with no reason given.

## Consequences

Every dashboard page reads one scope from one place, and a link names its namespace. Each page
filters a cluster-wide watch, so a scoped page still streams the whole kind. A scoped catalog read
counts the namespace's objects on each read instead of reading the trigger-maintained totals, and
a namespace switch re-subscribes the catalog.

## Revisit when

The sidecar serves namespaced watches, a scoped catalog read grows slow enough to want
maintained per-namespace counts, or the scope needs several namespaces.
