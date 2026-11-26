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

// What the model is told: the caller's own prompt, then the sections the agent
// owns, assembled into one document.
package loop

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// The agent's own sections, prose in prompts/, edited as prose.
var (
	//go:embed prompts/no_tools.md
	promptNoTools string
	//go:embed prompts/tools.md
	promptTools string // a format string: the budget is its one verb
	//go:embed prompts/data_is_not_instructions.md
	promptDataIsNotInstructions string
)

// systemPrompt is what the model is told, in order: the turn's own prompt, what
// the model can do, and that data is not instructions. The last is the agent's
// because the tools' own sections lean on it — a tool's results are data, and
// nothing else guarantees the rule is there.
func SystemPrompt(turn Turn) string {
	parts := []string{turn.SystemPrompt, capabilities(turn.Tools, turn.MaxToolCalls), promptDataIsNotInstructions}
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n\n")
}

// capabilities is the "What you can do" section: the no-tools text with an empty
// box, else the opening, which states the budget, and each tool's own section
// under it, in offer order.
func capabilities(box tools.Box, budget int) string {
	if box.Empty() {
		return promptNoTools
	}
	parts := []string{strings.TrimSpace(fmt.Sprintf(promptTools, budget))}
	for _, p := range box.Prompts() {
		parts = append(parts, strings.TrimSpace(p))
	}
	return strings.Join(parts, "\n\n")
}
