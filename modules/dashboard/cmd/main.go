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
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-logr/zerologr"
	"github.com/go-playground/validator/v10"
	zlog "github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	k8sruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"

	"github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"

	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/app"
)

type CLI struct {
	Config string `validate:"omitempty,file"`
}

func main() {
	var cli CLI
	var params []string

	// Init cobra command
	cmd := cobra.Command{
		Use:   "dashboard",
		Short: "Kubetail Dashboard",
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
			v.BindPFlag("dashboard.addr", cmd.Flags().Lookup("addr"))
			v.BindPFlag("dashboard.gin-mode", cmd.Flags().Lookup("gin-mode"))

			// Init config
			cfg, err := config.NewConfig(v, cli.Config)
			if err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}

			// set gin mode
			gin.SetMode(cfg.Dashboard.GinMode)

			// Override params from cli
			for _, param := range params {
				split := strings.SplitN(param, ":", 2)
				if len(split) == 2 {
					v.Set(split[0], split[1])
				}
			}

			// Configure logger
			config.ConfigureLogger(config.LoggerOptions{
				Enabled: cfg.Dashboard.Logging.Enabled,
				Level:   cfg.Dashboard.Logging.Level,
				Format:  cfg.Dashboard.Logging.Format,
			})

			// Capture unhandled kubernetes client errors
			k8sruntime.ErrorHandlers = []k8sruntime.ErrorHandler{func(ctx context.Context, err error, msg string, keysAndValues ...interface{}) {
				zlog.Error().Err(err).Msg(msg)
			}}

			// Capture messages sent to klog
			klog.SetLogger(zerologr.New(&zlog.Logger))

			// Capture messages sent to system logger
			log.SetOutput(k8shelpers.NewZlogWriter(zlog.Logger))

			// Create app
			app, err := app.NewApp(cfg)
			if err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}

			// create server
			server := http.Server{
				Addr:         cfg.Dashboard.Addr,
				Handler:      app,
				IdleTimeout:  1 * time.Minute,
				ReadTimeout:  5 * time.Second,
				WriteTimeout: 10 * time.Second,
			}

			// run server in go routine
			go func() {
				var serverErr error
				zlog.Info().Msg("Starting server on " + cfg.Dashboard.Addr)

				if cfg.Dashboard.TLS.Enabled {
					serverErr = server.ListenAndServeTLS(cfg.Dashboard.TLS.CertFile, cfg.Dashboard.TLS.KeyFile)
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

			// Attempt graceful shutdown
			done := make(chan struct{})
			go func() {
				if err := server.Shutdown(ctx); err != nil {
					zlog.Error().Err(err).Send()
				}
				close(done)
			}()

			// Shutdown app
			// TODO: handle long-lived requests shutdown (e.g. websockets)
			app.Shutdown()

			select {
			case <-done:
				zlog.Info().Msg("Completed graceful shutdown")
			case <-ctx.Done():
				zlog.Info().Msg("Exceeded deadline, exiting now")
			}
		},
	}

	// Define flags
	flagset := cmd.Flags()
	flagset.SortFlags = false
	flagset.StringVarP(&cli.Config, "config", "c", "", "Path to configuration file (e.g. \"/etc/kubetail/dashboard.yaml\")")
	flagset.StringP("addr", "a", ":80", "Host address to bind to")
	flagset.String("gin-mode", "release", "Gin mode (release, debug)")
	flagset.StringArrayVarP(&params, "param", "p", []string{}, "Config params")

	// Execute command
	if err := cmd.Execute(); err != nil {
		zlog.Fatal().Caller().Err(err).Send()
	}
}
