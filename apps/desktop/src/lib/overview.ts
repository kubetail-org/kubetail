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

// What Dashboard → Overview shows, derived from the watches it reads. Every number comes
// from here, so a tile and the card behind it cannot disagree. `useOverview` is the one
// hook the page reads: a summary served by the sidecar would replace its watches, not the
// sections.
import { useEffect, useMemo, useState } from 'react';

import { useActiveCluster } from '@/lib/active-cluster';
import { useClusterCachedDataEvents } from '@/lib/cluster-cached-data-events';
import type { ClusterCachedDataEvent } from '@/lib/cluster-cached-data-events';
import { useClusterCachedDataObjects } from '@/lib/cluster-cached-data-objects';
import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';
import { cacheCatchingUp } from '@/lib/cluster-health';
import { useClusterNamespaces } from '@/lib/cluster-namespaces';
import type { SubscriptionPhase } from '@/lib/graphql/use-folded-subscription';
import { useNamespaceScope } from '@/lib/namespace-scope';
import { ownerOf } from '@/lib/object-owner';
import { podStatus } from '@/lib/pod-status';
import type { PodBody, PodStatus } from '@/lib/pod-status';

export const PODS = { apiVersion: 'v1', resource: 'pods' };
const NODES = { apiVersion: 'v1', resource: 'nodes' };
const DEPLOYMENTS = { apiVersion: 'apps/v1', resource: 'deployments' };
const STATEFULSETS = { apiVersion: 'apps/v1', resource: 'statefulsets' };
const DAEMONSETS = { apiVersion: 'apps/v1', resource: 'daemonsets' };
const CRONJOBS = { apiVersion: 'batch/v1', resource: 'cronjobs' };
const JOBS = { apiVersion: 'batch/v1', resource: 'jobs' };

const HOUR_MS = 60 * 60 * 1000;

// The page can mount into a stream a kind page or the object panel already runs, and would
// never see its snapshot.
const UNSHARED = { unshared: true };

export type NodeCounts = { ready: number; total: number };

// Pods in scope, Succeeded ones left out: a finished Job's pod is not "not running".
export type PodCounts = { running: number; failing: number; pending: number; total: number };

// A failing or pending pod, with when its problem began.
export type ProblemPod = { object: ClusterCachedDataObject; status: PodStatus; sinceMs: number };

export type PodSummary = { counts: PodCounts; problems: ProblemPod[] };

export type NamespaceRow = { name: string; workloads: number; pods: number };

// Each section's data, or undefined until every watch it reads has its snapshot.
export type Overview = {
  // Whether the cluster has a synced cache to read.
  active: boolean;
  // Whether that cache is still catching up with the cluster, so the sections may be short.
  syncing: boolean;
  nodes?: NodeCounts;
  pods?: PodSummary;
  namespaces?: NamespaceRow[];
  // Warnings in scope seen in the last hour, newest first.
  warnings?: ClusterCachedDataEvent[];
};

function inScope(scope: string | undefined, namespace: string): boolean {
  return scope === undefined || namespace === scope;
}

type NodeBody = { status?: { conditions?: { type?: string; status?: string }[] } };

// Nodes are cluster-scoped, so the namespace scope does not apply.
export function nodeCounts(nodes: readonly ClusterCachedDataObject[]): NodeCounts {
  const ready = nodes.filter((n) =>
    (n.rawJSON as NodeBody | null)?.status?.conditions?.some((c) => c.type === 'Ready' && c.status === 'True'),
  ).length;
  return { ready, total: nodes.length };
}

// When a pod's problem began: its Ready condition's last transition, else its start, else
// its creation. Kubernetes records no time for a container's waiting state.
export function podSince(pod: ClusterCachedDataObject): number {
  const b = (pod.rawJSON ?? {}) as PodBody;
  const at =
    b.status?.conditions?.find((c) => c.type === 'Ready')?.lastTransitionTime ||
    b.status?.startTime ||
    pod.creationTimestamp;
  return at ? Date.parse(at) : Number.NaN;
}

// Failing before pending, then the longest-standing first; a pod with no time last.
function byProblem(a: ProblemPod, b: ProblemPod): number {
  if (a.status.tone !== b.status.tone) return a.status.tone === 'failing' ? -1 : 1;
  const aUndated = Number.isNaN(a.sinceMs);
  const bUndated = Number.isNaN(b.sinceMs);
  if (aUndated || bUndated) return Number(aUndated) - Number(bUndated);
  return a.sinceMs - b.sinceMs;
}

// The pods tile's counts and the problem pods behind them, from one pass, so the tile's
// failing + pending is always the card's count. A pod with no status yet is left out.
export function podSummary(pods: readonly ClusterCachedDataObject[], namespace: string | undefined): PodSummary {
  const counts: PodCounts = { running: 0, failing: 0, pending: 0, total: 0 };
  const problems: ProblemPod[] = [];
  for (const object of pods) {
    if (!inScope(namespace, object.namespace)) continue;
    const body = (object.rawJSON ?? {}) as PodBody;
    if (body.status?.phase === 'Succeeded') continue;
    const status = podStatus(body);
    if (!status) continue;
    counts.total += 1;
    if (status.tone === 'ok') {
      counts.running += 1;
    } else {
      counts[status.tone] += 1;
      problems.push({ object, status, sinceMs: podSince(object) });
    }
  }
  return { counts, problems: problems.toSorted(byProblem) };
}

const OWNER_KIND: Record<string, string> = {
  deploy: 'Deployment',
  replicaset: 'ReplicaSet',
  statefulset: 'StatefulSet',
  daemonset: 'DaemonSet',
  job: 'Job',
  cronjob: 'CronJob',
};

// "Pod · Deployment worker", from `ownerOf`'s "deploy/worker"; an owner of another kind as
// `ownerOf` writes it; "Pod" with none.
export function podOwnerLine(pod: ClusterCachedDataObject): string {
  const owner = ownerOf(pod.rawJSON);
  if (!owner) return 'Pod';
  const slash = owner.indexOf('/');
  const kind = OWNER_KIND[owner.slice(0, slash)];
  return kind ? `Pod · ${kind} ${owner.slice(slash + 1)}` : `Pod · ${owner}`;
}

type OwnedBody = { metadata?: { ownerReferences?: { kind?: string }[] } };

// Workloads per namespace: Deployments, StatefulSets, DaemonSets, CronJobs, and the Jobs no
// CronJob owns. A Deployment's ReplicaSets are not among the lists, so they never count.
export function workloadsByNamespace(
  lists: readonly (readonly ClusterCachedDataObject[])[],
  jobs: readonly ClusterCachedDataObject[],
): Map<string, number> {
  const counts = new Map<string, number>();
  const add = (o: ClusterCachedDataObject) => counts.set(o.namespace, (counts.get(o.namespace) ?? 0) + 1);
  for (const list of lists) list.forEach(add);
  for (const job of jobs) {
    const owners = (job.rawJSON as OwnedBody | null)?.metadata?.ownerReferences ?? [];
    if (!owners.some((r) => r.kind === 'CronJob')) add(job);
  }
  return counts;
}

// The namespaces in scope, each with its workloads and every pod in it, Succeeded ones too:
// the most pods first, then by name.
export function namespaceRows(
  namespaces: readonly string[],
  namespace: string | undefined,
  pods: readonly ClusterCachedDataObject[],
  workloads: ReadonlyMap<string, number>,
): NamespaceRow[] {
  const podCounts = new Map<string, number>();
  for (const p of pods) podCounts.set(p.namespace, (podCounts.get(p.namespace) ?? 0) + 1);
  return namespaces
    .filter((n) => inScope(namespace, n))
    .map((name) => ({ name, workloads: workloads.get(name) ?? 0, pods: podCounts.get(name) ?? 0 }))
    .toSorted((a, b) => b.pods - a.pods || a.name.localeCompare(b.name));
}

// Warning events in scope last seen within the hour before `now`, in the order given. Each
// event object counts once, whatever its `count`. One seen after `now` (clock skew) is recent.
export function recentWarnings(
  events: readonly ClusterCachedDataEvent[],
  namespace: string | undefined,
  now: number,
): ClusterCachedDataEvent[] {
  return events.filter((e) => {
    if (e.type !== 'Warning' || !inScope(namespace, e.involvedNamespace)) return false;
    const seen = e.lastSeen ? Date.parse(e.lastSeen) : Number.NaN;
    return seen > now - HOUR_MS;
  });
}

// The clock, read again every `tickMs`.
function useNow(tickMs: number): number {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), tickMs);
    return () => clearInterval(id);
  }, [tickMs]);
  return now;
}

type Watch = { active: boolean; phase: SubscriptionPhase };

function loaded(...watches: Watch[]): boolean {
  return watches.every((w) => w.active && w.phase !== 'connecting');
}

// The Overview's sections under the namespace scope. `tickMs` paces the warnings' one-hour
// window.
export function useOverview({ tickMs = 60_000 }: { tickMs?: number } = {}): Overview {
  const { namespace } = useNamespaceScope();
  const { cluster } = useActiveCluster();
  const nodes = useClusterCachedDataObjects(NODES, UNSHARED);
  const pods = useClusterCachedDataObjects(PODS, UNSHARED);
  const deployments = useClusterCachedDataObjects(DEPLOYMENTS, UNSHARED);
  const statefulsets = useClusterCachedDataObjects(STATEFULSETS, UNSHARED);
  const daemonsets = useClusterCachedDataObjects(DAEMONSETS, UNSHARED);
  const cronjobs = useClusterCachedDataObjects(CRONJOBS, UNSHARED);
  const jobs = useClusterCachedDataObjects(JOBS, UNSHARED);
  const events = useClusterCachedDataEvents(UNSHARED);
  const ns = useClusterNamespaces();
  const now = useNow(tickMs);

  const nodesLoaded = loaded(nodes);
  const podsLoaded = loaded(pods);
  const namespacesLoaded = loaded(ns, pods, deployments, statefulsets, daemonsets, cronjobs, jobs);
  const eventsLoaded = loaded(events);

  const nodeValue = useMemo(() => (nodesLoaded ? nodeCounts(nodes.objects) : undefined), [nodesLoaded, nodes.objects]);
  const podValue = useMemo(
    () => (podsLoaded ? podSummary(pods.objects, namespace) : undefined),
    [podsLoaded, pods.objects, namespace],
  );
  const workloads = useMemo(
    () =>
      workloadsByNamespace(
        [deployments.objects, statefulsets.objects, daemonsets.objects, cronjobs.objects],
        jobs.objects,
      ),
    [deployments.objects, statefulsets.objects, daemonsets.objects, cronjobs.objects, jobs.objects],
  );
  const namespaceValue = useMemo(
    () => (namespacesLoaded ? namespaceRows(ns.namespaces, namespace, pods.objects, workloads) : undefined),
    [namespacesLoaded, ns.namespaces, namespace, pods.objects, workloads],
  );
  const warningValue = useMemo(
    () => (eventsLoaded ? recentWarnings(events.events, namespace, now) : undefined),
    [eventsLoaded, events.events, namespace, now],
  );

  return {
    active: nodes.active,
    syncing: nodes.active && cluster !== undefined && cacheCatchingUp(cluster),
    nodes: nodeValue,
    pods: podValue,
    namespaces: namespaceValue,
    warnings: warningValue,
  };
}
