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

// The tools a chat can be offered on this machine and the user's setting of
// each: the one reader of the state watch and the mutation, so
// a change made in another window arrives here too.
import { useCallback, useState } from 'react';
import { useMutation } from 'urql';

import { graphql } from '@/gql';
import type { ToolSettingsStateWatchSubscription } from '@/gql/graphql';
import { refusalOf } from '@/lib/graphql/refusal';
import { stateWatchReducer } from '@/lib/graphql/state-watch';
import { useFoldedSubscription } from '@/lib/graphql/use-folded-subscription';

const ToolSettingsStateWatchDocument = graphql(`
  subscription ToolSettingsStateWatch {
    toolSettingsStateWatch {
      name
      label
      group
      description
      enabled
      alwaysOn
    }
  }
`);

const ToolSettingSetMutation = graphql(`
  mutation ToolSettingSet($name: String!, $enabled: Boolean!) {
    toolSettingSet(name: $name, enabled: $enabled) {
      name
      enabled
    }
  }
`);

export type ToolSetting = ToolSettingsStateWatchSubscription['toolSettingsStateWatch'][number];

export type ToolSettings = {
  /** Undefined until the watch's first frame lands. */
  tools: ToolSetting[] | undefined;
  /** The tools whose setting is in flight, by name. */
  changing: ReadonlySet<string>;
  /** The last refused change's message, until the next one starts. */
  error: string | null;
  set: (name: string, enabled: boolean) => Promise<void>;
};

export function useToolSettings(): ToolSettings {
  // Unshared: the dialog mounts late into any other window's stream.
  const { data } = useFoldedSubscription<ToolSettingsStateWatchSubscription, ToolSetting[]>(
    { query: ToolSettingsStateWatchDocument },
    stateWatchReducer((next) => next.toolSettingsStateWatch),
    { unshared: true },
  );
  const [, setMutation] = useMutation(ToolSettingSetMutation);
  const [changing, setChanging] = useState<ReadonlySet<string>>(new Set());
  const [error, setError] = useState<string | null>(null);

  const set = useCallback(
    async (name: string, enabled: boolean) => {
      setChanging((prev) => new Set(prev).add(name));
      setError(null);
      const { error: refused } = await setMutation({ name, enabled });
      setChanging((prev) => {
        const next = new Set(prev);
        next.delete(name);
        return next;
      });
      if (refused) setError(refusalOf(refused));
    },
    [setMutation],
  );

  return { tools: data, changing, error, set };
}
