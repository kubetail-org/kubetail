"""Helpers to extract apiserver URL + TLS material from the e2e kubeconfig.

Used by the WebSocket auth tests, which need to dial the kube-apiserver
directly (with and without client credentials) instead of going through the
dashboard or `kubetail` CLI.
"""

import base64
import json
import ssl
import tempfile
from dataclasses import dataclass
from pathlib import Path

from _namespace_rbac import kubectl


@dataclass
class ApiserverAccess:
    """Materialized apiserver coordinates for a Python TLS client.

    `cert_path` / `key_path` are paths to PEM files written to a private
    temp dir; callers can pass them to `ssl.SSLContext.load_cert_chain`.
    """
    server: str
    ca_path: str
    cert_path: str
    key_path: str
    _tmpdir: tempfile.TemporaryDirectory

    def cleanup(self):
        self._tmpdir.cleanup()


def materialize_apiserver_access() -> ApiserverAccess:
    """Read the e2e kubeconfig and write its TLS material to a temp dir.

    `kubectl config view --raw --minify --flatten -o json` resolves any
    file references and base64-encodes them; we decode and write to disk
    so Python's ssl module can consume them.
    """
    cfg = json.loads(
        kubectl("config", "view", "--raw", "--minify", "--flatten", "-o", "json").stdout
    )
    cluster = cfg["clusters"][0]["cluster"]
    user = cfg["users"][0]["user"]

    server = cluster["server"]

    tmpdir = tempfile.TemporaryDirectory(prefix="kubetail-e2e-tls-")
    base = Path(tmpdir.name)

    ca_path = base / "ca.crt"
    ca_path.write_bytes(base64.b64decode(cluster["certificate-authority-data"]))

    cert_path = base / "client.crt"
    cert_path.write_bytes(base64.b64decode(user["client-certificate-data"]))

    key_path = base / "client.key"
    key_path.write_bytes(base64.b64decode(user["client-key-data"]))

    return ApiserverAccess(
        server=server,
        ca_path=str(ca_path),
        cert_path=str(cert_path),
        key_path=str(key_path),
        _tmpdir=tmpdir,
    )


def ssl_ctx(access: ApiserverAccess, *, with_client_cert: bool) -> ssl.SSLContext:
    """Build an SSLContext that trusts the kind apiserver CA.

    With `with_client_cert=True`, also presents the kubeconfig's admin
    client cert/key — that's how we get an authenticated request through
    the apiserver. Without it, the apiserver sees no client cert and
    treats the caller as `system:anonymous`, which has no RBAC for
    api.kubetail.com and is rejected.
    """
    ctx = ssl.create_default_context(cafile=access.ca_path)
    if with_client_cert:
        ctx.load_cert_chain(access.cert_path, access.key_path)
    return ctx
