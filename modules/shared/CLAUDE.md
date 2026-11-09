# shared — packages the other Go modules import

No binary ships from here. `cli`, `dashboard` and `cluster-api` import it, and it imports none of them. [`modules/CLAUDE.md`](../CLAUDE.md) holds what every Go module follows; the root [`CLAUDE.md`](../../CLAUDE.md), what crosses areas.

**A change here changes every binary.** Run the workspace's tests (`go test -race github.com/kubetail-org/kubetail/modules/...` from `modules/`), not this module's alone. An exported signature here is an API with three callers: change it and them in one commit.

## Packages

| Package | Holds | Used by |
| --- | --- | --- |
| `k8shelpers` | Kubernetes access: the connection managers, the informer authorizers, the round-trippers that carry identity, the kubeconfig watcher, the namespace allow-list, the identity context keys. | all three |
| `logs` | Log streaming: `Stream`, `SourceWatcher`, the two `LogFetcher`s, the options, source parsing, download writers. | all three |
| `config` | `Environment` and its two values, `EnvironmentDesktop` and `EnvironmentCluster`. Nothing else: each module owns its own config struct and loader. | all three |
| `graphql/errors` | The `Err*` values and their codes (`KUBETAIL_FORBIDDEN`…). | `dashboard`, `cluster-api`, `k8shelpers`, the other `graphql` packages |
| `graphql/directives`, `graphql/model` | `@validate` and `@nullIfValidationFailed`; the `Int64` and timestamp scalars. | `dashboard`, `cluster-api` |
| `grpchelpers` | The client interceptors that send the caller's identity to the Cluster Agent. | `cluster-api` |
| `clusteragentpb` | Generated gRPC code for `proto/cluster_agent.proto`. | `cluster-api`, `logs` |
| `helm` | The Helm client behind `kubetail cluster …`: install, upgrade, uninstall, list, the chart repository. | `cli`, `dashboard` |
| `versioncheck` | `Checker`: the latest release, from the GitHub releases API. | `cli`, `dashboard` |
| `logging` | `ConfigureLogger`, the one place the global zerolog logger is set up. | all three |
| `middleware` | Gin middleware: the access log, cache-control. | `dashboard`, `cluster-api` |
| `httphelpers`, `ginhelpers` | `IsSameOrigin`, `IsAllowedOrigin`; `IsWebSocketRequest`. | `dashboard`, `cluster-api` |
| `util` | `SyncMap`, `SyncGroup` (run once per key), `UserAgent`. | `k8shelpers`, `dashboard` |
| `testutils` | `WebTestClient` (`modules/CLAUDE.md`, *Tests*). | tests in `dashboard`, `cluster-api` |
| `testpb`, `otel` | Generated code for `proto/test.proto`; an OpenTelemetry tracer setup. Nothing imports either. | none |

A new package goes here only when two modules need it. Code one module uses stays in that module's `internal/`.

## k8shelpers

**`ConnectionManager` is the one way to a Kubernetes client.** `NewConnectionManager(env, opts...)` returns the implementation for the environment:

- **`DesktopConnectionManager`** reads the kubeconfig, watches it for changes (`KubeConfigWatcher`), and caches a rest config, a clientset and a dynamic client per kube context. It connects to every context in the background unless built with `WithLazyConnect(true)`, as the CLI's `logs` does. A missing kubeconfig is an error at construction.
- **`InClusterConnectionManager`** has one rest config from the pod's ServiceAccount. Any non-empty kube context is an error, and `DerefKubeContext` returns `""`.

**Identity is put on by the transport, from the request's context** (`transport.go`, `keys.go`). `BearerTokenRoundTripper` sets `Authorization` from `K8STokenCtxKey`. `ImpersonatingRoundTripper`, in the cluster alone, sets the `Impersonate-*` headers from `K8SImpersonateCtxKey` (an `*ImpersonateInfo`). With neither on the context, a call runs as the process: the kubeconfig user, or the ServiceAccount.

**`NewInformer(ctx, kubeContext, token, namespace, gvr)` checks the caller before it hands out a shared informer.** Informer factories are shared per kube context and namespace and run on the manager's own credentials, so the authorizer (`authorizer.go`) first runs a `SelfSubjectAccessReview` for `list` and `watch` as the caller. An allowed answer is cached for five minutes; a denial is never cached. In the cluster the cache is keyed by the token or the whole impersonated identity, which `TestInClusterAuthorizer_IsAllowedInformer_CachePerImpersonated*` pin. On the desktop the caller is always the kubeconfig's user: the `token` argument is ignored and the key is the namespace, resource and verb alone. → [ADR: share informers and gate each caller with an access review](../../docs/adr/2026-11-09-share-informers-and-gate-each-caller-with-an-access-review.md)

**The namespace allow-list is `DerefNamespace` and `DerefNamespaceToList`** (`namespaces.go`). Given the allowed namespaces, the requested one and a default, they answer the namespace or namespaces to use, or `ErrForbidden` for one outside the list. `BypassNamespaceCheck` turns the check off and is matched by pointer identity, so it can only be passed deliberately.

## logs

**`Stream` is what a consumer reads**: `Sources()`, `Records()` (a channel), `Err()`, `Close()`. `NewStream(ctx, cm, sourcePaths, opts...)` builds one; the caller then calls `Start(ctx)`, once.

- **A source path** is `[namespace:]<type>/<name>[/<container>]` or a bare pod name, with `*` as a wildcard (`parsePath` in `source-watcher.go`, the type aliases in `helpers.go`).
- **`SourceWatcher` turns paths into containers.** It runs an informer per namespace and workload type plus one on nodes, follows owner references from a workload down to its pods, and publishes `ADDED` and `DELETED` sources. A container on a node it has not seen is skipped, so the caller needs to list and watch nodes. A path outside the allowed namespaces is dropped, not refused.
- **`LogFetcher` reads one container**: `StreamForward` and `StreamBackward`. `KubeLogFetcher` uses the pod log endpoint and is the default. `AgentLogFetcher` asks the Cluster Agent on the source's node over gRPC; only the Cluster API passes it, with `WithLogFetcher`.
- **A stream merges its sources in time order**: past records first, then the records that arrived while it was reading them, sorted and de-duplicated, then live ones. A fetch error ends the whole stream and is reported by `Err()`.
- **An `Option` is `func(target any) error` applied to both the stream and its source watcher.** Each option type-switches on its target and does nothing for one it does not handle. A new option needs a case for the right target, or it is silently ignored.
- **Modes**: `WithHead`, `WithTail` and `WithAll` each set the mode and the last one wins; unset behaves as tail. Head with follow is an error. `WithSince` and `WithUntil` take times; `ParseTimeArg` turns a flag into one, reading an ISO 8601 duration as that long ago.
- **`WithGrep` is case-insensitive** (it prefixes `(?i)`) and lets each space also match ANSI colour sequences. With `KubeLogFetcher` the stream filters in this process; with `AgentLogFetcher` the pattern goes to the agent.

## grpchelpers and the generated code

`ImpersonateUnaryClientInterceptor` and `ImpersonateStreamClientInterceptor` copy the `*ImpersonateInfo` on the context into `x-remote-user`, `x-remote-group` and `x-remote-extras` metadata, extras as JSON in the one header. The agent's side is `crates/cluster_agent/src/auth.rs`; change the two together. → [ADR: forward the caller's identity hop by hop](../../docs/adr/2026-11-09-forward-the-callers-identity-hop-by-hop.md)

`clusteragentpb/*.pb.go` and `testpb/*.pb.go` are protoc output. The hand-written file beside them holds only the `//go:generate` line. Regenerate with `go generate ./...` here after editing `proto/` (root guide, *The gRPC contract*); never hand-edit them.

## Security invariants

The whole picture is the root guide's *Security invariants*. What an ordinary change here can break:

- **An identity reaches Kubernetes and the agents through the context keys in `keys.go`.** Anything that replaces a request's context, or builds a client around the connection manager, drops it. The one explicit path beside them is the bearer token given to `NewInformer` (`logs.WithBearerToken`) for the informer check.
- **`NewInformer` calls the authorizer before it returns.** A new path to informer data makes the same check.
- **`ImpersonatingRoundTripper` clones the request before it adds headers.** A new round-tripper does the same.
- **`IsSameOrigin` ignores `X-Forwarded-*`** (`httphelpers`). A deployment behind a proxy names its origins in `allowed-origins`. → [ADR: defend against CSRF in two layers](../../docs/adr/2026-11-09-defend-against-csrf-in-two-layers.md)
- **The access log records request metadata (method, path, query string, forwarding headers, user agent, status, duration) and no `Authorization`, cookie or body** (`middleware.LoggingMiddleware`). Keep it so.

## Tests

The shared rules are in `modules/CLAUDE.md`. Here:

- **Fakes**: `k8s.io/client-go/kubernetes/fake` with reactors for the Kubernetes API (`k8shelpers/authorizer_test.go`); `mockLogFetcher` and `mockSourceWatcher` for a stream (`logs/stream_test.go`); `httptest` behind a URL field for the GitHub client (`versioncheck/github_test.go`).
- **`k8shelpers/mock`, `logs/mock` and `versioncheck/mock` are for other packages' tests.** When you add a method to one of those interfaces, add it to the mock.
- **A watcher is awaited, not slept on**: `k8shelpers/kube-config-watcher_test.go` waits on a `sync.WaitGroup` its subscription feeds.
- **References**: `logs/stream_test.go` (table-driven, channels built with `newForwardChannel` and `newBackwardChannel`), `k8shelpers/authorizer_test.go`, `k8shelpers/transport_test.go`.

When you add a package, change who uses one, or change an invariant above, update this file in the same change.
