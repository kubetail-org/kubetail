"""End-to-end test that a Dashboard session survives a pod restart.

Without `session.key-pairs` the Dashboard keeps its session keys in the
Secret `kubetail-dashboard-session-keys` in its own namespace. This module
deploys a sibling `kubetail-dashboard-restart` in `auth-mode: token` with no
key pairs, logs in, restarts it, and checks the same cookie still carries
the login.
"""

import contextlib
import subprocess
import time

import pytest
import requests

from _namespace_rbac import KUBECONFIG, free_port, kubectl

_NS = "kubetail-e2e"
_NAME = "kubetail-dashboard-restart"


def _manifest(image: str) -> str:
    return f"""\
apiVersion: v1
kind: ConfigMap
metadata:
  name: {_NAME}
  namespace: {_NS}
data:
  config.yaml: |
    addr: :8080
    auth-mode: token
    environment: cluster
    gin-mode: release
    session:
      cookie:
        name: kubetail_dashboard_restart_session
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {_NAME}
  namespace: {_NS}
spec:
  replicas: 1
  selector:
    matchLabels: {{app: {_NAME}}}
  template:
    metadata:
      labels: {{app: {_NAME}}}
    spec:
      serviceAccountName: kubetail-dashboard
      containers:
        - name: dashboard
          image: {image}
          imagePullPolicy: Never
          args: [--config=/etc/kubetail/config.yaml]
          ports:
            - {{name: http, containerPort: 8080}}
          volumeMounts:
            - {{name: config, mountPath: /etc/kubetail, readOnly: true}}
          readinessProbe:
            httpGet: {{path: /healthz, port: http}}
            initialDelaySeconds: 2
            periodSeconds: 2
      volumes:
        - name: config
          configMap: {{name: {_NAME}}}
"""


def _dashboard_image() -> str:
    out = kubectl(
        "-n",
        _NS,
        "get",
        "deployment",
        "kubetail-dashboard",
        "-o",
        "jsonpath={.spec.template.spec.containers[0].image}",
    ).stdout.strip()
    assert out, "could not resolve dashboard image"
    return out


def _rollout_status():
    kubectl("-n", _NS, "rollout", "status", f"deployment/{_NAME}", "--timeout=120s")


@contextlib.contextmanager
def _port_forward():
    """Forward a local port to the deployment's current pod."""
    local_port = free_port()
    pf = subprocess.Popen(
        [
            "kubectl",
            f"--kubeconfig={KUBECONFIG}",
            "-n",
            _NS,
            "port-forward",
            f"deployment/{_NAME}",
            f"{local_port}:8080",
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    try:
        base = f"http://localhost:{local_port}"
        deadline = time.monotonic() + 15
        while True:
            try:
                if requests.get(f"{base}/healthz", timeout=1).status_code == 200:
                    break
            except requests.RequestException:
                pass
            if time.monotonic() > deadline:
                raise RuntimeError("restart dashboard never became reachable")
            time.sleep(0.2)
        yield base
    finally:
        pf.terminate()
        try:
            pf.wait(timeout=5)
        except subprocess.TimeoutExpired:
            pf.kill()
            pf.wait()


@pytest.fixture(scope="module")
def restart_dashboard(cluster_api_url):
    # cluster_api_url anchors this fixture under the kubetail-e2e cluster env.
    del cluster_api_url

    kubectl("apply", "-f", "-", input=_manifest(_dashboard_image()))
    try:
        _rollout_status()
        yield
    finally:
        for kind in ("deployment", "configmap"):
            kubectl(
                "-n",
                _NS,
                "delete",
                kind,
                _NAME,
                "--ignore-not-found",
                "--wait=false",
                check=False,
            )


@pytest.fixture(scope="module")
def dashboard_sa_token():
    out = kubectl(
        "create", "token", "kubetail-dashboard", "-n", _NS, "--duration", "1h"
    ).stdout.strip()
    assert out, "empty SA token"
    return out


def _session_user(s, base):
    r = s.get(f"{base}/api/auth/session", timeout=10)
    assert r.status_code == 200, r.text
    return r.json()["user"]


def test_session_survives_pod_restart(restart_dashboard, dashboard_sa_token):
    del restart_dashboard
    s = requests.Session()

    with _port_forward() as base:
        r = s.get(f"{base}/api/auth/session", timeout=10)
        assert r.status_code == 200, r.text
        csrf = r.headers["X-CSRF-Token"]
        r = s.post(
            f"{base}/api/auth/login",
            headers={"Sec-Fetch-Site": "same-origin", "X-CSRF-Token": csrf},
            data={"token": dashboard_sa_token},
            timeout=10,
        )
        assert r.status_code == 204, r.text
        user = _session_user(s, base)
        assert user, "login did not stick"

    kubectl("-n", _NS, "rollout", "restart", f"deployment/{_NAME}")
    _rollout_status()

    with _port_forward() as base:
        assert _session_user(s, base) == user
