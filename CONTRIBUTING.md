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

Kubetail is organized as a monorepo with one folder per component:

### Main Components

- **`/apps/web`** - React/TypeScript frontend

- **`/apps/desktop`** - Kubetail Desktop, a Tauri app
  - `src/` - React/TypeScript webview
  - `src-tauri/` - Rust native host (windows, tray, sidecar lifecycle)
  - `sidecar/` - Go sidecar (GraphQL API, Kubernetes and cloud logic, AI agents)
  - `proto/` - gRPC contract between the host and the sidecar
  - `docs/` - the desktop's design decisions (`adr/`) and security model

- **`/cli`** - CLI tool (Go)

- **`/dashboard`** - Dashboard backend (Go)

- **`/cluster`**
  - `api/` - Cluster API server (Go)
  - `agent/` - Cluster Agent, a Rust workspace: `cluster_agent/`, `rgkl/` (log reading library) and `types/` (shared types)

- **`/libs`**
  - `go-common/` - Shared Go packages
  - `proto/` - Protocol Buffer definitions
  - `graphql/` - GraphQL schemas

- **`/docs`** - Architecture Decision Records (`adr/`)

### Quick Reference

| Working on... | Go to |
|---------------|-------|
| CLI | `cli/` |
| Web UI | `apps/web/` |
| Backend | `dashboard/` or `cluster/api/` |
| Agent | `cluster/agent/cluster_agent/` |
| Desktop pages, components and hooks | `apps/desktop/src/` |
| Desktop windows, tray, menus | `apps/desktop/src-tauri/src/` |
| Desktop GraphQL API | `apps/desktop/sidecar/graph/` |
| Desktop clusters, logs, settings | `apps/desktop/sidecar/internal/services/` |
| Desktop AI agent, providers and tools | `apps/desktop/sidecar/internal/agent/`, `llm/`, `tools/` |
| Desktop command sandbox | `apps/desktop/sidecar/internal/run/` |

## How to Run Tests and Other Checks

Every check is a `make <verb>-<area>` target, run from the repo root. Run the checks of each area you changed before you open a pull request; CI runs every area on every platform. `make help` lists every target and is the authority if this table falls behind.

| Area | Paths | lint | vet | test | cover |
|---|---|---|---|---|---|
| `cli` | `cli/`, `dashboard/` | `lint-cli` | `vet-cli` | `test-cli` | `cover-cli-changed` |
| `libs` | `libs/go-common/` | `lint-libs` | `vet-libs` | `test-libs` | `cover-libs-changed` |
| `cluster-api` | `cluster/api/` | `lint-cluster-api` | `vet-cluster-api` | `test-cluster-api` | `cover-cluster-api-changed` |
| `cluster-agent` | `cluster/agent/` | `lint-cluster-agent` | `vet-cluster-agent` | `test-cluster-agent` | `cover-cluster-agent-changed` |
| `web` | `apps/web/` | `lint-web` | `vet-web` | `test-web` | `cover-web-changed` |
| `desktop` | `apps/desktop/` | `lint-desktop` | `vet-desktop` | `test-desktop` | `cover-desktop-changed` |
| `e2e` | `e2e/` | `lint-e2e` | | `test-e2e` | |
| `repo` | everything else | `lint-repo` | | `test-repo` | |

- **`cover-<area>-changed`** lists the lines your branch changed that no test reaches. Cover or mark each one: CI fails a pull request on them.
- **`lint-repo`** runs `lint-repo-sh`, `lint-repo-md`, `lint-repo-yaml`, `lint-repo-toolchains` and `lint-repo-digests`, which you can also run alone.
- **A change can reach another area.** The *Checks* section of the root [README](README.md#checks) lists those cases, and each component's README lists what to run while you work.
- **The targets need `make` and a POSIX shell.** On Windows, use Git Bash or WSL.
- **After editing a GraphQL schema or a `.proto` file**, `make generate` regenerates both sides; `make generate-desktop` does the desktop's alone. The desktop's [README](apps/desktop/README.md) has its toolchain and how to run it (`pnpm tauri dev`).

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
2. **Run the checks**: Run the *Checks* "before a PR" list in the README of each component you changed, and the extra checks the root [README](README.md#checks) lists for a change that reaches other components. Where a component's tests are measured, its list ends with `make cover-<area>-changed`, which lists the changed lines no test covers: cover or mark each one. CI runs every check on every platform
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
- **Oxc**: `oxc.oxc-vscode`, which runs oxlint and oxfmt for the dashboard UI and the desktop webview
- **Tauri**: `tauri-apps.tauri-vscode`, for the desktop app
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
git commit -a -s --trailer "Assisted-by: Claude:claude-opus-5-5" -m "<message>"
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
