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
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestRejectsRemovedTLSFields(t *testing.T) {
	dir := t.TempDir()
	dummy := filepath.Join(dir, "dummy.pem")
	require.NoError(t, os.WriteFile(dummy, []byte("dummy"), 0o600))

	cases := []struct {
		name string
		body string
	}{
		{"client-ca-file", "tls:\n  client-ca-file: " + dummy + "\n"},
		{"enabled", "tls:\n  enabled: false\n"},
		{"client-auth-type", "tls:\n  client-auth-type: require-and-verify\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewConfig(writeConfig(t, tc.body), viper.New())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.name)
		})
	}
}

func TestRequiresCertAndKey(t *testing.T) {
	dir := t.TempDir()
	dummy := filepath.Join(dir, "dummy.pem")
	require.NoError(t, os.WriteFile(dummy, []byte("dummy"), 0o600))

	cases := []struct {
		name string
		body string
	}{
		{"missing key-file", "tls:\n  cert-file: " + dummy + "\n"},
		{"missing cert-file", "tls:\n  key-file: " + dummy + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewConfig(writeConfig(t, tc.body), viper.New())
			require.Error(t, err)
		})
	}
}
