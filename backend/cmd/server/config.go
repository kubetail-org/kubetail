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
	"os"
	"path/filepath"

	"github.com/go-playground/validator/v10"
	"github.com/kubetail-org/kubetail/internal/ginapp"
)

type Config struct {
	AuthMode   ginapp.AuthMode `mapstructure:"auth-mode" validate:"oneof=cluster token local"`
	KubeConfig string          `mapstructure:"kube-config"`
	BasePath   string          `mapstructure:"base-path"`
	Namespace  string

	// session options
	Session struct {
		Secret string

		// cookie options
		Cookie struct {
			Name     string
			Path     string
			Domain   string
			MaxAge   int `mapstructure:"max-age"`
			Secure   bool
			HttpOnly bool   `mapstructure:"http-only"`
			SameSite string `mapstructure:"same-site" validate:"oneof=none strict lax"`
		}
	}

	// csrf options
	CSRF struct {
		Enabled   bool
		Secret    string
		FieldName string `mapstructure:"field-name"`

		// cookie options
		Cookie struct {
			Name     string
			Path     string
			Domain   string
			MaxAge   int `mapstructure:"max-age"`
			Secure   bool
			HttpOnly bool   `mapstructure:"http-only"`
			SameSite string `mapstructure:"same-site" validate:"oneof=none strict lax"`
		}
	}

	// logging options
	Logging struct {
		// enable logging
		Enabled bool

		// log level
		Level string `validate:"oneof=debug info warn error disabled"`

		// log format
		Format string `validate:"oneof=json pretty"`

		// access-log options
		AccessLog struct {
			// enable access-log
			Enabled bool

			// hide health checks
			HideHealthChecks bool `mapstructure:"hide-health-checks"`
		} `mapstructure:"access-log"`
	}
}

// Validate config
func (cfg *Config) Validate() error {
	return validator.New().Struct(cfg)
}

func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	appCfg := ginapp.DefaultConfig()

	cfg := Config{}

	cfg.AuthMode = appCfg.AuthMode
	cfg.KubeConfig = filepath.Join(home, ".kube", "config")
	cfg.BasePath = appCfg.BasePath
	cfg.Namespace = appCfg.Namespace

	cfg.Session.Secret = appCfg.Session.Secret
	cfg.Session.Cookie.Name = appCfg.Session.Cookie.Name
	cfg.Session.Cookie.Path = appCfg.Session.Cookie.Path
	cfg.Session.Cookie.Domain = appCfg.Session.Cookie.Domain
	cfg.Session.Cookie.MaxAge = appCfg.Session.Cookie.MaxAge
	cfg.Session.Cookie.Secure = appCfg.Session.Cookie.Secure
	cfg.Session.Cookie.HttpOnly = appCfg.Session.Cookie.HttpOnly
	cfg.Session.Cookie.SameSite = "lax"

	cfg.CSRF.Enabled = appCfg.CSRF.Enabled
	cfg.CSRF.Secret = appCfg.CSRF.Secret
	cfg.CSRF.FieldName = appCfg.CSRF.FieldName
	cfg.CSRF.Cookie.Name = appCfg.CSRF.Cookie.Name
	cfg.CSRF.Cookie.Path = appCfg.CSRF.Cookie.Path
	cfg.CSRF.Cookie.Domain = appCfg.CSRF.Cookie.Domain
	cfg.CSRF.Cookie.MaxAge = appCfg.CSRF.Cookie.MaxAge
	cfg.CSRF.Cookie.Secure = appCfg.CSRF.Cookie.Secure
	cfg.CSRF.Cookie.HttpOnly = appCfg.CSRF.Cookie.HttpOnly
	cfg.CSRF.Cookie.SameSite = "strict"

	cfg.Logging.Enabled = true
	cfg.Logging.Level = "info"
	cfg.Logging.Format = "json"
	cfg.Logging.AccessLog.Enabled = appCfg.AccessLog.Enabled
	cfg.Logging.AccessLog.HideHealthChecks = appCfg.AccessLog.HideHealthChecks

	return cfg
}
