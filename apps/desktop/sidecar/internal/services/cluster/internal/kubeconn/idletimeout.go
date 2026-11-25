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

// The idle-read bound on one request that is not a stream. HTTP/2's READ_IDLE_TIMEOUT beside
// it is connection-level keepalive: it detects a dead peer, not a live one that has stopped
// sending, which is what a wedged LIST is.
package kubeconn

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/client-go/transport"
)

// ErrIdleTimeout is what a request the watchdog cancelled reports. Its own sentinel because
// the transport's answer to a cancel is a bare "context canceled", and that string is what a
// stalled cold list ends its run with — in front of a user, as the sync verdict's message.
var ErrIdleTimeout = errors.New("server stopped sending data")

// newIdleTimeoutWrapper installs the bound on a rest client's requests.
func newIdleTimeoutWrapper(timeout time.Duration) transport.WrapperFunc {
	return func(rt http.RoundTripper) http.RoundTripper {
		return &idleTimeoutRoundTripper{base: rt, timeout: timeout, afterFunc: afterFunc}
	}
}

// idleTimer is what the watchdog needs of a *time.Timer.
type idleTimer interface {
	Reset(time.Duration) bool
	Stop() bool
}

// afterFunc is time.AfterFunc as the watchdog's timer source.
func afterFunc(d time.Duration, f func()) idleTimer { return time.AfterFunc(d, f) }

type idleTimeoutRoundTripper struct {
	base    http.RoundTripper
	timeout time.Duration
	// afterFunc arms the watchdog's timer: time.AfterFunc in production, and a
	// test's own so it can tick the watchdog by hand.
	afterFunc func(time.Duration, func()) idleTimer
}

func (t *idleTimeoutRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Substring-matched rather than parsed: this runs on every LIST page and GET of a
	// resync, so allocating a url.Values map to read one param is wasted work on a hot path.
	// A follow is a stream like a watch, quiet for as long as the container is.
	if q := req.URL.RawQuery; strings.Contains(q, "watch=true") || strings.Contains(q, "follow=true") {
		return t.base.RoundTrip(req)
	}
	// Scoped to THIS request, so a stall never cancels the caller's run. Armed before
	// dialing, so a server that hangs before sending headers is cancelled too.
	ctx, cancel := context.WithCancel(req.Context())
	w := newIdleWatchdog(t.timeout, cancel, t.afterFunc)

	resp, err := t.base.RoundTrip(req.WithContext(ctx))
	if err != nil {
		w.stopAndCancel()
		return nil, w.substitute(err)
	}
	if resp.Body == nil {
		w.stopAndCancel()
		return resp, nil
	}
	w.progress() // headers arrived
	resp.Body = &idleReadCloser{body: resp.Body, w: w}
	return resp, nil
}

// idleWatchdog cancels a request that makes no read progress for a full window. Progress
// just bumps a counter; a self-rescheduling timer cancels only when the counter has not
// moved since its previous tick. Detection is coarse by construction — idle-to-cancel lands
// in [timeout, 2*timeout] — which is fine for a stall backstop.
//
// The timer re-arms ONLY from inside its own callback, never from the read path: a read
// landing exactly as the timer fires bumps the counter and the next tick sees it, where a
// Reset from the reader would race the firing cancel and kill a live transfer.
//
// mu closes the one window left, between a tick and a stop: without it a tick that read
// stopped==false could still Reset after stop() ran, arming a timer the watchdog was just
// retired. Held across the check and the Reset, and across the set and the Stop, the two are
// mutually exclusive.
type idleWatchdog struct {
	cancel  context.CancelFunc
	timeout time.Duration
	reads   atomic.Uint64
	// fired says the watchdog is what cancelled, which is what tells a stall from the
	// caller's own cancel — both reach the reader as context.Canceled.
	fired   atomic.Bool
	mu      sync.Mutex
	stopped bool // guarded by mu
	timer   idleTimer
	seen    uint64 // reads at the previous tick; touched only in tick, which is serial
}

func newIdleWatchdog(timeout time.Duration, cancel context.CancelFunc, afterFunc func(time.Duration, func()) idleTimer) *idleWatchdog {
	w := &idleWatchdog{cancel: cancel, timeout: timeout}
	w.timer = afterFunc(timeout, w.tick)
	return w
}

// progress records read activity — a header arrival or a body chunk.
func (w *idleWatchdog) progress() { w.reads.Add(1) }

func (w *idleWatchdog) tick() {
	n := w.reads.Load()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return
	}
	if n == w.seen {
		w.fired.Store(true)
		w.cancel()
		return
	}
	w.seen = n
	w.timer.Reset(w.timeout)
}

func (w *idleWatchdog) stop() {
	w.mu.Lock()
	w.stopped = true
	w.mu.Unlock()
	w.timer.Stop() // cancels an armed timer, including one a racing tick just re-armed
}

// stopAndCancel is the paired "done with this request", so no exit can retire the watchdog
// without also releasing its context.
func (w *idleWatchdog) stopAndCancel() {
	w.stop()
	w.cancel()
}

// substitute names the watchdog's own cancel, leaving every other failure — the caller's
// cancel included — to report itself.
func (w *idleWatchdog) substitute(err error) error {
	if w.fired.Load() {
		return ErrIdleTimeout
	}
	return err
}

// idleReadCloser reports read progress and retires the watchdog on Close. cancel is
// idempotent, so a stall-fire racing Close is harmless.
type idleReadCloser struct {
	body io.ReadCloser
	w    *idleWatchdog
}

func (r *idleReadCloser) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if n > 0 {
		r.w.progress()
	}
	if err != nil {
		return n, r.w.substitute(err)
	}
	return n, nil
}

func (r *idleReadCloser) Close() error {
	r.w.stopAndCancel()
	return r.body.Close()
}
