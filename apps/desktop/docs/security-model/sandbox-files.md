# The sandbox: files and the fence

[← Security model](../security-model.md)

A command the sandbox confines runs without asking, so the sandbox is the gate between the command and the user's files. A sandboxed command reads the system's and the toolchain folders Kubetail lists and the folders the user granted, and it writes only its workspace, its tool home, its kubectl cache and its `TMPDIR`. Two sets stay closed under every rule: the denied-always list and Kubetail's directories. Kubetail's directories are fenced at their resolved paths, and two paths are compared by the files they name, through a link, a `..` after one, a second name of one file or, on macOS, another case. A grant is the user's typing, checked whenever it is read. The fence assumes a rule's path keeps its name for the length of a run, and on Linux that a denied path exists when the run starts. Where Kubetail's own folders come from, and how a link in one is treated at start, is in [Kubetail's folders and links](folders.md).

## Protections

| ID | Protection | Status | Residual |
| --- | --- | --- | --- |
| SBX-1 | A command the sandbox confines runs without asking, in the foreground or the background, on macOS and Linux. | **By decision** | — |
| SBX-2 | A sandboxed command reads no file of the user's but the system's and the toolchain folders Kubetail lists, less the denied-always list and Kubetail's directories. | **Enforced** | — |
| SBX-3 | The zones are the lists. The denied-always list holds the credentials, histories, container sockets, browser profiles, `/etc`'s secret files, `/root` and the other users' homes. | **Enforced** | — |
| SBX-4 | A sandboxed command writes only its workspace, its tool home, its kubectl cache and its `TMPDIR`. | **Enforced** | — |
| SBX-5 | A run whose own path is a link is refused. | **Enforced** | — |
| SBX-6 | Of the user's `PATH`, only a folder every run reads already or one the user typed is readable. The zones open no `PATH` entry. | **Enforced** | — |
| SBX-7 | A toolchain folder is readable whole but for the denied-always list and Kubetail's directories. A toolchain folder on a credential path is left out, `~/.docker/bin` among them. | **Enforced** | — |
| SBX-8 | `~/Documents`, `~/Desktop` and `~/Downloads` stay shut under a Read of the home. | **Enforced** | — |
| SBX-9 | Every rule of a run's policy answers alike on both platforms. | **Enforced** | — |
| SBX-10 | A policy its `Check` refuses, or one Linux cannot enforce over its fixed mounts, starts nothing. | **Enforced** | — |
| SBX-11 | A missing Read or Write path opens nothing. | **Enforced** | — |
| SBX-12 | On macOS a run's `TMPDIR` starts with a copy of the user's xcrun lookup cache, which nothing a run writes reaches back into. | **Enforced** on macOS | — |
| SBX-13 | A grant is a Read or Write Files rule, checked again at the run's start. Where two grants nest, the wider wins. | **Enforced** | — |
| SBX-14 | A run reads the folders the user granted, or reads and writes them, with the denied-always list and Kubetail's directories still closed inside each. Through the real sandbox a read-write grant is written and a read one is not. | **Enforced** | — |
| SBX-15 | No rule opens the denied-always list: a Read of `~` still hides `~/.ssh`. | **Enforced** | On Linux a Deny is a mount, and a mount needs a path. A denied-always path that does not exist when a run starts is not covered, and a file something outside the run makes there while it runs is readable to it. The run itself cannot make the path. |
| SBX-16 | No rule opens Kubetail's directories, and inside them only a run's own paths open. Kubetail's log, config and state directories are among its directories, and on macOS and Windows so is the app folder that holds several of them. | **Enforced** | On Linux a Deny needs a path that exists. Kubetail's config directory is made at launch: removed mid-session, a settings save during a run makes it again uncovered. |
| SBX-17 | Each of Kubetail's directories is fenced at its resolved path. A `logs` linked out of the state is fenced at its target. | **Enforced** | — |
| SBX-18 | The file tools and the grant zones compare folders as files, so each folder keeps its own entry, and a second name of a nested folder, such as a bind mount's source, is still fenced. The sandbox denies by path, so it compiles one folder inside another with the one around it. | **Enforced** | — |
| SBX-19 | The host refuses a root holding `..`, by its name alone, since a link before one would lead the host's files away from the folder the sidecar fences. | **Enforced** | The host reads each root once, at launch: a link in a root's path that is retargeted while the app runs leaves the host's open log files in the folder it led to before. |
| SBX-20 | The sidecar logs only in the layout's log folder, and takes the host's profile. | **Enforced** | — |
| SBX-21 | The sidecar places and fences every folder from the same roots, the log, config and state directories and the app folders included. A shared fixture holds its layout to the host's. | **Enforced** | — |
| SBX-22 | The sidecar refuses a root holding `..`, a relative `HOME` a root falls back to, and a runtime directory or a debug build's data or cache override holding `..`. | **Enforced** | — |
| SBX-23 | A policy's check refuses a rule inside an Always path or beneath a Write rule. | **Enforced** | — |
| SBX-24 | Rules compare and compile in the disk's spelling. On a case-insensitive volume a link that spells a denied path or a Kubetail root in another case is still denied under a Read granted in the disk's spelling. | **Enforced** on macOS | — |
| SBX-25 | A folder that is the home or an app-data folder in another case is as broad as it. | **Enforced** on macOS | — |
| SBX-26 | A policy naming a path the kernel cannot name is refused. | **Enforced** | — |
| SBX-27 | The one run Kubetail starts without the denied-always list is the snapshot's login shell. Its output reaches only commands outside the sandbox, which read the home themselves. It still reads nothing of Kubetail's and writes only its scratch. | **By decision** | — |
| SBX-28 | macOS holds a Deny whether or not its path exists. | **Enforced** on macOS | — |
| SBX-29 | A rule's path is named when the run's rules compile, and a later change can leave the rule on a name the disk has stopped using until the next run: a rename by a process outside the sandbox, a link on the path as written repointed, or a rename by another run holding a Write rule over a folder on the path. | **By decision** | — |
| SBX-30 | Two paths are compared by the files they name, through a link, a `..` after one, a second name of one file or, on macOS, another case. Settings and the file tools so compare a grant with the zones however each is spelled. | **By decision** | Not compared as files: a path past its deepest folder that exists, and within one policy a file with two hard links or a folder mounted twice. |
| SBX-31 | The kernel names each rule path for the profile, on macOS as the volume stores it. A rule it cannot name refuses the run, and so does a policy that names one folder two ways. | **By decision** | — |
| SBX-32 | A policy's check refuses a rule beneath a Write rule of the same policy, so a run cannot rename a folder on its own rules' canonical paths. | **By decision** | — |
| SBX-33 | A mount that stops answering holds no probe's policy build and no run's command build past its caller's context. Building a probe's policy and a run's command runs on a worker the caller leaves when its context ends. | **Enforced** | Only the caller's wait is bounded: the filesystem call itself is not cancelled and holds its thread until the mount answers. Not bounded: the Linux probe's lookup of its bwrap and pasta candidates, the probes' scratch folder in `TMPDIR`, and a direct call to `System` or `Never`. |
| SBX-34 | When a hung build ends a run, Bash says whether its deadline or a cancel ended it. | **Enforced** | — |
| SBX-35 | At most 16 such workers run at once, after which a run waits for one to end until its own context does. | **Enforced** | — |
| SBX-36 | A worker left behind reads its own copy of the run and of the probed bwrap. | **Enforced** | — |
| SBX-37 | A session's folders come from one builder, which answers only folders that pass their check now. | **Enforced** | — |
| SBX-38 | The builder answers no folder where no sandbox confines the run: Windows, a Linux with no usable sandbox, a failed probe, a chat switched outside it. | **Enforced** | — |
| SBX-39 | A session with no folders function reads none, as the monitor's does. | **Enforced** | — |
| SBX-40 | A grant names its folder by its resolved path. A link is refused with its target, and a grant that moved is left out. | **Enforced** | — |
| SBX-41 | A `PATH` entry is compared as the folder the shell searches. | **Enforced** | — |
| SBX-42 | A grant is a `folder_grants` row, no rule, checked whenever it is read. A read of the grants that fails fails what needed them. | **Enforced** | — |
| SBX-43 | A grant is the user's typing, in Settings' *Folders* or under a sandboxed command that failed, both one `FolderGrantForm`. The path is sent as typed and the field starts empty, so nothing a command prints fills it. | **Enforced** | — |
| SBX-44 | No grant reaches a mount every run has its own of. | **Enforced** | — |
| SBX-45 | No read-write grant reaches what Kubetail knows runs outside the sandbox, nor the home. That is a folder of the `PATH` setting, typed or from the login shell, whatever the setting says of the shell's, a system or toolchain Read path, and the shells' startup files and folders, launchd's and systemd's. | **Enforced** | `NoWrite` is curated, so a file a startup file sources from elsewhere, a repository's `.git/config` hook or its `.envrc` is open to a read-write grant. |
| SBX-46 | A folder check whose read of the `PATH` fails is an error, never a refusal on the grant. | **Enforced** | — |
| SBX-47 | A sandboxed command's tools write under its chat's tool home, never another chat's. Only the kubectl cache is shared by a cluster's chats. | **Enforced** | The kubectl discovery cache is shared, so one chat can plant a document a later chat's `kubectl` reads. |

## Evidence

### SBX-1

- **Where:** `sidecar/internal/tools/bash/bash.go`, `sidecar/internal/run/sandbox`
- **Records:** [the sandbox is the gate](../adr/2026-09-28-the-sandbox-is-the-gate-for-a-sandboxed-command.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestASandboxedCallAsksNoOne`, `TestASandboxedBackgroundCallAsksNoOne` — `Approval` skips a call whose sandbox `Confines`.
- `TestBwrapConfinesOnAFixedPort`, `TestAProbeThatRunsIsASandbox`, `TestWindowsHasNoSandbox` — `Confines` is true for bubblewrap and Seatbelt alone.
- `TestASandboxedReadRunsUnasked`, `TestASubagentsSandboxedCallRunsUnasked` — end to end a sandboxed read runs with no approval, a subagent's too.
- `TestAProbeThatTimesOutKeepsTheSandbox` — a macOS probe that times out keeps the sandbox and its calls skip.
- Note: a profile that does not apply starts nothing.

### SBX-2

- **Where:** `sidecar/internal/run/sandbox`
- **Records:** [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md), [a run cannot replace its own paths](../security/2026-09-30-a-run-cannot-replace-its-own-paths.md), [the sandbox's own environment](../security/2026-10-02-the-sandboxs-own-environment.md)
- `TestTheHomeIsUnreadable`, `TestACredentialPathInsideAReadableTreeIsUnreadable`, `TestKubetailsDirectoriesAreUnreadable`, `TestASiblingRunsKubeconfigIsUnreadable` — what a run cannot read, on Linux and the same on macOS.
- `TestTheTempDirectoriesAreDenied`, `TestHomebrewsVarIsDenied`, `TestATerminalIsUnreachable` — on macOS, with the same.

### SBX-3

- **Where:** `sidecar/internal/run/sandbox`
- **Records:** [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestSystemReadsTheZones`, `TestAToolchainLinkToABroadFolderIsNotRead`, `TestAShellInABroadFolderReadsOnlyItself`, `TestTheShellsPrefixIsReadButNeverTheHome` — the zones are the lists.
- `TestEtcSecretsStayHidden`, `TestTheOtherHomesAreNever`, `TestANeverPathAboveTheHomeIsDropped`, `TestANestedHomeIsNotAnotherUsersHome` — the zones are the lists.

### SBX-4

- **Where:** `sidecar/internal/run/sandbox`
- **Records:** [a run cannot replace its own paths](../security/2026-09-30-a-run-cannot-replace-its-own-paths.md)
- `TestOnlyTheWorkspaceAndTheCacheAreWritten`, `TestARunCannotReplaceItsWorkspace` — what a run writes, on Linux and the same on macOS.
- `TestTheProfileKeepsARunsOwnRootsInPlace` — on macOS, with the same.

### SBX-5

- **Where:** `sidecar/internal/run/sandbox`
- **Records:** [a run cannot replace its own paths](../security/2026-09-30-a-run-cannot-replace-its-own-paths.md)
- `TestARunsOwnPathThatIsALinkIsRefused` — a run whose own path is a link is refused.

### SBX-6

- **Where:** `sidecar/internal/run/sandbox`
- `TestAFolderOnThePathIsNotRead` — the zones open no `PATH` entry.

### SBX-7

- **Where:** `sidecar/internal/run/sandbox`
- **Records:** [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md), [a run cannot replace its own paths](../security/2026-09-30-a-run-cannot-replace-its-own-paths.md), [the sandbox's own environment](../security/2026-10-02-the-sandboxs-own-environment.md)
- `TestSystemLeavesOutTheNeverPaths`, `TestDockerDesktopsBinStaysDenied`, `TestTheProbeKeepsTheCredentialsHidden` — a toolchain folder on a credential path is left out, `~/.docker/bin` among them.

### SBX-8

- **Where:** `sidecar/internal/run/sandbox`
- `TestAGrantOfTheHomeLeavesClosedFoldersShut` — `~/Documents`, `~/Desktop` and `~/Downloads` stay shut under a Read of the home.

### SBX-9

- **Where:** `sidecar/internal/run/sandbox`
- `TestEveryPairOfRulesAnswersAlike` — every rule of a run's policy answers alike on both platforms.

### SBX-10

- **Where:** `sidecar/internal/run/sandbox`
- `TestAPolicyChecks`, `TestAPolicyThatFailsCheckStartsNothing`, `TestARuleOverAFixedMountIsRefused` — a policy its `Check` refuses, or one Linux cannot enforce over its fixed mounts, starts nothing.

### SBX-11

- **Where:** `sidecar/internal/run/sandbox`
- `TestAMissingReadOrWritePathIsSkipped` — a missing Read or Write path opens nothing.

### SBX-12

- **Where:** `sidecar/internal/run/sandbox`
- **Records:** [a run starts with xcrun's cache](../security/2026-09-30-a-run-starts-with-xcruns-cache.md)
- `TestARunsTMPDIRStartsWithXcrunsCache` — a run's `TMPDIR` starts with a copy of the user's xcrun lookup cache.

### SBX-13

- **Where:** `sidecar/internal/run/sandbox`
- **Records:** [path grants](../security/2026-10-04-path-grants.md)
- `TestAGrantIsAFilesRule`, `TestNestedGrantsResolveToTheWider`, `TestAGrantThatFailsTheCheckIsLeftOut` — a grant is a Read or Write Files rule, checked again at the run's start, the wider winning where two nest.

### SBX-14

- **Where:** `sidecar/internal/run/sandbox`
- **Records:** [path grants](../security/2026-10-04-path-grants.md)
- `TestAReadWriteGrantIsWritten` — through the real sandbox a read-write grant is written and a read one is not.
- Note: a grant inside a closed folder opens that folder alone.

### SBX-15

- **Where:** `sidecar/internal/run/sandbox/policy.go`
- `TestTheDeniedAlwaysListWinsOverARead` — the denied-always list wins over a Read, on both platforms.
- `TestADeniedAlwaysPathStaysHiddenUnderAGrantedHome` — over a real grant of a home, `~/.ssh`, `~/.kube`, a closed folder and Kubetail's `app.db` stay shut, on both platforms.
- Note: an example of the residual is `ssh-keygen` making a key while a background command reads `~`.
- Note: macOS has no such residual. See SBX-28.

### SBX-16

- **Where:** `sidecar/internal/run/sandbox/policy.go`
- `TestTheLogDirectoryIsKubetails`, `TestTheConfigDirectoryIsKubetails`, `TestTheStateDirectoryIsKubetails`, `TestTheAppFolderIsKubetails` — Kubetail's log, config and state directories and the app folder are among its directories.
- `TestADeniedAlwaysPathStaysHiddenUnderAGrantedHome` — Kubetail's `app.db` stays shut under a real grant of a home.
- Note: the app folder holds several of Kubetail's directories on macOS and Windows.
- Note: the residual is the one SBX-15 states for a Deny on Linux. macOS holds a Deny whether or not its path exists.

### SBX-17

- **Where:** `sidecar/internal/run/sandbox/policy.go`
- `TestEveryResolvedFolderIsFenced` — each of Kubetail's directories is fenced at its resolved path.
- `TestLogsLinkedOutOfTheStateAreFencedAtTheirTarget` — a `logs` linked out of the state is fenced at its target.

### SBX-18

- **Where:** `sidecar/internal/run/sandbox/policy.go`
- `TestAKubetailFolderInsideAnotherIsDeniedOnce`, `TestANestedKubetailPathIsAcceptedAndDenied` — a Kubetail folder inside another is compiled with the one around it, and a nested one is still fenced.

### SBX-19

- **Where:** `sidecar/internal/run/sandbox/policy.go`
- `a_root_holding_dotdot_is_refused`, `a_refused_root_fails_the_launch`, `a_refused_state_root_fails_the_launch_on_linux`, `a_runtime_root_holding_dotdot_is_refused` — the host refuses a root holding `..`, by its name alone.

### SBX-20

- **Where:** `sidecar/internal/run/sandbox/policy.go`
- `TestResolveFoldersLogsInTheLayoutsLogFolder` — the sidecar logs only in the layout's log folder.
- `TestMainTakesTheFoldersOfTheProfileItIsPassed` — the sidecar takes the host's profile.

### SBX-21

- **Where:** `sidecar/internal/run/sandbox/policy.go`
- `TestConfigFromArgsPlacesTheFoldersFromTheEnvironment`, `the_sidecar_is_handed_no_folder_but_the_runtime_directory` — the sidecar places and fences every folder from the same roots.
- `each_platforms_layout_is_the_fixtures`, `TestEachPlatformsLayoutIsTheFixtures` — a shared fixture holds the sidecar's layout to the host's.

### SBX-22

- **Where:** `sidecar/internal/run/sandbox/policy.go`
- `TestAUnixRootHoldingDotDotIsRefused`, `TestConfigFromArgsRefusesARootHoldingDotDot` — the sidecar refuses a root holding `..`.
- `TestARootThatCannotBeReadIsAnError`, `TestLinuxReadsHomeOnlyForARootUnderIt` — it refuses a relative `HOME` a root falls back to.
- `TestConfigFromArgsRefusesARuntimeDirHoldingDotDot`, `TestConfigFromArgsRefusesAnOverrideHoldingDotDot` — it refuses a runtime directory or a debug build's data or cache override holding `..`.
- Note: `make clean-desktop-data` refuses a `HOME`, XDG or `TMPDIR` value holding `..` likewise.

### SBX-23

- **Where:** `sidecar/internal/run/sandbox/policy.go`
- `TestAPolicyChecks` — refuses a rule inside an Always path or beneath a Write rule.

### SBX-24

- **Where:** `sidecar/internal/run/sandbox/policy.go`
- `TestADenyLinkedInAnotherCaseIsCompiled`, `TestAKubetailRootLinkedInAnotherCaseIsDenied`, `TestCheckRefusesAReadOnAKubetailRootLinkedInAnotherCase`, `TestOutsideDropsARuleInsideAPathLinkedInAnotherCase` — a link that spells a denied path or a Kubetail root in another case is still denied, macOS.

### SBX-25

- **Where:** `sidecar/internal/run/sandbox/policy.go`
- `TestAFolderLinkedInAnotherCaseIsBroad`, `TestAPathEntryLinkedInAnotherCaseIsBroad` — a folder that is the home or an app-data folder in another case is as broad as it, macOS.

### SBX-26

- **Where:** `sidecar/internal/run/sandbox/policy.go`
- `TestARuleTheKernelCannotNameIsRefused` — a policy naming a path the kernel cannot name is refused.

### SBX-27

- **Where:** `sidecar/internal/run/sandbox/policy.go`
- **Records:** [ADR](../adr/2026-10-04-the-login-shell-runs-in-the-sandbox.md)
- `TestTheLoginShellsPolicyAnswersAlike`, `TestTheSnapshotsShellReadsTheHome` — the snapshot's login shell reads the home, reads nothing of Kubetail's and writes only its scratch.

### SBX-28

- **Where:** `sidecar/internal/run/sandbox/policy.go`
- `TestADenyHoldsForAPathMadeLater` — macOS holds a Deny whether or not its path exists.

### SBX-29

- **Where:** `sidecar/internal/lib/realpath/realpath.go`, `sidecar/internal/lib/realpath/kernelpath_darwin.go`, `sidecar/internal/run/sandbox/policy.go`, `sidecar/internal/services/settings/folders.go`, `sidecar/internal/tools/internal/fileguard/grant.go`
- **Records:** [paths are compared as files](../adr/2026-11-22-paths-are-compared-as-files.md)

### SBX-30

- **Where:** `sidecar/internal/lib/realpath/realpath.go`, `sidecar/internal/lib/realpath/kernelpath_darwin.go`, `sidecar/internal/run/sandbox/policy.go`, `sidecar/internal/services/settings/folders.go`, `sidecar/internal/tools/internal/fileguard/grant.go`
- **Records:** [paths are compared as files](../adr/2026-11-22-paths-are-compared-as-files.md), [TODO](../TODO.md#security)
- `TestUnderFollowsLinks`, `TestALinkBeforeDotDotIsFollowedFirst`, `TestUnderComparesFilesNotNames`, `TestUnderHoldsAFolderInAnotherCase` — two paths are compared by the files they name, through a link, a `..` after one, a second name of one file or, on macOS, another case.
- `TestAPathEntryThroughALinkAndDotDotIsTheFolderSearched`, `TestAHiddenFileUnderAnotherNameStaysHidden` — Settings and the file tools compare a grant with the zones however each is spelled.
- Note: the TODO link is for what is not compared as files.

### SBX-31

- **Where:** `sidecar/internal/lib/realpath/realpath.go`, `sidecar/internal/lib/realpath/kernelpath_darwin.go`, `sidecar/internal/run/sandbox/policy.go`
- **Records:** [paths are compared as files](../adr/2026-11-22-paths-are-compared-as-files.md)
- `TestCanonicalSpellsALinksTargetAsTheDiskDoes`, `TestCanonicalSpellsAnAccentAsStored`, `TestCanonicalSpellsWhatItMayNotRead` — the kernel names each rule path for the profile, on macOS as the volume stores it.
- `TestARuleTheKernelCannotNameIsRefused` — a rule the kernel cannot name refuses the run.
- `TestAPolicyNamingOneFolderTwoWaysIsRefused` — a policy that names one folder two ways refuses the run.

### SBX-32

- **Where:** `sidecar/internal/run/sandbox/policy.go`
- **Records:** [paths are compared as files](../adr/2026-11-22-paths-are-compared-as-files.md)
- `TestAPolicyChecks` — refuses a rule beneath a Write rule of the same policy.

### SBX-33

- **Where:** `sidecar/internal/run/sandbox/probe.go`, `sidecar/internal/run/sandbox/sandbox_linux.go`, `sidecar/internal/run/sandbox/network_linux.go`, `sidecar/internal/run/sandbox/sandbox_darwin.go`, `sidecar/internal/tools/bash/bash_unix.go`
- `TestTheProbePolicyIsBounded`, `TestThePastaProbePolicyIsBounded` — a probe's policy build is bounded.
- `TestTheProbesCheckIsBounded`, `TestThePastaProbesCheckIsBounded`, `TestACommandWhoseCheckHangsIsRefusedWhenItsContextEnds` — the check is bounded.
- `TestADenialLookupThatHangsIsBounded`, `TestThePastaProbesDenialLookupIsBounded` — each denial's lookup is bounded.
- `TestACommandWhoseProfileHangsIsRefusedWhenItsContextEnds` — macOS.
- Note: a run's command build is its check, each denial's lookup and, with the internet, the host's resolv.conf.
- Note: the resolv.conf lookup has no test of its own.
- Note: the probes' scratch folder in `TMPDIR` is made before the build and removed after it.

### SBX-34

- **Where:** `sidecar/internal/tools/bash/bash_unix.go`, `sidecar/internal/run/sandbox`
- `TestARunWhoseSandboxHangsSaysWhichStopEndedIt`, `TestAStartErrorNamesTheFirstStop` — Bash says whether its deadline or a cancel ended the run.

### SBX-35

- **Where:** `sidecar/internal/run/sandbox`
- `TestBoundedRunsAtMostItsCapAtOnce`, `TestBoundedStartsNothingForAnEndedContext` — at most 16 workers run at once, and a run waits for one to end until its own context does.

### SBX-36

- **Where:** `sidecar/internal/run/sandbox`
- `TestACheckLeftBehindReadsACopyOfTheRun`, `TestAProbesBuildLeftBehindReadsItsOwnBwrap` — a worker left behind reads its own copy of the run and of the probed bwrap.

### SBX-37

- **Where:** `sidecar/internal/agent/chat/grants.go`, `sidecar/internal/run/session`, `sidecar/internal/services/settings/folders.go`
- **Records:** [path grants](../security/2026-10-04-path-grants.md)
- `TestFoldersForAnswersOnlyCheckedFolders` — a hand-edited `~` read-write and `/proc` read reach neither the sandbox nor `Read` and `Write`.

### SBX-38

- **Where:** `sidecar/internal/agent/chat/grants.go`, `sidecar/internal/run/session`, `sidecar/internal/services/settings/folders.go`
- `TestNoFolderAppliesWithoutASandbox`, `TestNoFolderAppliesWithoutASandboxThroughTheFileTools`, `TestNoSnapshotRefusesEveryFolder` — no folder applies where no sandbox confines the run.

### SBX-39

- **Where:** `sidecar/internal/agent/chat/grants.go`, `sidecar/internal/run/session`, `sidecar/internal/services/settings/folders.go`
- `TestASessionWithNoFoldersReadsNone`, `TestASessionWithNoFoldersGetsNone`, `TestAChatsFoldersNeverReachTheMonitor` — a session with no folders function reads none, as the monitor's does.

### SBX-40

- **Where:** `sidecar/internal/agent/chat/grants.go`, `sidecar/internal/run/session`, `sidecar/internal/services/settings/folders.go`
- **Records:** [path grants](../security/2026-10-04-path-grants.md)
- `TestALinkIsRefusedWithItsTarget` — a grant names its folder by its resolved path and a link is refused with its target.
- `TestAMovedGrantIsRefused` — a grant that moved is left out.

### SBX-41

- **Where:** `sidecar/internal/agent/chat/grants.go`, `sidecar/internal/run/session`, `sidecar/internal/services/settings/folders.go`
- `TestAPathEntryThroughALinkAndDotDotIsTheFolderSearched` — a `PATH` entry is compared as the folder the shell searches.

### SBX-42

- **Where:** `sidecar/internal/agent/chat/grants.go`, `sidecar/internal/run/session`, `sidecar/internal/services/settings/folders.go`
- **Records:** [path grants](../security/2026-10-04-path-grants.md)
- `TestAGrantIsListedWithItsRefusal`, `TestAFoldersReadThatFailsIsAnError` — a grant is checked whenever it is read, and a read of the grants that fails fails what needed them.

### SBX-43

- **Where:** `sidecar/internal/agent/chat/grants.go`, `sidecar/internal/run/session`, `sidecar/internal/services/settings/folders.go`
- **Records:** [path grants](../security/2026-10-04-path-grants.md)
- `grants the folder as typed, read or read and write, and clears once it is taken` — the path is sent as typed.
- `opens the form in place, empty and with both durations, and Cancel closes it` — the field starts empty.

### SBX-44

- **Where:** `sidecar/internal/services/settings/folders.go`, `sidecar/internal/run/sandbox/lists*.go`
- **Records:** [path grants](../security/2026-10-04-path-grants.md)
- `TestAFixedMountCannotBeGranted`, `TestTmpIsAFixedMountOnLinux` — no grant reaches a mount every run has its own of.
- `TestCheckFolderRefusesEachRule` — the folder check refuses each rule.

### SBX-45

- **Where:** `sidecar/internal/services/settings/folders.go`, `sidecar/internal/run/sandbox/lists*.go`
- **Records:** [path grants](../security/2026-10-04-path-grants.md)
- `TestAWriteGrantOnCodeThatRunsIsRefused` — a startup file at its resolved target included.
- `TestCheckFolderRefusesEachRule` — the folder check refuses each rule.
- `TestAWriteGrantOnAShellFolderIsRefusedWithoutPath`, `TestAWriteGrantOnATypedPathFolderIsRefused` — a folder of the `PATH` setting, typed or from the login shell, whatever the setting says of the shell's.
- `TestARunKeepsItsNoWriteAfterTheSettingChanges` — a run keeps its `NoWrite` after the setting changes.

### SBX-46

- **Where:** `sidecar/internal/services/settings/folders.go`, `sidecar/internal/run/sandbox/lists*.go`
- `TestAFolderCheckReadThatFailsIsAnError`, `TestAFailedFolderCheckReadEndsTheWatch` — a check whose read of the `PATH` fails is an error, never a refusal on the grant.

### SBX-47

- **Where:** `sidecar/internal/tools/bash/toolhome.go`, `sidecar/internal/tools/workspace.go`
- **Records:** [the sandbox's own environment](../security/2026-10-02-the-sandboxs-own-environment.md)
- `TestARunWritesOnlyItsOwnToolHome`, `TestALinkInTheToolHomeIsRefused`, `TestTheToolHomeIsPerChat` — a run writes only its own chat's tool home, and a link in the tool home is refused.
