---
title: Share one pnpm store across the sandbox's worktrees, in the global virtual store layout
date: 2026-10-28
scope: repo
status: Accepted
---

# Share one pnpm store across the sandbox's worktrees, in the global virtual store layout

Supersedes Put the sandbox's node_modules on the overlay,
which kept the pnpm store on the host share and gave each worktree a full copy of its dependencies.

## Context

Development happens inside a Linux sandbox that bind-mounts this repo from a macOS host.
`scripts/sandbox-dev-setup.sh` puts each worktree's `node_modules` on the sandbox's overlay as a
bind mount, because reads from the host share are slow. The overlay is capped at 20G and also holds
Cargo's `target/` and Go's build cache.

`link(2)` returns `EXDEV` across two mounts, even on one filesystem, so pnpm could not hard-link
from its store into a bind-mounted `node_modules`. It copied every file instead: about 420M per
worktree. Nine worktrees took 2.6G, and the overlay was down to 1.3G free.

pnpm 12's `enableGlobalVirtualStore` puts the package files in one folder inside the store and fills
each project's `node_modules` with symlinks into it. The hard links then stay inside the store, on
one mount, and symlinks cross the bind mount.

## Decision

In the sandbox, the setup script exports `PNPM_CONFIG_STORE_DIR=/home/agent/kubetail-build/pnpm-store`
and `PNPM_CONFIG_ENABLE_GLOBAL_VIRTUAL_STORE=true` to the persistent env file. The store sits on
the overlay beside `target/`. Each worktree's `node_modules` stays a bind mount from
`/home/agent/kubetail-build/worktrees<repo path>/node_modules`, now holding only symlinks.

Both settings are environment variables, never `pnpm-workspace.yaml`, so the host and CI keep
pnpm's default layout.

The layout is stricter: a package resolves only the dependencies it declares. `@kubetail/ui`
imported React's types without declaring `@types/react`, and `tsc -b` failed on it. Version 3.0.2
declares it as an optional peer.

## Alternatives considered

**The store on the overlay, without the global virtual store.** Rejected by the superseded ADR,
and still wrong: pnpm copies across the bind mount wherever the store sits.

**A `packageExtensions` entry declaring `@types/react` for `@kubetail/ui`.** Not needed once
kubetail-org/kubetail-ui#127 shipped in 3.0.2.

**Keeping the copies and freeing the overlay by hand.** Rejected: the cost grows with every
worktree, and the overlay is already the routine constraint.

## Consequences

A worktree's `node_modules` takes about 4M of the overlay, and the store about 424M, once. A second
worktree installs in under a second.

The sandbox's install no longer matches the host's and CI's. An error seen only in the sandbox is
most likely another undeclared dependency. To rule the layout out, unset
`PNPM_CONFIG_ENABLE_GLOBAL_VIRTUAL_STORE`, empty `node_modules` and reinstall.

Files under `node_modules` are hard links into the shared store. Editing one, even to debug,
changes it for every worktree.

The env file is sandbox-wide, so the switch reaches every worktree at once. pnpm checks
dependencies before `pnpm run` and `pnpm exec`, so each worktree reinstalls in the new layout on
its next command. A branch still on `@kubetail/ui` 3.0.0 then fails `tsc -b` until it rebases.

The store is wiped when the sandbox is recreated. Downloading it again takes a few seconds.

## Revisit when

pnpm drops or changes `enableGlobalVirtualStore`, or the sandbox stops bind-mounting
`node_modules`.
