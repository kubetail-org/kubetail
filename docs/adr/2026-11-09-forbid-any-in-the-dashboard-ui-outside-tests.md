---
title: Forbid any in the dashboard UI outside tests
date: 2026-11-09
scope: dashboard-ui
status: Accepted
---

# Forbid any in the dashboard UI outside tests

## Context

[Lint the dashboard UI with oxlint's categories and a chosen rule list](2026-11-09-lint-the-dashboard-ui-with-oxlints-categories-and-a-chosen-rule-list.md) left out `typescript/no-explicit-any` and the five `typescript/no-unsafe-*` rules (`argument`, `assignment`, `call`, `member-access`, `return`). Turned on, they reported about 95 lines outside tests and 45 in them. [kstackhq/kstack](https://github.com/kstackhq/kstack) turns on all six.

An `any` switches off the type checks for every value that flows from it. Much of what the UI handles is log text and Kubernetes object fields, which are attacker-controlled, so a value that skips the checks is a security concern as well as a correctness one.

Most of the findings came from a few places. The shared hooks in `src/lib/hooks.ts` defaulted their data types to `any` and typed a watch event's object as `any`. `k8sPagination` in `src/apollo-client.ts` took `any` for its merge arguments. `src/lib/auth.tsx` trusted `resp.json()` and the `BroadcastChannel` message as a `Session`. Two dispatch tables, in `pages/home/workload-data-provider.tsx` and `components/widgets/SourcePickerModal.tsx`, typed their documents `TypedDocumentNode<any, any>`. The rest read GraphQL custom scalars, which codegen emits as `any` ([Override the codegen client preset's v6 defaults](2026-11-09-override-the-codegen-client-presets-v6-defaults.md)).

## Decision

**`dashboard-ui/.oxlintrc.json` turns on `typescript/no-explicit-any` and the five `typescript/no-unsafe-*` rules.** This replaces the earlier ADR's rejection of them. The rest of that ADR stands.

**Tests keep `no-explicit-any` but turn the `no-unsafe-*` rules off.** A `vi.fn()` mock's calls and results are `any`, so every assertion on one would be reported.

**Data from outside the type system is checked or narrowed where it enters:**

- JSON from the server is read as `unknown` and checked: `parseSession` in `lib/auth.tsx` for the session, which also parses each `BroadcastChannel` message, and a zod schema for the login form's errors.
- The shared hooks infer their data types from the documents they are given. A watch event's object has the minimal shape the hook reads.
- A dispatch table whose entries select different fields widens each document to `Record<string, unknown>`, and each entry's `getItems` narrows the result back to its own query type with `as`.
- A custom scalar is narrowed with `as` to the type it has at run time: `Date` or `bigint` where a type policy in `apollo-client.ts` converts it, a string where none does.

## Alternatives considered

- **Keep the rules off.** Values read from log text and Kubernetes objects would still skip the type checks unseen, and kstack shows the rules can be met.
- **Give custom scalars concrete types through codegen's `scalars` option.** A scalar has one generated type, but at run time the same `Time` field is a `Date` when a type policy converts it and a string when none does, or when the result skips the cache. Any single type would be wrong for some fields. `unknown` would be honest but changes every generated file and every reader of a scalar. That is the codegen ADR's to revisit.
- **Type each dispatch table entry exactly and index a union of entries.** TypeScript cannot correlate the entry's document with its `getItems` through a union, so the call site still needs an assertion. Widening the entry is simpler and puts the narrowing in one visible place.
- **Keep the `no-unsafe-*` rules on in tests and cast each mock.** It is about 45 casts that check nothing.

## Consequences

An `any` in a non-test file fails the lint. Reading a value of unknown shape needs a check or an explicit `as`, and the `as` is where a reviewer looks.

Typing the tests' runtime fixtures found a misspelt config key (`ocerscan`) that an `as any` had hidden.

A custom scalar is still `any` in its generated type. The rules make every read of one an explicit `as`, but nothing checks that the asserted type matches what the cache delivers.

## Revisit when

Codegen gives custom scalars a type other than `any`. Then most of the `as` assertions on scalars can go.
