// Package pokepb holds the generated gRPC bindings for the PokeService defined
// in the repo-root proto/ directory.
//
// The bindings are produced by protoc; regenerate with `make generate-proto` (or
// `go generate ./grpc/...` from the sidecar module) after editing
// ../../../proto/poke.proto. The generated *.pb.go files are committed.
// Never hand-edit them.
package pokepb

//go:generate sh ../../../scripts/protoc.sh --proto_path=../../../proto --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative ../../../proto/poke.proto
