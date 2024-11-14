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
	"github.com/spf13/cobra"
)

const clusterHelp = `
Subcommands for installing Kubetail cluster resources using Helm.

These commands use the Helm library under-the-hood so you don't need
to have Helm installed in order to use them. If you do have Helm
installed, these commands will integrate nicely with your existing
installation so you can switch between using them or Helm itself to
manage your cluster resources seamlessly.
`

// clusterCmd represents the ext command
var clusterCmd = &cobra.Command{
	Use:   "cluster",
	Short: "Manage cluster resources",
	Long:  clusterHelp,
}

func init() {
	rootCmd.AddCommand(clusterCmd)
}
