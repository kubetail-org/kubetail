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
		targetPath, err := config.DefaultConfigPath()
		if err != nil {
			zlog.Fatal().Err(err).Msg("Failed to determine config path")
		}

		if _, err := os.Stat(targetPath); err == nil {
			zlog.Fatal().Msgf("Configuration file already exists: %s", targetPath)
		}

		configDir := filepath.Dir(targetPath)
		if err := os.MkdirAll(configDir, 0755); err != nil {
			zlog.Fatal().Err(err).Msg("Failed to create configuration directory")
		}

		content, err := assets.FS.ReadFile("config.yaml")
		if err != nil {
			zlog.Fatal().Err(err).Msg("Failed to read configuration file")
		}

		err = os.WriteFile(targetPath, content, 0644)
		if err != nil {
			zlog.Fatal().Err(err).Msg("Failed to write configuration file")
		}

		zlog.Info().Msgf("Configuration initialized: %s", targetPath)
	},
}

func init() {
	configCmd.AddCommand(configInitCmd)
}
