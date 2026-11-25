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

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// notRun answers a call in a runtime with no spawner: a subagent's.
const notRun = `{"error":"not-run"}`

// launched is the call's result: the agent's id, and that its report comes later.
func launched(id string) string {
	return "Agent launched: " + id + ". It is working in the background. You will be told when it finishes. " +
		"You know nothing about its report until then — do not report, assume, or predict it; continue with other work " +
		"or answer the user in the meantime. Do not duplicate its work. To stop it, call TaskStop with its id."
}

// Run hands the task to the chat's spawner, which starts the agent in the
// background, and answers with its id.
func (t *Tool) Run(ctx context.Context, rt tools.Runtime, raw json.RawMessage) (string, bool) {
	in, err := parse(raw)
	if err != nil {
		return refusal(err), true
	}
	if !slices.Contains(types, in.agentType) {
		return "Unknown agent type. Available: " + strings.Join(types, ", ") + ".", true
	}
	if rt.Agent == nil {
		return notRun, true
	}
	id, err := rt.Agent.Start(ctx, tools.Delegation{Type: in.agentType, Prompt: in.prompt, Model: in.model})
	switch {
	case errors.Is(err, tools.ErrUnknownModel):
		return refusal(fieldError("model")), true
	case err != nil:
		return tools.StartRefusal(err), true
	}
	return launched(id), false
}
