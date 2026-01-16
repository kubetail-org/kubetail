# Kubetail Cluster Agent

A tonic-based gRPC service that runs on every node in a cluster and responds to requests from Kubetail Cluster API instances

## Overview

The Kubetail Cluster Agent is a small Rust gRPC service that's designed to run on every node in a Kubernetes cluster and respond to node-specific requests from Kubetail Cluster API instances using gRPC. Currently, the agent returns real-time information about container log files such as file size and when the last event occurred.

## Configure

### CLI

The Kubetail Cluster Agent executable supports the following command line configuration options:

| Flag         | Datatype | Description                      | Default  |
| ------------ | -------- | -------------------------------- | -------- |
| -c, --config | string   | Path to Kubetail config file     | ""       |
| -a, --addr   | string   | Host address to bind to          | ":50051" |
| -p, --param  | []string | Config params ("key:val" format) | []       |

### Config params

The Kubetail Cluster Agent can be configured using a configuration file written in YAML, JSON, TOML, INI, RON or JSON5 format. The application will automatically replace ENV variables written in the format `${NAME}` with their corresponding values. The config file supports the following options (also see [config/default/cluster-agent.yaml](../../config/default/cluster-agent.yaml)):

| Name                         | Datatype | Description                              | Default  | Status |
| ---------------------------- | -------- | ---------------------------------------- | -------- | ------ |
| allowed-namespaces           | []string | If populated, restricts namespace access | []       | stable |
| cluser-agent.addr            | string   | Host address to bind to                  | ":50051" | stable |
| cluser-agent.logging.enabled | bool     | Enable logging                           | true     | stable |
| cluser-agent.logging.level   | string   | Log level                                | "info"   | stable |
| cluser-agent.logging.format  | string   | Log format (json, pretty)                | "json"   | stable |
| cluser-agent.tls.enabled     | bool     | Enable TLS endpoint termination          | false    | stable |
| cluser-agent.tls.cert-file   | string   | Path to cert file                        | ""       | stable |
| cluser-agent.tls.key-file    | string   | Path to key file                         | ""       | stable |
| cluser-agent.tls.ca-file     | string   | Path to client CA bundle file            | ""       | alpha  |
| cluser-agent.tls.client-auth | string   | Controls client cert authentication      | "none"   | alpha  |

## gRPC

The gRPC services implemented by the Kubetail Cluster Agent are documented in [cluster_agent.proto](../../proto/cluster_agent.proto).

## Test

To run the test suite execute this command in the crates directory:

```console
cargo test
```
