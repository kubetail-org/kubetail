# Kubetail Desktop

A Kubernetes desktop app, in three parts that talk to each other. The root `CLAUDE.md` holds the repo's rules; this file holds what the desktop adds.

- **`src/`** — React webview. See `src/CLAUDE.md`.
- **`src-tauri/`** — Tauri/Rust native host (windows, tray, sidecar lifecycle), a Cargo workspace of its own. See `src-tauri/CLAUDE.md`.
- **`sidecar/`** — Go sidecar (GraphQL API, Kubernetes + cloud logic), a module outside the root `go.work`: run its Go commands in `sidecar/`. See `sidecar/CLAUDE.md`.
- **`lint/`** — Kubetail's own oxlint rules (`oxlint-plugin.js`), which `.oxlintrc.json` loads beside the webview's lint config. See `src/CLAUDE.md`, *Lint / build*.
- **`design/`** — how the webview's design in Figma maps to `src/`: the guide to syncing them in either direction, `components.json`, the manifest joining each Figma component and screen to its source, and the script that writes each one's stamp in Figma. The repo's `tools/designsync` checks the manifest (`make lint-desktop`) and lists what is out of sync (`make status-desktop-design`). See `design/CLAUDE.md`.
- **`src-icons/`** — the icon masters (SVG). `app-light.svg` and `app-dark.svg` are full-size: the Windows/Linux app icon and the webview's `AppMark`. The `-macos` masters are macOS's alone: `app-macos.svg` (Apple's grid, an 824px square on 1024) for `icon.icns`, and `tray-macos.svg` (full-size, since tray-icon fits the image to 18pt) for the template tray. `make generate-desktop-icons` generates `src-tauri/icons/`, `public/favicon.ico` and the webview's copies in `src/assets/icons/`; edit only the masters.
- **`licenses/`** — the licenses and `THIRD_PARTY_NOTICES.md` of the third-party programs the Linux packages carry (bwrap, pasta, the AppArmor profiles), installed with them. See `licenses/README.md` and `src-tauri/linux/CLAUDE.md`.
- **`proto/`** — shared gRPC contract (host↔sidecar). Single source of truth, compiled to Go (committed, by `make generate-desktop`) and Rust (by `src-tauri/build.rs`). `sidecar/internal/app/layout/testdata/layouts.json` pins the folder layout both sides compute.
- **`scripts/`** — the scripts only the desktop uses. The sidecar's build is `scripts/build-sidecar.go` (`make build-desktop-sidecar`); `tauri dev`/`tauri build` call it with `go run` from `apps/desktop/`, never `make` or `sh`, because on Windows Tauri runs them through `cmd.exe`.

The desktop's make targets are the root `Makefile`'s `<verb>-desktop` and `<verb>-desktop-<what>`; `make help` lists them. Environment variables that change a dev run are listed in [`docs/development-switches.md`](docs/development-switches.md). `make clean-desktop-data` deletes the dev app's data, cache, config and state folders (refuses while a dev sidecar runs; `sidecar/scripts/clean-dev-data.go`); `make clean-desktop-window` deletes only its saved window frame.

**`.github/` holds the desktop's workflows from Kstack, which GitHub runs only from the repo's root `.github/`.** Until step 3 of `docs/specs/desktop/` moves them there, no CI job checks the desktop: run its checks below.

## Host↔sidecar transport

Host and sidecar share one Unix socket / named pipe. **GraphQL** (webview↔sidecar) rides HTTP/1.1 (POST + SSE); **gRPC** (host↔sidecar control: tray auth state watch, login/logout, resync poke) rides HTTP/2. Both are multiplexed in the sidecar's `internal/app`, served by `net/http` as HTTP/1.1 plus **h2c** (prior knowledge only); no TLS. The cluster surface is **GraphQL-only**. → [ADR: single-socket h2c from the standard library](docs/adr/2026-10-26-single-socket-h2c-from-the-standard-library.md).

**The caches fill.** A reachable cluster whose credentials can read `kube-system` gets a cache; the cache controller arms `kubesync`, which mirrors each served kind into one `ClusterCachedKind` record, and each record arms that kind's sync (cold list, then watch). Every record's `events(...)` field serves its control-plane timeline, as do the event feeds `clusterEventFeed(id: ClusterID!)` and `objectEventFeed(id: ObjectID!)`. → `sidecar/CLAUDE.md`.

**Every data access is named for its contract**: [`docs/data-access.md`](docs/data-access.md) states each contract. A query is a Get, List or Fetch, its field ending in the verb (`envVarsGet`). Cluster data arrives as **collection watches**: an `Added` snapshot, one `Bookmark` closing it, then per-object `Added`/`Modified`/`Deleted` (`clusterCollectionWatch`, `clusterCacheCollectionWatch`, `clusterCachedKindCollectionWatch`). A **state watch** sends the whole value on subscribe and on each change, with no `Bookmark` (`authStateWatch`, `…ConnectionStateWatch`); `clusterCacheHealthKeyedStateWatch` is a **keyed state watch**, one value per `cacheID`. Until the launch has read the keyring, a connection's phase or a provider's source is `Restoring` and `authStateWatch` carries `restoring` ([ADR](docs/adr/2026-11-20-a-launch-restores-keyring-credentials-in-the-background.md)). `clusterDataRateSampleFeed(id)` is a **sample feed**: the whole retained window first, even if empty, then only newer samples; `sampleFeedReducer` keeps the last window ÷ interval samples and reads no times. `logsTail` is a **cursor tail**. The webview folds each subscription through its reducer in `src/lib/graphql/` and **joins client-side**: caches onto clusters by `clusterID`, kinds onto caches by `owner.id`, health values by `cacheID`. **A cluster's id is the `ClusterID` scalar, every other record's is `ObjectID`; the webview never builds or parses either.** Rules for consumers:

- **Never render an empty state before the `Bookmark` lands.**
- **Detect it as `type === 'Bookmark'`, never as a missing entity** — a nested non-null field erroring nulls its parent, so a null entity also arrives on ordinary changes.
- A change with no entity is dropped, not folded.

→ [ADR: name every data access](docs/adr/2026-11-25-name-every-data-access-for-its-family-kind-and-operation.md). The kube-context picker derives its list from `clusterCollectionWatch` (`src/lib/kube-config.tsx`).

**The sidecar never filters what it streams.** A record awaiting deletion arrives carrying `deletionRequestedAt` (an ordinary `Modified`); `Deleted` means its row is gone. `clusterDelete` returns once the row is marked and the teardown follows. **Only the `Deleted` is promised** — a fast teardown folds the mark into it, so never gate a removal on having seen the mark. `ClustersProvider`'s fold folds a tombstoned row as a removal, once, for every view; change that there (e.g. to draw "Deleting…"), never in views.

A new query or subscription takes its contract and name from [`docs/data-access.md`](docs/data-access.md).

> **The guides.** There is a `CLAUDE.md` per area (`src/`, `src-tauri/`, `sidecar/`, `design/`, one per larger sidecar package, listed in `sidecar/CLAUDE.md`, one per webview subsystem, listed in `src/CLAUDE.md`, and the host's, listed in `src-tauri/CLAUDE.md`); this file holds what crosses them. The desktop's ADRs are its own sequence, in [`docs/adr/`](docs/adr/README.md).
>
> **Security is recorded in two places.** [`docs/security-model.md`](docs/security-model.md) is the threat model and the map, and [`docs/security-model/`](docs/security-model/) states, a file per subject, what is protected now and which tests pin it; [`docs/security/`](docs/security/) holds dated review records, append-only. Gaps live in [`docs/TODO.md`](docs/TODO.md#security); an accepted risk becomes an ADR plus a **By decision** row in the model. When you change a boundary or land a protection, move its row in the same change; `make lint-desktop` fails on a row citing a test or file that is gone. → [ADR](docs/adr/2026-11-25-the-security-model-is-a-file-per-subject-checked-against-the-code.md).
>
> **Persisted data has a register.** [`docs/persisted-data.md`](docs/persisted-data.md) lists every format Kubetail keeps across a restart, with its class, version stamp and pinning test. A change to a persisted format updates its row in the same commit. No release has shipped a format yet, so each store's schema is one `0001_init.sql`; from `desktop/v0.1.0` on, a schema change is the next numbered migration, and a shipped migration is never edited or removed. There is no app-level data migration yet, so the layout under the data and cache roots only grows. → [ADR: each persisted format carries its own version stamp](docs/adr/2026-10-29-each-persisted-format-carries-its-own-version-stamp.md), [ADR: a shipped schema changes only by a new migration](docs/adr/2026-11-08-a-shipped-schema-changes-only-by-a-new-migration.md).

## Tests

What the desktop adds to the root `CLAUDE.md`'s *Testing conventions*; each area's guide adds its tooling. → [ADR: test conventions](docs/adr/2026-10-17-tests-split-by-platform-take-seams-as-parameters-and-never-sleep.md)

- **How a test waits.** TypeScript: `vi.useFakeTimers()` with `advanceTimersByTimeAsync`, `await waitFor(...)`, or a `flush()`/`act` helper. Go: a channel, through `testutil.Probe`/`Signal`, `testutil.Recv`/`Wait`, or `require.Eventually`. Rust: `#[tokio::test(start_paused = true)]`, as `connect_retries_until_endpoint_appears` in `src-tauri/src/services/sidecar/ipc.rs` does.
- **A negative assertion's window is a multiple of the (shrunk) cadence**, and its baseline is sampled off the same channel, never by polling a counter.
- **In Go, split a partly platform-specific test rather than skip it** (`atomicjson`'s `TestSaveCreatesMissingDirs` / `TestSaveIsOwnerOnly`). Unix paths (`/tmp`) and rename-over-open-file belong to the Unix half. Wait out a stray goroutine through the unit's test seam (`write`'s `TestWriteSetsTheStamp`).
- **`agent/chat`, `kubestore` and `kubeconn` call `t.Parallel()` in every test**; one that must touch process state (`t.Setenv`, `syscall.Umask`) stays serial and says why.
- **A Rust file with no line a test can reach is left out of coverage**, in the root `scripts/cover-changed.sh`'s `ignored` and in Codecov's `ignore` with its reason; a file with one reachable line marks the rest.

## Checks

While working, on what you touched: `go test ./internal/<pkg>/...` in `sidecar/`, `pnpm test --run <file>`, `cargo test` in `src-tauri/`.

Before a PR:

```sh
make generate-desktop                 # after a change to proto/ or the GraphQL schema
make lint-desktop vet-desktop test-desktop
make cover-desktop-changed            # FULL=1 runs every Go test; the host's coverage needs cargo-llvm-cov
```

`vet-desktop` and `test-desktop` build the sidecar first: `tauri-build` fails without the bundled binary.

## Security invariants

The whole picture is [`docs/security-model.md`](docs/security-model.md); each area's guide lists what an ordinary change there can break. Three hold everywhere:

- **Anything running in the webview holds the full cluster surface, and the shell behind it.** `graphql_query` forwards operations unexamined, so executing code can call every mutation, `approvalDecide` included. The CSP in `src-tauri/tauri.conf.json` is the containment.
- **The approval request is the user's eye, and the eye is the whole gate.** Approve is a click on the drawn request and nothing else. Adding an approval path, a grant path, or a way for the model to make a grant is a security change, recorded under `docs/security/`.
- **Cluster data, model output and command output are attacker-controlled text.** No HTML sink, control sequences stripped, every untrusted string drawn through `VisibleText`; the sidecar redacts Secret data at write time and never logs request data.
