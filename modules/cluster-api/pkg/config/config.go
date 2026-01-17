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
	"os"
	"path/filepath"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

// Represents the Cluster API configuration
type Config struct {
	// Shared/common options (currently used by multiple components)
	AllowedNamespaces []string `mapstructure:"allowed-namespaces"`

	Addr     string `mapstructure:"addr" validate:"omitempty,hostname_port"`
	GinMode  string `mapstructure:"gin-mode" validate:"omitempty,oneof=debug release"`
	BasePath string `mapstructure:"base-path"`

	CSRF struct {
		Enabled bool
	}

	ClusterAgent struct {
		DispatchUrl string `mapstructure:"dispatch-url"`
		TLS         struct {
			Enabled    bool
			CertFile   string `mapstructure:"cert-file" validate:"omitempty,file"`
			KeyFile    string `mapstructure:"key-file" validate:"omitempty,file"`
			CAFile     string `mapstructure:"ca-file" validate:"omitempty,file"`
			ServerName string `mapstructure:"server-name"`
		}
	} `mapstructure:"cluster-agent"`

	Logging struct {
		Enabled bool
		Level   string `validate:"oneof=debug info warn error disabled"`
		Format  string `validate:"oneof=json pretty"`

		AccessLog struct {
			Enabled          bool
			HideHealthChecks bool `mapstructure:"hide-health-checks"`
		} `mapstructure:"access-log"`
	}

	TLS struct {
		Enabled  bool
		CertFile string `mapstructure:"cert-file" validate:"omitempty,file"`
		KeyFile  string `mapstructure:"key-file" validate:"omitempty,file"`
	}
}

func (cfg *Config) validate() error {
	return validator.New().Struct(cfg)
}

func DefaultConfig() *Config {
	cfg := &Config{}

	cfg.AllowedNamespaces = []string{}

	cfg.Addr = ":8080"
	cfg.BasePath = "/"
	cfg.GinMode = "release"

	cfg.CSRF.Enabled = true

	cfg.ClusterAgent.DispatchUrl = "kubernetes://kubetail-cluster-agent:50051"
	cfg.ClusterAgent.TLS.Enabled = false
	cfg.ClusterAgent.TLS.CertFile = ""
	cfg.ClusterAgent.TLS.KeyFile = ""
	cfg.ClusterAgent.TLS.CAFile = ""
	cfg.ClusterAgent.TLS.ServerName = ""

	cfg.Logging.Enabled = true
	cfg.Logging.Level = "info"
	cfg.Logging.Format = "json"
	cfg.Logging.AccessLog.Enabled = true
	cfg.Logging.AccessLog.HideHealthChecks = false

	cfg.TLS.Enabled = false
	cfg.TLS.CertFile = ""
	cfg.TLS.KeyFile = ""

	return cfg
}

func NewConfig(configPath string, v *viper.Viper) (*Config, error) {
	if v == nil {
		v = viper.New()
	}

	if configPath != "" {
		// Read contents
		configBytes, err := os.ReadFile(configPath)
		if err != nil {
			return nil, err
		}

		// Expand env vars
		configBytes = []byte(os.ExpandEnv(string(configBytes)))

		// Load into viper
		v.SetConfigType(filepath.Ext(configPath)[1:])
		if err := v.ReadConfig(bytes.NewBuffer(configBytes)); err != nil {
			return nil, err
		}
	}

	cfg := DefaultConfig()

	// Unmarshal common/root options (e.g., allowed-namespaces)
	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}

	// Validate config
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}
