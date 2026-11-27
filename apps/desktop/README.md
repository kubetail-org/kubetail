# Kubetail Desktop

_A desktop app for monitoring and troubleshooting Kubernetes clusters with the help of AI agents._

<a href="https://www.loom.com/share/41ea1133c96b4dcebc10600ec75d8277">
  <img width="350" alt="video-thumbnail" src="https://github.com/user-attachments/assets/6d7f056f-fe8f-40d7-98f4-c345eb77d0aa" />
</a>

Kubetail Desktop is a command center for monitoring, troubleshooting and auditing your clusters. AI agents in several tabs manage clusters, read logs and debug issues in a sandbox that redacts secrets and withholds credentials from the models. It works with every popular AI provider, local models included, and you decide what the agents may do on your cluster.

> [!IMPORTANT]
> Kubetail Desktop is under development, and this is a developer-only preview.

Its releases are tagged `desktop/v<semver>`; the [CHANGELOG](../../CHANGELOG.md#desktop) links them.

## Requirements

- [Rust](https://rustup.rs/), through rustup: `src-tauri/rust-toolchain.toml` pins the version
- [Go](https://go.dev/dl/) 1.26
- [Node](https://nodejs.org/en/download) 26
- [pnpm](https://pnpm.io/installation) 12.17.0
- [protoc](https://github.com/protocolbuffers/protobuf/releases) 33.1, for `make generate-desktop`
- [cargo-llvm-cov](https://github.com/taiki-e/cargo-llvm-cov), for `make cover-desktop-changed` when the host changed

On Linux:

```console
sudo apt-get install -y build-essential libwebkit2gtk-4.1-dev \
  libayatana-appindicator3-dev librsvg2-dev
```

For the agent's command sandbox and its tests:

```console
sudo apt-get install -y bubblewrap passt
```

## Run

In `apps/desktop/`:

```console
pnpm install
pnpm tauri dev
```

`make clean-desktop-data`, from the repo root, wipes the dev app's data.

## Checks

While working, from the repo root, on what the change reaches:

```sh
make vet-desktop-changed test-desktop-changed   # this platform alone; a host-only change runs no Go test
```

Before you push, from the repo root:

```sh
make generate-desktop                 # after a change to proto/ or the GraphQL schema
make lint-desktop vet-desktop-changed test-desktop-changed
make cover-desktop-changed            # FULL=1 runs every Go test; the host's coverage needs cargo-llvm-cov
```

`vet-desktop` and `test-desktop` build the sidecar first: `tauri-build` fails without the bundled binary.

## Read next

- [`CLAUDE.md`](CLAUDE.md), and the guide in each area: conventions
- [`docs/adr/`](docs/adr/README.md): design decisions
- [`docs/security-model.md`](docs/security-model.md)
