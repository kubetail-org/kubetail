# Security record — the Sandbox console, 21 November 2026

**Subject:** the Sandbox console in Settings › Sandbox, which runs a command the user types
(`sandboxConsoleRun`, `sandboxConsoleStop`, `sandboxConsoleWatch`; `sidecar/internal/tools/bash/console.go`).
The living model is [security-model.md](../security-model.md).

## What it changes

A command now runs with no approval and no chat: the user types it, picks a cluster or none and
ticks Network or not, and the sidecar runs it as a chat's sandboxed command runs. Its output streams
back to the console alone. This is a new way to run a command that no approval gates, so it is
recorded here. It rests on the decision that [the sandbox is the
gate](../adr/2026-09-28-the-sandbox-is-the-gate-for-a-sandboxed-command.md) for a command it
confines.

## What holds

- **It never runs outside the sandbox.** A machine whose sandbox does not confine a command, or
  that has none (Windows), is refused before anything is made (`TestTheConsoleRunsNothingOutsideTheSandbox`,
  `TestSandboxConsoleWithNoShell`). Under the real sandbox a read of a file the policy does not open
  fails and its output shows the refusal (`TestTheSandboxRefusesAConsoleCommandsRead`).
- **It runs under a chat's policy, narrowed.** The run is built by `sandboxedRunFor`, as a chat's
  is: the same files, environment, `PATH`, limits and bound (`consoleTimeout`, Bash's default
  120 s). Its own workspace, under the cache's tmp directory, is removed when it ends
  (`TestEachConsoleCommandHasAWorkspaceOfItsOwn`). It reads the folders granted always and no
  chat's (`TestAConsoleCommandReadsTheAlwaysFolders`).
- **It changes no cluster and reaches no cloud platform.** Nobody is there to ask, so its grant has
  no asker: reads pass, every write is refused with `kubetail: this sandbox reads the cluster and
  changes nothing.`, and a Secret's data stays redacted (`TestAConsoleCommandChangesNoCluster`).
  The session is `session.Console`, which `platformsFor` mounts no cloud platform for.
- **Network is the user's tick alone**, and refused where the machine cannot give it
  (`TestTheConsoleRefusesWhatItCannotRun`, `TestAConsoleCommandWithNetworkHasTheInternet`).
- **One command at a time.** A second is refused `KUBETAIL_CONFLICT` while one runs
  (`TestARunningConsoleCommandStreamsAndStops`, `TestSandboxConsoleRunRefusals`). Closing the
  console, `sandboxConsoleStop` and the sidecar's stop end it (`TestCloseEndsARunningConsoleCommand`).
- **The command is bounded text**: not blank, at most 16 KiB, valid UTF-8, no control character
  but a tab or a newline (`TestCheckCommand`).
- **Output is attacker-controlled text.** The sidecar keeps at most 256 KiB and redacts it with
  `safe.Redact` (hygiene, not a boundary). Redaction matches a secret only once its line is whole,
  so a running command's last line, and one the cap cut, are held back (`TestPublishSendsWholeLines`,
  `TestConsoleOutputIsRedactedAndCapped`); the webview
  strips control sequences and draws it, the command and the notice through `VisibleText`
  (`draws each run: its command, its output stripped and spelled, and how it ended`).
- **Nothing reaches a chat or a model.** No row is written; the gauge holds the last command in
  memory alone.

## Residual risk

Anything running in the webview already holds the full surface, and now can also run a confined
command with network without a click, as it could already approve one. A command the console runs
leaves no record: no chat row and no journal of what its proxy decided. A cluster read reaches the
cluster as the user, with the user's credentials, as a chat's does.
