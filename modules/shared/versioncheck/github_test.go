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

	"github.com/stretchr/testify/assert"
)

func TestGithubClient_FetchLatestCLIVersion_EmptyTagName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		release := githubRelease{TagName: ""}
		json.NewEncoder(w).Encode(release)
	}))
	defer server.Close()

	c := NewChecker(WithHTTPClient(server.Client())).(*checker)
	c.githubClient.cliReleasesURL = server.URL

	info := c.GetLatestCLIVersion()

	assert.Empty(t, info.Version)
	assert.NotNil(t, info.Error)
	assert.Contains(t, info.Error.Error(), "tag_name is empty")
}

func TestGithubClient_FetchLatestCLIVersion_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	c := NewChecker(WithHTTPClient(server.Client())).(*checker)
	c.githubClient.cliReleasesURL = server.URL

	info := c.GetLatestCLIVersion()

	assert.Empty(t, info.Version)
	assert.NotNil(t, info.Error)
}

func TestGithubClient_FetchLatestHelmChartVersion_FiltersDraftAndPrerelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		releases := []githubRelease{
			{TagName: "kubetail-0.19.0", Draft: true, PublishedAt: "2024-01-05T00:00:00Z"},
			{TagName: "kubetail-0.18.0", Prerelease: true, PublishedAt: "2024-01-04T00:00:00Z"},
			{TagName: "kubetail-0.17.0", Draft: false, Prerelease: false, PublishedAt: "2024-01-01T00:00:00Z"},
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

func TestGithubClient_FetchLatestHelmChartVersion_FiltersNonKubetailPrefix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		releases := []githubRelease{
			{TagName: "other-chart-2.0.0", PublishedAt: "2024-01-03T00:00:00Z"},
			{TagName: "v1.5.0", PublishedAt: "2024-01-02T00:00:00Z"},
			{TagName: "kubetail-0.17.0", PublishedAt: "2024-01-01T00:00:00Z"},
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

func TestGithubClient_FetchLatestHelmChartVersion_PicksLatestByPublishedAt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		releases := []githubRelease{
			{TagName: "kubetail-0.16.0", PublishedAt: "2024-01-01T00:00:00Z"},
			{TagName: "kubetail-0.17.0", PublishedAt: "2024-01-03T00:00:00Z"},
			{TagName: "kubetail-0.15.0", PublishedAt: "2024-01-02T00:00:00Z"},
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

func TestGithubClient_FetchLatestHelmChartVersion_NoValidReleases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		releases := []githubRelease{
			// draft
			{TagName: "kubetail-0.17.0", Draft: true, PublishedAt: "2024-01-01T00:00:00Z"},
			// non-kubetail prefix
			{TagName: "other-0.17.0", PublishedAt: "2024-01-01T00:00:00Z"},
		}
		json.NewEncoder(w).Encode(releases)
	}))
	defer server.Close()

	c := NewChecker(WithHTTPClient(server.Client())).(*checker)
	c.githubClient.helmChartsReleasesURL = server.URL

	info := c.GetLatestHelmChartVersion()

	assert.Empty(t, info.Version)
	assert.NotNil(t, info.Error)
	assert.Contains(t, info.Error.Error(), "no valid releases found")
}

func TestGithubClient_FetchLatestHelmChartVersion_EmptyList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]githubRelease{})
	}))
	defer server.Close()

	c := NewChecker(WithHTTPClient(server.Client())).(*checker)
	c.githubClient.helmChartsReleasesURL = server.URL

	info := c.GetLatestHelmChartVersion()

	assert.Empty(t, info.Version)
	assert.NotNil(t, info.Error)
}

func TestGithubClient_FetchLatestHelmChartVersion_InvalidPublishedAt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		releases := []githubRelease{
			{TagName: "kubetail-0.18.0", PublishedAt: "invalid-date"},
			{TagName: "kubetail-0.17.0", PublishedAt: "2024-01-01T00:00:00Z"},
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
