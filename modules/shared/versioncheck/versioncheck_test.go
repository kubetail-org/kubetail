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

package versioncheck

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestChecker_GetLatestCLIVersion_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		release := githubRelease{
			TagName: "cli/v0.11.0",
		}
		json.NewEncoder(w).Encode(release)
	}))
	defer server.Close()

	c := NewChecker(WithHTTPClient(server.Client())).(*checker)
	c.githubClient.cliReleasesURL = server.URL

	info, err := c.GetLatestCLIVersion()

	assert.NoError(t, err)
	assert.Equal(t, "0.11.0", info.Version)
}

func TestChecker_GetLatestCLIVersion_NetworkError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := NewChecker(WithHTTPClient(server.Client())).(*checker)
	c.githubClient.cliReleasesURL = server.URL

	info, err := c.GetLatestCLIVersion()

	assert.Nil(t, info)
	assert.Error(t, err)
}

func TestChecker_GetLatestHelmChartVersion_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		releases := []githubRelease{
			{TagName: "kubetail-0.17.0-beta", Prerelease: true, PublishedAt: "2024-01-03T00:00:00Z"},
			{TagName: "kubetail-0.17.0", Prerelease: false, Draft: false, PublishedAt: "2024-01-02T00:00:00Z"},
			{TagName: "kubetail-0.16.0", Prerelease: false, Draft: false, PublishedAt: "2024-01-01T00:00:00Z"},
		}
		json.NewEncoder(w).Encode(releases)
	}))
	defer server.Close()

	c := NewChecker(WithHTTPClient(server.Client())).(*checker)
	c.githubClient.helmChartsReleasesURL = server.URL

	info, err := c.GetLatestHelmChartVersion()

	assert.NoError(t, err)
	assert.Equal(t, "0.17.0", info.Version)
}

func TestWithOptions(t *testing.T) {
	t.Run("WithHTTPClient", func(t *testing.T) {
		customClient := &http.Client{Timeout: 30 * time.Second}
		c := NewChecker(WithHTTPClient(customClient)).(*checker)
		assert.Equal(t, customClient, c.githubClient.httpClient)
	})

	t.Run("DefaultValues", func(t *testing.T) {
		c := NewChecker().(*checker)
		assert.NotNil(t, c.githubClient.httpClient)
	})
}
