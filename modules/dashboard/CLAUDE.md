# dashboard — Dashboard backend

A Go/Gin HTTP server: the built UI, a GraphQL API over the Kubernetes API (gqlgen), REST routes for sessions and downloads, and the proxy to the Cluster API. It runs as its own binary in the cluster (`cmd/main.go`) and inside the CLI for `kubetail serve`.

- **`pkg/app` and `pkg/config` are the CLI's API.** `modules/cli/cmd/serve.go` calls `config.DefaultConfig`, `app.NewApp`, `NotifyShutdown`, `DrainWithContext` and `Close`. A change to those signatures, or to a default in `DefaultConfig`, changes `kubetail serve`.
- **`pkg/app/app.go` is the composition root and holds every route.** `internal/cluster-api/` is package `clusterapi`. Hand-written GraphQL code sits in `graph/schema.helpers.go` and `graph/model/models_custom.go`.
- **`website/` is the built UI**, copied in by `make build-dashboard-ui`; git tracks only `.gitkeep`. Without it every page route answers 404 `website not found`, and the API still works.

## Config

`pkg/config.NewConfig` reads the file, expands `${VAR}` from the environment (the only way an environment variable reaches the config), and unmarshals over `DefaultConfig()`, the truth for defaults: the `cluster` environment on `:8080`. Keys are flat and top-level. A new key gets a `mapstructure` tag, a default there, a validation tag where it can be wrong, and an entry in `config/default/dashboard.yaml`.

## Routes

Routes mount under `cfg.BasePath`. `NewApp` builds three nested groups, and which group a route joins decides what protects it:

| Group | Middleware it adds, in order | Routes |
| --- | --- | --- |
| `root` | none | health, static assets, `/graphiql`, and the SPA shell on `/` and every unmatched path |
| `dynamicRoutes` | session cookie, security headers, `csrfProtectionMiddleware`, `authenticationMiddleware` | `/api/auth/*` |
| `protectedRoutes` | `k8sAuthenticationMiddleware`, `websocketCSRFContextMiddleware` | `/graphql`, `/cluster-api-proxy/*path`, `POST /api/v1/download` |

- **`authenticationMiddleware` finds the token**: the session cookie in `auth-mode: token`, or an `Authorization: Bearer` header in either mode, which wins. **`k8sAuthenticationMiddleware` enforces it**: 401 in `token` mode without one, and it puts the token on the request context under `k8shelpers.K8STokenCtxKey`.
- **In `auth-mode: auto` a request with no token runs as the process**: the kubeconfig user in `desktop`, the Dashboard's ServiceAccount in `cluster`.

## Kubernetes access

- **Kubernetes clients come from the `App`'s one `k8shelpers.ConnectionManager`**, with three exceptions that use credentials of their own: the Helm client (`desktop` only), the health monitor's aggregator clientset, and the authorizer's review client.
- **A resolver that takes `kubeContext` starts with `r.cm.DerefKubeContext(kubeContext)`** and passes the result down. In `cluster` the result is always `""`; a non-empty kube context handed to the connection manager directly is an error.
- **Namespaces go through the allow-list.** A get calls `k8shelpers.DerefNamespace(r.allowedNamespaces, namespace, default)`. A list or a watch goes through `r.listResource` or `r.watchResourceMulti`. An omitted namespace is the context's default; the empty string means all, which with an allow-list fans out across the allowed namespaces. Anything built from source paths passes `logs.WithAllowedNamespaces(r.allowedNamespaces)`.
- **Informers are shared and run on the process's credentials**, so `cm.NewInformer(ctx, kubeContext, token, namespace, gvr)` first checks the caller with a `SelfSubjectAccessReview`. That check is the only thing between a caller and an informer's data: always pass the request's token. → [ADR: share informers and gate each caller with an access review](../../docs/adr/2026-11-09-share-informers-and-gate-each-caller-with-an-access-review.md)
- **Logs are read from the Kubernetes API here** (`KubeLogFetcher`, the default of `logs.NewStream`). This module never speaks gRPC; the agent-backed path is the Cluster API's, which the browser reaches through the proxy.
- **An operation that exists in `desktop` alone returns `ErrForbidden` in `cluster`** (`kubeConfigGet`, `helmListReleases`…). A new one does the same.

## The Cluster API proxy

`/cluster-api-proxy/*` forwards to `/apis/api.kubetail.com/v1` on the kube-apiserver, never to the Cluster API's Service. `DesktopProxy` takes the kube context from the first path segment and authenticates with that context's kubeconfig credentials. `InClusterProxy` sends the session's token as `Authorization`, or falls back to the Dashboard's ServiceAccount. With `cluster-api-enabled: false` in `cluster` the route is a 503 stub. `HealthMonitor` maps the `Available` condition of the APIService `v1.api.kubetail.com` to the status the UI shows.

## Security invariants

What an ordinary change here can break, beside the root guide's list:

- **A route on `root` has no session, no CSRF check and no authentication.** Anything that reads or changes a user's data joins `dynamicRoutes` or `protectedRoutes`. The tests in `pkg/app/app_test.go` find the middleware by function name, so renaming one fails them.
- **CSRF is two layers** (`csrfProtectionMiddleware`): a request whose `Sec-Fetch-Site` is `cross-site` or `same-site` is refused, then `X-CSRF-Token`, or a form post's `csrfToken` field, must equal the session's token, compared in constant time. GET, HEAD and OPTIONS are skipped, so **the GraphQL server has no GET transport and no GET route changes state**. → [ADR: defend against CSRF in two layers](../../docs/adr/2026-11-09-defend-against-csrf-in-two-layers.md)
- **A WebSocket is gated twice**: `CheckOrigin` (same origin, or `allowed-origins`; a missing `Origin` is refused) and the `connection_init` payload's `csrfToken`. The same-origin check ignores `X-Forwarded-*`; a deployment behind a proxy sets `allowed-origins`.
- **The proxies strip what the caller must not choose**: `Origin`, `Authorization`, `X-Forwarded-Authorization` and every `Impersonate-*` header are deleted before forwarding, a path with a `..` segment is refused before it is joined, and an upgrade is refused unless its origin is allowed and it carries the forwarded CSRF token. The Cluster API relies on each of these. → [ADR: reach the Cluster API through the aggregation layer](../../docs/adr/2026-11-09-reach-the-cluster-api-through-the-aggregation-layer.md)
- **`websocketCSRFContextMiddleware` deletes a client's own `X-Forwarded-CSRF-Token`** before it sets the real one.
- **The token is trimmed before it is checked**, so a whitespace-only bearer does not pass for one.
- **`BypassNamespaceCheck` turns the allow-list off.** It is matched by pointer identity; never derive it from input.

## Tests

- **`pkg/app/testutils_test.go`** gives `newTestConfig()` and `newTestApp(cfg)`, which builds the app with no connection manager and no proxy (`app.cm` is nil, the proxy route is the 503 stub) and a `mockQueryHelpers` with no expectations. A test sets its expectations on `app.queryHelpers` or mounts its own handler on `app.dynamicRoutes` or `app.protectedRoutes`.
- **The GraphQL server** is stood up with `graph.NewServer(cfg, nil, clusterapi.NewHealthMonitor(cfg, nil))` behind `withSessionCSRFToken` and dialled with a `graphql-transport-ws` WebSocket (`graph/server_test.go`). A resolver is called directly on `&queryResolver{&Resolver{…}}` with the fakes it needs.
- **Seams are fields and parameters**: the dependencies `newApp` takes, the health monitor `graph.NewServer` takes (`internal/cluster-api/mock.MockHealthMonitor`), `downloadHandlers.newLogStream`, `Resolver.helmReleaseLister`, the transport `newInClusterProxy` takes.
- **The tests to extend for a boundary**: `pkg/app/middleware_test.go` (CSRF), `pkg/app/websocket_test.go` (WebSocket CSRF end to end), `internal/cluster-api/proxy_test.go` (header stripping, path traversal), `TestAllowedNamespacesGetQueries` in `graph/schema.resolvers_test.go`.
