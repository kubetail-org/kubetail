# Security record — Write replaces a file whole, 24 September 2026

**Subject:** `Write` creates a file, with any missing parent directories, or replaces one whole,
once the user approves the path and the content. It is the first tool that changes the user's
files without going through a shell, and it is offered on every machine. It builds on [read any
approved file](2026-09-23-read-any-approved-file.md), whose path rules, fence and stamps it
shares. The living model is [security-model.md](../security-model.md).

## What can change

Any regular text file the user owns, in a group the new file can be given, that they can write,
in a directory they can write, outside the data directory — after an approval that draws the
whole content. A new file can be made anywhere the user can write, with the directories on its
way. Before, the same change took an approved heredoc through bash, whose content hid inside
quoting; the request now draws the file itself.

## The request is the file

**Every Write that would run is asked** (`TestWriteAsksOnTheNameAlone`). The request draws the
path in mono through `VisibleText`, never folded, then the content through `VisibleText`, folded
past 2,000 characters or 24 lines behind *Show the rest*, and Approve waits on it. `VisibleText`
keeps `\n` and `\t` and spells `\r`, so a CRLF content shows a mark at every line end. A space
or tab that ends a line is spelled too, and a line under the content says whether the file ends
with a newline, since neither draws in a `<pre>`: the user sees the bytes that land, and content
of whitespace alone reads as its marks. Empty content reads *The file will be empty.* A settled call reads
`Write <path>`, its body the content folded the same way, then the result.

**The gate reads the path by name alone.** `Approval` skips a path `fileguard.Abs` refuses, one
under the data directory by name, and content holding a NUL, since `Run` refuses each before
touching anything; it asks for every other path alike. It never stats, opens or resolves the
path, so no refusal answers the model about a path no one approved — whether it exists, where a
link leads, who owns it — and no NFS or autofs path is mounted before the user decides.

## Replacing needs a whole read

An existing file is replaced only over the chat's stamp of it, matching the file's bytes now and
`Whole`: the model was shown every byte, unredacted and unconverted
(`TestWriteNeedsAMatchingWholeStamp`). So Write never puts a redaction marker, LF endings or
unseen lines in place of what was there. A redacted file, a CRLF file and one opening with a byte
order mark are never `Whole`; each is changed with Edit alone. A binary or a file past 8 MiB is
refused, since Read refuses both (`TestWriteRefusesABinaryAndALargeFile`). A successful write
stamps the file `Whole` with the content's hash; a write answered as cancelled stamps nothing
(`TestWriteSetsTheStamp`).

## The owner, the group and the permissions stay

`fileguard.Replaceable` refuses a file the user does not own, since the rename would hand it to
them (`TestReplaceableRefusesAnotherUsersFile`), and a file whose group the new file could not
be given (`TestReplaceableRefusesAGroupItCannotGive`). A group can be given when it is one of the
user's, which `Chown` allows, or when it is the directory's and a new file there takes it anyway:
always on macOS, and under a setgid directory on Linux — so a file in macOS's `/tmp`, group
`wheel`, is replaced (`TestReplaceableTakesAGroupTheNewFileGets`). The temporary file is
`Chown`ed only when its group differs (`TestReplaceKeepsTheGroup`). It also refuses a file the
user cannot write and a directory they cannot write, through `access(2)`, which opens nothing, so
no watcher sees a write that never happened (`TestReplaceableAndCreatableOpenNothing`). On
Windows the file's read-only attribute is checked, and a denying ACL answers at the rename.

An existing file keeps its permission bits; setuid, setgid and sticky are never carried over
(`TestReplaceKeepsTheMode`, `TestWriteKeepsAnExistingModeAndGroup`).

## The umask

`main` sets the process umask to owner-only, so every file the sidecar makes for itself is
private. A file the user asked for should not be: `setOwnerOnlyUmask` returns the umask it
replaced, the one the sidecar started with, and `app.Config.UserUmask` carries it to Write. A
new file is `0o666` and each directory made for it `0o777`, less that umask, set with an
explicit `Chmod` (`TestCreateAppliesTheUmask`, `TestWriteCreatesAFileAndItsParents`). A
directory made under a setgid parent keeps the bit when the user is in the parent's group
(`TestCreateKeepsASetgidDirectory`); Linux's `chmod` drops it otherwise.

## The write is a rename

The content goes to a temporary file beside the target, is synced, and is renamed over it, so a
reader never sees half a file (`TestReplaceIsARename`, `TestWriteIsARename`). A new file is put in
place by a rename that refuses to replace; on a filesystem without one, by a link, which also
fails on a file there; on one with no hard links either (FAT, exFAT), by an `O_EXCL` write, which
is not atomic but never replaces a file (`TestCreateRefusesAnExistingFile`,
`TestCreateFallsBackWhereTheFilesystemLacksACall`). A missing directory is made only where
nothing is: a link found there is refused, never followed
(`TestCreateDoesNotFollowALinkInItsWay`).

What the rename drops: the target is a new inode, so other hard links to the old one keep the
old contents, and extended attributes and ACLs are not carried over.

## The data directory and links

The data directory is fenced by name and on disk, as for Read: a path under it by name is
refused before anything is touched, and one reached through a link in a parent is refused
before any directory is made (`TestWriteRefusesTheDataDirByName`,
`TestWriteRefusesTheDataDirThroughALink`). A link in the last component is refused with its
target, and nothing is written through it (`TestWriteRefusesALinkNamingItsTarget`).

## No file work holds the turn

Everything after the string checks runs on a goroutine, and the call answers its cancel at once
(`TestAStuckWriteHonoursTheCancel`). `fileguard` checks the context before the first directory
and before the rename, so a write that wakes late changes nothing
(`TestReplaceAndCreateLeaveNothingBehindOnACancel`).

## Residual

- **A change between the stamp check and the rename.** A file changed in that window is replaced.
- **A link swapped into a parent directory in the same window.** It is followed.
- **A rename under way when the call is cancelled.** It lands, with no stamp, so the next Write
  or Edit of the file is refused as changed.
- **A crash between the temporary file and the rename** leaves a `.<name>.kubetail-*` file beside
  the target; one inside the link fallback can leave it as a second name for the new file.
- **A new file on FAT or exFAT** is written in place, so a reader can see part of it.
- **A directory made under a setgid parent whose group the user is not in** loses the bit, since
  Linux's `chmod` drops setgid for a caller outside the directory's group, and the `Chmod` that
  applies the umask is one. Files made in it then take the user's group, not the tree's.
