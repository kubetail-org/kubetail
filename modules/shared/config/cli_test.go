package config

import (
	"os"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var validCLIConfig = `
general:
  kubeconfig: "/path/to/kubeconfig"
commands:
  logs:
    kube-context: "my-context"
    head: 20
    tail: 30
  serve:
    port: 8080
    host: "0.0.0.0"
    skip-open: true
`

var invalidCLIConfig = `
general:
  kubeconfig: "/path/to/kubeconfig"
commands:
  logs:
    kube-context: "my-context"
    head: "invalid"  # This should be an int
    tail: 30
`

func TestNewCLIConfigFromFile_ValidConfig(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "cli-config-test-*.yaml")
	assert.Nil(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(validCLIConfig)
	assert.Nil(t, err)
	tmpFile.Close()

	cfg, err := NewCLIConfigFromFile(tmpFile.Name())
	require.Nil(t, err)
	assert.Equal(t, "/path/to/kubeconfig", cfg.General.KubeconfigPath)
	assert.Equal(t, "my-context", cfg.Commands.Logs.KubeContext)
	assert.Equal(t, int64(20), cfg.Commands.Logs.Head)
	assert.Equal(t, int64(30), cfg.Commands.Logs.Tail)
	assert.Equal(t, 8080, cfg.Commands.Serve.Port)
	assert.Equal(t, "0.0.0.0", cfg.Commands.Serve.Host)
	assert.Equal(t, true, cfg.Commands.Serve.SkipOpen)
}

func TestNewCLIConfigFromFile_InvalidYAML(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "cli-config-test-*.yaml")
	assert.Nil(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(invalidCLIConfig)
	assert.Nil(t, err)
	tmpFile.Close()

	// Invalid YAML that can't be unmarshaled should return an error
	_, err = NewCLIConfigFromFile(tmpFile.Name())
	require.NotNil(t, err)
}

func TestNewCLIConfigFromFile_NonExistentFile(t *testing.T) {
	// Non-existent file should return default config with a warning
	cfg, err := NewCLIConfigFromFile("/non/existent/file.yaml")
	require.Nil(t, err)
	// Should get default values
	assert.Equal(t, int64(10), cfg.Commands.Logs.Head)
	assert.Equal(t, int64(10), cfg.Commands.Logs.Tail)
}

func TestNewCLIConfigFromFile_NoExtension(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "cli-config-test")
	assert.Nil(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(validCLIConfig)
	assert.Nil(t, err)
	tmpFile.Close()

	// File without extension should return default config with a warning
	cfg, err := NewCLIConfigFromFile(tmpFile.Name())
	require.Nil(t, err)
	// Should get default values
	assert.Equal(t, int64(10), cfg.Commands.Logs.Head)
	assert.Equal(t, int64(10), cfg.Commands.Logs.Tail)
}

func TestNewCLIConfigFromViper_ValidConfig(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "cli-config-test-*.yaml")
	assert.Nil(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(validCLIConfig)
	assert.Nil(t, err)
	tmpFile.Close()

	v := viper.New()
	cfg, err := NewCLIConfigFromViper(v, tmpFile.Name())
	require.Nil(t, err)
	assert.Equal(t, "/path/to/kubeconfig", cfg.General.KubeconfigPath)
	assert.Equal(t, "my-context", cfg.Commands.Logs.KubeContext)
}

func TestDefaultCLIConfig(t *testing.T) {
	cfg := DefaultCLIConfig()
	assert.NotNil(t, cfg)
	assert.Equal(t, "", cfg.Commands.Logs.KubeContext)
	assert.Equal(t, int64(10), cfg.Commands.Logs.Head)
	assert.Equal(t, int64(10), cfg.Commands.Logs.Tail)
	assert.Equal(t, 7500, cfg.Commands.Serve.Port)
	assert.Equal(t, "localhost", cfg.Commands.Serve.Host)
	assert.Equal(t, false, cfg.Commands.Serve.SkipOpen)
	assert.Equal(t, "", cfg.General.KubeconfigPath)
}
