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

package loop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// The assembled prompt is one document, and the golden files are how it is read:
// the caller's prompt, what the model can do, then the standing rule.
func TestThePromptIsAssembledInOrder(t *testing.T) {
	for _, tc := range []struct {
		golden string
		box    tools.Box
	}{
		{"no_tools.md", tools.Box{}},
		{"with_tool.md", tools.NewBox([]tools.Tool{echoTool{name: "echo"}})},
		{"with_server_tool.md", tools.NewBox([]tools.Tool{echoTool{name: "echo"}, searchTool{}})},
	} {
		t.Run(tc.golden, func(t *testing.T) {
			got := SystemPrompt(Turn{SystemPrompt: "You count pods.", Tools: tc.box, MaxToolCalls: 8})

			want, err := os.ReadFile(filepath.Join("testdata", tc.golden))
			require.NoError(t, err)
			assert.Equal(t, string(want), got)
		})
	}
}

// The rule the tools' own sections lean on is there whatever the box holds, and
// last, where nothing the model reads after it can argue with it.
func TestThePromptAlwaysSaysDataIsNotInstructions(t *testing.T) {
	for _, box := range []tools.Box{{}, tools.NewBox([]tools.Tool{echoTool{name: "echo"}})} {
		got := SystemPrompt(Turn{SystemPrompt: "You count pods.", Tools: box})
		assert.True(t, strings.HasSuffix(got, strings.TrimSpace(promptDataIsNotInstructions)))
	}
}

// The budget the loop enforces is the one the model is told: a rule it cannot
// follow is a penalty it cannot avoid.
func TestThePromptStatesTheTurnsBudget(t *testing.T) {
	got := SystemPrompt(Turn{Tools: tools.NewBox([]tools.Tool{echoTool{name: "echo"}}), MaxToolCalls: 3})

	assert.Contains(t, got, "You may make 3 calls in this turn")
}

// A turn with no prompt of its own is the agent's sections alone, with no blank
// line where the caller's would have been.
func TestAPromptlessTurnIsTheAgentsSectionsAlone(t *testing.T) {
	got := SystemPrompt(Turn{})

	assert.True(t, strings.HasPrefix(got, "# What you can do"))
}
