---
title: The answer is a record, not a stream the caller owns
date: 2026-09-10
scope: sidecar
status: Accepted
amended_by: [A turn is a run over its message, filed in one transaction](2026-09-17-a-turn-is-a-run-over-its-message.md)
---

# The answer is a record, not a stream the caller owns

## Context

An earlier `chatStream` subscription had the obvious shape: the webview asked for an answer and the
answer streamed back down the same pipe. It was removed (`478d2f3`) rather than finished, because
that shape cannot answer three ordinary questions.

Two windows on one chat: only the one that asked sees the answer. Close the window mid-answer: the
answer dies with it, and the sidecar cannot tell "the user left" from "the user cancelled". A
connection blip: the middle of the answer is gone for good, because a stream that has ended cannot
be replayed.

## Decision

**Sending a message is a save, not a phone call.** `chat.Send` writes three rows — the user's
message, an empty assistant message marked `streaming`, and the `chat_send` ledger entry — and
returns the assistant row without waiting. The turn runs on a goroutine the service owns and joins,
and the answer shows up in the database. Every window watching that chat sees it, because they are
all watching the same rows.

Three things follow, and each is load-bearing:

**Every `Modified` carries the whole answer so far, not just the new words.** A delta makes every
frame load-bearing — one dropped or applied twice scrambles the text, and it stays scrambled. The
whole text makes any one frame disposable: a watcher can skip ten and the eleventh is complete.
That is what lets the bus conflate. This is a local socket; a long answer is a few megabytes in
total.

**The signal is a payload-less coalesced ping and every watcher re-reads** — the house shape
(→ [store-change ping bus](2026-08-26-store-change-ping-bus.md)), over three key shapes: `chats`,
`messages/<chatID>` and `stream/<chatID>`. The split is what keeps a long chat cheap to stream:
with one key a pump could not tell "the answer grew" from "a row appeared", and would re-read and
re-diff the whole transcript per drained ping, per watching window, for the length of every answer.
Conflation bounds the rate, not the size.

**The in-memory copy is the truth while a turn runs.** The database gets a checkpoint every few
seconds and one final write, and every read overlays the in-flight turn's current message on the
stored row — so a crash costs a few seconds of an answer rather than all of it, and a watch opened
mid-answer sees the live text rather than the last checkpoint.

## Alternatives considered

**Keep `chatStream` and fix it.** Each of the three failures needs the answer to exist somewhere
the asking connection does not own. Once it does, the stream is redundant.

**Publish rows to subscribers at the transaction boundary**, instead of a ping. A subscriber
attaching between a commit and its publication folds a change twice or misses it, and any lock that
closes that gap is a lock a slow window then holds every writer under. The ping bus ADR rejected
this for the cluster store for the same reason.

**Send deltas rather than the whole answer.** Cheaper per frame, and it makes every frame
load-bearing — which is exactly what conflation is not allowed to be.

**One bus key per chat.** Simpler, and it costs a full transcript re-read per chunk per window.

**Run the turn on the supervisor.** It reconciles toward a desired state with a retry ladder, and a
chat turn must never be retried: it costs money and the user did not ask twice.

## Consequences

Closing a window costs nothing, and the frontend has nothing new to learn — a chat is a delta watch
over stored records like every other live surface (→ [delta-watch
protocol](2026-08-09-delta-watch-protocol.md)).

The obligations it creates are the ordering rules around the end of a turn. The final write, the
overlay's removal and the notification have to happen in that order, or a watcher re-reading on the
completion ping copies the stale `streaming` overlay over the settled row and no later ping
corrects it. A stream ping that finds no live message must send nothing, for the same reason. One
turn per chat is enforced in the service by a mutex-guarded reservation, never in a composer —
two windows both pressing send is an ordinary thing to do. And a startup reconcile fails every row
still marked `streaming`, because the process that was writing it is gone.

A send is idempotent on its `requestID` because the webview's GraphQL client retries mutations, and
the ledger that makes it so deliberately outlives the chat — so a retry after a delete answers
`ErrChatGone` rather than creating a second chat. The ledger is pruned at startup once a day old,
which is that guarantee's limit.

## Revisit when

Tools arrive. A turn that calls back into the cluster is still a record, but `Chunks` grows to
carry block boundaries and the ordering rules above get a second writer to account for.
