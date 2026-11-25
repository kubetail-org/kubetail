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

//go:build debug

package kubeconn

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/supervisor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

func TestParseFaultsReadsEveryVerbAndFault(t *testing.T) {
	t.Parallel()

	faults, err := ParseFaults("get,list,watch secrets=403; get api=refused; list * = tls; " +
		"get pods=timeout; watch pods=quiet; watch pods=stalled; list nodes=401; list nodes=404; list nodes=429; " +
		"list nodes=500; list nodes=503; store=fail")

	require.NoError(t, err)
	assert.Equal(t, []rule{
		{verbs: []string{"get", "list", "watch"}, resource: "secrets", fault: "403", code: 403},
		{verbs: []string{"get"}, resource: "api", fault: "refused"},
		{verbs: []string{"list"}, resource: "*", fault: "tls"},
		{verbs: []string{"get"}, resource: "pods", fault: "timeout"},
		{verbs: []string{"watch"}, resource: "pods", fault: "quiet"},
		{verbs: []string{"watch"}, resource: "pods", fault: "stalled"},
		{verbs: []string{"list"}, resource: "nodes", fault: "401", code: 401},
		{verbs: []string{"list"}, resource: "nodes", fault: "404", code: 404},
		{verbs: []string{"list"}, resource: "nodes", fault: "429", code: 429},
		{verbs: []string{"list"}, resource: "nodes", fault: "500", code: 500},
		{verbs: []string{"list"}, resource: "nodes", fault: "503", code: 503},
	}, faults.rules, "in the order written")
	assert.True(t, faults.StoreFails())
}

func TestParseFaultsIgnoresWhitespaceAroundARuleAVerbAndTheEquals(t *testing.T) {
	t.Parallel()

	faults, err := ParseFaults("  list ,  watch \t secrets  =  403 ;  store = fail  ")

	require.NoError(t, err)
	assert.Equal(t, []rule{{verbs: []string{"list", "watch"}, resource: "secrets", fault: "403", code: 403}}, faults.rules)
	assert.True(t, faults.StoreFails())
}

// A mistyped rule fails the start with the rule quoted, so it never passes as a healthy cluster.
func TestParseFaultsRefusesAMistypedRuleNamingIt(t *testing.T) {
	t.Parallel()

	for _, spec := range []string{
		"post pods=403",      // an unknown verb
		"get pods=418",       // an unknown fault
		"get pods 403",       // no =
		"pods=403",           // no verb
		"get =403",           // no resource
		"get, pods=403",      // an empty verb
		"get Pods=403",       // not a path's resource name
		"list pods=quiet",    // quiet on a verb that is not watch
		"get pods=stalled",   // stalled on a verb that is not watch
		"watch pods=timeout", // timeout on a watch
		"store=broken",       // a store fault that is not fail
	} {
		_, err := ParseFaults("get api=refused; " + spec)
		assert.ErrorContains(t, err, `"`+spec+`"`, spec)
	}
}

// Every row of the spec's table: the verb and resource the API server would read off the path.
func TestReadRequestReadsThePathAsTheAPIServerDoes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		method, target   string
		verb, resource   string
		matchesSomething bool
	}{
		{"GET", "/api", "get", "api", true},
		{"GET", "/api/v1/namespaces/kube-system", "get", "namespaces", true},
		{"GET", "/api/v1/namespaces", "list", "namespaces", true},
		{"GET", "/api/v1/namespaces/default/secrets", "list", "secrets", true},
		{"GET", "/api/v1/secrets?watch=true&resourceVersion=12", "watch", "secrets", true},
		{"GET", "/apis/apps/v1/namespaces/default/deployments/web", "get", "deployments", true},
		{"GET", "/apis/apps/v1/deployments", "list", "deployments", true},
		{"GET", "/api/v1/namespaces/default/pods/web/log?follow=true", "", "", false},
		{"GET", "/api/v1/namespaces/default/status", "", "", false},
		{"GET", "/api/v1/namespaces/default/finalize", "", "", false},
		{"GET", "/apis/apps/v1", "", "", false},
		{"GET", "/apis", "", "", false},
		{"GET", "/api/v1", "", "", false},
		{"GET", "/version", "", "", false},
		{"POST", "/api/v1/namespaces/default/pods", "", "", false},
	} {
		req := httptest.NewRequest(tc.method, "https://k8s.example"+tc.target, nil)
		verb, resource, ok := readRequest(req, "")
		assert.Equal(t, tc.matchesSomething, ok, tc.target)
		assert.Equal(t, tc.verb, verb, tc.target)
		assert.Equal(t, tc.resource, resource, tc.target)
	}
}

// A kubeconfig may serve the API under a path, which the server never sees as part of the
// request.
func TestReadRequestDropsTheBaseURLsPathPrefix(t *testing.T) {
	t.Parallel()

	for _, prefix := range []string{"/k8s/clusters/c-1", "/k8s/clusters/c-1/"} {
		req := httptest.NewRequest("GET", "https://rancher.example/k8s/clusters/c-1/api/v1/namespaces/default/secrets", nil)
		verb, resource, ok := readRequest(req, prefix)
		require.True(t, ok, prefix)
		assert.Equal(t, "list", verb)
		assert.Equal(t, "secrets", resource)

		root := httptest.NewRequest("GET", "https://rancher.example/k8s/clusters/c-1/api", nil)
		_, resource, ok = readRequest(root, prefix)
		require.True(t, ok, prefix)
		assert.Equal(t, "api", resource)
	}
}

func TestMatchTakesTheFirstRuleWhoseVerbAndResourceHold(t *testing.T) {
	t.Parallel()

	faults := mustFaults(t, "watch secrets=403; list,get secrets=500; list *=refused")

	for _, tc := range []struct {
		verb, resource, fault string
	}{
		{"watch", "secrets", "403"},
		{"list", "secrets", "500"},
		{"get", "secrets", "500"},
		{"list", "pods", "refused"},
		{"get", "pods", ""},
		{"watch", "pods", ""},
	} {
		r, ok := faults.match(tc.verb, tc.resource)
		assert.Equal(t, tc.fault != "", ok, tc)
		assert.Equal(t, tc.fault, r.fault, tc)
	}
}

// mustFaults parses spec, which the test wrote to parse.
func mustFaults(t *testing.T, spec string) Faults {
	t.Helper()
	faults, err := ParseFaults(spec)
	require.NoError(t, err)
	return faults
}

// faultedConnTo builds a connection to srv carrying the rules in spec.
func faultedConnTo(t *testing.T, srv *httptest.Server, spec string) *Connection {
	t.Helper()
	conn, err := NewConnection(restConfigFor(srv), WithFaults(mustFaults(t, spec)))
	require.NoError(t, err)
	return conn
}

// A matched request is answered inside the connection; everything else reaches the server.
func TestAMatchedRequestNeverReachesTheServer(t *testing.T) {
	t.Parallel()

	c := serveCluster(t)
	conn := faultedConnTo(t, c.Server, "get namespaces=403; list *=500")

	err := conn.getJSON(t.Context(), kubeSystemPath, &struct{}{})

	var he *httpErr
	require.ErrorAs(t, err, &he)
	assert.Equal(t, http.StatusForbidden, he.code)
	_, reached := c.requests.TryAwait()
	assert.False(t, reached, "the faulted request never left the connection")

	for _, path := range []string{"/api", versionPath, "/api/v1/namespaces/default/pods/web/log", "/apis/apps/v1"} {
		_, _ = conn.getText(t.Context(), path)
		assert.Equal(t, path, c.requests.Await(t, path+" to reach the server"))
	}
	_ = conn.postJSON(t.Context(), "/api/v1/namespaces", []byte(`{}`), &struct{}{})
	assert.Equal(t, "/api/v1/namespaces", c.requests.Await(t, "a POST to reach the server"),
		"a write never reads as a list")
}

func TestAConnectionUnderAPathPrefixMatchesTheSameRequests(t *testing.T) {
	t.Parallel()

	c := serveCluster(t)
	faults := mustFaults(t, "get namespaces=403")
	cfg := restConfigFor(c.Server)
	cfg.Host += "/k8s/clusters/c-1"
	conn, err := NewConnection(cfg, WithFaults(faults))
	require.NoError(t, err)

	err = conn.getJSON(t.Context(), kubeSystemPath, &struct{}{})

	var he *httpErr
	require.ErrorAs(t, err, &he)
	assert.Equal(t, http.StatusForbidden, he.code)
	_, reached := c.requests.TryAwait()
	assert.False(t, reached)
}

// The idle bound stays the outer layer, so it bounds a faulted request too.
func TestTheFaultWrapperSitsInsideTheIdleBound(t *testing.T) {
	t.Parallel()

	faults := mustFaults(t, "get api=refused")
	conn, err := NewConnection(&rest.Config{Host: "https://k8s.example"}, WithFaults(faults))
	require.NoError(t, err)

	outer, ok := conn.Config.WrapTransport(http.DefaultTransport).(*idleTimeoutRoundTripper)
	require.True(t, ok)
	_, ok = outer.base.(*faultRoundTripper)
	assert.True(t, ok)
}

// Each fault answers as the transport would, so classify names it exactly as it names a real one.
func TestEachInjectedFaultClassifiesAsTheRealOne(t *testing.T) {
	t.Parallel()

	c := serveCluster(t)
	for fault, reason := range map[string]Reason{
		"401":     ReasonUnauthorized,
		"403":     ReasonForbidden,
		"404":     ReasonMalformed,
		"429":     ReasonThrottled,
		"500":     ReasonInternalError,
		"503":     ReasonServiceUnavailable,
		"refused": ReasonUnreachable,
		"tls":     ReasonTLSInvalid,
	} {
		conn := faultedConnTo(t, c.Server, "get api="+fault)

		err := conn.getJSON(t.Context(), "/api", &struct{}{})

		assert.Equal(t, reason, classify(err), fault)
	}
	_, reached := c.requests.TryAwait()
	assert.False(t, reached)
}

// A probe's own deadline ends a timeout, which reads as a server that never answered.
func TestAnInjectedTimeoutEndsWithTheProbesDeadline(t *testing.T) {
	t.Parallel()

	conn := faultedConnTo(t, serveCluster(t).Server, "get api=timeout")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	err := conn.getJSON(ctx, "/api", &struct{}{})

	assert.Equal(t, ReasonTimeout, classify(err))
}

// client-go reads an injected status as the StatusError a kind's sync reports.
func TestAnInjectedStatusReachesDynamicAsAStatusError(t *testing.T) {
	t.Parallel()

	conn := faultedConnTo(t, serveCluster(t).Server, "list,watch secrets=403")
	secrets := conn.Dynamic.Resource(schema.GroupVersionResource{Version: "v1", Resource: "secrets"})

	_, err := secrets.List(t.Context(), metav1.ListOptions{})
	assert.True(t, apierrors.IsForbidden(err), "list: %v", err)
	assert.Contains(t, err.Error(), "injected by KUBETAIL_FAULTS")

	_, err = secrets.Watch(t.Context(), metav1.ListOptions{})
	assert.True(t, apierrors.IsForbidden(err), "watch: %v", err)
}

// The probes keep their own reading of a 404: a missing kube-system is NotFound.
func TestTheServerUIDProbeReadsAnInjected404AsNotFound(t *testing.T) {
	t.Parallel()

	conn := faultedConnTo(t, serveCluster(t).Server, "get namespaces=404")

	res, _, _ := runProbe(t, serverUIDProbe{}, conn, nil)

	assert.Equal(t, supervisor.VerdictFailed, res.Verdict())
	assert.Equal(t, ReasonNotFound, res.Reason())
}

// A list has no caller deadline: the idle bound around the fault is what ends it, as it ends a
// stalled cold list, so it never holds a start slot past the bound.
func TestAnInjectedTimeoutOnAListEndsAtTheIdleBound(t *testing.T) {
	t.Parallel()

	faults := mustFaults(t, "list pods=timeout")
	unreached := roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("the faulted list reached the transport")
		return nil, nil
	})
	armed := make(chan func(), 1)
	rt := &idleTimeoutRoundTripper{base: faults.wrapper("")(unreached), timeout: time.Hour,
		afterFunc: func(_ time.Duration, tick func()) idleTimer {
			armed <- tick
			return &handTimer{}
		}}

	done := make(chan error, 1)
	go func() { _, rerr := rt.RoundTrip(listRequest("")); done <- rerr }()
	testutil.Recv(t, armed, "the watchdog to arm")()

	assert.ErrorIs(t, testutil.Recv(t, done, "the list to end"), ErrIdleTimeout)
}

// podsGVR is the collection the quiet tests watch.
var podsGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}

// opened is what one watch open through Dynamic answered with.
type opened struct {
	w   watch.Interface
	err error
}

// openPods opens a pods watch on ctx in the background, since a quiet reopen hangs.
func openPods(conn *Connection, ctx context.Context) <-chan opened {
	done := make(chan opened, 1)
	go func() {
		w, err := conn.Dynamic.Resource(podsGVR).Watch(ctx, metav1.ListOptions{})
		done <- opened{w, err}
	}()
	return done
}

// quietWindow bounds the negative assertions below: an open that hangs never answers, so the
// test waits this long to see it has not. A bounded wait by necessity, kept short.
const quietWindow = 20 * time.Millisecond

func TestAQuietWatchIsSilentThenItsReopenHangsUntilItsContextEnds(t *testing.T) {
	t.Parallel()

	c := serveCluster(t)
	conn := faultedConnTo(t, c.Server, "watch pods=quiet")
	// kubesync opens and reopens on one worker's context, and a stop cancels it.
	worker, stopWorker := context.WithCancel(t.Context())

	first := testutil.Recv(t, openPods(conn, worker), "the first open to answer")
	require.NoError(t, first.err)
	testutil.NoRecv(t, first.w.ResultChan(), quietWindow, "an event on the silent watch")
	first.w.Stop()
	testutil.RecvClosed(t, first.w.ResultChan(), "the stopped watch to end")
	require.NoError(t, worker.Err(), "the stop ended it, not the context")

	reopen := openPods(conn, worker)
	testutil.NoRecv(t, reopen, quietWindow, "the reopen to answer")
	stopWorker()
	assert.ErrorIs(t, testutil.Recv(t, reopen, "the cancelled reopen to end").err, context.Canceled)

	again := testutil.Recv(t, openPods(conn, t.Context()), "the next worker's open to answer")
	require.NoError(t, again.err)
	again.w.Stop()
	_, reached := c.requests.TryAwait()
	assert.False(t, reached, "no watch reached the server")
}

// A stalled watch has shown it bookmarks, so its silence after is a stream that stopped.
func TestAStalledWatchSendsOneBookmarkAtItsPositionThenNothing(t *testing.T) {
	t.Parallel()

	c := serveCluster(t)
	conn := faultedConnTo(t, c.Server, "watch pods=stalled")

	for _, position := range []string{"42", "43"} {
		w, err := conn.Dynamic.Resource(podsGVR).Watch(t.Context(),
			metav1.ListOptions{ResourceVersion: position, AllowWatchBookmarks: true})
		require.NoError(t, err)
		event := testutil.Recv(t, w.ResultChan(), "the bookmark")
		require.Equal(t, watch.Bookmark, event.Type, "%v", event.Object)
		assert.Equal(t, position, event.Object.(*unstructured.Unstructured).GetResourceVersion())
		testutil.NoRecv(t, w.ResultChan(), quietWindow, "an event after the bookmark")
		w.Stop()
		testutil.RecvClosed(t, w.ResultChan(), "the stopped watch to end")
	}
	_, reached := c.requests.TryAwait()
	assert.False(t, reached, "no watch reached the server")
}

// heldResets holds the callbacks a quiet sequence registers on its first open's context, so a
// test decides when, and whether, each runs.
type heldResets struct {
	mu  sync.Mutex
	fns []func()
}

func (h *heldResets) afterFunc(_ context.Context, f func()) func() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fns = append(h.fns, f)
	return func() bool { return true }
}

// run runs the i-th callback registered, as context.AfterFunc would once its context ended.
func (h *heldResets) run(i int) {
	h.mu.Lock()
	f := h.fns[i]
	h.mu.Unlock()
	f()
}

// quietTripper is one connection's wrapper under spec, its resets held. Its base fails the test,
// since nothing quiet may reach the server.
func quietTripper(t *testing.T, spec string) (*faultRoundTripper, *heldResets) {
	t.Helper()
	faults := mustFaults(t, spec)
	rt := faults.wrapper("")(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Errorf("%s reached the server", req.URL)
		return nil, nil
	})).(*faultRoundTripper)
	held := &heldResets{}
	rt.afterFunc = held.afterFunc
	return rt, held
}

// openOn opens a watch of path on ctx through rt, in the background.
func openOn(rt http.RoundTripper, ctx context.Context, path string) <-chan *http.Response {
	done := make(chan *http.Response, 1)
	go func() {
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "https://k8s.example"+path+"?watch=true", nil)
		resp, _ := rt.RoundTrip(req)
		done <- resp
	}()
	return done
}

// silent opens a watch that must answer at once with the silent 200.
func silent(t *testing.T, rt http.RoundTripper, ctx context.Context, path, what string) *http.Response {
	t.Helper()
	resp := testutil.Recv(t, openOn(rt, ctx, path), what)
	require.NotNil(t, resp, what)
	require.Equal(t, http.StatusOK, resp.StatusCode, what)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	return resp
}

// hangs opens a watch that must not answer, and leaves it to end with its context.
func hangs(t *testing.T, rt http.RoundTripper, ctx context.Context, path, what string) {
	t.Helper()
	testutil.NoRecv(t, openOn(rt, ctx, path), quietWindow, what)
}

const podsPath = "/api/v1/pods"

// Another client's watch on the collection kubesync watches gets a body of its own, and closing
// it moves nothing: only the sequence's own body starts the reopen.
func TestAnOpenDuringAQuietSequenceIsSilentAndOutsideIt(t *testing.T) {
	t.Parallel()

	rt, _ := quietTripper(t, "watch pods=quiet")
	first := silent(t, rt, t.Context(), podsPath, "the first open")

	extra := silent(t, rt, t.Context(), podsPath, "an open while the first stands")
	require.NoError(t, extra.Body.Close())
	silent(t, rt, t.Context(), podsPath, "an open after the extra closed").Body.Close()

	require.NoError(t, first.Body.Close())
	hangs(t, rt, t.Context(), podsPath, "the reopen to answer")
}

// The silent body ends on Close or on its context; Close is what a watcher's Stop sends.
func TestASilentBodyEndsOnCloseOrItsContext(t *testing.T) {
	t.Parallel()

	rt, _ := quietTripper(t, "watch *=quiet")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	byCancel := silent(t, rt, ctx, podsPath, "the first open")
	byClose := silent(t, rt, t.Context(), "/api/v1/secrets", "another collection's first open")

	read := readOnce(byCancel.Body)
	testutil.NoRecv(t, read, quietWindow, "the silent body to deliver")
	cancel()
	assert.ErrorIs(t, testutil.Recv(t, read, "the read to end with its context"), io.EOF)

	read = readOnce(byClose.Body)
	testutil.NoRecv(t, read, quietWindow, "the silent body to deliver")
	require.NoError(t, byClose.Body.Close())
	assert.ErrorIs(t, testutil.Recv(t, read, "the read to end on Close"), io.EOF)
}

// readOnce reads body once in the background.
func readOnce(body io.Reader) <-chan error {
	read := make(chan error, 1)
	go func() { _, err := body.Read(make([]byte, 1)); read <- err }()
	return read
}

// The synchronous check alone retires a sequence whose worker is gone, wherever it stopped:
// the resets stay held throughout.
func TestAnOpenRetiresASequenceWhoseFirstContextEnded(t *testing.T) {
	t.Parallel()

	rt, _ := quietTripper(t, "watch *=quiet")

	// Stopped with its watch still open.
	ctx, cancel := context.WithCancel(t.Context())
	silent(t, rt, ctx, podsPath, "the first open")
	cancel()
	second := silent(t, rt, t.Context(), podsPath, "the next worker's open")

	// Stopped after its body closed, its reopen not yet at the wrapper.
	ctx, cancel = context.WithCancel(t.Context())
	third := silent(t, rt, ctx, "/api/v1/secrets", "another collection's first open")
	require.NoError(t, third.Body.Close())
	cancel()
	silent(t, rt, t.Context(), "/api/v1/secrets", "the next worker's open after a close and a stop")
	second.Body.Close()
}

// The reset frees a collection nothing opens again; a late one leaves a newer sequence alone.
func TestALateResetLeavesANewerSequenceAlone(t *testing.T) {
	t.Parallel()

	rt, held := quietTripper(t, "watch pods=quiet")
	old, cancelOld := context.WithCancel(t.Context())
	silent(t, rt, old, podsPath, "the old worker's open")
	cancelOld()

	fresh := silent(t, rt, t.Context(), podsPath, "the new worker's open")
	held.run(0) // the old sequence's reset, running late

	require.NoError(t, fresh.Body.Close())
	hangs(t, rt, t.Context(), podsPath, "the new sequence's reopen")
}

func TestAResetFreesACollectionNothingOpensAgain(t *testing.T) {
	t.Parallel()

	rt, held := quietTripper(t, "watch pods=quiet")
	ctx, cancel := context.WithCancel(t.Context())
	silent(t, rt, ctx, podsPath, "the open")
	cancel()

	held.run(0)

	rt.mu.Lock()
	defer rt.mu.Unlock()
	assert.Empty(t, rt.quiet)
}

// Each collection, and each connection, plays its own sequence: no first open waits on another.
func TestEachCollectionAndConnectionPlaysItsOwnSequence(t *testing.T) {
	t.Parallel()

	rt, _ := quietTripper(t, "watch *=quiet")
	pods := silent(t, rt, t.Context(), podsPath, "pods' first open")
	silent(t, rt, t.Context(), "/api/v1/events", "core events' first open")
	silent(t, rt, t.Context(), "/apis/events.k8s.io/v1/events", "events.k8s.io's first open")

	require.NoError(t, pods.Body.Close())
	hangs(t, rt, t.Context(), podsPath, "pods' reopen")

	other, _ := quietTripper(t, "watch *=quiet")
	silent(t, other, t.Context(), podsPath, "pods' first open on another connection")
}

// The pool hands its rules to every connection its probe builds, so the ladder runs for real:
// an injected refusal fails the probe Unreachable and earns a retry, as a stopped server does.
func TestThePoolsProbeFailsAnInjectedRefusalAndRetries(t *testing.T) {
	t.Parallel()

	c := serveCluster(t)
	faults := mustFaults(t, "get api=refused")
	s := New(serving(c.Server, "prod", "key-1"), WithFaults(faults))
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	watched := lease.WatchState()
	defer watched.Close()

	first := awaitState(t, watched, func(st State) bool { return st.Connection.Failures >= 1 }).Connection
	assert.Equal(t, ReasonUnreachable, first.LastAttempt.Reason)
	assert.True(t, first.Scheduled(), "a failure earns a retry")

	s.supervisor.Wake("prod", nameConnection)
	awaitState(t, watched, func(st State) bool { return st.Connection.Failures >= 2 })
	_, reached := c.requests.TryAwait()
	assert.False(t, reached, "no probe reached the server")
}
