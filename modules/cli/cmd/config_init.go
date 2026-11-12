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

	zlog "github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/kubetail-org/kubetail/modules/cli/assets"
	"github.com/kubetail-org/kubetail/modules/cli/pkg/config"
)

const configInitHelp = `
This command creates a default configuration file.
`

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a default configuration file",
	Long:  configInitHelp,
	Run: func(cmd *cobra.Command, args []string) {
		flags := cmd.Flags()
		targetPath, _ := flags.GetString("path")
		format, _ := flags.GetString("format")
		force, _ := flags.GetBool("force")

		path, err := runConfigInit(targetPath, format, force)
		if err != nil { // coverage:ignore zlog.Fatal exits the process
			zlog.Fatal().Err(err).Msg("Failed to initialize configuration")
		}

		zlog.Info().Msgf("Configuration initialized: %s", path)
	},
}

// runConfigInit writes the default config file in the given format to
// targetPath, or to the default path when targetPath is empty, and returns
// the path it wrote. It refuses to overwrite a file unless force is set.
func runConfigInit(targetPath, format string, force bool) (string, error) {
	switch format {
	case "", "yml":
		format = "yaml"
	case "yaml", "json", "toml":
		// valid
	default:
		return "", fmt.Errorf("format '%s' is not supported", format)
	}

	if targetPath == "" {
		var err error
		targetPath, err = config.DefaultConfigPath(format)
		if err != nil {
			return "", fmt.Errorf("unable to determine config path: %w", err)
		}
	}

	if !force {
		if _, err := os.Stat(targetPath); err == nil {
			return "", fmt.Errorf("configuration file already exists: %s", targetPath)
		}
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return "", fmt.Errorf("failed to create configuration directory: %w", err)
	}

	content, err := assets.FS.ReadFile(fmt.Sprintf("config.%s", format))
	if err != nil { // coverage:ignore every supported format is embedded
		return "", fmt.Errorf("failed to read configuration file: %w", err)
	}

	if err := os.WriteFile(targetPath, content, 0644); err != nil {
		return "", fmt.Errorf("failed to write configuration file: %w", err)
	}

	return targetPath, nil
}

func init() {
	configCmd.AddCommand(configInitCmd)
	addConfigInitCmdFlags(configInitCmd)
}

func addConfigInitCmdFlags(cmd *cobra.Command) {
	flagset := cmd.Flags()
	flagset.SortFlags = false
	flagset.String("path", "", "Target path for configuration file (default is $HOME/.kubetail/config.yaml)")
	flagset.String("format", "yaml", "Configuration file format (yaml|json|toml)")
	flagset.Bool("force", false, "Overwrite existing configuration file")
}
