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

	"github.com/kubetail-org/kubetail/modules/cli/internal/helm"
	"github.com/spf13/cobra"
)

// clusterRepoRemoveCmd represents the `cluster repo remove` command
var clusterRepoRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove local charts and repository index",
	Long:  `This command removes the local charts and the charts repository index.`,
	Run: func(cmd *cobra.Command, args []string) {
		err := helm.RemoveRepo()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("Removed repository")
	},
}

func init() {
	clusterRepoCmd.AddCommand(clusterRepoRemoveCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// PurgeCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// PurgeCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
