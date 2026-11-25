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

package gcpauth

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

var t0 = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

// answer is config-helper's JSON for token, expiring at expiry.
func answer(token string, expiry time.Time) []byte {
	return fmt.Appendf(nil, `{
  "configuration": {"active_configuration": "default", "properties": {"core": {"account": "dev@example.com", "project": "proj-1"}}},
  "credential": {"access_token": %q, "token_expiry": %q}
}`, token, expiry.Format(time.RFC3339))
}

// clock is a settable now.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// helper is a fake config-helper that counts its runs, keeps whether the
// last forced a refresh, and answers what run says.
type helper struct {
	runs      atomic.Int32
	refreshed atomic.Bool
	run       func(ctx context.Context, n int32) ([]byte, error)
}

func (h *helper) Run(ctx context.Context, refresh bool) ([]byte, error) {
	h.refreshed.Store(refresh)
	return h.run(ctx, h.runs.Add(1))
}

// source is a Source over h on c, whose delayed forgets never run.
func source(h *helper, c *clock) *Source {
	s := New(h.Run)
	s.now = c.Now
	s.after = func(time.Duration, func()) {}
	return s
}

// later is a call Source put off, and its delay.
type later struct {
	delay time.Duration
	call  func()
}

// laterOn has s hand each call it puts off to the returned channel, instead
// of running it.
func laterOn(s *Source) chan later {
	ch := make(chan later, 8)
	s.after = func(d time.Duration, f func()) { ch <- later{d, f} }
	return ch
}

// tokens answers a fresh token per run, each expiring an hour after c's now.
func tokens(c *clock) func(context.Context, int32) ([]byte, error) {
	return func(_ context.Context, n int32) ([]byte, error) {
		return answer(fmt.Sprintf("opaque-token-number-%04d", n), c.Now().Add(time.Hour)), nil
	}
}

func TestTheHelpersAnswerIsTheToken(t *testing.T) {
	c := &clock{now: t0}
	h := &helper{run: func(context.Context, int32) ([]byte, error) {
		return answer("ya29.the-users-token", t0.Add(time.Hour)), nil
	}}

	got, err := source(h, c).Token(t.Context())

	require.NoError(t, err)
	assert.Equal(t, Token{Value: "ya29.the-users-token", Project: "proj-1", Account: "dev@example.com", Expiry: t0.Add(time.Hour)}, got)
}

func TestTheTokenIsReadOncePerExpiry(t *testing.T) {
	c := &clock{now: t0}
	h := &helper{run: tokens(c)}
	s := source(h, c)

	first, err := s.Token(t.Context())
	require.NoError(t, err)
	again, err := s.Token(t.Context())
	require.NoError(t, err)
	assert.Equal(t, first, again)
	assert.Equal(t, int32(1), h.runs.Load())

	c.set(t0.Add(59 * time.Minute))
	later, err := s.Token(t.Context())
	require.NoError(t, err)
	assert.NotEqual(t, first.Value, later.Value, "past the expiry less a minute the helper runs again")
	assert.Equal(t, int32(2), h.runs.Load())
}

func TestConcurrentCallersShareOneRun(t *testing.T) {
	c := &clock{now: t0}
	started, release := make(chan struct{}), make(chan struct{})
	h := &helper{}
	h.run = func(ctx context.Context, n int32) ([]byte, error) {
		close(started)
		<-release
		return tokens(c)(ctx, n)
	}
	s := source(h, c)

	got := make(chan Token, 2)
	for range 2 {
		go func() {
			tok, _ := s.Token(context.Background())
			got <- tok
		}()
	}
	testutil.Wait(t, started, "the helper's run")
	// A second caller that comes after the run finds its token cached, so
	// either way the helper runs once.
	close(release)
	a, b := testutil.Recv(t, got, "a token"), testutil.Recv(t, got, "a token")
	assert.Equal(t, a, b)
	assert.Equal(t, int32(1), h.runs.Load())
}

func TestEachFailureIsItsSentinel(t *testing.T) {
	cases := []struct {
		name string
		err  error
		out  []byte
		want error
	}{
		{"no binary", exec.ErrNotFound, nil, ErrNoCLI},
		{"a non-zero exit", &exec.ExitError{}, []byte("ERROR: (gcloud.config.config-helper) You do not currently have an active account selected."), ErrNoAccount},
		{"a timeout", context.DeadlineExceeded, nil, ErrTimeout},
		{"output past the bound", errTooLong, nil, ErrUnreadable},
		{"JSON without a token", nil, []byte(`{"credential": {}}`), ErrNoAccount},
		{"no JSON", nil, []byte("secret-looking output"), ErrUnreadable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &helper{run: func(context.Context, int32) ([]byte, error) { return tc.out, tc.err }}
			_, err := source(h, &clock{now: t0}).Token(t.Context())
			assert.ErrorIs(t, err, tc.want)
			assert.Equal(t, tc.want.Error(), err.Error(), "no error's text holds the helper's output")
		})
	}
}

func TestTheHelpersRunIsBounded(t *testing.T) {
	h := &helper{run: func(ctx context.Context, _ int32) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	s := source(h, &clock{now: t0})
	s.bound = time.Millisecond

	_, err := s.Token(t.Context())

	assert.ErrorIs(t, err, ErrTimeout)
}

func TestAFailureIsCachedBriefly(t *testing.T) {
	c := &clock{now: t0}
	h := &helper{run: func(context.Context, int32) ([]byte, error) { return nil, &exec.ExitError{} }}
	s := source(h, c)

	_, err := s.Token(t.Context())
	require.ErrorIs(t, err, ErrNoAccount)
	c.set(t0.Add(29 * time.Second))
	_, err = s.Token(t.Context())
	require.ErrorIs(t, err, ErrNoAccount)
	assert.Equal(t, int32(1), h.runs.Load(), "within 30 s the failure answers from the cache")

	c.set(t0.Add(30 * time.Second))
	h.run = tokens(c)
	tok, err := s.Token(t.Context())
	require.NoError(t, err)
	assert.NotEmpty(t, tok.Value)
	assert.Equal(t, int32(2), h.runs.Load())
}

func TestACallerThatGivesUpLeavesTheRunGoing(t *testing.T) {
	c := &clock{now: t0}
	started, release := make(chan struct{}), make(chan struct{})
	h := &helper{}
	h.run = func(ctx context.Context, n int32) ([]byte, error) {
		close(started)
		<-release
		return tokens(c)(ctx, n)
	}
	s := source(h, c)
	ctx, cancel := context.WithCancel(t.Context())

	gaveUp := make(chan error, 1)
	go func() {
		_, err := s.Token(ctx)
		gaveUp <- err
	}()
	testutil.Wait(t, started, "the helper's run")
	cancel()
	assert.ErrorIs(t, testutil.Recv(t, gaveUp, "the caller's error"), context.Canceled)

	close(release)
	tok, err := s.Token(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "opaque-token-number-0001", tok.Value, "the next caller gets that run's token")
	assert.Equal(t, int32(1), h.runs.Load())
}

func TestAHeldTokenIsARegisteredSecret(t *testing.T) {
	safe.ResetSecrets()
	t.Cleanup(safe.ResetSecrets)
	c := &clock{now: t0}
	h := &helper{run: tokens(c)}
	s := source(h, c)

	first, err := s.Token(t.Context())
	require.NoError(t, err)
	assert.NotContains(t, safe.Redact("seen "+first.Value), first.Value)

	forgets := laterOn(s)
	c.set(t0.Add(59 * time.Minute))
	second, err := s.Token(t.Context())
	require.NoError(t, err)
	assert.NotContains(t, safe.Redact("seen "+second.Value), second.Value)
	f := testutil.Recv(t, forgets, "the replaced token's forget")
	assert.Equal(t, time.Minute, f.delay, "a replaced token is forgotten when it expires")
	assert.NotContains(t, safe.Redact("seen "+first.Value), first.Value, "and is blanked until then")
	f.call()
	assert.Contains(t, safe.Redact("seen "+first.Value), first.Value)

	s.Drop(second.Value)
	f = testutil.Recv(t, forgets, "the dropped token's forget")
	assert.NotContains(t, safe.Redact("seen "+second.Value), second.Value, "a dropped token is blanked until it expires")
	f.call()
	assert.Contains(t, safe.Redact("seen "+second.Value), second.Value)
}

func TestAnExpiredTokenIsForgottenAtOnce(t *testing.T) {
	safe.ResetSecrets()
	t.Cleanup(safe.ResetSecrets)
	c := &clock{now: t0}
	s := source(&helper{run: tokens(c)}, c)
	tok, err := s.Token(t.Context())
	require.NoError(t, err)
	forgets := laterOn(s)

	c.set(t0.Add(2 * time.Hour))
	s.Drop(tok.Value)

	assert.Equal(t, time.Duration(0), testutil.Recv(t, forgets, "the forget").delay)
}

// A token Google refused is read again with a forced refresh, since gcloud
// would otherwise answer the same token from its own cache.
func TestADroppedTokenIsReadAgainFresh(t *testing.T) {
	c := &clock{now: t0}
	h := &helper{run: tokens(c)}
	s := source(h, c)
	tok, err := s.Token(t.Context())
	require.NoError(t, err)
	assert.False(t, h.refreshed.Load())

	s.Drop(tok.Value)
	_, err = s.Token(t.Context())
	require.NoError(t, err)
	assert.True(t, h.refreshed.Load(), "the read after a drop forces a refresh")

	c.set(t0.Add(time.Hour))
	_, err = s.Token(t.Context())
	require.NoError(t, err)
	assert.False(t, h.refreshed.Load(), "a read at expiry does not")
}

// A refresh that fails is forced again on the next read.
func TestAFailedRefreshIsForcedAgain(t *testing.T) {
	c := &clock{now: t0}
	h := &helper{run: tokens(c)}
	s := source(h, c)
	tok, err := s.Token(t.Context())
	require.NoError(t, err)
	s.Drop(tok.Value)
	h.run = func(context.Context, int32) ([]byte, error) { return nil, &exec.ExitError{} }
	_, err = s.Token(t.Context())
	require.ErrorIs(t, err, ErrNoAccount)

	c.set(t0.Add(failureLife))
	h.run = tokens(c)
	_, err = s.Token(t.Context())
	require.NoError(t, err)
	assert.True(t, h.refreshed.Load())
}

// A token whose answer names no expiry is kept for assumedLife less the
// early read, not read again on every call.
func TestATokenWithNoExpiryIsKeptBriefly(t *testing.T) {
	c := &clock{now: t0}
	h := &helper{run: func(context.Context, int32) ([]byte, error) {
		return []byte(`{"credential":{"access_token":"no-expiry-token-value","token_expiry":null}}`), nil
	}}
	s := source(h, c)

	tok, err := s.Token(t.Context())
	require.NoError(t, err)
	assert.Equal(t, t0.Add(assumedLife), tok.Expiry)
	c.set(t0.Add(assumedLife - early - time.Second))
	_, err = s.Token(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int32(1), h.runs.Load())

	c.set(t0.Add(assumedLife - early))
	_, err = s.Token(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int32(2), h.runs.Load())
}

// A run that answers as its bound passes keeps its answer.
func TestARunThatFinishesAtItsBoundKeepsItsToken(t *testing.T) {
	c := &clock{now: t0}
	h := &helper{run: func(ctx context.Context, n int32) ([]byte, error) {
		<-ctx.Done()
		return tokens(c)(ctx, n)
	}}
	s := source(h, c)
	s.bound = time.Millisecond

	tok, err := s.Token(t.Context())

	require.NoError(t, err)
	assert.NotEmpty(t, tok.Value)
}

func TestACallerAlreadyGoneStartsNoRun(t *testing.T) {
	h := &helper{run: tokens(&clock{now: t0})}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := source(h, &clock{now: t0}).Token(ctx)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, h.runs.Load())
}

func TestDropForgetsOnlyTheCachedToken(t *testing.T) {
	safe.ResetSecrets()
	t.Cleanup(safe.ResetSecrets)
	c := &clock{now: t0}
	h := &helper{run: tokens(c)}
	s := source(h, c)

	tok, err := s.Token(t.Context())
	require.NoError(t, err)
	s.Drop("ya29.some-other-token-value")
	_, err = s.Token(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int32(1), h.runs.Load(), "another token's drop keeps the cached one")

	s.Drop(tok.Value)
	_, err = s.Token(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int32(2), h.runs.Load(), "the cached token's drop reads again")
}

func TestASharedReadLooksAgainFirst(t *testing.T) {
	c := &clock{now: t0}
	h := &helper{run: tokens(c)}
	s := source(h, c)
	first, err := s.Token(t.Context())
	require.NoError(t, err)

	got, err := s.readShared()

	require.NoError(t, err)
	assert.Equal(t, first, got)
	assert.Equal(t, int32(1), h.runs.Load())
}

func TestTheDefaultSourceRunsGcloud(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // serial: it sets the process's PATH
	_, err := New(nil).Token(t.Context())
	assert.ErrorIs(t, err, ErrNoCLI)
}

// A default Source puts a dropped token's forget on the clock: one already
// expired is forgotten at once.
func TestTheDefaultSourceForgetsOnTheClock(t *testing.T) {
	safe.ResetSecrets()
	t.Cleanup(safe.ResetSecrets)
	expired := func(context.Context, bool) ([]byte, error) {
		return answer("default-source-token-value", time.Now().Add(-time.Minute)), nil
	}
	s := New(expired)
	tok, err := s.Token(t.Context())
	require.NoError(t, err)

	s.Drop(tok.Value)

	assert.Eventually(t, func() bool { return !safe.HasSecret(tok.Value) }, testutil.Timeout, time.Millisecond)
}
