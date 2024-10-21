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
	"google.golang.org/grpc/credentials"

	"github.com/kubetail-org/kubetail/modules/common/config"
	"github.com/kubetail-org/kubetail/modules/common/grpchelpers"
)

func NewServer(cfg *config.Config) (*grpc.Server, error) {
	// init grpc server
	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(grpchelpers.NewUnaryAuthServerInterceptor(cfg)),
	}

	// configure tls
	if cfg.Agent.TLS.Enabled {
		creds, err := credentials.NewServerTLSFromFile(cfg.Agent.TLS.CertFile, cfg.Agent.TLS.KeyFile)
		if err != nil {
			return nil, err
		}
		opts = append(opts, grpc.Creds(creds))
	}

	return grpc.NewServer(opts...), nil
}
