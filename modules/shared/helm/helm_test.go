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

package helm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	helmchart "helm.sh/helm/v3/pkg/chart"
	helmrelease "helm.sh/helm/v3/pkg/release"
)

func makeRelease(name, namespace, version string) *helmrelease.Release {
	return &helmrelease.Release{
		Name:      name,
		Namespace: namespace,
		Chart: &helmchart.Chart{
			Metadata: &helmchart.Metadata{Version: version},
		},
	}
}

func TestOldestChartVersion(t *testing.T) {
	tests := []struct {
		name     string
		releases []*helmrelease.Release
		want     string
	}{
		{
			name:     "empty input",
			releases: nil,
			want:     "",
		},
		{
			name: "single release",
			releases: []*helmrelease.Release{
				makeRelease("kubetail", "kubetail", "0.22.0"),
			},
			want: "0.22.0",
		},
		{
			name: "oldest wins in same namespace",
			releases: []*helmrelease.Release{
				makeRelease("kubetail", "kubetail", "0.22.0"),
				makeRelease("kubetail-2", "kubetail", "0.21.0"),
			},
			want: "0.21.0",
		},
		{
			name: "oldest wins across namespaces",
			releases: []*helmrelease.Release{
				makeRelease("kubetail", "kubetail-prod", "0.23.0"),
				makeRelease("kubetail", "kubetail-dev", "0.20.0"),
			},
			want: "0.20.0",
		},
		{
			name: "mixed names and namespaces",
			releases: []*helmrelease.Release{
				makeRelease("my-kubetail", "team-a", "0.23.0"),
				makeRelease("kubetail-prod", "prod", "0.21.0"),
				makeRelease("kubetail", "kubetail", "0.22.0"),
			},
			want: "0.21.0",
		},
		{
			name: "empty version skipped",
			releases: []*helmrelease.Release{
				makeRelease("kubetail", "kubetail", ""),
				makeRelease("kubetail-2", "kubetail", "0.22.0"),
			},
			want: "0.22.0",
		},
		{
			name: "invalid version skipped",
			releases: []*helmrelease.Release{
				makeRelease("kubetail", "kubetail", "not-a-version"),
				makeRelease("kubetail-2", "kubetail", "0.22.0"),
			},
			want: "0.22.0",
		},
		{
			name: "nil chart skipped",
			releases: []*helmrelease.Release{
				{Name: "kubetail", Namespace: "kubetail", Chart: nil},
				makeRelease("kubetail-2", "kubetail", "0.22.0"),
			},
			want: "0.22.0",
		},
		{
			name: "all invalid returns empty",
			releases: []*helmrelease.Release{
				makeRelease("kubetail", "kubetail", ""),
				makeRelease("kubetail-2", "kubetail", "bad"),
			},
			want: "",
		},
		{
			// Per SemVer 2.0 a pre-release is less than its stable, so the rc
			// surfaces as the install most in need of an upgrade.
			name: "prerelease is older than its stable",
			releases: []*helmrelease.Release{
				makeRelease("kubetail", "kubetail", "0.10.0"),
				makeRelease("kubetail-rc", "kubetail", "0.10.0-rc.1"),
			},
			want: "0.10.0-rc.1",
		},
		{
			// Original() must preserve the -rc.1 suffix so the upgrade prompt
			// shows the user exactly what they have installed.
			name: "prerelease suffix preserved",
			releases: []*helmrelease.Release{
				makeRelease("kubetail", "kubetail", "0.10.0-rc.1"),
			},
			want: "0.10.0-rc.1",
		},
		{
			name: "oldest among multiple prereleases",
			releases: []*helmrelease.Release{
				makeRelease("kubetail", "a", "0.10.0-rc.2"),
				makeRelease("kubetail", "b", "0.10.0-rc.1"),
				makeRelease("kubetail", "c", "0.10.0-beta.1"),
			},
			want: "0.10.0-beta.1",
		},
		{
			// Documents current policy: oldest wins regardless of stability,
			// so an rc still trumps a newer stable in the same cluster.
			name: "prerelease vs newer stable",
			releases: []*helmrelease.Release{
				makeRelease("kubetail", "a", "0.10.0-rc.1"),
				makeRelease("kubetail", "b", "0.11.0"),
			},
			want: "0.10.0-rc.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OldestChartVersion(tt.releases)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsUpdateAvailable(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{"latest greater", "0.9.0", "0.10.0", true},
		{"equal", "0.10.0", "0.10.0", false},
		{"latest older", "0.10.0", "0.9.0", false},
		{"current empty", "", "0.10.0", false},
		{"latest empty", "0.10.0", "", false},
		{"both empty", "", "", false},
		{"current unparseable", "not-a-version", "0.10.0", false},
		{"latest unparseable", "0.10.0", "bad", false},
		// Per SemVer 2.0, a pre-release ranks below its stable.
		{"prerelease vs matching stable", "0.10.0-rc.1", "0.10.0", true},
		{"stable vs matching prerelease", "0.10.0", "0.10.0-rc.1", false},
		{"prerelease ordering", "0.10.0-rc.1", "0.10.0-rc.2", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsUpdateAvailable(tt.current, tt.latest))
		})
	}
}
