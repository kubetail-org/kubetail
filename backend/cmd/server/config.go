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
	AuthMode            ginapp.AuthMode `mapstructure:"auth-mode" validate:"oneof=cluster token local"`
	KubeConfig          string          `mapstructure:"kube-config"`
	WSXSProtectStrategy string          `mapstructure:"ws-xs-protect-strategy" validate:"oneof=cookie none"`
	Namespace           string

	// session options
	Session struct {
		Secret string

		// cookie options
		Cookie struct {
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

		// enable http access request logging
		AccessLogEnabled bool `mapstructure:"access-log-enabled"`
	}
}

// Validate config
func (cfg *Config) Validate() error {
	return validator.New().Struct(cfg)
}

func DefaultConfig() Config {
	home, _ := os.UserHomeDir()

	cfg := Config{}

	cfg.AuthMode = "token"
	cfg.KubeConfig = filepath.Join(home, ".kube", "config")
	cfg.WSXSProtectStrategy = "cookie"
	cfg.Namespace = ""

	cfg.Session.Secret = ""
	cfg.Session.Cookie.Path = "/"
	cfg.Session.Cookie.Domain = ""
	cfg.Session.Cookie.MaxAge = 36400 * 30
	cfg.Session.Cookie.Secure = false
	cfg.Session.Cookie.HttpOnly = true
	cfg.Session.Cookie.SameSite = "lax"

	cfg.CSRF.Enabled = true
	cfg.CSRF.Secret = ""
	cfg.CSRF.FieldName = "csrf_token"
	cfg.CSRF.Cookie.Name = "csrf"
	cfg.CSRF.Cookie.Path = "/"
	cfg.CSRF.Cookie.Domain = ""
	cfg.CSRF.Cookie.MaxAge = 43200
	cfg.CSRF.Cookie.Secure = false
	cfg.CSRF.Cookie.HttpOnly = true
	cfg.CSRF.Cookie.SameSite = "strict"

	cfg.Logging.Enabled = true
	cfg.Logging.Level = "info"
	cfg.Logging.Format = "json"
	cfg.Logging.AccessLogEnabled = true
	return cfg
}
