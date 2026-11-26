# internal/services/logs — the log cache, its pages and tails

The service reads container logs through the cluster's connection, keeps them in a file per cache
(`logstore`), and answers pages and tails over the file. `README.md` is the Fetch and Tail
contract; the exact rules are the doc comments of `Fetch` and `TailFromCursor`. **A line reaches
the model only through `logline.ForModel`**: `LogsRead`, a `LogsWatch` notice and the View section.
→ [ADR: a rich view is a tool call's
action](../../../../docs/adr/2026-10-18-a-rich-view-is-a-tool-calls-action.md).

A **tail** is the cursor tail a caller opens (`TailFromCursor`, `cursorTail`,
[`docs/read-contracts/go.md`](../../../../docs/read-contracts/go.md)). A **tailer** is one
stream's following read beneath it, shared by every tail of that stream.

Each sub-package (`logline`, `refusal`, `iso8601`, `kubelet`, `logstore`, `logstest`) is a leaf
beneath `logs`; none imports it. The root `.gitignore` ignores `logs` folders, so this package
has its own exception there.

## The file (`logstore`)

One owner-only SQLite file per cluster cache, `<cache>/logs/<cache id>.db`. → [ADR: the log
cache](../../../../docs/adr/2026-10-17-the-log-cache.md).

- **`Manager` hands out refcounted `Store`s by cache id.** An open, close or delete runs outside
  its lock, one at a time per cache. Every operation goes through `Store.use`, and a close waits
  for them. `Clear` deletes the file; `Remove` also refuses the id for the manager's life; both
  work after `Close`.
- **A delete removes `-wal` and `-shm` first**, or a fresh database would replay the old WAL.
- **A file whose close ran out of `sqlitepool.CloseBudget` is kept as stale**, and nothing deletes
  or creates its path until a retry has waited its pools out: SQLite's close of a file's last
  connection deletes the WAL by path, which would be a fresh file's.
- Every knob is in `Retention`. Ids never come back (`AUTOINCREMENT`).
- **Only the page engine stamps a chunk's `read_at_ms`**; tailers and a tail's walks stamp
  nothing. `Store.WriteHeld` holds a tail's gap fill from commit until its walk returns.
- **The janitor** protects open, recently read and held chunks, evicts over-share streams first
  and never-read before read, then removes chunkless streams whose pod `PodsGone` says is gone (at
  most `sweepPods` pods a sweep, round robin), then each chain whose pod has no stream left.
- **A pod's controller chain is filed with its streams** (`pod_owners`): each filing adds the
  pairs the file lacks, since the mirror can show a pod before its ReplicaSet, and none is removed
  but with the pod.
- **Coverage is a pure function** (`CoverageOf`). **A `Fill` is one read of one gap.** **The trim**
  cuts a finished stream at the end of `finished_at`'s second (container times are whole seconds).

## The line (`logline`)

- **Every `Record` carries its own cursor.** A line cursor is versioned JSON in base64url; one with
  no runtime id is a moment. → [ADR: a log read is a window over a
  range](../../../../docs/adr/2026-10-23-a-log-read-is-a-window-over-a-range.md).
- **Every message goes through `KeepColor` before the file and the wire**: every control dropped
  but SGR sequences. Grep and `ForModel` read a stored line through `StripControl`, which drops
  those too; a refusal's message goes through `StripControl` alone.
- **`ForModel` strips again, then redacts, at read time.** The file and the viewer's wire keep the
  line unredacted.

## Refusals (`refusal`)

**Every refusal and notice carries a `Code`** from the one block in `refusal.go`, built through
`refusal.New`. The wire and the tools pass it through and read it through `refusal.As`; no layer
derives one from an error's identity. `Refusal` is a value, so no caller can change a sentinel's
words. The sentinels a scope raises (`ErrNoCache`, `ErrSyncing`, `ErrTooMany`) live in
`resolve.go`.

## The kubelet reader (`kubelet`)

- **A container's log is read over a `Claim`**: one raw `GET` of the `log` path through the
  connection's `HTTPClient`. A cut body answers its reason, never a bare cancel. A tail's claim
  also watches the record and revokes on disable or delete.
- **A backend writes every line it reads.** The page's test of a line (`pageQuery.keep`: the grep
  inside the range, every line in the padding) only counts what fills a page and decides what a
  walk passes. Only the forward read and the first `tailLines` window read as kubetail's own
  reader does.
- **`reader.lines` is the one place a bound is applied.** The walk applies the far bound beside
  the near one, and the filler passes the range to the read through `kubelet.Options`.
- **`FailureKind` is the one classification of a failed read**: fatal (401, revoked) fails the
  page or tail; denied (403) and ended (other <500) end one stream; transient (5xx, 408, 429, and
  any error with no status) is skipped by a page and retried by a tail; limit (`LoudSecond`,
  checked first) stops the stream at a second too loud to read.
- **A backward read sizes its windows from nearby rates** (`sizing.go`) and **a page reads within
  limits** (`limits.go`): the numbers and the reasoning are the ADR's. → [ADR: backward log reads
  assume normal logs](../../../../docs/adr/2026-10-18-backward-reads-assume-normal-logs.md).
  - A window counts each line as the body carried it, before cleaning; a rate from the file
    estimates that (`WireBytes`).
  - A reservation is charged, not enforced. A fill reads no gap past its stream's bytes or GETs,
    and gives back what it holds when it ends (`Limits.Ended`).
  - A backward read checks its limits only between windows; a window begun or replaced past them
    is the one-second window. A forward read is cut inside its GET, never before its first line in
    range.
  - **Every cut has an edge**: a line, or an empty window's start (`streamFill.stop`, where the
    walk stops).
  - A second past 1 MiB is a `LoudSecond`. The page is cut at that second, names the stream in a
    `stream-skipped` notice, and is never refused for it.
  - A tail's fill reads under `FollowLimits`, reads every second whole and is cut only at a
    second's end.

## The service (`logs`)

**`Scope`'s JSON is stored in the three log tools' calls** (`tools.LogsScopeInput`), so a tag
change in `scope.go` bumps each tool's contract. "View" is the panel the user looks at. `Service`
owns the log store and is the cluster service's `CacheFileOwner`.

- **A scope resolves inside one `ReadActive`**; workloads walk down controller references. With no
  container named, each pod's default: the `default-container` annotation, else the first, never
  an init container. A source with nothing now is not refused: it carries a `SourceState`.
  Refused: `Unmirrored`, `NoNodes`, more than `MaxStreams` containers, or a reading cut at its row
  or byte cap.
- **`ForModel` redacts each message before `capMessage` cuts it**, so a cut never leaves a fragment
  of a secret.
- **A page cursor holds everything a continuation needs and nothing of the file**: deflated,
  versioned JSON in base64url, never expiring. The service keeps nothing. Going newer, each stream
  goes on from its own last line passed (`edges`); going older, one position is exact.
- **A page adds the streams only the file holds** (`fileOnlyStreams`): a Pod source's gone
  instances and a workload's gone pods, found by its UID in the file's chains, each only where the
  read can deliver a line of it (`reaches`). A cursor's entries position streams and add none.
  **Gone means the mirror no longer holds the pod, and the mirror is asked** (`gonePods`, through
  `podsGone`), never that the resolve handed out no stream of it or that the workload's walk no
  longer reaches it. A gone pod's rows keep their filed place and default though a pod of its name
  runs (`podOf` by UID). → [ADR: a bounded log window reads its dropped lines
  back](../../../../docs/adr/2026-10-24-a-bounded-log-window-reads-dropped-lines-back.md).
- **A read is written a batch at a time as it arrives**, each batch checked against the mirror
  first and stamped.
  - A forward read is one window: its first batch links down, its last links up only when it read
    its range whole, and it is cancelled once the stream holds the page.
  - A backward read's windows arrive newest first. A window links up, and counts toward the page,
    only when it completes; one cut part way is an island linked to nothing. The read's link down
    is written once it ends. `AtEnd` goes only on a completed first window. A backward read stops
    between windows (`Options.More`), never inside one.
  - A first page's two reads fill the newer first; the older links up to its first line
    (`streamFill.up`).
- **A page never passes lines a stream did not read**: a short stop becomes the cut, and one with
  no edge is an internal error in `mergePage` unless the connection's retirement cut it
  (`errReplaced`).
- **A change to a read path answers to `TestPagesJoinWithoutLossOrRepeat` and
  `TestTheFileKeepsItsInvariant`** (seeded; fewer seeds under `-short`).
- **A read that reaches its backend's beginning never links** (`rotatedAway`, shared by the filler
  and the tailer): it writes an `at_beginning` chunk with no link, so the hole is a lost gap. →
  [ADR: a head read never links](../../../../docs/adr/2026-10-17-a-head-read-never-links.md).
- **A record carries the hole before it** (`Record.Gap`, `gaps.go`): every lost gap between it and
  its stream's last line the view passes, and one past a tailer's start once the tailer has
  committed past it. A walk gives a hole between two records to the later. Its older edge's
  record, and a tail's first line on a stream, look back past the edge under
  `Retention.LookBytes`.

### Tails (`tail.go`)

- **Either start is one page cursor** (`tailStartCursor`): a first page from `Since`, or the
  cursor with `Seen` merged in and its `Until` and `After` cleared, since a tail does not stop.
  Every stream, at open or joining later, starts at its `keyFor`.
- **A tail whose start is not now reads the instances only the file holds first**
  (`fileOnlyStreams`, then `pageOver` with `grepAll`, a page of `MaxPageSize` at a time): its
  streams are keyed by `StreamKey` and cannot hold them, and they never grow. The open defers
  `NoNodes` as a page does, refusing only with no such instance to read; a scope of gone pods
  alone then ends with it.
- A stream that ends on an error says so once a generation, in a `stream-skipped` notice, unless
  its container is waiting to start.
- **A tail walks the file from a watermark per stream.** One tailer per stream is shared through
  the file's registry and outlives its last tail by `TailGrace`. Tailers hand lines to the file's
  writer (`logstore/writer.go`) in batches; a tailer the writer cannot take from within
  `WriteWait` closes its chunk and reads from now, or ends with no tail. Every commit pings the
  file's bus. A walk crosses a tailer's gap only up to `Tail.Committed`. From now a tailer reads
  from `sinceTime`, never `tailLines=0`.
- **A walk that meets a live gap starts its fill on a goroutine of its own and stops**
  (`cursorTail.fill`); the fill's end comes back to the tail's loop (`filledGap`). Until then the
  stream is `filling`: no walk reads it, its watermark waits at the gap, and the view counts it as
  waiting.
- **A tail's streams are one reconcile** (`cursorTail.reconcile`), run after every mirror event
  (settled by `tailSettle`), node change, stream end or retry. A container is tailed unless it
  ended at the `generation` the view sees now. A dropped container is held for a backoff up to
  `tailRetryCap`. A `Pod` source leaves the view once its pod is gone by UID.
- **An ending the user can read is an `Ended` frame, not an error** (any `Refusal`); any other
  ending sets `Err`, and a cancel sets none. Its caller is the viewer's `logsTail`.

## Test fixtures (`logstest`)

`logstest.Server` is the fake log server every read test drives. Every read test trusts it, so
`server_test.go` pins how it answers each parameter the kubelet honours: a change to the fake
changes that test first. A `kubelet` test sets `Refused` to a `StatusError`. The fake cluster
service stays in `logs`' `testutil_test.go`; `kubelet`'s claim tests carry a smaller one.
`breakFile` fails a log file's statements from under the service (a raising trigger fails a write,
a dropped table every read of it); `logstore`'s tests do the same with `refuse`, `refuseClose` and
`alter`. A test package keeps its own copies of the one-line helpers (`at`, `line`, `lineAt`)
rather than importing them.
