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

// A chat that has not started. Nothing is written until the first send, which
// creates the chat and moves the window to its route — so a stray visit leaves no
// empty row in the sidebar.
import { createRoute, useNavigate } from '@tanstack/react-router';

import { CenteredColumn } from '@/components/chat/centered-column';
import { NewChatPane } from '@/components/chat/chat-pane';
import { Route as appRoute } from '@/routes/_app';

export const Route = createRoute({
  getParentRoute: () => appRoute,
  path: '/chat',
  component: Chat,
});

function Chat() {
  const navigate = useNavigate();

  return (
    <CenteredColumn>
      <NewChatPane
        mode="chat"
        centered
        empty={<h1 className="text-center text-2xl">Ask about your cluster.</h1>}
        // Replace, so Back returns to whatever came before the empty page.
        onCreated={(chatId) => navigate({ to: '/chat/$chatId', params: { chatId }, replace: true })}
      />
    </CenteredColumn>
  );
}
