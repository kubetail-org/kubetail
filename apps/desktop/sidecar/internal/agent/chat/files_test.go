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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// stampTool stamps /etc/hosts on the chat its turn runs in.
type stampTool struct{ testTool }

func (stampTool) Run(_ context.Context, rt tools.Runtime, _ json.RawMessage) (string, bool) {
	rt.Files.SetStamp("/etc/hosts", tools.Stamp{Sum: [32]byte{1}, Whole: true})
	return "ok", false
}

// A turn's tools stamp the files of their own chat; another chat never sees the
// stamp, and it goes when its chat is deleted.
func TestAChatsStampsGoWithIt(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, stampTool{testTool: testTool{name: "stamp"}})
	fakeOf(s).SetToolCalls(llm.StagedCall("stamp", `{}`))
	a := send(t, s, nil, "1", "hi")
	awaitSettled(t, s, a.ChatID, a.ID)
	b := send(t, s, nil, "2", "hi")
	awaitSettled(t, s, b.ChatID, b.ID)

	got, ok := s.chatFiles(a.ChatID).Stamp("/etc/hosts")
	require.True(t, ok, "the turn's tool stamped its chat")
	assert.Equal(t, tools.Stamp{Sum: [32]byte{1}, Whole: true}, got)
	_, ok = s.chatFiles(b.ChatID).Stamp("/etc/hosts")
	assert.False(t, ok, "another chat never sees it")

	require.NoError(t, s.Delete(t.Context(), a.ChatID))
	_, ok = s.chatFiles(a.ChatID).Stamp("/etc/hosts")
	assert.False(t, ok, "the stamp went with its chat")
}
