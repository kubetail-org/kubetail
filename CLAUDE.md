# Kubetail

A real-time logging dashboard for Kubernetes. A monorepo of parts that talk to each other:

- **`dashboard-ui/`** — the React single-page app. See `dashboard-ui/CLAUDE.md`.
- **`modules/`** — a Go workspace of four modules. See `modules/CLAUDE.md`, and the guide in each module:
  - **`modules/dashboard/`** — the Dashboard backend: serves the UI, a GraphQL API over the Kubernetes API, sessions and the proxy to the Cluster API.
  - **`modules/cluster-api/`** — the Cluster API: a GraphQL server inside the cluster that reads logs through the Cluster Agents.
  - **`modules/cli/`** — the `kubetail` CLI. `kubetail serve` runs the Dashboard on the user's machine; `kubetail logs` prints logs in the terminal.
  - **`modules/shared/`** — Go packages the other three import.
- **`crates/`** — a Rust workspace: the Cluster Agent (`cluster_agent`), one per node, and the log file reader it links (`rgkl`). See `crates/CLAUDE.md`.
- **`proto/`** — the gRPC contract between the Cluster API and the Cluster Agent. See *The gRPC contract*.
- **`config/default/`** — the documented default config file of each component. Each component's defaults are set in its own code; a change to a default is made in both, and the CLI's file is also copied into `modules/cli/assets/` (`modules/cli/CLAUDE.md`).
- **`build/package/`** — the release Dockerfiles, one per component. Each copies only the directories its component needs.
- **`hack/`** — local development: the Tilt manifests (`hack/tilt/kubetail.yaml` is the whole dev deployment), `ctlptl` cluster definitions and example workloads.
- **`e2e/`** — end-to-end tests in Python, run against a kind cluster. See *End-to-end tests*.
- **`scripts/`** — `install.sh`, the CLI installer, with its `bats` tests.
- **`.claude/skills/`** — `release-cli` and `release-cluster`, which tag releases. A person invokes them; never tag or push a tag yourself.

**The Helm chart is not in this repo.** A change that needs a new permission, Secret, port, environment variable or config key is made in `hack/tilt/kubetail.yaml` and `e2e/manifests/base.yaml.tmpl` here, and says in the PR that the chart needs the same.

## Local development

```sh
tilt up                                                          # the whole stack on a local cluster
cd dashboard-ui && pnpm install && pnpm dev                      # UI dev server, proxying to :4000
cd modules/dashboard && go run cmd/main.go -c hack/config.yaml   # Dashboard backend on :4000
make                                                             # the CLI with the UI embedded, in bin/kubetail
```

The UI reaches a Go binary by a copy step: `make build-dashboard-ui` builds `dashboard-ui/dist` and copies it to `modules/dashboard/website/`, which `modules/dashboard/embed.go` embeds. The CLI gets it by importing the Dashboard.

## How a request travels

**There are two environments, and code branches on them by name.** `desktop` is `kubetail serve` on the user's machine: credentials come from the kubeconfig, and a query names the kube context it wants, the kubeconfig's current one by default. `cluster` is the Dashboard deployed in the cluster: one cluster, the kube context is the empty string, and a request runs as the token it carries (from the session or an `Authorization` header), or as the Dashboard's ServiceAccount when `auth-mode` is `auto` and it carries none. The switch is `config.Environment` (`modules/shared/config`); each seam has one implementation per environment behind an interface (`ConnectionManager`, `Proxy`, `HealthMonitor`).

**The browser talks only to the Dashboard backend**: GraphQL at `/graphql` (POST for queries and mutations, WebSocket for subscriptions) and a few REST routes under `/api/`. Kubernetes objects are read with client-go.

**Logs have two backends.** Without the Cluster API, a log stream reads the pod log endpoint of the Kubernetes API (`KubeLogFetcher`). With it, the stream asks the Cluster Agent on the pod's node, which reads the file on disk and can grep it there (`AgentLogFetcher`). Both live in `modules/shared/logs` behind `LogFetcher`.

**Nothing dials the Cluster API directly.** It is registered as a Kubernetes APIService (`v1.api.kubetail.com`, served at `/apis/api.kubetail.com/v1`), so every caller goes through the kube-apiserver, which authenticates the caller and forwards the identity in front-proxy headers:

```
browser ──> Dashboard /cluster-api-proxy/[<kubeContext>/]… ──> kube-apiserver ──> Cluster API ──gRPC, mTLS──> Cluster Agent (per node)
kubetail logs ───────────────────────────────────────────────> kube-apiserver ──> Cluster API
```

The path carries the kube context in `desktop` alone. `kubetail logs` uses the Cluster API when it is installed and falls back to the Kubernetes API when it is not.

**The caller's identity travels the whole way, and each hop re-checks it.** The Cluster API impersonates the caller on its own Kubernetes calls and sends the identity to the agent as `x-remote-*` gRPC metadata; the agent authorizes it with a `SubjectAccessReview` before it opens a file. See *Security invariants*.

## The gRPC contract

`proto/cluster_agent.proto` is the single source of truth for `LogMetadataService` and `LogRecordsService`. The Rust server is generated at build time (`crates/types/build.rs`, never committed); the Go client is committed protoc output in `modules/shared/clusteragentpb`. After editing the proto:

```sh
cd modules/shared && go generate ./...   # needs protoc, protoc-gen-go, protoc-gen-go-grpc
cd crates && cargo build                 # needs protoc
```

Commit the regenerated `*.pb.go`; never hand-edit them. The Cluster API binds the `LogMetadata*` messages straight into its GraphQL schema (`modules/cluster-api/gqlgen.yml`), so a change to one of them is also a schema change. `LogMetadataWatchEvent.type` is a string (`ADDED`, `MODIFIED`, `DELETED`) spelled by hand in the agent and again in the UI (`pages/home/log-metadata-provider.tsx`); the Cluster API passes it through.

## The GraphQL contracts

Each server's `graph/schema.graphqls` is authoritative: `modules/dashboard` and `modules/cluster-api` have one each, and the UI generates its types from both. After editing a schema:

```sh
cd modules/<module> && go generate ./...       # gqlgen
cd dashboard-ui && pnpm graphql-codegen        # the UI's types
```

Generated files are committed and never hand-edited: `graph/generated.go`, `graph/model/models_gen.go` and `dashboard-ui/src/lib/graphql/*/__generated__/`. **CI does not regenerate or diff any of them**, so a stale file is caught only by a build or a reviewer.

**The log operations exist in both schemas with the same signature** (`logRecordsFetch`, `logRecordsFollow`), and the UI sends one set of documents to whichever server is in use. Change them in both schemas together.

> **Keep these docs current.** There is a `CLAUDE.md` per area and one per Go module; this file holds what crosses them. Each has an `AGENTS.md` symlink beside it, so add both when adding a guide. When you change architecture, conventions or commands, or add a pattern worth knowing, update the relevant `CLAUDE.md` in the same change.
>
> **These files state what is true now**: invariants, conventions, commands, traps. Why a design was chosen, and what was rejected, goes in the commit body or the PR's Summary.
>
> **Keep comments current — describe the present, not the past.** Code comments and these docs describe only the current code. Don't leave notes about how code used to work, what was renamed or removed, or which refactor got you here. Drop "used to", "no longer", "formerly". Contrasts with external systems ("unlike client-go's default") are fine.

## Writing standards

Applies to every hand-written file: Go, Rust, TypeScript, Python, Markdown. Not to generated output or lockfiles.

1. **Code — simple, idiomatic, easy for a human to follow.** Prefer the boring construction. Match the idiom of the file you are in over the one you would pick on a blank page. Cleverness that needs a comment to survive review is usually the wrong trade.
2. **Comments — terse, necessary, easy for a human to read.** Say what the code cannot: the why, the invariant, the trap that made this shape necessary. Never restate what the line already says (`// Init client`, a banner repeating the function's name). State the current design; don't argue against the alternatives you rejected.
3. **Documentation — simple, concise, easy for a human to read.** Lead with what is true now. One idea per sentence.

Watch for comments addressed to a reviewer rather than a reader opening the file cold; the tell is a comment that spends its length on the option not taken, or that points at a PR.

**A new Go, Rust or TypeScript source file opens with the Apache 2.0 header**, first line `Copyright 2024 The Kubetail Authors`. Copy it from a neighbouring file. Python and shell files carry none.

Older files predate these standards. Follow the standards in what you write and leave unrelated lines alone: a cleanup is a commit of its own.

## Testing conventions (all languages)

Each area's guide adds its tooling.

- **Every test file sits beside the file it covers** (`foo_test.go`, `foo.test.tsx`, an inline `mod tests` in Rust). A test with no counterpart means the unit is buried in a larger file: split the implementation out to match.
- **No magic sleeps.** Never `time.Sleep`, `setTimeout` or a manual delay to wait for asynchronous state.
  - **Go** — wait on a channel, with a `select` on `time.After` or a context deadline as the failsafe, or use `require.Eventually`.
  - **TypeScript** — `vi.useFakeTimers()` with `vi.advanceTimersByTimeAsync`, or `await waitFor(...)`.
  - **Rust** — await the unit's own signal (`LifecycleEvent::WatcherStarted`) before acting, and bound the wait for the result with `tokio::time::timeout`.
- **Pace a timing-dependent unit by parameter, not constant**: production passes the constant, a test passes a short one.
- **A test seam is a parameter or a field, never a swapped package variable or a global mode.** Take the dependency as a constructor argument, an option or an interface.
- **Go tests run on Linux, macOS and Windows in CI.** Use `t.TempDir()` and `filepath`. A test closes what it opens before it ends, since Windows removes no open file. A platform-specific test goes in a platform-named file (`foo_unix_test.go`, `//go:build !windows`), never behind a `runtime.GOOS` check.
- **A change to a security boundary ships with the test that pins it**, named for the property (`TestAggregationAuth_RejectsTrustedCertAppendedAsChainEntry`, `stream_forward_denies_before_file_lookup`).

Two bounded waits are allowed, and each says so in a comment:

- **A negative assertion** — a `select` on the channel that must stay silent against a short timer.
- **Latency injected into the code under test** — a fake that pauses on purpose to make a race determinate.

## Checks

Run the checks of every area you touched before you finish. The `Makefile` wraps most of them (`make lint-all`, `make vet-all`, `make test-all`, `make ci-checks`), with gaps: `make crates-test` covers `rgkl` alone, and no target runs `pnpm build`, the Python lint or the shell checks.

| Area | Format and lint | Static analysis | Tests |
| --- | --- | --- | --- |
| Go (`modules/`) | `gofmt -l .` must print nothing; fix with `go fmt ./...` in the module | `go vet ./...` | `go test -race ./...` |
| TypeScript (`dashboard-ui/`) | `pnpm lint`; fix with `pnpm lint --fix` | `pnpm build` (type-checks) | `pnpm test run` |
| Rust (`crates/`) | `cargo fmt --all -- --check` | `cargo clippy --all -- -D warnings` | `cargo test` |
| Python (`e2e/`) | `uv run ruff check . && uv run ruff format --check .` | | `make test-e2e` |
| Shell (`scripts/`) | `shellcheck scripts/install.sh` | | `bats scripts/` |

- **`make test-changed`, `make cover-changed` and `make lint`, named in the PR template and `CONTRIBUTING.md`, do not exist.** Use the commands above.
- **CI picks its jobs by path**: `modules/**` runs the Go jobs, `crates/**` the Rust jobs, `dashboard-ui/**` the TypeScript jobs. A change to `proto/`, `config/`, `build/`, `hack/` or `e2e/` alone runs none of them, and a schema change under `modules/` does not run the TypeScript jobs. Run the affected checks yourself.
- **The Docker builds, the CLI builds and the end-to-end suite run only when one PR touches `modules/`, `crates/` and `dashboard-ui/` together.** Each needs the lint jobs of all three areas, and a job that needs a skipped job is skipped. For a change that crosses a component boundary, run `make test-e2e` yourself and say so in the PR.
- **Workflows are linted with `actionlint`** (`.github/ci-config/actionlint.yaml`). Pin a new action to a commit SHA with its version in a comment, as most are.

## End-to-end tests

`make test-e2e` builds the CLI and the three images, creates or reuses the kind cluster `kubetail-e2e`, deploys `e2e/manifests/base.yaml.tmpl`, port-forwards, and runs `uv run pytest -v` in `e2e/`. Beside the Go and pnpm toolchains it needs Docker with buildx, kind, kubectl, uv, `envsubst` and `curl`. `cd e2e && uv run pytest -v` reruns the tests against what is deployed; `ONLY_BUILD=1` rebuilds without deploying, and `e2e/scripts/up.sh` loads the new images into the cluster; `e2e/scripts/down.sh` deletes it.

The suite is where the boundaries between components are tested for real: token auth, CSRF, WebSocket auth through the proxy, `allowed-namespaces`, namespace RBAC, and mTLS between the Cluster API and the agents (`test_zero_trust.py`). CI runs it on three Kubernetes versions, the oldest 1.21. A change to one of those boundaries updates its test.

## Security invariants

Each area's guide lists what an ordinary change there can break. These hold everywhere:

- **A request that carries an identity runs as that identity.** It rides on the request's `context.Context` (`k8shelpers.K8STokenCtxKey`, `K8SImpersonateCtxKey`) and each transport reads it from there. A call made with `context.Background()`, or with a client built outside the connection manager, drops it and runs as the component's ServiceAccount: a privilege escalation.
- **Every hop checks for itself.** The Dashboard checks the session and the CSRF token; the Cluster API accepts only the kube-apiserver's front-proxy certificate; the agent accepts only a client certificate from its CA and then authorizes the forwarded user. Don't drop a check because an earlier hop made one.
- **`allowed-namespaces` is enforced in each server, by each resolver and handler**: `k8shelpers.DerefNamespace` or `DerefNamespaceToList` for a namespace argument, `logs.WithAllowedNamespaces` for source paths. Nothing applies it for you, so a new resolver or handler that takes either calls one of them.
- **Log lines and Kubernetes object fields are attacker-controlled text.** The UI has no HTML sink of its own; nothing interpolates them into HTML, a CSS selector, a shell command or a file path unchecked.
- **Tokens are never logged and never ride in a URL.** The access log records the raw query string.
- **A state-changing HTTP route is never a GET.** The CSRF check skips GET, HEAD and OPTIONS.

## Dependencies

- Avoid introducing new external dependencies unless it will have a material impact on code readability or performance
- If a new dependency is required, state the reason clearly
- For Go: Use standard library when possible
- For Rust: Prefer well-maintained, audited crates
- For TypeScript: Consider bundle size impact

## Commits

Keep commits minimal and focused. Multiple commits to accomplish a task are fine if they represent logical, well-separated steps that make the change easier to review.

Use [conventional commit](https://www.conventionalcommits.org/) format: `<type>(<scope>): <description>`. Types: `build`, `chore`, `ci`, `docs`, `feat`, `fix`, `perf`, `refactor`, `revert`, `style`, `test`. Description in imperative mood, lowercase, no period, under 72 chars. Add body only if the "why" isn't obvious; wrap body lines at 72 characters. Always sign-off on commits (`-s`). Only add a "Co-authored-by" trailer if a human was not in the loop or if the user requested it.

## Pull Requests

PR titles should be capitalized, imperative mood, no conventional commit prefixes (e.g. "Add login page" not "feat: add login page"). Prefix PR titles with the correct emoji based on the change type: 🎣 Bug fix, 🐋 New feature, 📜 Documentation, ✨ General improvement. Always use the repo's `.github/pull_request_template.md` — fill in each section from the commits/diff, replace HTML comment placeholders with actual content. For checklist items that can be resolved automatically (like emoji prefixes), mark them as complete. Use prose in summaries. Reference related issues (e.g. "Closes #123", "Ref #124"). Keep changes minimal and focused for quick review.

## Issues

Pick the matching template in `.github/ISSUE_TEMPLATE/` and fill in each of its sections, replacing HTML comment placeholders with actual content: `task.md` for work ready to be done (keep `Summary` and `Done when`), `bug_report.md` for a bug not yet understood, `feature_request.md` for a request with no plan yet (`gh issue create --template "<template name>"`, e.g. `"Task"`). Titles are capitalized, imperative mood, no period, no emoji (e.g. "Add staticcheck to the Go CI checks"). Search open and closed issues for duplicates before opening one (`gh issue list --state all --search "<terms>"`); if one exists, comment there instead. Apply labels only from the existing set (`gh label list`), e.g. `lang/go`, `lang/rust`, `lang/ts+react`, `helm`, `packaging`, `docs`, `ci/cd`, plus a size (`easy`, `medium`, `hard`) where it fits; never create new labels. Reference related issues and PRs (e.g. "Ref #123").

## Releases

Each component is released by a tag: `cli/v<semver>`, `dashboard/v<semver>`, `cluster-api/v<semver>`, `cluster-agent/v<semver>`. `release-cli.yml` builds and signs the CLI and opens a draft GitHub release; `release-docker.yml` builds the images; the `publish-*` workflows feed the package managers. The CLI's update check parses the `cli/v` tag (`modules/shared/versioncheck`), so the tag scheme is a contract.
