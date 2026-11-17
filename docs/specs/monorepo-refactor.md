# Monorepo refactor: spec

This spec says how to reorganize the repo into one folder per component, and what stays as it is.

The CI workflows keep their shape here: they only get new paths. Splitting `ci.yml` by component is a separate spec, [`ci-workflow-split.md`](ci-workflow-split.md).

## Goal

- **One folder per component.** A reader finds the CLI in `cli/`, the Dashboard in `dashboard/`, and so on.
- **Shared code and shared schemas live under `libs/`.**
- **Room for the desktop and mobile apps**, without building them yet.

Nothing a user sees changes. No behaviour, flag, config key, image name or release tag changes.

## Target layout

```text
kubetail/
├── apps/
│   ├── web/              the React app (today: dashboard-ui/)
│   ├── desktop/          README only: coming soon
│   └── mobile/           README only: coming soon
├── cli/                  Go module (today: modules/cli/)
├── dashboard/            Go module (today: modules/dashboard/)
├── cluster/
│   ├── api/              Go module (today: modules/cluster-api/)
│   └── agent/            Rust workspace (today: crates/)
├── libs/
│   ├── go-common/        Go module (today: modules/shared/)
│   ├── proto/            the gRPC contract (today: proto/)
│   └── graphql/
│       ├── dashboard/    schema.graphqls (today: modules/dashboard/graph/)
│       └── cluster-api/  schema.graphqls (today: modules/cluster-api/graph/)
├── go.work  go.work.sum  the Go workspace (today: modules/go.work, go.work.sum)
├── tools/                unchanged, plus a go.work of its own
├── config/  docs/  e2e/  hack/  scripts/              unchanged
└── Makefile  Tiltfile  README.md  …                   unchanged
```

Each release Dockerfile moves next to its component: `build/package/Dockerfile.cli` becomes `cli/Dockerfile`, and the same for `dashboard/`, `cluster/api/` and `cluster/agent/`. The build context stays the repo root.

## What moves where

| Today | After | Notes |
| --- | --- | --- |
| `modules/cli/` | `cli/` | Module path becomes `github.com/kubetail-org/kubetail/cli` |
| `modules/dashboard/` | `dashboard/` | Module path becomes `…/kubetail/dashboard` |
| `modules/cluster-api/` | `cluster/api/` | Module path becomes `…/kubetail/cluster/api` |
| `modules/shared/` | `libs/go-common/` | Module path becomes `…/kubetail/libs/go-common` |
| `modules/go.work`, `go.work.sum` | the repo root | Lists the four modules above |
| `crates/` | `cluster/agent/` | The whole Cargo workspace moves as one, with `rust-toolchain.toml` |
| `proto/` | `libs/proto/` | The `go_package` option stays as it is |
| the two `schema.graphqls` files | `libs/graphql/<server>/` | The file name stays `schema.graphqls` |
| `dashboard-ui/` | `apps/web/` | A rename only. No split into a shared library yet |
| `build/package/Dockerfile.<c>` | `<component>/Dockerfile` | |

**Guides.** Each moved folder keeps its `README.md`, `CLAUDE.md` and `AGENTS.md`. `crates/CLAUDE.md` moves with the workspace to `cluster/agent/`. `modules/CLAUDE.md` has no folder left to sit in: what it says about all Go modules moves to the root guide.

**Docs.** `docs/` stays at the root. ADRs stay together in `docs/adr/`, for every component. A spec that crosses components lives in `docs/specs/`. A spec for one component lives in that component's own `docs/specs/` (`cli/docs/specs/`, `cluster/agent/docs/specs/`), created when the first one exists. No such folder is created by this refactor.

## What changes

- The folder layout: `apps/`, `libs/`, and one top-level folder per component.
- Shared schemas move under `libs/`, one folder per kind (`proto`, `graphql`).
- The Cluster API and the Cluster Agent sit together under `cluster/`. Each keeps its own version and release tag.
- `tools/` gets its own `go.work`.

## What stays the same

We weighed changing each of these and chose not to.

| Stays | Instead of | Why |
| --- | --- | --- |
| The `Makefile` | A `justfile` | CI and the `*-changed.sh` scripts are built on it |
| The release skills | release-please | A person cuts each release (ADR) |
| Dependabot | Renovate | Already decided (ADR) |
| The Cargo workspace | Separate crates, one `Cargo.lock` each | It holds the shared dependency versions and lints. Only the agent uses Rust |
| A committed `go.work`, now at the root | A local, gitignored `go.work` | Go coverage is measured across the workspace (ADR) |
| The Helm chart in its own repo | The chart in this repo | Out of scope |
| All ADRs in `docs/adr/` | ADRs in each component's folder | The decisions with the most reasoning cross components (ADR) |
| Dated ADR file names | Numbered ADRs (`0001-…`) | Already in use |
| The ADR `scope` values | Values renamed to match the folders | They label a component, not a path |
| No shared Rust or TypeScript library | `libs/rust-common`, `libs/js-common` | Each would have one consumer today |
| The release tags | Tags named after the folders | The release workflows and the CLI's update check parse them (ADR) |

## Steps

Each step is one pull request. Steps 2 to 7 are moves with no logic change. Land them close together, in a quiet week, so open pull requests rebase once.

1. **Write the ADR.** Record the layout and the table in "What stays the same". It names the new folders in code font and links none of them: a link to a path that does not exist yet fails `make lint-md`.
2. **Move the Go modules.**
   - Move `modules/*` to `cli/`, `dashboard/`, `cluster/api/` and `libs/go-common/`.
   - Rename the module paths and every import, then run `go generate`. gqlgen writes the module path into names inside `generated.go`, so a search and replace alone leaves it stale.
   - Move `go.work` and `go.work.sum` to the root. The coverage pattern becomes `github.com/kubetail-org/kubetail/...`.
   - Add `tools/go.work` (`go 1.26.11`, `use .`). Without it, every Go command in `tools/` finds the root `go.work`, which does not list `tools`, and fails.
   - Point `dashboard-ui/codegen.ts` at the schemas' new place. They move with the modules.
   - Update the seven package recipes (see "Things that can go wrong").
3. **Move the Rust workspace.** `crates/` to `cluster/agent/`.
4. **Move the proto files.** `proto/` to `libs/proto/`.
5. **Move the GraphQL schemas.** To `libs/graphql/`. Point each `gqlgen.yml` and `codegen.ts` at the new path, then regenerate.
6. **Rename the UI.** `dashboard-ui/` to `apps/web/`. Add the two "coming soon" READMEs, each linking a tracking issue. In the sandbox, unmount `dashboard-ui/node_modules` and `dashboard-ui/dist` before the move.
7. **Move the Dockerfiles.** `build/package/` goes away. `release-docker.yml`, `riscv64.yml`, `ci.yml` and `e2e/docker-bake.hcl` build the Dockerfile path from the component name: give each a lookup, since `cluster-api` now maps to `cluster/api/Dockerfile`.

## Check each step

A pull request runs only some of CI. The Alpine, 386 and arm64 jobs run on `main` alone, and the riscv64, stress, release and publish workflows run on other triggers. So before each move merges:

- `make check`, `make check-generated` and `make test-e2e` pass locally.
- All four release images build.
- A `workflow_dispatch` run of `ci.yml` on the branch passes. A dispatch runs every job.
- For steps 2, 3 and 7: `workflow_dispatch` runs of `riscv64.yml` and `stress.yml` pass.
- For step 2: one package builds from its recipe (`makepkg` or `rpmbuild`), and its `kubetail --version` prints the right version.

Before the first CLI release from the new layout, rehearse `release-cli.yml` in a fork with a test tag. The workflow checks out the tag, so a run against an old tag builds the old layout and proves nothing.

## Every move updates these

A move is not done until each of these points at the new path.

**By name.** `git grep` for the old path must return nothing, with two exceptions: the `release-<component>` filters keep the old paths (see below), and `docs/adr/` keeps them in its history.

- `.github/ci-config/path-filters.yml`
- `.github/workflows/*.yml`, `.github/actions/setup-environment/action.yml`, `.github/codecov.yml`, `.github/dependabot.yml`, `CODEOWNERS`
- `Makefile`, `Tiltfile`, `hack/tilt/`, `e2e/docker-bake.hcl`, `.dockerignore`, `.gitignore`, `.yamllintignore`, `.vscode/settings.json`
- every script in `scripts/` and its test, and `scripts/stress.go`
- the Dockerfiles, which copy folders by name
- the package recipes in `.github/ci-config/`. MacPorts spells the module path `${go.package}/modules/cli`, so a search for the full path misses it
- `README.md`, the translated READMEs in `.github/`, `CONTRIBUTING.md` and every `CLAUDE.md` guide
- `.claude/` settings and skills, including the path table in the `release-version` skill

**By depth.** These hold a relative path that breaks when a folder moves up or down, and no search for the old name finds them:

- the `replace` lines in each `go.mod` (`../shared` becomes `../libs/go-common`, or `../../libs/go-common` from `cluster/api`)
- the Dockerfiles' layout inside the image, which must match those `replace` lines. Copy each folder to the same relative place it has in the repo
- `cli/assets/embed.go`: its `go:generate` line copies from `../../../config/default`
- `crates/types/build.rs`: it reads `../../proto`
- `-o ../../bin/kubetail` after `cd modules/cli`, in the `Makefile`, `ci.yml`, `release-cli.yml` and six package recipes
- `codegen.ts`: its paths to the schemas change in steps 2, 5 and 6

**ADRs.** A path or name in an ADR that the code has moved away from is fixed in place, in the step that moves it. The decision's text does not change. Two ADRs name `modules/` in their decision: "Measure Go coverage across the workspace" and "Each Go module builds without go.work".

## Things that can go wrong

- **Package recipes report the wrong version.** Seven recipes in `.github/ci-config/` (Alpine, AUR, COPR, GURU, Launchpad, MacPorts, OBS) build from `modules/cli` and set the version with `-X github.com/kubetail-org/kubetail/modules/cli/cmd.version=…`. A stale `-X` path does not fail the build: the binary just reports the wrong version. They run only at release time, so CI does not catch it.
- **Recipes kept outside this repo.** The source tarball's layout changes from `modules/cli` to `cli`. Tell the maintainers of `kubetail-org/kubetail-nix` and nixpkgs before the first release after step 2.
- **Release notes lose commits.** `tools/releasenotes` finds a component's commits by the paths in its `release-<component>` filter. Keep the old paths there beside the new ones until each component has released once, or commits made before the move drop out.
- **The GraphQL schema is no longer embedded as a file.** gqlgen embeds a schema only when it sits inside the generated package's folder. From `libs/graphql/` it writes the schema into `generated.go` as a string. This needs no Dockerfile change, and the server behaves the same.
- **Broken links.** `make lint-md` fails on a link to a moved file, including links inside ADRs.
- **Dismissed CodeQL alerts come back.** Alerts are keyed by path, so a moved file's dismissed alerts reopen. Dismiss them again.
- **The release tags now also name Go module versions.** `cli/v1.2.3` reads as a version of the module `…/kubetail/cli`. `go install` of it fails on the `replace` lines, as it does today. Nothing to do.

## Out of scope

- Splitting `ci.yml` by component and adding one required check: see [`ci-workflow-split.md`](ci-workflow-split.md).
- Building the desktop or mobile app.
- Splitting the UI into a shared library and thin shells. That happens when the second app starts.
- Moving the Helm chart into this repo.
- Changing release tags, image names, config keys or anything a user sees.

## Done when

- The tree matches "Target layout".
- `modules/`, `crates/`, `proto/`, `dashboard-ui/` and `build/package/` no longer exist.
- `make check`, `make check-generated` and `make test-e2e` pass.
- A full run of CI on `main` passes, with the riscv64 and stress workflows.
- One release of each component has gone out from the new layout, with correct version output and complete release notes. After it, the old paths leave that component's `release-<component>` filter.
- The root guide and each component guide describe the new layout, and the ADR is linked from the root guide.
- This spec is deleted.
