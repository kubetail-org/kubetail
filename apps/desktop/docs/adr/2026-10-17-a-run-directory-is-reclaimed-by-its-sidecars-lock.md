---
title: Reclaim a gone sidecar's run directories by its lock, not its pid
date: 2026-10-17
scope: sidecar
status: Accepted
---

# Reclaim a gone sidecar's run directories by its lock, not its pid

## Context

This decision predates this file and was recorded in `sidecar/CLAUDE.md`. Each sandboxed bash
run makes a directory under `<runtime>/runs/<pid>-*` (its kubeconfig and proxy socket) and a
`TMPDIR` under `<cache>/tmp/<pid>-*`. A sidecar that crashes leaves them behind, and a crashed
run's `TMPDIR` can hold a whole build. The next sidecar has to tell a dead sidecar's entries from
a live one's: two sidecars can run side by side (dev and release, or a restart overlapping).

## Decision

A sidecar is gone when it no longer holds its lock (`internal/tools/bash/rundir_unix.go`).
`newRunDir` takes `<pid>.lock` in each of `runs/` and `tmp/` with `flock` before making anything
there (`holdRunLock`, once per process, held until exit). `New`'s sweep, on a goroutine of its
own, takes each `<pid>`'s lock it can, removes that pid's directories while holding it, then the
lock file. A sidecar that waited on a sweep's hold takes the lock again when the sweep removed the
file.

## Alternatives considered

**Ask whether the pid is alive.** A pid passes to later processes, across a reboot too, so a
reused pid would keep a gone sidecar's directories forever, or a live unrelated process would be
mistaken for a sidecar. A lock dies with the process that holds it.

## Consequences

The sweep runs off the startup path, since `New` runs before `READY`. Every directory a run makes
must be made after the lock is held, or a sweep can take it from under the run.
