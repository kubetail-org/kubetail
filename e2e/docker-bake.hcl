# The Docker · Build cache scopes to read. CI sets it to the runner's arch.
variable "CACHE_ARCH" {
  default = "amd64"
}

group "default" {
  targets = ["dashboard", "cluster-api", "cluster-agent"]
}

target "dashboard" {
  context    = "../"
  dockerfile = "dashboard/Dockerfile"
  target     = "final"
  tags       = ["kubetail-dashboard:e2e"]
  cache-from = ["type=gha,scope=dashboard-${CACHE_ARCH}"]
}

target "cluster-api" {
  context    = "../"
  dockerfile = "cluster/api/Dockerfile"
  target     = "final"
  tags       = ["kubetail-cluster-api:e2e"]
  cache-from = ["type=gha,scope=cluster-api-${CACHE_ARCH}"]
}

target "cluster-agent" {
  context    = "../"
  dockerfile = "cluster/agent/Dockerfile"
  target     = "final"
  tags       = ["kubetail-cluster-agent:e2e"]
  cache-from = ["type=gha,scope=cluster-agent-${CACHE_ARCH}"]
}
