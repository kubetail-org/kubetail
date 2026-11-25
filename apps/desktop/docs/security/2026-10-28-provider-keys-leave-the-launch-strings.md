# Security record — provider keys leave the launch strings, 28 October 2026

**Subject:** the provider keys a sidecar is started with no longer sit in the strings the kernel
keeps from its exec, which another process of the same user can read. The living model is
[security-model.md](../security-model.md); the gap was
[kstackhq/kstack#28](https://github.com/kstackhq/kstack/issues/28).

## The problem

The sidecar reads the provider keys from its environment and clears them with `os.Unsetenv`,
which changes Go's live copy alone. The kernel keeps the arguments and environment the process
was exec'd with, and a process of the same user reads them: on macOS before 27 through
`kern.procargs2`, as `ps -E` does, and on Linux through `/proc/<pid>/environ`. A sandboxed
command is refused that read ([no run reads another process's launch
strings](2026-10-13-no-run-reads-another-processs-launch-strings.md)); a process outside the
sandbox is not: a kubeconfig exec plugin reading its parent, a command run where no sandbox is
available, anything else running as the user.

## What changed

- **The sidecar execs itself once at startup, without the keys.** Started with a key variable
  set, `main` writes the keys to a pipe and calls `exec` on its own binary with the same
  arguments plus `--keys-fd=<n>` and the environment minus the key variables. The pid and every
  open file stay, so the host notices nothing. The kernel replaces the launch strings.
- **The new image reads the keys from the pipe** and answers the config from them. A flag naming
  anything but a readable pipe stops the start: the sidecar never runs without the keys it was
  given.
- **The pipe is written before anything reads it**, so the write is non-blocking, and keys the
  pipe cannot hold whole are an error, never a write that blocks the start.
- **A key variable set beside the flag is an error.** The flag is the re-exec's alone; a launch
  that passes it by hand with a key in the environment would keep the key in the launch strings.
- **No key is ever on the command line.** The flag carries a number; argv is as readable as the
  environment.
- **Windows is unchanged.** It has no exec, and nothing there reads another process's launch
  environment.

## Why this is safe

- **`TestTheReexecKeepsTheKeyOutOfTheLaunchStrings`** starts the binary with a fake key, reads
  the re-exec'd process through `kern.procargs2` on macOS and `/proc` on Linux, and finds neither
  the key's name nor its value while the catalog still lists the provider.
- **`TestAKeyInTheEnvironmentIsMovedToThePipe`** pins the exec's arguments and environment: the
  pipe's number and no key in argv, the key variables out of the environment and the rest kept,
  the pipe open across the exec.
- **`TestABadKeysFDStopsTheSidecar`** and **`TestAKeysFDThatIsNotAPipeIsAnError`** pin that a
  bad flag is an exit, not a keyless start.
- **The re-exec runs before anything else**: no goroutine of ours, no child, no file. The
  `sandbox-*` subcommands run first, as they take none of the sidecar's flags and inherit no
  key from the sidecar that starts them.

## What stays

- **A key the login shell answers** never reached the launch strings and is unchanged by this.
- **Other processes' launch strings** — the terminal, its shell, a host launched from one — still
  hold what they were started with. Only the sidecar's own are cleaned.
- **The pipe is open to the same reader for a moment.** Between the exec and the read, a process
  of the user that can read the launch strings can open the pipe too (`/proc/<pid>/fd/<n>` on
  Linux) and read or empty it; emptied, the sidecar stops with an error. The window is the
  new image's first step, and the reader is one that could read the strings before.
- **A process running as the user can read the sidecar's memory** where the platform allows it,
  which no re-exec changes. This is hardening against the one cheap read, not a boundary.
