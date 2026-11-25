---
title: The dashboard nav groups kinds by scope and API group, and lists every kind flat
date: 2026-11-12
scope: frontend
status: Accepted
---

# The dashboard nav groups kinds by scope and API group, and lists every kind flat

## Context

The new dashboard's mockups draw the sidebar as two sections, *Cluster* and *Namespace*, each
holding groups of kinds (Infrastructure, Workloads, Network, …) with live counts. The tree it
replaces ([the dashboard nav merge](2026-08-09-dashboard-nav-merge.md)) had one list of groups,
put discovered kinds behind *Show more…* or a chevron, and left the core group's other kinds and
every unmapped API group out. A cluster serves dozens of kinds with no objects, and the mockups
hide them.

## Decision

`buildDashboardNav` (`src/lib/dashboard-resources.ts`) places every kind the catalog serves:

- **The section is the kind's scope.** `Cluster` → *Cluster*, `Namespaced` → *Namespace*.
- **The group is the curated kind's, else its API group's** (`API_GROUP_TO_GROUP`), else for a
  core kind its plural's (`CORE_KIND_TO_GROUP`), else *Custom resources* for a CRD and
  *Extensions* for anything else. One list (`GROUPS`) orders the groups; a section draws the ones
  that hold its kinds.
- **A discovered kind is listed flat in its group**, after the curated kinds, by label. There is
  no *Show more…*.
- **Events are the pinned row**, never a kind in a section; `events.k8s.io`'s copy is left out.
- **A curated or core kind's id is its plural; any other's is `<group>/<resource>`.**

Before the catalog's `Bookmark` the tree is the curated kinds without counts. After it, the tree
lists only what the catalog serves, and `filterNav` hides kinds with no objects unless the kind
is the current page or a search is running. Group rows only open and close.
`DashboardResource` stays an open string, as the earlier ADR decided.

## Alternatives considered

**Keep *Show more…* for discovered kinds.** It hid most of a cluster's kinds behind a click, and
the mockups draw none. Hiding empty kinds now keeps the list short instead.

**Sections by curated group alone.** Several groups hold both scopes (Storage, Access control),
so a fixed group-to-section map would put PersistentVolumes beside PersistentVolumeClaims under
one heading that is wrong for one of them.

**Leave unmapped API groups out.** A CRD would have no way into the nav; *Custom resources* and
*Extensions* give every served kind a row.

## Consequences

Every served kind is reachable from the sidebar. A new curated kind needs its API group and scope
in `GROUPS`; a new built-in API group needs a row in `API_GROUP_TO_GROUP`, or it lands in
*Extensions*. Old group ids in a URL (`resource=workloads`) now read as a kind this cluster does
not serve.

## Revisit when

The sidecar gives counts per namespace, or a health verdict per kind: the sidebar's filters and
counts are drawn from those.
