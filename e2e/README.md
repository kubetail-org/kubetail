# Kubetail E2E Tests

End-to-end tests for the kubetail dashboard and CLI. Tests run across three environments
in sequence:

- **kubernetes-api** — cluster tests with the kubernetes-api backend
- **kubetail-api** — cluster tests with the kubetail-api backend
- **cli** — tests that run the `kubetail` binary on the host

## Prerequisites

- [uv](https://docs.astral.sh/uv/)
- [Docker](https://docs.docker.com/get-docker/)
- [kind](https://kind.sigs.k8s.io)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)

## Configuration

Default values are in `.env`. Override any value by editing that file or setting environment variables before running.

| Variable          | Default                 | Description                          |
|-------------------|-------------------------|--------------------------------------|
| `DASHBOARD_URL`   | `http://localhost:9999` | URL of the cluster dashboard         |
| `CLUSTER_API_URL` | `https://localhost:9998` | URL of the kubetail cluster-api     |
| `SERVE_PORT`      | `9898`                  | Port used by `kubetail serve`        |
| `KUBETAIL_CLI`    | `../bin/kubetail`       | Path to the kubetail binary          |

## Run the full suite

From the repo root:

```sh
make e2e
```

This builds the CLI and Docker images, then runs all three environments in sequence,
creating and tearing down the kind cluster around each cluster suite.

## Test against a specific Kubernetes version

Pass `KIND_IMAGE` to target a specific Kubernetes version:

```sh
KIND_IMAGE=kindest/node:v1.21.14 make test-e2e
KIND_IMAGE=kindest/node:v1.25.16 make test-e2e
```

Omit `KIND_IMAGE` to use kind's default (latest supported) node image.

## Iterate on tests

Once images and the CLI are built, you can re-run pytest directly without rebuilding:

```sh
cd e2e && uv run pytest -v
```

## Clean up

The test suite tears down the cluster automatically after each run. If a run is interrupted or you want to clean up manually:

```sh
./e2e/scripts/down.sh
```

This stops any active port-forwards, deletes the kind cluster, and removes the temporary kubeconfig at `/tmp/kubetail-e2e.kubeconfig`.

## Manual cluster management

To bring up and tear down the cluster manually (e.g. for debugging):

```sh
# Build images and CLI first
docker buildx bake --allow=fs.read=.. --load --file e2e/docker-bake.hcl
make build

# Bring up
./e2e/scripts/up.sh --backend=kubernetes-api   # or --backend=kubetail-api

# Run a single suite
cd e2e && uv run pytest -v

# Tear down
./e2e/scripts/down.sh
```
