# graphql — the GraphQL servers' schemas

`<server>/schema.graphqls` is the one schema file of `dashboard` and of `cluster/api`. Both servers have the same shape, and these rules hold for both: gqlgen writes the schema into `graph/generated.go` as a string; `graph/server.go` builds the handler and its transports, `graph/resolver.go` holds the `Resolver` struct and its dependencies, and `gqlgen.yml` binds GraphQL types straight to Go types (the Kubernetes API types in `dashboard`, the protobuf messages in `cluster/api`).

## Checks

After editing a schema, from the repo root:

```sh
make generate   # gqlgen, and the UI's types, generated from both schemas
make vet-web
```

Then the checks of the server's own guide.

## Rules

- **`logRecordsFetch` and `logRecordsFollow` exist in both schemas with the same signature**, since the UI sends one set of documents to whichever server is in use. Change both together.
- **`graph/schema.resolvers.go` is rewritten by gqlgen, which keeps the bodies.** A new field arrives as a stub that panics with `not implemented`. Anything that is not a resolver method goes in another file (`schema.helpers.go`, `resolver.go`), or gqlgen moves it to the end of the file.
- **Neither server sets an error presenter.** Whatever error a resolver returns reaches the client with its message, a client-go error included. For a failure the client should understand, return an `Err*` value or a message written for it.
- **In a subscription resolver, report a failure with `transport.AddSubscriptionError(ctx, …)`, then close the channel**, and stop when `ctx` is done (`logRecordsFollow` in either server).
- **`@validate` adds an error and still passes the value on**; a field marked `@nullIfValidationFailed` then resolves to null.
- **A new long-lived connection type is cancelled by `app.NotifyShutdown` and counted for `app.DrainWithContext`**, as WebSockets are (`cmd/main.go`).
