#!/bin/sh
# Each toolchain has one exact version, which CI installs and every Dockerfile
# builds with. Go and Node come from setup-environment's go-version and
# node-version defaults, Rust from crates/rust-toolchain.toml. Prints every
# FROM line whose golang, node or rust image names another version, and fails.
#
#   sh scripts/check-toolchain-versions.sh
set -eu
cd "$(dirname "$0")/.."

action=.github/actions/setup-environment/action.yml

# input_default <name>: the default of setup-environment's input <name>.
input_default() {
  awk -v key="  $1:" '
    $0 == key        { in_input = 1; next }
    /^  [^ ]/        { in_input = 0 }
    in_input && /^    default:/ { gsub(/[^0-9.]/, "", $2); print $2; exit }
  ' "$action"
}

# Every FROM line as file:line:FROM [flags] image, the stage name cut off so
# that a stage called node is not read as an image.
froms="$(grep -inE '^[[:space:]]*FROM[[:space:]]' build/package/Dockerfile.* hack/tilt/Dockerfile.* |
  sed -E 's/[[:space:]]+[Aa][Ss][[:space:]].*//')"

status=0

# check <image> <version> <source>: every <image> is <image>:<version>, with
# an optional variant (-slim) and digest.
check() {
  if ! printf '%s\n' "$2" | grep -qxE '[0-9]+\.[0-9]+\.[0-9]+'; then
    echo "$3 is not an exact version: '$2'" >&2
    status=1
    return
  fi
  version="$(printf '%s\n' "$2" | sed 's/\./\\./g')"
  bad="$(printf '%s\n' "$froms" |
    grep -E "[[:space:]/]$1([:@]|[[:space:]]*\$)" |
    grep -vE "[[:space:]/]$1:$version(-[^@[:space:]]*)?(@[^[:space:]]*)?[[:space:]]*\$" || true)"
  if [ -n "$bad" ]; then
    printf '%s\n' "$bad" | sed "s/\$/, want $2/"
    status=1
  fi
}

check golang "$(input_default go-version)" go-version
check node "$(input_default node-version)" node-version
check rust "$(sed -n 's/^channel = "\(.*\)"$/\1/p' crates/rust-toolchain.toml)" rust-toolchain.toml

if [ "$status" -ne 0 ]; then
  echo "the Dockerfiles must build with the toolchain versions CI installs" >&2
fi
exit "$status"
