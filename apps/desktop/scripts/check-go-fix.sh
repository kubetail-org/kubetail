#!/bin/sh
# Fails if `go fix` would rewrite the sidecar on any platform or build tag CI
# builds.
# stringsbuilder is off: it turns short `+=` helpers into Builder calls that read worse.
set -eu

cd "$(dirname "$0")/../sidecar"

flags=-stringsbuilder=false
status=0
check() {
	out=$("$@" 2>&1) || true
	if [ -n "$out" ]; then
		echo "$out"
		status=1
	fi
}
for goos in darwin linux windows; do
	check env GOOS=$goos GOARCH=amd64 go fix $flags -diff ./...
done
check env GOOS=linux GOARCH=arm64 go fix $flags -diff ./...
check go fix $flags -tags debug -diff ./...
for tag in livecache liveswitch; do
	check go fix $flags -tags $tag -diff ./internal/agent/catalog
done
if [ "$status" -ne 0 ]; then
	echo "Run 'go fix $flags ./...' in sidecar/ (with the GOOS or tag above) and review the result."
fi
exit $status
