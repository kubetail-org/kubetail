#!/usr/bin/env bats
# shellcheck disable=SC2031

install_sh_source() {
  KB_INSTALL_SH="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)/scripts/install.sh"
  # shellcheck source=/dev/null
  source "$KB_INSTALL_SH"
}

mock_uname_bin() {
  local d="${1:?mock bin dir}"
  mkdir -p "$d"
  cat >"$d/uname" <<'EOF'
#!/usr/bin/env sh
case "${1-}" in
  -s) printf '%s\n' "${UNAME_S:?}" ;;
  -m) printf '%s\n' "${UNAME_M:?}" ;;
  *) exit 1 ;;
esac
EOF
  chmod +x "$d/uname"
}

mock_curl_bin() {
  local d="${1:?mock bin dir}"
  mkdir -p "$d"
  cat >"$d/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
dest=""
url=""
want_dest=false
for a in "$@"; do
  if [ "$want_dest" = true ]; then
    dest="$a"
    want_dest=false
    continue
  fi
  case "$a" in
    -o | --output) want_dest=true ;;
    --output=*)
      dest="${a#--output=}"
      ;;
    http://* | https://*) url="$a" ;;
  esac
done
if [ -z "$dest" ] || [ -z "$url" ]; then
  echo "mock curl: missing output dest or url: $*" >&2
  exit 100
fi
base="${url##*/}"
base="${base%%\?*}"
case "$base" in
  SHA256SUMS)
    if command -v sha256sum >/dev/null 2>&1; then
      hash=$(printf '%s' "${KUBETAIL_TEST_PAYLOAD:?}" | sha256sum | awk '{print $1}')
    else
      hash=$(printf '%s' "${KUBETAIL_TEST_PAYLOAD:?}" | shasum -a 256 | awk '{print $1}')
    fi
    bn="${KUBETAIL_TEST_BINARY_NAME:?}"
    printf '%s  %s\n' "$hash" "$bn" >"$dest"
    ;;
  *)
    printf '%s' "${KUBETAIL_TEST_PAYLOAD:?}" >"$dest"
    ;;
esac
EOF
  chmod +x "$d/curl"
}

mock_curl_bad_checksum_bin() {
  local d="${1:?mock bin dir}"
  mkdir -p "$d"
  cat >"$d/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
dest=""
url=""
want_dest=false
for a in "$@"; do
  if [ "$want_dest" = true ]; then
    dest="$a"
    want_dest=false
    continue
  fi
  case "$a" in
    -o | --output) want_dest=true ;;
    --output=*)
      dest="${a#--output=}"
      ;;
    http://* | https://*) url="$a" ;;
  esac
done
base="${url##*/}"
base="${base%%\?*}"
case "$base" in
  SHA256SUMS)
    printf '%s  %s\n' "0000000000000000000000000000000000000000000000000000000000000000" "${KUBETAIL_TEST_BINARY_NAME:?}" >"$dest"
    ;;
  *)
    printf '%s' "${KUBETAIL_TEST_PAYLOAD:?}" >"$dest"
    ;;
esac
EOF
  chmod +x "$d/curl"
}

sha256_tool_available() {
  command -v sha256sum >/dev/null 2>&1 || command -v shasum >/dev/null 2>&1
}

is_windows() {
  case "$(uname -s)" in
    MINGW* | CYGWIN* | MSYS*) return 0 ;;
    *) return 1 ;;
  esac
}

setup() {
  unset KUBETAIL_INSTALL_DIR KUBETAIL_REPO_URL KUBETAIL_TEST_PAYLOAD KUBETAIL_TEST_BINARY_NAME || true
  install_sh_source
}

# --- kubetail_platform_supported ---

@test "kubetail_platform_supported accepts linux_amd64" {
  kubetail_platform_supported linux amd64
}

@test "kubetail_platform_supported accepts linux_arm64" {
  kubetail_platform_supported linux arm64
}

@test "kubetail_platform_supported accepts darwin_amd64" {
  kubetail_platform_supported darwin amd64
}

@test "kubetail_platform_supported accepts darwin_arm64" {
  kubetail_platform_supported darwin arm64
}

@test "kubetail_platform_supported accepts windows_amd64" {
  kubetail_platform_supported windows amd64
}

@test "kubetail_platform_supported accepts windows_arm64" {
  kubetail_platform_supported windows arm64
}

@test "kubetail_platform_supported rejects unknown pair" {
  run kubetail_platform_supported linux ppc64le
  [ "$status" -eq 1 ]
}

@test "kubetail_platform_supported rejects freebsd_amd64" {
  run kubetail_platform_supported freebsd amd64
  [ "$status" -eq 1 ]
}

# --- kubetail_normalize_os / kubetail_normalize_arch ---

@test "kubetail_normalize_os maps Linux" {
  mock_uname_bin "$BATS_TEST_TMPDIR/mockbin"
  export UNAME_S="Linux" UNAME_M="x86_64"
  run env UNAME_S="$UNAME_S" UNAME_M="$UNAME_M" PATH="$BATS_TEST_TMPDIR/mockbin:$PATH" bash -c 'source "$1"; kubetail_normalize_os' _ "$KB_INSTALL_SH"
  [ "$status" -eq 0 ]
  [ "$output" = "linux" ]
}

@test "kubetail_normalize_os maps Darwin" {
  mock_uname_bin "$BATS_TEST_TMPDIR/mockbin"
  run env UNAME_S="Darwin" UNAME_M="arm64" PATH="$BATS_TEST_TMPDIR/mockbin:$PATH" bash -c 'source "$1"; kubetail_normalize_os' _ "$KB_INSTALL_SH"
  [ "$status" -eq 0 ]
  [ "$output" = "darwin" ]
}

@test "kubetail_normalize_os maps mingw to windows" {
  mock_uname_bin "$BATS_TEST_TMPDIR/mockbin"
  run env UNAME_S="MINGW64_NT-10.0" UNAME_M="x86_64" PATH="$BATS_TEST_TMPDIR/mockbin:$PATH" bash -c 'source "$1"; kubetail_normalize_os' _ "$KB_INSTALL_SH"
  [ "$status" -eq 0 ]
  [ "$output" = "windows" ]
}

@test "kubetail_normalize_os maps cygwin to windows" {
  mock_uname_bin "$BATS_TEST_TMPDIR/mockbin"
  run env UNAME_S="CYGWIN_NT-10.0" UNAME_M="x86_64" PATH="$BATS_TEST_TMPDIR/mockbin:$PATH" bash -c 'source "$1"; kubetail_normalize_os' _ "$KB_INSTALL_SH"
  [ "$status" -eq 0 ]
  [ "$output" = "windows" ]
}

@test "kubetail_normalize_arch maps x86_64 to amd64" {
  mock_uname_bin "$BATS_TEST_TMPDIR/mockbin"
  run env UNAME_S="Linux" UNAME_M="x86_64" PATH="$BATS_TEST_TMPDIR/mockbin:$PATH" bash -c 'source "$1"; kubetail_normalize_arch' _ "$KB_INSTALL_SH"
  [ "$status" -eq 0 ]
  [ "$output" = "amd64" ]
}

@test "kubetail_normalize_arch maps aarch64 to arm64" {
  mock_uname_bin "$BATS_TEST_TMPDIR/mockbin"
  run env UNAME_S="Linux" UNAME_M="aarch64" PATH="$BATS_TEST_TMPDIR/mockbin:$PATH" bash -c 'source "$1"; kubetail_normalize_arch' _ "$KB_INSTALL_SH"
  [ "$status" -eq 0 ]
  [ "$output" = "arm64" ]
}

@test "kubetail_normalize_arch passes through unknown machine" {
  mock_uname_bin "$BATS_TEST_TMPDIR/mockbin"
  run env UNAME_S="Linux" UNAME_M="ppc64le" PATH="$BATS_TEST_TMPDIR/mockbin:$PATH" bash -c 'source "$1"; kubetail_normalize_arch' _ "$KB_INSTALL_SH"
  [ "$status" -eq 0 ]
  [ "$output" = "ppc64le" ]
}

# --- kubetail_expected_checksum ---

@test "kubetail_expected_checksum picks matching line" {
  local sums="$BATS_TEST_TMPDIR/sums"
  printf '%s\n' \
    'aaa1111111111111111111111111111111111111111111111111111111111111  other-bin' \
    'bbb2222222222222222222222222222222222222222222222222222222222222  kubetail-linux-amd64' \
    'ccc3333333333333333333333333333333333333333333333333333333333333  kubetail-linux-arm64' >"$sums"
  run kubetail_expected_checksum "$sums" "kubetail-linux-amd64"
  [ "$status" -eq 0 ]
  [ "$output" = "bbb2222222222222222222222222222222222222222222222222222222222222" ]
}

# --- kubetail_sha256_of_file ---

@test "kubetail_sha256_of_file returns hex digest" {
  if ! sha256_tool_available; then
    skip "sha256sum and shasum unavailable"
  fi
  local f="$BATS_TEST_TMPDIR/payload"
  printf 'x' >"$f"
  run kubetail_sha256_of_file "$f"
  [ "$status" -eq 0 ]
  [ "${#output}" -eq 64 ]
}

# --- kubetail_verify_checksum ---

@test "kubetail_verify_checksum succeeds for matching file" {
  if ! sha256_tool_available; then
    skip "sha256sum and shasum unavailable"
  fi
  local binf="$BATS_TEST_TMPDIR/binfile"
  local sums="$BATS_TEST_TMPDIR/sums"
  printf 'payload-bytes' >"$binf"
  local h
  h=$(kubetail_sha256_of_file "$binf")
  printf '%s  %s\n' "$h" "kubetail-linux-amd64" >"$sums"
  kubetail_verify_checksum "$binf" "$sums" "kubetail-linux-amd64"
}

@test "kubetail_verify_checksum fails when sums missing entry" {
  if ! sha256_tool_available; then
    skip "sha256sum and shasum unavailable"
  fi
  local binf="$BATS_TEST_TMPDIR/binfile"
  local sums="$BATS_TEST_TMPDIR/sums"
  printf 'payload-bytes' >"$binf"
  printf '%s  %s\n' "0000000000000000000000000000000000000000000000000000000000000000" "other-name" >"$sums"
  run kubetail_verify_checksum "$binf" "$sums" "kubetail-linux-amd64"
  [ "$status" -eq 1 ]
}

@test "kubetail_verify_checksum fails on hash mismatch" {
  if ! sha256_tool_available; then
    skip "sha256sum and shasum unavailable"
  fi
  local binf="$BATS_TEST_TMPDIR/binfile"
  local sums="$BATS_TEST_TMPDIR/sums"
  printf 'payload-bytes' >"$binf"
  printf '%s  %s\n' "0000000000000000000000000000000000000000000000000000000000000000" "kubetail-linux-amd64" >"$sums"
  run kubetail_verify_checksum "$binf" "$sums" "kubetail-linux-amd64"
  [ "$status" -eq 1 ]
}

# --- kubetail_download ---

@test "kubetail_download writes curl -o destination" {
  mock_curl_bin "$BATS_TEST_TMPDIR/mockbin"
  export PATH="$BATS_TEST_TMPDIR/mockbin:$PATH"
  export KUBETAIL_TEST_PAYLOAD='abc' KUBETAIL_TEST_BINARY_NAME='ignored'
  local out="$BATS_TEST_TMPDIR/out"
  kubetail_download "https://example.invalid/releases/latest/download/kubetail-linux-amd64" "$out"
  [ "$(cat "$out")" = 'abc' ]
}

# --- kubetail_install_binary ---

@test "kubetail_install_binary windows installs to HOME/bin" {
  export HOME="$BATS_TEST_TMPDIR/home"
  local src="$BATS_TEST_TMPDIR/kubetail.bin"
  printf 'e' >"$src"
  kubetail_install_binary windows "$src"
  [ -x "$HOME/bin/kubetail.exe" ]
  [ "$(cat "$HOME/bin/kubetail.exe")" = 'e' ]
}

@test "kubetail_install_binary unix uses KUBETAIL_INSTALL_DIR" {
  if is_windows; then
    skip "unix install path not applicable on Windows"
  fi
  local inst="$BATS_TEST_TMPDIR/instdir"
  export KUBETAIL_INSTALL_DIR="$inst"
  local src="$BATS_TEST_TMPDIR/kubetail.bin"
  printf 'z' >"$src"
  kubetail_install_binary linux "$src"
  [ -x "$inst/kubetail" ]
  [ "$(cat "$inst/kubetail")" = 'z' ]
}

# --- kubetail_main ---

@test "kubetail_main installs with mocked curl" {
  if ! sha256_tool_available; then
    skip "sha256sum and shasum unavailable"
  fi
  local on_windows=false
  is_windows && on_windows=true
  mock_curl_bin "$BATS_TEST_TMPDIR/mockbin"
  mock_uname_bin "$BATS_TEST_TMPDIR/mockuname"
  export PATH="$BATS_TEST_TMPDIR/mockbin:$BATS_TEST_TMPDIR/mockuname:$PATH"
  export KUBETAIL_REPO_URL="https://example.invalid"
  export KUBETAIL_TEST_PAYLOAD='fake-release-payload'
  export TMPDIR="$BATS_TEST_TMPDIR"
  if $on_windows; then
    export UNAME_S="MINGW64_NT-10.0" UNAME_M="x86_64"
    export KUBETAIL_TEST_BINARY_NAME="kubetail-windows-amd64"
    export HOME="$BATS_TEST_TMPDIR/home"
    run kubetail_main
    [ "$status" -eq 0 ]
    [ -x "$HOME/bin/kubetail.exe" ]
    [ "$(cat "$HOME/bin/kubetail.exe")" = 'fake-release-payload' ]
  else
    export UNAME_S="Linux" UNAME_M="x86_64"
    export KUBETAIL_TEST_BINARY_NAME="kubetail-linux-amd64"
    export KUBETAIL_INSTALL_DIR="$BATS_TEST_TMPDIR/instdir"
    run kubetail_main
    [ "$status" -eq 0 ]
    [ -x "$KUBETAIL_INSTALL_DIR/kubetail" ]
    [ "$(cat "$KUBETAIL_INSTALL_DIR/kubetail")" = 'fake-release-payload' ]
  fi
}

@test "kubetail_main fails on unsupported platform" {
  mock_uname_bin "$BATS_TEST_TMPDIR/mockuname"
  export PATH="$BATS_TEST_TMPDIR/mockuname:$PATH"
  export UNAME_S="FreeBSD" UNAME_M="amd64"
  run kubetail_main
  [ "$status" -eq 1 ]
  [[ "$output" == *Unsupported* ]]
}

@test "kubetail_main removes artifacts on checksum failure" {
  mock_curl_bad_checksum_bin "$BATS_TEST_TMPDIR/mockbin"
  mock_uname_bin "$BATS_TEST_TMPDIR/mockuname"
  export PATH="$BATS_TEST_TMPDIR/mockbin:$BATS_TEST_TMPDIR/mockuname:$PATH"
  export UNAME_S="Linux" UNAME_M="x86_64"
  export KUBETAIL_REPO_URL="https://example.invalid"
  export KUBETAIL_TEST_PAYLOAD='payload'
  export KUBETAIL_TEST_BINARY_NAME="kubetail-linux-amd64"
  export TMPDIR="$BATS_TEST_TMPDIR"
  run kubetail_main
  [ "$status" -eq 1 ]
  shopt -s nullglob
  leftovers=("$TMPDIR"/kubetail-linux-amd64-*)
  [ ${#leftovers[@]} -eq 0 ]
}
