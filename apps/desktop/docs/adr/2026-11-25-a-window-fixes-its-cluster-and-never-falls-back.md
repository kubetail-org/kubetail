---
title: A window fixes its cluster, and Kubetail never falls back from an unreachable one
date: 2026-11-25
scope: cross-cutting
status: Accepted
---

# A window fixes its cluster, and Kubetail never falls back from an unreachable one

## Context

A window with no `kubeContext` param opened on the kubeconfig's `current-context`. When that
context was gone, the window resolved to no cluster and the omnibox drew an empty name. When it was
present but unreachable, the window sat on a cluster that never connected ([#467]).

The fix adds a default cluster the user picks in Settings, stored as `clusters.is_default`. Two
questions came with it: what a window does when its cluster is unreachable, and what an open window
does when the default changes.

## Decision

**Kubetail never moves a window to a cluster the user did not choose.** An unreachable cluster stays
selected, and its health verdict says what is wrong. With neither the default cluster nor the
kubeconfig's current context usable, the window opens on no cluster and says so.

**A window fixes its cluster in the param.** `useFixWindowContext`
(`src/lib/active-kube-context.tsx`, called by `AppLayout`) writes the resolved context into the
`kubeContext` param, replacing the history entry. Changing the default, or the kubeconfig's
current context, moves no open window; only new windows follow it. A window that resolves nothing
writes nothing, so it follows a fix as it happens. A param naming a gone context yields to the
default and is rewritten to it.

**The default is a column, not a `settings` row.** `clusters.is_default`, with a partial unique
index allowing one marked row, rides `clusterCollectionWatch` with the cluster's other toggles and
goes with its row. Marking a row for deletion or disabling it clears the mark.

## Alternatives considered

- **Fall back to another cluster after a timeout.** Kubetail acts on clusters, so moving a window to
  one the user did not pick risks an action on the wrong cluster. It also needs a timeout that is
  wrong for a slow network or for a cluster coming up.
- **Let open windows follow the default.** A default changed in Settings would move every open
  window, the one holding Settings included, and carry the old cluster's namespace scope and open
  chat onto the new one. A `kubectl config use-context` did the same.
- **A `settings` row naming the cluster's id.** It needs a watch of its own and a check for an id
  whose cluster is gone; the column gets both from the row.

## Consequences

- Each window's URL names its cluster once one resolves, so a reload or a deep link keeps it.
- A window opened before the kubeconfig was fixed moves once, when its first cluster resolves.
- A cluster that leaves the kubeconfig still moves the windows on it, to the default, as it did
  before.

[#467]: https://github.com/kstackhq/kstack/issues/467
