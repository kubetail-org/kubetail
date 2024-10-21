# Kubetail Backend Common

Go module containing shared backend libraries

## Dependencies

```console
go install google.golang.org/protobuf/cmd/protoc-gen-go
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc
```

## gRPC

To run the gRPC code generator use the `go generate` command:

```console
go generate ./...
```
