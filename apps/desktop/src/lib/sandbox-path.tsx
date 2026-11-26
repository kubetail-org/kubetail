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

// The PATH of every sandboxed command: one string the user writes, where
// $PATH stands for the login shell's, and Refresh PATH, which reads the shell
// again. The one reader of the query and the mutations. After each change the
// query is asked again, since it alone carries the fault beside the setting.
// Another window can change the setting and each window caches its own, so it
// is also asked again on opening and whenever the window takes focus.
import { useCallback, useEffect, useState } from 'react';
import { useMutation, useQuery } from 'urql';

import { graphql } from '@/gql';
import type { SandboxPathGetQuery } from '@/gql/graphql';
import { refusalOf, savedUnread } from '@/lib/graphql/refusal';

const SandboxPathGetDocument = graphql(`
  query SandboxPathGet {
    sandboxPathGet {
      value
      refusal
      folders {
        path
        origin
        target
        searched
        reason
        addable
        shared
      }
    }
    sandboxPathFaultGet
  }
`);

const SandboxPathSetMutation = graphql(`
  mutation SandboxPathSet($value: String!) {
    sandboxPathSet(value: $value) {
      value
    }
  }
`);

const SandboxPathRefreshMutation = graphql(`
  mutation SandboxPathRefresh {
    sandboxPathRefresh {
      value
    }
  }
`);

export type SandboxPathFolder = SandboxPathGetQuery['sandboxPathGet']['folders'][number];

export type SandboxPath = {
  /** The setting as written; undefined until the sidecar has answered. */
  value: string | undefined;
  /** Why Kubetail cannot use the value; null when it can. */
  refusal: string | null | undefined;
  /** Each folder of the value, as a sandboxed command judges it now. */
  folders: SandboxPathFolder[] | undefined;
  /** Why the last read of the login shell failed, null once one answers. */
  fault: string | null | undefined;
  saving: boolean;
  refreshing: boolean;
  /** The last save's message: its refusal, or that it was taken but not read back. */
  saveError: string | null;
  /** The last refused refresh's message. */
  refreshError: string | null;
  /** Saves value; true once it was taken. */
  save: (value: string) => Promise<boolean>;
  /** True once the shell answered and its PATH was stored. */
  refresh: () => Promise<boolean>;
};

export function useSandboxPath(): SandboxPath {
  const [{ data }, reexecute] = useQuery({ query: SandboxPathGetDocument, requestPolicy: 'cache-and-network' });
  const [, saveMutation] = useMutation(SandboxPathSetMutation);
  const [, refreshMutation] = useMutation(SandboxPathRefreshMutation);
  const [saving, setSaving] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [refreshError, setRefreshError] = useState<string | null>(null);

  const askAgain = useCallback(() => reexecute({ requestPolicy: 'network-only' }), [reexecute]);
  useEffect(() => {
    window.addEventListener('focus', askAgain);
    return () => window.removeEventListener('focus', askAgain);
  }, [askAgain]);

  const save = useCallback(
    async (value: string) => {
      setSaving(true);
      setSaveError(null);
      const { error } = await saveMutation({ value });
      setSaving(false);
      setSaveError(refusalOf(error));
      askAgain();
      return !error || savedUnread(error);
    },
    [saveMutation, askAgain],
  );

  const refresh = useCallback(async () => {
    setRefreshing(true);
    const { error } = await refreshMutation({});
    setRefreshing(false);
    setRefreshError(refusalOf(error));
    askAgain();
    return !error || savedUnread(error);
  }, [refreshMutation, askAgain]);

  return {
    value: data?.sandboxPathGet.value,
    refusal: data?.sandboxPathGet.refusal,
    folders: data?.sandboxPathGet.folders,
    fault: data?.sandboxPathFaultGet,
    saving,
    refreshing,
    saveError,
    refreshError,
    save,
    refresh,
  };
}
