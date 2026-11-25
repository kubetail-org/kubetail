#!/usr/bin/env bash
# Set up a Linux-native dev environment for Kubetail inside the sandbox,
# without letting its build artifacts collide with the macOS host's.
#
# The repo is bind-mounted from the macOS host, so every build output path is
# shared. Two mechanisms keep the two platforms apart:
#
#   1. Configurable output paths (Cargo, pnpm store) are pointed at Linux-only
#      directories via environment variables.
#   2. Fixed paths that tooling can't relocate (node_modules, dist) get a
#      sandbox-only bind mount on top of them. A bind mount is invisible to the
#      host: macOS keeps its own node_modules underneath, untouched, while the
#      sandbox sees the Linux one.
#
# Cargo's target/, node_modules and the pnpm store live on the sandbox's own
# overlay ($OVERLAY_ROOT). Cargo needs it: the host mount's directory metadata
# is not coherent under dense writes, so a build there never converges.
# node_modules is read thousands of times per test run, and the host mount makes
# every read slow. Every worktree shares the one target/. Dev builds keep line
# tables only, as in CI: backtraces still show file and line, and target/ stays
# a fraction of the size full debug info takes.
#
# The image's uv is built for 4 KiB pages and aborts on a 16 KiB-page host
# (Apple silicon). When it does not run, the script pip-installs the pinned uv
# into ~/.local/bin, which comes before /usr/bin on PATH.
#
# Every worktree also shares the one pnpm store, in its global virtual store
# layout: package files live once inside the store, and each worktree's
# node_modules under $OVERLAY_ROOT/worktrees holds only symlinks into it. The
# store's hard links stay on one mount; link(2) fails across the node_modules
# bind mount even within one filesystem. Never edit a file under node_modules:
# it is the store's copy, shared by every worktree.
#
# The overlay is capped at 20G and wiped on sandbox recreate, so dist, small and
# rarely read, stays on the host's disk under $BUILD_ROOT.
#
# Idempotent — safe to re-run. Run it once per sandbox boot: bind mounts do not
# survive a restart, the env vars (in /etc/sandbox-persistent.sh) do.

set -euo pipefail

# No-op outside the sandbox. This runs from a SessionStart hook, and the repo is
# shared with the macOS host, so the host's own agents fire this same hook —
# where every redirection here would be wrong. $SANDBOX_VM_ID is set only inside
# the sandbox; the Linux check guards against a stray export.
if [ -z "${SANDBOX_VM_ID:-}" ] || [ "$(uname -s)" != "Linux" ]; then
  exit 0
fi

REPO="$(cd "$(dirname "$0")/.." && pwd)"
BUILD_ROOT="${KUBETAIL_SANDBOX_BUILD_ROOT:-$REPO/.sandbox-linux}"
OVERLAY_ROOT="${KUBETAIL_SANDBOX_OVERLAY_ROOT:-/home/agent/kubetail-build}"
NODE_MODULES="$OVERLAY_ROOT/worktrees$REPO/node_modules"
PNPM_STORE="$OVERLAY_ROOT/pnpm-store"
PERSIST="/etc/sandbox-persistent.sh"
MIN_FREE_MB=2048
# CI's setup-uv takes the latest uv, so the sandbox's pin lives here.
UV_VERSION=0.13.7

echo "→ build root: $BUILD_ROOT"
echo "→ overlay root: $OVERLAY_ROOT"
# Go's caches are deliberately absent here: they default to $HOME, which is
# already sandbox-local (not shared with the host), so they need no
# redirection. Keeping them out of the repo also keeps the module cache's
# vendored *.ts fixtures out of vitest's file discovery, which — unlike
# oxlint — does not honor .gitignore.
mkdir -p "$BUILD_ROOT/dist" "$PNPM_STORE" "$NODE_MODULES" "$OVERLAY_ROOT/target"

free_mb="$(df --output=avail -BM "$OVERLAY_ROOT" | tail -1 | tr -dc 0-9)"
if [ "$free_mb" -lt "$MIN_FREE_MB" ]; then
  echo "⚠ only ${free_mb}M free on the overlay; installs and builds may fail."
  echo "  Reclaim space with 'cargo clean', 'go clean -cache', or by deleting"
  echo "  removed worktrees' node_modules under $OVERLAY_ROOT/worktrees."
fi

# --- 1. bind mounts for paths the tooling can't relocate -------------------
# A mount from any other source (an older layout) is replaced. Comparing
# device and inode tells whether $target already shows $src.
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
bind "$NODE_MODULES" "$REPO/node_modules"
bind "$BUILD_ROOT/dist" "$REPO/dist"

# --- 2. env vars for the paths that are configurable -----------------------
# Written to the persistent env file, which is sourced before every command.
# Every line this script owns is dropped first, so a re-run rewrites the values
# rather than leaving an older copy above them.
#
# pnpm takes settings from PNPM_CONFIG_* or pnpm-workspace.yaml, and the yaml is
# committed — shared with the host and CI, which keep pnpm's default layout. The
# env file is shared by every worktree in the sandbox, so the store path must not
# depend on $REPO. Unset PNPM_CONFIG_ENABLE_GLOBAL_VIRTUAL_STORE and reinstall to
# rule out the layout when something fails only in the sandbox.
#
# Go is pinned to CI's version, read from setup-environment's go-version
# default. A go.mod's go line is only a minimum, and GOTOOLCHAIN=auto switches
# only upward, so the sandbox's newer go would otherwise run every check. +auto
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
  -e '/^export CARGO_PROFILE_DEV_DEBUG=/d' \
  -e '/^export PNPM_CONFIG_STORE_DIR=/d' \
  -e '/^export PNPM_CONFIG_ENABLE_GLOBAL_VIRTUAL_STORE=/d' \
  -e '/^export PATH=.*\.cargo\/bin/d' \
  -e '/^export GOTOOLCHAIN=/d' \
  "$PERSIST"
sudo tee -a "$PERSIST" >/dev/null <<EOF
# KUBETAIL_SANDBOX_ENV — Linux build outputs kept out of the macOS host's paths, and Go pinned to CI's.
export CARGO_TARGET_DIR="$OVERLAY_ROOT/target"
export CARGO_PROFILE_DEV_DEBUG=line-tables-only
export PNPM_CONFIG_STORE_DIR="$PNPM_STORE"
export PNPM_CONFIG_ENABLE_GLOBAL_VIRTUAL_STORE=true
export GOTOOLCHAIN=go$GO_VERSION+auto
export PATH="\$HOME/.cargo/bin:\$HOME/.local/share/pnpm:\$PATH"
EOF
echo "✓ env vars written to $PERSIST (Go pinned to $GO_VERSION)"

# --- 3. a uv that runs -------------------------------------------------------
# Checks that uv runs, not the page size, so this no-ops once the image's uv
# works. The check is a function so its stderr redirect also swallows bash's
# "Aborted" report for the crashed child.
uv_runs() {
  uv --version >/dev/null 2>&1
}
if uv_runs 2>/dev/null; then
  echo "✓ uv runs"
else
  python3 -m pip install --user --quiet "uv==$UV_VERSION"
  hash -r
  if ! uv_runs 2>/dev/null; then
    echo "✗ uv $UV_VERSION installed to ~/.local/bin but does not run; is ~/.local/bin ahead of /usr/bin on PATH?" >&2
    exit 1
  fi
  echo "✓ uv $UV_VERSION installed to ~/.local/bin"
fi

# --- 4. llvm-tools for cargo-llvm-cov ----------------------------------------
# Two rustup processes installing llvm-tools at once (parallel cargo calls, or
# sessions sharing ~/.rustup) can leave it half-installed: listed in the
# toolchain's multirust-config.toml, so `component add` answers "up to date",
# or its files left on disk unregistered, so `component add` fails on a
# conflict. Healthy means llvm-profdata on disk and the component in the
# toolchain's components list. Otherwise the repair deletes what the component
# ships (rustc's own libLLVM is in the sysroot's lib/, not here), drops its
# registration and installs it again. The calls run from src-tauri/ so its
# rust-toolchain.toml picks the toolchain cargo uses.
if command -v rustup >/dev/null 2>&1; then
  (
    cd "$REPO/src-tauri"
    host="$(rustc -vV | sed -n 's/^host: //p')"
    rustlib="$(rustc --print sysroot)/lib/rustlib"
    profdata="$rustlib/$host/bin/llvm-profdata"
    if [ -x "$profdata" ] && grep -qx "llvm-tools-preview-$host" "$rustlib/components"; then
      echo "✓ llvm-tools installed"
    else
      rm -f "${rustlib:?}/$host"/bin/{llc,opt,llvm-*} \
        "${rustlib:?}/$host"/lib/libLLVM* \
        "${rustlib:?}/manifest-llvm-tools-preview-$host"
      sed -i "/^llvm-tools-preview-$host\$/d" "$rustlib/components"
      rustup component remove llvm-tools-preview >/dev/null 2>&1 || true
      rustup component add llvm-tools-preview
      if [ ! -x "$profdata" ]; then
        echo "✗ rustup installed llvm-tools but $profdata is missing" >&2
        exit 1
      fi
      echo "✓ llvm-tools installed to $(rustc --print sysroot)"
    fi
  )
fi

echo
echo "Done. Open a new shell (or 'source $PERSIST') to pick up the env vars."
