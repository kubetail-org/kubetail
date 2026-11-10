# shared — packages the other Go modules import

No binary ships from here. `cli`, `dashboard` and `cluster-api` import it, and it imports none of them.

- **A change here changes every binary.** Run the workspace's tests (`go test -race github.com/kubetail-org/kubetail/modules/...` from `modules/`), not this module's alone. An exported signature here is an API with three callers: change it and them in one commit.
- **A new package goes here only when two modules need it.** Code one module uses stays in that module's `internal/`.
- **`config` holds `Environment` and its two values, nothing else**: each module owns its config struct and loader.
- **Look here before writing a helper**: `util` (`SyncMap`, `SyncGroup`), `httphelpers` (`IsSameOrigin`, `IsAllowedOrigin`), `ginhelpers`, `middleware` (the access log), `logging`, `graphql/errors`, `testutils`.
- **Nothing imports `testpb` or `otel`.**

## k8shelpers

- **`ConnectionManager` is the one way to a Kubernetes client.** `NewConnectionManager(env, opts...)` returns `DesktopConnectionManager`, which caches clients per kube context and watches the kubeconfig, or `InClusterConnectionManager`, for which any non-empty kube context is an error and `DerefKubeContext` returns `""`. The desktop one connects to every context in the background unless built with `WithLazyConnect(true)`, as the CLI's `logs` does.
- **Identity is put on by the transport, from the request's context** (`transport.go`, `keys.go`). `BearerTokenRoundTripper` sets `Authorization` from `K8STokenCtxKey`. `ImpersonatingRoundTripper`, in the cluster alone, sets the `Impersonate-*` headers from `K8SImpersonateCtxKey` (an `*ImpersonateInfo`). With neither on the context, a call runs as the process: the kubeconfig user, or the ServiceAccount.
- **`NewInformer(ctx, kubeContext, token, namespace, gvr)` checks the caller before it hands out a shared informer.** Informer factories run on the manager's own credentials, so the authorizer (`authorizer.go`) first runs a `SelfSubjectAccessReview` for `list` and `watch` as the caller. An allowed answer is cached for five minutes; a denial is never cached. In the cluster the cache is keyed by the token or the whole impersonated identity, which `TestInClusterAuthorizer_IsAllowedInformer_CachePerImpersonated*` pin. On the desktop the `token` argument is ignored. → [ADR: share informers and gate each caller with an access review](../../docs/adr/2026-11-09-share-informers-and-gate-each-caller-with-an-access-review.md)
- **The namespace allow-list is `DerefNamespace` and `DerefNamespaceToList`** (`namespaces.go`). Given the allowed namespaces, the requested one and a default, they answer the namespace or namespaces to use, or `ErrForbidden` for one outside the list. `BypassNamespaceCheck` turns the check off and is matched by pointer identity, so it can only be passed deliberately.

## logs

`NewStream(ctx, cm, sourcePaths, opts...)` builds a `Stream` (`Sources()`, `Records()`, `Err()`, `Close()`); the caller then calls `Start(ctx)`, once.

- **A source path** is `[namespace:]<type>/<name>[/<container>]` or a bare pod name, with `*` as a wildcard (`parsePath` in `source-watcher.go`, the type aliases in `helpers.go`).
- **`SourceWatcher` turns paths into containers**, following owner references from a workload down to its pods. A container on a node it has not seen is skipped, so the caller needs to list and watch nodes. A path outside the allowed namespaces is dropped, not refused.
- **`LogFetcher` reads one container.** `KubeLogFetcher` is the default; only the Cluster API passes `AgentLogFetcher`, with `WithLogFetcher`. The two must agree on ordering, time bounds and grep, and nothing checks that they do. → [ADR: read logs through two backends](../../docs/adr/2026-11-09-read-logs-through-two-backends-behind-logfetcher.md)
- **A stream merges its sources in time order**: past records first, then the records that arrived while it was reading them, sorted and de-duplicated, then live ones. A fetch error ends the whole stream and is reported by `Err()`.
- **An `Option` is `func(target any) error` applied to both the stream and its source watcher.** Each option type-switches on its target and does nothing for one it does not handle. A new option needs a case for the right target, or it is silently ignored.
- **Modes**: `WithHead`, `WithTail` and `WithAll` each set the mode and the last one wins; unset behaves as tail. Head with follow is an error.
- **`WithGrep` is case-insensitive** (it prefixes `(?i)`) and lets each space also match ANSI colour sequences. With `KubeLogFetcher` the stream filters in this process; with `AgentLogFetcher` the pattern goes to the agent.

## grpchelpers

`ImpersonateUnaryClientInterceptor` and `ImpersonateStreamClientInterceptor` copy the `*ImpersonateInfo` on the context into `x-remote-user`, `x-remote-group` and `x-remote-extras` metadata, extras as JSON in the one header. The agent's side is `crates/cluster_agent/src/auth.rs`; change the two together.

## Security invariants

What an ordinary change here can break, beside the root guide's list:

- **An identity reaches Kubernetes and the agents through the context keys in `keys.go`.** Anything that replaces a request's context, or builds a client around the connection manager, drops it. The one explicit path beside them is the bearer token given to `NewInformer` (`logs.WithBearerToken`) for the informer check.
- **`NewInformer` calls the authorizer before it returns.** A new path to informer data makes the same check.
- **Each round-tripper in `transport.go` clones the request before it sets headers**, since a `RoundTripper` must not modify its request. A new one does the same.
- **`IsSameOrigin` ignores `X-Forwarded-*`** (`httphelpers`).
- **The access log records request metadata (the query string included) and no `Authorization`, cookie or body** (`middleware.LoggingMiddleware`). Keep it so.

## Tests

- **Fakes**: `k8s.io/client-go/kubernetes/fake` with reactors for the Kubernetes API (`k8shelpers/authorizer_test.go`); `mockLogFetcher` and `mockSourceWatcher` for a stream (`logs/stream_test.go`); `httptest` behind a URL field for the GitHub client (`versioncheck/github_test.go`).
- **`k8shelpers/mock`, `logs/mock` and `versioncheck/mock` are for other packages' tests.**
- **A watcher is awaited, not slept on**: `k8shelpers/kube-config-watcher_test.go` waits on a `sync.WaitGroup` its subscription feeds.
