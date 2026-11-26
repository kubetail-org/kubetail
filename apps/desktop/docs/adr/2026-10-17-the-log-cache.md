---
title: Keep a cluster cache's log lines in a file of its own, evicted by read
date: 2026-10-17
scope: sidecar
status: Accepted
---

# Keep a cluster cache's log lines in a file of its own, evicted by read

## Context

Every log page and every follow made its own requests to the API server. The webview kept every
line it had read, because it could not count on reading one again: a backward page was
kubetail's doubling `tailLines` windows, the kubelet rotates logs, and a pod takes its log with
it when it goes. So two windows on one pod were two follows, the model's `LogsView` read nothing
a viewer could share, scrolling back read the same bytes again, and a long follow grew the
webview without end.

The sidecar already kept one SQLite file per cluster cache, the mirror (`kubestore`), with a
manager, a janitor and a schema policy we trusted. The design was settled across the log cache
sequence (`docs/specs/log-cache/`), built in six pull requests into `wip/log-views`.

## Decision

**One SQLite file per cluster cache, `<cache>/logs/<cache id>.db`**, owned by `services/logs`
through `logstore`'s `Manager`, shaped like `kubestore`'s: refcounted, created on first read,
cleared with `clusterCacheClear` and removed with the cache, through `cluster`'s
`CacheFileOwner`. `logsFetch` and `logsFollow` answer from it, and a gap is filled from the API
server.

- **Coverage is per stream and per chunk, claimed only from a read's first returned line.** A
  chunk says *every line the backend served for this stream between its edges that matches its
  pattern is in `lines`*. A forward read starts its chunk at the first line it got, never at the
  `sinceTime` it asked for, and a link between chunks is the backend's word that nothing lies
  between them.
- **A stream is a runtime instance, keyed by its `container_id`, and `previous` is derived.** An
  instance keeps one row and one set of lines whether it is read as current or, after a restart,
  as previous; the wire's `previous` is computed at read time.
- **A stream's place, labels and times are a snapshot from the mirror**, written when the row is
  made and updated while the instance runs, since the mirror holds only what is in the cluster
  now and the lines outlive the pod.
- **Eviction is LRU by read under a byte budget.** The janitor evicts while the file is over
  `Bytes` or a stream over `StreamShare`, never-read chunks first, oldest first within each. It
  protects open chunks and chunks a page stamped within `ReadTTL`. A tail's write and a follow's
  fill stamp nothing.
- **Backends are tiered, and a range has one backend.** `Backend` is the one seam a line enters
  through. A gap goes to the cluster's backends in order, the next asked only for what the one
  before said it lacks, and a chunk records its `origin`. A line's ordinal is its backend's, so a
  line keeps its key whichever fill found it. The kubelet is the only backend built.
- **One tailer per stream, shared, and the follow reads the file.** A tailer writes through the
  file's writer and pings a bus on every commit. A follow walks the file from a watermark per
  stream on each ping, so every line reaches a reader by one path. A tailer outlives its last
  follow by `TailGrace`, and one that falls behind the writer reopens from now, leaving a live gap
  a reader fills.
- **Messages are stored as text.** The path for later, if the budget turns out short, is
  `sqlite-zstd`'s: zstd with a dictionary trained per stream, per row behind a view, with the
  codec registered on the driver as `kubestore` registers `body()`.
- **Grep is a Go filter over each stream's walk.** A backend is never handed it, so the kubelet
  fills only unlabelled chunks; the `pattern` column is there for a backend that greps on its
  side.

The file is cluster data in the clear, under the decision
[the cache is ordinary application data](2026-09-02-the-cache-is-ordinary-application-data.md),
and the model reaches no line through it.

## Alternatives considered

- **The webview keeps every line.** What we had. It cannot share a read between two windows or
  with the model, and its memory grows with every follow.
- **A session table** holding a viewer's reads. It needs a lifetime the sidecar cannot see the
  end of, and stamping what a page read gives the same protection with nothing to track.
- **Coverage from the requested `sinceTime`.** Simpler, and wrong after a rotation: the kubelet
  answers from the oldest line it still has, so the chunk would claim lines it never saw.
- **`previous` on the stream's row.** The same instance is current while it runs and previous
  after a restart, so storing it would keep its lines twice.
- **A line cap on a tailer's grace.** A noisy stream is the one whose gap the kubelet can least
  refill, so it is the stream the grace helps most. Ending a graced tailer that falls behind the
  writer bounds it instead.
- **Eviction by age.** A quiet pod's hour-old lines that someone is reading are worth more than a
  firehose's minute-old lines nobody opened.
- **Protecting what a tailer wrote for `ReadTTL`.** It would hold ten minutes of every tailed
  stream whatever the budget said.
- **Compressing each message.** The redundancy in a log is between lines. SQLite's transparent
  options are closed: ZIPVFS is proprietary C that `modernc.org/sqlite` cannot load, and
  filesystem compression is not portable.
- **Grep as a SQL function.** It copies every message into Go before matching it, which the walk
  already does.

## Consequences

A line read again is an index lookup that answers what it answered before, while the budget
keeps it, and a refill from the kubelet after that. One tailer serves every reader of a stream.
The file keeps a log across restarts and after its pod is gone, which the kubelet cannot.

The file holds logs in the clear, and logs hold secrets more often than object bodies do; the
security model gives it a row of its own. A line evicted from a rotated log is gone: a page
crosses it as a lost gap. The budget is message bytes, so the file on disk runs over it by its
indexes and the WAL.

The invariant someone could break without noticing is the one `TestTheFileKeepsItsInvariant`
holds: rows and chunks change in one transaction, and no chunk is missing a line the backend
served into its range that matches its pattern. A new backend must classify its own failures,
cache nothing for a denied stream, and stop every read when the cluster is disabled or deleted.

## Revisit when

The budget's reach is measured short, which is when the dictionary path is taken. A backend
that greps on its side, or an archive, lands: each fits the schema, but brings its own cursor and
resolve changes.
