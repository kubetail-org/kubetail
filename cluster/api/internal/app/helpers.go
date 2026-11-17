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

package app

import (
	"crypto/tls"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	zlog "github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	grpcdispatcher "github.com/kubetail-org/grpc-dispatcher-go"

	"github.com/kubetail-org/kubetail/libs/go-common/grpchelpers"

	"github.com/kubetail-org/kubetail/cluster/api/internal/helpers"
	"github.com/kubetail-org/kubetail/cluster/api/pkg/config"
)

func healthzHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func mustNewGrpcDispatcher(cfg *config.Config) *grpcdispatcher.Dispatcher {
	// The cluster-agent strictly requires mTLS, so the cluster-api always
	// connects with a client cert and verifies the server against the
	// configured CA. Validation of the file paths happens in config.NewConfig.
	clientCert, err := tls.LoadX509KeyPair(cfg.ClusterAgent.TLS.CertFile, cfg.ClusterAgent.TLS.KeyFile)
	if err != nil {
		zlog.Fatal().Err(err).Send()
	}

	caPem, err := os.ReadFile(cfg.ClusterAgent.TLS.CAFile)
	if err != nil {
		zlog.Fatal().Err(err).Send()
	}
	roots, err := helpers.PoolFromPEM(string(caPem))
	if err != nil {
		zlog.Fatal().Err(err).Send()
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		ServerName:   cfg.ClusterAgent.TLS.ServerName,
		RootCAs:      roots,
	}

	dialOpts := []grpc.DialOption{
		grpc.WithUnaryInterceptor(grpchelpers.ImpersonateUnaryClientInterceptor),
		grpc.WithStreamInterceptor(grpchelpers.ImpersonateStreamClientInterceptor),
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
	}

	// TODO: reuse app clientset
	d, err := grpcdispatcher.NewDispatcher(
		cfg.ClusterAgent.DispatchUrl,
		grpcdispatcher.WithDialOptions(dialOpts...),
	)
	if err != nil {
		zlog.Fatal().Err(err).Send()
	}

	// start background processes
	d.Start()

	return d
}
