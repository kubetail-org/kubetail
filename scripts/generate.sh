#!/usr/bin/env bash
# Regenerates every committed output: the protoc and gqlgen code in the Go
# modules, the CLI's copy of config/default/cli.*, and the UI's
# __generated__/.
#
# The *.pb.go headers record the protoc and plugin versions, so it needs the
# protoc in setup-environment's protoc-version default, and builds the plugins
# tools/go.mod pins.
#
#   bash scripts/generate.sh
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

for module in libs/go-common dashboard cluster/api cli; do
  (cd "$module" && go generate ./...)
done
(cd apps/web && pnpm install --frozen-lockfile && pnpm graphql-codegen)
