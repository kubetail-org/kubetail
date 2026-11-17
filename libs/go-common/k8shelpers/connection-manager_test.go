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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// MockDesktopAuthorizer is a mock implementation of DesktopAuthorizer for testing
type MockDesktopAuthorizer struct {
	mock.Mock
}

// IsAllowedInformer is a mock implementation of the DesktopAuthorizer.IsAllowedInformer method
func (m *MockDesktopAuthorizer) IsAllowedInformer(ctx context.Context, clientset kubernetes.Interface, namespace string, gvr schema.GroupVersionResource) error {
	args := m.Called(ctx, clientset, namespace, gvr)
	return args.Error(0)
}

func TestDesktopConnectionManager_NewInformer_AuthorizationFailure(t *testing.T) {
	// Set up the expected error
	expectedError := errors.New("authorization failed")

	// Create a mock authorizer
	mockAuthorizer := new(MockDesktopAuthorizer)
	mockAuthorizer.On("IsAllowedInformer",
		mock.Anything,    // context
		mock.Anything,    // clientset
		"test-namespace", // namespace
		mock.MatchedBy(func(gvr schema.GroupVersionResource) bool {
			return gvr.Group == "apps" && gvr.Version == "v1" && gvr.Resource == "deployments"
		}), // gvr
	).Return(expectedError)

	// Create DesktopConnectionManager with the mock authorizer
	cm := &DesktopConnectionManager{
		authorizer: mockAuthorizer,
	}

	cm.csCache.LoadOrCompute("test-context", func() (*kubernetes.Clientset, error) {
		return &kubernetes.Clientset{}, nil
	})

	// Set up test parameters
	ctx := context.Background()
	kubeContext := "test-context"
	token := "" // Empty as tokens are not used in DesktopConnectionManager
	namespace := "test-namespace"
	gvr := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	// Call the method under test
	informer, startFn, err := cm.NewInformer(ctx, kubeContext, token, namespace, gvr)
	assert.Nil(t, informer)
	assert.Nil(t, startFn)
	assert.Error(t, err)
	assert.Equal(t, expectedError, err)

	// Verify that the mock was called as expected
	mockAuthorizer.AssertExpectations(t)
}

// MockInClusterAuthorizer is a mock implementation of InClusterAuthorizer for testing
type MockInClusterAuthorizer struct {
	mock.Mock
}

// IsAllowedInformer is a mock implementation of the InClusterAuthorizer.IsAllowedInformer method
func (m *MockInClusterAuthorizer) IsAllowedInformer(ctx context.Context, restConfig *rest.Config, token string, namespace string, gvr schema.GroupVersionResource) error {
	args := m.Called(ctx, restConfig, token, namespace, gvr)
	return args.Error(0)
}

func TestInClusterConnectionManager_NewInformer_AuthorizationFailure(t *testing.T) {
	// Set up the expected error
	expectedError := errors.New("authorization failed")

	// Create a mock authorizer
	mockAuthorizer := new(MockInClusterAuthorizer)
	mockAuthorizer.On("IsAllowedInformer",
		mock.Anything,    // context
		mock.Anything,    // restConfig
		"test-token",     // token
		"test-namespace", // namespace
		mock.MatchedBy(func(gvr schema.GroupVersionResource) bool {
			return gvr.Group == "apps" && gvr.Version == "v1" && gvr.Resource == "deployments"
		}), // gvr
	).Return(expectedError)

	// Create InClusterConnectionManager with the mock authorizer
	cm := &InClusterConnectionManager{
		restConfig:   &rest.Config{},
		authorizer:   mockAuthorizer,
		stopCh:       make(chan struct{}),
		factoryCache: make(map[string]informers.SharedInformerFactory),
	}

	// Set up test parameters
	ctx := context.Background()
	kubeContext := "" // Empty as it's not supported in InClusterConnectionManager
	token := "test-token"
	namespace := "test-namespace"
	gvr := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	// Call the method under test
	informer, startFn, err := cm.NewInformer(ctx, kubeContext, token, namespace, gvr)

	// Verify the results
	assert.Nil(t, informer)
	assert.Nil(t, startFn)
	assert.Error(t, err)
	assert.Equal(t, expectedError, err)

	// Verify that the mock was called as expected
	mockAuthorizer.AssertExpectations(t)
}

// Pins the wiring that propagates the originating user's identity to
// kube-apiserver. The in-cluster REST config must wrap its transport with
// both ImpersonatingRoundTripper (cluster-api → aggregation layer path) and
// BearerTokenRoundTripper (dashboard auth-mode: token path). A regression
// that drops either would silently downgrade per-user calls to run as the
// pod ServiceAccount, bypassing the caller's RBAC.
func TestInClusterConnectionManager_RestConfigWrapsTransportWithImpersonation(t *testing.T) {
	orig := inClusterConfigFn
	t.Cleanup(func() { inClusterConfigFn = orig })
	inClusterConfigFn = func() (*rest.Config, error) {
		return &rest.Config{Host: "https://example.invalid"}, nil
	}

	cm, err := NewInClusterConnectionManager()
	require.NoError(t, err)

	rc, err := cm.GetOrCreateRestConfig("")
	require.NoError(t, err)
	require.NotNil(t, rc.WrapTransport, "WrapTransport must be set so per-request identity headers reach kube-apiserver")

	inner := http.DefaultTransport
	wrapped := rc.WrapTransport(inner)
	impRT, ok := wrapped.(*ImpersonatingRoundTripper)
	require.Truef(t, ok, "WrapTransport must produce *ImpersonatingRoundTripper; got %T", wrapped)
	bearerRT, ok := impRT.Transport.(*BearerTokenRoundTripper)
	require.Truef(t, ok, "ImpersonatingRoundTripper must wrap *BearerTokenRoundTripper so dashboard token-auth still forwards the user's bearer token; got %T", impRT.Transport)
	assert.Same(t, inner, bearerRT.Transport, "BearerTokenRoundTripper must wrap the underlying transport, not replace it")
}

// End-to-end-ish coverage of the in-cluster identity-propagation chain: builds
// a real http.Client from the connection manager's rest.Config and confirms
// that the headers reaching the upstream depend on which context key the
// caller set. This guards against future refactors that swap the
// WrapTransport composition and silently downgrade per-user calls to the
// pod ServiceAccount.
func TestInClusterConnectionManager_RestConfigPropagatesIdentityHeaders(t *testing.T) {
	type captured struct {
		authorization   string
		impersonateUser string
		impersonateGrp  []string
	}

	var got captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = captured{
			authorization:   r.Header.Get("Authorization"),
			impersonateUser: r.Header.Get("Impersonate-User"),
			impersonateGrp:  r.Header.Values("Impersonate-Group"),
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	orig := inClusterConfigFn
	t.Cleanup(func() { inClusterConfigFn = orig })
	inClusterConfigFn = func() (*rest.Config, error) {
		// Bare config — no BearerToken/BearerTokenFile so we can observe
		// exactly what the WrapTransport chain contributes per request.
		return &rest.Config{Host: srv.URL}, nil
	}

	cm, err := NewInClusterConnectionManager()
	require.NoError(t, err)

	rc, err := cm.GetOrCreateRestConfig("")
	require.NoError(t, err)

	client, err := rest.HTTPClientFor(rc)
	require.NoError(t, err)

	tests := []struct {
		name     string
		ctx      func() context.Context
		wantAuth string
		wantUser string
		wantGrps []string
	}{
		{
			name:     "no identity on context — unrelated callers (health checks) pass through unchanged",
			ctx:      context.Background,
			wantAuth: "",
			wantUser: "",
			wantGrps: nil,
		},
		{
			name: "K8STokenCtxKey only — dashboard auth-mode: token must forward the user's bearer token",
			ctx: func() context.Context {
				return context.WithValue(context.Background(), K8STokenCtxKey, "user-token-abc")
			},
			wantAuth: "Bearer user-token-abc",
			wantUser: "",
			wantGrps: nil,
		},
		{
			name: "K8SImpersonateCtxKey only — cluster-api aggregation path must impersonate the originating user",
			ctx: func() context.Context {
				return context.WithValue(context.Background(), K8SImpersonateCtxKey, &ImpersonateInfo{
					User:   "alice@example.com",
					Groups: []string{"system:authenticated", "devs"},
				})
			},
			wantAuth: "",
			wantUser: "alice@example.com",
			wantGrps: []string{"system:authenticated", "devs"},
		},
		{
			name: "both keys present — both wrappers contribute headers independently",
			ctx: func() context.Context {
				ctx := context.WithValue(context.Background(), K8STokenCtxKey, "user-token-xyz")
				return context.WithValue(ctx, K8SImpersonateCtxKey, &ImpersonateInfo{User: "bob"})
			},
			wantAuth: "Bearer user-token-xyz",
			wantUser: "bob",
			wantGrps: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got = captured{}
			req, err := http.NewRequestWithContext(tt.ctx(), http.MethodGet, srv.URL+"/healthz", nil)
			require.NoError(t, err)

			resp, err := client.Do(req)
			require.NoError(t, err)
			resp.Body.Close()

			assert.Equal(t, tt.wantAuth, got.authorization, "Authorization header")
			assert.Equal(t, tt.wantUser, got.impersonateUser, "Impersonate-User header")
			assert.Equal(t, tt.wantGrps, got.impersonateGrp, "Impersonate-Group headers")
		})
	}
}

func TestInClusterConnectionManager_NewInformer_KubeContextNotSupported(t *testing.T) {
	// Create a mock authorizer
	mockAuthorizer := new(MockInClusterAuthorizer)

	// Create InClusterConnectionManager with the mock authorizer
	cm := &InClusterConnectionManager{
		authorizer:   mockAuthorizer,
		stopCh:       make(chan struct{}),
		factoryCache: make(map[string]informers.SharedInformerFactory),
	}

	// Set up test parameters with a non-empty kubeContext
	ctx := context.Background()
	kubeContext := "some-context" // This should cause an error as it's not supported
	token := "test-token"
	namespace := "test-namespace"
	gvr := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	// Call the method under test
	informer, startFn, err := cm.NewInformer(ctx, kubeContext, token, namespace, gvr)

	// Verify the results
	assert.Nil(t, informer)
	assert.Nil(t, startFn)
	assert.Error(t, err)

	// The mock should not have been called since the error happens before authorization check
	mockAuthorizer.AssertNotCalled(t, "IsAllowedInformer")
}
