# Architecture Decision Records

An ADR records **why** a design is the way it is: the forces, the alternatives rejected, and what the decision costs. The `CLAUDE.md` guides record **what is true now**: invariants, conventions, commands, traps. Keeping the two apart is the point. Rationale is read once; the guides are read on every task.

Rule of thumb: a paragraph in a `CLAUDE.md` that explains *why* rather than *what*, or names an option we didn't take, belongs here. So does a code comment that argues for a choice.

## Filenames

```
docs/adr/YYYY-MM-DD-short-slug.md
```

The date is the day the ADR was **written**. For a decision made earlier, the body says roughly when it was made. Several ADRs can share a date. There is one flat sequence for the whole repo, with no per-area directories: the decisions with the most reasoning behind them cross areas and would have no natural home. Scope is a frontmatter field instead.

## Frontmatter

```yaml
---
title: Reach the Cluster API through the aggregation layer   # an imperative phrase
date: 2026-11-09
scope: cross-cutting
status: Accepted
amended_by: 2026-12-01-some-later-decision.md                # only when a later ADR replaces part of this one
---
```

`scope` is one of `cross-cutting`, `dashboard-ui`, `dashboard`, `cluster-api`, `cli`, `shared`, `cluster-agent`, `proto` or `repo`.

## Sections

Context, Decision, Alternatives considered, Consequences, and Revisit when. Copy [`TEMPLATE.md`](TEMPLATE.md) and keep it to about a page. Prefer prose where the reasoning is a chain.

**Alternatives considered is required.** An ADR with no rejected option is documentation, and belongs in a `CLAUDE.md`.

## Referencing an ADR

Link by path and name it by its title or slug, never by date alone:

```markdown
→ [ADR: reach the Cluster API through the aggregation layer](docs/adr/2026-11-09-reach-the-cluster-api-through-the-aggregation-layer.md)
```

The path is relative to the file that holds the link, so a nested guide climbs to the root (`../../docs/adr/…` from `modules/cli/CLAUDE.md`). The `AGENTS.md` symlinks resolve it the same way.

## Status

- **`Accepted`**: in force. The only status a `CLAUDE.md` may link.
- **`Superseded by [<slug>](<path>)`**: kept for the record. Nothing outside `docs/adr/` links it.

Superseding is a normal change. Write a new ADR, flip the old one's status, and **repoint every `CLAUDE.md` link in the same commit**. A guide that links a superseded ADR is the one way this system rots.

**A later ADR may replace part of an accepted one.** The newer names what it replaces. The older keeps its status, since the rest of it is still in force, and gains an `amended_by` pointer, a list when there are several. Adding the pointer is a metadata edit, like flipping a status; the decision itself is never rewritten. When the whole decision goes, supersede it.

## Exemption from "describe the present"

The root `CLAUDE.md` asks code comments and docs to describe only the current code. **`docs/adr/` is the one exemption.** ADRs are an append-only record: what we rejected, what we replaced and what we believed at the time is what they are for. Edit an accepted ADR only to fix an error or flip its status. A path or name the code has moved away from is updated in place; that is a fix. A decision that changed gets a new ADR.

## Index

| Date | ADR | Scope | Status |
| --- | --- | --- | --- |
| 2026-11-09 | [Record architecture decisions as ADRs](2026-11-09-record-architecture-decisions.md) | repo | Accepted |
| 2026-11-09 | [Commit generated Go and TypeScript, generate Rust at build time](2026-11-09-commit-generated-go-and-typescript-generate-rust-at-build-time.md) | cross-cutting | Accepted |
| 2026-11-09 | [Embed the UI by a copy step and render from the Vite manifest](2026-11-09-embed-the-ui-by-a-copy-step-and-render-from-the-vite-manifest.md) | cross-cutting | Accepted |
| 2026-11-09 | [Forward the caller's identity hop by hop](2026-11-09-forward-the-callers-identity-hop-by-hop.md) | cross-cutting | Accepted |
| 2026-11-09 | [GraphQL for user-facing APIs, gRPC to the agent](2026-11-09-graphql-for-user-facing-apis-grpc-to-the-agent.md) | cross-cutting | Accepted |
| 2026-11-09 | [One implementation per environment behind an interface](2026-11-09-one-implementation-per-environment-behind-an-interface.md) | cross-cutting | Accepted |
| 2026-11-09 | [Reach the Cluster API through the aggregation layer](2026-11-09-reach-the-cluster-api-through-the-aggregation-layer.md) | cross-cutting | Accepted |
| 2026-11-09 | [Read logs through two backends behind LogFetcher](2026-11-09-read-logs-through-two-backends-behind-logfetcher.md) | cross-cutting | Accepted |
| 2026-11-09 | [Authenticate the front proxy in middleware](2026-11-09-authenticate-the-front-proxy-in-middleware.md) | cluster-api | Accepted |
| 2026-11-09 | [Defend against CSRF in two layers](2026-11-09-defend-against-csrf-in-two-layers.md) | dashboard | Accepted |
| 2026-11-09 | [Check hook dependencies in the dashboard UI](2026-11-09-check-hook-dependencies-in-the-dashboard-ui.md) | dashboard-ui | Accepted |
| 2026-11-09 | [Forbid any in the dashboard UI outside tests](2026-11-09-forbid-any-in-the-dashboard-ui-outside-tests.md) | dashboard-ui | Accepted |
| 2026-11-09 | [Lint the dashboard UI with oxlint's categories and a chosen rule list](2026-11-09-lint-the-dashboard-ui-with-oxlints-categories-and-a-chosen-rule-list.md) | dashboard-ui | Accepted |
| 2026-11-09 | [Override the codegen client preset's v6 defaults](2026-11-09-override-the-codegen-client-presets-v6-defaults.md) | dashboard-ui | Accepted |
| 2026-11-09 | [Require switches over a union to list every member](2026-11-09-require-switches-over-a-union-to-list-every-member.md) | dashboard-ui | Accepted |
| 2026-11-09 | [Dependabot moves dependencies, and a person merges every update](2026-11-09-dependabot-moves-dependencies-and-a-person-merges-every-update.md) | repo | Accepted |
| 2026-11-09 | [Each Go module builds without go.work](2026-11-09-each-go-module-builds-without-go-work.md) | repo | Accepted |
| 2026-11-09 | [Each component owns its config](2026-11-09-each-component-owns-its-config.md) | repo | Accepted |
| 2026-11-09 | [Gate PRs on changed lines with Codecov's merged result](2026-11-09-gate-prs-on-changed-lines-with-codecovs-merged-result.md) | repo | Accepted |
| 2026-11-09 | [Keep the sandbox's build outputs off the host mount](2026-11-09-keep-the-sandboxs-build-outputs-off-the-host-mount.md) | repo | Accepted |
| 2026-11-09 | [Measure Go coverage across the workspace](2026-11-09-measure-go-coverage-across-the-workspace.md) | repo | Accepted |
| 2026-11-09 | [Pin fsnotify to v1.8.0](2026-11-09-pin-fsnotify-to-v1-8-0.md) | repo | Accepted |
| 2026-11-09 | [Release each component by its own tag](2026-11-09-release-each-component-by-its-own-tag.md) | repo | Accepted |
| 2026-11-10 | [Attest build provenance in the release jobs](2026-11-10-attest-build-provenance-in-the-release-jobs.md) | repo | Accepted |
| 2026-11-10 | [Pin each toolchain to one version that CI and the images share](2026-11-10-pin-each-toolchain-to-one-version-that-ci-and-the-images-share.md) | repo | Accepted |
| 2026-11-10 | [Run each OS once on a PR and the other platforms on main](2026-11-10-run-each-os-once-on-a-pr-and-the-other-platforms-on-main.md) | repo | Accepted |
| 2026-11-10 | [Test each component on the platforms it ships for, or record why not](2026-11-10-test-each-component-on-the-platforms-it-ships-for.md) | repo | Accepted |
| 2026-11-09 | [Tests sit beside their files, never sleep, and take seams as parameters](2026-11-09-tests-sit-beside-their-files-never-sleep-and-take-seams-as-parameters.md) | repo | Accepted |
| 2026-11-10 | [Write release notes with a skill over deterministic tooling](2026-11-10-write-release-notes-with-a-skill-over-deterministic-tooling.md) | repo | Accepted |
| 2026-11-09 | [Share informers and gate each caller with an access review](2026-11-09-share-informers-and-gate-each-caller-with-an-access-review.md) | shared | Accepted |
