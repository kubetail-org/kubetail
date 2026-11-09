# e2e — end-to-end tests

Python tests (pytest, run with uv) against a kind cluster.

```sh
make test-e2e                                            # from the repo root: build, deploy, run
cd e2e && uv run pytest -v                               # rerun against what is deployed
cd e2e && uv run ruff check . && uv run ruff format --check .
```

- **`make test-e2e`** builds the CLI and the three images, creates or reuses the kind cluster `kubetail-e2e`, deploys `manifests/base.yaml.tmpl`, port-forwards and runs the tests. Beside the Go and pnpm toolchains it needs Docker with buildx, kind, kubectl, uv, `envsubst` and `curl`. `ONLY_BUILD=1` rebuilds without deploying, `scripts/up.sh` loads the new images into the cluster and `scripts/down.sh` deletes it.
- **The suite is where the boundaries between components are tested for real**: token auth, CSRF, WebSocket auth through the proxy, `allowed-namespaces`, namespace RBAC, and mTLS between the Cluster API and the agents (`test_zero_trust.py`). A change to one of those boundaries updates its test.
- **CI runs the suite on three Kubernetes versions, the oldest 1.21**, and only when a PR touches `modules/`, `crates/` and `dashboard-ui/` together.
