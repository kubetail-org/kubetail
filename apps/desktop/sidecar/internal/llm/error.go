// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package llm

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ErrStreamIdle is a stream that sent nothing for longer than its dialect's bound.
var ErrStreamIdle = errors.New("llm: the stream went idle")

// Error is a provider's failure as the record keeps it: the provider's id, then
// what is known about it in fields that cannot carry the request.
//
// Never a response body's text, on any path. A body can echo the request — the
// conversation included — and redaction knows the keys, not a prompt.
type Error struct {
	ProviderID string
	// Status is the HTTP status. Zero when the call got no response, or when the
	// failure arrived as an event on a stream the API had already answered.
	Status int
	// Type is the API's own error type: rate_limit_error, invalid_request_error.
	Type string
	// Code is the dialect's classification of the failure, where it has one:
	// CodeContextLengthExceeded. The Messages API's error shape has no code of its own.
	Code string
	// Kind names a failure the API did not describe: the transport's, or a reply
	// that could not be read.
	Kind string

	// Class is what Code means, for a code the provider's row defines.
	Class CodeClass
	// cause is for the caller's own branching, never for the text.
	cause error
}

// CodeClass is what an error code a provider defines means.
type CodeClass int

const (
	// CodeOther is a code with no meaning the app acts on.
	CodeOther CodeClass = iota
	// CodeUsageLimit is a plan's usage limit: nothing is billed elsewhere, and
	// nothing retries.
	CodeUsageLimit
)

// CodeContextLengthExceeded is the request the model could not read whole.
const CodeContextLengthExceeded = "context_length_exceeded"

// ContextFull reports whether the provider refused to read the request.
func (e *Error) ContextFull() bool { return e.Code == CodeContextLengthExceeded }

// UsageLimit reports whether the provider refused the request at a plan's limit.
func (e *Error) UsageLimit() bool { return e.Class == CodeUsageLimit }

// ResponseError is a failure the API described. Its class is what p's row says
// the code means.
func ResponseError(p Provider, status int, typ, code string) *Error {
	return &Error{ProviderID: p.ID, Status: status, Type: typ, Code: code, Class: p.Codes[code]}
}

// TransportError is a failure with no response at all — a connection refused, a
// TLS failure, a timeout. Its cause names a host and never a body, so it is logged
// here, the one place it is read; the text keeps the kind alone.
func TransportError(providerID string, err error) *Error {
	slog.Error("provider transport failure", "provider", providerID, "err", err)
	return &Error{ProviderID: providerID, Kind: kindOf(err), cause: err}
}

// SignError is a Signer that could not sign: nothing was sent. Its cause is
// logged here, where it is read, unless it says it all: ErrSignedOut,
// ErrCredentialChanged, or a context's end while the signer waited, which ends
// the request and says nothing of the credential.
func SignError(providerID string, err error) *Error {
	switch {
	case errors.Is(err, context.Canceled):
		return &Error{ProviderID: providerID, Kind: "cancelled", cause: err}
	case errors.Is(err, context.DeadlineExceeded):
		return &Error{ProviderID: providerID, Kind: "timed out", cause: err}
	case errors.Is(err, ErrSignedOut):
		return &Error{ProviderID: providerID, Kind: "signed out", cause: err}
	case errors.Is(err, ErrCredentialChanged):
		return &Error{ProviderID: providerID, Kind: "credential changed", cause: err}
	}
	slog.Error("provider credential unavailable", "provider", providerID, "err", err)
	return &Error{ProviderID: providerID, Kind: "credential unavailable", cause: err}
}

// ReadError is a reply that could not be read: a body that is not the API's error
// shape, an event the SDK could not decode, a block out of order. The cause is the
// response's own bytes, which can echo the conversation, so it is neither kept nor
// logged.
func ReadError(providerID string) *Error {
	return &Error{ProviderID: providerID, Kind: "unreadable reply"}
}

// IncompleteError is a stream that ended clean before the reply did: no stop
// reason arrived. Its cause is io.ErrUnexpectedEOF, for a caller that branches on
// it.
func IncompleteError(providerID string) *Error {
	return &Error{ProviderID: providerID, Kind: "incomplete reply", cause: io.ErrUnexpectedEOF}
}

func (e *Error) Error() string {
	parts := []string{e.ProviderID + ":"}
	if e.Status > 0 {
		parts = append(parts, strconv.Itoa(e.Status))
	}
	for _, s := range []string{e.Type, e.Code, e.Kind} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

func (e *Error) Unwrap() error { return e.cause }

// kindOf is all a failure with no response says about itself.
func kindOf(err error) string {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	return "connection failed"
}

// noResponse reports a failure that got no response: a dial, a DNS lookup, a
// handshake, a timeout. The client wraps every failure in a *url.Error, so it is
// what is inside that decides: a response the client could not parse is wrapped
// the same way and carries the response's bytes in its text.
func noResponse(err error) bool {
	if urlErr, ok := errors.AsType[*url.Error](err); ok {
		err = urlErr.Err
	}
	var netErr net.Error
	var tlsErr *tls.CertificateVerificationError
	return errors.As(err, &netErr) || errors.As(err, &tlsErr)
}
