---
title: A signed-in provider's catalog follows its connection
date: 2026-11-18
scope: sidecar
status: Accepted
amended_by: [Every provider is one row, signed by one service](2026-11-20-every-provider-is-one-row-signed-by-one-service.md)
---

# A signed-in provider's catalog follows its connection

## Context

The ChatGPT plan's models are the account's: `GET /v1/models` lists `{slug, display_name,
visibility}` and nothing else. The list exists only while signed in, and changes with the
account. [A discovered model is listed on its catalog's
word](2026-10-17-a-discovered-model-is-listed-on-its-catalogs-word.md) says discovery runs once,
and that absent metadata admits no model.

## Decision

This replaces three parts of that ADR, for a signed-in provider:

- **The catalog is read when the connection changes**, not once: on each `Connected` session, at
  a launch that finds one, on a poke, and six hours after the last read, since a machine that
  never sleeps is never poked. Any other phase empties it. `services/connectors/internal/catalogsync` is
  its only writer, and a read's result is set only while its session stands.
- **`visibility == "list"` is the catalog's word that a model can chat.** The route serves chat
  models alone.
- **A listed model with no metadata of its own is admitted on the fallback**: the `openai` row's
  entry of the same id where it has one, else no efforts, `Tools: true`, the written cap and no
  context window. The route documents function tools for every model it lists.

`llm.Service` holds a fixed provider list in picker order; `SetCatalog` replaces one provider's
models, and `Subscribe` is a gauge of the list, served as `modelsWatch`.

## Alternatives considered

- **A mutable provider list.** The picker's order would move with every connection.
- **Listing nothing an `openai` entry does not describe.** OpenAI's own examples name ids that row
  lacks, so the picker would be empty for most accounts.

## Consequences

A model the fallback admits shows no thinking and is never refused ahead for its size; an
overflow arrives as the provider's own error.
