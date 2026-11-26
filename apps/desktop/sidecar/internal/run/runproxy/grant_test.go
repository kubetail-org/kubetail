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

package runproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
)

// testLimits leave every limit wide but the one a test narrows.
var testLimits = Limits{QPS: 1000, Burst: 1000, Open: 32, Bodies: 16}

const noAsker = "kubetail: this sandbox reads AWS and changes nothing."

// switchedOn is a cloud platform's switch that is always on.
func switchedOn(context.Context) bool { return true }

// policySession is a session of kind whose every account has mode.
func policySession(kind session.Kind, mode permissions.Mode) session.Session {
	return session.Session{Kind: kind, Policy: func(context.Context, permissions.Target, string) permissions.Policy {
		return permissions.Policy{Mode: mode}
	}}
}

func newTestGrant(t *testing.T, asker Asker, limits Limits) *Grant {
	t.Helper()
	g := NewGrant(permissions.AWS, switchedOn, policySession(session.Chat, permissions.ModeAsk), asker, noAsker, limits)
	t.Cleanup(g.End)
	return g
}

func TestOnlyTheGrantsTokenHolds(t *testing.T) {
	g := newTestGrant(t, nil, testLimits)
	other := newTestGrant(t, nil, testLimits)
	assert.Len(t, g.Token(), 64, "256 bits, hex")
	assert.NotEqual(t, g.Token(), other.Token())
	assert.True(t, g.Holds(g.Token()))
	assert.False(t, g.Holds(""))
	assert.False(t, g.Holds(other.Token()))
	g.End()
	assert.False(t, g.Holds(g.Token()), "an ended grant's token holds nothing")
}

// The switch is read on every Enter, with the request's context: off, the
// request is refused; on again, it enters.
func TestEnterReadsTheSwitch(t *testing.T) {
	type key struct{}
	on := true
	var read context.Context
	g := NewGrant(permissions.AWS, func(ctx context.Context) bool { read = ctx; return on }, session.Session{Kind: session.Chat}, nil, noAsker, testLimits)
	t.Cleanup(g.End)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(context.WithValue(r.Context(), key{}, "the request's"))

	on = false
	_, done, ok := g.Enter(r)
	done()
	assert.False(t, ok, "a switch that is off refuses the request")
	assert.Equal(t, "the request's", read.Value(key{}))

	on = true
	_, done, ok = g.Enter(r)
	assert.True(t, ok)
	done()
}

// A switch read in flight belongs to the grant: End cancels it, and Wait
// joins the request that made it.
func TestEndCancelsASwitchReadAndWaitJoinsIt(t *testing.T) {
	reading := make(chan struct{})
	g := NewGrant(permissions.AWS, func(ctx context.Context) bool {
		close(reading)
		<-ctx.Done()
		return true
	}, session.Session{Kind: session.Chat}, nil, noAsker, testLimits)
	entered := make(chan bool, 1)
	go func() {
		_, done, ok := g.Enter(httptest.NewRequest(http.MethodGet, "/", nil))
		done()
		entered <- ok
	}()
	testutil.Wait(t, reading, "the switch read")
	g.End()
	assert.False(t, testutil.Recv(t, entered, "Enter's answer"), "a grant that ended mid-read lets nothing in")
	g.Wait()
}

// Admit challenges a request without the token before reading the switch, and
// refuses one Enter refuses 401, never challenged.
func TestAdmitChecksTheTokenThenTheSwitch(t *testing.T) {
	on, reads := true, 0
	g := NewGrant(permissions.GitHub, func(context.Context) bool { reads++; return on }, session.Session{Kind: session.Chat}, nil, noAsker,
		Limits{QPS: 1000, Burst: 1000, Open: 32, Bodies: 16, Challenges: true})
	t.Cleanup(g.End)
	admit := func(token string) (*httptest.ResponseRecorder, bool) {
		w := httptest.NewRecorder()
		_, done, ok := g.Admit(w, httptest.NewRequest(http.MethodGet, "/", nil), token)
		done()
		return w, ok
	}

	w, ok := admit("")
	assert.False(t, ok)
	assert.Equal(t, http.StatusProxyAuthRequired, w.Code)
	assert.Zero(t, reads, "a request without the token read the switch")

	on = false
	w, ok = admit(g.Token())
	assert.False(t, ok)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Empty(t, w.Header().Get("Proxy-Authenticate"))
	assert.Equal(t, unauthorized+"\n", w.Body.String())

	on = true
	_, ok = admit(g.Token())
	assert.True(t, ok)
	assert.Equal(t, 2, reads)
}

func TestAGrantWithNoSwitchPanics(t *testing.T) {
	assert.Panics(t, func() { NewGrant(permissions.AWS, nil, session.Session{}, nil, noAsker, testLimits) })
}

func TestAGrantForTheClusterPanics(t *testing.T) {
	assert.Panics(t, func() { NewGrant(permissions.Kubernetes, switchedOn, session.Session{}, nil, noAsker, testLimits) })
}

// A limit below one would refuse every request the user approves.
func TestAGrantWithAZeroLimitPanics(t *testing.T) {
	for name, narrow := range map[string]func(*Limits){
		"qps":    func(l *Limits) { l.QPS = 0 },
		"burst":  func(l *Limits) { l.Burst = 0 },
		"open":   func(l *Limits) { l.Open = 0 },
		"bodies": func(l *Limits) { l.Bodies = 0 },
	} {
		limits := testLimits
		narrow(&limits)
		assert.Panics(t, func() { NewGrant(permissions.AWS, switchedOn, session.Session{}, nil, noAsker, limits) }, name)
	}
}

func TestAChallengingGrantAnswers407(t *testing.T) {
	limits := testLimits
	limits.Challenges = true
	w := httptest.NewRecorder()
	newTestGrant(t, nil, limits).Challenge(w)
	assert.Equal(t, http.StatusProxyAuthRequired, w.Code)
	assert.Equal(t, `Basic realm="kubetail"`, w.Header().Get("Proxy-Authenticate"))

	w = httptest.NewRecorder()
	newTestGrant(t, nil, testLimits).Challenge(w)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Empty(t, w.Header().Get("Proxy-Authenticate"))
	assert.Equal(t, "kubetail: unauthorized\n", w.Body.String())
}

// Enter lets a request in while the grant lives, ends it with the grant, and
// Wait joins it.
func TestEndCancelsAnEnteredRequest(t *testing.T) {
	g := newTestGrant(t, nil, testLimits)
	r, done, ok := g.Enter(httptest.NewRequest(http.MethodGet, "/", nil))
	require.True(t, ok)
	g.End()
	testutil.Wait(t, r.Context().Done(), "the request to end with the grant")
	joined := make(chan struct{})
	go func() {
		g.Wait()
		close(joined)
	}()
	done()
	testutil.Wait(t, joined, "Wait to join the handler")
	_, done, ok = g.Enter(httptest.NewRequest(http.MethodGet, "/", nil))
	assert.False(t, ok, "an ended grant lets nothing in")
	done()
}

func TestOnlyAllowedHeadersAreForwarded(t *testing.T) {
	in := http.Header{
		"Content-Type":           {"application/json"},
		"X-Amz-Target":           {"ec2.Describe"},
		"X-Http-Method-Override": {"DELETE"},
		"Authorization":          {"Bearer token"},
		"Proxy-Authorization":    {"Basic abc"},
	}
	out := Outbound(in, []string{"Content-Type", "X-Amz-Target", "Authorization", "Proxy-Authorization"})
	assert.Equal(t, http.Header{
		"Content-Type": {"application/json"},
		"X-Amz-Target": {"ec2.Describe"},
	}, out)
}

// post is a request with body, and its media type.
func post(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestAnEncodedBodyIsRefused(t *testing.T) {
	limits := testLimits
	limits.Bodies = 1
	g := newTestGrant(t, nil, limits)
	r := post("gzipped")
	r.Header.Set("Content-Encoding", "gzip")
	w := httptest.NewRecorder()
	_, release, status := g.Body(w, r)
	assert.Equal(t, http.StatusForbidden, status)
	release()
	// No slot was taken: the one slot is still free.
	_, release, status = g.Body(httptest.NewRecorder(), post("{}"))
	assert.Zero(t, status)
	release()
}

func TestBodiesHeldAreCapped(t *testing.T) {
	limits := testLimits
	limits.Bodies = 2
	g := newTestGrant(t, nil, limits)
	held := make([]func(), 0, 2)
	for range 2 {
		body, release, status := g.Body(httptest.NewRecorder(), post(`{"n":1}`))
		require.Zero(t, status)
		assert.Equal(t, `{"n":1}`, string(body))
		held = append(held, release)
	}
	// None free: the body is not read.
	r := post("unread")
	_, release, status := g.Body(httptest.NewRecorder(), r)
	assert.Equal(t, http.StatusTooManyRequests, status)
	release()
	rest := make([]byte, 6)
	n, _ := r.Body.Read(rest)
	assert.Equal(t, "unread", string(rest[:n]), "a refused body is left unread")

	// A request with no body takes none.
	body, release, status := g.Body(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Zero(t, status)
	assert.Nil(t, body)
	release()

	held[0]()
	_, release, status = g.Body(httptest.NewRecorder(), post("{}"))
	assert.Zero(t, status, "a slot comes free when its request ends")
	release()
	held[1]()
}

func TestABodyPastTheCapIs413(t *testing.T) {
	g := newTestGrant(t, nil, testLimits)
	_, release, status := g.Body(httptest.NewRecorder(), post(strings.Repeat("x", maxBody+1)))
	defer release()
	assert.Equal(t, http.StatusRequestEntityTooLarge, status)
}

// The read body is the request's body again, for a cloud platform that forwards
// r.
func TestABodyReadIsForwardable(t *testing.T) {
	g := newTestGrant(t, nil, testLimits)
	r := post(`{"a":1}`)
	_, release, status := g.Body(httptest.NewRecorder(), r)
	defer release()
	require.Zero(t, status)
	rest := make([]byte, 16)
	n, _ := r.Body.Read(rest)
	assert.Equal(t, `{"a":1}`, string(rest[:n]))
	assert.Equal(t, int64(7), r.ContentLength)
}

func TestOpenRequestsAreCapped(t *testing.T) {
	limits := testLimits
	limits.Open = 1
	g := newTestGrant(t, nil, limits)
	release, status := g.Slot(t.Context())
	require.Zero(t, status)
	none, status := g.Slot(t.Context())
	assert.Equal(t, http.StatusTooManyRequests, status)
	none()
	release()
	release, status = g.Slot(t.Context())
	assert.Zero(t, status)
	release()
}

// A slot whose limiter wait ends, as the grant does, is no slot.
func TestASlotWhoseWaitEndsIsUnauthorized(t *testing.T) {
	g := newTestGrant(t, nil, testLimits)
	g.End()
	release, status := g.Slot(g.ctx)
	assert.Equal(t, http.StatusUnauthorized, status)
	release()
	// The open slot it took is given back.
	assert.True(t, g.open.TryAcquire(int64(testLimits.Open)))
}

// failingBody ends before its length, as a client that leaves does.
type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("the client left") }

func TestABodyThatEndsEarlyIs400(t *testing.T) {
	limits := testLimits
	limits.Bodies = 1
	g := newTestGrant(t, nil, limits)
	r := httptest.NewRequest(http.MethodPost, "/", io.NopCloser(failingBody{}))
	r.ContentLength = 10
	_, release, status := g.Body(httptest.NewRecorder(), r)
	assert.Equal(t, http.StatusBadRequest, status)
	release()
	_, release, status = g.Body(httptest.NewRecorder(), post("{}"))
	assert.Zero(t, status, "its slot was given back")
	release()
}

// A body whose client stalls partway is not read past the grant's end: End
// interrupts the read on the socket, the slot comes free, and Wait returns
// with the connection still open.
func TestEndInterruptsAStalledBodyRead(t *testing.T) {
	limits := testLimits
	limits.Bodies = 1
	g := newTestGrant(t, nil, limits)
	reading := testutil.NewSignal()
	statuses := make(chan int, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r, done, ok := g.Enter(r)
		require.True(t, ok)
		defer done()
		reading.Fire()
		_, release, status := g.Body(w, r)
		release()
		statuses <- status
	}))
	t.Cleanup(srv.Close)

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	_, err = fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: x\r\nContent-Type: text/plain\r\nContent-Length: 100\r\n\r\nhalf")
	require.NoError(t, err)
	reading.Wait(t, "the body's read to start")

	g.End()
	assert.Equal(t, http.StatusBadRequest, testutil.Recv(t, statuses, "the stalled read's answer"))
	testutil.WaitReturn(t, g.Wait, "Wait, with the upload still open")
	assert.True(t, g.bodies.TryAcquire(1), "the body slot came free")
}

// Each status Body and Slot answer has its message.
func TestEachStatusHasItsRefusal(t *testing.T) {
	assert.Contains(t, BodyRefusal(http.StatusForbidden), "encoded body")
	assert.Contains(t, BodyRefusal(http.StatusTooManyRequests), "too many request bodies")
	assert.Equal(t, "kubetail: the sandbox does not send a body past 1 MiB", BodyRefusal(http.StatusRequestEntityTooLarge))
	assert.Contains(t, BodyRefusal(http.StatusBadRequest), "did not arrive whole")
	assert.Contains(t, BodyRefusal(http.StatusTeapot), "was refused", "a status Body never answers")
	assert.Equal(t, refusedOpen, SlotRefusal(http.StatusTooManyRequests))
	assert.Equal(t, unauthorized, SlotRefusal(http.StatusUnauthorized))
}

// An Upgrade, or Upgrade anywhere in a Connection list, asks past HTTP.
func TestUpgradesReadsTheConnectionList(t *testing.T) {
	assert.False(t, Upgrades(http.Header{"Connection": {"keep-alive"}}))
	for _, h := range []http.Header{
		{"Upgrade": {"h2c"}}, {"Connection": {"Upgrade"}}, {"Connection": {"keep-alive, upgrade"}},
		{"Connection": {"keep-alive", "Upgrade"}}, {"Upgrade": {""}}, {"Upgrade": {"", "websocket"}},
	} {
		assert.True(t, Upgrades(h), h)
	}
}

// A body a cloud platform streams itself holds one of the grant's body slots, and
// End interrupts its stalled read as it does Body's: the slot comes free and
// Wait returns with the upload still open.
func TestABodySlotIsCappedAndEndsWithTheGrant(t *testing.T) {
	limits := testLimits
	limits.Bodies = 1
	g := newTestGrant(t, nil, limits)
	reading := testutil.NewSignal()
	errs := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r, done, ok := g.Enter(r)
		require.True(t, ok)
		defer done()
		release, status := g.BodySlot(w, r)
		require.Zero(t, status)
		defer release()
		reading.Fire()
		_, err := io.ReadAll(r.Body)
		errs <- err
	}))
	t.Cleanup(srv.Close)

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	_, err = fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 100\r\n\r\nhalf")
	require.NoError(t, err)
	reading.Wait(t, "the stream's read to start")

	release, status := g.BodySlot(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil))
	assert.Equal(t, http.StatusTooManyRequests, status, "every body slot is held")
	release()

	g.End()
	assert.Error(t, testutil.Recv(t, errs, "the stalled read's end"))
	testutil.WaitReturn(t, g.Wait, "Wait, with the upload still open")
	assert.True(t, g.bodies.TryAcquire(1), "the body slot came free")
}
