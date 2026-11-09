---
title: Keep the sandbox's build outputs off the host mount
date: 2026-11-09
scope: repo
status: Accepted
---

# Keep the sandbox's build outputs off the host mount

## Context

Agents develop Kubetail inside a Linux sandbox on a macOS host. The sandbox bind-mounts the repo from the host, so every build output path is shared. A `pnpm install` in the sandbox replaced the host's macOS native binaries in `dashboard-ui/node_modules`, and a `cargo build` wrote Linux artifacts into the host's `crates/target`, and the reverse.

The sandbox has its own disk, an overlay of about 20G that a sandbox recreate wipes. The host mount is slow to read and, under Cargo's dense writes, its directory metadata is not coherent. Every worktree runs in the same sandbox and shares one persistent env file, `/etc/sandbox-persistent.sh`.

kstack, which develops in the same kind of sandbox, solved this with `scripts/sandbox-dev-setup.sh` run from a `SessionStart` hook.

## Decision

`scripts/sandbox-dev-setup.sh`, adapted from kstack's, runs from a `SessionStart` hook in `.claude/settings.json`. It no-ops unless `$SANDBOX_VM_ID` is set and the OS is Linux.

- Configurable paths move through env vars in `/etc/sandbox-persistent.sh`: `CARGO_TARGET_DIR` and `PNPM_CONFIG_STORE_DIR`, both under `/home/agent/kubetail-build` on the overlay and shared by every worktree.
- Paths the tooling cannot relocate get a sandbox-only bind mount: `dashboard-ui/node_modules` from a per-worktree directory on the overlay, and `dashboard-ui/dist` from `.sandbox-linux/dist` on the host disk.
- `modules/dashboard/website/` is not mounted. Its contents are the same on both platforms, and `make build-dashboard-ui` removes and recreates it, which a mount point refuses.
- Go is pinned to CI's version by `GOTOOLCHAIN=go<version>+auto`, read from `.github/actions/setup-environment/action.yml`.
- A `uv` that does not run is replaced by the PyPI build in `~/.local/bin`. The image's package (0.13.7) is built with jemalloc for 4 KiB pages and aborts on the sandbox's 16 KiB pages; the PyPI build of the same version runs.

## Alternatives considered

**pnpm's global virtual store**, which kstack uses so each worktree's `node_modules` holds only symlinks into the shared store. Rejected: with it, `tsc` resolves two copies of `@types/react`, and `pnpm build` and `pnpm test` fail. Without it pnpm copies from the store, since `link(2)` fails across the bind mount, so each worktree pays its own `node_modules` on the overlay.

**One `node_modules` on the overlay for every worktree.** Rejected: worktrees can be on different lockfiles, and an install in one would rewrite another's dependencies.

**Leaving `node_modules` and `target/` on the host mount under `.sandbox-linux/`.** Rejected: Cargo builds there do not converge, and test runs read `node_modules` slowly.

**Installing `uv` from its standalone installer.** Rejected: the sandbox already has `pip`, and `~/.local/bin` is already first on `PATH`.

## Consequences

The host's build outputs stay untouched, and the sandbox's builds run at overlay speed. The overlay fills: each worktree's `node_modules` is a full copy, and the script warns under 2G free.

Bind mounts do not survive a sandbox restart, so the hook reruns the script each session. The script owns its lines in `/etc/sandbox-persistent.sh` and rewrites them on every run; a variable it stops writing must stay in its delete list until no sandbox carries it.

CI's `astral-sh/setup-uv` takes the latest `uv`, so the script's pin drifts from CI and needs bumping by hand.

## Revisit when

The image's `uv` package runs on 16 KiB pages: the `uv` step becomes a no-op and can go. pnpm's global virtual store resolves types correctly: it would save the overlay a copy of `node_modules` per worktree.
