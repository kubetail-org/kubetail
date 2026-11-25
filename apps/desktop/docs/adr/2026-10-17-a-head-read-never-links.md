---
title: A head read never links, and a line carries the hole before it
date: 2026-10-17
scope: sidecar
status: Accepted
---

# A head read never links, and a line carries the hole before it

## Context

The kubelet keeps only the recent part of a container's log. A line rotated away before the log
cache read it is gone. In the cache (→ [the log cache](2026-10-17-the-log-cache.md)), a link
between two chunks promises the backend holds nothing between them. A forward fill or a tailer
resuming from a chunk's last line, `prev`, still linked to it after a rotation, so the hole
joined one span and every page and follow walked past it as if nothing were missing. The
viewer could not tell a lost range from a quiet one.

## Decision

**A read that reached the backend's oldest line is a head read, and a head read never links.**
A forward read from `prev`'s second whose GET returned no line at or before `prev`, and got a
line or ended clean, found `prev`'s line rotated away (`filler.headRead`, `fill.go`). A backward
read that reached the log's first line past its lower bound did too (`reachedFirst`,
`fetcher.go`). A tailer resuming from `prev` takes the filler's test (`rotatedAway`) and opens a
head chunk (`tailer.go`). The chunk is head and has no link, so `CoverageOf` reads the hole as a
lost gap. A read from any other bound, a page's key, says nothing of rotation and links as
before.

**A record carries every marked hole between it and the line of its stream before it that the
view passes** (`Record.Gap`, `holes.go`). A marked hole is a lost gap between two spans. One
past a running tailer's start, as a tailer resumed after a rotation leaves, is marked once the
tailer has committed past it, since until then it may still write lines there. A walk
gives a hole between two of its records to the later one. The record at its older edge, and a
follow's first line on a stream, look back past the edge for theirs (`looker`), under
`Retention.LookBytes`. The wire's `LogRecord.gap` carries it; the viewer draws a notice above
the line.

## Alternatives considered

- **A list of gaps beside the page's records, or a follow frame of its own.** Merge, follow and
  the viewer's window would each need to place it. A field on the record rides through all of
  them unchanged, and a record is sent once across a tiling, so a hole is too.
- **Marking the start of a stream from `started_at`.** Most containers write nothing for a while
  after starting, so the guess would mark nearly every log (`docs/TODO.md`).

## Consequences

The gap before a stream's oldest span stays unmarked: a quiet start and a rotated one look the
same. A hole with no later line the grep passes has no carrier, and a page not following never
shows it. A hole that becomes lost after its carrier is on screen shows on the next landing. A
dev file written before this keeps its links across holes until eviction takes them.

`TestTheFileKeepsItsInvariant` holds that no span covers a line the instance wrote that the file
lacks. `TestPagesJoinWithoutLossOrRepeat` holds that each marked hole rides on exactly one
record.

## Revisit when

An archive backend lands, which fills holes and could answer for a stream's start.
