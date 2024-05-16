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
	AuthMode          ginapp.AuthMode `mapstructure:"auth-mode" validate:"oneof=cluster token local"`
	KubeConfig        string          `mapstructure:"kube-config"`
	BasePath          string          `mapstructure:"base-path"`
	AllowedNamespaces []string        `mapstructure:"allowed-namespaces"`

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

	// TLS options
	TLS struct {
		// enable tls termination
		Enabled bool

		// TLS certificate file
		CertFile string `mapstructure:"cert-file" validate:"omitempty,file"`

		// TLS certificate key file
		KeyFile string `mapstructure:"key-file" validate:"omitempty,file"`
	}
}

// Validate config
func (cfg *Config) Validate() error {
	return validator.New().Struct(cfg)
}

func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	appDefault := ginapp.DefaultConfig()

	cfg := Config{}

	cfg.AuthMode = appDefault.AuthMode
	cfg.KubeConfig = filepath.Join(home, ".kube", "config")
	cfg.BasePath = appDefault.BasePath
	cfg.AllowedNamespaces = appDefault.AllowedNamespaces

	cfg.Session.Secret = appDefault.Session.Secret
	cfg.Session.Cookie.Name = appDefault.Session.Cookie.Name
	cfg.Session.Cookie.Path = appDefault.Session.Cookie.Path
	cfg.Session.Cookie.Domain = appDefault.Session.Cookie.Domain
	cfg.Session.Cookie.MaxAge = appDefault.Session.Cookie.MaxAge
	cfg.Session.Cookie.Secure = appDefault.Session.Cookie.Secure
	cfg.Session.Cookie.HttpOnly = appDefault.Session.Cookie.HttpOnly
	cfg.Session.Cookie.SameSite = fromSameSite(appDefault.Session.Cookie.SameSite)

	cfg.CSRF.Enabled = appDefault.CSRF.Enabled
	cfg.CSRF.Secret = appDefault.CSRF.Secret
	cfg.CSRF.FieldName = appDefault.CSRF.FieldName
	cfg.CSRF.Cookie.Name = appDefault.CSRF.Cookie.Name
	cfg.CSRF.Cookie.Path = appDefault.CSRF.Cookie.Path
	cfg.CSRF.Cookie.Domain = appDefault.CSRF.Cookie.Domain
	cfg.CSRF.Cookie.MaxAge = appDefault.CSRF.Cookie.MaxAge
	cfg.CSRF.Cookie.Secure = appDefault.CSRF.Cookie.Secure
	cfg.CSRF.Cookie.HttpOnly = appDefault.CSRF.Cookie.HttpOnly
	cfg.CSRF.Cookie.SameSite = fromCsrfSameSite(appDefault.CSRF.Cookie.SameSite)

	cfg.Logging.Enabled = true
	cfg.Logging.Level = "info"
	cfg.Logging.Format = "json"
	cfg.Logging.AccessLog.Enabled = appDefault.AccessLog.Enabled
	cfg.Logging.AccessLog.HideHealthChecks = appDefault.AccessLog.HideHealthChecks

	cfg.TLS.Enabled = false
	cfg.TLS.CertFile = ""
	cfg.TLS.KeyFile = ""

	return cfg
}
