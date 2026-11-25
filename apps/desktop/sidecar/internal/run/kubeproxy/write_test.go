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
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
)

// asked is one request put to a fakeAsker, its write, and where its answer
// goes.
type asked struct {
	r      Request
	w      Write
	answer chan<- permissions.Answer
	ctx    context.Context
}

// The two answers most tests give.
var (
	once   = permissions.Answer{Approved: true, Duration: permissions.DurationOnce}
	denied = permissions.Answer{}
)

// fakeAsker hands each write it is asked to the test, and waits for the
// test's answer or the write's context.
type fakeAsker chan asked

func (a fakeAsker) Ask(ctx context.Context, r Request) (permissions.Answer, error) {
	answer := make(chan permissions.Answer, 1)
	select {
	case a <- asked{r: r, w: *r.Write, answer: answer, ctx: ctx}:
	case <-ctx.Done():
		return permissions.Answer{}, ctx.Err()
	}
	select {
	case ok := <-answer:
		return ok, nil
	case <-ctx.Done():
		return permissions.Answer{}, ctx.Err()
	}
}

// Record records nothing: a fakeAsker's tests read what is asked.
func (a fakeAsker) Record(context.Context, Request, permissions.Decision, string) error {
	return nil
}

// recorded is one request a recordingAsker was told was decided, and its write.
type recorded struct {
	r   Request
	w   Write
	d   permissions.Decision
	why string
}

// recordingAsker asks as its fakeAsker does, and hands each record to the
// test, failing it with err when set.
type recordingAsker struct {
	fakeAsker
	records chan recorded
	err     error
}

func newRecordingAsker() *recordingAsker {
	return &recordingAsker{fakeAsker: make(fakeAsker, 1), records: make(chan recorded, 16)}
}

func (a *recordingAsker) Record(_ context.Context, r Request, d permissions.Decision, why string) error {
	a.records <- recorded{r: r, w: *r.Write, d: d, why: why}
	return a.err
}

// sessionIn is a chat's session whose mode is mode in every account, under
// rules.
func sessionIn(mode permissions.Mode, rules ...permissions.Rule) session.Session {
	return session.Session{
		Kind: session.Chat,
		Policy: func(context.Context, permissions.Target, string) permissions.Policy {
			return permissions.Policy{Mode: mode, Rules: rules}
		},
	}
}

// rule is a Kubernetes rule of effect and level in scope, naming everything
// else by "*".
func rule(effect permissions.Effect, level permissions.AccessLevel, scope string) permissions.Rule {
	return permissions.Rule{ID: string(effect), Effect: effect, AccessLevel: level, Principal: "*", Target: "kubernetes",
		Scope: scope, Service: "*", Operation: "*", Resource: "*"}
}

// askSession asks for every write, as a fresh Settings does.
var askSession = sessionIn(permissions.ModeAsk)

// serveIn is a grant in context dev under sess that puts each write to asker.
func serveIn(t *testing.T, up Upstream, sess session.Session, asker Asker) *served {
	t.Helper()
	return serveGrant(t, NewGrant(up, sess, "c1", "dev", asker, noAsker, 1000, 1000, 32))
}

// next is the next write put to a, once it is asked.
func (a fakeAsker) next(t *testing.T) asked {
	t.Helper()
	return testutil.Recv(t, (<-chan asked)(a), "a write to be asked")
}

// serveAsking is a grant over up that puts each write to asker.
func serveAsking(t *testing.T, up Upstream, asker Asker) *served {
	t.Helper()
	return serveGrant(t, NewGrant(up, askSession, "c1", "dev", asker, noAsker, 1000, 1000, 32))
}

// writeRequest is method on path with body sent as contentType, as a command
// in the sandbox sends it.
func (s *served) writeRequest(t *testing.T, method, path, contentType, body string) *http.Request {
	t.Helper()
	req := s.request(t, method, path, s.g.Token())
	if body != "" {
		req.Body = io.NopCloser(strings.NewReader(body))
		req.ContentLength = int64(len(body))
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req
}

// reply is a response and its body, read.
type reply struct {
	resp *http.Response
	body string
}

// sendAsync sends req on its own goroutine and hands back the reply.
func (s *served) sendAsync(t *testing.T, req *http.Request) <-chan reply {
	t.Helper()
	out := make(chan reply, 1)
	go func() {
		resp, err := s.client.Do(req)
		if err != nil {
			out <- reply{}
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		out <- reply{resp: resp, body: string(body)}
	}()
	return out
}

const patchBody = `{"spec":{"replicas":3}}`

// A grant with no one to ask refuses every write with its own message, and a
// burst of them past the queue's bound reads that message, never the 429.
func TestAGrantWithNoAskerRefusesWrites(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	s := serve(t, api.upstream())

	var wg sync.WaitGroup
	for range maxQueuedWrites + 4 {
		wg.Go(func() {
			resp, body := s.send(t, "DELETE", "/api/v1/namespaces/web/pods/x", s.g.Token())
			assertForbidden(t, resp, body, noAsker)
		})
	}
	wg.Wait()
	assert.Empty(t, api.requests())
}

// A write reaches the cluster only once the asker approves it, and then as the
// bytes shown, with its length set.
func TestAWriteWaitsOnTheAsker(t *testing.T) {
	var got []byte
	var length int64
	api := newAPIServer(t, func(_ http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		length = r.ContentLength
	})
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	done := s.sendAsync(t, s.writeRequest(t, "PATCH", "/apis/apps/v1/namespaces/web/deployments/x", "application/merge-patch+json", patchBody))
	a := asker.next(t)

	assert.Equal(t, "PATCH", a.w.Method)
	assert.Equal(t, patchBody, string(a.w.Body))
	assert.Empty(t, changes(api.requests()), "nothing changes the cluster while the user decides")
	a.answer <- once
	r := testutil.Recv(t, done, "the write to be answered")
	require.NotNil(t, r.resp)
	assert.Equal(t, http.StatusOK, r.resp.StatusCode)
	assert.Equal(t, patchBody, string(got))
	assert.Equal(t, int64(len(patchBody)), length)
}

// changes are the requests that change the cluster: every one but a read and
// a dry run, which is how a preview reaches it.
func changes(reqs []*http.Request) []*http.Request {
	var out []*http.Request
	for _, r := range reqs {
		if r.Method != http.MethodGet && r.URL.Query().Get("dryRun") != "All" {
			out = append(out, r)
		}
	}
	return out
}

// A denied write answers 403, and the cluster never sees it.
func TestADeniedWriteIsForbidden(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	done := s.sendAsync(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/x", "", ""))
	asker.next(t).answer <- denied
	r := testutil.Recv(t, done, "the write to be answered")

	require.NotNil(t, r.resp)
	assertForbidden(t, r.resp, r.body, refusedDenied)
	assert.Empty(t, api.requests())
}

// errAsker fails every write it is asked with err.
type errAsker struct{ err error }

func (a errAsker) Ask(context.Context, Request) (permissions.Answer, error) {
	return permissions.Answer{}, a.err
}

func (a errAsker) Record(context.Context, Request, permissions.Decision, string) error {
	return a.err
}

// A write the asker could not record answers 403 saying so, not that the user
// did not answer, and the cluster never sees it.
func TestAWriteThatCannotBeRecordedIsForbidden(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	s := serveAsking(t, api.upstream(), errAsker{errors.New("store refused")})

	r := testutil.Recv(t, s.sendAsync(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/x", "", "")), "the write to be answered")

	require.NotNil(t, r.resp)
	assertForbidden(t, r.resp, r.body, refusedUnrecorded)
	assert.Empty(t, api.requests())
}

// A self review changes nothing, so it reaches the cluster with no one asked.
func TestASelfReviewPassesUnasked(t *testing.T) {
	api := newAPIServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) })
	asker := make(fakeAsker)
	s := serveAsking(t, api.upstream(), asker)

	resp, _ := s.do(t, s.writeRequest(t, "POST", "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", "application/json", `{}`))

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Len(t, api.requests(), 1)
}

// quietWindow bounds the negative assertions below: a write that should not be
// asked yet has no event to wait for.
const quietWindow = 50 * time.Millisecond

// serveCountingWaiters is serveAsking with the writes waiting for the lock
// counted.
func serveCountingWaiters(t *testing.T, up Upstream, asker Asker) (*served, *atomic.Int32) {
	t.Helper()
	return serveCountingWaitersIn(t, up, askSession, asker)
}

// serveCountingWaitersIn is serveCountingWaiters under sess.
func serveCountingWaitersIn(t *testing.T, up Upstream, sess session.Session, asker Asker) (*served, *atomic.Int32) {
	t.Helper()
	var waiting atomic.Int32
	g := NewGrant(up, sess, "c1", "dev", asker, noAsker, 1000, 1000, 32)
	g.waitersMoved = func(delta int) { waiting.Add(int32(delta)) }
	return serveGrant(t, g), &waiting
}

// A second write is asked only once the first has been forwarded, and a write
// waiting for its turn is dropped when its client gives up.
func TestWritesAskOneAtATime(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	asker := make(fakeAsker, 1)
	s, waiting := serveCountingWaiters(t, api.upstream(), asker)

	first := s.sendAsync(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/a", "", ""))
	a := asker.next(t)
	ctx, giveUp := context.WithCancel(t.Context())
	gone := s.sendAsync(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/b", "", "").WithContext(ctx))
	second := s.sendAsync(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/c", "", ""))
	require.Eventually(t, func() bool { return waiting.Load() == 2 }, testutil.Timeout, time.Millisecond, "both to queue")
	giveUp()
	testutil.Recv(t, gone, "the write given up on to end")
	require.Eventually(t, func() bool { return waiting.Load() == 1 }, testutil.Timeout, time.Millisecond, "its place to free")

	testutil.NoRecv(t, (<-chan asked)(asker), quietWindow, "a second write asked while the first waits")
	a.answer <- once
	testutil.Recv(t, first, "the first write to be answered")
	b := asker.next(t)
	assert.Equal(t, "/api/v1/namespaces/web/pods/c", b.w.Path)
	b.answer <- once
	testutil.Recv(t, second, "the second write to be answered")
	require.Len(t, api.requests(), 2)
	assert.Equal(t, "/api/v1/namespaces/web/pods/a", api.requests()[0].URL.Path)
}

// signalReader fires read when it is read.
type signalReader struct {
	io.Reader
	read *testutil.Signal
}

func (r signalReader) Read(p []byte) (int, error) {
	r.read.Fire()
	return r.Reader.Read(p)
}

// With one write held and the queue full, one more answers 429 with no
// Retry-After; a queued write's body is not read until it holds the lock.
func TestQueuedWritesAreBounded(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	asker := make(fakeAsker, 1)
	s, waiting := serveCountingWaiters(t, api.upstream(), asker)

	s.sendAsync(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/a", "", ""))
	held := asker.next(t)
	body := signalReader{Reader: strings.NewReader(patchBody), read: testutil.NewSignal()}
	req := httptest.NewRequest("PATCH", "http://"+Host+"/api/v1/namespaces/web/configmaps/x", body)
	req.Header.Set("Proxy-Authorization", proxyAuth(ProxyUser, s.g.Token()))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	go s.g.ServeHTTP(httptest.NewRecorder(), req)
	for range maxQueuedWrites - 1 {
		s.sendAsync(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/b", "", ""))
	}
	require.Eventually(t, func() bool { return waiting.Load() == maxQueuedWrites }, testutil.Timeout, time.Millisecond, "the queue to fill")

	resp, reply := s.do(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/c", "", ""))
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Empty(t, resp.Header.Values("Retry-After"))
	assert.Equal(t, "kubetail: too many requests are waiting on the user. Send one at a time.", statusOf(t, reply).Message)
	testutil.NoRecv(t, body.read.Chan(), quietWindow, "a queued write's body read")

	// The queued writes race each other into the lock's line, so any that took
	// the lock ahead of the PATCH are asked first: deny them until it holds it.
	held.answer <- once
	for {
		select {
		case <-body.read.Chan():
			return
		case a := <-asker:
			a.answer <- denied
		case <-time.After(testutil.Timeout):
			t.Fatal("timed out waiting for the queued write's body to be read once it holds the lock")
		}
	}
}

// A write waiting on the user holds no slot: with a cap of one, a read passes
// while it waits.
func TestAWriteWaitingHoldsNoSlot(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	asker := make(fakeAsker, 1)
	s := serveGrant(t, NewGrant(api.upstream(), askSession, "c1", "dev", asker, noAsker, 1000, 1000, 1))

	s.sendAsync(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/a", "", ""))
	asker.next(t)

	resp, _ := s.send(t, "GET", "/api/v1/pods", s.g.Token())
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// writeCase is one write a test sends: its method, path, headers and body.
type writeCase struct {
	method, path, contentType, body string
	header                          http.Header
}

// sendWrite sends c, answering any ask with a denial, and answers the reply
// and the write it was asked as, nil when it was not asked.
func (s *served) sendWrite(t *testing.T, asker fakeAsker, c writeCase) (reply, *Write) {
	t.Helper()
	req := s.writeRequest(t, c.method, c.path, c.contentType, c.body)
	maps.Copy(req.Header, c.header)
	done := s.sendAsync(t, req)
	select {
	case a := <-asker:
		a.answer <- denied
		return testutil.Recv(t, done, "the write to be answered"), &a.w
	case r := <-done:
		return r, nil
	}
}

// askedFor sends c and answers the write it was asked as, which it must be.
func (s *served) askedFor(t *testing.T, asker fakeAsker, c writeCase) Write {
	t.Helper()
	_, w := s.sendWrite(t, asker, c)
	require.NotNil(t, w, "the write to be asked")
	return *w
}

// A body the user could not read as it will be sent is refused unasked: one
// that is not JSON or YAML, one encoded, one not UTF-8, one past 1 MiB, one
// repeating a key in an object. A media type's parameters are not its type.
func TestAWriteThatCannotBeShownIsRefused(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)
	const path = "/api/v1/namespaces/web/configmaps"

	for name, c := range map[string]struct {
		c   writeCase
		why refusal
	}{
		"protobuf":     {writeCase{method: "POST", path: path, contentType: "application/vnd.kubernetes.protobuf", body: "k8s\x00"}, refusedUnshowable},
		"no type":      {writeCase{method: "POST", path: path, body: `{}`}, refusedUnshowable},
		"encoded":      {writeCase{method: "POST", path: path, contentType: "application/json", body: `{}`, header: http.Header{"Content-Encoding": {"gzip"}}}, refusedUnshowable},
		"not UTF-8":    {writeCase{method: "POST", path: path, contentType: "application/json", body: "{\"a\":\"\xff\"}"}, refusedUnshowable},
		"past 1 MiB":   {writeCase{method: "POST", path: path, contentType: "application/json", body: `{"a":"` + strings.Repeat("x", maxWriteBody) + `"}`}, refusedTooLarge},
		"with a body":  {writeCase{method: "DELETE", path: path + "/x", body: `{}`}, refusedUnshowable},
		"a pdf delete": {writeCase{method: "DELETE", path: path + "/x", contentType: "application/pdf", body: `{}`}, refusedUnshowable},
		"a namespace create repeating metadata": {writeCase{method: "POST", path: "/api/v1/namespaces", contentType: "application/json",
			body: `{"metadata":{"name":"blocked"},"metadata":{"labels":{"x":"y"}}}`}, refusedRepeatedKey},
		"a put repeating spec": {writeCase{method: "PUT", path: path + "/x", contentType: "application/json",
			body: `{"spec":{"replicas":0},"spec":{"template":{}}}`}, refusedRepeatedKey},
		"a patch repeating a nested key": {writeCase{method: "PATCH", path: path + "/x", contentType: "application/merge-patch+json",
			body: `{"spec":{"replicas":0,"replicas":3}}`}, refusedRepeatedKey},
		"an apply repeating spec": {writeCase{method: "PATCH", path: path + "/x", contentType: "application/apply-patch+yaml",
			body: "spec:\n  replicas: 0\nspec:\n  template: {}\n"}, refusedRepeatedKey},
		"a json patch repeating path": {writeCase{method: "PATCH", path: path + "/x", contentType: "application/json-patch+json",
			body: `[{"op":"replace","path":"/spec/replicas","path":"/metadata/labels/a","value":0}]`}, refusedRepeatedKey},
	} {
		r, write := s.sendWrite(t, asker, c.c)
		t.Run(name, func(t *testing.T) {
			assert.Nil(t, write)
			require.NotNil(t, r.resp)
			assertForbidden(t, r.resp, r.body, c.why)
		})
	}
	assert.Empty(t, api.requests())

	for _, contentType := range []string{
		"application/json; charset=utf-8", "application/merge-patch+json", "application/strategic-merge-patch+json",
		"application/json-patch+json", "application/apply-patch+yaml",
	} {
		_, write := s.sendWrite(t, asker, writeCase{method: "PATCH", path: path + "/x", contentType: contentType, body: `{}`})
		assert.NotNil(t, write, contentType)
	}
	_, write := s.sendWrite(t, asker, writeCase{method: "PUT", path: path + "/x", contentType: "application/json",
		body: `{"metadata":{"name":"x"},"data":{"metadata":"y","name":"z"}}`})
	assert.NotNil(t, write, "a key repeated across objects is not repeated")
}

// A DELETE may carry no body, and asks.
func TestADeleteWithNoBodyAsks(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	_, write := s.sendWrite(t, asker, writeCase{method: "DELETE", path: "/api/v1/namespaces/web/pods/x"})

	assert.NotNil(t, write)
}

// A write carrying the sandbox's own redaction, as text or as its base64, is
// refused unasked: it would put the redacted value back in place of the real
// one. The check reads the values as the API server decodes them, so an escape
// that spells the mark differently does not hide it.
func TestAWriteCarryingRedactedIsRefused(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)
	const secret = "/api/v1/namespaces/web/secrets/x"

	for name, c := range map[string]writeCase{
		"as text":           {contentType: "application/merge-patch+json", body: `{"stringData":{"a":"[redacted]"}}`},
		"as base64":         {contentType: "application/merge-patch+json", body: `{"data":{"a":"W3JlZGFjdGVkXQ=="}}`},
		"escaped in JSON":   {contentType: "application/merge-patch+json", body: `{"stringData":{"a":"\u005bredacted\u005d"}}`},
		"base64, escaped":   {contentType: "application/json-patch+json", body: `[{"op":"replace","path":"/data/a","value":"W3JlZGFjdGVkXQ\u003d\u003d"}]`},
		"escaped in YAML":   {contentType: "application/apply-patch+yaml", body: "stringData:\n  a: \"\\x5bredacted\\x5d\"\n"},
		"in a YAML unicode": {contentType: "application/apply-patch+yaml", body: "stringData:\n  a: \"\\u005bredacted]\"\n"},
		"escaped in a key":  {contentType: "application/merge-patch+json", body: `{"stringData":{"\u005bredacted]":"x"}}`},
	} {
		c.method, c.path = "PATCH", secret
		r, write := s.sendWrite(t, asker, c)
		t.Run(name, func(t *testing.T) {
			assert.Nil(t, write)
			assertForbidden(t, r.resp, r.body, refusedRedacted)
		})
	}
	assert.Empty(t, api.requests())
}

// A body its media type cannot decode is refused unasked, since what it would
// do cannot be read off it.
func TestAWriteThatDoesNotDecodeIsRefused(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	for _, c := range []writeCase{
		{contentType: "application/merge-patch+json", body: `{"a":`},
		{contentType: "application/merge-patch+json", body: `{"a":1} {"b":2}`},
		{contentType: "application/apply-patch+yaml", body: "a: [\n"},
	} {
		c.method, c.path = "PATCH", "/api/v1/namespaces/web/configmaps/x"
		r, write := s.sendWrite(t, asker, c)
		assert.Nil(t, write, c.body)
		assertForbidden(t, r.resp, r.body, refusedUnshowable)
	}
	assert.Empty(t, api.requests())
}

// readSecrets lists path's Secrets through s and gives the read answer.
func (s *served) readSecrets(t *testing.T, asker fakeAsker, path string, answer permissions.Answer) {
	t.Helper()
	done := s.sendAsync(t, s.request(t, "GET", path, s.g.Token()))
	asker.next(t).answer <- answer
	testutil.Recv(t, done, "the read of "+path)
}

// A helm release write follows what its command read: after a read of the
// namespace's Secret data the user approved, each write of a release goes on
// to its own decision; after one that passed redacted, in the namespace or
// across the cluster, each is refused unasked. A read elsewhere, or none at
// all, leaves it to its own decision. A DELETE of the collection names no
// release.
func TestAHelmReleaseWriteFollowsWhatTheCommandRead(t *testing.T) {
	api := answering(t, `{"kind":"SecretList","apiVersion":"v1","items":[]}`)
	asker := make(fakeAsker, 1)
	const secrets = "/api/v1/namespaces/web/secrets"
	const release = secrets + "/sh.helm.release.v1.x.v1"
	writes := map[string]writeCase{
		"a put":                     {method: "PUT", path: release, contentType: "application/json", body: `{"kind":"Secret","type":"helm.sh/release.v1"}`},
		"a patch":                   {method: "PATCH", path: release, contentType: "application/merge-patch+json", body: `{"metadata":{"labels":{"status":"superseded"}}}`},
		"a delete":                  {method: "DELETE", path: release},
		"a post typed as a release": {method: "POST", path: secrets, contentType: "application/json", body: `{"kind":"Secret","type":"helm.sh/release.v1"}`},
		"a post typed twice":        {method: "POST", path: secrets, contentType: "application/json", body: `{"type":"helm.sh/release.v1","Type":"Opaque"}`},
		"a post of no object":       {method: "POST", path: secrets, contentType: "application/json", body: `[]`},
		"a post typed by a number":  {method: "POST", path: secrets, contentType: "application/json", body: `{"type":5}`},
	}

	s := serveAsking(t, api.upstream(), asker)
	s.readSecrets(t, asker, secrets, once)
	for name, c := range writes {
		_, write := s.sendWrite(t, asker, c)
		assert.NotNil(t, write, "after an approved read, %s", name)
	}

	for _, read := range []string{secrets, "/api/v1/secrets"} {
		s = serveAsking(t, api.upstream(), asker)
		s.readSecrets(t, asker, read, denied)
		for name, c := range writes {
			r, write := s.sendWrite(t, asker, c)
			assert.Nil(t, write, "after %s read redacted, %s", read, name)
			assertForbidden(t, r.resp, r.body, refusedHelm)
		}
		// helm reuses the stored values, so its release carries the marks it
		// read; the gate's refusal names the fix, the mark check's does not.
		r, write := s.sendWrite(t, asker, writeCase{method: "PUT", path: release, contentType: "application/json",
			body: releaseBody(t, `{"name":"x","config":{"password":"[redacted]"}}`)})
		assert.Nil(t, write, "a release carrying the mark")
		assertForbidden(t, r.resp, r.body, refusedHelm)
		_, write = s.sendWrite(t, asker, writeCase{method: "DELETE", path: secrets})
		assert.NotNil(t, write, "a delete of the collection")
		_, write = s.sendWrite(t, asker, writeCase{method: "POST", path: secrets, contentType: "application/json", body: `{"kind":"Secret","type":"Opaque"}`})
		assert.NotNil(t, write, "a post of another type")
	}

	s = serveAsking(t, api.upstream(), asker)
	s.readSecrets(t, asker, "/api/v1/namespaces/api/secrets", denied)
	for name, c := range writes {
		_, write := s.sendWrite(t, asker, c)
		assert.NotNil(t, write, "after a read elsewhere, %s", name)
	}

	s = serveIn(t, api.upstream(), sessionIn(permissions.ModeAuto), asker)
	for name, c := range writes {
		resp, body := s.do(t, s.writeRequest(t, c.method, c.path, c.contentType, c.body))
		assert.Equal(t, http.StatusOK, resp.StatusCode, "with no read before, %s: %s", name, body)
	}
}

// releaseBody is a release Secret's body carrying release, as helm writes it.
func releaseBody(t *testing.T, release string) string {
	t.Helper()
	return `{"kind":"Secret","type":"helm.sh/release.v1","data":{"release":` + marshaled(releaseValue(t, release, true)) + `}}`
}

// A release is read inside: after an approved read, one whose config or
// manifest Secret holds [redacted] is refused; one that does not decode, one
// under stringData, and a JSON patch are refused as unshowable; a release with
// no mark, a delete and a patch of its labels go on to their own decision.
func TestARedactedReleaseIsRefusedUnderTheGrant(t *testing.T) {
	api := answering(t, `{"kind":"SecretList","apiVersion":"v1","items":[]}`)
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)
	const secrets = "/api/v1/namespaces/web/secrets"
	const release = secrets + "/sh.helm.release.v1.x.v1"
	s.readSecrets(t, asker, secrets, once)

	inConfig := releaseBody(t, `{"name":"x","config":{"password":"[redacted]"}}`)
	inManifest := releaseBody(t, `{"name":"x","manifest":"kind: Secret\ndata:\n  password: W3JlZGFjdGVkXQ==\n"}`)
	for name, c := range map[string]struct {
		write writeCase
		why   refusal
	}{
		"a put, config":        {writeCase{method: "PUT", path: release, contentType: "application/json", body: inConfig}, refusedRedacted},
		"a post, config":       {writeCase{method: "POST", path: secrets, contentType: "application/json", body: inConfig}, refusedRedacted},
		"a put, manifest":      {writeCase{method: "PUT", path: release, contentType: "application/json", body: inManifest}, refusedRedacted},
		"a post, manifest":     {writeCase{method: "POST", path: secrets, contentType: "application/json", body: inManifest}, refusedRedacted},
		"one not decoding":     {writeCase{method: "PUT", path: release, contentType: "application/json", body: `{"type":"helm.sh/release.v1","data":{"release":"bm90IGEgcmVsZWFzZQ=="}}`}, refusedUnshowable},
		"one under stringData": {writeCase{method: "PUT", path: release, contentType: "application/json", body: `{"type":"helm.sh/release.v1","stringData":{"release":"x"}}`}, refusedUnshowable},
		"a json patch":         {writeCase{method: "PATCH", path: release, contentType: "application/json-patch+json", body: `[{"op":"replace","path":"/data/release","value":"x"}]`}, refusedUnshowable},
	} {
		r, write := s.sendWrite(t, asker, c.write)
		assert.Nil(t, write, name)
		assertForbidden(t, r.resp, r.body, c.why)
	}

	for name, c := range map[string]writeCase{
		"a release with no mark": {method: "PUT", path: release, contentType: "application/json", body: releaseBody(t, `{"name":"x","config":{"password":"hunter2"}}`)},
		"a delete":               {method: "DELETE", path: release},
		"a patch of its labels":  {method: "PATCH", path: release, contentType: "application/merge-patch+json", body: `{"metadata":{"labels":{"status":"superseded"}}}`},
	} {
		_, write := s.sendWrite(t, asker, c)
		assert.NotNil(t, write, name)
	}
}

// A body carrying the mark is refused whatever the command read.
func TestAWriteCarryingRedactedIsStillRefused(t *testing.T) {
	api := answering(t, `{"kind":"SecretList","apiVersion":"v1","items":[]}`)
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)
	s.readSecrets(t, asker, "/api/v1/namespaces/web/secrets", once)

	r, write := s.sendWrite(t, asker, writeCase{method: "PATCH", path: "/api/v1/namespaces/web/secrets/x",
		contentType: "application/merge-patch+json", body: `{"data":{"a":"W3JlZGFjdGVkXQ=="}}`})
	assert.Nil(t, write)
	assertForbidden(t, r.resp, r.body, refusedRedacted)
}

// A write carries its path and query as sent, its media type as sent, and its
// subresource as the policy parsed the path.
func TestAWriteNamesItsSubresource(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	evict := s.askedFor(t, asker, writeCase{method: "POST", path: "/api/v1/namespaces/web/pods/x/eviction?fieldManager=kubectl", contentType: "application/json; charset=utf-8", body: `{}`})
	assert.Equal(t, Write{
		Method: "POST", Path: "/api/v1/namespaces/web/pods/x/eviction?fieldManager=kubectl", Subresource: "eviction",
		ContentType: "application/json; charset=utf-8", Body: []byte(`{}`),
	}, evict)
	assert.Equal(t, "finalize", s.askedFor(t, asker, writeCase{method: "PUT", path: "/api/v1/namespaces/web/finalize", contentType: "application/json", body: `{}`}).Subresource)
	assert.Empty(t, s.askedFor(t, asker, writeCase{method: "POST", path: "/api/v1/namespaces/web/pods", contentType: "application/json", body: `{}`}).Subresource)
}

// A dry run on a group the API server serves itself is a read: it runs unasked
// and unrecorded in every mode, as a read does. It is read strictly: only a POST, PUT or
// PATCH whose every dryRun is All. A DELETE never is one, since the API server
// reads a delete's options from its body when it has one. On any other group
// it asks, still marked a dry run, since an aggregated API may ignore it.
func TestADryRunIsARead(t *testing.T) {
	const pods = "/api/v1/namespaces/web/pods"
	dryRuns := []writeCase{
		{method: "POST", path: pods + "?dryRun=All", contentType: "application/json", body: `{}`},
		{method: "PUT", path: pods + "/x?dryRun=All&dryRun=All", contentType: "application/json", body: `{}`},
		{method: "PATCH", path: pods + "/x?fieldManager=m&dryRun=All", contentType: "application/merge-patch+json", body: `{}`},
	}
	for _, mode := range permissions.Modes {
		api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
		asker := newRecordingAsker()
		s := serveIn(t, api.upstream(), sessionIn(mode), asker)
		for _, c := range dryRuns {
			resp, body := s.do(t, s.writeRequest(t, c.method, c.path, c.contentType, c.body))
			assert.Equal(t, http.StatusOK, resp.StatusCode, "%s %s under %s: %s", c.method, c.path, mode, body)
		}
		assert.Len(t, api.requests(), len(dryRuns), "under %s", mode)
		assert.Empty(t, asker.records, "a read is not recorded")
	}

	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)
	for _, c := range []writeCase{
		{method: "POST", path: pods + "?dryrun=All", contentType: "application/json", body: `{}`},
		{method: "POST", path: pods + "?dryRun=All&dryRun=x", contentType: "application/json", body: `{}`},
		{method: "POST", path: pods + "?dryRun=", contentType: "application/json", body: `{}`},
		{method: "DELETE", path: pods + "/x?dryRun=All", contentType: "application/json", body: `{}`},
		{method: "DELETE", path: pods + "/x?dryRun=All"},
		{method: "DELETE", path: pods + "/x", contentType: "application/json", body: `{"dryRun":["All"]}`},
	} {
		assert.False(t, s.askedFor(t, asker, c).DryRun, c.method+" "+c.path+" "+c.body)
	}
	custom := writeCase{method: "POST", path: "/apis/example.com/v1/namespaces/web/widgets?dryRun=All", contentType: "application/json", body: `{}`}
	assert.True(t, s.askedFor(t, asker, custom).DryRun, "a custom group's dry run asks")
	assert.Empty(t, api.requests())

	secrets := answering(t, secretJSON)
	s = serveIn(t, secrets.upstream(), sessionIn(permissions.ModeReadOnly), newRecordingAsker())
	resp, body := s.do(t, s.writeRequest(t, "POST", "/api/v1/namespaces/web/secrets?dryRun=All", "application/json", `{"metadata":{"name":"db"}}`))
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	assert.JSONEq(t, redactedSecretJSON, body, "a dry run on secrets reads nothing a GET would not")
}

// A write's answer on secrets is redacted, as a read's is.
func TestAWritesAnswerOnSecretsIsRedacted(t *testing.T) {
	api := answering(t, secretJSON)
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	done := s.sendAsync(t, s.writeRequest(t, "PATCH", "/api/v1/namespaces/web/secrets/db", "application/merge-patch+json", `{"metadata":{"labels":{"a":"b"}}}`))
	asker.next(t).answer <- once
	r := testutil.Recv(t, done, "the write to be answered")

	require.NotNil(t, r.resp)
	require.Equal(t, http.StatusOK, r.resp.StatusCode, r.body)
	assert.JSONEq(t, redactedSecretJSON, r.body)
}

// A write whose wait ends forwards nothing: its client dropping the connection
// ends the wait, and so does the grant's end.
func TestAWriteWhoseWaitEndsForwardsNothing(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	ctx, drop := context.WithCancel(t.Context())
	s.sendAsync(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/a", "", "").WithContext(ctx))
	a := asker.next(t)
	drop()
	testutil.Wait(t, a.ctx.Done(), "the wait to end with its client")

	s.sendAsync(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/b", "", ""))
	b := asker.next(t)
	s.g.End()
	testutil.Wait(t, b.ctx.Done(), "the wait to end with the grant")
	s.g.Wait()
	assert.Empty(t, api.requests())
}

// holdingAsker waits for its write's context to end, then for release, so a
// test can hold a handler open past End.
type holdingAsker struct {
	asked   chan struct{}
	release chan struct{}
}

func (a holdingAsker) Ask(ctx context.Context, _ Request) (permissions.Answer, error) {
	close(a.asked)
	<-ctx.Done()
	<-a.release
	return permissions.Answer{}, ctx.Err()
}

func (holdingAsker) Record(context.Context, Request, permissions.Decision, string) error {
	return nil
}

// End cancels a waiting Ask and returns; Wait returns only once that Ask has.
// A request arriving after End answers 401.
func TestWaitJoinsTheHandlers(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	asker := holdingAsker{asked: make(chan struct{}), release: make(chan struct{})}
	s := serveAsking(t, api.upstream(), asker)
	s.sendAsync(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/a", "", ""))
	testutil.Wait(t, asker.asked, "the write to be asked")

	s.g.End()
	waited := make(chan struct{})
	go func() {
		s.g.Wait()
		close(waited)
	}()
	testutil.NoRecv(t, (<-chan struct{})(waited), quietWindow, "Wait returning while an Ask is open")
	close(asker.release)
	testutil.Wait(t, waited, "Wait to return once the Ask has")

	resp, _ := s.send(t, "GET", "/api", s.g.Token())
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// failingReader fails every read, as a client that drops its body does.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// A write whose body cannot be read is never asked.
func TestAWriteWhoseBodyCannotBeReadIsNotAsked(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)
	req := httptest.NewRequest("PATCH", "http://"+Host+"/api/v1/namespaces/web/configmaps/x", failingReader{})
	req.Header.Set("Proxy-Authorization", proxyAuth(ProxyUser, s.g.Token()))
	req.Header.Set("Content-Type", "application/merge-patch+json")

	s.g.ServeHTTP(httptest.NewRecorder(), req)

	testutil.NoRecv(t, (<-chan asked)(asker), quietWindow, "a write whose body could not be read asked")
	assert.Empty(t, api.requests())
}

// A write whose query does not parse is refused unasked: the pair that fails
// is dropped on the way to the API server, so a selector shown on the request
// would not reach it, and a delete of the matching pods would delete them all.
func TestAWriteWithAQueryThatDoesNotParseIsRefused(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	for _, path := range []string{
		"/api/v1/namespaces/web/pods?labelSelector=app%3Dfoo;",
		"/api/v1/namespaces/web/pods?labelSelector=app%zz",
	} {
		r, write := s.sendWrite(t, asker, writeCase{method: "DELETE", path: path})
		assert.Nil(t, write, path)
		assertForbidden(t, r.resp, r.body, refusedQuery)
	}
	assert.Empty(t, api.requests())
}

// A JSON body that does not parse has no keys uniqueKeys can vouch for.
func TestUniqueKeysRefusesABodyThatDoesNotParse(t *testing.T) {
	for _, body := range []string{``, `{"a":`, `[1,`} {
		assert.False(t, uniqueKeys("application/json", []byte(body)), body)
	}
	assert.True(t, uniqueKeys("application/json", []byte(`{"a":[1,{"b":2}]}`)))
}

// A body with anything after its one value is refused, a stray closing bracket
// included, so the body described is the body the API server reads.
func TestABodyWithTrailingDataIsRefused(t *testing.T) {
	for _, body := range []string{`{"a":1}}`, `{"a":1}]`, `{"a":1} x`, `{"a":1} {}`} {
		_, err := decodeBody("application/json", []byte(body))
		assert.ErrorContains(t, err, "trailing data", body)
		assert.False(t, uniqueKeys("application/json", []byte(body)), body)
	}
	value, err := decodeBody("application/json", []byte("{\"a\":1}\n"))
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"a": json.Number("1")}, value)
	assert.True(t, uniqueKeys("application/json", []byte("{\"a\":1}\n")))
}

// A background command's grant has no asker, so it refuses every write as
// today, under Auto and under an Allow rule alike, and records nothing.
func TestABackgroundWriteIsRefused(t *testing.T) {
	allow := rule(permissions.Allow, permissions.Write, "c1/**")
	for _, sess := range []session.Session{sessionIn(permissions.ModeAuto), sessionIn(permissions.ModeAsk, allow)} {
		api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
		s := serveGrant(t, NewGrant(api.upstream(), sess, "c1", "dev", nil, noAsker, 1000, 1000, 32))

		resp, body := s.do(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/x", "", ""))

		assertForbidden(t, resp, body, noAsker)
		assert.Empty(t, api.requests())
	}
}

// A write the mode or a rule allows reaches the cluster with nobody asked,
// recorded allowed with its reason before it is forwarded; one whose record the
// store refuses forwards nothing.
func TestAnAllowedWriteForwardsUnasked(t *testing.T) {
	asker := newRecordingAsker()
	var recordedFirst bool
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) { recordedFirst = len(asker.records) == 1 })
	s := serveIn(t, api.upstream(), sessionIn(permissions.ModeAuto), asker)

	resp, body := s.do(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/x", "", ""))

	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	assert.True(t, recordedFirst, "the record lands before the forward")
	r := testutil.Recv(t, (<-chan recorded)(asker.records), "the record")
	assert.Equal(t, permissions.Allowed, r.d)
	assert.Equal(t, "auto mode", r.why)
	assert.Equal(t, "/api/v1/namespaces/web/pods/x", r.w.Path)
	assert.Empty(t, asker.fakeAsker, "nobody was asked")

	allow := rule(permissions.Allow, permissions.Write, "c1/**")
	s = serveIn(t, api.upstream(), sessionIn(permissions.ModeAsk, allow), asker)
	resp, body = s.do(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/x", "", ""))
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	r = testutil.Recv(t, (<-chan recorded)(asker.records), "the record")
	assert.Equal(t, "a rule allows it: Allow writes anywhere in dev", r.why)

	failing := newRecordingAsker()
	failing.err = errors.New("store refused")
	before := len(api.requests())
	s = serveIn(t, api.upstream(), sessionIn(permissions.ModeAuto), failing)
	resp, body = s.do(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/x", "", ""))
	assertForbidden(t, resp, body, refusedUnrecorded)
	assert.Len(t, api.requests(), before, "nothing runs that the record does not hold")
}

// A write the mode or a rule refuses is a 403 naming why, recorded refused; a
// record the store refuses changes nothing about the answer. A session with no
// policy is read-only.
func TestADeniedWriteIsAForbiddenStatus(t *testing.T) {
	deny := rule(permissions.Deny, permissions.Write, "c1/**")
	for _, c := range []struct {
		sess session.Session
		why  string
	}{
		{sessionIn(permissions.ModeReadOnly), "this account is read-only"},
		{session.Session{Kind: session.Chat}, "this account is read-only"},
		{sessionIn(permissions.ModeAuto, deny), "a rule denies it: Deny writes anywhere in dev"},
		{session.Session{Kind: session.Monitor, Policy: sessionIn(permissions.ModeAuto).Policy}, "it is never allowed"},
	} {
		for _, fail := range []bool{false, true} {
			api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
			asker := newRecordingAsker()
			if fail {
				asker.err = errors.New("store refused")
			}
			s := serveIn(t, api.upstream(), c.sess, asker)

			resp, body := s.do(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/x", "", ""))

			assertForbidden(t, resp, body, refusal("kubetail: Delete pods/x in web on dev is not allowed: "+c.why))
			r := testutil.Recv(t, (<-chan recorded)(asker.records), "the record")
			assert.Equal(t, permissions.Denied, r.d)
			assert.Equal(t, c.why, r.why)
			assert.Empty(t, api.requests())
		}
	}
}

// A read-only context refuses every write, a destructive one and an RBAC one
// included, and asks before a Secret read shows its data, answering it
// redacted once denied.
func TestAReadOnlyContextRefusesEveryWrite(t *testing.T) {
	api := answering(t, secretJSON)
	asker := newRecordingAsker()
	s := serveIn(t, api.upstream(), sessionIn(permissions.ModeReadOnly), asker)
	for _, c := range []writeCase{
		{method: "DELETE", path: "/api/v1/namespaces/web"},
		{method: "POST", path: "/apis/rbac.authorization.k8s.io/v1/namespaces/web/rolebindings", contentType: "application/json", body: `{}`},
		{method: "PATCH", path: "/apis/apps/v1/namespaces/web/deployments/api", contentType: "application/merge-patch+json", body: `{"spec":{"replicas":0}}`},
		{method: "PUT", path: "/api/v1/namespaces/web/configmaps/x", contentType: "application/json", body: `{}`},
	} {
		resp, body := s.do(t, s.writeRequest(t, c.method, c.path, c.contentType, c.body))
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, c.method+" "+c.path)
		assert.Contains(t, body, "this account is read-only", c.method+" "+c.path)
	}
	assert.Empty(t, api.requests())

	done := s.sendAsync(t, s.request(t, "GET", "/api/v1/namespaces/web/secrets/db", s.g.Token()))
	asker.next(t).answer <- denied
	r := testutil.Recv(t, done, "the read")
	require.NotNil(t, r.resp)
	require.Equal(t, http.StatusOK, r.resp.StatusCode)
	assert.JSONEq(t, redactedSecretJSON, r.body)
}

// The proxy asks the session for the policy of the cluster its grant was made
// for, and the action carries the grant's context and its session's kind.
func TestThePolicyIsReadForTheGrantsCluster(t *testing.T) {
	var clusters []string
	sess := session.Session{
		Kind: session.Subagent,
		Policy: func(_ context.Context, target permissions.Target, account string) permissions.Policy {
			clusters = append(clusters, string(target)+" "+account)
			return permissions.Policy{Mode: permissions.ModeAuto}
		},
	}
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	asker := newRecordingAsker()
	s := serveGrant(t, NewGrant(api.upstream(), sess, "c1", "arn:aws:eks:us-east-1:1:cluster/dev", asker, noAsker, 1000, 1000, 32))

	resp, body := s.do(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/x", "", ""))

	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	assert.Equal(t, []string{"kubernetes c1"}, clusters)
	r := testutil.Recv(t, (<-chan recorded)(asker.records), "the record")
	assert.Equal(t, permissions.Subagent, r.r.Action.Principal)
	assert.Equal(t, "arn:aws:eks:us-east-1:1:cluster/dev", r.r.Action.AccountName)
}

// A write is decided under the write lock, so an allowed one behind a pending
// one waits for it, and the cluster sees them in the order they were decided.
func TestDecideRunsUnderTheWriteLock(t *testing.T) {
	var mu sync.Mutex
	var order []string
	api := newAPIServer(t, func(_ http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, r.Method+" "+r.URL.Path)
	})
	allow := rule(permissions.Allow, permissions.Write, "c1/**")
	allow.Resource = "configmaps"
	asker := newRecordingAsker()
	s, waiters := serveCountingWaitersIn(t, api.upstream(), sessionIn(permissions.ModeAsk, allow), asker)

	first := s.sendAsync(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/x", "", ""))
	pending := asker.next(t)
	second := s.sendAsync(t, s.writeRequest(t, "PUT", "/api/v1/namespaces/web/configmaps/c", "application/json", `{}`))
	require.Eventually(t, func() bool { return waiters.Load() == 1 }, time.Second, time.Millisecond, "the allowed write waits for the lock")
	assert.Empty(t, api.requests())

	pending.answer <- once
	testutil.Recv(t, first, "the first write")
	testutil.Recv(t, second, "the second write")
	assert.Equal(t, []string{"DELETE /api/v1/namespaces/web/pods/x", "PUT /api/v1/namespaces/web/configmaps/c"}, order)
}

// A request carries the action the classifier read, whether a rule may allow
// it, and the rules each allow answer adds, in the words Settings uses.
func TestARequestCarriesTheAction(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	done := s.sendAsync(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/x", "", ""))
	a := asker.next(t)
	assert.Equal(t, "Delete pods/x in web on dev", a.r.Action.Summary)
	assert.Equal(t, permissions.Write, a.r.Action.AccessLevel)
	assert.True(t, a.r.Grantable)
	assert.Equal(t, "Allow delete of core pods in dev / web for this command", a.r.CommandRule)
	assert.Equal(t, "Allow writes in dev / web", a.r.ChatRule)
	a.answer <- denied
	testutil.Recv(t, done, "the write to be answered")

	asker2 := newRecordingAsker()
	s = serveIn(t, api.upstream(), sessionIn(permissions.ModeAuto), asker2)
	resp, body := s.do(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/y", "", ""))
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	r := testutil.Recv(t, (<-chan recorded)(asker2.records), "the write's record")
	assert.Equal(t, "Delete pods/y in web on dev", r.r.Action.Summary, "an allowed write is recorded with its action")
}

// A dry run that asks, on a group version that may ignore it, offers no rule:
// a rule names no dry run, so one written from it would allow the real write.
func TestADryRunAsksWithNoGrant(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	done := s.sendAsync(t, s.writeRequest(t, "PATCH", "/apis/example.com/v1/namespaces/web/widgets/w?dryRun=All", "application/merge-patch+json", `{}`))
	a := asker.next(t)
	assert.True(t, a.r.Action.DryRun)
	assert.False(t, a.r.Grantable)
	assert.Empty(t, a.r.CommandRule)
	assert.Empty(t, a.r.ChatRule)
	a.answer <- denied
	testutil.Recv(t, done, "the write to be answered")
	assert.Empty(t, api.requests())
}

// A destructive write asks each time, so it offers no rule.
func TestADestructiveWriteOffersNoRule(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	done := s.sendAsync(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web", "", ""))
	a := asker.next(t)
	assert.Equal(t, permissions.Destructive, a.r.Action.AccessLevel)
	assert.False(t, a.r.Grantable)
	a.answer <- denied
	testutil.Recv(t, done, "the write to be answered")
}

// A command answer allows the same change for the rest of the command: the
// other pods' deletes run unasked, recorded allowed under the rule's line, and
// a change of another resource still asks.
func TestACommandAnswerAllowsTheRestOfTheCommand(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	asker := newRecordingAsker()
	s := serveIn(t, api.upstream(), askSession, asker)

	done := s.sendAsync(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/a", "", ""))
	asker.next(t).answer <- permissions.Answer{Approved: true, Duration: permissions.DurationCommand}
	testutil.Recv(t, done, "the first delete")

	for _, pod := range []string{"b", "c"} {
		resp, body := s.do(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/"+pod, "", ""))
		require.Equal(t, http.StatusOK, resp.StatusCode, body)
		r := testutil.Recv(t, (<-chan recorded)(asker.records), "the delete's record")
		assert.Equal(t, permissions.Allowed, r.d)
		assert.Equal(t, "a rule allows it: Allow delete of core pods in dev / web for this command", r.why)
	}

	done = s.sendAsync(t, s.writeRequest(t, "DELETE", "/apis/apps/v1/namespaces/web/deployments/d", "", ""))
	asker.next(t).answer <- denied
	testutil.Recv(t, done, "the deployment's delete")
}

// A command's rule lives on its grant: the next command asks again.
func TestACommandRuleEndsWithTheGrant(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)
	done := s.sendAsync(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/a", "", ""))
	asker.next(t).answer <- permissions.Answer{Approved: true, Duration: permissions.DurationCommand}
	testutil.Recv(t, done, "the first command's delete")

	next := serveAsking(t, api.upstream(), asker)
	done = next.sendAsync(t, next.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/b", "", ""))
	asker.next(t).answer <- denied
	testutil.Recv(t, done, "the next command's delete")
}

// The session's rules are read on every write, so one written between two
// writes of one command allows the second.
func TestARuleWrittenBetweenTwoWritesAllowsTheSecond(t *testing.T) {
	api := newAPIServer(t, func(http.ResponseWriter, *http.Request) {})
	var rules atomic.Pointer[[]permissions.Rule]
	rules.Store(&[]permissions.Rule{})
	sess := session.Session{Kind: session.Chat, Policy: func(context.Context, permissions.Target, string) permissions.Policy {
		return permissions.Policy{Mode: permissions.ModeAsk, Rules: *rules.Load()}
	}}
	asker := newRecordingAsker()
	s := serveIn(t, api.upstream(), sess, asker)

	done := s.sendAsync(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/a", "", ""))
	asker.next(t).answer <- once
	testutil.Recv(t, done, "the first delete")
	rules.Store(&[]permissions.Rule{permissions.GrantRule(permissions.Action{Target: permissions.Kubernetes, AccessLevel: permissions.Write, Scope: "c1/web"})})

	resp, body := s.do(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/b", "", ""))
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	assert.Equal(t, permissions.Allowed, testutil.Recv(t, (<-chan recorded)(asker.records), "the record").d)
}
