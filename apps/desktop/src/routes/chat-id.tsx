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

// One chat on a page of its own.
import { createRoute, useNavigate } from '@tanstack/react-router';

import { ChatPane } from '@/components/chat/chat-pane';
import { Route as appRoute } from '@/routes/_app';

export const Route = createRoute({
  getParentRoute: () => appRoute,
  path: '/chat/$chatId',
  component: ChatById,
});

function ChatById() {
  const { chatId } = Route.useParams();
  const navigate = useNavigate();

  return <ChatPane chatID={chatId} mode="chat" onGone={() => navigate({ to: '/chat' })} />;
}
