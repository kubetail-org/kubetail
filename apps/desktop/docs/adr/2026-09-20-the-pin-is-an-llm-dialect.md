---
title: The wire a chat is pinned to is an LLM dialect
date: 2026-09-20
scope: cross-cutting
status: Accepted
amended_by:
  - [A chat can switch to any model, and each run records the dialect it ran on](2026-09-25-a-chat-can-switch-to-any-model.md)
---

# The wire a chat is pinned to is an LLM dialect

## Context

*A chat stays on the protocol it started on* pins a
chat to a value named `Protocol`: `llm.Protocol`, the `conversations.protocol` column, the
GraphQL `Protocol` enum on `Chat` and `Provider`, and `KUBETAIL_CHAT_PROTOCOL_MISMATCH`. The word
names the mechanism, a wire format an adapter speaks, and in networking it means more than
that. What the chat store depends on is a guarantee: the set of providers whose transcripts
replay on one another. Anthropic direct, Bedrock and Vertex are one such set; the Chat
Completions vendors and Ollama are another. The adapter is a consequence of the set, one per
set because the set shares a shape.

## Decision

The value is a **dialect**: the same language with a different accent, so a transcript
written under one provider speaks to the others. In Go it is `llm.Dialect`, the package
carrying the qualifier. Where there is no package the name carries it, since a bare "dialect"
on a chat reads as a human language's: the `conversations.llm_dialect` column, the GraphQL
`LLMDialect` enum and `llmDialect` fields on `Chat` and `Provider`, and
`KUBETAIL_CHAT_LLM_DIALECT_MISMATCH`; the generated TypeScript spells the type `LlmDialect`.
The members are unchanged, `messages`, `responses`, `chatcompletions`, `fake`, each named for
the API that defined the dialect's shape. The adapters live under `llm/dialect/`, one package
per dialect. The pin itself, and every rule in the earlier ADR, stand as written with the term
read as dialect.

## Alternatives considered

- **Keep `Protocol`.** Defensible and already spelled across four surfaces, but it names how
  the wire is spoken rather than what the pin promises.
- **`APIFamily`.** Says the members are related and no more; a phrase where one word does.
- **`Wire`, `Format`, `API`.** The mechanism again. `API` alone reads as one endpoint rather
  than the set of providers that share it, and `Format` collides with `fmt` habits as a Go type.
- **`LLMDialect` in Go too.** `llm.LLMDialect` stutters; a model API that is not an LLM's
  gets its own package, which is the room the prefix was meant to leave.

## Consequences

- One rename across Go, the schema, the column, the generated types and the webview, with a
  dev database reset under the pre-release policy. Nothing has shipped.
- A reader of the earlier ADR maps `Protocol` to dialect; the ADR is not edited, being the
  record of the decision as it was made.
