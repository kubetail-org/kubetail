// Package authpb holds the generated gRPC bindings for the AuthService defined
// in the repo-root proto/ directory.
//
// The bindings are produced by protoc; regenerate with `make generate-proto` (or
// `go generate ./grpc/...` from the sidecar module) after editing
// ../../../proto/auth.proto. The generated *.pb.go files are committed.
// Never hand-edit them.
package authpb

//go:generate sh ../../../scripts/protoc.sh --proto_path=../../../proto --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative ../../../proto/auth.proto
