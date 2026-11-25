// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package pokepb holds the generated gRPC bindings for the PokeService defined
// in apps/desktop/proto/.
//
// The bindings are produced by protoc; regenerate them with `make generate`
// after editing apps/desktop/proto/poke.proto. The generated *.pb.go files
// are committed. Never hand-edit them.
package pokepb

//go:generate protoc --proto_path=../../../proto --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative ../../../proto/poke.proto
