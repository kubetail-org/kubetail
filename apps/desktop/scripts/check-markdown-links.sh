#!/bin/sh
# Checks tracked Markdown's local paths and anchors with lychee. Uses the
# lychee on PATH when it is the pinned version; otherwise downloads the
# release's prebuilt binary into the user cache, checked against its SHA-256.
set -eu

cd "$(dirname "$0")/.."

# Keep in step with lycheeVersion in .github/workflows/ci.yml.
version=0.24.2

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

# Sets lychee to a verified lychee $version, downloading it on first use.
fetch_lychee() {
	case "$(uname -s)/$(uname -m)" in
	Darwin/arm64)
		target=aarch64-apple-darwin
		digest=c9d3740ea2d891854d37116c9fba840f37b6e7c89d330e7db84ac333631c4977
		;;
	Darwin/x86_64)
		target=x86_64-apple-darwin
		digest=887503a9cff667d322b8d0892b40bf49976eb9507af8483220a3706cdad55978
		;;
	Linux/x86_64)
		target=x86_64-unknown-linux-musl
		digest=73657a111819a30c47c08352896796f23d64e4eb2b3ed39b6d32149241566fc5
		;;
	Linux/aarch64 | Linux/arm64)
		target=aarch64-unknown-linux-musl
		digest=5d0b0e3aeab240f41920c633a6eaf97599be6eedda034b36e858ede7dba5e535
		;;
	*)
		echo "lychee $version: no prebuilt binary for $(uname -s)/$(uname -m); install it yourself" >&2
		exit 1
		;;
	esac

	dir=${XDG_CACHE_HOME:-$HOME/.cache}/kubetail/lychee-$version
	tarball=$dir/lychee-$target.tar.gz
	mkdir -p "$dir"

	if [ ! -f "$tarball" ]; then
		echo "→ downloading lychee $version ($target)" >&2
		url=https://github.com/lycheeverse/lychee/releases/download/lychee-v$version/lychee-$target.tar.gz
		curl -fsSL --retry 3 --retry-all-errors -o "$tarball.part" "$url"
		mv "$tarball.part" "$tarball"
	fi

	# Checked on every run, so a corrupted cache fails rather than runs.
	actual=$(sha256 "$tarball")
	if [ "$actual" != "$digest" ]; then
		echo "lychee $version: $tarball has SHA-256 $actual; expected $digest" >&2
		echo "delete $dir and run again" >&2
		exit 1
	fi

	if [ ! -x "$dir/lychee-$target/lychee" ]; then
		tar -xzf "$tarball" -C "$dir"
	fi
	lychee=$dir/lychee-$target/lychee
}

if [ "$(lychee --version 2>/dev/null || true)" = "lychee $version" ]; then
	lychee=lychee
else
	fetch_lychee
fi

git ls-files '*.md' | "$lychee" --config .github/ci-config/lychee.toml --files-from -
