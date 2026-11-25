# internal/services/logs — the log cache, its pages and tails

The service reads container logs through the cluster's connection, keeps them in a file per cache
(`logstore`), and answers pages and tails over the file. A line reaches the model through
`LogsRead`, a `LogsWatch` notice and the View section, all through `logline.ForModel`; the
`LogsView` tool (`internal/tools/CLAUDE.md`) opens a view the user looks at. `README.md` is the
Fetch API's contract for a reader new to it. → [ADR: a rich view
is a tool call's action](../../../../docs/adr/2026-10-18-a-rich-view-is-a-tool-calls-action.md).

Two words name two things. A **tail** is the cursor tail a caller opens (`TailFromCursor`,
`cursorTail`, `tail.go`; [`docs/data-access.md`](../../../../docs/data-access.md)). A
**tailer** is one stream's following read beneath it, shared by every tail of that stream.

The package is layered. Each sub-package is a leaf of the one above it, and none imports `logs`:

| Package | Holds |
| --- | --- |
| `logs` | the service: `service.go`, the scope (`scope.go`) and its resolution (`resolve.go`), pages (`fetch.go`), tails (`tail.go`), the holes a record carries (`gaps.go`); `podsGone` sits with the service |
| `logs/logline` | what a line is: `Record`, its cursor and bounds, cleaning, grep, `ForModel` |
| `logs/refusal` | `Refusal`, `Code` and the one block of codes |
| `logs/iso8601` | a span of time as an ISO 8601 duration: `Parse`, `Span` (a fixed length, under a cap) and `Format`, shared by `graph` and the tools |
| `logs/kubelet` | reading one container's log through the API server: the claim, the read, its windows and limits |
| `logs/logstore` | the file |
| `logs/logstest` | the fake log server the tests read from |

The root `.gitignore` ignores `logs`, so the package has its own exception there.

## The file (`logstore`)

**`services/logs/logstore` is the log cache's file**: one owner-only SQLite database per cluster
cache at `<cache>/logs/<cache id>.db` (schema `migrations/0001_init.sql`: `streams`,
`pod_owners`, `chunks`, `lines`). → [ADR: the log cache](../../../../docs/adr/2026-10-17-the-log-cache.md).

- `Manager` (`manager.go`) hands out refcounted `Store`s by cache id. An open, close or delete runs
  outside its lock, one at a time per cache. Every operation goes through `Store.use`, and a close
  waits for them. `Clear` deletes the file; `Remove` also refuses the id for the manager's life;
  both still work after `Close`. A delete removes `-wal` and `-shm` first, or a fresh database
  would replay the old WAL. A file whose close ran out of `sqlitepool.CloseBudget` is kept as
  stale, and nothing deletes or creates its path until a retry has waited its pools out:
  SQLite's close of a file's last connection deletes the WAL by path, which would be a fresh
  file's.
- `Retention` (`retention.go`) holds every knob. Ids never come back (`AUTOINCREMENT`).
- **Only the page engine stamps a chunk's `read_at_ms`**; tailers and a tail's own walks stamp
  nothing.
  `Store.WriteHeld` holds a tail's gap fill from commit until its walk returns.
- **The janitor** (`janitor.go`) sweeps on `Interval` and past `SweepBytes`: it first retries
  closing the chunks of ended tails whose close failed, then protects open, recently read and held
  chunks, evicts over-share streams first and never-read before read, then removes chunkless
  streams whose pod `PodsGone` says is gone (`service.go`; unset, none), at most `sweepPods` pods
  a sweep, round robin, then every pod's chain whose pod has no stream left. `Store.Sweep` runs
  one by hand.
- **A pod's controller chain is filed with its streams** (`pod_owners`, `Stream.Owners`): each
  filing adds the pairs the file lacks, since the mirror can show a pod before its ReplicaSet, and
  none is removed but with the pod. `OwnedStreams` answers an owner's pods' streams with their
  oldest and newest line.
- `statements.go` is the file's `sqlstmt.Set`. **Coverage is a pure function** (`CoverageOf`,
  `coverage.go`). **A `Fill` is one read of one gap** (`fill.go`). **The trim** (`trim.go`) cuts a
  finished stream at the end of `finished_at`'s second (container times are whole seconds).

## The line (`logline`)

- **A `Record` is one line** (`record.go`): its moment, message, `StreamKey`, `Place`, runtime id
  and ordinal, and the `Gap` before it. Every `Record` carries its own cursor (`Cursor()`).
- **A line cursor names a line** (`cursor.go`): moment, container, runtime id, ordinal, as
  versioned JSON in base64url (`Encode`, `Decode`); one with no runtime id is a moment
  (`MomentAt`). A `Bound` is one end of a read's range, a cursor and whether a line at it is in
  range; `StreamPos` reads one as each stream's position. A `Direction` is `Newer` or `Older`.
  → [ADR: a log read is a window over a range](../../../../docs/adr/2026-10-23-a-log-read-is-a-window-over-a-range.md).
- **Every message goes through `KeepColor`** (`clean.go`) before the file and the wire: every control
  dropped but SGR sequences. Grep (`Matches`, over a `CompileGrep` regexp of at most `MaxGrep`
  bytes) and `ForModel` read a stored line through `StripControl`, which drops those too; a
  refusal's message goes through `StripControl` alone.
- **A line reaches the model through `ForModel` alone** (`formodel.go`): stripped again, then
  redacted, at read time. The file and the wire to the viewer keep it unredacted.

## Refusals (`refusal`)

**Every refusal and notice carries a `Code`** from the one block in `refusal.go`, built through
`refusal.New`; the wire and the tools pass it through, read through `refusal.As`, and no layer
derives one from an error's identity. `Refusal` is a value, so no caller can change a sentinel's
words. `Unmirrored` and `NoNodes` wrap one with what the mirror lacked. The sentinels a scope
raises (`ErrNoCache`, `ErrSyncing`, `ErrTooMany`) live in `resolve.go`.

## The kubelet reader (`kubelet`)

- **A container's log is read over a `Claim`** (`claim.go`), one raw `GET` of the `log` path
  through the connection's `HTTPClient`; a cut body answers its reason, never a bare cancel. A
  tail's claim (`Open`) also watches the record and revokes on disable or delete.
- **A read goes through a `Backend`** (`backend.go`) over a `Log`, the container's log as the
  kubelet serves it; `Kubelet` (`kubelet.go`) is the one backend, and `Service.backends` the
  ordered list, a test field. A read takes `Options` and answers a `Read`, its `Item`s in the
  order read (`read.go`). Only the forward read and the first `tailLines` window follow kubetail.
  **A backend writes every line it reads**; the page's test of a line (`pageQuery.keep`: the
  grep inside the range, every line in the padding) only counts what fills a page
  (`filler.pull`, through `Options.Keep`) and decides what a walk passes. **`reader.lines` is the one place a bound is
  applied.** **`FailureKind` is the one classification of a failed read**: fatal (401, revoked)
  fails the page or tail; denied (403) and ended (other <500) end one stream; transient (5xx,
  408, 429, and any error with no status, a transport's included) is skipped by a page and retried
  by a tail; limit (`LoudSecond`, checked first) stops the stream at a second too loud to read.
- **A backward read sizes its windows** (`sizing.go`). → [ADR: backward log reads assume normal
  logs](../../../../docs/adr/2026-10-18-backward-reads-assume-normal-logs.md). Off the log's end it
  reads one `tailLines` window, read whole, then windows of time ending where it began, the first
  sized from that window's rate; under an upper bound, windows of time alone, the first sized from
  the rate of the lines just above the gap (`filler.rateAbove`; ten seconds with none). They start
  at the top of a second, and each is sized to hold about the page's kept lines with half again and
  at most its byte target. A window counts each line as the body carried it, before cleaning
  (`readLine`'s size); a rate from the file estimates that (`WireBytes`). A window checks the rate
  of its newest second as it reads; one due to hold four times the page or four times its byte
  target is stopped (`Item.WindowCut`), left as an island, and replaced (`reader.replace`): from
  the second after its stop, by the one-second window when stopped in its last second or once the
  stream has reached a limit, or halved when stopped in its first, whose second then bounds
  widening until a window from it completes. A window that leaves the read short is followed by an
  older one sized from its own rate, eight times longer after silence.
- **A page reads within limits** (`limits.go`: a `Quota`, and the `Limits` each stream spends).
  16 MiB and 16 GETs a stream, 64 MiB a query (each direction of a page is one), 1 MiB in one
  second. Each stream filled gets an allowance, the smaller of 16 MiB and 64 MiB ÷ streams, which
  it spends until its first window ends; the rest seeds a pool shared by later windows, and what
  is left of an allowance joins it then. A forward read reserves nothing: it spends its allowance,
  then the pool. A later window of time targets a quarter of its stream's share of the pool, the
  pool divided by the streams still reading from it with no reservation out, and reserves four
  times it (`Limits.take`); what it does not use goes back. A reservation is charged, not
  enforced. A fill reads no gap past its stream's bytes or GETs, and gives back what it holds and
  leaves the pool when it ends (`Limits.Ended`). A backward read checks its limits only between
  windows: it is cut once its bytes or GETs are spent or its next target is under 64 KiB
  (`Limits.reached`), and a window begun or replaced past that point is the one-second window; a
  forward read is cut inside its GET, never before its first line in range, and a forward read
  from a second's last nanosecond starts at the next second (`sinceSecond`). **Every cut has an
  edge**: a line, or an empty window's start, a moment (`streamFill.stop`, which the walk stops
  at). A second past 1 MiB, counted as each line's parts are read, is a `LoudSecond`: it stops a
  wider window, and stops the stream from the one-second window, `wholeMoment` or a forward read;
  the page is cut at that second (backward its top, forward its last nanosecond), names the stream
  in a `stream-skipped` notice, and is never refused for it. A tail's fill reads under the
  smaller of 16 MiB and 64 MiB ÷ its streams (`FollowLimits`), reads every second whole and is cut
  only at a second's end.

## The service (`logs`)

**`scope.go` is a read's vocabulary** (`Scope`, what a read covers, with `SourceKind`,
`FilterField`, `MirrorKind`). `Scope`'s JSON is stored in the three log tools' calls
(`tools.LogsScopeInput`), so a tag change here bumps each tool's contract. "View" is the panel the
user looks at. `service.go` is `Service`, built in `app` as `logs.New`, which owns the log store
and is the cluster service's `CacheFileOwner`.

- **A scope resolves inside one `ReadActive`** (`resolve.go`); workloads walk down controller
  references (`podsOf`). With no container named, each pod's default (`containersOf`: the
  `default-container` annotation, else the first, never an init container). A `stream` is named by
  its `StreamKey` and carries its instance's runtime id; it also carries the snapshot its file row
  keeps once the pod is gone, and its pod's controller chain (`owners`, `podOwners`), and
  `stream.log` is what a backend reads. The target carries each source's UID (`uidOf`). A source with nothing now
  is not refused: it carries a `SourceState` (`Reading`, `Missing`, `Idle`). Refused:
  `Unmirrored`, `NoNodes`, more than `MaxStreams` containers, or a reading cut at its row or byte
  cap. Tests run over `cluster/clustertest`.
- **`Service.Fetch` answers one window query from the file** (`fetch.go`, in sections: the API and
  the page engine, the page cursor, the resolved window, the streams a page reads, the filler, the
  walker, the merge, the caps). A `FetchRequest` is a first page, its `Scope`, its `Window`
  (`Since`/`Until`, inclusive moments, padded `Before`/`After` by lines or a span) and `PageFrom`
  (`PageFromFirst`, `PageFromLast`), or a continuation, a `Cursor`, with `Seen` beside a newer
  one merged into its entries (`mergeSeen`). A first page from either end may send `Seen`, with
  no padding. `PageSize`, `MaxMessageBytes`, `MaxPageBytes` and
  `ForModel` are named on every page; `ForModel` redacts each message through `logline.ForModel`
  before `capMessage` cuts it, so a cut never leaves a fragment of a secret. A `FetchResponse`
  echoes the request as it ran, with `ResolvedAt`, the first page's clock, its records oldest first, its
  `Notices` (never nil), and its place as `Page{Prev, Next, Follow}`. The rules are `Fetch`'s doc
  comment. → [ADR: a log read is a window over a range](../../../../docs/adr/2026-10-23-a-log-read-is-a-window-over-a-range.md).
  **A page cursor holds everything a continuation needs** (`pageCursor`): the scope, the window
  as the first page pinned it (`resolvedWindow`: `ResolvedAt`, `Start`), the way it reads, where
  each stream goes on from (`seen`, one line cursor per container by name: newer cursors, and an older one from a first page's `Seen`, each entry moved to its stream's reach as `edges` writes it), a
  floor for streams with none (`at`), and each counted side's distance; deflated JSON in base64url,
  versioned, never expiring, holding nothing of the file. The service keeps nothing. A first page
  is a cursor from the window's edge (`firstPage`). Going newer each stream goes on from its own
  last line passed (`edges`, from what `walk` reports in `streamReach`), so a late line is on the
  next page; going older one position is exact. A first page whose own edge is a counted padding
  reads that side's padding as a second cursor the other way from the bound (`lookup`), in the
  same `page` call as the range's, and joins the two (`joinReads`); a counted side is trimmed once
  its count is spent (`trim`). `capPage` cuts messages and the page's bytes. `CompileGrep`
  refuses a bad grep, on tails too.
  The far bound is applied in the walk next to the near one (`pageQuery.far`, `clampTo`), and the
  filler clamps each gap's download to the range by passing it in through `kubelet.Options`
  (`fillGap`), so `reader.lines` stays the one place a bound is applied. A newer page in a window
  whose After is counted reads, in the fill and the walk, no more lines past the range's end than
  the count leaves (`pageQuery.full`). A page resolves, files streams, adds the streams only the
  file holds (`fileOnlyStreams`, each source's through `sourceRows` and every row through
  `fileRows`): a Pod source's gone instances, and a workload's gone pods, found by its UID in the
  file's chains, each only where a read can deliver a line of it, from the stream's own `keyFor` to
  the read's far edge (`reaches`), a one-sided bound, so a page back takes every gone pod older
  than its start. A cursor's entries position streams and add none. **Gone means the mirror no
  longer holds the pod, and the mirror is asked** (`gonePods`, through the sweep's `podsGone`, over
  the owned pods the resolve did not hand out, so most pages ask about none), never that the
  resolve handed out no stream of it or that the workload's walk no longer reaches it: a previous
  view of a pod that never restarted, and a pod the workload orphaned, read nothing from the file.
  A workload the mirror no longer holds has no UID to find its pods by, and reads none. → [ADR: a
  bounded log window reads its dropped lines back](../../../../docs/adr/2026-10-24-a-bounded-log-window-reads-dropped-lines-back.md). A gone pod's rows keep their filed place and
  default though a pod of its name runs (`podOf` by UID). Then over those streams (`pageOver`)
  fills gaps from the backends
  (`filler`), walks (`walker`) and merges (`mergePage`). **A read is written a batch at a time as it arrives** (`window`, `WriteBatch`),
  each batch checked against the mirror first and stamped. A forward read is one window: its first
  batch links down, its last links up only when it read its range whole, and it is cancelled once
  the stream holds the page. A backward read's windows (`Item.WindowEnd`) arrive newest first,
  each oldest first: a window links up only when it completes, and counts toward the page only
  then; one cut part way is an island linked to nothing, and the read's link down is written once
  it ends. `AtEnd` goes only on a completed first window. A backward read is stopped between
  windows (`Options.More`, answered once the window's last batch is written), never inside one. A
  first page's two reads fill the newer first; the older reads back at that read's rate and links
  up to its first line (`streamFill.up`). Each walk reads its share of `maxWalkBudget` (64 MiB)
  plus what its fill downloaded. A page never passes lines a stream did not read: a short stop
  becomes the cut; a short stop with no edge is an internal error in `mergePage`, unless the
  connection's retirement cut it (`errReplaced`). **A change to a read path answers to
  `TestPagesJoinWithoutLossOrRepeat` and `TestTheFileKeepsItsInvariant`** (seeded; fewer seeds
  under `-short`).
- **A read that reaches its backend's beginning never links** (`rotatedAway`, which the filler
  and the tailer share; `reachedBeginning`, `reachedFirst`): a read from a chunk's last line that
  found it rotated away writes an `at_beginning` chunk with no link, so the hole is a lost gap. **A record carries the hole before it** (`Record.Gap`,
  `gaps.go`): every lost gap between two spans that lies between it and its stream's last line
  the view passes; one past a tailer's start once the tailer has committed past it. A walk gives
  a hole between two records to the later; its older edge's record, and a tail's first line on
  a stream, look back past the edge (`looker`, under `Retention.LookBytes`). → [ADR: a head read
  never links](../../../../docs/adr/2026-10-17-a-head-read-never-links.md).
- **`Service.TailFromCursor` speaks the page's vocabulary** (`tail.go`, in sections: the tail, the
  tailer, the registry). A `TailRequest` is a `Scope` and a start: now, `Since` (an inclusive
  moment, as a window's), or `Cursor` (a page's `Next` or `Follow`, whose scope it reads), with
  `Seen` beside either; and `MaxMessageBytes` and `ForModel`, cut through `capMessage` as a
  page's. It answers a `TailResponse`: the request as it ran, the start's scope and
  `ResolvedAt`, and the `Frames`. Its rules are `TailFromCursor`'s doc comment. **Either start is one
  page cursor** (`tailStartCursor`): a first page from `Since`, or the cursor with `Seen` merged in and its `Until` and `After` cleared, since a tail
  does not stop. Every stream, at open or joining later, starts at its `keyFor`. A `TailFrame`
  carries a `Record`, a `Notice` or, last, `Ended`, a `Refusal`. A tail hands out no cursor: a
  caller goes on from its start and the lines it holds, as `Seen`. **A tail whose start is not
  now reads the instances only the file holds first** (`cursorTail.open` files them through
  `fileOnlyStreams`, its workloads' gone pods included, each reaching its start; `readOnly` reads them through `pageOver` with `grepAll`, a page of
  `MaxPageSize` at a time from the start cursor): its streams are keyed by `StreamKey` and cannot
  hold them, and they never grow, so one read is enough. The open defers `NoNodes` as a page does,
  refusing it only with no such instance to read; once they are read, a scope of gone pods alone
  ends with it. A stream `failed` ends on an error says so once a generation, a `stream-skipped`
  notice, unless its container is waiting to start.
- **A tail walks the file from a watermark per stream.** One **tailer** per stream is shared through the file's
  **registry** and outlives its last tail by `TailGrace`. Tailers hand lines to **the file's
  writer** (`logstore/writer.go`) in batches; a tailer the writer cannot take from within
  `WriteWait` closes its chunk and reads from now, or ends with no tail. Every commit pings the
  file's bus (`Store.Subscribe`). A walk crosses a tailer's gap only up to `Tail.Committed`. **A
  walk that meets a live gap starts its fill on a goroutine of its own and stops** (`cursorTail.fill`);
  the fill's end comes back to the tail's loop, which walks the stream on (`filledGap`). Until
  then the stream is `filling`: no walk reads it, its watermark waits at the gap, and the view
  counts it as waiting. Lines are ordered per container only. From now it reads from
  `sinceTime`, never `tailLines=0`.
- **A tail's streams are one reconcile** (`cursorTail.reconcile`), run after every mirror event
  (settled by `tailSettle`), node change, stream end or retry. A container is tailed unless it
  ended at the `generation` the view sees now. A dropped container is held for a backoff up to
  `tailRetryCap`. A `Pod` source leaves the view once its pod is gone by UID. **An ending the user
  can read is an `Ended` frame, not an error** (any `Refusal`, via `refusal.As`); any other
  ending sets `Err`, and a cancel sets none. **Its caller is the viewer's `logsTail`.**

## Test fixtures (`logstest`)

`logstest.Server` is the fake log server every read test drives: a log per container, a follow
per request, refusals, rotation and broken bodies, served over HTTP or straight through `Get`.
`Refused` builds the error `Get` answers a refusal with; a `kubelet` test sets it to a
`StatusError`. `LineEvery`, `Tails`, `WideLines`, `LoudLine` and `Stamped` make its lines.
Every read test trusts it, so `server_test.go` pins how it answers each parameter the
kubelet honours; a change to the fake changes that test first.
The fake cluster service stays in `logs`' `testutil_test.go`; `kubelet`'s claim tests carry a
smaller one of their own. `breakFile` fails a log file's statements from under the service: a
trigger that raises fails a write, a dropped table every read of it; `logstore`'s own tests do
the same with `refuse`, `refuseClose` and `alter`. A test package keeps its
own copies of the few one-line helpers (`at`, `line`, `lineAt`) rather than importing them.
