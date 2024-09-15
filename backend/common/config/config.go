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
	AuthModeCluster AuthMode = "cluster"
	AuthModeToken   AuthMode = "token"
	AuthModeLocal   AuthMode = "local"
)

// Application configuration
type Config struct {
	AuthMode          AuthMode `mapstructure:"auth-mode"`
	AllowedNamespaces []string `mapstructure:"allowed-namespaces"`
	KubeConfig        string   `mapstructure:"kube-config"`

	// server options
	Server struct {
		Addr     string `validate:"omitempty,hostname_port"`
		BasePath string `mapstructure:"base-path"`
		GinMode  string `validate:"omitempty,oneof=debug release"`

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

	// agent options
	Agent struct {
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
	}
}

// Validate config
func (cfg *Config) validate() error {
	return validator.New().Struct(cfg)
}

func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()

	cfg := &Config{}

	cfg.AuthMode = AuthModeToken
	cfg.AllowedNamespaces = []string{}
	cfg.KubeConfig = filepath.Join(home, ".kube", "config")

	cfg.Server.Addr = ":4000"
	cfg.Server.BasePath = "/"
	cfg.Server.GinMode = "release"
	cfg.Server.Session.Secret = ""
	cfg.Server.Session.Cookie.Name = "session"
	cfg.Server.Session.Cookie.Path = "/"
	cfg.Server.Session.Cookie.Domain = ""
	cfg.Server.Session.Cookie.MaxAge = 86400 * 30 // 30 days
	cfg.Server.Session.Cookie.Secure = false
	cfg.Server.Session.Cookie.HttpOnly = true
	cfg.Server.Session.Cookie.SameSite = http.SameSiteLaxMode
	cfg.Server.CSRF.Enabled = true
	cfg.Server.CSRF.Secret = ""
	cfg.Server.CSRF.FieldName = "csrf_token"
	cfg.Server.CSRF.Cookie.Name = "csrf"
	cfg.Server.CSRF.Cookie.Path = "/"
	cfg.Server.CSRF.Cookie.Domain = ""
	cfg.Server.CSRF.Cookie.MaxAge = 60 * 60 * 12 // 12 hours
	cfg.Server.CSRF.Cookie.Secure = false
	cfg.Server.CSRF.Cookie.HttpOnly = true
	cfg.Server.CSRF.Cookie.SameSite = csrf.SameSiteStrictMode
	cfg.Server.Logging.Enabled = true
	cfg.Server.Logging.Level = "info"
	cfg.Server.Logging.Format = "json"
	cfg.Server.Logging.AccessLog.Enabled = true
	cfg.Server.Logging.AccessLog.HideHealthChecks = false

	cfg.Agent.Addr = ":50051"
	cfg.Agent.ContainerLogsDir = "/var/log/containers"
	cfg.Agent.Logging.Enabled = true
	cfg.Agent.Logging.Level = "info"
	cfg.Agent.Logging.Format = "json"

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
	case "cluster":
		authMode = AuthModeCluster
	case "token":
		authMode = AuthModeToken
	case "local":
		authMode = AuthModeLocal
	default:
		return nil, fmt.Errorf("invalid AuthMode value: %s", authModeStr)
	}

	return authMode, nil
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

func ConfigureLogger(opts LoggerOptions) {
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
}
