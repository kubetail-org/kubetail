---
title: Tests sit beside their files, never sleep, and take seams as parameters
date: 2026-11-09
scope: repo
status: Accepted
---

# Tests sit beside their files, never sleep, and take seams as parameters

## Context

The repo has Go, Rust and TypeScript suites. The Go suites run in CI on Linux, macOS and Windows, on amd64 and arm64: Windows since #595 and macOS since #596 (both August 2025). Several units under test are asynchronous by nature: file watchers, informers, WebSocket subscriptions, graceful shutdown.

Timing has caused flaky tests here more than once. #338 (May 2025) patched a flaky `KubeConfigWatcher` test. #639 (September 2025) replaced sleeps in the Rust agent's watcher tests with barriers and waits on a precondition, and let most of them run in parallel, to make them stable and faster. Portability needed fixing too: #584 (August 2025) removed path-separator and error-string assumptions so the Go tests could run on Windows.

The rules were written down in two steps. #1047 (April 2026) added "never use `time.Sleep` or `setTimeout` in tests to wait for asynchronous state" to the root `CLAUDE.md`, in the same PR as new drain and shutdown tests. #1143 (May 2026) then replaced the sleeps left in the `SyncGroup` tests, citing that rule. #1216 (November 2026) adopted the rest from kstack's testing conventions (kstack ADR `2026-10-17-tests-split-by-platform-take-seams-as-parameters-and-never-sleep`): tests beside the file they cover, seams as parameters, timing paced by parameter, and platform-named test files. #1216 gives no reasons of its own. kstack's ADR gives the reasons for the platform, seam and sleep rules; the reason for keeping each test beside its file is not recorded in either repo.

## Decision

The root `CLAUDE.md`, *Testing conventions*, holds the rules. Every test file sits beside the file it covers. No test waits on the wall clock for asynchronous state: Go waits on a channel or uses `require.Eventually`, TypeScript uses fake timers or `waitFor`, and Rust awaits the unit's own signal under `tokio::time::timeout`. Two bounded waits are allowed and say so: a negative assertion, and latency injected into a fake on purpose. A timing-dependent unit takes its pace as a parameter. A test seam is a parameter or a field, never a swapped package variable or a global mode. A platform-specific Go test goes in a platform-named file with a build constraint, never behind a `runtime.GOOS` check.

## Alternatives considered

**Real sleeps.** Used before #639 and #1143. #1143 states the case against them: the delays are arbitrary and make tests flaky on slow machines. A sleep also fails at the end of the wait rather than when the invariant breaks.

**Package variables and global modes as seams.** The existing practice in places (see Consequences). Following kstack's reasoning, a swapped variable reaches every test running beside it, so the package cannot run in parallel; and a global mode makes every test under it skip the same code.

**`runtime.GOOS` skips.** Used by #584 to skip symlink tests on Windows, in Go Cluster Agent code since removed. Following kstack's reasoning, a skip compiles the test everywhere and quietly drops it on the platform it was written for, and the file's name no longer says who the test is for.

## Consequences

Tests stay deterministic under load on every CI runner, and fail at the moment an invariant breaks. A timing-dependent unit grows a parameter whose only non-production caller is a test. A negative assertion still has a window, so its size has to track the cadence it waits out.

Older code breaks the rules in four places, each recorded in its area guide:

- `inClusterConfigFn` in `modules/shared/k8shelpers/connection-manager.go` is a package variable that `connection-manager_test.go` swaps (`modules/CLAUDE.md`).
- `NewApp` in `dashboard` and `cluster-api` skips its real dependencies when `gin.Mode()` is `gin.TestMode`, which each package's `testutils_test.go` sets in `init()`. In `cluster-api` that skips the authentication middleware too (`modules/CLAUDE.md`, `modules/cluster-api/CLAUDE.md`).
- `headFlag` and `tailFlag` in `modules/cli/cmd` are package variables shared by every command built with `addLogsCmdFlags`; a value set in one subtest leaks into the next (`modules/cli/CLAUDE.md`).
- The Cluster Agent's `LogMetadataWatcher` debounces with a fixed `Duration::from_secs(2)` rather than a parameter, so its tests take seconds each, and its rename and modify tests are `#[cfg(not(target_os = "macos"))]` (`crates/CLAUDE.md`).

Each is a known cost, not a pattern to copy. New code follows the rules; fixing an old seam is a change of its own.
