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

package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureLog routes slog to a buffer for the test, at Debug so a stream's count
// of what it dropped is seen too.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// timeoutErr is a net.Error that timed out.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "dial tcp 10.0.0.1:443: i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

var _ net.Error = timeoutErr{}

func TestErrorRendersWhatTheAPIGave(t *testing.T) {
	err := ResponseError(Provider{ID: "anthropic"}, 429, "rate_limit_error", "")
	assert.EqualError(t, err, "anthropic: 429 rate_limit_error")

	full := ResponseError(Provider{ID: "anthropic"}, 400, "invalid_request_error", CodeContextLengthExceeded)
	assert.EqualError(t, full, "anthropic: 400 invalid_request_error context_length_exceeded")
	assert.True(t, full.ContextFull())
	assert.False(t, err.ContextFull())
}

func TestTransportErrorLogsItsCause(t *testing.T) {
	buf := captureLog(t)

	TransportError("anthropic", errors.New("dial tcp 10.0.0.1:443: connection refused"))

	assert.Contains(t, buf.String(), "connection refused")
	assert.Contains(t, buf.String(), "provider=anthropic")
}

func TestTransportErrorRendersTheKindAndNoAddress(t *testing.T) {
	captureLog(t)
	err := TransportError("anthropic", errors.New("dial tcp 10.0.0.1:443: connection refused"))

	assert.EqualError(t, err, "anthropic: connection failed")
}

func TestTransportErrorNamesATimeout(t *testing.T) {
	captureLog(t)
	err := TransportError("anthropic", timeoutErr{})

	assert.EqualError(t, err, "anthropic: timeout")
}

func TestTransportErrorKeepsItsCause(t *testing.T) {
	captureLog(t)
	cause := errors.New("dial")
	err := TransportError("anthropic", cause)

	require.ErrorIs(t, err, cause)
}

// A signer that waited until its request's context ended failed nothing of the
// credential: no error is logged, and the kind names the end.
func TestSignErrorLogsNoEndedContext(t *testing.T) {
	for _, c := range []struct {
		err  error
		kind string
	}{
		{context.Canceled, "cancelled"},
		{fmt.Errorf("oauthconn: refresh: %w", context.DeadlineExceeded), "timed out"},
	} {
		t.Run(c.kind, func(t *testing.T) {
			buf := captureLog(t)

			err := SignError("chatgpt", c.err)

			assert.Equal(t, c.kind, err.Kind)
			assert.ErrorIs(t, err, c.err)
			assert.Empty(t, buf.String())
		})
	}
}

func TestSignErrorLogsAnUnavailableCredential(t *testing.T) {
	buf := captureLog(t)

	err := SignError("chatgpt", errors.New("the keyring is locked"))

	assert.Equal(t, "credential unavailable", err.Kind)
	assert.Contains(t, buf.String(), "level=ERROR")
	assert.Contains(t, buf.String(), "provider credential unavailable")
}

// The cause of an unreadable reply is the reply's own bytes, so nothing of it
// survives: not in the text, not in the chain, not in the log.
func TestIncompleteErrorIsAnUnexpectedEOF(t *testing.T) {
	err := IncompleteError("anthropic")

	assert.EqualError(t, err, "anthropic: incomplete reply")
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestReadErrorKeepsNoCauseAndLogsNothing(t *testing.T) {
	buf := captureLog(t)

	err := ReadError("anthropic")

	assert.EqualError(t, err, "anthropic: unreadable reply")
	assert.NoError(t, errors.Unwrap(err))
	assert.Empty(t, buf.String())
}
