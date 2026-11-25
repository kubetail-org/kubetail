# Security record — a settled call reads by its kind, 15 November 2026

**Subject:** a settled call's row in the transcript leads with its kind and the model's
description, and the command moves into the call's body. The calls fold into the answer's
work disclosure. This changes the transcript half of
[the command description](2026-09-23-the-command-description.md); the approval request is
untouched. The living model is [security-model.md](../security-model.md).

## What changes

**The row is the kind, then the description.** A settled call's row is its kind in the app's
words (*Command*, *Read*, *Fetch page*), upright and medium, then the model's description, italic
and bare, one `truncate` line through `ModelDescription`. A call with no description shows its
target in the row as before: the command, the path, the URL or the query, spelled. The tag
speaks only when something went wrong or is still going; any failure reads *error*, and the
output's first line keeps the exit code or the timeout.

**The command is one click away.** Open, the call's body starts with `$ ` and the command,
spelled through `VisibleText`, in the foreground, above what it printed. The directory follows as
*Ran in …*, with *sandboxed* and *with network* as before.

**The calls fold into the work.** Settled calls sit in the answer's *Worked for* disclosure, which
is closed once text arrives. These stay under the answer: a log card, a running background task
and its Stop, a write or edit that ran unasked (its content drawn open), and a failed sandboxed
command's *Grant a folder…*.

## What this gives up

**A lying description now names a call until it is opened.** "List files" can sit on the row of an
`rm` that ran. The transcript is a record, not a gate: by the time a row is settled, the command
ran or was refused. The kind is the app's and leads the row, the description reads as the model's
by its italic, and the command that ran is in the body, unchanged and spelled.

## The bound is unchanged

**The approval request still decides on the command.** It is drawn exactly as before: the command
whole, every invisible character spelled, the description above it after *The model says:*, quoted
(`draws the model's description above the command, labelled and quoted`).

**What ran is still what is shown.** The body's command is the action's `text`, read by the
sidecar from the arguments, never the description (`draws a settled command by its kind and
description, the command in its body`). The row has no `title`, and an override in the command or
the description cannot reorder the tag (`gives the summary no title`, `spells an override in the
command, keeping the tag in place` and its twin for the description).

**Nothing that needs the user's eye is folded.** An approval request is never in the work
disclosure, nor is an unasked change, a task the user may stop, or a grant offer (`leaves a log
card, a running task and a grant offer outside`).
