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

// Package clusterapi provides a thin CLI-side client for the Kubetail
// cluster-api, reachable via the kube-apiserver aggregation layer.
package clusterapi

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	aggregator "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset"
)

// APIServiceName is the metadata.name of the Kubetail cluster-api APIService
// registered with the kube-apiserver aggregation layer.
const APIServiceName = "v1.api.kubetail.com"

// APIServicePath is the path under which the cluster-api is exposed by the
// kube-apiserver via the APIServiceName APIService.
const APIServicePath = "/apis/api.kubetail.com/v1"

// availabilityProbeTimeout caps how long IsKubetailAPIAvailable will wait
// for the apiserver to respond before giving up.
const availabilityProbeTimeout = 2 * time.Second

// IsKubetailAPIAvailable reports whether the Kubetail cluster-api APIService
// is registered and Available on the cluster pointed to by restConfig.
//
// A NotFound APIService is treated as a normal "not available" outcome
// (returns false, nil). Other errors (network, RBAC, etc.) are surfaced so
// callers that explicitly opt into the Kubetail backend can fail loudly.
func IsKubetailAPIAvailable(ctx context.Context, restConfig *rest.Config) (bool, error) {
	client, err := aggregator.NewForConfig(restConfig)
	if err != nil {
		return false, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, availabilityProbeTimeout)
	defer cancel()
	return isAvailableFromClient(probeCtx, client)
}

// isAvailableFromClient is the testable seam: it takes an aggregator
// clientset (real or fake) and returns whether the cluster-api APIService
// reports an Available=True condition.
func isAvailableFromClient(ctx context.Context, client aggregator.Interface) (bool, error) {
	apiSvc, err := client.ApiregistrationV1().APIServices().Get(ctx, APIServiceName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	for _, c := range apiSvc.Status.Conditions {
		if c.Type == apiregistrationv1.Available {
			return c.Status == apiregistrationv1.ConditionTrue, nil
		}
	}
	return false, nil
}
