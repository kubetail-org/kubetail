---
title: The coverage gate runs in CI, on Codecov's merged result
date: 2026-10-30
scope: repo
status: Accepted
amended_by:
  - [A wip branch's tip gets a coverage report](2026-11-04-a-wip-branchs-tip-gets-a-coverage-report.md)
  - [The coverage gate diffs the commits itself](2026-11-05-the-coverage-gate-diffs-the-commits-itself.md)
---

# The coverage gate runs in CI, on Codecov's merged result

## Context

[Codecov gates the changed lines](2026-10-30-codecov-gates-the-changed-lines.md) made Codecov's
`codecov/patch` and `codecov/project` statuses the gate. Neither behaved (#571):

- **`codecov/project` never posted** (#569), so nothing caught a deleted or weakened test.
- **`codecov/patch` posted before CI finished.** Codecov judges whether CI passed from the commit's
  statuses, but GitHub Actions reports check runs, not statuses. Our commits carry none, Codecov
  reads the empty set as success, and `require_ci_to_pass` holds nothing back. Each upload revised
  the status, so a status read mid-run could be green on a partial merge. (`fetch_and_update_whether_ci_passed`
  in Codecov's `apps/worker/tasks/notify.py`.)

Codecov's merging and storage work. Its public API serves the merged result without a token: a
commit's uploads and state, and a comparison with each changed line's coverage.

## Decision

The gate is CI's `Coverage · Gate` job. It needs every coverage job, runs unless the run is
cancelled, and runs `tools/coveragegate`, which:

1. polls Codecov until every upload this run made is processed, the commit is `complete`, and the
   comparison is of this commit, for at most 15 minutes;
2. fetches the comparison (`?pullid=` on a pull request, the commit against its parent on a push)
   and applies the two rules the earlier ADR set:
   - **patch**: every changed line a report measured is covered; a miss or a partial fails;
   - **project**: the total is no more than 0.1 points below the base;
3. prints each uncovered changed line as an `::error` annotation, and lists them all in the log and
   the step summary;
4. says so when Codecov did not finish in time, so a delay is not read as a drop.

Codecov posts no status and no check (`.github/codecov.yml`). Uploads, merging across OSes,
carryforward and markers stay as the earlier ADR decided. The job, like Codecov's statuses before
it, is not a required check: the reviewer reads it.

This replaces the earlier ADR's two statuses and its consequences about reading them; the rest of
it stands.

## Alternatives considered

- **Make Codecov wait for CI.** It reads commit statuses, which Actions does not write. A job
  posting a status to hold Codecov back needs `statuses: write`, which a fork's pull request lacks.
- **Compute the gate without Codecov.** The earlier ADR rejected maintaining the OS merge and the
  base history ourselves; nothing here changes that.

## Consequences

- One verdict per commit, posted after every upload, on pull requests (forks included) and on
  pushes to `main`.
- The script parses Codecov's API. A change to its response shape fails the job, not silently
  passes it: a comparison of another commit is waited on, never judged.
- Codecov down or slow fails the job after 15 minutes with a message naming the delay; re-run it.
- GitHub shows at most 10 error annotations per step; the log and the step summary list every line.
