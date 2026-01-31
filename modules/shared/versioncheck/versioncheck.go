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
	"net/http"
	"time"

	zlog "github.com/rs/zerolog/log"

	"github.com/kubetail-org/kubetail/modules/shared/util"
)

const (
	DefaultCacheTTL = 12 * time.Hour
	defaultTimeout  = 10 * time.Second
)

type Component string

const (
	ComponentCLI       Component = "cli"
	ComponentHelmChart Component = "helm-chart"
)

type VersionInfo struct {
	Version     string
	LastChecked time.Time
	Error       error
}

type LatestVersions struct {
	CLI       *VersionInfo
	HelmChart *VersionInfo
}

type Checker interface {
	GetLatestCLIVersion() *VersionInfo
	GetLatestHelmChartVersion() *VersionInfo
	GetLatestVersions() *LatestVersions
}

type cacheEntry struct {
	version     string
	lastChecked time.Time
	expiration  time.Time
}

type checker struct {
	githubClient *githubClient
	cache        util.SyncGroup[Component, cacheEntry]
	cacheTTL     time.Duration
}

type CheckerOption func(*checker)

func WithCacheTTL(ttl time.Duration) CheckerOption {
	return func(c *checker) {
		c.cacheTTL = ttl
	}
}

func WithHTTPClient(client *http.Client) CheckerOption {
	return func(c *checker) {
		c.githubClient.httpClient = client
	}
}

func NewChecker(options ...CheckerOption) Checker {
	c := &checker{
		githubClient: &githubClient{
			httpClient: &http.Client{
				Timeout: defaultTimeout,
			},
			userAgent: "kubetail-version-checker",
		},
		cache:    util.SyncGroup[Component, cacheEntry]{},
		cacheTTL: DefaultCacheTTL,
	}

	for _, opt := range options {
		opt(c)
	}

	return c
}

func (c *checker) GetLatestCLIVersion() *VersionInfo {
	info := &VersionInfo{}

	version, lastChecked, err := c.getLatestVersion(ComponentCLI, c.githubClient.fetchLatestCLIVersion)
	info.LastChecked = lastChecked
	if err != nil {
		zlog.Debug().Err(err).Msg("Failed to get latest CLI version")
		info.Error = err
		return info
	}

	info.Version = version
	return info
}

func (c *checker) GetLatestHelmChartVersion() *VersionInfo {
	info := &VersionInfo{}

	version, lastChecked, err := c.getLatestVersion(ComponentHelmChart, c.githubClient.fetchLatestHelmChartVersion)
	info.LastChecked = lastChecked
	if err != nil {
		zlog.Debug().Err(err).Msg("Failed to get latest Helm chart version")
		info.Error = err
		return info
	}

	info.Version = version
	return info
}

func (c *checker) GetLatestVersions() *LatestVersions {
	return &LatestVersions{
		CLI:       c.GetLatestCLIVersion(),
		HelmChart: c.GetLatestHelmChartVersion(),
	}
}

func (c *checker) getLatestVersion(component Component, fetchFunc func(context.Context) (string, error)) (string, time.Time, error) {
	// check cache first
	if entry, ok := c.cache.Load(component); ok {
		if time.Now().Before(entry.expiration) {
			return entry.version, entry.lastChecked, nil
		}
		// expired, delete to allow re-computation
		c.cache.Delete(component)
	}

	entry, _, err := c.cache.LoadOrCompute(component, func() (cacheEntry, error) {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()

		version, err := fetchFunc(ctx)
		if err != nil {
			return cacheEntry{}, err
		}

		now := time.Now()
		return cacheEntry{
			version:     version,
			lastChecked: now,
			expiration:  now.Add(c.cacheTTL),
		}, nil
	})

	if err != nil {
		return "", time.Time{}, err
	}

	return entry.version, entry.lastChecked, nil
}
