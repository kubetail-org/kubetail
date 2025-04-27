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

package k8shelpers

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Represents DesktopAuthorizer interface
type DesktopAuthorizer interface {
	IsAllowedInformer(ctx context.Context, clientset kubernetes.Interface, namespace string, gvr schema.GroupVersionResource) error
}

// Represents DesktopAuthorizer
type DefaultDesktopAuthorizer struct {
}

// Create new DesktopAuthorizer instance
func NewDesktopAuthorizer() DesktopAuthorizer {
	return &DefaultDesktopAuthorizer{}
}

// Check permission for creating new informers
func (a *DefaultDesktopAuthorizer) IsAllowedInformer(ctx context.Context, clientset kubernetes.Interface, namespace string, gvr schema.GroupVersionResource) error {
	// Convenience method for handing errors
	doSAR := func(verb string) error {
		sar := &authv1.SelfSubjectAccessReview{
			Spec: authv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authv1.ResourceAttributes{
					Namespace: namespace,
					Group:     gvr.Group,
					Verb:      verb,
					Resource:  gvr.Resource,
				},
			},
		}

		result, err := clientset.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})
		if err != nil {
			return err
		}

		if !result.Status.Allowed {
			attrs := result.Spec.ResourceAttributes
			fmt := "permission denied: `%s \"%s\"/\"%s\"` in namespace `%s`"
			return status.Errorf(codes.Unauthenticated, fmt, attrs.Verb, attrs.Group, attrs.Resource, attrs.Namespace)
		}

		return nil
	}

	// Make individual requests in an error group
	g, ctx := errgroup.WithContext(ctx)

	// Check node `list` permissions
	g.Go(func() error {
		return doSAR("list")
	})

	// Check node `watch` permissions
	g.Go(func() error {
		return doSAR("watch")
	})

	return g.Wait()
}

// Represents InClusterAuthorizer interface
type InClusterAuthorizer interface {
	IsAllowedInformer(ctx context.Context, restConfig *rest.Config, token string, namespace string, gvr schema.GroupVersionResource) error
}

// Represents InClusterAuthorizer
type DefaultInClusterAuthorizer struct {
}

// Create new InClusterAuthorizer instance
func NewInClusterAuthorizer() InClusterAuthorizer {
	return &DefaultInClusterAuthorizer{}
}

// Check permission for creating new informers
func (a *DefaultInClusterAuthorizer) IsAllowedInformer(ctx context.Context, restConfig *rest.Config, token string, namespace string, gvr schema.GroupVersionResource) error {
	tokenTrimmed := strings.TrimSpace(token)

	if tokenTrimmed == "" {
		return fmt.Errorf("token is required")
	}

	// Clone rest config and set bearer token
	rcClone := *restConfig
	rcClone.BearerToken = tokenTrimmed

	// Init clientset
	// TODO: use kubernetes.NewForConfigAndClient to re-use underlying transport
	clientset, err := kubernetes.NewForConfig(&rcClone)
	if err != nil {
		return err
	}

	// Convenience method for handing errors
	doSAR := func(verb string) error {
		sar := &authv1.SelfSubjectAccessReview{
			Spec: authv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authv1.ResourceAttributes{
					Namespace: namespace,
					Group:     gvr.Group,
					Verb:      verb,
					Resource:  gvr.Resource,
				},
			},
		}

		result, err := clientset.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})
		if err != nil {
			return err
		}

		if !result.Status.Allowed {
			attrs := result.Spec.ResourceAttributes
			fmt := "permission denied: `%s \"%s\"/\"%s\"` in namespace `%s`"
			return status.Errorf(codes.Unauthenticated, fmt, attrs.Verb, attrs.Group, attrs.Resource, attrs.Namespace)
		}

		return nil
	}

	// Make individual requests in an error group
	g, ctx := errgroup.WithContext(ctx)

	// Check node `list` permissions
	g.Go(func() error {
		return doSAR("list")
	})

	// Check node `watch` permissions
	g.Go(func() error {
		return doSAR("watch")
	})

	return g.Wait()
}
