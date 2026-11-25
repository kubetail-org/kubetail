# Security record — path grants, 4 October 2026

**Subject:** the user grants a folder, read or read-write, for a chat or always, and a sandboxed
command, `Read`, `Write` and `Edit` reach it without a request. This is step 4D of the
agent-security sequence. The living model is [security-model.md](../security-model.md); the
decision is [a folder grant is a rule, and the file tools walk it by handle](../adr/2026-10-04-a-folder-grant-is-a-rule.md).

## What widens

Before this step an unasked sandboxed command read nothing of the user's beyond the system and
the toolchain folders, and wrote only its workspace, its `TMPDIR`, its tool home and the kubectl
cache. After it, a command reads and writes the folders the user granted, for the chat or always,
and so do `Read`, `Write` and `Edit` without a request.

## What holds it

- **The Always part is the floor.** No grant opens a credential path, a private folder or
  Kubetail's directories, Kubetail's log directory among them: a grant is a Files rule, and the
  Always part outranks every Files rule. Pinned over a real grant on both platforms
  (`TestADeniedAlwaysPathStaysHiddenUnderAGrantedHome`), with `TestTheDeniedAlwaysListWinsOverARead`
  for the policy and `TestTheLogDirectoryIsKubetails` for the log directory.
- **A grant is the user's own act.** It is written in Settings or through `folderGrant`, never by
  the model; no command asks for one and no folder rule reaches a verdict
  (`TestAFolderRuleMatchesNoAction`).
- **A grant names its folder by its canonical path.** A link on it or on its way is refused with
  its target, so the folder the user reads is the folder granted (`TestALinkIsRefusedWithItsTarget`,
  `TestAFolderRefusalCarriesItsReason`), and so is a spelling the disk does not list, since a
  case-insensitive filesystem opens `/Users/ME` as the home while every zone is compared by text
  (`TestAFolderSpelledInAnotherCaseIsRefused`). The zones and the `PATH` folders are canonical too.
- **A closed path is compared at the target it has now.** The denied-always list and the closed
  folders are kept as listed and resolved on every check, every gate and every walk, so a never
  path that is a link (`~/.ssh` → `~/dotfiles/ssh`) retargeted after the last sync hides its new
  target and not its old one (`TestARetargetedNeverPathStaysHidden`,
  `TestACheckResolvesTheZonesNow`). The gate's resolving runs on a goroutine the turn's cancel
  abandons, so a dead network mount holds no turn
  (`TestGrantedAnswersTheCancelWhileResolvingBlocks`).
- **A grant is checked when written, every time a session reads it, and again at every run.**
  `foldersFor` is the one builder of a session's folders and answers only folders that pass, so a
  hand-edited rule — `~` read-write, `/proc` read — reaches neither the sandbox nor the file tools
  (`TestFoldersForAnswersOnlyCheckedFolders`), and one that resolves anywhere but where it did is
  left out (`TestAMovedGrantIsRefused`, `TestAGrantThatFailsTheCheckIsLeftOut`). The store's
  read-back checks the value alone (`TestAFolderRuleIsShapeChecked`), and a rules field Kubetail
  cannot read grants nothing (`TestHeldRulesGrantNoFolder`).
- **No folder applies where no sandbox confines the run**: Windows, a Linux with no usable
  sandbox, a failed probe, a chat switched outside it (`TestNoFolderAppliesWithoutASandbox`,
  `TestNoFolderAppliesWithoutASandboxThroughTheFileTools`). A service with no zones refuses every
  folder (`TestNoSnapshotRefusesEveryFolder`), and a session with no `Folders` reads none
  (`TestASessionWithNoFoldersGetsNone`, `TestASessionWithNoFoldersReadsNone`).
- **No grant reaches a mount every run has its own of**: `/proc`, `/dev` and `/tmp` on Linux,
  `/dev` on macOS (`TestAFixedMountCannotBeGranted`, `TestTmpIsAFixedMountOnLinux`).
- **No read-write grant reaches what Kubetail knows runs outside the sandbox**: a `PATH` entry the
  list stores or the login shell answered, whatever its state, a system or toolchain Read path,
  and `sandbox.NoWrite` — the shells' startup files and folders, launchd's agents, systemd's units,
  at their resolved targets (`TestAWriteGrantOnCodeThatRunsIsRefused`,
  `TestCheckFolderRefusesEachRule`). The home is read-only.
- **A file tool walks a granted path by handle from `/`.** The grant's own folder is checked like
  every other, a link inside the grant is followed only while it stays inside, and each handle is
  compared with the closed paths, so a link — swapped in or not — never leads it into a closed
  path (`TestTheWalkChecksTheGrantItself`, `TestTheWalkRefusesALinkSwappedIn`,
  `TestTheWalkRefusesWhatTheSandboxKeepsShut`, `TestTheWalkRenamesOnTheHandle`,
  `TestALinkToAHiddenPathUnderAGrantedHomeIsRefused`, `TestAWalkThroughAClosedFolderIsRefused`),
  and a folder missing when the walk ends is never read through, since a link made there after
  the walk would be followed past those checks (`TestAnAncestorMadeAfterTheWalkIsNotFollowed`). A
  closed path under a grant is never skipped, and asks as before
  (`TestAHiddenPathUnderAGrantedHomeStillAsks`, `TestGrantedComparesResolvedPaths`). The run takes
  the folder its approval decided on and never reads the grants again
  (`TestTheRunTakesTheGatesApproval`, `TestARevokeBetweenApprovalAndRunChangesNothing`).
- **A grant is seen**: in Settings, under the composer's *Allowed for this chat*, and in the
  question's context (`TestTheContextListsTheGrants`). An unasked write is drawn open in the
  transcript, as a workspace write is.

## Residuals

- A granted folder's contents leave the machine on the model's word, as cluster reads do: a
  repository with a `.env` in it is readable once its folder is granted, and the denied-always list
  names credential paths under the home, not inside a project.
- A read-write grant lets a command plant a file a later command outside the sandbox or a `git`
  hook runs. A repository also runs code with no command of the user's: an IDE's `git status` runs
  a `core.fsmonitor` from `.git/config`, and direnv runs an `.envrc` on `cd`. `NoWrite` closes the
  places Kubetail knows of; a file a startup file sources from elsewhere (`source ~/work/env.sh`) is
  open to a read-write grant, and through the login shell's snapshot it shapes every command
  outside the sandbox and every credential plugin.
- The denied-always list is curated, so a secret in a folder it does not name (`~/.pgpass`, a
  password store, a browser profile it misses) is readable under a grant of `~`. Settings warns of
  it beside a grant of the home.
- A folder granted always is readable by every chat.
- On macOS a read grant of `~` opens the host's own settings folder (`host.json`) and its WebKit
  storage, neither holding a credential or cluster data.
- A grant revoked while a command runs holds for that command until it exits, a background one
  included, since its sandbox was built when it started.
- On Linux a denied-always path under a granted folder that does not exist when a run starts is not
  mounted over, so a file something outside the run makes there while it runs is readable to it;
  macOS holds the Deny whether or not the path exists.

## What closes

The TODO item *Revisit how little of the home a sandboxed command reads*: this is the allow-list
it asked for, kept in `settings.json` rather than `host.json`.
