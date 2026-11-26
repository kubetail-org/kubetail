---
title: Merge a curated nav tree with discovered kinds; DashboardResource is an open string
date: 2026-08-09
scope: frontend
status: Accepted
amended_by: [The dashboard nav groups kinds by scope and API group, and lists every kind flat](2026-11-12-the-dashboard-nav-groups-kinds-by-scope-and-api-group.md)
---

# Merge a curated nav tree with discovered kinds; DashboardResource is an open string

## Context

The dashboard sidebar navigates Kubernetes resource kinds. A hand-picked set (pods, nodes,
namespaces…) should always appear in a deliberate order, but a live cluster serves hundreds of
kinds — including CRDs unknowable at build time — and the nav should reach all of them without
burying the curated core.

## Decision

Two layers (`src/lib/dashboard-resources.ts`). The **curated** base is `DASHBOARD_NAV`, an
ordered `as const` tree. The **rendered** tree is `buildDashboardNav(serverKinds)`: every
non-curated kind from the active cache's live catalog is bucketed into a group by its API
group (`API_GROUP_TO_GROUP`). Discovered kinds always land in `moreChildren`, curated ones in
`children`, so the disclosure style is **derived from shape** by the renderer, with no stored
flag: a group with curated children shows extras behind "Show more (N)…" at the same depth
(one flat list, never nested deeper); a group with none collapses behind a chevron on its own
row. Counts join on by API group + resource plural (`CURATED_LEAF_API_GROUP`) so a CRD reusing
a built-in's plural in another group can't hijack its badge. Kinds with unmapped API groups —
including the core group — are deferred to the Custom Resources work.

Because dynamic ids (`<group>/<resource>`) aren't in the static tree, `DashboardResource` is
a **plain string**, not a closed union, and the `resource` search param is validated leniently
(any non-empty string), resolved at render time against the built tree.

## Alternatives considered

**A closed union of known kinds.** Rejected: it cannot name a CRD, and every cluster serves a
different set — the type would be a lie the first time a discovered kind is selected.

**Fully dynamic nav (no curated layer).** Rejected: ordering and grouping straight from
discovery reads as an API dump; the curated layer is the product opinion.

**A stored per-node disclosure flag.** Rejected: it duplicates what the node's shape already
says and can drift from it; deriving style from `children`/`moreChildren` keeps one source.

**Validating `resource` strictly against the live catalog.** Rejected: the catalog loads
async — a deep link would bounce before discovery lands. Lenient validation + graceful
fallback (placeholder panel) handles both the loading window and a genuinely gone kind.

## Consequences

The nav tracks the cluster live (kinds and per-kind counts stream via
`clusterCachedDataKindsWatch`) while the curated skeleton stays stable. The obligations: new
curated leaves need a `CURATED_LEAF_API_GROUP` entry for their count join; anything consuming
a resource id must treat it as an open string and handle "names no kind".
