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
	"os"

	"github.com/spf13/cobra"
)

const targetRepoURL = "https://github.com/kubetail-org/helm-charts"

// clusterInstallCmd represents the `cluster install` command
var clusterInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install cluster resources",
	Long: `This command installs Kubetail cluster resources into an
	existing Kubernetes cluster.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Initialize Helm settings
		settings := cli.New()

		// Load the repository file
		repoFile := settings.RepositoryConfig
		r, err := repo.LoadFile(repoFile)
		if err != nil {
			fmt.Printf("Failed to load repo file: %v\n", err)
			os.Exit(1)
		}

		// Check if the target repository is in the list
		repoFound := false
		for _, cfg := range r.Repositories {
			if cfg.URL == targetRepoURL {
				repoFound = true
				fmt.Printf("Repository '%s' is installed.\n", targetRepoURL)
				break
			}
		}

		if !repoFound {
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
