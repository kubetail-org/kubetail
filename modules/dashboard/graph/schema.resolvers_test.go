// Copyright 2024-2025 Andres Morey
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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/utils/ptr"

	"github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/kubetail-org/kubetail/modules/shared/graphql/errors"

	k8shelpersmock "github.com/kubetail-org/kubetail/modules/dashboard/internal/k8shelpers/mock"
)

func TestAllowedNamespacesGetQueries(t *testing.T) {
	// Init connection manager
	cm := &k8shelpersmock.MockConnectionManager{}
	cm.On("GetDefaultNamespace", mock.Anything).Return("default")

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
		{"namespace specified but not allowed", ptr.To[string]("nsforbidden")},
		{"namespace specified as wildcard", ptr.To[string]("")},
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
		{"namespace specified but not allowed", ptr.To[string]("nsforbidden")},
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
	resolver := &Resolver{environment: config.EnvironmentCluster}

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
