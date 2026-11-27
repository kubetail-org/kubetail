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

// The sandbox switch a new chat starts with: the one reader of its state watch
// and its mutations. A chat copies the default when it is created, and a send
// that creates one names the default it saw, so a new chat's Send waits while
// a change is in flight. A send that saw a stale default is still refused,
// which covers a change made in another window.
import { useState } from 'react';
import { useMutation } from 'urql';

import { graphql } from '@/gql';
import type { SandboxDefaultStateWatchSubscription } from '@/gql/graphql';
import { usePendingSwitch, useSwitchingAny } from '@/lib/chat-switch';
import { stateWatchReducer } from '@/lib/graphql/state-watch';
import { useFoldedSubscription } from '@/lib/graphql/use-folded-subscription';

const SandboxDefaultStateWatchDocument = graphql(`
  subscription SandboxDefaultStateWatch($clusterID: ClusterID) {
    sandboxDefaultStateWatch(clusterID: $clusterID) {
      sandboxDisabled
      clusterOverride
    }
  }
`);

const SandboxDefaultSetMutation = graphql(`
  mutation SandboxDefaultSet($clusterID: ClusterID, $sandboxDisabled: Boolean!) {
    sandboxDefaultSet(clusterID: $clusterID, sandboxDisabled: $sandboxDisabled) {
      sandboxDisabled
      clusterOverride
    }
  }
`);

const SandboxDefaultResetMutation = graphql(`
  mutation SandboxDefaultReset($clusterID: ClusterID!, $sandboxDisabled: Boolean!) {
    sandboxDefaultReset(clusterID: $clusterID, sandboxDisabled: $sandboxDisabled) {
      sandboxDisabled
      clusterOverride
    }
  }
`);

export type SandboxDefault = SandboxDefaultStateWatchSubscription['sandboxDefaultStateWatch'];

// The in-flight key of a scope's change: null for every cluster.
const switchKey = (clusterID: string | null) => `sandbox-default:${clusterID ?? ''}`;

/**
 * The default for `clusterID`'s new chats, or every cluster's with null.
 * Undefined until the watch answers, and while there is no cluster.
 */
export function useSandboxDefault(clusterID: string | null | undefined): SandboxDefault | undefined {
  // Unshared: the composer and Settings each mount late into the other's stream.
  const { data } = useFoldedSubscription<SandboxDefaultStateWatchSubscription, SandboxDefault>(
    {
      query: SandboxDefaultStateWatchDocument,
      variables: { clusterID: clusterID ?? null },
      pause: clusterID === undefined,
    },
    stateWatchReducer((next) => next.sandboxDefaultStateWatch),
    { unshared: true },
  );
  return data;
}

export type SandboxDefaultSwitching = {
  /** Whether a change of this scope's default is in flight. */
  switching: boolean;
  setSandboxDisabled: (disabled: boolean) => void;
  /**
   * Takes a cluster back to every cluster's default, `everyCluster` as the
   * caller saw it. The sidecar refuses a reset that would land elsewhere, since
   * that value decided whether the reset confirmed.
   */
  reset: (everyCluster: boolean) => void;
  /** Whether the last reset was refused because every cluster's default changed under it. */
  everyClusterChanged: boolean;
};

/** One scope's default: null for every cluster. */
export function useSandboxDefaultSwitch(clusterID: string | null): SandboxDefaultSwitching {
  const [, set] = useMutation(SandboxDefaultSetMutation);
  const [, reset] = useMutation(SandboxDefaultResetMutation);
  const { pending, run } = usePendingSwitch(switchKey(clusterID));
  const [everyClusterChanged, setEveryClusterChanged] = useState(false);
  return {
    switching: pending,
    setSandboxDisabled: (disabled) => {
      setEveryClusterChanged(false);
      run(() => set({ clusterID, sandboxDisabled: disabled }));
    },
    // Every cluster's default has nothing to go back to.
    reset: (everyCluster) => {
      if (!clusterID) return;
      setEveryClusterChanged(false);
      run(async () => {
        const result = await reset({ clusterID, sandboxDisabled: everyCluster });
        setEveryClusterChanged(result.error?.graphQLErrors[0]?.extensions?.code === 'KUBETAIL_CONFLICT');
      });
    },
    everyClusterChanged,
  };
}

/**
 * Whether a change that can move `clusterID`'s default is in flight, from
 * whichever window drew it: its own, or every cluster's.
 */
export function useSandboxDefaultChanging(clusterID: string | undefined): boolean {
  return useSwitchingAny(clusterID ? [switchKey(null), switchKey(clusterID)] : [switchKey(null)]);
}
