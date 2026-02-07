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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var helmChartTagRegex = regexp.MustCompile(`^kubetail-(\d+\.\d+\.\d+)$`)

const (
	cliReleasesURL        = "https://api.github.com/repos/kubetail-org/kubetail/releases/latest"
	helmChartsReleasesURL = "https://api.github.com/repos/kubetail-org/helm-charts/releases"
)

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	PublishedAt string `json:"published_at"`
}

type githubClient struct {
	httpClient            *http.Client
	userAgent             string
	cliReleasesURL        string
	helmChartsReleasesURL string
}

func (g *githubClient) getCLIReleasesURL() string {
	if g.cliReleasesURL != "" {
		return g.cliReleasesURL
	}
	return cliReleasesURL
}

func (g *githubClient) getHelmChartsReleasesURL() string {
	if g.helmChartsReleasesURL != "" {
		return g.helmChartsReleasesURL
	}
	return helmChartsReleasesURL
}

func (g *githubClient) fetchLatestCLIVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.getCLIReleasesURL(), nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if g.userAgent != "" {
		req.Header.Set("User-Agent", g.userAgent)
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}

	if release.TagName == "" {
		return "", fmt.Errorf("release tag_name is empty")
	}

	version, err := parseCLITag(release.TagName)
	if err != nil {
		return "", err
	}

	return version, nil
}

// parseCLITag extracts a semantic version from a CLI tag name.
// Expected format: "cli/vX.Y.Z"
func parseCLITag(tag string) (string, error) {
	if !strings.HasPrefix(tag, "cli/v") {
		return "", fmt.Errorf("unexpected tag_name format: %s", tag)
	}
	version := strings.TrimPrefix(tag, "cli/v")
	return version, nil
}

func (g *githubClient) fetchLatestHelmChartVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.getHelmChartsReleasesURL(), nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if g.userAgent != "" {
		req.Header.Set("User-Agent", g.userAgent)
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", err
	}

	var latestVersion string
	var latestTime time.Time

	for _, r := range releases {
		if r.Draft || r.Prerelease {
			continue
		}

		if r.TagName == "" {
			continue
		}

		version := parseHelmChartTag(r.TagName)
		if version == "" {
			continue
		}

		publishedAt, err := time.Parse(time.RFC3339, r.PublishedAt)
		if err != nil {
			continue
		}

		if latestVersion == "" || publishedAt.After(latestTime) {
			latestVersion = version
			latestTime = publishedAt
		}
	}

	if latestVersion == "" {
		return "", fmt.Errorf("no valid helm chart release found")
	}

	return latestVersion, nil
}

// parseHelmChartTag extracts a semantic version from a Helm chart tag name.
// Expected format: "kubetail-X.Y.Z" (returns "" if not matched).
func parseHelmChartTag(tag string) string {
	matches := helmChartTagRegex.FindStringSubmatch(tag)
	if len(matches) != 2 {
		return ""
	}
	return matches[1]
}
