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
	"testing"

	"github.com/stretchr/testify/assert"
)

// A server call splits the text around it, and the thinking reads first
// whenever it arrived.
func TestPartialSplitsTextAtAServerCall(t *testing.T) {
	var p Partial
	for _, c := range []Chunk{
		{Text: "Let me "},
		{Text: "search."},
		{Kind: ChunkThinking, Text: "I should look it up."},
		{Kind: ChunkServer, Call: withoutPayload(searchUse("srv_1", "kubernetes 1.36"))},
		{Text: "It shipped."},
	} {
		p.Add(c)
	}

	assert.Equal(t, []Block{
		ThinkingBlock("I should look it up."),
		TextBlock("Let me search."),
		withoutPayload(searchUse("srv_1", "kubernetes 1.36")),
		TextBlock("It shipped."),
	}, p.Blocks())
}

// What Blocks hands out is a copy: a later chunk does not reach it.
func TestPartialBlocksIsACopy(t *testing.T) {
	var p Partial
	p.Add(Chunk{Text: "One"})
	first := p.Blocks()

	p.Add(Chunk{Text: " two"})

	assert.Equal(t, []Block{TextBlock("One")}, first)
	assert.Equal(t, []Block{TextBlock("One two")}, p.Blocks())
}

// A reply that has said nothing holds no block, not an empty one.
func TestPartialHoldsNothingBeforeItsFirstText(t *testing.T) {
	var p Partial
	assert.Empty(t, p.Blocks())

	p.Add(Chunk{Text: ""})
	p.Add(Chunk{Kind: ChunkThinking, Text: ""})

	assert.Empty(t, p.Blocks())
}
