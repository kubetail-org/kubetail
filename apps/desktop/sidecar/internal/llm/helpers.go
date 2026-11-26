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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
)

// What the wires share: the idle guard every stream runs under, the ids a
// replayed call goes under, and the client and error mapping the two wires over
// the OpenAI SDK build on.

// sectionJoiner puts the separator ahead of a section's first text when it is
// not the first section, so the streamed summary reads as the stored one will.
// K is whatever the wire calls a section: a block index, an (item, part) pair.
type sectionJoiner[K comparable] struct {
	last    K
	started bool
}

// join is what to emit for a section's delta. An empty delta opens no section:
// a section that says nothing leaves no separator behind it.
func (j *sectionJoiner[K]) join(k K, delta string) string {
	if delta == "" {
		return ""
	}
	if !j.started {
		j.last, j.started = k, true
		return delta
	}
	if k != j.last {
		j.last = k
		return ThinkingSeparator + delta
	}
	return delta
}

// idleGuard ends a call whose stream has gone quiet. It measures the wire, not the
// text: a turn can think for minutes and write nothing, and only the stream can
// tell that from a dead socket, since only it sees the traffic between.
type idleGuard struct {
	bound  time.Duration
	timer  idleTimer
	cancel context.CancelCauseFunc
}

// idleTimer is what the guard needs of a *time.Timer.
type idleTimer interface {
	Reset(time.Duration) bool
	Stop() bool
}

// idleClock arms the guard's timer: wallClock in production, and a test's own
// so it can move the guard's clock by hand.
type idleClock func(d time.Duration, f func()) idleTimer

// wallClock is time.AfterFunc as the guard's clock.
func wallClock(d time.Duration, f func()) idleTimer { return time.AfterFunc(d, f) }

// idleGuardKey is the context key of a stream's guard, where authorize finds it.
type idleGuardKey struct{}

// newIdleGuard starts the clock over ctx. The context it returns ends with
// ErrStreamIdle as its cause once bound passes with no event, so a trip and a
// caller's cancel read apart after the stream has ended on it. It carries the
// guard, so authorize can stop the clock while the request is signed.
func newIdleGuard(ctx context.Context, bound time.Duration, clock idleClock) (*idleGuard, context.Context) {
	ctx, cancel := context.WithCancelCause(ctx)
	g := &idleGuard{bound: bound, cancel: cancel}
	g.timer = clock(bound, func() { cancel(ErrStreamIdle) })
	return g, context.WithValue(ctx, idleGuardKey{}, g)
}

// event restarts the clock. Called for every event the stream receives.
func (g *idleGuard) event() { g.timer.Reset(g.bound) }

// pause stops the clock until resume. It stops the timer, not the context, so
// a cancel still ends what runs meanwhile.
func (g *idleGuard) pause() { g.timer.Stop() }

// resume restarts the clock at the full bound.
func (g *idleGuard) resume() { g.timer.Reset(g.bound) }

// stop ends the clock and releases the context. The caller owes it once the
// stream is over.
func (g *idleGuard) stop() {
	g.timer.Stop()
	g.cancel(nil)
}

// watchBody is request middleware that counts every read off the wire as an
// event. The SDK drops a ping before the loop sees it, and a ping is the only
// traffic some minutes of a turn carry, so the events alone are not all the proof
// of life there is.
func (g *idleGuard) watchBody(resp *http.Response, err error) (*http.Response, error) {
	if resp == nil || resp.Body == nil {
		return resp, err
	}
	resp.Body = beatingBody{ReadCloser: resp.Body, guard: g}
	return resp, err
}

// beatingBody resets the guard on every read that carried bytes.
type beatingBody struct {
	io.ReadCloser
	guard *idleGuard
}

func (b beatingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.guard.event()
	}
	return n, err
}

// openAIOptions is a client over the OpenAI SDK built from the provider alone.
// NewClient is never called: it is the one caller of DefaultClientOptions, the
// SDK's one reader of the vendor's variables, so each wire builds its service from
// these options instead. Retries are off: a retry repeats a paid call, and after
// an ambiguous failure can run a turn twice. The SDK is given no key: send sets
// the credential. wire is the wire's own options, which go ahead of the
// provider's omits.
func openAIOptions(p Provider, wire ...option.RequestOption) []option.RequestOption {
	opts := []option.RequestOption{
		option.WithBaseURL(p.BaseURL),
		option.WithMaxRetries(0),
		option.WithHTTPClient(NoRedirectClient),
		option.WithMiddleware(func(r *http.Request, next option.MiddlewareNext) (*http.Response, error) {
			return send(p, r, next)
		}),
	}
	return append(opts, providerOptions(p, wire, option.WithQueryAdd, option.WithJSONSet, option.WithJSONDel)...)
}

// authorize sets r's credential: the signer's, or the key in the header the row
// names, else the dialect's own. A provider with neither sends none. The
// stream's idle clock stops meanwhile: a signer may wait for the keyring, which
// only the user bounds. A request with no guard, a catalog read, has no clock.
func authorize(p Provider, r *http.Request) error {
	if g, ok := r.Context().Value(idleGuardKey{}).(*idleGuard); ok {
		g.pause()
		defer g.resume()
	}
	switch {
	case p.Signer != nil:
		return p.Signer.Sign(r)
	case p.Key != "":
		name, prefix := p.AuthHeader()
		r.Header.Set(name, prefix+p.Key)
	}
	return nil
}

// send is every wire's middleware: name Kubetail, authorize, send, and tell a
// signer of a 401. Kubetail's User-Agent leads and the SDK's own follows: some
// vendors watch for anonymous clients, and read the SDK's for support. A
// redirect fails here, since NoRedirectClient follows none and the SDKs read a
// 3xx as an answer.
func send(p Provider, r *http.Request, next func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	r.Header.Set("User-Agent", version.Prefix(r.Header.Get("User-Agent")))
	if err := authorize(p, r); err != nil {
		// A cancel or an idle trip while the signer waited ends the request,
		// not the credential.
		if ctxErr := r.Context().Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, SignError(p.ID, err)
	}
	resp, err := next(r)
	if err != nil {
		return resp, err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		if p.Signer != nil {
			p.Signer.Refused(r)
		}
	case resp.StatusCode/100 == 3:
		resp.Body.Close()
		return nil, ResponseError(p, resp.StatusCode, "", "")
	}
	return resp, nil
}

// SignedDo sends req as every wire sends a request of p: authorized, following
// no redirect, a signer told of a 401. Its User-Agent leads with Kubetail's.
func SignedDo(p Provider, req *http.Request) (*http.Response, error) {
	return send(p, req, NoRedirectClient.Do)
}

// NoRedirectClient is every request's client. It puts Kubetail's User-Agent
// ahead of the request's own, an AWS SDK's included. It follows no redirect: Go
// resends Authorization to a target on the same domain or a subdomain, and any
// other header to any target, and a credential is for its endpoint alone.
var NoRedirectClient = &http.Client{
	Transport:     version.Transport(nil),
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// providerOptions is a provider's own lines as one SDK's request options: its
// query parameters, its extra body fields, the wire's own options, and last the
// body paths it omits, so an omit also deletes what the wire wrote. Query keys
// and Extra paths go in sorted order, so a request is the same on every run.
// Each constructor is the SDK's own, so this names no SDK.
func providerOptions[O any](p Provider, wire []O,
	query func(k, v string) O, set func(path string, v any) O, del func(path string) O,
) []O {
	var opts []O
	for _, k := range slices.Sorted(maps.Keys(p.Query)) {
		opts = append(opts, query(k, p.Query[k]))
	}
	for _, path := range slices.Sorted(maps.Keys(p.Extra)) {
		opts = append(opts, set(path, p.Extra[path]))
	}
	opts = append(opts, wire...)
	for _, path := range p.Omit {
		opts = append(opts, del(path))
	}
	return opts
}

// requestMark is the request-level cache_control, for an hour as the Messages
// wire's cacheHour: the endpoint places it after the last cacheable block and
// moves it forward each turn.
var requestMark = map[string]any{"type": "ephemeral", "ttl": "1h"}

// cacheOptions is a request's caching as one SDK's request options: the
// request-level mark for a model that caches only on marks, and the affinity
// key where the provider routes it, none for an empty key or no route.
func cacheOptions[O any](p Provider, m Model, key string, header func(k, v string) O, set func(path string, v any) O) []O {
	var opts []O
	if m.Cache == CacheMarks {
		opts = append(opts, set("cache_control", requestMark))
	}
	switch {
	case key == "":
	case p.AffinityRoute.Header != "":
		opts = append(opts, header(p.AffinityRoute.Header, key))
	case p.AffinityRoute.Field != "":
		opts = append(opts, set(p.AffinityRoute.Field, key))
	}
	return opts
}

// openAIFailure is how a stream that did not finish is recorded. A stream that
// ended on its context reports the context's cause: the guard's trip or the
// caller's cancel. An error the API described keeps its status, its type when it
// is one the API defines, and never its message. A failure with no response at
// all names a host and no body, so it may be logged. Everything else is the
// reply's own bytes, which can echo the conversation, and is kept nowhere.
func openAIFailure(ctx context.Context, p Provider, err error) error {
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	// The middleware's own: nothing was sent, or a redirect was refused.
	if own, ok := errors.AsType[*Error](err); ok {
		return own
	}
	if api, ok := errors.AsType[*openai.Error](err); ok {
		return ResponseError(p, api.StatusCode, openAIErrorType(api.Type), openAICode(p, api.Code))
	}
	if noResponse(err) {
		return TransportError(p.ID, err)
	}
	return ReadError(p.ID)
}

// openAIErrorTypes is the types we name. OpenAI's own vocabulary is not
// documented whole and has not been read off a live 401 and 429 here; a
// spelling it sends that is not listed renders as "", and the status says what
// happened. Any other spelling is a vendor's own text and is not kept.
var openAIErrorTypes = map[string]bool{
	"invalid_request_error": true,
	"authentication_error":  true,
	"permission_error":      true,
	"not_found_error":       true,
	"rate_limit_error":      true,
	"server_error":          true,
	"insufficient_quota":    true,
	"api_error":             true,
}

func openAIErrorType(t string) string {
	if openAIErrorTypes[t] {
		return t
	}
	return ""
}

// openAICode is the code an OpenAI wire keeps: one p's row defines, or the
// request the model cannot read, which OpenAI and the compatible vendors that
// classify at all spell the same way. Every other code is the body's text and
// is not kept.
func openAICode(p Provider, code string) string {
	if _, defined := p.Codes[code]; defined || code == CodeContextLengthExceeded {
		return code
	}
	return ""
}

// replayIDs is what each call of a request replays under: the id, by the
// record's own, every id handed out, and how many calls there have been. A
// foreign round goes under ids of the wire's own — Mistral takes a tool_call_id
// of nine alphanumerics, and the Messages API only [a-zA-Z0-9_-] — and the
// counter runs over the conversation, so the same history always mints the same
// ids and a turn's rendering stays the leading slice of the next's.
type replayIDs struct {
	byRecord map[string]string
	out      map[string]bool
	calls    int
}

// mint is the id the call under recordID replays under, remembered for the result
// that answers it. **No id goes out twice**: an own id already handed out is
// minted, since the Chat Completions reader numbers call-<n> per reply and a
// chat can hold two calls under one id, and a minted spelling already out takes
// the next number. A foreign row is minted whatever its ids.
func (ids *replayIDs) mint(recordID string, foreign bool) string {
	if ids.byRecord == nil {
		ids.byRecord, ids.out = map[string]string{}, map[string]bool{}
	}
	id := recordID
	if foreign || ids.out[id] {
		n := ids.calls
		id = fmt.Sprintf("call%05d", n)
		for ids.out[id] {
			n++
			id = fmt.Sprintf("call%05d", n)
		}
	}
	ids.calls++
	ids.byRecord[recordID] = id
	ids.out[id] = true
	return id
}

// of is the id a result answers: the one its call was last minted under, or its
// own when no call of the conversation carries it.
func (ids *replayIDs) of(recordID string) string {
	if id, ok := ids.byRecord[recordID]; ok {
		return id
	}
	return recordID
}

// withReplayID is payload with field set to id: the wire's own entry for a call
// whose id was minted, so the entry and its result name the same call. A payload
// is an object by the time a wire sees it (disownUnreplayable), so the decode
// holds; were it not, the payload goes as it is.
func withReplayID(payload json.RawMessage, field, id string) json.RawMessage {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return payload
	}
	// A string: the marshals cannot fail.
	fields[field], _ = json.Marshal(id)
	out, _ := json.Marshal(fields)
	return out
}

// replay is blocks with each call under the id it replays under and each result
// under its call's, for a wire whose calls carry no payload naming an id. A copy:
// the record keeps its own ids.
func (ids *replayIDs) replay(blocks []Block, foreign bool) []Block {
	out := slices.Clone(blocks)
	for i, b := range out {
		switch b.Type {
		case BlockToolUse:
			out[i].ID = ids.mint(b.ID, foreign)
		case BlockToolResult:
			out[i].ID = ids.of(b.ID)
		}
	}
	return out
}
