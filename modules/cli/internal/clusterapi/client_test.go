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

package clusterapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	gwebsocket "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/modules/shared/logs"
)

func TestClient_LogRecordsFetch_SendsCorrectRequest(t *testing.T) {
	var (
		gotMethod   string
		gotPath     string
		gotAuth     string
		gotContent  string
		gotBodyJSON map[string]any
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContent = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBodyJSON)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"logRecordsFetch": {
					"records": [
						{
							"timestamp": "2024-01-02T15:04:05.123456789Z",
							"message": "hello world",
							"source": {
								"metadata": {"region":"r1","zone":"z1","os":"linux","arch":"arm64","node":"n1"},
								"namespace": "ns1",
								"podName": "p1",
								"containerName": "c1",
								"containerID": "cid1"
							}
						}
					],
					"nextCursor": null
				}
			}
		}`))
	}))
	defer srv.Close()

	httpClient := &http.Client{Transport: bearerInjector{token: "tkn-123", base: http.DefaultTransport}}
	c := newClientForTest(httpClient, srv.URL+APIServicePath+"/graphql")

	resp, err := c.LogRecordsFetch(context.Background(), LogRecordsFetchVars{
		Sources: []string{"deployments/web"},
		Mode:    "TAIL",
		Limit:   100,
	})
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, APIServicePath+"/graphql", gotPath)
	assert.Equal(t, "Bearer tkn-123", gotAuth)
	assert.Equal(t, "application/json", gotContent)

	require.NotNil(t, gotBodyJSON)
	assert.Contains(t, gotBodyJSON, "query")
	require.Contains(t, gotBodyJSON, "variables")
	vars := gotBodyJSON["variables"].(map[string]any)
	assert.Equal(t, []any{"deployments/web"}, vars["sources"])
	assert.Equal(t, "TAIL", vars["mode"])
	assert.EqualValues(t, 100, vars["limit"])

	require.Len(t, resp.Records, 1)
	rec := resp.Records[0]
	assert.Equal(t, "hello world", rec.Message)
	assert.Equal(t, "p1", rec.Source.PodName)
	assert.Equal(t, "c1", rec.Source.ContainerName)
	assert.Equal(t, "n1", rec.Source.Metadata.Node)
	assert.Equal(t, 2024, rec.Timestamp.Year())
	assert.Nil(t, resp.NextCursor)
}

func TestClient_LogRecordsFetch_PropagatesGraphQLErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":[{"message":"boom"}]}`))
	}))
	defer srv.Close()

	c := newClientForTest(http.DefaultClient, srv.URL+APIServicePath+"/graphql")
	_, err := c.LogRecordsFetch(context.Background(), LogRecordsFetchVars{Sources: []string{"x"}})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "boom"), "error should mention server message: %v", err)
}

func TestClient_LogRecordsFollow_StreamsWSFrames(t *testing.T) {
	srv := newWSTestServer(t, func(conn *gwebsocket.Conn) {
		expectInit(t, conn)
		writeAck(t, conn)
		id := expectSubscribe(t, conn)
		writeNext(t, conn, id, `{"data":{"logRecordsFollow":{"timestamp":"2024-01-02T15:04:05Z","message":"line A","source":{"metadata":{"region":"","zone":"","os":"","arch":"","node":"n1"},"namespace":"ns","podName":"p","containerName":"c","containerID":"cid"}}}}`)
		writeNext(t, conn, id, `{"data":{"logRecordsFollow":{"timestamp":"2024-01-02T15:04:06Z","message":"line B","source":{"metadata":{"region":"","zone":"","os":"","arch":"","node":"n1"},"namespace":"ns","podName":"p","containerName":"c","containerID":"cid"}}}}`)
		writeComplete(t, conn, id)
	})
	defer srv.Close()

	c := newClientForTest(http.DefaultClient, srv.URL+APIServicePath+"/graphql")
	records, errs := c.LogRecordsFollow(context.Background(), LogRecordsFollowVars{Sources: []string{"x"}})

	got := drainRecords(t, records, errs, 2, 2*time.Second)
	require.Len(t, got, 2)
	assert.Equal(t, "line A", got[0].Message)
	assert.Equal(t, "line B", got[1].Message)
}

func TestClient_LogRecordsFollow_NegotiatesGraphQLTransportWS(t *testing.T) {
	srv := newWSTestServer(t, func(conn *gwebsocket.Conn) {
		assert.Equal(t, "graphql-transport-ws", conn.Subprotocol())
		expectInit(t, conn)
		writeAck(t, conn)
		id := expectSubscribe(t, conn)
		writeComplete(t, conn, id)
	})
	defer srv.Close()

	c := newClientForTest(http.DefaultClient, srv.URL+APIServicePath+"/graphql")
	records, errs := c.LogRecordsFollow(context.Background(), LogRecordsFollowVars{Sources: []string{"x"}})
	for range records {
	}
	for e := range errs {
		require.NoError(t, e)
	}
}

func TestClient_LogRecordsFollow_PassesVariables(t *testing.T) {
	gotVars := make(chan map[string]any, 1)
	srv := newWSTestServer(t, func(conn *gwebsocket.Conn) {
		expectInit(t, conn)
		writeAck(t, conn)

		var sub gqlWSMessage
		require.NoError(t, conn.ReadJSON(&sub))
		require.Equal(t, "subscribe", sub.Type)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(sub.Payload, &payload))
		gotVars <- payload["variables"].(map[string]any)
		writeComplete(t, conn, sub.ID)
	})
	defer srv.Close()

	c := newClientForTest(http.DefaultClient, srv.URL+APIServicePath+"/graphql")
	records, errs := c.LogRecordsFollow(context.Background(), LogRecordsFollowVars{
		Sources: []string{"deployments/web"},
		Grep:    "GET /about",
		After:   "2024-01-02T15:04:05Z",
	})
	for range records {
	}
	for e := range errs {
		require.NoError(t, e)
	}

	vars := <-gotVars
	assert.Equal(t, []any{"deployments/web"}, vars["sources"])
	assert.Equal(t, "GET /about", vars["grep"])
	assert.Equal(t, "2024-01-02T15:04:05Z", vars["after"])
}

func TestClient_LogRecordsFollow_ContextCancellationStopsStream(t *testing.T) {
	srv := newWSTestServer(t, func(conn *gwebsocket.Conn) {
		expectInit(t, conn)
		writeAck(t, conn)
		id := expectSubscribe(t, conn)
		writeNext(t, conn, id, `{"data":{"logRecordsFollow":{"timestamp":"2024-01-02T15:04:05Z","message":"only","source":{"metadata":{"region":"","zone":"","os":"","arch":"","node":""},"namespace":"","podName":"","containerName":"","containerID":""}}}}`)
		// Hold the connection open until the client closes it.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := newClientForTest(http.DefaultClient, srv.URL+APIServicePath+"/graphql")
	records, errs := c.LogRecordsFollow(ctx, LogRecordsFollowVars{Sources: []string{"x"}})

	select {
	case rec, ok := <-records:
		require.True(t, ok)
		assert.Equal(t, "only", rec.Message)
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive first record")
	}
	cancel()

	waitClose := func(ch <-chan struct{}, name string) {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s did not close after cancel", name)
		}
	}
	rDone := make(chan struct{})
	go func() {
		for range records {
		}
		close(rDone)
	}()
	eDone := make(chan struct{})
	go func() {
		for range errs {
		}
		close(eDone)
	}()
	waitClose(rDone, "records")
	waitClose(eDone, "errs")
}

func TestClient_LogRecordsFollow_PropagatesGraphQLErrorFrame(t *testing.T) {
	srv := newWSTestServer(t, func(conn *gwebsocket.Conn) {
		expectInit(t, conn)
		writeAck(t, conn)
		id := expectSubscribe(t, conn)
		// graphql-transport-ws "error" carries the GraphQL errors array
		// directly as payload and terminates the subscription.
		require.NoError(t, conn.WriteJSON(map[string]any{
			"id":      id,
			"type":    "error",
			"payload": []map[string]any{{"message": "kapow"}},
		}))
	})
	defer srv.Close()

	c := newClientForTest(http.DefaultClient, srv.URL+APIServicePath+"/graphql")
	records, errs := c.LogRecordsFollow(context.Background(), LogRecordsFollowVars{Sources: []string{"x"}})

	var gotErr error
	var mu sync.Mutex
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for e := range errs {
			if e != nil {
				mu.Lock()
				gotErr = e
				mu.Unlock()
			}
		}
	}()
	for range records {
	}
	wg.Wait()
	require.Error(t, gotErr)
	assert.Contains(t, gotErr.Error(), "kapow")
}

// newWSTestServer wraps httptest with a gorilla upgrader that runs handle
// once per upgraded connection. The handler is responsible for closing the
// conn (gracefully or by returning).
func newWSTestServer(t *testing.T, handle func(*gwebsocket.Conn)) *httptest.Server {
	t.Helper()
	upgrader := gwebsocket.Upgrader{
		Subprotocols: []string{"graphql-transport-ws"},
		CheckOrigin:  func(*http.Request) bool { return true },
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade failed: %v", err)
			return
		}
		defer conn.Close()
		handle(conn)
	}))
}

func expectInit(t *testing.T, conn *gwebsocket.Conn) {
	t.Helper()
	var msg gqlWSMessage
	require.NoError(t, conn.ReadJSON(&msg))
	require.Equal(t, "connection_init", msg.Type)
}

func writeAck(t *testing.T, conn *gwebsocket.Conn) {
	t.Helper()
	require.NoError(t, conn.WriteJSON(map[string]any{"type": "connection_ack"}))
}

func expectSubscribe(t *testing.T, conn *gwebsocket.Conn) string {
	t.Helper()
	var msg gqlWSMessage
	require.NoError(t, conn.ReadJSON(&msg))
	require.Equal(t, "subscribe", msg.Type)
	require.NotEmpty(t, msg.ID)
	return msg.ID
}

func writeNext(t *testing.T, conn *gwebsocket.Conn, id, payload string) {
	t.Helper()
	require.NoError(t, conn.WriteJSON(map[string]any{
		"id":      id,
		"type":    "next",
		"payload": json.RawMessage(payload),
	}))
}

func writeComplete(t *testing.T, conn *gwebsocket.Conn, id string) {
	t.Helper()
	require.NoError(t, conn.WriteJSON(map[string]any{"id": id, "type": "complete"}))
}

func drainRecords(t *testing.T, records <-chan logs.LogRecord, errs <-chan error, want int, timeout time.Duration) []logs.LogRecord {
	t.Helper()
	got := make([]logs.LogRecord, 0, want)
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for len(got) < want {
		select {
		case rec, ok := <-records:
			if !ok {
				return got
			}
			got = append(got, rec)
		case e, ok := <-errs:
			if ok && e != nil {
				t.Fatalf("unexpected error from stream: %v", e)
			}
		case <-deadline.C:
			t.Fatalf("timed out waiting for %d records, got %d", want, len(got))
		}
	}
	return got
}

// bearerInjector is a tiny RoundTripper that sets a static bearer token so
// tests can verify auth-header propagation without needing a real
// rest.Config.
type bearerInjector struct {
	token string
	base  http.RoundTripper
}

func (b bearerInjector) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r)
}
