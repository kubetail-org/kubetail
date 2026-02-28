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
	k8shelpersmock "github.com/kubetail-org/kubetail/modules/shared/k8shelpers/mock"
	"github.com/kubetail-org/kubetail/modules/shared/versioncheck"
	vcmock "github.com/kubetail-org/kubetail/modules/shared/versioncheck/mock"

	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/config"
)

// mockHelmReleaseGetter implements helmReleaseGetter for testing.
type mockHelmReleaseGetter struct {
	release *release.Release
	err     error
}

func (m *mockHelmReleaseGetter) GetRelease(namespace, releaseName string) (*release.Release, error) {
	return m.release, m.err
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

			r := &queryResolver{&Resolver{
				cfg:            &config.Config{CLIVersion: tt.cliVersion},
				environment:    tt.environment,
				versionChecker: vc,
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

			r := &queryResolver{&Resolver{
				cfg:            &config.Config{},
				environment:    sharedcfg.EnvironmentCluster,
				versionChecker: vc,
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
		release             *release.Release
		getErr              error
		latestVersion       string
		checkerErr          error
		expectedNil         bool
		expectedCurrent     string
		expectedLatest      string
		expectedUpdateAvail bool
	}{
		{
			name:                "update available",
			release:             makeRelease("kubetail", "0.9.0"),
			latestVersion:       "0.10.0",
			expectedCurrent:     "0.9.0",
			expectedLatest:      "0.10.0",
			expectedUpdateAvail: true,
		},
		{
			name:                "up to date",
			release:             makeRelease("kubetail", "0.10.0"),
			latestVersion:       "0.10.0",
			expectedCurrent:     "0.10.0",
			expectedLatest:      "0.10.0",
			expectedUpdateAvail: false,
		},
		{
			name:        "release not found returns nil",
			getErr:      fmt.Errorf("release not found"),
			expectedNil: true,
		},
		{
			name:        "nil release returns nil",
			expectedNil: true,
		},
		{
			name:        "version checker error returns nil",
			release:     makeRelease("kubetail", "0.9.0"),
			checkerErr:  fmt.Errorf("network error"),
			expectedNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vc := &vcmock.MockChecker{}
			if tt.checkerErr != nil {
				vc.On("GetLatestHelmChartVersion").Return(nil, tt.checkerErr)
			} else if tt.latestVersion != "" {
				vc.On("GetLatestHelmChartVersion").Return(&versioncheck.VersionInfo{Version: tt.latestVersion}, nil)
			}

			r := &queryResolver{&Resolver{
				cfg:            &config.Config{},
				environment:    sharedcfg.EnvironmentDesktop,
				versionChecker: vc,
				helmReleaseGetter: &mockHelmReleaseGetter{
					release: tt.release,
					err:     tt.getErr,
				},
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
