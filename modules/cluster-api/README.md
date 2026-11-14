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

The Cluster API reads a config file in YAML, JSON, TOML, HCL or envfile format, chosen by its extension. It replaces `${NAME}` with the value of the environment variable `NAME`. Keys are top-level. [config/default/cluster-api.yaml](../../config/default/cluster-api.yaml) documents every key and its default.

`-p key:value` overrides a key. Precedence is `-p`, then the flags, then the file, then the defaults.

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
