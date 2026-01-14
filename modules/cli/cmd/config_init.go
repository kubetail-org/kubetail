package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kubetail-org/kubetail/modules/cli/assets"
	"github.com/kubetail-org/kubetail/modules/shared/config"
	zlog "github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

const configInitHelp = `
This command creates a default configuration file.
`

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a default configuration file",
	Long:  configInitHelp,
	Run: func(cmd *cobra.Command, args []string) {
		targetPath, _ := cmd.Flags().GetString("path")

		format, _ := cmd.Flags().GetString("format")

		if format == "yml" || format == "" {
			format = "yaml"
		}
		if format != "yaml" && format != "json" && format != "toml" {
			zlog.Fatal().Msgf("Format '%s' is not supported", format)
		}

		if targetPath == "" {
			tmp, err := config.DefaultConfigPath(format)
			if err != nil {
				zlog.Fatal().Err(err).Msg("Unable to determine config path")
			}
			targetPath = tmp
		}

		force, _ := cmd.Flags().GetBool("force")
		if !force {
			if _, err := os.Stat(targetPath); err == nil {
				zlog.Fatal().Msgf("Configuration file already exists: %s", targetPath)
			}
		}

		configDir := filepath.Dir(targetPath)
		if err := os.MkdirAll(configDir, 0755); err != nil {
			zlog.Fatal().Err(err).Msg("Failed to create configuration directory")
		}

		content, err := assets.FS.ReadFile(fmt.Sprintf("config.%s", format))
		if err != nil {
			zlog.Fatal().Err(err).Msg("Failed to read configuration file")
		}

		if err := os.WriteFile(targetPath, content, 0644); err != nil {
			zlog.Fatal().Err(err).Msg("Failed to write configuration file")
		}

		zlog.Info().Msgf("Configuration initialized: %s", targetPath)
	},
}

func init() {
	configCmd.AddCommand(configInitCmd)

	flagset := configInitCmd.Flags()
	flagset.SortFlags = false
	flagset.String("path", "", "Target path for configuration file (default is $HOME/.kubetail/config.yaml)")
	flagset.String("format", "", "Format of configuration file: yaml, toml or json (default: yaml)")
	flagset.Bool("force", false, "Overwrite existing configuration file")
}
