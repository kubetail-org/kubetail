# Kubetail Dashboard Server

Go-based HTTP server that serves up the Kubetail dashboard frontend and handles the dashboard's backend requests

## Overview

The Kubetail dashboard server is a Go-based HTTP server that hosts the Kubetail dashboard web app frontend and associated backend services. The backend proxies requests from the frontend to the user's Kubernetes API and, when required, to the Kubetail API running inside the cluster. It also provides some other custom functionality such as authentication. It uses the Gin Web framework to serve HTTP requests. Under the hood, it uses the Kubernetes Go-client to communicate with the Kubernetes API. Externally, it responds to Kubernetes-related queries via a GraphQL endpoint powered by [gqlgen](https://github.com/99designs/gqlgen) and serves other requests via a simple REST API.

In development, the backend and frontend are kept separate but in production the frontend website is packaged as a static site and deployed to the server's `website` directory from where it is served at the apex endpoint.

## Configure

### CLI Flags

The Kubetail backend server executable supports the following command line configuration options:

| Flag         | Datatype | Description                      | Default   |
| ------------ | -------- | -------------------------------- | --------- |
| -c, --config | string   | Path to Kubetail config file     | ""        |
| -a, --addr   | string   | Host address to bind to          | ":8080"   |
| --gin-mode   | string   | Gin mode (release, debug)        | "release" |
| -p, --param  | []string | Config params ("key:val" format) | []        |

### Config file

The Dashboard reads a config file in YAML, JSON, TOML, HCL or envfile format, chosen by its extension. It replaces `${NAME}` with the value of the environment variable `NAME`. Keys are top-level. [config/default/dashboard.yaml](../../config/default/dashboard.yaml) documents every key and its default.

`-p key:value` overrides a key. Precedence is `-p`, then the flags, then the file, then the defaults.

## GraphQL

The GraphQL schema can be found here: [GraphQL schema](graph/schema.graphqls). To run the gqlgen GraphQL code generator use the `go generate` command:

```console
go generate ./...
```

## Test

To run the test suite directly:

```console
go test ./...
```

Using the Makefile from the project root:

```console
# Run tests for all modules including dashboard
make test-go

# Run linter
make lint-go

# Run code vetting
make vet-go
```
