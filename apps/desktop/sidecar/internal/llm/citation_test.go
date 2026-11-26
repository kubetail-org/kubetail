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

// Each dialect reads its own shape: the fake reads what it wrote, and the wires
// that record no citation yet read none, even from a row another dialect wrote.
func TestCitationsReadsTheDialectsShape(t *testing.T) {
	f := NewFake(0)
	f.SetReply(Chunk{Text: "It shipped."})
	f.SetCitations(releases)
	resp, err := f.Stream(t.Context(), Request{}, func(Chunk) {})
	assert.NoError(t, err)

	assert.Equal(t, []Citation{releases}, Citations(DialectFake, resp.Blocks))
	assert.Empty(t, Citations(DialectResponses, []Block{citedText("It shipped.")}))
	assert.Empty(t, Citations(DialectChatCompletions, []Block{citedText("It shipped.")}))
	assert.Empty(t, Citations(Dialect("smoke-signals"), []Block{citedText("It shipped.")}))
}
