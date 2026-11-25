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

package gcpproxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
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

// usersToken is the token the fake source lends.
const usersToken = "users-own-google-token"

// fakeSource lends usersToken, or err, and keeps what was dropped.
type fakeSource struct {
	mu      sync.Mutex
	err     error
	token   Token
	dropped []string
}

func (s *fakeSource) Token(context.Context) (Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return Token{}, s.err
	}
	if s.token.Value == "" {
		return Token{Value: usersToken, Account: "dev@example.com"}, nil
	}
	return s.token, nil
}

// lend makes tok the token the source lends from now on.
func (s *fakeSource) lend(tok Token) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = tok
}

func (s *fakeSource) Drop(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropped = append(s.dropped, token)
}

// seen is one request the fake upstream got.
type seen struct {
	host, method, uri, body string
	header                  http.Header
}

// upstream is Google as a TLS server: it keeps each request and answers
// with answer, by default a JSON body naming the request.
type upstream struct {
	srv    *httptest.Server
	seen   chan seen
	answer func(w http.ResponseWriter, r *http.Request)
}

func newUpstream(t *testing.T) *upstream {
	t.Helper()
	u := &upstream{seen: make(chan seen, 16)}
	u.answer = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"answered": r.Method + " " + r.URL.Path})
	}
	u.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		u.seen <- seen{host: r.Header.Get("X-Test-Host"), method: r.Method, uri: r.URL.RequestURI(), body: string(body), header: r.Header.Clone()}
		u.answer(w, r)
	}))
	t.Cleanup(u.srv.Close)
	return u
}

// RoundTrip sends r to the server whatever its host, carrying the host in
// X-Test-Host, as a transport pointed at Google would dial it.
func (u *upstream) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" {
		return nil, errors.New("not https")
	}
	out := r.Clone(r.Context())
	out.Header.Set("X-Test-Host", r.URL.Host)
	out.URL.Host = u.srv.Listener.Addr().String()
	return u.srv.Client().Transport.RoundTrip(out)
}

// asker hands each request to the test and answers what it is told, once
// unless told otherwise; it keeps each record.
type asker struct {
	asked    chan runproxy.Request
	answers  chan bool
	duration permissions.Duration
	records  chan record
}

// record is one decision recorded with nobody asked.
type record struct {
	req    runproxy.Request
	d      permissions.Decision
	reason string
}

func newAsker() *asker {
	return &asker{asked: make(chan runproxy.Request, 8), answers: make(chan bool, 8), records: make(chan record, 16), duration: permissions.DurationOnce}
}

func (a *asker) Ask(ctx context.Context, r runproxy.Request) (permissions.Answer, error) {
	a.asked <- r
	select {
	case ok := <-a.answers:
		return permissions.Answer{Approved: ok, Duration: a.duration}, nil
	case <-ctx.Done():
		return permissions.Answer{}, ctx.Err()
	}
}

func (a *asker) Record(_ context.Context, r runproxy.Request, d permissions.Decision, reason string) error {
	a.records <- record{r, d, reason}
	return nil
}

// rig is a handler on a grant, served as the run's door serves it.
type rig struct {
	grant *runproxy.Grant
	off   atomic.Bool // the grant's switch, on until a test turns it off
	src   *fakeSource
	up    *upstream
	asker *asker
	door  *httptest.Server

	mu       sync.Mutex
	policy   permissions.Policy // every account's, ask until a test sets it
	accounts []string           // each account the grant read a policy for
}

// setPolicy makes p every account's policy from now on.
func (r *rig) setPolicy(p permissions.Policy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.policy = p
}

func newRig(t *testing.T, limits runproxy.Limits) *rig {
	t.Helper()
	r := &rig{src: &fakeSource{}, up: newUpstream(t), asker: newAsker(), policy: permissions.Policy{Mode: permissions.ModeAsk}}
	sess := session.Session{Kind: session.Chat, Policy: func(_ context.Context, target permissions.Target, account string) permissions.Policy {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.accounts = append(r.accounts, string(target)+" "+account)
		return r.policy
	}}
	r.grant = runproxy.NewGrant(permissions.GCP, func(context.Context) bool { return !r.off.Load() }, sess, r.asker, "", limits)
	t.Cleanup(r.grant.End)
	r.door = httptest.NewServer(New(r.grant, r.src, r.up))
	t.Cleanup(r.door.Close)
	return r
}

// With the switch off, a request with the grant's token is refused 401 and
// nothing reaches Google.
func TestASwitchedOffPlatformIsUnauthorized(t *testing.T) {
	r := newRig(t, roomy)
	r.off.Store(true)
	resp := r.get(t, "http://storage.gcp.kubetail.invalid/storage/v1/b/bkt/o/notes.txt?alt=media")
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Empty(t, r.up.seen)
}

var roomy = runproxy.Limits{QPS: 1000, Burst: 1000, Open: 8, Bodies: 8}

// send sends method to target, a URL on a route, through the door as gcloud
// does through its proxy: in absolute form, with token as its bearer.
func (r *rig) send(t *testing.T, method, target, token string, body io.Reader, header http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, target, body)
	require.NoError(t, err)
	maps.Copy(req.Header, header)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	door, _ := url.Parse(r.door.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(door)}}
	resp, err := client.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// get sends a GET with the grant's token.
func (r *rig) get(t *testing.T, target string) *http.Response {
	t.Helper()
	return r.send(t, http.MethodGet, target, r.grant.Token(), nil, nil)
}

// refusal is a refusal's message, after checking it is Google's 403.
func refusal(t *testing.T, resp *http.Response) string {
	t.Helper()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	var e googleError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&e))
	assert.Equal(t, 403, e.Error.Code)
	assert.Equal(t, "PERMISSION_DENIED", e.Error.Status)
	assert.True(t, strings.HasPrefix(e.Error.Message, "kubetail: "), e.Error.Message)
	return e.Error.Message
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(b)
}

const clusters = "http://container.gcp.kubetail.invalid/v1/projects/p/locations/-/clusters"

func TestARequestCarriesTheUsersToken(t *testing.T) {
	r := newRig(t, roomy)

	resp := r.get(t, clusters)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.JSONEq(t, `{"answered":"GET /v1/projects/p/locations/-/clusters"}`, readAll(t, resp))
	s := testutil.Recv(t, r.up.seen, "the upstream request")
	assert.Equal(t, "Bearer "+usersToken, s.header.Get("Authorization"))
	assert.NotContains(t, strings.Join(s.header.Values("Authorization"), " "), r.grant.Token())
}

func TestOnlyTheGrantsBearerPasses(t *testing.T) {
	r := newRig(t, roomy)

	assert.Equal(t, http.StatusUnauthorized, r.send(t, http.MethodGet, clusters, "", nil, nil).StatusCode)
	assert.Equal(t, http.StatusUnauthorized, r.send(t, http.MethodGet, clusters, "another-token", nil, nil).StatusCode)
	token := r.grant.Token()
	r.grant.End()
	assert.Equal(t, http.StatusUnauthorized, r.send(t, http.MethodGet, clusters, token, nil, nil).StatusCode)
	assert.Empty(t, r.up.seen)
}

func TestTheUpstreamIsTheTablesHost(t *testing.T) {
	r := newRig(t, roomy)
	paths := map[string]string{
		"compute": "/compute/v1/projects/p/zones/z/instances", "container": "/v1/projects/p/locations/-/clusters",
		"iam": "/v1/projects/p/serviceAccounts", "logging": "/v2/projects/p/logs",
		"storage": "/storage/v1/b?project=p", "cloudresourcemanager": "/v3/projects/p",
	}
	for _, name := range Services() {
		resp := r.get(t, "http://"+Host(name)+paths[name])
		require.Equal(t, http.StatusOK, resp.StatusCode, name)
		s := testutil.Recv(t, r.up.seen, name)
		assert.Equal(t, name+".googleapis.com", s.host)
		assert.Equal(t, paths[name], s.uri, "the path and query as sent")
	}
}

func TestOnlyTheListedHeadersAreForwarded(t *testing.T) {
	r := newRig(t, roomy)
	header := http.Header{
		"X-Http-Method-Override": {"DELETE"}, "X-Goog-Api-Key": {"k"}, "X-Goog-Encryption-Key": {"e"},
		"Cookie": {"c"}, "Accept": {"application/json"}, "X-Goog-Api-Client": {"gl-python"},
		"X-Goog-User-Project": {"billing"}, "User-Agent": {"google-cloud-sdk"},
	}

	resp := r.send(t, http.MethodGet, clusters, r.grant.Token(), nil, header)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	s := testutil.Recv(t, r.up.seen, "the upstream request")
	assert.Equal(t, http.MethodGet, s.method)
	for _, name := range []string{"X-Http-Method-Override", "X-Goog-Api-Key", "X-Goog-Encryption-Key", "Cookie"} {
		assert.Empty(t, s.header.Values(name), name)
	}
	for _, name := range []string{"Accept", "X-Goog-Api-Client", "X-Goog-User-Project"} {
		assert.Equal(t, header.Get(name), s.header.Get(name), name)
	}
	assert.Equal(t, version.UserAgent()+" google-cloud-sdk", s.header.Get("User-Agent"), "Kubetail named ahead of the command")
}

func TestAChangeAsksWithItsAccount(t *testing.T) {
	r := newRig(t, roomy)
	target := "http://container.gcp.kubetail.invalid/v1/projects/p/locations/us-central1/clusters/dev"
	header := http.Header{"X-Goog-User-Project": {"billing"}}

	done := make(chan *http.Response, 1)
	go func() { done <- r.send(t, http.MethodDelete, target, r.grant.Token(), nil, header) }()
	req := testutil.Recv(t, r.asker.asked, "the request")
	assert.Equal(t, permissions.GCP, req.Action.Target)
	assert.Equal(t, permissions.Destructive, req.Action.AccessLevel)
	assert.Equal(t, "Google Cloud: DELETE container projects/p/locations/us-central1/clusters/dev", req.Action.Summary)
	assert.Equal(t, runproxy.Sent{
		Host: "container.googleapis.com", Method: http.MethodDelete, Path: "/v1/projects/p/locations/us-central1/clusters/dev",
		Details: []runproxy.Detail{{Label: "Account", Value: "dev@example.com"}, {Label: "Service", Value: "container"}, {Label: "Quota project", Value: "billing"}},
	}, req.Sent)
	assert.Empty(t, r.up.seen, "nothing is sent before the user decides")
	r.asker.answers <- true
	assert.Equal(t, http.StatusOK, testutil.Recv(t, done, "the answer").StatusCode)
	assert.Equal(t, http.MethodDelete, testutil.Recv(t, r.up.seen, "the change").method)

	go func() { done <- r.send(t, http.MethodDelete, target, r.grant.Token(), nil, nil) }()
	testutil.Recv(t, r.asker.asked, "the second request")
	r.asker.answers <- false
	refusal(t, testutil.Recv(t, done, "the denial"))
	assert.Empty(t, r.up.seen)
}

func TestAnObjectReadAsks(t *testing.T) {
	r := newRig(t, roomy)
	done := make(chan *http.Response, 1)
	go func() { done <- r.get(t, "http://storage.gcp.kubetail.invalid/storage/v1/b/bkt/o/notes.txt?alt=media") }()
	req := testutil.Recv(t, r.asker.asked, "the request")
	assert.Equal(t, permissions.DataRead, req.Action.AccessLevel)
	r.asker.answers <- true
	assert.Equal(t, http.StatusOK, testutil.Recv(t, done, "the answer").StatusCode)
}

func TestAnInstanceReadAsks(t *testing.T) {
	r := newRig(t, roomy)
	done := make(chan *http.Response, 1)
	go func() {
		done <- r.get(t, "http://compute.gcp.kubetail.invalid/compute/v1/projects/p/zones/z/instances/i/serialPort")
	}()
	req := testutil.Recv(t, r.asker.asked, "the request")
	assert.Equal(t, permissions.DataRead, req.Action.AccessLevel)
	r.asker.answers <- false
	refusal(t, testutil.Recv(t, done, "the denial"))
}

const upload = "http://storage.gcp.kubetail.invalid/upload/storage/v1/b/bkt/o?uploadType=media&name=notes.txt"

func TestABinaryUploadIsRefused(t *testing.T) {
	r := newRig(t, roomy)
	resp := r.send(t, http.MethodPost, upload, r.grant.Token(), strings.NewReader("\xff\xfe\x00binary"), http.Header{"Content-Type": {"application/octet-stream"}})
	refusal(t, resp)
	assert.Empty(t, r.asker.asked)
	assert.Empty(t, r.up.seen)
}

func TestATextUploadIsShownWhole(t *testing.T) {
	r := newRig(t, roomy)
	done := make(chan *http.Response, 1)
	go func() {
		done <- r.send(t, http.MethodPost, upload, r.grant.Token(), strings.NewReader("hello\n"), http.Header{"Content-Type": {"text/plain"}})
	}()
	req := testutil.Recv(t, r.asker.asked, "the request")
	assert.Equal(t, "hello\n", string(req.Sent.Body))
	assert.Equal(t, "text/plain", req.Sent.ContentType)
	assert.Equal(t, "/upload/storage/v1/b/bkt/o?uploadType=media&name=notes.txt", req.Sent.Path)
	r.asker.answers <- true
	assert.Equal(t, http.StatusOK, testutil.Recv(t, done, "the answer").StatusCode)
	assert.Equal(t, "hello\n", testutil.Recv(t, r.up.seen, "the upload").body)
}

func TestAChangeCarryingABlankedValueIsRefused(t *testing.T) {
	r := newRig(t, roomy)
	body := `{"fingerprint":"f","items":[{"key":"startup-script","value":"[redacted]"},{"key":"a","value":"b"}]}`
	resp := r.send(t, http.MethodPost, "http://compute.gcp.kubetail.invalid/compute/v1/projects/p/zones/z/instances/i/setMetadata",
		r.grant.Token(), strings.NewReader(body), http.Header{"Content-Type": {"application/json"}})
	assert.Equal(t, refusedWriteBack, refusal(t, resp))
	assert.Empty(t, r.asker.asked, "never asked")
	assert.Empty(t, r.up.seen)
}

func TestARefusalIsGooglesErrorShape(t *testing.T) {
	r := newRig(t, roomy)
	resp := r.send(t, http.MethodPost, "http://iam.gcp.kubetail.invalid/v1/projects/p/serviceAccounts/sa/keys", r.grant.Token(), strings.NewReader("{}"), nil)
	assert.Equal(t, refusedCredential, refusal(t, resp))
	assert.Equal(t, "application/json; charset=UTF-8", resp.Header.Get("Content-Type"))
}

func TestAnUpstream401DropsTheToken(t *testing.T) {
	r := newRig(t, roomy)
	r.up.answer = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"code":401}}`)
	}
	resp := r.get(t, clusters)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "the 401 passes through")
	assert.Equal(t, []string{usersToken}, r.src.dropped)
}

func TestEachSourceFailureIsA403NamingIt(t *testing.T) {
	r := newRig(t, roomy)
	for err, words := range map[error]string{
		ErrNoCLI: "gcloud is not installed", ErrNoAccount: "gcloud auth login", ErrTimeout: "did not answer in time",
		ErrUnreadable:    "could not be read",
		context.Canceled: "sign-in",
	} {
		r.src.err = err
		assert.Contains(t, refusal(t, r.get(t, clusters)), words)
		change := r.send(t, http.MethodDelete, clusters+"/dev", r.grant.Token(), nil, nil)
		assert.Contains(t, refusal(t, change), words, "a change borrows before it asks")
	}
	assert.Empty(t, r.asker.asked)
	assert.Empty(t, r.up.seen)
}

// A change borrows once for the account it draws and again to send.
func TestASourceThatFailsOnceApprovedSendsNothing(t *testing.T) {
	r := newRig(t, roomy)
	done := make(chan *http.Response, 1)
	go func() {
		done <- r.send(t, http.MethodDelete, "http://storage.gcp.kubetail.invalid/storage/v1/b/bkt/o/x", r.grant.Token(), nil, nil)
	}()
	testutil.Recv(t, r.asker.asked, "the request")
	r.src.err = ErrNoAccount
	r.asker.answers <- true
	assert.Equal(t, refusedNoAccount, refusal(t, testutil.Recv(t, done, "the refusal")))
	assert.Empty(t, r.up.seen)
}

func TestTheGrantsLimitsAnswer403(t *testing.T) {
	t.Run("a full queue", func(t *testing.T) {
		r := newRig(t, runproxy.Limits{QPS: 1000, Burst: 1000, Open: 16, Bodies: 8})
		// One request is with the user and the grant queues eight behind it,
		// so of ten the tenth is refused.
		const sent = 10
		done := make(chan *http.Response, sent)
		change := func() { done <- r.send(t, http.MethodDelete, clusters+"/dev", r.grant.Token(), nil, nil) }
		go change()
		testutil.Recv(t, r.asker.asked, "the first change")
		for range sent - 1 {
			go change()
		}
		assert.Contains(t, refusal(t, testutil.Recv(t, done, "the refusal")), "waiting on the user")
		for range sent - 1 {
			r.asker.answers <- false
			refusal(t, testutil.Recv(t, done, "a denial"))
		}
	})
	t.Run("no slot", func(t *testing.T) {
		r := newRig(t, runproxy.Limits{QPS: 1000, Burst: 1000, Open: 1, Bodies: 8})
		release := make(chan struct{})
		r.up.answer = func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-release
		}
		defer close(release)
		logs := "http://logging.gcp.kubetail.invalid/v2/projects/p/logs"
		open := r.get(t, logs)
		require.Equal(t, http.StatusOK, open.StatusCode)
		refusal(t, r.get(t, logs))
	})
	t.Run("no body slot", func(t *testing.T) {
		r := newRig(t, runproxy.Limits{QPS: 1000, Burst: 1000, Open: 8, Bodies: 1})
		target := "http://compute.gcp.kubetail.invalid/compute/v1/projects/p/zones/z/instances"
		done := make(chan *http.Response, 1)
		go func() {
			done <- r.send(t, http.MethodPost, target, r.grant.Token(), strings.NewReader(`{"name":"a"}`), nil)
		}()
		testutil.Recv(t, r.asker.asked, "the first change")
		refusal(t, r.send(t, http.MethodPost, target, r.grant.Token(), strings.NewReader(`{"name":"b"}`), nil))
		r.asker.answers <- false
		refusal(t, testutil.Recv(t, done, "the first change's denial"))
	})
}

// A read's body is buffered before it is sent, so its body slot is free
// while its answer streams.
func TestAReadGivesUpItsBodySlotBeforeItIsSent(t *testing.T) {
	r := newRig(t, runproxy.Limits{QPS: 1000, Burst: 1000, Open: 8, Bodies: 1})
	release := make(chan struct{})
	r.up.answer = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release
	}
	defer close(release)
	entries := "http://logging.gcp.kubetail.invalid/v2/entries:list"
	open := r.send(t, http.MethodPost, entries, r.grant.Token(), strings.NewReader(`{"resourceNames":["projects/p"]}`), nil)
	require.Equal(t, http.StatusOK, open.StatusCode)

	done := make(chan *http.Response, 1)
	go func() {
		done <- r.send(t, http.MethodPost, "http://compute.gcp.kubetail.invalid/compute/v1/projects/p/zones/z/instances",
			r.grant.Token(), strings.NewReader(`{"name":"a"}`), nil)
	}()
	testutil.Recv(t, r.asker.asked, "a change with a body, while the read streams")
	r.asker.answers <- false
	refusal(t, testutil.Recv(t, done, "the denial"))
}

// A read whose context ends while it waits on the grant's limiter answers
// the grant's 401.
func TestAReadWhoseWaitEndsIsUnauthorized(t *testing.T) {
	r := newRig(t, roomy)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, clusters, nil)
	w := httptest.NewRecorder()

	New(r.grant, r.src, r.up).read(w, req, serviceNamed("container"))

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Empty(t, r.up.seen)
}

func TestAnUpstreamFailureIsA403(t *testing.T) {
	r := newRig(t, roomy)
	r.up.answer = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "https://evil.example/")
		w.WriteHeader(http.StatusFound)
	}
	assert.Equal(t, refusedReach, refusal(t, r.get(t, clusters)))
	testutil.Recv(t, r.up.seen, "the redirected request")

	r.up.answer = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "<p>try later</p>")
	}
	resp := r.get(t, clusters)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode, "Google's own 503 passes through")
	assert.Equal(t, "<p>try later</p>", readAll(t, resp))
	testutil.Recv(t, r.up.seen, "the request")

	r.up.srv.Close()
	refusal(t, r.get(t, clusters))
}

func TestASummaryReadsPastTheVersion(t *testing.T) {
	assert.Equal(t, "Google Cloud: POST compute projects/p/zones/z/instances",
		summary("compute", http.MethodPost, []string{"compute", "beta", "projects", "p", "zones", "z", "instances"}))
	assert.Equal(t, "Google Cloud: DELETE storage b/bkt/o/dir/x",
		summary("storage", http.MethodDelete, []string{"storage", "v1", "b", "bkt", "o", "dir/x"}))
	assert.Equal(t, "Google Cloud: GET storage b/bkt",
		summary("storage", http.MethodGet, []string{"b", "bkt"}), "a path naming no version")
}

// A change runs as the account the user approved: a token refreshed for it
// is sent, and another account's is refused unsent.
func TestAnApprovedChangeRunsAsTheApprovedAccount(t *testing.T) {
	r := newRig(t, roomy)
	target := "http://container.gcp.kubetail.invalid/v1/projects/p/locations/l/clusters/dev"
	done := make(chan *http.Response, 1)

	go func() { done <- r.send(t, http.MethodDelete, target, r.grant.Token(), nil, nil) }()
	testutil.Recv(t, r.asker.asked, "the request")
	r.src.lend(Token{Value: "refreshed-token", Account: "dev@example.com"})
	r.asker.answers <- true
	assert.Equal(t, http.StatusOK, testutil.Recv(t, done, "the answer").StatusCode)
	assert.Equal(t, "Bearer refreshed-token", testutil.Recv(t, r.up.seen, "the change").header.Get("Authorization"))

	go func() { done <- r.send(t, http.MethodDelete, target, r.grant.Token(), nil, nil) }()
	testutil.Recv(t, r.asker.asked, "the second request")
	r.src.lend(Token{Value: "other-token", Account: "someone-else@example.com"})
	r.asker.answers <- true
	assert.Equal(t, refusedAccountChanged, refusal(t, testutil.Recv(t, done, "the refusal")))
	assert.Empty(t, r.up.seen, "nothing is sent as another account")
}

const (
	myProject   = "http://compute.gcp.kubetail.invalid/compute/v1/projects/my-project"
	startVM     = myProject + "/zones/us-central1-a/instances/vm/start"
	serialPort  = myProject + "/zones/us-central1-a/instances/vm/serialPort"
	jsonHeaders = "application/json"
)

// post sends a POST of body to target with the grant's token.
func (r *rig) post(t *testing.T, target, body string) *http.Response {
	t.Helper()
	return r.send(t, http.MethodPost, target, r.grant.Token(), strings.NewReader(body), http.Header{"Content-Type": {jsonHeaders}})
}

// gcpRule is a rule of effect on Google Cloud's writes in my-project.
func gcpRule(effect permissions.Effect, level permissions.AccessLevel) permissions.Rule {
	return permissions.Rule{ID: string(effect), Effect: effect, AccessLevel: level, Principal: "*", Target: "gcp",
		Scope: "my-project/**", Service: "*", Operation: "*", Resource: "*"}
}

func TestARuleAndAModeDecideAGCPAction(t *testing.T) {
	t.Run("auto forwards a write unasked and records it", func(t *testing.T) {
		r := newRig(t, roomy)
		r.setPolicy(permissions.Policy{Mode: permissions.ModeAuto})
		assert.Equal(t, http.StatusOK, r.post(t, startVM, "").StatusCode)
		rec := testutil.Recv(t, r.asker.records, "the record")
		assert.Equal(t, permissions.Allowed, rec.d)
		assert.Equal(t, "auto mode", rec.reason)
		testutil.Recv(t, r.up.seen, "the write")
		assert.Empty(t, r.asker.asked)
		assert.Equal(t, []string{"gcp my-project"}, r.accounts)
	})
	t.Run("a write billed to another project asks under auto", func(t *testing.T) {
		r := newRig(t, roomy)
		r.setPolicy(permissions.Policy{Mode: permissions.ModeAuto})
		done := make(chan *http.Response, 1)
		go func() {
			done <- r.send(t, http.MethodPost, startVM, r.grant.Token(), strings.NewReader(""),
				http.Header{"Content-Type": {jsonHeaders}, "X-Goog-User-Project": {"other-project"}})
		}()
		req := testutil.Recv(t, r.asker.asked, "the request")
		assert.Equal(t, "my-project/us-central1-a", req.Action.Scope)
		assert.True(t, req.Action.Once)
		r.asker.answers <- false
		testutil.Recv(t, done, "the answer")
	})
	t.Run("a deny rule for the project refuses a write naming another project", func(t *testing.T) {
		r := newRig(t, roomy)
		r.setPolicy(permissions.Policy{Mode: permissions.ModeAuto, Rules: []permissions.Rule{gcpRule(permissions.Deny, permissions.Write)}})
		w := r.post(t, myProject+"/zones/us-central1-a/instances/vm/setLabels", `{"labels":{"a":"projects/other-project"}}`)
		assert.Contains(t, refusal(t, w), "a rule denies it")
		assert.Empty(t, r.asker.asked)
		assert.Empty(t, r.up.seen)
	})
	t.Run("read-only denies", func(t *testing.T) {
		r := newRig(t, roomy)
		r.setPolicy(permissions.Policy{Mode: permissions.ModeReadOnly})
		assert.Contains(t, refusal(t, r.post(t, startVM, "")), "this account is read-only")
		assert.Equal(t, permissions.Denied, testutil.Recv(t, r.asker.records, "the record").d)
		assert.Empty(t, r.up.seen)
	})
	t.Run("an allow rule forwards", func(t *testing.T) {
		r := newRig(t, roomy)
		r.setPolicy(permissions.Policy{Mode: permissions.ModeAsk, Rules: []permissions.Rule{gcpRule(permissions.Allow, permissions.Write)}})
		assert.Equal(t, http.StatusOK, r.post(t, startVM, "").StatusCode)
		assert.Equal(t, "a rule allows it: Allow writes anywhere in Google Cloud my-project", testutil.Recv(t, r.asker.records, "the record").reason)
		testutil.Recv(t, r.up.seen, "the write")
	})
	t.Run("a deny rule refuses", func(t *testing.T) {
		r := newRig(t, roomy)
		r.setPolicy(permissions.Policy{Mode: permissions.ModeAuto, Rules: []permissions.Rule{gcpRule(permissions.Deny, permissions.Write)}})
		assert.Contains(t, refusal(t, r.post(t, startVM, "")), "a rule denies it")
		assert.Empty(t, r.up.seen)
	})
	t.Run("a command answer allows the next", func(t *testing.T) {
		r := newRig(t, roomy)
		r.asker.duration = permissions.DurationCommand
		done := make(chan *http.Response, 1)
		go func() { done <- r.post(t, startVM, "") }()
		req := testutil.Recv(t, r.asker.asked, "the request")
		assert.True(t, req.Grantable)
		assert.Equal(t, "Allow writes in Google Cloud my-project / us-central1-a", req.ChatRule)
		r.asker.answers <- true
		assert.Equal(t, http.StatusOK, testutil.Recv(t, done, "the answer").StatusCode)
		testutil.Recv(t, r.up.seen, "the first write")
		assert.Equal(t, http.StatusOK, r.post(t, startVM, "").StatusCode, "the next runs unasked")
		assert.Equal(t, permissions.Allowed, testutil.Recv(t, r.asker.records, "the record").d)
		assert.Empty(t, r.asker.asked)
	})
	t.Run("an in-project data read forwards unasked under auto", func(t *testing.T) {
		r := newRig(t, roomy)
		r.setPolicy(permissions.Policy{Mode: permissions.ModeAuto})
		assert.Equal(t, http.StatusOK, r.get(t, serialPort).StatusCode)
		rec := testutil.Recv(t, r.asker.records, "the record")
		assert.Equal(t, permissions.Allowed, rec.d)
		assert.Equal(t, permissions.DataRead, rec.req.Action.AccessLevel)
		assert.Empty(t, r.asker.asked)
	})
	t.Run("a data read in no project asks once", func(t *testing.T) {
		r := newRig(t, roomy)
		r.setPolicy(permissions.Policy{Mode: permissions.ModeAuto})
		done := make(chan *http.Response, 1)
		go func() { done <- r.get(t, "http://storage.gcp.kubetail.invalid/storage/v1/b/bkt/o/notes.txt?alt=media") }()
		req := testutil.Recv(t, r.asker.asked, "the request")
		assert.False(t, req.Grantable)
		r.asker.answers <- false
		refusal(t, testutil.Recv(t, done, "the denial"))
	})
	t.Run("a write naming another project asks once", func(t *testing.T) {
		r := newRig(t, roomy)
		r.setPolicy(permissions.Policy{Mode: permissions.ModeAuto})
		done := make(chan *http.Response, 1)
		go func() {
			done <- r.post(t, myProject+"/zones/us-central1-a/instances/vm/setLabels", `{"labels":{"a":"projects/other-project"}}`)
		}()
		req := testutil.Recv(t, r.asker.asked, "the request")
		assert.Equal(t, "my-project/us-central1-a", req.Action.Scope)
		assert.True(t, req.Action.Once)
		assert.False(t, req.Grantable)
		r.asker.answers <- false
		refusal(t, testutil.Recv(t, done, "the denial"))
	})
}

// The action decided and recorded holds every field the classifier filled.
func TestNoProxyDropsAFieldBeforeTheDecision(t *testing.T) {
	r := newRig(t, roomy)
	r.setPolicy(permissions.Policy{Mode: permissions.ModeAuto})
	require.Equal(t, http.StatusOK, r.post(t, startVM, "").StatusCode)
	got := testutil.Recv(t, r.asker.records, "the record").req.Action
	assert.Equal(t, permissions.Action{
		Principal: permissions.Chat, Target: permissions.GCP, Scope: "my-project/us-central1-a", AccountName: "my-project",
		Service: "compute", Operation: "start", Resource: "instances", Name: "vm", AccessLevel: permissions.Write,
		Summary: "Google Cloud: POST compute projects/my-project/zones/us-central1-a/instances/vm/start",
	}, got)
}
