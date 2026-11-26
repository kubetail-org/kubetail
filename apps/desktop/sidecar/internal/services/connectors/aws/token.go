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
	"context"
	"crypto/ecdsa"
	"errors"
	"net/http"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/signin"
	"github.com/aws/aws-sdk-go-v2/service/signin/types"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/middleware"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
)

// tokenClient is AWS's token endpoint, CreateOAuth2Token, each request
// carrying a proof under the session's key.
type tokenClient struct {
	api *signin.Client
	now func() time.Time
}

func newTokenClient(base string, httpClient *http.Client, now func() time.Time) *tokenClient {
	api := signin.New(signin.Options{
		Region:       Region,
		BaseEndpoint: sdkaws.String(base),
		HTTPClient:   httpClient,
		Credentials:  sdkaws.AnonymousCredentials{},
		// The caller tries again on its next need; a refresh token is spent
		// once, so the SDK must not resend one on its own.
		Retryer: sdkaws.NopRetryer{},
	})
	return &tokenClient{api: api, now: now}
}

// tokens is one answer of the token endpoint, registered with the redactor
// until forget.
type tokens struct {
	creds   sdkaws.Credentials
	refresh string
	// idToken is the exchange's alone; a refresh answers none.
	idToken string
}

// forget drops the answer's tokens from the redactor.
func (t tokens) forget() {
	safe.ForgetSecret(t.refresh)
	safe.ForgetSecret(t.creds.SecretAccessKey)
	safe.ForgetSecret(t.creds.SessionToken)
	safe.ForgetSecret(t.idToken)
}

// exchange trades an authorization code for the session's first tokens.
func (c *tokenClient) exchange(ctx context.Context, key *ecdsa.PrivateKey, code, verifier, redirectURI string) (tokens, error) {
	return c.call(ctx, key, &types.CreateOAuth2TokenRequestBody{
		ClientId:     sdkaws.String(ClientID),
		GrantType:    sdkaws.String("authorization_code"),
		Code:         sdkaws.String(code),
		CodeVerifier: sdkaws.String(verifier),
		RedirectUri:  sdkaws.String(redirectURI),
	})
}

// refresh trades the refresh token for new credentials and its replacement.
func (c *tokenClient) refresh(ctx context.Context, key *ecdsa.PrivateKey, refreshToken string) (tokens, error) {
	return c.call(ctx, key, &types.CreateOAuth2TokenRequestBody{
		ClientId:     sdkaws.String(ClientID),
		GrantType:    sdkaws.String("refresh_token"),
		RefreshToken: sdkaws.String(refreshToken),
	})
}

// errIncomplete is an answer missing a token or the credentials' lifetime.
var errIncomplete = errors.New("aws: the token endpoint's answer is incomplete")

func (c *tokenClient) call(ctx context.Context, key *ecdsa.PrivateKey, body *types.CreateOAuth2TokenRequestBody) (tokens, error) {
	out, err := c.api.CreateOAuth2Token(ctx, &signin.CreateOAuth2TokenInput{TokenInput: body},
		signin.WithAPIOptions(func(stack *middleware.Stack) error {
			return stack.Finalize.Add(dpopMiddleware{key: key, now: c.now}, middleware.After)
		}))
	if err != nil {
		return tokens{}, err
	}
	o := out.TokenOutput
	if o == nil || o.AccessToken == nil || o.ExpiresIn == nil {
		return tokens{}, errIncomplete
	}
	t := tokens{
		creds: sdkaws.Credentials{
			AccessKeyID:     sdkaws.ToString(o.AccessToken.AccessKeyId),
			SecretAccessKey: sdkaws.ToString(o.AccessToken.SecretAccessKey),
			SessionToken:    sdkaws.ToString(o.AccessToken.SessionToken),
			Source:          "Kubetail AWS connector",
			CanExpire:       true,
			Expires:         c.now().Add(time.Duration(*o.ExpiresIn) * time.Second),
		},
		refresh: sdkaws.ToString(o.RefreshToken),
		idToken: sdkaws.ToString(o.IdToken),
	}
	if !t.creds.HasKeys() || t.creds.SessionToken == "" || t.refresh == "" {
		return tokens{}, errIncomplete
	}
	safe.AddSecret(t.refresh)
	safe.AddSecret(t.creds.SecretAccessKey)
	safe.AddSecret(t.creds.SessionToken)
	safe.AddSecret(t.idToken)
	return t, nil
}

// refused reports whether AWS answered err itself, as opposed to never being
// reached or answering no API error.
func refused(err error) bool {
	_, ok := errors.AsType[smithy.APIError](err)
	return ok
}

// sessionEnded reports whether err is AWS refusing the session's refresh
// token for good: expired, revoked, or its principal's password changed.
func sessionEnded(err error) bool {
	if _, ok := errors.AsType[*types.InvalidGrantException](err); ok {
		return true
	}
	_, ok := errors.AsType[*types.AccessDeniedException](err)
	return ok
}
