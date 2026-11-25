---
title: The coverage gate diffs the commits itself, and reads only Codecov's stored reports
date: 2026-11-05
scope: repo
status: Accepted
---

# The coverage gate diffs the commits itself, and reads only Codecov's stored reports

## Context

[The coverage gate runs in CI](2026-10-30-the-coverage-gate-runs-in-ci-on-codecovs-merged-result.md)
read each changed line's coverage from Codecov's `/compare/` endpoint. On [#735](https://github.com/kstackhq/kstack/issues/735) every upload was
processed and both commits had reports, yet every `/compare/?pullid=735` answered 403 with
`{"detail": "Github API rate limit error: Forbidden"}` ([#737](https://github.com/kstackhq/kstack/issues/737)). `/compare/` is the one endpoint the
gate called that needs GitHub: Codecov fetches the diff with a token of its own, shared by every
anonymous API caller, so the limit was not kubetail's. Authenticating takes a Codecov token, which a
fork's PR cannot read. The gate retried the 403 for 15 minutes, printing nothing, and then blamed a
Codecov delay.

The endpoints that serve Codecov's stored data answered throughout: `/report/?sha=` lists each
file's `[line, coverage]` pairs, and `/commits/<sha>/` carries each commit's `totals`.

## Decision

`tools/coveragegate` builds the comparison itself and makes no `/compare/` request:

- The changed lines are the lines `git diff <base>...<head>` adds. A push run's base is the head's
  parent, as Codecov records it. The gate job checks out the whole history.
- The head's line coverage comes from `/report/?sha=<head>`; the head's and the base's totals from
  `/commits/<sha>/`. A base `/commits/` does not know fails at once, as before.
- An error keeps Codecov's `detail`. A 403 whose detail names a rate limit fails at once, asking
  for a re-run; other errors are retried.
- Each poll prints its reason for waiting when the reason changes.

`tools/coverdiff` and the gate share one diff parser, `tools/internal/unidiff`.

This replaces the earlier ADR's step 2, the comparison fetched from `/compare/`, and its waiting
for the comparison to be of this commit.

## Alternatives considered

- **Authenticate to Codecov.** Its token is a repository secret, so a fork's PR would still hit the
  shared limit.
- **Keep `/compare/` and fall back to the diff on a 403.** It keeps two paths to one verdict, and
  the fallback is the one that always works.
- **Read the diff from GitHub's API.** It needs a token too, and the checkout already has the
  commits.

## Consequences

- The gate depends on no Codecov call that reaches GitHub, so GitHub's limits on Codecov no longer
  stall it.
- The gate job fetches the whole history, as the Go coverage jobs do.
- The patch is `git diff`'s, so a renamed file's unchanged lines are not changed lines. A path
  Codecov's report names differently from git is unmeasured, and is skipped as any unmeasured line
  is.
- A three-dot diff takes the merge base, so a PR whose base moved on is judged on its own changes.
