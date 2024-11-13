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

	"github.com/spf13/cobra"
)

// clusterPurgeCmd represents the `cluster Purge` command
var clusterPurgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Remove local charts and remote repository information",
	Long: `This command Purges the information of available charts locally
from the remote chart respository.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Purge called")
	},
}

func init() {
	clusterCmd.AddCommand(clusterPurgeCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// PurgeCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// PurgeCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
