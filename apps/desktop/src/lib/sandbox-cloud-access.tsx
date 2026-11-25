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

// The cloud platforms sandboxed commands may reach as the user, read-only.
import { useMemo } from 'react';
import { useQuery } from 'urql';

import { graphql } from '@/gql';
import type { PermissionTarget } from '@/gql/graphql';

const SandboxCloudAccessGetDocument = graphql(`
  query SandboxCloudAccessGet {
    sandboxCloudAccessGet {
      target
      on
    }
  }
`);

export type SandboxCloudAccess = {
  /** The platforms turned on, in the sidecar's order; undefined until it answers, or when it cannot. */
  platforms: PermissionTarget[] | undefined;
  /** Whether the sidecar could not read the switches. */
  readFailed: boolean;
};

export function useSandboxCloudAccess(): SandboxCloudAccess {
  // Each opening asks again: another window can turn a platform on or off.
  const [{ data, error }] = useQuery({ query: SandboxCloudAccessGetDocument, requestPolicy: 'cache-and-network' });
  const readFailed = error !== undefined;
  // A failed read hides the platforms an earlier one answered: they may be stale.
  const rows = readFailed ? undefined : data?.sandboxCloudAccessGet;
  return useMemo(
    () => ({ platforms: rows?.filter((row) => row.on).map((row) => row.target), readFailed }),
    [rows, readFailed],
  );
}
