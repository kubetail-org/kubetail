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

// A chat's own rules: the ones an approval answered "Allow for this
// chat" wrote, which the composer lists and removes. The one reader of the
// query and the mutation. Another window can add or remove one, so the query
// is asked again on focus and after a removal; an approval in this window
// names the rule's type (`chatGrantsContext`), which asks it again too.
import { useCallback, useEffect, useState } from 'react';
import { useMutation, useQuery } from 'urql';

import { graphql } from '@/gql';
import type { ChatPermissionRulesGetQuery } from '@/gql/graphql';

const ChatPermissionRulesGetDocument = graphql(`
  query ChatPermissionRulesGet($chatID: ChatID!) {
    chatPermissionRulesGet(chatID: $chatID) {
      id
      line
    }
  }
`);

const ChatPermissionRuleRemoveMutation = graphql(`
  mutation ChatPermissionRuleRemove($chatID: ChatID!, $id: String!) {
    chatPermissionRuleRemove(chatID: $chatID, id: $id) {
      id
    }
  }
`);

/**
 * The operation context that ties the chat's rules to urql's cache: the query
 * carries it, and so does a mutation that writes a rule, so the list is asked
 * again. A list holding no rule carries no PermissionRule for the cache to key on,
 * so the type is named.
 */
export const chatGrantsContext = { additionalTypenames: ['PermissionRule'] };

export type ChatGrant = ChatPermissionRulesGetQuery['chatPermissionRulesGet'][number];

export type ChatGrants = {
  /** Undefined until the sidecar has answered. */
  rules: ChatGrant[] | undefined;
  /** The rule whose removal is in flight. */
  removing: string | null;
  /** The last refused removal's reason, until the next one starts. */
  error: string | null;
  remove: (id: string) => Promise<void>;
};

export function useChatGrants(chatID: string): ChatGrants {
  const [{ data }, reexecute] = useQuery({
    query: ChatPermissionRulesGetDocument,
    variables: { chatID },
    context: chatGrantsContext,
    requestPolicy: 'cache-and-network',
  });
  const [, removeMutation] = useMutation(ChatPermissionRuleRemoveMutation);
  const [removing, setRemoving] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const askAgain = useCallback(() => reexecute({ requestPolicy: 'network-only' }), [reexecute]);
  useEffect(() => {
    window.addEventListener('focus', askAgain);
    return () => window.removeEventListener('focus', askAgain);
  }, [askAgain]);

  const remove = useCallback(
    async (id: string) => {
      setRemoving(id);
      setError(null);
      const result = await removeMutation({ chatID, id });
      setRemoving(null);
      if (result.error) setError(result.error.graphQLErrors[0]?.message ?? result.error.message);
      askAgain();
    },
    [askAgain, chatID, removeMutation],
  );

  return { rules: data?.chatPermissionRulesGet, removing, error, remove };
}
