# Kubetail

_Kubetail is a desktop app for monitoring and troubleshooting K8s clusters with help of AI_

<a href="https://www.loom.com/share/41ea1133c96b4dcebc10600ec75d8277">
  <img width="350" alt="video-thumbnail" src="https://github.com/user-attachments/assets/6d7f056f-fe8f-40d7-98f4-c345eb77d0aa" />
</a>

<a href="https://discord.gg/CmsmWAVkvX"><img src="https://img.shields.io/discord/1212031524216770650?logo=Discord&style=flat-square&logoColor=FFFFFF&labelColor=5B65F0&label=Discord&color=64B73A"></a>
[![Slack](https://img.shields.io/badge/Slack-kubetail-364954?logo=slack&labelColor=4D1C51)](https://kubernetes.slack.com/archives/C08SHG1GR37)
[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](https://github.com/kubetail-org/.github/blob/main/CODE_OF_CONDUCT.md)
[![Contributor Resources](https://img.shields.io/badge/Contributor%20Resources-purple?style=flat-square)](https://github.com/kubetail-org)

## Introduction

<img width="150" alt="kubetail" src="https://github.com/user-attachments/assets/8540a80b-cb5f-42e6-8176-f41983b44676" />

<br>
<br>

**Kubetail** is a desktop app that provides a secure command center for monitoring, troubleshooting and auditing your K8s clusters with the help of AI agents. With Kubetail, you can use AI agents running in multiple tabs to manage clusters, view logs and debug issues in a sandboxed environment that redacts secrets and withholds credentials from the AI models. Kubetail supports all popular AI providers including local models and gives you full control over what the agents can do on your cluster. 

> [!IMPORTANT]
> Kubetail is currently under development and this is a developer-only preview of the app.

What changed in each release: [CHANGELOG.md](CHANGELOG.md).

# Development

## Requirements

- [Rust](https://rustup.rs/) 1.98.1
- [Go](https://go.dev/dl/) 1.26
- [Node](https://nodejs.org/en/download) 24
- [pnpm](https://pnpm.io/installation) 12.4.1 (`npm install -g pnpm@12.4.1`)
- [uv](https://docs.astral.sh/uv/getting-started/installation/), for `make lint`'s YAML linters
- curl, for `make lint-md`, which downloads a pinned [lychee](https://github.com/lycheeverse/lychee) when the one on PATH is not 0.24.2
- curl and unzip, for `make generate-proto`, which downloads a pinned [protoc](https://github.com/protocolbuffers/protobuf/releases) when the one on PATH is not 36.0
- curl, for `make cover-changed` when `src-tauri/` or `proto/` changed, which downloads a pinned [cargo-llvm-cov](https://github.com/taiki-e/cargo-llvm-cov) when the one on PATH is not 0.9.1; never `cargo install` it

On Linux:

```console
sudo apt-get install -y build-essential libwebkit2gtk-4.1-dev \
  libayatana-appindicator3-dev librsvg2-dev
```

For the agent's command sandbox and its tests:

```console
sudo apt-get install -y bubblewrap passt
```

### VS Code

Install the workspace's recommended extensions when prompted. They provide the repo's formatters and linters plus Go, Rust, Tailwind, GraphQL, Vitest and Tauri support.

## Run

```console
pnpm tauri dev
```

## Commands

```console
make help           # every target
make test-changed   # tests touched by your branch
make check-changed  # before a pull request: the checks for what your branch changed
make check          # every lint, vet and test
make generate-proto # after editing proto/
make clean-data     # wipe the dev app's data
```

## Read next

- `CLAUDE.md` (root and per area) — conventions
- `docs/adr/` — design decisions
- `docs/security-model.md`
