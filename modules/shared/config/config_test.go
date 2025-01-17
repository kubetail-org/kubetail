package config

import (
	"net/http"
	"os"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var cfg1 = `
dashboard:
  environment: cluster
  session:
    cookie:
      same-site: strict
`

func TestConfig(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "config-test-*.yaml")
	assert.Nil(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(cfg1)
	assert.Nil(t, err)
	tmpFile.Close()

	cfg, err := NewConfig(viper.New(), tmpFile.Name())
	require.Nil(t, err)
	assert.Equal(t, EnvironmentCluster, cfg.Dashboard.Environment)
	assert.Equal(t, http.SameSiteStrictMode, cfg.Dashboard.Session.Cookie.SameSite)
}
