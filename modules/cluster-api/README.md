# Kubetail Cluster API

Go-based HTTP server that handles Kubetail Cluster API requests

## Configure

### CLI Flags

The Kubetail Cluster API executable supports the following command line configuration options:

| Flag         | Datatype   | Description                      | Default   |
| ------------ | ---------- | -------------------------------- | --------- |
| -c, --config | string     | Path to Kubetail config file     | ""        |
| -a, --addr   | string     | Host address to bind to          | ":8080"   |
| --gin-mode   | string     | Gin mode (release, debug)        | "release" |
| -p, --param  | []string   | Config params ("key:val" format) | []        |

### Config file

The Kubetail Cluster API executable can be configured using a configuration file written in YAML, JSON, TOML, HCL or envfile format. The application will automatically replace ENV variables written in the format `${NAME}` with their corresponding values. The config file supports the following options (also see [config/default/cluster-api.yaml](../../config/default/cluster-api.yaml)):

| Name                                              | Datatype | Description                                        | Default                                     | Status |
| ------------------------------------------------- | -------- | -------------------------------------------------- | ------------------------------------------- | ------ |
| allowed-namespaces                                | []string | If populated, restricts namespace access           | []                                          | stable |
| cluster-api.addr                                  | string   | Host address to bind to                            | ":8080"                                     | stable |
| cluster-api.base-path                             | string   | URL path prefix                                    | "/"                                         | stable |
| cluster-api.gin-mode                              | string   | Gin mode (release, debug)                          | "release"                                   | stable |
| cluster-api.cluster-agent.dispatch-url            | string   | URL for sending dispatch requests to cluster-agent | "kubernetes://kubetail-cluster-agent:50051" | alpha  |
| cluster-api.cluster-agent.tls.enabled             | bool     | Enable tls                                         | false                                       | alpha  |
| cluster-api.cluster-agent.tls.cert-file           | string   | Path to tls certificate file                       | ""                                          | alpha  |
| cluster-api.cluster-agent.tls.key-file            | string   | Path to tls key file                               | ""                                          | alpha  |  
| cluster-api.cluster-agent.tls.ca-file             | string   | Path to tls CA bundle file                         | ""                                          | alpha  |
| cluster-api.cluster-agent.tls.server-name         | string   | Server name for conection verification             | ""                                          | alpha  |
| cluster-api.csrf.enabled                          | bool     | Enable CSRF protection                             | true                                        | stable |
| cluster-api.logging.enabled                       | bool     | Enable logging                                     | true                                        | stable |
| cluster-api.logging.level                         | string   | Log level                                          | "info"                                      | stable |
| cluster-api.logging.format                        | string   | Log format (json, pretty)                          | "json"                                      | stable |
| cluster-api.logging.access-log.enabled            | bool     | Enable access log                                  | true                                        | stable |
| cluster-api.logging.access-log.hide-health-checks | bool     | Hide requests to /healthz from access log          | false                                       | stable |
| cluster-api.tls.enabled                           | bool     | Enable tls                                         | false                                       | stable |
| cluster-api.tls.cert-file                         | string   | Path to tls certificate file                       | ""                                          | stable |
| cluster-api.tls.key-file                          | string   | Path to tls key file                               | ""                                          | stable |  

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
