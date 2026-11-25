---
title: GitHub Releases is the changelog, and the releaser writes the notes with a skill over deterministic tooling
date: 2026-11-06
scope: repo
status: Accepted
amended_by:
  - [The agent posts a draft's release notes with gh when the user asks](2026-11-07-the-agent-posts-the-notes-when-the-user-asks.md)
---

# GitHub Releases is the changelog, and the releaser writes the notes with a skill over deterministic tooling

## Context

`release-cli.yml` and `release-desktop.yml` create a draft GitHub release with `--notes ""`, so
every release's notes were written by hand (#777). GitHub's generated notes list every PR but read
poorly: one line per PR, internal changes mixed in, no grouping by what a user sees.

Two products release from one repository, each on its own tags (`cli/v*`, `desktop/v*`), each
with rcs. Squash merges give every PR one commit whose subject is a conventional commit ending in
`(#NNN)`, and PR bodies follow a template with `Summary` and `Key Changes`.

PR bodies from outside contributors are attacker-controlled. Whatever reads them to write prose
runs beside the releaser's shell and `gh` credentials, often with permissions bypassed.

## Decision

GitHub Releases is the changelog. There is no `CHANGELOG.md`.

The releaser writes each release's notes with the `release-notes` skill
(`.claude/skills/release-notes/SKILL.md`), run against the draft. The skill writes only prose, in
`notes.json`. `tools/releasenotes` does everything that must be deterministic:

- `collect` reads the range: `git log <prev>..<tag> -- <paths>`, where the paths are the
  product's `release-<product>` filter in `.github/ci-config/path-filters.yml`. A stable release
  starts after the product's last stable tag, so its rcs fold in; an rc starts after the
  product's previous tag of any kind. For each PR it adds the commit's type, scope and `!`, and
  the PR's labels and author. The title, `Summary` and `Key Changes` go under a
  field marked `untrusted`. It also lists the range's new contributors.
- `check` fails a citation outside the range, and a `feat`, `fix` or `!` PR neither cited nor
  omitted with a reason. It also fails prose holding a link, HTML, a mention or `#N`.
- `render` checks, then writes the Markdown. It builds every link and adds a collapsed list of
  every change in the range, the new contributors, the compare link and a pointer to
  `SHA256SUMS(.asc)`.
- `publish` prints the notes, reads a yes from the terminal and runs `gh release edit --notes-file`.

Each release workflow's step summary tells the releaser to run the skill before publishing the draft.

## Alternatives considered

- **GitHub's generated notes.** Complete, but one line per PR, ungrouped, internal changes
  included. They stay available as `render`'s "All changes" list.
- **A committed `CHANGELOG.md`.** Releases are tag-triggered, so it needs a bot commit after the
  tag or a release PR before it. Releases already holds the notes, and the website and app can
  read them there.
- **Notes from commit subjects alone (release-please, git-cliff).** Deterministic, but a subject
  is written for a reviewer. Related PRs stay separate items, and nothing says what a user sees.
- **A skill that does it all with `gh`.** Simpler, but then the agent that reads attacker text
  also builds the links and writes to GitHub, and nothing checks that a fix was not dropped.
- **Writing the notes inside the release workflows.** It needs a model API key in CI and a
  reviewer anyway. `claude-code-action` can run the same `SKILL.md` later if drafts are wanted.

## Consequences

- Only `collect` reads PR text, only `render` builds links, and only `publish`, after a person
  answers on a terminal, writes to GitHub. The record is
  [release notes from pull request text](../security/2026-11-06-release-notes-from-pull-request-text.md).
- `publish` reads the terminal, never standard input, so an agent cannot answer it. The releaser
  runs it in a terminal of their own.
- A product's paths live in `path-filters.yml`. A new top-level directory that ships in the
  desktop app must be added to `release-desktop`, or its PRs leave the notes.
- Tags order as semver does, so `-rc9` follows `-rc10`. A tenth rc needs a dot: `-rc.10`.
- The type and `!` come from the squash commit's subject, so a subject that is not a
  conventional commit's makes its PR optional for `check`.
- A commit pushed without a PR is listed under "All changes" but cannot be cited.
- New contributors are authors with no PR merged before the previous tag's commit, as GitHub's
  search sees it. Every author of a product's first release is new.

## Revisit when

Releases need notes with no person present, or another surface (the website's changelog, the
updater's `latest.json`, an in-app "What's new") needs more than the highlights paragraph.
