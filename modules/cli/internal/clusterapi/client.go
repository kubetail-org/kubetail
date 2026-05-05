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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	gwebsocket "github.com/gorilla/websocket"
	"k8s.io/client-go/rest"
	clientgows "k8s.io/client-go/transport/websocket"

	"github.com/kubetail-org/kubetail/modules/shared/logs"
)

// APIServiceName is the metadata.name of the Kubetail cluster-api APIService
// registered with the kube-apiserver aggregation layer.
const APIServiceName = "v1.api.kubetail.com"

// APIServicePath is the path under which the cluster-api is exposed by the
// kube-apiserver via the APIServiceName APIService.
const APIServicePath = "/apis/api.kubetail.com/v1"

// ErrAPINotInstalled is returned when a request to the cluster-api endpoint
// receives an HTTP 404 from the kube-apiserver, indicating the APIService for
// the Kubetail cluster-api is not registered on this cluster (i.e., the
// kubetail-api is not installed). Callers can use errors.Is to detect this
// and decide whether to fall back to a different backend.
var ErrAPINotInstalled = errors.New("kubetail cluster-api is not installed on this cluster")

// graphqlTransportWSSubprotocol is the WebSocket subprotocol the cluster-api
// (gqlgen Websocket transport) speaks for GraphQL subscriptions.
const graphqlTransportWSSubprotocol = "graphql-transport-ws"

// graphql-transport-ws message types.
const (
	msgConnectionInit  = "connection_init"
	msgConnectionAck   = "connection_ack"
	msgConnectionError = "connection_error"
	msgPing            = "ping"
	msgPong            = "pong"
	msgSubscribe       = "subscribe"
	msgNext            = "next"
	msgError           = "error"
	msgComplete        = "complete"
)

// wsDialFunc opens a WebSocket connection. Replaced in tests.
type wsDialFunc func(ctx context.Context, urlStr string, header http.Header, subprotocols []string) (*gwebsocket.Conn, error)

// Client is a thin GraphQL client for the Kubetail cluster-api, intended to
// be reached through the kube-apiserver aggregation layer (auth, TLS, and
// routing all handled by client-go's rest transport for queries, and
// client-go's websocket round-tripper for subscriptions).
type Client struct {
	httpClient *http.Client
	endpoint   string
	wsDial     wsDialFunc
}

// NewClient builds a Client whose HTTP transport carries the user's
// kubeconfig credentials, with requests directed at
//
//	<restConfig.Host>/apis/api.kubetail.com/v1/graphql
//
// which the kube-apiserver routes to cluster-api via the registered
// APIService. Subscriptions ride a WebSocket upgrade on the same path so
// that the kube-apiserver hijacks the connection (long-running) instead of
// applying its non-watch request deadline to a streamed POST.
func NewClient(restConfig *rest.Config) (*Client, error) {
	t, err := rest.TransportFor(restConfig)
	if err != nil {
		return nil, err
	}
	host := strings.TrimRight(restConfig.Host, "/")
	return &Client{
		httpClient: &http.Client{Transport: t},
		endpoint:   host + APIServicePath + "/graphql",
		wsDial:     newClientGoWSDialer(restConfig),
	}, nil
}

// newClientForTest constructs a Client with a caller-supplied http.Client
// and endpoint. Used by tests to point at an httptest server. The WebSocket
// dialer is a plain gorilla dialer that rewrites http(s) -> ws(s).
func newClientForTest(c *http.Client, endpoint string) *Client {
	return &Client{
		httpClient: c,
		endpoint:   endpoint,
		wsDial: func(ctx context.Context, urlStr string, header http.Header, subprotocols []string) (*gwebsocket.Conn, error) {
			wsURL, err := httpToWS(urlStr)
			if err != nil {
				return nil, err
			}
			d := &gwebsocket.Dialer{Subprotocols: subprotocols}
			conn, _, err := d.DialContext(ctx, wsURL, header)
			return conn, err
		},
	}
}

// newClientGoWSDialer returns a wsDialFunc that uses client-go's WebSocket
// round-tripper, so the upgrade carries the same auth/TLS as a normal
// kube-apiserver request (bearer token, exec plugins, OIDC, mTLS).
func newClientGoWSDialer(restConfig *rest.Config) wsDialFunc {
	return func(ctx context.Context, urlStr string, header http.Header, subprotocols []string) (*gwebsocket.Conn, error) {
		rt, holder, err := clientgows.RoundTripperFor(restConfig)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
		if err != nil {
			return nil, err
		}
		for k, vs := range header {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
		conn, err := clientgows.Negotiate(rt, holder, req, subprotocols...)
		if err != nil {
			return nil, err
		}
		if conn == nil {
			return nil, fmt.Errorf("kubetail-api: websocket negotiation returned nil connection")
		}
		return conn, nil
	}
}

// Ping issues a trivial GraphQL request whose only purpose is to confirm the
// cluster-api endpoint is reachable. It returns ErrAPINotInstalled when the
// kube-apiserver responds with 404 (APIService not registered). Used by
// callers that need to probe availability without fetching records — e.g.
// `kubetail logs -f --tail=0`, where no bootstrap fetch is otherwise issued
// and backend auto-selection would otherwise commit to Kubetail before any
// network round-trip.
func (c *Client) Ping(ctx context.Context) error {
	var out struct {
		Typename string `json:"__typename"`
	}
	return c.do(ctx, `query { __typename }`, map[string]any{}, &out)
}

// LogRecordsFetchVars carries the variables for a logRecordsFetch query.
// Empty/zero values are omitted from the request.
type LogRecordsFetchVars struct {
	KubeContext string
	Sources     []string
	Mode        string // "HEAD" or "TAIL"
	Since       string
	Until       string
	Grep        string
	Limit       int
	Cursor      string // forward-pagination cursor; sent as the `after` arg

	// Source filters mapped 1:1 to the cluster-api LogSourceFilter input.
	Regions []string
	Zones   []string
	OSes    []string
	Arches  []string
	Nodes   []string
}

// LogRecordsQueryResponse mirrors the GraphQL `LogRecordsQueryResponse`.
type LogRecordsQueryResponse struct {
	Records    []logs.LogRecord
	NextCursor *string
}

const logRecordsFetchQuery = `
query CLILogRecordsFetch(
	$kubeContext: String,
	$sources: [String!]!,
	$mode: LogRecordsQueryMode,
	$since: String,
	$until: String,
	$after: String,
	$grep: String,
	$limit: Int,
	$sourceFilter: LogSourceFilter
) {
	logRecordsFetch(
		kubeContext: $kubeContext,
		sources: $sources,
		mode: $mode,
		since: $since,
		until: $until,
		after: $after,
		grep: $grep,
		limit: $limit,
		sourceFilter: $sourceFilter
	) {
		records {
			timestamp
			message
			source {
				metadata { region zone os arch node }
				namespace
				podName
				containerName
				containerID
			}
		}
		nextCursor
	}
}
`

// LogRecordsFetch executes a logRecordsFetch query and returns its decoded
// response. GraphQL-level errors are surfaced as a Go error.
func (c *Client) LogRecordsFetch(ctx context.Context, v LogRecordsFetchVars) (*LogRecordsQueryResponse, error) {
	vars := buildFetchVariables(v)
	var out struct {
		LogRecordsFetch *gqlLogRecordsQueryResponse `json:"logRecordsFetch"`
	}
	if err := c.do(ctx, logRecordsFetchQuery, vars, &out); err != nil {
		return nil, err
	}
	if out.LogRecordsFetch == nil {
		return &LogRecordsQueryResponse{}, nil
	}
	resp := &LogRecordsQueryResponse{
		Records:    make([]logs.LogRecord, 0, len(out.LogRecordsFetch.Records)),
		NextCursor: out.LogRecordsFetch.NextCursor,
	}
	for _, r := range out.LogRecordsFetch.Records {
		resp.Records = append(resp.Records, r.toLogRecord())
	}
	return resp, nil
}

func buildFetchVariables(v LogRecordsFetchVars) map[string]any {
	m := map[string]any{"sources": v.Sources}
	if v.KubeContext != "" {
		m["kubeContext"] = v.KubeContext
	}
	if v.Mode != "" {
		m["mode"] = v.Mode
	}
	if v.Since != "" {
		m["since"] = v.Since
	}
	if v.Until != "" {
		m["until"] = v.Until
	}
	if v.Cursor != "" {
		m["after"] = v.Cursor
	}
	if v.Grep != "" {
		m["grep"] = v.Grep
	}
	if v.Limit > 0 {
		m["limit"] = v.Limit
	}
	if filter := buildSourceFilter(v.Regions, v.Zones, v.OSes, v.Arches, v.Nodes); filter != nil {
		m["sourceFilter"] = filter
	}
	return m
}

// buildSourceFilter assembles a LogSourceFilter input map from non-empty
// dimensions. Returns nil if every dimension is empty so the caller can omit
// the variable entirely.
func buildSourceFilter(regions, zones, oses, arches, nodes []string) map[string]any {
	filter := map[string]any{}
	if len(regions) > 0 {
		filter["region"] = regions
	}
	if len(zones) > 0 {
		filter["zone"] = zones
	}
	if len(oses) > 0 {
		filter["os"] = oses
	}
	if len(arches) > 0 {
		filter["arch"] = arches
	}
	if len(nodes) > 0 {
		filter["node"] = nodes
	}
	if len(filter) == 0 {
		return nil
	}
	return filter
}

// do performs a single GraphQL POST and decodes data into out. Any
// `errors` array in the response is concatenated and returned as an error.
func (c *Client) do(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: HTTP 404: %s", ErrAPINotInstalled, truncate(string(respBytes), 256))
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("kubetail-api: HTTP %d: %s", resp.StatusCode, truncate(string(respBytes), 256))
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []gqlError      `json:"errors"`
	}
	if err := json.Unmarshal(respBytes, &envelope); err != nil {
		return fmt.Errorf("kubetail-api: decode response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return errFromGraphQLErrors(envelope.Errors)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("kubetail-api: decode data: %w", err)
	}
	return nil
}

// gqlError is the standard GraphQL error shape (we only consume `message`).
type gqlError struct {
	Message string `json:"message"`
}

func errFromGraphQLErrors(errs []gqlError) error {
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		if e.Message != "" {
			parts = append(parts, e.Message)
		}
	}
	if len(parts) == 0 {
		return fmt.Errorf("kubetail-api: GraphQL request failed (no message)")
	}
	return fmt.Errorf("kubetail-api: %s", strings.Join(parts, "; "))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

type gqlLogRecord struct {
	Timestamp time.Time    `json:"timestamp"`
	Message   string       `json:"message"`
	Source    gqlLogSource `json:"source"`
}

type gqlLogSource struct {
	Metadata      gqlLogSourceMetadata `json:"metadata"`
	Namespace     string               `json:"namespace"`
	PodName       string               `json:"podName"`
	ContainerName string               `json:"containerName"`
	ContainerID   string               `json:"containerID"`
}

type gqlLogSourceMetadata struct {
	Region string `json:"region"`
	Zone   string `json:"zone"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Node   string `json:"node"`
}

type gqlLogRecordsQueryResponse struct {
	Records    []gqlLogRecord `json:"records"`
	NextCursor *string        `json:"nextCursor"`
}

// LogRecordsFollowVars carries the variables for a logRecordsFollow
// subscription. Empty/zero values are omitted from the request.
type LogRecordsFollowVars struct {
	KubeContext string
	Sources     []string
	Since       string
	After       string
	Grep        string

	// Source filters mapped 1:1 to the cluster-api LogSourceFilter input.
	Regions []string
	Zones   []string
	OSes    []string
	Arches  []string
	Nodes   []string
}

const logRecordsFollowQuery = `
subscription CLILogRecordsFollow(
	$kubeContext: String,
	$sources: [String!]!,
	$since: String,
	$after: String,
	$grep: String,
	$sourceFilter: LogSourceFilter
) {
	logRecordsFollow(
		kubeContext: $kubeContext,
		sources: $sources,
		since: $since,
		after: $after,
		grep: $grep,
		sourceFilter: $sourceFilter
	) {
		timestamp
		message
		source {
			metadata { region zone os arch node }
			namespace
			podName
			containerName
			containerID
		}
	}
}
`

// LogRecordsFollow opens a graphql-transport-ws subscription over a
// WebSocket against the cluster-api and returns a records channel and an
// error channel. Both channels are closed when the subscription ends
// (server `complete`, connection drop, or context cancellation). Per-frame
// GraphQL errors are forwarded on the error channel and do not terminate
// the stream.
//
// WebSocket is used (vs. SSE-over-POST) because the kube-apiserver hijacks
// upgrade requests at the aggregation layer; that bypasses the apiserver's
// non-watch request-deadline filter, which would otherwise tear down a
// long-lived streamed response after roughly a minute.
func (c *Client) LogRecordsFollow(ctx context.Context, v LogRecordsFollowVars) (<-chan logs.LogRecord, <-chan error) {
	records := make(chan logs.LogRecord, 16)
	errs := make(chan error, 4)

	failClosed := func(err error) (<-chan logs.LogRecord, <-chan error) {
		errs <- err
		close(records)
		close(errs)
		return records, errs
	}

	// Pass the http(s) endpoint as-is. The default (client-go) dialer
	// rewrites the scheme internally; the test dialer rewrites via httpToWS.
	conn, err := c.wsDial(ctx, c.endpoint, http.Header{}, []string{graphqlTransportWSSubprotocol})
	if err != nil {
		return failClosed(fmt.Errorf("kubetail-api: websocket dial: %w", err))
	}

	go runGraphQLTransportWS(ctx, conn, logRecordsFollowQuery, buildFollowVariables(v), records, errs)
	return records, errs
}

// httpToWS rewrites an http(s) endpoint into a ws(s) URL.
func httpToWS(endpoint string) (string, error) {
	switch {
	case strings.HasPrefix(endpoint, "https://"):
		return "wss://" + strings.TrimPrefix(endpoint, "https://"), nil
	case strings.HasPrefix(endpoint, "http://"):
		return "ws://" + strings.TrimPrefix(endpoint, "http://"), nil
	default:
		return "", fmt.Errorf("kubetail-api: unsupported endpoint scheme: %s", endpoint)
	}
}

func buildFollowVariables(v LogRecordsFollowVars) map[string]any {
	m := map[string]any{"sources": v.Sources}
	if v.KubeContext != "" {
		m["kubeContext"] = v.KubeContext
	}
	if v.Since != "" {
		m["since"] = v.Since
	}
	if v.After != "" {
		m["after"] = v.After
	}
	if v.Grep != "" {
		m["grep"] = v.Grep
	}
	if filter := buildSourceFilter(v.Regions, v.Zones, v.OSes, v.Arches, v.Nodes); filter != nil {
		m["sourceFilter"] = filter
	}
	return m
}

// runGraphQLTransportWS drives a graphql-transport-ws subscription on conn
// (https://github.com/enisdenjo/graphql-ws/blob/master/PROTOCOL.md). It
// completes the connection_init/connection_ack handshake, sends a single
// `subscribe`, and forwards `next`/`error` payloads onto records/errs until
// the server emits `complete`, the connection drops, or ctx is cancelled.
// The conn and both channels are owned by this goroutine; all are closed
// before it returns.
func runGraphQLTransportWS(
	ctx context.Context,
	conn *gwebsocket.Conn,
	query string,
	variables map[string]any,
	records chan<- logs.LogRecord,
	errs chan<- error,
) {
	const subscriptionID = "1"

	defer close(records)
	defer close(errs)
	defer conn.Close()

	// Unblock a blocked ReadMessage when ctx is cancelled.
	stopCloser := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCloser()

	pushErr := func(err error) {
		if err == nil {
			return
		}
		select {
		case errs <- err:
		case <-ctx.Done():
		}
	}

	if err := conn.WriteJSON(map[string]any{"type": msgConnectionInit, "payload": map[string]any{}}); err != nil {
		pushErr(fmt.Errorf("kubetail-api: connection_init: %w", err))
		return
	}

ackLoop:
	for {
		var msg gqlWSMessage
		if err := conn.ReadJSON(&msg); err != nil {
			if ctx.Err() == nil {
				pushErr(fmt.Errorf("kubetail-api: read connection_ack: %w", err))
			}
			return
		}
		switch msg.Type {
		case msgConnectionAck:
			break ackLoop
		case msgPing:
			_ = conn.WriteJSON(map[string]any{"type": msgPong})
		case msgConnectionError, msgError:
			pushErr(fmt.Errorf("kubetail-api: server rejected connection_init: %s", string(msg.Payload)))
			return
		}
	}

	subPayload := map[string]any{"query": query, "variables": variables}
	if err := conn.WriteJSON(map[string]any{"id": subscriptionID, "type": msgSubscribe, "payload": subPayload}); err != nil {
		pushErr(fmt.Errorf("kubetail-api: subscribe: %w", err))
		return
	}

	for {
		var msg gqlWSMessage
		if err := conn.ReadJSON(&msg); err != nil {
			if ctx.Err() == nil && !isExpectedCloseErr(err) {
				pushErr(fmt.Errorf("kubetail-api: read frame: %w", err))
			}
			return
		}
		switch msg.Type {
		case msgPing:
			_ = conn.WriteJSON(map[string]any{"type": msgPong})
		case msgPong:
		case msgNext:
			var payload struct {
				Data *struct {
					LogRecordsFollow *gqlLogRecord `json:"logRecordsFollow"`
				} `json:"data"`
				Errors []gqlError `json:"errors"`
			}
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				pushErr(fmt.Errorf("kubetail-api: decode next payload: %w", err))
				continue
			}
			if len(payload.Errors) > 0 {
				pushErr(errFromGraphQLErrors(payload.Errors))
				continue
			}
			if payload.Data != nil && payload.Data.LogRecordsFollow != nil {
				select {
				case records <- payload.Data.LogRecordsFollow.toLogRecord():
				case <-ctx.Done():
					return
				}
			}
		case msgError:
			var gqlErrs []gqlError
			if len(msg.Payload) > 0 {
				_ = json.Unmarshal(msg.Payload, &gqlErrs)
			}
			if len(gqlErrs) > 0 {
				pushErr(errFromGraphQLErrors(gqlErrs))
			} else {
				pushErr(fmt.Errorf("kubetail-api: subscription error: %s", string(msg.Payload)))
			}
			return
		case msgComplete:
			return
		}
	}
}

// gqlWSMessage is the envelope shared by every graphql-transport-ws frame.
// Payload is left as RawMessage so per-type decoding stays explicit.
type gqlWSMessage struct {
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// isExpectedCloseErr returns true for WebSocket close frames the caller
// shouldn't surface as errors (normal completion or our own cancellation).
func isExpectedCloseErr(err error) bool {
	return gwebsocket.IsCloseError(err,
		gwebsocket.CloseNormalClosure,
		gwebsocket.CloseGoingAway,
		gwebsocket.CloseNoStatusReceived,
	)
}

func (r gqlLogRecord) toLogRecord() logs.LogRecord {
	return logs.LogRecord{
		Timestamp: r.Timestamp,
		Message:   r.Message,
		Source: logs.LogSource{
			Metadata: logs.LogSourceMetadata{
				Region: r.Source.Metadata.Region,
				Zone:   r.Source.Metadata.Zone,
				OS:     r.Source.Metadata.OS,
				Arch:   r.Source.Metadata.Arch,
				Node:   r.Source.Metadata.Node,
			},
			Namespace:     r.Source.Namespace,
			PodName:       r.Source.PodName,
			ContainerName: r.Source.ContainerName,
			ContainerID:   r.Source.ContainerID,
		},
	}
}
