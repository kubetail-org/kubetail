---
title: Authorization is binary, and a prompt is a denial the user may lift
date: 2026-10-03
scope: sidecar
status: Accepted
---

# Authorization is binary, and a prompt is a denial the user may lift

## Context

`permissions.Decide` answered `Allowed`, `Prompted` or `Denied` by walking eight branches in a
fixed order (permissions are classes, modes and rules decided at the
proxy, its *Decide's
order is fixed* bullet). The order was right, but the steps after it — the prompt's grants (4B),
the egress proxy (4C), Secret reads (5A) and the monitoring session (6B) — each explained a
behavior by where it fell in that order. A grant written from an answer, for one, had to be
argued safe by noting that `AskFor` comes before `Allow`. Step 3C of the agent-security
sequence restates the engine as a model those steps can name instead.

## Decision

**Authorization is binary and fails closed.** The policy permits an action or it does not; a
forbid wins over a permit, and nothing matching is a denial. **A prompt is a denial the user may
lift**, once with an answer or for good with a grant. A refusal is a denial no answer lifts.

`Decide` is two calls, composed, and keeps its signature:

- **`Policy.Authorize(act)`** answers a **`Verdict`**, the strongest of what matched, and the
  reason. The four verdicts are numbered in strength order, so the zero value is a denial:
  `Unmatched` (nothing matched; a grant lifts it), `Permit` (class 1 or 2, an `Allow` rule, or
  `Auto`), `Forbid` (an `AskFor` rule or class 5; an answer lifts it once, no grant does) and
  `Refuse` (a `Deny` rule, or `ReadOnly` on class 3, 4 or 5). The reason is the first source of
  that verdict in that order, a rule's line when a rule decided it.
- **`Verdict.Outcome()`** is the `Decision` the proxies act on: `Permit` runs, `Refuse` is
  denied, `Unmatched` and `Forbid` prompt.

**The verdict is order-independent**: the rules' order picks the reason alone. Adding a rule
never changes what another means.

This replaces the earlier ADR's *Decide's order is fixed* bullet. Every answer a rule Kubetail
reads can get is unchanged; the one answer that changes is a `Deny` or `AskFor` of class 1 or 2,
which now wins over that class's permit where it was never read. No such rule reaches the
engine: `securityconfig` accepts classes 4 and 5 alone.

## Alternatives considered

**A decision plus a *grantable* flag.** `Prompted` with a bit carries the same information, but
the bit's meaning is the verdict's: `Unmatched` is the default denial and `Forbid` a matched one.
Naming them keeps the proxies, the request and Settings agreeing on what a prompt is.

**A prompt as a third effect, beside permit and forbid.** It is what the branch list encoded,
and it is why every later step had to place itself in the order. As a kind of denial it composes:
a session that cannot ask turns every prompt into a refusal in `Outcome` alone (step 5A), and a
grant lifts only `Unmatched`, so no answer writes a rule that reaches past a forbid (step 4B).

**A policy library (cedar-go).** It would give a vetted evaluator for `Authorize`. The rules are
a flat struct the file holds and Settings draws, so a policy language on disk would accept rules
Settings cannot show, and the security model needs every rule on screen in the user's words.
Compiled in memory it would replace about sixty lines and leave the classifier, the reasons,
the mode resolution and the held-field flow untouched, while the match semantics (`*` crossing
`/`, a resource covering its `scale`, a set namespace skipping a cluster-scoped write) would
become generated conditions.

## Consequences

- Later steps name a verdict: 4B's `Grantable` is `Unmatched` with a context; 4C's unlisted host
  is `Unmatched` and an `AskFor` host `Forbid`; 5A's `NoSecretData` is a `Refuse` source and
  `NoPrompts` a parameter of `Outcome`; 6B's monitor is `Refuse` or a denied `Unmatched`.
- `TestTheVerdictMatchesTheOrderedTable` keeps the branch list as a second statement of the
  policy, exhaustive over every mode, class and rule that can exist. A step that changes the
  table changes that list in the same commit.
- The shape checks carry one more premise: a rule of class 1 or 2 is an `Allow` or it is
  refused, in `securityconfig` and in step 4D's read-back of a chat's grants.

## Revisit when

A rule needs set membership or hierarchy that the flat struct cannot say.
