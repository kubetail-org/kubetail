---
title: Check hook dependencies in the dashboard UI
date: 2026-11-09
scope: dashboard-ui
status: Accepted
---

# Check hook dependencies in the dashboard UI

## Context

[Lint the dashboard UI with oxlint's categories and a chosen rule list](2026-11-09-lint-the-dashboard-ui-with-oxlints-categories-and-a-chosen-rule-list.md) turned `react/exhaustive-deps` off. Turning it on reported 30 hooks. Some omissions were deliberate: an effect that runs once per mount, or a callback whose identity is meant to change. Others were stale closures. `useGetQueryWithSubscription` did not resubscribe when its name or namespace changed. `useEffectServerStatus` did not pass on a new message while the status stayed the same. `LogMetadataProvider` rebuilt its map on `loading` and not on the data it reads.

This ADR replaces that ADR's bullet on `react/exhaustive-deps`. The rest of it stands.

## Decision

**`react/exhaustive-deps` is on in `dashboard-ui/.oxlintrc.json`, as an error like every other rule.**

Each finding was settled in one of three ways:

- **Add the dependency** when it is stable, or when rerunning on it is correct. This covers refs, Jotai and `useState` setters, memoized services and module constants.
- **Restructure the hook** so that what it lists is stable:
  - `useListQueryWithSubscription` and `useCounterQueryWithSubscription` deep-compare their `variables`, because callers pass a new object on each render.
  - `useKubernetesAPIServerStatus` and `useClusterAPIServerStatus` memoize the `ServerStatus` they return.
  - Pull-to-refresh in the log viewer reads `loadMoreAfter` through `useEffectEvent`.
- **Disable the rule on the line**, with an `oxlint-disable-next-line react/exhaustive-deps -- <reason>` comment, where the omission is the point. There are seven:
  - The Kubernetes watch reads `resourceVersion` once, when it starts.
  - The log viewer initializes once per mount.
  - `getItemKey` and the two width-measuring ref callbacks change identity on purpose.
  - `useLogViewerState` takes its dependencies from the caller.

## Alternatives considered

- **Leave the rule off and keep asking for arrays written as though it were on.** That is what found the stale closures above: nothing checked the arrays, and some were wrong.
- **Run the autofix.** It adds every missing dependency. That would resubscribe the Kubernetes watch on every event, and rerun the server-status effect on every render of a page that reads the atom it writes.
- **Make every log viewer runtime object stable first**, so that `useLoadMore` and pull-to-refresh could list `loadMoreBefore` and `loadMoreAfter` directly. `useLoadMore` reruns on every render today because those callbacks are new each time. Making them stable would change when the viewer loads more lines, and that is a behaviour change to review on its own.

## Consequences

A missing or needless hook dependency fails `pnpm lint`. A deliberate omission has to say why on the line.

The disable comments cover the whole dependency array. They hide a new omission in an array that already has one, so a change to one of those seven arrays lists everything else by hand.

Enabling the React compiler will remove some hand-written memoization, and with it some of these arrays.

## Revisit when

The React compiler is on and `useMemo`, `useCallback` and `memo` go away, or the log viewer's runtime objects become stable.
