# Kubetail Backend Server

Go-based HTTP server that handles web requests from the Kubetail frontend

## Overview

The Kubetail backend server is a Go-based HTTP server that's designed to proxy requests from the Kubetail frontend to the user's Kubernetes API and to the Kubetail backend agents as well as provide some other custom functionality such as authentication. It uses the Gin Web framework to serve HTTP requests. Under the hood, it uses the Kubernetes Go-client to communicate with the Kubernetes API and gRPC to communicate with Kubetail backend agents. Externally, it responds to Kubernetes-related queries via a GraphQL endpoint powered by [gqlgen](https://github.com/99designs/gqlgen) and serves other requests via a simple REST API.

In development, the backend and frontend servers are kept separate but in production the frontend website is packaged as a static site and deployed to the server's `website` directory from where it is served at the apex endpoint.

## Configure

### CLI Flags

The Kubetail backend server executable supports the following command line configuration options:

| Flag         | Datatype | Description                      | Default   |
| ------------ | -------- | -------------------------------- | --------- |
| -c, --config | string   | Path to Kubetail config file     | ""        |
| -a, --addr   | string   | Host address to bind to          | ":4000"   |
| --gin-mode   | string   | Gin mode (release, debug)        | "release" |
| -p, --param  | []string | Config params ("key:val" format) | []        |

### Config file

The Kubetail backend server can be configured using a configuration file written in YAML, JSON, TOML, HCL or envfile format. The application will automatically replace ENV variables written in the format `${NAME}` with their corresponding values. The config file supports the following options (also see [hack/config.yaml](hack/config.yaml)):

| Name                                         | Datatype | Description                                          | Default                             |
| -------------------------------------------- | -------- | ---------------------------------------------------- | ----------------------------------- |
| auth-mode                                    | string   | Auth mode (token, cluster, local)                    | "token"                             |
| allowed-namespaces                           | []string | If populated, restricts namespace access             | []                                  |
| server.addr                                  | string   | Host address to bind to                              | ":4000"                             |
| server.agent-dispatch-url                    | string   | Url for sending dispatch requests to agent           | "kubernetes://kubetail-agent:50051" |
| server.base-path                             | string   | URL path prefix                                      | "/"                                 |
| server.gin-mode                              | string   | Gin mode (release, debug)                            | "release"                           |
| server.csrf.enabled                          | bool     | Enable CSRF protection                               | true                                |
| server.csrf.field-name                       | string   | CSRF token name in forms                             | "csrf_token"                        |
| server.csrf.secret                           | string   | CSRF hash key                                        | ""                                  |
| server.csrf.cookie.name                      | string   | CSRF cookie name                                     | "csrf"                              |
| server.csrf.cookie.path                      | string   | CSRF cookie path                                     | "/"                                 |
| server.csrf.cookie.domain                    | string   | CSRF cookie domain                                   | ""                                  |
| server.csrf.cookie.max-age                   | int      | CSRF cookie max age (in seconds)                     | 43200                               |
| server.csrf.cookie.secure                    | bool     | CSRF cookie secure property                          | false                               |
| server.csrf.cookie.http-only                 | bool     | CSRF cookie HttpOnly property                        | true                                |
| server.csrf.cookie.same-site                 | string   | CSRF cookie SameSite property (strict, lax, none)    | "strict"                            |
| server.logging.enabled                       | bool     | Enable logging                                       | true                                |
| server.logging.level                         | string   | Log level                                            | "info"                              |
| server.logging.format                        | string   | Log format (json, pretty)                            | "json"                              |
| server.logging.access-log.enabled            | bool     | Enable access log                                    | true                                |
| server.logging.access-log.hide-health-checks | bool     | Hide requests to /healthz from access log            | false                               |
| server.session.secret                        | string   | Session hash key                                     | ""                                  |
| server.session.cookie.name                   | string   | Session cookie name                                  | "session"                           |
| server.session.cookie.path                   | string   | Session cookie path                                  | "/"                                 |
| server.session.cookie.domain                 | string   | Session cookie domain                                | ""                                  |
| server.session.cookie.max-age                | int      | Session cookie max age (in seconds)                  | 43200                               |
| server.session.cookie.secure                 | bool     | Session cookie secure property                       | false                               |
| server.session.cookie.http-only              | bool     | Session cookie HttpOnly property                     | true                                |
| server.session.cookie.same-site              | string   | Session cookie SameSite property (strict, lax, none) | "strict"                            |
| server.tls.enabled                           | bool     | Enable TLS endpoint termination                      | false                               |
| server.tls.cert-file                         | string   | Path to cert file                                    | ""                                  |
| server.tls.key-file                          | string   | Path to key file                                     | ""                                  |

## GraphQL

The GraphQL schema can be found here: [GraphQL schema](graph/schema.graphqls). To run the gqlgen GraphQL code generator use the `go generate` command:

```console
go generate ./...
```

## Test

This project uses the [stretchr/testify](https://github.com/stretchr/testify) library for testing. To run the test suite execute this command:

```console
go test ./...
```
