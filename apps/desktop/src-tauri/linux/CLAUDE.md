# src-tauri/linux — what the Linux packages carry

Kubetail's own bwrap and pasta, the AppArmor profiles that confine them, the `.deb`'s maintainer scripts, and the source archive the release ships for them. Also owns `scripts/build-bwrap.sh`, `scripts/build-pasta.sh`, `scripts/build-embedded-sources.sh`, `scripts/check-sandbox-chain.sh` and `licenses/`. The sandbox that runs these programs is in [`sidecar/CLAUDE.md`](../../sidecar/CLAUDE.md).

## The programs

- **bwrap**, at `/usr/lib/kubetail-desktop/bwrap`, is the sidecar's sandbox. `make build-desktop-bwrap` builds a pinned, SHA-256-checked bubblewrap into this folder (git-ignored). The release runs it on the bundle job's own runner before `tauri build`, so bwrap links the app's glibc; it links `libcap`, which the packages depend on. **bwrap is never setuid.** → [ADR: Kubetail ships its own bwrap](../../docs/adr/2026-09-28-kubetail-ships-its-own-bwrap.md).
- **pasta**, at `/usr/lib/kubetail-desktop/pasta`, gives a sandboxed run the internet. `make build-desktop-pasta` builds a pinned, SHA-256-checked passt snapshot into this folder as `pasta` (passt runs as pasta by its name), with no AVX2 build; the release builds it beside bwrap. → [ADR: Kubetail ships its own pasta](../../docs/adr/2026-10-04-kubetail-ships-its-own-pasta.md).
- **The packages are `kubetail-desktop`.** Tauri names the deb's `Package:`, the rpm's `Name:`, the desktop entry and `/usr/lib/<name>/` after `productName`, so `tauri.linux.conf.json` sets its own; `kubetail-desktop.desktop` keeps the entry's `Name=Kubetail`. `the_bundle_takes_the_kubetail_desktop_names` pins them.

## The AppArmor profiles

The `.deb` carries one profile per program. `postinst.sh` loads them, `kubetail-desktop-bwrap` first; `prerm.sh` removes them, `kubetail-desktop-pasta` first. Neither script ever fails the install.

- **`kubetail-desktop-bwrap`** is AppArmor's `bwrap-userns-restrict`, renamed `kubetail_desktop_bwrap`/`kubetail_desktop_unpriv_bwrap` and attached to `/usr/lib/kubetail-desktop/bwrap`.
- **`kubetail-desktop-pasta`** is passt's own `usr.bin.pasta` with its abstractions written out, renamed `kubetail_desktop_pasta`. It starts Kubetail's bwrap under `kubetail_desktop_bwrap` (`Px`, so **the bwrap profile must be loaded first**) or the system's (`PUx`).
- **`postinst.sh` loads a profile only where `apparmor_parser`, a running AppArmor and abi 4.0 (`/etc/apparmor.d/abi/4.0`) are present.** A profile the parser cannot read fails `apparmor.service` on every boot.
- **`scripts/check-sandbox-chain.sh` checks the profiles on a stock Ubuntu 24.04**, with `kernel.apparmor_restrict_unprivileged_userns` at 1: it runs the bundled bwrap and pasta through `sandbox-init`/`sandbox-shell`/`sandbox-pasta` and reaches `https://1.1.1.1/`. The release runs it after installing the `.deb` (`check-bundle-linux`, gating the release); a pull request touching the Go paths runs it as `sandbox-apparmor` in `ci.yml`.

## Sources and licenses

bubblewrap is LGPL-2.0-or-later and passt GPL-2.0-or-later, so the release carries their source.

- **The release's one source asset is `kubetail-desktop-<version>-embedded-sources.tar.gz`**, built by `make build-desktop-embedded-sources` from the tarballs the two builds leave here, with the scripts that built them and a README naming each program's version, upstream and license. The archive is reproducible: each Linux bundle job builds it, and the release job fails unless both architectures' archives are byte-equal.
- **Licenses and notices live in `licenses/`**, committed: each project's license text and `THIRD_PARTY_NOTICES.md`, which names every bundled program's version, copyright lines and source. `tauri.linux.conf.json` installs them in each package.
- **Bumping a pin means updating `licenses/`** (`licenses/README.md`): `make build-desktop-bwrap` and `make build-desktop-pasta` fail when the notices do not name the pinned version or a license differs from the pinned tarball's.
