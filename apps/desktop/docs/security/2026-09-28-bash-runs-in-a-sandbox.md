# Security record — Bash runs in a sandbox, 28 September 2026

**Subject:** on macOS and Linux a Bash call the sandbox confines runs without asking the user, in
the foreground or in the background. A `Write` or `Edit` in the chat's workspace runs without
asking on every machine. A call with `dangerouslyDisableSandbox`, every Bash call on a machine with
no sandbox, and a write anywhere but the workspace still ask. This is step 8 of
sandboxed Bash: the step the sequence exists for. The living model is
[security-model.md](../security-model.md); the decisions are
[the sandbox is the gate for a sandboxed command](../adr/2026-09-28-the-sandbox-is-the-gate-for-a-sandboxed-command.md),
[helm's release Secrets pass, redacted inside](../adr/2026-09-28-helm-release-secrets-pass-redacted-inside.md) and
[a sandboxed forwarder holds a loopback port on macOS](../adr/2026-09-28-a-sandboxed-forwarder-holds-a-loopback-port-on-macos.md).

## What changed

Until now the bound on every command was the user reading it and pressing Approve
([the bash tool](2026-09-18-bash-tool.md)). For a sandboxed command the bound is now what the
process can reach, not what the command says.

- **Bash.** `Approval` skips a call whose sandbox `Confines` (`TestASandboxedCallAsksNoOne`,
  `TestASandboxedBackgroundCallAsksNoOne`). A call with the flag, a call through a sandbox that
  does not confine, and a call on a machine with no sandbox ask. `Confines` is true only for
  bubblewrap on Linux (`TestBwrapConfinesOnAFixedPort`) and Seatbelt on macOS
  (`TestAProbeThatRunsIsASandbox`); Windows has none (`TestWindowsHasNoSandbox`).
- **Write and Edit.** `Approval` skips a path under the workspace by name, the test `Fence.File`
  opens the workspace's root by, so every unasked write goes through the root and a link cannot
  carry it out (`TestAWriteInTheWorkspaceAsksNoOne`, `TestAWriteElsewhereStillAsks`,
  `TestAWriteThatSkipsGoesThroughTheRoot`, in both tools).
- **The request.** A command with the flag reads *Run this command outside the sandbox?*, or
  *Run this command in the background, outside the sandbox?* (`says a command outside the sandbox
  runs outside it`). A machine with no sandbox never sets the flag and keeps the old headings.
- **The transcript.** An unasked `Write` that succeeded draws its content open under its summary,
  and an `Edit` its two strings, each folded past 24 lines or 2,000 characters (`draws its content
  open, folded, and not again inside the disclosure`, `draws an unasked edit open, and not again
  inside the disclosure`). A command that later names the file names text the user has had on
  screen.

## The bound

A sandboxed command reaches no network, no credential, and no file outside the workspace, the
kubectl cache, its `TMPDIR` and the system (`sandbox_linux_test.go`, `sandbox_darwin_test.go`). It
reaches the chat's cluster read-only through the proxy, with Secret values redacted. A change to the
cluster, `exec`, `attach` and `port-forward` come back `Forbidden` and never reach the server.

End to end, against a fake API server, with the real sandbox, `kubectl` and `jq`:

- `kubectl get pods -o json | jq '.items | length'` answers with no approval and a row that reads
  `sandboxed` (`TestASandboxedReadRunsUnasked`), and so does a subagent's
  (`TestASubagentsSandboxedCallRunsUnasked`);
- `kubectl delete pod x` answers the proxy's `Forbidden` with no approval, and the server sees no
  `DELETE` (`TestASandboxedChangeIsForbidden`);
- the same delete with the flag waits for the user, and a denial runs nothing
  (`TestTheFlagAsks`).

CI runs these on Linux and macOS under `KUBETAIL_REQUIRE_SANDBOX=1`, with `kubectl` and `jq`
installed by `setup-environment`.

## Prompt injection

An instruction in cluster text can now make a command run, not only ask for one. What it runs
reads the chat's cluster, less Secret values, and writes the workspace. **The only way out is the
provider**, which already has the chat: an injection chooses which cluster data reaches it. It can
make no network call, read no credential, and change nothing in the cluster.

An unasked `Write` needs no sandbox: it reaches only the workspace. So on a machine with no
sandbox, an injection can write the workspace without asking, and everything that runs from there
still asks.

## An unconfirmed Seatbelt skips too

A macOS probe that runs out of time keeps the sandbox, its reason saying so
(`TestAProbeThatTimesOutKeepsTheSandbox`), and its calls now run unasked. That fails closed: a
profile that does not apply leaves the command unstarted, never unconfined.

## Residuals

- ConfigMap data, env values and logs go to the provider as they are. So do a helm release's
  chart, its default values, its rendered notes and its other objects' rendered manifests, where
  a value can be rendered into an env var.
- A Secret typed `helm.sh/release.v1` by hand passes as a release does, all but its `config` and
  its manifests' Secrets, since anyone who can create a Secret sets its `type`.
- A sandboxed command can write its cluster's kubectl cache, which later runs on that cluster read.
  Its reach stays that one cluster, whose text the attacker can already put in front of the model.
- **A file a sandboxed command wrote is not drawn.** A command outside the sandbox that later names
  it names text the user has not seen. The request shows the command, not the files it names.
- **An approved command can read a workspace file it does not name.** A command outside the
  sandbox starts in the workspace, and tools read config from their working directory. A
  sandboxed command, or an unasked `Write`, can leave `.git/config` with `core.fsmonitor` set; the
  user then approves `git status`, and the planted command runs with the user's files,
  credentials and network. The same holds for any tool that reads config or runs hooks from its
  working directory, and for a `PATH` holding `.`. It holds on every machine, since the unasked
  `Write` needs no sandbox. Accepted for now: a **By decision** row, and closing it is in
  [`TODO.md`](../TODO.md#security).
- A link a command outside the sandbox puts in a `PATH` entry (`ln -s ~/notes/x ~/.local/bin/`)
  opens its directory to every later sandboxed run, unasked, since the trees are read at each run.
- On macOS, a process a sandboxed command spawns out of its group through `posix_spawn` outlives
  the run, confined, and can reach whatever later listens on the run's forwarder port
  ([ADR](../adr/2026-09-28-a-macos-run-keeps-its-group-by-refusing-setsid.md)).
- A sandbox escape, through a kernel, bubblewrap or Seatbelt bug, is outside the guarantee.
- System files the user can read are readable, and on macOS so are app bundles and Homebrew's
  `etc`.
- The limiter bounds a background loop's load on the API server, not how long it runs.
