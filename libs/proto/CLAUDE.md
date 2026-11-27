# proto — the gRPC contract

`cluster_agent.proto` defines `LogMetadataService` and `LogRecordsService`, which the Cluster API calls on the Cluster Agent. The Go client is committed protoc output in `libs/go-common/clusteragentpb`; the Rust server is generated on every build by `cluster/agent/types/build.rs`. → [ADR: GraphQL for user-facing APIs, gRPC to the agent](../../docs/adr/2026-11-09-graphql-for-user-facing-apis-grpc-to-the-agent.md)

## Checks

Keep this section in step with the one in `README.md`.

While working, after an edit:

```sh
make generate                     # from the repo root: regenerates the *.pb.go
cd cluster/agent && cargo build   # needs protoc
```

Before you push, the checks of both sides: the root guide's *Go modules › Checks* for `libs` and `cluster-api`, and `cluster/agent/`'s.

## Generated code

Commit the regenerated `*.pb.go`; never hand-edit them. Their headers record the `protoc` and plugin versions, so they are generated with one of each: `protoc` is `protoc-version` in `.github/actions/setup-environment/action.yml`, which CI installs, and `make generate` refuses another; the plugins are pinned in `tools/go.mod`, and it builds them. Dependabot moves the plugins in a `protobuf` group of their own, whose PR fails `Check · Generated` until `make generate` is run on it. A change under `libs/proto/` alone runs CI's Rust jobs but not its Go jobs, so run the Go checks yourself.

- **A `LogMetadata*` message is also a GraphQL type.** The Cluster API binds them straight into its schema (`cluster/api/gqlgen.yml`), so a change to one is a schema change: follow `libs/graphql/CLAUDE.md`.
- **`LogMetadataWatchEvent.type` is a string** (`ADDED`, `MODIFIED`, `DELETED`) spelled by hand in the agent and again in the UI (`pages/home/log-metadata-provider.tsx`); the Cluster API passes it through.
- **The identity metadata is not in the proto.** `x-remote-user`, `x-remote-group` and `x-remote-extras` are set in `libs/go-common/grpchelpers` and read in `cluster/agent/cluster_agent/src/auth.rs`; change the two together.
