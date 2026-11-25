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

package githubauth

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// fakeGh answers each read with its next answer, the last one again once
// they run out, and counts the reads. An answer of "" fails the read.
type fakeGh struct {
	answers []string
	reads   int
}

func (f *fakeGh) read(context.Context) ([]byte, error) {
	answer := f.answers[min(f.reads, len(f.answers)-1)]
	f.reads++
	if answer == "" {
		return nil, errors.New("gh: not logged in")
	}
	return []byte(answer), nil
}

// clock is a time a test moves by hand.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

// clocked is a source over gh whose time is c's, holding a token for an hour
// and refusing to read again for a minute.
func clocked(gh *fakeGh, c *clock) *Source {
	s := newSource(gh.read)
	s.now, s.hold, s.retry = c.now, time.Hour, time.Minute
	return s
}

func mustToken(t *testing.T, s *Source) string {
	t.Helper()
	token, err := s.Token(t.Context())
	require.NoError(t, err)
	return token
}

func TestTheTokenIsReadOnceAndKept(t *testing.T) {
	t.Parallel()
	gh := &fakeGh{answers: []string{"gho_readOnceAndKept0001\n"}}
	s := newSource(gh.read)

	for range 3 {
		token, err := s.Token(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "gho_readOnceAndKept0001", token)
	}
	assert.Equal(t, 1, gh.reads)
}

func TestAnAnswerThatIsNotATokenIsRefused(t *testing.T) {
	t.Parallel()
	for name, answer := range map[string]string{
		"blank":              "\n",
		"two lines":          "gho_aaaaaaaaaaaaaaaa\ngho_bbbbbbbbbbbbbbbb\n",
		"a space":            "gho_aaaa aaaaaaaaaaaa\n",
		"a message":          "no oauth token found for github.com\n",
		"too long":           strings.Repeat("a", 256),
		"too short to blank": "gho_short\n",
		"a carriage end":     "gho_aaaaaaaaaaaaaaaa\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newSource((&fakeGh{answers: []string{answer}}).read)
			token, err := s.Token(t.Context())
			assert.ErrorIs(t, err, ErrNoLogin)
			assert.Empty(t, token)
		})
	}
}

func TestATokenExpiresFromTheCache(t *testing.T) {
	t.Parallel()
	gh := &fakeGh{answers: []string{"gho_expiresFromCache01\n", "gho_expiresFromCache02\n"}}
	c := &clock{t: time.Unix(1000, 0)}
	s := clocked(gh, c)

	assert.Equal(t, "gho_expiresFromCache01", mustToken(t, s))
	c.t = c.t.Add(time.Hour - time.Second)
	assert.Equal(t, "gho_expiresFromCache01", mustToken(t, s), "held until the hour is up")
	c.t = c.t.Add(time.Second)
	assert.Equal(t, "gho_expiresFromCache02", mustToken(t, s))
	assert.Equal(t, 2, gh.reads)
}

// A failed read answers ErrNoLogin until the retry interval is up, so a run
// with no login does not run gh on every request.
func TestAFailedReadIsHeldForTheInterval(t *testing.T) {
	t.Parallel()
	for name, failed := range map[string]string{"no login": "", "not a token": "not a token\n"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gh := &fakeGh{answers: []string{failed, "gho_heldForTheInterval1\n"}}
			c := &clock{t: time.Unix(1000, 0)}
			s := clocked(gh, c)

			for range 3 {
				_, err := s.Token(t.Context())
				assert.ErrorIs(t, err, ErrNoLogin)
			}
			assert.Equal(t, 1, gh.reads)
			c.t = c.t.Add(time.Minute)
			assert.Equal(t, "gho_heldForTheInterval1", mustToken(t, s))
		})
	}
}

// A token GitHub refuses is dropped and read again, but gh runs at most once
// per retry interval, whatever made it run: a command that draws 401s cannot
// run it on every request, nor right after the hold ran out.
func TestRepeatedRefusalsRunGhOncePerInterval(t *testing.T) {
	t.Parallel()
	gh := &fakeGh{answers: []string{"gho_refusedOncePerInt01\n", "gho_refusedOncePerInt02\n", "gho_refusedOncePerInt03\n"}}
	c := &clock{t: time.Unix(1000, 0)}
	s := clocked(gh, c)

	first := mustToken(t, s)
	s.Refused(first)
	assert.Equal(t, first, mustToken(t, s), "a refusal within a minute of gh's run keeps the token")

	c.t = c.t.Add(time.Minute)
	s.Refused(first)
	second := mustToken(t, s)
	assert.Equal(t, "gho_refusedOncePerInt02", second, "a refusal a minute on drops it")
	for range 3 {
		s.Refused(second)
		assert.Equal(t, second, mustToken(t, s))
	}
	s.Refused(first)
	assert.Equal(t, second, mustToken(t, s), "a refusal of a token no longer held drops nothing")

	c.t = c.t.Add(time.Hour)
	third := mustToken(t, s)
	assert.Equal(t, "gho_refusedOncePerInt03", third, "the hold ran out")
	s.Refused(third)
	assert.Equal(t, third, mustToken(t, s), "a refusal right after that read keeps it")
	assert.Equal(t, 3, gh.reads)
}

func TestAHeldTokenIsBlankedAndADroppedOneIsForgotten(t *testing.T) {
	t.Parallel()
	const refused, expired = "gho_blankedThenForgotten1", "gho_blankedThenForgotten2"
	gh := &fakeGh{answers: []string{refused + "\n", expired + "\n", "gho_blankedThenForgotten3\n"}}
	c := &clock{t: time.Unix(1000, 0)}
	s := clocked(gh, c)

	require.Equal(t, refused, mustToken(t, s))
	assert.NotContains(t, safe.String("token "+refused), refused, "blanked while held")
	c.t = c.t.Add(time.Minute)
	s.Refused(refused)
	assert.NotContains(t, safe.String("token "+refused), refused, "still blanked: requests may carry it")
	require.Equal(t, expired, mustToken(t, s))
	assert.Contains(t, safe.String("token "+refused), refused, "forgotten once another replaces it")

	assert.NotContains(t, safe.String("token "+expired), expired)
	c.t = c.t.Add(time.Hour)
	mustToken(t, s)
	assert.Contains(t, safe.String("token "+expired), expired, "forgotten once it expires and is replaced")
}

// A token being read again, on expiry or after a refusal, stays blanked
// while gh runs, and after it when gh prints it again.
func TestATokenStaysBlankedWhileItIsReadAgain(t *testing.T) {
	t.Parallel()
	const token = "gho_blankedWhileReadAgain1"
	var blankedDuringRead []bool
	c := &clock{t: time.Unix(1000, 0)}
	s := newSource(func(context.Context) ([]byte, error) {
		blankedDuringRead = append(blankedDuringRead, !strings.Contains(safe.String(token), token))
		return []byte(token + "\n"), nil
	})
	s.now, s.hold, s.retry = c.now, time.Hour, time.Minute

	mustToken(t, s)
	c.t = c.t.Add(time.Hour)
	mustToken(t, s)
	c.t = c.t.Add(time.Minute)
	s.Refused(token)
	mustToken(t, s)
	require.Len(t, blankedDuringRead, 3)
	assert.Equal(t, []bool{true, true}, blankedDuringRead[1:], "the expiry's read and the refusal's")
	assert.NotContains(t, safe.String(token), token)
}

func TestConcurrentReadsShareOneRun(t *testing.T) {
	t.Parallel()
	var reads atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	s := newSource(func(context.Context) ([]byte, error) {
		if reads.Add(1) == 1 {
			close(entered)
		}
		<-release
		return []byte("gho_sharedByEveryReader1\n"), nil
	})

	var wg sync.WaitGroup
	tokens := make(chan string, 4)
	for range 4 {
		wg.Go(func() {
			token, _ := s.Token(context.Background())
			tokens <- token
		})
	}
	testutil.Wait(t, entered, "the first read")
	close(release)
	wg.Wait()
	close(tokens)
	for token := range tokens {
		assert.Equal(t, "gho_sharedByEveryReader1", token)
	}
	assert.Equal(t, int32(1), reads.Load())
}

// The read outlives the request that started it, so a request the client
// gave up on does not leave a failure held for every request after it.
func TestACancelledRequestDoesNotFailTheRead(t *testing.T) {
	t.Parallel()
	s := newSource(func(ctx context.Context) ([]byte, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return []byte("gho_readPastACancel0001\n"), nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	token, err := s.Token(ctx)
	require.NoError(t, err)
	assert.Equal(t, "gho_readPastACancel0001", token)
}

// Neither the token nor what gh printed is logged; a failure logs one fixed
// reason and its class.
func TestTheTokenIsNeverLogged(t *testing.T) { // serial: CaptureLogs swaps the process's logger
	logs := testutil.CaptureLogs(t)
	const token = "gho_neverLoggedAnywhere1"
	gh := &fakeGh{answers: []string{"error: gho_neverLoggedPrinted\n", token + "\n"}}
	c := &clock{t: time.Unix(1000, 0)}
	s := clocked(gh, c)

	_, err := s.Token(t.Context())
	require.ErrorIs(t, err, ErrNoLogin)
	c.t = c.t.Add(time.Minute)
	assert.Equal(t, token, mustToken(t, s))

	assert.Contains(t, logs.String(), "the GitHub login could not be read")
	assert.Contains(t, logs.String(), "not a token")
	assert.NotContains(t, logs.String(), "gho_neverLogged")
}

// Each way a read fails is its own class, so a missing gh reads apart from a
// missing login.
func TestAFailedReadIsClassed(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("exec: %w", exec.ErrNotFound), "gh not found"},
		{&fs.PathError{Op: "fork/exec", Path: "/x/gh", Err: fs.ErrNotExist}, "gh not found"},
		{context.DeadlineExceeded, "timed out"},
		{errors.New("exit status 1"), "gh failed"},
	} {
		assert.Equal(t, c.want, failureOf(c.err), c.err)
	}
}

// New holds a token five minutes and reads again no sooner than a minute.
func TestNewHoldsATokenFiveMinutes(t *testing.T) {
	t.Parallel()
	s := New()
	assert.NotNil(t, s.read)
	assert.Equal(t, 5*time.Minute, s.hold)
	assert.Equal(t, time.Minute, s.retry)
}
