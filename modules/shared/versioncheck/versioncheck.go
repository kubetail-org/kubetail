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
)

const (
	defaultTimeout = 10 * time.Second
)

type VersionInfo struct {
	Version string
}

type Checker interface {
	GetLatestCLIVersion() (*VersionInfo, error)
	GetLatestHelmChartVersion() (*VersionInfo, error)
}

type checker struct {
	githubClient *githubClient
}

type CheckerOption func(*checker)

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
	}

	for _, opt := range options {
		opt(c)
	}

	return c
}

func (c *checker) GetLatestCLIVersion() (*VersionInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	version, err := c.githubClient.fetchLatestCLIVersion(ctx)
	if err != nil {
		return nil, err
	}

	return &VersionInfo{Version: version}, nil
}

func (c *checker) GetLatestHelmChartVersion() (*VersionInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	version, err := c.githubClient.fetchLatestHelmChartVersion(ctx)
	if err != nil {
		return nil, err
	}

	return &VersionInfo{Version: version}, nil
}
