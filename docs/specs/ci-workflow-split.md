# CI workflow split: spec

**Status: ready.** Start after [`monorepo-refactor.md`](monorepo-refactor.md) has merged.

## Goal

- **One workflow per area**, so a reader finds an area's CI in one file and can run it alone.
- **A pull request runs only the areas it touches.**
- **CI gates `main`**: each area's result and the coverage gate are required checks.

## Layout

`ci.yml` is deleted. Each area is a top-level workflow, not a reusable one.

| Workflow | Paths | Jobs |
|---|---|---|
| `ci-cli.yml` | `cli/`, `dashboard/`, `libs/go-common/`, `libs/graphql/dashboard/` | Go lint, vet, staticcheck, and test on the CLI's OS and arch matrix; `Go · Test (linux, 386)`; the Alpine builds; the cli and dashboard images |
| `ci-cluster.yml` | `cluster/api/`, `cluster/agent/`, `libs/proto/`, `libs/graphql/cluster-api/`, `libs/go-common/` | Go lint, vet, staticcheck, and test of the Cluster API on linux/amd64 and linux/arm64; Rust lint, vet, and test; the cluster-api and cluster-agent images |
| `ci-web.yml` | `apps/web/`, `libs/graphql/` | TypeScript lint, type-check, test, and build |
| `ci-e2e.yml` | every path the three above watch, and `e2e/` | the end-to-end suite, which builds its own images |
| `ci-repo.yml` | the rest | shell, YAML and actions, Markdown, toolchains, digests, the Python lint, `tools/`, `Generated · Check` |

The Dashboard backend is tested in `ci-cli.yml`: it ships inside the CLI, so its tests run on every platform the CLI ships for. `libs/go-common/` is tested there for the same reason. A change to it also runs `ci-cluster.yml`, whose Cluster API imports it.

`coverage.yml`, `codeql.yml`, `audit.yml`, `stress.yml`, `riscv64.yml`, `release-*.yml` and `publish-*.yml` keep their shape. The coverage gate needs one merged result, and CodeQL and the audits work per language.

## Every `ci-*.yml`

- **Triggers**: `pull_request`, `push` to `main`, and `workflow_dispatch` with a `cache-mode` input. Any one can run alone: `gh workflow run ci-cli.yml --ref <branch>`.
- **No `on: paths:`.** A workflow that never starts never reports, and its required check waits forever. The workflow always starts, and its Plan job skips the work.
- **A Plan job**, from a composite action `.github/actions/plan`. It keeps today's fork check, runs `dorny/paths-filter` on a pull request, and says which of the workflow's jobs run. A push or a dispatch runs them all.
- **Its own `CACHE_MODE`, concurrency group and `Caches · Prune` job**, as `ci.yml` has today.
- **A last job, `<Area> · Passed`** (`CLI · Passed`, `Cluster · Passed`, `Web · Passed`, `E2E · Passed`, `Repo · Passed`). It needs every other job, runs `if: always()`, and fails when any of them failed or was cancelled. A job skipped by its `if:` counts as passed. These are the required checks, so each name is unique across the repo.

## Path filters

`.github/ci-config/path-filters.yml` moves to keys narrow enough to compose: `cli`, `dashboard`, `go-common`, `graphql-dashboard`, `graphql-cluster-api`, `cluster-api`, `cluster-agent`, `proto`, `web`, `e2e`, plus today's `shared`, `shell`, `yaml`, `generated`, `docker`, `codeql`, `python`, `actions` and `release-*`.

- Each Plan composes its area from these keys: `ci-cli` runs on `cli || dashboard || go-common || graphql-dashboard`.
- `coverage.yml` and `codeql.yml` compose their languages from the same keys: `go` is `cli || dashboard || go-common || cluster-api || graphql-*`. A path that runs a test still measures its coverage.
- `shared` (the setup action, the Plan action, `path-filters.yml`, `codecov.yml`, `tools/`) runs every area.
- Each `ci-*.yml` is in its own area's filter and in `yaml`. An edit to `ci-cli.yml` runs `ci-cli.yml`.

## Caches

- **Keys start with the job id**, as today. Job ids are unique across the `ci-*.yml` files (`cli-test`, `cluster-api-test`, `rust-test`), so no two workflows write one key.
- **Vet and staticcheck restore their area's test cache and save none.** That retires the `go-vet` and `go-staticcheck` caches, about 1 GB each. Staticcheck keeps its own small analysis cache.
- **`Go · Test (linux, 386)`, the Alpine builds and the end-to-end suite read `cli-test-linux-<arch>`.**
- **Budget**: the Cluster API's two new caches replace the two retired ones. The CLI's six caches shrink, since they no longer hold the Cluster API's packages.
- **Each Prune job prunes its own workflow's keys**, so a dispatched run that saves leaves one copy of each.

## Dependabot

- **Go stays one entry over every module.** The modules carry `replace`s for each other and build without `go.work`, so a dependency they share must move in all of them in one pull request.
- **npm and cargo are already one area each** (`apps/web/`, `cluster/agent/`).
- **Docker and GitHub Actions stay one entry each.** One base-image bump is one pull request, and the release notes read it by path for each component.

## Guards

- **`make lint-yaml` checks that every `ci-*.yml` ends in a `· Passed` job** that needs every other job.
- **The root guide states that a new `ci-*.yml` gates nothing until its `· Passed` job is required** in the `main` ruleset. No check can read the ruleset offline.

## Steps

One pull request each.

1. **This spec.**
2. **`ci-repo.yml` and `ci-web.yml`**, the Plan action and the new filter keys. `coverage.yml` and `codeql.yml` move to the new keys. Their jobs leave `ci.yml`.
3. **`ci-cli.yml`, `ci-cluster.yml` and `ci-e2e.yml`**, with the cache changes above. `ci.yml` is deleted. Write an ADR for one workflow per area.
4. **Required checks**: the five `· Passed` jobs and `coverage / Gate` join the `main` ruleset, a step for a person. Write an ADR that amends "Dependabot moves dependencies, and a person merges every update": CI becomes required, which meets its "Revisit when".
5. **Docs**: the root guide's job names and the guard above, and the two ADRs that name jobs: "Run each OS once on a PR and the other platforms on main" and "Test each component on the platforms it ships for". Delete this spec.

## Check each step

- `make lint-yaml` and `make lint-md` pass.
- On the pull request: each area's jobs run when its paths change, and skip otherwise.
- After merge, on `main`: every area runs, and the cache list holds one copy of each key and no `go-vet` or `go-staticcheck` key.
- `gh workflow run <file> --ref main` runs one area alone.

## Things that can go wrong

- **A skipped workflow blocks a PR.** Only a workflow-level `paths:` causes it. Never add one.
- **A cache written twice.** Two jobs with the same id in different files share a key. Keep job ids unique.
- **A path in no filter** runs nothing on a pull request. `main` runs every area, so it shows up there.

## Done when

- A pull request that touches one area runs that area, plus `ci-repo` and `ci-e2e` where their paths apply.
- Every `ci-*.yml` runs alone by dispatch.
- `main` saves the same caches as before, minus the two retired ones, plus the Cluster API's, with no key written twice.
- The five `· Passed` jobs and `coverage / Gate` are required on `main`.
- This spec is deleted.
