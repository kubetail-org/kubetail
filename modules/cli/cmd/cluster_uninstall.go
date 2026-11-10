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

const clusterUninstallHelp = `
This command removes an existing release.
`

// clusterUninstallCmd represents the `cluster uninstall` command
var clusterUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall an existing release",
	Long:  clusterUninstallHelp,
	// coverage:ignore-start uninstalls a release from a live cluster
	Run: func(cmd *cobra.Command, args []string) {
		cli.ExitOnError(runClusterUninstall(cmd.OutOrStdout(), newHelmClient(cmd)))
	},
	// coverage:ignore-stop
}

// releaseUninstaller is the subset of helm.Client that uninstall uses.
type releaseUninstaller interface {
	UninstallRelease(namespace, releaseName string) (*release.Release, error)
}

// runClusterUninstall uninstalls the default release and reports it to w.
func runClusterUninstall(w io.Writer, client releaseUninstaller) error {
	r, err := client.UninstallRelease(helm.DefaultNamespace, helm.DefaultReleaseName)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Deleted release '%s' in namespace '%s'\n", r.Name, r.Namespace)
	return nil
}

func init() {
	clusterCmd.AddCommand(clusterUninstallCmd)

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// serveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	flagset := clusterUninstallCmd.Flags()
	flagset.SortFlags = false
	flagset.String(KubeContextFlag, "", "Name of the kubeconfig context to use")
	//flagset.String("name", helm.DefaultReleaseName, "Release name")
	//flagset.StringP("namespace", "n", helm.DefaultNamespace, "Namespace to install into")
}
