#!/usr/bin/env bash

set -euo pipefail

# Overridable for tests.
: "${KUBETAIL_REPO_URL:=https://github.com/kubetail-org/kubetail}"
# When set, non-Windows installs use this directory instead of /usr/local/bin.
: "${KUBETAIL_INSTALL_DIR:=}"
EXECUTABLE_NAME="kubetail"

# --- platform ---

kubetail_normalize_os() {
  local raw
  raw=$(uname -s | tr '[:upper:]' '[:lower:]')
  case "$raw" in
    mingw* | cygwin*) echo "windows" ;;
    linux) echo "linux" ;;
    darwin) echo "darwin" ;;
    *) echo "$raw" ;;
  esac
}

kubetail_normalize_arch() {
  case "$(uname -m)" in
    x86_64) echo "amd64" ;;
    arm64 | aarch64) echo "arm64" ;;
    *) uname -m ;;
  esac
}

kubetail_platform_supported() {
  local os="$1" arch="$2" pair="${1}_${2}"
  case "$pair" in
    linux_amd64 | linux_arm64 | darwin_amd64 | darwin_arm64 | windows_amd64 | windows_arm64) return 0 ;;
    *) return 1 ;;
  esac
}

# --- download / checksum ---

kubetail_download() {
  local url="$1" dest="$2"
  curl --silent --show-error --fail --location --output "$dest" "$url"
}

kubetail_sha256_of_file() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
  else
    echo "Neither sha256sum nor shasum is available." >&2
    return 1
  fi
}

kubetail_expected_checksum() {
  local sums_file="$1" binary_name="$2"
  awk -v name="$binary_name" '$2 == name {print $1; exit}' "$sums_file"
}

kubetail_verify_checksum() {
  local binary_path="$1" sums_path="$2" binary_basename="$3"
  local expected actual
  expected=$(kubetail_expected_checksum "$sums_path" "$binary_basename")
  if [ -z "$expected" ]; then
    echo "Could not find checksum for $binary_basename in SHA256SUMS." >&2
    return 1
  fi
  actual=$(kubetail_sha256_of_file "$binary_path")
  if [ "$expected" != "$actual" ]; then
    echo "Checksum mismatch for $binary_basename." >&2
    return 1
  fi
}

# --- install ---

kubetail_install_binary() {
  local os="$1" binary_path="$2"

  chmod +x "$binary_path"

  if [ "$os" = "windows" ]; then
    local install_dir="$HOME/bin"
    local dest="$install_dir/${EXECUTABLE_NAME}.exe"
    mkdir -p "$install_dir"
    mv "$binary_path" "$dest"
  else
    if [ -n "${KUBETAIL_INSTALL_DIR:-}" ]; then
      mkdir -p "$KUBETAIL_INSTALL_DIR"
      mv "$binary_path" "${KUBETAIL_INSTALL_DIR}/${EXECUTABLE_NAME}"
    else
      local dest="/usr/local/bin/${EXECUTABLE_NAME}"
      if [ "$(id -u)" -ne 0 ]; then
        sudo mv "$binary_path" "$dest"
      else
        mv "$binary_path" "$dest"
      fi
    fi
  fi
}

kubetail_main() {
  local os arch binary_name binary_url sums_url sums_file binary_path
  # Not local: the EXIT trap references tmp after the function returns.
  tmp=""
  trap 'rm -rf "$tmp"' EXIT

  os=$(kubetail_normalize_os)
  arch=$(kubetail_normalize_arch)

  if ! kubetail_platform_supported "$os" "$arch"; then
    echo "Unsupported OS (\"$os\") or architecture (\"$arch\")." >&2
    echo "Please report to ${KUBETAIL_REPO_URL}/issues" >&2
    return 1
  fi

  binary_name="${EXECUTABLE_NAME}-${os}-${arch}"
  binary_url="${KUBETAIL_REPO_URL}/releases/latest/download/${binary_name}"
  sums_url="${KUBETAIL_REPO_URL}/releases/latest/download/SHA256SUMS"
  tmp=$(mktemp -d)
  sums_file="${tmp}/SHA256SUMS"
  binary_path="${tmp}/${binary_name}"

  printf "⬇️  Downloading kubetail for %s/%s... " "$os" "$arch"
  kubetail_download "$binary_url" "$binary_path"
  echo "✅ success"

  printf "🔍 Verifying checksum... "
  kubetail_download "$sums_url" "$sums_file"
  if ! kubetail_verify_checksum "$binary_path" "$sums_file" "$binary_name"; then
    return 1
  fi
  echo "✅ success"

  if [ "$os" = "windows" ]; then
    printf "➡️  Moving to %s/bin/%s.exe... " "$HOME" "$EXECUTABLE_NAME"
  else
    if [ -n "${KUBETAIL_INSTALL_DIR:-}" ]; then
      printf "➡️  Moving to %s/%s... " "$KUBETAIL_INSTALL_DIR" "$EXECUTABLE_NAME"
    else
      printf "➡️  Moving to /usr/local/bin/%s... " "$EXECUTABLE_NAME"
    fi
  fi
  kubetail_install_binary "$os" "$binary_path"
  echo "✅ success"

  if [ "$os" = "windows" ]; then
    echo "🎉 Installation complete. Please add $HOME/bin to your PATH if necessary."
  else
    echo "🎉 Installation complete"
  fi

  echo "🚀 Have fun tailing your Kubernetes logs!"
}

if [[ "${BASH_SOURCE:-$0}" == "${0}" ]]; then
  kubetail_main "$@"
fi
