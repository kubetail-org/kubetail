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

package browser

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

const (
	lbState = "test-state-123"
	lbPath  = "/auth/callback"
)

// newTestListener binds a real 127.0.0.1 listener and registers its shutdown.
func newTestListener(t *testing.T) *Listener {
	t.Helper()
	l, err := Listen(lbState, lbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	return l
}

// callbackURL is the listener's redirect URL carrying the given query.
func callbackURL(t *testing.T, l *Listener, q url.Values) string {
	t.Helper()
	u, err := url.Parse(l.RedirectURL())
	require.NoError(t, err)
	u.RawQuery = q.Encode()
	return u.String()
}

// hitCallback fires a GET at the listener's redirect URL with the given query.
func hitCallback(t *testing.T, l *Listener, q url.Values) *http.Response {
	t.Helper()
	resp, err := http.Get(callbackURL(t, l, q))
	require.NoError(t, err)
	return resp
}

// The happy path: a redirect carrying the expected state and a code lands on the
// "signed in" page, and Wait answers the whole query.
func TestListenerDeliversTheQuery(t *testing.T) {
	l := newTestListener(t)

	resp := hitCallback(t, l, url.Values{"state": {lbState}, "code": {"the-code"}, "client_id": {"issued"}})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "Signed in")

	q, err := l.Wait(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "the-code", q.Get("code"))
	assert.Equal(t, "issued", q.Get("client_id"))
}

// RedirectURL points at the bound 127.0.0.1 listener and the path it was given,
// so it can be registered as the redirect_uri.
func TestListenerRedirectURL(t *testing.T) {
	u, err := url.Parse(newTestListener(t).RedirectURL())
	require.NoError(t, err)
	assert.Equal(t, "http", u.Scheme)
	assert.Equal(t, lbPath, u.Path)
	assert.True(t, strings.HasPrefix(u.Host, "127.0.0.1:"), u.Host)
}

// Invalid callbacks are rejected (400) and must neither consume nor abort the
// one-shot flow: state is validated before anything else, so a stray request —
// wrong state, missing code, or an ?error= without the matching state — can't
// kill it. Each case is proven by a following legit redirect that still delivers.
func TestListenerRejectsInvalidCallbackWithoutConsuming(t *testing.T) {
	for _, tc := range []struct {
		name string
		q    url.Values
	}{
		{"bad state", url.Values{"state": {"wrong"}, "code": {"stray"}}},
		{"missing code", url.Values{"state": {lbState}}},
		{"error without matching state", url.Values{"error": {"access_denied"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := newTestListener(t)

			bad := hitCallback(t, l, tc.q)
			bad.Body.Close()
			assert.Equal(t, http.StatusBadRequest, bad.StatusCode)

			good := hitCallback(t, l, url.Values{"state": {lbState}, "code": {"real-code"}})
			good.Body.Close()
			q, err := l.Wait(context.Background())
			require.NoError(t, err)
			assert.Equal(t, "real-code", q.Get("code"), "a stray request must not consume the flow")
		})
	}
}

// A redirect carrying ?error= WITH the expected state surfaces as a Wait error
// naming the issuer's code.
func TestListenerSurfacesAnAuthorizationError(t *testing.T) {
	l := newTestListener(t)

	resp := hitCallback(t, l, url.Values{"state": {lbState}, "error": {"access_denied"}})
	resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	_, err := l.Wait(context.Background())
	authErr, ok := errors.AsType[*AuthorizationError](err)
	require.True(t, ok, "err = %v", err)
	assert.Equal(t, "access_denied", authErr.Code)
	assert.Contains(t, err.Error(), "access_denied")
}

// A second declined authorization must still get a response, though only the
// first callback reaches Wait: a handler that waited to hand its error over would
// park for the life of the process.
func TestListenerSurvivesRepeatedAuthorizationErrors(t *testing.T) {
	l := newTestListener(t)
	q := url.Values{"state": {lbState}, "error": {"access_denied"}}

	first := hitCallback(t, l, q)
	first.Body.Close()

	// A wedged handler never answers, so the response is the only event to wait on.
	u := callbackURL(t, l, q)
	testutil.WaitReturn(t, func() {
		if resp, err := http.Get(u); err == nil {
			resp.Body.Close()
		}
	}, "the second authorization-error callback to answer")

	_, err := l.Wait(context.Background())
	assert.ErrorContains(t, err, "access_denied")
}

// The callback listens on a port any local process can reach, so a peer that
// opens a connection and then stalls must not hold a slot until the sign-in
// times out.
func TestListenerBoundsItsReads(t *testing.T) {
	l := newTestListener(t)
	for name, got := range map[string]time.Duration{
		"ReadHeaderTimeout": l.srv.ReadHeaderTimeout,
		"ReadTimeout":       l.srv.ReadTimeout,
		"IdleTimeout":       l.srv.IdleTimeout,
	} {
		assert.Positive(t, got, name)
	}
}

// The Serve goroutine's whole job is to surface a listener failure to whoever
// is waiting. Closing l.ln rather than l.Close() is what makes Serve fail: Close
// is the shutdown path Serve reports as ErrServerClosed and swallows.
func TestListenerSurfacesAServeFailure(t *testing.T) {
	l := newTestListener(t)
	require.NoError(t, l.ln.Close())

	// Bounded: a Serve failure that never reaches the channel leaves Wait with
	// nothing to return, and the test has to fail rather than hang.
	ctx, cancel := context.WithTimeout(context.Background(), testutil.Timeout)
	defer cancel()
	_, err := l.Wait(ctx)
	assert.ErrorIs(t, err, net.ErrClosed)
}

// The callback is one-shot: a redirect replayed after the code has been accepted
// is answered and dropped, never queued behind the first.
func TestListenerKeepsOnlyTheFirstCode(t *testing.T) {
	l := newTestListener(t)

	for _, code := range []string{"first", "second"} {
		resp := hitCallback(t, l, url.Values{"state": {lbState}, "code": {code}})
		resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

	q, err := l.Wait(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "first", q.Get("code"))
}

// The first callback to land is the answer, whichever kind it is: a declined
// authorization is not overtaken by a code behind it, nor a code by an error.
// Repeated, since a listener that raced the two would pass one run half the time.
func TestListenerAnswersTheFirstCallback(t *testing.T) {
	declined := url.Values{"state": {lbState}, "error": {"access_denied"}}
	code := url.Values{"state": {lbState}, "code": {"c"}}
	for range 8 {
		l := newTestListener(t)
		for _, q := range []url.Values{declined, code} {
			hitCallback(t, l, q).Body.Close()
		}
		_, err := l.Wait(context.Background())
		require.ErrorContains(t, err, "access_denied")

		l = newTestListener(t)
		for _, q := range []url.Values{code, declined} {
			hitCallback(t, l, q).Body.Close()
		}
		q, err := l.Wait(context.Background())
		require.NoError(t, err)
		require.Equal(t, "c", q.Get("code"))
	}
}

// Wait honors context cancellation when no redirect arrives.
func TestListenerWaitRespectsContext(t *testing.T) {
	l := newTestListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := l.Wait(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

// The opener is chosen per platform, and every arm is checked from whichever one
// runs the suite: the mapping takes the OS as an argument so none of it is
// unreachable.
func TestCommand(t *testing.T) {
	const u = "https://issuer/authorize?state=x"
	for _, tc := range []struct {
		goos string
		name string
		args []string
	}{
		{"darwin", "open", []string{u}},
		{"windows", "rundll32", []string{"url.dll,FileProtocolHandler", u}},
		{"linux", "xdg-open", []string{u}},
		{"freebsd", "xdg-open", []string{u}},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			name, args := Command(tc.goos, u)
			assert.Equal(t, tc.name, name)
			assert.True(t, slices.Equal(args, tc.args), "%v", args)
		})
	}
}
