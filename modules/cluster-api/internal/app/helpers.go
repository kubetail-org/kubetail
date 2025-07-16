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

package app

import (
	"crypto/tls"
	"crypto/x509"
	"os"

	zlog "github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	grpcdispatcher "github.com/kubetail-org/grpc-dispatcher-go"

	"github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/kubetail-org/kubetail/modules/shared/grpchelpers"
)

func mustNewGrpcDispatcher(cfg *config.Config) *grpcdispatcher.Dispatcher {
	dialOpts := []grpc.DialOption{
		grpc.WithUnaryInterceptor(grpchelpers.AuthUnaryClientInterceptor),
		grpc.WithStreamInterceptor(grpchelpers.AuthStreamClientInterceptor),
	}

	// configure tls
	if cfg.ClusterAPI.ClusterAgent.TLS.Enabled {
		// Client cert for mTLS
		clientCert, err := tls.LoadX509KeyPair(cfg.ClusterAPI.ClusterAgent.TLS.CertFile, cfg.ClusterAPI.ClusterAgent.TLS.KeyFile)
		if err != nil {
			zlog.Fatal().Err(err).Send()
		}

		// Init tls config
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{clientCert},
			ServerName:   cfg.ClusterAPI.ClusterAgent.TLS.ServerName,
		}

		if cfg.ClusterAPI.ClusterAgent.TLS.CAFile != "" {
			// Root CA for server verification
			caPem, err := os.ReadFile(cfg.ClusterAPI.ClusterAgent.TLS.CAFile)
			if err != nil {
				zlog.Fatal().Err(err).Send()
			}
			roots := x509.NewCertPool()
			roots.AppendCertsFromPEM(caPem)
			tlsCfg.RootCAs = roots
		} else {
			// Skip verification
			tlsCfg.InsecureSkipVerify = true
		}

		dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	} else {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// TODO: reuse app clientset
	d, err := grpcdispatcher.NewDispatcher(
		cfg.ClusterAPI.ClusterAgent.DispatchUrl,
		grpcdispatcher.WithDialOptions(dialOpts...),
	)
	if err != nil {
		zlog.Fatal().Err(err).Send()
	}

	// start background processes
	d.Start()

	return d
}
