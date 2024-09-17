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

package ginapp

import (
	"fmt"
	"html/template"
	"net"
	"path"

	grpcdispatcher "github.com/kubetail-org/grpc-dispatcher-go"
	zlog "github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/client-go/rest"

	"github.com/kubetail-org/kubetail/backend/common/config"
	"github.com/kubetail-org/kubetail/backend/common/grpchelpers"
	"github.com/kubetail-org/kubetail/backend/server/internal/k8shelpers"
)

const k8sTokenSessionKey = "k8sToken"
const k8sTokenCtxKey = "k8sToken"

func mustConfigureK8S(cfg *config.Config) *rest.Config {
	opts := k8shelpers.Options{KubeConfig: cfg.KubeConfig, Mode: k8shelpers.ModeCluster}
	switch cfg.AuthMode {
	case config.AuthModeCluster:
		opts.Mode = k8shelpers.ModeCluster
	case config.AuthModeLocal:
		opts.Mode = k8shelpers.ModeLocal
	default:
		opts.Mode = k8shelpers.ModeToken
	}
	return k8shelpers.MustConfigure(opts)
}

func mustLoadTemplatesWithFuncs(glob string) *template.Template {
	funcMap := template.FuncMap{
		"pathJoin": path.Join,
	}

	tmpl := template.New("").Funcs(funcMap)

	// parse templates from a specified directory or pattern
	parsedTemplates, err := tmpl.ParseGlob(glob)
	if err != nil {
		panic(err)
	}

	return parsedTemplates
}

func mustNewGrpcDispatcher(cfg *config.Config) *grpcdispatcher.Dispatcher {
	dialOpts := []grpc.DialOption{
		grpc.WithUnaryInterceptor(grpchelpers.NewUnaryAuthClientInterceptor(cfg)),
	}

	// configure tls
	if cfg.Agent.TLS.Enabled {
		creds, err := credentials.NewServerTLSFromFile(cfg.Agent.TLS.CertFile, cfg.Agent.TLS.KeyFile)
		if err != nil {
			zlog.Fatal().Err(err).Send()
		}
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(creds))
	} else {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// get port
	_, port, err := net.SplitHostPort(cfg.Agent.Addr)
	if err != nil {
		zlog.Fatal().Err(err).Send()
	}

	// TODO: reuse app clientset
	d, err := grpcdispatcher.NewDispatcher(
		fmt.Sprintf("kubernetes://kubetail-agent:%s", port),
		grpcdispatcher.WithDialOptions(dialOpts...),
	)
	if err != nil {
		zlog.Fatal().Err(err).Send()
	}

	// start background processes
	d.Start()

	return d
}
