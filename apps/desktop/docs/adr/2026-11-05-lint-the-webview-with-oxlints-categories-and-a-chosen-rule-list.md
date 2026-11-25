---
title: Lint the webview with oxlint's categories and a chosen rule list
date: 2026-11-05
scope: frontend
status: Accepted
amended_by:
  - [Lint the React compiler's rules with oxlint's native ports](2026-11-05-lint-the-react-compilers-rules-with-oxlints-native-ports.md)
---

# Lint the webview with oxlint's categories and a chosen rule list

Supersedes *Lint the webview with oxlint, every rule an error*, which ported airbnb's style guide
into `.oxlintrc.json` rule by rule.

## Context

`.oxlintrc.json` was about 510 lines. oxlint cannot load an ESLint shareable config, so the move
from ESLint ([#18](https://github.com/kstackhq/kstack/issues/18)) named each airbnb rule beyond the
`correctness` category, with airbnb's options. About 160 of its 206 entries were that port
([#733](https://github.com/kstackhq/kstack/issues/733)).

airbnb's set was chosen for ESLint and for code we do not write. Many entries covered class
components, prop-types, sloppy mode or libraries this code does not use. Each was a line a reader
had to weigh, and none said why it was there.

Most of the code here is written by agents, which run the lint after each change. An agent treats
a warning as noise and a disable comment as a way out. What we want from the lint is modern
TypeScript that is fast, safe with attacker-controlled text, and easy for a person to read.

oxlint sorts its rules into categories. `correctness` is code that is wrong, `suspicious` code
that is probably wrong, `perf` code with a faster spelling. `pedantic`, `style` and `restriction`
hold stricter and more opinionated rules. It has no `no-restricted-syntax`; its JS plugins take
ESLint's plugin API, AST selectors included. Its type-aware rules run through `oxlint-tsgolint`.

## Decision

**`.oxlintrc.json` is the lint, and every rule is an error.** `denyWarnings` fails the run on a
warning. Unused disable comments fail, and only `oxlint-disable` comments are read.

**Three categories are on: `correctness`, `suspicious` and `perf`.** The config names no rule one
of them already enables, except to give it options or turn it off, and each of those says why:

- `react/react-in-jsx-scope`, `oxc/no-map-spread`, `vitest/no-commented-out-tests`,
  `typescript/no-unsafe-type-assertion` and `typescript/consistent-return` are off. The first
  predates the automatic JSX runtime. The second asks a `map` to mutate, where React state is
  copied. The third reads prose in a comment as a test. The fourth reports every narrowing `as`,
  227 of them, in code we consider correct. The fifth reports a switch that lists every member of
  a union and has no `default`.
- In test files `unicorn/consistent-function-scoping`, `react/no-object-type-as-default-prop` and
  `react/jsx-no-constructed-context-values` are off: a test's helper sits in its `describe`, and a
  test renders once.

**Rules from `pedantic`, `style` and `restriction` are named one by one**, in four groups:

- *Bugs.* `array-callback-return`, `eqeqeq`, `no-case-declarations`, `no-loop-func`,
  `import/no-cycle` and their like. `typescript/no-explicit-any` and the five
  `typescript/no-unsafe-*` rules stop an `any` from carrying cluster data past the types. The
  `no-unsafe-*` rules are off in tests, where a mock's calls are `any`.
- *Control flow and mutation.* `no-param-reassign`, `no-nested-ternary`, `no-else-return`,
  `no-multi-assign`, `no-sequences`, `one-var`, and `no-restricted-globals` for the DOM globals
  that read as a forgotten local (`name`, `event`, `status`, `length`).
- *Modern syntax.* `prefer-const`, `prefer-template`, `object-shorthand`, `prefer-object-spread`,
  `typescript/prefer-nullish-coalescing`, `unicorn/prefer-at`,
  `unicorn/prefer-string-replace-all`, `unicorn/prefer-number-properties` and their like.
  `suspicious` adds `unicorn/no-array-sort` and `no-array-reverse`, so a sort is `toSorted`;
  `tsconfig.app.json`'s `lib` is `ES2023` for it.
- *JSX.* `react/button-has-type`, `react/self-closing-comp`, `react/jsx-no-useless-fragment`,
  `react/jsx-boolean-value`, `react/jsx-curly-brace-presence`, `react/jsx-fragments`.

**Type-aware rules run on every lint** (`typeAware`): the categories' promise rules
(`no-floating-promises`, `await-thenable`), plus `no-misused-promises`,
`switch-exhaustiveness-check`, `no-deprecated` and `no-misused-spread` by name.

**A switch over a union lists every member**, so a member added later fails the lint where it is
not handled. A `default` does not stand in for one. Any other switch has a `default`
(`requireDefaultForNonUnion`).

**`tsc` checks what it can, and the lint does not repeat it.** `noFallthroughCasesInSwitch`,
`noImplicitReturns` and `verbatimModuleSyntax` are set in both tsconfigs, so `no-fallthrough`,
`typescript/consistent-return` and `typescript/consistent-type-imports` are not on.

**The compiler's rules come from `eslint-plugin-react-hooks`**, loaded as the JS plugin
`react-hooks-js`, with `recommended-latest`'s rules. The native `react/*` ports are off.

**Kubetail's rules live in `lint/oxlint-plugin.js`.** `kubetail/no-restricted-syntax`, ESLint's rule
by the same name, carries the HTML-sink and template-engine bans and the `for…in` ban.
`kubetail/disable-needs-reason` fails a disable comment without a `-- reason`. `react/no-danger` and
`no-restricted-imports` complete the security bans, which `docs/security-model.md` lists as
enforced.

**oxfmt sorts imports** (`sortImports` in `.oxfmtrc.json`): packages, then `@/`, then relative
paths, a blank line between groups.

Of airbnb's candidates in [#733](https://github.com/kstackhq/kstack/issues/733):

| Rule | Kept | Why |
| --- | --- | --- |
| `no-param-reassign` | yes | A caller's object is not the callee's to change. |
| `no-shadow` | yes, by `suspicious` | |
| `no-await-in-loop` | yes, by `perf` | |
| `react/no-array-index-key` | yes, by `perf` | |
| `for…in` ban | yes | It walks the prototype chain. |
| `for…of` ban | no | airbnb banned it for the regenerator runtime, which a webview never loads. It is the loop that can `break` and `await`. |
| `no-plusplus` | no | It guarded against semicolon insertion, which oxfmt owns. |
| `no-continue` | no | A `continue` is a loop's guard clause. |

## Alternatives considered

- **Keep the port.** It lints the same code. It costs 300 lines nobody chose, and it enforces no
  rule oxlint has added since airbnb was written.
- **`pedantic` as a category.** It reports about 4,000 lines here, most from
  `prefer-readonly-parameter-types`, `no-confusing-void-expression`, `require-unicode-regexp` and
  `max-lines-per-function`. Turning those off one by one leaves a list as long as the one naming
  what we want, and each oxlint upgrade adds to it.
- **`style` or `restriction` as a category.** They contradict themselves and the code:
  `no-ternary`, `no-magic-numbers`, `sort-keys`, `oxc/no-optional-chaining`,
  `oxc/no-async-await`. About 30,000 reports.
- **`default-case`, with a `default` counted as exhaustive.** Every switch then has a `default`,
  and a new union member falls into it unseen.
- **`typescript/no-unsafe-type-assertion` on.** It would make each `as` a disable comment or a
  runtime guard. The 41 outside tests narrow a DOM lookup, parsed JSON or a select's value, each
  after a check.
- **`.sort()` on a fresh copy, with `unicorn/no-array-sort` off.** It saves one copy. The rule
  cannot tell a fresh array from shared state, and sorting React state in place is the bug.
- **oxlint's native compiler rules.** `react/immutability` reports a variable reassigned inside an
  effect's closure, which the compiler accepts. The plugin is the compiler the Vite transform
  runs, so the two agree.
- **Every type-aware rule.** `strict-boolean-expressions`, `prefer-readonly-parameter-types` and
  `no-unnecessary-condition` report thousands of lines in code we consider correct.
- **The plugin in TypeScript.** oxlint loads a plugin with Node's own loader, which reads a `.ts`
  file only where type stripping is on. A JS file with `// @ts-check` loads under any Node and is
  still type-checked by `tsconfig.node.json`.

## Consequences

`.oxlintrc.json` is about 275 lines: 131 rule entries, 30 of them the compiler's rules and their
native ports. `pnpm lint` takes about 7 seconds, as before, most of it the compiler plugin.

An oxlint upgrade can add a rule to an enabled category. The upgrade's PR fixes what it reports
or turns it off with a reason.

`for…of`, `++` and `continue` are allowed. Existing code uses `forEach` and `+= 1`; both
spellings now pass.

These airbnb rules are no longer enforced:

- *Class components and prop-types:* `class-methods-use-this`, `react/prefer-es6-class`,
  `react/state-in-constructor`, `react/no-redundant-should-component-update`,
  `react/require-render-return`.
- *Sloppy mode and old module systems:* `vars-on-top`, `no-proto`, `no-multi-str`, `yoda`,
  `no-inner-declarations`, `no-lone-blocks`, `no-label-var`, `no-extra-label`, `unicode-bom`,
  `import/no-amd`, `import/no-dynamic-require`, `import/no-webpack-loader-syntax`.
- *What the compiler or formatter already checks:* `react/jsx-filename-extension`,
  `react/no-unknown-property`, `import/export`, `import/first`, `import/newline-after-import`,
  `no-regex-spaces`, `typescript/ban-tslint-comment`, `typescript/no-unnecessary-qualifier`,
  `typescript/no-mixed-enums`, `typescript/class-literal-property-style`.
- *Covered by a kept rule or by `tsc`:* `guard-for-in` (the `for…in` ban),
  `no-restricted-properties` (`unicorn/prefer-number-properties`,
  `prefer-exponentiation-operator`), `prefer-promise-reject-errors` (its `typescript/` twin),
  `default-case` (`switch-exhaustiveness-check`), `no-fallthrough`
  (`noFallthroughCasesInSwitch`), `typescript/no-require-imports` (`src/` has no `require` in its types).
- *Style we do not hold:* `no-plusplus`, `no-continue`, `no-bitwise`, `no-void`, `func-names`, `new-cap`,
  `grouped-accessor-pairs`, `no-array-constructor`, `no-empty-function`, `prefer-destructuring`,
  `prefer-numeric-literals`, `prefer-regex-literals`, `symbol-description`,
  `import/no-named-default`, `react/jsx-pascal-case`, `react/no-unescaped-entities`.
- *In oxlint's `nursery`:* `no-restricted-exports`, `no-unreachable-loop`.

airbnb's options are gone where oxlint's defaults pass: the `jsx-a11y` rules, `no-unused-vars`,
`react/jsx-no-script-url`, and all but 17 names of `no-restricted-globals`.

`eslint-plugin-react-hooks` keeps `eslint` installed as its peer, which nothing runs.

A rule added to the config either passes on the code or comes with its fixes. A disable comment is
a claim a reviewer reads.

## Revisit when

oxlint's native compiler rules agree with the compiler on this code, or oxlint gains
`no-restricted-syntax`.
