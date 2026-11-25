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

// Package refusal is why the logs service declines a read, as a value with a Code
// the wire and the tools pass through unchanged.
package refusal

import (
	"errors"
)

// Code is one of the block below: why a request was refused, or what a page or a
// follow wants its caller to know. The wire and the tools pass it through unchanged.
type Code string

// The codes refusals and notices carry.
const (
	// CodeCursorInvalid is a page cursor this version of the service did not write,
	// or a Seen entry that is not a line cursor.
	CodeCursorInvalid Code = "cursor-invalid"
	// CodeConflictingParameters is a request whose arguments do not go together.
	CodeConflictingParameters Code = "conflicting-parameters"
	// CodeInvalidRange is a Since after its Until.
	CodeInvalidRange Code = "invalid-range"
	// CodeBadGrep is a grep that does not compile, or one over MaxGrep.
	CodeBadGrep Code = "bad-grep"
	// CodeBadPage is a PageSize outside 0 to MaxPageSize, or a negative byte cap.
	CodeBadPage Code = "bad-page"
	// CodeBadPadding is a Padding past its limits.
	CodeBadPadding Code = "bad-padding"
	// CodeBadScope is a filter field or a source kind the service cannot read.
	CodeBadScope Code = "bad-scope"
	// CodeNotMirrored, CodeNoNodes and CodeTooMany are a scope the mirror refuses:
	// Unmirrored, NoNodes, and more streams than the service reads at once.
	CodeNotMirrored Code = "not-mirrored"
	CodeNoNodes     Code = "no-nodes"
	CodeTooMany     Code = "too-many"
	// CodeNoContainer is a container the tools' scope check finds in no pod. The
	// service never raises it.
	CodeNoContainer Code = "no-container"
	// CodeNoCache, CodeSyncing and CodeRevoked are a cluster with no cache, one still
	// syncing, and one Kubetail no longer connects to.
	CodeNoCache Code = "no-cache"
	CodeSyncing Code = "syncing"
	CodeRevoked Code = "revoked"
	// CodeReplaced is a connection replaced while the page was read: the one refusal
	// a caller sends again as it came.
	CodeReplaced Code = "replaced"
	// CodeUnauthorized is the cluster refusing to show a log.
	CodeUnauthorized Code = "unauthorized"
	// CodeFinished is a scope whose every container stopped, or whose Pod sources
	// are all gone.
	CodeFinished Code = "finished"
	// CodeReadFailed is a scope no container of which could be read.
	CodeReadFailed Code = "read-failed"
	// CodeTooLoud is a second of more than a mebibyte of log. It never refuses a
	// page: it reaches a caller inside a stream-skipped notice.
	CodeTooLoud Code = "too-loud"

	// CodeEmptyPadding is padding asked past an open bound that found no line.
	CodeEmptyPadding Code = "empty-padding"
	// CodePaddingShort is a counted Before that found fewer lines than it asked for.
	CodePaddingShort Code = "padding-short"
	// CodePageByteLimitExceeded is a page whose first line alone is over MaxPageBytes.
	CodePageByteLimitExceeded Code = "page-byte-limit-exceeded"
	// CodeStreamSkipped is a stream of the scope the page could not read.
	CodeStreamSkipped Code = "stream-skipped"
)

// Refusal is a read the service declines: its Code, which the wire and the tools
// pass through unchanged, and its Reason, in words the user can act on. It is a
// value, so a caller holding one cannot change the sentinels below.
type Refusal struct {
	Code   Code
	Reason string
}

func (r Refusal) Error() string { return r.Reason }

// New is a refusal of code with reason. Every refusal the package makes is
// built here, so none is made without a code.
func New(code Code, reason string) Refusal { return Refusal{Code: code, Reason: reason} }

// As is the Refusal err is, bare or inside Unmirrored or NoNodes: the one
// way a caller reads a refusal's Code.
func As(err error) (Refusal, bool) {
	var refusal Refusal
	var unmirrored Unmirrored
	var noNodes NoNodes
	switch {
	case errors.As(err, &refusal):
		return refusal, true
	case errors.As(err, &unmirrored):
		return unmirrored.Refusal, true
	case errors.As(err, &noNodes):
		return noNodes.Refusal, true
	}
	return Refusal{}, false
}

// Unmirrored is a view whose sources could never appear: the cache does not
// mirror the kind of the source at index Source, or pods at all when Source is -1.
type Unmirrored struct {
	Refusal
	Source int
}

// NoNodes is a filter on a node label over a mirror with no nodes.
type NoNodes struct{ Refusal }
