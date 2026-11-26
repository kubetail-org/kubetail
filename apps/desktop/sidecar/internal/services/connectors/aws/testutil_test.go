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

package aws

import (
	"cmp"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

var errBoom = errors.New("boom")

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

// rig is a service over a fake AWS sign-in and keyring, rebuilt over the same
// keyring by restart.
type rig struct {
	t       *testing.T
	signin  *fakeSignin
	keyring *fakeKeyring
	// calls is keyring as the service reaches it: recorded, and held on request.
	calls *oskeyringtest.Keyring
	opts  []option
	svc   *Service
}

func newRig(t *testing.T, opts ...option) *rig {
	t.Helper()
	r := &rig{t: t, signin: newFakeSignin(t), keyring: newFakeKeyring(), opts: opts}
	r.calls = oskeyringtest.Wrap(r.keyring)
	r.svc = r.build()
	return r
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
	opts := append([]option{withBrowser(following)}, r.opts...)
	s, err := newWithOptions(Config{SigninURL: r.signin.url(), Keyring: r.calls}, opts...)
	require.NoError(r.t, err)
	r.t.Cleanup(s.Close)
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

func (r *rig) storeSession(e entry) {
	r.t.Helper()
	require.NoError(r.t, saveEntry(r.keyring, e))
}

// stored is the keyring's entry, and whether the account holds one.
func (r *rig) stored() (entry, bool) {
	r.t.Helper()
	e, found, err := parseEntry(r.keyring.Get(sessionAccount))
	require.NoError(r.t, err)
	return e, found
}

func jsonEntry(t *testing.T, e entry) string {
	t.Helper()
	data, err := json.Marshal(e)
	require.NoError(t, err)
	return string(data)
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

// following is a browser that gets rawURL on a goroutine. On the authorize URL
// it is a user who signs in: the fake's redirect lands on the loopback.
func following(rawURL string) error {
	go func() {
		resp, err := http.Get(rawURL)
		if err == nil {
			resp.Body.Close()
		}
	}()
	return nil
}

// declining is a browser whose user declines on AWS's page: AWS redirects with
// access_denied.
func declining(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	q := u.Query()
	return following(q.Get("redirect_uri") + "?" + url.Values{"error": {"access_denied"}, "state": {q.Get("state")}}.Encode())
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

// fakeSignin is AWS's sign-in as the connector sees it: the authorize page,
// which redirects to the loopback with a code, and the token endpoint, which
// answers credentials for a code whose verifier matches its challenge, or for
// a refresh token, each under a valid DPoP proof. A refresh token is spent
// once, and only the key it was issued under may spend it. Its fields steer
// the next answers; every request is recorded.
type fakeSignin struct {
	t   *testing.T
	srv *httptest.Server

	mu sync.Mutex
	// subject is the ID token's sub; empty answers testARN.
	subject string
	// tokenErr, when set, answers every token request.
	tokenErr *apiError
	// answer, when set, answers every token request in place of the fake.
	answer http.HandlerFunc
	// hold, when set, parks each token request until it is closed.
	hold chan struct{}
	// arrived is signalled each time a token request is parked on hold.
	arrived *testutil.Probe[string]
	// expiresIn is the credentials' lifetime; zero answers 900.
	expiresIn int32

	// prefix makes this fake's tokens its own: the redactor is the process's.
	prefix     string
	minted     int
	codes      map[string]url.Values // code → its authorize request
	refreshes  map[string]string     // live refresh token → the key it is bound to
	authorizes []url.Values
	bodies     []map[string]string
	agents     []string // each token request's User-Agent
	keyIDs     []string // every access key id handed out
	secrets    []string // every refresh token, secret key and session token handed out
}

// apiError is an error AWS answers: its status, type and OAuth code.
type apiError struct {
	status   int
	typ      string
	code     string
	dropConn bool
}

func newFakeSignin(t *testing.T) *fakeSignin {
	t.Helper()
	f := &fakeSignin{
		t: t, prefix: uuid.NewString(), arrived: testutil.NewProbe[string](8),
		codes: map[string]url.Values{}, refreshes: map[string]string{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/authorize", f.authorize)
	mux.HandleFunc("POST /v1/token", f.token)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSignin) url() string { return f.srv.URL }

// authorize redirects to redirect_uri with a code and the state, as AWS does.
func (f *fakeSignin) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f.mu.Lock()
	f.authorizes = append(f.authorizes, q)
	f.minted++
	code := fmt.Sprintf("code-%d", f.minted)
	f.codes[code] = q
	f.mu.Unlock()
	redirect := q.Get("redirect_uri") + "?" + url.Values{"code": {code}, "state": {q.Get("state")}}.Encode()
	http.Redirect(w, r, redirect, http.StatusFound)
}

func (f *fakeSignin) token(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
	f.mu.Lock()
	f.bodies = append(f.bodies, body)
	f.agents = append(f.agents, r.Header.Get("User-Agent"))
	fail, hold, answer := f.tokenErr, f.hold, f.answer
	f.mu.Unlock()
	if hold != nil {
		f.arrived.Fire(body["grantType"])
		<-hold
	}
	if fail != nil {
		f.answerError(w, *fail)
		return
	}
	if answer != nil {
		answer(w, r)
		return
	}
	key, ok := proofKey(r.Header.Get("DPoP"), "http://"+r.Host+r.URL.Path)
	if !ok || body["clientId"] != ClientID {
		f.answerError(w, apiError{status: http.StatusBadRequest, typ: "ValidationException", code: "INVALID_REQUEST"})
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch body["grantType"] {
	case "authorization_code":
		q, known := f.codes[body["code"]]
		if !known || q.Get("code_challenge_method") != "SHA-256" || q.Get("redirect_uri") != body["redirectUri"] ||
			oauth2.S256ChallengeFromVerifier(body["codeVerifier"]) != q.Get("code_challenge") {
			f.answerError(w, apiError{status: http.StatusBadRequest, typ: "InvalidGrantException", code: "invalid_grant"})
			return
		}
		delete(f.codes, body["code"])
		f.answerTokens(w, key, true)
	case "refresh_token":
		if bound, live := f.refreshes[body["refreshToken"]]; !live || bound != key {
			f.answerError(w, apiError{status: http.StatusBadRequest, typ: "InvalidGrantException", code: "invalid_grant"})
			return
		}
		delete(f.refreshes, body["refreshToken"])
		f.answerTokens(w, key, false)
	default:
		f.answerError(w, apiError{status: http.StatusBadRequest, typ: "ValidationException", code: "unsupported_grant_type"})
	}
}

// answerTokens mints credentials and a refresh token bound to key (f.mu held).
func (f *fakeSignin) answerTokens(w http.ResponseWriter, key string, withID bool) {
	f.minted++
	n := f.minted
	refresh := fmt.Sprintf("refresh-%s-%d", f.prefix, n)
	keyID := fmt.Sprintf("ASIA%s%d", f.prefix, n)
	f.refreshes[refresh] = key
	secret, sessionToken := fmt.Sprintf("secret-%s-%d", f.prefix, n), fmt.Sprintf("session-%s-%d", f.prefix, n)
	f.keyIDs = append(f.keyIDs, keyID)
	f.secrets = append(f.secrets, refresh, secret, sessionToken)
	expiresIn := f.expiresIn
	if expiresIn == 0 {
		expiresIn = 900
	}
	answer := map[string]any{
		"accessToken": map[string]string{
			"accessKeyId": keyID, "secretAccessKey": secret, "sessionToken": sessionToken,
		},
		"expiresIn": expiresIn, "refreshToken": refresh, "tokenType": "aws_sigv4",
	}
	if withID {
		answer["idToken"] = unsignedJWT(map[string]string{"sub": cmp.Or(f.subject, testARN)})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(answer)
}

func (f *fakeSignin) answerError(w http.ResponseWriter, e apiError) {
	if e.dropConn {
		conn, _, err := w.(http.Hijacker).Hijack()
		require.NoError(f.t, err)
		conn.Close()
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.status)
	_ = json.NewEncoder(w).Encode(map[string]string{"__type": e.typ, "error": e.code, "message": "refused"})
}

// set runs fn under the fake's lock, to steer its next answers.
func (f *fakeSignin) set(fn func(f *fakeSignin)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeSignin) authorizeQueries() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.authorizes...)
}

func (f *fakeSignin) tokenBodies() []map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]string(nil), f.bodies...)
}

func (f *fakeSignin) userAgents() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.agents...)
}

func (f *fakeSignin) mintedSecrets() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.secrets...)
}

func (f *fakeSignin) accessKeyIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.keyIDs...)
}

// verifyProof checks a DPoP proof as AWS does: a dpop+jwt whose ES256
// signature verifies under the P-256 key its own header carries. It answers
// that key and the proof's claims.
func verifyProof(proof string) (*ecdsa.PublicKey, dpopClaims, error) {
	var claims dpopClaims
	tok, err := jwt.ParseSigned(proof, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		return nil, claims, err
	}
	header := tok.Headers[0]
	if header.ExtraHeaders[jose.HeaderType] != "dpop+jwt" || header.JSONWebKey == nil {
		return nil, claims, errors.New("no dpop+jwt carrying its key")
	}
	pub, ok := header.JSONWebKey.Key.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		return nil, claims, errors.New("the proof's key is no P-256 public key")
	}
	return pub, claims, tok.Claims(pub, &claims)
}

// proofKey names the key that signed a valid proof for a POST to url.
func proofKey(proof, url string) (string, bool) {
	pub, claims, err := verifyProof(proof)
	if err != nil || claims.Method != http.MethodPost || claims.URL != url || claims.ID == "" {
		return "", false
	}
	point, err := pub.Bytes()
	if err != nil {
		return "", false
	}
	return hex.EncodeToString(point), true
}

// unsignedJWT is a JWT carrying claims, its signature a placeholder: the
// connector reads an ID token it was handed by the token endpoint itself.
func unsignedJWT(claims map[string]string) string {
	head := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES384","typ":"JWT"}`))
	data, _ := json.Marshal(claims)
	return head + "." + base64.RawURLEncoding.EncodeToString(data) + ".c2ln"
}

// connect signs in through the following browser and waits for Connected.
func connect(t *testing.T, s *Service) connectors.State {
	t.Helper()
	require.NoError(t, s.Connect(t.Context()))
	return await(t, s, "Connected", inPhase(connectors.Connected))
}

// clock is a time a test moves by hand.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Unix(1_800_000_000, 0)} }

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}
