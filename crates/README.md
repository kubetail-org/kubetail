# Kubetail Rust Packages

Workspace that contains the Rust packages used by Kubetail

## Overview

This workspace contains the following modules:

* [cluser_agent](cluster_agent) - Kubetail Cluster Agent
* [rgkl](rgkl) - RipGrep for Kubernetes Logs (RGKL)

Please view the README in each directory for more details. 

## Development Commmands

### Run linter

```console
cargo fmt --all -- --check
```

### Run clippy

```console
cargo clippy --all -- -D warnings
```

### Run tests

```console
cargo test
```

### Run builder

```console
cargo build --release
```
