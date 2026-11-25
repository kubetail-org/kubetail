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

package signer

import (
	"context"
	"net/http"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
)

// recording is a binding that signs with a fixed header and counts refusals.
type recording struct{ refused int }

func (r *recording) Sign(req *http.Request) error {
	req.Header.Set("Authorization", "Bearer restored")
	return nil
}

func (r *recording) Refused(*http.Request) { r.refused++ }

func newRequest(t *testing.T, ctx context.Context) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.test/v1", nil)
	require.NoError(t, err)
	return req
}

// A pending binding signs once ready closes, with what resolve answers; until
// then a request's context ends the wait, and Refused reaches nothing.
func TestPendingWaitsForReady(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ready, done := make(chan struct{}), make(chan struct{})
		bound := &recording{}
		resolved := 0
		p := Pending(ready, done, func() llm.Signer { resolved++; return bound })

		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		assert.ErrorIs(t, p.Sign(newRequest(t, ctx)), context.Canceled)
		p.Refused(newRequest(t, t.Context()))
		assert.Zero(t, resolved, "nothing is resolved before ready")

		req := newRequest(t, t.Context())
		signed := make(chan error, 1)
		go func() { signed <- p.Sign(req) }()
		synctest.Wait()
		assert.Empty(t, signed, "Sign waits for ready")

		close(ready)
		require.NoError(t, <-signed)
		assert.Equal(t, "Bearer restored", req.Header.Get("Authorization"))
		p.Refused(req)
		assert.Equal(t, 1, bound.refused)
	})
}

// A Sign waiting with a live request ends signed out with its service.
func TestPendingEndsWithItsService(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ready, done := make(chan struct{}), make(chan struct{})
		p := Pending(ready, done, func() llm.Signer { return &recording{} })

		req := newRequest(t, t.Context())
		signed := make(chan error, 1)
		go func() { signed <- p.Sign(req) }()
		synctest.Wait()
		assert.Empty(t, signed, "Sign waits for ready")

		close(done)
		require.ErrorIs(t, <-signed, llm.ErrSignedOut)
		assert.Empty(t, req.Header.Get("Authorization"))
	})
}
