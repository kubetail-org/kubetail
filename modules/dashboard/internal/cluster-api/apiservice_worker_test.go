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
	"testing"
	"time"

	"github.com/kubetail-org/megaphone"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	aggregatorfake "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/fake"
)

func newWorker() *apiServiceHealthMonitorWorker {
	return &apiServiceHealthMonitorWorker{
		lastStatus: HealthStatusUnknown,
		mp:         megaphone.New[HealthStatus](),
		shutdownCh: make(chan struct{}),
	}
}

func newAPIService(name, uid string, conditions ...apiregistrationv1.APIServiceCondition) *apiregistrationv1.APIService {
	return &apiregistrationv1.APIService{
		ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(uid)},
		Status:     apiregistrationv1.APIServiceStatus{Conditions: conditions},
	}
}

func availableCond(s apiregistrationv1.ConditionStatus) apiregistrationv1.APIServiceCondition {
	return apiregistrationv1.APIServiceCondition{Type: apiregistrationv1.Available, Status: s}
}

func TestAPIServiceWorker_GetHealthStatus(t *testing.T) {
	tests := []struct {
		name string
		seed *apiregistrationv1.APIService
		want HealthStatus
	}{
		{
			name: "no APIService",
			seed: nil,
			want: HealthStatusNotFound,
		},
		{
			name: "no Available condition",
			seed: newAPIService(APIServiceName, "uid-a"),
			want: HealthStatusPending,
		},
		{
			name: "Available True",
			seed: newAPIService(APIServiceName, "uid-a", availableCond(apiregistrationv1.ConditionTrue)),
			want: HealthStatusSuccess,
		},
		{
			name: "Available False",
			seed: newAPIService(APIServiceName, "uid-a", availableCond(apiregistrationv1.ConditionFalse)),
			want: HealthStatusFailure,
		},
		{
			name: "only non-Available conditions",
			seed: newAPIService(APIServiceName, "uid-a", apiregistrationv1.APIServiceCondition{
				Type:   "SomethingElse",
				Status: apiregistrationv1.ConditionTrue,
			}),
			want: HealthStatusPending,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorker()
			w.apiSvc = tt.seed
			assert.Equal(t, tt.want, w.getHealthStatus_UNSAFE())
		})
	}
}

func TestAPIServiceWorker_OnInformerAdd_PopulatesCacheAndPublishes(t *testing.T) {
	w := newWorker()
	apiSvc := newAPIService(APIServiceName, "uid-a", availableCond(apiregistrationv1.ConditionTrue))

	w.onInformerAdd(apiSvc)

	assert.Same(t, apiSvc, w.apiSvc)
	assert.Equal(t, HealthStatusSuccess, w.lastStatus)
}

func TestAPIServiceWorker_OnInformerUpdate_AppliesPendingFoldIn(t *testing.T) {
	w := newWorker()
	first := newAPIService(APIServiceName, "uid-a")

	w.onInformerAdd(first)
	assert.Equal(t, HealthStatusPending, w.lastStatus)

	failed := newAPIService(APIServiceName, "uid-a", availableCond(apiregistrationv1.ConditionFalse))
	w.onInformerUpdate(first, failed)
	assert.Equal(t, HealthStatusPending, w.lastStatus, "Failure from Pending should fold to Pending")

	// After a non-pending lastStatus, Failure should land directly.
	w.lastStatus = HealthStatusSuccess
	w.onInformerUpdate(failed, failed)
	assert.Equal(t, HealthStatusFailure, w.lastStatus)
}

func TestAPIServiceWorker_OnInformerDelete_ClearsCache(t *testing.T) {
	w := newWorker()
	apiSvc := newAPIService(APIServiceName, "uid-a", availableCond(apiregistrationv1.ConditionTrue))

	w.onInformerAdd(apiSvc)
	w.onInformerDelete(apiSvc)

	assert.Nil(t, w.apiSvc)
	assert.Equal(t, HealthStatusNotFound, w.lastStatus)
}

func TestAPIServiceWorker_OnInformerDelete_UnwrapsTombstone(t *testing.T) {
	w := newWorker()
	apiSvc := newAPIService(APIServiceName, "uid-a", availableCond(apiregistrationv1.ConditionTrue))

	w.onInformerAdd(apiSvc)
	w.onInformerDelete(cache.DeletedFinalStateUnknown{Key: APIServiceName, Obj: apiSvc})

	assert.Nil(t, w.apiSvc)
	assert.Equal(t, HealthStatusNotFound, w.lastStatus)
}

func TestNewAPIServiceWorker_FiltersByName(t *testing.T) {
	target := newAPIService(APIServiceName, "uid-target", availableCond(apiregistrationv1.ConditionTrue))
	other := newAPIService("v1.other.example.com", "uid-other", availableCond(apiregistrationv1.ConditionFalse))

	client := aggregatorfake.NewClientset(target, other)
	w, err := newAPIServiceHealthMonitorWorker(client)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, w.Start(ctx))
	t.Cleanup(w.Shutdown)

	// Status==Success implies the matching APIService entered the cache; if the
	// field selector were ineffective, the False-condition `other` would yield
	// Failure (or Pending under fold-in).
	assert.Equal(t, HealthStatusSuccess, w.GetHealthStatus())
}
