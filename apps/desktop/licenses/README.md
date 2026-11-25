# Third-party licenses

The licenses and notices of the third-party programs Kubetail's Linux packages carry. Kubetail's own
license is [`LICENSE`](../LICENSE) at the repository root.

- [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md) names each program, its version, license,
  copyright notices and where its source is.
- `<project>-LICENSE*` is each project's license text, copied unchanged from its source.

`src-tauri/tauri.linux.conf.json` installs every file here but this one: under
`/usr/share/doc/kubetail-desktop/` in the `.deb`, `/usr/share/licenses/kubetail-desktop/` in the `.rpm`.

## Updating a bundled program

`scripts/build-bwrap.sh` and `scripts/build-pasta.sh` pin bubblewrap's and passt's versions. They
fail when `THIRD_PARTY_NOTICES.md` does not name the pinned version, or when a license here differs
from the one in the pinned tarball. After bumping `VERSION` and `SHA256` in a script:

1. Run `make build-bwrap` or `make build-pasta` on Linux.
2. If it reports a changed license, copy the file it names over the one here.
3. In `THIRD_PARTY_NOTICES.md`, update the version, the tarball's URL and SHA-256, and the
   copyright lines (`grep -rhi copyright` over the tarball's `.c` and `.h` files lists them).

An AppArmor profile adapted from upstream (`src-tauri/linux/kubetail-desktop-bwrap`, `kubetail-desktop-pasta`) names
the version it was taken from; update its entry when you take it from a newer one.

`make build-embedded-sources` (`scripts/build-embedded-sources.sh`) bundles the pinned tarballs into
`src-tauri/linux/embedded-sources.tar.gz`, which the release attaches as
`kubetail-desktop-<version>-embedded-sources.tar.gz`, with a README naming each program's version, upstream and
license; it reads the versions from the tarballs, so a bump needs no change there.

A new bundled program gets its license file here, an entry in `THIRD_PARTY_NOTICES.md`, a line for
each in both formats' `files`, and an `add` line in `scripts/build-embedded-sources.sh`.
