// Copyright 2024 Andres Morey
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

package server

import (
	"google.golang.org/grpc"

	"github.com/kubetail-org/kubetail/backend/agent/internal/grpchelpers"
	"github.com/kubetail-org/kubetail/backend/common/config"
)

func NewServer(cfg *config.Config) *grpc.Server {
	// init grpc server
	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(grpchelpers.NewUnaryAuthServerInterceptor(cfg)),
	}
	return grpc.NewServer(opts...)
}
