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
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/kubetail-org/kubetail/modules/cli/internal/helm"
)

// clusterListCmd represents the `cluster list` command
var clusterListCmd = &cobra.Command{
	Use:   "list",
	Short: "List current releases",
	Long:  `This command lists the currently installed releases.`,
	Run: func(cmd *cobra.Command, args []string) {
		releases, err := helm.ListReleases()
		if err != nil {
			log.Fatal(err)
		}

		// Create a new tab writer with desired padding and settings
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

		// Print headers
		fmt.Fprintln(w, "NAME\tNAMESPACE\tREVISION\tUPDATED\tSTATUS\tCHART\tAPP VERSION")

		// Print data rows
		for _, r := range releases {
			chartName := fmt.Sprintf("%s-%s", r.Chart.Metadata.Name, r.Chart.Metadata.Version)
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\t%s\n", r.Name, r.Namespace, r.Version, r.Info.LastDeployed, r.Info.Status, chartName, r.Chart.AppVersion())
		}

		// Flush to output
		w.Flush()
	},
}

func init() {
	clusterCmd.AddCommand(clusterListCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// installCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// installCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
