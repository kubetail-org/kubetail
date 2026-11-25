package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// After the close, a request is refused before it reaches any service. The gate is
// checked under the lock that close takes, so this one case also covers a handler
// dispatched before the close that reaches the gate after it.
func TestARequestAfterCloseIsRefusedAtTheGate(t *testing.T) {
	r := newRequests()
	called := false
	ts := httptest.NewServer(r.wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})))
	t.Cleanup(ts.Close)

	r.close()
	resp, err := http.Get(ts.URL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.False(t, called, "the handler ran after the close")
}

// close ends a request still running, and wait returns once its handler has.
func TestCloseEndsARequestAndWaitJoinsIt(t *testing.T) {
	r := newRequests()
	started := testutil.NewSignal()
	ts := httptest.NewServer(r.wrap(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		started.Fire()
		<-req.Context().Done()
	})))
	t.Cleanup(ts.Close)
	go func() {
		if resp, err := http.Get(ts.URL); err == nil {
			_ = resp.Body.Close()
		}
	}()
	started.Wait(t, "the handler to start")

	waited := waitAsync(r)
	// A negative assertion: nothing paces wait, so the window only has to be long
	// enough for a wait that ignored the handler to have returned.
	testutil.NoRecv(t, waited, 50*time.Millisecond, "wait while a handler runs")

	r.close()
	testutil.Wait(t, waited, "wait to return once close ended the handler")
}

// Shutdown counts HTTP/2 streams, not handlers: a stream the client reset leaves its
// count while the handler runs on. Only wait still sees it, which is why the stop
// joins handlers after every Shutdown, not only one that timed out.
func TestShutdownReturnsWhileAResetStreamsHandlerRuns(t *testing.T) {
	r := newRequests()
	started := testutil.NewSignal()
	release := testutil.NewSignal()
	ts := httptest.NewUnstartedServer(r.wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		started.Fire()
		<-release.Chan() // ignores its context
	})))
	ts.Config.Protocols = Protocols()
	ts.Start()
	t.Cleanup(ts.Close)
	t.Cleanup(func() { release.Fire() })

	// Prior knowledge, as the host dials.
	tr := &http.Transport{Protocols: new(http.Protocols)}
	tr.Protocols.SetUnencryptedHTTP2(true)
	t.Cleanup(tr.CloseIdleConnections)
	reqCtx, reset := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, ts.URL, nil)
	require.NoError(t, err)
	go func() {
		if resp, err := tr.RoundTrip(req); err == nil {
			_ = resp.Body.Close()
		}
	}()
	started.Wait(t, "the handler to start")
	reset()

	ctx, cancel := context.WithTimeout(context.Background(), testutil.Timeout)
	defer cancel()
	require.NoError(t, ts.Config.Shutdown(ctx))

	waited := waitAsync(r)
	// A negative assertion: wait has nothing to wake it but the handler's return.
	testutil.NoRecv(t, waited, 50*time.Millisecond, "wait while the reset stream's handler runs")
	release.Fire()
	testutil.Wait(t, waited, "wait to return once the handler has")
}

// waitAsync runs r.wait on its own goroutine and closes the channel when it returns.
func waitAsync(r *requests) <-chan struct{} {
	waited := make(chan struct{})
	go func() {
		r.wait()
		close(waited)
	}()
	return waited
}
