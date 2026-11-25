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

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/clustercard"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/memory"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// The terms of what one turn adds to its request before the model's last answer,
// each named so a change to a budget moves the ceiling.

// bytesPerToken turns a byte budget into tokens: about half what English costs,
// and near the worst case for command output (paths, hashes, JSON).
const bytesPerToken = 2

// contextTokens is the question's context block at its largest: the card and
// both memory scopes at their budgets, the workspace's path, and the View.
const contextTokens = (clustercard.Budget + 2*memory.ScopeBudget + workspaceShare + viewBudget) / bytesPerToken

// questionTokens is a long question.
const questionTokens = 4_000

// typicalResultBytes is a tool result as one usually comes back. A turn whose
// results run larger is stopped by the provider's refusal.
const typicalResultBytes = 4 << 10

// turnInputAllowance is what one turn on box adds to its request, for a budget of
// maxCalls calls: the context block and a long question, the budget's worth of
// results at a typical size when the box holds a tool the sidecar runs, and each
// provider tool's own allowance.
func turnInputAllowance(box tools.Box, maxCalls int) int {
	sum := contextTokens + questionTokens
	if box.HasRunner() {
		sum += maxCalls * typicalResultBytes / bytesPerToken
	}
	for _, b := range box.Budgeted() {
		sum += b.Allowance()
	}
	return sum
}

// contextCeiling is the most a chat may have used before a send on m is refused:
// the window, less the cap every request asks for, less the allowance. Zero for a
// model whose window is not stated. A ceiling at or under zero is not checked
// ahead. A model that states no cap reserves nothing for the answer, since none is
// asked for.
func contextCeiling(m llm.Model, allowance int) int64 {
	if m.ContextWindow == 0 {
		return 0
	}
	return int64(m.ContextWindow - m.MaxOutputTokens - allowance)
}

// roomFor refuses a send the target could not read: a chat whose newest turn this
// model refused to read on its first request, or one past the model's ceiling by
// what its own provider last counted on the chat, since an older turn's request
// on that provider is a prefix of this one's and another provider's count does
// not carry. A turn that overflowed on a later request was pushed over by its own
// tool results, which the next request drops. box is what the turn would be
// offered.
func roomFor(ctx context.Context, st stmts, chatID ChatID, target llm.Target, box tools.Box) error {
	run, ok, err := newestRun(ctx, st, chatID)
	if err != nil {
		return err
	}
	// A refused request reports no usage, so the count alone would admit the send.
	if ok && run.status == RunFailed && run.errText == contextFullText && run.firstCallErred &&
		run.providerID == target.Provider.ID && run.modelID == target.Model.ID {
		return ErrChatContextFull
	}
	ceiling := contextCeiling(target.Model, turnInputAllowance(box, MaxToolCalls))
	if ceiling <= 0 {
		return nil
	}
	used, err := lastContextUse(ctx, st, chatID, target.Provider.ID)
	if err != nil {
		return err
	}
	if used > ceiling {
		return ErrChatContextFull
	}
	return nil
}
