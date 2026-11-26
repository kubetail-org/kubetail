---
title: Mark the prompt cache for an hour, and only where a provider needs marks
date: 2026-10-17
scope: sidecar
status: Accepted
---

# Mark the prompt cache for an hour, and only where a provider needs marks

## Context

This decision predates the file; it was recorded in `sidecar/CLAUDE.md` (*Models*). The vendor
facts behind it are in [`docs/notes/prompt-caching.md`](../notes/prompt-caching.md).

The model APIs are stateless: every turn resends the system prompt and the transcript, and a
provider bills a prefix it has seen at a fraction. Providers cache in three ways: on their own,
only what a request marks (Anthropic, and Claude through OpenRouter), or per machine, found by a
key the request carries.

## Decision

**The Messages wire marks two places with `ttl: 1h`** (`cacheHour`): the system block, a fixed read
point every chat shares, and the request-level `cache_control`, which the API places after the last
block and moves forward each turn. A turn then reads the prior transcript and writes only what the
last one added.

**Every catalog entry states how it caches** (`Model.Cache`): `CacheAuto`, `CacheMarks` or
`CacheNone`. The Chat Completions wire sends the request-level mark only for a `CacheMarks` model.

**A request carries an affinity key** (`Request.AffinityKey`): a chat's id, or a subagent's run id.
`Provider.AffinityRoute` names where it goes, header or body field, for the vendors that route a
cache by it. The Responses wire sends no `prompt_cache_key`: from GPT-5.6 on OpenAI routes the cache
itself, and the key only separates cache accounting.

**The prefix stays stable**: the system prompt has nothing per-turn in it, history is append-only,
and anything per-turn rides in the messages at the end.

## Alternatives considered

- **The 5-minute default TTL.** A chat is often picked up again within the hour, and one long think
  would outlast five minutes. A one-hour write costs more; a read costs the same.
- **Marking on every wire.** An endpoint that does not know `cache_control` may refuse the request,
  so the mark goes only where the catalog says it is needed.
- **A subagent under its parent's key.** A subagent's prefix is its brief, not the parent's
  transcript, so it gets its own key.

## Consequences

A cache regression is no error, only a higher bill. `CacheReadTokens` and `CacheWriteTokens` on the
model call's row are the only evidence; `make check-cache` confirms a row against real keys. A model
or effort switch starts a new cache, at the cost of one full-price turn.
