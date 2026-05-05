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
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kubetail-org/kubetail/modules/shared/logging"

	"github.com/kubetail-org/kubetail/modules/cli/internal/updatecheck"
	"github.com/kubetail-org/kubetail/modules/cli/pkg/config"
)

const (
	KubeconfigFlag  = clientcmd.RecommendedConfigPathFlag
	KubeContextFlag = "kube-context"
	InClusterFlag   = "in-cluster"
)

var version = "dev" // default version for local builds

// waitForRefresh and cachedUpdateOpts are set by PersistentPreRunE and consumed by
// PersistentPostRunE so resolveUpdateCheckOptions runs only once per command invocation.
var waitForRefresh func() = func() {}
var cachedUpdateOpts *updatecheck.Options

// getCommandDisplayName determines the CLI display name based on how it's invoked
func getCommandDisplayName() string {
	// Get the base name of the executable
	executable := filepath.Base(os.Args[0])

	// Check if running as a kubectl plugin (via krew or direct kubectl invocation)
	if executable == "kubectl" || strings.HasPrefix(executable, "kubectl-") {
		return "kubectl kubetail"
	}

	// Default to standalone binary name
	return "kubetail"
}

func resolveUpdateCheckOptions(cmd *cobra.Command) (updatecheck.Options, bool) {
	rootFlags := cmd.Root().PersistentFlags()
	configPath, _ := rootFlags.GetString("config")
	v := viper.New()
	v.BindPFlag("general.kubeconfig", rootFlags.Lookup(KubeconfigFlag))
	var kubeconfigPath string
	if cfg, err := config.NewConfig(configPath, v); err == nil && cfg != nil {
		kubeconfigPath = cfg.General.KubeconfigPath
	}
	// --kube-context is a local flag on each subcommand, not a root persistent flag.
	kubeContext, _ := cmd.Flags().GetString(KubeContextFlag)
	cliCacheFile, err := updatecheck.DefaultCLICacheFile()
	if err != nil {
		return updatecheck.Options{}, false
	}
	helmCacheFile, err := updatecheck.DefaultHelmCacheFile()
	if err != nil {
		return updatecheck.Options{}, false
	}
	return updatecheck.Options{
		CLICacheFile:      cliCacheFile,
		HelmCacheFile:     helmCacheFile,
		CurrentCLIVersion: version,
		KubeconfigPath:    kubeconfigPath,
		KubeContext:       kubeContext,
		CacheTTL:          updatecheck.DefaultCacheTTL,
	}, true
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:     "kubetail",
	Version: version,
	Short:   "Kubetail - Kubernetes logging utility",
	Annotations: map[string]string{
		cobra.CommandDisplayNameAnnotation: getCommandDisplayName(),
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if opts, ok := resolveUpdateCheckOptions(cmd); ok {
			cachedUpdateOpts = &opts
			waitForRefresh = updatecheck.TriggerRefreshIfStale(opts)
		}
		return nil
	},
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		waitForRefresh()
		if cachedUpdateOpts != nil {
			for _, n := range updatecheck.Notify(*cachedUpdateOpts) {
				fmt.Fprint(os.Stderr, colorizeWarning(os.Stderr, n.Message))
			}
		}
		return nil
	},
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
		fs.SortFlags = false
		fs.SetNormalizeFunc(normalizeFlags)
	}
	if pfs := cmd.PersistentFlags(); pfs != nil {
		pfs.SortFlags = false
		pfs.SetNormalizeFunc(normalizeFlags)
	}
	if ifs := cmd.InheritedFlags(); ifs != nil {
		ifs.SortFlags = false
	}
	for _, c := range cmd.Commands() {
		applyFlagNormalization(c)
	}
}

func addRootCmdFlags(cmd *cobra.Command) {
	addRootCmdFlagsTo(cmd.PersistentFlags())
}

func addRootCmdFlagsTo(flagset *pflag.FlagSet) {
	flagset.String(KubeconfigFlag, "", "Path to kubeconfig file")
	flagset.Bool(InClusterFlag, false, "Use in-cluster Kubernetes configuration")
	flagset.StringP("config", "c", "", "Path to config file (default is $HOME/.kubetail/config.yaml)")
}

func init() {
	// Configure logger
	logging.ConfigureLogger(logging.LoggerOptions{
		Enabled: true,
		Level:   "info",
		Format:  "cli",
	})

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.cli.yaml)")

	rootCmd.Flags().SortFlags = false

	addRootCmdFlags(rootCmd)

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	//rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
