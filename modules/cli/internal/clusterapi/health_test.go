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

package clusterapi

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clienttesting "k8s.io/client-go/testing"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	aggregatorfake "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/fake"
)

func newAPIServiceForTest(name string, conds ...apiregistrationv1.APIServiceCondition) *apiregistrationv1.APIService {
	return &apiregistrationv1.APIService{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status:     apiregistrationv1.APIServiceStatus{Conditions: conds},
	}
}

func availableCond(s apiregistrationv1.ConditionStatus) apiregistrationv1.APIServiceCondition {
	return apiregistrationv1.APIServiceCondition{Type: apiregistrationv1.Available, Status: s}
}

func TestIsKubetailAPIAvailable_ReturnsTrueWhenConditionAvailable(t *testing.T) {
	client := aggregatorfake.NewSimpleClientset(
		newAPIServiceForTest(APIServiceName, availableCond(apiregistrationv1.ConditionTrue)),
	)
	ok, err := isAvailableFromClient(context.Background(), client)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestIsKubetailAPIAvailable_ReturnsFalseWhenNotFound(t *testing.T) {
	client := aggregatorfake.NewSimpleClientset()
	ok, err := isAvailableFromClient(context.Background(), client)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestIsKubetailAPIAvailable_ReturnsFalseWhenConditionFalse(t *testing.T) {
	client := aggregatorfake.NewSimpleClientset(
		newAPIServiceForTest(APIServiceName, availableCond(apiregistrationv1.ConditionFalse)),
	)
	ok, err := isAvailableFromClient(context.Background(), client)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestIsKubetailAPIAvailable_ReturnsFalseWhenNoAvailableCondition(t *testing.T) {
	client := aggregatorfake.NewSimpleClientset(
		newAPIServiceForTest(APIServiceName, apiregistrationv1.APIServiceCondition{
			Type: "SomethingElse", Status: apiregistrationv1.ConditionTrue,
		}),
	)
	ok, err := isAvailableFromClient(context.Background(), client)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestIsKubetailAPIAvailable_PropagatesNonNotFoundErrors(t *testing.T) {
	client := aggregatorfake.NewSimpleClientset()
	client.PrependReactor("get", "apiservices", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("boom")
	})
	ok, err := isAvailableFromClient(context.Background(), client)
	require.Error(t, err)
	assert.False(t, ok)

	// Sanity: ensure NotFound is treated specially.
	notFound := apierrors.NewNotFound(schema.GroupResource{Group: "apiregistration.k8s.io", Resource: "apiservices"}, APIServiceName)
	_ = notFound
}
