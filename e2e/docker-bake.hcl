group "default" {
  targets = ["dashboard", "cluster-api", "cluster-agent"]
}

target "dashboard" {
  context    = "../"
  dockerfile = "build/package/Dockerfile.dashboard"
  target     = "final"
  tags       = ["kubetail-dashboard:e2e"]
  cache-from = ["type=gha,scope=dashboard-final-amd64"]
}

target "cluster-api" {
  context    = "../"
  dockerfile = "build/package/Dockerfile.cluster-api"
  target     = "final"
  tags       = ["kubetail-cluster-api:e2e"]
  cache-from = ["type=gha,scope=cluster-api-final-amd64"]
}

target "cluster-agent" {
  context    = "../"
  dockerfile = "build/package/Dockerfile.cluster-agent"
  target     = "final"
  tags       = ["kubetail-cluster-agent:e2e"]
  cache-from = ["type=gha,scope=cluster-agent-final-amd64"]
}
