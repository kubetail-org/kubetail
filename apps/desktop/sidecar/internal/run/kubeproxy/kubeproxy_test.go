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

package kubeproxy

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
)

// upstreamFunc is an Upstream answering from a function.
type upstreamFunc func(ctx context.Context) (Endpoint, error)

func (f upstreamFunc) Endpoint(ctx context.Context) (Endpoint, error) { return f(ctx) }

// apiServer is an httptest API server that records each request it is sent
// and answers with handle.
type apiServer struct {
	*httptest.Server
	mu   sync.Mutex
	seen []*http.Request
}

func newAPIServer(t *testing.T, handle http.HandlerFunc) *apiServer {
	t.Helper()
	s := &apiServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.seen = append(s.seen, r.Clone(context.Background()))
		s.mu.Unlock()
		handle(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

// requests is every request the server has been sent.
func (s *apiServer) requests() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.seen...)
}

// upstream is s as a connection: its URL, and a client that sends the
// connection's own credential.
func (s *apiServer) upstream() Upstream { return s.upstreamUntil(nil) }

// upstreamUntil is upstream, its connection retired when done closes.
func (s *apiServer) upstreamUntil(done <-chan struct{}) Upstream {
	base, _ := url.Parse(s.URL)
	client := &http.Client{Transport: bearer{token: "users-own", base: s.Client().Transport}}
	return upstreamFunc(func(context.Context) (Endpoint, error) { return Endpoint{Base: base, Client: client, Done: done}, nil })
}

// bearer adds a credential only when a request carries none, as client-go's
// bearer round-tripper does.
type bearer struct {
	token string
	base  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Header.Get("Authorization") == "" {
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return b.base.RoundTrip(r)
}

// noAsker is the refusal of a test grant with no one to ask.
const noAsker = "kubetail: nobody to ask"

// served is a grant served as a run's forwarder reaches it, and a client that
// sends through it as client-go does through proxy-url: in absolute form.
type served struct {
	g      *Grant
	client *http.Client
}

func serve(t *testing.T, up Upstream) *served {
	t.Helper()
	return serveGrant(t, NewGrant(up, askSession, "c1", "dev", nil, noAsker, 1000, 1000, 32))
}

func serveGrant(t *testing.T, g *Grant) *served {
	t.Helper()
	srv := httptest.NewServer(g)
	t.Cleanup(srv.Close)
	t.Cleanup(g.End)
	proxy, _ := url.Parse(srv.URL)
	return &served{g: g, client: &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}}}
}

// send sends method on path to the cluster's name with token, and answers the
// response, its body read.
func (s *served) send(t *testing.T, method, path, token string) (*http.Response, string) {
	t.Helper()
	return s.do(t, s.request(t, method, path, token))
}

// request is method on path to the cluster's name with token as the proxy
// credentials, as client-go sends them from proxy-url's userinfo.
func (s *served) request(t *testing.T, method, path, token string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, "http://"+Host+path, nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Proxy-Authorization", proxyAuth(ProxyUser, token))
	}
	return req
}

// proxyAuth is the Proxy-Authorization Go's transport sends for a proxy URL
// holding user and password.
func proxyAuth(user, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

// do sends req and answers the response, its body read.
func (s *served) do(t *testing.T, req *http.Request) (*http.Response, string) {
	t.Helper()
	resp, err := s.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, string(body)
}

// statusOf reads a Status body.
func statusOf(t *testing.T, body string) metav1.Status {
	t.Helper()
	var st metav1.Status
	require.NoError(t, json.Unmarshal([]byte(body), &st), body)
	return st
}

// A request without the grant's token, with another, or with the token once
// the grant is dead, is unauthorized, and the cluster never sees it.
func TestAWrongOrDeadTokenIsUnauthorized(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	s := serve(t, api.upstream())

	for name, token := range map[string]string{"none": "", "another": "x" + s.g.Token()[1:], "short": "x"} {
		resp, body := s.send(t, "GET", "/api", token)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, name)
		assert.Equal(t, metav1.StatusReasonUnauthorized, statusOf(t, body).Reason, name)
	}
	for name, header := range map[string][2]string{
		"another user":   {"Proxy-Authorization", proxyAuth("admin", s.g.Token())},
		"as a bearer":    {"Authorization", "Bearer " + s.g.Token()},
		"as basic auth":  {"Authorization", proxyAuth(ProxyUser, s.g.Token())},
		"a bearer proxy": {"Proxy-Authorization", "Bearer " + s.g.Token()},
	} {
		req := s.request(t, "GET", "/api", "")
		req.Header.Set(header[0], header[1])
		resp, _ := s.do(t, req)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, name)
	}
	s.g.End()
	resp, _ := s.send(t, "GET", "/api", s.g.Token())
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "dead")
	assert.Empty(t, api.requests())
}

// A grant's token is 256 random bits, a new one each grant.
func TestEachGrantHasItsOwnToken(t *testing.T) {
	a, b := NewGrant(nil, askSession, "c1", "dev", nil, noAsker, 1, 1, 1), NewGrant(nil, askSession, "c1", "dev", nil, noAsker, 1, 1, 1)
	assert.Len(t, a.Token(), 64)
	assert.NotEqual(t, a.Token(), b.Token())
}

// A read reaches the cluster with the connection's credentials and its own
// host, under the connection's base path, and never with the run's token or
// the kubeconfig's server name.
func TestAGetPassesWithTheConnectionsCredentials(t *testing.T) {
	api := newAPIServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "pods") })
	s := serve(t, api.upstream())

	resp, body := s.send(t, "GET", "/api/v1/namespaces/web/pods?limit=1", s.g.Token())

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "pods", body)
	seen := api.requests()
	require.Len(t, seen, 1)
	assert.Equal(t, "Bearer users-own", seen[0].Header.Get("Authorization"))
	assert.Equal(t, api.Listener.Addr().String(), seen[0].Host)
	assert.Equal(t, "/api/v1/namespaces/web/pods", seen[0].URL.Path)
	assert.Equal(t, "limit=1", seen[0].URL.RawQuery)
	for _, vs := range seen[0].Header {
		for _, v := range vs {
			assert.NotContains(t, v, s.g.Token())
			assert.NotContains(t, v, Host)
		}
	}
}

// A connection whose server sits under a path, as Rancher's does, keeps it.
func TestTheConnectionsBasePathIsKept(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	base, _ := url.Parse(api.URL + "/k8s/clusters/c-1")
	s := serve(t, upstreamFunc(func(context.Context) (Endpoint, error) { return Endpoint{Base: base, Client: api.Client()}, nil }))

	s.send(t, "GET", "/api/v1/pods", s.g.Token())

	require.Len(t, api.requests(), 1)
	assert.Equal(t, "/k8s/clusters/c-1/api/v1/pods", api.requests()[0].URL.Path)
}

// The audit log reads Kubetail ahead of the command, though the connection's
// client-go sets its own agent only on a request that names none.
func TestKubetailIsNamedAheadOfTheCommandsAgent(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	base, _ := url.Parse(api.URL)
	client, err := rest.HTTPClientFor(&rest.Config{Host: api.URL, UserAgent: version.UserAgent()})
	require.NoError(t, err)
	s := serve(t, upstreamFunc(func(context.Context) (Endpoint, error) { return Endpoint{Base: base, Client: client}, nil }))

	req := s.request(t, "GET", "/api", s.g.Token())
	req.Header.Set("User-Agent", "kubectl/v1.34.0 (linux/arm64) kubernetes/abcdef0")
	s.do(t, req)

	seen := api.requests()
	require.Len(t, seen, 1)
	assert.Equal(t, version.UserAgent()+" kubectl/v1.34.0 (linux/arm64) kubernetes/abcdef0", seen[0].Header.Get("User-Agent"))
}

// Neither the run's proxy credentials nor any credential of the client's own
// reach the server: a transport that adds the user's credential only when a
// request has none would otherwise send the client's and never the user's.
func TestTheClientsAuthorizationNeverReachesTheServer(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	base, _ := url.Parse(api.URL)
	s := serve(t, upstreamFunc(func(context.Context) (Endpoint, error) { return Endpoint{Base: base, Client: api.Client()}, nil }))

	req := s.request(t, "GET", "/api", s.g.Token())
	req.Header.Set("Authorization", "Bearer the-clients-own")
	s.do(t, req)

	require.Len(t, api.requests(), 1)
	assert.Empty(t, api.requests()[0].Header.Values("Authorization"))
	assert.Empty(t, api.requests()[0].Header.Values("Proxy-Authorization"))
}

// client-go, reading a kubeconfig shaped as a run's is, reaches the cluster:
// the token rides in proxy-url's userinfo, since clientcmd applies a user's
// credentials only to a server reached over TLS.
func TestAnAbsoluteFormRequestPasses(t *testing.T) {
	api := newAPIServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"major":"1","minor":"34","gitVersion":"v1.34.0"}`)
	})
	g := NewGrant(api.upstream(), askSession, "c1", "dev", nil, noAsker, 1000, 1000, 32)
	t.Cleanup(g.End)
	srv := httptest.NewServer(g)
	t.Cleanup(srv.Close)
	proxy, _ := url.Parse(srv.URL)
	proxy.User = url.UserPassword(ProxyUser, g.Token())
	kubeconfig := clientcmdapi.NewConfig()
	kubeconfig.Clusters["c"] = &clientcmdapi.Cluster{Server: "http://" + Host, ProxyURL: proxy.String()}
	kubeconfig.AuthInfos["u"] = &clientcmdapi.AuthInfo{}
	kubeconfig.Contexts["c"] = &clientcmdapi.Context{Cluster: "c", AuthInfo: "u"}
	kubeconfig.CurrentContext = "c"
	cfg, err := clientcmd.NewDefaultClientConfig(*kubeconfig, nil).ClientConfig()
	require.NoError(t, err)

	client, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)
	v, err := client.Discovery().ServerVersion()

	require.NoError(t, err)
	assert.Equal(t, "v1.34.0", v.GitVersion)
}

// assertForbidden checks resp is the policy's Forbidden Status saying why.
func assertForbidden(t *testing.T, resp *http.Response, body string, why refusal) {
	t.Helper()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	st := statusOf(t, body)
	assert.Equal(t, metav1.StatusReasonForbidden, st.Reason)
	assert.Equal(t, string(why), st.Message)
}

// Each refused row of the policy is a Forbidden Status with its own message,
// and the cluster sees none of them.
func TestEachRefusalIsAForbiddenStatus(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	s := serve(t, api.upstream())
	for name, c := range map[string]struct {
		method, path string
		header       http.Header
		why          refusal
	}{
		"not canonical": {method: "GET", path: "/api/v1/configmaps/../secrets", why: refusedPath},
		"exec":          {method: "POST", path: "/api/v1/namespaces/web/pods/x/exec", why: refusedReach},
		"proxy":         {method: "GET", path: "/api/v1/proxy/nodes/n1", why: refusedReach},
		"upgrade":       {method: "GET", path: "/api/v1/pods", header: http.Header{"Upgrade": {"websocket"}, "Connection": {"Upgrade"}}, why: refusedReach},
		"impersonate":   {method: "GET", path: "/api/v1/pods", header: http.Header{"Impersonate-User": {"admin"}}, why: refusedReach},
		"token":         {method: "POST", path: "/api/v1/namespaces/web/serviceaccounts/x/token", why: refusedToken},
		"method":        {method: "OPTIONS", path: "/api/v1/pods", why: refusedMethod},
		"path":          {method: "GET", path: "/metrics", why: refusedPath},
	} {
		req := s.request(t, c.method, c.path, s.g.Token())
		maps.Copy(req.Header, c.header)
		resp, body := s.do(t, req)
		t.Run(name, func(t *testing.T) { assertForbidden(t, resp, body, c.why) })
	}
	assert.Empty(t, api.requests())
}

// kubectl auth can-i, can-i --list and whoami reach the cluster unasked; a
// review the API server would not take as a create of one is a write.
func TestTheSelfReviewsPass(t *testing.T) {
	api := newAPIServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) })
	s := serve(t, api.upstream())

	for _, path := range []string{
		"/apis/authorization.k8s.io/v1/selfsubjectaccessreviews",
		"/apis/authorization.k8s.io/v1/selfsubjectrulesreviews",
		"/apis/authentication.k8s.io/v1/selfsubjectreviews",
	} {
		resp, _ := s.send(t, "POST", path, s.g.Token())
		assert.Equal(t, http.StatusCreated, resp.StatusCode, path)
	}
	assert.Len(t, api.requests(), 3)

	for _, path := range []string{
		"/apis/authorization.k8s.io/v1/namespaces/web/selfsubjectaccessreviews",
		"/apis/authorization.k8s.io/v1/selfsubjectaccessreviews/x",
	} {
		resp, body := s.send(t, "POST", path, s.g.Token())
		assertForbidden(t, resp, body, noAsker)
	}
	assert.Len(t, api.requests(), 3)
}

// A path that reads differently decoded, raw or resolved is refused, and the
// cluster sees none.
func TestAPathThatIsNotCanonicalIsRefused(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	s := serve(t, api.upstream())

	for _, path := range []string{
		"/api/v1/namespaces/web/configmaps/../secrets/x",
		"/api/v1/./pods",
		"/apis//v1/pods",
		"/api/v1/pods/",
		"/api/v1/namespaces/web/configmaps/a%2Fb",
	} {
		resp, body := s.send(t, "GET", path, s.g.Token())
		assertForbidden(t, resp, body, refusedPath)
	}
	assert.Empty(t, api.requests())
}

// Each error an upstream answers is a 503 with its own message, never the
// error's text; any other is the cluster being unreachable.
func TestEachUpstreamSentinelIsServiceUnavailable(t *testing.T) {
	for err, want := range map[error]string{
		ErrIdentityMismatch:  "kubetail: identity-mismatch: the kube-context no longer vouches for this chat's cluster",
		ErrNotIdentified:     "kubetail: Kubetail has not identified this cluster, since it cannot read the kube-system namespace, so the sandbox cannot reach it",
		ErrNotConnectable:    "kubetail: Kubetail does not connect to this cluster: it is disabled, being deleted, or has no kubeconfig credentials",
		errors.New("secret"): "kubetail: the cluster is not reachable",
	} {
		wrapped := fmt.Errorf("%w: detail", err)
		s := serve(t, upstreamFunc(func(context.Context) (Endpoint, error) { return Endpoint{}, wrapped }))

		resp, body := s.send(t, "GET", "/api", s.g.Token())

		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode, err)
		st := statusOf(t, body)
		assert.Equal(t, metav1.StatusReasonServiceUnavailable, st.Reason)
		assert.Equal(t, want, st.Message)
	}
}

// A cluster that does not answer is a 502 that names no error.
func TestAClusterThatDoesNotAnswerIsBadGateway(t *testing.T) {
	// The server hangs up rather than closing: a closed server's port is free
	// for the proxy's own listener to take, and the proxy would answer itself.
	api := newAPIServer(t, func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := http.NewResponseController(w).Hijack()
		require.NoError(t, err)
		conn.Close()
	})
	s := serve(t, api.upstream())

	resp, body := s.send(t, "GET", "/api", s.g.Token())

	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Equal(t, "kubetail: the cluster did not answer", statusOf(t, body).Message)
}

// watchServer is an API server that answers a watch by sending each line it is
// handed on next, flushed at once, until the request ends, and ended gets a
// value when it does. Any other request it answers empty.
func watchServer(t *testing.T) (api *apiServer, next chan string, ended chan struct{}) {
	t.Helper()
	next, ended = make(chan string), make(chan struct{}, 8)
	api = newAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("watch") != "true" {
			return
		}
		defer func() { ended <- struct{}{} }()
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		for {
			select {
			case line := <-next:
				_, _ = io.WriteString(w, line+"\n")
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	return api, next, ended
}

// openWatch opens a watch through s and answers its lines as they arrive,
// closed when the stream ends, and a func that closes it from the client.
func (s *served) openWatch(t *testing.T) (<-chan string, func()) {
	t.Helper()
	resp, err := s.client.Do(s.request(t, "GET", "/api/v1/pods?watch=true", s.g.Token()))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	lines := make(chan string)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	return lines, func() { _ = resp.Body.Close() }
}

// Each event of a watch reaches the client before the next is sent.
func TestAWatchStreams(t *testing.T) {
	api, next, _ := watchServer(t)
	s := serve(t, api.upstream())
	lines, _ := s.openWatch(t)

	for _, event := range []string{"ADDED a", "MODIFIED a", "DELETED a"} {
		next <- event
		assert.Equal(t, event, testutil.Recv(t, lines, "the event"))
	}
}

// A watch open when the grant ends is closed, at the cluster and at the client.
func TestEndCancelsWhatIsInFlight(t *testing.T) {
	api, next, ended := watchServer(t)
	s := serve(t, api.upstream())
	lines, _ := s.openWatch(t)
	next <- "ADDED a"
	testutil.Recv(t, lines, "the first event")

	s.g.End()

	testutil.Wait(t, ended, "the cluster's side to end")
	testutil.WaitClosed(t, lines, "the client's side to end")
}

// A watch open when its connection is retired is closed, at the cluster and at
// the client.
func TestARetiredConnectionCancelsItsWatch(t *testing.T) {
	api, next, ended := watchServer(t)
	done := make(chan struct{})
	s := serve(t, api.upstreamUntil(done))
	lines, _ := s.openWatch(t)
	next <- "ADDED a"
	testutil.Recv(t, lines, "the first event")

	close(done)

	testutil.Wait(t, ended, "the cluster's side to end")
	testutil.WaitClosed(t, lines, "the client's side to end")
}

// Past the burst, a request waits for the limiter rather than fails, as
// client-go's own limiter has it. The wait is a lower bound, so no load on the
// machine can make it pass early.
func TestABurstWaits(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	const qps, burst = 10, 2
	s := serveGrant(t, NewGrant(api.upstream(), askSession, "c1", "dev", nil, noAsker, qps, burst, 32))

	start := time.Now()
	for range burst + 2 {
		resp, _ := s.send(t, "GET", "/api", s.g.Token())
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

	assert.GreaterOrEqual(t, time.Since(start), 2*time.Second/qps*9/10, "two past the burst waited a tenth of a second each")
	assert.Len(t, api.requests(), burst+2)
}

// With the cap's watches open, the next request answers 429 with no
// Retry-After, so client-go does not retry it; a watch closing frees its slot.
func TestPastTheCapIsTooManyRequests(t *testing.T) {
	api, _, ended := watchServer(t)
	s := serveGrant(t, NewGrant(api.upstream(), askSession, "c1", "dev", nil, noAsker, 1000, 1000, 2))
	_, closeFirst := s.openWatch(t)
	s.openWatch(t)

	resp, body := s.send(t, "GET", "/api", s.g.Token())

	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Empty(t, resp.Header.Values("Retry-After"))
	st := statusOf(t, body)
	assert.Equal(t, metav1.StatusReasonTooManyRequests, st.Reason)
	assert.Equal(t, "kubetail: too many requests are open at once. Close a watch or a follow, then try again.", st.Message)

	closeFirst()
	testutil.Recv(t, ended, "the first watch to end at the cluster")
	require.Eventually(t, func() bool {
		resp, _ := s.send(t, "GET", "/api", s.g.Token())
		return resp.StatusCode == http.StatusOK
	}, testutil.Timeout, time.Millisecond, "a slot to free")
}

// A body past 64 KiB answers 413 and the cluster never sees it; one within it
// arrives whole.
func TestAnOversizedBodyIsRefused(t *testing.T) {
	var got []byte
	api := newAPIServer(t, func(_ http.ResponseWriter, r *http.Request) { got, _ = io.ReadAll(r.Body) })
	s := serve(t, api.upstream())
	post := func(size int) (*http.Response, string) {
		req, err := http.NewRequest("POST", "http://"+Host+"/apis/authorization.k8s.io/v1/selfsubjectaccessreviews",
			strings.NewReader(strings.Repeat("x", size)))
		require.NoError(t, err)
		req.Header.Set("Proxy-Authorization", proxyAuth(ProxyUser, s.g.Token()))
		return s.do(t, req)
	}

	resp, body := post(64<<10 + 1)
	assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
	assert.Equal(t, "kubetail: the request body is too large", statusOf(t, body).Message)
	assert.Empty(t, api.requests())

	resp, _ = post(64 << 10)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Len(t, got, 64<<10)
}

// A request waiting on the limiter ends when its client gives up, and the
// cluster never sees it. The rate is one an hour, so the wait outlasts any
// client timeout.
func TestAWaitOnTheLimiterEndsWithItsClient(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	s := serveGrant(t, NewGrant(api.upstream(), askSession, "c1", "dev", nil, noAsker, rate.Every(time.Hour), 1, 32))
	s.send(t, "GET", "/api", s.g.Token())
	s.client.Timeout = 100 * time.Millisecond

	_, err := s.client.Do(s.request(t, "GET", "/api", s.g.Token()))

	assert.Error(t, err)
	assert.Len(t, api.requests(), 1)
}

// A request whose Host is not the kubeconfig's server name is unauthorized,
// whether it came in absolute form or not: a browser page can send neither.
func TestAnotherHostIsUnauthorized(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	s := serve(t, api.upstream())

	req, err := http.NewRequest("GET", "http://localhost/api", nil)
	require.NoError(t, err)
	req.Header.Set("Proxy-Authorization", proxyAuth(ProxyUser, s.g.Token()))
	resp, err := s.client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	w := httptest.NewRecorder()
	direct := httptest.NewRequest("GET", "/api", nil)
	direct.Header.Set("Proxy-Authorization", proxyAuth(ProxyUser, s.g.Token()))
	s.g.ServeHTTP(w, direct)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Empty(t, api.requests())
}
