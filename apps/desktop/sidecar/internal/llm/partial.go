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

// Partial is a reply in flight as its chunks build it. The agent and the fake
// both fold through it, so a live row, a row that broke off and the fake's reply
// split text at a server call the same way. The zero value is empty.
type Partial struct {
	thinking string
	blocks   []Block
}

// Add folds one chunk in. Thinking grows one thinking block, which reads first
// whenever it arrived; text grows the last block when that is text, else starts
// one; a server call appends its call.
func (p *Partial) Add(c Chunk) {
	switch c.Kind {
	case ChunkThinking:
		p.thinking += c.Text
	case ChunkServer:
		p.blocks = append(p.blocks, c.Call)
	default:
		if c.Text == "" {
			return
		}
		if n := len(p.blocks); n > 0 && p.blocks[n-1].Type == BlockText {
			p.blocks[n-1].Text += c.Text
			return
		}
		p.blocks = append(p.blocks, TextBlock(c.Text))
	}
}

// Blocks is the reply so far, a copy the caller may keep.
func (p *Partial) Blocks() []Block {
	var out []Block
	if p.thinking != "" {
		out = append(out, ThinkingBlock(p.thinking))
	}
	return append(out, p.blocks...)
}
