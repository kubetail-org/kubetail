---
name: release-version
description: Recommend the next release version of Kubetail Desktop under SemVer 2.0.0. Use when asked which version to release or tag next.
argument-hint: desktop
allowed-tools: Bash(git tag:*), Bash(git log:*), Bash(git show:*), Bash(git diff:*), Bash(git fetch:*), Bash(git rev-parse:*), Bash(git merge-base:*), Read, Grep
---

# Recommend the next release version

Recommend the next version of one product, `desktop`, given as the argument. If the argument is
anything else, say it is not a product here and stop.

**This skill recommends and nothing else.** It never creates or pushes a tag, edits a file or
dispatches a workflow. Releasing is the user's step.

The rules are [SemVer 2.0.0](https://semver.org/).

## The products

| Product   | Tag                  | Workflow                                | Paths                                     | Untagged start |
| --------- | -------------------- | --------------------------------------- | ----------------------------------------- | -------------- |
| `desktop` | `desktop/v<version>` | `.github/workflows/release-desktop.yml` | `src/` `src-tauri/` `sidecar/` `proto/`   | the manifest's version (`src-tauri/tauri.conf.json`) |

The sidecar ships inside the desktop app, so its changes are the desktop's.

## 1. Find the last release

Fetch tags first, so a tag pushed from elsewhere is seen: `git fetch origin --tags --quiet`. If the
fetch fails, carry on with the local tags and say so.

List the product's tags (`git tag -l 'desktop/v*'`) and take the highest
**by SemVer precedence**, not by date or by `sort -V`. Precedence (SemVer §11):

1. Compare major, minor and patch as numbers.
2. With those equal, a version with a prerelease sorts **before** the one without
   (`1.0.0-rc.1` < `1.0.0`).
3. Two prereleases compare identifier by identifier, split on `.`. A numeric identifier compares as
   a number and sorts before an alphanumeric one; alphanumeric identifiers compare in ASCII order.
   With all shared identifiers equal, the shorter list sorts first.
4. Build metadata (`+…`) is ignored.

Skip a tag whose version does not pass its product's workflow check (step 5), and say you skipped it.

**No tag:** this is the product's first release. Recommend the untagged start (the version in
`src-tauri/tauri.conf.json`) as the version, apply no bump, and say
so. Still list the commits from step 2, over the whole history, as what the release holds.

## 2. Collect the commits

```sh
git log --no-merges --format='%h%x09%s' <last-tag>..HEAD -- <paths>
```

Read each commit's body as well (`git log --format=%B -1 <sha>`) for a `BREAKING CHANGE:` or
`BREAKING-CHANGE:` footer. A squash-merged PR's body may list several commits; read it whole.

If the list is empty, say there is nothing to release and stop.

## 3. Classify each commit

From the conventional-commit header, `type(scope)!: subject`:

| Header                                              | Level |
| --------------------------------------------------- | ----- |
| `!` after the type or scope, or a breaking footer    | major |
| `feat`                                               | minor |
| `fix`, `perf`                                        | patch |
| `docs`, `test`, `chore`, `refactor`, `ci`, `style`, `build` | none |

A header that is not a conventional commit gets no level from its header: read its diff.

**Read the diff (`git show --stat <sha>`, then the relevant hunks) when the type looks wrong for
the change.** For example, a `refactor` that renames a setting, a `chore` that
changes what a user sees, or a `fix` that removes a behaviour someone relied on. Classify by what
the change does to the product's users, and name each commit you reclassified and why.

What counts as breaking:

- **Desktop:** dropping a supported OS or architecture, or losing or misreading data a previous
  version persisted (`docs/persisted-data.md`). The GraphQL and gRPC surfaces are internal to the
  app; a change there alone is not breaking.

The release's level is the highest level among the commits. If it is none, say there is nothing
to release and stop.

## 4. Pick the version

Let the last release be `X.Y.Z`, with or without a prerelease.

**From a release (`X.Y.Z`, no prerelease):**

- At `1.0.0` or later: major → `(X+1).0.0`, minor → `X.(Y+1).0`, patch → `X.Y.(Z+1)`.
- **Before `1.0.0`:** a breaking change bumps the minor, `0.Y.Z` → `0.(Y+1).0`; a `feat`, `fix` or
  `perf` bumps the patch, `0.Y.(Z+1)`. SemVer leaves `0.x` unstable, and this is the convention the
  project keeps. Say plainly when this rule applies.
- A bump resets every field below it (`0.3.4` → `0.4.0`).

**From a prerelease (`X.Y.Z-<pre>`):** first find the version the prerelease leads to. Apply the
release's level, by the rules above, to the highest release tag of the product below `X.Y.Z`
(`0.0.0` when there is none).

- If that gives `X.Y.Z` or lower, `X.Y.Z` already holds these changes. **Offer both:** the next
  prerelease of `X.Y.Z`, and `X.Y.Z` itself. Recommend one and say why (more changes to test
  favours the prerelease; a fix-only run after a tested candidate favours the release).
- If it gives a higher version, the changes outgrow `X.Y.Z`: recommend that higher version, and
  offer its first prerelease (`-rc.1`) beside it.

**The next prerelease** increments the last numeric identifier: `rc.1` → `rc.2`, `beta.3` →
`beta.4`; one with no numeric identifier gains `.1` (`beta` → `beta.1`). Recommend dot-separated numbers (`rc.1`, `rc.2`): `rc10` sorts before `rc2`, since an
alphanumeric identifier compares as text.

Never recommend build metadata (`+…`): the workflows reject it.

**Desktop prereleases are `rc.<n>` only**: the RPM and MSI versions are derived from that form
alone. Never recommend `beta.<n>` or `rc<n>` for the desktop.

## 5. Check the version

The version must pass the check its product's workflow runs. Run it; it exits non-zero on a
version the workflow rejects:

```sh
scripts/check-release-version.sh desktop <version>
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
5. **Tag to push**, as commands for the user to run:

   ```sh
   git tag desktop/v<version> <sha>
   git push origin desktop/v<version>
   ```

   with `<sha>` the commit to release, `HEAD` unless the user names another. A tag with a `-`
   makes the workflow draft a prerelease.
6. **Desktop only, the manifests to set to the version** before tagging, so the tree matches the
   release: `src-tauri/tauri.conf.json` (`version`), `src-tauri/Cargo.toml` (`[package] version`),
   `package.json` (`version`), and the `kubetail-desktop` entry of `src-tauri/Cargo.lock`, which cargo
   rewrites from `Cargo.toml`. The release build stamps the tag's version itself (`tauri build -c`,
   `SIDECAR_VERSION`), so a stale manifest does not fail the release, but the tagged tree then
   names a different version.
