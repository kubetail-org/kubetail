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

package k8shelpers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/kubetail-org/kubetail/modules/shared/util"
)

const (
	// CacheTTL defines the time-to-live for cached authorization results (5 minutes)
	cacheTTL = 5 * time.Minute
)

// cacheKey represents a unique key for caching authorization results
type cacheKey struct {
	namespace string
	group     string
	resource  string
	verb      string
}

// cacheValue represents a cached authorization result with expiration
type cacheValue struct {
	allowed    bool
	expiration time.Time
}

// Represents DesktopAuthorizer interface
type DesktopAuthorizer interface {
	IsAllowedInformer(ctx context.Context, clientset kubernetes.Interface, namespace string, gvr schema.GroupVersionResource) error
}

// Represents DesktopAuthorizer
type DefaultDesktopAuthorizer struct {
	cache util.SyncMap[cacheKey, cacheValue]
}

// Create new DesktopAuthorizer instance
func NewDesktopAuthorizer() DesktopAuthorizer {
	return &DefaultDesktopAuthorizer{
		cache: util.SyncMap[cacheKey, cacheValue]{},
	}
}

// Check permission for creating new informers
func (a *DefaultDesktopAuthorizer) IsAllowedInformer(ctx context.Context, clientset kubernetes.Interface, namespace string, gvr schema.GroupVersionResource) error {
	// Convenience method for handing errors
	doSAR := func(verb string) error {
		// Check cache first
		key := cacheKey{
			namespace: namespace,
			group:     gvr.Group,
			resource:  gvr.Resource,
			verb:      verb,
		}

		// Check if we have a valid cached result
		if cachedVal, ok := a.cache.Load(key); ok {
			if time.Now().Before(cachedVal.expiration) {
				// Cache hit and still valid
				if !cachedVal.allowed {
					attrs := &authv1.ResourceAttributes{
						Namespace: namespace,
						Group:     gvr.Group,
						Verb:      verb,
						Resource:  gvr.Resource,
					}
					fmt := "permission denied: `%s \"%s\"/\"%s\"` in namespace `%s`"
					return status.Errorf(codes.Unauthenticated, fmt, attrs.Verb, attrs.Group, attrs.Resource, attrs.Namespace)
				}
				return nil
			}
			// Cache expired, remove it
			a.cache.Delete(key)
		}

		// Cache miss or expired, perform the actual check
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

		// Cache the result only if the user is authorized
		if result.Status.Allowed {
			a.cache.Store(key, cacheValue{
				allowed:    true,
				expiration: time.Now().Add(cacheTTL),
			})
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
	clientsetInitializer clientsetInitializer
	cache                util.SyncMap[string, cacheValue]
}

// Create new InClusterAuthorizer instance
func NewInClusterAuthorizer() InClusterAuthorizer {
	return &DefaultInClusterAuthorizer{
		clientsetInitializer: &defaultClientsetInitializer{},
		cache:                util.SyncMap[string, cacheValue]{},
	}
}

// Check permission for creating new informers
func (a *DefaultInClusterAuthorizer) IsAllowedInformer(ctx context.Context, restConfig *rest.Config, token string, namespace string, gvr schema.GroupVersionResource) error {
	tokenTrimmed := strings.TrimSpace(token)

	// For in-cluster authorizer, include token in cache key
	cacheKeyPrefix := fmt.Sprintf("%s:", tokenTrimmed)

	// Clone rest config and set bearer token
	rcClone := *restConfig
	rcClone.BearerToken = tokenTrimmed

	if tokenTrimmed != "" {
		rcClone.BearerTokenFile = ""
	}

	// Init clientset
	// TODO: use kubernetes.NewForConfigAndClient to re-use underlying transport
	clientset, err := a.clientsetInitializer.newClientset(&rcClone)
	if err != nil {
		return err
	}

	// Convenience method for handing errors
	doSAR := func(verb string) error {
		// Check cache first
		key := cacheKey{
			namespace: namespace,
			group:     gvr.Group,
			resource:  gvr.Resource,
			verb:      verb,
		}

		// Add token to make the key unique for each token
		tokenKey := fmt.Sprintf("%s%v", cacheKeyPrefix, key)

		// Check if we have a valid cached result
		if cachedVal, ok := a.cache.Load(tokenKey); ok {
			if time.Now().Before(cachedVal.expiration) {
				// Cache hit and still valid
				if !cachedVal.allowed {
					attrs := &authv1.ResourceAttributes{
						Namespace: namespace,
						Group:     gvr.Group,
						Verb:      verb,
						Resource:  gvr.Resource,
					}
					fmt := "permission denied: `%s \"%s\"/\"%s\"` in namespace `%s`"
					return status.Errorf(codes.Unauthenticated, fmt, attrs.Verb, attrs.Group, attrs.Resource, attrs.Namespace)
				}
				return nil
			}
			// Cache expired, remove it
			a.cache.Delete(tokenKey)
		}

		// Cache miss or expired, perform the actual check
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

		// Cache the result only if the user is authorized
		if result.Status.Allowed {
			a.cache.Store(tokenKey, cacheValue{
				allowed:    true,
				expiration: time.Now().Add(cacheTTL),
			})
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

// Interface to facilitate testing
type clientsetInitializer interface {
	newClientset(restConfig *rest.Config) (kubernetes.Interface, error)
}

// Default implementation
type defaultClientsetInitializer struct{}

// Create new clientset
func (d *defaultClientsetInitializer) newClientset(restConfig *rest.Config) (kubernetes.Interface, error) {
	return kubernetes.NewForConfig(restConfig)
}
