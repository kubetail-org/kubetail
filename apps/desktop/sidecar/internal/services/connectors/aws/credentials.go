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
	"errors"
	"fmt"
	"log/slog"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// ErrSignedOut is a request for credentials with no session.
var ErrSignedOut = errors.New("aws: not signed in to AWS")

// expiryWindow is how long before they expire credentials are refreshed, so
// none is handed out to expire in flight.
const expiryWindow = time.Minute

var _ sdkaws.CredentialsProvider = (*Service)(nil)

// Retrieve answers the session's credentials, refreshing them first when they
// are near expiry. One refresh runs at a time, under the service's own bound:
// a caller's cancel must not drop a refresh token AWS has already rotated.
// While Restoring it waits for the restore.
func (s *Service) Retrieve(ctx context.Context) (sdkaws.Credentials, error) {
	if err := s.awaitRestore(ctx); err != nil {
		return sdkaws.Credentials{}, err
	}
	s.refreshing.Lock()
	defer s.refreshing.Unlock()

	sess, gen, err := s.held()
	if err != nil {
		return sdkaws.Credentials{}, err
	}
	if !sess.creds.Expires.IsZero() && s.now().Before(sess.creds.Expires.Add(-expiryWindow)) {
		return sess.creds, nil
	}
	ctx, cancel := context.WithTimeout(s.ctx, issuerTimeout)
	defer cancel()
	tok, err := s.tokens.refresh(ctx, sess.key, sess.refresh)

	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.gen {
		tok.forget()
		return sdkaws.Credentials{}, ErrSignedOut
	}
	if err != nil {
		return sdkaws.Credentials{}, s.refreshFailedLocked(err)
	}
	// The old refresh token is spent: memory holds the replacement from here,
	// whatever the keyring's write does.
	s.session.forget()
	s.session.take(tok)
	if err := s.saveLocked(); err != nil {
		return sdkaws.Credentials{}, err
	}
	return s.session.creds, nil
}

// awaitRestore waits for the restore to publish. ctx ends the wait with its
// error, and Close with ErrSignedOut.
func (s *Service) awaitRestore(ctx context.Context) error {
	select {
	case <-s.restored:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return ErrSignedOut
	}
}

// held is the session as it stands, and its generation. A rotated refresh
// token still out of the keyring is saved first; the write is retried, never
// the refresh, since the token a refresh would spend is the one in memory.
func (s *Service) held() (session, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil {
		return session{}, 0, ErrSignedOut
	}
	if s.session.unsaved {
		if err := s.saveLocked(); err != nil {
			return session{}, 0, err
		}
	}
	return *s.session, s.gen, nil
}

// saveLocked writes the session's entry after a refresh, and marks the session
// unsaved until a write lands (s.mu held).
func (s *Service) saveLocked() error {
	err := saveEntry(s.keyring, s.session.entry())
	s.session.unsaved = err != nil
	if err != nil {
		slog.Error("aws: could not save the rotated refresh token", "err", err)
		return fmt.Errorf("aws: save the rotated refresh token: %w", err)
	}
	return nil
}

// refreshFailedLocked is what a failed refresh answers. One AWS refused for
// good ends the session; any other clears nothing, and the next Retrieve tries
// again (s.mu held).
func (s *Service) refreshFailedLocked(err error) error {
	if !sessionEnded(err) {
		slog.Warn("aws: refresh failed", "err", err)
		return fmt.Errorf("aws: refresh: %w", err)
	}
	slog.Warn("aws: AWS ended the session", "err", err)
	s.clearLocked() //nolint:errcheck // the session has ended either way, and clearLocked logs the delete
	s.publishLocked(connectors.State{Phase: connectors.Failed, Failure: connectors.SessionEnded})
	return ErrSignedOut
}
