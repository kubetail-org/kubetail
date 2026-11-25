#!/usr/bin/env bash
# Runs what the sidecar's probe runs through Kubetail's own bwrap and pasta, as
# installed by the .deb: a run with no network, then one with the internet
# that reaches an address outside. Run it as an ordinary user on a stock
# Ubuntu 24.04, where root is not restricted and AppArmor's profiles are what
# let either run.
#
# Usage: scripts/check-sandbox-chain.sh
set -euo pipefail

sidecar=/usr/bin/kubetail-sidecar
bwrap=/usr/lib/kubetail-desktop/bwrap
pasta=/usr/lib/kubetail-desktop/pasta

# What every run binds, after its own namespaces.
mounts=(
  --unshare-pid --unshare-ipc --unshare-uts --unshare-cgroup-try
  --die-with-parent --new-session --as-pid-1
  --proc /proc --dev /dev --tmpfs /tmp
  --ro-bind /usr /usr --symlink usr/bin /bin --symlink usr/sbin /sbin
  --symlink usr/lib /lib --symlink usr/lib64 /lib64 --ro-bind /etc /etc --remount-ro /
)

# /bin/true is a program, so the shell execs once more under the child profile.
echo "→ a run with no network"
"$bwrap" --unshare-net "${mounts[@]}" \
  -- "$sidecar" sandbox-init -- "$sidecar" sandbox-shell -- /bin/sh -c /bin/true

# pasta makes the network namespace and starts bwrap in it as its uid 0, so
# bwrap makes the run's own user namespace as the user.
echo "→ a run with the internet"
"$sidecar" sandbox-pasta -- "$pasta" --config-net --quiet --no-map-gw \
  -t none -u none -T none -U none -- \
  "$bwrap" --unshare-user --uid "$(id -u)" --gid "$(id -g)" --cap-drop ALL "${mounts[@]}" \
  -- "$sidecar" sandbox-init --stderr-on-stdin -- "$sidecar" sandbox-shell -- \
  /bin/sh -c 'curl -sS -o /dev/null --max-time 20 https://1.1.1.1/'

echo "✓ both chains ran"
