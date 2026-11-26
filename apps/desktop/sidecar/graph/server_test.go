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

package graph_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/graph"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/chat"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/logging"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oauth"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

// TestSandboxStateGet is the canary: a fresh server with no sandbox should answer
// `{ sandboxStateGet { available } }` with false. If this passes, the gqlgen wiring
// (schema -> resolver -> handler) is intact.
func TestSandboxStateGet(t *testing.T) {
	h := graph.NewServer(context.Background(), &graph.Resolver{})
	ts := httptest.NewServer(h)
	defer ts.Close()

	body := strings.NewReader(`{"query":"{ sandboxStateGet { available } }"}`)
	resp, err := http.Post(ts.URL+"/graphql", "application/json", body)
	if err != nil {
		t.Fatalf("POST /graphql: %v", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Data struct {
			Sandbox *struct {
				Available bool `json:"available"`
			} `json:"sandboxStateGet"`
		} `json:"data"`
	}
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	if out.Data.Sandbox == nil || out.Data.Sandbox.Available {
		t.Fatalf("want available=false (raw=%s)", raw)
	}
}

// TestResolverErrorIsLogged exercises the error presenter installed in
// NewServer: a query against an unknown field should produce one log line
// at ERROR level via the configured slog default.
func TestResolverErrorIsLogged(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	var buf bytes.Buffer
	slog.SetDefault(logging.Init(&buf, slog.LevelInfo))

	h := graph.NewServer(context.Background(), &graph.Resolver{})
	ts := httptest.NewServer(h)
	defer ts.Close()

	body := strings.NewReader(`{"query":"{ noSuchField }"}`)
	resp, err := http.Post(ts.URL+"/graphql", "application/json", body)
	if err != nil {
		t.Fatalf("POST /graphql: %v", err)
	}
	resp.Body.Close()

	var found bool
	for line := range strings.Lines(strings.TrimSpace(buf.String())) {
		if !strings.Contains(line, `"level":"ERROR"`) || !strings.Contains(line, "graphql") {
			continue
		}
		if !strings.Contains(line, `"error":`) {
			t.Errorf("missing error key: %s", line)
		}
		found = true
	}
	if !found {
		t.Errorf("expected an ERROR graphql log line, got: %s", buf.String())
	}
}

// TestErrorLogOmitsRequestData exercises resolver and validation errors. Values may
// arrive as variables, literals, names or aliases, and parsers may echo them in errors.
func TestErrorLogOmitsRequestData(t *testing.T) {
	const sentinel = "sensitive_8675309"
	requests := []string{
		`{"query":"mutation($id: ObjectID!) { clusterEnabledSet(id: $id, enabled: true) { id } }","variables":{"id":"8675309000000001"}}`,
		`{"query":"mutation { clusterEnabledSet(id: \"8675309000000001\", enabled: true) { id } }"}`,
		`{"query":"mutation($id: ObjectID!) { clusterEnabledSet(id: $id, enabled: true) { id } }","variables":{"id":"sensitive_8675309"}}`,
		`{"query":"mutation { clusterEnabledSet(id: \"sensitive_8675309\", enabled: true) { id } }"}`,
		`{"query":"mutation sensitive_8675309 { sensitive_8675309: clusterEnabledSet(id: \"8675309000000001\", enabled: true) { id } }"}`,
		`{"query":"{ sensitive_8675309 }"}`,
	}
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	for _, body := range requests {
		t.Run(body, func(t *testing.T) {
			var buf bytes.Buffer
			slog.SetDefault(logging.Init(&buf, slog.LevelInfo))
			h := graph.NewServer(context.Background(), &graph.Resolver{ClusterSvc: newFakeClusterService(nil)})
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			h.ServeHTTP(rr, req)
			if !bytes.Contains(rr.Body.Bytes(), []byte(`"errors"`)) {
				t.Fatalf("expected an error response: %s", rr.Body.String())
			}
			if !bytes.Contains(buf.Bytes(), []byte(`"level":"ERROR"`)) {
				t.Fatalf("expected an error log: %s", buf.String())
			}
			for _, secret := range []string{sentinel, "8675309000000001"} {
				if strings.Contains(buf.String(), secret) {
					t.Errorf("request data reached log: %s", buf.String())
				}
			}
		})
	}
}

// A coded refusal logs its code. Every mapped error is the same Go type, so without
// the code the log line would not say which refusal it was.
func TestErrorLogNamesTheCode(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	var buf bytes.Buffer
	slog.SetDefault(logging.Init(&buf, slog.LevelInfo))

	srv := newChatServer(t)
	id := appdb.NewID()
	postGQL(t, srv.URL, `{"query":"mutation { chatRename(id: \"`+id+`\", title: \"t\") { id } }"}`)

	if !strings.Contains(buf.String(), `"code":"KUBETAIL_RECORD_NOT_FOUND"`) {
		t.Errorf("the log names no code: %s", buf.String())
	}
	if strings.Contains(buf.String(), id) {
		t.Errorf("request data reached the log: %s", buf.String())
	}
}

// A plain resolver error logs its own type, not just gqlgen's wrapper, and a cancel
// is named apart from an errors.New of the same type.
func TestErrorLogNamesTheCause(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	cases := []struct {
		err      error
		canceled string
	}{
		{errors.New("loopback bind failed"), `"canceled":false`},
		{context.Canceled, `"canceled":true`},
	}
	for _, c := range cases {
		t.Run(c.err.Error(), func(t *testing.T) {
			var buf bytes.Buffer
			slog.SetDefault(logging.Init(&buf, slog.LevelInfo))

			svc := newFakeAuth(oauth.Identity{})
			svc.connectErr = c.err
			ts := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{Auth: svc}))
			defer ts.Close()
			postGQL(t, ts.URL, `{"query":"mutation { authLoginStart }"}`)

			for _, want := range []string{`"error":"*gqlerror.Error"`, `"cause":"*errors.errorString"`, c.canceled, `"ctx_err":false`} {
				if !strings.Contains(buf.String(), want) {
					t.Errorf("the log lacks %s: %s", want, buf.String())
				}
			}
		})
	}
}

// TestGracefulShutdownEndsSSEStream verifies that cancelling the streams context
// tells every active SSE subscription to flush its terminal `event: complete` and
// return, instead of the stream being cut mid-frame when the process exits.
func TestGracefulShutdownEndsSSEStream(t *testing.T) {
	// authStateWatch is the streaming canary: a fake auth emits the current
	// snapshot on subscribe, then keeps the stream open until shutdown.
	streams, endStreams := context.WithCancel(context.Background())
	defer endStreams()
	srv := graph.NewServer(streams, &graph.Resolver{Auth: newFakeAuth(oauth.Identity{})})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp := openSSESubscription(t, ts.URL, "", "subscription { authStateWatch { authenticated } }")
	defer resp.Body.Close() // belt-and-suspenders; shutdown ends the stream
	events := sseEvents(t, resp)

	// Wait for the first frame so we know the subscription is established.
	if ev := nextSSE(t, events); ev.event != "next" {
		t.Fatalf("want first event=next, got %q", ev.event)
	}

	endStreams()

	// The stream must terminate with a `complete` event, proving the handler
	// flushed its terminal frame on the shutdown-cancelled context rather than
	// being cut mid-stream.
	for {
		ev := nextSSE(t, events)
		if ev.event == "complete" {
			break
		}
		if ev.event != "next" {
			t.Fatalf("unexpected event before complete: %q", ev.event)
		}
	}
}

// ctxChat reads no rule, and fails once its context has ended, so a query
// shows whether it ran under the streams context.
type ctxChat struct{ chat.Service }

func (ctxChat) ChatRules(ctx context.Context, _ chat.ChatID) ([]permissions.Rule, error) {
	return nil, ctx.Err()
}

// Ending the streams leaves a query alone: only an SSE request runs under that
// context, and a query in flight at shutdown gets the request grace.
func TestEndingTheStreamsLeavesAQuery(t *testing.T) {
	streams, endStreams := context.WithCancel(context.Background())
	endStreams()
	ts := httptest.NewServer(graph.NewServer(streams, &graph.Resolver{ChatSvc: ctxChat{}}))
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/graphql", "application/json",
		strings.NewReader(`{"query":"{ chatPermissionRulesGet(chatID: \"`+appdb.NewID()+`\") { id } }"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(raw), `"chatPermissionRulesGet":[]`) || strings.Contains(string(raw), `"errors"`) {
		t.Fatalf("query after the streams ended = %s, want no rules", raw)
	}
}

// sseEvent is one parsed Server-Sent Event: gqlgen emits `event: next` with a
// JSON `data:` line per subscription value, and a final `event: complete`.
type sseEvent struct {
	event string
	data  string
}

// openSSESubscription POSTs a subscription to srvURL's /graphql endpoint with
// `Accept: text/event-stream` and returns the live streaming response. The
// caller must `defer resp.Body.Close()` — that cancels the subscription, and
// it must run before the test server's Close() (which blocks on outstanding
// requests), so close the body via defer rather than t.Cleanup. A non-empty
// token is sent as the `Authorization` bearer header — the single auth path
// shared with queries and mutations.
func openSSESubscription(t *testing.T, srvURL, token, query string) *http.Response {
	t.Helper()
	body := fmt.Sprintf(`{"query":%q}`, query)
	req, err := http.NewRequest(http.MethodPost, srvURL+"/graphql", strings.NewReader(body))
	if err != nil {
		t.Fatalf("new SSE request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /graphql (SSE): %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE status = %d, want 200", resp.StatusCode)
	}
	return resp
}

// sseEvents parses resp.Body into a stream of events, skipping comment-only
// frames (gqlgen's leading `:` and its keep-alive `: ping` lines). The channel
// closes when the body ends. A scan error (as opposed to a clean EOF) is
// reported via t.Errorf — goroutine-safe, unlike t.Fatal — so a truncated
// stream surfaces as a real failure rather than a mysterious early close.
func sseEvents(t *testing.T, resp *http.Response) <-chan sseEvent {
	t.Helper()
	ch := make(chan sseEvent)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(resp.Body)
		var ev sseEvent
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if ev.event != "" || ev.data != "" {
					ch <- ev
					ev = sseEvent{}
				}
			case strings.HasPrefix(line, "event:"):
				ev.event = strings.TrimSpace(line[len("event:"):])
			case strings.HasPrefix(line, "data:"):
				ev.data = strings.TrimSpace(line[len("data:"):])
				// `:` comment / keep-alive lines fall through and are ignored.
			}
		}
		// The expected teardown is a caller's deferred resp.Body.Close()
		// (cancelling the subscription) racing this mid-read. That surfaces
		// two ways depending on which side wins the race: net.ErrClosed when
		// the underlying conn is torn down, or net/http's unexported
		// errReadOnClosedResBody ("http: read on closed response body") when
		// the client closes the body first. Neither is a real scan failure —
		// only something else is worth a test error.
		if err := sc.Err(); err != nil && !isExpectedStreamClose(err) {
			t.Errorf("SSE scan: %v", err)
		}
	}()
	return ch
}

// isExpectedStreamClose reports whether err is the benign result of the
// subscription being torn down mid-read (deferred resp.Body.Close). It covers
// net.ErrClosed (underlying conn gone) and net/http's unexported
// errReadOnClosedResBody, which has no sentinel to match with errors.Is, so it
// is matched by its stable message.
func isExpectedStreamClose(err error) bool {
	return errors.Is(err, net.ErrClosed) ||
		strings.Contains(err.Error(), "read on closed response body")
}

// nextSSE pulls the next event with a deadline so a stalled stream fails the
// test instead of hanging.
func nextSSE(t *testing.T, ch <-chan sseEvent) sseEvent {
	t.Helper()
	return testutil.Recv(t, ch, "an SSE event")
}
