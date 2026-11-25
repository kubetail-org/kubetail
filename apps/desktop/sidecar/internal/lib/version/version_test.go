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

package version

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An ordinary build carries no stamp: the build script sets Version through the
// linker only when SIDECAR_VERSION is given.
func TestVersionDefaultsToDev(t *testing.T) {
	if Version != "dev" {
		t.Errorf("Version = %q, want dev", Version)
	}
}

func TestUserAgentNamesTheDesktopAppAndThePlatform(t *testing.T) {
	assert.Equal(t, "kubetail-desktop/dev ("+runtime.GOOS+"/"+runtime.GOARCH+")", UserAgent())
}

// userAgentOf is the User-Agent a server reads off req sent through Transport.
func userAgentOf(t *testing.T, base http.RoundTripper, ua string) string {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
	}))
	t.Cleanup(srv.Close)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}

	resp, err := (&http.Client{Transport: Transport(base)}).Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, ua, req.Header.Get("User-Agent"), "the caller's request is untouched")
	return got
}

func TestTransportNamesARequestThatNamesNoAgent(t *testing.T) {
	assert.Equal(t, UserAgent(), userAgentOf(t, nil, ""))
	assert.Equal(t, UserAgent(), userAgentOf(t, http.DefaultTransport, ""))
}

func TestTransportPutsKubetailAheadOfARequestsOwnAgent(t *testing.T) {
	assert.Equal(t, UserAgent()+" other/1", userAgentOf(t, nil, "other/1"))
	assert.Equal(t, UserAgent()+" other/1", userAgentOf(t, nil, UserAgent()+" other/1"))
}

func TestPrefixNamesTheDesktopAppOnce(t *testing.T) {
	assert.Equal(t, UserAgent(), Prefix(""))
	assert.Equal(t, UserAgent()+" kubectl/v1.31.0", Prefix("kubectl/v1.31.0"))
	assert.Equal(t, UserAgent()+" kubectl/v1.31.0", Prefix(Prefix("kubectl/v1.31.0")))
	assert.Equal(t, UserAgent(), Prefix(UserAgent()))
}

// A command may claim another build: the running one still leads.
func TestPrefixLeadsAnotherBuildsAgent(t *testing.T) {
	for _, ua := range []string{"kubetail-desktop/old", "kubetail-desktop/old (linux/amd64)", UserAgent() + "x"} {
		assert.Equal(t, UserAgent()+" "+ua, Prefix(ua), ua)
	}
}
