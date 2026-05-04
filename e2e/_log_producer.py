"""Shared constants and helpers for the e2e log-producer fixture.

Used by test_logs_backends.py, test_cluster_api_websocket_auth.py, and the
`log_producer` fixture in conftest.py.
"""

import string
from dataclasses import dataclass
from pathlib import Path

LP_NS = "e2e-log-producer"
LP_NAME = "log-producer"
LP_LINE_PREFIX = "kubetail-e2e-line"
POD_IMAGE = "busybox:1.36"

_MANIFEST_PATH = Path(__file__).parent / "manifests" / "log_producer.yaml.tmpl"


@dataclass(frozen=True)
class LogProducer:
    namespace: str
    name: str
    line_prefix: str

    @property
    def source(self) -> str:
        """The kubetail logs source path: '<ns>:deployment/<name>'."""
        return f"{self.namespace}:deployment/{self.name}"


def rendered_manifest() -> str:
    return string.Template(_MANIFEST_PATH.read_text()).substitute(
        LP_NS=LP_NS,
        LP_NAME=LP_NAME,
        LP_LINE_PREFIX=LP_LINE_PREFIX,
        POD_IMAGE=POD_IMAGE,
    )
