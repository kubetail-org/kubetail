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
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

const targetRepoURL = "https://github.com/kubetail-org/helm-charts"

// clusterInstallCmd represents the `cluster install` command
var clusterInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install from latest chart available locally",
	Long: `This command creates a new release in an existing
	Kubernetes cluster using the latest chart available locally.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Execute the `helm repo list` command
		xcmd := exec.Command("helm", "repo", "list")
		var out bytes.Buffer
		xcmd.Stdout = &out

		// Run the command and capture the output
		if err := xcmd.Run(); err != nil {
			fmt.Printf("Error executing helm command: %v\n", err)
			return
		}

		// Check if the output contains the target repo URL
		if strings.Contains(out.String(), targetRepoURL) {
			fmt.Printf("Repository '%s' is installed.\n", targetRepoURL)
		} else {
			fmt.Printf("Repository '%s' is not installed.\n", targetRepoURL)
		}
	},
}

func init() {
	clusterCmd.AddCommand(clusterInstallCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// installCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// installCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
