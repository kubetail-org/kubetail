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

// The window's cluster's status, opened from the omnibox's server icon: a header with the
// overall verdict, then three sections (kubeconfig, connection, cache) and a footer linking
// to the Clusters dialog beside a reconnect. Every value is always drawn, a dash standing in
// for a missing one, so the popup never reflows while it is open. The history and the
// controls that act on it stay in the Clusters dialog.
import { Popover as PopoverPrimitive } from '@base-ui/react/popover';
import { Alert, AlertDescription, AlertTitle } from '@kubetail/ui/elements/alert';
import { Button } from '@kubetail/ui/elements/button';
import { ArrowRight, Check, CircleAlert, Server, TriangleAlert } from 'lucide-react';
import { type ReactElement, type ReactNode, type RefObject, useState } from 'react';

import { ClusterDot } from '@/components/cluster/cluster-dot';
import { DataRateSparkline } from '@/components/cluster/data-rate-sparkline';
import { Countdown } from '@/components/primitives/countdown';
import { RelativeTime } from '@/components/primitives/relative-time';
import { RetryButton } from '@/components/primitives/retry-button';
import { VisibleText } from '@/components/primitives/visible-text';
import {
  type NextCheckState,
  useClusterCacheStatsStateWatch,
  useClusterDataRateSampleFeed,
  useClusterScheduleStateWatch,
} from '@/lib/cluster-diagnostics';
import {
  type Status,
  TONE,
  type Tone,
  cacheFailureSummary,
  connectionFailureSummary,
  connectionStatus,
  countLabel,
  findCondition,
  overallTone,
  syncReason,
  syncStatus,
} from '@/lib/cluster-health';
import { type Cluster, type ClusterCacheHealth, formatRate } from '@/lib/clusters';
import { useDialog } from '@/lib/dialog';
import { humanMinutes } from '@/lib/human-duration';

import { OmniboxPopover } from '../omnibox-popover';

// The header's pill: the overall tone in one word.
const PILL: Record<Tone, { label: string; className: string }> = {
  ok: { label: 'Healthy', className: 'border-emerald-200 bg-emerald-50 dark:border-emerald-900 dark:bg-emerald-950' },
  attention: { label: 'Degraded', className: 'border-amber-200 bg-amber-50 dark:border-amber-900 dark:bg-amber-950' },
  error: { label: 'Unhealthy', className: 'border-red-200 bg-red-50 dark:border-red-900 dark:bg-red-950' },
  muted: { label: 'Inactive', className: 'border-border bg-muted' },
};

// The header's subtitle, one phrase per axis.
function summary(connection: Status, sync: Status): string {
  const conn = connection.tone === 'ok' ? 'Connection stable' : connection.label;
  const cache = sync.label === 'Current' ? 'cache in sync' : `cache ${sync.label.toLowerCase()}`;
  return `${conn} · ${cache}`;
}

function Section({ title, verdict, children }: { title: string; verdict?: ReactNode; children: ReactNode }) {
  return (
    <section className="space-y-2 border-t px-3 py-2.5">
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-xs text-muted-foreground">{title}</h3>
        {verdict}
      </div>
      {children}
    </section>
  );
}

// A section's verdict: a check when healthy, the tone's dot otherwise.
function Verdict({ status, label = status.label }: { status: Status; label?: string }) {
  return (
    <span data-tone={status.tone} className={`inline-flex items-center gap-1 text-xs ${TONE[status.tone].text}`}>
      {status.tone === 'ok' ? <Check className="size-3.5" aria-hidden /> : <ClusterDot tone={status.tone} />}
      {label}
    </span>
  );
}

function Stat({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="truncate text-sm font-medium tabular-nums">{children}</dd>
    </div>
  );
}

// A kubeconfig value: cut to the cell, the whole on hover; a dash when the kubeconfig has none.
function KubeconfigValue({ text }: { text: string | undefined }) {
  if (!text) return '—';
  return (
    <span className="block truncate" title={text}>
      <VisibleText text={text} />
    </span>
  );
}

function KubeconfigSection({ context, cluster }: { context: string; cluster: Cluster }) {
  const { kubeconfig } = cluster.status.source;
  const rows: [string, string | undefined][] = [
    ['Context', context],
    ['Server', kubeconfig?.cluster.entry?.server],
    ['User', kubeconfig?.user.name],
  ];
  return (
    <Section title="Kubeconfig">
      <dl className="grid grid-cols-[4rem_minmax(0,1fr)] gap-x-2 gap-y-0.5 text-xs">
        {rows.map(([label, value]) => (
          <div key={label} className="contents">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="min-w-0 font-mono">
              <KubeconfigValue text={value} />
            </dd>
          </div>
        ))}
      </dl>
    </Section>
  );
}

// A hovered sparkline sample: its download rate and age.
function rateTooltip(sample: { at: string; receivedBytesPerSecond: number }) {
  return (
    <>
      {formatRate(sample.receivedBytesPerSecond)} · <RelativeTime ms={Date.parse(sample.at)} />
    </>
  );
}

// The shared verdict's "Active" reads as "Connected" under a section about the connection.
function connectionVerdictLabel(status: Status): string {
  return status.tone === 'ok' ? 'Connected' : status.label;
}

function ConnectionSection({ cluster }: { cluster: Cluster }) {
  const status = connectionStatus(cluster, 'active');
  const connected = findCondition(cluster.conditions, 'Connected');
  // Up or down, the condition's transition is when that began. `unconfirmed` is a previous
  // sidecar's write: when it began is not known yet.
  const down = connected?.status === 'False';
  const sinceMs =
    connected && connected.status !== 'Unknown' && !connected.unconfirmed
      ? Date.parse(connected.transitionedAt)
      : Number.NaN;
  const rates = useClusterDataRateSampleFeed(cluster.id, !cluster.spec.enabled);
  const rate = rates?.samples.at(-1);

  return (
    <Section title="Connection" verdict={<Verdict status={status} label={connectionVerdictLabel(status)} />}>
      <dl className="grid grid-cols-3 items-end gap-2">
        <Stat label={down ? 'Downtime' : 'Uptime'}>
          {Number.isNaN(sinceMs) ? '—' : <RelativeTime ms={sinceMs} format={humanMinutes} />}
        </Stat>
        <Stat label="Download">{rate ? formatRate(rate.receivedBytesPerSecond) : '—'}</Stat>
        {rates && (
          <DataRateSparkline
            series={rates}
            receivedOnly
            scale="sqrt"
            tooltip={rateTooltip}
            className="block h-6 w-full"
          />
        )}
      </dl>
    </Section>
  );
}

// "3 attempts · Retrying in 28s", then "3 attempts · Retrying now…" while the probe runs.
function RetryLine({ check }: { check: NextCheckState }) {
  const attempts = check.failures > 0 ? countLabel(check.failures, 'attempt') : null;
  let next: ReactNode = null;
  if (check.probing) {
    if (attempts) next = 'Retrying now…';
  } else if (check.atMs !== null) {
    next = (
      <>
        Retrying <Countdown ms={check.atMs} />
      </>
    );
  }
  if (!attempts && !next) return null;
  // Tabular figures, so the countdown's digits do not jitter as it ticks.
  return (
    <span className="block tabular-nums">
      {attempts}
      {attempts && next && ' · '}
      {next}
    </span>
  );
}

// A syncing cache's verdict counts its progress, and a current one its paused kinds. A paused
// kind never syncs, so it is left out of the progress.
function cacheVerdictLabel(status: Status, health: ClusterCacheHealth | null | undefined): string {
  if (!health || health.totalKinds === 0) return status.label;
  const active = health.totalKinds - health.pausedKinds;
  if (status.label === 'Syncing') return `Syncing (${active - health.unhealthyKinds}/${active})…`;
  if (status.label === 'Current' && health.pausedKinds > 0) return `Current · ${health.pausedKinds} paused`;
  return status.label;
}

function CacheSection({ cluster }: { cluster: Cluster }) {
  const status = syncStatus(cluster, 'active');
  const cache = cluster.activeCache;
  const health = cache?.health;
  const reason = syncReason(status, health);
  const contents = useClusterCacheStatsStateWatch(cluster.id, cache?.id ?? '', !cache);
  const lastEventMs = Date.parse(health?.lastUpdateAt ?? '');

  return (
    <Section title="Cache" verdict={<Verdict status={status} label={cacheVerdictLabel(status, health)} />}>
      {/* An error's or a stale sync's reason is in the alert above the sections. */}
      {reason && status.tone !== 'error' && health?.reason !== 'Stale' && (
        <p className="text-xs text-muted-foreground">{reason}</p>
      )}
      <dl className="grid grid-cols-3 gap-2">
        <Stat label="Populated kinds">{contents ? contents.kindCount.toLocaleString() : '—'}</Stat>
        <Stat label="Objects">{contents ? contents.objectCount.toLocaleString() : '—'}</Stat>
        <Stat label="Last event">{Number.isNaN(lastEventMs) ? '—' : <RelativeTime ms={lastEventMs} />}</Stat>
      </dl>
    </Section>
  );
}

// A failed connection: the probe's class of failure over its message, then the retry line.
function ConnectionAlert({ cluster, check }: { cluster: Cluster; check: NextCheckState }) {
  const connected = findCondition(cluster.conditions, 'Connected');
  const detail = connected?.message || connected?.reason;
  return (
    <Alert variant="destructive" className="px-3 py-2 text-xs">
      <CircleAlert />
      <AlertTitle className="text-xs">{connectionFailureSummary(connected?.reason)}</AlertTitle>
      <AlertDescription className="text-xs">
        {detail && (
          <p>
            <VisibleText text={detail} />
          </p>
        )}
        <RetryLine check={check} />
      </AlertDescription>
    </Alert>
  );
}

// A failed sync: its class of failure over `syncReason`'s line.
function CacheAlert({ status, health }: { status: Status; health: ClusterCacheHealth | null | undefined }) {
  const reason = syncReason(status, health);
  return (
    <Alert variant="destructive" className="px-3 py-2 text-xs">
      <CircleAlert />
      <AlertTitle className="text-xs">{cacheFailureSummary(health?.reason, health?.failure)}</AlertTitle>
      {reason && <AlertDescription className="text-xs">{reason}</AlertDescription>}
    </Alert>
  );
}

// `Alert` has no amber variant, so an `attention` alert takes the tone's shade, as the
// destructive variant does red.
const ATTENTION_ALERT =
  'px-3 py-2 text-xs text-amber-600 dark:text-amber-400 [&>svg]:text-current *:data-[slot=alert-description]:text-amber-600/90 dark:*:data-[slot=alert-description]:text-amber-400/90';

// A stale sync's title: up to three stale kinds by name, past that a count, which picks out no
// kind the alphabet happened to put first. The Clusters dialog lists them all.
function staleTitle(health: ClusterCacheHealth): string {
  const names = health.unhealthyKindRefs.filter((k) => k.reason === 'Stale').map((k) => k.resource);
  if (names.length === 0) return 'Some kinds aren’t receiving updates';
  if (names.length <= 3) return `${names.join(', ')} not receiving updates`;
  if (names.length >= health.totalKinds - health.pausedKinds) return 'No kinds are receiving updates';
  return `${countLabel(names.length, 'kind')} not receiving updates`;
}

// A stale sync: the kinds whose watch is reopening, and what that means for their data.
function StaleAlert({ health }: { health: ClusterCacheHealth }) {
  return (
    <Alert className={ATTENTION_ALERT}>
      <TriangleAlert />
      <AlertTitle className="text-xs">{staleTitle(health)}</AlertTitle>
      <AlertDescription className="text-xs">
        Data shown may be out of date. Kubetail will restart watches automatically until updates resume.
      </AlertDescription>
    </Alert>
  );
}

// The context, the overall tone as a pill and a one-line subtitle; `children` are the alerts.
function Header({ context, tone, subtitle }: { context: string; tone: Tone; subtitle: string }) {
  return (
    <header className="shrink-0 space-y-0.5 px-3 py-2.5">
      <div className="flex items-center gap-2">
        <Server className="size-4 shrink-0" aria-hidden />
        <p className="min-w-0 flex-1 truncate text-sm font-semibold" title={context}>
          <VisibleText text={context} />
        </p>
        <span
          data-tone={tone}
          className={`inline-flex shrink-0 items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs ${PILL[tone].className} ${TONE[tone].text}`}
        >
          <ClusterDot tone={tone} />
          {PILL[tone].label}
        </span>
      </div>
      <p className="text-xs text-muted-foreground">{subtitle}</p>
    </header>
  );
}

// View details opens the Clusters dialog on this cluster's connection pane, closing the popup.
// Down, the button retries the probe now; up, it reconnects. Either holds while a probe runs,
// whoever asked for it.
function Footer({
  cluster,
  failed,
  probing,
  onClose,
}: {
  cluster: Cluster;
  failed: boolean;
  probing: boolean;
  onClose: () => void;
}) {
  const { openDialog } = useDialog();
  const labels = failed
    ? { label: 'Retry now', retryingLabel: 'Retrying…', checkingLabel: 'Retrying…' }
    : { label: 'Reconnect', retryingLabel: 'Reconnecting…', checkingLabel: 'Checking…' };
  return (
    <footer className="flex shrink-0 items-center justify-between border-t bg-muted/50 px-3 py-2">
      <Button
        variant="link"
        size="xs"
        className="h-auto px-0 text-foreground"
        onClick={() => {
          onClose();
          openDialog('clusters', { clusterId: cluster.id, pane: 'connection' });
        }}
      >
        View details
        <ArrowRight aria-hidden />
      </Button>
      <RetryButton clusterId={cluster.id} size="xs" checking={probing} {...labels} />
    </footer>
  );
}

// The popup over a record. The schedule state watch is read once here: a second subscriber to the
// same shared stream, mounting after the first, would miss its current value.
function ClusterStatus({ context, cluster, onClose }: { context: string; cluster: Cluster; onClose: () => void }) {
  const connection = connectionStatus(cluster, 'active');
  const sync = syncStatus(cluster, 'active');
  const check = useClusterScheduleStateWatch(cluster.id);
  const failed = connection.tone === 'error';
  const health = cluster.activeCache?.health;
  const stale = sync.tone === 'attention' && health?.reason === 'Stale' ? health : null;
  return (
    <>
      <Header context={context} tone={overallTone(connection.tone, sync.tone)} subtitle={summary(connection, sync)} />
      {/* Content-sized; scrolls when the window is too short for it. The alerts scroll too, so a
          long message never pushes the footer's buttons out of the popup. */}
      <div className="min-h-0 flex-auto overflow-y-auto">
        {(failed || sync.tone === 'error' || stale) && (
          <div className="space-y-2 px-3 pb-2.5">
            {failed && <ConnectionAlert cluster={cluster} check={check} />}
            {sync.tone === 'error' && <CacheAlert status={sync} health={health} />}
            {stale && <StaleAlert health={stale} />}
          </div>
        )}
        <KubeconfigSection context={context} cluster={cluster} />
        <ConnectionSection cluster={cluster} />
        <CacheSection cluster={cluster} />
      </div>
      <Footer cluster={cluster} failed={failed} probing={check.probing} onClose={onClose} />
    </>
  );
}

// `cluster` is the context's record, absent until the sidecar has reached it. Hangs from the
// omnibox, at least 320px wide.
export function ClusterStatusPopover({
  anchorRef,
  context,
  cluster,
  children,
}: {
  anchorRef: RefObject<HTMLElement | null>;
  context: string;
  cluster: Cluster | undefined;
  children: ReactElement;
}) {
  const [open, setOpen] = useState(false);
  const popup = (
    <PopoverPrimitive.Popup
      data-testid="cluster-status"
      aria-label="Cluster status"
      className="flex flex-col overflow-hidden rounded-xl border bg-popover text-popover-foreground shadow-lg outline-hidden"
      style={{
        width: 'max(var(--anchor-width), 320px)',
        maxWidth: 'var(--available-width)',
        maxHeight: 'var(--available-height)',
      }}
    >
      {cluster ? (
        <ClusterStatus context={context} cluster={cluster} onClose={() => setOpen(false)} />
      ) : (
        <Header context={context} tone="muted" subtitle="Kubetail has not reached this cluster yet." />
      )}
    </PopoverPrimitive.Popup>
  );

  return (
    <OmniboxPopover anchorRef={anchorRef} popup={popup} open={open} onOpenChange={setOpen}>
      {children}
    </OmniboxPopover>
  );
}
