# Security record — the command names its directory, 23 September 2026

**Subject:** a bash call can say where its command runs, with an optional `workdir`, and the
approval request shows the directory under the command. Nothing carries between calls. This
builds on [the bash tool](2026-09-18-bash-tool.md) and
[the command description](2026-09-23-the-command-description.md). The living model is
[security-model.md](../security-model.md).

## What is new

**The model chooses where a command starts.** Until now every command started in the user's
home. Now a call's `workdir` names the directory: absolute, `~`-prefixed, or relative to the
home. What a relative path in the command means depends on it, so the directory is part of what
the user approves.

It adds no reach. A command could always `cd` first, and an approved command still reaches what
the user can. A directory is a place, not a boundary: `..` may leave the home.

## The command runs where the request says

**One function computes both.** `Approval` and `Run` read the input through one `parse` and
resolve the directory through one `resolveDir`, so the directory shown is the directory the shell
starts in (`TestTheApprovalShowsTheDirectoryItRunsIn`, `TestACommandRunsInItsWorkdir`). The
result is cleaned, so `..` is resolved before the user sees it.

**Nothing carries between calls.** A `cd` ends with its command, so no earlier command changes
where the next one runs behind the request (`TestACdDoesNotCarry`).

**`pwd` agrees with the request.** On Unix the shell is given `PWD=<dir>`, so a directory reached
through a symlink reads as the request showed it (`TestACommandRunsInItsWorkdir`).

## Nothing touches the directory before the user decides

`resolveDir` is string work alone: no `Stat`, no `EvalSymlinks`. On Windows a UNC path that was
touched would start an SMB login and could send the user's NTLM hash to that host; on Unix an NFS
or autofs path would be mounted. The one check on the directory, that it exists, runs at `Run`,
after the approval, and a missing one starts nothing (`TestAMissingWorkdirIsNotStarted`).

**A dead mount cannot hold the turn at the check.** A stat on an unreachable network mount can
block in the kernel past any cancel, so the check runs on a goroutine of its own and `Run`
answers the turn's cancel or the call's deadline without it
(`TestAStuckDirectoryCheckHonoursTheCancel`). Starting the shell there cannot be cancelled, so a
mount that dies between the check and the start still holds the call until the filesystem
answers. That is the residual.

On Windows a path only Git Bash can place (`/tmp`), a rooted path with no drive (`\x`) and a
drive-relative path (`C:x`) are refused, so none is silently joined to the home
(`dir_windows_test.go`'s `TestResolveDir`, which the Linux CI does not run).

## The request draws it

**Under the command, never above it.** A muted `in`, then the directory in mono through
`VisibleText`, so an invisible or reordering character is spelled out. Only fixed text and the
one-line description come before the command, so a directory cannot push the command out of view
or pass for part of it. On a folded command the line follows *Show the rest*, and Approve still
waits on the rest (`draws the directory under the command, above the buttons`, `draws the
directory after Show the rest on a folded command`, `spells an invisible character in the
directory out`).

**Bounded at parse.** `parse` refuses a `workdir` that is empty, over 4,096 bytes, or holds a
control character, newline included, so the line is never folded or cut (`TestWorkdirIsParsed`).

## The record

The directory shown is stored on the approval (`approvals.dir`,
`TestTheApprovalRowHoldsTheDirectory`) and served as `ToolCallApproval.dir` on the live list and
the stored read. The settled disclosure opens with the same line.
