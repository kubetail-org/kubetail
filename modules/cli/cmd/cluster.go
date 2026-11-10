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
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/kubetail-org/kubetail/modules/shared/helm"

	"github.com/kubetail-org/kubetail/modules/cli/pkg/config"
)

const clusterHelp = `
Subcommands for installing Kubetail cluster resources using Helm.

These commands use the Helm library under-the-hood so you don't need
to have Helm installed in order to use them. If you do have Helm
installed, you can switch between them or Helm itself to manage your
cluster resources seamlessly.
`

// clusterCmd represents the ext command
var clusterCmd = &cobra.Command{
	Use:   "cluster",
	Short: "Manage cluster resources",
	Long:  clusterHelp,
}

func init() {
	rootCmd.AddCommand(clusterCmd)
}

// newClusterHelmClient builds the Helm client for a `cluster` subcommand.
// The kubeconfig comes from the flag, then the config file's general.kubeconfig.
func newClusterHelmClient(cmd *cobra.Command) (*helm.Client, error) {
	flags := cmd.Flags()

	configPath, _ := flags.GetString("config")
	kubeContext, _ := flags.GetString(KubeContextFlag)

	v := viper.New()
	v.BindPFlag("general.kubeconfig", flags.Lookup(KubeconfigFlag))

	cfg, err := config.NewConfig(configPath, v)
	if err != nil {
		return nil, err
	}

	return helm.NewClient(helm.WithKubeconfigPath(cfg.General.KubeconfigPath), helm.WithKubeContext(kubeContext)), nil
}
