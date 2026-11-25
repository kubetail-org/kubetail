# Step 3 — Harness

Prerequisite: step 2 on `wip/desktop`. [The sequence's README](README.md) holds the decisions.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. CI | In review | — |
| 2. Filters and coverage | In review | — |
| 3. Fork gates and secret names | In review | — |
| 4. Keep desktop releases from the CLI's publishing | In review | — |
| 5. Release | In review | — |
| 6. Signing | Planned (maintainer) | — |
| 7. Dependabot | In review | — |
| 8. Audit, CodeQL, stress | In review | — |
| 9. Skills | In review | — |
| 10. Update the guides | In review | — |

1. **CI.** Add `ci-desktop.yml` and `coverage-desktop.yml`, called from `ci.yml` and `coverage.yml`, built from Kstack's `ci.yml` and `coverage.yml`. Bring `.github/actions/stub-sidecar`. `.github/ci-config/zizmor.yml`'s desktop entries (`release-desktop.yml`, `action.yml:228`) follow the files they name, and go with `apps/desktop/.github/`.
2. **Filters and coverage.**
   - `desktop` keys and `release-desktop` in `.github/ci-config/path-filters.yml`.
   - The desktop runners in `runners.json`.
   - A `desktop` component and flags in `.github/codecov.yml`, with the Codecov path fix for this repo.
   - `.github/codecov.yml`'s `ignore` takes the desktop's paths that `scripts/cover-changed.sh`'s `ignored` lists (step 2).
3. **Fork gates and secret names.** Rewrite what step 1 left. Each gate becomes `github.repository == 'kubetail-org/kubetail'`, as in this repo's workflows. Each secret and variable takes the name `release-cli.yml` already uses for the same credential:

   | Kstack | Here |
   | --- | --- |
   | `KSTACK_BOT_*` | `KUBETAIL_BOT_*` |
   | `APPLE_CERTIFICATE`, `APPLE_CERTIFICATE_PASSWORD` | `APPLE_DEVELOPER_CERT_P12_B64`, `APPLE_DEVELOPER_CERT_P12_PASSWORD` |
   | `APPLE_API_KEY_CONTENT` | `APPLE_STORE_CONNECT_KEY_P8` |
   | `vars.APPLE_SIGNING_IDENTITY`, `vars.APPLE_API_KEY`, `vars.APPLE_API_ISSUER` | `vars.APPLE_DEVELOPER_CERT_IDENTITY`, `vars.APPLE_STORE_CONNECT_KEY_ID`, `vars.APPLE_STORE_CONNECT_ISSUER_ID` |
   | `SIGNPATH_API_TOKEN`, `vars.SIGNPATH_ORGANIZATION_ID` | unchanged |

4. **Keep desktop releases from the CLI's publishing.** Every `publish-*.yml` runs on `release: released`, reading its version from a `cli/v` tag, and on `workflow_dispatch`, with `version` and `dry_run` inputs. Gate each one's jobs on `github.event_name == 'workflow_dispatch' || startsWith(github.event.release.tag_name, 'cli/v')`: a dispatch carries no release, so a gate on the tag alone would skip it.
5. **Release.** Bring `release-desktop.yml`, building from `apps/desktop/` and tagging `desktop/v<semver>`, with its attest step in the publishing job. It creates the draft with `--latest=false`, and the `release-desktop` skill tells the releaser to leave *Set as the latest release* unticked when publishing it.
6. **Signing.** A maintainer sets up what this repo lacks, in its `production` environment:
   - a new updater key pair, current and standby, as `docs/updater-key.md` describes, in `TAURI_SIGNING_PRIVATE_KEY*`, with its public keys and ids committed to `tauri.conf.json`;
   - SignPath's artifact configuration for the desktop installers, at the renamed paths (`PFiles/Kubetail/kubetail-desktop.exe` and the NSIS equivalent).
7. **Dependabot.** Add `apps/desktop` (npm), `apps/desktop/src-tauri` (cargo) and `apps/desktop/sidecar` (gomod) to `.github/dependabot.yml`, with Kstack's groups and cooldowns.
8. **Audit, CodeQL, stress.** Merge Kstack's `audit.yml`, `codeql.yml` and `stress.yml` settings for the webview, the host and the sidecar into this repo's, with `.cargo/audit.toml`'s ignores.
9. **Skills.** Extend the `release-notes` and `release-version` skills to `desktop`, and add a `release-desktop` skill.
10. **Update the guides.** In one commit:
   - **The root `CLAUDE.md`** states what this step makes true: `ci-desktop.yml` and `coverage-desktop.yml` in the CI paragraph, with the `desktop` coverage area; `desktop/v<semver>` among the release tags, and that only `cli/v*` releases run the `publish-*.yml` workflows; `release-desktop.yml` in the attestation bullet; `release-desktop` among the release skills.
   - **`apps/desktop/CLAUDE.md`** drops its CI paragraphs that the root guide now states: the fork gates, job names, the coverage gate and the PR legs. It keeps what only the desktop's jobs do, such as building the release profile on `main`.

## Acceptance

- CI and coverage pass on the PR from `wip/desktop` into `main`, with the desktop jobs running.
- `make lint-repo-yaml` passes.
- The root `CLAUDE.md` is true of the desktop's CI and releases, and no rule in `apps/desktop/CLAUDE.md` restates one in the root guide.
- A dispatch of `release-desktop.yml` on `wip/desktop` builds, signs and notarizes every platform without publishing.
- Every job of every `publish-*.yml` carries the gate.
- A dry-run dispatch of `publish-aur.yml` on `wip/desktop` runs its jobs.
- By hand, when `desktop/v0.1.0` is published (a release candidate is a pre-release and fires no `released` event): no `publish-*.yml` run gets past its gate, and the CLI's release stays the latest.
