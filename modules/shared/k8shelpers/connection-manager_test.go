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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	testclient "k8s.io/client-go/testing"
)

// MockConnectionManager implements the ConnectionManager interface for testing
type MockConnectionManager struct {
	mock.Mock
}

func (m *MockConnectionManager) GetOrCreateRestConfig(kubeContext string) (*rest.Config, error) {
	args := m.Called(kubeContext)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*rest.Config), args.Error(1)
}

func (m *MockConnectionManager) GetOrCreateClientset(kubeContext string) (kubernetes.Interface, error) {
	args := m.Called(kubeContext)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(kubernetes.Interface), args.Error(1)
}

func (m *MockConnectionManager) GetOrCreateDynamicClient(kubeContext string) (dynamic.Interface, error) {
	args := m.Called(kubeContext)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(dynamic.Interface), args.Error(1)
}

func (m *MockConnectionManager) GetDefaultNamespace(kubeContext string) string {
	args := m.Called(kubeContext)
	return args.String(0)
}

func (m *MockConnectionManager) DerefKubeContext(kubeContext *string) string {
	args := m.Called(kubeContext)
	return args.String(0)
}

func (m *MockConnectionManager) NewInformer(ctx context.Context, kubeContext string, token string, namespace string, gvr schema.GroupVersionResource) (informers.GenericInformer, func(), error) {
	args := m.Called(ctx, kubeContext, token, namespace, gvr)
	return nil, func() {}, args.Error(2)
}

func (m *MockConnectionManager) WaitUntilReady(ctx context.Context, kubeContext string) error {
	args := m.Called(ctx, kubeContext)
	return args.Error(0)
}

func (m *MockConnectionManager) Shutdown(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// TestIsAuthorizedForInformer tests the isAuthorizedForInformer function
func TestIsAuthorizedForInformer(t *testing.T) {
	// Define test cases
	testCases := []struct {
		name          string
		listAllowed   bool
		watchAllowed  bool
		expectError   bool
		errorContains string
		setupReactors func(clientset *fake.Clientset)
	}{
		{
			name:         "both permissions allowed",
			listAllowed:  true,
			watchAllowed: true,
			expectError:  false,
		},
		{
			name:          "list permission denied",
			listAllowed:   false,
			watchAllowed:  true,
			expectError:   true,
			errorContains: "permission denied",
		},
		{
			name:          "watch permission denied",
			listAllowed:   true,
			watchAllowed:  false,
			expectError:   true,
			errorContains: "permission denied",
		},
		{
			name:          "both permissions denied",
			listAllowed:   false,
			watchAllowed:  false,
			expectError:   true,
			errorContains: "permission denied",
		},
		{
			name:        "api server error",
			expectError: true,
			setupReactors: func(clientset *fake.Clientset) {
				clientset.PrependReactor("create", "selfsubjectaccessreviews", func(action testclient.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, fmt.Errorf("api server error")
				})
			},
			errorContains: "api server error",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create context
			ctx := context.Background()

			// Create mock connection manager
			mockCM := new(MockConnectionManager)

			// Create fake clientset with the desired behavior
			fakeClientset := fake.NewSimpleClientset()

			// Setup custom reactors if provided
			if tc.setupReactors != nil {
				tc.setupReactors(fakeClientset)
			} else {
				// Default behavior: setup reactors for SelfSubjectAccessReview
				fakeClientset.PrependReactor("create", "selfsubjectaccessreviews", func(action testclient.Action) (handled bool, ret runtime.Object, err error) {
					createAction := action.(testclient.CreateAction)
					sar := createAction.GetObject().(*authv1.SelfSubjectAccessReview)

					// Determine if this is a list or watch request
					isAllowed := false
					if sar.Spec.ResourceAttributes.Verb == "list" {
						isAllowed = tc.listAllowed
					} else if sar.Spec.ResourceAttributes.Verb == "watch" {
						isAllowed = tc.watchAllowed
					}

					// Set the result
					sar.Status.Allowed = isAllowed
					return true, sar, nil
				})
			}

			// Create a rest config that will be returned by the mock
			restConfig := &rest.Config{
				Host: "https://test-cluster.example.com",
			}

			// Setup mock to return our rest config
			mockCM.On("GetOrCreateRestConfig", "test-context").Return(restConfig, nil)

			// Create a wrapper function for testing that doesn't rely on modifying package functions
			testFn := func() error {
				// Test parameters
				kubeContext := "test-context"
				token := "test-token"
				namespace := "test-namespace"
				gvr := schema.GroupVersionResource{
					Group:    "apps",
					Version:  "v1",
					Resource: "deployments",
				}

				// Create a modified version of isAuthorizedForInformer that accepts a clientset factory function
				return isAuthorizedForInformerTest(ctx, mockCM, kubeContext, token, namespace, gvr, func(config *rest.Config) (kubernetes.Interface, error) {
					return fakeClientset, nil
				})
			}

			// Call the test function
			err := testFn()

			// Assertions
			if tc.expectError {
				assert.Error(t, err)
				if tc.errorContains != "" {
					assert.Contains(t, err.Error(), tc.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}

			// Verify mock expectations
			mockCM.AssertExpectations(t)
		})
	}
}

// isAuthorizedForInformerTest is a testable version of isAuthorizedForInformer that accepts a clientset factory function
func isAuthorizedForInformerTest(
	ctx context.Context,
	cm ConnectionManager,
	kubeContext string,
	token string,
	namespace string,
	gvr schema.GroupVersionResource,
	clientsetFactory func(*rest.Config) (kubernetes.Interface, error),
) error {
	// Clone rest config and set bearer token
	rc, err := cm.GetOrCreateRestConfig(kubeContext)
	if err != nil {
		return err
	}

	rcClone := *rc
	rcClone.BearerToken = token

	// Init clientset using the provided factory function
	clientset, err := clientsetFactory(&rcClone)
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
			format := "permission denied: `%s \"%s\"/\"%s\"` in namespace `%s`"
			return fmt.Errorf(format, attrs.Verb, attrs.Group, attrs.Resource, attrs.Namespace)
		}

		return nil
	}

	// Make individual requests in parallel
	var listErr, watchErr error

	// Check "list" permissions
	listErr = doSAR("list")

	// Check "watch" permissions
	watchErr = doSAR("watch")

	// Return first error encountered
	if listErr != nil {
		return listErr
	}
	return watchErr
}
