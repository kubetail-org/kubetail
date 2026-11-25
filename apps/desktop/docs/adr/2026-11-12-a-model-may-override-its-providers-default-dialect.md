---
title: A model may override its provider's default dialect
date: 2026-11-12
scope: sidecar
status: Accepted
---

# A model may override its provider's default dialect

## Context

A provider had one dialect and one base URL (`llm.Provider.Dialect`, `Provider.BaseURL`), so every
model in its catalog went on the same wire.

OpenCode Go (#850) is a gateway that serves each model on its native API under one key: Chat
Completions at `https://opencode.ai/inference/go/openai/v1`, Responses at the same URL for
`grok-4.7`, `gpt-5.6-luna` and `gpt-6-luna`, and Messages at
`https://opencode.ai/inference/go/anthropic` for `claude-haiku-5-5`, `minimax-m3`, `qwen3.8-max`,
`qwen3.8-flash` and `qwen3.7-plus`. Its first row sent every model on Chat Completions, and the
gateway translated for the eight whose native API is another. Translation is where reasoning
effort, thinking and prompt caching are most likely lost: `claude-haiku-5-5` on Chat Completions
gets no cache marks.

Everything downstream of a send already reads the dialect off `Target.Provider.Dialect`: the
switch in `Target.Stream`, the dialect each run stores, and `Message.foreign`, which keys a
payload on provider and dialect both.

## Decision

A provider's `Dialect` and `BaseURL` are defaults for every model in its catalog. A model may
override either with its own `llm.Model.Dialect` or `Model.BaseURL`; empty keeps the default.

`Provider.ForModel(m)` is the provider with m's overrides applied, and every `Target` holds it.
`Service.Resolve` alone builds one, and checks the resulting dialect against what `Stream`
speaks. `agent/chat`'s `subagentTarget` resolves a subagent's model afresh rather than reuse the
parent target's provider, which already carries the parent model's overrides.

The OpenCode Go row keeps Chat Completions as its default. The eight models whose native API is
another override it, each with the output cap its wire requires and the cache kind of that wire
(`CacheMarks` on Messages). The Messages models also override the base URL. `gemini-4-argon`
keeps the default: Kubetail has no Gemini dialect.

A debug build's `KUBETAIL_<ID>_BASE_URL` replaces the row's default and clears each model's base URL
override (`catalog.New`), so one stub answers the whole row. The GraphQL
`Provider.llmDialect` is the provider's default; the webview does not read it.

## Alternatives considered

- **One provider per wire** (`opencode-go-messages`, `opencode-go-responses`). Three picker
  groups for one key and one vendor, three rows in `KeyVars` sharing a variable, and a chat that
  moves between OpenCode Go models would look like a vendor switch.
- **Let the gateway translate.** No code, but it loses the thinking and caching the native wires
  carry, which is why the gateway publishes the native endpoints.
- **A dialect on every model, with none on the provider.** Every row but one would repeat the
  same dialect on every entry.

## Consequences

- A chat that moves between two OpenCode Go models on different dialects drops the other wire's
  thinking payloads, as a vendor switch does: `foreign` already treats one provider's two
  dialects as two writers.
- The `KeyHeader`, `Extra`, `Query`, `Omit` and `AffinityRoute` lines stay per provider, with no
  override, and apply on every wire its models use.
- Code that reads a provider's dialect without a model (catalog tests, the GraphQL field) reads
  the default. Anything that decides by wire must go through `ForModel`.

## Revisit when

A vendor needs a different key, header or parameter set per wire.
