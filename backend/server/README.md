# Kubetail Backend Server

Go-based HTTP server that handles web requests from the Kubetail frontend

## Overview

The Kubetail backend server is a Go-based HTTP server that's designed to proxy requests from the Kubetail frontend to the user's Kubernetes API and to the Kubetail backend agents as well as provide some other custom functionality such as authentication. It uses the Gin Web framework to serve HTTP requests. Under the hood, it uses the Kubernetes Go-client to communicate with the Kubernetes API and gRPC to communicate with Kubetail backend agents. Externally, it responds to Kubernetes-related queries via a GraphQL endpoint powered by [gqlgen](https://github.com/99designs/gqlgen) and serves other requests via a simple REST API.

In development, the backend and frontend servers are kept separate but in production the frontend website is packaged as a static site and deployed to the server's `website` directory from where it is served at the apex endpoint.

## GraphQL

The GraphQL schema can be found here: [GraphQL schema](graph/schema.graphqls). To run the gqlgen GraphQL code generator use the `go generate` command:

```console
go generate ./...
```

## Test

This project uses the [stretchr/testify](https://github.com/stretchr/testify) library for testing. To run the test suite execute this command:

```console
go test ./...
```
