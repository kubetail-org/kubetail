---
name: release-version
description: Recommend the next release version of a Kubetail component (cli, dashboard, cluster-api, cluster-agent or desktop) under SemVer 2.0.0. Use when asked which version to release or tag next.
argument-hint: cli | dashboard | cluster-api | cluster-agent | desktop
allowed-tools: Bash(git tag:*), Bash(git log:*), Bash(git show:*), Bash(git diff:*), Bash(git fetch:*), Bash(git rev-parse:*), Bash(git merge-base:*), Bash(apps/desktop/scripts/check-release-version.sh:*), Read, Grep
---

# Recommend the next release version

Recommend the next version of one component, given as the argument: `cli`, `dashboard`,
`cluster-api`, `cluster-agent` or `desktop`. If the argument is missing or is anything else, ask
which component and stop.

**This skill recommends and nothing else.** It never creates or pushes a tag, edits a file or
dispatches a workflow. A person tags with the `release-cli`, `release-cluster` or `release-desktop`
skill.

The rules are [SemVer 2.0.0](https://semver.org/).

## The components

| Component       | Tag                        | Workflows                               | Paths                                                                       |
| --------------- | -------------------------- | --------------------------------------- | --------------------------------------------------------------------------- |
| `cli`           | `cli/v<version>`           | `release-cli.yml`, `release-docker.yml` | `cli/` `dashboard/` `libs/go-common/` `libs/graphql/dashboard/` `apps/web/` |
| `dashboard`     | `dashboard/v<version>`     | `release-docker.yml`                    | `dashboard/` `libs/go-common/` `libs/graphql/dashboard/` `apps/web/`        |
| `cluster-api`   | `cluster-api/v<version>`   | `release-docker.yml`                    | `cluster/api/` `libs/go-common/` `libs/graphql/cluster-api/` `libs/proto/`  |
| `cluster-agent` | `cluster-agent/v<version>` | `release-docker.yml`                    | `cluster/agent/` `libs/proto/`                                              |
| `desktop`       | `desktop/v<version>`       | `release-desktop.yml`                   | `apps/desktop/src/` `apps/desktop/src-tauri/` `apps/desktop/sidecar/` `apps/desktop/proto/` |

Each component's full list, with its Dockerfile and default config, is its `release-<component>`
filter in `.github/ci-config/path-filters.yml`. Use those globs as the paths below. The CLI
embeds the Dashboard, so the Dashboard's changes are the CLI's too. The desktop ships its sidecar
inside the app, so the sidecar's changes are the desktop's.

## 1. Find the last release

Fetch tags first, so a tag pushed from elsewhere is seen: `git fetch --tags --quiet` against the
remote that points to `kubetail-org/kubetail`. If the fetch fails, carry on with the local tags and
say so.

List the component's tags (`git tag -l '<component>/v*'`) and take the highest **by SemVer
precedence**, not by date or by `sort -V`. Precedence (SemVer §11):

1. Compare major, minor and patch as numbers.
2. With those equal, a version with a prerelease sorts **before** the one without
   (`1.0.0-rc.1` < `1.0.0`).
3. Two prereleases compare identifier by identifier, split on `.`. A numeric identifier compares as
   a number and sorts before an alphanumeric one; alphanumeric identifiers compare in ASCII order.
   With all shared identifiers equal, the shorter list sorts first.
4. Build metadata (`+…`) is ignored.

Skip a tag whose version fails step 5's check, and say you skipped it. If the component has no
tag, say so and stop, except for the desktop.

**The desktop's first release.** With no `desktop/v*` tag, recommend the version in
`apps/desktop/src-tauri/tauri.conf.json` and apply no bump. Still list the commits from step 2,
over the whole history, as what the release holds.

## 2. Collect the commits

```sh
git log --no-merges --format='%h%x09%s' <last-tag>..HEAD -- <paths>
```

Read each commit's body as well (`git log --format=%B -1 <sha>`) for a `BREAKING CHANGE:` or
`BREAKING-CHANGE:` footer. A squash-merged PR's body may list several commits; read it whole.

If the list is empty, say there is nothing to release and stop.

## 3. Classify each commit

From the conventional-commit header, `type(scope)!: subject`:

| Header                                                      | Level |
| ----------------------------------------------------------- | ----- |
| `!` after the type or scope, or a breaking footer           | major |
| `feat`                                                      | minor |
| `fix`, `perf`                                               | patch |
| `docs`, `test`, `chore`, `refactor`, `ci`, `style`, `build` | none  |

A header that is not a conventional commit gets no level from its header: read its diff. A
`build(deps)` or `fix(deps)` bump counts as a patch when it changes what ships (a Go module, a
crate or an npm package the component uses), and as none when it touches only tests or tooling.

**Read the diff (`git show --stat <sha>`, then the relevant hunks) when the type looks wrong for
the change.** For example, a `refactor` that renames a CLI flag or a config key, a `chore` that
changes what a user sees, or a `fix` that removes a behaviour someone relied on. A commit under
`libs/go-common/` or `libs/proto/` counts only for the components whose code it changes. Classify by
what the change does to the component's users, and name each commit you reclassified and why.

What counts as breaking:

- **`cli`:** removing or renaming a command, flag, argument or config key, changing an exit code,
  or changing output a script may parse.
- **`dashboard`:** removing or renaming a flag, environment variable or config key, or needing a
  permission the Helm chart's current release does not grant. Its GraphQL API serves its own UI
  alone; a change there is not breaking.
- **`cluster-api`:** the same as the Dashboard, plus removing or changing a GraphQL field that a
  released Dashboard or `kubetail logs` reads, since a cluster runs them at different versions.
- **`cluster-agent`:** the same as the Dashboard, plus a change to the gRPC contract in `libs/proto/`
  that a released Cluster API cannot speak.
- **`desktop`:** dropping a supported OS or architecture, or losing or misreading data a previous
  version persisted (`apps/desktop/docs/persisted-data.md`). The GraphQL and gRPC surfaces are
  internal to the app; a change there alone is not breaking.

The release's level is the highest level among the commits. If it is none, say there is nothing
to release and stop.

## 4. Pick the version

Let the last release be `X.Y.Z`, with or without a prerelease.

**From a release (`X.Y.Z`, no prerelease):**

- At `1.0.0` or later: major → `(X+1).0.0`, minor → `X.(Y+1).0`, patch → `X.Y.(Z+1)`.
- **Before `1.0.0`:** a breaking change or a `feat` bumps the minor, `0.Y.Z` → `0.(Y+1).0`; a
  `fix` or `perf` bumps the patch, `0.Y.(Z+1)`. SemVer leaves `0.x` unstable, and this is the
  convention every component keeps, but for the desktop, where a `feat` bumps the patch. Say
  plainly when this rule applies.
- A bump resets every field below it (`0.3.4` → `0.4.0`).

**From a prerelease (`X.Y.Z-<pre>`):** first find the version the prerelease leads to. Apply the
release's level, by the rules above, to the highest release tag of the component below `X.Y.Z`
(`0.0.0` when there is none).

- If that gives `X.Y.Z` or lower, `X.Y.Z` already holds these changes. **Offer both:** the next
  prerelease of `X.Y.Z`, and `X.Y.Z` itself. Recommend one and say why (more changes to test
  favours the prerelease; a fix-only run after a tested candidate favours the release).
- If it gives a higher version, the changes outgrow `X.Y.Z`: recommend that higher version, and
  offer its first prerelease (`-rc.1`) beside it.

**The next prerelease** increments the last numeric identifier: `rc.1` → `rc.2`, `beta.3` →
`beta.4`; one with no numeric identifier gains `.1` (`beta` → `beta.1`). Recommend dot-separated
numbers (`rc.1`, `rc.2`) for a new version: `rc10` sorts before `rc2`, since an alphanumeric
identifier compares as text.

**The old form (`rc1`).** Every prerelease tagged so far uses it (`dashboard/v0.17.0-rc3`). Do not
switch forms within one version: `0.17.0-rc.2` sorts **before** `0.17.0-rc1` (`rc` < `rc1`). The
next prerelease of a version in the old form is `rc<n+1>`, and the dotted form starts with the next
version. Say this when it applies.

Never recommend build metadata (`+…`).

**The desktop's prereleases are `rc.<n>` alone**: its RPM and MSI versions are derived from that
form. Never recommend `beta.<n>` or `rc<n>` for it.

## 5. Check the version

The version must match SemVer without build metadata, which `tools/releasenotes` and the
workflows' prerelease check (a `-` in the version) rely on:

```
^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$
```

For the desktop, run the check `release-desktop.yml` runs; it exits non-zero on a version the
workflow rejects:

```sh
apps/desktop/scripts/check-release-version.sh desktop <version>
```

Check too that the tag does not exist yet (`git rev-parse -q --verify refs/tags/<tag>` prints
nothing) and that it sorts above the last release. If any check fails, fix the recommendation
before printing it.

## 6. Report

Print, in this order:

1. **Recommended version**, and the alternative when step 4 offers two.
2. **Last release**: its tag and commit, or "none, first release".
3. **Bump level**: major, minor or patch, and the rule that turned it into this version
   (the `0.x` rule, a prerelease, or none).
4. **Commits that drove it**: each commit at the release's level as `sha subject`, then a count
   of the lower-level and no-level commits. Name any commit reclassified from its diff.
5. **Helm chart changes**: for a cluster component, each commit whose PR says the chart needs a
   new permission, Secret, port, environment variable or config key. The chart, in
   `kubetail-org/helm-charts`, must ship it with this release.
6. **Next step**: tag it with `/release-cli`, `/release-cluster` or `/release-desktop`, then write
   the notes with `/release-notes <component> <tag>`.
