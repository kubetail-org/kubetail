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

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/modules/cli/assets"
)

// setHome points the user's home directory at dir on every OS.
func setHome(t *testing.T, dir string) {
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

func TestRunConfigInit_Formats(t *testing.T) {
	tests := []struct {
		format string
		asset  string
	}{
		{"", "config.yaml"},
		{"yml", "config.yaml"},
		{"yaml", "config.yaml"},
		{"json", "config.json"},
		{"toml", "config.toml"},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "nested", "config")

			path, err := runConfigInit(target, tt.format, false)
			require.NoError(t, err)
			assert.Equal(t, target, path)

			want, err := assets.FS.ReadFile(tt.asset)
			require.NoError(t, err)
			got, err := os.ReadFile(target)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestRunConfigInit_DefaultPath(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	path, err := runConfigInit("", "json", false)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".kubetail", "config.json"), path)
	assert.FileExists(t, path)
}

func TestRunConfigInit_NoHome(t *testing.T) {
	setHome(t, "")

	_, err := runConfigInit("", "yaml", false)

	assert.ErrorContains(t, err, "unable to determine config path")
}

func TestRunConfigInit_UnsupportedFormat(t *testing.T) {
	target := filepath.Join(t.TempDir(), "config.ini")

	_, err := runConfigInit(target, "ini", false)

	assert.EqualError(t, err, "format 'ini' is not supported")
	assert.NoFileExists(t, target)
}

func TestRunConfigInit_RefusesToOverwrite(t *testing.T) {
	target := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(target, []byte("existing"), 0644))

	_, err := runConfigInit(target, "yaml", false)

	assert.ErrorContains(t, err, "configuration file already exists")
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "existing", string(got))
}

func TestRunConfigInit_ForceOverwrites(t *testing.T) {
	target := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(target, []byte("existing"), 0644))

	_, err := runConfigInit(target, "yaml", true)
	require.NoError(t, err)

	want, err := assets.FS.ReadFile("config.yaml")
	require.NoError(t, err)
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestRunConfigInit_DirectoryError(t *testing.T) {
	// A file where the parent directory should be makes MkdirAll fail.
	parent := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(parent, nil, 0644))

	_, err := runConfigInit(filepath.Join(parent, "config.yaml"), "yaml", false)

	assert.ErrorContains(t, err, "failed to create configuration directory")
}

func TestRunConfigInit_WriteError(t *testing.T) {
	// A directory at the target path makes WriteFile fail.
	target := t.TempDir()

	_, err := runConfigInit(target, "yaml", true)

	assert.ErrorContains(t, err, "failed to write configuration file")
}

func TestConfigInitCmd_Run(t *testing.T) {
	target := filepath.Join(t.TempDir(), "config.toml")

	cmd := &cobra.Command{}
	addConfigInitCmdFlags(cmd)
	require.NoError(t, cmd.Flags().Set("path", target))
	require.NoError(t, cmd.Flags().Set("format", "toml"))

	configInitCmd.Run(cmd, nil)

	assert.FileExists(t, target)
}
