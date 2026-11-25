#!/bin/sh
# Runs protoc with the sidecar's Go plugins; arguments pass through to protoc.
# Uses the protoc on PATH when it is the pinned version; otherwise downloads
# the release's prebuilt zip into the user cache, checked against its SHA-256.
# protoc-gen-go and protoc-gen-go-grpc are `tool` dependencies in
# sidecar/go.mod, built fresh on each run so their version is go.mod's.
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)

# The committed bindings record this version; a different one changes them.
version=36.0

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

# Sets protoc to a verified protoc $version, downloading it on first use.
fetch_protoc() {
	exe=
	case "$(uname -s)/$(uname -m)" in
	Darwin/arm64)
		target=osx-aarch_64
		digest=b6bc4afdcb880124bf342851d05155b6e3d9b6e661236d87b9c614250d26ae00
		;;
	Darwin/x86_64)
		target=osx-x86_64
		digest=2847d952ecd1c466769ae3ca319c9cd34c3613542eba335dc9b02c49537f6c70
		;;
	Linux/x86_64)
		target=linux-x86_64
		digest=bc8211ce760bd43ee21ddc145d6d9dbaeeabae205267a79d9054a240e367d4b4
		;;
	Linux/aarch64 | Linux/arm64)
		target=linux-aarch_64
		digest=4a00ec5e256d20a3deadd9e77d56da0ac04c72367c3c959f6d08e110a368400a
		;;
	MINGW*/x86_64 | MSYS*/x86_64 | CYGWIN*/x86_64)
		target=win64
		digest=510fb2369a4720adb457783768c5e1481a0e1137d9e7694478dfe9b8fe445dc1
		exe=.exe
		;;
	*)
		echo "protoc $version: no prebuilt binary for $(uname -s)/$(uname -m); install it yourself" >&2
		exit 1
		;;
	esac

	dir=${XDG_CACHE_HOME:-$HOME/.cache}/kubetail/protoc-$version
	zip=$dir/protoc-$version-$target.zip
	mkdir -p "$dir"

	if [ ! -f "$zip" ]; then
		echo "→ downloading protoc $version ($target)" >&2
		url=https://github.com/protocolbuffers/protobuf/releases/download/v$version/protoc-$version-$target.zip
		curl -fsSL --retry 3 --retry-all-errors -o "$zip.part" "$url"
		mv "$zip.part" "$zip"
	fi

	# Checked on every run, so a corrupted cache fails rather than runs.
	actual=$(sha256 "$zip")
	if [ "$actual" != "$digest" ]; then
		echo "protoc $version: $zip has SHA-256 $actual; expected $digest" >&2
		echo "delete $dir and run again" >&2
		exit 1
	fi

	protoc=$dir/$target/bin/protoc$exe
	if [ ! -x "$protoc" ]; then
		unzip -qo "$zip" -d "$dir/$target"
	fi
}

if [ "$(protoc --version 2>/dev/null || true)" = "libprotoc $version" ]; then
	protoc=protoc
else
	fetch_protoc
fi

plugins=$(mktemp -d)
trap 'rm -rf "$plugins"' EXIT
goexe=$(cd "$root/sidecar" && go env GOEXE)
(
	cd "$root/sidecar"
	go build -o "$plugins/protoc-gen-go$goexe" google.golang.org/protobuf/cmd/protoc-gen-go
	go build -o "$plugins/protoc-gen-go-grpc$goexe" google.golang.org/grpc/cmd/protoc-gen-go-grpc
)

"$protoc" \
	--plugin="protoc-gen-go=$plugins/protoc-gen-go$goexe" \
	--plugin="protoc-gen-go-grpc=$plugins/protoc-gen-go-grpc$goexe" \
	"$@"
