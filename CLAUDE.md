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
- **pnpm**: Frontend package management

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

# Run tests
pnpm test

# Lint code
pnpm lint

# Generate GraphQL types
pnpm graphql-codegen
```

### Go Development
```bash
# Start dashboard server (requires config)
cd modules/dashboard
go run cmd/main.go -c ../../config/default/dashboard.yaml

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

## Development Patterns

- **Monorepo structure**: Each module has clear boundaries and responsibilities
- **GraphQL API design**: User-facing APIs use GraphQL with code generation
- **gRPC API design**: Inter-service APIs use gRPC with code generation
- **Shared libraries**: Common Go functionality in `modules/shared/`
- **Component-based React**: Hooks-based architecture with Recoil for performance-sensitive state management

## Key Files

- `Makefile` - Main build orchestration
- `modules/go.work` - Go workspace configuration
- `dashboard-ui/package.json` - Frontend scripts and dependencies
- `crates/rgkl/Cargo.toml` - Rust project configuration
