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
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/mitchellh/mapstructure"
	"github.com/spf13/viper"

	sharedcfg "github.com/kubetail-org/kubetail/modules/shared/config"
)

// AuthMode represents the authentication mode for the dashboard.
type AuthMode string

const (
	AuthModeAuto  AuthMode = "auto"
	AuthModeToken AuthMode = "token"
)

// Represents the Dashboard configuration
type Config struct {
	// Shared/common options (currently used by multiple components)
	AllowedNamespaces []string `mapstructure:"allowed-namespaces"`
	KubeconfigPath    string   `mapstructure:"kubeconfig"`

	Addr               string   `mapstructure:"addr" validate:"omitempty,hostname_port"`
	AuthMode           AuthMode `mapstructure:"auth-mode"`
	BasePath           string   `mapstructure:"base-path"`
	ClusterAPIEndpoint string   `mapstructure:"cluster-api-endpoint"`
	GinMode            string   `mapstructure:"gin-mode" validate:"omitempty,oneof=debug release"`
	Environment        sharedcfg.Environment

	// csrf options
	CSRF struct {
		Enabled bool
	}

	// logging options
	Logging struct {
		Enabled bool
		Level   string `validate:"oneof=debug info warn error disabled"`
		Format  string `validate:"oneof=json pretty"`

		// access-log options
		AccessLog struct {
			Enabled          bool
			HideHealthChecks bool `mapstructure:"hide-health-checks"`
		} `mapstructure:"access-log"`
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

	// TLS options
	TLS struct {
		Enabled  bool
		CertFile string `mapstructure:"cert-file" validate:"omitempty,file"`
		KeyFile  string `mapstructure:"key-file" validate:"omitempty,file"`
	}

	// UI options
	UI struct {
		ClusterAPIEnabled bool `mapstructure:"cluster-api-enabled"`
	}
}

func (cfg *Config) validate() error {
	return validator.New().Struct(cfg)
}

func DefaultConfig() *Config {
	cfg := &Config{}

	cfg.AllowedNamespaces = []string{}
	cfg.KubeconfigPath = ""

	cfg.Addr = ":8080"
	cfg.AuthMode = AuthModeAuto
	cfg.BasePath = "/"
	cfg.ClusterAPIEndpoint = ""
	cfg.Environment = sharedcfg.EnvironmentCluster
	cfg.GinMode = "release"
	cfg.CSRF.Enabled = true
	cfg.Logging.Enabled = true
	cfg.Logging.Level = "info"
	cfg.Logging.Format = "json"
	cfg.Logging.AccessLog.Enabled = true
	cfg.Logging.AccessLog.HideHealthChecks = false
	cfg.Session.Secret = ""
	cfg.Session.Cookie.Name = "kubetail_dashboard_session"
	cfg.Session.Cookie.Path = "/"
	cfg.Session.Cookie.Domain = ""
	cfg.Session.Cookie.MaxAge = 86400 * 30
	cfg.Session.Cookie.Secure = false
	cfg.Session.Cookie.HttpOnly = true
	cfg.Session.Cookie.SameSite = http.SameSiteLaxMode
	cfg.TLS.Enabled = false
	cfg.TLS.CertFile = ""
	cfg.TLS.KeyFile = ""
	cfg.UI.ClusterAPIEnabled = true

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

	authModeStr := strings.ToLower(data.(string))
	switch authModeStr {
	case "auto":
		return AuthModeAuto, nil
	case "token":
		return AuthModeToken, nil
	default:
		return nil, fmt.Errorf("invalid AuthMode value: %s", authModeStr)
	}
}

// Custom unmarshaler for Environment
func environmentDecodeHook(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
	if f.Kind() != reflect.String {
		return data, nil
	}
	if t != reflect.TypeOf(sharedcfg.Environment("")) {
		return data, nil
	}

	envStr := strings.ToLower(data.(string))
	switch envStr {
	case "cluster":
		return sharedcfg.EnvironmentCluster, nil
	case "desktop":
		return sharedcfg.EnvironmentDesktop, nil
	default:
		return nil, fmt.Errorf("invalid Environment value: %s", envStr)
	}
}

// Custom unmarshaler for http.SameSite
func httpSameSiteDecodeHook(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
	if f.Kind() != reflect.String {
		return data, nil
	}
	if t != reflect.TypeOf(http.SameSite(0)) {
		return data, nil
	}

	sameSiteStr := strings.ToLower(data.(string))
	switch sameSiteStr {
	case "strict":
		return http.SameSiteStrictMode, nil
	case "lax":
		return http.SameSiteLaxMode, nil
	case "none":
		return http.SameSiteNoneMode, nil
	default:
		return nil, fmt.Errorf("invalid http.SameSite value: %s", sameSiteStr)
	}
}

func NewConfig(v *viper.Viper, configPath string) (*Config, error) {
	if v == nil {
		v = viper.New()
	}

	if configPath != "" {
		// read contents
		configBytes, err := os.ReadFile(configPath)
		if err != nil {
			return nil, err
		}

		// expand env vars
		configBytes = []byte(os.ExpandEnv(string(configBytes)))

		// load into viper
		v.SetConfigType(filepath.Ext(configPath)[1:])
		if err := v.ReadConfig(bytes.NewBuffer(configBytes)); err != nil {
			return nil, err
		}
	}

	cfg := DefaultConfig()

	hookFunc := mapstructure.ComposeDecodeHookFunc(
		authModeDecodeHook,
		environmentDecodeHook,
		httpSameSiteDecodeHook,
	)
	decodeOpt := viper.DecodeHook(hookFunc)

	// Unmarshal
	if err := v.Unmarshal(cfg, decodeOpt); err != nil {
		return nil, err
	}

	// Validate config
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}
