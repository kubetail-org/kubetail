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

package session

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

// A subagent's session is its parent's under Kind Subagent, compared field by
// field.
func TestNarrowKeepsTheParentsIdentity(t *testing.T) {
	folders := []Folder{{Path: "/Users/me/code", Write: true}}
	for _, outside := range []bool{false, true} {
		parent := Session{Kind: Chat, Outside: outside, Folders: func(context.Context) ([]Folder, error) { return folders, nil }}

		got := Narrow(parent)

		assert.Equal(t, Subagent, got.Kind)
		assert.Equal(t, parent.Outside, got.Outside)
		read, err := got.GrantedFolders(t.Context())
		require.NoError(t, err)
		assert.Equal(t, folders, read, "a subagent reads its parent's folders")
	}
}

// Only a chat spawns a subagent; a narrowed session of another kind keeps
// its kind, so its boundary row holds.
func TestNarrowOfAMonitorStaysAMonitor(t *testing.T) {
	for _, kind := range []Kind{Monitor, Console, Subagent} {
		assert.Equal(t, kind, Narrow(Session{Kind: kind}).Kind)
	}
	assert.Equal(t, Subagent, Narrow(Session{Kind: Chat}).Kind)
}

// Kind is the principal a proxy stamps on every action.
func TestAKindIsAPrincipal(t *testing.T) {
	assert.Equal(t, permissions.Chat, Chat)
	assert.Equal(t, permissions.Principals, []Kind{Chat, Subagent, Monitor, Console})
}

func TestASessionWithNoFoldersReadsNone(t *testing.T) {
	folders, err := Session{Kind: Chat}.GrantedFolders(t.Context())
	require.NoError(t, err)
	assert.Nil(t, folders)
}

// A read of the folders that fails is the reader's error, never an empty
// list.
func TestAFoldersReadThatFailsIsAnError(t *testing.T) {
	failed := errors.New("no folders")
	_, err := Session{Folders: func(context.Context) ([]Folder, error) { return nil, failed }}.GrantedFolders(t.Context())
	assert.ErrorIs(t, err, failed)
}

// A subagent decides by its parent's mode and rules.
func TestNarrowKeepsThePolicy(t *testing.T) {
	policy := permissions.Policy{Mode: permissions.ModeAuto, Rules: []permissions.Rule{permissions.Refused}}
	var read []string
	parent := Session{Kind: Chat, Policy: func(_ context.Context, target permissions.Target, account string) permissions.Policy {
		read = append(read, string(target)+":"+account)
		return policy
	}}

	got := Narrow(parent)

	assert.Equal(t, policy, got.Policy(t.Context(), permissions.AWS, "123456789012"))
	assert.Equal(t, []string{"aws:123456789012"}, read)
}

// A subagent has its parent's network, the turn's toggle included, for its
// whole life.
func TestNarrowKeepsTheNetwork(t *testing.T) {
	parent := Session{Kind: Chat, Network: func(context.Context) Network { return NetworkTurn }}

	got := Narrow(parent)

	assert.Equal(t, NetworkTurn, got.Network(t.Context()))
}

// A session made without sessionFor has no network, ever.
func TestTheZeroSessionHasNoNetwork(t *testing.T) {
	assert.Nil(t, Session{Kind: Monitor}.Network)
}

// A session with no Policy is read-only: its command's rules allow nothing.
func TestASessionWithNoPolicyIsReadOnly(t *testing.T) {
	allow := permissions.Rule{Effect: permissions.Allow}

	got := Session{Kind: Chat}.PolicyFor(t.Context(), permissions.Kubernetes, "c1", []permissions.Rule{allow})

	assert.Equal(t, permissions.Policy{Mode: permissions.ModeReadOnly}, got)
}

// The policy is read for the action's target and account, and the command's
// rules follow the session's without being written into its slice.
func TestPolicyForJoinsTheCommandsRules(t *testing.T) {
	held := make([]permissions.Rule, 1, 4)
	held[0] = permissions.Rule{ID: "held"}
	var target permissions.Target
	var account string
	s := Session{Kind: Chat, Policy: func(_ context.Context, tg permissions.Target, a string) permissions.Policy {
		target, account = tg, a
		return permissions.Policy{Mode: permissions.ModeAsk, Rules: held}
	}}

	got := s.PolicyFor(t.Context(), permissions.AWS, "123456789012", []permissions.Rule{{ID: "command"}})

	assert.Equal(t, permissions.AWS, target)
	assert.Equal(t, "123456789012", account)
	assert.Equal(t, permissions.ModeAsk, got.Mode)
	assert.Equal(t, []permissions.Rule{{ID: "held"}, {ID: "command"}}, got.Rules)
	assert.Equal(t, permissions.Rule{}, held[:2][1], "the session's slice is not written past its length")
}
