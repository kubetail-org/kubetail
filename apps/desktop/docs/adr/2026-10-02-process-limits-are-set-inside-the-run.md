---
title: Bound a sandboxed run's processes with resource limits set inside the run
date: 2026-10-02
scope: sidecar
status: Accepted
---

# Bound a sandboxed run's processes with resource limits set inside the run

## Context

A sandboxed command was bounded in time alone: a foreground call by its timeout (at most
`MaxTimeout`, 600 s) and the group kill after `killGrace`, a background one by nothing but Stop.
A fork loop, a leak or a runaway descriptor count could take the machine meanwhile. On Linux
nothing stopped a process tracing its own children or reading the user's kernel keyring; on
macOS `sudo` could be started.

A desktop app gets no delegated cgroup: systemd's user session gives none to an unprivileged
program by default, and bwrap's `--unshare-cgroup-try` does not make one. So a bound has to be a
resource limit, which both kernels hold per process, set by a process of the run before the shell
starts. What each kernel counts against `RLIMIT_NPROC` differs: Linux from 5.14 counts per user
namespace, every thread a task; Linux before 5.14 and macOS count the user's whole machine, and
macOS counts processes, never threads.

## Decision

A run carries `sandbox.Limits` on its `Policy`, and the run's first processes set them, never
the sidecar:

- **The forwarder (`sandbox-init`) sets a core size of zero on every run, and no other limit.**
  On macOS it becomes every run's first process, with or without a relay.
- **`sandbox-shell`, the forwarder's child, sets every other limit** — CPU time, open files,
  memory and the process count, each clamped to its own hard limit — then execs the shell. On
  Linux, given memory or a process count, it execs with a raw `execve`, the collector off and
  nothing allocated in between. macOS gains a `sandbox-shell` of its own for this, with no filter.
  The forwarder lives as long as the run and relays every connection, so a limit on it binds the
  relay rather than the command: its CPU time grows over a long run until the kernel kills it, and
  with it, as pid 1, the whole run; each relay holds two descriptors; on Linux each of its threads
  is a task, and its runtime dies once it cannot start one.
- **The process limit is a margin over a base the sandbox answers, on a foreground run alone**
  (`Sandbox.CountedProcesses`): 0 on Linux from 5.14 in a user namespace the probe confirmed as
  the run's own, which bwrap is asked for with `--unshare-user-try`; else the user's count, a scan
  of `/proc` by real uid summing threads on Linux and `kern.proc.ruid` on macOS. The margin is in
  the kernel's unit: 512 processes on macOS, `max(1024, 128 × CPUs)` tasks on Linux. The forwarder
  starts before the limit is set and counts against it, so the sandbox adds `forwarderTasks`: 1
  process on macOS, and 32 tasks on Linux, where the forwarder runs with one P and holds 7 threads
  relaying 256 connections at once. Where the count is the user's whole machine, other programs
  move it after the base is read, and runs started together read one base and share one margin;
  a foreground run ends within `MaxTimeout`, so the drift has little time, while a background
  run lasts hours. A background run has no process limit, as it has no CPU limit.
- **CPU time is `MaxTimeout` plus `killGrace` times the CPU count, on a foreground run alone**,
  with a 5 s grace to the hard limit. No foreground process reaches it before the clock ends the
  run, so it binds only one that outlives the clock. A background run has no clock and no CPU
  limit.
- **Memory is 16 GiB of address space, on Linux alone.** `node` reserves about 10 GiB for one
  WebAssembly memory and runs under it (`TestNodeRunsUnderTheMemoryLimit`); `java -version`
  (OpenJDK 25) ran under it on an 11 GiB machine. The ≥64 GiB checks the spec asks for, `java`
  and `pwsh` on a machine whose default heap is largest, were not run when this was written.
  On macOS no process can set a useful `RLIMIT_AS`, since every one already maps the shared
  region, about 466 GiB, so memory there is bounded by the clock alone, and `Command` refuses a
  policy that asks for it rather than run without it.
- **macOS refuses `sudo`, `su`, `login` and `security_authtrampoline`** by path in
  `profile_darwin.sb`. macOS 27.0.1 also refuses every other setuid program under any
  Seatbelt profile, `ps` and `top` included; the list holds wherever it does not. **Linux's filter refuses tracing** (`ptrace`, `process_vm_readv`,
  `process_vm_writev`, `pidfd_getfd`, `kcmp`, `process_madvise`) **and the keyring** (`keyctl`,
  `add_key`, `request_key`) with `EPERM`.

## Alternatives considered

- **A cgroup's `pids.max` and `memory.max`.** Exact and kernel-enforced, but needs a delegated
  cgroup no desktop session gives an unprivileged app.
- **A fixed process limit.** On a machine-wide count it either refuses the shell on a busy
  machine or allows thousands of forks on a quiet one.
- **`--unshare-user`, required.** On a machine with user namespaces off it would fail every run,
  the probe would answer no sandbox, and every command would run outside it. A looser count is
  the smaller loss.
- **Any limit on the forwarder but the core size.** A child inherits the forwarder's limits, but
  so does the forwarder, which outlives every command and relays all of them. `sandbox-shell`
  execs and keeps nothing running, so its limits hold the command alone.
- **A CPU limit of `MaxTimeout`.** `RLIMIT_CPU` counts every thread, so a parallel build spends
  600 s of CPU in under a minute; it would kill legitimate work well inside its timeout.
- **`(deny process-exec (file-mode #o4000))` on macOS.** It also refused setgid files.

## Consequences

- A runaway foreground command is bounded per process in CPU, files and (on Linux) memory, and
  per run in its process count; no run leaves a core dump.
- A background command is bounded in files and (on Linux) memory alone: a fork loop in one runs
  until Stop or the user's own process limit.
- The files, memory and CPU limits are per run, so parallel runs multiply them. The process limit
  multiplies only where the run counts its own namespace; on a machine-wide count, foreground runs
  started together share one margin, and a busy machine can fill it before a run's clock ends.
  `RLIMIT_CPU` is per process, so a fork loop can spend the process limit times the CPU limit
  before the clock stops it.
- Every run starts this executable twice, as `sandbox-init` and `sandbox-shell`, on both
  platforms.
- On macOS no command can list processes: `ps` and `top` are setuid, and `pgrep` needs
  `sysmond`, which the profile does not reach.
- On macOS a process that ignores `SIGXCPU` outlives its CPU limit, since XNU enforces no hard
  one (`TestAnIgnoredSIGXCPUOutlivesTheLimit` pins it), and memory is the clock's alone.
- The base is read once per run, and the probe decides the namespace once: if user namespaces
  are turned off while Kubetail runs, later runs' forks fail until it restarts. A sidecar in a
  container's PID namespace scans short. A user running as root has no process limit.
- `strace` and `gdb` do not run in the Linux sandbox. A process can still read its own
  descendants' `/proc/<pid>/mem` and `environ`, which the kernel checks without the syscall.

## Revisit when

A desktop session delegates a cgroup to an unprivileged app by default, or macOS enforces a hard
CPU limit, or a runtime the users need fails under 16 GiB of address space.
