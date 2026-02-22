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

package graph

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/utils/ptr"

	sharedcfg "github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/kubetail-org/kubetail/modules/shared/graphql/errors"
	"github.com/kubetail-org/kubetail/modules/shared/helm"
	k8shelpersmock "github.com/kubetail-org/kubetail/modules/shared/k8shelpers/mock"
	"github.com/kubetail-org/kubetail/modules/shared/versioncheck"
	vcmock "github.com/kubetail-org/kubetail/modules/shared/versioncheck/mock"

	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/config"
)

func mockVersionChecker(t *testing.T, vc versioncheck.Checker) {
	orig := newVersionChecker
	newVersionChecker = func() versioncheck.Checker { return vc }
	t.Cleanup(func() { newVersionChecker = orig })
}

type mockHelmListClientImpl struct {
	releases []*release.Release
	err      error
}

func (m *mockHelmListClientImpl) ListReleases() ([]*release.Release, error) {
	return m.releases, m.err
}

func mockHelmListClientFn(t *testing.T, client helmListClient) {
	orig := newHelmListClient
	newHelmListClient = func(opts ...helm.ClientOption) helmListClient { return client }
	t.Cleanup(func() { newHelmListClient = orig })
}

func makeRelease(name, version string) *release.Release {
	return &release.Release{
		Name: name,
		Chart: &chart.Chart{
			Metadata: &chart.Metadata{
				Version: version,
			},
		},
	}
}

func TestAllowedNamespacesGetQueries(t *testing.T) {
	// Init connection manager
	cm := &k8shelpersmock.MockConnectionManager{}
	cm.On("GetDefaultNamespace", mock.Anything).Return("default")
	cm.On("DerefKubeContext", mock.Anything).Return("")

	// Init resolver
	r := &queryResolver{&Resolver{
		allowedNamespaces: []string{"ns1", "ns2"},
		cm:                cm,
	}}

	// Table-driven tests
	tests := []struct {
		name         string
		setNamespace *string
	}{
		{"namespace not specified", nil},
		{"namespace specified but not allowed", ptr.To("nsforbidden")},
		{"namespace specified as wildcard", ptr.To("")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := r.AppsV1DaemonSetsGet(context.Background(), nil, tt.setNamespace, "", nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, errors.ErrForbidden)

			_, err = r.AppsV1DeploymentsGet(context.Background(), nil, tt.setNamespace, "", nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, errors.ErrForbidden)

			_, err = r.AppsV1ReplicaSetsGet(context.Background(), nil, tt.setNamespace, "", nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, errors.ErrForbidden)

			_, err = r.AppsV1StatefulSetsGet(context.Background(), nil, tt.setNamespace, "", nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, errors.ErrForbidden)

			_, err = r.BatchV1CronJobsGet(context.Background(), nil, tt.setNamespace, "", nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, errors.ErrForbidden)

			_, err = r.BatchV1JobsGet(context.Background(), nil, tt.setNamespace, "", nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, errors.ErrForbidden)

			_, err = r.CoreV1PodsGet(context.Background(), nil, tt.setNamespace, "", nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, errors.ErrForbidden)
		})
	}
}

func TestAllowedNamespacesListQueries(t *testing.T) {
	// Init connection manager
	cm := &k8shelpersmock.MockConnectionManager{}
	cm.On("GetDefaultNamespace", mock.Anything).Return("default")
	cm.On("DerefKubeContext", mock.Anything).Return("")

	// Init resolver
	r := &queryResolver{&Resolver{
		allowedNamespaces: []string{"ns1", "ns2"},
		cm:                cm,
	}}

	// Table-driven tests
	tests := []struct {
		name         string
		setNamespace *string
	}{
		{"namespace not specified", nil},
		{"namespace specified but not allowed", ptr.To("nsforbidden")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			_, err := r.AppsV1DaemonSetsList(context.Background(), nil, tt.setNamespace, nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, errors.ErrForbidden)

			_, err = r.AppsV1DeploymentsList(context.Background(), nil, tt.setNamespace, nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, errors.ErrForbidden)

			_, err = r.AppsV1ReplicaSetsList(context.Background(), nil, tt.setNamespace, nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, errors.ErrForbidden)

			_, err = r.AppsV1StatefulSetsList(context.Background(), nil, tt.setNamespace, nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, errors.ErrForbidden)

			_, err = r.BatchV1CronJobsList(context.Background(), nil, tt.setNamespace, nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, errors.ErrForbidden)

			_, err = r.BatchV1JobsList(context.Background(), nil, tt.setNamespace, nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, errors.ErrForbidden)

			_, err = r.CoreV1PodsList(context.Background(), nil, tt.setNamespace, nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, errors.ErrForbidden)
		})
	}
}

func TestDesktopOnlyRequests(t *testing.T) {
	cm := &k8shelpersmock.MockConnectionManager{}
	cm.On("DerefKubeContext", mock.Anything).Return("")

	resolver := &Resolver{
		environment: sharedcfg.EnvironmentCluster,
		cm:          cm,
	}

	t.Run("kubeConfigGet", func(t *testing.T) {
		r := &queryResolver{resolver}
		_, err := r.KubeConfigGet(context.Background())
		assert.NotNil(t, err)
		assert.Equal(t, err, errors.ErrForbidden)
	})

	t.Run("kubeConfigWatch", func(t *testing.T) {
		r := &subscriptionResolver{resolver}
		_, err := r.KubeConfigWatch(context.Background())
		assert.NotNil(t, err)
		assert.Equal(t, err, errors.ErrForbidden)
	})

	t.Run("helmListReleases", func(t *testing.T) {
		r := &queryResolver{resolver}
		_, err := r.HelmListReleases(context.Background(), nil)
		assert.NotNil(t, err)
		assert.Equal(t, err, errors.ErrForbidden)
	})

	t.Run("helmInstallLatest", func(t *testing.T) {
		r := &mutationResolver{resolver}
		_, err := r.HelmInstallLatest(context.Background(), nil)
		assert.NotNil(t, err)
		assert.Equal(t, err, errors.ErrForbidden)
	})
}

func TestCliVersionStatus(t *testing.T) {
	tests := []struct {
		name                string
		environment         sharedcfg.Environment
		cliVersion          string
		latestVersion       string
		checkerErr          error
		expectedNil         bool
		expectedCurrent     string
		expectedLatest      string
		expectedUpdateAvail bool
	}{
		{
			name:                "upgrade available",
			environment:         sharedcfg.EnvironmentDesktop,
			cliVersion:          "0.11.0",
			latestVersion:       "0.12.0",
			expectedNil:         false,
			expectedCurrent:     "0.11.0",
			expectedLatest:      "0.12.0",
			expectedUpdateAvail: true,
		},
		{
			name:                "up to date",
			environment:         sharedcfg.EnvironmentDesktop,
			cliVersion:          "0.12.0",
			latestVersion:       "0.12.0",
			expectedNil:         false,
			expectedCurrent:     "0.12.0",
			expectedLatest:      "0.12.0",
			expectedUpdateAvail: false,
		},
		{
			name:        "cluster mode returns nil",
			environment: sharedcfg.EnvironmentCluster,
			cliVersion:  "0.11.0",
			expectedNil: true,
		},
		{
			name:        "dev version returns nil",
			environment: sharedcfg.EnvironmentDesktop,
			cliVersion:  "dev",
			expectedNil: true,
		},
		{
			name:        "empty version returns nil",
			environment: sharedcfg.EnvironmentDesktop,
			cliVersion:  "",
			expectedNil: true,
		},
		{
			name:        "version checker error returns nil",
			environment: sharedcfg.EnvironmentDesktop,
			cliVersion:  "0.11.0",
			checkerErr:  fmt.Errorf("network error"),
			expectedNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vc := &vcmock.MockChecker{}
			if tt.checkerErr != nil {
				vc.On("GetLatestCLIVersion").Return(nil, tt.checkerErr)
			} else if tt.latestVersion != "" {
				vc.On("GetLatestCLIVersion").Return(&versioncheck.VersionInfo{Version: tt.latestVersion}, nil)
			}
			mockVersionChecker(t, vc)

			r := &queryResolver{&Resolver{
				cfg:         &config.Config{CLIVersion: tt.cliVersion},
				environment: tt.environment,
			}}

			result, err := r.CliVersionStatus(context.Background())
			assert.Nil(t, err)

			if tt.expectedNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.Equal(t, tt.expectedCurrent, result.CurrentVersion)
				assert.Equal(t, tt.expectedLatest, result.LatestVersion)
				assert.Equal(t, tt.expectedUpdateAvail, result.UpdateAvailable)
			}
		})
	}
}

func TestClusterVersionStatus_ClusterMode(t *testing.T) {
	tests := []struct {
		name                string
		envValue            string
		latestVersion       string
		checkerErr          error
		expectedNil         bool
		expectedCurrent     string
		expectedLatest      string
		expectedUpdateAvail bool
	}{
		{
			name:                "update available",
			envValue:            "0.9.0",
			latestVersion:       "0.10.0",
			expectedNil:         false,
			expectedCurrent:     "0.9.0",
			expectedLatest:      "0.10.0",
			expectedUpdateAvail: true,
		},
		{
			name:                "up to date",
			envValue:            "0.10.0",
			latestVersion:       "0.10.0",
			expectedNil:         false,
			expectedCurrent:     "0.10.0",
			expectedLatest:      "0.10.0",
			expectedUpdateAvail: false,
		},
		{
			name:        "env var not set returns nil",
			expectedNil: true,
		},
		{
			name:        "version checker error returns nil",
			envValue:    "0.9.0",
			checkerErr:  fmt.Errorf("network error"),
			expectedNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv("KUBETAIL_CHART_VERSION", tt.envValue)
			}

			vc := &vcmock.MockChecker{}
			if tt.checkerErr != nil {
				vc.On("GetLatestHelmChartVersion").Return(nil, tt.checkerErr)
			} else if tt.latestVersion != "" {
				vc.On("GetLatestHelmChartVersion").Return(&versioncheck.VersionInfo{Version: tt.latestVersion}, nil)
			}
			mockVersionChecker(t, vc)

			r := &queryResolver{&Resolver{
				cfg:         &config.Config{},
				environment: sharedcfg.EnvironmentCluster,
			}}

			result, err := r.ClusterVersionStatus(context.Background(), nil)
			assert.Nil(t, err)

			if tt.expectedNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.Equal(t, tt.expectedCurrent, result.CurrentVersion)
				assert.Equal(t, tt.expectedLatest, result.LatestVersion)
				assert.Equal(t, tt.expectedUpdateAvail, result.UpdateAvailable)
			}
		})
	}
}

func TestClusterVersionStatus_DesktopMode(t *testing.T) {
	tests := []struct {
		name                string
		releases            []*release.Release
		listErr             error
		latestVersion       string
		checkerErr          error
		expectedNil         bool
		expectedCurrent     string
		expectedLatest      string
		expectedUpdateAvail bool
	}{
		{
			name: "single release update available",
			releases: []*release.Release{
				makeRelease("kubetail", "0.9.0"),
			},
			latestVersion:       "0.10.0",
			expectedCurrent:     "0.9.0",
			expectedLatest:      "0.10.0",
			expectedUpdateAvail: true,
		},
		{
			name: "single release up to date",
			releases: []*release.Release{
				makeRelease("kubetail", "0.10.0"),
			},
			latestVersion:       "0.10.0",
			expectedCurrent:     "0.10.0",
			expectedLatest:      "0.10.0",
			expectedUpdateAvail: false,
		},
		{
			name: "multiple releases picks highest version",
			releases: []*release.Release{
				makeRelease("kubetail", "0.9.0"),
				makeRelease("kubetail-staging", "0.11.0"),
				makeRelease("kubetail-old", "0.8.0"),
			},
			latestVersion:       "0.12.0",
			expectedCurrent:     "0.11.0",
			expectedLatest:      "0.12.0",
			expectedUpdateAvail: true,
		},
		{
			name: "multiple releases highest is up to date",
			releases: []*release.Release{
				makeRelease("kubetail", "0.9.0"),
				makeRelease("kubetail-other", "0.12.0"),
			},
			latestVersion:       "0.12.0",
			expectedCurrent:     "0.12.0",
			expectedLatest:      "0.12.0",
			expectedUpdateAvail: false,
		},
		{
			name:        "list releases error returns nil",
			listErr:     fmt.Errorf("connection refused"),
			expectedNil: true,
		},
		{
			name: "version checker error returns nil",
			releases: []*release.Release{
				makeRelease("kubetail", "0.9.0"),
			},
			checkerErr:  fmt.Errorf("network error"),
			expectedNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := &k8shelpersmock.MockConnectionManager{}
			cm.On("DerefKubeContext", mock.Anything).Return("")

			mockHelmListClientFn(t, &mockHelmListClientImpl{
				releases: tt.releases,
				err:      tt.listErr,
			})

			vc := &vcmock.MockChecker{}
			if tt.checkerErr != nil {
				vc.On("GetLatestHelmChartVersion").Return(nil, tt.checkerErr)
			} else if tt.latestVersion != "" {
				vc.On("GetLatestHelmChartVersion").Return(&versioncheck.VersionInfo{Version: tt.latestVersion}, nil)
			}
			mockVersionChecker(t, vc)

			r := &queryResolver{&Resolver{
				cfg:         &config.Config{},
				cm:          cm,
				environment: sharedcfg.EnvironmentDesktop,
			}}

			result, err := r.ClusterVersionStatus(context.Background(), nil)
			assert.Nil(t, err)

			if tt.expectedNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.Equal(t, tt.expectedCurrent, result.CurrentVersion)
				assert.Equal(t, tt.expectedLatest, result.LatestVersion)
				assert.Equal(t, tt.expectedUpdateAvail, result.UpdateAvailable)
			}
		})
	}
}
