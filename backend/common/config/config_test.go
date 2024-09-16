package config

import (
	"net/http"
	"os"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

var cfg1 = `
auth-mode: cluster
server:
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
	assert.Nil(t, err)
	assert.Equal(t, AuthModeCluster, cfg.AuthMode)
	assert.Equal(t, http.SameSiteStrictMode, cfg.Server.Session.Cookie.SameSite)
}
