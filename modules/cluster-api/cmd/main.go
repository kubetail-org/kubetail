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

package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	zlog "github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/kubetail-org/kubetail/modules/shared/logging"

	"github.com/kubetail-org/kubetail/modules/cluster-api/internal/app"
	"github.com/kubetail-org/kubetail/modules/cluster-api/internal/helpers"
	"github.com/kubetail-org/kubetail/modules/cluster-api/pkg/config"
)

type CLI struct {
	Config string `validate:"omitempty,file"`
}

func main() {
	// Init cobra command
	cmd := cobra.Command{
		Use:   "cluster-api",
		Short: "Kubetail Cluster API",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// Validate CLI flags
			var cli CLI
			cli.Config, _ = cmd.Flags().GetString("config")
			return validator.New().Struct(cli)
		},
		Run: func(cmd *cobra.Command, args []string) {
			// Listen for termination signals as early as possible
			quit := make(chan os.Signal, 1)
			signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
			defer close(quit)

			// Init config
			cfg, err := loadConfig(cmd)
			if err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}

			// set gin mode
			gin.SetMode(cfg.GinMode)

			// Configure logger
			logging.ConfigureLogger(logging.LoggerOptions{
				Enabled: cfg.Logging.Enabled,
				Level:   cfg.Logging.Level,
				Format:  cfg.Logging.Format,
			})

			// Create app
			app, err := app.NewApp(cfg)
			if err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}

			// Listener is fixed at TLS 1.2 + RequestClientCert; the auth
			// middleware is the actual auth boundary.
			server := &http.Server{
				Addr:        cfg.Addr,
				Handler:     app,
				IdleTimeout: 1 * time.Minute,
				ReadTimeout: 5 * time.Second,
				TLSConfig:   helpers.BuildTLSConfig(),
			}

			// Register shutdown hook so long-lived connections are notified before
			// server.Shutdown() waits for regular requests to drain
			server.RegisterOnShutdown(app.NotifyShutdown)

			// Run server in goroutine
			go func() {
				zlog.Info().Msg("Starting server on " + cfg.Addr)
				serverErr := server.ListenAndServeTLS(cfg.TLS.CertFile, cfg.TLS.KeyFile)
				if serverErr != nil && serverErr != http.ErrServerClosed {
					zlog.Fatal().Caller().Err(serverErr).Send()
				}
			}()

			// wait for termination signal
			<-quit

			zlog.Info().Msg("Starting graceful shutdown...")

			// graceful shutdown with 30 second deadline
			// TODO: make timeout configurable
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Stop accepting new connections; triggers RegisterOnShutdown callbacks
			server.Shutdown(ctx)

			// Wait for open WebSocket connections to drain
			if err := app.DrainWithContext(ctx); err != nil {
				zlog.Error().Err(err).Send()
			}

			// Release resources (gRPC dispatcher, ConnectionManager)
			app.Close()

			zlog.Info().Msg("Completed shutdown")
		},
	}

	addFlags(&cmd)

	// Execute command
	if err := cmd.Execute(); err != nil {
		zlog.Fatal().Caller().Err(err).Send()
	}
}

func addFlags(cmd *cobra.Command) {
	flagset := cmd.Flags()
	flagset.SortFlags = false
	flagset.StringP("config", "c", "", "Path to configuration file (e.g. \"/etc/kubetail/cluster-api.yaml\")")
	flagset.StringP("addr", "a", ":8080", "Host address to bind to")
	flagset.String("gin-mode", "release", "Gin mode (release, debug)")
	flagset.StringArrayP("param", "p", []string{}, "Config params")
}

// loadConfig builds the config from, in order of precedence: params, flags
// set on the command line, the config file, the defaults.
func loadConfig(cmd *cobra.Command) (*config.Config, error) {
	flags := cmd.Flags()
	configPath, _ := flags.GetString("config")
	params, _ := flags.GetStringArray("param")

	v := viper.New()
	v.BindPFlag("addr", flags.Lookup("addr"))
	v.BindPFlag("gin-mode", flags.Lookup("gin-mode"))

	for _, param := range params {
		if key, value, ok := strings.Cut(param, ":"); ok {
			v.Set(key, value)
		}
	}

	return config.NewConfig(configPath, v)
}
