---
title: Lint the React compiler's rules with oxlint's native ports
date: 2026-11-05
scope: frontend
status: Accepted
---

# Lint the React compiler's rules with oxlint's native ports

Replaces the compiler-rules decision of *Lint the webview with oxlint's categories and a chosen
rule list*, which loaded them from `eslint-plugin-react-hooks`. The rest of that ADR stands.

## Context

oxlint loaded the compiler's rules from `eslint-plugin-react-hooks` as the JS plugin
`react-hooks-js`. That plugin runs the compiler itself, so the lint agreed with the Vite transform.
It cost about 5 of `pnpm lint`'s 7.6 seconds, and it kept `eslint` installed as its peer, which
nothing ran ([#732](https://github.com/kstackhq/kstack/issues/732)).

oxlint has native ports of the same rules. They were off because `react/immutability` reported
variables reassigned inside an effect's closure, which the compiler accepts.

We wrote one component per rule in `recommended-latest`, each breaking it on purpose, and ran it
through both. On oxlint 1.90:

| Rule | Probe | Plugin | Native |
| --- | --- | --- | --- |
| `rules-of-hooks` | a hook called under an `if` | yes | yes |
| `exhaustive-deps` | a missing dependency in `useEffect` and in `useCallback`; an array rebuilt each render as a dependency | yes | yes |
| `static-components` | a component defined in render | yes | yes |
| `use-memo` | a `useMemo` callback with a parameter | yes | yes |
| `void-use-memo` | a `useMemo` that returns nothing | yes | yes |
| `preserve-manual-memoization` | narrower deps, a memo's value mutated later, an optional-chain dep | no | no |
| `incompatible-library` | `useVirtualizer` | yes | yes |
| `immutability` | state mutated in a handler, a prop's field assigned, a prop mutated in a handler, a memo's value mutated | yes | yes |
| `immutability` | a prop array pushed to during render | no | no |
| `globals` | a module variable reassigned in render | yes | yes |
| `refs` | a ref read in render, a ref written in render | yes | yes |
| `set-state-in-effect` | `setState` called synchronously in an effect | yes | yes |
| `error-boundaries` | JSX built inside `try` | yes | yes |
| `purity` | `Date.now()` in render | yes | yes |
| `set-state-in-render` | `setState` called unconditionally in render | yes | yes |
| `unsupported-syntax` | `eval` in a component | yes | yes |
| `config`, `gating` | compiler options; no native port | — | — |

The two agree on every probe, misses included. On this code the native rules report nothing,
`react/immutability` included: the effect-closure reassignments in `markdown.tsx` and
`held-still.ts` pass.

## Decision

**The compiler's rules are oxlint's native `react/*` ports.** `correctness` turns on all of
`recommended-latest`'s set but two, `react/rules-of-hooks` and `react/unsupported-syntax`, which
the config names. `eslint-plugin-react-hooks` and the `react-hooks-js` plugin entry are gone.
Disable comments name the rules `react/…`.

**`react/exhaustive-effect-dependencies` is off.** It is beyond `recommended-latest`, and it reports
a dependency an effect lists in order to rerun on it, such as the location `left-sidebar.tsx`
closes its drawer on.

## Alternatives considered

- **Keep the plugin.** It is the compiler, so it cannot drift from the transform. The probes found
  no drift, and it costs two thirds of the lint's time and an ESLint install.

## Consequences

`pnpm lint` takes about 2.7 seconds. `eslint` and its dependencies leave `pnpm-lock.yaml`, and
with them `minimatch` 3.x; the minimatch advisory exception now names GraphQL codegen alone.

The lint and the Vite transform can now disagree: an oxlint port can lag the compiler, or report
code the compiler accepts. A disagreement shows as a lint error on code the compiler compiles, or
as a compiler bail-out the lint does not report.

## Revisit when

A native rule reports code the compiler accepts, or misses a violation the compiler reports.
