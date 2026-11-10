# Kubetail

A real-time logging dashboard for Kubernetes, in one monorepo. `dashboard-ui/`, `modules/` and each module in it, `crates/`, `proto/` and `e2e/` have a guide of their own.

- **`dashboard-ui/`** — the React single-page app.
- **`modules/`** — a Go workspace of four modules:
  - **`dashboard/`** — the Dashboard backend: serves the UI, a GraphQL API over the Kubernetes API, sessions and the proxy to the Cluster API.
  - **`cluster-api/`** — the Cluster API: a GraphQL server inside the cluster that reads logs through the Cluster Agents.
  - **`cli/`** — the `kubetail` CLI. `kubetail serve` runs the Dashboard on the user's machine; `kubetail logs` prints logs in the terminal.
  - **`shared/`** — Go packages the other three import.
- **`crates/`** — a Rust workspace: the Cluster Agent (`cluster_agent`), one per node, and the log file reader it links (`rgkl`).
- **`proto/`** — the gRPC contract between the Cluster API and the Cluster Agent.
- **`config/default/`** — the documented default config file of each component. Defaults are set in each component's code; change both. → [ADR: each component owns its config](docs/adr/2026-11-09-each-component-owns-its-config.md)
- **`tools/`** — a Go module outside the workspace: the coverage tools CI runs, `releasenotes` (the `release-notes` skill's), and pins of the Go tools CI runs (`actionlint`, `govulncheck`), so Dependabot moves them.
- **`build/package/`** — the release Dockerfiles. Each copies only the directories its component needs.
- **`hack/tilt/kubetail.yaml`** — the whole dev deployment.
- **`e2e/`** — end-to-end tests in Python, run against a kind cluster.
- **`docs/adr/`** — Architecture Decision Records: why the design is the way it is.

**The Helm chart is not in this repo.** A change that needs a new permission, Secret, port, environment variable or config key is made in `hack/tilt/kubetail.yaml` and `e2e/manifests/base.yaml.tmpl` here, and says in the PR that the chart needs the same.

## Local development

```sh
tilt up                                                          # the whole stack on a local cluster
cd dashboard-ui && pnpm install && pnpm dev                      # UI dev server, proxying to :4000
cd modules/dashboard && go run cmd/main.go -c hack/config.yaml   # Dashboard backend on :4000
make                                                             # the CLI with the UI embedded, in bin/kubetail
```

`make build-dashboard-ui` builds the UI and copies it to `modules/dashboard/website/`, which the Dashboard embeds. `pnpm build` alone changes nothing a Go binary serves. → [ADR: embed the UI by a copy step](docs/adr/2026-11-09-embed-the-ui-by-a-copy-step-and-render-from-the-vite-manifest.md)

**In the Linux sandbox on a macOS host, `scripts/sandbox-dev-setup.sh` runs before any build, test or install.** A `SessionStart` hook runs it; it is idempotent, so run it yourself if unsure. It keeps Linux build outputs off the host's paths: Cargo's `target/`, the pnpm store and each worktree's `dashboard-ui/node_modules` live under `/home/agent/kubetail-build`, and `dashboard-ui/dist` under `.sandbox-linux/`. pnpm uses its global virtual store there, so `node_modules` holds symlinks into a store every worktree shares: never edit a file under it. After the script remounts `node_modules`, run `pnpm install`. It also pins Go to CI's version (rerun it after bumping the `go-version` default in `.github/actions/setup-environment/action.yml`) and replaces an image `uv` that crashes. → [ADR: keep the sandbox's build outputs off the host mount](docs/adr/2026-11-09-keep-the-sandboxs-build-outputs-off-the-host-mount.md)

## How a request travels

**There are two environments, and code branches on them by name** (`config.Environment` in `modules/shared/config`). `desktop` is `kubetail serve` and `kubetail logs` on the user's machine: credentials come from the kubeconfig, and a query names its kube context, the kubeconfig's current one by default. `cluster` is the Dashboard deployed in the cluster, or the CLI with `--in-cluster`: the kube context is the empty string, and a request runs as the token it carries, or as the Dashboard's ServiceAccount when `auth-mode` is `auto` and it carries none. → [ADR: one implementation per environment](docs/adr/2026-11-09-one-implementation-per-environment-behind-an-interface.md)

**The browser talks only to the Dashboard backend**: GraphQL at `/graphql` (POST, and WebSocket for subscriptions) and a few REST routes under `/api/`.

**Logs have two backends** behind `LogFetcher` (`modules/shared/logs`). `KubeLogFetcher` reads the pod log endpoint of the Kubernetes API. `AgentLogFetcher`, used when the Cluster API is installed, asks the Cluster Agent on the pod's node, which reads the file on disk and greps it there. → [ADR: read logs through two backends](docs/adr/2026-11-09-read-logs-through-two-backends-behind-logfetcher.md)

**Nothing dials the Cluster API directly.** It is the Kubernetes APIService `v1.api.kubetail.com`, so every caller goes through the kube-apiserver, which authenticates the caller and forwards the identity in front-proxy headers. → [ADR: reach the Cluster API through the aggregation layer](docs/adr/2026-11-09-reach-the-cluster-api-through-the-aggregation-layer.md)

```
browser ──> Dashboard /cluster-api-proxy/[<kubeContext>/]… ──> kube-apiserver ──> Cluster API ──gRPC, mTLS──> Cluster Agent (per node)
kubetail logs ───────────────────────────────────────────────> kube-apiserver ──> Cluster API
```

## Generated code

Generated Go and TypeScript are committed and never hand-edited: `*.pb.go`, `graph/generated.go`, `graph/model/models_gen.go`, `dashboard-ui/src/lib/graphql/*/__generated__/`. The Rust gRPC code is generated on every build and never committed. **CI does not regenerate or diff any of them**, so regenerate after editing a source: `proto/CLAUDE.md` for the proto, `modules/CLAUDE.md` for a GraphQL schema, `dashboard-ui/CLAUDE.md` for the UI's operations. → [ADR: commit generated Go and TypeScript](docs/adr/2026-11-09-commit-generated-go-and-typescript-generate-rust-at-build-time.md)

## These guides

Each guide has an `AGENTS.md` symlink beside it; a new guide gets one. This file holds what crosses areas.

- **Update a guide in the same change** as the invariant, convention or command it states.
- **Keep them light: they are read on every task.** A line belongs when a change would go wrong without it and the files being edited do not say it: an invariant, a convention, a command, a coupling between files, a trap. What a file holds, what a function does and which flags or keys exist are read from the code. State a fact once, in the narrowest guide that covers it.
- **They state what is true now.** Why a design was chosen, and what was rejected, goes in an ADR ([`docs/adr/README.md`](docs/adr/README.md)), linked from the guide that states the decision. When a decision changes, write a new ADR, flip the old one's status and repoint every link in the same commit. A guide never links a superseded ADR.
- **Describe the present, not the past**, in guides and code comments alike: drop "used to", "no longer", "formerly", and notes on what was renamed or removed. A contrast with an external system ("unlike client-go's default") is fine. `docs/adr/` is the one exemption: it is an append-only record.

## Writing standards

For every hand-written file, not generated output or lockfiles.

1. **Code — simple, idiomatic, easy for a human to follow.** Prefer the boring construction. Match the idiom of the file you are in.
2. **Comments — terse, necessary, easy for a human to read.** Say what the code cannot: the why, the invariant, the trap. Never restate the code, argue against an alternative you rejected (that is what `docs/adr/` is for), address a reviewer or point at a PR.
3. **Documentation — simple, concise, easy for a human to read.** Lead with what is true now. One idea per sentence.

`scripts/writing-standards-hook.sh` (`.claude/settings.json`) restates these every turn. Keep the two in sync.

**A new Go, Rust or TypeScript source file opens with the Apache 2.0 header**, first line `Copyright 2024 The Kubetail Authors`, copied from a neighbouring file. Python and shell files carry none.

Older files predate these standards. Leave unrelated lines alone: a cleanup is a commit of its own.

## Testing conventions

Each area's guide adds its tooling and how it waits. → [ADR: test conventions](docs/adr/2026-11-09-tests-sit-beside-their-files-never-sleep-and-take-seams-as-parameters.md)

- **Every test file sits beside the file it covers** (`foo_test.go`, `foo.test.tsx`, an inline `mod tests` in Rust). A test with no counterpart means the unit is buried in a larger file: split it out.
- **No magic sleeps.** Never `time.Sleep`, `setTimeout` or a manual delay to wait for asynchronous state: wait on the unit's own signal, with a deadline as the failsafe.
- **Two bounded waits are allowed, and each says so in a comment**: a negative assertion (the channel that must stay silent, against a short timer), and latency injected into a fake to make a race determinate.
- **Pace a timing-dependent unit by parameter, not constant.**
- **A test seam is a parameter or a field, never a swapped package variable or a global mode.**
- **A change to a security boundary ships with the test that pins it**, named for the property (`TestAggregationAuth_RejectsTrustedCertAppendedAsChainEntry`, `stream_forward_denies_before_file_lookup`).
- **Every line a PR changes is covered.** CI's `coverage / Gate` fails on a changed line no test reaches, or on a total more than 0.1 points below the base; `make cover-changed` lists the same lines locally. → [ADR: gate PRs on changed lines with Codecov's merged result](docs/adr/2026-11-09-gate-prs-on-changed-lines-with-codecovs-merged-result.md)
- **A line no test can reach is marked with its reason**: `// coverage:ignore <reason>` at the end of the line a Go statement or function begins on, or `// coverage:ignore-start <reason>` … `// coverage:ignore-stop` in Go and, each on a line of its own, in Rust; `/* v8 ignore next -- <reason> */` (or `start`/`stop`) in TypeScript. A marker without a reason exempts nothing and fails the lint (`scripts/check-coverage-markers.sh`). Generated code and test helpers are left out in `.github/codecov.yml`; `scripts/cover-changed.sh` holds the same list.

## Checks

Run the checks in the guide of every area you touched before you finish. For `scripts/`: `make lint-sh` and `bats scripts/`.

- **`make lint` runs every linter CI runs, and `make test-changed` the tests for what the branch changed.** `make vet-all`, `test-all` and `ci-checks` wrap most of the rest, with gaps: `make crates-test` covers `rgkl` alone, and no target runs `pnpm build`.
- **Markdown links are checked offline in CI** (`make lint-md`): a moved or renamed file breaks every relative link to it.
- **On a pull request, CI picks its jobs by path**, from `.github/ci-config/path-filters.yml`. A change to `config/`, `hack/` or `e2e/` alone runs no Go, Rust or TypeScript job, and a schema change under `modules/` does not run the TypeScript jobs. Run the affected checks yourself. A push to `main` runs every job.
- **Only runs on `main` save CI caches; PRs only read them.** `CACHE_MODE` in `ci.yml` decides, and `Caches · Prune` keeps the newest of each. A Go job that saves does it in its last step, and its test flags must match a PR's, or a PR replays no cached test result.
- **A PR runs each OS once; `main` runs the other platforms.** On a PR, `Go · Build and Test` runs on linux/amd64 alone, since `coverage.yml` tests macOS and Windows. `Docker · Build` runs when the PR touches any Go, Rust or TypeScript path, and builds the cli image's `final` stage for amd64 alone unless a Dockerfile changed: E2E builds the other images. The arm64 legs and the Alpine and riscv64 jobs run only on `main`, so a failure on those platforms shows up after merge. → [ADR: run each OS once on a PR and the other platforms on main](docs/adr/2026-11-10-run-each-os-once-on-a-pr-and-the-other-platforms-on-main.md)
- **The end-to-end suite runs when a PR touches any Go, Rust or TypeScript path.** A PR that changes only `e2e/` does not run the suite: run `make test-e2e` yourself (`e2e/CLAUDE.md`) and say so in the PR.
- **`make lint-yaml` lints every YAML file, then the workflows with `actionlint` and `zizmor`** (`.github/ci-config/zizmor.yml`). It needs `uv`, Go and `shellcheck`, and CI's `YAML · Lint` job runs it. Pin a new action to a commit SHA with its exact version in a comment (`# v7.0.1`), and pass `${{ }}` values to a `run:` script through `env:`.
- **A job's `name:` never repeats its workflow's name**: the checks list shows `codeql / Go`, not `codeql / CodeQL · Go`. So names repeat across workflows (`Go`, `Plan`, `Fork gate`), and a ruleset's required check, which matches the job name alone, would match each of them: rename a job to something unique before requiring it.
- **`coverage.yml` reads the same path filters as `ci.yml`**, so a path that runs a language's tests measures its coverage too. A coverage job measures as `scripts/cover-changed.sh` does; change both together. The tools it runs live in `tools/`, a Go module outside the workspace.

## Security invariants

Each area's guide lists what an ordinary change there can break. These hold everywhere:

- **A request that carries an identity runs as that identity.** It rides on the request's `context.Context` (`k8shelpers.K8STokenCtxKey`, `K8SImpersonateCtxKey`) and each transport reads it from there. A call made with `context.Background()`, or with a client built outside the connection manager, drops it and runs as the component's ServiceAccount: a privilege escalation.
- **The identity travels the whole way, and every hop checks for itself.** The Dashboard checks the session and the CSRF token; the Cluster API accepts only the kube-apiserver's front-proxy certificate; the agent accepts only a client certificate from its CA, then authorizes the forwarded user with a `SubjectAccessReview`. Don't drop a check because an earlier hop made one. → [ADR: forward the caller's identity hop by hop](docs/adr/2026-11-09-forward-the-callers-identity-hop-by-hop.md)
- **`allowed-namespaces` is enforced in each server, by each resolver and handler**: `k8shelpers.DerefNamespace` or `DerefNamespaceToList` for a namespace argument, `logs.WithAllowedNamespaces` for source paths. Nothing applies it for you.
- **Log lines and Kubernetes object fields are attacker-controlled text.** Nothing interpolates them into HTML, a CSS selector, a shell command or a file path unchecked.
- **Tokens are never logged and never ride in a URL.** The access log records the raw query string.
- **A state-changing HTTP route is never a GET.** The CSRF check skips GET, HEAD and OPTIONS.

## Dependencies

Add an external dependency only for a material gain in readability or performance, and state the reason. Go: the standard library first. Rust: well-maintained, audited crates. TypeScript: weigh the bundle size.

- **Dependabot proposes updates weekly** (`.github/dependabot.yml`) and a person merges each one. It never moves a toolchain: the Go, Node and Rust versions, `packageManager` and the `@types/node` major are bumped by hand. → [ADR: Dependabot moves dependencies, and a person merges every update](docs/adr/2026-11-09-dependabot-moves-dependencies-and-a-person-merges-every-update.md)
- **Each toolchain has one exact version, set in one place**: Go and Node in the `go-version` and `node-version` defaults of `.github/actions/setup-environment/action.yml`, Rust in `crates/rust-toolchain.toml`. CI installs it, and the `golang`, `node` and `rust` images in `build/package/` and `hack/tilt/` carry the same version in their tags. Bump them in one commit, with Node the `@types/node` major in `dashboard-ui/package.json`, and re-pin each image's digest; `make lint-toolchains` (CI's `Toolchains · Lint`) fails on a mismatch. → [ADR: pin each toolchain to one version that CI and the images share](docs/adr/2026-11-10-pin-each-toolchain-to-one-version-that-ci-and-the-images-share.md)
- **Every image a Dockerfile pulls is pinned by tag and digest** (`alpine:3.24.4@sha256:…`), so a re-pushed tag cannot change a release; `make lint-digests` fails on one without. Dependabot moves the other images, tag and digest together. `docker buildx imagetools inspect <image>:<tag>` prints the digest to pin.
- **`audit.yml` fails a PR only for the advisories it brings in**, and audits the whole tree on `main` and nightly. A failure there opens a `security` issue.

## Commits, pull requests, issues, releases

- **Commits** are [conventional](https://www.conventionalcommits.org/): `<type>(<scope>): <description>`, the description in imperative mood, lowercase, no period, under 72 characters. Types: `build`, `chore`, `ci`, `docs`, `feat`, `fix`, `perf`, `refactor`, `revert`, `style`, `test`. Add a body only if the why isn't obvious, wrapped at 72. Always sign off (`-s`). Add `Co-authored-by` only if no human was in the loop or the user asked. Keep each commit minimal and focused; several are fine when they are well-separated steps.
- **A pull request** fills in `.github/pull_request_template.md` from the diff, in prose, with the placeholders replaced and the checklist items you can settle ticked. Its title is capitalized, in imperative mood, with no conventional-commit prefix, and leads with the emoji the template lists (🎣 bug fix, 🐋 new feature, 📜 documentation, ✨ general improvement). Reference related issues (`Closes #123`, `Ref #124`).
- **An issue** fills in a template from `.github/ISSUE_TEMPLATE/` (`gh issue create --template "Task"`): `task.md` for work ready to be done (keep `Summary` and `Done when`), `bug_report.md` for a bug not yet understood, `feature_request.md` for a request with no plan yet. Its title is capitalized, in imperative mood, with no period and no emoji. Search open and closed issues first (`gh issue list --state all --search "<terms>"`) and comment on a duplicate instead. Apply labels only from the existing set (`gh label list`); never create one.
- **Each component is released by a tag**: `cli/v<semver>`, `dashboard/v<semver>`, `cluster-api/v<semver>`, `cluster-agent/v<semver>`. The release workflows and the CLI's update check parse it, so the scheme is a contract. A person cuts a release with the `release-cli` or `release-cluster` skill; never tag or push a tag yourself. → [ADR: release each component by its own tag](docs/adr/2026-11-09-release-each-component-by-its-own-tag.md)
- **`release-version` recommends a component's next version and `release-notes` writes a draft release's notes** over `tools/releasenotes`. Each reads a component's paths from its `release-<component>` filter in `.github/ci-config/path-filters.yml`: a directory a component starts to ship goes there. → [ADR: write release notes with a skill over deterministic tooling](docs/adr/2026-11-10-write-release-notes-with-a-skill-over-deterministic-tooling.md)
