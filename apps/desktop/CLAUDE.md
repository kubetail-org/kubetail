# Kubetail Desktop

A Kubernetes desktop app in three parts: the React webview (`src/`), the Tauri/Rust host (`src-tauri/`, a Cargo workspace of its own) and the Go sidecar (`sidecar/`, a module outside the root `go.work`: run its Go commands in `sidecar/`). Each has a guide; this file holds what crosses them.

- **`lint/`** — Kubetail's own oxlint rules, which `.oxlintrc.json` loads. See `src/CLAUDE.md`, *Lint / build*.
- **`design/`** — how the Figma design maps to `src/`. See `design/CLAUDE.md`.
- **`src-icons/`** — the icon masters. `make generate-desktop-icons` generates `src-tauri/icons/`, `public/favicon.ico` and `src/assets/icons/`; edit only the masters. `app-macos.svg` sits on Apple's grid (an 824px square on 1024); `tray-macos.svg` is full-size, since tray-icon fits the image to 18pt.
- **`licenses/`** — what the Linux packages carry. See `src-tauri/linux/CLAUDE.md`.
- **`proto/`** — the host↔sidecar gRPC contract, compiled to Go (committed, by `make generate-desktop`) and Rust (by `src-tauri/build.rs`).
- **`scripts/build-sidecar.go`** — `tauri dev`/`tauri build` call it with `go run` from `apps/desktop/`, never `make` or `sh`, because on Windows Tauri runs them through `cmd.exe`.

The desktop's make targets are `<verb>-desktop` and `<verb>-desktop-<what>` in the root `Makefile`; `make help` lists them. A switch that changes a dev run gets a row in [`docs/development-switches.md`](docs/development-switches.md). `make clean-desktop-data` refuses while a dev sidecar runs.

CI checks the desktop in `ci-desktop.yml` and `coverage-desktop.yml`, on the `desktop` set of `.github/ci-config/runners.json`, and releases it with `release-desktop.yml`.

The desktop's ADRs are their own sequence, in [`docs/adr/`](docs/adr/README.md).

## Host↔sidecar transport

Host and sidecar share one Unix socket / named pipe, served by the sidecar's `net/http` as HTTP/1.1 plus h2c (prior knowledge only), with no TLS. **GraphQL** (webview↔sidecar) rides HTTP/1.1 (POST + SSE); **gRPC** (host↔sidecar control: auth state, login/logout, resync poke) rides HTTP/2. The cluster surface is GraphQL-only. → [ADR: single-socket h2c from the standard library](docs/adr/2026-10-26-single-socket-h2c-from-the-standard-library.md).

## Data access

**A new query or subscription takes its contract and name from [`docs/data-access.md`](docs/data-access.md)**, which states each contract and what its consumer may assume. → [ADR: name every data access](docs/adr/2026-11-25-name-every-data-access-for-its-family-kind-and-operation.md).

- **Never render an empty state before a collection watch's `Bookmark` lands.**
- **Detect it as `type === 'Bookmark'`, never as a missing entity**: a nested non-null field that errors nulls its parent, so a null entity also arrives on ordinary changes. A change with no entity is dropped, not folded.
- **The webview joins client-side**: caches onto clusters by `clusterID`, kinds onto caches by `owner.id`, health values by `cacheID`.
- **A cluster's id is the `ClusterID` scalar, every other record's is `ObjectID`; the webview never builds or parses either.**
- Until the launch has read the keyring, a connection's phase or a provider's source is `Restoring`, and `authStateWatch` carries `restoring`. → [ADR](docs/adr/2026-11-20-a-launch-restores-keyring-credentials-in-the-background.md).

A reachable cluster whose credentials can read `kube-system` gets a cache, which mirrors each served kind (`sidecar/internal/services/cluster/CLAUDE.md`).

**The sidecar never filters what it streams.** A record awaiting deletion arrives as an ordinary `Modified` carrying `deletionRequestedAt`; `Deleted` means its row is gone. `clusterDelete` returns once the row is marked. **Only the `Deleted` is promised**: a fast teardown folds the mark into it, so never gate a removal on having seen the mark.

## Security and persisted data

**Security is recorded in two places.** [`docs/security-model.md`](docs/security-model.md) is the threat model and the map, and [`docs/security-model/`](docs/security-model/) states, a file per subject, what is protected now and which tests pin it. [`docs/security/`](docs/security/) holds dated review records, append-only. Gaps live in [`docs/TODO.md`](docs/TODO.md#security); an accepted risk becomes an ADR plus a **By decision** row in the model. A change to a boundary or a new protection moves its row in the same change; `make lint-desktop` fails on a row citing a test or file that is gone. → [ADR](docs/adr/2026-11-25-the-security-model-is-a-file-per-subject-checked-against-the-code.md).

**Persisted data has a register.** [`docs/persisted-data.md`](docs/persisted-data.md) lists every format kept across a restart; a change to a persisted format updates its row in the same commit. Until `desktop/v0.1.0` ships, each store's schema is one `0001_init.sql`; from then on, a schema change is the next numbered migration, and a shipped migration is never edited or removed. There is no app-level data migration, so the layout under the data and cache roots only grows. → [ADR: each persisted format carries its own version stamp](docs/adr/2026-10-29-each-persisted-format-carries-its-own-version-stamp.md), [ADR: a shipped schema changes only by a new migration](docs/adr/2026-11-08-a-shipped-schema-changes-only-by-a-new-migration.md).

Each area's guide lists what an ordinary change there can break. Three invariants hold everywhere:

- **Anything running in the webview holds the full cluster surface, and the shell behind it.** `graphql_query` forwards operations unexamined, so executing code can call every mutation, `approvalDecide` included. The CSP in `src-tauri/tauri.conf.json` is the containment.
- **The approval request is the user's eye, and the eye is the whole gate.** Approve is a click on the drawn request and nothing else. Adding an approval path, a grant path, or a way for the model to make a grant is a security change, recorded under `docs/security/`.
- **Cluster data, model output and command output are attacker-controlled text.** No HTML sink, control sequences stripped, every untrusted string drawn through `VisibleText`; the sidecar redacts Secret data at write time and never logs request data.

## Tests

What the desktop adds to the root *Testing conventions*. → [ADR: test conventions](docs/adr/2026-10-17-tests-split-by-platform-take-seams-as-parameters-and-never-sleep.md)

- **How a test waits.** TypeScript: `vi.useFakeTimers()` with `advanceTimersByTimeAsync`, `await waitFor(...)`, or a `flush()`/`act` helper. Go: a channel, through `testutil.Probe`/`Signal`, `testutil.Recv`/`Wait`, or `require.Eventually`. Rust: `#[tokio::test(start_paused = true)]` (`connect_retries_until_endpoint_appears` in `src-tauri/src/services/sidecar/ipc.rs`).
- **A negative assertion's window is a multiple of the (shrunk) cadence**, and its baseline is sampled off the same channel, never by polling a counter.
- **In Go, split a partly platform-specific test rather than skip it** (`atomicjson`'s `TestSaveCreatesMissingDirs` / `TestSaveIsOwnerOnly`). Unix paths (`/tmp`) and rename-over-open-file belong to the Unix half. Wait out a stray goroutine through the unit's test seam (`write`'s `TestWriteSetsTheStamp`).
- **`agent/chat`, `kubestore` and `kubeconn` call `t.Parallel()` in every test**; one that must touch process state (`t.Setenv`, `syscall.Umask`) stays serial and says why.
- **A Rust file with no line a test can reach is left out of coverage**, in the root `scripts/cover-changed.sh`'s `ignored` and in Codecov's `ignore` with its reason; a file with one reachable line marks the rest.

## Checks

While working, on what you touched: `go test ./internal/<pkg>/...` in `sidecar/`, `pnpm test --run <file>`, `cargo test` in `src-tauri/`.

Before a PR, from the repo root:

```sh
make generate-desktop                 # after a change to proto/ or the GraphQL schema
make lint-desktop vet-desktop test-desktop
make cover-desktop-changed            # FULL=1 runs every Go test; the host's coverage needs cargo-llvm-cov
```

`vet-desktop` and `test-desktop` build the sidecar first: `tauri-build` fails without the bundled binary.
