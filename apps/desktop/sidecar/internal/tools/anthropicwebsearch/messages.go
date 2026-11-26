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

package anthropicwebsearch

import (
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

var (
	_ tools.Budgeted         = (*Tool)(nil)
	_ llm.MessagesServerTool = (*Tool)(nil)
)

// MessagesOffer is web_search_20260318 at maxUses. The model calls the search
// itself: the default runs it from a code-execution container, which would put
// that container's blocks in the reply.
func (t *Tool) MessagesOffer(maxUses int) anthropic.ToolUnionParam {
	return anthropic.ToolUnionParam{OfWebSearchTool20260318: &anthropic.WebSearchTool20260318Param{
		MaxUses:        anthropic.Int(int64(maxUses)),
		AllowedCallers: []string{"direct"},
	}}
}

// The SDK's constants, so the names read are the ones the offer sends.

func (t *Tool) MessagesCallName() string { return string(constant.WebSearch("").Default()) }

func (t *Tool) MessagesResultType() string {
	return string(constant.WebSearchToolResult("").Default())
}

func (t *Tool) MessagesUses(u anthropic.ServerToolUsage) int { return int(u.WebSearchRequests) }
