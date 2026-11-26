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

package kubeproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"sigs.k8s.io/yaml"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

const (
	// maxQueuedWrites is how many writes may wait for the one ahead of them.
	maxQueuedWrites = 8
	// maxWriteBody is the largest change a user can be asked to read, below the
	// API server's own 3 MiB.
	maxWriteBody = 1 << 20
)

// showableTypes are the media types of a body the user reads as sent.
var showableTypes = map[string]bool{
	"application/json":                       true,
	"application/merge-patch+json":           true,
	"application/strategic-merge-patch+json": true,
	"application/json-patch+json":            true,
	"application/apply-patch+yaml":           true,
}

// redactedMarks are what the sandbox reads in place of a redacted value: the
// text, and its base64 as a Secret's data holds it.
var redactedMarks = []string{safe.Redacted, redactedData}

// releasePrefix names every helm release Secret.
const releasePrefix = "sh.helm.release.v1."

// Write is one held request, as the user will see it.
type Write struct {
	Method      string
	Path        string // path and raw query, as forwarded
	Subresource string // the path's subresource as the policy parsed it; "" for none
	ContentType string
	Body        []byte // valid UTF-8, or empty
	DryRun      bool   // a POST, PUT or PATCH whose every dryRun is All
}

// Request is one classified action held for the user, or decided with nobody
// asked.
type Request struct {
	Action permissions.Action
	// Grantable is whether an answer may write a rule that allows it.
	Grantable bool
	// CommandRule and ChatRule are the rules each allow answer adds, as
	// Rule.Line draws them; empty when the action is not grantable.
	CommandRule string
	ChatRule    string
	// Write is the request as sent; nil for an action with none.
	Write *Write
	// Diff is the change as a unified diff of YAML; "" for none.
	Diff string
	// DiffCut is a diff that stops short of the whole change.
	DiffCut bool
	// DiffError is why there is no diff, when one was looked for.
	DiffError string
}

// Asker puts a request to the user, and records one decided with nobody
// asked. From Ask, a context error is a wait that ended without a decision,
// which the asker records as abandoned; any other error is a request or
// decision the asker could not record. From Record, an error is a record the
// asker could not write; reason is the decision in the user's words.
type Asker interface {
	Ask(ctx context.Context, r Request) (permissions.Answer, error)
	Record(ctx context.Context, r Request, d permissions.Decision, reason string) error
}

// serveWrite decides a write the policy passed: forwards it when the policy
// allows it, refuses it when the policy denies it, and otherwise puts it to
// the user and forwards it once approved. Each one decided with nobody asked
// is recorded first.
func (g *Grant) serveWrite(w http.ResponseWriter, r *http.Request, p apiPath) {
	// What needs no body is refused before the queue, so a burst of them reads
	// its own refusal.
	if g.asker == nil {
		writeStatus(w, http.StatusForbidden, g.refusal)
		return
	}
	// A pair that does not parse is dropped on its way to the API server, so the
	// query shown would not be the query that runs: a selector could vanish.
	if _, err := url.ParseQuery(r.URL.RawQuery); err != nil {
		writeStatus(w, http.StatusForbidden, string(refusedQuery))
		return
	}
	// Held until the forward returns, so the cluster sees writes in the order
	// they were decided, and one wait on the user at a time.
	if !g.takeWriteLock(r.Context(), w) {
		return
	}
	defer g.writeLock.Release(1)
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWriteBody))
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		writeStatus(w, http.StatusForbidden, string(refusedTooLarge))
		return
	}
	if err != nil {
		return
	}
	// helm rebuilds a release from the Secrets it read, so one this command
	// read redacted, in the namespace or across the cluster, would be written
	// back as [redacted]. Ahead of the body check, which would refuse the marks
	// inside such a release without saying what allows them.
	release := writesRelease(r, p, body)
	if release && (g.redactedReads[p.namespace] || g.redactedReads[""]) {
		writeStatus(w, http.StatusForbidden, string(refusedHelm))
		return
	}
	if why := checkBody(r, body, release); why != pass {
		writeStatus(w, http.StatusForbidden, string(why))
		return
	}
	act := g.classify(r, p, body)
	write := Write{
		Method: r.Method, Path: r.URL.RequestURI(), Subresource: p.subresource,
		ContentType: r.Header.Get("Content-Type"), Body: body, DryRun: act.DryRun,
	}
	// A read, a dry run the API server honors, changes nothing: it is
	// forwarded unasked and unrecorded, as a read is.
	if act.AccessLevel == permissions.Read {
		g.forward(w, r, p, body, p.onSecrets())
		return
	}
	v, reason := g.policy(r.Context()).Authorize(act)
	req := newRequest(act, v, &write)
	switch d := v.Outcome(); d {
	case permissions.Allowed:
		if err := g.asker.Record(r.Context(), req, d, reason); err != nil {
			writeStatus(w, http.StatusForbidden, string(refusedUnrecorded))
			return
		}
		g.forward(w, r, p, body, p.onSecrets())
		return
	case permissions.Denied:
		// Nothing runs either way, so the refusal does not wait on the record.
		if err := g.asker.Record(r.Context(), req, d, reason); err != nil {
			slog.Warn("a refused cluster write was not recorded", "err", err)
		}
		writeStatus(w, http.StatusForbidden, "kubetail: "+act.Summary+" is not allowed: "+reason)
		return
	}
	pv := g.previewChange(r, p, body)
	req.Diff, req.DiffCut, req.DiffError = pv.diff, pv.cut, pv.err
	answer, err := g.ask(r.Context(), req)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		writeStatus(w, http.StatusForbidden, string(refusedUnanswered))
		return
	}
	if err != nil {
		writeStatus(w, http.StatusForbidden, string(refusedUnrecorded))
		return
	}
	if !answer.Approved {
		writeStatus(w, http.StatusForbidden, string(refusedDenied))
		return
	}
	g.forward(w, r, p, body, p.onSecrets())
}

// ask puts req to the user, and an approval for the command adds its rule for
// the rest of the command. Called under the write lock, which guards the
// command's rules.
func (g *Grant) ask(ctx context.Context, req Request) (permissions.Answer, error) {
	answer, err := g.asker.Ask(ctx, req)
	if err == nil && answer.Approved && answer.Duration == permissions.DurationCommand && req.Grantable {
		g.commandRules = append(g.commandRules, permissions.CommandRule(req.Action))
	}
	return answer, err
}

// newRequest is act held for the user under verdict v, with the rule each
// allow answer adds when an answer may add one.
func newRequest(act permissions.Action, v permissions.Verdict, write *Write) Request {
	req := Request{Action: act, Write: write}
	if v.Grantable {
		req.Grantable = true
		req.CommandRule, req.ChatRule = permissions.GrantLines(act)
	}
	return req
}

// classify is classify's action for the grant's cluster, sent by the grant's
// session's kind.
func (g *Grant) classify(r *http.Request, p apiPath, body []byte) permissions.Action {
	act := classify(r, p, body, g.cluster, g.context)
	act.Principal = g.session.Kind
	return act
}

// policy is the session's policy for the grant's cluster, read now, so a
// change made meanwhile applies. Called under the write lock.
func (g *Grant) policy(ctx context.Context) permissions.Policy {
	return g.session.PolicyFor(ctx, permissions.Kubernetes, string(g.cluster), g.commandRules)
}

// takeWriteLock waits for the write lock, as one of at most maxQueuedWrites
// writes and Secret reads, and answers false, having answered w, when the queue
// is full or ctx ends first. A write's body is read only once the lock is held,
// so a queued write holds nothing but its connection.
func (g *Grant) takeWriteLock(ctx context.Context, w http.ResponseWriter) bool {
	if !g.writeWaiters.TryAcquire(1) {
		// No Retry-After, so client-go does not retry it.
		writeStatus(w, http.StatusTooManyRequests, "kubetail: too many requests are waiting on the user. Send one at a time.")
		return false
	}
	defer g.writeWaiters.Release(1)
	g.waiting(1)
	defer g.waiting(-1)
	return g.writeLock.Acquire(ctx, 1) == nil
}

// waiting tells waitersMoved, when set, that delta writes started or stopped
// waiting for the lock.
func (g *Grant) waiting(delta int) {
	if g.waitersMoved != nil {
		g.waitersMoved(delta)
	}
}

// checkBody is why a write's body cannot be put to the user, or pass. A DELETE
// may carry none, and then has no type to check.
func checkBody(r *http.Request, body []byte, release bool) refusal {
	if r.Header.Get("Content-Encoding") != "" || !utf8.Valid(body) {
		return refusedUnshowable
	}
	if len(body) == 0 && r.Method == http.MethodDelete {
		return pass
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !showableTypes[mediaType] {
		return refusedUnshowable
	}
	if len(body) > 0 {
		value, err := decodeBody(mediaType, body)
		if err != nil {
			return refusedUnshowable
		}
		// decodeBody keeps a repeated key's last value, where the API server's
		// typed decoder merges repeated objects, so a body repeating one would
		// be classified and shown as other than it runs.
		if !uniqueKeys(mediaType, body) {
			return refusedRepeatedKey
		}
		if holdsMark(value) {
			return refusedRedacted
		}
		if release {
			return checkRelease(mediaType, value)
		}
	}
	return pass
}

// checkRelease is why a release write's body cannot be sent, or pass. helm's
// encoding hides a mark from holdsMark, so the release is decoded and read
// inside; a body that could set it any other way is refused.
func checkRelease(mediaType string, value any) refusal {
	if mediaType == "application/json-patch+json" {
		return refusedUnshowable
	}
	obj, _ := value.(map[string]any)
	if _, ok := obj["stringData"]; ok {
		return refusedUnshowable
	}
	data, _ := obj["data"].(map[string]any)
	raw, ok := data["release"]
	if !ok {
		return pass
	}
	encoded, _ := raw.(string)
	release, err := decodeRelease(encoded)
	if err != nil {
		return refusedUnshowable
	}
	for _, field := range release {
		v, err := decodeBody("application/json", field)
		if err != nil { // coverage:ignore each field is a json.RawMessage that decodeRelease's Unmarshal validated
			return refusedUnshowable
		}
		if holdsMark(v) {
			return refusedRedacted
		}
	}
	return pass
}

// decodeBody is body as the API server decodes it for its media type: an apply
// patch through sigs.k8s.io/yaml, as the server reads one, anything else as one
// JSON value. An escape is decoded, so a mark it spells differently is found.
func decodeBody(mediaType string, body []byte) (any, error) {
	if mediaType == "application/apply-patch+yaml" {
		converted, err := yaml.YAMLToJSON(body)
		if err != nil {
			return nil, err
		}
		body = converted
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	if !atEnd(dec) {
		return nil, errors.New("kubeproxy: a body holds trailing data after its value")
	}
	return value, nil
}

// uniqueKeys is whether body parses with no object repeating a key.
func uniqueKeys(mediaType string, body []byte) bool {
	if mediaType == "application/apply-patch+yaml" {
		_, err := yaml.YAMLToJSONStrict(body)
		return err == nil
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	return uniqueKeysIn(dec) && atEnd(dec)
}

// atEnd is whether dec holds nothing but whitespace past the value it read.
// dec.More is not enough: it is false before a stray ']' or '}'.
func atEnd(dec *json.Decoder) bool {
	_, err := dec.Token()
	return err == io.EOF
}

// uniqueKeysIn reads the next JSON value off dec, false if it does not parse or
// an object in it repeats a key.
func uniqueKeysIn(dec *json.Decoder) bool {
	tok, err := dec.Token()
	if err != nil {
		return false
	}
	switch tok {
	case json.Delim('{'):
		seen := map[string]bool{}
		for dec.More() {
			key, err := dec.Token()
			if err != nil || seen[key.(string)] {
				return false
			}
			seen[key.(string)] = true
			if !uniqueKeysIn(dec) {
				return false
			}
		}
	case json.Delim('['):
		for dec.More() {
			if !uniqueKeysIn(dec) {
				return false
			}
		}
	default:
		return true
	}
	_, err = dec.Token()
	return err == nil
}

// holdsMark is whether any string in value, a key included, holds a redacted
// mark.
func holdsMark(value any) bool {
	switch v := value.(type) {
	case string:
		for _, mark := range redactedMarks {
			if strings.Contains(v, mark) {
				return true
			}
		}
	case map[string]any:
		for k, item := range v {
			if holdsMark(k) || holdsMark(item) {
				return true
			}
		}
	case []any:
		if slices.ContainsFunc(v, holdsMark) {
			return true
		}
	}
	return false
}

// writesRelease is whether r writes a helm release Secret: a PUT, PATCH or
// DELETE of one by name, or a POST to secrets of one.
func writesRelease(r *http.Request, p apiPath, body []byte) bool {
	if !p.onSecrets() {
		return false
	}
	if r.Method == http.MethodPost {
		return mayBeRelease(body)
	}
	return strings.HasPrefix(p.name, releasePrefix)
}

// mayBeRelease is whether body may be a helm release: anything but a JSON
// object whose type is a string other than a release's. The keys are matched
// exactly, as the API server matches them.
func mayBeRelease(body []byte) bool {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil || obj == nil {
		return true
	}
	var typ string
	if raw, ok := obj["type"]; ok && json.Unmarshal(raw, &typ) != nil {
		return true
	}
	return typ == helmReleaseType
}

// isDryRun is whether r asks the API server for a dry run, read strictly since
// the request claims it: a POST, PUT or PATCH whose query names dryRun, and
// every value All. The API server reads a DELETE's options from its body when
// it has one, and ignores the query then, so a DELETE is never one.
func isDryRun(r *http.Request) bool {
	if r.Method == http.MethodDelete {
		return false
	}
	values := r.URL.Query()["dryRun"]
	for _, v := range values {
		if v != "All" {
			return false
		}
	}
	return len(values) > 0
}
