# cluster-api — Cluster API

A Go/Gin GraphQL server that runs in the cluster and answers log queries from the Cluster Agents: file metadata, records, grep, follow. It is the Kubernetes APIService `v1.api.kubetail.com`, so every request for data arrives through the kube-apiserver, which has already authenticated the caller.

- **It always runs in the `cluster` environment.** The `kubeContext` arguments in the schema mirror the Dashboard's and are ignored.
- **It cannot start without five certificate files**: `tls.cert-file` and `tls.key-file` for its listener, which is always TLS, and `cluster-agent.tls.{cert-file,key-file,ca-file}` for dialling the agents.
- **`internal/app/app.go` is the composition root and holds every route.**
- **Config**: `pkg/config.NewConfig` reads the file, expands `${VAR}` from the environment, and unmarshals over `DefaultConfig()`. Keys are flat and top-level. `config/default/cluster-api.yaml` matches the code's defaults; keep it so. A config that sets a removed TLS key (`removedTLSKeys`) is refused at startup.

## Routes

Everything mounts under `cfg.BasePath`.

| Group | Middleware | Routes |
| --- | --- | --- |
| `/apis/api.kubetail.com/v1` | security headers, `newAggregationAuthMiddleware` | `/graphql`, `POST /download`, `GET /healthz` |
| `root` | none | `/healthz`, discovery (`/apis`, `/apis/api.kubetail.com/v1`), OpenAPI, `/graphiql`, favicons |

- **`/healthz` exists twice on purpose**: on `root` for the kubelet's probes, and in the group so a caller can check the whole chain through the aggregation layer.
- **A new route in the group is also listed in `ext.go`**, among the `APIResource`s the kube-apiserver fetches for discovery.
- **The group and version are spelled in several places**: `app.go`, `ext.go`, `openapi.go`, `static/swagger.json`, and the Dashboard's and CLI's clients (`APIServicePath`). The APIService object itself is a manifest (`hack/tilt/kubetail.yaml`, the e2e manifest, the chart), not code.

## GraphQL and logs

- **There are no mutations.** Watching log sources is the Dashboard's job (`logSourcesWatch`): this schema has no such field.
- **Transports are SSE, WebSocket and POST, in that order.** SSE is registered before POST so a request with `Accept: text/event-stream` is not answered as a plain POST.
- **`NewGrpcError`** turns an agent's error into `INTERNAL_SERVER_ERROR`; `logRecordsFollow` maps `Unavailable` to `ErrServiceUnavailable`.
- **Records come from the agent on the source's node.** Every stream passes `logs.WithLogFetcher(logs.NewAgentLogFetcher(r.grpcDispatcher))`. Sources are resolved from the Kubernetes API, as in the Dashboard.
- **Metadata fans out to every agent**: `grpcDispatcher.Fanout` for a list, `FanoutSubscribe` for a watch.
- **The dispatcher** (`github.com/kubetail-org/grpc-dispatcher-go`) is built once in `mustNewGrpcDispatcher` from `cluster-agent.dispatch-url` (`kubernetes://<service>:<port>`). It dials with mutual TLS and the two impersonation interceptors. A bad certificate or URL is fatal at startup.

## Security invariants

What an ordinary change here can break, beside the root guide's list:

- **`newAggregationAuthMiddleware` is the only authentication.** The listener requests a client certificate and does not require one. A route added on `root` is open to anything that can reach the pod; a route that returns data goes in the group. → [ADR: authenticate the front proxy in middleware](../../docs/adr/2026-11-09-authenticate-the-front-proxy-in-middleware.md)
- **Only the kube-apiserver's front proxy is accepted.** The middleware verifies the peer's leaf certificate against the cluster's `requestheader-client-ca-file`, checks its CN against `requestheader-allowed-names` when that list is not empty, then reads the user, groups and extras from the request headers. **Only the leaf (`PeerCertificates[0]`) is verified**: the rest of the chain is whatever the client sent. **Groups are read with `Header.Values`**, since each group is a header of its own.
- **The identity is an `*k8shelpers.ImpersonateInfo` on the request context**, and everything downstream depends on it staying there: client-go calls impersonate it, the informer check is keyed by it, and the gRPC interceptors send it to the agent. With no identity on the context, the client-go calls and the informer check silently run as the Cluster API's ServiceAccount, which holds the `impersonate` permission.
- **This module authorizes nothing itself.** The Kubernetes API authorizes the impersonated calls and the agent authorizes log access. Keep both interceptors in the dial options: `TestImpersonateInfoOnContextReachesAgentAsRemoteHeaders` and its streaming sibling pin them.
- **A WebSocket upgrade carrying an `Origin` header is refused.** A legitimate one comes through the Dashboard's proxy, which strips it. If the upgrade carried `X-Forwarded-CSRF-Token`, the `connection_init` payload's `csrfToken` must match it.
- **`allowed-namespaces` is enforced here too**: `DerefNamespaceToList` for metadata, `logs.WithAllowedNamespaces` for streams and downloads.

## Tests

- **`NewApp` builds its dependencies from the cluster and hands them to `newApp`**, which tests call with fakes (`appDeps`). Both mount the same routes and middleware.
- **`internal/app/testutils_test.go`** gives `NewTestConfig()`, `NewTestApp(cfg)` and `newTestAppWithAuth(cfg, authCfg)`. Each builds no connection manager and no dispatcher, and mounts the real authentication middleware. `NewTestApp`'s trusts no CA, so it rejects every request to the group; to reach one, use `newTestAppWithAuth` with `newTestAuthCfg` and send `newFrontProxyRequest`.
- **The middleware is tested directly** in `internal/app/middleware_test.go`, with a CA made in the test (`newTestCA`, `requestWithCert`, `runMiddleware`). Add a case there for any change to who is accepted.
- **The GraphQL server** is `graph.NewServer(nil, nil, []string{})` behind `testutils.NewWebTestClient` (`graph/server_test.go`).
- **Identity reaching the agent** is tested against a real `grpc.NewServer()` on a loopback port (`startRecordingAgent`, `dialAgent` in `graph/schema.resolvers_test.go`).
