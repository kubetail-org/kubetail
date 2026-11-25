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

package runproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

// maxQueued is how many requests may wait for the one being decided, as many
// as the cluster's writes.
const maxQueued = 8

// What Decide answers a request it forwards nothing for.
const (
	refusedUnshowable = "kubetail: this request cannot be shown as it would be sent, so it was not sent. Run the command outside the sandbox."
	refusedQuery      = "kubetail: this request's query does not parse, so it cannot be shown as it would be sent."
	refusedForm       = "kubetail: this request's form body does not parse, so it cannot be shown as it would be sent."
	refusedDenied     = "kubetail: the user did not approve this request."
	refusedUnanswered = "kubetail: the user did not answer this request."
	refusedUnrecorded = "kubetail: this request could not be recorded, so it was not sent."
	refusedQueued     = "kubetail: too many requests are waiting on the user. Send one at a time."
	refusedOpen       = "kubetail: too many requests are open at once. Close one, then try again."
	unauthorized      = "kubetail: unauthorized"
)

// Request is one classified action, held for the user or decided unasked.
type Request struct {
	Action permissions.Action
	// Grantable is whether an answer may write a rule that allows it.
	Grantable bool
	// CommandRule and ChatRule are the rules each allow answer adds, as
	// Rule.Line draws them; empty when the action is not grantable.
	CommandRule string
	ChatRule    string
	Sent        Sent
}

// Sent is the request as the user is shown it.
type Sent struct {
	Host        string // the upstream host dialled
	Method      string
	Path        string // path and raw query, as forwarded
	ContentType string
	Body        []byte // as sent; empty with Unshown
	// Unshown is the cloud platform's one fixed sentence for a body the request
	// does not show.
	Unshown string
	Details []Detail
}

// Detail is one line the cloud platform's classifier read from the request.
type Detail struct {
	Label string
	Value string
}

// Asker puts a request to the user and records one decided unasked. From
// Ask, a context error is a wait that ended with no decision; any other error
// is a request or answer that could not be recorded. From Record, an error is
// a record that could not be written; reason is the decision in the user's
// words.
type Asker interface {
	Ask(ctx context.Context, r Request) (permissions.Answer, error)
	Record(ctx context.Context, r Request, d permissions.Decision, reason string) error
}

// Outcome is how Decide ended.
type Outcome struct {
	Forwarded bool   // forward was called
	Err       error  // forward's error
	Status    int    // what to answer when nothing was forwarded
	Message   string // the refusal in the user's words, starting "kubetail:"
}

func refused(status int, message string) Outcome {
	return Outcome{Status: status, Message: message}
}

// waiting tells waitersMoved, when set, that delta requests started or stopped
// waiting for the lock.
func (g *Grant) waiting(delta int) {
	if g.waitersMoved != nil {
		g.waitersMoved(delta)
	}
}

// Decide decides req and calls forward once it may run. It stamps the
// grant's target and its session's kind on the action and keeps every other
// field the classifier filled. A grant with no asker, and a request that
// cannot be shown, are refused before any queue, so what is recorded is what
// was sent. The rest wait for the grant's lock, so the user sees one at a time
// and the cloud platform sees changes in the order decided. The verdict is
// the session's policy for the action's account plus the command's rules: an
// allow is recorded, then forwarded; a deny is recorded and refused; an ask
// goes to the user. A data read gives up the lock once it may run, so a long
// download holds no other request; a change holds it until forward returns.
// Only what runs takes a slot and the limiter, so a request waiting on the
// user holds neither.
func (g *Grant) Decide(ctx context.Context, req Request, forward func() error) Outcome {
	req.Action.Target, req.Action.Principal = g.target, g.session.Kind
	req.Grantable, req.CommandRule, req.ChatRule = false, "", ""
	if g.asker == nil {
		return refused(http.StatusForbidden, g.refusal)
	}
	if why := unshowable(req.Sent); why != "" {
		return refused(http.StatusForbidden, why)
	}
	if !g.waiters.TryAcquire(1) {
		return refused(http.StatusTooManyRequests, refusedQueued)
	}
	g.waiting(1)
	err := g.lock.Acquire(ctx, 1)
	g.waiting(-1)
	g.waiters.Release(1)
	if err != nil {
		return refused(http.StatusForbidden, refusedUnanswered)
	}
	unlock := sync.OnceFunc(func() { g.lock.Release(1) })
	defer unlock()
	v, reason := g.policy(ctx, req.Action).Authorize(req.Action)
	switch d := v.Outcome(); d {
	case permissions.Allowed:
		if err := g.asker.Record(ctx, req, d, reason); err != nil {
			slog.Warn("an allowed cloud platform request was not recorded", "err", err)
			return refused(http.StatusForbidden, refusedUnrecorded)
		}
	case permissions.Denied:
		if err := g.asker.Record(ctx, req, d, reason); err != nil {
			slog.Warn("a refused cloud platform request was not recorded", "err", err)
			return refused(http.StatusForbidden, refusedUnrecorded)
		}
		return refused(http.StatusForbidden, "kubetail: "+req.Action.Summary+" is not allowed: "+reason)
	default:
		if v.Grantable {
			req.Grantable = true
			req.CommandRule, req.ChatRule = permissions.GrantLines(req.Action)
		}
		answer, err := g.asker.Ask(ctx, req)
		switch {
		case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
			return refused(http.StatusForbidden, refusedUnanswered)
		case err != nil:
			return refused(http.StatusForbidden, refusedUnrecorded)
		case !answer.Approved:
			return refused(http.StatusForbidden, refusedDenied)
		}
		if answer.Duration == permissions.DurationCommand && req.Grantable {
			g.commandRules = append(g.commandRules, permissions.CommandRule(req.Action))
		}
	}
	if req.Action.AccessLevel == permissions.DataRead {
		unlock()
	}
	release, status := g.Slot(ctx)
	if status != 0 {
		return refused(status, SlotRefusal(status))
	}
	defer release()
	return Outcome{Forwarded: true, Err: forward()}
}

// policy is the session's policy for act's account, read now, so a change
// made meanwhile applies. Called under the lock, which guards the command's
// rules.
func (g *Grant) policy(ctx context.Context, act permissions.Action) permissions.Policy {
	return g.session.PolicyFor(ctx, g.target, act.Account(), g.commandRules)
}

// unshowable is why s cannot be put to the user, or "": a body that is not
// UTF-8 text and has no Unshown sentence, or a query or form body
// url.ParseQuery refuses, since a pair that does not parse is dropped and what
// is shown would not be what runs.
func unshowable(s Sent) string {
	if s.Unshown == "" && !utf8.Valid(s.Body) {
		return refusedUnshowable
	}
	_, query, _ := strings.Cut(s.Path, "?")
	if _, err := url.ParseQuery(query); err != nil {
		return refusedQuery
	}
	if s.Unshown == "" && IsForm(s.ContentType) {
		if _, err := url.ParseQuery(string(s.Body)); err != nil {
			return refusedForm
		}
	}
	return ""
}

// AnyJSONString is whether body is not one JSON value, or hit holds for any of
// its keys or strings, read token by token so a duplicate key or an escape
// cannot hide one.
func AnyJSONString(body []byte, hit func(string) bool) bool {
	// A decoder answers io.EOF for a value cut short, so the whole is checked
	// first.
	if !json.Valid(body) {
		return true
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	for {
		tok, err := dec.Token()
		if err != nil { // io.EOF: json.Valid passed the body, so it is read whole
			return false
		}
		if s, ok := tok.(string); ok && hit(s) {
			return true
		}
	}
}

// IsForm is whether contentType names a URL-encoded form, read as leniently as
// a cloud platform reads it: parameters and case ignored.
func IsForm(contentType string) bool {
	mediaType, _, _ := strings.Cut(contentType, ";")
	return strings.EqualFold(strings.TrimSpace(mediaType), "application/x-www-form-urlencoded")
}
