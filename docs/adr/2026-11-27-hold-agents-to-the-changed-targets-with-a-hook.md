---
title: Hold agents to the -changed targets with a hook
date: 2026-11-27
scope: repo
status: Accepted
---

# Hold agents to the -changed targets with a hook

## Context

"Scope local test and vet runs to what a change reaches" made `vet-<area>-changed` and `test-<area>-changed` the local check before a push, and left the full targets to CI and to reproducing a CI failure. The guides say so, but agents still run the full desktop checks before a push. In 27 Claude Code sessions on one maintainer's machine since the `-changed` targets landed, `make vet-desktop` ran in 8 sessions and `make test-desktop` in 7. `vet-desktop-changed` and `test-desktop-changed` ran in 4 each.

The full desktop targets are the costly ones. In the #1637 benchmark, on GitHub-hosted runners:

- Warm, with nothing changed, `test-desktop` takes 140 s on `ubuntu-24.04` and 400 s on `macos-26`. vitest reruns the whole webview suite every time (96 s and 222 s), and on macOS the sidecar's `go test` spends 179 s confirming its cached results.
- After a leaf change, `test-desktop-changed` takes 4 s and 13 s.
- Cold, `test-desktop` takes 10–16 min and 18–26 min.

The Go areas' full targets cost about what their `-changed` ones do once the cache is warm (1–8 s against about 1 s), and they also run staticcheck. `test-e2e` builds the images and a kind cluster. `FULL=1` on a `cover-<area>-changed` target runs every test of the area.

## Decision

A `PreToolUse` hook on Bash in `.claude/settings.json` runs `scripts/check-make-target-hook.sh`. It refuses a command that runs `make vet-desktop`, `make test-desktop` or one of their `-sidecar`, `-host` and `-webview` parts, `make test-e2e`, or a `cover-<area>-changed` target with `FULL=1`. Its one line per target names the `-changed` target to run instead and the override: a command that sets `CHECKS=full` passes. The script parses the command as the shell would, so a target named in a `grep`, a quoted commit message, a heredoc or a `make -n` passes, and one run through `bash -c` does not.

For a human, each of those targets prints one line before it runs, naming its `-changed` counterpart (`note: CI runs test-desktop-sidecar; before a push, run make test-desktop-changed`). The line prints everywhere, CI included, since CI calls these targets and they do not branch on `CI`.

## Alternatives considered

- **Stronger wording in the guides alone.** The guides already name the `-changed` targets as the check before a push, and the counts above are what that produced.
- **Refuse the targets in the Makefile, outside CI.** It would hold every caller, agents outside Claude Code included, but CI calls these targets, and branching on `CI=true` makes a local run differ from CI's, which is what reproducing a failure depends on.
- **Gate the Go areas' full targets too.** Warm, they cost about what `-changed` does, and they run staticcheck, which no `-changed` target does.
- **Match the target names anywhere in the command.** Simpler, but it would refuse a `grep`, a commit message or an issue body that names one.

## Consequences

- An agent in Claude Code that reaches for a full desktop check, `test-e2e` or a `FULL=1` cover run is refused and told what to run, and reruns with `CHECKS=full` only to reproduce a CI failure.
- Agents that read `AGENTS.md` outside Claude Code get the guides' text and the Makefile's note alone.
- A command the parser misreads, such as one built from variables or `eval`, passes unchecked. The hook is advice with teeth, not a boundary.
- A new full target as slow as these adds a case to the script and to `scripts/check-make-target-hook.bats`, and a note to the Makefile.

## Revisit when

The full desktop checks get fast enough warm that running them costs about what `-changed` does (#1647, #1648), or agents start reaching for another slow target.
