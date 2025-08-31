# Kubetail Go Modules

Workspace that contains the Go modules used by Kubetail

## Overview

This workspace contains the following modules:

* [cli](cli) - Kubetail CLI
* [cluster-api](cluster-api) - Kubetail Cluster API
* [dashboard](dashboard) - Kubetail Dashboard
* [shared](shared) - Shared libraries

Please view the README in each directory for more details. 

## Development Commands

### Run code generators

First install the dependencies:

```console
brew install protobuf protoc-gen-go protoc-gen-go-grpc
```

Next, run the code generators:

```console
go generate github.com/kubetail-org/kubetail/modules/...
```

### Run tests

Using the Go toolchain directly:
```console
go test github.com/kubetail-org/kubetail/modules/...
```

Using the Makefile:
```console
# Run tests for all Go modules
make modules-test

# Run linter for all Go modules
make modules-lint

# Run code vetting for all Go modules
make modules-vet
```

You can also run all Go module checks at once with:
```console
make modules-all
```
