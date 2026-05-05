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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	helmchart "helm.sh/helm/v3/pkg/chart"
	helmrelease "helm.sh/helm/v3/pkg/release"

	"github.com/kubetail-org/kubetail/modules/shared/versioncheck"
)

func ptr[T any](v T) *T { return &v }

func TestReadWriteCLICache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cli-update-check.json")

	fetchedAt := time.Now().UnixMilli()
	c := &CLICache{
		SchemaVersion:   cliCacheVersion,
		LatestVersion:   "1.2.3",
		FetchedAt:       &fetchedAt,
		SkippedVersions: []string{"1.1.0"},
	}

	require.NoError(t, writeJSONCache(path, c))

	got, err := readCLICache(path)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, c.LatestVersion, got.LatestVersion)
	assert.Equal(t, *c.FetchedAt, *got.FetchedAt)
	assert.Equal(t, c.SkippedVersions, got.SkippedVersions)
}

func TestReadCLICache_MissingFile(t *testing.T) {
	got, err := readCLICache(filepath.Join(t.TempDir(), "nonexistent.json"))
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestReadCLICache_SchemaMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cli.json")
	require.NoError(t, writeJSONCache(path, &CLICache{SchemaVersion: 999, LatestVersion: "1.0.0"}))
	got, err := readCLICache(path)
	assert.NoError(t, err)
	assert.Nil(t, got, "mismatched schema version should be treated as missing")
}

func TestReadHelmCache_SchemaMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helm.json")
	require.NoError(t, writeJSONCache(path, &HelmCache{SchemaVersion: 999, LatestVersion: "1.0.0"}))
	got, err := readHelmCache(path)
	assert.NoError(t, err)
	assert.Nil(t, got, "mismatched schema version should be treated as missing")
}

func TestReadWriteHelmCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "helm-update-check.json")

	fetchedAt := time.Now().UnixMilli()
	cache := &HelmCache{
		SchemaVersion: helmCacheVersion,
		LatestVersion: "1.0.0",
		FetchedAt:     &fetchedAt,
		Clusters: map[string]ClusterCacheEntry{
			"ctx-a": {CurrentVersion: "0.9.0", FetchedAt: &fetchedAt},
			"ctx-b": {CurrentVersion: "1.0.0", FetchedAt: ptr(fetchedAt - 1000)},
		},
	}

	require.NoError(t, writeJSONCache(path, cache))

	got, err := readHelmCache(path)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, cache.LatestVersion, got.LatestVersion)
	assert.Equal(t, cache.Clusters["ctx-a"].CurrentVersion, got.Clusters["ctx-a"].CurrentVersion)
	assert.Equal(t, *cache.Clusters["ctx-b"].FetchedAt, *got.Clusters["ctx-b"].FetchedAt)
}

func TestReadHelmCache_MissingFile(t *testing.T) {
	got, err := readHelmCache(filepath.Join(t.TempDir(), "nonexistent.json"))
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		current string
		latest  string
		want    bool
	}{
		{"1.0.0", "1.2.3", true},
		{"1.9.0", "1.10.0", true}, // important: 10 > 9
		{"1.2.3", "1.2.3", false}, // equal
		{"1.2.3", "1.0.0", false}, // current is newer
		{"", "1.0.0", false},      // empty current
		{"1.0.0", "", false},      // empty latest
		{"bad", "1.0.0", false},   // invalid current
		{"1.0.0", "bad", false},   // invalid latest
	}

	for _, tc := range cases {
		got := compareVersions(tc.current, tc.latest)
		assert.Equal(t, tc.want, got, "compareVersions(%q, %q)", tc.current, tc.latest)
	}
}

func TestIsStale(t *testing.T) {
	ttl := time.Hour

	assert.True(t, isStale(nil, ttl), "nil fetchedAt should be stale")

	fresh := time.Now().UnixMilli()
	assert.False(t, isStale(&fresh, ttl), "just-written entry should not be stale")

	old := time.Now().Add(-2 * time.Hour).UnixMilli()
	assert.True(t, isStale(&old, ttl), "2h-old entry with 1h TTL should be stale")
}

func helmCache(latestVersion string, fetchedAt int64, clusters map[string]ClusterCacheEntry) *HelmCache {
	return &HelmCache{LatestVersion: latestVersion, FetchedAt: &fetchedAt, Clusters: clusters}
}

func TestBuildNotifications(t *testing.T) {
	fetchedAt := time.Now().UnixMilli()

	cases := []struct {
		name              string
		cliCache          *CLICache
		helmCache         *HelmCache
		currentCLIVersion string
		kubeContext       string
		wantCount         int
		wantCLI           bool
		wantHelm          bool
	}{
		{
			name:              "cli update available",
			cliCache:          &CLICache{LatestVersion: "1.2.3", FetchedAt: &fetchedAt},
			helmCache:         helmCache("0.9.0", fetchedAt, map[string]ClusterCacheEntry{"": {CurrentVersion: "0.9.0"}}),
			currentCLIVersion: "1.0.0",
			wantCount:         1,
			wantCLI:           true,
		},
		{
			name:              "cli up to date",
			cliCache:          &CLICache{LatestVersion: "1.2.3", FetchedAt: &fetchedAt},
			currentCLIVersion: "1.2.3",
			wantCount:         0,
		},
		{
			name:              "helm update available",
			cliCache:          &CLICache{LatestVersion: "1.0.0", FetchedAt: &fetchedAt},
			helmCache:         helmCache("1.0.0", fetchedAt, map[string]ClusterCacheEntry{"": {CurrentVersion: "0.9.0"}}),
			currentCLIVersion: "1.0.0",
			wantCount:         1,
			wantHelm:          true,
		},
		{
			name:              "helm not installed",
			cliCache:          &CLICache{LatestVersion: "1.0.0", FetchedAt: &fetchedAt},
			helmCache:         helmCache("1.0.0", fetchedAt, map[string]ClusterCacheEntry{"": {CurrentVersion: ""}}),
			currentCLIVersion: "1.0.0",
			wantCount:         0,
		},
		{
			name:              "helm latest unknown",
			cliCache:          &CLICache{LatestVersion: "1.0.0", FetchedAt: &fetchedAt},
			helmCache:         helmCache("", fetchedAt, map[string]ClusterCacheEntry{"": {CurrentVersion: "0.9.0"}}),
			currentCLIVersion: "1.0.0",
			wantCount:         0,
		},
		{
			name:              "both updates available",
			cliCache:          &CLICache{LatestVersion: "1.2.3", FetchedAt: &fetchedAt},
			helmCache:         helmCache("1.0.0", fetchedAt, map[string]ClusterCacheEntry{"": {CurrentVersion: "0.9.0"}}),
			currentCLIVersion: "1.0.0",
			wantCount:         2,
			wantCLI:           true,
			wantHelm:          true,
		},
		{
			name:              "nil caches",
			cliCache:          nil,
			helmCache:         nil,
			currentCLIVersion: "1.0.0",
			wantCount:         0,
		},
		{
			name:              "helm update for specific context",
			cliCache:          &CLICache{LatestVersion: "1.0.0", FetchedAt: &fetchedAt},
			helmCache:         helmCache("1.0.0", fetchedAt, map[string]ClusterCacheEntry{"prod": {CurrentVersion: "0.9.0"}}),
			currentCLIVersion: "1.0.0",
			kubeContext:       "prod",
			wantCount:         1,
			wantHelm:          true,
		},
		{
			name:              "helm entry for different context is ignored",
			cliCache:          &CLICache{LatestVersion: "1.0.0", FetchedAt: &fetchedAt},
			helmCache:         helmCache("1.0.0", fetchedAt, map[string]ClusterCacheEntry{"prod": {CurrentVersion: "0.9.0"}}),
			currentCLIVersion: "1.0.0",
			kubeContext:       "staging",
			wantCount:         0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			notes := buildNotifications(tc.cliCache, tc.helmCache, tc.currentCLIVersion, tc.kubeContext)
			assert.Len(t, notes, tc.wantCount)

			if tc.wantCLI {
				found := false
				for _, n := range notes {
					if strings.Contains(n.Message, "kubetail CLI") {
						found = true
						assert.Contains(t, n.Message, "kubetail.com/docs/install")
					}
				}
				assert.True(t, found, "expected CLI notification")
			}
			if tc.wantHelm {
				found := false
				for _, n := range notes {
					if strings.Contains(n.Message, "Kubetail API") {
						found = true
						assert.Contains(t, n.Message, "kubetail cluster upgrade")
					}
				}
				assert.True(t, found, "expected Helm notification")
			}
		})
	}
}

type mockChecker struct {
	callCount int
}

func newMockChecker() *mockChecker {
	return &mockChecker{}
}

func (m *mockChecker) GetLatestCLIVersion() (*versioncheck.VersionInfo, error) {
	m.callCount++
	return &versioncheck.VersionInfo{Version: "9.9.9"}, nil
}

func (m *mockChecker) GetLatestHelmChartVersion() (*versioncheck.VersionInfo, error) {
	return &versioncheck.VersionInfo{Version: "9.9.9"}, nil
}

var syncRunner = func(f func()) { f() }

func TestTriggerRefreshIfStale_DevBuild(t *testing.T) {
	checker := newMockChecker()
	TriggerRefreshIfStale(Options{
		CLICacheFile:      filepath.Join(t.TempDir(), "cli.json"),
		HelmCacheFile:     filepath.Join(t.TempDir(), "helm.json"),
		CurrentCLIVersion: "dev",
		CacheTTL:          time.Hour,
		Checker:           checker,
		Runner:            syncRunner,
	})
	assert.Equal(t, 0, checker.callCount, "dev build should not trigger refresh")
}

func TestTriggerRefreshIfStale_StaleCache_LaunchesRefresh(t *testing.T) {
	dir := t.TempDir()
	cliPath := filepath.Join(dir, "cli.json")

	oldMs := time.Now().Add(-25 * time.Hour).UnixMilli()
	require.NoError(t, writeJSONCache(cliPath, &CLICache{SchemaVersion: cliCacheVersion, LatestVersion: "1.0.0", FetchedAt: &oldMs}))

	checker := newMockChecker()
	TriggerRefreshIfStale(Options{
		CLICacheFile:      cliPath,
		HelmCacheFile:     filepath.Join(dir, "helm.json"),
		CurrentCLIVersion: "1.0.0",
		CacheTTL:          time.Hour,
		Checker:           checker,
		Runner:            syncRunner,
	})

	assert.Equal(t, 1, checker.callCount, "expected CLI refresh to run")
}

func TestTriggerRefreshIfStale_FreshCache_NoRefresh(t *testing.T) {
	dir := t.TempDir()
	cliPath := filepath.Join(dir, "cli.json")
	helmPath := filepath.Join(dir, "helm.json")

	freshMs := time.Now().UnixMilli()
	require.NoError(t, writeJSONCache(cliPath, &CLICache{SchemaVersion: cliCacheVersion, LatestVersion: "1.0.0", FetchedAt: &freshMs}))
	require.NoError(t, writeJSONCache(helmPath, &HelmCache{
		SchemaVersion: helmCacheVersion,
		LatestVersion: "1.0.0",
		FetchedAt:     &freshMs,
		Clusters:      map[string]ClusterCacheEntry{"my-ctx": {CurrentVersion: "1.0.0", FetchedAt: &freshMs}},
	}))

	checker := newMockChecker()
	TriggerRefreshIfStale(Options{
		CLICacheFile:      cliPath,
		HelmCacheFile:     helmPath,
		CurrentCLIVersion: "1.0.0",
		KubeContext:       "my-ctx",
		CacheTTL:          time.Hour,
		Checker:           checker,
		Runner:            syncRunner,
	})

	assert.Equal(t, 0, checker.callCount, "expected no background refresh for fresh cache")
}

func TestResolveKubeContext(t *testing.T) {
	t.Run("explicit context is returned as-is", func(t *testing.T) {
		assert.Equal(t, "prod", resolveKubeContext("", "prod"))
	})

	t.Run("empty context reads current-context from kubeconfig", func(t *testing.T) {
		kubeconfig := `
apiVersion: v1
kind: Config
current-context: default-ctx
contexts:
- name: default-ctx
  context:
    cluster: default-cluster
    user: default-user
clusters: []
users: []
`
		f, err := os.CreateTemp(t.TempDir(), "kubeconfig-*.yaml")
		require.NoError(t, err)
		_, err = f.WriteString(kubeconfig)
		require.NoError(t, err)
		f.Close()

		assert.Equal(t, "default-ctx", resolveKubeContext(f.Name(), ""))
	})

	t.Run("missing kubeconfig returns empty string", func(t *testing.T) {
		assert.Equal(t, "", resolveKubeContext(filepath.Join(t.TempDir(), "nonexistent"), ""))
	})
}

func TestNotify_DevBuild(t *testing.T) {
	notes := Notify(Options{
		CLICacheFile:      filepath.Join(t.TempDir(), "cli.json"),
		HelmCacheFile:     filepath.Join(t.TempDir(), "helm.json"),
		CurrentCLIVersion: "dev",
	})
	assert.Empty(t, notes)
}

func TestNotify_ReturnsNotificationFromCache(t *testing.T) {
	dir := t.TempDir()
	cliPath := filepath.Join(dir, "cli.json")

	freshMs := time.Now().UnixMilli()
	require.NoError(t, writeJSONCache(cliPath, &CLICache{SchemaVersion: cliCacheVersion, LatestVersion: "2.0.0", FetchedAt: &freshMs}))

	notes := Notify(Options{
		CLICacheFile:      cliPath,
		HelmCacheFile:     filepath.Join(dir, "helm.json"),
		CurrentCLIVersion: "1.0.0",
	})
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "kubetail CLI")
}

func TestNotify_EmptyCache_NoNotification(t *testing.T) {
	notes := Notify(Options{
		CLICacheFile:      filepath.Join(t.TempDir(), "cli.json"),
		HelmCacheFile:     filepath.Join(t.TempDir(), "helm.json"),
		CurrentCLIVersion: "1.0.0",
	})
	assert.Empty(t, notes)
}

// mockHelmLister is a helmLister that returns a fixed list of releases.
type mockHelmLister struct {
	releases []*helmrelease.Release
	err      error
}

func (m *mockHelmLister) ListReleases() ([]*helmrelease.Release, error) {
	return m.releases, m.err
}

func makeRelease(name, namespace, version string) *helmrelease.Release {
	return &helmrelease.Release{
		Name:      name,
		Namespace: namespace,
		Chart: &helmchart.Chart{
			Metadata: &helmchart.Metadata{Version: version},
		},
	}
}

func TestGetInstalledHelmChartVersion(t *testing.T) {
	cases := []struct {
		name     string
		releases []*helmrelease.Release
		err      error
		want     string
	}{
		{
			name:     "no releases",
			releases: nil,
			want:     "",
		},
		{
			name: "list error",
			err:  errors.New("connection refused"),
			want: "",
		},
		{
			name:     "single release",
			releases: []*helmrelease.Release{makeRelease("kubetail", "kubetail-system", "0.22.0")},
			want:     "0.22.0",
		},
		{
			name: "multiple releases same namespace",
			releases: []*helmrelease.Release{
				makeRelease("kubetail", "kubetail-system", "0.22.0"),
				makeRelease("kubetail-2", "kubetail-system", "0.21.0"),
			},
			want: "0.21.0",
		},
		{
			name: "multiple releases different namespaces",
			releases: []*helmrelease.Release{
				makeRelease("kubetail", "kubetail-system", "0.23.0"),
				makeRelease("kubetail", "monitoring", "0.20.0"),
			},
			want: "0.20.0",
		},
		{
			name: "multiple releases different names and namespaces",
			releases: []*helmrelease.Release{
				makeRelease("my-kubetail", "team-a", "0.22.0"),
				makeRelease("kubetail-prod", "team-b", "0.19.0"),
				makeRelease("kubetail", "kubetail-system", "0.23.0"),
			},
			want: "0.19.0",
		},
		{
			name: "release with empty version is skipped",
			releases: []*helmrelease.Release{
				makeRelease("kubetail", "kubetail-system", ""),
				makeRelease("kubetail-2", "kubetail-system", "0.22.0"),
			},
			want: "0.22.0",
		},
		{
			name: "release with invalid version is skipped",
			releases: []*helmrelease.Release{
				makeRelease("kubetail", "kubetail-system", "not-a-version"),
				makeRelease("kubetail-2", "kubetail-system", "0.22.0"),
			},
			want: "0.22.0",
		},
		{
			name: "release with nil chart is skipped",
			releases: []*helmrelease.Release{
				{Name: "kubetail", Namespace: "kubetail-system", Chart: nil},
				makeRelease("kubetail-2", "kubetail-system", "0.22.0"),
			},
			want: "0.22.0",
		},
		{
			name: "all releases invalid",
			releases: []*helmrelease.Release{
				makeRelease("kubetail", "kubetail-system", ""),
				makeRelease("kubetail-2", "kubetail-system", "bad"),
			},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lister := &mockHelmLister{releases: tc.releases, err: tc.err}
			assert.Equal(t, tc.want, getInstalledHelmChartVersion(lister))
		})
	}
}

func TestDefaultCLICacheFile(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	path, err := DefaultCLICacheFile()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".kubetail", "cache", "cli-update-check.json"), path)
}

func TestDefaultHelmCacheFile(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	path, err := DefaultHelmCacheFile()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".kubetail", "cache", "cluster-update-check.json"), path)
}
