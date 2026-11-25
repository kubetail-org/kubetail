---
title: Name workflow jobs verb first
date: 2026-11-19
scope: repo
status: Accepted
---

# Name workflow jobs verb first

## Context

The 9 workflows in `.github/workflows/` named their jobs three ways. `ci.yml`, `audit.yml` and `stress.yml` used `<Subject> · <Verb>` (`Go · Lint`, `Rust · Audit`). The release workflows used sentences (`Configure workflow`, `Build sidecar`, `Create GitHub release`). `auto-assign-issue.yml`'s one job had no `name:`, so GitHub showed its id. Sorted or scanned, the checks list grouped jobs by language, so a reader looking for every lint job hunted across groups.

kubetail adopted a verb-first scheme for its workflows the same day ([kubetail's ADR](https://github.com/kubetail-org/kubetail/blob/main/docs/adr/2026-11-19-name-workflow-jobs-verb-first.md)). The two projects share their CI conventions, so Kubetail follows it.

## Decision

Every job is named `<Verb> · <Qualifier> (<matrix values>)`:

- The verb comes first, from a small set: `Lint`, `Vet`, `Staticcheck`, `Test`, `Build`, `Build and Test`, `Check`, `Sign`, `Release`, `Review dependencies`, `Submit dependencies`, `Prune`, `Report`, `Reuse`, `Add`, `Assign`.
- The qualifier is the language, artifact or channel that tells two jobs with the same verb apart (`Lint · Rust`, `Build · Sidecar`, `Release · GitHub`). `ci.yml` covers every language in one workflow, so its jobs keep the qualifier. It is left out only where a verb has one subject (`Staticcheck`).
- Matrix values go in parentheses, values only, comma-separated, from OS to arch to variant: `Vet · Rust (linux, arm64)`, `Test (linux, amd64, race, cpu 4)`.
- A single-purpose workflow (`audit`, `codeql`, `coverage`) drops the verb and keeps the qualifier (`codeql / Go`, `audit / Rust`).
- `Plan`, `Gate`, `Fork gate`, `Prune · Caches` and `Report · Failures` are fixed.

Only `name:` changed. Job ids stay, since cache keys start with them and `needs:` refers to them. No ruleset requires a job by name.

## Alternatives considered

- **Keep `<Subject> · <Verb>` and name the rest to match.** The smallest change, but the subject leads, so same-verb jobs scatter in the checks list.
- **Drop the language where the workflow says it, as kubetail does.** kubetail runs one CI workflow per area, most of them single-language; Kubetail's `ci.yml` covers Go, Rust and TypeScript at once, so without the qualifier its jobs could not be told apart.
- **Free-form sentences, as the release workflows had.** Readable one at a time, but nothing lines up, and a name says nothing about what kind of job it is until it is read in full.

## Consequences

- Jobs with the same verb line up in the checks list, and a name grows at its end, with the qualifier and then the matrix values.
- Kubetail and kubetail name their jobs alike, so a reader of one reads the other's checks list.
- A name with no `${{ }}` expression gets its matrix values appended by GitHub. A name that mixes fixed values with matrix ones (`Build · Bundle (linux, …)`) spells them out with an expression, which shows unexpanded while the job is skipped.
- `stress.yml`'s report job rebuilds each job's `STRESS_ID` from its name, so that name's format is a contract inside the file.

## Revisit when

A ruleset requires a job by name. A rename would then break a required check, and stable names would matter more than the verb order.
