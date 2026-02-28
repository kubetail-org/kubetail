package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sharedcfg "github.com/kubetail-org/kubetail/modules/shared/config"
)

func TestLoadServerConfig_Defaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd := &cobra.Command{}
	addServerCmdFlags(cmd)

	cfg, opts, err := loadServerConfig(cmd)
	assert.NoError(t, err)

	// validate config
	assert.Equal(t, "localhost:7500", cfg.Addr)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, sharedcfg.EnvironmentDesktop, cfg.Environment)
	assert.Equal(t, "", cfg.KubeconfigPath)
	assert.Equal(t, false, cfg.Logging.AccessLog.Enabled)
	assert.Equal(t, []string{"timestamp", "dot"}, cfg.UI.Columns)

	// validate serveOptions
	assert.Equal(t, 7500, opts.port)
	assert.Equal(t, "localhost", opts.host)
	assert.Equal(t, false, opts.skipOpen)
}

func TestLoadServerConfig(t *testing.T) {
	cmd := &cobra.Command{}
	addServerCmdFlags(cmd)

	// adding this to test the flags added in the root command
	addRootCmdFlagsTo(cmd.Flags())

	mockFlags := []struct {
		port       int
		host       string
		logLevel   string
		skipOpen   bool
		test       bool
		kubeconfig string
		inCluster  bool
	}{
		{7500, "localhost", "info", false, false, "", false},
		{8080, "localhost", "debug", true, false, "/home/.kube/customConfig", true},
	}

	for _, val := range mockFlags {
		cmd.Flags().Set("port", fmt.Sprintf("%d", val.port))
		cmd.Flags().Set("host", val.host)
		cmd.Flags().Set("log-level", val.logLevel)
		cmd.Flags().Set("skip-open", fmt.Sprintf("%t", val.skipOpen))
		cmd.Flags().Set("test", fmt.Sprintf("%t", val.test))

		cmd.Flags().Set(InClusterFlag, fmt.Sprintf("%t", val.inCluster))
		cmd.Flags().Set(KubeconfigFlag, val.kubeconfig)

		cfg, opts, err := loadServerConfig(cmd)
		assert.NoError(t, err)

		// validate config
		assert.Equal(t, cfg.Addr, fmt.Sprintf("%s:%d", val.host, val.port))
		assert.Equal(t, cfg.Logging.Level, val.logLevel)
		assert.Equal(t, cfg.KubeconfigPath, val.kubeconfig)
		assert.Equal(t, cfg.Logging.AccessLog.Enabled, false)

		if val.inCluster {
			assert.Equal(t, cfg.Environment, sharedcfg.EnvironmentCluster)
		} else {
			assert.Equal(t, cfg.Environment, sharedcfg.EnvironmentDesktop)
		}

		// validate serveOptions
		assert.Equal(t, opts.port, val.port)
		assert.Equal(t, opts.host, val.host)
		assert.Equal(t, opts.skipOpen, val.skipOpen)
	}
}

func TestLoadServerConfig_DashboardColumns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	configYAML := `
dashboard:
  columns: ["timestamp", "dot", "pod", "container"]
`
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(filePath, []byte(configYAML), 0644)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	addServerCmdFlags(cmd)
	addRootCmdFlagsTo(cmd.Flags())
	cmd.Flags().Set("config", filePath)

	cfg, _, err := loadServerConfig(cmd)
	require.NoError(t, err)

	assert.Equal(t, []string{"timestamp", "dot", "pod", "container"}, cfg.UI.Columns)
}
