# Security record — the settings move into app.db, 25 November 2026

**Subject:** every setting the sidecar kept in `settings.json`, and a chat's grants in
`chat_grants`, moved into `app.db` (migration `0006`): the approval modes, the cloud platform
switches, the tool settings and the sandbox's `PATH` as rows of `settings`, one per key and scope;
the rules in `kube_rules`; the folder grants in `folder_grants`; the login shell's last `PATH` in
`shell_path`. The decisions are
[ADR: keep every setting as rows in app.db](../adr/2026-11-24-settings-are-rows-in-app-db.md),
[ADR: a folder grant is its own row, and a mode and a rule belong to a cluster](../adr/2026-11-24-a-folder-grant-is-its-own-row-and-a-mode-and-a-rule-belong-to-a-cluster.md)
and [ADR: a sandboxed command's PATH is one setting the user writes](../adr/2026-11-24-the-sandbox-path-is-one-setting-the-user-writes.md).
The living model is [security-model.md](../security-model.md).

## What it changes

### Permissions

- A rule's read-back was the file's checks, and a value they refused held the whole field: no
  `allow`, plus `permissions.Refused` and `RefusedSecrets`. It is now `kube_rules`' `CHECK`s,
  which refuse each shape Kubetail cannot apply before it is stored, so a stored rule always applies
  and no rule holds another back.
- A failed read of a chat's rules was `permissions.Refused` alone, which left Secret reads to the
  mode. It is now `permissions.Unread()`, `RefusedSecrets` included, as a failed read of the
  always rules is.
- A folder grant is no rule. Nothing about the rules can close it any more, and its own check,
  against the zones and the `PATH`, still runs on every read.
- A mode and a rule are keyed by the cluster's record, never its context's name. Deleting a
  cluster deletes its mode and its rules, and a context added again under an old name inherits
  no `allow`. A rule may name no cluster, and then applies to every cluster.
- No `allow` names every cluster: one would reach a cluster added after it.

### The sandbox's `PATH`

- **`sandboxPathSet` is a grant path.** A folder the user types into the setting is readable to
  every sandboxed run: a read grant made by typing, beside Settings' folder grants. Include, a
  click on a drawn folder, is gone.
- Nothing is stored per folder. A shell folder is judged afresh at every run, so the sync, its
  tombstones and its `strict` flag are gone, and so is the gap they left: a removal no longer
  needs to outlive a store that cannot read it.
- Remove of one shell folder is gone. Writing a value without `$PATH` drops every shell folder
  from the search.

### Single-value settings

- **A row of `settings` has no `CHECK` on its value.** A stored value Kubetail cannot use is
  refused at the read: a switch or a tool reads off, a mode read-only.
- **A missing row is the default, not a failed read**: every switch on, the `PATH` `$PATH`.
  Whoever can delete a row in `app.db` can also write one.
- **A value Kubetail cannot use is logged by key and reason, never by value**, and is not listed in
  Settings. Choosing there writes over it.

## What holds

### Permissions

- **Every shape Kubetail refuses is a `CHECK`**: the two ids Kubetail keeps, an `allow` of a
  destructive write, an `allow` naming no cluster, a class no rule decides, and a Secret read
  rule naming more than a Secret read (`TestMigration0006AddsTheSettingsTables`). The Go check
  refuses the same rules in the user's words (`TestTheGoCheckAndTheTableRefuseTheSameRules`,
  `TestARuleKubetailCannotApplyIsRefused`).
- **A failed read never permits.** Rules read as `Unread()` (`TestAGrantsReadThatFailsRefuses`,
  `TestAPolicyThatCannotBeReadRefuses`, `TestUnreadRulesRefuseWritesAndSecretReads`), a mode as
  read-only (`TestAPolicyThatCannotBeReadRefuses`), and the folder grants as an error that fails
  the run, the file tool or the send (`TestAFoldersReadThatFailsIsAnError`,
  `TestAFoldersReadThatFailsFailsTheRun`, `TestAFoldersReadThatFailsFailsTheCall`).
- **A mode belongs to a cluster Settings lists.** `permissionModeSet` refuses a cluster that is
  gone, awaiting deletion or has no kube-context (`TestAModeIsRefusedForAClusterSettingsDoesNotList`,
  `TestAPermissionRefusalCarriesItsCode`). A cloud platform's request still always asks: no
  mode reaches it.
- **A rule names its cluster by id, and goes with it.** `kubeRuleAdd` refuses a cluster as
  `permissionModeSet` does (`TestARuleNamesAClusterSettingsListsAndGoesWithIt`), a rule matches
  only its own cluster's actions, whatever their context is named
  (`TestAGrantRuleNamesTheActionsCluster`), and an approval's rule names the action's cluster
  (`TestAChatAnswerWritesAClassSixGrant`).
- **A folder grant is still checked where it is read**, and a stored one that fails is listed
  with its refusal and given to no run (`TestAGrantIsListedWithItsRefusal`,
  `TestFoldersForAnswersOnlyCheckedFolders`).
- **Each row has one writer.** `services/settings` writes the rows whose `chat_id` is NULL,
  `agent/chat` a chat's; a chat's rows go with the chat (`TestAChatsGrantsGoWithTheChat`).

### The sandbox's `PATH`

- **A startup file opens nothing.** The value is split before it is expanded, and the shell's
  answer is spliced in already split, so no folder it lists is ever a typed one
  (`TestTheShellsPathIsNeverTyped`). A shell folder is searched only when every run reads it
  already and no group but an administrators' one can write it
  (`TestAShellFolderIsSearchedOnlyWhenOpen`). A startup file still chooses among open folders, and
  `logToolMoves` still logs when it moves `kubectl` or `helm` (`TestAMovedToolIsLogged`).
- **A typed folder is held to a folder grant's checks.** A link or another spelling is left out,
  naming the folder to type instead (`TestATypedLinkIsLeftOut`,
  `TestATypedFolderInAnotherCaseIsLeftOut`); the denied-always list, Kubetail's directories, a closed
  folder itself and any broad, world-writable or project folder are left out
  (`TestAFilteredFolderIsLeftOut`, `TestTheRunLeavesOutAFolderNoRuleOpens`,
  `TestTheRunLeavesOutABroadFolder`). A folder inside a closed one opens that folder alone
  (`TestATypedFolderInsideAClosedOneIsOpened`).
- **The grammar expands only `$PATH`, `~` and `$HOME`**, and refuses `..`, a relative path, the
  bare home and an empty element, which a shell would search as the working directory
  (`TestCheckPathRefusesEachBadElement`). `~` is the zones' home, never a value from the shell.
- **No read-write grant reaches a `PATH` folder**: every folder of the shell's, whatever the
  value, and every typed one, in Settings and at the run
  (`TestAWriteGrantOnAShellFolderIsRefusedWithoutPath`,
  `TestAWriteGrantOnATypedPathFolderIsRefused`). A run reads its search and its grant check from
  one read (`TestARunKeepsItsNoWriteAfterTheSettingChanges`).
- **A read that fails never permits.** A run whose setting or shell `PATH` cannot be read, or whose
  setting Kubetail cannot use, does not start (`TestARunWhosePathCannotBeReadDoesNotStart`,
  `TestARefusedPathStartsNoRun`, `TestARunPathReadThatFailsIsAnError`); a folder check whose read
  fails is an error, never a refusal on the grant (`TestAFolderCheckReadThatFailsIsAnError`,
  `TestAFailedFolderCheckReadEndsTheWatch`).
- **`shell_path` is a copy.** It holds what the shell answered, and every folder of it is checked
  at each run, so a stale one grants nothing. A failed or empty read keeps it
  (`TestAFailedShellReadKeepsTheLastPath`, `TestAnEmptyShellPathKeepsTheLastRow`).
- **Settings shows what the user saves.** Add copies a folder from the login shell's `PATH`, never
  from command output, into the field, and saves nothing; a draft holding a hidden character is
  drawn through `VisibleText` before Save.

### Single-value settings

- **No value Kubetail cannot use grants anything.** A switch reads off
  (`TestASwitchValueKubetailCannotUseIsOff`), a mode read-only, in a cluster's row or the default's
  (`TestAModeKubetailCannotUseIsReadOnly`), a tool off in Settings and in a turn's offer
  (`TestARowKubetailCannotUseTurnsItsToolOff`, `TestATurnIsNotOfferedAToolWhoseSettingCannotBeUsed`),
  and the log names no value (`TestASettingKubetailCannotUseReadsItsStrictestOrDefault`).
- **A `PATH` Kubetail cannot use starts no run.** A refused value and one that is not text are read
  with their refusal, never as `$PATH` (`TestARefusedPathSettingIsReadWithItsRefusal`,
  `TestAPathSettingThatIsNotTextIsReadWithItsRefusal`, `TestARefusedPathStartsNoRun`).
- **A failed read never permits.** The switches read off at a run's start and at a grant, a mode
  read-only, and a failed read of the tools refuses the send
  (`TestACloudPlatformSwitchReadThatFailsIsAnError`, `TestAPolicyThatCannotBeReadRefuses`,
  `TestASendThatCannotReadTheToolSettingsIsRefused`).

## Residual risk

- **A rule's words name its cluster by the record's kube-context**, which two clusters from two
  kubeconfig files can share. The rule still matches one cluster alone; Settings names each
  cluster by its name and context in the form.
- **A typed folder is readable to every run**, as a read grant is. Its checks stop the folders
  Kubetail knows to hold secrets, not a folder the user names that holds one.
- **A typed folder is checked again at each run, by its name.** A link put in its place is left
  out; a folder renamed and another made under the old name is read.
- **A single value Kubetail cannot use is not shown.** A cluster whose stored mode Kubetail cannot
  read shows read-only in Settings as if the user chose it. It fails safe, and choosing a mode
  repairs it.
- **An install of `desktop/v0.0.1` loses its settings**, by decision: its modes, rules, folder
  grants, executables, `PATH` decisions and chats' grants. A cluster the user set read-only becomes
  `ask` until they set it again, deny and ask rules go too, an included folder the sandbox does
  not already read must be typed again, and a removed one that is open is searched again. The
  release notes must say so.
