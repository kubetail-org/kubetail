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

import { useAtomValue, useSetAtom } from 'jotai';
import { useSubscription } from '@apollo/client/react';
import { useContext, useMemo, useRef } from 'react';
import { useSearchParams } from 'react-router-dom';

import { CONSOLE_NODES_LIST_FETCH, CONSOLE_NODES_LIST_WATCH, LOG_SOURCES_WATCH } from '@/lib/graphql/dashboard/ops';
import type { ConsoleNodesListItemFragmentFragment } from '@/lib/graphql/dashboard/__generated__/graphql';
import { WatchEventType } from '@/lib/graphql/dashboard/__generated__/graphql';
import { useListQueryWithSubscription } from '@/lib/hooks';
import { Counter, safeDigest } from '@/lib/util';

import { PageContext } from './shared';
import { sourceMapAtom, sourcesAtom } from './state';
import { cssID } from './util';

/**
 * useNodes - Custom hook to provide realtime information about nodes
 */

export function useNodes() {
  const { kubeContext } = useContext(PageContext);

  const { fetching, data } = useListQueryWithSubscription({
    query: CONSOLE_NODES_LIST_FETCH,
    subscription: CONSOLE_NODES_LIST_WATCH,
    queryDataKey: 'coreV1NodesList',
    subscriptionDataKey: 'coreV1NodesWatch',
    variables: { kubeContext: kubeContext || '' },
  });

  const loading = fetching; // treat still-fetching as still-loading
  const nodes = data?.coreV1NodesList?.items
    ? data.coreV1NodesList.items
    : ([] as ConsoleNodesListItemFragmentFragment[]);

  return { loading, nodes };
}

/**
 * useFacets - Custom hook to provide realtime facets
 */

export const useFacets = () => {
  const sources = useAtomValue(sourcesAtom);
  const { nodes } = useNodes();

  return useMemo(() => {
    // Calculate facets
    const regionCounts = new Counter();
    const zoneCounts = new Counter();
    const archCounts = new Counter();
    const osCounts = new Counter();
    const nodeCounts = new Counter();

    // Update nodes facet
    nodes.forEach((node) => {
      nodeCounts.set(node.metadata.name, 0);
    });

    // Update facets
    sources.forEach((source) => {
      regionCounts.update(source.metadata.region);
      zoneCounts.update(source.metadata.zone);
      archCounts.update(source.metadata.arch);
      osCounts.update(source.metadata.os);
      nodeCounts.update(source.metadata.node);
    });

    return {
      region: regionCounts,
      zone: zoneCounts,
      os: osCounts,
      arch: archCounts,
      node: nodeCounts,
    };
  }, [sources, nodes]);
};

/**
 * SourcesFetcher component
 */

export const SourcesFetcher = () => {
  const { kubeContext } = useContext(PageContext);

  const [searchparams] = useSearchParams();
  const sourceStrings = searchparams.getAll('source');

  const setSources = useSetAtom(sourceMapAtom);

  useSubscription(LOG_SOURCES_WATCH, {
    variables: { kubeContext, sources: sourceStrings },
    onData: ({ data }) => {
      const ev = data.data?.logSourcesWatch;
      if (!ev) return;

      const source = ev?.object;
      if (!source) return;

      const k = `${source.namespace}/${source.podName}/${source.containerName}`;
      setSources((prevMap) => {
        const newMap = new Map(prevMap);
        if (ev?.type === WatchEventType.Added) newMap.set(k, source);
        else if (ev?.type === WatchEventType.Deleted) newMap.delete(k);
        return newMap;
      });
    },
  });

  return null;
};

/**
 * Configure container colors component
 */

const palette = [
  '#3B6EDC', // Muted Blue
  '#2F9E5F', // Muted Green
  '#D14343', // Muted Red
  '#D38B2A', // Muted Amber
  '#8456D8', // Muted Purple
  '#2C9CB3', // Muted Cyan
  '#7A6BD1', // Muted Violet
  '#D14D8A', // Muted Pink
  '#7FA83A', // Muted Lime
  '#E06C3A', // Muted Orange
  '#2F9A8A', // Muted Teal
  '#5C63D6', // Muted Indigo
  '#A46A3D', // Muted Brown
  '#C24A77', // Muted Rose
  '#6B8F3A', // Muted Forest Green
  '#4B4FCF', // Muted Deep Blue
  '#9A4EB3', // Muted Magenta
  '#BFA23A', // Muted Gold
  '#4A84C8', // Muted Sky Blue
  '#247A8A', // Muted Blue-Green
];

export const ConfigureContainerColors = () => {
  const sources = useAtomValue(sourcesAtom);
  const containerKeysRef = useRef(new Set<string>());

  sources.forEach((source) => {
    const k = cssID(source.namespace, source.podName, source.containerName);

    // skip if previously defined
    if (containerKeysRef.current.has(k)) return;
    containerKeysRef.current.add(k);

    (async () => {
      // set css var
      const colorIDX = (await safeDigest(k)).getUint32(0) % palette.length;
      document.documentElement.style.setProperty(`--${k}-color`, palette[colorIDX]);
    })();
  });

  return null;
};
