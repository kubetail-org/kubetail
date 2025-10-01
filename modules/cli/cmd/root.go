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
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	KubeconfigFlag  = clientcmd.RecommendedConfigPathFlag
	KubeContextFlag = "kube-context"
)

var version = "dev" // default version for local builds

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:     "kubetail",
	Version: version,
	Short:   "Kubetail - Kubernetes logging utility",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	// Ensure all commands/flagsets use the shared normalizer before parsing
	applyFlagNormalization(rootCmd)

	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

// normalizeFlags provides shared normalization across all commands/flagsets.
// It maps --context to the canonical --kube-context and normalizes underscores to dashes.
func normalizeFlags(f *pflag.FlagSet, name string) pflag.NormalizedName {
	if name == "context" {
		return pflag.NormalizedName(KubeContextFlag)
	}
	n := strings.ReplaceAll(name, "_", "-")
	return pflag.NormalizedName(n)
}

// applyFlagNormalization recursively applies the shared normalization function
// to the provided command and all of its sub-commands.
func applyFlagNormalization(cmd *cobra.Command) {
	if fs := cmd.Flags(); fs != nil {
		fs.SetNormalizeFunc(normalizeFlags)
	}
	if pfs := cmd.PersistentFlags(); pfs != nil {
		pfs.SetNormalizeFunc(normalizeFlags)
	}
	for _, c := range cmd.Commands() {
		applyFlagNormalization(c)
	}
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.cli.yaml)")
	rootCmd.PersistentFlags().String(KubeconfigFlag, "", "Path to kubeconfig file")

	// Apply normalization to root flagsets as well
	rootCmd.PersistentFlags().SetNormalizeFunc(normalizeFlags)
	rootCmd.Flags().SetNormalizeFunc(normalizeFlags)

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	//rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
