---
title: Write the read contracts once and each surface's rules beside them
date: 2026-11-26
scope: cross-cutting
status: Accepted
---

# Write the read contracts once and each surface's rules beside them

## Context

One document, `docs/data-access.md`, defined the contracts and also held the rules of every
place a contract is written: Go method names, GraphQL field names and descriptions, gRPC
methods, the host's commands, and the webview's reducers, frame detection and stream sharing.
The two were interleaved. Its *Naming* section listed a rule that holds everywhere beside
Go-only ones, and its tables paired each contract with a Go and a GraphQL name. A reader who
wanted what applies to a GraphQL field had to read all of it, and several guides restated the
parts they needed.

## Decision

`docs/read-contracts/` holds four files.

- [`README.md`](../read-contracts/README.md) defines the contracts and names no language or
  transport: the terms, the taxonomy, how to choose and pair, each contract's semantics, the
  policies, and the naming rules every surface shares.
- [`go.md`](../read-contracts/go.md), [`graphql.md`](../read-contracts/graphql.md) and
  [`webview.md`](../read-contracts/webview.md) each say how one surface writes a contract, and
  link to its definition without restating it. gRPC is in `go.md`, since its methods are named
  as Go's are. The host's commands are in `graphql.md`, since they carry GraphQL.

Each rule is in one file, and a guide links the narrowest file that covers it instead of
restating a rule.

## Alternatives considered

- **Keep one document and add a section per surface.** The rules would be apart, but the file
  would still be 25 KB that every reader opens whole, and a guide could not link a surface.
- **Put each surface's rules in its `CLAUDE.md`.** The guides are read on every task and have
  byte budgets. The naming tables are needed only when a read is added or renamed.
- **A file for gRPC and one for the host.** Each would hold a single rule.

## Consequences

A contract's semantics change in `README.md` alone. A new surface gets a file of its own and a
link from `README.md`. A rule that seems to belong in two files belongs in `README.md` if it
names no surface, and otherwise in the surface that writes the name.
