# Kubetail Shared Go Libraries

The Go packages that `cli`, `dashboard` and `cluster/api` import. No binary ships from here. Among them:

* `k8shelpers`: Kubernetes clients for each environment, and the namespace allow-list
* `logs`: log streams, read from the Kubernetes API or from the Cluster Agents
* `clusteragentpb`: the Go code generated from the gRPC contract in [`libs/proto`](../proto)
* `grpchelpers`: forwards the caller's identity to the Cluster Agent
* `helm`: the Helm client behind `kubetail cluster`
* `graphql/errors`: the errors the GraphQL servers return

## Generate

`go generate ./...` runs `protoc` to regenerate `clusteragentpb`. The `protoc` and plugin versions are pinned; [`libs/proto/CLAUDE.md`](../proto/CLAUDE.md) says how to match them.

## Checks

[The root guide's *Go modules › Checks*](../../CLAUDE.md#checks) holds the commands every Go module shares.

While working, from the repo root:

```sh
make vet-libs-changed test-libs-changed          # what the change reaches in this module
make test-cli-changed test-cluster-api-changed   # what it reaches in the modules that import this one
```

Before you push, from the repo root:

```sh
make lint-libs vet-libs-changed test-libs-changed cover-libs-changed   # only this module's own tests count toward its coverage
make test-cli-changed test-cluster-api-changed                         # the modules that import this one
```

