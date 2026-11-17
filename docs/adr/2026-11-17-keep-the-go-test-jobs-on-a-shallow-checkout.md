---
title: Keep the Go test jobs on a shallow checkout
date: 2026-11-17
scope: repo
status: Accepted
---

# Keep the Go test jobs on a shallow checkout

## Context

Go's test cache keys a test on the files it opened, by size and modification time. A checkout stamps every file with the current time, so a test that opens a file in the repo would never replay from the cache CI restores. kstack fixes this with `scripts/restore-mtime.go`: a full-history checkout (`fetch-depth: 0`), then each tracked file dated by the commit that last changed it. #1498 proposed the same for `ci.yml`'s `Go · Build and Test` and `coverage.yml`'s Go job.

The Go test cache rechecks only the files a test opened inside its module's root. Every file the tests of `modules/` open is under `t.TempDir()`, outside the root, or embedded, which the build hashes by content. Measured on 2026-11-17:

- On PR runs that restored a cache, `Go · Build and Test` replayed all 25 test packages, `(cached)`, from a shallow checkout (#1492's run 43166592462; run 42994529367). The one PR run that replayed nothing found no cache, after the Go 1.26.11 bump.
- Locally, after touching every tracked file and directory, all 25 packages still replayed.
- A full clone of the repo took 11 to 16 seconds through the sandbox's proxy, against 2 to 4 for `--depth 1`, before `go run scripts/restore-mtime.go` compiles and walks the history.

## Decision

The Go test jobs check out at the default depth of 1 and leave modification times as the checkout sets them. A Go test reads its fixtures from an embedded file or writes them to `t.TempDir()`, never opens a file in its module (`modules/CLAUDE.md`).

## Alternatives considered

**Restore modification times from the history, as kstack does.** It lets no test replay that does not replay today, and adds the full-history fetch and a script to every Go test leg, on all six platforms on `main`.

**Hash the content of the files a test opens.** The Go command offers no such option.

## Consequences

The Go test jobs keep their fastest checkout and no extra script.

A test that opens a file in its module, a `testdata/` file for instance, reruns on every CI run, and nothing fails to say so. The guide's rule is the guard.

## Revisit when

A test needs to read a file in the repo that it cannot embed, or Go's test cache starts keying on content.
