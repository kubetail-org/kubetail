# shellcheck shell=sh
# Sourced by check-go-fix.sh and staticcheck.sh. go_builds <module> prints the
# builds a module is checked under, one per line: GOOS, GOARCH, build tags and
# packages, with - for the host's GOARCH and for no tag. A file that only one
# build compiles is checked in that build alone.
#
# The workspace's modules differ between Linux and Windows. The desktop's
# sidecar ships for macOS, Linux and Windows, on two Linux architectures
# (seccomp_linux_<arch>.go), and keeps code behind the debug tag and the live
# checks' tags.
#
# With GO_BUILDS=host, as make sets it, each build runs for this platform
# instead, once per set of tags: CI checks the other platforms.
go_builds() {
	if [ "${GO_BUILDS:-}" = host ]; then
		all_go_builds "$1" | awk -v goos="$(go env GOOS)" '{ $1 = goos; $2 = "-" } !seen[$0]++'
	else
		all_go_builds "$1"
	fi
}

all_go_builds() {
	case "$1" in
	apps/desktop/sidecar)
		echo "linux amd64 - ./..."
		echo "linux arm64 - ./..."
		echo "darwin amd64 - ./..."
		echo "windows amd64 - ./..."
		echo "linux - debug ./..."
		echo "linux - livecache ./internal/agent/catalog"
		echo "linux - liveswitch ./internal/agent/catalog"
		;;
	*)
		echo "linux - - ./..."
		echo "windows - - ./..."
		;;
	esac
}
