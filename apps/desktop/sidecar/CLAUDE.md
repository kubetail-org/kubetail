# sidecar — Go backend

A Go binary the Tauri host starts. It serves GraphQL and a gRPC control channel and owns all
Kubernetes logic. **No TCP**: it listens on a Unix socket (named pipe on Windows), prints
`READY unix:<path>` and exits on `SIGINT`/`SIGTERM` or **stdin EOF**. **It places Kubetail's folders
itself** (`internal/app/layout`, from its environment's roots), as the host places its own from the
same roots; `layout/testdata/layouts.json` pins both. The host passes only
`--runtime-dir`, required and absolute, with no `..` on Unix, and `--dev`, its profile. Run tests point the roots at
`t.TempDir()` (`scratchRoots`). → [ADR](../docs/adr/2026-11-23-the-sidecar-places-kubetails-folders-itself.md).

**`<data>/app.db` belongs to `internal/appdb`, and the app owns it.** `build`
(`internal/app/runtime.go`) opens it before any service, under the data directory's lock, and hands the `*appdb.DB` to each service that uses it;
a service closes only the statements it prepared. `appdb.Open` gives a one-connection migrated
writer and a `query_only` reader, plus a janitor. A service's statements are a `sqlstmt.Set` on
`db.Write, db.Read`. A table change is the next numbered migration (*Persisted formats*). Tests open the
DB through `appdb/appdbtest`, never `appdb.Open`. **The change bus**: `Notify(key)` after commit,
never in it; `ListenForChanges` before the first read; never count notices, which coalesce. The keys
are named in `appdb.go`. `NewID()` mints UUIDv7; `ValidateUUID` accepts only canonical v4/v7.
→ [ADR: the app owns app.db](../docs/adr/2026-09-16-the-app-owns-app-db.md),
[ADR: a shipped schema changes only by a new migration](../docs/adr/2026-11-08-a-shipped-schema-changes-only-by-a-new-migration.md).

## Directories

**`app/paths.go` names every path under the three directories** (its doc comment
is the tree). Each owner gets its own `Paths` (`cluster.Paths`, `bash.Paths`), naming no other.
**Each subtree has one owner**, which makes it 0700, sweeps and removes it.

**`app.ResolveFolders` follows links once, before the log opens**, checks the layout, then
makes the data and cache 0700. `Config.Folders` is resolved and fenced (`DeniedDirs`);
`makeSubfolders` refuses a linked subfolder; nothing writes a link's parent. →
[ADR](../docs/adr/2026-11-24-follow-a-link-to-a-kubetail-folder-once-at-start-and-refuse-one-below-it.md),
[ADR: each file in the platform's directory for its
kind](../docs/adr/2026-09-27-kubetails-files-follow-the-platforms-directory-kinds.md).

**One sidecar per data directory.** `build` locks `<data>/sidecar.lock` (`internal/lib/filelock`:
`flock` on Unix, `LockFileEx` on Windows) before anything opens a file there. The lock is the
runtime's first part, so it closes last. A second sidecar fails `app.New` with *another Kubetail is
using `<data>`*, which `run` prints as `FATAL`. The kernel drops the lock when the process ends, a
crash included. `build` runs after the `--keys-fd` re-exec, which a lock would not survive, and the
`sandbox-*` subcommands never take it. **Never delete `sidecar.lock`**: a second process would lock
a new file under the same name while the first still holds the old one.

## Guides

This file is the whole sidecar: the process, its directories, the layering, the socket, the
security invariants, the small services, and the conventions every package follows. The larger
packages each have a `CLAUDE.md` of their own, loaded when you work in them:

| Guide | Covers |
| --- | --- |
| `internal/services/cluster/CLAUDE.md` | clusters, caches, kinds, cached data; the private leaves `kubeconn`, `kubesync`, `kubestore`; streams and watches |
| `internal/services/logs/CLAUDE.md` | the log cache (`logstore`), the line vocabulary (`logline`), refusals (`refusal`), the kubelet reader (`kubelet`), pages, follows, tailers |
| `internal/services/settings/CLAUDE.md` | the settings' `app.db` tables: `settings` (the switches, the `PATH`, the tools), modes, rules, folder grants, executables |
| `internal/llm/CLAUDE.md` | dialects, providers, the record's blocks, the wires, the fake |
| `internal/run/CLAUDE.md` | the login shell, the session, the sandbox, permissions, the cluster proxy |
| `internal/tools/CLAUDE.md` | the box, actions and results, each tool (Bash, Read/Write/Edit, WebFetch, KubeQuery, LogsView, LogsRead, LogsWatch, web search), `fileguard`, `logsinput` |
| `internal/agent/CLAUDE.md` | the catalog, the loop, the chat service (turn, rows, approvals, tasks, the wire errors) |
| `internal/services/providercredentials/CLAUDE.md` | every provider's saved and environment keys, its signer and its catalog sync |
| `internal/services/connectors/internal/oauthconn/CLAUDE.md` | the OAuth engine, which `chatgpt`, `google` and `kubetailcloud` run |
| `internal/services/connectors/openrouter/CLAUDE.md` | the OpenRouter connector: a sign-in that grants a key, kept in the keyring |
| `internal/services/connectors/aws/CLAUDE.md` | the AWS connector: `aws login`'s sign-in, a DPoP-bound session in the keyring, an `aws.CredentialsProvider` |
| `internal/services/connectors/github/CLAUDE.md` | the GitHub connector: the device flow, a token in the keyring for the proxy |
| `internal/services/connectors/google/CLAUDE.md` | the Google connector: Google's sign-in on the engine, signing Vertex AI in the user's project |
| `internal/services/connectors/kubetailcloud/CLAUDE.md` | the Kubetail Cloud connector: the Kubetail Cloud account on the engine, behind `authStateWatch` and the tray |

## Layout

`main.go` is lifecycle only; `internal/app` is the composition root; GraphQL is `graph/`.

**`build` fills one `Runtime`** (`internal/app/runtime.go`), a field per service in build order.
A service never sees the runtime: it takes constructor arguments. Field order is build order is
start order (`rt.add`); stop and close reverse it. An edge back to a later service is a setter at
the end of `build` (`SetAlwaysFolders`, `SetCacheFiles`). **kubeconfig before cluster is
load-bearing** (`app_test.go`). → [ADR: the runtime is built in
order](../docs/adr/2026-10-06-the-runtime-is-built-in-order.md).

`main` runs `sandbox.Main` first (the `sandbox-*` subcommands), then `scrubExecEnvironment`
(`scrubexec_unix.go`): started with a provider key variable set, the sidecar execs itself once with
the keys on a pipe named by `--keys-fd` and the variables left out, so the launch strings the
kernel keeps hold no key; the flag is the re-exec's alone, and a flag naming anything but a
readable pipe stops the start. Then it tightens the umask (the original becomes
`Config.UserUmask`), parses flags, and installs the logger. `run(ctx, cfg, …)` is
the tested seam; `main` alone sets `RunLoginShell`, so tests never spawn a shell. `main` itself is
tested in the test binary started as a sidecar (`runMain`, `TestMain`'s `childEnv`), whose coverage
merges into the run's; such a test fails before `app.New`, which would run the real shell. `New` runs the
login shell **before anything reads the environment**. **`READY` promises a socket, not a finished
startup**: it prints after the bind, before `Start`.

**Logging** (`internal/lib/logging`): one JSON object per line, keyed as the host keys its own;
**nothing here formats a human-readable line**. The sidecar writes `sidecar.log`, rotated by size, in
the layout's log folder, which it makes owner-only (`openLog`); a folder or file it cannot make or
open leaves it on stderr, and never stops it. → [ADR: JSON logs rendered by the
host](../docs/adr/2026-09-08-json-logs-rendered-by-the-host.md), [ADR: two processes, two log
files](../docs/adr/2026-09-08-two-processes-two-log-files.md).

- `graph/` — schema, generated code, resolvers. Resolver deps are non-nil; tests wire fakes. A
  domain refusal reaches the wire as `KUBETAIL_VALIDATION_ERROR` through a mapper in `graph/util.go`.
  A `Chat` folder grant with no `chatID` is refused, never widened to every chat.
- `grpc/` — committed protoc output; regenerate with `make generate-desktop`, **never hand-edit `*.pb.go`**. It runs the repo's pinned `protoc` and plugins.
- `internal/` → [ADR: services, lib and the
  rest](../docs/adr/2026-10-06-sidecar-packages-group-into-services-lib-and-the-rest.md):
  `services/` (state outliving a request, reached from `graph/`/`grpc/`), `lib/` (no Kubetail
  domain; `lib` is never a package name; `oauth` is the OAuth client), `llm/`, `run/` (what confines one run), `agent/`
  (above `tools`), and `app`, `appdb` at the top. `sqlitepool` is the one home of the SQLite open
  contract: every pool is a `Pool`, whose close returns once its connections have closed, so
  a store deletes its file straight after closing; a file closes all its pools under one
  `CloseBudget`, and one a connection outlives is kept as stale until a retry waits it out
  (the cluster and logs guides); `apimeta` is the wire vocabulary no service owns (the delta-frame type, `ObjectID`,
  `ClusterID`, `ChatID`); `sqlstmt` is a store's statement table (→ [ADR: one statement
  set](../docs/adr/2026-09-16-sqlstmt-prepares-a-services-statements.md)).

**The folders are layers**; an import points only right to left:

```
lib → appdb → llm → run → services → tools → agent → app
```

`TestInternalFoldersAreLayers` fails on a backward edge: move the package, never add the edge. →
[ADR: folders are layers](../docs/adr/2026-10-07-sidecar-folders-are-layers.md).

## gRPC + GraphQL over one socket (h2c)

`internal/app` owns the topology; `grpc/` owns `IsGRPCRequest`. `main.go`'s `http.Server` speaks
`app.Protocols()` (HTTP/1.1 plus unencrypted HTTP/2). → [ADR: single-socket h2c from the standard
library](../docs/adr/2026-10-26-single-socket-h2c-from-the-standard-library.md).

**Shutdown** (`shutdown.go`): `srv.Shutdown` (stops listening, then its hook runs `CancelStreams`,
so SSE and gRPC streams end at once; requests get `requestGrace`, 10s) → `CancelStreams` again, since
`Shutdown` does not wait for its hooks → `CloseRequests` (a later
request gets a 503; every handler still running is cancelled, and its connection cut if the grace
ran out) → `WaitRequests` (every HTTP handler, then gRPC `Stop`, which waits for its method
handlers) → `stop` → `Close`. Each step logs its duration under the name it has in the log:
`drain requests`, `cancel requests`, `wait requests`, `stop services`, `close services`. One
watchdog bounds the whole stop at `shutdownBudget` (20s, the grace plus 10s): it removes the
socket file, logs every goroutine's stack, one record each, and exits 1. Traps: cancel per
request, never via
`http.Server.BaseContext`, which kills the shared connection; handlers are joined before `stop`, and
loops before `Close`, after every `Shutdown`, since `Shutdown` counts streams, not handlers, and a
gRPC method handler outlives its `ServeHTTP` once the client resets the stream; nothing after the
grace has a deadline but the watchdog. The host's kill (`SIDECAR_SHUTDOWN_GRACE`) sits above the
budget.

## Security invariants

Full picture: [`docs/security-model.md`](../docs/security-model.md).

- **Every endpoint is an argument** (`configFromArgs`). The environment reaches the config only
  through `applyEnvOverrides`, a no-op without `-tags debug` (`make build-desktop-sidecar-dev` builds one, for a
  run with no host), via the `getenv` it is handed; `KUBETAIL_FAULTS`, which fails cluster requests,
  is among what it reads. `KUBETAIL_LOG_LEVEL` is the one variable a release build reads directly:
  a log level redirects nothing. **The folder roots** (`layout.RootsFromEnv`) are read in every
  build too: the host places its own from the same variables.
  **Provider keys are the exception**: read in every build, registered with `safe.AddSecret`
  before the logger exists, then stripped from the process (`takeProviderKeys`); the shell's keys
  join `Config.LLMKeys` and are never set on the process. `config_test.go` pins it. → [provider
  keys from the login shell](../docs/security/2026-10-07-provider-keys-from-the-login-shell.md).
- **Only the host process may connect.** `ipc.Authenticated` checks the peer pid against
  `--host-pid`; the socket is 0600 from birth, the pipe owner-only. Never widen it or add a TCP
  listener. The GET transport registered beside POST and SSE (`server.go`) is harmless only
  because the transport is local.
- **Redaction happens at write time, off the body's own group and kind**
  (`kubestore/objects.go`); a read path never re-derives it. It fails closed (check a `Nested*`
  read's `err`, not `found`). A cache file still holds cluster data in the clear, by decision. →
  [ADR: the cache is ordinary application
  data](../docs/adr/2026-09-02-the-cache-is-ordinary-application-data.md), [ADR: secret
  redaction](../docs/adr/2026-08-30-secret-redaction-at-write-time.md).
- **Reading a kubeconfig can execute code** (`exec` plugins, on every probe). Widening what is
  probed widens what runs.
- **Diagnostics exclude request data.** The GraphQL presenter logs operation type, Go error type, the
  wrapped cause's type, whether it is a cancel and whether the request's context ended, and
  `code` only. Everything else renders through `internal/lib/safe` at the sink, so a plain
  `slog.Error("…", "err", err)` is correct; a non-string, non-error value logs as its type. A
  persisted message is rendered where it is recorded (`supervisor.Fail`). Auth projections omit
  tokens.
- **A restored account is display-only.** The engine verifies the ID token and its nonce at
  sign-in; at launch the account is read back from the `oauth_connections` row. Keep it out of
  authorization.
- **The sidecar runs the user's `az`** (`services/azureauth`), off `PATH`, with fixed arguments but
  for a tenant the account holds; its output is never logged or kept, and a token it gives is held
  in memory under `safe.AddSecret`. It runs no earlier than a run that mounts Azure.

## Memory (`internal/services/memory`)

**One row per note**, scoped to a cluster or every cluster. `checkNote` (`note.go`) guards every
write, and `ScopeBudget` caps a scope. The model's writes reach only notes it wrote
(`ErrUserNote`); the dialog's make a note the user's. The `Memory` tool asks only for `scope:
everywhere`, after a dry check. **A note the user wrote is a request; one the model wrote is
information.** → [ADR: memory is
rows](../docs/adr/2026-09-24-memory-is-rows-scoped-to-a-cluster-or-every-cluster.md), [security
record](../docs/security/2026-09-24-memory.md).

## User's variables (`internal/services/envvars`)

**One row per variable** (`env_vars`), for every cluster or one, with a `Target`: commands in the
sandbox, outside it, or both. `Check` guards every write and every read against the target; a
stored row it refuses is listed with its `Refusal` and given to no command. `For(cluster,
sandboxed)` is what a command gets: every cluster's, the cluster's value in place of one of the
same name. A sandboxed run reads it at its start; a command outside the sandbox at its approval
(`internal/tools/CLAUDE.md`). → [ADR: rows](../docs/adr/2026-11-20-a-setting-with-scopes-is-rows.md),
[ADR: either side of the sandbox](../docs/adr/2026-11-20-the-users-variables-reach-commands-on-either-side-of-the-sandbox.md),
[security record](../docs/security/2026-11-20-the-users-variables.md).

## Tool settings (`internal/services/toolsettings`)

**A `tools.<name>.enabled` row of `settings` per tool, overriding on.** Only every cluster's
scope is written; turning a tool back on keeps its row; a value Kubetail cannot use is off. A row
for a tool the box lacks, or turning off one listed `AlwaysOn` (TaskStop), is ignored. The chat
service reads it where it reserves a turn. → [ADR](../docs/adr/2026-11-21-tool-settings-are-rows-in-app-db.md),
[security record](../docs/security/2026-11-20-the-user-turns-tools-off.md).

## Azure (`internal/services/azureauth`)

**The user's `az` login, for Azure's mount**, in `run/azureproxy`'s types. `Account` discovers it
(`az version`, `az account list`, `az cloud show`) when a run first asks, and again once `azureProfile.json`,
`config` or `clouds.config` changes. One discovery runs at a time, apart from any run's context: a
run with no account waits for it until its own context ends, one with an account does not. A
discovery whose `az` did not run to an answer (`ErrNotRun`: none found, or cut off at its bound)
keeps nothing; an `az` before 2.73, which asks IMDS, is no login. `Token` runs `az account
get-access-token` per tenant, one at a time, kept until five minutes before expiry (`expires_on`,
else `expiresOn` from an `az` before 2.54); `Drop` forgets one Azure answered `401`. **Each
discovery drops every held token**, and a token `az` gave while one completed is not kept: the user
may have signed in as someone else. Every failure is `ErrNoToken`. →
[ADR](../docs/adr/2026-11-17-a-sandboxed-command-reaches-azure-through-a-token-injecting-proxy.md).

## Connectors (`internal/services/connectors`)

**A connector is an account on a service that Kubetail signs in to, and whose requests it may then
sign**, one package each: `chatgpt`, `openrouter`, `aws`, whose credentials nothing uses yet,
`github`, whose token GitHub's proxy sends, `google`, which signs Vertex AI, and `kubetailcloud`, the Kubetail Cloud account, which signs nothing. Each has `New`, `State`, `WatchState`, `Connect`, `Disconnect` and `Close`; a model
provider's connector is also its `providercredentials.Connector`, which signs its rows. **Each `New` is `Restoring` and makes no keyring call**: it
reads on a goroutine, `Restored()` closes once it has published, a credential asked for meanwhile
waits, and a change answers `ErrRestoring`. → [ADR](../docs/adr/2026-11-20-a-launch-restores-keyring-credentials-in-the-background.md).
The root package is their vocabulary; `oauthconn` is in `connectors/internal/`; `catalogsync` and `signer`, `providercredentials`' too, in `services/internal/`. → [ADR](../docs/adr/2026-11-19-a-connector-is-one-package-and-its-machinery-is-internal.md).

**`catalogsync` keeps a provider's catalog**, its only writer: read when its credential changes,
on a poke and every six hours, emptied while it has none. `providercredentials`' `SyncCatalogs` starts it for
every provider; `app` passes the read, `catalog.Models`.

## GitHub's token (`internal/services/githubauth`)

**The user's `gh` token, the GitHub proxy's while the GitHub connector is signed out**
(`github.Service.Tokens`). `Token` runs `gh auth token --hostname
github.com` on the sidecar's `PATH` on first use (no stdin, prompts off, killed past 10 s), holds it
five minutes under `safe.AddSecret` and never writes or logs it. `gh` runs at most once a minute:
a failure is held that long, and `Refused` (an upstream `401`) drops the token only a minute after
the last run. Concurrent reads share one run; the exec is a field.

## Kubeconfig (`internal/services/kubeconfig`)

**The one reader of the user's kubeconfig.** Nothing else watches the file, calls `clientcmd`, or builds a `rest.Config`. `New` reads nothing, not even `KUBECONFIG`; `Start` reads, after `app.New`'s shell import, so a GUI launch sees the login shell's file list. `Get()` returns the last snapshot plus whether a read has happened; `WatchConfigState()` is current-on-subscribe; `Close()` ends every subscription in the process, so only the app calls it.

`RESTConfig(contextName)` resolves one context to credentials and the pool's cache key: one snapshot per call; only a config the loading rules produced; the key excludes the context name, covers static exec/auth-provider config and `proxy-url`, and length-prefixes every value. Sentinels: `ErrContextNotFound` (also for an empty name) and `ErrNotRead`. Not memoized.

**A merge with no contexts at all is never published**, since `clientcmd` truncates before it writes. A reload stats the chain before and after its load and drops a load any file changed under.

Watches are pull-first: a 30-minute backstop tick under fsnotify and a poke subscription. **Keep the tick under any new push layer.** Use `resolvePaths` and keep every path in one namespace (no `filepath.EvalSymlinks`, which matches nothing on macOS).

## AWS login (`internal/services/awsauth`)

**The user's AWS credentials**, for `run/awsproxy`: a `Service` per profile, the one
the run's kube-context names (`kubeconfig.AWSProfile`), else the SDK's default chain, resolved
only when a request needs it. A chain is fixed at load, so it is loaded again after a load yields
none, a retrieval fails or a `Drop`: new profiles or keys need no restart. It holds the secret key and
session token under `safe.AddSecret`, forgets them on refresh or `Drop`, and answers a failure
as a `NoLoginError` naming the profile, not the SDK's text. →
[ADR](../docs/adr/2026-11-18-a-runs-aws-identity-follows-its-chats-cluster.md).

**Each retrieval and each `Drop` takes one turn**, so what `safe` blanks is always what the cache
last answered. A retrieval loads its chain in the turn, and holds the turn until the SDK's refresh
ends, even once its caller has given up, or until `retrieveTimeout`, after which its chain is
unloaded.

**A `credential_process` runs quiet** (`process_unix.go`): no stdin, no stderr, since the host logs
the sidecar's stderr as is, and in a session of its own, so it has no terminal to prompt on. The
SDK's timeout kills its process group, and a descendant that left the group is waited for no
longer than `helperWaitDelay`. A role assumed from a source profile's process is no login, since
the SDK runs that one with the sidecar's own. The package is built for Unix alone
(`awsauth_unix.go`): Windows has no sandbox to reach AWS from.

## Google Cloud token (`internal/services/gcpauth`)

**The user's Google access token, for `run/gcpproxy`.** `Source` runs `gcloud config config-helper`
off the sidecar's `PATH`, as `gke-gcloud-auth-plugin` does: no earlier than a run's mount with
Google Cloud's switch on. It holds the token until a minute before expiry and a failure for 30 s, shares one
run among callers, and registers the token with `safe` until it expires. The read after a `Drop`
forces gcloud to refresh, which otherwise answers its own cached token. Its stderr is never read.
→ [ADR](../docs/adr/2026-11-17-google-cloud-is-reached-with-the-active-gcloud-accounts-token.md).

## Resync broadcaster (`internal/services/poke`)

A leaf: wall-clock gap detector (15s tick, 2× factor) plus a `gochan/broadcast` hub. `Poke(src)` never blocks. A poke is a fan-out, never a cascade through spec counters or conditions. → [ADR: poke resync fan-out](../docs/adr/2026-08-09-poke-resync-fanout.md).

## GraphQL via gqlgen

`graph/schema.graphqls` is authoritative and also feeds the frontend's codegen. One file, sectioned by noun, `Query`/`Mutation`/`Subscription` at the end. After editing:

```sh
cd sidecar && go tool gqlgen generate
```

Implement the panicking stubs it appends to `schema.resolvers.go`. **Never hand-edit `generated.go`/`models_gen.go`.** Re-run the frontend `pnpm codegen`. Renaming a schema file is a two-pass regen: delete the old `*.resolvers.go` between passes and check no body came through as `panic("not implemented")`.

## Queries and subscriptions

A data access (a query, or an API that keeps delivering after the call returns) has a contract, and is named for it. Choose the contract first; [`docs/data-access.md`](../docs/data-access.md) states each one.

| Contract | Go method | GraphQL field |
| --- | --- | --- |
| Get, List, Fetch | `Get<Noun>`, `List<Noun>s`, `Fetch<Noun>` | `<noun>Get`, `<noun>List`, `<noun>Fetch` |
| Collection watch | `Watch<Noun>Collection` | `<noun>CollectionWatch` |
| State, keyed state watch | `Watch<Noun>State`, `…KeyedState` | `<noun>StateWatch`, `…KeyedStateWatch` |
| Event, sample feed | `Subscribe<Noun>EventFeed`, `…SampleFeed` | `<noun>EventFeed`, `…SampleFeed` |
| Cursor tail | `Tail<Noun>FromCursor` | `<noun>Tail` |
| Change, broadcast notification | `ListenFor<Noun>Changes`, `…Notifications` | none: they stay in the sidecar |

- A method drops a noun its receiver names (`Caches().WatchCollectionByCluster`); a scope is a `By<Scope>` suffix in Go and an argument in GraphQL.
- A service method that opens with `Get`, `List`, `Fetch`, `Watch`, `Tail` or `Listen`, or a feed method with `Subscribe`, has that verb's contract. A poller or file watcher takes another verb.
- A state watch sends its value on subscribe. A change notification sends nothing until a change, so listen before the first read.
- A Watch, Feed or Tail that can fail returns `*Stream[T]`; its resolver goes through `subscriptionStream`, which turns `Err()` into the `subscriptionFailed` frame. Every one ends with its `ctx`.
- A field's description opens with its contract. A query with no caller is removed.

## Patterns

- A type's methods live in the type's file; a helper belongs to whoever calls it, and only what more than one needs goes on the service.
- Pub/sub: unkeyed → `gochan` (`watch` for latest-value with a seed, `broadcast` for fan-out); keyed → `gobus` (`watch` delivers nothing until the next send; `conflate` for bursts). Never hand-roll a subscriber map.
- Fan-in from producers that come and go → `gochan` `mpsc` with `WithKeepOpen`, a sender per producer; without the option the hub closes for good once no sender is open.
- **Every outgoing request names the app** (`version.UserAgent()`, `kubetail-desktop/<version> (<GOOS>/<GOARCH>)`) ahead of its own: a new `http.Client` takes `version.Transport`, a proxy calls `version.Prefix`. Exception: the AWS SDK appends the bare token.
- Work to do is a queue, not a bus: `internal/lib/workqueue`, one `Queue` per job. `Done` is owed for every key taken.
- Unexported functional options for test seams; exported `New` takes production knobs only.
- **Production code reaches the API server through `kubeconn.Connection`'s `Dynamic` client or raw requests on its `HTTPClient`.** Never import `k8s.io/api/...`, `client-go/discovery` or the typed clientset (`client-go/kubernetes`): `kubernetes/scheme` links every API group, about 16 MB. Tests may use them. `make vet-desktop` and CI fail on them (`scripts/check-sidecar-deps.sh`).

## Persisted formats

Every file the sidecar keeps has a row in [`docs/persisted-data.md`](../docs/persisted-data.md),
with its class, its version stamp and the test that pins it, and that file's *How a change
ships* is the procedure for each kind of change. **A change to a format updates its row in the
same commit.** What the sidecar's side of it looks like:

- **A SQLite schema** (`appdb`, `kubestore`, `logstore`): the next numbered `*.sql` plus its
  SHA-256 line in the package's `shippedMigrations`. **A shipped migration is never edited or
  removed**: `TestAShippedMigrationIsNeverEdited` fails on a changed, added or missing file. A
  migration past `0001` ships with a test that builds the previous schema from the frozen files,
  seeds it, migrates and reads the seed back. **Rebuilding a `kubestore` table a trigger names**
  fails at `ALTER TABLE … RENAME` (*no such table* in the trigger): drop the triggers first and
  recreate them after.
- **A JSON column**: rides `app.db`'s version, so the change is an `app.db` migration.
- **A path under `<data>` or `<cache>`**: never renamed, moved or removed. There is no app-level
  data migration yet, so the layout only grows: a new path is its owner's to create.

A refusal reaches the user. `sqlitemigrate` refuses a newer file with `*NewerSchemaError`, which `build`
turns into *app.db was written by a newer Kubetail (…)*; the error's own text is the reason, logged
as `app init`. When `app.New` fails, `run` also prints `FATAL <reason>` on stdout for whatever
spawned it, as `main` does for a bad flag or root; the host shows it on the start screen. Tests make a newer file with
`sqlitemigrate/sqlitemigratetest`'s `StampNewer` and checksum a migration set with its
`Checksums`.

→ [ADR: each persisted format carries its own version stamp](../docs/adr/2026-10-29-each-persisted-format-carries-its-own-version-stamp.md),
[ADR: a shipped schema changes only by a new migration](../docs/adr/2026-11-08-a-shipped-schema-changes-only-by-a-new-migration.md).

## Tests & checks

- testify + `httptest`. Resolver tests stand up `graph.NewServer(ctx, &graph.Resolver{...})` + `POST /graphql`; lifecycle tests stand up `app.New(...)`. Filesystem via `t.TempDir()`.
- A fixture that needs a stored status writes it with `beehive.NewAdminClient`, never by registering a controller. A controller's own writes are asserted by calling `Reconcile` against a stubbed `ControllerClient`.
- Wait on channels through `internal/lib/testutil` (`Wait`, `Recv`, `RecvClosed`, `WaitClosed`); the one failsafe is `testutil.Timeout`.
- A fake that notifies the test uses `testutil.Signal` (single-shot) or `testutil.Probe[T]` (repeating, drops oldest). A consumer doing edge detection needs a lossless fan-out instead.
- `make vet-desktop` runs go vet and staticcheck on every build `scripts/go-builds.sh` lists: each GOOS, linux/arm64 and each build tag. A new split joins that list and `vet-desktop`'s `go vet` lines.
- **`make vet-desktop` and CI fail on code `go fix` would rewrite** (`scripts/check-go-fix.sh`), on every GOOS, on linux/arm64, and on the `debug`, `livecache` and `liveswitch` tags. Run the `go fix` command it prints and review the result.
- **CI's race step runs `go test -race -short ./...`** with `GORACE=atexit_sleep_ms=0`. A sweep too slow for the race detector shrinks under `testing.Short()`; a test never skips under it. The nightly stress run sets neither `-short` nor `GORACE`, so the full sweep and the exit sleep run.
- A test outside `services/cluster` that needs a real cache store uses `cluster/clustertest` (`NewStore`, `Apply`, `Delete`, `Mirror`, `Read`); it carries the query views. Its `Connection` is one a test can `Retire`.

**A nightly stress run hunts concurrency bugs** (the root `.github/workflows/stress-desktop.yml`):
every desktop runner, `-shuffle=on -count=3`, one job per `-cpu` value (1 and 4; the `cpu` dispatch input is a
JSON array, `[1,4]`), each with `-race` (keeping `checkptr` on for modernc's SQLite) and without.
A test that hangs under `-cpu 1` is a finding. Each failing test gets an issue with the command that
reproduces it from the repo root, labelled `desktop`:

```sh
go -C apps/desktop/sidecar test -race -cpu 1 -shuffle=<seed> -count=3 ./internal/agent/chat
```

**Coverage is measured in `coverage-desktop.yml`'s `Measure · Go`**, one runner per OS, beside the tests: the suite
untagged and, on Linux alone, with `-tags debug` (the only build with the environment overrides and
`KUBETAIL_FAULTS`; a test only that build can pass carries `//go:build debug`; no debug-tagged file is
OS-specific, so Linux's pass reaches all of them), both with
`-coverpkg=./...` and `CGO_ENABLED=0`, as the release is built.
`tools/coverignore` merges the two profiles and drops every block a marker exempts: the statement
or function whose first line ends in `// coverage:ignore <reason>`, body included, and the lines
from `// coverage:ignore-start <reason>` to `// coverage:ignore-stop`. Codecov merges the three
OSes, so a `_windows.go` line is judged by the Windows upload. Generated files are left out of coverage, in `scripts/cover-changed.sh`'s `ignored` and Codecov's
`ignore`. `make cover-desktop-changed` lists the changed lines this OS leaves uncovered.

When you change the sidecar's wiring or conventions, update this file or the package's guide in the
same change. When you change *why*, write an ADR.
