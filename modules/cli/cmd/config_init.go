package cmd

import (
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
	PreRun: func(cmd *cobra.Command, args []string) {
		// Configure logger
		config.ConfigureLogger(config.LoggerOptions{
			Enabled: true,
			Level:   "info",
			Format:  "cli",
		})
	},
	Run: func(cmd *cobra.Command, args []string) {
		targetPath, _ := cmd.Flags().GetString("path")
		if targetPath == "" {
			tmp, err := config.DefaultConfigPath()
			if err != nil {
				zlog.Fatal().Err(err).Msg("Unable to determine config path")
			}
			targetPath = tmp
		}

		force, _ := cmd.Flags().GetBool("force")
		if _, err := os.Stat(targetPath); err == nil {
			if !force {
				zlog.Fatal().Msgf("Configuration file already exists: %s", targetPath)
			}
			zlog.Info().Msgf("Overwriting existing configuration file: %s", targetPath)
		}

		configDir := filepath.Dir(targetPath)
		if err := os.MkdirAll(configDir, 0755); err != nil {
			zlog.Fatal().Err(err).Msg("Failed to create configuration directory")
		}

		content, err := assets.FS.ReadFile("config.yaml")
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
	flagset.Bool("force", false, "Overwrite existing configuration file")
}
