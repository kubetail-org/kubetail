import subprocess

import pytest

from conftest import assert_healthz


@pytest.mark.cli
def test_version(cli):
    result = subprocess.run([cli, "--version"], capture_output=True, text=True)
    assert result.returncode == 0
    assert "kubetail" in result.stdout.lower()


@pytest.mark.cli
def test_serve_healthz(serve_url):
    assert_healthz(serve_url)
