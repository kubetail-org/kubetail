"""Tests that exec the `kubetail` binary on the host."""

import os
import re
import subprocess
import time

import pytest

from _log_producer import LP_LINE_PREFIX
from conftest import assert_healthz

KUBECONFIG = "/tmp/kubetail-e2e.kubeconfig"

_LINE_RE = re.compile(rf"\b{re.escape(LP_LINE_PREFIX)}-\d+\b")


def _env():
    env = os.environ.copy()
    env["KUBECONFIG"] = KUBECONFIG
    return env


def test_version(cli):
    result = subprocess.run([cli, "--version"], capture_output=True, text=True)
    assert result.returncode == 0
    assert "kubetail" in result.stdout.lower()


def test_serve_healthz(serve_url):
    assert_healthz(serve_url)


# ---------------------------------------------------------------------------
# `kubetail logs` against both backends
#
# Covers the four backend x mode combinations:
#   - kubernetes-api backend, --tail (one-shot via pods/log)
#   - kubetail-api backend,   --tail (GraphQL POST through aggregation)
#   - kubernetes-api backend, --follow
#   - kubetail-api backend,   --follow (graphql-transport-ws over the apiserver)
#
# The kubetail-api follow case is the regression-guard for the apiserver's
# 60s deadline on non-watch requests — the previous SSE-over-POST path got
# RST_STREAM INTERNAL_ERROR after ~60s through aggregation. WebSocket
# upgrades get hijacked by the apiserver and bypass that filter; this test
# only asserts the stream produces data, not that it survives 60s, since
# waiting that long in CI is wasteful. See test_cluster_api_websocket_auth.py
# for the auth-gate proof that pins down the upgrade behavior.
# ---------------------------------------------------------------------------


@pytest.fixture(params=["kubernetes", "kubetail"])
def backend(request):
    return request.param


def test_logs_tail(cli, log_producer, backend):
    result = subprocess.run(
        [
            cli, "logs", log_producer.source,
            "--backend", backend,
            "--tail", "5",
            "--raw",
        ],
        capture_output=True,
        text=True,
        env=_env(),
        timeout=30,
    )
    assert result.returncode == 0, (
        f"kubetail logs failed (backend={backend}): "
        f"stdout={result.stdout!r} stderr={result.stderr!r}"
    )
    matches = _LINE_RE.findall(result.stdout)
    assert len(matches) >= 1, (
        f"expected at least one '{LP_LINE_PREFIX}-N' line "
        f"(backend={backend}), got: {result.stdout!r}"
    )


def test_logs_follow(cli, log_producer, backend):
    proc = subprocess.Popen(
        [
            cli, "logs", log_producer.source,
            "--backend", backend,
            "--follow",
            "--raw",
        ],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        env=_env(),
    )
    try:
        seen = []
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline and len(seen) < 3:
            line = proc.stdout.readline()
            if not line:
                break
            if _LINE_RE.search(line):
                seen.append(line.rstrip("\n"))
    finally:
        proc.terminate()
        try:
            stderr = proc.communicate(timeout=5)[1]
        except subprocess.TimeoutExpired:
            proc.kill()
            stderr = proc.communicate()[1]

    assert len(seen) >= 3, (
        f"expected >=3 follow lines (backend={backend}), got {len(seen)}: "
        f"{seen!r} stderr={stderr!r}"
    )
