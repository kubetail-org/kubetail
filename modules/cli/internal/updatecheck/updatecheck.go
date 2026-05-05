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

package updatecheck

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	semver "github.com/Masterminds/semver/v3"
	helmrelease "helm.sh/helm/v3/pkg/release"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kubetail-org/kubetail/modules/cli/pkg/config"
	"github.com/kubetail-org/kubetail/modules/shared/helm"
	"github.com/kubetail-org/kubetail/modules/shared/versioncheck"
)

const (
	DefaultCacheTTL  = 12 * time.Hour
	cliCacheVersion  = 1
	helmCacheVersion = 1
)

// CLICache matches the UpdateState shape used in the dashboard-ui localStorage.
type CLICache struct {
	SchemaVersion   int      `json:"schemaVersion"`
	LatestVersion   string   `json:"latestVersion,omitempty"`
	FetchedAt       *int64   `json:"fetchedAt,omitempty"`
	DismissedAt     *int64   `json:"dismissedAt,omitempty"`
	SkippedVersions []string `json:"skippedVersions,omitempty"`
}

// ClusterCacheEntry stores the installed helm chart version for a specific kube context.
type ClusterCacheEntry struct {
	CurrentVersion  string   `json:"currentVersion,omitempty"`
	FetchedAt       *int64   `json:"fetchedAt,omitempty"`
	DismissedAt     *int64   `json:"dismissedAt,omitempty"`
	SkippedVersions []string `json:"skippedVersions,omitempty"`
}

// HelmCache stores the latest upstream helm chart version (shared across all contexts)
// and per-context installed versions in Clusters.
type HelmCache struct {
	SchemaVersion int                          `json:"schemaVersion"`
	LatestVersion string                       `json:"latestVersion,omitempty"`
	FetchedAt     *int64                       `json:"fetchedAt,omitempty"`
	Clusters      map[string]ClusterCacheEntry `json:"clusters,omitempty"`
}

type Notification struct {
	Message string
}

type helmLister interface {
	ListReleases() ([]*helmrelease.Release, error)
}

type Options struct {
	CLICacheFile      string
	HelmCacheFile     string
	CurrentCLIVersion string
	KubeconfigPath    string
	KubeContext       string
	CacheTTL          time.Duration
	Checker           versioncheck.Checker
	HelmLister        helmLister
	// Runner executes background refresh tasks. Defaults to a goroutine.
	// Tests can inject a synchronous runner to control execution.
	Runner func(func())
}

func defaultCacheFile(name string) (string, error) {
	dir, err := config.DefaultCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func DefaultCLICacheFile() (string, error) {
	return defaultCacheFile("cli-update-check.json")
}

func DefaultHelmCacheFile() (string, error) {
	return defaultCacheFile("cluster-update-check.json")
}

// TriggerRefreshIfStale checks if either cache is stale, fires background
// goroutines to refresh them, and returns a function that blocks until all
// launched goroutines complete. Call the returned function (with a deadline if
// needed) before the process exits to ensure the cache is written.
func TriggerRefreshIfStale(opts Options) func() {
	if opts.CurrentCLIVersion == "dev" {
		return func() {}
	}
	if opts.CacheTTL == 0 {
		opts.CacheTTL = DefaultCacheTTL
	}
	run := opts.Runner
	if run == nil {
		run = func(f func()) { go f() }
	}

	var wg sync.WaitGroup

	cliCache, _ := readCLICache(opts.CLICacheFile)
	var cliFetchedAt *int64
	if cliCache != nil {
		cliFetchedAt = cliCache.FetchedAt
	}
	if isStale(cliFetchedAt, opts.CacheTTL) {
		wg.Add(1)
		run(func() { defer wg.Done(); _ = RefreshCLICache(opts) })
	}

	helmCache, _ := readHelmCache(opts.HelmCacheFile)
	kubeContext := resolveKubeContext(opts.KubeconfigPath, opts.KubeContext)
	var latestHelmFetchedAt, clusterFetchedAt *int64
	if helmCache != nil {
		latestHelmFetchedAt = helmCache.FetchedAt
		if entry, ok := helmCache.Clusters[kubeContext]; ok {
			clusterFetchedAt = entry.FetchedAt
		}
	}
	if isStale(latestHelmFetchedAt, opts.CacheTTL) || isStale(clusterFetchedAt, opts.CacheTTL) {
		wg.Add(1)
		run(func() { defer wg.Done(); _ = RefreshHelmCache(opts) })
	}

	return wg.Wait
}

// Notify reads the cache files and returns any pending update notifications.
func Notify(opts Options) []Notification {
	if opts.CurrentCLIVersion == "dev" {
		return nil
	}
	cliCache, _ := readCLICache(opts.CLICacheFile)
	helmCache, _ := readHelmCache(opts.HelmCacheFile)
	kubeContext := resolveKubeContext(opts.KubeconfigPath, opts.KubeContext)
	return buildNotifications(cliCache, helmCache, opts.CurrentCLIVersion, kubeContext)
}

// RefreshCLICache fetches the latest CLI version and writes it to the CLI cache file.
func RefreshCLICache(opts Options) error {
	checker := opts.Checker
	if checker == nil {
		checker = versioncheck.NewChecker()
	}

	var latestVersion string
	if info, err := checker.GetLatestCLIVersion(); err == nil {
		latestVersion = info.Version
	}

	existing, _ := readCLICache(opts.CLICacheFile)
	fetchedAt := time.Now().UnixMilli()
	c := &CLICache{
		SchemaVersion: cliCacheVersion,
		LatestVersion: latestVersion,
		FetchedAt:     &fetchedAt,
	}
	if existing != nil {
		c.DismissedAt = existing.DismissedAt
		c.SkippedVersions = existing.SkippedVersions
	}

	return writeJSONCache(opts.CLICacheFile, c)
}

// RefreshHelmCache fetches the latest Helm chart version and the installed version
// for the current kube context, then writes the result to the Helm cache file.
func RefreshHelmCache(opts Options) error {
	checker := opts.Checker
	if checker == nil {
		checker = versioncheck.NewChecker()
	}

	kubeContext := resolveKubeContext(opts.KubeconfigPath, opts.KubeContext)

	var (
		latestVersion  string
		currentVersion string
		wg             sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		if info, err := checker.GetLatestHelmChartVersion(); err == nil {
			latestVersion = info.Version
		}
	}()
	go func() {
		defer wg.Done()
		lister := opts.HelmLister
		if lister == nil {
			lister = helm.NewClient(helm.WithKubeconfigPath(opts.KubeconfigPath), helm.WithKubeContext(kubeContext))
		}
		currentVersion = getInstalledHelmChartVersion(lister)
	}()
	wg.Wait()

	cache, _ := readHelmCache(opts.HelmCacheFile)
	if cache == nil {
		cache = &HelmCache{}
	}
	if cache.Clusters == nil {
		cache.Clusters = make(map[string]ClusterCacheEntry)
	}

	fetchedAt := time.Now().UnixMilli()
	cache.SchemaVersion = helmCacheVersion
	cache.LatestVersion = latestVersion
	cache.FetchedAt = &fetchedAt

	existingCluster := cache.Clusters[kubeContext]
	cache.Clusters[kubeContext] = ClusterCacheEntry{
		CurrentVersion:  currentVersion,
		FetchedAt:       &fetchedAt,
		DismissedAt:     existingCluster.DismissedAt,
		SkippedVersions: existingCluster.SkippedVersions,
	}

	return writeJSONCache(opts.HelmCacheFile, cache)
}

func readJSONFile[T any](path string) (*T, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func readCLICache(path string) (*CLICache, error) {
	c, err := readJSONFile[CLICache](path)
	if c != nil && c.SchemaVersion != cliCacheVersion {
		return nil, nil
	}
	return c, err
}

func readHelmCache(path string) (*HelmCache, error) {
	c, err := readJSONFile[HelmCache](path)
	if c != nil && c.SchemaVersion != helmCacheVersion {
		return nil, nil
	}
	return c, err
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".update-check-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func writeJSONCache(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}

func isStale(fetchedAt *int64, ttl time.Duration) bool {
	return fetchedAt == nil || time.Since(time.UnixMilli(*fetchedAt)) > ttl
}

func compareVersions(current, latest string) bool {
	if current == "" || latest == "" {
		return false
	}
	c, err := semver.NewVersion(current)
	if err != nil {
		return false
	}
	l, err := semver.NewVersion(latest)
	if err != nil {
		return false
	}
	return l.GreaterThan(c)
}

func buildNotifications(cliCache *CLICache, helmCache *HelmCache, currentCLIVersion, kubeContext string) []Notification {
	var notes []Notification

	if cliCache != nil && compareVersions(currentCLIVersion, cliCache.LatestVersion) {
		notes = append(notes, Notification{
			Message: fmt.Sprintf(
				"Warning: A new version of the kubetail CLI is available (%s > %s). See https://kubetail.com/docs/install to upgrade.\n",
				cliCache.LatestVersion, currentCLIVersion,
			),
		})
	}

	if helmCache == nil {
		return notes
	}
	if entry, ok := helmCache.Clusters[kubeContext]; ok && entry.CurrentVersion != "" && compareVersions(entry.CurrentVersion, helmCache.LatestVersion) {
		notes = append(notes, Notification{
			Message: fmt.Sprintf(
				"Warning: A new version of the Kubetail API is available (%s > %s). To upgrade, run: kubetail cluster upgrade\n",
				helmCache.LatestVersion, entry.CurrentVersion,
			),
		})
	}

	return notes
}

// resolveKubeContext returns kubeContext if non-empty, otherwise reads the
// current-context from the kubeconfig so the cache key is always an actual name.
func resolveKubeContext(kubeconfigPath, kubeContext string) string {
	if kubeContext != "" {
		return kubeContext
	}
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		loadingRules.ExplicitPath = kubeconfigPath
	}
	cfg, err := loadingRules.Load()
	if err != nil || cfg == nil {
		return ""
	}
	return cfg.CurrentContext
}

// getInstalledHelmChartVersion returns the oldest installed kubetail chart version
// across all namespaces. Oldest is used so the notification reflects the release
// most in need of upgrading when multiple installations exist.
func getInstalledHelmChartVersion(lister helmLister) string {
	releases, err := lister.ListReleases()
	if err != nil {
		return ""
	}
	var oldest *semver.Version
	for _, rel := range releases {
		if rel.Chart == nil || rel.Chart.Metadata == nil || rel.Chart.Metadata.Version == "" {
			continue
		}
		v, err := semver.NewVersion(rel.Chart.Metadata.Version)
		if err != nil {
			continue
		}
		if oldest == nil || v.LessThan(oldest) {
			oldest = v
		}
	}
	if oldest == nil {
		return ""
	}
	return oldest.Original()
}
