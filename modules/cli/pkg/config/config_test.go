// Copyright 2024 The Kubetail Authors
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
	"os"
	"path/filepath"
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
    columns: ["timestamp", "dot", "pod"]
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

func TestDefaultCLIConfig(t *testing.T) {
	cfg := DefaultCLIConfig()
	assert.NotNil(t, cfg)
	assert.Equal(t, "", cfg.Commands.Logs.KubeContext)
	assert.Equal(t, int64(10), cfg.Commands.Logs.Head)
	assert.Equal(t, int64(10), cfg.Commands.Logs.Tail)
	assert.Equal(t, []string{"timestamp", "dot"}, cfg.Commands.Logs.Columns)
	assert.Equal(t, 7500, cfg.Commands.Serve.Port)
	assert.Equal(t, "localhost", cfg.Commands.Serve.Host)
	assert.Equal(t, false, cfg.Commands.Serve.SkipOpen)
	assert.Equal(t, "", cfg.General.KubeconfigPath)
}

func TestNewCLIConfigSuccess(t *testing.T) {
	t.Run("properly formatted file", func(t *testing.T) {
		tmpDir := t.TempDir()

		filePath := filepath.Join(tmpDir, "cli.yaml")
		err := os.WriteFile(filePath, []byte(validCLIConfig), 0644)
		require.NoError(t, err)

		cfg, err := NewConfig(filePath, nil)
		require.Nil(t, err)
		assert.Equal(t, "/path/to/kubeconfig", cfg.General.KubeconfigPath)
		assert.Equal(t, "my-context", cfg.Commands.Logs.KubeContext)
		assert.Equal(t, int64(20), cfg.Commands.Logs.Head)
		assert.Equal(t, int64(30), cfg.Commands.Logs.Tail)
		assert.Equal(t, []string{"timestamp", "dot", "pod"}, cfg.Commands.Logs.Columns)
		assert.Equal(t, 8080, cfg.Commands.Serve.Port)
		assert.Equal(t, "0.0.0.0", cfg.Commands.Serve.Host)
		assert.Equal(t, true, cfg.Commands.Serve.SkipOpen)
	})

	t.Run("with viper override", func(t *testing.T) {
		tmpDir := t.TempDir()

		filePath := filepath.Join(tmpDir, "cli.yaml")
		err := os.WriteFile(filePath, []byte(validCLIConfig), 0644)
		require.NoError(t, err)

		// Override the value of `head` using a viper instance
		var headVal int64 = 100

		v := viper.New()
		v.Set("commands.logs.head", headVal)

		cfg, err := NewConfig(filePath, v)
		require.Nil(t, err)
		assert.Equal(t, headVal, cfg.Commands.Logs.Head)
	})
}

func TestNewCLIConfigError(t *testing.T) {
	t.Run("improperly formatted file", func(t *testing.T) {
		tmpDir := t.TempDir()

		filePath := filepath.Join(tmpDir, "cli.yaml")
		err := os.WriteFile(filePath, []byte(invalidCLIConfig), 0644)
		require.NoError(t, err)

		cfg, err := NewConfig(filePath, nil)
		require.NotNil(t, err)
		require.Nil(t, cfg)
	})

	t.Run("missing file", func(t *testing.T) {
		cfg, err := NewConfig("/does/not/exist.yaml", nil)
		require.NotNil(t, err)
		require.Nil(t, cfg)
	})

	t.Run("missing extension", func(t *testing.T) {
		tmpDir := t.TempDir()

		filePath := filepath.Join(tmpDir, "cli")
		err := os.WriteFile(filePath, []byte(validCLIConfig), 0644)
		require.NoError(t, err)

		cfg, err := NewConfig(filePath, nil)
		require.NotNil(t, err)
		require.Nil(t, cfg)
	})
}
