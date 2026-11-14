#!/bin/sh
# Fails if `go fix` would rewrite a Go module, on Linux or Windows. Takes the
# modules to check, by their directory in modules/; all of them by default.
# plusbuild is off: it reads files the build ignores, such as tools.go, which
# the go command leaves out of its cache key, so a cached diff outlives the fix.
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"

flags=-plusbuild=false
status=0
if [ $# -eq 0 ]; then
	set -- cli cluster-api dashboard shared
fi
for mod in "$@"; do
	for goos in linux windows; do
		out=$(GOOS=$goos go -C "$root/modules/$mod" fix $flags -diff ./... 2>&1) || true
		if [ -n "$out" ]; then
			echo "$out"
			echo "Run 'GOOS=$goos go fix $flags ./...' in modules/$mod and review the result."
			status=1
		fi
	done
done
exit $status
