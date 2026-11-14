---
name: release-cli
description: Create GitHub releases for the Kubetail CLI by tagging the repo with the appropriate semver version.
disable-model-invocation: true
---

# Release Skill

Tag and push a new semver release for the `cli` component.

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

3. **Recommend the version** — follow `.claude/skills/release-version/SKILL.md` for `cli`. It finds the latest `cli/v*` tag, reads the commits under the CLI's paths since then and picks the next version under SemVer.

   - If it finds nothing to release, stop.
   - Take its recommended version as `{X.Y.Z}`, or the alternative it offers if the user picks that.

4. **Confirm with user before tagging** — present a summary:

   ```
   Component | Current tag  | New tag      | Reason
   ----------|--------------|--------------|-------
   cli       | cli/v0.3.1   | cli/v0.4.0   | new feature: dark mode support
   ```

   Wait for explicit user confirmation before proceeding.

5. **Tag and push**:

   ```sh
   git tag -s cli/v{X.Y.Z} -m "release: cli/v{X.Y.Z}"
   git push {upstream} cli/v{X.Y.Z}
   ```

   Confirm the push succeeded.

6. **Write the notes** — `release-cli.yml` opens a draft release for the tag. Once it has, tell the user to run `/release-notes cli cli/v{X.Y.Z}`, which writes the notes to the draft. The user publishes the draft on GitHub.

## Rules

- NEVER skip the user confirmation step — always show the summary and wait for approval.
- NEVER tag or push without user confirmation.
- NEVER use `--no-verify` or `--force` when pushing tags.
- If `git status` shows uncommitted changes, stop immediately and tell the user to commit or stash first.
- If HEAD is not at `{upstream}/main`, stop immediately and tell the user to check out `{upstream}/main` first.
