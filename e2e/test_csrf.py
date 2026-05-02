import asyncio
import json

import pytest
import requests
import websockets

pytestmark = [pytest.mark.cluster, pytest.mark.kubetail_api]


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


# ---------------------------------------------------------------------------
# HTTP CSRF
# ---------------------------------------------------------------------------

@_SEC_FETCH_SITE_CASES
def test_sec_fetch_site_rejected(target_url, sec_fetch_site):
    s, tok = _session(target_url)
    r = _post(s, f"{target_url}/graphql", sec_fetch_site=sec_fetch_site, csrf_token=tok, json={"query": "{__typename}"})
    assert r.status_code == 403


@_TOKEN_CASES
def test_csrf_token_rejected(target_url, bad_token):
    s, _ = _session(target_url)
    r = _post(s, f"{target_url}/graphql", sec_fetch_site="same-origin", csrf_token=bad_token, json={"query": "{__typename}"})
    assert r.status_code == 403


def test_cross_session_token_rejected(target_url):
    _, tok_other = _session(target_url)
    s, _ = _session(target_url)
    r = _post(s, f"{target_url}/graphql", sec_fetch_site="same-origin", csrf_token=tok_other, json={"query": "{__typename}"})
    assert r.status_code == 403


def test_form_without_csrf_field_rejected(target_url):
    s, _ = _session(target_url)
    r = _post(s, f"{target_url}/graphql", sec_fetch_site="same-origin", data={"query": "{__typename}"})
    assert r.status_code == 403


def test_forwarded_csrf_token_smuggling_rejected(target_url):
    s, _ = _session(target_url)
    headers = {"Sec-Fetch-Site": "same-origin", "X-Forwarded-CSRF-Token": "attacker-token"}
    r = s.post(f"{target_url}/graphql", headers=headers, json={"query": "{__typename}"})
    assert r.status_code == 403


def test_valid_csrf_passes(target_url):
    s, tok = _session(target_url)
    r = _post(s, f"{target_url}/graphql", sec_fetch_site="same-origin", csrf_token=tok, json={"query": "{__typename}"})
    assert r.status_code != 403


def test_text_plain_content_type_rejected(target_url):
    """text/plain with a JSON body is a CORS-simple request — must still be CSRF-rejected."""
    s, _ = _session(target_url)
    headers = {"Sec-Fetch-Site": "same-origin", "Content-Type": "text/plain"}
    r = s.post(f"{target_url}/graphql", headers=headers, data=json.dumps({"query": "{__typename}"}))
    assert r.status_code == 403


def test_get_mutation_rejected(target_url):
    """Mutations via GET (CORS-simple, browser-fireable cross-origin) must not succeed."""
    s, tok = _session(target_url)
    headers = {"Sec-Fetch-Site": "same-origin", "X-CSRF-Token": tok}
    r = s.get(f"{target_url}/graphql", params={"query": "mutation { __typename }"}, headers=headers)
    assert r.status_code >= 400 or "errors" in r.json()


def test_token_without_session_rejected(target_url):
    """A fabricated token with no session cookie must not pass."""
    s = requests.Session()
    r = _post(s, f"{target_url}/graphql", sec_fetch_site="same-origin", csrf_token="deadbeef", json={"query": "{__typename}"})
    assert r.status_code == 403


def test_query_string_token_rejected(target_url):
    """Token must come from header or form body, not URL query (avoids leak via Referer/logs)."""
    s, tok = _session(target_url)
    headers = {"Sec-Fetch-Site": "same-origin"}
    r = s.post(f"{target_url}/graphql", params={"csrfToken": tok}, headers=headers, json={"query": "{__typename}"})
    assert r.status_code == 403


def test_logout_endpoint_csrf_rejected(target_url):
    """Other mutation endpoints (e.g. /api/auth/logout) must enforce CSRF too."""
    s, _ = _session(target_url)
    r = s.post(f"{target_url}/api/auth/logout", headers={"Sec-Fetch-Site": "same-origin"})
    assert r.status_code == 403


# ---------------------------------------------------------------------------
# WebSocket / CSWSH
# ---------------------------------------------------------------------------

_WS_SUBPROTOCOL = "graphql-transport-ws"


def _ws_init_message(csrf_token=None):
    payload = {"csrfToken": csrf_token} if csrf_token is not None else {}
    return json.dumps({"type": "connection_init", "payload": payload})


def _ws_url(target_url):
    return target_url.replace("http://", "ws://").replace("https://", "wss://") + "/graphql"


def _ws_headers(*, origin=None, cookies=None):
    headers = {}
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


async def _ws_connect(target_url, *, origin=None, cookies=None):
    """
    Attempt a WebSocket upgrade. Returns (connected: bool, status_code_or_None).
    connected=False means the HTTP upgrade was rejected.
    """
    try:
        async with websockets.connect(
            _ws_url(target_url),
            subprotocols=[_WS_SUBPROTOCOL],
            additional_headers=_ws_headers(origin=origin, cookies=cookies),
            open_timeout=5,
        ):
            return True, None
    except websockets.exceptions.InvalidStatus as e:
        return False, e.response.status_code
    except (websockets.exceptions.WebSocketException, OSError, asyncio.TimeoutError):
        return False, None


async def _ws_send_init(target_url, *, origin, cookies=None, csrf_token=None, url_suffix=""):
    """
    Complete the upgrade, send connection_init, and return the first message.
    Returns {"type": "connection_error"} if the server closes cleanly on rejection.
    Returns None if the upgrade itself was rejected.
    """
    try:
        async with websockets.connect(
            _ws_url(target_url) + url_suffix,
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


_WS_ORIGIN_CASES = pytest.mark.parametrize(
    "origin",
    ["http://evil.example.com", "http://localhost:9999.evil.com", "null"],
    ids=["cross-origin", "subdomain-confusion", "null-origin"],
)


@_WS_ORIGIN_CASES
def test_ws_cross_origin_upgrade_rejected(target_url, origin):
    connected, _ = asyncio.run(_ws_connect(target_url, origin=origin))
    assert not connected, f"expected upgrade to be rejected for Origin: {origin}"


def test_ws_no_origin_upgrade_rejected(target_url):
    connected, _ = asyncio.run(_ws_connect(target_url))
    assert not connected, "expected upgrade to be rejected with no Origin header"


def test_ws_missing_csrf_token_rejected(target_url):
    s, _ = _session(target_url)
    msg = asyncio.run(_ws_send_init(target_url, origin=target_url, cookies=s.cookies))
    _assert_ws_rejected(msg, "missing csrfToken")


def test_ws_wrong_csrf_token_rejected(target_url):
    s, _ = _session(target_url)
    msg = asyncio.run(_ws_send_init(target_url, origin=target_url, cookies=s.cookies, csrf_token="deadbeef"))
    _assert_ws_rejected(msg, "wrong csrfToken")


def test_ws_cross_session_csrf_token_rejected(target_url):
    _, tok_other = _session(target_url)
    s, _ = _session(target_url)
    msg = asyncio.run(_ws_send_init(target_url, origin=target_url, cookies=s.cookies, csrf_token=tok_other))
    _assert_ws_rejected(msg, "cross-session csrfToken")


def test_ws_valid_connection_accepted(target_url):
    s, tok = _session(target_url)
    msg = asyncio.run(_ws_send_init(target_url, origin=target_url, cookies=s.cookies, csrf_token=tok))
    assert msg is not None and msg.get("type") == "connection_ack", (
        f"expected connection_ack with valid origin and csrfToken, got {msg}"
    )


def test_ws_no_cookie_rejected(target_url):
    """WS connect with valid Origin and a fabricated token but no session cookie must fail."""
    msg = asyncio.run(_ws_send_init(target_url, origin=target_url, cookies=None, csrf_token="deadbeef"))
    _assert_ws_rejected(msg, "no-cookie WS connect")


def test_ws_query_string_token_rejected(target_url):
    """CSRF token must come from connection_init payload, not URL query."""
    s, tok = _session(target_url)
    msg = asyncio.run(_ws_send_init(
        target_url,
        origin=target_url,
        cookies=s.cookies,
        csrf_token=None,
        url_suffix=f"?csrfToken={tok}",
    ))
    _assert_ws_rejected(msg, "query-string WS token")
