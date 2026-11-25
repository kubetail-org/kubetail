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

// Chat in the dashboard's right sidebar. Which chat it is on is window state, so it
// rides in the `chat` search param (docs/adr/2026-08-09-url-params-as-window-state.md):
// every window's panel can be on a different one, and a reload lands back on it.
//
// Two states, one scroller each: the recent list over an unstarted chat, or the open
// chat with a way back to the list. The composer is always there, since typing is
// how a chat starts here.
import { Button } from '@kubetail/ui/elements/button';
import { useNavigate, useSearch } from '@tanstack/react-router';
import { useRef } from 'react';

import { ChatNav } from '@/components/chat/chat-nav';
import { ChatPane, NewChatPane } from '@/components/chat/chat-pane';

export function DashboardChat() {
  const { chat } = useSearch({ strict: false });
  const navigate = useNavigate();
  const composerInput = useRef<HTMLTextAreaElement>(null);
  const select = (next: string | undefined, replace = false) =>
    navigate({ to: '.', search: (prev) => ({ ...prev, chat: next }), replace });

  if (!chat) {
    return (
      <NewChatPane
        mode="dashboard"
        empty={
          <div className="min-h-0 flex-1 overflow-y-auto p-2">
            <ChatNav mode="dashboard" focusFallback={composerInput} />
          </div>
        }
        // Replace, so Back returns to whatever came before the list.
        onCreated={(id) => select(id, true)}
        inputRef={composerInput}
      />
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 p-2">
        <Button type="button" variant="ghost" size="sm" onClick={() => select(undefined)}>
          Chats
        </Button>
      </div>
      <ChatPane chatID={chat} mode="dashboard" onGone={() => select(undefined)} />
    </div>
  );
}
