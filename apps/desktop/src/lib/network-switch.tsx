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

// A chat's network switch: its mutation, held in flight by `useChatSwitch`.
import { useMutation } from 'urql';

import { graphql } from '@/gql';
import { useChatSwitch } from '@/lib/chat-switch';

const ChatNetworkEnabledSetMutation = graphql(`
  mutation ChatNetworkEnabledSet($id: ChatID!, $enabled: Boolean!) {
    chatNetworkEnabledSet(id: $id, enabled: $enabled) {
      id
      networkEnabled
    }
  }
`);

export type NetworkSwitching = {
  /** Whether a switch of the chat is in flight, or committed and not yet watched. */
  switching: boolean;
  setNetworkEnabled: (enabled: boolean) => void;
};

/** The chat's network switch, given the value the pane's list watch shows. */
export function useNetworkSwitch(chatID: string, watched: boolean | undefined): NetworkSwitching {
  const [, set] = useMutation(ChatNetworkEnabledSetMutation);
  const { switching, switchTo } = useChatSwitch(
    `network:${chatID}`,
    watched,
    async (enabled) => (await set({ id: chatID, enabled })).data?.chatNetworkEnabledSet.networkEnabled,
  );
  return { switching, setNetworkEnabled: switchTo };
}
