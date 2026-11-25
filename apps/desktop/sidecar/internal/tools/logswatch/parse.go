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

package logswatch

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/logsinput"
)

// A watch's bounds: the lines that end it, at most maxLimit and one when the call
// names none; and how long it runs at most, from its start.
const (
	maxLimit     = 100
	defaultLimit = 1
	maxWatch     = 24 * time.Hour
)

// The forms since and until take, which a refusal names with the clock.
const (
	sinceForms = "an RFC 3339 time or an ISO 8601 duration back from now such as PT10M, not after now"
	untilForms = "an RFC 3339 time or an ISO 8601 duration ahead of now such as PT2H, after now"
)

// input is one call's arguments: the scope's, and where the watch starts, since,
// now for none, or the cursor a read printed, nil for none; until is when the
// watch ends on time alone, and capped whether that was cut to maxWatch; then the
// limits.
type input struct {
	logsinput.Input
	since  time.Time
	cursor *string
	until  time.Time
	capped bool

	limit, maxBytes, maxMessageBytes int
}

// parse reads the call's arguments, the shared ones through logsinput, and reads
// the window on the clock now.
func parse(raw json.RawMessage, now time.Time) (input, error) {
	in := input{limit: defaultLimit, maxBytes: logsinput.SampleBudget, maxMessageBytes: logsinput.DefaultMessageBytes}
	var since, until, cursor string
	shared, err := logsinput.Parse(raw, logsinput.Fields{
		"since": &since, "until": &until, "cursor": &cursor, "limit": &in.limit, "max_bytes": &in.maxBytes,
		"max_message_bytes": &in.maxMessageBytes,
		"anchor":            logsinput.BadInput("anchor", "a watch takes since and until"),
		"pin_to_end":        logsinput.BadInput("pin_to_end", "a watch opens no viewer"),
		"page_from":         logsinput.BadInput("page_from", "a watch reads its lines in order"),
	})
	if err != nil {
		return input{}, err
	}
	in.Input = shared
	switch {
	case in.limit < 1 || in.limit > maxLimit:
		return input{}, logsinput.BadInput("limit", fmt.Sprintf("1 to %d", maxLimit))
	case in.maxBytes < 1 || in.maxBytes > logsinput.SampleBudget:
		return input{}, logsinput.BadInput("max_bytes", fmt.Sprintf("1 to %d", logsinput.SampleBudget))
	}
	if in.until, in.capped, err = untilOf(until, now); err != nil {
		return input{}, err
	}
	if err := logsinput.CheckMessageBytes(in.maxMessageBytes); err != nil {
		return input{}, err
	}
	if cursor != "" {
		if in.NamesScope() || since != "" {
			return input{}, logsinput.BadInput("cursor", "a token already names the sources, filters, grep and where the watch starts: send it without them")
		}
		in.cursor = &cursor
		return in, nil
	}
	if len(in.Sources) == 0 {
		return input{}, logsinput.BadInput("sources", "name sources, or a next or follow token a read printed")
	}
	in.since, err = sinceOf(since, now)
	return in, err
}

// sinceOf is where the window s names starts on the clock now: now for none, else
// a moment not after now.
func sinceOf(s string, now time.Time) (time.Time, error) {
	if s == "" {
		return now, nil
	}
	at, err := logsinput.MomentOf(s, now, logsinput.Since, logsinput.Behind)
	if err == nil && at.After(now) {
		err = errAfterNow
	}
	if err != nil {
		return time.Time{}, logsinput.BadInput("since", logsinput.MomentMessage(err, sinceForms, now))
	}
	return at, nil
}

// errAfterNow is a since past now or an until not after it.
var errAfterNow = errors.New("on the wrong side of now")

// untilOf is when the watch ends on time alone: the moment s names after now, cut
// to maxWatch from now with capped set; maxWatch from now for none.
func untilOf(s string, now time.Time) (time.Time, bool, error) {
	limit := now.Add(maxWatch)
	if s == "" {
		return limit, false, nil
	}
	at, err := logsinput.MomentOf(s, now, logsinput.Until, logsinput.Ahead)
	if err == nil && !at.After(now) {
		err = errAfterNow
	}
	if err != nil {
		return time.Time{}, false, logsinput.BadInput("until", logsinput.MomentMessage(err, untilForms, now))
	}
	if at.After(limit) {
		return limit, true, nil
	}
	return at, false, nil
}
