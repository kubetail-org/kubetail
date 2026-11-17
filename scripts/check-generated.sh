#!/usr/bin/env bash
# Runs scripts/generate.sh and fails if any committed output changed. A change
# the tree already had is not counted, so it runs on a dirty tree; what it
# regenerates stays in the tree, ready to commit.
#
#   bash scripts/check-generated.sh
set -euo pipefail
cd "$(dirname "$0")/.."

outputs=(
  modules/shared/clusteragentpb
  modules/shared/testpb
  modules/dashboard/graph
  modules/cluster-api/graph
  modules/cli/assets
  dashboard-ui/src/lib/graphql/dashboard/__generated__
  dashboard-ui/src/lib/graphql/cluster-api/__generated__
)

# Every file under the outputs with its checksum.
snapshot() {
  find "${outputs[@]}" -type f -exec cksum {} + | sort -k3
}

before="$(snapshot)"
bash scripts/generate.sh
after="$(snapshot)"

if [ "$before" != "$after" ]; then
  echo "generated code is out of date; commit what was regenerated:" >&2
  diff <(printf '%s\n' "$before") <(printf '%s\n' "$after") |
    sed -n 's/^[<>] [0-9]* [0-9]* //p' | sort -u >&2
  exit 1
fi
echo "Generated code is current."
