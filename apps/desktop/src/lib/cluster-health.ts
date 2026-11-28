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

// A cluster record's health: a connection axis and a sync axis, each a word with a
// tone, and the overall tone, the worse of the two. The Clusters dialog draws the
// axes as columns and the overall tone as its leading circle; the cluster picker
// and the omnibox's dot show the overall tone per kubeconfig context.
import { clusterForContext } from '@/lib/clusters';
import type { Cluster, ClusterCacheHealth } from '@/lib/clusters';

export type Tone = 'ok' | 'attention' | 'error' | 'muted';
// An object's tone, as its status reads: healthy, on its way somewhere, or broken.
export type StatusTone = 'ok' | 'pending' | 'failing';
// A record still named by the kubeconfig, or one whose cache outlived its context.
export type Group = 'active' | 'orphaned';

// Per tone: its severity, its dot fill, and a text-tuned shade for a tinted label.
export const TONE: Record<Tone, { severity: number; dot: string; text: string }> = {
  error: { severity: 3, dot: 'bg-red-500', text: 'text-red-600 dark:text-red-400' },
  attention: { severity: 2, dot: 'bg-amber-500', text: 'text-amber-600 dark:text-amber-400' },
  ok: { severity: 1, dot: 'bg-emerald-500', text: 'text-emerald-600 dark:text-emerald-400' },
  muted: { severity: 0, dot: 'bg-muted-foreground/50', text: 'text-muted-foreground' },
};

// An object's tone in the palette cluster health uses: green, amber, red.
export const HEALTH_TONE: Record<StatusTone, Tone> = { ok: 'ok', pending: 'attention', failing: 'error' };

export type Status = { label: string; tone: Tone };
export type Health = { tone: Tone; summary: string };

export function overallTone(connection: Tone, sync: Tone): Tone {
  return TONE[connection].severity >= TONE[sync].severity ? connection : sync;
}

// Pending = not yet identified (kube-system UID probe hasn't succeeded, e.g. a
// stopped minikube): can't sync or cache yet.
export function isPending(c: Cluster): boolean {
  return !c.status.server.uid;
}

export function findCondition<T extends { type: string }>(conditions: T[], type: string): T | undefined {
  return conditions.find((cond) => cond.type === type);
}

// Derives from the live `Connected` condition, not the sticky `server.uid` (never
// clears, so it would read "Active" after a drop).
export function connectionStatus(c: Cluster, group: Group): Status {
  if (group === 'orphaned') return { label: 'Unavailable', tone: 'muted' };
  const connected = findCondition(c.conditions, 'Connected');
  if (connected?.status === 'True') return { label: 'Active', tone: 'ok' };
  if (connected?.status === 'False') return { label: 'Disconnected', tone: 'error' };
  return { label: 'Connecting', tone: 'attention' };
}

// A request's class of failure (the sidecar's kubeconn.Reason), in a few words: a failed
// `Connected` condition's reason, or a failed sync's `failure`.
const FAILURES = new Map([
  ['Unreachable', 'Can’t reach the server'],
  ['TLSInvalid', 'Server certificate rejected'],
  ['Timeout', 'Server didn’t answer in time'],
  ['ContextNotFound', 'Context missing from kubeconfig'],
  ['ResolveFailed', 'Kubeconfig entry can’t be used'],
  ['Unauthorized', 'Credentials rejected'],
  ['Forbidden', 'Permission denied'],
  ['NotFound', 'Resource no longer served'],
  ['InternalError', 'Server error'],
  ['ServiceUnavailable', 'Server unavailable'],
  ['Throttled', 'Rate limited by the server'],
  ['Malformed', 'Unexpected response from the server'],
]);

export function connectionFailureSummary(reason: string | undefined): string {
  return FAILURES.get(reason ?? '') ?? 'Connection failed';
}

// An `error` sync verdict, in a few words: a failed sync by its class of failure where it has
// one.
export function cacheFailureSummary(reason: string | undefined, failure?: string): string {
  if (reason === 'SyncFailed') return FAILURES.get(failure ?? '') ?? 'Some kinds aren’t syncing';
  if (reason === 'StoreFailed') return 'Cache storage failed';
  return 'Cache error';
}

// Catching up, or discovery hasn't landed: not a fault.
const CATCHING_UP = new Set(['Syncing', 'Unknown']);

// Whether the active cache is still catching up with the cluster, so its rows may not hold
// every object yet. Reads the sidecar's per-kind rollup, dominated by the worst kind: neither
// the cache's coarse Synced condition nor any single kind's would do. No rollup yet means
// nothing observed, only work in progress.
export function cacheCatchingUp(c: Cluster): boolean {
  const health = c.activeCache?.health;
  return !health || CATCHING_UP.has(health.reason);
}

// Gates on the live `Connected` condition first (a dropped connection leaves the
// engine reporting a stale "Watching"), then reads the rollup.
export function syncStatus(c: Cluster, group: Group): Status {
  if (group === 'orphaned') return { label: 'Stopped', tone: 'muted' };
  if (isPending(c)) return { label: 'Not synced', tone: 'muted' };
  if (!c.spec.syncEnabled) return { label: 'Paused', tone: 'muted' };
  const connected = findCondition(c.conditions, 'Connected');
  // Muted, not amber: the fault is on the connection axis, and graying the gated
  // sync value keeps it reading as a downstream symptom.
  if (connected?.status === 'False') return { label: 'Stalled', tone: 'muted' };
  const health = c.activeCache?.health;
  if (!health || CATCHING_UP.has(health.reason)) return { label: 'Syncing', tone: 'ok' };
  switch (health.reason) {
    case 'SyncFailed':
      return { label: 'Error', tone: 'error' };
    // The cache's file will not open, so nothing under it syncs and no kind can say
    // why. It never clears on its own (the Clear button is the fix), which is why it
    // reads as hard as a failing kind rather than as a stall.
    case 'StoreFailed':
      return { label: 'Storage error', tone: 'error' };
    // A watch went quiet past the freshness threshold: amber, not a hard error.
    case 'Stale':
      return { label: 'Stale', tone: 'attention' };
    // Steady state: all kinds caught up and streaming deltas.
    case 'Watching':
      return { label: 'Current', tone: 'ok' };
    // Waiting on credentials; muted like Stalled, since the sync itself is fine.
    case 'NoConnection':
      return { label: 'Connecting', tone: 'muted' };
    // Every kind paused. Distinct from the spec.syncEnabled gate above, which flips
    // instantly on resume, before the kinds' reconciles land.
    case 'Paused':
      return { label: 'Paused', tone: 'muted' };
    // An unknown reason is DEGRADED, not healthy (the schema and the sidecar's fold
    // both read it that way); green here would paint every future verdict healthy.
    default:
      return { label: 'Degraded', tone: 'attention' };
  }
}

export function overallStatus(connection: Status, sync: Status): Health {
  return { tone: overallTone(connection.tone, sync.tone), summary: `${connection.label} · ${sync.label}` };
}

// A cluster record's health, as its active cache and connection give it.
export function clusterHealth(record: Cluster): Health {
  return overallStatus(connectionStatus(record, 'active'), syncStatus(record, 'active'));
}

// A kubeconfig context's health: its record's, or muted while no record matches,
// which is also every context's before the clusters snapshot lands.
export function contextHealth(clusters: Cluster[] | null, context: string): Health {
  const record = clusterForContext(clusters, context);
  if (!record) return { tone: 'muted', summary: 'Not reached' };
  return clusterHealth(record);
}

// "120 kinds" / "1 kind" — suffix-"s" pluralisation covers every noun counted here.
export function countLabel(n: number, noun: string): string {
  return `${n.toLocaleString()} ${n === 1 ? noun : `${noun}s`}`;
}

// "118 of 120 kinds — widgets, gateways not syncing", "3 of 5 kinds syncing, 2 paused", or
// plain "120 kinds". Read straight off the rollup — re-folding the per-kind stream here
// would be a second definition of health that can disagree with the badge above it
// mid-frame.
//
// Paused kinds are counted apart because they are neither: they stay in totalKinds and are
// never offenders, so a label spending unhealthyKinds alone would render a plain "5 kinds"
// over a cache where two of them are switched off.
export function kindsSyncingLabel(health: ClusterCacheHealth): string {
  const idle = health.unhealthyKinds + health.pausedKinds;
  if (idle === 0) return countLabel(health.totalKinds, 'kind');
  const syncing = `${health.totalKinds - idle} of ${countLabel(health.totalKinds, 'kind')}`;
  if (health.unhealthyKinds === 0) return `${syncing} syncing, ${health.pausedKinds} paused`;
  // unhealthyKinds counts every non-Watching kind; offenderList names only those behind
  // the verdict. Mid-pause there's a count with no names — say how many are syncing and
  // stop.
  const offenders = offenderList(health);
  const paused = health.pausedKinds > 0 ? `, ${health.pausedKinds} paused` : '';
  if (!offenders) return `${syncing} syncing${paused}`;
  return `${syncing} — ${offenders} not syncing${paused}`;
}

// Cap lives here, not in the sidecar: how many names fit is a layout question; the
// wire carries the full sorted list.
const OFFENDER_CAP = 3;

// Names only the kinds whose own reason is the cache's: under a Stale or SyncFailed
// verdict, a kind still on its cold list is neither.
export function offenderList(health: ClusterCacheHealth): string {
  // Plural alone; the ref's api group is for keying, not display.
  const names = health.unhealthyKindRefs.filter((k) => k.reason === health.reason).map((k) => k.resource);
  if (names.length <= OFFENDER_CAP) return names.join(', ');
  return `${names.slice(0, OFFENDER_CAP).join(', ')} +${names.length - OFFENDER_CAP} more`;
}

// One line saying why a bad sync verdict is bad, off the rollup alone. A muted verdict
// names its own cause; a reason the rollup cannot explain gets no line rather than a guess.
export function syncReason(status: Status, health: ClusterCacheHealth | null | undefined): string | null {
  if (!health || (status.tone !== 'error' && status.tone !== 'attention')) return null;
  if (health.reason === 'Stale') {
    const offenders = offenderList(health);
    return offenders ? `${offenders} not receiving updates.` : 'The watch may have stopped delivering updates.';
  }
  if (health.reason === 'StoreFailed') return 'The cache file will not open.';
  if (health.unhealthyKinds > 0) return kindsSyncingLabel(health);
  return null;
}
