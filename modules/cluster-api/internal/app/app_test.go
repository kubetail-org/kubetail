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

package app

import (
	"compress/gzip"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	k8shelpersmock "github.com/kubetail-org/kubetail/modules/shared/k8shelpers/mock"
)

func TestRequestID(t *testing.T) {
	app := NewTestApp(nil)

	// add route for testing
	app.GET("/x", func(c *gin.Context) {
		c.String(http.StatusOK, requestid.Get(c))
	})

	// request 1
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("GET", "/x", nil)
	app.ServeHTTP(w1, r1)
	id1 := w1.Body.String()

	// request 2
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/x", nil)
	app.ServeHTTP(w2, r2)
	id2 := w2.Body.String()

	// check result
	assert.NotEqual(t, "", id1)
	assert.NotEqual(t, "", id2)
	assert.NotEqual(t, id1, id2)
}

func TestGzip(t *testing.T) {
	app := NewTestApp(nil)

	// add route for testing
	app.GET("/x", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// request without compression
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("GET", "/x", nil)
	app.ServeHTTP(w1, r1)
	assert.Equal(t, w1.Body.String(), "ok")

	// request with compression
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/x", nil)
	r2.Header["Accept-Encoding"] = []string{"gzip"}
	app.ServeHTTP(w2, r2)

	gzreader, err := gzip.NewReader(w2.Body)
	assert.Equal(t, nil, err)
	uncompressed, err := io.ReadAll(gzreader)
	assert.Equal(t, nil, err)
	assert.Equal(t, "ok", string(uncompressed))
}

// newFrontProxyRequest builds a request as the kube-apiserver's front proxy
// sends it: the proxy's client certificate and the caller's username header.
func newFrontProxyRequest(t *testing.T, proxyCA *testCA, method, target string, body io.Reader) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, target, body)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{proxyCA.issue(t, "front-proxy-client")}}
	r.Header.Set("X-Remote-User", "alice")
	return r
}

// Introspection makes no Kubernetes or agent call, so it needs nothing past
// the front proxy's authentication.
func TestGraphQLAllowsUnauthenticatedIntrospection(t *testing.T) {
	proxyCA := newTestCA(t, "proxy-ca")
	app := newTestAppWithAuth(nil, newTestAuthCfg(proxyCA))

	body := `{"query":"{ __typename }"}`
	w := httptest.NewRecorder()
	r := newFrontProxyRequest(t, proxyCA, "POST", "/apis/api.kubetail.com/v1/graphql", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	app.ServeHTTP(w, r)

	assert.Equal(t, http.StatusOK, w.Result().StatusCode)
	assert.Contains(t, w.Body.String(), `"__typename":"Query"`)
}

func TestGraphQLRejectsRequestNotFromFrontProxy(t *testing.T) {
	proxyCA := newTestCA(t, "proxy-ca")
	app := newTestAppWithAuth(nil, newTestAuthCfg(proxyCA))

	body := `{"query":"{ __typename }"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/apis/api.kubetail.com/v1/graphql", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Remote-User", "alice")
	app.ServeHTTP(w, r)

	assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
}

func TestClose(t *testing.T) {
	t.Run("without a connection manager", func(t *testing.T) {
		app := NewTestApp(nil)
		assert.NotPanics(t, app.Close)
	})

	t.Run("with a connection manager", func(t *testing.T) {
		cm := &k8shelpersmock.MockConnectionManager{}
		cm.On("Close").Return()

		app := NewTestApp(nil)
		app.cm = cm
		app.Close()

		cm.AssertCalled(t, "Close")
	})
}

func TestHealthz(t *testing.T) {
	app := NewTestApp(nil)

	// make request
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/healthz", nil)
	app.ServeHTTP(w, r)

	// check response
	result := w.Result()
	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, "{\"status\":\"ok\"}", w.Body.String())
}

// The dashboard cluster-api proxy rewrites `/cluster-api-proxy/healthz` to
// `/apis/api.kubetail.com/v1/healthz`, so the health endpoint must also be
// reachable under the aggregated APIService path.
func TestHealthzAggregated(t *testing.T) {
	proxyCA := newTestCA(t, "proxy-ca")
	app := newTestAppWithAuth(nil, newTestAuthCfg(proxyCA))

	w := httptest.NewRecorder()
	r := newFrontProxyRequest(t, proxyCA, "GET", "/apis/api.kubetail.com/v1/healthz", nil)
	app.ServeHTTP(w, r)

	result := w.Result()
	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, "{\"status\":\"ok\"}", w.Body.String())
}
