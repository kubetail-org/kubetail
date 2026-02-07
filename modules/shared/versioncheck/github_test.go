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

	info, err := c.GetLatestCLIVersion()

	assert.Nil(t, info)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tag_name is empty")
}

func TestGithubClient_FetchLatestCLIVersion_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	c := NewChecker(WithHTTPClient(server.Client())).(*checker)
	c.githubClient.cliReleasesURL = server.URL

	info, err := c.GetLatestCLIVersion()

	assert.Nil(t, info)
	assert.Error(t, err)
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

	info, err := c.GetLatestHelmChartVersion()

	assert.NoError(t, err)
	assert.Equal(t, "0.17.0", info.Version)
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

	info, err := c.GetLatestHelmChartVersion()

	assert.NoError(t, err)
	assert.Equal(t, "0.17.0", info.Version)
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

	info, err := c.GetLatestHelmChartVersion()

	assert.NoError(t, err)
	assert.Equal(t, "0.17.0", info.Version)
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

	info, err := c.GetLatestHelmChartVersion()

	assert.Nil(t, info)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no valid helm chart release found")
}

func TestGithubClient_FetchLatestHelmChartVersion_EmptyList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]githubRelease{})
	}))
	defer server.Close()

	c := NewChecker(WithHTTPClient(server.Client())).(*checker)
	c.githubClient.helmChartsReleasesURL = server.URL

	info, err := c.GetLatestHelmChartVersion()

	assert.Nil(t, info)
	assert.Error(t, err)
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

	info, err := c.GetLatestHelmChartVersion()

	assert.NoError(t, err)
	assert.Equal(t, "0.17.0", info.Version)
}

func TestParseCLITag(t *testing.T) {
	tests := []struct {
		name    string
		tag     string
		want    string
		wantErr bool
	}{
		{"valid tag", "cli/v0.11.1", "0.11.1", false},
		{"invalid tag", "cli/v", "", true},
		{"missing cli prefix", "v0.11.1", "", true},
		{"no prefix at all", "0.11.1", "", true},
		{"empty string", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCLITag(tt.tag)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestParseHelmChartTag(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		want string
	}{
		{"valid tag", "kubetail-0.17.0", "0.17.0"},
		{"pre-release suffix", "kubetail-0.17.0-rc1", ""},
		{"other chart", "other-chart-1.0.0", ""},
		{"missing version", "kubetail-", ""},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseHelmChartTag(tt.tag)
			assert.Equal(t, tt.want, got)
		})
	}
}
