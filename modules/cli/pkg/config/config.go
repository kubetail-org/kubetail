package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

type CLI struct {
	Config string `validate:"omitempty,file"`
}

type CLIConfig struct {
	// Global settings
	General struct {
		KubeconfigPath string `mapstructure:"kubeconfig"`
	} `mapstructure:"general"`

	Commands struct {
		Logs struct {
			KubeContext string `mapstructure:"kube-context"`
			Head        int64  `mapstructure:"head"`
			Tail        int64  `mapstructure:"tail"`
		} `mapstructure:"logs"`

		Serve struct {
			Port     int    `mapstructure:"port"`
			Host     string `mapstructure:"host"`
			SkipOpen bool   `mapstructure:"skip-open"`
		} `mapstructure:"serve"`
	} `mapstructure:"commands"`
}

func (cfg *CLIConfig) validate() error {
	return validator.New().Struct(cfg)
}

func DefaultCLIConfig() *CLIConfig {
	cfg := &CLIConfig{}

	cfg.Commands.Logs.KubeContext = ""
	cfg.Commands.Logs.Head = 10
	cfg.Commands.Logs.Tail = 10

	cfg.Commands.Serve.Port = 7500
	cfg.Commands.Serve.Host = "localhost"
	cfg.Commands.Serve.SkipOpen = false

	cfg.General.KubeconfigPath = ""

	return cfg
}

func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}

	return filepath.Join(home, ".kubetail", "config.yaml"), nil
}

func NewCLIConfigFromFile(configPath string) (*CLIConfig, error) {
	if configPath == "" {
		f, err := DefaultConfigPath()
		if err != nil {
			return nil, err
		}
		configPath = f
	}

	v := viper.New()

	configBytes, err := os.ReadFile(configPath)
	if err == nil {
		configBytes = []byte(os.ExpandEnv(string(configBytes)))

		if len(filepath.Ext(configPath)) <= 1 {
			return nil, fmt.Errorf("file %q must have a valid extension (e.g., .yaml, .json)", configPath)
		}

		v.SetConfigType(filepath.Ext(configPath)[1:])
		if err := v.ReadConfig(bytes.NewBuffer(configBytes)); err != nil {
			return nil, err
		}
	}

	cfg := DefaultCLIConfig()

	// unmarshal
	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}

	// validate config
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func NewCLIConfigFromViper(v *viper.Viper, configPath string) (*CLIConfig, error) {
	if v == nil {
		v = viper.New()
	}
	if configPath == "" {
		f, err := DefaultConfigPath()
		if err != nil {
			return nil, err
		}
		configPath = f
	}

	configBytes, err := os.ReadFile(configPath)
	if err == nil {
		configBytes = []byte(os.ExpandEnv(string(configBytes)))

		if len(filepath.Ext(configPath)) <= 1 {
			return nil, fmt.Errorf("file %q must have a valid extension (e.g., .yaml, .json)", configPath)
		}

		// load into viper
		v.SetConfigType(filepath.Ext(configPath)[1:])
		if err := v.ReadConfig(bytes.NewBuffer(configBytes)); err != nil {
			return nil, err
		}
	}

	cfg := DefaultCLIConfig()

	// unmarshal
	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}

	// validate config
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}
