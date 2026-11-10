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
)

func TestNewClusterHelmClient(t *testing.T) {
	writeConfig := func(t *testing.T, kubeconfig string) string {
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte("general:\n  kubeconfig: "+kubeconfig+"\n"), 0644))
		return path
	}

	tests := []struct {
		name           string
		configFile     string
		flagKubeconfig string
		kubeContext    string
		want           string
	}{
		{"no flag and no file", "", "", "", ""},
		{"config file", "/from/file", "", "", "/from/file"},
		{"flag overrides config file", "/from/file", "/from/flag", "", "/from/flag"},
		{"kube context", "", "", "my-context", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())

			cmd := &cobra.Command{}
			addRootCmdFlagsTo(cmd.Flags())
			cmd.Flags().String(KubeContextFlag, "", "")

			if tt.configFile != "" {
				require.NoError(t, cmd.Flags().Set("config", writeConfig(t, tt.configFile)))
			}
			if tt.flagKubeconfig != "" {
				require.NoError(t, cmd.Flags().Set(KubeconfigFlag, tt.flagKubeconfig))
			}
			if tt.kubeContext != "" {
				require.NoError(t, cmd.Flags().Set(KubeContextFlag, tt.kubeContext))
			}

			client, err := newClusterHelmClient(cmd)
			require.NoError(t, err)
			assert.Equal(t, tt.want, client.KubeConfig)
			assert.Equal(t, tt.kubeContext, client.KubeContext)
		})
	}
}

func TestNewClusterHelmClient_InvalidConfigFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("general: ["), 0644))

	cmd := &cobra.Command{}
	addRootCmdFlagsTo(cmd.Flags())
	cmd.Flags().String(KubeContextFlag, "", "")
	require.NoError(t, cmd.Flags().Set("config", path))

	_, err := newClusterHelmClient(cmd)
	assert.Error(t, err)
}
