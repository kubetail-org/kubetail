# Security record — the login shell in the sandbox, 4 October 2026

**Subject:** the three runs of the user's login shell Kubetail starts itself — the `PATH`
resolution at launch, Refresh PATH, and the shell snapshot — run in the OS sandbox. The living
model is [security-model.md](../security-model.md); the decision is
[the login shell runs in the sandbox](../adr/2026-10-04-the-login-shell-runs-in-the-sandbox.md).

## The problem

Running the login shell runs the user's startup files: `~/.zshrc`, `~/.bashrc`, `config.fish`.
All three runs ran them with full access. A startup file changed to do harm — a compromised
plugin manager's update check, a line that reads `app.db` and posts it — did it with Kubetail's
own files and the network in reach, every launch.

## What changed

- **`loginshell.Launch` takes a `Start`**, which builds the command. `loginshell.In(sb, deny,
  kubetailDirs, tmp)` is the one place that builds the login shell's policy: a Read of `/` and
  the executable, Kubetail's data, cache and runtime directories closed, `deny` as the
  denied-always list, one write — the `TMPDIR` made for the run, removed when the shell exits —
  and no network.
- **What the shell reads follows where its output goes.** The resolution and the refresh pass
  `sb.Never(home)`: their `PATH` reaches every sandboxed run, and on macOS the imported
  variables reach the sidecar's own process. The snapshot passes none: its output reaches only
  commands outside the sandbox, which read the home themselves.
- **The environment** loses every variable `sandbox.Unpassable` names, `SSH_AUTH_SOCK`
  included, and `TMPDIR` is the scratch folder.
- **The app probes the sandbox once** and runs the launch resolution in it, before anything
  that reads the environment, so the probe is not run twice. On Linux with no sandbox the
  launch resolution is skipped, since nothing reads it there.
- **A refused start fails closed.** The resolution and the snapshot fail as on any other
  `Fault` (`sandbox refused`, `no scratch`, or `timeout` for a start cut short), and the shell
  never runs unconfined on a machine whose sandbox answered the probe.
- **The `TMPDIR` is a sandboxed run's** (`bash.TempDir`): a folder under `<cache>/tmp/` taken
  under the sidecar's lock, so the sweep that removes a gone sidecar's runs removes it too and
  never takes a live one's.

## Why this is safe

- **The shell reads nothing of Kubetail's** (`TestTheLoginShellCannotReadKubetailsDirectories`)
  and, for the resolution, nothing on the denied-always list
  (`TestTheResolutionCannotReadTheNeverList`), so the run whose output leaves the sandbox has no
  credential of the list's to put in it.
- **It writes only its scratch** (`TestTheLoginShellWritesItsScratch`,
  `TestTheLoginShellCannotWriteTheHome`), and the scratch goes with the run
  (`TestTheLoginShellsScratchIsGone`, `TestTheLoginShellsTempDirIsARuns`).
- **It reaches no network** (`TestTheLoginShellHasNoNetwork`), and on macOS no trust daemon
  (`TestTheLoginShellLooksUpNoTrustDaemon`).
- **The kill still reaches what it started** (`TestLaunchSetsTheSession`). On Linux bwrap's
  PID namespace ends with the run; on macOS a process started into a new session through
  `posix_spawn` can outlive the kill, confined, with nothing to write once its scratch is gone.
- **A refused policy runs nothing** (`TestAPolicyTheSandboxRefusesIsAFault`,
  `TestAScratchThatCannotBeMadeIsAFault`, `TestTheLaunchResolutionRunsInTheSandbox`), and a
  start cut short is a timeout, not a refusal (`TestAStartCutShortIsATimeout`).
- **The snapshot reads the whole home on purpose** (`TestTheSnapshotsShellReadsTheHome`,
  `TestTheSnapshotIsTakenInTheSandbox`). It is the one run Kubetail starts that reads `~/.ssh` and
  `~/.kube`. What bounds it is that it writes nothing outside its scratch, reaches nothing, and
  that its answer is sourced only by commands outside the sandbox, which read the home anyway.

## What stays

Two residuals, each a **By decision** row in the model:

- **A command outside the sandbox runs the `-c` startup files.** It is `<shell> -c` with the
  process environment, so zsh reads `/etc/zshenv` and `~/.zshenv`, and bash the file
  `BASH_ENV` names, with full access, once per command. That command asks, every one, and
  running the user's shell as they set it up is what the chat's switch is for.
- **What the login shell outputs shapes what runs outside the sandbox.** The snapshot's
  functions, aliases and `PATH` run in every command outside the sandbox, and on macOS the
  imported variables (`HTTPS_PROXY` among them) and the imported `PATH` shape the sidecar's own
  `exec` credential plugins. A startup file can use either to postpone its harm. On macOS the
  import can also carry out a secret the resolution's shell read from a file the denied-always
  list does not name (`~/.pgpass`), in `HTTPS_PROXY` or `OLLAMA_HOST`: the list's curated-list
  residual.

A command can change a startup file only where it can write it: outside the sandbox, by the
chat's switch, or, once folder grants land, through a read-write grant of a folder holding a
file a startup file sources.

With no sandbox on the machine, the snapshot and, on macOS, the launch resolution run
unconfined as before, and the sidecar logs once for each that it did.
