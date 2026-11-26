---
title: A discovered model is listed only on its catalog's word
date: 2026-10-17
scope: sidecar
status: Accepted
amended_by: [A signed-in provider's catalog follows its connection](2026-11-18-a-signed-in-providers-catalog-follows-its-connection.md)
---

# A discovered model is listed only on its catalog's word

## Context

This decision predates the file; it was recorded in `sidecar/CLAUDE.md` (*Models*) as the design
model discovery has to reproduce when it is rebuilt. Each vendor's `GET /models` lists more than chat
models (embeddings, image and audio models), in a shape of its own, and some say nothing about what
a model can do.

## Decision

**A model is listed only when its catalog says it can chat; absent metadata never admits one.**
Each vendor's decoder reads the vendor's own field (Mistral's `capabilities.completion_chat`,
Fireworks' `supports_chat`, Ollama's `completion` capability). A body of another shape lists
nothing.

**`intersect` is for a catalog that does not say** (Gemini, Groq, xAI): the written catalog is the
models, and discovery keeps the entries the vendor still serves. `everyID` admits every id, only for
a vendor that serves nothing but chat models (DeepSeek).

**Discovery runs once, off the path to READY**, each entry under its own deadline. A catalog that
cannot be read is empty, logged by provider and failure kind, never a URL, body or key.

## Alternatives considered

- **Admitting a model when the field is missing.** A shape change at the vendor would list every
  embedding model in the picker.
- **A naming rule** (guessing chat models from their ids). A vendor whose catalog stops showing
  the field its decoder reads moves to `intersect`, where the written catalog decides.

## Consequences

A vendor's catalog change can empty its picker; `make llm-catalogs` prints what each keyed entry
would list beside what the vendor serves, which is how a written catalog is checked.
