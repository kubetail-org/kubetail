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

// The app bar's context omnibox: the address bar of the cluster surface, where the
// cluster is the address. One white pill: on the left the cluster chip, then a divider,
// then the Chat and Dashboard tabs. In the chip the server icon, carrying the window's
// cluster's verdict, opens its status; a press anywhere else there opens the cluster
// picker, which switches the window's cluster and leads to the Clusters dialog.
import { Button } from '@kubetail/ui/elements/button';
import { Spinner } from '@kubetail/ui/elements/spinner';
import { cn } from '@kubetail/ui/lib/utils';
import { Server } from 'lucide-react';
import type { ReactNode, RefObject } from 'react';
import { useRef } from 'react';

import { ClusterDot } from '@/components/cluster/cluster-dot';
import { ClusterPicker } from '@/components/cluster/cluster-picker';
import { ClusterStatusPopover } from '@/components/cluster/cluster-status';
import { UnverifiedTLSBadge } from '@/components/cluster/unverified-tls-badge';
import { useActiveKubeContext } from '@/lib/active-kube-context';
import { contextHealth } from '@/lib/cluster-health';
import type { Health } from '@/lib/cluster-health';
import { clusterForContext, useClusters } from '@/lib/clusters';
import { APP_BAR_HOVER_CLASS } from '@/lib/control-style';
import { tlsUnverifiedReason } from '@/lib/kube-config';

import { ModeTabs } from './mode-tabs';

// Each control's `after` box stretches its hit area over the pill's padding and the gaps
// beside it, so every press in the omnibox, to its drawn edge, lands on a control. It sits
// behind the control's content, so a tooltip there still shows; `isolate` keeps it within
// the control rather than behind the pill's fill.
const HIT_AREA_CLASS = 'relative isolate after:absolute after:-z-10';

// Clears the icon around the dot's corner. A ring in the omnibox's colour would do
// the same, but that colour is translucent in dark mode.
const DOT_CUTOUT = { maskImage: 'radial-gradient(circle at 100% 100%, transparent 6px, black 6.5px)' };

// A narrow omnibox has room for no words but the cluster's name.
const COMPACT_HIDDEN = '@max-[14rem]:hidden';

// The server icon, with the cluster's verdict as a dot in its corner when given one.
function ServerGlyph({ health }: { health?: Health }) {
  return (
    <span data-testid="cluster-glyph" className="relative flex">
      <Server aria-hidden className="size-4" style={health ? DOT_CUTOUT : undefined} />
      {health && <ClusterDot tone={health.tone} title={health.summary} className="absolute -right-1 -bottom-1" />}
    </span>
  );
}

// The server icon as its own control, opening the window's cluster's status: `icon-sm`'s
// 28px, a circle, its icon gray. Its hit area covers the chip's 6px left padding and the
// pill's 4px beside it, and the 2px above and below it in the chip and the pill's 3px past
// that. The insets count from inside the button's own 1px transparent border (the
// library's, for its focus ring). Under 14rem the chip's padding goes, leaving the name
// room. Nothing while the window is on no cluster: the trigger draws the bare icon then.
function StatusButton({ anchorRef }: { anchorRef: RefObject<HTMLElement | null> }) {
  const { context } = useActiveKubeContext();
  const { clusters } = useClusters();
  if (context === '') return null;

  const cluster = clusterForContext(clusters, context);
  const health = contextHealth(clusters, context);
  return (
    <ClusterStatusPopover anchorRef={anchorRef} context={context} cluster={cluster}>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={`Cluster status: ${health.summary}`}
        className={cn(
          HIT_AREA_CLASS,
          'rounded-full text-muted-foreground after:-inset-y-1.5 after:-right-px after:-left-2.75 @max-[14rem]:after:-left-1.25',
          APP_BAR_HOVER_CLASS,
        )}
      >
        <ServerGlyph health={health} />
      </Button>
    </ClusterStatusPopover>
  );
}

// The rest of the chip after the status button, reading the window's cluster with its TLS
// badge, or why there is none. It fills the chip's height and holds its gap and right
// padding; its hit area reaches over the pill's 3px above and below and the 4px gap up to
// the divider, so a press anywhere there lands on it. In a narrow omnibox the name
// truncates first and the badge shrinks to its icon, so the warning stays; the connecting
// and empty states draw their icon alone, their words left to the button's name. With no
// cluster it also draws the bare icon, where the status button would be.
function ClusterTrigger({ anchorRef }: { anchorRef: RefObject<HTMLElement | null> }) {
  const { context, active, noneSelected, phase } = useActiveKubeContext();
  const hasCluster = context !== '';

  let name: string;
  let label: ReactNode;
  if (hasCluster) {
    const entry = active?.clusterEntry ?? null;
    // The button's name stands in for everything inside it, the badge's words included.
    name = tlsUnverifiedReason(entry) ? `Cluster: ${context}, unverified TLS` : `Cluster: ${context}`;
    label = (
      <>
        <span data-testid="cluster-trigger-name" className="truncate font-semibold text-foreground">
          {context}
        </span>
        <UnverifiedTLSBadge entry={entry} collapsible />
      </>
    );
  } else if (phase === 'connecting') {
    // Said apart from "No kubeconfig", so a stalled first dial does not read as
    // a permanent absence.
    name = 'Cluster: connecting';
    label = (
      <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <Spinner size="xs" className="mr-0" />
        <span className={COMPACT_HIDDEN}>Connecting…</span>
      </span>
    );
  } else if (noneSelected) {
    name = 'Cluster: none selected';
    label = <span className={cn('text-xs text-muted-foreground', COMPACT_HIDDEN)}>No cluster selected</span>;
  } else {
    name = 'Cluster: no kubeconfig';
    label = <span className={cn('text-xs text-muted-foreground', COMPACT_HIDDEN)}>No kubeconfig</span>;
  }

  return (
    <ClusterPicker anchorRef={anchorRef}>
      <button
        type="button"
        aria-label={name}
        className={cn(
          HIT_AREA_CLASS,
          'flex h-full min-w-0 items-center rounded-full pr-2.5 after:-inset-y-0.75 after:-right-1',
          // With no status button, the bare icon takes its place and the hit area its
          // reach over the chip's and the pill's left padding.
          hasCluster ? 'pl-1.5 after:left-0' : 'after:-left-2.5 @max-[14rem]:after:-left-1',
        )}
      >
        {!hasCluster && (
          <span className="flex size-7 shrink-0 items-center justify-center text-muted-foreground">
            <ServerGlyph />
          </span>
        )}
        <span className="flex min-w-0 items-center gap-2 text-[13px] text-muted-foreground @max-[14rem]:gap-1">
          {label}
        </span>
      </button>
    </ClusterPicker>
  );
}

// The status button and the trigger, which share one hover wash.
function ClusterChip({ anchorRef }: { anchorRef: RefObject<HTMLElement | null> }) {
  return (
    <div
      data-testid="cluster-chip"
      className={cn('flex h-8 min-w-0 items-center rounded-full pl-1.5 @max-[14rem]:pl-0', APP_BAR_HOVER_CLASS)}
    >
      <StatusButton anchorRef={anchorRef} />
      <ClusterTrigger anchorRef={anchorRef} />
    </div>
  );
}

export function ContextOmnibox() {
  // Every popover hangs from the omnibox itself, not from the control that opens it.
  const boxRef = useRef<HTMLDivElement>(null);

  return (
    <div
      ref={boxRef}
      data-testid="context-omnibox"
      // Opts out of the app bar's drag region: this is a control, and pressing
      // it should no more move the window than a browser's address bar.
      data-tauri-drag-region="false"
      // The bar's middle column sets the width, so the pill fills it.
      // A container, so its controls compact to its width: the chip as wide as its
      // content, the name truncating first; the tabs filling the rest.
      className="@container flex h-9.5 w-full items-center gap-1 rounded-full bg-white px-1 py-0.75 shadow-pill dark:bg-input/30"
    >
      <ClusterChip anchorRef={boxRef} />
      <div aria-hidden className="h-4.5 w-px shrink-0 bg-sidebar-border" />
      {/* Its hit area covers the divider and the gap after it, the pill's right padding,
      and its 3px above and below. */}
      <ModeTabs className={cn(HIT_AREA_CLASS, 'flex-1 after:-inset-y-0.75 after:-right-1 after:-left-1.25')} />
    </div>
  );
}
