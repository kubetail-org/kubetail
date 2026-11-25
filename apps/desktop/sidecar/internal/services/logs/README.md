# The log Fetch API

`Service.Fetch` answers one page of container log lines. You say what to read, when to read it
from, and where the page starts. The service fills its local file from the cluster, merges every
container's lines by time, and hands back one page with cursors to the pages around it.

This is the high-level contract. The exact rules are the doc comment on `Fetch` in `fetch.go`.

## One request, one page

A request is either a **first page** or a **continuation**. Never both.

A **first page** names:

- **Scope.** What to read: one or more sources (a pod or a workload), which of their containers,
  optional filters on the node a line ran on, and an optional grep over each message.
- **Window.** When to read. `Since` and `Until` bound the range. Each includes every line at that
  exact moment. Either can be open. `Before` and `After` pad the range: a number of lines, or a
  span of time, never both. The grep does not apply to padding.
- **PageFrom.** Which end the first page comes from. `PageFromFirst` serves the oldest lines of the
  window first. `PageFromLast` serves the newest.
- **Seen**, optionally, with no padding. One line cursor per container. With `PageFromFirst` it is
  the newest line you already have, and that container resumes past it. With `PageFromLast` it
  is the oldest, and that container reads from strictly before it; every `Prev` from there carries
  the entries on, each moved back as its container is read, so a container whose clock runs
  behind never comes back past it. Every other container reads from the window's edge.

A **continuation** sends one `Cursor`. The cursor came from an earlier page's `Prev`, `Next` or
`Follow`. It holds the scope and the window, so sending those again is refused. Beside a `Next` or
`Follow` it may send **Seen**, merged into the cursor's own, the newer line of a container
standing: how a caller that followed from the cursor pages on from it. Not when the window's
`After` is a count.

Every request may set three caps, and they are read on every page:

| Cap | Meaning | Default | Most |
| --- | --- | --- | --- |
| `PageSize` | lines on the page | 100 | 500 |
| `MaxMessageBytes` | longest message, cut past it | the file's own cap | the file's own cap |
| `MaxPageBytes` | sum of the page's messages | 1 MiB | 1 MiB |

`ForModel`, also read on every page, passes each message through `logline.ForModel` before the
cut, so a page for the model is cut on the redacted text.

## One response

A response carries:

- **Records**, oldest first, each with its timestamp, message, stream, and a line cursor.
- **Page**: `Prev`, `Next` and `Follow`, each a page cursor or nil.
  - `Prev` goes on toward older lines.
  - `Next` goes on toward newer lines.
  - `Follow` polls the end of a live window for lines written since. It is set instead of `Next`,
    never beside it.
- **Request**, as it ran: defaults filled, caps applied, and on a continuation the scope and
  window the cursor holds.
- **ResolvedAt**, the clock of the first page. Every page of one window reports the same one.
- **Notices**, things about this page you should know. Never nil. A continuation carries none
  forward.

## Paging

Each page's cursors carry everything the next page needs. The service remembers nothing between
calls, and a cursor never expires.

Scroll older with `Prev`, newer with `Next`, and poll with `Follow`. A `Follow` page answers like
any other: with `Follow` again when there is still nothing newer, or `Next` once there is.

A page cursor is opaque. Do not build or parse one. A cursor the service did not write is refused
as `cursor-invalid`.

## Guarantees

**The window**

1. `Since` and `Until` include the lines stamped at them. `Since` after `Until` is `invalid-range`.
2. A line's place in the window is its timestamp against the bounds the response echoes. With
   an open `Until` and `After` set, the range ends the nanosecond before `ResolvedAt`: a line at
   or past it is padding.
3. Padding by span widens a bound by that much time. Padding by count is a number of lines past
   the bound, which the pages share: as pages move away from the bound they deliver up to that
   many, and no more.
4. With an open `Since` there is no padding before it, and the first page says so
   (`empty-padding`). With an open `Until`, padding after it is measured from the first page's
   clock.
5. A window is live while its end is open, lies in the future, or its counted `After` has not yet
   been filled. A live window's newest page sets `Follow`.

**The lines**

6. Filters apply to every line. The grep applies to the range alone: padding lines arrive
   unfiltered, so a counted padding costs its count whatever the grep, and shows what was
   happening around the matches.
7. Lines are merged across every container by timestamp. Within one container they are in log
   order.
8. A message longer than `MaxMessageBytes` is cut at a character boundary, never inside a color
   sequence, and the record says how many bytes were cut. With `ForModel` it is redacted first.
9. A page keeps lines from its starting edge while their messages fit `MaxPageBytes`. The first
   line is always kept, with `page-byte-limit-exceeded` when it alone is over. A dropped line is
   where the next page starts.
10. Terminal control sequences are stripped from every message before it leaves the service.
    Color is kept.

**The pages**

11. Along one direction, no line is repeated and no line is skipped, whatever each page's caps.
    The one exception is a second too loud to read, named in a `stream-skipped` notice.
12. `Prev` is set only when a line is known to lie older inside the window, `Next` only when one
    is known to lie newer.
13. A late line, stamped among lines already delivered, arrives on the next `Next` or `Follow`
    page. Each container resumes from its own last line, so a container whose clock runs behind
    loses nothing.
14. A container that fails to read on one page is skipped and named in a notice. The page is
    still served. That container resumes where it stopped on the next `Next`.
15. The scope is resolved again on every page. A scope that is now refused keeps the refusal's
    code. A workload's pods the mirror no longer holds are read from the file, found by the
    controller chain filed with their streams, so a replaced pod's lines read as previous on any
    page, a landing's included. A workload the mirror no longer holds reads none.

**The refusals**

A request that cannot be served returns a refusal with a code, never a partial page. The ones a
caller can act on:

| Code | Why |
| --- | --- |
| `conflicting-parameters` | a first page and a cursor together, or neither; a cursor with a scope or window; `Seen` with padding, beside a `Prev`, or a cursor whose `After` is counted; padding with both `Lines` and `Span` |
| `invalid-range` | `Since` after `Until` |
| `bad-padding` | more than 500 lines, or a span over 7 days |
| `bad-page` | `PageSize` over 500, or a negative cap |
| `bad-grep` | a grep that does not compile, or over 1000 bytes |
| `cursor-invalid` | a cursor the service did not write |
| `too-many` | more than 100 containers in scope |
| `not-mirrored`, `no-cache`, `syncing` | the cluster cache cannot resolve the scope yet |
| `read-failed` | every container failed to read and the page has nothing |

## Tail

`Service.TailFromCursor` is the other verb: it streams lines as they are written. It takes the
same scope, or a page's `Next` or `Follow` cursor, which holds one. It starts from now, from `Since`, or from
that cursor, with `Seen` beside either. A tail from a cursor reads every line a poll of it would,
then goes on past the window's end. Each frame carries a record, a notice, or, last, the
refusal that ended it. `MaxMessageBytes` and `ForModel` cut each message as a page's do. The
response echoes the request as it ran, the cursor's scope on a cursor start, and `ResolvedAt`, the
cursor's first page's clock or now, beside the frames. A tail hands out no
cursor: to reopen one, send the same start and the last line you hold of each container as
`Seen`. Lines are ordered per container only. See `CLAUDE.md` for how it works.
