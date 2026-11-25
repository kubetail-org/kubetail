#!/bin/sh
# Unloads and deletes the profiles postinst.sh installed, on removal alone: an
# upgrade's postinst puts the new ones in their place.
set -e

if [ "$1" = remove ]; then
  for profile in kubetail-desktop-pasta kubetail-desktop-bwrap; do
    [ -e "/etc/apparmor.d/$profile" ] || continue
    if command -v apparmor_parser >/dev/null 2>&1 && [ -d /sys/kernel/security/apparmor ]; then
      apparmor_parser --remove "/etc/apparmor.d/$profile" || true
    fi
    rm -f "/etc/apparmor.d/$profile"
  done
fi
