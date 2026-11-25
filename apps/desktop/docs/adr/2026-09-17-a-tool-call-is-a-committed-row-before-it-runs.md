---
title: A tool call is a committed row before it runs
date: 2026-09-17
scope: sidecar
status: Accepted
---

# A tool call is a committed row before it runs

## Context

[Tool rounds live in the answer's row](2026-09-14-tool-rounds-live-in-the-answers-row.md) kept a
turn's accounting in two side tables keyed by message and written once, at settlement. So a
stranded turn had no call rows, a cancelled turn lost the call it broke in, and nothing on disk
said a tool had been asked until the turn was over. That is fine for a tool that reads the local
mirror and not for the mutating tools the schema reserves `is_mutating`, `approvals` and
`waiting_approval` for.

With [a turn is a run over its message](2026-09-17-a-turn-is-a-run-over-its-message.md) landed,
`llm_calls`, `tool_calls` and `approvals` were the last tables of the target schema. The
question here is *when* a call's row is written, and what may follow a write that fails.

## Decision

**A tool call's running row commits before the tool executes.** The row is inserted `running`
immediately before `Call`. If that write fails the tool does not run, and it and every later call
in its reply settle `failed` as `not-run`. `tool_calls` is the record of what touched the
cluster, and a mutating tool will rely on the row being there before the change is.

**A model call proceeds past a failed start write.** Its row is inserted before the provider is
contacted, but a start write that fails is logged and the call goes on, as a failed checkpoint
does. Every write is a full-row upsert by id, so the finish write heals a lost start. A model
call touches nothing of the user's — the provider bills it either way — so the committed-start
rule is the tool's alone.

**No external operation follows a failed write.** A failed model finish write ends the turn only
when the reply asked for tools, since only then would something follow; under a reply that ended
the turn, the settle write heals the row. A failed tool write ends the turn either way, and a
tool that returned keeps its real result: the read happened, and the audit trail must say what
it did.

**Settlement owns every call row and holds the one retry.** The turn keeps each call's outcome
in memory — an interrupted call closed at the interruption with `cancelled` or its safe error, a
cut tool closed by what it did — and `settleRow` upserts them all in the run's transaction, which
`persist` retries as before. A crash leaves the rows as they were; the next start closes what is
still open by the rows' own state (`stranded`), under a terminal run too, since a crash can land
between a run's settlement and its calls'.

**Usage is stored uncached and read inclusive.** `llm_calls.input_tokens` is the uncached
remainder, as the schema declares; the public numbers include the cached part and are the three
input columns summed. A call with no usage object, or an inconsistent one, stores NULL in all
four token columns rather than a clamped number.

## Alternatives considered

- **Write tool rows at settlement, as before.** Enough for a read-only tool, but the audit trail
  is then a record of turns that finished rather than of calls that ran, and an approval gate
  cannot rest on a row that does not exist yet.
- **Fail the turn on any failed write, start writes included.** Uniform, but a lost start is
  healed by the finish and a model call has no side effect to guard; it would fail turns for a
  transient write with nothing to protect.
- **Retry the mid-turn writes.** A second retry path beside settlement's, with its own stop and
  context. Settlement already retries the whole record; the mid-turn writes exist for their
  timing, not their durability.
- **Store input tokens inclusive.** No conversion, but the schema's contract and its cost formula
  are uncached-plus-caches, and one conversion at the storage boundary leaves every provider's
  decoder as it is.

## Consequences

`llm_calls`, `tool_calls` and `approvals` replace the two message-keyed tables, and the schema is
the seven tables of the target design. A turn makes two writes per model call and two per tool
call, each one row on the single writer. A tool the cancel cut has a row where it had none.
Within one run id order is call order — the one place the repo reads order off a UUIDv7, since
one process mints a run's calls.

## Revisit when

A mutating tool lands: `is_mutating`, `awaiting_approval` and `approvals` are written by nothing
yet, and the gate's shape is the next decision. Or when interrupted-call usage is worth
recording — the provider reports input before the first chunk — which this pass leaves NULL.
