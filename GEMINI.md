# Project: Kubetail

Kubetail is a real-time logging dashboard for Kubernetes that provides both browser and terminal interfaces for tailing logs across multi-container workloads. This is a monorepo containing TypeScript/React frontend, Go backend services, and Rust components.

## General Instructions

- Follow the existing code style in each file
- Add comments for complex logic
- Use meaningful variable and function names
- Ensure all tests pass before submitting changes
- Reference related issues in pull requests
- Keep changes minimal and focused for quick merging

## Monorepo Structure

This project is organized as a monorepo with the following components:

- `/crates`: Rust crates for high-performance components
  - `/cluster_agent`: Cluster Agent (Rust)
  - `/rgkl`: Log search engine (Rust)
- `/dashboard-ui`: Dashboard frontend (TypeScript/React)
- `/modules`: Go modules using Go workspaces
  - `/cli`: CLI tool (Go)
  - `/cluster-api`: Cluster API server (Go)
  - `/dashboard`: Dashboard backend (Go)
  - `/shared`: Shared Go packages
- `/proto`: Protocol Buffer definitions for gRPC

## Technology Stack

- **Frontend**: TypeScript, React, Vite, Tailwind CSS, Apollo Client, Jotai
- **Backend**: Go 1.24+, Gin framework, gqlgen (GraphQL)
- **High-Performance Components**: Rust with Cargo
- **API Layers**: GraphQL (user-facing), gRPC (inter-service)
- **Package Management**: pnpm (frontend), Go modules, Cargo

## TypeScript/React Coding Style

- Use 2 spaces for indentation
- Use functional components with hooks
- Prefer TypeScript strict mode
- Use Apollo Client for GraphQL queries
- Follow existing patterns in `dashboard-ui/src/`
- Run `pnpm lint` before committing
- Ensure tests pass with `pnpm test run`
- Generate GraphQL types with `pnpm graphql-codegen` after schema changes

## Go Coding Style

- Follow standard Go formatting (`gofmt`)
- Use Go 1.24+ features appropriately
- Organize code in modules under `modules/` directory
- Use Go workspaces (`modules/go.work`)
- Run `go vet` to catch potential issues
- Run tests with race detection: `go test -race ./...`
- Use gqlgen for GraphQL code generation
- Keep shared functionality in `modules/shared/`

## Rust Coding Style

- Follow Rust formatting (`cargo fmt`)
- Use Rust 2021 edition
- Run `cargo clippy -- -D warnings` to catch issues
- Ensure all tests pass with `cargo test`
- Focus on performance and safety
- Keep crates modular and well-documented

## Testing Requirements

### Frontend Tests
```bash
cd dashboard-ui
pnpm test run
```

### Go Tests
```bash
# Test all modules
cd modules
go test -race github.com/kubetail-org/kubetail/modules/...

# Test specific module
cd modules/<module-name>
go test -race ./...
```

### Rust Tests
```bash
cd crates/<crate-name>
cargo test
```

## Linting and Verification

### TypeScript/React
```bash
cd dashboard-ui
pnpm lint
```

### Go
```bash
# Format check
cd modules
test -z $(gofmt -l .)

# Vet check
cd modules
go vet github.com/kubetail-org/kubetail/modules/...
```

### Rust
```bash
cd crates/<crate-name>
cargo fmt --all -- --check
cargo clippy --all -- -D warnings
```

## Building

### Full Build (CLI with embedded dashboard)
```bash
make
```

### Frontend Build
```bash
cd dashboard-ui
pnpm build
```

### Go Build
```bash
cd modules/<module-name>
go build ./...
```

### Rust Build
```bash
cd crates/<crate-name>
cargo build --release
```

## Code Generation

This project uses code generation for several components:

- **GraphQL schemas**: Use gqlgen with `gqlgen.yml` and `schema.graphqls` files
- **Protocol Buffers**: Defined in `proto/` directory
- **Frontend GraphQL types**: Run `pnpm graphql-codegen` in `dashboard-ui/`
- **Backend types**: Run `go generate github.com/kubetail-org/kubetail/modules/...` in `modules/`

After modifying GraphQL schemas or Protocol Buffers, regenerate types before committing.

## Regarding Dependencies

- Avoid introducing new external dependencies unless absolutely necessary
- If a new dependency is required, state the reason clearly
- For Go: Use standard library when possible
- For Rust: Prefer well-maintained, audited crates
- For TypeScript: Consider bundle size impact

## Pull Request Guidelines

When creating a pull request:

1. Reference any related issues at the top (e.g., "Fixes #123")
2. Include a clear summary of the changes
3. List specific changes made
4. Ensure all tests pass (TypeScript, Go, and Rust)
5. Verify linting passes for all modified components
6. Keep changes minimal and focused for quick review

## Development Environment

- Use the provided `Makefile` for build orchestration
- `Tiltfile` is available for local Kubernetes development
- Configuration examples are in `hack/` directory
- Test manifests are in `hack/manifests/` and `hack/test-configs/`

## Important Context

- The project uses GraphQL for user-facing APIs and gRPC for inter-service communication
- The CLI tool embeds the dashboard UI for desktop usage
- Logs are fetched directly from Kubernetes API (no external log forwarding required)
- The application tracks container lifecycle events to maintain log timeline accuracy
- Data never leaves the user's possession (private by default)
