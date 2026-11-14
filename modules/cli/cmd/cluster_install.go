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
	// coverage:ignore-start installs a release into a live cluster
	Run: func(cmd *cobra.Command, args []string) {
		cli.ExitOnError(runClusterInstall(cmd.OutOrStdout(), newHelmClient(cmd)))
	},
	// coverage:ignore-stop
}

// releaseInstaller is the subset of helm.Client that install uses.
type releaseInstaller interface {
	InstallLatest(namespace, releaseName string) (*release.Release, error)
}

// runClusterInstall installs the latest chart as the default release and
// reports it to w.
func runClusterInstall(w io.Writer, client releaseInstaller) error {
	r, err := client.InstallLatest(helm.DefaultNamespace, helm.DefaultReleaseName)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Installed release '%s' into namespace '%s' successfully\n", r.Name, r.Namespace)
	return nil
}

func init() {
	clusterCmd.AddCommand(clusterInstallCmd)

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// serveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	flagset := clusterInstallCmd.Flags()
	flagset.SortFlags = false
	flagset.String(KubeContextFlag, "", "Name of the kubeconfig context to use")
	//flagset.String("name", helm.DefaultReleaseName, "Release name")
	//flagset.StringP("namespace", "n", helm.DefaultNamespace, "Namespace to install into")
}
