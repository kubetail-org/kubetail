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

package config

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/gorilla/csrf"
	"github.com/mitchellh/mapstructure"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

// Auth-mode
type AuthMode string

const (
	AuthModeAuto  AuthMode = "auto"
	AuthModeToken AuthMode = "token"
)

// Environment
type Environment string

const (
	EnvironmentCluster Environment = "cluster"
	EnvironmentDesktop Environment = "desktop"
)

// Application configuration
type Config struct {
	AllowedNamespaces []string `mapstructure:"allowed-namespaces"`
	KubeconfigPath    string   `mapstructure:"kubeconfig"`

	// Dashboard options
	Dashboard struct {
		AuthMode           AuthMode `mapstructure:"auth-mode"`
		BasePath           string   `mapstructure:"base-path"`
		ClusterAPIEndpoint string   `mapstructure:"cluster-api-endpoint"`
		GinMode            string   `mapstructure:"gin-mode" validate:"omitempty,oneof=debug release"`
		Environment        Environment

		// UI optins
		UI struct {
			ClusterAPIEnabled bool `mapstructure:"cluster-api-enabled"`
		}

		HTTP struct {
			Enabled bool
			Address string `validate:"omitempty,hostname"`
			Port    uint   `validate:"omitempty,port"`
		}

		HTTPS struct {
			Enabled bool
			Address string `validate:"omitempty,hostname"`
			Port    uint   `validate:"omitempty,port"`
			TLS     struct {
				// TLS certificate file
				CertFile string `mapstructure:"cert-file" validate:"omitempty,file"`

				// TLS certificate key file
				KeyFile string `mapstructure:"key-file" validate:"omitempty,file"`
			}
		}

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
				HttpOnly bool          `mapstructure:"http-only"`
				SameSite http.SameSite `mapstructure:"same-site"`
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
				HttpOnly bool              `mapstructure:"http-only"`
				SameSite csrf.SameSiteMode `mapstructure:"same-site"`
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

	// Cluster API options
	ClusterAPI struct {
		GinMode                 string `mapstructure:"gin-mode" validate:"omitempty,oneof=debug release"`
		BasePath                string `mapstructure:"base-path"`
		ClusterAgentDispatchUrl string `mapstructure:"cluster-agent-dispatch-url"`

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
				HttpOnly bool              `mapstructure:"http-only"`
				SameSite csrf.SameSiteMode `mapstructure:"same-site"`
			}
		}

		HTTP struct {
			Enabled bool
			Address string `validate:"omitempty,hostname"`
			Port    uint   `validate:"omitempty,port"`
		}

		HTTPS struct {
			Enabled bool
			Address string `validate:"omitempty,hostname"`
			Port    uint   `validate:"omitempty,port"`
			TLS     struct {
				// TLS certificate file
				CertFile string `mapstructure:"cert-file" validate:"omitempty,file"`

				// TLS certificate key file
				KeyFile string `mapstructure:"key-file" validate:"omitempty,file"`
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
	} `mapstructure:"cluster-api"`

	// Cluster Agent options
	ClusterAgent struct {
		Addr             string `validate:"omitempty,hostname_port"`
		ContainerLogsDir string `mapstructure:"container-logs-dir"`

		// TLS options
		TLS struct {
			// enable tls termination
			Enabled bool

			// TLS certificate file
			CertFile string `mapstructure:"cert-file" validate:"omitempty,file"`

			// TLS certificate key file
			KeyFile string `mapstructure:"key-file" validate:"omitempty,file"`
		}

		// logging options
		Logging struct {
			// enable logging
			Enabled bool

			// log level
			Level string `validate:"oneof=debug info warn error disabled"`

			// log format
			Format string `validate:"oneof=json pretty"`
		}

		// OTel options
		OTel struct {
			Enabled     bool
			Debug       bool
			Endpoint    string
			ServiceName string
		}
	} `mapstructure:"cluster-agent"`
}

// Validate config
func (cfg *Config) validate() error {
	return validator.New().Struct(cfg)
}

func DefaultConfig() *Config {
	cfg := &Config{}

	cfg.AllowedNamespaces = []string{}
	cfg.Dashboard.HTTP.Enabled = true
	cfg.Dashboard.HTTP.Address = ""
	cfg.Dashboard.HTTP.Port = 8080
	cfg.Dashboard.HTTPS.Enabled = false
	cfg.Dashboard.HTTPS.Address = ""
	cfg.Dashboard.HTTPS.Port = 8443
	cfg.Dashboard.HTTPS.TLS.CertFile = ""
	cfg.Dashboard.HTTPS.TLS.KeyFile = ""
	cfg.Dashboard.AuthMode = AuthModeAuto
	cfg.Dashboard.BasePath = "/"
	cfg.Dashboard.ClusterAPIEndpoint = ""
	cfg.Dashboard.Environment = EnvironmentDesktop
	cfg.Dashboard.GinMode = "release"
	cfg.Dashboard.CSRF.Enabled = true
	cfg.Dashboard.CSRF.Secret = ""
	cfg.Dashboard.CSRF.FieldName = "csrf_token"
	cfg.Dashboard.CSRF.Cookie.Name = "kubetail_dashboard_csrf"
	cfg.Dashboard.CSRF.Cookie.Path = "/"
	cfg.Dashboard.CSRF.Cookie.Domain = ""
	cfg.Dashboard.CSRF.Cookie.MaxAge = 60 * 60 * 12 // 12 hours
	cfg.Dashboard.CSRF.Cookie.Secure = false
	cfg.Dashboard.CSRF.Cookie.HttpOnly = true
	cfg.Dashboard.CSRF.Cookie.SameSite = csrf.SameSiteStrictMode
	cfg.Dashboard.Logging.Enabled = true
	cfg.Dashboard.Logging.Level = "info"
	cfg.Dashboard.Logging.Format = "json"
	cfg.Dashboard.Logging.AccessLog.Enabled = true
	cfg.Dashboard.Logging.AccessLog.HideHealthChecks = false
	cfg.Dashboard.Session.Secret = ""
	cfg.Dashboard.Session.Cookie.Name = "kubetail_dashboard_session"
	cfg.Dashboard.Session.Cookie.Path = "/"
	cfg.Dashboard.Session.Cookie.Domain = ""
	cfg.Dashboard.Session.Cookie.MaxAge = 86400 * 30 // 30 days
	cfg.Dashboard.Session.Cookie.Secure = false
	cfg.Dashboard.Session.Cookie.HttpOnly = true
	cfg.Dashboard.Session.Cookie.SameSite = http.SameSiteLaxMode
	cfg.Dashboard.UI.ClusterAPIEnabled = true

	cfg.ClusterAPI.HTTP.Enabled = true
	cfg.ClusterAPI.HTTP.Address = ""
	cfg.ClusterAPI.HTTP.Port = 8080
	cfg.ClusterAPI.HTTPS.Enabled = false
	cfg.ClusterAPI.HTTPS.Address = ""
	cfg.ClusterAPI.HTTPS.Port = 8443
	cfg.ClusterAPI.HTTPS.TLS.CertFile = ""
	cfg.ClusterAPI.HTTPS.TLS.KeyFile = ""
	cfg.ClusterAPI.BasePath = "/"
	cfg.ClusterAPI.ClusterAgentDispatchUrl = "kubernetes://kubetail-cluster-agent:50051"
	cfg.ClusterAPI.GinMode = "release"
	cfg.ClusterAPI.CSRF.Enabled = true
	cfg.ClusterAPI.CSRF.Secret = ""
	cfg.ClusterAPI.CSRF.FieldName = "csrf_token"
	cfg.ClusterAPI.CSRF.Cookie.Name = "kubetail_cluster_api_csrf"
	cfg.ClusterAPI.CSRF.Cookie.Path = "/"
	cfg.ClusterAPI.CSRF.Cookie.Domain = ""
	cfg.ClusterAPI.CSRF.Cookie.MaxAge = 60 * 60 * 12 // 12 hours
	cfg.ClusterAPI.CSRF.Cookie.Secure = false
	cfg.ClusterAPI.CSRF.Cookie.HttpOnly = true
	cfg.ClusterAPI.CSRF.Cookie.SameSite = csrf.SameSiteStrictMode
	cfg.ClusterAPI.Logging.Enabled = true
	cfg.ClusterAPI.Logging.Level = "info"
	cfg.ClusterAPI.Logging.Format = "json"
	cfg.ClusterAPI.Logging.AccessLog.Enabled = true
	cfg.ClusterAPI.Logging.AccessLog.HideHealthChecks = false

	cfg.ClusterAgent.Addr = ":50051"
	cfg.ClusterAgent.ContainerLogsDir = "/var/log/containers"
	cfg.ClusterAgent.Logging.Enabled = true
	cfg.ClusterAgent.Logging.Level = "info"
	cfg.ClusterAgent.Logging.Format = "json"
	cfg.ClusterAgent.OTel.Enabled = false
	cfg.ClusterAgent.OTel.Debug = false
	cfg.ClusterAgent.OTel.Endpoint = "localhost:4317"
	cfg.ClusterAgent.OTel.ServiceName = "kubetail"

	return cfg
}

// Custom unmarshaler for AuthMode
func authModeDecodeHook(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
	if f.Kind() != reflect.String {
		return data, nil
	}

	if t != reflect.TypeOf(AuthMode("")) {
		return data, nil
	}

	var authMode AuthMode
	authModeStr := strings.ToLower(data.(string))
	switch authModeStr {
	case "auto":
		authMode = AuthModeAuto
	case "token":
		authMode = AuthModeToken
	default:
		return nil, fmt.Errorf("invalid AuthMode value: %s", authModeStr)
	}

	return authMode, nil
}

// Custom unmarshaler for Environment
func environmentDecodeHook(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
	if f.Kind() != reflect.String {
		return data, nil
	}

	if t != reflect.TypeOf(Environment("")) {
		return data, nil
	}

	var env Environment
	envStr := strings.ToLower(data.(string))
	switch envStr {
	case "cluster":
		env = EnvironmentCluster
	case "desktop":
		env = EnvironmentDesktop
	default:
		return nil, fmt.Errorf("invalid Environment value: %s", envStr)
	}

	return env, nil
}

// Custom unmarshaler for http.SameSite
func httpSameSiteDecodeHook(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
	if f.Kind() != reflect.String {
		return data, nil
	}

	if t != reflect.TypeOf(http.SameSite(0)) {
		return data, nil
	}

	var sameSite http.SameSite
	sameSiteStr := strings.ToLower(data.(string))
	switch sameSiteStr {
	case "strict":
		sameSite = http.SameSiteStrictMode
	case "lax":
		sameSite = http.SameSiteLaxMode
	case "none":
		sameSite = http.SameSiteNoneMode
	default:
		return nil, fmt.Errorf("invalid http.SameSite value: %s", sameSiteStr)
	}

	return sameSite, nil
}

// Custom unmarshaler for csrf.SameSite
func csrfSameSiteDecodeHook(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
	if f.Kind() != reflect.String {
		return data, nil
	}

	if t != reflect.TypeOf(csrf.SameSiteStrictMode) {
		return data, nil
	}

	var sameSite csrf.SameSiteMode
	sameSiteStr := strings.ToLower(data.(string))
	switch sameSiteStr {
	case "strict":
		sameSite = csrf.SameSiteStrictMode
	case "lax":
		sameSite = csrf.SameSiteLaxMode
	case "none":
		sameSite = csrf.SameSiteNoneMode
	default:
		return nil, fmt.Errorf("invalid csrf.SameSite value: %s", sameSiteStr)
	}

	return sameSite, nil
}

func NewConfig(v *viper.Viper, f string) (*Config, error) {
	if f != "" {
		// read contents
		configBytes, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}

		// expand env vars
		configBytes = []byte(os.ExpandEnv(string(configBytes)))

		// load into viper
		v.SetConfigType(filepath.Ext(f)[1:])
		if err := v.ReadConfig(bytes.NewBuffer(configBytes)); err != nil {
			return nil, err
		}
	}

	cfg := DefaultConfig()

	// unmarshal
	hookFunc := mapstructure.ComposeDecodeHookFunc(
		authModeDecodeHook,
		environmentDecodeHook,
		httpSameSiteDecodeHook,
		csrfSameSiteDecodeHook,
	)
	if err := v.Unmarshal(cfg, viper.DecodeHook(hookFunc)); err != nil {
		return nil, err
	}

	// validate config
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Logging options
type LoggerOptions struct {
	Enabled bool
	Level   string
	Format  string
}

var configureLoggerOnce sync.Once

func ConfigureLogger(opts LoggerOptions) {
	// ensure this will only be called once
	configureLoggerOnce.Do(func() {
		if !opts.Enabled {
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
		level, err := zerolog.ParseLevel(opts.Level)
		if err != nil {
			panic(err)
		}
		zerolog.SetGlobalLevel(level)

		// configure output format
		if opts.Format == "pretty" {
			zlog.Logger = zlog.Logger.Output(zerolog.ConsoleWriter{
				Out:        os.Stdout,
				TimeFormat: time.RFC3339Nano,
			})
		}
	})
}
