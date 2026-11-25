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
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// stubMemories is an index a test sets, and a binding that refuses a forget with
// the cluster and chat it was bound to.
type stubMemories struct {
	mu      sync.Mutex
	section string
}

func (m *stubMemories) Section(context.Context, apimeta.ClusterID) json.RawMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return json.RawMessage(m.section)
}

func (m *stubMemories) set(section string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.section = section
}

func startServiceWithMemories(t *testing.T, cards ClusterCards, memories *stubMemories, offered ...tools.Tool) *service {
	t.Helper()
	dir := t.TempDir()
	box, lists := testReaders, noLists
	if len(offered) > 0 {
		box, lists = testBox(offered...)
	}
	s, err := newService(openTestDB(t, dir), chatsDirIn(dir), monitorDirIn(dir), fakeLLM(), cards, memories, box, lists, sandbox.State{}, testSettings(t), nil)
	require.NoError(t, err)
	startPrepared(t, s)
	return s
}

// The index rides the context block beside the card, so a change to either is
// resent the same way, and a question with nothing changed carries no block.
func TestAMemoryChangeResendsTheContext(t *testing.T) {
	t.Parallel()

	memories := &stubMemories{section: `{"today":"2026-09-23","memories":[]}`}
	s := startServiceWithMemories(t, &stubClusterCards{card: "## Cluster\n\n```json\n{}\n```"}, memories)

	first := sendAndSettle(t, s, nil, "1", "1", "one")
	context := newestContextOf(t, s, first.ChatID)
	assert.Contains(t, context, "## Cluster")
	assert.Contains(t, context, "## Memory")
	assert.Contains(t, context, `"memories":[]`)

	sendAndSettle(t, s, &first.ChatID, "1", "2", "two")
	assert.Equal(t, []llm.BlockType{llm.BlockText}, blockTypes(t, questions(t, s, first.ChatID)[1]), "nothing changed")

	memories.set(`{"today":"2026-09-23","memories":[{"name":"pages"}]}`)
	sendAndSettle(t, s, &first.ChatID, "1", "3", "three")
	assert.Equal(t, []llm.BlockType{llm.BlockContext, llm.BlockText}, blockTypes(t, questions(t, s, first.ChatID)[2]))
	assert.Contains(t, newestContextOf(t, s, first.ChatID), `"name":"pages"`)
}

func TestAnUnreadableMemoryStillSends(t *testing.T) {
	t.Parallel()

	s := startServiceWithMemories(t, &stubClusterCards{card: "## Cluster\n\n```json\n{}\n```"}, &stubMemories{section: `{"unavailable":true}`})

	msg := sendAndSettle(t, s, nil, "1", "1", "one")

	assert.Contains(t, newestContextOf(t, s, msg.ChatID), "## Memory\n\n```json\n{\"unavailable\":true}\n```")
}

// An index that is not one JSON value is sent as unavailable rather than failing
// the send or forging the block.
func TestAMalformedIndexIsSentAsUnavailable(t *testing.T) {
	t.Parallel()

	s := startServiceWithMemories(t, &stubClusterCards{card: "## Cluster\n\n```json\n{}\n```"}, &stubMemories{section: `{"a":1} {}`})

	msg := sendAndSettle(t, s, nil, "1", "1", "one")

	assert.Contains(t, newestContextOf(t, s, msg.ChatID), "## Memory\n\n```json\n{\"unavailable\":true}\n```")
}
