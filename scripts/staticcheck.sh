#!/bin/sh
# Runs staticcheck over the Go modules named by their directory from the repo
# root, or the workspace's, once per build each is checked under
# (scripts/go-builds.sh), then merges each module's runs. A single run reports
# code used only by another build as unused; -merge reports U1000 only when
# every run agrees.
#
# With GO_BUILDS=host (scripts/go-builds.sh) U1000 is off: one platform's
# builds report code that only another platform's use.
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
. "$root/scripts/go-builds.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Built for the host: under GOOS=windows, `go tool` would build a Windows binary.
go build -C "$root/tools" -o "$work/staticcheck" honnef.co/go/tools/cmd/staticcheck

checks=inherit
[ "${GO_BUILDS:-}" != host ] || checks=inherit,-U1000

status=0
if [ $# -eq 0 ]; then
	set -- cli cluster/api dashboard libs/go-common
fi
for mod in "$@"; do
	cd "$root/$mod"
	name=$(echo "$mod" | tr / -)
	builds=$(go_builds "$mod")
	i=0
	while read -r goos goarch tags pkgs; do
		[ "$goarch" != - ] || goarch=$(go env GOARCH)
		[ "$tags" != - ] || tags=""
		i=$((i + 1))
		# shellcheck disable=SC2086 # packages split on spaces
		GOOS=$goos GOARCH=$goarch "$work/staticcheck" -f binary -checks "$checks" -tags "$tags" $pkgs >"$work/$name-$i.bin"
	done <<END
$builds
END
	if ! "$work/staticcheck" -merge "$work/$name"-*.bin >"$work/$name.out"; then
		sed "s|^|$mod/|" "$work/$name.out"
		status=1
	fi
done
exit $status
