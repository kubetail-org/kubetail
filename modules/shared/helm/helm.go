// Copyright 2024-2026 The Kubetail Authors
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

package helm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/repo"
)

// Target repo and chart values
const (
	targetRepoURL   = "https://kubetail-org.github.io/helm-charts/"
	targetRepoName  = "kubetail"
	targetChartName = "kubetail"
)

// Default values
const (
	DefaultReleaseName = "kubetail"
	DefaultNamespace   = "kubetail-system"
)

// Chart version constraint
const chartSemverConstraint = ">= 0.9.0"

func noopLogger(format string, v ...interface{}) {}

// Client
type Client struct {
	*cli.EnvSettings
}

// InstallLatest creates a new release from the latest chart
func (c *Client) InstallLatest(namespace, releaseName string) (*release.Release, error) {
	// Init action config
	actionConfig, err := c.newActionConfig(namespace)
	if err != nil {
		return nil, err
	}

	// Create an install action
	install := action.NewInstall(actionConfig)
	install.ReleaseName = releaseName
	install.Namespace = namespace
	install.CreateNamespace = true

	// Get chart
	chart, err := c.getChart(install.ChartPathOptions)
	if err != nil {
		if strings.Contains(err.Error(), fmt.Sprintf("repo %s not found", targetRepoName)) {
			// Add repo
			if err := c.AddRepo(); err != nil {
				return nil, err
			}

			// Get chart again
			chart, err = c.getChart(install.ChartPathOptions)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	// Check semver constraints
	constraint, err := semver.NewConstraint(chartSemverConstraint)
	if err != nil {
		return nil, err
	}

	v, err := semver.NewVersion(chart.Metadata.Version)
	if err != nil {
		return nil, err
	}

	if !constraint.Check(v) {
		return nil, fmt.Errorf("requires chart version %s", chartSemverConstraint)
	}

	// Exclude dashboard
	vals := map[string]interface{}{
		"kubetail": map[string]interface{}{
			"dashboard": map[string]interface{}{
				"enabled": false,
			},
		},
	}

	// Install the chart
	release, err := install.Run(chart, vals)
	if err != nil {
		return nil, fmt.Errorf("failed to install chart '%s': %v", targetChartName, err)
	}

	return release, nil
}

// UpgradeRelease upgrades an existing release
func (c *Client) UpgradeRelease(namespace, releaseName string) (*release.Release, error) {
	// Init action config
	actionConfig, err := c.newActionConfig(namespace)
	if err != nil {
		return nil, err
	}

	// Create upgrade action
	upgrade := action.NewUpgrade(actionConfig)
	upgrade.Namespace = namespace

	// Get chart
	chart, err := c.getChart(upgrade.ChartPathOptions)
	if err != nil {
		return nil, err
	}

	// Run upgrade
	release, err := upgrade.Run(releaseName, chart, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to upgrade release %s: %w", releaseName, err)
	}

	return release, nil
}

// UninstallRelease uninstalls a release
func (c *Client) UninstallRelease(namespace, releaseName string) (*release.UninstallReleaseResponse, error) {
	// Init action config
	actionConfig, err := c.newActionConfig(namespace)
	if err != nil {
		return nil, err
	}

	// Create uninstall action
	uninstall := action.NewUninstall(actionConfig)

	// Run uninstall
	response, err := uninstall.Run(releaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to uninstall release %s: %w", releaseName, err)
	}

	return response, nil
}

// ListReleases lists all releases across all namespaces.
func (c *Client) ListReleases() ([]*release.Release, error) {
	// Init action config
	actionConfig, err := c.newActionConfig("")
	if err != nil {
		return nil, err
	}

	// New list action
	list := action.NewList(actionConfig)
	list.AllNamespaces = true // Enable search across all namespaces
	list.Limit = 0

	// Run the list action
	releases, err := list.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to list releases: %w", err)
	}

	// Filter releases
	var filteredReleases []*release.Release
	for _, r := range releases {
		if r.Chart != nil && strings.HasPrefix(r.Chart.Metadata.Name, targetChartName) {
			filteredReleases = append(filteredReleases, r)
		}
	}

	return filteredReleases, nil
}

// AddRepo adds the repository
func (c *Client) AddRepo() error {
	// Ensure helm environment
	if err := c.ensureEnv(); err != nil {
		return err
	}

	repoFile := c.RepositoryConfig

	// Load the Helm repository file
	repoFileContent, err := repo.LoadFile(repoFile)
	if err != nil {
		return fmt.Errorf("failed to load repository file: %w", err)
	}

	// Check if the repository already exists
	for _, r := range repoFileContent.Repositories {
		if r.Name == targetRepoName {
			return fmt.Errorf("repository '%s' already exists", targetRepoName)
		}
	}

	// Define the new repository entry
	newEntry := &repo.Entry{
		Name: targetRepoName,
		URL:  targetRepoURL,
	}

	// Initialize the new repository
	chartRepo, err := repo.NewChartRepository(newEntry, getter.All(c.EnvSettings))
	if err != nil {
		return fmt.Errorf("failed to initialize chart repository: %w", err)
	}
	chartRepo.CachePath = c.RepositoryCache

	// Download the repository index to verify it’s accessible
	if _, err := chartRepo.DownloadIndexFile(); err != nil {
		return fmt.Errorf("failed to reach repository %s at %s: %w", targetRepoName, targetRepoURL, err)
	}

	// Add the repository to the repo file content and save it
	repoFileContent.Repositories = append(repoFileContent.Repositories, newEntry)
	if err := repoFileContent.WriteFile(repoFile, os.ModePerm); err != nil {
		return fmt.Errorf("failed to save repository file: %w", err)
	}

	return nil
}

// UpdateRepo updates the repository
func (c *Client) UpdateRepo() error {
	repoFile := c.RepositoryConfig

	// Read the repository file
	repoFileContent, err := repo.LoadFile(repoFile)
	if err != nil {
		return fmt.Errorf("repository '%s' not found", targetRepoName)
	}

	// Find the repository entry
	var entry *repo.Entry
	for _, r := range repoFileContent.Repositories {
		if r.Name == targetRepoName {
			entry = r
			break
		}
	}

	if entry == nil {
		return fmt.Errorf("repository '%s' not found", targetRepoName)
	}

	// Set up the repository chart
	chartRepo, err := repo.NewChartRepository(entry, getter.All(c.EnvSettings))
	if err != nil {
		return fmt.Errorf("failed to initialize chart repository: %w", err)
	}
	chartRepo.CachePath = c.RepositoryCache

	// Download the latest index file for the repository
	if _, err := chartRepo.DownloadIndexFile(); err != nil {
		return fmt.Errorf("failed to update repository '%s': %w", targetRepoName, err)
	}

	return nil
}

// RemoveRepo removes the repository
func (c *Client) RemoveRepo() error {
	repoFile := c.RepositoryConfig

	// Load the Helm repository file
	repoFileContent, err := repo.LoadFile(repoFile)
	if err != nil {
		return fmt.Errorf("repository '%s' not found", targetRepoName)
	}

	// Check if the repository exists
	repoIndex := -1
	for i, r := range repoFileContent.Repositories {
		if r.Name == targetRepoName {
			repoIndex = i
			break
		}
	}

	if repoIndex == -1 {
		return fmt.Errorf("repository '%s' not found", targetRepoName)
	}

	// Remove the repository entry
	repoFileContent.Repositories = append(repoFileContent.Repositories[:repoIndex], repoFileContent.Repositories[repoIndex+1:]...)

	// Get current repository file mode
	fileInfo, err := os.Stat(repoFile)
	if err != nil {
		return fmt.Errorf("failed to read repository file: %w", err)
	}

	// Save the updated repository file
	if err := repoFileContent.WriteFile(repoFile, fileInfo.Mode()); err != nil {
		return fmt.Errorf("failed to save updated repository file: %w", err)
	}

	return nil
}

// newActionConfig
func (c *Client) newActionConfig(namespace string) (*action.Configuration, error) {
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(c.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER"), noopLogger); err != nil {
		return nil, fmt.Errorf("failed to initialize Helm action configuration: %v", err)
	}
	return actionConfig, nil
}

// ensureEnv ensures helm environment is up
func (c *Client) ensureEnv() error {
	repoFile := c.RepositoryConfig

	// Check if the repositories.yaml file exists
	if _, err := os.Stat(repoFile); os.IsNotExist(err) {
		// Create the necessary directories if they don’t exist
		if err := os.MkdirAll(filepath.Dir(repoFile), os.ModePerm); err != nil {
			return fmt.Errorf("failed to create directories for repo file: %w", err)
		}

		// Create an empty repositories file
		f, err := os.Create(repoFile)
		if err != nil {
			return fmt.Errorf("failed to create repo file: %w", err)
		}
		defer f.Close()

		// Write an empty repository configuration
		emptyRepoFile := &repo.File{}
		if err := emptyRepoFile.WriteFile(repoFile, os.ModePerm); err != nil {
			return fmt.Errorf("failed to write empty repo file: %w", err)
		}
	}

	return nil
}

// getChart returns the kubetail chart
func (c *Client) getChart(pathOptions action.ChartPathOptions) (*chart.Chart, error) {
	// Get the latest version of the chart
	chartID := fmt.Sprintf("%s/%s", targetRepoName, targetChartName)
	chartPath, err := pathOptions.LocateChart(chartID, c.EnvSettings)
	if err != nil {
		return nil, fmt.Errorf("failed to locate chart '%s': %w", chartID, err)
	}

	// Load the chart
	chart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart: %w", err)
	}

	return chart, err
}

type ClientOption func(c *Client)

// Return new client
func NewClient(options ...ClientOption) *Client {
	c := &Client{cli.New()}
	for _, option := range options {
		option(c)
	}
	return c
}

// Option KubeconfigPath
func WithKubeconfigPath(kubeconfigPath string) ClientOption {
	return func(c *Client) {
		c.EnvSettings.KubeConfig = kubeconfigPath
	}

}

// Option KubeContext
func WithKubeContext(kubeContext string) ClientOption {
	return func(c *Client) {
		c.KubeContext = kubeContext
	}
}
