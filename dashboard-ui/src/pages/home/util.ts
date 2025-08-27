// Copyright 2024-2025 Andres Morey
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

import fastDeepEqualES6 from 'fast-deep-equal/es6';
import { useAtomValue } from 'jotai';
import { useEffect, useMemo, useState } from 'react';

import type { FileInfo, KubeContext } from './shared';
import { logMetadataMapAtomFamily, ownershipMapAtomFamily } from './state';

/**
 *
 */

function getContainerIDs(parentID: string, ownershipMap: Map<string, string[]>, containerIDs: string[] = []): string[] {
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
  const logMetadataMap = useAtomValue(logMetadataMapAtomFamily(kubeContext));
  const ownershipMap = useAtomValue(ownershipMapAtomFamily(kubeContext));

  const [val, setVal] = useState(new Map<string, FileInfoWithMetadata>());

  const ids = useMemo(() => uids, [JSON.stringify(uids)]);

  // Memoize container IDs for all uids to avoid recalculating unnecessarily
  const allContainerIDs = useMemo(() => {
    const containerIDs = new Set<string>();
    ids.forEach((uid) => {
      getContainerIDs(uid, ownershipMap).forEach((id) => containerIDs.add(id));
    });
    return Array.from(containerIDs);
  }, [ids, ownershipMap]);

  // Create a stable reference to relevant metadata only
  const relevantMetadata = useMemo(() => {
    const relevant = new Map<string, FileInfo>();
    allContainerIDs.forEach((containerID) => {
      const metadata = logMetadataMap.inner.get(containerID);
      if (metadata) {
        relevant.set(containerID, metadata);
      }
    });
    return relevant;
  }, [logMetadataMap, allContainerIDs]);

  useEffect(() => {
    const newVal = new Map<string, { size: number; lastModifiedAt: Date; containerIDs: string[] }>();

    ids.forEach((uid) => {
      const containerIDs = getContainerIDs(uid, ownershipMap);

      // combine fileInfo
      const fileInfo = {
        size: 0,
        lastModifiedAt: new Date(0),
        containerIDs,
      } as FileInfoWithMetadata;

      containerIDs.forEach((containerID) => {
        const v = relevantMetadata.get(containerID);

        if (v?.size) {
          fileInfo.size += parseInt(v.size, 10);
        }

        if (v?.lastModifiedAt) {
          fileInfo.lastModifiedAt = new Date(Math.max(v.lastModifiedAt.getTime(), fileInfo.lastModifiedAt.getTime()));
        }
      });

      // update map
      if (fileInfo.lastModifiedAt.getTime() > 0) newVal.set(uid, fileInfo);
    });

    // Compare newVal against current val and return current val if contents are the same
    setVal((prevVal) => (fastDeepEqualES6(prevVal, newVal) ? prevVal : newVal));
  }, [ids, ownershipMap, relevantMetadata]);

  return val;
}
