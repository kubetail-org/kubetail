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

const clusterInstallHelp = `
This command creates a new release using the latest chart available.

If the Kubetail charts repository is already present in Helm, this command
will use the latest version of the "kubetail" chart available locally. If
it isn't, it will add the repository and then install the latest version.
`

// clusterInstallCmd represents the `cluster install` command
var clusterInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Create a new release",
	Long:  clusterInstallHelp,
	Run: func(cmd *cobra.Command, args []string) {
		// Get flags
		kubeContext, _ := cmd.Flags().GetString("kube-context")
		name, _ := cmd.Flags().GetString("name")
		namespace, _ := cmd.Flags().GetString("namespace")

		// Init client
		client, err := helm.NewClient(kubeContext)
		cli.ExitOnError(err)

		// Install
		release, err := client.InstallLatest(namespace, name)
		cli.ExitOnError(err)

		fmt.Printf("Installed release '%s' into namespace '%s' successfully\n", release.Name, release.Namespace)
	},
}

func init() {
	clusterCmd.AddCommand(clusterInstallCmd)

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// serveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	flagset := clusterInstallCmd.Flags()
	flagset.SortFlags = false
	flagset.String("kube-context", "", "Name of the kubeconfig context to use")
	flagset.String("name", helm.DefaultReleaseName, "Release name")
	flagset.StringP("namespace", "n", helm.DefaultNamespace, "Namespace to install into")
}
