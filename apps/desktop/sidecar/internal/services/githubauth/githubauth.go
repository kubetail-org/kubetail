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

// Package githubauth is the user's GitHub token, read from gh outside the
// sandbox for the proxy a sandboxed command reaches GitHub through. It keeps
// the token in memory alone and never writes it.
package githubauth

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
)

// ErrNoLogin is every way the token cannot be read: no gh, no login, a read
// past its bound, or an answer that is not a token.
var ErrNoLogin = errors.New("no GitHub login was found")

// tokenShape is a token as gh prints one, at least 16 characters long: the
// shortest value safe.AddSecret blanks. gh's tokens are 40 and longer.
var tokenShape = regexp.MustCompile(`^[A-Za-z0-9_]{16,255}$`)

// A token is held for holdFor, and a failure, or a token GitHub refused, is
// read again no sooner than retryAfter. gh gets readBound to answer.
const (
	holdFor    = 5 * time.Minute
	retryAfter = time.Minute
	readBound  = 10 * time.Second
)

// Source is the user's token, read on first use and held. It registers the
// token held with safe.AddSecret, and forgets it only once another answer
// replaces it, so a token requests may still carry stays blanked.
type Source struct {
	// read runs gh auth token and answers what it printed.
	read  func(context.Context) ([]byte, error)
	now   func() time.Time
	hold  time.Duration // how long a token is held
	retry time.Duration // the least time between two runs of gh

	mu sync.Mutex
	// token is the token held, "" for a failure held; until is when the
	// answer held ends, zero with none.
	token string
	until time.Time
	ran   time.Time // when gh last ran
}

// New is the user's token as gh, found on the sidecar's PATH, prints it.
func New() *Source { return newSource(ghToken("gh", readBound)) }

func newSource(read func(context.Context) ([]byte, error)) *Source {
	return &Source{read: read, now: time.Now, hold: holdFor, retry: retryAfter}
}

// Token is the user's token, or ErrNoLogin. Concurrent calls share one read,
// which outlives ctx: a request a client gave up on must not leave a failure
// held for every request after it.
func (s *Source) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// A zero until, nothing held, is before any now.
	if now := s.now(); !now.Before(s.until) {
		old := s.token
		s.ran = now
		token, failure := s.readToken(context.WithoutCancel(ctx))
		if failure != "" {
			slog.Warn("the GitHub login could not be read", "reason", failure)
			s.token, s.until = "", now.Add(s.retry)
		} else {
			// Registered before the old one is forgotten, so a token gh
			// prints again is never left unblanked.
			safe.AddSecret(token)
			s.token, s.until = token, now.Add(s.hold)
		}
		if old != "" {
			safe.ForgetSecret(old)
		}
	}
	if s.token == "" {
		return "", ErrNoLogin
	}
	return s.token, nil
}

// Refused expires token, which GitHub answered 401, unless gh ran less than
// the retry interval ago: so a token GitHub refuses is read again, and a
// command that draws 401s cannot run gh more than once an interval.
func (s *Source) Refused(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == "" || token != s.token || s.now().Before(s.ran.Add(s.retry)) {
		return
	}
	s.until = time.Time{}
}

// readToken is the token gh prints as its one line, or why there is none in
// one fixed word, never gh's own.
func (s *Source) readToken(ctx context.Context) (token, failure string) {
	out, err := s.read(ctx)
	if err != nil {
		return "", failureOf(err)
	}
	token = strings.TrimSuffix(string(out), "\n")
	if !tokenShape.MatchString(token) {
		return "", "not a token"
	}
	return token, ""
}

// failureOf is a failed run of gh's class: no gh to run, a run past its
// bound, or any other.
func failureOf(err error) string {
	switch {
	case errors.Is(err, exec.ErrNotFound), errors.Is(err, fs.ErrNotExist):
		return "gh not found"
	case errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	}
	return "gh failed"
}

// ghToken runs gh at name for github.com's token, with no stdin and prompts
// off, and kills it past bound. What it prints on stderr is dropped unread.
func ghToken(name string, bound time.Duration) func(context.Context) ([]byte, error) {
	return func(ctx context.Context) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, bound)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, "auth", "token", "--hostname", "github.com")
		cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1")
		cmd.Stderr = io.Discard
		// A child gh leaves holding stdout would hold Output past the kill.
		cmd.WaitDelay = time.Second
		out, err := cmd.Output()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return out, err
	}
}
