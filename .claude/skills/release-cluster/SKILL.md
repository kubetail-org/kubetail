---
name: release-cluster
description: Create GitHub releases for dashboard, cluster-api, and cluster-agent components by tagging the repo with the appropriate semver version.
disable-model-invocation: true
---

# Release Skill

Tag and push new semver releases for `dashboard`, `cluster-api`, and `cluster-agent` components.

## Steps

1. **Detect upstream remote** — find the remote that points to `kubetail-org/kubetail`:

   ```sh
   git remote -v | grep 'kubetail-org/kubetail' | head -1 | awk '{print $1}'
   ```

   Store the result as `{upstream}`. If no remote matches, stop and ask the user which remote to use.

2. **Verify branch and state**:
   - Run `git fetch {upstream}` to update remote refs
   - Run `git status -uno` to confirm working tree is clean
   - Run `git rev-parse HEAD` and `git rev-parse {upstream}/main` to confirm HEAD is at `{upstream}/main`

   If working tree is dirty or HEAD is not at `{upstream}/main`, stop and ask the user to resolve before continuing.

3. **Recommend a version for each component** — follow `.claude/skills/release-version/SKILL.md` for `dashboard`, `cluster-api` and `cluster-agent` in turn. It finds each component's latest tag, reads the commits under its paths since then and picks the next version under SemVer.

   - If it finds nothing to release for a component, skip that component.
   - Take its recommended version as the new tag, or the alternative it offers if the user picks that.

4. **Confirm with user before tagging** — present a summary table:

   ```
   Component     | Current tag          | New tag            | Reason
   --------------|----------------------|--------------------|-------
   dashboard     | dashboard/v0.0.9     | dashboard/v0.1.0   | new feature: collapsible sidebar
   cluster-api   | cluster-api/v0.0.8   | cluster-api/v0.1.0 | new feature: graceful shutdown
   cluster-agent | cluster-agent/v0.0.3 | (skip)             | no changes
   ```

   Wait for explicit user confirmation before proceeding.

5. **Tag and push sequentially** — for each component that needs a release (order: `dashboard`, `cluster-api`, `cluster-agent`):

   ```sh
   git tag -s {component}/v{X.Y.Z} -m "release: {component}/v{X.Y.Z}"
   git push {upstream} {component}/v{X.Y.Z}
   ```

   Do these one at a time. Confirm each push succeeded before moving to the next.

6. **Write the notes** — `release-docker.yml` builds the images but opens no release. For each component tagged, tell the user to draft a release for the tag on GitHub, then run `/release-notes {component} {component}/v{X.Y.Z}`, which writes the notes to the draft. The user publishes the draft on GitHub.

## Rules

- NEVER skip the user confirmation step — always show the summary table and wait for approval.
- NEVER tag or push without user confirmation.
- NEVER use `--no-verify` or `--force` when pushing tags.
- If `git status` shows uncommitted changes, stop immediately and tell the user to commit or stash first.
- If HEAD is not at `{upstream}/main`, stop immediately and tell the user to check out `{upstream}/main` first.
