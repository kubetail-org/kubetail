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

## Checks

Run the checks in the guide of every area you touched before you finish. For `scripts/`: `shellcheck scripts/*.sh` and `bats scripts/`.

- **`make lint-all`, `vet-all`, `test-all` and `ci-checks` wrap most of them**, with gaps: `make crates-test` covers `rgkl` alone, and no target runs `pnpm build`, the Python lint or the shell checks. `make test-changed`, `make cover-changed` and `make lint`, named in the PR template and `CONTRIBUTING.md`, do not exist.
- **CI picks its jobs by path**: `modules/**` runs the Go jobs, `crates/**` the Rust jobs, `dashboard-ui/**` the TypeScript jobs. A change to `proto/`, `config/`, `build/`, `hack/` or `e2e/` alone runs none of them, and a schema change under `modules/` does not run the TypeScript jobs. Run the affected checks yourself.
- **The Docker builds, the CLI builds and the end-to-end suite run only when one PR touches `modules/`, `crates/` and `dashboard-ui/` together.** For a change that crosses a component boundary, run `make test-e2e` yourself (`e2e/CLAUDE.md`) and say so in the PR.
- **`make lint-yaml` lints every YAML file, then the workflows with `actionlint` and `zizmor`** (`.github/ci-config/zizmor.yml`). It needs `uv`, Go and `shellcheck`, and CI's `YAML · Lint` job runs it on every PR. Pin a new action to a commit SHA with its exact version in a comment (`# v7.0.1`), and pass `${{ }}` values to a `run:` script through `env:`.

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

## Commits, pull requests, issues, releases

- **Commits** are [conventional](https://www.conventionalcommits.org/): `<type>(<scope>): <description>`, the description in imperative mood, lowercase, no period, under 72 characters. Types: `build`, `chore`, `ci`, `docs`, `feat`, `fix`, `perf`, `refactor`, `revert`, `style`, `test`. Add a body only if the why isn't obvious, wrapped at 72. Always sign off (`-s`). Add `Co-authored-by` only if no human was in the loop or the user asked. Keep each commit minimal and focused; several are fine when they are well-separated steps.
- **A pull request** fills in `.github/pull_request_template.md` from the diff, in prose, with the placeholders replaced and the checklist items you can settle ticked. Its title is capitalized, in imperative mood, with no conventional-commit prefix, and leads with the emoji the template lists (🎣 bug fix, 🐋 new feature, 📜 documentation, ✨ general improvement). Reference related issues (`Closes #123`, `Ref #124`).
- **An issue** fills in a template from `.github/ISSUE_TEMPLATE/` (`gh issue create --template "Task"`): `task.md` for work ready to be done (keep `Summary` and `Done when`), `bug_report.md` for a bug not yet understood, `feature_request.md` for a request with no plan yet. Its title is capitalized, in imperative mood, with no period and no emoji. Search open and closed issues first (`gh issue list --state all --search "<terms>"`) and comment on a duplicate instead. Apply labels only from the existing set (`gh label list`); never create one.
- **Each component is released by a tag**: `cli/v<semver>`, `dashboard/v<semver>`, `cluster-api/v<semver>`, `cluster-agent/v<semver>`. The release workflows and the CLI's update check parse it, so the scheme is a contract. A person cuts a release with the `release-cli` or `release-cluster` skill; never tag or push a tag yourself. → [ADR: release each component by its own tag](docs/adr/2026-11-09-release-each-component-by-its-own-tag.md)
