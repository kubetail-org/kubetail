# Backend Server

Kubetail backend server

## Overview

The kubetail backend server is a Go-based HTTP server that's designed to proxy requests from the kubetail frontend website to the user's Kubernetes API as well as provide some other custom functionality such as authentication. It uses the Gin Web framework to serve HTTP requests and the Kubernetes Go-client under the hood to communicate with the Kubernetes API. Externally, it responds to Kubernetes-related queries via a GraphQL endpoint powered by [gqlgen](https://github.com/99designs/gqlgen) and serves other requests via a simple REST API.

In development, the backend and frontend servers are kept separate but in production the frontend website is packaged as a static site and deployed to the server's `website` directory from where it is served at the apex endpoint.

## Quickstart

By default, the backend development server will use your local kubectl config file to connect to your Kubernetes API. To run the app in development you can use the `go run` command:

```sh
go run ./cmd/server -c hack/server.yaml
```

To check on the health status go to: [http://localhost:4000/healthz](http://localhost:4000/healthz)

To use the GraphQL playground go to: [http://localhost:4000/graphiql](http://localhost:4000/graphiql)

## Configure

### CLI Flags

The server executable supports the following command line configuration options:

| Flag         | Datatype    | Description               | Default   |
| ------------ | ----------- | ------------------------- | --------- |
| -c, --config | string      | Path to config file       | ""        |
| -a, --addr   | string      | Host address to bind to   | ":4000"   |
| --gin-mode   | string      | Gin mode (release, debug) | "release" |

### Config Params

The server can be configured using a configuration file written in YAML, JSON, TOML, HCL or envfile format. It will automatically replace ENV variables written in the format `${NAME}` with their corresponding values. The config file supports the following options:

| Name                                  | Datatype | Description                                          | Default                |
| ------------------------------------- | -------- | ---------------------------------------------------- | ---------------------- |
| addr                                  | string   | Host address to bind to                              | ":4000"                |
| auth-mode                             | string   | Auth mode (token, cluster, local)                    | "token"                |
| gin-mode                              | string   | Gin mode (release, debug)                            | "release"              |
| kube-config                           | string   | Kubectl config file path                             | "${HOME}/.kube/config" |
| csrf.enabled                          | bool     | Enable CSRF protection                               | true                   |
| csrf.field-name                       | string   | CSRF token name in forms                             | "csrf_token"           |
| csrf.secret                           | string   | CSRF hash key                                        | ""                     |
| csrf.cookie.name                      | string   | CSRF cookie name                                     | "csrf"                 |
| csrf.cookie.path                      | string   | CSRF cookie path                                     | "/"                    |
| csrf.cookie.domain                    | string   | CSRF cookie domain                                   | ""                     |
| csrf.cookie.max-age                   | int      | CSRF cookie max age (in seconds)                     | 43200                  |
| csrf.cookie.secure                    | bool     | CSRF cookie secure property                          | false                  |
| csrf.cookie.http-only                 | bool     | CSRF cookie HttpOnly property                        | true                   |
| csrf.cookie.same-site                 | string   | CSRF cookie SameSite property (strict, lax, none)    | "strict"               |
| logging.enabled                       | bool     | Enable logging                                       | true                   |
| logging.level                         | string   | Log level                                            | "info"                 |
| logging.format                        | string   | Log format (json, pretty)                            | "json"                 |
| logging.access-log.enabled            | bool     | Enable access log                                    | true                   |
| logging.access-log.hide-health-checks | bool     | Hide requests to /healthz                            | false                  |
| session.secret                        | string   | Session hash key                                     | ""                     |
| session.cookie.path                   | string   | Session cookie path                                  | "/"                    |
| session.cookie.domain                 | string   | Session cookie domain                                | ""                     |
| session.cookie.max-age                | int      | Session cookie max age (in seconds)                  | 43200                  |
| session.cookie.secure                 | bool     | Session cookie secure property                       | false                  |
| session.cookie.http-only              | bool     | Session cookie HttpOnly property                     | true                   |
| session.cookie.same-site              | string   | Session cookie SameSite property (strict, lax, none) | "strict"               |

## GraphQL

The GraphQL schema can be found here: [GraphQL schema](graph/schema.graphqls). To run the gqlgen GraphQL code generator use the `go generate` command:

```sh
go generate ./...
```

### Test

This project uses the [stretchr/testify](https://github.com/stretchr/testify) library for testing. To run the test suite:

```sh
go test ./...
```
