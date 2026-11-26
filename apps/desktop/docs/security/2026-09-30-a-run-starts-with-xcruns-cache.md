# Security record — a run starts with xcrun's cache, 30 September 2026

**Subject:** on macOS a sandboxed run's `TMPDIR` now starts with a copy of the user's xcrun
lookup cache. The profile is unchanged. The living model is
[security-model.md](../security-model.md); the sandbox is recorded in
[Bash runs in a sandbox](2026-09-28-bash-runs-in-a-sandbox.md).

## The problem

`/usr/bin/git`, `/usr/bin/python3`, `make`, `clang` and the rest are xcrun shims. The shim looks
the real tool up in a cache at `$TMPDIR/xcrun_db`. Inside the sandbox it can read and write that
cache: `TMPDIR` is the run's own. But each run's `TMPDIR` is new and empty, so the first shim
call of every run misses.

With Command Line Tools selected, a miss is a cheap lookup. With Xcode.app selected, a miss runs
`xcodebuild`, which loads Xcode's frameworks inside the sandbox. On a CI runner that took 12–14s
for `git --version` and `python3 -c pass`.

## What changed

- **`sandbox.SeedTmpDir` copies the cache in.** `newRunDir` calls it after making the run's
  `TMPDIR`. It reads `xcrun_db` from the sidecar's own `TMPDIR`, the user's temp directory, and
  writes it into the run's, 0600. A cache that is missing or unreadable copies nothing, and the
  run fills its own. It is a no-op off macOS.
- **The profile is unchanged.** No path, rule or Mach service was added. `/private/var/folders`
  and `/tmp` stay unreadable.

## Why this is safe

- **The copy is the run's own.** A run can rewrite it, but the copy goes when the run's `TMPDIR`
  goes. Nothing a run writes to it reaches another run, the user's cache or a command outside
  the sandbox.
- **A run never writes the source.** The user's temp directory is outside every rule the profile
  grants, so a run cannot poison the cache the next run is seeded from.
- **The cache names tools, not secrets.** Each entry is a lookup key (the tool, the SDK, the
  developer directory) and the path it resolved to. A run could already read every one of those
  paths.

## Residuals

- A tool the user has never run outside Kubetail is still a miss, and with Xcode.app selected it
  still runs `xcodebuild` in the sandbox.
- The fix is checked with Command Line Tools on a Mac and with Xcode.app only by CI's macOS
  runners (`TestTheListedProgramsRun`, which warms the runner's cache first).
