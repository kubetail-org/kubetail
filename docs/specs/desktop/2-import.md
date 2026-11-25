# Step 2 — Import into `apps/desktop/`

Prerequisite: step 1 merged into Kstack's `main`. One commit into `wip/desktop`, then the fixes that make it build here. [The sequence's README](README.md) holds the decisions and the layout.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. Import | Planned | — |
| 2. Settle the ADRs | Planned | — |
| 3. Align toolchains | Planned | — |
| 4. Merge `tools/` | Planned | — |
| 5. Merge `scripts/sandbox-dev-setup.sh` | Planned | — |
| 6. Place the scripts | Planned | — |
| 7. Add the Makefile area | Planned | — |
| 8. Fold the guides | Planned | — |
| 9. Merge the root files | Planned | — |

1. **Import.** Copy the tree of step 1's merge commit into the layout, with the message *Decisions* asks for, and delete `1-rename.md` with its row in the README.
2. **Settle the ADRs.** Move Kstack's ADRs to `docs/adr/`. Drop those *Decisions* drops in step 2, repointing each link to this repo's ADR for the same decision. Set each kept ADR's scope as *Decisions* says, add scope `desktop`, and add them to the index in `docs/adr/README.md`.
3. **Align toolchains** with this repo's: Rust `1.98.1` → `1.99.1` (`src-tauri/rust-toolchain.toml` keeps its own file, at the same version as `cluster/agent/`'s), pnpm `12.4.1` → `12.17.0`, Go and Node from `.github/actions/setup-environment/action.yml`. Extend `make lint-repo-toolchains` to check `src-tauri/rust-toolchain.toml` and `apps/desktop/package.json`'s `packageManager`.
4. **Merge `tools/`.** `designsync` and `securitymodel` come over; `releasenotes`, `coverdiff`, `coverignore` and `coveragegate` keep one copy.
5. **Merge `scripts/sandbox-dev-setup.sh`.** `apps/desktop/node_modules`, `src-tauri/target` and the desktop `dist` move under `/home/agent/kubetail-build` and `.sandbox-linux/` as the web app's do.
6. **Place the scripts.** Those only the desktop app uses (`build-sidecar.go`, `build-bwrap.sh`, `build-pasta.sh`, `build-embedded-sources.sh`, `sign-updater.sh`) go to `apps/desktop/scripts/`; the rest merge into `scripts/`.
7. **Add the Makefile area.** The root targets `generate-desktop`, `build-desktop`, `lint-desktop`, `vet-desktop`, `test-desktop` and `cover-desktop-changed`.
   - `lint-desktop` runs Kstack's lints too: oxlint, Tailwind, the design manifest and the security model.
   - `generate` runs `generate-desktop`, so `check-generated` covers the sidecar's committed `graph/generated.go` and `*.pb.go`.
   - `scripts/check-license-headers.sh` checks `apps/desktop` by default.
8. **Fold the guides.** `apps/desktop/CLAUDE.md` takes what the Kstack root guide held about the app; the area guides keep their places under `apps/desktop/`; each gets an `AGENTS.md` and a byte budget.
9. **Merge the root files** in the layout's table, and replace `apps/desktop/README.md`.

## Acceptance

- From a clean checkout, `make generate-desktop build-desktop lint-desktop vet-desktop test-desktop` passes.
- `make check-generated` passes.
- `make lint-repo-toolchains lint-repo-md lint-repo-claude-md` passes.
- No link in the repo points at a dropped ADR.
- By hand, on a machine with a display: `pnpm tauri dev` in `apps/desktop/` opens the app.
