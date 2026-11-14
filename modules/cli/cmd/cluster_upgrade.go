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
	"fmt"
	"io"

	"github.com/spf13/cobra"
	release "helm.sh/helm/v4/pkg/release/v1"

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
	// coverage:ignore-start upgrades a release in a live cluster
	Run: func(cmd *cobra.Command, args []string) {
		cli.ExitOnError(runClusterUpgrade(cmd.OutOrStdout(), newHelmClient(cmd)))
	},
	// coverage:ignore-stop
}

// releaseUpgrader is the subset of helm.Client that upgrade uses.
type releaseUpgrader interface {
	UpgradeRelease(namespace, releaseName string) (*release.Release, error)
}

// runClusterUpgrade upgrades the default release and reports it to w.
func runClusterUpgrade(w io.Writer, client releaseUpgrader) error {
	r, err := client.UpgradeRelease(helm.DefaultNamespace, helm.DefaultReleaseName)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Successfully upgraded release '%s' in namespace '%s' (revision: %d)\n", r.Name, r.Namespace, r.Version)
	return nil
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
