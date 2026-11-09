# dashboard — Dashboard backend

A Go/Gin HTTP server. It serves the built UI, a GraphQL API over the Kubernetes API (gqlgen), a few REST routes for sessions and downloads, and the proxy that carries the browser to the Cluster API. It runs in two places: as its own binary in the cluster (`cmd/main.go`), and inside the CLI, which imports `pkg/app` and `pkg/config` for `kubetail serve`. [`modules/CLAUDE.md`](../CLAUDE.md) holds what every Go module follows; the root [`CLAUDE.md`](../../CLAUDE.md), what crosses areas.

**`pkg/app` and `pkg/config` are the CLI's API.** `modules/cli/cmd/serve.go` calls `config.DefaultConfig`, `app.NewApp`, `NotifyShutdown`, `DrainWithContext` and `Close`. A change to those signatures, or to a default in `DefaultConfig`, changes `kubetail serve`.

```sh
go run cmd/main.go -c hack/config.yaml   # desktop environment on :4000, the port the UI dev server proxies to
```

## Layout

| Path | Holds |
| --- | --- |
| `cmd/main.go` | Lifecycle only: flags, config, logging, the HTTP server, shutdown. |
| `embed.go` | The three embedded trees: `templates/`, `static/`, `website/`. |
| `pkg/app/app.go` | `App` and `NewApp`: the composition root and every route. |
| `pkg/app/middleware.go` | CSRF, authentication, the WebSocket CSRF context. |
| `pkg/app/auth.go` | `LoginPOST`, `LogoutPOST`, `SessionGET`. |
| `pkg/app/helpers.go` | Session keys, `newClusterAPIProxy`, the `queryHelpers` seam (TokenReview). |
| `pkg/app/website.go` | The SPA shell: `index.tmpl`, the Vite manifest, the runtime config. |
| `pkg/app/download.go` | `POST /api/v1/download`. |
| `pkg/config/` | `Config`, `DefaultConfig`, `NewConfig`. |
| `pkg/preferences/` | The user's preferences file: `Preferences`, `Merge`, the file-backed `Store`. |
| `graph/` | The GraphQL server (`modules/CLAUDE.md`, *The GraphQL servers*). `schema.helpers.go` holds the hand-written helpers; `model/models_custom.go` the hand-written scalars and kubeconfig wrappers. |
| `internal/cluster-api/` | Package `clusterapi`: the `Proxy` (`DesktopProxy`, `InClusterProxy`) and the `HealthMonitor` that watches the APIService. |
| `internal/formerrors/` | Validator errors as form messages. |
| `templates/`, `static/` | `index.tmpl`; `graphiql.html`, `robots.txt`. |
| `website/` | The built UI, copied in by `make build-dashboard-ui`. Git tracks only `.gitkeep`. |

## Config

`pkg/config.NewConfig(v, configPath)` reads the file, expands `${VAR}` from the environment (the only way an environment variable reaches the config), and unmarshals over `DefaultConfig()`. **Keys are flat and top-level**: `addr`, `auth-mode`, `base-path`, `environment`, `kubeconfig`, `gin-mode`, `allowed-namespaces`, `allowed-origins`, `cluster-api-enabled`, `local-storage-dir`, `session.*`, `tls.*`, `ui.columns`, `logging.*`. `-p key:value` overrides one from the command line.

**`DefaultConfig()` is the truth for defaults.** It is the `cluster` environment on `:8080`; the CLI sets `desktop` unless it is run with `--in-cluster`. A new key gets a `mapstructure` tag, a default there, a validation tag where it can be wrong, and an entry in `config/default/dashboard.yaml`.

## Routes

Routes mount under `cfg.BasePath`. `NewApp` builds three nested groups, and which group a route joins decides what protects it:

| Group | Middleware it adds, in order | Routes |
| --- | --- | --- |
| `root` | none | `/healthz`, `/graphiql`, `/robots.txt`, `/assets/*`, favicons, `GET /`, and every unmatched path (the SPA shell, the engine's `NoRoute`) |
| `dynamicRoutes` | session cookie, security headers, `csrfProtectionMiddleware`, `authenticationMiddleware` | `/api/auth/login`, `/api/auth/logout`, `/api/auth/session` |
| `protectedRoutes` | `k8sAuthenticationMiddleware`, `websocketCSRFContextMiddleware` | `/graphql`, `/cluster-api-proxy/*path`, `POST /api/v1/download` |

- **`authenticationMiddleware` finds the token**: the session cookie in `auth-mode: token`, or an `Authorization: Bearer` header in either mode, which wins. **`k8sAuthenticationMiddleware` enforces it**: 401 in `token` mode without one, and it puts the token on the request context under `k8shelpers.K8STokenCtxKey`.
- **`auth-mode` is `auto` or `token`.** In `auto` a request with no token runs as the process: the kubeconfig user in `desktop`, the Dashboard's ServiceAccount in `cluster`.
- **Login validates the token with a `TokenReview`**, stores it in the cookie session and rotates the CSRF token. `SessionGET` always answers with an `X-CSRF-Token` header, minting the token on first use.
- **Session keys** are `session.key-pairs` from the config; else, in `desktop`, a file under `local-storage-dir` created on first run; else random per process, so sessions end with the pod.
- **The SPA shell needs `.vite/manifest.json` in `website/`.** Without a built UI every page route answers 404 `{"status":"website not found"}`; the API still works. `index.tmpl` writes `window.runtimeConfig`, the UI's `appConfig`.

## Kubernetes access

- **`App` owns one `k8shelpers.ConnectionManager`** and hands it to the GraphQL server and the proxy. Kubernetes clients come from it, with three exceptions that use credentials of their own: the Helm client (`desktop` only, the kubeconfig), the health monitor's aggregator clientset, and the authorizer's review client.
- **A resolver that takes `kubeContext` starts with `r.cm.DerefKubeContext(kubeContext)`** and passes the result down. In `desktop` an omitted one is the kubeconfig's current context. In `cluster` the result is always `""`, so a client's kube context is ignored; a non-empty one handed to the connection manager directly is an error.
- **Namespaces go through the allow-list.** A get calls `k8shelpers.DerefNamespace(r.allowedNamespaces, namespace, default)`. A list or a watch goes through `r.listResource` or `r.watchResourceMulti`. An omitted namespace is the context's default; the empty string means all, which with an allow-list fans out across the allowed namespaces, a list merging its pages under one `continue` token. Anything built from source paths passes `logs.WithAllowedNamespaces(r.allowedNamespaces)`.
- **Informers are shared and run on the process's credentials**, so `cm.NewInformer(ctx, kubeContext, token, namespace, gvr)` first checks the caller with a `SelfSubjectAccessReview` for `list` and `watch`. That check is the only thing between a caller and an informer's data: always pass the request's token. (`desktop` has one user, the kubeconfig's, and ignores it.) → [ADR: share informers and gate each caller with an access review](../../docs/adr/2026-11-09-share-informers-and-gate-each-caller-with-an-access-review.md)
- **Logs are read from the Kubernetes API here.** `logs.NewStream` with no `WithLogFetcher` uses `KubeLogFetcher`. This module never speaks gRPC; the agent-backed path is the Cluster API's, which the browser reaches through the proxy.
- **Some operations exist in `desktop` alone** and return `ErrForbidden` in `cluster`: `kubeConfigGet`, `kubeConfigWatch`, `helmInstallLatest`, `helmListReleases`. Preferences need a `local-storage-dir`.

## The Cluster API proxy

`/cluster-api-proxy/*` forwards to `/apis/api.kubetail.com/v1` on the kube-apiserver, never to the Cluster API's Service (root guide, *How a request travels*). `DesktopProxy` takes the kube context from the first path segment and authenticates with that context's kubeconfig credentials. `InClusterProxy` sends the session's token as `Authorization`, or falls back to the Dashboard's ServiceAccount. With `cluster-api-enabled: false` in `cluster` the route is a 503 stub. → [ADR: reach the Cluster API through the aggregation layer](../../docs/adr/2026-11-09-reach-the-cluster-api-through-the-aggregation-layer.md)

`HealthMonitor` watches the APIService `v1.api.kubetail.com` and maps its `Available` condition to the status the UI shows; `desktop` runs one worker per kube context.

## Security invariants

The whole picture is the root guide's *Security invariants*. What an ordinary change here can break:

- **A route on `root` has no session, no CSRF check and no authentication.** Anything that reads or changes a user's data joins `dynamicRoutes` or `protectedRoutes`. `TestCSRFProtectionAppliedToDynamicRoutes` and its siblings in `pkg/app/app_test.go` find the middleware by function name, so renaming one fails them.
- **CSRF is two layers** (`csrfProtectionMiddleware`): a request whose `Sec-Fetch-Site` is `cross-site` or `same-site` is refused, then `X-CSRF-Token` must equal the session's token, compared in constant time. A form post may send it as a `csrfToken` field instead. GET, HEAD and OPTIONS are skipped, so **the GraphQL server has no GET transport and no GET route changes state**. → [ADR: defend against CSRF in two layers](../../docs/adr/2026-11-09-defend-against-csrf-in-two-layers.md)
- **A WebSocket is gated twice**: `CheckOrigin` (same origin, or `allowed-origins`; a missing `Origin` is refused) and the `connection_init` payload's `csrfToken`, checked against the session's. The same-origin check ignores `X-Forwarded-*`; a deployment behind a proxy sets `allowed-origins`.
- **The proxies strip what the caller must not choose**: `Origin`, `Authorization`, `X-Forwarded-Authorization` and every `Impersonate-*` header are deleted before forwarding, a path with a `..` segment is refused before it is joined, and an upgrade is refused unless its origin is allowed and it carries the forwarded CSRF token. The Cluster API relies on each of these.
- **`websocketCSRFContextMiddleware` deletes a client's own `X-Forwarded-CSRF-Token`** before it sets the real one.
- **The token is trimmed before it is checked**, so a whitespace-only bearer does not pass for one.
- **`BypassNamespaceCheck` turns the allow-list off.** It is matched by pointer identity; never derive it from input.

## Tests

The shared rules are in `modules/CLAUDE.md`. Here:

- **`pkg/app/testutils_test.go`** sets `gin.TestMode` and gives `newTestConfig()` and `newTestApp(cfg)`. Under test mode `NewApp` builds no connection manager and no proxy (`app.cm` is nil, the proxy route is the 503 stub), and a test sets `app.queryHelpers = &mockQueryHelpers{}` or mounts its own handler on `app.dynamicRoutes` or `app.protectedRoutes`.
- **The GraphQL server** is stood up with `graph.NewServer(config.DefaultConfig(), nil)` behind `withSessionCSRFToken` and dialled with a `graphql-transport-ws` WebSocket (`graph/server_test.go`). A resolver is called directly on `&queryResolver{&Resolver{…}}` with the fakes it needs.
- **Seams are fields**: `App.queryHelpers`, `downloadHandlers.newLogStream`, `Resolver.helmReleaseLister`, the transport `newInClusterProxy` takes.
- **References**: `pkg/app/middleware_test.go` (CSRF tables), `pkg/app/websocket_test.go` (WebSocket CSRF end to end), `internal/cluster-api/proxy_test.go` (header stripping, path traversal, shutdown), `graph/schema.resolvers_test.go`'s `TestAllowedNamespacesGetQueries`, `pkg/preferences/store_test.go`.

When you change a route, a middleware, a config key or what the CLI imports, update this file in the same change.
