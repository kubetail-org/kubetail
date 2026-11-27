#!/bin/sh
# Runs go vet over the Go modules named by their directory from the repo root,
# once per build each is checked under (scripts/go-builds.sh).
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
. "$root/scripts/go-builds.sh"
status=0

for mod in "$@"; do
	builds=$(go_builds "$mod")
	while read -r goos goarch tags pkgs; do
		[ "$goarch" != - ] || goarch=$(go env GOARCH)
		[ "$tags" != - ] || tags=""
		# shellcheck disable=SC2086 # packages split on spaces
		GOOS=$goos GOARCH=$goarch go -C "$root/$mod" vet -tags "$tags" $pkgs || status=1
	done <<END
$builds
END
done
exit $status
