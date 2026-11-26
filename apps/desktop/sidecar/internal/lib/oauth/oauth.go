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

// Package oauth is an OAuth2/OIDC client: PKCE (S256), the
// code→token exchange, refresh, RFC 7009 revocation, ID-token verification. A leaf:
// it owns the value types the protocol produces (Token, Identity).
//
// The Client takes resolved endpoint URLs and is Hydra-agnostic. The loopback listener
// lives in oauth/browser and the verifier/state generation in the callers — not protocol
// concerns.
package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
)

// expiryLeeway expires a token early, so one handed out stays valid through the request
// that uses it.
const expiryLeeway = 30 * time.Second

// Token is a grant's credential. Access and id tokens are short-lived and renewed via
// Refresh; an issuer that rotates refresh tokens hands a replacement back with each one.
type Token struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	Expiry       time.Time
	// Scope is the scopes the issuer granted, space-separated, as its response
	// said them.
	Scope string
}

// Valid reports an access token present and outside the leeway window of expiry.
func (t Token) Valid(now time.Time) bool {
	return t.AccessToken != "" && now.Before(t.Expiry.Add(-expiryLeeway))
}

// Identity is the ID-token claims the app cares about, from a token VerifyNonce
// checked. The JSON tags decode straight from the OIDC claims.
type Identity struct {
	UserID string `json:"sub"`
	Email  string `json:"email"`
	Name   string `json:"name"`
}

// Config is the OAuth client's static configuration. Its endpoints are given, written out
// or read by Discover, so tests can point them at httptest servers.
type Config struct {
	ClientID string
	// ClientSecret is an installed app's secret, which its provider requires on
	// the token endpoint though the app cannot keep it confidential. Empty for a
	// public client.
	ClientSecret  string
	AuthURL       string
	TokenURL      string
	JWKSURL       string
	RevocationURL string // Hydra's RFC 7009 token-revocation endpoint
	Issuer        string // expected "iss" of the ID token
	Scopes        []string
	// Resource is the RFC 8707 resource the tokens are for, sent on the authorize,
	// exchange and refresh requests when set.
	Resource string
}

// Client performs the OAuth flow and verifies ID tokens.
type Client struct {
	oc            *oauth2.Config
	verifier      *oidc.IDTokenVerifier
	revocationURL string
	resource      string
	httpc         *http.Client
}

// newHTTPClient is the issuer's client, naming Kubetail on every request, go-oidc's
// and oauth2's included. It follows no redirect: a 307 or 308 would resend a form
// holding a code or a refresh token to wherever it points.
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport:     version.Transport(nil),
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// NewClient does no network I/O — the JWKS fetch is lazy, on first VerifyNonce. That fetch
// reuses the 15s-timeout HTTP client, because go-oidc ignores context cancellation for
// the key set, so only the client timeout bounds it.
func NewClient(cfg Config) *Client {
	oc := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Scopes:       cfg.Scopes,
		// client_id, and a secret if any, go in the form, never as Basic auth.
		Endpoint: oauth2.Endpoint{
			AuthURL:   cfg.AuthURL,
			TokenURL:  cfg.TokenURL,
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
	c := &Client{
		oc:            oc,
		revocationURL: cfg.RevocationURL,
		resource:      cfg.Resource,
		httpc:         newHTTPClient(),
	}
	if cfg.JWKSURL != "" {
		// The base context is irrelevant — go-oidc ignores its cancel; ClientContext
		// is only how the timeout-bounded client reaches the lazy fetch.
		keySet := oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), c.httpc), cfg.JWKSURL)
		c.verifier = oidc.NewVerifier(cfg.Issuer, keySet, &oidc.Config{ClientID: cfg.ClientID})
	}
	return c
}

// Revoke revokes a refresh token (RFC 7009), which also invalidates its grant's access
// tokens; the desktop public client needs only client_id. A nil error means "no longer
// usable" — the server also reports success for an already-invalid token.
func (c *Client) Revoke(ctx context.Context, token string) error {
	// Cap of the non-2xx body read for the error message.
	const maxErrBody = 4 << 10
	if c.revocationURL == "" {
		return fmt.Errorf("oauth: no revocation endpoint configured")
	}
	form := url.Values{
		"token":           {token},
		"token_type_hint": {"refresh_token"},
		"client_id":       {c.oc.ClientID},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.revocationURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBody))
		return fmt.Errorf("oauth: revoke failed %d: %s", resp.StatusCode, body)
	}
	return nil
}

// AuthCodeURL builds the PKCE (S256) authorize URL. redirect is the loopback listener's
// ephemeral address and MUST match the one passed to Exchange. extra is the caller's own
// parameters, nil for none.
func (c *Client) AuthCodeURL(state, verifier, redirect string, extra url.Values) string {
	opts := []oauth2.AuthCodeOption{
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("redirect_uri", redirect),
	}
	if c.resource != "" {
		opts = append(opts, oauth2.SetAuthURLParam("resource", c.resource))
	}
	for k := range extra {
		opts = append(opts, oauth2.SetAuthURLParam(k, extra.Get(k)))
	}
	return c.oc.AuthCodeURL(state, opts...)
}

// Exchange trades an authorization code + PKCE verifier for a Token; redirect must echo
// the one sent to AuthCodeURL, as OAuth requires.
func (c *Client) Exchange(ctx context.Context, code, verifier, redirect string) (Token, error) {
	opts := []oauth2.AuthCodeOption{
		oauth2.VerifierOption(verifier),
		oauth2.SetAuthURLParam("redirect_uri", redirect),
	}
	if c.resource != "" {
		opts = append(opts, oauth2.SetAuthURLParam("resource", c.resource))
	}
	tok, err := c.oc.Exchange(context.WithValue(ctx, oauth2.HTTPClient, c.httpc), code, opts...)
	if retrieve, ok := errors.AsType[*oauth2.RetrieveError](err); ok {
		return Token{}, &TokenError{Status: retrieve.Response.StatusCode, Code: retrieve.ErrorCode}
	}
	if err != nil {
		return Token{}, err
	}
	return toToken(tok), nil
}

// TokenError is a token request the issuer refused: its status, and its error code
// (RFC 6749 §5.2) when the body named one.
type TokenError struct {
	Status int
	Code   string
}

func (e *TokenError) Error() string {
	return fmt.Sprintf("oauth: token endpoint refused the grant: %d %s", e.Status, e.Code)
}

// maxBody caps a reply of the issuer's that is decoded: a token response, the
// discovery document.
const maxBody = 1 << 20

// Refresh renews the tokens from a refresh token. It posts its own form, since
// x/oauth2's token source carries no extra parameter. An issuer that rotates refresh
// tokens answers a replacement, which the Token carries; one that does not leaves the
// refresh token as it was sent.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {c.oc.ClientID},
	}
	if c.oc.ClientSecret != "" {
		form.Set("client_secret", c.oc.ClientSecret)
	}
	if c.resource != "" {
		form.Set("resource", c.resource)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.oc.Endpoint.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpc.Do(req)
	if err != nil {
		return Token{}, err
	}
	defer resp.Body.Close()
	var body struct {
		AccessToken  string      `json:"access_token"`
		RefreshToken string      `json:"refresh_token"`
		IDToken      string      `json:"id_token"`
		ExpiresIn    json.Number `json:"expires_in"`
		Scope        string      `json:"scope"`
		Error        string      `json:"error"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&body)
	if resp.StatusCode/100 != 2 {
		return Token{}, &TokenError{Status: resp.StatusCode, Code: body.Error}
	}
	if decodeErr != nil || body.AccessToken == "" {
		return Token{}, fmt.Errorf("oauth: the token endpoint answered no access token")
	}
	tok := Token{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		IDToken:      body.IDToken,
		Scope:        body.Scope,
	}
	// Capped as x/oauth2 caps it, so a huge value cannot overflow into the past.
	if n, err := body.ExpiresIn.Int64(); err == nil && n > 0 {
		tok.Expiry = time.Now().Add(time.Duration(min(n, math.MaxInt32)) * time.Second)
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = refreshToken
	}
	return tok, nil
}

// VerifyNonce validates an ID token against the JWKS (signature, issuer, audience,
// expiry) and the nonce the authorize request sent, and returns its identity claims.
func (c *Client) VerifyNonce(ctx context.Context, rawIDToken, nonce string) (Identity, error) {
	if c.verifier == nil {
		return Identity{}, fmt.Errorf("oauth: no verifier configured (missing JWKSURL)")
	}
	idt, err := c.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return Identity{}, err
	}
	if nonce == "" || idt.Nonce != nonce {
		return Identity{}, fmt.Errorf("oauth: the ID token's nonce does not match")
	}
	var id Identity
	if err := idt.Claims(&id); err != nil {
		return Identity{}, err
	}
	return id, nil
}

// toToken maps an oauth2.Token to ours, lifting id_token and scope out of the extra
// fields.
func toToken(t *oauth2.Token) Token {
	idToken, _ := t.Extra("id_token").(string)
	scope, _ := t.Extra("scope").(string)
	return Token{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		IDToken:      idToken,
		Expiry:       t.Expiry,
		Scope:        scope,
	}
}

// Endpoints is what an OpenID Provider's configuration document says of it.
type Endpoints struct {
	Issuer        string
	AuthURL       string
	TokenURL      string
	JWKSURL       string
	RevocationURL string
}

// Discover reads issuer's endpoints from its /.well-known/openid-configuration. The
// document's issuer must be issuer exactly, and every endpoint it names must be on the
// issuer's origin, so a document served from elsewhere cannot send a code or a token
// there. The authorization, token and JWKS endpoints are required.
func Discover(ctx context.Context, issuer string) (Endpoints, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(issuer, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return Endpoints{}, err
	}
	resp, err := newHTTPClient().Do(req)
	if err != nil {
		return Endpoints{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Endpoints{}, fmt.Errorf("oauth: discovery answered %d", resp.StatusCode)
	}
	var doc struct {
		Issuer        string `json:"issuer"`
		AuthURL       string `json:"authorization_endpoint"`
		TokenURL      string `json:"token_endpoint"`
		JWKSURL       string `json:"jwks_uri"`
		RevocationURL string `json:"revocation_endpoint"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&doc); err != nil {
		return Endpoints{}, fmt.Errorf("oauth: unreadable discovery document: %w", err)
	}
	if doc.Issuer != issuer {
		return Endpoints{}, fmt.Errorf("oauth: discovery names another issuer")
	}
	origin, err := originOf(issuer)
	if err != nil { // coverage:ignore the request above parsed the same URL
		return Endpoints{}, err
	}
	for _, endpoint := range []string{doc.AuthURL, doc.TokenURL, doc.JWKSURL, doc.RevocationURL} {
		if endpoint == "" {
			continue
		}
		if o, err := originOf(endpoint); err != nil || o != origin {
			return Endpoints{}, fmt.Errorf("oauth: discovery names an endpoint off the issuer's origin")
		}
	}
	if doc.AuthURL == "" || doc.TokenURL == "" || doc.JWKSURL == "" {
		return Endpoints{}, fmt.Errorf("oauth: discovery is missing an endpoint")
	}
	return Endpoints(doc), nil
}

// originOf is rawURL's scheme and host, the host without case.
func originOf(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}
