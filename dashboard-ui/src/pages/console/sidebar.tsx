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

import type { CheckedState } from '@radix-ui/react-checkbox';
import { useSetAtom } from 'jotai';
import { CirclePlus as CirclePlusIcon, Trash2 as TrashIcon } from 'lucide-react';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';

import { Checkbox } from '@kubetail/ui/elements/checkbox';
import { Label } from '@kubetail/ui/elements/label';

import KubetailLogo from '@/assets/logo.svg?react';
import SourcePickerModal from '@/components/widgets/SourcePickerModal';
import type { LogSourceFragmentFragment } from '@/lib/graphql/dashboard/__generated__/graphql';
import { MapSet, getBasename, joinPaths } from '@/lib/util';
import type { Counter } from '@/lib/util';
import { ALL_WORKLOAD_KINDS, GLYPH_ICON_MAP, PLURAL_LABEL_MAP, WorkloadKind } from '@/lib/workload';

import { filtersAtom } from './state';
import { cssID } from './util';
import { useSources, useViewerFacets } from './viewer';

/**
 * Helper functions
 */

export const generateMapKey = (namespace: string, podName: string) => `${namespace}/${podName}`;

/**
 * Sidebar workloads component
 */

const workloadTypestrMap: Record<string, WorkloadKind> = {
  daemonsets: WorkloadKind.DAEMONSETS,
  deployments: WorkloadKind.DEPLOYMENTS,
  replicasets: WorkloadKind.REPLICASETS,
  statefulsets: WorkloadKind.STATEFULSETS,
  cronjobs: WorkloadKind.CRONJOBS,
  jobs: WorkloadKind.JOBS,
  pods: WorkloadKind.PODS,
};

export function parseSourceArg(input: string) {
  const regex = /^([^:]+):([^/]+)\/(.+)$/;
  const match = input.match(regex);

  if (!match) {
    throw new Error(
      `Invalid input format. Expected format is "<namespace>:<workload-type>/<workload-name>", got "${input}"`,
    );
  }

  // Destructure the match array. The first element is the full match, so we skip it.
  const [, namespace, workloadTypeStr, workloadName] = match;

  const workloadType = workloadTypestrMap[workloadTypeStr];

  if (!workloadType) {
    throw new Error(`Invalid workload type: ${workloadTypeStr}`);
  }

  return { namespace, workloadType, workloadName };
}

const SidebarWorkloads = () => {
  const [isPickerOpen, setIsPickerOpen] = useState(false);
  const [searchParams] = useSearchParams();

  const kubeContext = searchParams.get('kubeContext') || '';

  // Build workload map
  const workloadMap = useMemo(() => {
    const m = new MapSet<WorkloadKind, { namespace: string; name: string }>();
    searchParams.getAll('source').forEach((source) => {
      const { namespace, workloadType, workloadName } = parseSourceArg(source);
      m.add(workloadType, { namespace, name: workloadName });
    });
    return m;
  }, [searchParams]);

  const deleteSource = useCallback(
    (sourcePath: string) => {
      searchParams.delete('source', sourcePath);

      // TODO: instead of navigating to new url can we use react-router?
      const currentUrl = new URL(window.location.href);
      currentUrl.search = new URLSearchParams(searchParams).toString();
      window.location.href = currentUrl.toString();
    },
    [searchParams],
  );

  return (
    <>
      <SourcePickerModal open={isPickerOpen} onOpenChange={setIsPickerOpen} />
      {kubeContext !== '' && (
        <div
          className="mb-2 font-bold text-primary overflow-hidden text-ellipsis whitespace-nowrap"
          title={kubeContext}
        >
          Cluster:
          {kubeContext}
        </div>
      )}
      <div className="flex items-center justify-between mb-1">
        <div className="flex items-center space-x-1">
          <span className="font-bold text-chrome-500">Sources</span>
        </div>
        <button
          type="button"
          onClick={() => setIsPickerOpen(true)}
          className="cursor-pointer"
          aria-label="Open source picker"
        >
          <CirclePlusIcon className="h-5 w-5 text-primary" />
        </button>
      </div>
      <div className="space-y-2">
        {ALL_WORKLOAD_KINDS.map((kind) => {
          const objs = workloadMap.get(kind);
          if (!objs) return;

          const vals = Array.from(objs.values());
          vals.sort((a, b) => a.name.localeCompare(b.name));

          const Icon = GLYPH_ICON_MAP[kind];
          return (
            <div key={kind}>
              <div className="flex items-center space-x-1">
                <div>
                  <Icon className="h-[18px] w-[18px]" />
                </div>
                <div className="font-semibold text-chrome-500">{PLURAL_LABEL_MAP[kind]}</div>
              </div>
              <ul className="pl-[23px]">
                {vals.map((val) => (
                  <li key={val.name} className="flex items-center justify-between">
                    <span className="whitespace-nowrap overflow-hidden text-ellipsis">
                      {val.name.replace(/\/\*$/, '')}
                    </span>
                    <button
                      type="button"
                      onClick={() => deleteSource(`${val.namespace}:${kind}/${val.name}`)}
                      aria-label="Delete source"
                    >
                      <TrashIcon className="h-[18px] w-[18px] text-chrome-300 hover:text-chrome-500 cursor-pointer" />
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          );
        })}
      </div>
    </>
  );
};

/**
 * Sidebar pods and containers component
 */

type ContainersProps = {
  namespace: string;
  podName: string;
  containerNames?: string[];
};

const Containers = ({ namespace, podName, containerNames = [] }: ContainersProps) => {
  const [searchParams, setSearchParams] = useSearchParams();

  const sortedContainerNames = useMemo(() => containerNames.sort(), [containerNames]);

  const handleToggle = useCallback(
    (name: string, value: string, checked: CheckedState) => {
      if (checked) searchParams.append(name, value);
      else searchParams.delete(name, value);
      setSearchParams(new URLSearchParams(searchParams));
    },
    [searchParams, setSearchParams],
  );

  return (
    <>
      {sortedContainerNames.map((containerName) => {
        const k = cssID(namespace, podName, containerName);
        const urlKey = 'container';
        const urlVal = `${namespace}:${podName}/${containerName}`;
        return (
          <Label key={containerName} className="flex item-center justify-between">
            <div className="flex items-center space-x-1">
              <div className="w-[13px] h-[13px]" style={{ backgroundColor: `var(--${k}-color)` }} />
              <div>{containerName}</div>
            </div>
            <Checkbox
              className="bg-background"
              checked={searchParams.has(urlKey, urlVal)}
              name={urlKey}
              value={urlVal}
              onCheckedChange={(checked) => handleToggle(urlKey, urlVal, checked)}
            />
          </Label>
        );
      })}
    </>
  );
};

class ContainerGroup {
  namespace: string;

  podName: string;

  containers: string[];

  constructor(namespace: string, podName: string, containers: string[] = []) {
    this.namespace = namespace;
    this.podName = podName;
    this.containers = containers;
  }

  addContainer(containerName: string): void {
    if (!this.containers.includes(containerName)) {
      this.containers.push(containerName);
    }
  }
}

const SidebarPodsAndContainers = () => {
  const { sources } = useSources();
  const [searchParams] = useSearchParams();

  const containerGroups = useMemo(() => {
    // Create synthetic sources from search params
    searchParams.getAll('container').forEach((key) => {
      const match = key.match(/^([^:]+):([^/]+)\/(.+)$/);
      if (!match) return; // Skip if pattern doesn't match

      const synthetic = {
        namespace: match[1],
        podName: match[2],
        containerName: match[3],
      } as LogSourceFragmentFragment;

      if (
        !sources.some(
          (s) =>
            s.namespace === synthetic.namespace &&
            s.podName === synthetic.podName &&
            s.containerName === synthetic.containerName,
        )
      ) {
        sources.push(synthetic);
      }
    });
    sources.sort((a, b) => a.podName.localeCompare(b.podName));

    // Group containers by pod
    const groupMap = new Map<string, ContainerGroup>();
    sources.forEach((source) => {
      const k = generateMapKey(source.namespace, source.podName);
      if (!groupMap.has(k)) groupMap.set(k, new ContainerGroup(source.namespace, source.podName));
      groupMap.get(k)?.addContainer(source.containerName);
    });

    return Array.from(groupMap.values()).sort((a, b) => {
      const keyA = `${a.namespace}/${a.podName}`;
      const keyB = `${b.namespace}/${b.podName}`;
      return keyA.localeCompare(keyB);
    });
  }, [sources, searchParams]);

  return (
    <>
      <div className="border-t border-chrome-divider mt-2.5" />
      <div className="py-2.5 font-bold text-chrome-500">Pods/Containers</div>
      <div className="space-y-3">
        {containerGroups.map((group) => (
          <div key={`${group.namespace}/${group.podName}`}>
            <div className="flex items-center justify-between">
              <div className="whitespace-nowrap overflow-hidden text-ellipsis">{group.podName}</div>
            </div>
            <Containers namespace={group.namespace} podName={group.podName} containerNames={group.containers} />
          </div>
        ))}
      </div>
    </>
  );
};

/**
 * Sidebar facets component
 */

const Facets = ({ label, counter }: { label: string; counter: Counter }) => {
  const [searchParams, setSearchParams] = useSearchParams();
  const urlKey = label.toLocaleLowerCase();

  const entries = counter.orderedEntries();

  const handleToggle = useCallback(
    (name: string, value: string, checked: CheckedState) => {
      if (checked) searchParams.append(name, value);
      else searchParams.delete(name, value);
      setSearchParams(new URLSearchParams(searchParams));
    },
    [searchParams, setSearchParams],
  );

  // If there are no entries, or only one empty entry, return null
  if (entries.length === 0 || (entries.length === 1 && entries[0][0] === '')) {
    return null;
  }

  return (
    <>
      <div className="border-t border-chrome-300 mt-2.5 py-2.5 font-bold text-chrome-500">{label}</div>
      <div className="space-y-1.5">
        {entries.map(([facet, count]) => (
          <div key={facet}>
            <Label className="flex items-center">
              <Checkbox
                className="bg-background"
                checked={searchParams.has(urlKey, facet)}
                name={urlKey}
                value={facet}
                onCheckedChange={(checked) => handleToggle(urlKey, facet, checked)}
              />
              <div className="grow flex justify-between">
                <div>{facet}</div>
                <div>{`(${count})`}</div>
              </div>
            </Label>
          </div>
        ))}
      </div>
    </>
  );
};

const SidebarFacets = () => {
  const facets = useViewerFacets();

  return (
    <div>
      <Facets label="Region" counter={facets.region} />
      <Facets label="Zone" counter={facets.zone} />
      <Facets label="OS" counter={facets.os} />
      <Facets label="Arch" counter={facets.arch} />
      <Facets label="Node" counter={facets.node} />
    </div>
  );
};

/**
 * Sidebar
 */

export const Sidebar = () => {
  const [searchParams] = useSearchParams();
  const setFilters = useSetAtom(filtersAtom);

  // sync filters with search params
  useEffect(() => {
    const filters = new MapSet<string, string>();
    ['region', 'zone', 'os', 'arch', 'node', 'container'].forEach((key) => {
      if (searchParams.has(key)) filters.set(key, new Set(searchParams.getAll(key)));
    });
    setFilters(filters);
  }, [searchParams]);

  return (
    <div className="text-sm px-[7px] pt-2.5">
      <a href={joinPaths(getBasename(), '/')}>
        <KubetailLogo className="text-primary h-[38px] w-auto mt-1 mb-3" />
      </a>
      <SidebarWorkloads />
      <SidebarPodsAndContainers />
      <SidebarFacets />
    </div>
  );
};
