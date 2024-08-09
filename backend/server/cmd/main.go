package main

import (
	"fmt"
	"net/http"
	"strings"
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
			fmt.Printf("config path: %s\n", cli.Config)
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
			defer app.Teardown()

			// create server
			server := http.Server{
				Addr:         cfg.Server.Addr,
				Handler:      app,
				IdleTimeout:  1 * time.Minute,
				ReadTimeout:  5 * time.Second,
				WriteTimeout: 10 * time.Second,
			}

			// run server
			var serverErr error
			zlog.Info().Msg("Starting server on " + cfg.Server.Addr)

			if cfg.Server.TLS.Enabled {
				serverErr = server.ListenAndServeTLS(cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile)
			} else {
				serverErr = server.ListenAndServe()
			}

			if serverErr != nil {
				zlog.Fatal().Caller().Err(err).Send()
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
