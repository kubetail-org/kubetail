# internal/tools — every tool the model can call

Each package directly under `tools/` is one tool; what they share sits beside `box.go` and under
`tools/internal/`. What confines a run is `internal/run/CLAUDE.md`; who calls a tool and files its
rows is `internal/agent/CLAUDE.md`.

## The box

**A tool's kind is the interfaces it implements** (`tool.go`); the loop finds them by type
assertion.

- **`Reader`** (every tool): `Name()` and `ContractName()` are each unique in the box, and every
  stored call names and records them.
- **`Runner`** adds `Run`, which never returns a Go error's text. `Custom` is offered by its
  `Definition()`; `PerTarget` adds `For(target)`.
- **A vendor's tool** is any tool that is not `Custom`: `Name()` is ours, `ContractName()` the
  vendor's shape id. The provider runs it when it is `Budgeted`, the sidecar when it is a `Runner`.
  A server tool's package is named for its vendor, whose SDK it imports.
- **`Gated`** adds `Approval`, which only reads `rt`; an error it returns may be a `*Refusal`,
  carrying the result the model reads in place of `bad-input`. **`Shown`** adds `RunShown`, whose
  action the row keeps → [ADR](../../../docs/adr/2026-10-08-a-shown-tool-records-its-action-at-the-run.md).

**One `Box` holds every tool** (`box.go`), in offer order, plus readers for tools this machine cannot
offer. `NewBox` panics on a wiring mistake. `For(target, names)` is a turn's box; a name with no tool
is skipped (how a machine with no shell drops bash). `WithoutNames(names)` drops the tools the user
turned off from the offer, the prompt and `Runner`, keeping the readers. **Every stored call is read
through the box, under the row's name and contract**; callers ask the box rather than range over it,
so kinds never leak past `tools`. → [ADR: every tool is in the box](../../../docs/adr/2026-09-24-every-tool-is-in-the-box.md).

**A tool's `Runtime`** (`rt`) is the chat its call runs in. `agent/chat` sets every field; a test sets
the ones its tool reads. `Agent` and `Watches` are nil in a subagent's and a monitor's. The chat,
cluster and workspace come from the runtime alone, never the session.

- **Open and remove the chat's directories only through `rootdir`** (`internal/lib/rootdir`): it
  refuses links and anything but a directory, and `RemoveAll` never follows a link →
  [security record](../../../docs/security/2026-09-30-a-run-cannot-replace-its-own-paths.md).
  `WorkspacePath` is where every command starts; `ToolHomePath` where sandboxed tools write.
- **`FileStamps`** keys by `fileguard.Abs`; a `Stamp`'s `Whole` is set only when the model saw every
  byte unaltered.
- **`ActionAsker`** puts a sandboxed command's classified action to the user, or `Record`s one
  decided unasked. Nil where nobody can be asked.
- **A tool that acts through a service is built with it** (in `app`'s `chatTools`) and maps the
  service's errors to the model's codes → [ADR](../../../docs/adr/2026-09-27-a-tool-calls-its-service-directly.md).
- `InlineLimit` caps a result; `FileLimit` caps saved output and Read.
- **The chat's `defaultToolTimeout` is 2 s.** A tool that does I/O is `Bounded` and names its own;
  one that forgets times out silently.

## Actions and results

**A tool that can show a call exports `ActionOf`** over the same `parse` its `Run` uses. A
`tools.Action` is a `Description` and exactly one kind; its JSON tags are the wire (gqlgen binds
`ToolAction` onto it). **A call's kind is its tool's** (`Box.ActionKind`), whatever the arguments
hold; `TestEveryActionKindIsServed` exists because a kind the gqlgen binding lacks is served as `""`
silently. **A call that asks has an action the request can draw**: a `Command`, `Read`, `Write`,
`Edit`, `Fetch`, or a `Memory` with `Scope` `everywhere`. → [ADR: a tool call shows itself from its
arguments](../../../docs/adr/2026-09-23-a-tool-call-shows-itself-from-its-arguments.md).

**Every tool has a contract** covering what its calls store: its arguments, its shown action, and
for `LogsWatch` its task's `WatchResult`. Ours are `<name>/v<n>`, exported as `ContractName` in the
tool's package. **The box reads a row only under its reader's contract**; a refused or undecodable
payload is a bare sentinel (`ErrNoReader`, `ErrContract`, `ErrDecode`, `ErrKind`), never the
decoder's error. **A change that would read an old row differently bumps the contract**: a stricter
`parse`, or a stored field renamed, retyped or newly required. An optional addition does not. The
first bump moves the check into the tool, whose every decoder, the watch result's included, then
reads each contract it has written. A bump lives in the tool's package: `ContractName` moves on, the
old value stays as an unexported constant saying what changed, and the fixture tables
(`TestActionOfReadsOldRowsTheSame`, `TestShownOldRowsDecodeTheSame`) gain one; **they are
append-only**. → [ADR: a tool call records its tool's contract](../../../docs/adr/2026-10-25-a-tool-call-records-its-tools-contract.md).

**An oversized result is saved one way for every tool** (`save.go`): past `InlineLimit`, `Fit` sends
the text to the `Saver` and answers a `<persisted-output>` block with a preview. A failed save falls
back to `Cut`, never splitting a rune. `SaveTo(dir)` names the file itself, never with a provider's
id.

## The file tools' checks (`tools/internal/fileguard`)

**The checks every file tool makes on a path**; it imports `tools` alone. Its errors are values, so
each tool words its own refusal.

- `Abs` is the path a tool opens: absolute and plain, with no control character; Git Bash drive
  paths read as native; UNC and device paths refused on Windows.
- `Fence` is Kubetail's directories, which no file tool opens but the chat's workspace. **`Holds`
  re-reads each directory on every call and makes a missing one 0700**: the OS or user may recreate
  one while Kubetail runs.
- **`Fence.File` is the file Write and Edit change**: under the workspace by name, a name under
  `OpenWorkspace`'s root; else the path, once `Holds` finds it outside.
- **A granted folder is reached by handle** (`grant.go`, `walk.go`). `WithHidden` gives the fence the
  zones' `Never` and `Closed` paths, **compared as files each time, never cached**. `Granted`
  resolves on a goroutine abandoned when `ctx` ends (a dead network mount blocks in a syscall),
  answering false so the tool asks. `Walk` opens `/` as an `os.Root` and walks down without
  following links, compares each handle with the closed paths, and never reads through a folder
  that was missing at the walk. `WalkRefusal` is its one wording for the model.
  → [path grants](../../../docs/security/2026-10-04-path-grants.md),
  [ADR: a folder grant is a rule](../../../docs/adr/2026-10-04-a-folder-grant-is-a-rule.md).
- `Open` accepts only a regular file of at most `FileLimit`, by `Lstat` before and `Stat` after;
  `O_NONBLOCK` on Unix keeps a FIFO from blocking the open.
- **The write half** (`write*.go`): `Replace` renames a synced temp file over the target; `Create`
  never replaces and never follows a planted link. Both check `ctx` before changing the disk and
  remove what they made on failure.
- **The root forms** (`…In`) take an `os.Root` and refuse what their path forms refuse; `OpenIn`
  refuses a file that is not the one its `Lstat` saw (`ErrChanged`). Writability under a root is the
  owner's mode bits.

## Read, Write and Edit (`tools/read`, `tools/write`, `tools/edit`)

**`Approval` classifies a path by name alone**, touching nothing on disk: unasked in the chat's
directory (Read) or workspace (Write, Edit), and in a granted folder through `Fence.Walk` alone (a
failed read of the granted folders refuses the call, `tools.FoldersUnread`); refused elsewhere in
Kubetail's directories; asked for the rest. **File work runs on a goroutine**: a stat on a dead
mount blocks where no context reaches. Refusals are words, never a Go error's text. Read redacts the
whole file and sets the path's **stamp**; Write needs a matching `Whole` stamp and Edit a matching
one, so the model Reads first. Edit refuses a `new_string` holding `safe.Redacted`, and its overlap
probe stays linear in the file's length. → [read any approved
file](../../../docs/security/2026-09-23-read-any-approved-file.md), [the write
tool](../../../docs/security/2026-09-24-write-tool.md), [the edit
tool](../../../docs/security/2026-09-24-edit-tool.md), [the
workspace](../../../docs/security/2026-09-27-the-workspace.md).

## WebFetch (`tools/webfetch`)

One approved `https` GET, returned as markdown. **`ActionOf` is `rebuild`**, so a later refusal or
`x/net` update changes no stored call's action. **`NewTransport` is the boundary**: its dialer
refuses any address `Dialing.Public` rejects, on the address dialled after DNS, the HTTPS proxy alone
excepted. `Run` follows redirects itself; one to another host or port goes back to the model.
Failures name their kind, never the error's text. → [the web fetch
tool](../../../docs/security/2026-09-24-web-fetch-tool.md).

## Web search (`tools/anthropicwebsearch`)

The Messages API's web search, a `Budgeted` tool the provider runs. `Prompt()` reads the month at
each call: the sidecar outlives a month.

## KubeQuery (`tools/kubequery`)

**Read-only SQL over the chat's cluster's cache, run without asking.** `parse` is strict (a `;` only
at the end). `Run` reads one `ReadActive` and the cache's health, and queries only when the verdict
answers: `health.Trusted()`, or **a `Connecting` cache once some kind has rows**; that answer reads
`last-known` or `partial` with reason `Connecting`. → [ADR: a Connecting cache answers from the kinds
that have listed](../../../docs/adr/2026-11-03-a-connecting-cache-answers-from-the-kinds-that-have-listed.md).
Errors pass through `safe.String`, and `read-failed` carries none of its text. Cells go through
`safe.RedactJSON`/`safe.Redact`. **No answer ends mid-row**: past `InlineLimit` it is saved, and a
head too large for any row is `too-large`. `prompts/kubequery.md` lists only the views that are
built. → [kubequery reads the mirror](../../../docs/security/2026-09-27-kubequery-reads-the-mirror.md),
[ADR: the cluster is kubectl in Bash, and KubeQuery over the mirror](../../../docs/adr/2026-09-27-the-cluster-is-kubectl-in-bash-and-kubequery.md),
[ADR: views on a connection of its own](../../../docs/adr/2026-09-27-kubequery-reads-views-on-a-connection-of-its-own.md),
[ADR: tables written with the object](../../../docs/adr/2026-09-27-kubequery-reads-tables-written-with-the-object.md),
[ADR: reads without asking](../../../docs/adr/2026-09-27-kubequery-reads-without-asking.md).

## The log tools

**`tools/internal/logsinput` is what LogsView, LogsRead and LogsWatch share.** `Parse` is the
strict walk (each key once); each tool passes its own `Fields` and says whether it needs sources.
`Resolve` checks each source inside one `ReadActive`, then runs `logs.Check`, so a scope a tool
takes is one the service reads. `MomentOf` reads an RFC 3339 time as the interval it spells (`Since`
its first nanosecond, `Until` its last; a date is UTC and takes no offset), or an ISO 8601 duration
applied on the calendar in UTC, a month end clamped to the target month's last day. A service
refusal is answered by its own code. Tests run over `logsinputtest` (`cluster/clustertest`
beneath), whose `Logs` is a real log service over a fake API server. The service is
`internal/services/logs/CLAUDE.md`.

- **LogsView is a live view of logs the user looks at, opened by the model's call.** It is `Shown`,
  never `Gated`, and reads no log line: it checks the call against the mirror, hands back a
  `LogsViewAction` the row keeps (its enums bind member by member in `gqlgen.yml`), and answers a one-line receipt through `safe.Redact`.
  `ResolvedAt` is the moment it resolved on the tool's clock, which a frozen card reads up to.
  **A source the mirror does not hold yet opens**: a view waits for it. A refusal is one JSON line
  with a code, never the model's text.
- **LogsRead is the model's read of a window of logs, a page at a time.** `Shown` and `Bounded`
  (30 s; the page's own deadline is 25 s, past which it answers `timed-out`), never `Gated`. A
  `cursor` call sends the token and the caps alone, since the token holds the scope as the read
  that printed it resolved it. **Every page is asked with `ForModel`**, so the service redacts each
  message before it cuts it; a page token is printed whole after the redaction. Its action holds the
  window as it ran, the lines the model read, and `viewAround`, the scope's view at its first line's
  moment, unpinned. → [ADR: a log read is a window over a range](../../../docs/adr/2026-10-23-a-log-read-is-a-window-over-a-range.md),
  [the model reads logs](../../../docs/security/2026-10-17-the-model-reads-logs.md).
- **LogsWatch is a follow of a scope that runs as a background task and wakes the model once, when
  it ends.** `Shown` and `Bounded` (30 s), never `Gated`. A watch starts now, at `since`, or from a
  read's `next` or `follow` token. The follow opens with `ForModel` under a context whose deadline
  is `until` (a day at most), free of the call's once open. The watch holds each line as the service
  cut it until `limit` or `max_bytes` and reads an `Ended` frame as why it ended; its header prints
  no cursor. → [the model watches logs](../../../docs/security/2026-10-18-the-model-watches-logs.md).

## Bindings

**A binding is a `Tool` over a capability.** The capability runs something and knows no contract;
the binding parses its contract's input into the capability's request and renders the outcome in
the contract's shape. Two bindings of one capability share its code by construction. No capability
ships yet.

## Bash (`tools/bash`)

**A turn can run a command, once the user says so.** `app.go`'s `chatTools` is the one
`tools.NewBox`; it offers Bash wherever `bash.New` finds a shell. `sandboxStateOf` builds the one
`sandbox.State`: `NetworkAvailable` is true wherever the sandbox is on macOS and only with a `pasta`
that passed the probe on Linux, and a sandbox with no shell clears the network answer with it
(`TestNoShellOffersNoNetwork`). Tests that want bash whatever the machine's login shell clear
`SHELL`. `make generate-desktop-prompts` pins the tool's words (`bash/prompts/`). A tool with no
sandbox offers no `network`: `Definition` drops it, and `parse` still reads it. Bash's per-call bound
is its own timeout plus `killGrace` plus `callMargin`, so a timed-out command answers with its own
result. → [the bash tool](../../../docs/security/2026-09-18-bash-tool.md), [bash offered to every
turn](../../../docs/security/2026-09-22-bash-offered-to-every-turn.md).

- **The shell**: on Unix the login shell if `zsh` or `bash`, else bash on `PATH`; on Windows Git for
  Windows' from its install record, never `PATH` (WSL's launcher). Read the version with
  `--version`, never `-c` (sources `BASH_ENV`).
- **Sandboxed = the tool has a sandbox and `Session.Outside` is false**; the model has no say. A
  sandboxed call on a sandbox that `Confines` runs unasked except for network (`networkFor`). →
  [ADR](../../../docs/adr/2026-09-28-the-sandbox-is-the-gate-for-a-sandboxed-command.md),
  [network is the user's switch](../../../docs/security/2026-10-04-network-is-the-users-switch.md).
- **Input goes through one token-walking `parse`** for `Approval`, `Run`, `CallTimeout` and
  `ActionOf`; never a struct decode, which lets case variants and duplicates through. → [the request
  reads the arguments](../../../docs/security/2026-09-23-the-request-reads-the-arguments.md).
- **The schema's `description`** names what the command changes and never calls it safe. → [the
  command description](../../../docs/security/2026-09-23-the-command-description.md).

**A sandboxed run carries its own directory, environment and kubeconfig** (`sandboxedRunFor`). →
[Bash runs in a sandbox](../../../docs/security/2026-09-28-bash-runs-in-a-sandbox.md).

- Its directory (`<runtime>/runs/<pid>-*`) holds `kubeconfig` and `px`, the forwarder's socket,
  outside the workspace; a socket path must fit (`checkSocketPath`). `TMPDIR` is
  `<cache>/tmp/<pid>-*`, on macOS seeded with xcrun's cache →
  [record](../../../docs/security/2026-09-30-a-run-starts-with-xcruns-cache.md). A gone sidecar's
  entries are swept by its lock → [ADR](../../../docs/adr/2026-10-17-a-run-directory-is-reclaimed-by-its-sidecars-lock.md).
- **`PATH` is read once at start** (`readPath`): one `RunPath` gives its search and grant check,
  each folder checked by origin (`settings.RunCheck`); a failed read starts no run. With no folder it
  is `/nonexistent`, since an empty one searches `.`. →
  [ADR](../../../docs/adr/2026-11-24-the-sandbox-path-is-one-setting-the-user-writes.md).
- **The environment is one table** (`env.go`); nothing else of the process's passes. `ZDOTDIR` is the
  run directory, so `zsh -c` never sources a workspace `.zshenv`. The user's variables (`userEnv`,
  read before the run makes anything; a failed read fails it) follow `LANG` and `TZ`. A name the
  table sets joins `sandbox.KubetailSets`, so Settings refuses it: else a user's value would replace
  the table's before it, or be silently replaced after. A mount's test checks its variables with
  `assertNoUserCanSet`.
- **A command outside the sandbox gets the variables its request showed**: `Approval` reads them
  into `tools.Approval.Env`, the row keeps them, and `RunApproved` takes them from the approval,
  never `userEnv` again. → [the user's variables](../../../docs/security/2026-11-20-the-users-variables.md).
- **Whatever can hang on a network mount is built on a goroutine abandoned when the context ends.**
- The cluster's kubectl cache is keyed by `serverKey(uid)`; the kubeconfig names a fixed server →
  [ADR](../../../docs/adr/2026-10-17-a-sandboxed-kubeconfig-names-a-fixed-server.md).
- Folder grants are rechecked at the run (`grantRules`), and a read of them that fails fails the
  run → [path grants](../../../docs/security/2026-10-04-path-grants.md).
- Tests lay folders out under Kubetail's three as `app/paths.go` does (`kubetailDirs`). `onRunDir` is
  a test's hook on a run's fresh directory.

**A run with a cluster or a cloud platform serves its door on its socket** (`proxy.go`): one server
routing by transport and `Host` to the cluster's grant and each cloud platform's, anything else the
cluster grant's 401. The door authenticates nothing and refuses a cloud platform's upgrade; a cloud
platform's grant's `Admit` checks the token, then the switch: off is `401`. The claim watches the
record and revokes on disable, mark or delete. A claim that cannot be made still runs the command,
answering each request not connectable. A background command's writes are refused. **The run ends
in order** (`sandboxedRun.end`): every grant, server, handlers joined, claim, directory, so no
request reaches the user once `Run` returns. → [cluster writes
ask](../../../docs/security/2026-09-29-cluster-writes-ask.md), [the permissions
engine](../../../docs/security/2026-10-02-the-permissions-engine.md).

**A cloud platform joins a run through `CloudPlatform`** (`cloud.go`), a list `app` hands `bash.New`
beside their switches (`app/cloudplatforms_unix.go`), and none on a machine with no sandbox. A run of
a chat or a subagent mounts each cloud platform whose switch is on at its start (none if
unreadable); a monitor's and the probe's mount none. Two routes on one host, two values for one
variable, or a failed mount fail the run and end what was made; a proxy variable brings
`NO_PROXY=127.0.0.1`. **A mount's rules**: every value comes from the mount, never the sidecar's
environment; its grant takes `cloudLimits`; a file it writes goes through `writeNew` (0600,
`O_EXCL`) under the run's directory, the tool home or `TMPDIR`, and holds a token or a placeholder,
never a credential; a credential lives in a service under `services/`, resolved no earlier than a
mount with the switch on, and held under `safe.AddSecret` until `safe.ForgetSecret`. →
[ADR](../../../docs/adr/2026-11-17-a-sandboxed-command-reaches-a-cloud-platform-through-a-proxy-on-its-runs-one-relay.md),
[the run's door](../../../docs/security/2026-11-17-the-runs-door.md).

- **AWS** (`aws.go`): one route at the loopback authority, `AWS_` placeholders and
  `<run>/aws/config`, reaching AWS as the profile `CloudRun.KubeContext` names, one login and bucket
  list per profile.
- **GitHub** (`github.go`) writes `gitconfig` (named through `GIT_CONFIG_COUNT` and `include.path`),
  `gh/hosts.yml` (a placeholder login) and `gh/config.yml` (stamped, so no gh migrates them) under the
  run's directory, and sets `GH_HOST=github.localhost` and `HTTP_PROXY`.
- **Google Cloud** (`gcp.go`): its mount waits 5 s for the project.
- **Azure** (`azure.go`) mounts a TLS route at the loopback authority and, with an account, `az`'s
  config under `TMPDIR/azure`, signing each subscription in as a managed identity whose endpoint is
  the door. A mount waits `azureMountWait` for the login, then mounts with none. →
  [ADR](../../../docs/adr/2026-11-17-a-sandboxed-command-reaches-azure-through-a-token-injecting-proxy.md).
- **A `Route` with `TLS` is served over the door's TLS** on the same listener: the run gets a
  `runproxy.Certificate` and its bundle at `CloudRun.CAFile`.

**The executable probe** (`executableprobe.go`) runs each executable in the sandbox, **never through
a shell**, with no cluster and no network. It checks each registered one itself: one
`CheckExecutable` refuses never runs. **Every disk read outside Kubetail's directories is bounded**
(`onDisk`), so a dead mount fails one probe and holds neither the queue nor `Close`. One probe runs
at once. Its report reaches the user alone, never a model: `sandboxExecutablesProbe` starts one and
answers at once, since a probe can outlast the host's request budget, and
`sandboxExecutablesStateWatch` is what its end reaches.

**The Sandbox console** (`console.go`) runs one typed command at a time as a chat's sandboxed call,
in a `scratchDir`, as `session.Console`: the always folders, no asker (so no cluster change) and no
cloud platform. → [record](../../../docs/security/2026-11-21-the-sandbox-console.md).

**A command runs where its request shows** (`resolveWorkdir`): its `workdir`, else the workspace. A
sandboxed call must start under the workspace by name (guidance, not a boundary). **`resolveWorkdir`
never touches the filesystem**: a UNC path touched before the user decides starts an SMB login. →
[the command names its directory](../../../docs/security/2026-09-23-the-command-names-its-directory.md).

**The wrapper** (`wrapper.go`) sources the snapshot (outside the sandbox) and `eval`s the quoted
command. On Windows it is a file, never a command line, which MSYS would reparse. **The snapshot**
(`snapshot.go`) is the user's profile, dumped once per start, for commands outside the sandbox alone;
its stop waits for the shell's reap. → [bash runs the login
shell](../../../docs/security/2026-09-23-bash-runs-the-login-shell.md).

**A stop is the runner's record** (`result.Stop`), never read off the exit code, and **a stopped
command is always an error result**. A cancel kills at once. →
[ADR](../../../docs/adr/2026-10-17-a-bash-stop-is-the-runners-record.md).

**An error result's first line is the sidecar's** (`Exit code N`, `Command timed out …`) and the
output starts on the next, so nothing a command prints can pass for a header. The output is redacted
by `safe.Redact`: **hygiene, never the boundary**; the approval request is the bound. A confined run
that failed ends with `sandboxLine`, so the model knows the sandbox may be why.

**A background command** (`run_in_background`) is approved like any other. `startTask` is bounded by
the call's context only until the process starts, then clears the `exec.Cmd`'s `Cancel`; the chat
holds the task from there. `TaskStop` (`tools/taskstop`) is the model's stop.
