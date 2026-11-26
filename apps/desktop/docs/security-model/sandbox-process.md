# The sandbox: environment, PATH and processes

[← Security model](../security-model.md)

A sandboxed command starts with what Kubetail gives it and nothing else. Its environment is one table, so no credential variable rides in. Its `PATH` is one setting the user writes, and a folder is searched only when a rule already opens it. Its processes are bounded, gain no root, trace no other process and read no kernel keyring. The same rule holds for the commands Kubetail runs on its own initiative: the Sandbox console, the executable probe and the login shell's run. These rows defend against a command the model wrote and against what the user's startup files and installed programs bring in. Native Windows has no sandbox, and several rows record what the sandbox leaves open by decision.

## Protections

| ID | Protection | Status | Residual |
| --- | --- | --- | --- |
| PROC-1 | A sandboxed command's environment is one table: `PATH`, `LANG` and `TZ` from the sidecar, the user's variables for sandboxed commands from `app.db`, and the rest built. No credential variable rides in. | **Enforced** | — |
| PROC-2 | No run holds a name `NeverEnv` lists, every `AZURE_` and `ARM_` name but `AZURE_CONFIG_DIR` among them. Both platforms' `Command` refuse a run holding a listed name. | **Enforced** | — |
| PROC-3 | An AWS variable passes only when it holds the value AWS's mount writes. The sandboxed login shell holds none. | **Enforced** | — |
| PROC-4 | A variable of the user's whose target includes the sandbox is checked on write and on every read. It is never a name `NeverEnv` lists, a variable that changes how a program starts, a shell variable that runs code, nor one the table sets itself. So it cannot replace `PATH`, `HOME`, `KUBECONFIG`, a cloud platform's mount or a proxy variable. Of the `KUBETAIL` names, only the three a run sets are refused; the CLI's others are the user's. | **Enforced** | — |
| PROC-5 | A run of a chat holds its cluster's variables too. The probe's and a monitor's runs hold the user's variables. A run that cannot read them does not start. | **Enforced** | — |
| PROC-6 | A sandboxed command's `PATH` is one setting the user writes, where `$PATH` stands for the login shell's. The value is split before it is expanded, so nothing the shell lists is ever typed. | **Enforced** | — |
| PROC-7 | A folder of the shell's is searched only when every run reads it already and no group but an administrators' one can write it, so a startup file opens nothing. | **Enforced** | A folder an administrators' group can write is searched unasked. A startup file chooses which `kubectl` runs among the open folders, which `logToolMoves` logs. |
| PROC-8 | A folder the user typed is their read grant, held to a folder grant's checks. A link or another spelling is left out, naming the folder to type. The denied-always list, Kubetail's directories and a closed folder itself stay shut. | **Enforced** | A typed folder is readable to every run. |
| PROC-9 | A world-writable, broad or project folder is left out, whatever its origin. | **Enforced** | — |
| PROC-10 | A setting or shell `PATH` that cannot be read, or a setting Kubetail cannot use, starts no run. Each run reads its `PATH` once. | **Enforced** | — |
| PROC-11 | Reads of the shell run one at a time. One that fails or answers no `PATH` keeps the last. | **Enforced** | — |
| PROC-12 | On Linux two bind mounts of one directory are one `PATH` folder. The second entry is dropped as a duplicate, whatever is mounted beneath only it. | **By decision** | — |
| PROC-13 | The Sandbox console runs the command the user typed only where a sandbox confines it, as a chat's sandboxed command runs. | **By decision** | — |
| PROC-14 | A console command runs in a workspace of its own, with the folders granted always. | **By decision** | — |
| PROC-15 | A console command reads the cluster picked but never changes it, holds no cloud platform, and reaches the internet only when the user ticks it. | **By decision** | A console command leaves no record of what its proxy decided. |
| PROC-16 | Console commands run one at a time. The output is capped, redacted and drawn as text, and reaches no chat and no model. | **By decision** | — |
| PROC-17 | The executable probe runs only the listed invocations: the curated executables', the user's registered ones, and asdf's or mise's resolver for a listed name. Each is a binary resolved on the `PATH` a run would search and its arguments, never a shell. | **Enforced** | — |
| PROC-18 | The probe runs in the real sandbox with no cluster, no network and a throwaway home, reading the folders granted always alone. Its report reaches the user alone, never a model. | **Enforced** | — |
| PROC-19 | The probe checks each registered executable itself, whatever its caller passed. One `CheckExecutable` refuses is listed with its refusal and never run. | **Enforced** | — |
| PROC-20 | A sandboxed command's CPU time is bounded in the foreground. | **By decision** | On macOS a process that ignores `SIGXCPU` outlives its CPU limit. |
| PROC-21 | A sandboxed command's open files are bounded, and it leaves no core dump. | **By decision** | — |
| PROC-22 | A sandboxed command's process count is bounded in the foreground, over what the kernel already counts against the run. | **By decision** | Where the kernel counts the user's whole machine, the base is read once per run, foreground runs started together share one margin, and other programs can fill it before the clock ends. A sidecar in a container's PID namespace scans short. Root has no process limit. |
| PROC-23 | A sandboxed command's memory is bounded on Linux. | **By decision** | Memory on macOS is bounded by the clock or by Stop alone. |
| PROC-24 | The limits are set inside the run, per run and per process. | **By decision** | A background run's CPU and process count are bounded by the clock or by Stop alone. Limits are per run and per process, so parallel runs and a fork loop multiply them. |
| PROC-25 | No sandboxed command gains root through `sudo` or another program that escalates. On Linux the run has `no_new_privs`. On macOS the profile refuses `sudo`, `su`, `login` and `security_authtrampoline` by path. | **Enforced** | — |
| PROC-26 | macOS 27 refuses every other setuid program under the profile. | **Enforced** | On a macOS that runs setuid programs under a profile, one off the list, the system's own such as `ARDAgent` or a third-party one, would run as root inside it. |
| PROC-27 | On Linux a sandboxed command traces no process and reads no other's memory or descriptors. The filter answers `EPERM` to `ptrace`, `process_vm_readv`, `process_vm_writev`, `pidfd_getfd`, `kcmp` and `process_madvise`. | **Enforced** | On Linux a process can read its own descendants' `/proc/<pid>/mem` and `environ`, which the kernel checks without the syscall. |
| PROC-28 | On Linux the forwarder is non-dumpable. | **Enforced** | — |
| PROC-29 | On macOS a sandboxed command reaches no other process's task port. The profile refuses the task port and other processes' information, and their arguments and environment (PROC-30). | **Enforced** | — |
| PROC-30 | On macOS a sandboxed command reads neither the arguments nor the environment of a process outside its run. | **Enforced** on macOS | The guarantee rests on the session, so a caller that replaces `Command`'s `SysProcAttr` without `Setsid` loses it. |
| PROC-31 | A sandboxed command neither reads nor adds a key in the user's kernel keyring. The filter answers `EPERM` to `keyctl`, `add_key` and `request_key`. | **Enforced** | — |
| PROC-32 | Native Windows has no sandbox, so every Bash call there asks. WSL2 runs the Linux build's. | **By decision** | In WSL2 the Linux rows and the refusal of a Windows program started inside are checked by hand, since CI has no WSL2 runner. |
| PROC-33 | On macOS a process a sandboxed command spawns out of its group through `posix_spawn` outlives the run, and the sidecar, confined, with the forwarder's port in reach. The lifeline's kill reaches the forwarder's group alone. | **By decision** | — |
| PROC-34 | Installing the `.deb` on Ubuntu 24.04 lets any local program run `/usr/lib/kubetail-desktop/bwrap` and get a user namespace whose processes hold no capability. | **By decision** | — |
| PROC-35 | Installing the `.deb` on Ubuntu 24.04 lets any local program run `/usr/lib/kubetail-desktop/pasta` and get a user and network namespace in which pasta holds capabilities and can start only a bwrap. | **By decision** | — |
| PROC-36 | The sidecar runs the account record's login shell once at startup on macOS and Linux, never `$SHELL`. | **Enforced** | — |
| PROC-37 | The login shell's run stays bounded: it has a fixed environment and a timeout. Its environment is the fixed list and nothing else of the process's. | **Enforced** | — |
| PROC-38 | The sidecar reads from the shell `PATH`, on macOS a compile-time allowlist of variables, and the provider keys, which it never sets. The command is built from the allowlist and constants alone. Only listed names are taken, and only the kind's rules rewrite a value, with a `~` left verbatim rather than expanded. | **Enforced** | — |
| PROC-39 | A failure installs nothing, leaving the environment and the stored list as they were. | **Enforced** | — |
| PROC-40 | The login shell runs in the sandbox, as does Refresh PATH's. It reads nothing in Kubetail's directories or on the denied-always list, writes only a scratch folder of its own, and reaches no network. | **Enforced** | — |
| PROC-41 | A start the sandbox refuses runs no shell. On Linux with no sandbox the resolution is not run. | **Enforced** | — |
| PROC-42 | A startup line that writes the home fails without ending the shell, which runs interactive. bash and zsh read on. | **Enforced** | A POSIX `sh` such as `dash` abandons the rest of that startup file, by decision, since the shell writes only its scratch. |

## Evidence

### PROC-1

- **Where:** `sidecar/internal/tools/bash/env.go`, `sidecar/internal/run/sandbox/env.go`, `sidecar/internal/run/sandbox/placeholders.go`
- **Records:** [the user's variables](../security/2026-11-20-the-users-variables.md)
- `TestTheSandboxedEnvironmentIsFixed`, `TestTheEnvironmentIsExactlyGiven` — the environment is the one table, on both platforms.

### PROC-2

- **Where:** `sidecar/internal/tools/bash/env.go`, `sidecar/internal/run/sandbox/env.go`, `sidecar/internal/run/sandbox/placeholders.go`
- **Records:** [the user's variables](../security/2026-11-20-the-users-variables.md)
- `TestARunWithAnUnpassableVariableIsRefused`, `TestARunsCheckRefusesAnUnpassableVariable` — both platforms' `Command` refuse a run holding a listed name.
- Note: A run with Azure on holds `AZURE_CONFIG_DIR`, `IDENTITY_ENDPOINT`, `IDENTITY_HEADER` (the grant's token) and `REQUESTS_CA_BUNDLE`, all set by the mount.

### PROC-3

- **Where:** `sidecar/internal/tools/bash/env.go`, `sidecar/internal/run/sandbox/env.go`, `sidecar/internal/run/sandbox/placeholders.go`
- **Records:** [the user's variables](../security/2026-11-20-the-users-variables.md)
- `TestOnlyAPlaceholderPassesTheCheck` — an AWS name passes only with a mount's value.
- `TestTheSandboxedShellDropsAWSCredentials` — the sandboxed login shell holds none.

### PROC-4

- **Where:** `sidecar/internal/tools/bash/env.go`, `sidecar/internal/run/sandbox/env.go`, `sidecar/internal/run/sandbox/placeholders.go`, `sidecar/internal/services/envvars/check.go`
- **Records:** [the user's variables](../security/2026-11-20-the-users-variables.md)
- `TestCheckRefusesForEveryCommand`, `TestCheckRefusesForASandboxedCommandAlone`, `TestKubetailSetsNamesTheTablesVariables`, `TestKubetailSetsEveryToolchainVariable` — the check on write and on every read, and the names the table sets itself.
- `TestCheckPassesAKubetailVariableARunDoesNotSet` — a `KUBETAIL` name a run does not set, `KUBETAIL_NAMESPACE`, passes.
- Note: `assertNoUserCanSet` is cited beside these tests.

### PROC-5

- **Where:** `sidecar/internal/tools/bash/env.go`, `sidecar/internal/run/sandbox/env.go`, `sidecar/internal/run/sandbox/placeholders.go`
- **Records:** [the user's variables](../security/2026-11-20-the-users-variables.md)
- `TestARunThatCannotReadTheUsersVariablesFails` — a run that cannot read the user's variables does not start.
- Note: A value is no secret.

### PROC-6

- **Where:** `sidecar/internal/services/settings`, `sidecar/internal/tools/bash/path.go`
- **Records:** [the `PATH` setting](../security/2026-11-25-settings-in-app-db.md), [ADR](../adr/2026-11-24-the-sandbox-path-is-one-setting-the-user-writes.md)
- `TestCheckPathRefusesEachBadElement`, `TestTheShellsPathIsNeverTyped`, `TestATypedFolderIsCleaned`, `TestARunPathExpandsTheSetting` — the setting's elements are checked, split before `$PATH` is expanded.

### PROC-7

- **Where:** `sidecar/internal/services/settings`, `sidecar/internal/tools/bash/path.go`
- **Records:** [the `PATH` setting](../security/2026-11-25-settings-in-app-db.md), [ADR](../adr/2026-11-24-the-sandbox-path-is-one-setting-the-user-writes.md)
- `TestAShellFolderIsSearchedOnlyWhenOpen`, `TestSearchFolderRefusesWhatARunCannotSearch`, `TestSearchFolderRefusesAFixedMount`, `TestTheRunLeavesOutAFolderNoRuleOpens`, `TestTheRunFollowsALinkIntoAnOpenFolder`, `TestAClosedShellFolderIsFoundOnceTyped` — a shell folder is searched only when it is open already.
- `TestAMovedToolIsLogged` — `logToolMoves` logs the tool a startup file moved.

### PROC-8

- **Where:** `sidecar/internal/services/settings`, `sidecar/internal/tools/bash/path.go`
- **Records:** [the `PATH` setting](../security/2026-11-25-settings-in-app-db.md), [ADR](../adr/2026-11-24-the-sandbox-path-is-one-setting-the-user-writes.md)
- `TestATypedFolderIsARead`, `TestATypedLinkIsLeftOut`, `TestATypedFolderInsideAClosedOneIsOpened`, `TestWithSearchOpensTheFolderItself`, `TestAddableFolders` — a typed folder is a read grant under a folder grant's checks; a link is left out.
- `TestATypedFolderInAnotherCaseIsLeftOut` — another spelling is left out; macOS, on a volume that folds case.

### PROC-9

- **Where:** `sidecar/internal/services/settings`, `sidecar/internal/tools/bash/path.go`
- **Records:** [the `PATH` setting](../security/2026-11-25-settings-in-app-db.md), [ADR](../adr/2026-11-24-the-sandbox-path-is-one-setting-the-user-writes.md)
- `TestAFilteredFolderIsLeftOut`, `TestAFolderListedTwiceIsJudgedAtEachPlace`, `TestFilterDirDropsEachRule`, `TestTheRunLeavesOutABroadFolder` — each filter rule drops its folder, typed or the shell's.

### PROC-10

- **Where:** `sidecar/internal/services/settings`, `sidecar/internal/tools/bash/path.go`
- **Records:** [the `PATH` setting](../security/2026-11-25-settings-in-app-db.md), [ADR](../adr/2026-11-24-the-sandbox-path-is-one-setting-the-user-writes.md)
- `TestTheRunFreezesThePath` — each run reads its `PATH` once.
- `TestARunWhosePathCannotBeReadDoesNotStart`, `TestARefusedPathStartsNoRun`, `TestARunPathReadThatFailsIsAnError` — a `PATH` that cannot be read or used starts no run.

### PROC-11

- **Where:** `sidecar/internal/services/settings`, `sidecar/internal/tools/bash/path.go`
- **Records:** [the `PATH` setting](../security/2026-11-25-settings-in-app-db.md), [ADR](../adr/2026-11-24-the-sandbox-path-is-one-setting-the-user-writes.md)
- `TestShellPathWritesRunOneAtATime`, `TestAShellPathWriteIsBounded`, `TestTheLaunchRunsTheShellOnce` — reads of the shell run one at a time.
- `TestAFailedShellReadKeepsTheLastPath`, `TestAnEmptyShellPathKeepsTheLastRow` — a read that fails or answers no `PATH` keeps the last.

### PROC-12

- **Where:** `sidecar/internal/services/settings/filter.go`
- **Records:** [two mounts of one folder are one `PATH` folder](../adr/2026-11-23-two-mounts-of-one-folder-are-one-path-folder.md)
- Note: Making the mounts takes root.

### PROC-13

- **Where:** `sidecar/internal/tools/bash/console.go`, `src/components/settings/settings-dialog/sandbox-console.tsx`
- **Records:** [the sandbox is the gate](../adr/2026-09-28-the-sandbox-is-the-gate-for-a-sandboxed-command.md), [the Sandbox console](../security/2026-11-21-the-sandbox-console.md)
- `TestTheConsoleRunsNothingOutsideTheSandbox`, `TestTheConsoleRefusesWhatItCannotRun` — the console runs nothing outside the sandbox.

### PROC-14

- **Where:** `sidecar/internal/tools/bash/console.go`, `src/components/settings/settings-dialog/sandbox-console.tsx`
- **Records:** [the sandbox is the gate](../adr/2026-09-28-the-sandbox-is-the-gate-for-a-sandboxed-command.md), [the Sandbox console](../security/2026-11-21-the-sandbox-console.md)
- `TestTheSandboxRefusesAConsoleCommandsRead`, `TestAConsoleCommandReadsTheAlwaysFolders` — the sandbox refuses a console command's read outside the folders granted always.

### PROC-15

- **Where:** `sidecar/internal/tools/bash/console.go`, `src/components/settings/settings-dialog/sandbox-console.tsx`
- **Records:** [the sandbox is the gate](../adr/2026-09-28-the-sandbox-is-the-gate-for-a-sandboxed-command.md), [the Sandbox console](../security/2026-11-21-the-sandbox-console.md)
- `TestAConsoleCommandChangesNoCluster` — the cluster picked is read but never changed.

### PROC-16

- **Where:** `sidecar/internal/tools/bash/console.go`, `src/components/settings/settings-dialog/sandbox-console.tsx`
- **Records:** [the sandbox is the gate](../adr/2026-09-28-the-sandbox-is-the-gate-for-a-sandboxed-command.md), [the Sandbox console](../security/2026-11-21-the-sandbox-console.md)
- `TestARunningConsoleCommandStreamsAndStops`, `TestConsoleOutputIsRedactedAndCapped` — a running command streams and stops; its output is redacted and capped.

### PROC-17

- **Where:** `sidecar/internal/tools/bash/executableprobe.go`, `sidecar/internal/services/settings/executables.go`
- `TestTheProbeRunsOnlyTheList`, `TestAProbeOverAPathThatCannotBeReadRunsNothing` — the probe runs only the list, and nothing over a `PATH` that cannot be read.
- Note: The probe runs the user's own binaries on Kubetail's initiative, where a chat would run them, with less.

### PROC-18

- **Where:** `sidecar/internal/tools/bash/executableprobe.go`, `sidecar/internal/services/settings/executables.go`
- `TestTheProbesSandboxHasNoClusterAndNoNetwork`, `TestAProbeReadsTheAlwaysFoldersAlone`, `TestTheProbesWorkspaceIsGone`, `TestAProbeIsBounded` — no cluster and no network, the folders granted always alone, a throwaway home, and a bounded run.

### PROC-19

- **Where:** `sidecar/internal/tools/bash/executableprobe.go`, `sidecar/internal/services/settings/executables.go`
- `TestCheckExecutableRefusesEachBadShape`, `TestAProbeRunsNoRefusedExecutable` — `CheckExecutable` refuses each bad shape, and the probe runs no refused executable.
- Note: A registered executable is whatever the user named, read from `app.db`'s `executables`.
- Note: Its output is drawn as text, never read for a path.

### PROC-20

- **Where:** `sidecar/internal/run/sandbox`, `sidecar/internal/tools/bash/bash.go`
- **Records:** [process limits are set inside the run](../adr/2026-10-02-process-limits-are-set-inside-the-run.md)
- `TestACPUSpinEndsWithSIGXCPU` — a CPU spin ends with `SIGXCPU`, on both platforms.
- `TestAnIgnoredSIGXCPUIsKilledAtTheHardLimit` — an ignored `SIGXCPU` is killed at the hard limit, on Linux.
- `TestAnIgnoredSIGXCPUOutlivesTheLimit` — the macOS residual.

### PROC-21

- **Where:** `sidecar/internal/run/sandbox`, `sidecar/internal/tools/bash/bash.go`
- **Records:** [process limits are set inside the run](../adr/2026-10-02-process-limits-are-set-inside-the-run.md)
- `TestOpeningPastTheFileLimitFails` — opening past the file limit fails, on both platforms.
- `TestTheCoreSizeIsZero` — no core dump.

### PROC-22

- **Where:** `sidecar/internal/run/sandbox`, `sidecar/internal/tools/bash/bash.go`
- **Records:** [process limits are set inside the run](../adr/2026-10-02-process-limits-are-set-inside-the-run.md)
- `TestAForkLoopStopsUnderAMachineWideCount` — a fork loop stops under a machine-wide count, on both platforms.
- `TestAForkLoopStopsAtTheProcessLimit` — a fork loop stops at the process limit, on Linux.
- `TestTheForwarderStaysUnderItsTasks` — the forwarder stays under its tasks, on Linux.

### PROC-23

- **Where:** `sidecar/internal/run/sandbox`, `sidecar/internal/tools/bash/bash.go`
- **Records:** [process limits are set inside the run](../adr/2026-10-02-process-limits-are-set-inside-the-run.md)
- `TestAllocatingPastTheMemoryLimitFails` — allocating past the memory limit fails, on Linux.
- `TestAMemoryLimitIsRefused` — macOS.

### PROC-24

- **Where:** `sidecar/internal/run/sandbox`, `sidecar/internal/tools/bash/bash.go`
- **Records:** [process limits are set inside the run](../adr/2026-10-02-process-limits-are-set-inside-the-run.md)
- `TestTheShellSetsTheLimits`, `TestTheShellAppliesItsLimits`, `TestTheWorkspacePolicyHasTheLimits`, `TestASandboxedCommandRunsUnderTheLimits` — the shell sets and applies the limits, and the workspace policy has them.
- `TestABackgroundRunHasNoCPUOrProcessLimit` — a background run has no CPU or process limit.
- Note: The source lists `TestTheShellSetsTheLimits`, `TestTheShellAppliesItsLimits`, `TestTheCoreSizeIsZero` and `TestTheForwarderStaysUnderItsTasks` together, followed by (Linux).

### PROC-25

- **Where:** `sidecar/internal/run/sandbox/seccomp_linux.go`, `sidecar/internal/run/sandbox/profile_darwin.sb`
- `TestSudoCannotGainRoot` — `sudo` gains no root, on both platforms.
- `TestTheProfileRefusesSetuidPrograms` — the macOS profile refuses the listed programs by path.

### PROC-26

- **Where:** `sidecar/internal/run/sandbox/seccomp_linux.go`, `sidecar/internal/run/sandbox/profile_darwin.sb`
- `TestEverySetuidProgramIsRefused` — macOS 27 refuses every other setuid program under the profile.

### PROC-27

- **Where:** `sidecar/internal/run/sandbox/seccomp_linux.go`, `sidecar/internal/run/sandbox/profile_darwin.sb`
- `TestTheFilterRefusesTracing`, `TestTheShellCannotTraceAProcess`, `TestTheShellCannotTraceItsOwnChild` — the filter answers `EPERM` to the tracing syscalls.

### PROC-28

- **Where:** `sidecar/internal/run/sandbox/seccomp_linux.go`, `sidecar/internal/run/sandbox/profile_darwin.sb`
- `TestTheFirstProcessCannotBeRead` — the forwarder is non-dumpable.

### PROC-29

- **Where:** `sidecar/internal/run/sandbox/seccomp_linux.go`, `sidecar/internal/run/sandbox/profile_darwin.sb`
- `TestATaskPortIsRefused` — the profile refuses the task port and other processes' information.

### PROC-30

- **Where:** `sidecar/internal/run/sandbox/profile_darwin.sb`, `sidecar/internal/run/sandbox/sandbox_darwin.go`
- **Records:** [a macOS run leads its own session](../adr/2026-10-13-a-macos-run-leads-its-own-session-so-no-run-reads-another-processs-launch-strings.md), [no run reads another process's launch strings](../security/2026-10-13-no-run-reads-another-processs-launch-strings.md)
- `TestAnotherProcessesLaunchStringsAreRefused` — another process's launch strings are refused.
- Note: The profile denies process information outside the run and `kern.procargs*`, which together refuse `kern.procargs2` for a process in another process group.
- Note: `Command` starts every run in a session of its own, which the profile keeps it in.
- Note: On Linux the run's PID namespace hides every other process.

### PROC-31

- **Where:** `sidecar/internal/run/sandbox/seccomp_linux.go`
- `TestTheFilterRefusesTheKeyring`, `TestTheShellCannotReadTheKeyring` — the filter answers `EPERM` to the keyring syscalls.
- `TestTheKeychainIsDenied` — macOS has no kernel keyring, and its Keychain is denied.

### PROC-32

- **Where:** `sidecar/internal/run/sandbox/sandbox_windows.go`
- **Records:** [native Windows has no sandbox](../adr/2026-09-28-native-windows-has-no-sandbox.md), [TODO](../TODO.md#security)
- `TestWindowsHasNoSandbox` — `Probe` answers none and `Command` starts nothing.
- Note: In WSL2 the Linux rows hold.
- Note: A Windows program started inside WSL2 should not start, since the seccomp filter refuses the sockets WSL's interop needs.

### PROC-33

- **Where:** `sidecar/internal/run/sandbox/profile_darwin.sb`
- **Records:** [a macOS run keeps its group](../adr/2026-09-28-a-macos-run-keeps-its-group-by-refusing-setsid.md)
- `TestAProcessCannotLeaveItsGroup` — `setsid` and `setpgid` are refused.
- `TestAProcessThatLeavesTheGroupStaysConfined` — a process that leaves stays confined.

### PROC-34

- **Where:** `src-tauri/linux/kubetail-desktop-bwrap`, `src-tauri/linux/postinst.sh`
- **Records:** [Kubetail ships its own bwrap](../adr/2026-09-28-kubetail-ships-its-own-bwrap.md)
- Note: The profile is AppArmor's own for `/usr/bin/bwrap`, renamed and attached to Kubetail's path.
- Note: Ubuntu 25.04 and later make the same grant for every program.
- Note: The release's 24.04 check installs the `.deb` and runs the sandbox's chain through it.

### PROC-35

- **Where:** `src-tauri/linux/kubetail-desktop-pasta`, `src-tauri/linux/postinst.sh`
- **Records:** [Kubetail ships its own pasta](../adr/2026-10-04-kubetail-ships-its-own-pasta.md)
- `scripts/check-sandbox-chain.sh` — runs a command with the internet through it on a stock 24.04, in the release's check and on every pull request.
- Note: The profile is passt's own for `/usr/bin/pasta`, renamed and attached to Kubetail's path, with its exec rule narrowed from every program under `/usr/bin` to the sandbox's bwrap.

### PROC-36

- **Where:** `sidecar/internal/run/loginshell`, `sidecar/internal/app/launchshell_unix.go`, `sidecar/internal/app/launchshell_darwin.go`
- **Records:** [ADR](../adr/2026-09-07-shell-environment-allowlist.md), [`PATH` from the login shell](../security/2026-10-02-path-from-the-login-shell.md), [the login shell in the sandbox](../security/2026-10-04-the-login-shell-in-the-sandbox.md), [TODO](../TODO.md#security)
- `TestResolveRunsTheAccountShell`, `TestAccountShellAsksGetent`, `TestAccountShellAsksDscl`, `TestAccountShellFallsBack` — the shell is the record's.
- `TestResolveIsRunOnceAtLaunch` — one run answers both readers.

### PROC-37

- **Where:** `sidecar/internal/run/loginshell`, `sidecar/internal/app/launchshell_unix.go`, `sidecar/internal/app/launchshell_darwin.go`
- **Records:** [ADR](../adr/2026-09-07-shell-environment-allowlist.md), [`PATH` from the login shell](../security/2026-10-02-path-from-the-login-shell.md), [the login shell in the sandbox](../security/2026-10-04-the-login-shell-in-the-sandbox.md), [TODO](../TODO.md#security)
- `TestTheShellsEnvironmentIsScrubbed` — the environment is the fixed list and nothing else of the process's.

### PROC-38

- **Where:** `sidecar/internal/run/loginshell`, `sidecar/internal/app/launchshell_unix.go`, `sidecar/internal/app/launchshell_darwin.go`
- **Records:** [ADR](../adr/2026-09-07-shell-environment-allowlist.md), [`PATH` from the login shell](../security/2026-10-02-path-from-the-login-shell.md), [the login shell in the sandbox](../security/2026-10-04-the-login-shell-in-the-sandbox.md), [TODO](../TODO.md#security)
- `TestImportedIsExactlyTheAllowlist` — the command is built from the allowlist and constants alone.
- `TestParseTakesEachVariablesFrame`, `TestResolveEnvAppliesTheKindOfEachVariable`, `TestResolvePathMakesAnEntryMeaningfulOutsideTheShell`, `TestResolveImportsTheVariablesTheShellExports` — only listed names are taken and only the kind's rules rewrite a value.
- Note: Widens the exec-plugin surface and selects the browser opener's `open`.

### PROC-39

- **Where:** `sidecar/internal/run/loginshell`, `sidecar/internal/app/launchshell_unix.go`, `sidecar/internal/app/launchshell_darwin.go`
- **Records:** [ADR](../adr/2026-09-07-shell-environment-allowlist.md), [`PATH` from the login shell](../security/2026-10-02-path-from-the-login-shell.md), [the login shell in the sandbox](../security/2026-10-04-the-login-shell-in-the-sandbox.md), [TODO](../TODO.md#security)
- `TestResolveLeavesTheEnvironmentAloneOnFailure`, `TestAFailedShellReadKeepsTheLastPath` — a failure leaves the environment and the stored list as they were.

### PROC-40

- **Where:** `sidecar/internal/run/loginshell`, `sidecar/internal/app/launchshell_unix.go`, `sidecar/internal/app/launchshell_darwin.go`
- **Records:** [ADR](../adr/2026-09-07-shell-environment-allowlist.md), [`PATH` from the login shell](../security/2026-10-02-path-from-the-login-shell.md), [the login shell in the sandbox](../security/2026-10-04-the-login-shell-in-the-sandbox.md), [TODO](../TODO.md#security)
- `TestTheLoginShellCannotReadKubetailsDirectories`, `TestTheResolutionCannotReadTheNeverList` — it reads nothing in Kubetail's directories or on the denied-always list.
- `TestTheLoginShellWritesItsScratch`, `TestTheLoginShellCannotWriteTheHome` — it writes only a scratch folder of its own.
- `TestTheLoginShellHasNoNetwork` — it reaches no network.
- `TestTheLaunchResolutionRunsInTheSandbox`, `TestTheRefreshRunsInTheSandbox` — the launch's run and Refresh PATH's both run in the sandbox.

### PROC-41

- **Where:** `sidecar/internal/run/loginshell`, `sidecar/internal/app/launchshell_unix.go`, `sidecar/internal/app/launchshell_darwin.go`
- **Records:** [ADR](../adr/2026-09-07-shell-environment-allowlist.md), [`PATH` from the login shell](../security/2026-10-02-path-from-the-login-shell.md), [the login shell in the sandbox](../security/2026-10-04-the-login-shell-in-the-sandbox.md), [TODO](../TODO.md#security)
- `TestAPolicyTheSandboxRefusesIsAFault` — a start the sandbox refuses runs no shell.
- `TestLinuxSkipsTheResolutionWithNoSandbox` — on Linux with no sandbox the resolution is not run.

### PROC-42

- **Where:** `sidecar/internal/run/loginshell`, `sidecar/internal/app/launchshell_unix.go`, `sidecar/internal/app/launchshell_darwin.go`
- **Records:** [ADR](../adr/2026-09-07-shell-environment-allowlist.md), [`PATH` from the login shell](../security/2026-10-02-path-from-the-login-shell.md), [the login shell in the sandbox](../security/2026-10-04-the-login-shell-in-the-sandbox.md), [TODO](../TODO.md#security)
- `TestAProfileThatWritesTheHomeStillResolves` — a startup line that writes the home fails without ending the shell.
