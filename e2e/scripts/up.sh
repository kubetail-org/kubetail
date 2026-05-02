#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="kubetail-e2e"
export KUBETAIL_DASHBOARD_IMAGE="kubetail-dashboard:e2e"
export KUBETAIL_CLUSTER_API_IMAGE="kubetail-cluster-api:e2e"
export KUBETAIL_CLUSTER_AGENT_IMAGE="kubetail-cluster-agent:e2e"
export KUBECONFIG="/tmp/kubetail-e2e.kubeconfig"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# shellcheck disable=SC1091
source "$SCRIPT_DIR/../.env"

# Allow flags to override .env defaults
while [ $# -gt 0 ]; do
  case $1 in
    --backend=*) BACKEND="${1#--backend=}"; shift ;;
    --backend) BACKEND="$2"; shift 2 ;;
    *) echo "Unknown argument: $1" >&2; exit 1 ;;
  esac
done

DASHBOARD_PORT=$(echo "$DASHBOARD_URL" | grep -oE '[0-9]+$')
CLUSTER_API_PORT=$(echo "$CLUSTER_API_URL" | grep -oE '[0-9]+$')
TLS_DIR="$REPO_ROOT/hack/tilt/tls"
MANIFEST="$SCRIPT_DIR/../manifests/${BACKEND}.yaml"
PID_FILE="/tmp/kubetail-e2e-pf.pid"

# Create cluster if it doesn't exist
if ! k3d cluster list 2>/dev/null | grep -q "^$CLUSTER_NAME"; then
  echo "Creating k3d cluster: $CLUSTER_NAME"
  k3d cluster create "$CLUSTER_NAME"
else
  echo "Cluster $CLUSTER_NAME already exists, reusing."
fi

# Always write kubeconfig — k3d only writes it on create, not on reuse
k3d kubeconfig get "$CLUSTER_NAME" > "$KUBECONFIG"

kubectl wait --for=condition=Ready nodes --all --timeout=60s

# Load images into cluster
echo "Loading images into cluster..."
if [ "$BACKEND" = "kubernetes-api" ]; then
  k3d image import "$KUBETAIL_DASHBOARD_IMAGE" --cluster "$CLUSTER_NAME"
else
  k3d image import \
    "$KUBETAIL_DASHBOARD_IMAGE" \
    "$KUBETAIL_CLUSTER_API_IMAGE" \
    "$KUBETAIL_CLUSTER_AGENT_IMAGE" \
    --cluster "$CLUSTER_NAME"
fi

# Apply manifest (namespace is included)
envsubst < "$MANIFEST" | kubectl apply -f -

# Create TLS secrets for kubetail-api backend (idempotent via --dry-run + apply)
if [ "$BACKEND" = "kubetail-api" ]; then
  kubectl create secret generic kubetail-ca \
    --from-file=ca.crt="$TLS_DIR/ca.crt" \
    --namespace=kubetail-system \
    --dry-run=client -o yaml | kubectl apply -f -

  kubectl create secret tls kubetail-cluster-api-tls \
    --cert="$TLS_DIR/cluster-api.crt" \
    --key="$TLS_DIR/cluster-api.key" \
    --namespace=kubetail-system \
    --dry-run=client -o yaml | kubectl apply -f -

  kubectl create secret tls kubetail-cluster-agent-tls \
    --cert="$TLS_DIR/cluster-agent.crt" \
    --key="$TLS_DIR/cluster-agent.key" \
    --namespace=kubetail-system \
    --dry-run=client -o yaml | kubectl apply -f -
fi

# Wait for workloads to be ready
kubectl rollout status deployment/kubetail-dashboard \
  --namespace=kubetail-system --timeout=120s
if [ "$BACKEND" = "kubetail-api" ]; then
  kubectl rollout status deployment/kubetail-cluster-api \
    --namespace=kubetail-system --timeout=120s
  kubectl rollout status daemonset/kubetail-cluster-agent \
    --namespace=kubetail-system --timeout=120s
fi

# Kill any existing port-forwards
if [ -f "$PID_FILE" ]; then
  while read -r OLD_PID; do
    kill "$OLD_PID" 2>/dev/null || true
  done < "$PID_FILE"
  rm "$PID_FILE"
fi

# Start port-forwards in background
kubectl port-forward \
  --namespace=kubetail-system \
  service/kubetail-dashboard \
  "${DASHBOARD_PORT}:8080" >/dev/null 2>&1 &
echo $! >> "$PID_FILE"

if [ "$BACKEND" = "kubetail-api" ]; then
  kubectl port-forward \
    --namespace=kubetail-system \
    service/kubetail-cluster-api \
    "${CLUSTER_API_PORT}:8080" >/dev/null 2>&1 &
  echo $! >> "$PID_FILE"
fi

# Wait for port-forwards to be ready
wait_for_port() {
  local port=$1
  for _ in $(seq 1 50); do
    if curl -sf "http://localhost:${port}/healthz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.2
  done
  echo "Timed out waiting for port ${port}" >&2
  return 1
}

wait_for_port "$DASHBOARD_PORT"
if [ "$BACKEND" = "kubetail-api" ]; then
  wait_for_port "$CLUSTER_API_PORT"
fi

echo ""
echo "Dashboard: http://localhost:${DASHBOARD_PORT}"
if [ "$BACKEND" = "kubetail-api" ]; then
  echo "Cluster API: http://localhost:${CLUSTER_API_PORT}"
fi
echo "Backend: $BACKEND"
echo ""
echo "Run tests:"
echo "  cd e2e && uv run pytest -v"
