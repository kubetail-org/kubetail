---
title: Ollama is local, and OLLAMA_HOST says where
date: 2026-10-17
scope: sidecar
status: Accepted
---

# Ollama is local, and OLLAMA_HOST says where

## Context

This decision predates the file; it was recorded in `sidecar/CLAUDE.md` (*Models*) as the design
the `llm` rebuild has to reproduce. Ollama is the one provider with no key: a daemon on the machine,
dialed on Chat Completions. Ollama can also serve cloud-backed models, which run on Ollama's servers.

## Decision

**The Ollama entry is always present and last**, at `llm.OllamaBaseURL` (loopback, port 11434)
unless `OLLAMA_HOST` says otherwise. `llm.ParseOllamaHost` reads the variable the way Ollama's own
client does, so Kubetail dials the daemon `ollama run` would: a host or `host:port`, or an http(s)
URL with its path kept; no scheme means http on 11434. A wildcard host (`0.0.0.0`, `::`) dials
loopback. A value it cannot parse warns once, naming the variable and never the value, and keeps
the default.

**A cloud-backed model is never listed** (a model with a `remote_host`). A user who wants a cloud
model sets that vendor's key.

## Alternatives considered

- **A Kubetail variable for the daemon's address.** It could disagree with the one `ollama` reads.
- **Dialing a wildcard host as written.** The same variable sets the daemon's bind address, where a
  wildcard means every interface; dialed, it means nothing.
- **Listing cloud-backed models.** They leave the machine through a provider the user has not keyed.

## Consequences

Kubetail follows the user's Ollama setup with nothing to configure. A model pulled after launch needs
a restart, since discovery runs once.
