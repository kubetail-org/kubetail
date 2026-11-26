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

// Whether this machine offers sandboxed Bash, and whether a sandboxed command can
// be given the internet. A query, not a watch: the answer is fixed for the
// sidecar's life, so it is asked once and never polled.
import { useCallback, useMemo } from 'react';
import { useQuery } from 'urql';

import { graphql } from '@/gql';

const SandboxStateGetDocument = graphql(`
  query SandboxStateGet {
    sandboxStateGet {
      available
      reason
      networkAvailable
      networkReason
    }
  }
`);

/**
 * `available` is undefined until the sidecar has answered, and after a failure:
 * a sidecar that could not be reached has not said there is no sandbox.
 * `reason` is what the probe found, or why there is none, in the sidecar's words.
 * `networkAvailable` is the same for the network, and `networkReason` why not.
 * `failed` is that failure, and `retry` the only way back, since a query re-runs
 * for nobody.
 */
export type Sandbox = {
  available: boolean | undefined;
  reason: string;
  networkAvailable: boolean | undefined;
  networkReason: string;
  failed: boolean;
  retry: () => void;
};

export function useSandbox(): Sandbox {
  const [{ data, error, fetching }, reexecute] = useQuery({ query: SandboxStateGetDocument });
  const available = data?.sandboxStateGet.available;
  const reason = data?.sandboxStateGet.reason ?? '';
  const networkAvailable = data?.sandboxStateGet.networkAvailable;
  const networkReason = data?.sandboxStateGet.networkReason ?? '';
  // Past the cache: what failed is what we are asking again for.
  const retry = useCallback(() => reexecute({ requestPolicy: 'network-only' }), [reexecute]);
  return useMemo(
    () => ({ available, reason, networkAvailable, networkReason, failed: !fetching && error !== undefined, retry }),
    [available, reason, networkAvailable, networkReason, error, fetching, retry],
  );
}
