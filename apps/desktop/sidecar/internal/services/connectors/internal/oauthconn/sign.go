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

package oauthconn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oauth"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/internal/signer"
)

// session is a signed-in account's tokens. The access token is memory's alone:
// a launch refreshes at its first Sign.
type session struct {
	// id names the sign-in, in the row and in the keyring's entry.
	id       string
	clientID string
	refresh  string
	access   oauth.Token
	// unsaved is a rotated refresh token not yet in the keyring. Until it lands
	// Sign hands out no access token: the old refresh token is spent, and a crash
	// would lose the session without a sign.
	unsaved bool
}

// refresh is one refresh under way for the session of gen, which every caller
// of that session that arrives during it waits for. token and err are its
// answer, set before done closes.
type refresh struct {
	gen   uint64
	done  chan struct{}
	token string
	err   error
}

// anySession is the generation the service itself signs under: whichever
// session is current. A session's own generation is never 0.
const anySession = 0

// errOffResource is a request outside the one resource the tokens are for.
var errOffResource = errors.New("oauthconn: the request is not for the API this credential is for")

// Sign sets r's bearer to the current session's access token, refreshing it
// first when it has expired. It signs only the API base and what is under it.
func (s *Service) Sign(r *http.Request) error { return s.sign(r, anySession) }

// Refused drops the access token r carried, if it is still the current one, so
// the next Sign refreshes.
func (s *Service) Refused(r *http.Request) { s.refused(r, anySession) }

// Bind is a binding for the current session alone. With no session, during a
// sign-in included, it has ended for good: an attempt's generation becomes
// its session's, so a binding made during one would come alive later. While
// Restoring it is the session the restore publishes, never one connected
// after.
func (s *Service) Bind() llm.Signer {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.state.Phase {
	case connectors.Restoring:
		return signer.Pending(s.restored, s.ctx.Done(), s.restoredBinding)
	case connectors.Connected:
		return sessionBinding{s: s, gen: s.gen}
	}
	return signer.Ended{}
}

// restoredBinding is the binding of the session the restore published.
func (s *Service) restoredBinding() llm.Signer {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.restoredGen == 0 {
		return signer.Ended{}
	}
	return sessionBinding{s: s, gen: s.restoredGen}
}

// sessionBinding signs under one generation, and ends once it has moved.
type sessionBinding struct {
	s   *Service
	gen uint64
}

func (b sessionBinding) Sign(r *http.Request) error { return b.s.sign(r, b.gen) }
func (b sessionBinding) Refused(r *http.Request)    { b.s.refused(r, b.gen) }

// sessionLocked is the session gen names, nil when it has ended (s.mu held).
func (s *Service) sessionLocked(gen uint64) *session {
	if gen != anySession && gen != s.gen {
		return nil
	}
	return s.session
}

func (s *Service) sign(r *http.Request, gen uint64) error {
	if !signer.Covers(s.base, r.URL) {
		return errOffResource
	}
	if err := s.awaitRestore(r.Context()); err != nil {
		return err
	}
	tok, ref, err := s.accessToken(gen)
	if ref != nil {
		// The wait is the caller's to end; the refresh is not.
		select {
		case <-ref.done:
			tok, err = ref.token, ref.err
		case <-r.Context().Done():
			return r.Context().Err()
		}
	}
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "Bearer "+tok)
	return nil
}

// awaitRestore waits for the restore to publish. ctx ends the wait with its
// error, and Close with llm.ErrSignedOut.
func (s *Service) awaitRestore(ctx context.Context) error {
	select {
	case <-s.restored:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return llm.ErrSignedOut
	}
}

func (s *Service) refused(r *http.Request, gen uint64) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.sessionLocked(gen); sess != nil && sess.access.AccessToken == tok {
		safe.ForgetSecret(tok)
		sess.access = oauth.Token{}
	}
}

// accessToken is the access token of the session gen names while it is valid.
// Otherwise it answers the refresh to wait for, starting one when none is
// under way.
func (s *Service) accessToken(gen uint64) (string, *refresh, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessionLocked(gen)
	if sess == nil {
		return "", nil, llm.ErrSignedOut
	}
	if sess.unsaved {
		// The write is retried, never the refresh: the token it would spend is
		// the one in memory now.
		if err := saveEntry(s.keyring, s.refreshAccount(), sess.id, sess.refresh); err != nil {
			return "", nil, fmt.Errorf("oauthconn: save the rotated refresh token: %w", err)
		}
		sess.unsaved = false
	}
	if sess.access.Valid(time.Now()) {
		return sess.access.AccessToken, nil, nil
	}
	// An ended session's refresh may still be under way; it answers that
	// session alone.
	if s.refreshing == nil || s.refreshing.gen != s.gen {
		ref := &refresh{gen: s.gen, done: make(chan struct{})}
		s.refreshing = ref
		clientID, token := sess.clientID, sess.refresh
		s.wg.Go(func() { s.runRefresh(ref, clientID, token) })
	}
	return "", s.refreshing, nil
}

// runRefresh renews ref's session, under the service's own bound: a caller's
// cancel must not drop a token the issuer has already rotated.
func (s *Service) runRefresh(ref *refresh, clientID, refreshToken string) {
	ctx, cancel := context.WithTimeout(s.ctx, issuerTimeout)
	defer cancel()
	client, err := s.clientFor(ctx, clientID)
	var tok oauth.Token
	if err == nil {
		tok, err = client.Refresh(ctx, refreshToken)
	}
	if err == nil {
		safe.AddSecret(tok.AccessToken)
		safe.AddSecret(tok.RefreshToken)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	defer close(ref.done)
	if s.refreshing == ref {
		s.refreshing = nil
	}
	if ref.gen != s.gen {
		if err == nil {
			s.revokeLocked(ref.gen, clientID, tok)
		}
		ref.err = llm.ErrSignedOut
		return
	}
	if err != nil {
		ref.err = s.refreshFailedLocked(err)
		return
	}
	// The old refresh token is spent: memory holds the replacement from here,
	// whatever the keyring's write does.
	sess := s.session
	safe.ForgetSecret(sess.refresh)
	safe.ForgetSecret(sess.access.AccessToken)
	sess.refresh, sess.access, sess.unsaved = tok.RefreshToken, tok, true
	if err := saveEntry(s.keyring, s.refreshAccount(), sess.id, tok.RefreshToken); err != nil {
		slog.Error("oauth: could not save the rotated refresh token", "provider", s.p.ID, "err", err)
		ref.err = fmt.Errorf("oauthconn: save the rotated refresh token: %w", err)
		return
	}
	sess.unsaved = false
	ref.token = tok.AccessToken
}

// refreshFailedLocked is what a failed refresh answers its waiters. One the
// issuer refused ends the session; one that never reached it clears nothing,
// and the next Sign tries again (s.mu held).
func (s *Service) refreshFailedLocked(err error) error {
	tokErr, ok := errors.AsType[*oauth.TokenError](err)
	if !ok || !slices.Contains(s.p.SessionEnders, tokErr.Code) {
		slog.Warn("oauth: refresh failed", "provider", s.p.ID, "err", err)
		return fmt.Errorf("oauthconn: refresh: %w", err)
	}
	slog.Warn("oauth: the issuer ended the session", "provider", s.p.ID, "code", tokErr.Code)
	s.clearLocked(tokErr.Code == s.p.ClientGone)
	s.publishLocked(connectors.State{Phase: connectors.Failed, Failure: connectors.SessionEnded})
	return llm.ErrSignedOut
}
