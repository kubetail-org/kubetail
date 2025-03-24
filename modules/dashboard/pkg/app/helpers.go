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

package app

import (
	"context"
	"fmt"

	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"

	clusterapi "github.com/kubetail-org/kubetail/modules/dashboard/internal/cluster-api"
)

const k8sTokenSessionKey = "k8sToken"

const k8sTokenGinKey = "k8sToken"

// newClusterAPIProxy
func newClusterAPIProxy(cfg *config.Config, cm k8shelpers.ConnectionManager, pathPrefix string) (clusterapi.Proxy, error) {
	// Initialize new ClusterAPI proxy depending on environment
	switch cfg.Dashboard.Environment {
	case config.EnvironmentDesktop:
		return clusterapi.NewDesktopProxy(cm, pathPrefix)
	case config.EnvironmentCluster:
		return clusterapi.NewInClusterProxy(cfg.Dashboard.ClusterAPIEndpoint, pathPrefix)
	default:
		return nil, fmt.Errorf("env not supported: %s", cfg.Dashboard.Environment)
	}
}

// queryHelpers interface
type queryHelpers interface {
	HasAccess(ctx context.Context, token string) (*authv1.TokenReview, error)
}

// Represents implementation of queryHelpers
type realQueryHelpers struct {
	cm k8shelpers.ConnectionManager
}

// Create new k8sQueryHelpers instance
func newRealQueryHelpers(cm k8shelpers.ConnectionManager) *realQueryHelpers {
	return &realQueryHelpers{cm}
}

// HasAccess
func (qh *realQueryHelpers) HasAccess(ctx context.Context, token string) (*authv1.TokenReview, error) {
	// Get client
	clientset, err := qh.cm.GetOrCreateClientset(nil)
	if err != nil {
		return nil, err
	}

	// Use Token service
	tokenReview := &authv1.TokenReview{
		Spec: authv1.TokenReviewSpec{
			Token: token,
		},
	}

	// Execute
	return clientset.AuthenticationV1().TokenReviews().Create(ctx, tokenReview, metav1.CreateOptions{})
}
