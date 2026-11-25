---
title: Accept picomatch's ReDoS advisory in dev tooling
date: 2026-11-18
scope: repo
status: Accepted
---

# Accept picomatch's ReDoS advisory in dev tooling

## Context

[GHSA-fc5q-39xc-vjpf](https://github.com/advisories/GHSA-fc5q-39xc-vjpf), high, covers picomatch
`<= 4.0.7`: a glob such as `+(*)` or `+(*a)` compiles to a regular expression that backtracks
exponentially on a short crafted string. No patched version exists; 4.0.7 is the latest release.
`TypeScript · Audit` fails at moderate on the whole tree, so `main` has failed since the advisory
reached the audit ([#969](https://github.com/kstackhq/kstack/issues/969)).

The lockfile holds picomatch 4.0.7 and 2.3.2. `pnpm why picomatch --prod` prints nothing: every path
runs through build and test tooling (vite, vitest, rollup's plugin utilities, tinyglobby, fdir,
micromatch under `@graphql-codegen/cli`), none of which ships in the app. The patterns those tools
compile come from our own config and source (`vite.config.ts`, `codegen.ts`, `import.meta.glob`),
and the attack needs the attacker to supply the pattern.

## Decision

Accept it. `pnpm-workspace.yaml`'s `auditConfig.ignoreGhsas` lists the advisory with its reason, so
`TypeScript · Audit` passes again and still fails on any other advisory. `TypeScript · Dependency
review` reads the same list.

## Alternatives considered

**Override picomatch to a fixed fork or a git revision.** There is no published fix to override to,
and `blockExoticSubdeps` refuses a git source deep in the tree, rightly.

**Raise the audit level to `critical`.** It would hide every high advisory, not this one.

## Consequences

The ignore is per advisory ID. A new advisory against picomatch fails the audit; a rescope of this
one to a runtime path would not, so the acceptance holds only while picomatch stays out of the
production tree.

## Revisit when

picomatch publishes a patched release: drop the ignore and let Dependabot's security update move the
lockfile. Or anything outside dev tooling starts depending on picomatch (`pnpm why picomatch
--prod` prints a path), or a dev tool starts compiling globs from input we do not write.
