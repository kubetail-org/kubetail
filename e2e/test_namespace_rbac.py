"""End-to-end namespace-scoped RBAC tests.

A user whose RBAC only grants `pods/log` in their own namespace must be
denied when querying any other namespace's logs. Four paths are exercised:

* `TestCliDashboard` — `kubetail serve` (desktop env). The kubeconfig's
  credentials *are* the identity; the dashboard's `DefaultDesktopAuthorizer`
  checks via SelfSubjectAccessReview before opening informers.

* `TestCliApiProxy` — `kubetail serve`'s `/cluster-api-proxy/<ctx>/graphql`.
  `DesktopProxy` forwards the kubeconfig user's bearer token through
  kube-apiserver aggregation to the cluster-api -> cluster-agent.

* `TestClusterDashboard` — in-cluster dashboard's `/graphql`. The user's
  bearer token is forwarded to the kube-apiserver per request, so the same
  SAR-based authorization applies.

* `TestClusterApiProxy` — in-cluster dashboard's `/cluster-api-proxy/graphql`.
  Token rides through `InClusterProxy` -> kube-apiserver aggregation to the
  cluster-api, which fans out gRPC to the cluster-agent; the cluster-agent
  runs SAR for `pods/log` in the requested namespace as the user.
"""

import json
import os
import subprocess
import tempfile
import time

import pytest
import requests

from _namespace_rbac import (
    GROUP_NS,
    SA1_NAME,
    SA1_NS,
    SA2_NS,
    free_port,
    graphql_field,
    has_authz_denial,
    kubectl,
    post_graphql,
)

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

# k3d names the kubeconfig context "k3d-<cluster>". The CLI's DesktopProxy
# requires /cluster-api-proxy/<kubeContext>/<relPath>; in cluster mode the
# InClusterProxy ignores the path tail.
_E2E_KUBE_CONTEXT = "k3d-kubetail-e2e"


_NAMESPACE_CASES = pytest.mark.parametrize(
    "namespace,expect_denial",
    [(SA2_NS, True), (SA1_NS, False)],
    ids=["sa1-on-sa2-ns-denied", "sa1-on-own-ns-allowed"],
)


# ---------------------------------------------------------------------------
# CLI / desktop env — kubetail serve with a namespace-restricted kubeconfig.
# ---------------------------------------------------------------------------


def _build_restricted_kubeconfig(token):
    """Take the e2e admin kubeconfig and swap the user for the SA token.

    Uses `kubectl config view --raw --minify --flatten -o json` so we get the
    cluster entry (with embedded CA data) without adding a YAML dependency.
    """
    cfg = json.loads(
        kubectl(
            "config", "view", "--raw", "--minify", "--flatten", "-o", "json"
        ).stdout
    )
    cluster_entry = cfg["clusters"][0]
    context_name = cfg["contexts"][0]["name"]
    return {
        "apiVersion": "v1",
        "kind": "Config",
        "clusters": [cluster_entry],
        "users": [{"name": SA1_NAME, "user": {"token": token}}],
        "contexts": [
            {
                "name": context_name,
                "context": {"cluster": cluster_entry["name"], "user": SA1_NAME},
            }
        ],
        "current-context": context_name,
    }


@pytest.fixture(scope="module")
def restricted_serve_url(restricted_sa_tokens, cli):
    kubeconfig = _build_restricted_kubeconfig(restricted_sa_tokens[SA1_NS])
    fh = tempfile.NamedTemporaryFile(
        mode="w", suffix=".kubeconfig", delete=False
    )
    json.dump(kubeconfig, fh)
    fh.close()

    port = free_port()
    env = os.environ.copy()
    env["KUBECONFIG"] = fh.name
    serve_proc = subprocess.Popen(
        [cli, "serve", "--port", str(port), "--skip-open"],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        env=env,
    )

    base_url = f"http://localhost:{port}"
    try:
        deadline = time.monotonic() + 15
        ready = False
        while time.monotonic() < deadline:
            try:
                if requests.get(f"{base_url}/healthz", timeout=1).status_code == 200:
                    ready = True
                    break
            except requests.RequestException:
                pass
            time.sleep(0.2)
        if not ready:
            raise RuntimeError("kubetail serve never became healthy")
        yield base_url
    finally:
        serve_proc.terminate()
        try:
            serve_proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            serve_proc.kill()
            serve_proc.wait()
        os.unlink(fh.name)


class TestCliDashboard:
    @_NAMESPACE_CASES
    def test_log_records_fetch(self, restricted_serve_url, namespace, expect_denial):
        body = post_graphql(
            restricted_serve_url, "/graphql",
            _DASHBOARD_LOG_FETCH, {"sources": [f"{namespace}:pods/chatter"]},
        )
        assert has_authz_denial(body) == expect_denial, body
        if expect_denial:
            assert graphql_field(body, "logRecordsFetch") is None, body


class TestCliApiProxy:
    @_NAMESPACE_CASES
    def test_log_metadata_list(self, restricted_serve_url, namespace, expect_denial):
        body = post_graphql(
            restricted_serve_url,
            f"/cluster-api-proxy/{_E2E_KUBE_CONTEXT}/graphql",
            _CLUSTER_API_LOG_METADATA, {"namespace": namespace},
        )
        assert has_authz_denial(body) == expect_denial, body


# ---------------------------------------------------------------------------
# Cluster env — in-cluster dashboard. Bearer token rides on each request.
# ---------------------------------------------------------------------------


class TestClusterDashboard:
    @_NAMESPACE_CASES
    def test_log_records_fetch(
        self, dashboard_url, restricted_sa_tokens, namespace, expect_denial
    ):
        body = post_graphql(
            dashboard_url, "/graphql",
            _DASHBOARD_LOG_FETCH, {"sources": [f"{namespace}:pods/chatter"]},
            bearer=restricted_sa_tokens[SA1_NS],
        )
        assert has_authz_denial(body) == expect_denial, body
        if expect_denial:
            assert graphql_field(body, "logRecordsFetch") is None, body


class TestClusterApiProxy:
    @_NAMESPACE_CASES
    def test_log_metadata_list(
        self, dashboard_url, restricted_sa_tokens, namespace, expect_denial
    ):
        body = post_graphql(
            dashboard_url, "/cluster-api-proxy/graphql",
            _CLUSTER_API_LOG_METADATA, {"namespace": namespace},
            bearer=restricted_sa_tokens[SA1_NS],
        )
        assert has_authz_denial(body) == expect_denial, body

    @_NAMESPACE_CASES
    def test_log_records_fetch(
        self, dashboard_url, restricted_sa_tokens, namespace, expect_denial
    ):
        body = post_graphql(
            dashboard_url, "/cluster-api-proxy/graphql",
            _DASHBOARD_LOG_FETCH, {"sources": [f"{namespace}:pods/chatter"]},
            bearer=restricted_sa_tokens[SA1_NS],
        )
        assert has_authz_denial(body) == expect_denial, body
        if expect_denial:
            assert graphql_field(body, "logRecordsFetch") is None, body

    @pytest.mark.parametrize(
        "token_ns,query_ns,expect_denial",
        [
            (SA1_NS, SA1_NS, False),
            (SA1_NS, SA2_NS, True),
            (SA2_NS, SA2_NS, False),
            (SA2_NS, SA1_NS, True),
        ],
        ids=["sa1-on-own", "sa1-on-other", "sa2-on-own", "sa2-on-other"],
    )
    def test_identity_keyed_authorization(
        self, dashboard_url, restricted_sa_tokens,
        token_ns, query_ns, expect_denial,
    ):
        """Two SAs scoped to different namespaces (sa1 -> SA1_NS,
        sa2 -> SA2_NS) must not bleed into each other. Same query,
        two identities — exercises identity flow through kube-apiserver
        aggregation -> cluster-api -> cluster-agent and the cluster-agent's
        identity-keyed SAR cache."""
        body = post_graphql(
            dashboard_url, "/cluster-api-proxy/graphql",
            _CLUSTER_API_LOG_METADATA, {"namespace": query_ns},
            bearer=restricted_sa_tokens[token_ns],
        )
        assert has_authz_denial(body) == expect_denial, body

    def test_agent_permission_denied_yields_no_data(
        self, dashboard_url, restricted_sa_tokens,
    ):
        """When the cluster-agent answers PermissionDenied for a fan-out
        shard, cluster-api must surface that as a GraphQL error and leave
        the data field null. A regression that returned partial items
        alongside the error would slip past `has_authz_denial` alone — this
        test pins the no-leak side of the contract explicitly."""
        body = post_graphql(
            dashboard_url, "/cluster-api-proxy/graphql",
            _CLUSTER_API_LOG_METADATA, {"namespace": SA2_NS},
            bearer=restricted_sa_tokens[SA1_NS],
        )
        assert has_authz_denial(body), body
        assert graphql_field(body, "logMetadataList") is None, body

    @pytest.mark.parametrize(
        "token_ns,expect_denial",
        [(GROUP_NS, False), (SA1_NS, True)],
        ids=["group-sa-allowed", "non-member-sa-denied"],
    )
    def test_group_bound_access(
        self, dashboard_url, restricted_sa_tokens, token_ns, expect_denial,
    ):
        """pods/log in GROUP_NS is bound to Group `system:serviceaccounts:
        GROUP_NS`. The group-bound SA (lives in GROUP_NS, no other RBAC) is a
        member -> allowed. SA1 (lives in SA1_NS) is not a member -> denied.
        Proves group identity threads through kube-apiserver -> cluster-api
        front-proxy headers -> cluster-agent SAR and is honored, not silently
        dropped."""
        body = post_graphql(
            dashboard_url, "/cluster-api-proxy/graphql",
            _CLUSTER_API_LOG_METADATA, {"namespace": GROUP_NS},
            bearer=restricted_sa_tokens[token_ns],
        )
        assert has_authz_denial(body) == expect_denial, body
