#!/bin/sh
# Prints the path of a shellcheck at the pinned version: the one on PATH when it
# matches, otherwise the release's static binary, downloaded into the user cache
# and checked against its SHA-256.
set -eu

version=0.11.0

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

if shellcheck --version 2>/dev/null | grep -qx "version: $version"; then
	command -v shellcheck
	exit 0
fi

# The release publishes no checksums file; these are its assets' digests.
case "$(uname -s)/$(uname -m)" in
Darwin/arm64)
	target=darwin.aarch64
	digest=339b930feb1ea764467013cc1f72d09cd6b869ebf1013296ba9055ab2ffbd26f
	;;
Darwin/x86_64)
	target=darwin.x86_64
	digest=c2c15e08df0e8fbc374c335b230a7ee958c313fa5714817a59aa59f1aa594f51
	;;
Linux/x86_64)
	target=linux.x86_64
	digest=b7af85e41cc99489dcc21d66c6d5f3685138f06d34651e6d34b42ec6d54fe6f6
	;;
Linux/aarch64 | Linux/arm64)
	target=linux.aarch64
	digest=68a8133197a50beb8803f8d42f9908d1af1c5540d4bb05fdfca8c1fa47decefc
	;;
*)
	echo "shellcheck $version: no prebuilt binary for $(uname -s)/$(uname -m); install it yourself" >&2
	exit 1
	;;
esac

dir=${XDG_CACHE_HOME:-$HOME/.cache}/kubetail/shellcheck-$version
tarball=$dir/shellcheck-v$version.$target.tar.gz
mkdir -p "$dir"

if [ ! -f "$tarball" ]; then
	echo "→ downloading shellcheck $version ($target)" >&2
	url=https://github.com/koalaman/shellcheck/releases/download/v$version/shellcheck-v$version.$target.tar.gz
	curl -fsSL --retry 3 --retry-all-errors -o "$tarball.part" "$url"
	mv "$tarball.part" "$tarball"
fi

# Checked on every run, so a corrupted cache fails rather than runs.
actual=$(sha256 "$tarball")
if [ "$actual" != "$digest" ]; then
	echo "shellcheck $version: $tarball has SHA-256 $actual; expected $digest" >&2
	echo "delete $dir and run again" >&2
	exit 1
fi

if [ ! -x "$dir/shellcheck-v$version/shellcheck" ]; then
	tar -xzf "$tarball" -C "$dir"
fi
echo "$dir/shellcheck-v$version/shellcheck"
