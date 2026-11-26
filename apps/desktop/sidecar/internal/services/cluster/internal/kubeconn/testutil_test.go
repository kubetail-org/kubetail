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

package kubeconn

import (
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/supervisor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// deadHost answers nothing, and does so at once: a literal address, so nothing here resolves a
// name. **No test may dial a name that leaves the machine** — a DNS lookup is slow, and on a
// network with a wildcard resolver it answers, which turns "unreachable" into whatever that
// host is.
const deadHost = "http://127.0.0.1:1"

// apiVersions is what a Kubernetes API server answers at /api.
const apiVersions = `{"kind":"APIVersions","versions":["v1"]}`

// serveAPI is a server that answers /api like a Kubernetes API server and 404s everything else.
func serveAPI(t *testing.T) *httptest.Server {
	t.Helper()
	return serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(apiVersions))
	}))
}

// The bodies a healthy cluster answers the four probes behind reachability with.
const (
	readyzOK       = "ok"
	kubeSystemJSON = `{"metadata":{"name":"kube-system","uid":"uid-1"}}`
	versionJSON    = `{"major":"1","minor":"31","gitVersion":"v1.31.4"}`
	reviewJSON     = `{"status":{"userInfo":{"username":"admin@example","groups":["system:masters","system:authenticated"]}}}`
)

// cluster is a server answering every endpoint the five probes read, each route replaceable so a
// test can make one of them misbehave while the rest stay healthy.
type cluster struct {
	*httptest.Server
	mu     sync.Mutex
	routes map[string]http.HandlerFunc
	// requests records the paths served, for a test asserting what was asked rather than what
	// came back.
	requests *testutil.Probe[string]
}

// serveCluster is a healthy cluster: every probe's endpoint answers.
func serveCluster(t *testing.T) *cluster {
	t.Helper()
	c := &cluster{routes: map[string]http.HandlerFunc{}, requests: testutil.NewProbe[string](16)}
	for path, body := range map[string]string{
		"/api":                apiVersions,
		readyzPath:            readyzOK,
		kubeSystemPath:        kubeSystemJSON,
		versionPath:           versionJSON,
		selfSubjectReviewPath: reviewJSON,
	} {
		c.answer(path, body)
	}
	c.Server = serve(t, c)
	return c
}

// answer replaces a route with a 200 carrying body.
func (c *cluster) answer(path, body string) {
	c.route(path, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
}

// fail replaces a route with a status, and the body that status carries.
func (c *cluster) fail(path string, code int, body string) {
	c.route(path, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	})
}

func (c *cluster) route(path string, h http.HandlerFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.routes[path] = h
}

func (c *cluster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	h, ok := c.routes[r.URL.Path]
	c.mu.Unlock()

	c.requests.Fire(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	h(w, r)
}

// runProbe runs one probe body against conn the way the supervisor would, and hands back what it
// recorded. prev is what the probe last committed, or nil for one that has committed nothing.
func runProbe[T any](t *testing.T, p supervisor.Job[T], conn *Connection, prev *T) (supervisor.Result, T, bool) {
	t.Helper()
	snap := supervisor.NewSnapshot(map[string]any{nameConnection: connInfo{conn: conn}})
	pass := supervisor.NewJobPass("prod", prev, snap)
	res := p.Run(t.Context(), pass)
	v, committed := pass.Updated()
	return res, v, committed
}

// serve runs h over TLS for the life of the test.
func serve(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// restConfigFor is a config that trusts srv's certificate and nothing else.
func restConfigFor(srv *httptest.Server) *rest.Config {
	return &rest.Config{Host: srv.URL, TLSClientConfig: rest.TLSClientConfig{CAData: caPEM(srv)}}
}

// restConfigForHost is a config aimed at a bare address, trusting nothing.
func restConfigForHost(host string) *rest.Config {
	return &rest.Config{Host: host}
}

// caPEM is srv's self-signed certificate, which is its own CA.
func caPEM(srv *httptest.Server) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
}

// connTo builds a connection to srv, trusting its certificate.
func connTo(t *testing.T, srv *httptest.Server) *Connection {
	t.Helper()
	conn, err := NewConnection(restConfigFor(srv))
	require.NoError(t, err)
	return conn
}
