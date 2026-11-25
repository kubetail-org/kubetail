---
title: The security model is a file per subject, checked against the code
date: 2026-11-25
scope: repo
status: Accepted
---

# The security model is a file per subject, checked against the code

## Context

`docs/security-model.md` had grown to 208 KB in 388 lines. One table of 231 rows held almost all of it, with cells up to 6,079 characters. A cell mixed the property, the tests that pin it, its explanation and its residual risk, so the residual a reviewer needs most was the last sentence of the longest cells. The file named assets and boundaries but no attacker, and its boundary list had no entry for the sandbox, which about half the rows describe. One subject, such as how Kubetail's folders and links are fenced, was spread over ten rows in three places.

Nothing checked the file against the code. An audit on 25 November 2026 found five cited tests that had been renamed or removed, four cited files that had moved, seven records in `docs/security/` the model did not link, and two statuses the legend did not define ([#1211](https://github.com/kstackhq/kstack/issues/1211)).

## Decision

- **`docs/security-model.md` is the map.** It holds the threat model (who attacks, what each starts with, what Kubetail assumes, what it does not protect), the assets, the boundaries, the legend, an index and the terms.
- **The protections are a file per subject under `docs/security-model/`.** Each opens with its rule in a few sentences, then a table of rows, then an Evidence entry per row.
- **A row states one property.** It has an id (`FOLD-3`), a status and a residual, and neither its protection nor its residual is over 600 characters. Tests, paths, records and explanation go in the row's Evidence entry, where a test is cited as the first words of a bullet.
- **`Built` is a status.** Five rows used it for a mechanism whose tests are in and whose live check is still owed, so the legend defines it.
- **`tools/securitymodel` checks the model**, as `make lint-security-model`, in `make lint`, `make check-changed` and CI's `Lint · Security model` job, which runs on every change since a test renamed anywhere is a stale citation. It fails on a cited test or path that is not in the tree, a row without what its status needs, a status the legend does not define, a cell over the limit and a record nothing links.
- **The index and `not-protected.md` are written by the tool** (`go run ./securitymodel write`), and the check fails when either is out of date. The register lists every row that is not *Enforced*, and every residual, in one place.

The reshaping changed no claim and no status. Every backticked name and link of the old table is in the new files.

## Alternatives considered

- **Keep one file and cap the cell length.** It fixes the unreadable cells and nothing else: the table is still one list with no subject, and the file stays over 200 KB.
- **Keep the tests inline in the table.** One place to read, but the table is then mostly test names, which is what made it unreadable. The Evidence entry keeps them one click from the row.
- **Generate the whole model from annotations in the code.** The claims would sit beside their tests, but a By decision row has no code to sit beside, and a reviewer reads the model as prose.
- **A path filter on the CI job.** Cheaper, but the citations name tests across the whole tree, so any filter narrower than the tree misses renames.

## Consequences

- A reader starts from the attacker or the subject, and reads a section of a few dozen rows.
- A renamed or removed test fails the lint until its row is updated, so a pull request that moves a protection's test also touches the model.
- A row's id is stable: other documents can cite `KUBE-12`. Removing a row leaves a gap in the numbering.
- The model is about twice its old size on disk, since every row carries its own Evidence entry.
- A test cited by a *Pending rebuild* row is not checked, since the rebuild owes it.
