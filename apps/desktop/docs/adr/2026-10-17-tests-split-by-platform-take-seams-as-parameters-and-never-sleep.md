---
title: Split tests by platform file, take seams as parameters, and never sleep
date: 2026-10-17
scope: cross-cutting
status: Accepted
---

# Split tests by platform file, take seams as parameters, and never sleep

## Context

These testing rules apply to the Go sidecar, the Rust host and the React webview alike. They
predate this file and were recorded, with their reasons, in the root `CLAUDE.md`. The suites run
in parallel, under load in CI, and on macOS, Linux and Windows.

## Decision

- **A platform-specific Go test lives in a platform-named file** (`foo_unix_test.go` with
  `//go:build !windows`), never behind a `runtime.GOOS` check. A test that is partly
  platform-specific is split, its cross-platform half staying in the plain file.
- **A test seam is a parameter or a field**, never a package variable a test swaps. A production
  constant a test would have to outwait (a retry cadence, a poll interval) becomes an argument,
  and production passes the constant (`appdb`'s `runJanitor`).
- **No test blocks on the wall clock to let something happen.** Go waits on a channel
  (`testutil.Probe`, `testutil.Recv`, `require.Eventually`); TypeScript uses fake timers or
  `waitFor`; Rust uses `#[tokio::test(start_paused = true)]`. Two bounded waits are allowed and
  must say so: a negative assertion, written as a `select` on the tripwire against `time.After`
  sized as a multiple of the shrunk cadence; and latency injected into a fake on purpose.

## Alternatives considered

- **`runtime.GOOS` skips.** They compile the test everywhere and decline to run it, so the file's
  name stops saying who the test is for, and the assertion silently disappears on the platform it
  was written for.
- **Package variables swapped by tests.** A swapped variable reaches every test running beside
  it, so the whole package has to stay serial.
- **Real sleeps.** They are flaky under load, slow in aggregate, and fail at the end of the wait
  rather than the moment the invariant breaks. A test that encodes a production constant also
  breaks whenever the constant is tuned.

## Consequences

Packages such as `agent/chat`, `kubestore` and `kubeconn` call `t.Parallel()` in every test; a
test that must touch process state (`t.Setenv`, `syscall.Umask`) stays serial and says why.
Timing-dependent units grow a parameter whose only non-production caller is a test. A negative
assertion still has a window, so its size has to track the cadence it waits out.
