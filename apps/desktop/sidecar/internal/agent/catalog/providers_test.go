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
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/providercredentials"
)

// Every keyed provider says everything a send needs: an id, a label, a dialect
// with a wire, a base URL, its key variable, and a catalog with one entry per
// model id, since a send names a model by its id alone. A cap is required
// on the two dialects whose wire refuses a request without one, and a model's
// default is one of its own efforts, since that is what a picker starts on.
func TestEveryKeyedProviderIsWhole(t *testing.T) {
	for _, kp := range keyedRows() {
		p := kp.provider
		assert.NotEmpty(t, p.ID)
		assert.NotEmpty(t, p.Label, p.ID)
		wires := []llm.Dialect{llm.DialectMessages, llm.DialectResponses, llm.DialectChatCompletions}
		assert.Contains(t, wires, p.Dialect, p.ID)
		assert.NotEmpty(t, p.BaseURL, p.ID)
		assert.NotEmpty(t, kp.keyVar, p.ID)
		assert.NotEmpty(t, p.Catalog, p.ID)
		var ids []string
		for _, m := range p.Catalog {
			assert.NotEmpty(t, m.ID, p.ID)
			assert.NotContains(t, ids, m.ID, "%s lists %s twice", p.ID, m.ID)
			ids = append(ids, m.ID)
			assert.NotEmpty(t, m.Label, m.ID)
			if len(m.Efforts) == 0 {
				assert.Empty(t, m.DefaultEffort, m.ID)
			} else {
				assert.Contains(t, m.Efforts, m.DefaultEffort, m.ID)
			}
			d := p.ForModel(m).Dialect
			assert.Contains(t, wires, d, m.ID)
			if d == llm.DialectMessages || d == llm.DialectResponses {
				assert.Positive(t, m.MaxOutputTokens, m.ID)
			}
		}
	}
}

// rowIDs is rows' provider ids, in order.
func rowIDs(rows []row) []string {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.provider.ID)
	}
	return ids
}

// keyedRows is every row that takes a key: each one's catalog is written.
func keyedRows() []row {
	var keyed []row
	for _, r := range rows("") {
		if r.keyVar != "" {
			keyed = append(keyed, r)
		}
	}
	return keyed
}

var keyedIDs = []string{"anthropic", "openai", "gemini", "groq", "mistral", "deepseek", "xai", "openrouter", "together", "fireworks", "opencode-go"}

// Every provider is one row, in picker order: the keyed ones, then those only a
// connector signs. Only chatgpt reads its catalog.
func TestRowsAtListsEveryProviderOnce(t *testing.T) {
	all := rowsAt(nil, vertexBase)
	assert.Equal(t, append(slices.Clone(keyedIDs), "chatgpt", "google"), rowIDs(all))
	for _, r := range all {
		assert.Equal(t, r.provider.ID == ChatGPTID, r.models != nil, r.provider.ID)
		assert.Equal(t, slices.Contains(keyedIDs, r.provider.ID), r.keyVar != "", r.provider.ID)
	}
}

// With no project and no debug base, Vertex AI has no endpoint: its row alone
// is left out.
func TestRowsAtLeavesOutARowWithNoBaseURL(t *testing.T) {
	assert.Equal(t, append(slices.Clone(keyedIDs), "chatgpt"), rowIDs(rowsAt(nil, "")))
	assert.Contains(t, rowIDs(rowsAt(map[string]string{GoogleID: "http://127.0.0.1:1"}, "")), GoogleID)
}

// A debug base moves its row and clears each of its models' own bases, so one
// stub answers the whole row; it touches no other row.
func TestADebugBaseMovesItsRowAlone(t *testing.T) {
	const stub = "http://127.0.0.1:1"
	baseURLs := map[string]string{"opencode-go": stub, GoogleID: stub}
	release := rows(vertexBase)
	for i, r := range rowsAt(baseURLs, vertexBase) {
		p := r.provider
		if _, moved := baseURLs[p.ID]; !moved {
			assert.Equal(t, release[i].provider, p, p.ID)
			continue
		}
		assert.Equal(t, stub, p.BaseURL, p.ID)
		require.NotEmpty(t, p.Catalog, p.ID)
		for _, m := range p.Catalog {
			assert.Empty(t, m.BaseURL, m.ID)
		}
	}
}

// Vertex AI's catalog is written: the Gemini row's models under Vertex AI's
// publisher ids. That Vertex AI serves each id and effort is UNVERIFIED.
func TestTheGoogleRowsCatalogIsGeminisUnderVertexIDs(t *testing.T) {
	var google llm.Provider
	for _, r := range rows(vertexBase) {
		if r.provider.ID == GoogleID {
			google = r.provider
		}
	}
	gemini := newGeminiProvider().Catalog
	require.Len(t, google.Catalog, len(gemini))
	for i, m := range google.Catalog {
		want := gemini[i]
		want.ID = "google/" + want.ID
		assert.Equal(t, want, m)
	}
}

// modelIDs is a provider's catalog in picker order.
func modelIDs(p llm.Provider) []string {
	ids := make([]string, 0, len(p.Catalog))
	for _, m := range p.Catalog {
		ids = append(ids, m.ID)
	}
	return ids
}

// effortsOf is a provider's catalog as the efforts each model lists, by model id.
// A model listing none is left out.
func effortsOf(p llm.Provider) map[string][]string {
	out := map[string][]string{}
	for _, m := range p.Catalog {
		if len(m.Efforts) > 0 {
			out[m.ID] = m.Efforts
		}
	}
	return out
}

// windowsOf is a provider's catalog as the window each model states, by model
// id. A model stating none is left out.
func windowsOf(p llm.Provider) map[string]int {
	out := map[string]int{}
	for _, m := range p.Catalog {
		if m.ContextWindow > 0 {
			out[m.ID] = m.ContextWindow
		}
	}
	return out
}

// Each row is written out whole and as its comment says: the dialect, where it
// goes, the catalog in picker order, the efforts each model takes in the
// vendor's own words, the request parameters of the vendor's own, and each
// model's overrides of the row's dialect and base URL. The two dialects that
// require a cap get the written one on every model sent on them.
func TestEachKeyedProviderIsWrittenAsItsRow(t *testing.T) {
	// OpenCode Go overrides its Chat Completions default with each model's native
	// API: Responses at the row's URL, Messages at its own.
	type override struct {
		dialect llm.Dialect
		baseURL string
	}
	responses := override{dialect: llm.DialectResponses}
	messages := override{llm.DialectMessages, "https://opencode.ai/inference/go/anthropic"}
	overrides := map[string]override{
		"opencode-go/grok-4.7":         responses,
		"opencode-go/gpt-5.6-luna":     responses,
		"opencode-go/gpt-6-luna":       responses,
		"opencode-go/claude-haiku-5-5": messages,
		"opencode-go/minimax-m3":       messages,
		"opencode-go/qwen3.8-max":      messages,
		"opencode-go/qwen3.8-flash":    messages,
		"opencode-go/qwen3.7-plus":     messages,
	}
	rows := []struct {
		id, label string
		dialect   llm.Dialect
		baseURL   string
		models    []string
		efforts   map[string][]string
		extra     map[string]any
		windows   map[string]int
	}{
		{"anthropic", "Anthropic", llm.DialectMessages, "https://api.anthropic.com",
			[]string{"claude-fable-5-1", "claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"},
			map[string][]string{
				"claude-fable-5-1":  {"low", "medium", "high", "xhigh", "max"},
				"claude-opus-5-5":   {"low", "medium", "high", "xhigh", "max"},
				"claude-sonnet-5-5": {"low", "medium", "high", "xhigh", "max"},
				"claude-haiku-5-5":  {"low", "medium", "high", "xhigh", "max"},
			}, nil,
			map[string]int{"claude-fable-5-1": 1_000_000, "claude-opus-5-5": 1_000_000, "claude-sonnet-5-5": 1_000_000, "claude-haiku-5-5": 1_000_000}},
		{"openai", "OpenAI", llm.DialectResponses, "https://api.openai.com/v1",
			[]string{"gpt-6-astra", "gpt-5.6", "gpt-5.6-terra", "gpt-5.6-luna"},
			map[string][]string{
				"gpt-6-astra":   {"low", "medium", "high", "xhigh", "max"},
				"gpt-5.6":       {"none", "low", "medium", "high", "xhigh", "max"},
				"gpt-5.6-terra": {"none", "low", "medium", "high", "xhigh", "max"},
				"gpt-5.6-luna":  {"none", "low", "medium", "high", "xhigh", "max"},
			}, nil,
			map[string]int{"gpt-6-astra": 1_050_000, "gpt-5.6": 1_050_000, "gpt-5.6-terra": 1_050_000, "gpt-5.6-luna": 1_050_000}},
		{"gemini", "Gemini", llm.DialectChatCompletions, "https://generativelanguage.googleapis.com/v1beta/openai",
			[]string{"gemini-3.1-pro-preview", "gemini-3.8-flash", "gemini-3.5-flash-lite"},
			map[string][]string{
				"gemini-3.1-pro-preview": {"low", "medium", "high"},
				"gemini-3.8-flash":       {"low", "medium", "high"},
				"gemini-3.5-flash-lite":  {"minimal", "low", "medium", "high"},
			}, nil,
			map[string]int{"gemini-3.1-pro-preview": 1_048_576, "gemini-3.8-flash": 1_048_576, "gemini-3.5-flash-lite": 1_048_576}},
		{"groq", "Groq", llm.DialectChatCompletions, "https://api.groq.com/openai/v1",
			[]string{"openai/gpt-oss-120b", "openai/gpt-oss-20b", "qwen/qwen3.8-27b", "qwen/qwen3.6-27b"},
			map[string][]string{
				"openai/gpt-oss-120b": {"low", "medium", "high"},
				"openai/gpt-oss-20b":  {"low", "medium", "high"},
				"qwen/qwen3.8-27b":    {"none", "low", "medium", "high"},
				"qwen/qwen3.6-27b":    {"none", "default"},
			}, nil,
			map[string]int{"openai/gpt-oss-120b": 131_072, "openai/gpt-oss-20b": 131_072, "qwen/qwen3.8-27b": 262_144, "qwen/qwen3.6-27b": 131_072}},
		{"mistral", "Mistral", llm.DialectChatCompletions, "https://api.mistral.ai/v1",
			[]string{"mistral-large-latest", "mistral-medium-latest", "mistral-small-latest"},
			map[string][]string{}, nil,
			map[string]int{}},
		{"deepseek", "DeepSeek", llm.DialectChatCompletions, "https://api.deepseek.com/v1",
			[]string{"deepseek-chat", "deepseek-reasoner"},
			map[string][]string{}, nil,
			map[string]int{}},
		{"xai", "xAI", llm.DialectChatCompletions, "https://api.x.ai/v1",
			[]string{"grok-4.6", "grok-4.5", "grok-4.3"},
			map[string][]string{
				"grok-4.6": {"low", "medium", "high", "xhigh"},
				"grok-4.5": {"low", "medium", "high"},
				"grok-4.3": {"none", "low", "medium", "high"},
			}, nil,
			map[string]int{"grok-4.6": 500_000, "grok-4.5": 500_000, "grok-4.3": 1_000_000}},
		{"openrouter", "OpenRouter", llm.DialectChatCompletions, "https://openrouter.ai/api/v1",
			[]string{"openrouter/auto", "openai/gpt-5.6", "anthropic/claude-haiku-4.5"},
			map[string][]string{}, map[string]any{"provider.data_collection": "deny"},
			map[string]int{}},
		{"together", "Together", llm.DialectChatCompletions, "https://api.together.xyz/v1",
			[]string{"deepseek-ai/DeepSeek-V3", "meta-llama/Llama-3.3-70B-Instruct-Turbo", "Qwen/Qwen2.5-72B-Instruct-Turbo"},
			map[string][]string{}, nil,
			map[string]int{}},
		{"fireworks", "Fireworks", llm.DialectChatCompletions, "https://api.fireworks.ai/inference/v1",
			[]string{"accounts/fireworks/models/deepseek-v3", "accounts/fireworks/models/llama-v3p3-70b-instruct", "accounts/fireworks/models/qwen3-235b-a22b"},
			map[string][]string{}, nil,
			map[string]int{}},
		{"opencode-go", "OpenCode Go", llm.DialectChatCompletions, "https://opencode.ai/inference/go/openai/v1",
			[]string{
				"deepseek-v4.1-flash", "deepseek-v4-pro", "glm-5.2", "glm-5.3", "grok-4.7",
				"glm-5.3-flash", "gpt-5.6-luna", "gpt-6-luna", "claude-haiku-5-5", "hy3",
				"hy4-preview", "kimi-k2.7-code", "kimi-k3", "mimo-v2.6-flash", "mimo-v2.6-pro",
				"minimax-m3", "minimax-m3.1-flash", "longcat-2.0", "step-5-preview",
				"qwen3.8-max", "qwen3.8-flash", "qwen3.7-plus", "kimi-k3.1-pro",
				"gemini-4-argon", "hy4",
			},
			map[string][]string{
				"deepseek-v4.1-flash": {"low", "high", "max"},
				"deepseek-v4-pro":     {"high", "max"},
				"glm-5.2":             {"high", "max"},
				"glm-5.3":             {"low", "high", "max"},
				"grok-4.7":            {"low", "medium", "high", "xhigh"},
				"glm-5.3-flash":       {"low", "high", "max"},
				"gpt-5.6-luna":        {"none", "low", "medium", "high", "xhigh", "max"},
				"gpt-6-luna":          {"none", "low", "medium", "high", "xhigh", "max"},
				"claude-haiku-5-5":    {"low", "medium", "high", "xhigh", "max"},
				"hy3":                 {"none", "low", "high"},
				"hy4-preview":         {"none", "high"},
				"kimi-k3":             {"max"},
				"minimax-m3.1-flash":  {"low", "medium", "high", "xhigh", "max"},
				"step-5-preview":      {"low", "medium", "high"},
				"qwen3.8-max":         {"low", "medium", "xhigh"},
				"qwen3.8-flash":       {"low", "medium", "xhigh"},
				"kimi-k3.1-pro":       {"low", "high", "max"},
				"gemini-4-argon":      {"low", "medium", "high"},
				"hy4":                 {"none", "high"},
			}, nil,
			map[string]int{
				"deepseek-v4.1-flash": 1_000_000, "deepseek-v4-pro": 1_000_000,
				"glm-5.2": 1_000_000, "glm-5.3": 1_000_000, "grok-4.7": 500_000,
				"glm-5.3-flash": 1_000_000, "gpt-5.6-luna": 1_050_000, "gpt-6-luna": 1_050_000,
				"claude-haiku-5-5": 1_000_000, "hy3": 256_000, "hy4-preview": 1_024_000,
				"kimi-k2.7-code": 262_144, "kimi-k3": 1_048_576,
				"mimo-v2.6-flash": 1_048_576, "mimo-v2.6-pro": 1_048_576,
				"minimax-m3": 1_000_000, "minimax-m3.1-flash": 1_000_000,
				"longcat-2.0": 1_000_000, "step-5-preview": 1_000_000,
				"qwen3.8-max": 1_000_000, "qwen3.8-flash": 1_000_000,
				"qwen3.7-plus": 1_000_000, "kimi-k3.1-pro": 1_048_576,
				"gemini-4-argon": 1_048_576, "hy4": 1_048_576,
			}},
	}
	keyed := keyedRows()
	require.Len(t, keyed, len(rows))
	overridden := map[string]override{}
	for i, want := range rows {
		p := keyed[i].provider
		assert.Equal(t, want.id, p.ID)
		assert.Equal(t, want.label, p.Label, p.ID)
		assert.Equal(t, want.dialect, p.Dialect, p.ID)
		assert.Equal(t, want.baseURL, p.BaseURL, p.ID)
		assert.Equal(t, want.models, modelIDs(p), p.ID)
		assert.Equal(t, want.efforts, effortsOf(p), p.ID)
		assert.Equal(t, want.extra, p.Extra, p.ID)
		assert.Equal(t, want.windows, windowsOf(p), p.ID)
		for _, m := range p.Catalog {
			if o := (override{m.Dialect, m.BaseURL}); o != (override{}) {
				overridden[p.ID+"/"+m.ID] = o
			}
			if d := p.ForModel(m).Dialect; d == llm.DialectMessages || d == llm.DialectResponses {
				assert.Equal(t, writtenCap, m.MaxOutputTokens, m.ID)
			}
		}
	}
	assert.Equal(t, overrides, overridden)
}

// The provider denies data collection on every request, which is what the egress
// record rests on: OpenRouter routes only to downstream providers it classifies
// as not collecting user data.
func TestOpenRouterProviderDeniesDataCollection(t *testing.T) {
	assert.Equal(t, map[string]any{"provider.data_collection": "deny"}, newOpenRouterProvider().Extra)
}

// KeyVars is the table by provider id, and the fake is not in it.
func TestKeyVarsIsTheKeyedTable(t *testing.T) {
	assert.Equal(t, map[string]string{
		"anthropic":   "ANTHROPIC_API_KEY",
		"openai":      "OPENAI_API_KEY",
		"gemini":      "GEMINI_API_KEY",
		"groq":        "GROQ_API_KEY",
		"mistral":     "MISTRAL_API_KEY",
		"deepseek":    "DEEPSEEK_API_KEY",
		"xai":         "XAI_API_KEY",
		"openrouter":  "OPENROUTER_API_KEY",
		"together":    "TOGETHER_API_KEY",
		"fireworks":   "FIREWORKS_API_KEY",
		"opencode-go": "OPENCODE_GO_API_KEY",
	}, KeyVars())
}

// Each keyed provider's base URL moves under a variable of its own, and the fake,
// with no URL, under none. A hyphen in an id, which no shell variable can hold,
// is spelled as an underscore.
func TestBaseURLVarsNamesEachProvidersOverride(t *testing.T) {
	assert.Equal(t, map[string]string{
		"anthropic":   "KUBETAIL_ANTHROPIC_BASE_URL",
		"openai":      "KUBETAIL_OPENAI_BASE_URL",
		"gemini":      "KUBETAIL_GEMINI_BASE_URL",
		"groq":        "KUBETAIL_GROQ_BASE_URL",
		"mistral":     "KUBETAIL_MISTRAL_BASE_URL",
		"deepseek":    "KUBETAIL_DEEPSEEK_BASE_URL",
		"xai":         "KUBETAIL_XAI_BASE_URL",
		"openrouter":  "KUBETAIL_OPENROUTER_BASE_URL",
		"together":    "KUBETAIL_TOGETHER_BASE_URL",
		"fireworks":   "KUBETAIL_FIREWORKS_BASE_URL",
		"opencode-go": "KUBETAIL_OPENCODE_GO_BASE_URL",
		"chatgpt":     "KUBETAIL_CHATGPT_BASE_URL",
		"google":      "KUBETAIL_GOOGLE_BASE_URL",
	}, BaseURLVars())
	assert.Equal(t, "KUBETAIL_BEDROCK_GPT_BASE_URL", idVar("bedrock-gpt", "BASE_URL"))
}

// Every model of a wire whose whole catalog takes tools says so.
func TestEveryModelOnAWireThatCarriesToolsTakesThem(t *testing.T) {
	for _, kp := range keyedRows() {
		for _, m := range kp.provider.Catalog {
			if d := kp.provider.ForModel(m).Dialect; d == llm.DialectMessages || d == llm.DialectResponses {
				assert.True(t, m.Tools, m.ID)
			}
		}
	}
}

// DeepSeek's reasoner is the one entry of its wire held back: its documentation
// has said both that a round's reasoning_content must come back on the assistant
// message carrying the calls and that sending it back is refused, and this wire's
// walk sends no thinking either way. The flag waits on a run with a key.
func TestDeepSeeksReasonerTakesNoTools(t *testing.T) {
	p := newDeepSeekProvider()

	for _, m := range p.Catalog {
		assert.Equal(t, m.ID == "deepseek-chat", m.Tools, m.ID)
	}
}

// Every Claude model reads a million tokens.
func TestAnthropicCatalogStatesAContextWindow(t *testing.T) {
	for _, m := range newAnthropicProvider().Catalog {
		assert.Equal(t, 1_000_000, m.ContextWindow, m.ID)
	}
}

// Each Claude model defaults to the effort the API gives it when none is sent.
func TestAnthropicCatalogDefaultsToTheAPIsEffort(t *testing.T) {
	defaults := map[string]string{}
	for _, m := range newAnthropicProvider().Catalog {
		defaults[m.ID] = m.DefaultEffort
	}
	assert.Equal(t, map[string]string{
		"claude-fable-5-1":  "high",
		"claude-opus-5-5":   "medium",
		"claude-sonnet-5-5": "high",
		"claude-haiku-5-5":  "medium",
	}, defaults)
}

// Every entry states how its provider caches it, since the wire marks a prompt by
// that word alone. Every entry sent on Messages caches on marks, and so does Claude
// through OpenRouter; Groq documents no cache on its Qwen models; every other
// entry caches on its own.
func TestEveryModelStatesItsCaching(t *testing.T) {
	notAuto := map[string]llm.CacheKind{
		"openrouter/anthropic/claude-haiku-4.5": llm.CacheMarks,
		"groq/qwen/qwen3.8-27b":                 llm.CacheNone,
		"groq/qwen/qwen3.6-27b":                 llm.CacheNone,
	}
	providers := []llm.Provider{llm.FakeProvider(nil)}
	for _, kp := range keyedRows() {
		providers = append(providers, kp.provider)
	}
	for _, p := range providers {
		for _, m := range p.Catalog {
			want, ok := notAuto[p.ID+"/"+m.ID]
			switch {
			case p.ForModel(m).Dialect == llm.DialectMessages:
				want = llm.CacheMarks
			case !ok:
				want = llm.CacheAuto
			}
			assert.Equal(t, want, m.Cache, "%s/%s", p.ID, m.ID)
		}
	}
}

// A route puts the key in one place: a header or a body field, never both.
func TestEveryAffinityRouteNamesOnePlace(t *testing.T) {
	for _, kp := range keyedRows() {
		r := kp.provider.AffinityRoute
		assert.False(t, r.Header != "" && r.Field != "", kp.provider.ID)
	}
}

// Only the vendors that document a key routing a conversation to its cache get
// one, where each documents it. OpenAI's prompt_cache_key only separates cache
// accounting, so the openai row sends none.
func TestEachRowRoutesTheAffinityKeyWhereItsVendorSays(t *testing.T) {
	routes := map[string]llm.AffinityRoute{}
	for _, kp := range keyedRows() {
		if kp.provider.AffinityRoute != (llm.AffinityRoute{}) {
			routes[kp.provider.ID] = kp.provider.AffinityRoute
		}
	}
	assert.Equal(t, map[string]llm.AffinityRoute{
		"mistral":     {Field: "prompt_cache_key"},
		"xai":         {Header: "x-grok-conv-id"},
		"openrouter":  {Field: "session_id"},
		"fireworks":   {Header: "x-session-affinity"},
		"opencode-go": {Header: "x-opencode-session"},
	}, routes)
}

// A stated window holds the cap every request asks for, so no request is refused
// for its cap alone.
func TestEveryWrittenWindowHoldsTheCap(t *testing.T) {
	for _, kp := range keyedRows() {
		for _, m := range kp.provider.Catalog {
			if m.ContextWindow > 0 {
				assert.Greater(t, m.ContextWindow, m.MaxOutputTokens, m.ID)
			}
		}
	}
}

// The connector's key signs as a bearer, which is right only on the row's one
// dialect: no OpenRouter model states a dialect or base URL of its own.
func TestNoOpenRouterModelLeavesTheRowsDialect(t *testing.T) {
	for _, m := range newOpenRouterProvider().Catalog {
		assert.Empty(t, m.Dialect, m.ID)
		assert.Empty(t, m.BaseURL, m.ID)
	}
	assert.Empty(t, newOpenRouterProvider().KeyHeader)
}

// A catalog with no reader is the row's written one, read without a request,
// at the base a debug build moved. An id not in the table is an error.
func TestModelsAnswersTheWrittenCatalog(t *testing.T) {
	release, err := Models(nil, "")(t.Context(), llm.Provider{ID: "opencode-go"})
	require.NoError(t, err)
	assert.Equal(t, newOpenCodeGoProvider().Catalog, release, "a release keeps each model's base")

	moved, err := Models(map[string]string{"opencode-go": "http://127.0.0.1:1"}, "")(t.Context(), llm.Provider{ID: "opencode-go"})
	require.NoError(t, err)
	require.Len(t, moved, len(newOpenCodeGoProvider().Catalog))
	for _, m := range moved {
		assert.Empty(t, m.BaseURL, m.ID)
	}

	vertex, err := Models(nil, vertexBase)(t.Context(), llm.Provider{ID: GoogleID})
	require.NoError(t, err)
	assert.Equal(t, newGoogleProvider(vertexBase).Catalog, vertex)

	_, err = Models(nil, "")(t.Context(), llm.Provider{ID: "nope"})
	assert.ErrorContains(t, err, "nope is not a provider")
}

// Every row is a provider for the service, in picker order. A keyed row's routes
// are its bases and their headers: one in a release, and opencode-go's two. A
// row with no key has neither a key variable nor a route.
func TestProviders(t *testing.T) {
	rows := Providers(nil, vertexBase)
	require.Len(t, rows, len(rowsAt(nil, vertexBase)))
	for _, r := range rows {
		assert.Equal(t, KeyVars()[r.ID], r.KeyVar, r.ID)
		assert.NotEmpty(t, r.Label, r.ID)
		switch r.ID {
		case "opencode-go":
		case ChatGPTID, GoogleID:
			assert.Empty(t, r.Routes, r.ID)
		default:
			assert.Len(t, r.Routes, 1, r.ID)
		}
	}
	assert.Equal(t, []string{ChatGPTID, GoogleID}, []string{rows[len(rows)-2].ID, rows[len(rows)-1].ID})
	assert.Equal(t, []providercredentials.Route{{BaseURL: "https://api.anthropic.com", Header: "X-Api-Key"}}, rows[0].Routes)
	assert.Equal(t, []providercredentials.Route{{BaseURL: "https://api.openai.com/v1", Header: "Authorization", Prefix: "Bearer "}}, rows[1].Routes)
	assert.Equal(t, []providercredentials.Route{
		{BaseURL: "https://opencode.ai/inference/go/openai/v1", Header: "Authorization", Prefix: "Bearer "},
		{BaseURL: "https://opencode.ai/inference/go/anthropic", Header: "X-Api-Key"},
	}, rows[10].Routes)

	moved := Providers(map[string]string{"opencode-go": "http://127.0.0.1:1"}, "")
	assert.Equal(t, []providercredentials.Route{
		{BaseURL: "http://127.0.0.1:1", Header: "Authorization", Prefix: "Bearer "},
		{BaseURL: "http://127.0.0.1:1", Header: "X-Api-Key"},
	}, moved[10].Routes, "one stub takes both headers")
	assert.NotContains(t, rowIDs(rowsAt(nil, "")), GoogleID, "no project, no google provider")
}

// A release sets one header per request: no two routes of a provider with
// different headers have one base covering the other.
func TestEachBaseOfAKeyedRowTakesOneHeader(t *testing.T) {
	covers := func(base, u string) bool { return strings.HasPrefix(u+"/", strings.TrimSuffix(base, "/")+"/") }
	for _, r := range Providers(nil, "") {
		for _, a := range r.Routes {
			for _, b := range r.Routes {
				if a.Header != b.Header {
					assert.False(t, covers(a.BaseURL, b.BaseURL), "%s: %s covers %s", r.ID, a.BaseURL, b.BaseURL)
				}
			}
		}
	}
}
