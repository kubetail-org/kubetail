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

package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	zlog "github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/kubetail-org/kubetail/modules/shared/config"

	"github.com/kubetail-org/kubetail/modules/cluster-api/internal/app"
)

type CLI struct {
	Config string `validate:"omitempty,file"`
}

func main() {
	var cli CLI
	var params []string

	// Init cobra command
	cmd := cobra.Command{
		Use:   "cluster-api",
		Short: "Kubetail Cluster API",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// Validate CLI flags
			return validator.New().Struct(cli)
		},
		Run: func(cmd *cobra.Command, args []string) {
			// Listen for termination signals as early as possible
			quit := make(chan os.Signal, 1)
			signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
			defer close(quit)

			// Init viper
			v := viper.New()
			v.BindPFlag("cluster-api.addr", cmd.Flags().Lookup("addr"))
			v.BindPFlag("cluster-api.gin-mode", cmd.Flags().Lookup("gin-mode"))

			// Init config
			cfg, err := config.NewConfig(v, cli.Config)
			if err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}

			// set gin mode
			gin.SetMode(cfg.ClusterAPI.GinMode)

			// Override params from cli
			for _, param := range params {
				split := strings.SplitN(param, ":", 2)
				if len(split) == 2 {
					v.Set(split[0], split[1])
				}
			}

			// Configure logger
			config.ConfigureLogger(config.LoggerOptions{
				Enabled: cfg.ClusterAPI.Logging.Enabled,
				Level:   cfg.ClusterAPI.Logging.Level,
				Format:  cfg.ClusterAPI.Logging.Format,
			})

			// Create app
			app, err := app.NewApp(cfg)
			if err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}

			// create server
			server := http.Server{
				Addr:         cfg.ClusterAPI.Addr,
				Handler:      app,
				IdleTimeout:  1 * time.Minute,
				ReadTimeout:  5 * time.Second,
				WriteTimeout: 10 * time.Second,
			}

			// run server in go routine
			go func() {
				var serverErr error
				zlog.Info().Msg("Starting server on " + cfg.ClusterAPI.Addr)

				if cfg.ClusterAPI.TLS.Enabled {
					serverErr = server.ListenAndServeTLS(cfg.ClusterAPI.TLS.CertFile, cfg.ClusterAPI.TLS.KeyFile)
				} else {
					serverErr = server.ListenAndServe()
				}

				// log non-normal errors
				if serverErr != nil && serverErr != http.ErrServerClosed {
					zlog.Fatal().Caller().Err(err).Send()
				}
			}()

			// wait for termination signal
			<-quit

			zlog.Info().Msg("Starting graceful shutdown...")

			// graceful shutdown with 30 second deadline
			// TODO: make timeout configurable
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			var wg sync.WaitGroup
			wg.Add(2)

			// attempt graceful shutdown
			go func() {
				defer wg.Done()
				if err := server.Shutdown(ctx); err != nil {
					zlog.Error().Err(err).Send()
				}
			}()

			// shutdown app
			// TODO: handle long-lived requests shutdown (e.g. websockets)
			go func() {
				defer wg.Done()
				if err := app.Shutdown(ctx); err != nil {
					zlog.Error().Err(err).Send()
				}
			}()

			wg.Wait()

			if ctx.Err() == nil {
				zlog.Info().Msg("Completed graceful shutdown")
			}
		},
	}

	// Define flags
	flagset := cmd.Flags()
	flagset.SortFlags = false
	flagset.StringVarP(&cli.Config, "config", "c", "", "Path to configuration file (e.g. \"/etc/kubetail/cluster-api.yaml\")")
	flagset.StringP("addr", "a", ":8080", "Host address to bind to")
	flagset.String("gin-mode", "release", "Gin mode (release, debug)")
	flagset.StringArrayVarP(&params, "param", "p", []string{}, "Config params")

	// Execute command
	if err := cmd.Execute(); err != nil {
		zlog.Fatal().Caller().Err(err).Send()
	}
}
