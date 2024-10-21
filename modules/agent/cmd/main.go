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
	"context"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-playground/validator/v10"
	zlog "github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/kubetail-org/kubetail/modules/agent/internal/server"
	"github.com/kubetail-org/kubetail/modules/agent/internal/services/logmetadata"
	"github.com/kubetail-org/kubetail/modules/common/agentpb"
	"github.com/kubetail-org/kubetail/modules/common/config"
	"github.com/kubetail-org/kubetail/modules/common/k8shelpers"
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
			// listen for termination signals as early as possible
			quit := make(chan os.Signal, 1)
			signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
			defer close(quit)

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

			// init grpc server
			grpcServer, err := server.NewServer(cfg)
			if err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}

			// init logmetadata service
			svc, err := logmetadata.NewLogMetadataService(k8sCfg, os.Getenv("NODE_NAME"), cfg.Agent.ContainerLogsDir)
			if err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}

			// register logmetadata service
			agentpb.RegisterLogMetadataServiceServer(grpcServer, svc)

			// create health server
			healthServer := health.NewServer()

			// register health server
			grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)

			// set overall health status
			healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

			// init listener
			lis, err := net.Listen("tcp", cfg.Agent.Addr)
			if err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}

			// run server in go routine
			go func() {
				zlog.Info().Msg("Starting kubetail-agent on " + cfg.Agent.Addr)
				if err := grpcServer.Serve(lis); err != nil {
					zlog.Fatal().Caller().Err(err).Send()
				}
			}()

			// wait for termination signal
			<-quit

			// shutdown server
			zlog.Info().Msg("Starting graceful shutting...")

			// graceful shutdown with 30 sec deadline
			// TODO: make timeout configurable
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// start graceful shutdown
			done := make(chan struct{})
			go func() {
				grpcServer.GracefulStop()
				close(done)
			}()

			// shutdown service
			svc.Shutdown()

			select {
			case <-done:
				zlog.Info().Msg("Completed graceful shutdown")
			case <-ctx.Done():
				zlog.Error().Msg("Exceeded deadline, shutting down forcefully")
				grpcServer.Stop()
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
