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

package graph

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicFake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

func TestAllowedNamespacesGetQueries(t *testing.T) {
	// init resolver
	r := queryResolver{&Resolver{
		allowedNamespaces: []string{"ns1", "ns2"},
		TestClientset:     fake.NewSimpleClientset(),
	}}

	// table-driven tests
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
			_, err := r.AppsV1DaemonSetsGet(context.Background(), "", tt.setNamespace, nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, ErrForbidden)

			_, err = r.AppsV1DeploymentsGet(context.Background(), "", tt.setNamespace, nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, ErrForbidden)

			_, err = r.AppsV1ReplicaSetsGet(context.Background(), "", tt.setNamespace, nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, ErrForbidden)

			_, err = r.AppsV1StatefulSetsGet(context.Background(), "", tt.setNamespace, nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, ErrForbidden)

			_, err = r.BatchV1CronJobsGet(context.Background(), "", tt.setNamespace, nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, ErrForbidden)

			_, err = r.BatchV1JobsGet(context.Background(), "", tt.setNamespace, nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, ErrForbidden)

			_, err = r.CoreV1PodsGet(context.Background(), tt.setNamespace, "", nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, ErrForbidden)
		})
	}
}

func TestAllowedNamespacesListQueries(t *testing.T) {
	// init dynamic client
	scheme := runtime.NewScheme()
	appsv1.AddToScheme(scheme)
	batchv1.AddToScheme(scheme)
	corev1.AddToScheme(scheme)
	dynamicClient := dynamicFake.NewSimpleDynamicClient(scheme)

	// init resolver
	r := queryResolver{&Resolver{
		allowedNamespaces: []string{"ns1", "ns2"},
		TestDynamicClient: dynamicClient,
	}}

	// table-driven tests
	tests := []struct {
		name         string
		setNamespace *string
	}{
		{"namespace not specified", nil},
		{"namespace specified but not allowed", ptr.To[string]("nsforbidden")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := r.AppsV1DaemonSetsList(context.Background(), tt.setNamespace, nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, ErrForbidden)

			_, err = r.AppsV1DeploymentsList(context.Background(), tt.setNamespace, nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, ErrForbidden)

			_, err = r.AppsV1ReplicaSetsList(context.Background(), tt.setNamespace, nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, ErrForbidden)

			_, err = r.AppsV1StatefulSetsList(context.Background(), tt.setNamespace, nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, ErrForbidden)

			_, err = r.BatchV1CronJobsList(context.Background(), tt.setNamespace, nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, ErrForbidden)

			_, err = r.BatchV1JobsList(context.Background(), tt.setNamespace, nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, ErrForbidden)

			_, err = r.CoreV1PodsList(context.Background(), tt.setNamespace, nil)
			assert.NotNil(t, err)
			assert.Equal(t, err, ErrForbidden)
		})
	}
}
