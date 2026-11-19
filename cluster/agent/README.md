# Kubetail Rust Packages

Workspace that contains the Rust packages used by Kubetail

## Overview

This workspace contains the following modules:

* [cluster_agent](cluster_agent) - Kubetail Cluster Agent
* [rgkl](rgkl) - RipGrep for Kubernetes Logs (RGKL)
* [types](types) - gRPC types generated from `libs/proto/`

Please view the README in each directory for more details. 

## Checks

In `cluster/agent/`.

While working:

```sh
cargo fmt --all
cargo test -p rgkl                 # one crate, by name; `cargo test -p cluster_agent auth` filters its tests
```

Before a PR:

```sh
make lint-cluster-agent vet-cluster-agent test-cluster-agent   # from the repo root: rustfmt's check, license headers, coverage markers, clippy, cargo test
make cover-cluster-agent-changed                               # from the repo root; needs cargo-llvm-cov
```

CI tests and builds on Linux alone. To run a local agent under Tilt, set `KUBETAIL_RUST_DEV_MODE` to `docker` or `local`; without it Tilt pulls the published image and your changes to `cluster/agent/` are not deployed.

## Build

```console
cargo build --release
```
