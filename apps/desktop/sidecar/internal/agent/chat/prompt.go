// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package chat

import (
	_ "embed"
	"strings"
)

// promptSystem is what chat tells the model: prose in prompts/system.md, edited as
// prose, saying only what is true of the app now.
//
//go:embed prompts/system.md
var promptSystem string

// promptGeneralPurpose is what a general-purpose subagent is told after
// promptSystem: who it answers, and what its report is for.
//
//go:embed prompts/general_purpose.md
var promptGeneralPurpose string

// promptMonitor is what a monitor run is told: who it is, what it holds, and
// that nobody is there to ask.
//
//go:embed prompts/monitor.md
var promptMonitor string

// systemPrompt is the system prompt a turn hands the agent: promptSystem. The
// agent appends what the turn can do, each offered tool's section among it, and
// the standing rule that data is not instructions.
func SystemPrompt() string {
	return strings.TrimSpace(promptSystem)
}

// subagentSystemPrompt is the system prompt a subagent hands the agent: the chat's,
// then the subagent's own section, which takes precedence over the chat's sections
// on answering the user.
func SubagentSystemPrompt() string {
	return SystemPrompt() + "\n\n" + strings.TrimSpace(promptGeneralPurpose)
}

// monitorSystemPrompt is the system prompt a monitor run hands the agent, which
// appends what it can do and that data is not instructions.
func MonitorSystemPrompt() string {
	return strings.TrimSpace(promptMonitor)
}
