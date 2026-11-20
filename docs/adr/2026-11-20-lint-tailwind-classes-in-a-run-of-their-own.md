---
title: Lint Tailwind classes in a run of their own
date: 2026-11-20
scope: web
status: Accepted
---

# Lint Tailwind classes in a run of their own

## Context

Tailwind v4 has a canonical spelling for many classes (`wrap-break-word` for `break-words`,
`size-4` for `h-4 w-4`). Nothing in `apps/web` checked the UI's classes: a non-canonical spelling,
a class Tailwind does not know, or two classes setting the same property reached `main` unless an
editor's Tailwind IntelliSense caught it. The first run over `src/` found 64 non-canonical
spellings and 4 unknown classes, each a silent bug: `item-center`, `text-danger`, `border-divider`
and Tailwind v3's `bg-opacity-80`.

`eslint-plugin-better-tailwindcss` runs these checks under oxlint, which `apps/web` already lints
with, and autofixes the spellings. It loads Tailwind's design system from `src/index.css` in a
worker and asks Tailwind to canonicalize each class string. kstackhq/kstack made the same choice
and measured the same cost.

Measured on this code, in the Linux sandbox (8 cores), median of five warm runs:

| Run | Time |
|---|---|
| `oxlint` with `.oxlintrc.json` | 2.6s |
| `oxlint` with the three Tailwind rules merged into `.oxlintrc.json` | 15.6s |
| `pnpm lint:tailwind` over `src/` | 14.5s |
| `pnpm lint:tailwind` over one file | 7.3s |
| `pnpm lint` (oxlint, then `oxfmt --check`) | 1.9s |

The runs varied by up to 40% with the machine's load; an unloaded run of `pnpm lint:tailwind` took
6s. A run over one file is about half a run over `src/`: loading Tailwind is fixed cost.

## Decision

**The Tailwind rules live in `apps/web/.oxlintrc.tailwind.json`, run by `pnpm lint:tailwind`.**
`enforce-canonical-classes`, `no-unknown-classes` and `no-conflicting-classes` are errors. The
plugin's stylistic rules stay off; `oxfmt` owns formatting. `pnpm lint` stays at its own speed.

It runs in two places: `make lint-web`, after `pnpm lint`, and a `Lint Tailwind classes` step of
`ci-web.yml`'s `Lint` job, after `pnpm lint`. `apps/web/CLAUDE.md` tells an agent to run it after
changing classes, as it does for `pnpm lint`.

A test's marker class is allowed by the rule's `ignore` option in a file-scoped override, not a
disable comment: `.oxlintrc.json` does not know the rule, so it would report the comment as unused.

## Alternatives considered

- **The rules in `.oxlintrc.json`.** One command, but `pnpm lint` goes from about 2s to about 15s
  on every run, for a check that matters only when classes change.
- **The Tailwind rules as a second oxlint process beside the first, inside `pnpm lint`.** Saves
  nothing: that process alone is the 14.5s run.
- **The plugin's `collapse: false`.** kstack measured it saving about 1s, and it loses the
  `h-4 w-4` → `size-4` merges.
- **A Claude Code hook running it at the end of each turn.** It would spend several seconds on
  every turn of a branch that changes `src/`, and no other lint is enforced that way.

## Consequences

- A person running `pnpm lint` before a commit does not see a non-canonical class. The editor's
  Tailwind IntelliSense does, `make lint-web` does, and CI fails on it.
- An agent that skips it learns of a finding from CI, after it pushes.
- Only `className` and `cn(...)` are read. A class string in a plain constant is not checked.
- The plugin reads `src/index.css` as the design system. A theme token or `@plugin` added
  elsewhere is unknown to it, and its classes fail `no-unknown-classes`.

## Revisit when

oxlint or the plugin loads Tailwind fast enough that the rules in `.oxlintrc.json` keep `pnpm lint`
near its own time.
