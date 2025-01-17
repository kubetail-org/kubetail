# Kubetail Cluster API

Go-based HTTP server that handles Kubetail Cluster API requests

## Configure

### CLI Flags

The Kubetail Cluster API executable supports the following command line configuration options:

| Flag         | Datatype | Description                      | Default   |
| ------------ | -------- | -------------------------------- | --------- |
| -c, --config | string   | Path to Kubetail config file     | ""        |
| -a, --addr   | string   | Host address to bind to          | ":80"     |
| --gin-mode   | string   | Gin mode (release, debug)        | "release" |
| -p, --param  | []string | Config params ("key:val" format) | []        |

### Config file

The Kubetail Cluster API executable can be configured using a configuration file written in YAML, JSON, TOML, HCL or envfile format. The application will automatically replace ENV variables written in the format `${NAME}` with their corresponding values. The config file supports the following options (also see [hack/config.yaml](../../hack/config.yaml)):

| Name                                              | Datatype | Description                                          | Default                                     |
| ------------------------------------------------- | -------- | ---------------------------------------------------- | ------------------------------------------- |
| allowed-namespaces                                | []string | If populated, restricts namespace access             | []                                          |
| cluster-api.addr                                  | string   | Host address to bind to                              | ":80"                                       |
| cluster-api.base-path                             | string   | URL path prefix                                      | "/"                                         |
| cluster-api.cluster-agent-dispatch-url            | string   | URL for sending dispatch requests to cluster-agent   | "kubernetes://kubetail-cluster-agent:50051" |
| cluster-api.gin-mode                              | string   | Gin mode (release, debug)                            | "release"                                   |
| cluster-api.csrf.enabled                          | bool     | Enable CSRF protection                               | true                                        |
| cluster-api.csrf.field-name                       | string   | CSRF token name in forms                             | "csrf_token"                                |
| cluster-api.csrf.secret                           | string   | CSRF hash key                                        | ""                                          |
| cluster-api.csrf.cookie.name                      | string   | CSRF cookie name                                     | "csrf"                                      |
| cluster-api.csrf.cookie.path                      | string   | CSRF cookie path                                     | "/"                                         |
| cluster-api.csrf.cookie.domain                    | string   | CSRF cookie domain                                   | ""                                          |
| cluster-api.csrf.cookie.max-age                   | int      | CSRF cookie max age (in seconds)                     | 43200                                       |
| cluster-api.csrf.cookie.secure                    | bool     | CSRF cookie secure property                          | false                                       |
| cluster-api.csrf.cookie.http-only                 | bool     | CSRF cookie HttpOnly property                        | true                                        |
| cluster-api.csrf.cookie.same-site                 | string   | CSRF cookie SameSite property (strict, lax, none)    | "strict"                                    |
| cluster-api.logging.enabled                       | bool     | Enable logging                                       | true                                        |
| cluster-api.logging.level                         | string   | Log level                                            | "info"                                      |
| cluster-api.logging.format                        | string   | Log format (json, pretty)                            | "json"                                      |
| cluster-api.logging.access-log.enabled            | bool     | Enable access log                                    | true                                        |
| cluster-api.logging.access-log.hide-health-checks | bool     | Hide requests to /healthz from access log            | false                                       |
| cluster-api.tls.enabled                           | bool     | Enable TLS endpoint termination                      | false                                       |
| cluster-api.tls.cert-file                         | string   | Path to cert file                                    | ""                                          |
| cluster-api.tls.key-file                          | string   | Path to key file                                     | ""                                          |

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
