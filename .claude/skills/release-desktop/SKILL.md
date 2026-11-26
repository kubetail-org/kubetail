---
name: release-desktop
description: Create a GitHub release of Kubetail Desktop by tagging the repo desktop/v<semver>.
disable-model-invocation: true
---

# Release Skill

Tag and push a new semver release for the `desktop` component.

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

3. **Recommend the version** — follow `.claude/skills/release-version/SKILL.md` for `desktop`. It finds the latest `desktop/v*` tag, reads the commits under the desktop's paths since then and picks the next version under SemVer.

   - If it finds nothing to release, stop.
   - Take its recommended version as `{X.Y.Z}`, or the alternative it offers if the user picks that.

4. **Check the manifests** — the tagged tree should name the version it releases. `version` in `apps/desktop/src-tauri/tauri.conf.json` and `apps/desktop/package.json`, `[package] version` in `apps/desktop/src-tauri/Cargo.toml`, and the `kubetail-desktop` entry of `apps/desktop/src-tauri/Cargo.lock` hold `{X.Y.Z}` without its prerelease. The build stamps the tag's version itself, so a stale manifest fails nothing, but if one differs, stop and ask the user to land a commit that sets them first.

5. **Confirm with user before tagging** — present a summary:

   ```
   Component | Current tag      | New tag          | Reason
   ----------|------------------|------------------|-------
   desktop   | desktop/v0.1.0   | desktop/v0.2.0   | new feature: log viewer
   ```

   Wait for explicit user confirmation before proceeding.

6. **Tag and push**:

   ```sh
   git tag -s desktop/v{X.Y.Z} -m "release: desktop/v{X.Y.Z}"
   git push {upstream} desktop/v{X.Y.Z}
   ```

   Confirm the push succeeded.

7. **Write the notes** — `release-desktop.yml` builds, signs and notarizes every platform, then opens a draft release for the tag, a prerelease when the version has a `-`. Once it has, tell the user to run `/release-notes desktop desktop/v{X.Y.Z}`, which writes the notes to the draft.

8. **Publish** — the user publishes the draft on GitHub, and **leaves *Set as the latest release* unticked**. The repo's latest release is the CLI's: the README's download link and the CLI's packages read it. Publishing a final sends it to every installed app on macOS and Windows: `publish-desktop-updates.yml` copies its manifests to `updates.kubetail.com`. Confirm that run succeeded. A pre-release reaches no installed app.

## Rules

- NEVER skip the user confirmation step — always show the summary and wait for approval.
- NEVER tag or push without user confirmation.
- NEVER use `--no-verify` or `--force` when pushing tags.
- If `git status` shows uncommitted changes, stop immediately and tell the user to commit or stash first.
- If HEAD is not at `{upstream}/main`, stop immediately and tell the user to check out `{upstream}/main` first.
