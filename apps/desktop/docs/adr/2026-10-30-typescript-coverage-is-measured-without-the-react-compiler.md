---
title: TypeScript coverage is measured without the React compiler
date: 2026-10-30
scope: frontend
status: Accepted
---

# TypeScript coverage is measured without the React compiler

## Context

`vite.config.ts` runs the React compiler (`reactCompilerPreset` through `@rolldown/plugin-babel`)
in every build, vitest included. The coverage sweep (#551) found two ways it bends what v8
measures (#554):

- **It strips comments inside a component or hook body**, JSX comments included. A
  `/* v8 ignore next -- <reason> */` there has no effect, and `make lint-markers` cannot tell it
  from one that works.
- **It adds a "reuse the cached result" branch for every memoized value**, which v8 reports on a
  closing token (`};`, `)}`, `</div>`). Only a re-render with unchanged inputs reaches it. About 30
  of the sweep's 82 gaps were these, and `codecov/patch` would flag them on every new component.

## Decision

Coverage is its own vitest mode: `vitest run --mode coverage` turns coverage on and loads no
compiler (`vite.config.ts`). `TypeScript · Coverage` and `make cover-js` run it and measure the
source as written; `TypeScript · Test`, `pnpm test` and the build keep the compiler. One switch
sets both, so coverage is never measured on compiled code, and no other flag can turn the
compiler off.

The whole suite passed without the compiler but for one test, which pinned the log viewer row's
re-render behaviour and passed only through the compiler's memoization. That behaviour is a
promise the row makes: a log view moves every row on a prepend, and redrawing their cells is the
cost the row exists to avoid. So the row memoizes its cells with `useMemo`, as `src/CLAUDE.md`'s
*write as though the compiler weren't there* asks for code whose speed is part of its contract.
Memoizing by hand is not the general answer to a coverage run: a component with no such promise
keeps leaning on the compiler, and no test pins its re-renders.

Measured on the suite at the time: statements went from 95.44% to 98.84% and branches from 89.32%
to 94.35%. Lines went from 99.91% to 99.81%: eight inline callbacks (a dialog's dismissal, an
input's `onChange`) that no test called had read as covered because the compiler moved them into
code that ran. They are tested now.

## Alternatives considered

- **A marker form the compiler keeps.** It keeps no comment inside a function it compiles, and v8
  reads markers from the transformed source.
- **Exclude the cached branches by setting.** Neither vitest nor v8 can tell a compiler-made
  branch from a written one.
- **Test the re-render path.** It is what the sweep did, and it puts a test on every component
  for code nobody wrote.

## Consequences

- A marker in a component works, and a component's coverage is its own code's.
- Coverage runs code the shipped app does not run in that shape. A bug only the compiler's output
  has is caught by `TypeScript · Test`, never by a coverage gap.
- A test that depends on the compiler's memoization fails in the coverage run. Where the
  re-render is a promise the code makes, memoize by hand; elsewhere the test is testing the
  compiler, and goes.
- A bare `vitest run --coverage` measures the compiled code. Coverage is read from
  `--mode coverage`.
