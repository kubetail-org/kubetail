---
title: Lint Tailwind classes in a run of their own
date: 2026-11-05
scope: frontend
status: Accepted
---

# Lint Tailwind classes in a run of their own

## Context

Tailwind v4 has a canonical spelling for many classes (`wrap-break-word` for `break-words`,
`size-4` for `h-4 w-4`). Only the editor's Tailwind IntelliSense reported them, so they reached
`main` unchecked ([#760](https://github.com/kstackhq/kstack/issues/760)).
`eslint-plugin-better-tailwindcss` runs the same check under oxlint and autofixes it.

The plugin loads Tailwind's design system in a worker and asks Tailwind to canonicalize each
class string. On this code, with the three rules below in `.oxlintrc.json`, `oxlint` went from
about 2.0s to 7.3s. A profile showed oxlint's JS thread blocked on that one worker for 4.7s,
and the worker busy in Tailwind's code. Tailwind already caches per class, so the time is its own
work. `collapse: false` saved about 1s and lost the `h-4 w-4` → `size-4` merges. Running the
Tailwind rules as a second oxlint process beside the first saved nothing: that process alone
takes about 6s. A run over one file takes about 3.6s, nearly all of it loading Tailwind.

## Decision

**The Tailwind rules live in `.oxlintrc.tailwind.json`, run by `pnpm lint:tailwind`.**
`enforce-canonical-classes`, `no-unknown-classes` and `no-conflicting-classes` are errors. The
plugin's stylistic rules stay off; `oxfmt` owns formatting. `pnpm lint` stays at its own speed.

It runs in two places: a step of CI's `TypeScript · Lint`, after `pnpm lint`, and `make lint`,
through `make lint-tailwind`. The guides tell an agent to run it after changing classes, as they
do for `pnpm lint`. A Claude Code hook running it at the end of each turn was weighed and left
out: it would spend ~4s on every turn of a branch that changes `src/`, and no other lint is
enforced that way.

A test's marker class is allowed by the rule's `ignore` option in a file-scoped override, not a
disable comment: `.oxlintrc.json` does not know the rule, so it reports the comment as unused.

## Consequences

- A person running `pnpm lint` before a commit does not see a non-canonical class. The editor's
  Tailwind IntelliSense does, and CI fails on it.
- An agent that skips it learns of a finding from CI, after it pushes.
- Only `className` and `cn(...)` are read. A class string in a plain constant is not checked.
