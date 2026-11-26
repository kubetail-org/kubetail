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
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

const (
	// maxAnswerBody caps each answer of GitHub's.
	maxAnswerBody = 64 << 10
	// defaultInterval is the wait between polls when GitHub names none (RFC 8628 §3.2).
	defaultInterval = 5
	// slowDownStep is what a slow_down adds to the interval (RFC 8628 §3.5).
	slowDownStep = 5
	deviceGrant  = "urn:ietf:params:oauth:grant-type:device_code"
)

// loginShape is a GitHub login: what the row draws as the account.
var loginShape = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)

// tokenShape is a token as GitHub grants one, at least 16 characters long: the
// shortest value safe.AddSecret blanks.
var tokenShape = regexp.MustCompile(`^[A-Za-z0-9_]{16,255}$`)

// userCodeShape is a code as GitHub issues one ("WDJB-MJHT"). It is drawn for
// the user to type, so nothing else is.
var userCodeShape = regexp.MustCompile(`^[A-Z0-9]{4,8}(-[A-Z0-9]{4,8})?$`)

// deviceCode is GitHub's answer to a device code request.
type deviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	// ExpiresIn and Interval are seconds.
	ExpiresIn int `json:"expires_in"`
	Interval  int `json:"interval"`
}

// requestCode asks GitHub for a device code under the OAuth App. Only a whole
// answer whose page is on GitHub's origin is one.
func (s *Service) requestCode(ctx context.Context) (deviceCode, connectors.Failure) {
	var c deviceCode
	form := url.Values{"client_id": {s.clientID}, "scope": {strings.Join(scopes, " ")}}
	if failure := s.post(ctx, "login/device/code", form, &c); failure != "" {
		return c, failure
	}
	if c.DeviceCode == "" || !userCodeShape.MatchString(c.UserCode) || !s.onAuthOrigin(c.VerificationURI) {
		slog.Warn("github: the device code answer was not one")
		return c, connectors.Refused
	}
	if c.Interval <= 0 {
		c.Interval = defaultInterval
	}
	return c, ""
}

// onAuthOrigin reports whether raw is a page on GitHub's own origin, the one
// the browser may open.
func (s *Service) onAuthOrigin(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == s.auth.Scheme && u.Host == s.auth.Host
}

// post sends form to path under GitHub's origin and decodes its JSON answer
// into out. A 200, or the 400 an OAuth error may come as, is decoded; any other
// 4xx is GitHub's refusal, and the rest Unavailable.
func (s *Service) post(ctx context.Context, path string, form url.Values, out any) connectors.Failure {
	ctx, cancel := context.WithTimeout(ctx, s.issuerTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.auth.JoinPath(path).String(), strings.NewReader(form.Encode()))
	if err != nil { // coverage:ignore the origin parsed in New, and the method is a constant
		return connectors.Unavailable
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := llm.NoRedirectClient.Do(req)
	if err != nil {
		slog.Warn("github: GitHub could not be reached", "path", path, "err", err)
		return connectors.Unavailable
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK, resp.StatusCode == http.StatusBadRequest:
	case resp.StatusCode > 400 && resp.StatusCode < 500:
		slog.Warn("github: GitHub refused", "path", path, "status", resp.StatusCode)
		return connectors.Refused
	default:
		slog.Warn("github: GitHub answered", "path", path, "status", resp.StatusCode)
		return connectors.Unavailable
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAnswerBody)).Decode(out); err != nil {
		slog.Warn("github: GitHub's answer did not parse", "path", path)
		return connectors.Unavailable
	}
	return ""
}

// tokenAnswer is the token endpoint's answer: a token, or an error code.
type tokenAnswer struct {
	AccessToken string `json:"access_token"`
	Error       string `json:"error"`
	// Interval is the wait a slow_down names, in seconds.
	Interval int `json:"interval"`
}

// poll asks the token endpoint for c's token once an interval, until GitHub
// grants one or ends the sign-in, or ctx ends. A ctx past its deadline is
// TimedOut; a cancelled one answers neither a token nor a failure, since
// whoever cancelled it ended the attempt.
func (s *Service) poll(ctx context.Context, c deviceCode) (string, connectors.Failure) {
	form := url.Values{"client_id": {s.clientID}, "device_code": {c.DeviceCode}, "grant_type": {deviceGrant}}
	interval := c.Interval
	for {
		select {
		case <-ctx.Done():
			return "", ctxFailure(ctx)
		case <-time.After(s.pace(interval)):
		}
		var answer tokenAnswer
		if failure := s.post(ctx, "login/oauth/access_token", form, &answer); failure != "" {
			if ctx.Err() != nil {
				return "", ctxFailure(ctx)
			}
			return "", failure
		}
		switch answer.Error {
		case "":
			if !tokenShape.MatchString(answer.AccessToken) {
				slog.Warn("github: the token endpoint answered no token")
				return "", connectors.Refused
			}
			safe.AddSecret(answer.AccessToken)
			return answer.AccessToken, ""
		case "authorization_pending":
		case "slow_down":
			interval = max(answer.Interval, interval+slowDownStep)
		case "access_denied":
			return "", connectors.Declined
		case "expired_token":
			return "", connectors.TimedOut
		default:
			slog.Warn("github: the token endpoint refused the sign-in", "code", answer.Error)
			return "", connectors.Refused
		}
	}
}

// ctxFailure is what an ended ctx says of the sign-in.
func ctxFailure(ctx context.Context) connectors.Failure {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return connectors.TimedOut
	}
	return ""
}

// fetchLogin names token's account. GitHub's 401 is its refusal, as is a login
// that is not one; any other failure is Unavailable.
func (s *Service) fetchLogin(token string) (string, connectors.Failure) {
	status, login, err := s.user(token)
	switch {
	case status == http.StatusUnauthorized:
		slog.Warn("github: GitHub refused the new token")
		return "", connectors.Refused
	case err != nil || status != http.StatusOK:
		slog.Warn("github: the account could not be read", "status", status, "err", err)
		return "", connectors.Unavailable
	case !loginShape.MatchString(login):
		slog.Warn("github: the account's login was not one")
		return "", connectors.Refused
	}
	return login, ""
}

// user asks GitHub's API whose token it is: the answer's status and, from a 200,
// the login. Only the service and the request's own timeout bound it: a token
// GitHub granted is owed an outcome whatever its attempt came to.
func (s *Service) user(token string) (status int, login string, err error) {
	ctx, cancel := context.WithTimeout(s.ctx, s.issuerTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.api.JoinPath("user").String(), nil)
	if err != nil { // coverage:ignore the base parsed in New, and the method is a constant
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := llm.NoRedirectClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, "", nil
	}
	var user struct {
		Login string `json:"login"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, maxAnswerBody)).Decode(&user)
	return resp.StatusCode, user.Login, err
}
