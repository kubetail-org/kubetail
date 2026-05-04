#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
E2E_DIR="$REPO_ROOT/e2e"

echo "Building CLI (dashboard UI + kubetail binary)..."
make -C "$REPO_ROOT" build

echo "Building e2e Docker images..."
cd "$E2E_DIR"
docker buildx bake --allow=fs.read=.. --load --file docker-bake.hcl

echo ""
echo "Build complete."
echo "  CLI:    $REPO_ROOT/bin/kubetail"
echo "  Images: kubetail-dashboard:e2e, kubetail-cluster-api:e2e, kubetail-cluster-agent:e2e"
