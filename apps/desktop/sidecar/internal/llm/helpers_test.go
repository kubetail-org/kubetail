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
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The guard ends its context with ErrStreamIdle when its timer fires; an event
// resets the timer; stopped, it ends the context with no cause.
func TestIdleGuardTripsOnlyWhenNothingArrives(t *testing.T) {
	const bound = time.Minute

	timer := newFakeTimer()
	_, ctx := newIdleGuard(t.Context(), bound, timer.clock)
	timer.trip()
	testutil.Wait(t, ctx.Done(), "the guard's trip")
	assert.ErrorIs(t, context.Cause(ctx), ErrStreamIdle)

	timer = newFakeTimer()
	g, quiet := newIdleGuard(t.Context(), bound, timer.clock)
	g.event()
	timer.awaitReset(t)
	g.stop()
	testutil.Wait(t, quiet.Done(), "the stopped guard's release")
	assert.NotErrorIs(t, context.Cause(quiet), ErrStreamIdle)
}

// Every read off the wire that carried bytes is an event, so a heartbeat the SDK
// swallows still counts; a response with no body is passed through untouched.
func TestIdleGuardCountsBytesOffTheWire(t *testing.T) {
	g, _ := newIdleGuard(t.Context(), testutil.Timeout, wallClock)
	t.Cleanup(g.stop)

	resp, err := g.watchBody(&http.Response{Body: io.NopCloser(strings.NewReader("ping"))}, nil)
	require.NoError(t, err)
	body, ok := resp.Body.(beatingBody)
	require.True(t, ok, "the body is wrapped")
	assert.Same(t, g, body.guard)
	read, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "ping", string(read))

	none := &http.Response{}
	got, err := g.watchBody(none, errors.New("dial"))
	assert.Same(t, none, got)
	assert.EqualError(t, err, "dial")
	assert.Nil(t, got.Body)
}

// A failure keeps the API's status, its type when the API defines that word, and
// the one code we name; the context's cause outranks all of it, a failure with no
// response is the transport's, and anything else is unreadable.
func TestOpenAIFailureKeepsTheStatusTypeAndCode(t *testing.T) {
	full := &openai.Error{StatusCode: 400, Type: "invalid_request_error", Code: "context_length_exceeded", Message: "MARKER"}
	assert.Equal(t, ResponseError(Provider{ID: "openai"}, 400, "invalid_request_error", CodeContextLengthExceeded), openAIFailure(t.Context(), Provider{ID: "openai"}, full))

	vendors := &openai.Error{StatusCode: 503, Type: "model_not_ready", Code: "MARKER", Message: "MARKER"}
	assert.Equal(t, ResponseError(Provider{ID: "openai"}, 503, "", ""), openAIFailure(t.Context(), Provider{ID: "openai"}, vendors))

	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(ErrStreamIdle)
	assert.Equal(t, ErrStreamIdle, openAIFailure(ctx, Provider{ID: "openai"}, full))

	dial := &url.Error{Op: "Post", URL: "http://127.0.0.1:1/v1", Err: &net.OpError{Op: "dial", Err: errors.New("refused")}}
	var llmErr *Error
	require.ErrorAs(t, openAIFailure(t.Context(), Provider{ID: "openai"}, dial), &llmErr)
	assert.Equal(t, "connection failed", llmErr.Kind)

	assert.Equal(t, ReadError("openai"), openAIFailure(t.Context(), Provider{ID: "openai"}, errors.New("MARKER")))
}

// A type the API defines is kept; a compatible vendor's own spelling is not.
func TestOpenAIErrorTypeKeepsTheAPIsListAlone(t *testing.T) {
	assert.Equal(t, "rate_limit_error", openAIErrorType("rate_limit_error"))
	assert.Equal(t, "insufficient_quota", openAIErrorType("insufficient_quota"))
	assert.Empty(t, openAIErrorType("model_not_ready"))
	assert.Empty(t, openAIErrorType(""))
}

// One code is ours to name, and a row names its own; every other is the
// body's text.
func TestOpenAICodeNamesTheCodeWeName(t *testing.T) {
	plan := Provider{ID: "plan", Codes: limitCodes}
	assert.Equal(t, CodeContextLengthExceeded, openAICode(Provider{}, "context_length_exceeded"))
	assert.Equal(t, "subscription_sharing_usage_limit_exceeded", openAICode(plan, "subscription_sharing_usage_limit_exceeded"))
	assert.Empty(t, openAICode(Provider{}, "subscription_sharing_usage_limit_exceeded"))
	assert.Empty(t, openAICode(plan, "invalid_api_key"))
	assert.Empty(t, openAICode(plan, ""))
}

// Two providers on one dialect are two clients, each built from its own entry: a
// turn reaches its own endpoint with its own key, and a failure names the
// provider it came from.
func TestTwoProvidersOnOneDialect(t *testing.T) {
	first, firstRec := chatReplay(t, chatStream)
	second, secondRec := chatReplay(t, chatStream)
	second.ID, second.Key = "deepseek", "sk-deepseek"

	for _, p := range []Provider{first, second} {
		_, _, err := askChat(t, chatRequest(p))
		require.NoError(t, err, p.ID)
	}

	assert.Equal(t, int64(1), firstRec.calls.Load())
	assert.Equal(t, int64(1), secondRec.calls.Load())
	assert.Equal(t, "Bearer gsk-test", firstRec.header.Get("Authorization"))
	assert.Equal(t, "Bearer sk-deepseek", secondRec.header.Get("Authorization"))

	t.Run("a failure names the provider it came from", func(t *testing.T) {
		refused := groqAt(newServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(429)
			fmt.Fprint(w, `{"error":{"type":"rate_limit_error","message":"slow down"}}`)
		}))
		refused.ID = "deepseek"
		_, _, err := askChat(t, chatRequest(refused))
		assert.EqualError(t, err, "deepseek: 429 rate_limit_error")

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		addr := ln.Addr().String()
		require.NoError(t, ln.Close())
		gone := groqAt("http://" + addr)
		gone.ID = "mistral"
		_, _, err = askChat(t, chatRequest(gone))
		assert.EqualError(t, err, "mistral: connection failed")

		broken, _ := chatReplay(t, []event{chatStream[0], {"", `{"error":{"message":"x"}}`}})
		broken.ID = "together"
		_, _, err = askChat(t, chatRequest(broken))
		assert.EqualError(t, err, "together: unreadable reply")
	})
}

// A provider's lines come out in one order on every run: its query and its
// extras each sorted, and its omits last, so a path an extra set can still be
// deleted.
func TestProviderOptionsSortsWhatAMapHolds(t *testing.T) {
	p := Provider{
		Query: map[string]string{"b": "2", "a": "1"},
		Extra: map[string]any{"y": 1, "x": 2},
		Omit:  []string{"store", "x"},
	}
	assert.Equal(t, []string{
		"query a=1", "query b=2", "set x=2", "set y=1", "del store", "del x",
	}, stringOptions(p, nil))
}

// The wire's own options go after the provider's extras and before its omits, so
// a row can still delete a field the wire's cache options set.
func TestProviderOptionsPutsTheWiresOwnBeforeTheOmits(t *testing.T) {
	p := Provider{Extra: map[string]any{"x": 1}, Omit: []string{"cache_control"}}

	assert.Equal(t, []string{"set x=1", "wire cache_control", "del cache_control"},
		stringOptions(p, []string{"wire cache_control"}))
}

// stringOptions is providerOptions spelled as strings, one per option.
func stringOptions(p Provider, wire []string) []string {
	return providerOptions(p, wire,
		func(k, v string) string { return "query " + k + "=" + v },
		func(path string, v any) string { return fmt.Sprintf("set %s=%v", path, v) },
		func(path string) string { return "del " + path })
}

// The first section's deltas arrive bare and the next section's first delta is
// led by the separator, so a streamed summary reads as the stored one will. An
// empty delta is empty and opens no section, so a section that says nothing
// leaves no separator behind it.
func TestSectionJoinerSeparatesSectionsAfterTheFirst(t *testing.T) {
	var j sectionJoiner[int]

	assert.Empty(t, j.join(0, ""))
	assert.Equal(t, "I count ", j.join(0, "I count "))
	assert.Equal(t, "the pods.", j.join(0, "the pods."))
	assert.Empty(t, j.join(1, ""))
	assert.Equal(t, "\n\nThey are twelve.", j.join(2, "They are twelve."))
	assert.Equal(t, " Twelve exactly.", j.join(2, " Twelve exactly."))
}

// A foreign call is minted, an own one keeps its id, and an own id a call of the
// conversation already went under is minted on its second use. A result answers
// the id its call was last minted under.
func TestReplayIDsMintForeignAndSpentIDs(t *testing.T) {
	var ids replayIDs

	assert.Equal(t, "call00000", ids.mint("toolu_1", true))
	assert.Equal(t, "call-1", ids.mint("call-1", false))
	assert.Equal(t, "call00002", ids.mint("call-1", false))
	assert.Equal(t, "call00000", ids.of("toolu_1"))
	assert.Equal(t, "call00002", ids.of("call-1"))
	assert.Equal(t, "orphan", ids.of("orphan"))
}

// Every id handed out is distinct: a mint skips a spelling an own call already
// went under, and an own id equal to one already minted is minted too.
func TestReplayIDsNeverHandOutAnIDTwice(t *testing.T) {
	var ids replayIDs

	assert.Equal(t, "call00001", ids.mint("call00001", false))
	assert.Equal(t, "call00002", ids.mint("toolu_1", true))
	assert.Equal(t, "call00003", ids.mint("call00002", false))
	assert.Equal(t, "call00002", ids.of("toolu_1"))
	assert.Equal(t, "call00003", ids.of("call00002"))
}

// The named field takes the id and every other key keeps its value, byte for byte.
func TestWithReplayIDSetsOneField(t *testing.T) {
	payload := json.RawMessage(`{"id":"call-1","type":"function","extra_content":{"google":{"thought_signature":"c2ln"}},"n":1.50}`)

	got := withReplayID(payload, "id", "call00003")

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got, &fields))
	assert.JSONEq(t, `"call00003"`, string(fields["id"]))
	assert.Equal(t, `"function"`, string(fields["type"]))
	assert.Equal(t, `{"google":{"thought_signature":"c2ln"}}`, string(fields["extra_content"]))
	assert.Equal(t, `1.50`, string(fields["n"]))
	assert.Len(t, fields, 4)
}

// A payload that is not an object goes as it is.
func TestWithReplayIDLeavesANonObjectAlone(t *testing.T) {
	payload := json.RawMessage(`["call-1"]`)

	assert.Equal(t, payload, withReplayID(payload, "id", "call00003"))
}

// A Sign ended by its request's context is the request's end, not a credential
// that is unavailable.
func TestASignEndedByItsRequestIsNoCredentialError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1/", nil)
	require.NoError(t, err)

	_, err = SignedDo(Provider{ID: "p", Signer: newHeldSigner()}, req)

	assert.ErrorIs(t, err, context.Canceled)
	var e *Error
	assert.False(t, errors.As(err, &e), "%v", err)
}

func TestSignedDoNamesKubetail(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
	}))
	t.Cleanup(srv.Close)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	resp, err := SignedDo(Provider{ID: "p"}, req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, version.UserAgent(), got)
}

// The connectors send through NoRedirectClient without send, so the client
// itself names Kubetail.
func TestNoRedirectClientNamesKubetail(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
	}))
	t.Cleanup(srv.Close)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	resp, err := NoRedirectClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, version.UserAgent(), got)
}
