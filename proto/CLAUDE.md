# proto — the gRPC contract

`cluster_agent.proto` defines `LogMetadataService` and `LogRecordsService`, which the Cluster API calls on the Cluster Agent. The Go client is committed protoc output in `modules/shared/clusteragentpb`; the Rust server is generated on every build by `crates/types/build.rs`. → [ADR: GraphQL for user-facing APIs, gRPC to the agent](../docs/adr/2026-11-09-graphql-for-user-facing-apis-grpc-to-the-agent.md)

After editing it:

```sh
cd modules/shared && go generate ./...   # needs protoc, protoc-gen-go, protoc-gen-go-grpc
cd crates && cargo build                 # needs protoc
```

Commit the regenerated `*.pb.go`; never hand-edit them. CI runs no job for a change under `proto/` alone, so run the Go and Rust checks yourself.

- **A `LogMetadata*` message is also a GraphQL type.** The Cluster API binds them straight into its schema (`modules/cluster-api/gqlgen.yml`), so a change to one is a schema change: follow `modules/CLAUDE.md`, *The GraphQL servers*.
- **`LogMetadataWatchEvent.type` is a string** (`ADDED`, `MODIFIED`, `DELETED`) spelled by hand in the agent and again in the UI (`pages/home/log-metadata-provider.tsx`); the Cluster API passes it through.
- **The identity metadata is not in the proto.** `x-remote-user`, `x-remote-group` and `x-remote-extras` are set in `modules/shared/grpchelpers` and read in `crates/cluster_agent/src/auth.rs`; change the two together.
