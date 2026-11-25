#!/usr/bin/env bash
# Signs a desktop release's updater artifacts and writes one manifest per key.
#
#   scripts/sign-updater.sh <version> <dir>
#
# <dir> holds kubetail-desktop-<version>-darwin-universal.app.tar.gz and the
# signed kubetail-desktop-<version>-windows-{amd64,arm64}.msi. Each is signed, the
# version bound into the signature, with:
#
#   - the current key, TAURI_SIGNING_PRIVATE_KEY and its password in
#     TAURI_SIGNING_PRIVATE_KEY_PASSWORD, which must be the one whose public half
#     src-tauri/tauri.conf.json carries, and whose id its endpoint names;
#   - the previous key, if TAURI_SIGNING_PRIVATE_KEY_PREVIOUS is set (password
#     in TAURI_SIGNING_PRIVATE_KEY_PREVIOUS_PASSWORD): a retired key, for the
#     apps two rotations behind, which trust neither the current key nor the
#     standby.
#
# Each key's signatures go in <dir>/latest-<key id>.json, the manifest the apps
# that trust that key read from https://updates.kubetail.com/desktop/stable/.
# Prints each manifest's name. The standby key, src-tauri/updater-standby.json,
# signs nothing: the script only checks that it is set and is neither key it
# signs with. → docs/updater-key.md
#
# The signer is the Tauri CLI's prebuilt cargo-tauri, checked against its
# SHA-256. It runs in release-desktop.yml's sign-updater job, which installs
# nothing else. → docs/adr/2026-11-08-the-updater-trusts-a-current-and-a-standby-key.md
set -euo pipefail
# A failure inside `$(sign_with ...)` fails the script too.
shopt -s inherit_errexit

version=$1
dir=$2

root=$(cd "$(dirname "$0")/.." && pwd)
repo=https://github.com/kubetail-org/kubetail
updates=https://updates.kubetail.com/desktop/stable

# The Tauri CLI version pnpm-lock.yaml resolves.
tauri_cli=2.12.4

case "$(uname -s)/$(uname -m)" in
Linux/x86_64)
	target=x86_64-unknown-linux-gnu
	digest=820ae3c7580ca637cad15f65638960ee2a96e5a491e224691c86d6f25e6c9e8a
	;;
Linux/aarch64)
	target=aarch64-unknown-linux-gnu
	digest=4f601b9f462c3d3d10ce67b6ae963f099f6a5a3253920dae95a253abd558d723
	;;
*)
	echo "sign-updater: no pinned cargo-tauri for $(uname -s)/$(uname -m)" >&2
	exit 1
	;;
esac

tools=$(mktemp -d)
trap 'rm -rf "$tools"' EXIT

curl -fsSL --retry 3 --retry-all-errors -o "$tools/cargo-tauri.tgz" \
	"https://github.com/tauri-apps/tauri/releases/download/tauri-cli-v$tauri_cli/cargo-tauri-$target.tgz"
echo "$digest  $tools/cargo-tauri.tgz" | sha256sum -c --quiet -
tar -xzf "$tools/cargo-tauri.tgz" -C "$tools" cargo-tauri

# A minisign key id, as minisign prints it in a public key's comment: bytes
# 2..10 of a key's or signature's second line, which tauri wraps in one more
# base64, read as a little-endian integer. Both pinned targets are
# little-endian, so od's own byte order is the key's.
key_id() {
	base64 -d <<<"$1" | sed -n 2p | base64 -d | od -An -tx8 -j2 -N8 | tr -d ' \n' | tr a-f A-F
}

# Prints the id of a key the app is built with: <file>'s public key, read by
# the jq filter <pubkey>. Fails when the key is unset or when <endpoint>, a jq
# filter too, is not the manifest named after it.
committed_key() {
	local name=$1 file=$2 pubkey endpoint id
	pubkey=$(jq -r "$3" "$file")
	endpoint=$(jq -r "$4" "$file")
	if [ -z "$pubkey" ]; then
		echo "sign-updater: no $name public key in $file" >&2
		exit 1
	fi
	id=$(key_id "$pubkey")
	if [ "$endpoint" != "$updates/latest-$id.json" ]; then
		echo "sign-updater: the $name endpoint is $endpoint; the $name key $id reads latest-$id.json" >&2
		exit 1
	fi
	echo "$id"
}

current=$(committed_key current "$root/src-tauri/tauri.conf.json" \
	.plugins.updater.pubkey '.plugins.updater.endpoints | join(" ")')
standby=$(committed_key standby "$root/src-tauri/updater-standby.json" .pubkey .endpoint)
if [ "$standby" = "$current" ]; then
	echo "sign-updater: the standby key is the current one" >&2
	exit 1
fi

# Platform keys the updater looks up (`<os>-<arch>`), and the file each takes.
macos="kubetail-desktop-$version-darwin-universal.app.tar.gz"
declare -A files=(
	[darwin-aarch64]=$macos
	[darwin-x86_64]=$macos
	[windows-x86_64]="kubetail-desktop-$version-windows-amd64.msi"
	[windows-aarch64]="kubetail-desktop-$version-windows-arm64.msi"
)

# Signs every file with one key and writes its manifest. Prints the key's id,
# read off its first signature.
sign_with() {
	local key=$1 password=$2
	local id='' platforms='{}' platform file signature
	for platform in "${!files[@]}"; do
		file=${files[$platform]}
		TAURI_SIGNING_PRIVATE_KEY=$key TAURI_SIGNING_PRIVATE_KEY_PASSWORD=$password \
			"$tools/cargo-tauri" signer sign --app-version "$version" "$dir/$file" >/dev/null
		signature=$(cat "$dir/$file.sig")
		rm "$dir/$file.sig"
		id=${id:-$(key_id "$signature")}
		# The tag's slash stays as it is: GitHub serves desktop/v<version> under it.
		platforms=$(jq -c --arg p "$platform" --arg s "$signature" \
			--arg u "$repo/releases/download/desktop/v$version/$file" \
			'.[$p] = {signature: $s, url: $u}' <<<"$platforms")
	done
	jq -n --arg v "$version" --arg d "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --argjson p "$platforms" \
		'{version: $v, pub_date: $d, platforms: $p}' >"$dir/latest-$id.json"
	echo "$id"
}

id=$(sign_with "$TAURI_SIGNING_PRIVATE_KEY" "${TAURI_SIGNING_PRIVATE_KEY_PASSWORD:-}")
if [ "$id" != "$current" ]; then
	echo "sign-updater: TAURI_SIGNING_PRIVATE_KEY is key $id; tauri.conf.json pins $current" >&2
	exit 1
fi
echo "latest-$id.json"

if [ -n "${TAURI_SIGNING_PRIVATE_KEY_PREVIOUS:-}" ]; then
	id=$(sign_with "$TAURI_SIGNING_PRIVATE_KEY_PREVIOUS" "${TAURI_SIGNING_PRIVATE_KEY_PREVIOUS_PASSWORD:-}")
	if [ "$id" = "$current" ] || [ "$id" = "$standby" ]; then
		echo "sign-updater: the previous key is the current or the standby one" >&2
		exit 1
	fi
	echo "latest-$id.json"
fi
