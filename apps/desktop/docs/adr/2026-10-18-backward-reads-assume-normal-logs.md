---
title: Backward log reads assume normal logs, and stop clearly where normal ends
date: 2026-10-18
scope: sidecar
status: Accepted
---

# Backward log reads assume normal logs, and stop clearly where normal ends

## Context

The log cache (→ [the log cache](2026-10-17-the-log-cache.md)) fills a gap from the kubelet. Reading
forward is easy. Reading backward, to scroll up or open a log at a moment, is not, because of what
the kubelet does (`staging/src/k8s.io/cri-client/pkg/logs/`, read on 2026-10-17):

- **Only the current log file is served.** Rotated files are never read, so a container's readable
  log is one file: `containerLogMaxSize`, 10 MiB by default.
- **`sinceTime` filters; it does not seek.** The kubelet reads the file from its first byte and
  skips older lines, so each request costs the node a scan up to its start.
- **`sinceTime` is whole seconds.** A window starts at the top of a second, and the smallest is one
  second.
- **`tailLines` counts raw file lines.** The runtime splits a long line into parts of 16 KiB, and
  each part counts.

So the kubelet cannot return "the lines before this point". A backward read picks a start time and
downloads everything from there up to the point. The start was guessed from the time span alone,
and a page's 32 MiB was split evenly across its containers, about 330 KiB each at a hundred. A
burst just before the point made the first window larger than that, and the page was refused with
*"ran out of budget"*. A retry made the same guess. In a 100-replica view where one pod was quiet
for 7 days and then burst at more than about 9 KiB/s, scrolling up stuck there for good
([#320](https://github.com/kstackhq/kstack/issues/320)). A grep was no better: the kubelet cannot
grep, so a sparse one downloads a whole log to say it found nothing.

The Kubernetes API cannot serve every log well. Most clusters sit well inside these numbers:

| Normal | Limit |
| --- | --- |
| One container's log, as the kubelet serves it | about 10 MiB (the kubelet's default) |
| One container's busiest second | at most 1 MiB, about 5,000 lines |
| One view | at most 100 containers (`maxStreams`) |

The design was settled across five pull requests into `wip/log-views`.

## Decision

**Design for normal logs and name where normal ends.** The limits above become the read's
limits, and a log past them stops with a reason the user can read.

**Every kubelet line is written as it arrives and grepped on disk.** A read's lines go to the file
in batches (`window`, `WriteBatch`), so memory stays flat. The grep decides what counts toward a
page and what the walk passes, never what is written, so a later search over the same range
downloads nothing. The file grows to 1 GiB (`logstore.DefaultRetention.Bytes`).

**Windows are sized from nearby rates, by kept lines and by bytes** (`windows.go`). The lines just
above the gap are already in the file, so the guess costs no GET. A window aims to hold the page's
kept lines with half again, and at most its byte target. It is corrected as it is read: one due to
hold four times the page or four times its target is stopped, kept as an island, and replaced from
the second after its stop, or halved when stopped in its first second. A window that leaves the
read short is followed by an older one, eight times longer after silence. Off the log's end the
read takes one `tailLines` window, then windows of time.

**The limits** (`limits.go`): 16 MiB and 16 GETs a stream, 64 MiB a query, 1 MiB in one second.
Each stream spends an allowance, the smaller of 16 MiB and 64 MiB ÷ streams, on its first window;
the rest seeds a pool its later windows share. A later window targets a quarter of its stream's
share of the pool, at most 4 MiB, so a sparse grep reads a default log in about three GETs. It
reserves four times its target, the most it reads before it is stopped. A reservation is charged,
not enforced, so no read ends without an edge. **The walk follows the fill**: each stream's walk
reads its share of 64 MiB plus what its fill downloaded.

**Cut for work, stop only for the abnormal.** A page past its limits is cut with `more: true`, and
the next page goes on from its edge. Every cut has an edge, a line or an empty window's start, so a
page always makes progress. Only a second past 1 MiB is abnormal: it stops its stream, cuts the
page at that second, and names the stream in `skipped` (`failLimit`). The rest of the view goes
on.

## Alternatives considered

- **Windows sized from the time span alone.** What we had. A burst near the point makes the window
  huge, and the same guess fails the same way on every retry.
- **`tailLines` offsets kept as hints.** `tailLines` counts from the log's live end, so an offset
  drifts as the log grows and breaks at a rotation. Each read downloads everything from the offset
  to the end, so reading far back costs the whole recent log every time.
- **A map of probes**, sampling the log at moments to learn its rates. Each probe is a GET and a scan
  of the node's file up to its start. The lines next to the point give a rate for free.
- **Keeping only matching lines.** It downloads as much and keeps nothing for the next search, which
  pays again.
- **Refusing a page that runs out of budget.** The refusal was the bug. A large read is normal and
  makes progress, so it cuts.

## Consequences

Scrolling up, opening a log at a moment, and a grep work for any normal log, and a search over a
range already read is free. The cost is an approximate guess: a window can over-read up to four
times its target before it is stopped, and a sparse grep over a hundred logs takes several pages.

A hostile container can stop its own stream and make pages slow. A window is not stopped in its
last second, so it can read about one second past its reservation, at most 1 MiB. The `tailLines`
window asks for no more lines than its allowance holds at 16 KiB a part (`limits.tailLines`), so it
stays within the allowance, or 160 KiB at the least. The 1 GiB file holds cluster data in the
clear, under the log cache's decision.

The invariants someone could break without noticing are the ones `TestPagesJoinWithoutLossOrRepeat`
and `TestTheFileKeepsItsInvariant` hold: every cut has an edge, and a page never passes lines a
stream did not read.

## Revisit when

Users hit the limits on ordinary clusters: the kubelet's default log size grows, or views past a
hundred containers become common. A user whose logs pass them is the case for offering kubetail,
installed in the cluster, which reads rotated files and greps on the node.
