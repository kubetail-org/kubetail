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
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
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

	clientset := fake.NewSimpleClientset()
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
	clientset := fake.NewSimpleClientset()
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
	clientset := fake.NewSimpleClientset()
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
	clientset := fake.NewSimpleClientset()
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

	clientset := fake.NewSimpleClientset()
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
		cache: sync.Map{},
	}
	err := authorizer.IsAllowedInformer(context.Background(), clientset, setNamespace, setGVR)
	assert.NoError(t, err)

	// Check call and cache
	var count int
	authorizer.cache.Range(func(key, value any) bool {
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
	authorizer.cache.Range(func(key, value any) bool {
		v := value.(cacheValue)
		v.expiration = time.Now().Add(-1 * time.Minute)
		authorizer.cache.Store(key, v) // Store the updated value back in the map
		return true
	})

	err = authorizer.IsAllowedInformer(context.Background(), clientset, setNamespace, setGVR)
	assert.NoError(t, err)
	assert.Equal(t, 4, len(capturedSARs))
}

/*
func TestDesktopAuthorizer_IsAllowedInformer_WatchPermissionDenied(t *testing.T) {
	// Create mock clientset and auth client
	_, mockAuth := newMockClientset()

	// Set up expectations for list permission (allowed)
	listSAR := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Namespace: "test-namespace",
				Group:     "apps",
				Verb:      "list",
				Resource:  "deployments",
			},
		},
	}
	listResult := &authv1.SelfSubjectAccessReview{
		Status: authv1.SubjectAccessReviewStatus{
			Allowed: true,
		},
	}
	mockAuth.On("CreateSelfSubjectAccessReview", mock.Anything, listSAR, mock.Anything).Return(listResult, nil)

	// Set up expectations for watch permission (denied)
	watchSAR := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Namespace: "test-namespace",
				Group:     "apps",
				Verb:      "watch",
				Resource:  "deployments",
			},
		},
	}
	watchResult := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Namespace: "test-namespace",
				Group:     "apps",
				Verb:      "watch",
				Resource:  "deployments",
			},
		},
		Status: authv1.SubjectAccessReviewStatus{
			Allowed: false,
		},
	}
	mockAuth.On("CreateSelfSubjectAccessReview", mock.Anything, watchSAR, mock.Anything).Return(watchResult, nil)

	// Create authorizer and test
	authorizer := NewDesktopAuthorizer()
	gvr := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	err := authorizer.IsAllowedInformer(context.Background(), nil, "test-namespace", gvr)
	assert.Error(t, err)

	// Verify the error is a permission denied error
	statusErr, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, statusErr.Code())

	// Verify expectations
	mock.AssertExpectationsForObjects(t, &mockAuth.Mock)
}

func TestDesktopAuthorizer_IsAllowedInformer_APIError(t *testing.T) {
	// Create mock clientset and auth client
	_, mockAuth := newMockClientset()

	// Set up expectations for list permission with API error
	listSAR := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Namespace: "test-namespace",
				Group:     "apps",
				Verb:      "list",
				Resource:  "deployments",
			},
		},
	}
	expectedError := errors.New("API server error")
	mockAuth.On("CreateSelfSubjectAccessReview", mock.Anything, listSAR, mock.Anything).Return(nil, expectedError)

	// Create authorizer and test
	authorizer := NewDesktopAuthorizer()
	gvr := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	err := authorizer.IsAllowedInformer(context.Background(), nil, "test-namespace", gvr)
	assert.Error(t, err)
	assert.Equal(t, expectedError, err)

	// Verify expectations
	mock.AssertExpectationsForObjects(t, &mockAuth.Mock)
}

func TestDesktopAuthorizer_IsAllowedInformer_CacheHit(t *testing.T) {
	// Create mock clientset and auth client
	_, mockAuth := newMockClientset()

	// Set up expectations for list permission
	listSAR := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Namespace: "test-namespace",
				Group:     "apps",
				Verb:      "list",
				Resource:  "deployments",
			},
		},
	}
	listResult := &authv1.SelfSubjectAccessReview{
		Status: authv1.SubjectAccessReviewStatus{
			Allowed: true,
		},
	}
	mockAuth.On("CreateSelfSubjectAccessReview", mock.Anything, listSAR, mock.Anything).Return(listResult, nil)

	// Set up expectations for watch permission
	watchSAR := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Namespace: "test-namespace",
				Group:     "apps",
				Verb:      "watch",
				Resource:  "deployments",
			},
		},
	}
	watchResult := &authv1.SelfSubjectAccessReview{
		Status: authv1.SubjectAccessReviewStatus{
			Allowed: true,
		},
	}
	mockAuth.On("CreateSelfSubjectAccessReview", mock.Anything, watchSAR, mock.Anything).Return(watchResult, nil)

	// Create authorizer and test
	authorizer := NewDesktopAuthorizer()
	gvr := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	// First call should hit the API
	err := authorizer.IsAllowedInformer(context.Background(), nil, "test-namespace", gvr)
	assert.NoError(t, err)

	// Second call should use the cache
	err = authorizer.IsAllowedInformer(context.Background(), nil, "test-namespace", gvr)
	assert.NoError(t, err)

	// Verify expectations (API should only be called once)
	mock.AssertExpectationsForObjects(t, &mockAuth.Mock)
}

func TestDesktopAuthorizer_IsAllowedInformer_CacheExpiry(t *testing.T) {
	// Create a custom authorizer with a short cache TTL for testing
	authorizer := &DefaultDesktopAuthorizer{
		cache: sync.Map{},
	}

	// Create mock clientset and auth client
	_, mockAuth := newMockClientset()

	// Set up expectations for list permission (called twice)
	listSAR := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Namespace: "test-namespace",
				Group:     "apps",
				Verb:      "list",
				Resource:  "deployments",
			},
		},
	}
	listResult := &authv1.SelfSubjectAccessReview{
		Status: authv1.SubjectAccessReviewStatus{
			Allowed: true,
		},
	}
	mockAuth.On("CreateSelfSubjectAccessReview", mock.Anything, listSAR, mock.Anything).Return(listResult, nil).Twice()

	// Set up expectations for watch permission (called twice)
	watchSAR := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Namespace: "test-namespace",
				Group:     "apps",
				Verb:      "watch",
				Resource:  "deployments",
			},
		},
	}
	watchResult := &authv1.SelfSubjectAccessReview{
		Status: authv1.SubjectAccessReviewStatus{
			Allowed: true,
		},
	}
	mockAuth.On("CreateSelfSubjectAccessReview", mock.Anything, watchSAR, mock.Anything).Return(watchResult, nil).Twice()

	gvr := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	// First call should hit the API
	err := authorizer.IsAllowedInformer(context.Background(), nil, "test-namespace", gvr)
	assert.NoError(t, err)

	// Manually expire the cache by setting expired cache entries
	key1 := cacheKey{
		namespace: "test-namespace",
		group:     "apps",
		resource:  "deployments",
		verb:      "list",
	}
	key2 := cacheKey{
		namespace: "test-namespace",
		group:     "apps",
		resource:  "deployments",
		verb:      "watch",
	}
	authorizer.cache.Store(key1, cacheValue{
		allowed:    true,
		expiration: time.Now().Add(-1 * time.Minute), // Expired
	})
	authorizer.cache.Store(key2, cacheValue{
		allowed:    true,
		expiration: time.Now().Add(-1 * time.Minute), // Expired
	})

	// Second call should hit the API again because cache is expired
	err = authorizer.IsAllowedInformer(context.Background(), nil, "test-namespace", gvr)
	assert.NoError(t, err)

	// Verify expectations (API should be called twice)
	mock.AssertExpectationsForObjects(t, &mockAuth.Mock)
}
*/
