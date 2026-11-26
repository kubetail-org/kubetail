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

// One scope's variables, the ones the user gives their commands, and the user's
// two changes: set and remove. The one reader of the query and the mutations.
// After each change the query is asked again; another window can change the
// list, so it is also asked again on focus.
import { useCallback, useEffect, useState } from 'react';
import { useMutation, useQuery } from 'urql';

import { graphql } from '@/gql';
import type { EnvVarsGetQuery, EnvVarTarget } from '@/gql/graphql';
import { refusalOf } from '@/lib/graphql/refusal';

const EnvVarsGetDocument = graphql(`
  query EnvVarsGet($clusterID: ClusterID) {
    envVarsGet(clusterID: $clusterID) {
      name
      value
      target
      refusal
    }
  }
`);

const EnvVarSetMutation = graphql(`
  mutation EnvVarSet($clusterID: ClusterID, $name: String!, $value: String!, $target: EnvVarTarget!) {
    envVarSet(clusterID: $clusterID, name: $name, value: $value, target: $target) {
      name
    }
  }
`);

const EnvVarRemoveMutation = graphql(`
  mutation EnvVarRemove($clusterID: ClusterID, $name: String!) {
    envVarRemove(clusterID: $clusterID, name: $name) {
      name
    }
  }
`);

export type EnvVar = EnvVarsGetQuery['envVarsGet'][number];
export type { EnvVarTarget };

export type EnvVars = {
  /** In the order added; undefined until the sidecar has answered for this scope. */
  vars: EnvVar[] | undefined;
  /** Why the list could not be read, or null. */
  loadError: string | null;
  /** A set or a remove is in flight. */
  changing: boolean;
  /** This scope's last remove's refusal, until the next change starts. */
  removeError: string | null;
  /** Sets name to value for target's commands; answers the refusal, or null once it is taken. */
  set: (name: string, value: string, target: EnvVarTarget) => Promise<string | null>;
  remove: (name: string) => Promise<void>;
};

/** The variables of one cluster's chats, or with null every cluster's. */
export function useEnvVars(clusterID: string | null): EnvVars {
  const [{ data, error: loadError, operation }, reexecute] = useQuery({
    query: EnvVarsGetDocument,
    variables: { clusterID },
    requestPolicy: 'cache-and-network',
  });
  const [, setMutation] = useMutation(EnvVarSetMutation);
  const [, removeMutation] = useMutation(EnvVarRemoveMutation);
  const [changing, setChanging] = useState(false);
  const [removeError, setRemoveError] = useState<{ clusterID: string | null; text: string | null }>({
    clusterID,
    text: null,
  });
  // urql keeps the last scope's answer while it asks for the next, and a
  // change from what is drawn acts on this scope: another scope's list is none.
  const answered = (operation?.variables.clusterID ?? null) === clusterID;

  const askAgain = useCallback(() => reexecute({ requestPolicy: 'network-only' }), [reexecute]);
  useEffect(() => {
    window.addEventListener('focus', askAgain);
    return () => window.removeEventListener('focus', askAgain);
  }, [askAgain]);

  const set = useCallback(
    async (name: string, value: string, target: EnvVarTarget) => {
      setChanging(true);
      setRemoveError({ clusterID, text: null });
      const { error } = await setMutation({ clusterID, name, value, target });
      setChanging(false);
      askAgain();
      return refusalOf(error);
    },
    [clusterID, setMutation, askAgain],
  );

  const remove = useCallback(
    async (name: string) => {
      setChanging(true);
      setRemoveError({ clusterID, text: null });
      const { error } = await removeMutation({ clusterID, name });
      setChanging(false);
      setRemoveError({ clusterID, text: refusalOf(error) });
      askAgain();
    },
    [clusterID, removeMutation, askAgain],
  );

  return {
    vars: answered ? data?.envVarsGet : undefined,
    loadError: answered ? refusalOf(loadError) : null,
    changing,
    removeError: removeError.clusterID === clusterID ? removeError.text : null,
    set,
    remove,
  };
}
