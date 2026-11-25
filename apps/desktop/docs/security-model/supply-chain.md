# Supply chain, release and updates

[← Security model](../security-model.md)

What our jobs run, what a build installs and what a release signs are each pinned, audited or kept apart. Every action is pinned to a commit, dependencies are audited and arrive after a cooldown, and a person merges every update. A release's secrets stay in our own steps and, where they can, off any runner that installs dependencies. These rows defend against someone who can move a tag, publish a hostile package version, poison a cache or steer the text an agent reads. Several are held by review alone, some risks are accepted by decision, and a release is not yet gated on the checks for its own commit. The in-app updater is built and waits on its end-to-end check.

## Protections

| ID | Protection | Status | Residual |
| --- | --- | --- | --- |
| SUPPLY-1 | Every action in `.github/`, first-party ones included, is pinned to a full commit SHA, so no one who can move a tag or branch changes what our jobs run. | **Enforced** | A `docker://` step's image is not checked. |
| SUPPLY-2 | Workflows and composite actions are audited on every pull request: no expression expanded into a `run:` block, no checkout leaving its token on disk, no workflow-wide write permission, no dangerous trigger. | **Enforced** | `release-desktop.yml`'s workflow-wide `contents: write` is ignored until R-08 lands. |
| SUPPLY-3 | Go, Rust and npm advisories are audited over the whole tree daily and on every push to `main`. | **Enforced** | Releases do not depend on these jobs (R-08). |
| SUPPLY-4 | A failed advisory audit on `main` opens an issue labelled `security`. | **Enforced** | — |
| SUPPLY-5 | A pull request that changes a Go or Rust dependency fails on a moderate-or-higher advisory it brings in, and only on those. | **Enforced** | A Go call newly reaching a vulnerable function, and a RustSec-only advisory, wait for `audit.yml`'s full audits. |
| SUPPLY-6 | A pull request that changes an npm dependency fails on a moderate-or-higher advisory it brings in, and only on those. | **Enforced** | — |
| SUPPLY-7 | Go, Rust, npm and Actions dependencies are proposed weekly, a week after release. | **Enforced** by Dependabot | — |
| SUPPLY-8 | Security fixes are proposed as soon as an alert opens. | **Enforced** by Dependabot | — |
| SUPPLY-9 | An npm package installs only once it is a week old. | **Enforced** by pnpm on every install | — |
| SUPPLY-10 | An npm package never installs from a git URL or tarball deep in the tree. | **Enforced** by pnpm on every install | — |
| SUPPLY-11 | An npm package never installs with weaker publish trust than an earlier version of it. | **Enforced** by pnpm on every install | — |
| SUPPLY-12 | Every dependency update merges by a maintainer's review. None merges automatically. | **By decision** | CI's jobs are not required checks, so nothing short of a review stops a red update. |
| SUPPLY-13 | Toolchains are bumped by hand. | **By decision** | — |
| SUPPLY-14 | Tauri's minor and major versions are bumped by hand. | **By decision** | — |
| SUPPLY-15 | Coverage reports leave for Codecov as file paths and line hit counts, never source or a secret. | **By decision** | — |
| SUPPLY-16 | No Codecov token is stored. | **By decision** | — |
| SUPPLY-17 | The GPG signing key and the token that creates the release are used by `gpg` and `gh` in our own steps, never handed to a third-party action. | **Held by review** | — |
| SUPPLY-19 | A release job restores no GitHub Actions cache, so nothing a `main` run of `ci.yml`, `coverage.yml` or `audit.yml` saves reaches a release build. | **Held by review** | zizmor's `cache-poisoning` audit does not see inside the composite action. |
| SUPPLY-20 | The SignPath token never shares a runner with installed dependencies. | **Held by review** | — |
| SUPPLY-21 | The updater's signing key never shares a runner with installed dependencies. | **Held by review** | Who can use the key is who can run a job in the `production` environment. |
| SUPPLY-22 | The Apple signing certificate and App Store Connect key sit in the job that runs `pnpm install` and `pnpm tauri build`. | **By decision** | A dependency's install or build script can read them. |
| SUPPLY-23 | A dispatched release version cannot reach a shell as source. | **Held by review** | Nothing exercises `scripts/check-release-version.sh`. |
| SUPPLY-24 | The agent writing a release's notes builds no link: the check fails prose holding a link, HTML, a mention or `#N` outside a code span. | **Enforced** for the links | — |
| SUPPLY-25 | Every link in a release's notes is built by `render`. | **Enforced** for the links | — |
| SUPPLY-26 | `publish` writes a draft's notes only on a yes read from the terminal, never standard input, so an agent's shell cannot answer it. | **Enforced** for `publish`'s write | — |
| SUPPLY-27 | Pull request text reaches the agent writing a release's notes as data. | **Held by review** for the agent | The agent runs with the releaser's shell while it reads the text, so a steered agent can still run commands in that session or write a false claim the releaser must catch. |
| SUPPLY-28 | The agent writes to GitHub only a draft's notes, only when the user asks. | **Held by review** for the agent's own write | — |
| SUPPLY-29 | The twenty-one advisories Tauri's Linux stack, `urlpattern`, the Tauri plugins' zbus 5 and `tauri-utils`' `serde_with` pin are left unresolved. | **By decision** | — |
| SUPPLY-30 | picomatch's ReDoS advisory, unpatched upstream, is left unresolved in build and test tooling. | **By decision** | — |
| SUPPLY-31 | The app tells the user a newer release exists. | **Not built** | Nothing checks, so a user can sit on a version with a known bug indefinitely. |
| SUPPLY-32 | Signed in-app updates on macOS and Windows: the app installs only an artifact signed by one of the two keys it was built with, current or standby. | **Built**, not yet checked end to end | The source's status is "Built, not yet checked end to end". Nothing publishes the manifests to `updates.kubetail.com` yet, and the manual check waits on the real key. Linux applies no update. |
| SUPPLY-33 | An in-app update's signature is bound to the version the manifest names. | **Built**, not yet checked end to end | As SUPPLY-32. |
| SUPPLY-34 | Each updater key has its own manifest on `updates.kubetail.com`, and each endpoint is held to its key's id. | **Built**, not yet checked end to end | As SUPPLY-32. |
| SUPPLY-35 | A release is gated on the checks for its own commit. | **Not built** | `release-desktop.yml` grants write permissions to every job, and audits run only per pull request. |
| SUPPLY-36 | The macOS bundle grants only the hardened-runtime exceptions it needs. | **Not built** | Three exceptions ship whose necessity has never been tested against a signed build. |

## Evidence

### SUPPLY-1

- **Where:** `.github/workflows/`, `.github/actions/`, `.github/ci-config/zizmor.yml`
- `.github/ci-config/zizmor.yml` — zizmor's `unpinned-uses` audit fails `Lint · YAML` (and `make lint-repo-yaml`) on a tag or branch reference.
- Note: in CI the audit checks each SHA and its version comment against the action's repository.
- Note: `.github/dependabot.yml` moves the pins and their version comments.
- Note: a `docker://` step's image is not checked. zizmor flags it only under `--persona pedantic`, and no step uses one.

### SUPPLY-2

- **Where:** `.github/workflows/`, `.github/actions/`, `.github/ci-config/zizmor.yml`
- `.github/ci-config/zizmor.yml` — the `Lint · YAML` job runs zizmor, which fails on any finding this file does not ignore with a reason.
- Note: `release-desktop.yml`'s workflow-wide `contents: write` is ignored until R-08 lands.

### SUPPLY-3

- **Where:** `.github/workflows/audit.yml`, `src-tauri/.cargo/audit.toml`, `pnpm-workspace.yaml`
- **Records:** [the advisories Tauri's dependency graph pins](../adr/2026-09-05-tauri-pinned-transitive-advisories.md)
- `.github/workflows/audit.yml` — the `audit / Go`, `audit / Rust` and `audit / TypeScript` jobs.
- Note: npm fails at moderate against a reasoned ignore list in `pnpm-workspace.yaml`.
- Note: Rust denies unsound and unmaintained against a reasoned ignore list.
- Note: releases do not depend on these jobs (R-08).

### SUPPLY-4

- **Where:** `.github/workflows/audit.yml`
- `.github/workflows/audit.yml` — the `Report · Failures` job.

### SUPPLY-5

- **Where:** `.github/workflows/audit.yml`
- **Records:** [by decision](../../../../docs/adr/2026-10-26-a-pull-request-fails-only-for-the-advisories-it-brings-in.md)
- `.github/workflows/audit.yml` — `Review dependencies · Go + Rust` compares the base and head dependency graphs (Go and Cargo, by version, against GitHub's advisory database).
- Note: a Go call newly reaching a vulnerable function, and a RustSec-only advisory, wait for `audit.yml`'s full audits.

### SUPPLY-6

- **Where:** `.github/workflows/audit.yml`, `scripts/audit-npm-diff.sh`
- **Records:** [by decision](../../../../docs/adr/2026-10-26-a-pull-request-fails-only-for-the-advisories-it-brings-in.md)
- `scripts/audit-npm-diff.sh` — `Review dependencies · TypeScript` compares `pnpm audit` on the base and the head, with the head's ignore list.

### SUPPLY-7

- **Where:** `.github/dependabot.yml`
- `.github/dependabot.yml` — minor and patch updates grouped per ecosystem, each major alone.

### SUPPLY-8

- **Where:** `.github/dependabot.yml`
- `.github/dependabot.yml` — security updates grouped and not delayed.
- Note: alerts and security updates read GitHub's dependency graph, which reads only `pnpm-lock.yaml`'s first YAML document (pnpm itself).
- Note: `audit.yml` submits the pnpm tree to the dependency graph on every push to `main` (`scripts/npm-dependency-snapshot.sh`).

### SUPPLY-9

- **Where:** `pnpm-workspace.yaml`
- `pnpm-workspace.yaml` — `minimumReleaseAge`, matching Dependabot's cooldown.
- Note: `minimumReleaseAge`, `blockExoticSubdeps` and `trustPolicy: no-downgrade` are each set explicitly, so a change in pnpm's defaults cannot loosen one.
- Note: a frozen install fails on a lockfile entry they reject.
- Note: `minimumReleaseAgeExclude` and `trustPolicyExclude` hold the exceptions, each with its reason.

### SUPPLY-10

- **Where:** `pnpm-workspace.yaml`
- `pnpm-workspace.yaml` — `blockExoticSubdeps`.

### SUPPLY-11

- **Where:** `pnpm-workspace.yaml`
- `pnpm-workspace.yaml` — `trustPolicy: no-downgrade`.

### SUPPLY-12

- **Where:** `.github/dependabot.yml`
- **Records:** [Dependabot moves dependencies, and a person merges every update](../../../../docs/adr/2026-11-09-dependabot-moves-dependencies-and-a-person-merges-every-update.md)
- Note: the source also places this in `main`'s ruleset.
- Note: CI's jobs are not required checks, so nothing short of a review stops a red update.

### SUPPLY-13

- **Where:** `sidecar/go.mod`, `tools/go.mod`, `.github/actions/setup-environment`, `src-tauri/rust-toolchain.toml`, `package.json`
- **Records:** [the same ADR](../../../../docs/adr/2026-11-09-dependabot-moves-dependencies-and-a-person-merges-every-update.md)
- Note: the versions are `sidecar/go.mod` and `tools/go.mod` (`go`), `.github/actions/setup-environment` (`go-version`, `node-version`, `kubectl-version`), `src-tauri/rust-toolchain.toml`, and `package.json` (`packageManager`, and `@types/node`'s major, which tracks Node's).
- Note: Dependabot moves none of these.
- Note: Dependabot moves the audit tools. `govulncheck` is a `tool` in `tools/go.mod`, and `cargo-audit` comes from the pinned `taiki-e/install-action`, whose release fixes its version and checksums.

### SUPPLY-14

- **Where:** `src-tauri/Cargo.toml`, `package.json`
- **Records:** [the same ADR](../../../../docs/adr/2026-11-09-dependabot-moves-dependencies-and-a-person-merges-every-update.md)
- Note: Tauri and its plugins are in `src-tauri/Cargo.toml`, Tauri in `package.json`.
- Note: a Tauri advisory reaches us through `audit / Rust` and `audit / TypeScript`, since the ignore holds back security updates too.

### SUPPLY-15

- **Where:** `.github/workflows/coverage.yml`, `.github/codecov.yml`
- **Records:** [Gate PRs on changed lines with Codecov's merged result](../../../../docs/adr/2026-11-09-gate-prs-on-changed-lines-with-codecovs-merged-result.md), [review](../security/2026-10-30-coverage-reports-go-to-codecov.md)
- Note: each upload names its report file with `disable_search`, so nothing else on the runner is sent.
- Note: `codecov/codecov-action` is pinned by SHA (zizmor's `unpinned-uses`) and every checkout sets `persist-credentials: false`.
- Note: Codecov posts no status or check (`.github/codecov.yml`). `coverage / Gate` reads its public API with `contents: read` alone.
- Note: Codecov's GitHub app changes no code. What it reads is public already.

### SUPPLY-16

- **Where:** `.github/workflows/coverage.yml`, `.github/codecov.yml`
- **Records:** [Gate PRs on changed lines with Codecov's merged result](../../../../docs/adr/2026-11-09-gate-prs-on-changed-lines-with-codecovs-merged-result.md), [review](../security/2026-10-30-coverage-reports-go-to-codecov.md)
- Note: an upload authenticates with a short-lived OIDC token from a job granted `id-token: write`, or tokenless from a fork.

### SUPPLY-17

- **Where:** `.github/workflows/release-desktop.yml`
- Note: the workflow's `release` job imports the key, signs `SHA256SUMS` and runs `gh release create` as plain steps.
- Note: zizmor's `superfluous-actions` audit in `Lint · YAML` fails a third-party action that duplicates what the runner already does.

### SUPPLY-19

- **Where:** `.github/workflows/release-desktop.yml`, `.github/actions/setup-environment/action.yml`
- Note: `setup-environment` caches nothing unless asked.
- Note: no step in the release workflow passes it a `node-cache`, `go-cache-key` or `rust-cache-key`.
- Note: zizmor's `cache-poisoning` audit does not see inside the composite action.

### SUPPLY-20

- **Where:** `.github/workflows/release-desktop.yml`
- Note: `build-bundle-windows` builds the unsigned MSI with no secret.
- Note: `sign-bundle-windows` submits it to SignPath, checks out nothing and installs nothing, and alone runs in the `production` environment.

### SUPPLY-21

- **Where:** `.github/workflows/release-desktop.yml`
- Note: only `sign-updater` reads `TAURI_SIGNING_PRIVATE_KEY` and, when a retired key is kept, `TAURI_SIGNING_PRIVATE_KEY_PREVIOUS`.
- Note: the standby key is offline and never a secret.
- Note: `sign-updater` runs `scripts/sign-updater.sh`, which installs nothing but the `cargo-tauri` it fetches and checks by SHA-256.
- Note: who can use the key is who can run a job in the `production` environment.

### SUPPLY-22

- **Where:** `.github/workflows/release-desktop.yml`
- **Records:** [Tauri signs the macOS bundle beside its dependencies](../adr/2026-10-28-tauri-signs-the-macos-bundle-beside-its-dependencies.md)
- Note: a dependency's install or build script can read them.
- Note: Tauri signs, notarizes and staples during the build.

### SUPPLY-23

- **Where:** `.github/workflows/release-desktop.yml`
- **Records:** [TODO](../TODO.md#security)
- Note: the version reaches every script as an environment value, which zizmor in `Lint · YAML` enforces.
- Note: the version must pass `scripts/check-release-version.sh`, which the workflow's `config` job runs: `major.minor.patch` with an optional `-rc.<n>`.
- Note: nothing exercises the script.

### SUPPLY-24

- **Where:** `tools/releasenotes`, `.claude/skills/release-notes/SKILL.md`
- **Records:** [release notes from pull request text](../security/2026-11-06-release-notes-from-pull-request-text.md), [the agent posts approved release notes](../security/2026-11-07-the-agent-posts-approved-release-notes.md), [ADR](../../../../docs/adr/2026-11-10-write-release-notes-with-a-skill-over-deterministic-tooling.md)
- `TestCheckFails` — `check` fails prose holding a link, HTML, a mention or `#N` outside a code span.

### SUPPLY-25

- **Where:** `tools/releasenotes`
- **Records:** [release notes from pull request text](../security/2026-11-06-release-notes-from-pull-request-text.md)
- `TestRender`, `TestEscape` — `render` builds every link.

### SUPPLY-26

- **Where:** `tools/releasenotes`
- **Records:** [the agent posts approved release notes](../security/2026-11-07-the-agent-posts-approved-release-notes.md), [ADR](../../../../docs/adr/2026-11-10-write-release-notes-with-a-skill-over-deterministic-tooling.md)
- `TestPublishWritesNothingOtherwise` — `publish` reads its yes from the terminal, never standard input, so an agent's shell cannot answer it.

### SUPPLY-27

- **Where:** `tools/releasenotes`, `.claude/skills/release-notes/SKILL.md`
- **Records:** [release notes from pull request text](../security/2026-11-06-release-notes-from-pull-request-text.md)
- Note: only `collect` reads PR text, under a field marked `untrusted`.
- Note: the agent runs with the releaser's shell while it reads the text, so a steered agent can still run commands in that session or write a false claim the releaser must catch.

### SUPPLY-28

- **Where:** `tools/releasenotes`, `.claude/skills/release-notes/SKILL.md`
- **Records:** [the agent posts approved release notes](../security/2026-11-07-the-agent-posts-approved-release-notes.md), [ADR](../../../../docs/adr/2026-11-10-write-release-notes-with-a-skill-over-deterministic-tooling.md)
- Note: when the user asks in a later message, the skill lets the agent run `gh release view` to confirm the release is a draft and `gh release edit --notes-file` with the rendered notes, and nothing else.
- Note: it never publishes the draft.

### SUPPLY-29

- **Where:** `src-tauri/.cargo/audit.toml`
- **Records:** [the advisories Tauri's dependency graph pins](../adr/2026-09-05-tauri-pinned-transitive-advisories.md)
- Note: `glib`'s unsoundness is unreachable and the patched line belongs to gtk4.
- Note: none of the twenty unmaintained notices has an upgrade we can take.
- Note: chrono is locked but never compiled.
- Note: each is ignored with a reason, and the deny flags of SUPPLY-3 fail a new one.

### SUPPLY-30

- **Where:** `pnpm-workspace.yaml`
- **Records:** [accept picomatch's ReDoS advisory in dev tooling](../adr/2026-11-18-accept-picomatchs-redos-advisory-in-dev-tooling.md)
- Note: nothing in the production tree depends on picomatch, and the tools compile only globs we write.
- Note: it is ignored by advisory ID, so a new picomatch advisory fails the audit.

### SUPPLY-31

- **Records:** [TODO](../TODO.md#security)
- Note: no spec yet.
- Note: nothing checks, so a user can sit on a version with a known bug indefinitely.

### SUPPLY-32

- **Where:** `src-tauri/src/updater/`, `src-tauri/tauri.conf.json`, `scripts/sign-updater.sh`
- **Records:** [the updater trusts a current and a standby key](../adr/2026-11-08-the-updater-trusts-a-current-and-a-standby-key.md), [record](../security/2026-11-08-in-app-updater.md), [the updater key](../updater-key.md), [TODO](../TODO.md#security)
- `the_updater_pins_its_keys_manifests_and_signed_version` — the updater pins its keys.
- Note: the source gives this row the status **Built, not yet checked end to end**.
- Note: the configuration is `plugins.updater` in `src-tauri/tauri.conf.json`.
- Note: the plugin verifies before it writes.
- Note: a rotation promotes the standby, so a lost or stolen current key strands no app.
- Note: the release fails on a current key that is not the committed one or a standby that is unset.
- Note: Linux applies no update.
- Note: nothing publishes the manifests to `updates.kubetail.com` yet, and the manual check (an update installs, a different key is refused) waits on the real key.

### SUPPLY-33

- **Where:** `src-tauri/src/updater/`, `src-tauri/tauri.conf.json`
- **Records:** [the updater trusts a current and a standby key](../adr/2026-11-08-the-updater-trusts-a-current-and-a-standby-key.md), [record](../security/2026-11-08-in-app-updater.md), [TODO](../TODO.md#security)
- `the_updater_pins_its_keys_manifests_and_signed_version` — the updater pins its signed version.
- Note: `requireSignedVersion` binds each signature to its version.
- Note: the source's status is **Built, not yet checked end to end**, as SUPPLY-32.

### SUPPLY-34

- **Where:** `src-tauri/src/updater/`, `src-tauri/tauri.conf.json`
- **Records:** [the updater trusts a current and a standby key](../adr/2026-11-08-the-updater-trusts-a-current-and-a-standby-key.md), [record](../security/2026-11-08-in-app-updater.md), [TODO](../TODO.md#security)
- `the_updater_pins_its_keys_manifests_and_signed_version` — holds each endpoint to its key's id.
- Note: each key has its own manifest on `updates.kubetail.com`.
- Note: the source's status is **Built, not yet checked end to end**, as SUPPLY-32.

### SUPPLY-35

- **Records:** [TODO](../TODO.md#security)
- Note: no spec yet.
- Note: `release-desktop.yml` grants write permissions to every job, and audits run only per pull request.

### SUPPLY-36

- **Where:** `src-tauri/entitlements.plist`
- **Records:** [TODO](../TODO.md#security)
- Note: no spec yet.
- Note: three exceptions ship whose necessity has never been tested against a signed build.
