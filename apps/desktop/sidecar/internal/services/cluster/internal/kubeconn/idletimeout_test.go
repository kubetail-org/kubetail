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
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// stallBody is a server that holds the connection open and sends nothing — what
// READ_IDLE_TIMEOUT cannot see, since the peer is alive.
type stallBody struct{ ctx context.Context }

func (b *stallBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *stallBody) Close() error { return nil }

// servingStall answers with a body that never delivers, over the request's own context.
func servingStall() (http.RoundTripper, *stallBody) {
	body := &stallBody{}
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body.ctx = req.Context()
		return &http.Response{StatusCode: 200, Body: body}, nil
	}), body
}

func listRequest(query string) *http.Request {
	return httptest.NewRequest(http.MethodGet, "https://k8s/api/v1/pods?"+query, nil)
}

// A kind sync holds its start slot until the watch is open, so one hung cold list costs a
// permanent fraction of the fleet's start capacity and the kinds behind it never list a row.
func TestIdleTimeoutCancelsAStalledBody(t *testing.T) {
	t.Parallel()

	base, _ := servingStall()
	rt := newIdleTimeoutWrapper(30 * time.Millisecond)(base)

	resp, err := rt.RoundTrip(listRequest(""))
	require.NoError(t, err)

	// In a goroutine, so a wrapper that never cancels fails the test rather than hanging it.
	done := make(chan error, 1)
	go func() { _, rerr := io.ReadAll(resp.Body); done <- rerr }()
	assert.Error(t, testutil.Recv(t, done, "the stalled body to be cancelled"))
	_ = resp.Body.Close()
}

// A server that hangs before sending headers stalls just as hard, so the watchdog is armed
// before the request is dialed rather than around the body alone.
func TestIdleTimeoutCancelsAStallBeforeHeaders(t *testing.T) {
	t.Parallel()

	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	rt := newIdleTimeoutWrapper(30 * time.Millisecond)(base)

	done := make(chan error, 1)
	go func() { _, rerr := rt.RoundTrip(listRequest("")); done <- rerr }()
	assert.Error(t, testutil.Recv(t, done, "the request stalled before headers to be cancelled"))
}

// handTimer is the watchdog's timer under a test's control: it never fires on its own, and
// the test ticks it. Reset and Stop record nothing the tests read.
type handTimer struct{ tick func() }

func (*handTimer) Reset(time.Duration) bool { return true }
func (*handTimer) Stop() bool               { return true }

// tickedByHand is a wrapper whose watchdog ticks only when the test says so.
func tickedByHand(base http.RoundTripper) (http.RoundTripper, *handTimer) {
	timer := &handTimer{}
	rt := &idleTimeoutRoundTripper{base: base, timeout: time.Hour, afterFunc: func(_ time.Duration, f func()) idleTimer {
		timer.tick = f
		return timer
	}}
	return rt, timer
}

// chunkedBody delivers one byte per read, then EOF, honouring the request's context so a
// cancel reaches it.
type chunkedBody struct {
	ctx    context.Context
	chunks int
	served int
}

func (b *chunkedBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	if b.served >= b.chunks {
		return 0, io.EOF
	}
	b.served++
	p[0] = 'x'
	return 1, nil
}

func (b *chunkedBody) Close() error { return nil }

// The bound is on read PROGRESS, never wall-clock: a large collection legitimately takes
// longer than the window to stream, and a deadline would kill exactly the case this exists
// to protect. Headers and every body chunk count. The watchdog is ticked by hand, so the
// test is a sequence — a read between every two ticks, however long the machine takes —
// rather than a race against a real timer.
func TestIdleTimeoutLetsASlowButStreamingBodyFinish(t *testing.T) {
	t.Parallel()

	body := &chunkedBody{chunks: 8}
	rt, timer := tickedByHand(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body.ctx = req.Context()
		return &http.Response{StatusCode: 200, Body: body}, nil
	}))

	resp, err := rt.RoundTrip(listRequest(""))
	require.NoError(t, err)
	// More ticks than chunks, so the whole body streams under a window that elapsed
	// many times over; a read lands between every two of them.
	var data []byte
	buf := make([]byte, 1)
	for range 8 {
		timer.tick()
		n, rerr := resp.Body.Read(buf)
		require.NoError(t, rerr, "a steadily-progressing body must not be cancelled")
		data = append(data, buf[:n]...)
	}
	timer.tick()
	_, rerr := resp.Body.Read(buf)
	assert.ErrorIs(t, rerr, io.EOF)

	assert.Len(t, data, 8)
	require.NoError(t, resp.Body.Close())
}

// The same watchdog, with the reads stopped: two ticks with nothing between them are the
// stall, and the body reports it as the watchdog's own error.
func TestIdleTimeoutCancelsABodyThatStoppedProgressing(t *testing.T) {
	t.Parallel()

	body := &chunkedBody{chunks: 8}
	rt, timer := tickedByHand(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body.ctx = req.Context()
		return &http.Response{StatusCode: 200, Body: body}, nil
	}))

	resp, err := rt.RoundTrip(listRequest(""))
	require.NoError(t, err)
	buf := make([]byte, 1)
	_, err = resp.Body.Read(buf)
	require.NoError(t, err)
	timer.tick() // sees the read: the window resets
	timer.tick() // sees nothing: the stall

	_, err = resp.Body.Read(buf)
	assert.ErrorIs(t, err, ErrIdleTimeout)
}

// A response with no body has nothing left to read, so the request is done at its headers:
// its context is released and the watchdog retired, and no later tick fires on it.
func TestIdleTimeoutRetiresAtAResponseWithNoBody(t *testing.T) {
	t.Parallel()

	var reqCtx context.Context
	rt, timer := tickedByHand(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		reqCtx = req.Context()
		return &http.Response{StatusCode: http.StatusNoContent}, nil
	}))

	resp, err := rt.RoundTrip(listRequest(""))

	require.NoError(t, err)
	assert.Nil(t, resp.Body)
	assert.ErrorIs(t, reqCtx.Err(), context.Canceled, "the request's context is released")
	assert.NotPanics(t, func() { timer.tick(); timer.tick() })
}

// A tick that lands after the watchdog was stopped neither cancels nor re-arms: the request
// it watched has already finished.
func TestAStoppedWatchdogNeverFires(t *testing.T) {
	t.Parallel()

	cancelled := false
	timer := &handTimer{}
	w := newIdleWatchdog(time.Hour, func() { cancelled = true }, func(_ time.Duration, f func()) idleTimer {
		timer.tick = f
		return timer
	})

	w.stop()
	timer.tick()
	timer.tick()

	assert.False(t, cancelled)
	assert.False(t, w.fired.Load())
}

// A healthy watch is legitimately silent between bookmarks, so an inactivity bound would
// kill it. It is left with no watchdog at all rather than a longer one — RetryWatcher and
// HTTP/2 keepalive are what govern a watch.
func TestIdleTimeoutExemptsAWatch(t *testing.T) {
	t.Parallel()

	base, _ := servingStall()
	rt := newIdleTimeoutWrapper(10 * time.Millisecond)(base)

	resp, err := rt.RoundTrip(listRequest("watch=true"))

	require.NoError(t, err)
	_, wrapped := resp.Body.(*idleReadCloser)
	assert.False(t, wrapped, "a watch response carries no idle reader")
}

// A follow is a stream like a watch: a quiet `logs -f` would otherwise be cut.
func TestAFollowOutlivesTheIdleWindow(t *testing.T) {
	t.Parallel()

	base, _ := servingStall()
	rt := newIdleTimeoutWrapper(10 * time.Millisecond)(base)

	resp, err := rt.RoundTrip(listRequest("follow=true"))

	require.NoError(t, err)
	_, wrapped := resp.Body.(*idleReadCloser)
	assert.False(t, wrapped, "a follow response carries no idle reader")
}

// The verdict a stall ends a kind's run with is what the sync panel puts in front of the
// user, and a bare "context canceled" says nothing about what happened. The classification
// is otherwise right — the run's own context is untouched, so it reads as SyncFailed — so
// only the message needs supplying.
func TestIdleTimeoutSaysTheServerStoppedSending(t *testing.T) {
	t.Parallel()

	base, _ := servingStall()
	rt := newIdleTimeoutWrapper(30 * time.Millisecond)(base)

	resp, err := rt.RoundTrip(listRequest(""))
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { _, rerr := io.ReadAll(resp.Body); done <- rerr }()
	assert.ErrorIs(t, testutil.Recv(t, done, "the stalled read to fail"), ErrIdleTimeout)
	_ = resp.Body.Close()
}

// A request cancelled by its CALLER reports what the caller did, not a stall the server
// never had — the sentinel names the watchdog's own cancel and nothing else.
func TestIdleTimeoutLeavesACallersCancelAlone(t *testing.T) {
	t.Parallel()

	base, _ := servingStall()
	rt := newIdleTimeoutWrapper(time.Minute)(base)
	ctx, cancel := context.WithCancel(context.Background())

	resp, err := rt.RoundTrip(listRequest("").WithContext(ctx))
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { _, rerr := io.ReadAll(resp.Body); done <- rerr }()
	cancel()
	rerr := testutil.Recv(t, done, "the cancelled read to fail")
	assert.NotErrorIs(t, rerr, ErrIdleTimeout)
	assert.ErrorIs(t, rerr, context.Canceled)
	_ = resp.Body.Close()
}

// The bound has to reach the connection every LIST actually rides, beside the QPS/burst
// tuning. Asserted through the config's wrapper, since the production window is far longer
// than a test should wait out.
func TestNewConnectionBoundsItsNonWatchReads(t *testing.T) {
	t.Parallel()

	conn, err := NewConnection(&rest.Config{Host: "https://k8s.example"})
	require.NoError(t, err)

	require.NotNil(t, conn.Config.WrapTransport, "the connection carries the idle bound")
	_, wrapped := conn.Config.WrapTransport(http.DefaultTransport).(*idleTimeoutRoundTripper)
	assert.True(t, wrapped)
}
