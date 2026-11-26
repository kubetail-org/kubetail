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

package llm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The definitions reach the request in offer order.
func TestToolDefinitionsReachTheRequestInOrder(t *testing.T) {
	ours := ToolDefinition{Name: "echo", Description: "says it back", InputSchema: json.RawMessage(`{"type":"object"}`)}
	f := NewFake(0)
	second := ToolDefinition{Name: "count", InputSchema: json.RawMessage(`{"type":"object"}`)}
	target := Target{Provider: FakeProvider(f), Model: Model{ID: "m", Tools: true}}
	_, err := target.Stream(t.Context(), "", nil, "", []ToolDefinition{ours, second}, nil, func(Chunk) {})

	require.NoError(t, err)
	assert.Equal(t, []ToolDefinition{ours, second}, f.LastRequest().Tools, "the offer reaches the request in order")
}
