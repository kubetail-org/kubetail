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
| -a, --addr   | string   | Host address to bind to          | ":80"     |
| --gin-mode   | string   | Gin mode (release, debug)        | "release" |
| -p, --param  | []string | Config params ("key:val" format) | []        |

### Config file

The Kubetail Dashboard server can be configured using a configuration file written in YAML, JSON, TOML, HCL or envfile format. The application will automatically replace ENV variables written in the format `${NAME}` with their corresponding values. The config file supports the following options (also see [hack/config.yaml](../../hack/config.yaml)):

| Name                                            | Datatype | Description                                          | Default                       | Status       |
| ----------------------------------------------- | -------- | ---------------------------------------------------- | ----------------------------- | ------------ |
| allowed-namespaces                              | []string | If populated, restricts namespace access             | []                            |              |
| dashboard.addr                                  | string   | Host address to bind to                              | ":80"                         |              |
| dashboard.auth-mode                             | string   | Auth mode (auto, token)                              | "auto"                        | experimental |
| dashboard.base-path                             | string   | URL path prefix                                      | "/"                           |              |
| dashboard.cluster-api-endpoint                  | string   | Service url for Cluster API                          | "http://kubetail-cluster-api" | experimental |
| dashboard.environment                           | string   | Environment (desktop, cluster)                       | "desktop"                     | experimental |
| dashboard.gin-mode                              | string   | Gin mode (release, debug)                            | "release"                     |              |
| dashboard.csrf.enabled                          | bool     | Enable CSRF protection                               | true                          |              |
| dashboard.csrf.field-name                       | string   | CSRF token name in forms                             | "csrf_token"                  |              |
| dashboard.csrf.secret                           | string   | CSRF hash key                                        | ""                            |              |
| dashboard.csrf.cookie.name                      | string   | CSRF cookie name                                     | "csrf"                        |              |
| dashboard.csrf.cookie.path                      | string   | CSRF cookie path                                     | "/"                           |              |
| dashboard.csrf.cookie.domain                    | string   | CSRF cookie domain                                   | ""                            |              |
| dashboard.csrf.cookie.max-age                   | int      | CSRF cookie max age (in seconds)                     | 43200                         |              |
| dashboard.csrf.cookie.secure                    | bool     | CSRF cookie secure property                          | false                         |              |
| dashboard.csrf.cookie.http-only                 | bool     | CSRF cookie HttpOnly property                        | true                          |              |
| dashboard.csrf.cookie.same-site                 | string   | CSRF cookie SameSite property (strict, lax, none)    | "strict"                      |              |
| dashboard.logging.enabled                       | bool     | Enable logging                                       | true                          |              |
| dashboard.logging.level                         | string   | Log level                                            | "info"                        |              |
| dashboard.logging.format                        | string   | Log format (json, pretty)                            | "json"                        |              |
| dashboard.logging.access-log.enabled            | bool     | Enable access log                                    | true                          |              |
| dashboard.logging.access-log.hide-health-checks | bool     | Hide requests to /healthz from access log            | false                         |              |
| dashboard.session.secret                        | string   | Session hash key                                     | ""                            |              |
| dashboard.session.cookie.name                   | string   | Session cookie name                                  | "session"                     |              |
| dashboard.session.cookie.path                   | string   | Session cookie path                                  | "/"                           |              |
| dashboard.session.cookie.domain                 | string   | Session cookie domain                                | ""                            |              |
| dashboard.session.cookie.max-age                | int      | Session cookie max age (in seconds)                  | 43200                         |              |
| dashboard.session.cookie.secure                 | bool     | Session cookie secure property                       | false                         |              |
| dashboard.session.cookie.http-only              | bool     | Session cookie HttpOnly property                     | true                          |              |
| dashboard.session.cookie.same-site              | string   | Session cookie SameSite property (strict, lax, none) | "strict"                      |              |
| dashboard.tls.enabled                           | bool     | Enable TLS endpoint termination                      | false                         |              |
| dashboard.tls.cert-file                         | string   | Path to cert file                                    | ""                            |              |
| dashboard.tls.key-file                          | string   | Path to key file                                     | ""                            |              |
| dashboard.ui.cluster-api-enabled                | bool     | Enable Cluster API features                          | true                          | experimental |

## GraphQL

The GraphQL schema can be found here: [GraphQL schema](graph/schema.graphqls). To run the gqlgen GraphQL code generator use the `go generate` command:

```console
go generate ./...
```

## Test

To run the test suite execute this command:

```console
go test ./...
```
