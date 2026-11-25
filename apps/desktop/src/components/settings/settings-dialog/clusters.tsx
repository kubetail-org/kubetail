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

// The Settings dialog's Clusters tab: every kubeconfig cluster Kubetail tracks,
// then the archived ones, each with its cache size and its actions. An action
// writes through and the row redraws from clusterCollectionWatch; a failure reaches the
// error bus through the client.
import { Button } from '@kubetail/ui/elements/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@kubetail/ui/elements/dropdown-menu';
import { cn } from '@kubetail/ui/lib/utils';
import { Ellipsis, Trash2 } from 'lucide-react';
import { type ReactNode } from 'react';
import { useMutation } from 'urql';

import { VisibleText } from '@/components/primitives/visible-text';
import { useClusterCacheStatsStateWatch } from '@/lib/cluster-diagnostics';
import {
  ClusterCacheClearMutation,
  ClusterDefaultSetMutation,
  ClusterDeleteMutation,
  ClusterEnabledSetMutation,
  ClusterSyncEnabledSetMutation,
} from '@/lib/cluster-mutations';
import { type Cluster, formatBytes, useClusters } from '@/lib/clusters';
import { visibleTitle } from '@/lib/visible-text';

import { Group } from './group';

export function ClustersSettings() {
  const { clusters } = useClusters();
  // Nothing before the Bookmark: a half-listed fleet must not read as the whole.
  if (clusters === null) return null;

  const tracked = clusters.filter((c) => c.spec.source.kubeconfig);
  const present = tracked.filter((c) => c.status.source.kubeconfig?.isPresent);
  const archived = tracked.filter((c) => !c.status.source.kubeconfig?.isPresent);

  return (
    <>
      <Group title="Kubeconfig" lede="Clusters Kubetail found in your kubeconfig.">
        {present.length > 0 ? (
          <ClusterList clusters={present} />
        ) : (
          <p className="text-sm text-muted-foreground">No clusters in your kubeconfig.</p>
        )}
      </Group>
      {archived.length > 0 && (
        <Group title="Archived" lede="No longer in your kubeconfig. Caches are kept until you delete them.">
          <ClusterList clusters={archived} />
        </Group>
      )}
    </>
  );
}

function ClusterList({ clusters }: { clusters: Cluster[] }) {
  return (
    <ul className="divide-y rounded-lg border">
      {clusters.map((c) => (
        <ClusterRow key={c.id} cluster={c} />
      ))}
    </ul>
  );
}

function ClusterRow({ cluster }: { cluster: Cluster }) {
  const { spec, activeCache } = cluster;
  const source = cluster.status.source.kubeconfig;
  const title = spec.name || spec.source.kubeconfig?.context || '—';
  const contents = useClusterCacheStatsStateWatch(cluster.id, activeCache?.id ?? '', !activeCache);
  const stored = activeCache && contents?.exists ? contents : null;
  return (
    <li className="flex items-center gap-3 px-4 py-2.5">
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="flex min-w-0 items-center gap-2">
          <span
            data-slot="name"
            className={cn('truncate text-sm', spec.enabled ? 'text-popover-foreground' : 'text-muted-foreground')}
          >
            <VisibleText text={title} />
          </span>
          {spec.isDefault && <Badge>Default</Badge>}
          {!spec.enabled && <Badge>Disabled</Badge>}
          {spec.enabled && !spec.syncEnabled && <Badge>Sync paused</Badge>}
        </div>
        {/* Each value shrinks by its length, so a long one never hides the other. */}
        <div className="flex min-w-0 items-center gap-1.5 font-mono text-xs text-muted-foreground">
          <Detail text={source?.cluster.entry?.server || '—'} />
          <span aria-hidden>·</span>
          <Detail text={source?.user.name || '—'} />
        </div>
      </div>
      <span data-slot="size" className="w-16 shrink-0 text-right text-xs text-muted-foreground">
        {stored ? formatBytes(stored.bytes) : '—'}
      </span>
      {source?.isPresent ? (
        <RowMenu cluster={cluster} title={title} onDisk={stored !== null} />
      ) : (
        <DeleteButton id={cluster.id} title={title} />
      )}
    </li>
  );
}

function RowMenu({ cluster, title, onDisk }: { cluster: Cluster; title: string; onDisk: boolean }) {
  const { id, spec, activeCache } = cluster;
  const [, setDefault] = useMutation(ClusterDefaultSetMutation);
  const [, setSyncEnabled] = useMutation(ClusterSyncEnabledSetMutation);
  const [, setEnabled] = useMutation(ClusterEnabledSetMutation);
  const [, clearCache] = useMutation(ClusterCacheClearMutation);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<Button variant="ghost" size="icon" className="size-7 rounded-md" />}
        aria-label={`Actions for ${title}`}
      >
        <Ellipsis className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" side="bottom">
        {/* A disabled cluster can be neither the default nor synced. */}
        {spec.enabled && (
          <>
            <DropdownMenuItem onClick={() => void setDefault({ id, isDefault: !spec.isDefault })}>
              {spec.isDefault ? 'Remove as default' : 'Set as default'}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => void setSyncEnabled({ id, syncEnabled: !spec.syncEnabled })}>
              {spec.syncEnabled ? 'Pause syncing' : 'Resume syncing'}
            </DropdownMenuItem>
          </>
        )}
        <DropdownMenuItem onClick={() => void setEnabled({ id, enabled: !spec.enabled })}>
          {spec.enabled ? 'Disable' : 'Enable'}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          variant="destructive"
          disabled={!onDisk}
          onClick={activeCache ? () => void clearCache({ id: activeCache.id }) : undefined}
        >
          Reset cache
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function DeleteButton({ id, title }: { id: string; title: string }) {
  const [, remove] = useMutation(ClusterDeleteMutation);
  return (
    <Button
      variant="ghost"
      size="icon"
      className="size-7 rounded-md"
      aria-label={`Delete ${title}`}
      onClick={() => void remove({ id })}
    >
      <Trash2 className="size-4" />
    </Button>
  );
}

// One value of the detail line, cut with an ellipsis and whole in its tooltip.
function Detail({ text }: { text: string }) {
  return (
    <span className="min-w-0 truncate" title={visibleTitle(text)}>
      <VisibleText text={text} />
    </span>
  );
}

// The design's outline Badge at h-5, drawn here since @kubetail/ui has none.
function Badge({ children }: { children: ReactNode }) {
  return (
    <span className="inline-flex h-5 shrink-0 items-center rounded-md border px-2 text-xs font-medium whitespace-nowrap text-muted-foreground">
      {children}
    </span>
  );
}
