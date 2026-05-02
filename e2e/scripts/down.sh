#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="kubetail-e2e"
KUBECONFIG="/tmp/kubetail-e2e.kubeconfig"
PID_FILE="/tmp/kubetail-e2e-pf.pid"

# Stop port-forwards
if [ -f "$PID_FILE" ]; then
  while read -r PID; do
    if kill -0 "$PID" 2>/dev/null; then
      kill "$PID"
      echo "Stopped port-forward (PID $PID)"
    fi
  done < "$PID_FILE"
  rm "$PID_FILE"
fi

# Delete cluster
if k3d cluster list 2>/dev/null | grep -q "^$CLUSTER_NAME"; then
  echo "Deleting k3d cluster: $CLUSTER_NAME"
  KUBECONFIG="$KUBECONFIG" k3d cluster delete "$CLUSTER_NAME"
else
  echo "Cluster $CLUSTER_NAME not found, nothing to delete."
fi

# Remove dedicated kubeconfig
rm -f "$KUBECONFIG"
