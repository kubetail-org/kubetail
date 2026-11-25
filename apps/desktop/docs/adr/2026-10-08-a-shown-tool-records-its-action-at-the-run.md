---
title: A tool whose action is what its run showed records it at the run
date: 2026-10-08
scope: cross-cutting
status: Accepted
amends:
  - [A tool call shows itself from its arguments, and an approval is only the decision](2026-09-23-a-tool-call-shows-itself-from-its-arguments.md)
amended_by:
  - [A tool call records its tool's contract beside its stored JSON](2026-10-25-a-tool-call-records-its-tools-contract.md)
---

# A tool whose action is what its run showed records it at the run

## Context

[A tool call shows itself from its arguments](2026-09-23-a-tool-call-shows-itself-from-its-arguments.md)
has every stored call read through its tool's `Action(arguments, cwd, sandboxed)` on every read,
so nothing but the arguments and the two gate columns says what a call did, and a read
recomputes it the same way for every row ever written.

`LogsView` (`docs/specs/log-views/`) breaks that for two values. The view it opens shows a
source's default container, which the tool resolves off the mirror at the run, and an anchor
the model typed as a duration, `2m`, which is a moment only on the clock the run read. The
action must carry both as shown: the webview mounts the viewer off it, and the record must say
what the user saw. A read that recomputed them later would read a mirror and a clock that have
moved.

## Decision

**A `tools.Shown` tool resolves its action at the run, and the row keeps it.** `Shown` is a
`Runner` whose `RunShown(ctx, rt, input)` answers the text, the error flag and the action it
showed, nil on a refusal; `Run` on such a tool is `RunShown` with the action dropped. The loop
hands the action to `Recorder.ToolCallFinished` beside the result, and `agent/chat` writes it as
`tool_calls.shown_action`, JSON of the `tools.Action`, with the result, NULL on every other call and
on a refusal. A read serves the stored action when the row has one, and the arguments' reading
through the box otherwise (`actionOf` in `record.go`).

A `Shown` tool's `Action(arguments)` answers no action: a call of it that never ran, or was
refused, shows its kind alone, as a refused call of any tool does.

## Alternatives considered

- **Recompute at read, from the arguments.** What the arguments say is what the model typed:
  `containers` absent reads as every container where the run showed one, and `2m` has no
  moment. The record would say something other than what the user saw.
- **Have the model type what the run resolves.** The model does not know the default
  container or the time of day, and asking it to look both up first is two more calls for
  every view.
- **Carry the resolved values on the gate's columns**, as `cwd` and `sandboxed` are. Those are
  bash's, written before the run, and a view's resolution needs the whole action.
- **Let the tool write the row itself** through the runtime. The loop's recorder is the one
  writer of a call's row, and the result write is where the action belongs.

## Consequences

A tool with an action the arguments cannot say has a place for it, and the read path has one
rule: the row's action when the run recorded one, the arguments' otherwise. The cost is one
column and one more argument on `ToolCallFinished`. The record's claim that a stored action is
what the user saw now rests on the run writing it with the result, which the settle's rewrite
of every row carries too.

## Revisit when

A second tool needs the run's resolution read back for a purpose the stored JSON does not
serve, such as a query over it, which a column per field would answer and this one does not.
