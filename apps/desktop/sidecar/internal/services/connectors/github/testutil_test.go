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

package github

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

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

const testClientID = "Ov23liTestClient"

// fakeGitHub is GitHub as the device flow sees it: the device code endpoint,
// the token endpoint, which answers each poll from a script and then a token,
// and the API's /user. Its fields steer the next answers; every request is
// recorded.
type fakeGitHub struct {
	t   *testing.T
	srv *httptest.Server

	mu sync.Mutex
	// code, when set, answers the device code request in place of a minted one.
	code http.HandlerFunc
	// verificationURI is the page the code answer names; empty is the fake's own.
	verificationURI string
	// polls is what the token endpoint answers before it grants a token: each
	// an error code, taken in turn.
	polls []string
	// pollAnswer, when set, answers every poll in place of the script.
	pollAnswer http.HandlerFunc
	// slowDownInterval is the interval a slow_down answer names; zero names none.
	slowDownInterval int
	// userStatus answers every /user; zero is 200 with login, or userBody when set.
	userStatus int
	userBody   string
	// dropUser closes /user's connection unanswered.
	dropUser bool
	login    string
	// holdPoll, when set, parks the poll that would grant a token until it is closed.
	holdPoll chan struct{}
	// holdUser, when set, parks /user until it is closed.
	holdUser chan struct{}
	// arrived is signalled each time a request is parked.
	arrived *testutil.Probe[string]

	// prefix makes this fake's tokens its own: the redactor is the process's.
	prefix     string
	minted     int
	codes      []url.Values
	tokenPolls []url.Values
	users      []string // the bearer each /user carried
	tokens     []string
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{t: t, prefix: strings.ReplaceAll(uuid.NewString(), "-", ""), login: "octocat", arrived: testutil.NewProbe[string](8)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/device/code", f.deviceCode)
	mux.HandleFunc("POST /login/oauth/access_token", f.accessToken)
	mux.HandleFunc("GET /api/user", f.user)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) authURL() string { return f.srv.URL }
func (f *fakeGitHub) apiURL() string  { return f.srv.URL + "/api" }

// park waits on hold, when set, until it is closed.
func (f *fakeGitHub) park(hold chan struct{}, what string) {
	if hold != nil {
		f.arrived.Fire(what)
		<-hold
	}
}

func (f *fakeGitHub) deviceCode(w http.ResponseWriter, r *http.Request) {
	require.NoError(f.t, r.ParseForm())
	f.mu.Lock()
	f.codes = append(f.codes, r.PostForm)
	answer, page := f.code, f.verificationURI
	f.mu.Unlock()
	if answer != nil {
		answer(w, r)
		return
	}
	if page == "" {
		page = f.srv.URL + "/login/device"
	}
	writeJSON(w, map[string]any{
		"device_code": "device-" + f.prefix, "user_code": "WDJB-MJHT", "verification_uri": page,
		"expires_in": 900, "interval": 5,
	})
}

func (f *fakeGitHub) accessToken(w http.ResponseWriter, r *http.Request) {
	require.NoError(f.t, r.ParseForm())
	f.mu.Lock()
	f.tokenPolls = append(f.tokenPolls, r.PostForm)
	answer := f.pollAnswer
	var next string
	if len(f.polls) > 0 {
		next, f.polls = f.polls[0], f.polls[1:]
	}
	slow, hold := f.slowDownInterval, f.holdPoll
	f.mu.Unlock()
	switch {
	case answer != nil:
		answer(w, r)
	case next == "slow_down" && slow != 0:
		writeJSON(w, map[string]any{"error": next, "interval": slow})
	case next != "":
		writeJSON(w, map[string]any{"error": next})
	default:
		f.park(hold, "poll")
		f.mu.Lock()
		f.minted++
		// Not gho_: the redactor blanks that shape whether registered or not.
		token := fmt.Sprintf("test_%s%04d", f.prefix, f.minted)
		f.tokens = append(f.tokens, token)
		f.mu.Unlock()
		writeJSON(w, map[string]any{"access_token": token, "token_type": "bearer", "scope": "repo,read:org,workflow"})
	}
}

func (f *fakeGitHub) user(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.users = append(f.users, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	status, login, body, drop, hold := f.userStatus, f.login, f.userBody, f.dropUser, f.holdUser
	f.mu.Unlock()
	f.park(hold, "user")
	if drop {
		conn, _, err := w.(http.Hijacker).Hijack()
		require.NoError(f.t, err)
		conn.Close()
		return
	}
	if status != 0 && status != http.StatusOK {
		w.WriteHeader(status)
		return
	}
	if body != "" {
		_, _ = w.Write([]byte(body))
		return
	}
	writeJSON(w, map[string]any{"login": login, "id": 1})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// set runs fn under the fake's lock, to steer its next answers.
func (f *fakeGitHub) set(fn func(f *fakeGitHub)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeGitHub) codeRequests() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.codes...)
}

func (f *fakeGitHub) pollRequests() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.tokenPolls...)
}

func (f *fakeGitHub) userRequests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.users...)
}

// mintedTokens is every token the token endpoint has granted.
func (f *fakeGitHub) mintedTokens() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.tokens...)
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

// fakeBrowser records each URL it was opened on, and fails when told to.
type fakeBrowser struct {
	mu   sync.Mutex
	urls []string
	err  error
}

func (b *fakeBrowser) open(rawURL string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.urls = append(b.urls, rawURL)
	return b.err
}

func (b *fakeBrowser) opened() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.urls...)
}

// rig is a service over a fake GitHub, browser and keyring, rebuilt over the
// same keyring by restart. Its pace turns GitHub's seconds into milliseconds
// and records each wait.
type rig struct {
	t       *testing.T
	github  *fakeGitHub
	keyring *fakeKeyring
	// calls is keyring as the service reaches it: recorded, and held on request.
	calls   *oskeyringtest.Keyring
	browser *fakeBrowser
	// checkEnded fires each time a token check ends, attemptEnded each time a
	// sign-in's polling and its outcome do.
	checkEnded   *testutil.Probe[struct{}]
	attemptEnded *testutil.Probe[struct{}]
	clientID     string
	opts         []option
	svc          *Service

	mu    sync.Mutex
	waits []int
}

func newRig(t *testing.T, opts ...option) *rig {
	t.Helper()
	r := &rig{
		t: t, github: newFakeGitHub(t), keyring: newFakeKeyring(), browser: &fakeBrowser{},
		checkEnded: testutil.NewProbe[struct{}](4), attemptEnded: testutil.NewProbe[struct{}](4),
		clientID: testClientID, opts: opts,
	}
	r.calls = oskeyringtest.Wrap(r.keyring)
	r.svc = r.build()
	return r
}

func (r *rig) config() Config {
	return Config{ClientID: r.clientID, AuthURL: r.github.authURL(), APIURL: r.github.apiURL(), Keyring: r.calls}
}

func (r *rig) pace(seconds int) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.waits = append(r.waits, seconds)
	return time.Duration(seconds) * time.Millisecond
}

// paced is every span of GitHub's seconds the service timed: the code's
// lifetime, then each wait between polls.
func (r *rig) paced() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), r.waits...)
}

func (r *rig) build() *Service {
	r.t.Helper()
	s := r.start()
	awaitRestored(r.t, s)
	return s
}

// awaitRestored waits for s to publish what the keyring held.
func awaitRestored(t *testing.T, s *Service) {
	t.Helper()
	testutil.Wait(t, s.Restored(), "the restore")
}

// start is build without the wait for the restore.
func (r *rig) start() *Service {
	r.t.Helper()
	opts := append([]option{
		withBrowser(r.browser.open), withPace(r.pace),
		withCheckEnded(func() { r.checkEnded.Fire(struct{}{}) }),
		withAttemptEnded(func() { r.attemptEnded.Fire(struct{}{}) }),
	}, r.opts...)
	s, err := newWithOptions(r.config(), opts...)
	require.NoError(r.t, err)
	r.t.Cleanup(s.Close)
	return s
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

// stored is the keyring's entry, and whether the account holds anything.
func (r *rig) stored() (entry, bool) {
	r.t.Helper()
	v, ok := r.keyring.entry(tokenAccount)
	if !ok {
		return entry{}, false
	}
	var e entry
	require.NoError(r.t, json.Unmarshal([]byte(v), &e))
	return e, true
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

// connect signs in, GitHub granting at the first poll, and waits for Connected.
func connect(t *testing.T, s *Service) connectors.State {
	t.Helper()
	require.NoError(t, s.Connect(t.Context()))
	return await(t, s, "Connected", inPhase(connectors.Connected))
}

// storedEntry is an entry as the keyring holds it.
func storedEntry(token, login string) string {
	data, _ := json.Marshal(entry{V: entryVersion, Token: token, Login: login})
	return string(data)
}
