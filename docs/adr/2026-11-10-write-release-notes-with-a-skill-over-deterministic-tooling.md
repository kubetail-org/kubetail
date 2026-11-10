---
title: Write release notes with a skill over deterministic tooling
date: 2026-11-10
scope: repo
status: Accepted
---

# Write release notes with a skill over deterministic tooling

## Context

Four components release from one repository, each on its own tags (`cli/v*`, `dashboard/v*`, `cluster-api/v*`, `cluster-agent/v*`), some with rcs. `release-cli.yml` opens a draft GitHub release for the CLI; `release-docker.yml` builds the images and opens none. Picking the next version and writing each release's notes were done by hand, and the `release-cli` and `release-cluster` skills each carried their own copy of the component's paths and a rough bump rule.

Squash merges give every PR one commit whose subject is a conventional commit ending in `(#NNN)`, and PR bodies follow a template with `Summary` and `Key Changes`.

PR bodies from outside contributors are attacker-controlled. Whatever reads them to write prose runs beside the releaser's shell and `gh` credentials, often with permissions bypassed.

The kstack repository (`kstackhq/kstack`) had solved the same problem for its two products with a `release-version` skill, a `release-notes` skill and a Go tool, `tools/releasenotes`. In November 2026 they were copied here and adapted to the four components.

## Decision

The `release-version` skill recommends a component's next version from the commits since its last tag, by SemVer and the project's `0.x` rule (a `feat` or breaking change bumps the minor). It only reads git. `release-cli` and `release-cluster` follow it before they tag.

The `release-notes` skill writes only prose, in `notes.json`. `tools/releasenotes` does everything that must be deterministic:

- `collect` reads the range: `git log <prev>..<tag> -- <paths>`, where the paths are the component's `release-<component>` filter in `.github/ci-config/path-filters.yml`. A stable release starts after the component's last stable tag, so its rcs fold in; an rc starts after the component's previous tag of any kind. Each PR's title, `Summary` and `Key Changes` go under a field marked `untrusted`.
- `check` fails a citation outside the range, a `feat`, `fix` or `!` PR neither cited nor omitted with a reason, and prose holding a link, HTML, a mention or `#N`.
- `render` checks, then writes the Markdown, building every link.
- `publish` prints the notes, reads a yes from the terminal and runs `gh release edit --notes-file`.

When the user, in a later message, asks the agent to post notes they have seen, the skill lets the agent run exactly `gh release view` (to confirm a draft) and `gh release edit --notes-file`. It never publishes the draft.

## Alternatives considered

- **GitHub's generated notes.** Complete, but one line per PR, ungrouped, internal changes included. They stay available as `render`'s "All changes" list.
- **Notes from commit subjects alone (release-please, git-cliff).** Deterministic, but a subject is written for a reviewer. Related PRs stay separate items, and nothing says what a user sees.
- **A skill that does it all with `gh`.** Simpler, but then the agent that reads attacker text also builds the links and writes to GitHub, and nothing checks that a fix was not dropped.
- **`publish --yes` for the agent.** A steered agent could pass the flag without the user asking, so the confirmation would mean nothing.
- **Paths in each skill.** Two skills each held a copy, and they had drifted: `release-cluster` left `modules/shared/` out of the Cluster API. One filter per component in `path-filters.yml` serves the tool and both skills.

## Consequences

- Only `collect` reads PR text, only `render` builds links, and only `publish`, or the agent on the user's later request, writes to GitHub. The agent's limit is the skill's instructions, not a mechanism.
- A component's paths live in `path-filters.yml`. A directory a component starts to ship must be added to its `release-<component>` filter, or its PRs leave the notes. `modules/shared/` and `proto/` count for every component that ships them, so the notes omit a change there that does not touch the component, with a reason.
- Tags order as SemVer does, so `-rc9` follows `-rc10`. The existing `rc<n>` tags keep their form within a version; a new version's rcs use `rc.<n>`.
- The type and `!` come from the squash commit's subject, so a subject that is not a conventional commit's makes its PR optional for `check`.
- A cluster component has no draft until the releaser drafts one on GitHub.

## Revisit when

Releases need notes with no person present, `release-docker.yml` starts opening releases, or the harness can bind an agent to an allow-list of commands.
