"""CSRF / CSWSH tests for the dashboard's /graphql and /cluster-api-proxy."""

import asyncio
import json

import pytest
import requests
import websockets

# kind names the kubeconfig context "kind-<cluster>". Used by the DesktopProxy
# in CLI mode, which requires /cluster-api-proxy/<kubeContext>/<relPath>;
# in cluster mode the InClusterProxy ignores the path tail.
_E2E_KUBE_CONTEXT = "kind-kubetail-e2e"

_PROXY_HTTP_PATH = f"/cluster-api-proxy/{_E2E_KUBE_CONTEXT}/healthz"
_PROXY_WS_PATHS = {
    "cli": f"/cluster-api-proxy/{_E2E_KUBE_CONTEXT}/graphql",
    "cluster": "/cluster-api-proxy/graphql",
}

_WS_SUBPROTOCOL = "graphql-transport-ws"


def _to_ws(url):
    return url.replace("http://", "ws://").replace("https://", "wss://")


# CSRF behavior is identical at the dashboard layer regardless of how the
# dashboard is being served, but the proxy WS path differs: cluster mode uses
# /cluster-api-proxy/graphql while the desktop CLI uses
# /cluster-api-proxy/<kubeContext>/graphql. Parametrizing here (rather than
# at the conftest level) keeps the env axis local to the only suite that
# actually exercises both.
@pytest.fixture(params=["cluster", "cli"])
def env(request):
    return request.param


@pytest.fixture
def target_url(env, request):
    return request.getfixturevalue(
        "dashboard_url" if env == "cluster" else "serve_url"
    )


@pytest.fixture
def dashboard_ws_url(target_url):
    return _to_ws(target_url) + "/graphql"


@pytest.fixture
def proxy_ws_url(target_url, env):
    return _to_ws(target_url) + _PROXY_WS_PATHS[env]


def _session(target_url):
    """Return (session, csrf_token) for the given dashboard base URL."""
    s = requests.Session()
    resp = s.get(f"{target_url}/api/auth/session")
    assert resp.status_code == 200
    token = resp.headers.get("X-CSRF-Token", "")
    assert token, "X-CSRF-Token missing from session response"
    return s, token


def _post(session, url, *, sec_fetch_site=None, csrf_token=None, **kwargs):
    headers = kwargs.pop("headers", {})
    if sec_fetch_site is not None:
        headers["Sec-Fetch-Site"] = sec_fetch_site
    if csrf_token is not None:
        headers["X-CSRF-Token"] = csrf_token
    return session.post(url, headers=headers, **kwargs)


def _ws_init_message(csrf_token=None):
    payload = {"csrfToken": csrf_token} if csrf_token is not None else {}
    return json.dumps({"type": "connection_init", "payload": payload})


def _ws_headers(*, origin=None, cookies=None, extra=None):
    headers = dict(extra or {})
    if origin is not None:
        headers["Origin"] = origin
    cookie_header = "; ".join(f"{k}={v}" for k, v in (cookies or {}).items())
    if cookie_header:
        headers["Cookie"] = cookie_header
    return headers


def _assert_ws_rejected(msg, reason):
    assert msg is None or msg.get("type") == "connection_error", (
        f"expected rejection for {reason}, got {msg}"
    )


async def _ws_upgrade(ws_url, *, origin=None, cookies=None, extra_headers=None):
    """Attempt a WebSocket upgrade. Returns True if the upgrade succeeded."""
    try:
        async with websockets.connect(
            ws_url,
            subprotocols=[_WS_SUBPROTOCOL],
            additional_headers=_ws_headers(origin=origin, cookies=cookies, extra=extra_headers),
            open_timeout=5,
        ):
            return True
    except (websockets.exceptions.WebSocketException, OSError, asyncio.TimeoutError):
        return False


async def _ws_send_init(ws_url, *, origin, cookies=None, csrf_token=None, url_suffix=""):
    """Complete the upgrade, send connection_init, return the first message.

    Returns {"type": "connection_error"} if the server closes cleanly on
    rejection; None if the upgrade itself was rejected.
    """
    try:
        async with websockets.connect(
            ws_url + url_suffix,
            subprotocols=[_WS_SUBPROTOCOL],
            additional_headers=_ws_headers(origin=origin, cookies=cookies),
            open_timeout=5,
        ) as ws:
            await ws.send(_ws_init_message(csrf_token))
            try:
                return json.loads(await asyncio.wait_for(ws.recv(), timeout=5))
            except websockets.exceptions.ConnectionClosedOK:
                # gqlgen closes with 1000 when InitFunc rejects
                return {"type": "connection_error"}
    except (websockets.exceptions.InvalidStatus, ConnectionRefusedError):
        return None


_SEC_FETCH_SITE_CASES = pytest.mark.parametrize(
    "sec_fetch_site",
    [None, "cross-site", "same-site", "none"],
    ids=["missing", "cross-site", "same-site", "none"],
)

_TOKEN_CASES = pytest.mark.parametrize(
    "bad_token",
    [None, "deadbeef", ""],
    ids=["missing", "wrong", "empty"],
)

_WS_ORIGIN_CASES = pytest.mark.parametrize(
    "origin",
    ["http://evil.example.com", "http://localhost:9999.evil.com", "null"],
    ids=["cross-origin", "subdomain-confusion", "null-origin"],
)


# ---------------------------------------------------------------------------
# Dashboard /graphql — HTTP CSRF
# ---------------------------------------------------------------------------


class TestDashboardHTTPCSRF:

    @_SEC_FETCH_SITE_CASES
    def test_sec_fetch_site_rejected(self, target_url, sec_fetch_site):
        s, tok = _session(target_url)
        r = _post(s, f"{target_url}/graphql", sec_fetch_site=sec_fetch_site, csrf_token=tok, json={"query": "{__typename}"})
        assert r.status_code == 403

    @_TOKEN_CASES
    def test_csrf_token_rejected(self, target_url, bad_token):
        s, _ = _session(target_url)
        r = _post(s, f"{target_url}/graphql", sec_fetch_site="same-origin", csrf_token=bad_token, json={"query": "{__typename}"})
        assert r.status_code == 403

    def test_cross_session_token_rejected(self, target_url):
        _, tok_other = _session(target_url)
        s, _ = _session(target_url)
        r = _post(s, f"{target_url}/graphql", sec_fetch_site="same-origin", csrf_token=tok_other, json={"query": "{__typename}"})
        assert r.status_code == 403

    def test_form_without_csrf_field_rejected(self, target_url):
        s, _ = _session(target_url)
        r = _post(s, f"{target_url}/graphql", sec_fetch_site="same-origin", data={"query": "{__typename}"})
        assert r.status_code == 403

    def test_forwarded_csrf_token_smuggling_rejected(self, target_url):
        s, _ = _session(target_url)
        headers = {"Sec-Fetch-Site": "same-origin", "X-Forwarded-CSRF-Token": "attacker-token"}
        r = s.post(f"{target_url}/graphql", headers=headers, json={"query": "{__typename}"})
        assert r.status_code == 403

    def test_valid_csrf_passes(self, target_url):
        s, tok = _session(target_url)
        r = _post(s, f"{target_url}/graphql", sec_fetch_site="same-origin", csrf_token=tok, json={"query": "{__typename}"})
        assert r.status_code != 403

    def test_text_plain_content_type_rejected(self, target_url):
        """text/plain with a JSON body is a CORS-simple request — must still be CSRF-rejected."""
        s, _ = _session(target_url)
        headers = {"Sec-Fetch-Site": "same-origin", "Content-Type": "text/plain"}
        r = s.post(f"{target_url}/graphql", headers=headers, data=json.dumps({"query": "{__typename}"}))
        assert r.status_code == 403

    def test_get_mutation_rejected(self, target_url):
        """Mutations via GET (CORS-simple, browser-fireable cross-origin) must not succeed."""
        s, tok = _session(target_url)
        headers = {"Sec-Fetch-Site": "same-origin", "X-CSRF-Token": tok}
        r = s.get(f"{target_url}/graphql", params={"query": "mutation { __typename }"}, headers=headers)
        assert r.status_code >= 400 or "errors" in r.json()

    def test_token_without_session_rejected(self, target_url):
        """A fabricated token with no session cookie must not pass."""
        s = requests.Session()
        r = _post(s, f"{target_url}/graphql", sec_fetch_site="same-origin", csrf_token="deadbeef", json={"query": "{__typename}"})
        assert r.status_code == 403

    def test_query_string_token_rejected(self, target_url):
        """Token must come from header or form body, not URL query (avoids leak via Referer/logs)."""
        s, tok = _session(target_url)
        headers = {"Sec-Fetch-Site": "same-origin"}
        r = s.post(f"{target_url}/graphql", params={"csrfToken": tok}, headers=headers, json={"query": "{__typename}"})
        assert r.status_code == 403

    def test_logout_endpoint_csrf_rejected(self, target_url):
        """Other mutation endpoints (e.g. /api/auth/logout) must enforce CSRF too."""
        s, _ = _session(target_url)
        r = s.post(f"{target_url}/api/auth/logout", headers={"Sec-Fetch-Site": "same-origin"})
        assert r.status_code == 403


# ---------------------------------------------------------------------------
# Dashboard /graphql — WebSocket / CSWSH
# ---------------------------------------------------------------------------


class TestDashboardWSCSWSH:

    @_WS_ORIGIN_CASES
    def test_ws_cross_origin_upgrade_rejected(self, dashboard_ws_url, origin):
        assert not asyncio.run(_ws_upgrade(dashboard_ws_url, origin=origin)), (
            f"expected upgrade to be rejected for Origin: {origin}"
        )

    def test_ws_no_origin_upgrade_rejected(self, dashboard_ws_url):
        assert not asyncio.run(_ws_upgrade(dashboard_ws_url)), (
            "expected upgrade to be rejected with no Origin header"
        )

    def test_ws_missing_csrf_token_rejected(self, target_url, dashboard_ws_url):
        s, _ = _session(target_url)
        msg = asyncio.run(_ws_send_init(dashboard_ws_url, origin=target_url, cookies=s.cookies))
        _assert_ws_rejected(msg, "missing csrfToken")

    def test_ws_wrong_csrf_token_rejected(self, target_url, dashboard_ws_url):
        s, _ = _session(target_url)
        msg = asyncio.run(_ws_send_init(dashboard_ws_url, origin=target_url, cookies=s.cookies, csrf_token="deadbeef"))
        _assert_ws_rejected(msg, "wrong csrfToken")

    def test_ws_cross_session_csrf_token_rejected(self, target_url, dashboard_ws_url):
        _, tok_other = _session(target_url)
        s, _ = _session(target_url)
        msg = asyncio.run(_ws_send_init(dashboard_ws_url, origin=target_url, cookies=s.cookies, csrf_token=tok_other))
        _assert_ws_rejected(msg, "cross-session csrfToken")

    def test_ws_no_cookie_rejected(self, target_url, dashboard_ws_url):
        """WS connect with valid Origin and a fabricated token but no session cookie must fail."""
        msg = asyncio.run(_ws_send_init(dashboard_ws_url, origin=target_url, cookies=None, csrf_token="deadbeef"))
        _assert_ws_rejected(msg, "no-cookie WS connect")

    def test_ws_query_string_token_rejected(self, target_url, dashboard_ws_url):
        """CSRF token must come from connection_init payload, not URL query."""
        s, tok = _session(target_url)
        msg = asyncio.run(_ws_send_init(
            dashboard_ws_url,
            origin=target_url,
            cookies=s.cookies,
            csrf_token=None,
            url_suffix=f"?csrfToken={tok}",
        ))
        _assert_ws_rejected(msg, "query-string WS token")


# ---------------------------------------------------------------------------
# Dashboard /graphql — successful subscription handshake (sanity-check that
# the rejection tests are targeting a real graphql-transport-ws endpoint).
# ---------------------------------------------------------------------------


class TestDashboardWSAccepted:
    def test_valid_connection_accepted(self, target_url, dashboard_ws_url):
        s, tok = _session(target_url)
        msg = asyncio.run(_ws_send_init(dashboard_ws_url, origin=target_url, cookies=s.cookies, csrf_token=tok))
        assert msg is not None and msg.get("type") == "connection_ack", (
            f"expected connection_ack from dashboard /graphql, got {msg}"
        )


# ---------------------------------------------------------------------------
# /cluster-api-proxy/* — HTTP CSRF (gate fires before the proxy is consulted)
# ---------------------------------------------------------------------------


class TestProxyHTTPCSRF:
    def _post(self, session, target_url, **kwargs):
        return _post(session, f"{target_url}{_PROXY_HTTP_PATH}", json={}, **kwargs)

    def test_post_missing_csrf_rejected(self, target_url):
        s, _ = _session(target_url)
        assert self._post(s, target_url, sec_fetch_site="same-origin").status_code == 403

    def test_post_wrong_csrf_rejected(self, target_url):
        s, _ = _session(target_url)
        assert self._post(s, target_url, sec_fetch_site="same-origin", csrf_token="deadbeef").status_code == 403

    @_SEC_FETCH_SITE_CASES
    def test_post_sec_fetch_site_rejected(self, target_url, sec_fetch_site):
        s, tok = _session(target_url)
        assert self._post(s, target_url, sec_fetch_site=sec_fetch_site, csrf_token=tok).status_code == 403

    def test_post_cross_session_token_rejected(self, target_url):
        _, tok_other = _session(target_url)
        s, _ = _session(target_url)
        assert self._post(s, target_url, sec_fetch_site="same-origin", csrf_token=tok_other).status_code == 403

    def test_post_token_without_session_rejected(self, target_url):
        s = requests.Session()
        assert self._post(s, target_url, sec_fetch_site="same-origin", csrf_token="deadbeef").status_code == 403


# ---------------------------------------------------------------------------
# /cluster-api-proxy/*/graphql — WebSocket / CSWSH (gate is inside the proxy,
# so a real proxy must be wired up — DesktopProxy in cli, InClusterProxy in
# cluster mode with the kubetail-api backend).
# ---------------------------------------------------------------------------


class TestProxyWSCSWSH:
    @_WS_ORIGIN_CASES
    def test_cross_origin_upgrade_rejected(self, target_url, proxy_ws_url, origin):
        s, _ = _session(target_url)
        assert not asyncio.run(_ws_upgrade(proxy_ws_url, origin=origin, cookies=s.cookies)), (
            f"expected upgrade rejection for Origin: {origin}"
        )

    def test_no_origin_upgrade_rejected(self, target_url, proxy_ws_url):
        s, _ = _session(target_url)
        assert not asyncio.run(_ws_upgrade(proxy_ws_url, cookies=s.cookies)), (
            "expected upgrade rejection with no Origin header"
        )

    def test_no_session_upgrade_rejected(self, target_url, proxy_ws_url):
        """Same-origin upgrade with no session cookie has no X-Forwarded-CSRF-Token stamped — must be rejected."""
        assert not asyncio.run(_ws_upgrade(proxy_ws_url, origin=target_url, cookies=None)), (
            "expected upgrade rejection without a CSRF-bearing session"
        )

    def test_forwarded_csrf_smuggling_rejected(self, target_url, proxy_ws_url):
        """A client-supplied X-Forwarded-CSRF-Token without a session must be stripped and the upgrade rejected."""
        assert not asyncio.run(_ws_upgrade(
            proxy_ws_url,
            origin=target_url,
            cookies=None,
            extra_headers={"X-Forwarded-CSRF-Token": "attacker-token"},
        )), "client-supplied X-Forwarded-CSRF-Token must not bypass CSWSH gate"


# ---------------------------------------------------------------------------
# /cluster-api-proxy/*/graphql — successful subscription handshake (proves
# the upgrade reaches the cluster-api's graphql InitFunc end-to-end).
# ---------------------------------------------------------------------------


class TestProxyWSAccepted:
    # Cluster-only for now: the CLI's DesktopProxy WS path through
    # kubectl-proxy → kube-apiserver aggregation isn't validated end-to-end.

    def test_valid_connection_accepted(self, target_url, proxy_ws_url):
        s, tok = _session(target_url)
        msg = asyncio.run(_ws_send_init(proxy_ws_url, origin=target_url, cookies=s.cookies, csrf_token=tok))
        assert msg is not None and msg.get("type") == "connection_ack", (
            f"expected connection_ack from cluster-api-proxy graphql, got {msg}"
        )
