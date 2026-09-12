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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"

	"github.com/kubetail-org/kubetail/modules/shared/httphelpers"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
)

func TestInClusterProxy_StripsOriginHeader(t *testing.T) {
	var capturedOrigin string
	var captured bool
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedOrigin = r.Header.Get("Origin")
		captured = true
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxy, err := newInClusterProxy(backend.URL, "/prefix", nil, http.DefaultTransport)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/prefix/somepath", nil)
	// httptest.NewRequest uses example.com as the request Host
	req.Header.Set("Origin", "https://example.com")

	proxy.ServeHTTP(httptest.NewRecorder(), req)

	require.True(t, captured, "backend was not called")
	assert.Empty(t, capturedOrigin, "Origin header must be stripped before forwarding")
}

func TestInClusterProxy_RejectsCrossOriginUpgradeRequest(t *testing.T) {
	tests := []struct {
		name   string
		origin string
	}{
		{"cross-origin Origin", "https://evil.example.com"},
		{"missing Origin", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured bool
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured = true
				w.WriteHeader(http.StatusOK)
			}))
			defer backend.Close()

			proxy, err := newInClusterProxy(backend.URL, "/prefix", nil, http.DefaultTransport)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodGet, "/prefix/somepath", nil)
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Connection", "Upgrade")
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			rec := httptest.NewRecorder()
			proxy.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusForbidden, rec.Code)
			assert.False(t, captured, "backend must not be reached on cross-origin upgrade")
		})
	}
}

func TestInClusterProxy_RejectsPathTraversal(t *testing.T) {
	tests := []string{
		"/prefix/ctx/../../api/v1/pods",
		"/prefix/..",
		"/prefix/foo/../../bar",
		// Go's URL parser decodes %2e%2e into ".." in r.URL.Path before the
		// handler runs, so the segment check still catches it.
		"/prefix/ctx/%2e%2e/api/v1/pods",
	}

	for _, p := range tests {
		t.Run(p, func(t *testing.T) {
			var captured bool
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured = true
				w.WriteHeader(http.StatusOK)
			}))
			defer backend.Close()

			proxy, err := newInClusterProxy(backend.URL, "/prefix", nil, http.DefaultTransport)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodGet, "http://example.com"+p, nil)
			rec := httptest.NewRecorder()
			proxy.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusForbidden, rec.Code)
			assert.False(t, captured, "backend must not be reached on traversal")
		})
	}
}

func TestDesktopProxy_RejectsPathTraversal(t *testing.T) {
	tests := []string{
		"/prefix/ctx/../../api/v1/pods",
		"/prefix/ctx/foo/../../bar",
		"/prefix/ctx/%2e%2e/api/v1/pods",
	}

	for _, p := range tests {
		t.Run(p, func(t *testing.T) {
			var captured bool
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured = true
				w.WriteHeader(http.StatusOK)
			})

			proxy, err := NewDesktopProxy(nil, "/prefix", nil)
			require.NoError(t, err)
			proxy.phCache["ctx"] = handler

			req := httptest.NewRequest(http.MethodGet, "http://example.com"+p, nil)
			rec := httptest.NewRecorder()
			proxy.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusForbidden, rec.Code)
			assert.False(t, captured, "backend handler must not be reached on traversal")
		})
	}
}

func TestInClusterProxy_AllowedOriginsAcceptsCrossHostUpgrade(t *testing.T) {
	var captured bool
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = true
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxy, err := newInClusterProxy(backend.URL, "/prefix", []string{"https://allowed.example.com"}, http.DefaultTransport)
	require.NoError(t, err)

	// Request Host is example.com (httptest default) but Origin matches the
	// allowlist — emulates a Host-rewriting reverse proxy in front.
	req := httptest.NewRequest(http.MethodGet, "/prefix/somepath", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Origin", "https://allowed.example.com")
	req.Header.Set(httphelpers.HeaderForwardedCSRFToken, "test-csrf")

	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	assert.NotEqual(t, http.StatusForbidden, rec.Code)
	assert.True(t, captured, "allowlisted upgrade must reach the backend")
}

func TestInClusterProxy_ForwardsUserTokenAsAuthorization(t *testing.T) {
	tests := []struct {
		name      string
		userToken string
		clientHdr string
		wantAuth  string
		wantFwd   string
	}{
		{
			name:      "forwards user token as Authorization",
			userToken: "user-token-123",
			wantAuth:  "Bearer user-token-123",
		},
		{
			name:      "no Authorization without user token",
			userToken: "",
			wantAuth:  "",
		},
		{
			name:      "ignores client-supplied Authorization without a session token",
			userToken: "",
			clientHdr: "Bearer attacker",
			wantAuth:  "",
		},
		{
			// Defense-in-depth: a malicious client header must never tunnel
			// past the user's session identity. The Director deletes
			// Authorization before re-setting from ctx, so the attacker's
			// token can't end up forwarded — but only this test pins it.
			name:      "client-supplied Authorization replaced by session token",
			userToken: "user-token-123",
			clientHdr: "Bearer attacker",
			wantAuth:  "Bearer user-token-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedAuth, capturedFwd string
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedAuth = r.Header.Get("Authorization")
				capturedFwd = r.Header.Get("X-Forwarded-Authorization")
				w.WriteHeader(http.StatusOK)
			}))
			defer backend.Close()

			proxy, err := newInClusterProxy(backend.URL, "/prefix", nil, http.DefaultTransport)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodGet, "/prefix/somepath", nil)
			if tt.clientHdr != "" {
				req.Header.Set("Authorization", tt.clientHdr)
			}
			if tt.userToken != "" {
				ctx := context.WithValue(req.Context(), k8shelpers.K8STokenCtxKey, tt.userToken)
				req = req.WithContext(ctx)
			}

			proxy.ServeHTTP(httptest.NewRecorder(), req)

			assert.Equal(t, tt.wantAuth, capturedAuth)
			assert.Equal(t, tt.wantFwd, capturedFwd)
		})
	}
}

// Verifies the SA-token fallback behavior introduced when NewInClusterProxy
// switched from rest.AnonymousClientConfig to rest.TransportFor(restConfig).
// With a non-anonymous transport, BearerAuthRoundTripper injects the
// dashboard's SA token only when the Director has not already set
// Authorization from a user-supplied bearer token.
func TestInClusterProxy_FallsBackToServiceAccountTokenWithoutUserToken(t *testing.T) {
	const saToken = "dashboard-sa-token"

	tests := []struct {
		name      string
		userToken string
		wantAuth  string
	}{
		{
			name:      "user token forwarded, SA token suppressed",
			userToken: "user-token-123",
			wantAuth:  "Bearer user-token-123",
		},
		{
			name:      "SA token used when no user token in context",
			userToken: "",
			wantAuth:  "Bearer " + saToken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedAuth string
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedAuth = r.Header.Get("Authorization")
				w.WriteHeader(http.StatusOK)
			}))
			defer backend.Close()

			transport, err := rest.TransportFor(&rest.Config{BearerToken: saToken})
			require.NoError(t, err)

			proxy, err := newInClusterProxy(backend.URL, "/prefix", nil, transport)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodGet, "/prefix/somepath", nil)
			if tt.userToken != "" {
				ctx := context.WithValue(req.Context(), k8shelpers.K8STokenCtxKey, tt.userToken)
				req = req.WithContext(ctx)
			}

			proxy.ServeHTTP(httptest.NewRecorder(), req)

			assert.Equal(t, tt.wantAuth, capturedAuth)
		})
	}
}

func TestInClusterProxy_RewritesPathToAggregationLayer(t *testing.T) {
	var capturedPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxy, err := newInClusterProxy(backend.URL, "/prefix", nil, http.DefaultTransport)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/prefix/graphql", nil)
	proxy.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "/apis/api.kubetail.com/v1/graphql", capturedPath)
}

// --- InClusterProxy drain/shutdown tests ---

func TestInClusterProxy_DrainWithContext_NoConnections(t *testing.T) {
	proxy, err := newInClusterProxy("http://localhost", "/prefix", nil, http.DefaultTransport)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	require.NoError(t, proxy.DrainWithContext(ctx))
}

func TestInClusterProxy_DrainWithContext_CancelledContext(t *testing.T) {
	proxy, err := newInClusterProxy("http://localhost", "/prefix", nil, http.DefaultTransport)
	require.NoError(t, err)

	// Simulate an open connection that never finishes
	proxy.wg.Add(1)
	defer proxy.wg.Done()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = proxy.DrainWithContext(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestInClusterProxy_DrainWithContext_DeadlineExceeded(t *testing.T) {
	proxy, err := newInClusterProxy("http://localhost", "/prefix", nil, http.DefaultTransport)
	require.NoError(t, err)

	// Simulate an open connection that never finishes
	proxy.wg.Add(1)
	defer proxy.wg.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err = proxy.DrainWithContext(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// newWSBackend creates a test HTTP server that upgrades to WebSocket and signals
// on the returned channel once the connection is accepted. The backend holds the
// connection open until the client disconnects.
func newWSBackend(t *testing.T) (*httptest.Server, <-chan struct{}) {
	t.Helper()
	connected := make(chan struct{}, 1)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		connected <- struct{}{}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(backend.Close)
	return backend, connected
}

// wsOriginHeader returns an Origin header matching the given server URL, so
// a WebSocket dial passes the proxy's same-origin gate. It also stamps a
// non-empty X-Forwarded-CSRF-Token to satisfy the proxy's session-presence
// check (normally set by websocketCSRFContextMiddleware upstream).
func wsOriginHeader(serverURL string) http.Header {
	return http.Header{
		"Origin":                             []string{serverURL},
		httphelpers.HeaderForwardedCSRFToken: []string{"test-csrf"},
	}
}

// waitConnected waits for the backend to accept a connection or fails the test.
func waitConnected(t *testing.T, connected <-chan struct{}) {
	t.Helper()
	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for backend WebSocket connection")
	}
}

func TestInClusterProxy_NotifyShutdown_ClosesConnections(t *testing.T) {
	backend, connected := newWSBackend(t)

	proxy, err := newInClusterProxy(backend.URL, "/prefix", nil, http.DefaultTransport)
	require.NoError(t, err)

	proxyServer := httptest.NewServer(proxy)
	defer proxyServer.Close()

	// Dial WebSocket through the proxy
	wsURL := "ws" + proxyServer.URL[4:] + "/prefix/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, wsOriginHeader(proxyServer.URL))
	require.NoError(t, err)
	defer conn.Close()

	waitConnected(t, connected)

	// Signal shutdown — should close the hijacked connection
	proxy.NotifyShutdown()

	// The connection should be closed by the proxy
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, readErr := conn.ReadMessage()
	require.Error(t, readErr)

	// All connections should be drained
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, proxy.DrainWithContext(ctx))
}

func TestInClusterProxy_NotifyShutdown_ClosesMultipleConnections(t *testing.T) {
	backend, connected := newWSBackend(t)

	proxy, err := newInClusterProxy(backend.URL, "/prefix", nil, http.DefaultTransport)
	require.NoError(t, err)

	proxyServer := httptest.NewServer(proxy)
	defer proxyServer.Close()

	wsURL := "ws" + proxyServer.URL[4:] + "/prefix/ws"

	const numConns = 3
	conns := make([]*websocket.Conn, numConns)

	for i := range numConns {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, wsOriginHeader(proxyServer.URL))
		require.NoError(t, err)
		defer conn.Close()
		waitConnected(t, connected)
		conns[i] = conn
	}

	// Signal shutdown
	proxy.NotifyShutdown()

	// All connections should be closed by the proxy
	for i, conn := range conns {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, readErr := conn.ReadMessage()
		require.Error(t, readErr, "connection %d should be closed", i)
	}

	// All connections should be drained
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, proxy.DrainWithContext(ctx))
}

// --- DesktopProxy drain/shutdown tests ---

func TestDesktopProxy_StripsOriginHeader(t *testing.T) {
	var capturedOrigin string
	var captured bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedOrigin = r.Header.Get("Origin")
		captured = true
		w.WriteHeader(http.StatusOK)
	})

	proxy, err := NewDesktopProxy(nil, "/prefix", nil)
	require.NoError(t, err)
	proxy.phCache["ctx"] = handler

	req := httptest.NewRequest(http.MethodGet, "/prefix/ctx/relpath", nil)
	req.Header.Set("Origin", "https://example.com")

	proxy.ServeHTTP(httptest.NewRecorder(), req)

	require.True(t, captured, "backend handler was not called")
	assert.Empty(t, capturedOrigin, "Origin header must be stripped before forwarding")
}

func TestDesktopProxy_RewritesPathToAggregationLayer(t *testing.T) {
	var capturedPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})

	proxy, err := NewDesktopProxy(nil, "/prefix", nil)
	require.NoError(t, err)
	proxy.phCache["ctx"] = handler

	req := httptest.NewRequest(http.MethodPost, "/prefix/ctx/graphql", nil)
	proxy.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "/apis/api.kubetail.com/v1/graphql", capturedPath)
}

func TestDesktopProxy_DoesNotInjectAuthorizationHeader(t *testing.T) {
	var capturedAuth, capturedFwd string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedFwd = r.Header.Get("X-Forwarded-Authorization")
		w.WriteHeader(http.StatusOK)
	})

	proxy, err := NewDesktopProxy(nil, "/prefix", nil)
	require.NoError(t, err)
	proxy.phCache["ctx"] = handler

	req := httptest.NewRequest(http.MethodGet, "/prefix/ctx/relpath", nil)
	// Client-supplied attempts must not be forwarded — under aggregation auth
	// the kubectl proxy adds Authorization from the kubeconfig; X-Forwarded-
	// Authorization is no longer used for identity propagation.
	req.Header.Set("X-Forwarded-Authorization", "Bearer attacker-token")
	proxy.ServeHTTP(httptest.NewRecorder(), req)

	assert.Empty(t, capturedAuth, "kubectl proxy handler attaches Authorization itself; the request reaching it must not carry one")
	assert.Empty(t, capturedFwd, "X-Forwarded-Authorization must not be forwarded")
}

func TestDesktopProxy_StripsImpersonationHeaders(t *testing.T) {
	var captured http.Header
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	})

	proxy, err := NewDesktopProxy(nil, "/prefix", nil)
	require.NoError(t, err)
	proxy.phCache["ctx"] = handler

	req := httptest.NewRequest(http.MethodGet, "/prefix/ctx/relpath", nil)
	req.Header.Set("Impersonate-User", "system:admin")
	req.Header.Set("Impersonate-Group", "system:masters")
	req.Header.Set("Impersonate-Uid", "abc")
	req.Header.Set("Impersonate-Extra-Reason", "testing")
	proxy.ServeHTTP(httptest.NewRecorder(), req)

	for k := range captured {
		assert.False(t, strings.HasPrefix(strings.ToLower(k), "impersonate-"),
			"client-supplied %s must not be forwarded to kube-apiserver", k)
	}
}

func TestInClusterProxy_StripsImpersonationHeaders(t *testing.T) {
	var captured http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxy, err := newInClusterProxy(backend.URL, "/prefix", nil, http.DefaultTransport)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/prefix/somepath", nil)
	req.Header.Set("Impersonate-User", "system:admin")
	req.Header.Set("Impersonate-Group", "system:masters")
	req.Header.Set("Impersonate-Uid", "abc")
	req.Header.Set("Impersonate-Extra-Reason", "testing")
	proxy.ServeHTTP(httptest.NewRecorder(), req)

	for k := range captured {
		assert.False(t, strings.HasPrefix(strings.ToLower(k), "impersonate-"),
			"client-supplied %s must not be forwarded to kube-apiserver", k)
	}
}

func TestDesktopProxy_RejectsCrossOriginUpgradeRequest(t *testing.T) {
	tests := []struct {
		name   string
		origin string
	}{
		{"cross-origin Origin", "https://evil.example.com"},
		{"missing Origin", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured bool
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured = true
				w.WriteHeader(http.StatusOK)
			})

			proxy, err := NewDesktopProxy(nil, "/prefix", nil)
			require.NoError(t, err)
			proxy.phCache["ctx"] = handler

			req := httptest.NewRequest(http.MethodGet, "/prefix/ctx/relpath", nil)
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Connection", "Upgrade")
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			rec := httptest.NewRecorder()
			proxy.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusForbidden, rec.Code)
			assert.False(t, captured, "backend handler must not be reached on cross-origin upgrade")
		})
	}
}

func TestDesktopProxy_AllowedOriginsAcceptsCrossHostUpgrade(t *testing.T) {
	var captured bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = true
		w.WriteHeader(http.StatusOK)
	})

	proxy, err := NewDesktopProxy(nil, "/prefix", []string{"https://allowed.example.com"})
	require.NoError(t, err)
	proxy.phCache["ctx"] = handler

	req := httptest.NewRequest(http.MethodGet, "/prefix/ctx/relpath", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Origin", "https://allowed.example.com")
	req.Header.Set(httphelpers.HeaderForwardedCSRFToken, "test-csrf")

	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	assert.NotEqual(t, http.StatusForbidden, rec.Code)
	assert.True(t, captured, "allowlisted upgrade must reach the backend handler")
}

func TestDesktopProxy_DrainWithContext_NoConnections(t *testing.T) {
	proxy, err := NewDesktopProxy(nil, "/prefix", nil)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	require.NoError(t, proxy.DrainWithContext(ctx))
}

func TestDesktopProxy_DrainWithContext_CancelledContext(t *testing.T) {
	proxy, err := NewDesktopProxy(nil, "/prefix", nil)
	require.NoError(t, err)

	// Simulate an open connection that never finishes
	proxy.wg.Add(1)
	defer proxy.wg.Done()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = proxy.DrainWithContext(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestDesktopProxy_DrainWithContext_DeadlineExceeded(t *testing.T) {
	proxy, err := NewDesktopProxy(nil, "/prefix", nil)
	require.NoError(t, err)

	// Simulate an open connection that never finishes
	proxy.wg.Add(1)
	defer proxy.wg.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err = proxy.DrainWithContext(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestDesktopProxy_NotifyShutdown_ClosesConnections(t *testing.T) {
	backend, connected := newWSBackend(t)

	// Use InClusterProxy as backend transport to avoid needing a real k8s ConnectionManager.
	// The DesktopProxy delegates to a k8s proxy handler, so we build a custom DesktopProxy
	// that routes directly to our test backend instead.
	proxy, err := NewDesktopProxy(nil, "/prefix", nil)
	require.NoError(t, err)

	// Replace the handler: wrap in a server that upgrades through the backend
	// We test the shutdown plumbing by going through InClusterProxy's hijack path
	// since DesktopProxy uses the same hijackTrackingResponseWriter mechanism.
	inProxy, err := newInClusterProxy(backend.URL, "/prefix", nil, http.DefaultTransport)
	require.NoError(t, err)

	// Wire the DesktopProxy's shutdownCh into the InClusterProxy
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Track on the DesktopProxy's WaitGroup
		proxy.wg.Add(1)
		defer proxy.wg.Done()

		if r.Header.Get("Upgrade") != "" {
			hw := &hijackTrackingResponseWriter{ResponseWriter: w}
			doneCh := make(chan struct{})
			defer close(doneCh)
			go func() {
				select {
				case <-doneCh:
				case <-proxy.shutdownCh:
					hw.closeConn()
				}
			}()
			inProxy.ReverseProxy.ServeHTTP(hw, r)
			return
		}
		inProxy.ReverseProxy.ServeHTTP(w, r)
	}))
	defer proxyServer.Close()

	// Dial WebSocket through the proxy
	wsURL := "ws" + proxyServer.URL[4:] + "/prefix/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	waitConnected(t, connected)

	// Signal shutdown
	proxy.NotifyShutdown()

	// The connection should be closed
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, readErr := conn.ReadMessage()
	require.Error(t, readErr)

	// All connections should be drained
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, proxy.DrainWithContext(ctx))
}

// --- TLS transport tests ---

func TestInClusterProxy_TLSBackend_Returns502WithDefaultTransport(t *testing.T) {
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxy, err := newInClusterProxy(backend.URL, "/prefix", nil, http.DefaultTransport)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/prefix/graphql", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestInClusterProxy_TLSBackend_SucceedsWithMatchingTransport(t *testing.T) {
	var called bool
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxy, err := newInClusterProxy(backend.URL, "/prefix", nil, backend.Client().Transport)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/prefix/graphql", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, called)
}

// --- URL isolation test (run with -race to detect data race) ---

func TestInClusterProxy_Director_IsolatesURLPerRequest(t *testing.T) {
	var mu sync.Mutex
	received := make(map[string]int)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received[r.URL.Path]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxy, err := newInClusterProxy(backend.URL, "/prefix", nil, http.DefaultTransport)
	require.NoError(t, err)

	proxyServer := httptest.NewServer(proxy)
	defer proxyServer.Close()

	const n = 20
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			_, _ = http.Get(fmt.Sprintf("%s/prefix/path%d", proxyServer.URL, i)) //nolint:noctx
		})
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	for i := range n {
		p := fmt.Sprintf("/apis/api.kubetail.com/v1/path%d", i)
		assert.Equal(t, 1, received[p], "path %s should reach backend exactly once", p)
	}
}
