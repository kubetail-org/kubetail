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

package catalog

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/chatgpt"
)

// ChatGPTID is the ChatGPT plan's provider id: a provider of its own, apart from
// openai, since a payload replays only to the provider that wrote it.
const ChatGPTID = chatgpt.ID

// chatGPTCodes is the codes of OpenAI's errors-and-recovery page for Sign in
// with ChatGPT, kept on a failure so the chat can say what happened.
var chatGPTCodes = map[string]llm.CodeClass{
	"subscription_sharing_usage_limit_exceeded":   llm.CodeUsageLimit,
	"subscription_sharing_user_not_eligible":      llm.CodeOther,
	"subscription_sharing_usage_unavailable":      llm.CodeOther,
	"subscription_sharing_unsupported_capability": llm.CodeOther,
	"subscription_sharing_route_not_supported":    llm.CodeOther,
	"subscription_sharing_invalid_user":           llm.CodeOther,
	"subscription_sharing_user_unavailable":       llm.CodeOther,
	"chatpass_v2_scope_not_authorized":            llm.CodeOther,
	"chatpass_v2_invalid_authorization_context":   llm.CodeOther,
}

// newChatGPTProvider is the Responses API on the user's ChatGPT plan, signed by
// its connector. Its catalog is the account's, read when the connection changes;
// it starts empty. The route is a preview whose field list moves: every field it
// refuses goes in Omit, never in the wire. The wire asks for the reasoning only
// with an effort, and a model listed with none still reasons, so Extra asks on
// every request: under store: false the encrypted copy is the only one a tool
// round can replay, and the summary is the thinking the transcript shows.
func newChatGPTProvider() llm.Provider {
	return llm.Provider{
		ID: ChatGPTID, Label: "ChatGPT plan", Dialect: llm.DialectResponses,
		BaseURL: chatgpt.APIBaseURL,
		Extra: map[string]any{
			"include":           []string{"reasoning.encrypted_content"},
			"reasoning.summary": "auto",
		},
		Omit:  []string{"max_output_tokens"},
		Codes: chatGPTCodes,
	}
}

// chatGPTModels decodes the account's GET /models: each entry whose visibility
// is "list", in the server's order, under its slug and display name. The rest
// of a model is the openai row's entry of the same id; an id that row lacks is
// listed with no efforts, tools, the written cap (the wire refuses a model with
// none, and Omit deletes it) and no context window. A slug listed twice keeps
// its first entry, and one with no display name is labelled with its slug. A
// body of another shape lists nothing.
func chatGPTModels(body []byte) []llm.Model {
	var list struct {
		Models []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			Visibility  string `json:"visibility"`
		} `json:"models"`
	}
	if json.Unmarshal(body, &list) != nil {
		return nil
	}
	known := map[string]llm.Model{}
	for _, k := range newOpenAIProvider().Catalog {
		known[k.ID] = k
	}
	listed := map[string]bool{}
	var models []llm.Model
	for _, m := range list.Models {
		if m.Visibility != "list" || m.Slug == "" || listed[m.Slug] {
			continue
		}
		listed[m.Slug] = true
		model, ok := known[m.Slug]
		if !ok {
			model = llm.Model{ID: m.Slug, MaxOutputTokens: writtenCap, Tools: true, Cache: llm.CacheAuto}
		}
		model.Label = cmp.Or(m.DisplayName, m.Slug)
		models = append(models, model)
	}
	return models
}

// maxModelsBody caps the models list.
const maxModelsBody = 1 << 20

// ReadChatGPTModels reads the account's catalog off p, the chatgpt row.
func ReadChatGPTModels(ctx context.Context, p llm.Provider) ([]llm.Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := llm.SignedDo(p, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog: the models list answered %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxModelsBody))
	if err != nil {
		return nil, err
	}
	return chatGPTModels(body), nil
}
