---
title: Hold window-scoped view state in URL search params, not providers
date: 2026-08-09
scope: frontend
status: Accepted
---

# Hold window-scoped view state in URL search params, not providers

## Context

Two pieces of view state needed a home: the active kube-context (one window-wide choice shared
by chat and dashboard) and the dashboard's focused resource kind. The app is multi-window, has
router-driven history navigation (the context bar's back/forward `HistoryNav`), and wants
deep-linkable views — a secondary window opens at a route path and should reconstruct its
state from it.

## Decision

Both live in **search params** on their routes. `kubeContext` sits on the pathless `_app`
layout route (declared via `validateSearch`, kept across chat↔dashboard navigation with
`retainSearchParams`), so it is per-window and survives mode switches. `resource` sits on
`/dashboard`. Each selection is a real, history-pushing navigation — exactly the steps
`HistoryNav` walks. Accessors wrap the params: `useActiveKubeContext`
(`src/lib/active-kube-context.tsx`) resolves the param against the live context list, falling
back to the kubeconfig's current context when absent or naming a gone context;
`resolveDashboardResource` does the same for the dashboard. Links write params functionally
(`(prev) => ({ ...prev, resource })`) so sibling params survive a write.

Selection is a frontend view-scope only — it never rewrites the kubeconfig's current-context.

## Alternatives considered

**React context providers.** Rejected: provider state is per-render-tree, not per-URL — it
can't deep-link, doesn't participate in history (back/forward would skip selections), and
each new window would boot to a default instead of what its opener encoded in the URL.

**The host file / persisted settings.** Rejected for `kubeContext`: it is per-window state,
and `host.json` is app-global; two windows on different contexts is a supported arrangement.

**Path segments (`/dashboard/pods`).** Workable for `resource`, but `kubeContext` crosses two
peer routes, which search params + `retainSearchParams` express directly; keeping both in the
same mechanism is worth more than a prettier path.

## Consequences

State is inspectable, shareable, and window-scoped for free; new window-scoped state should
follow the same pattern. The costs: params must be validated leniently (a stale or
hand-edited URL must degrade, not crash), and every `Link` that writes one param must spread
the previous search or it silently drops the others.
