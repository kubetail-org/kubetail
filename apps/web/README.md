# Kubetail Dashboard UI

React-based static website for the Kubetail Dashboard

## Overview

The Kubetail Dashboard UI is a React-based static website that's designed to query the Kubetail Dashboard server and display results to the user in a clean, easy-to-use interface. Kubernetes-related requests to the server's GraphQL endpoint are made using [Apollo Client](https://www.apollographql.com/docs/react/) and authentication-related requests to the REST API are made using simple `fetch()` requests. The code is written in TypeScript and is generally written to be as type-safe as possible. In development, the application uses [vite](https://vitejs.dev/) and in production, it's deployed as a static website hosted by the server.

## GraphQL

This project uses [graphql-codegen](https://the-guild.dev/graphql/codegen) to generate TypeScript definitions for its internal GraphQL queries. To run the code generator:

```sh
pnpm graphql-codegen
```

## Checks

In `apps/web/`.

While working:

```sh
pnpm format                                # oxfmt: formats and sorts imports
pnpm lint                                  # oxlint (type-aware), then `oxfmt --check`. A warning fails the run.
pnpm exec vitest related --run <file>...   # the tests that import the files you touched
```

Before a PR:

```sh
pnpm build                 # tsc -b && vite build. `tsc -b` (`make vet-ts`) is the only type-check, and it covers test files
pnpm test run              # `pnpm test` alone is watch mode and never exits
make lint-ts               # from the repo root: `pnpm lint`, license headers, coverage markers
make cover-web-changed     # from the repo root: the changed lines no test covers
```

## Build

To package the application into a standalone static website run the build step:

```sh
pnpm build
```

This will place the static assets in the `dist` directory.

Using the Makefile from the project root:

```console
# Build the dashboard UI
make build-dashboard-ui
```
