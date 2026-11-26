# .github — CI, coverage and release workflows

How the workflows are built. The root guide's *Checks* holds what every change needs to know about CI.

## Checks

While working, and before a PR:

```sh
make lint-repo-yaml   # from the repo root: yamllint, actionlint, zizmor and tools/cicallers
```

It needs `uv`, Go and `shellcheck`; CI's `Lint · YAML` runs it. A change to `scripts/` also runs `make lint-repo-sh test-repo`.

## Workflows

- **CI is one workflow per area, called from three**: `ci.yml` calls `ci-cli.yml` (the CLI and the Dashboard backend), `ci-libs.yml` (`libs/go-common`), `ci-cluster.yml`, `ci-web.yml`, `ci-desktop.yml`, `ci-e2e.yml` and `ci-repo.yml`; `coverage.yml` calls `coverage-<area>.yml` for `cli`, `libs`, `cluster`, `web` and `desktop`; `stress.yml` calls `stress-<area>.yml` nightly for `cli`, `libs`, `cluster` and `desktop`. The first two run on every pull request and push to `main`, and never take an `on: paths:`. Each area file also runs alone by dispatch (`gh workflow run ci-cli.yml --ref <branch>`), so a `ci-*.yml` keeps a Plan job, its own `CACHE_MODE` and its own `Prune · Caches`. → [ADR: run CI as one workflow per area](../docs/adr/2026-11-19-run-ci-as-one-workflow-per-area.md)
- **An area file runs on a pull request only when its caller calls it.** `tools/cicallers` fails when `ci.yml`, `coverage.yml` or `stress.yml` misses a `ci-*.yml`, `coverage-*.yml` or `stress-*.yml` beside it. → [ADR: report CI and coverage without requiring them to merge](../docs/adr/2026-11-19-report-ci-and-coverage-without-requiring-them-to-merge.md)
- **On a pull request, each workflow's Plan picks its jobs by path.** `.github/actions/plan` composes them from the narrow keys of `ci-config/path-filters.yml`, as `codeql.yml` does. A path is in a key when it matches one of its patterns and none of its `!` patterns; each key that runs code jobs ends in `!**/*.md`. A push to `main` or a dispatch runs every job. Every job needs `Plan`, which skips a fork's push and cron runs.
- **A push to a `wip/*` branch runs `coverage.yml` and `codeql.yml`, not `ci.yml`**, and saves no Go or Rust cache. It gives the branch's tip the Codecov report and CodeQL baseline a pull request into it is compared with; a pull request into `wip/*` runs CodeQL too. → [ADR: run coverage and CodeQL on a push to a wip branch](../docs/adr/2026-11-19-run-coverage-and-codeql-on-a-push-to-a-wip-branch.md)
- **Pin a new action to a commit SHA with its exact version in a comment** (`# v7.0.1`), and pass `${{ }}` values to a `run:` script through `env:`. zizmor's config is `ci-config/zizmor.yml`.

## Caches

- **Only runs on `main` save CI caches; PRs only read them.** Each `ci-*.yml`'s `CACHE_MODE` decides, and its `Prune · Caches` keeps the newest of each cache its own jobs save.
- **The BuildKit caches `Build · Docker` saves are the exception**: their blobs are shared across scopes, so `ci.yml`'s `Prune · BuildKit` prunes them once, after `cli` and `cluster`, and only when both areas' Docker builds succeeded. It keeps the newest index of each scope and the blobs accessed since the run was created (`scripts/prune-buildkit-caches.sh`).
- **A cache key starts with its job id**, so a job id is unique across the `ci-*.yml` files.
- **Only the `*-test` jobs save a Go cache, in their last step**; vet, staticcheck, Alpine, 386 and E2E restore one. A test job's flags must match a PR's, or a PR replays no cached test result.

## Platforms

- **On a PR, `ci-cli.yml`'s and `ci-libs.yml`'s `Build and Test` run on linux/amd64 alone**, since `coverage-cli.yml` and `coverage-libs.yml` test macOS and Windows. `ci-cli.yml`'s `Build · Docker` builds the cli image's `final` stage for amd64 alone unless a Dockerfile changed, and `ci-cluster.yml` builds its images only then: E2E builds the other images. The arm64 legs and the Alpine and 386 jobs run only on `main` and on a dispatch; the riscv64 tests and images run weekly, in `riscv64.yml`. → [ADR: run each OS once on a PR and the other platforms on main](../docs/adr/2026-11-10-run-each-os-once-on-a-pr-and-the-other-platforms-on-main.md), [ADR: run the riscv64 jobs weekly](../docs/adr/2026-11-10-run-the-riscv64-jobs-weekly.md)
- **`ci-config/runners.json` lists the runners the `ci-*.yml` files test on**, in sets (`default`, and the desktop's own), and marks the ones a PR runs; the Plan action turns a workflow's set into its matrices. → [ADR: define the CI runner matrix in one runners.json file](../docs/adr/2026-11-17-define-the-ci-runner-matrix-in-one-runners-json-file.md), [ADR: keep a runner set per area in runners.json](../docs/adr/2026-11-25-keep-a-runner-set-per-area-in-runners-json.md)

## Job names

- **A job's `name:` is `<Verb> · <Qualifier> (<matrix values>)`**: `Lint · Rust`, `Build · Docker (cli, amd64, final)`. The qualifier tells same-verb jobs apart and is left out when the workflow has one subject (`ci / libs / Lint`). Matrix values are values only, from OS to arch to variant; GitHub appends them to a name with no `${{ }}` expression. A single-purpose workflow drops the verb (`codeql / Go`) unless nothing would be left (`Measure`). `Plan`, `Gate`, `Prune · Caches` and `Report · Failures` are fixed. Change only `name:`, never a job id. → [ADR: name workflow jobs verb first](../docs/adr/2026-11-19-name-workflow-jobs-verb-first.md)
- **A job's `name:` never repeats its workflow's name**: the checks list shows `codeql / Go`, not `codeql / CodeQL · Go`. So names repeat across workflows (`Go`, `Plan`), and a ruleset's required check, which matches the job name alone, would match each of them: rename a job to something unique before requiring it.

## Coverage and stress

- **Each area measures its coverage in its own `coverage-*.yml`**, on the same filter keys as its `ci-*.yml`, so a path that runs a language's tests measures its coverage too. Its uploads carry the area's Codecov flags, and its gate reads the area's component in `codecov.yml`; a new area adds both, with its flags in `flag_management`. No area's flags may measure another component's paths. A coverage job measures as `scripts/cover-changed.sh` does; change both together. A new coverage area also gets a case in `scripts/cover-changed.sh`, a `cover-<area>-changed` target and a case in `scripts/cover-changed.bats`, which runs each area's suite and checks it lists a known uncovered line. The tools it runs live in `tools/`.
- **Each `stress-<area>.yml` runs its area's tests with `-shuffle=on`, `-count` and several `-cpu` values, with and without `-race`**, and opens an issue labelled with the area per failing test, or per package that fails outside one, with its output. Its report script is `scripts/stress.go`, tested by `make test-repo`. → [ADR: run the stress tests as one workflow per area](../docs/adr/2026-11-26-run-the-stress-tests-as-one-workflow-per-area.md)
- **`audit.yml` fails a PR only for the advisories it brings in**, and audits the whole tree on `main` and nightly. A failure there opens a `security` issue.

## Releases

**A published `<component>/v*` release runs the `publish-<component>-*.yml` workflows**, each job gated on its tag, and a `desktop/v*` release is never the repo's latest.

**Each release attests its build provenance** in the job that publishes it: `release` in `release-cli.yml` and `release-desktop.yml`, `create-and-publish-manifest` in `release-docker.yml`. `SECURITY.md`'s `gh attestation verify` commands name those workflow files: renaming one, or moving the attest step out of it, breaks them. → [ADR: attest build provenance in the release jobs](../docs/adr/2026-11-10-attest-build-provenance-in-the-release-jobs.md)
