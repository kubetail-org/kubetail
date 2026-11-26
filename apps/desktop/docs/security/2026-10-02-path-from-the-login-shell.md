# Security record — `PATH` from the login shell, 2 October 2026

**Subject:** a sandboxed command's `PATH` is the user's login shell's, filtered and frozen in
`settings.json`, and it is the run's Read rules too. The living model is
[security-model.md](../security-model.md); the zones are recorded in
[the sandbox's own environment](2026-10-02-the-sandboxs-own-environment.md).

## The problem

A sandboxed command's `PATH` was the sidecar's own: on macOS what `loginshell.Import` read from
`$SHELL` at launch, on Linux whatever the process started with. A line in `~/.zshrc` changed what
every sandboxed command found at the next launch, and nobody was told. A dev run from a terminal
and a Finder launch found different tools, and an entry no list covered found nothing.

## What changed

- **One run of the account's login shell at launch.** The shell is the account record's
  (`dscl` on macOS, `getent` then `/etc/passwd` on Linux), never `$SHELL`. It runs with a fixed
  environment: the identity, locale and agent sockets copied, `TERM=dumb`, and `PATH` at the
  platform's login default. The sockets keep a startup file that starts an agent when none is set
  from leaving a new one behind at every launch. On macOS the same run gives the allowlisted environment it gave before; on Linux it is
  a new run. Both are bounded by 10 seconds.
- **The list is filtered.** Empty, relative, missing, world-writable and `node_modules` entries
  go, and so does anything on or under the denied-always list or Kubetail's directories, and `/`,
  the home, `~/.config`, `~/.local`, `~/.local/share`, `~/Library` or a folder holding one. Every
  check is on the folder the entry resolves to.
- **The list is frozen.** At each launch the fresh list is diffed against the stored one. A new
  entry adopted unasked is one already open to every run and not writable by a non-admin group.
  Every other new entry waits for the user. A removal is kept as `gone`.
- **A run reads the list once.** It checks each entry again (`RunCheck`) and searches the folders
  that pass, which are also its Read rules. A folder the system list already reads gets no rule.
- **The user sees it** in Settings, with Include, Remove and Refresh PATH.

## Why this is safe

- **Only an entry already readable to every run is adopted unasked.** Adopting it adds no
  readable surface (`TestSyncPathDiffsFourWays`).
- **A world-writable folder never gets in**, at the sync or at the run
  (`TestSearchFolderRefusesWhatARunCannotSearch`, `TestTheRunLeavesOutAnEntryNoRuleOpens`), and neither does one
  whose name holds the `PATH` separator, which a joined `PATH` would split into other folders
  (`TestSearchFolderRefusesWhatARunCannotSearch`), or one that is a mount every Linux run has,
  whose Read rule would stop every run from starting (`TestSearchFolderRefusesAFixedMount`).
- **No entry opens the home whole.** `/`, the home, an app-data folder under it, and any folder
  holding one are dropped at the sync and left out at the run, so one Include cannot make the
  whole home readable (`TestSearchFolderRefusesWhatARunCannotSearch`,
  `TestSyncPathDropsABroadEntry`, `TestTheRunLeavesOutABroadEntry`).
- **A folder another group can write waits** (`TestAnAdoptedEntryThatBecameSharedWaits`), and
  every sync redraws whether a folder is shared, so Include is never offered without the warning
  (`TestSharedFollowsAnUnmovedFolder`).
- **Include approves the folder the user saw.** It carries the target drawn, and one a refresh has
  moved since is refused (`TestIncludeApprovesTheTargetShown`).
- **The denied-always list and Kubetail's directories filter twice**: at the sync, and at the run
  against the run's own `Always` paths, so a stored list older than a new denial still passes
  `Policy.Check` (`TestTheRunLeavesOutAnEntryNoRuleOpens`).
- **An approval holds for the folder it resolved to.** A link repointed after the sync grants
  nothing new: the run leaves it out unless the new folder is one the sync would adopt unasked
  (`TestTheRunLeavesOutAMovedEntry`, `TestTheRunFollowsALinkIntoAnOpenFolder`).
- **A removal holds** while the entry is off the shell's `PATH`
  (`TestARemovalOutlivesTheEntrysAbsence`), and through a hand edit the store cannot read: the
  store keeps the field's raw JSON, refuses a write that would drop it unnamed, and the next sync
  adopts nothing new unasked, across a restart too, since the strictness is stored
  (`TestARefusedEntryHoldsTheSync`, `TestARemovalWhileHeldKeepsTheSyncStrict`,
  `TestARemovalWhileHeldStaysStrictAcrossARestart`).
- **Nothing a run does changes the list.** Only the sidecar's socket reaches the mutations.
- **A removal is not undone by a default.** Only a list never resolved searches the platform's
  default folders, each under the same checks. Whether a sync has run is stored, never read off the
  list's length: a resolved list that is empty, or whose entries all fail, searches nothing
  (`TestTheRunFreezesThePath`, `TestAnEmptyListSearchesTheCheckedDefault`,
  `TestASyncMarksTheListResolved`, `TestRefusedPathMarksAnswerTheirSafeValue`). A hand-written
  `null` is refused like any wrong value, so it answers the safe value too (`TestANullIsRefused`).
- **A sync's reading of the disk is bounded**, at launch and on refresh (`TestARefreshIsBounded`).

**One rule decides what a run may search.** `sandbox.SearchFolder` holds every requirement a
run's own policy makes of a `PATH` folder, beside the code that enforces them, and the sync and
the run both call it. A table test walks every combination of an entry's state, the folder it
leads to, the shell's answer, the stored marks and the user's action, and checks these invariants
on each (`TestEveryStateCombinationKeepsTheInvariants`).

## What widened

An entry the user includes is a Read rule. A folder the lists do not open — inside the home, in a
closed folder, a closed folder itself, or outside every open folder — becomes readable to every
sandboxed run, with everything under it (`TestAnIncludedClosedFolderIsOpened`). Only Include opens
one, no Always path is ever opened, and no broad folder is ever one.

## Residuals

- **The login shell runs unconfined** until step 4A, as it did before on macOS.
- **A startup file chooses which `kubectl` runs** among the open folders, by putting one first.
  The sync logs the move once.
- **A program in an adopted folder that links into a folder no rule opens** is found but cannot
  run. Step 6A's probe names the denial.
- **A command outside the sandbox can edit a startup file**, as it can edit anything of the
  user's.
- **A folder an administrators' group can write is adopted unasked.** Another administrator, or a
  process running as one, can plant a program in it without a password. On a machine with one
  administrator, the common case, that is the user.
