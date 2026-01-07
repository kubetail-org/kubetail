package cmd

import (
	"github.com/spf13/cobra"
)

const configHelp = `The config command provides a set of subcommands to manage kubetail configuration.

You can use it to initialize new configuration file  or modify specific values to customize the behavior of kubetail CLI.
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
