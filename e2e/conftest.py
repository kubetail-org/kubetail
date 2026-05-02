import os
import subprocess
import tempfile
import time
from pathlib import Path

import pytest
import requests
from dotenv import load_dotenv

load_dotenv(Path(__file__).parent / ".env")

# In cli env the backend axis is meaningless; collapse to a single canonical
# value so tests parametrized over _backend don't run twice in cli mode.
_CLI_CANONICAL_BACKEND = "kubetail-api"


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


@pytest.fixture(scope="session", params=["cluster", "cli"])
def _env(request):
    return request.param


@pytest.fixture(scope="session", params=["kubernetes-api", "kubetail-api"])
def _backend(_env, request):
    backend = request.param
    scripts_dir = Path(__file__).parent / "scripts"
    subprocess.run(
        ["bash", str(scripts_dir / "up.sh"), f"--backend={backend}"],
        check=True,
    )
    try:
        yield backend
    finally:
        subprocess.run(["bash", str(scripts_dir / "down.sh")], check=True)


@pytest.fixture(scope="session")
def dashboard_url(_env, _backend, request):
    if _env != "cluster":
        pytest.skip("not in cluster env")
    return request.config.getoption("--dashboard-url").rstrip("/")


@pytest.fixture(scope="session")
def cluster_api_url(_env, _backend, request):
    if _env != "cluster" or _backend != "kubetail-api":
        pytest.skip("not in kubetail-api cluster env")
    return request.config.getoption("--cluster-api-url").rstrip("/")


@pytest.fixture(scope="session")
def target_url(_env, _backend, request):
    """Dashboard URL for the active env — cluster dashboard or kubetail serve."""
    if _env == "cluster":
        return request.config.getoption("--dashboard-url").rstrip("/")
    return request.getfixturevalue("serve_url")


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


@pytest.fixture(scope="session")
def serve_url(cli, request):
    port = int(os.environ.get("SERVE_PORT", 9898))
    env = os.environ.copy()
    if not Path(env.get("KUBECONFIG", Path.home() / ".kube" / "config")).exists():
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
    resp = requests.get(f"{url}/healthz")
    assert resp.status_code == 200
    assert resp.json() == {"status": "ok"}


def pytest_collection_modifyitems(config, items):
    selected, deselected = [], []
    for item in items:
        callspec = getattr(item, "callspec", None)
        params = callspec.params if callspec else {}
        env = params.get("_env")
        backend = params.get("_backend")

        drop = (
            (item.get_closest_marker("cluster") and env is not None and env != "cluster")
            or (item.get_closest_marker("cli") and env is not None and env != "cli")
            or (item.get_closest_marker("kubernetes_api") and backend is not None and backend != "kubernetes-api")
            or (item.get_closest_marker("kubetail_api") and backend is not None and backend != "kubetail-api")
            # In cli env, _backend is meaningless — keep only the canonical backend.
            or (env == "cli" and backend is not None and backend != _CLI_CANONICAL_BACKEND)
        )
        (deselected if drop else selected).append(item)

    if deselected:
        config.hook.pytest_deselected(items=deselected)
        items[:] = selected
