#!/usr/bin/env bash
# Builds src-tauri/linux/embedded-sources.tar.gz, which the release attaches as
# kubetail-desktop-<version>-embedded-sources.tar.gz: the one asset holding the source of every third-party program the Linux packages carry: each
# program's upstream tarball unpacked unchanged, the Kubetail scripts that built
# it, and a README naming each one's version, upstream and license.
#
# It reads the tarballs `make build-bwrap` and `make build-pasta` leave in src-tauri/linux/,
# so it follows their pins. The archive is reproducible: the same tarballs give
# the same bytes on any runner, which lets the release check that every
# architecture built from the same source.
#
# Usage: scripts/build-embedded-sources.sh   (needs GNU tar and gzip)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="$ROOT/src-tauri/linux"
ARCHIVE="$OUT_DIR/embedded-sources.tar.gz"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

TREE="$WORK/embedded-sources"
mkdir -p "$TREE/kubetail"

# Unpacks a tarball into $TREE/<dir> and prints the README's entry for it.
add() {
  local dir="$1" name="$2" binary="$3" prefix="$4" upstream="$5" license="$6" script="$7"
  local matches=("$OUT_DIR/$prefix"-*.tar.xz) file version
  file="${matches[0]}"
  if [ "${#matches[@]}" -ne 1 ] || [ ! -f "$file" ]; then
    echo "want one $prefix-*.tar.xz in $OUT_DIR, found: ${matches[*]}" >&2
    exit 1
  fi
  version="$(basename "$file" .tar.xz)"
  version="${version#"$prefix"-}"

  mkdir -p "$TREE/$dir"
  tar -xJf "$file" -C "$TREE/$dir" --strip-components=1
  install -m 0644 "$ROOT/scripts/$script" "$TREE/kubetail/$script"

  cat <<EOF

## $name

- **Built into:** \`$binary\`
- **Directory:** \`$dir/\`
- **Version:** $version
- **Upstream:** <$upstream>
- **Unpacked from:** \`$(basename "$file")\` (SHA-256 \`$(sha256sum "$file" | cut -d' ' -f1)\`)
- **License:** $license; see the license files in \`$dir/\`
- **Built with:** \`kubetail/$script\`
EOF
}

{
  cat <<'EOF'
# Embedded sources

The complete source of the third-party programs Kubetail's Linux packages carry, unmodified. Each
directory is the program's upstream release tarball, unpacked unchanged, with its own license and
copyright notices. `kubetail/` holds the scripts Kubetail built each program with. Each program keeps
its own license; Kubetail's license does not cover them.
EOF
  add bubblewrap Bubblewrap /usr/lib/kubetail-desktop/bwrap bubblewrap \
    https://github.com/containers/bubblewrap LGPL-2.0-or-later build-bwrap.sh
  add passt passt /usr/lib/kubetail-desktop/pasta passt \
    https://passt.top/ "GPL-2.0-or-later, with parts under BSD-3-Clause" build-pasta.sh
} > "$TREE/README.md"

# Fixed order, owners, modes and times, and no gzip timestamp, so the bytes
# depend on the tarballs alone.
tar -C "$WORK" --format=gnu --sort=name --mtime=@0 \
  --owner=0 --group=0 --numeric-owner --mode=u+rwX,go+rX,go-w \
  -cf - embedded-sources | gzip -9n > "$ARCHIVE"
echo "✓ $ARCHIVE"
