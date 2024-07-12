# Backend Agent

Kubetail backend agent

## Overview

The kubetail backend agent is a Go-based gRPC server that's designed to run on every node in a kubernetes cluster and field node-specific requests from the kubetail backend server. Currently, it returns realtime information about the container log files on disk.
