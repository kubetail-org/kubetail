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

package signer

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
)

// Covers takes the same scheme, the host without case, the same effective port,
// and a path that is the base's or continues it after a slash.
func TestCovers(t *testing.T) {
	for _, tc := range []struct {
		base string
		in   map[string]bool
	}{
		{"https://api.test/v1", map[string]bool{
			"https://api.test/v1":                   true,
			"https://API.test/v1":                   true,
			"https://api.test:443/v1/models":        true,
			"https://api.test/v1/responses":         true,
			"https://api.test/v1x":                  false,
			"https://api.test.example/v1/responses": false,
			"http://api.test/v1/responses":          false,
			"https://api.test:8443/v1/responses":    false,
			"https://api.test/v2/responses":         false,
			"https://api.test/":                     false,
		}},
		{"https://openrouter.test/api/v1/", map[string]bool{
			"https://openrouter.test/api/v1":        true,
			"https://openrouter.test/api/v1/models": true,
			"https://openrouter.test/auth":          false,
		}},
		{"http://openrouter.test/api/v1", map[string]bool{
			"http://openrouter.test:80/api/v1/models": true,
			"http://openrouter.test:81/api/v1/models": false,
			"http://openrouter.test:8080/api/v1":      false,
		}},
	} {
		base, err := url.Parse(tc.base)
		require.NoError(t, err)
		for raw, want := range tc.in {
			u, err := url.Parse(raw)
			require.NoError(t, err)
			assert.Equal(t, want, Covers(base, u), "%s under %s", raw, tc.base)
		}
	}
}

// A base that names its port covers that port alone.
func TestCoversANamedPort(t *testing.T) {
	base, err := url.Parse("http://local.test:8080/v1")
	require.NoError(t, err)
	for raw, want := range map[string]bool{"http://local.test:8080/v1/x": true, "http://local.test/v1/x": false} {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		assert.Equal(t, want, Covers(base, u), raw)
	}
}

// A base with no host covers nothing, a relative URL included.
func TestCoversRefusesABaseWithNoHost(t *testing.T) {
	base, err := url.Parse("")
	require.NoError(t, err)
	for _, raw := range []string{"/anything", "", "https://api.test/v1"} {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		assert.False(t, Covers(base, u), raw)
	}
}

// An ended binding fails every Sign signed out, sets no header, and its
// Refused is a no-op.
func TestEndedSignsNothing(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://api.test/v1", nil)
	require.NoError(t, err)

	Ended{}.Refused(req)

	assert.ErrorIs(t, Ended{}.Sign(req), llm.ErrSignedOut)
	assert.Empty(t, req.Header.Get("Authorization"))
}
