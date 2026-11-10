# proto — the gRPC contract

`cluster_agent.proto` defines `LogMetadataService` and `LogRecordsService`, which the Cluster API calls on the Cluster Agent. The Go client is committed protoc output in `modules/shared/clusteragentpb`; the Rust server is generated on every build by `crates/types/build.rs`. → [ADR: GraphQL for user-facing APIs, gRPC to the agent](../docs/adr/2026-11-09-graphql-for-user-facing-apis-grpc-to-the-agent.md)

After editing it:

```sh
make check-generated       # regenerates the *.pb.go, then fails because they changed
cd crates && cargo build   # needs protoc
```

Commit the regenerated `*.pb.go`; never hand-edit them. Their headers record the `protoc` and plugin versions, so they are generated with one of each: `protoc` is `protoc-version` in `.github/actions/setup-environment/action.yml`, which CI installs, and `make check-generated` refuses another; the plugins are pinned in `tools/go.mod`, and it builds them. A Dependabot update of a plugin fails `Generated · Check` until its PR regenerates the `*.pb.go`. A change under `proto/` alone runs CI's Rust jobs but not its Go jobs, so run the Go checks yourself.

- **A `LogMetadata*` message is also a GraphQL type.** The Cluster API binds them straight into its schema (`modules/cluster-api/gqlgen.yml`), so a change to one is a schema change: follow `modules/CLAUDE.md`, *The GraphQL servers*.
- **`LogMetadataWatchEvent.type` is a string** (`ADDED`, `MODIFIED`, `DELETED`) spelled by hand in the agent and again in the UI (`pages/home/log-metadata-provider.tsx`); the Cluster API passes it through.
- **The identity metadata is not in the proto.** `x-remote-user`, `x-remote-group` and `x-remote-extras` are set in `modules/shared/grpchelpers` and read in `crates/cluster_agent/src/auth.rs`; change the two together.
