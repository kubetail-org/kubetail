# sidecar — Go backend

A Go binary the Tauri host starts. It serves GraphQL and a gRPC control channel and owns all
Kubernetes logic. **No TCP**: it listens on a Unix socket (named pipe on Windows), prints
`READY unix:<path>` and exits on `SIGINT`/`SIGTERM` or **stdin EOF**. **It places Kubetail's
folders itself** (`internal/app/layout`, from its environment's roots), as the host places its own
from the same roots; `layout/testdata/layouts.json` pins both. The host passes only `--runtime-dir`
(required, absolute, no `..` on Unix) and `--dev`, its profile. Run tests point the roots at
`t.TempDir()` (`scratchRoots`). → [ADR](../docs/adr/2026-11-23-the-sidecar-places-kubetails-folders-itself.md).

**`<data>/app.db` belongs to `internal/appdb`, and the app owns it.** `build`
(`internal/app/runtime.go`) opens it before any service, under the data directory's lock, and hands
the `*appdb.DB` to each service that uses it; a service closes only the statements it prepared.
`appdb.Open` gives a one-connection migrated writer and a `query_only` reader. A service's
statements are a `sqlstmt.Set` on `db.Write, db.Read`. Tests open the DB through `appdb/appdbtest`,
never `appdb.Open`. **The change bus**: `Notify(key)` after commit, never in it;
`ListenForChanges` before the first read; never count notices, which coalesce. Ids are `NewID()`
(UUIDv7); `ValidateUUID` accepts only canonical v4/v7.
→ [ADR: the app owns app.db](../docs/adr/2026-09-16-the-app-owns-app-db.md).

## Directories

**`app/paths.go` names every path under the three directories** (its doc comment is the tree).
Each owner gets its own `Paths` (`cluster.Paths`, `bash.Paths`), naming no other. **Each subtree
has one owner**, which makes it 0700, sweeps and removes it.

**`app.ResolveFolders` follows links once, before the log opens**, checks the layout, then makes
the data and cache 0700. `Config.Folders` is resolved and fenced (`DeniedDirs`); `makeSubfolders`
refuses a linked subfolder; nothing writes a link's parent. →
[ADR](../docs/adr/2026-11-24-follow-a-link-to-a-kubetail-folder-once-at-start-and-refuse-one-below-it.md),
[ADR: each file in the platform's directory for its
kind](../docs/adr/2026-09-27-kubetails-files-follow-the-platforms-directory-kinds.md).

**One sidecar per data directory.** `build` locks `<data>/sidecar.lock` (`internal/lib/filelock`)
before anything opens a file there; the lock is the runtime's first part, so it closes last. A
second sidecar fails `app.New` (*another Kubetail is using `<data>`*), printed as `FATAL`. The
kernel drops the lock when the process ends, a crash included. `build` runs after the `--keys-fd`
re-exec, which a lock would not survive, and the `sandbox-*` subcommands never take it. **Never
delete `sidecar.lock`**: a second process would lock a new file under the same name while the first
still holds the old one.

## Guides

This file holds the process, its directories, the layering, the socket, the security invariants,
the small services and the conventions every package follows. The larger packages have their own:
`internal/services/cluster`, `services/logs`, `services/settings`, `services/providercredentials`,
`services/connectors/internal/oauthconn`, each connector under `services/connectors/`, `internal/llm`,
`internal/run`, `internal/tools` and `internal/agent`.

## Layout

`main.go` is lifecycle only; `internal/app` is the composition root; GraphQL is `graph/`.

**`build` fills one `Runtime`** (`internal/app/runtime.go`), a field per service. A service never
sees the runtime: it takes constructor arguments. Field order is build order is start order
(`rt.add`); stop and close reverse it. An edge back to a later service is a setter at the end of
`build` (`SetAlwaysFolders`, `SetCacheFiles`). **kubeconfig before cluster is load-bearing**
(`app_test.go`). → [ADR: the runtime is built in order](../docs/adr/2026-10-06-the-runtime-is-built-in-order.md).

**`main` runs `sandbox.Main` first** (the `sandbox-*` subcommands), then `scrubExecEnvironment`
(`scrubexec_unix.go`): started with a provider key variable set, the sidecar execs itself once with
the keys on a pipe named by `--keys-fd` and the variables left out, so the launch strings the
kernel keeps hold no key. The flag is the re-exec's alone; one naming anything but a readable pipe
stops the start. Then it tightens the umask (the original becomes `Config.UserUmask`), parses
flags and installs the logger. `run(ctx, cfg, …)` is the tested seam; only `main` sets
`RunLoginShell`, so tests never spawn a shell. `main` is tested in the test binary started as a
sidecar (`runMain`, `TestMain`'s `childEnv`); such a test fails before `app.New`, which would run
the real shell. `New` runs the login shell **before anything reads the environment**. **`READY`
promises a socket, not a finished startup**: it prints after the bind, before `Start`.

**Logging** (`internal/lib/logging`): one JSON object per line, keyed as the host keys its own;
**nothing here formats a human-readable line**. `sidecar.log` is rotated by size in the layout's
log folder, made owner-only (`openLog`); a folder or file it cannot make or open leaves it on
stderr and never stops it. → [ADR: JSON logs rendered by the host](../docs/adr/2026-09-08-json-logs-rendered-by-the-host.md),
[ADR: two processes, two log files](../docs/adr/2026-09-08-two-processes-two-log-files.md).

- `graph/` — resolver deps are non-nil; tests wire fakes. A domain refusal reaches the wire as
  `KUBETAIL_VALIDATION_ERROR` through a mapper in `graph/util.go`. A `Chat` folder grant with no
  `chatID` is refused, never widened to every chat.
- `grpc/` — committed protoc output, from `make generate-desktop`; it owns `IsGRPCRequest`.
- `internal/` → [ADR: services, lib and the rest](../docs/adr/2026-10-06-sidecar-packages-group-into-services-lib-and-the-rest.md):
  `services/` holds state outliving a request, reached from `graph/`/`grpc/`; `lib/` holds no
  Kubetail domain, and `lib` is never a package name. `apimeta` is the wire vocabulary no service
  owns (the delta frame, `ObjectID`, `ClusterID`, `ChatID`); `sqlstmt` is a store's statement table
  (→ [ADR](../docs/adr/2026-09-16-sqlstmt-prepares-a-services-statements.md)).
- **`sqlitepool` is the one home of the SQLite open contract**: every pool is a `Pool`, whose close
  returns once its connections have closed, so a store deletes its file straight after closing. A
  file closes all its pools under one `CloseBudget`; one a connection outlives is kept as stale
  (the cluster and logs guides).

**The folders are layers**; an import points only right to left:

```
lib → appdb → llm → run → services → tools → agent → app
```

`TestInternalFoldersAreLayers` fails on a backward edge: move the package, never add the edge. →
[ADR: folders are layers](../docs/adr/2026-10-07-sidecar-folders-are-layers.md).

## Shutdown

`main.go`'s `http.Server` speaks `app.Protocols()` (HTTP/1.1 plus unencrypted HTTP/2).
**`shutdown.go` stops in order**: `srv.Shutdown` (stops listening; its hook runs `CancelStreams`, so
SSE and gRPC streams end at once; requests get `requestGrace`, 10s) → `CancelStreams` again, since
`Shutdown` does not wait for its hooks → `CloseRequests` (a later request gets a 503; every handler
still running is cancelled, its connection cut if the grace ran out) → `WaitRequests` (every HTTP
handler, then gRPC `Stop`) → `stop` → `Close`. One watchdog bounds the whole stop at
`shutdownBudget` (20s): it removes the socket file, logs every goroutine's stack and exits 1. The
host's kill (`SIDECAR_SHUTDOWN_GRACE`) sits above the budget. Traps:

- Cancel per request, never via `http.Server.BaseContext`, which kills the shared connection.
- Handlers are joined before `stop`, and loops before `Close`, after every `Shutdown`: `Shutdown`
  counts streams, not handlers, and a gRPC method handler outlives its `ServeHTTP` once the client
  resets the stream.
- Nothing after the grace has a deadline but the watchdog.

## Security invariants

Full picture: [`docs/security-model.md`](../docs/security-model.md).

- **Every endpoint is an argument** (`configFromArgs`). The environment reaches the config only
  through `applyEnvOverrides`, a no-op without `-tags debug` (`make build-desktop-sidecar-dev`),
  via the `getenv` it is handed; `KUBETAIL_FAULTS` is among what it reads. A release build reads
  only `KUBETAIL_LOG_LEVEL` (a log level redirects nothing) and the folder roots
  (`layout.RootsFromEnv`, which the host reads too) directly.
- **Provider keys are the exception**: one variable per provider, read once per launch in every
  build, registered with `safe.AddSecret` before the logger exists, then stripped from the process
  (`takeProviderKeys`). The login shell's keys (the launch's winning) join `Config.LLMKeys` and are
  never set on the process. `config_test.go` pins it. → [provider keys from the login
  shell](../docs/security/2026-10-07-provider-keys-from-the-login-shell.md), [cloud providers from the
  environment](../docs/security/2026-09-12-cloud-providers-from-the-environment.md).
- **Only the host process may connect.** `ipc.Authenticated` checks the peer pid against
  `--host-pid`; the socket is 0600 from birth, the pipe owner-only. Never widen it or add a TCP
  listener. The GET transport beside POST and SSE (`server.go`) is harmless only because the
  transport is local.
- **Redaction happens at write time, off the body's own group and kind** (`kubestore/objects.go`);
  a read path never re-derives it. It fails closed (check a `Nested*` read's `err`, not `found`). A
  cache file holds cluster data in the clear, by decision. → [ADR: the cache is ordinary application
  data](../docs/adr/2026-09-02-the-cache-is-ordinary-application-data.md), [ADR: secret
  redaction](../docs/adr/2026-08-30-secret-redaction-at-write-time.md).
- **Reading a kubeconfig can execute code** (`exec` plugins, on every probe). Widening what is
  probed widens what runs.
- **Diagnostics exclude request data.** The GraphQL presenter logs operation type, Go error type,
  the wrapped cause's type, whether it is a cancel or the request's context ended, and `code` only.
  Everything else renders through `internal/lib/safe` at the sink, so a plain
  `slog.Error("…", "err", err)` is correct; a non-string, non-error value logs as its type. A
  persisted message is rendered where it is recorded (`supervisor.Fail`). Auth projections omit
  tokens.
- **A restored account is display-only.** The engine verifies the ID token and its nonce at
  sign-in; at launch the account is read back from its `oauth_connections` row. Keep it out of
  authorization.
- **The sidecar runs the user's `az`, `gh` and `gcloud`** off `PATH`, with fixed arguments (`az`'s
  but for a tenant the account holds), and never logs or keeps their output; a token one gives is
  held in memory under `safe.AddSecret`.

## Small services

**Memory (`services/memory`): one row per note**, scoped to a cluster or every cluster. `checkNote`
guards every write; `ScopeBudget` caps a scope. The model's writes reach only notes it wrote
(`ErrUserNote`); the dialog's make a note the user's. The `Memory` tool asks only for `scope:
everywhere`, after a dry check. **A note the user wrote is a request; one the model wrote is
information.** → [ADR](../docs/adr/2026-09-24-memory-is-rows-scoped-to-a-cluster-or-every-cluster.md),
[security record](../docs/security/2026-09-24-memory.md).

**User's variables (`services/envvars`): one row per variable**, for every cluster or one, with a
`Target`: commands in the sandbox, outside it, or both. `Check` guards every write and every read
against the target; a stored row it refuses is listed with its `Refusal` and given to no command.
`For(cluster, sandboxed)` is what a command gets: every cluster's, the cluster's value in place of
one of the same name. When a command reads it is `internal/tools/CLAUDE.md`. →
[ADR: rows](../docs/adr/2026-11-20-a-setting-with-scopes-is-rows.md),
[ADR: either side of the sandbox](../docs/adr/2026-11-20-the-users-variables-reach-commands-on-either-side-of-the-sandbox.md),
[security record](../docs/security/2026-11-20-the-users-variables.md).

**Tool settings (`services/toolsettings`): a `tools.<name>.enabled` row of `settings` per tool,
overriding on.** Only every cluster's scope is written; turning a tool back on keeps its row; a
value Kubetail cannot use is off. A row for a tool the box lacks, or turning off an `AlwaysOn` tool
(TaskStop), is ignored. The chat service reads it where it reserves a turn. →
[ADR](../docs/adr/2026-11-21-tool-settings-are-rows-in-app-db.md),
[security record](../docs/security/2026-11-20-the-user-turns-tools-off.md).

**Connectors (`services/connectors`): an account on a service that Kubetail signs in to, and whose
requests it may then sign**, one package each. Each has `New`, `State`, `WatchState`, `Connect`,
`Disconnect` and `Close`; a model provider's connector is also its `providercredentials.Connector`.
**Each `New` is `Restoring` and makes no keyring call**: it reads on a goroutine, `Restored()`
closes once it has published, a credential asked for meanwhile waits, and a change answers
`ErrRestoring`. The restore reaches no network, and `Close` leaves a keyring call that never
answers behind. → [ADR](../docs/adr/2026-11-20-a-launch-restores-keyring-credentials-in-the-background.md).
**A connector outside `oauthconn` (`openrouter`, `github`, `aws`) keeps one keyring entry, and an
entry present is connected**: a newer `v` is left in place, an entry this build cannot use is
deleted, and an unreadable keyring writes nothing. Its `Connect` checks the keyring before any
browser opens, and is refused during an attempt, while connected and in `Failed{NotDeleted}`. Its
`Disconnect` drops the credential from memory first, then deletes the entry; a refused delete is
`Failed{NotDeleted}` and returns the error.
The root package is their vocabulary; `oauthconn` is in `connectors/internal/`; `catalogsync` and
`signer` in `services/internal/`. → [ADR](../docs/adr/2026-11-19-a-connector-is-one-package-and-its-machinery-is-internal.md).

**`catalogsync` is a provider's catalog's only writer**: it reads when the credential changes, on a
poke and every six hours, and empties the catalog while there is no credential.

**Kubeconfig (`services/kubeconfig`): the one reader of the user's kubeconfig.** Nothing else
watches the file, calls `clientcmd` or builds a `rest.Config`. `New` reads nothing, not even
`KUBECONFIG`; `Start` reads, after `app.New`'s shell import, so a GUI launch sees the login shell's
file list. `Close()` ends every subscription in the process, so only the app calls it.

- `RESTConfig(contextName)` reads one snapshot per call, is not memoized, and takes only a config
  the loading rules produced. Its cache key excludes the context name, covers static
  exec/auth-provider config and `proxy-url`, and length-prefixes every value.
- **A merge with no contexts at all is never published**, since `clientcmd` truncates before it
  writes. A reload stats the chain before and after its load and drops a load any file changed
  under.
- Watches are pull-first: a 30-minute backstop tick under fsnotify, plus pokes. **Keep the tick
  under any new push layer.** Use `resolvePaths` and keep every path in one namespace (no
  `filepath.EvalSymlinks`, which matches nothing on macOS).

**Resync (`services/poke`)**: a wall-clock gap detector plus a broadcast hub. `Poke(src)` never
blocks. A poke is a fan-out, never a cascade through spec counters or conditions. →
[ADR](../docs/adr/2026-08-09-poke-resync-fanout.md).

### Cloud logins

Each holds what a run's proxy under `internal/run` signs with.

- **Azure (`services/azureauth`)**: the user's `az` login, discovered when a run first asks, and
  again once `azureProfile.json`, `config` or `clouds.config` changes. One discovery runs at a
  time, apart from any run's context: a run with no account waits for it until its own context
  ends; one with an account does not. A discovery whose `az` did not run to an answer (`ErrNotRun`)
  keeps nothing; an `az` before 2.73, which asks IMDS, is no login. A token is per tenant, one fetch
  at a time, kept until five minutes before expiry; `Drop` forgets one Azure answered `401`. **Each
  discovery drops every held token**, and a token `az` gave while one completed is not kept: the
  user may have signed in as someone else. Every failure is `ErrNoToken`. →
  [ADR](../docs/adr/2026-11-17-a-sandboxed-command-reaches-azure-through-a-token-injecting-proxy.md).
- **GitHub (`services/githubauth`)**: the user's `gh` token, the GitHub proxy's while the GitHub
  connector is signed out. `gh auth token` runs on first use with no stdin, prompts off, killed
  past 10 s; the token is held five minutes and never written. `gh` runs at most once a minute: a
  failure is held that long, and `Refused` (an upstream `401`) drops the token only a minute after
  the last run. Concurrent reads share one run.
- **AWS (`services/awsauth`)**: a `Service` per profile, the one the run's kube-context names
  (`kubeconfig.AWSProfile`), else the SDK's default chain, resolved only when a request needs it.
  A chain is fixed at load, so it is loaded again after a load yields none, a retrieval fails or a
  `Drop`: new profiles or keys need no restart. It holds the secret key and session token under
  `safe.AddSecret`, forgets them on refresh or `Drop`, and answers a failure as a `NoLoginError` naming the profile, not the SDK's
  text. **Each retrieval and each `Drop` takes one turn**, so what `safe` blanks is what the cache
  last answered; a retrieval holds the turn until the SDK's refresh ends, even once its caller has
  given up, or until `retrieveTimeout`, after which its chain is unloaded. **A `credential_process`
  runs quiet** (`process_unix.go`): no stdin, no stderr (the host logs the sidecar's stderr as is),
  in a session of its own so it has no terminal to prompt on. The SDK's timeout kills its process
  group; a descendant that left the group is waited for no longer than `helperWaitDelay`. A role assumed from a source profile's
  process is no login. The package is Unix-only: Windows has no sandbox to reach AWS from. →
  [ADR](../docs/adr/2026-11-18-a-runs-aws-identity-follows-its-chats-cluster.md).
- **Google Cloud (`services/gcpauth`)**: `gcloud config config-helper`, as
  `gke-gcloud-auth-plugin` runs it. The token is held until a minute before expiry, a failure for
  30 s, one run shared among callers. The read after a `Drop` forces gcloud to refresh, which
  otherwise answers its own cached token. Its stderr is never read. →
  [ADR](../docs/adr/2026-11-17-google-cloud-is-reached-with-the-active-gcloud-accounts-token.md).

## GraphQL

`graph/schema.graphqls` is authoritative and feeds the webview's codegen: one file, sectioned by
noun, `Query`/`Mutation`/`Subscription` at the end. `make generate-desktop` runs gqlgen and the
codegen; implement the panicking stubs gqlgen appends to `schema.resolvers.go`. Renaming a schema
file is a two-pass regen: delete the old `*.resolvers.go` between passes and check no body came
through as `panic("not implemented")`.

**A data access is named for its contract**; choose the contract first from
[`docs/data-access.md`](../docs/data-access.md).

| Contract | Go method | GraphQL field |
| --- | --- | --- |
| Get, List, Fetch | `Get<Noun>`, `List<Noun>s`, `Fetch<Noun>` | `<noun>Get`, `<noun>List`, `<noun>Fetch` |
| Collection watch | `Watch<Noun>Collection` | `<noun>CollectionWatch` |
| State, keyed state watch | `Watch<Noun>State`, `…KeyedState` | `<noun>StateWatch`, `…KeyedStateWatch` |
| Event, sample feed | `Subscribe<Noun>EventFeed`, `…SampleFeed` | `<noun>EventFeed`, `…SampleFeed` |
| Cursor tail | `Tail<Noun>FromCursor` | `<noun>Tail` |
| Change, broadcast notification | `ListenFor<Noun>Changes`, `…Notifications` | none: they stay in the sidecar |

- A method drops a noun its receiver names (`Caches().WatchCollectionByCluster`); a scope is a
  `By<Scope>` suffix in Go and an argument in GraphQL.
- A service method that opens with `Get`, `List`, `Fetch`, `Watch`, `Tail` or `Listen`, or a feed
  method with `Subscribe`, has that verb's contract. A poller or file watcher takes another verb.
- A state watch sends its value on subscribe. A change notification sends nothing until a change,
  so listen before the first read.
- A Watch, Feed or Tail that can fail returns `*Stream[T]`; its resolver goes through
  `subscriptionStream`, which turns `Err()` into the `subscriptionFailed` frame. Every one ends with
  its `ctx`.
- A field's description opens with its contract. A query with no caller is removed.

## Patterns

- A type's methods live in the type's file; a helper belongs to whoever calls it, and only what
  more than one needs goes on the service.
- Pub/sub: unkeyed → `gochan` (`watch` for latest-value with a seed, `broadcast` for fan-out);
  keyed → `gobus` (`watch` delivers nothing until the next send; `conflate` for bursts). Never
  hand-roll a subscriber map.
- Fan-in from producers that come and go → `gochan` `mpsc` with `WithKeepOpen`, a sender per
  producer; without the option the hub closes for good once no sender is open.
- **Every outgoing request names the app** (`version.UserAgent()`) ahead of its own: a new
  `http.Client` takes `version.Transport`, a proxy calls `version.Prefix`. Exception: the AWS SDK
  appends the bare token.
- Work to do is a queue, not a bus: `internal/lib/workqueue`, one `Queue` per job. `Done` is owed
  for every key taken.
- **Production code reaches the API server through `kubeconn.Connection`'s `Dynamic` client or raw
  requests on its `HTTPClient`.** Never import `k8s.io/api/...`, `client-go/discovery` or the typed
  clientset: `kubernetes/scheme` links every API group, about 16 MB. Tests may.
  `scripts/check-sidecar-deps.sh` fails on them (`make vet-desktop`, CI).

## Persisted formats

[`docs/persisted-data.md`](../docs/persisted-data.md)'s *How a change ships* is the procedure for
each kind of change. The sidecar's side:

- **A SQLite schema** (`appdb`, `kubestore`, `logstore`): the next numbered `*.sql` plus its
  SHA-256 line in the package's `shippedMigrations`; `TestAShippedMigrationIsNeverEdited` fails on
  a changed, added or missing file. A migration past `0001` ships with a test that builds the
  previous schema from the frozen files, seeds it, migrates and reads the seed back. **Rebuilding a
  `kubestore` table a trigger names** fails at `ALTER TABLE … RENAME`: drop the triggers first and
  recreate them after.
- **A JSON column** rides `app.db`'s version, so its change is an `app.db` migration.
- **A path under `<data>` or `<cache>`** is never renamed, moved or removed; a new path is its
  owner's to create.

**A refusal reaches the user.** `sqlitemigrate` refuses a newer file with `*NewerSchemaError`,
which `build` turns into *app.db was written by a newer Kubetail (…)*, logged as `app init`. When
`app.New` fails, `run` also prints `FATAL <reason>` on stdout, as `main` does for a bad flag or
root; the host shows it on the start screen. Tests use `sqlitemigrate/sqlitemigratetest`
(`StampNewer`, `Checksums`).

## Tests & checks

- Resolver tests stand up `graph.NewServer(ctx, &graph.Resolver{...})` + `POST /graphql`;
  lifecycle tests stand up `app.New(...)`.
- A fixture that needs a stored status writes it with `beehive.NewAdminClient`, never by
  registering a controller. A controller's own writes are asserted by calling `Reconcile` against a
  stubbed `ControllerClient`.
- The one wait failsafe is `testutil.Timeout`. `testutil.Probe[T]` drops the oldest, so a consumer
  doing edge detection needs a lossless fan-out instead.
- A test outside `services/cluster` that needs a real cache store uses `cluster/clustertest`; its
  `Connection` is one a test can `Retire`.
- **`make vet-desktop` runs go vet, staticcheck and the `go fix` check on every build
  `scripts/go-builds.sh` lists** (each GOOS, linux/arm64, the `debug`, `livecache` and
  `liveswitch` tags). A new split joins that list and `vet-desktop`'s `go vet` lines.
- **CI's race step runs `go test -race -short ./...`** with `GORACE=atexit_sleep_ms=0`. A sweep too
  slow for the race detector shrinks under `testing.Short()`; a test never skips under it.
- **The nightly stress run** (`stress-desktop.yml`) runs every desktop runner, `-shuffle=on
  -count=3`, a job per `-cpu` value (1 and 4; the `cpu` dispatch input is a JSON array), each with
  `-race` (keeping `checkptr` on for modernc's SQLite) and without, and sets neither `-short` nor
  `GORACE`. A test that hangs under `-cpu 1` is a finding. An issue's command reproduces it from
  the repo root: `go -C apps/desktop/sidecar test -race -cpu 1 -shuffle=<seed> -count=3 ./internal/agent/chat`.
- **Coverage is `coverage-desktop.yml`'s `Measure · Go`**, one runner per OS: the suite untagged
  and, on Linux alone, with `-tags debug`, both with `-coverpkg=./...` and `CGO_ENABLED=0`, as the
  release is built. `debug` is the only build with the environment overrides and `KUBETAIL_FAULTS`;
  a test only it can pass carries `//go:build debug`, and no debug-tagged file is OS-specific.
  `tools/coverignore` merges the two profiles and applies the markers. Codecov merges the three
  OSes, so a `_windows.go` line is judged by the Windows upload.
