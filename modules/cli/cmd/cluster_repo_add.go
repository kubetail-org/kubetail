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

	"github.com/kubetail-org/kubetail/modules/shared/helm"

	"github.com/kubetail-org/kubetail/modules/cli/internal/cli"
)

const clusterRepoAddHelp = `
This command adds Kubetail's remote charts repository to Helm.
`

// clusterRepoAddCmd represents the `cluster repo Add` command
var clusterRepoAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add the repository",
	Long:  clusterRepoAddHelp,
	// coverage:ignore-start changes the Helm repository config on this machine
	Run: func(cmd *cobra.Command, args []string) {
		cli.ExitOnError(runClusterRepoAdd(cmd.OutOrStdout(), helm.NewClient()))
	},
	// coverage:ignore-stop
}

// repoAdder is the subset of helm.Client that repo add uses.
type repoAdder interface {
	AddRepo() error
}

// runClusterRepoAdd adds Kubetail's charts repository and reports it to w.
func runClusterRepoAdd(w io.Writer, client repoAdder) error {
	if err := client.AddRepo(); err != nil {
		return err
	}
	fmt.Fprintln(w, "Added repository 'kubetail'")
	return nil
}

func init() {
	clusterRepoCmd.AddCommand(clusterRepoAddCmd)
}
