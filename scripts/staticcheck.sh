#!/bin/sh
# Runs staticcheck over the Go modules named by their directory from the repo
# root, or all of them, once per GOOS, then merges each module's runs.
# A single run reports code used only by another platform as unused; -merge
# reports U1000 only when every run agrees.
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Built for the host: under GOOS=windows, `go tool` would build a Windows binary.
go build -C "$root/tools" -o "$work/staticcheck" honnef.co/go/tools/cmd/staticcheck

status=0
if [ $# -eq 0 ]; then
	set -- cli cluster/api dashboard libs/go-common
fi
for mod in "$@"; do
	cd "$root/$mod"
	name=$(echo "$mod" | tr / -)
	for goos in linux windows; do
		GOOS=$goos "$work/staticcheck" -f binary ./... >"$work/$name-$goos.bin"
	done
	if ! "$work/staticcheck" -merge "$work/$name"-*.bin >"$work/$name.out"; then
		sed "s|^|$mod/|" "$work/$name.out"
		status=1
	fi
done
exit $status
