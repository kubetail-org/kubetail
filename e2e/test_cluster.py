from conftest import assert_healthz


def test_dashboard_healthz(dashboard_url):
    assert_healthz(dashboard_url)


def test_cluster_api_healthz(cluster_api_url):
    assert_healthz(cluster_api_url)
