#!/usr/bin/env bats

# Each test runs a copy of the script in a scratch repo, with sudo, uname, uv,
# python3, rustc and rustup stubbed on PATH, so nothing touches the real repo,
# mounts, toolchain or /etc/sandbox-persistent.sh.

setup() {
  SRC_REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"
  REPO="$BATS_TEST_TMPDIR/repo"
  STUBS="$BATS_TEST_TMPDIR/stubs"
  LOG="$BATS_TEST_TMPDIR/calls.log"
  SYSROOT="$BATS_TEST_TMPDIR/sysroot"
  RUSTLIB="$SYSROOT/lib/rustlib"
  PROFDATA="$RUSTLIB/x86_64-unknown-linux-gnu/bin/llvm-profdata"
  mkdir -p "$REPO/scripts" "$REPO/.github/actions/setup-environment" "$REPO/cluster/agent" \
    "$STUBS" "$(dirname "$PROFDATA")"
  cp "$SRC_REPO/scripts/sandbox-dev-setup.sh" "$REPO/scripts/"
  cp "$SRC_REPO/.github/actions/setup-environment/action.yml" "$REPO/.github/actions/setup-environment/"
  : >"$LOG"

  # sudo runs its command, except mount and umount, which it only logs.
  cat >"$STUBS/sudo" <<EOF
#!/usr/bin/env bash
case "\$1" in
  mount | umount) echo "sudo \$*" >>"$LOG" ;;
  *) "\$@" ;;
esac
EOF
  cat >"$STUBS/uname" <<'EOF'
#!/usr/bin/env sh
printf '%s\n' "${UNAME_S:?}"
EOF
  # uv exits UV_EXIT until python3 installs one that runs, which it does when
  # UV_INSTALL_RUNS is set.
  cat >"$STUBS/uv" <<EOF
#!/usr/bin/env sh
[ -e "$BATS_TEST_TMPDIR/uv-installed" ] && exit 0
exit "\${UV_EXIT:-0}"
EOF
  cat >"$STUBS/python3" <<EOF
#!/usr/bin/env sh
echo "python3 \$*" >>"$LOG"
[ -z "\${UV_INSTALL_RUNS:-}" ] || touch "$BATS_TEST_TMPDIR/uv-installed"
EOF
  cat >"$STUBS/rustc" <<EOF
#!/usr/bin/env sh
case "\$*" in
  -vV) echo "host: x86_64-unknown-linux-gnu" ;;
  "--print sysroot") echo "$SYSROOT" ;;
esac
EOF
  # rustup logs each call, and component add installs llvm-profdata.
  cat >"$STUBS/rustup" <<EOF
#!/usr/bin/env sh
echo "rustup \$*" >>"$LOG"
if [ "\$*" = "component add llvm-tools-preview" ]; then
  printf '#!/bin/sh\n' >"$PROFDATA" && chmod +x "$PROFDATA"
fi
EOF
  # llvm-tools starts healthy.
  printf '#!/bin/sh\n' >"$PROFDATA"
  chmod +x "$PROFDATA"
  echo "llvm-tools-preview-x86_64-unknown-linux-gnu" >"$RUSTLIB/components"
  # Present so the script does not install it; sudo intercepts every call.
  printf '#!/usr/bin/env sh\nexit 1\n' >"$STUBS/mount"
  chmod +x "$STUBS"/*

  export PATH="$STUBS:$PATH"
  export KUBETAIL_SANDBOX_OVERLAY_ROOT="$BATS_TEST_TMPDIR/overlay"
  export KUBETAIL_SANDBOX_PERSIST="$BATS_TEST_TMPDIR/persist.sh"
  export SANDBOX_VM_ID=test
  export UNAME_S=Linux
}

run_script() {
  run bash "$REPO/scripts/sandbox-dev-setup.sh"
}

require_linux() {
  # The setup path calls GNU stat and df.
  case "$OSTYPE" in linux*) ;; *) skip "needs Linux" ;; esac
}

@test "no-op outside the sandbox" {
  unset SANDBOX_VM_ID
  run_script
  [ "$status" -eq 0 ]
  [ -z "$output" ]
  [ ! -e "$KUBETAIL_SANDBOX_PERSIST" ]
  [ ! -e "$KUBETAIL_SANDBOX_OVERLAY_ROOT" ]
}

@test "no-op on macOS" {
  UNAME_S=Darwin run_script
  [ "$status" -eq 0 ]
  [ -z "$output" ]
  [ ! -e "$KUBETAIL_SANDBOX_PERSIST" ]
  [ ! -e "$KUBETAIL_SANDBOX_OVERLAY_ROOT" ]
}

@test "bind-mounts node_modules and dist from Linux-only directories" {
  require_linux
  run_script
  [ "$status" -eq 0 ]
  grep -qx "sudo mount --bind $KUBETAIL_SANDBOX_OVERLAY_ROOT/worktrees$REPO/apps/web/node_modules $REPO/apps/web/node_modules" "$LOG"
  grep -qx "sudo mount --bind $REPO/.sandbox-linux/dist $REPO/apps/web/dist" "$LOG"
  grep -qx "sudo mount --bind $KUBETAIL_SANDBOX_OVERLAY_ROOT/worktrees$REPO/apps/desktop/node_modules $REPO/apps/desktop/node_modules" "$LOG"
  grep -qx "sudo mount --bind $REPO/.sandbox-linux/desktop-dist $REPO/apps/desktop/dist" "$LOG"
}

@test "writes each env var once across re-runs" {
  require_linux
  echo 'export KEEP=1' >"$KUBETAIL_SANDBOX_PERSIST"
  run_script
  [ "$status" -eq 0 ]
  run_script
  [ "$status" -eq 0 ]
  for var in CARGO_TARGET_DIR CARGO_PROFILE_DEV_DEBUG PNPM_CONFIG_STORE_DIR PNPM_CONFIG_ENABLE_GLOBAL_VIRTUAL_STORE GOTOOLCHAIN; do
    [ "$(grep -c "^export $var=" "$KUBETAIL_SANDBOX_PERSIST")" -eq 1 ]
  done
  [ "$(grep -c KUBETAIL_SANDBOX_ENV "$KUBETAIL_SANDBOX_PERSIST")" -eq 1 ]
  grep -qx 'export KEEP=1' "$KUBETAIL_SANDBOX_PERSIST"
}

@test "pins Go to CI's version" {
  require_linux
  run_script
  [ "$status" -eq 0 ]
  grep -Eqx 'export GOTOOLCHAIN=go[0-9]+\.[0-9]+(\.[0-9]+)?\+auto' "$KUBETAIL_SANDBOX_PERSIST"
}

@test "leaves a working uv alone" {
  require_linux
  run_script
  [ "$status" -eq 0 ]
  run grep -q '^python3' "$LOG"
  [ "$status" -ne 0 ]
}

@test "installs the pinned uv when uv does not run" {
  require_linux
  UV_EXIT=134 UV_INSTALL_RUNS=1 run_script
  [ "$status" -eq 0 ]
  grep -Eqx 'python3 -m pip install --user --quiet uv==[0-9.]+' "$LOG"
}

@test "fails when the installed uv does not run either" {
  require_linux
  UV_EXIT=134 run_script
  [ "$status" -eq 1 ]
  [[ "$output" == *"does not run; is ~/.local/bin ahead of /usr/bin on PATH?"* ]]
}

@test "leaves a healthy llvm-tools alone" {
  require_linux
  run_script
  [ "$status" -eq 0 ]
  [[ "$output" == *"✓ llvm-tools installed"* ]]
  run grep -q '^rustup component add' "$LOG"
  [ "$status" -ne 0 ]
}

@test "reinstalls an llvm-tools that is registered with no files" {
  require_linux
  rm "$PROFDATA"
  run_script
  [ "$status" -eq 0 ]
  grep -qx 'rustup component add llvm-tools-preview' "$LOG"
  [ -x "$PROFDATA" ]
}

@test "reinstalls an llvm-tools whose files are not registered" {
  require_linux
  : >"$RUSTLIB/components"
  touch "$RUSTLIB/manifest-llvm-tools-preview-x86_64-unknown-linux-gnu"
  run_script
  [ "$status" -eq 0 ]
  grep -qx 'rustup component remove llvm-tools-preview' "$LOG"
  grep -qx 'rustup component add llvm-tools-preview' "$LOG"
  [ ! -e "$RUSTLIB/manifest-llvm-tools-preview-x86_64-unknown-linux-gnu" ]
}
