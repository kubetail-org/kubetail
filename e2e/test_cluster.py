import pytest

from conftest import assert_healthz


@pytest.mark.cluster
def test_dashboard_healthz(dashboard_url):
    assert_healthz(dashboard_url)


@pytest.mark.cluster
@pytest.mark.kubetail_api
def test_cluster_api_healthz(cluster_api_url):
    assert_healthz(cluster_api_url)


@pytest.mark.cluster
@pytest.mark.kubernetes_api
def test_kubernetes_api_example(dashboard_url):
    """Placeholder: runs only when --backend=kubernetes-api."""
    pass


@pytest.mark.cluster
@pytest.mark.kubetail_api
def test_kubetail_api_example(dashboard_url):
    """Placeholder: runs only when --backend=kubetail-api."""
    pass
