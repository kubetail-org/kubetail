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

// Test-only mocks shared by the resolver tests. The cluster fixtures are large
// enough to have a file of their own (cluster_testutils_test.go); the rest live
// here.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/amorey/gochan/watch"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/chat"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oauth"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
)

// fakeAuth is the Kubetail Cloud connector as the resolvers use it: State and
// WatchState over a latest-value hub, Connect signing in as loginAs, and
// Disconnect signing out.
type fakeAuth struct {
	mu      sync.Mutex
	loginAs oauth.Identity

	connectErr    error // when set, Connect fails without signing in
	disconnectErr error // when set, Disconnect fails without signing out

	hub *watch.Hub[connectors.State]
	tx  *watch.Sender[connectors.State]
	st  connectors.State
}

// newFakeAuth returns a signed-out fake whose Connect signs in as loginAs.
func newFakeAuth(loginAs oauth.Identity) *fakeAuth {
	return newFakeAuthState(connectors.State{Phase: connectors.SignedOut}, loginAs)
}

// signedInFakeAuth returns a fake already signed in as id.
func signedInFakeAuth(id oauth.Identity) *fakeAuth {
	return newFakeAuthState(connectedAs(id), id)
}

func newFakeAuthState(st connectors.State, loginAs oauth.Identity) *fakeAuth {
	f := &fakeAuth{st: st, loginAs: loginAs, hub: watch.New(st)}
	f.tx = f.hub.Sender()
	return f
}

// connectedAs is the state of a session signed in as id.
func connectedAs(id oauth.Identity) connectors.State {
	return connectors.State{Phase: connectors.Connected, Subject: id.UserID, Email: id.Email, Name: id.Name}
}

func (f *fakeAuth) State() connectors.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.st
}

func (f *fakeAuth) Connect(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.connectErr != nil {
		return f.connectErr
	}
	f.publishLocked(connectedAs(f.loginAs))
	return nil
}

func (f *fakeAuth) Disconnect(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.disconnectErr != nil {
		return f.disconnectErr
	}
	f.publishLocked(connectors.State{Phase: connectors.SignedOut})
	return nil
}

func (f *fakeAuth) WatchState() (<-chan connectors.State, func()) {
	rx := f.hub.Receiver()
	return rx.Chan(), rx.Close
}

// publishLocked makes st the state (f.mu held).
func (f *fakeAuth) publishLocked(st connectors.State) {
	f.st = st
	f.tx.Send(st) //nolint:errcheck // Send never blocks; a closed hub is a no-op
}

// gqlError is the half of a GraphQL error a client may branch on: never the
// message, only the code.
type gqlError struct {
	Message    string         `json:"message"`
	Path       []any          `json:"path"`
	Extensions map[string]any `json:"extensions"`
}

// mutation posts one operation and returns its data and coded errors.
func mutation(t *testing.T, srvURL, mutation string) (map[string]any, []gqlError) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"query": mutation})
	raw := postGQL(t, srvURL, string(body))

	var resp struct {
		Data   map[string]any
		Errors []gqlError
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode response %s: %v", raw, err)
	}
	return resp.Data, resp.Errors
}

// firstFrame opens a subscription and returns its first frame's data, failing the test
// on any error: for a state watch, the whole value as it is now.
func firstFrame(t *testing.T, srvURL, subscription string) map[string]any {
	t.Helper()
	resp := openSSESubscription(t, srvURL, "", subscription)
	defer resp.Body.Close()
	ev := nextSSE(t, sseEvents(t, resp))
	var frame struct {
		Data   map[string]any
		Errors []gqlError
	}
	require.NoError(t, json.Unmarshal([]byte(ev.data), &frame), ev.data)
	require.Empty(t, frame.Errors, ev.data)
	return frame.Data
}

// refusingChat answers every send, cancel, decision, stop, rules read and watch with err, for the refusals the real service gives
// only under conditions a resolver test cannot set up. The embedded Service is nil,
// so any other call panics, and a test leaning on it by accident fails on its code.
type refusingChat struct {
	chat.Service
	err error
}

func (c refusingChat) Send(context.Context, *chat.ChatID, chat.Mode, apimeta.ClusterID, chat.SendSwitches, string, string, string, string, string, *chat.Snapshot) (chat.ChatMessage, error) {
	return chat.ChatMessage{}, c.err
}

func (c refusingChat) Cancel(context.Context, chat.ChatID) error { return c.err }

// recordingChat hands each send's snapshot to snaps and answers an empty message.
// Like refusingChat, any other call panics.
type recordingChat struct {
	chat.Service
	snaps chan *chat.Snapshot
}

func (c recordingChat) Send(_ context.Context, _ *chat.ChatID, _ chat.Mode, _ apimeta.ClusterID, _ chat.SendSwitches, _, _, _, _, _ string, snap *chat.Snapshot) (chat.ChatMessage, error) {
	c.snaps <- snap
	return chat.ChatMessage{}, nil
}

// switchesChat hands each send's switches to switches and answers an empty
// message. Any other call panics.
type switchesChat struct {
	chat.Service
	switches chan chat.SendSwitches
}

func (c switchesChat) Send(_ context.Context, _ *chat.ChatID, _ chat.Mode, _ apimeta.ClusterID, switches chat.SendSwitches, _, _, _, _, _ string, _ *chat.Snapshot) (chat.ChatMessage, error) {
	c.switches <- switches
	return chat.ChatMessage{}, nil
}

func (c refusingChat) Approve(context.Context, chat.ApprovalID, chat.ApprovalDecision) (bool, error) {
	return false, c.err
}

func (c refusingChat) StopBackgroundTask(context.Context, chat.ToolCallID) (bool, error) {
	return false, c.err
}

func (c refusingChat) ChatRules(context.Context, chat.ChatID) ([]permissions.Rule, error) {
	return nil, c.err
}

func (c refusingChat) WatchCollection(context.Context) (*chat.Stream[chat.ChatCollectionWatchFrame], error) {
	return nil, c.err
}

func (c refusingChat) WatchMessageCollection(context.Context, chat.ChatID) (*chat.Stream[chat.ChatMessageCollectionWatchFrame], error) {
	return nil, c.err
}

// testSettings is a settings service over a fresh app.db: every default.
func testSettings(t *testing.T) *settings.Service {
	t.Helper()
	s, err := settings.Open(appdbtest.Open(t))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	return settings.NewService(s, nil, nil, nil, "")
}
