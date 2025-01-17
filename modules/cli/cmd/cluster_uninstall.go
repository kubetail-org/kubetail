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
	Use:   "uninstall",
	Short: "Uninstall an existing release",
	Long:  clusterUninstallHelp,
	Run: func(cmd *cobra.Command, args []string) {
		// Init client
		client, err := helm.NewClient()
		cli.ExitOnError(err)

		// Uninstall
		response, err := client.UninstallRelease()
		cli.ExitOnError(err)

		fmt.Printf("Deleted release '%s' in namespace '%s'\n", response.Release.Name, response.Release.Namespace)
	},
}

func init() {
	clusterCmd.AddCommand(clusterUninstallCmd)
}
