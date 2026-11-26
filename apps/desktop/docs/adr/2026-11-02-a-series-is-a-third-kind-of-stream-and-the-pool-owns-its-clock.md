---
title: A series is a third kind of stream, and the pool owns its clock
date: 2026-11-02
scope: cross-cutting
status: Accepted
amended_by:
  - [Name every data access for its family, kind and operation](2026-11-25-name-every-data-access-for-its-family-kind-and-operation.md)
---

# A series is a third kind of stream, and the pool owns its clock

## Context

A cluster's connection detail showed its data rate as two numbers, from `clusterDataRateWatch`.
We wanted a sparkline beside them, full the moment the row opens. The stream could not give one:
each subscriber ran its own sampler, so history started when the panel opened, two panels on one
cluster sampled it twice, and a quiet connection sent nothing, so the client could not tell zero
from no news.

The schema had two live shapes. A delta watch replays a keyed set and closes it with a
`Bookmark`. A gauge sends one current value. A window of recent points is neither.

## Decision

**The connection pool samples.** `kubeconn` runs one ticker for the pool (`datarate.go`), every
`DataRateInterval` (2s). Each tick reads `Connection.Bytes` on every claimed context's connection
and appends one sample to that entry's ring, which holds `DataRateWindow ÷ DataRateInterval` (60)
samples. Each sample is numbered within its ring; readers compare numbers, never times. A
replaced or missing connection samples zero. `Lease.DataRates` reads the ring and
`Lease.WatchDataRates` nudges after each sample.

**A series is a third kind of stream.** Its rules:

1. The first frame is the whole retained window, sent even if empty. Its arrival is what a
   delta watch's `Bookmark` is: before it nothing is known, after it empty means empty.
2. Every frame is a list of points, oldest first, each with `at`. A later frame holds only the
   points newer than the last one sent. The frame carries `intervalSeconds` and
   `windowSeconds`; their quotient is how many points the client keeps.
3. The subscription ends in `Watch`; the frame type ends in `SeriesFrame`.
4. The client folds every series with one helper, `seriesReducer`: append, then keep the last
   window ÷ interval points. It reads no times.

`clusterDataRateWatch(id)` is the first series. It has no interval argument and sends every tick.

## Alternatives considered

- **Keep a sampler per subscriber and backfill on the client.** History would still start when
  the view opens, and two views would still sample twice. The fix is one clock with a ring
  behind it.
- **Send the window as a query and stream only new samples.** Two operations race: a sample
  can land between the query and the subscription. One subscription that subscribes to the nudge
  before its first read cannot miss one.
- **A cursor so a reconnect resumes where it left off.** The window is 60 points; sending it
  again is cheaper than the cursor.
- **Trim the client's points by time.** A wall clock that steps backwards would freeze or reorder
  the line. Counting points needs no clock.
- **Send only when the rate moved, as the gauge did.** A zero is the flat part of the line, and a
  missing point cannot be told from a stalled stream.

## Consequences

- An enabled cluster always has a ring, since the cluster controller claims every one. A
  disabled cluster has none, and its history starts again when it is enabled.
- Every claimed context costs one counter read per tick, and the ring 60 small slots.
- A consumer of a series mounted into a shared urql stream misses the window, so the data-rate
  view subscribes `unshared`.
- A future series (a cache's size, say) is a ring, a frame type and a query. It does not need a
  new client fold.
