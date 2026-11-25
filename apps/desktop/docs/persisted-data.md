# Persisted data

Everything Kubetail keeps across a restart, how each format is versioned, and what a build does with
a file a newer build wrote. Present tense, like a `CLAUDE.md`: this file states what is true now.
The policy and its reasons are in two ADRs: [each format carries its own version
stamp](adr/2026-10-29-each-persisted-format-carries-its-own-version-stamp.md), and [a shipped
schema changes only by a new migration](adr/2026-11-08-a-shipped-schema-changes-only-by-a-new-migration.md).

**A change to a persisted format updates its row here in the same commit.**

No release has shipped a format yet. Each store's schema is one `0001_init.sql`. The first Kubetail
Desktop release, `desktop/v0.1.0`, ships every format: from then on a build has to read it, and a
schema change is the next numbered migration.

## Policy

A downgrade is unsupported. Each format's own stamp is the enforcement: a build whose formats match
the files on disk opens them, whatever its version. There is no app-level marker and no app-level
migration step; every format migrates when it opens.

- **Durable**: user data. Carries a version and migrates forward from every shipped version. A
  file written by a newer Kubetail is refused with a message naming the file and both versions.
- **Preference**: a choice that is cheap to lose. A newer file is read, and a write keeps what
  this build does not know, so a dev build and a release can share it.
- **Disposable**: a store Kubetail rebuilds. A bad value reads as the default.

A migration is never removed: a user may skip releases.

### How a change ships

Each kind of change has one shape. The tests named here are what fails when it is skipped.

- **A SQLite schema** (`app.db`, `kube/*.db`, `logs/*.db`): the next numbered `*.sql` in the
  store's `migrations/`, plus its SHA-256 line in the store's `shippedMigrations`. A shipped
  migration is never edited or removed: `TestAShippedMigrationIsNeverEdited` fails on a changed,
  added or missing file. The migration ships with a test that builds the previous schema from
  the frozen files, seeds it, migrates and reads the seed back. `kubestore`'s triggers name
  their tables, so rebuilding one drops the triggers first and recreates them after. A migration
  runs on the file in place, with no copy; the first `app.db` migration past `0001` waits for the
  backup in [#823](https://github.com/kstackhq/kstack/issues/823).
- **A JSON column**: rides `app.db`'s version, so the change is an `app.db` migration using
  SQLite's JSON functions. A rewrite SQL cannot express has no path yet (below). A new key a
  row may lack, read as its zero value, needs none: no stored row changes.
- **A preference** (`host.json`): a field's layout change bumps `schemaVersion` and upgrades
  in the reader. A file a build cannot parse reads as defaults and its next write replaces it, so
  a new choice for a setting is a new field, never a new value of an old one.
- **A disposable store**: no migration. A renamed `localStorage` key needs nothing; a renamed
  keyring entry deletes the old one.

### What cannot change yet

There is no app-level data migration: nothing runs on upgrade except each format's own
migrations, and a SQL migration changes tables only. So nothing can move, rename or remove a
path under `<data>` or `<cache>`, or rewrite a JSON column in Go. Until that exists:

- **The layout under `<data>` and `<cache>` only grows.** A new path needs nothing: its owner
  creates it, and this register gains a row. An existing path keeps its name and its root.
- **The data root never moves.** Transcripts name absolute paths no migration can rewrite.

Adding the mechanism is in [`TODO.md`](TODO.md), *Sidecar (Go)*.

## Where it lives

| Root | Resolved by | Holds |
| --- | --- | --- |
| `<data>` | the sidecar's `layout.Of`, made by `app.ResolveFolders` | what a user would lose |
| `<cache>` | the sidecar's `layout.Of`, made by `app.ResolveFolders` | the cluster and log caches |
| `<config>` | `paths.rs` `Folders::make` | `host.json` |
| `<state>` | `paths.rs` `Folders::make` | the window state, and on Linux and Windows the logs, in `logs/` |
| OS keyring | service `<service>`: `Kubetail` (`Kubetail-dev` in debug; `KEYCHAIN_SERVICE` in `src-tauri/src/services/sidecar/service.rs`, and a sidecar run with `--dev` and no `--keychain-service`) | the Kubetail account's, the ChatGPT plan's and the Google account's refresh tokens, the OpenRouter connector's key, the AWS connector's session, the saved provider API keys |
| Webview storage | the webview's profile: `<state>/webview` on Linux and Windows (`paths.rs` `webview_dir`), WKWebView's own under the bundle id on macOS | `localStorage` under `kubetail:*`, split by origin |

Each resolves through `paths.rs` `layout` and the sidecar's `layout.Of`, which `sidecar/internal/app/layout/testdata/layouts.json` holds to one answer, and which give every kind its own folder on every platform:
none is another's, and none sits inside another's. On Linux that holds for the default XDG roots. An override can undo it: two roots set to one
directory give their kinds one `kubetail` folder, and a root set inside another's `kubetail` folder
puts its kind inside the other's. Each platform's folders are listed below. The
leaf is `kubetail` (`kubetail-dev` in debug) on Linux and `Kubetail` (`Kubetail-dev`) elsewhere. The
caches live under `<cache>` because the OS may reclaim that root; they are still durable, since they
hold history the cluster no longer has. `make clean-desktop-data` clears the dev build's `<data>`, `<cache>`,
`<config>` and `<state>`, with `<state>` its webview profile on Linux and Windows, on macOS and
Windows the app folder that holds them, on Windows the `%APPDATA%\Kubetail-dev` folder that holds
the config, and on macOS the logs.

A dev build and a release never read each other's keyring entries or webview storage. The keyring
entries sit under different services. `localStorage` is per origin: a dev build loads
`http://localhost:1420` (`devUrl` in `tauri.conf.json`), a release `tauri://localhost`
(`http://tauri.localhost` on Windows). On macOS the profile folder is shared, but its storage is
not. `make clean-desktop-data` leaves the macOS profile and the `Kubetail-dev` keyring entries alone.

### Every folder, by platform

A release build's paths; a dev build's leaf ends in `-dev`. The sidecar owns the data, cache and
runtime folders and writes `sidecar.log`; the host owns the rest. The tree under each sidecar folder
is the doc comment on `paths` in `sidecar/internal/app/paths.go`.

**Linux.** An XDG variable that is unset or relative reads as its default, shown here.

| Path | Kind | Holds |
| --- | --- | --- |
| `~/.local/share/kubetail/` | `<data>` | `app.db`, `beehive.db`, `chats/`, `monitor/`, `sidecar.lock` |
| `~/.cache/kubetail/` | `<cache>` | `kube/`, `logs/` (the log cache), `kubectl/`, `tmp/` |
| `~/.config/kubetail/` | `<config>` | `host.json` |
| `~/.local/state/kubetail/` | `<state>` | `window-state.json` |
| `~/.local/state/kubetail/logs/` | logs | `main.log`, `webview.log`, `sidecar.log` |
| `~/.local/state/kubetail/webview/` | webview profile | `localStorage`, WebKitGTK's cache |
| `$XDG_RUNTIME_DIR/kubetail/`, else `/tmp/kubetail-<uid>/` | `<runtime>` | the IPC socket, `shell/`, `runs/` |

**macOS.**

| Path | Kind | Holds |
| --- | --- | --- |
| `~/Library/Application Support/Kubetail/data/` | `<data>` | `app.db`, `beehive.db`, `chats/`, `monitor/`, `sidecar.lock` |
| `~/Library/Application Support/Kubetail/config/` | `<config>` | `host.json` |
| `~/Library/Application Support/Kubetail/state/` | `<state>` | `window-state.json` |
| `~/Library/Caches/Kubetail/` | `<cache>` | `kube/`, `logs/` (the log cache), `kubectl/`, `tmp/` |
| `~/Library/Logs/Kubetail/` | logs | `main.log`, `webview.log`, `sidecar.log` |
| `$TMPDIR/kubetail-<uid>/` | `<runtime>` | the IPC socket, `shell/`, `runs/` |
| WKWebView's own, under the bundle id `com.kubetail.desktop` | webview profile | `localStorage`; a dev build and a release share the folder, each origin its own storage, and `make clean-desktop-data` leaves it |

**Windows.**

| Path | Kind | Holds |
| --- | --- | --- |
| `%LOCALAPPDATA%\Kubetail\data\` | `<data>` | `app.db`, `beehive.db`, `chats\`, `monitor\`, `sidecar.lock` |
| `%LOCALAPPDATA%\Kubetail\cache\` | `<cache>` | `kube\`, `logs\` (the log cache), `kubectl\`, `tmp\` |
| `%LOCALAPPDATA%\Kubetail\state\` | `<state>` | `window-state.json` |
| `%LOCALAPPDATA%\Kubetail\state\logs\` | logs | `main.log`, `webview.log`, `sidecar.log` |
| `%LOCALAPPDATA%\Kubetail\state\webview\` | webview profile | `localStorage`, in WebView2's `EBWebView\` |
| `%LOCALAPPDATA%\Kubetail\run\` | `<runtime>` | `shell\`, `runs\`; the IPC endpoint is a named pipe |
| `%APPDATA%\Kubetail\config\` | `<config>` | `host.json` |

## Register

| Format | Where | Owner | Class | Version stamp | Forward path | Newer file | Pinned by |
| --- | --- | --- | --- | --- | --- | --- | --- |
| App DB | `<data>/app.db` | `internal/appdb` | durable | `sqlitemigrate` | SQL migrations | refused; the sidecar does not start and the start screen names the file | `appdb` `TestAShippedMigrationIsNeverEdited`, `TestOpenRefusesANewerFile`, `TestAtMostOneClusterIsTheDefault`; the message: `TestANewerAppDBFailsTheBuildNamingTheVersions`, `TestRunFailsWhenTheAppCannotBeComposed`, `wait_ready_returns_the_sidecars_reason` |
| App DB JSON columns | `app.db`: message content blocks, server-tool counts, tool-call input and error, `ActionRequest` (its `cloud` optional; its action's keys `principal`, `target`, `scope`, `accountName`, `service`, `operation`, `resource`, `name`, `accessLevel` by name, `confined`, `once`, `spans`, `summary` and `dryRun`), watch result | `agent/chat`, `tools` | durable | rides `app.db`'s | an `app.db` migration | with `app.db` | `TestTheSettingsTablesHoldWhatTheirChecksAllow`, `TestACloudRequestRoundTripsAsJSON`, `TestClusterWritesReadOldRowsTheSame` |
| Control-plane store | `<data>/beehive.db` | `github.com/amorey/beehive/sqlite` | durable | beehive's own runner | beehive's migrations, taken by a module bump | refused; the sidecar does not start and the start screen names the file | beehive's `internal/sqlitemigrate` tests; the message: `TestANewerBeehiveDBFailsTheServiceSayingSo`, `TestRunFailsWhenTheAppCannotBeComposed`, `wait_ready_returns_the_sidecars_reason` |
| Chat and monitor directories | `<data>/chats/<chat id>/`, `<data>/monitor/<cluster id>/`: `results/`, `tasks/`, `workspace/`, `toolhome/` (each tool's caches and configuration, `gcloud`'s among them) | `agent/chat`, `tools` | durable | none | none yet: the layout only grows and the root never moves (*What cannot change yet*) | a path this build does not know is left alone | — |
| Cluster cache | `<cache>/kube/<cache id>.db` | `kubestore` | durable | `sqlitemigrate` | SQL migrations | refused; the cache reports `StoreFailed` with the message and the rest of the app runs | `kubestore` `TestAShippedMigrationIsNeverEdited`, `TestOpenRefusesANewerFile`, `TestTheEventsAreIndexedByInvolvedNamespace`; `kubesync` `TestArmReportsAStoreThatWillNotOpen` (any open failure reaches `StoreFailed`) |
| Cluster cache `raw_json` | `kube/*.db` `objects.raw_json` | `kubestore/rawcodec.go` | durable | first byte (zlib `0x78`, JSON `{`) | a `kube/*.db` migration | with `kube/*.db` | `rawcodec_test.go` |
| Log cache | `<cache>/logs/<cache id>.db` | `logstore` | durable | `sqlitemigrate` | SQL migrations | refused; `OpenOrCreate` returns the error to the caller | `logstore` `TestAShippedMigrationIsNeverEdited`, `TestOpenOrCreateRefusesANewerFile` |
| Host file | `<config>/host.json` | `src-tauri/src/host_file.rs` | preference | `schemaVersion` 1 | none | read; a write drops unknown fields and stamps 1 (accepted gap, ADR; [#824](https://github.com/kstackhq/kstack/issues/824)) | `update_stamps_current_version` |
| Window state | `<state>/window-state.json`: the main window's logical frame and `maximized`, and the log windows' last `width` and `height`; `make clean-desktop-window` and `make clean-desktop-data` delete the dev build's | `src-tauri/src/window_state.rs` | disposable (windows open at their default sizes) | `schemaVersion` 2 | version 1 is read as is, with no log size | unknown versions and invalid files are ignored, and the next write replaces them; a write keeps the other entry of a file it can read | `the_file_is_stamped_with_its_schema_version`, `a_version_one_main_frame_survives_a_log_size_write`, `each_entry_survives_a_write_of_the_other` |
| OAuth connections | `app.db` `oauth_connections`, a row per provider signed in to with OAuth: issued client id (NULL for a fixed client), subject, email, name, the session id its keyring entry must name; `provider_data` is one provider's own JSON (ChatGPT: `{"hostID"}`); no token | `internal/appdb`, `services/connectors/internal/oauthconn/registration.go` | durable | rides `app.db`'s | an `app.db` migration | with `app.db` | `TestTheOAuthConnectionsHoldOneRowPerProvider`, `TestARegistrationRoundTrips`, `TestARegistrationLeavesOtherProvidersAlone` |
| User's variables | `app.db` `env_vars`, a row per variable: name, value, target (sandbox, outside or both) and an optional cluster, in the order added; `tool_calls.env`, the variables a command outside the sandbox was shown | `internal/appdb`, `services/envvars`, `agent/chat` | durable | rides `app.db`'s | an `app.db` migration | with `app.db`; a row the check refuses is listed with its reason and given to no command | `TestTheUsersVariablesHoldOneNamePerScope`, `TestARefusedRowIsListedWithItsReasonAndGivenToNoCommand` |
| Single-value settings | `app.db` `settings`, a row per setting and scope the user has set: a dotted `key` (`sandbox.<platform>.enabled`, `sandbox.path`, `tools.<name>.enabled`), an optional cluster (NULL for every cluster; a cluster's goes with it), a `value` that is an integer, text, or NULL for the default chosen, and `created_at`, `updated_at`; no row is the default, and a row is never deleted but with its cluster | `internal/appdb`, `services/settings`, `services/toolsettings`, `agent/chat` | durable | rides `app.db`'s | an `app.db` migration for the table; a new setting is an entry in `services/settings` and needs none | with `app.db`; a value Kubetail cannot use reads as its entry's strictest value (a switch or a tool off) or its default, logged by key alone; a `PATH` it cannot use is listed with its refusal and starts no run; a row whose key no entry has is kept and ignored; a failed read is an error | `TestTheSettingsTablesHoldWhatTheirChecksAllow`, `TestASettingKubetailCannotUseReadsItsStrictestOrDefault`, `TestASettingReadThatFailsIsAnError`, `TestASwitchValueKubetailCannotUseIsOff`, `TestARowKubetailCannotUseTurnsItsToolOff`, `TestARefusedPathSettingIsReadWithItsRefusal`, `TestAPathSettingThatIsNotTextIsReadWithItsRefusal`, `TestSetWritesOneRowPerToolAndKeepsIt`, `TestRowsOutsideTheListAreIgnored`, `TestAnAlwaysOnToolStaysOn` |
| Permission rules | `app.db` `permission_rules`, a row per rule: effect, access level, principal and target by name or `*`, and the scope, service, operation and resource patterns, with `created_at` and `updated_at` (an edit keeps the id and moves `updated_at` alone); an always rule with `chat_id` NULL, a chat's with it set, gone with the chat; a Kubernetes rule whose scope is a cluster's id or starts with it and `/` goes with the cluster (a trigger on `clusters`) | `internal/appdb`, `services/settings`, `agent/chat` | durable | rides `app.db`'s | an `app.db` migration | with `app.db`; the schema enumerates nothing, `settings.CheckRule` refuses on write what Go cannot apply, and a stored row it refuses reads as a refusal of every write and data read, logged by id | `TestTheSettingsTablesHoldWhatTheirChecksAllow`, `TestTheGoCheckRefusesWhatNoRuleMayHold`, `TestARowGoCannotReadDenies`, `TestARuleRoundTripsThroughItsRow`, `TestAnAlwaysRuleIsEditedInPlace`, `TestAChatsRuleRoundTripsThroughItsRow` |
| Permission modes | `app.db` `permission_modes`, a row per target and account (`*` for the target's default): the mode by name and `created_at`, `updated_at`; a Kubernetes account's row goes with its cluster (the same trigger), a cloud account's stays until the user clears it | `internal/appdb`, `services/settings` | durable | rides `app.db`'s | an `app.db` migration | with `app.db`; a mode Kubetail cannot use reads as read-only; a failed read is an error, which a decision reads as read-only | `TestTheSettingsTablesHoldWhatTheirChecksAllow`, `TestAModeIsItsAccountsThenItsTargetsThenAsk`, `TestAModeKubetailCannotUseIsReadOnly` |
| Folder grants | `app.db` `folder_grants`, a row per granted folder, absolute and resolved, with its access (`read` or `write`); one per folder always (`chat_id` NULL) and one per folder in each chat, gone with the chat | `internal/appdb`, `services/settings`, `agent/chat` | durable | rides `app.db`'s | an `app.db` migration | with `app.db`; a grant that fails its check on read is listed with its refusal and given to no run | `TestTheSettingsTablesHoldWhatTheirChecksAllow`, `TestAlwaysFolderGrantsAreGrantedChangedAndRevoked`, `TestAGrantIsListedWithItsRefusal` |
| The login shell's last `PATH` | `app.db` `shell_path`, one row (`id` 1) once a read of the shell answered: its `PATH`, joined by `:` | `internal/appdb`, `services/settings` | durable | rides `app.db`'s | an `app.db` migration | with `app.db`; a copy every run checks again, so no row is refused; a failed read is an error | `TestTheSettingsTablesHoldWhatTheirChecksAllow`, `TestTheShellPathRow`, `TestTheShellsPathOutlivesARestart` |
| Registered executables | `app.db` `executables`, a row per program the user asked the probe to check: its `name`, `invocation` and `created_at`, read in the order added | `internal/appdb`, `services/settings` | durable | rides `app.db`'s | an `app.db` migration | with `app.db`; a row `CheckExecutable` refuses is listed with its refusal and never probed, and a failed read is an error | `TestTheSettingsTablesHoldWhatTheirChecksAllow`, `TestARefusedExecutableIsListedWithItsReason` |
| OAuth refresh tokens | OS keyring, service `<service>`, account `<provider>-refresh-token` (`chatgpt-refresh-token`, `kubetail-cloud-refresh-token`, `google-refresh-token`): `{"v":1,"token","session"}` | `services/connectors/internal/oauthconn/keyring.go` | disposable (sign in again) | `v` 1 | a renamed entry deletes the old one | read as no session and deleted | `TestNewRestoresWhatTheStoresHold` |
| OpenRouter key | OS keyring, service `<service>`, account `openrouter-api-key`: `{"v":1,"key"}` | `services/connectors/openrouter/keyring.go` | disposable (connect again; the old key stays live on OpenRouter) | `v` 1 | a renamed entry deletes the old one | a newer `v` is left in place; one that does not parse is deleted | `TestNewRestoresWhatTheKeyringHolds`, `TestANewerEntryIsLeft` |
| Saved provider keys | OS keyring, service `<service>`, account `<id>-api-key` per keyed provider (`openrouter-saved-api-key` for OpenRouter): `{"v":1,"key"}` | `services/providercredentials/keyring.go` | disposable (save again; the key stays live at its vendor) | `v` 1 | a renamed entry deletes the old one | a newer `v` is left in place; one that does not parse is deleted | `TestTheAccountNames`, `TestNewRestoresWhatTheKeyringHolds`, `TestANewerEntryIsLeft` |
| AWS session | OS keyring, service `<service>`, account `aws-session`: `{"v":1,"refresh","dpopKey","arn"}` | `services/connectors/aws/keyring.go` | disposable (connect again; the old session stays valid at AWS until it expires) | `v` 1 | a renamed entry deletes the old one | a newer `v` is left in place; one this build cannot use is deleted | `TestAnEntryReadsBackAsItWasSaved`, `TestANewerKubetailsEntryIsLeft`, `TestAnEntryThisBuildCannotUseIsDeleted` |
| GitHub token | OS keyring, service `<service>`, account `github-token`: `{"v":1,"token","login"}` | `services/connectors/github/keyring.go` | disposable (connect again; the old token stays authorized on GitHub) | `v` 1 | a renamed entry deletes the old one | a newer `v` is left in place; one that does not parse is deleted | `TestNewRestoresWhatTheKeyringHolds`, `TestANewerEntryIsLeft` |
| Pane state | `localStorage` `kubetail:sidebar-open`, `kubetail:sidebar-width`, the right sidebar's per-mode open and width, `kubetail:log-viewer-wrap`, the dashboard nav's `kubetail:dashboard-nav-open:<section>/<group>`, `kubetail:chatgpt-plan-confirmed` (the ChatGPT plan's first confirmation was seen) | `src/lib/persisted-flag.ts`, `persisted-width.ts`, `settings-dialog/connectors.tsx` | disposable | none | a bad value reads as the default | — | `persisted-*.test.ts`, `connectors.test.tsx` |
| Log-viewer columns | `localStorage` `kubetail:log-viewer-columns` | `log-viewer/columns.ts` | disposable | none | unknown names dropped, bad JSON reads as the default | — | `columns.test.ts` |
| Table columns | `localStorage` `kubetail:table-columns:<apiVersion>/<resource>`, one per kind: `shown` (column key → shown) and `order` (column keys) | `src/lib/table-columns.ts` | disposable | none | a part of the wrong shape reads as no choices; an unknown key is ignored | — | `table-columns.test.tsx` |

## Not persisted formats

Kept on disk but never read back as a format, or not kept across a restart:

- **Logs**: the host's and the sidecar's text logs, in `<state>/logs` on Linux. Appended, never parsed.
- **`<cache>/kubectl/`**: kubectl's own cache, in kubectl's format.
- **`<cache>/tmp/`, `<runtime>/`**: a run's scratch, swept at start.
- **`sessionStorage`** `kubetail:history-forward-ceiling`, `kubetail:chatgpt-plan-pending` (a sign-in
  this window started awaits the plan's confirmation): one window session.
- **Provider API keys from the environment**: passed in at launch (`cfg.LLMKeys`), never written.
  A key saved from the Providers pane is the *Saved provider keys* row.
- **Provider payloads** inside the message blocks: a cache the provider may invalidate. A payload
  replays only to the provider and dialect its run recorded; any other target is sent Kubetail's
  fields alone. Kubetail's own block format is what migrates.

## Releasing

A release owes this register nothing by itself: every format migrates when it opens. The
updater's restart waits for the old sidecar to exit before the new one migrates:
`stop_for_install` runs before the restart is requested, and on Windows before the installer
(`src-tauri/src/updater/CLAUDE.md`, *The install*).
