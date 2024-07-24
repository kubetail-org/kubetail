# Kubetail Backend Agent

Go-based service that runs on every node in a cluster and responds to requests from Kubetail backend server instances

## Overview

The Kubetail backend agent is a small Go-based service that's designed to run on every node in a Kubernetes cluster and respond to node-specific requests from Kubetail backend server instances using gRPC. Currently, the agent returns realtime information about container log files such as file size and when the last event occurred.

## Configure

### CLI

The Kubetail backend agent executable (`kubetail-agent`) supports the following command line configuration options:

| Flag         | Datatype    | Description               | Default   |
| ------------ | ----------- | ------------------------- | --------- |
| -c, --config | string      | Path to config file       | ""        |
| -a, --addr   | string      | Host address to bind to   | ":5000"   |

### Config params
