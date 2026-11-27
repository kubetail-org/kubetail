# Kubetail gRPC Contract

`cluster_agent.proto` defines the gRPC services the Cluster API calls on the Cluster Agent. The Go client is generated into [`libs/go-common/clusteragentpb`](../go-common/clusteragentpb) and committed; the Rust server is generated on every build of [`cluster/agent`](../../cluster/agent). `test.proto` backs the Go tests.

## Checks

While working, after an edit:

```sh
make generate                     # from the repo root: regenerates the *.pb.go
cd cluster/agent && cargo build   # needs protoc
```

Before you push, the checks of both sides: [the root guide's *Go modules › Checks*](../../CLAUDE.md#checks) for `libs` and `cluster-api`, and [`cluster/agent/`'s](../../cluster/agent/README.md#checks).

