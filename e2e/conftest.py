import os
import subprocess
import tempfile
import time
from pathlib import Path

import pytest
import requests
import urllib3
from dotenv import load_dotenv

# e2e cluster-api uses a self-signed cert; suppress the noisy warning.
urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

load_dotenv(Path(__file__).parent / ".env")


def pytest_addoption(parser):
    parser.addoption(
        "--dashboard-url",
        default=os.environ.get("DASHBOARD_URL", "http://localhost:9999"),
        help="Base URL of the dashboard server under test",
    )
    parser.addoption(
        "--cluster-api-url",
        default=os.environ.get("CLUSTER_API_URL", "http://localhost:9998"),
        help="Base URL of the kubetail cluster-api server under test",
    )
    parser.addoption(
        "--cli",
        default=os.environ.get("KUBETAIL_CLI"),
        help="Path to the kubetail binary",
    )


@pytest.fixture(scope="session")
def _cluster_ready(request):
    """Precondition: e2e cluster is up and the Dashboard answers /healthz.

    pytest no longer owns cluster lifecycle — run e2e/scripts/up.sh first
    (the Makefile test-e2e target does this automatically).
    """
    if not Path(_E2E_KUBECONFIG).exists():
        pytest.fail(
            f"e2e cluster not running — run e2e/scripts/up.sh first "
            f"(missing kubeconfig {_E2E_KUBECONFIG})"
        )
    url = request.config.getoption("--dashboard-url").rstrip("/")
    try:
        resp = requests.get(f"{url}/healthz", timeout=2, verify=False)
        resp.raise_for_status()
    except Exception as e:
        pytest.fail(
            f"e2e dashboard not reachable at {url} — run e2e/scripts/up.sh "
            f"first ({e})"
        )


@pytest.fixture(scope="session")
def dashboard_url(_cluster_ready, request):
    return request.config.getoption("--dashboard-url").rstrip("/")


@pytest.fixture(scope="session")
def cluster_api_url(_cluster_ready, request):
    return request.config.getoption("--cluster-api-url").rstrip("/")


@pytest.fixture(scope="session")
def cli(request):
    path = request.config.getoption("--cli")
    if path is None:
        pytest.skip("--cli not provided")
    return path


_DUMMY_KUBECONFIG = """\
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://localhost:6443
  name: fake
contexts:
- context:
    cluster: fake
    user: fake
  name: fake
current-context: fake
users:
- name: fake
  user: {}
"""


# Kubeconfig written by scripts/up.sh.
_E2E_KUBECONFIG = "/tmp/kubetail-e2e.kubeconfig"


@pytest.fixture(scope="session")
def serve_url(cli):
    port = int(os.environ.get("SERVE_PORT", 9898))
    env = os.environ.copy()
    if Path(_E2E_KUBECONFIG).exists():
        env["KUBECONFIG"] = _E2E_KUBECONFIG
    elif not Path(env.get("KUBECONFIG", Path.home() / ".kube" / "config")).exists():
        tmp = tempfile.NamedTemporaryFile(
            mode="w", suffix=".kubeconfig", delete=False
        )
        tmp.write(_DUMMY_KUBECONFIG)
        tmp.flush()
        env["KUBECONFIG"] = tmp.name
    proc = subprocess.Popen(
        [cli, "serve", "--port", str(port), "--skip-open"],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        env=env,
    )
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        try:
            requests.get(f"http://localhost:{port}/healthz", timeout=1)
            break
        except requests.ConnectionError:
            time.sleep(0.2)
    yield f"http://localhost:{port}"
    proc.terminate()
    proc.wait()


def assert_healthz(url):
    resp = requests.get(f"{url}/healthz", verify=False)
    assert resp.status_code == 200
    assert resp.json() == {"status": "ok"}


@pytest.fixture(scope="session")
def restricted_sa_tokens(_cluster_ready):
    """Apply the namespace-scoped RBAC manifest and yield SA bearer tokens.

    Returns a dict mapping namespace -> token, where each SA's RBAC grants
    pod/log access in that namespace only. Shared by the cli and cluster
    namespace-rbac tests so the cluster only pays the manifest-apply cost
    once.
    """
    from _namespace_rbac import (
        BASELINE_CLUSTER_ROLE,
        GROUP_NS,
        GROUP_SA_NAME,
        SA1_NAME,
        SA1_NS,
        SA2_NAME,
        SA2_NS,
        kubectl,
        rendered_manifest,
    )

    kubectl("apply", "-f", "-", input=rendered_manifest())
    try:
        tokens = {}
        for ns, sa in [
            (SA1_NS, SA1_NAME),
            (SA2_NS, SA2_NAME),
            (GROUP_NS, GROUP_SA_NAME),
        ]:
            tok = kubectl(
                "create", "token", sa, "-n", ns, "--duration", "1h"
            ).stdout.strip()
            assert tok, f"empty token for {ns}/{sa}"
            tokens[ns] = tok
        yield tokens
    finally:
        # Best-effort cleanup; don't fail teardown if the cluster is gone.
        for ns in (SA1_NS, SA2_NS, GROUP_NS):
            kubectl("delete", "namespace", ns, "--wait=false", check=False)
        kubectl(
            "delete", "clusterrolebinding", BASELINE_CLUSTER_ROLE,
            "--ignore-not-found", check=False,
        )
        kubectl(
            "delete", "clusterrole", BASELINE_CLUSTER_ROLE,
            "--ignore-not-found", check=False,
        )
