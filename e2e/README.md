# Kubetail E2E Tests

End-to-end tests for the kubetail dashboard and CLI. Tests run across three environments
in sequence:

- **kubernetes-api** — cluster tests with the kubernetes-api backend
- **kubetail-api** — cluster tests with the kubetail-api backend
- **cli** — tests that run the `kubetail` binary on the host

## Prerequisites

- [uv](https://docs.astral.sh/uv/)
- [Docker](https://docs.docker.com/get-docker/)
- [kind](https://kind.sigs.k8s.io/)
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

This builds the CLI and Docker images, brings up the kind cluster (reusing
it if already running), and runs all three environments in sequence. The
cluster persists after the run — tear it down with `e2e/scripts/down.sh`
when you're done.

## Iterate on tests

Once images and the CLI are built, you can re-run pytest directly without rebuilding:

```sh
cd e2e && uv run pytest -v
```

## Manual cluster management

To bring up and tear down the cluster manually (e.g. for debugging):

```sh
# Build images and CLI first
./e2e/scripts/build.sh

# Bring up
./e2e/scripts/up.sh

# Run a single suite
cd e2e && uv run pytest -v

# Tear down
./e2e/scripts/down.sh
```
