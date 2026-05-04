"""Zero-trust ingress tests: cluster-api only honors aggregated requests
through kube-apiserver; cluster-agent only honors mTLS clients."""

import asyncio
import base64
import json
import socket
import ssl
import subprocess
import time
from contextlib import contextmanager
from pathlib import Path

import grpc
import pytest
import requests
import websockets
import websockets.exceptions


_KUBECONFIG = "/tmp/kubetail-e2e.kubeconfig"
_NS = "kubetail-system"
_AGGREGATED_BASE = "/apis/api.kubetail.com/v1"

# Self-signed cert that chains to neither the cluster-api's client-CA pool
# nor its requestheader-client-CA pool. Used to exercise the middleware's
# "no valid certificate found" branch — see e2e/tls/README.md.
_UNTRUSTED_CLIENT_CERT = (
    str(Path(__file__).parent / "tls" / "untrusted-client.crt"),
    str(Path(__file__).parent / "tls" / "untrusted-client.key"),
)

# TLS material the cluster mounts via secrets. Re-used here so the e2e tests
# can drive the cluster-agent's gRPC listener directly. `cluster-api.{crt,key}`
# is the cert kube-apiserver-proxied cluster-api presents to the agent — the
# only client identity the agent allows past its trust-chain interceptor.
# `cluster-agent.{crt,key}` is signed by the same CA but has the agent's CN,
# so it lets us exercise the "valid CA but disallowed CN" branch.
_KUBETAIL_TLS_DIR = Path(__file__).parent.parent / "hack" / "tilt" / "tls"
_KUBETAIL_CA = _KUBETAIL_TLS_DIR / "ca.crt"
_CLUSTER_API_CLIENT_CERT = _KUBETAIL_TLS_DIR / "cluster-api.crt"
_CLUSTER_API_CLIENT_KEY = _KUBETAIL_TLS_DIR / "cluster-api.key"
_CLUSTER_AGENT_CERT = _KUBETAIL_TLS_DIR / "cluster-agent.crt"
_CLUSTER_AGENT_KEY = _KUBETAIL_TLS_DIR / "cluster-agent.key"

# Override SNI/SAN verification for the local port-forward. The agent's serving
# cert is signed for the in-cluster service DNS name, not 127.0.0.1.
_AGENT_TARGET_NAME = "kubetail-cluster-agent.kubetail-system.svc"

# Fully-qualified gRPC method on the cluster-agent. Picked because its request
# message (`LogMetadataListRequest`) has all-optional fields, so an empty body
# is a syntactically valid proto — the call is rejected by the auth interceptor
# rather than by deserialization.
_LIST_METHOD = "/cluster_agent.LogMetadataService/List"


# kind/kubeadm stores the front-proxy client cert+key under this path inside
# the control-plane container. kind names the control-plane container
# "<cluster>-control-plane".
_KIND_SERVER = "kubetail-e2e-control-plane"
_FRONT_PROXY_CRT = "/etc/kubernetes/pki/front-proxy-client.crt"
_FRONT_PROXY_KEY = "/etc/kubernetes/pki/front-proxy-client.key"


@pytest.fixture(scope="session")
def front_proxy_client_cert(tmp_path_factory):
    """Extract the kube-apiserver's front-proxy client cert+key from the
    kind control-plane container. This is the cert kube-apiserver presents
    when it forwards aggregated requests to cluster-api — anything signed
    by it (and matching the requestheader-allowed-names CN) is trusted by
    aggregationAuthMiddleware to *carry* an identity in headers, but the
    headers themselves still have to be present."""
    def _docker_cat(path):
        return subprocess.run(
            ["docker", "exec", _KIND_SERVER, "cat", path],
            check=True, capture_output=True,
        ).stdout

    try:
        crt_bytes = _docker_cat(_FRONT_PROXY_CRT)
        key_bytes = _docker_cat(_FRONT_PROXY_KEY)
    except (FileNotFoundError, subprocess.CalledProcessError) as e:
        pytest.skip(f"front-proxy material unavailable (not a kind cluster?): {e}")

    d = tmp_path_factory.mktemp("front-proxy-cert")
    crt = d / "fp.crt"
    key = d / "fp.key"
    crt.write_bytes(crt_bytes)
    key.write_bytes(key_bytes)
    return (str(crt), str(key))


@pytest.fixture(scope="session")
def admin_client_cert(tmp_path_factory):
    """Extract the e2e admin's client cert+key from the kubeconfig.

    The kind admin cert is signed by the same CA the cluster-api loads
    into its ClientCAs pool (extension-apiserver-authentication's
    client-ca-file), so a request bearing it takes the middleware's
    direct-cert path."""
    cfg = json.loads(subprocess.run(
        ["kubectl", f"--kubeconfig={_KUBECONFIG}", "config", "view",
         "--raw", "--minify", "--flatten", "-o", "json"],
        check=True, capture_output=True, text=True,
    ).stdout)
    user = cfg["users"][0]["user"]
    d = tmp_path_factory.mktemp("admin-cert")
    crt = d / "client.crt"
    key = d / "client.key"
    crt.write_bytes(base64.b64decode(user["client-certificate-data"]))
    key.write_bytes(base64.b64decode(user["client-key-data"]))
    return (str(crt), str(key))


class TestClusterAPIAggregationGate:
    def test_direct_aggregated_healthz_unauthorized(self, cluster_api_url):
        r = requests.get(f"{cluster_api_url}{_AGGREGATED_BASE}/healthz", verify=False)
        assert r.status_code == 401

    def test_direct_aggregated_graphql_unauthorized(self, cluster_api_url):
        r = requests.post(
            f"{cluster_api_url}{_AGGREGATED_BASE}/graphql",
            json={"query": "{__typename}"},
            verify=False,
        )
        assert r.status_code == 401

    def test_direct_aggregated_download_unauthorized(self, cluster_api_url):
        r = requests.post(f"{cluster_api_url}{_AGGREGATED_BASE}/download", verify=False)
        assert r.status_code == 401

    def test_root_healthz_open(self, cluster_api_url):
        # Unaggregated root /healthz is intentionally open for the kubelet probe.
        r = requests.get(f"{cluster_api_url}/healthz", verify=False)
        assert r.status_code == 200

    def test_spoofed_front_proxy_headers_rejected(self, cluster_api_url):
        """Front-proxy impersonation headers without any client cert must
        not grant identity. Trips the middleware's first gate
        (`len(r.TLS.PeerCertificates) == 0`) before header parsing."""
        r = requests.post(
            f"{cluster_api_url}{_AGGREGATED_BASE}/graphql",
            json={"query": "{__typename}"},
            headers={
                "X-Remote-User": "system:masters",
                "X-Remote-Group": "system:masters",
                "X-Remote-Extra-foo": "bar",
            },
            verify=False,
        )
        assert r.status_code == 401

    def test_untrusted_client_cert_with_spoofed_headers_rejected(self, cluster_api_url):
        """Same spoofed headers but with an arbitrary self-signed cert that
        doesn't chain to the front-proxy CA pool. Exercises the middleware's
        `no valid certificate found` branch — proves *having* a cert isn't
        sufficient to reach the header-parsing path."""
        r = requests.post(
            f"{cluster_api_url}{_AGGREGATED_BASE}/graphql",
            json={"query": "{__typename}"},
            headers={
                "X-Remote-User": "system:masters",
                "X-Remote-Group": "system:masters",
            },
            cert=_UNTRUSTED_CLIENT_CERT,
            verify=False,
        )
        assert r.status_code == 401

    def test_legitimate_cluster_cert_not_front_proxy_rejected(
        self, cluster_api_url, admin_client_cert,
    ):
        """A holder of a legitimate cluster cert (kubectl admin, controller,
        system:node, etc.) is NOT kube-apiserver — they must be rejected at
        the gate. The cluster-api accepts requests only via the front-proxy
        chain. Spoofed headers in particular are never read because the cert
        itself fails verification against the requestheader-client-ca-file
        pool (the only trust anchor we honor)."""
        r = requests.post(
            f"{cluster_api_url}{_AGGREGATED_BASE}/graphql",
            json={"query": '{ logMetadataList(namespace: "kubetail-system") { items { id } } }'},
            headers={
                "X-Remote-User": "spoofed-attacker",
                "X-Remote-Group": "system:masters",
                "X-Remote-Extra-Scopes": "openid",
            },
            cert=admin_client_cert,
            verify=False,
        )
        assert r.status_code == 401, r.text

    def test_front_proxy_cert_without_user_header_rejected(
        self, cluster_api_url, front_proxy_client_cert,
    ):
        """Holding a valid front-proxy cert authorizes you to *forward* an
        identity — it doesn't make you one. Without an X-Remote-User header
        the middleware has no user to attach, so the request must be rejected
        rather than fall through to anonymous-but-trusted."""
        r = requests.get(
            f"{cluster_api_url}{_AGGREGATED_BASE}/healthz",
            cert=front_proxy_client_cert,
            verify=False,
        )
        assert r.status_code == 401, r.text

    def test_front_proxy_cert_with_user_header_authorized(
        self, cluster_api_url, front_proxy_client_cert,
    ):
        """Sanity check that the 401 above isn't cert-rejection in disguise:
        the same cert with X-Remote-User present passes the gate."""
        r = requests.get(
            f"{cluster_api_url}{_AGGREGATED_BASE}/healthz",
            headers={"X-Remote-User": "front-proxy-sanity"},
            cert=front_proxy_client_cert,
            verify=False,
        )
        assert r.status_code == 200, r.text

    def test_direct_ws_upgrade_rejected(self, cluster_api_url):
        """The aggregation middleware fires on every protected route — including
        WebSocket upgrades. A direct upgrade attempt without a client cert must
        get the same 401 the HTTP path returns; otherwise a WS-only auth bypass
        would let an attacker stream subscriptions without identity."""
        ws_url = (
            cluster_api_url.replace("https://", "wss://")
            + f"{_AGGREGATED_BASE}/graphql"
        )
        ctx = ssl.create_default_context()
        ctx.check_hostname = False
        ctx.verify_mode = ssl.CERT_NONE

        async def upgrade():
            async with websockets.connect(
                ws_url,
                ssl=ctx,
                subprotocols=["graphql-transport-ws"],
                open_timeout=5,
            ):
                pass

        with pytest.raises(websockets.exceptions.InvalidStatus) as exc:
            asyncio.run(upgrade())
        assert exc.value.response.status_code == 401

    @pytest.mark.usefixtures("cluster_api_url")
    def test_through_kube_apiserver_authorized(self):
        out = subprocess.run(
            ["kubectl", f"--kubeconfig={_KUBECONFIG}", "get", "--raw", f"{_AGGREGATED_BASE}/healthz"],
            check=True,
            capture_output=True,
            text=True,
        )
        assert json.loads(out.stdout) == {"status": "ok"}


def _cluster_agent_pod():
    out = subprocess.run(
        [
            "kubectl", f"--kubeconfig={_KUBECONFIG}", "-n", _NS,
            "get", "pods",
            "-l", "app.kubernetes.io/component=cluster-agent",
            "-o", "jsonpath={.items[0].metadata.name}",
        ],
        check=True,
        capture_output=True,
        text=True,
    )
    name = out.stdout.strip()
    assert name, "no cluster-agent pod found"
    return name


@contextmanager
def _port_forward(pod, remote_port):
    # Bind :0 to grab a free port, then release it before kubectl claims it.
    # Racy in theory; fine on a dev/CI host where nothing else is squatting.
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        local_port = s.getsockname()[1]
    proc = subprocess.Popen(
        [
            "kubectl", f"--kubeconfig={_KUBECONFIG}", "-n", _NS,
            "port-forward", f"pod/{pod}", f"{local_port}:{remote_port}",
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    try:
        for _ in range(100):
            try:
                with socket.create_connection(("127.0.0.1", local_port), timeout=0.5):
                    break
            except OSError:
                time.sleep(0.1)
        else:
            raise RuntimeError("port-forward never came up")
        yield local_port
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()


@pytest.fixture(scope="module")
def cluster_agent_local_port(cluster_api_url):
    del cluster_api_url  # only used to sequence with the kubetail-api cluster fixture
    with _port_forward(_cluster_agent_pod(), 50051) as port:
        yield port


class TestClusterAgentMTLSGate:
    def test_tls_without_client_cert_rejected(self, cluster_agent_local_port):
        ctx = ssl.create_default_context()
        ctx.check_hostname = False
        ctx.verify_mode = ssl.CERT_NONE
        with socket.create_connection(("127.0.0.1", cluster_agent_local_port), timeout=5) as raw:
            with pytest.raises(OSError):
                with ctx.wrap_socket(raw, server_hostname="kubetail-cluster-agent.kubetail-system.svc") as tls:
                    # Some servers defer the cert demand to the first record.
                    tls.send(b"\x00")
                    tls.recv(1)


def _grpc_channel(cert_path: Path, key_path: Path, port: int) -> grpc.Channel:
    creds = grpc.ssl_channel_credentials(
        root_certificates=_KUBETAIL_CA.read_bytes(),
        private_key=key_path.read_bytes(),
        certificate_chain=cert_path.read_bytes(),
    )
    return grpc.secure_channel(
        f"127.0.0.1:{port}",
        creds,
        options=(("grpc.ssl_target_name_override", _AGENT_TARGET_NAME),),
    )


def _call_list(channel: grpc.Channel, *, metadata=()) -> grpc.RpcError:
    """Invoke LogMetadataService/List with an empty request body and return
    the resulting RpcError. Bypasses generated stubs so the test stays
    dependency-light and proto-version-agnostic."""
    rpc = channel.unary_unary(
        _LIST_METHOD,
        request_serializer=lambda _: b"",
        response_deserializer=lambda b: b,
    )
    with pytest.raises(grpc.RpcError) as exc:
        rpc(None, metadata=metadata, timeout=5)
    return exc.value


class TestClusterAgentAuthGate:
    """A valid mTLS handshake is necessary but not sufficient: the agent's
    trust-chain interceptor (crates/cluster_agent/src/auth.rs) must also see
    a forwarded identity from a CN on the allowed-names list. Each parametrized
    case below asserts a different way that gate must fail closed."""

    @pytest.mark.parametrize(
        "case,cert,key,metadata,expected",
        [
            # Allowlisted CN, no forwarded identity — answering would serve data
            # under the cluster-api SA's privileges.
            (
                "missing_user_metadata",
                _CLUSTER_API_CLIENT_CERT, _CLUSTER_API_CLIENT_KEY,
                (),
                grpc.StatusCode.UNAUTHENTICATED,
            ),
            # Empty value must not be treated as anonymous-but-trusted.
            (
                "empty_user_metadata",
                _CLUSTER_API_CLIENT_CERT, _CLUSTER_API_CLIENT_KEY,
                (("x-remote-user", ""),),
                grpc.StatusCode.UNAUTHENTICATED,
            ),
            # CA membership alone doesn't confer the right to forward identities:
            # the agent's own cert chains to kubetail-ca but its CN isn't on
            # the allowlist.
            (
                "valid_ca_but_disallowed_cn",
                _CLUSTER_AGENT_CERT, _CLUSTER_AGENT_KEY,
                (("x-remote-user", "spoofed-attacker"), ("x-remote-group", "system:masters")),
                grpc.StatusCode.PERMISSION_DENIED,
            ),
        ],
        ids=lambda v: v if isinstance(v, str) else None,
    )
    def test_rejected(self, cluster_agent_local_port, case, cert, key, metadata, expected):
        del case  # used as the parametrize id
        with _grpc_channel(cert, key, cluster_agent_local_port) as channel:
            err = _call_list(channel, metadata=metadata)
        assert err.code() == expected, (err.code(), err.details())
