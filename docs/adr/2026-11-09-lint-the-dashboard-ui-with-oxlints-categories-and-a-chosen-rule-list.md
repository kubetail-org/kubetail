---
title: Lint the dashboard UI with oxlint's categories and a chosen rule list
date: 2026-11-09
scope: dashboard-ui
status: Accepted
amended_by:
  - 2026-11-09-require-switches-over-a-union-to-list-every-member.md
  - 2026-11-09-check-hook-dependencies-in-the-dashboard-ui.md
---

# Lint the dashboard UI with oxlint's categories and a chosen rule list

## Context

`dashboard-ui` linted with ESLint 9 and `eslint-config-airbnb-extended`, with the typescript-eslint, react, react-hooks, react-refresh, jsx-a11y and import-x plugins. Prettier ran inside ESLint as a rule. That was about fifteen devDependencies, and every `pnpm lint` ran the formatter through the linter.

oxlint and oxfmt are the linter and formatter of the Oxc project. oxlint sorts its rules into categories: `correctness` is code that is wrong, `suspicious` code that is probably wrong, `perf` code with a faster spelling, and `pedantic`, `style` and `restriction` hold stricter and more opinionated rules. Its type-aware rules run through `oxlint-tsgolint`. It cannot load an ESLint shareable config such as airbnb's.

[kstackhq/kstack](https://github.com/kstackhq/kstack) made the same move and settled on categories plus a chosen rule list. Its `.oxlintrc.json` is the model here.

## Decision

**`dashboard-ui/.oxlintrc.json` is the lint, and every rule is an error.** `denyWarnings` fails the run on a warning. An unused disable comment fails, and only `oxlint-disable` comments are read.

**`correctness`, `suspicious` and `perf` are on.** Rules from the other categories are named one by one, as kstack names them: bugs (`eqeqeq`, `array-callback-return`, `import/no-cycle`), control flow (`no-param-reassign`, `no-nested-ternary`, `no-else-return`), modern syntax (`prefer-const`, `prefer-template`, `typescript/prefer-nullish-coalescing`) and JSX (`react/self-closing-comp`, `react/button-has-type`). Type-aware rules run on every lint, so a promise nobody awaits is marked `void`.

**A rule the categories turn on is turned off or given options only with a comment saying why.** Among them:

- `react/exhaustive-deps` is off. About 30 effects and callbacks leave a dependency out, and adding one changes when the hook runs. Code is written as though the rule were on.
- `react/immutability`, `react/incompatible-library`, `react/memo-dependencies` and `react/purity` are off. They check code for the React compiler, which is off here.
- `react/only-export-components` is not named. It is in no enabled category, and what it guards against costs a full page reload in development, not a bug.
- `typescript/switch-exhaustiveness-check` counts a `default` as handling the rest of a union.

**oxfmt formats and sorts imports** (`dashboard-ui/.oxfmtrc.json`): single quotes, 120 columns, and the groups third-party, `@kubetail/*`, `@/`, relative, stylesheets. `pnpm format` writes; `pnpm lint` runs oxlint, then `oxfmt --check`.

## Alternatives considered

- **Keep ESLint and airbnb.** It works. It is about fifteen packages, and airbnb's set was chosen for code we do not write: class components, prop-types, sloppy mode.
- **Port airbnb rule by rule into `.oxlintrc.json`.** kstack tried this first. It came to about 160 entries that nobody chose and none of which said why it was there.
- **Turn `react/exhaustive-deps` on now.** Each of the 30 findings needs a judgement on whether the effect should rerun. That is a behaviour change to review on its own, not part of a tool switch.
- **Take all of kstack's rules.** `typescript/no-explicit-any` and the five `typescript/no-unsafe-*` rules report about 140 lines here, most where Apollo's cache and the GraphQL custom scalars hand back `any`. Typing them is its own change.
- **kstack's stricter switch check, where a `default` does not count.** Every existing switch over a union has a `default`, and listing each member would grow them without catching a bug today.

## Consequences

Fourteen ESLint and Prettier packages are gone, and three Oxc packages replace them. Existing `eslint-disable` comments were converted or removed; a new one would be ignored.

The import order that `CLAUDE.md` described is now enforced by the formatter.

An oxlint upgrade can add a rule to an enabled category. The upgrade's change fixes what it reports or turns it off with a reason.

A missing hook dependency is still not caught.

## Revisit when

The `any`s from Apollo and the custom scalars are typed, so the `no-explicit-any` and `no-unsafe-*` rules can go on; or someone reviews the effects that leave out a dependency, so `react/exhaustive-deps` can.
