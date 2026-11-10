#!/usr/bin/env bash
# Regenerates every committed output and fails if any file changed: the
# protoc and gqlgen code under modules/, the CLI's copy of
# config/default/cli.*, and the UI's __generated__/. A change the tree already
# had is not counted, so it runs on a dirty tree; what it regenerates stays in
# the tree, ready to commit.
#
# The *.pb.go headers record the protoc and plugin versions, so it needs the
# protoc in setup-environment's protoc-version default, and builds the plugins
# tools/go.mod pins.
#
#   bash scripts/check-generated.sh
set -euo pipefail
cd "$(dirname "$0")/.."

action=.github/actions/setup-environment/action.yml
want="$(awk '
  $0 == "  protoc-version:" { in_input = 1; next }
  /^  [^ ]/                 { in_input = 0 }
  in_input && /^    default:/ { gsub(/[^0-9.]/, "", $2); print $2; exit }
' "$action")"
have="$(protoc --version 2>/dev/null | awk '{ print $2 }' || true)"
if [ "$have" != "$want" ]; then
  echo "protoc ${have:-not found}, want $want (protoc-version in $action)" >&2
  exit 1
fi

bin="$(mktemp -d)"
trap 'rm -rf "$bin"' EXIT
(cd tools && go build -o "$bin/" \
  google.golang.org/protobuf/cmd/protoc-gen-go \
  google.golang.org/grpc/cmd/protoc-gen-go-grpc)
export PATH="$bin:$PATH"

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

for module in shared dashboard cluster-api cli; do
  (cd "modules/$module" && go generate ./...)
done
(cd dashboard-ui && pnpm install --frozen-lockfile && pnpm graphql-codegen)

after="$(snapshot)"

if [ "$before" != "$after" ]; then
  echo "generated code is out of date; commit what was regenerated:" >&2
  diff <(printf '%s\n' "$before") <(printf '%s\n' "$after") |
    sed -n 's/^[<>] [0-9]* [0-9]* //p' | sort -u >&2
  exit 1
fi
echo "Generated code is current."
