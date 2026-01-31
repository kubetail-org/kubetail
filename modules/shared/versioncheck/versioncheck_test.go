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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	info := c.GetLatestCLIVersion()

	assert.Equal(t, "cli/v0.11.0", info.Version)
	assert.Nil(t, info.Error)
	assert.False(t, info.LastChecked.IsZero())
}

func TestChecker_GetLatestCLIVersion_NetworkError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := NewChecker(WithHTTPClient(server.Client())).(*checker)
	c.githubClient.cliReleasesURL = server.URL

	info := c.GetLatestCLIVersion()

	assert.Empty(t, info.Version)
	assert.NotNil(t, info.Error)
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

	info := c.GetLatestHelmChartVersion()

	assert.Equal(t, "kubetail-0.17.0", info.Version)
	assert.Nil(t, info.Error)
}

func TestChecker_CacheHit(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		release := githubRelease{TagName: "cli/v0.11.0"}
		json.NewEncoder(w).Encode(release)
	}))
	defer server.Close()

	c := NewChecker(
		WithHTTPClient(server.Client()),
		WithCacheTTL(1*time.Hour),
	).(*checker)
	c.githubClient.cliReleasesURL = server.URL

	// First call - should fetch
	c.GetLatestCLIVersion()
	assert.Equal(t, int32(1), callCount.Load())

	// Second call - should use cache
	c.GetLatestCLIVersion()
	assert.Equal(t, int32(1), callCount.Load())
}

func TestChecker_CacheExpiry(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		release := githubRelease{TagName: "cli/v0.11.0"}
		json.NewEncoder(w).Encode(release)
	}))
	defer server.Close()

	c := NewChecker(
		WithHTTPClient(server.Client()),
		WithCacheTTL(50*time.Millisecond),
	).(*checker)
	c.githubClient.cliReleasesURL = server.URL

	// First call
	c.GetLatestCLIVersion()
	assert.Equal(t, int32(1), callCount.Load())

	// Wait for cache to expire
	time.Sleep(100 * time.Millisecond)

	// Second call - should fetch again
	c.GetLatestCLIVersion()
	assert.Equal(t, int32(2), callCount.Load())
}

func TestChecker_ConcurrentRequests_SingleFetch(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		time.Sleep(50 * time.Millisecond)
		release := githubRelease{TagName: "cli/v0.11.0"}
		json.NewEncoder(w).Encode(release)
	}))
	defer server.Close()

	c := NewChecker(
		WithHTTPClient(server.Client()),
		WithCacheTTL(1*time.Hour),
	).(*checker)
	c.githubClient.cliReleasesURL = server.URL

	// Launch multiple concurrent requests
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			info := c.GetLatestCLIVersion()
			assert.Equal(t, "cli/v0.11.0", info.Version)
		}()
	}
	wg.Wait()

	// Should only have made ONE request despite 10 concurrent calls
	assert.Equal(t, int32(1), callCount.Load())
}

func TestChecker_ConcurrentRequests_AfterExpiry(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		time.Sleep(30 * time.Millisecond)
		release := githubRelease{TagName: "cli/v0.11.0"}
		json.NewEncoder(w).Encode(release)
	}))
	defer server.Close()

	c := NewChecker(
		WithHTTPClient(server.Client()),
		WithCacheTTL(50*time.Millisecond),
	).(*checker)
	c.githubClient.cliReleasesURL = server.URL

	// First fetch
	c.GetLatestCLIVersion()
	assert.Equal(t, int32(1), callCount.Load())

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	// Multiple concurrent requests after expiry
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.GetLatestCLIVersion()
		}()
	}
	wg.Wait()

	// Should have made only 2 requests total (initial + one after expiry)
	assert.Equal(t, int32(2), callCount.Load())
}

func TestChecker_GetLatestVersions(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cli", func(w http.ResponseWriter, r *http.Request) {
		release := githubRelease{TagName: "cli/v0.11.0"}
		json.NewEncoder(w).Encode(release)
	})
	mux.HandleFunc("/helm", func(w http.ResponseWriter, r *http.Request) {
		releases := []githubRelease{
			{TagName: "kubetail-0.17.0", PublishedAt: "2026-01-15T06:05:16Z"},
		}
		json.NewEncoder(w).Encode(releases)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewChecker(WithHTTPClient(server.Client())).(*checker)
	c.githubClient.cliReleasesURL = server.URL + "/cli"
	c.githubClient.helmChartsReleasesURL = server.URL + "/helm"

	result := c.GetLatestVersions()

	require.NotNil(t, result.CLI)
	require.NotNil(t, result.HelmChart)
	assert.Equal(t, "cli/v0.11.0", result.CLI.Version)
	assert.Equal(t, "kubetail-0.17.0", result.HelmChart.Version)
}

func TestWithOptions(t *testing.T) {
	t.Run("WithCacheTTL", func(t *testing.T) {
		c := NewChecker(WithCacheTTL(1 * time.Hour)).(*checker)
		assert.Equal(t, 1*time.Hour, c.cacheTTL)
	})

	t.Run("WithHTTPClient", func(t *testing.T) {
		customClient := &http.Client{Timeout: 30 * time.Second}
		c := NewChecker(WithHTTPClient(customClient)).(*checker)
		assert.Equal(t, customClient, c.githubClient.httpClient)
	})

	t.Run("DefaultValues", func(t *testing.T) {
		c := NewChecker().(*checker)
		assert.Equal(t, DefaultCacheTTL, c.cacheTTL)
		assert.NotNil(t, c.githubClient.httpClient)
	})
}
