# crates — Rust in-cluster components

A Cargo workspace (`Cargo.toml`, resolver 3) of three crates. The root [`CLAUDE.md`](../CLAUDE.md) holds what crosses areas: the request path, the gRPC contract, the writing standards and the testing conventions.

| Crate | Kind | Holds |
| --- | --- | --- |
| `cluster_agent` | binary | The Cluster Agent: a gRPC server, one per node, that lists, watches and streams the container log files on that node. |
| `rgkl` | library | The log file reader the agent links: forward and backward streaming, grep, time bounds, follow. No binary. |
| `types` | library | `types::cluster_agent`: the tonic/prost code generated from `proto/cluster_agent.proto`, plus `FILE_DESCRIPTOR_SET` for reflection. Nothing hand-written. |

**Run cargo from `crates/`, never from a crate's directory.** `cargo test` and `cargo build` in `crates/rgkl` cover `rgkl` alone, which is what `make crates-test` and `make crates-build` do. CI runs every Rust step from `crates/`.

**`protoc` must be on `PATH` for any build, clippy or test.** `types/build.rs` compiles the proto into `$OUT_DIR` on every build; the output is never committed. The procedure after editing a `.proto` is the root guide's *The gRPC contract*.

**The toolchain is `stable`, unpinned** (`rust-toolchain.toml`). A new stable release can add clippy lints that fail `-D warnings` on code nobody touched; fix them in a commit of their own.

## cluster_agent

Runs as a DaemonSet with the node's `/var/log` mounted read-only (`hack/tilt/kubetail.yaml`). Only the Cluster API calls it. Unix only (`tokio::signal::unix`).

**Startup** (`main.rs`): parse the config, install logging, build the `Authorizer` (which needs a Kubernetes client config it can infer, in-cluster or a kubeconfig, so the binary does not start without one), configure TLS, register the services, serve until SIGINT or SIGTERM. A signal cancels the root `CancellationToken`; every stream holds a child token and runs on the shared `TaskTracker`, which `main` waits on before it exits.

**Config** (`config.rs`): `-c/--config <FILE>` is required and its extension picks the format. `-p/--param key:value` (repeatable) and `-a/--addr` override it. Precedence is overrides, then the file, then the defaults in `builder_with_defaults`. `${NAME}` in the file is replaced from the environment before parsing. Keys are top-level and kebab-case: `addr`, `container-logs-dir`, `logging.{enabled,level,format}`, `tls.{cert-file,key-file,ca-file,allowed-names}`. The three TLS paths have no default. `:50051` is shorthand for `[::]:50051`. `config/default/cluster-agent.yaml` is the documented default file. `NODE_NAME` is read from the environment by `LogMetadataImpl::new`.

| Module | Holds |
| --- | --- |
| `main.rs` | Startup, flags, TLS, the shutdown signal. |
| `config.rs` | `Config` and its parsing. |
| `auth.rs` | Authentication: `Identity`, `authenticate`, `check`, the tonic `interceptor`, `identity_from`. Pure functions, tested without a server. |
| `authorizer.rs` | Authorization: the `AccessReviewer` trait, `KubeAccessReviewer`, the caching `Authorizer`, `build_sar`. |
| `log_metadata.rs` | `LogMetadataService` (`List`, `Watch`), the file-name regex `LOG_FILE_REGEX`. |
| `log_metadata/log_metadata_watcher.rs` | `LogMetadataWatcher`: a debounced `notify` watch on the logs directory and each log file, turned into `ADDED`/`MODIFIED`/`DELETED` events. |
| `log_records.rs` | `LogRecordsService` (`StreamForward`, `StreamBackward`): resolves the file and hands it to `rgkl`. |

A log file is `<pod>_<namespace>_<container>-<container-id>.log` under `container-logs-dir`. `log_records.rs` builds that name from the request and canonicalizes it, which follows the `/var/log/containers` symlink to the real file.

## Security invariants

- **The listener is mutual TLS, always.** `configure_tls` sets the server identity and `client_ca_root` with no optional client auth. Everything below rests on it: weaken it and the identity headers are forgeable.
- **The agent never sees a Kubernetes token.** It trusts the `x-remote-user`, `x-remote-group` and `x-remote-extras` metadata a peer sends, provided the peer's certificate chains to `tls.ca-file` and, when `tls.allowed-names` is not empty, its CN is listed. An empty list accepts any certificate from the CA. The Go side that sets these headers is `modules/shared/grpchelpers`.
- **Every RPC authenticates, then authorizes, before it reads a log file or lists one.** Each method calls `auth::identity_from(&request)?` and `authorizer.is_authorized(&identity, &namespaces, "get")`; `List` checks only that the logs directory exists before it does. The check is a `SubjectAccessReview` for `get` on `pods/log` per namespace, or one cluster-wide review for an empty namespace list. A new method repeats both calls; a new service is wrapped in `auth::interceptor` in `main.rs`. `stream_forward_denies_before_file_lookup` and `stream_backward_denies_before_file_lookup` pin the order.
- **The health and reflection services carry no interceptor.** They sit behind mTLS alone. Nothing that returns log data goes on an unwrapped service.
- **`Authorizer` caches decisions for 30 seconds, denials included** (10,000 entries, keyed by identity, namespace and verb). A stream is authorized once, when it opens.
- **The pod, namespace, container and container id in a `LogRecordsStreamRequest` are joined into a path unchecked.** Only `namespace` is authorized. Treat every one of them as attacker-controlled when changing `get_log_filename`.

## rgkl

Two entry points, both called by `cluster_agent/src/log_records.rs`:

```rust
stream_forward(ctx, &path, start_time, stop_time, grep, follow_from, sender)
stream_backward(ctx, &path, start_time, stop_time, grep, sender)
```

Each sends `Result<LogRecord, Status>` on a tokio `mpsc` channel and returns `()`. An error travels in the channel, never as a return value.

- **Search is ripgrep's crates** (`grep`): a `Searcher` with mmap off and a 1 MiB heap limit, printing JSON into `CallbackWriter`, which `process_output` (`util/writer.rs`) parses back into a `LogRecord`.
- **The format comes from the file name**: Docker JSON when the path ends in `-json.log`, else CRI. Each stream function decides it; `util/format.rs` holds the enum.
- **A grep matches the message alone**, never the timestamp or stream, and is case-sensitive here; the Go side adds `(?i)` (`modules/shared/logs`). Each space in the pattern becomes a pattern that also matches ANSI sequences. No grep is `PassThroughMatcher`. A regex the engine rejects is `FsWatcherError::InvalidGrep`, which reaches the caller as `InvalidArgument`.
- **Time bounds are a binary search over byte offsets** (`util/offset.rs`), which assumes timestamps ascend through the file.
- **Forward** searches from the start offset, then, unless `stop_time` is set or `follow_from` is `Noop`, watches the file with `notify` and searches each new chunk. `FollowFrom::End` ignores both time bounds, starts at the end of the file and follows. Only `Modify` events are handled; a rotated file is not reopened.
- **Backward** reads lines in reverse through `ReverseLineReader` (`util/reader.rs`) into the same searcher. It never follows.
- **Sends block.** `process_output` sends with `task::block_in_place(|| sender.blocking_send(..))`, which panics on a current-thread runtime. A test in which a record is sent is `#[tokio::test(flavor = "multi_thread")]`.
- **A lost client is noticed on the next send.** A failed send cancels the stream's token.

## Conventions

- **No `unwrap` outside tests.** The workspace denies `clippy::unwrap_used` and every crate inherits it (`[lints] workspace = true`). A test module that calls `unwrap` opens with `#![allow(clippy::unwrap_used)]`. `expect` is for what cannot fail at run time (a static regex, a required clap argument) and says so in its message.
- **Errors are a `thiserror` enum per area with one mapping to `tonic::Status`**: `AuthError` through `auth_error_to_status`, `WatcherError` and `FsWatcherError` through `From<_> for Status`. Add a variant and its mapping together. Startup code returns `Box<dyn Error>`.
- **Cancellation is `tokio_util`'s `CancellationToken`.** A loop selects on `ctx.cancelled()`; a followed stream or a metadata watch cancelled by shutdown sends `Unavailable` before it ends. Per-request work is spawned through the `TaskTracker`, never a bare `tokio::spawn`, so shutdown waits for it.
- **Logging is `tracing`**: `info!` for lifecycle in `main.rs`, `debug!` for nearly everything else. `configure_logging` writes JSON, or the pretty format when `logging.format` is `pretty`.
- **Decisions are free functions, I/O is a thin shell around them.** `authenticate`, `check`, `build_sar` and the `util/offset.rs` searches are tested without a server, a cluster or a watcher. Shape new logic the same way.
- **Module files are `foo.rs` beside a `foo/` directory** in `cluster_agent`. A service implementation is `<Service>Impl`.
- **Every file opens with the Apache license header** (root guide).

## Tests & checks

```sh
cd crates
cargo test                         # every crate
cargo test -p cluster_agent auth   # one crate, tests matching a name
cargo fmt --all                    # write; CI runs it with -- --check
cargo clippy --all -- -D warnings
```

- **Tests are inline `#[cfg(test)]` modules** in the file they cover. There is no `tests/` directory.
- **A test seam is a parameter, a generic or a field.** `Authorizer::with_reviewer` takes a fake `AccessReviewer` (`CountingReviewer`, `DenyReviewer`, `AlwaysAllowReviewer`). `LogMetadataWatcher::watch::<T: Watcher>` takes the watcher type, so a test passes `PollWatcher`. `mint_cert` (`auth.rs`) makes a certificate with `rcgen`.
- **No sleeps.** A test that changes a file first awaits `LifecycleEvent::WatcherStarted` on the `lifecycle_tx` channel, then awaits the event it expects under a `tokio::time::timeout`. `test_newly_created_file_receives_modify_events` (`log_metadata_watcher.rs`) is the reference; `test_follow_from` (`rgkl/src/stream_forward.rs`) shows the signal for a followed stream.
- **The metadata watcher's debounce is two real seconds**, so its tests take seconds each. Rename and modify tests are `#[cfg(not(target_os = "macos"))]`.
- **Files go in `tempfile::tempdir()` or a `NamedTempFile`.** `config.rs` tests are `#[serial]`.
- **Table tests use `rstest`** (`util/matcher.rs`, `stream_forward.rs`).
- **CI's clippy does not lint test code** (no `--all-targets`). CI tests and builds on Linux alone, amd64 and arm64. `make crates-lint` and `make crates-vet` cover the workspace; `make crates-test` and `make crates-build` do not.
- **To run a local agent under Tilt**, set `KUBETAIL_RUST_DEV_MODE` to `docker` or `local` (a musl debug build). Without it Tilt pulls the published image and your changes to `crates/` are not deployed.

When you change the agent's lifecycle, its checks, `rgkl`'s contract or a convention here, update this file in the same change.
