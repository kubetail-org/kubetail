---
title: Bound a model stream by wire silence, per dialect
date: 2026-10-17
scope: sidecar
status: Accepted
---

# Bound a model stream by wire silence, per dialect

## Context

This decision predates the file; it was recorded in `sidecar/CLAUDE.md` (*Models*) while the
`llm` wires were built.

Neither model SDK puts a deadline on a stream. A turn at high effort can think for minutes and
write no text, so a quiet transcript does not mean a dead socket. Only the wire can tell the two
apart. The SDKs also drop heartbeats before a caller sees them: anthropic's SSE decoder skips
`ping` outright, and a heartbeat is the only traffic some minutes of a turn carry. The APIs differ
in how quiet a live stream can be: Anthropic pings every few seconds through a think; the
Responses stream sends nothing between `response.in_progress` and the first summary part, which
can be minutes at high effort; a cold local model on Chat Completions loads its weights and reads
the whole prompt before it sends anything, response headers included.

## Decision

`llm` bounds every stream with an idle guard (`helpers.go`). Every event **and** every read off
the response body resets it. A stream quiet for the bound ends with `ErrStreamIdle` as the
context's cause, so a trip and a caller's cancel read apart through `context.Cause`.

The bound is the dialect's own constant, beside its wire, passed in by `Target.Stream`: a minute
for Messages (`messagesIdle`), ten for Responses, five for Chat Completions. The guard's clock is a
parameter (`idleClock`): production passes `wallClock`, a test a `fakeTimer` it fires by hand.

The fake has no guard. A test stages a quiet turn as `FailAfter(n, ErrStreamIdle)`.

## Alternatives considered

- **A whole-turn deadline.** A long think is a legitimate turn; any deadline short enough to
  catch a dead socket cuts it off.
- **Reset on text.** A turn that thinks writes no text for minutes, so this trips on live turns.
- **Reset on SDK events alone.** The SDK swallows the heartbeats, so a live Anthropic think looks
  silent. Reads off the body see them.
- **One bound for every dialect.** It would be too long for Anthropic or too short for Responses
  and a cold local model.

## Consequences

A dead socket is found within a minute on Messages and within ten on Responses. A new dialect must
choose its own bound from what its API sends while thinking. Idle tests never wait on the wall
clock.
