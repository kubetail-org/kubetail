#!/bin/sh
# Runs staticcheck over the sidecar once per GOOS, once for linux/arm64 and
# once per build tag, then merges the runs. A single run reports code used
# only by another platform or tag as unused; -merge reports U1000 only when
# every run agrees. The tools module needs one run.
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Built for the host: under GOOS=windows, `go tool` would build a Windows binary.
go build -C "$root/tools" -o "$work/staticcheck" honnef.co/go/tools/cmd/staticcheck

cd "$root/sidecar"
for goos in linux darwin windows; do
	GOOS=$goos GOARCH=amd64 "$work/staticcheck" -f binary ./... >"$work/$goos.bin"
done
GOOS=linux GOARCH=arm64 "$work/staticcheck" -f binary ./... >"$work/linux-arm64.bin"
"$work/staticcheck" -f binary -tags debug ./... >"$work/debug.bin"
for tag in livecache liveswitch; do
	"$work/staticcheck" -f binary -tags "$tag" ./internal/agent/catalog >"$work/$tag.bin"
done
"$work/staticcheck" -merge "$work"/*.bin

cd "$root/tools"
"$work/staticcheck" ./...
