# Security record — Read opens any file the user approves, 23 September 2026

**Subject:** `Read` opens any regular text file on the machine once the user approves its path,
and is offered on every machine. It still reads the chat's own results directory without
asking, and it still opens nothing else under the app's data directory. This builds on
[saved output and Read](2026-09-23-saved-output-and-read.md) and [the bash
tool](2026-09-18-bash-tool.md). The living model is [security-model.md](../security-model.md).

## What is new

**Any approved file's text now leaves for the provider.** A read outside the results directory
waits on an approval request that names the path. Once approved, up to 30,000 bytes of the
file, redacted, go into the chat's history and to the provider on every later turn. Before, the
same read took an approved `cat` through bash; the request now names a file instead of a
command, and a machine with no shell can read at all.

**`Read` is gated, with a skip.** `tools.Approval` has `Skip`, and only `Read` sets it: for a
path in the chat's results, for a path in the rest of the data directory, and for a path that
is not one absolute, plain line. The last two are refused by `Run`, so no one is asked about a read
that cannot happen. The zero value asks, so a gated tool that forgets asks
(`TestAGatedToolAsksByDefault`). A skipped call runs with no approval row and `is_mutating` 0
(`TestASkippedApprovalRunsUnasked`, in `agent` and `services/chat`). Every other gated call keeps
`is_mutating` 1, an approved `Read` included: the column means "gated".

## The path drawn is the path opened

**Only a plain absolute path of one line is opened** (`fileguard.Abs`). The request draws the path
whole and never folds it, so a path holding a control character or longer than 4,096 bytes is
refused before anything else is judged: a newline would put the file opened below the line the
user reads (`TestAbsRefusesAPathThatIsNotOneLine`). A path with an empty, `.` or `..`
component is refused with its plain form, since the request draws the path as the model wrote
it and `/tmp/link/../x` would show one file and open another
(`TestAbsRefusesAPathThatIsNotPlain`). `~` and relative paths are refused
(`TestAbsRefusesARelativePath`). On Windows `/` and `\` are both separators, a Git Bash drive
path is the one other spelling of the same file, and a UNC or device path is refused before
anything touches it, since opening one reaches out to another machine
(`TestReadRefusesAPathOffThisMachine`).

**The last component is never followed.** `fileguard.Open` `Lstat`s first and refuses a link with
its target, absolute and plain, resolved through the link's own directory, so the model can ask
for the real file and the user approves that (`TestOpenRefusesALinkNamingItsTarget`). On
Windows a reparse point is a link when its tag is a symbolic link or a junction
(`TestOpenRefusesAJunction`); any other tag, a OneDrive file among them, is judged by what
`Stat` says of the file behind it.

**Nothing is opened that would be refused.** The type is judged by the `Lstat` before the open,
since opening some devices has side effects, and a FIFO is refused without waiting on a writer
(`TestOpenRefusesAFIFOBeforeOpeningIt`). On Unix the open adds `O_NOFOLLOW`, and the open file's
own `Stat` is checked again, which catches a swap in between.

## The data directory is fenced, by name and by identity

Nothing under `--data-dir` but the chat's results is read, approved or not. By name
(`Fence.Named`), such a path is skipped and refused. On disk (`Fence.Holds`), the longest prefix
of the path that exists is resolved, links followed, and it and each ancestor are compared with
the data directory by `os.SameFile`, so a link into it, a spelling in another case and a Windows
short name are all caught (`TestFenceHoldsALinkIntoTheDataDir`,
`TestFenceHoldsTheDataDirInAnotherCase`, `TestReadRefusesTheDataDirThroughALink`). An ancestor
that cannot be resolved for any reason but absence is refused as unreadable
(`TestFenceCannotTellPastAnUnreadableAncestor`). Every refusal inside the data directory is one
sentence that names nothing about the path.

## No file work holds the turn

A stat or open on a stale network mount blocks in a syscall no context reaches. So the fence's
check, the open and the read run on a goroutine, and the call answers its cancel or timeout at
once (`TestReadAnswersTheCancelWhileTheFileWorkBlocks`). The goroutine closes what it opened
when its syscall returns, and what it hands back then is dropped.

## Stamps

Every successful read outside the data directory sets the path's stamp on the chat: the SHA-256
of the raw bytes, and `Whole` when the model was shown every byte as it is — from line 1 to the
end, no line or result cut, nothing redacted, no byte order mark or `\r` stripped
(`TestReadStampsWholeOnlyForEveryByte`, `TestReadRedactsAFileOutsideTheResults`). A read of the
same bytes never takes back `Whole` (`TestARangeReadKeepsAWholeStamp`). A call answered
`cancelled` or `timeout` stamps nothing. Stamps live in memory, per chat, and go with the chat
(`TestAChatsStampsGoWithIt`); a restart forgets them, which fails closed for the `Write` and
`Edit` steps that will read them.

## Residual

- **A swap between the check and the open.** A link swapped into a parent directory between
  `Holds` and the open is followed. On Windows a link-type reparse point swapped in between the
  tag read and the open is followed too, since Windows has no `O_NOFOLLOW`.
- **A goroutine left in a syscall on a dead network mount.** It holds nothing the app waits on.
- **A link already in a parent directory.** The request names the path through it, and the file
  read is wherever it leads. Parent links are followed because the system's own are everywhere
  (macOS's `/tmp` and `/var`).
- **A hard link outside the data directory to a file inside it.** `Holds` judges a file by its
  directories, and a hard link has no directory in common with its target. Making one takes an
  approved command.

The hard-link residual of [saved output and Read](2026-09-23-saved-output-and-read.md) stands as
it was: a read of the results is still unasked.
