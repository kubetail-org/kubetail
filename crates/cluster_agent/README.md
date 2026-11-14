# Kubetail Cluster Agent

A tonic-based gRPC service that runs on every node in a cluster and responds to requests from Kubetail Cluster API instances

## Overview

The Kubetail Cluster Agent is a small Rust gRPC service that's designed to run on every node in a Kubernetes cluster and respond to node-specific requests from Kubetail Cluster API instances using gRPC. Currently, the agent returns real-time information about container log files such as file size and when the last event occurred.

## Configure

### CLI

The Kubetail Cluster Agent executable supports the following command line configuration options:

| Flag         | Datatype | Description                             | Default  |
| ------------ | -------- | --------------------------------------- | -------- |
| -c, --config | string   | Path to Kubetail config file (required) |          |
| -a, --addr   | string   | Host address to bind to                 | ":50051" |
| -p, --param  | []string | Config params ("key:val" format)        | []       |

### Config file

The Cluster Agent reads a config file in YAML, JSON, TOML, INI, RON or JSON5 format, chosen by its extension. It replaces `${NAME}` with the value of the environment variable `NAME`. Keys are top-level. [config/default/cluster-agent.yaml](../../config/default/cluster-agent.yaml) documents every key and its default.

`-p key:value` overrides a key, and takes precedence over the file and the defaults.

## gRPC

The gRPC services implemented by the Kubetail Cluster Agent are documented in [cluster_agent.proto](../../proto/cluster_agent.proto).

## Test

To run the test suite execute this command in the crates directory:

```console
cargo test
```
