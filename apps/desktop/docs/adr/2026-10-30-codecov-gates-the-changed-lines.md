---
title: Codecov gates the changed lines, and the total is a backstop
date: 2026-10-30
scope: repo
status: Accepted
amended_by: [The coverage gate runs in CI, on Codecov's merged result](2026-10-30-the-coverage-gate-runs-in-ci-on-codecovs-merged-result.md)
---

# Codecov gates the changed lines, and the total is a backstop

## Context

Every coverage check was code we wrote: `sidecar/scripts/coverage.go` ran the Go suite twice,
unioned three OS profiles and gated the union against two threshold files; `make cover-js` gated
vitest against a third. Each threshold was a ratchet a person raised by hand. Each sat at the top
of the run-to-run spread, so a pull request that changed nothing coverage depends on could fail.

The fix under way on `wip/coverage-gates` (#494, #495, #497) measured that spread and was writing
`tools/coverchanged`, about 1,400 lines that intersect the diff with the profiles, with ignore
markers and threshold guards still to come. #97 had kept a hosted service out of scope, to avoid a
third party that sees our code.

Twenty uncached runs on CI (#495) measured the spread: the Go union moved between 97.271% and
97.329% of 22,576 statements (13 statements, 0.058 points); TypeScript did not move.

## Decision

Codecov holds the gate. CI uploads the reports it already produces (`ci.yml`): each OS's Go
profile under `go-<os>`, vitest's `lcov.info` under `typescript`, and `cargo llvm-cov`'s lcov per
OS under `rust-<os>`. Codecov merges every upload of a commit, so a block covered on any OS is
covered, and a `_windows.go` line is judged by the Windows upload alone. `.github/codecov.yml` sets two
statuses:

- **`codecov/patch` is the gate**: every changed line a report measured is covered (`target:
  100%`). A changed line no report measured is not counted. Codecov annotates the uncovered lines
  in the pull request's diff.
- **`codecov/project` is the backstop**: the total may not fall more than 0.1% below the base
  commit (`target: auto`). It is the only check that sees a pull request delete or weaken a test.
  0.1% is about seven times the measured spread, so a pull request changing nothing it measures
  does not fail it.

Neither is a required status check on `main`: like CI's jobs, a red status is read by the reviewer
before merging. Carryforward keeps a language's last report when the
path filter skips its jobs. A line a test cannot reach is exempted where it stands, with a reason:
`/* v8 ignore next -- <reason> */` in TypeScript, `// coverage:ignore <reason>` in Go, and in Rust
`// coverage:ignore-start <reason>` to `// coverage:ignore-stop`, which `tools/coverignore` drops
from `cargo llvm-cov`'s report before the upload.

The uploads authenticate with an OIDC token (`use_oidc: true`), so no Codecov token is stored. A
fork's pull request cannot mint one; the action then uploads tokenless, which Codecov accepts for a
public repository.

## Alternatives considered

- **Finish `tools/coverchanged`.** It reproduces what Codecov does — diff intersection, OS
  merge, per-line listing, history for the base — and leaves us maintaining it, plus a place to
  store `main`'s reports for the base comparison.
- **Keep the total as a ratcheted threshold, with a tolerance.** It still fails pull requests for
  lines they did not touch, and it says nothing about the lines they did.
- **Make `codecov/project` informational.** It is the only check that catches a deleted test. If it
  proves noisy, `informational: true` turns it into a report without removing it.

#97's objection to a third party does not hold for what leaves: the reports carry file paths and
line hit counts, never source, and the repository is public.

## Consequences

- No threshold file, nothing ratcheted by hand. The loop for an author is: open the pull request,
  read `codecov/patch`'s annotations, test or mark each listed line, push until green.
- Nothing blocks a merge mechanically. A reviewer who misses a red `codecov/patch` lets an
  untested line in; the next `project` status shows it only if the total drops past the tolerance.
- Codecov down means the statuses stay pending; a reviewer merges without them or waits.
- A gap in the path filters passes silently: carryforward judges the skipped language against its
  last report. Every push to `main` runs every job, so a gap shows as a `project` drop on the next
  pull request.
- A status seen mid-run can be red: each upload revises it. Read it after CI finishes.
- The Go `coverage:ignore` filter is the one piece of our own tooling that stays.

## Revisit when

The repository goes private (Codecov is then paid), or untested lines merge past a red status
often enough that the statuses should be required checks.
