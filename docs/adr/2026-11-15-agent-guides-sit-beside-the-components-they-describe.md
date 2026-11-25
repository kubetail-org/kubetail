---
title: Agent guides sit beside the components they describe
date: 2026-11-15
scope: repo
status: Accepted
---

# Agent guides sit beside the components they describe

## Context

`apps/desktop/src/CLAUDE.md` had grown to 89 KB. Claude Code loads a `CLAUDE.md` when a file in its directory or below is read, so any session that opened a webview file loaded all of it: the chat, the dashboard, the log viewer, the omnibox and the settings dialog. A change to a table cell paid for the approval request's traps. The sidecar had already split its guide by package, one `CLAUDE.md` per larger package, but the webview had no such seams in its docs.

The webview's code splits two ways. `apps/desktop/src/components/` has one folder per area (`chat/`, `dashboard/`, `logs/`, `cluster/`, `settings/`, `shell/`, `primitives/`). `apps/desktop/src/lib/` is flat: each subsystem's hooks and providers sit side by side with every other subsystem's.

## Decision

`apps/desktop/src/CLAUDE.md` holds what crosses the webview: the security invariants, routing and component areas, errors, window state, theming and storage, GraphQL, codegen, tests, and lint and build. Each larger subsystem has its own guide in its components folder (`apps/desktop/src/components/<area>/CLAUDE.md`), with an `AGENTS.md` beside it, and a byte budget in `.github/ci-config/claude-md-budgets.yml`.

Each guide also owns its subsystem's `apps/desktop/src/lib/` modules and lists them in its first paragraph. A nested guide loads only for files in its own folder, so `apps/desktop/src/CLAUDE.md`'s *Guides* table tells an agent to open the subsystem's guide before changing those modules. A guide about code in another area's folder (the cluster guide covers `shell/context-omnibox/`) says so in the guide that does load, which points to it.

The security invariants stay in `apps/desktop/src/CLAUDE.md`, whatever subsystem they name, so they load for every webview file.

The host follows the same rule. `apps/desktop/src-tauri/CLAUDE.md` keeps what crosses the host, and a part with a folder of its own and a body of rules no other part needs gets a guide there: `apps/desktop/src-tauri/linux/CLAUDE.md` for what the Linux packages carry and `apps/desktop/src-tauri/src/updater/CLAUDE.md` for the updater. A part that is one file (`logging.rs`, `log_windows.rs`) stays in the area guide, since a guide loads only by folder.

## Alternatives considered

- **Keep one `apps/desktop/src/CLAUDE.md`.** Every webview session pays for every subsystem, and the file keeps growing with each feature. Its budget was already the largest in the repo.
- **Put the guides beside `apps/desktop/src/lib/` modules.** `apps/desktop/src/lib/` is flat, so a guide there would load for every module. Giving each subsystem a `apps/desktop/src/lib/` folder would move most of the webview's imports for the sake of the docs.
- **Keep the subsystem guides under `docs/` and link them from `apps/desktop/src/CLAUDE.md`.** Nothing would load them automatically. An agent would open one only if it noticed the link, which is the failure the split is meant to remove.

## Consequences

A session that touches one webview subsystem loads `apps/desktop/src/CLAUDE.md` (about 27 KB) and that subsystem's guide, not 89 KB. A host session no longer loads the Linux packaging and updater rules (about 7 KB) unless it reads a file in their folders.

An agent changing a `apps/desktop/src/lib/` module, or a script a host guide owns, must open that guide itself. That is the cost of a flat `apps/desktop/src/lib/`, and the *Guides* table and each guide's module list are what pay it. A new module joins the list of the guide that owns it, in the same change.

A rule that holds across subsystems belongs in `apps/desktop/src/CLAUDE.md`. Copying it into two guides lets the copies drift apart.

## Revisit when

`apps/desktop/src/lib/` is split into a folder per subsystem, so each guide can sit beside its modules.
