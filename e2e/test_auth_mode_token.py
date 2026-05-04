"""End-to-end tests for the in-cluster dashboard's `auth-mode: token`.

The standard cluster fixture deploys the dashboard with `auth-mode: auto`,
so this module spins up a sibling `kubetail-dashboard-token` configured
with `auth-mode: token` and tears it down at module exit. It reuses the
existing `kubetail-dashboard` ServiceAccount so RBAC for the dashboard's
own SA-fallback path stays identical.
"""

import asyncio
import subprocess
import time

import pytest
import requests
import websockets
import websockets.exceptions

from _namespace_rbac import (
    KUBECONFIG,
    SA1_NS,
    SA2_NS,
    free_port,
    has_authz_denial,
    kubectl,
    session,
)


_NS = "kubetail-system"
_NAME = "kubetail-dashboard-token"
_WS_SUBPROTOCOL = "graphql-transport-ws"

_DASHBOARD_LOG_FETCH = (
    "query Q($sources:[String!]!){"
    "logRecordsFetch(sources:$sources, mode:TAIL, limit:1)"
    "{records{message}}}"
)
_CLUSTER_API_LOG_METADATA = (
    "query Q($namespace:String){"
    "logMetadataList(namespace:$namespace)"
    "{items{id}}}"
)

_PROTECTED_PATHS = pytest.mark.parametrize(
    "path", ["/graphql", "/cluster-api-proxy/graphql"],
)

_FORWARDING_CASES = pytest.mark.parametrize(
    "namespace,expect_denial",
    [(SA2_NS, True), (SA1_NS, False)],
    ids=["foreign-ns-denied", "own-ns-allowed"],
)


def _manifest(image: str) -> str:
    return f"""\
apiVersion: v1
kind: ConfigMap
metadata:
  name: {_NAME}
  namespace: {_NS}
data:
  config.yaml: |
    allowed-namespaces: []
    addr: :8080
    auth-mode: token
    base-path: /
    cluster-api-enabled: true
    environment: cluster
    gin-mode: release
    ui:
      cluster-api-enabled: true
    session:
      key-pairs:
        - signing-key: deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef
          encryption-key: cafebabecafebabecafebabecafebabecafebabecafebabecafebabecafebabe
      cookie:
        name: kubetail_dashboard_token_session
        path: /
        max-age: 2592000
        secure: false
        http-only: true
        same-site: lax
    logging:
      enabled: true
      level: info
      format: json
      access-log:
        enabled: true
        hide-health-checks: true
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
---
apiVersion: v1
kind: Service
metadata:
  name: {_NAME}
  namespace: {_NS}
spec:
  selector: {{app: {_NAME}}}
  ports:
    - {{name: http, port: 8080, targetPort: http}}
"""


def _dashboard_image() -> str:
    out = kubectl(
        "-n", _NS,
        "get", "deployment", "kubetail-dashboard",
        "-o", "jsonpath={.spec.template.spec.containers[0].image}",
    ).stdout.strip()
    assert out, "could not resolve dashboard image"
    return out


@pytest.fixture(scope="module")
def token_dashboard_url(cluster_api_url):
    # cluster_api_url anchors this fixture under the kubetail-api cluster env.
    del cluster_api_url

    kubectl("apply", "-f", "-", input=_manifest(_dashboard_image()))
    try:
        kubectl(
            "-n", _NS, "rollout", "status", f"deployment/{_NAME}",
            "--timeout=120s",
        )

        local_port = free_port()
        pf = subprocess.Popen(
            [
                "kubectl", f"--kubeconfig={KUBECONFIG}",
                "-n", _NS, "port-forward",
                f"service/{_NAME}", f"{local_port}:8080",
            ],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        )
        try:
            base = f"http://localhost:{local_port}"
            deadline = time.monotonic() + 15
            ready = False
            while time.monotonic() < deadline:
                try:
                    if requests.get(f"{base}/healthz", timeout=1).status_code == 200:
                        ready = True
                        break
                except requests.RequestException:
                    pass
                time.sleep(0.2)
            if not ready:
                raise RuntimeError("token-mode dashboard never became healthy")
            yield base
        finally:
            pf.terminate()
            try:
                pf.wait(timeout=5)
            except subprocess.TimeoutExpired:
                pf.kill()
                pf.wait()
    finally:
        for kind in ("service", "deployment", "configmap"):
            kubectl(
                "-n", _NS, "delete", kind, _NAME,
                "--ignore-not-found", "--wait=false", check=False,
            )


@pytest.fixture(scope="module")
def dashboard_sa_token():
    """SA token for the canonical 'authorized user' — its ClusterRole grants
    everything the dashboard itself can do, so happy-path requests through
    token mode succeed end-to-end."""
    out = kubectl(
        "create", "token", "kubetail-dashboard",
        "-n", _NS, "--duration", "1h",
    ).stdout.strip()
    assert out, "empty SA token"
    return out


def _post_graphql(base_url, path, query, variables=None, *, bearer=None):
    s, csrf = session(base_url)
    headers = {"Sec-Fetch-Site": "same-origin", "X-CSRF-Token": csrf}
    if bearer is not None:
        headers["Authorization"] = f"Bearer {bearer}"
    return s.post(
        f"{base_url}{path}",
        headers=headers,
        json={"query": query, "variables": variables or {}},
        timeout=20,
    )


async def _ws_upgrade(ws_url, *, origin, cookies, bearer):
    headers = {"Origin": origin}
    cookie_header = "; ".join(f"{k}={v}" for k, v in (cookies or {}).items())
    if cookie_header:
        headers["Cookie"] = cookie_header
    if bearer is not None:
        headers["Authorization"] = f"Bearer {bearer}"
    try:
        async with websockets.connect(
            ws_url,
            subprotocols=[_WS_SUBPROTOCOL],
            additional_headers=headers,
            open_timeout=5,
        ):
            return None
    except websockets.exceptions.InvalidStatus as e:
        return e.response.status_code


def _ws_url(base_url, path):
    return base_url.replace("http://", "ws://") + path


class TestHTTPRequiresBearer:
    @_PROTECTED_PATHS
    def test_no_bearer_rejected(self, token_dashboard_url, path):
        r = _post_graphql(token_dashboard_url, path, "{__typename}")
        assert r.status_code == 401, r.text

    @pytest.mark.parametrize(
        "header",
        ["Bearer ", "Bearer    "],
        ids=["empty-after-bearer", "whitespace-only"],
    )
    def test_blank_bearer_rejected(self, token_dashboard_url, header):
        s, csrf = session(token_dashboard_url)
        r = s.post(
            f"{token_dashboard_url}/graphql",
            headers={
                "Sec-Fetch-Site": "same-origin",
                "X-CSRF-Token": csrf,
                "Authorization": header,
            },
            json={"query": "{__typename}"},
            timeout=10,
        )
        assert r.status_code == 401, r.text

    @_PROTECTED_PATHS
    def test_bearer_passes(self, token_dashboard_url, dashboard_sa_token, path):
        r = _post_graphql(
            token_dashboard_url, path, "{__typename}",
            bearer=dashboard_sa_token,
        )
        assert r.status_code == 200, r.text
        assert r.json().get("data", {}).get("__typename"), r.text


class TestWebSocketRequiresBearer:
    @_PROTECTED_PATHS
    def test_no_bearer_rejected(self, token_dashboard_url, path):
        s, _ = session(token_dashboard_url)
        status = asyncio.run(_ws_upgrade(
            _ws_url(token_dashboard_url, path),
            origin=token_dashboard_url, cookies=s.cookies, bearer=None,
        ))
        assert status == 401, f"expected 401 on upgrade, got {status}"

    @_PROTECTED_PATHS
    def test_bearer_passes_upgrade(
        self, token_dashboard_url, dashboard_sa_token, path,
    ):
        # Pins only that the auth gate lets the upgrade through. We don't
        # complete the GraphQL handshake — InitFunc still requires the
        # session's CSRF token, and that gate is covered by test_csrf.py.
        s, _ = session(token_dashboard_url)
        status = asyncio.run(_ws_upgrade(
            _ws_url(token_dashboard_url, path),
            origin=token_dashboard_url, cookies=s.cookies,
            bearer=dashboard_sa_token,
        ))
        assert status is None, f"expected upgrade to succeed, got status {status}"


# A namespace-restricted SA token denied on a foreign namespace and allowed
# on its own namespace proves the bearer reaches the kube-apiserver / cluster-
# api as identity. If the dashboard ignored the header and used its own SA,
# the foreign-namespace query would succeed (the dashboard SA has cluster-
# wide pod/log access).
class TestBearerForwarded:
    @pytest.mark.parametrize(
        "path,query,variables_for",
        [
            (
                "/graphql",
                _DASHBOARD_LOG_FETCH,
                lambda ns: {"sources": [f"{ns}:pods/chatter"]},
            ),
            (
                "/cluster-api-proxy/graphql",
                _CLUSTER_API_LOG_METADATA,
                lambda ns: {"namespace": ns},
            ),
        ],
        ids=["dashboard-graphql", "cluster-api-proxy"],
    )
    @_FORWARDING_CASES
    def test_identity_threading(
        self, token_dashboard_url, restricted_sa_tokens,
        path, query, variables_for, namespace, expect_denial,
    ):
        r = _post_graphql(
            token_dashboard_url, path, query, variables_for(namespace),
            bearer=restricted_sa_tokens[SA1_NS],
        )
        assert r.status_code == 200, r.text
        assert has_authz_denial(r.json()) == expect_denial, r.text
