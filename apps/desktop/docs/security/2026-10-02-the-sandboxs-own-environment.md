# Security record — the sandbox's own environment, 2 October 2026

**Subject:** what a sandboxed command reads and the environment it holds come from Kubetail's lists
and one table, never from the user's `PATH` or shell. This is step 2A of the build order for
[the sandbox, credentials and permissions note](../notes/sandbox-credentials-and-permissions.md).
The living model is [security-model.md](../security-model.md); the decision is
[the sandbox's zones are Kubetail's lists](../adr/2026-10-02-the-sandboxs-zones-are-kubetails-lists.md).

## What changed

Until now a `PATH` entry under the home opened a tree, an entry outside the lists opened its
folder, and every sandboxed command sourced the user's shell snapshot, whose last line set `PATH`
([Bash runs in a sandbox](2026-09-28-bash-runs-in-a-sandbox.md)).

- **A `PATH` entry opens nothing.** `Sandbox.System(home, shell)` reads the `System` list, each
  `Toolchain` folder that exists, the shell's folder and Kubetail's executable, and nothing else
  (`TestAFolderOnThePathIsNotRead`, `TestSystemReadsTheZones`). A toolchain folder that links to
  the home, a folder above it, or a folder of every app's data under it (`~/.config`, `~/.local`,
  `~/Library`) reads nothing, so a link cannot turn a location into a read of the home
  (`TestAToolchainLinkToABroadFolderIsNotRead`). The shell's folder is held to the same rule: a
  shell in the home, an app-data folder or `/` reads the program alone
  (`TestAShellInABroadFolderReadsOnlyItself`), and a shell that is a link reads where each link
  on its way leads, so it still runs (`TestAShellLinkReadsWhereItLeads`, `TestALinkedShellRuns`). A shell under the home reads its
  own folder alone, never a parent that holds other apps' data
  (`TestTheShellsPrefixIsReadButNeverTheHome`).
- **`/etc` is read whole and its secret files are not** (`TestEtcSecretsStayHidden`).
- **`Never` grows**: the tools' credentials, the shells' and REPLs' histories, the container
  sockets, the browsers' profiles, `/root`, and every other user's home, read at call time
  (`TestNeverHoldsTheNotesList`, `TestTheOtherHomesAreNever`). A path on or above the user's home
  is left out, so a home of `/root` works (`TestANeverPathAboveTheHomeIsDropped`), and so is an
  entry that holds the user's home, so a nested home keeps its own reads
  (`TestANestedHomeIsNotAnotherUsersHome`). Listing the
  other homes can hang on a network mount, so it runs under the run's bound and the probe's
  (`TestTheEnvironmentIsBuiltOnThePolicyGoroutine`, `TestTheProbePolicyIsBounded`).
- **`~/Documents`, `~/Desktop` and `~/Downloads` are `Closed`**: a Read of the home leaves them
  shut, and a Read of a folder inside one opens it (`TestAGrantOfTheHomeLeavesClosedFoldersShut`).
  Nothing grants a folder yet, so this binds once step 4D does.
- **The environment is one table.** `sandboxedRunEnv` copies `PATH`, `LANG` and `TZ` from the
  sidecar's and builds the rest. `LC_*` no longer passes (`TestTheSandboxedEnvironmentIsFixed`).
- **What may never pass is one list**, `sandbox.NeverEnv`, and both platforms' `Command` refuse a
  run holding a name on it (`TestARunWithAnUnpassableVariableIsRefused`,
  `TestARunsCheckRefusesAnUnpassableVariable`).
- **A tool's writes under the home go to the chat's tool home**, `<chat>/toolhome/`, through its own
  variables. It is made through the chat's root, a link at any level refused
  (`TestALinkInTheToolHomeIsRefused`), and a run writes its own chat's alone
  (`TestARunWritesOnlyItsOwnToolHome`).
- **A sandboxed command neither sources nor waits for the snapshot**
  (`TestASandboxedRunSourcesNoSnapshot`, `TestASandboxedRunDoesNotWaitForTheSnapshot`). On a
  machine with a sandbox the snapshot is taken the first time a command runs outside it
  (`TestTheSnapshotIsTakenOnTheFirstRunOutside`), and never once the sidecar is stopping
  (`TestNoSnapshotStartsAfterTheStop`, `TestTheStopReapsASnapshotStartedLate`).

## The bound

What a sandboxed command reads is the policy Kubetail builds from its lists. No startup file, no
`PATH` entry and no variable of the user's widens it. The environment a run holds is the table,
and a name on `NeverEnv` refuses the run on both platforms whoever built it.

## What widens

- The toolchain folders are readable whether or not `PATH` names them.
- `~/.rustup` is a toolchain location, with `RUSTUP_HOME` pointing at it, since `~/.cargo/bin`
  holds only rustup's proxies. `CARGO_HOME` is the tool home's `cargo` folder, so cargo's registry
  and config are per chat and `~/.cargo` beyond its `bin` stays unread.
- `/snap` (Linux), `/nix/store` (macOS), `/nix/var/nix/profiles` and `/run/current-system`
  (both) join `System`.
- The tool home is writable. It is per chat, and those writes landed in the per-chat workspace
  before, so no chat reads what another wrote.

## Residuals

- **A toolchain folder can hold a secret Kubetail does not know of.** mise's `config.toml` may carry
  `[env]` values, and nvm's per-version `npmrc` may carry a registry token; a run reads both.
  Settings will show the list (step 7A).
- **The kubectl discovery cache is shared by every chat on a cluster**, as before: one chat can
  plant a discovery document a later chat's `kubectl` reads.
- **A Kubetail directory inside a `Never` path fails every sandboxed run.** The chat's switch is the
  way out.
- **Until step 3A, a `PATH` entry no list covers finds nothing**, under the home or outside it,
  Flatpak's `/var/lib/flatpak/exports/bin` among them, and a Linux sandbox's `PATH` is the
  desktop session's, without what a `.bashrc` or `.zshrc` adds. The chat's switch is the way out.
  Step 3A reaches users in the same release, so no release ships this loss.
