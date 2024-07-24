package config

import (
	"fmt"
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

	v := viper.New()

	cfg, err := NewConfig(v, tmpFile.Name())
	assert.Nil(t, err)
	fmt.Println(err)
	//assert.Equal(t, cfg.AuthMode, "cluster")
	fmt.Println(cfg.Server.Session.Cookie.SameSite)
}
