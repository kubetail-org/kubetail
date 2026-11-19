---
title: Name workflow jobs verb first
date: 2026-11-19
scope: repo
status: Accepted
---

# Name workflow jobs verb first

## Context

The 31 workflows in `.github/workflows/` named their jobs three ways. The CI, coverage and security workflows used `<Subject> · <Verb>` (`Go · Lint`, `Docker · Build`). The release and publish workflows used sentences (`Create GitHub release`, `Update AUR kubetail-bin repo`). 19 jobs had no `name:`, so GitHub showed their ids.

CI had just split into one workflow per area (→ [ADR](2026-11-19-run-ci-as-one-workflow-per-area.md)), and the checks list shows the caller and the area before the job: `ci / libs / Go · Lint`. Four of those areas cover a single language, so the `Go ·` or `TypeScript ·` in their job names repeated what the area already said. Sorted or scanned, the list grouped jobs by language, so a reader looking for every lint job hunted across groups.

## Decision

Every job is named `<Verb> · <Qualifier> (<matrix values>)`:

- The verb comes first, from a small set: `Lint`, `Vet`, `Staticcheck`, `Test`, `Build`, `Build and Test`, `Check`, `Measure`, `Sign`, `Verify`, `Package`, `Publish`, `Release`, `Review dependencies`, `Submit dependencies`, `Prune`, `Report`.
- The qualifier is the language, artifact or channel that tells two jobs with the same verb apart (`Lint · Rust`, `Build · Docker`, `Publish · Krew`). It is left out when the workflow has one subject: Go alone in `ci-cli.yml` and `ci-libs.yml`, TypeScript alone in `ci-web.yml`.
- Matrix values go in parentheses, values only, comma-separated, from OS to arch to variant: `Test (linux, 386)`, `Build (alpine, riscv64)`.
- A single-purpose workflow (`audit`, `codeql`, `coverage-*`, `ci-e2e`) drops the verb and keeps the qualifier (`codeql / Go`), unless nothing would be left; then the verb stays (`coverage-web / Measure`).
- `Plan`, `Gate`, `Fork gate`, `Prune · Caches`, `Report · Failures`, `CI · Passed` and `Coverage · Passed` are fixed.

Only `name:` changed. Job ids stay, since cache keys start with them and `needs:` refers to them. `CI · Passed` and `Coverage · Passed` stay, since the `main` ruleset requires them and `tools/cipassed` matches `<Name> · Passed`.

## Alternatives considered

- **Keep `<Subject> · <Verb>` and name the rest to match.** The smallest change, but the subject leads, so same-verb jobs scatter, and every job in a single-language area carries a subject the checks list already shows.
- **Keep the language everywhere, verb first** (`Lint · Go` in `ci-libs.yml`). Stable when an area gains a language, but it repeats the area in four of the six CI areas, and names grow at the front.
- **Free-form sentences, as the release workflows had.** Readable one at a time, but nothing lines up, and a name says nothing about what kind of job it is until it is read in full.

## Consequences

- Jobs with the same verb line up in the checks list, and a name grows at its end, with the qualifier and then the matrix values.
- A failing check reads `ci / libs / Lint`: the area says the language.
- When an area gains a second language, its jobs gain a qualifier and are renamed. A ruleset that requires one of them must change with it.
- A name with no `${{ }}` expression gets its matrix values appended by GitHub. A name that mixes fixed values with matrix ones (`Build (alpine, …)`) spells them out with an expression, which shows unexpanded while the job is skipped.
- `stress.yml`'s report job rebuilds each job's id from its name, so that name's format is a contract inside the file.

## Revisit when

The `main` ruleset requires a job inside an area, rather than `CI · Passed` and `Coverage · Passed`. A rename would then break a required check, and stable names would matter more than the verb order.
