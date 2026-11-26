# src-tauri — Tauri/Rust native host

The desktop app's native host (Tauri 2): windows, the tray and menus, the sidecar's lifecycle, and the GraphQL bridge from the webview to the sidecar's socket. [`linux/CLAUDE.md`](linux/CLAUDE.md) (what the Linux packages carry) and [`src/updater/CLAUDE.md`](src/updater/CLAUDE.md) (the in-app updater and its keys) have guides of their own; open one before changing the scripts it lists.

## Distribution and releases

**Not distributed through the Mac App Store.** `release-desktop.yml` ships a notarized `.dmg` with the sidecar signed inside, an `.msi` signed through SignPath, unsigned `.deb`/`.rpm` (Linux's only formats), and `SHA256SUMS` with a detached GPG signature. The app and its bundled sidecar (`externalBin`) are versioned and released together. macOS and Windows update in the app; Linux does not. **A Linux `tauri build` without `make build-desktop-bwrap` and `make build-desktop-pasta` first fails**: `tauri.linux.conf.json` lists both programs in each format's `files`. → [ADR: Linux ships only the .deb and the .rpm](../docs/adr/2026-11-07-linux-ships-only-the-deb-and-the-rpm.md).

**A release file is named `kubetail-desktop-<version>-<os>-<arch>.<ext>`**, all lowercase, with Go's words (`linux`, `darwin`, `windows`; `amd64`, `arm64`, and `universal` for the one macOS build). `<version>` is the SemVer, also for the RPM and MSI. The updater's `latest-<key id>.json` carry no version, since installed apps read their URLs. The `release` job fails on any set of files other than the one it lists. Tauri names its files after `productName`, so `release-desktop.yml` renames each as it collects it, the `.msi` only after SignPath has signed it, since SignPath's artifact configuration matches Tauri's name. → [ADR: release files are named kubetail-\<product\>-\<version\>-\<os\>-\<arch\>](../docs/adr/2026-11-08-release-files-are-named-kubetail-product-version-os-arch.md).

**Cut a release with `/release-desktop`.** It checks that `tauri.conf.json`, `Cargo.toml`, `Cargo.lock` and `package.json` name the version, and pushes `desktop/v<version>`, which makes a draft release. Write its notes with `/release-notes desktop desktop/v<version>` before publishing it, off the repo's latest release (the CLI's). A published final reaches installed apps (`publish-desktop-updates.yml`).

**A desktop version is `<x>.<y>.<z>` or `<x>.<y>.<z>-rc.<n>`**; `scripts/check-release-version.sh`, which `release-desktop.yml`'s `config` job and the skill run, rejects any other and derives the package versions. The deb, `.dmg` and app carry the SemVer. The RPM is Version `<x>.<y>.<z>`, Release `0.rc.<n>` (`1` for a final), bundled from the built binary by `tauri bundle`; the MSI is `<x>.<y>.<z>.<n>` (`<x>.<y>.<z>` for a final), set through `bundle.windows.wix.version`. → [ADR: desktop pre-releases are rc.N](../docs/adr/2026-11-06-desktop-pre-releases-are-rc-n-mapped-per-package-format.md).

## Modules

**Platform glue is wiring alone.** `app.rs`, `app_menu/macos.rs`, `dock_menu/macos.rs` and the `wake/` platform sources hold no decision. What an item, event or reading means is a function compiled and tested on every platform: the event loop's in `lib.rs` (`exit_requested`, `start_quit`, `exited`, `page_started`, `first_signal`), `AppMenuAction`, `DockAction` and `terminate_reply` in their `mod.rs`, `wake/core.rs`'s "which integer means online" mappings, `os_theme.rs`'s readings.

- **`app.rs`** builds the app and runs `setup`; every item is inside a coverage marker. It also holds the commands only the real app can run: `quit` routes through `AppHandle::exit`, and **`open_account_url` names its own URL**, since a command that opened whatever it was handed would be an exfiltration channel. It swaps the tray's menu (`rebuild_tray_menu`), which on Linux needs GTK.
- **`commands.rs`** — keep the handlers thin; delegate to services. `cloud.rs` is the one home of the kubetail cloud base URL.
- **`host_file.rs`** — `host.json`, the persisted-settings source of truth, in `Folders::config`. All-`Option` patches (`HostFilePatch`) go through the one `update_host_file` command; reads are defensive, writes atomic; persistence only. It reaches the webview (`src/lib/host-file.ts`) through `window.__KUBETAIL_HOST__`, set by the initialization script `build_window` injects, and the `host-file-updated` broadcast. **The initialization script's text is fixed at the build**: after a write, `window_manager::host_file_written` stores the file in each window's `sessionStorage`, where a reload's script reads it first, and repaints each native background; a page that has just loaded calls `read_host_file` for a write it missed. A layout change bumps `CURRENT_SCHEMA_VERSION` and updates its row in [`docs/persisted-data.md`](../docs/persisted-data.md) in the same commit. → [ADR: host.json settings](../docs/adr/2026-08-09-host-json-settings.md), [ADR: host.json in the Kubetail config directory](../docs/adr/2026-11-21-host-json-lives-under-the-kubetail-config-directory.md), [ADR: a reload reads the latest host.json](../docs/adr/2026-11-26-a-reload-reads-the-latest-host-json-from-session-storage.md).
- **`paths.rs`** is the one place a folder's leaf is resolved, a folder per kind on each platform. **The sidecar places its folders itself** from the same environment; `sidecar/internal/app/layout/testdata/layouts.json` pins both layouts (`each_platforms_layout_is_the_fixtures`). The host makes its own folders and passes only `--runtime-dir`, and `--dev` for its profile (`cmd_args`). **`Folders::make` follows each folder's links once, in `setup`**; callers take `&Folders`. On Windows no link may lead out of the profile. A config folder that was itself a link keeps its target's mode. **A root holding `..` is refused** (`plain_root`) and fails the launch. → [ADR: a folder per kind](../docs/adr/2026-11-22-each-folder-kind-has-its-own-folder-on-every-platform.md), [ADR: the sidecar places Kubetail's folders itself](../docs/adr/2026-11-23-the-sidecar-places-kubetails-folders-itself.md), [ADR: each file in the platform's directory for its kind](../docs/adr/2026-09-27-kubetails-files-follow-the-platforms-directory-kinds.md).
- **`app_menu/`** is the macOS menu bar; its Edit items are what deliver ⌘C/⌘V/⌘X/⌘A/⌘Z to the webview. Linux and Windows have none: the webview's `AppMenu` calls `new_window`/`quit`.
- **`tray/`** — Quit, Account Settings and the update item leave the app, so `app.rs` handles them. The account section is hidden for the MVP behind `SHOW_ACCOUNT_SECTION`, as the webview app bar leaves out `AccountAvatar`. The tray does not surface kube-context.
- **`icons/`** is generated by `make generate-desktop-icons`; never edit it. The macOS tray draws `icons/tray.png` as a template image.
- **`window_title.rs`** strips each part of a log window's title as `src/lib/visible-text.ts` strips, and caps it.
- **`os_theme.rs`**: every failure degrades to light.
- **`wake/`** fires the sidecar's `Poke` on wake-from-sleep and network-return; failures are logged and swallowed. → [ADR: poke resync fan-out](../docs/adr/2026-08-09-poke-resync-fanout.md).

## Logging

**Text on stderr always, plus JSON files** (`.json().flatten_event(true)`), rotated by size, in `~/Library/Logs/Kubetail{,-dev}` on macOS and `<state>/logs` on Linux and Windows, never `app_log_dir` (bundle id, no dev leaf). → [ADR: JSON logs rendered by the host](../docs/adr/2026-09-08-json-logs-rendered-by-the-host.md).

**The subscriber is built in two steps**, since the log directory needs an `AppHandle`: `app.rs` installs `logging::process_subscriber` (the registry, an empty `reload::Layer`, the `EnvFilter` and stderr), then `logging::install_file_layer` swaps the file layer in.

- **The reload layer goes first, directly on the registry**, or its handle's type cannot be written.
- **The swap is the first thing `setup` does, after `Folders::make` and before `SidecarService::spawn`**, so startup reaches the file.

**`main.log` and `webview.log` are split by target, by the writers, never by a per-layer filter**: `reload` cannot carry a `Filtered` layer. `webview.log` holds every record carrying `logging::WEBVIEW_TARGET`; `main.log` takes the sidecar's forwarded lines only in a release build (`is_host_log`). → [ADR: webview records get their own log file](../docs/adr/2026-09-08-webview-records-get-their-own-log-file.md).

- **The first `#[instrument]` in the host needs span rendering added to `TerminalFormat`.** Colour is guarded on `Writer::has_ansi_escapes`.
- **Failing to open a log never stops the app**; it runs on stderr alone.
- **The log file is flushed by hand.** `App::run` ends with `std::process::exit`, so no `Drop` runs: `RunEvent::Exit` calls `AppState::flush_log` last, and nothing logged after it reaches the file. Tests drop the guards before reading a file.
- **Every message is one line.** `logging::sanitize_text` escapes control characters and caps the result after escaping, on a character boundary. Every webview string and every line off the sidecar's pipes goes through it.
- **The level is `KUBETAIL_LOG_LEVEL` only.** A runtime setting would need its own `reload::Layer` around the `EnvFilter`.

**The sidecar writes `sidecar.log` itself**, in the host's log folder; `spawn` passes only `--log-stderr`, in debug. `logs.rs` re-emits each JSON pipe line under `logging::SIDECAR_TARGET`, extra fields folded into the message as `key=value`. **A line that will not parse passes through at `warn`** (a Go panic). **A message is sanitized before it is emitted**, since `serde_json` decodes its control characters (`a_forwarded_message_stays_on_one_line`). → [ADR: two processes, two log files](../docs/adr/2026-09-08-two-processes-two-log-files.md).

**Webview errors reach `webview.log` through `webview_record_error`.** The command reads the label off its `tauri::Webview` argument, never the payload. `stack`, `componentStack` and `context` ride together under **`reported`**, never flattened into host fields; the sanitized message is the one top-level page value. **The host bounds what it writes**: `MAX_*_BYTES` per field, truncated before serializing, never cut JSON. The command is `async`, off the main thread.

**Each webview is rate limited** (`AppState::webview_log`, keyed by label), and nothing is lost silently: a summary names what was withheld. Labels are `main`, `window-N` or `logs-N`, from one counter, never reused. **The globs in `capabilities/default.json` (`main`, `window-*`) and `capabilities/logs.json` (`logs-*`) must admit every label minted**, or the window silently gets no permissions.

## Windows

All chrome decisions live in `build_window` (`window_manager.rs`); `tauri.conf.json` declares **no windows** (a unit test pins this). → [ADR: per-platform window chrome](../docs/adr/2026-08-09-per-platform-window-chrome.md).

- **Building a window is a blocking call: use the blocking pool.** `WebviewWindowBuilder::build()` parks its thread until the webview exists; on the main thread that deadlocks Windows. Every off-main-thread entry point uses `tauri::async_runtime::spawn_blocking`, never `spawn`, which would park a worker serving `graphql_query`. Such commands take `AppHandle`. The macOS menus call from the main thread, which is fine there.
- **Windows are visible from creation, painted with the resolved scheme at t0** (`background_color_for`). There is **no reveal step**; do not reintroduce one. On macOS this depends on **`tauri/macos-private-api`** (mirrored by `app.macOSPrivateApi`); `background_color` is always passed on opaque platforms, and the document stays opaque. → [ADR: first-paint theming](../docs/adr/2026-08-09-first-paint-theming.md).
- **Every new window is sized to its monitor and positioned explicitly** (`fit_size`, `cascade_position`), on all three platforms. The monitors come through the `Monitors` trait, since the mock runtime has none.
- **The main window reopens at its saved frame** (`window_state.rs`) while it still `fits` a connected monitor. **Log windows cascade in a group per app window** (`LogGroups`), joined when placed, before the build, so a quick second open steps off it; never maximized. Frames are in the builder's logical px. A minimized or fullscreen window's frame is skipped, and a frame that `covers` its work area counts as maximized, as Windows reports a maximize's move before the window reads maximized.
- **The traffic-light constants track the webview's**: the bar heights in `app-bar/bar.tsx` and `log-window/bar.tsx`, the gutter in `traffic-light-gutter.tsx`'s `TRAFFIC_LIGHT_GUTTER`. **The position's `y` is not the gap above the buttons**: they keep an 8pt inset from the container's bottom, which `traffic_light_position` derives. `LIGHT_BACKGROUND`/`DARK_BACKGROUND` track `@kubetail/ui`'s `--background` by eye.

## Log windows (`log_windows.rs`)

A log window (`logs-N`) shows one tool call's logs, paired to the app window that opened it. `LogWindows` holds the pair behind one `Mutex`. An app window's **page** is a number each `log_window_subscribe` mints; it ends when its window is destroyed or its page reloads. **Every app-window command names its page** and is refused on an ended one. **Every command reads the caller's label off its `Webview`** and checks the label's kind, beside the capability. → [ADR: a log window belongs to one app window](../docs/adr/2026-11-04-a-log-window-belongs-to-one-app-window-and-the-host-keeps-the-pair.md), [log windows review](../docs/security/2026-11-04-log-windows.md).

- **An entry is `Building`, then `Live`.** `log_window_open` reserves under the lock, then builds on the blocking pool; a second open waits on the first's outcome. A close or destroy during the build removes the entry and answers its waiters with `AppError::Cancelled`; the build then finds no entry and destroys its window. The app window never sees a `Building` entry.
- **The lock guards the maps and the channel sends, and nothing else.** Every frame is sent under it with the change it reports, so `snapshot` comes first and nothing races it. Transitions return what the wiring does next; windows are built, surfaced and destroyed after the lock is released.
- **The host destroys the log windows it closes, never `close()`s them**: a page can cancel `CloseRequested`, and a log window must not outlive its app window. An app window's `Destroyed` destroys its log windows (`lib.rs::window_destroyed`).
- **A log window's page load steps its generation**, and `log_window_viewport` refuses a push from an earlier one. The push is bounded (256 KiB) and opaque past `generation` and `grep`; the host forwards its `viewport` unread.

## Sidecar IPC (`services/sidecar/`)

The sidecar is a bundled child process on a per-instance socket picked before the spawn, inside an owner-only runtime directory (`paths.rs::runtime_dir`).

- **Stdout carries the readiness protocol and nothing else**: `READY `, or `FATAL <reason>` when the sidecar cannot compose its app, which `ready()` returns for the start screen to show.
- **`SidecarService::ready()` waits on `READY_BUDGET` (30s), not `DEFAULT_CONNECT_BUDGET`**, since the sidecar runs the login shell before it binds. `ReadyGate` does not retry, so one timeout is an error screen.
- **Closing the last window does not exit the app**; only Quit or a Unix signal does. Dropping the child closes stdin, and on EOF the sidecar runs its stop sequence.
- **The commands carry any operation and are named for the transport**, never for a read contract. → [`docs/read-contracts/graphql.md`](../docs/read-contracts/graphql.md#the-transports-names).
- **Queries and mutations** take one fresh HTTP/1 connection per call (`graphql/query.rs`), within the 30s `REQUEST_BUDGET`.
- **Subscriptions** take one SSE connection each (`graphql/subscribe.rs`), relayed as `{type,payload}` envelopes without knowing their kind. Graceful completion sends `{"type":"complete"}`, an abnormal drop `{"type":"closed"}`, read off body framing, since gqlgen's `event: complete` is data-less. **`{"type":"open"}` is sent only after the 200**, never on a failed dial or the ack; the webview keys its resets on it. → [ADR: transport-status generation](../docs/adr/2026-08-09-transport-status-generation.md).
- **gRPC** (`grpc.rs`) uses one cached `Channel` over the same socket, re-dialed on drop; its bindings come from `build.rs` over `proto/`.

## Graceful shutdown (`AppState::shutdown`)

Quit cancels the app-wide `CancellationToken` before `SidecarService::graceful_shutdown`. **Every app-lifetime background task holds a clone and `tokio::select!`s on `shutdown.cancelled()` in each loop and await.** Per-subscription teardown is the `oneshot` cancel table in `graphql/subscribe.rs`, keyed by op id and tagged by webview.

**The wait for the sidecar never runs on the event loop**, where it would freeze the app for up to `SIDECAR_SHUTDOWN_GRACE` (23s, above the sidecar's 20s `shutdownBudget`). The quit state is `AppState::quit` (`quit.rs`):

- **The first `ExitRequested` with a code** moves it from *running* to *waiting*, cancels `shutdown`, prevents the exit, hides every window, and starts a thread that waits, moves to *done*, and calls `exit` again (`lib.rs::start_quit`).
- **Every `ExitRequested` while *waiting* is prevented.** Every Quit path reaches the same `app.exit(0)`, and letting a second through would run `RunEvent::Exit` and kill the sidecar mid-stop.
- **No window opens once a quit has begun.** Every path that opens or shows one ends in a `WindowManager` method (`new_window`, `show_main_window`, `open_log_window`, `surface_window`), which checks the `shutdown` token on entry and again on the main thread behind what it presented, even when a step failed. A new path needs no check of its own; a refused ask logs a `warn`.
- `RunEvent::Exit` keeps the kill as the backstop and warns when it was needed.
- A macOS logout skips all of this: the host answers `NSTerminateNow` and the kill runs at once.

## Security invariants

What the host owns, beside [`docs/security-model.md`](../docs/security-model.md):

- **The webview never navigates off the app origin.** `build_window` installs `on_navigation(is_app_origin)`, an allowlist ending in `false` (app origin, `about:blank`, the dev server in debug), and `on_new_window`, always `Deny`. A feature that needs an external URL needs a host command. Verified by hand, not by a test.
- **A window calls only what its capability allows.** `build.rs`'s `COMMANDS` lists every command, and tauri-build writes an `allow-<command>` permission for each (underscores as hyphens) into the gitignored `permissions/autogenerated/`; a new command goes in `invoke_handler()`, `COMMANDS` and a capability (`every_command_is_in_the_app_manifest`). `capabilities/default.json` grants no emit, so a page cannot send an event as the host; no menu, tray, image, resources, path or app API; no shell, opener or updater grants. `capabilities/logs.json` grants nothing that opens a window, changes a preference, quits or opens a URL. Pinned by `default_capability_grants_what_the_app_window_calls` and `logs_capability_grants_the_viewer_and_nothing_else`. Add a permission only when a call site needs it, scoped as narrowly as the plugin allows.
- **Every dial checks who answered.** An address and the pid that must serve it travel together as one `ipc::Target`. The pid is read from the kernel (`peer.rs`), published by `spawn` and cleared when the child exits; `None` or an unreadable peer refuses. Pinned by `connect_refuses_a_peer_that_is_not_the_expected_process` and `query_refuses_a_server_that_is_not_the_sidecar`.
- **Nothing the sidecar needs may come from this process's environment**, which it inherits. Every endpoint is passed by `cmd_args` from constants in `service.rs`; new configuration goes the same way. Kubetail's folders are the exception: the sidecar reads their roots from the environment, as the host does, and `layouts.json` holds the two layouts to one answer. `KUBETAIL_*` overrides work only in a `-tags debug` sidecar (`make build-desktop-sidecar-dev`).
- **The host tears down a webview's subscriptions** (`lib.rs::cancel_webview_subscriptions`) on `PageLoadEvent::Started` and `WindowEvent::Destroyed`, since a reload runs no JS teardown. Pinned by `cancel_webview_drops_only_that_webviews_subscriptions`.
- **Structured text from the webview never becomes a host log field or a second log line**: `reported` alone, sanitized, capped and paced. Pinned by `page_supplied_fields_ride_under_one_key` and the sanitizer tests.
- **The macOS entitlements are exceptions.** `disable-library-validation` and `allow-unsigned-executable-memory` remain unverified pending signed macOS testing (H-3). Don't add an entitlement without saying in `entitlements.plist` what needs it.

## Conventions

- **No `unwrap`/`expect`** (`#![warn(clippy::unwrap_used)]`). Poisoned mutexes: `.lock().unwrap_or_else(|p| p.into_inner())`.
- **Transport failures cross the command boundary as `AppError::Io`**, so urql sees a `networkError` and retries. **HTTP non-2xx is not an error**: return `Ok(GraphqlResponse{status, body})`.
- Menu and tray callbacks are sync and can't return errors: log failures, and use `tauri::async_runtime::spawn` for async work. Queue an off-main-thread menu change with `app.run_on_main_thread(...)`.

## Tests and checks

- Inline `#[cfg(test)] mod tests` with `#[tokio::test]`; UDS/pipe fixtures via `interprocess`. Factor pure logic into free functions to unit-test it.
- **Code that holds an `AppHandle` is generic over `R: Runtime`**, so tests run it on Tauri's mock runtime (`testutil.rs`). Where the mock falls short, the seam is a parameter: `Folders::make` takes `paths::Roots` (`testutil::folders(scratch)`), since Tauri's directory override does not reach the platform roots; `SidecarService::spawn` takes its command (`testutil::script_sidecar`); `WindowManager` takes its monitors (`testutil::OneMonitor`); a quit path takes its exit, since `AppHandle::exit` is unimplemented there. `services/sidecar/testutil.rs` serves fake gRPC services. AppKit builds menus on the main thread alone, which no test runs on, so menu-building tests are `cfg(not(target_os = "macos"))`.
- **A test reads its logs through `logging::captured` or `capture_thread`.** The first capture sets a global default that writes nothing, so a callsite another thread reaches first stays live for the capture; no test sets another global default.
- **The suite runs under both profiles.** A PR tests the dev profile; a push to `main` also tests the release profile (`ci-desktop.yml`'s `Build and Test · Rust release`) on `release-desktop.yml`'s runners. A test of a `cfg!(debug_assertions)` branch asserts against `cfg!(debug_assertions)`, so each profile pins its own half (`is_app_origin`'s). `cargo test --release` runs it locally. → [ADR: main builds and tests the release profile](../docs/adr/2026-11-10-main-builds-and-tests-the-release-profile.md).
- **Coverage is measured by `coverage-desktop.yml`'s `Measure · Rust`** (`cargo llvm-cov`, one runner per OS), which runs `tools/coverignore -lcov` over the report before the upload. Where a marker's reason is the real runtime, say what of it the line needs ("needs a real window system's tray"). `app.rs` and `wake/macos.rs` are marked item by item; a file with no reachable line is in `.github/codecov.yml`'s `ignore`. An `async` command's `#[tauri::command]` line counts its last argument's parse failure, which `new_window`'s lone `AppHandle` cannot produce, so that line is marked.
- `make lint-desktop` runs `cargo fmt --check`, `make vet-desktop` `cargo clippy --all-targets -- -D warnings`.
- **The compiler is pinned** in `rust-toolchain.toml`. rustup resolves it from the working directory, so **a `rustup`/`cargo` call from the repo root needs `working-directory: ./src-tauri`**, or it silently uses the default toolchain.
