"""WebSocket auth gating for the cluster-api graphql endpoint.

Two layers of defense exist on top of the cluster-api `/graphql` WS upgrade:

  1. **kube-apiserver aggregation gate** — when a client dials the apiserver
     at `/apis/api.kubetail.com/v1/graphql`, kube-apiserver authenticates
     the request before forwarding to the cluster-api Service. Anonymous
     callers fail RBAC (or authn outright) and never reach cluster-api.
     This is the layer the kubetail CLI relies on for follow streams: an
     attacker who can't satisfy kube-apiserver auth can't open a
     subscription, period.

  2. **cluster-api front-proxy gate** — direct hits to the cluster-api
     Service (bypassing the apiserver) are rejected by
     newAggregationAuthMiddleware unless the request carries a valid
     front-proxy client cert. This protects against in-cluster lateral
     callers that can route to the cluster-api Service IP without going
     through the apiserver.

These tests dial WebSocket upgrades directly (no `kubetail logs` in the
loop) so the assertions are about the upgrade response, not about CLI UX.
"""

import asyncio
import json

import pytest
import websockets

from _kubeconfig import materialize_apiserver_access, ssl_ctx

_WS_SUBPROTOCOL = "graphql-transport-ws"
_GRAPHQL_PATH = "/apis/api.kubetail.com/v1/graphql"


def _to_ws(url: str) -> str:
    return url.replace("http://", "ws://").replace("https://", "wss://")


@pytest.fixture(scope="module")
def apiserver():
    access = materialize_apiserver_access()
    yield access
    access.cleanup()


# ---------------------------------------------------------------------------
# Layer 1: kube-apiserver aggregation gate
# ---------------------------------------------------------------------------


def test_apiserver_rejects_anonymous_ws_upgrade(apiserver):
    """A client with no kubeconfig credentials cannot upgrade through the
    aggregation layer. kube-apiserver answers 401/403 on the upgrade
    response (anonymous auth either disabled outright or mapped to
    system:anonymous, which has no RBAC for api.kubetail.com).
    """
    ws_url = _to_ws(apiserver.server) + _GRAPHQL_PATH
    ctx = ssl_ctx(apiserver, with_client_cert=False)

    async def go():
        with pytest.raises(websockets.exceptions.InvalidStatus) as excinfo:
            async with websockets.connect(
                ws_url,
                subprotocols=[_WS_SUBPROTOCOL],
                ssl=ctx,
                open_timeout=5,
            ):
                pass
        return excinfo.value.response.status_code

    status = asyncio.run(go())
    assert status in (401, 403), (
        f"expected 401/403 from apiserver auth gate, got {status}"
    )


def test_apiserver_authenticated_ws_upgrade_succeeds(apiserver):
    """With a valid kubeconfig client cert (cluster-admin in kind), the
    upgrade is forwarded to cluster-api which completes the
    graphql-transport-ws handshake by replying `connection_ack`.

    Proves end-to-end that an authenticated WebSocket actually reaches
    cluster-api through aggregation — i.e. that the auth gate is the only
    thing blocking the anonymous case above, not some unrelated
    misconfiguration.
    """
    ws_url = _to_ws(apiserver.server) + _GRAPHQL_PATH
    ctx = ssl_ctx(apiserver, with_client_cert=True)

    async def go():
        async with websockets.connect(
            ws_url,
            subprotocols=[_WS_SUBPROTOCOL],
            ssl=ctx,
            open_timeout=10,
        ) as ws:
            await ws.send(json.dumps({"type": "connection_init", "payload": {}}))
            return json.loads(await asyncio.wait_for(ws.recv(), timeout=5))

    msg = asyncio.run(go())
    assert msg.get("type") == "connection_ack", msg


# ---------------------------------------------------------------------------
# Layer 2: cluster-api front-proxy gate (direct, bypassing the apiserver)
# ---------------------------------------------------------------------------


def test_cluster_api_rejects_direct_ws_upgrade_without_front_proxy_cert(
    cluster_api_url,
):
    """A WS upgrade hitting the cluster-api Service directly (port-forwarded
    in e2e; in-cluster service IP in production) without a front-proxy
    client cert is rejected by newAggregationAuthMiddleware with HTTP 401.

    This is the regression guard for the "cluster-api accepts requests
    only via the kube-apiserver chain" invariant — without it, anyone
    on-cluster who can reach the Service IP could open a follow stream.
    """
    ws_url = _to_ws(cluster_api_url) + _GRAPHQL_PATH

    # cluster-api uses a self-signed cert in e2e; suppress verification
    # since we're testing authn, not TLS trust. (The real test in
    # production is the apiserver layer above; this layer's job is the
    # 401 once a TLS handshake completes.)
    import ssl as _ssl
    ctx = _ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = _ssl.CERT_NONE

    async def go():
        with pytest.raises(websockets.exceptions.InvalidStatus) as excinfo:
            async with websockets.connect(
                ws_url,
                subprotocols=[_WS_SUBPROTOCOL],
                ssl=ctx,
                open_timeout=5,
            ):
                pass
        return excinfo.value.response.status_code

    status = asyncio.run(go())
    assert status == 401, (
        f"expected 401 from cluster-api front-proxy gate, got {status}"
    )
