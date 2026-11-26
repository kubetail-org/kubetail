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

package oauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
)

// testVerifier is a sample PKCE code verifier. The protocol layer treats the
// verifier as caller-supplied (the auth package generates the real one),
// so these tests just need any non-empty value.
const testVerifier = "test-verifier-1234567890"

// s256Challenge is the test oracle for the PKCE code challenge of a verifier —
// base64url(SHA256(verifier)), no padding (RFC 7636). Production builds the
// challenge via oauth2.S256ChallengeOption inside AuthCodeURL; this independent
// computation lets TestAuthCodeURL assert that wiring produced the right value.
func s256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// AuthCodeURL carries client_id, the per-flow redirect_uri (the loopback
// listener's ephemeral 127.0.0.1 address — exactly one), code_challenge (S256),
// code_challenge_method, and state.
func TestAuthCodeURL(t *testing.T) {
	c := NewClient(Config{
		ClientID: "kubetail-desktop",
		AuthURL:  "https://oauth.kubetail.com/oauth2/auth",
		TokenURL: "https://oauth.kubetail.com/oauth2/token",
		Scopes:   []string{"openid", "offline_access"},
	})
	redirect := "http://127.0.0.1:49217/oauth/callback"
	raw := c.AuthCodeURL("xyz-state", testVerifier, redirect, nil)

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := u.Query()
	if q.Get("client_id") != "kubetail-desktop" {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	if got := q["redirect_uri"]; len(got) != 1 || got[0] != redirect {
		t.Errorf("redirect_uri = %v, want exactly [%q]", got, redirect)
	}
	if q.Get("state") != "xyz-state" {
		t.Errorf("state = %q", q.Get("state"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q", q.Get("code_challenge_method"))
	}
	if q.Get("code_challenge") != s256Challenge(testVerifier) {
		t.Errorf("code_challenge mismatch")
	}
}

// stubTokenServer serves an OAuth token endpoint returning a fixed token bundle.
func stubTokenServer(t *testing.T, idTok string, wantGrant string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != wantGrant {
			t.Errorf("grant_type = %q, want %q", got, wantGrant)
		}
		if wantGrant == "authorization_code" && r.Form.Get("code_verifier") == "" {
			t.Errorf("missing code_verifier on auth-code exchange")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-123",
			"refresh_token": "refresh-123",
			"id_token":      idTok,
			"token_type":    "bearer",
			"expires_in":    3600,
		})
	}))
}

// Exchange posts code + verifier and parses the token bundle.
func TestExchange(t *testing.T) {
	ts := stubTokenServer(t, "id-tok", "authorization_code")
	defer ts.Close()

	c := NewClient(Config{
		ClientID: "kubetail-desktop",
		TokenURL: ts.URL,
	})
	tok, err := c.Exchange(context.Background(), "the-code", testVerifier, "")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if tok.AccessToken != "access-123" || tok.RefreshToken != "refresh-123" || tok.IDToken != "id-tok" {
		t.Fatalf("token: %+v", tok)
	}
	if tok.Expiry.IsZero() {
		t.Fatalf("expiry not set")
	}
}

// Refresh exchanges the static refresh token for a fresh bundle.
func TestRefresh(t *testing.T) {
	ts := stubTokenServer(t, "id-tok-2", "refresh_token")
	defer ts.Close()

	c := NewClient(Config{
		ClientID: "kubetail-desktop",
		TokenURL: ts.URL,
	})
	tok, err := c.Refresh(context.Background(), "refresh-123")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if tok.AccessToken != "access-123" {
		t.Fatalf("access token: %q", tok.AccessToken)
	}
}

// Revoke POSTs the token + client_id (public client, no secret) to the
// revocation endpoint and treats a 2xx as success.
func TestRevoke(t *testing.T) {
	var gotToken, gotClient, gotCT string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		gotToken = r.Form.Get("token")
		gotClient = r.Form.Get("client_id")
		gotCT = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := NewClient(Config{
		ClientID:      "kubetail-desktop",
		RevocationURL: ts.URL,
	})
	if err := c.Revoke(context.Background(), "refresh-123"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if gotToken != "refresh-123" || gotClient != "kubetail-desktop" {
		t.Fatalf("posted token=%q client_id=%q", gotToken, gotClient)
	}
	if gotCT != "application/x-www-form-urlencoded" {
		t.Fatalf("content-type = %q", gotCT)
	}
}

// A non-2xx revocation response surfaces an error.
func TestRevokeServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer ts.Close()

	c := NewClient(Config{ClientID: "x", RevocationURL: ts.URL})
	if err := c.Revoke(context.Background(), "t"); err == nil {
		t.Fatal("want error on 500, got nil")
	}
}

// Revoke without a configured endpoint errors rather than silently no-op'ing.
func TestRevokeNoEndpoint(t *testing.T) {
	c := NewClient(Config{ClientID: "x"})
	if err := c.Revoke(context.Background(), "t"); err == nil {
		t.Fatal("want error when no revocation endpoint is configured")
	}
}

// --- ID-token verification helpers ---

type idTokenServer struct {
	srv      *httptest.Server
	signKey  *rsa.PrivateKey
	otherKey *rsa.PrivateKey
	issuer   string
}

func newIDTokenServer(t *testing.T) *idTokenServer {
	t.Helper()
	signKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: signKey.Public(), KeyID: "test-key", Algorithm: "RS256", Use: "sig",
	}}}
	mux := http.NewServeMux()
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	})
	its := &idTokenServer{signKey: signKey, otherKey: otherKey}
	its.srv = httptest.NewServer(mux)
	its.issuer = its.srv.URL
	return its
}

func (its *idTokenServer) sign(t *testing.T, key *rsa.PrivateKey, aud string, extra map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"),
	)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	claims := jwt.Claims{
		Issuer:   its.issuer,
		Subject:  "user-1",
		Audience: jwt.Audience{aud},
		Expiry:   jwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt: jwt.NewNumericDate(time.Now()),
	}
	raw, err := jwt.Signed(signer).Claims(claims).Claims(extra).Serialize()
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return raw
}

// VerifyNonce decodes sub/email/name from a properly signed token.
func TestVerifyValidToken(t *testing.T) {
	its := newIDTokenServer(t)
	defer its.srv.Close()

	c := NewClient(Config{
		ClientID: "kubetail-desktop",
		Issuer:   its.issuer,
		JWKSURL:  its.srv.URL + "/jwks",
	})
	raw := its.sign(t, its.signKey, "kubetail-desktop", map[string]any{
		"email": "a@example.com", "name": "Ada", "nonce": "n-1",
	})
	id, err := c.VerifyNonce(context.Background(), raw, "n-1")
	if err != nil {
		t.Fatalf("VerifyNonce: %v", err)
	}
	if id.UserID != "user-1" || id.Email != "a@example.com" || id.Name != "Ada" {
		t.Fatalf("identity: %+v", id)
	}
}

// Token.Valid requires a present access token AND a now strictly before expiry
// minus the leeway window — so a token handed out stays usable through the
// request that consumes it.
func TestTokenValid(t *testing.T) {
	// epoch is a fixed reference time for the deterministic cases below.
	epoch := time.Unix(1_700_000_000, 0)
	tests := []struct {
		name string
		tok  Token
		want bool
	}{
		{
			name: "valid well before expiry",
			tok:  Token{AccessToken: "a", Expiry: epoch.Add(time.Hour)},
			want: true,
		},
		{
			name: "no access token is never valid",
			tok:  Token{Expiry: epoch.Add(time.Hour)},
			want: false,
		},
		{
			name: "already expired",
			tok:  Token{AccessToken: "a", Expiry: epoch.Add(-time.Minute)},
			want: false,
		},
		{
			name: "inside the leeway window counts as expired",
			tok:  Token{AccessToken: "a", Expiry: epoch.Add(expiryLeeway - time.Second)},
			want: false,
		},
		{
			name: "exactly at the leeway boundary is not valid",
			tok:  Token{AccessToken: "a", Expiry: epoch.Add(expiryLeeway)},
			want: false,
		},
		{
			name: "just outside the leeway window is valid",
			tok:  Token{AccessToken: "a", Expiry: epoch.Add(expiryLeeway + time.Second)},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tok.Valid(epoch); got != tt.want {
				t.Fatalf("Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

// VerifyNonce rejects a token signed by a key absent from the JWKS.
func TestVerifyBadSignature(t *testing.T) {
	its := newIDTokenServer(t)
	defer its.srv.Close()

	c := NewClient(Config{
		ClientID: "kubetail-desktop",
		Issuer:   its.issuer,
		JWKSURL:  its.srv.URL + "/jwks",
	})
	raw := its.sign(t, its.otherKey, "kubetail-desktop", map[string]any{"nonce": "n-1"}) // wrong key
	if _, err := c.VerifyNonce(context.Background(), raw, "n-1"); err == nil {
		t.Fatal("expected verification to fail for a bad signature")
	}
}

// A properly signed token whose identity claim has the wrong type is refused:
// the signature vouches for the claims, not for their shape.
func TestVerifyRefusesAClaimOfTheWrongType(t *testing.T) {
	its := newIDTokenServer(t)
	defer its.srv.Close()

	c := NewClient(Config{
		ClientID: "kubetail-desktop",
		Issuer:   its.issuer,
		JWKSURL:  its.srv.URL + "/jwks",
	})
	raw := its.sign(t, its.signKey, "kubetail-desktop", map[string]any{"email": 42, "nonce": "n-1"})
	if _, err := c.VerifyNonce(context.Background(), raw, "n-1"); err == nil {
		t.Fatal("expected VerifyNonce to refuse a numeric email claim")
	}
}

// VerifyNonce without a JWKS URL has no keys to check against, so it refuses rather
// than accepting the token unverified.
func TestVerifyWithoutAVerifierRefuses(t *testing.T) {
	c := NewClient(Config{ClientID: "x"})
	if _, err := c.VerifyNonce(context.Background(), "any.token.here", "n-1"); err == nil {
		t.Fatal("want an error when no verifier is configured")
	}
}

// A token endpoint that rejects the grant fails both exchanges; neither returns
// a half-built Token.
func TestTokenEndpointFailuresSurface(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
	}))
	defer ts.Close()

	c := NewClient(Config{ClientID: "kubetail-desktop", TokenURL: ts.URL})
	if tok, err := c.Exchange(context.Background(), "code", testVerifier, ""); err == nil {
		t.Errorf("Exchange returned %+v, want an error", tok)
	}
	if tok, err := c.Refresh(context.Background(), "refresh-123"); err == nil {
		t.Errorf("Refresh returned %+v, want an error", tok)
	}
}

// A refresh response that carries no new refresh token keeps the one we sent:
// Hydra omits it when the grant's token is static, and losing it would sign the
// user out at the next refresh.
func TestRefreshKeepsAStaticRefreshToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"access-456","token_type":"bearer","expires_in":3600}`)
	}))
	defer ts.Close()

	c := NewClient(Config{ClientID: "kubetail-desktop", TokenURL: ts.URL})
	tok, err := c.Refresh(context.Background(), "refresh-123")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if tok.RefreshToken != "refresh-123" {
		t.Errorf("RefreshToken = %q, want the one we sent back", tok.RefreshToken)
	}
}

// expires_in may arrive as a string, and a huge one is capped rather than
// overflowing into an expiry in the past.
func TestRefreshReadsExpiresIn(t *testing.T) {
	for _, tc := range []struct {
		name, expiresIn string
		want            time.Duration
	}{
		{"a number", "3600", time.Hour},
		{"a string", `"3600"`, time.Hour},
		{"huge", "99999999999", math.MaxInt32 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"access_token":"a","token_type":"bearer","expires_in":`+tc.expiresIn+`}`)
			}))
			defer ts.Close()

			before := time.Now()
			tok, err := NewClient(Config{ClientID: "kubetail-desktop", TokenURL: ts.URL}).Refresh(context.Background(), "r")
			require.NoError(t, err)
			assert.WithinRange(t, tok.Expiry, before.Add(tc.want), time.Now().Add(tc.want))
		})
	}
}

// A revocation URL that cannot become a request fails before any network call.
func TestRevokeRejectsAnUnusableEndpoint(t *testing.T) {
	c := NewClient(Config{ClientID: "x", RevocationURL: "://bad"})
	if err := c.Revoke(context.Background(), "t"); err == nil {
		t.Fatal("want an error for an unusable revocation endpoint")
	}
}

// The resource (RFC 8707) and a caller's own parameters ride the authorize URL
// beside the standard ones.
func TestAuthCodeURLSendsTheResourceAndExtraParameters(t *testing.T) {
	c := NewClient(Config{
		ClientID: "dynamic_agent_client",
		AuthURL:  "https://issuer.test/oauth/authorize",
		Scopes:   []string{"openid", "offline_access"},
		Resource: "https://api.test/v1",
	})

	raw := c.AuthCodeURL("st", testVerifier, "http://127.0.0.1:1/auth/callback", url.Values{"nonce": {"n-1"}, "agent_name_hint": {"Kubetail"}})

	u, err := url.Parse(raw)
	require.NoError(t, err)
	q := u.Query()
	assert.Equal(t, "https://api.test/v1", q.Get("resource"))
	assert.Equal(t, "n-1", q.Get("nonce"))
	assert.Equal(t, "Kubetail", q.Get("agent_name_hint"))
	assert.Equal(t, "openid offline_access", q.Get("scope"))
	assert.Equal(t, "dynamic_agent_client", q.Get("client_id"))
}

// tokenForm serves a token endpoint that records each form it is posted and
// answers body.
func tokenForm(t *testing.T, body string) (*httptest.Server, *[]url.Values) {
	t.Helper()
	var forms []url.Values
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		forms = append(forms, r.PostForm)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(ts.Close)
	return ts, &forms
}

// The exchange sends the resource and keeps the scopes the issuer granted.
func TestExchangeSendsTheResourceAndKeepsTheGrantedScopes(t *testing.T) {
	ts, forms := tokenForm(t, `{"access_token":"a","refresh_token":"r","id_token":"i","token_type":"bearer","expires_in":3600,"scope":"openid chatgpt.tokens.use.direct"}`)
	c := NewClient(Config{ClientID: "issued", TokenURL: ts.URL, Resource: "https://api.test/v1"})

	tok, err := c.Exchange(context.Background(), "the-code", testVerifier, "http://127.0.0.1:1/cb")

	require.NoError(t, err)
	assert.Equal(t, "openid chatgpt.tokens.use.direct", tok.Scope)
	require.Len(t, *forms, 1)
	assert.Equal(t, "https://api.test/v1", (*forms)[0].Get("resource"))
	assert.Equal(t, "issued", (*forms)[0].Get("client_id"))
}

// A refresh posts its own form, with the resource and no scope, and keeps the
// replacement refresh token a rotating issuer sends.
func TestRefreshPostsTheResourceAndKeepsTheRotation(t *testing.T) {
	ts, forms := tokenForm(t, `{"access_token":"a2","refresh_token":"r2","id_token":"i2","token_type":"bearer","expires_in":3600,"scope":"openid"}`)
	c := NewClient(Config{ClientID: "issued", TokenURL: ts.URL, Resource: "https://api.test/v1", Scopes: []string{"openid"}})

	before := time.Now()
	tok, err := c.Refresh(context.Background(), "r1")

	require.NoError(t, err)
	assert.Equal(t, Token{AccessToken: "a2", RefreshToken: "r2", IDToken: "i2", Expiry: tok.Expiry, Scope: "openid"}, tok)
	assert.WithinDuration(t, before.Add(time.Hour), tok.Expiry, time.Minute)
	require.Len(t, *forms, 1)
	assert.Equal(t, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {"r1"},
		"client_id":     {"issued"},
		"resource":      {"https://api.test/v1"},
	}, (*forms)[0])
}

// A grant the issuer refuses is a TokenError carrying its code, on the exchange
// and on a refresh; a body with no code still carries the status.
func TestARefusedGrantIsATokenError(t *testing.T) {
	refusing := func(status int, body string) *Client {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		}))
		t.Cleanup(ts.Close)
		return NewClient(Config{ClientID: "issued", TokenURL: ts.URL})
	}

	_, err := refusing(http.StatusBadRequest, `{"error":"invalid_grant"}`).Refresh(context.Background(), "r1")
	tokErr, ok := errors.AsType[*TokenError](err)
	require.True(t, ok, "err = %v", err)
	assert.Equal(t, TokenError{Status: http.StatusBadRequest, Code: "invalid_grant"}, *tokErr)

	_, err = refusing(http.StatusUnauthorized, `{"error":"invalid_client"}`).Exchange(context.Background(), "code", testVerifier, "")
	tokErr, ok = errors.AsType[*TokenError](err)
	require.True(t, ok, "err = %v", err)
	assert.Equal(t, "invalid_client", tokErr.Code)

	_, err = refusing(http.StatusBadGateway, `<html>`).Refresh(context.Background(), "r1")
	tokErr, ok = errors.AsType[*TokenError](err)
	require.True(t, ok, "err = %v", err)
	assert.Equal(t, TokenError{Status: http.StatusBadGateway}, *tokErr)

	_, err = refusing(http.StatusOK, `{"token_type":"bearer"}`).Refresh(context.Background(), "r1")
	assert.Error(t, err, "a reply with no access token is no token")
	assert.Equal(t, "oauth: token endpoint refused the grant: 400 invalid_grant", (&TokenError{Status: 400, Code: "invalid_grant"}).Error())

	_, err = NewClient(Config{ClientID: "issued", TokenURL: "://bad"}).Refresh(context.Background(), "r1")
	assert.Error(t, err, "an endpoint that is no URL")
	_, err = NewClient(Config{ClientID: "issued", TokenURL: "http://127.0.0.1:1"}).Refresh(context.Background(), "r1")
	assert.Error(t, err, "an endpoint nothing answers")
}

// A revocation says what it revokes.
func TestRevokeSendsTheTokenTypeHint(t *testing.T) {
	ts, forms := tokenForm(t, `{}`)
	c := NewClient(Config{ClientID: "issued", RevocationURL: ts.URL})

	require.NoError(t, c.Revoke(context.Background(), "r1"))

	require.Len(t, *forms, 1)
	assert.Equal(t, "refresh_token", (*forms)[0].Get("token_type_hint"))
}

// VerifyNonce checks the nonce the authorize request sent.
func TestVerifyChecksTheNonce(t *testing.T) {
	its := newIDTokenServer(t)
	defer its.srv.Close()
	c := NewClient(Config{ClientID: "kubetail-desktop", Issuer: its.issuer, JWKSURL: its.srv.URL + "/jwks"})
	raw := its.sign(t, its.signKey, "kubetail-desktop", map[string]any{"nonce": "n-1"})

	_, err := c.VerifyNonce(context.Background(), raw, "n-1")
	require.NoError(t, err)
	_, err = c.VerifyNonce(context.Background(), raw, "n-2")
	assert.Error(t, err)
	_, err = c.VerifyNonce(context.Background(), its.sign(t, its.signKey, "kubetail-desktop", nil), "n-1")
	assert.Error(t, err, "a token with no nonce")
	_, err = c.VerifyNonce(context.Background(), its.sign(t, its.otherKey, "kubetail-desktop", map[string]any{"nonce": "n-1"}), "n-1")
	assert.Error(t, err, "a token another key signed")
	_, err = c.VerifyNonce(context.Background(), its.sign(t, its.signKey, "kubetail-desktop", nil), "")
	assert.Error(t, err, "no nonce expected")
}

// The issuer's client follows no redirect: a 307 or 308 would resend a form
// holding a code or a refresh token to wherever it points.
func TestTheIssuersClientFollowsNoRedirect(t *testing.T) {
	var elsewhere int
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { elsewhere++ }))
	defer target.Close()
	for _, code := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL, code)
		}))
		c := NewClient(Config{ClientID: "issued", TokenURL: ts.URL, RevocationURL: ts.URL})

		_, err := c.Exchange(context.Background(), "code", testVerifier, "")
		assert.Error(t, err)
		_, err = c.Refresh(context.Background(), "r1")
		assert.Error(t, err)
		assert.Error(t, c.Revoke(context.Background(), "r1"))
		ts.Close()
	}
	assert.Zero(t, elsewhere)
}

// discoveryServer serves an OpenID configuration doc builds from its own URL.
func discoveryServer(t *testing.T, doc func(base string) map[string]any) string {
	t.Helper()
	var base string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc(base))
	}))
	t.Cleanup(ts.Close)
	base = ts.URL
	return base
}

func goodDoc(base string) map[string]any {
	return map[string]any{
		"issuer":                 base,
		"authorization_endpoint": base + "/oauth/authorize",
		"token_endpoint":         base + "/oauth/token",
		"jwks_uri":               base + "/.well-known/jwks.json",
		"revocation_endpoint":    base + "/oauth/revoke",
	}
}

// Discover reads the endpoints off the issuer's own document.
func TestDiscoverReadsTheEndpoints(t *testing.T) {
	base := discoveryServer(t, goodDoc)

	got, err := Discover(context.Background(), base)

	require.NoError(t, err)
	assert.Equal(t, Endpoints{
		Issuer:        base,
		AuthURL:       base + "/oauth/authorize",
		TokenURL:      base + "/oauth/token",
		JWKSURL:       base + "/.well-known/jwks.json",
		RevocationURL: base + "/oauth/revoke",
	}, got)
}

// A document naming another issuer, or an endpoint off the issuer's origin, is
// refused; so is one missing an endpoint the flow needs, and a redirect.
func TestDiscoverRefusesADocumentItCannotTrust(t *testing.T) {
	for name, edit := range map[string]func(base string, doc map[string]any){
		"another issuer":             func(_ string, doc map[string]any) { doc["issuer"] = "https://evil.test" },
		"an endpoint elsewhere":      func(_ string, doc map[string]any) { doc["token_endpoint"] = "https://evil.test/token" },
		"an endpoint on plain http":  func(base string, doc map[string]any) { doc["jwks_uri"] = "ftp" + base[4:] + "/jwks" },
		"a revocation elsewhere":     func(_ string, doc map[string]any) { doc["revocation_endpoint"] = "https://evil.test/revoke" },
		"no token endpoint":          func(_ string, doc map[string]any) { delete(doc, "token_endpoint") },
		"an endpoint that is no URL": func(_ string, doc map[string]any) { doc["authorization_endpoint"] = "://bad" },
	} {
		t.Run(name, func(t *testing.T) {
			base := discoveryServer(t, func(base string) map[string]any {
				doc := goodDoc(base)
				edit(base, doc)
				return doc
			})

			_, err := Discover(context.Background(), base)

			assert.Error(t, err)
		})
	}

	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, discoveryServer(t, goodDoc)+r.URL.Path, http.StatusFound)
	}))
	defer redirecting.Close()
	_, err := Discover(context.Background(), redirecting.URL)
	assert.Error(t, err)

	notJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html>") }))
	defer notJSON.Close()
	_, err = Discover(context.Background(), notJSON.URL)
	assert.Error(t, err)

	_, err = Discover(context.Background(), "http://127.0.0.1:1")
	assert.Error(t, err)
	_, err = Discover(context.Background(), "://bad")
	assert.Error(t, err)
}

// A client with a secret sends it on the exchange and on a refresh, as a
// provider whose installed-app clients carry one requires; a client without
// one sends none.
func TestAClientSecretGoesOnTheExchangeAndTheRefresh(t *testing.T) {
	ts, forms := tokenForm(t, `{"access_token":"a","refresh_token":"r","id_token":"i","token_type":"bearer","expires_in":3600}`)
	c := NewClient(Config{ClientID: "fixed", ClientSecret: "not-confidential", TokenURL: ts.URL})

	_, err := c.Exchange(context.Background(), "the-code", testVerifier, "http://127.0.0.1:1/cb")
	require.NoError(t, err)
	_, err = c.Refresh(context.Background(), "r")
	require.NoError(t, err)

	require.Len(t, *forms, 2)
	for _, form := range *forms {
		assert.Equal(t, "not-confidential", form.Get("client_secret"))
	}

	public := NewClient(Config{ClientID: "public", TokenURL: ts.URL})
	_, err = public.Refresh(context.Background(), "r")
	require.NoError(t, err)
	assert.NotContains(t, (*forms)[2], "client_secret")
}

// Every request to the issuer names Kubetail: oauth2's token requests, the revoke
// and the discovery.
func TestEveryRequestToTheIssuerNamesKubetail(t *testing.T) {
	var agents []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agents = append(agents, r.Header.Get("User-Agent"))
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()
	c := NewClient(Config{ClientID: "issued", TokenURL: ts.URL, RevocationURL: ts.URL})

	_, _ = c.Exchange(context.Background(), "code", testVerifier, "")
	_, _ = c.Refresh(context.Background(), "r1")
	_ = c.Revoke(context.Background(), "r1")
	_, _ = Discover(context.Background(), ts.URL)

	assert.Equal(t, []string{version.UserAgent(), version.UserAgent(), version.UserAgent(), version.UserAgent()}, agents)
}
