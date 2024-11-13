// Copyright 2024 Andres Morey
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

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/repo"
)

// Target repository name and URL
const (
	targetRepoURL = "https://kubetail-org.github.io/helm-charts/"

	defaultRepoName    = "kubetail"
	defaultChartName   = "kubetail"
	defaultReleaseName = "kubetail"
	defaultNamespace   = "kubetail-system"
)

func noopLogger(format string, v ...interface{}) {}

// ensureHelmEnv initializes the Helm environment, creating necessary files and directories if needed.
func ensureHelmEnv() error {
	settings := cli.New()

	// Ensure the repository configuration file exists
	repoFile := settings.RepositoryConfig
	if err := ensureRepoFileExists(repoFile); err != nil {
		return err
	}

	/*
		// Ensure Helm cache directory exists
		helmCacheHome := settings.RepositoryCache
		if err := ensureDirExists(helmCacheHome); err != nil {
			return fmt.Errorf("failed to set up Helm cache directory: %v", err)
		}
	*/
	return nil
}

/*
// Ensure that a directory exists, creating it if necessary.
func ensureDirExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(path, os.ModePerm); err != nil {
			return fmt.Errorf("failed to create directory '%s': %v", path, err)
		}
	}
	return nil
}
*/

// Ensure the Helm repository configuration file exists
func ensureRepoFileExists(repoFile string) error {
	// Check if the repositories.yaml file exists
	if _, err := os.Stat(repoFile); os.IsNotExist(err) {
		// Create the necessary directories if they don’t exist
		if err := os.MkdirAll(filepath.Dir(repoFile), os.ModePerm); err != nil {
			return fmt.Errorf("failed to create directories for repo file: %v", err)
		}

		// Create an empty repositories file
		f, err := os.Create(repoFile)
		if err != nil {
			return fmt.Errorf("failed to create repo file: %v", err)
		}
		defer f.Close()

		// Write an empty repository configuration
		emptyRepoFile := &repo.File{}
		if err := emptyRepoFile.WriteFile(repoFile, os.ModePerm); err != nil {
			return fmt.Errorf("failed to write empty repo file: %v", err)
		}
	}

	return nil
}

// EnsureRepo checks if the target repository exists, and if not, it adds it.
func EnsureRepo() (string, error) {
	if err := ensureHelmEnv(); err != nil {
		return "", err
	}

	// Initialize Helm settings
	settings := cli.New()

	// Load repository configuration file
	repoFile := settings.RepositoryConfig
	r, err := repo.LoadFile(repoFile)
	if err != nil {
		return "", fmt.Errorf("failed to load repository file: %v", err)
	}

	// Check if the repository already exists
	for _, cfg := range r.Repositories {
		if cfg.URL == targetRepoURL {
			fmt.Println(cfg)
			return cfg.Name, nil
		}
	}

	// Create a new repository entry
	entry := &repo.Entry{
		Name: defaultRepoName,
		URL:  targetRepoURL,
	}

	// Initialize the repository
	newRepo, err := repo.NewChartRepository(entry, getter.All(settings))
	if err != nil {
		return "", fmt.Errorf("failed to create repository: %v", err)
	}

	// Download the index file to verify the repository
	_, err = newRepo.DownloadIndexFile()
	if err != nil {
		return "", fmt.Errorf("failed to download index file for repo '%s': %v", defaultRepoName, err)
	}

	// Update the repository list and save it to the configuration file
	r.Update(entry)
	if err := r.WriteFile(repoFile, os.ModePerm); err != nil {
		return "", fmt.Errorf("failed to write repository file: %v", err)
	}

	return defaultRepoName, nil
}

// Function to install the latest version of a chart
func InstallLatest(repoName string) (*release.Release, error) {
	// Ensure Helm settings and action configuration
	settings := cli.New()
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(settings.RESTClientGetter(), defaultNamespace, os.Getenv("HELM_DRIVER"), noopLogger); err != nil {
		return nil, fmt.Errorf("failed to initialize Helm action configuration: %v", err)
	}

	// Create an install action
	install := action.NewInstall(actionConfig)
	install.ReleaseName = defaultReleaseName
	install.Namespace = defaultNamespace
	install.CreateNamespace = true

	// Get the latest version of the chart
	chartID := fmt.Sprintf("%s/%s", repoName, defaultChartName)
	chartPath, err := install.ChartPathOptions.LocateChart(chartID, settings)
	fmt.Println(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to locate chart '%s': %v", chartID, err)
	}

	// Load the chart
	chart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart from path '%s': %v", chartPath, err)
	}

	// Install the chart
	release, err := install.Run(chart, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to install chart '%s': %v", defaultChartName, err)
	}

	return release, nil
}

// UpgradeRelease upgrades a Helm release to a new chart version.
func UpgradeRelease() (*release.Release, error) {
	repoName := defaultRepoName
	releaseName := defaultReleaseName
	chartName := defaultChartName
	namespace := defaultNamespace

	settings := cli.New()
	actionConfig := new(action.Configuration)

	// Initialize Helm action configuration
	if err := actionConfig.Init(settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER"), noopLogger); err != nil {
		return nil, fmt.Errorf("failed to initialize Helm action config: %w", err)
	}

	// Create upgrade action
	upgrade := action.NewUpgrade(actionConfig)
	upgrade.Namespace = namespace

	// Get the latest version of the chart
	chartID := fmt.Sprintf("%s/%s", repoName, chartName)
	chartPath, err := upgrade.ChartPathOptions.LocateChart(chartID, settings)
	if err != nil {
		return nil, fmt.Errorf("failed to locate chart '%s': %v", chartID, err)
	}

	// Load the chart
	chart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart: %w", err)
	}

	// Run upgrade
	release, err := upgrade.Run(releaseName, chart, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to upgrade release %s: %w", releaseName, err)
	}

	return release, nil
}

// ListReleases lists all releases of a specific chart across all namespaces.
func ListReleases() ([]*release.Release, error) {
	chartName := defaultChartName

	settings := cli.New()
	actionConfig := new(action.Configuration)

	// Initialize action configuration with an empty namespace to search all namespaces
	if err := actionConfig.Init(settings.RESTClientGetter(), "", os.Getenv("HELM_DRIVER"), noopLogger); err != nil {
		return nil, fmt.Errorf("failed to initialize Helm action config: %w", err)
	}

	list := action.NewList(actionConfig)
	list.AllNamespaces = true                    // Enable search across all namespaces
	list.Filter = fmt.Sprintf("^%s$", chartName) // Set filter for specific chart name
	list.Deployed = true                         // List only deployed releases (you can add other statuses if needed)

	// Run the list action
	releases, err := list.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to list releases: %w", err)
	}

	// Filter releases by chart name
	var filteredReleases []*release.Release
	for _, rel := range releases {
		if strings.HasPrefix(rel.Chart.Metadata.Name, chartName) {
			filteredReleases = append(filteredReleases, rel)
		}
	}

	return filteredReleases, nil
}

// UninstallRelease uninstalls a Helm release
func UninstallRelease() (*release.UninstallReleaseResponse, error) {
	releaseName := defaultReleaseName
	namespace := defaultNamespace

	actionConfig := new(action.Configuration)
	settings := cli.New()

	// Initialize Helm action configuration
	if err := actionConfig.Init(settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER"), noopLogger); err != nil {
		return nil, fmt.Errorf("failed to initialize Helm action config: %w", err)
	}

	uninstall := action.NewUninstall(actionConfig)

	// Uninstall the release
	response, err := uninstall.Run(releaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to uninstall release %s: %w", releaseName, err)
	}

	return response, nil
}

// AddHRepo adds a new Helm repository with the given name and URL.
func AddRepo() error {
	repoName := defaultRepoName
	repoURL := targetRepoURL

	settings := cli.New()

	// Load the Helm repository file
	repoFile := settings.RepositoryConfig
	repoFileContent, err := repo.LoadFile(repoFile)
	if err != nil {
		return fmt.Errorf("failed to load repository file: %w", err)
	}

	// Check if the repository already exists
	for _, r := range repoFileContent.Repositories {
		if r.Name == repoName {
			return fmt.Errorf("repository %s already exists", repoName)
		}
	}

	// Define the new repository entry
	newEntry := &repo.Entry{
		Name: repoName,
		URL:  repoURL,
	}

	// Initialize the new repository
	chartRepo, err := repo.NewChartRepository(newEntry, getter.All(settings))
	if err != nil {
		return fmt.Errorf("failed to initialize chart repository: %w", err)
	}
	chartRepo.CachePath = settings.RepositoryCache

	// Download the repository index to verify it’s accessible
	if _, err := chartRepo.DownloadIndexFile(); err != nil {
		return fmt.Errorf("failed to reach repository %s at %s: %w", repoName, repoURL, err)
	}

	// Add the repository to the repo file content and save it
	repoFileContent.Repositories = append(repoFileContent.Repositories, newEntry)
	if err := repoFileContent.WriteFile(repoFile, os.FileMode(0644)); err != nil {
		return fmt.Errorf("failed to save repository file: %w", err)
	}

	return nil
}

// UpdateRepo updates the Helm repository with the given name.
func UpdateRepo() error {
	repoName := defaultRepoName

	settings := cli.New()

	// Load repositories from the Helm repository file
	repoFile := settings.RepositoryConfig
	repoCache := settings.RepositoryCache

	// Read the repository file
	repoFileContent, err := repo.LoadFile(repoFile)
	if err != nil {
		return fmt.Errorf("failed to load repository file: %w", err)
	}

	// Find the repository entry
	var entry *repo.Entry
	for _, r := range repoFileContent.Repositories {
		if r.Name == repoName {
			entry = r
			break
		}
	}

	if entry == nil {
		return fmt.Errorf("repository %s not found", repoName)
	}

	// Set up the repository chart
	chartRepo, err := repo.NewChartRepository(entry, getter.All(settings))
	if err != nil {
		return fmt.Errorf("failed to initialize chart repository: %w", err)
	}
	chartRepo.CachePath = repoCache

	// Download the latest index file for the repository
	if _, err := chartRepo.DownloadIndexFile(); err != nil {
		return fmt.Errorf("failed to update repository %s: %w", repoName, err)
	}

	return nil
}

// RemoveHelmRepo removes the Helm repository with the given name.
func RemoveRepo() error {
	repoName := defaultRepoName

	settings := cli.New()

	// Load the Helm repository file
	repoFile := settings.RepositoryConfig
	repoFileContent, err := repo.LoadFile(repoFile)
	if err != nil {
		return fmt.Errorf("failed to load repository file: %w", err)
	}

	// Check if the repository exists
	repoIndex := -1
	for i, r := range repoFileContent.Repositories {
		if r.Name == repoName {
			repoIndex = i
			break
		}
	}

	if repoIndex == -1 {
		return fmt.Errorf("repository %s not found", repoName)
	}

	// Remove the repository entry
	repoFileContent.Repositories = append(repoFileContent.Repositories[:repoIndex], repoFileContent.Repositories[repoIndex+1:]...)

	// Save the updated repository file
	if err := repoFileContent.WriteFile(repoFile, os.FileMode(0644)); err != nil {
		return fmt.Errorf("failed to save updated repository file: %w", err)
	}

	return nil
}
