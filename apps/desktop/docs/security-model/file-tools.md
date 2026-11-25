# The file and web tools

[← Security model](../security-model.md)

`Read`, `Write`, `Edit` and `WebFetch` are the chat's tools for files and the web. Each is gated: a call either falls where the chat may act unasked, or the user approves exactly what the request draws. Unasked means the chat's own directory for `Read`, the chat's workspace for `Write` and `Edit`, and a folder the session was granted. Kubetail's directories (data, cache, runtime, and the host's logs and config) stay shut to all three file tools but for the chat's own directory, by name and by identity, so a link cannot reach them. A link in the last component, a link swapped in, and a link out of a root or a granted folder are refused. `WebFetch` sends one `https` GET to a URL the user approves and reaches no non-public address. The rows assume the model, the files it names and the pages it fetches are attacker-controlled; each row's Residual states what stays open.

## Protections

| ID | Protection | Status | Residual |
| --- | --- | --- | --- |
| FILE-1 | `chatTools` offers `Read`, `Write`, `Edit` and `WebFetch` on every machine. | **Enforced** | — |
| FILE-2 | `Read` opens the chat's own directory (its saved results, its tasks' output and its workspace) without an approval, and any other regular text file once the user approves its path. It never opens the rest of Kubetail's directories: data, cache, runtime, and the host's logs and config. | **Enforced** | — |
| FILE-3 | A skipped call leaves no approval row and `is_mutating` 0. The zero value of `Approval.Skip` asks. | **Enforced** | — |
| FILE-4 | The path drawn is the path opened. `Read` takes only a plain absolute path of one line: no control character, and at most 4,096 bytes, so the request draws it whole. A Git Bash drive path is its one other spelling. A UNC or device path is refused untouched. | **Enforced** | — |
| FILE-5 | A link in the last component is refused with its target. | **Enforced** | A link swapped into a parent between the check and the open, or on Windows a link-type reparse point swapped in after the tag read. A link already in a parent directory, which the request names the path through. |
| FILE-6 | A FIFO is refused before any open. | **Enforced** | — |
| FILE-7 | Every refusal outside the data directory says what is wrong without a Go error's text. | **Enforced** | — |
| FILE-8 | Kubetail's directories are fenced by name and by identity. The longest existing prefix of the path, links followed, and each of its ancestors are compared with each fenced directory by `os.SameFile`. | **Enforced** | A hard link outside the data directory to a file in it. |
| FILE-9 | Each fenced directory is read again on every check and made again when missing, so one the OS or the user cleared or recreated stays fenced. | **Enforced** | — |
| FILE-10 | An ancestor the fence cannot resolve is refused. | **Enforced** | — |
| FILE-11 | Inside the fence, every refusal is one sentence that names nothing about the path. | **Enforced** | — |
| FILE-12 | The file work runs where a cancel can leave it. | **Enforced** | A goroutine left in a syscall on a dead mount. |
| FILE-13 | A path in a folder the session was granted, outside what the sandbox keeps shut there, is read without an approval. The skip names the folder. | **Enforced** | — |
| FILE-14 | A path in a granted folder is reached through a walk of that folder alone, by handle from `/`, never by path, whatever the session's folders say by then. | **Enforced** | — |
| FILE-15 | A closed path under the grant asks as before and is read as any approved path, at the target it has now. | **Enforced** | — |
| FILE-16 | A link into a closed path under the grant, swapped in or not, is refused. | **Enforced** | — |
| FILE-17 | The gate's resolving answers the turn's cancel. | **Enforced** | — |
| FILE-18 | The chat's results are reached only through the `tool-results` root. A linked chat entry is refused, one swapped in after the check included, and so is a link out at the open. | **Enforced** | After an approved command, a hard link in the results to any file, readable unasked. |
| FILE-19 | `Delete` refuses an id that is not a UUID before touching anything. | **Enforced** | — |
| FILE-20 | Every read is redacted whole before the range is taken. | **Enforced** | — |
| FILE-21 | A read in the workspace stamps the file. The rest of the results are never stamped. | **Enforced** | — |
| FILE-22 | A chat's directory goes with the chat. `Delete` removes the directory, one holding a read-only directory included, never walking a link. | **Enforced** | — |
| FILE-23 | The start sweep removes what a failed removal left. | **Enforced** | — |
| FILE-24 | `Write` creates a file, or replaces one whole, in the chat's workspace unasked, and anywhere else only once the user approves the path and the whole content. The gate reads the path by name alone. It asks for every call `Run` would not refuse before touching anything, but one in the workspace. | **Enforced** | — |
| FILE-25 | A path the gate skips on in the workspace is written through the workspace's root. | **Enforced** | — |
| FILE-26 | The request draws the path and the content. An unasked write's content is drawn open under its summary, folded. | **Enforced** | — |
| FILE-27 | `Write` replaces only a file the chat read whole and unchanged since. A replace needs a matching `Whole` stamp. | **Enforced** | — |
| FILE-28 | `Write` replaces only a file that the user owns, in a group the new file can be given, and that the user can write in a directory they can write. `Replaceable` refuses another owner, a group the new file cannot be given, and what the user cannot write, opening nothing. | **Enforced** | — |
| FILE-29 | The write is a rename that keeps the permission bits and the group. | **Enforced** | A file changed, or a link swapped into a parent, between the check and the rename. The temporary file a crash leaves. Hard links, xattrs and ACLs the rename drops. |
| FILE-30 | A new file never replaces one, and takes the sidecar's starting umask. | **Enforced** | A non-atomic write of a new file on FAT or exFAT. Setgid dropped from a directory made under a setgid parent whose group the user is not in. |
| FILE-31 | A link in a new file's way is never followed. | **Enforced** | — |
| FILE-32 | `Write` never writes under Kubetail's directories but the chat's workspace. The data directory is fenced by name and through a link. | **Enforced** | — |
| FILE-33 | The chat's workspace is reached by name. The rest of the data directory is refused. | **Enforced** | — |
| FILE-34 | Every change in the workspace goes through a root on it, so a link at the file or on the way is refused and nothing outside changes. The path forms' checks are kept. | **Enforced** | — |
| FILE-35 | A stuck write answers the cancel and changes nothing. | **Enforced** | A rename under way when the call is cancelled, which lands unstamped. |
| FILE-36 | A path in a folder granted read and write is written unasked. A path in a read grant asks. | **Enforced** | — |
| FILE-37 | A granted write goes through the walk of the folder the approval named, on its handles, so a link out of the folder is refused and nothing outside changes. | **Enforced** | — |
| FILE-38 | `Edit` replaces one exact occurrence of a string, or every one with `replace_all`, in the chat's workspace unasked, and anywhere else only once the user approves the path and both strings. The gate reads the input alone and asks for every call `Run` would not refuse before touching anything. | **Enforced** | A match against the raw file tells the model something about text `Read` redacted, one approved probe per answer. |
| FILE-39 | `Edit` refuses by input, unasked: an empty or no-op `old_string`, and a `new_string` holding a NUL or `[redacted]`. | **Enforced** | A file holding `[redacted]` as its own text is not editable with it as context. |
| FILE-40 | A path the gate skips on in the workspace is changed through the workspace's root. | **Enforced** | — |
| FILE-41 | An unasked edit's two strings are drawn open under its summary. | **Enforced** | — |
| FILE-42 | `Edit` changes only a file the chat read, whole or not, and unchanged since. It never makes a new file. An edit needs a matching stamp. | **Enforced** | — |
| FILE-43 | A match is exact, but for LF against a file that is CRLF throughout. | **Enforced** | The request draws LF strings in a CRLF file. |
| FILE-44 | Without `replace_all` a match is unique, overlaps included, by a count and one overlap probe. | **Enforced** | — |
| FILE-45 | The result's size is bounded before it is built. | **Enforced** | — |
| FILE-46 | `Edit` changes only a file `Write` could replace. It writes through `Replaceable` and `Replace`, so `Write`'s owner, group, mode and rename guarantees hold. | **Enforced** | `Write`'s residuals (FILE-29, FILE-30, FILE-35). |
| FILE-47 | `Edit` never changes a file under Kubetail's directories but the chat's workspace. The data directory is fenced through a link. | **Enforced** | — |
| FILE-48 | The chat's workspace is reached by name, and there only through a root on it. The rest of the data directory is refused, and so is a link in the workspace. | **Enforced** | — |
| FILE-49 | A stuck edit answers the cancel and changes nothing. | **Enforced** | — |
| FILE-50 | In a folder granted read and write an edit skips and goes through the walk, as `Write` does. In a read grant it asks. An edit through a link to a closed path is refused. | **Enforced** | — |
| FILE-51 | `WebFetch` sends one `https` GET, from the user's machine and network, to a URL the user approves. The gate asks for every URL `target` does not refuse by name, and a refused one never reaches the network. | **Enforced** | A URL built from cluster text that the user approves. |
| FILE-52 | The request draws the URL and the host it dials. | **Enforced** | — |
| FILE-53 | `WebFetch` reaches no local, private or other non-public address, on any hop. The dial refuses a non-public address after DNS, on every connection. | **Enforced** | A public name inside a VPN. |
| FILE-54 | The dial lets through only the configured `HTTPS_PROXY`. | **Enforced** | With a proxy, the proxy decides where a name lands. A `NO_PROXY` entry naming the proxy. |
| FILE-55 | A redirect is followed only to the approved host and port or its `www.` twin, over https, five at most, and each hop is checked by name first. Any other redirect is returned to the model. | **Enforced** | A same-host or `www.` hop is followed without asking. |
| FILE-56 | `WebFetch` sends no credential: no userinfo, cookie or `Authorization`. | **Enforced** | — |
| FILE-57 | What `WebFetch` returns is web text, not redacted inline, with every server-supplied string in its own lines capped. A status is read by its code, never its reason phrase, and a URL, media type and title are capped. | **Enforced** | A conversion left running after a cancel. |

## Evidence

### FILE-1

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/write`, `sidecar/internal/tools/edit`, `sidecar/internal/tools/webfetch`
- `TestTheFileToolsAreOfferedWithoutAShell`, `TestBashIsOfferedWhenItIsFound` — `chatTools` offers each of the four tools on every machine.
- `TestEveryGatedToolCanBeShown` — cited for `WebFetch` beside the two above.

### FILE-2

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md)
- `TestReadOfTheResultsIsSkipped` — a path in the chat's results is never asked about.
- `TestReadOfTheDataDirIsSkippedAndRefused`, `TestReadOfTheCacheAndRuntimeDirsIsSkippedAndRefused` — a path in the rest of the data directory is never asked about, and is refused.
- `TestReadOfAPathAbsRefusesIsSkippedAndRefused` — a path that is not one absolute, plain line is never asked about.
- `TestReadElsewhereIsAsked` — anything else is asked about.
- Note: `Read` is gated, with `Approval.Skip`.
- Note: `Read` returns what it opens redacted (FILE-20).

### FILE-3

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md)
- `TestASkippedApprovalRunsUnasked` — a skipped call leaves no approval row and `is_mutating` 0.
- `TestAGatedToolAsksByDefault` — the zero value asks.

### FILE-4

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md)
- `TestAbsRefusesAPathThatIsNotOneLine` — no control character, at most 4,096 bytes.
- `TestAbsRefusesAPathThatIsNotPlain`, `TestAbsRefusesARelativePath`, `TestReadRefusesAPathOffThisMachine` — a Git Bash drive path is the one other spelling; a UNC or device path is refused untouched.

### FILE-5

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md)
- `TestOpenRefusesALinkNamingItsTarget`, `TestOpenRefusesAJunction` — a link in the last component is refused with its target.

### FILE-6

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md)
- `TestOpenRefusesAFIFOBeforeOpeningIt` — a FIFO is refused before any open.

### FILE-7

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md)
- `TestReadRefusalsSayWhatIsWrong` — each refusal outside the data directory.

### FILE-8

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md)
- `TestAFenceHoldsEachOfItsDirectories`, `TestFenceHoldsALinkIntoTheDataDir`, `TestFenceHoldsTheDataDirInAnotherCase`, `TestReadRefusesTheDataDirThroughALink` — the fence by name and by identity: each directory, a link into the data directory, another case, and a read through a link.

### FILE-9

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md)
- `TestHoldsSeesAFencedDirectoryMadeAgain`, `TestHoldsMakesAMissingFencedDirectory` — a directory made again is seen, and a missing one is made.

### FILE-10

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md)
- `TestFenceCannotTellPastAnUnreadableAncestor` — an ancestor the fence cannot resolve.

### FILE-11

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md)
- `TestReadRefusesAPathOutsideTheChat` — the one sentence a refusal inside the fence gives.

### FILE-12

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md)
- `TestReadAnswersTheCancelWhileTheFileWorkBlocks` — the read answers the cancel while the file work blocks.

### FILE-13

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md), [path grants](../security/2026-10-04-path-grants.md)
- `TestAReadInAGrantedFolderAsksNoOne` — a path in a granted folder skips, naming the folder.

### FILE-14

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md), [path grants](../security/2026-10-04-path-grants.md)
- `TestARevokeBetweenApprovalAndRunChangesNothing`, `TestAGrantedCallAnswersAtItsEnd` — the walk of the granted folder alone, whatever the session's folders say by then.

### FILE-15

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md), [path grants](../security/2026-10-04-path-grants.md)
- `TestAHiddenPathUnderAGrantedHomeStillAsks` — a closed path under the grant asks as before and is read as any approved path.
- `TestARetargetedNeverPathStaysHidden`, `TestACheckResolvesTheZonesNow` — the path is judged at the target it has now.

### FILE-16

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md), [path grants](../security/2026-10-04-path-grants.md)
- `TestALinkToAHiddenPathUnderAGrantedHomeIsRefused`, `TestAWalkThroughAClosedFolderIsRefused` — a link into a closed path, swapped in or not.

### FILE-17

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md), [path grants](../security/2026-10-04-path-grants.md)
- `TestGrantedAnswersTheCancelWhileResolvingBlocks` — the gate's resolving answers the turn's cancel.

### FILE-18

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md)
- `TestOpenResultsRefusesALinkedDirectory` — a linked chat entry is refused.
- `TestOpenRefusesALinkSwappedInAfterTheCheck` — a link swapped in after the check is refused.
- `TestReadRefusesALinkOut` — a link out is refused at the open.

### FILE-19

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md)
- `TestADeleteOfAPathIsRefused` — an id that is not a UUID is refused before anything is touched.

### FILE-20

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md)
- `TestReadRedactsWhatACommandWrote`, `TestReadRedactsAFileOutsideTheResults` — a file a command wrote and a file outside the results are both redacted.

### FILE-21

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md)
- `TestReadStampsAWorkspaceFile` — a workspace read stamps the file; the rest of the results are never stamped.

### FILE-22

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md)
- `TestDeletingAChatRemovesItsResults`, `TestDeletingAChatRemovesALinkNotItsTarget` — `Delete` removes the directory, and a link rather than its target.
- `TestAReadOnlyWorkspaceGoesWithTheChat`, `TestAChatEntryThatIsALinkIsNeverWalked` — a read-only directory goes too, and a link is never walked.

### FILE-23

- **Where:** `sidecar/internal/tools/read`, `sidecar/internal/tools/internal/fileguard`, `sidecar/internal/agent/chat/chatdir.go`, `sidecar/internal/agent/chat/sweep.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md), [saved output and Read](../security/2026-09-23-saved-output-and-read.md), [the workspace](../security/2026-09-27-the-workspace.md)
- `TestTheStartSweepRemovesTheDirectoriesOfGoneChats`, `TestAFailedRemovalIsLeftForTheStartSweep` — the start sweep removes the directories of gone chats, a failed removal's included.

### FILE-24

- **Where:** `sidecar/internal/tools/write`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the write tool](../security/2026-09-24-write-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestWriteAsksOnTheNameAlone` — the gate reads the path by name alone.
- `TestAWriteInTheWorkspaceAsksNoOne`, `TestAWriteElsewhereStillAsks` — a call in the workspace asks no one; one elsewhere still asks.

### FILE-25

- **Where:** `sidecar/internal/tools/write`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the write tool](../security/2026-09-24-write-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestAWriteThatSkipsGoesThroughTheRoot` — a write that skips goes through the root.

### FILE-26

- **Where:** `sidecar/internal/tools/write`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the write tool](../security/2026-09-24-write-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `draws the path and the content as text, with two buttons` — the request draws the path and the content.
- `draws its content open, folded, and not again inside the disclosure` — an unasked write's content is drawn open under its summary, folded.

### FILE-27

- **Where:** `sidecar/internal/tools/write`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the write tool](../security/2026-09-24-write-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestWriteNeedsAMatchingWholeStamp` — a replace needs a matching `Whole` stamp.

### FILE-28

- **Where:** `sidecar/internal/tools/write`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the write tool](../security/2026-09-24-write-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestReplaceableRefusesAnotherUsersFile` — another owner is refused.
- `TestReplaceableRefusesAGroupItCannotGive`, `TestReplaceableTakesAGroupTheNewFileGets` — a group the new file cannot be given is refused; one it gets is taken.
- `TestReplaceableRefusesWhatItCannotWrite` — what the user cannot write is refused.
- `TestReplaceableAndCreatableOpenNothing` — the checks open nothing.

### FILE-29

- **Where:** `sidecar/internal/tools/write`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the write tool](../security/2026-09-24-write-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestReplaceIsARename`, `TestReplaceKeepsTheMode`, `TestReplaceKeepsTheGroup` — the write is a rename that keeps the permission bits and group.

### FILE-30

- **Where:** `sidecar/internal/tools/write`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the write tool](../security/2026-09-24-write-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestCreateRefusesAnExistingFile` — a new file never replaces one.
- `TestCreateAppliesTheUmask` — a new file takes the sidecar's starting umask.

### FILE-31

- **Where:** `sidecar/internal/tools/write`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the write tool](../security/2026-09-24-write-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestCreateDoesNotFollowALinkInItsWay` — a link in a new file's way is never followed.

### FILE-32

- **Where:** `sidecar/internal/tools/write`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the write tool](../security/2026-09-24-write-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestWriteRefusesTheDataDirByName`, `TestWriteRefusesTheDataDirThroughALink`, `TestWriteRefusesTheCacheDir` — the data directory is fenced by name and through a link, and the cache directory is refused.

### FILE-33

- **Where:** `sidecar/internal/tools/write`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the write tool](../security/2026-09-24-write-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestWriteReachesTheWorkspace` — the workspace is reached by name.
- `TestTheRestOfTheDataDirectoryStaysFenced` — the rest of the data directory is refused.

### FILE-34

- **Where:** `sidecar/internal/tools/write`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the write tool](../security/2026-09-24-write-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestALinkOutOfTheWorkspaceIsRefused`, `TestTheRootFormsRefuseALinkOut`, `TestCreateInRefusesADirectoryLinkOut`, `TestReplaceInRefusesADirectoryLinkOut` — a link at the file or on the way is refused and nothing outside changes.
- `TestReplaceableIn`, `TestCreatableIn`, `TestCreateInNeverReplaces` — the path forms' checks are kept in the root forms.

### FILE-35

- **Where:** `sidecar/internal/tools/write`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the write tool](../security/2026-09-24-write-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestAStuckWriteHonoursTheCancel` — a stuck write answers the cancel and changes nothing.

### FILE-36

- **Where:** `sidecar/internal/tools/write`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the write tool](../security/2026-09-24-write-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md), [path grants](../security/2026-10-04-path-grants.md)
- `TestAWriteInAGrantedFolderAsksNoOne` — a path in a folder granted read and write skips.
- `TestAWriteInAReadGrantStillAsks` — a path in a read grant asks.

### FILE-37

- **Where:** `sidecar/internal/tools/write`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the write tool](../security/2026-09-24-write-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md), [path grants](../security/2026-10-04-path-grants.md)
- `TestAGrantedWriteStaysInTheFolder`, `TestTheWalkRenamesOnTheHandle`, `TestAWriteInAGrantedFolderMakesItsParents` — the write stays in the folder, renames on the handle, and makes its parents there.

### FILE-38

- **Where:** `sidecar/internal/tools/edit`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the edit tool](../security/2026-09-24-edit-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestEditAsksOnTheNameAlone` — the gate reads the input alone.
- `TestAWriteInTheWorkspaceAsksNoOne`, `TestAWriteElsewhereStillAsks` — a call in the workspace asks no one; one elsewhere still asks.
- Note: The source's wording: the gate "asks for every call `Run` would not refuse before touching anything, an empty or no-op `old_string` and a `new_string` holding a NUL or `[redacted]` among those refused, and so is every call in the workspace".

### FILE-39

- **Where:** `sidecar/internal/tools/edit`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the edit tool](../security/2026-09-24-edit-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestEditRefusesByInputUnasked` — the calls refused by their input, without asking.

### FILE-40

- **Where:** `sidecar/internal/tools/edit`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the edit tool](../security/2026-09-24-edit-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestAWriteThatSkipsGoesThroughTheRoot` — a change that skips goes through the root.

### FILE-41

- **Where:** `sidecar/internal/tools/edit`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the edit tool](../security/2026-09-24-edit-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `draws an unasked edit open, and not again inside the disclosure` — an unasked edit's two strings are drawn open under its summary.

### FILE-42

- **Where:** `sidecar/internal/tools/edit`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the edit tool](../security/2026-09-24-edit-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestEditNeedsAMatchingStamp` — an edit needs a matching stamp.

### FILE-43

- **Where:** `sidecar/internal/tools/edit`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the edit tool](../security/2026-09-24-edit-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestEditMatchesLFInACRLFFile`, `TestEditFoldsNothingElse` — LF matches in a CRLF file, and nothing else is folded.

### FILE-44

- **Where:** `sidecar/internal/tools/edit`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the edit tool](../security/2026-09-24-edit-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestEditRefusesAMatchThatIsNotUnique`, `TestEditUniquenessIsLinear` — a match that is not unique is refused, by a count and one overlap probe.

### FILE-45

- **Where:** `sidecar/internal/tools/edit`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the edit tool](../security/2026-09-24-edit-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestEditRefusesAnOversizedResultBeforeBuildingIt` — an oversized result is refused before it is built.

### FILE-46

- **Where:** `sidecar/internal/tools/edit`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the edit tool](../security/2026-09-24-edit-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestEditKeepsTheModeAndGroup`, `TestEditRefusesWhatItCannotWrite`, `TestEditIsARename` — the mode and group are kept, what the user cannot write is refused, and the change is a rename.

### FILE-47

- **Where:** `sidecar/internal/tools/edit`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the edit tool](../security/2026-09-24-edit-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestEditRefusesTheDataDirThroughALink`, `TestEditRefusesTheCacheDir` — the data directory is fenced through a link, and the cache directory is refused.

### FILE-48

- **Where:** `sidecar/internal/tools/edit`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the edit tool](../security/2026-09-24-edit-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestEditReachesTheWorkspace`, `TestEditChangesAFileACommandMade` — the workspace is reached by name.
- `TestTheRestOfTheDataDirectoryStaysFenced` — the rest is refused.
- `TestALinkOutOfTheWorkspaceIsRefused` — a link there is refused.

### FILE-49

- **Where:** `sidecar/internal/tools/edit`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the edit tool](../security/2026-09-24-edit-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md)
- `TestAStuckEditHonoursTheCancel` — a stuck edit answers the cancel and changes nothing.

### FILE-50

- **Where:** `sidecar/internal/tools/edit`, `sidecar/internal/tools/internal/fileguard/write*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the edit tool](../security/2026-09-24-edit-tool.md), [the workspace](../security/2026-09-27-the-workspace.md), [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md), [path grants](../security/2026-10-04-path-grants.md)
- `TestAnEditInAGrantedFolderAsksNoOne`, `TestAnEditInAReadGrantStillAsks`, `TestAnEditThroughALinkToAHiddenPathIsRefused` — an edit in a folder granted read and write skips and goes through the walk, as `Write` does.

### FILE-51

- **Where:** `sidecar/internal/tools/webfetch`, `sidecar/internal/app/app.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the web fetch tool](../security/2026-09-24-web-fetch-tool.md)
- `TestWebFetchRefusesByName`, `TestTargetRefusesByName` — refused by name: other schemes, userinfo, dotless and numeric hosts, non-public literals, and a URL past 8,192 bytes.

### FILE-52

- **Where:** `sidecar/internal/tools/webfetch`, `sidecar/internal/app/app.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the web fetch tool](../security/2026-09-24-web-fetch-tool.md)
- `draws a fetch request with the URL and the host as text` — the request draws the URL and the host.

### FILE-53

- **Where:** `sidecar/internal/tools/webfetch`, `sidecar/internal/app/app.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the web fetch tool](../security/2026-09-24-web-fetch-tool.md)
- `TestPublic`, `TestWebFetchRefusesNonPublicNames`, `TestWebFetchRefusesARebindingName` — the dial refuses a non-public address after DNS, on every connection.

### FILE-54

- **Where:** `sidecar/internal/tools/webfetch`, `sidecar/internal/app/app.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the web fetch tool](../security/2026-09-24-web-fetch-tool.md)
- `TestWebFetchLetsTheProxyThrough`, `TestWebFetchChecksTheHTTPProxysAddress`, `TestProxyAddrIsTheDialsSpelling` — only the configured `HTTPS_PROXY` is let through.

### FILE-55

- **Where:** `sidecar/internal/tools/webfetch`, `sidecar/internal/app/app.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the web fetch tool](../security/2026-09-24-web-fetch-tool.md)
- `TestWebFetchFollowsWwwAndReturnsOtherRedirects`, `TestSameSite` — a redirect is followed only on the approved host and port, https, five at most, each hop checked by name first.

### FILE-56

- **Where:** `sidecar/internal/tools/webfetch`, `sidecar/internal/app/app.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the web fetch tool](../security/2026-09-24-web-fetch-tool.md)
- `TestWebFetchSendsNoCredentials` — no credential is sent.

### FILE-57

- **Where:** `sidecar/internal/tools/webfetch`, `sidecar/internal/app/app.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [the web fetch tool](../security/2026-09-24-web-fetch-tool.md)
- `TestWebFetchAnswersAStatusWithoutTheBody`, `TestOtherTypesAreRefused`, `TestATitleIsOneShortLine` — a status is read by its code, never its reason phrase; a URL, media type and title are capped.
