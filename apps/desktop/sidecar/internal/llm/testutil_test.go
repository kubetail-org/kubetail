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

package llm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/stretchr/testify/require"
)

type event struct{ event, data string }

// searchName is the test search's name: what its calls and usage are recorded
// under. The wire reads a call by its call name, never this.
const searchName = "acme_search"

// sse writes one event the way the API does, flushing so a test reads it as it lands.
func sse(w http.ResponseWriter, e event) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.event, e.data)
	w.(http.Flusher).Flush()
}

// recorded is what the server was asked, for the request half of a test.
type recorded struct {
	calls  atomic.Int64
	mu     sync.Mutex
	body   map[string]any
	header http.Header
	path   string
	query  url.Values
}

func (r *recorded) take(t *testing.T, req *http.Request) {
	t.Helper()
	r.calls.Add(1)
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	r.mu.Lock()
	defer r.mu.Unlock()
	r.body, r.header, r.path, r.query = decoded, req.Header.Clone(), req.URL.Path, req.URL.Query()
}

// newServer runs handler until the test ends and answers its base URL.
func newServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL
}

// newRawServer answers one connection with line as its whole status line, and
// hands back its base URL. The request is read before the reply goes: closing a
// socket with bytes still unread sends a RST, which would lose the reply the
// test is about and leave a transport failure in its place.
func newRawServer(t *testing.T, line string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		if req, err := http.ReadRequest(br); err == nil {
			io.Copy(io.Discard, req.Body)
		}
		fmt.Fprintf(c, "HTTP/1.1 %s\r\n\r\n", line)
	}()
	return "http://" + ln.Addr().String()
}

// openAIEnv is the SDK's whole chain at v3.61.0, read off DefaultClientOptions.
// It moves with a bump.
var openAIEnv = []string{
	"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_ADMIN_KEY", "OPENAI_ORG_ID",
	"OPENAI_PROJECT_ID", "OPENAI_WEBHOOK_SECRET", "OPENAI_CUSTOM_HEADERS",
}

// fakeTimer is the idle guard's timer under a test's hand: it fires only when
// the test says so, and not while stopped, and reports every reset, so a test
// can wait for a read to reach the guard before it goes on.
type fakeTimer struct {
	mu      sync.Mutex
	fire    func()
	stopped bool
	resets  *testutil.Probe[struct{}]
}

func newFakeTimer() *fakeTimer {
	return &fakeTimer{resets: testutil.NewProbe[struct{}](1)}
}

// clock is the idleClock a wire is handed.
func (f *fakeTimer) clock(_ time.Duration, fire func()) idleTimer {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fire = fire
	return f
}

func (f *fakeTimer) Reset(time.Duration) bool {
	f.mu.Lock()
	f.stopped = false
	f.mu.Unlock()
	f.resets.Fire(struct{}{})
	return true
}

func (f *fakeTimer) Stop() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = true
	return true
}

// trip fires the guard, as the bound passing would, unless it is stopped.
func (f *fakeTimer) trip() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.stopped {
		f.fire()
	}
}

// awaitReset waits for the next reset: a read that reached the guard.
func (f *fakeTimer) awaitReset(t *testing.T) {
	t.Helper()
	f.resets.Await(t, "the idle guard's reset")
}

// beat asks the server for one beat after another, each seen to reset the guard
// before the next is asked for, then ends the stream. There are more beats than
// the stream's opening reads, so those cannot stand in for them.
func beat(t *testing.T, timer *fakeTimer, beats chan<- struct{}) {
	t.Helper()
	for range 15 {
		timer.resets.Drain()
		beats <- struct{}{}
		timer.awaitReset(t)
	}
	close(beats)
}

// serveBeats answers with head, then calls beat each time the test sends on
// beats, then tail once the test closes it.
func serveBeats(beats <-chan struct{}, head, beat, tail func(http.ResponseWriter)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		head(w)
		for {
			select {
			case _, ok := <-beats:
				if !ok {
					tail(w)
					return
				}
				beat(w)
			case <-r.Context().Done():
				return
			}
		}
	}
}

// streamResult is what a stream run on its own goroutine answered.
type streamResult struct {
	resp Response
	err  error
}

// goStream runs a stream on a goroutine of its own, so the test can move its
// clock while it reads.
func goStream(stream func() (Response, error)) <-chan streamResult {
	done := make(chan streamResult, 1)
	go func() {
		resp, err := stream()
		done <- streamResult{resp, err}
	}()
	return done
}

// fakeSigner signs every request with its token, or fails with err, and records
// the Authorization of each request it was told was refused.
type fakeSigner struct {
	token string
	err   error

	mu      sync.Mutex
	refused []string
}

func (s *fakeSigner) Sign(r *http.Request) error {
	if s.err != nil {
		return s.err
	}
	r.Header.Set("Authorization", "Bearer "+s.token)
	return nil
}

func (s *fakeSigner) Refused(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refused = append(s.refused, r.Header.Get("Authorization"))
}

func (s *fakeSigner) refusals() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refused
}

// signedBy is p with s as its credential in place of its key.
func signedBy(p Provider, s Signer) Provider {
	p.Key, p.Signer = "", s
	return p
}

// heldSigner signs once released, and tells the test each time a Sign waits.
type heldSigner struct {
	waiting *testutil.Signal
	release chan struct{}
}

func newHeldSigner() heldSigner {
	return heldSigner{waiting: testutil.NewSignal(), release: make(chan struct{})}
}

func (s heldSigner) Sign(r *http.Request) error {
	s.waiting.Fire()
	select {
	case <-s.release:
		return nil
	case <-r.Context().Done():
		return r.Context().Err()
	}
}

func (heldSigner) Refused(*http.Request) {}
