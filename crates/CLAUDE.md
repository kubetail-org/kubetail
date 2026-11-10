# crates — Rust in-cluster components

A Cargo workspace of three crates: `cluster_agent` (the Cluster Agent, a gRPC server on each node that lists, watches and streams that node's container log files), `rgkl` (the log file reader it links) and `types` (the tonic/prost code generated from `proto/cluster_agent.proto`; nothing hand-written).

- **Run cargo from `crates/`, never from a crate's directory.** From the repo root, `make lint-rust`, `vet-rust` and `test-rust` do.
- **`protoc` must be on `PATH` for any build, clippy or test.** `types/build.rs` compiles the proto on every build; the output is never committed. Editing the proto: `proto/CLAUDE.md`.
- **`rust-toolchain.toml` pins the exact Rust version**, which CI and the agent's Dockerfiles share (root `CLAUDE.md`, Dependencies). A bump can add clippy lints that fail `-D warnings` on code nobody touched; fix them in the same PR.

## Checks

```sh
cd crates
cargo fmt --all                    # write; CI runs it with -- --check
cargo clippy --all --all-targets -- -D warnings
cargo test                         # every crate; `cargo test -p cluster_agent auth` for one crate, by name
```

CI tests and builds on Linux alone. To run a local agent under Tilt, set `KUBETAIL_RUST_DEV_MODE` to `docker` or `local`; without it Tilt pulls the published image and your changes to `crates/` are not deployed.

## cluster_agent

Runs as a DaemonSet with the node's `/var/log` mounted read-only. Only the Cluster API calls it. Unix only.

- **Config** (`config.rs`): `-c/--config <FILE>` is required. Precedence is `-p key:value` overrides, then the file, then the defaults in `builder_with_defaults`, which `config/default/cluster-agent.yaml` documents. Keys are top-level and kebab-case. `NODE_NAME` is read from the environment.
- **It does not start without a Kubernetes client config** it can infer, in-cluster or a kubeconfig: the `Authorizer` needs one.
- **Authentication is `auth.rs`, authorization is `authorizer.rs`.**
- **The health service reports serving from just before `serve` until a signal arrives**, then not serving while the server drains. A new service is listed in `SERVICES` (`health.rs`) so a check names it.
- **A log file is `<pod>_<namespace>_<container>-<container-id>.log` under `container-logs-dir`.** `log_records.rs` builds that name from the request and canonicalizes it, which follows the `/var/log/containers` symlink to the real file.

## Security invariants

- **The listener is mutual TLS, always.** `configure_tls` sets the server identity and `client_ca_root` with no optional client auth. Everything below rests on it: weaken it and the identity headers are forgeable.
- **The agent never sees a Kubernetes token.** It trusts the `x-remote-user`, `x-remote-group` and `x-remote-extras` metadata a peer sends, provided the peer's certificate chains to `tls.ca-file` and, when `tls.allowed-names` is not empty, its CN is listed. An empty list accepts any certificate from the CA. The Go side that sets these headers is `modules/shared/grpchelpers`; change the two together.
- **Every RPC authenticates, then authorizes, before it reads a log file or lists one.** Each method calls `auth::identity_from(&request)?` and `authorizer.is_authorized(&identity, &namespaces, "get")`: a `SubjectAccessReview` for `get` on `pods/log` per namespace, or one cluster-wide review for an empty namespace list. A new method repeats both calls; a new service is wrapped in `auth::interceptor` in `main.rs`. `stream_forward_denies_before_file_lookup`, `stream_backward_denies_before_file_lookup` and `list_denies_before_logs_dir_check` pin the order.
- **The health and reflection services carry no interceptor.** They sit behind mTLS alone. Nothing that returns log data goes on an unwrapped service.
- **`Authorizer` caches decisions for 30 seconds, denials included**, keyed by identity, namespace and verb. A stream is authorized once, when it opens.
- **The pod, namespace, container and container id in a `LogRecordsStreamRequest` are joined into a path unchecked.** Only `namespace` is authorized. Treat every one of them as attacker-controlled when changing `get_log_filename`.

## rgkl

Two entry points, `stream_forward` and `stream_backward`, both called by `cluster_agent/src/log_records.rs`. Each sends `Result<LogRecord, Status>` on a tokio `mpsc` channel and returns `()`. An error travels in the channel, never as a return value.

- **The format comes from the file name**: Docker JSON when the path ends in `-json.log`, else CRI.
- **A CRI line is split only in `util/cri.rs`.** The grep, the time search and the record writer all call it, so they agree on which lines exist and which are final. A line it rejects is dropped.
- **A grep matches the message alone**, never the timestamp or stream, and is case-sensitive here; the Go side adds `(?i)` (`modules/shared/logs`). Each space in the pattern becomes a pattern that also matches ANSI sequences. A regex the engine rejects reaches the caller as `InvalidArgument`.
- **Time bounds are a binary search over byte offsets** (`util/offset.rs`), which assumes timestamps ascend through the file.
- **Forward** follows the file after the search unless `stop_time` is set or `follow_from` is `Noop`. `FollowFrom::End` ignores both time bounds. Only `Modify` events are handled; a rotated file is not reopened. **Backward** never follows.
- **Sends block.** `process_output` sends with `task::block_in_place(|| sender.blocking_send(..))`, which panics on a current-thread runtime. A test in which a record is sent is `#[tokio::test(flavor = "multi_thread")]`.
- **A lost client is noticed on the next send.** A failed send cancels the stream's token.

## Conventions

- **No `unwrap` outside tests.** The workspace denies `clippy::unwrap_used`, and a crate inherits it with `[lints] workspace = true`. A test module that calls `unwrap` opens with `#![allow(clippy::unwrap_used)]`. `expect` is for what cannot fail at run time (a static regex, a required clap argument) and says so in its message.
- **Errors are a `thiserror` enum per area with one mapping to `tonic::Status`**: `AuthError` through `auth_error_to_status`, `WatcherError` and `FsWatcherError` through `From<_> for Status`. Add a variant and its mapping together.
- **Cancellation is `tokio_util`'s `CancellationToken`.** A signal cancels the root token and every stream holds a child. A loop selects on `ctx.cancelled()`; a followed stream or a metadata watch cancelled by shutdown sends `Unavailable` before it ends. Per-request work is spawned through the `TaskTracker`, never a bare `tokio::spawn`, so shutdown waits for it.
- **Logging is `tracing`**: `info!` for lifecycle in `main.rs`, `debug!` for nearly everything else.
- **Decisions are free functions, I/O is a thin shell around them.** `authenticate`, `check`, `build_sar` and the `util/offset.rs` searches are tested without a server, a cluster or a watcher. Shape new logic the same way.
- **Module files are `foo.rs` beside a `foo/` directory** in `cluster_agent`. A service implementation is `<Service>Impl`.

## Tests

- **Tests are inline `#[cfg(test)]` modules** in the file they cover. There is no `tests/` directory.
- **A test seam is a parameter, a generic or a field.** `Authorizer::with_reviewer` takes a fake `AccessReviewer`. `LogMetadataWatcher::watch::<T: Watcher>` takes the watcher type, so a test passes `PollWatcher`. `mint_cert` (`auth.rs`) makes a certificate with `rcgen`.
- **No sleeps.** A test that changes a file first awaits `LifecycleEvent::WatcherStarted` on the `lifecycle_tx` channel, then awaits the event it expects under a `tokio::time::timeout`. `test_newly_created_file_receives_modify_events` (`log_metadata_watcher.rs`) is the reference; `test_follow_from` (`rgkl/src/stream_forward.rs`) shows the signal for a followed stream.
- **`LogMetadataWatcher::new` takes the debounce.** Tests pass a short one, and a negative assertion waits longer than it. Rename and modify tests are `#[cfg(not(target_os = "macos"))]`.
- **Files go in `tempfile::tempdir()` or a `NamedTempFile`.** `config.rs` tests are `#[serial]`. Table tests use `rstest`.
