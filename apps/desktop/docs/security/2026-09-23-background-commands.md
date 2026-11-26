# Security record — background commands, 23 September 2026

**Subject:** bash takes `run_in_background`. Such a command is approved like any other, answers
at once, and keeps running after its turn. When it exits, the sidecar starts a turn on its own.
A new `TaskStop` tool, and a *Stop* button, end one. This builds on
[the bash tool](2026-09-18-bash-tool.md) and [saved output and Read](2026-09-23-saved-output-and-read.md).
The living model is [security-model.md](../security-model.md).

## What is new

**A process outlives its turn, and a Cancel.** Until now every command ended with its call. A
background command runs until it exits, the model or the user stops it, its chat is deleted, or
the app quits. A Cancel stops the answer and leaves the command running
(`TestACancelledTurnKicksNothing`).

**A turn can start without the user sending anything.** When a background command exits, the
sidecar files a message holding its notice and starts a turn on it. That turn reads the cluster
and talks to the provider like any other, with no new gesture from the user.

## The approval

**Approved like any other command, and it says it outlives the answer.** The request's heading
is *Run this command in the background?*, with *It keeps running after this answer, until it
exits or you stop it.* under it. The flag is stored on the approval
(`approvals.background`), so the transcript reads it off the row
(`TestBashReadsRunInBackground`, `TestABackgroundApprovalSaysSo`, the transcript's
`says on the approval request that the command keeps running`). Nothing else about the gate
changes.

## The turn the sidecar starts

**Only an exit starts one, and never at startup.** A stop, the user's or the app's, and a task a
crash lost ride the chat's next question instead (`TestANoticeRidesTheNextSend`,
`TestTheStartSweepMarksRunningTasksLost`). A turn that was cancelled or failed does not start
one, so a Cancel stops the chat (`TestACancelledTurnKicksNothing`).

**It is bounded by approvals.** Each exit comes from a command the user approved. The turn it
starts can run another only by asking again, so the chain is as long as the user lets it be.

**It is a send, with every check a send makes.** The chat must exist and not be being deleted,
its cluster must accept, the last answer's model must still resolve, and its dialect must be the
chat's. Any refusal starts nothing and leaves the notices waiting
(`TestAKickTheSendRefusesStartsNothing`). It reserves the chat's one slot, so it never runs
beside a turn the user started (`TestANoticeTurnWaitsForTheSlot`,
`TestNoticesThatPileUpRideOneMessage`). A write that fails files nothing
(`TestANoticeTurnThatCannotWriteStartsNothing`).

## Stopping

**The model stops only its own chat's commands.** `TaskStop` is ungated: it only ends what the
user already approved. It reaches its chat's tasks alone, and answers another chat's id and a
finished one with the same text, so it learns nothing of either
(`TestTaskStopStopsTheChatsOwnTask`, `TestTheModelStopsTheChatsOwnTask`).

**The user can stop any running command** from the transcript (`TestAStoppedTaskEnds`,
`TestBackgroundTaskStopReachesTheService`). Both stops send SIGTERM, then SIGKILL after 5s
(`TestAStopSendsTermFirst`, `TestAStopKillsWhatIgnoresTerm`). On Windows the job ends at once.

## Lifetime

**A chat's commands die with the chat.** `Delete` joins the chat's turn, then kills its commands
at once and waits for each row, so nothing writes after the rows go and every file is closed
before the directory does. A start during the delete is refused
(`TestDeletingAChatStopsAndJoinsItsTasks`).

**Every command dies with the app.** Stopping the service kills every command at once and waits
for its row, inside the host's 6s grace. A start behind the stop is refused, and one that
registered first is killed as it starts (`TestShutdownStopsEveryTask`,
`TestATaskStartingDuringShutdownIsStopped`, `TestAStartBehindTheStopIsRefused`).

**Bash's exit kills its group**, as in the foreground, so a `sleep &` inside a background command
goes when the command does (`TestATasksGroupDiesWithBash`).

## Limits

At most 4 commands run at once in a chat and 16 in the app. A slot is held until the command's
row is written, so the count always matches the rows still running (`TestAFifthTaskIsRefused`,
`TestASeventeenthTaskIsRefused`, `TestASlotFreesWhenTheRowIsWritten`).

## The output file and the notice

**The file is raw on disk and redacted when read.** Output goes to
`<results>/<chat>/tasks/<id>.output`, 0600, as it arrives, up to 8 MiB less room for the end
line (`TestStartingATaskRecordsItAndAnswersAtOnce`, `TestTheOutputFileStopsAtTheLimit`). It is
not redacted as it is written, since a key could straddle two writes. `Read` redacts the whole
file before taking a range ([saved output and Read](2026-09-23-saved-output-and-read.md)).

**A notice is one line, escaped, and drawn as the model's.** It names the command by the model's
description, else the command, each cut to its first line of 200 characters
(`TestANoticeHoldsOneLine`). Every field is escaped, so nothing a model or a command wrote can
close a tag (`TestANoticeEscapesEveryField`). The transcript draws the description as the model's
claim and the command spelled, as it draws a call.

## Residual

- **A crash on Linux or macOS can leave a command running.** Its row reads `lost`, and the
  notice says it may still be running. A command that writes again usually dies of SIGPIPE,
  since its pipe lost its reader. On Windows the kill-on-close job ends the tree with the
  sidecar.
- **A notice turn sends the chat to the provider without a new gesture.** What it sends is what
  the next question would have sent, and each one follows an exit of an approved command.
