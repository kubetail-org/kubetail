// Copyright 2024-2025 Andres Morey
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
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kubetail-org/kubetail/modules/shared/helm"

	"github.com/kubetail-org/kubetail/modules/cli/internal/cli"
)

const clusterUpgradeHelp = `
This command upgrades an existing release using the latest chart available locally.
`

// clusterUpgradeCmd represents the `cluster upgrade` command
var clusterUpgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade an existing release",
	Long:  clusterUpgradeHelp,
	Run: func(cmd *cobra.Command, args []string) {
		// Get flags
		kubeconfigPath, _ := cmd.Flags().GetString(KubeconfigFlag)
		kubeContext, _ := cmd.Flags().GetString(KubeContextFlag)
		//name, _ := cmd.Flags().GetString("name")
		//namespace, _ := cmd.Flags().GetString("namespace")
		name := helm.DefaultReleaseName
		namespace := helm.DefaultNamespace

		// Init client
		client := helm.NewClient(helm.WithKubeconfigPath(kubeconfigPath), helm.WithKubeContext(kubeContext))

		// Upgrade
		release, err := client.UpgradeRelease(namespace, name)
		cli.ExitOnError(err)

		fmt.Printf("Successfully upgraded release '%s' in namespace '%s' (revision: %d)\n", release.Name, release.Namespace, release.Version)
	},
}

func init() {
	clusterCmd.AddCommand(clusterUpgradeCmd)

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// serveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	flagset := clusterUpgradeCmd.Flags()
	flagset.SortFlags = false
	flagset.String(KubeContextFlag, "", "Name of the kubeconfig context to use")
	//flagset.String("name", helm.DefaultReleaseName, "Relase name")
	//flagset.StringP("namespace", "n", helm.DefaultNamespace, "Namespace to install into")
}
