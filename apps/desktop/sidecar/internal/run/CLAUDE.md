# internal/run — what confines one run

Ten leaves that know no tool and no record: `loginshell` (the user's shell, read once a launch),
`session` (one run's policy), `sandbox` (the machine's sandbox), `permissions` (the decision over an
action), `kubeproxy` (the proxy a sandboxed command reaches its cluster through),
`runproxy` (what every cloud platform's proxy shares, and the run's server), `awsproxy` (AWS's),
`azureproxy` (Azure's), `githubproxy` (GitHub's) and `gcpproxy` (Google Cloud's). A tool imports
these; none imports a tool.
Posture and the tests that pin it:
[`docs/security-model.md`](../../../docs/security-model.md).

## The login shell (`loginshell`)

`app.New` runs the user's login shell once per launch and on Refresh PATH, **in the sandbox**
(macOS and Linux; never on Windows). `Resolve` answers `Path` (for the settings sync), `Env` (the
allowlisted environment, set process-wide on macOS, so the connectors' browser opener (`lib/oauth/browser`) resolves
`open` against it too) and `Keys` (provider keys, never set on the
process). The shell is the account record's (`accountShell`), never `$SHELL`. The environment is
scrubbed and `PATH` starts at `DefaultPath`. **`loginshell.In`** (`sandboxed.go`) is the one place
its sandbox policy is built; a failed start is a `Fault`, so it never runs unconfined where a
sandbox exists. → [ADR: the login shell runs in the
sandbox](../../../docs/adr/2026-10-04-the-login-shell-runs-in-the-sandbox.md), [security
record](../../../docs/security/2026-10-04-the-login-shell-in-the-sandbox.md).

**The allowlist (`imported`) is the security boundary**; adding a row is a security change, since
credential plugins inherit it. No wildcards, no secrets (`TestImportedIsExactlyTheAllowlist`).
Nothing from a kubeconfig, cluster data or the socket may reach the shell's command. → [ADR:
shell-environment allowlist](../../../docs/adr/2026-09-07-shell-environment-allowlist.md).

Traps (→ [ADR: the login shell is read by
markers](../../../docs/adr/2026-10-17-the-login-shell-is-read-by-markers-in-a-session-of-its-own.md)):
run it in a **session**, not a process group; the marker starts with a letter and the separator
is `\000`; stop at the last marker, never EOF; keep each value byte for byte; follow `printenv`
with `|| true`. Any failure keeps the inherited environment, logs one fixed reason and never a
value, and surfaces as `sandboxPathFaultGet`.

## The session (`session`)

**A `session.Session` is one agent run's policy** (`internal/run/session`, a leaf a proxy
imports, never the reverse): `Kind` (`permissions.Principal`; `Console` is the Sandbox console's, which no chat holds), `Outside`, and live readers `Policy` (by
target and account: its mode, and every rule), `Network` (a failed
read is `NoNetwork`) and `Folders` (read only through `GrantedFolders`; a failed read is an error, never none). What a kind may do
is the boundary's, never the session's. **`session.Narrow`** builds a subagent's from a chat's (any other kind keeps its own): identity
copied at spawn, each live field handed on or tightened, never widened. A new field is classed by
that rule and gets its line in `TestNarrowKeepsTheParentsIdentity`.

## The sandbox (`sandbox`)

**`internal/run/sandbox` is the machine's sandbox.** →
[Bash runs in a sandbox](../../../docs/security/2026-09-28-bash-runs-in-a-sandbox.md).

- **`Probe(ctx)`** answers a `*Sandbox` (nil for none) and a `Status`, or `ctx`'s error: a probe
  cut short is no verdict.
- **`(*Sandbox).Command(ctx, Run)`** builds the unstarted process for a `Run`. **It fails rather than narrows or widens**: a run failing `Run.check`
  (its `Policy.Check`, or an `Env` entry `Unpassable` matches) or one its platform cannot enforce
  is an error the model reads.
- **Windows has none**: `Probe` answers none, `Command` errors →
  [ADR](../../../docs/adr/2026-09-28-native-windows-has-no-sandbox.md).

**A `Policy` is everything enforced for a run** (`policy.go`):

- **`Files`**: `Read`, `Write`, `Deny`, each covering a subtree. **The deepest rule wins; a tie
  goes to the narrower** (Deny, Read, Write). **Nothing may sit beneath a Write rule** (a run can
  rename what it writes). An uncovered path is off limits.
- **`Always`**: `Deny` (denied-always) and `Kubetail` (Kubetail's directories, where only the run's
  own paths open). **No Files rule opens an Always path**, so a Read of `~` never exposes `~/.ssh`.
- **`Network`**: at most one `Relay` (loopback port → Unix socket outside), `Internet`, and a
  `Resolver` (Linux only). Zero value is no network.
- **`Limits`**: `CPUSeconds` (hard limit `cpuGrace` above), `MemoryBytes` (Linux only),
  `OpenFiles`, `Processes`, counted over `Sandbox.CountedProcesses()` plus `forwarderTasks` →
  [ADR: process limits are set inside the run](../../../docs/adr/2026-10-02-process-limits-are-set-inside-the-run.md).

**`Check`** refuses, among others, anything beneath a Write, a run's own path that is a link, a
rule the kernel cannot name, a folder named two ways and a memory or process limit with no
open-files limit. **Two paths compare as files** (`lib/realpath`); text tells only inside one
compiled policy. Both platforms compile one merged list (`Policy.rules`), last match winning, own
paths last. → [ADR](../../../docs/adr/2026-11-22-paths-are-compared-as-files.md).

**`neverEnv` (`env.go`) is the one list of variables that may never pass** (agent sockets,
`DOCKER_HOST`, tokens, loader variables, shell startup hooks, every `AWS_`, `ARM_` and `AZURE_`
but `AZURE_CONFIG_DIR`). **`Run.check` passes one only as a placeholder** (`placeholders.go`): an
exact name holding the value AWS's mount writes, so a real key fails the run →
[the sandbox's own environment](../../../docs/security/2026-10-02-the-sandboxs-own-environment.md).

**The zones are Kubetail's lists** (`lists.go`, `lists_linux.go`, `lists_darwin.go`): `System`,
`Toolchain`, `Never`, `Closed` and `NoWrite` (the home, which a read grant may name and a
read-write one may not). **Adding a location is a security change**: every
run reads it. The zones open no `PATH` entry. `Sandbox.System` never reads a broad folder (`/`,
the home, an app-data folder) through a link or the shell's location. `FixedMount(p)` is what no
grant may name. A stalled mount can hang building a probe's policy or a command: both are
`bounded`, a direct `System` or `Never` is not. A test that strands one waits on `boundedCalls`. →
[ADR: the sandbox's zones are Kubetail's lists](../../../docs/adr/2026-10-02-the-sandboxs-zones-are-kubetails-lists.md),
[path grants](../../../docs/security/2026-10-04-path-grants.md).

**`search.go`'s `SearchFolder` is the one rule for a folder a run's `PATH` may search**; the
settings filter and the per-run check both call it. `FilePolicy.WithSearch` adds the Read rules.

**On Linux it is bubblewrap** (`sandbox_linux.go`): the system's bwrap first, then Kubetail's own
beside the executable, never off `PATH` →
[ADR: Kubetail ships its own bwrap](../../../docs/adr/2026-09-28-kubetail-ships-its-own-bwrap.md). Each
probe runs in its own process group, killed whole at the bound (`runProbe`). `Command` refuses a
rule that would replace a fixed mount (`/tmp`, `/dev`, `/proc`). Mount order matters; see
`mounter`. `/run` and `/var` are never mounted. **A run with `Internet` starts under `pasta`**
(`network_linux.go`) as the user holding no capability; the probe reads no route, so Kubetail
started offline still offers network → [ADR: Kubetail ships its own pasta](../../../docs/adr/2026-10-04-kubetail-ships-its-own-pasta.md).

**On macOS it is Seatbelt** (`sandbox_darwin.go`, `profile_darwin.sb`). A probe that times out
keeps the sandbox. `Port` is a free loopback port →
[ADR](../../../docs/adr/2026-09-28-a-sandboxed-forwarder-holds-a-loopback-port-on-macos.md). **Every
path is a `-D` parameter, never profile text** (a path can hold `"` or `)`), and resolved first.
**`Command` starts the run in its own session (`Setsid`); a caller replacing `SysProcAttr` must
keep it** →
[ADR](../../../docs/adr/2026-10-13-a-macos-run-leads-its-own-session-so-no-run-reads-another-processs-launch-strings.md).
The profile refuses `setsid`/`setpgid` →
[ADR](../../../docs/adr/2026-09-28-a-macos-run-keeps-its-group-by-refusing-setsid.md). Fixed file rules
come after the merged rules so no policy undoes them; `/dev/tty` is never readable. A run with
`Internet` has IPv4 alone →
[record](../../../docs/security/2026-10-04-a-macos-run-with-the-internet-has-ipv4-only.md).
`refusedServices` lists what no profile names.

**`sandbox-shell` sets the limits and confines the shell** (`sandbox.ShellMain`), between the
forwarder and the shell, so limits never hold the forwarder. On Linux it installs the seccomp
filter (`seccomp_linux.go`, classic BPF, no cgo; its architecture in `seccomp_linux_<arch>.go`,
whose `TestTheFilter*` and `TestTheShell*` tests CI's `Check · AppArmor profiles` runs
on both architectures on every pull request, picking them by name) on every thread with `no_new_privs`, then sets
limits, clamped to existing hard limits. **Under `--memory` or `--processes` it execs through a
raw `execve` with allocation done first** (`execLimited`): an allocation past `RLIMIT_AS` crashes
the runtime.

**The forwarder is `kubetail-sidecar sandbox-init`** (`forward.go`; on Linux a run with the internet
starts as `sandbox-pasta`, `sandbox.PastaMain` in `pasta_linux.go`, which keeps pasta's own output
out of the run's and hands the run's stderr to `sandbox-init` on stdin, `--stderr-on-stdin`), reached through `sandbox.Main`
before `main` reads a flag; `ForwarderArgs` is the one writer of its command line. It relays its
one port to the socket, reaps as PID 1, and checks nothing (every check is the proxy's). **It
catches and drops `SIGTERM`/`SIGINT`/`SIGHUP` with `signal.Notify`, never `signal.Ignore`**: an
ignored signal stays ignored across `exec`. Its own failures, and `sandbox-shell`'s, exit 125 with
one `sandbox-init:`/`sandbox-shell:` line; the line is what tells them from a command's 125.

**Every run ends with the sidecar, however it dies.** `Probe` makes one pipe, the lifeline;
the `Sandbox` holds its write end as a bare descriptor (no finalizer closes it, only the process's
end) and never writes to it. Each run inherits the read
end on fd 3 (`ExtraFiles`), and `--lifeline 3` tells the forwarder, which marks it close-on-exec
and, at EOF, kills its process group and exits (`watchLifeline`); on macOS a process that left the
group through `posix_spawn` survives (security model). On Linux a run with the internet hands it
to `sandbox-pasta` instead, since pasta closes every descriptor it did not open; at EOF it kills
its group, pasta and the child pasta makes during setup with it; `Command` starts every run in a
session of its own on both platforms, so that kill reaches nothing of the sidecar's. A failed or
cancelled probe closes the pipe; `Close` closes a probed `Sandbox`'s, ending its runs, once, and
`Command` refuses after it: the runtime holds it as a part, and **a test that probes registers it
as cleanup** (`closing`, `confining`).

**Tests.** A test binary that starts the chain calls `sandbox.Main` from its `TestMain`, or it
runs its own suite inside the probe; each helper branch exits by `testutil.ExitHelper`. A test
needing a sandbox calls `testutil.RequireSandbox` (fails under `KUBETAIL_REQUIRE_SANDBOX=1`, which
CI sets). **No test writes inside the module** (Go's test cache keys on it); fixtures go under
`testutil.GrantableDir`. `app`'s `startE2E` runs a chat through the real sandbox.

## Permissions (`permissions`)

**`internal/run/permissions` is the decision** (`permissions.go`, `match.go`), over one classified
`Action`: `Principal`, `Target` (`Kubernetes`, `AWS`, `Azure`, `GCP`, `GitHub`), `Scope` (`/`-separated
segments, the first the account: a cluster's id, a cloud account; `""` is an account the classifier
cannot trust), `AccountName` (words only), `Service`, `Operation`, `Resource`, one `AccessLevel`
(`read`, `data-read`, `write`, `access-control`, `destructive`, `reach`), `Confined`, `Once`, `Spans`,
`DryRun`.

- **`Policy.Authorize` takes the first of**: the boundary's `Never`, a `Deny` rule, the mode's deny,
  an `Ask` rule, the limit `Once`, an `Allow` rule, the mode's allow, else ask (`Grantable` unless a
  dry run or a rule it cannot store). `boundary` is Kubetail's table by principal and level (`Free`,
  `Scoped`: free when `Confined`, `Once`, `Never`); `Once` and an empty scope raise the limit to
  `Once`. `modes` is the default effect per mode and level. A principal that does not ask (`asks`)
  is denied what would ask; a subagent takes the stricter of its verdict and the chat's. **Order-independent**;
  rule order picks the reason alone. Every principal and mode has every level.
- **A rule** names a level, a principal and a target (or `*`), and patterns: `Service` and `Operation`
  by `Match`, `Scope` and `Resource` by `MatchPath` (`*` never crosses `/`, `*` alone is anything, a
  last `**` is that and below). No empty pattern is a wildcard; `Service` `""` is the core group. A
  deny or ask matches the levels its own `Covers` (`write` covers `destructive` and `access-control`),
  and a scope below a `Spans` action's; an allow its own level and scope, all below for `Spans`. **Every pattern Kubetail writes from a value goes through `Literal`.**
- `GrantRule` (the level in the scope as a literal, and below it for `Spans`, for every principal; a one-segment scope names
  its service and resource too) and `CommandRule` (also the operation) are an answer's rules.
  `Refused` and `RefusedData` stand in for rules Kubetail could not read. `Rule.Line(account)` is the
  rule in words; keep its quoting, so ` / ` outside quotes separates segments. A folder grant is no rule.

**`kubeproxy/classify.go` classifies a request**: scope `<cluster id>[/<namespace>]`, a Namespace
object at cluster scope and `Once`; `reach` is `classifyRefusal`'s (the handler still refuses it);
`destructive` the list `AlwaysAsks` words, a replica write `keepsReplicas` cannot read as plain
included; `access-control` RBAC, admission and a CSR's approval (`AccessControl` in words), `Confined` in
a namespace; `data-read` a `GET` of core `secrets` not metadata only, `Spans` in no namespace; a dry run on a group version in
`honorsDryRun` is `read`. A write `builtin` lacks in a built-in group, or a custom resource's
subresource but `status` and `scale`, is `Once`. →
[ADR: permissions are access levels, a boundary, modes and rules](../../../docs/adr/2026-11-25-permissions-are-access-levels-a-boundary-modes-and-rules-over-one-action.md),
[the permissions engine](../../../docs/security/2026-10-02-the-permissions-engine.md),
[one engine for every target](../../../docs/security/2026-11-25-target-neutral-permissions.md),
[Secret data is a permissioned read](../../../docs/security/2026-10-05-secret-data-is-a-permissioned-read.md).

## The cluster proxy (`kubeproxy`)

**`internal/run/kubeproxy` is the cluster proxy**; it imports nothing of `tools` or
`services/cluster`. Files: `kubeproxy.go` (grant, handler), `policy.go` (path parse, read
policy), `write.go` (write path), `classify.go`, `diff.go` (preview), `secretread.go`, `redact.go`
and `redact_helm.go` (Secret rewriter), `status.go` (every refusal's `Status`, and `Unauthorized`).
→ [ADR: the proxy reads a request as the API server will](../../../docs/adr/2026-10-17-the-cluster-proxy-reads-a-request-as-the-api-server-does.md).

- **A `Grant`** (`NewGrant`) holds the run's session, the cluster whose policy decides its writes and
  the kube-context they are classified in, a
  256-bit token, an `Upstream` (an `Endpoint` per request, whose `Done` cancels requests still open,
  watches included), a `rate.Limiter`, a cap on open requests, and an `Asker` (or a refusal every
  write answers). `End` makes every later request 401 and cancels those in flight; `Wait` joins
  every handler it let in.
- **The token is proxy credentials** (`Proxy-Authorization` via the run's `proxy-url`); the handler
  deletes it and `Authorization` before forwarding, so the connection's transport adds the user's.
- **The handler, in order:** `Host` must be `cluster.kubetail.invalid` and the credentials match
  (constant-time), else 401; the policy, else 403; a write goes to the write path; a body over
  64 KiB is 413; no slot is 429 with no `Retry-After`; the limiter; `Endpoint` (each sentinel a 503
  with its own message, never the error's text); then a per-request `httputil.ReverseProxy` with
  `FlushInterval: -1`, an upstream failure a 502.
- **The policy** refuses a non-canonical path first, then parses as `RequestInfoFactory` does. `GET`
  of resources, discovery, `/version`, `/openapi/…` and health paths pass. `POST`/`PUT`/`PATCH`/
  `DELETE` of a resource go to the write path, except a `POST` of a self review, which passes.
  Refused: `exec`, `attach`, `portforward`, every `proxy` path, `Upgrade`/`Connection: upgrade`/
  `Impersonate-*` headers, a ServiceAccount's `token` subresource (its answer is a credential), and
  anything else.
- **The write path** (`write.go`) refuses, before any queue, a grant with no asker and a query
  `url.ParseQuery` refuses. It then takes the write lock (a one-slot semaphore, at most
  `maxQueuedWrites` waiting, one more a 429) and holds it until the forward returns, so writes reach
  the cluster in the order decided; reads the body under `maxWriteBody` (1 MiB); runs **the helm
  gate** (refuses a helm release write when `redactedReads` holds its namespace or `""`, since helm
  rebuilds a release from the Secrets it read — ahead of `checkBody`, so the refusal says why); then
  `checkBody` (encoding, UTF-8, media type, `decodeBody`, `uniqueKeys`, any `[redacted]` mark or its
  base64, and `checkRelease` inside a release). Then it classifies the request into a `Write` and
  takes one verdict: `Authorize` over the session's `Policy` for the grant's cluster plus the
  grant's `commandRules` (no `Policy` is read-only), the session's kind its principal. A `read`
  (an honored dry run) is forwarded unasked and unrecorded.
- **Verdicts.** `Allowed` is recorded through `Asker.Record`, then forwarded; a failed record
  forwards nothing. `Denied` is recorded and answered 403. `Prompted` is previewed, then `Ask`ed
  with a `Request` (action, `Grantable`, the rule lines each allow answer adds, the `Write`, the
  diff). Only an approval forwards, taking a slot and the limiter then, so a write waiting on the
  user holds neither. A `Command` approval adds `CommandRule(act)` to the grant for the rest of the
  command. → [cluster writes ask](../../../docs/security/2026-09-29-cluster-writes-ask.md),
  [the permissions engine](../../../docs/security/2026-10-02-the-permissions-engine.md),
  [ADR: a sandboxed command asks for each cluster write](../../../docs/adr/2026-09-29-a-sandboxed-command-asks-for-each-cluster-write.md).
- **The preview** (`diff.go`) runs only for a write that asks, a `PUT` or `PATCH` of a named object
  on a group version in `honorsDryRun`: a `GET`, then the request with `dryRun=All`, bounded by
  `diffTimeout` and `maxDiffObject`. Both answers drop `managedFields`, `resourceVersion` and
  `generation`; Secret values and every last-applied annotation are compared, then redacted, before
  the diff. A failure is a `DiffError` in the user's words.
- **The Secret read** (`secretread.go`): a read of core `secrets` that is not `metadataOnly` is
  decided like a write, under the same lock (`takeWriteLock`, `decideSecretRead`), and only
  `Allowed` or an approval shows the data. A read that passes redacted marks its namespace in
  `redactedReads`, which the helm gate reads. The lock is released before the forward, so an allowed
  watch never holds it. `forward` takes `redact`: only an allowed Secret read passes unredacted.
  → [Secret data is a permissioned read](../../../docs/security/2026-10-05-secret-data-is-a-permissioned-read.md).
- **A redacted `secrets` request is rewritten both ways** (`rewriteSecrets`): `Accept` narrowed to
  JSON and `Accept-Encoding` dropped; any other response type or encoding is a 502. The body is
  rewritten as a stream (`json.Decoder`, `UseNumber`), `items`/`rows` one element at a time.
  `redact` replaces each `data`/`stringData` value and every `last-applied-configuration`.
  **A helm release is redacted inside** (`redact_helm.go`). **A failure fails closed**: a 502
  before the first piece, a cut body after (a watch sends its status line at once).
  → [ADR: helm release Secrets pass redacted inside](../../../docs/adr/2026-09-28-helm-release-secrets-pass-redacted-inside.md).

## The cloud platform grant (`runproxy`)

**`internal/run/runproxy` is what each cloud platform's proxy (`run/<platform>proxy`) builds on**;
it imports `permissions` and `session` alone. `NewServer` is the run's server: HTTP/1.1, no read or
write timeout, which would cut a watch. With a `Certificate` (`cert.go`: ECDSA, `127.0.0.1` alone,
key in memory) it serves TLS on the same listener: `listen.go` reads a connection's first byte
under the header bound; `IsTLS` tells a handler.

- **A `Grant`** is one run's access to one cloud platform: a 256-bit token, its `Limits`, and an
  `Asker` or a refusal. `NewGrant` panics on `Kubernetes` or a zero limit. `End` makes `Holds` and
  `Enter` false and cancels what `Enter` let in; `Wait` joins it. `Challenge` answers 407 when
  `Limits.Challenges`, else 401.
- `Body` reads a body in one of `Limits.Bodies` slots (403 encoded, 429 none free, 413 past 1 MiB);
  `BodySlot` takes one for a body the cloud platform reads itself; each cuts a read short at `End`, as
  `CutReadsOnEnd` does for a body streamed unread;
  `Slot` takes a slot, then the limiter; `BodyRefusal` and `SlotRefusal` word their statuses; `IsForm` reads a form's type;
  `Upgrades` reads an `Upgrade` header, even an empty one, and the `Connection` list, for the
  run's door and `kubeproxy` alike; `Outbound` never copies `Authorization` or
  `Proxy-Authorization`.
- **`Decide` is the one decision path**: it stamps the target and the session's kind and keeps
  every other field, refuses with no asker and a request that cannot be shown (not UTF-8 with no
  `Unshown`, a query or form body that does not parse), then takes its lock (`maxQueued` waiting)
  and reads `session.Policy` for the action's account plus the command's rules. An allow is
  recorded, then forwarded; a deny recorded and refused; an ask goes to the `Asker` with
  `Grantable` and the rule lines, and a `command` `Answer` adds `CommandRule`. A read never reaches
  it. A data read gives up the lock before `forward`, a change at its return. **Each proxy's
  classifier fills every field**, an account's scope canonical and lower case, and names in an
  `ordinary` table the changes a mode or rule may allow; any other is `Once`. One reaching past its
  account keeps the scope and is `Once`.

## Azure's proxy (`azureproxy`)

**`internal/run/azureproxy` is Azure Resource Manager's proxy** over a `runproxy.Grant` and the
`Account` its run mounted with. The token is a bearer, or `X-IDENTITY-HEADER` at `IdentityPath`,
the managed-identity endpoint, which answers the grant's token. `path.go` reads Resource
Manager's grammar, its words ASCII, any case, and two polling paths (`GET`).
`classify.go` refuses AKS run-command, access profiles, a credential action (by name, or listed:
`publishxml`, `readonlykeys`, any method; `reach`) and an unnamed `list`/`get`; then data reads (the
secret-metadata list, and an operation's result in its namespaces), reads, `Microsoft.Authorization`
at any scope (`access-control`, `Confined` for a role assignment in a group), destructive, writes;
a data read in no group `Spans`.
A change naming no subscription is refused; any other goes to `Decide`. Forwarding adds
the user's token for the subscription's tenant; a `401` drops it and answers the
sign-in sentence. `rewrite.go` points `Location`, `Azure-AsyncOperation`, `Operation-Location`
and the top-level `nextLink` at the door. A `429`, which `az` retries, is answered `403`. →
[ADR](../../../docs/adr/2026-11-17-a-sandboxed-command-reaches-azure-through-a-token-injecting-proxy.md),
[record](../../../docs/security/2026-11-17-azure-through-the-proxy.md).

## The AWS proxy (`awsproxy`)

**`internal/run/awsproxy` re-signs** →
[ADR](../../../docs/adr/2026-11-17-a-sandboxed-command-reaches-aws-through-a-re-signing-proxy.md).
The run's key id is `KUBETAIL` and its grant's token. The handler reads service and region from the
SigV4 scope (`scope.go`), refuses a service off `services.go`'s table, drops the sandbox's
signature, and signs for the table's host with the user's credentials (`Identity`, `forward.go`);
a login resolving none answers its `Refusal`'s text, else a fixed sentence.
The classifier (`classify.go`) reads the operation only where the row's protocol names it, refusing
one named elsewhere or twice, then checks refused (`reach`), data reads, destructive,
`access-control` (`iam`, a policy, permission or grant; S3's `s3AccessQuery` and ACL headers, ahead
of destructive), then a read, and decides the rest. The scope is the credentials' account and the
region. A read naming another account's bucket, registry or template
URL asks; `Buckets` is the login's own, one list shared by its runs, trusted only while fresh and
for the login listed (its account, else access key). One credential read
serves the owner, the decision and a read's forward, which on an owned bucket sends
`X-Amz-Expected-Bucket-Owner`; a change's forward re-reads, sending nothing as another login.
An `x-amz` header off `headers.go`'s list, a forwarded header sent twice, a streamed or multipart
upload, and a body that is not text are refused, as its service's clients read errors
(`refuse.go`). An error answer is read whole and checked for the user's key; a dead credential's
code calls `Drop` first and forgets the bucket list (`readError`, the list's too). Adding a row or a header is a security change.

## GitHub's proxy (`githubproxy`)

**`internal/run/githubproxy` is GitHub's proxy over a `runproxy.Grant`**; the token comes through
its `TokenSource`. → [ADR](../../../docs/adr/2026-11-17-a-sandboxed-command-reaches-github-over-plain-http-at-ghs-own-host-name.md).

- **The hosts are `gh`'s**: `GitHost` (`github.localhost`, Git's smart HTTP to `github.com`) and
  `APIHost` (`api.github.localhost`, REST and GraphQL). The grant's token is the `Basic` proxy
  password, and the grant challenges with `407`, since `git` sends it only once asked.
- **The headers forwarded are lists by exact name** (`apiHeaders`, `gitHeaders`); `Git-Protocol`
  rides a fetch alone and `GraphQL-Features` `/graphql` alone. A refusal starts `kubetail:`, a 403,
  in GitHub's JSON on the API host and text on the Git host.
- **The classifier** (`classify.go`, `graphql.go`): a read passes; a change, a push and
  `dataPaths` are decided; `credentialPaths` is refused (`reach`); a change to who may do what
  (`accessControlPaths`) is `access-control`; a route by id has scope `""`; `withBody` reads `accessFields`; `spanningPaths` are `Spans`. A path matches in any case, and one by id (`/repositories/{id}`,
  `/organizations/{id}`) as its form by name. GraphQL is a JSON `POST` with no query string; any
  mutation makes its document a change (`accessControlWords`, a parsed `maintainerCanModify`:
  `access-control`; mixed with destructive, refused).
- **The push reader** (`push.go`) reads at most 64 KiB of pkt-lines (shallow lines, commands,
  options) in a body slot, refuses what it cannot read, then streams the pack unread. An answer
  GitHub breaks off is broken off to the client.
- **No redirect is followed.** A link to the upstream's own origin, in `Location` or `Link`, is
  handed back as the route's; one to another host passes on an approved data read alone.

## Google Cloud's proxy (`gcpproxy`)

**`internal/run/gcpproxy` is the proxy a sandboxed `gcloud` reaches Google through**, over a
`runproxy.Grant`. It reaches the user's token through `TokenSource`, never `services`.
`services.go` is the table: six services, each one upstream host and its path prefixes, routed as
`<service>.gcp.kubetail.invalid`. `request.go` reads a path by decoded segment and refuses an encoded
`/` or `.` outside a storage object's name. `classify.go` answers read, change, data read,
`access-control` (`setIamPolicy`, `iam`, a storage ACL) or refused (`reach`); a project only as an
id is an account; billing another (`X-Goog-User-Project`, `userProject`, each once) is `Once`. `redact.go` blanks secrets in `compute`'s and `container`'s 2xx answers and fails
closed. **Every refusal is Google's 403**, a grant's 429 included, since `gcloud` retries a 429 and
a 5xx. → [ADR](../../../docs/adr/2026-11-17-google-cloud-is-reached-with-the-active-gcloud-accounts-token.md).
