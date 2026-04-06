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

3. **Find latest tag for each component** — run these in parallel:

   ```sh
   git tag --list "dashboard/v*" | sort -V | tail -1
   git tag --list "cluster-api/v*" | sort -V | tail -1
   git tag --list "cluster-agent/v*" | sort -V | tail -1
   ```

4. **Check for changes since latest tag** — for each component, run:

   ```sh
   # dashboard
   git log {latest-tag}..HEAD --oneline -- modules/dashboard/ dashboard-ui/

   # cluster-api
   git log {latest-tag}..HEAD --oneline -- modules/cluster-api/

   # cluster-agent
   git log {latest-tag}..HEAD --oneline -- crates/cluster_agent/
   ```

   - If no commits touch a component's paths, skip that component (no release needed)
   - `modules/shared/` changes that affect a component count as changes to that component
   - `crates/types/` changes that affect a Rust component count as changes to that component

5. **Determine new version using semver** — analyze commits for each component:
   - **Patch** (`Z+1`): bug fixes, tests, docs, refactors, chores, ci changes
   - **Minor** (`Y+1`, reset `Z=0`): new backwards-compatible features
   - **Major** (`X+1`, reset `Y=0`, `Z=0`): breaking changes

   Use conventional commit prefixes as hints (`fix:` → patch, `feat:` → minor, `BREAKING CHANGE` → major), but read the actual messages to make a judgment call.

6. **Confirm with user before tagging** — present a summary table:

   ```
   Component     | Current tag          | New tag            | Reason
   --------------|----------------------|--------------------|-------
   dashboard     | dashboard/v0.0.9     | dashboard/v0.1.0   | new feature: collapsible sidebar
   cluster-api   | cluster-api/v0.0.8   | cluster-api/v0.1.0 | new feature: graceful shutdown
   cluster-agent | cluster-agent/v0.0.3 | (skip)             | no changes
   ```

   Wait for explicit user confirmation before proceeding.

7. **Tag and push sequentially** — for each component that needs a release (order: `dashboard`, `cluster-api`, `cluster-agent`):

   ```sh
   git tag -s {component}/v{X.Y.Z} -m "release: {component}/v{X.Y.Z}"
   git push {upstream} {component}/v{X.Y.Z}
   ```

   Do these one at a time. Confirm each push succeeded before moving to the next.

## Rules

- NEVER skip the user confirmation step — always show the summary table and wait for approval.
- NEVER tag or push without user confirmation.
- NEVER use `--no-verify` or `--force` when pushing tags.
- If `git status` shows uncommitted changes, stop immediately and tell the user to commit or stash first.
- If HEAD is not at `{upstream}/main`, stop immediately and tell the user to check out `{upstream}/main` first.
