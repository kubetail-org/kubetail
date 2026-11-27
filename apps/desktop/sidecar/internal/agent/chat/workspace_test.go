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

package chat

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// A chat's first question tells the model its workspace's path, after the
// card, and the path is fixed for the chat, so the next question with nothing
// else changed carries no context block.
func TestTheContextNamesTheWorkspace(t *testing.T) {
	t.Parallel()

	s := serviceWithClusterCards(t, &stubClusterCards{card: "## Cluster\n\n```json\n{}\n```"})

	first := sendAndSettle(t, s, nil, "1", "1", "one")
	path, err := json.Marshal(tools.WorkspacePath(s.chatDir(first.ChatID)))
	require.NoError(t, err)
	context := newestContextOf(t, s, first.ChatID)
	assert.Contains(t, context, "## Cluster")
	assert.Contains(t, context, "## Workspace\n\n```json\n{\"path\":"+string(path)+"}\n```")

	sendAndSettle(t, s, &first.ChatID, "1", "2", "two")
	assert.Equal(t, []llm.BlockType{llm.BlockText}, blockTypes(t, questions(t, s, first.ChatID)[1]), "nothing changed")
}

// On a machine with a sandbox the context says where the chat's commands run,
// after the workspace, and says it again once the user switches the chat.
func TestTheContextSaysWhereCommandsRun(t *testing.T) {
	t.Parallel()

	s := serviceWithClusterCards(t, &stubClusterCards{card: "## Cluster\n\n```json\n{}\n```"})
	s.sandboxState = sandbox.State{Available: true, NetworkAvailable: true}

	first := sendAndSettle(t, s, nil, "1", "1", "one")
	context := newestContextOf(t, s, first.ChatID)
	sandboxed := "## Sandbox\n\n```json\n{\"commands\":\"sandboxed\",\"network\":\"off\"}\n```"
	assert.Contains(t, context, sandboxed)
	assert.Greater(t, strings.Index(context, sandboxed), strings.Index(context, "## Workspace"), "after the workspace")

	_, err := s.SetSandboxDisabled(t.Context(), first.ChatID, true)
	require.NoError(t, err)
	sendAndSettle(t, s, &first.ChatID, "1", "2", "two")
	assert.Contains(t, newestContextOf(t, s, first.ChatID), "## Sandbox\n\n```json\n{\"commands\":\"outside\"}\n```")

	// A message's override says so for its turn alone.
	other := sendAndSettle(t, s, nil, "1", "3", "three")
	overridden, err := s.Send(t.Context(), &other.ChatID, ModeChat, "1", SendSwitches{SandboxDisabledThisTurn: true}, "fake", "fake", "high", reqID("4"), "four", nil)
	require.NoError(t, err)
	awaitSettled(t, s, other.ChatID, overridden.ID)
	assert.Contains(t, newestContextOf(t, s, other.ChatID), "## Sandbox\n\n```json\n{\"commands\":\"outside\"}\n```")
	sendAndSettle(t, s, &other.ChatID, "1", "5", "five")
	assert.Contains(t, newestContextOf(t, s, other.ChatID), sandboxed)

	none := serviceWithClusterCards(t, &stubClusterCards{card: "## Cluster\n\n```json\n{}\n```"})
	msg := sendAndSettle(t, none, nil, "1", "1", "one")
	assert.NotContains(t, newestContextOf(t, none, msg.ChatID), "## Sandbox")
}

// withSandboxReplaced swaps the newest context's Sandbox section, which is
// always its last, and hands the context back unchanged when there is nothing
// to swap.
func TestWithSandboxReplacedSwapsTheLastSection(t *testing.T) {
	t.Parallel()

	s := &service{sandboxState: sandbox.State{Available: true, NetworkAvailable: true}}
	card := "## Cluster\n\n```json\n{}\n```"
	off, chat := sandboxState{}, sandboxState{networkEnabled: true}
	outside := sandboxState{outside: true}

	assert.Equal(t, s.withSandbox(card, outside), s.withSandboxReplaced(s.withSandbox(card, off), outside))
	assert.Equal(t, s.withSandbox(card, off), s.withSandboxReplaced(s.withSandbox(card, outside), off))
	assert.Equal(t, s.withSandbox(card, off), s.withSandboxReplaced(s.withSandbox(card, chat), off), "the network moved")
	assert.Equal(t, s.withSandbox(card, chat), s.withSandboxReplaced(s.withSandbox(card, chat), chat), "already says so")
	assert.Equal(t, s.withSandbox("", off), s.withSandboxReplaced(s.withSandbox("", chat), off), "the block's only section")
	assert.Equal(t, card, s.withSandboxReplaced(card, outside), "no section to swap")

	code := sandboxState{folders: []session.Folder{{Path: "/home/me/code"}}}
	assert.Equal(t, s.withSandbox(card, code), s.withSandboxReplaced(s.withSandbox(card, off), code), "the folders changed")
	assert.Equal(t, s.withSandbox(card, off), s.withSandboxReplaced(s.withSandbox(card, code), off))
	assert.Equal(t, s.withSandbox("", off), s.withSandboxReplaced(s.withSandbox("", code), off), "a block of the section alone")

	none := &service{}
	assert.Equal(t, card, none.withSandboxReplaced(card, outside), "no sandbox on this machine")
}

// sandboxSectionOf is the newest context's Sandbox section, decoded.
func sandboxSectionOf(t *testing.T, s *service, chatID ChatID) map[string]string {
	t.Helper()
	context := newestContextOf(t, s, chatID)
	_, section, ok := strings.Cut(context, "## Sandbox\n\n```json\n")
	require.True(t, ok, context)
	var got map[string]string
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSuffix(section, "\n```")), &got))
	return got
}

// The Sandbox section says the network a sandboxed chat's commands have: the
// chat's switch over the turn's toggle, off, or none on this machine at all.
// It is sent again whenever it says something new, so a question after one
// sent with the toggle says off.
func TestTheContextSaysTheNetwork(t *testing.T) {
	t.Parallel()

	s := serviceWithClusterCards(t, &stubClusterCards{card: "## Cluster\n\n```json\n{}\n```"})
	s.sandboxState = sandbox.State{Available: true, NetworkAvailable: true}

	first := sendAndSettle(t, s, nil, "1", "1", "one")
	assert.Equal(t, "off", sandboxSectionOf(t, s, first.ChatID)["network"])

	toggled, err := s.Send(t.Context(), &first.ChatID, ModeChat, "1", SendSwitches{NetworkThisTurn: true}, "fake", "fake", "high", reqID("2"), "two", nil)
	require.NoError(t, err)
	awaitSettled(t, s, toggled.ChatID, toggled.ID)
	assert.Equal(t, "on for this message", sandboxSectionOf(t, s, first.ChatID)["network"])

	sendAndSettle(t, s, &first.ChatID, "1", "3", "three")
	assert.Equal(t, "off", sandboxSectionOf(t, s, first.ChatID)["network"], "the toggle was that message's alone")

	_, err = s.SetNetworkEnabled(t.Context(), first.ChatID, true)
	require.NoError(t, err)
	switched, err := s.Send(t.Context(), &first.ChatID, ModeChat, "1", SendSwitches{NetworkEnabled: true, NetworkThisTurn: true}, "fake", "fake", "high", reqID("4"), "four", nil)
	require.NoError(t, err)
	awaitSettled(t, s, switched.ChatID, switched.ID)
	assert.Equal(t, "on for this chat", sandboxSectionOf(t, s, first.ChatID)["network"])

	_, err = s.SetSandboxDisabled(t.Context(), first.ChatID, true)
	require.NoError(t, err)
	// Leaving the sandbox turned the switch off, so the sender sees off.
	outside, err := s.Send(t.Context(), &first.ChatID, ModeChat, "1", SendSwitches{SandboxDisabled: true}, "fake", "fake", "high", reqID("5"), "five", nil)
	require.NoError(t, err)
	awaitSettled(t, s, outside.ChatID, outside.ID)
	assert.Equal(t, map[string]string{"commands": "outside"}, sandboxSectionOf(t, s, first.ChatID), "outside, there is no network line")

	none := serviceWithClusterCards(t, &stubClusterCards{card: "## Cluster\n\n```json\n{}\n```"})
	none.sandboxState = sandbox.State{Available: true, NetworkReason: "pasta not found"}
	msg := sendAndSettle(t, none, nil, "1", "1", "one")
	assert.Equal(t, "unavailable on this machine", sandboxSectionOf(t, none, msg.ChatID)["network"])
}

func TestTheSandboxSectionListsAtMostTwentyFolders(t *testing.T) {
	t.Parallel()

	s := &service{sandboxState: sandbox.State{Available: true}}
	var folders []session.Folder
	for i := range 22 {
		folders = append(folders, session.Folder{Path: fmt.Sprintf("/f/%02d", i), Write: i%2 == 1})
	}
	section := sectionOf(t, s.withSandbox("", sandboxState{folders: folders}))
	assert.Len(t, section["read"], 10)
	assert.Len(t, section["readWrite"], 10)
	assert.EqualValues(t, 2, section["more"])
	assert.Equal(t, map[string]any{"commands": "outside"}, sectionOf(t, s.withSandbox("", sandboxState{outside: true, folders: folders})),
		"a chat outside the sandbox lists none")
	assert.Equal(t, map[string]any{"commands": "sandboxed", "network": "unavailable on this machine"}, sectionOf(t, s.withSandbox("", sandboxState{})))
}

// sectionOf is the one section block holds, decoded.
func sectionOf(t *testing.T, block string) map[string]any {
	t.Helper()
	_, body, ok := strings.Cut(block, "```json\n")
	require.True(t, ok, block)
	var v map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSuffix(body, "\n```")), &v))
	return v
}

func TestTheContextListsTheGrants(t *testing.T) {
	t.Parallel()

	s := serviceWithClusterCards(t, &stubClusterCards{card: "## Cluster\n\n```json\n{}\n```"})
	home := grantable(t, s)
	code, svc := filepath.Join(home, "code"), filepath.Join(home, "code", "svc")
	require.NoError(t, s.GrantFolder(t.Context(), "", code, false))
	require.NoError(t, s.settings.GrantFolder(t.Context(), filepath.Join(home, "gone"), false))

	first := sendAndSettle(t, s, nil, "1", "1", "one")
	context := newestContextOf(t, s, first.ChatID)
	_, last, _ := strings.Cut(context, "## Sandbox")
	assert.Equal(t, map[string]any{"commands": "sandboxed", "network": "unavailable on this machine", "read": []any{code}, "readWrite": []any{}}, sectionOf(t, last),
		"a refused folder is left out")

	require.NoError(t, s.GrantFolder(t.Context(), first.ChatID, svc, true))
	sendAndSettle(t, s, &first.ChatID, "1", "2", "two")
	_, last, _ = strings.Cut(newestContextOf(t, s, first.ChatID), "## Sandbox")
	assert.Equal(t, map[string]any{"commands": "sandboxed", "network": "unavailable on this machine", "read": []any{code}, "readWrite": []any{svc}}, sectionOf(t, last),
		"the next question carries the grants as they are now")
}

// The section names the cloud platforms the build has whose switch is on,
// leaves the key out with none, and goes again when a switch changes.
func TestTheSandboxSectionNamesTheCloudPlatformsOn(t *testing.T) {
	t.Parallel()

	s := serviceWithClusterCards(t, &stubClusterCards{card: "## Cluster\n\n```json\n{}\n```"})
	s.sandboxState = sandbox.State{Available: true, NetworkAvailable: true}
	s.cloudTargets = []permissions.Target{permissions.AWS, permissions.GCP}

	first := sendAndSettle(t, s, nil, "1", "1", "one")
	assert.Contains(t, newestContextOf(t, s, first.ChatID),
		"## Sandbox\n\n```json\n{\"cloud\":[\"aws\",\"gcp\"],\"commands\":\"sandboxed\",\"network\":\"off\"}\n```",
		"a switch the build has no cloud platform for is not named")

	require.NoError(t, s.settings.SetCloudPlatformSwitch(t.Context(), permissions.AWS, false))
	sendAndSettle(t, s, &first.ChatID, "1", "2", "two")
	assert.Contains(t, newestContextOf(t, s, first.ChatID),
		"## Sandbox\n\n```json\n{\"cloud\":[\"gcp\"],\"commands\":\"sandboxed\",\"network\":\"off\"}\n```")

	require.NoError(t, s.settings.SetCloudPlatformSwitch(t.Context(), permissions.GCP, false))
	sendAndSettle(t, s, &first.ChatID, "1", "3", "three")
	assert.Contains(t, newestContextOf(t, s, first.ChatID), "## Sandbox\n\n```json\n{\"commands\":\"sandboxed\",\"network\":\"off\"}\n```")

	off := sandboxState{}
	on := sandboxState{cloudTargetsOn: []string{"gcp"}}
	newest := s.withSandbox("## Cluster\n\n```json\n{}\n```", off)
	assert.NotEqual(t, newest, s.withSandboxReplaced(newest, on), "a switch that moved sends the section again")
}

// A send whose switches cannot be read names no cloud platform.
func TestTheSandboxSectionNamesNoCloudPlatformWhenTheSwitchesCannotBeRead(t *testing.T) {
	t.Parallel()

	s := serviceWithClusterCards(t, &stubClusterCards{card: "## Cluster\n\n```json\n{}\n```"})
	s.sandboxState = sandbox.State{Available: true, NetworkAvailable: true}
	s.cloudTargets = []permissions.Target{permissions.AWS}
	// The switches read from a file of their own, so the chat's own reads of
	// settings still answer.
	unread := appdbtest.Open(t)
	s.settings = settings.NewService(settingsStore(t, unread), nil, nil, nil, "")
	_, err := unread.Write.Exec(`DROP TABLE settings`)
	require.NoError(t, err)

	sent := sendAndSettle(t, s, nil, "1", "1", "one")
	assert.Contains(t, newestContextOf(t, s, sent.ChatID), "## Sandbox\n\n```json\n{\"commands\":\"sandboxed\",\"network\":\"off\"}\n```")
}
