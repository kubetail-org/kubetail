---
title: A sandboxed command's PATH is one setting the user writes
date: 2026-11-24
scope: sidecar, webview
status: Accepted
---

# A sandboxed command's PATH is one setting the user writes

## Context

A sandboxed command's `PATH` was the login shell's, filtered and frozen: a stored list of every
folder the shell listed, each `adopted`, `pending` or `gone`, kept up to date by a sync at launch
and on Refresh PATH. A folder is readable in a run only when it is searched, so the list was also
a set of read grants. The sync existed to stop a startup file from making a folder readable
unseen.

Most of that state protected nothing. A shell folder was adopted unasked only when every run could
already read it, so the list carried two real decisions: Include of a closed folder, and Remove.
Around them grew a diff, removals kept as tombstones, a stored `Target` compared as text, a
`strict` flag that survived a restart, and the rules for an entry the store could not read. Moving
the settings into `app.db` would have moved all of it into rows.

## Decision

The `PATH` is one string, the `sandbox.path` row of `settings`, written as in a shell's startup
file: folders joined by `:`, where `$PATH` stands for the login shell's `PATH`, and `~` or
`$HOME` for the home. With no row it is `$PATH`.

- **Split first, then expand.** `parsePath` splits the value before anything is expanded. Each
  element is exactly `$PATH` (or `${PATH}`), or a folder the user typed. The shell's answer is
  spliced in already split and never parsed, so no folder the shell lists is ever a typed one.
  `$PATH` mixed with text, any other variable, a relative path, `..`, the bare home and an empty
  element refuse the whole value.
- **Two origins, two rules** (`RunCheck.Folders`). A shell folder is searched only when every run
  reads it already and no group but an administrators' one can write it: a startup file can
  reorder open folders, never open one. A typed folder is the user's read grant, held to a folder
  grant's checks: a link or another spelling is refused naming the folder to type instead, and the
  denied-always list, Kubetail's directories and the closed folders stay shut.
- **No read-write grant reaches a `PATH` folder**: every folder of the shell's, whatever the value
  says, since the user's terminal still runs what they hold, and every typed one.
- **The shell's last `PATH` is a copy** in `shell_path`, written by the launch's one run of the
  shell and by Refresh PATH. A read that fails, or answers an empty `PATH`, keeps the last one.
  With none, `$PATH` is the platform's default folders. Every folder of it is checked again at
  each run, so a stale copy grants nothing.
- **A run reads its `PATH` once**, one read transaction answering both what it searches and its
  `NoWrite` folders.

Settings shows the value over each folder, searched or not and why, with an Add that appends a
closed shell folder's target to the field before Save.

This supersedes `PATH` is the login shell's, filtered and
frozen and a stored `PATH` decision
holds for its exact path, and
replaces the paragraph of [two mounts of one folder are one `PATH`
folder](2026-11-23-two-mounts-of-one-folder-are-one-path-folder.md) on stored decisions, since none
is stored. Its duplicate rule stands: a folder already searched is not searched again, compared as
files.

## Alternatives considered

- **Keep the sync, with rows.** It moves every rule the frozen list had into `app.db`, and the
  rules for a row Kubetail cannot read on top. The two decisions it protects fit in a string.
- **The shell's `PATH` plus custom folders, prepended or appended.** No Remove, and an included
  folder loses its place in the shell's order. One string written as a shell writes it gives both
  back, and needs no schema of its own.
- **Expand, then split.** Simpler code, but it loses each folder's origin, and with it the one
  rule that keeps a startup file from opening a folder.
- **Keep the shell's `PATH` in memory alone.** A launch whose read fails would search the
  platform's default folders for the whole session.

## Consequences

- An install of `desktop/v0.0.1` loses its `PATH` decisions: an included folder the sandbox does
  not already read must be typed, and a removed one that is open is searched again.
- **Removing one folder of the shell's `PATH` is gone.** The only way is a value without `$PATH`
  that lists the folders it keeps, which stops following the shell. An open folder stays readable
  either way, so Remove only chose which program a name finds; a typed folder before `$PATH` does
  that.
- `sandboxPathSet` is a grant path: typed text gives every run read access to a folder tree.
- A folder whose path holds `$` cannot be typed: the grammar has no escape.
- A closed folder itself can no longer be put on the `PATH`, as it cannot be granted; a folder
  inside one can.

## Revisit when

The settings sync across machines: a typed folder names one machine's disk.
