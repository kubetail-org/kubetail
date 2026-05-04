"""End-to-end tests for the `allowed-namespaces` config option.

Patches the in-cluster dashboard and cluster-api to set
`allowed-namespaces: [<SA1_NS>]`, then asserts:

* Even a cluster-admin caller cannot reach `<SA2_NS>` — the config gate
  fires before the kube-apiserver is consulted, so broad RBAC does not
  leak namespaces outside the allow-list.

* A caller whose RBAC is scoped to a namespace they don't otherwise
  control still cannot read `<SA1_NS>` just because it is on the
  allow-list — the dashboard / cluster-api never inherits its own
  ServiceAccount privileges to the requester.

The two halves together pin both directions of the contract: the config
narrows the surface, but it never widens it.
"""

import concurrent.futures
import json
import os
import re
import signal
import subprocess
import time
from pathlib import Path

import pytest
import requests

from _namespace_rbac import (
    KUBECONFIG,
    SA1_NS,
    SA2_NS,
    graphql_field,
    has_authz_denial,
    kubectl,
    post_graphql,
)

_KUBE_NS = "kubetail-system"
_PF_PID_FILE = "/tmp/kubetail-e2e-pf.pid"

_ALLOWED_NS = SA1_NS
_DISALLOWED_NS = SA2_NS

_PODS_LIST_QUERY = (
    "query Q($namespace:String){"
    "coreV1PodsList(namespace:$namespace){items{metadata{name namespace}}}}"
)

_NAMESPACES_LIST_QUERY = (
    "query Q{coreV1NamespacesList{items{metadata{name}}}}"
)

_LOG_METADATA_QUERY = (
    "query Q($namespace:String){"
    "logMetadataList(namespace:$namespace){items{spec{namespace}}}}"
)

_LOG_RECORDS_QUERY = (
    "query Q($sources:[String!]!){"
    "logRecordsFetch(sources:$sources, mode:TAIL, limit:1)"
    "{records{message}}}"
)


def _port_from_url(url):
    return int(url.rsplit(":", 1)[1])


def _patch_allowed_namespaces(configmap, allowed):
    """Rewrite just the `allowed-namespaces` line in a kubetail ConfigMap.

    The ConfigMap has a single `config.yaml` blob; we keep it intact and
    swap one line so any other tunable (auth-mode, session keys, TLS) is
    preserved verbatim. The line lives at the document root, so a
    line-anchored regex is enough — no YAML lib needed.
    """
    cm = json.loads(
        kubectl("get", "configmap", configmap, "-n", _KUBE_NS, "-o", "json").stdout
    )
    yaml_blob = cm["data"]["config.yaml"]
    flow = "[" + ", ".join(allowed) + "]"
    new_blob, count = re.subn(
        r"^allowed-namespaces:.*$",
        f"allowed-namespaces: {flow}",
        yaml_blob,
        count=1,
        flags=re.MULTILINE,
    )
    assert count == 1, (
        f"allowed-namespaces line not found in ConfigMap {configmap}"
    )
    cm["data"]["config.yaml"] = new_blob
    kubectl("apply", "-f", "-", input=json.dumps(cm))


def _rollout_restart(deployment):
    kubectl("rollout", "restart", f"deployment/{deployment}", "-n", _KUBE_NS)


def _rollout_wait(deployment):
    kubectl(
        "rollout", "status", f"deployment/{deployment}",
        "-n", _KUBE_NS, "--timeout=120s",
    )


def _kill_existing_port_forwards():
    """Tear down whichever port-forwards `up.sh` (or a previous fixture
    invocation) left running. Their underlying pods are about to be
    rolled, so leaving the kubectl processes alive would just race the
    new ones for the local port."""
    try:
        text = Path(_PF_PID_FILE).read_text()
    except FileNotFoundError:
        return
    for line in text.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            pid = int(line)
        except ValueError:
            continue
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
    Path(_PF_PID_FILE).unlink(missing_ok=True)


def _start_port_forward(service, local_port, remote_port):
    return subprocess.Popen(
        [
            "kubectl", f"--kubeconfig={KUBECONFIG}",
            "port-forward", "-n", _KUBE_NS,
            f"service/{service}", f"{local_port}:{remote_port}",
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )


def _wait_for_healthz(url, *, verify=False, timeout=30):
    deadline = time.monotonic() + timeout
    last_err = None
    while time.monotonic() < deadline:
        try:
            r = requests.get(f"{url}/healthz", verify=verify, timeout=2)
            if r.status_code == 200:
                return
            last_err = f"status={r.status_code}"
        except requests.RequestException as e:
            last_err = repr(e)
        time.sleep(0.3)
    raise RuntimeError(f"healthz never became ready at {url}: {last_err}")


def _wait_for_apiservice_available(name="v1.api.kubetail.com", timeout=60):
    """Wait until the kube-apiserver's APIService aggregation handler can
    actually dial the cluster-api Service. After a rollout-restart, the
    Service Endpoints flip to the new pod and kube-proxy iptables update;
    on some CNIs (notably kind's default) this can lag the deployment's
    Ready signal by a few seconds, so the proxy path returns
    `connection refused` even though `kubectl rollout status` returned."""
    deadline = time.monotonic() + timeout
    last_status = None
    while time.monotonic() < deadline:
        out = kubectl(
            "get", "apiservice", name,
            "-o", "jsonpath={.status.conditions[?(@.type=='Available')].status}",
        ).stdout.strip()
        last_status = out
        if out == "True":
            return
        time.sleep(0.3)
    raise RuntimeError(
        f"APIService {name} never became Available (last status={last_status!r})"
    )


def _wait_for_proxy_path(dashboard_url, timeout=60):
    """Poll the dashboard's /cluster-api-proxy until it actually round-trips
    to the cluster-api. After a cluster-api rollout, the kube-apiserver's
    aggregation dialer can still hold a connection to the terminated pod's
    IP — kube-proxy iptables and the apiserver's own connection cache both
    take a few seconds to converge. Symptom: `connection refused` on the
    Service ClusterIP even though APIService.Available is True."""
    deadline = time.monotonic() + timeout
    last_err = None
    while time.monotonic() < deadline:
        try:
            body = post_graphql(
                dashboard_url,
                "/cluster-api-proxy/graphql",
                "query Q{__typename}",
                {},
            )
            if isinstance(body, dict) and body.get("data", {}).get("__typename"):
                return
            last_err = repr(body)
        except (AssertionError, requests.RequestException) as e:
            last_err = repr(e)
        time.sleep(0.3)
    raise RuntimeError(f"cluster-api proxy never warmed up: {last_err}")


def _reconfigure(allowed, *, dashboard_url, cluster_api_url):
    """Apply `allowed-namespaces=allowed` to the live deployments and
    re-establish port-forwards on the original local ports. Used for
    both the setup and teardown legs of the fixture."""
    _kill_existing_port_forwards()

    deployments = ["kubetail-dashboard", "kubetail-cluster-api"]
    for d in deployments:
        _patch_allowed_namespaces(d, allowed)

    # Kick off both rollouts concurrently — independent and ~30s each.
    for d in deployments:
        _rollout_restart(d)
    with concurrent.futures.ThreadPoolExecutor(max_workers=len(deployments)) as ex:
        for fut in concurrent.futures.as_completed(
            [ex.submit(_rollout_wait, d) for d in deployments]
        ):
            fut.result()

    procs = [
        _start_port_forward(
            "kubetail-dashboard", _port_from_url(dashboard_url), 8080,
        ),
        _start_port_forward(
            "kubetail-cluster-api", _port_from_url(cluster_api_url), 443,
        ),
    ]
    Path(_PF_PID_FILE).write_text("\n".join(str(p.pid) for p in procs) + "\n")

    _wait_for_healthz(dashboard_url)
    _wait_for_healthz(cluster_api_url, verify=False)
    _wait_for_apiservice_available()
    _wait_for_proxy_path(dashboard_url)
    return procs


@pytest.fixture(scope="module")
def restricted_deployments(dashboard_url, cluster_api_url, restricted_sa_tokens):
    """Patch the in-cluster components to allow only `SA1_NS`, then revert.

    Depends on `restricted_sa_tokens` so the user-side namespaces, pods,
    and ServiceAccounts already exist when we patch the config. We
    reuse the original local ports (those returned by `dashboard_url` /
    `cluster_api_url`) so other session-scoped fixtures keep working.
    """
    procs = _reconfigure(
        [_ALLOWED_NS],
        dashboard_url=dashboard_url,
        cluster_api_url=cluster_api_url,
    )

    try:
        yield {
            "allowed_ns": _ALLOWED_NS,
            "dashboard_url": dashboard_url,
            "cluster_api_url": cluster_api_url,
        }
    finally:
        for p in procs:
            p.terminate()
            try:
                p.wait(timeout=5)
            except subprocess.TimeoutExpired:
                p.kill()
                p.wait()
        _reconfigure(
            [],
            dashboard_url=dashboard_url,
            cluster_api_url=cluster_api_url,
        )


# ---------------------------------------------------------------------------
# Dashboard /graphql.
# ---------------------------------------------------------------------------


class TestDashboardAllowedNamespaces:
    """The dashboard's own resolvers gate every namespace-scoped query
    through `DerefNamespace[ToList]`; allowed-namespaces narrows that
    surface even when the dashboard's ServiceAccount has cluster-wide
    pod/log RBAC."""

    def test_admin_outside_allowed_denied(self, restricted_deployments):
        """Admin RBAC is irrelevant — the config gate fires before the
        kube-apiserver is consulted, so a query for `_DISALLOWED_NS`
        must return Forbidden."""
        body = post_graphql(
            restricted_deployments["dashboard_url"],
            "/graphql",
            _PODS_LIST_QUERY,
            {"namespace": _DISALLOWED_NS},
        )
        assert has_authz_denial(body), body
        assert graphql_field(body, "coreV1PodsList") is None, body

    def test_admin_inside_allowed_ok(self, restricted_deployments):
        """Sanity check: the same query against the allowed namespace
        succeeds. Without this we couldn't tell denial from a wholesale
        outage."""
        body = post_graphql(
            restricted_deployments["dashboard_url"],
            "/graphql",
            _PODS_LIST_QUERY,
            {"namespace": _ALLOWED_NS},
        )
        assert not has_authz_denial(body), body
        items = graphql_field(body, "coreV1PodsList", "items") or []
        names = [i["metadata"]["namespace"] for i in items]
        assert names and all(n == _ALLOWED_NS for n in names), body

    def test_namespaces_list_filtered_to_allowed(self, restricted_deployments):
        """`coreV1NamespacesList` post-filters the kube-apiserver
        response so the UI only sees allowed namespaces. A regression
        that returned the full list would let the UI offer namespaces
        the resolvers will then reject — confusing and a privacy leak."""
        body = post_graphql(
            restricted_deployments["dashboard_url"],
            "/graphql",
            _NAMESPACES_LIST_QUERY,
            {},
        )
        items = graphql_field(body, "coreV1NamespacesList", "items") or []
        names = sorted(i["metadata"]["name"] for i in items)
        assert names == [_ALLOWED_NS], body

    def test_user_without_rbac_denied_for_logs_in_allowed(
        self, restricted_deployments, restricted_sa_tokens,
    ):
        """The user-RBAC enforcement that allowed-namespaces must not
        override is exercised by log streaming: the bearer token rides
        the dashboard's stream pipeline through to the cluster-agent,
        which runs SAR for `pods/log` against the caller's identity.
        SA2 has no pods/log RBAC in `_ALLOWED_NS`, so even with that
        namespace on the allow-list the request must be denied. (Direct
        kube-apiserver queries like coreV1PodsList run under the
        dashboard SA's identity by design, so they can't witness this
        contract — the log path is the right surface to test.)"""
        body = post_graphql(
            restricted_deployments["dashboard_url"],
            "/graphql",
            _LOG_RECORDS_QUERY,
            {"sources": [f"{_ALLOWED_NS}:pods/chatter"]},
            bearer=restricted_sa_tokens[_DISALLOWED_NS],
        )
        assert has_authz_denial(body), body
        assert graphql_field(body, "logRecordsFetch") is None, body


# ---------------------------------------------------------------------------
# Cluster-api /graphql via /cluster-api-proxy.
# ---------------------------------------------------------------------------


class TestClusterAPIAllowedNamespaces:
    """The cluster-api enforces its own allowed-namespaces independently
    of the dashboard's. Even if the dashboard somehow forwarded a request
    for a disallowed namespace, the cluster-api gate must still fire."""

    def test_admin_outside_allowed_denied(self, restricted_deployments):
        body = post_graphql(
            restricted_deployments["dashboard_url"],
            "/cluster-api-proxy/graphql",
            _LOG_METADATA_QUERY,
            {"namespace": _DISALLOWED_NS},
        )
        assert has_authz_denial(body), body
        assert graphql_field(body, "logMetadataList") is None, body

    def test_admin_inside_allowed_ok(self, restricted_deployments):
        body = post_graphql(
            restricted_deployments["dashboard_url"],
            "/cluster-api-proxy/graphql",
            _LOG_METADATA_QUERY,
            {"namespace": _ALLOWED_NS},
        )
        assert not has_authz_denial(body), body
        # logMetadataList may legitimately return zero items if the
        # cluster-agent has not yet indexed any chatter logs; we only
        # care that the call wasn't denied.
        assert graphql_field(body, "logMetadataList") is not None, body

    def test_user_without_rbac_denied_in_allowed(
        self, restricted_deployments, restricted_sa_tokens,
    ):
        """Mirror of the dashboard test, but exercising the cluster-agent's
        identity-keyed SAR. SA2's token rides the aggregation chain to the
        cluster-agent, which asks the kube-apiserver `can SA2 get pods/log
        in _ALLOWED_NS?` — the answer is no, regardless of allow-list."""
        body = post_graphql(
            restricted_deployments["dashboard_url"],
            "/cluster-api-proxy/graphql",
            _LOG_METADATA_QUERY,
            {"namespace": _ALLOWED_NS},
            bearer=restricted_sa_tokens[_DISALLOWED_NS],
        )
        assert has_authz_denial(body), body
