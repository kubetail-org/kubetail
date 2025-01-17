# Kubetail Cluster Agent

Go-based gRPC service that runs on every node in a cluster and responds to requests from Kubetail Cluster API instances

## Overview

The Kubetail Cluster Agent is a small Go-based gRPC service that's designed to run on every node in a Kubernetes cluster and respond to node-specific requests from Kubetail Cluster API instances using gRPC. Currently, the agent returns real-time information about container log files such as file size and when the last event occurred.

## Configure

### CLI

The Kubetail Cluster Agent executable supports the following command line configuration options:

| Flag         | Datatype | Description                      | Default  |
| ------------ | -------- | -------------------------------- | -------- |
| -c, --config | string   | Path to Kubetail config file     | ""       |
| -a, --addr   | string   | Host address to bind to          | ":50051" |
| -p, --param  | []string | Config params ("key:val" format) | []       |

### Config params

The Kubetail Cluster Agent can be configured using a configuration file written in YAML, JSON, TOML, HCL or envfile format. The application will automatically replace ENV variables written in the format `${NAME}` with their corresponding values. The config file supports the following options (also see [hack/config.yaml](../../hack/config.yaml)):

| Name                  | Datatype | Description                              | Default  |
| --------------------- | -------- | ---------------------------------------- | -------- |
| auth-mode             | string   | Auth mode (token, cluster, local)        | "token"  |
| allowed-namespaces    | []string | If populated, restricts namespace access | []       |
| agent.addr            | string   | Host address to bind to                  | ":50051" |
| agent.logging.enabled | bool     | Enable logging                           | true     |
| agent.logging.level   | string   | Log level                                | "info"   |
| agent.logging.format  | string   | Log format (json, pretty)                | "json"   |
| agent.tls.enabled     | bool     | Enable TLS endpoint termination          | false    |
| agent.tls.cert-file   | string   | Path to cert file                        | ""       |
| agent.tls.key-file    | string   | Path to key file                         | ""       |

## gRPC

The Kubetail Cluster Agent implements the gRPC service `LogMetadataService` documented in [agent.proto](../../proto/agent.proto).

## Test

To run the test suite execute this command:

```console
go test ./...
```
