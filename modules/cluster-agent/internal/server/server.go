// Copyright 2024-2025 Andres Morey
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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/kubetail-org/kubetail/modules/shared/grpchelpers"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
)

func NewServer(cfg *config.Config) (*grpc.Server, error) {
	// Init server options
	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(grpchelpers.AuthUnaryServerInterceptor),
		grpc.StreamInterceptor(grpchelpers.AuthStreamServerInterceptor),
	}

	// Configure tls
	if cfg.ClusterAgent.TLS.Enabled {
		// Load server cert and key
		serverCert, err := tls.LoadX509KeyPair(cfg.ClusterAgent.TLS.CertFile, cfg.ClusterAgent.TLS.KeyFile)
		if err != nil {
			return nil, err
		}

		// Init tls config
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{serverCert},
			ClientAuth:   cfg.ClusterAgent.TLS.ClientAuth,
		}

		// Add CA bundle for mTLS
		if cfg.ClusterAgent.TLS.CAFile != "" {
			// Load CA cert to validate client certs
			caPEM, err := os.ReadFile(cfg.ClusterAgent.TLS.CAFile)
			if err != nil {
				return nil, err
			}

			// Init cert pool
			certPool := x509.NewCertPool()
			if !certPool.AppendCertsFromPEM(caPEM) {
				return nil, fmt.Errorf("failed to append CA cert to pool")
			}

			tlsConfig.ClientCAs = certPool
		}

		opts = append(opts, grpc.Creds(credentials.NewTLS(tlsConfig)))
	}

	// Add otel stats handler if tracing is enabled
	if cfg.ClusterAgent.OTel.Enabled {
		opts = append(opts, grpc.StatsHandler(otelgrpc.NewServerHandler()))
	}

	return grpc.NewServer(opts...), nil
}
