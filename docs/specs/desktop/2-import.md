# Step 2 — Import into `apps/desktop/`

Prerequisite: step 1 merged into Kstack's `main`. One commit into `wip/desktop`, then the fixes that make it build here. [The sequence's README](README.md) holds the decisions and the layout.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. Import | Done | #1570 |
| 2. Settle the ADRs | Done | — |
| 3. Align toolchains | Done | — |
| 4. Merge `tools/` | Done | — |
| 5. Merge `scripts/sandbox-dev-setup.sh` | Done | — |
| 6. Place the scripts | Done | — |
| 7. Add the Makefile area | Done | — |
| 8. Fold the guides | Done | — |
| 9. Merge the root files | Done | — |

1. **Import.** Copy the tree of step 1's merge commit into the layout, with the message *Decisions* asks for, and delete `1-rename.md` with its row in the README.
2. **Settle the ADRs.** Kstack's ADRs stay in `apps/desktop/docs/adr/`. Drop those *Decisions* drops in step 2, repointing each link to this repo's ADR for the same decision. Move the two repo-wide ones to `docs/adr/` with scope `repo`, add them to its index, and repoint their links. `apps/desktop/docs/adr/README.md` says it holds the desktop app's decisions, and `docs/adr/README.md` links it.
3. **Align toolchains** with this repo's: Rust `1.98.1` → `1.99.1` (`src-tauri/rust-toolchain.toml` keeps its own file, at the same version as `cluster/agent/`'s), pnpm `12.4.1` → `12.17.0`, Go and Node from `.github/actions/setup-environment/action.yml`. Extend `make lint-repo-toolchains` to check `src-tauri/rust-toolchain.toml` and `apps/desktop/package.json`'s `packageManager`.
4. **Merge `tools/`.** `designsync` and `securitymodel` come over; `releasenotes`, `coverdiff`, `coverignore` and `coveragegate` keep one copy.
5. **Merge `scripts/sandbox-dev-setup.sh`.** `apps/desktop/node_modules`, `src-tauri/target` and the desktop `dist` move under `/home/agent/kubetail-build` and `.sandbox-linux/` as the web app's do.
6. **Place the scripts.** Those only the desktop app uses (`build-sidecar.go`, `build-bwrap.sh`, `build-pasta.sh`, `build-embedded-sources.sh`, `sign-updater.sh`, `check-release-version.sh`, `check-sandbox-chain.sh`, `check-sidecar-deps.sh`, `expose-dev.sh`) stay in `apps/desktop/scripts/`; the rest merge into `scripts/`.
   - `restore-mtime.go` moves to `scripts/`.
   - The root's `check-go-fix.sh`, `staticcheck.sh` (over `scripts/go-builds.sh`), `check-coverage-markers.sh`, `check-license-headers.sh`, `changed.sh` and `npm-dependency-snapshot.sh` take the desktop's cases; its copies of them, and of `audit-npm-diff.sh`, `check-claude-md.sh`, `check-markdown-links.sh` and `writing-standards-hook.sh`, go.
   - `protoc.sh` goes: `scripts/generate.sh` runs the sidecar's `go generate` with the repo's protoc and plugins. `shellcheck-path.sh` goes: `make lint-repo-sh` lints every script. `check-changed.sh`, `test-changed.sh` and `vet-go-changed.sh` go: the root's checks are per area.
7. **Add the Makefile area.** The root targets `generate-desktop`, `build-desktop`, `lint-desktop`, `vet-desktop`, `test-desktop` and `cover-desktop-changed`, and the desktop's helpers as `<verb>-desktop-<what>`. `apps/desktop/Makefile` goes.
   - `lint-desktop` runs Kstack's lints too: oxlint, Tailwind, the design manifest and the security model.
   - `generate` runs `generate-desktop`, so `check-generated` covers the sidecar's committed `graph/generated.go` and `*.pb.go`.
   - `scripts/check-license-headers.sh` checks `apps/desktop` by default.
   - `scripts/cover-changed.sh` and its bats suite take a `desktop` case.
8. **Fold the guides.** Each fact goes in one guide, the narrowest that covers it, and the root `CLAUDE.md` changes in the same commit as the desktop guide.
   - **The root `CLAUDE.md`** states what this step makes true:
     - `apps/desktop/` in the component list, pointing to its guide, without the line that it holds a README;
     - in *How a request travels*, that the Kubetail Desktop app is not the `desktop` environment, which is `kubetail serve` and `kubetail logs`;
     - in *Go modules*, the sidecar as a module outside `go.work`, as `tools/` is, that builds alone and is outside the fsnotify pin;
     - in *Dependencies*, the desktop's `rust-toolchain.toml` beside `cluster/agent/`'s, at the same version, which `make lint-repo-toolchains` checks;
     - in *Checks*, the `desktop` area and its row in the *Also run* table;
     - that the desktop's ADRs are in `apps/desktop/docs/adr/`.
   - **`apps/desktop/CLAUDE.md`** takes what the Kstack root guide held about the app, and drops what the root `CLAUDE.md` states: the writing standards, the license header, the testing conventions, the rules for guides, ADRs, commits, pull requests, issues and specs, and the sandbox setup. Where the two differ, the root's rule holds. What stays is what the desktop adds.
   - **The area guides** keep their places under `apps/desktop/` and drop what the root guide or `apps/desktop/CLAUDE.md` states.
   - Each guide gets an `AGENTS.md` and a byte budget. The root guide's budget rises only by what it gains, and the commit body says so.
9. **Merge the root files** in the layout's table, and replace `apps/desktop/README.md`.

## Acceptance

- From a clean checkout, `make generate-desktop build-desktop lint-desktop vet-desktop test-desktop` passes.
- `make check-generated` passes.
- `make lint-repo-toolchains lint-repo-md lint-repo-claude-md` passes.
- The root `CLAUDE.md` is true of the desktop as this step leaves it, and no rule in `apps/desktop/CLAUDE.md` restates one in the root guide.
- No link in the repo points at a dropped ADR.
- By hand, on a machine with a display: `pnpm tauri dev` in `apps/desktop/` opens the app.
