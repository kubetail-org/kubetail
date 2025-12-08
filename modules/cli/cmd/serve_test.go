package cmd

import (
	"fmt"
	"testing"

	"github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestLoadServerConfig_Defaults(t *testing.T) {
	cmd := &cobra.Command{}
	addServerCmdFlags(cmd)

	cfg, opts, err := loadServerConfig(cmd)
	assert.NoError(t, err)

	// validate config
	assert.Equal(t, "localhost:7500", cfg.Dashboard.Addr)
	assert.Equal(t, "info", cfg.Dashboard.Logging.Level)
	assert.Equal(t, config.EnvironmentDesktop, cfg.Dashboard.Environment)
	assert.Equal(t, "", cfg.KubeconfigPath)
	assert.Equal(t, false, cfg.Dashboard.Logging.AccessLog.Enabled)

	// validate serveOptions
	assert.Equal(t, 7500, opts.port)
	assert.Equal(t, "localhost", opts.host)
	assert.Equal(t, false, opts.skipOpen)
}

func TestLoadServerConfig(t *testing.T) {
	cmd := &cobra.Command{}
	addServerCmdFlags(cmd)

	// adding this to test the flags added in the root command
	cmd.Flags().String(KubeconfigFlag, "", "Path to kubeconfig file")
	cmd.Flags().Bool(InClusterFlag, false, "Use in-cluster Kubernetes configuration")

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
		assert.Equal(t, cfg.Dashboard.Addr, fmt.Sprintf("%s:%d", val.host, val.port))
		assert.Equal(t, cfg.Dashboard.Logging.Level, val.logLevel)
		assert.Equal(t, cfg.KubeconfigPath, val.kubeconfig)
		assert.Equal(t, cfg.Dashboard.Logging.AccessLog.Enabled, false)

		if val.inCluster {
			assert.Equal(t, cfg.Dashboard.Environment, config.EnvironmentCluster)
		} else {
			assert.Equal(t, cfg.Dashboard.Environment, config.EnvironmentDesktop)
		}

		// validate serveOptions
		assert.Equal(t, opts.port, val.port)
		assert.Equal(t, opts.host, val.host)
		assert.Equal(t, opts.skipOpen, val.skipOpen)
	}
}
