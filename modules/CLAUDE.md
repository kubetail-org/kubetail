# modules — Go workspace

Four Go modules under one `go.work`: `cli`, `dashboard`, `cluster-api` and `shared`. This file holds what every module follows; each module's guide adds its own.

- **Imports point one way.** `shared` imports no sibling. `dashboard` and `cluster-api` import `shared` alone. `cli` imports `shared` and exactly two Dashboard packages, `dashboard/pkg/app` and `dashboard/pkg/config`. Nothing imports `cli` or `cluster-api`.
- **Each module builds without `go.work`**, as the release builds do. Every `go.mod` carries a `replace` for each sibling it imports and a complete `go.sum`. A new import of a sibling module needs the `replace`, and a `COPY` in the component's Dockerfile under `build/package/`. → [ADR: each Go module builds without go.work](../docs/adr/2026-11-09-each-go-module-builds-without-go-work.md)
- **Every module pins `fsnotify` to v1.8.0 with a `replace`**; keep the four in step. → [ADR: pin fsnotify to v1.8.0](../docs/adr/2026-11-09-pin-fsnotify-to-v1-8-0.md)
- **Go is 1.26**; CI and the Dockerfiles pin the patch version (`go-version` in `.github/actions/setup-environment/action.yml`).

## Checks

```sh
cd modules/<module>
go fmt ./...            # CI fails if `gofmt -l .` prints anything
go vet ./...
go test -race ./...
```

From the repo root, `make vet-go` adds a `GOOS=windows` pass, `scripts/check-go-fix.sh` and staticcheck. CI runs all three. `make vet-go-changed` runs `go vet` and `go fix` on the changed packages alone, with their importers, on this platform; `ALL_PLATFORMS=1` adds the other two. Ignore a staticcheck finding with `//lint:ignore <check> <reason>` on the line above it.

From `modules/`, `go test -race github.com/kubetail-org/kubetail/modules/...` covers the workspace, as `make lint-go`, `vet-go` and `test-go` do from the repo root. A change to `shared` runs the workspace's tests. CI tests from a vendored tree (`go work vendor`); don't commit `modules/vendor`.

Coverage is measured across the workspace: every test binary counts every package (`-coverpkg`), so a `shared` line that the Dashboard's tests reach is covered. `make cover-changed` runs only the changed packages' tests; `FULL=1` runs them all, as CI does. → [ADR: measure Go coverage across the workspace](../docs/adr/2026-11-09-measure-go-coverage-across-the-workspace.md)

`go generate ./...` in a module runs its generators: gqlgen in `dashboard` and `cluster-api`, `protoc` in `shared`, a copy of `config/default/cli.*` in `cli`.

## Conventions

- **`context.Context` is the first parameter**, and it carries the caller's identity. Pass the request's context down; never start a Kubernetes or gRPC call from `context.Background()` on a request path.
- **Each environment is an implementation behind one interface**, picked by a factory that switches on `config.Environment`: `ConnectionManager`, `Proxy`, `HealthMonitor`. Behaviour that differs by environment goes in both implementations. A resolver checks the environment itself only to refuse an operation that exists in one alone.
- **Errors**: wrap with `fmt.Errorf("doing x: %w", err)` when the caller could not otherwise tell where it failed; sentinels are `errors.New`, tested with `errors.Is`. A resolver that refuses a request returns one of the `Err*` values in `shared/graphql/errors` (`ErrForbidden`, `ErrUnauthenticated`, `ErrValidationError`, `ErrServiceUnavailable`…), whose `extensions.code` is what a client matches on.
- **Logging is zerolog's global logger**, imported as `zlog "github.com/rs/zerolog/log"`. `zlog.Fatal()` is for startup only. Never log a token, a cookie or a request body.
- **A constructor is `NewX(required…, opts ...Option)`** where there is anything optional. Exported options are production knobs; a seam only tests need is an unexported option, field or constructor (`newClientForTest`).
- **An interface sits beside its implementation** (`k8shelpers.ConnectionManager`, `logs.Stream`). A small unexported interface on the consumer's side is fine as a test seam.
- **Mocks are hand-written testify mocks in a `mock/` subpackage** of the package they fake (`k8shelpers/mock`). No mock generator. A method added to the interface is added to the mock.
- **A method that needs its caller to hold the lock ends in `_UNSAFE`**; the method that takes the lock and calls it has the plain name.
- **Fan-out to subscribers is `github.com/kubetail-org/megaphone`.** Don't hand-roll a subscriber map.
- **Use the current standard library**: `any`, `new(expr)`, `wg.Go(...)`, range over an integer, `slices` and `maps`. CI fails when `go fix` would rewrite a file.
- **Imports are grouped, a blank line between groups**: standard library, third-party, `modules/shared/...`, then the module's own packages.
- **File names follow their directory**: kebab-case in `shared` and the older Dashboard packages, snake_case in `cli`. In a directory that mixes them, a new file is snake_case.

## The GraphQL servers

`dashboard` and `cluster-api` have the same shape: `graph/schema.graphqls` is the one schema file, `graph/server.go` builds the handler and its transports, `graph/resolver.go` holds the `Resolver` struct and its dependencies, and `gqlgen.yml` binds GraphQL types straight to Go types (the Kubernetes API types in `dashboard`, the protobuf messages in `cluster-api`).

After editing a schema:

```sh
cd modules/<module> && go generate ./...       # gqlgen
cd dashboard-ui && pnpm graphql-codegen        # the UI's types, generated from both schemas
```

- **`logRecordsFetch` and `logRecordsFollow` exist in both schemas with the same signature**, since the UI sends one set of documents to whichever server is in use. Change both together.
- **`graph/schema.resolvers.go` is rewritten by gqlgen, which keeps the bodies.** A new field arrives as a stub that panics with `not implemented`. Anything that is not a resolver method goes in another file (`schema.helpers.go`, `resolver.go`), or gqlgen moves it to the end of the file.
- **Neither server sets an error presenter.** Whatever error a resolver returns reaches the client with its message, a client-go error included. For a failure the client should understand, return an `Err*` value or a message written for it.
- **In a subscription resolver, report a failure with `transport.AddSubscriptionError(ctx, …)`, then close the channel**, and stop when `ctx` is done (`logRecordsFollow` in either server).
- **`@validate` adds an error and still passes the value on**; a field marked `@nullIfValidationFailed` then resolves to null.
- **A new long-lived connection type is cancelled by `app.NotifyShutdown` and counted for `app.DrainWithContext`**, as WebSockets are (`cmd/main.go`).

## Tests

The repo-wide rules are the root guide's *Testing conventions*.

- **testify `assert` and `require`**, plain `func TestX(t *testing.T)`, table-driven with `t.Run` where cases repeat. No testify `suite` in new tests.
- **White-box by default** (`package foo`). An external `package foo_test` only to pin a public contract, and it says so.
- **To wait, `select` on a channel against `time.After` or a context deadline**, or use `require.Eventually`. `drainAll` in `cli/internal/clusterapi/stream_test.go` shows the shape.
- **A test passes in any order, at any `-cpu`, run repeatedly.** The nightly `stress.yml` runs every test with `-shuffle=on`, `-count` and several `-cpu` values, with and without `-race`, and opens an issue per failing test. Its report script is `scripts/stress.go`.
- **Tests run on Linux, macOS and Windows in CI.** Use `t.TempDir()`, `t.Setenv` and `filepath`. A test closes what it opens before it ends, since Windows removes no open file. A platform-specific test goes in a platform-named file (`foo_unix_test.go`, `//go:build !windows`), never behind a `runtime.GOOS` check.
- **`shared/testutils.NewWebTestClient(t, handler)`** is the HTTP client for app and GraphQL tests: it keeps cookies, sends `Sec-Fetch-Site: same-origin` and replays the last `X-CSRF-Token` on unsafe methods. Prime it with `client.Get("/api/auth/session")`, and `defer client.Teardown()`.
- **Kubernetes is faked with `k8s.io/client-go/kubernetes/fake`** and `k8shelpers/mock.MockConnectionManager`; a server with `httptest`; a gRPC peer with a real `grpc.NewServer()` on `127.0.0.1:0`.
- **`NewApp` in `dashboard` and `cluster-api` builds the real dependencies and passes them to the unexported `newApp`**, which tests call with fakes. Every route and middleware is mounted either way.
- **One package variable is swapped by tests**, `inClusterConfigFn` in `k8shelpers`. New seams are parameters or fields.
