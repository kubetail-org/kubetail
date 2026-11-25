#!/bin/sh
# Installs and loads the AppArmor profiles that let Kubetail's own bwrap and pasta
# make the user namespaces Ubuntu 23.10 and 24.04 restrict. The profiles are
# written for abi 4.0, and a profile in /etc/apparmor.d that the parser cannot
# read fails apparmor.service on every boot, so they go there only where the
# parser has that abi. Where AppArmor is not running there is nothing to load,
# and a load that fails leaves Bash unsandboxed, or its commands without the
# internet, rather than the package uninstalled.
set -e

if [ "$1" = configure ] && command -v apparmor_parser >/dev/null 2>&1 \
  && [ -d /sys/kernel/security/apparmor ] && [ -e /etc/apparmor.d/abi/4.0 ]; then
  # bwrap's first: pasta's profile hands Kubetail's bwrap to kubetail_desktop_bwrap.
  for profile in kubetail-desktop-bwrap kubetail-desktop-pasta; do
    { cp "/usr/lib/kubetail-desktop/apparmor/$profile" "/etc/apparmor.d/$profile" \
      && apparmor_parser --replace --write-cache "/etc/apparmor.d/$profile"; } || true
  done
fi
