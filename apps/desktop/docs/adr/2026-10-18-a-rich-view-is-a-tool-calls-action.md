---
title: A rich view is a tool call's action
date: 2026-10-18
scope: cross-cutting
status: Accepted
---

# A rich view is a tool call's action

## Context

The user asks for logs in words — *tail webapp logs*, *errors in webapp today* — and expects a
live view of exactly that, which the user and the model then look at together. The model has to
decide what to show, the user has to see it, and the record has to say what the model saw and
what it did not.

The log-views sequence settled this across its rungs (`LogsView`, the inline card, the View
section, `LogsRead`, the selection chip, `LogsWatch`), from early October 2026. This ADR writes
down the decisions the rungs shared.

## Decision

**A rich view is a tool call's action.** The sidecar's tool reads the call's arguments into an
action, the row keeps it (→ [a shown tool records its action at the
run](2026-10-08-a-shown-tool-records-its-action-at-the-run.md)), and the webview draws the action
as the view. The model opens a view by making a call, so deciding to show one is deciding to
fetch, and it never composes UI.

- **What the model reads and what the user sees are separate tools.** `LogsView` opens a view and
  hands the model a one-line receipt. `LogsRead` hands the model a bounded sample and draws it
  frozen. `LogsWatch` follows a view without the model and wakes it once. The call's kind says
  which happened, the way `Read` and `Write` do.
- **A view is what the log backend takes**: sources, their filters, an optional grep, an anchor
  and `pinToEnd`. The viewer hands it on without translating. The anchor is an edge or a moment;
  `pinToEnd` is the model's word, since *tail the logs* and *the latest lines* share an anchor and
  differ only in it.
- **The action is what was shown.** The sidecar checks each source against the mirror and turns
  *yesterday* into an absolute time; the action carries the resolved values, never what the model
  typed.
- **A change is a new call.** The model never edits a view. What the user changes by hand is the
  viewer's own state and leaves no row.
- **The focused view is window state**, beside the right sidebar's open state, out of the URL.
- **The model learns the screen at send, by pull.** The composer attaches a snapshot of the
  focused view, and the user's selected lines, to `chatSend`; the sidecar writes them into the
  question's context block. Nothing streams the viewer's state.
- **A line reaches the model through `logs.ForModel` alone**, redacted at read time. The log file
  and the viewer keep a line as the container wrote it.

## Alternatives considered

- **One log tool with a flag for "show" or "read".** The row could not say honestly whether the
  model saw the lines, and the transcript would have to look inside the arguments to draw it.
- **The model composes the view** as markup or a UI description. It would put model output on a
  drawing path and ask the model for a skill it lacks, where fetching is one it has.
- **The model edits an open view.** A view would change under its own row, and the record would no
  longer say what the user saw when.
- **Stream the viewer's state to the sidecar.** The model would see the screen without the user
  asking, and the record could not say what it saw. A pull at send puts exactly that on the record.
- **The focused view in the URL.** It is chrome, not what the window is looking at, and a reload
  should not reopen it.

## Consequences

Every view is on the record as a row, and the transcript can redraw it from the action alone. A
second rich view, such as a usage graph, is another tool with its own action and its own card;
the loop, the row and the focused-view plumbing do not change. The cost is that the model changes
a view only by asking again, so every change is a call and a row.

## Revisit when

A view needs to change faster than a model turn, or the user wants the model to follow what they
scroll to without sending.
