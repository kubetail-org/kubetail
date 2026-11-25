# Security record — a chat switches vendor, 25 September 2026

**Subject:** any send in a chat may name any model in the catalog, whichever provider wrote the
chat's earlier answers. The living model is [security-model.md](../security-model.md), and the
decision is [a chat can switch to any model](../adr/2026-09-25-a-chat-can-switch-to-any-model.md).

## What a switch sends

**The whole transcript goes to the new vendor.** After a switch, the next request carries every
question and answer so far: the cluster cards and memory notes that rode each question, every
tool call's arguments and its output, and every answer's text. A vendor that saw none of the chat
before sees all of it from then on. That is the same egress as a first send on that vendor, of a
longer history.

**No provider's payloads reach another.** A row keeps each provider's verbatim items as payloads:
signed thinking, encrypted reasoning, a call a vendor signed, a search's results. `stripForeign`
sends another provider's rows as the app's fields alone, keyed on the provider that wrote the row,
never on its dialect. `TestEveryDialectReplaysEveryOtherDialectsTurn` pins that a target's request
is the same with the source's payloads as without them, for every ordered pair of wires, and that
no source thinking is sent. `TestAChatMovesAcrossDialects` pins that the history reaches the
target with each row still naming the provider that wrote it.

**A call id is the wire's own.** A foreign call goes under an id the target's wire mints, so no
vendor's id spelling reaches another. The record keeps the writer's own.

## Who decides

**The user's pick in the composer is the consent.** The model select groups every model under
its provider's label, so the user sees which vendor the next send goes to. Nothing switches a
chat but a send the user makes.

**The model cannot switch vendor.** A subagent runs on its parent's provider:
`subagentTarget` looks the named model up in the parent's own catalog alone
(`TestAnUnknownModelWritesNoRun`). A notice turn runs on what the chat's last answer ran on.

## What is not covered

The Messages API may refuse a history `tool_use` naming a tool the request does not offer, and a
Chat Completions vendor may hold id rules the fixtures do not know. Either is a refused request,
not an egress: `make check-switch` walks one chat across the real APIs, and is run by hand.
