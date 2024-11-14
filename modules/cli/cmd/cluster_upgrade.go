// Copyright 2024 Andres Morey
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
	"log"

	"github.com/spf13/cobra"

	"github.com/kubetail-org/kubetail/modules/cli/internal/helm"
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
		release, err := helm.UpgradeRelease()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Successfully upgraded release: %s to version %s\n", release.Name, release.Chart.Metadata.Version)

	},
}

func init() {
	clusterCmd.AddCommand(clusterUpgradeCmd)
}
