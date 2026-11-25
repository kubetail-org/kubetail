# Step 1 — Rename, in Kstack

Built on a branch of `kstackhq/kstack`, where Kstack's own checks still run, and merged into its `main`. [The sequence's README](README.md) holds the decisions this step applies.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. Rename | Done | — |
| 2. Set the names | Done | — |
| 3. Move the endpoints | Done | — |
| 4. Narrow the reserved variables | Done | — |
| 5. Drop the `desktop/v0.0.1` cleanup | Done | — |
| 6. Collapse the migrations | Done | — |
| 7. Drop the history ADRs | Done | — |
| 8. Remove the CLI | Done | — |

1. **Rename.** Replace `Kstack Desktop`, `Kstack`, `kstack`, `KSTACK` and `kstackhq/kstack` with their Kubetail forms across files, file names and folder names. Replace `2026 The Kstack Authors` with `2024 The Kubetail Authors`. Leave the strings bound to the repo the step runs in, for step 3 to rewrite:
   - the fork gates, `github.repository == 'kstackhq/kstack'`;
   - the secret and variable names, `secrets.*` and `vars.*`;
   - the Codecov path fix, `github.com/kstackhq/kstack/::`;
   - the links to the repo in `.github/ISSUE_TEMPLATE/` and `CHANGELOG.md`;
   - the links to Kstack's own issues and pull requests (`github.com/kstackhq/kstack/issues/<n>`), which stay where they point.
2. **Set the names** in *Decisions*: `productName` and Linux's own in `tauri.linux.conf.json` with its `desktopTemplate`, the identifier, binaries, release files, packages, Linux helper, doc and license paths, AppArmor profiles, data folders, environment variables and the sidecar's module path, which need not match its folder until step 2 moves it there.
3. **Move the endpoints** to the `kubetail.com` hosts in *Decisions*: the updater endpoints, the sidecar's `-oauth-issuer` default and the host's `OAUTH_ISSUER`, `DASHBOARD_URL`, the security contact, and every doc, test and security-model entry that names them. Rename the `kstackcloud` connector to `kubetailcloud`.
4. **Narrow the reserved variables.** This changes a security boundary: PROC-4 (*Enforced*), which keeps a user's variable from overriding one the run's table sets, and CMD-50, which lists what a run sets. `kubetailEnv` (`run/sandbox/env.go`) and `kubetailSets` (`services/envvars/check.go`) reserve the three names a run sets, `KUBETAIL`, `KUBETAIL_SIDECAR_PID` and `KUBETAIL_DESKTOP_PID`, instead of the `KUBETAIL` prefix. `TestKubetailSetsNamesTheTablesVariables` keeps refusing each of the three, `KUBETAIL_DESKTOP_PID` added, and a new test, cited under PROC-4, lets a user's `KUBETAIL_NAMESPACE` through.
5. **Drop the `desktop/v0.0.1` cleanup.** Remove the launch-time delete of the old `auth-token` keyring entry from `kubetailcloud`, its tests, its `persisted-data.md` row and its `TODO.md` item.
6. **Collapse the migrations** of `appdb` and `kubestore` into one `0001_init.sql` each (`logstore` has one already), and clear the shipped marks in `docs/persisted-data.md`.
7. **Drop the history ADRs** that *Decisions* drops in step 1, and repoint or remove every link to them.
8. **Remove the CLI**: `cli/`, the `release-cli` workflow, its path filter, its docs and its `CHANGELOG.md` section.

## Acceptance

- `make check`, `make lint-security-model` and CI pass on the branch, with every job running.
- `git grep -i kstack` finds only the strings task 1 leaves.
- `git grep -E 'kstack\.sh'` finds nothing.
- The release dispatch builds a deb and an rpm whose package name is `kubetail-desktop` (`dpkg-deb -f <deb> Package`, `rpm -qp --qf '%{NAME}' <rpm>`), and an app bundle named `Kubetail.app`.
- By hand, on a machine with a display: a fresh `pnpm tauri dev` creates its folders and keyring entries under the Kubetail names.
