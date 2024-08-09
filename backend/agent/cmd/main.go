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

package main

import (
	"net"
	"os"
	"strings"

	"github.com/go-playground/validator/v10"
	zlog "github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/kubetail-org/kubetail/backend/agent/internal/server"
	"github.com/kubetail-org/kubetail/backend/agent/internal/services/logmetadata2"
	"github.com/kubetail-org/kubetail/backend/common/agentpb"
	"github.com/kubetail-org/kubetail/backend/common/config"
	"github.com/kubetail-org/kubetail/backend/common/k8shelpers"
)

type CLI struct {
	Addr   string `validate:"omitempty,hostname_port"`
	Config string `validate:"omitempty,file"`
}

func main() {
	var cli CLI
	var params []string

	// init cobra command
	cmd := cobra.Command{
		Use:   "kubetail-agent",
		Short: "Kubetail Backend Agent",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// validate cli flags
			return validator.New().Struct(cli)
		},
		Run: func(cmd *cobra.Command, args []string) {
			// init viper
			v := viper.New()
			v.BindPFlag("agent.addr", cmd.Flags().Lookup("addr"))

			// override params from cli
			for _, param := range params {
				split := strings.SplitN(param, ":", 2)
				if len(split) == 2 {
					v.Set(split[0], split[1])
				}
			}

			// init config
			cfg, err := config.NewConfig(v, cli.Config)
			if err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}

			// configure logger
			config.ConfigureLogger(config.LoggerOptions{
				Enabled: cfg.Agent.Logging.Enabled,
				Level:   cfg.Agent.Logging.Level,
				Format:  cfg.Agent.Logging.Format,
			})

			// configure k8s
			k8sCfg, err := k8shelpers.Configure(cfg)
			if err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}

			// init service
			svc, err := logmetadata2.NewLogMetadataService(k8sCfg, os.Getenv("NODE_NAME"), cfg.Agent.ContainerLogsDir)
			if err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}

			// init grpc server
			grpcServer := server.NewServer(cfg)
			agentpb.RegisterLogMetadataServiceServer(grpcServer, svc)

			// init listener
			lis, err := net.Listen("tcp", cfg.Agent.Addr)
			if err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}

			// start grpc server
			zlog.Info().Msg("Starting server on " + cfg.Agent.Addr)
			if err := grpcServer.Serve(lis); err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}
		},
	}

	// define flags
	flagset := cmd.Flags()
	flagset.SortFlags = false
	flagset.StringVarP(&cli.Config, "config", "c", "", "Path to configuration file (e.g. \"/etc/kubetail/config.yaml\")")
	flagset.StringP("addr", "a", ":50051", "Host address to bind to")
	flagset.StringArrayVarP(&params, "param", "p", []string{}, "Config params")

	// execute command
	if err := cmd.Execute(); err != nil {
		zlog.Fatal().Caller().Err(err).Send()
	}
}
