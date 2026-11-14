---
title: Require switches over a union to list every member
date: 2026-11-09
scope: dashboard-ui
status: Accepted
---

# Require switches over a union to list every member

## Context

[Lint the dashboard UI with oxlint's categories and a chosen rule list](2026-11-09-lint-the-dashboard-ui-with-oxlints-categories-and-a-chosen-rule-list.md) turned on `typescript/switch-exhaustiveness-check` with `considerDefaultExhaustiveForUnions`. A switch over a union passed if it had a `default`, so a member added to a union, such as a new `Status` or `Theme`, fell into the `default` without the lint saying so. `statusMessage` in `ServerStatus.tsx` already left out `Status.Degraded` and threw in its `default`.

[kstackhq/kstack](https://github.com/kstackhq/kstack/blob/main/.oxlintrc.json) uses the stricter setting. Turning it on here reported 9 switches. Seven more listed every member and still had a `default`, most of them `throw new Error('not implemented')` that could not run.

This ADR replaces the earlier ADR's bullet on `typescript/switch-exhaustiveness-check` and the alternative it rejected.

## Decision

**`typescript/switch-exhaustiveness-check` runs without `considerDefaultExhaustiveForUnions`, and with `allowDefaultCaseForExhaustiveSwitch: false` and `requireDefaultForNonUnion`** (`dashboard-ui/.oxlintrc.json`). A switch over a union lists every member, and a `default` does not stand in for them. A switch that lists every member has no `default`, so a new member fails the lint. Any other switch has a `default`.

A switch over a nullable string names `null` or `undefined` as a case and keeps its `default` for the other strings.

## Alternatives considered

- **Keep `considerDefaultExhaustiveForUnions`.** The earlier ADR chose it because listing each member grew the switches without catching a bug. The cost was 9 switches and a few lines each, and it is the check that would catch the next missed member.
- **Require every member but allow a `default` beside them.** The lint would still catch a new member. The `default` never runs, yet reads as a fallback someone relies on, and the seven already in the code show they accumulate.

## Consequences

Adding a member to a union fails the lint at every switch over it, and the change says what each one does with it.

`statusMessage` handles `Status.Degraded` instead of throwing. The other `throw`s went with their `default`s; the type checker guarantees the case.

## Revisit when

oxlint gains a setting that tells a closed union from an open string, so a nullable string no longer needs its `null` or `undefined` named.
