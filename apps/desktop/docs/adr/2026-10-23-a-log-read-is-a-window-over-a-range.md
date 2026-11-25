---
title: A log read is a window over a range
date: 2026-10-23
scope: cross-cutting
status: Accepted
---

# A log read is a window over a range

## Context

Two callers read logs, and each had its own shape. The model's `LogsRead` took `since`, `until`,
`mode` and `limit`; the viewer's `logsFetch` took a mode, a cursor and a moment. Neither could
ask for context around a range, cap a message's size, or recover a message that was cut. The
follow under the viewer sent checkpoints, and `LogsWatch` cut lines on its own.

A design written on 2026-10-21 proposed one API for both: a range of time, padded with context
lines, paged with cursors, with line ids, problem-details errors and a cursor that expires. It
was built in five steps (#382 to #386, the `log-fetch-api` spec sequence). This records where
the built API follows that design and where it departs.

## Decision

**A caller asks about a range and gets a window.** `logs.FetchRequest` is a `Scope` (what is read:
sources, filters, grep), a `Window` (`Since` and `Until`, padded by `Before` and `After`, each a
count of lines or a span) and a first page from the window's `First` or `Last`. A bound is a
moment, and every line stamped at it is in range, so a line's printed nanosecond in both bounds
reads that line. A count of context is a distance: `Before: 100` is the hundred lines before the
range, whichever they turn out to be. Filters apply to the whole window; grep to the range alone,
so context lines pass unfiltered.

**A page's place is a stateless page cursor.** `Prev`, `Next` and `Follow` are opaque strings
holding the scope, the window as the first page pinned it, the direction, where the page ended,
and each counted side's remaining distance; going newer, one line cursor per stream that had a
line on the page, since streams' clocks differ and "newer than the newest line" would skip a late
one. The JSON is versioned, deflated and base64url. A continuation sends the cursor alone. The
service keeps nothing, so `Fetch` is a function of its request, and no cursor expires: it holds
moments, names, runtime ids and ordinals, never a file-local value, and stays exact across a cache
clear. `Follow` is the live end's page cursor, polled through `Fetch`.

**A line cursor stays a position, respelled.** It names one line: moment, namespace, pod,
container, the instance's runtime id and its ordinal. The stream is still named by its runtime
instance, not by `previous`, as the cursor ADR
decided. It is now versioned JSON in base64url, the page cursor's form, so a line cursor sits
inside a page cursor as it is. Lines carry no printed id; a timestamp addresses a line.

**The follow is the same API's other verb.** `FollowRequest` takes the same `Scope` and message
cap, and starts from now, `Since`, or a page's `Next` or `Follow`. `Seen`, the last line the
caller holds of each stream, rides beside either start, and each named stream reads past it. The
follow sends no checkpoint.

**Callers take the API as it is.** The wire's `logsFetch` and `logsFollow` carry it field for
field. `LogsRead` prints a page cursor whole as its token and takes it back as `cursor`;
`LogsWatch` takes one the same way. The viewer's library keeps its own query union (`First`,
`Last`, `At`, `Continue`); `At` is the app's client joining two first pages, a window since the
moment and one until the nanosecond before it, since the service has no anchor. A request for
the model says so (`ForModel`), and the service redacts a message before it cuts it, so a cut
never leaves a fragment of a secret. Notices and refusals share one closed, kebab-case code
block (`logs/refusal`), passed through by every layer. `logs.View` is now `logs.Scope`; "view" is
the panel the user looks at.

## Alternatives considered

- **Server-side cursors, or an alias table.** A short id the model copies easily, at the price of
  a table to write, sweep and lose on a cache clear. A cursor of a Deployment's `MaxStreams` pods
  fits the model's inline budget; if the model copies long tokens badly, an alias belongs in the
  tool layer, where the cost is.
- **The tab-separated line cursor.** It is short, but it cannot sit inside a JSON page cursor
  without a second escaping, and two spellings of one position is one too many.
- **Line ids, as the design had them.** A printed id per line would spend most of the model's
  budget on lines it never addresses. A nanosecond timestamp names a line as well.
- **A bound that names a line.** Moments alone keep the window one shape; a continuation is
  exclusive through its cursor, so a page never repeats a line.
- **`cursor_expired`.** Nothing in a cursor goes stale. Evicted data refills from the kubelet or
  crosses a lost gap, as any read does. A cursor of another version is `cursor-invalid`.
- **A range-too-large refusal.** A page is already cut at its read limits and says more remains.
- **RFC 9457 problem details.** GraphQL answers inside a 200 with `extensions.code`, and a tool
  refuses with one JSON line, so the envelope's `type`, `title` and `status` have nowhere to go.
  The shared code vocabulary is kept without it.
- **An anchor in the service.** The viewer's jump-to-time is two first pages the client sends
  together; an `At` page in the service would be a third page shape for one caller.
- **A follow checkpoint.** It named the oldest watermark across the scope, so one quiet stream
  made every reconnect replay every busy one. The caller already holds its last line per stream.

## Consequences

One read path serves both callers, and every page and follow carries the same records, notices
and refusals. A page cursor is a few hundred characters for a handful of streams and grows with
the scope and the page's streams; nothing bounds a scope's size yet, so nothing bounds a cursor,
and the decoder caps only inflation (`docs/TODO.md`, *Bound a log scope's size, then cap the page
cursor*). One wrong character in a copied token is `cursor-invalid`.

A change to a cursor's fields is a version bump; nothing migrates, since nothing had shipped when
this landed. A record's `TruncatedBytes` counts only the request's cut; the file's own 16 KiB cut
at write time is still silent (`docs/TODO.md`).

## Revisit when

The model copies long tokens unreliably, or a realistic scope's cursor no longer fits the tool's
inline budget.
