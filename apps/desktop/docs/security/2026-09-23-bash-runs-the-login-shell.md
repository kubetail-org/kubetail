# Security record — bash runs the login shell, 23 September 2026

**Subject:** a command now runs in the user's own shell, after a snapshot of their login profile.
This builds on [the bash tool](2026-09-18-bash-tool.md),
[bash offered to every turn](2026-09-22-bash-offered-to-every-turn.md) and
[the command description](2026-09-23-the-command-description.md). The living model is
[security-model.md](../security-model.md).

## What is new

**The shell is the user's.** On Unix it is `$SHELL` when that names zsh or bash by an absolute
path to an executable, and bash on `PATH` otherwise (`TestTheShellIsTheLoginShellWhenItIsZshOrBash`).
Windows is Git Bash, as before.

**A command runs after the user's profile.** After READY the sidecar runs the login shell once,
`-l -i`, and writes what it built to `<dataDir>/shell/snapshot.sh`: its functions, its options,
its regular aliases and its `PATH` (`TestTheSnapshotHoldsTheProfile`). Every command sources it,
then runs through `eval` with stdin closed. So a command's meaning can follow the user's own
definitions: an `ls` aliased to something else runs as that. An alias such as `rm -i` meets EOF on
stdin and answers no.

**The login shell runs after every start, on every platform**, whether or not chat is ever used.
Until now only macOS ran it. On macOS it now runs twice: `importShellEnv` before READY, and the
snapshot after it. The snapshot is a lifecycle part whose stop kills and reaps the shell, so a
profile stuck in its rc files never outlives the sidecar (`TestStoppingTheSnapshotReapsTheShell`).
A test's app never takes one (`TestAppTakesTheSnapshotOnlyWhenAsked`).

**`zsh -c` sources `.zshenv` on every call**, before the snapshot. That is zsh's own rule, and
the user's terminal does the same.

**`PATH` is the login shell's**, frozen at the snapshot, on Linux and Windows as well as macOS.
A credential plugin a command reaches now resolves through that `PATH`, which the S-2 item in
[TODO](../TODO.md#security) names.

## The bound is unchanged

**The approved text is the command.** The wrapper is the sidecar's, the same for every call, and
the command is quoted into it whole (`TestQuoteRoundTrips`,
`TestTheApprovalAndRunReadTheInputTheSameWay`). The approval request shows the command alone,
as before.

**The snapshot keeps what would change a command beyond the user's own definitions out.**
- Global and suffix aliases are left out. They expand anywhere in a line, so they would change
  parts of the approved text that do not look like a command.
- The options that say how the snapshot's own shell was started are left out. `monitor` above
  all: replayed, it moves a command's background jobs out of its process group, out of reach of
  the group kill (`TestTheSnapshotLeavesOutStartupOptionsAndGlobalAliases`,
  `TestRunKillsWhatACommandLeftBehindUnderZsh`).
- The dump itself cannot be steered by the profile's names: alias expansion is off while it
  runs, and every command in it goes through `builtin` (`TestTheDumpIgnoresTheProfilesNames`).
- Only `PATH` is exported. A provider key the user's rc files export does not reach a command.

**A failed snapshot is not a failed tool.** A profile that hangs or prints past 4 MiB costs the
snapshot, and commands run without it (`TestAFailedSnapshotOffersTheToolAnyway`).

## The snapshot's place

`<dataDir>/shell/snapshot.sh`, mode 0400 in a 0700 directory, and read-only on Windows
(`TestTheSnapshotIsReadOnly`). It is rewritten at every start and never read from a previous
one. **A command the user approves can still rewrite it**, and that changes every later command
in the process until the next start. The approval request shows such a command like any other.

## The kill shims

On Unix the snapshot ends with `kill` and `pkill` functions that refuse the sidecar's and the
host's processes (`KUBETAIL_SIDECAR_PID`, `KUBETAIL_DESKTOP_PID`), whatever the profile aliased them
to (`TestTheKillShimsRefuseKubetail`). **They are a courtesy, not a boundary**: `/bin/kill` passes
them, and so does `kill -9 -1`. Windows has none: there `KUBETAIL_SIDECAR_PID` is a Windows pid,
Git Bash's `kill` takes MSYS pids, and `taskkill` passes around any shim.

## The environment

A command's environment gains `KUBETAIL=1`, `KUBETAIL_SIDECAR_PID` and, when the host passed its
pid, `KUBETAIL_DESKTOP_PID` (`TestACommandSeesKubetailsOwnVariables`).
