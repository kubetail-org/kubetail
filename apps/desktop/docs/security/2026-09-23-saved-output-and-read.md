# Security record — saved output and Read, 23 September 2026

**Subject:** a command's output past the inline limit is saved to a file in the chat's results
directory, and a new `Read` tool, offered beside bash, reads a range of it without an approval.
This builds on [the bash tool](2026-09-18-bash-tool.md). The living model is
[security-model.md](../security-model.md).

## What is new

**More output stays on disk.** Output up to 30,000 bytes, header included, comes back whole.
Past that, up to 8 MiB a call is written to `<dataDir>/tool-results/<chatID>/<name>.txt`, where
16 KiB was kept before, in `app.db`. The model gets a 2 KB preview and the path.

**`Read` is the first tool that reads a file, and the first since bash that runs without an
approval.** It opens only files in the chat's results directory.

**Each call can leave more in the chat's history**: up to 30,000 bytes where it left 16 KiB, about
240 KB across a turn's 8 calls. All of it goes to the provider on every later turn.

## The saved file

**Redacted, owner-only, deleted with the chat.** The whole capture goes through `safe.Redact`
before anything is written, so the file, the preview and the result agree, and a key across the
preview's edge is redacted in both (`TestTheSavedFileIsRedactedWhole`). The file is created
`O_EXCL`, 0600, in a directory made 0700 (`TestLargeOutputIsSavedWithAPreview`,
`TestOpenResultsMakesTheChatsDirectoryOnlyWhenAsked`). Its name is `rand.Text()`, never a
provider's id.

Redaction does not catch everything a cluster holds, such as a Secret's base64 `data:`, so more
of it now stays on disk until the chat goes. That is [the bash tool](2026-09-18-bash-tool.md)'s
residual, kept for longer.

**Deleted with the chat.** `Delete` removes the chat's directory once its rows are gone, and a
cluster's chats take theirs with them (`TestDeletingAChatRemovesItsResults`). `RemoveAll` removes
a link and never its target (`TestDeletingAChatRemovesALinkNotItsTarget`). A failed removal is
logged, and the next start sweeps every entry that is not a chat's
(`TestTheStartSweepRemovesResultsOfGoneChats`).

**A failed save never names a Go error.** It falls back to the cut at the inline limit, with a
fixed note (`TestAFailedSaveFallsBackToACut`).

## The results directory is reached through one root

The service holds `tool-results` open as an `os.Root`. A chat's entry is `Lstat`ed, made and
opened through it, and anything but a directory is refused, a link included
(`TestOpenResultsRefusesALinkedDirectory`). The opened directory must be the one the `Lstat` saw,
so a link swapped in between the two is refused too, even one to another chat's directory
(`TestOpenResultsRefusesALinkSwappedInAfterTheCheck`). So a command that swaps a link in for the
chat's entry reaches no other directory.

**A chat id is never a path.** The id names the directory `Delete` removes, so an id that is not
a UUID is refused before anything is touched: `chatDelete(id: "<a live chat>/out.txt")` would
otherwise remove that chat's file, with its row kept and its turn still running
(`TestADeleteOfAPathIsRefused`).

## `Read`

**Confined by name.** The path must be absolute, and its `filepath.Rel` to the chat's directory
local. The file is opened through the chat's root, which refuses a link that leads out of it at
the moment of the open (`TestReadRefusesALinkOut`). It must be a regular file of at most 8 MiB.
Every refusal is one sentence that names nothing about the path
(`TestReadRefusesAPathOutsideTheChat`).

**It cannot hang the turn.** On Unix the open is `O_NONBLOCK`. Opening a FIFO for reading
otherwise blocks until something writes to it, in a syscall no context reaches, holding the
turn and every delete and shutdown that joins it (`TestReadRefusesAFIFOAtOnce`). The read goes
through a `LimitReader`, since a command left running can keep appending after the `Stat`.

**What it returns is redacted, whoever wrote the file.** A command can write into the directory,
so its contents are not known to be redacted. The whole file is redacted before the range is
taken, so a key is never cut in half (`TestReadRedactsWhatACommandWrote`). It is bounded to 30
seconds, and returns at most 30,000 bytes a call.

## Residual

It needs an approved command first.

- **A hard link is a regular file, which no root stops.** After one approved
  `ln ~/.aws/credentials <dir>/x`, every later turn can `Read` that file's current contents
  without an approval. They are redacted, but otherwise not limited. "Confined to the chat's
  directory" means confined by name, not by what the names point at.
