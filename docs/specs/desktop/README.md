# Kubetail Desktop

Bring the Kstack desktop app into this repo as Kubetail Desktop, at `apps/desktop/`, and rename it throughout: `kstack` → `kubetail`, `Kstack` → `Kubetail`, `Kstack Desktop` → `Kubetail Desktop`. Kstack's only releases are `desktop/v0.0.1` and its release candidates, and nothing installed needs a migration path.

Answers #TBD. The work is built on `wip/desktop` and lands on `main` in one PR.

| Step | Spec | Built in | Status | PR |
| --- | --- | --- | --- | --- |
| 2 | [Import into `apps/desktop/`](2-import.md) | `wip/desktop` | In progress | #1572 |
| 3 | [Harness](3-harness.md) | `wip/desktop` | In progress | — |

Step 1, the rename, was built in `kstackhq/kstack` and landed on `wip/desktop` with step 2's import commit (#1570). Each step's spec tracks its tasks' status and PRs.

## Decisions

- **One import commit, no history.** Step 1 merges into Kstack's `main`, which is frozen from then on: Kstack's open PRs are closed, to be reopened here once step 2 lands, and its Dependabot is turned off. The import commit copies that merge commit's tree into `apps/desktop/`, and its message names the source, `Imported from kstackhq/kstack@<sha>`. `kstackhq/kstack` is archived once this lands, not deleted.
- **Every license header is `Copyright 2024 The Kubetail Authors`**, the header `scripts/check-license-headers.sh` expects. `LICENSE` stays Apache 2.0. The import commit's message states the change of copyright line, since Kstack has contributors beyond its maintainer.
- **Contracts change outright.** The Tauri identifier, keyring service names, data and cache folders, environment variables, error codes, endpoints, updater key, release file names, package names, binary names and AppArmor profiles take the Kubetail names below. No code reads the Kstack ones, and step 1 removes the code that cleans up after `desktop/v0.0.1`.
- **The names.**

  | What | Name |
  | --- | --- |
  | Brand, in docs and on the website | Kubetail Desktop |
  | `productName`, which names the installed app | `Kubetail`: `Kubetail.app`, `PFiles/Kubetail/` |
  | Tauri identifier | `com.kubetail.desktop` |
  | Binaries | `kubetail-desktop`, `kubetail-sidecar` |
  | Release files | `kubetail-desktop-<version>-<os>-<arch>` |
  | deb and rpm packages | `kubetail-desktop`, apart from the CLI's `kubetail` package |
  | Linux desktop entry | `kubetail-desktop.desktop`, showing `Name=Kubetail` |
  | Linux helpers and AppArmor profiles | `/usr/lib/kubetail-desktop/`, `kubetail-desktop-bwrap`, `kubetail-desktop-pasta` |
  | Linux docs and licenses | `/usr/share/doc/kubetail-desktop/`, `/usr/share/licenses/kubetail-desktop/`, named for the package; `kubetail/` is the CLI's |
  | Data and cache folders | `Kubetail` on macOS and Windows (`Application Support/Kubetail`), `kubetail` on Linux, following each platform's directory kinds; nothing shared with the CLI's `~/.kubetail` |
  | Environment variables and GraphQL error codes | `KUBETAIL_*`, one namespace with the other components: `KSTACK_SIDECAR_PID` → `KUBETAIL_SIDECAR_PID`, `KSTACK=1` → `KUBETAIL=1`, except `KSTACK_HOST_PID` → `KUBETAIL_DESKTOP_PID`. A name the other components already use keeps their meaning: the six error codes both repos hold (`KUBETAIL_FORBIDDEN` and the rest) mean the same, and the sandbox setup's `KSTACK_SANDBOX_*` merge into this repo's `KUBETAIL_SANDBOX_*` with `scripts/sandbox-dev-setup.sh` |
  | The webview's settings global | `window.__KUBETAIL_HOST__` |

- **The sidecar reserves only the variables it sets.** Kstack's sidecar reserves the whole `KSTACK` prefix: a sandboxed run's table claims it (`kstackEnv` in `run/sandbox/env.go`), and the user's variables may not name it (`kstackSets` in `services/envvars/check.go`). Renamed, that prefix is `KUBETAIL`, which the CLI's own variables use, so each list names the three variables a run sets instead: `KUBETAIL`, `KUBETAIL_SIDECAR_PID` and `KUBETAIL_DESKTOP_PID`.
- **Linux takes its own `productName`, `kubetail-desktop`.** Tauri names the deb's `Package:` and the rpm's `Name:` after `productName` and has no setting for either, so `tauri.linux.conf.json` sets `kubetail-desktop`, which also names the desktop entry and `/usr/lib/kubetail-desktop/`. A `desktopTemplate` for the deb and the rpm keeps the entry's `Name=Kubetail`. Kstack's packages were named `kstack`; only their release files were `kstack-desktop-…`.
- **Every endpoint moves to `kubetail.com`.** Each host keeps its paths and the status the security model gives it: `updates.kubetail.com` is built and not yet published to, as `updates.kstack.sh` is.

  | Kstack | Kubetail |
  | --- | --- |
  | `updates.kstack.sh` (per-key manifests, `/desktop/stable/latest-<id>.json`) | `updates.kubetail.com` |
  | `oauth.kstack.sh` (the OAuth issuer) | `oauth.kubetail.com` |
  | `app.kstack.sh` (the account dashboard) | `app.kubetail.com` |
  | `hello@kstack.sh` | `hello@kubetail.com` |
  | the `kstackcloud` connector, *Kstack Cloud* | `kubetailcloud`, *Kubetail Cloud* |
  | `*.kstack.invalid` | `*.kubetail.invalid` |

- **The SQL migrations reset.** Each schema's migrations collapse into one `0001_init.sql`, and `persisted-data.md` clears its shipped marks. The rule that a shipped migration is never edited applies again from the first Kubetail Desktop release.
- **ADRs carry over, except history and duplicates, and stay with the app.** The desktop's ADRs are its own decisions, so they stay in `apps/desktop/docs/adr/`, a sequence and index apart from the repo's `docs/adr/`, with their dates and `kstack` renamed in their file names and text. Only a decision that holds for the whole repo moves to `docs/adr/`. The import commit's message states this one-time rewrite of accepted ADRs.
  - **Dropped in step 1:** the 14 superseded ones, `the-desktop-app-is-kstack-and-its-binary-kstack-desktop` and `kstack-app-installs-the-newest-desktop-release-with-the-platforms-installer`, which exist only for the names and the CLI being left behind.
  - **Dropped in step 2**, where this repo's ADR holds the same decision and its links can be repointed: `record-architecture-decisions`, `a-spec-never-reaches-main`, `dependabot-moves-dependencies-and-a-person-merges-every-update`, `codecov-gates-the-changed-lines`, `the-coverage-gate-runs-in-ci-on-codecovs-merged-result`, `the-coverage-gate-diffs-the-commits-itself`, `coverage-runs-on-its-own-triggers-from-shared-path-filters`, `a-wip-branchs-tip-gets-a-coverage-report`, `share-one-pnpm-store-across-sandbox-worktrees`, `the-releaser-writes-the-notes-with-a-skill-over-deterministic-tooling`, `the-agent-posts-the-notes-when-the-user-asks`, `a-pull-request-runs-one-leg-per-os`, `name-workflow-jobs-verb-first`, and Kstack's `lint-tailwind-classes-in-a-run-of-their-own`.
  - **Moved to `docs/adr/` with scope `repo`**, decisions that hold for the whole repo and have no ADR of their own here: `a-pull-request-fails-only-for-the-advisories-it-brings-in` and `agent-guides-sit-beside-the-components-they-describe`.
  - **Every other ADR stays in `apps/desktop/docs/adr/`** with the scope it has in Kstack. There, `repo` means the desktop's own tooling and jobs: its macOS signing, its Linux formats, its pre-release versions, its release file names, its platform splits, its release-profile tests, its picomatch advisory, `designsync` and the security model.
- **The kstack CLI is not migrated.** The `kubetail` CLI takes over its role later.
- **The webview moves as it is.** Sharing code with `apps/web` is later work; `apps/desktop/README.md` stops promising it.
- **Releases are tagged `desktop/v<semver>`**, beside the other components' tags, and never become the repo's latest release, which the README's download link and the CLI's packages rely on. The Kstack tags are not carried over, and the first Kubetail Desktop release is `desktop/v0.1.0`.

## Layout

```
apps/desktop/
  src/  public/  index.html  package.json  pnpm-*.yaml  vite.config.ts  …   the webview, with its configs and .env.development
  src-tauri/                                                              the Tauri host (its own Cargo workspace)
  sidecar/                                                                the Go sidecar (its own module)
  proto/                                                                  the host↔sidecar contract
  design/  lint/  licenses/  src-icons/  scripts/  .cargo/                as in Kstack
  docs/                                                                   adr/ (the desktop's ADRs), persisted-data.md,
                                                                          data-access.md, security-model.md and
                                                                          security-model/, security/, notes/,
                                                                          development-switches.md, updater-key.md, TODO.md
docs/adr/                                                                 the two repo-wide ADRs from Kstack
```

- **The sidecar's module path is `github.com/kubetail-org/kubetail/apps/desktop/sidecar`.** It stays outside the root `go.work`, as `tools/` does: it imports no sibling, and a workspace would force one fsnotify version on every module, while the sidecar uses v1.10.1 and the others pin v1.8.0 ([ADR](../../adr/2026-11-09-pin-fsnotify-to-v1-8-0.md)).
- **The desktop docs live under `apps/desktop/docs/`**, so `docs/security-model.md` stays free for the repo-wide model in #1218.
- **The desktop's ADRs live in `apps/desktop/docs/adr/`**, with their own `README.md` index and `TEMPLATE.md`. A new decision about the app goes there; one that holds for the whole repo goes in `docs/adr/`. `docs/adr/` gains no `desktop` scope.
- **What has no place above** is merged into this repo's file of the same job or dropped:

  | Kstack | Here |
  | --- | --- |
  | `SECURITY.md`, `CONTRIBUTING.md` | merged into the root ones |
  | `CHANGELOG.md` | a *Desktop* section in the root one |
  | `.claude/settings.json`, `.gitignore`, `.gitattributes`, `.yamllint.yml`, `.yamllintignore` | merged into the root ones, paths prefixed with `apps/desktop/` |
  | `.claude/skills/`, `tools/`, `.github/` | merged in step 2 or 3 |
  | `docs/specs/`, `.github/ISSUE_TEMPLATE/`, `.github/FUNDING.yml`, `.vscode/` | dropped; this repo's apply |

## Outside the steps

Operations work a maintainer owns, named in the `wip/desktop` PR. None of it blocks landing; all of it precedes `desktop/v0.1.0`.

- **DNS and hosting** for `updates.kubetail.com`, `oauth.kubetail.com` and `app.kubetail.com`.
- **The OAuth issuer** at `oauth.kubetail.com`, with the desktop's client registered, and the live checks `TODO.md` lists for it, which Kstack never ran against `oauth.kstack.sh`.
- **Publishing the updater manifests** to `updates.kubetail.com`, as `updater-key.md` describes; Kstack never built it.

## Open questions

None.

## When it lands

- **The guides**: steps 2 and 3 keep the root `CLAUDE.md` and `apps/desktop/CLAUDE.md` current as they go. Check once more that nothing in the root guide is false for the desktop, and that `apps/desktop/CLAUDE.md` restates nothing the root guide holds.
- **`SECURITY.md`**: the `gh attestation verify` command for a desktop release, naming `release-desktop.yml`.
- **ADRs**: the two repo-wide ones in `docs/adr/`, the rest in `apps/desktop/docs/adr/`; in `apps/desktop/docs/adr/`, a new ADR for importing Kstack without its history and resetting its migrations, which records the names above and amends the carried-over `release-files-are-named-kubetail-product-version-os-arch`, whose Linux packages kept `productName`'s name; in `docs/adr/`, a new ADR that amends the fsnotify ADR to leave the sidecar outside the pin, and the platform matrix ADR adds the desktop app. `docs/adr/README.md` says the desktop's ADRs are in `apps/desktop/docs/adr/`.
- **The Helm chart** needs nothing.
- Delete `docs/specs/desktop/`.
