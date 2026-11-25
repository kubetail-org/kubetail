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

package azureauth

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/azureproxy"
)

// ErrNoToken is every failure to get a token, in one sentence: az's own
// output can name the user, so it is never kept.
var ErrNoToken = errors.New("azureauth: az gave no Resource Manager token")

// renewBefore is how long before its expiry a token is asked for again.
const renewBefore = 5 * time.Minute

// tokens is the Resource Manager token held per tenant, each registered with
// safe while it is held. One call per tenant runs at a time, under that
// tenant's lock, a one-slot channel so a waiter can give up.
type tokens struct {
	mu    sync.Mutex
	held  map[string]token
	locks map[string]chan struct{}
}

type token struct {
	value   string
	expires time.Time
}

func newTokens() tokens {
	return tokens{held: map[string]token{}, locks: map[string]chan struct{}{}}
}

func (ts *tokens) lock(tenant string) chan struct{} {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	l, ok := ts.locks[tenant]
	if !ok {
		l = make(chan struct{}, 1)
		ts.locks[tenant] = l
	}
	return l
}

func (ts *tokens) get(tenant string) (token, bool) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	tok, ok := ts.held[tenant]
	return tok, ok
}

// keep holds tok for tenant, in place of the token before it.
func (ts *tokens) keep(tenant string, tok token) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if old, ok := ts.held[tenant]; ok {
		safe.ForgetSecret(old.value)
	}
	safe.AddSecret(tok.value)
	ts.held[tenant] = tok
}

// clear forgets every held token.
func (ts *tokens) clear() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for tenant, tok := range ts.held {
		safe.ForgetSecret(tok.value)
		delete(ts.held, tenant)
	}
}

// drop forgets tenant's token if it is still value.
func (ts *tokens) drop(tenant, value string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if tok, ok := ts.held[tenant]; ok && tok.value == value {
		delete(ts.held, tenant)
		safe.ForgetSecret(value)
	}
}

// Token is a Resource Manager token for tenant, one of the account's alone,
// kept until five minutes before it expires or the next discovery.
func (s *Service) Token(ctx context.Context, tenant string) (string, error) {
	s.mu.Lock()
	acct, ok, login := s.account, s.ok, s.login
	s.mu.Unlock()
	holds := func(sub azureproxy.Subscription) bool { return sub.TenantID == tenant }
	if !ok || !slices.ContainsFunc(acct.Subscriptions, holds) {
		return "", ErrNoToken
	}
	l := s.tokens.lock(tenant)
	select {
	case l <- struct{}{}:
		defer func() { <-l }()
	case <-ctx.Done():
		return "", ErrNoToken
	}
	if tok, ok := s.tokens.get(tenant); ok && s.now().Before(tok.expires.Add(-renewBefore)) {
		return tok.value, nil
	}
	out, err := s.run(ctx, "account", "get-access-token", "--resource", azureproxy.Clouds[acct.Cloud.Name].Resource,
		"--tenant", tenant, "--output", "json")
	if err != nil {
		return "", ErrNoToken
	}
	var answer struct {
		AccessToken string      `json:"accessToken"`
		ExpiresOn   json.Number `json:"expires_on"`
		// ExpiresOnLocal is the expiry az before 2.54 gives alone, a local
		// time.
		ExpiresOnLocal string `json:"expiresOn"`
	}
	if json.Unmarshal(out, &answer) != nil || answer.AccessToken == "" {
		return "", ErrNoToken
	}
	expires, ok := expiry(answer.ExpiresOn, answer.ExpiresOnLocal)
	if !ok {
		return "", ErrNoToken
	}
	// A discovery that completed while az ran may have replaced the login
	// this token is for, so it is neither kept nor used.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.login != login {
		return "", ErrNoToken
	}
	s.tokens.keep(tenant, token{value: answer.AccessToken, expires: expires})
	return answer.AccessToken, nil
}

// Drop forgets tenant's token if it is still token, for an upstream 401.
func (s *Service) Drop(tenant, token string) { s.tokens.drop(tenant, token) }

// expiry is a token's expiry: epoch, az's expires_on, else local, its
// expiresOn.
func expiry(epoch json.Number, local string) (time.Time, bool) {
	if epoch != "" {
		sec, err := epoch.Int64()
		return time.Unix(sec, 0), err == nil
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05.999999", local, time.Local)
	return t, err == nil
}
