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
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/graph"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oauth"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

const clustersWatchQuery = `subscription { clusterCollectionWatch { type cluster { id } } }`

// newFailingWatchServer wires a fixture service whose every watch ends with err once
// its snapshot has been replayed — a source dying mid-stream.
func newFailingWatchServer(t *testing.T, err error) *httptest.Server {
	t.Helper()
	svc := newFakeClusterService(clusterFixtures())
	svc.watchFail = err
	srv := httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{
		ClusterSvc: svc,
		Auth:       newFakeAuth(oauth.Identity{}),
	}))
	t.Cleanup(srv.Close)
	return srv
}

// collectSSE drains a subscription to the end of its body, returning every event. Safe
// to run unbounded only because a failing watch ends its own stream; the failsafe is
// there so a regression that leaves it open fails instead of hanging.
func collectSSE(t *testing.T, events <-chan sseEvent) []sseEvent {
	t.Helper()
	var got []sseEvent
	deadline := time.After(testutil.Timeout)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return got
			}
			got = append(got, ev)
		case <-deadline:
			t.Fatal("the subscription never ended")
			return nil
		}
	}
}

// A watch whose source dies must reach the client as a GraphQL error. gqlgen ends a
// subscription the moment its resolver channel closes and cannot emit an error from
// there, so without the extension a broken watch is byte-identical to a graceful end:
// the webview reconnects silently and a permanently broken watch loops forever with
// nothing shown to the user.
func TestFailedSubscriptionEndsTheSubscriptionWithAnError(t *testing.T) {
	srv := newFailingWatchServer(t, errors.New("Cluster watch ended: watch too old"))

	resp := openSSESubscription(t, srv.URL, "", clustersWatchQuery)
	defer resp.Body.Close()

	got := collectSSE(t, sseEvents(t, resp))
	require.NotEmpty(t, got)
	assert.Equal(t, "complete", got[len(got)-1].event, "the transport still closes the stream")

	// The reason rides the LAST data frame: everything before it is real state the
	// client must still fold in, so an error that pre-empted the snapshot would lose it.
	data := got[:len(got)-1]
	require.NotEmpty(t, data)
	assert.Contains(t, data[len(data)-1].data, "watch too old")

	for _, ev := range data[:len(data)-1] {
		assert.NotContains(t, ev.data, "errors", "only the terminal frame carries the reason")
	}
}

// A chat watch that dies after its snapshot says so once, in a frame that carries no
// data: the client routes on the marker, and a failure frame that held data would
// fold the last frame's data twice.
func TestChatCollectionWatchReportsItsFailure(t *testing.T) {
	srv, db, _ := newChatServerOver(t)
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, sandboxDisabledThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)
	chatID := sent["chatSend"].(map[string]any)["chatID"].(string)

	// The answer settles first, so no turn is left to read the closed reader.
	transcript, messages := chatFrames(t, srv, `subscription { chatMessageCollectionWatch(chatID: "`+chatID+`") { message { seq status } } }`)
	defer transcript.Body.Close()
	awaitChatFrame(t, messages, func(f map[string]any) bool {
		msg, ok := f["message"].(map[string]any)
		return ok && msg["seq"] == float64(1) && msg["status"] == "Complete"
	})
	transcript.Body.Close()

	resp, events := chatFrames(t, srv, `subscription { chatCollectionWatch { type chat { id } } }`)
	defer resp.Body.Close()
	awaitChatFrame(t, events, func(f map[string]any) bool { return f["type"] == "Bookmark" })

	// The reader alone: closing the database closes the change hub too, which ends
	// the watch cleanly.
	require.NoError(t, db.Read.Close())
	db.Notify(appdb.KeyChats)

	var failed []sseEvent
	for _, ev := range collectSSE(t, events) {
		if strings.Contains(ev.data, `"subscriptionFailed":true`) {
			failed = append(failed, ev)
		}
	}
	require.Len(t, failed, 1)
	assert.Equal(t, "next", failed[0].event)
	assert.Contains(t, failed[0].data, `"data":null`)
}

// The complement: a healthy watch's frames stay clean, so a consumer treating any
// `errors` key as a dead watch can't be tripped by ordinary traffic.
func TestHealthySubscriptionFramesCarryNoError(t *testing.T) {
	srv := newTestServer(t, clusterFixtures())

	resp := openSSESubscription(t, srv.URL, "", clustersWatchQuery)
	defer resp.Body.Close()

	events := sseEvents(t, resp)
	// One frame per fixture cluster; a healthy watch then holds the stream open, so
	// read exactly that many rather than draining to a close that never comes.
	for range clusterFixtures() {
		assert.NotContains(t, nextSSE(t, events).data, "errors")
	}
}

// The extension names itself for gqlgen's registry; the name is part of the
// server wiring, not decoration.
func TestSubscriptionFailureExtensionName(t *testing.T) {
	if got := (graph.SubscriptionFailureExtension{}).ExtensionName(); got != "SubscriptionFailure" {
		t.Errorf("ExtensionName() = %q, want %q", got, "SubscriptionFailure")
	}
}

// A response reaching the interceptor without the operation context it installs
// falls straight through. Nothing in production skips InterceptOperation, so
// this is the arm that keeps a misconfigured server serving rather than panicking.
func TestSubscriptionFailureInterceptResponseIgnoresAnUninstrumentedContext(t *testing.T) {
	got := (graph.SubscriptionFailureExtension{}).InterceptResponse(
		context.Background(),
		func(context.Context) *graphql.Response { return nil },
	)
	if got != nil {
		t.Errorf("InterceptResponse = %+v, want nil", got)
	}
}
