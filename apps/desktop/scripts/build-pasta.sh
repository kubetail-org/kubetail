#!/usr/bin/env bash
# Builds the pasta Kubetail's Linux packages carry, from one pinned passt release,
# into src-tauri/linux/: pasta itself and the tarball it was built from, which
# `make build-desktop-embedded-sources` bundles as pasta's source. It fails when licenses/ is
# stale for that release (licenses/README.md).
#
# pasta is passt's binary under another name, which is how it knows to run as
# pasta. Its AVX2 build is left out: pasta runs without it, after one warning
# a run never shows.
#
# Run it on the machine that builds the bundle: pasta links the glibc it finds.
#
# Usage: scripts/build-pasta.sh   (needs curl, make and a C compiler)
set -euo pipefail

VERSION="2026_10_02.cba3570"
SHA256="aa75616cc43925f0bd6c05bcfbbaecd77397c03d7bd23591b34ef535be5dfcdc"

if [ "$(uname -s)" != Linux ]; then
  echo "pasta is Linux's alone" >&2
  exit 1
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="$ROOT/src-tauri/linux"
LICENSES="$ROOT/licenses"
TARBALL="passt-$VERSION.tar.xz"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "→ fetching passt $VERSION"
curl -fsSL --retry 3 --retry-all-errors -o "$WORK/$TARBALL" "https://passt.top/passt/snapshot/$TARBALL"
echo "$SHA256  $WORK/$TARBALL" | sha256sum -c --quiet -

tar -xJf "$WORK/$TARBALL" -C "$WORK"
SRC="$WORK/passt-$VERSION"

grep -qF -- "**Version:** $VERSION" "$LICENSES/THIRD_PARTY_NOTICES.md" ||
  { echo "licenses/THIRD_PARTY_NOTICES.md does not name passt $VERSION" >&2; exit 1; }
for license in GPL-2.0-or-later BSD-3-Clause; do
  cmp -s "$SRC/LICENSES/$license.txt" "$LICENSES/passt-LICENSE-$license" ||
    { echo "passt $VERSION's LICENSES/$license.txt differs from licenses/passt-LICENSE-$license" >&2; exit 1; }
done

# The tarball has no git history to describe the version from.
make -C "$SRC" VERSION="$VERSION" passt

mkdir -p "$OUT_DIR"
install -m 0755 "$SRC/passt" "$OUT_DIR/pasta"
install -m 0644 "$WORK/$TARBALL" "$OUT_DIR/$TARBALL"
echo "✓ $OUT_DIR/pasta ($("$OUT_DIR/pasta" --version | head -1))"
