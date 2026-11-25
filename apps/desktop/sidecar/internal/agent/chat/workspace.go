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
	"context"
	"encoding/json"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/clustercard"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// workspaceShare is the Workspace section's share of the context block: a
// path and its heading, an estimate, so a longer path still goes.
const workspaceShare = 512

// withWorkspace is card with the chat's workspace appended as its own section:
// the file tools take absolute paths alone, so the model learns the path here.
// The path is fixed for the chat, so the section changes the context block on
// the chat's first send alone.
func (s *service) withWorkspace(card string, id ChatID) string {
	return withSection(card, "Workspace", map[string]any{"path": tools.WorkspacePath(s.chatDir(id))})
}

// sandboxState is what the Sandbox section tells the model: whether the
// chat's commands run outside the sandbox, and, in it, whether the chat's
// switch or the turn's toggle gives them the internet, the folders they read
// and write, and the cloud targets they reach, by their words.
type sandboxState struct {
	outside         bool
	networkEnabled  bool
	networkThisTurn bool
	folders         []session.Folder
	cloudTargetsOn  []string
}

// cloudTargetsOn is the words of the cloud targets the build has whose switch
// is on; none when the switches cannot be read, as a run then reaches none. A
// machine with no sandbox draws no Sandbox section, so it reads nothing.
func (s *service) cloudTargetsOn(ctx context.Context) []string {
	if len(s.cloudTargets) == 0 || !s.sandboxState.Available {
		return nil
	}
	sw, err := s.settings.CloudPlatformSwitches(ctx)
	if err != nil {
		return nil
	}
	var on []string
	for _, target := range s.cloudTargets {
		if sw.On(target) {
			on = append(on, string(target))
		}
	}
	return on
}

// maxListedFolders is how many folders the Sandbox section names; past it, a
// count says how many it left out.
const maxListedFolders = 20

// withSandbox is card with where the chat's commands run appended as its own
// section, on a machine with a sandbox: in it, with the network they have and
// the folders they read and write and the cloud platforms they reach, or
// outside it once the user switched the chat, where none applies. The block
// goes again when what it says changes.
func (s *service) withSandbox(card string, state sandboxState) string {
	if !s.sandboxState.Available {
		return card
	}
	if state.outside {
		return withSection(card, "Sandbox", map[string]any{"commands": "outside"})
	}
	fields := map[string]any{"commands": "sandboxed", "network": s.networkValue(state)}
	if len(state.cloudTargetsOn) > 0 {
		fields["cloud"] = state.cloudTargetsOn
	}
	if folders := state.folders; len(folders) > 0 {
		read, readWrite := []string{}, []string{}
		for _, f := range folders[:min(len(folders), maxListedFolders)] {
			if f.Write {
				readWrite = append(readWrite, f.Path)
			} else {
				read = append(read, f.Path)
			}
		}
		fields["read"], fields["readWrite"] = read, readWrite
		if more := len(folders) - maxListedFolders; more > 0 {
			fields["more"] = more
		}
	}
	return withSection(card, "Sandbox", fields)
}

// networkValue is the section's network key: the network a sandboxed chat's
// commands have, the chat's switch over the turn's toggle.
func (s *service) networkValue(state sandboxState) string {
	switch {
	case !s.sandboxState.NetworkAvailable:
		return "unavailable on this machine"
	case state.networkEnabled:
		return "on for this chat"
	case state.networkThisTurn:
		return "on for this message"
	}
	return "off"
}

// sandboxHeading opens the Sandbox section. JSON escapes every newline in a
// value, so no value can spell it.
const sandboxHeading = "## Sandbox\n\n```json\n"

// withSandboxReplaced is newest with its Sandbox section saying state. The
// section is always the block's last, found by its heading; newest comes back
// unchanged when it already says so or has no such section.
func (s *service) withSandboxReplaced(newest string, state sandboxState) string {
	i := strings.LastIndex(newest, sandboxHeading)
	if !s.sandboxState.Available || i < 0 || i > 0 && !strings.HasSuffix(newest[:i], "\n\n") {
		return newest
	}
	if i == 0 {
		return s.withSandbox("", state)
	}
	// The card's seam between two sections ends the one before with its fence.
	return s.withSandbox(strings.TrimSuffix(newest[:i], "\n\n"), state)
}

// withSection is card with fields appended as the section heading.
func withSection(card, heading string, fields map[string]any) string {
	raw, err := json.Marshal(fields)
	if err == nil {
		card, err = clustercard.WithSection(card, heading, raw)
	}
	if err != nil { // coverage:ignore strings, lists of strings and counts always marshal, and WithSection takes any one value
		panic(err)
	}
	return card
}
