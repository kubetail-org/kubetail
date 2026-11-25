---
title: Keep each file in the platform's directory for its kind
date: 2026-09-27
scope: cross-cutting
status: Accepted
amended_by:
  - [A shipped schema changes only by a new migration](2026-11-08-a-shipped-schema-changes-only-by-a-new-migration.md)
  - [Linux folders follow XDG](2026-11-21-linux-folders-follow-xdg.md)
  - [Each folder kind has its own folder on every platform](2026-11-22-each-folder-kind-has-its-own-folder-on-every-platform.md)
  - [The sidecar places Kubetail's folders itself](2026-11-23-the-sidecar-places-kubetails-folders-itself.md)
---

# Keep each file in the platform's directory for its kind

## Context

Every file the sidecar kept lived under one data directory: `app.db`, `beehive.db`, the settings,
the kubestore mirror (`caches/`), the shell snapshot (`shell/`), and `clusters/<id>/`, which held
both the cluster's kubectl cache and its chats' directories. A sandboxed run's own directory, with
its kubeconfig, its socket and its `TMPDIR`, went under `os.TempDir()` instead, because a socket
path has to fit `sun_path` and a data directory's can run past it.

That mixed three kinds of file. Some a user would lose: the chats and the database. Some Kubetail
rebuilds: the mirror and the kubectl cache. Some live for a session: sockets and the snapshot.
Backups copied gigabytes of mirror. On Linux a run's `TMPDIR` sat on the tmpfs the session keeps
its sockets on, and a run's directory sat in the shared `/tmp`. Each package also joined its own
paths from the data directory, so no one place listed what the sidecar keeps. The Linux (bubblewrap) and
macOS (Seatbelt) sandboxes need that list to deny Kubetail's directories to a command.

## Decision

The host resolves three directories and passes each to the sidecar as a flag. Each is required
and absolute:

- `--data-dir`: `local_data_dir/Kubetail`.
- `--cache-dir`: `cache_dir/Kubetail`, or `<data>/cache` on Windows, where the cache root is the data
  directory's own parent.
- `--runtime-dir`: `$XDG_RUNTIME_DIR/kubetail`, else `<temp>/kubetail-<uid>` (or a fresh
  `mkdtemp` directory beside it when another user has claimed that name), or `<data>/run` on
  Windows, which has no per-user runtime directory.

`app/paths.go` names every path under them in one place (`pathsOf`). Each service is handed its own
paths and names nothing else. Each subtree has one owner, which makes it, sweeps it and removes it:

- services/chat owns `<data>/chats/<chat id>`. A chat's directory no longer names its cluster.
- services/cluster owns `<cache>/kubestore`.
- bash owns `<runtime>/shell`, `<runtime>/runs/<pid>-*` (a run's kubeconfig, socket and `ZDOTDIR`),
  `<cache>/tmp/<pid>-*` (a run's `TMPDIR`, on disk) and `<cache>/kubectl/<cluster id>/<server>`
  (the kubectl cache a sandboxed run's `KUBECACHEDIR` names). A sandboxed run is its one user, so
  bash reads the record from services/cluster and keeps the directory itself; it sweeps a deleted
  cluster's cache at the next start.

`beehive.db` stays in the data directory. It names each mirror file by cache id and nothing sweeps
a file no record names, so it has to outlive a cleared cache. Read, Write and Edit fence all three
directories.

## Alternatives considered

- **Keep one data directory and exclude subdirectories from backups.** Each platform marks an
  exclusion differently, and none of them helps a run's `TMPDIR` or socket.
- **Keep the kubectl cache under a per-cluster directory in the data directory.** The cache is
  rebuildable, and keeping chats under their cluster tied a chat's paths to a cluster id it never
  reads from disk.
- **Let each service keep joining its own paths.** Nothing could then list Kubetail's directories for
  the fence or the sandbox without repeating every join.
- **Put a run's `TMPDIR` in the runtime directory beside its socket.** On Linux that is a tmpfs
  sized from RAM, so a command could fill memory through it.
- **Use `%TEMP%` on Windows for the runtime directory.** `TEMP` can point at a directory users
  share.

## Consequences

- Backups skip the mirror, the kubectl cache and every `TMPDIR`. The OS or the user may clear the
  cache directory, which costs a resync and loses nothing.
- A run's socket path is the runtime directory plus 35 bytes, which fits every supported platform.
  The length check stays for a long `$TMPDIR`.
- The runtime directory can sit under a temp directory the OS cleans (macOS `$TMPDIR`, a `/tmp`
  fallback on Linux). A snapshot removed mid-session is taken again at the next start.
- Nothing migrates: nothing has shipped.
- An owner that grows a new subtree adds it to `pathsOf`, or the fence and the sandbox miss it.

## Revisit when

A platform gains a sandbox that cannot bind back a path under the runtime or cache directory, or
Kubetail ships and old layouts need a migration.
