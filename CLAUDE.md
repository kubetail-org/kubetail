# Kubetail

A real-time logging dashboard for Kubernetes, in one monorepo with one folder per component. `apps/web/`, `apps/desktop/`, each Go module, `cluster/agent/`, `libs/proto/`, `libs/graphql/`, `e2e/` and `.github/` have a guide of their own. → [ADR: keep one folder per component](docs/adr/2026-11-17-one-folder-per-component.md)

- **`apps/web/`** — the React single-page app. `apps/mobile/` holds a README until work on it starts.
- **`apps/desktop/`** — Kubetail Desktop, a Tauri app: a React webview, a Rust host and a Go sidecar, each with a guide under `apps/desktop/CLAUDE.md`.
- **`dashboard/`** — the Dashboard backend: serves the UI, a GraphQL API over the Kubernetes API, sessions and the proxy to the Cluster API.
- **`cli/`** — the `kubetail` CLI. `kubetail serve` runs the Dashboard on the user's machine; `kubetail logs` prints logs in the terminal.
- **`cluster/api/`** — the Cluster API: a GraphQL server inside the cluster that reads logs through the Cluster Agents.
- **`cluster/agent/`** — a Rust workspace: the Cluster Agent (`cluster_agent`), one per node, and the log file reader it links (`rgkl`).
- **`libs/`** — what more than one component reads: `go-common/`, the Go packages the other Go modules import; `proto/`, the gRPC contract between the Cluster API and the Cluster Agent; `graphql/<server>/`, each GraphQL server's schema.
- **`go.work`** — the Go workspace of `cli`, `dashboard`, `cluster/api` and `libs/go-common`.
- **`<component>/Dockerfile`** — each component's release image, built from the repo root. It copies only the folders its component needs, each to the same relative place it has in the repo.
- **`config/default/`** — the documented default config file of each component. Defaults are set in each component's code; change both. → [ADR: each component owns its config](docs/adr/2026-11-09-each-component-owns-its-config.md)
- **`tools/`** — a Go module outside the workspace, with a `go.work` of its own: the repo's own tools (coverage, `releasenotes`, the desktop's `designsync` and `securitymodel`) and pins of the Go tools CI runs, so Dependabot moves them.
- **`hack/tilt/kubetail.yaml`** — the whole dev deployment.
- **`e2e/`** — end-to-end tests in Python, run against a kind cluster.
- **`docs/adr/`** — Architecture Decision Records: why the design is the way it is.

**The Helm chart is not in this repo.** A change that needs a new permission, Secret, port, environment variable or config key is made in `hack/tilt/kubetail.yaml` and `e2e/manifests/base.yaml.tmpl` here, and says in the PR that the chart needs the same.

## Local development

```sh
tilt up                                                  # the whole stack on a local cluster
cd apps/web && pnpm install && pnpm dev                  # UI dev server, proxying to :4000
cd dashboard && go run cmd/main.go -c hack/config.yaml   # Dashboard backend on :4000
make                                                     # the CLI with the UI embedded, in bin/kubetail
```

`make build-dashboard-ui` builds the UI and copies it to `dashboard/website/`, which the Dashboard embeds. `pnpm build` alone changes nothing a Go binary serves. → [ADR: embed the UI by a copy step](docs/adr/2026-11-09-embed-the-ui-by-a-copy-step-and-render-from-the-vite-manifest.md)

**In the Linux sandbox on a macOS host, `scripts/sandbox-dev-setup.sh` runs before any build, test or install.** A `SessionStart` hook runs it; it is idempotent, so run it yourself if unsure. It keeps Linux build outputs off the host's paths: Cargo's `target/`, the pnpm store and each worktree's `apps/web/node_modules` live under `/home/agent/kubetail-build`, and `apps/web/dist` under `.sandbox-linux/`. pnpm uses its global virtual store there, so `node_modules` holds symlinks into a store every worktree shares: never edit a file under it. After the script remounts `node_modules`, run `pnpm install`. It also pins Go to CI's version (rerun it after bumping the `go-version` default in `.github/actions/setup-environment/action.yml`) and replaces an image `uv` that crashes. → [ADR: keep the sandbox's build outputs off the host mount](docs/adr/2026-11-09-keep-the-sandboxs-build-outputs-off-the-host-mount.md)

## How a request travels

**There are two environments, and code branches on them by name** (`config.Environment` in `libs/go-common/config`). `desktop` is `kubetail serve` and `kubetail logs` on the user's machine, not the Kubetail Desktop app in `apps/desktop/`: credentials come from the kubeconfig, and a query names its kube context, the kubeconfig's current one by default. `cluster` is the Dashboard deployed in the cluster, or the CLI with `--in-cluster`: the kube context is the empty string, and a request runs as the token it carries, or as the Dashboard's ServiceAccount when `auth-mode` is `auto` and it carries none. → [ADR: one implementation per environment](docs/adr/2026-11-09-one-implementation-per-environment-behind-an-interface.md)

**The browser talks only to the Dashboard backend**: GraphQL at `/graphql` (POST, and WebSocket for subscriptions) and a few REST routes under `/api/`.

**Logs have two backends** behind `LogFetcher` (`libs/go-common/logs`). `KubeLogFetcher` reads the pod log endpoint of the Kubernetes API. `AgentLogFetcher`, used when the Cluster API is installed, asks the Cluster Agent on the pod's node, which reads the file on disk and greps it there. → [ADR: read logs through two backends](docs/adr/2026-11-09-read-logs-through-two-backends-behind-logfetcher.md)

**Nothing dials the Cluster API directly.** It is the Kubernetes APIService `v1.api.kubetail.com`, so every caller goes through the kube-apiserver, which authenticates the caller and forwards the identity in front-proxy headers. → [ADR: reach the Cluster API through the aggregation layer](docs/adr/2026-11-09-reach-the-cluster-api-through-the-aggregation-layer.md)

```
browser ──> Dashboard /cluster-api-proxy/[<kubeContext>/]… ──> kube-apiserver ──> Cluster API ──gRPC, mTLS──> Cluster Agent (per node)
kubetail logs ───────────────────────────────────────────────> kube-apiserver ──> Cluster API
```

## Generated code

Generated Go and TypeScript are committed and never hand-edited: `*.pb.go`, `graph/generated.go`, `graph/model/models_gen.go`, `apps/web/src/lib/graphql/*/__generated__/`, `apps/desktop/src/gql/`. The Rust gRPC code is generated on every build and never committed. After editing a source, `make generate` runs every generator, `cli/assets/`'s copy of `config/default/cli.*` among them, with the `protoc` version and plugins CI pins; `libs/proto/CLAUDE.md`, *Go modules* below and `apps/web/CLAUDE.md` say what each source needs. `make check-generated` runs it and fails if a committed file changed; CI's `Check · Generated` runs that. → [ADR: commit generated Go and TypeScript](docs/adr/2026-11-09-commit-generated-go-and-typescript-generate-rust-at-build-time.md), [ADR: check generated code in CI by regenerating it](docs/adr/2026-11-10-check-generated-code-in-ci-by-regenerating-it.md)

## Go modules

Four modules under the root `go.work`: `cli`, `dashboard`, `cluster/api` and `libs/go-common`. Each module's guide adds its own. The desktop's sidecar, `apps/desktop/sidecar`, is a module outside it, as `tools/` is: it imports no sibling, builds alone, runs its Go commands in its own directory and stays outside the fsnotify pin.

- **Imports point one way.** `libs/go-common` imports no sibling. `dashboard` and `cluster/api` import `libs/go-common` alone. `cli` imports `libs/go-common` and exactly two Dashboard packages, `dashboard/pkg/app` and `dashboard/pkg/config`. Nothing imports `cli` or `cluster/api`.
- **Each module builds without `go.work`**, as the release builds do. Every `go.mod` carries a `replace` for each sibling it imports and a complete `go.sum`. A new import of a sibling module needs the `replace`, and a `COPY` in the component's `Dockerfile`, to the same relative place it has in the repo. → [ADR: each Go module builds without go.work](docs/adr/2026-11-09-each-go-module-builds-without-go-work.md)
- **Every workspace module pins `fsnotify` to v1.8.0 with a `replace`**; keep the four in step. → [ADR: pin fsnotify to v1.8.0](docs/adr/2026-11-09-pin-fsnotify-to-v1-8-0.md)
- **Go is 1.26**; CI and the Dockerfiles pin the patch version (`go-version` in `.github/actions/setup-environment/action.yml`).

### Checks

These hold for every module; each module's guide adds its own and names its coverage target.

While working, in the module, on the packages you touched:

```sh
go vet ./pkg/app/...
go test ./pkg/app/...
```

Before a PR:

```sh
go fmt ./...              # in the module; CI fails if `gofmt -l .` prints anything
```

then the area's `make lint-<area> vet-<area> test-<area>`, which its guide lists. The areas are `cli` (`cli` and `dashboard`), `libs` and `cluster-api`. `lint-<area>` checks gofmt, the license headers and the coverage markers. `vet-<area>` runs `go vet` for this platform and for `GOOS=windows`, `scripts/check-go-fix.sh` and staticcheck. `test-<area>` runs `go test -race`. Ignore a staticcheck finding with `//lint:ignore <check> <reason>` on the line above it. Don't commit a `vendor/` directory.

Coverage is measured per area: every test binary counts every package of its area (`-coverpkg`), so a `libs/go-common` line counts only when its own tests reach it. Each area's `make cover-<area>-changed` runs only the changed packages' tests; `FULL=1` runs them all, as CI does. → [ADR: measure and gate coverage per area](docs/adr/2026-11-19-measure-and-gate-coverage-per-area.md)

`go generate ./...` in a module runs its generators: gqlgen in `dashboard` and `cluster/api`, `protoc` in `libs/go-common`, a copy of `config/default/cli.*` in `cli`.

### Conventions

- **`context.Context` is the first parameter**, and it carries the caller's identity. Pass the request's context down; never start a Kubernetes or gRPC call from `context.Background()` on a request path.
- **Each environment is an implementation behind one interface**, picked by a factory that switches on `config.Environment`: `ConnectionManager`, `Proxy`, `HealthMonitor`. Behaviour that differs by environment goes in both implementations. A resolver checks the environment itself only to refuse an operation that exists in one alone.
- **Errors**: wrap with `fmt.Errorf("doing x: %w", err)` when the caller could not otherwise tell where it failed; sentinels are `errors.New`, tested with `errors.Is`. A resolver that refuses a request returns one of the `Err*` values in `libs/go-common/graphql/errors` (`ErrForbidden`, `ErrUnauthenticated`, `ErrValidationError`, `ErrServiceUnavailable`…), whose `extensions.code` is what a client matches on.
- **Logging is zerolog's global logger**, imported as `zlog "github.com/rs/zerolog/log"`. `zlog.Fatal()` is for startup only. Never log a token, a cookie or a request body.
- **A constructor is `NewX(required…, opts ...Option)`** where there is anything optional. Exported options are production knobs; a seam only tests need is an unexported option, field or constructor (`newClientForTest`).
- **An interface sits beside its implementation** (`k8shelpers.ConnectionManager`, `logs.Stream`). A small unexported interface on the consumer's side is fine as a test seam.
- **Mocks are hand-written testify mocks in a `mock/` subpackage** of the package they fake (`k8shelpers/mock`). No mock generator. A method added to the interface is added to the mock.
- **A method that needs its caller to hold the lock ends in `_UNSAFE`**; the method that takes the lock and calls it has the plain name.
- **Fan-out to subscribers is `github.com/kubetail-org/megaphone`.** Don't hand-roll a subscriber map.
- **Use the current standard library**: `any`, `new(expr)`, `wg.Go(...)`, range over an integer, `slices` and `maps`. CI fails when `go fix` would rewrite a file.
- **Imports are grouped, a blank line between groups**: standard library, third-party, `libs/go-common/...`, then the module's own packages.
- **File names follow their directory**: kebab-case in `libs/go-common` and the older Dashboard packages, snake_case in `cli`. In a directory that mixes them, a new file is snake_case.

**The GraphQL servers, `dashboard` and `cluster/api`, share their rules in [`libs/graphql/CLAUDE.md`](libs/graphql/CLAUDE.md)**: read it before changing a schema or a resolver.

### Tests

The repo-wide rules are the root guide's *Testing conventions*.

- **testify `assert` and `require`**, plain `func TestX(t *testing.T)`, table-driven with `t.Run` where cases repeat. No testify `suite` in new tests.
- **White-box by default** (`package foo`). An external `package foo_test` only to pin a public contract, and it says so.
- **To wait, `select` on a channel against `time.After` or a context deadline**, or use `require.Eventually`. `drainAll` in `cli/internal/clusterapi/stream_test.go` shows the shape.
- **A test passes in any order, at any `-cpu`, run repeatedly**: the nightly `stress.yml` reruns each area's tests so and opens an issue per failing test.
- **Tests run on Linux, macOS and Windows in CI.** Use `t.TempDir()`, `t.Setenv` and `filepath`. A test closes what it opens before it ends, since Windows removes no open file. A platform-specific test goes in a platform-named file (`foo_unix_test.go`, `//go:build !windows`), never behind a `runtime.GOOS` check.
- **`libs/go-common/testutils.NewWebTestClient(t, handler)`** is the HTTP client for app and GraphQL tests: it keeps cookies, sends `Sec-Fetch-Site: same-origin` and replays the last `X-CSRF-Token` on unsafe methods. Prime it with `client.Get("/api/auth/session")`, and `defer client.Teardown()`.
- **Kubernetes is faked with `k8s.io/client-go/kubernetes/fake`** and `k8shelpers/mock.MockConnectionManager`; a server with `httptest`; a gRPC peer with a real `grpc.NewServer()` on `127.0.0.1:0`.
- **`NewApp` in `dashboard` and `cluster/api` builds the real dependencies and passes them to the unexported `newApp`**, which tests call with fakes. Every route and middleware is mounted either way.
- **One package variable is swapped by tests**, `inClusterConfigFn` in `k8shelpers`. New seams are parameters or fields.

## These guides

Each guide has an `AGENTS.md` symlink beside it; a new guide gets one. This file holds what crosses areas.

- **Update a guide in the same change** as the invariant, convention or command it states.
- **Keep them light: they are read on every task.** A line belongs when a change would go wrong without it and the files being edited do not say it: an invariant, a convention, a command, a coupling between files, a trap. What a file holds, what a function does and which flags or keys exist are read from the code. State a fact once, in the narrowest guide that covers it.
- **Each guide has a byte budget** in `.github/ci-config/claude-md-budgets.yml`, which CI enforces (`make lint-repo-md`, `Lint · Markdown`). A new guide gets one, and a commit that raises one explains why in its body.
- **They state what is true now.** Why a design was chosen, and what was rejected, goes in an ADR ([`docs/adr/README.md`](docs/adr/README.md)), linked from the guide that states the decision. The desktop app's own decisions are in [`apps/desktop/docs/adr/`](apps/desktop/docs/adr/README.md). When a decision changes, write a new ADR, flip the old one's status and repoint every link in the same commit. A guide never links a superseded ADR.
- **Describe the present, not the past**, in guides and code comments alike: drop "used to", "no longer", "formerly", and notes on what was renamed or removed. A contrast with an external system ("unlike client-go's default") is fine. `docs/adr/` is the one exemption: it is an append-only record.

## Writing standards

For every hand-written file, not generated output or lockfiles.

1. **Code — simple, idiomatic, easy for a human to follow.** Prefer the boring construction. Match the idiom of the file you are in.
2. **Names — descriptive, idiomatic, consistent with the rest of the codebase,** signatures and schemas included. One concept, one name.
3. **Comments — terse, necessary, easy for a human to read.** Say what the code cannot: the why, the invariant, the trap. Never restate the code, argue against an alternative you rejected (that is what `docs/adr/` is for), address a reviewer or point at a PR.
4. **Documentation — simple, concise, easy for a human to read.** Lead with what is true now. One idea per sentence.

`scripts/writing-standards-hook.sh` (`.claude/settings.json`) restates these every turn. Keep the two in sync.

**Every hand-written Go, Rust and TypeScript file opens with the Apache 2.0 header**, first line `Copyright 2024 The Kubetail Authors`, copied from a neighbouring file. Each `make lint-<area>` fails on one without (`scripts/check-license-headers.sh`). Python and shell files carry none.

Older files predate these standards. Leave unrelated lines alone: a cleanup is a commit of its own.

## Testing conventions

Each area's guide adds its tooling and how it waits. → [ADR: test conventions](docs/adr/2026-11-09-tests-sit-beside-their-files-never-sleep-and-take-seams-as-parameters.md)

- **Every test file sits beside the file it covers** (`foo_test.go`, `foo.test.tsx`, an inline `mod tests` in Rust). A test with no counterpart means the unit is buried in a larger file: split it out.
- **No magic sleeps.** Never `time.Sleep`, `setTimeout` or a manual delay to wait for asynchronous state: wait on the unit's own signal, with a deadline as the failsafe.
- **Two bounded waits are allowed, and each says so in a comment**: a negative assertion (the channel that must stay silent, against a short timer), and latency injected into a fake to make a race determinate.
- **Pace a timing-dependent unit by parameter, not constant.**
- **A test seam is a parameter or a field, never a swapped package variable or a global mode.**
- **A change to a security boundary ships with the test that pins it**, named for the property (`TestAggregationAuth_RejectsTrustedCertAppendedAsChainEntry`, `stream_forward_denies_before_file_lookup`).
- **Every line a PR changes is covered.** Each `coverage-*.yml`'s `Gate` fails on a changed line in its paths that no test reaches, or on the area's total falling more than 0.1 points below the base; each area's `make cover-<area>-changed` lists the same lines locally. → [ADR: gate PRs on changed lines with Codecov's merged result](docs/adr/2026-11-09-gate-prs-on-changed-lines-with-codecovs-merged-result.md), [ADR: measure and gate coverage per area](docs/adr/2026-11-19-measure-and-gate-coverage-per-area.md)
- **A line no test can reach is marked with its reason**: `// coverage:ignore <reason>` at the end of the line a Go statement or function begins on, or `// coverage:ignore-start <reason>` … `// coverage:ignore-stop` in Go and, each on a line of its own, in Rust; `/* v8 ignore next -- <reason> */` (or `start`/`stop`) in TypeScript. A marker without a reason exempts nothing and fails the lint (`scripts/check-coverage-markers.sh`). Generated code and test helpers are left out in `.github/codecov.yml`; `scripts/cover-changed.sh` holds the same list.

## Checks

Each area's guide has a `## Checks` section: what to run while working, and what to run before a PR, ending with the area's `make cover-<area>-changed` where its coverage is measured. Run the second list of every area you touched before you finish. CI runs every check on every platform and is the backstop. A guide's `## Checks` and the one in the `README.md` beside it say the same; change both together. → [ADR: run each area's own checks locally, with CI as the backstop](docs/adr/2026-11-19-run-each-areas-own-checks-locally-with-ci-as-the-backstop.md)

A change in one place can break another area. Also run:

| A change to | Also run |
|---|---|
| `libs/go-common/` | `make test-cli test-cluster-api` |
| `libs/graphql/*/schema.graphqls` | `make generate`, `make vet-web` |
| `libs/proto/` | `make generate`, the Go and Rust checks |
| `apps/desktop/proto/` or the sidecar's GraphQL schema | `make generate-desktop`, then `make vet-desktop test-desktop` |
| a `Dockerfile` or a toolchain version | `make lint-repo-toolchains lint-repo-digests` |
| `.github/` or other YAML | `make lint-repo-yaml` |
| `scripts/` | `make lint-repo-sh test-repo` |
| `tools/` | `cd tools && gofmt -l . && go vet ./... && go test ./...`, and `make lint-desktop` for `designsync` or `securitymodel` |
| Markdown | `make lint-repo-md` |
| `config/` or `hack/` | CI runs no code job for these: name the checks you ran in the PR |

- **Each check is `make <verb>-<area>`**, over the areas CI checks apart: `cli` (with `dashboard`), `libs`, `cluster-api`, `cluster-agent`, `web`, `desktop`, `e2e` and `repo`, for what belongs to no component. No target runs every area. `make help` lists them. `vet-web` type-checks the UI with `tsc -b`; no target runs `vite build` or the checks of `tools/`.
- **`scripts/cover-changed.sh <area>` lists an area's changed lines that no local test run reaches**, measured as its `coverage-*.yml` measures them.
- **Markdown links are checked offline in CI** (`make lint-repo-md`): a moved or renamed file breaks every relative link to it.
- **CI is one workflow per area**, and how the workflows are built is [`.github/CLAUDE.md`](.github/CLAUDE.md). On a pull request each picks its jobs by path, from `.github/ci-config/path-filters.yml`: a change to Markdown alone runs no Go, Rust, TypeScript, Docker or E2E job, and a change to `config/` or `hack/` alone runs no Go, Rust or TypeScript job. A push to `main` or a dispatch runs every job.
- **CI and coverage report on a pull request but do not block a merge.** The `main` ruleset requires only `DCO`, so a red pull request can merge: the reviewer reads the checks. → [ADR: report CI and coverage without requiring them to merge](docs/adr/2026-11-19-report-ci-and-coverage-without-requiring-them-to-merge.md)
- **A PR runs each OS once; `main` runs the other platforms**: the arm64 legs and the Alpine and 386 jobs, so a failure on those platforms shows up after merge. riscv64 runs weekly. → [ADR: run each OS once on a PR and the other platforms on main](docs/adr/2026-11-10-run-each-os-once-on-a-pr-and-the-other-platforms-on-main.md)
- **CI checks each component on every platform it ships for, except the gaps the ADR lists with a reason.** The CLI builds for every shipped platform on `main`, and its unit tests run on every OS and arch a runner has, with 386 standing in for 32-bit arm. E2E runs on Linux alone. A new shipped platform or image target, or code specific to one OS or arch, updates the ADR's matrix. → [ADR: test each component on the platforms it ships for](docs/adr/2026-11-10-test-each-component-on-the-platforms-it-ships-for.md)

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
- **Each toolchain has one exact version, set in one place**: Go and Node in the `go-version` and `node-version` defaults of `.github/actions/setup-environment/action.yml`, Rust in `cluster/agent/rust-toolchain.toml`, pnpm in `apps/web/package.json`'s `packageManager`. `apps/desktop/src-tauri/rust-toolchain.toml` and `apps/desktop/package.json`'s `packageManager` pin the same. CI installs it, and the `golang`, `node` and `rust` images in the release Dockerfiles and `hack/tilt/` carry the same version in their tags. Bump them in one commit, with Node the `@types/node` major in `apps/web/package.json`, and re-pin each image's digest; `make lint-repo-toolchains` (CI's `Lint · Toolchains`) fails on a mismatch. → [ADR: pin each toolchain to one version that CI and the images share](docs/adr/2026-11-10-pin-each-toolchain-to-one-version-that-ci-and-the-images-share.md)
- **Every image a Dockerfile pulls is pinned by tag and digest** (`alpine:3.24.4@sha256:…`), so a re-pushed tag cannot change a release; `make lint-repo-digests` fails on one without. Dependabot moves the other images, tag and digest together. `docker buildx imagetools inspect <image>:<tag>` prints the digest to pin.

## Commits, pull requests, issues, releases

- **Commits** are [conventional](https://www.conventionalcommits.org/): `<type>(<scope>): <description>`, the description in imperative mood, lowercase, no period, under 72 characters. Types: `build`, `chore`, `ci`, `docs`, `feat`, `fix`, `perf`, `refactor`, `revert`, `style`, `test`. Add a body only if the why isn't obvious, wrapped at 72. Always sign off (`-s`). Every commit an AI tool helped write carries an attribution trailer, with a human in the loop or without: `Assisted-by: AGENT_NAME:MODEL_VERSION` or `Co-authored-by: NAME <EMAIL>`. Keep each commit minimal and focused; several are fine when they are well-separated steps.
- **A pull request** fills in `.github/pull_request_template.md` from the diff, in prose, with the placeholders replaced and the checklist items you can settle ticked. Its title is capitalized, in imperative mood, with no conventional-commit prefix, and leads with the emoji the template lists (🎣 bug fix, 🐋 new feature, 📜 documentation, ✨ general improvement). Reference related issues (`Closes #123`, `Ref #124`).
- **A spec plans a large change before it is built, and never reaches `main`.** Propose it as a PR titled `📜 Spec: …`, labelled `spec`, from `gh pr create --template spec.md`. Accepted, it merges into `wip/<topic>`; implementation PRs target that branch, and the PR from it into `main` deletes the spec. An issue labelled `needs-spec` waits for one. To write or build a spec, read [`docs/specs/README.md`](docs/specs/README.md) first. → [ADR: keep specs off main and build them on a wip branch](docs/adr/2026-11-17-keep-specs-off-main-and-build-them-on-a-wip-branch.md)
- **An issue** fills in a template from `.github/ISSUE_TEMPLATE/` (`gh issue create --template "Task"`): `task.md` for work ready to be done (keep `Summary` and `Done when`), `bug_report.md` for a bug not yet understood, `feature_request.md` for a request with no plan yet. Its title is capitalized, in imperative mood, with no period and no emoji. Search open and closed issues first (`gh issue list --state all --search "<terms>"`) and comment on a duplicate instead. Apply labels only from the existing set (`gh label list`); never create one.
- **Each component is released by a tag**: `cli/v<semver>`, `dashboard/v<semver>`, `cluster-api/v<semver>`, `cluster-agent/v<semver>`, `desktop/v<semver>`. The release workflows and the CLI's update check parse it, so the scheme is a contract. A person cuts a release with the `release-cli`, `release-cluster` or `release-desktop` skill; never tag or push a tag yourself. → [ADR: release each component by its own tag](docs/adr/2026-11-09-release-each-component-by-its-own-tag.md)
- **`release-version` recommends a component's next version and `release-notes` writes a draft release's notes** over `tools/releasenotes`. Each reads a component's paths from its `release-<component>` filter in `.github/ci-config/path-filters.yml`: a directory a component starts to ship goes there. → [ADR: write release notes with a skill over deterministic tooling](docs/adr/2026-11-10-write-release-notes-with-a-skill-over-deterministic-tooling.md)
