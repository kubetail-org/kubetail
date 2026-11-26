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

// Package gcpauth holds the user's Google Cloud access token: the active
// gcloud account's, read outside the sandbox by running gcloud's
// config-helper, as gke-gcloud-auth-plugin does, and kept in memory.
package gcpauth

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
)

// The ways a read fails, each worded by its caller. None holds the helper's
// output.
var (
	ErrNoCLI      = errors.New("gcloud is not installed")
	ErrNoAccount  = errors.New("gcloud has no signed-in account")
	ErrTimeout    = errors.New("gcloud did not answer in time")
	ErrUnreadable = errors.New("gcloud's answer could not be read")
)

const (
	// bound is how long one helper run may take.
	bound = 30 * time.Second
	// early is how long before its expiry a token is read again.
	early = time.Minute
	// failureLife is how long a failed read answers from the cache, so a
	// machine with no login runs the helper at most twice a minute.
	failureLife = 30 * time.Second
	// assumedLife is the life of a token whose answer names no expiry.
	assumedLife = 5 * time.Minute
)

// Token is the active account's access token and what it was read with.
type Token struct {
	Value   string
	Project string // core/project of the active configuration; "" for none
	Account string // core/account
	Expiry  time.Time
}

// Source answers the active gcloud account's access token and project.
type Source struct {
	run   func(ctx context.Context, refresh bool) ([]byte, error)
	now   func() time.Time
	after func(d time.Duration, f func()) // runs f once d has passed
	bound time.Duration
	group singleflight.Group

	mu       sync.Mutex
	token    Token // zero when none is held
	failure  error // the last read's failure, until failedAt+failureLife
	failedAt time.Time
	// refresh is set by a Drop and cleared by the next read that succeeds:
	// gcloud answers a token Google refused from its own cache until it is
	// told to refresh.
	refresh bool
}

// New is a Source over run, the helper's run, which forces a refresh when
// told to; nil runs gcloud off the sidecar's PATH.
func New(run func(ctx context.Context, refresh bool) ([]byte, error)) *Source {
	if run == nil {
		run = runner("gcloud")
	}
	return &Source{
		run: run, now: time.Now, bound: bound,
		after: func(d time.Duration, f func()) { time.AfterFunc(d, f) },
	}
}

// Token answers the held token, or reads one. Concurrent callers share one
// read. A caller whose ctx ends gets its error; the read goes on to its own
// bound and is kept for the next caller.
func (s *Source) Token(ctx context.Context) (Token, error) {
	if tok, ok, err := s.cached(); ok {
		return tok, err
	}
	if err := ctx.Err(); err != nil {
		return Token{}, err
	}
	ch := s.group.DoChan("", s.readShared)
	select {
	case r := <-ch:
		if r.Err != nil {
			return Token{}, r.Err
		}
		return r.Val.(Token), nil
	case <-ctx.Done():
		return Token{}, ctx.Err()
	}
}

// cached is the held token, or the failure still answered, and whether
// either is.
func (s *Source) cached() (Token, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	switch {
	case s.token.Value != "" && now.Before(s.token.Expiry.Add(-early)):
		return s.token, true, nil
	case s.failure != nil && now.Before(s.failedAt.Add(failureLife)):
		return Token{}, true, s.failure
	}
	return Token{}, false, nil
}

// Drop forgets token if it is still the one held, so the next call reads
// again, with a refresh.
func (s *Source) Drop(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token != "" && s.token.Value == token {
		s.forgetLater(s.token)
		s.token, s.refresh = Token{}, true
	}
}

// forgetLater keeps tok blanked in rendered text until it expires: a token
// let go may still be live, and in a request or an error on its way out.
func (s *Source) forgetLater(tok Token) {
	s.after(max(tok.Expiry.Sub(s.now()), 0), func() { safe.ForgetSecret(tok.Value) })
}

// readShared is the read concurrent callers share. A caller that found
// nothing cached may join after another's read has ended, so it looks again
// before it runs the helper.
func (s *Source) readShared() (any, error) {
	if tok, ok, err := s.cached(); ok {
		return tok, err
	}
	return s.read()
}

// read runs the helper under its own bound and keeps what it answers.
func (s *Source) read() (Token, error) {
	s.mu.Lock()
	refresh := s.refresh
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), s.bound)
	defer cancel()
	out, err := s.run(ctx, refresh)
	tok, err := parse(out, err, ctx.Err())
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.failure, s.failedAt = err, s.now()
		return Token{}, err
	}
	if tok.Expiry.IsZero() {
		tok.Expiry = s.now().Add(assumedLife)
	}
	if tok.Value != s.token.Value {
		if s.token.Value != "" {
			s.forgetLater(s.token)
		}
		safe.AddSecret(tok.Value)
	}
	s.token, s.failure, s.refresh = tok, nil, false
	return tok, nil
}

// helperAnswer is the part of config-helper's JSON a Token is read from.
type helperAnswer struct {
	Configuration struct {
		Properties struct {
			Core struct {
				Account string `json:"account"`
				Project string `json:"project"`
			} `json:"core"`
		} `json:"properties"`
	} `json:"configuration"`
	Credential struct {
		AccessToken string    `json:"access_token"`
		TokenExpiry time.Time `json:"token_expiry"`
	} `json:"credential"`
}

// parse is the Token in a run's output, or the sentinel for its failure:
// runErr is the run's error, ctxErr its bound's, which counts only for a run
// that failed.
func parse(out []byte, runErr, ctxErr error) (Token, error) {
	switch {
	case errors.Is(runErr, exec.ErrNotFound):
		return Token{}, ErrNoCLI
	case errors.Is(runErr, errTooLong):
		return Token{}, ErrUnreadable
	case runErr != nil && (ctxErr != nil || errors.Is(runErr, context.DeadlineExceeded)):
		return Token{}, ErrTimeout
	case runErr != nil:
		return Token{}, ErrNoAccount
	}
	var a helperAnswer
	if json.Unmarshal(out, &a) != nil {
		return Token{}, ErrUnreadable
	}
	if a.Credential.AccessToken == "" {
		return Token{}, ErrNoAccount
	}
	return Token{
		Value:   a.Credential.AccessToken,
		Project: a.Configuration.Properties.Core.Project,
		Account: a.Configuration.Properties.Core.Account,
		Expiry:  a.Credential.TokenExpiry,
	}, nil
}
