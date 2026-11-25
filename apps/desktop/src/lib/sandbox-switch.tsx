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

// A chat's sandbox switch: its mutation, held in flight by `useChatSwitch`.
import { useMutation } from 'urql';

import { graphql } from '@/gql';
import { useChatSwitch } from '@/lib/chat-switch';

const ChatSandboxDisabledSetMutation = graphql(`
  mutation ChatSandboxDisabledSet($id: ChatID!, $sandboxDisabled: Boolean!) {
    chatSandboxDisabledSet(id: $id, sandboxDisabled: $sandboxDisabled) {
      id
      sandboxDisabled
    }
  }
`);

export type SandboxSwitching = {
  /** Whether a switch of the chat is in flight, or committed and not yet watched. */
  switching: boolean;
  setSandboxDisabled: (disabled: boolean) => void;
};

/** The chat's sandbox switch, given the value the pane's list watch shows. */
export function useSandboxSwitch(chatID: string, watched: boolean | undefined): SandboxSwitching {
  const [, set] = useMutation(ChatSandboxDisabledSetMutation);
  const { switching, switchTo } = useChatSwitch(
    `sandbox:${chatID}`,
    watched,
    async (disabled) =>
      (await set({ id: chatID, sandboxDisabled: disabled })).data?.chatSandboxDisabledSet.sandboxDisabled,
  );
  return { switching, setSandboxDisabled: switchTo };
}
