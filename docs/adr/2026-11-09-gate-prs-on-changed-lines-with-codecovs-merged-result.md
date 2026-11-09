---
title: Gate PRs on changed lines with Codecov's merged result
date: 2026-11-09
scope: repo
status: Accepted
---

# Gate PRs on changed lines with Codecov's merged result

## Context

CI measured no coverage (#1211), so a PR could add untested code without anyone noticing. Kubetail has three suites in three languages, and the Go tests run on Linux, macOS and Windows, where files split by OS. A line is covered if any OS's run reaches it, so the gate has to judge a merge of every run, and compare it with the base commit's.

This design is ported from kstack, a sibling project that solved the same problem first. Its record: Codecov's own `codecov/patch` status posted before CI finished, since Codecov waits on commit statuses and GitHub Actions writes check runs; `codecov/project` never posted; and Codecov's `/compare/` endpoint fails under a GitHub rate limit shared by every anonymous caller. The endpoints that serve Codecov's stored data (`/report/`, `/commits/`) stayed reliable.

## Decision

Codecov stores and merges; CI judges. `.github/workflows/coverage.yml` uploads each OS's Go profile (`go-linux`, `go-darwin`, `go-windows`), vitest's lcov (`typescript`) and `cargo llvm-cov`'s lcov (`rust`) with an OIDC token, so no Codecov secret is stored. `.github/codecov.yml` turns off Codecov's statuses, checks and comments, and carries each flag forward when the path filter skips its job.

The `Coverage · Gate` job runs `tools/coveragegate` once every coverage job has finished. It waits for Codecov to process this run's uploads, takes the changed lines from `git diff <base>...<head>`, reads the head's line coverage and both commits' totals from Codecov, and fails when:

- **patch**: a changed line a report measured is not covered, or only partly;
- **project**: the total falls more than 0.1 points below the base, the one check that sees a deleted or weakened test.

Each uncovered line is an `::error` annotation and is listed in the log and the step summary. A line no test can reach is exempted where it stands, with a reason; `tools/coverignore` drops the Go and Rust markers' lines before the upload, and v8 reads the TypeScript markers itself. `make cover-changed` runs the same measurement and diff locally through `tools/coverdiff`.

## Alternatives considered

- **Codecov's `codecov/patch` and `codecov/project` statuses.** kstack used them first: patch posted on a partial merge before CI finished, and project never posted.
- **Compute the gate in CI without Codecov.** Merging three OSes' profiles is easy; keeping the base commit's reports somewhere to compare with is not, and Codecov already stores them.
- **A ratcheted total threshold.** It fails PRs for lines they did not touch, says nothing about the lines they did, and needs a person to raise it.
- **Codecov's `/compare/` endpoint for the changed lines.** It calls GitHub with a token Codecov shares among anonymous callers and fails under its rate limit; the checkout already holds both commits.

## Consequences

- A PR's verdict covers every language it touched, judged on every OS's run, posted after the last upload.
- The gate needs the base commit's report. Every push to `main` measures every language for that reason; a PR opened before its base's run has uploaded fails at once and asks for a re-run.
- Codecov down or slow fails the job after 15 minutes with a message naming the delay, not a coverage failure.
- A gap in the path filters would pass a changed line as unmeasured, so `ci.yml` and `coverage.yml` read one filter file.
- A language whose coverage job failed is not waited for; that job's own failure is what the PR shows.
- The gate parses Codecov's API. A change to its response shape fails the job rather than passing it.
- The reports carry file paths and hit counts, never source, and the repository is public.

## Revisit when

The repository goes private, which makes Codecov paid, or Codecov's statuses start waiting for GitHub check runs.
