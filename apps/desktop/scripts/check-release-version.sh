#!/usr/bin/env bash
# Checks a release version: check-release-version.sh desktop <version>.
# The release workflow's config job and the release-version skill run it.
# The version enters build configuration, paths and shell scripts downstream,
# so nothing passes that the pattern does not match whole.
#
# It also prints the versions the RPM and MSI carry, as
# key=value lines for $GITHUB_OUTPUT. Both are derived from <x>.<y>.<z> and
# <x>.<y>.<z>-rc.<n> alone, so it accepts no other prerelease.
set -euo pipefail

product="${1:-}"
version="${2:-}"
core='(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)'

case "$product" in
  desktop)
    if [[ ! "$version" =~ ^(${core})(-rc\.([1-9][0-9]*))?$ ]]; then
      echo "Invalid release version \"$version\": want <x>.<y>.<z> or <x>.<y>.<z>-rc.<n>" >&2
      exit 1
    fi
    xyz="${BASH_REMATCH[1]}"
    rc="${BASH_REMATCH[6]}"
    # RPM Release 0.rc.<n> sorts before Release 1, the final.
    echo "rpm_version=${xyz}"
    if [ -n "$rc" ]; then
      echo "rpm_release=0.rc.${rc}"
      echo "msi_version=${xyz}.${rc}"
    else
      echo "rpm_release=1"
      echo "msi_version=${xyz}"
    fi
    ;;
  *)
    echo "usage: check-release-version.sh desktop <version>" >&2
    exit 2
    ;;
esac
