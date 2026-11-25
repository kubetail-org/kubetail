---
title: Decide every target's request by access level, a boundary, a mode and rules over one action
date: 2026-11-25
scope: cross-cutting
status: Accepted
---

# Decide every target's request by access level, a boundary, a mode and rules over one action

## Context

The permissions engine decided a sandboxed command's cluster write by its class, the cluster's
mode and the user's rules
(permissions are classes, modes and rules).
Four cloud platforms joined it, and each of their changes asked every time: a mode was the
user's word on a cluster, no rule could name a cloud account, and an answer could not outlast
the request. The rule's fields named a cluster's object, so a cloud action carried a class and a
summary alone. A change to who may do what sat in the destructive class with a scale to zero.
The monitor and the console were held back by two flags on the session, which every new reader
had to copy. Issue #1196 asked for one engine that never branches on the target.

## Decision

**A classifier per target reduces a request to one `permissions.Action`; everything after it is
target-neutral.** The action carries a principal, a target, a scope of `/`-separated segments
whose first is the account (a cluster's record id, a cloud account the credentials vouch for),
a service, an operation, a resource and one access level: `read`, `data-read`, `write`,
`access-control`, `destructive` or `reach`. A classifier that cannot trust the account leaves the
scope empty, and such an action asks each time. One that reaches past a known account keeps that
account's scope, so its mode and rules still refuse it, and asks each time. An action that
reaches every scope below its own (`Spans`, a list across namespaces) is decided by a deny or ask
for any of them, and allowed only by an allow reaching them all.

**The verdict has three layers.** The boundary (`boundary`, `asks`) is Kubetail's table of what
each principal may ever do: a chat and its subagents may be allowed reads, data reads and
writes, an access control change only inside a scope that bounds it (`Confined`), a destructive
change never unasked, and reach never; the monitor and the console read and nothing else, and
never ask. The account's mode (`read-only`, `ask`, `auto`) is the default per level. The rules
narrow or widen within the boundary, matched by level, principal, target and path patterns
whose `*` never crosses a segment. A deny wins, then the mode's deny, then an ask, then the
boundary's `Once`, then an allow, then the mode. A subagent is never wider than its chat. An
answer writes an allow of the action's level and scope for every principal.

**An operation no table names asks each time.** Kubernetes names the built-in resources the API
server serves itself (`builtin`, pinned against client-go's scheme); each cloud platform names
the changes a mode or a rule may allow (`ordinary`), starting short. Everything else is a write
marked `Once`, which a deny and read-only still stop.

Modes and rules are rows of `permission_modes` and `permission_rules`, keyed by target and
account; a cluster's go with its record. This supersedes the class engine, and replaces the rule
half of [a mode and a rule belong to a cluster](2026-11-24-a-folder-grant-is-its-own-row-and-a-mode-and-a-rule-belong-to-a-cluster.md):
a mode and a rule now belong to an account, of which a cluster is one kind.

## Alternatives considered

- **Keep the classes and add a cloud class per platform.** Every rule, mode and line would
  branch on the target again, and a new platform would touch the engine.
- **Let a mode reach a cloud platform's every change.** A cloud API has thousands of
  operations, and a word like `Delete` in one is no guide to its blast radius. Allowing by list
  keeps an unknown change asking until a review names it.
- **Keep the monitor's and console's flags on the session.** Each proxy copied them onto its
  policy, and a reader that forgot did not fail closed. A boundary row per principal is one
  table, which a test walks in full.
- **Let a rule name a principal finer than a session's kind.** No caller needs it yet, and the
  column stores a name, so it can come later with no table rebuild.
- **Grant a Namespace object with its namespace's rule.** A rule over every Namespace would be
  wider than a namespace's, and no rule names one object, so a Namespace write asks each time.

## Consequences

A cloud platform's change may run unasked under `auto` or a rule, and an answer may grant one,
within the `ordinary` table; what it leaves out asks as it did. A cloud data read runs unasked
under `auto`, as a Secret read does on a cluster. A namespace's `access-control` rule reaches that
namespace's Roles, `kube-system`'s included. A resource pattern no longer covers its scale. The
boundary, the mode table, `builtin` and each `ordinary` are security tables: a row added to any
is a security change, and each has a test that fails on a missing or extra row. A stored rule
`settings.CheckRule` refuses reads as a refusal of every write and data read. The vendor pin of
each `ordinary` is its own follow-up.

Opening a pull request keeps GitHub's default, which lets the base's maintainers push to its
branch, and runs unasked under `auto`: `gh pr create` sets it on every pull request, and it reaches
only a fork's branch the user opened the pull request from. Changing it afterward is an access
control change.

## Revisit when

A user or a team is wanted as a principal, or a cloud platform's vendor model can pin its
`ordinary` table.
