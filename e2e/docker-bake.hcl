group "default" {
  targets = ["dashboard", "cluster-api", "cluster-agent"]
}

target "dashboard" {
  context    = "../"
  dockerfile = "build/package/Dockerfile.dashboard"
  target     = "final"
  tags       = ["kubetail-dashboard:e2e"]
}

target "cluster-api" {
  context    = "../"
  dockerfile = "build/package/Dockerfile.cluster-api"
  target     = "final"
  tags       = ["kubetail-cluster-api:e2e"]
}

target "cluster-agent" {
  context    = "../"
  dockerfile = "build/package/Dockerfile.cluster-agent"
  target     = "final"
  tags       = ["kubetail-cluster-agent:e2e"]
}
