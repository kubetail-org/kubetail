---
title: A tool call records its tool's contract beside its stored JSON
date: 2026-10-25
scope: sidecar
status: Accepted
amends:
  - [A tool call shows itself from its arguments, and an approval is only the decision](2026-09-23-a-tool-call-shows-itself-from-its-arguments.md)
  - [A tool whose action is what its run showed records it at the run](2026-10-08-a-shown-tool-records-its-action-at-the-run.md)
---

# A tool call records its tool's contract beside its stored JSON

## Context

A `tool_calls` row keeps JSON that later builds read back: `arguments`, which the tool parses
on every read, and `shown_action`, a `tools.Action`. A `LogsWatch` call's task keeps a third, a
`tools.WatchResult`. A vendor's tool already recorded its shape in `contract_name`
(`web_search_20260318`); ours recorded nothing. The only way to change one of our shapes was a
new tool name ([a tool call shows itself from its
arguments](2026-09-23-a-tool-call-shows-itself-from-its-arguments.md)), which also changed what
the model is offered. A stored shown action or watch result that did not decode failed the whole
chat's read, and a renamed field decoded silently as its zero value. Issue #356 asked whether a
version would help.

## Decision

Every tool answers `ContractName()` on `tools.Reader`. Ours are `<name>/v<n>`, the name
lower-cased (`bash/v1`); a vendor's stays its own identifier. `newToolCall` records the contract
from the service's box on the row's first write, and no later write changes it. The box reads a
row only under its reader's current contract: `Box.Action` for arguments, `Box.Shown` for a shown
action, and `Box.WatchResult` for a watch's result. A payload that is
refused or does not decode is no action, not a failed read, and logs one warning carrying a bare
sentinel (`tools.ErrDecode` and its siblings), never the decoder's error, which can quote the row.
A change that would read an old row differently bumps the tool's contract; the first bump moves
the check into the tool, whose decoders then read every contract it has written.

`approvals.request` is the gate's record, not a tool's, and is not versioned here. A request that
does not decode is served with its approval and a null `ClusterWrite.action`, and offers Deny
alone.

## Alternatives considered

- **A version field inside the JSON.** `arguments` is the model's bytes verbatim, drawn by the
  approval request and sent back in the history; adding to it changes what the model reads. A
  field in `shown_action` alone would leave the arguments unversioned.
- **A new tool name per shape.** The name is what the model is taught, so a storage change would
  change the offer, and the prompt goldens with it.
- **A new column.** `contract_name` already holds a vendor's shape; filling it for ours needs no
  schema change and one rule for both.
- **Reading a NULL contract as `v1`.** It would make the oldest rows depend on a guess. NULL
  means no tool gave the row a contract, and such a row shows its kind alone.

## Consequences

- A tool can change what it stores without changing its name, and every row written before reads
  as it did, pinned by append-only fixture tables (`TestActionOfReadsOldRowsTheSame`,
  `TestShownOldRowsDecodeTheSame`).
- A row written by a newer build, or one with no contract, shows its kind and nothing it might
  misread. Nothing had shipped, so there was no backfill; a dev database is reset.
- One corrupt row costs its chat one action and logs on every read until it is removed.
- `tools.Native` is gone: once every tool answers a contract, it named nothing `Tool` did not.
- `LogsScopeInput` is an alias of `logs.Scope`, so a JSON tag change there bumps three tools.

## Revisit when

The gate's own record needs to change shape: `approvals.request` then needs a version of its own.
