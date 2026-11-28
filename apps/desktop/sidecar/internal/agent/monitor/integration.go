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

package monitor

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// Registration is one integration and what the core needs to know of it.
type Registration struct {
	Name        string        // as monitor_targets.integration holds it
	Cadence     time.Duration // how often one of its targets is read
	Signals     []Signal      // each signal it can find
	Integration Integration
	Triager     Triager // nil for an integration that cannot triage
}

// Integration lists and reads the targets of one system, such as Kubernetes.
type Integration interface {
	// ListTargets lists every target the integration has now, readable or not.
	ListTargets(ctx context.Context) ([]Target, error)
	// ListenForTargetChanges receives when ListTargets may answer differently.
	// Notices coalesce. The channel is nil for a listing that never changes,
	// and stops receiving when ctx ends.
	ListenForTargetChanges(ctx context.Context) <-chan struct{}
	// Read takes one reading of a target. An error is a reading that judges nothing.
	Read(ctx context.Context, t Target) (Reading, error)
}

// Target is one thing an integration reads.
type Target struct {
	Key   string // the integration's own; the core compares it and never parses it
	Label string // one line naming the target
}

// Signal is one state an integration finds true or not at each reading.
type Signal struct {
	Name         string        // carries its integration's prefix: kubernetes.crash_loop
	Label        string        // fixed text naming the signal
	OpenAfter    time.Duration // how long it holds before an occurrence starts
	ResolveAfter time.Duration // how long it is absent before the occurrence ends; zero ends it at the next judged reading
	Severity     Severity
}

// Severity is how loud a signal's incidents are.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
)

// Reading is what one reading of a target found: plain data, which the fold reads
// and calls nothing on.
type Reading struct {
	UID          string                   // what the target reached; "" while it cannot say
	Observations []Observation            // what is true now
	Unknown      map[string]ReadErrorCode // each signal this reading could not judge, by name, and why
}

// Observation is one signal holding for one subject.
type Observation struct {
	Signal  string // its name
	Subject Subject
	Detail  map[string]string // what the integration saw this time
}

// Subject is what a signal holds for, in its integration's own words.
type Subject struct {
	Key        string            // the integration's own; the core compares it and never parses it
	Label      string            // one line naming the subject
	Attributes map[string]string // the integration's own facts about the subject
}

// ReadErrorCode says why a signal could not be judged. The codes are the core's,
// and an integration maps its own failures onto them.
type ReadErrorCode string

const (
	CodeNotReady         ReadErrorCode = "not_ready"         // the target is still loading
	CodeDisabled         ReadErrorCode = "disabled"          // the user switched the read off
	CodeUnavailable      ReadErrorCode = "unavailable"       // the target did not answer
	CodePermissionDenied ReadErrorCode = "permission_denied" // the target refused the read
	CodeUnauthenticated  ReadErrorCode = "unauthenticated"   // the target refused the credentials
	CodeRateLimited      ReadErrorCode = "rate_limited"      // the target asked the reader to wait
	CodeUnsupported      ReadErrorCode = "unsupported"       // the target cannot answer this signal at all
	CodeTruncated        ReadErrorCode = "truncated"         // the answer was cut at its bound
	CodeTimeout          ReadErrorCode = "timeout"           // the reading outlived its cadence; the loop's
	CodeInvalid          ReadErrorCode = "invalid"           // the reading broke the core's bounds; the check's
)

// known is whether c is one of the core's codes: a code outside them has no
// value on the wire.
func (c ReadErrorCode) known() bool {
	switch c {
	case CodeNotReady, CodeDisabled, CodeUnavailable, CodePermissionDenied, CodeUnauthenticated,
		CodeRateLimited, CodeUnsupported, CodeTruncated, CodeTimeout, CodeInvalid:
		return true
	}
	return false
}

// ReadError is a failed reading and the code every signal reads under it.
type ReadError struct {
	Code ReadErrorCode
	Err  error
}

func (e *ReadError) Error() string {
	if e.Err == nil {
		return "monitor: reading " + string(e.Code)
	}
	return "monitor: reading " + string(e.Code) + ": " + e.Err.Error()
}

func (e *ReadError) Unwrap() error { return e.Err }

// codeOf is the code a failed reading carries: unavailable for one that carries
// none, invalid for one the core lacks.
func codeOf(err error) ReadErrorCode {
	re, ok := errors.AsType[*ReadError](err)
	switch {
	case !ok || re.Code == "":
		return CodeUnavailable
	case !re.Code.known():
		slog.Warn("monitor: a failed reading carries a code the core lacks", "code", string(re.Code))
		return CodeInvalid
	}
	return re.Code
}
