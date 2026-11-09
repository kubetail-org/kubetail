# cluster-api — Cluster API

A Go/Gin GraphQL server that runs in the cluster and answers log queries from the Cluster Agents: file metadata, records, grep, follow. It is registered as the Kubernetes APIService `v1.api.kubetail.com`, so every request for data arrives through the kube-apiserver, which has already authenticated the caller. [`modules/CLAUDE.md`](../CLAUDE.md) holds what every Go module follows; the root [`CLAUDE.md`](../../CLAUDE.md), what crosses areas.

**It always runs in the `cluster` environment**: one in-cluster connection manager, no kube contexts. The `kubeContext` arguments in the schema mirror the Dashboard's and are ignored.

**It cannot start without five certificate files**: `tls.cert-file` and `tls.key-file` for its listener, and `cluster-agent.tls.{cert-file,key-file,ca-file}` for dialling the agents. The listener is always TLS.

## Layout

| Path | Holds |
| --- | --- |
| `cmd/main.go` | Lifecycle only: flags, config, logging, the TLS server, shutdown. |
| `embed.go` | `static/`: `swagger.json`, `graphiql.html`, favicons. |
| `internal/app/app.go` | `App` and `NewApp`: the composition root and every route. |
| `internal/app/middleware.go` | `newAggregationAuthMiddleware`, the one authentication boundary, and `loadAggregationAuthConfig`. |
| `internal/app/ext.go`, `openapi.go` | The discovery and OpenAPI documents the kube-apiserver fetches from an aggregated API. |
| `internal/app/helpers.go` | `mustNewGrpcDispatcher`, the health handler. |
| `internal/app/download.go` | `POST …/download`. |
| `internal/helpers/tls.go` | `BuildTLSConfig`, `PoolFromPEM`. |
| `pkg/config/` | `Config`, `DefaultConfig`, `NewConfig`. |
| `graph/` | The GraphQL server (`modules/CLAUDE.md`, *The GraphQL servers*). `errors.go` holds `NewGrpcError`. |

## Config

`pkg/config.NewConfig(configPath, v)` reads the file, expands `${VAR}` from the environment, and unmarshals over `DefaultConfig()`. Keys are flat and top-level: `addr`, `base-path`, `gin-mode`, `allowed-namespaces`, `tls.*`, `cluster-agent.dispatch-url`, `cluster-agent.tls.*`, `logging.*`. A config that still sets a removed TLS key (`removedTLSKeys`) is refused with an error naming it. `config/default/cluster-api.yaml` matches the code's defaults; keep it so. → [ADR: authenticate the front proxy in middleware](../../docs/adr/2026-11-09-authenticate-the-front-proxy-in-middleware.md)

## Routes

Everything mounts under `cfg.BasePath`.

| Group | Middleware | Routes |
| --- | --- | --- |
| `/apis/api.kubetail.com/v1` | security headers, `newAggregationAuthMiddleware` | `/graphql`, `POST /download`, `GET /healthz` |
| `root` | none | `GET /`, `/healthz`, `/apis`, `/apis/api.kubetail.com/v1` (discovery), `/openapi/v2`, `/openapi/v3`, `/openapi/v3/apis/api.kubetail.com/v1`, `/graphiql`, favicons |

- **`/healthz` exists twice on purpose**: on `root` for the kubelet's probes, and in the group so a caller can check the whole chain through the aggregation layer.
- **The discovery and OpenAPI routes are what the kube-apiserver fetches from an aggregated API.** `ext.go` lists the group's routes as `APIResource`s (`graphql`, `download`, `healthz`); add a new route there too.
- **The group and version are spelled in several places**: `app.go`, `ext.go`, `openapi.go`, `static/swagger.json`, and the Dashboard's and CLI's clients (`APIServicePath`). The APIService object itself is a manifest (`hack/tilt/kubetail.yaml`, the e2e manifest, the chart), not code.

## GraphQL

`Query` has `logMetadataList` and `logRecordsFetch`; `Subscription` has `logMetadataWatch`, `logRecordsFollow` and `logSourcesWatch`, the last declared in the schema and not implemented: its resolver panics. There are no mutations. `gqlgen.yml` binds the `LogMetadata*` types to the protobuf messages in `shared/clusteragentpb` and `LogRecord`/`LogSource` to `shared/logs`.

- **Transports are SSE, WebSocket and POST, in that order.** SSE is registered before POST so a request with `Accept: text/event-stream` is not answered as a plain POST. The browser and the CLI use POST for a fetch and WebSocket for a follow.
- **The log operations mirror the Dashboard's** (root guide, *The GraphQL contracts*): change both schemas together, then run the UI's codegen.
- **`NewGrpcError`** turns an agent's error into `INTERNAL_SERVER_ERROR`; `logRecordsFollow` maps `Unavailable` to `ErrServiceUnavailable`.

## How logs are read

- **Sources are resolved from the Kubernetes API**, through the shared `logs.SourceWatcher` and the connection manager's informers, as the Dashboard does.
- **Records come from the agent on the source's node.** Every stream passes `logs.WithLogFetcher(logs.NewAgentLogFetcher(r.grpcDispatcher))`, which dials that node's agent for `LogRecordsService`.
- **Metadata fans out to every agent**: `grpcDispatcher.Fanout` for a list, `FanoutSubscribe` for a watch, over `LogMetadataService`.
- **The dispatcher** (`github.com/kubetail-org/grpc-dispatcher-go`) is built once in `mustNewGrpcDispatcher` from `cluster-agent.dispatch-url` (`kubernetes://<service>:<port>`). It dials with mutual TLS and the two impersonation interceptors. A bad certificate or URL is fatal at startup.

## Security invariants

The whole picture is the root guide's *Security invariants*. What an ordinary change here can break:

- **`newAggregationAuthMiddleware` is the only authentication.** The listener requests a client certificate and does not require one. A route added on `root` is open to anything that can reach the pod; a route that returns data goes in the group. → [ADR: authenticate the front proxy in middleware](../../docs/adr/2026-11-09-authenticate-the-front-proxy-in-middleware.md)
- **Only the kube-apiserver's front proxy is accepted.** The middleware verifies the peer's leaf certificate against the cluster's `requestheader-client-ca-file`, checks its CN against `requestheader-allowed-names` when that list is not empty, then reads the user, groups and extras from the request headers that ConfigMap names. **Only the leaf (`PeerCertificates[0]`) is verified**: the rest of the chain is whatever the client sent. **Groups are read with `Header.Values`**, since each group is a header of its own.
- **That configuration is read once**, from `kube-system/extension-apiserver-authentication` in `NewApp`.
- **The identity is an `*k8shelpers.ImpersonateInfo` on the request context**, and everything downstream depends on it staying there: client-go calls impersonate it, the informer check is keyed by it, and the gRPC interceptors (`shared/grpchelpers`) send it to the agent as `x-remote-user`, `x-remote-group` and `x-remote-extras`. With no identity on the context, the client-go calls and the informer check silently run as the Cluster API's ServiceAccount, which holds the `impersonate` permission; the agent call fails, since the agent requires `x-remote-user`. → [ADR: forward the caller's identity hop by hop](../../docs/adr/2026-11-09-forward-the-callers-identity-hop-by-hop.md)
- **This module authorizes nothing itself.** The Kubernetes API authorizes the impersonated calls and the agent authorizes log access. Keep both interceptors in the dial options: `TestImpersonateInfoOnContextReachesAgentAsRemoteHeaders` and its streaming sibling pin them.
- **A WebSocket upgrade carrying an `Origin` header is refused.** A legitimate one comes through the Dashboard's proxy, which strips it. If the upgrade carried `X-Forwarded-CSRF-Token`, the `connection_init` payload's `csrfToken` must match it.
- **`allowed-namespaces` is enforced here too**: `DerefNamespaceToList` for metadata, `logs.WithAllowedNamespaces` for streams and downloads.

## Tests

The shared rules are in `modules/CLAUDE.md`. Here:

- **`internal/app/testutils_test.go`** sets `gin.TestMode` and gives `NewTestConfig()` and `NewTestApp(cfg)`. Under test mode `NewApp` builds no connection manager and no dispatcher, **and mounts no authentication middleware**, so an app test says nothing about who may call a route.
- **The middleware is tested directly** in `internal/app/middleware_test.go`, with a CA made in the test (`newTestCA`, `requestWithCert`, `runMiddleware`). Add a case there for any change to who is accepted.
- **The GraphQL server** is `graph.NewServer(nil, nil, []string{})` behind `testutils.NewWebTestClient` (`graph/server_test.go`).
- **Identity reaching the agent** is tested against a real `grpc.NewServer()` on a loopback port (`startRecordingAgent`, `dialAgent` in `graph/schema.resolvers_test.go`).
- **References**: `TestAggregationAuth_RejectsTrustedCertAppendedAsChainEntry`, `TestAggregationAuth_ExtractsAllGroups`, `TestServerWebSocketCSRFInit`, `TestServerSSETransportServesQueries`, `TestDownloadIgnoresKubeContextFromForm`, `TestRejectsRemovedTLSFields`.

When you change a route, the authentication, the dispatcher or a config key, update this file in the same change.
