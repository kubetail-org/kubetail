# Contributing to Kubetail

Thank you for your interest in contributing to Kubetail! We're building the most user-friendly, cost-effective, and secure logging platform for Kubernetes, and we'd love your help.

This document will guide you through the contribution process.

## Table of Contents

- [Where to Find Code](#where-to-find-code)
- [How to Run Tests and Other Checks](#how-to-run-tests-and-other-checks)
- [Commit Guidelines](#commit-guidelines)
- [Pull Request Guidelines](#pull-request-guidelines)
- [Branch Naming Guidelines](#branch-naming-guidelines)
- [Editor Configuration](#editor-configuration)
- [Bots and Automation](#bots-and-automation)
- [Community](#community)

## Where to Find Code

Kubetail is organized as a monorepo with components in different languages:

### Main Components

- **`/crates`** - Rust crates
  - `cluster_agent/` - Cluster Agent
  - `rgkl/` - Log reading library
  - `types/` - Shared types
  
- **`/modules`** - Go modules
  - `cli/` - CLI tool
  - `cluster-api/` - Cluster API server
  - `dashboard/` - Dashboard backend
  - `shared/` - Shared packages
  
- **`/dashboard-ui`** - React/TypeScript frontend
  
- **`/proto`** - Protocol Buffer definitions

### Quick Reference

| Working on... | Go to |
|---------------|-------|
| CLI | `modules/cli/` |
| Web UI | `dashboard-ui/` |
| Backend | `modules/dashboard/` or `modules/cluster-api/` |
| Agent | `crates/cluster_agent/` |

## How to Run Tests and Other Checks

Make sure your changes pass all tests and checks before submitting a pull request.

### TypeScript/React (Dashboard UI)

```bash
cd dashboard-ui

# Install dependencies 
pnpm install

# Run linter
pnpm lint

# Run tests
pnpm test run

# Build check
pnpm build
```

### Go Modules

```bash
cd modules

# Format check
test -z $(gofmt -l .)

# Format check (Windows PowerShell)
$output = gofmt -l .; if ($output) { throw "Files need formatting: $output" }

# Vet (static analysis)
go vet github.com/kubetail-org/kubetail/modules/...

# Run tests
go test -race github.com/kubetail-org/kubetail/modules/...
```

### Rust Crates

```bash
cd crates/<crate-name>

# Format check
cargo fmt --all -- --check

# Lint
cargo clippy --all -- -D warnings

# Run tests
cargo test

# Build
cargo build --release
```

## Commit Guidelines

All commits must be squashed into a single, signed commit before merging.

### Format

We follow a specific commit format. See the [Pull Request Commit Format](https://github.com/kubetail-org/.github/blob/main/pull-request-commit-format.md) for detailed guidelines.

Quick reference:

```
<type>: commit title goes here (all lowercase)

* <type>: Main change 1
* <type>: Main change 2

Signed-off-by: Your Name <you@example.com>
```

**Types:**
- `new` - New features or capabilities
- `fix` - Bug fixes
- `doc` - Documentation changes
- `test` - Test additions or modifications
- `ref` - Code refactoring (no functional changes)
- `chore` - Maintenance tasks, dependency updates
- `wip` - Work in progress (use sparingly)

## Pull Request Guidelines

### Before Submitting

1. **Check for duplicates**: Review existing [issues](https://github.com/kubetail-org/kubetail/issues) and [pull requests](https://github.com/kubetail-org/kubetail/pulls)
2. **Run tests**: Execute all relevant tests for your changes and ensure they pass
3. **Format code**: Run formatters for the languages you modified
4. **Update branch**: Rebase your branch to the latest `main`
5. **Squash commits**: Combine all commits into a single, signed commit following our [commit format](https://github.com/kubetail-org/.github/blob/main/pull-request-commit-format.md)

### PR Title Format

Add an emoji to indicate the PR type:

- 🎣 Bug fix
- 🐋 New feature
- 📜 Documentation
- ✨ General improvement

### PR Description

Your PR should include:

- Link to related issue: `Fixes #123`
- **Summary**: Explain the goal of your PR
- **Changes**: List the specific changes made

### PR Checklist

- [ ] Add the correct emoji to the PR title
- [ ] Link the issue number with `Fixes #`
- [ ] Add summary and explain changes
- [ ] Rebase branch to HEAD
- [ ] Squash changes into one signed commit

## Branch Naming Guidelines

Use descriptive branch names with this pattern:

```
<type>/<short-description>
```

### Types

- **feat/** - New features
- **fix/** - Bug fixes
- **docs/** - Documentation changes
- **refactor/** - Code refactoring
- **test/** - Test additions or changes
- **chore/** - Maintenance tasks

## Editor Configuration

### AI-Assisted Editors

For AI-assisted editors like Cursor or GitHub Copilot, refer to the [`AGENTS.md`](./AGENTS.md) file for comprehensive guidance on working with this codebase.

### Visual Studio Code

Recommended extensions:
- **Go**: `golang.go`
- **Rust**: `rust-lang.rust-analyzer`
- **ESLint**: `dbaeumer.vscode-eslint`
- **Prettier**: `esbenp.prettier-vscode`

## Bots and Automation

### GitHub Actions

Our CI/CD pipeline automatically runs on every pull request:

- **Tests**: All unit and integration tests across Go, Rust, and TypeScript
- **Linting**: Code formatting and style checks
- **Build**: Ensures all components build successfully
- **Security**: Vulnerability scanning

You can see the status of these checks in your PR. If any checks fail, review the logs and fix the issues before requesting a review.

### CLA Assistant

If this is your first contribution, our [CLA (Contributor License Agreement)](https://cla-assistant.io/) assistant will prompt you to sign the CLA when you create your pull request. This is a one-time requirement.

## Community

We'd love to hear from you! Here's how to connect with the Kubetail community.

### Communication Channels

- **[Discord](https://discord.gg/CmsmWAVkvX)**: Join for real-time discussions, questions, and community chat
- **[Slack](https://kubernetes.slack.com/archives/C08SHG1GR37)**: Connect with us on the Kubernetes workspace

### Code of Conduct

Please read and follow our [Code of Conduct](https://github.com/kubetail-org/.github/blob/main/CODE_OF_CONDUCT.md). We are committed to providing a welcoming and inclusive environment for all contributors.

---

Thank you for contributing to Kubetail! 🚀


