#!/bin/sh
# Fails if `go fix` would rewrite a Go module, in any build it is checked under
# (scripts/go-builds.sh). Takes the modules to check, by their directory from
# the repo root; the workspace's by default.
# plusbuild is off: it reads files the build ignores, such as tools.go, which
# the go command leaves out of its cache key, so a cached diff outlives the fix.
# stringsbuilder is off: it turns short `+=` helpers into Builder calls that
# read worse.
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
. "$root/scripts/go-builds.sh"
flags="-plusbuild=false -stringsbuilder=false"
status=0

if [ $# -eq 0 ]; then
	set -- cli cluster/api dashboard libs/go-common
fi
for mod in "$@"; do
	builds=$(go_builds "$mod")
	while read -r goos goarch tags pkgs; do
		[ "$goarch" != - ] || goarch=$(go env GOARCH)
		[ "$tags" != - ] || tags=""
		# shellcheck disable=SC2086 # flags and packages split on spaces
		out=$(GOOS=$goos GOARCH=$goarch go -C "$root/$mod" fix $flags -tags "$tags" -diff $pkgs 2>&1) || true
		if [ -n "$out" ]; then
			echo "$out"
			echo "Run 'GOOS=$goos GOARCH=$goarch go fix $flags -tags \"$tags\" $pkgs' in $mod and review the result."
			status=1
		fi
	done <<END
$builds
END
done
exit $status
