# Agents

This document provides comprehensive guidance for AI agents working with this codebase.

## Monorepo Layout

- `/crates`: Rust crates
  - `/cluster_agent`: Cluster Agent
  - `/rgkl`: Log search engine for Kubernetes log files
- `/dashboard-ui`: Dashboard frontend (TypeScript/React)
- `/modules`: Go modules
  - `/cli`: CLI
  - `/cluster-api`: Cluster API
  - `/dashboard`: Dashboard backend
  - `/shared`: Shared Go packages
- `/proto`: ProtoBuffer definitions

### General Conventions

- Follow the existing code style in each file/package
- Prefer small, reviewable changes with targeted tests
- Use meaningful variable and function names

## Running TypeScript Checks

### Lint

To run linter for the Dashboard frontend:

```bash
cd dashboard-ui
pnpm lint
```

### Test

To run tests for the Dashboard frontend:

```bash
cd dashboard-ui
pnpm test run
```

## Build

To build the Dashboard frontend:

```bash
cd dashboard-ui
pnpm build
```

## Running Go Checks

### Lint

To run format checker for all modules:

```bash
cd modules
test -z $(gofmt -l .)
```

### Vet

To vet a specific module:

```bash
cd modules/<module-name>
go vet ./...
```

To vet all modules:

```bash
cd modules
go vet github.com/kubetail-org/kubetail/modules/...
```

### Test

To run tests for a specific module:

```bash
cd modules/<module-name>
go test -race ./...
```

To run all tests:

```bash
cd modules
go test -race github.com/kubetail-org/kubetail/modules/...
```

## Running Rust Checks

### Lint

To lint a specific crate:

```bash
cd crates/<crate-name>
cargo fmt --all -- --check
```

### Vet

To vet a specific crate:

```bash
cd crates/<crate-name>
cargo clippy --all -- -D warnings
```

### Test

To run tests for a specific crate:

```bash
cd crates/<crate-name>
cargo test
```

### Build

To build a specific crate:

```bash
cd crates/<crate-name>
cargo build --release
```

## Pull Request Guidelines

When the agent helps create a PR, please ensure it:

1. References any related issues at the top of the PR comment
2. Includes a summary of the PR
3. Includes a list of the changes made
4. Ensures all tests pass
