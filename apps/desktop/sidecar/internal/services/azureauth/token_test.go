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
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// clock is a time a test moves.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func tokenLine(tenant string) string {
	return "account get-access-token --resource https://management.core.windows.net/ --tenant " + tenant + " --output json"
}

// tokenAnswer is get-access-token's answer: token, expiring at expires.
func tokenAnswer(token string, expires time.Time) string {
	return fmt.Sprintf(`{"accessToken": %q, "expires_on": %d, "tokenType": "Bearer"}`, token, expires.Unix())
}

// tokenService is a service whose account is discovered, on clk.
func tokenService(t *testing.T, az *fakeAz, clk *clock) *Service {
	t.Helper()
	t.Cleanup(safe.ResetSecrets)
	s := newService(az.run, func() string { return "1" }, clk.Now)
	_, ok := s.Account(t.Context())
	require.True(t, ok)
	return s
}

func TestATokenIsAskedOncePerTenantAndKeptToItsExpiry(t *testing.T) {
	clk := &clock{now: time.Unix(1_800_000_000, 0)}
	az := newFakeAz()
	az.set(tokenLine(tenantA), tokenAnswer("token-a-0123456789", clk.Now().Add(time.Hour)))
	az.set(tokenLine(tenantB), tokenAnswer("token-b-0123456789", clk.Now().Add(time.Hour)))
	s := tokenService(t, az, clk)

	// Two callers at once share one az.
	gate := make(chan struct{})
	az.setGate(gate)
	got := make(chan string, 2)
	for range 2 {
		go func() {
			tok, err := s.Token(t.Context(), tenantA)
			assert.NoError(t, err)
			got <- tok
		}()
	}
	gate <- struct{}{}
	assert.Equal(t, "token-a-0123456789", testutil.Recv(t, got, "the first token"))
	assert.Equal(t, "token-a-0123456789", testutil.Recv(t, got, "the second token"))
	az.setGate(nil)
	assert.Equal(t, 1, az.count("account get-access-token"))

	tok, err := s.Token(t.Context(), tenantB)
	require.NoError(t, err)
	assert.Equal(t, "token-b-0123456789", tok)
	assert.Equal(t, 1, az.count(tokenLine(tenantB)), "each tenant has its own")

	clk.add(54 * time.Minute)
	_, err = s.Token(t.Context(), tenantA)
	require.NoError(t, err)
	assert.Equal(t, 1, az.count(tokenLine(tenantA)), "kept until five minutes before its expiry")
	clk.add(2 * time.Minute)
	_, err = s.Token(t.Context(), tenantA)
	require.NoError(t, err)
	assert.Equal(t, 2, az.count(tokenLine(tenantA)), "asked again past it")
}

func TestADroppedTokenIsAskedAgain(t *testing.T) {
	clk := &clock{now: time.Now()}
	az := newFakeAz()
	az.set(tokenLine(tenantA), tokenAnswer("token-a-0123456789", clk.Now().Add(time.Hour)))
	s := tokenService(t, az, clk)
	tok, err := s.Token(t.Context(), tenantA)
	require.NoError(t, err)

	s.Drop(tenantA, "another-token-0123456789")
	_, err = s.Token(t.Context(), tenantA)
	require.NoError(t, err)
	assert.Equal(t, 1, az.count(tokenLine(tenantA)), "a token no longer held drops nothing")

	s.Drop(tenantA, tok)
	_, err = s.Token(t.Context(), tenantA)
	require.NoError(t, err)
	assert.Equal(t, 2, az.count(tokenLine(tenantA)))
}

func TestAHeldTokenIsBlankedAndADroppedOneIsNot(t *testing.T) {
	clk := &clock{now: time.Now()}
	az := newFakeAz()
	az.set(tokenLine(tenantA), tokenAnswer("held-token-0123456789", clk.Now().Add(time.Hour)))
	az.set(tokenLine(tenantB), tokenAnswer("expiring-token-0123456789", clk.Now().Add(10*time.Minute)))
	s := tokenService(t, az, clk)

	tok, err := s.Token(t.Context(), tenantA)
	require.NoError(t, err)
	assert.Equal(t, safe.Redacted, safe.String(tok))
	s.Drop(tenantA, tok)
	assert.Equal(t, tok, safe.String(tok), "a dropped token is forgotten")

	expiring, err := s.Token(t.Context(), tenantB)
	require.NoError(t, err)
	az.set(tokenLine(tenantB), tokenAnswer("next-token-0123456789", clk.Now().Add(time.Hour)))
	clk.add(6 * time.Minute)
	_, err = s.Token(t.Context(), tenantB)
	require.NoError(t, err)
	assert.Equal(t, expiring, safe.String(expiring), "an expired token is forgotten once replaced")
	assert.Equal(t, safe.Redacted, safe.String("next-token-0123456789"))
}

// A tenant that is not the account's never reaches az, nor does a token
// asked before any account.
func TestOnlyTheAccountsTenantsReachAz(t *testing.T) {
	az := newFakeAz()
	s := tokenService(t, az, &clock{now: time.Now()})
	_, err := s.Token(t.Context(), "00000000-0000-0000-0000-00000000000f")
	assert.ErrorIs(t, err, ErrNoToken)
	_, err = s.Token(t.Context(), "--debug")
	assert.ErrorIs(t, err, ErrNoToken)
	assert.Zero(t, az.count("account get-access-token"))

	fresh := newService(az.run, func() string { return "1" }, time.Now)
	_, err = fresh.Token(t.Context(), tenantA)
	assert.ErrorIs(t, err, ErrNoToken)
	assert.Zero(t, az.count("account get-access-token"))
}

// A failed az, or an answer with no token, is ErrNoToken alone: neither the
// error nor the log holds what az printed.
func TestAFailedAzSaysNothingOfItsOutput(t *testing.T) {
	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	clk := &clock{now: time.Now()}
	az := newFakeAz()
	az.fail[tokenLine(tenantA)] = errors.New("AADSTS70043: refresh token expired for user ana@example.com")
	az.set(tokenLine(tenantB), `{"accessToken": "", "expires_on": 1}`)
	s := tokenService(t, az, clk)
	for _, answer := range []string{
		`{"accessToken": "token-0123456789abcdef", "expires_on": 1.5}`,
		`{"accessToken": "token-0123456789abcdef"}`,
		`not json`,
	} {
		az.set(tokenLine(tenantA), answer)
		delete(az.fail, tokenLine(tenantA))
		_, err := s.Token(t.Context(), tenantA)
		assert.ErrorIs(t, err, ErrNoToken, answer)
	}
	az.fail[tokenLine(tenantA)] = errors.New("AADSTS70043: refresh token expired for user ana@example.com")

	for _, tenant := range []string{tenantA, tenantB} {
		_, err := s.Token(t.Context(), tenant)
		require.ErrorIs(t, err, ErrNoToken)
		assert.Equal(t, ErrNoToken.Error(), err.Error())
	}
	assert.NotContains(t, logged.String(), "AADSTS")
	assert.NotContains(t, logged.String(), "ana@example.com")
}

// A rediscovery, as after the user signs in again, drops every held token,
// so the next request asks az for the login it now holds.
func TestARediscoveryDropsEveryToken(t *testing.T) {
	clk := &clock{now: time.Now()}
	az := newFakeAz()
	az.set(tokenLine(tenantA), tokenAnswer("admin-token-0123456789", clk.Now().Add(time.Hour)))
	p := &profile{stamp: "1"}
	s := newService(az.run, p.get, clk.Now)
	t.Cleanup(safe.ResetSecrets)
	_, ok := s.Account(t.Context())
	require.True(t, ok)
	old, err := s.Token(t.Context(), tenantA)
	require.NoError(t, err)

	az.set(tokenLine(tenantA), tokenAnswer("reader-token-0123456789", clk.Now().Add(time.Hour)))
	p.set("2")
	_, _ = s.Account(t.Context())
	testutil.Wait(t, s.discovery(), "the rediscovery")

	tok, err := s.Token(t.Context(), tenantA)
	require.NoError(t, err)
	assert.Equal(t, "reader-token-0123456789", tok)
	assert.Equal(t, old, safe.String(old), "the dropped token is forgotten")
}

// A token az gave while a rediscovery completed is not kept: it may be the
// login the rediscovery replaced.
func TestATokenFromBeforeARediscoveryIsNotKept(t *testing.T) {
	clk := &clock{now: time.Now()}
	az := newFakeAz()
	az.set(tokenLine(tenantA), tokenAnswer("admin-token-0123456789", clk.Now().Add(time.Hour)))
	s := tokenService(t, az, clk)
	az.before[tokenLine(tenantA)] = func() { s.discoverUnder(t.Context(), "2") }

	_, err := s.Token(t.Context(), tenantA)
	assert.ErrorIs(t, err, ErrNoToken)
	assert.Equal(t, "admin-token-0123456789", safe.String("admin-token-0123456789"), "never registered")

	delete(az.before, tokenLine(tenantA))
	tok, err := s.Token(t.Context(), tenantA)
	require.NoError(t, err)
	assert.Equal(t, "admin-token-0123456789", tok)
	assert.Equal(t, 2, az.count(tokenLine(tenantA)), "asked again")
}

// A request that gives up stops waiting for its tenant's token at once.
func TestATokenWaitEndsWithItsRequest(t *testing.T) {
	clk := &clock{now: time.Now()}
	az := newFakeAz()
	az.set(tokenLine(tenantA), tokenAnswer("token-a-0123456789", clk.Now().Add(time.Hour)))
	s := tokenService(t, az, clk)
	gate := make(chan struct{})
	az.setGate(gate)
	first := make(chan error, 1)
	go func() {
		_, err := s.Token(t.Context(), tenantA)
		first <- err
	}()
	require.Eventually(t, func() bool { return az.count(tokenLine(tenantA)) == 1 }, testutil.Timeout, time.Millisecond)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := s.Token(ctx, tenantA)
	assert.ErrorIs(t, err, ErrNoToken)
	close(gate)
	assert.NoError(t, testutil.Recv(t, first, "the first token"))
}

// An az from before expires_on reads its expiry from expiresOn, a local time.
func TestAnOlderAzsExpiryIsRead(t *testing.T) {
	clk := &clock{now: time.Date(2026, 11, 17, 12, 0, 0, 0, time.Local)}
	az := newFakeAz()
	az.set(tokenLine(tenantA), `{"accessToken": "old-az-token-0123456789", "expiresOn": "2026-11-17 13:00:00.000000"}`)
	s := tokenService(t, az, clk)
	_, err := s.Token(t.Context(), tenantA)
	require.NoError(t, err)
	clk.add(54 * time.Minute)
	_, err = s.Token(t.Context(), tenantA)
	require.NoError(t, err)
	assert.Equal(t, 1, az.count(tokenLine(tenantA)), "kept to five minutes before expiresOn")
	clk.add(2 * time.Minute)
	_, err = s.Token(t.Context(), tenantA)
	require.NoError(t, err)
	assert.Equal(t, 2, az.count(tokenLine(tenantA)))
}
