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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

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

	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	assert.NotEqual(t, http.StatusForbidden, rec.Code)
	assert.True(t, captured, "allowlisted upgrade must reach the backend")
}

func TestInClusterProxy_XForwardedAuthorization(t *testing.T) {
	tests := []struct {
		name       string
		userToken  string
		wantHeader string
	}{
		{
			name:       "forwards user token as X-Forwarded-Authorization",
			userToken:  "user-token-123",
			wantHeader: "Bearer user-token-123",
		},
		{
			name:       "no X-Forwarded-Authorization without user token",
			userToken:  "",
			wantHeader: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedHeader string
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedHeader = r.Header.Get("X-Forwarded-Authorization")
				w.WriteHeader(http.StatusOK)
			}))
			defer backend.Close()

			proxy, err := newInClusterProxy(backend.URL, "/prefix", nil, http.DefaultTransport)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodGet, "/prefix/somepath", nil)
			if tt.userToken != "" {
				ctx := context.WithValue(req.Context(), k8shelpers.K8STokenCtxKey, tt.userToken)
				req = req.WithContext(ctx)
			}

			proxy.ServeHTTP(httptest.NewRecorder(), req)

			assert.Equal(t, tt.wantHeader, capturedHeader)
		})
	}
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
// a WebSocket dial passes the proxy's same-origin gate.
func wsOriginHeader(serverURL string) http.Header {
	return http.Header{"Origin": []string{serverURL}}
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

	clientset := fake.NewSimpleClientset()
	clientset.Fake.PrependReactor("create", "serviceaccounts", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &authv1.TokenRequest{
			Status: authv1.TokenRequestStatus{
				Token:               "fake-sat",
				ExpirationTimestamp: metav1.NewTime(time.Now().Add(time.Hour)),
			},
		}, nil
	})

	shutdownCh := make(chan struct{})
	defer close(shutdownCh)
	sat, err := k8shelpers.NewServiceAccountToken(context.Background(), clientset, "ns", "sa", shutdownCh)
	require.NoError(t, err)

	proxy, err := NewDesktopProxy(nil, "/prefix", nil)
	require.NoError(t, err)
	proxy.phCache["ctx"] = handler
	proxy.satCache["ctx/ns"] = sat

	req := httptest.NewRequest(http.MethodGet, "/prefix/ctx/ns/svc/relpath", nil)
	// httptest.NewRequest uses example.com as the request Host
	req.Header.Set("Origin", "https://example.com")

	proxy.ServeHTTP(httptest.NewRecorder(), req)

	require.True(t, captured, "backend handler was not called")
	assert.Empty(t, capturedOrigin, "Origin header must be stripped before forwarding")
}

func TestDesktopProxy_OverwritesClientSuppliedXForwardedAuthorization(t *testing.T) {
	var capturedValues []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedValues = r.Header.Values("X-Forwarded-Authorization")
		w.WriteHeader(http.StatusOK)
	})

	clientset := fake.NewSimpleClientset()
	clientset.Fake.PrependReactor("create", "serviceaccounts", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &authv1.TokenRequest{
			Status: authv1.TokenRequestStatus{
				Token:               "fake-sat",
				ExpirationTimestamp: metav1.NewTime(time.Now().Add(time.Hour)),
			},
		}, nil
	})

	shutdownCh := make(chan struct{})
	defer close(shutdownCh)
	sat, err := k8shelpers.NewServiceAccountToken(context.Background(), clientset, "ns", "sa", shutdownCh)
	require.NoError(t, err)

	proxy, err := NewDesktopProxy(nil, "/prefix", nil)
	require.NoError(t, err)
	proxy.phCache["ctx"] = handler
	proxy.satCache["ctx/ns"] = sat

	req := httptest.NewRequest(http.MethodGet, "/prefix/ctx/ns/svc/relpath", nil)
	req.Header.Set("Origin", "https://example.com")
	// Client-supplied attempt to inject an upstream token. The proxy must
	// clobber this value with its own service-account-token bearer header
	// rather than appending alongside it.
	req.Header.Set("X-Forwarded-Authorization", "Bearer attacker-token")

	proxy.ServeHTTP(httptest.NewRecorder(), req)

	require.Len(t, capturedValues, 1, "exactly one X-Forwarded-Authorization value must be forwarded")
	assert.Equal(t, "Bearer fake-sat", capturedValues[0])
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

			req := httptest.NewRequest(http.MethodGet, "/prefix/ctx/ns/svc/relpath", nil)
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

	clientset := fake.NewSimpleClientset()
	clientset.Fake.PrependReactor("create", "serviceaccounts", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &authv1.TokenRequest{
			Status: authv1.TokenRequestStatus{
				Token:               "fake-sat",
				ExpirationTimestamp: metav1.NewTime(time.Now().Add(time.Hour)),
			},
		}, nil
	})

	shutdownCh := make(chan struct{})
	defer close(shutdownCh)
	sat, err := k8shelpers.NewServiceAccountToken(context.Background(), clientset, "ns", "sa", shutdownCh)
	require.NoError(t, err)

	proxy, err := NewDesktopProxy(nil, "/prefix", []string{"https://allowed.example.com"})
	require.NoError(t, err)
	proxy.phCache["ctx"] = handler
	proxy.satCache["ctx/ns"] = sat

	req := httptest.NewRequest(http.MethodGet, "/prefix/ctx/ns/svc/relpath", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Origin", "https://allowed.example.com")

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
