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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/clustercard"
)

// serviceWithClusterCards is a started service over the fake and cluster cards.
func serviceWithClusterCards(t *testing.T, clusterCards ClusterCards) *service {
	t.Helper()
	return startServiceWith(t, t.TempDir(), fakeLLM(), clusterCards, testReaders, noLists)
}

// sendAndSettle is one turn, run to its end so the next send is not refused. It
// sends the chat's switch as stored, as a sender who read the list would.
func sendAndSettle(t *testing.T, s *service, chatID *ChatID, clusterID apimeta.ClusterID, key, text string) ChatMessage {
	t.Helper()
	var disabled bool
	if chatID != nil {
		c, _, err := s.Get(t.Context(), *chatID)
		require.NoError(t, err)
		disabled = c.SandboxDisabled
	} else if s.sandboxState.Available {
		d, err := settingsStore(t, s.db).GetSandboxDefault(t.Context(), clusterID)
		require.NoError(t, err)
		disabled = d.Disabled
	}
	msg, err := s.Send(t.Context(), chatID, ModeChat, clusterID, SendSwitches{SandboxDisabled: disabled}, "fake", "fake", "high", reqID(key), text, nil)
	require.NoError(t, err)
	awaitSettled(t, s, msg.ChatID, msg.ID)
	return msg
}

// blockTypes is the shape of a stored message's content.
func blockTypes(t *testing.T, msg ChatMessage) []llm.BlockType {
	t.Helper()
	var types []llm.BlockType
	for _, b := range blocksOf(t, msg) {
		types = append(types, b.Type)
	}
	return types
}

// questions is the chat's user rows, in order.
func questions(t *testing.T, s *service, chatID ChatID) []ChatMessage {
	t.Helper()
	msgs, err := listMessages(t.Context(), s.store.Stmts(), chatID, testReaders)
	require.NoError(t, err)
	var out []ChatMessage
	for _, m := range msgs {
		if m.Role == RoleUser {
			out = append(out, m)
		}
	}
	return out
}

// newestContextOf is the context the model currently holds for the chat.
func newestContextOf(t *testing.T, s *service, chatID ChatID) string {
	t.Helper()
	card, err := newestContext(t.Context(), s.store.Stmts(), chatID)
	require.NoError(t, err)
	return card
}

func TestSendCarriesTheCardOnlyWhenItChanged(t *testing.T) {
	t.Parallel()

	clusterCards := &stubClusterCards{card: "card one"}
	s := serviceWithClusterCards(t, clusterCards)

	first := sendAndSettle(t, s, nil, "1", "1", "one")
	chatID := first.ChatID
	sendAndSettle(t, s, &chatID, "1", "2", "two")
	clusterCards.set("card two")
	sendAndSettle(t, s, &chatID, "1", "3", "three")

	q := questions(t, s, chatID)
	require.Len(t, q, 3)
	assert.Equal(t, []llm.BlockType{llm.BlockContext, llm.BlockText}, blockTypes(t, q[0]), "the first message always carries one")
	assert.Equal(t, []llm.BlockType{llm.BlockText}, blockTypes(t, q[1]), "an equal card is not resent")
	assert.Equal(t, []llm.BlockType{llm.BlockContext, llm.BlockText}, blockTypes(t, q[2]), "a changed card goes")
	assert.Contains(t, string(q[2].Content), "card two")
	assert.Equal(t, s.withWorkspace("card two", chatID), newestContextOf(t, s, chatID))
}

func TestSendSendsARunOfUnavailableCardsOnce(t *testing.T) {
	t.Parallel()

	clusterCards := &stubClusterCards{card: "healthy"}
	s := serviceWithClusterCards(t, clusterCards)

	first := sendAndSettle(t, s, nil, "1", "1", "one")
	chatID := first.ChatID
	clusterCards.set(clustercard.Unavailable)
	sendAndSettle(t, s, &chatID, "1", "2", "two")
	sendAndSettle(t, s, &chatID, "1", "3", "three")

	q := questions(t, s, chatID)
	require.Len(t, q, 3)
	assert.Equal(t, []llm.BlockType{llm.BlockContext, llm.BlockText}, blockTypes(t, q[1]))
	assert.Equal(t, []llm.BlockType{llm.BlockText}, blockTypes(t, q[2]))
}

// The withdrawal and the recovery are each a change: healthy, unavailable, healthy
// is three clusterCards.
func TestSendCarriesTheRecoveryAfterAnUnavailableCard(t *testing.T) {
	t.Parallel()

	clusterCards := &stubClusterCards{card: "healthy"}
	s := serviceWithClusterCards(t, clusterCards)

	first := sendAndSettle(t, s, nil, "1", "1", "one")
	chatID := first.ChatID
	clusterCards.set(clustercard.Unavailable)
	sendAndSettle(t, s, &chatID, "1", "2", "two")
	clusterCards.set("healthy")
	sendAndSettle(t, s, &chatID, "1", "3", "three")

	for i, q := range questions(t, s, chatID) {
		assert.Equal(t, []llm.BlockType{llm.BlockContext, llm.BlockText}, blockTypes(t, q), i)
	}
	assert.Equal(t, s.withWorkspace("healthy", chatID), newestContextOf(t, s, chatID))
}

// blockingClusterCards renders until its context ends, then answers unavailable,
// as clustercard does when its reads are cut short.
type blockingClusterCards struct{}

func (blockingClusterCards) ClusterCard(ctx context.Context, _ apimeta.ClusterID) string {
	<-ctx.Done()
	return clustercard.Unavailable
}

// The timeout is the render's alone: the question is accepted with the unavailable
// card, and its rows go in on the send's own context.
func TestACardThatTimesOutIsUnavailable(t *testing.T) {
	t.Parallel()

	s := serviceWithClusterCards(t, blockingClusterCards{})
	s.clusterCardTimeout = time.Millisecond

	msg := sendAndSettle(t, s, nil, "1", "1", "one")

	q := questions(t, s, msg.ChatID)
	require.Len(t, q, 1)
	assert.Equal(t, marshalBlocks([]llm.Block{llm.ContextBlock(s.withWorkspace(clustercard.Unavailable, msg.ChatID)), llm.TextBlock("one")}), q[0].Content)
}

// A repeat of an accepted request is answered by its key before anything is read:
// it asks the clusterCards nothing.
func TestAReplayRendersNoCard(t *testing.T) {
	t.Parallel()

	clusterCards := &stubClusterCards{card: "card"}
	s := serviceWithClusterCards(t, clusterCards)

	first := sendAndSettle(t, s, nil, "1", "1", "one")
	again, err := s.Send(t.Context(), nil, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("1"), "one", nil)
	require.NoError(t, err)

	assert.Equal(t, first.ID, again.ID)
	assert.Equal(t, []apimeta.ClusterID{"1"}, clusterCards.askedFor())
}

func TestNewestCardIsReadOffTheRecord(t *testing.T) {
	t.Parallel()

	s := serviceWithClusterCards(t, &stubClusterCards{card: "card"})
	first := sendAndSettle(t, s, nil, "1", "1", "one")

	assert.Equal(t, s.withWorkspace("card", first.ChatID), newestContextOf(t, s, first.ChatID))
	assert.Equal(t, "", newestContextOf(t, s, "no-such-chat"), "a chat with no card has none")
}

func TestSendRendersTheCardForTheChatsStoredCluster(t *testing.T) {
	t.Parallel()

	clusterCards := &stubClusterCards{card: "card"}
	s := serviceWithClusterCards(t, clusterCards)

	first := sendAndSettle(t, s, nil, "1", "1", "one")
	chatID := first.ChatID
	sendAndSettle(t, s, &chatID, "2", "2", "two")

	assert.Equal(t, []apimeta.ClusterID{"1", "1"}, clusterCards.askedFor(), "the send's argument is ignored for an existing chat")
}

func TestSendIsAcceptedWhenTheCardIsUnavailable(t *testing.T) {
	t.Parallel()

	s := serviceWithClusterCards(t, &stubClusterCards{card: clustercard.Unavailable})

	msg := sendAndSettle(t, s, nil, "1", "1", "one")

	assert.Contains(t, string(questions(t, s, msg.ChatID)[0].Content), "unavailable")
}

func TestSendRefusedWhileATurnRunsWritesNoCard(t *testing.T) {
	t.Parallel()

	clusterCards := &stubClusterCards{card: "card one"}
	s := serviceWithClusterCards(t, clusterCards)
	gate := make(chan struct{})
	fakeOf(s).SetGate(gate)
	t.Cleanup(func() { close(gate) })

	first := send(t, s, nil, "1", "one")
	clusterCards.set("card two")
	_, err := s.Send(t.Context(), &first.ChatID, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("2"), "two", nil)
	require.ErrorIs(t, err, ErrTurnInFlight)

	assert.Equal(t, s.withWorkspace("card one", first.ChatID), newestContextOf(t, s, first.ChatID))
}

// The card reaches the wire where it was stored: first on the question that
// carried it, and on no other.
func TestTheWireReceivesTheCardAheadOfTheText(t *testing.T) {
	t.Parallel()

	s := serviceWithClusterCards(t, &stubClusterCards{card: "the card"})

	first := sendAndSettle(t, s, nil, "1", "1", "one")
	chatID := first.ChatID
	sendAndSettle(t, s, &chatID, "1", "2", "two")

	msgs := fakeOf(s).LastRequest().Messages
	require.Len(t, msgs, 3)
	assert.Equal(t, []llm.Block{llm.ContextBlock(s.withWorkspace("the card", chatID)), llm.TextBlock("one")}, msgs[0].Blocks)
	assert.Equal(t, []llm.Block{llm.TextBlock("two")}, msgs[2].Blocks, "the turn that did not change it carries none")
}

// deletingClusterCards deletes the chat while its card renders: the window between
// the send's read of the chat and its transaction.
type deletingClusterCards struct {
	s      *service
	chatID ChatID
}

func (c *deletingClusterCards) ClusterCard(ctx context.Context, _ apimeta.ClusterID) string {
	if c.chatID != "" {
		if err := c.s.Delete(ctx, c.chatID); err != nil {
			panic(err)
		}
	}
	return "card"
}

func TestSendIntoAChatDeletedWhileItsCardRendersIsGone(t *testing.T) {
	t.Parallel()

	clusterCards := &deletingClusterCards{}
	s := serviceWithClusterCards(t, clusterCards)
	clusterCards.s = s

	first := sendAndSettle(t, s, nil, "1", "1", "one")
	clusterCards.chatID = first.ChatID
	_, err := s.Send(t.Context(), &first.ChatID, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("2"), "two", nil)

	require.ErrorIs(t, err, ErrChatGone)
	chats, err := s.List(t.Context())
	require.NoError(t, err)
	assert.Empty(t, chats, "no chat was created in its place")
}
