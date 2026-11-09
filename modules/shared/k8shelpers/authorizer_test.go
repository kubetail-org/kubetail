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
	"errors"
	"testing"
	"time"

	"github.com/kubetail-org/kubetail/modules/shared/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	ktesting "k8s.io/client-go/testing"
)

func TestNewDesktopAuthorizer(t *testing.T) {
	authorizer := NewDesktopAuthorizer()
	_, ok := authorizer.(*DefaultDesktopAuthorizer)
	assert.True(t, ok, "NewDesktopAuthorizer should return a *DefaultDesktopAuthorizer")
}

func TestDesktopAuthorizer_IsAllowedInformer_Success(t *testing.T) {
	setNamespace := "test-namespace"
	setGVR := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	// Prepare fake clientset
	capturedSARs := []*authorizationv1.SelfSubjectAccessReview{}

	clientset := fake.NewClientset()
	clientset.Fake.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		createAction := action.(ktesting.CreateAction)
		obj := createAction.GetObject().(*authorizationv1.SelfSubjectAccessReview)
		capturedSARs = append(capturedSARs, obj)

		// Verify the request the helper builds
		if ra := obj.Spec.ResourceAttributes; ra == nil ||
			(ra.Verb != "list" && ra.Verb != "watch") || ra.Resource != setGVR.Resource || ra.Namespace != setNamespace {
			t.Fatalf("unexpected SSAR payload: %#v", obj.Spec.ResourceAttributes)
		}

		// Fabricate a positive response so the helper returns true
		return true, &authorizationv1.SelfSubjectAccessReview{
			ObjectMeta: metav1.ObjectMeta{Name: "ssar-response"},
			Status: authorizationv1.SubjectAccessReviewStatus{
				Allowed: true,
			},
		}, nil
	})

	// Create authorizer and test
	authorizer := NewDesktopAuthorizer()
	err := authorizer.IsAllowedInformer(context.Background(), clientset, setNamespace, setGVR)
	assert.NoError(t, err)

	// Verify the SSARs were created
	assert.Equal(t, 2, len(capturedSARs))
	verbs := []string{capturedSARs[0].Spec.ResourceAttributes.Verb, capturedSARs[1].Spec.ResourceAttributes.Verb}
	assert.Contains(t, verbs, "list")
	assert.Contains(t, verbs, "watch")
}

func TestDesktopAuthorizer_IsAllowedInformer_ListPermissionDenied(t *testing.T) {
	setNamespace := "test-namespace"
	setGVR := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	// Prepare fake clientset
	clientset := fake.NewClientset()
	clientset.Fake.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		createAction := action.(ktesting.CreateAction)
		obj := createAction.GetObject().(*authorizationv1.SelfSubjectAccessReview)

		var resp *authorizationv1.SelfSubjectAccessReview

		// Reject list request
		if obj.Spec.ResourceAttributes.Verb == "list" {
			resp = &authorizationv1.SelfSubjectAccessReview{
				ObjectMeta: metav1.ObjectMeta{Name: "ssar-response"},
				Spec: authorizationv1.SelfSubjectAccessReviewSpec{
					ResourceAttributes: &authorizationv1.ResourceAttributes{
						Namespace: setNamespace,
						Group:     setGVR.Group,
						Verb:      "list",
						Resource:  setGVR.Resource,
					},
				},
				Status: authorizationv1.SubjectAccessReviewStatus{
					Allowed: false,
				},
			}
		} else {
			resp = &authorizationv1.SelfSubjectAccessReview{
				ObjectMeta: metav1.ObjectMeta{Name: "ssar-response"},
				Status: authorizationv1.SubjectAccessReviewStatus{
					Allowed: true,
				},
			}
		}

		return true, resp, nil
	})

	// Create authorizer and test
	authorizer := NewDesktopAuthorizer()
	err := authorizer.IsAllowedInformer(context.Background(), clientset, setNamespace, setGVR)
	assert.Error(t, err)
}

func TestDesktopAuthorizer_IsAllowedInformer_WatchPermissionDenied(t *testing.T) {
	setNamespace := "test-namespace"
	setGVR := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	// Prepare fake clientset
	clientset := fake.NewClientset()
	clientset.Fake.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		createAction := action.(ktesting.CreateAction)
		obj := createAction.GetObject().(*authorizationv1.SelfSubjectAccessReview)

		var resp *authorizationv1.SelfSubjectAccessReview

		// Reject watch request
		if obj.Spec.ResourceAttributes.Verb == "watch" {
			resp = &authorizationv1.SelfSubjectAccessReview{
				ObjectMeta: metav1.ObjectMeta{Name: "ssar-response"},
				Spec: authorizationv1.SelfSubjectAccessReviewSpec{
					ResourceAttributes: &authorizationv1.ResourceAttributes{
						Namespace: setNamespace,
						Group:     setGVR.Group,
						Verb:      "list",
						Resource:  setGVR.Resource,
					},
				},
				Status: authorizationv1.SubjectAccessReviewStatus{
					Allowed: false,
				},
			}
		} else {
			resp = &authorizationv1.SelfSubjectAccessReview{
				ObjectMeta: metav1.ObjectMeta{Name: "ssar-response"},
				Status: authorizationv1.SubjectAccessReviewStatus{
					Allowed: true,
				},
			}
		}

		return true, resp, nil
	})

	// Create authorizer and test
	authorizer := NewDesktopAuthorizer()
	err := authorizer.IsAllowedInformer(context.Background(), clientset, setNamespace, setGVR)
	assert.Error(t, err)
}

func TestDesktopAuthorizer_IsAllowedInformer_Error(t *testing.T) {
	setNamespace := "test-namespace"
	setGVR := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	// Prepare fake clientset
	clientset := fake.NewClientset()
	clientset.Fake.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		createAction := action.(ktesting.CreateAction)
		obj := createAction.GetObject().(*authorizationv1.SelfSubjectAccessReview)

		// Reject watch request
		if obj.Spec.ResourceAttributes.Verb == "watch" {
			return true, nil, errors.New("API server error")
		}

		resp := &authorizationv1.SelfSubjectAccessReview{
			ObjectMeta: metav1.ObjectMeta{Name: "ssar-response"},
			Status: authorizationv1.SubjectAccessReviewStatus{
				Allowed: true,
			},
		}

		return true, resp, nil
	})

	// Create authorizer and test
	authorizer := NewDesktopAuthorizer()
	err := authorizer.IsAllowedInformer(context.Background(), clientset, setNamespace, setGVR)
	assert.Error(t, err)
}

func TestDesktopAuthorizer_IsAllowedInformer_CacheExpiry(t *testing.T) {
	setNamespace := "test-namespace"
	setGVR := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	// Prepare fake clientset
	capturedSARs := []*authorizationv1.SelfSubjectAccessReview{}

	clientset := fake.NewClientset()
	clientset.Fake.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		createAction := action.(ktesting.CreateAction)
		obj := createAction.GetObject().(*authorizationv1.SelfSubjectAccessReview)
		capturedSARs = append(capturedSARs, obj)

		// Verify the request the helper builds
		if ra := obj.Spec.ResourceAttributes; ra == nil ||
			(ra.Verb != "list" && ra.Verb != "watch") || ra.Resource != setGVR.Resource || ra.Namespace != setNamespace {
			t.Fatalf("unexpected SSAR payload: %#v", obj.Spec.ResourceAttributes)
		}

		// Fabricate a positive response so the helper returns true
		return true, &authorizationv1.SelfSubjectAccessReview{
			ObjectMeta: metav1.ObjectMeta{Name: "ssar-response"},
			Status: authorizationv1.SubjectAccessReviewStatus{
				Allowed: true,
			},
		}, nil
	})

	// Create authorizer and test
	authorizer := &DefaultDesktopAuthorizer{
		cache: util.SyncMap[cacheKey, cacheValue]{},
	}
	err := authorizer.IsAllowedInformer(context.Background(), clientset, setNamespace, setGVR)
	assert.NoError(t, err)

	// Check call and cache
	var count int
	authorizer.cache.Range(func(key cacheKey, value cacheValue) bool {
		count++
		return true
	})

	assert.Equal(t, 2, len(capturedSARs))
	assert.Equal(t, 2, count)

	// Execute again and ensure cache was used
	err = authorizer.IsAllowedInformer(context.Background(), clientset, setNamespace, setGVR)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(capturedSARs))

	// Expire cache and try again
	authorizer.cache.Range(func(key cacheKey, value cacheValue) bool {
		value.expiration = time.Now().Add(-1 * time.Minute)
		authorizer.cache.Store(key, value) // Store the updated value back in the map
		return true
	})

	err = authorizer.IsAllowedInformer(context.Background(), clientset, setNamespace, setGVR)
	assert.NoError(t, err)
	assert.Equal(t, 4, len(capturedSARs))
}

type MockClientsetInitializer struct {
	mock.Mock
}

func (m *MockClientsetInitializer) newClientset(restConfig *rest.Config) (kubernetes.Interface, error) {
	args := m.Called(restConfig)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(kubernetes.Interface), args.Error(1)
}

func TestNewInClusterAuthorizer(t *testing.T) {
	authorizer := NewInClusterAuthorizer()
	_, ok := authorizer.(*DefaultInClusterAuthorizer)
	assert.True(t, ok, "NewInClusterAuthorizer should return a *DefaultInClusterAuthorizer")
}

func TestInClusterAuthorizer_IsAllowedInformer_CorrectBearerToken(t *testing.T) {
	setNamespace := "test-namespace"
	setGVR := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}
	expectedToken := "test-bearer-token"

	// Create a fake clientset that will accept any request
	clientset := fake.NewClientset()
	clientset.Fake.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &authorizationv1.SelfSubjectAccessReview{
			Status: authorizationv1.SubjectAccessReviewStatus{
				Allowed: true,
			},
		}, nil
	})

	// Create a mock clientsetInitializer that captures the restConfig
	mockClientsetInitializer := new(MockClientsetInitializer)

	// Set up the mock to capture the restConfig argument
	var capturedConfig *rest.Config
	mockClientsetInitializer.On("newClientset", mock.Anything).Run(func(args mock.Arguments) {
		// Capture the restConfig that was passed to newClientset
		capturedConfig = args.Get(0).(*rest.Config)
	}).Return(clientset, nil)

	// Create authorizer with mock
	authorizer := &DefaultInClusterAuthorizer{
		clientsetInitializer: mockClientsetInitializer,
		cache:                util.SyncMap[string, cacheValue]{},
	}

	// Use a dummy rest.Config for the test
	dummyConfig := &rest.Config{
		Host: "https://example.com",
	}

	// Call IsAllowedInformer with our test token
	err := authorizer.IsAllowedInformer(context.Background(), dummyConfig, expectedToken, setNamespace, setGVR)
	assert.NoError(t, err)

	// Verify the mock was called
	mockClientsetInitializer.AssertCalled(t, "newClientset", mock.Anything)

	// Verify that the restConfig passed to newClientset has the correct bearer token
	assert.NotNil(t, capturedConfig, "restConfig should not be nil")
	assert.Equal(t, expectedToken, capturedConfig.BearerToken, "BearerToken should match the token passed to IsAllowedInformer")
	assert.Equal(t, "", capturedConfig.BearerTokenFile, "BearerTokenFile should be empty when a token is provided")
	assert.Equal(t, dummyConfig.Host, capturedConfig.Host, "Host should be preserved from the original config")
}

func TestInClusterAuthorizer_IsAllowedInformer_ListPermissionDenied(t *testing.T) {
	setNamespace := "test-namespace"
	setGVR := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	// Prepare fake clientset
	clientset := fake.NewClientset()
	clientset.Fake.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		createAction := action.(ktesting.CreateAction)
		obj := createAction.GetObject().(*authorizationv1.SelfSubjectAccessReview)

		var resp *authorizationv1.SelfSubjectAccessReview

		// Reject list request
		if obj.Spec.ResourceAttributes.Verb == "list" {
			resp = &authorizationv1.SelfSubjectAccessReview{
				ObjectMeta: metav1.ObjectMeta{Name: "ssar-response"},
				Spec: authorizationv1.SelfSubjectAccessReviewSpec{
					ResourceAttributes: &authorizationv1.ResourceAttributes{
						Namespace: setNamespace,
						Group:     setGVR.Group,
						Verb:      "list",
						Resource:  setGVR.Resource,
					},
				},
				Status: authorizationv1.SubjectAccessReviewStatus{
					Allowed: false,
				},
			}
		} else {
			resp = &authorizationv1.SelfSubjectAccessReview{
				ObjectMeta: metav1.ObjectMeta{Name: "ssar-response"},
				Status: authorizationv1.SubjectAccessReviewStatus{
					Allowed: true,
				},
			}
		}

		return true, resp, nil
	})

	// Create mock clientsetInitializer
	mockClientsetInitializer := new(MockClientsetInitializer)
	mockClientsetInitializer.On("newClientset", mock.Anything).Return(clientset, nil)

	// Create authorizer with mock and test
	authorizer := &DefaultInClusterAuthorizer{
		clientsetInitializer: mockClientsetInitializer,
		cache:                util.SyncMap[string, cacheValue]{},
	}

	// Use a dummy rest.Config for the test
	dummyConfig := &rest.Config{}
	dummyToken := "test-token"
	err := authorizer.IsAllowedInformer(context.Background(), dummyConfig, dummyToken, setNamespace, setGVR)
	assert.Error(t, err)

	// Verify that the mock was called
	mockClientsetInitializer.AssertCalled(t, "newClientset", mock.Anything)
}

func TestInClusterAuthorizer_IsAllowedInformer_WatchPermissionDenied(t *testing.T) {
	setNamespace := "test-namespace"
	setGVR := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	// Prepare fake clientset
	clientset := fake.NewClientset()
	clientset.Fake.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		createAction := action.(ktesting.CreateAction)
		obj := createAction.GetObject().(*authorizationv1.SelfSubjectAccessReview)

		var resp *authorizationv1.SelfSubjectAccessReview

		// Reject watch request
		if obj.Spec.ResourceAttributes.Verb == "watch" {
			resp = &authorizationv1.SelfSubjectAccessReview{
				ObjectMeta: metav1.ObjectMeta{Name: "ssar-response"},
				Spec: authorizationv1.SelfSubjectAccessReviewSpec{
					ResourceAttributes: &authorizationv1.ResourceAttributes{
						Namespace: setNamespace,
						Group:     setGVR.Group,
						Verb:      "watch",
						Resource:  setGVR.Resource,
					},
				},
				Status: authorizationv1.SubjectAccessReviewStatus{
					Allowed: false,
				},
			}
		} else {
			resp = &authorizationv1.SelfSubjectAccessReview{
				ObjectMeta: metav1.ObjectMeta{Name: "ssar-response"},
				Status: authorizationv1.SubjectAccessReviewStatus{
					Allowed: true,
				},
			}
		}

		return true, resp, nil
	})

	// Create mock clientsetInitializer
	mockClientsetInitializer := new(MockClientsetInitializer)
	mockClientsetInitializer.On("newClientset", mock.Anything).Return(clientset, nil)

	// Create authorizer with mock and test
	authorizer := &DefaultInClusterAuthorizer{
		clientsetInitializer: mockClientsetInitializer,
		cache:                util.SyncMap[string, cacheValue]{},
	}

	// Use a dummy rest.Config for the test
	dummyConfig := &rest.Config{}
	dummyToken := "test-token"
	err := authorizer.IsAllowedInformer(context.Background(), dummyConfig, dummyToken, setNamespace, setGVR)
	assert.Error(t, err)

	// Verify that the mock was called
	mockClientsetInitializer.AssertCalled(t, "newClientset", mock.Anything)
}

func TestInClusterAuthorizer_IsAllowedInformer_Error(t *testing.T) {
	setNamespace := "test-namespace"
	setGVR := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	// Prepare fake clientset
	clientset := fake.NewClientset()
	clientset.Fake.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		createAction := action.(ktesting.CreateAction)
		obj := createAction.GetObject().(*authorizationv1.SelfSubjectAccessReview)

		// Reject watch request with an error
		if obj.Spec.ResourceAttributes.Verb == "watch" {
			return true, nil, errors.New("API server error")
		}

		resp := &authorizationv1.SelfSubjectAccessReview{
			ObjectMeta: metav1.ObjectMeta{Name: "ssar-response"},
			Status: authorizationv1.SubjectAccessReviewStatus{
				Allowed: true,
			},
		}

		return true, resp, nil
	})

	// Create mock clientsetInitializer
	mockClientsetInitializer := new(MockClientsetInitializer)
	mockClientsetInitializer.On("newClientset", mock.Anything).Return(clientset, nil)

	// Create authorizer with mock and test
	authorizer := &DefaultInClusterAuthorizer{
		clientsetInitializer: mockClientsetInitializer,
		cache:                util.SyncMap[string, cacheValue]{},
	}

	// Use a dummy rest.Config for the test
	dummyConfig := &rest.Config{}
	dummyToken := "test-token"
	err := authorizer.IsAllowedInformer(context.Background(), dummyConfig, dummyToken, setNamespace, setGVR)
	assert.Error(t, err)

	// Verify that the mock was called
	mockClientsetInitializer.AssertCalled(t, "newClientset", mock.Anything)
}

func TestInClusterAuthorizer_IsAllowedInformer_CacheExpiry(t *testing.T) {
	setNamespace := "test-namespace"
	setGVR := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	// Prepare fake clientset
	capturedSARs := []*authorizationv1.SelfSubjectAccessReview{}

	clientset := fake.NewClientset()
	clientset.Fake.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		createAction := action.(ktesting.CreateAction)
		obj := createAction.GetObject().(*authorizationv1.SelfSubjectAccessReview)
		capturedSARs = append(capturedSARs, obj)

		// Verify the request the helper builds
		if ra := obj.Spec.ResourceAttributes; ra == nil ||
			(ra.Verb != "list" && ra.Verb != "watch") || ra.Resource != setGVR.Resource || ra.Namespace != setNamespace {
			t.Fatalf("unexpected SSAR payload: %#v", obj.Spec.ResourceAttributes)
		}

		// Fabricate a positive response so the helper returns true
		return true, &authorizationv1.SelfSubjectAccessReview{
			ObjectMeta: metav1.ObjectMeta{Name: "ssar-response"},
			Status: authorizationv1.SubjectAccessReviewStatus{
				Allowed: true,
			},
		}, nil
	})

	// Create mock clientsetInitializer
	mockClientsetInitializer := new(MockClientsetInitializer)
	mockClientsetInitializer.On("newClientset", mock.Anything).Return(clientset, nil)

	// Create authorizer with mock and test
	authorizer := &DefaultInClusterAuthorizer{
		clientsetInitializer: mockClientsetInitializer,
		cache:                util.SyncMap[string, cacheValue]{},
	}

	// Use a dummy rest.Config for the test
	dummyConfig := &rest.Config{}
	dummyToken := "test-token"

	// First call
	err := authorizer.IsAllowedInformer(context.Background(), dummyConfig, dummyToken, setNamespace, setGVR)
	assert.NoError(t, err)

	// Check call and cache
	var count int
	authorizer.cache.Range(func(key string, value cacheValue) bool {
		count++
		return true
	})

	assert.Equal(t, 2, len(capturedSARs))
	assert.Equal(t, 2, count)

	// Verify the mock was used
	mockClientsetInitializer.AssertNumberOfCalls(t, "newClientset", 1)

	// Execute again and ensure cache was used (no new SARs)
	err = authorizer.IsAllowedInformer(context.Background(), dummyConfig, dummyToken, setNamespace, setGVR)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(capturedSARs))

	// Verify the mock was used
	mockClientsetInitializer.AssertNumberOfCalls(t, "newClientset", 2)

	// Expire cache and try again
	authorizer.cache.Range(func(key string, value cacheValue) bool {
		value.expiration = time.Now().Add(-1 * time.Minute)
		authorizer.cache.Store(key, value) // Store the updated value back in the map
		return true
	})

	// Call again after cache expiry
	err = authorizer.IsAllowedInformer(context.Background(), dummyConfig, dummyToken, setNamespace, setGVR)
	assert.NoError(t, err)
	assert.Equal(t, 4, len(capturedSARs))

	// Verify the mock was used
	mockClientsetInitializer.AssertNumberOfCalls(t, "newClientset", 3)
}

func TestInClusterAuthorizer_IsAllowedInformer_ClientsetInitializerError(t *testing.T) {
	setNamespace := "test-namespace"
	setGVR := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	// Create mock clientsetInitializer that returns an error
	mockClientsetInitializer := new(MockClientsetInitializer)
	expectedError := errors.New("failed to initialize clientset")
	mockClientsetInitializer.On("newClientset", mock.Anything).Return(nil, expectedError)

	// Create authorizer with mock
	authorizer := &DefaultInClusterAuthorizer{
		clientsetInitializer: mockClientsetInitializer,
		cache:                util.SyncMap[string, cacheValue]{},
	}

	// Use a dummy rest.Config for the test
	dummyConfig := &rest.Config{}
	dummyToken := "test-token"

	// Call should return the error from clientsetInitializer
	err := authorizer.IsAllowedInformer(context.Background(), dummyConfig, dummyToken, setNamespace, setGVR)
	assert.Error(t, err)
	assert.Equal(t, expectedError, err)

	// Verify the mock was called
	mockClientsetInitializer.AssertCalled(t, "newClientset", mock.Anything)
}

// Without per-identity cache keys an "allowed" answer for the first user
// would be served to every subsequent impersonated user, since under
// aggregation auth the bearer token is always empty.
func TestInClusterAuthorizer_IsAllowedInformer_CachePerImpersonatedIdentity(t *testing.T) {
	setNamespace := "test-namespace"
	setGVR := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	clientset := fake.NewClientset()
	clientset.Fake.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &authorizationv1.SelfSubjectAccessReview{
			Status: authorizationv1.SubjectAccessReviewStatus{Allowed: true},
		}, nil
	})

	mockInit := new(MockClientsetInitializer)
	mockInit.On("newClientset", mock.Anything).Return(clientset, nil)
	authorizer := &DefaultInClusterAuthorizer{
		clientsetInitializer: mockInit,
		cache:                util.SyncMap[string, cacheValue]{},
	}

	ctxAlice := context.WithValue(context.Background(), K8SImpersonateCtxKey, &ImpersonateInfo{User: "alice"})
	ctxBob := context.WithValue(context.Background(), K8SImpersonateCtxKey, &ImpersonateInfo{User: "bob"})

	assert.NoError(t, authorizer.IsAllowedInformer(ctxAlice, &rest.Config{}, "", setNamespace, setGVR))
	preBob := len(clientset.Fake.Actions())
	assert.NoError(t, authorizer.IsAllowedInformer(ctxBob, &rest.Config{}, "", setNamespace, setGVR))
	postBob := len(clientset.Fake.Actions())
	assert.Greater(t, postBob, preBob, "bob must miss alice's cache and re-issue SSARs")
}

// Extras are forwarded to kube-apiserver and can be consumed by webhook
// authorizers. Cache keys must include them so an allow decision for one
// credential/extras set cannot bleed into another request with the same
// user and groups but different authentication extras.
func TestInClusterAuthorizer_IsAllowedInformer_CachePerImpersonatedExtras(t *testing.T) {
	setNamespace := "test-namespace"
	setGVR := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	clientset := fake.NewClientset()
	clientset.Fake.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &authorizationv1.SelfSubjectAccessReview{
			Status: authorizationv1.SubjectAccessReviewStatus{Allowed: true},
		}, nil
	})

	mockInit := new(MockClientsetInitializer)
	mockInit.On("newClientset", mock.Anything).Return(clientset, nil)
	authorizer := &DefaultInClusterAuthorizer{
		clientsetInitializer: mockInit,
		cache:                util.SyncMap[string, cacheValue]{},
	}

	ctxCredentialA := context.WithValue(context.Background(), K8SImpersonateCtxKey, &ImpersonateInfo{
		User:   "alice",
		Groups: []string{"devs"},
		Extras: map[string][]string{"authentication.kubernetes.io/credential-id": {"cred-a"}},
	})
	ctxCredentialB := context.WithValue(context.Background(), K8SImpersonateCtxKey, &ImpersonateInfo{
		User:   "alice",
		Groups: []string{"devs"},
		Extras: map[string][]string{"authentication.kubernetes.io/credential-id": {"cred-b"}},
	})

	assert.NoError(t, authorizer.IsAllowedInformer(ctxCredentialA, &rest.Config{}, "", setNamespace, setGVR))
	preCredentialB := len(clientset.Fake.Actions())
	assert.NoError(t, authorizer.IsAllowedInformer(ctxCredentialB, &rest.Config{}, "", setNamespace, setGVR))
	postCredentialB := len(clientset.Fake.Actions())
	assert.Greater(t, postCredentialB, preCredentialB, "different extras must miss the cache and re-issue SSARs")
}

func TestInClusterAuthorizer_IsAllowedInformer_Success(t *testing.T) {
	setNamespace := "test-namespace"
	setGVR := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	// Prepare fake clientset
	capturedSARs := []*authorizationv1.SelfSubjectAccessReview{}

	clientset := fake.NewClientset()
	clientset.Fake.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		createAction := action.(ktesting.CreateAction)
		obj := createAction.GetObject().(*authorizationv1.SelfSubjectAccessReview)
		capturedSARs = append(capturedSARs, obj)

		// Verify the request the helper builds
		if ra := obj.Spec.ResourceAttributes; ra == nil ||
			(ra.Verb != "list" && ra.Verb != "watch") || ra.Resource != setGVR.Resource || ra.Namespace != setNamespace {
			t.Fatalf("unexpected SSAR payload: %#v", obj.Spec.ResourceAttributes)
		}

		// Fabricate a positive response so the helper returns true
		return true, &authorizationv1.SelfSubjectAccessReview{
			ObjectMeta: metav1.ObjectMeta{Name: "ssar-response"},
			Status: authorizationv1.SubjectAccessReviewStatus{
				Allowed: true,
			},
		}, nil
	})

	// Create mock clientsetInitializer
	mockClientsetInitializer := new(MockClientsetInitializer)
	mockClientsetInitializer.On("newClientset", mock.Anything).Return(clientset, nil)

	// Create authorizer with mock and test
	authorizer := &DefaultInClusterAuthorizer{
		clientsetInitializer: mockClientsetInitializer,
		cache:                util.SyncMap[string, cacheValue]{},
	}

	// Use a dummy rest.Config for the test
	dummyConfig := &rest.Config{}
	dummyToken := "test-token"
	err := authorizer.IsAllowedInformer(context.Background(), dummyConfig, dummyToken, setNamespace, setGVR)
	assert.NoError(t, err)

	// Verify the SSARs were created
	assert.Equal(t, 2, len(capturedSARs))
	verbs := []string{capturedSARs[0].Spec.ResourceAttributes.Verb, capturedSARs[1].Spec.ResourceAttributes.Verb}
	assert.Contains(t, verbs, "list")
	assert.Contains(t, verbs, "watch")

	// Verify that the mock was called with the expected arguments
	mockClientsetInitializer.AssertCalled(t, "newClientset", mock.Anything)

	// Verify the number of calls to the mock
	mockClientsetInitializer.AssertNumberOfCalls(t, "newClientset", 1)
}
