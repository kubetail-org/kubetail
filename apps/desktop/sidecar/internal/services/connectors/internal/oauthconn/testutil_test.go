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

package oauthconn

import (
	"crypto/rand"
	"crypto/rsa"
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

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oauth"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// apiBase is the resource every test's service signs for.
const apiBase = "https://api.test/v1"

// The test provider: one that registers a client at the first sign-in, keeps a
// host id for the installation and requires one scope.
const (
	providerID     = "acme"
	refreshAccount = providerID + "-refresh-token"
	checkAccount   = providerID + "-keyring-check"
	firstClientID  = "first-client"
	planScope      = "plan.use"
	clientGone     = "invalid_client"
)

var scopes = []string{"openid", "email", "offline_access", planScope}

func testProvider(issuer string) Provider {
	return Provider{
		ID: providerID, Issuer: issuer, APIBaseURL: apiBase,
		Scopes: scopes, RequiredScope: planScope,
		CallbackPath: "/auth/callback", FirstClientID: firstClientID,
		NewData: func() map[string]string { return hostData("host-" + uuid.NewString()) },
		Authorize: func(data map[string]string, registering bool) url.Values {
			q := url.Values{"host": {data["hostID"]}}
			if registering {
				q.Set("registering", "yes")
			}
			return q
		},
		SessionEnders: []string{"invalid_grant", "refresh_token_reused", clientGone},
		ClientGone:    clientGone,
	}
}

// fixedClientID is the fixed-client provider's client.
const fixedClientID = "fixed-client"

// fixedProvider is a provider whose client is fixed in code and that signs no
// API, as the Kubetail sign-in is. It shares the test provider's ID, so the rig's
// accounts and row are the same.
func fixedProvider(issuer string) Provider {
	return Provider{
		ID: providerID, Issuer: issuer, ClientID: fixedClientID,
		Scopes:        []string{"openid", "offline_access", "email", "profile"},
		CallbackPath:  "/oauth/callback",
		SessionEnders: []string{"invalid_grant"},
	}
}

// newFixedRig is a rig under the fixed-client provider.
func newFixedRig(t *testing.T, opts ...option) *rig {
	t.Helper()
	return newRigWith(t, fixedProvider, opts...)
}

// installedSecret is the installed-app provider's client secret.
const installedSecret = "not-confidential"

// installedProvider is a provider whose endpoints are written out, whose fixed
// client carries a secret, and whose tokens sign an API that takes no RFC 8707
// resource, as Google's are. Its issuer serves no discovery.
func installedProvider(issuer string) Provider {
	return Provider{
		ID: providerID, ClientID: fixedClientID, ClientSecret: installedSecret,
		Endpoints: &oauth.Endpoints{
			Issuer: issuer, AuthURL: issuer + "/oauth/authorize", TokenURL: issuer + "/oauth/token",
			JWKSURL: issuer + "/jwks", RevocationURL: issuer + "/oauth/revoke",
		},
		APIBaseURL: apiBase, NoResource: true,
		Scopes: scopes, RequiredScope: planScope,
		CallbackPath: "/callback", SessionEnders: []string{"invalid_grant"},
	}
}

// hostData is the test provider's provider_data.
func hostData(id string) map[string]string { return map[string]string{"hostID": id} }

// fakeIssuer is a provider's auth server as the flow sees it: discovery, the
// authorize redirect, a token endpoint that rotates refresh tokens, the JWKS and
// revocation. Its fields steer the next answers; every request is recorded.
type fakeIssuer struct {
	t   *testing.T
	srv *httptest.Server
	key *rsa.PrivateKey

	mu sync.Mutex
	// issued is the client id a first sign-in registers.
	issued string
	// subject, email and name are the account the next sign-in verifies as.
	subject, email, name string
	// scope is what the token endpoint grants.
	scope string
	// accessTTL is each access token's lifetime.
	accessTTL time.Duration
	// deny, when set, is the error code the authorize redirect carries.
	deny string
	// exchangeError, when set, refuses every code exchange with that code.
	exchangeError string
	// badNonce mints ID tokens carrying a nonce nobody sent.
	badNonce bool
	// noRefresh answers a code exchange with no refresh token.
	noRefresh bool
	// callbackClientID, when set, is the client_id the redirect carries in place
	// of the one the issuer would.
	callbackClientID *string
	// refreshCode, when set, refuses every refresh with that error code.
	refreshCode string
	// revokeStatus answers every revocation; zero is 200.
	revokeStatus int
	// refuseRevoke, when set, is the one token whose revocation answers 500.
	refuseRevoke string
	// revokeEcho puts the token in a refused revocation's body.
	revokeEcho bool
	// hold, when set, parks the token endpoint until it is closed or sent on.
	hold chan struct{}
	// arrived is signalled each time a token request is parked on hold.
	arrived *testutil.Probe[url.Values]
	// holdDiscovery and holdRevoke, when set, park those requests until closed
	// or until the request ends; discovering and revoking signal each arrival.
	holdDiscovery, holdRevoke chan struct{}
	discovering, revoking     *testutil.Probe[struct{}]
	// discoveries counts the discovery requests.
	discoveries int

	minted     int
	nonces     map[string]string // code → nonce
	clientOf   map[string]string // code → client id the authorize named
	live       map[string]bool   // refresh tokens the issuer still honours
	authorizes []url.Values
	tokenForms []url.Values
	revoked    []url.Values
	tokens     []string
}

// issuerKey is the signing key every fake issuer shares: generating one is
// slow, and no test needs two issuers' keys to differ.
var issuerKey = sync.OnceValues(func() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 2048)
})

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	key, err := issuerKey()
	require.NoError(t, err)
	f := &fakeIssuer{
		t: t, key: key,
		issued: "client-issued", subject: "user-1", email: "ada@example.com", name: "Ada Lovelace",
		scope:     strings.Join(scopes, " "),
		accessTTL: time.Hour,
		nonces:    map[string]string{}, clientOf: map[string]string{}, live: map[string]bool{},
		arrived:     testutil.NewProbe[url.Values](8),
		discovering: testutil.NewProbe[struct{}](8),
		revoking:    testutil.NewProbe[struct{}](8),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", f.discovery)
	mux.HandleFunc("/oauth/authorize", f.authorize)
	mux.HandleFunc("/oauth/token", f.token)
	mux.HandleFunc("/jwks", f.jwks)
	mux.HandleFunc("/oauth/revoke", f.revoke)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIssuer) url() string { return f.srv.URL }

// park holds r until hold closes, signalling arrived first, and reports whether
// r outlived the wait. A nil hold parks nothing.
func park(r *http.Request, hold chan struct{}, arrived *testutil.Probe[struct{}]) bool {
	if hold == nil {
		return true
	}
	arrived.Fire(struct{}{})
	select {
	case <-hold:
		return true
	case <-r.Context().Done():
		return false
	}
}

func (f *fakeIssuer) discovery(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	hold := f.holdDiscovery
	f.discoveries++
	f.mu.Unlock()
	if !park(r, hold, f.discovering) {
		return
	}
	base := f.srv.URL
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"issuer":                 base,
		"authorization_endpoint": base + "/oauth/authorize",
		"token_endpoint":         base + "/oauth/token",
		"jwks_uri":               base + "/jwks",
		"revocation_endpoint":    base + "/oauth/revoke",
	})
}

// authorize redirects to the loopback as a provider that registers does: the code, the state, and
// the client id the sign-in runs under, a first sign-in's newly issued.
func (f *fakeIssuer) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f.mu.Lock()
	f.authorizes = append(f.authorizes, q)
	back := url.Values{"state": {q.Get("state")}}
	if f.deny != "" {
		back.Set("error", f.deny)
	} else {
		clientID := q.Get("client_id")
		if clientID == firstClientID {
			clientID = f.issued
		}
		f.minted++
		code := fmt.Sprintf("code-%d", f.minted)
		f.nonces[code] = q.Get("nonce")
		f.clientOf[code] = clientID
		back.Set("code", code)
		back.Set("client_id", clientID)
		if f.callbackClientID != nil {
			if *f.callbackClientID == "" {
				back.Del("client_id")
			} else {
				back.Set("client_id", *f.callbackClientID)
			}
		}
	}
	f.mu.Unlock()
	http.Redirect(w, r, q.Get("redirect_uri")+"?"+back.Encode(), http.StatusFound)
}

func (f *fakeIssuer) token(w http.ResponseWriter, r *http.Request) {
	require.NoError(f.t, r.ParseForm())
	form := r.PostForm
	f.mu.Lock()
	f.tokenForms = append(f.tokenForms, form)
	// A refresh token is checked as it arrives, so one revoked while the
	// request is held still answers.
	honoured := f.live[form.Get("refresh_token")]
	hold := f.hold
	f.mu.Unlock()
	if hold != nil {
		f.arrived.Fire(form)
		<-hold
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch form.Get("grant_type") {
	case "authorization_code":
		if f.exchangeError != "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":%q}`, f.exchangeError)
			return
		}
		code := form.Get("code")
		clientID, ok := f.clientOf[code]
		if !ok || clientID != form.Get("client_id") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		delete(f.clientOf, code)
		nonce := f.nonces[code]
		if f.badNonce {
			nonce = "not-the-one-sent"
		}
		if f.noRefresh {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "access-token-bare", "id_token": f.idToken(clientID, nonce),
				"token_type": "bearer", "expires_in": int(f.accessTTL / time.Second), "scope": f.scope,
			})
			return
		}
		f.answerLocked(w, clientID, nonce)
	case "refresh_token":
		if f.refreshCode != "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":%q}`, f.refreshCode)
			return
		}
		old := form.Get("refresh_token")
		if !honoured {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"refresh_token_reused"}`))
			return
		}
		delete(f.live, old)
		f.answerLocked(w, form.Get("client_id"), "")
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

// answerLocked mints a token set: a new access and refresh token, and an ID
// token for clientID carrying nonce.
func (f *fakeIssuer) answerLocked(w http.ResponseWriter, clientID, nonce string) {
	f.minted++
	refresh := fmt.Sprintf("refresh-token-%04d", f.minted)
	access := fmt.Sprintf("access-token-%04d", f.minted)
	id := f.idToken(clientID, nonce)
	f.live[refresh] = true
	f.tokens = append(f.tokens, refresh, access, id)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":  access,
		"refresh_token": refresh,
		"id_token":      id,
		"token_type":    "bearer",
		"expires_in":    int(f.accessTTL / time.Second),
		"scope":         f.scope,
	})
}

func (f *fakeIssuer) idToken(aud, nonce string) string {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k"))
	require.NoError(f.t, err)
	raw, err := jwt.Signed(signer).Claims(jwt.Claims{
		Issuer: f.srv.URL, Subject: f.subject, Audience: jwt.Audience{aud},
		Expiry: jwt.NewNumericDate(time.Now().Add(time.Hour)), IssuedAt: jwt.NewNumericDate(time.Now()),
	}).Claims(map[string]any{"email": f.email, "name": f.name, "nonce": nonce}).Serialize()
	require.NoError(f.t, err)
	return raw
}

func (f *fakeIssuer) jwks(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: f.key.Public(), KeyID: "k", Algorithm: "RS256", Use: "sig"}}})
}

func (f *fakeIssuer) revoke(w http.ResponseWriter, r *http.Request) {
	require.NoError(f.t, r.ParseForm())
	f.mu.Lock()
	hold := f.holdRevoke
	f.mu.Unlock()
	if !park(r, hold, f.revoking) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked = append(f.revoked, r.PostForm)
	if f.refuseRevoke != "" && r.PostForm.Get("token") == f.refuseRevoke {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if f.revokeStatus != 0 {
		w.WriteHeader(f.revokeStatus)
		if f.revokeEcho {
			_, _ = w.Write([]byte("cannot revoke " + r.PostForm.Get("token")))
		}
		return
	}
	delete(f.live, r.PostForm.Get("token"))
}

// set runs fn under the issuer's lock, to steer its next answers.
func (f *fakeIssuer) set(fn func(f *fakeIssuer)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeIssuer) authorizeQueries() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.authorizes...)
}

func (f *fakeIssuer) forms(grant string) []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []url.Values
	for _, form := range f.tokenForms {
		if form.Get("grant_type") == grant {
			out = append(out, form)
		}
	}
	return out
}

// mintedTokens is every token the issuer has handed out.
func (f *fakeIssuer) mintedTokens() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.tokens...)
}

// revokedTokens is every token a revocation named, in order.
func (f *fakeIssuer) revokedTokens() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, form := range f.revoked {
		out = append(out, form.Get("token"))
	}
	return out
}

// awaitRevoked waits for a revocation of token.
func (f *fakeIssuer) awaitRevoked(t *testing.T, token string) {
	t.Helper()
	require.Eventually(t, func() bool {
		return slices.Contains(f.revokedTokens(), token)
	}, testutil.Timeout, time.Millisecond, "a revocation of %s", token)
}

// fakeKeyring is the OS keyring in memory, whose writes and deletes fail on
// request.
type fakeKeyring struct {
	mu        sync.Mutex
	entries   map[string]string
	setErr    map[string]error
	deleteErr map[string]error
	getErr    error
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

func (k *fakeKeyring) failSet(account string, err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.setErr[account] = err
}

func (k *fakeKeyring) failDelete(account string, err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.deleteErr[account] = err
}

var errBoom = errors.New("boom")

// following is a browser that follows the authorize URL, as a user who signs
// in would: the issuer's redirect lands on the loopback.
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

func (p *parked) opened() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.urls)
}

// rig is a service over a fake issuer and keyring and an app.db in a temporary
// directory, rebuilt over the same stores by restart.
type rig struct {
	t       *testing.T
	issuer  *fakeIssuer
	keyring *fakeKeyring
	// calls is keyring as the service reaches it: recorded, and held on request.
	calls *oskeyringtest.Keyring
	// dir holds app.db.
	dir      string
	db       *appdb.DB
	provider func(issuer string) Provider
	opts     []option
	svc      *Service
}

func newRig(t *testing.T, opts ...option) *rig {
	t.Helper()
	return newRigWith(t, testProvider, opts...)
}

// newRigWith is a rig whose service runs under the provider p builds.
func newRigWith(t *testing.T, p func(issuer string) Provider, opts ...option) *rig {
	t.Helper()
	r := &rig{t: t, issuer: newFakeIssuer(t), keyring: newFakeKeyring(), dir: t.TempDir(), provider: p, opts: opts}
	r.calls = oskeyringtest.Wrap(r.keyring)
	r.db = appdbtest.OpenIn(t, r.dir)
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
	return startService(r.t, Config{Provider: r.provider(r.issuer.url()), DB: r.db, Keyring: r.calls}, opts...)
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

// seed writes the row and the keyring's entry a launch finds. An empty refresh
// writes no entry; the entry names the session entrySession.
func (r *rig) seed(reg registration, refresh, entrySession string) {
	r.t.Helper()
	store, err := openRegistration(r.db, providerID)
	require.NoError(r.t, err)
	require.NoError(r.t, store.save(reg))
	require.NoError(r.t, store.close())
	if refresh != "" {
		require.NoError(r.t, saveEntry(r.keyring, refreshAccount, entrySession, refresh))
	}
}

// savedRegistration is what the row holds now.
func savedRegistration(t *testing.T, r *rig) registration {
	t.Helper()
	store, err := openRegistration(r.db, providerID)
	require.NoError(t, err)
	require.NoError(t, store.load())
	require.NoError(t, store.close())
	return store.get()
}

// storedRefresh is the refresh token the keyring's entry holds, and whether the
// account holds anything.
func storedRefresh(t *testing.T, r *rig) (string, bool) {
	t.Helper()
	e, found, err := parseEntry(r.keyring.Get(refreshAccount))
	require.NoError(t, err)
	return e.Token, found
}

// restart is a new service over the same stores, as the next launch is.
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
	require.NoError(t, s.Connect(t.Context(), false))
	return await(t, s, "Connected", inPhase(connectors.Connected))
}

// apiRequest is a request to the resource the service signs for.
func apiRequest(t *testing.T, path string) *http.Request {
	t.Helper()
	r, err := http.NewRequestWithContext(t.Context(), http.MethodPost, apiBase+path, nil)
	require.NoError(t, err)
	return r
}

// signedToken signs a request with signer and answers its bearer.
func signedToken(t *testing.T, signer interface{ Sign(*http.Request) error }) (string, error) {
	t.Helper()
	r := apiRequest(t, "/responses")
	if err := signer.Sign(r); err != nil {
		return "", err
	}
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), nil
}
