# Kubetail's folders and links

[← Security model](../security-model.md)

Kubetail's folders are the fence a sandboxed command must not read, and a symlink or a Windows junction gives a folder a second name. So a link is followed in one place, at one time, and refused everywhere else. A layout folder may be a link: each program follows it once, at start, and the fence holds the resolved paths, compared as files and never as text. A subfolder below one (`chats`, `kube`, `runs` and the rest) must be a real folder, the Unix runtime folder may not itself be a link, and no read-write grant sits on or above a link parent, the folder that holds a followed link. Kubetail assumes every folder on the path to a layout folder, and to a link's target, can be written only by the user or by root; it checks the runtime folder alone, because its default parent is shared. A process of the user's own outside the sandbox, or another account that can write a folder on the path, can still repoint a link. On Unix the folders are private by their modes (0700 directories, 0600 files); on Windows they rely on the profile's ACL, the runtime folder is `%LOCALAPPDATA%\Kubetail\run`, and a link or junction may not lead a layout folder out of the profile. The sandbox's side of the fence is in [the sandbox's files](sandbox-files.md).

## Protections

| ID | Protection | Status | Residual |
| --- | --- | --- | --- |
| FOLD-1 | A subfolder of Kubetail's directories (`chats`, `monitor`, `kube`, `logs`, `kubectl`, `tmp`, `shell`, `runs`) must be a folder. Each is made at start through an `os.Root` on its resolved parent, and a symlink or a Windows junction there fails the start, before any service runs. | **Enforced** | — |
| FOLD-2 | The host refuses a linked `<state>/webview`. | **Enforced** | — |
| FOLD-3 | The sidecar follows each layout folder's links once, at start, and the fence holds the resolved paths. | **Enforced** | — |
| FOLD-4 | A link to nothing or a cycle of links at a layout folder fails the start. | **Enforced** | — |
| FOLD-5 | Two kinds linked to one folder, or one linked inside another, fail the start. | **Enforced** | — |
| FOLD-6 | Kubetail assumes the path to its folders is the user's alone and checks only the runtime folder. | **By decision** | A process of the user's own outside the sandbox, or another account that can write a folder on the path, can still plant or repoint a link at one of Kubetail's folders. |
| FOLD-7 | The host follows each folder's links once, at launch. A linked folder is named by its target, and a link to nothing fails the launch. | **Enforced** on Unix | — |
| FOLD-8 | On Windows a link that leads a layout folder out of the profile fails the launch. | **Enforced** on Windows | — |
| FOLD-9 | A sandboxed command cannot repoint a link Kubetail followed to one of its folders: no read-write grant is on or above the folder that holds one. The grant is refused when it is made and when it is a stored grant. | **Enforced** | It rests on the assumption that only the user or root can write the folders on the path. |
| FOLD-10 | A run's policy with a Write rule on or above a link parent does not compile. | **Enforced** | It rests on the assumption that only the user or root can write the folders on the path. |
| FOLD-11 | A read-write grant of a link parent is left out of a command's grants. | **Enforced** | It rests on the assumption that only the user or root can write the folders on the path. |
| FOLD-12 | The socket lives in a private per-user directory: `$XDG_RUNTIME_DIR/kubetail` when the session manager gives one, else `/tmp/kubetail-<uid>` created 0700. | **Enforced** | — |
| FOLD-13 | A directory left by an earlier run is adopted only once this user is proved to own it. The ownership check comes before the chmod, so another user's symlink is refused rather than tightened. | **Enforced** | — |
| FOLD-14 | A fallback name another user claimed first gives way to a directory beside it: the one an earlier launch made if this user owns it and it is no link, else a fresh `mkdtemp` one. Squatting cannot keep the app from starting, and launches keep to one directory. | **Enforced** | — |
| FOLD-15 | The sidecar refuses to start a command while the runtime directory is not this user's alone: a directory no other user can write and not a link. | **Enforced** | It does not check a folder above the runtime folder. |
| FOLD-16 | The sidecar refuses, at start, a runtime folder that is itself a link, and follows the path above it. | **Enforced** | It does not check a folder above the runtime folder. |
| FOLD-17 | The data, config, cache and runtime directories are created 0700. | **Enforced** on Unix | — |
| FOLD-18 | On macOS the app folder holding the data, config and state is made 0700 before any of them. | **Enforced** on Unix | — |
| FOLD-19 | `host.json` and a command's saved output are written 0600. | **Enforced** on Unix | — |
| FOLD-20 | The bash tool's shell directory is 0700 and the snapshot in it 0400. | **Enforced** on Unix | — |
| FOLD-21 | On Windows these directories and files, the bash tool's shell directory included, rely on the inherited profile ACL. | **By decision** on Windows | — |
| FOLD-22 | A config folder that is itself a link at launch keeps its target's mode, a dotfiles repository's being the user's, and `host.json` is written where it leads. | **Enforced** on Unix | — |
| FOLD-23 | The host's log directory is created 0700 (`~/Library/Logs/Kubetail` on macOS, a `logs` leaf on the state directory on Linux and Windows). The log file inside it is created under the umask. | **Enforced** on Unix | The file holds cluster names and server hostnames, so the directory is the gate. |
| FOLD-24 | A host log directory that cannot be made leaves the host on stderr. | **Enforced** on Unix | — |
| FOLD-25 | `window-state.json` sits beside the host's log directory in the state directory, inside the sidecar's fence, which the sidecar places from the layout both sides compute. | **Enforced** on Unix | — |
| FOLD-26 | The sidecar's log file is created 0600 under a 0700 directory. | **Enforced** on Unix | On Windows the file relies on the inherited profile ACL, as the cache files do. |
| FOLD-27 | The sidecar makes its log directory owner-only, and the state or app folder above it, before it opens the file. | **Enforced** on Unix | — |
| FOLD-28 | The sidecar logs to stderr when its log directory is missing or open to others. | **Enforced** on Unix | — |

## Evidence

### FOLD-1

- **Where:** `sidecar/internal/app/subfolders.go`, `sidecar/internal/lib/rootdir`
- **Records:** [ADR](../adr/2026-11-24-follow-a-link-to-a-kubetail-folder-once-at-start-and-refuse-one-below-it.md)
- `TestALinkedSubfolderFailsTheStart` — each subfolder, Unix.
- `TestAJunctionedSubfolderFailsTheStart` — Windows.
- `TestOpenRefusesAJunctionAndADirectorySymlink` — Windows.
- `TestAFileWhereASubfolderGoesFailsTheStart` — a file where a subfolder goes fails the start.

### FOLD-2

- **Where:** `src-tauri/src/paths.rs`
- **Records:** [ADR](../adr/2026-11-24-follow-a-link-to-a-kubetail-folder-once-at-start-and-refuse-one-below-it.md)
- `a_linked_webview_dir_is_refused` — a linked `<state>/webview`.
- `a_junctioned_webview_dir_is_refused` — Windows.

### FOLD-3

- **Where:** `sidecar/internal/app/subfolders.go`, `sidecar/internal/app/folders.go`
- **Records:** [Kubetail's folders are resolved once](../security/2026-11-24-kubetails-folders-are-resolved-once.md)
- `TestALinkedLayoutFolderIsResolved`, `TestARootUnderALinkedParentIsResolved` — each layout folder's links are followed once, at start, and the fence holds the resolved paths.

### FOLD-4

- **Where:** `sidecar/internal/app/subfolders.go`, `sidecar/internal/app/folders.go`
- **Records:** [Kubetail's folders are resolved once](../security/2026-11-24-kubetails-folders-are-resolved-once.md)
- `TestALinkedLayoutFolderWhoseTargetIsMissingFailsTheStart` — a link to nothing fails the start.
- `TestACycleOfLinksAtALayoutFolderFailsTheStart` — a cycle of links fails the start.

### FOLD-5

- **Where:** `sidecar/internal/app/subfolders.go`, `sidecar/internal/app/folders.go`
- **Records:** [Kubetail's folders are resolved once](../security/2026-11-24-kubetails-folders-are-resolved-once.md)
- `TestKindsLinkedOntoOneAnotherFailTheStart` — two kinds linked to one folder, or one linked inside another.

### FOLD-6

- **Records:** [assumes](../adr/2026-11-24-follow-a-link-to-a-kubetail-folder-once-at-start-and-refuse-one-below-it.md)
- Note: no sandboxed command can plant or repoint a link at one of Kubetail's folders. A run writes only below the subfolders and in folders granted, which are kept clear of every link parent.
- Note: a process of the user's own outside the sandbox, or another account that can write a folder on the path, still can.

### FOLD-7

- **Where:** `src-tauri/src/paths.rs`
- `a_linked_layout_folder_is_named_by_its_target` — a linked folder is named by its target.
- `a_link_to_nothing_is_an_error_naming_both_ends`, `a_log_folder_linked_to_nothing_fails_the_launch` — a link to nothing fails the launch.
- Note: `Folders::make` follows the links.

### FOLD-8

- **Where:** `src-tauri/src/paths.rs`
- `a_link_may_not_lead_a_layout_folder_out_of_the_profile`, `a_junction_out_of_the_profile_fails_the_launch` — on Windows a link out of the profile fails the launch.

### FOLD-9

- **Where:** `sidecar/internal/services/settings/folders.go`
- **Records:** [assumption](../adr/2026-11-24-follow-a-link-to-a-kubetail-folder-once-at-start-and-refuse-one-below-it.md)
- `TestAWriteGrantOnOrAboveALinkParentIsRefused` — at the grant and for a stored grant, rule `link-parent`.
- Note: a read-only grant of a link parent is accepted.

### FOLD-10

- **Where:** `sidecar/internal/run/sandbox/policy.go`
- **Records:** [assumption](../adr/2026-11-24-follow-a-link-to-a-kubetail-folder-once-at-start-and-refuse-one-below-it.md)
- `TestAPolicyChecks` — a Write rule on or above a link parent does not compile.

### FOLD-11

- **Where:** `sidecar/internal/tools/bash/grants.go`
- **Records:** [assumption](../adr/2026-11-24-follow-a-link-to-a-kubetail-folder-once-at-start-and-refuse-one-below-it.md)
- `TestAReadWriteGrantOfALinkParentIsLeftOut` — a read-write grant of a link parent is left out.

### FOLD-12

- **Where:** `src-tauri/src/paths.rs`
- `runtime_dir_is_owner_only`, `runtime_dir_path_is_per_user_outside_xdg` — the socket's directory is private and per-user.
- Note: on Windows it is `%LOCALAPPDATA%\Kubetail\run`, a sibling of the data directory under the profile ACL.
- Note: a sandboxed run's kubeconfig and socket go in its `runs/`.

### FOLD-13

- **Where:** `src-tauri/src/paths.rs`
- `ensure_private_dir_refuses_a_symlink_without_touching_its_target` — another user's symlink is refused rather than tightened.

### FOLD-14

- **Where:** `src-tauri/src/paths.rs`
- `a_squatted_fallback_gives_way_to_a_fresh_directory` — a claimed fallback name gives way to a fresh directory beside it.
- `a_later_launch_keeps_the_directory_an_earlier_one_made` — launches keep to one directory.
- `a_link_named_like_an_earlier_directory_is_passed_over` — an earlier directory is kept only if it is no link.
- Note: the sidecar sweeps that directory's crash leftovers.

### FOLD-15

- **Where:** `sidecar/internal/tools/bash/rundir_unix.go`
- `TestACommandRefusesASnapshotAnotherCouldSwap`, `TestARunsDirectoryNotThisUsersAloneMakesNoRun` — no command starts while the directory is not this user's alone.
- Note: every command sources the snapshot there, and a sandboxed run keeps its kubeconfig there.
- Note: before each sandboxed run the sidecar refuses a runtime folder that is a link, that another user owns, or that group or others can write (`checkPrivate`).
- Note: neither this check nor the one at start checks a folder above it.

### FOLD-16

- **Where:** `sidecar/internal/app/folders.go`
- `TestALinkedRuntimeFolderFailsTheStart` — a runtime folder that is itself a link fails the start.
- `TestARuntimeFolderUnderALinkedParentIsResolved` — the path above it is followed.
- Note: neither this check nor the one before each sandboxed run checks a folder above it.

### FOLD-17

- **Where:** `src-tauri/src/paths.rs`, `sidecar/internal/app/layout/make.go`, `sidecar/internal/app/app.go`, `sidecar/internal/lib/rootdir`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/umask_unix.go`
- `ensure_data_dir_creates_and_tightens_to_0700` — the data directory.
- `config_dir_is_created_owner_only`, `an_open_config_dir_is_tightened`, `a_config_dir_under_a_linked_root_is_made_owner_only` — the config directory.
- `TestMakeOwnerOnlyMakesAndTightensTo0700` — a directory is made and tightened to 0700.
- `TestResolveFoldersTightensItsFoldersAndTheAppFolderAbove`, `TestResolveFoldersMakesAMissingFolderOwnerOnly` — the folders the sidecar resolves.
- `TestSetOwnerOnlyUmask` — the sidecar's umask.
- `TestMakeRootMakesItOwnerOnly` — a folder made through `rootdir`.
- `TestTheServiceMakesTheChatsDirectory`, `TestAChatsDirectoryIsMadeOnlyWhenAsked` — the chats directory.

### FOLD-18

- **Where:** `src-tauri/src/paths.rs`, `sidecar/internal/app/layout/make.go`
- `the_app_folder_is_made_owner_only_before_a_kind_in_it` — the host makes the app folder owner-only before a kind in it.
- `TestMakeOwnerOnlyTightensAnOpenAppFolderAbove` — the sidecar tightens an open app folder above.

### FOLD-19

- **Where:** `src-tauri/src/host_file.rs`, `sidecar/internal/lib/atomicjson`, `sidecar/internal/services/cluster/service.go`, `sidecar/internal/lib/sqlitepool`, `sidecar/internal/appdb`, `sidecar/internal/services/cluster/internal/kubestore/manager.go`, `sidecar/internal/tools/bash/bash.go`
- `a_write_after_the_directory_went_is_owner_only` — `host.json`.
- `TestSaveIsOwnerOnly` — a file `atomicjson` saves.
- `TestOpenWriterFilesAreOwnerOnly` — the files `sqlitepool` opens for writing.
- `TestCacheFileIsOwnerOnly` — a cache file.
- `TestLargeOutputIsSavedWithAPreview` — a command's saved output.

### FOLD-20

- **Where:** `sidecar/internal/tools/bash/snapshot_unix.go`
- `TestTheSnapshotIsReadOnly` — the shell directory 0700 on Unix with the snapshot 0400.

### FOLD-21

- **Records:** [by decision](../adr/2026-09-02-windows-cache-files-rely-on-the-profile-acl.md)
- Note: the bash tool's shell directory is included.

### FOLD-22

- **Where:** `src-tauri/src/paths.rs`
- `a_linked_config_dir_keeps_its_targets_mode` — the link's target keeps its mode, and `host.json` is written where the link leads.

### FOLD-23

- **Where:** `src-tauri/src/paths.rs`, `src-tauri/src/logging.rs`
- `log_dir_is_created_0700` — the host's log directory.
- Note: the sidecar makes the same directory owner-only itself (FOLD-27).
- Note: the directory is the gate for the log file as it is for the data directory.

### FOLD-24

- **Where:** `src-tauri/src/paths.rs`, `src-tauri/src/logging.rs`
- `a_log_dir_that_cannot_be_made_is_none` — a log directory that cannot be made leaves the host on stderr.

### FOLD-25

- **Where:** `src-tauri/src/paths.rs`, `sidecar/internal/app/layout`
- `each_platforms_layout_is_the_fixtures`, `TestEachPlatformsLayoutIsTheFixtures` — the layout both sides compute.

### FOLD-26

- **Where:** `sidecar/internal/lib/logging/open.go`
- `TestOpenCreatesAnOwnerOnlyFile` — the log file is created 0600.
- Note: the file holds the same cluster names and hostnames the host's file does.

### FOLD-27

- **Where:** `sidecar/internal/lib/logging/open.go`, `sidecar/main.go`, `sidecar/internal/app/layout/make.go`
- `TestMainMakesTheLogFolderAndLogsThere` — the sidecar makes the directory owner-only before it opens the file.
- `TestMakeOwnerOnlyTightensAnOpenStateFolderAboveTheLogs` — the state or app folder above it.

### FOLD-28

- **Where:** `sidecar/internal/lib/logging/open.go`
- `TestOpenRefusesAMissingDirectory` — a missing directory.
- `TestOpenRefusesADirectoryOthersCanReach` — a directory open to others.
