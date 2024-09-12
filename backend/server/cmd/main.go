package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-playground/validator/v10"
	zlog "github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/kubetail-org/kubetail/backend/common/config"
	"github.com/kubetail-org/kubetail/backend/server/internal/ginapp"
)

type CLI struct {
	Addr    string `validate:"omitempty,hostname_port"`
	Config  string `validate:"omitempty,file"`
	GinMode string `validate:"omitempty,oneof=debug release"`
}

func main() {
	var cli CLI
	var params []string

	// init cobra command
	cmd := cobra.Command{
		Use:   "kubetail-server",
		Short: "Kubetail Backend Server",
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
			v.BindPFlag("server.addr", cmd.Flags().Lookup("addr"))
			v.BindPFlag("server.gin-mode", cmd.Flags().Lookup("gin-mode"))

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
				Enabled: cfg.Server.Logging.Enabled,
				Level:   cfg.Server.Logging.Level,
				Format:  cfg.Server.Logging.Format,
			})

			// create app
			app, err := ginapp.NewGinApp(cfg)
			if err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}

			// create server
			server := http.Server{
				Addr:         cfg.Server.Addr,
				Handler:      app,
				IdleTimeout:  1 * time.Minute,
				ReadTimeout:  5 * time.Second,
				WriteTimeout: 10 * time.Second,
			}

			// run server in go routine
			go func() {
				var serverErr error
				zlog.Info().Msg("Starting server on " + cfg.Server.Addr)

				if cfg.Server.TLS.Enabled {
					serverErr = server.ListenAndServeTLS(cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile)
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

			// attempt graceful shutdown
			done := make(chan struct{})
			go func() {
				if err := server.Shutdown(ctx); err != nil {
					zlog.Error().Err(err).Send()
				}
				close(done)
			}()

			// shutdown app
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

	// define flags
	flagset := cmd.Flags()
	flagset.SortFlags = false
	flagset.StringVarP(&cli.Config, "config", "c", "", "Path to configuration file (e.g. \"/etc/kubetail/config.yaml\")")
	flagset.StringP("addr", "a", ":4000", "Host address to bind to")
	flagset.String("gin-mode", "release", "Gin mode (release, debug)")
	flagset.StringArrayVarP(&params, "param", "p", []string{}, "Config params")

	// execute command
	if err := cmd.Execute(); err != nil {
		zlog.Fatal().Caller().Err(err).Send()
	}
}
