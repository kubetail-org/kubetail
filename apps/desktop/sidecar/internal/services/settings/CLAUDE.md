# internal/services/settings — the user's settings

**Every setting is a row in `app.db`.** `Open(db)` prepares the store's statements. A failed read
is an error, never the default. A setting with structure has a table of its own, where a row
Kubetail cannot use is refused and listed with its reason, never dropped. → [ADR: settings are rows in app.db](../../../../docs/adr/2026-11-24-settings-are-rows-in-app-db.md).

Every data access here is named for its contract: [`docs/data-access.md`](../../../../docs/data-access.md).

**A single-value setting is a row of `settings`** (`scalar.go`): a dotted key, a scope
(`cluster_id` NULL for every cluster) and a `value` of any kind. It belongs there when it is one
value read and written whole, a value Kubetail cannot use can cost a permission but never grant one,
and the code names it. An `Entry[T]` is its key, default, strictest value, whether a cluster may
have its own, and check; `T` is how it is stored (a bool as 0 or 1, an int64, a string, else JSON
that refuses an unknown field). No row, or a NULL, is the default; a reset writes NULL; a row goes
only with its cluster. A cluster's value is its own row, else every cluster's, else the default
(`readSetting`). A write runs the check first (`SettingRefusal`). A stored value Kubetail cannot use
reads as the strictest value, else the default, logged by key and reason, never by value; a row no
entry names is kept and ignored. **Adding a setting** is an entry, its typed methods, its wire
field and its control: no migration, no statement. `tools.go` exports the tools' read and write,
for `toolsettings` and `agent/chat`. → [ADR](../../../../docs/adr/2026-11-24-settings-are-rows-in-app-db.md),
[security record](../../../../docs/security/2026-11-25-settings-in-app-db.md).

**The modes, the always rules and folder grants are `app.db` rows**: `permission_modes` by target
and account, `permission_rules` and `folder_grants` with `chat_id` NULL (a chat's are
`agent/chat`'s), in `permissions.go`, `store.go` and `statements.go`. Each write is one transaction, then
notifies `appdb.KeyPermissions` (`ListenForPermissionChanges`). → [ADR](../../../../docs/adr/2026-11-25-permissions-are-access-levels-a-boundary-modes-and-rules-over-one-action.md),
[security record](../../../../docs/security/2026-11-25-target-neutral-permissions.md).

- `ModeFor` is the account's row, else the target's `*` row, else `ask`; one Kubetail cannot use
  is read-only. `SetMode` and `ClearMode` take a listed cluster's id
  (`ErrClusterGone`, `ErrNoKubeContext`), a cloud account with no `/`, or `*`; else a `ModeRefusal`.
  `canonicalScope` lowers a cloud account or scope, as classifiers do.
- `CheckRule`, run on every write and read, refuses with a `RuleRefusal` what no rule may hold; a
  stored row it refuses reads as `permissions.Unread()` under its id. `AddRule` and
  `UpdateRule` check a cluster as `SetMode` does and answer a rule already held.
  `RuleColumns`, `ScanRules`, `RuleValues` and `FolderGrant*` are the encoding `agent/chat` shares.
- `GrantFolder` upserts on the folder, keeping its id; its caller checks the folder.
- A failed read is an error.

**The `PATH` is one string, the `sandbox.path` setting** (`path.go`), written as in a shell's
startup file, `$PATH` by default. `parsePath` splits it before expanding: each element is exactly
`$PATH`/`${PATH}` or a typed folder (absolute, `~/`, `$HOME/`, `${HOME}/`); `CheckPath` refuses the whole
value, naming the element, for `$PATH` mixed with text, any other `$`, a relative path, `..`, the
home itself, an empty element or a NUL. **`ExpandPath` splices the shell's `PATH` in already split**,
so nothing it lists is ever typed, and joins a typed folder to the canonical home. A `PathFolder`
carries its `Origin` (`OriginShell`, `OriginTyped`). `SetPath` upserts it and notifies
`appdb.KeySandboxPath` (`ListenForSandboxPathChanges`); a stored value the grammar refuses is read with its
`Refusal`, one that is not text as an empty value with its own. → [ADR](../../../../docs/adr/2026-11-24-the-sandbox-path-is-one-setting-the-user-writes.md),
[security record](../../../../docs/security/2026-11-25-settings-in-app-db.md).

**`Service` (`service.go`) holds the login shell's last `PATH`** in `shell_path`, a copy no row of
which is refused, over `Zones` (`Never`, `Open`, `Home`), the refresh's resolver, the launch's
fault and the platform's default folders (`$PATH` with no row). On a machine with no sandbox its
path methods answer `ErrNoSandbox`. **`SetShellPath`**, one at a time and bounded by
`DiskTimeout`, retakes the zones' snapshot, then stores the `PATH`, and pings `KeySandboxPath`
whether or not it changed; an empty one is `ErrEmptyShellPath`. A change logs a tracked tool's
move (`logToolMoves`, after the write). `RefreshPath` runs the shell, then `SetShellPath`; a read
that fails or answers no `PATH` keeps the last and sets the fault. **`RunPath` reads both rows in
one read transaction**: `Search`, the value expanded, and `NoWrite`, every shell folder whatever
the value says plus each typed one; a refused value is a `PathRefusal`, so no run starts.
`PathNoWrite` is `NoWrite` alone; `PathFolders` is the setting and each folder judged, for
Settings, bounded by `DiskTimeout`.

**`RunCheck.Folders` (`checkrun.go`) is the one rule** a run and Settings both read. Every folder
passes `filterDir` (`filter.go`: `sandbox.SearchFolder`, `node_modules`; `Shared` marks a folder
a non-admin group can write). A shell folder is searched only when open (under an `Open` Read
path and no Deny path, as files) and unshared; a typed one when `checkFolder` passes it for a
read, unstored, so a link or another spelling is left out naming the folder to type. A folder
already searched is not searched again, compared as files. `Addable` marks a shell folder left out
only for being closed or shared, that no typed folder covers, whose target could be typed.

**A folder grant is checked against the zones** (`folders.go`): a `FolderGrant`, read or
read-write, on a folder. `CheckFolder` answers a `FolderRefusal`; the
rules are in the function and `TestCheckFolderRefusesEachRule`. **A grant is its canonical
path** (`realpath.Canonical`), **and is compared with the zones and `PATH` as files**
(`realpath.Under`, `realpath.Same`), never by text: macOS opens `/Users/ME` as `/Users/me`. **The
service holds the zones as a snapshot** taken by each `SetShellPath`, or on first use; checks
re-resolve its paths but never re-run the zones function. They read `PathNoWrite` for the `PATH`;
a read that fails is an error, never a `FolderRefusal`. Every disk read goes through `readWithin`, bounded by
`DiskTimeout`. With no sandbox every folder is refused. → [ADR: a folder grant is
checked](../../../../docs/adr/2026-10-04-a-folder-grant-is-a-rule.md), [security
record](../../../../docs/security/2026-10-04-path-grants.md).

**The registered executables are `executables` rows** (`executables.go`), probed beside
`CuratedExecutables`. `CheckExecutable` checks a register and every read: a refused row is listed
with its `Refusal`, never probed, and removable. A stored name is `ErrExecutableListed` before any
check. A write notifies `appdb.KeyExecutables` (`ListenForExecutableChanges`).

**The cloud platform switches are `sandbox.<platform>.enabled` settings** (`cloudplatform.go`),
each on by default ([ADR](../../../../docs/adr/2026-11-18-every-cloud-platform-is-on-until-the-user-turns-it-off.md)).
`CloudPlatformSwitches` reads all four in one read; a value Kubetail cannot use is off. A failed read
is off everywhere but Settings, which answers the error. Nothing watches the switches.
