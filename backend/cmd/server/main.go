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
	"bytes"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/gorilla/csrf"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/kubetail-org/kubetail/internal/ginapp"
)

type CLI struct {
	Addr    string `validate:"omitempty,hostname_port"`
	Config  string `validate:"omitempty,file"`
	GinMode string `validate:"omitempty,oneof=debug release"`
}

func configureLogger(config Config) {
	if !config.Logging.Enabled {
		zlog.Logger = zerolog.Nop()
		log.SetOutput(io.Discard)
		return
	}

	// global settings
	zerolog.TimestampFunc = func() time.Time {
		return time.Now().UTC()
	}
	zerolog.TimeFieldFormat = time.RFC3339Nano
	zerolog.DurationFieldUnit = time.Millisecond

	// set log level
	level, err := zerolog.ParseLevel(config.Logging.Level)
	if err != nil {
		panic(err)
	}
	zerolog.SetGlobalLevel(level)

	// configure output format
	if config.Logging.Format == "pretty" {
		zlog.Logger = zlog.Logger.Output(zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339Nano,
		})
	}
}

func toSameSite(input string) http.SameSite {
	switch input {
	case "lax":
		return http.SameSiteLaxMode
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		panic(errors.New("not implemented"))
	}
}

func toCsrfSameSite(input string) csrf.SameSiteMode {
	switch input {
	case "lax":
		return csrf.SameSiteLaxMode
	case "strict":
		return csrf.SameSiteStrictMode
	case "none":
		return csrf.SameSiteNoneMode
	default:
		panic(errors.New("not implemented"))
	}
}

func main() {
	var cli CLI
	var params []string

	// init cobra command
	cmd := cobra.Command{
		Use:   "server",
		Short: "KubeTail Dashboard Server",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// validate cli flags
			return validator.New().Struct(cli)
		},
		Run: func(cmd *cobra.Command, args []string) {
			// init app config
			cfg := DefaultConfig()

			// init viper
			v := viper.New()
			v.BindPFlag("addr", cmd.Flags().Lookup("addr"))
			v.BindPFlag("gin-mode", cmd.Flags().Lookup("gin-mode"))

			// load config from file
			if cli.Config != "" {
				// read contents
				configBytes, err := os.ReadFile(cli.Config)
				if err != nil {
					zlog.Fatal().Caller().Err(err).Send()
				}

				// expand env vars
				configBytes = []byte(os.ExpandEnv(string(configBytes)))

				// load into viper
				v.SetConfigType(filepath.Ext(cli.Config)[1:])
				if err := v.ReadConfig(bytes.NewBuffer(configBytes)); err != nil {
					zlog.Fatal().Caller().Err(err).Send()
				}
			}

			// override params from cli
			for _, param := range params {
				split := strings.SplitN(param, ":", 2)
				if len(split) == 2 {
					v.Set(split[0], split[1])
				}
			}

			// unmarshal
			if err := v.Unmarshal(&cfg); err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}

			// validate config
			if err := cfg.Validate(); err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}

			// set gin mode
			gin.SetMode(v.GetString("gin-mode"))

			// configure logger
			configureLogger(cfg)

			// create app
			appCfg := ginapp.DefaultConfig()
			appCfg.AuthMode = ginapp.AuthMode(cfg.AuthMode)
			appCfg.KubeConfig = cfg.KubeConfig
			appCfg.Namespace = cfg.Namespace
			appCfg.AccessLog.Enabled = cfg.Logging.AccessLog.Enabled
			appCfg.AccessLog.HideHealthChecks = cfg.Logging.AccessLog.HideHealthChecks
			appCfg.Session.Secret = cfg.Session.Secret
			appCfg.Session.Cookie.Name = cfg.Session.Cookie.Name
			appCfg.Session.Cookie.Path = cfg.Session.Cookie.Path
			appCfg.Session.Cookie.Domain = cfg.Session.Cookie.Domain
			appCfg.Session.Cookie.MaxAge = cfg.Session.Cookie.MaxAge
			appCfg.Session.Cookie.Secure = cfg.Session.Cookie.Secure
			appCfg.Session.Cookie.HttpOnly = cfg.Session.Cookie.HttpOnly
			appCfg.Session.Cookie.SameSite = toSameSite(cfg.Session.Cookie.SameSite)
			appCfg.CSRF.Enabled = cfg.CSRF.Enabled
			appCfg.CSRF.Secret = cfg.CSRF.Secret
			appCfg.CSRF.FieldName = cfg.CSRF.FieldName
			appCfg.CSRF.Cookie.Name = cfg.CSRF.Cookie.Name
			appCfg.CSRF.Cookie.Path = cfg.CSRF.Cookie.Path
			appCfg.CSRF.Cookie.Domain = cfg.CSRF.Cookie.Domain
			appCfg.CSRF.Cookie.MaxAge = cfg.CSRF.Cookie.MaxAge
			appCfg.CSRF.Cookie.Secure = cfg.CSRF.Cookie.Secure
			appCfg.CSRF.Cookie.HttpOnly = cfg.CSRF.Cookie.HttpOnly
			appCfg.CSRF.Cookie.SameSite = toCsrfSameSite(cfg.CSRF.Cookie.SameSite)

			app, err := ginapp.NewGinApp(appCfg)
			if err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}

			// create server
			server := http.Server{
				Addr:         v.GetString("addr"),
				Handler:      app,
				IdleTimeout:  1 * time.Minute,
				ReadTimeout:  5 * time.Second,
				WriteTimeout: 10 * time.Second,
			}

			// run server
			zlog.Info().Msg("Starting server on " + v.GetString("addr"))
			if err := server.ListenAndServe(); err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}
		},
	}

	// define flags
	flagset := cmd.Flags()
	flagset.SortFlags = false
	flagset.StringVarP(&cli.Config, "config", "c", "", "Path to configuration file (e.g. \"/etc/kubetail/server.yaml\")")
	flagset.StringP("addr", "a", ":4000", "Host address to bind to")
	flagset.String("gin-mode", "release", "Gin mode (release, debug)")

	// execute command
	if err := cmd.Execute(); err != nil {
		zlog.Fatal().Caller().Err(err).Send()
	}
}
