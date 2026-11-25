---
title: Record a bash stop as the runner's act, and kill at once on a cancel
date: 2026-10-17
scope: sidecar
status: Accepted
---

# Record a bash stop as the runner's act, and kill at once on a cancel

## Context

This decision predates this file and was recorded in `sidecar/CLAUDE.md`. A bash command ends
one of three ways: it exits, its own timeout fires, or the loop's context ends (the user's Cancel,
a deleted chat, shutdown). The loop keeps any result that is not an error, so a stopped command
read as a success would be kept as one. The host allows the sidecar 6s to exit, the drain spends
up to 3s before `stop` cancels the turns, and a Unix process group outlives the sidecar.

## Decision

A timeout sends SIGTERM to the group, then SIGKILL after `killGrace` (5s), or at once if the
loop's context ends during the grace. A context ending sends SIGKILL at once, with no grace.
Windows terminates the job with 143 on a timeout and 137 on a cancel.

The stop is recorded by the runner as it signals (`result.Stop`), under a lock (`guard`) the reap
closes, so a stop that finds bash reaped sends and records nothing and a late escalation never
signals a reused group id. A stopped command is always an error result.

## Alternatives considered

**Read the stop off the exit code or the context.** A trap that exits 0 was still stopped, and an
exit of 143 by itself was not; the context can end after the command already finished.

**Grant the grace on a cancel too.** 5s of grace inside the host's 6s, after up to 3s of drain,
leaves the group running when the host kills the sidecar.

## Consequences

`stopBash` asks the `os.Process` first, since `exec` can run the stop after the OS has collected
bash and before `Wait` returns. Group and job are killed again after bash exits, so a `sleep &`
is gone before the result is read; a descendant that left the Unix group is outside the
guarantee.
