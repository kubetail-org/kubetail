# shellcheck shell=sh
# Sourced by check-go-fix.sh, go-vet.sh and staticcheck.sh, each of which sets
# root to the repo root. go_builds <module> prints the builds a module is
# checked under, one per line: GOOS, GOARCH, build tags and packages, with -
# for the host's GOARCH and for no tag. A file that only one build compiles is
# checked in that build alone.
#
# The workspace's modules differ between Linux and Windows. The desktop's
# sidecar ships for macOS, Linux and Windows, on two Linux architectures
# (seccomp_linux_<arch>.go), and keeps code behind the debug tag and the live
# checks' tags. The live tags are not exclusive, so one build checks both. The
# debug build checks only the packages whose files the tag changes: the others
# compile the same in both builds.
#
# With GO_BUILDS=host, as make sets it, each build runs for this platform
# instead, once per set of tags: CI checks the other platforms.
go_builds() {
	builds=$(all_go_builds "$1") || return
	if [ "${GO_BUILDS:-}" = host ]; then
		echo "$builds" | awk -v goos="$(go env GOOS)" '{ $1 = goos; $2 = "-" } !seen[$0]++'
	else
		echo "$builds"
	fi
}

all_go_builds() {
	case "$1" in
	apps/desktop/sidecar)
		# shellcheck disable=SC2154 # root is the caller's
		debug=$(tagged_packages "$root/$1" debug) || return
		echo "linux amd64 - ./..."
		echo "linux arm64 - ./..."
		echo "darwin amd64 - ./..."
		echo "windows amd64 - ./..."
		# shellcheck disable=SC2086 # packages split on spaces
		[ -z "$debug" ] || echo "linux - debug" $debug
		echo "linux - livecache,liveswitch ./internal/agent/catalog"
		;;
	*)
		echo "linux - - ./..."
		echo "windows - - ./..."
		;;
	esac
}

# tagged_packages <dir> <tags> prints the import path of each package of the
# module in dir whose files, tests included, differ on Linux under the tags.
tagged_packages() {
	format='{{.ImportPath}} {{.GoFiles}} {{.TestGoFiles}} {{.XTestGoFiles}}'
	untagged=$(GOOS=linux go -C "$1" list -f "$format" ./...) || return
	tagged=$(GOOS=linux go -C "$1" list -tags "$2" -f "$format" ./...) || return
	printf '%s\n%s\n' "$untagged" "$tagged" | sort | uniq -u | cut -d ' ' -f 1 | uniq
}
