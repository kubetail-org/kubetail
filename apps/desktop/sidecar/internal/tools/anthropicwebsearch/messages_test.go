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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
)

// The names the binding reads are the SDK's own, so they are the ones its
// offer sends.
func TestTheBindingReadsTheSDKsNames(t *testing.T) {
	search := New(time.Now)

	assert.Equal(t, string(constant.WebSearch20260318("").Default()), string(search.ContractName()))
	assert.Equal(t, string(constant.WebSearch("").Default()), search.MessagesCallName())
	assert.Equal(t, string(constant.WebSearchToolResult("").Default()), search.MessagesResultType())
	assert.Equal(t, 3, search.MessagesUses(anthropic.ServerToolUsage{WebSearchRequests: 3}))
}

// The offer is capped and called by the model itself, never from a
// code-execution container.
func TestTheOfferIsCappedAndDirect(t *testing.T) {
	body, err := json.Marshal(New(time.Now).MessagesOffer(2))

	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"web_search_20260318","name":"web_search","max_uses":2,"allowed_callers":["direct"]}`, string(body))
}

// searchReply is a recorded Messages reply that searched once: the call, its
// result, and a usage counting the one search.
var searchReply = []string{
	`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5-20251001","content":[],"stop_reason":null,"usage":{"input_tokens":2,"output_tokens":1}}}`,
	`{"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"srv_1","name":"web_search","input":{}}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"kubernetes 1.36\"}"}}`,
	`{"type":"content_block_stop","index":0}`,
	`{"type":"content_block_start","index":1,"content_block":{"type":"web_search_tool_result","tool_use_id":"srv_1","content":[{"type":"web_search_result","url":"https://kubernetes.io/releases","title":"Releases","encrypted_content":"ZW5j"}]}}`,
	`{"type":"content_block_stop","index":1}`,
	`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":9,"server_tool_use":{"web_search_requests":1}}}`,
	`{"type":"message_stop"}`,
}

// On the Messages wire the search is offered in its shape after nothing else,
// its call is read under its name, and its count is kept under that name.
func TestTheSearchRidesTheMessagesWire(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		w.Header().Set("Content-Type", "text/event-stream")
		for _, data := range searchReply {
			var e struct{ Type string }
			assert.NoError(t, json.Unmarshal([]byte(data), &e))
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, data)
		}
	}))
	t.Cleanup(srv.Close)
	search := New(time.Now)
	target := llm.Target{
		Provider: llm.Provider{ID: "anthropic", Dialect: llm.DialectMessages, BaseURL: srv.URL, Key: "k"},
		Model:    llm.Model{ID: "claude-haiku-4-5-20251001", MaxOutputTokens: 1024, Tools: true},
	}
	msgs := []llm.Message{{Role: "user", Blocks: []llm.Block{llm.TextBlock("what shipped in 1.36?")}}}
	var chunks []llm.Chunk

	resp, err := target.Stream(t.Context(), "", msgs, "", nil, []llm.NativeOffer{{Tool: search, Server: true, MaxUses: 5}}, func(c llm.Chunk) {
		chunks = append(chunks, c)
	})

	require.NoError(t, err)
	assert.Equal(t, []any{map[string]any{
		"type": "web_search_20260318", "name": "web_search", "max_uses": float64(5), "allowed_callers": []any{"direct"},
	}}, body["tools"])
	call := llm.ServerUseBlock("srv_1", search.Name(), json.RawMessage(`{"query":"kubernetes 1.36"}`))
	assert.Equal(t, []llm.Chunk{{Kind: llm.ChunkServer, Call: call}}, chunks)
	assert.Equal(t, map[string]int{search.Name(): 1}, resp.ServerUses)
}
