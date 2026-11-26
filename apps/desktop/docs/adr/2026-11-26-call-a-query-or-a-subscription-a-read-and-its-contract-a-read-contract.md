---
title: Call a query or a subscription a read, and its contract a read contract
date: 2026-11-26
scope: cross-cutting
status: Accepted
---

# Call a query or a subscription a read, and its contract a read contract

## Context

[Name every data access for its family, kind and operation](2026-11-25-name-every-data-access-for-its-family-kind-and-operation.md)
put queries and subscriptions under one set of rules and called an operation they cover a "data
access". The term says little. Every operation the rules cover reads, once or continuously, and
the rules leave out anything that starts something new: a probe, a mutation, a background task.
"Data access" does not say that, and a write is a data access too. It is also awkward as a
count noun: "a data access", "every data access".

The guides already said "contract" for what the rules define: an operation is named for its
contract.

## Decision

An operation through which a consumer observes data a source owns is a **read**: a query reads
once, and a subscription keeps reading. What a read returns or delivers, and when, is its
**read contract**. The rules are
[`docs/read-contracts/`](../read-contracts/README.md), and the guides say "a read is named for
its contract".

This replaces the term in the 2026-11-25 ADR and nothing else in it. Its families, kinds,
contracts and names stand.

## Alternatives considered

- **Keep "data access".** It is the name the document shipped under, but it covers writes and
  hides the one property every contract shares.
- **"Observation".** It is exact, and the document defines a read as observing. Nobody says
  "an observation of clusters"; they say they read the clusters.
- **"Query contract".** `Query` is the transport's noun for the half that returns once, so it
  cannot also name both halves.
- **"Data contract" or "API contract".** Either would take in mutations, which have no family
  and need their own rule.

## Consequences

"Read" is prose and a document name, never a contract or a verb a method takes. The 2026-11-25
ADR rejected `Read` as the verb for sequences because `ReadActive`, `ReadOwn` and `io.Reader`
hold it, and that stands: a sequence's query is still a Fetch.

The 2026-11-25 ADR keeps its title and its text, so "data access" stays in `docs/adr/` and
appears nowhere else.
