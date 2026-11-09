# modules — Go workspace

Four Go modules under one `go.work`. This file holds what every module follows; each module's guide adds its own. The root [`CLAUDE.md`](../CLAUDE.md) holds what crosses areas: the request path, the gRPC and GraphQL contracts, the writing standards and the testing conventions.

| Module | Guide | Holds |
| --- | --- | --- |
| `cli/` | [`cli/CLAUDE.md`](./cli/CLAUDE.md) | The `kubetail` command. |
| `dashboard/` | [`dashboard/CLAUDE.md`](./dashboard/CLAUDE.md) | The Dashboard backend. |
| `cluster-api/` | [`cluster-api/CLAUDE.md`](./cluster-api/CLAUDE.md) | The Cluster API. |
| `shared/` | [`shared/CLAUDE.md`](./shared/CLAUDE.md) | Packages the other three import. |

**Imports point one way.** `shared` imports no sibling. `dashboard` and `cluster-api` import `shared` alone. `cli` imports `shared` and exactly two Dashboard packages, `dashboard/pkg/app` and `dashboard/pkg/config`, to run the Dashboard in `kubetail serve`. Nothing imports `cli` or `cluster-api`.

**Each module builds without `go.work`.** The release Dockerfiles copy only the modules a component needs, and the CLI release builds with `GOWORK=off`, so every `go.mod` carries a `replace` for each sibling it imports and a complete `go.sum`. A new import of a sibling module needs the `replace`, and a `COPY` in the component's Dockerfile under `build/package/`. Every module also pins `fsnotify` to v1.8.0 with a `replace`; keep the four in step.

**Go is 1.26**; CI and the Dockerfiles pin the patch version (`go-version` in `.github/actions/setup-environment/action.yml`).

## Checks

```sh
cd modules/<module>
go fmt ./...            # CI fails if `gofmt -l .` prints anything
go vet ./...
go test ./...
go test -race ./...     # CI runs both (no race run on Windows arm64)
```

From `modules/`, `go test -race github.com/kubetail-org/kubetail/modules/...` covers the workspace; `make modules-lint`, `make modules-vet` and `make modules-test` do the same from the repo root. A change to `shared` runs the workspace's tests, since every module imports it.

CI runs each module's tests on Linux, macOS and Windows, amd64 and arm64, from a vendored tree (`go work vendor`). Don't commit `modules/vendor`.

## Code generation

`go generate ./...` in a module runs its directives; from `modules/`, `go generate github.com/kubetail-org/kubetail/modules/...` runs them all and so needs `protoc`.

| Module | Directive | Writes |
| --- | --- | --- |
| `dashboard`, `cluster-api` | `gqlgen generate` (`graph/resolver.go`) | `graph/generated.go`, `graph/model/models_gen.go`, and new stubs in `graph/schema.resolvers.go` |
| `shared` | `protoc` (`clusteragentpb/clusteragentpb.go`, `testpb/testpb.go`) | the `*.pb.go` beside them |
| `cli` | `cp` of `config/default/cli.*` (`assets/embed.go`) | `assets/config.{yaml,toml,json}` |

Generated files are committed and never hand-edited. What follows a schema or proto change is in the root guide.

## Conventions

- **`context.Context` is the first parameter**, and it carries the caller's identity (root guide, *Security invariants*). Pass the request's context down; never start a Kubernetes or gRPC call from `context.Background()` on a request path.
- **Each environment is an implementation behind one interface**, picked by a factory that switches on `config.Environment`: `ConnectionManager`, `Proxy`, `HealthMonitor`. Behaviour that differs by environment goes in both implementations. A resolver checks the environment itself only to refuse an operation that exists in one alone.
- **Errors**: wrap with `fmt.Errorf("doing x: %w", err)` when the caller could not otherwise tell where it failed; sentinels are `errors.New`, tested with `errors.Is`. A resolver that refuses a request returns one of the `Err*` values in `shared/graphql/errors` (`ErrForbidden`, `ErrUnauthenticated`, `ErrValidationError`, `ErrServiceUnavailable`…), whose `extensions.code` (`KUBETAIL_FORBIDDEN`…) is what a client matches on.
- **Logging is zerolog's global logger**, imported as `zlog "github.com/rs/zerolog/log"`: `zlog.Error().Err(err).Msg("…")`, `zlog.Fatal().Caller().Err(err).Send()` at startup only. `shared/logging.ConfigureLogger` installs it at startup; `kubetail serve` configures it once more from `--log-level`. Never log a token, a cookie or a request body.
- **A constructor is `NewX(required…, opts ...Option)`** where there is anything optional (`k8shelpers.ConnectionManagerOption`, `logs.Option`, `helm.ClientOption`). Exported options are production knobs; a seam only tests need is an unexported option, field or constructor (`newClientForTest`).
- **An interface sits beside its implementation** (`k8shelpers.ConnectionManager`, `logs.Stream`, `logs.LogFetcher`). A small unexported interface on the consumer's side is fine as a test seam (`queryHelpers`, `helmReleaseLister`).
- **Mocks are hand-written testify mocks in a `mock/` subpackage** of the package they fake: `k8shelpers/mock`, `logs/mock`, `versioncheck/mock`. No mock generator.
- **A method that needs its caller to hold the lock ends in `_UNSAFE`** (`getOrCreateRestConfig_UNSAFE`); the method that takes the lock and calls it has the plain name. `setError_SAFE` is the one wrapper spelled `_SAFE`.
- **Fan-out to subscribers is `github.com/kubetail-org/megaphone`.** Don't hand-roll a subscriber map.
- **Use the current standard library**: `any`, `wg.Go(...)`, range over an integer, `slices` and `maps`. `go vet` passes; nothing rewrites these for you.
- **Imports are grouped, a blank line between groups**: standard library, third-party, `modules/shared/...`, then the module's own packages.
- **File names follow their directory**: kebab-case in `shared` and the older Dashboard packages (`connection-manager.go`), snake_case in `cli`. In a directory that mixes them, a new file is snake_case.

## The GraphQL servers

`dashboard` and `cluster-api` have the same shape: `graph/schema.graphqls` is the one schema file, `graph/server.go` builds the handler and its transports, `graph/resolver.go` holds the `Resolver` struct and its dependencies, and `gqlgen.yml` binds GraphQL types straight to Go types (the Kubernetes API types in `dashboard`, the protobuf messages in `cluster-api`).

- **`graph/schema.resolvers.go` is rewritten by gqlgen, which keeps the bodies.** A new field arrives as a stub that panics with `not implemented`, to fill in. Anything that is not a resolver method goes in another file (`schema.helpers.go`, `resolver.go`), or gqlgen moves it to the end of the file.
- **Neither server sets an error presenter.** Whatever error a resolver returns reaches the client with its message, a client-go error included. For a failure the client should understand, return an `Err*` value or a message written for it.
- **In a subscription resolver, report a failure with `transport.AddSubscriptionError(ctx, …)`, then close the channel**, and stop when `ctx` is done (`logRecordsFollow` in either server).
- **`@validate` adds an error and still passes the value on**; a field marked `@nullIfValidationFailed` then resolves to null (`shared/graphql/directives`).
- **Both servers shut down the same way** (`cmd/main.go`): `server.Shutdown` under a 30 second budget, whose hook `app.NotifyShutdown` cancels WebSocket and other long-lived connections; `app.DrainWithContext` waits for in-flight requests; `app.Close` releases the rest. A new long-lived connection type is cancelled by `NotifyShutdown` and counted for the drain.

## Tests

The repo-wide rules are the root guide's *Testing conventions*.

- **testify `assert` and `require`**, plain `func TestX(t *testing.T)`, table-driven with `t.Run` where cases repeat. No testify `suite` in new tests.
- **White-box by default** (`package foo`). An external `package foo_test` only to pin a public contract, and it says so.
- **No `time.Sleep`.** Wait on a channel with a `select` against `time.After` or a context deadline; the helpers in `cli/internal/clusterapi/stream_test.go` (`drainAll`) and `dashboard/internal/cluster-api/health-monitor_test.go` show the shape.
- **`shared/testutils.NewWebTestClient(t, handler)`** is the HTTP client for app and GraphQL tests: an `httptest.Server` with a cookie jar that sends `Sec-Fetch-Site: same-origin` and replays the last `X-CSRF-Token` on unsafe methods. Prime it with `client.Get("/api/auth/session")`, and `defer client.Teardown()`.
- **Kubernetes is faked with `k8s.io/client-go/kubernetes/fake`** and `k8shelpers/mock.MockConnectionManager`; a server with `httptest`; a gRPC peer with a real `grpc.NewServer()` on `127.0.0.1:0`. Files go in `t.TempDir()`; environment variables through `t.Setenv`.
- **`NewApp` in `dashboard` and `cluster-api` skips its real dependencies when `gin.Mode()` is `gin.TestMode`**, which each package's `testutils_test.go` sets in `init()`. That global is the existing seam; don't add a second one like it, and remember a test under it does not exercise what it skips.
- **One package variable is swapped by tests**, `inClusterConfigFn` in `k8shelpers`. New seams are parameters or fields.

When you change a convention every module follows, update this file in the same change; a module's own belong in its guide.
