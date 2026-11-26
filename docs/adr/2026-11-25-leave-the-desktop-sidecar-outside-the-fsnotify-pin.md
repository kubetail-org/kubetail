---
title: Leave the desktop sidecar outside the fsnotify pin
date: 2026-11-25
scope: repo
status: Accepted
---

# Leave the desktop sidecar outside the fsnotify pin

## Context

[Pin fsnotify to v1.8.0](2026-11-09-pin-fsnotify-to-v1-8-0.md) holds every Go module back from fsnotify v1.9.0, whose kqueue backend sends no `Create` event when a watched symlink is replaced in place (fsnotify#689). It names the four workspace modules and says the pins move together.

In November 2026 the Kubetail Desktop sidecar arrived at `apps/desktop/sidecar`, a fifth module. It requires fsnotify v1.10.1 and uses it in `internal/services/kubeconfig` to watch the user's kubeconfig files, on macOS among other platforms. It imports no sibling module, has a `go.work` of its own and stays outside the root `go.work`.

## Decision

The sidecar takes no fsnotify `replace` and follows fsnotify's releases through Dependabot. The pin, and the rule that its `replace` lines move together, covers the four workspace modules alone: `libs/go-common`, `dashboard`, `cluster/api` and `cli`. This replaces the part of the earlier ADR that reads as covering every Go module.

## Alternatives considered

**Pin the sidecar to v1.8.0 too.** It would hold back a module that shares no code and no build with the others, for a regression its tests have not shown. The sidecar's watcher also polls on a tick and re-resolves symlinks before each reload, so a missed event delays a reload rather than losing it. Its symlink tests (`TestRepointedSymlinkFollowsToTheNewTarget` and its neighbours) run on macOS on `main`.

**Add the sidecar to the root `go.work`.** A workspace resolves one version of each dependency for all its modules, so it would force the pin on the sidecar, and tie its builds to the other modules' dependency graph.

## Consequences

Two fsnotify versions ship from this repo: v1.8.0 in the CLI, the Dashboard and the Cluster API, v1.10.1 or later in the desktop app. A fix or a regression in fsnotify reaches them separately.

Whether `KubeConfigWatcher` in `libs/go-common` would behave the same under the sidecar's version is still the earlier ADR's open question. If the sidecar shows the regression, it takes the pin with its own `replace`.

## Revisit when

The earlier ADR's pin is lifted, or the sidecar's kubeconfig watcher misses a symlink swap on macOS.
