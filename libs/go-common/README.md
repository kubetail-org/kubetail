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

Before a PR:

```sh
make lint-libs vet-libs test-libs   # from the repo root
make test-cli test-cluster-api      # from the repo root: every module that imports this one
make cover-libs-changed             # from the repo root; only this module's own tests count
```

