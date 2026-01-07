package cmd

import (
	"github.com/spf13/cobra"
)

const configHelp = `
Subcommands for initializing and modifying the kubetail configuration.
`

// configCmd represents the ext command
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage kubetail configuration",
	Long:  configHelp,
}

func init() {
	rootCmd.AddCommand(configCmd)
}
