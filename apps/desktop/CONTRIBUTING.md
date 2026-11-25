# Contributing to Kubetail

Thank you for your interest in contributing to Kubetail! We're building a secure desktop command center for monitoring, troubleshooting and auditing Kubernetes clusters with the help of AI agents, and we'd love your help.

This document will guide you through the contribution process.

## Table of Contents

- [Where to Find Code](#where-to-find-code)
- [How to Run Tests and Other Checks](#how-to-run-tests-and-other-checks)
- [Commit Guidelines](#commit-guidelines)
- [Pull Request Guidelines](#pull-request-guidelines)
- [Issues and Specs](#issues-and-specs)
- [Branch Naming Guidelines](#branch-naming-guidelines)
- [Editor Configuration](#editor-configuration)
- [Automation](#bots-and-automation)
- [AI Policy](#ai-policy)
- [Community](#community)

## Where to Find Code

Kubetail is a Tauri desktop app organized as a monorepo with components in different languages:

### Main Components

- **`/src`** - React/TypeScript webview (the UI)

- **`/src-tauri`** - Rust native host (windows, tray, sidecar lifecycle)

- **`/sidecar`** - Go sidecar (GraphQL API, Kubernetes and cloud logic, AI agents)

- **`/proto`** - gRPC contract between the host and the sidecar

- **`/docs`** - Design decisions (`adr/`), specs, and the security model

Each area has its own `CLAUDE.md` with its conventions. The root [`CLAUDE.md`](./CLAUDE.md) covers what crosses them.

### Quick Reference

| Working on... | Go to |
|---------------|-------|
| Pages and routing | `src/routes/` |
| UI components | `src/components/` |
| UI state and data hooks | `src/lib/` |
| Windows, tray, menus | `src-tauri/src/` |
| Starting and talking to the sidecar | `src-tauri/src/services/sidecar/` |
| GraphQL API | `sidecar/graph/` (see below after editing the schema) |
| Clusters, kubeconfig, logs, settings | `sidecar/internal/services/` |
| AI agent loop and chats | `sidecar/internal/agent/` |
| AI providers and models | `sidecar/internal/llm/` |
| Agent tools | `sidecar/internal/tools/` |
| Command sandbox and permissions | `sidecar/internal/run/` |
| Host↔sidecar messages | `proto/` (run `make generate-proto` after editing) |

`sidecar/graph/schema.graphqls` defines the GraphQL API for both the sidecar and the UI. After editing it, regenerate both sides:

```bash
cd sidecar && go tool gqlgen generate
cd .. && pnpm codegen
```

## How to Run Tests and Other Checks

See the [README](./README.md#development) for the toolchain and how to run the app (`pnpm tauri dev`).

The `Makefile` is the entry point for every language:

```bash
# Tests touched by your branch (run this while working)
make test-changed

# Before a pull request: the linters, static analysis and tests for what your
# branch changed, then the changed lines no test covers
make check-changed

# All tests
make test

# Formatters and linters (gofmt, cargo fmt, oxlint + oxfmt)
make lint

# Static analysis (go vet, staticcheck, go fix, cargo clippy)
make vet

# Regenerate gRPC bindings after editing proto/
make generate-proto
```

`make check` runs every lint, vet and test; `make help` lists every target.

CI runs every suite and uploads coverage to Codecov. `codecov/patch` lists, in the PR's diff, every changed line no test reached: cover each, or mark it with its reason (`// coverage:ignore <reason>` in Go, `/* v8 ignore next -- <reason> */` in TypeScript).

To fix TypeScript formatting, run `pnpm format`.

## Commit Guidelines

Keep commits minimal and focused. Multiple commits in a PR are encouraged if they represent logical, well-separated steps that make the overall change easier to review.

### Format

We follow the [Conventional Commits](https://www.conventionalcommits.org) format:

```
<type>[optional scope]: commit title goes here (all lowercase)

[optional body]

Signed-off-by: Your Name <you@example.com>
```

The subject is imperative, has no trailing period, and is at most 72 characters. Add a scope only when it adds information (`feat(logs): ...`). Add a body only when the why isn't obvious.

**Types:**

- `build` - Changes that affect the build system or external dependencies
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
2. **Run the checks**: Run `make check-changed` and fix what it reports. It runs the linters, the static analysis and the tests for the areas you changed, then lists the changed lines no test covers: cover or mark each one. It checks your machine's platform; CI checks the others on the PR (`ALL_PLATFORMS=1 make check-changed` checks them locally)
3. **Clean commits**: Ensure each commit is minimal, focused, and follows our [commit format](#commit-guidelines)
4. **Update docs**: If you change architecture, conventions or commands, update the relevant `CLAUDE.md` in the same PR

### PR Title Format

Write a natural, capitalized title and lead it with an emoji for the PR type:

- 🎣 Bug fix
- 🐋 New feature
- 📜 Documentation
- ✨ General improvement

### PR Description

Follow the [PR template](./.github/pull_request_template.md). Your PR should include:

- Link to related issues: `Fixes #123`
- **Summary**: Explain the goal of your PR (why)
- **Key Changes**: List the specific key changes made (what)

## Issues and Specs

- **Work to be done**: use the [task template](./.github/ISSUE_TEMPLATE/task.md)
- **A bug**: use the [bug report template](./.github/ISSUE_TEMPLATE/bug_report.md)
- **A feature idea**: use the [feature request template](./.github/ISSUE_TEMPLATE/feature_request.md)

Larger changes start as a spec. Open it as a PR titled `📜 Spec: …`, with `gh pr create --template spec.md`. See [`docs/specs/README.md`](./docs/specs/README.md) for how a spec moves to implementation.

## Branch Naming Guidelines

Use descriptive branch names. As a suggestion, you can use this pattern:

```
<type>/<short-description>
```

You can also use the auto-generated branch names that GitHub provides for each Issue.

## Editor Configuration

### AI-Assisted Editors

For AI-assisted editors like Claude Code, Codex, Cursor or GitHub Copilot, refer to the [`CLAUDE.md`](./CLAUDE.md) files (root and per area) for guidance on working with this codebase. `AGENTS.md` points to the same files.

### Visual Studio Code

Recommended extensions (also listed in `.vscode/extensions.json`):
- **Oxc**: `oxc.oxc-vscode`
- **Tauri**: `tauri-apps.tauri-vscode`
- **Rust**: `rust-lang.rust-analyzer`
- **Go**: `golang.go`

## Bots and Automation

### GitHub Actions

Our CI pipeline automatically runs on every pull request:

- **Tests**: All test suites across Go, Rust, and TypeScript, plus the coverage gates
- **Linting**: Code formatting and style checks, including shellcheck
- **Vetting**: `go vet`, staticcheck, `go fix -diff` and `cargo clippy`
- **Security**: the dependency reviews (`Review dependencies · Go + Rust` and `Review dependencies · TypeScript`), which run when your PR changes a dependency and fail only on advisories it brings in, and CodeQL. The full dependency audits (govulncheck, cargo audit, pnpm audit) run daily on `main`, so an advisory against a dependency you did not touch never blocks your PR

You can see the status of these checks in your PR. If any checks fail, review the logs and fix the issues before requesting a review.

## AI Policy

As a contributor you're encouraged to use AI tools in your workflow just as you would use classic tools such as search engines, language servers, linters, debuggers, documentation, or books. These tools are an invaluable resource and can help you write better code and explore ideas more efficiently.

That said, AI tools are different than classic tools because they can blur the line between helping you to do the work and doing the work for you. And when that line becomes blurry, it can limit opportunities to build the deep understanding that comes from writing and reasoning through code yourself.

As an open source project, Kubetail is not only committed to building a great tool for Kubernetes operators but also to helping our contributors grow as engineers. We invest a lot of time and effort into code quality, thoughtful reviews, and well-defined engineering specs. We do so happily because we enjoy it but also because it's our responsibility to the community.

In return, we ask that contributions be authored by you. While AI tools can support your workflow, submitted code should reflect your own understanding and intent. To keep our focus on meaningful collaboration within the community, we do not accept contributions authored entirely by LLMs without a human in the loop.

When you use an AI tool, include an Assisted-by tag in the following format:

```
Assisted-by: AGENT_NAME:MODEL_VERSION
```

Where:

- `AGENT_NAME` is the name of the AI tool or framework
- `MODEL_VERSION` is the specific model version used

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

Thanks for contributing to Kubetail! 🥞
