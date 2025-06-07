# Kubetail Dashboard UI

React-based static website for the Kubetail Dashboard

## Overview

The Kubetail Dashboard UI is a React-based static website that's designed to query the Kubetail Dashboard server and display results to the user in a clean, easy-to-use interface. Kubernetes-related requests to the server's GraphQL endpoint are made using [Apollo Client](https://www.apollographql.com/docs/react/) and authentication-related requests to the REST API are made using simple `fetch()` requests. The code is written in TypeScript and is generally written to be as type-safe as possible. In development, the application uses [vite](https://vitejs.dev/) and in production, it's deployed as a static website hosted by the server.

## GraphQL

This project uses [graphql-codegen](https://the-guild.dev/graphql/codegen) to generate TypeScript definitions for its internal GraphQL queries. To run the code generator:

```sh
pnpm graphql-codegen
```

## Test

This project uses [vitest](https://vitest.dev/) for testing. To run the test suite:

```sh
pnpm test
```

Using the Makefile from the project root:

```console
# Run dashboard UI tests
make dashboard-ui-test
```

## Lint

This project uses TypeScript and enforces the [Airbnb JavaScript styleguide](https://github.com/airbnb/javascript) using eslint. To run the linter:

```sh
pnpm lint
```

Using the Makefile from the project root:

```console
# Run dashboard UI linting
make dashboard-ui-lint
```

## Combined Operations

To run all dashboard UI tests and linting in one command, use:

```console
make dashboard-ui-all
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
