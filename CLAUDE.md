# Kubetail

## Architecture

Monorepo with a TypeScript+React frontend, Go backends, and Rust in-cluster components:

- **CLI** (`modules/cli/`) — Go CLI that embeds the dashboard UI
- **Dashboard Backend** (`modules/dashboard/`) — Go/Gin HTTP server with GraphQL API (gqlgen)
- **Dashboard Frontend** (`dashboard-ui/`) — React/Vite SPA with Apollo Client, Tailwind CSS, Jotai
- **Cluster API** (`modules/cluster-api/`) — Go GraphQL API server for cluster operations
- **Cluster Agent** (`crates/cluster_agent/`) — Rust agent running in Kubernetes clusters
- **Log Search Engine** (`crates/rgkl/`) — High-performance Rust binary for log searching/streaming
- **Shared Libraries** (`modules/shared/`) — Common Go packages shared across services

GraphQL for user-facing APIs, gRPC + Protocol Buffers for inter-service communication.

The Cluster API is exposed to the Dashboard as a Kubernetes APIService (aggregation layer): the Dashboard's `/cluster-api-proxy/*` route forwards to the kube-apiserver, which routes to the cluster-api Service via the registered APIService. This applies in both desktop and in-cluster modes — the Dashboard never dials the cluster-api directly. Authentication is handled by kube-apiserver (user's kubeconfig credentials in desktop mode; the Dashboard's ServiceAccount, or a session bearer token if set, in-cluster). The TLS leg is kube-apiserver ↔ cluster-api, configured via the APIService and the `kubetail-cluster-api-tls` / `kubetail-ca` secrets.

## Project Structure

```
dashboard-ui/         — React/Vite frontend (pnpm)
modules/cli/          — CLI Go module
modules/dashboard/    — Dashboard Go backend
modules/cluster-api/  — Cluster API Go backend
modules/shared/       — Shared Go libraries
modules/go.work       — Go workspace config
crates/cluster_agent/ — Rust cluster agent
crates/rgkl/          — Rust log search engine
proto/                — Protocol Buffer definitions
config/default/       — Default config files (cli, dashboard, cluster-api, cluster-agent)
hack/manifests/       — Test manifests
hack/test-configs/    — Test configurations
hack/tilt             - Tilt configurations
Makefile              — Build orchestration
Tiltfile              — Local Kubernetes dev setup
```

Frontend builds are embedded into Go binaries via `embed.go`.

## Local Development

```sh
# Tilt (all infra + services)
tilt up

# Frontend dev server
cd dashboard-ui && pnpm install && pnpm dev

# Dashboard backend
cd modules/dashboard && go run cmd/main.go -c hack/config.yaml

# Full CLI build (with embedded dashboard UI)
make
```

## Testing

```sh
# Frontend tests (single pass):
cd dashboard-ui && pnpm test run

# Go tests (all modules):
cd modules && go test -race github.com/kubetail-org/kubetail/modules/...

# Go tests (single module):
cd modules/<module-name> && go test ./...

# Rust tests:
cd crates/rgkl && cargo test
```

Always use `pnpm` (not `npx`) to run frontend tests.

### E2E tests

E2E tests live in `e2e/` and require [kind](https://kind.sigs.k8s.io), Docker, kubectl, and uv.

```sh
# Full suite (builds images + CLI, then runs pytest):
make test-e2e

# Target a specific Kubernetes version:
KIND_IMAGE=kindest/node:v1.21.14 make test-e2e
KIND_IMAGE=kindest/node:v1.25.16 make test-e2e

# Re-run pytest without rebuilding (cluster must already be up):
cd e2e && uv run pytest -v

# Bring up / tear down the cluster manually:
./e2e/scripts/up.sh --backend=kubetail-api   # or --backend=kubernetes-api
./e2e/scripts/down.sh
```

`down.sh` stops port-forwards, deletes the kind cluster, and removes `/tmp/kubetail-e2e.kubeconfig`. Run it to clean up after an interrupted test run.

Never use `time.Sleep` (Go) or `setTimeout`/manual delays (TypeScript) in tests to wait for asynchronous state. Use channels, synchronization primitives, or condition-based polling instead.

## Import Order (JavaScript/TypeScript)

Organize imports into three groups separated by blank lines, sorted alphabetically by path within each group:

1. **Third-party** — packages from `node_modules` (e.g. `react`, `@apollo/client`, `jotai`)
2. **First-party packages** — self-authored packages from `node_modules` (e.g. `@kubetail/*`)
3. **Local** — relative imports (e.g. `@/*`, `./*`)

## JavaScript/TypeScript Linting

After every set of changes to JavaScript/TypeScript files, run `pnpm lint --fix` inside the affected package directory:

```sh
cd dashboard-ui && pnpm lint --fix
```

## Go Formatting

After every set of changes to Go files, run `go fmt ./...` inside each affected module directory:

```sh
cd modules/dashboard && go fmt ./...
cd modules/cluster-api && go fmt ./...
cd modules/shared && go fmt ./...
cd modules/cli && go fmt ./...
```

## Rust Formatting

After every set of changes to Rust files:

```sh
cd crates/rgkl && cargo fmt --all
cd crates/rgkl && cargo clippy --all -- -D warnings
```

## Code Generation

- **GraphQL schemas**: `gqlgen.yml` and `schema.graphqls` in relevant modules
- **Protocol Buffers**: Defined in `proto/`
- **Frontend GraphQL types**: `cd dashboard-ui && pnpm graphql-codegen`
- **Backend types**: `cd modules && go generate github.com/kubetail-org/kubetail/modules/...`

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
