// Copyright 2024-2026 The Kubetail Authors
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

import fastDeepEqual from 'fast-deep-equal';
import { useAtomValue } from 'jotai';
import { selectAtom } from 'jotai/utils';
import { useEffect, useMemo, useState } from 'react';

import type { KubeContext, FileInfo } from './shared';
import { logMetadataMapAtomFamily, ownershipMapAtomFamily } from './state';

/**
 * getContainerIDs
 */

export function getContainerIDs(
  parentID: string,
  ownershipMap: Map<string, string[]>,
  containerIDs: string[] = [],
): string[] {
  ownershipMap.get(parentID)?.forEach((childID) => {
    if (ownershipMap.has(childID)) getContainerIDs(childID, ownershipMap, containerIDs);
    else containerIDs.push(childID);
  });

  return containerIDs;
}

/**
 * useLogFileInfo hook
 */

type FileInfoWithMetadata = {
  size: number;
  lastModifiedAt: Date;
  containerIDs: string[];
};

export function useLogFileInfo(kubeContext: KubeContext, uids: string[]) {
  const [val, setVal] = useState(new Map<string, FileInfoWithMetadata>());

  // Isolate container id map relevant to hook
  const workloadContainersMapAtom = useMemo(
    () =>
      selectAtom(
        ownershipMapAtomFamily(kubeContext),
        (data) => Object.fromEntries(uids.map((uid) => [uid, getContainerIDs(uid, data)])),
        (a, b) => fastDeepEqual(a, b),
      ),
    [kubeContext, uids],
  );

  const workloadContainersMap = useAtomValue(workloadContainersMapAtom);

  // Stable reference to all container ids
  const allContainerIDs = useMemo(
    () => Array.from(new Set(Object.values(workloadContainersMap).flat())),
    [workloadContainersMap],
  );

  // Isolate metadata relevant to hook
  const metadataAtom = useMemo(
    () =>
      selectAtom(
        logMetadataMapAtomFamily(kubeContext),
        (data) => {
          const out: Record<string, FileInfo> = {};
          for (let i = 0; i < allContainerIDs.length; i += 1) {
            const id = allContainerIDs[i];
            const v = data.inner.get(id);
            if (v) out[id] = v;
          }
          return out;
        },
        (a, b) => fastDeepEqual(a, b),
      ),
    [kubeContext, allContainerIDs],
  );

  const metadata = useAtomValue(metadataAtom);

  useEffect(() => {
    const newVal = new Map<string, { size: number; lastModifiedAt: Date; containerIDs: string[] }>();

    uids.forEach((uid) => {
      const containerIDs = workloadContainersMap[uid];

      // Combine fileInfo
      const fileInfo = {
        size: 0,
        lastModifiedAt: new Date(0),
        containerIDs,
      } as FileInfoWithMetadata;

      containerIDs.forEach((containerID) => {
        const v = metadata[containerID];

        if (v?.size) {
          fileInfo.size += parseInt(v.size, 10);
        }

        if (v?.lastModifiedAt) {
          fileInfo.lastModifiedAt = new Date(Math.max(v.lastModifiedAt.getTime(), fileInfo.lastModifiedAt.getTime()));
        }
      });

      // Update map
      if (fileInfo.lastModifiedAt.getTime() > 0) newVal.set(uid, fileInfo);
    });

    setVal(newVal);
  }, [uids, metadata, workloadContainersMap]);

  return val;
}
