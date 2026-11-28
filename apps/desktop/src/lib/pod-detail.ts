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

// What a pod's columns and its detail read off its body beyond its status: readiness,
// restarts, its last termination, and each container's state and resources.
import type { StatusTone } from '@/lib/cluster-health';
import type { ContainerSpec, ContainerStatus, PodBody } from '@/lib/pod-status';

// Denominator is the spec count (kubectl's), so a pod whose statuses haven't all landed
// still reads against the expected total.
export function podReady(b: PodBody): { ready: number; total: number } | undefined {
  const cs = b.status?.containerStatuses ?? [];
  const total = b.spec?.containers?.length ?? cs.length;
  return total ? { ready: cs.filter((c) => c.ready).length, total } : undefined;
}

const allStatuses = (b: PodBody) => [
  ...(b.status?.initContainerStatuses ?? []),
  ...(b.status?.containerStatuses ?? []),
];

export function podRestarts(b: PodBody): number | undefined {
  // Init-container retries count too: during init they're the only restarts
  // recorded, and a sidecar keeps accruing them afterwards.
  const all = allStatuses(b);
  return all.length === 0 ? undefined : all.reduce((n, c) => n + (c.restartCount ?? 0), 0);
}

export type Termination = { reason?: string; exitCode?: number; finishedAt: string };

// The newest of the containers' last terminations.
export function podLastTermination(b: PodBody): Termination | undefined {
  let newest: Termination | undefined;
  let newestMs = Number.NEGATIVE_INFINITY;
  for (const c of allStatuses(b)) {
    const t = c.lastState?.terminated;
    const ms = t?.finishedAt ? Date.parse(t.finishedAt) : Number.NaN;
    if (!t?.finishedAt || Number.isNaN(ms) || ms <= newestMs) continue;
    newest = { reason: t.reason, exitCode: t.exitCode, finishedAt: t.finishedAt };
    newestMs = ms;
  }
  return newest;
}

// A container's state as its pill draws it.
type StatePill = { label: string; tone: StatusTone };

// Waiting reasons that are a container on its way up, not stuck.
const STARTING = new Set(['ContainerCreating', 'PodInitializing']);

// A container's state as its status reports it, or nothing before it has one.
export function containerState(c: ContainerStatus | undefined): StatePill | undefined {
  const state = c?.state;
  if (state?.waiting) {
    const reason = state.waiting.reason || 'Waiting';
    return { label: reason, tone: STARTING.has(reason) || reason === 'Waiting' ? 'pending' : 'failing' };
  }
  if (state?.running) return { label: 'Running', tone: c?.ready ? 'ok' : 'pending' };
  if (state?.terminated) {
    const reason = state.terminated.reason || 'Terminated';
    return { label: reason, tone: reason === 'Completed' ? 'ok' : 'failing' };
  }
  return undefined;
}

export type PodContainer = {
  name: string;
  image?: string;
  init: boolean;
  state?: StatePill;
  restarts?: number;
};

function containerRows(specs: ContainerSpec[] | undefined, statuses: ContainerStatus[] | undefined, init: boolean) {
  return (specs ?? []).map((spec): PodContainer => {
    const status = statuses?.find((s) => s.name === spec.name);
    return {
      name: spec.name ?? '',
      image: spec.image,
      init,
      state: containerState(status),
      restarts: status?.restartCount,
    };
  });
}

// The containers, then the init containers, each joined to its status by name.
export function podContainers(b: PodBody): PodContainer[] {
  return [
    ...containerRows(b.spec?.containers, b.status?.containerStatuses, false),
    ...containerRows(b.spec?.initContainers, b.status?.initContainerStatuses, true),
  ];
}

export type ContainerResources = { name: string; cpu: string; memory: string };

const pair = (request: string | undefined, limit: string | undefined) => `${request || '—'} / ${limit || '—'}`;

// Each container's requests and limits, `request / limit`, a dash for a side unset.
export function podResources(b: PodBody): ContainerResources[] {
  return (b.spec?.containers ?? []).map((c) => ({
    name: c.name ?? '',
    cpu: pair(c.resources?.requests?.cpu, c.resources?.limits?.cpu),
    memory: pair(c.resources?.requests?.memory, c.resources?.limits?.memory),
  }));
}
