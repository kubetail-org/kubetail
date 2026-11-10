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
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	release "helm.sh/helm/v4/pkg/release/v1"
)

// fakeHelmClient stands in for helm.Client in the cluster command tests. It
// records the release each call names and returns rel and err.
type fakeHelmClient struct {
	rel       *release.Release
	releases  []*release.Release
	err       error
	namespace string
	name      string
	calls     int
}

func (f *fakeHelmClient) record(namespace, name string) (*release.Release, error) {
	f.calls++
	f.namespace, f.name = namespace, name
	return f.rel, f.err
}

func (f *fakeHelmClient) InstallLatest(namespace, name string) (*release.Release, error) {
	return f.record(namespace, name)
}

func (f *fakeHelmClient) UpgradeRelease(namespace, name string) (*release.Release, error) {
	return f.record(namespace, name)
}

func (f *fakeHelmClient) UninstallRelease(namespace, name string) (*release.Release, error) {
	return f.record(namespace, name)
}

func (f *fakeHelmClient) ListReleases() ([]*release.Release, error) {
	f.calls++
	return f.releases, f.err
}

func (f *fakeHelmClient) AddRepo() error {
	f.calls++
	return f.err
}

func (f *fakeHelmClient) RemoveRepo() error {
	f.calls++
	return f.err
}

func (f *fakeHelmClient) UpdateRepo() error {
	f.calls++
	return f.err
}

func TestClusterCmd_RegistersSubcommands(t *testing.T) {
	for _, name := range []string{"install", "list", "repo", "uninstall", "upgrade"} {
		sub, _, err := clusterCmd.Find([]string{name})
		require.NoError(t, err, name)
		assert.Equal(t, name, sub.Name())
		assert.Equal(t, clusterCmd, sub.Parent())
	}
	assert.Equal(t, rootCmd, clusterCmd.Parent())
}

func TestClusterCmd_KubeContextFlag(t *testing.T) {
	for _, c := range []*cobra.Command{clusterInstallCmd, clusterListCmd, clusterUninstallCmd, clusterUpgradeCmd} {
		assert.NotNil(t, c.Flags().Lookup(KubeContextFlag), c.Name())
	}
}

func TestNewHelmClient(t *testing.T) {
	cmd := &cobra.Command{}
	addRootCmdFlagsTo(cmd.Flags())
	cmd.Flags().String(KubeContextFlag, "", "")
	require.NoError(t, cmd.Flags().Set(KubeconfigFlag, "/tmp/kubeconfig"))
	require.NoError(t, cmd.Flags().Set(KubeContextFlag, "my-context"))

	client := newHelmClient(cmd)

	assert.Equal(t, "/tmp/kubeconfig", client.EnvSettings.KubeConfig)
	assert.Equal(t, "my-context", client.EnvSettings.KubeContext)
}
