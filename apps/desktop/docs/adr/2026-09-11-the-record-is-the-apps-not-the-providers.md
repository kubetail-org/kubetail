---
title: The record is the app's blocks, not one provider's wire format
date: 2026-09-11
scope: sidecar
status: Accepted
amended_by:
  - [A chat can switch to any model, and each run records the dialect it ran on](2026-09-25-a-chat-can-switch-to-any-model.md)
---

# The record is the app's blocks, not one provider's wire format

## Context

`chat_message.content` held the Messages API's content blocks **verbatim**: the API is stateless,
every turn resends the whole conversation, and storing exactly what came back meant the next turn
could send it back without a translation step. With one provider that is the cheapest correct
thing.

More than one provider ends it. A chat can move from model to model between turns, across
providers, so what is stored cannot be one provider's wire format: the next turn may be going
somewhere that could not read it. Two further facts shape the answer. A thinking block is only
useful to the model that wrote it — Anthropic asks for its own back unchanged, OpenAI's reasoning
item carries encrypted content only it can decrypt — and neither provider's API accepts the
other's. And an assistant turn can be *entirely* such blocks: an answer cut off by its output cap
while still reasoning is complete, and has no text at all.

## Decision

**`internal/llm` defines the schema a message is stored in**, and each provider encodes a request
from it and decodes its reply into it:

```
{ "type": "text", "text": "…" }
{ "type": "native", "provider": "anthropic", "block": { …the provider's own block, verbatim… } }
```

- `text` is what every provider has and what the transcript draws.
- `native` is a block only its provider can use, kept **verbatim** so that provider gets it back
  unchanged, and **dropped by every other provider** when it encodes the request.
- **A message the drop leaves empty is left out of the request**, not sent as an empty turn — an
  empty assistant message is what an API would refuse.
- **The block says whose it is; nothing else does.** A message carries no provider field beside its
  blocks: the row would then hold the same fact twice, and only the block's copy is checked where
  it matters, in the encoder.
- **A `native` block must be replayable from the record alone**, with no state the provider kept
  server-side. That is what makes OpenAI's `store: false` and `reasoning.encrypted_content` a
  requirement rather than a preference: a provider that remembered the conversation for us would be
  a retention decision made by omission.

## Consequences

**Losing another provider's thinking is what a switch costs**, and what every harness that allows
one pays. A switch is within one protocol, from
*A chat stays on the protocol it started on*. A chat that changes model mid-way sends the new provider the text of what came before and
none of the reasoning behind it. An answer that was *only* reasoning disappears from the request
entirely — the transcript still shows it, drawn as the reason it stopped.

**A `native` block is resent, by its own provider, every turn.** Both APIs are stateless and both
expect it: the cost is request size, which the context-window pass is where to count.

**`textOf` in the webview reads `text` blocks and nothing else**, which is what it did before this
change — the schema was chosen so that stayed true.

**Tool blocks join this schema**, not a provider's, when tools land.
