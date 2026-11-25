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

package openrouter

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// fakeOpenRouter is OpenRouter as the sign-in sees it: the auth page, which
// redirects to the callback with a code, the exchange, which answers a key for
// a code whose verifier matches its challenge, and the key check. Its fields
// steer the next answers; every request is recorded.
type fakeOpenRouter struct {
	t   *testing.T
	srv *httptest.Server

	mu sync.Mutex
	// exchange, when set, answers every exchange in place of a minted key.
	exchange http.HandlerFunc
	// checkStatus answers every key check; zero is 200.
	checkStatus int
	// hold, when set, parks the exchange until it is closed.
	hold chan struct{}
	// holdCheck, when set, parks the key check until it is closed.
	holdCheck chan struct{}
	// dropCheck closes the key check's connection unanswered.
	dropCheck bool
	// arrived is signalled each time a request is parked on hold.
	arrived *testutil.Probe[string]

	// prefix makes this fake's keys its own: the redactor is the process's.
	prefix     string
	minted     int
	challenges map[string]string // code → the challenge its auth page was opened with
	auths      []url.Values
	exchanges  []map[string]string
	checks     []string // the bearer each key check carried
	keys       []string
}

func newFakeOpenRouter(t *testing.T) *fakeOpenRouter {
	t.Helper()
	f := &fakeOpenRouter{t: t, prefix: uuid.NewString(), challenges: map[string]string{}, arrived: testutil.NewProbe[string](8)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth", f.auth)
	mux.HandleFunc("POST /api/v1/auth/keys", f.exchangeCode)
	mux.HandleFunc("GET /api/v1/key", f.checkKey)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeOpenRouter) authURL() string { return f.srv.URL }
func (f *fakeOpenRouter) apiBase() string { return f.srv.URL + "/api/v1" }

// auth redirects to callback_url as OpenRouter does, appending the code to the
// query the callback already carries.
func (f *fakeOpenRouter) auth(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f.mu.Lock()
	f.auths = append(f.auths, q)
	f.minted++
	code := fmt.Sprintf("code-%d", f.minted)
	f.challenges[code] = q.Get("code_challenge")
	f.mu.Unlock()
	http.Redirect(w, r, q.Get("callback_url")+"&"+url.Values{"code": {code}}.Encode(), http.StatusFound)
}

// park waits on hold, when set, until it is closed.
func (f *fakeOpenRouter) park(hold chan struct{}, what string) {
	if hold != nil {
		f.arrived.Fire(what)
		<-hold
	}
}

func (f *fakeOpenRouter) exchangeCode(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
	f.mu.Lock()
	f.exchanges = append(f.exchanges, body)
	answer, hold := f.exchange, f.hold
	f.mu.Unlock()
	f.park(hold, "exchange")
	if answer != nil {
		answer(w, r)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	challenge, ok := f.challenges[body["code"]]
	if !ok || body["code_challenge_method"] != "S256" || oauth2.S256ChallengeFromVerifier(body["code_verifier"]) != challenge {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	delete(f.challenges, body["code"])
	f.minted++
	key := fmt.Sprintf("openrouter-%s-%04d", f.prefix, f.minted)
	f.keys = append(f.keys, key)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"key": key})
}

func (f *fakeOpenRouter) checkKey(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.checks = append(f.checks, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	status, drop, hold := f.checkStatus, f.dropCheck, f.holdCheck
	f.mu.Unlock()
	f.park(hold, "check")
	if drop {
		conn, _, err := w.(http.Hijacker).Hijack()
		require.NoError(f.t, err)
		conn.Close()
		return
	}
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
}

// set runs fn under the fake's lock, to steer its next answers.
func (f *fakeOpenRouter) set(fn func(f *fakeOpenRouter)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeOpenRouter) authQueries() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.auths...)
}

func (f *fakeOpenRouter) exchangeBodies() []map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]string(nil), f.exchanges...)
}

func (f *fakeOpenRouter) keyChecks() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.checks...)
}

// mintedKeys is every key the exchange has handed out.
func (f *fakeOpenRouter) mintedKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.keys...)
}

// fakeKeyring is the OS keyring in memory, whose reads, writes and deletes fail
// on request.
type fakeKeyring struct {
	mu        sync.Mutex
	entries   map[string]string
	setErr    map[string]error
	deleteErr map[string]error
	getErr    error
	// beforeSet, when set, is told the account of each write before it lands.
	beforeSet func(account string)
	sets      int
}

func newFakeKeyring() *fakeKeyring {
	return &fakeKeyring{entries: map[string]string{}, setErr: map[string]error{}, deleteErr: map[string]error{}}
}

func (k *fakeKeyring) Get(account string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.getErr != nil {
		return "", k.getErr
	}
	v, ok := k.entries[account]
	if !ok {
		return "", oskeyring.ErrNoEntry
	}
	return v, nil
}

func (k *fakeKeyring) Set(account, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.beforeSet != nil {
		k.beforeSet(account)
	}
	k.sets++
	if err := k.setErr[account]; err != nil {
		return err
	}
	k.entries[account] = value
	return nil
}

func (k *fakeKeyring) Delete(account string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.deleteErr[account]; err != nil {
		return err
	}
	delete(k.entries, account)
	return nil
}

func (k *fakeKeyring) entry(account string) (string, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.entries[account]
	return v, ok
}

func (k *fakeKeyring) put(account, value string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.entries[account] = value
}

func (k *fakeKeyring) failSet(account string, err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.setErr[account] = err
}

func (k *fakeKeyring) onSet(fn func(account string)) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.beforeSet = fn
}

func (k *fakeKeyring) failDelete(account string, err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.deleteErr[account] = err
}

func (k *fakeKeyring) failGet(err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.getErr = err
}

var errBoom = errors.New("boom")

// following is a browser that follows the auth URL, as a user who signs in
// would: OpenRouter's redirect lands on the loopback.
func following(rawURL string) error {
	go func() {
		resp, err := http.Get(rawURL)
		if err == nil {
			resp.Body.Close()
		}
	}()
	return nil
}

// parked is a browser that opens and goes nowhere: a user who has not signed in
// yet. It records each URL it was opened on.
type parked struct {
	mu   sync.Mutex
	urls []string
}

func (p *parked) open(rawURL string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.urls = append(p.urls, rawURL)
	return nil
}

func (p *parked) opened() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.urls...)
}

// rig is a service over a fake OpenRouter and keyring, rebuilt over the same
// keyring by restart.
type rig struct {
	t          *testing.T
	openRouter *fakeOpenRouter
	keyring    *fakeKeyring
	// calls is keyring as the service reaches it: recorded, and held on request.
	calls *oskeyringtest.Keyring
	// checkEnded fires each time a key check ends.
	checkEnded *testutil.Probe[struct{}]
	opts       []option
	svc        *Service
}

func newRig(t *testing.T, opts ...option) *rig {
	t.Helper()
	r := &rig{t: t, openRouter: newFakeOpenRouter(t), keyring: newFakeKeyring(), checkEnded: testutil.NewProbe[struct{}](4), opts: opts}
	r.calls = oskeyringtest.Wrap(r.keyring)
	r.svc = r.build()
	return r
}

func (r *rig) config() Config {
	return Config{AuthURL: r.openRouter.authURL(), APIBaseURL: r.openRouter.apiBase(), Keyring: r.calls}
}

func (r *rig) build() *Service {
	r.t.Helper()
	s := r.start()
	awaitRestored(r.t, s)
	return s
}

// start is build without the wait for the restore.
func (r *rig) start() *Service {
	r.t.Helper()
	opts := append([]option{
		withBrowser(following), withCheckEnded(func() { r.checkEnded.Fire(struct{}{}) }),
	}, r.opts...)
	return startService(r.t, r.config(), opts...)
}

// newService is a service that has restored, closed when the test ends.
func newService(t *testing.T, cfg Config, opts ...option) *Service {
	t.Helper()
	s := startService(t, cfg, opts...)
	awaitRestored(t, s)
	return s
}

// startService is newService without the wait for the restore.
func startService(t *testing.T, cfg Config, opts ...option) *Service {
	t.Helper()
	s, err := newWithOptions(cfg, opts...)
	require.NoError(t, err)
	t.Cleanup(s.Close)
	return s
}

// awaitRestored waits for s to publish what the keyring held.
func awaitRestored(t *testing.T, s *Service) {
	t.Helper()
	testutil.Wait(t, s.Restored(), "the restore")
}

// restart is a new service over the same keyring, as the next launch is.
func (r *rig) restart() *Service {
	r.t.Helper()
	r.svc.Close()
	r.svc = r.build()
	return r.svc
}

// relaunchHeld relaunches with the keyring's calls of kinds held until the
// release it answers, which the test's end calls too.
func (r *rig) relaunchHeld(kinds ...oskeyringtest.Kind) (*Service, func()) {
	r.t.Helper()
	release := r.calls.Hold(kinds...)
	r.t.Cleanup(release)
	r.svc.Close()
	r.svc = r.start()
	return r.svc, release
}

// awaitCall waits for the service to make c.
func (r *rig) awaitCall(c oskeyringtest.Call) {
	r.t.Helper()
	require.Eventually(r.t, func() bool { return slices.Contains(r.calls.Calls(), c) }, testutil.Timeout, time.Millisecond, "%v", c)
}

// storedKey is the key the keyring's entry holds, and whether the account
// holds anything.
func (r *rig) storedKey() (string, bool) {
	r.t.Helper()
	v, ok := r.keyring.entry(keyAccount)
	if !ok {
		return "", false
	}
	var e entry
	require.NoError(r.t, json.Unmarshal([]byte(v), &e))
	return e.Key, true
}

// await waits for the service to publish a state matching ok, and answers it.
func await(t *testing.T, s *Service, what string, ok func(connectors.State) bool) connectors.State {
	t.Helper()
	states, cancel := s.WatchState()
	defer cancel()
	deadline := time.After(testutil.Timeout)
	for {
		select {
		case st := <-states:
			if ok(st) {
				return st
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s; last state %+v", what, s.State())
		}
	}
}

func inPhase(p connectors.Phase) func(connectors.State) bool {
	return func(s connectors.State) bool { return s.Phase == p }
}

func failedWith(f connectors.Failure) func(connectors.State) bool {
	return func(s connectors.State) bool { return s.Phase == connectors.Failed && s.Failure == f }
}

// connect signs in through the following browser and waits for Connected.
func connect(t *testing.T, s *Service) connectors.State {
	t.Helper()
	require.NoError(t, s.Connect(t.Context()))
	return await(t, s, "Connected", inPhase(connectors.Connected))
}

// apiRequest is a request to the API base the service signs for.
func (r *rig) apiRequest(path string) *http.Request {
	r.t.Helper()
	req, err := http.NewRequestWithContext(r.t.Context(), http.MethodPost, r.openRouter.apiBase()+path, nil)
	require.NoError(r.t, err)
	return req
}

// signedKey signs a request with signer and answers its bearer.
func (r *rig) signedKey(signer interface{ Sign(*http.Request) error }) (string, error) {
	r.t.Helper()
	req := r.apiRequest("/chat/completions")
	if err := signer.Sign(req); err != nil {
		return "", err
	}
	return strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer "), nil
}
