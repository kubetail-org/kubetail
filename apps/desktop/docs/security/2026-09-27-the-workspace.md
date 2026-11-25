# Security record — the workspace, 27 September 2026

**Subject:** every chat gets a **workspace**, `workspace/` in the chat's own directory,
`<dataDir>/chats/<chatID>`, beside its saved `results/` and its `tasks/`. Every Bash call
starts in it, Read opens it without asking and stamps what it reads there, and Write and Edit may
change files in it: the fence's one opening under the data directory. Every Write and Edit still
asks. This is step 2 of sandboxed Bash. The living model is
[security-model.md](../security-model.md).

## The fence's one opening

Write and Edit refused the whole data directory, and the workspace is inside it. They now reach the
chat's workspace by name (`Fence.NamedOutsideWorkspace`; `TestWriteReachesTheWorkspace`,
`TestEditReachesTheWorkspace`). The rest stays refused: the chat's results beside `work`, another
chat's workspace, and `app.db` (`TestTheRestOfTheDataDirectoryStaysFenced`, in both tools). A path
that reaches the workspace by another spelling, through a link or in another case, is not under it
by name, so the fence's check on disk refuses it as the data directory. The user still approves the
path and the content of every write.

## Why a root

A command can leave a link in the workspace, such as `work/notes -> ~/.zshrc`. Opened by path, a
write would follow it into the user's files. So every open, stat, create and rename in the
workspace goes through an `os.Root` on it (`Fence.File` and `fileguard`'s root forms). The root
refuses any name that leads out of it. A link at the name is refused with its target, and so is a
link anywhere on the way of a new file (`TestALinkOutOfTheWorkspaceIsRefused` in Write and Edit,
`TestTheRootFormsRefuseALinkOut`, `TestReplaceInRefusesADirectoryLinkOut`,
`TestCreateInRefusesADirectoryLinkOut`, `TestCreatableIn`). The workspace itself is opened through
the chat's results root, and refused unless it is the directory its `Lstat` saw
(`TestTheWorkspaceRefusesALink`). This matters once step 6 lets a sandboxed command write the
workspace and step 8 lets these writes skip the question.

The root forms make the path forms' checks with the calls a root offers. Writability is read off
the owner's mode bits, stricter than `access(2)`: everything in the workspace is the user's, so a
grant to anyone else never makes a file there writable (`TestReplaceableIn`, `TestCreatableIn`). A
new file is placed by a link from its temporary file, which fails on a file already there, so it
never replaces one (`TestCreateInNeverReplaces`).

## Read stamps the workspace

A read in the workspace stamps the file (`TestReadStampsAWorkspaceFile`), so Write and Edit can
change a file a command made (`TestEditChangesAFileACommandMade`). Only the workspace is stamped:
the rest of the chat's directory stays unstamped, and the fence refuses it to Write and Edit anyway.

## Where a command starts

Every call starts in the workspace unless its `workdir` names another directory, and the request's
`in <dir>` line names it (`TestApprovalKeepsTheCwd`). A sandboxed call must start under the
workspace by name, whatever `..` it holds (`TestWhereACallStarts`,
`TestASandboxedWorkdirOutsideTheWorkspaceIsRefused`). That check is guidance, not a boundary: a
link in the workspace can start a command elsewhere, which steps 6 and 7 contain. Until then the
stand-in confines nothing, and every call asks.

## Residuals

- Nothing caps the workspace's size. It grows until the chat is deleted.
- The workspace's path reaches the provider in the question's context. It names the data
  directory, as the saved-output paths already do.
- A removal can fail on what a chmod cannot undo, such as a macOS `uchg` flag or a process still
  writing there. It is logged, and each start's sweep tries again
  (`TestAReadOnlyWorkspaceGoesWithTheChat`, `TestAChatEntryThatIsALinkIsNeverWalked`,
  `TestAFailedRemovalIsLeftForTheStartSweep`).
- Read follows a link that stays inside the chat's results, and stamps the name it was given. A
  write through that name is refused, since the root forms never write through a link.
- Write's residuals hold in the workspace: a file changed between the check and the rename, and the
  temporary file a crash leaves.

## Amended 28 September 2026

A `Write` or `Edit` in the workspace no longer asks, on every machine, and a command the sandbox
confines runs unasked on macOS and Linux. The gate skips on the test by name that makes
`Fence.File` open the workspace's root, so the root above is what keeps an unasked write inside
it. What an unasked write put there is drawn open in the transcript. → [Bash runs in a
sandbox](2026-09-28-bash-runs-in-a-sandbox.md).
