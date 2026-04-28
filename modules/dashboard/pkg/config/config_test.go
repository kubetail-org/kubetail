// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"bytes"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadYAML(t *testing.T, body string) (*Config, error) {
	t.Helper()
	v := viper.New()
	v.SetConfigType("yaml")
	require.NoError(t, v.ReadConfig(bytes.NewBufferString(body)))
	// NewConfig with empty path skips file-read but still applies viper bindings.
	// Using an in-memory viper avoids touching the filesystem.
	cfg := DefaultConfig()
	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func TestDefaultConfigAllowedOrigins(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, []string{}, cfg.AllowedOrigins)
}

func TestAllowedOriginsAcceptsValidEntries(t *testing.T) {
	cfg, err := loadYAML(t, `
allowed-origins:
  - https://kubetail.example.com
  - https://kubetail.example.com:8443
`)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"https://kubetail.example.com",
		"https://kubetail.example.com:8443",
	}, cfg.AllowedOrigins)
}

func TestAllowedOriginsRejectsMalformedEntry(t *testing.T) {
	_, err := loadYAML(t, `
allowed-origins:
  - not-a-url
`)
	assert.Error(t, err)
}
