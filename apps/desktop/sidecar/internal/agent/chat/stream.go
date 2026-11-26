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
	"sync/atomic"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/deltafold"
)

// The collection watch frame type is shared vocabulary (internal/lib/apimeta), aliased here so
// this package's watches keep speaking their own name. The same four the cluster
// watches use.
type CollectionWatchFrameType = apimeta.CollectionWatchFrameType

const (
	CollectionWatchFrameAdded    = apimeta.CollectionWatchFrameAdded
	CollectionWatchFrameModified = apimeta.CollectionWatchFrameModified
	CollectionWatchFrameDeleted  = apimeta.CollectionWatchFrameDeleted
	CollectionWatchFrameBookmark = apimeta.CollectionWatchFrameBookmark
)

// ChatCollectionWatchFrame is one frame on the list watch. Chat is nil on the Bookmark.
type ChatCollectionWatchFrame struct {
	Type CollectionWatchFrameType
	Chat *Chat
}

// ChatMessageCollectionWatchFrame is one frame on a chat's transcript watch. Message is nil
// on the Bookmark.
type ChatMessageCollectionWatchFrame struct {
	Type    CollectionWatchFrameType
	Message *ChatMessage
}

// Stream is a watch: its frames, and why they stopped. Frames closes on every exit,
// so Err is what tells a failure from an ordinary teardown. The same shape as
// cluster.Stream.
type Stream[T any] struct {
	Frames <-chan T
	err    atomic.Pointer[error]
}

// Err returns why the stream ended, or nil if it ended cleanly. Read it once Frames
// has closed; before that it is always nil.
func (s *Stream[T]) Err() error {
	if p := s.err.Load(); p != nil {
		return *p
	}
	return nil
}

// newStream runs pump on a goroutine in the service's WaitGroup, under a context
// that ends with the consumer's or the service's, whichever comes first — closing
// the hub ends a pump waiting on its receiver, but not one blocked sending to a
// consumer that stopped reading. What pump returns is the stream's terminal error,
// recorded before Frames closes; a cancellation ends the stream cleanly.
func newStream[T any](s *service, ctx context.Context, pump func(context.Context, chan<- T) error) *Stream[T] {
	ch := make(chan T, 1)
	st := &Stream[T]{Frames: ch}
	if s.enter() != nil {
		close(ch) // stopping: an already-ended stream, which every watcher is about to see
		return st
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	go func() {
		defer s.wg.Done()
		defer close(ch)
		defer stop()
		defer cancel()
		if err := pump(ctx, ch); err != nil && ctx.Err() == nil {
			st.err.Store(&err)
		}
	}()
	return st
}

// One fold per watch. Chat and ChatMessage are comparable, so a change is !=.
type (
	chatFold    = deltafold.Fold[ChatID, Chat, ChatCollectionWatchFrame]
	messageFold = deltafold.Fold[MessageID, ChatMessage, ChatMessageCollectionWatchFrame]
)

func newChatFold() *chatFold {
	return deltafold.New(chatKey, deltafold.Equal[Chat], chatFrame)
}

func newMessageFold() *messageFold {
	return deltafold.New(messageKey, deltafold.Equal[ChatMessage], messageFrame)
}

func chatKey(c Chat) ChatID { return c.ID }

func chatFrame(t CollectionWatchFrameType, c Chat) ChatCollectionWatchFrame {
	if t == CollectionWatchFrameBookmark {
		return ChatCollectionWatchFrame{Type: t}
	}
	return ChatCollectionWatchFrame{Type: t, Chat: &c}
}

func messageKey(m ChatMessage) MessageID { return m.ID }

func messageFrame(t CollectionWatchFrameType, m ChatMessage) ChatMessageCollectionWatchFrame {
	if t == CollectionWatchFrameBookmark {
		return ChatMessageCollectionWatchFrame{Type: t}
	}
	return ChatMessageCollectionWatchFrame{Type: t, Message: &m}
}
