---
title: Name every data access for its family, kind and operation
date: 2026-11-25
scope: cross-cutting
status: Accepted
---

# Name every data access for its family, kind and operation

## Context

Name every subscription for its family and kind
gave each subscription a contract and a name that states it. Queries had no such rule, and the
two halves were not described as one system:

- `models` and `clusters` both returned a list, but `models` was one value replaced whole and
  `clusters` a set of items. The bare nouns hid the difference.
- `logsFetch` was the only query field with a verb, and nothing said what `Fetch` meant.
- `sandbox` returned `SandboxStatus`, though the subscription rules already renamed `Status` to
  `State`.
- 17 of the 25 `Query` fields had no caller in the webview. Most duplicated a watch whose first
  delivery is the whole state.
- "Family" named a verb (`Watch`, `Subscribe`), so the same word could not also name what the
  consumer holds. The time series feed had no parallel with the event feed, though both are
  retained histories.

## Decision

[`docs/data-access.md`](../data-access.md) replaces `docs/subscriptions.md` and covers queries and
subscriptions in one document. The pairing rules (a Get returns what its state watch delivers, a
List what its collection watch or feed delivers, a Fetch what its tail delivers) belong to
neither half alone.

- **A family is a data model**: State, Collection, History, Sequence and Notification. `Watch`,
  `Subscribe`, `Tail` and `Listen` are verbs. **A kind is the data's element** (a value, an item,
  an event, a sample, an entry, a notice). **A contract is one operation over a kind**: Get, List
  and Fetch for queries; the eight subscription contracts as before.
- **Queries have three verbs: Get, List and Fetch.** A GraphQL query field ends in its verb, as
  mutations and subscriptions already end in theirs (`envVarsGet`, `logsFetch`), and its
  description opens with its contract. A Fetch's result type is `<Noun>FetchResult`.
- **A List covers collections and retained histories alike.** Identity and order come from the
  kind: an item's id, an event's id newest first, a sample's position oldest first.
- **The time series feed is the sample feed**, so History reads `ListEvents` /
  `SubscribeEventFeed` beside `ListSamples` / `SubscribeSampleFeed`.
- **A query with no caller is removed**, and nothing adds one only to initialize a subscription:
  a watch or a feed delivers its whole state first.

## Alternatives considered

- **Define a `Query` or `Search` contract now.** It would have one Go-only member,
  `CachedData().Query`, which evaluates a caller's SQL. The verbs stay free for a contract that
  needs them.
- **Reserve `Subscribe` for feeds across the codebase.** Generic pub/sub code and libraries use
  `Subscribe` for every kind. The rule binds a service method that opens with it, not the word.
- **`Read` for sequences.** It is taken by `ReadActive`, `ReadOwn`, `io.Reader` and ordinary prose.
- **Bare GraphQL nouns for queries.** A bare noun cannot tell a Get of a list from a List.
- **A Get for a sample window.** It would break the parallel with events, whose retained history
  is a List.
- **Keep the query fields that duplicate a watch.** A caller that reads once can open the watch
  and take its first delivery. A duplicate is a second contract to keep in step, and none had a
  caller.

## Consequences

Seven query fields were renamed and 17 deleted in one release. The webview and the sidecar ship
together, so no deployed client reads the old names. The tests that read state through a
deleted query now take the paired watch's first frame. Mutation names still drift (`Save` and
`Set`, `Delete` and `Remove`); they belong to no data family and need their own rule. This
supersedes the 2026-11-25 subscription ADR, whose decisions stand under the new terms.
