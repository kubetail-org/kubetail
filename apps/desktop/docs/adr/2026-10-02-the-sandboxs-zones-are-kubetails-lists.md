---
title: The sandbox's zones are Kubetail's lists, and its environment one table
date: 2026-10-02
scope: sidecar
status: Accepted
---

# The sandbox's zones are Kubetail's lists, and its environment one table

## Context

A sandboxed run took three things from the user's shell. `PATH` decided what it read: every entry
under the home opened a tree, every entry outside the lists opened its folder, and the links on
the way were recreated. The locale passed whole, `LC_*` included. And every sandboxed command
sourced the shell snapshot, so the user's functions and aliases ran inside the sandbox, and every
sandboxed run waited for it. The snapshot's last line set `PATH`, so a startup file chose what the
sandbox read.

Tools that write under `$HOME` wrote into the workspace, since `HOME` is the workspace. Step 4D
will grant folders, so a broad grant must not reach another user's home or the user's documents
by accident.

## Decision

- **The zones are lists** (`sandbox.Lists`): `System`, read by default; `Toolchain`, the folders
  under the home that hold the user's tools, each read where it exists, with the variables that
  point its tool at it; `Never`, denied always, with `/root` and the other users' homes read at
  call time; and `Closed`, the user's documents folders, denied as a Files Deny so a grant inside
  one opens what it names. A `PATH` entry opens nothing.
- **The environment is one table** (`sandboxedRunEnv`), which copies `PATH`, `LANG` and `TZ` and
  builds the rest. `sandbox.NeverEnv` names what may never pass, and `Run.check` refuses it in
  both platforms' `Command`.
- **The tool home is per chat** (`tools.ToolHomePath`), and only the kubectl cache is per cluster.
- **A sandboxed run sources no snapshot.** On a machine with a sandbox the snapshot is taken on
  first need, once, under a lock the lifecycle stop takes before it drains the shell.

## Alternatives considered

- **Keep `PATH` choosing the zones.** A startup file can put anything on `PATH`, so the user's
  shell would widen the sandbox unseen.
- **Put the documents folders on `Never`.** Then step 4D could never grant a project under
  `~/Documents`, where many macOS users keep code. A Files Deny keeps them shut under a grant of
  `~` and opens a folder granted inside one.
- **Glob the other homes.** A policy has no glob. Listing `/home` or `/Users` each run also sees
  a user added since the start.
- **A tool home per cluster**, to save a download for helm's indexes. A shared config or build
  cache lets one chat plant what a later chat runs, and those writes were per chat already.
- **Keep sourcing the snapshot in the sandbox.** The user's functions run there only if the
  sandbox runs their startup files, which this sequence takes away (step 4A).

## Consequences

- An entry no list covers finds nothing until step 3A freezes the login shell's `PATH` and opens
  each entry's own folder. That step reaches users in the same release.
- Adding a toolchain location is a security change: its folders become readable on every run.
- On a machine where every chat stays sandboxed, the user's startup files never run.

## Revisit when

Step 3A lands, which decides which `PATH` entries the sandbox gets, and step 4D, which grants
folders over `Closed` and `Never`.
