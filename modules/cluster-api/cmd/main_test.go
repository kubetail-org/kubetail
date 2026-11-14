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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestCommand returns a command with the server's flags, parsed from args
// and pointed at the config file.
func newTestCommand(t *testing.T, configPath string, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	addFlags(cmd)
	require.NoError(t, cmd.ParseFlags(append([]string{"-c", configPath}, args...)))
	return cmd
}

// writeConfig writes a config with the certificate files the Cluster API
// requires, plus extra.
func writeConfig(t *testing.T, extra string) string {
	t.Helper()
	dir := t.TempDir()
	pem := filepath.Join(dir, "dummy.pem")
	require.NoError(t, os.WriteFile(pem, []byte("dummy"), 0o600))

	body := fmt.Sprintf(`tls:
  cert-file: %[1]q
  key-file: %[1]q
cluster-agent:
  tls:
    cert-file: %[1]q
    key-file: %[1]q
    ca-file: %[1]q
%[2]s`, pem, extra)

	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      string
		args        []string
		wantAddr    string
		wantGinMode string
	}{
		{"defaults", "", nil, ":8080", "release"},
		{"flags", "", []string{"--addr", ":9999", "--gin-mode", "debug"}, ":9999", "debug"},
		{"file over flag defaults", "addr: :7000\ngin-mode: debug\n", nil, ":7000", "debug"},
		{"flags over file", "addr: :7000\ngin-mode: debug\n", []string{"-a", ":9999", "--gin-mode", "release"}, ":9999", "release"},
		{"params over file and flags", "addr: :7000\n", []string{"-a", ":9999", "-p", "addr::6000"}, ":6000", "release"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadConfig(newTestCommand(t, writeConfig(t, tt.config), tt.args...))
			require.NoError(t, err)
			assert.Equal(t, tt.wantAddr, cfg.Addr)
			assert.Equal(t, tt.wantGinMode, cfg.GinMode)
		})
	}
}

func TestLoadConfig_ParamsOverrideConfigFile(t *testing.T) {
	cmd := newTestCommand(t, writeConfig(t, "addr: :8888\n"), "-p", "addr::9999", "-p", "logging.level:debug")
	cfg, err := loadConfig(cmd)
	require.NoError(t, err)
	assert.Equal(t, ":9999", cfg.Addr)
	assert.Equal(t, "debug", cfg.Logging.Level)
}
