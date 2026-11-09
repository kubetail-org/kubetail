#!/usr/bin/env bash
# Set up a Linux-native dev environment for Kubetail inside the sandbox,
# without letting its build artifacts collide with the macOS host's.
#
# The repo is bind-mounted from the macOS host, so every build output path is
# shared. Two mechanisms keep the two platforms apart:
#
#   1. Configurable output paths (Cargo's target/, the pnpm store) are pointed
#      at Linux-only directories through environment variables.
#   2. Fixed paths the tooling can't relocate (dashboard-ui/node_modules and
#      dashboard-ui/dist) get a sandbox-only bind mount on top of them. A bind
#      mount is invisible to the host: macOS keeps its own directory
#      underneath, untouched, while the sandbox sees the Linux one.
#
# target/, node_modules and the pnpm store live on the sandbox's own overlay
# ($OVERLAY_ROOT). The host mount's directory metadata is not coherent under
# Cargo's dense writes, and node_modules is read thousands of times per test
# run, which the host mount makes slow. Every worktree shares the one target/
# and the one pnpm store, and has its own node_modules. pnpm copies from the
# store rather than hard-linking, since link(2) fails across the node_modules
# bind mount.
#
# dist, small and rarely read, stays on the host's disk under $BUILD_ROOT.
# modules/dashboard/website/ is left alone: `make build-dashboard-ui` removes
# and recreates it, which a mount point would refuse, and its contents are the
# same on both platforms.
#
# The image's uv is built for 4 KiB pages and aborts on a 16 KiB-page host, so
# a uv that does not run is replaced by the PyPI build in ~/.local/bin, which
# comes first on PATH.
#
# Idempotent: safe to re-run. Bind mounts do not survive a sandbox restart; the
# env vars (in /etc/sandbox-persistent.sh) do.

set -euo pipefail

# No-op outside the sandbox. This runs from a SessionStart hook, and the repo is
# shared with the macOS host, whose agents fire the same hook. $SANDBOX_VM_ID is
# set only inside the sandbox; the Linux check guards against a stray export.
if [ -z "${SANDBOX_VM_ID:-}" ] || [ "$(uname -s)" != "Linux" ]; then
  exit 0
fi

REPO="$(cd "$(dirname "$0")/.." && pwd)"
BUILD_ROOT="${KUBETAIL_SANDBOX_BUILD_ROOT:-$REPO/.sandbox-linux}"
OVERLAY_ROOT="${KUBETAIL_SANDBOX_OVERLAY_ROOT:-/home/agent/kubetail-build}"
PERSIST="${KUBETAIL_SANDBOX_PERSIST:-/etc/sandbox-persistent.sh}"
NODE_MODULES="$OVERLAY_ROOT/worktrees$REPO/dashboard-ui/node_modules"
PNPM_STORE="$OVERLAY_ROOT/pnpm-store"
# CI's astral-sh/setup-uv takes the latest uv, so the pin lives here.
UV_VERSION="0.13.7"
MIN_FREE_MB=2048

echo "→ build root: $BUILD_ROOT"
echo "→ overlay root: $OVERLAY_ROOT"
mkdir -p "$BUILD_ROOT/dist" "$PNPM_STORE" "$NODE_MODULES" "$OVERLAY_ROOT/target"

free_mb="$(df --output=avail -BM "$OVERLAY_ROOT" | tail -1 | tr -dc 0-9)"
if [ "$free_mb" -lt "$MIN_FREE_MB" ]; then
  echo "⚠ only ${free_mb}M free on the overlay; installs and builds may fail."
  echo "  Reclaim space with 'cargo clean', 'go clean -cache', or by deleting"
  echo "  removed worktrees' node_modules under $OVERLAY_ROOT/worktrees."
fi

# --- 1. bind mounts for paths the tooling can't relocate -------------------
# The image ships util-linux without mount and umount.
if ! command -v mount >/dev/null; then
  sudo apt-get update -qq
  sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq mount >/dev/null
fi

# A mount from any other source is replaced. Comparing device and inode tells
# whether $target already shows $src.
bind() {
  local src="$1" target="$2"
  mkdir -p "$target"
  if [ "$(stat -c %d:%i "$src")" = "$(stat -c %d:%i "$target")" ]; then
    echo "✓ $target already bind-mounted"
    return
  fi
  if mountpoint -q "$target"; then
    sudo umount "$target"
  fi
  sudo mount --bind "$src" "$target"
  echo "✓ $target bind-mounted from $src"
}
bind "$NODE_MODULES" "$REPO/dashboard-ui/node_modules"
bind "$BUILD_ROOT/dist" "$REPO/dashboard-ui/dist"

# --- 2. env vars for the paths that are configurable -----------------------
# Written to the persistent env file, which is sourced before every command and
# shared by every worktree, so no value may depend on $REPO. Every line this
# script owns is dropped first, so a re-run rewrites the values rather than
# leaving an older copy above them.
#
# pnpm takes the store path from PNPM_CONFIG_STORE_DIR rather than
# pnpm-workspace.yaml, which is committed and shared with the host and CI.
# pnpm's global virtual store stays off: tsc then resolves two copies of
# @types/react and `pnpm build` fails.
#
# Go is pinned to CI's version, read from setup-environment's go-version
# default. A go.mod's go line is only a minimum, and GOTOOLCHAIN=auto switches
# only upward, so a newer system go would otherwise run every check. +auto
# still upgrades if a go.mod ever requires a newer Go.
GO_VERSION="$(awk '
  /^  go-version:/ { in_go = 1; next }
  /^  [^ ]/        { in_go = 0 }
  in_go && /^    default:/ { gsub(/[^0-9.]/, "", $2); print $2; exit }
' "$REPO/.github/actions/setup-environment/action.yml")"
if [ -z "$GO_VERSION" ]; then
  echo "✗ no go-version default in .github/actions/setup-environment/action.yml" >&2
  exit 1
fi

sudo touch "$PERSIST"
sudo sed -i \
  -e '/KUBETAIL_SANDBOX_ENV/d' \
  -e '/^export CARGO_TARGET_DIR=/d' \
  -e '/^export PNPM_CONFIG_STORE_DIR=/d' \
  -e '/^export GOTOOLCHAIN=/d' \
  "$PERSIST"
sudo tee -a "$PERSIST" >/dev/null <<EOF
# KUBETAIL_SANDBOX_ENV — Linux build outputs kept out of the macOS host's paths, and Go pinned to CI's.
export CARGO_TARGET_DIR="$OVERLAY_ROOT/target"
export PNPM_CONFIG_STORE_DIR="$PNPM_STORE"
export GOTOOLCHAIN=go$GO_VERSION+auto
EOF
echo "✓ env vars written to $PERSIST (Go pinned to $GO_VERSION)"

# --- 3. a uv that runs -----------------------------------------------------
# Checks that uv runs, not the page size, so this is a no-op once the image's
# package works. Calling a function with stderr discarded keeps bash's
# "Aborted" report for a crashing uv out of the output.
uv_runs() { uv --version >/dev/null 2>&1; }
if uv_runs 2>/dev/null; then
  echo "✓ uv runs"
else
  python3 -m pip install --user --quiet "uv==$UV_VERSION"
  echo "✓ uv $UV_VERSION installed in ~/.local/bin"
fi

echo
echo "Done. Open a new shell (or 'source $PERSIST') to pick up the env vars."
