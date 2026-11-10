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

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConfig_ParamsOverrideConfigFile(t *testing.T) {
	dir := t.TempDir()
	dummy := filepath.Join(dir, "dummy.pem")
	require.NoError(t, os.WriteFile(dummy, []byte("dummy"), 0o600))

	body := "addr: :8888\n" +
		"tls:\n  cert-file: " + dummy + "\n  key-file: " + dummy + "\n" +
		"cluster-agent:\n  tls:\n    cert-file: " + dummy + "\n    key-file: " + dummy + "\n    ca-file: " + dummy + "\n"
	configPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(body), 0o600))

	cfg, err := newConfig(configPath, viper.New(), []string{"addr::9999", "logging.level:debug"})
	require.NoError(t, err)
	assert.Equal(t, ":9999", cfg.Addr)
	assert.Equal(t, "debug", cfg.Logging.Level)
}
