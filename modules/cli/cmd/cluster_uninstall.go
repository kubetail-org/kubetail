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

const clusterUninstallHelp = `
This command removes an existing release.
`

// clusterUninstallCmd represents the `cluster uninstall` command
var clusterUninstallCmd = &cobra.Command{
	Use:   "uninstall [release-name]",
	Short: "Uninstall an existing release",
	Long:  clusterUninstallHelp,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		// Get args
		releaseName := args[0]

		// Get flags
		kubeContext, _ := cmd.Flags().GetString("kube-context")
		namespace, _ := cmd.Flags().GetString("namespace")

		// Init client
		client, err := helm.NewClient(kubeContext)
		cli.ExitOnError(err)

		// Uninstall
		response, err := client.UninstallRelease(namespace, releaseName)
		cli.ExitOnError(err)

		fmt.Printf("Deleted release '%s' in namespace '%s'\n", response.Release.Name, response.Release.Namespace)
	},
}

func init() {
	clusterCmd.AddCommand(clusterUninstallCmd)

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// serveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	flagset := clusterUninstallCmd.Flags()
	flagset.SortFlags = false
	flagset.String("kube-context", "", "Name of the kubeconfig context to use")
	flagset.StringP("namespace", "n", helm.DefaultNamespace, "Namespace to install into")
}
