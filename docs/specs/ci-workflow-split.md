# CI workflow split: spec

**Status: draft. Not ready to implement.** The questions below need answers first.

This spec follows [`monorepo-refactor.md`](monorepo-refactor.md). Start it after the folders have moved.

## Goal

- **Workflow files named by component**, so a reader finds a component's CI in one file.
- **One required check, `ci-ok`**, that passes only when every job that ran passed.

## Proposed shape

`ci.yml` stays the one entry point. Its Plan job reads the path filters and calls one reusable workflow per component.

```text
.github/workflows/
├── ci.yml              entry point: Plan, calls the files below, ends in ci-ok
├── ci-cli.yml          reusable
├── ci-dashboard.yml    reusable
├── ci-cluster.yml      reusable (the Cluster API and the Cluster Agent)
├── ci-web.yml          reusable
├── ci-repo.yml         reusable: shell, YAML, Markdown, toolchains, generated code
└── …                   codeql, coverage, audit, release-*, publish-*: not split
```

`codeql.yml`, `coverage.yml` and `audit.yml` stay split by language. CodeQL analyses one language across the whole repo, and the coverage gate needs one merged result.

## Open questions

Each of these blocks the work.

1. **Who tests `libs/go-common`?** Today one job tests every Go module, so the modules share one cache. Split by component, the shared library is either tested in each caller or needs a home of its own. It must keep the CLI's full OS and arch matrix on `main`, because it ships inside the CLI.
2. **Who owns the jobs that cross components?** `Docker · Build`, the end-to-end suite and the `tools/` tests belong to no single component.
3. **What are the filter keys?** `ci.yml` and `coverage.yml` decide from the same filters today, so a path that runs a test also measures its coverage. Component keys in one and language keys in the other let them drift. Either both move to component keys, or `ci.yml` maps components back to languages.
4. **How does `ci-ok` cover the coverage gate?** `coverage / Gate` runs in another workflow, and a job cannot depend on a job in another workflow. Either `ci.yml` calls coverage as a reusable workflow, or both checks are required.
5. **How do the caches work?**
   - `CACHE_MODE` is set in `ci.yml`'s `env`. A called workflow does not see it, so it becomes an input.
   - Go cache keys start with the job id. Two called workflows that each have a `go-test` job write the same key.
   - The Alpine, 386 and end-to-end jobs read the `go-test-linux-amd64` cache by name.
   - More jobs mean more caches, against the quota the platform ADR budgets for.
6. **Is the split worth it?** A component here spans languages (the Dashboard is Go and TypeScript, the cluster is Go and Rust), and the jobs are built around languages. If the answers above cost more than the file names are worth, keep `ci.yml` whole and add only `ci-ok`.

## Every change here updates these

- `.github/ci-config/path-filters.yml`: the new `ci-*.yml` files join the `shared` and `shell` filters. Otherwise an edit to `ci-cli.yml` runs only the YAML lint.
- The job names in the root guide, and in the two ADRs that name them: "Run each OS once on a PR and the other platforms on main" and "Test each component on the platforms it ships for".
- "Dependabot moves dependencies, and a person merges every update": it rests on CI's jobs not being required checks. A required `ci-ok` meets its "Revisit when". Write a new ADR that amends it.

## Done when

- The open questions are answered here, and this spec's status says ready.
- `ci-ok` is required on `main`, and the coverage gate is still enforced.
- A pull request that changes one component runs that component's jobs and no others.
- A push to `main` saves the same caches as before, with no key written twice.
- This spec is deleted.
