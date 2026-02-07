# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Kubetail is a real-time logging dashboard for Kubernetes that provides both browser and terminal interfaces for tailing logs across multi-container workloads. The source code is organized in a monorepo structure. The user-facing component of the application uses TypeScript+React for the frontend and Go for the backend. In addition, the application has in-cluster components that are written in Go and Rust.

## Architecture

### Core Components
- **CLI Tool** (`modules/cli/`) - Go-based command-line interface that embeds the dashboard UI
- **Dashboard Backend** (`modules/dashboard/`) - Go HTTP server with GraphQL API using Gin framework and gqlgen  
- **Dashboard Frontend** (`dashboard-ui/`) - React/TypeScript SPA with Apollo Client, Vite, and Tailwind CSS
- **Cluster API** (`modules/cluster-api/`) - Go-based GraphQL API server for cluster operations
- **Cluster Agent** (`crates/cluster_agent/`) - Rust-based agent that runs in Kubernetes clusters
- **Shared Libraries** (`modules/shared/`) - Common Go packages shared across components
- **Log Search Engine** (`crates/rgkl/`) - High-performance Rust binary for log searching and streaming

### Technology Stack
- **TypeScript/React**: Frontend with Vite, Tailwind CSS, Apollo Client, and Jotai state management
- **Go 1.24+**: Backend services using Go workspaces (`modules/go.work`)
- **Rust**: High-performance log processing in `crates/rgkl/`
- **GraphQL**: User-facing API layer uses gqlgen for Go backends with code generation
- **gRPC**: Inter-service API layer uses gRPC with code generation
- **Protocol Buffers**: Inter-service communication

### TypeScript/React Coding Style

- Use 2 spaces for indentation
- Use functional components with hooks
- Prefer TypeScript strict mode
- Use Apollo Client for GraphQL queries
- Follow existing patterns in `dashboard-ui/src/`

### Go Coding Style

- Follow standard Go formatting (`gofmt`)
- Use Go 1.24+ features appropriately
- Use Go workspaces (`modules/go.work`)
- Keep shared functionality in `modules/shared/`

### Rust Coding Style

- Follow Rust formatting (`cargo fmt`)
- Use Rust 2021 edition
- Focus on performance and safety
- Keep crates modular and well-documented

## Development Commands

### CLI Build Commands
```bash
# Full build (CLI with embedded dashboard UI)
make

# Clean build artifacts
make clean
```

### Frontend Development
```bash
cd dashboard-ui

# Install dependencies
pnpm install

# Start dev server
pnpm dev

# Build for production
pnpm build

# Run tests (single pass)
pnpm test run

# Lint code
pnpm lint

# Generate GraphQL types
pnpm graphql-codegen
```

### Go Development
```bash
# Start dashboard server (requires config)
cd modules/dashboard
go run cmd/main.go -c hack/config.yaml

# Run Go tests across all modules
cd modules
go test -race github.com/kubetail-org/kubetail/modules/...

# Test specific module
cd modules/<module-name>
go test ./...

# Lint code
cd modules
test -z $(gofmt -l .)

# Vet code
cd modules
go vet github.com/kubetail-org/kubetail/modules/...
```

### Rust Development
```bash
# Lint code
cd crates/rgkl
cargo fmt --all -- --check

# Vet code
cd crates/rgkl
cargo clippy --all -- -D warnings

# Run tests
cd crates/rgkl
cargo test

# Build for production
cd crates/rgkl
cargo build --release
```

## Testing Strategy

- **Frontend**: Vitest with jsdom, React Testing Library, mocked Apollo Client
- **Go**: Standard Go testing with race detection (`go test -race`)
- **Rust**: Cargo test with unit and integration tests

## Code Generation

Several components use code generation:
- **GraphQL schemas**: Use `gqlgen.yml` and `schema.graphqls` files in relevant modules
- **Protocol Buffers**: Defined in `proto/` directory
- **Frontend GraphQL types**: Use `pnpm graphql-codegen` in `dashboard-ui/`
- **Backend types**: Use `go generate github.com/kubetail-org/kubetail/modules/...` in `modules/`

## Dependencies

- Avoid introducing new external dependencies unless absolutely necessary
- If a new dependency is required, state the reason clearly
- For Go: Use standard library when possible
- For Rust: Prefer well-maintained, audited crates
- For TypeScript: Consider bundle size impact

## Development Patterns

- **Monorepo structure**: Each module has clear boundaries and responsibilities
- **GraphQL API design**: User-facing APIs use GraphQL with code generation
- **gRPC API design**: Inter-service APIs use gRPC with code generation
- **Shared libraries**: Common Go functionality in `modules/shared/`
- **Component-based React**: Hooks-based architecture with Jotai for state management

## Key Files

- `Makefile` - Main build orchestration
- `modules/go.work` - Go workspace configuration
- `dashboard-ui/package.json` - Frontend scripts and dependencies
- `crates/rgkl/Cargo.toml` - Rust project configuration
- `Tiltfile` - Local Kubernetes development setup
- `hack/config.yaml` - Example configuration
- `hack/manifests/` - Test manifests
- `hack/test-configs/` - Test configurations

## Pull Request Guidelines

When creating a pull request:

1. Reference any related issues at the top (e.g., "Fixes #123")
2. Include a clear summary of the changes
3. List specific changes made
4. Ensure all tests pass (TypeScript, Go, and Rust)
5. Verify linting passes for all modified components
6. Keep changes minimal and focused for quick review

## Important Context

- The project uses GraphQL for user-facing APIs and gRPC for inter-service communication
- The CLI tool embeds the dashboard UI for desktop usage
- Logs are fetched from Kubernetes API by default or from the Kubetail API (Cluster API + Cluster Agent) if the Kubetail cluster resources are installed
- The application tracks container lifecycle events to maintain log timeline accuracy
- Data never leaves the user's possession (private by default)
