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

package githubproxy

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
)

// seen is one request an upstream received.
type seen struct {
	method, uri, host string
	header            http.Header
	body              string
}

// upstream is a fake GitHub origin: it records each request and answers it
// with answer, by default 200 and "ok". arrived gets each request's path once
// its headers are in, before its body is read.
type upstream struct {
	srv     *httptest.Server
	seen    chan seen
	arrived chan string
	mu      sync.Mutex
	answer  http.HandlerFunc
}

func newUpstream(t *testing.T) *upstream {
	u := &upstream{seen: make(chan seen, 16), arrived: make(chan string, 16)}
	u.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.arrived <- r.URL.Path
		body, _ := io.ReadAll(r.Body)
		u.seen <- seen{method: r.Method, uri: r.RequestURI, host: r.Host, header: r.Header.Clone(), body: string(body)}
		u.mu.Lock()
		answer := u.answer
		u.mu.Unlock()
		if answer != nil {
			answer(w, r)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func (u *upstream) answers(h http.HandlerFunc) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.answer = h
}

// fakeTokens answers token, or err, and hands out next once a token is
// refused.
type fakeTokens struct {
	mu      sync.Mutex
	token   string
	next    string
	err     error
	refused []string
}

func (f *fakeTokens) Token(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.token, f.err
}

func (f *fakeTokens) Refused(token string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refused = append(f.refused, token)
	if f.next != "" {
		f.token = f.next
	}
}

// fakeAsker hands each request asked to the test and waits for its answer,
// an approval holding for duration, and hands each record to the test.
type fakeAsker struct {
	asked    chan runproxy.Request
	answers  chan bool
	recorded chan record
	duration permissions.Duration
}

// record is one request recorded with nobody asked.
type record struct {
	req    runproxy.Request
	d      permissions.Decision
	reason string
}

func newFakeAsker() *fakeAsker {
	return &fakeAsker{asked: make(chan runproxy.Request, 4), answers: make(chan bool, 4), recorded: make(chan record, 8), duration: permissions.DurationOnce}
}

func (a *fakeAsker) Ask(ctx context.Context, r runproxy.Request) (permissions.Answer, error) {
	a.asked <- r
	select {
	case ok := <-a.answers:
		return permissions.Answer{Approved: ok, Duration: a.duration}, nil
	case <-ctx.Done():
		return permissions.Answer{}, ctx.Err()
	}
}

func (a *fakeAsker) Record(_ context.Context, r runproxy.Request, d permissions.Decision, reason string) error {
	a.recorded <- record{r, d, reason}
	return nil
}

// sessionIn is a chat's session whose every account has mode, under rules.
func sessionIn(mode permissions.Mode, rules ...permissions.Rule) session.Session {
	return session.Session{Kind: session.Chat, Policy: func(context.Context, permissions.Target, string) permissions.Policy {
		return permissions.Policy{Mode: mode, Rules: rules}
	}}
}

// harness is a proxy over a fake GitHub, served on plain HTTP as the run's
// door serves it.
type harness struct {
	git, api *upstream
	tokens   *fakeTokens
	grant    *runproxy.Grant
	off      atomic.Bool // the grant's switch, on until a test turns it off
	front    *httptest.Server
}

// newHarness is a proxy whose requests that ask go to asker, nil for none.
func newHarness(t *testing.T, asker runproxy.Asker) *harness {
	return newHarnessWith(t, asker, runproxy.Limits{QPS: 1000, Burst: 1000, Open: 8, Bodies: 8, Challenges: true})
}

func newHarnessWith(t *testing.T, asker runproxy.Asker, limits runproxy.Limits) *harness {
	return newHarnessIn(t, asker, limits, sessionIn(permissions.ModeAsk))
}

// newHarnessIn is a proxy whose grant runs under sess.
func newHarnessIn(t *testing.T, asker runproxy.Asker, limits runproxy.Limits, sess session.Session) *harness {
	h := &harness{git: newUpstream(t), api: newUpstream(t), tokens: &fakeTokens{token: "users-own-token"}}
	h.grant = runproxy.NewGrant(permissions.GitHub, func(context.Context) bool { return !h.off.Load() }, sess, asker, "kubetail: nobody can be asked.", limits)
	t.Cleanup(h.grant.End)
	client := h.git.srv.Client()
	pool := client.Transport.(*http.Transport).TLSClientConfig.RootCAs
	pool.AddCert(h.api.srv.Certificate())
	h.front = httptest.NewServer(New(h.grant, h.tokens, Upstreams{Git: h.git.srv.URL, API: h.api.srv.URL, Client: client}))
	t.Cleanup(h.front.Close)
	return h
}

// client is a client whose every request goes to the proxy with token as its
// proxy credentials, "" for none, and follows no redirect.
func (h *harness) client(token string) *http.Client {
	proxy, _ := url.Parse(h.front.URL)
	if token != "" {
		proxy.User = url.UserPassword("kubetail", token)
	}
	return &http.Client{
		Transport:     &http.Transport{Proxy: http.ProxyURL(proxy)},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// do sends method to rawURL through the proxy with the grant's token, with
// body and header, and answers the response and its body.
func (h *harness) do(t *testing.T, method, rawURL, body string, header http.Header) (*http.Response, string) {
	t.Helper()
	return h.doAs(t, h.grant.Token(), method, rawURL, body, header)
}

func (h *harness) doAs(t *testing.T, token, method, rawURL, body string, header http.Header) (*http.Response, string) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, rawURL, r)
	require.NoError(t, err)
	maps.Copy(req.Header, header)
	resp, err := h.client(token).Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, string(out)
}

// message is the message of a refusal the API host wrote in GitHub's shape.
func message(t *testing.T, body string) string {
	t.Helper()
	var m struct {
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &m), body)
	return m.Message
}

// jsonType is the Content-Type gh sends to the API.
var jsonType = http.Header{"Content-Type": {"application/json; charset=utf-8"}}

const (
	apiURL = "http://" + APIHost
	gitURL = "http://" + GitHost
)

// With the switch off, a request with the grant's token is refused 401, never
// challenged, and nothing reaches GitHub.
func TestASwitchedOffPlatformIsUnauthorized(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	h.off.Store(true)
	resp, _ := h.do(t, http.MethodGet, apiURL+"/user", "", nil)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("Proxy-Authenticate"))
	assert.Empty(t, h.api.seen)
}

func TestARequestWithoutTheGrantsTokenIsChallenged(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	for name, token := range map[string]string{"none": "", "a wrong one": strings.Repeat("0", 64)} {
		resp, _ := h.doAs(t, token, http.MethodGet, apiURL+"/user", "", nil)
		assert.Equal(t, http.StatusProxyAuthRequired, resp.StatusCode, name)
		assert.Equal(t, `Basic realm="kubetail"`, resp.Header.Get("Proxy-Authenticate"), name)
	}
	resp, _ := h.doAs(t, "", http.MethodGet, apiURL+"/user", "", http.Header{"Proxy-Authorization": {"Basic !!!"}})
	assert.Equal(t, http.StatusProxyAuthRequired, resp.StatusCode, "credentials that do not decode")
	resp, _ = h.doAs(t, "", http.MethodGet, apiURL+"/user", "", http.Header{"Proxy-Authorization": {"Bearer " + h.grant.Token()}})
	assert.Equal(t, http.StatusProxyAuthRequired, resp.StatusCode, "the token outside Basic credentials")
	token := h.grant.Token()
	h.grant.End()
	resp, _ = h.doAs(t, token, http.MethodGet, gitURL+"/o/r/info/refs?service=git-upload-pack", "", nil)
	assert.Equal(t, http.StatusProxyAuthRequired, resp.StatusCode, "an ended grant's")
	assert.Empty(t, h.api.seen)
	assert.Empty(t, h.git.seen)
}

// The upstream is one of two fixed origins whatever the request names.
func TestTheUpstreamIsFixed(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	resp, _ := h.do(t, http.MethodGet, "http://evil.example/user", "", nil)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "another host")
	resp, _ = h.do(t, http.MethodGet, "http://"+APIHost+":8080/user", "", nil)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "the host on another port")

	header := http.Header{"X-Forwarded-Host": {"evil.example"}, "Forwarded": {"host=evil.example"}}
	h.do(t, http.MethodGet, apiURL+"/user?host=evil.example&url=https://evil.example/", "", header)
	got := testutil.Recv(t, h.api.seen, "the read")
	assert.Equal(t, h.api.srv.Listener.Addr().String(), got.host)
	assert.Equal(t, "/user?host=evil.example&url=https://evil.example/", got.uri)
	assert.Empty(t, got.header.Values("X-Forwarded-Host"))
	assert.Empty(t, got.header.Values("Forwarded"))

	h.do(t, http.MethodGet, gitURL+"/o/r/info/refs?service=git-upload-pack", "", nil)
	assert.Equal(t, h.git.srv.Listener.Addr().String(), testutil.Recv(t, h.git.seen, "the fetch").host)
	assert.Empty(t, h.api.seen)
}

func TestGitHubsUpstreamsAreItsOrigins(t *testing.T) {
	t.Parallel()
	up := GitHub()
	assert.Equal(t, "https://github.com", up.Git)
	assert.Equal(t, "https://api.github.com", up.API)
	require.NotNil(t, up.Client)
	assert.Nil(t, up.Client.CheckRedirect, "New sets its own")
}

func TestARequestThatCannotBeReadAsSentIsRefused(t *testing.T) {
	t.Parallel()
	h := newHarness(t, newFakeAsker())
	for name, c := range map[string]struct {
		method, url string
		header      http.Header
	}{
		"a dot segment":           {http.MethodGet, apiURL + "/repos/o/r/./hooks", nil},
		"a dot-dot segment":       {http.MethodGet, apiURL + "/repos/o/r/x/../hooks", nil},
		"an empty segment":        {http.MethodGet, apiURL + "/repos//o/r", nil},
		"a trailing slash":        {http.MethodGet, apiURL + "/user/", nil},
		"an escaped slash":        {http.MethodGet, apiURL + "/repos/o/r%2Fhooks", nil},
		"an encoded API request":  {http.MethodPost, apiURL + "/repos/o/r/issues", http.Header{"Content-Encoding": {"gzip"}}},
		"an encoded Git ref list": {http.MethodGet, gitURL + "/o/r/info/refs?service=git-upload-pack", http.Header{"Content-Encoding": {"gzip"}}},
		"an encoded push":         {http.MethodPost, gitURL + "/o/r/git-receive-pack", http.Header{"Content-Encoding": {"gzip"}}},
	} {
		resp, _ := h.do(t, c.method, c.url, "x", c.header)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, name)
	}
	assert.Empty(t, h.api.seen)
	assert.Empty(t, h.git.seen)
}

func TestOnlyTheListedHeadersReachGitHub(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	sent := http.Header{
		"Authorization":          {"token kubetail-placeholder"},
		"Content-Type":           {"application/json"},
		"Cookie":                 {"user_session=x"},
		"X-Http-Method-Override": {"DELETE"},
		"X-Github-Otp":           {"123456"},
		"Accept":                 {"application/vnd.github+json"},
		"X-Github-Api-Version":   {"2022-11-28"},
		"User-Agent":             {"GitHub CLI 2.103.3"},
		"If-None-Match":          {`"etag"`},
		"Graphql-Features":       {"preview"},
		"Git-Protocol":           {"version=2"},
	}
	h.do(t, http.MethodGet, apiURL+"/user", "", sent)
	got := testutil.Recv(t, h.api.seen, "the read").header
	for _, name := range []string{"Accept", "X-Github-Api-Version", "If-None-Match"} {
		assert.Equal(t, sent.Get(name), got.Get(name), name)
	}
	assert.Equal(t, version.UserAgent()+" GitHub CLI 2.103.3", got.Get("User-Agent"), "Kubetail named ahead of the command")
	for _, name := range []string{"Cookie", "X-Http-Method-Override", "X-Github-Otp", "Graphql-Features", "Git-Protocol", "Proxy-Authorization"} {
		assert.Empty(t, got.Values(name), name)
	}

	h.do(t, http.MethodPost, apiURL+"/graphql", `{"query":"{ viewer { login } }"}`, sent)
	got = testutil.Recv(t, h.api.seen, "the GraphQL read").header
	assert.Equal(t, "preview", got.Get("Graphql-Features"), "on /graphql alone")

	for _, c := range []struct {
		method, url, body string
		protocol          bool
	}{
		{http.MethodGet, gitURL + "/o/r/info/refs?service=git-upload-pack", "", true},
		{http.MethodPost, gitURL + "/o/r.git/git-upload-pack", "0000", true},
		{http.MethodGet, gitURL + "/o/r/info/refs?service=git-receive-pack", "", false},
		{http.MethodPost, gitURL + "/o/r/git-receive-pack", "0000", false},
	} {
		h.do(t, c.method, c.url, c.body, sent)
		got := testutil.Recv(t, h.git.seen, c.url).header
		assert.Equal(t, c.protocol, got.Get("Git-Protocol") == "version=2", c.url)
		assert.Empty(t, got.Values("Cookie"), c.url)
		assert.Equal(t, version.UserAgent()+" GitHub CLI 2.103.3", got.Get("User-Agent"), c.url)
	}
}

// Each upstream sees the user's token in its own form, never the run's
// placeholder.
func TestTheTokenIsAddedOutside(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	sent := http.Header{"Authorization": {"token kubetail-placeholder"}}
	h.do(t, http.MethodGet, apiURL+"/user", "", sent)
	assert.Equal(t, []string{"Bearer users-own-token"}, testutil.Recv(t, h.api.seen, "the read").header.Values("Authorization"))
	h.do(t, http.MethodGet, gitURL+"/o/r/info/refs?service=git-upload-pack", "", sent)
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:users-own-token"))
	assert.Equal(t, []string{basic}, testutil.Recv(t, h.git.seen, "the fetch").header.Values("Authorization"))
}

// A HEAD passes as a read, and a read's body, which nothing reads, is not
// sent.
func TestAReadIsSentWithoutABody(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	resp, _ := h.do(t, http.MethodHead, apiURL+"/repos/o/r", "", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, http.MethodHead, testutil.Recv(t, h.api.seen, "the head").method)
	h.do(t, http.MethodGet, apiURL+"/user", `{"unread":true}`, nil)
	assert.Empty(t, testutil.Recv(t, h.api.seen, "the read").body)
}

func TestAnUnknownMethodIsRefused(t *testing.T) {
	t.Parallel()
	h := newHarness(t, newFakeAsker())
	resp, body := h.do(t, "PROPFIND", apiURL+"/user", "", nil)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, refusedMethod, message(t, body))
}

func TestAGitRequestThatIsNotSmartHTTPIsRefused(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	for _, path := range []string{"/o/r/HEAD", "/o/r.git/info/lfs/objects/batch", "/o/r/pulls", "/login"} {
		resp, body := h.do(t, http.MethodGet, gitURL+path, "", nil)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, path)
		assert.Equal(t, "text/plain; charset=utf-8", resp.Header.Get("Content-Type"), path)
		assert.True(t, strings.HasPrefix(body, "kubetail: "), body)
	}
	assert.Empty(t, h.git.seen)
}

func TestACredentialPathIsRefusedUnasked(t *testing.T) {
	t.Parallel()
	a := newFakeAsker()
	h := newHarness(t, a)
	for _, path := range []string{"/app-manifests/abc/conversions", "/repositories/42/actions/runners/registration-token"} {
		resp, body := h.do(t, http.MethodPost, apiURL+path, "{}", nil)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, path)
		assert.Equal(t, refusedCredential, message(t, body))
	}
	assert.Empty(t, a.asked)
	assert.Empty(t, h.api.seen)
}

// A data read asks under ask mode, and once approved its answer's link to
// another host passes; a read's never does.
func TestARedirectToAnotherHostNeedsAnApprovedDataRead(t *testing.T) {
	t.Parallel()
	a := newFakeAsker()
	h := newHarness(t, a)
	h.api.answers(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "https://results.example/signed?sig=x")
		w.WriteHeader(http.StatusFound)
	})

	resp, body := h.do(t, http.MethodGet, apiURL+"/repos/o/r/contents/README.md", "", nil)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "a read's")
	assert.Equal(t, refusedLink, message(t, body))
	assert.Empty(t, resp.Header.Get("Location"))
	testutil.Recv(t, h.api.seen, "the read")

	a.answers <- true
	resp, _ = h.do(t, http.MethodGet, apiURL+"/repos/o/r/actions/runs/1/logs", "", nil)
	asked := testutil.Recv(t, a.asked, "the data read")
	assert.Equal(t, permissions.Action{
		Principal: permissions.Chat, Target: permissions.GitHub, Scope: "o/r", AccountName: "o", Service: "api", Operation: "get",
		Resource: "actions", AccessLevel: permissions.DataRead, Summary: "GitHub: GET /repos/o/r/actions/runs/1/logs",
	}, asked.Action)
	assert.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Equal(t, "https://results.example/signed?sig=x", resp.Header.Get("Location"))
	assert.Equal(t, "/repos/o/r/actions/runs/1/logs", testutil.Recv(t, h.api.seen, "the approved read").uri)

	a.answers <- false
	resp, body = h.do(t, http.MethodGet, apiURL+"/repositories/42/tarball", "", nil)
	testutil.Recv(t, a.asked, "the tarball")
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Contains(t, message(t, body), "did not approve")
	assert.Empty(t, h.api.seen)
}

// The proxy follows no redirect: the client gets the answer as sent, with a
// link to the upstream's own origin handed back as the route's.
func TestARedirectOnTheSameUpstreamNamesTheRoute(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	h.api.answers(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/o/old" {
			w.Header().Set("Location", h.api.srv.URL+"/repositories/42?x=1")
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
		w.Header().Set("Location", r.URL.Query().Get("to"))
		w.WriteHeader(http.StatusFound)
	})
	resp, _ := h.do(t, http.MethodGet, apiURL+"/repos/o/old", "", nil)
	assert.Equal(t, http.StatusMovedPermanently, resp.StatusCode)
	assert.Equal(t, apiURL+"/repositories/42?x=1", resp.Header.Get("Location"))
	assert.Len(t, h.api.seen, 1, "not followed")
	<-h.api.seen

	resp, _ = h.do(t, http.MethodGet, apiURL+"/x?to=/relative", "", nil)
	assert.Equal(t, "/relative", resp.Header.Get("Location"), "a relative link names the route already")
	<-h.api.seen

	lookalike := h.api.srv.URL + "0/x"
	resp, _ = h.do(t, http.MethodGet, apiURL+"/x?to="+url.QueryEscape(lookalike), "", nil)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "a host that starts with the upstream's name is another host")
	<-h.api.seen

	h.git.answers(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", h.git.srv.URL+"/o/new.git/info/refs?service=git-upload-pack")
		w.WriteHeader(http.StatusMovedPermanently)
	})
	resp, _ = h.do(t, http.MethodGet, gitURL+"/o/old/info/refs?service=git-upload-pack", "", nil)
	assert.Equal(t, gitURL+"/o/new.git/info/refs?service=git-upload-pack", resp.Header.Get("Location"))
}

func TestALinkHeaderNamesTheRoute(t *testing.T) {
	t.Parallel()
	a := newFakeAsker()
	h := newHarness(t, a)
	h.api.answers(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Link", `<`+h.api.srv.URL+`/user/repos?page=2>; rel="next", <`+h.api.srv.URL+`/user/repos?page=9>; rel="last"`)
		w.Header().Add("Link", `</user/repos?page=1>; rel="first"`)
		w.Header().Add("Link", `<https://downloads.example/signed>; rel="alternate"`)
		w.Header().Add("Set-Cookie", "session=x")
	})
	resp, _ := h.do(t, http.MethodGet, apiURL+"/user/repos", "", nil)
	assert.Equal(t, []string{
		`<` + apiURL + `/user/repos?page=2>; rel="next", <` + apiURL + `/user/repos?page=9>; rel="last"`,
		`</user/repos?page=1>; rel="first"`,
	}, resp.Header.Values("Link"), "a link to another host is dropped from a read")
	assert.Empty(t, resp.Header.Values("Set-Cookie"))

	a.answers <- true
	resp, _ = h.do(t, http.MethodGet, apiURL+"/repos/o/r/actions/runs/1/logs", "", nil)
	testutil.Recv(t, a.asked, "the data read")
	assert.Contains(t, resp.Header.Values("Link"), `<https://downloads.example/signed>; rel="alternate"`, "an approved data read may hand one back")

	h.git.answers(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Link", `<`+h.git.srv.URL+`/o/r/info/refs?service=git-upload-pack>; rel="next"`)
	})
	resp, _ = h.do(t, http.MethodGet, gitURL+"/o/r/info/refs?service=git-upload-pack", "", nil)
	assert.Equal(t, []string{`<` + gitURL + `/o/r/info/refs?service=git-upload-pack>; rel="next"`}, resp.Header.Values("Link"))
}

// A link names the upstream's origin however it spells it: the scheme and
// host in any case, the default port or none, a path or none.
func TestAnyLinkToTheUpstreamsOriginNamesTheRoute(t *testing.T) {
	t.Parallel()
	rt := newRoute("https://api.github.com", APIHost, true)
	for raw, want := range map[string]string{
		"https://api.github.com/repositories/1?x=1": apiURL + "/repositories/1?x=1",
		"https://api.github.com:443/repositories/1": apiURL + "/repositories/1",
		"HTTPS://API.GITHUB.COM/repositories/1":     apiURL + "/repositories/1",
		"https://api.github.com?x=1":                apiURL + "?x=1",
		"/relative?x=1":                             "/relative?x=1",
	} {
		got, ok := rt.handBackURL(raw, false)
		assert.True(t, ok, raw)
		assert.Equal(t, want, got, raw)
	}
	for _, raw := range []string{
		"https://api.github.com.evil/x",
		"https://api.github.com:8443/x",
		"http://api.github.com/x",
		"//api.github.com/x",
		"https://codeload.github.com/o/r/tar.gz/main",
		"https://api.github.com/%zz",
	} {
		_, ok := rt.handBackURL(raw, false)
		assert.False(t, ok, raw)
		got, ok := rt.handBackURL(raw, true)
		assert.Equal(t, ok, raw != "https://api.github.com/%zz", raw)
		if ok {
			assert.Equal(t, raw, got, "another host passes as it is where it may")
		}
	}
}

func TestAnAPIChangeShowsWhatIsSent(t *testing.T) {
	t.Parallel()
	a := newFakeAsker()
	h := newHarness(t, a)
	a.answers <- true
	body := `{"title":"a bug"}`
	resp, got := h.do(t, http.MethodPost, apiURL+"/repos/o/r/issues?x=1", body, http.Header{
		"Content-Type": {"application/json"}, "Accept": {"application/vnd.github+json"}, "X-Github-Api-Version": {"2022-11-28"},
	})
	asked := testutil.Recv(t, a.asked, "the change")
	assert.Equal(t, runproxy.Request{
		Action: permissions.Action{
			Principal: permissions.Chat, Target: permissions.GitHub, Scope: "o/r", AccountName: "o", Service: "api", Operation: "post",
			Resource: "issues", AccessLevel: permissions.Write, Summary: "GitHub: POST /repos/o/r/issues",
		},
		Grantable:   true,
		CommandRule: "Allow post of api issues in GitHub o / r for this command",
		ChatRule:    "Allow writes in GitHub o / r",
		Sent: runproxy.Sent{
			Host: h.api.srv.Listener.Addr().String(), Method: http.MethodPost, Path: "/repos/o/r/issues?x=1",
			ContentType: "application/json", Body: []byte(body),
			Details: []runproxy.Detail{{Label: "Accept", Value: "application/vnd.github+json"}, {Label: "API version", Value: "2022-11-28"}},
		},
	}, asked)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "ok", got)
	assert.Equal(t, body, testutil.Recv(t, h.api.seen, "the change").body)

	a.answers <- true
	doc := `{"query":"mutation M { gone: deleteRef(input: {refId: \"x\"}) { clientMutationId } }","operationName":"M"}`
	h.do(t, http.MethodPost, apiURL+"/graphql", doc, http.Header{"Content-Type": {"application/json"}, "Graphql-Features": {"preview"}})
	asked = testutil.Recv(t, a.asked, "the mutation")
	assert.Equal(t, permissions.Action{
		Principal: permissions.Chat, Target: permissions.GitHub, Service: "graphql", Operation: "mutation", Resource: "graphql",
		AccessLevel: permissions.Destructive, Summary: "GitHub: GraphQL deleteRef",
	}, asked.Action)
	assert.Equal(t, []runproxy.Detail{{Label: "Mutation", Value: "deleteRef"}, {Label: "Operation", Value: "M"}, {Label: "Features", Value: "preview"}}, asked.Sent.Details)
	assert.Equal(t, doc, string(asked.Sent.Body))
	assert.Equal(t, doc, testutil.Recv(t, h.api.seen, "the mutation").body)
}

func TestAGraphQLReadIsForwardedUnasked(t *testing.T) {
	t.Parallel()
	a := newFakeAsker()
	h := newHarness(t, a)
	doc := `{"query":"{ viewer { login } }"}`
	resp, _ := h.do(t, http.MethodPost, apiURL+"/graphql", doc, jsonType)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, doc, testutil.Recv(t, h.api.seen, "the query").body)
	resp, _ = h.do(t, http.MethodPost, apiURL+"/graphql", doc, http.Header{"Content-Type": {"Application/JSON"}})
	assert.Equal(t, http.StatusOK, resp.StatusCode, "the media type in any case")
	testutil.Recv(t, h.api.seen, "the second query")
	assert.Empty(t, a.asked)

	mutation := url.QueryEscape(`mutation { deleteRef(input: {refId: "x"}) { clientMutationId } }`)
	for name, c := range map[string]struct {
		method, path, body string
		header             http.Header
	}{
		"a GET":                   {http.MethodGet, "/graphql", "", jsonType},
		"a subscription":          {http.MethodPost, "/graphql", `{"query":"subscription { x }"}`, jsonType},
		"no body":                 {http.MethodPost, "/graphql", "", jsonType},
		"a query string":          {http.MethodPost, "/graphql?query=" + mutation, doc, jsonType},
		"a body that is not JSON": {http.MethodPost, "/graphql", doc, http.Header{"Content-Type": {"text/plain"}}},
		"no media type":           {http.MethodPost, "/graphql", doc, nil},
	} {
		resp, body := h.do(t, c.method, apiURL+c.path, c.body, c.header)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, name)
		assert.True(t, strings.HasPrefix(message(t, body), "kubetail: "), body)
	}
	assert.Empty(t, h.api.seen)
}

func TestOnlyAnApprovedChangeReachesGitHub(t *testing.T) {
	t.Parallel()
	a := newFakeAsker()
	h := newHarness(t, a)
	a.answers <- false
	resp, body := h.do(t, http.MethodDelete, apiURL+"/repos/o/r", "", nil)
	assert.Equal(t, permissions.Destructive, testutil.Recv(t, a.asked, "the delete").Action.AccessLevel)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Contains(t, message(t, body), "did not approve")

	none := newHarness(t, nil)
	resp, body = none.do(t, http.MethodPost, apiURL+"/repos/o/r/issues", "{}", nil)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "kubetail: nobody can be asked.", message(t, body))
	resp, body = none.do(t, http.MethodPost, gitURL+"/o/r/git-receive-pack",
		pushBody(nil, "report-status", []string{idA + " " + idB + " refs/heads/main"}, nil), nil)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "kubetail: nobody can be asked.\n", body)
	assert.Empty(t, h.api.seen)
	assert.Empty(t, none.api.seen)
	assert.Empty(t, none.git.seen)
}

// A ref deleted by push, by REST under either form of its path, or by
// GraphQL is destructive.
func TestADeletedRefIsDestructive(t *testing.T) {
	t.Parallel()
	a := newFakeAsker()
	h := newHarness(t, a)
	for name, c := range map[string]struct{ method, url, body string }{
		"a push":             {http.MethodPost, gitURL + "/o/r/git-receive-pack", pushBody(nil, "report-status", []string{idB + " " + zero + " refs/heads/old"}, nil)},
		"REST by name":       {http.MethodDelete, apiURL + "/repos/o/r/git/refs/heads/old", ""},
		"REST by id":         {http.MethodDelete, apiURL + "/repositories/42/git/refs/heads/old", ""},
		"a GraphQL mutation": {http.MethodPost, apiURL + "/graphql", `{"query":"mutation { deleteRef(input: {refId: \"x\"}) { clientMutationId } }"}`},
	} {
		a.answers <- false
		resp, _ := h.do(t, c.method, c.url, c.body, jsonType)
		assert.Equal(t, permissions.Destructive, testutil.Recv(t, a.asked, name).Action.AccessLevel, name)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, name)
	}
	assert.Empty(t, h.api.seen)
	assert.Empty(t, h.git.seen)
}

// A push asks with its refs, and once approved its body reaches GitHub
// byte for byte.
func TestAPushAsksAndIsForwardedWhole(t *testing.T) {
	t.Parallel()
	a := newFakeAsker()
	h := newHarness(t, a)
	body := pushBody(nil, "report-status push-options", []string{idA + " " + idB + " refs/heads/main"}, []string{"ci.skip"})
	a.answers <- true
	resp, _ := h.do(t, http.MethodPost, gitURL+"/o/r.git/git-receive-pack", body, http.Header{"Content-Type": {"application/x-git-receive-pack-request"}})
	asked := testutil.Recv(t, a.asked, "the push")
	assert.Equal(t, runproxy.Request{
		Action: permissions.Action{
			Principal: permissions.Chat, Target: permissions.GitHub, Scope: "o/r", AccountName: "o", Service: "git", Operation: "push",
			Resource: "git-receive-pack", AccessLevel: permissions.Write, Once: true, Summary: "GitHub: push to o/r",
		},
		Sent: runproxy.Sent{
			Host: h.git.srv.Listener.Addr().String(), Method: http.MethodPost, Path: "/o/r.git/git-receive-pack",
			ContentType: "application/x-git-receive-pack-request", Unshown: unshownPack,
			Details: []runproxy.Detail{
				{Label: "Repository", Value: "o/r"},
				{Label: "Updates", Value: "refs/heads/main " + idA + " → " + idB},
				{Label: "Push option", Value: "ci.skip"},
			},
		},
	}, asked)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, body, testutil.Recv(t, h.git.seen, "the push").body)
}

func TestAPushProbePasses(t *testing.T) {
	t.Parallel()
	a := newFakeAsker()
	h := newHarness(t, a)
	resp, _ := h.do(t, http.MethodPost, gitURL+"/o/r/git-receive-pack", "0000", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "0000", testutil.Recv(t, h.git.seen, "the probe").body)
	assert.Empty(t, a.asked)
}

// A push that does not read, and one that names more refs than fit a
// request, are each refused in their own words, unasked.
func TestAPushTheProxyCannotReadSaysWhy(t *testing.T) {
	t.Parallel()
	a := newFakeAsker()
	h := newHarness(t, a)
	resp, body := h.do(t, http.MethodPost, gitURL+"/o/r/git-receive-pack", pkt("push-cert\x00report-status\n"), nil)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, refusedPush+"\n", body)

	var commands []string
	for range 700 {
		commands = append(commands, zero+" "+idA+" refs/tags/a-tag-with-a-name-long-enough-to-fill-the-opening")
	}
	resp, body = h.do(t, http.MethodPost, gitURL+"/o/r/git-receive-pack", pushBody(nil, "report-status", commands, nil), nil)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, refusedPushTooLarge+"\n", body)
	assert.Empty(t, a.asked)
	assert.Empty(t, h.git.seen)
}

// A fetch's body streams unread, so its encoding passes with it.
func TestAFetchIsStreamedWhateverItsEncoding(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	body := "\x1f\x8b\x08\x00 not text \xff"
	resp, _ := h.do(t, http.MethodPost, gitURL+"/o/r/git-upload-pack", body, http.Header{"Content-Encoding": {"gzip"}, "Content-Type": {"application/x-git-upload-pack-request"}})
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	got := testutil.Recv(t, h.git.seen, "the fetch")
	assert.Equal(t, body, got.body)
	assert.Equal(t, "gzip", got.header.Get("Content-Encoding"))
}

func TestNoLoginIsOneFixedMessage(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	h.tokens.err = errors.New("no GitHub login was found: gh said something")
	resp, body := h.do(t, http.MethodGet, apiURL+"/user", "", nil)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "kubetail: no GitHub login was found; run `gh auth login` outside Kubetail.", message(t, body))
	resp, body = h.do(t, http.MethodGet, gitURL+"/o/r/info/refs?service=git-upload-pack", "", nil)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, refusedNoLogin+"\n", body)
	assert.Empty(t, h.api.seen)
}

// An upstream 401 is reported to the token source, and the request after it
// carries the token it reads anew.
func TestARefusedTokenIsReadAgain(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	h.tokens.next = "users-new-token"
	h.api.answers(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	resp, _ := h.do(t, http.MethodGet, apiURL+"/user", "", nil)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Equal(t, "Bearer users-own-token", testutil.Recv(t, h.api.seen, "the refused read").header.Get("Authorization"))
	assert.Equal(t, []string{"users-own-token"}, h.tokens.refused)
	h.do(t, http.MethodGet, apiURL+"/user", "", nil)
	assert.Equal(t, "Bearer users-new-token", testutil.Recv(t, h.api.seen, "the next read").header.Get("Authorization"))
}

func TestAnUnreachableGitHubIs502(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	h.api.srv.Close()
	resp, body := h.do(t, http.MethodGet, apiURL+"/user", "", nil)
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Equal(t, refusedUnreachable, message(t, body))
}

// A body past what the user can be asked to read is 413 and one that ends
// early is 400, each in GitHub's shape.
func TestABodyTheGrantCannotHoldIsRefused(t *testing.T) {
	t.Parallel()
	h := newHarness(t, newFakeAsker())
	for _, path := range []string{"/repos/o/r/issues", "/graphql"} {
		resp, body := h.do(t, http.MethodPost, apiURL+path, strings.Repeat("x", 1<<20+1), jsonType)
		assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode, path)
		assert.Equal(t, runproxy.BodyRefusal(http.StatusRequestEntityTooLarge), message(t, body), path)
	}

	conn, err := net.Dial("tcp", h.front.Listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	credentials := base64.StdEncoding.EncodeToString([]byte("kubetail:" + h.grant.Token()))
	_, err = io.WriteString(conn, "POST "+apiURL+"/repos/o/r/issues HTTP/1.1\r\nHost: "+APIHost+
		"\r\nProxy-Authorization: Basic "+credentials+"\r\nContent-Length: 10\r\n\r\n{")
	require.NoError(t, err)
	require.NoError(t, conn.(*net.TCPConn).CloseWrite())
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, runproxy.BodyRefusal(http.StatusBadRequest), message(t, string(body)))
	assert.Empty(t, h.api.seen)
}

// A change waiting on the user holds its body, so with every body slot
// held the next body is 429.
func TestBodiesHeldAreCapped(t *testing.T) {
	t.Parallel()
	a := newFakeAsker()
	h := newHarnessWith(t, a, runproxy.Limits{QPS: 1000, Burst: 1000, Open: 8, Bodies: 1, Challenges: true})
	answered := make(chan int, 1)
	go func() {
		resp, _ := h.do(t, http.MethodPost, apiURL+"/repos/o/r/issues", "{}", nil)
		answered <- resp.StatusCode
	}()
	testutil.Recv(t, a.asked, "the first change")
	resp, body := h.do(t, http.MethodPost, apiURL+"/repos/o/r/issues", "{}", nil)
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, runproxy.BodyRefusal(http.StatusTooManyRequests), message(t, body))
	a.answers <- false
	assert.Equal(t, http.StatusForbidden, testutil.Recv(t, answered, "the first change's answer"))
}

// A read past the grant's open requests is 429 with no Retry-After, which
// no client retries.
func TestAReadPastTheOpenCapIs429(t *testing.T) {
	t.Parallel()
	h := newHarnessWith(t, nil, runproxy.Limits{QPS: 1000, Burst: 1000, Open: 1, Bodies: 1, Challenges: true})
	release := make(chan struct{})
	h.api.answers(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release
	})
	t.Cleanup(func() { close(release) })

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, apiURL+"/user", nil)
	require.NoError(t, err)
	first, err := h.client(h.grant.Token()).Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Body.Close() })
	testutil.Recv(t, h.api.seen, "the first read")

	resp, body := h.do(t, http.MethodGet, apiURL+"/user", "", nil)
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("Retry-After"))
	assert.True(t, strings.HasPrefix(message(t, body), "kubetail: "), body)
	resp, body = h.do(t, http.MethodPost, apiURL+"/graphql", `{"query":"{ viewer { login } }"}`, jsonType)
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode, "no body slot")
	assert.True(t, strings.HasPrefix(message(t, body), "kubetail: "), body)
}

// A push's opening is read in one of the grant's body slots, so stalled
// uploads cannot hold more than the grant's bodies, and the grant's end cuts
// a stalled read short, so Wait returns with the upload still open.
func TestAStalledPushHoldsABodySlotUntilTheGrantEnds(t *testing.T) {
	t.Parallel()
	h := newHarnessWith(t, nil, runproxy.Limits{QPS: 1000, Burst: 1000, Open: 8, Bodies: 1, Challenges: true})
	// Two stalled pushes race for the one slot: the one that takes it holds
	// it, so the other is answered at once, and only the holder stays open.
	answered := make(chan int, 2)
	for range 2 {
		conn := stalledPush(t, h)
		go func() {
			resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				answered <- 0
				return
			}
			_ = resp.Body.Close()
			answered <- resp.StatusCode
		}()
	}
	assert.Equal(t, http.StatusTooManyRequests, testutil.Recv(t, answered, "the push that found the slot held"))

	push := pushBody(nil, "report-status", []string{idA + " " + idB + " refs/heads/main"}, nil)
	resp, body := h.do(t, http.MethodPost, gitURL+"/o/r/git-receive-pack", push, nil)
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, runproxy.BodyRefusal(http.StatusTooManyRequests)+"\n", body)

	h.grant.End()
	testutil.WaitReturn(t, h.grant.Wait, "Wait, with the upload still open")
	assert.Empty(t, h.git.seen)
}

// stalledPush opens a push through the proxy whose body stops partway
// through its opening. Its length is past the 256 KiB net/http drains from
// an unread body before it answers, so a refusal is answered at once.
func stalledPush(t *testing.T, h *harness) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", h.front.Listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	credentials := base64.StdEncoding.EncodeToString([]byte("kubetail:" + h.grant.Token()))
	_, err = fmt.Fprintf(conn, "POST %s/o/r/git-receive-pack HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: Basic %s\r\nContent-Length: %d\r\n\r\n%s",
		gitURL, GitHost, credentials, 1<<20, pkt(idA + " " + idB + " refs/heads/main\x00report-status\n")[:20])
	require.NoError(t, err)
	return conn
}

// A fetch's body streams to GitHub unread, and the grant's end cuts a
// stalled upload short, so Wait returns with the upload still open.
func TestAStalledFetchEndsWithTheGrant(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	conn, err := net.Dial("tcp", h.front.Listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	credentials := base64.StdEncoding.EncodeToString([]byte("kubetail:" + h.grant.Token()))
	_, err = fmt.Fprintf(conn, "POST %s/o/r/git-upload-pack HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: Basic %s\r\nContent-Length: 1000\r\n\r\n0032want ",
		gitURL, GitHost, credentials)
	require.NoError(t, err)
	assert.Equal(t, "/o/r/git-upload-pack", testutil.Recv(t, h.git.arrived, "the fetch at GitHub"))

	h.grant.End()
	testutil.WaitReturn(t, h.grant.Wait, "Wait, with the upload still open")
}

// An answer GitHub breaks off mid-body is broken off to the client too, never
// completed as if it were whole.
func TestATruncatedAnswerIsNotCompleted(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	h.api.answers(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "the first half")
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, apiURL+"/repos/o/r/contents/big", nil)
	require.NoError(t, err)
	resp, err := h.client(h.grant.Token()).Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	assert.Error(t, err, "the client sees the answer broken off")
	assert.Equal(t, "the first half", string(body))
}

// limits are a harness's default limits.
var limits = runproxy.Limits{QPS: 1000, Burst: 1000, Open: 8, Bodies: 8, Challenges: true}

// githubRule is a GitHub rule of effect at level in scope, naming everything
// else by "*".
func githubRule(effect permissions.Effect, level permissions.AccessLevel, scope string) permissions.Rule {
	return permissions.Rule{ID: string(effect), Effect: effect, AccessLevel: level, Principal: "*", Target: "github",
		Scope: scope, Service: "*", Operation: "*", Resource: "*"}
}

func TestARuleAndAModeDecideAGitHubAction(t *testing.T) {
	t.Parallel()
	const issue = "/repos/o/r/issues"
	t.Run("auto forwards a write unasked and records it", func(t *testing.T) {
		t.Parallel()
		a := newFakeAsker()
		h := newHarnessIn(t, a, limits, sessionIn(permissions.ModeAuto))
		resp, _ := h.do(t, http.MethodPost, apiURL+issue, "{}", jsonType)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		rec := testutil.Recv(t, a.recorded, "the record")
		assert.Equal(t, permissions.Allowed, rec.d)
		assert.Equal(t, "auto mode", rec.reason)
		testutil.Recv(t, h.api.seen, "the write")
		assert.Empty(t, a.asked)
	})
	// An ordinary route whose body changes who may push asks, though auto
	// would run the route's other edits.
	t.Run("auto asks for a pull request's maintainer edits", func(t *testing.T) {
		t.Parallel()
		a := newFakeAsker()
		a.answers <- false
		h := newHarnessIn(t, a, limits, sessionIn(permissions.ModeAuto))
		resp, _ := h.do(t, http.MethodPatch, apiURL+"/repos/o/r/pulls/1", `{"maintainer_can_modify":true}`, jsonType)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		assert.Equal(t, permissions.AccessControl, testutil.Recv(t, a.asked, "the edit").Action.AccessLevel)
		assert.Empty(t, h.api.seen)
	})
	t.Run("a document mixing a destructive and an access control mutation is refused", func(t *testing.T) {
		t.Parallel()
		a := newFakeAsker()
		h := newHarnessIn(t, a, limits, sessionIn(permissions.ModeAsk))
		doc := `mutation { deleteRef(input: {}) { clientMutationId } updateBranchProtectionRule(input: {}) { clientMutationId } }`
		resp, body := h.do(t, http.MethodPost, apiURL+"/graphql", string(graphQLBody(t, doc, "")), jsonType)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		assert.Equal(t, refusedGraphQLMixed, message(t, body))
		assert.Empty(t, a.asked)
		assert.Empty(t, h.api.seen)
	})
	t.Run("read-only denies", func(t *testing.T) {
		t.Parallel()
		a := newFakeAsker()
		h := newHarnessIn(t, a, limits, sessionIn(permissions.ModeReadOnly))
		resp, body := h.do(t, http.MethodPost, apiURL+issue, "{}", jsonType)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		assert.Contains(t, message(t, body), "this account is read-only")
		assert.Equal(t, permissions.Denied, testutil.Recv(t, a.recorded, "the record").d)
		assert.Empty(t, h.api.seen)
	})
	t.Run("an allow rule forwards", func(t *testing.T) {
		t.Parallel()
		a := newFakeAsker()
		h := newHarnessIn(t, a, limits, sessionIn(permissions.ModeAsk, githubRule(permissions.Allow, permissions.Write, "o/**")))
		resp, _ := h.do(t, http.MethodPost, apiURL+issue, "{}", jsonType)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "a rule allows it: Allow writes anywhere in GitHub o", testutil.Recv(t, a.recorded, "the record").reason)
		assert.Empty(t, a.asked)

		// A change off the ordinary table still asks.
		a.answers <- false
		resp, _ = h.do(t, http.MethodPost, apiURL+"/repos/o/r/transfer", "{}", jsonType)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		assert.False(t, testutil.Recv(t, a.asked, "the transfer").Grantable)
	})
	t.Run("a deny rule refuses", func(t *testing.T) {
		t.Parallel()
		a := newFakeAsker()
		h := newHarnessIn(t, a, limits, sessionIn(permissions.ModeAuto, githubRule(permissions.Deny, permissions.Write, "*")))
		resp, body := h.do(t, http.MethodPost, apiURL+issue, "{}", jsonType)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		assert.Contains(t, message(t, body), "a rule denies it")
		assert.Empty(t, h.api.seen)
	})
	t.Run("a command answer allows the next", func(t *testing.T) {
		t.Parallel()
		a := newFakeAsker()
		a.duration = permissions.DurationCommand
		h := newHarness(t, a)
		a.answers <- true
		resp, _ := h.do(t, http.MethodPost, apiURL+issue, "{}", jsonType)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		testutil.Recv(t, a.asked, "the first")
		resp, _ = h.do(t, http.MethodPost, apiURL+issue, "{}", jsonType)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, permissions.Allowed, testutil.Recv(t, a.recorded, "the second's record").d)
		assert.Empty(t, a.asked)
	})
	t.Run("a data read in the account runs under auto; one by id asks", func(t *testing.T) {
		t.Parallel()
		a := newFakeAsker()
		h := newHarnessIn(t, a, limits, sessionIn(permissions.ModeAuto))
		resp, _ := h.do(t, http.MethodGet, apiURL+"/repos/o/r/actions/runs/1/logs", "", nil)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, permissions.Allowed, testutil.Recv(t, a.recorded, "the record").d)
		testutil.Recv(t, h.api.seen, "the read")

		a.answers <- false
		h.do(t, http.MethodGet, apiURL+"/repositories/42/actions/runs/1/logs", "", nil)
		asked := testutil.Recv(t, a.asked, "the read by id")
		assert.False(t, asked.Grantable)
	})
}

// What the classifier read is what is recorded: no field is dropped on the
// way to the decision.
func TestNoProxyDropsAFieldBeforeTheDecision(t *testing.T) {
	t.Parallel()
	a := newFakeAsker()
	h := newHarnessIn(t, a, limits, sessionIn(permissions.ModeAuto))
	h.do(t, http.MethodPatch, apiURL+"/repos/O/R/issues/3", "{}", jsonType)
	rec := testutil.Recv(t, a.recorded, "the record")
	want := classifyAPI(http.MethodPatch, "/repos/O/R/issues/3", "").act
	want.Principal, want.Target, want.Summary = permissions.Chat, permissions.GitHub, "GitHub: PATCH /repos/O/R/issues/3"
	assert.Equal(t, want, rec.req.Action)
	assert.Equal(t, "o/r", rec.req.Action.Scope)
}
