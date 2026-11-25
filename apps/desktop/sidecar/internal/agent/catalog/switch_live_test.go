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

//go:build liveswitch

package catalog

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
)

// echo is the one tool the walk offers: each step's prompt makes the model call it,
// so every record the next step reads holds a round.
var echo = llm.ToolDefinition{
	Name:        "echo",
	Description: "Returns the text it is given.",
	InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
}

// maxRounds bounds one step's tool loop.
const maxRounds = 3

// A chat walks every dialect: Anthropic, OpenAI, one Chat Completions vendor, then
// Anthropic again, each at its first tool model's default effort. Each step is a
// real round whose history is the steps before it, each row under the provider
// that wrote it, and each must stream a reply the API accepts. A provider with no
// key is skipped by name.
func TestAChatSwitchesAcrossEveryDialect(t *testing.T) {
	svc := llm.New(New(Config{Credentials: newKeys(t, liveKeys(t), nil)}).Providers()...)
	walk := []string{"anthropic", "openai", chatCompletionsVendor(t, svc), "anthropic"}

	var history []llm.Message
	for i, providerID := range walk {
		target, ok := toolTarget(t, svc, providerID)
		if !ok {
			t.Logf("step %d: skipped %q: no key, or no model that takes tools", i, providerID)
			continue
		}
		question := llm.Message{Role: "user", Blocks: []llm.Block{llm.TextBlock(fmt.Sprintf(
			"Call the echo tool once with the text %q, then answer with the word it returns.", fmt.Sprintf("step %d", i)))}}
		answer := llm.Message{Role: "assistant", ProviderID: target.Provider.ID, Dialect: target.Provider.Dialect, Effort: target.Effort}
		for range maxRounds {
			msgs := append(slices.Clone(history), question, answer)
			resp, err := target.Stream(t.Context(), "Be terse.", msgs, "", []llm.ToolDefinition{echo}, nil, func(llm.Chunk) {})
			require.NoError(t, err, "step %d on %s/%s", i, target.Provider.ID, target.Model.ID)
			answer.Blocks = append(answer.Blocks, resp.Blocks...)
			calls := 0
			for _, b := range resp.Blocks {
				if b.Type == llm.BlockToolUse {
					answer.Blocks = append(answer.Blocks, llm.ToolResultBlock(b.ID, string(b.Input), false))
					calls++
				}
			}
			if calls == 0 {
				break
			}
		}
		fmt.Printf("step %d\t%s\t%s\t%d blocks\n", i, target.Provider.ID, target.Model.ID, len(answer.Blocks))
		history = append(history, question, answer)
	}
}

// chatCompletionsVendor is the walk's Chat Completions step: Gemini when it is
// keyed, else the first keyed provider on that dialect, else "gemini" to be
// skipped by name.
func chatCompletionsVendor(t *testing.T, svc *llm.Service) string {
	t.Helper()
	var first string
	for _, p := range testutil.Current(t, svc.WatchProvidersState, "the providers") {
		if p.Dialect != llm.DialectChatCompletions || len(p.Catalog) == 0 {
			continue
		}
		if p.ID == "gemini" {
			return p.ID
		}
		if first == "" {
			first = p.ID
		}
	}
	if first == "" {
		return "gemini"
	}
	return first
}

// toolTarget is providerID's first model that takes tools, at its default effort.
func toolTarget(t *testing.T, svc *llm.Service, providerID string) (llm.Target, bool) {
	t.Helper()
	for _, p := range testutil.Current(t, svc.WatchProvidersState, "the providers") {
		if p.ID != providerID {
			continue
		}
		for _, m := range p.Catalog {
			if m.Tools {
				target, err := svc.Resolve(p.ID, m.ID, m.DefaultEffort)
				require.NoError(t, err)
				return target, true
			}
		}
	}
	return llm.Target{}, false
}
