---
title: The cluster card rides the user message
date: 2026-09-13
scope: sidecar
status: Accepted
---

# The cluster card rides the user message

## Context

A chat is filed under a cluster and the sidecar mirrors that cluster into a cache, but none of it
reached the model: every turn went under one constant system prompt that said the model can see
nothing, and the conversation was the user's text alone. The model could not tell a question
about Istio from one about a cluster with no Istio.

The orientation it lacks is small and mostly static — the context's name, the server version, the
connection and cache verdicts, the namespaces, a node count, the API groups served. What
constrains where it goes is the provider's prompt cache: both cloud encoders cache the prompt
prefix, matched from the start of the prompt to the first token that differs, and `CacheKey`
already groups a chat's requests so that cache hits.

## Decision

The sidecar renders a **cluster card** (`internal/clustercard`) from what it already holds and
attaches it to the **user message** as a block of its own, `context` in `llm`'s schema, ahead of
the text. `chat.Send` renders it before its transaction and, inside the transaction, compares
it against the newest context block in the chat's record: an equal card is not sent again, a
changed one goes, and the first message of a chat always carries one. The record is the whole
truth — `buildRequest` resends the row as it is, and the compare reads the same rows.

The card is the `## Cluster` section of one `<context>` block, a fenced JSON object on one line.
The block is the whole context in one piece, so the rule for the model is "the newest block is
current, in full", and a later kind of context is another section under it. JSON is what keeps a
value from forging the shape: every string is escaped, so nothing a server spells can close the
block or end the fence, with no per-field guard to forget. It also gives each list a `status` the
prompt defines once. A card that cannot be rendered is `{"unavailable": true}`, deduplicated like
any other, which the system prompt reads as withdrawing every earlier card.

Every encoder sends the block as text ahead of the message's own. The transcript draws it as a
collapsed *Cluster context* disclosure on the question that carried it, so what the provider was
told is on screen at the message it rode.

## Alternatives considered

- **The system prompt.** The obvious place for orientation, and the wrong one under a prefix
  cache: a system prompt that changed between turns would evict the whole conversation's prefix
  on every send.
- **A `system`-role message in the conversation.** Cached like any message, but the Messages API
  has no such role, and the Responses API treats it as a second instruction block. A block on the
  user message is one shape on every wire.
- **A card on every turn.** Simplest to reason about, and up to 4 KiB per turn for a cluster
  that has not moved. Nothing in the card moves on every turn, so one card per change is cheap.
- **A `last_card` column on the chat.** A cursor for the compare, so the transaction need not
  scan the transcript. But the block is always a message's first, so one `json_extract` query
  finds the newest in the record itself, and a cursor beside the record is a second thing that
  can be wrong.
- **Markdown instead of JSON.** Fewer bytes, but every value then needed its own guard against
  forging the shape, and every verdict was prose the model had to parse.
- **A timestamp on the card.** It would defeat the dedupe on its own; the message's `createdAt`
  already says when the card was true.

## Consequences

The model knows where it is, and only that: a namespace listed exists, a kind listed is served,
and nothing more can be read off the card. Facts come from tools, later.

The record says what the model saw, and a chat's cache prefix survives a turn whose card did not
change. The cost is the card's bytes on the turn it changes — a hard 4 KiB ceiling, about a
thousand tokens, which a local model on a modest context window pays from its allowance.

Cluster data the user did not type — namespace names, group and kind names — leaves the machine
for the provider the user picked, under the user's own send. The endpoint and the principal are
left out, and the transcript's disclosure is how the user sees what went. The security record and
the model's *By decision* row carry that.

Three invariants someone could break without noticing: the compare must stay inside the send's
transaction, or two sends can both decide "changed"; a context block must stay the first block of
its message, which is where the compare's query reads it; and nothing may stamp the card with a
value that moves every turn, or every message carries one.

## Revisit when

A per-cluster or per-provider consent switch is wanted — namespace names are the one field that
can carry a tenant's name, and a switch is a Settings change with its own review. Or when tools
land and the card's job shrinks to naming the cluster the tools read.
