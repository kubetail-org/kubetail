# Kubetail Cluster API

Go-based HTTP server that handles Kubetail Cluster API requests

## Configure

### CLI Flags

The Kubetail Cluster API executable supports the following command line configuration options:

| Flag         | Datatype   | Description                      | Default                   |
| ------------ | ---------- | -------------------------------- | ------------------------- | --------- | --- |
| -c, --config | string     | Path to Kubetail config file     | ""                        |
| <!--         | --gin-mode | string                           | Gin mode (release, debug) | "release" | --> |
| -p, --param  | []string   | Config params ("key:val" format) | []                        |

### Config file

The Kubetail Cluster API executable can be configured using a configuration file written in YAML, JSON, TOML, HCL or envfile format. The application will automatically replace ENV variables written in the format `${NAME}` with their corresponding values. The config file supports the following options (also see [hack/config.yaml](../../hack/config.yaml)):

| Name                                              | Datatype | Description                                        | Default                                     | Status |
| ------------------------------------------------- | -------- | -------------------------------------------------- | ------------------------------------------- | ------ |
| allowed-namespaces                                | []string | If populated, restricts namespace access           | []                                          | stable |
| cluster-api.base-path                             | string   | URL path prefix                                    | "/"                                         | stable |
| cluster-api.cluster-agent-dispatch-url            | string   | URL for sending dispatch requests to cluster-agent | "kubernetes://kubetail-cluster-agent:50051" | stable |
| cluster-api.gin-mode                              | string   | Gin mode (release, debug)                          | "release"                                   | stable |
| cluster-api.csrf.enabled                          | bool     | Enable CSRF protection                             | true                                        | stable |
| cluster-api.csrf.field-name                       | string   | CSRF token name in forms                           | "csrf_token"                                | stable |
| cluster-api.csrf.secret                           | string   | CSRF hash key                                      | ""                                          | stable |
| cluster-api.csrf.cookie.name                      | string   | CSRF cookie name                                   | "csrf"                                      | stable |
| cluster-api.csrf.cookie.path                      | string   | CSRF cookie path                                   | "/"                                         | stable |
| cluster-api.csrf.cookie.domain                    | string   | CSRF cookie domain                                 | ""                                          | stable |
| cluster-api.csrf.cookie.max-age                   | int      | CSRF cookie max age (in seconds)                   | 43200                                       | stable |
| cluster-api.csrf.cookie.secure                    | bool     | CSRF cookie secure property                        | false                                       | stable |
| cluster-api.csrf.cookie.http-only                 | bool     | CSRF cookie HttpOnly property                      | true                                        | stable |
| cluster-api.csrf.cookie.same-site                 | string   | CSRF cookie SameSite property (strict, lax, none)  | "strict"                                    | stable |
| cluster-api.http.enabled                          | bool     | Enables http server                                | true                                        | stable |
| cluster-api.http.address                          | string   | URL of the http server                             | ""                                          | stable |
| cluster-api.http.port                             | int      | Port of the http server                            | 8080                                        | stable |
| cluster-api.https.enabled                         | bool     | Enables https server                               | false                                       | stable |
| cluster-api.https.address                         | string   | URL of the https server                            | ""                                          | stable |
| cluster-api.https.port                            | int      | Port of the https server                           | 8443                                        | stable |
| cluster-api.https.tls.cert-file                   | string   | Path to tls certificate file                       | ""                                          | stable |
| cluster-api.https.tls.key-file                    | string   | Path to tls key file                               | ""                                          | stable |
| cluster-api.logging.enabled                       | bool     | Enable logging                                     | true                                        | stable |
| cluster-api.logging.level                         | string   | Log level                                          | "info"                                      | stable |
| cluster-api.logging.format                        | string   | Log format (json, pretty)                          | "json"                                      | stable |
| cluster-api.logging.access-log.enabled            | bool     | Enable access log                                  | true                                        | stable |
| cluster-api.logging.access-log.hide-health-checks | bool     | Hide requests to /healthz from access log          | false                                       | stable |

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
