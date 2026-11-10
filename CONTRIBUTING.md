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
- [Automation](#bots-and-automation)
- [AI Policy](#ai-policy)
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

- **`/docs`** - Architecture Decision Records (`adr/`)

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

# Run linter and format check
pnpm lint

# Format code and sort imports
pnpm format

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

# From the repo root: vet for Windows too, check go fix, run staticcheck
make vet-go

# Run tests
go test -race github.com/kubetail-org/kubetail/modules/...
```

### Rust Crates

The Rust crates form one Cargo workspace in `crates/`. Run cargo from there, not from a crate's directory. Building needs `protoc` on your `PATH`.

```bash
cd crates

# Format check
cargo fmt --all -- --check

# Lint
cargo clippy --all --all-targets -- -D warnings

# Run tests
cargo test

# Build
cargo build --release
```

From the repo root, `make lint-rust`, `make vet-rust` and `make test-rust` run the same checks.

### Python (E2E Tests)

```bash
cd e2e

# Install dependencies (including dev tools)
uv sync --group dev

# Lint
uv run ruff check .
uv run ruff format --check .
```

## Commit Guidelines

Keep commits minimal and focused. Multiple commits in a PR are fine if they represent logical, well-separated steps that make the change easier to review.

### Format


We follow the [Conventional Commits](https://www.conventionalcommits.org) format:

```
<type>[optional scope]: commit title goes here (all lowercase)

[optional body]

Signed-off-by: Your Name <you@example.com>
```

**Types:**

- `build` - Changes that affect the build system or external dependencies (example scopes: gulp, broccoli, npm)
- `chore` - Routine maintenance or housekeeping changes (e.g., updating configs, dependencies, or scripts)
- `ci` - Changes to our CI configuration files and scripts
- `docs` - Documentation only changes
- `feat` - A new feature
- `fix` - A bug fix
- `perf` - A code change that improves performance
- `refactor` - A code change that neither fixes a bug nor adds a feature
- `revert` - Changes that restore old code or behavior
- `style` - Changes that do not affect the meaning of the code (white-space, formatting, missing semi-colons, etc)
- `test` - Adding missing tests or correcting existing tests

## Pull Request Guidelines

### Before Submitting

1. **Check for duplicates**: Review existing [issues](https://github.com/kubetail-org/kubetail/issues) and [pull requests](https://github.com/kubetail-org/kubetail/pulls)
2. **Run the checks**: Run `make check-changed` and fix what it reports. It runs the linters, the static analysis and the tests for the areas you changed, then lists the changed lines no test covers: cover or mark each one
3. **Clean commits**: Ensure each commit is minimal, focused, and follows our [commit format](#commit-guidelines)
4. **Update docs**: If you change architecture, conventions or commands, update the relevant `CLAUDE.md` in the same PR
5. **Record decisions**: A new architectural decision, or a change to one, gets an ADR in [`docs/adr/`](docs/adr/README.md)

### PR Title Format

Add an emoji to indicate the PR type:

- 🎣 Bug fix
- 🐋 New feature
- 📜 Documentation
- ✨ General improvement

### PR Description

Your PR should include:

- Link to related issues: `Fixes #123`
- **Summary**: Explain the goal of your PR
- **Key Changes**: List the specific key changes made

## Branch Naming Guidelines

Use descriptive branch names. As a suggestion, you can use this pattern:

```
<type>/<short-description>
```

## Editor Configuration

### AI-Assisted Editors

For AI-assisted editors like Claude Code, Codex, Cursor or GitHub Copilot, refer to the [`CLAUDE.md`](./CLAUDE.md) files (root and per area) for guidance on working with this codebase. `AGENTS.md` points to the same files.

The repo's `.claude/settings.json` runs `scripts/writing-standards-hook.sh` on every prompt in Claude Code. It needs `bash` and `jq` on your `PATH` (on Windows, Git Bash or WSL).

### Visual Studio Code

Recommended extensions (also listed in `.vscode/extensions.json`):
- **Oxc**: `oxc.oxc-vscode`, which runs oxlint and oxfmt for the dashboard UI
- **Rust**: `rust-lang.rust-analyzer`
- **Go**: `golang.go`

`.vscode/settings.json` formats on save with each language's formatter and turns Prettier off.

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

## AI Policy

As a contributor you're encouraged to use AI tools in your workflow just as you would use classic tools such as search engines, language servers, linters, debuggers, documentation, or books. These tools are an invaluable resource and can help you write better code and explore ideas more efficiently.

That said, AI tools are different than classic tools because they can blur the line between helping you to do the work and doing the work for you. And when that line becomes blurry, it can limit opportunities to build the deep understanding that comes from writing and reasoning through code yourself.

As an open source project, Kubetail is not only committed to building a great tool for Kubernetes operators but also to helping our contributors grow as engineers. We invest a lot of time and effort into code quality, thoughtful reviews, and well-defined engineering specs. We do so happily because we enjoy it but also because it's our responsibility to the community.

In return, we ask that contributions be authored by you. While AI tools can support your workflow, submitted code should reflect your own understanding and intent. To keep our focus on meaningful collaboration within the community, we do not accept contributions authored entirely by LLMs without a human in the loop.

Every commit an AI tool helped write carries an attribution trailer. Use either of these formats:

```
Assisted-by: AGENT_NAME:MODEL_VERSION
Co-authored-by: NAME <EMAIL>
```

Where:

- `AGENT_NAME` is the name of the AI tool or framework
- `MODEL_VERSION` is the specific model version used
- `NAME <EMAIL>` is the name and email the AI tool commits as

Example:

```
git commit -a -s --trailer "Assisted-by: Claude:claude-opus-4.6" -m "<message>"
```

## Community

We'd love to hear from you! Here's how to connect with the Kubetail community.

### Communication Channels

- **[Discord](https://discord.gg/CmsmWAVkvX)**: Join for real-time discussions, questions, and community chat
- **[Slack](https://kubernetes.slack.com/archives/C08SHG1GR37)**: Connect with us on the Kubernetes workspace

### Code of Conduct

Please read and follow our [Code of Conduct](https://github.com/kubetail-org/.github/blob/main/CODE_OF_CONDUCT.md). We are committed to providing a welcoming and inclusive environment for all contributors.

---

Thanks for contributing to Kubetail! 🐋
