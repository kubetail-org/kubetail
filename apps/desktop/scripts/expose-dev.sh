#!/usr/bin/env bash
# Expose the Kubetail dev server from the sbx sandbox to the host.
# Run this on your macOS host, not inside the sandbox.
#
# Usage:
#   ./scripts/expose-dev.sh <sandbox-name>
#   ./scripts/expose-dev.sh claude-kubetail
#
# Ports published:
#   1420 — Vite dev server  (http://localhost:1420)

set -euo pipefail

SANDBOX="${1:-}"

if [[ -z "$SANDBOX" ]]; then
  echo "Usage: $0 <sandbox-name>" >&2
  exit 1
fi

echo "Publishing ports for sandbox: $SANDBOX"
sbx ports "$SANDBOX" --publish 1420:1420/tcp

echo ""
echo "Dev server available at: http://localhost:1420"
