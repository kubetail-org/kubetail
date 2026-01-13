package config

import (
	"bytes"
	"fmt"
	"os"
	"strconv"

	"path/filepath"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

type CLI struct {
	Config string `validate:"omitempty,file"`
}

// Application configuration
type CLIConfig struct {
	// Global settings
	General struct {
		KubeconfigPath string `mapstructure:"kubeconfig"`
	} `mapstructure:"general"`

	// Settings specific to sub-commands
	Commands struct {
		// Default behavior for the 'logs' command
		Logs struct {
			KubeContext string `mapstructure:"kube-context"`
			Head        int64  `mapstructure:"head"`
			Tail        int64  `mapstructure:"tail"`
		} `mapstructure:"logs"`

		// Default behavior for the 'serve' command
		Serve struct {
			Port     int    `mapstructure:"port"`
			Host     string `mapstructure:"host"`
			SkipOpen bool   `mapstructure:"skip-open"`
		} `mapstructure:"serve"`
	} `mapstructure:"commands"`
}

// Validate config
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

func DefaultConfigPath(format string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}

	return filepath.Join(home, ".kubetail", fmt.Sprintf("config.%s", format)), nil
}

func NewCLIConfigFromViper(v *viper.Viper, configPath string) (*CLIConfig, error) {
	if configPath == "" {
		if f, err := DefaultConfigPath("yaml"); err != nil {
			return nil, err
		} else {
			configPath = f
		}
	}

	// read contents
	configBytes, err := os.ReadFile(configPath)
	if err == nil {
		// expand env vars
		configBytes = []byte(os.ExpandEnv(string(configBytes)))

		// check extension
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

type OptionalInt64 struct {
	Value           int64
	IsValueProvided bool
}

func (f *OptionalInt64) String() string {
	return strconv.FormatInt(f.Value, 10)
}

func (f *OptionalInt64) Set(s string) error {
	if s == "N" {
		f.IsValueProvided = false
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	f.Value = v
	f.IsValueProvided = true
	return nil
}

func (f *OptionalInt64) Type() string { return "int64" }
