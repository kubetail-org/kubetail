---
title: The sidecar places Kubetail's folders itself
date: 2026-11-23
scope: cross-cutting
status: Accepted
amends:
  - [Two processes, two log files](2026-09-08-two-processes-two-log-files.md)
  - [Keep each file in the platform's directory for its kind](2026-09-27-kubetails-files-follow-the-platforms-directory-kinds.md)
  - [Keep host.json in the Kubetail config directory](2026-11-21-host-json-lives-under-the-kubetail-config-directory.md)
  - [Linux folders follow XDG](2026-11-21-linux-folders-follow-xdg.md)
  - [Each folder kind has its own folder on every platform](2026-11-22-each-folder-kind-has-its-own-folder-on-every-platform.md)
---

# The sidecar places Kubetail's folders itself

## Context

The host placed every Kubetail folder and handed each to the sidecar as a flag: `--data-dir`,
`--cache-dir`, `--runtime-dir`, `--config-dir`, `--state-dir`, `--app-dir` and the folder of
`--log-file`. The sidecar wrote in the first three and fenced the rest from its file tools and its
sandbox.

The sidecar will be started by the `kubetail` CLI, with no host, and a host may join it later. It then
has to know every folder, the host's included, with nobody to tell it. The hand-off also cost
something on each new kind: a flag, a `Config` field, a `refuseDotDot` row, a line in `pathsOf` and
a row in the host's contract test. A kind the host forgot to pass was left unfenced with no error.

## Decision

Each program places its own folders from one layout.

- The sidecar's `internal/app/layout` mirrors `layout` in `src-tauri/src/paths.rs`: a pure function
  from a platform, the dev or release leaf and the platform's roots to every folder.
  `sidecar/internal/app/layout/testdata/layouts.json` pins both to one answer for Linux, macOS and Windows, dev and
  release. Both decoders refuse an unknown field, and each side compares every field it has, so a
  kind added to one side alone fails a test.
- The sidecar reads the roots from its environment as the host's path resolver does
  (`layout.RootsFromEnv`: `HOME` and the XDG variables, `LOCALAPPDATA`, `APPDATA`), and refuses a
  Unix root holding `..`. On Linux, as on the host, `HOME` is read only for a root that falls back
  to it, and refused there when it is relative. On Windows it reads the profile folders from the
  Known Folder API (`layout.KnownFolders`), as the host's path resolver does, not from the
  environment.
- The leaf is the profile `--dev` names, which defaults to the sidecar's own build. The host passes
  its own build's, so the two take one leaf whatever each was built with, and a launcher picks the
  profile it is. A dev profile's keyring service defaults to `Kubetail-dev` too.
- The sidecar makes its data and cache directories owner-only, and the app folder above them when
  it is not owner-only already (`layout.MakeOwnerOnly`). The host makes its own folders and none of
  the sidecar's.
- The host passes no folder but `--runtime-dir`. Outside `XDG_RUNTIME_DIR` a squatted name gives
  way to a random one beside it, so the runtime directory cannot be computed.
- The sidecar writes `sidecar.log` in the layout's log folder, beside the host's `main.log`. There
  is no `--log-file`. It makes the folder owner-only, and the Kubetail folder above it (the state
  folder on Linux, the app folder on Windows), as the host does; a folder or file it cannot make
  or open leaves it on stderr, which the host forwards.
- `pathsOf` fences every folder `Config.Layout` names.
- The shared fixture is the one guard that the two layouts agree. The host does not check the
  sidecar's layout at run time: the sidecar reports none, and its startup is `READY` or `FATAL`.

A debug build's `KUBETAIL_DATA_DIR`, `KUBETAIL_CACHE_DIR` and `KUBETAIL_RUNTIME_DIR` move where the
sidecar writes, not its layout. The host never reads them. The moved folders are fenced beside the layout's, made if
missing but never tightened, since a developer may share them, and refused on Unix when they hold
`..`.

`make clean-data` deletes the dev layout the sidecar computes: its script,
`sidecar/scripts/clean-dev-data.go`, imports `internal/app/layout`, so no third copy exists. It
refuses while a dev sidecar holds the data directory's lock or listens in the host's runtime
directory, and deletes each root's own folder whole. It is for a dev app that has quit: a launch during it is not guarded.

## Alternatives considered

- **Keep the flags and add the CLI's own copy of them.** The CLI would need the host's layout
  anyway, and a host that joins later could not hand the sidecar anything at its start.
- **Have the sidecar report its layout and the host adopt it.** The host's folders exist before the
  sidecar starts (its log is open first), so the host cannot wait to be told where they are.
- **Pin the two layouts with a test per side instead of a shared fixture.** Two tests can agree with
  their own code and disagree with each other.
- **Have the sidecar report its layout at startup and the host refuse one that differs.** It ties
  the sidecar's start to a host, which a sidecar the CLI starts has none of. The host spawns the
  sidecar with its own environment and profile, so only drift between the two copies could make
  them differ, which the fixture catches before a release.

## Consequences

- Drift between the two copies that the fixture misses goes unseen at run time: the sidecar would
  write and fence other folders than the host's.
- Once the CLI starts the sidecar, a host that joins one started with a different environment is
  not told so.
- The log directory is fenced on every launch, and a sidecar started without a host still logs to
  `sidecar.log`, making its folder when it is missing.
- A root the sidecar cannot read stops its start with a `FATAL` reason. Where the host's resolver
  still finds one, the two differ only there: an unset `HOME`, which the host reads from the user's
  passwd entry, when a root falls back to it.

## Revisit when

The CLI starts the sidecar: it may need the runtime directory placed by the sidecar too, a socket
a host that arrives later can find, and a way for that host to learn the sidecar's layout.
